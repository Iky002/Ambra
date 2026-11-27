package session

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/core/auth"
	"github.com/ambra-media/ambra/internal/pkg/eventbus"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestH04H05AndPlayQueue_LiveTvGateUnratedAllowedTagsAndSessionPlayQueue(t *testing.T) {
	dir := t.TempDir()
	st, err := sqlite.Open(filepath.Join(dir, "session_audit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	ctx := context.Background()
	userRepo := sqlite.NewUserRepo(st)
	libRepo := sqlite.NewLibraryRepo(st)
	cfg := config.Default(dir)
	mgr := NewManager(userRepo, cfg, eventbus.New())
	mgr.SetLibraryRepo(libRepo)

	liveLib, err := libRepo.CreateLibrary(ctx, "Live TV DVR", "livetv", []string{"/dvr"})
	if err != nil {
		t.Fatal(err)
	}
	moviesLib, err := libRepo.CreateLibrary(ctx, "Movies", "movies", []string{"/movies"})
	if err != nil {
		t.Fatal(err)
	}

	dvrItem := &sqlite.ItemRecord{
		ID:             guid.New(),
		LibraryID:      liveLib.ID,
		Type:           "Movie",
		Name:           "Secret DVR Recording",
		SortName:       "secret dvr recording",
		Path:           "/dvr/rec1.ts",
		MediaType:      "Video",
		Container:      "ts",
		OfficialRating: "PG",
	}
	unratedMovie := &sqlite.ItemRecord{
		ID:        guid.New(),
		LibraryID: moviesLib.ID,
		Type:      "Movie",
		Name:      "Mystery Unrated Film",
		SortName:  "mystery unrated film",
		Path:      "/movies/unrated.mkv",
		MediaType: "Video",
		Container: "mkv",
	}
	taggedSafeMovie := &sqlite.ItemRecord{
		ID:             guid.New(),
		LibraryID:      moviesLib.ID,
		Type:           "Movie",
		Name:           "Approved Kids Film",
		SortName:       "approved kids film",
		Path:           "/movies/kids.mkv",
		MediaType:      "Video",
		Container:      "mkv",
		OfficialRating: "G",
	}
	untaggedMovie := &sqlite.ItemRecord{
		ID:             guid.New(),
		LibraryID:      moviesLib.ID,
		Type:           "Movie",
		Name:           "Untagged Film",
		SortName:       "untagged film",
		Path:           "/movies/untagged.mkv",
		MediaType:      "Video",
		Container:      "mkv",
		OfficialRating: "G",
	}
	for _, it := range []*sqlite.ItemRecord{dvrItem, unratedMovie, taggedSafeMovie, untaggedMovie} {
		if err := libRepo.UpsertItem(ctx, it); err != nil {
			t.Fatal(err)
		}
	}
	if err := libRepo.ReplaceItemTaxonomies(ctx, taggedSafeMovie.ID, "Tag", []string{"kids-ok"}); err != nil {
		t.Fatal(err)
	}

	// Non-admin user with EnableAllFolders=true, EnableLiveTvAccess=false, BlockUnratedItems=["Movie"], AllowedTags=["kids-ok"].
	pol := sqlite.DefaultUserPolicy(false)
	pol.EnableAllFolders = true
	pol.EnableLiveTvAccess = false
	pol.BlockUnratedItems = []string{"Movie"}
	pol.AllowedTags = []string{"kids-ok"}
	kid := &sqlite.UserRecord{
		Name:    "kid",
		IsAdmin: false,
		Policy:  pol,
		Config:  sqlite.DefaultUserConfig(),
	}
	if err := userRepo.CreateUser(ctx, kid); err != nil {
		t.Fatal(err)
	}

	// H-04: UserCanAccessLibrary denies livetv library even when EnableAllFolders=true!
	if UserCanAccessLibrary(ctx, libRepo, kid, liveLib.ID) {
		t.Fatal("expected UserCanAccessLibrary=false for livetv library when EnableLiveTvAccess=false and EnableAllFolders=true")
	}
	if !UserCanAccessLibrary(ctx, libRepo, kid, moviesLib.ID) {
		t.Fatal("expected UserCanAccessLibrary=true for movies library")
	}

	// H-04 & H-05: hydrateNowPlayingItem refuses to hydrate DVR item, unrated movie, or movie missing AllowedTags!
	for _, blockedID := range []string{dvrItem.ID, unratedMovie.ID, untaggedMovie.ID} {
		cl := auth.ClientInfo{DeviceID: "dev-" + blockedID[:6]}
		if err := mgr.ReportPlaybackStart(ctx, kid, cl, PlaybackStartRequest{ItemId: blockedID}); err != nil {
			t.Fatalf("ReportPlaybackStart without checker failed: %v", err)
		}
		sess, ok := mgr.GetSessionByUserDevice(kid.ID, cl.DeviceID)
		if !ok || sess.NowPlayingItem == nil {
			t.Fatal("expected session NowPlayingItem placeholder")
		}
		if sess.NowPlayingItem.Name != "" {
			t.Fatalf("hydrateNowPlayingItem leaked restricted item metadata %q for item %s", sess.NowPlayingItem.Name, blockedID)
		}
	}

	// Allowed item hydrates Name AND tracks multi-item PlayQueue (3.1-10)!
	clOK := auth.ClientInfo{DeviceID: "dev-ok"}
	id1 := guid.Normalize(taggedSafeMovie.ID)
	id2 := guid.Normalize(untaggedMovie.ID)
	if err := mgr.ReportPlaybackStart(ctx, kid, clOK, PlaybackStartRequest{
		ItemId:       id1,
		QueueItemIds: []string{id1, id2},
	}); err != nil {
		t.Fatal(err)
	}
	sessOK, _ := mgr.GetSessionByUserDevice(kid.ID, clOK.DeviceID)
	if sessOK.NowPlayingItem == nil || sessOK.NowPlayingItem.Name != "Approved Kids Film" {
		t.Fatalf("expected hydrated Name for allowed item, got %+v", sessOK.NowPlayingItem)
	}
	if got := mgr.GetSessionPlayQueueIDs(sessOK.Id); !reflect.DeepEqual(got, []string{id1, id2}) {
		t.Fatalf("expected playQueue=[%s %s], got %v", id1, id2, got)
	}

	// SetSessionPlayQueue PlayNext / Stopped clears queue
	id3 := guid.Normalize(dvrItem.ID)
	mgr.SetSessionPlayQueue(sessOK.Id, []string{id3}, "PlayNext")
	if got := mgr.GetSessionPlayQueueIDs(sessOK.Id); !reflect.DeepEqual(got, []string{id1, id3, id2}) {
		t.Fatalf("expected PlayNext queue=[%s %s %s], got %v", id1, id3, id2, got)
	}
	_ = mgr.ReportPlaybackStopped(ctx, kid, clOK, PlaybackStoppedRequest{ItemId: id1})
	if got := mgr.GetSessionPlayQueueIDs(sessOK.Id); len(got) != 0 {
		t.Fatalf("expected empty playQueue after playback stopped, got %v", got)
	}
}
