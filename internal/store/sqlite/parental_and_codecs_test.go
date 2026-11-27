package sqlite

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ambra-media/ambra/internal/api/dto"
	"github.com/ambra-media/ambra/internal/pkg/guid"
)

func TestSQLiteFiltersParentalOptionsAndTaxonomies(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "cov_v0100.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	repo := NewLibraryRepo(store)

	lib, err := repo.CreateLibrary(ctx, "Movies", "movies", []string{"/movies"})
	if err != nil {
		t.Fatalf("CreateLibrary: %v", err)
	}
	if !lib.SaveLocalMetadataEnabled() {
		t.Fatalf("expected SaveLocalMetadataEnabled default true")
	}
	if (*LibraryRecord)(nil).SaveLocalMetadataEnabled() != true {
		t.Fatalf("expected nil LibraryRecord SaveLocalMetadataEnabled true")
	}

	updatedLib, err := repo.UpdateLibraryOptions(ctx, lib.ID, map[string]any{"SaveLocalMetadata": false, "CustomKey": "v"})
	if err != nil {
		t.Fatalf("UpdateLibraryOptions: %v", err)
	}
	if updatedLib.SaveLocalMetadataEnabled() {
		t.Fatalf("expected SaveLocalMetadataEnabled false after update")
	}
	if opts := ParseLibraryOptions(updatedLib.OptionsJSON); opts["CustomKey"] != "v" {
		t.Fatalf("expected CustomKey=v, got %+v", opts)
	}

	y2024 := 2024
	rItem := &ItemRecord{
		ID:             "m-r-rated",
		LibraryID:      lib.ID,
		ParentID:       lib.ID,
		Type:           "Movie",
		MediaType:      "Video",
		Name:           "Restricted Movie",
		OfficialRating: "R",
		ProductionYear: &y2024,
	}
	if err := repo.UpsertItem(ctx, rItem); err != nil {
		t.Fatalf("UpsertItem: %v", err)
	}
	if err := repo.ReplaceItemTaxonomies(ctx, rItem.ID, "genre", []string{"Sci-Fi"}); err != nil {
		t.Fatalf("ReplaceItemTaxonomies genre: %v", err)
	}
	if err := repo.ReplaceItemTaxonomies(ctx, rItem.ID, "studio", []string{"A24"}); err != nil {
		t.Fatalf("ReplaceItemTaxonomies studio: %v", err)
	}
	if err := repo.ReplaceItemTaxonomies(ctx, rItem.ID, "tag", []string{"NSFW", "Trending"}); err != nil {
		t.Fatalf("ReplaceItemTaxonomies tag: %v", err)
	}

	genres, studios, tags, err := repo.GetTaxonomiesByItemID(ctx, rItem.ID)
	if err != nil || len(genres) != 1 || len(studios) != 1 || len(tags) != 2 {
		t.Fatalf("GetTaxonomiesByItemID: g=%v s=%v t=%v err=%v", genres, studios, tags, err)
	}

	// Parental helpers
	if len(ExceedingParentalRatings(7)) == 0 {
		t.Fatalf("expected ratings exceeding 7")
	}
	maxPG13 := 7
	blocked, err := repo.IsItemBlockedByParentalPolicy(ctx, rItem, &maxPG13, nil)
	if err != nil || !blocked {
		t.Fatalf("expected R movie blocked by maxRating=7, got blocked=%v err=%v", blocked, err)
	}
	blocked, err = repo.IsItemBlockedByParentalPolicy(ctx, rItem, nil, []string{"nsfw", "NSFW", " "})
	if err != nil || !blocked {
		t.Fatalf("expected movie blocked by NSFW tag, got blocked=%v err=%v", blocked, err)
	}
	blocked, err = repo.IsItemBlockedByParentalPolicy(ctx, nil, &maxPG13, []string{"nsfw"})
	if err != nil || blocked {
		t.Fatalf("expected nil record not blocked, got blocked=%v err=%v", blocked, err)
	}

	// GetQueryFilters (unrestricted + restricted + empty allowed)
	qf, err := repo.GetQueryFilters(ctx, QueryFiltersFilter{
		ParentID:         lib.ID,
		IncludeItemTypes: []string{"Movie"},
	})
	if err != nil || len(qf.Genres) != 1 || len(qf.Studios) != 1 || len(qf.Tags) != 2 || len(qf.Years) != 1 || len(qf.OfficialRatings) != 1 {
		t.Fatalf("GetQueryFilters unexpected: %+v err=%v", qf, err)
	}
	qfEmpty, err := repo.GetQueryFilters(ctx, QueryFiltersFilter{AllowedLibraryIDs: []string{}})
	if err != nil || len(qfEmpty.Genres) != 0 {
		t.Fatalf("GetQueryFilters empty allowed unexpected: %+v err=%v", qfEmpty, err)
	}
	qfBlocked, err := repo.GetQueryFilters(ctx, QueryFiltersFilter{
		AllowedLibraryIDs: []string{lib.ID},
		MaxParentalRating: &maxPG13,
		BlockedTags:       []string{"NSFW"},
	})
	if err != nil || len(qfBlocked.Genres) != 0 {
		t.Fatalf("GetQueryFilters parental blocked unexpected: %+v err=%v", qfBlocked, err)
	}

	// GetResumeItemsWithAllowedLibraries
	items, total, err := repo.GetResumeItemsWithAllowedLibraries(ctx, "srv1", "u1", "", []string{"Movie"}, 0, 10, nil, []string{lib.ID})
	if err != nil || total != 0 || len(items) != 0 {
		t.Fatalf("GetResumeItemsWithAllowedLibraries unexpected: total=%d len=%d err=%v", total, len(items), err)
	}
}

