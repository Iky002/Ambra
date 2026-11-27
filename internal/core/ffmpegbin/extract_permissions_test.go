package ffmpegbin

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestAuditV0107_ExtractOneEnforcesExecutableMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix execute bits check")
	}
	dir := t.TempDir()
	payload := []byte("#!/bin/sh\necho ok\n")
	dest := filepath.Join(dir, "ffmpeg")

	// Pre-create same-size file WITHOUT execute bits (0o644)
	if err := os.WriteFile(dest, payload, 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := extractOne(dir, "ffmpeg", payload)
	if err != nil {
		t.Fatalf("extractOne: %v", err)
	}
	if out != dest {
		t.Fatalf("expected %q, got %q", dest, out)
	}
	info, err := os.Stat(dest)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o111 == 0 {
		t.Fatalf("expected executable permissions on %q, got %v", dest, info.Mode().Perm())
	}
}
