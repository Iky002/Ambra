package wasmhost

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ambra-media/ambra/internal/plugins"
	"github.com/tetratelabs/wazero/sys"
)

// --- Minimal valid WebAssembly 1.0 binary builders for pure-Go wazero tests ---

func appendULEB128(dst []byte, v uint32) []byte {
	for {
		b := byte(v & 0x7f)
		v >>= 7
		if v != 0 {
			b |= 0x80
		}
		dst = append(dst, b)
		if v == 0 {
			return dst
		}
	}
}

func appendSLEB128(dst []byte, v int32) []byte {
	for {
		b := byte(v & 0x7f)
		v >>= 7
		if (v == 0 && (b&0x40) == 0) || (v == -1 && (b&0x40) != 0) {
			dst = append(dst, b)
			return dst
		}
		dst = append(dst, b|0x80)
	}
}

func appendSection(dst []byte, id byte, content []byte) []byte {
	dst = append(dst, id)
	dst = appendULEB128(dst, uint32(len(content)))
	return append(dst, content...)
}

func appendStr(dst []byte, s string) []byte {
	dst = appendULEB128(dst, uint32(len(s)))
	return append(dst, s...)
}

// buildWasiOutputModule creates a valid WASI WASM module that writes outText to fd (1=stdout, 2=stderr)
// and optionally calls proc_exit(exitCode) when callExit is true.
func buildWasiOutputModule(fd uint32, outText string, callExit bool, exitCode uint32) []byte {
	wasm := []byte{0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00}

	// Type section:
	// type 0: (i32, i32, i32, i32) -> (i32)  [fd_write]
	// type 1: () -> ()                        [_start]
	// type 2: (i32) -> ()                     [proc_exit] (if callExit)
	var typeSec []byte
	if callExit {
		typeSec = appendULEB128(typeSec, 3)
	} else {
		typeSec = appendULEB128(typeSec, 2)
	}
	typeSec = append(typeSec, 0x60, 4, 0x7f, 0x7f, 0x7f, 0x7f, 1, 0x7f)
	typeSec = append(typeSec, 0x60, 0, 0)
	if callExit {
		typeSec = append(typeSec, 0x60, 1, 0x7f, 0)
	}
	wasm = appendSection(wasm, 1, typeSec)

	// Import section:
	// func 0: wasi_snapshot_preview1.fd_write (type 0)
	// func 1: wasi_snapshot_preview1.proc_exit (type 2) [if callExit]
	var importSec []byte
	if callExit {
		importSec = appendULEB128(importSec, 2)
	} else {
		importSec = appendULEB128(importSec, 1)
	}
	importSec = appendStr(importSec, "wasi_snapshot_preview1")
	importSec = appendStr(importSec, "fd_write")
	importSec = append(importSec, 0x00, 0x00)
	if callExit {
		importSec = appendStr(importSec, "wasi_snapshot_preview1")
		importSec = appendStr(importSec, "proc_exit")
		importSec = append(importSec, 0x00, 0x02)
	}
	wasm = appendSection(wasm, 2, importSec)

	// Function section: 1 local function (_start) of type 1
	wasm = appendSection(wasm, 3, []byte{1, 1})

	// Memory section: 1 memory, min 1 page
	wasm = appendSection(wasm, 5, []byte{1, 0x00, 1})

	// Export section: "memory" (mem 0), "_start" (func startIdx)
	startIdx := byte(1)
	if callExit {
		startIdx = 2
	}
	var exportSec []byte
	exportSec = appendULEB128(exportSec, 2)
	exportSec = appendStr(exportSec, "memory")
	exportSec = append(exportSec, 0x02, 0x00)
	exportSec = appendStr(exportSec, "_start")
	exportSec = append(exportSec, 0x00, startIdx)
	wasm = appendSection(wasm, 7, exportSec)

	// Code section:
	// i32.const fd; i32.const 0 (iovs ptr); i32.const 1 (iovs len); i32.const 8 (nwritten ptr); call 0; drop
	// [if callExit: i32.const exitCode; call 1]
	// end
	var body []byte
	body = append(body, 0x00) // 0 locals
	body = append(body, 0x41)
	body = appendSLEB128(body, int32(fd))
	body = append(body, 0x41, 0x00) // iovs = 0
	body = append(body, 0x41, 0x01) // iovs_len = 1
	body = append(body, 0x41, 0x08) // nwritten = 8
	body = append(body, 0x10, 0x00) // call fd_write
	body = append(body, 0x1a)       // drop
	if callExit {
		body = append(body, 0x41)
		body = appendSLEB128(body, int32(exitCode))
		body = append(body, 0x10, 0x01) // call proc_exit
	}
	body = append(body, 0x0b) // end

	var codeSec []byte
	codeSec = appendULEB128(codeSec, 1)
	codeSec = appendULEB128(codeSec, uint32(len(body)))
	codeSec = append(codeSec, body...)
	wasm = appendSection(wasm, 10, codeSec)

	// Data section:
	// offset 0: iovec { ptr: 16, len: len(outText) } (8 bytes) + 8 bytes padding + outText
	memInit := make([]byte, 16+len(outText))
	binary.LittleEndian.PutUint32(memInit[0:4], 16)
	binary.LittleEndian.PutUint32(memInit[4:8], uint32(len(outText)))
	copy(memInit[16:], outText)

	var dataSec []byte
	dataSec = appendULEB128(dataSec, 1)
	dataSec = append(dataSec, 0x00, 0x41, 0x00, 0x0b) // mem 0, offset i32.const 0
	dataSec = appendULEB128(dataSec, uint32(len(memInit)))
	dataSec = append(dataSec, memInit...)
	wasm = appendSection(wasm, 11, dataSec)

	return wasm
}

// buildInfiniteLoopModule creates a valid WASM module whose _start loops forever.
func buildInfiniteLoopModule() []byte {
	wasm := []byte{0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00}
	wasm = appendSection(wasm, 1, []byte{1, 0x60, 0, 0})    // type 0: ()->()
	wasm = appendSection(wasm, 3, []byte{1, 0})             // func 0: type 0
	wasm = appendSection(wasm, 5, []byte{1, 0x00, 1})       // memory 0: min 1 page
	var exportSec []byte
	exportSec = appendULEB128(exportSec, 2)
	exportSec = appendStr(exportSec, "memory")
	exportSec = append(exportSec, 0x02, 0x00)
	exportSec = appendStr(exportSec, "_start")
	exportSec = append(exportSec, 0x00, 0x00)
	wasm = appendSection(wasm, 7, exportSec)
	// body: 0 locals, loop (blocktype empty=0x40), br 0, end loop, end func
	body := []byte{0x00, 0x03, 0x40, 0x0c, 0x00, 0x0b, 0x0b}
	codeSec := append([]byte{1, byte(len(body))}, body...)
	wasm = appendSection(wasm, 10, codeSec)
	return wasm
}

