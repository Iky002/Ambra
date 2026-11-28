package sqlite

import (
	"context"
	"fmt"
	"testing"

	"github.com/ambra-media/ambra/internal/api/dto"
	"github.com/ambra-media/ambra/internal/pkg/guid"
)

func TestPolicyRBACFailClosedAndGetItemByIDScopedHydration(t *testing.T) {
	ctx := context.Background()
	st := openTestStore(t)
	libRepo := NewLibraryRepo(st)
	userRepo := NewUserRepo(st)

	libA, err := libRepo.CreateLibrary(ctx, "AllowedTV", "tvshows", nil)
	if err != nil {
		t.Fatal(err)
	}
	libB, err := libRepo.CreateLibrary(ctx, "SecretTV", "tvshows", nil)
	if err != nil {
		t.Fatal(err)
	}

	series := &ItemRecord{
		ID:        guid.New(),
		LibraryID: libA.ID,
		ParentID:  libA.ID,
		Type:      "Series",
		IsFolder:  true,
		Name:      "Split Lib Show",
		SortName:  "split lib show",
	}
	seasonA := &ItemRecord{
		ID:        guid.New(),
		LibraryID: libA.ID,
		ParentID:  series.ID,
		SeriesID:  series.ID,
		Type:      "Season",
		IsFolder:  true,
		Name:      "Season 1",
		SortName:  "season 01",
	}
	seasonB := &ItemRecord{
		ID:        guid.New(),
		LibraryID: libB.ID,
		ParentID:  series.ID,
		SeriesID:  series.ID,
		Type:      "Season",
		IsFolder:  true,
		Name:      "Season 2 Secret",
		SortName:  "season 02",
	}
	epA := &ItemRecord{
		ID:        guid.New(),
		LibraryID: libA.ID,
		ParentID:  seasonA.ID,
		SeriesID:  series.ID,
		SeasonID:  seasonA.ID,
		Type:      "Episode",
		MediaType: "Video",
		Name:      "Ep Allowed",
		SortName:  "ep allowed",
	}
	epB := &ItemRecord{
		ID:        guid.New(),
		LibraryID: libB.ID,
		ParentID:  seasonB.ID,
		SeriesID:  series.ID,
		SeasonID:  seasonB.ID,
		Type:      "Episode",
		MediaType: "Video",
		Name:      "Ep Secret",
		SortName:  "ep secret",
	}
	for _, rec := range []*ItemRecord{series, seasonA, seasonB, epA, epB} {
		if err := libRepo.UpsertItem(ctx, rec); err != nil {
			t.Fatal(err)
		}
	}

	// 1. Default "{}" policy_json keeps EnableAllFolders=true (unrestricted allowedLibs == nil).
	defaultUser := &UserRecord{ID: guid.New(), Name: "default_empty_pol", Policy: DefaultUserPolicy(false)}
	if err := userRepo.CreateUser(ctx, defaultUser); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Writer.ExecContext(ctx, `UPDATE users SET policy_json = '{}' WHERE id = ?`, defaultUser.ID); err != nil {
		t.Fatal(err)
	}
	_, defAllowed, defRestricted := libRepo.loadRestrictedUserPolicy(ctx, defaultUser.ID)
	if defAllowed != nil || defRestricted {
		t.Fatalf("expected '{}' user policy to default to unrestricted folders, got allowed=%v restricted=%v", defAllowed, defRestricted)
	}

	// 2. Explicit {"EnableAllFolders": false} with omitted/null EnabledFolders fails closed (allowedLibs = []string{}).
	failClosedUser := &UserRecord{ID: guid.New(), Name: "fail_closed_pol", Policy: DefaultUserPolicy(false)}
	if err := userRepo.CreateUser(ctx, failClosedUser); err != nil {
		t.Fatal(err)
	}
	for _, rawJSON := range []string{`{"EnableAllFolders":false}`, `{"EnableAllFolders":false,"EnabledFolders":null}`} {
		if _, err := st.Writer.ExecContext(ctx, `UPDATE users SET policy_json = ? WHERE id = ?`, rawJSON, failClosedUser.ID); err != nil {
			t.Fatal(err)
		}
		_, fcAllowed, fcRestricted := libRepo.loadRestrictedUserPolicy(ctx, failClosedUser.ID)
		if fcAllowed == nil || len(fcAllowed) != 0 || !fcRestricted {
			t.Fatalf("expected %s to fail closed with empty non-nil allowedLibs, got allowed=%v restricted=%v", rawJSON, fcAllowed, fcRestricted)
		}
	}

	// 3. IsItemBlockedByPolicy fails closed when EnableAllFolders=false and EnabledFolders==nil.
	blocked, err := libRepo.IsItemBlockedByPolicy(ctx, epA, dto.UserPolicy{
		EnableAllFolders:   false,
		EnabledFolders:     nil,
		EnableLiveTvAccess: true,
	})
	if err != nil || !blocked {
		t.Fatalf("expected IsItemBlockedByPolicy to fail closed when EnableAllFolders=false and EnabledFolders=nil, got blocked=%v err=%v", blocked, err)
	}

	// 4. GetItemByID passes allowedLibs to hydrateDTOsWithPolicy so Series ChildCount / RecursiveItemCount exclude disallowed libraries!
	scopedPol := DefaultUserPolicy(false)
	scopedPol.EnableAllFolders = false
	scopedPol.EnabledFolders = []string{libA.ID}
	scopedUser := &UserRecord{ID: guid.New(), Name: "scoped_viewer", Policy: scopedPol}
	if err := userRepo.CreateUser(ctx, scopedUser); err != nil {
		t.Fatal(err)
	}

	adminSeriesDTO, err := libRepo.GetItemByID(ctx, "srv1", "", series.ID)
	if err != nil {
		t.Fatal(err)
	}
	if adminSeriesDTO.RecursiveItemCount == nil || *adminSeriesDTO.RecursiveItemCount != 2 {
		t.Fatalf("expected unrestricted Series RecursiveItemCount=2, got %v", adminSeriesDTO.RecursiveItemCount)
	}
	if adminSeriesDTO.ChildCount == nil || *adminSeriesDTO.ChildCount != 2 {
		t.Fatalf("expected unrestricted Series ChildCount=2, got %v", adminSeriesDTO.ChildCount)
	}

	scopedSeriesDTO, err := libRepo.GetItemByID(ctx, "srv1", scopedUser.ID, series.ID)
	if err != nil {
		t.Fatal(err)
	}
	if scopedSeriesDTO.RecursiveItemCount == nil || *scopedSeriesDTO.RecursiveItemCount != 1 {
		t.Fatalf("expected scoped GetItemByID RecursiveItemCount=1 (excluding secret libB), got %v", scopedSeriesDTO.RecursiveItemCount)
	}
	if scopedSeriesDTO.ChildCount == nil || *scopedSeriesDTO.ChildCount != 1 {
		t.Fatalf("expected scoped GetItemByID ChildCount=1 (excluding secret libB), got %v", *scopedSeriesDTO.ChildCount)
	}
}

