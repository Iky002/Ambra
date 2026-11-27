package compat_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ambra-media/ambra/internal/api/dto"
	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/server"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func doCompatJSON(t *testing.T, handler http.Handler, token, method, path string, body any, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("encode json for %s %s: %v", method, path, err)
		}
	}
	req := httptest.NewRequest(method, path, &buf)
	req.RemoteAddr = "127.0.0.1:45123"
	if token != "" {
		req.Header.Set("X-Emby-Token", token)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	return w
}

func createCompatNonAdminUser(t *testing.T, srv *server.Server, adminToken, name, pw string, policy dto.UserPolicy) (token string, userID string) {
	t.Helper()
	w := doCompatJSON(t, srv.Handler, adminToken, http.MethodPost, "/emby/Users/New", map[string]any{
		"Name":            name,
		"Password":        pw,
		"IsAdministrator": false,
	}, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("create user %s failed: %d %s", name, w.Code, w.Body.String())
	}
	var created dto.UserDto
	if err := json.NewDecoder(w.Body).Decode(&created); err != nil || created.Id == "" {
		t.Fatalf("decode user %s: %v", name, err)
	}
	wPol := doCompatJSON(t, srv.Handler, adminToken, http.MethodPost, "/emby/Users/"+created.Id+"/Policy", policy, nil)
	if wPol.Code != http.StatusNoContent {
		t.Fatalf("update policy for %s failed: %d %s", name, wPol.Code, wPol.Body.String())
	}
	tok, uid := loginUser(t, srv.Handler, name, pw, "dev-"+name)
	return tok, uid
}