// buildNoopModule creates a valid WASM module whose _start does nothing.
func buildNoopModule() []byte {
	wasm := []byte{0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00}
	wasm = appendSection(wasm, 1, []byte{1, 0x60, 0, 0})
	wasm = appendSection(wasm, 3, []byte{1, 0})
	wasm = appendSection(wasm, 5, []byte{1, 0x00, 1})
	var exportSec []byte
	exportSec = appendULEB128(exportSec, 2)
	exportSec = appendStr(exportSec, "memory")
	exportSec = append(exportSec, 0x02, 0x00)
	exportSec = appendStr(exportSec, "_start")
	exportSec = append(exportSec, 0x00, 0x00)
	wasm = appendSection(wasm, 7, exportSec)
	body := []byte{0x00, 0x0b}
	codeSec := append([]byte{1, byte(len(body))}, body...)
	wasm = appendSection(wasm, 10, codeSec)
	return wasm
}

func TestHostLoadFromDirAndExecutePlugins(t *testing.T) {
	ctx := context.Background()
	host, err := NewHost(ctx)
	if err != nil {
		t.Fatalf("NewHost failed: %v", err)
	}
	defer func() {
		if err := host.Close(ctx); err != nil {
			t.Fatalf("Host.Close failed: %v", err)
		}
		// Second Close should be a no-op.
		if err := host.Close(ctx); err != nil {
			t.Fatalf("second Host.Close failed: %v", err)
		}
	}()

	// Non-existent dir returns (0, nil).
	reg := plugins.NewRegistry()
	n, err := host.LoadFromDir(ctx, filepath.Join(t.TempDir(), "does-not-exist"), reg)
	if err != nil || n != 0 {
		t.Fatalf("expected (0, nil) for missing dir, got (%d, %v)", n, err)
	}

	pluginsDir := t.TempDir()

	// Loose file and subdir without manifest should be ignored.
	_ = os.WriteFile(filepath.Join(pluginsDir, "readme.txt"), []byte("ignore"), 0o644)
	_ = os.MkdirAll(filepath.Join(pluginsDir, "empty-dir"), 0o755)

	// Non-WASM manifest should be skipped.
	nonWasmDir := filepath.Join(pluginsDir, "http-plugin")
	_ = os.MkdirAll(nonWasmDir, 0o755)
	_ = os.WriteFile(filepath.Join(nonWasmDir, "manifest.json"), []byte(`{"Id":"http-1","Name":"HTTP","Enabled":true}`), 0o644)

	// 1. Metadata WASM plugin (writes MetadataResult JSON to stdout and exits 0 via proc_exit(0)).
	metaDir := filepath.Join(pluginsDir, "douban-wasm")
	_ = os.MkdirAll(metaDir, 0o755)
	metaJSON := `{"Name":"流浪地球","OriginalTitle":"The Wandering Earth","Year":2019,"CommunityRating":8.4,"Genres":["Sci-Fi"]}`
	_ = os.WriteFile(filepath.Join(metaDir, "plugin.wasm"), buildWasiOutputModule(1, metaJSON, true, 0), 0o644)
	_ = os.WriteFile(filepath.Join(metaDir, "manifest.json"), []byte(`{
		"Version": "1.0.0",
		"Description": "Douban WASM scraper",
		"Enabled": true,
		"Priority": 10,
		"Kinds": ["metadata"],
		"WasmFile": "plugin.wasm",
		"TimeoutMs": 2000
	}`), 0o644)

	// 2. Stream resolver WASM plugin (writes StreamResolveResult JSON to stdout).
	streamDir := filepath.Join(pluginsDir, "alist-wasm")
	_ = os.MkdirAll(streamDir, 0o755)
	streamJSON := `{"URL":"https://93.184.216.34/movie.mkv?sig=abc","Headers":{"Referer":"https://93.184.216.34"},"Is302":true}`
	_ = os.WriteFile(filepath.Join(streamDir, "resolver.wasm"), buildWasiOutputModule(1, streamJSON, false, 0), 0o644)
	_ = os.WriteFile(filepath.Join(streamDir, "manifest.json"), []byte(`{
		"Id": "alist-wasm",
		"Name": "Alist Stream Resolver",
		"Version": "1.0.0",
		"Enabled": true,
		"Priority": 20,
		"Kinds": ["stream"],
		"WasmFile": "resolver.wasm"
	}`), 0o644)

	count, err := host.LoadFromDir(ctx, pluginsDir, reg)
	if err != nil {
		t.Fatalf("LoadFromDir failed: %v", err)
	}
	if count != 2 {
		t.Fatalf("expected 2 loaded WASM plugins, got %d", count)
	}

	providers := reg.MetadataProviders()
	if len(providers) != 2 {
		t.Fatalf("expected 2 metadata providers registered, got %d", len(providers))
	}
	// First by priority (10) is douban-wasm (ID defaulted from dir name).
	if providers[0].Manifest().ID != "douban-wasm" || providers[0].Manifest().Name != "douban-wasm" {
		t.Fatalf("unexpected manifest: %+v", providers[0].Manifest())
	}
	if err := providers[0].Configure(map[string]any{"api_key": "secret"}); err != nil {
		t.Fatalf("Configure failed: %v", err)
	}

	metaRes, err := providers[0].FetchMetadata(ctx, plugins.MediaQuery{
		ItemType: "Movie",
		Title:    "流浪地球",
		Year:     2019,
	})
	if err != nil {
		t.Fatalf("FetchMetadata failed: %v", err)
	}
	if metaRes == nil || metaRes.Name != "流浪地球" || metaRes.Year != 2019 || metaRes.CommunityRating != 8.4 {
		t.Fatalf("unexpected metadata result: %+v", metaRes)
	}

	resolvers := reg.StreamResolvers()
	streamRes, err := resolvers[1].ResolveStream(ctx, "/alist/movies/earth.strm")
	if err != nil {
		t.Fatalf("ResolveStream failed: %v", err)
	}
	if streamRes == nil || !streamRes.Is302 || streamRes.URL != "https://93.184.216.34/movie.mkv?sig=abc" || streamRes.Headers["Referer"] != "https://93.184.216.34" {
		t.Fatalf("unexpected stream resolve result: %+v", streamRes)
	}
}

