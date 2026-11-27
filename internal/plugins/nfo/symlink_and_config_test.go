package nfo

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/ambra-media/ambra/internal/plugins"
)

func TestAuditV0107_H7_NFOSymlinkBoundaryProtection(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks require privileges on windows")
	}
	tmp := t.TempDir()
	mediaDir := filepath.Join(tmp, "movie")
	outsideDir := filepath.Join(tmp, "outside")
	_ = os.MkdirAll(mediaDir, 0o755)
	_ = os.MkdirAll(outsideDir, 0o755)

	secretNFO := filepath.Join(outsideDir, "secret.nfo")
	_ = os.WriteFile(secretNFO, []byte(`<movie><title>Secret Escaped</title></movie>`), 0o600)

	movieFile := filepath.Join(mediaDir, "Movie.mkv")
	_ = os.WriteFile(movieFile, []byte("mkv"), 0o600)

	// 1. Escaping symlink NFO must be rejected
	escapingNFO := filepath.Join(mediaDir, "Movie.nfo")
	if err := os.Symlink(secretNFO, escapingNFO); err != nil {
		t.Fatal(err)
	}
	if _, err := readLimitedNFO(escapingNFO); err == nil {
		t.Fatal("expected readLimitedNFO to reject escaping symlink")
	}
	p := New()
	if res, err := p.FetchMetadata(context.Background(), plugins.MediaQuery{ItemType: "Movie", Path: movieFile}); err != nil || res != nil {
		t.Fatalf("expected nil metadata for escaping NFO symlink, got res=%+v err=%v", res, err)
	}

	// 2. Intra-directory symlink NFO is allowed
	realLocal := filepath.Join(mediaDir, "local_real.nfo")
	_ = os.WriteFile(realLocal, []byte(`<movie><title>Local Safe</title></movie>`), 0o600)
	_ = os.Remove(escapingNFO)
	if err := os.Symlink(realLocal, escapingNFO); err != nil {
		t.Fatal(err)
	}
	res, err := p.FetchMetadata(context.Background(), plugins.MediaQuery{ItemType: "Movie", Path: movieFile})
	if err != nil || res == nil || res.Name != "Local Safe" {
		t.Fatalf("expected intra-dir symlink NFO to load, got res=%+v err=%v", res, err)
	}

	// 3. Directory target and missing target rejected
	if _, err := readLimitedNFO(mediaDir); err == nil {
		t.Fatal("expected error reading directory as NFO")
	}
	if _, err := readLimitedNFO(filepath.Join(mediaDir, "missing.nfo")); err == nil {
		t.Fatal("expected error reading missing NFO")
	}
}

func TestAuditV0109_L28_PascalCaseEnabled(t *testing.T) {
	p := New()
	if !p.Manifest().Enabled {
		t.Fatal("expected enabled by default")
	}
	if err := p.Configure(map[string]any{"Enabled": false}); err != nil {
		t.Fatalf("Configure Enabled=false: %v", err)
	}
	if p.Manifest().Enabled {
		t.Fatal("expected Enabled=false via PascalCase")
	}
	if err := p.Configure(map[string]any{"Enabled": true}); err != nil {
		t.Fatalf("Configure Enabled=true: %v", err)
	}
	if !p.Manifest().Enabled {
		t.Fatal("expected Enabled=true via PascalCase")
	}
}
