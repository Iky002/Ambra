package playback

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/plugins"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestAuditWave1_H03_EnableMediaPlaybackAndVTTTimeMap(t *testing.T) {
	tmp := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmp, "pb_wave1.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	repo := sqlite.NewLibraryRepo(store)
	userRepo := sqlite.NewUserRepo(store)
	engine := New(repo, plugins.NewRegistry(), config.Default(tmp))
	engine.SetUserRepo(userRepo)
	ctx := context.Background()

	moviePath := filepath.Join(tmp, "sample.mp4")
	_ = os.WriteFile(moviePath, []byte("MP4"), 0o644)
	itemID := guid.New()
	_ = repo.UpsertItem(ctx, &sqlite.ItemRecord{
		ID:        itemID,
		LibraryID: "lib1",
		Type:      "Movie",
		Name:      "Sample",
		Container: "mp4",
		Path:      moviePath,
	})
	_ = repo.ReplaceMediaSourcesAndStreams(ctx, itemID, []sqlite.MediaSourceRecord{{
		ID:        guid.New(),
		ItemID:    itemID,
		Name:      "Sample",
		Path:      moviePath,
		Protocol:  "File",
		Container: "mp4",
	}})

	disabledPol := sqlite.DefaultUserPolicy(false)
	disabledPol.EnableMediaPlayback = false
	blockedUser := &sqlite.UserRecord{
		ID:     guid.New(),
		Name:   "blocked",
		Policy: disabledPol,
		Config: sqlite.DefaultUserConfig(),
	}
	adminPol := sqlite.DefaultUserPolicy(true)
	adminPol.EnableMediaPlayback = false
	adminUser := &sqlite.UserRecord{
		ID:      guid.New(),
		Name:    "admin",
		IsAdmin: true,
		Policy:  adminPol,
		Config:  sqlite.DefaultUserConfig(),
	}
	okUser := &sqlite.UserRecord{
		ID:     guid.New(),
		Name:   "viewer",
		Policy: sqlite.DefaultUserPolicy(false),
		Config: sqlite.DefaultUserConfig(),
	}
	for _, u := range []*sqlite.UserRecord{blockedUser, adminUser, okUser} {
		if err := userRepo.CreateUser(ctx, u); err != nil {
			t.Fatal(err)
		}
	}

	// 1. H-03: Non-admin with EnableMediaPlayback=false is denied with ErrPlaybackForbidden.
	if _, _, err := engine.Decide(ctx, itemID, PlaybackInfoRequest{UserID: blockedUser.ID}); !errors.Is(err, ErrPlaybackForbidden) {
		t.Fatalf("expected ErrPlaybackForbidden when EnableMediaPlayback=false, got %v", err)
	}
	if _, _, err := engine.Decide(ctx, itemID, PlaybackInfoRequest{UserID: adminUser.ID}); err != nil {
		t.Fatalf("expected admin user to bypass EnableMediaPlayback=false, got %v", err)
	}
	if _, _, err := engine.Decide(ctx, itemID, PlaybackInfoRequest{UserID: okUser.ID}); err != nil {
		t.Fatalf("expected EnableMediaPlayback=true user to succeed, got %v", err)
	}

	// 2. 3.1-7: ServeSubtitleWithVTTTimeMap injects X-TIMESTAMP-MAP=MPEGTS:900000,LOCAL:00:00:00.000 when requested.
	srtPath := filepath.Join(tmp, "sub.srt")
	_ = os.WriteFile(srtPath, []byte("1\n00:00:01,000 --> 00:00:03,000\nHello HLS\n"), 0o644)

	_, withMap, err := ServeSubtitleWithVTTTimeMap(srtPath, "vtt", 0, true)
	if err != nil {
		t.Fatalf("ServeSubtitleWithVTTTimeMap(true): %v", err)
	}
	if !strings.Contains(string(withMap), "X-TIMESTAMP-MAP=MPEGTS:900000,LOCAL:00:00:00.000") {
		t.Fatalf("expected X-TIMESTAMP-MAP header in VTT output, got:\n%s", string(withMap))
	}

	_, withoutMap, err := ServeSubtitleWithVTTTimeMap(srtPath, "vtt", 0, false)
	if err != nil {
		t.Fatalf("ServeSubtitleWithVTTTimeMap(false): %v", err)
	}
	if strings.Contains(string(withoutMap), "X-TIMESTAMP-MAP") {
		t.Fatalf("did not expect X-TIMESTAMP-MAP when addVttTimeMap=false, got:\n%s", string(withoutMap))
	}
}
