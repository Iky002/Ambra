package hls

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadFirstLineURL(t *testing.T) {
	tmp := t.TempDir()

	// BOM + comments + blank lines + raw spaces in URL
	strmPath := filepath.Join(tmp, "bom_spaces.strm")
	content := "\uFEFF# comment line\n\n  # another comment\n  https://cdn.example.com/My Movies/Test Video 4K.mkv  \nhttps://ignored.example.com/second.mkv\n"
	if err := os.WriteFile(strmPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	got := readFirstLineURL(strmPath)
	want := "https://cdn.example.com/My%20Movies/Test%20Video%204K.mkv"
	if got != want {
		t.Fatalf("readFirstLineURL = %q, want %q", got, want)
	}

	// BOM directly before URL on first line
	bomDirect := filepath.Join(tmp, "bom_direct.strm")
	if err := os.WriteFile(bomDirect, []byte("\uFEFFhttp://alist.local/d/a b.mp4"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := readFirstLineURL(bomDirect); got != "http://alist.local/d/a%20b.mp4" {
		t.Fatalf("readFirstLineURL bomDirect = %q", got)
	}

	// Only comments / empty -> ""
	emptyPath := filepath.Join(tmp, "empty.strm")
	_ = os.WriteFile(emptyPath, []byte("# only comment\n\n"), 0o644)
	if got := readFirstLineURL(emptyPath); got != "" {
		t.Fatalf("expected empty string, got %q", got)
	}

	// Missing file -> ""
	if got := readFirstLineURL(filepath.Join(tmp, "missing.strm")); got != "" {
		t.Fatalf("expected empty string for missing file, got %q", got)
	}
}
