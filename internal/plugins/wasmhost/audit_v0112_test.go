package wasmhost

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ambra-media/ambra/internal/plugins"
	"github.com/tetratelabs/wazero/sys"
)

// buildAmbraNotifyStdinLogModule constructs a WebAssembly 1.0 module that exports "ambra_notify" () -> (i32).
// When called, it reads the JSON notification Request from WASI stdin (fd 0) into memory[32..],
// forwards those exact bytes to ambra.log(1, 32, nread), and returns 0.
func buildAmbraNotifyStdinLogModule() []byte {
	wasm := []byte{0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00}

	// Types:
	// 0: (i32,i32,i32,i32)->(i32) [fd_read]
	// 1: (i32,i32,i32)->()        [ambra.log]
	// 2: ()->(i32)                [ambra_notify]
	var typeSec []byte
	typeSec = appendULEB128(typeSec, 3)
	typeSec = append(typeSec, 0x60, 4, 0x7f, 0x7f, 0x7f, 0x7f, 1, 0x7f)
	typeSec = append(typeSec, 0x60, 3, 0x7f, 0x7f, 0x7f, 0)
	typeSec = append(typeSec, 0x60, 0, 1, 0x7f)
	wasm = appendSection(wasm, 1, typeSec)

	// Imports:
	// 0: wasi_snapshot_preview1.fd_read (type 0)
	// 1: ambra.log (type 1)
	var importSec []byte
	importSec = appendULEB128(importSec, 2)
	importSec = appendStr(importSec, "wasi_snapshot_preview1")
	importSec = appendStr(importSec, "fd_read")
	importSec = append(importSec, 0x00, 0x00)
	importSec = appendStr(importSec, "ambra")
	importSec = appendStr(importSec, "log")
	importSec = append(importSec, 0x00, 0x01)
	wasm = appendSection(wasm, 2, importSec)

	// Function section: func 2 has type 2
	wasm = appendSection(wasm, 3, []byte{1, 2})

	// Memory section: 1 page
	wasm = appendSection(wasm, 5, []byte{1, 0x00, 1})

	// Export section: "memory" and "ambra_notify"
	var exportSec []byte
	exportSec = appendULEB128(exportSec, 2)
	exportSec = appendStr(exportSec, "memory")
	exportSec = append(exportSec, 0x02, 0x00)
	exportSec = appendStr(exportSec, "ambra_notify")
	exportSec = append(exportSec, 0x00, 0x02)
	wasm = appendSection(wasm, 7, exportSec)

	// Code section for ambra_notify:
	// iovec at mem[0] = {ptr: 32, len: 4096}, nread at mem[8]
	// fd_read(0, 0, 1, 8) -> drop
	// ambra.log(1, 32, i32.load(8))
	// return 0
	var body []byte
	body = append(body, 0x00)       // 0 locals
	body = append(body, 0x41, 0x00) // fd = 0 (stdin)
	body = append(body, 0x41, 0x00) // iovs = 0
	body = append(body, 0x41, 0x01) // iovs_len = 1
	body = append(body, 0x41, 0x08) // nread_ptr = 8
	body = append(body, 0x10, 0x00) // call fd_read
	body = append(body, 0x1a)       // drop
	body = append(body, 0x41, 0x01) // level = 1
	body = append(body, 0x41, 0x20) // msgPtr = 32
	body = append(body, 0x41, 0x08) // addr = 8
	body = append(body, 0x28, 0x02, 0x00) // i32.load align=4 offset=0
	body = append(body, 0x10, 0x01) // call ambra.log
	body = append(body, 0x41, 0x00) // return 0
	body = append(body, 0x0b)       // end

	var codeSec []byte
	codeSec = appendULEB128(codeSec, 1)
	codeSec = appendULEB128(codeSec, uint32(len(body)))
	codeSec = append(codeSec, body...)
	wasm = appendSection(wasm, 10, codeSec)

	// Data section: mem[0..8] = {ptr: 32, len: 4096}
	var initMem [8]byte
	binary.LittleEndian.PutUint32(initMem[0:4], 32)
	binary.LittleEndian.PutUint32(initMem[4:8], 4096)
	var dataSec []byte
	dataSec = appendULEB128(dataSec, 1)
	dataSec = append(dataSec, 0x00, 0x41, 0x00, 0x0b)
	dataSec = appendULEB128(dataSec, uint32(len(initMem)))
	dataSec = append(dataSec, initMem[:]...)
	wasm = appendSection(wasm, 11, dataSec)
	return wasm
}