func TestParentalFailClosedUnratedAllowedTagsAndSeriesStats(t *testing.T) {
	ctx := context.Background()
	st, err := Open(filepath.Join(t.TempDir(), "parental_remediation.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer st.Close()

	repo := NewLibraryRepo(st)
	ur := NewUserRepo(st)

	// 1. Expanded ratings + Fail-Closed ParseParentalRatingValue vs backward-compatible ParentalRatingScore
	for _, tc := range []struct {
		label string
		score int
	}{
		{"19", 10},
		{"19+", 10},
		{"R+", 9},
		{"NR", 9},
		{"UNRATED", 9},
		{"NOT-RATED", 9},
		{"NOT RATED", 9},
	} {
		got, ok := ParentalRatingScore(tc.label)
		if !ok || got != tc.score {
			t.Fatalf("ParentalRatingScore(%q) = (%d, %v), want (%d, true)", tc.label, got, ok, tc.score)
		}
		if v := ParseParentalRatingValue(tc.label); v != tc.score {
			t.Fatalf("ParseParentalRatingValue(%q) = %d, want %d", tc.label, v, tc.score)
		}
	}

	if s, ok := ParentalRatingScore(""); ok || s != 0 {
		t.Fatalf("ParentalRatingScore(empty) = (%d, %v), want (0, false)", s, ok)
	}
	if s, ok := ParentalRatingScore("CUSTOM-ADULT-99"); ok || s != 0 {
		t.Fatalf("ParentalRatingScore(unknown) = (%d, %v), want (0, false)", s, ok)
	}
	if v := ParseParentalRatingValue("   "); v != 0 {
		t.Fatalf("ParseParentalRatingValue(blank) = %d, want 0", v)
	}
	if v := ParseParentalRatingValue("CUSTOM-ADULT-99"); v != 100 {
		t.Fatalf("ParseParentalRatingValue(unknown non-empty) = %d, want 100 (fail-closed)", v)
	}

	libKids, _ := repo.CreateLibrary(ctx, "Kids", "tvshows", []string{"/kids"})
	libAdult, _ := repo.CreateLibrary(ctx, "Private", "movies", []string{"/private"})

	maxPG := 5
	kidUser := &UserRecord{
		ID:   guid.New(),
		Name: "kid",
		Policy: dto.UserPolicy{
			EnableAllFolders:  false,
			EnabledFolders:    []string{libKids.ID},
			MaxParentalRating: &maxPG,
			BlockUnratedItems: []string{"Movie"},
			BlockedTags:       []string{"nsfw"},
			AllowedTags:       []string{"SafeForKids"},
		},
		Config: DefaultUserConfig(),
	}
	if err := ur.CreateUser(ctx, kidUser); err != nil {
		t.Fatal(err)
	}

	// Movies: G+SafeForKids (visible), UnknownRating+SafeForKids (fail-closed!), Unrated+SafeForKids (blocked by BlockUnratedItems), G without AllowedTag (blocked by AllowedTags), Private lib G+SafeForKids (blocked by AllowedLibraryIDs)
	mkMov := func(libID, name, rating string, tags []string) *ItemRecord {
		m := &ItemRecord{ID: guid.New(), LibraryID: libID, ParentID: libID, Type: "Movie", MediaType: "Video", Name: name, OfficialRating: rating}
		if err := repo.UpsertItem(ctx, m); err != nil {
			t.Fatal(err)
		}
		_ = repo.UpsertSearchFTS(ctx, m.ID, name, "", "", "", "")
		if len(tags) > 0 {
			_ = repo.ReplaceItemTaxonomies(ctx, m.ID, "tag", tags)
		}
		return m
	}

	mSafe := mkMov(libKids.ID, "Safe Cartoon", "G", []string{"safeforkids"})
	mUnknownRating := mkMov(libKids.ID, "Mystery Foreign Restricted", "KR-RESTRICTED-X", []string{"safeforkids"})
	mUnrated := mkMov(libKids.ID, "Unrated Indie", "", []string{"safeforkids"})
	mNoAllowedTag := mkMov(libKids.ID, "Untagged G Movie", "G", []string{"other"})
	mPrivate := mkMov(libAdult.ID, "Private Movie", "G", []string{"safeforkids"})

	// Verify single-item IsItemBlockedByPolicy
	if blocked, err := repo.IsItemBlockedByPolicy(ctx, mSafe, kidUser.Policy); err != nil || blocked {
		t.Fatalf("expected mSafe allowed, got blocked=%v err=%v", blocked, err)
	}
	for _, blockedItem := range []*ItemRecord{mUnknownRating, mUnrated, mNoAllowedTag, mPrivate} {
		if blocked, err := repo.IsItemBlockedByPolicy(ctx, blockedItem, kidUser.Policy); err != nil || !blocked {
			t.Fatalf("expected %q blocked by policy, got blocked=%v err=%v", blockedItem.Name, blocked, err)
		}
	}

	// Verify QueryItems enforces fail-closed unknown rating, BlockUnratedItems, AllowedTags, AllowedLibraryIDs
	gotMovies, total, err := repo.QueryItems(ctx, ItemFilter{
		UserID:            kidUser.ID,
		AllowedLibraryIDs: kidUser.Policy.EnabledFolders,
		MaxParentalRating: kidUser.Policy.MaxParentalRating,
		BlockUnratedItems: kidUser.Policy.BlockUnratedItems,
		BlockedTags:       kidUser.Policy.BlockedTags,
		AllowedTags:       kidUser.Policy.AllowedTags,
		IncludeItemTypes:  []string{"Movie"},
	})
	if err != nil || total != 1 || len(gotMovies) != 1 || gotMovies[0].Id != mSafe.ID {
		t.Fatalf("QueryItems expected only Safe Cartoon, got total=%d items=%+v err=%v", total, gotMovies, err)
	}

	// 2. BoxSet visibility: Private/Restricted BoxSet hidden; Safe BoxSet visible in QueryItems, SearchHints, ListCollections, GetItemCountsWithPolicy
	colSafe, err := repo.CreateCollection(ctx, "Safe Kids Collection", "", []string{mSafe.ID, mUnknownRating.ID})
	if err != nil {
		t.Fatal(err)
	}
	colPrivate, err := repo.CreateCollection(ctx, "Private Secret Collection", "", []string{mPrivate.ID})
	if err != nil {
		t.Fatal(err)
	}
	colRestrictedOnly, err := repo.CreateCollection(ctx, "Restricted Collection", "", []string{mUnknownRating.ID, mUnrated.ID})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = colPrivate, colRestrictedOnly

	cols, colTotal, err := repo.ListCollections(ctx, ItemFilter{
		UserID:            kidUser.ID,
		AllowedLibraryIDs: kidUser.Policy.EnabledFolders,
		MaxParentalRating: kidUser.Policy.MaxParentalRating,
		BlockUnratedItems: kidUser.Policy.BlockUnratedItems,
		BlockedTags:       kidUser.Policy.BlockedTags,
		AllowedTags:       kidUser.Policy.AllowedTags,
	})
	if err != nil || colTotal != 1 || len(cols) != 1 || cols[0].Id != colSafe.ID {
		t.Fatalf("ListCollections expected only colSafe, got total=%d cols=%+v err=%v", colTotal, cols, err)
	}

	hints, hintTotal, err := repo.SearchHints(ctx, SearchHintFilter{
		UserID:            kidUser.ID,
		AllowedLibraryIDs: kidUser.Policy.EnabledFolders,
		MaxParentalRating: kidUser.Policy.MaxParentalRating,
		BlockUnratedItems: kidUser.Policy.BlockUnratedItems,
		BlockedTags:       kidUser.Policy.BlockedTags,
		AllowedTags:       kidUser.Policy.AllowedTags,
		SearchTerm:        "Collection",
	})
	if err != nil || hintTotal != 1 || hints[0].Id != colSafe.ID {
		t.Fatalf("SearchHints expected only colSafe, got total=%d hints=%+v err=%v", hintTotal, hints, err)
	}

	// Opening colSafe only returns mSafe (hides mUnknownRating)
	colItems, colItemTotal, err := repo.GetContainerItemsWithPolicy(ctx, "srv", kidUser.ID, colSafe.ID, 0, 20, nil, kidUser.Policy.EnabledFolders, kidUser.Policy.MaxParentalRating, kidUser.Policy.BlockedTags)
	if err != nil || colItemTotal != 1 || len(colItems) != 1 || colItems[0].Id != mSafe.ID {
		t.Fatalf("GetContainerItemsWithPolicy expected only mSafe, got total=%d items=%+v err=%v", colItemTotal, colItems, err)
	}

	// 3. Series/Season stats propagate parental policy -> no phantom UnplayedItemCount from restricted episodes!
	series := &ItemRecord{ID: guid.New(), LibraryID: libKids.ID, ParentID: libKids.ID, Type: "Series", IsFolder: true, Name: "Mixed Show", OfficialRating: "TV-PG"}
	season := &ItemRecord{ID: guid.New(), LibraryID: libKids.ID, ParentID: series.ID, SeriesID: series.ID, Type: "Season", IsFolder: true, Name: "Season 1", IndexNumber: intPtr(1)}
	epSafe := &ItemRecord{ID: guid.New(), LibraryID: libKids.ID, ParentID: season.ID, SeriesID: series.ID, SeasonID: season.ID, Type: "Episode", Name: "Ep1 Safe", OfficialRating: "TV-Y7", ParentIndexNumber: intPtr(1), IndexNumber: intPtr(1)}
	epAdult := &ItemRecord{ID: guid.New(), LibraryID: libKids.ID, ParentID: season.ID, SeriesID: series.ID, SeasonID: season.ID, Type: "Episode", Name: "Ep2 TV-MA", OfficialRating: "TV-MA", ParentIndexNumber: intPtr(1), IndexNumber: intPtr(2)}
	for _, r := range []*ItemRecord{series, season, epSafe, epAdult} {
		_ = repo.UpsertItem(ctx, r)
	}
	_ = repo.ReplaceItemTaxonomies(ctx, series.ID, "tag", []string{"SafeForKids"})
	_, _ = repo.MarkPlayed(ctx, kidUser.ID, epSafe.ID)

	seriesList, _, err := repo.QueryItems(ctx, ItemFilter{
		UserID:            kidUser.ID,
		AllowedLibraryIDs: kidUser.Policy.EnabledFolders,
		MaxParentalRating: kidUser.Policy.MaxParentalRating,
		BlockUnratedItems: kidUser.Policy.BlockUnratedItems,
		BlockedTags:       kidUser.Policy.BlockedTags,
		AllowedTags:       kidUser.Policy.AllowedTags,
		IncludeItemTypes:  []string{"Series"},
	})
	if err != nil || len(seriesList) != 1 {
		t.Fatalf("QueryItems Series: %+v err=%v", seriesList, err)
	}
	if seriesList[0].RecursiveItemCount == nil || *seriesList[0].RecursiveItemCount != 1 {
		t.Fatalf("expected RecursiveItemCount=1 for restricted kid, got %v", seriesList[0].RecursiveItemCount)
	}
	if seriesList[0].UserData == nil || seriesList[0].UserData.UnplayedItemCount == nil || *seriesList[0].UserData.UnplayedItemCount != 0 {
		t.Fatalf("expected UnplayedItemCount=0 after watching safe episode (no phantom TV-MA count), got %+v", seriesList[0].UserData)
	}

	// Single-item GetItemByID also filters Series UnplayedItemCount for restricted user!
	seriesDetail, err := repo.GetItemByID(ctx, "srv", kidUser.ID, series.ID)
	if err != nil {
		t.Fatal(err)
	}
	if seriesDetail.UserData == nil || seriesDetail.UserData.UnplayedItemCount == nil || *seriesDetail.UserData.UnplayedItemCount != 0 {
		t.Fatalf("GetItemByID Series expected UnplayedItemCount=0, got %+v", seriesDetail.UserData)
	}

	cnts, err := repo.GetItemCountsWithPolicy(ctx, kidUser.Policy.EnabledFolders, kidUser.Policy)
	if err != nil {
		t.Fatal(err)
	}
	if cnts.MovieCount != 1 || cnts.BoxSetCount != 1 || cnts.EpisodeCount != 1 {
		t.Fatalf("unexpected GetItemCountsWithPolicy: %+v", cnts)
	}
}

func TestQueryItemsLikesHDRVideoCodecsVideoRangesAndTradSimpPinyin(t *testing.T) {
	ctx := context.Background()
	st, err := Open(filepath.Join(t.TempDir(), "filters_pinyin.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	repo := NewLibraryRepo(st)
	ur := NewUserRepo(st)
	u := &UserRecord{ID: guid.New(), Name: "cinephile", Policy: DefaultUserPolicy(true), Config: DefaultUserConfig()}
	_ = ur.CreateUser(ctx, u)
	lib, _ := repo.CreateLibrary(ctx, "Cinema", "movies", []string{"/cinema"})

	mHDR := &ItemRecord{ID: guid.New(), LibraryID: lib.ID, ParentID: lib.ID, Type: "Movie", MediaType: "Video", Name: "Dune HDR"}
	mSDR := &ItemRecord{ID: guid.New(), LibraryID: lib.ID, ParentID: lib.ID, Type: "Movie", MediaType: "Video", Name: "Clerks SDR"}
	_ = repo.UpsertItem(ctx, mHDR)
	_ = repo.UpsertItem(ctx, mSDR)

	_ = repo.ReplaceMediaSourcesAndStreams(ctx, mHDR.ID, []MediaSourceRecord{{
		ID: guid.New(), ItemID: mHDR.ID, Path: "/cinema/dune.mkv", Container: "mkv",
		Streams: []MediaStreamRecord{{StreamIndex: 0, StreamType: "Video", Codec: "hevc", Width: 3840, Height: 2160, VideoRange: "HDR10"}},
	}})
	_ = repo.ReplaceMediaSourcesAndStreams(ctx, mSDR.ID, []MediaSourceRecord{{
		ID: guid.New(), ItemID: mSDR.ID, Path: "/cinema/clerks.mp4", Container: "mp4",
		Streams: []MediaStreamRecord{{StreamIndex: 0, StreamType: "Video", Codec: "h264", Width: 1920, Height: 1080, VideoRange: "SDR"}},
	}})

	tTrue, tFalse := true, false
	if _, err := ur.SetUserItemLikes(ctx, u.ID, mHDR.ID, &tTrue); err != nil {
		t.Fatal(err)
	}
	if _, err := ur.SetUserItemLikes(ctx, u.ID, mSDR.ID, &tFalse); err != nil {
		t.Fatal(err)
	}

	likedItems, likedTotal, err := repo.QueryItems(ctx, ItemFilter{UserID: u.ID, IsLiked: &tTrue})
	if err != nil || likedTotal != 1 || likedItems[0].Id != mHDR.ID || likedItems[0].UserData == nil || likedItems[0].UserData.Likes == nil || !*likedItems[0].UserData.Likes {
		t.Fatalf("IsLiked=true failed: total=%d items=%+v err=%v", likedTotal, likedItems, err)
	}
	dislikedItems, dislikedTotal, err := repo.QueryItems(ctx, ItemFilter{UserID: u.ID, IsLiked: &tFalse})
	if err != nil || dislikedTotal != 1 || dislikedItems[0].Id != mSDR.ID || dislikedItems[0].UserData == nil || dislikedItems[0].UserData.Likes == nil || *dislikedItems[0].UserData.Likes {
		t.Fatalf("IsLiked=false failed: total=%d items=%+v err=%v", dislikedTotal, dislikedItems, err)
	}

	hdrItems, hdrTotal, err := repo.QueryItems(ctx, ItemFilter{IsHDR: &tTrue})
	if err != nil || hdrTotal != 1 || hdrItems[0].Id != mHDR.ID {
		t.Fatalf("IsHDR=true failed: total=%d items=%+v err=%v", hdrTotal, hdrItems, err)
	}
	sdrItems, sdrTotal, err := repo.QueryItems(ctx, ItemFilter{IsHDR: &tFalse})
	if err != nil || sdrTotal != 1 || sdrItems[0].Id != mSDR.ID {
		t.Fatalf("IsHDR=false failed: total=%d items=%+v err=%v", sdrTotal, sdrItems, err)
	}
	codecItems, codecTotal, err := repo.QueryItems(ctx, ItemFilter{VideoCodecs: []string{"HEVC"}})
	if err != nil || codecTotal != 1 || codecItems[0].Id != mHDR.ID {
		t.Fatalf("VideoCodecs=HEVC failed: total=%d err=%v", codecTotal, err)
	}
	rangeItems, rangeTotal, err := repo.QueryItems(ctx, ItemFilter{VideoRanges: []string{"hdr10"}})
	if err != nil || rangeTotal != 1 || rangeItems[0].Id != mHDR.ID {
		t.Fatalf("VideoRanges=hdr10 failed: total=%d err=%v", rangeTotal, err)
	}

	// Verify 2,500+ Traditional-to-Simplified mappings & required characters
	if len(tradToSimpMap) < 2500 {
		t.Fatalf("expected >= 2500 Traditional-to-Simplified mappings, got %d", len(tradToSimpMap))
	}
	tradSample := "諜獵貓蟻鯊喪屍鏈鋸盜謎豬獸聖龍劍俠戰車門風雲電驚魂間職魔夢紅國語雙粵繁簡劇場版終極殺陣異形鐵血戰士"
	wantSimp := "谍猎猫蚁鲨丧尸链锯盗谜猪兽圣龙剑侠战车门风云电惊魂间职魔梦红国语双粤繁简剧场版终极杀阵异形铁血战士"
	if got := ToSimplified(tradSample); got != wantSimp {
		t.Fatalf("ToSimplified mismatch:\n got: %s\nwant: %s", got, wantSimp)
	}
	if skTrad, skSimp := PinyinSortKey("鐵血戰士：終極獵殺"), PinyinSortKey("铁血战士：终极猎杀"); skTrad != skSimp {
		t.Fatalf("PinyinSortKey mismatch: trad=%q simp=%q", skTrad, skSimp)
	}
	fTrad, iTrad := ToPinyin("鏈鋸人 劇場版")
	fSimp, iSimp := ToPinyin("链锯人 剧场版")
	if fTrad != fSimp || iTrad != iSimp {
		t.Fatalf("ToPinyin mismatch: trad=(%q,%q) simp=(%q,%q)", fTrad, iTrad, fSimp, iSimp)
	}
}

func TestPromoteAndSplitUpdateMtimeAndPreventAutoMerge(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	st, err := Open(filepath.Join(tmp, "mv_mtime.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	repo := NewLibraryRepo(st)
	lib, _ := repo.CreateLibrary(ctx, "Movies", "movies", []string{tmp})

	f1 := filepath.Join(tmp, "m_4k.mkv")
	f2 := filepath.Join(tmp, "m_1080p.mkv")
	_ = os.WriteFile(f1, []byte("4k"), 0o644)
	_ = os.WriteFile(f2, []byte("1080p"), 0o644)
	t1 := time.Unix(1700000100, 0)
	t2 := time.Unix(1700000999, 0)
	_ = os.Chtimes(f1, t1, t1)
	_ = os.Chtimes(f2, t2, t2)

	mt1 := t1.Unix()
	mt2 := t2.Unix()
	m1 := &ItemRecord{ID: guid.New(), LibraryID: lib.ID, ParentID: lib.ID, Type: "Movie", MediaType: "Video", Name: "Film", Path: f1, Container: "mkv", FileMtime: &mt1}
	m2 := &ItemRecord{ID: guid.New(), LibraryID: lib.ID, ParentID: lib.ID, Type: "Movie", MediaType: "Video", Name: "Film", Path: f2, Container: "mkv", FileMtime: &mt2}
	for _, m := range []*ItemRecord{m1, m2} {
		_ = repo.UpsertItem(ctx, m)
		_ = repo.UpsertMediaSourceAndStreams(ctx, m.ID, MediaSourceRecord{Name: "src", Path: m.Path, Container: "mkv", Size: 10})
	}

	primaryID, _, err := repo.MergeItemVersions(ctx, []string{m1.ID, m2.ID})
	if err != nil {
		t.Fatal(err)
	}

	// SplitItemVersions writes real os.Stat mtime and sets PreventAutoMerge=true on primary and split items
	splitIDs, err := repo.SplitItemVersions(ctx, primaryID)
	if err != nil || len(splitIDs) != 1 {
		t.Fatalf("SplitItemVersions: %v err=%v", splitIDs, err)
	}
	pRec, _ := repo.GetItemRecordByID(ctx, primaryID)
	sRec, _ := repo.GetItemRecordByID(ctx, splitIDs[0])
	if !pRec.Extra.PreventAutoMerge || !sRec.Extra.PreventAutoMerge {
		t.Fatalf("expected PreventAutoMerge=true on both items, got primary=%v split=%v", pRec.Extra.PreventAutoMerge, sRec.Extra.PreventAutoMerge)
	}
	if sRec.FileMtime == nil || *sRec.FileMtime != mt2 {
		t.Fatalf("expected split item FileMtime=%d, got %v", mt2, sRec.FileMtime)
	}

	// Re-merge manually, remove primary file f1 from disk, call DeleteItemByPath(f1) -> promotes f2 and updates file_mtime to mt2!
	mergedID, _, err := repo.MergeItemVersions(ctx, []string{primaryID, splitIDs[0]})
	if err != nil {
		t.Fatal(err)
	}
	_ = os.Remove(f1)
	if deleted, err := repo.DeleteItemByPath(ctx, lib.ID, f1); err != nil || !deleted {
		t.Fatalf("DeleteItemByPath(f1): deleted=%v err=%v", deleted, err)
	}
	promoted, err := repo.GetItemRecordByID(ctx, mergedID)
	if err != nil {
		t.Fatal(err)
	}
	if promoted.Path != f2 || promoted.FileMtime == nil || *promoted.FileMtime != mt2 {
		t.Fatalf("expected promoted Path=%q FileMtime=%d, got Path=%q FileMtime=%v", f2, mt2, promoted.Path, promoted.FileMtime)
	}
}