// TestDualUserHorizontalIDORAndUserPurgeMatrix verifies cross-user isolation end-to-end:
// - POST /Sessions/{Id}/Users/{UserB} -> 403
// - Private playlist isolation in GET /Items and GET /Playlists/{Id}/Items
// - Watch Party restricted item isolation (POST /Parties -> 403 & GET /Parties hides restricted parties)
// - SyncJob isolation & PurgeUser cleanup across notifications, parties, and sync jobs on DELETE /Users/{Id}
func TestDualUserHorizontalIDORAndUserPurgeMatrix(t *testing.T) {
	srv, _, adminToken, _ := newCompatTestServer(t)
	ctx := context.Background()

	libRepo := sqlite.NewLibraryRepo(srv.Store)
	moviesDir := t.TempDir()
	adultDir := t.TempDir()
	moviesLib, err := libRepo.CreateLibrary(ctx, "SharedMovies", "movies", []string{moviesDir})
	if err != nil {
		t.Fatal(err)
	}
	adultLib, err := libRepo.CreateLibrary(ctx, "VaultMovies", "movies", []string{adultDir})
	if err != nil {
		t.Fatal(err)
	}

	sharedFile := filepath.Join(moviesDir, "shared.mkv")
	vaultFile := filepath.Join(adultDir, "vault.mkv")
	_ = os.WriteFile(sharedFile, []byte("shared-video-data"), 0o644)
	_ = os.WriteFile(vaultFile, []byte("vault-video-data"), 0o644)

	sharedItem := &sqlite.ItemRecord{
		ID:             guid.New(),
		LibraryID:      moviesLib.ID,
		ParentID:       moviesLib.ID,
		Type:           "Movie",
		MediaType:      "Video",
		Name:           "Shared Adventure",
		Path:           sharedFile,
		Container:      "mkv",
		OfficialRating: "PG",
	}
	vaultItem := &sqlite.ItemRecord{
		ID:             guid.New(),
		LibraryID:      adultLib.ID,
		ParentID:       adultLib.ID,
		Type:           "Movie",
		MediaType:      "Video",
		Name:           "Restricted Vault Film",
		Path:           vaultFile,
		Container:      "mkv",
		OfficialRating: "R",
	}
	for _, it := range []*sqlite.ItemRecord{sharedItem, vaultItem} {
		if err := libRepo.UpsertItem(ctx, it); err != nil {
			t.Fatal(err)
		}
	}

	// User A has access to both libraries + content downloading; User B only has SharedMovies
	userAToken, userAID := createCompatNonAdminUser(t, srv, adminToken, "idor_user_a", "passA1234", dto.UserPolicy{
		EnableAllFolders:         false,
		EnabledFolders:           []string{moviesLib.ID, adultLib.ID},
		EnableMediaPlayback:      true,
		EnableContentDownloading: true,
		EnableRemoteAccess:       true,
	})
	userBToken, userBID := createCompatNonAdminUser(t, srv, adminToken, "idor_user_b", "passB1234", dto.UserPolicy{
		EnableAllFolders:         false,
		EnabledFolders:           []string{moviesLib.ID},
		EnableMediaPlayback:      true,
		EnableContentDownloading: true,
		EnableRemoteAccess:       true,
	})

	// Ensure active session exists for User A
	wSess := doCompatJSON(t, srv.Handler, userAToken, http.MethodGet, "/emby/Sessions", nil, nil)
	if wSess.Code != http.StatusOK {
		t.Fatalf("GET /Sessions as User A: %d", wSess.Code)
	}
	var userASessions []dto.SessionInfo
	_ = json.NewDecoder(wSess.Body).Decode(&userASessions)
	if len(userASessions) == 0 {
		t.Fatal("expected User A session in /Sessions")
	}
	userASessionID := userASessions[0].Id

	// 1. Session AdditionalUsers horizontal IDOR: User A cannot attach User B -> 403
	wAddSessUser := doCompatJSON(t, srv.Handler, userAToken, http.MethodPost, "/emby/Sessions/"+userASessionID+"/Users/"+userBID, nil, nil)
	if wAddSessUser.Code != http.StatusForbidden {
		t.Fatalf("expected 403 when User A attaches User B to session, got %d %s", wAddSessUser.Code, wAddSessUser.Body.String())
	}

	// 2. Private playlist isolation in GET /Items & GET /Playlists/{Id}/Items
	wPl := doCompatJSON(t, srv.Handler, userAToken, http.MethodPost, "/emby/Playlists", map[string]any{
		"Name":      "UserA Secret Playlist",
		"MediaType": "Video",
		"UserId":    userAID,
		"Ids":       []string{sharedItem.ID},
	}, nil)
	if wPl.Code != http.StatusOK {
		t.Fatalf("User A create playlist: %d %s", wPl.Code, wPl.Body.String())
	}
	var plCreated struct {
		Id string `json:"Id"`
	}
	_ = json.NewDecoder(wPl.Body).Decode(&plCreated)

	wItemsA := doCompatJSON(t, srv.Handler, userAToken, http.MethodGet, "/emby/Items?IncludeItemTypes=Playlist&Recursive=true", nil, nil)
	var itemsResA dto.QueryResult[dto.BaseItemDto]
	_ = json.NewDecoder(wItemsA.Body).Decode(&itemsResA)
	if itemsResA.TotalRecordCount != 1 || len(itemsResA.Items) != 1 || itemsResA.Items[0].Id != plCreated.Id {
		t.Fatalf("expected User A to see own playlist in GET /Items, got %+v", itemsResA)
	}

	wItemsB := doCompatJSON(t, srv.Handler, userBToken, http.MethodGet, "/emby/Items?IncludeItemTypes=Playlist&Recursive=true", nil, nil)
	var itemsResB dto.QueryResult[dto.BaseItemDto]
	_ = json.NewDecoder(wItemsB.Body).Decode(&itemsResB)
	for _, it := range itemsResB.Items {
		if it.Id == plCreated.Id {
			t.Fatalf("private playlist IDOR leak: User B saw User A's playlist %s in GET /Items", plCreated.Id)
		}
	}
	wPlDirectB := doCompatJSON(t, srv.Handler, userBToken, http.MethodGet, "/emby/Playlists/"+plCreated.Id+"/Items", nil, nil)
	if wPlDirectB.Code != http.StatusForbidden && wPlDirectB.Code != http.StatusNotFound {
		t.Fatalf("expected 403/404 when User B reads User A's private playlist items, got %d", wPlDirectB.Code)
	}

	// 3. Watch Party restricted item isolation (M-04 wired via server.New)
	isPub := true
	// User B cannot create a party referencing vaultItem -> 403
	wPartyBlocked := doCompatJSON(t, srv.Handler, userBToken, http.MethodPost, "/emby/Parties", map[string]any{
		"Name":     "Sneaky Vault Room",
		"ItemId":   vaultItem.ID,
		"IsPublic": isPub,
	}, nil)
	if wPartyBlocked.Code != http.StatusForbidden {
		t.Fatalf("expected 403 when User B creates Watch Party on restricted item, got %d %s", wPartyBlocked.Code, wPartyBlocked.Body.String())
	}

	// User A creates a public party playing vaultItem -> 200, but User B listing GET /Parties must NOT see it!
	wPartyVault := doCompatJSON(t, srv.Handler, userAToken, http.MethodPost, "/emby/Parties", map[string]any{
		"Name":     "UserA Vault Room",
		"ItemId":   vaultItem.ID,
		"IsPublic": isPub,
	}, nil)
	if wPartyVault.Code != http.StatusOK {
		t.Fatalf("User A create party on vaultItem: %d %s", wPartyVault.Code, wPartyVault.Body.String())
	}
	var partyA struct {
		Id string `json:"Id"`
	}
	_ = json.NewDecoder(wPartyVault.Body).Decode(&partyA)

	wListPartiesB := doCompatJSON(t, srv.Handler, userBToken, http.MethodGet, "/emby/Parties", nil, nil)
	if wListPartiesB.Code != http.StatusOK {
		t.Fatalf("User B GET /Parties: %d", wListPartiesB.Code)
	}
	var partiesForB []struct {
		Id string `json:"Id"`
	}
	_ = json.NewDecoder(wListPartiesB.Body).Decode(&partiesForB)
	for _, p := range partiesForB {
		if p.Id == partyA.Id {
			t.Fatalf("Watch Party restricted item leak: User B saw party %s playing vaultItem", partyA.Id)
		}
	}

	// 4. SyncJob isolation & PurgeUser on DELETE /Users/{Id} (M-11 wired via server.New)
	wSyncA := doCompatJSON(t, srv.Handler, userAToken, http.MethodPost, "/emby/Sync/Jobs", map[string]any{
		"TargetId": "dev-a",
		"Name":     "UserA Offline Sync",
		"Quality":  "original",
		"ItemIds":  []string{sharedItem.ID},
	}, nil)
	if wSyncA.Code != http.StatusOK {
		t.Fatalf("User A create sync job: %d %s", wSyncA.Code, wSyncA.Body.String())
	}
	var syncResA struct {
		Job struct {
			Id string `json:"Id"`
		} `json:"Job"`
	}
	_ = json.NewDecoder(wSyncA.Body).Decode(&syncResA)

	wSyncB := doCompatJSON(t, srv.Handler, userBToken, http.MethodPost, "/emby/Sync/Jobs", map[string]any{
		"TargetId": "dev-b",
		"Name":     "UserB Offline Sync",
		"Quality":  "original",
		"ItemIds":  []string{sharedItem.ID},
	}, nil)
	if wSyncB.Code != http.StatusOK {
		t.Fatalf("User B create sync job: %d %s", wSyncB.Code, wSyncB.Body.String())
	}
	var syncResB struct {
		Job struct {
			Id string `json:"Id"`
		} `json:"Job"`
	}
	_ = json.NewDecoder(wSyncB.Body).Decode(&syncResB)

	// User B listing /Sync/Jobs sees only User B's job, and cannot delete User A's job
	wJobsB := doCompatJSON(t, srv.Handler, userBToken, http.MethodGet, "/emby/Sync/Jobs", nil, nil)
	var jobsListB struct {
		Items []struct {
			Id string `json:"Id"`
		} `json:"Items"`
	}
	_ = json.NewDecoder(wJobsB.Body).Decode(&jobsListB)
	if len(jobsListB.Items) != 1 || jobsListB.Items[0].Id != syncResB.Job.Id {
		t.Fatalf("SyncJob isolation violated for User B: %+v", jobsListB.Items)
	}
	if wDelCross := doCompatJSON(t, srv.Handler, userBToken, http.MethodDelete, "/emby/Sync/Jobs/"+syncResA.Job.Id, nil, nil); wDelCross.Code != http.StatusForbidden {
		t.Fatalf("expected 403 when User B deletes User A's SyncJob, got %d", wDelCross.Code)
	}

	// Admin deletes User A -> PurgeUser cleans User A's Watch Party & SyncJob while keeping User B's SyncJob intact!
	if wDelUserA := doCompatJSON(t, srv.Handler, adminToken, http.MethodDelete, "/emby/Users/"+userAID, nil, nil); wDelUserA.Code != http.StatusNoContent {
		t.Fatalf("DELETE /Users/%s failed: %d %s", userAID, wDelUserA.Code, wDelUserA.Body.String())
	}

	wJobsAdmin := doCompatJSON(t, srv.Handler, adminToken, http.MethodGet, "/emby/Sync/Jobs", nil, nil)
	var jobsListAdmin struct {
		Items []struct {
			Id string `json:"Id"`
		} `json:"Items"`
	}
	_ = json.NewDecoder(wJobsAdmin.Body).Decode(&jobsListAdmin)
	for _, j := range jobsListAdmin.Items {
		if j.Id == syncResA.Job.Id {
			t.Fatalf("expected User A's SyncJob %s purged upon DELETE /Users/%s", syncResA.Job.Id, userAID)
		}
	}
	if len(jobsListAdmin.Items) != 1 || jobsListAdmin.Items[0].Id != syncResB.Job.Id {
		t.Fatalf("expected User B's SyncJob %s preserved after purging User A, got %+v", syncResB.Job.Id, jobsListAdmin.Items)
	}

	wPartiesAdmin := doCompatJSON(t, srv.Handler, adminToken, http.MethodGet, "/emby/Parties", nil, nil)
	var partiesAdmin []struct {
		Id string `json:"Id"`
	}
	_ = json.NewDecoder(wPartiesAdmin.Body).Decode(&partiesAdmin)
	for _, p := range partiesAdmin {
		if p.Id == partyA.Id {
			t.Fatalf("expected User A's Watch Party %s purged upon DELETE /Users/%s", partyA.Id, userAID)
		}
	}
}