func TestAuditV0112_RedirectAllowedHostsEnforcementAndWasmNotifier(t *testing.T) {
	ctx := context.Background()
	host, err := NewHost(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer host.Close(ctx)

	// H-12: Target server on "localhost" (unallowed) vs initial server on "127.0.0.1" (allowed).
	var unallowedHits atomic.Int32
	evilSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		unallowedHits.Add(1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"Name":"ShouldNeverReach"}`))
	}))
	defer evilSrv.Close()
	evilParsed, _ := url.Parse(evilSrv.URL)
	unallowedRedirectURL := "http://localhost:" + evilParsed.Port() + "/secret"

	redirSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/ok-hop" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"Name":"AllowedRedirectOK"}`))
			return
		}
		if r.URL.Path == "/redir-allowed" {
			http.Redirect(w, r, "/ok-hop", http.StatusFound)
			return
		}
		http.Redirect(w, r, unallowedRedirectURL, http.StatusFound)
	}))
	defer redirSrv.Close()

	host.SetHTTPClient(redirSrv.Client())

	// 1. 302 redirect from allowed 127.0.0.1 to unallowed "localhost" MUST fail with HostHTTPErrForbidden (-3).
	reqEvilJSON, _ := json.Marshal(hostHTTPRequestPayload{
		Method: http.MethodGet,
		URL:    redirSrv.URL + "/redir-evil",
	})
	wasmEvil := buildAmbraHTTPAndLogModule("check-redir", string(reqEvilJSON), 4096)
	plugEvil, err := host.LoadModuleWithAllowedHosts(
		ctx,
		plugins.Manifest{ID: "ext.redir.block", Enabled: true, Kinds: []plugins.Kind{plugins.KindMetadata}},
		[]string{"127.0.0.1"},
		wasmEvil,
		5*time.Second,
	)
	if err != nil {
		t.Fatal(err)
	}
	_, err = plugEvil.FetchMetadata(ctx, plugins.MediaQuery{Title: "Test"})
	if err == nil {
		t.Fatal("expected 302 redirect to unallowed host to fail")
	}
	var exitErr *sys.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != uint32(-HostHTTPErrForbidden) {
		t.Fatalf("expected HostHTTPErrForbidden exit code %d, got err=%v", -HostHTTPErrForbidden, err)
	}
	if unallowedHits.Load() != 0 {
		t.Fatalf("unallowed redirect target was contacted %d times!", unallowedHits.Load())
	}

	// 2. 302 redirect staying on allowed host 127.0.0.1 succeeds!
	reqAllowedJSON, _ := json.Marshal(hostHTTPRequestPayload{
		Method: http.MethodGet,
		URL:    redirSrv.URL + "/redir-allowed",
	})
	wasmOK := buildAmbraHTTPAndLogModule("check-ok", string(reqAllowedJSON), 4096)
	plugOK, err := host.LoadModuleWithAllowedHosts(
		ctx,
		plugins.Manifest{ID: "ext.redir.ok", Enabled: true, Kinds: []plugins.Kind{plugins.KindMetadata}},
		[]string{"127.0.0.1"},
		wasmOK,
		5*time.Second,
	)
	if err != nil {
		t.Fatal(err)
	}
	// FetchMetadata parses the stdout hostHTTPResponsePayload (which has Body string, Status int — valid JSON object)
	if _, err := plugOK.FetchMetadata(ctx, plugins.MediaQuery{Title: "OK"}); err != nil {
		t.Fatalf("expected 302 redirect to allowed host 127.0.0.1 to succeed, got %v", err)
	}

	// 3. 3.1-5: WasmPlugin.Notify returns nil cleanly when ambra_notify is NOT exported.
	noNotifyPlug, err := host.LoadModule(
		ctx,
		plugins.Manifest{ID: "ext.no.notify", Enabled: true, Kinds: []plugins.Kind{plugins.KindMetadata}},
		buildNoopModule(),
		time.Second,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := noNotifyPlug.Notify(ctx, plugins.NotificationPayload{Event: "playback.start", Title: "Movie"}); err != nil {
		t.Fatalf("expected nil error when ambra_notify is not exported, got %v", err)
	}

	// 4. 3.1-5: WasmPlugin.Notify invokes exported ambra_notify and forwards JSON event payload!
	var (
		logMu      sync.Mutex
		loggedMsgs []string
	)
	host.mu.Lock()
	host.logHook = func(pluginID string, level uint32, msg string) {
		if pluginID == "ext.with.notify" {
			logMu.Lock()
			loggedMsgs = append(loggedMsgs, msg)
			logMu.Unlock()
		}
	}
	host.mu.Unlock()

	notifyPlug, err := host.LoadModule(
		ctx,
		plugins.Manifest{ID: "ext.with.notify", Enabled: true, Kinds: []plugins.Kind{plugins.KindNotifier}},
		buildAmbraNotifyStdinLogModule(),
		2*time.Second,
	)
	if err != nil {
		t.Fatal(err)
	}
	_ = notifyPlug.Configure(map[string]any{"webhook": "https://hooks.example.test", "enabled": true})

	reg := plugins.NewRegistry()
	reg.Register(notifyPlug)
	notifiers := reg.Notifiers()
	if len(notifiers) != 1 {
		t.Fatalf("expected WasmPlugin registered in registry.Notifiers(), got %d", len(notifiers))
	}
	if err := notifiers[0].Notify(ctx, plugins.NotificationPayload{
		Event:   "library.item_added",
		Title:   "Dune: Part Two",
		Message: "Added to Movies",
	}); err != nil {
		t.Fatalf("WasmPlugin.Notify failed: %v", err)
	}

	logMu.Lock()
	defer logMu.Unlock()
	if len(loggedMsgs) != 1 || !strings.Contains(loggedMsgs[0], `"action":"notify"`) || !strings.Contains(loggedMsgs[0], `"Dune: Part Two"`) {
		t.Fatalf("expected ambra_notify to receive JSON notification envelope, got %+v", loggedMsgs)
	}
}
