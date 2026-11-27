package wasmhost

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ambra-media/ambra/internal/plugins"
)

// buildNotify2ParamModule builds a WASM module exporting "memory", optional allocator allocExport (i32)->(i32),
// and "ambra_notify" (i32, i32)->(i32) that logs memory[ptr..ptr+len] via ambra.log(level, ptr, len) and returns retCode.
func buildNotify2ParamModule(allocExport string, allocPtr int32, logLevel int32, retCode int32) []byte {
	wasm := []byte{0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00}

	// Types:
	// 0: (i32, i32, i32) -> ()    [ambra.log]
	// 1: (i32, i32) -> (i32)      [ambra_notify]
	// 2: (i32) -> (i32)           [allocator, optional]
	var typeSec []byte
	if allocExport != "" {
		typeSec = appendULEB128(typeSec, 3)
	} else {
		typeSec = appendULEB128(typeSec, 2)
	}
	typeSec = append(typeSec, 0x60, 3, 0x7f, 0x7f, 0x7f, 0)
	typeSec = append(typeSec, 0x60, 2, 0x7f, 0x7f, 1, 0x7f)
	if allocExport != "" {
		typeSec = append(typeSec, 0x60, 1, 0x7f, 1, 0x7f)
	}
	wasm = appendSection(wasm, 1, typeSec)

	// Imports: func 0 = ambra.log (type 0)
	var importSec []byte
	importSec = appendULEB128(importSec, 1)
	importSec = appendStr(importSec, "ambra")
	importSec = appendStr(importSec, "log")
	importSec = append(importSec, 0x00, 0x00)
	wasm = appendSection(wasm, 2, importSec)

	// Functions: func 1 = ambra_notify (type 1), optional func 2 = allocator (type 2)
	if allocExport != "" {
		wasm = appendSection(wasm, 3, []byte{2, 1, 2})
	} else {
		wasm = appendSection(wasm, 3, []byte{1, 1})
	}

	// Memory: 1 page
	wasm = appendSection(wasm, 5, []byte{1, 0x00, 1})

	// Exports
	var exportSec []byte
	if allocExport != "" {
		exportSec = appendULEB128(exportSec, 3)
	} else {
		exportSec = appendULEB128(exportSec, 2)
	}
	exportSec = appendStr(exportSec, "memory")
	exportSec = append(exportSec, 0x02, 0x00)
	exportSec = appendStr(exportSec, "ambra_notify")
	exportSec = append(exportSec, 0x00, 0x01)
	if allocExport != "" {
		exportSec = appendStr(exportSec, allocExport)
		exportSec = append(exportSec, 0x00, 0x02)
	}
	wasm = appendSection(wasm, 7, exportSec)

	// Code for func 1 (ambra_notify):
	// i32.const logLevel; local.get 0; local.get 1; call 0; i32.const retCode; end
	var notifyBody []byte
	notifyBody = append(notifyBody, 0x00) // 0 locals
	notifyBody = append(notifyBody, 0x41)
	notifyBody = appendSLEB128(notifyBody, logLevel)
	notifyBody = append(notifyBody, 0x20, 0x00) // local.get 0 (ptr)
	notifyBody = append(notifyBody, 0x20, 0x01) // local.get 1 (len)
	notifyBody = append(notifyBody, 0x10, 0x00) // call ambra.log
	notifyBody = append(notifyBody, 0x41)
	notifyBody = appendSLEB128(notifyBody, retCode)
	notifyBody = append(notifyBody, 0x0b)

	var codeSec []byte
	if allocExport != "" {
		codeSec = appendULEB128(codeSec, 2)
	} else {
		codeSec = appendULEB128(codeSec, 1)
	}
	codeSec = appendULEB128(codeSec, uint32(len(notifyBody)))
	codeSec = append(codeSec, notifyBody...)

	if allocExport != "" {
		var allocBody []byte
		allocBody = append(allocBody, 0x00, 0x41)
		allocBody = appendSLEB128(allocBody, allocPtr)
		allocBody = append(allocBody, 0x0b)
		codeSec = appendULEB128(codeSec, uint32(len(allocBody)))
		codeSec = append(codeSec, allocBody...)
	}
	wasm = appendSection(wasm, 10, codeSec)
	return wasm
}

