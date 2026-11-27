package strm

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestResolverManifestAndConfigure(t *testing.T) {
	r := New(true)
	m := r.Manifest()
	if m.ID != "ambra.builtin.strm" || !m.Enabled || !m.BuiltIn {
		t.Fatalf("unexpected manifest: %+v", m)
	}
	if err := r.Configure(map[string]any{"enabled": false, "enable_302": false}); err != nil {
		t.Fatalf("Configure failed: %v", err)
	}
	if r.Manifest().Enabled || r.enable302.Load() {
		t.Fatalf("expected enabled=false and enable_302=false")
	}
}

func TestResolveSTRMExtraBranches(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	r := New(true)

	// Missing file returns error
	if _, err := r.ResolveStream(ctx, filepath.Join(dir, "missing.strm")); err == nil {
		t.Fatalf("expected error for missing strm file")
	}

	// Empty / comments-only strm returns nil, nil
	emptyPath := filepath.Join(dir, "empty.strm")
	_ = os.WriteFile(emptyPath, []byte("# only comment\n   \n"), 0o644)
	if res, err := r.ResolveStream(ctx, emptyPath); err != nil || res != nil {
		t.Fatalf("expected nil,nil for empty strm, got res=%+v err=%v", res, err)
	}

	// BOM + http URL
	httpPath := filepath.Join(dir, "http.strm")
	_ = os.WriteFile(httpPath, []byte("\xef\xbb\xbfhttp://192.168.1.20/a.mp4\n"), 0o644)
	res, err := r.ResolveStream(ctx, httpPath)
	if err != nil || res == nil || res.URL != "http://192.168.1.20/a.mp4" || !res.Is302 {
		t.Fatalf("unexpected http result: res=%+v err=%v", res, err)
	}

	// Local file path, file://, concat:, and cloud metadata URLs must be rejected
	for _, unsafeTarget := range []string{
		"/mnt/media/movie.mkv",
		"file:///etc/passwd",
		"concat:/etc/passwd",
		"http://169.254.169.254/latest/meta-data/",
	} {
		badPath := filepath.Join(dir, "unsafe.strm")
		_ = os.WriteFile(badPath, []byte(unsafeTarget+"\n"), 0o644)
		if res, err := r.ResolveStream(ctx, badPath); err == nil || res != nil {
			t.Fatalf("expected error for unsafe strm target %q, got res=%+v err=%v", unsafeTarget, res, err)
		}
	}

	// Directory named .strm must be rejected
	dirStrm := filepath.Join(dir, "folder.strm")
	if err := os.MkdirAll(dirStrm, 0o755); err != nil {
		t.Fatal(err)
	}
	if res, err := r.ResolveStream(ctx, dirStrm); err == nil || res != nil {
		t.Fatalf("expected error for directory .strm path, got res=%+v err=%v", res, err)
	}

	// Symlink inside parent tree is allowed; symlink escaping parent directory is rejected
	subDir := filepath.Join(dir, "nested")
	outsideDir := t.TempDir()
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatal(err)
	}
	inTreeTarget := filepath.Join(subDir, "real.strm")
	_ = os.WriteFile(inTreeTarget, []byte("https://93.184.216.34/ok.mp4\n"), 0o644)
	inTreeLink := filepath.Join(dir, "linked-in-tree.strm")
	if err := os.Symlink(inTreeTarget, inTreeLink); err == nil {
		if res, err := r.ResolveStream(ctx, inTreeLink); err != nil || res == nil || res.URL != "https://93.184.216.34/ok.mp4" {
			t.Fatalf("expected symlink within parent tree to resolve, got res=%+v err=%v", res, err)
		}
	}

	outsideTarget := filepath.Join(outsideDir, "secret.strm")
	_ = os.WriteFile(outsideTarget, []byte("https://93.184.216.34/escaped.mp4\n"), 0o644)
	escapedLink := filepath.Join(dir, "escaped.strm")
	if err := os.Symlink(outsideTarget, escapedLink); err == nil {
		if res, err := r.ResolveStream(ctx, escapedLink); err == nil || res != nil {
			t.Fatalf("expected escaping .strm symlink to be rejected, got res=%+v err=%v", res, err)
		}
	}
}
