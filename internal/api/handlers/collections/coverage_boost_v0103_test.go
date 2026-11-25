package collections

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ambra-media/ambra/internal/api/dto"
	"github.com/ambra-media/ambra/internal/api/middleware"
	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/core/auth"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

type ctxKey string

const testUserCtxKey ctxKey = "uid"

func TestCollectionsCoverageBoostV0103(t *testing.T) {
	tmp := t.TempDir()
	cfg, err := config.Load(filepath.Join(tmp, "data"))
	if err != nil {
		t.Fatal(err)
	}
	store, err := sqlite.Open(cfg.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	ctx := context.Background()
	repo := sqlite.NewLibraryRepo(store)
	userRepo := sqlite.NewUserRepo(store)
	authSvc := auth.NewService(userRepo, cfg)

	admin, err := authSvc.CreateUser(ctx, "admin", "admin123", true)
	if err != nil {
		t.Fatal(err)
	}
	alice, err := authSvc.CreateUser(ctx, "alice", "alice123", false)
	if err != nil {
		t.Fatal(err)
	}
	bob, err := authSvc.CreateUser(ctx, "bob", "bob123", false)
	if err != nil {
		t.Fatal(err)
	}

	libSafe, _ := repo.CreateLibrary(ctx, "Safe", "tvshows", []string{"/safe"})

	ap := alice.Policy
	ap.EnableAllFolders = false
	ap.EnabledFolders = []string{libSafe.ID}
	maxPG := 5
	ap.MaxParentalRating = &maxPG
	_ = userRepo.UpdateUserPolicy(ctx, alice.ID, ap)
	authSvc.InvalidateUserCache(alice.ID)

	// Series rated R + Episode with empty OfficialRating (must inherit R rating!)
	rSeries := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: libSafe.ID, ParentID: libSafe.ID,
		Type: "Series", MediaType: "Video", Name: "Adult Show", OfficialRating: "R",
	}
	rEpisode := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: libSafe.ID, ParentID: rSeries.ID, SeriesID: rSeries.ID,
		Type: "Episode", MediaType: "Video", Name: "Ep1", Path: "/safe/ep1.mkv",
	}
	safeEp1 := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: libSafe.ID, ParentID: libSafe.ID,
		Type: "Movie", MediaType: "Video", Name: "Safe 1", Path: "/safe/s1.mkv", OfficialRating: "G",
	}
	safeEp2 := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: libSafe.ID, ParentID: libSafe.ID,
		Type: "Movie", MediaType: "Video", Name: "Safe 2", Path: "/safe/s2.mkv", OfficialRating: "G",
	}
	safeEp3 := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: libSafe.ID, ParentID: libSafe.ID,
		Type: "Movie", MediaType: "Video", Name: "Safe 3", Path: "/safe/s3.mkv", OfficialRating: "G",
	}
	for _, it := range []*sqlite.ItemRecord{rSeries, rEpisode, safeEp1, safeEp2, safeEp3} {
		if err := repo.UpsertItem(ctx, it); err != nil {
			t.Fatal(err)
		}
	}
	// Add media source + media stream with physical paths to safeEp1 to verify path stripping
	msID := guid.New()
	if err := repo.ReplaceMediaSourcesAndStreams(ctx, safeEp1.ID, []sqlite.MediaSourceRecord{{
		ID: msID, ItemID: safeEp1.ID, Path: "/safe/s1.mkv", Container: "mkv",
		Streams: []sqlite.MediaStreamRecord{
			{MediaSourceID: msID, ItemID: safeEp1.ID, StreamIndex: 0, StreamType: "Video", Codec: "h264"},
			{MediaSourceID: msID, ItemID: safeEp1.ID, StreamIndex: 1, StreamType: "Subtitle", Codec: "srt", IsExternal: true, Path: "/safe/s1.en.srt"},
		},
	}}); err != nil {
		t.Fatal(err)
	}

	col, err := repo.CreateCollection(ctx, "Mixed", "", []string{safeEp1.ID, rEpisode.ID, safeEp2.ID, safeEp3.ID})
	if err != nil {
		t.Fatal(err)
	}

	// Create 3 playlists owned by alice + 1 owned by bob (public) + 1 owned by bob (private with corrupt visibility JSON)
	pl1, _ := repo.CreatePlaylist(ctx, "Alice PL 1", "Video", alice.ID, []string{safeEp1.ID})
	pl2, _ := repo.CreatePlaylist(ctx, "Alice PL 2", "Video", alice.ID, []string{safeEp2.ID})
	pl3, _ := repo.CreatePlaylist(ctx, "Alice PL 3", "Video", alice.ID, []string{safeEp3.ID})
	plBobPub, _ := repo.CreatePlaylist(ctx, "Bob Public", "Video", bob.ID, nil)
	plBobCorrupt, _ := repo.CreatePlaylist(ctx, "Bob Corrupt", "Video", bob.ID, nil)
	_, err = store.Writer.ExecContext(ctx, `INSERT OR REPLACE INTO server_config(key, value_json, updated_at) VALUES(?, ?, 1)`,
		"item_visibility_"+guid.Normalize(plBobPub.ID), `{"IsPublic":true}`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.Writer.ExecContext(ctx, `INSERT OR REPLACE INTO server_config(key, value_json, updated_at) VALUES(?, ?, 1)`,
		"item_visibility_"+guid.Normalize(plBobCorrupt.ID), `{not-valid-json`)
	if err != nil {
		t.Fatal(err)
	}

	h := New(repo, cfg)
	h.SetUserRepo(userRepo)
	h.SetStore(store)
	authMW := middleware.NewAuth(authSvc, nil)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux, AuthFuncs{
		RequireAuth:  authMW.RequireAuth,
		RequireAdmin: authMW.RequireAdmin,
		UserIDFromCtx: func(ctx context.Context) string {
			if ac, ok := middleware.AuthFromContext(ctx); ok && ac != nil {
				return ac.UserID()
			}
			if v, ok := ctx.Value(testUserCtxKey).(string); ok {
				return v
			}
			return ""
		},
	})

	adminAuth, _ := authSvc.AuthenticateByName(ctx, "admin", "admin123", "", auth.ClientInfo{DeviceID: "d0"})
	aliceAuth, _ := authSvc.AuthenticateByName(ctx, "alice", "alice123", "", auth.ClientInfo{DeviceID: "d1"})

	// 1. Helper methods: canAccessUserData, allowedLibrariesForRequest, allowedLibrariesForUser,
	// userParentalPolicyForRequest, isPlaylistPublic, canViewPlaylist, isNonAdminCaller
	t.Run("rbac_helpers_branches", func(t *testing.T) {
		reqEmpty := httptest.NewRequest(http.MethodGet, "/", nil)
		// Empty targetUserID -> true
		if !h.canAccessUserData(reqEmpty, "") {
			t.Fatal("expected true for empty targetUserID")
		}
		// UserIDFromCtx fallback without AuthFromContext: admin -> true, non-admin mismatch -> false
		reqAdminUID := reqEmpty.WithContext(context.WithValue(ctx, testUserCtxKey, admin.ID))
		if !h.canAccessUserData(reqAdminUID, alice.ID) {
			t.Fatal("expected admin via UserIDFromCtx to access alice data")
		}
		reqBobUID := reqEmpty.WithContext(context.WithValue(ctx, testUserCtxKey, bob.ID))
		if h.canAccessUserData(reqBobUID, alice.ID) {
			t.Fatal("expected non-admin bob via UserIDFromCtx to be denied alice data")
		}
		// No auth context and nil UserIDFromCtx -> true (unit test fallback)
		hBare := New(repo, cfg)
		if !hBare.canAccessUserData(reqEmpty, alice.ID) {
			t.Fatal("expected true when no auth context configured")
		}

		// allowedLibrariesForRequest: AuthFromContext with userRepo == nil and EnableAllFolders == false
		acAlice := &middleware.AuthContext{
			User: &sqlite.UserRecord{
				ID: alice.ID,
				Policy: dto.UserPolicy{
					EnableAllFolders: false,
					EnabledFolders:   []string{libSafe.ID},
				},
			},
		}
		reqAC := reqEmpty.WithContext(middleware.WithAuthContext(ctx, acAlice))
		gotLibs := hBare.allowedLibrariesForRequest(reqAC, "")
		if len(gotLibs) != 1 || gotLibs[0] != guid.Normalize(libSafe.ID) {
			t.Fatalf("expected [libSafe] without userRepo, got %v", gotLibs)
		}
		// allowedLibrariesForRequest without AuthFromContext: uses UserIDFromCtx or fallbackUserID
		reqAliceUID := reqEmpty.WithContext(context.WithValue(ctx, testUserCtxKey, alice.ID))
		if got := h.allowedLibrariesForRequest(reqAliceUID, ""); len(got) != 1 {
			t.Fatalf("expected 1 allowed lib via UserIDFromCtx, got %v", got)
		}
		if got := h.allowedLibrariesForRequest(reqEmpty, alice.ID); len(got) != 1 {
			t.Fatalf("expected 1 allowed lib via fallbackUserID, got %v", got)
		}

		// allowedLibrariesForUser edge cases
		if got := h.allowedLibrariesForUser(ctx, ""); got != nil {
			t.Fatalf("expected nil for empty userID, got %v", got)
		}
		if got := h.allowedLibrariesForUser(ctx, guid.New()); got != nil {
			t.Fatalf("expected nil for unknown userID, got %v", got)
		}

		// userParentalPolicyForRequest: UserIDFromCtx nil + AuthContext present, and admin via UserIDFromCtx
		hNoUIDFn := New(repo, cfg)
		hNoUIDFn.SetUserRepo(userRepo)
		if maxR, _ := hNoUIDFn.userParentalPolicyForRequest(reqAC, ""); maxR == nil || *maxR != maxPG {
			t.Fatalf("expected maxPG via AuthContext when UserIDFromCtx is nil, got %v", maxR)
		}
		if maxR, tags := h.userParentalPolicyForRequest(reqAdminUID, ""); maxR != nil || len(tags) != 0 {
			t.Fatalf("expected nil parental policy for admin via UserIDFromCtx, got %v %v", maxR, tags)
		}

		// isPlaylistPublic: corrupt JSON -> false; DB error -> false
		if h.isPlaylistPublic(ctx, plBobCorrupt.ID) {
			t.Fatal("expected false for corrupt visibility JSON")
		}
		cancelledCtx, cancel := context.WithCancel(ctx)
		cancel()
		if h.isPlaylistPublic(cancelledCtx, plBobPub.ID) {
			t.Fatal("expected false on DB error")
		}

		// canViewPlaylist via UserIDFromCtx (no AuthFromContext): admin -> true, non-admin on public -> true
		if !h.canViewPlaylist(reqAdminUID, pl1.ID, alice.ID) {
			t.Fatal("expected admin via UserIDFromCtx to view alice playlist")
		}
		if !h.canViewPlaylist(reqAliceUID, plBobPub.ID, bob.ID) {
			t.Fatal("expected alice via UserIDFromCtx to view bob public playlist")
		}

		// isNonAdminCaller via UserIDFromCtx: admin -> false
		if h.isNonAdminCaller(reqAdminUID) {
			t.Fatal("expected admin via UserIDFromCtx to not be non-admin")
		}
	})

	// 2. handleGetContainerItems: Series parental inheritance, StartIndex/Limit pagination, MediaSources/MediaStreams path stripping
	t.Run("container_items_inheritance_pagination_and_path_stripping", func(t *testing.T) {
		// Full filtered list for alice: safeEp1, safeEp2, safeEp3 (rEpisode excluded by parent Series R rating!)
		req := httptest.NewRequest(http.MethodGet, "/Collections/"+col.ID+"/Items?Fields=Path,MediaSources", nil)
		req.Header.Set("X-Emby-Token", aliceAuth.AccessToken)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("got %d: %s", rec.Code, rec.Body.String())
		}
		var qr dto.QueryResult[sqlite.PlaylistItemDto]
		if err := json.Unmarshal(rec.Body.Bytes(), &qr); err != nil {
			t.Fatal(err)
		}
		if qr.TotalRecordCount != 3 || len(qr.Items) != 3 {
			t.Fatalf("expected 3 safe items (rEpisode filtered via series rating), got total=%d len=%d", qr.TotalRecordCount, len(qr.Items))
		}
		for _, it := range qr.Items {
			if it.Path != "" {
				t.Fatalf("expected stripped item path, got %q", it.Path)
			}
			for _, ms := range it.MediaSources {
				if ms.Path != "" {
					t.Fatalf("expected stripped MediaSource path, got %q", ms.Path)
				}
				for _, st := range ms.MediaStreams {
					if st.Path != "" {
						t.Fatalf("expected stripped MediaStream path, got %q", st.Path)
					}
				}
			}
		}

		// Pagination: StartIndex=1&Limit=1
		reqPage := httptest.NewRequest(http.MethodGet, "/Collections/"+col.ID+"/Items?StartIndex=1&Limit=1", nil)
		reqPage.Header.Set("X-Emby-Token", aliceAuth.AccessToken)
		recPage := httptest.NewRecorder()
		mux.ServeHTTP(recPage, reqPage)
		var qrPage dto.QueryResult[sqlite.PlaylistItemDto]
		_ = json.Unmarshal(recPage.Body.Bytes(), &qrPage)
		if qrPage.TotalRecordCount != 3 || len(qrPage.Items) != 1 {
			t.Fatalf("expected total=3 len=1 for paginated filtered container, got total=%d len=%d", qrPage.TotalRecordCount, len(qrPage.Items))
		}

		// Pagination beyond end: StartIndex=99
		reqPast := httptest.NewRequest(http.MethodGet, "/Collections/"+col.ID+"/Items?StartIndex=99", nil)
		reqPast.Header.Set("X-Emby-Token", aliceAuth.AccessToken)
		recPast := httptest.NewRecorder()
		mux.ServeHTTP(recPast, reqPast)
		var qrPast dto.QueryResult[sqlite.PlaylistItemDto]
		_ = json.Unmarshal(recPast.Body.Bytes(), &qrPast)
		if qrPast.TotalRecordCount != 3 || len(qrPast.Items) != 0 {
			t.Fatalf("expected total=3 len=0 for StartIndex=99, got total=%d len=%d", qrPast.TotalRecordCount, len(qrPast.Items))
		}
	})

	// 3. handleListPlaylists pagination for non-admin caller (StartIndex/Limit + StartIndex > len)
	t.Run("list_playlists_non_admin_pagination", func(t *testing.T) {
		_ = pl2
		_ = pl3
		req1 := httptest.NewRequest(http.MethodGet, "/Playlists?StartIndex=1&Limit=1", nil)
		req1.Header.Set("X-Emby-Token", aliceAuth.AccessToken)
		rec1 := httptest.NewRecorder()
		mux.ServeHTTP(rec1, req1)
		if rec1.Code != http.StatusOK {
			t.Fatalf("got %d: %s", rec1.Code, rec1.Body.String())
		}
		var qr1 dto.QueryResult[dto.BaseItemDto]
		_ = json.Unmarshal(rec1.Body.Bytes(), &qr1)
		// Alice sees her 3 playlists + Bob's 1 public playlist = 4 total, sliced to 1
		if qr1.TotalRecordCount != 4 || len(qr1.Items) != 1 {
			t.Fatalf("expected total=4 len=1, got total=%d len=%d", qr1.TotalRecordCount, len(qr1.Items))
		}

		req2 := httptest.NewRequest(http.MethodGet, "/Playlists?StartIndex=99", nil)
		req2.Header.Set("X-Emby-Token", aliceAuth.AccessToken)
		rec2 := httptest.NewRecorder()
		mux.ServeHTTP(rec2, req2)
		var qr2 dto.QueryResult[dto.BaseItemDto]
		_ = json.Unmarshal(rec2.Body.Bytes(), &qr2)
		if qr2.TotalRecordCount != 4 || len(qr2.Items) != 0 {
			t.Fatalf("expected total=4 len=0 for StartIndex=99, got total=%d len=%d", qr2.TotalRecordCount, len(qr2.Items))
		}
	})

	// 4. handleCreatePlaylist as admin with omitted UserId defaults to admin's ID
	t.Run("admin_create_playlist_default_user", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/Playlists", strings.NewReader(`{"Name":"Admin Default PL"}`))
		req.Header.Set("X-Emby-Token", adminAuth.AccessToken)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
		}
		var res creationResult
		_ = json.Unmarshal(rec.Body.Bytes(), &res)
		created, err := repo.GetItemRecordByID(ctx, res.Id)
		if err != nil || guid.Normalize(created.ParentID) != guid.Normalize(admin.ID) {
			t.Fatalf("expected playlist owner to default to admin, got rec=%+v err=%v", created, err)
		}
	})

	// 5. handleMovePlaylistItem missing item (404) & handlePlaylistInstantMix missing playlist (404)
	t.Run("move_and_instant_mix_not_found", func(t *testing.T) {
		reqMove := httptest.NewRequest(http.MethodPost, "/Playlists/"+pl1.ID+"/Items/"+guid.New()+"/Move/0", nil)
		reqMove.Header.Set("X-Emby-Token", aliceAuth.AccessToken)
		recMove := httptest.NewRecorder()
		mux.ServeHTTP(recMove, reqMove)
		if recMove.Code != http.StatusNotFound {
			t.Fatalf("expected 404 moving nonexistent item, got %d (%s)", recMove.Code, recMove.Body.String())
		}

		reqMix := httptest.NewRequest(http.MethodGet, "/Playlists/"+guid.New()+"/InstantMix", nil)
		reqMix.Header.Set("X-Emby-Token", aliceAuth.AccessToken)
		recMix := httptest.NewRecorder()
		mux.ServeHTTP(recMix, reqMix)
		if recMix.Code != http.StatusNotFound {
			t.Fatalf("expected 404 for nonexistent playlist InstantMix, got %d (%s)", recMix.Code, recMix.Body.String())
		}
	})

	// 6. Direct calls to addItemsToContainer & handleRemoveContainerItems on nonexistent ID (404) and closed DB (500)
	t.Run("direct_container_mutation_error_paths", func(t *testing.T) {
		missingID := guid.New()

		reqAdd := httptest.NewRequest(http.MethodPost, "/Collections/"+missingID+"/Items?Ids="+safeEp1.ID, nil)
		reqAdd.SetPathValue("Id", missingID)
		recAdd := httptest.NewRecorder()
		h.addItemsToContainer(recAdd, reqAdd, true)
		if recAdd.Code != http.StatusNotFound {
			t.Fatalf("expected 404 adding to missing container, got %d (%s)", recAdd.Code, recAdd.Body.String())
		}

		reqRem := httptest.NewRequest(http.MethodDelete, "/Collections/"+missingID+"/Items?Ids="+safeEp1.ID, nil)
		reqRem.SetPathValue("Id", missingID)
		recRem := httptest.NewRecorder()
		h.handleRemoveContainerItems(recRem, reqRem)
		if recRem.Code != http.StatusNotFound {
			t.Fatalf("expected 404 removing from missing container, got %d (%s)", recRem.Code, recRem.Body.String())
		}

		// Closed DB handler for 500 paths
		closedStore, err := sqlite.Open(filepath.Join(tmp, "closed.db"))
		if err != nil {
			t.Fatal(err)
		}
		closedRepo := sqlite.NewLibraryRepo(closedStore)
		_ = closedStore.Close()
		hClosed := New(closedRepo, cfg)

		recAdd500 := httptest.NewRecorder()
		hClosed.addItemsToContainer(recAdd500, reqAdd, true)
		if recAdd500.Code != http.StatusInternalServerError {
			t.Fatalf("expected 500 on closed DB add, got %d", recAdd500.Code)
		}

		recRem500 := httptest.NewRecorder()
		hClosed.handleRemoveContainerItems(recRem500, reqRem)
		if recRem500.Code != http.StatusInternalServerError {
			t.Fatalf("expected 500 on closed DB remove, got %d", recRem500.Code)
		}

		// handleDeleteContainer error on missing / closed DB
		reqDel := httptest.NewRequest(http.MethodDelete, "/Collections/"+missingID, nil)
		reqDel.SetPathValue("Id", missingID)
		recDel500 := httptest.NewRecorder()
		hClosed.handleDeleteContainer(recDel500, reqDel, "BoxSet", false)
		if recDel500.Code != http.StatusInternalServerError {
			t.Fatalf("expected 500 on closed DB delete, got %d", recDel500.Code)
		}
	})
}