// buildNotifyBadSigModule builds a WASM module exporting "ambra_notify" with 1 parameter (unsupported).
func buildNotifyBadSigModule() []byte {
	wasm := []byte{0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00}
	wasm = appendSection(wasm, 1, []byte{1, 0x60, 1, 0x7f, 1, 0x7f})
	wasm = appendSection(wasm, 3, []byte{1, 0})
	wasm = appendSection(wasm, 5, []byte{1, 0x00, 1})
	var exportSec []byte
	exportSec = appendULEB128(exportSec, 2)
	exportSec = appendStr(exportSec, "memory")
	exportSec = append(exportSec, 0x02, 0x00)
	exportSec = appendStr(exportSec, "ambra_notify")
	exportSec = append(exportSec, 0x00, 0x00)
	wasm = appendSection(wasm, 7, exportSec)
	body := []byte{0x00, 0x20, 0x00, 0x0b}
	wasm = appendSection(wasm, 10, append([]byte{1, byte(len(body))}, body...))
	return wasm
}

// buildNotifyStderrExitModule exports "ambra_notify" ()->() that writes stderrText to fd 2 and calls proc_exit(exitCode).
func buildNotifyStderrExitModule(stderrText string, exitCode int32) []byte {
	wasm := []byte{0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00}
	var typeSec []byte
	typeSec = appendULEB128(typeSec, 3)
	typeSec = append(typeSec, 0x60, 4, 0x7f, 0x7f, 0x7f, 0x7f, 1, 0x7f) // fd_write
	typeSec = append(typeSec, 0x60, 1, 0x7f, 0)                         // proc_exit
	typeSec = append(typeSec, 0x60, 0, 0)                               // ambra_notify
	wasm = appendSection(wasm, 1, typeSec)

	var importSec []byte
	importSec = appendULEB128(importSec, 2)
	importSec = appendStr(importSec, "wasi_snapshot_preview1")
	importSec = appendStr(importSec, "fd_write")
	importSec = append(importSec, 0x00, 0x00)
	importSec = appendStr(importSec, "wasi_snapshot_preview1")
	importSec = appendStr(importSec, "proc_exit")
	importSec = append(importSec, 0x00, 0x01)
	wasm = appendSection(wasm, 2, importSec)

	wasm = appendSection(wasm, 3, []byte{1, 2})
	wasm = appendSection(wasm, 5, []byte{1, 0x00, 1})

	var exportSec []byte
	exportSec = appendULEB128(exportSec, 2)
	exportSec = appendStr(exportSec, "memory")
	exportSec = append(exportSec, 0x02, 0x00)
	exportSec = appendStr(exportSec, "ambra_notify")
	exportSec = append(exportSec, 0x00, 0x02)
	wasm = appendSection(wasm, 7, exportSec)

	var body []byte
	body = append(body, 0x00)
	if stderrText != "" {
		body = append(body, 0x41, 0x02, 0x41, 0x00, 0x41, 0x01, 0x41, 0x08, 0x10, 0x00, 0x1a)
	}
	body = append(body, 0x41)
	body = appendSLEB128(body, exitCode)
	body = append(body, 0x10, 0x01, 0x0b)

	var codeSec []byte
	codeSec = appendULEB128(codeSec, 1)
	codeSec = appendULEB128(codeSec, uint32(len(body)))
	codeSec = append(codeSec, body...)
	wasm = appendSection(wasm, 10, codeSec)

	memInit := make([]byte, 16+len(stderrText))
	binary.LittleEndian.PutUint32(memInit[0:4], 16)
	binary.LittleEndian.PutUint32(memInit[4:8], uint32(len(stderrText)))
	copy(memInit[16:], stderrText)
	var dataSec []byte
	dataSec = appendULEB128(dataSec, 1)
	dataSec = append(dataSec, 0x00, 0x41, 0x00, 0x0b)
	dataSec = appendULEB128(dataSec, uint32(len(memInit)))
	dataSec = append(dataSec, memInit...)
	wasm = appendSection(wasm, 11, dataSec)
	return wasm
}