func TestWasmPluginTimeoutTrapAndEdgeCases(t *testing.T) {
	ctx := context.Background()
	host, err := NewHost(ctx)
	if err != nil {
		t.Fatalf("NewHost failed: %v", err)
	}
	defer host.Close(ctx)

	// 1. Empty binary rejected.
	if _, err := host.LoadModule(ctx, plugins.Manifest{ID: "empty"}, nil, 0); err == nil {
		t.Fatal("expected error for empty wasm binary")
	}

	// 2. Invalid wasm binary rejected.
	if _, err := host.LoadModule(ctx, plugins.Manifest{ID: "bad"}, []byte("not-wasm"), 0); err == nil {
		t.Fatal("expected compile error for invalid wasm binary")
	}

	// 3. Infinite loop triggers context timeout circuit-breaker!
	loopPlugin, err := host.LoadModule(ctx, plugins.Manifest{ID: "looper", Enabled: true}, buildInfiniteLoopModule(), 60*time.Millisecond)
	if err != nil {
		t.Fatalf("LoadModule looper failed: %v", err)
	}
	start := time.Now()
	_, err = loopPlugin.FetchMetadata(ctx, plugins.MediaQuery{Title: "Hang"})
	elapsed := time.Since(start)
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("expected timeout error from infinite loop, got: %v", err)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("timeout circuit-breaker took too long: %v", elapsed)
	}

	// 4. Non-zero exit code with stderr message is surfaced.
	failPlugin, err := host.LoadModule(ctx, plugins.Manifest{ID: "failer", Enabled: true}, buildWasiOutputModule(2, "boom error", true, 1), time.Second)
	if err != nil {
		t.Fatalf("LoadModule failer failed: %v", err)
	}
	_, err = failPlugin.ResolveStream(ctx, "/x.strm")
	if err == nil || !strings.Contains(err.Error(), "boom error") {
		t.Fatalf("expected stderr message in error, got: %v", err)
	}

	// 5. Empty stdout returns (nil, nil) for both FetchMetadata and ResolveStream.
	noopPlugin, err := host.LoadModule(ctx, plugins.Manifest{ID: "noop", Enabled: true}, buildNoopModule(), time.Second)
	if err != nil {
		t.Fatalf("LoadModule noop failed: %v", err)
	}
	if res, err := noopPlugin.FetchMetadata(ctx, plugins.MediaQuery{Title: "None"}); err != nil || res != nil {
		t.Fatalf("expected (nil, nil) for noop FetchMetadata, got (%+v, %v)", res, err)
	}
	if res, err := noopPlugin.ResolveStream(ctx, "/none.strm"); err != nil || res != nil {
		t.Fatalf("expected (nil, nil) for noop ResolveStream, got (%+v, %v)", res, err)
	}

	// 6. Invalid JSON stdout returns decode error.
	badJSONPlugin, err := host.LoadModule(ctx, plugins.Manifest{ID: "badjson", Enabled: true}, buildWasiOutputModule(1, "{not-json", false, 0), time.Second)
	if err != nil {
		t.Fatalf("LoadModule badjson failed: %v", err)
	}
	if _, err := badJSONPlugin.FetchMetadata(ctx, plugins.MediaQuery{}); err == nil {
		t.Fatal("expected decode error for invalid metadata JSON")
	}
	if _, err := badJSONPlugin.ResolveStream(ctx, "/x"); err == nil {
		t.Fatal("expected decode error for invalid stream JSON")
	}
}

func TestLoadFromDirErrors(t *testing.T) {
	ctx := context.Background()
	host, err := NewHost(ctx)
	if err != nil {
		t.Fatalf("NewHost failed: %v", err)
	}
	defer host.Close(ctx)

	// Path traversal in WasmFile should be skipped (loaded=0) without aborting other plugins.
	dir1 := t.TempDir()
	_ = os.MkdirAll(filepath.Join(dir1, "evil"), 0o755)
	_ = os.WriteFile(filepath.Join(dir1, "evil", "manifest.json"), []byte(`{"Id":"evil","WasmFile":"../escape.wasm"}`), 0o644)
	_ = os.MkdirAll(filepath.Join(dir1, "good"), 0o755)
	_ = os.WriteFile(filepath.Join(dir1, "good", "manifest.json"), []byte(`{"Id":"good","Name":"Good","Enabled":true,"WasmFile":"ok.wasm"}`), 0o644)
	_ = os.WriteFile(filepath.Join(dir1, "good", "ok.wasm"), buildNoopModule(), 0o644)
	if n, err := host.LoadFromDir(ctx, dir1, nil); err != nil || n != 1 {
		t.Fatalf("expected evil skipped and good loaded (n=1, err=nil), got n=%d err=%v", n, err)
	}

	// Malformed manifest.json should be skipped (loaded=0).
	dir2 := t.TempDir()
	_ = os.MkdirAll(filepath.Join(dir2, "broken"), 0o755)
	_ = os.WriteFile(filepath.Join(dir2, "broken", "manifest.json"), []byte(`{invalid`), 0o644)
	if n, err := host.LoadFromDir(ctx, dir2, nil); err != nil || n != 0 {
		t.Fatalf("expected broken manifest skipped (n=0, err=nil), got n=%d err=%v", n, err)
	}

	// Missing wasm file should be skipped (loaded=0).
	dir3 := t.TempDir()
	_ = os.MkdirAll(filepath.Join(dir3, "missing"), 0o755)
	_ = os.WriteFile(filepath.Join(dir3, "missing", "manifest.json"), []byte(`{"Id":"m","WasmFile":"nope.wasm"}`), 0o644)
	if n, err := host.LoadFromDir(ctx, dir3, nil); err != nil || n != 0 {
		t.Fatalf("expected missing wasm file skipped (n=0, err=nil), got n=%d err=%v", n, err)
	}
}

