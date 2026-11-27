package sqlite

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/ambra-media/ambra/internal/api/dto"
	"github.com/ambra-media/ambra/internal/pkg/guid"
)

func TestAuditV0112_LiveTvPolicyAndWithUserPolicyPushdown(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	store, err := Open(filepath.Join(tmp, "audit_v0112.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	repo := NewLibraryRepo(store)
	userRepo := NewUserRepo(store)

	moviesDir := filepath.Join(tmp, "movies")
	liveDir := filepath.Join(tmp, "livetv")
	_ = os.MkdirAll(moviesDir, 0o755)
	_ = os.MkdirAll(liveDir, 0o755)

	movLib, err := repo.CreateLibrary(ctx, "Movies", "movies", []string{moviesDir})
	if err != nil {
		t.Fatal(err)
	}
	liveLib, err := repo.CreateLibrary(ctx, "Live TV", "livetv", []string{liveDir})
	if err != nil {
		t.Fatal(err)
	}

	// H-04: Non-admin with EnableAllFolders=true and EnableLiveTvAccess=false MUST NOT access livetv library.
	noLivePol := DefaultUserPolicy(false)
	noLivePol.EnableAllFolders = true
	noLivePol.EnableLiveTvAccess = false

	if !repo.CanUserAccessLibrary(ctx, noLivePol, movLib.ID) {
		t.Fatal("expected non-admin to access Movies library")
	}
	if repo.CanUserAccessLibrary(ctx, noLivePol, liveLib.ID) {
		t.Fatal("expected CanUserAccessLibrary=false on livetv library when EnableLiveTvAccess=false")
	}
	allowedIDs := repo.AllowedLibraryIDsForPolicy(ctx, noLivePol)
	if allowedIDs == nil || len(allowedIDs) != 1 || guid.Normalize(allowedIDs[0]) != guid.Normalize(movLib.ID) {
		t.Fatalf("expected explicit non-livetv library slice [%s], got %v", movLib.ID, allowedIDs)
	}

	adminPol := DefaultUserPolicy(true)
	if !repo.CanUserAccessLibrary(ctx, adminPol, liveLib.ID) || repo.AllowedLibraryIDsForPolicy(ctx, adminPol) != nil {
		t.Fatal("expected admin to have unrestricted nil AllowedLibraryIDsForPolicy and access to livetv")
	}

	liveChannel := &ItemRecord{
		ID:             guid.New(),
		LibraryID:      liveLib.ID,
		ParentID:       liveLib.ID,
		Type:           "TvChannel",
		MediaType:      "Video",
		Name:           "HBO Live",
		SortName:       "hbo live",
		OfficialRating: "G",
	}
	if err := repo.UpsertItem(ctx, liveChannel); err != nil {
		t.Fatal(err)
	}
	if blocked, err := repo.IsItemBlockedByPolicy(ctx, liveChannel, noLivePol); err != nil || !blocked {
		t.Fatalf("expected IsItemBlockedByPolicy=true for livetv item when EnableLiveTvAccess=false, got (%v, %v)", blocked, err)
	}

	// H-05: Seed rated+tagged, unrated, and wrong-tag items to verify *WithUserPolicy SQL pushdown.
	yr2024 := 2024
	yr2025 := 2025
	movApproved := &ItemRecord{
		ID:             guid.New(),
		LibraryID:      movLib.ID,
		ParentID:       movLib.ID,
		Type:           "Movie",
		MediaType:      "Video",
		Name:           "Approved Movie",
		SortName:       "approved movie",
		OfficialRating: "PG",
		ProductionYear: &yr2024,
	}
	movApproved2 := &ItemRecord{
		ID:             guid.New(),
		LibraryID:      movLib.ID,
		ParentID:       movLib.ID,
		Type:           "Movie",
		MediaType:      "Video",
		Name:           "Approved Sequel",
		SortName:       "approved sequel",
		OfficialRating: "G",
		ProductionYear: &yr2024,
	}
	movUnrated := &ItemRecord{
		ID:             guid.New(),
		LibraryID:      movLib.ID,
		ParentID:       movLib.ID,
		Type:           "Movie",
		MediaType:      "Video",
		Name:           "Unrated Movie",
		SortName:       "unrated movie",
		ProductionYear: &yr2025,
	}
	movWrongTag := &ItemRecord{
		ID:             guid.New(),
		LibraryID:      movLib.ID,
		ParentID:       movLib.ID,
		Type:           "Movie",
		MediaType:      "Video",
		Name:           "WrongTag Movie",
		SortName:       "wrongtag movie",
		OfficialRating: "G",
		ProductionYear: &yr2025,
	}
	for _, it := range []*ItemRecord{movApproved, movApproved2, movUnrated, movWrongTag} {
		if err := repo.UpsertItem(ctx, it); err != nil {
			t.Fatal(err)
		}
	}
	_ = repo.ReplaceItemTaxonomies(ctx, movApproved.ID, "tag", []string{"kids-ok"})
	_ = repo.ReplaceItemTaxonomies(ctx, movApproved2.ID, "tag", []string{"kids-ok"})
	_ = repo.ReplaceItemTaxonomies(ctx, movUnrated.ID, "tag", []string{"kids-ok"})
	_ = repo.ReplaceItemTaxonomies(ctx, movWrongTag.ID, "tag", []string{"adults-only"})

	_ = repo.ReplaceItemTaxonomies(ctx, movApproved.ID, "genre", []string{"Animation"})
	_ = repo.ReplaceItemTaxonomies(ctx, movApproved2.ID, "genre", []string{"Animation"})
	_ = repo.ReplaceItemTaxonomies(ctx, movUnrated.ID, "genre", []string{"HorrorUnrated"})
	_ = repo.ReplaceItemTaxonomies(ctx, movWrongTag.ID, "genre", []string{"WrongTagGenre"})

	maxPG := 5
	kidPol := dto.UserPolicy{
		EnableAllFolders:   true,
		EnableLiveTvAccess: false,
		MaxParentalRating:  &maxPG,
		BlockUnratedItems:  []string{"Movie", "Series"},
		AllowedTags:        []string{"kids-ok"},
	}
	kid := &UserRecord{
		ID:     guid.New(),
		Name:   "kid_v0112",
		Policy: kidPol,
		Config: DefaultUserConfig(),
	}
	if err := userRepo.CreateUser(ctx, kid); err != nil {
		t.Fatal(err)
	}
	kidAllowed := repo.AllowedLibraryIDsForPolicy(ctx, kidPol)

	// Mark movApproved played & movApproved2 + movUnrated resumable
	if _, err := repo.MarkPlayedWithPolicy(ctx, kid.ID, movApproved.ID, kidPol); err != nil {
		t.Fatal(err)
	}
	_, _ = store.Writer.ExecContext(ctx,
		`INSERT OR REPLACE INTO user_item_data(user_id, item_id, played, play_count, playback_position_ticks, is_favorite) VALUES(?, ?, 0, 0, 50000, 0)`,
		guid.Normalize(kid.ID), guid.Normalize(movApproved2.ID),
	)
	_, _ = store.Writer.ExecContext(ctx,
		`INSERT OR REPLACE INTO user_item_data(user_id, item_id, played, play_count, playback_position_ticks, is_favorite) VALUES(?, ?, 0, 0, 50000, 0)`,
		guid.Normalize(kid.ID), guid.Normalize(movUnrated.ID),
	)

	latest, err := repo.GetLatestItemsWithUserPolicy(ctx, "srv", kid.ID, "", []string{"Movie"}, 20, nil, kidAllowed, kidPol)
	if err != nil || len(latest) != 2 {
		t.Fatalf("expected 2 latest items under UserPolicy, got len=%d err=%v", len(latest), err)
	}

	resumed, resTotal, err := repo.GetResumeItemsWithUserPolicy(ctx, "srv", kid.ID, "", nil, 0, 20, nil, kidAllowed, kidPol)
	if err != nil || resTotal != 1 || len(resumed) != 1 || guid.Normalize(resumed[0].Id) != guid.Normalize(movApproved2.ID) {
		t.Fatalf("expected only movApproved2 in ResumeItemsWithUserPolicy, got total=%d items=%+v err=%v", resTotal, resumed, err)
	}

	taxItems, taxTotal, err := repo.ListTaxonomiesWithUserPolicy(ctx, "srv", "genre", "", 0, 50, kidAllowed, kidPol)
	if err != nil || taxTotal != 1 || len(taxItems) != 1 || taxItems[0].Name != "Animation" {
		t.Fatalf("expected only Animation genre under UserPolicy, got total=%d items=%+v err=%v", taxTotal, taxItems, err)
	}

	simItems, simTotal, err := repo.GetSimilarItemsWithUserPolicy(ctx, "srv", kid.ID, movApproved.ID, 20, nil, kidAllowed, kidPol)
	if err != nil || simTotal != 1 || len(simItems) != 1 || guid.Normalize(simItems[0].Id) != guid.Normalize(movApproved2.ID) {
		t.Fatalf("expected only movApproved2 in SimilarItemsWithUserPolicy, got total=%d items=%+v err=%v", simTotal, simItems, err)
	}

	mixItems, _, err := repo.GetInstantMixWithUserPolicy(ctx, "srv", kid.ID, movApproved.ID, 20, nil, kidAllowed, kidPol)
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range mixItems {
		if guid.Normalize(it.Id) == guid.Normalize(movUnrated.ID) || guid.Normalize(it.Id) == guid.Normalize(movWrongTag.ID) {
			t.Fatalf("blocked item %s leaked in GetInstantMixWithUserPolicy", it.Name)
		}
	}

	recs, err := repo.GetMovieRecommendationsWithUserPolicy(ctx, "srv", kid.ID, 5, 10, nil, kidAllowed, kidPol)
	if err != nil {
		t.Fatal(err)
	}
	for _, cat := range recs {
		for _, it := range cat.Items {
			if guid.Normalize(it.Id) == guid.Normalize(movUnrated.ID) || guid.Normalize(it.Id) == guid.Normalize(movWrongTag.ID) {
				t.Fatalf("blocked item %s leaked in GetMovieRecommendationsWithUserPolicy", it.Name)
			}
		}
	}

	ratings, ratingsTotal, err := repo.ListFilterValuesWithUserPolicy(ctx, "srv", "OfficialRating", "", nil, 0, 50, kidAllowed, kidPol)
	if err != nil || ratingsTotal != 2 || len(ratings) != 2 {
		t.Fatalf("expected only G and PG from ListFilterValuesWithUserPolicy, got total=%d items=%+v err=%v", ratingsTotal, ratings, err)
	}

	qf, err := repo.GetQueryFilters(ctx, QueryFiltersFilter{
		AllowedLibraryIDs: kidAllowed,
		MaxParentalRating: kidPol.MaxParentalRating,
		BlockUnratedItems: kidPol.BlockUnratedItems,
		AllowedTags:       kidPol.AllowedTags,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(qf.Genres) != 1 || qf.Genres[0].Name != "Animation" || len(qf.Years) != 1 || qf.Years[0] != 2024 {
		t.Fatalf("GetQueryFilters leaked blocked genres/years: %+v", qf)
	}

	// M-14: MarkPlayedWithPolicy / MarkUnplayedWithPolicy on Series skips blocked episodes.
	series := &ItemRecord{
		ID: guid.New(), LibraryID: movLib.ID, ParentID: movLib.ID,
		Type: "Series", IsFolder: true, Name: "Mixed Series", SortName: "mixed series", OfficialRating: "PG",
	}
	s1 := 1
	epNum1, epNum2 := 1, 2
	epSafe := &ItemRecord{
		ID: guid.New(), LibraryID: movLib.ID, ParentID: series.ID, SeriesID: series.ID,
		Type: "Episode", MediaType: "Video", Name: "Safe Ep", OfficialRating: "G",
		ParentIndexNumber: &s1, IndexNumber: &epNum1,
	}
	epAdult := &ItemRecord{
		ID: guid.New(), LibraryID: movLib.ID, ParentID: series.ID, SeriesID: series.ID,
		Type: "Episode", MediaType: "Video", Name: "Adult Ep", OfficialRating: "R",
		ParentIndexNumber: &s1, IndexNumber: &epNum2,
	}
	for _, it := range []*ItemRecord{series, epSafe, epAdult} {
		_ = repo.UpsertItem(ctx, it)
	}
	_ = repo.ReplaceItemTaxonomies(ctx, series.ID, "tag", []string{"kids-ok"})
	_ = repo.ReplaceItemAncestors(ctx, epSafe.ID, []AncestorLink{{AncestorID: series.ID, Depth: 1}})
	_ = repo.ReplaceItemAncestors(ctx, epAdult.ID, []AncestorLink{{AncestorID: series.ID, Depth: 1}})

	if _, err := repo.MarkPlayedWithPolicy(ctx, kid.ID, series.ID, kidPol); err != nil {
		t.Fatalf("MarkPlayedWithPolicy on series failed: %v", err)
	}
	udSafe, _ := repo.GetUserItemData(ctx, kid.ID, epSafe.ID)
	udAdult, _ := repo.GetUserItemData(ctx, kid.ID, epAdult.ID)
	if !udSafe.Played || udAdult.Played {
		t.Fatalf("expected epSafe played=true and epAdult played=false, got safe=%v adult=%v", udSafe.Played, udAdult.Played)
	}
}

func TestAuditV0112_ValidateLibraryMountPath(t *testing.T) {
	base := t.TempDir()
	dataDir := filepath.Join(base, "ambra-data")
	mediaDir := filepath.Join(base, "media", "movies")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(mediaDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Valid disjoint media path and non-existent child directory under existing media root
	if got, err := ValidateLibraryMountPath(mediaDir, dataDir); err != nil || got == "" {
		t.Fatalf("expected valid mediaDir accepted, got (%q, %v)", got, err)
	}
	if _, err := ValidateLibraryMountPath(filepath.Join(mediaDir, "new-subfolder"), dataDir); err != nil {
		t.Fatalf("expected non-existent child under valid mediaDir accepted, got %v", err)
	}

	// Reject empty, relative, control chars, root, OS system dirs, dataDir equal/parent/child, and symlink to /etc or dataDir
	symToEtc := filepath.Join(base, "link-etc")
	_ = os.Symlink("/etc", symToEtc)
	symToData := filepath.Join(base, "link-data")
	_ = os.Symlink(dataDir, symToData)

	badPaths := []string{
		"",
		"relative/movies",
		".",
		"..",
		mediaDir + "\x00evil",
		"/",
		"/etc",
		"/etc/passwd",
		"/proc",
		"/sys",
		"/dev",
		"/usr/bin",
		"/var/run",
		dataDir,
		filepath.Join(dataDir, "plugins"),
		base, // parent directory containing dataDir!
		symToEtc,
		symToData,
	}
	for _, bp := range badPaths {
		if _, err := ValidateLibraryMountPath(bp, dataDir); err == nil {
			t.Fatalf("expected ValidateLibraryMountPath(%q, %q) to fail, but it succeeded", bp, dataDir)
		}
	}
}

func TestAuditV0112_PlaylistSQLPushdownLargeIDSetAndUserDeletionCleanup(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	store, err := Open(filepath.Join(tmp, "playlists_v0112.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	repo := NewLibraryRepo(store)
	userRepo := NewUserRepo(store)

	libDir := filepath.Join(tmp, "media")
	_ = os.MkdirAll(libDir, 0o755)
	lib, _ := repo.CreateLibrary(ctx, "Movies", "movies", []string{libDir})

	mov := &ItemRecord{ID: guid.New(), LibraryID: lib.ID, ParentID: lib.ID, Type: "Movie", Name: "Matrix", SortName: "matrix"}
	_ = repo.UpsertItem(ctx, mov)

	alice := &UserRecord{ID: guid.New(), Name: "alice_pl", Policy: DefaultUserPolicy(false), Config: DefaultUserConfig()}
	bob := &UserRecord{ID: guid.New(), Name: "bob_pl", Policy: DefaultUserPolicy(false), Config: DefaultUserConfig()}
	_ = userRepo.CreateUser(ctx, alice)
	_ = userRepo.CreateUser(ctx, bob)

	alicePriv, _ := repo.CreatePlaylist(ctx, "Alice Private Mix", "Video", alice.ID, []string{mov.ID})
	bobPriv, _ := repo.CreatePlaylist(ctx, "Bob Secret Mix", "Video", bob.ID, []string{mov.ID})
	bobPub, _ := repo.CreatePlaylist(ctx, "Bob Public Mix", "Video", bob.ID, []string{mov.ID})
	_, _ = store.Writer.ExecContext(ctx,
		`INSERT OR REPLACE INTO server_config(key, value_json, updated_at) VALUES(?, '{"IsPublic":true}', 1)`,
		"item_visibility_"+guid.Normalize(bobPub.ID),
	)

	// 1. H-10: SQL playlist pushdown for Alice sees Alice Private + Bob Public, NOT Bob Secret.
	items, total, err := repo.QueryItems(ctx, ItemFilter{
		ServerID:              "srv",
		UserID:                alice.ID,
		PlaylistViewerUserID:  alice.ID,
		PlaylistViewerIsAdmin: false,
		IncludeItemTypes:      []string{"Playlist"},
	})
	if err != nil || total != 2 || len(items) != 2 {
		t.Fatalf("expected 2 visible playlists for Alice, got total=%d len=%d err=%v", total, len(items), err)
	}
	for _, it := range items {
		if guid.Normalize(it.Id) == guid.Normalize(bobPriv.ID) {
			t.Fatal("Bob's private playlist leaked to Alice in QueryItems")
		}
	}

	hints, hTotal, err := repo.SearchHints(ctx, SearchHintFilter{
		SearchTerm:            "Mix",
		PlaylistViewerUserID:  alice.ID,
		PlaylistViewerIsAdmin: false,
		IncludeItemTypes:      []string{"Playlist"},
	})
	if err != nil || hTotal != 2 || len(hints) != 2 {
		t.Fatalf("expected 2 visible playlist hints for Alice, got total=%d len=%d err=%v", hTotal, len(hints), err)
	}

	// 2. H-10: Passing 35,000 IDs and 35,000 ExcludeItemIDs (> SQLite 32,766 variable limit) must succeed via json_each!
	hugeIDs := make([]string, 35000)
	hugeExcludes := make([]string, 35000)
	for i := 0; i < 35000; i++ {
		hugeIDs[i] = fmt.Sprintf("000000000000000000000000%08x", i+1)
		hugeExcludes[i] = fmt.Sprintf("ffffffffffffffffffffffff%08x", i+1)
	}
	hugeIDs[17500] = mov.ID
	gotHuge, gotHugeTotal, err := repo.QueryItems(ctx, ItemFilter{
		ServerID:       "srv",
		IDs:            hugeIDs,
		ExcludeItemIDs: hugeExcludes,
		Limit:          10,
	})
	if err != nil || gotHugeTotal != 1 || len(gotHuge) != 1 || guid.Normalize(gotHuge[0].Id) != guid.Normalize(mov.ID) {
		t.Fatalf("QueryItems with 35,000 IDs + 35,000 ExcludeItemIDs failed: total=%d len=%d err=%v", gotHugeTotal, len(gotHuge), err)
	}

	// 3. L-08: Deleting Bob purges Bob's playlists, item_collections, FTS, and server_config item_visibility_<id>.
	if err := userRepo.DeleteUser(ctx, bob.ID); err != nil {
		t.Fatalf("DeleteUser(bob) failed: %v", err)
	}
	for _, deletedPL := range []string{bobPriv.ID, bobPub.ID} {
		if _, err := repo.GetItemRecordByID(ctx, deletedPL); err == nil {
			t.Fatalf("expected playlist %s deleted with owner Bob", deletedPL)
		}
		var visCount int
		_ = store.Reader.QueryRowContext(ctx, `SELECT COUNT(*) FROM server_config WHERE key = ?`, "item_visibility_"+guid.Normalize(deletedPL)).Scan(&visCount)
		if visCount != 0 {
			t.Fatalf("expected item_visibility_%s purged from server_config", deletedPL)
		}
		var colCount int
		_ = store.Reader.QueryRowContext(ctx, `SELECT COUNT(*) FROM item_collections WHERE container_id = ?`, guid.Normalize(deletedPL)).Scan(&colCount)
		if colCount != 0 {
			t.Fatalf("expected item_collections purged for deleted playlist %s", deletedPL)
		}
	}
	// Alice's playlist remains intact.
	if _, err := repo.GetItemRecordByID(ctx, alicePriv.ID); err != nil {
		t.Fatalf("expected Alice's playlist untouched, got %v", err)
	}
}