// buildHostLogEdgeModule calls ambra.log with zero len, huge len, out-of-bounds ptr, and all log levels.
func buildHostLogEdgeModule() []byte {
	wasm := []byte{0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00}
	var typeSec []byte
	typeSec = appendULEB128(typeSec, 2)
	typeSec = append(typeSec, 0x60, 3, 0x7f, 0x7f, 0x7f, 0)
	typeSec = append(typeSec, 0x60, 0, 0)
	wasm = appendSection(wasm, 1, typeSec)

	var importSec []byte
	importSec = appendULEB128(importSec, 1)
	importSec = appendStr(importSec, "ambra")
	importSec = appendStr(importSec, "log")
	importSec = append(importSec, 0x00, 0x00)
	wasm = appendSection(wasm, 2, importSec)

	wasm = appendSection(wasm, 3, []byte{1, 1})
	wasm = appendSection(wasm, 5, []byte{1, 0x00, 2})

	var exportSec []byte
	exportSec = appendULEB128(exportSec, 2)
	exportSec = appendStr(exportSec, "memory")
	exportSec = append(exportSec, 0x02, 0x00)
	exportSec = appendStr(exportSec, "_start")
	exportSec = append(exportSec, 0x00, 0x01)
	wasm = appendSection(wasm, 7, exportSec)

	var body []byte
	body = append(body, 0x00)
	// 1) msgLen == 0
	body = append(body, 0x41, 0x00, 0x41, 0x00, 0x41, 0x00, 0x10, 0x00)
	// 2) msgLen > maxHostLogMsgBytes (e.g. 70000) reading from ptr 0 (within 2 pages = 131072 bytes)
	body = append(body, 0x41, 0x00, 0x41, 0x00, 0x41)
	body = appendSLEB128(body, 70000)
	body = append(body, 0x10, 0x00)
	// 3) out-of-bounds ptr (200000, len 10)
	body = append(body, 0x41, 0x01, 0x41)
	body = appendSLEB128(body, 200000)
	body = append(body, 0x41, 0x0a, 0x10, 0x00)
	// 4) levels 2 (Warn) and 3 (Error)
	body = append(body, 0x41, 0x02, 0x41, 0x00, 0x41, 0x04, 0x10, 0x00)
	body = append(body, 0x41, 0x03, 0x41, 0x00, 0x41, 0x04, 0x10, 0x00)
	body = append(body, 0x0b)

	var codeSec []byte
	codeSec = appendULEB128(codeSec, 1)
	codeSec = appendULEB128(codeSec, uint32(len(body)))
	codeSec = append(codeSec, body...)
	wasm = appendSection(wasm, 10, codeSec)

	var dataSec []byte
	dataSec = appendULEB128(dataSec, 1)
	dataSec = append(dataSec, 0x00, 0x41, 0x00, 0x0b, 4, 't', 'e', 's', 't')
	wasm = appendSection(wasm, 11, dataSec)
	return wasm
}