// TestParentalLiveTVAndPlaybackPolicyMatrix verifies BlockUnratedItems, AllowedTags,
// EnableLiveTvAccess=false on CollectionType=="livetv", and EnableMediaPlayback=false across endpoints.
func TestParentalLiveTVAndPlaybackPolicyMatrix(t *testing.T) {
	srv, _, adminToken, _ := newCompatTestServer(t)
	ctx := context.Background()

	libRepo := sqlite.NewLibraryRepo(srv.Store)
	moviesDir := t.TempDir()
	dvrDir := t.TempDir()
	moviesLib, err := libRepo.CreateLibrary(ctx, "FamilyMovies", "movies", []string{moviesDir})
	if err != nil {
		t.Fatal(err)
	}
	dvrLib, err := libRepo.CreateLibrary(ctx, "LiveTVRecordings", "livetv", []string{dvrDir})
	if err != nil {
		t.Fatal(err)
	}

	mkMedia := func(dir, name string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte("0123456789abcdef-media-stream"), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}

	safeMovie := &sqlite.ItemRecord{
		ID:             guid.New(),
		LibraryID:      moviesLib.ID,
		ParentID:       moviesLib.ID,
		Type:           "Movie",
		MediaType:      "Video",
		Name:           "Approved Kids Movie",
		Path:           mkMedia(moviesDir, "approved.mkv"),
		Container:      "mkv",
		OfficialRating: "PG",
	}
	unratedMovie := &sqlite.ItemRecord{
		ID:             guid.New(),
		LibraryID:      moviesLib.ID,
		ParentID:       moviesLib.ID,
		Type:           "Movie",
		MediaType:      "Video",
		Name:           "Unrated Mystery",
		Path:           mkMedia(moviesDir, "unrated.mkv"),
		Container:      "mkv",
		OfficialRating: "",
	}
	untaggedMovie := &sqlite.ItemRecord{
		ID:             guid.New(),
		LibraryID:      moviesLib.ID,
		ParentID:       moviesLib.ID,
		Type:           "Movie",
		MediaType:      "Video",
		Name:           "Rated Without Allowed Tag",
		Path:           mkMedia(moviesDir, "untagged.mkv"),
		Container:      "mkv",
		OfficialRating: "PG",
	}
	dvrRecording := &sqlite.ItemRecord{
		ID:             guid.New(),
		LibraryID:      dvrLib.ID,
		ParentID:       dvrLib.ID,
		Type:           "Video",
		MediaType:      "Video",
		Name:           "Late Night Broadcast",
		Path:           mkMedia(dvrDir, "broadcast.ts"),
		Container:      "ts",
		OfficialRating: "G",
	}
	audioTrack := &sqlite.ItemRecord{
		ID:             guid.New(),
		LibraryID:      moviesLib.ID,
		ParentID:       moviesLib.ID,
		Type:           "Audio",
		MediaType:      "Audio",
		Name:           "Theme Track",
		Path:           mkMedia(moviesDir, "theme.mp3"),
		Container:      "mp3",
		OfficialRating: "G",
	}
	for _, it := range []*sqlite.ItemRecord{safeMovie, unratedMovie, untaggedMovie, dvrRecording, audioTrack} {
		if err := libRepo.UpsertItem(ctx, it); err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []string{safeMovie.ID, unratedMovie.ID, dvrRecording.ID, audioTrack.ID} {
		if err := libRepo.ReplaceItemTaxonomies(ctx, id, "tag", []string{"kid-approved"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := libRepo.ReplaceItemTaxonomies(ctx, untaggedMovie.ID, "tag", []string{"other-tag"}); err != nil {
		t.Fatal(err)
	}

	kidToken, kidID := createCompatNonAdminUser(t, srv, adminToken, "policy_kid", "kidpw1234", dto.UserPolicy{
		EnableAllFolders:         true,
		EnableLiveTvAccess:       false,
		EnableMediaPlayback:      true,
		EnableAudioPlaybackTranscoding: true,
		EnableVideoPlaybackTranscoding: true,
		EnableContentDownloading: true,
		EnableRemoteAccess:       true,
		BlockUnratedItems:        []string{"Movie"},
		AllowedTags:              []string{"kid-approved"},
	})

	// 1. /Views hides CollectionType=="livetv" library even when EnableAllFolders==true
	wViews := doCompatJSON(t, srv.Handler, kidToken, http.MethodGet, "/emby/Users/"+kidID+"/Views", nil, nil)
	if wViews.Code != http.StatusOK {
		t.Fatalf("GET /Views: %d", wViews.Code)
	}
	var views dto.QueryResult[dto.BaseItemDto]
	_ = json.NewDecoder(wViews.Body).Decode(&views)
	if views.TotalRecordCount != 1 || len(views.Items) != 1 || views.Items[0].Id != moviesLib.ID {
		t.Fatalf("expected only FamilyMovies view (livetv excluded), got %+v", views)
	}

	// 2. /Items enforces BlockUnratedItems + AllowedTags + EnableLiveTvAccess=false
	wItems := doCompatJSON(t, srv.Handler, kidToken, http.MethodGet, "/emby/Items?IncludeItemTypes=Movie,Video&Recursive=true", nil, nil)
	if wItems.Code != http.StatusOK {
		t.Fatalf("GET /Items: %d", wItems.Code)
	}
	var items dto.QueryResult[dto.BaseItemDto]
	_ = json.NewDecoder(wItems.Body).Decode(&items)
	if items.TotalRecordCount != 1 || len(items.Items) != 1 || items.Items[0].Id != safeMovie.ID {
		t.Fatalf("expected only safeMovie in /Items, got %+v", items)
	}

	// 3. /Videos/{Id}/stream allows safeMovie (200) and blocks unrated, untagged, and livetv DVR items (403)
	if w := doCompatJSON(t, srv.Handler, kidToken, http.MethodGet, "/emby/Videos/"+safeMovie.ID+"/stream?Static=true", nil, nil); w.Code != http.StatusOK {
		t.Fatalf("expected 200 streaming safeMovie, got %d", w.Code)
	}
	for _, blockedID := range []string{unratedMovie.ID, untaggedMovie.ID, dvrRecording.ID} {
		if w := doCompatJSON(t, srv.Handler, kidToken, http.MethodGet, "/emby/Videos/"+blockedID+"/stream?Static=true", nil, nil); w.Code != http.StatusForbidden {
			t.Fatalf("expected 403 streaming blocked item %s, got %d", blockedID, w.Code)
		}
	}

	// 4. /Sync/Jobs allows safeMovie (200) and blocks unrated, untagged, DVR item, and DVR ParentId (403)
	if w := doCompatJSON(t, srv.Handler, kidToken, http.MethodPost, "/emby/Sync/Jobs", map[string]any{
		"TargetId": "kid-tablet",
		"Quality":  "original",
		"ItemIds":  []string{safeMovie.ID},
	}, nil); w.Code != http.StatusOK {
		t.Fatalf("expected 200 syncing safeMovie, got %d %s", w.Code, w.Body.String())
	}
	for _, blockedID := range []string{unratedMovie.ID, untaggedMovie.ID, dvrRecording.ID} {
		if w := doCompatJSON(t, srv.Handler, kidToken, http.MethodPost, "/emby/Sync/Jobs", map[string]any{
			"TargetId": "kid-tablet",
			"Quality":  "original",
			"ItemIds":  []string{blockedID},
		}, nil); w.Code != http.StatusForbidden {
			t.Fatalf("expected 403 syncing blocked item %s, got %d %s", blockedID, w.Code, w.Body.String())
		}
	}
	if w := doCompatJSON(t, srv.Handler, kidToken, http.MethodPost, "/emby/Sync/Jobs", map[string]any{
		"TargetId": "kid-tablet",
		"Quality":  "original",
		"ParentId": dvrLib.ID,
	}, nil); w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 syncing livetv ParentId %s, got %d", dvrLib.ID, w.Code)
	}

	// 5. EnableMediaPlayback=false returns 403 across video, HLS, PlaybackInfo, and audio stream endpoints
	noPlayToken, noPlayID := createCompatNonAdminUser(t, srv, adminToken, "no_playback_user", "nopw1234", dto.UserPolicy{
		EnableAllFolders:    true,
		EnableMediaPlayback: false,
		EnableRemoteAccess:  true,
	})
	playbackEndpoints := []struct {
		method string
		path   string
	}{
		{http.MethodPost, "/emby/Items/" + safeMovie.ID + "/PlaybackInfo?UserId=" + noPlayID},
		{http.MethodGet, "/emby/Videos/" + safeMovie.ID + "/stream?Static=true"},
		{http.MethodGet, "/emby/Videos/" + safeMovie.ID + "/master.m3u8"},
		{http.MethodGet, "/emby/Audio/" + audioTrack.ID + "/stream?Static=true"},
		{http.MethodGet, "/emby/Audio/" + audioTrack.ID + "/universal"},
	}
	for _, ep := range playbackEndpoints {
		w := doCompatJSON(t, srv.Handler, noPlayToken, ep.method, ep.path, nil, nil)
		if w.Code != http.StatusForbidden {
			t.Fatalf("EnableMediaPlayback=false on %s %s: expected 403, got %d %s", ep.method, ep.path, w.Code, w.Body.String())
		}
	}
}

// TestDynamicCORSAndSetupOriginEnforcement verifies H-01 dynamic CORS hot-reload via
// POST /System/Configuration and cross-origin rejection on /api/setup/admin.
func TestDynamicCORSAndSetupOriginEnforcement(t *testing.T) {
	srv, _, adminToken, _ := newCompatTestServer(t)

	// 1. Before allowlisting, cross-origin request gets no Access-Control-Allow-Origin
	wBefore := doCompatJSON(t, srv.Handler, "", http.MethodGet, "/emby/System/Info/Public", nil, map[string]string{
		"Origin": "https://portal.ambra.example",
	})
	if got := wBefore.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("expected empty Access-Control-Allow-Origin before allowlisting, got %q", got)
	}

	// 2. Update AllowedOrigins via POST /System/Configuration -> immediately reflected without restart
	wUpdate := doCompatJSON(t, srv.Handler, adminToken, http.MethodPost, "/emby/System/Configuration", map[string]any{
		"AllowedOrigins": []string{"https://portal.ambra.example"},
	}, nil)
	if wUpdate.Code != http.StatusNoContent {
		t.Fatalf("POST /System/Configuration AllowedOrigins: %d %s", wUpdate.Code, wUpdate.Body.String())
	}

	wAllowed := doCompatJSON(t, srv.Handler, "", http.MethodGet, "/emby/System/Info/Public", nil, map[string]string{
		"Origin": "https://portal.ambra.example",
	})
	if got := wAllowed.Header().Get("Access-Control-Allow-Origin"); got != "https://portal.ambra.example" {
		t.Fatalf("expected dynamic Access-Control-Allow-Origin=https://portal.ambra.example, got %q", got)
	}
	if cred := wAllowed.Header().Get("Access-Control-Allow-Credentials"); cred != "true" {
		t.Fatalf("expected Access-Control-Allow-Credentials=true for explicit origin, got %q", cred)
	}

	wEvil := doCompatJSON(t, srv.Handler, "", http.MethodGet, "/emby/System/Info/Public", nil, map[string]string{
		"Origin": "https://evil.example",
	})
	if got := wEvil.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("expected evil origin rejected by CORS, got %q", got)
	}

	// 3. Fresh unconfigured server rejects cross-origin POST /api/setup/admin with 403
	t.Setenv("AMBRA_SKIP_DEFAULT_ADMIN", "1")
	freshCfg, err := config.Load(filepath.Join(t.TempDir(), "fresh-data"))
	if err != nil {
		t.Fatal(err)
	}
	freshSrv, err := server.New(context.Background(), freshCfg)
	if err != nil {
		t.Fatal(err)
	}
	defer freshSrv.Close()

	evilReq := httptest.NewRequest(http.MethodPost, "http://media.local:8096/api/setup/admin", strings.NewReader(`{
		"server_name": "Owned",
		"username": "attacker",
		"password": "attacker-password"
	}`))
	evilReq.Host = "media.local:8096"
	evilReq.RemoteAddr = "127.0.0.1:40001"
	evilReq.Header.Set("Content-Type", "application/json")
	evilReq.Header.Set("Origin", "https://evil.example")
	evilW := httptest.NewRecorder()
	freshSrv.Handler.ServeHTTP(evilW, evilReq)
	if evilW.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for cross-origin POST /api/setup/admin, got %d %s", evilW.Code, evilW.Body.String())
	}

	// Same-origin POST /api/setup/admin succeeds with 200
	sameOriginReq := httptest.NewRequest(http.MethodPost, "http://media.local:8096/api/setup/admin", strings.NewReader(`{
		"server_name": "Home Ambra",
		"username": "owner",
		"password": "owner-password"
	}`))
	sameOriginReq.Host = "media.local:8096"
	sameOriginReq.RemoteAddr = "127.0.0.1:40002"
	sameOriginReq.Header.Set("Content-Type", "application/json")
	sameOriginReq.Header.Set("Origin", "http://media.local:8096")
	sameW := httptest.NewRecorder()
	freshSrv.Handler.ServeHTTP(sameW, sameOriginReq)
	if sameW.Code != http.StatusOK {
		t.Fatalf("expected 200 for same-origin POST /api/setup/admin, got %d %s", sameW.Code, sameW.Body.String())
	}
}

