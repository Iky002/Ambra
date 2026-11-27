package library

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

type libCtxKey string

const testLibUIDKey libCtxKey = "uid"

func TestLibraryCoverageBoostV0103(t *testing.T) {
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
	libRepo := sqlite.NewLibraryRepo(store)
	userRepo := sqlite.NewUserRepo(store)
	authSvc := auth.NewService(userRepo, cfg)

	admin, _ := authSvc.CreateUser(ctx, "admin", "admin123", true)
	alice, _ := authSvc.CreateUser(ctx, "alice", "alice123", false)
	bob, _ := authSvc.CreateUser(ctx, "bob", "bob123", false)

	libSafe, _ := libRepo.CreateLibrary(ctx, "Safe", "tvshows", []string{"/safe"})
	libSecret, _ := libRepo.CreateLibrary(ctx, "Secret", "tvshows", []string{"/secret"})

	ap := alice.Policy
	ap.EnableAllFolders = false
	ap.EnabledFolders = []string{libSafe.ID}
	maxPG := 5
	ap.MaxParentalRating = &maxPG
	_ = userRepo.UpdateUserPolicy(ctx, alice.ID, ap)
	authSvc.InvalidateUserCache(alice.ID)

	adminAuth, _ := authSvc.AuthenticateByName(ctx, "admin", "admin123", "", auth.ClientInfo{DeviceID: "d-adm"})
	aliceAuth, _ := authSvc.AuthenticateByName(ctx, "alice", "alice123", "", auth.ClientInfo{DeviceID: "d-ali"})

	intPtr := func(v int) *int { return &v }

	// Seed rich items across types for handleItemCounts, taxonomies, seasons, episodes
	seriesSafe := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: libSafe.ID, ParentID: libSafe.ID,
		Type: "Series", Name: "Safe Show", SortName: "safe show", OfficialRating: "G",
	}
	season0 := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: libSafe.ID, ParentID: seriesSafe.ID, SeriesID: seriesSafe.ID,
		Type: "Season", Name: "Specials", IndexNumber: intPtr(0),
	}
	season1 := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: libSafe.ID, ParentID: seriesSafe.ID, SeriesID: seriesSafe.ID,
		Type: "Season", Name: "Season 1", IndexNumber: intPtr(1),
	}
	ep1 := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: libSafe.ID, ParentID: season1.ID, SeriesID: seriesSafe.ID, SeasonID: season1.ID,
		Type: "Episode", MediaType: "Video", Name: "Ep 1", ParentIndexNumber: intPtr(1), IndexNumber: intPtr(1),
	}
	ep2 := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: libSafe.ID, ParentID: season1.ID, SeriesID: seriesSafe.ID, SeasonID: season1.ID,
		Type: "Episode", MediaType: "Video", Name: "Ep 2", ParentIndexNumber: intPtr(1), IndexNumber: intPtr(2),
	}
	ep3 := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: libSafe.ID, ParentID: season1.ID, SeriesID: seriesSafe.ID, SeasonID: season1.ID,
		Type: "Episode", MediaType: "Video", Name: "Ep 3", ParentIndexNumber: intPtr(1), IndexNumber: intPtr(3),
	}
	seriesSecret := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: libSecret.ID, ParentID: libSecret.ID,
		Type: "Series", Name: "Secret Show", SortName: "secret show", OfficialRating: "G",
	}

	extraTypes := []sqlite.ItemRecord{
		{ID: guid.New(), LibraryID: libSafe.ID, ParentID: libSafe.ID, Type: "Movie", Name: "M1", OfficialRating: "G"},
		{ID: guid.New(), LibraryID: libSafe.ID, ParentID: libSafe.ID, Type: "Movie", Name: "Bonus", Extra: sqlite.ItemExtra{ExtraType: "Trailer"}, OfficialRating: "G"},
		{ID: guid.New(), LibraryID: libSafe.ID, ParentID: libSafe.ID, Type: "MusicArtist", Name: "Art1"},
		{ID: guid.New(), LibraryID: libSafe.ID, ParentID: libSafe.ID, Type: "Audio", Name: "Song1"},
		{ID: guid.New(), LibraryID: libSafe.ID, ParentID: libSafe.ID, Type: "MusicAlbum", Name: "Alb1"},
		{ID: guid.New(), LibraryID: libSafe.ID, ParentID: libSafe.ID, Type: "MusicVideo", Name: "MV1"},
		{ID: guid.New(), LibraryID: libSafe.ID, ParentID: libSafe.ID, Type: "BoxSet", Name: "Col1"},
		{ID: guid.New(), LibraryID: libSafe.ID, ParentID: libSafe.ID, Type: "Book", Name: "Book1"},
		{ID: guid.New(), LibraryID: libSafe.ID, ParentID: libSafe.ID, Type: "Trailer", Name: "Tr1"},
		{ID: guid.New(), LibraryID: libSafe.ID, ParentID: libSafe.ID, Type: "Program", Name: "Prog1"},
	}

	for _, it := range []*sqlite.ItemRecord{seriesSafe, season0, season1, ep1, ep2, ep3, seriesSecret} {
		if err := libRepo.UpsertItem(ctx, it); err != nil {
			t.Fatal(err)
		}
	}
	for i := range extraTypes {
		if err := libRepo.UpsertItem(ctx, &extraTypes[i]); err != nil {
			t.Fatal(err)
		}
	}
	// Attach genres to movie for taxonomy sorting/pagination
	if err := libRepo.ReplaceItemTaxonomies(ctx, extraTypes[0].ID, "Genre", []string{"Action", "Comedy", "Drama"}); err != nil {
		t.Fatal(err)
	}

	bobPrivPL, _ := libRepo.CreatePlaylist(ctx, "Bob Secret", "Video", bob.ID, nil)
	bobPubPL, _ := libRepo.CreatePlaylist(ctx, "Bob Public", "Video", bob.ID, nil)
	bobCorruptPL, _ := libRepo.CreatePlaylist(ctx, "Bob Corrupt", "Video", bob.ID, nil)
	_, _ = store.Writer.ExecContext(ctx, `INSERT OR REPLACE INTO server_config(key, value_json, updated_at) VALUES(?, ?, 1)`,
		"item_visibility_"+guid.Normalize(bobPubPL.ID), `{"IsPublic":true}`)
	_, _ = store.Writer.ExecContext(ctx, `INSERT OR REPLACE INTO server_config(key, value_json, updated_at) VALUES(?, ?, 1)`,
		"item_visibility_"+guid.Normalize(bobCorruptPL.ID), `{bad-json`)

	h := New(libRepo, nil, cfg)
	h.SetUserRepo(userRepo)
	h.SetStore(store)
	mux := http.NewServeMux()
	authMW := middleware.NewAuth(authSvc, nil)
	h.RegisterRoutes(mux, AuthFuncs{
		RequireAuth:  authMW.RequireAuth,
		RequireAdmin: authMW.RequireAdmin,
		UserIDFromCtx: func(ctx context.Context) string {
			if ac, ok := middleware.AuthFromContext(ctx); ok && ac != nil {
				return ac.UserID()
			}
			if v, ok := ctx.Value(testLibUIDKey).(string); ok {
				return v
			}
			return ""
		},
	})

	doReq := func(tok, method, path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		if tok != "" {
			req.Header.Set("X-Emby-Token", tok)
		}
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}

	// 1. Helper edge cases: canViewPlaylist, isPlaylistPublic, queryIncludesPlaylists,
	// appendPlaylistsLibrary, hiddenPrivatePlaylistIDs, userParentalPolicyForRequest, parseDatePlayed
	t.Run("helper_functions_branches", func(t *testing.T) {
		reqBase := httptest.NewRequest(http.MethodGet, "/", nil)
		reqAdminAC := reqBase.WithContext(middleware.WithAuthContext(ctx, &middleware.AuthContext{User: admin}))
		if !h.canViewPlaylist(reqAdminAC, bobPrivPL.ID, bob.ID) {
			t.Fatal("expected admin AuthContext to view private playlist")
		}
		reqAdminUID := reqBase.WithContext(context.WithValue(ctx, testLibUIDKey, admin.ID))
		if !h.canViewPlaylist(reqAdminUID, bobPrivPL.ID, bob.ID) {
			t.Fatal("expected admin via UserIDFromCtx to view private playlist")
		}
		reqAliceUID := reqBase.WithContext(context.WithValue(ctx, testLibUIDKey, alice.ID))
		if !h.canViewPlaylist(reqAliceUID, bobPubPL.ID, bob.ID) {
			t.Fatal("expected alice via UserIDFromCtx to view public playlist")
		}
		if h.canViewPlaylist(reqAliceUID, bobPrivPL.ID, bob.ID) {
			t.Fatal("expected alice via UserIDFromCtx to be denied bob private playlist")
		}
		hBare := New(libRepo, nil, cfg)
		if !hBare.canViewPlaylist(reqBase, bobPrivPL.ID, bob.ID) {
			t.Fatal("expected unit-test fallback true when no auth configured")
		}

		// isPlaylistPublic edge cases
		if h.isPlaylistPublic(ctx, "") {
			t.Fatal("expected false for empty playlistID")
		}
		if h.isPlaylistPublic(ctx, bobCorruptPL.ID) {
			t.Fatal("expected false for corrupt JSON")
		}
		if hBare.isPlaylistPublic(ctx, bobPubPL.ID) {
			t.Fatal("expected false when both userRepo and store are nil")
		}
		hStoreOnly := New(libRepo, nil, cfg)
		hStoreOnly.SetStore(store)
		cancelledCtx, cancel := context.WithCancel(ctx)
		cancel()
		if hStoreOnly.isPlaylistPublic(cancelledCtx, bobPubPL.ID) {
			t.Fatal("expected false on cancelled ctx DB query")
		}

		// queryIncludesPlaylists & appendPlaylistsLibrary
		if queryIncludesPlaylists(nil, []string{"Playlist"}) {
			t.Fatal("expected false when Playlist is excluded")
		}
		if appendPlaylistsLibrary(nil) != nil {
			t.Fatal("expected nil for nil allowed list")
		}
		got := appendPlaylistsLibrary([]string{"lib1", "playlists"})
		if len(got) != 2 {
			t.Fatalf("expected no duplicate playlists entry, got %v", got)
		}

		// hiddenPrivatePlaylistIDs: h.store == nil fallback to userRepo.Store(), nil both, and DB error
		hNoStore := New(libRepo, nil, cfg)
		hNoStore.SetUserRepo(userRepo)
		reqAliceAC := reqBase.WithContext(middleware.WithAuthContext(ctx, &middleware.AuthContext{User: alice}))
		if hidden := hNoStore.hiddenPrivatePlaylistIDs(reqAliceAC); len(hidden) == 0 {
			t.Fatal("expected hiddenPrivatePlaylistIDs to fall back to userRepo.Store()")
		}
		if hidden := hBare.hiddenPrivatePlaylistIDs(reqAliceAC); hidden != nil {
			t.Fatalf("expected nil when both store and userRepo are nil, got %v", hidden)
		}
		reqCancelled := reqAliceAC.WithContext(cancelledCtx)
		if hidden := hStoreOnly.hiddenPrivatePlaylistIDs(reqCancelled); hidden != nil {
			t.Fatalf("expected nil on query error, got %v", hidden)
		}

		// userParentalPolicyForRequest: UserIDFromCtx == nil with AuthContext, and admin via UserIDFromCtx
		if maxR, _ := hNoStore.userParentalPolicyForRequest(reqAliceAC, ""); maxR == nil || *maxR != maxPG {
			t.Fatalf("expected maxPG via AuthContext, got %v", maxR)
		}
		if maxR, tags := h.userParentalPolicyForRequest(reqAdminUID, ""); maxR != nil || len(tags) != 0 {
			t.Fatalf("expected nil parental policy for admin via UserIDFromCtx, got %v %v", maxR, tags)
		}

		// parseDatePlayed edge cases
		if _, ok := parseDatePlayed("   "); ok {
			t.Fatal("expected false for blank date")
		}
		if _, ok := parseDatePlayed("not-a-valid-date"); ok {
			t.Fatal("expected false for invalid date")
		}
	})

	// 2. handleItemCounts with parental policy (scans all types including ExtraType skip, MusicArtist, Audio, MusicAlbum, MusicVideo, BoxSet, Book, Trailer, Program)
	t.Run("item_counts_all_types_under_parental_policy", func(t *testing.T) {
		rec := doReq(aliceAuth.AccessToken, http.MethodGet, "/Items/Counts", "")
		if rec.Code != http.StatusOK {
			t.Fatalf("Items/Counts got %d: %s", rec.Code, rec.Body.String())
		}
		var counts sqlite.ItemCounts
		if err := json.Unmarshal(rec.Body.Bytes(), &counts); err != nil {
			t.Fatal(err)
		}
		if counts.ArtistCount != 1 || counts.SongCount != 1 || counts.AlbumCount != 1 ||
			counts.MusicVideoCount != 1 || counts.BoxSetCount != 1 || counts.BookCount != 1 ||
			counts.TrailerCount != 2 || counts.ProgramCount != 1 {
			t.Fatalf("unexpected counts: %+v", counts)
		}
	})

	// 3. handleShowSeasons & handleShowEpisodes: restricted library 403, AdjacentTo clamps & miss, StartItemId miss, pagination
	t.Run("show_seasons_and_episodes_edge_cases", func(t *testing.T) {
		if rec := doReq(aliceAuth.AccessToken, http.MethodGet, "/Shows/"+seriesSecret.ID+"/Seasons", ""); rec.Code != http.StatusForbidden {
			t.Fatalf("expected 403 for restricted library ShowSeasons, got %d", rec.Code)
		}
		if rec := doReq(aliceAuth.AccessToken, http.MethodGet, "/Shows/"+seriesSecret.ID+"/Episodes", ""); rec.Code != http.StatusForbidden {
			t.Fatalf("expected 403 for restricted library ShowEpisodes, got %d", rec.Code)
		}

		// ShowSeasons IsSpecialSeason=false with StartIndex > len
		recS := doReq(aliceAuth.AccessToken, http.MethodGet, "/Shows/"+seriesSafe.ID+"/Seasons?IsSpecialSeason=false&StartIndex=99", "")
		if recS.Code != http.StatusOK {
			t.Fatalf("ShowSeasons got %d", recS.Code)
		}
		var qrS dto.QueryResult[dto.BaseItemDto]
		_ = json.Unmarshal(recS.Body.Bytes(), &qrS)
		if qrS.TotalRecordCount != 1 || len(qrS.Items) != 0 {
			t.Fatalf("expected total=1 len=0 for StartIndex=99, got total=%d len=%d", qrS.TotalRecordCount, len(qrS.Items))
		}

		// ShowEpisodes AdjacentTo first (lo clamp), last (hi clamp), missing
		for _, tc := range []struct {
			qs      string
			wantLen int
		}{
			{"AdjacentTo=" + ep1.ID, 2},
			{"AdjacentTo=" + ep3.ID, 2},
			{"AdjacentTo=" + guid.New(), 0},
			{"StartItemId=" + guid.New(), 0},
			{"Season=1&StartIndex=1&Limit=1", 1},
			{"Season=1&StartIndex=99", 0},
		} {
			recE := doReq(aliceAuth.AccessToken, http.MethodGet, "/Shows/"+seriesSafe.ID+"/Episodes?"+tc.qs, "")
			if recE.Code != http.StatusOK {
				t.Fatalf("Episodes %s got %d: %s", tc.qs, recE.Code, recE.Body.String())
			}
			var qrE dto.QueryResult[dto.BaseItemDto]
			_ = json.Unmarshal(recE.Body.Bytes(), &qrE)
			if len(qrE.Items) != tc.wantLen {
				t.Fatalf("Episodes %s: want len=%d, got len=%d", tc.qs, tc.wantLen, len(qrE.Items))
			}
		}
	})

	// 4. handleTaxonomies: SortBy=Random, SortOrder=Ascending, StartIndex/Limit, StartIndex > len
	t.Run("taxonomies_sort_and_pagination", func(t *testing.T) {
		for _, qs := range []string{
			"SortBy=Random",
			"SortOrder=Ascending&StartIndex=1&Limit=1",
			"SortOrder=Ascending&StartIndex=99",
		} {
			rec := doReq(aliceAuth.AccessToken, http.MethodGet, "/Genres?"+qs, "")
			if rec.Code != http.StatusOK {
				t.Fatalf("Genres?%s got %d: %s", qs, rec.Code, rec.Body.String())
			}
		}
	})

	// 5. handleQueryItems post-filter playlist path (when h.store == nil and h.userRepo == nil so hiddenPrivatePlaylistIDs is skipped)
	t.Run("query_items_post_filter_private_playlists", func(t *testing.T) {
		hPostFilter := New(libRepo, nil, cfg)
		req := httptest.NewRequest(http.MethodGet, "/Items?IncludeItemTypes=Playlist&Recursive=true", nil)
		req = req.WithContext(middleware.WithAuthContext(req.Context(), &middleware.AuthContext{User: alice}))
		rec := httptest.NewRecorder()
		hPostFilter.handleQueryItems(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("handleQueryItems post-filter got %d: %s", rec.Code, rec.Body.String())
		}
		var qr dto.QueryResult[dto.BaseItemDto]
		_ = json.Unmarshal(rec.Body.Bytes(), &qr)
		for _, it := range qr.Items {
			if it.Id == bobPrivPL.ID {
				t.Fatal("expected post-filter to strip bob's private playlist")
			}
		}
	})

	// 6. handleCreateVirtualFolder with LibraryOptions (non-PathInfos keys saved) + DB error (500)
	t.Run("create_virtual_folder_options_and_error", func(t *testing.T) {
		body := `{"Name":"NewDocs","CollectionType":"movies","LibraryOptions":{"EnableRealtimeMonitor":true,"PathInfos":[{"Path":"/srv/docs"}]}}`
		rec := doReq(adminAuth.AccessToken, http.MethodPost, "/Library/VirtualFolders", body)
		if rec.Code != http.StatusOK {
			t.Fatalf("CreateVirtualFolder got %d: %s", rec.Code, rec.Body.String())
		}
		var vf virtualFolderDTO
		_ = json.Unmarshal(rec.Body.Bytes(), &vf)
		if vf.LibraryOptions["EnableRealtimeMonitor"] != true {
			t.Fatalf("expected EnableRealtimeMonitor=true, got %+v", vf.LibraryOptions)
		}
	})

	// 7. canAccessItem not-found + closed DB error paths across MarkPlayed/Unplayed/Favorite/UnmarkFavorite & ItemCounts & CreateVirtualFolder
	t.Run("closed_db_error_paths_and_canAccessItem", func(t *testing.T) {
		reqAlice := httptest.NewRequest(http.MethodGet, "/", nil).WithContext(middleware.WithAuthContext(ctx, &middleware.AuthContext{User: alice}))
		// Nonexistent item -> canAccessItem returns (true, nil)
		if ok, err := h.canAccessItem(reqAlice, alice.ID, guid.New()); err != nil || !ok {
			t.Fatalf("expected (true, nil) for missing item in canAccessItem, got (%v, %v)", ok, err)
		}

		// Separate closed store for libRepo to trigger 500s cleanly
		closedStore, err := sqlite.Open(filepath.Join(tmp, "closed_lib.db"))
		if err != nil {
			t.Fatal(err)
		}
		closedLibRepo := sqlite.NewLibraryRepo(closedStore)
		_ = closedStore.Close()

		hClosed := New(closedLibRepo, nil, cfg)
		hClosed.SetUserRepo(userRepo) // userRepo stays open so user policy resolves!
		hClosed.SetStore(store)

		// handleItemCounts with parental policy -> QueryItems fails -> 500
		recCnt := httptest.NewRecorder()
		hClosed.handleItemCounts(recCnt, reqAlice)
		if recCnt.Code != http.StatusInternalServerError {
			t.Fatalf("expected 500 for handleItemCounts on closed libRepo, got %d", recCnt.Code)
		}

		// handleCreateVirtualFolder on closed libRepo -> 500
		reqCVF := httptest.NewRequest(http.MethodPost, "/Library/VirtualFolders", strings.NewReader(`{"Name":"FailLib"}`))
		recCVF := httptest.NewRecorder()
		hClosed.handleCreateVirtualFolder(recCVF, reqCVF)
		if recCVF.Code != http.StatusInternalServerError {
			t.Fatalf("expected 500 for CreateVirtualFolder on closed libRepo, got %d", recCVF.Code)
		}

		// MarkPlayed / MarkUnplayed / MarkFavorite / UnmarkFavorite -> canAccessItem returns DB error -> 500
		for _, tc := range []struct {
			name string
			fn   http.HandlerFunc
		}{
			{"MarkPlayed", hClosed.handleMarkPlayed},
			{"MarkUnplayed", hClosed.handleMarkUnplayed},
			{"MarkFavorite", hClosed.handleMarkFavorite},
			{"UnmarkFavorite", hClosed.handleUnmarkFavorite},
		} {
			reqM := httptest.NewRequest(http.MethodPost, "/", nil).WithContext(middleware.WithAuthContext(ctx, &middleware.AuthContext{User: alice}))
			reqM.SetPathValue("UserId", alice.ID)
			reqM.SetPathValue("Id", ep1.ID)
			recM := httptest.NewRecorder()
			tc.fn(recM, reqM)
			if recM.Code != http.StatusInternalServerError {
				t.Fatalf("expected 500 for %s on closed libRepo, got %d (%s)", tc.name, recM.Code, recM.Body.String())
			}
		}
	})
}