func TestIncludeItemTypesLargeSetBindLimitSafety(t *testing.T) {
	ctx := context.Background()
	st := openTestStore(t)
	repo := NewLibraryRepo(st)

	lib, err := repo.CreateLibrary(ctx, "Movies", "movies", nil)
	if err != nil {
		t.Fatal(err)
	}
	mov := &ItemRecord{
		ID:             guid.New(),
		LibraryID:      lib.ID,
		ParentID:       lib.ID,
		Type:           "Movie",
		MediaType:      "Video",
		Name:           "Inception",
		SortName:       "inception",
		OfficialRating: "PG-13",
		ProductionYear: intPtr(2010),
	}
	if err := repo.UpsertItemsBatch(ctx, []BatchItemInput{{
		Item:           mov,
		FTSTitleRaw:    "Inception",
		FTSTitleTokens: "inception",
	}}); err != nil {
		t.Fatal(err)
	}
	if err := repo.ReplaceItemTaxonomies(ctx, mov.ID, "genre", []string{"Sci-Fi"}); err != nil {
		t.Fatal(err)
	}

	// Build 40,000 IncludeItemTypes (> SQLite's 32766 SQLITE_MAX_VARIABLE_NUMBER).
	hugeTypes := make([]string, 40000)
	hugeTypes[0] = "Movie"
	for i := 1; i < len(hugeTypes); i++ {
		hugeTypes[i] = fmt.Sprintf("SyntheticType_%d", i)
	}

	hints, totalHints, err := repo.SearchHints(ctx, SearchHintFilter{
		SearchTerm:       "Inception",
		IncludeItemTypes: hugeTypes,
		Limit:            10,
	})
	if err != nil {
		t.Fatalf("SearchHints with 40k IncludeItemTypes failed: %v", err)
	}
	if totalHints != 1 || len(hints) != 1 || hints[0].Id != mov.ID {
		t.Fatalf("unexpected SearchHints result: total=%d hints=%+v", totalHints, hints)
	}

	qf, err := repo.GetQueryFilters(ctx, QueryFiltersFilter{
		ParentID:         lib.ID,
		IncludeItemTypes: hugeTypes,
	})
	if err != nil {
		t.Fatalf("GetQueryFilters with 40k IncludeItemTypes failed: %v", err)
	}
	if len(qf.Genres) != 1 || qf.Genres[0].Name != "Sci-Fi" {
		t.Fatalf("unexpected GetQueryFilters genres: %+v", qf.Genres)
	}

	vals, totalVals, err := repo.ListFilterValuesWithUserPolicy(
		ctx,
		"srv1",
		"OfficialRating",
		lib.ID,
		hugeTypes,
		0,
		50,
		nil,
		DefaultUserPolicy(true),
	)
	if err != nil {
		t.Fatalf("ListFilterValuesWithUserPolicy with 40k IncludeItemTypes failed: %v", err)
	}
	if totalVals != 1 || len(vals) != 1 || vals[0].Name != "PG-13" {
		t.Fatalf("unexpected ListFilterValuesWithUserPolicy result: total=%d vals=%+v", totalVals, vals)
	}
}

