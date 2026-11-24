package sqlite

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/ambra-media/ambra/internal/api/dto"
	"github.com/ambra-media/ambra/internal/pkg/guid"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func intPtr(v int) *int       { return &v }
func int64Ptr(v int64) *int64 { return &v }
func f64Ptr(v float64) *float64 { return &v }
func boolPtr(v bool) *bool    { return &v }

func TestOpenInvalidPath(t *testing.T) {
	if _, err := Open("/nonexistent-dir-ambra-test/sub/test.db"); err == nil {
		t.Fatal("expected error opening invalid path")
	}
}

func TestUserRepoFullCRUD(t *testing.T) {
	store := openTestStore(t)
	repo := NewUserRepo(store)
	ctx := context.Background()

	cnt, err := repo.CountUsers(ctx)
	if err != nil || cnt != 0 {
		t.Fatalf("CountUsers: cnt=%d err=%v", cnt, err)
	}

	// CreateUser with empty ID and zero CreatedAt and nil slices
	now := time.Now().UTC()
	u1 := &UserRecord{
		Name:         "Alice",
		PasswordHash: "hash1",
		IsAdmin:      true,
		LastLoginAt:  &now,
		LastActiveAt: &now,
	}
	if err := repo.CreateUser(ctx, u1); err != nil {
		t.Fatalf("CreateUser u1: %v", err)
	}
	u2 := &UserRecord{
		ID:         guid.New(),
		Name:       "HiddenBob",
		IsDisabled: false,
		Policy:     dto.UserPolicy{IsHidden: true},
	}
	if err := repo.CreateUser(ctx, u2); err != nil {
		t.Fatalf("CreateUser u2: %v", err)
	}

	// GetUserByID, GetUserByName, NotFound
	got, err := repo.GetUserByID(ctx, u1.ID)
	if err != nil || got.Name != "Alice" || !got.IsAdmin || got.LastLoginAt == nil {
		t.Fatalf("GetUserByID: %+v err=%v", got, err)
	}
	if _, err := repo.GetUserByID(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
	gotByName, err := repo.GetUserByName(ctx, "alice")
	if err != nil || gotByName.ID != u1.ID {
		t.Fatalf("GetUserByName: %+v err=%v", gotByName, err)
	}

	// ListUsers & ListPublicUsers (excludes hidden)
	all, err := repo.ListUsers(ctx)
	if err != nil || len(all) != 2 {
		t.Fatalf("ListUsers: len=%d err=%v", len(all), err)
	}
	pub, err := repo.ListPublicUsers(ctx)
	if err != nil || len(pub) != 1 || pub[0].Name != "Alice" {
		t.Fatalf("ListPublicUsers: %+v err=%v", pub, err)
	}

	// UpdateUserPassword, UpdateUserPolicy, UpdateUserConfig + ErrNotFound cases
	if err := repo.UpdateUserPassword(ctx, u1.ID, "hash2"); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpdateUserPassword(ctx, "missing", "x"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
	if err := repo.UpdateUserPolicy(ctx, u1.ID, dto.UserPolicy{IsAdministrator: true}); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpdateUserPolicy(ctx, "missing", dto.UserPolicy{}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
	if err := repo.UpdateUserConfig(ctx, u1.ID, dto.UserConfiguration{SubtitleMode: "Always"}); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpdateUserConfig(ctx, "missing", dto.UserConfiguration{}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
	if err := repo.UpdateLastLogin(ctx, u1.ID, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpdateLastActive(ctx, u1.ID, time.Now()); err != nil {
		t.Fatal(err)
	}

	// AccessTokens & Devices
	tok := &AccessTokenRecord{
		Token: "tok-1", UserID: u1.ID, DeviceID: "dev-1", DeviceName: "Mac", AppName: "Web", AppVersion: "1.0",
	}
	if err := repo.CreateAccessToken(ctx, tok); err != nil {
		t.Fatal(err)
	}
	gotTok, err := repo.GetAccessToken(ctx, "tok-1")
	if err != nil || gotTok.DeviceID != "dev-1" {
		t.Fatalf("GetAccessToken: %+v err=%v", gotTok, err)
	}
	if _, err := repo.GetAccessToken(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
	if err := repo.TouchAccessToken(ctx, "tok-1", time.Now()); err != nil {
		t.Fatal(err)
	}
	devs, err := repo.ListDevices(ctx)
	if err != nil || len(devs) != 1 || devs[0].LastUserName != "Alice" {
		t.Fatalf("ListDevices: %+v err=%v", devs, err)
	}
	if err := repo.DeleteAccessToken(ctx, "tok-1"); err != nil {
		t.Fatal(err)
	}
	_ = repo.CreateAccessToken(ctx, &AccessTokenRecord{Token: "tok-2", UserID: u1.ID, DeviceID: "dev-2"})
	if err := repo.DeleteTokensByDevice(ctx, "dev-2"); err != nil {
		t.Fatal(err)
	}
	_ = repo.CreateAccessToken(ctx, &AccessTokenRecord{Token: "tok-3", UserID: u1.ID, DeviceID: "dev-3"})
	if err := repo.DeleteTokensByUser(ctx, u1.ID); err != nil {
		t.Fatal(err)
	}

	// API Keys
	if err := repo.CreateAPIKey(ctx, &APIKeyRecord{Token: "ak-1", AppName: "Sonarr"}); err != nil {
		t.Fatal(err)
	}
	if ak, err := repo.GetAPIKey(ctx, "ak-1"); err != nil || ak.AppName != "Sonarr" {
		t.Fatalf("GetAPIKey: %+v err=%v", ak, err)
	}
	if _, err := repo.GetAPIKey(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
	if aks, err := repo.ListAPIKeys(ctx); err != nil || len(aks) != 1 {
		t.Fatalf("ListAPIKeys: %+v err=%v", aks, err)
	}
	if err := repo.DeleteAPIKey(ctx, "ak-1"); err != nil {
		t.Fatal(err)
	}

	// DisplayPreferences
	if _, err := repo.GetDisplayPreferences(ctx, "dp1", u1.ID, "web"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
	if err := repo.UpsertDisplayPreferences(ctx, &DisplayPreferencesRecord{ID: "dp1", UserID: u1.ID, Client: "web"}); err != nil {
		t.Fatal(err)
	}
	if dp, err := repo.GetDisplayPreferences(ctx, "dp1", u1.ID, "web"); err != nil || dp.DataJSON != "{}" {
		t.Fatalf("GetDisplayPreferences: %+v err=%v", dp, err)
	}

	// Activity Logs
	if err := repo.CreateActivityLog(ctx, &ActivityLogRecord{Name: "Ev1", Type: "T1", Overview: "Ov", ShortOverview: "SO", ItemID: "i1", UserID: u1.ID}); err != nil {
		t.Fatal(err)
	}
	logs, total, err := repo.ListActivityLogs(ctx, -1, 0, nil)
	if err != nil || total != 1 || len(logs) != 1 {
		t.Fatalf("ListActivityLogs: len=%d total=%d err=%v", len(logs), total, err)
	}
	minD := time.Now().UTC().Add(-time.Hour)
	logs2, total2, err := repo.ListActivityLogs(ctx, 0, 10, &minD)
	if err != nil || total2 != 1 || len(logs2) != 1 {
		t.Fatalf("ListActivityLogs minDate: len=%d total=%d err=%v", len(logs2), total2, err)
	}

	// ServerConfig
	if _, err := repo.GetServerConfig(ctx, "k1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
	if err := repo.SetServerConfig(ctx, "k1", `{"a":1}`); err != nil {
		t.Fatal(err)
	}
	if v, err := repo.GetServerConfig(ctx, "k1"); err != nil || v != `{"a":1}` {
		t.Fatalf("GetServerConfig: %q err=%v", v, err)
	}

	// GetItemRuntimeTicks & UpsertPlaybackProgress
	if _, err := repo.GetItemRuntimeTicks(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
	if _, err := repo.GetUserItemData(ctx, u1.ID, "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
	libRepo := NewLibraryRepo(store)
	m := &ItemRecord{LibraryID: "lib1", Type: "Movie", Name: "M1"}
	_ = libRepo.UpsertItem(ctx, m)
	if rt, err := repo.GetItemRuntimeTicks(ctx, m.ID); err != nil || rt != 0 {
		t.Fatalf("GetItemRuntimeTicks nil: rt=%d err=%v", rt, err)
	}
	m.RuntimeTicks = int64Ptr(9999)
	_ = libRepo.UpsertItem(ctx, m)
	if rt, err := repo.GetItemRuntimeTicks(ctx, m.ID); err != nil || rt != 9999 {
		t.Fatalf("GetItemRuntimeTicks 9999: rt=%d err=%v", rt, err)
	}
	ud, err := repo.UpsertPlaybackProgress(ctx, u1.ID, m.ID, 500, false, intPtr(1), intPtr(2))
	if err != nil || ud.PlaybackPositionTicks != 500 || *ud.AudioStreamIndex != 1 || *ud.SubtitleStreamIndex != 2 {
		t.Fatalf("UpsertPlaybackProgress partial: %+v err=%v", ud, err)
	}
	ud2, err := repo.UpsertPlaybackProgress(ctx, u1.ID, m.ID, 9000, true, nil, nil)
	if err != nil || !ud2.Played || ud2.PlayCount != 1 || ud2.PlaybackPositionTicks != 0 {
		t.Fatalf("UpsertPlaybackProgress played: %+v err=%v", ud2, err)
	}

	// DeleteUser
	if err := repo.DeleteUser(ctx, u2.ID); err != nil {
		t.Fatal(err)
	}
	if err := repo.DeleteUser(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound on DeleteUser, got %v", err)
	}
}

func TestLibraryRepoQueriesSearchAndUserData(t *testing.T) {
	store := openTestStore(t)
	libRepo := NewLibraryRepo(store)
	userRepo := NewUserRepo(store)
	ctx := context.Background()

	// CreateUser for user-scoped queries
	user := &UserRecord{Name: "viewer"}
	_ = userRepo.CreateUser(ctx, user)

	// Library CRUD
	lib, err := libRepo.CreateLibrary(ctx, "Movies", "", nil)
	if err != nil || lib.CollectionType != "movies" {
		t.Fatalf("CreateLibrary: %+v err=%v", lib, err)
	}
	lib, err = libRepo.AddLibraryPath(ctx, lib.ID, "/media/movies")
	if err != nil || len(lib.Paths) != 1 {
		t.Fatalf("AddLibraryPath: %+v err=%v", lib, err)
	}
	// Duplicate path is idempotent
	lib, err = libRepo.AddLibraryPath(ctx, lib.ID, "/media/movies")
	if err != nil || len(lib.Paths) != 1 {
		t.Fatalf("AddLibraryPath dup: %+v err=%v", lib, err)
	}
	if _, err := libRepo.AddLibraryPath(ctx, "missing", "/x"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
	libs, err := libRepo.ListLibraries(ctx)
	if err != nil || len(libs) != 1 {
		t.Fatalf("ListLibraries: %+v err=%v", libs, err)
	}

	// Build Movie + Series + Season + Episode + Audio + Book + MusicArtist + MusicAlbum + MusicVideo
	now := time.Now().Unix()
	movie := &ItemRecord{
		LibraryID:       lib.ID,
		ParentID:        lib.ID,
		Type:            "Movie",
		MediaType:       "Video",
		Name:            "流浪地球",
		OriginalTitle:   "The Wandering Earth",
		ProductionYear:  intPtr(2019),
		PremiereDate:    int64Ptr(now - 100),
		CommunityRating: f64Ptr(8.5),
		OfficialRating:  "PG-13",
		RuntimeTicks:    int64Ptr(7200_0000000),
		Container:       "mkv",
		Path:            "/media/movies/earth.mkv",
		Extra: ItemExtra{
			Overview:    "Sci-fi epic",
			Taglines:    []string{"Save Earth"},
			ProviderIDs: map[string]string{"Tmdb": "535167"},
		},
	}
	if err := libRepo.UpsertItem(ctx, movie); err != nil {
		t.Fatal(err)
	}
	if found, err := libRepo.FindItemByPath(ctx, lib.ID, "/media/movies/earth.mkv"); err != nil || found.ID != movie.ID {
		t.Fatalf("FindItemByPath: %+v err=%v", found, err)
	}
	if _, err := libRepo.FindItemByPath(ctx, lib.ID, "/nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
	if rec, err := libRepo.GetItemRecordByID(ctx, movie.ID); err != nil || rec.Name != "流浪地球" {
		t.Fatalf("GetItemRecordByID: %+v err=%v", rec, err)
	}
	if _, err := libRepo.GetItemRecordByID(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}

	// Relations: Ancestors, MediaSources+Streams, Images, Taxonomies, People, FTS
	if err := libRepo.ReplaceItemAncestors(ctx, movie.ID, []AncestorLink{{AncestorID: ""}, {AncestorID: lib.ID, Depth: 1}}); err != nil {
		t.Fatal(err)
	}
	if err := libRepo.ReplaceMediaSourcesAndStreams(ctx, movie.ID, []MediaSourceRecord{{
		Name: "earth.mkv", Path: "/media/movies/earth.mkv", Container: "mkv", Size: 1024, Bitrate: 5000, RuntimeTicks: 7200_0000000,
		SupportsDirectPlay: true, SupportsDirectStream: true, SupportsTranscoding: true,
		Streams: []MediaStreamRecord{
			{StreamIndex: 0, StreamType: "Video", Codec: "hevc", Width: 1920, Height: 1080, VideoRange: "HDR10", IsDefault: true},
			{StreamIndex: 1, StreamType: "Audio", Codec: "aac", Language: "chi", AudioLayout: "5.1", Channels: 6, SampleRate: 48000, IsDefault: true},
			{StreamIndex: 2, StreamType: "Subtitle", Codec: "srt", Language: "chi", SubVariant: "CHS", IsExternal: true, Path: "/media/movies/earth.chs.srt"},
		},
	}}); err != nil {
		t.Fatal(err)
	}
	if err := libRepo.ReplaceItemImages(ctx, movie.ID, []ImageRecord{
		{ImageType: "Primary", ImageIndex: 0, Path: "/img/p.jpg", Width: 300, Height: 450},
		{ImageType: "Backdrop", ImageIndex: 0, Path: "/img/b.jpg", Tag: "btag", Width: 1920, Height: 1080},
	}); err != nil {
		t.Fatal(err)
	}
	if img, err := libRepo.GetImageByItemAndType(ctx, movie.ID, "primary", 0); err != nil || img.Path != "/img/p.jpg" {
		t.Fatalf("GetImageByItemAndType: %+v err=%v", img, err)
	}
	if _, err := libRepo.GetImageByItemAndType(ctx, movie.ID, "Logo", 0); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
	if err := libRepo.ReplaceItemTaxonomies(ctx, movie.ID, "genre", []string{"Sci-Fi", "Sci-Fi", "Action", " "}); err != nil {
		t.Fatal(err)
	}
	if err := libRepo.ReplaceItemTaxonomies(ctx, movie.ID, "studio", []string{"China Film"}); err != nil {
		t.Fatal(err)
	}
	if err := libRepo.ReplaceItemTaxonomies(ctx, movie.ID, "tag", []string{"Space"}); err != nil {
		t.Fatal(err)
	}
	if err := libRepo.ReplaceItemPeople(ctx, movie.ID, []PersonInput{
		{Name: "Wu Jing", Role: "Liu Peiqiang", Type: "Actor"},
		{Name: "Guo Fan", Type: ""},
		{Name: "  "},
	}); err != nil {
		t.Fatal(err)
	}
	if err := libRepo.UpsertSearchFTS(ctx, movie.ID, "流浪地球 The Wandering Earth", "流浪 浪地 地球 the wandering earth", "liulangdiqiu", "lldq", "Wu Jing"); err != nil {
		t.Fatal(err)
	}

	// Series + Episode + extra types for ItemCounts
	series := &ItemRecord{LibraryID: lib.ID, ParentID: lib.ID, Type: "Series", IsFolder: true, Name: "Three Body"}
	_ = libRepo.UpsertItem(ctx, series)
	ep1 := &ItemRecord{LibraryID: lib.ID, ParentID: series.ID, SeriesID: series.ID, Type: "Episode", MediaType: "Video", Name: "Ep1", ParentIndexNumber: intPtr(1), IndexNumber: intPtr(1)}
	ep2 := &ItemRecord{LibraryID: lib.ID, ParentID: series.ID, SeriesID: series.ID, Type: "Episode", MediaType: "Video", Name: "Ep2", ParentIndexNumber: intPtr(1), IndexNumber: intPtr(2)}
	_ = libRepo.UpsertItem(ctx, ep1)
	_ = libRepo.UpsertItem(ctx, ep2)
	for _, tp := range []string{"MusicArtist", "Audio", "MusicAlbum", "MusicVideo", "Book"} {
		_ = libRepo.UpsertItem(ctx, &ItemRecord{LibraryID: lib.ID, ParentID: lib.ID, Type: tp, Name: "Item-" + tp})
	}

	// GetItemByID with and without userID + NotFound
	dtoFull, err := libRepo.GetItemByID(ctx, "srv1", user.ID, movie.ID)
	if err != nil || dtoFull.Name != "流浪地球" || len(dtoFull.Genres) != 2 || len(dtoFull.Studios) != 1 || len(dtoFull.People) != 2 || len(dtoFull.MediaSources) != 1 {
		t.Fatalf("GetItemByID full: %+v err=%v", dtoFull, err)
	}
	if dtoNoUser, err := libRepo.GetItemByID(ctx, "srv1", "", movie.ID); err != nil || dtoNoUser.UserData != nil {
		t.Fatalf("GetItemByID no user: %+v err=%v", dtoNoUser, err)
	}
	if _, err := libRepo.GetItemByID(ctx, "srv1", user.ID, "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}

	// UserItemData helpers: default, MarkFavorite, UnmarkFavorite, MarkPlayed, MarkUnplayed
	ud, err := libRepo.GetUserItemData(ctx, user.ID, movie.ID)
	if err != nil || ud.Played || ud.IsFavorite {
		t.Fatalf("GetUserItemData default: %+v err=%v", ud, err)
	}
	if ud, err = libRepo.MarkFavorite(ctx, user.ID, movie.ID); err != nil || !ud.IsFavorite {
		t.Fatalf("MarkFavorite: %+v err=%v", ud, err)
	}
	if ud, err = libRepo.UnmarkFavorite(ctx, user.ID, movie.ID); err != nil || ud.IsFavorite {
		t.Fatalf("UnmarkFavorite: %+v err=%v", ud, err)
	}
	if ud, err = libRepo.MarkPlayed(ctx, user.ID, movie.ID); err != nil || !ud.Played || ud.PlayCount != 1 {
		t.Fatalf("MarkPlayed: %+v err=%v", ud, err)
	}
	if ud, err = libRepo.MarkUnplayed(ctx, user.ID, movie.ID); err != nil || ud.Played {
		t.Fatalf("MarkUnplayed: %+v err=%v", ud, err)
	}
	// Set resumable progress
	_, _ = userRepo.UpsertPlaybackProgress(ctx, user.ID, movie.ID, 12345, false, nil, nil)

	// BuildFTSQuery edge cases
	if got := BuildFTSQuery("   "); got != "" {
		t.Fatalf("expected empty FTS query, got %q", got)
	}
	if got := BuildFTSQuery("!!!"); got != "" {
		t.Fatalf("expected empty FTS query for punct only, got %q", got)
	}
	if got := BuildFTSQuery("流浪地球 2019 Earth"); got == "" {
		t.Fatal("expected non-empty FTS query")
	}

	// QueryItems with all filters & sort orders (note: test ParentID+Recursive separately from SearchTerm+LibraryID)
	personID := dtoFull.People[0].Id
	items, total, err := libRepo.QueryItems(ctx, ItemFilter{
		ServerID:         "srv1",
		UserID:           user.ID,
		LibraryID:        lib.ID,
		IncludeItemTypes: []string{"Movie"},
		ExcludeItemTypes: []string{"Book"},
		Genres:           []string{"Sci-Fi"},
		Studios:          []string{"China Film"},
		Tags:             []string{"Space"},
		PersonIDs:        []string{personID},
		Years:            []int{2019},
		IsUnplayed:       boolPtr(true),
		IsResumable:      boolPtr(true),
		SearchTerm:       "流浪地球",
		SortBy:           []string{"SortName", "DateCreated", "PremiereDate", "CommunityRating", "Runtime", "IndexNumber", "DatePlayed", "Random"},
		SortOrder:        "Descending",
		StartIndex:       0,
		Limit:            10,
		Fields:           []string{"Overview", "Path", "ProviderIds", "Genres", "Studios", "People", "MediaSources"},
	})
	if err != nil || total != 1 || len(items) != 1 {
		t.Fatalf("QueryItems full filter: len=%d total=%d err=%v", len(items), total, err)
	}
	// Recursive parent query
	recItems, recTotal, err := libRepo.QueryItems(ctx, ItemFilter{
		ServerID:  "srv1",
		UserID:    user.ID,
		ParentID:  lib.ID,
		Recursive: true,
	})
	if err != nil || recTotal != 1 || len(recItems) != 1 {
		t.Fatalf("QueryItems recursive parent: len=%d total=%d err=%v", len(recItems), recTotal, err)
	}

	// Additional QueryItems branches: non-recursive parent, IsPlayed, IsFavorite, StartIndex without Limit, unknown SortBy, no user DatePlayed, FTS default sort
	_, _, _ = libRepo.QueryItems(ctx, ItemFilter{
		ServerID: "srv1", UserID: user.ID, ParentID: lib.ID, Recursive: false,
		IsPlayed: boolPtr(true), IsFavorite: boolPtr(true), StartIndex: 1, SortBy: []string{"UnknownField"},
	})
	_, _, _ = libRepo.QueryItems(ctx, ItemFilter{
		ServerID: "srv1", SeriesID: series.ID, SeasonID: "none", SortBy: []string{"DatePlayed"},
	})
	_, _, _ = libRepo.QueryItems(ctx, ItemFilter{
		ServerID: "srv1", SearchTerm: "流浪",
	})

	// GetLatestItems, GetResumeItems, GetNextUpEpisodes
	latest, err := libRepo.GetLatestItems(ctx, "srv1", user.ID, lib.ID, nil, 0, nil)
	if err != nil || len(latest) == 0 {
		t.Fatalf("GetLatestItems: len=%d err=%v", len(latest), err)
	}
	resumable, rTotal, err := libRepo.GetResumeItems(ctx, "srv1", user.ID, lib.ID, nil, 0, 0, nil)
	if err != nil || rTotal != 1 || len(resumable) != 1 {
		t.Fatalf("GetResumeItems: len=%d total=%d err=%v", len(resumable), rTotal, err)
	}
	nextUp, nTotal, err := libRepo.GetNextUpEpisodes(ctx, "srv1", user.ID, series.ID, 0, nil)
	if err != nil || nTotal != 1 || len(nextUp) != 1 || nextUp[0].Id != ep1.ID {
		t.Fatalf("GetNextUpEpisodes: len=%d total=%d err=%v", len(nextUp), nTotal, err)
	}

	// SearchHints (without ParentID for result verification, and with ParentID to exercise parent join)
	if hEmpty, c, err := libRepo.SearchHints(ctx, SearchHintFilter{SearchTerm: ""}); err != nil || c != 0 || len(hEmpty) != 0 {
		t.Fatalf("SearchHints empty: %v %d %v", hEmpty, c, err)
	}
	hints, hTotal, err := libRepo.SearchHints(ctx, SearchHintFilter{
		SearchTerm: "流浪地球", IncludeItemTypes: []string{"Movie"}, Limit: 0,
	})
	if err != nil || hTotal != 1 || len(hints) != 1 || hints[0].PrimaryImageTag == "" || hints[0].BackdropImageTag != "btag" {
		t.Fatalf("SearchHints: %+v total=%d err=%v", hints, hTotal, err)
	}
	_, _, _ = libRepo.SearchHints(ctx, SearchHintFilter{
		SearchTerm: "流浪地球", ParentID: lib.ID, Limit: 5,
	})

	// ListTaxonomies: years, persons, genres, studios (with and without parentID)
	for _, kind := range []string{"years", "persons", "genres", "studios"} {
		res, tot, err := libRepo.ListTaxonomies(ctx, "srv1", kind, lib.ID, 0, 0)
		if err != nil || tot == 0 || len(res) == 0 {
			t.Errorf("ListTaxonomies(%s): len=%d tot=%d err=%v", kind, len(res), tot, err)
		}
	}
	// paginateSlice edge cases
	if s := paginateSlice([]int{1, 2, 3}, -1, 2); len(s) != 2 {
		t.Fatalf("paginateSlice(-1,2) = %v", s)
	}
	if s := paginateSlice([]int{1, 2, 3}, 10, 2); len(s) != 0 {
		t.Fatalf("paginateSlice(10,2) = %v", s)
	}

	// GetItemCounts
	counts, err := libRepo.GetItemCounts(ctx)
	if err != nil || counts.MovieCount != 1 || counts.SeriesCount != 1 || counts.EpisodeCount != 2 || counts.BookCount != 1 {
		t.Fatalf("GetItemCounts: %+v err=%v", counts, err)
	}

	// DeleteLibraryByName: not found, then valid
	if err := libRepo.DeleteLibraryByName(ctx, "NoSuchLib"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
	if err := libRepo.DeleteLibraryByName(ctx, "Movies"); err != nil {
		t.Fatalf("DeleteLibraryByName: %v", err)
	}
}

func TestCollectionsPlaylistsSimilarMixRecommendationsAndFilters(t *testing.T) {
	store := openTestStore(t)
	libRepo := NewLibraryRepo(store)
	userRepo := NewUserRepo(store)
	ctx := context.Background()

	user := &UserRecord{Name: "u1"}
	_ = userRepo.CreateUser(ctx, user)
	lib, _ := libRepo.CreateLibrary(ctx, "Main", "movies", []string{"/m"})

	m1 := &ItemRecord{LibraryID: lib.ID, ParentID: lib.ID, Type: "Movie", MediaType: "Video", Name: "Movie 1", OfficialRating: "PG", Container: "mkv"}
	m2 := &ItemRecord{LibraryID: lib.ID, ParentID: lib.ID, Type: "Movie", MediaType: "Video", Name: "Movie 2", OfficialRating: "R", Container: "mp4"}
	m3 := &ItemRecord{LibraryID: lib.ID, ParentID: lib.ID, Type: "Movie", MediaType: "Video", Name: "Movie 3"}
	a1 := &ItemRecord{LibraryID: lib.ID, ParentID: lib.ID, Type: "Audio", MediaType: "Audio", Name: "Song 1"}
	for _, it := range []*ItemRecord{m1, m2, m3, a1} {
		_ = libRepo.UpsertItem(ctx, it)
		_ = libRepo.ReplaceItemAncestors(ctx, it.ID, []AncestorLink{{AncestorID: lib.ID, Depth: 1}})
	}
	_ = libRepo.ReplaceItemTaxonomies(ctx, m1.ID, "genre", []string{"Drama"})
	_ = libRepo.ReplaceItemTaxonomies(ctx, m2.ID, "genre", []string{"Drama"})
	_ = libRepo.ReplaceItemTaxonomies(ctx, m1.ID, "tag", []string{"Classic"})
	_ = libRepo.ReplaceItemTaxonomies(ctx, a1.ID, "genre", []string{"Rock"})
	_ = libRepo.ReplaceMediaSourcesAndStreams(ctx, m1.ID, []MediaSourceRecord{{
		Name: "m1.mkv", Path: "/m/m1.mkv",
		Streams: []MediaStreamRecord{
			{StreamIndex: 0, StreamType: "Video", Codec: "h264", VideoRange: "SDR"},
			{StreamIndex: 1, StreamType: "Audio", Codec: "ac3", AudioLayout: "5.1", Language: "eng"},
			{StreamIndex: 2, StreamType: "Subtitle", Codec: "ass", Language: "chi"},
		},
	}})

	// CreateCollection validation & success
	if _, err := libRepo.CreateCollection(ctx, "  ", "", nil); err == nil {
		t.Fatal("expected error for empty collection name")
	}
	col, err := libRepo.CreateCollection(ctx, "Sci-Fi BoxSet", "", []string{m1.ID, m2.ID, m1.ID, "nonexistent"})
	if err != nil {
		t.Fatalf("CreateCollection: %v", err)
	}
	// Deduplicate skips existing
	added, err := libRepo.AddItemsToContainer(ctx, col.ID, []string{m1.ID, m3.ID, col.ID, ""}, true)
	if err != nil || added != 1 {
		t.Fatalf("AddItemsToContainer dedup: added=%d err=%v", added, err)
	}
	if _, err := libRepo.AddItemsToContainer(ctx, "missing", []string{m1.ID}, true); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}

	// CreatePlaylist with inferred mediaType & default mediaType
	if _, err := libRepo.CreatePlaylist(ctx, "", "", user.ID, nil); err == nil {
		t.Fatal("expected error for empty playlist name")
	}
	pl, err := libRepo.CreatePlaylist(ctx, "My Video Playlist", "", user.ID, []string{m1.ID, m2.ID, m3.ID})
	if err != nil || pl.MediaType != "Video" {
		t.Fatalf("CreatePlaylist video: %+v err=%v", pl, err)
	}
	plAudio, err := libRepo.CreatePlaylist(ctx, "Empty Audio Playlist", "", user.ID, nil)
	if err != nil || plAudio.MediaType != "Audio" {
		t.Fatalf("CreatePlaylist default audio: %+v err=%v", plAudio, err)
	}

	// GetContainerItems with userID, without userID, with limit/startIndex, and NotFound
	itemsWithUser, total, err := libRepo.GetContainerItems(ctx, "srv1", user.ID, pl.ID, 0, 2, nil)
	if err != nil || total != 3 || len(itemsWithUser) != 2 {
		t.Fatalf("GetContainerItems with user: len=%d total=%d err=%v", len(itemsWithUser), total, err)
	}
	itemsNoUser, total2, err := libRepo.GetContainerItems(ctx, "srv1", "", pl.ID, 1, 0, nil)
	if err != nil || total2 != 3 || len(itemsNoUser) != 2 {
		t.Fatalf("GetContainerItems no user: len=%d total=%d err=%v", len(itemsNoUser), total2, err)
	}
	if _, _, err := libRepo.GetContainerItems(ctx, "srv1", "", "missing", 0, 10, nil); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}

	// MoveContainerItem: by entryID, by itemID, clamp negative/overflow, same index, empty container, unknown item
	if err := libRepo.MoveContainerItem(ctx, pl.ID, itemsWithUser[0].PlaylistItemId, 10); err != nil {
		t.Fatalf("MoveContainerItem by entryID: %v", err)
	}
	if err := libRepo.MoveContainerItem(ctx, pl.ID, m3.ID, -5); err != nil {
		t.Fatalf("MoveContainerItem by itemID clamp<0: %v", err)
	}
	if err := libRepo.MoveContainerItem(ctx, pl.ID, m3.ID, 0); err != nil {
		t.Fatalf("MoveContainerItem same index: %v", err)
	}
	if err := libRepo.MoveContainerItem(ctx, pl.ID, "unknown-item", 0); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound for unknown item, got %v", err)
	}
	if err := libRepo.MoveContainerItem(ctx, plAudio.ID, m1.ID, 0); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound for empty container, got %v", err)
	}

	// RemoveItemsFromContainer: empty slice, NotFound, valid removal
	if n, err := libRepo.RemoveItemsFromContainer(ctx, pl.ID, nil); err != nil || n != 0 {
		t.Fatalf("RemoveItemsFromContainer empty: n=%d err=%v", n, err)
	}
	if _, err := libRepo.RemoveItemsFromContainer(ctx, "missing", []string{m1.ID}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
	if n, err := libRepo.RemoveItemsFromContainer(ctx, pl.ID, []string{"", m2.ID}); err != nil || n != 1 {
		t.Fatalf("RemoveItemsFromContainer m2: n=%d err=%v", n, err)
	}

	// GetSimilarItems: nonexistent, and valid (hits both genre match and fallback)
	if sim, cnt, err := libRepo.GetSimilarItems(ctx, "srv1", user.ID, "missing", 0, nil); err != nil || cnt != 0 || len(sim) != 0 {
		t.Fatalf("GetSimilarItems missing: %v %d %v", sim, cnt, err)
	}
	if sim, cnt, err := libRepo.GetSimilarItems(ctx, "srv1", user.ID, m1.ID, 5, nil); err != nil || cnt < 2 || len(sim) < 2 {
		t.Fatalf("GetSimilarItems m1: len=%d cnt=%d err=%v", len(sim), cnt, err)
	}

	// GetInstantMix: nonexistent, movie, audio, playlist
	if mix, cnt, err := libRepo.GetInstantMix(ctx, "srv1", user.ID, "missing", 0, nil); err != nil || cnt != 0 || len(mix) != 0 {
		t.Fatalf("GetInstantMix missing: %v %d %v", mix, cnt, err)
	}
	if mix, cnt, err := libRepo.GetInstantMix(ctx, "srv1", user.ID, m1.ID, 2, nil); err != nil || cnt == 0 || len(mix) == 0 {
		t.Fatalf("GetInstantMix movie: len=%d cnt=%d err=%v", len(mix), cnt, err)
	}
	if mix, cnt, err := libRepo.GetInstantMix(ctx, "srv1", user.ID, a1.ID, 2, nil); err != nil || cnt == 0 || len(mix) == 0 {
		t.Fatalf("GetInstantMix audio: len=%d cnt=%d err=%v", len(mix), cnt, err)
	}

	// GetMovieRecommendations: with genres, and fallback when no movie genres exist
	recs, err := libRepo.GetMovieRecommendations(ctx, "srv1", user.ID, 0, 0, nil)
	if err != nil || len(recs) == 0 {
		t.Fatalf("GetMovieRecommendations with genres: %+v err=%v", recs, err)
	}
	// Clear movie genres to test "Recently Added" fallback
	_ = libRepo.ReplaceItemTaxonomies(ctx, m1.ID, "genre", nil)
	_ = libRepo.ReplaceItemTaxonomies(ctx, m2.ID, "genre", nil)
	recsFallback, err := libRepo.GetMovieRecommendations(ctx, "srv1", user.ID, 3, 5, nil)
	if err != nil || len(recsFallback) != 1 || recsFallback[0].CategoryId != "recent-movies" {
		t.Fatalf("GetMovieRecommendations fallback: %+v err=%v", recsFallback, err)
	}

	// ListFilterValues across all dimensions
	dims := []string{
		"MusicGenre", "GameGenre", "Tag", "OfficialRating", "ItemType", "Container",
		"AudioCodec", "AudioLayout", "VideoCodec", "SubtitleCodec", "StreamLanguage", "ExtendedVideoType", "UnknownDim",
	}
	for _, dim := range dims {
		if _, _, err := libRepo.ListFilterValues(ctx, "srv1", dim, lib.ID, nil, 0, 0); err != nil {
			t.Errorf("ListFilterValues(%s) with parent: %v", dim, err)
		}
		if _, _, err := libRepo.ListFilterValues(ctx, "srv1", dim, "", []string{"Movie"}, 0, 10); err != nil {
			t.Errorf("ListFilterValues(%s) with types: %v", dim, err)
		}
	}
	// Tag without scope
	if tags, tot, err := libRepo.ListFilterValues(ctx, "srv1", "Tag", "", nil, 0, 10); err != nil || tot != 1 || len(tags) != 1 {
		t.Fatalf("ListFilterValues(Tag no scope): len=%d tot=%d err=%v", len(tags), tot, err)
	}

	// DeleteItem: valid & NotFound
	if err := libRepo.DeleteItem(ctx, pl.ID); err != nil {
		t.Fatalf("DeleteItem: %v", err)
	}
	if err := libRepo.DeleteItem(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound on DeleteItem, got %v", err)
	}
}

func TestClosedStoreErrorPaths(t *testing.T) {
	store := openTestStore(t)
	userRepo := NewUserRepo(store)
	libRepo := NewLibraryRepo(store)
	ctx := context.Background()

	// Create one library & item before closing Reader/Writer selectively
	lib, err := libRepo.CreateLibrary(ctx, "L1", "movies", []string{"/a"})
	if err != nil {
		t.Fatal(err)
	}
	item := &ItemRecord{LibraryID: lib.ID, Type: "Movie", Name: "M1"}
	if err := libRepo.UpsertItem(ctx, item); err != nil {
		t.Fatal(err)
	}

	// Close Writer only first: Reader queries succeed while Writer fails
	_ = store.Writer.Close()
	if _, err := libRepo.CreateLibrary(ctx, "L2", "movies", nil); err == nil {
		t.Error("expected CreateLibrary error")
	}
	if err := libRepo.DeleteLibrary(ctx, lib.ID); err == nil {
		t.Error("expected DeleteLibrary error")
	}
	if _, err := libRepo.AddLibraryPath(ctx, lib.ID, "/b"); err == nil {
		t.Error("expected AddLibraryPath error")
	}
	if err := libRepo.ReplaceItemAncestors(ctx, item.ID, nil); err == nil {
		t.Error("expected ReplaceItemAncestors error")
	}
	if err := libRepo.ReplaceMediaSourcesAndStreams(ctx, item.ID, nil); err == nil {
		t.Error("expected ReplaceMediaSourcesAndStreams error")
	}
	if err := libRepo.ReplaceItemImages(ctx, item.ID, nil); err == nil {
		t.Error("expected ReplaceItemImages error")
	}
	if err := libRepo.ReplaceItemTaxonomies(ctx, item.ID, "genre", []string{"A"}); err == nil {
		t.Error("expected ReplaceItemTaxonomies error")
	}
	if err := libRepo.ReplaceItemPeople(ctx, item.ID, []PersonInput{{Name: "P"}}); err == nil {
		t.Error("expected ReplaceItemPeople error")
	}
	if err := libRepo.UpsertSearchFTS(ctx, item.ID, "a", "a", "", "", ""); err == nil {
		t.Error("expected UpsertSearchFTS error")
	}
	if _, err := libRepo.CreateCollection(ctx, "C1", "", nil); err == nil {
		t.Error("expected CreateCollection error")
	}
	if _, err := libRepo.CreatePlaylist(ctx, "P1", "Video", "u1", nil); err == nil {
		t.Error("expected CreatePlaylist error")
	}
	if err := libRepo.DeleteItem(ctx, item.ID); err == nil {
		t.Error("expected DeleteItem error")
	}
	if _, err := libRepo.AddItemsToContainer(ctx, item.ID, []string{"x"}, true); err == nil {
		t.Error("expected AddItemsToContainer error")
	}
	if _, err := libRepo.RemoveItemsFromContainer(ctx, item.ID, []string{"x"}); err == nil {
		t.Error("expected RemoveItemsFromContainer error")
	}
	if err := libRepo.MoveContainerItem(ctx, item.ID, "x", 0); err == nil {
		t.Error("expected MoveContainerItem error")
	}
	if _, err := libRepo.MarkPlayed(ctx, "u1", item.ID); err == nil {
		t.Error("expected MarkPlayed error")
	}
	if _, err := libRepo.MarkUnplayed(ctx, "u1", item.ID); err == nil {
		t.Error("expected MarkUnplayed error")
	}
	if _, err := libRepo.MarkFavorite(ctx, "u1", item.ID); err == nil {
		t.Error("expected MarkFavorite error")
	}
	if _, err := libRepo.UnmarkFavorite(ctx, "u1", item.ID); err == nil {
		t.Error("expected UnmarkFavorite error")
	}
	if err := userRepo.UpdateUserPassword(ctx, "u1", "h"); err == nil {
		t.Error("expected UpdateUserPassword error")
	}
	if err := userRepo.UpdateUserPolicy(ctx, "u1", dto.UserPolicy{}); err == nil {
		t.Error("expected UpdateUserPolicy error")
	}
	if err := userRepo.UpdateUserConfig(ctx, "u1", dto.UserConfiguration{}); err == nil {
		t.Error("expected UpdateUserConfig error")
	}
	if err := userRepo.DeleteUser(ctx, "u1"); err == nil {
		t.Error("expected DeleteUser error")
	}
	if err := userRepo.CreateActivityLog(ctx, &ActivityLogRecord{Name: "x"}); err == nil {
		t.Error("expected CreateActivityLog error")
	}
	if _, err := userRepo.UpsertPlaybackProgress(ctx, "u1", item.ID, 10, false, nil, nil); err == nil {
		t.Error("expected UpsertPlaybackProgress false error")
	}
	if _, err := userRepo.UpsertPlaybackProgress(ctx, "u1", item.ID, 10, true, nil, nil); err == nil {
		t.Error("expected UpsertPlaybackProgress true error")
	}

	// Now close Reader as well to hit Reader error paths
	_ = store.Reader.Close()
	if _, err := libRepo.ListLibraries(ctx); err == nil {
		t.Error("expected ListLibraries error")
	}
	if _, err := libRepo.GetLibrary(ctx, lib.ID); err == nil {
		t.Error("expected GetLibrary error")
	}
	if err := libRepo.DeleteLibraryByName(ctx, "L1"); err == nil {
		t.Error("expected DeleteLibraryByName error")
	}
	if _, _, err := libRepo.QueryItems(ctx, ItemFilter{}); err == nil {
		t.Error("expected QueryItems error")
	}
	if _, err := libRepo.GetItemByID(ctx, "s", "u", item.ID); err == nil {
		t.Error("expected GetItemByID error")
	}
	if _, _, err := libRepo.GetNextUpEpisodes(ctx, "s", "u", "", 10, nil); err == nil {
		t.Error("expected GetNextUpEpisodes error")
	}
	if _, _, err := libRepo.SearchHints(ctx, SearchHintFilter{SearchTerm: "test"}); err == nil {
		t.Error("expected SearchHints error")
	}
	for _, kind := range []string{"years", "persons", "genres"} {
		if _, _, err := libRepo.ListTaxonomies(ctx, "s", kind, "", 0, 10); err == nil {
			t.Errorf("expected ListTaxonomies(%s) error", kind)
		}
	}
	if _, err := libRepo.GetItemCounts(ctx); err == nil {
		t.Error("expected GetItemCounts error")
	}
	if _, err := libRepo.GetUserItemData(ctx, "u", item.ID); err == nil {
		t.Error("expected GetUserItemData error")
	}
	if _, err := libRepo.GetMediaSourcesByItemID(ctx, item.ID); err == nil {
		t.Error("expected GetMediaSourcesByItemID error")
	}
	if _, err := libRepo.GetImageByItemAndType(ctx, item.ID, "Primary", 0); err == nil {
		t.Error("expected GetImageByItemAndType error")
	}
	if _, _, err := libRepo.GetContainerItems(ctx, "s", "u", item.ID, 0, 10, nil); err == nil {
		t.Error("expected GetContainerItems error")
	}
	if _, _, err := libRepo.GetSimilarItems(ctx, "s", "u", item.ID, 5, nil); err == nil {
		t.Error("expected GetSimilarItems error")
	}
	if _, _, err := libRepo.GetInstantMix(ctx, "s", "u", item.ID, 5, nil); err == nil {
		t.Error("expected GetInstantMix error")
	}
	if _, err := libRepo.GetMovieRecommendations(ctx, "s", "u", 5, 5, nil); err == nil {
		t.Error("expected GetMovieRecommendations error")
	}
	if _, _, err := libRepo.ListFilterValues(ctx, "s", "Tag", "", nil, 0, 10); err == nil {
		t.Error("expected ListFilterValues error")
	}
	if _, err := userRepo.ListUsers(ctx); err == nil {
		t.Error("expected ListUsers error")
	}
	if _, err := userRepo.ListPublicUsers(ctx); err == nil {
		t.Error("expected ListPublicUsers error")
	}
	if _, err := userRepo.GetAccessToken(ctx, "t"); err == nil {
		t.Error("expected GetAccessToken error")
	}
	if _, err := userRepo.ListDevices(ctx); err == nil {
		t.Error("expected ListDevices error")
	}
	if _, err := userRepo.GetAPIKey(ctx, "k"); err == nil {
		t.Error("expected GetAPIKey error")
	}
	if _, err := userRepo.ListAPIKeys(ctx); err == nil {
		t.Error("expected ListAPIKeys error")
	}
	if _, err := userRepo.GetDisplayPreferences(ctx, "id", "u", "c"); err == nil {
		t.Error("expected GetDisplayPreferences error")
	}
	if _, _, err := userRepo.ListActivityLogs(ctx, 0, 10, nil); err == nil {
		t.Error("expected ListActivityLogs error")
	}
	if _, err := userRepo.GetItemRuntimeTicks(ctx, item.ID); err == nil {
		t.Error("expected GetItemRuntimeTicks error")
	}
	if _, err := userRepo.GetUserItemData(ctx, "u", item.ID); err == nil {
		t.Error("expected userRepo.GetUserItemData error")
	}
	if _, err := userRepo.GetUserByID(ctx, "u"); err == nil {
		t.Error("expected GetUserByID error")
	}
}