// TestPlaylistDeleteInfoAndPluginDiskUninstallContracts verifies schema & write-then-read assertions:
// - POST /Playlists/{Id}/Items -> 200 with ItemAddedCount
// - GET /Items/{Id}/DeleteInfo -> 200 with Paths
// - POST /Plugins/{Id}/Delete -> removes external plugin bundle from <DataDir>/plugins/<dir>
func TestPlaylistDeleteInfoAndPluginDiskUninstallContracts(t *testing.T) {
	t.Setenv("AMBRA_ADMIN_USER", "admin")
	t.Setenv("AMBRA_ADMIN_PASS", "admin")
	dataDir := filepath.Join(t.TempDir(), "data")

	// Seed an external HTTP plugin bundle in <DataDir>/plugins/custom-ext-sub before server.New
	pluginBundleDir := filepath.Join(dataDir, "plugins", "custom-ext-sub")
	if err := os.MkdirAll(pluginBundleDir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifestJSON := `{
		"Id": "custom-ext-sub",
		"Name": "Custom External Subtitle Plugin",
		"Version": "1.2.3",
		"Endpoint": "https://93.184.216.34/plugin",
		"Enabled": true,
		"Priority": 40,
		"Kinds": ["subtitle"]
	}`
	if err := os.WriteFile(filepath.Join(pluginBundleDir, "manifest.json"), []byte(manifestJSON), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := config.Load(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	srv, err := server.New(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	if p, ok := srv.Registry.Get("ambra.builtin.tmdb"); ok {
		_ = p.Configure(map[string]any{"enabled": false})
	}

	adminToken, adminID := loginUser(t, srv.Handler, "admin", "admin", "dev-contracts")

	// Seed 2 real media files inside a registered library
	mediaDir := filepath.Join(t.TempDir(), "contract-movies")
	m1Dir := filepath.Join(mediaDir, "Movie One (2023)")
	m2Dir := filepath.Join(mediaDir, "Movie Two (2024)")
	_ = os.MkdirAll(m1Dir, 0o755)
	_ = os.MkdirAll(m2Dir, 0o755)
	m1Path := filepath.Join(m1Dir, "Movie One (2023).mkv")
	m2Path := filepath.Join(m2Dir, "Movie Two (2024).mkv")
	_ = os.WriteFile(m1Path, []byte("mkv-bytes-1"), 0o644)
	_ = os.WriteFile(m2Path, []byte("mkv-bytes-2"), 0o644)

	wAddLib := doCompatJSON(t, srv.Handler, adminToken, http.MethodPost, "/emby/Library/VirtualFolders?name=ContractMovies&collectionType=movies", map[string]any{
		"Name":           "ContractMovies",
		"CollectionType": "movies",
		"Paths":          []string{mediaDir},
	}, nil)
	if wAddLib.Code != http.StatusOK && wAddLib.Code != http.StatusNoContent {
		t.Fatalf("create library: %d %s", wAddLib.Code, wAddLib.Body.String())
	}
	if _, err := srv.Scanner.ScanAll(context.Background()); err != nil {
		t.Fatalf("ScanAll: %v", err)
	}

	wMovies := doCompatJSON(t, srv.Handler, adminToken, http.MethodGet, "/emby/Users/"+adminID+"/Items?IncludeItemTypes=Movie&SortBy=SortName", nil, nil)
	var movieList dto.QueryResult[dto.BaseItemDto]
	_ = json.NewDecoder(wMovies.Body).Decode(&movieList)
	if len(movieList.Items) != 2 {
		t.Fatalf("expected 2 scanned movies, got %d", len(movieList.Items))
	}
	id1, id2 := movieList.Items[0].Id, movieList.Items[1].Id

	// 1. POST /Playlists/{Id}/Items -> 200 OK with {"ItemAddedCount": 2} + write-then-read verification
	wCreatePl := doCompatJSON(t, srv.Handler, adminToken, http.MethodPost, "/emby/Playlists", map[string]any{
		"Name":      "Schema Check Playlist",
		"MediaType": "Video",
		"UserId":    adminID,
	}, nil)
	if wCreatePl.Code != http.StatusOK {
		t.Fatalf("POST /Playlists: %d %s", wCreatePl.Code, wCreatePl.Body.String())
	}
	var plCreated struct {
		Id string `json:"Id"`
	}
	_ = json.NewDecoder(wCreatePl.Body).Decode(&plCreated)

	wAddItems := doCompatJSON(t, srv.Handler, adminToken, http.MethodPost, "/emby/Playlists/"+plCreated.Id+"/Items", map[string]any{
		"Ids": []string{id1, id2},
	}, nil)
	if wAddItems.Code != http.StatusOK {
		t.Fatalf("POST /Playlists/{Id}/Items expected 200, got %d %s", wAddItems.Code, wAddItems.Body.String())
	}
	var addItemsRes struct {
		ItemAddedCount int `json:"ItemAddedCount"`
	}
	if err := json.NewDecoder(wAddItems.Body).Decode(&addItemsRes); err != nil || addItemsRes.ItemAddedCount != 2 {
		t.Fatalf("expected ItemAddedCount=2, got %+v err=%v", addItemsRes, err)
	}

	wReadPl := doCompatJSON(t, srv.Handler, adminToken, http.MethodGet, "/emby/Playlists/"+plCreated.Id+"/Items", nil, nil)
	var plItems dto.QueryResult[dto.BaseItemDto]
	_ = json.NewDecoder(wReadPl.Body).Decode(&plItems)
	if plItems.TotalRecordCount != 2 || len(plItems.Items) != 2 {
		t.Fatalf("write-then-read GET /Playlists/{Id}/Items expected 2 items, got %+v", plItems)
	}

	// 2. GET /Items/{Id}/DeleteInfo -> 200 OK with Paths containing physical file path
	wDelInfo := doCompatJSON(t, srv.Handler, adminToken, http.MethodGet, "/emby/Items/"+id1+"/DeleteInfo", nil, nil)
	if wDelInfo.Code != http.StatusOK {
		t.Fatalf("GET /Items/{Id}/DeleteInfo expected 200, got %d %s", wDelInfo.Code, wDelInfo.Body.String())
	}
	var delInfo struct {
		Paths []string `json:"Paths"`
	}
	if err := json.NewDecoder(wDelInfo.Body).Decode(&delInfo); err != nil || len(delInfo.Paths) == 0 {
		t.Fatalf("expected non-empty Paths in DeleteInfo, got %+v err=%v", delInfo, err)
	}
	matchedPath := false
	for _, p := range delInfo.Paths {
		if strings.Contains(p, "Movie One (2023)") {
			matchedPath = true
			break
		}
	}
	if !matchedPath {
		t.Fatalf("expected DeleteInfo.Paths to reference Movie One (2023), got %v", delInfo.Paths)
	}

	// 3. POST /Plugins/{Id}/Delete removes external plugin bundle from <DataDir>/plugins/<dir>
	wPluginsBefore := doCompatJSON(t, srv.Handler, adminToken, http.MethodGet, "/emby/Plugins", nil, nil)
	if wPluginsBefore.Code != http.StatusOK || !strings.Contains(wPluginsBefore.Body.String(), "custom-ext-sub") {
		t.Fatalf("expected custom-ext-sub listed in GET /Plugins before uninstall: %s", wPluginsBefore.Body.String())
	}

	wDelPlugin := doCompatJSON(t, srv.Handler, adminToken, http.MethodPost, "/emby/Plugins/custom-ext-sub/Delete", nil, nil)
	if wDelPlugin.Code != http.StatusNoContent {
		t.Fatalf("POST /Plugins/custom-ext-sub/Delete expected 204, got %d %s", wDelPlugin.Code, wDelPlugin.Body.String())
	}

	if _, err := os.Stat(pluginBundleDir); !os.IsNotExist(err) {
		t.Fatalf("expected external plugin directory %s removed from disk, stat err=%v", pluginBundleDir, err)
	}
	wPluginsAfter := doCompatJSON(t, srv.Handler, adminToken, http.MethodGet, "/emby/Plugins", nil, nil)
	if wPluginsAfter.Code != http.StatusOK || strings.Contains(wPluginsAfter.Body.String(), "custom-ext-sub") {
		t.Fatalf("expected custom-ext-sub unregistered from GET /Plugins after uninstall: %s", wPluginsAfter.Body.String())
	}
}