// buildLargeStdoutModule creates a valid WASI WASM module that writes 70 * 65000 bytes (> 4 MiB)
// to stdout so limitedBuffer truncation is triggered, optionally calling proc_exit(0).
func buildLargeStdoutModule(callExit bool) []byte {
	wasm := []byte{0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00}

	var typeSec []byte
	if callExit {
		typeSec = appendULEB128(typeSec, 3)
	} else {
		typeSec = appendULEB128(typeSec, 2)
	}
	typeSec = append(typeSec, 0x60, 4, 0x7f, 0x7f, 0x7f, 0x7f, 1, 0x7f) // type 0: fd_write
	typeSec = append(typeSec, 0x60, 0, 0)                               // type 1: _start
	if callExit {
		typeSec = append(typeSec, 0x60, 1, 0x7f, 0) // type 2: proc_exit
	}
	wasm = appendSection(wasm, 1, typeSec)

	var importSec []byte
	if callExit {
		importSec = appendULEB128(importSec, 2)
	} else {
		importSec = appendULEB128(importSec, 1)
	}
	importSec = appendStr(importSec, "wasi_snapshot_preview1")
	importSec = appendStr(importSec, "fd_write")
	importSec = append(importSec, 0x00, 0x00)
	if callExit {
		importSec = appendStr(importSec, "wasi_snapshot_preview1")
		importSec = appendStr(importSec, "proc_exit")
		importSec = append(importSec, 0x00, 0x02)
	}
	wasm = appendSection(wasm, 2, importSec)

	wasm = appendSection(wasm, 3, []byte{1, 1})
	wasm = appendSection(wasm, 5, []byte{1, 0x00, 1})

	startIdx := byte(1)
	if callExit {
		startIdx = 2
	}
	var exportSec []byte
	exportSec = appendULEB128(exportSec, 2)
	exportSec = appendStr(exportSec, "memory")
	exportSec = append(exportSec, 0x02, 0x00)
	exportSec = appendStr(exportSec, "_start")
	exportSec = append(exportSec, 0x00, startIdx)
	wasm = appendSection(wasm, 7, exportSec)

	var body []byte
	body = append(body, 0x01, 0x01, 0x7f) // 1 local: i32 (idx 0)
	body = append(body, 0x03, 0x40)       // loop
	body = append(body, 0x41, 0x01)       // fd=1 (stdout)
	body = append(body, 0x41, 0x00)       // iovs=0
	body = append(body, 0x41, 0x01)       // iovs_len=1
	body = append(body, 0x41, 0x08)       // nwritten=8
	body = append(body, 0x10, 0x00)       // call fd_write
	body = append(body, 0x1a)             // drop
	body = append(body, 0x20, 0x00)       // local.get 0
	body = append(body, 0x41, 0x01)       // i32.const 1
	body = append(body, 0x6a)             // i32.add
	body = append(body, 0x22, 0x00)       // local.tee 0
	body = append(body, 0x41)
	body = appendSLEB128(body, 70)        // 70 * 65000 = 4.55MB > 4MiB
	body = append(body, 0x49)             // i32.lt_u
	body = append(body, 0x0d, 0x00)       // br_if 0
	body = append(body, 0x0b)             // end loop
	if callExit {
		body = append(body, 0x41, 0x00, 0x10, 0x01) // proc_exit(0)
	}
	body = append(body, 0x0b) // end func

	var codeSec []byte
	codeSec = appendULEB128(codeSec, 1)
	codeSec = appendULEB128(codeSec, uint32(len(body)))
	codeSec = append(codeSec, body...)
	wasm = appendSection(wasm, 10, codeSec)

	// iovec { ptr: 16, len: 65000 }
	iovec := make([]byte, 8)
	binary.LittleEndian.PutUint32(iovec[0:4], 16)
	binary.LittleEndian.PutUint32(iovec[4:8], 65000)
	var dataSec []byte
	dataSec = appendULEB128(dataSec, 1)
	dataSec = append(dataSec, 0x00, 0x41, 0x00, 0x0b)
	dataSec = appendULEB128(dataSec, uint32(len(iovec)))
	dataSec = append(dataSec, iovec...)
	wasm = appendSection(wasm, 11, dataSec)
	return wasm
}

