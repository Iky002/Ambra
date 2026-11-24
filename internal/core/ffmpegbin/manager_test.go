package ffmpegbin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestEmbedStubDefaults(t *testing.T) {
	if HasEmbedded() {
		t.Fatal("expected HasEmbedded() to be false without embed_ffmpeg build tag")
	}
	if EmbeddedFFmpeg != nil || EmbeddedFFprobe != nil {
		t.Fatal("expected nil EmbeddedFFmpeg and EmbeddedFFprobe by default")
	}
}

func TestExtractEmbedded(t *testing.T) {
	// Empty payload -> no-op
	ff, fp, err := ExtractEmbedded("", nil, nil)
	if err != nil || ff != "" || fp != "" {
		t.Fatalf("expected no-op for empty bytes, got %q %q %v", ff, fp, err)
	}

	// Empty targetDir with non-empty payload -> error
	if _, _, err := ExtractEmbedded("  ", []byte("abc"), nil); err == nil {
		t.Fatal("expected error when targetDir is empty")
	}

	// Regular file as targetDir -> MkdirAll error
	notDir := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(notDir, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ExtractEmbedded(notDir, []byte("ffmpeg-bin"), nil); err == nil {
		t.Fatal("expected error when targetDir is a regular file")
	}

	// Normal extraction
	targetDir := filepath.Join(t.TempDir(), "bin")
	ffmpegData := []byte("#!/bin/sh\necho ffmpeg\n")
	ffprobeData := []byte("#!/bin/sh\necho ffprobe\n")
	ff, fp, err = ExtractEmbedded(targetDir, ffmpegData, ffprobeData)
	if err != nil {
		t.Fatalf("ExtractEmbedded failed: %v", err)
	}
	if ff != filepath.Join(targetDir, binaryName("ffmpeg", runtime.GOOS)) {
		t.Fatalf("unexpected ffmpeg path: %q", ff)
	}
	if fp != filepath.Join(targetDir, binaryName("ffprobe", runtime.GOOS)) {
		t.Fatalf("unexpected ffprobe path: %q", fp)
	}

	info, err := os.Stat(ff)
	if err != nil {
		t.Fatalf("stat ffmpeg failed: %v", err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o111 == 0 {
		t.Fatalf("expected executable permission, got %o", info.Mode().Perm())
	}

	// Set older mtime and re-extract with identical size -> must skip rewrite
	oldTime := time.Now().Add(-2 * time.Hour).Truncate(time.Second)
	if err := os.Chtimes(ff, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}
	ff2, fp2, err := ExtractEmbedded(targetDir, ffmpegData, ffprobeData)
	if err != nil || ff2 != ff || fp2 != fp {
		t.Fatalf("second ExtractEmbedded failed: %q %q %v", ff2, fp2, err)
	}
	info2, err := os.Stat(ff2)
	if err != nil {
		t.Fatal(err)
	}
	if !info2.ModTime().Equal(oldTime) {
		t.Fatalf("expected identical-size file to skip rewrite, mtime changed from %v to %v", oldTime, info2.ModTime())
	}

	// Different size -> overwrites file
	newData := []byte("#!/bin/sh\necho ffmpeg-v2-longer\n")
	ff3, _, err := ExtractEmbedded(targetDir, newData, nil)
	if err != nil {
		t.Fatalf("ExtractEmbedded overwrite failed: %v", err)
	}
	gotBytes, err := os.ReadFile(ff3)
	if err != nil || string(gotBytes) != string(newData) {
		t.Fatalf("expected updated content, got %q (err=%v)", string(gotBytes), err)
	}

	// Windows naming check
	winDir := t.TempDir()
	winFF, winFP, err := extractEmbeddedForOS(winDir, []byte("win-ff"), []byte("win-fp"), "windows")
	if err != nil {
		t.Fatalf("windows extract failed: %v", err)
	}
	if filepath.Base(winFF) != "ffmpeg.exe" || filepath.Base(winFP) != "ffprobe.exe" {
		t.Fatalf("expected .exe names on windows, got %q / %q", winFF, winFP)
	}

	// Error when destination is a directory (rename fails)
	dirConflict := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dirConflict, binaryName("ffmpeg", runtime.GOOS)), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ExtractEmbedded(dirConflict, []byte("bin"), nil); err == nil {
		t.Fatal("expected error when destination path is a directory")
	}
	if err := os.MkdirAll(filepath.Join(dirConflict, binaryName("ffprobe", runtime.GOOS)), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ExtractEmbedded(dirConflict, nil, []byte("bin")); err == nil {
		t.Fatal("expected error when ffprobe destination path is a directory")
	}
}

func TestResolvePriorityOrder(t *testing.T) {
	origExec := executablePathFn
	t.Cleanup(func() { executablePathFn = origExec })

	t.Setenv("AMBRA_FFMPEG_PATH", "")
	t.Setenv("AMBRA_FFPROBE_PATH", "")

	exeDir := t.TempDir()
	dataDir := t.TempDir()
	fakeExe := filepath.Join(exeDir, "ambra")
	executablePathFn = func() (string, error) { return fakeExe, nil }

	// Priority 6: Fallback to PATH ("ffmpeg" / "ffprobe")
	ff, fp := Resolve(ResolveOptions{
		DataDir:       dataDir,
		ConfigFFmpeg:  "ffmpeg",
		ConfigFFprobe: "ffprobe",
		GOOS:          "linux",
	})
	if ff != "ffmpeg" || fp != "ffprobe" {
		t.Fatalf("expected PATH fallback, got %q / %q", ff, fp)
	}

	// Priority 5: <DataDir>/bin/ffmpeg & ffprobe
	dataBin := filepath.Join(dataDir, "bin")
	if err := os.MkdirAll(dataBin, 0o755); err != nil {
		t.Fatal(err)
	}
	dataFF := filepath.Join(dataBin, "ffmpeg")
	dataFP := filepath.Join(dataBin, "ffprobe")
	if err := os.WriteFile(dataFF, []byte("data-ff"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dataFP, []byte("data-fp"), 0o755); err != nil {
		t.Fatal(err)
	}
	ff, fp = Resolve(ResolveOptions{DataDir: dataDir, GOOS: "linux"})
	if ff != dataFF || fp != dataFP {
		t.Fatalf("expected DataDir/bin binaries %q / %q, got %q / %q", dataFF, dataFP, ff, fp)
	}

	// Priority 4b: Executable sibling ./bin/ffmpeg overrides <DataDir>/bin/
	exeSubBin := filepath.Join(exeDir, "bin")
	if err := os.MkdirAll(exeSubBin, 0o755); err != nil {
		t.Fatal(err)
	}
	exeSubFF := filepath.Join(exeSubBin, "ffmpeg")
	exeSubFP := filepath.Join(exeSubBin, "ffprobe")
	if err := os.WriteFile(exeSubFF, []byte("sub-ff"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(exeSubFP, []byte("sub-fp"), 0o755); err != nil {
		t.Fatal(err)
	}
	ff, fp = Resolve(ResolveOptions{DataDir: dataDir, GOOS: "linux"})
	if ff != exeSubFF || fp != exeSubFP {
		t.Fatalf("expected ./bin sibling %q / %q, got %q / %q", exeSubFF, exeSubFP, ff, fp)
	}

	// Priority 4a: Executable sibling ./ffmpeg overrides ./bin/ffmpeg
	exeFF := filepath.Join(exeDir, "ffmpeg")
	exeFP := filepath.Join(exeDir, "ffprobe")
	if err := os.WriteFile(exeFF, []byte("exe-ff"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(exeFP, []byte("exe-fp"), 0o755); err != nil {
		t.Fatal(err)
	}
	ff, fp = Resolve(ResolveOptions{DataDir: dataDir, GOOS: "linux"})
	if ff != exeFF || fp != exeFP {
		t.Fatalf("expected sibling %q / %q, got %q / %q", exeFF, exeFP, ff, fp)
	}

	// Priority 3: Embedded bytes extracted to <DataDir>/bin/ override sibling binaries
	embedDataDir := t.TempDir()
	ff, fp = Resolve(ResolveOptions{
		DataDir:         embedDataDir,
		GOOS:            "linux",
		EmbeddedFFmpeg:  []byte("embedded-ffmpeg-bytes"),
		EmbeddedFFprobe: []byte("embedded-ffprobe-bytes"),
	})
	wantEmbFF := filepath.Join(embedDataDir, "bin", "ffmpeg")
	wantEmbFP := filepath.Join(embedDataDir, "bin", "ffprobe")
	if ff != wantEmbFF || fp != wantEmbFP {
		t.Fatalf("expected embedded extracted %q / %q, got %q / %q", wantEmbFF, wantEmbFP, ff, fp)
	}

	// Priority 2: Custom non-default config path overrides embedded & sibling
	ff, fp = Resolve(ResolveOptions{
		DataDir:         embedDataDir,
		ConfigFFmpeg:    "/opt/custom/ffmpeg",
		ConfigFFprobe:   "/opt/custom/ffprobe",
		GOOS:            "linux",
		EmbeddedFFmpeg:  []byte("embedded-ffmpeg-bytes"),
		EmbeddedFFprobe: []byte("embedded-ffprobe-bytes"),
	})
	if ff != "/opt/custom/ffmpeg" || fp != "/opt/custom/ffprobe" {
		t.Fatalf("expected custom config paths, got %q / %q", ff, fp)
	}

	// Priority 1: Explicit env var overrides everything (via opts and via os.Getenv)
	ff, fp = Resolve(ResolveOptions{
		DataDir:        embedDataDir,
		ConfigFFmpeg:   "/opt/custom/ffmpeg",
		ConfigFFprobe:  "/opt/custom/ffprobe",
		EnvFFmpeg:      "/env/opts/ffmpeg",
		EnvFFprobe:     "/env/opts/ffprobe",
		EmbeddedFFmpeg: []byte("embedded"),
	})
	if ff != "/env/opts/ffmpeg" || fp != "/env/opts/ffprobe" {
		t.Fatalf("expected EnvFFmpeg/EnvFFprobe opts, got %q / %q", ff, fp)
	}

	t.Setenv("AMBRA_FFMPEG_PATH", "/env/os/ffmpeg")
	t.Setenv("AMBRA_FFPROBE_PATH", "/env/os/ffprobe")
	ff, fp = Resolve(ResolveOptions{
		DataDir:       embedDataDir,
		ConfigFFmpeg:  "/opt/custom/ffmpeg",
		ConfigFFprobe: "/opt/custom/ffprobe",
	})
	if ff != "/env/os/ffmpeg" || fp != "/env/os/ffprobe" {
		t.Fatalf("expected OS env overrides, got %q / %q", ff, fp)
	}
}

func TestResolveWindowsAndDirectories(t *testing.T) {
	t.Setenv("AMBRA_FFMPEG_PATH", "")
	t.Setenv("AMBRA_FFPROBE_PATH", "")

	winExeDir := t.TempDir()
	winFF := filepath.Join(winExeDir, "ffmpeg.exe")
	if err := os.WriteFile(winFF, []byte("exe"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Directory named ffprobe.exe must be ignored
	if err := os.MkdirAll(filepath.Join(winExeDir, "ffprobe.exe"), 0o755); err != nil {
		t.Fatal(err)
	}

	ff, fp := Resolve(ResolveOptions{
		ExecutablePath: filepath.Join(winExeDir, "ambra.exe"),
		ConfigFFmpeg:   "ffmpeg.exe",
		ConfigFFprobe:  "ffprobe.exe",
		GOOS:           "windows",
	})
	if ff != winFF {
		t.Fatalf("expected windows sibling %q, got %q", winFF, ff)
	}
	if fp != "ffprobe" {
		t.Fatalf("expected directory ffprobe.exe to be ignored and fallback to ffprobe, got %q", fp)
	}
}

func TestDownloadStatic(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/releases/ffmpeg":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("#!/bin/sh\necho static-ffmpeg\n"))
		case "/":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("root-binary"))
		case "/empty":
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	origClient := downloadClient
	downloadClient = srv.Client()
	t.Cleanup(func() { downloadClient = origClient })

	// Valid download with filename from URL path
	targetDir := filepath.Join(t.TempDir(), "bin")
	dest, err := DownloadStatic(context.Background(), targetDir, srv.URL+"/releases/ffmpeg")
	if err != nil {
		t.Fatalf("DownloadStatic failed: %v", err)
	}
	if dest != filepath.Join(targetDir, "ffmpeg") {
		t.Fatalf("unexpected dest path: %q", dest)
	}
	info, err := os.Stat(dest)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o111 == 0 {
		t.Fatalf("expected executable permissions, got %o", info.Mode().Perm())
	}

	// Root URL path falls back to default binary name, nil ctx is handled
	destRoot, err := DownloadStatic(nil, t.TempDir(), srv.URL+"/")
	if err != nil {
		t.Fatalf("DownloadStatic root path failed: %v", err)
	}
	if filepath.Base(destRoot) != binaryName("ffmpeg", runtime.GOOS) {
		t.Fatalf("expected default binary name for root URL, got %q", destRoot)
	}

	// HTTP 404 error
	if _, err := DownloadStatic(context.Background(), targetDir, srv.URL+"/missing"); err == nil {
		t.Fatal("expected error for HTTP 404")
	}

	// Empty body error
	if _, err := DownloadStatic(context.Background(), targetDir, srv.URL+"/empty"); err == nil {
		t.Fatal("expected error for empty downloaded body")
	}

	// Invalid URL / empty targetDir
	if _, err := DownloadStatic(context.Background(), "", srv.URL+"/releases/ffmpeg"); err == nil {
		t.Fatal("expected error for empty targetDir")
	}
	if _, err := DownloadStatic(context.Background(), targetDir, "ftp://example.com/ffmpeg"); err == nil {
		t.Fatal("expected error for unsupported scheme")
	}
	if _, err := DownloadStatic(context.Background(), targetDir, "not a url"); err == nil {
		t.Fatal("expected error for invalid URL")
	}

	// Cancelled context
	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := DownloadStatic(canceledCtx, targetDir, srv.URL+"/releases/ffmpeg"); err == nil {
		t.Fatal("expected error for cancelled context")
	}

	// Target dir is a regular file -> MkdirAll fails
	fileAsDir := filepath.Join(t.TempDir(), "notadir")
	if err := os.WriteFile(fileAsDir, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := DownloadStatic(context.Background(), fileAsDir, srv.URL+"/releases/ffmpeg"); err == nil {
		t.Fatal("expected error when targetDir is a file")
	}
}
