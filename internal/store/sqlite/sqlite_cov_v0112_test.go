package sqlite

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ambra-media/ambra/internal/api/dto"
	"github.com/ambra-media/ambra/internal/pkg/guid"
)

func TestCovV0112_MountPathBranches(t *testing.T) {
	if isWindowsDriveRoot("x") {
		t.Fatal("single char cannot be windows drive root")
	}
	if isWindowsDriveRoot("1:") {
		t.Fatal("digit prefix cannot be windows drive root")
	}
	for _, root := range []string{"C:", `C:\`, "d:/", `Z:\\/`} {
		if !isWindowsDriveRoot(root) {
			t.Fatalf("expected %q to be windows drive root", root)
		}
		if !isFilesystemRoot(root) {
			t.Fatalf("expected %q to be filesystem root", root)
		}
	}
	if isWindowsDriveRoot(`C:\Movies`) {
		t.Fatal("non-root windows path should not be drive root")
	}
	for _, r := range []string{"/", `\`, "."} {
		if !isFilesystemRoot(r) {
			t.Fatalf("expected %q to be filesystem root", r)
		}
	}
	if isFilesystemRoot("/media/movies") {
		t.Fatal("expected /media/movies not to be filesystem root")
	}

	base := t.TempDir()
	dataDir := filepath.Join(base, "data")
	mediaDir := filepath.Join(base, "media")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(mediaDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Deep non-existent nested path under valid ancestor
	deep := filepath.Join(mediaDir, "sub1", "sub2", "sub3")
	if got, err := ValidateLibraryMountPath(deep, dataDir); err != nil || !strings.HasSuffix(got, filepath.Join("sub1", "sub2", "sub3")) {
		t.Fatalf("expected deep missing child accepted, got (%q, %v)", got, err)
	}

	// Empty dataDir is allowed
	if _, err := ValidateLibraryMountPath(mediaDir, "   "); err != nil {
		t.Fatalf("expected empty dataDir allowed, got %v", err)
	}

	// Windows drive root rejected
	if _, err := ValidateLibraryMountPath("C:", dataDir); err == nil {
		t.Fatal("expected C: rejected")
	}

	// Symlink resolving to filesystem root "/"
	symRoot := filepath.Join(base, "sym-root")
	if err := os.Symlink("/", symRoot); err == nil {
		if _, vErr := ValidateLibraryMountPath(symRoot, dataDir); vErr == nil {
			t.Fatal("expected symlink to / rejected")
		}
	}

	// ENOTDIR path component (regular file used as parent directory) triggers non-ErrNotExist error
	regFile := filepath.Join(base, "regular-file")
	if err := os.WriteFile(regFile, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ValidateLibraryMountPath(filepath.Join(regFile, "child"), dataDir); err == nil {
		t.Fatal("expected ENOTDIR library path rejected")
	}
	if _, err := ValidateLibraryMountPath(filepath.Join(regFile, "child", "grandchild"), dataDir); err == nil {
		t.Fatal("expected nested ENOTDIR library path rejected")
	}
	if _, err := ValidateLibraryMountPath(mediaDir, filepath.Join(regFile, "bad-data")); err == nil {
		t.Fatal("expected ENOTDIR dataDir rejected")
	}
}

func TestCovV0112_CodecAliasesVideoRangesAndOrderBy(t *testing.T) {
	if got := expandStreamCodecAliases(nil, "video"); got != nil {
		t.Fatalf("expected nil for empty codecs, got %v", got)
	}
	vCodecs := expandStreamCodecAliases([]string{"", " h265 ", "hevc", "avc", "vp9"}, "video")
	if len(vCodecs) != 7 {
		t.Fatalf("unexpected expanded video codecs: %v", vCodecs)
	}
	aCodecs := expandStreamCodecAliases([]string{"", "eac3", "dd", "opus"}, "audio")
	if len(aCodecs) != 5 {
		t.Fatalf("unexpected expanded audio codecs: %v", aCodecs)
	}
	sCodecs := expandStreamCodecAliases([]string{"", "srt", "pgs", "vtt", "vobsub", "ass"}, "subtitle")
	if len(sCodecs) != 10 {
		t.Fatalf("unexpected expanded subtitle codecs: %v", sCodecs)
	}
	oCodecs := expandStreamCodecAliases([]string{"ttf", "ttf"}, "attachment")
	if len(oCodecs) != 1 || oCodecs[0] != "ttf" {
		t.Fatalf("unexpected expanded attachment codecs: %v", oCodecs)
	}

	if sqlStr, args := appendVideoRangesSQL("i", nil); sqlStr != "" || len(args) != 0 {
		t.Fatalf("expected empty video ranges SQL, got %q", sqlStr)
	}
	if sqlStr, args := appendVideoRangesSQL("i", []string{"", "   "}); sqlStr != "" || len(args) != 0 {
		t.Fatalf("expected whitespace video ranges SQL empty, got %q", sqlStr)
	}
	sqlStr, vrArgs := appendVideoRangesSQL("i", []string{"SDR", "sdr", "HDR", "DOVI", "HDR10+", "HLG_10%"})
	if !strings.Contains(sqlStr, "ESCAPE") || len(vrArgs) != 1 {
		t.Fatalf("unexpected video ranges SQL=%q args=%v", sqlStr, vrArgs)
	}

	// buildOrderBy branches
	if got := buildOrderBy(nil, "ASC", true, false); !strings.Contains(got, "bm25") {
		t.Fatalf("expected bm25 order for ftsActive, got %q", got)
	}
	if got := buildOrderBy([]string{"unknown_field"}, "DESC", false, false); got != "ORDER BY i.sort_key DESC" {
		t.Fatalf("unexpected fallback order: %q", got)
	}
	allSortFields := []string{
		"SortName", "DateCreated", "PremiereDate", "CommunityRating",
		"CriticRating", "OfficialRating", "PlayCount", "Runtime",
		"IndexNumber", "DatePlayed", "Random",
	}
	withUserOrder := buildOrderBy(allSortFields, "Descending", false, true)
	if !strings.Contains(withUserOrder, "u.play_count") || !strings.Contains(withUserOrder, "u.last_played_date") {
		t.Fatalf("expected user-joined order columns, got %q", withUserOrder)
	}
	noUserOrder := buildOrderBy([]string{"PlayCount", "DatePlayed"}, "ASC", false, false)
	if strings.Contains(noUserOrder, "u.play_count") || !strings.Contains(noUserOrder, "i.date_modified") {
		t.Fatalf("expected non-user fallback order columns, got %q", noUserOrder)
	}
}

func TestCovV0112_QueryItemsCollectionsStreamsAndFilters(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	store, err := Open(filepath.Join(tmp, "query_cov_v0112.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	repo := NewLibraryRepo(store)
	libDir := filepath.Join(tmp, "movies")
	_ = os.MkdirAll(libDir, 0o755)
	lib, err := repo.CreateLibrary(ctx, "Movies", "movies", []string{libDir})
	if err != nil {
		t.Fatal(err)
	}

	yr := 2023
	comm := 8.4
	folder := &ItemRecord{
		ID: guid.New(), LibraryID: lib.ID, ParentID: lib.ID,
		Type: "Folder", IsFolder: true, Name: "SciFi Folder", SortName: "scifi folder",
	}
	mov1 := &ItemRecord{
		ID: guid.New(), LibraryID: lib.ID, ParentID: folder.ID,
		Type: "Movie", MediaType: "Video", Container: "mkv",
		Name: "Alpha HDR", SortName: "alpha hdr", OfficialRating: "PG-13",
		ProductionYear: &yr, CommunityRating: &comm, Path: filepath.Join(libDir, "alpha.mkv"),
	}
	mov2 := &ItemRecord{
		ID: guid.New(), LibraryID: lib.ID, ParentID: lib.ID,
		Type: "Movie", MediaType: "Video", Container: "mp4",
		Name: "Beta SDR", SortName: "beta sdr", OfficialRating: "G",
		ProductionYear: &yr, Path: filepath.Join(libDir, "beta.mp4"),
	}
	for _, it := range []*ItemRecord{folder, mov1, mov2} {
		if err := repo.UpsertItem(ctx, it); err != nil {
			t.Fatal(err)
		}
	}
	_ = repo.ReplaceItemAncestors(ctx, folder.ID, []AncestorLink{{AncestorID: lib.ID, Depth: 1}})
	_ = repo.ReplaceItemAncestors(ctx, mov1.ID, []AncestorLink{
		{AncestorID: folder.ID, Depth: 1},
		{AncestorID: lib.ID, Depth: 2},
	})
	_ = repo.ReplaceItemAncestors(ctx, mov2.ID, []AncestorLink{{AncestorID: lib.ID, Depth: 1}})

	if err := repo.ReplaceMediaSourcesAndStreams(ctx, mov1.ID, []MediaSourceRecord{{
		ID: mov1.ID, ItemID: mov1.ID, Path: mov1.Path, Container: "mkv",
		Streams: []MediaStreamRecord{
			{ItemID: mov1.ID, StreamIndex: 0, StreamType: "Video", Codec: "hevc", Width: 3840, Height: 2160, VideoRange: "HDR10"},
			{ItemID: mov1.ID, StreamIndex: 1, StreamType: "Audio", Codec: "eac3"},
			{ItemID: mov1.ID, StreamIndex: 2, StreamType: "Subtitle", Codec: "subrip"},
		},
	}}); err != nil {
		t.Fatal(err)
	}
	if err := repo.ReplaceMediaSourcesAndStreams(ctx, mov2.ID, []MediaSourceRecord{{
		ID: mov2.ID, ItemID: mov2.ID, Path: mov2.Path, Container: "mp4",
		Streams: []MediaStreamRecord{
			{ItemID: mov2.ID, StreamIndex: 0, StreamType: "Video", Codec: "h264", Width: 640, Height: 480, VideoRange: "SDR"},
			{ItemID: mov2.ID, StreamIndex: 1, StreamType: "Audio", Codec: "ac3"},
		},
	}}); err != nil {
		t.Fatal(err)
	}

	_ = repo.ReplaceItemTaxonomies(ctx, mov1.ID, "genre", []string{"Sci-Fi"})
	_ = repo.ReplaceItemTaxonomies(ctx, mov1.ID, "studio", []string{"Warner"})
	_ = repo.ReplaceItemTaxonomies(ctx, mov1.ID, "tag", []string{"imax"})
	_ = repo.ReplaceItemTaxonomies(ctx, mov2.ID, "tag", []string{"spoiler"})

	col, err := repo.CreateCollection(ctx, "Alpha Collection", "", []string{mov1.ID})
	if err != nil {
		t.Fatal(err)
	}
	// Rate the BoxSet so BlockUnratedItems=["BoxSet"] passes
	col.OfficialRating = "PG-13"
	_ = repo.UpsertItem(ctx, col)

	maxR := 10
	// GroupItemsIntoCollections with LibraryID, ParentID (recursive & non-recursive), parental policy
	grouped, _, err := repo.QueryItems(ctx, ItemFilter{
		ServerID:                  "srv",
		LibraryID:                 lib.ID,
		ParentID:                  lib.ID,
		Recursive:                 true,
		GroupItemsIntoCollections: true,
		IncludeItemTypes:          []string{"Movie"},
		AllowedLibraryIDs:         []string{lib.ID},
		MaxParentalRating:         &maxR,
		BlockUnratedItems:         []string{"BoxSet"},
		BlockedTags:               []string{"forbidden"},
	})
	if err != nil || len(grouped) != 2 {
		t.Fatalf("expected BoxSet + mov2 in recursive grouped query, got len=%d err=%v", len(grouped), err)
	}

	groupedDirect, _, err := repo.QueryItems(ctx, ItemFilter{
		ServerID:                  "srv",
		ParentID:                  folder.ID,
		Recursive:                 false,
		GroupItemsIntoCollections: true,
		IncludeItemTypes:          []string{"Movie", "BoxSet"},
	})
	if err != nil || len(groupedDirect) != 1 {
		t.Fatalf("expected 1 BoxSet in non-recursive folder grouped query, got len=%d err=%v", len(groupedDirect), err)
	}

	// ListCollections with non-collections LibraryID normalizes LibraryID
	cols, _, err := repo.ListCollections(ctx, ItemFilter{ServerID: "srv", LibraryID: lib.ID})
	if err != nil || len(cols) != 1 {
		t.Fatalf("expected 1 collection from ListCollections, got len=%d err=%v", len(cols), err)
	}

	// Stream, codec, container, folder, name range, and boolean filters
	tVal, fVal := true, false
	filtered, total, err := repo.QueryItems(ctx, ItemFilter{
		ServerID:                "srv",
		IsFolder:                &fVal,
		MediaTypes:              []string{"Video"},
		Containers:              []string{".mkv", "mkv", " "},
		VideoCodecs:             []string{"h265"},
		AudioCodecs:             []string{"ddp"},
		SubtitleCodecs:          []string{"srt"},
		VideoRanges:             []string{"HDR"},
		IsHD:                    &tVal,
		Is4K:                    &tVal,
		IsHDR:                   &tVal,
		HasSubtitles:            &tVal,
		Tags:                    []string{"imax"},
		ExcludeTags:             []string{"spoiler"},
		NameStartsWith:          "A",
		NameStartsWithOrGreater: "A",
		NameLessThan:            "Z",
		MinCommunityRating:      &comm,
		OfficialRatings:         []string{"PG-13"},
		Years:                   []int{2023},
	})
	if err != nil || total != 1 || len(filtered) != 1 || guid.Normalize(filtered[0].Id) != guid.Normalize(mov1.ID) {
		t.Fatalf("expected mov1 from rich stream filter, got total=%d items=%+v err=%v", total, filtered, err)
	}

	// Negative boolean branches (IsFolder=true, IsHD=false, Is4K=false, IsHDR=false, HasSubtitles=false)
	foldersOnly, _, err := repo.QueryItems(ctx, ItemFilter{ServerID: "srv", LibraryID: lib.ID, IsFolder: &tVal})
	if err != nil || len(foldersOnly) != 1 {
		t.Fatalf("expected 1 folder, got len=%d err=%v", len(foldersOnly), err)
	}
	sdOnly, _, err := repo.QueryItems(ctx, ItemFilter{
		ServerID:     "srv",
		LibraryID:    lib.ID,
		IsHD:         &fVal,
		Is4K:         &fVal,
		IsHDR:        &fVal,
		HasSubtitles: &fVal,
	})
	if err != nil || len(sdOnly) != 1 || guid.Normalize(sdOnly[0].Id) != guid.Normalize(mov2.ID) {
		t.Fatalf("expected mov2 for SD/SDR/no-subs filter, got %+v err=%v", sdOnly, err)
	}
}

func TestCovV0112_PolicyPlaylistsCascadeUsersAndMigrations(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	store, err := Open(filepath.Join(tmp, "policy_cov_v0112.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	// Exercise idempotent legacy migration paths V3, V4, V5, V7
	if err := store.applyMigrationV3(ctx); err != nil {
		t.Fatalf("applyMigrationV3 failed: %v", err)
	}
	if err := store.applyMigrationV4(ctx); err != nil {
		t.Fatalf("applyMigrationV4 failed: %v", err)
	}
	if err := store.applyMigrationV5(ctx); err != nil {
		t.Fatalf("applyMigrationV5 failed: %v", err)
	}
	if err := store.applyMigrationV7(ctx); err != nil {
		t.Fatalf("applyMigrationV7 failed: %v", err)
	}

	repo := NewLibraryRepo(store)
	userRepo := NewUserRepo(store)

	movDir := filepath.Join(tmp, "mov")
	otherDir := filepath.Join(tmp, "other")
	_ = os.MkdirAll(movDir, 0o755)
	_ = os.MkdirAll(otherDir, 0o755)
	movLib, _ := repo.CreateLibrary(ctx, "Movies", "movies", []string{movDir})
	otherLib, _ := repo.CreateLibrary(ctx, "Other", "tvshows", []string{otherDir})

	// Before any livetv library exists, EnableAllFolders=true with EnableLiveTvAccess=false returns nil
	openPol := dto.UserPolicy{EnableAllFolders: true, EnableLiveTvAccess: false}
	if got := repo.AllowedLibraryIDsForPolicy(ctx, openPol); got != nil {
		t.Fatalf("expected nil AllowedLibraryIDsForPolicy when no livetv library exists, got %v", got)
	}

	liveDir := filepath.Join(tmp, "live")
	_ = os.MkdirAll(liveDir, 0o755)
	liveLib, _ := repo.CreateLibrary(ctx, "Live", "livetv", []string{liveDir})

	if repo.CanUserAccessLibrary(ctx, openPol, "") {
		t.Fatal("empty libraryID should return false")
	}
	scopedPol := dto.UserPolicy{
		EnableAllFolders:   false,
		EnableLiveTvAccess: false,
		EnabledFolders:     []string{"", movLib.ID, movLib.ID, liveLib.ID},
	}
	if !repo.CanUserAccessLibrary(ctx, scopedPol, movLib.ID) {
		t.Fatal("expected access to movLib")
	}
	if repo.CanUserAccessLibrary(ctx, scopedPol, otherLib.ID) {
		t.Fatal("expected no access to otherLib")
	}
	if repo.CanUserAccessLibrary(ctx, scopedPol, liveLib.ID) {
		t.Fatal("expected no access to liveLib when EnableLiveTvAccess=false")
	}
	allowedScoped := repo.AllowedLibraryIDsForPolicy(ctx, scopedPol)
	if len(allowedScoped) != 1 || guid.Normalize(allowedScoped[0]) != guid.Normalize(movLib.ID) {
		t.Fatalf("expected [movLib.ID], got %v", allowedScoped)
	}

	// Users: DeleteUserSafe & UpdateUserPolicySafe edge cases
	admin1 := &UserRecord{Name: "admin1", IsAdmin: true, Policy: DefaultUserPolicy(true)}
	admin2 := &UserRecord{ID: guid.New(), Name: "admin2", IsAdmin: true, Policy: DefaultUserPolicy(true), Config: DefaultUserConfig()}
	_ = userRepo.CreateUser(ctx, admin1)
	if !errors.Is(userRepo.CreateUser(ctx, &UserRecord{Name: "ADMIN1"}), ErrUserExists) {
		t.Fatal("expected ErrUserExists on case-insensitive duplicate username")
	}
	if !errors.Is(userRepo.DeleteUserSafe(ctx, guid.New()), ErrNotFound) {
		t.Fatal("expected ErrNotFound deleting unknown user")
	}
	if !errors.Is(userRepo.DeleteUserSafe(ctx, admin1.ID), ErrLastAdmin) {
		t.Fatal("expected ErrLastAdmin deleting sole admin")
	}
	if !errors.Is(userRepo.UpdateUserPolicySafe(ctx, guid.New(), DefaultUserPolicy(false)), ErrNotFound) {
		t.Fatal("expected ErrNotFound updating unknown user policy")
	}
	if !errors.Is(userRepo.UpdateUserPolicySafe(ctx, admin1.ID, DefaultUserPolicy(false)), ErrLastAdmin) {
		t.Fatal("expected ErrLastAdmin demoting sole admin")
	}
	_ = userRepo.CreateUser(ctx, admin2)
	demotedPol := dto.UserPolicy{IsAdministrator: false}
	if err := userRepo.UpdateUserPolicySafe(ctx, admin2.ID, demotedPol); err != nil {
		t.Fatalf("expected demoting second admin to succeed, got %v", err)
	}
	if err := userRepo.DeleteUserSafe(ctx, admin2.ID); err != nil {
		t.Fatalf("expected deleting demoted user to succeed, got %v", err)
	}

	// Playlists & CountPlaylistsByOwner
	movA := &ItemRecord{
		ID: guid.New(), LibraryID: movLib.ID, ParentID: movLib.ID,
		Type: "Movie", MediaType: "Video", Name: "Seed Movie", SortName: "seed movie",
		OfficialRating: "G", Path: filepath.Join(movDir, "seed.mkv"),
	}
	movB := &ItemRecord{
		ID: guid.New(), LibraryID: movLib.ID, ParentID: movLib.ID,
		Type: "Movie", MediaType: "Video", Name: "Other Movie", SortName: "other movie",
		OfficialRating: "G", Path: filepath.Join(movDir, "other.mkv"),
	}
	_ = repo.UpsertItem(ctx, movA)
	_ = repo.UpsertItem(ctx, movB)

	if n, err := repo.CountPlaylistsByOwner(ctx, ""); err != nil || n != 0 {
		t.Fatalf("expected (0, nil) for empty owner, got (%d, %v)", n, err)
	}
	if _, err := repo.CreatePlaylist(ctx, "   ", "", admin1.ID, nil); err == nil {
		t.Fatal("expected empty playlist name error")
	}
	pl1, err := repo.CreatePlaylist(ctx, "Auto Video PL", "", admin1.ID, []string{movA.ID})
	if err != nil || pl1.MediaType != "Video" {
		t.Fatalf("expected inferred Video playlist, got %+v err=%v", pl1, err)
	}
	pl2, err := repo.CreatePlaylist(ctx, "Default Audio PL", "", admin1.ID, nil)
	if err != nil || pl2.MediaType != "Audio" {
		t.Fatalf("expected default Audio playlist, got %+v err=%v", pl2, err)
	}
	if cnt, err := repo.CountPlaylistsByOwner(ctx, admin1.ID); err != nil || cnt != 2 {
		t.Fatalf("expected 2 playlists for admin1, got (%d, %v)", cnt, err)
	}

	// Seed accessibility helper branches
	if ok, err := repo.isSeedAccessibleUnderUserPolicy(ctx, nil, nil, scopedPol); err != nil || ok {
		t.Fatalf("expected nil seed inaccessible, got (%v, %v)", ok, err)
	}
	if ok, err := repo.isSeedAccessibleUnderUserPolicy(ctx, movA, []string{otherLib.ID}, scopedPol); err != nil || ok {
		t.Fatalf("expected seed in disallowed library inaccessible, got (%v, %v)", ok, err)
	}

	// Recommendations fallback ("Recently Added" when no genres exist, then favorited seed + top genres)
	if emptyRecs, err := repo.GetMovieRecommendationsWithUserPolicy(ctx, "srv", admin1.ID, 0, 0, nil, []string{}, openPol); err != nil || len(emptyRecs) != 0 {
		t.Fatalf("expected empty recommendations for empty allowed slice, got %+v err=%v", emptyRecs, err)
	}
	noGenreRecs, err := repo.GetMovieRecommendationsWithUserPolicy(ctx, "srv", admin1.ID, 0, 0, nil, []string{movLib.ID}, openPol)
	if err != nil || len(noGenreRecs) != 1 || noGenreRecs[0].CategoryId != "recent-movies" {
		t.Fatalf("expected recent-movies fallback category, got %+v err=%v", noGenreRecs, err)
	}
	_ = repo.ReplaceItemTaxonomies(ctx, movA.ID, "genre", []string{"Adventure"})
	_ = repo.ReplaceItemTaxonomies(ctx, movB.ID, "genre", []string{"Adventure", "Comedy"})
	_, _ = repo.MarkFavorite(ctx, admin1.ID, movA.ID)
	favRecs, err := repo.GetMovieRecommendationsWithUserPolicy(ctx, "srv", admin1.ID, 4, 5, nil, []string{movLib.ID}, openPol)
	if err != nil || len(favRecs) < 2 || favRecs[0].RecommendationType != "SimilarToLikedItem" {
		t.Fatalf("expected SimilarToLikedItem + top genre categories, got %+v err=%v", favRecs, err)
	}

	// GetQueryFilters with empty allowed libraries and scoped ParentID/IncludeItemTypes
	if qfEmpty, err := repo.GetQueryFilters(ctx, QueryFiltersFilter{AllowedLibraryIDs: []string{}}); err != nil || len(qfEmpty.Genres) != 0 {
		t.Fatalf("expected empty filters, got %+v err=%v", qfEmpty, err)
	}
	if qfScoped, err := repo.GetQueryFilters(ctx, QueryFiltersFilter{
		ParentID:         movLib.ID,
		IncludeItemTypes: []string{"Movie"},
	}); err != nil || len(qfScoped.Genres) != 2 {
		t.Fatalf("expected 2 scoped genres, got %+v err=%v", qfScoped, err)
	}

	// MarkPlayedAtWithPolicy (zero time), MarkUnplayedWithPolicy (non-admin cascade), ErrNotFound cases
	missingID := guid.New()
	if _, err := repo.MarkPlayedAtWithPolicy(ctx, admin1.ID, missingID, time.Time{}, openPol); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound from MarkPlayedAtWithPolicy, got %v", err)
	}
	if _, err := repo.MarkUnplayedWithPolicy(ctx, admin1.ID, missingID, openPol); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound from MarkUnplayedWithPolicy, got %v", err)
	}
	if _, err := repo.MarkFavorite(ctx, admin1.ID, missingID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound from MarkFavorite, got %v", err)
	}
	if _, err := repo.UnmarkFavorite(ctx, admin1.ID, missingID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound from UnmarkFavorite, got %v", err)
	}

	series := &ItemRecord{ID: guid.New(), LibraryID: movLib.ID, ParentID: movLib.ID, Type: "Series", IsFolder: true, Name: "S", OfficialRating: "G"}
	epG := &ItemRecord{ID: guid.New(), LibraryID: movLib.ID, ParentID: series.ID, SeriesID: series.ID, Type: "Episode", Name: "E1", OfficialRating: "G"}
	epR := &ItemRecord{ID: guid.New(), LibraryID: movLib.ID, ParentID: series.ID, SeriesID: series.ID, Type: "Episode", Name: "E2", OfficialRating: "R"}
	for _, it := range []*ItemRecord{series, epG, epR} {
		_ = repo.UpsertItem(ctx, it)
	}
	_, _ = repo.MarkPlayedAtWithPolicy(ctx, admin1.ID, series.ID, time.Time{}, DefaultUserPolicy(true))
	maxG := 1
	kidPol := dto.UserPolicy{EnableAllFolders: true, MaxParentalRating: &maxG}
	if _, err := repo.MarkUnplayedWithPolicy(ctx, admin1.ID, series.ID, kidPol); err != nil {
		t.Fatalf("MarkUnplayedWithPolicy failed: %v", err)
	}
	udG, _ := repo.GetUserItemData(ctx, admin1.ID, epG.ID)
	udR, _ := repo.GetUserItemData(ctx, admin1.ID, epR.ID)
	if udG.Played || !udR.Played {
		t.Fatalf("expected epG unplayed and blocked epR still played, got G=%v R=%v", udG.Played, udR.Played)
	}

	// UpdateLibraryOptions, ListLibraryItemPaths, DeleteItemsByPathPrefix, DeleteLibrary
	if _, err := repo.UpdateLibraryOptions(ctx, missingID, map[string]any{"X": 1}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound on UpdateLibraryOptions, got %v", err)
	}
	if updatedLib, err := repo.UpdateLibraryOptions(ctx, otherLib.ID, map[string]any{"CustomFlag": true}); err != nil || !strings.Contains(updatedLib.OptionsJSON, "CustomFlag") {
		t.Fatalf("UpdateLibraryOptions failed: %+v err=%v", updatedLib, err)
	}
	if paths, err := repo.ListLibraryItemPaths(ctx, movLib.ID); err != nil || len(paths) < 2 {
		t.Fatalf("ListLibraryItemPaths failed: %v err=%v", paths, err)
	}
	if n, err := repo.DeleteItemsByPathPrefix(ctx, "", ""); err != nil || n != 0 {
		t.Fatalf("expected (0, nil) for empty prefix delete, got (%d, %v)", n, err)
	}
	if n, err := repo.DeleteItemsByPathPrefix(ctx, movLib.ID, movDir); err != nil || n < 2 {
		t.Fatalf("expected DeleteItemsByPathPrefix to remove movDir items, got (%d, %v)", n, err)
	}
	if !errors.Is(repo.DeleteItem(ctx, missingID), ErrNotFound) {
		t.Fatal("expected ErrNotFound deleting missing item")
	}
	if err := repo.DeleteLibrary(ctx, otherLib.ID); err != nil {
		t.Fatalf("DeleteLibrary failed: %v", err)
	}
}
