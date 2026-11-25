package compatmedia

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ambra-media/ambra/internal/api/dto"
	"github.com/ambra-media/ambra/internal/api/middleware"
	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/core/auth"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestCompatMediaV097HardeningAndParity(t *testing.T) {
	tmp := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmp, "cm_v097.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	ctx := context.Background()
	repo := sqlite.NewLibraryRepo(store)
	userRepo := sqlite.NewUserRepo(store)
	cfg := config.Default(tmp)

	authSvc := auth.NewService(userRepo, cfg)
	_, _ = authSvc.CreateUser(ctx, "admin", "admin123", true)
	alice, _ := authSvc.CreateUser(ctx, "alice", "alice123", false)
	bob, _ := authSvc.CreateUser(ctx, "bob", "bob123", false)

	libA, _ := repo.CreateLibrary(ctx, "LibA", "movies", []string{filepath.Join(tmp, "a")})
	libB, _ := repo.CreateLibrary(ctx, "LibB", "movies", []string{filepath.Join(tmp, "b")})

	maxPG := 5
	alicePol := alice.Policy
	alicePol.EnableAllFolders = false
	alicePol.EnabledFolders = []string{libA.ID}
	alicePol.MaxParentalRating = &maxPG
	alicePol.BlockedTags = []string{"nsfw"}
	if err := userRepo.UpdateUserPolicy(ctx, alice.ID, alicePol); err != nil {
		t.Fatal(err)
	}

	movieSafe := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: libA.ID, ParentID: libA.ID,
		Type: "Movie", MediaType: "Video", Name: "Safe Movie", SortName: "safe movie",
		OfficialRating: "PG", Path: filepath.Join(tmp, "a", "safe.mkv"),
		Extra: sqlite.ItemExtra{
			Overview:     "Some overview",
			Taglines:     []string{"Tagline 1"},
			LockedFields: []string{"Name", "Overview"},
			LockData:     true,
		},
	}
	movieR := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: libA.ID, ParentID: libA.ID,
		Type: "Movie", MediaType: "Video", Name: "Adult R Movie", SortName: "adult r movie",
		OfficialRating: "R", Path: filepath.Join(tmp, "a", "r.mkv"),
	}
	movieSecret := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: libB.ID, ParentID: libB.ID,
		Type: "Movie", MediaType: "Video", Name: "Secret Movie", SortName: "secret movie",
		OfficialRating: "G", Path: filepath.Join(tmp, "b", "secret.mkv"),
	}
	for _, m := range []*sqlite.ItemRecord{movieSafe, movieR, movieSecret} {
		_ = repo.UpsertItem(ctx, m)
	}

	// Artists & AudioBooks & Channels & Episodes (past vs future)
	artistSafe := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: libA.ID, ParentID: libA.ID,
		Type: "MusicArtist", Name: "Safe Artist", SortName: "safe artist", OfficialRating: "G",
	}
	artistSecret := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: libB.ID, ParentID: libB.ID,
		Type: "MusicArtist", Name: "Secret Artist", SortName: "secret artist", OfficialRating: "G",
	}
	abSafe := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: libA.ID, ParentID: libA.ID,
		Type: "AudioBook", MediaType: "Audio", Name: "Safe Book", SortName: "safe book", OfficialRating: "PG",
	}
	abR := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: libA.ID, ParentID: libA.ID,
		Type: "AudioBook", MediaType: "Audio", Name: "Adult Book", SortName: "adult book", OfficialRating: "R",
	}
	ch1ID, ch2ID := guid.New(), guid.New()
	ch1 := &sqlite.ItemRecord{ID: ch1ID, LibraryID: libA.ID, ParentID: libA.ID, Type: "Channel", Name: "Ch1", SortName: "ch1"}
	ch2 := &sqlite.ItemRecord{ID: ch2ID, LibraryID: libA.ID, ParentID: libA.ID, Type: "Channel", Name: "Ch2", SortName: "ch2"}
	ch1Vid := &sqlite.ItemRecord{ID: guid.New(), LibraryID: libA.ID, ParentID: ch1ID, Type: "ChannelVideoItem", MediaType: "Video", Name: "Ch1Vid", SortName: "ch1vid", OfficialRating: "PG"}
	ch2Vid := &sqlite.ItemRecord{ID: guid.New(), LibraryID: libA.ID, ParentID: ch2ID, Type: "ChannelVideoItem", MediaType: "Video", Name: "Ch2Vid", SortName: "ch2vid", OfficialRating: "PG"}
	ch2VidR := &sqlite.ItemRecord{ID: guid.New(), LibraryID: libA.ID, ParentID: ch2ID, Type: "ChannelVideoItem", MediaType: "Video", Name: "Ch2VidR", SortName: "ch2vidr", OfficialRating: "R"}

	pastTS := time.Now().Add(-48 * time.Hour).UTC().Unix()
	futureTS := time.Now().Add(48 * time.Hour).UTC().Unix()
	epPast := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: libA.ID, ParentID: libA.ID,
		Type: "Episode", MediaType: "Video", Name: "Past Ep", SortName: "past ep",
		PremiereDate: &pastTS, OfficialRating: "PG",
	}
	epFutureSafe := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: libA.ID, ParentID: libA.ID,
		Type: "Episode", MediaType: "Video", Name: "Future Safe Ep", SortName: "future safe ep",
		PremiereDate: &futureTS, OfficialRating: "PG",
	}
	epFutureR := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: libA.ID, ParentID: libA.ID,
		Type: "Episode", MediaType: "Video", Name: "Future R Ep", SortName: "future r ep",
		PremiereDate: &futureTS, OfficialRating: "R",
	}
	epFutureSecret := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: libB.ID, ParentID: libB.ID,
		Type: "Episode", MediaType: "Video", Name: "Future Secret Ep", SortName: "future secret ep",
		PremiereDate: &futureTS, OfficialRating: "G",
	}
	for _, it := range []*sqlite.ItemRecord{artistSafe, artistSecret, abSafe, abR, ch1, ch2, ch1Vid, ch2Vid, ch2VidR, epPast, epFutureSafe, epFutureR, epFutureSecret} {
		_ = repo.UpsertItem(ctx, it)
	}
	_ = repo.ReplaceItemAncestors(ctx, ch1Vid.ID, []sqlite.AncestorLink{{AncestorID: ch1ID, Depth: 1}, {AncestorID: libA.ID, Depth: 2}})
	_ = repo.ReplaceItemAncestors(ctx, ch2Vid.ID, []sqlite.AncestorLink{{AncestorID: ch2ID, Depth: 1}, {AncestorID: libA.ID, Depth: 2}})
	_ = repo.ReplaceItemAncestors(ctx, ch2VidR.ID, []sqlite.AncestorLink{{AncestorID: ch2ID, Depth: 1}, {AncestorID: libA.ID, Depth: 2}})
	// Mark both audiobooks as resumable for alice
	for _, abID := range []string{abSafe.ID, abR.ID} {
		_, err := store.Writer.ExecContext(ctx,
			`INSERT INTO user_item_data (user_id, item_id, playback_position_ticks, played, last_played_date) VALUES (?, ?, ?, 0, ?)`,
			alice.ID, abID, int64(123456), time.Now().Unix(),
		)
		if err != nil {
			t.Fatal(err)
		}
	}

	box, _ := repo.CreateCollection(ctx, "TestBox", "", []string{movieSafe.ID, movieR.ID, movieSecret.ID})
	alicePl, _ := repo.CreatePlaylist(ctx, "AlicePl", "Video", alice.ID, []string{movieSafe.ID, movieR.ID})
	bobPl, _ := repo.CreatePlaylist(ctx, "BobPl", "Video", bob.ID, []string{movieSafe.ID})

	// Seed 2 backdrop images on movieSafe for index swap test
	_ = repo.UpsertImage(ctx, &sqlite.ImageRecord{ItemID: movieSafe.ID, ImageType: "Backdrop", ImageIndex: 0, Path: "/tmp/b0.jpg", Tag: "tag0"})
	_ = repo.UpsertImage(ctx, &sqlite.ImageRecord{ItemID: movieSafe.ID, ImageType: "Backdrop", ImageIndex: 1, Path: "/tmp/b1.jpg", Tag: "tag1"})

	adminAuth, _ := authSvc.AuthenticateByName(ctx, "admin", "admin123", "", auth.ClientInfo{DeviceID: "d-admin"})
	aliceAuth, _ := authSvc.AuthenticateByName(ctx, "alice", "alice123", "", auth.ClientInfo{DeviceID: "d-alice"})
	bobAuth, _ := authSvc.AuthenticateByName(ctx, "bob", "bob123", "", auth.ClientInfo{DeviceID: "d-bob"})

	authMW := middleware.NewAuth(authSvc, nil)
	h := New(repo, nil, nil, nil, store, cfg)
	h.SetUserRepo(userRepo)
	router := h.BuildRouter(AuthFuncs{
		RequireAuth:  authMW.RequireAuth,
		RequireAdmin: authMW.RequireAdmin,
		UserIDFromCtx: func(c context.Context) string {
			if ac, ok := middleware.AuthFromContext(c); ok && ac != nil {
				return ac.UserID()
			}
			return ""
		},
	})

	call := func(token, method, path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		if token != "" {
			req.Header.Set("X-Emby-Token", token)
		}
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}

	// 1. P0 POST /Playlists/{Id}/Items/Delete: rejects BoxSet (404), rejects non-owner (403), allows owner (204)
	if rec := call(aliceAuth.AccessToken, http.MethodPost, "/Playlists/"+box.ID+"/Items/Delete", `{"Ids":["`+movieSafe.ID+`"]}`); rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 deleting BoxSet item via /Playlists/{Id}/Items/Delete, got %d", rec.Code)
	}
	if rec := call(aliceAuth.AccessToken, http.MethodPost, "/Playlists/"+bobPl.ID+"/Items/Delete", `{"Ids":["`+movieSafe.ID+`"]}`); rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for alice deleting from bob's playlist, got %d", rec.Code)
	}
	if rec := call(bobAuth.AccessToken, http.MethodPost, "/Playlists/"+bobPl.ID+"/Items/Delete", `{"Ids":["`+movieSafe.ID+`"]}`); rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204 for bob deleting from own playlist, got %d", rec.Code)
	}

	// 2. Parental & library RBAC via canAccessItem on Images, Ancestors, DeleteInfo, ExternalIdInfos, RemoteImages
	for _, blockedID := range []string{movieR.ID, movieSecret.ID} {
		for _, suffix := range []string{"/Images", "/Ancestors", "/DeleteInfo", "/ExternalIdInfos", "/RemoteImages"} {
			if rec := call(aliceAuth.AccessToken, http.MethodGet, "/Items/"+blockedID+suffix, ""); rec.Code != http.StatusForbidden {
				t.Fatalf("expected 403 for alice GET /Items/%s%s, got %d", blockedID, suffix, rec.Code)
			}
		}
	}
	// Allowed item succeeds for alice (except admin-only DeleteInfo)
	for _, suffix := range []string{"/Images", "/Ancestors", "/ExternalIdInfos", "/RemoteImages"} {
		if rec := call(aliceAuth.AccessToken, http.MethodGet, "/Items/"+movieSafe.ID+suffix, ""); rec.Code != http.StatusOK {
			t.Fatalf("expected 200 for alice GET /Items/%s%s, got %d", movieSafe.ID, suffix, rec.Code)
		}
	}
	if rec := call(aliceAuth.AccessToken, http.MethodGet, "/Items/"+movieSafe.ID+"/DeleteInfo", ""); rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for non-admin alice GET /Items/%s/DeleteInfo, got %d", movieSafe.ID, rec.Code)
	}
	if rec := call(adminAuth.AccessToken, http.MethodGet, "/Items/"+movieSafe.ID+"/DeleteInfo", ""); rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for admin GET /Items/%s/DeleteInfo, got %d", movieSafe.ID, rec.Code)
	}

	// 3. GET /Shows/Upcoming: only future episodes, respecting AllowedLibraryIDs + parental policy
	rec := call(aliceAuth.AccessToken, http.MethodGet, "/Shows/Upcoming", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for /Shows/Upcoming, got %d", rec.Code)
	}
	var upcoming dto.QueryResult[dto.BaseItemDto]
	_ = json.Unmarshal(rec.Body.Bytes(), &upcoming)
	if upcoming.TotalRecordCount != 1 || len(upcoming.Items) != 1 || upcoming.Items[0].Id != epFutureSafe.ID {
		t.Fatalf("expected only epFutureSafe in /Shows/Upcoming for alice, got %+v", upcoming)
	}

	// 4. Artists, AudioBooks/NextUp, Channels/Items/Latest, Collections/ProviderItems, Playlists/AddToPlaylistInfo
	rec = call(aliceAuth.AccessToken, http.MethodGet, "/Artists", "")
	var artists dto.QueryResult[dto.BaseItemDto]
	_ = json.Unmarshal(rec.Body.Bytes(), &artists)
	if artists.TotalRecordCount != 1 || artists.Items[0].Id != artistSafe.ID {
		t.Fatalf("expected only artistSafe in /Artists, got %+v", artists)
	}

	rec = call(aliceAuth.AccessToken, http.MethodGet, "/AudioBooks/NextUp", "")
	var abRes dto.QueryResult[dto.BaseItemDto]
	_ = json.Unmarshal(rec.Body.Bytes(), &abRes)
	if abRes.TotalRecordCount != 1 || abRes.Items[0].Id != abSafe.ID {
		t.Fatalf("expected only abSafe in /AudioBooks/NextUp, got %+v", abRes)
	}

	// Empty ChannelIds returns empty rather than entire library
	rec = call(aliceAuth.AccessToken, http.MethodGet, "/Channels/Items/Latest", "")
	var chEmpty dto.QueryResult[dto.BaseItemDto]
	_ = json.Unmarshal(rec.Body.Bytes(), &chEmpty)
	if chEmpty.TotalRecordCount != 0 || len(chEmpty.Items) != 0 {
		t.Fatalf("expected empty result for /Channels/Items/Latest without ChannelIds, got %+v", chEmpty)
	}

	// Comma-separated ChannelIds splits properly and enforces parental controls
	rec = call(aliceAuth.AccessToken, http.MethodGet, "/Channels/Items/Latest?ChannelIds="+ch1ID+","+ch2ID, "")
	var chItems dto.QueryResult[dto.BaseItemDto]
	_ = json.Unmarshal(rec.Body.Bytes(), &chItems)
	if chItems.TotalRecordCount != 2 || len(chItems.Items) != 2 {
		t.Fatalf("expected 2 safe channel items across ch1+ch2, got %+v", chItems)
	}

	rec = call(aliceAuth.AccessToken, http.MethodGet, "/Collections/"+box.ID+"/ProviderItems", "")
	var provItems dto.QueryResult[sqlite.PlaylistItemDto]
	_ = json.Unmarshal(rec.Body.Bytes(), &provItems)
	if provItems.TotalRecordCount != 1 || provItems.Items[0].Id != movieSafe.ID {
		t.Fatalf("expected only movieSafe in ProviderItems, got %+v", provItems)
	}

	// AddToPlaylistInfo: 403 on bob's private playlist; filters blocked movieR on alice's own playlist
	if rec := call(aliceAuth.AccessToken, http.MethodGet, "/Playlists/"+bobPl.ID+"/AddToPlaylistInfo?Ids="+movieSafe.ID, ""); rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for alice AddToPlaylistInfo on bob's private playlist, got %d", rec.Code)
	}
	rec = call(aliceAuth.AccessToken, http.MethodGet, "/Playlists/"+alicePl.ID+"/AddToPlaylistInfo?Ids="+movieSafe.ID+","+movieR.ID, "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"DuplicateCount":1`) {
		t.Fatalf("expected DuplicateCount=1 (only movieSafe visible), got %d %s", rec.Code, rec.Body.String())
	}

	// 5. Image index swap handles unique constraint collision safely
	if rec := call(adminAuth.AccessToken, http.MethodPost, "/Items/"+movieSafe.ID+"/Images/Backdrop/0/Index?newIndex=1", ""); rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204 swapping backdrop index 0 <-> 1, got %d %s", rec.Code, rec.Body.String())
	}
	img0, err := repo.GetImageByItemAndType(ctx, movieSafe.ID, "Backdrop", 0)
	if err != nil || img0.Tag != "tag1" {
		t.Fatalf("expected Backdrop[0] to now have tag1, got %+v err=%v", img0, err)
	}
	img1, err := repo.GetImageByItemAndType(ctx, movieSafe.ID, "Backdrop", 1)
	if err != nil || img1.Tag != "tag0" {
		t.Fatalf("expected Backdrop[1] to now have tag0, got %+v err=%v", img1, err)
	}

	// 6. POST /Library/VirtualFolders/Paths/Update replaces old path with new path
	newPath := filepath.Join(tmp, "a_renamed")
	if rec := call(adminAuth.AccessToken, http.MethodPost, "/Library/VirtualFolders/Paths/Update", `{"Id":"`+libA.ID+`","OldPath":"`+filepath.Join(tmp, "a")+`","PathInfo":{"Path":"`+newPath+`"}}`); rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204 updating virtual folder path, got %d", rec.Code)
	}
	updatedLib, _ := repo.GetLibrary(ctx, libA.ID)
	if len(updatedLib.Paths) != 1 || updatedLib.Paths[0] != newPath {
		t.Fatalf("expected libA paths to be [%s], got %v", newPath, updatedLib.Paths)
	}

	// 7. POST /Items/Metadata/Reset parses JSON body ItemIds and resets Overview, Taglines, LockedFields
	if rec := call(adminAuth.AccessToken, http.MethodPost, "/Items/Metadata/Reset", `{"ItemIds":["`+movieSafe.ID+`"]}`); rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204 for Metadata/Reset, got %d", rec.Code)
	}
	resetRec, _ := repo.GetItemRecordByID(ctx, movieSafe.ID)
	if resetRec.Extra.Overview != "" || len(resetRec.Extra.Taglines) != 0 || len(resetRec.Extra.LockedFields) != 0 || resetRec.Extra.LockData {
		t.Fatalf("expected Overview/Taglines/LockedFields/LockData to be reset, got %+v", resetRec.Extra)
	}

	// 8. MakePublic / MakePrivate requires admin or ownership/access
	if rec := call(aliceAuth.AccessToken, http.MethodPost, "/Items/"+bobPl.ID+"/MakePublic", ""); rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for alice MakePublic on bob's playlist, got %d", rec.Code)
	}
	if rec := call(aliceAuth.AccessToken, http.MethodPost, "/Items/"+movieSecret.ID+"/MakePrivate", ""); rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for alice MakePrivate on forbidden library item, got %d", rec.Code)
	}
	if rec := call(aliceAuth.AccessToken, http.MethodPost, "/Items/"+alicePl.ID+"/MakePublic", ""); rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204 for alice MakePublic on own playlist, got %d", rec.Code)
	}
	if rec := call(adminAuth.AccessToken, http.MethodPost, "/Items/"+bobPl.ID+"/MakePrivate", ""); rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204 for admin MakePrivate on bob's playlist, got %d", rec.Code)
	}
}
