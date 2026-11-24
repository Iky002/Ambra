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
	if r.Manifest().Enabled || r.enable302 {
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
	_ = os.WriteFile(httpPath, []byte("\xef\xbb\xbfhttp://example.com/a.mp4\n"), 0o644)
	res, err := r.ResolveStream(ctx, httpPath)
	if err != nil || res == nil || res.URL != "http://example.com/a.mp4" || !res.Is302 {
		t.Fatalf("unexpected http result: res=%+v err=%v", res, err)
	}

	// Local file path inside strm -> Is302=false even when enable302=true
	localPath := filepath.Join(dir, "local.strm")
	_ = os.WriteFile(localPath, []byte("/mnt/media/movie.mkv\n"), 0o644)
	res, err = r.ResolveStream(ctx, localPath)
	if err != nil || res == nil || res.URL != "/mnt/media/movie.mkv" || res.Is302 {
		t.Fatalf("unexpected local result: res=%+v err=%v", res, err)
	}
}
