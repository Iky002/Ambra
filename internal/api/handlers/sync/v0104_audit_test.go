package sync

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/ambra-media/ambra/internal/api/middleware"
	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestSyncV0104ItemIDCapReadyACLParentalAndMediaStreamSanitize(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default(dir)
	st, err := sqlite.Open(cfg.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	ctx := context.Background()
	libRepo := sqlite.NewLibraryRepo(st)
	userRepo := sqlite.NewUserRepo(st)

	libA, _ := libRepo.CreateLibrary(ctx, "AllowedLib", "movies", []string{dir})
	libB, _ := libRepo.CreateLibrary(ctx, "RevokedLib", "movies", []string{dir})

	itemA := guid.New()
	itemB := guid.New()
	itemR := guid.New()
	_ = libRepo.UpsertItem(ctx, &sqlite.ItemRecord{
		ID: itemA, LibraryID: libA.ID, ParentID: libA.ID, Type: "Movie", MediaType: "Video",
		Name: "Family Film", OfficialRating: "PG", Path: "/srv/media/family.mkv",
	})
	_ = libRepo.ReplaceMediaSourcesAndStreams(ctx, itemA, []sqlite.MediaSourceRecord{{
		ID: "ms-a", ItemID: itemA, Name: "family.mkv", Path: "/srv/media/family.mkv", Container: "mkv",
		Streams: []sqlite.MediaStreamRecord{
			{MediaSourceID: "ms-a", ItemID: itemA, StreamIndex: 0, StreamType: "Video", Codec: "h264"},
			{MediaSourceID: "ms-a", ItemID: itemA, StreamIndex: 1, StreamType: "Subtitle", Codec: "srt", IsExternal: true, Path: "/srv/media/family.zh.srt"},
		},
	}})
	_ = libRepo.UpsertItem(ctx, &sqlite.ItemRecord{
		ID: itemB, LibraryID: libB.ID, ParentID: libB.ID, Type: "Movie", MediaType: "Video",
		Name: "Later Revoked Film", OfficialRating: "PG", Path: "/srv/media/revoked.mkv",
	})
	_ = libRepo.UpsertItem(ctx, &sqlite.ItemRecord{
		ID: itemR, LibraryID: libA.ID, ParentID: libA.ID, Type: "Movie", MediaType: "Video",
		Name: "Adult Film", OfficialRating: "R", Path: "/srv/media/adult.mkv",
	})

	pol := sqlite.DefaultUserPolicy(false)
	pol.EnableAllFolders = true
	pol.EnableContentDownloading = true
	alice := &sqlite.UserRecord{Name: "alice_v104", Policy: pol, Config: sqlite.DefaultUserConfig()}
	_ = userRepo.CreateUser(ctx, alice)

	h := New(st, cfg)
	h.SetLibraryRepo(libRepo)
	h.SetUserRepo(userRepo)
	mux := http.NewServeMux()
	authMW := middleware.NewAuth(nil, nil)
	h.RegisterRoutes(mux, AuthFuncs{RequireAuth: authMW.RequireAuth, RequireAdmin: authMW.RequireAdmin})

	aliceAC := &middleware.AuthContext{User: alice}

	// 1. ItemIds exceeding 500 rejected with 400
	tooMany := make([]string, maxSyncJobItemIDs+1)
	for i := range tooMany {
		tooMany[i] = fmt.Sprintf("item-%d", i)
	}
	if r := doSyncAuthReq(t, mux, aliceAC, http.MethodPost, "/Sync/Jobs", map[string]any{
		"TargetId": "dev-1",
		"ItemIds":  tooMany,
	}); r.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for >%d ItemIds, got %d body=%s", maxSyncJobItemIDs, r.Code, r.Body.String())
	}

	// 2. Alice initially has access to all 3 items and creates a sync job
	recCreate := doSyncAuthReq(t, mux, aliceAC, http.MethodPost, "/Sync/Jobs", map[string]any{
		"TargetId": "dev-1",
		"ItemIds":  []string{itemA, itemB, itemR},
	})
	if recCreate.Code != http.StatusOK {
		t.Fatalf("create sync job: %d %s", recCreate.Code, recCreate.Body.String())
	}

	// 3. Admin later revokes libB and sets MaxParentalRating=7 (PG-13) for Alice!
	maxRating := 7
	alice.Policy.EnableAllFolders = false
	alice.Policy.EnabledFolders = []string{libA.ID}
	alice.Policy.MaxParentalRating = &maxRating
	if err := userRepo.UpdateUserPolicy(ctx, alice.ID, alice.Policy); err != nil {
		t.Fatalf("UpdateUserPolicy: %v", err)
	}

	// 4. GET /Sync/Items/Ready MUST re-check ACL & parental policy -> only itemA returned, AND MediaStreams[].Path stripped!
	recReady := doSyncAuthReq(t, mux, aliceAC, http.MethodGet, "/Sync/Items/Ready?TargetId=dev-1", nil)
	if recReady.Code != http.StatusOK {
		t.Fatalf("GET /Sync/Items/Ready status=%d", recReady.Code)
	}
	var ready []SyncedItem
	_ = json.Unmarshal(recReady.Body.Bytes(), &ready)
	if len(ready) != 1 || ready[0].ItemId != itemA {
		t.Fatalf("expected only itemA after ACL/parental revocation, got %+v", ready)
	}
	if ready[0].Item == nil || ready[0].Item.Path != "" {
		t.Fatalf("expected Item.Path stripped, got %+v", ready[0].Item)
	}
	for _, ms := range ready[0].Item.MediaSources {
		if ms.Path != "" {
			t.Fatalf("expected MediaSource.Path stripped, got %q", ms.Path)
		}
		for _, strm := range ms.MediaStreams {
			if strm.Path != "" {
				t.Fatalf("expected MediaSource.MediaStreams[].Path stripped, got %q", strm.Path)
			}
		}
	}
	for _, strm := range ready[0].Item.MediaStreams {
		if strm.Path != "" {
			t.Fatalf("expected Item.MediaStreams[].Path stripped, got %q", strm.Path)
		}
	}
}
