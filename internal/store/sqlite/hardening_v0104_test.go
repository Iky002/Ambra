package sqlite

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/ambra-media/ambra/internal/pkg/guid"
)

func TestQueryItemsExplicitFalseUserFlags(t *testing.T) {
	ctx := context.Background()
	store, err := Open(filepath.Join(t.TempDir(), "query_flags_v0104.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	userRepo := NewUserRepo(store)
	libRepo := NewLibraryRepo(store)
	user := &UserRecord{ID: guid.New(), Name: "viewer", Policy: DefaultUserPolicy(false), Config: DefaultUserConfig()}
	if err := userRepo.CreateUser(ctx, user); err != nil {
		t.Fatal(err)
	}
	lib, err := libRepo.CreateLibrary(ctx, "Movies", "movies", []string{"/movies"})
	if err != nil {
		t.Fatal(err)
	}

	mPlayedFav := &ItemRecord{ID: guid.New(), LibraryID: lib.ID, ParentID: lib.ID, Type: "Movie", Name: "PlayedFav"}
	mResumable := &ItemRecord{ID: guid.New(), LibraryID: lib.ID, ParentID: lib.ID, Type: "Movie", Name: "Resumable"}
	mUntouched := &ItemRecord{ID: guid.New(), LibraryID: lib.ID, ParentID: lib.ID, Type: "Movie", Name: "Untouched"}
	for _, it := range []*ItemRecord{mPlayedFav, mResumable, mUntouched} {
		if err := libRepo.UpsertItem(ctx, it); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := libRepo.MarkPlayed(ctx, user.ID, mPlayedFav.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := libRepo.MarkFavorite(ctx, user.ID, mPlayedFav.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := userRepo.UpsertPlaybackProgress(ctx, user.ID, mResumable.ID, 12345, false, nil, nil); err != nil {
		t.Fatal(err)
	}

	bFalse := false
	bTrue := true

	// IsPlayed=false -> excludes mPlayedFav (returns mResumable, mUntouched)
	items, total, err := libRepo.QueryItems(ctx, ItemFilter{UserID: user.ID, LibraryID: lib.ID, IsPlayed: &bFalse})
	if err != nil || total != 2 || len(items) != 2 {
		t.Fatalf("IsPlayed=false: got total=%d len=%d err=%v", total, len(items), err)
	}
	for _, it := range items {
		if it.Id == mPlayedFav.ID {
			t.Fatalf("IsPlayed=false must exclude played item %s", mPlayedFav.ID)
		}
	}

	// IsUnplayed=false -> returns ONLY played items (mPlayedFav)
	items, total, err = libRepo.QueryItems(ctx, ItemFilter{UserID: user.ID, LibraryID: lib.ID, IsUnplayed: &bFalse})
	if err != nil || total != 1 || len(items) != 1 || items[0].Id != mPlayedFav.ID {
		t.Fatalf("IsUnplayed=false: want only mPlayedFav, got total=%d items=%+v err=%v", total, items, err)
	}

	// IsFavorite=false -> excludes mPlayedFav (returns mResumable, mUntouched)
	items, total, err = libRepo.QueryItems(ctx, ItemFilter{UserID: user.ID, LibraryID: lib.ID, IsFavorite: &bFalse})
	if err != nil || total != 2 || len(items) != 2 {
		t.Fatalf("IsFavorite=false: got total=%d len=%d err=%v", total, len(items), err)
	}
	for _, it := range items {
		if it.Id == mPlayedFav.ID {
			t.Fatalf("IsFavorite=false must exclude favorite item %s", mPlayedFav.ID)
		}
	}

	// IsResumable=false -> excludes mResumable (returns mPlayedFav, mUntouched)
	items, total, err = libRepo.QueryItems(ctx, ItemFilter{UserID: user.ID, LibraryID: lib.ID, IsResumable: &bFalse})
	if err != nil || total != 2 || len(items) != 2 {
		t.Fatalf("IsResumable=false: got total=%d len=%d err=%v", total, len(items), err)
	}
	for _, it := range items {
		if it.Id == mResumable.ID {
			t.Fatalf("IsResumable=false must exclude resumable item %s", mResumable.ID)
		}
	}

	// Positive sanity check: IsResumable=true -> only mResumable
	items, total, err = libRepo.QueryItems(ctx, ItemFilter{UserID: user.ID, LibraryID: lib.ID, IsResumable: &bTrue})
	if err != nil || total != 1 || len(items) != 1 || items[0].Id != mResumable.ID {
		t.Fatalf("IsResumable=true: want only mResumable, got total=%d items=%+v err=%v", total, items, err)
	}
}

func TestBuildFTSQueryStripsControlCharsAndRunsSafely(t *testing.T) {
	ctx := context.Background()
	store, err := Open(filepath.Join(t.TempDir(), "fts_ctrl_v0104.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	repo := NewLibraryRepo(store)
	lib, _ := repo.CreateLibrary(ctx, "Movies", "movies", []string{"/movies"})
	m := &ItemRecord{ID: guid.New(), LibraryID: lib.ID, ParentID: lib.ID, Type: "Movie", Name: "Matrix Reloaded"}
	if err := repo.UpsertItem(ctx, m); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpsertSearchFTS(ctx, m.ID, m.Name, m.Name, "", "", ""); err != nil {
		t.Fatal(err)
	}

	// Pure control chars yield empty FTS query.
	if q := BuildFTSQuery("\x00\x01\x02\x1f\x7f"); q != "" {
		t.Fatalf("expected empty FTS query for pure control chars, got %q", q)
	}

	// Mixed control characters + valid terms must strip control chars and match cleanly without SQL/FTS5 error.
	rawTerm := "Mat\x00rix\x07\x1bRelo\x7faded"
	q := BuildFTSQuery(rawTerm)
	for _, r := range q {
		if r < 0x20 || r == 0x7f {
			t.Fatalf("FTS query still contains control char %U: %q", r, q)
		}
	}
	if !strings.Contains(q, `"mat"*`) || !strings.Contains(q, `"rix"*`) {
		t.Fatalf("unexpected FTS query after stripping control chars: %q", q)
	}

	items, total, err := repo.QueryItems(ctx, ItemFilter{
		LibraryID:  lib.ID,
		SearchTerm: "Matrix\x00\x1f\x7f",
	})
	if err != nil || total != 1 || len(items) != 1 {
		t.Fatalf("FTS search with control chars failed: total=%d len=%d err=%v", total, len(items), err)
	}
}

func TestMergeCrossLibraryRejectedAndSplitAndUpsertFTSTransactional(t *testing.T) {
	ctx := context.Background()
	store, err := Open(filepath.Join(t.TempDir(), "mv_v0104.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	repo := NewLibraryRepo(store)
	lib1, _ := repo.CreateLibrary(ctx, "Movies A", "movies", []string{"/a"})
	lib2, _ := repo.CreateLibrary(ctx, "Movies B", "movies", []string{"/b"})

	m1 := &ItemRecord{ID: guid.New(), LibraryID: lib1.ID, ParentID: lib1.ID, Type: "Movie", Name: "Dune", Path: "/a/dune.mkv"}
	m2 := &ItemRecord{ID: guid.New(), LibraryID: lib2.ID, ParentID: lib2.ID, Type: "Movie", Name: "Dune", Path: "/b/dune.mkv"}
	m3 := &ItemRecord{ID: guid.New(), LibraryID: lib1.ID, ParentID: lib1.ID, Type: "Movie", Name: "Dune 4K", Path: "/a/dune-4k.mkv"}
	for _, it := range []*ItemRecord{m1, m2, m3} {
		if err := repo.UpsertItem(ctx, it); err != nil {
			t.Fatal(err)
		}
		if err := repo.UpsertMediaSourceAndStreams(ctx, it.ID, MediaSourceRecord{
			Name: it.Name, Path: it.Path, Container: "mkv", Size: 1000,
		}); err != nil {
			t.Fatal(err)
		}
	}

	// Cross-library merge MUST fail.
	if _, _, err := repo.MergeItemVersions(ctx, []string{m1.ID, m2.ID}); err == nil || !strings.Contains(err.Error(), "different libraries") {
		t.Fatalf("expected cross-library merge error, got %v", err)
	}
	// Both items remain intact.
	if _, err := repo.GetItemRecordByID(ctx, m2.ID); err != nil {
		t.Fatalf("m2 should remain intact after rejected merge: %v", err)
	}

	// Same-library merge + transactional SplitItemVersions.
	if err := repo.ReplaceItemAncestors(ctx, m1.ID, []AncestorLink{{AncestorID: m1.ID, Depth: 0}, {AncestorID: lib1.ID, Depth: 1}}); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpsertSearchFTS(ctx, m1.ID, "Dune", "Dune", "", "", ""); err != nil {
		t.Fatal(err)
	}
	// Repeat identical UpsertSearchFTS (dirty-check no-op path inside Writer tx) + updated title path.
	if err := repo.UpsertSearchFTS(ctx, m1.ID, "Dune", "Dune", "", "", ""); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpsertSearchFTS(ctx, m1.ID, "Dune Part One", "Dune Part One", "", "", ""); err != nil {
		t.Fatal(err)
	}

	if _, merged, err := repo.MergeItemVersions(ctx, []string{m1.ID, m3.ID}); err != nil || merged != 1 {
		t.Fatalf("same-library MergeItemVersions failed: merged=%d err=%v", merged, err)
	}

	// Concurrent UpsertSearchFTS + SplitItemVersions race check.
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			_ = repo.UpsertSearchFTS(ctx, m2.ID, "Dune", "Dune", "", "", "")
		}(i)
	}
	splitIDs, err := repo.SplitItemVersions(ctx, m1.ID)
	wg.Wait()
	if err != nil || len(splitIDs) != 1 {
		t.Fatalf("SplitItemVersions failed: splitIDs=%v err=%v", splitIDs, err)
	}
	if _, err := repo.SplitItemVersions(ctx, "nonexistent-id"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound for nonexistent SplitItemVersions, got %v", err)
	}
}

func TestEscapeSQLiteURIPathWindowsBackslashes(t *testing.T) {
	got := escapeSQLiteURIPath(`C:\Users\Ambra#1\data%20\db?v=1.db`)
	want := `C:/Users/Ambra%231/data%2520/db%3Fv=1.db`
	if got != want {
		t.Fatalf("escapeSQLiteURIPath = %q, want %q", got, want)
	}
}

func TestCreateCollectionAndPlaylistWriteAncestors(t *testing.T) {
	ctx := context.Background()
	store, err := Open(filepath.Join(t.TempDir(), "ancestors_v0104.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	repo := NewLibraryRepo(store)
	lib, _ := repo.CreateLibrary(ctx, "Movies", "movies", []string{"/m"})
	parentFolder := &ItemRecord{ID: guid.New(), LibraryID: lib.ID, ParentID: lib.ID, Type: "Folder", IsFolder: true, Name: "Saga"}
	if err := repo.UpsertItem(ctx, parentFolder); err != nil {
		t.Fatal(err)
	}
	if err := repo.ReplaceItemAncestors(ctx, parentFolder.ID, []AncestorLink{
		{AncestorID: parentFolder.ID, Depth: 0},
		{AncestorID: lib.ID, Depth: 1},
	}); err != nil {
		t.Fatal(err)
	}

	col, err := repo.CreateCollection(ctx, "Sci-Fi Box", parentFolder.ID, nil)
	if err != nil {
		t.Fatalf("CreateCollection: %v", err)
	}
	pl, err := repo.CreatePlaylist(ctx, "Roadtrip", "Audio", "user123", nil)
	if err != nil {
		t.Fatalf("CreatePlaylist: %v", err)
	}

	checkAncestors := func(descendantID string, mustContain []string) {
		t.Helper()
		rows, err := store.Reader.QueryContext(ctx, `SELECT ancestor_id, depth FROM item_ancestors WHERE descendant_id = ?`, descendantID)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		found := map[string]int{}
		for rows.Next() {
			var aid string
			var d int
			if err := rows.Scan(&aid, &d); err != nil {
				t.Fatal(err)
			}
			found[aid] = d
		}
		for _, want := range mustContain {
			if _, ok := found[guid.Normalize(want)]; !ok {
				t.Fatalf("descendant %s missing ancestor %s; got %+v", descendantID, want, found)
			}
		}
		if found[descendantID] != 0 {
			t.Fatalf("descendant %s must have self ancestor at depth 0, got %+v", descendantID, found)
		}
	}

	checkAncestors(col.ID, []string{col.ID, parentFolder.ID, lib.ID, "collections"})
	checkAncestors(pl.ID, []string{pl.ID, "user123", "playlists"})
}

func TestSeriesSeasonStatsExcludesExtrasAndItemCountsTrailerAndParental(t *testing.T) {
	ctx := context.Background()
	store, err := Open(filepath.Join(t.TempDir(), "stats_counts_v0104.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	userRepo := NewUserRepo(store)
	libRepo := NewLibraryRepo(store)
	user := &UserRecord{ID: guid.New(), Name: "u", Policy: DefaultUserPolicy(false), Config: DefaultUserConfig()}
	_ = userRepo.CreateUser(ctx, user)

	lib, _ := libRepo.CreateLibrary(ctx, "Mixed", "tvshows", []string{"/tv"})
	s1 := 1
	e1 := 1

	series := &ItemRecord{ID: guid.New(), LibraryID: lib.ID, ParentID: lib.ID, Type: "Series", IsFolder: true, Name: "Show", OfficialRating: "TV-PG"}
	season := &ItemRecord{ID: guid.New(), LibraryID: lib.ID, ParentID: series.ID, SeriesID: series.ID, Type: "Season", IsFolder: true, Name: "S1", IndexNumber: &s1}
	extraSeason := &ItemRecord{ID: guid.New(), LibraryID: lib.ID, ParentID: series.ID, SeriesID: series.ID, Type: "Season", IsFolder: true, Name: "Featurette Season", Extra: ItemExtra{ExtraType: "featurette"}}
	ep := &ItemRecord{ID: guid.New(), LibraryID: lib.ID, ParentID: season.ID, SeriesID: series.ID, SeasonID: season.ID, Type: "Episode", Name: "E1", ParentIndexNumber: &s1, IndexNumber: &e1}
	extraEp := &ItemRecord{ID: guid.New(), LibraryID: lib.ID, ParentID: season.ID, SeriesID: series.ID, SeasonID: season.ID, Type: "Episode", Name: "S1 Trailer", Extra: ItemExtra{ExtraType: "trailer"}}

	movieG := &ItemRecord{ID: guid.New(), LibraryID: lib.ID, ParentID: lib.ID, Type: "Movie", Name: "Kids Film", OfficialRating: "G"}
	movieR := &ItemRecord{ID: guid.New(), LibraryID: lib.ID, ParentID: lib.ID, Type: "Movie", Name: "Adult Film", OfficialRating: "R"}
	trailerG := &ItemRecord{ID: guid.New(), LibraryID: lib.ID, ParentID: movieG.ID, Type: "Movie", Name: "Kids Trailer", OfficialRating: "G", Extra: ItemExtra{ExtraType: "trailer"}}
	trailerR := &ItemRecord{ID: guid.New(), LibraryID: lib.ID, ParentID: movieR.ID, Type: "Movie", Name: "Adult Trailer", OfficialRating: "R", Extra: ItemExtra{ExtraType: "trailer"}}

	for _, it := range []*ItemRecord{series, season, extraSeason, ep, extraEp, movieG, movieR, trailerG, trailerR} {
		if err := libRepo.UpsertItem(ctx, it); err != nil {
			t.Fatal(err)
		}
	}

	// 1. loadSeriesSeasonStatsBatch must ignore extraSeason & extraEp.
	stats, err := libRepo.loadSeriesSeasonStatsBatch(ctx, user.ID, []string{series.ID}, []string{season.ID})
	if err != nil {
		t.Fatal(err)
	}
	if st := stats[series.ID]; st == nil || st.childCount != 1 || st.recursiveCount != 1 || st.unplayedCount != 1 {
		t.Fatalf("series stats polluted by extras: %+v", st)
	}
	if st := stats[season.ID]; st == nil || st.childCount != 1 || st.recursiveCount != 1 || st.unplayedCount != 1 {
		t.Fatalf("season stats polluted by extras: %+v", st)
	}

	// 2. GetItemCounts reports real TrailerCount (3 trailers: extraEp, trailerG, trailerR).
	counts, err := libRepo.GetItemCounts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if counts.MovieCount != 2 || counts.SeriesCount != 1 || counts.EpisodeCount != 1 || counts.TrailerCount != 3 {
		t.Fatalf("unexpected unfiltered ItemCounts: %+v", counts)
	}

	// 3. GetItemCountsFiltered respects maxParentalRating for both main items and trailers.
	maxPG := 5
	filtered, err := libRepo.GetItemCountsFiltered(ctx, []string{lib.ID}, &maxPG)
	if err != nil {
		t.Fatal(err)
	}
	if filtered.MovieCount != 1 || filtered.TrailerCount != 2 {
		t.Fatalf("expected 1 PG-or-below movie and 2 trailers, got %+v", filtered)
	}
}

func TestUserSafeDeleteUpdatePolicyAndDisplayPreferencesCleanup(t *testing.T) {
	ctx := context.Background()
	store, err := Open(filepath.Join(t.TempDir(), "user_safe_v0104.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	repo := NewUserRepo(store)

	admin1 := &UserRecord{ID: guid.New(), Name: "admin1", IsAdmin: true, Policy: DefaultUserPolicy(true), Config: DefaultUserConfig()}
	admin2 := &UserRecord{ID: guid.New(), Name: "admin2", IsAdmin: true, Policy: DefaultUserPolicy(true), Config: DefaultUserConfig()}
	member := &UserRecord{ID: guid.New(), Name: "member", IsAdmin: false, Policy: DefaultUserPolicy(false), Config: DefaultUserConfig()}
	for _, u := range []*UserRecord{admin1, admin2, member} {
		if err := repo.CreateUser(ctx, u); err != nil {
			t.Fatal(err)
		}
		if err := repo.UpsertDisplayPreferences(ctx, &DisplayPreferencesRecord{
			ID: "usersettings", UserID: u.ID, Client: "emby", DataJSON: `{"theme":"dark"}`,
		}); err != nil {
			t.Fatal(err)
		}
	}

	// 1. DeleteUser removes non-admin and cleans up display_preferences.
	if err := repo.DeleteUser(ctx, member.ID); err != nil {
		t.Fatalf("DeleteUser member: %v", err)
	}
	if _, err := repo.GetDisplayPreferences(ctx, "usersettings", member.ID, "emby"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected member display_preferences deleted, err=%v", err)
	}

	// 2. With 2 active admins, DeleteUserSafe on admin2 succeeds and cleans up display_preferences.
	if err := repo.DeleteUserSafe(ctx, admin2.ID); err != nil {
		t.Fatalf("DeleteUserSafe admin2: %v", err)
	}
	if _, err := repo.GetDisplayPreferences(ctx, "usersettings", admin2.ID, "emby"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected admin2 display_preferences deleted, err=%v", err)
	}

	// 3. Now admin1 is the last active admin -> DeleteUserSafe must return ErrLastAdmin.
	if err := repo.DeleteUserSafe(ctx, admin1.ID); !errors.Is(err, ErrLastAdmin) {
		t.Fatalf("expected ErrLastAdmin deleting last admin, got %v", err)
	}
	// UpdateUserPolicySafe: demoting last active admin -> ErrLastAdmin.
	demote := admin1.Policy
	demote.IsAdministrator = false
	if err := repo.UpdateUserPolicySafe(ctx, admin1.ID, demote); !errors.Is(err, ErrLastAdmin) {
		t.Fatalf("expected ErrLastAdmin demoting last admin, got %v", err)
	}
	// UpdateUserPolicySafe: disabling last active admin -> ErrLastAdmin.
	disable := admin1.Policy
	disable.IsDisabled = true
	if err := repo.UpdateUserPolicySafe(ctx, admin1.ID, disable); !errors.Is(err, ErrLastAdmin) {
		t.Fatalf("expected ErrLastAdmin disabling last admin, got %v", err)
	}
	// UpdateUserPolicySafe: keeping last admin active (only changing another flag) succeeds.
	benign := admin1.Policy
	benign.EnableRemoteAccess = false
	if err := repo.UpdateUserPolicySafe(ctx, admin1.ID, benign); err != nil {
		t.Fatalf("benign policy update on last admin should succeed, got %v", err)
	}

	// 4. Non-existent user -> ErrNotFound.
	if err := repo.DeleteUserSafe(ctx, guid.New()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound for missing user DeleteUserSafe, got %v", err)
	}
	if err := repo.UpdateUserPolicySafe(ctx, guid.New(), DefaultUserPolicy(false)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound for missing user UpdateUserPolicySafe, got %v", err)
	}
}