func TestWasmHostLimitsSymlinksTimeoutClampAndFiltering(t *testing.T) {
	// 1. limitedBuffer truncation unit test
	lb := &limitedBuffer{limit: 5}
	n, err := lb.Write([]byte("abc"))
	if err != nil || n != 3 || lb.truncated || lb.String() != "abc" {
		t.Fatalf("unexpected first write: n=%d err=%v trunc=%v s=%q", n, err, lb.truncated, lb.String())
	}
	n, err = lb.Write([]byte("defgh"))
	if err != nil || n != 5 || !lb.truncated || lb.String() != "abcde" {
		t.Fatalf("unexpected partial truncation write: n=%d err=%v trunc=%v s=%q", n, err, lb.truncated, lb.String())
	}
	n, err = lb.Write([]byte("xyz"))
	if err != nil || n != 3 || !lb.truncated || lb.String() != "abcde" {
		t.Fatalf("unexpected overflow write: n=%d err=%v trunc=%v s=%q", n, err, lb.truncated, lb.String())
	}

	ctx := context.Background()
	host, err := NewHost(ctx)
	if err != nil {
		t.Fatalf("NewHost failed: %v", err)
	}
	defer host.Close(ctx)

	// 2. LoadModule size limit and timeout clamping
	oversized := make([]byte, maxWasmFileBytes+1)
	if _, err := host.LoadModule(ctx, plugins.Manifest{ID: "huge"}, oversized, 0); err == nil {
		t.Fatal("expected error for wasm binary exceeding maxWasmFileBytes")
	}
	clampedMod, err := host.LoadModule(ctx, plugins.Manifest{ID: "clamp", Enabled: true, Kinds: []plugins.Kind{plugins.KindMetadata}}, buildNoopModule(), 10*time.Minute)
	if err != nil {
		t.Fatalf("LoadModule clamp failed: %v", err)
	}
	if clampedMod.timeout != maxPluginTimeout {
		t.Fatalf("expected timeout clamped to %v, got %v", maxPluginTimeout, clampedMod.timeout)
	}

	// 3. Enabled / Kinds filtering and Configure ("enabled" and "Enabled")
	// Kinds=[metadata] -> ResolveStream skipped without invoking WASM
	if res, err := clampedMod.ResolveStream(ctx, "/test.strm"); err != nil || res != nil {
		t.Fatalf("expected (nil, nil) for unsupported kind ResolveStream, got (%+v, %v)", res, err)
	}
	// Disable via lowercase "enabled"
	if err := clampedMod.Configure(map[string]any{"enabled": false}); err != nil {
		t.Fatal(err)
	}
	if clampedMod.Manifest().Enabled {
		t.Fatal("expected plugin disabled via lowercase enabled")
	}
	if res, err := clampedMod.FetchMetadata(ctx, plugins.MediaQuery{Title: "Disabled"}); err != nil || res != nil {
		t.Fatalf("expected (nil, nil) when disabled, got (%+v, %v)", res, err)
	}
	// Re-enable via capitalized "Enabled"
	if err := clampedMod.Configure(map[string]any{"Enabled": true}); err != nil {
		t.Fatal(err)
	}
	if !clampedMod.Manifest().Enabled {
		t.Fatal("expected plugin re-enabled via capitalized Enabled")
	}
	// Stream-only plugin -> FetchMetadata returns (nil, nil)
	streamOnly, err := host.LoadModule(ctx, plugins.Manifest{ID: "stream-only", Enabled: true, Kinds: []plugins.Kind{plugins.KindStream}}, buildNoopModule(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if res, err := streamOnly.FetchMetadata(ctx, plugins.MediaQuery{Title: "Skip"}); err != nil || res != nil {
		t.Fatalf("expected (nil, nil) for stream-only FetchMetadata, got (%+v, %v)", res, err)
	}

	// 4. invoke: stdout truncation (normal return and proc_exit(0)), non-zero exit with empty stderr
	truncReturn, err := host.LoadModule(ctx, plugins.Manifest{ID: "trunc-ret", Enabled: true}, buildLargeStdoutModule(false), 5*time.Second)
	if err != nil {
		t.Fatalf("LoadModule trunc-ret failed: %v", err)
	}
	if _, err := truncReturn.FetchMetadata(ctx, plugins.MediaQuery{Title: "Big"}); err == nil || !strings.Contains(err.Error(), "stdout exceeded") {
		t.Fatalf("expected stdout exceeded error on normal return, got %v", err)
	}

	truncExit, err := host.LoadModule(ctx, plugins.Manifest{ID: "trunc-exit", Enabled: true}, buildLargeStdoutModule(true), 5*time.Second)
	if err != nil {
		t.Fatalf("LoadModule trunc-exit failed: %v", err)
	}
	if _, err := truncExit.FetchMetadata(ctx, plugins.MediaQuery{Title: "BigExit"}); err == nil || !strings.Contains(err.Error(), "stdout exceeded") {
		t.Fatalf("expected stdout exceeded error on proc_exit(0), got %v", err)
	}

	silentFail, err := host.LoadModule(ctx, plugins.Manifest{ID: "silent-fail", Enabled: true}, buildWasiOutputModule(2, "", true, 2), time.Second)
	if err != nil {
		t.Fatalf("LoadModule silent-fail failed: %v", err)
	}
	if _, err := silentFail.FetchMetadata(ctx, plugins.MediaQuery{Title: "Fail"}); err == nil || !strings.Contains(err.Error(), "failed:") {
		t.Fatalf("expected generic failure error when stderr is empty, got %v", err)
	}

	// 5. LoadFromDir: dir is regular file, symlink escape, oversized manifest/wasm, invalid wasm, TimeoutMs clamp
	notADir := filepath.Join(t.TempDir(), "file")
	_ = os.WriteFile(notADir, []byte("x"), 0o644)
	if _, err := host.LoadFromDir(ctx, notADir, nil); err == nil {
		t.Fatal("expected error when plugin dir is a file")
	}

	pluginsDir := t.TempDir()
	outsideDir := t.TempDir()
	outsideWasm := filepath.Join(outsideDir, "outside.wasm")
	_ = os.WriteFile(outsideWasm, buildNoopModule(), 0o644)

	// 5a. Symlink escaping plugin dir -> rejected
	symDir := filepath.Join(pluginsDir, "sym-escape")
	_ = os.MkdirAll(symDir, 0o755)
	_ = os.WriteFile(filepath.Join(symDir, "manifest.json"), []byte(`{"Id":"sym","Enabled":true,"WasmFile":"link.wasm"}`), 0o644)
	_ = os.Symlink(outsideWasm, filepath.Join(symDir, "link.wasm"))

	// 5b. Oversized manifest.json -> skipped
	bigManifestDir := filepath.Join(pluginsDir, "big-manifest")
	_ = os.MkdirAll(bigManifestDir, 0o755)
	if mf, err := os.Create(filepath.Join(bigManifestDir, "manifest.json")); err == nil {
		_ = mf.Truncate(maxManifestBytes + 1)
		_ = mf.Close()
	}

	// 5c. Oversized wasm file -> skipped
	bigWasmDir := filepath.Join(pluginsDir, "big-wasm")
	_ = os.MkdirAll(bigWasmDir, 0o755)
	_ = os.WriteFile(filepath.Join(bigWasmDir, "manifest.json"), []byte(`{"Id":"bw","Enabled":true,"WasmFile":"huge.wasm"}`), 0o644)
	if wf, err := os.Create(filepath.Join(bigWasmDir, "huge.wasm")); err == nil {
		_ = wf.Truncate(maxWasmFileBytes + 1)
		_ = wf.Close()
	}

	// 5d. Invalid wasm binary -> compile error skipped
	badWasmDir := filepath.Join(pluginsDir, "bad-wasm")
	_ = os.MkdirAll(badWasmDir, 0o755)
	_ = os.WriteFile(filepath.Join(badWasmDir, "manifest.json"), []byte(`{"Id":"bad","Enabled":true,"WasmFile":"bad.wasm"}`), 0o644)
	_ = os.WriteFile(filepath.Join(badWasmDir, "bad.wasm"), []byte("corrupt-bytes"), 0o644)

	// 5e. Valid plugin with huge TimeoutMs -> clamped to maxPluginTimeout and loaded
	clampedDir := filepath.Join(pluginsDir, "clamped-timeout")
	_ = os.MkdirAll(clampedDir, 0o755)
	_ = os.WriteFile(filepath.Join(clampedDir, "manifest.json"), []byte(`{"Id":"clamped-ok","Name":"Clamped","Enabled":true,"WasmFile":"ok.wasm","TimeoutMs":999999}`), 0o644)
	_ = os.WriteFile(filepath.Join(clampedDir, "ok.wasm"), buildNoopModule(), 0o644)

	reg := plugins.NewRegistry()
	nLoaded, err := host.LoadFromDir(ctx, pluginsDir, reg)
	if err != nil {
		t.Fatalf("LoadFromDir failed: %v", err)
	}
	if nLoaded != 1 {
		t.Fatalf("expected only clamped-ok loaded (1), got %d", nLoaded)
	}
	provs := reg.MetadataProviders()
	if len(provs) != 1 || provs[0].Manifest().ID != "clamped-ok" {
		t.Fatalf("unexpected loaded providers: %+v", provs)
	}
	if wp, ok := provs[0].(*WasmPlugin); !ok || wp.timeout != maxPluginTimeout {
		t.Fatalf("expected TimeoutMs clamped to %v, got %+v", maxPluginTimeout, provs[0])
	}
}

func TestWasmHostAuditV0104Security(t *testing.T) {
	ctx := context.Background()
	host, err := NewHost(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer host.Close(ctx)

	// 1. limitedBuffer invokes cancel immediately on overflow
	cancelled := false
	lb := &limitedBuffer{limit: 4, cancel: func() { cancelled = true }}
	_, _ = lb.Write([]byte("123456"))
	if !cancelled || !lb.isTruncated() {
		t.Fatal("expected limitedBuffer to call cancel on overflow")
	}

	// 2. BuiltIn forced false and ambra.builtin.* prefix rejected
	if _, err := host.LoadModule(ctx, plugins.Manifest{ID: "ambra.builtin.fake", BuiltIn: true, Enabled: true}, buildNoopModule(), time.Second); err == nil {
		t.Fatal("expected ambra.builtin.* wasm plugin to be rejected")
	}
	extMod, err := host.LoadModule(ctx, plugins.Manifest{ID: "ext.ok", BuiltIn: true, Enabled: true}, buildNoopModule(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if extMod.Manifest().BuiltIn {
		t.Fatal("expected BuiltIn forced to false for WASM plugin")
	}

	// 3. ResolveStream SSRF targets blocked (file:// and cloud metadata)
	for _, badURL := range []string{"file:///etc/passwd", "http://169.254.169.254/latest/meta-data"} {
		ssrfMod, err := host.LoadModule(ctx, plugins.Manifest{ID: "ssrf", Enabled: true, Kinds: []plugins.Kind{plugins.KindStream}},
			buildWasiOutputModule(1, `{"URL":"`+badURL+`","Is302":true}`, false, 0), time.Second)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := ssrfMod.ResolveStream(ctx, "/x.strm"); err == nil {
			t.Fatalf("expected SSRF URL %q to be blocked", badURL)
		}
	}

	// 4. Concurrency semaphore respects cancelled context when full
	for i := 0; i < cap(host.sem); i++ {
		host.sem <- struct{}{}
	}
	cctx, ccancel := context.WithTimeout(ctx, 20*time.Millisecond)
	defer ccancel()
	if _, err := extMod.FetchMetadata(cctx, plugins.MediaQuery{Title: "Wait"}); err == nil {
		t.Fatal("expected semaphore wait to fail on context deadline")
	}
	for i := 0; i < cap(host.sem); i++ {
		<-host.sem
	}
}

// buildAmbraHTTPAndLogModule constructs a WebAssembly 1.0 binary that:
// 1) calls ambra.log(1, logPtr, len(logMsg))
// 2) calls ambra.http_request(reqPtr, len(reqJSON), respPtr, respCap)
//    - if ret > 0: writes the returned JSON slice [respPtr..respPtr+ret] to stdout
//    - if ret <= 0: calls wasi proc_exit(uint32(-ret)) so tests can assert exact negative status/length codes.
func buildAmbraHTTPAndLogModule(logMsg, reqJSON string, respCap uint32) []byte {
	wasm := []byte{0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00}

	// Type section:
	// 0: (i32,i32,i32,i32)->(i32) [fd_write]
	// 1: (i32)->()                [proc_exit]
	// 2: (i32,i32,i32)->()        [ambra.log]
	// 3: (i32,i32,i32,i32)->(i32) [ambra.http_request]
	// 4: ()->()                   [_start]
	var typeSec []byte
	typeSec = appendULEB128(typeSec, 5)
	typeSec = append(typeSec, 0x60, 4, 0x7f, 0x7f, 0x7f, 0x7f, 1, 0x7f)
	typeSec = append(typeSec, 0x60, 1, 0x7f, 0)
	typeSec = append(typeSec, 0x60, 3, 0x7f, 0x7f, 0x7f, 0)
	typeSec = append(typeSec, 0x60, 4, 0x7f, 0x7f, 0x7f, 0x7f, 1, 0x7f)
	typeSec = append(typeSec, 0x60, 0, 0)
	wasm = appendSection(wasm, 1, typeSec)

	// Import section:
	var importSec []byte
	importSec = appendULEB128(importSec, 4)
	importSec = appendStr(importSec, "wasi_snapshot_preview1")
	importSec = appendStr(importSec, "fd_write")
	importSec = append(importSec, 0x00, 0x00)
	importSec = appendStr(importSec, "wasi_snapshot_preview1")
	importSec = appendStr(importSec, "proc_exit")
	importSec = append(importSec, 0x00, 0x01)
	importSec = appendStr(importSec, "ambra")
	importSec = appendStr(importSec, "log")
	importSec = append(importSec, 0x00, 0x02)
	importSec = appendStr(importSec, "ambra")
	importSec = appendStr(importSec, "http_request")
	importSec = append(importSec, 0x00, 0x03)
	wasm = appendSection(wasm, 2, importSec)

	// Function section: _start (func idx 4) has type 4
	wasm = appendSection(wasm, 3, []byte{1, 4})

	// Memory section: 1 memory, min 1 page (64 KiB)
	wasm = appendSection(wasm, 5, []byte{1, 0x00, 1})

	// Export section: "memory" and "_start"
	var exportSec []byte
	exportSec = appendULEB128(exportSec, 2)
	exportSec = appendStr(exportSec, "memory")
	exportSec = append(exportSec, 0x02, 0x00)
	exportSec = appendStr(exportSec, "_start")
	exportSec = append(exportSec, 0x00, 0x04)
	wasm = appendSection(wasm, 7, exportSec)

	const (
		logPtr  = 32
		respPtr = 2048
	)
	reqPtr := logPtr + len(logMsg)

	var body []byte
	body = append(body, 0x01, 0x01, 0x7f) // 1 local: i32 (local 0)

	// ambra.log(1, logPtr, len(logMsg))
	body = append(body, 0x41)
	body = appendSLEB128(body, 1)
	body = append(body, 0x41)
	body = appendSLEB128(body, int32(logPtr))
	body = append(body, 0x41)
	body = appendSLEB128(body, int32(len(logMsg)))
	body = append(body, 0x10, 0x02)

	// local 0 = ambra.http_request(reqPtr, len(reqJSON), respPtr, respCap)
	body = append(body, 0x41)
	body = appendSLEB128(body, int32(reqPtr))
	body = append(body, 0x41)
	body = appendSLEB128(body, int32(len(reqJSON)))
	body = append(body, 0x41)
	body = appendSLEB128(body, int32(respPtr))
	body = append(body, 0x41)
	body = appendSLEB128(body, int32(respCap))
	body = append(body, 0x10, 0x03)
	body = append(body, 0x21, 0x00)

	// if (local 0 > 0)
	body = append(body, 0x20, 0x00, 0x41, 0x00, 0x4a, 0x04, 0x40)
	// iovec.ptr (addr 0) = respPtr
	body = append(body, 0x41, 0x00, 0x41)
	body = appendSLEB128(body, int32(respPtr))
	body = append(body, 0x36, 0x02, 0x00)
	// iovec.len (addr 4) = local 0
	body = append(body, 0x41, 0x04, 0x20, 0x00, 0x36, 0x02, 0x00)
	// fd_write(1, 0, 1, 8)
	body = append(body, 0x41, 0x01, 0x41, 0x00, 0x41, 0x01, 0x41, 0x08, 0x10, 0x00, 0x1a)
	// else: proc_exit(0 - local 0)
	body = append(body, 0x05, 0x41, 0x00, 0x20, 0x00, 0x6b, 0x10, 0x01)
	// end if + end func
	body = append(body, 0x0b, 0x0b)

	var codeSec []byte
	codeSec = appendULEB128(codeSec, 1)
	codeSec = appendULEB128(codeSec, uint32(len(body)))
	codeSec = append(codeSec, body...)
	wasm = appendSection(wasm, 10, codeSec)

	// Data section at offset 32: logMsg + reqJSON
	initData := append([]byte(logMsg), []byte(reqJSON)...)
	var dataSec []byte
	dataSec = appendULEB128(dataSec, 1)
	dataSec = append(dataSec, 0x00, 0x41)
	dataSec = appendSLEB128(dataSec, int32(logPtr))
	dataSec = append(dataSec, 0x0b)
	dataSec = appendULEB128(dataSec, uint32(len(initData)))
	dataSec = append(dataSec, initData...)
	wasm = appendSection(wasm, 11, dataSec)

	return wasm
}

func TestAmbraHostLogHTTPRequestSSRFAndSubtitleProvider(t *testing.T) {
	ctx := context.Background()

	// 1. Default strict Host: ambra.http_request MUST block 127.0.0.1 loopback SSRF AND non-allowlisted domains!
	strictHost, err := NewHost(ctx)
	if err != nil {
		t.Fatalf("NewHost strict: %v", err)
	}
	defer strictHost.Close(ctx)

	var logMu sync.Mutex
	var logs []string
	strictHost.mu.Lock()
	strictHost.logHook = func(pluginID string, level uint32, msg string) {
		logMu.Lock()
		logs = append(logs, pluginID+":"+msg)
		logMu.Unlock()
	}
	strictHost.mu.Unlock()

	localTarget := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Plugin-Test", "ok")
		_, _ = w.Write([]byte(`{"Name":"RemoteWasmMovie","Year":2026}`))
	}))
	defer localTarget.Close()

	// 1a. Loopback 127.0.0.1 blocked by NewSafeHTTPClientNoLoopback / ValidateRemoteURLNoLoopback even when host is in AllowedHosts
	uLocal, _ := url.Parse(localTarget.URL)
	loopbackReq, _ := json.Marshal(hostHTTPRequestPayload{Method: "GET", URL: localTarget.URL})
	loopbackWasm := buildAmbraHTTPAndLogModule("attempting loopback", string(loopbackReq), 4096)
	loopbackPlugin, err := strictHost.LoadModuleWithAllowedHosts(
		ctx,
		plugins.Manifest{ID: "ext.wasm.ssrf", Name: "SSRF Test", Enabled: true, Kinds: []plugins.Kind{plugins.KindMetadata}},
		[]string{uLocal.Hostname()},
		loopbackWasm,
		2*time.Second,
	)
	if err != nil {
		t.Fatalf("LoadModule loopback: %v", err)
	}
	_, err = loopbackPlugin.FetchMetadata(ctx, plugins.MediaQuery{Title: "SSRF"})
	if err == nil {
		t.Fatal("expected ambra.http_request to block 127.0.0.1 loopback SSRF")
	}
	var exitErr *sys.ExitError
	if !errors.As(err, &exitErr) || int32(exitErr.ExitCode()) != -HostHTTPErrForbidden {
		t.Fatalf("expected exit code %d (Forbidden) for loopback SSRF, got err=%v", -HostHTTPErrForbidden, err)
	}

	logMu.Lock()
	gotLogCount := len(logs)
	logMu.Unlock()
	if gotLogCount != 1 || logs[0] != "ext.wasm.ssrf:attempting loopback" {
		t.Fatalf("expected ambra.log captured once, got %v", logs)
	}

	// 1b. Non-allowlisted host blocked immediately by AllowedHosts check
	disallowedReq, _ := json.Marshal(hostHTTPRequestPayload{Method: "GET", URL: "https://evil.example.org/steal"})
	disallowedWasm := buildAmbraHTTPAndLogModule("disallowed host", string(disallowedReq), 4096)
	disallowedPlugin, err := strictHost.LoadModuleWithAllowedHosts(
		ctx,
		plugins.Manifest{ID: "ext.wasm.allowlist", Name: "Allowlist Test", Enabled: true, Kinds: []plugins.Kind{plugins.KindMetadata}},
		[]string{"api.trusted.example.com", "*.sub.example.com"},
		disallowedWasm,
		2*time.Second,
	)
	if err != nil {
		t.Fatal(err)
	}
	_, err = disallowedPlugin.FetchMetadata(ctx, plugins.MediaQuery{Title: "BlockedHost"})
	if !errors.As(err, &exitErr) || int32(exitErr.ExitCode()) != -HostHTTPErrForbidden {
		t.Fatalf("expected exit code %d (Forbidden) for non-allowlisted domain, got %v", -HostHTTPErrForbidden, err)
	}

	// 2. Normal allowed HTTP round-trip + buffer-too-small negative required length check
	roundTripHost, err := NewHost(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer roundTripHost.Close(ctx)

	// Route allowed.example.com to localTarget via custom test RoundTripper while keeping AllowedHosts enforcement active
	roundTripHost.SetHTTPClient(&http.Client{
		Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			redirected, _ := http.NewRequestWithContext(req.Context(), req.Method, localTarget.URL+req.URL.Path, req.Body)
			redirected.Header = req.Header.Clone()
			return localTarget.Client().Transport.RoundTrip(redirected)
		}),
	})

	allowedReqBytes, _ := json.Marshal(hostHTTPRequestPayload{
		Method:  "POST",
		URL:     "https://sub.allowed.example.com/scrape",
		Headers: map[string]string{"X-Token": "abc"},
		Body:    `{"q":"dune"}`,
	})

	// 2a. Buffer too small (respCap = 8) -> returns negative required byte length (<= -12)
	tinyBufWasm := buildAmbraHTTPAndLogModule("tiny buf", string(allowedReqBytes), 8)
	tinyBufPlugin, err := roundTripHost.LoadModuleWithAllowedHosts(
		ctx,
		plugins.Manifest{ID: "ext.wasm.tinybuf", Name: "TinyBuf", Enabled: true},
		[]string{"*.allowed.example.com"},
		tinyBufWasm,
		2*time.Second,
	)
	if err != nil {
		t.Fatal(err)
	}
	_, err = tinyBufPlugin.invoke(ctx, Request{Action: "fetch_metadata"})
	if !errors.As(err, &exitErr) || exitErr.ExitCode() < 40 {
		t.Fatalf("expected positive exitCode == requiredJSONLen (>=40) when respCap=8, got err=%v", err)
	}

	// 2b. Sufficient buffer (respCap = 4096) -> full JSON response written into WASM linear memory!
	okWasm := buildAmbraHTTPAndLogModule("ok request", string(allowedReqBytes), 4096)
	okPlugin, err := roundTripHost.LoadModuleWithAllowedHosts(
		ctx,
		plugins.Manifest{ID: "ext.wasm.httpok", Name: "HTTPOK", Enabled: true, Priority: 15},
		[]string{"*.allowed.example.com"},
		okWasm,
		2*time.Second,
	)
	if err != nil {
		t.Fatal(err)
	}
	rawStdout, err := okPlugin.invoke(ctx, Request{Action: "fetch_metadata"})
	if err != nil {
		t.Fatalf("expected successful ambra.http_request round-trip, got err=%v", err)
	}
	var hostResp hostHTTPResponsePayload
	if err := json.Unmarshal(rawStdout, &hostResp); err != nil {
		t.Fatalf("unmarshal hostHTTPResponsePayload: %v (raw=%s)", err, string(rawStdout))
	}
	if hostResp.Status != http.StatusOK || hostResp.Headers["X-Plugin-Test"] != "ok" || !strings.Contains(hostResp.Body, "RemoteWasmMovie") {
		t.Fatalf("unexpected ambra.http_request response: %+v", hostResp)
	}

	// 3. WasmPlugin SubtitleProvider (SearchSubtitles & DownloadSubtitle) + dynamic Priority & DefaultConfig
	subSearchJSON := `[{"Id":"sub-1","Name":"Dune.zh.srt","ProviderName":"WasmSub","Language":"zho","Format":"srt"}]`
	subSearchPlugin, err := strictHost.LoadModule(
		ctx,
		plugins.Manifest{ID: "ext.wasm.subsearch", Name: "WasmSub", Enabled: true, Priority: 30, Kinds: []plugins.Kind{plugins.KindSubtitle}},
		buildWasiOutputModule(1, subSearchJSON, false, 0),
		time.Second,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := subSearchPlugin.Configure(map[string]any{"priority": float64(12), "enabled": true, "token": "xyz"}); err != nil {
		t.Fatal(err)
	}
	if subSearchPlugin.Manifest().Priority != 12 {
		t.Fatalf("expected updated Priority=12, got %d", subSearchPlugin.Manifest().Priority)
	}
	if def := subSearchPlugin.DefaultConfig(); def["priority"] != 12 || def["token"] != "xyz" {
		t.Fatalf("unexpected DefaultConfig: %+v", def)
	}
	hits, err := subSearchPlugin.SearchSubtitles(ctx, plugins.SubtitleSearchQuery{Title: "Dune", Language: "zh"})
	if err != nil || len(hits) != 1 || hits[0].ID != "sub-1" {
		t.Fatalf("SearchSubtitles got (%+v, %v)", hits, err)
	}

	subDlJSON := `{"language":"zh-CN","format":"srt","content":"1\n00:00:01,000 --> 00:00:02,000\n你好\n"}`
	subDlPlugin, err := strictHost.LoadModule(
		ctx,
		plugins.Manifest{ID: "ext.wasm.subdl", Name: "WasmSubDL", Enabled: true, Kinds: []plugins.Kind{plugins.KindSubtitle}},
		buildWasiOutputModule(1, subDlJSON, false, 0),
		time.Second,
	)
	if err != nil {
		t.Fatal(err)
	}
	dlRes, err := subDlPlugin.DownloadSubtitle(ctx, "sub-1")
	if err != nil || dlRes == nil || dlRes.Format != "srt" || !strings.Contains(string(dlRes.Content), "你好") {
		t.Fatalf("DownloadSubtitle got (%+v, %v)", dlRes, err)
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}