func TestPrivatePlaylistIsolationInLatestAndSimilar(t *testing.T) {
	ctx := context.Background()
	st := openTestStore(t)
	repo := NewLibraryRepo(st)
	userRepo := NewUserRepo(st)

	lib, err := repo.CreateLibrary(ctx, "Movies", "movies", nil)
	if err != nil {
		t.Fatal(err)
	}
	mov := &ItemRecord{ID: guid.New(), LibraryID: lib.ID, ParentID: lib.ID, Type: "Movie", Name: "Matrix", SortName: "matrix"}
	if err := repo.UpsertItem(ctx, mov); err != nil {
		t.Fatal(err)
	}

	alice := &UserRecord{ID: guid.New(), Name: "alice_priv", Policy: DefaultUserPolicy(false), Config: DefaultUserConfig()}
	bob := &UserRecord{ID: guid.New(), Name: "bob_priv", Policy: DefaultUserPolicy(false), Config: DefaultUserConfig()}
	if err := userRepo.CreateUser(ctx, alice); err != nil {
		t.Fatal(err)
	}
	if err := userRepo.CreateUser(ctx, bob); err != nil {
		t.Fatal(err)
	}

	alicePriv, err := repo.CreatePlaylist(ctx, "Alice Private", "Video", alice.ID, []string{mov.ID})
	if err != nil {
		t.Fatal(err)
	}
	bobSeed, err := repo.CreatePlaylist(ctx, "Bob Seed", "Video", bob.ID, []string{mov.ID})
	if err != nil {
		t.Fatal(err)
	}
	pubPl, err := repo.CreatePlaylist(ctx, "Public Mix", "Video", alice.ID, []string{mov.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Writer.ExecContext(ctx,
		`INSERT OR REPLACE INTO server_config(key, value_json, updated_at) VALUES(?, '{"IsPublic":true}', 1)`,
		"item_visibility_"+guid.Normalize(pubPl.ID),
	); err != nil {
		t.Fatal(err)
	}

	// 1. Bob must NEVER see Alice's private playlist in GetLatestItemsWithUserPolicy.
	latestBob, err := repo.GetLatestItemsWithUserPolicy(ctx, "srv1", bob.ID, "", []string{"Playlist"}, 20, nil, nil, bob.Policy)
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range latestBob {
		if guid.Normalize(it.Id) == guid.Normalize(alicePriv.ID) {
			t.Fatalf("Alice's private playlist leaked to Bob via GetLatestItemsWithUserPolicy")
		}
	}

	// 2. Empty userID + non-admin policy must ONLY see public playlists in GetLatestItemsWithUserPolicy.
	latestAnon, err := repo.GetLatestItemsWithUserPolicy(ctx, "srv1", "", "", []string{"Playlist"}, 20, nil, nil, DefaultUserPolicy(false))
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range latestAnon {
		if guid.Normalize(it.Id) == guid.Normalize(alicePriv.ID) || guid.Normalize(it.Id) == guid.Normalize(bobSeed.ID) {
			t.Fatalf("private playlist %q leaked to unauthenticated non-admin GetLatestItemsWithUserPolicy", it.Name)
		}
	}

	// 3. Bob must NEVER see Alice's private playlist in GetSimilarItemsWithUserPolicy.
	similarBob, _, err := repo.GetSimilarItemsWithUserPolicy(ctx, "srv1", bob.ID, bobSeed.ID, 20, nil, nil, bob.Policy)
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range similarBob {
		if guid.Normalize(it.Id) == guid.Normalize(alicePriv.ID) {
			t.Fatalf("Alice's private playlist leaked to Bob via GetSimilarItemsWithUserPolicy")
		}
	}

	// 4. Empty userID + non-admin policy seeded from public playlist must NEVER see private playlists in GetSimilarItemsWithUserPolicy.
	similarAnon, _, err := repo.GetSimilarItemsWithUserPolicy(ctx, "srv1", "", pubPl.ID, 20, nil, nil, DefaultUserPolicy(false))
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range similarAnon {
		if guid.Normalize(it.Id) == guid.Normalize(alicePriv.ID) || guid.Normalize(it.Id) == guid.Normalize(bobSeed.ID) {
			t.Fatalf("private playlist %q leaked to unauthenticated non-admin GetSimilarItemsWithUserPolicy", it.Name)
		}
	}
}

func TestLocalExtrasParentPolicyInheritanceAndVirtualLibraryPolicy(t *testing.T) {
	ctx := context.Background()
	st := openTestStore(t)
	repo := NewLibraryRepo(st)
	userRepo := NewUserRepo(st)

	lib, err := repo.CreateLibrary(ctx, "Movies", "movies", nil)
	if err != nil {
		t.Fatal(err)
	}
	gMovie := &ItemRecord{
		ID:             guid.New(),
		LibraryID:      lib.ID,
		ParentID:       lib.ID,
		Type:           "Movie",
		Name:           "Kids Movie",
		SortName:       "kids movie",
		OfficialRating: "G",
	}
	rMovie := &ItemRecord{
		ID:             guid.New(),
		LibraryID:      lib.ID,
		ParentID:       lib.ID,
		Type:           "Movie",
		Name:           "Adult Movie",
		SortName:       "adult movie",
		OfficialRating: "R",
	}
	gTrailer := &ItemRecord{
		ID:        guid.New(),
		LibraryID: lib.ID,
		ParentID:  gMovie.ID,
		Type:      "Video",
		Name:      "Kids Trailer",
		SortName:  "kids trailer",
		Extra:     ItemExtra{ExtraType: "Trailer"},
	}
	rTrailer := &ItemRecord{
		ID:        guid.New(),
		LibraryID: lib.ID,
		ParentID:  rMovie.ID,
		Type:      "Video",
		Name:      "Adult Trailer",
		SortName:  "adult trailer",
		Extra:     ItemExtra{ExtraType: "Trailer"},
	}
	for _, rec := range []*ItemRecord{gMovie, rMovie, gTrailer, rTrailer} {
		if err := repo.UpsertItem(ctx, rec); err != nil {
			t.Fatal(err)
		}
	}
	if err := repo.ReplaceItemTaxonomies(ctx, gMovie.ID, "tag", []string{"kids"}); err != nil {
		t.Fatal(err)
	}
	if err := repo.ReplaceItemTaxonomies(ctx, rMovie.ID, "tag", []string{"gore"}); err != nil {
		t.Fatal(err)
	}

	maxPG := ParseParentalRatingValue("PG")
	kidsPol := dto.UserPolicy{
		EnableAllFolders:   false,
		EnabledFolders:     []string{lib.ID},
		EnableLiveTvAccess: true,
		MaxParentalRating:  &maxPG,
		BlockUnratedItems:  []string{"All"},
		AllowedTags:        []string{"kids"},
		BlockedTags:        []string{"gore"},
	}

	// Trailer of R-rated movie must be blocked via parent inheritance.
	if blocked, err := repo.IsItemBlockedByPolicy(ctx, rTrailer, kidsPol); err != nil || !blocked {
		t.Fatalf("expected R-rated movie trailer to be blocked, got blocked=%v err=%v", blocked, err)
	}
	// Trailer of G-rated movie inherits "G" rating so it is NOT blocked by BlockUnratedItems=["All"] / MaxParentalRating=PG.
	kidsRatingPol := kidsPol
	kidsRatingPol.AllowedTags = nil
	if blocked, err := repo.IsItemBlockedByPolicy(ctx, gTrailer, kidsRatingPol); err != nil || blocked {
		t.Fatalf("expected G-rated trailer to inherit rating and be allowed under BlockUnratedItems=[All], got blocked=%v err=%v", blocked, err)
	}
	// Under strict AllowedTags allowlist, an untagged extra stays blocked until explicitly tagged.
	if blocked, err := repo.IsItemBlockedByPolicy(ctx, gTrailer, kidsPol); err != nil || !blocked {
		t.Fatalf("expected untagged extra to be blocked under strict AllowedTags, got blocked=%v err=%v", blocked, err)
	}
	if err := repo.ReplaceItemTaxonomies(ctx, gTrailer.ID, "tag", []string{"kids"}); err != nil {
		t.Fatal(err)
	}
	if blocked, err := repo.IsItemBlockedByPolicy(ctx, gTrailer, kidsPol); err != nil || blocked {
		t.Fatalf("expected G-rated kids-tagged trailer to be allowed, got blocked=%v err=%v", blocked, err)
	}

	// Virtual libraries "collections" and "playlists" must be accessible to folder-restricted users.
	if !repo.CanUserAccessLibrary(ctx, kidsPol, "collections") || !repo.CanUserAccessLibrary(ctx, kidsPol, "playlists") {
		t.Fatalf("expected virtual libraries collections/playlists to be accessible in CanUserAccessLibrary")
	}

	// Playlist container must NOT be falsely blocked by BlockUnratedItems=["All"] or AllowedTags=["kids"].
	pl, err := repo.CreatePlaylist(ctx, "Kids Mix", "Video", "", []string{gMovie.ID})
	if err != nil {
		t.Fatal(err)
	}
	if blocked, err := repo.IsItemBlockedByPolicy(ctx, pl, kidsPol); err != nil || blocked {
		t.Fatalf("expected Playlist container not to be falsely blocked by BlockUnratedItems=[All]/AllowedTags, got blocked=%v err=%v", blocked, err)
	}

	// loadRestrictedUserPolicy must fail closed on missing user or corrupted policy_json.
	_, missLibs, missRestricted := repo.loadRestrictedUserPolicy(ctx, guid.New())
	if !missRestricted || missLibs == nil || len(missLibs) != 0 {
		t.Fatalf("expected missing user to fail closed, got libs=%v restricted=%v", missLibs, missRestricted)
	}
	badUser := &UserRecord{ID: guid.New(), Name: "bad_json_user", Policy: DefaultUserPolicy(false)}
	if err := userRepo.CreateUser(ctx, badUser); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Writer.ExecContext(ctx, `UPDATE users SET policy_json = '{not-json' WHERE id = ?`, badUser.ID); err != nil {
		t.Fatal(err)
	}
	_, badLibs, badRestricted := repo.loadRestrictedUserPolicy(ctx, badUser.ID)
	if !badRestricted || badLibs == nil || len(badLibs) != 0 {
		t.Fatalf("expected corrupted policy_json to fail closed, got libs=%v restricted=%v", badLibs, badRestricted)
	}

	// QueryItems with empty UserID and IsPlayed=true / IsFavorite=true / IsResumable=true must return 0 items.
	tr := true
	for _, tc := range []struct {
		name string
		f    ItemFilter
	}{
		{"IsPlayed", ItemFilter{IsPlayed: &tr}},
		{"IsFavorite", ItemFilter{IsFavorite: &tr}},
		{"IsResumable", ItemFilter{IsResumable: &tr}},
		{"IsLiked", ItemFilter{IsLiked: &tr}},
	} {
		items, total, err := repo.QueryItems(ctx, tc.f)
		if err != nil || total != 0 || len(items) != 0 {
			t.Fatalf("expected empty result for %s with empty UserID, got total=%d len=%d err=%v", tc.name, total, len(items), err)
		}
	}
}

func TestGetItemCountsForUserTrailerParentPolicyAndEmptyUserPlaylistVisibility(t *testing.T) {
	ctx := context.Background()
	st := openTestStore(t)
	repo := NewLibraryRepo(st)

	libAllowed, err := repo.CreateLibrary(ctx, "AllowedMovies", "movies", nil)
	if err != nil {
		t.Fatal(err)
	}
	libSecret, err := repo.CreateLibrary(ctx, "SecretMovies", "movies", nil)
	if err != nil {
		t.Fatal(err)
	}

	gMovie := &ItemRecord{
		ID:             guid.New(),
		LibraryID:      libAllowed.ID,
		ParentID:       libAllowed.ID,
		Type:           "Movie",
		MediaType:      "Video",
		Name:           "Family Adventure",
		SortName:       "family adventure",
		OfficialRating: "G",
	}
	rMovie := &ItemRecord{
		ID:             guid.New(),
		LibraryID:      libAllowed.ID,
		ParentID:       libAllowed.ID,
		Type:           "Movie",
		MediaType:      "Video",
		Name:           "Restricted Action",
		SortName:       "restricted action",
		OfficialRating: "R",
	}
	taggedMovie := &ItemRecord{
		ID:             guid.New(),
		LibraryID:      libAllowed.ID,
		ParentID:       libAllowed.ID,
		Type:           "Movie",
		MediaType:      "Video",
		Name:           "Tagged Horror",
		SortName:       "tagged horror",
		OfficialRating: "G",
	}
	secretMovie := &ItemRecord{
		ID:             guid.New(),
		LibraryID:      libSecret.ID,
		ParentID:       libSecret.ID,
		Type:           "Movie",
		MediaType:      "Video",
		Name:           "Secret Movie",
		SortName:       "secret movie",
		OfficialRating: "G",
	}

	// Unrated local trailers attached to each movie.
	gTrailer := &ItemRecord{
		ID:        guid.New(),
		LibraryID: libAllowed.ID,
		ParentID:  gMovie.ID,
		Type:      "Video",
		MediaType: "Video",
		Extra:     ItemExtra{ExtraType: "Trailer"},
		Name:      "Family Trailer",
		SortName:  "family trailer",
	}
	rTrailer := &ItemRecord{
		ID:        guid.New(),
		LibraryID: libAllowed.ID,
		ParentID:  rMovie.ID,
		Type:      "Video",
		MediaType: "Video",
		Extra:     ItemExtra{ExtraType: "Trailer"},
		Name:      "Restricted Trailer",
		SortName:  "restricted trailer",
	}
	taggedTrailer := &ItemRecord{
		ID:        guid.New(),
		LibraryID: libAllowed.ID,
		ParentID:  taggedMovie.ID,
		Type:      "Video",
		MediaType: "Video",
		Extra:     ItemExtra{ExtraType: "Trailer"},
		Name:      "Tagged Trailer",
		SortName:  "tagged trailer",
	}
	secretTrailer := &ItemRecord{
		ID:        guid.New(),
		LibraryID: libAllowed.ID, // even if library_id matches, parent is in libSecret
		ParentID:  secretMovie.ID,
		Type:      "Video",
		MediaType: "Video",
		Extra:     ItemExtra{ExtraType: "Trailer"},
		Name:      "Secret Trailer",
		SortName:  "secret trailer",
	}

	for _, rec := range []*ItemRecord{gMovie, rMovie, taggedMovie, secretMovie, gTrailer, rTrailer, taggedTrailer, secretTrailer} {
		if err := repo.UpsertItem(ctx, rec); err != nil {
			t.Fatal(err)
		}
	}
	if err := repo.ReplaceItemTaxonomies(ctx, taggedMovie.ID, "tag", []string{"horror"}); err != nil {
		t.Fatal(err)
	}

	// Create one private playlist owned by ownerID and one public playlist owned by ownerID.
	ownerID := guid.New()
	privPl, err := repo.CreatePlaylist(ctx, "Owner Private Mix", "Video", ownerID, []string{gMovie.ID})
	if err != nil {
		t.Fatal(err)
	}
	pubPl, err := repo.CreatePlaylist(ctx, "Public Mix", "Video", ownerID, []string{gMovie.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Writer.ExecContext(ctx,
		`INSERT OR REPLACE INTO server_config(key, value_json, updated_at) VALUES(?, '{"IsPublic":true}', 1)`,
		"item_visibility_"+guid.Normalize(pubPl.ID),
	); err != nil {
		t.Fatal(err)
	}

	// 1. Restricted user policy: maxParentalRating=G(1), BlockedTags=["horror"], allowedLibraryIDs=[libAllowed.ID].
	// Only gMovie's trailer should be counted in TrailerCount (rTrailer, taggedTrailer, secretTrailer excluded).
	maxG := 1
	restrictedPol := dto.UserPolicy{
		IsAdministrator:    false,
		EnableAllFolders:   false,
		EnabledFolders:     []string{libAllowed.ID},
		EnableLiveTvAccess: true,
		MaxParentalRating:  &maxG,
		BlockedTags:        []string{"horror"},
	}
	counts, err := repo.GetItemCountsForUser(ctx, ownerID, false, []string{libAllowed.ID}, restrictedPol)
	if err != nil {
		t.Fatalf("GetItemCountsForUser restricted: %v", err)
	}
	if counts.MovieCount != 1 {
		t.Fatalf("expected MovieCount=1, got %d", counts.MovieCount)
	}
	if counts.TrailerCount != 1 {
		t.Fatalf("expected TrailerCount=1 (only permitted movie's trailer), got %d", counts.TrailerCount)
	}

	// 2. Non-admin with empty userID must hide private playlists in GetItemCountsForUser (only public playlist counted).
	anonPol := dto.UserPolicy{
		IsAdministrator:    false,
		EnableAllFolders:   true,
		EnableLiveTvAccess: true,
	}
	anonCounts, err := repo.GetItemCountsForUser(ctx, "", false, nil, anonPol)
	if err != nil {
		t.Fatalf("GetItemCountsForUser empty userID: %v", err)
	}
	ownerCounts, err := repo.GetItemCountsForUser(ctx, ownerID, false, nil, anonPol)
	if err != nil {
		t.Fatalf("GetItemCountsForUser owner: %v", err)
	}
	if ownerCounts.ItemCount != anonCounts.ItemCount+1 {
		t.Fatalf("expected owner to see 1 more item (private playlist) than empty-userID non-admin: owner=%d anon=%d",
			ownerCounts.ItemCount, anonCounts.ItemCount)
	}

	// 3. GetContainerItemsWithUserPolicy with empty userID and !IsAdministrator must hide private playlists
	// inside a container while still returning public playlists.
	box, err := repo.CreateCollection(ctx, "Mixed Container", "", []string{privPl.ID, pubPl.ID})
	if err != nil {
		t.Fatal(err)
	}
	anonItems, anonTotal, err := repo.GetContainerItemsWithUserPolicy(ctx, "srv1", "", box.ID, 0, 50, nil, nil, anonPol)
	if err != nil {
		t.Fatalf("GetContainerItemsWithUserPolicy empty userID: %v", err)
	}
	if anonTotal != 1 || len(anonItems) != 1 || guid.Normalize(anonItems[0].Id) != guid.Normalize(pubPl.ID) {
		t.Fatalf("expected only public playlist %s for empty-userID non-admin, got total=%d items=%+v", pubPl.ID, anonTotal, anonItems)
	}
	ownerItems, ownerTotal, err := repo.GetContainerItemsWithUserPolicy(ctx, "srv1", ownerID, box.ID, 0, 50, nil, nil, anonPol)
	if err != nil {
		t.Fatalf("GetContainerItemsWithUserPolicy owner: %v", err)
	}
	if ownerTotal != 2 || len(ownerItems) != 2 {
		t.Fatalf("expected owner to see both playlists (2), got total=%d len=%d", ownerTotal, len(ownerItems))
	}
}


