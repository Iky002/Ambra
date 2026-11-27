package collections

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/ambra-media/ambra/internal/api/dto"
	"github.com/ambra-media/ambra/internal/api/middleware"
	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestV0110_CollectionsPolicyFilterHelpers(t *testing.T) {
	for _, exempt := range []string{"CollectionFolder", "Folder", "UserView", "Person", "Genre", "Studio"} {
		if matchesBlockUnratedType(exempt, "Video", []string{"*"}) {
			t.Fatalf("expected %s exempt from matchesBlockUnratedType", exempt)
		}
	}
	if matchesBlockUnratedType("Movie", "Video", []string{"", "  ", "Audio"}) {
		t.Fatal("did not expect Movie to match Audio")
	}
	if !matchesBlockUnratedType("Movie", "Video", []string{"*"}) ||
		!matchesBlockUnratedType("Movie", "Video", []string{"all"}) ||
		!matchesBlockUnratedType("Clip", "Video", []string{"video"}) {
		t.Fatal("expected wildcard/mediaType match")
	}
	if !matchesBlockUnratedType("Season", "", []string{"Series"}) ||
		!matchesBlockUnratedType("Episode", "Video", []string{"Series"}) {
		t.Fatal("expected Series rule to match Season/Episode")
	}
	if !matchesBlockUnratedType("Audio", "Audio", []string{"Music"}) ||
		!matchesBlockUnratedType("MusicAlbum", "", []string{"Music"}) {
		t.Fatal("expected Music rule to match Audio/MusicAlbum")
	}
}