func TestWasmhostCovV0112_SubtitlesSearchAndDownload(t *testing.T) {
	ctx := context.Background()
	host, err := NewHost(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer host.Close(ctx)

	host.SetHTTPClient(nil) // nil is a safe no-op

	// 1. Plugin that does not support KindSubtitle returns (nil, nil)
	metaOnly, err := host.LoadModule(ctx, plugins.Manifest{
		ID:      "ext.meta.only",
		Enabled: true,
		Kinds:   []plugins.Kind{plugins.KindMetadata},
	}, buildNoopModule(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if subs, err := metaOnly.SearchSubtitles(ctx, plugins.SubtitleSearchQuery{}); err != nil || subs != nil {
		t.Fatalf("expected (nil, nil) for non-subtitle plugin SearchSubtitles, got (%v, %v)", subs, err)
	}
	if dl, err := metaOnly.DownloadSubtitle(ctx, "sub-1"); err != nil || dl != nil {
		t.Fatalf("expected (nil, nil) for non-subtitle plugin DownloadSubtitle, got (%v, %v)", dl, err)
	}

	// 2. Empty stdout on SearchSubtitles & DownloadSubtitle returns (nil, nil)
	emptySub, err := host.LoadModule(ctx, plugins.Manifest{
		ID:      "ext.sub.empty",
		Enabled: true,
		Kinds:   []plugins.Kind{plugins.KindSubtitle},
	}, buildWasiOutputModule(1, "   \n", false, 0), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if subs, err := emptySub.SearchSubtitles(ctx, plugins.SubtitleSearchQuery{}); err != nil || subs != nil {
		t.Fatalf("expected (nil, nil) for empty SearchSubtitles, got (%v, %v)", subs, err)
	}
	if dl, err := emptySub.DownloadSubtitle(ctx, "sub-1"); err != nil || dl != nil {
		t.Fatalf("expected (nil, nil) for empty DownloadSubtitle, got (%v, %v)", dl, err)
	}

	// 3. SearchSubtitles: JSON array, wrapped "subtitles", wrapped "results", and invalid JSON cases
	for _, tc := range []struct {
		id      string
		out     string
		wantLen int
		wantErr bool
	}{
		{id: "ext.sub.arr", out: `[{"Id":"s1","Name":"Eng","Language":"en","Format":"srt"}]`, wantLen: 1},
		{id: "ext.sub.wrap1", out: `{"subtitles":[{"Id":"s2","Name":"Chs","Language":"zh","Format":"ass"}]}`, wantLen: 1},
		{id: "ext.sub.wrap2", out: `{"results":[{"Id":"s3","Name":"Jpn","Language":"ja","Format":"vtt"}]}`, wantLen: 1},
		{id: "ext.sub.badarr", out: `[{"Id":`, wantErr: true},
		{id: "ext.sub.badobj", out: `{"subtitles":bad}`, wantErr: true},
	} {
		p, err := host.LoadModule(ctx, plugins.Manifest{
			ID:      tc.id,
			Enabled: true,
			Kinds:   []plugins.Kind{plugins.KindSubtitle},
		}, buildWasiOutputModule(1, tc.out, false, 0), time.Second)
		if err != nil {
			t.Fatal(err)
		}
		got, err := p.SearchSubtitles(ctx, plugins.SubtitleSearchQuery{})
		if tc.wantErr {
			if err == nil {
				t.Fatalf("%s: expected error, got %+v", tc.id, got)
			}
			continue
		}
		if err != nil || len(got) != tc.wantLen {
			t.Fatalf("%s: got len=%d err=%v", tc.id, len(got), err)
		}
	}

	// 4. DownloadSubtitle: content_base64 (valid & invalid), uppercase keys with plain SRT vs base64 string, and invalid JSON
	b64Hello := base64.StdEncoding.EncodeToString([]byte("WEBVTT\n\nhello"))
	plainSRT := "1\n00:00:01,000 --> 00:00:02,000\nHello!"
	for _, tc := range []struct {
		id       string
		out      string
		wantLang string
		wantFmt  string
		wantBody string
		wantErr  bool
	}{
		{
			id:       "ext.dl.b64",
			out:      `{"language":"en","format":"vtt","content_base64":"` + b64Hello + `"}`,
			wantLang: "en", wantFmt: "vtt", wantBody: "WEBVTT\n\nhello",
		},
		{
			id:      "ext.dl.badb64",
			out:     `{"language":"en","format":"vtt","content_base64":"%%%not-base64%%%"}`,
			wantErr: true,
		},
		{
			id:       "ext.dl.upper.srt",
			out:      `{"Language":"zh","Format":"srt","Content":"1\n00:00:01,000 --> 00:00:02,000\nHello!"}`,
			wantLang: "zh", wantFmt: "srt", wantBody: plainSRT,
		},
		{
			id:       "ext.dl.upper.b64content",
			out:      `{"Language":"ja","Format":"vtt","Content":"` + b64Hello + `"}`,
			wantLang: "ja", wantFmt: "vtt", wantBody: "WEBVTT\n\nhello",
		},
		{
			id:      "ext.dl.badjson",
			out:     `{not-json`,
			wantErr: true,
		},
	} {
		p, err := host.LoadModule(ctx, plugins.Manifest{
			ID:      tc.id,
			Enabled: true,
			Kinds:   []plugins.Kind{plugins.KindSubtitle},
		}, buildWasiOutputModule(1, tc.out, false, 0), time.Second)
		if err != nil {
			t.Fatal(err)
		}
		res, err := p.DownloadSubtitle(ctx, "id-1")
		if tc.wantErr {
			if err == nil {
				t.Fatalf("%s: expected error, got %+v", tc.id, res)
			}
			continue
		}
		if err != nil || res == nil {
			t.Fatalf("%s: unexpected err=%v res=%+v", tc.id, err, res)
		}
		if res.Language != tc.wantLang || res.Format != tc.wantFmt || string(res.Content) != tc.wantBody {
			t.Fatalf("%s: got (%q, %q, %q), want (%q, %q, %q)", tc.id, res.Language, res.Format, string(res.Content), tc.wantLang, tc.wantFmt, tc.wantBody)
		}
	}
}

func TestWasmhostCovV0112_NotifyVariantsAndHostHelpers(t *testing.T) {
	ctx := context.Background()
	host, err := NewHost(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer host.Close(ctx)

	// 1. Disabled plugin Notify returns nil
	pDisabled, err := host.LoadModule(ctx, plugins.Manifest{ID: "ext.notify.off", Enabled: false}, buildNotify2ParamModule("malloc", 128, 1, 0), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if err := pDisabled.Notify(ctx, plugins.NotificationPayload{Event: "test"}); err != nil {
		t.Fatalf("expected nil for disabled Notify, got %v", err)
	}

	// 2. 2-param ambra_notify with exported malloc succeeds
	pAlloc, err := host.LoadModule(ctx, plugins.Manifest{ID: "ext.notify.malloc", Enabled: true}, buildNotify2ParamModule("malloc", 256, 0, 0), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if err := pAlloc.Notify(ctx, plugins.NotificationPayload{Event: "playback.stop", Title: "Arrival"}); err != nil {
		t.Fatalf("2-param Notify with malloc failed: %v", err)
	}

	// 3. 2-param ambra_notify WITHOUT allocator (uses top of memory) returning negative code -> error
	pNoAllocNeg, err := host.LoadModule(ctx, plugins.Manifest{ID: "ext.notify.neg", Enabled: true}, buildNotify2ParamModule("", 0, 2, -7), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if err := pNoAllocNeg.Notify(ctx, plugins.NotificationPayload{Event: "playback.start"}); err == nil || !strings.Contains(err.Error(), "error code -7") {
		t.Fatalf("expected error code -7, got %v", err)
	}

	// 4. Unsupported 1-param ambra_notify signature
	pBadSig, err := host.LoadModule(ctx, plugins.Manifest{ID: "ext.notify.badsig", Enabled: true}, buildNotifyBadSigModule(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if err := pBadSig.Notify(ctx, plugins.NotificationPayload{Event: "test"}); err == nil || !strings.Contains(err.Error(), "unsupported ambra_notify signature") {
		t.Fatalf("expected unsupported signature error, got %v", err)
	}

	// 5. ambra_notify calling proc_exit(0) returns nil; proc_exit(2) with stderr and without stderr returns error
	pExit0, _ := host.LoadModule(ctx, plugins.Manifest{ID: "ext.notify.exit0", Enabled: true}, buildNotifyStderrExitModule("", 0), time.Second)
	if err := pExit0.Notify(ctx, plugins.NotificationPayload{Event: "ok"}); err != nil {
		t.Fatalf("expected nil on proc_exit(0), got %v", err)
	}
	pExitStderr, _ := host.LoadModule(ctx, plugins.Manifest{ID: "ext.notify.exit2", Enabled: true}, buildNotifyStderrExitModule("boom detail", 2), time.Second)
	if err := pExitStderr.Notify(ctx, plugins.NotificationPayload{Event: "fail"}); err == nil || !strings.Contains(err.Error(), "boom detail") {
		t.Fatalf("expected stderr detail in error, got %v", err)
	}
	pExitNoStderr, _ := host.LoadModule(ctx, plugins.Manifest{ID: "ext.notify.exit3", Enabled: true}, buildNotifyStderrExitModule("", 3), time.Second)
	if err := pExitNoStderr.Notify(ctx, plugins.NotificationPayload{Event: "fail"}); err == nil {
		t.Fatal("expected error on proc_exit(3)")
	}

	// 6. Cancelled context waiting for semaphore slot
	for i := 0; i < cap(host.sem); i++ {
		host.sem <- struct{}{}
	}
	canceledCtx, cancel := context.WithCancel(ctx)
	cancel()
	errSemNotify := pAlloc.Notify(canceledCtx, plugins.NotificationPayload{Event: "blocked"})
	_, errSemInvoke := pAlloc.FetchMetadata(canceledCtx, plugins.MediaQuery{Title: "blocked"})
	for i := 0; i < cap(host.sem); i++ {
		<-host.sem
	}
	if errSemNotify == nil || !strings.Contains(errSemNotify.Error(), "cancelled waiting for execution slot") {
		t.Fatalf("expected sem cancel error in Notify, got %v", errSemNotify)
	}
	if errSemInvoke == nil || !strings.Contains(errSemInvoke.Error(), "cancelled waiting for execution slot") {
		t.Fatalf("expected sem cancel error in invoke, got %v", errSemInvoke)
	}

	// 7. hostLog edge cases & Configure type branches
	pLogEdge, err := host.LoadModule(ctx, plugins.Manifest{ID: "ext.log.edge", Enabled: true}, buildHostLogEdgeModule(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	_ = pLogEdge.Configure(nil)
	_ = pLogEdge.Configure(map[string]any{"Enabled": true, "Priority": int64(9)})
	if pLogEdge.Manifest().Priority != 9 {
		t.Fatalf("expected priority 9, got %d", pLogEdge.Manifest().Priority)
	}
	_ = pLogEdge.Configure(map[string]any{"priority": float64(11)})
	if pLogEdge.Manifest().Priority != 11 {
		t.Fatalf("expected priority 11, got %d", pLogEdge.Manifest().Priority)
	}
	if _, err := pLogEdge.FetchMetadata(ctx, plugins.MediaQuery{Title: "LogEdge"}); err != nil {
		t.Fatalf("hostLog edge module failed: %v", err)
	}
}

func TestWasmhostCovV0112_RedirectClientAndFSChecks(t *testing.T) {
	// 1. wrapWasmHTTPClient CheckRedirect branches
	cDefault := wrapWasmHTTPClient(nil, false)
	manyVia := make([]*http.Request, 10)
	reqOK, _ := http.NewRequest(http.MethodGet, "https://93.184.216.34/next", nil)
	if err := cDefault.CheckRedirect(reqOK, manyVia); err == nil || !strings.Contains(err.Error(), "stopped after 10 redirects") {
		t.Fatalf("expected 10 redirects error, got %v", err)
	}
	if err := cDefault.CheckRedirect(nil, nil); !errors.Is(err, errRedirectHostForbidden) {
		t.Fatalf("expected forbidden for nil req, got %v", err)
	}
	badSchemeReq, _ := http.NewRequest(http.MethodGet, "ftp://example.com/file", nil)
	if err := cDefault.CheckRedirect(badSchemeReq, nil); !errors.Is(err, errRedirectHostForbidden) {
		t.Fatalf("expected forbidden for ftp redirect, got %v", err)
	}
	loopReq, _ := http.NewRequest(http.MethodGet, "http://127.0.0.1:8080/internal", nil)
	if err := cDefault.CheckRedirect(loopReq, nil); !errors.Is(err, errRedirectHostForbidden) {
		t.Fatalf("expected forbidden for loopback redirect on default client, got %v", err)
	}

	// Custom client: metadata IP blocked, cross-origin sensitive headers stripped, prevCheck honored
	cCustom := wrapWasmHTTPClient(&http.Client{}, true)
	metaReq, _ := http.NewRequest(http.MethodGet, "http://169.254.169.254/latest/meta-data", nil)
	if err := cCustom.CheckRedirect(metaReq, nil); !errors.Is(err, errRedirectHostForbidden) {
		t.Fatalf("expected forbidden for cloud metadata IP redirect, got %v", err)
	}

	origURL, _ := url.Parse("https://origin.example.com/start")
	origReq := &http.Request{URL: origURL}
	crossReq, _ := http.NewRequest(http.MethodGet, "https://cdn.other.com/asset", nil)
	crossReq.Header.Set("Authorization", "Bearer secret")
	crossReq.Header.Set("Cookie", "sid=123")
	crossReq.Header.Set("X-Emby-Token", "tok")
	if err := cCustom.CheckRedirect(crossReq, []*http.Request{origReq}); err != nil {
		t.Fatalf("expected cross-origin redirect on custom client to succeed, got %v", err)
	}
	if crossReq.Header.Get("Authorization") != "" || crossReq.Header.Get("Cookie") != "" || crossReq.Header.Get("X-Emby-Token") != "" {
		t.Fatalf("expected sensitive headers stripped on cross-origin redirect, got %+v", crossReq.Header)
	}

	sentinel := errors.New("custom check stop")
	cPrev := wrapWasmHTTPClient(&http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error { return sentinel },
	}, true)
	if err := cPrev.CheckRedirect(crossReq, nil); !errors.Is(err, sentinel) {
		t.Fatalf("expected prevCheck sentinel, got %v", err)
	}

	// 2. matchAllowedHost edge cases
	if matchAllowedHost("", []string{"*"}) {
		t.Fatal("empty targetHost should return false")
	}
	if !matchAllowedHost("sub.example.com.", []string{"", "*.example.com."}) {
		t.Fatal("wildcard subdomain with trailing dot should match")
	}
	if matchAllowedHost("example.com", []string{"*.example.com"}) {
		t.Fatal("apex domain should not match *.example.com")
	}
	if !matchAllowedHost("anything.test", []string{"*"}) {
		t.Fatal("* should match non-empty host")
	}

	// 3. readFileWithLimit & isWithinRealDir
	dir := t.TempDir()
	if _, err := readFileWithLimit(filepath.Join(dir, "nope"), 10); err == nil {
		t.Fatal("expected error for missing file")
	}
	bigFile := filepath.Join(dir, "big.bin")
	if err := os.WriteFile(bigFile, []byte("0123456789abcdef"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := readFileWithLimit(bigFile, 8); err == nil {
		t.Fatal("expected size limit error")
	}
	if isWithinRealDir(filepath.Join(dir, "sub"), dir) {
		t.Fatal("parent dir must not be within child dir")
	}

	// 4. hostHTTPRequest error return codes (bad JSON, empty URL, bad scheme, metadata IP, >2MiB response, network error)
	ctx := context.Background()
	host, err := NewHost(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer host.Close(ctx)

	hugeSrv := newHugeAndHeaderTestServer(t)
	defer hugeSrv.Close()
	host.SetHTTPClient(hugeSrv.Client())

	for _, tc := range []struct {
		id      string
		payload string
	}{
		{id: "ext.http.badjson", payload: `{not-json`},
		{id: "ext.http.emptyurl", payload: `{"url":"   "}`},
		{id: "ext.http.badscheme", payload: `{"url":"ftp://example.com/a"}`},
		{id: "ext.http.metaip", payload: `{"url":"http://169.254.169.254/latest"}`},
		{id: "ext.http.huge", payload: `{"method":"POST","url":"` + hugeSrv.URL + `/huge","headers":{"X-Test":"1"},"body":"hi"}`},
		{id: "ext.http.neterr", payload: `{"url":"http://127.0.0.1:1/unreachable"}`},
	} {
		w := buildAmbraHTTPAndLogModule("http-edge", tc.payload, 4096)
		plug, err := host.LoadModule(ctx, plugins.Manifest{
			ID:      tc.id,
			Enabled: true,
			Kinds:   []plugins.Kind{plugins.KindMetadata},
		}, w, 2*time.Second)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := plug.FetchMetadata(ctx, plugins.MediaQuery{Title: "Edge"}); err == nil {
			t.Fatalf("%s: expected error from hostHTTPRequest", tc.id)
		}
	}
}

func newHugeAndHeaderTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/huge", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		chunk := make([]byte, 64<<10)
		for i := 0; i < 34; i++ { // > 2 MiB
			_, _ = w.Write(chunk)
		}
	})
	return httptest.NewServer(mux)
}

