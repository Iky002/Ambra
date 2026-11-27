package scanner

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/pkg/eventbus"
	"github.com/ambra-media/ambra/internal/plugins"
	"github.com/ambra-media/ambra/internal/plugins/nfo"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestAuditV0112_ProviderIdsLockCompanionNFOReplaceFalseAndScanPathAsync(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	dataDir := filepath.Join(tmp, "data")
	moviesDir := filepath.Join(tmp, "movies")
	movDir := filepath.Join(moviesDir, "Inception (2010)")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(movDir, 0o755); err != nil {
		t.Fatal(err)
	}

	store, err := sqlite.Open(filepath.Join(dataDir, "scanner_v0112.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	repo := sqlite.NewLibraryRepo(store)
	reg := plugins.NewRegistry()
	reg.Register(nfo.New())
	bus := eventbus.New()
	cfg := config.Default(dataDir)

	mkvPath := filepath.Join(movDir, "inception.mkv")
	nfoPath := filepath.Join(movDir, "inception.nfo")
	_ = os.WriteFile(mkvPath, []byte("video-bytes"), 0o644)
	_ = os.WriteFile(nfoPath, []byte(`<movie><title>Inception</title><tmdbid>27205</tmdbid></movie>`), 0o644)

	lib, err := repo.CreateLibrary(ctx, "Movies", "movies", []string{moviesDir})
	if err != nil {
		t.Fatal(err)
	}

	sc := New(repo, reg, bus, cfg)

	// 1. ScanPathAsync + WaitIdle scans targeted directory cleanly.
	sc.ScanPathAsync(lib.ID, movDir)
	waitCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	sc.WaitIdle(waitCtx)

	rec, err := repo.FindItemByMediaSourcePath(ctx, lib.ID, mkvPath)
	if err != nil || rec == nil {
		t.Fatalf("expected item created by ScanPathAsync, got (%+v, %v)", rec, err)
	}

	// 2. L-09: User customizes Overview and locks ProviderIds (with manually curated Tmdb="999999").
	rec.Extra.Overview = "Custom user curated overview"
	rec.Extra.ProviderIDs = map[string]string{"Tmdb": "999999"}
	rec.Extra.LockedFields = []string{"ProviderIds"}
	if err := repo.UpsertItem(ctx, rec); err != nil {
		t.Fatal(err)
	}

	// Modify companion .nfo with a DIFFERENT tmdbid ("111111") and NO <plot>, then trigger companion NFO incremental change!
	_ = os.WriteFile(nfoPath, []byte(`<movie><title>Inception Director Cut</title><tmdbid>111111</tmdbid></movie>`), 0o644)
	if _, err := sc.ScanPath(ctx, lib.ID, nfoPath); err != nil {
		t.Fatalf("companion ScanPath(nfo) failed: %v", err)
	}

	afterCompanion, err := repo.GetItemRecordByID(ctx, rec.ID)
	if err != nil {
		t.Fatal(err)
	}
	// Because handleCompanionFileChange uses replaceMetadata=false, existing non-empty Overview MUST NOT be wiped,
	// AND because ProviderIds is in LockedFields, Tmdb MUST stay "999999"!
	if afterCompanion.Extra.Overview != "Custom user curated overview" {
		t.Fatalf("expected existing Overview preserved on incremental companion NFO update (replace=false), got %q", afterCompanion.Extra.Overview)
	}
	if afterCompanion.Extra.ProviderIDs["Tmdb"] != "999999" {
		t.Fatalf("expected locked ProviderIds['Tmdb']='999999' preserved on companion refresh, got %+v", afterCompanion.Extra.ProviderIDs)
	}

	// Even a full replaceMetadata=true RefreshItem MUST still honor LockedFields=["ProviderIds"]!
	if err := sc.RefreshItem(ctx, rec.ID, true, false); err != nil {
		t.Fatalf("RefreshItem failed: %v", err)
	}
	afterFullRefresh, err := repo.GetItemRecordByID(ctx, rec.ID)
	if err != nil {
		t.Fatal(err)
	}
	if afterFullRefresh.Extra.ProviderIDs["Tmdb"] != "999999" {
		t.Fatalf("expected locked ProviderIds['Tmdb']='999999' preserved on ReplaceAllMetadata refresh, got %+v", afterFullRefresh.Extra.ProviderIDs)
	}
}
