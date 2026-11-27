package collections

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/ambra-media/ambra/internal/api/dto"
	"github.com/ambra-media/ambra/internal/api/middleware"
	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestCollectionsCovV0112_ExtraPolicyChildVisibilityAndHelpers(t *testing.T) {
	// 1. clampCollectionsLimit & matchesBlockUnratedType branches
	if got := clampCollectionsLimit(0, 0, 5000); got != 5000 {
		t.Fatalf("expected maxVal 5000 when defaultVal<=0, got %d", got)
	}
	if got := clampCollectionsLimit(9999, 50, 5000); got != 5000 {
		t.Fatalf("expected clamped 5000, got %d", got)
	}
	if matchesBlockUnratedType("Person", "", []string{"*"}) || matchesBlockUnratedType("Folder", "", []string{"All"}) {
		t.Fatal("Folder/Person container types must never match blockUnrated")
	}
	if !matchesBlockUnratedType("Episode", "Video", []string{"", "Series"}) {
		t.Fatal("Episode must match Series blockUnrated")
	}
	if !matchesBlockUnratedType("MusicAlbum", "Audio", []string{"Music"}) {
		t.Fatal("MusicAlbum must match Music blockUnrated")
	}
	if !matchesBlockUnratedType("Movie", "Video", []string{"*"}) {
		t.Fatal("Movie must match * blockUnrated")
	}

	tmp := t.TempDir()
	st, err := sqlite.Open(filepath.Join(tmp, "coll_cov.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	ctx := context.Background()
	libRepo := sqlite.NewLibraryRepo(st)
	userRepo := sqlite.NewUserRepo(st)
	cfg := config.Default(tmp)

	h := New(libRepo, cfg)
	h.SetUserRepo(userRepo)
	h.SetStore(st)

	// Seed series (rated + tagged), rated episode, unrated series + unrated episode, and untagged movie
	seriesRatedID := guid.New()
	_ = libRepo.UpsertItem(ctx, &sqlite.ItemRecord{
		ID:             seriesRatedID,
		LibraryID:      "lib-a",
		Type:           "Series",
		Name:           "Rated Series",
		OfficialRating: "PG",
	})
	epInheritedID := guid.New()
	_ = libRepo.UpsertItem(ctx, &sqlite.ItemRecord{
		ID:        epInheritedID,
		LibraryID: "lib-a",
		SeriesID:  seriesRatedID,
		Type:      "Episode",
		Name:      "Ep Inherited",
	})
	epUnratedID := guid.New()
	_ = libRepo.UpsertItem(ctx, &sqlite.ItemRecord{
		ID:        epUnratedID,
		LibraryID: "lib-a",
		Type:      "Episode",
		Name:      "Ep Unrated",
	})
	movieRatedR := guid.New()
	_ = libRepo.UpsertItem(ctx, &sqlite.ItemRecord{
		ID:             movieRatedR,
		LibraryID:      "lib-a",
		Type:           "Movie",
		Name:           "Movie R",
		OfficialRating: "R",
	})

	// Insert tag via LibraryRepo.ReplaceItemTaxonomies on seriesRatedID
	if err := libRepo.ReplaceItemTaxonomies(ctx, seriesRatedID, "tag", []string{"FamilyTag"}); err != nil {
		t.Fatalf("ReplaceItemTaxonomies failed: %v", err)
	}

	epInheritedRec, _ := libRepo.GetItemRecordByID(ctx, epInheritedID)
	epUnratedRec, _ := libRepo.GetItemRecordByID(ctx, epUnratedID)
	movieRRec, _ := libRepo.GetItemRecordByID(ctx, movieRatedR)

	// 2. isRecordBlockedByExtraPolicy: nil/empty policy, inherited series rating & tag, unrated block, missing allowed tag
	if h.isRecordBlockedByExtraPolicy(ctx, nil, extendedParentalPolicy{}) {
		t.Fatal("nil record must return false")
	}
	polInheritOK := extendedParentalPolicy{
		blockUnratedItems: []string{"Series"},
		allowedTags:       []string{"FamilyTag"},
	}
	if h.isRecordBlockedByExtraPolicy(ctx, epInheritedRec, polInheritOK) {
		t.Fatal("expected episode inheriting Series OfficialRating and Series FamilyTag to be allowed")
	}
	if !h.isRecordBlockedByExtraPolicy(ctx, epUnratedRec, extendedParentalPolicy{blockUnratedItems: []string{"Series"}}) {
		t.Fatal("expected unrated episode without rated series to be blocked")
	}
	if !h.isRecordBlockedByExtraPolicy(ctx, movieRRec, extendedParentalPolicy{allowedTags: []string{"FamilyTag"}}) {
		t.Fatal("expected movie without allowed tag to be blocked")
	}

	// Fallback recordHasAnyTag when store is nil on handler (uses userRepo.Store()) and when both store & userRepo are nil (DTO tags)
	hUserStoreOnly := New(libRepo, cfg)
	hUserStoreOnly.SetUserRepo(userRepo)
	if !hUserStoreOnly.recordHasAnyTag(ctx, "", seriesRatedID, map[string]bool{"familytag": true}) {
		t.Fatal("expected userRepo.Store() fallback in recordHasAnyTag to match familytag")
	}
	hRepoOnly := New(libRepo, cfg)
	if !hRepoOnly.recordHasAnyTag(ctx, seriesRatedID, "", map[string]bool{"familytag": true}) {
		t.Fatal("expected DTO tag fallback in recordHasAnyTag to match familytag")
	}
	if hRepoOnly.recordHasAnyTag(ctx, "", "", map[string]bool{"familytag": true}) {
		t.Fatal("empty ID must return false")
	}
	if !isLibraryAllowed("any-lib", nil) {
		t.Fatal("nil allowed list must allow all libraries")
	}
	if h.isPlaylistPublic(ctx, "") {
		t.Fatal("empty playlist ID must not be public")
	}

	// 3. isChildVisibleToUser: nil rec, private vs owned playlist, disallowed library, parental rating block, visible
	ownerID := guid.New()
	otherID := guid.New()
	privPlaylistID := guid.New()
	_ = libRepo.UpsertItem(ctx, &sqlite.ItemRecord{
		ID:       privPlaylistID,
		ParentID: ownerID,
		Type:     "Playlist",
		Name:     "Owner Private",
	})
	privPlaylistRec, _ := libRepo.GetItemRecordByID(ctx, privPlaylistID)

	otherUser := &sqlite.UserRecord{
		ID:     otherID,
		Name:   "OtherViewer",
		Policy: dto.UserPolicy{EnableAllFolders: false, EnabledFolders: []string{"lib-a"}},
	}
	_ = userRepo.CreateUser(ctx, otherUser)
	otherCtx := middleware.WithAuthContext(ctx, &middleware.AuthContext{User: otherUser})
	reqOther := httptest.NewRequest(http.MethodGet, "/Playlists", nil).WithContext(otherCtx)

	if h.isChildVisibleToUser(reqOther, nil, nil, extendedParentalPolicy{}) {
		t.Fatal("nil itemRec must not be visible")
	}
	if h.isChildVisibleToUser(reqOther, privPlaylistRec, nil, extendedParentalPolicy{}) {
		t.Fatal("other user's private playlist must not be visible")
	}
	disallowedRec := &sqlite.ItemRecord{ID: guid.New(), LibraryID: "lib-other", Type: "Movie", Name: "Hidden Lib"}
	if h.isChildVisibleToUser(reqOther, disallowedRec, []string{guid.Normalize("lib-a")}, extendedParentalPolicy{}) {
		t.Fatal("item in disallowed library must not be visible")
	}
	five := 5
	if h.isChildVisibleToUser(reqOther, movieRRec, []string{guid.Normalize("lib-a")}, extendedParentalPolicy{maxRating: &five}) {
		t.Fatal("R-rated movie must not be visible under maxRating=5")
	}
	if !h.isChildVisibleToUser(reqOther, epInheritedRec, []string{guid.Normalize("lib-a")}, extendedParentalPolicy{}) {
		t.Fatal("allowed item must be visible")
	}

	// 4. userExtendedPolicyForRequest, playlistViewerForRequest, allowedLibrariesForUser / Request edge branches
	extPolOther := h.userExtendedPolicyForRequest(reqOther, "")
	if extPolOther.hasExtraPolicy() {
		t.Fatal("expected no extra policy on basic user")
	}
	reqUnauth := httptest.NewRequest(http.MethodGet, "/Playlists", nil)
	if admPol := h.userExtendedPolicyForRequest(reqUnauth, ""); admPol.hasExtraPolicy() {
		t.Fatal("expected empty extended policy for admin/unauth unit-test request")
	}

	// Anonymous non-admin AuthContext with empty UserID -> "__anonymous__"
	anonCtx := middleware.WithAuthContext(ctx, &middleware.AuthContext{User: &sqlite.UserRecord{ID: ""}})
	reqAnon := httptest.NewRequest(http.MethodGet, "/Playlists", nil).WithContext(anonCtx)
	if vUID, isAdmin := h.playlistViewerForRequest(reqAnon, ""); isAdmin || vUID != "__anonymous__" {
		t.Fatalf("expected (__anonymous__, false), got (%q, %v)", vUID, isAdmin)
	}

	// Handler without repo/userRepo fallback in allowedLibrariesForRequest & allowedLibrariesForUser
	hBare := &Handler{userRepo: userRepo}
	if libs := hBare.allowedLibrariesForUser(ctx, ""); libs != nil {
		t.Fatalf("expected nil for empty userID, got %v", libs)
	}
	if libs := hBare.allowedLibrariesForUser(ctx, guid.New()); len(libs) != 0 {
		t.Fatalf("expected empty slice for missing user, got %v", libs)
	}
	if libs := hBare.allowedLibrariesForUser(ctx, otherID); len(libs) != 1 {
		t.Fatalf("expected 1 enabled folder without repo, got %v", libs)
	}

	hNoRepos := &Handler{}
	reqWithFolders := httptest.NewRequest(http.MethodGet, "/Items", nil).WithContext(otherCtx)
	if libs := hNoRepos.allowedLibrariesForRequest(reqWithFolders, ""); len(libs) != 1 {
		t.Fatalf("expected 1 folder from AuthContext without repos, got %v", libs)
	}

	// 5. handleDeleteContainer & handleMovePlaylistItem error/success paths via HTTP router
	adminID := guid.New()
	adminUser := &sqlite.UserRecord{
		ID:        adminID,
		Name:      "CollAdmin",
		IsAdmin:   true,
		Policy:    sqlite.DefaultUserPolicy(true),
		CreatedAt: time.Now().UTC(),
	}
	_ = userRepo.CreateUser(ctx, adminUser)

	mux := http.NewServeMux()
	h.RegisterRoutes(mux, AuthFuncs{
		UserIDFromCtx: func(c context.Context) string {
			if ac, ok := middleware.AuthFromContext(c); ok && ac != nil {
				return ac.UserID()
			}
			return ""
		},
	})

	collID := guid.New()
	_ = libRepo.UpsertItem(ctx, &sqlite.ItemRecord{
		ID:        collID,
		LibraryID: "collections",
		Type:      "BoxSet",
		Name:      "To Delete BoxSet",
	})

	// Wrong type: DELETE /Playlists/{collID} -> 404
	wWrong := httptest.NewRecorder()
	mux.ServeHTTP(wWrong, httptest.NewRequest(http.MethodDelete, "/Playlists/"+collID, nil))
	if wWrong.Code != http.StatusNotFound {
		t.Fatalf("expected 404 deleting BoxSet via /Playlists, got %d", wWrong.Code)
	}

	// Non-owner DELETE /Playlists/{privPlaylistID} -> 403
	wForbid := httptest.NewRecorder()
	mux.ServeHTTP(wForbid, httptest.NewRequest(http.MethodDelete, "/Playlists/"+privPlaylistID, nil).WithContext(otherCtx))
	if wForbid.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for non-owner playlist delete, got %d", wForbid.Code)
	}

	// Invalid NewIndex on move playlist item -> 400
	wBadIdx := httptest.NewRecorder()
	mux.ServeHTTP(wBadIdx, httptest.NewRequest(http.MethodPost, "/Playlists/"+privPlaylistID+"/Items/item1/Move/notanint", nil))
	if wBadIdx.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for non-integer NewIndex, got %d", wBadIdx.Code)
	}

	// Valid DELETE /Collections/{collID} -> 204, then second delete -> 404
	wDel := httptest.NewRecorder()
	mux.ServeHTTP(wDel, httptest.NewRequest(http.MethodDelete, "/Collections/"+collID, nil))
	if wDel.Code != http.StatusNoContent {
		t.Fatalf("expected 204 deleting collection, got %d", wDel.Code)
	}
	wDelAgain := httptest.NewRecorder()
	mux.ServeHTTP(wDelAgain, httptest.NewRequest(http.MethodDelete, "/Collections/"+collID, nil))
	if wDelAgain.Code != http.StatusNotFound {
		t.Fatalf("expected 404 deleting already-deleted collection, got %d", wDelAgain.Code)
	}
}
