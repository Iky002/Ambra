package strm

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestResolveSTRM(t *testing.T) {
	dir := t.TempDir()
	strmPath := filepath.Join(dir, "Movie.strm")
	content := "# comment line\n\nhttps://cdn.example.com/media/movie.mkv?token=abc\n"
	if err := os.WriteFile(strmPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	r := New(true)
	res, err := r.ResolveStream(context.Background(), strmPath)
	if err != nil || res == nil {
		t.Fatalf("ResolveStream failed: res=%v err=%v", res, err)
	}
	if res.URL != "https://cdn.example.com/media/movie.mkv?token=abc" || !res.Is302 {
		t.Fatalf("unexpected resolve result: %+v", res)
	}

	// Non-strm file returns nil
	nonStrm, err := r.ResolveStream(context.Background(), filepath.Join(dir, "Movie.mkv"))
	if err != nil || nonStrm != nil {
		t.Fatalf("expected nil for non-strm file")
	}

	// Disabled 302
	r2 := New(false)
	res2, err := r2.ResolveStream(context.Background(), strmPath)
	if err != nil || res2 == nil || res2.Is302 {
		t.Fatalf("expected Is302=false when disabled, got %+v", res2)
	}
}