func TestV0110_CollectionsExtendedPolicyReorderAndPermissions(t *testing.T) {
	tmp := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmp, "col_v0110.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	ctx := context.Background()
	repo := sqlite.NewLibraryRepo(store)
	userRepo := sqlite.NewUserRepo(store)
	cfg := config.Default(tmp)

	libAllowed, _ := repo.CreateLibrary(ctx, "Allowed", "movies", []string{filepath.Join(tmp, "allowed")})
	libRestricted, _ := repo.CreateLibrary(ctx, "Restricted", "movies", []string{filepath.Join(tmp, "restricted")})

	movieOK1 := &sqlite.ItemRecord{
		ID:             guid.New(),
		LibraryID:      libAllowed.ID,
		ParentID:       libAllowed.ID,
		Type:           "Movie",
		MediaType:      "Video",
		Name:           "Movie OK 1",
		OfficialRating: "G",
		Path:           filepath.Join(tmp, "allowed", "m1.mkv"),
	}
	movieOK2 := &sqlite.ItemRecord{
		ID:             guid.New(),
		LibraryID:      libAllowed.ID,
		ParentID:       libAllowed.ID,
		Type:           "Movie",
		MediaType:      "Video",
		Name:           "Movie OK 2",
		OfficialRating: "PG",
		Path:           filepath.Join(tmp, "allowed", "m2.mkv"),
	}
	movieUnrated := &sqlite.ItemRecord{
		ID:        guid.New(),
		LibraryID: libAllowed.ID,
		ParentID:  libAllowed.ID,
		Type:      "Movie",
		MediaType: "Video",
		Name:      "Movie Unrated",
		Path:      filepath.Join(tmp, "allowed", "m_unrated.mkv"),
	}
	movieWrongTag := &sqlite.ItemRecord{
		ID:             guid.New(),
		LibraryID:      libAllowed.ID,
		ParentID:       libAllowed.ID,
		Type:           "Movie",
		MediaType:      "Video",
		Name:           "Movie WrongTag",
		OfficialRating: "G",
		Path:           filepath.Join(tmp, "allowed", "m_wrong.mkv"),
	}
	movieOtherLib := &sqlite.ItemRecord{
		ID:             guid.New(),
		LibraryID:      libRestricted.ID,
		ParentID:       libRestricted.ID,
		Type:           "Movie",
		MediaType:      "Video",
		Name:           "Movie OtherLib",
		OfficialRating: "G",
		Path:           filepath.Join(tmp, "restricted", "m_other.mkv"),
	}

	// Series R -> unrated Episode (should be blocked by Series parental rating in isChildVisibleToUser)
	seriesR := &sqlite.ItemRecord{
		ID:             guid.New(),
		LibraryID:      libAllowed.ID,
		ParentID:       libAllowed.ID,
		Type:           "Series",
		IsFolder:       true,
		Name:           "Series R",
		OfficialRating: "R",
	}
	epUnderSeriesR := &sqlite.ItemRecord{
		ID:        guid.New(),
		LibraryID: libAllowed.ID,
		ParentID:  seriesR.ID,
		SeriesID:  seriesR.ID,
		Type:      "Episode",
		MediaType: "Video",
		Name:      "Ep Under R Series",
	}

	// Series PG with AllowedTag -> unrated Episode (inherits rating and tag from Series)
	seriesPG := &sqlite.ItemRecord{
		ID:             guid.New(),
		LibraryID:      libAllowed.ID,
		ParentID:       libAllowed.ID,
		Type:           "Series",
		IsFolder:       true,
		Name:           "Series PG",
		OfficialRating: "PG",
	}
	epUnderSeriesPG := &sqlite.ItemRecord{
		ID:        guid.New(),
		LibraryID: libAllowed.ID,
		ParentID:  seriesPG.ID,
		SeriesID:  seriesPG.ID,
		Type:      "Episode",
		MediaType: "Video",
		Name:      "Ep Under PG Series",
	}

	song1 := &sqlite.ItemRecord{
		ID:             guid.New(),
		LibraryID:      libAllowed.ID,
		ParentID:       libAllowed.ID,
		Type:           "Audio",
		MediaType:      "Audio",
		Name:           "Song 1",
		OfficialRating: "G",
	}

	for _, it := range []*sqlite.ItemRecord{
		movieOK1, movieOK2, movieUnrated, movieWrongTag, movieOtherLib,
		seriesR, epUnderSeriesR, seriesPG, epUnderSeriesPG, song1,
	} {
		if err := repo.UpsertItem(ctx, it); err != nil {
			t.Fatal(err)
		}
	}

	for _, id := range []string{movieOK1.ID, movieOK2.ID, movieUnrated.ID, seriesPG.ID, song1.ID} {
		_ = repo.ReplaceItemTaxonomies(ctx, id, "tag", []string{"KidsAllowed"})
	}
	_ = repo.ReplaceItemTaxonomies(ctx, movieWrongTag.ID, "tag", []string{"Other"})
	_ = repo.ReplaceItemTaxonomies(ctx, song1.ID, "genre", []string{"Pop"})

	maxPG := 5
	kidPol := sqlite.DefaultUserPolicy(false)
	kidPol.EnableAllFolders = false
	kidPol.EnabledFolders = []string{libAllowed.ID}
	kidPol.MaxParentalRating = &maxPG
	kidPol.BlockUnratedItems = []string{"Movie", "Series", "BoxSet"}
	kidPol.AllowedTags = []string{"kidsallowed"}

	kid := &sqlite.UserRecord{
		ID:     guid.New(),
		Name:   "kid_col",
		Policy: kidPol,
		Config: sqlite.DefaultUserConfig(),
	}
	bob := &sqlite.UserRecord{
		ID:     guid.New(),
		Name:   "bob_col",
		Policy: sqlite.DefaultUserPolicy(false),
		Config: sqlite.DefaultUserConfig(),
	}
	admin := &sqlite.UserRecord{
		ID:      guid.New(),
		Name:    "admin_col",
		IsAdmin: true,
		Policy:  sqlite.DefaultUserPolicy(true),
		Config:  sqlite.DefaultUserConfig(),
	}
	_ = userRepo.CreateUser(ctx, kid)
	_ = userRepo.CreateUser(ctx, bob)
	_ = userRepo.CreateUser(ctx, admin)

	// Direct helper fallback tests when store & userRepo are nil
	hFallback := New(repo, cfg)
	reqFB := httptest.NewRequest(http.MethodGet, "/Collections", nil)
	reqFB = reqFB.WithContext(middleware.WithAuthContext(ctx, &middleware.AuthContext{User: kid}))
	polFB := hFallback.userExtendedPolicyForRequest(reqFB, "")
	if !polFB.hasExtraPolicy() {
		t.Fatal("expected userExtendedPolicyForRequest fallback to ac.User")
	}
	if !hFallback.recordHasAnyTag(ctx, movieOK1.ID, "", map[string]bool{"kidsallowed": true}) {
		t.Fatal("expected recordHasAnyTag fallback via repo.GetItemByID")
	}
	if !hFallback.recordHasAnyTag(ctx, epUnderSeriesPG.ID, seriesPG.ID, map[string]bool{"kidsallowed": true}) {
		t.Fatal("expected recordHasAnyTag fallback via SeriesID")
	}
	if hFallback.recordHasAnyTag(ctx, "", "", map[string]bool{"kidsallowed": true}) {
		t.Fatal("expected false on empty IDs")
	}
	if hFallback.isChildVisibleToUser(reqFB, nil, nil, polFB) {
		t.Fatal("expected nil itemRec invisible")
	}

	// Setup full mux
	mux := http.NewServeMux()
	h := New(repo, cfg)
	h.SetUserRepo(userRepo)
	h.SetStore(store)
	h.RegisterRoutes(mux, AuthFuncs{
		UserIDFromCtx: func(c context.Context) string {
			if ac, ok := middleware.AuthFromContext(c); ok && ac != nil {
				return ac.UserID()
			}
			return ""
		},
	})

	kidAC := &middleware.AuthContext{User: kid}
	bobAC := &middleware.AuthContext{User: bob}
	adminAC := &middleware.AuthContext{User: admin}

	doJSON := func(ac *middleware.AuthContext, method, target string, body any) *httptest.ResponseRecorder {
		var buf bytes.Buffer
		if body != nil {
			_ = json.NewEncoder(&buf).Encode(body)
		}
		req := httptest.NewRequest(method, target, &buf)
		req.Header.Set("Content-Type", "application/json")
		if ac != nil {
			req = req.WithContext(middleware.WithAuthContext(req.Context(), ac))
		}
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}

	// 1. Admin creates 3 Collections via JSON body:
	// - boxVisible1: contains movieOK1, movieOK2, movieUnrated, epUnderSeriesR, epUnderSeriesPG (tagged KidsAllowed, rated G)
	// - boxVisible2: contains movieOK2 (tagged KidsAllowed, rated G)
	// - boxEmptyForKid: contains only movieOtherLib and movieWrongTag (tagged KidsAllowed, rated G -> no visible children for kid!)
	// - boxBlockedItself: untagged BoxSet -> blocked by kid's AllowedTags!
	rec := doJSON(adminAC, http.MethodPost, "/Collections", map[string]any{
		"Name": "Box Visible 1",
		"Ids":  []string{movieOK1.ID, movieOK2.ID, movieUnrated.ID, epUnderSeriesR.ID, epUnderSeriesPG.ID},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("create box1 failed: %d %s", rec.Code, rec.Body.String())
	}
	var box1, box2, boxEmpty, boxUntagged creationResult
	_ = json.Unmarshal(rec.Body.Bytes(), &box1)

	rec = doJSON(adminAC, http.MethodPost, "/Collections", map[string]any{"Name": "Box Visible 2", "Ids": []string{movieOK2.ID}})
	_ = json.Unmarshal(rec.Body.Bytes(), &box2)
	rec = doJSON(adminAC, http.MethodPost, "/Collections", map[string]any{"Name": "Box No Visible Child", "Ids": []string{movieOtherLib.ID, movieWrongTag.ID}})
	_ = json.Unmarshal(rec.Body.Bytes(), &boxEmpty)
	rec = doJSON(adminAC, http.MethodPost, "/Collections", map[string]any{"Name": "Box Untagged", "Ids": []string{movieOK1.ID}})
	_ = json.Unmarshal(rec.Body.Bytes(), &boxUntagged)

	for _, id := range []string{box1.Id, box2.Id, boxEmpty.Id} {
		if r, _ := repo.GetItemRecordByID(ctx, id); r != nil {
			r.OfficialRating = "G"
			_ = repo.UpsertItem(ctx, r)
		}
		_ = repo.ReplaceItemTaxonomies(ctx, id, "tag", []string{"KidsAllowed"})
	}

	// 2. GET /Collections for kid: only box1 and box2 visible; test pagination & cross-user 403
	if rec := doJSON(kidAC, http.MethodGet, "/Collections?UserId="+bob.ID, nil); rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 on cross-user /Collections, got %d", rec.Code)
	}
	rec = doJSON(kidAC, http.MethodGet, "/Collections?StartIndex=1&Limit=1", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /Collections status=%d", rec.Code)
	}
	var colRes dto.QueryResult[dto.BaseItemDto]
	_ = json.Unmarshal(rec.Body.Bytes(), &colRes)
	if colRes.TotalRecordCount != 2 || len(colRes.Items) != 1 {
		t.Fatalf("expected 2 visible collections for kid and 1 paged, got %+v", colRes)
	}
	rec = doJSON(kidAC, http.MethodGet, "/Collections?StartIndex=20&Limit=1", nil)
	_ = json.Unmarshal(rec.Body.Bytes(), &colRes)
	if len(colRes.Items) != 0 {
		t.Fatal("expected empty collections slice when StartIndex > total")
	}

	// 3. GET /Collections/{Id}/Items: blocked on boxUntagged (403), filtered children on box1 (movieOK1, movieOK2, epUnderSeriesPG = 3 visible!)
	if rec := doJSON(kidAC, http.MethodGet, "/Collections/"+boxUntagged.Id+"/Items", nil); rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 on untagged collection for kid with AllowedTags, got %d", rec.Code)
	}
	rec = doJSON(kidAC, http.MethodGet, "/Collections/"+box1.Id+"/Items?StartIndex=1&Limit=1", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /Collections/{Id}/Items status=%d", rec.Code)
	}
	var plItems dto.QueryResult[sqlite.PlaylistItemDto]
	_ = json.Unmarshal(rec.Body.Bytes(), &plItems)
	if plItems.TotalRecordCount != 3 || len(plItems.Items) != 1 {
		t.Fatalf("expected 3 visible children (movieOK1, movieOK2, epUnderSeriesPG) and 1 paged, got %+v", plItems)
	}
	rec = doJSON(kidAC, http.MethodGet, "/Collections/"+box1.Id+"/Items?StartIndex=99&Limit=1", nil)
	_ = json.Unmarshal(rec.Body.Bytes(), &plItems)
	if len(plItems.Items) != 0 {
		t.Fatal("expected empty container items when StartIndex > total")
	}

	// 4. Playlists: create via JSON body, list with pagination, reorder (Move), remove via JSON body, InstantMix, delete 404/204
	rec = doJSON(kidAC, http.MethodPost, "/Playlists", map[string]any{
		"Name":      "Kid Mix",
		"MediaType": "Audio",
		"Ids":       []string{movieOK1.ID, movieOK2.ID, movieUnrated.ID, song1.ID},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /Playlists failed: %d %s", rec.Code, rec.Body.String())
	}
	var kidPL creationResult
	_ = json.Unmarshal(rec.Body.Bytes(), &kidPL)
	_ = repo.ReplaceItemTaxonomies(ctx, kidPL.Id, "tag", []string{"KidsAllowed"})

	// Create second playlist for kid to exercise pagination in handleListPlaylists
	rec = doJSON(kidAC, http.MethodPost, "/Playlists", map[string]any{"Name": "Kid Mix 2"})
	var kidPL2 creationResult
	_ = json.Unmarshal(rec.Body.Bytes(), &kidPL2)
	_ = repo.ReplaceItemTaxonomies(ctx, kidPL2.Id, "tag", []string{"KidsAllowed"})

	if rec := doJSON(kidAC, http.MethodGet, "/Playlists?UserId="+bob.ID, nil); rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 on cross-user /Playlists, got %d", rec.Code)
	}
	rec = doJSON(kidAC, http.MethodGet, "/Playlists?StartIndex=1&Limit=1", nil)
	_ = json.Unmarshal(rec.Body.Bytes(), &colRes)
	if colRes.TotalRecordCount != 2 || len(colRes.Items) != 1 {
		t.Fatalf("expected 2 visible playlists for kid and 1 paged, got %+v", colRes)
	}
	rec = doJSON(kidAC, http.MethodGet, "/Playlists?StartIndex=50&Limit=1", nil)
	_ = json.Unmarshal(rec.Body.Bytes(), &colRes)
	if len(colRes.Items) != 0 {
		t.Fatal("expected empty playlists when StartIndex > total")
	}

	// MovePlaylistItem: invalid index (400), missing item (404), valid move (204)
	if rec := doJSON(kidAC, http.MethodPost, "/Playlists/"+kidPL.Id+"/Items/"+movieOK2.ID+"/Move/notanumber", nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 on invalid NewIndex, got %d", rec.Code)
	}
	if rec := doJSON(kidAC, http.MethodPost, "/Playlists/"+kidPL.Id+"/Items/"+guid.New()+"/Move/0", nil); rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 moving unknown item, got %d", rec.Code)
	}
	if rec := doJSON(kidAC, http.MethodPost, "/Playlists/"+kidPL.Id+"/Items/"+movieOK2.ID+"/Move/0", nil); rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204 moving playlist item, got %d %s", rec.Code, rec.Body.String())
	}

	// Remove items via JSON body Ids
	if rec := doJSON(kidAC, http.MethodDelete, "/Playlists/"+kidPL.Id+"/Items", map[string]any{"Ids": []string{movieOK2.ID}}); rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204 removing playlist item via JSON Ids, got %d", rec.Code)
	}

	// Playlist InstantMix: 403 cross-user, 404 unknown, 404 non-playlist, 403 private playlist by bob, 200 by owner
	if rec := doJSON(kidAC, http.MethodGet, "/Playlists/"+kidPL.Id+"/InstantMix?UserId="+bob.ID, nil); rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 on cross-user Playlist InstantMix, got %d", rec.Code)
	}
	if rec := doJSON(kidAC, http.MethodGet, "/Playlists/"+guid.New()+"/InstantMix", nil); rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 on unknown Playlist InstantMix, got %d", rec.Code)
	}
	if rec := doJSON(kidAC, http.MethodGet, "/Playlists/"+movieOK1.ID+"/InstantMix", nil); rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 on Movie ID in Playlist InstantMix, got %d", rec.Code)
	}
	if rec := doJSON(bobAC, http.MethodGet, "/Playlists/"+kidPL.Id+"/InstantMix", nil); rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for bob on kid's private Playlist InstantMix, got %d", rec.Code)
	}
	if rec := doJSON(kidAC, http.MethodGet, "/Playlists/"+kidPL.Id+"/InstantMix", nil); rec.Code != http.StatusOK {
		t.Fatalf("expected 200 on owner Playlist InstantMix, got %d %s", rec.Code, rec.Body.String())
	}

	// Delete unknown collection/playlist -> 404
	if rec := doJSON(adminAC, http.MethodDelete, "/Collections/"+guid.New(), nil); rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 deleting unknown collection, got %d", rec.Code)
	}
}
