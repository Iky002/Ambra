package library

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/ambra-media/ambra/internal/api/dto"
	"github.com/ambra-media/ambra/internal/api/httpx"
	"github.com/ambra-media/ambra/internal/api/middleware"
	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestV0110_PolicyFilterPureHelpers(t *testing.T) {
	if got := computeSyntheticLibraryCoverTag("", "tag1"); got != "" {
		t.Fatalf("expected empty cover tag for empty libraryID, got %q", got)
	}
	if got := computeSyntheticLibraryCoverTag(guid.New(), "   "); got != "" {
		t.Fatalf("expected empty cover tag for blank childTag, got %q", got)
	}
	libID := guid.New()
	tag1 := computeSyntheticLibraryCoverTag(libID, "childA")
	tag2 := computeSyntheticLibraryCoverTag(libID, "childB")
	if len(tag1) != 16 || tag1 == tag2 {
		t.Fatalf("unexpected synthetic cover tags: %q vs %q", tag1, tag2)
	}

	if normalizeLowerSet(nil) != nil {
		t.Fatal("expected nil set for empty input")
	}
	set := normalizeLowerSet([]string{" Kids ", "", "FAMILY", "kids"})
	if len(set) != 2 || !set["kids"] || !set["family"] {
		t.Fatalf("unexpected normalized set: %+v", set)
	}

	fields := ensureFields([]string{"Overview", " tags "}, "Tags", "MediaStreams")
	if len(fields) != 3 || fields[2] != "MediaStreams" {
		t.Fatalf("unexpected ensureFields result: %+v", fields)
	}

	// matchesBlockUnratedType exemptions and wildcards/hierarchies
	for _, exempt := range []string{"CollectionFolder", "Folder", "UserView", "Person", "Genre", "MusicGenre", "GameGenre", "Studio"} {
		if matchesBlockUnratedType(exempt, "Video", []string{"*"}) {
			t.Fatalf("expected %s to be exempt from BlockUnratedItems", exempt)
		}
	}
	if matchesBlockUnratedType("Movie", "Video", []string{"", "  ", "Audio"}) {
		t.Fatal("did not expect Movie/Video to match Audio blockUnrated")
	}
	if !matchesBlockUnratedType("Movie", "Video", []string{"*"}) {
		t.Fatal("expected * to match Movie")
	}
	if !matchesBlockUnratedType("Movie", "Video", []string{"all"}) {
		t.Fatal("expected All to match Movie")
	}
	if !matchesBlockUnratedType("CustomClip", "Video", []string{"video"}) {
		t.Fatal("expected mediaType Video to match")
	}
	if !matchesBlockUnratedType("Season", "", []string{"Series"}) || !matchesBlockUnratedType("Episode", "Video", []string{"Series"}) {
		t.Fatal("expected Series rule to cover Season and Episode")
	}
	if !matchesBlockUnratedType("Audio", "Audio", []string{"Music"}) ||
		!matchesBlockUnratedType("MusicAlbum", "", []string{"Music"}) ||
		!matchesBlockUnratedType("MusicArtist", "", []string{"Music"}) {
		t.Fatal("expected Music rule to cover Audio/MusicAlbum/MusicArtist")
	}

	// HDR & Codec helpers
	for _, sdr := range []string{"", "  ", "sdr", "SDR", "BT709"} {
		if isHDRStreamRange(sdr) {
			t.Fatalf("expected %q not to be HDR", sdr)
		}
	}
	for _, hdr := range []string{"HDR10", "HDR10+", "HLG", "DOVI", "DolbyVision", "SMPTE2084-PQ", "BT2020-10", "SMPTEST2084", "ARIB-STD-B67"} {
		if !isHDRStreamRange(hdr) {
			t.Fatalf("expected %q to be HDR", hdr)
		}
	}

	if !streamMatchesRangeToken("", "SDR") || !streamMatchesRangeToken("SDR", "sdr") || streamMatchesRangeToken("HDR10", "SDR") {
		t.Fatal("SDR token matching failed")
	}
	if !streamMatchesRangeToken("DOVI", "HDR") || streamMatchesRangeToken("SDR", "HDR") {
		t.Fatal("HDR token matching failed")
	}
	if !streamMatchesRangeToken("DolbyVision", "DOVI") || !streamMatchesRangeToken("dovi", "dolbyvision") {
		t.Fatal("DOVI token matching failed")
	}
	if !streamMatchesRangeToken("HDR10+", "hdr10plus") || !streamMatchesRangeToken("HDR10PLUS", "HDR10+") {
		t.Fatal("HDR10+ token matching failed")
	}
	if !streamMatchesRangeToken("HLG", "hlg") || streamMatchesRangeToken("", "HLG") {
		t.Fatal("default range token matching failed")
	}

	for _, c := range []string{"h265", "x265", "HEVC"} {
		if normalizeCodecToken(c) != "hevc" {
			t.Fatalf("expected hevc for %q", c)
		}
	}
	for _, c := range []string{"avc", "x264", "H264"} {
		if normalizeCodecToken(c) != "h264" {
			t.Fatalf("expected h264 for %q", c)
		}
	}
	if normalizeCodecToken(" AV1 ") != "av1" {
		t.Fatal("expected av1 codec token")
	}

	req := httptest.NewRequest(http.MethodGet, "/Items?IsHDR=true&VideoRanges=HDR10&ExtendedVideoTypes=DOVI&VideoTypes=hlg,bluray,sdr&VideoCodecs=x265&VideoCodec=avc&Codec=av1", nil)
	splitFn := func(vals []string) []string { return vals }
	isHDR, ranges, codecs := parseVideoRangeAndHDRQuery(httpx.ParseQuery(req), splitFn)
	if isHDR == nil || !*isHDR {
		t.Fatal("expected IsHDR=true")
	}
	if len(ranges) != 4 || len(codecs) != 3 {
		t.Fatalf("unexpected ranges=%v codecs=%v", ranges, codecs)
	}
}

func TestV0110_ExtendedPolicyAndPostFiltersAndRoutes(t *testing.T) {
	tmp := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmp, "lib_v0110.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	ctx := context.Background()
	repo := sqlite.NewLibraryRepo(store)
	userRepo := sqlite.NewUserRepo(store)
	cfg := config.Default(tmp)

	libAllowed, err := repo.CreateLibrary(ctx, "AllowedLib", "movies", []string{filepath.Join(tmp, "allowed")})
	if err != nil {
		t.Fatal(err)
	}
	libRestricted, err := repo.CreateLibrary(ctx, "RestrictedLib", "movies", []string{filepath.Join(tmp, "restricted")})
	if err != nil {
		t.Fatal(err)
	}

	// Items in AllowedLib:
	// 1. movieHDRAllowed: Rated PG, Tag "approved", HDR10 + HEVC, Liked=true, Backdrop/Primary image tags
	movieHDRAllowed := &sqlite.ItemRecord{
		ID:             guid.New(),
		LibraryID:      libAllowed.ID,
		ParentID:       libAllowed.ID,
		Type:           "Movie",
		MediaType:      "Video",
		Name:           "Alpha HDR Movie",
		SortName:       "alpha hdr movie",
		OfficialRating: "PG",
		Path:           filepath.Join(tmp, "allowed", "alpha.mkv"),
	}
	// 2. movieSDRAllowed: Rated G, Tag "approved", SDR + H264, Liked=false
	movieSDRAllowed := &sqlite.ItemRecord{
		ID:             guid.New(),
		LibraryID:      libAllowed.ID,
		ParentID:       libAllowed.ID,
		Type:           "Movie",
		MediaType:      "Video",
		Name:           "Beta SDR Movie",
		SortName:       "beta sdr movie",
		OfficialRating: "G",
		Path:           filepath.Join(tmp, "allowed", "beta.mp4"),
	}
	// 3. movieUnrated: Unrated, Tag "approved" -> blocked when BlockUnratedItems=["Movie"]
	movieUnrated := &sqlite.ItemRecord{
		ID:        guid.New(),
		LibraryID: libAllowed.ID,
		ParentID:  libAllowed.ID,
		Type:      "Movie",
		MediaType: "Video",
		Name:      "Gamma Unrated Movie",
		SortName:  "gamma unrated movie",
		Path:      filepath.Join(tmp, "allowed", "gamma.mkv"),
	}
	// 4. movieWrongTag: Rated G, Tag "other" -> blocked when AllowedTags=["approved"]
	movieWrongTag := &sqlite.ItemRecord{
		ID:             guid.New(),
		LibraryID:      libAllowed.ID,
		ParentID:       libAllowed.ID,
		Type:           "Movie",
		MediaType:      "Video",
		Name:           "Delta WrongTag Movie",
		SortName:       "delta wrongtag movie",
		OfficialRating: "G",
		Path:           filepath.Join(tmp, "allowed", "delta.mkv"),
	}

	// 5. Series (Rated PG, Tag "approved") -> Season 0 (Special) & Season 1 -> Ep1, Ep2, Ep3 (unrated on episode, inherits rating & tag from Series!)
	seriesOK := &sqlite.ItemRecord{
		ID:             guid.New(),
		LibraryID:      libAllowed.ID,
		ParentID:       libAllowed.ID,
		Type:           "Series",
		IsFolder:       true,
		Name:           "Approved Series",
		SortName:       "approved series",
		OfficialRating: "PG",
	}
	s0 := 0
	s1 := 1
	e1, e2, e3 := 1, 2, 3
	seasonSpecial := &sqlite.ItemRecord{
		ID:          guid.New(),
		LibraryID:   libAllowed.ID,
		ParentID:    seriesOK.ID,
		SeriesID:    seriesOK.ID,
		Type:        "Season",
		IsFolder:    true,
		Name:        "Specials",
		IndexNumber: &s0,
	}
	season1 := &sqlite.ItemRecord{
		ID:          guid.New(),
		LibraryID:   libAllowed.ID,
		ParentID:    seriesOK.ID,
		SeriesID:    seriesOK.ID,
		Type:        "Season",
		IsFolder:    true,
		Name:        "Season 1",
		IndexNumber: &s1,
	}
	ep1 := &sqlite.ItemRecord{
		ID:                guid.New(),
		LibraryID:         libAllowed.ID,
		ParentID:          season1.ID,
		SeriesID:          seriesOK.ID,
		SeasonID:          season1.ID,
		Type:              "Episode",
		MediaType:         "Video",
		Name:              "Ep 1",
		ParentIndexNumber: &s1,
		IndexNumber:       &e1,
	}
	ep2 := &sqlite.ItemRecord{
		ID:                guid.New(),
		LibraryID:         libAllowed.ID,
		ParentID:          season1.ID,
		SeriesID:          seriesOK.ID,
		SeasonID:          season1.ID,
		Type:              "Episode",
		MediaType:         "Video",
		Name:              "Ep 2",
		ParentIndexNumber: &s1,
		IndexNumber:       &e2,
	}
	ep3 := &sqlite.ItemRecord{
		ID:                guid.New(),
		LibraryID:         libAllowed.ID,
		ParentID:          season1.ID,
		SeriesID:          seriesOK.ID,
		SeasonID:          season1.ID,
		Type:              "Episode",
		MediaType:         "Video",
		Name:              "Ep 3",
		ParentIndexNumber: &s1,
		IndexNumber:       &e3,
	}

	// SeriesUnrated: neither series nor episode has rating -> blocked by BlockUnratedItems
	seriesUnrated := &sqlite.ItemRecord{
		ID:        guid.New(),
		LibraryID: libAllowed.ID,
		ParentID:  libAllowed.ID,
		Type:      "Series",
		IsFolder:  true,
		Name:      "Unrated Series",
	}
	epUnrated := &sqlite.ItemRecord{
		ID:                guid.New(),
		LibraryID:         libAllowed.ID,
		ParentID:          seriesUnrated.ID,
		SeriesID:          seriesUnrated.ID,
		Type:              "Episode",
		MediaType:         "Video",
		Name:              "Unrated Ep",
		ParentIndexNumber: &s1,
		IndexNumber:       &e1,
	}

	// Additional item types for handleItemCounts switch branches
	extraTypes := []*sqlite.ItemRecord{
		{ID: guid.New(), LibraryID: libAllowed.ID, ParentID: libAllowed.ID, Type: "MusicArtist", Name: "Artist1", OfficialRating: "G"},
		{ID: guid.New(), LibraryID: libAllowed.ID, ParentID: libAllowed.ID, Type: "Audio", MediaType: "Audio", Name: "Song1", OfficialRating: "G"},
		{ID: guid.New(), LibraryID: libAllowed.ID, ParentID: libAllowed.ID, Type: "MusicAlbum", Name: "Album1", OfficialRating: "G"},
		{ID: guid.New(), LibraryID: libAllowed.ID, ParentID: libAllowed.ID, Type: "MusicVideo", MediaType: "Video", Name: "MV1", OfficialRating: "G"},
		{ID: guid.New(), LibraryID: libAllowed.ID, ParentID: libAllowed.ID, Type: "BoxSet", Name: "Box1", OfficialRating: "G"},
		{ID: guid.New(), LibraryID: libAllowed.ID, ParentID: libAllowed.ID, Type: "Book", Name: "Book1", OfficialRating: "G"},
		{ID: guid.New(), LibraryID: libAllowed.ID, ParentID: libAllowed.ID, Type: "Trailer", MediaType: "Video", Name: "Trailer1", OfficialRating: "G"},
		{ID: guid.New(), LibraryID: libAllowed.ID, ParentID: libAllowed.ID, Type: "Program", Name: "Prog1", OfficialRating: "G"},
		{ID: guid.New(), LibraryID: libAllowed.ID, ParentID: movieHDRAllowed.ID, Type: "Video", Extra: sqlite.ItemExtra{ExtraType: "behindthescenes"}, Name: "Extra1", OfficialRating: "G"},
	}

	allItems := []*sqlite.ItemRecord{
		movieHDRAllowed, movieSDRAllowed, movieUnrated, movieWrongTag,
		seriesOK, seasonSpecial, season1, ep1, ep2, ep3,
		seriesUnrated, epUnrated,
	}
	allItems = append(allItems, extraTypes...)
	for _, it := range allItems {
		if err := repo.UpsertItem(ctx, it); err != nil {
			t.Fatal(err)
		}
		_ = repo.UpsertSearchFTS(ctx, it.ID, it.Name+" SearchToken", "", "", "", "")
	}

	// Attach Primary image to movieSDRAllowed so libraryToViewDto picks it up
	_ = repo.UpsertImage(ctx, &sqlite.ImageRecord{
		ItemID:    movieSDRAllowed.ID,
		ImageType: "Primary",
		Path:      filepath.Join(tmp, "cover.jpg"),
		Tag:       "primtag123",
	})

	// Taxonomies
	for _, taggedID := range []string{movieHDRAllowed.ID, movieSDRAllowed.ID, movieUnrated.ID, seriesOK.ID} {
		_ = repo.ReplaceItemTaxonomies(ctx, taggedID, "tag", []string{"Approved"})
	}
	for _, et := range extraTypes {
		_ = repo.ReplaceItemTaxonomies(ctx, et.ID, "tag", []string{"Approved"})
	}
	_ = repo.ReplaceItemTaxonomies(ctx, movieWrongTag.ID, "tag", []string{"Other"})
	_ = repo.ReplaceItemTaxonomies(ctx, seriesUnrated.ID, "tag", []string{"Approved"})
	_ = repo.ReplaceItemTaxonomies(ctx, movieHDRAllowed.ID, "genre", []string{"Sci-Fi", "Action"})
	_ = repo.ReplaceItemTaxonomies(ctx, movieSDRAllowed.ID, "genre", []string{"Drama"})

	// Media sources & streams for HDR/SDR/codec tests
	_ = repo.ReplaceMediaSourcesAndStreams(ctx, movieHDRAllowed.ID, []sqlite.MediaSourceRecord{
		{
			ID: movieHDRAllowed.ID, ItemID: movieHDRAllowed.ID, Name: "alpha.mkv", Path: movieHDRAllowed.Path, Container: "mkv",
			Streams: []sqlite.MediaStreamRecord{
				{StreamIndex: 0, StreamType: "Video", Codec: "hevc", VideoRange: "HDR10", Width: 3840, Height: 2160},
				{StreamIndex: 1, StreamType: "Audio", Codec: "eac3", Language: "eng"},
			},
		},
	})
	_ = repo.ReplaceMediaSourcesAndStreams(ctx, movieSDRAllowed.ID, []sqlite.MediaSourceRecord{
		{
			ID: movieSDRAllowed.ID, ItemID: movieSDRAllowed.ID, Name: "beta.mp4", Path: movieSDRAllowed.Path, Container: "mp4",
			Streams: []sqlite.MediaStreamRecord{
				{StreamIndex: 0, StreamType: "Video", Codec: "h264", VideoRange: "SDR", Width: 1920, Height: 1080},
			},
		},
	})

	// Create restricted user with BlockUnratedItems + AllowedTags + EnabledFolders
	maxR := 7
	pol := sqlite.DefaultUserPolicy(false)
	pol.EnableAllFolders = false
	pol.EnabledFolders = []string{libAllowed.ID}
	pol.MaxParentalRating = &maxR
	pol.BlockUnratedItems = []string{"Movie", "Series"}
	pol.AllowedTags = []string{"approved"}

	strictUser := &sqlite.UserRecord{
		ID:     guid.New(),
		Name:   "strict_kid",
		Policy: pol,
		Config: sqlite.DefaultUserConfig(),
	}
	if err := userRepo.CreateUser(ctx, strictUser); err != nil {
		t.Fatal(err)
	}
	otherUser := &sqlite.UserRecord{
		ID:     guid.New(),
		Name:   "other_user",
		Policy: sqlite.DefaultUserPolicy(false),
		Config: sqlite.DefaultUserConfig(),
	}
	_ = userRepo.CreateUser(ctx, otherUser)

	// Private playlist owned by otherUser
	privPL, err := repo.CreatePlaylist(ctx, "OtherPrivateSearchToken", "Video", otherUser.ID, []string{movieHDRAllowed.ID})
	if err != nil {
		t.Fatal(err)
	}
	_ = repo.UpsertSearchFTS(ctx, privPL.ID, privPL.Name, "", "", "", "")

	// Likes, Played, and Resume data for strictUser
	_, err = store.Writer.ExecContext(ctx,
		`INSERT INTO user_item_data (user_id, item_id, played, play_count, is_favorite, likes, playback_position_ticks, last_played_date)
		 VALUES (?, ?, 0, 0, 1, 1, 40000000, 1700000100),
		        (?, ?, 0, 0, 0, 0, 30000000, 1700000200),
		        (?, ?, 0, 0, 0, 1, 20000000, 1700000300),
		        (?, ?, 1, 1, 0, 1, 0, 1700000400)`,
		strictUser.ID, movieHDRAllowed.ID,
		strictUser.ID, movieSDRAllowed.ID,
		strictUser.ID, movieWrongTag.ID,
		strictUser.ID, ep1.ID,
	)
	if err != nil {
		t.Fatal(err)
	}

	// Direct unit checks on Handler methods without store/userRepo (fallback paths)
	hNoStore := New(repo, nil, cfg)
	fallbackPol := extendedParentalPolicy{
		blockUnratedItems: []string{"Movie", "Series"},
		allowedTags:       []string{"approved"},
	}
	// recordHasAnyTag fallback via h.repo.GetItemByID when store & userRepo are nil
	if !hNoStore.recordHasAnyTag(ctx, movieHDRAllowed.ID, "", map[string]bool{"approved": true}) {
		t.Fatal("expected recordHasAnyTag repo fallback to find Approved tag")
	}
	if !hNoStore.recordHasAnyTag(ctx, ep1.ID, seriesOK.ID, map[string]bool{"approved": true}) {
		t.Fatal("expected recordHasAnyTag repo fallback to find Approved tag via SeriesID")
	}
	if hNoStore.recordHasAnyTag(ctx, "", "", map[string]bool{"approved": true}) {
		t.Fatal("expected false for empty IDs in recordHasAnyTag")
	}
	// allowedLibrariesForRequest & userExtendedPolicyForRequest when userRepo is nil
	reqNoRepo := httptest.NewRequest(http.MethodGet, "/Items", nil)
	reqNoRepo = reqNoRepo.WithContext(middleware.WithAuthContext(ctx, &middleware.AuthContext{User: strictUser}))
	libsNoRepo := hNoStore.allowedLibrariesForRequest(reqNoRepo, "")
	if len(libsNoRepo) != 1 || libsNoRepo[0] != guid.Normalize(libAllowed.ID) {
		t.Fatalf("expected allowedLibrariesForRequest fallback from ac.User, got %v", libsNoRepo)
	}
	polNoRepo := hNoStore.userExtendedPolicyForRequest(reqNoRepo, "")
	if !polNoRepo.hasExtraPolicy() || len(polNoRepo.allowedTags) != 1 {
		t.Fatalf("expected userExtendedPolicyForRequest fallback from ac.User, got %+v", polNoRepo)
	}
	// filterDTOItemsPost direct coverage for MediaSources fallback & repo fallback
	tBool := true
	fBool := false
	dtoFromMediaSources := dto.BaseItemDto{
		Id:             movieHDRAllowed.ID,
		Type:           "Movie",
		OfficialRating: "PG",
		Tags:           []string{"Approved"},
		UserData:       &dto.UserItemDataDto{Likes: &tBool},
		MediaSources: []dto.MediaSourceInfo{
			{MediaStreams: []dto.MediaStream{{Type: "Audio", Codec: "aac"}, {Type: "Video", Codec: "hevc", VideoRange: "DOVI"}}},
		},
	}
	dtoFromRepoStreams := dto.BaseItemDto{
		Id:             movieHDRAllowed.ID,
		Type:           "Movie",
		OfficialRating: "PG",
	}
	dtoSDR := dto.BaseItemDto{
		Id:             movieSDRAllowed.ID,
		Type:           "Movie",
		OfficialRating: "G",
		Tags:           []string{"Approved"},
		MediaStreams:   []dto.MediaStream{{Type: "Video", Codec: "h264", VideoRange: "SDR"}},
	}
	gotHDR := hNoStore.filterDTOItemsPost(ctx, strictUser.ID, []dto.BaseItemDto{dtoFromMediaSources, dtoFromRepoStreams, dtoSDR}, fallbackPol, nil, &tBool, []string{"HDR"}, []string{"h265"})
	if len(gotHDR) != 2 {
		t.Fatalf("expected 2 HDR/HEVC items, got %d", len(gotHDR))
	}
	gotSDR := hNoStore.filterDTOItemsPost(ctx, strictUser.ID, []dto.BaseItemDto{dtoFromMediaSources, dtoSDR}, fallbackPol, nil, &fBool, []string{"SDR"}, []string{"avc"})
	if len(gotSDR) != 1 || gotSDR[0].Id != movieSDRAllowed.ID {
		t.Fatalf("expected 1 SDR/H264 item, got %+v", gotSDR)
	}
	if len(hNoStore.filterDTOItemsPost(ctx, "", nil, fallbackPol, nil, nil, nil, nil)) != 0 {
		t.Fatal("expected empty slice from filterDTOItemsPost on nil items")
	}

	// Full HTTP mux with store + userRepo wired
	mux := http.NewServeMux()
	h := New(repo, nil, cfg)
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

	strictAC := &middleware.AuthContext{User: strictUser}
	doReq := func(method, target string, ac *middleware.AuthContext) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, target, nil)
		if ac != nil {
			req = req.WithContext(middleware.WithAuthContext(req.Context(), ac))
		}
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}

	// 1. Views & GetItem on Library ID (allowed vs forbidden)
	rec := doReq(http.MethodGet, "/Users/"+strictUser.ID+"/Views", strictAC)
	if rec.Code != http.StatusOK {
		t.Fatalf("Views status=%d", rec.Code)
	}
	if rec := doReq(http.MethodGet, "/Users/"+otherUser.ID+"/Views", strictAC); rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 accessing other user's Views, got %d", rec.Code)
	}
	if rec := doReq(http.MethodGet, "/Items/"+libAllowed.ID, strictAC); rec.Code != http.StatusOK {
		t.Fatalf("expected 200 GetItem on allowed library ID, got %d", rec.Code)
	}
	if rec := doReq(http.MethodGet, "/Items/"+libRestricted.ID, strictAC); rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 GetItem on restricted library ID, got %d", rec.Code)
	}

	// 2. GetItem blocked by ExtraPolicy (unrated movie, wrong tag movie, unrated series episode, private playlist) vs allowed (episode inheriting rating+tag & movie with MediaSources track selection)
	for _, blockedID := range []string{movieUnrated.ID, movieWrongTag.ID, epUnrated.ID, privPL.ID} {
		if rec := doReq(http.MethodGet, "/Users/"+strictUser.ID+"/Items/"+blockedID, strictAC); rec.Code != http.StatusForbidden {
			t.Fatalf("expected 403 for blocked item %s, got %d", blockedID, rec.Code)
		}
	}
	if rec := doReq(http.MethodGet, "/Users/"+strictUser.ID+"/Items/"+ep2.ID, strictAC); rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for episode inheriting series rating and allowed tag, got %d", rec.Code)
	}
	if rec := doReq(http.MethodGet, "/Users/"+strictUser.ID+"/Items/"+movieHDRAllowed.ID, strictAC); rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for movieHDRAllowed with MediaSources track selection, got %d", rec.Code)
	}

	// Post-query private playlist filtering when store/userRepo are nil on Handler
	muxNoStore := http.NewServeMux()
	hNoStore.RegisterRoutes(muxNoStore, AuthFuncs{})
	openKid := *otherUser
	reqPL := httptest.NewRequest(http.MethodGet, "/Items?IncludeItemTypes=Playlist", nil)
	reqPL = reqPL.WithContext(middleware.WithAuthContext(ctx, &middleware.AuthContext{User: strictUser}))
	recPL := httptest.NewRecorder()
	muxNoStore.ServeHTTP(recPL, reqPL)
	if recPL.Code != http.StatusOK {
		t.Fatalf("muxNoStore /Items playlist filter status=%d", recPL.Code)
	}
	_ = openKid

	// 3. QueryItems with Filters, IsLiked, IsHDR, VideoRanges, VideoCodecs, MinCommunityRating, Years, cross-user 403, and pagination
	if rec := doReq(http.MethodGet, "/Users/"+otherUser.ID+"/Items", strictAC); rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 on cross-user /Items, got %d", rec.Code)
	}
	rec = doReq(http.MethodGet, "/Users/"+strictUser.ID+"/Items?Recursive=true&IncludeItemTypes=Movie&Filters=IsLikes,IsFavorite&IsHDR=true&VideoRanges=HDR10&VideoCodecs=hevc", strictAC)
	if rec.Code != http.StatusOK {
		t.Fatalf("QueryItems HDR/Liked status=%d body=%s", rec.Code, rec.Body.String())
	}
	var qRes dto.QueryResult[dto.BaseItemDto]
	_ = json.Unmarshal(rec.Body.Bytes(), &qRes)
	if qRes.TotalRecordCount != 1 || qRes.Items[0].Id != movieHDRAllowed.ID {
		t.Fatalf("expected only movieHDRAllowed, got %+v", qRes)
	}
	// Pagination out-of-range and slice branches on post-filtered QueryItems
	rec = doReq(http.MethodGet, "/Users/"+strictUser.ID+"/Items?Recursive=true&IncludeItemTypes=Movie&StartIndex=50&Limit=1", strictAC)
	_ = json.Unmarshal(rec.Body.Bytes(), &qRes)
	if len(qRes.Items) != 0 {
		t.Fatalf("expected 0 items for StartIndex=50, got %d", len(qRes.Items))
	}
	rec = doReq(http.MethodGet, "/Users/"+strictUser.ID+"/Items?Recursive=true&IncludeItemTypes=Movie&StartIndex=1&Limit=1", strictAC)
	_ = json.Unmarshal(rec.Body.Bytes(), &qRes)
	if len(qRes.Items) != 1 {
		t.Fatalf("expected 1 item for StartIndex=1&Limit=1, got %d", len(qRes.Items))
	}
	rec = doReq(http.MethodGet, "/Users/"+strictUser.ID+"/Items?Recursive=true&IncludeItemTypes=Movie&Filters=IsDislikes&IsHDR=false&StartIndex=0&Limit=1", strictAC)
	_ = json.Unmarshal(rec.Body.Bytes(), &qRes)
	if len(qRes.Items) != 1 || qRes.Items[0].Id != movieSDRAllowed.ID {
		t.Fatalf("expected movieSDRAllowed for IsDislikes+SDR, got %+v", qRes)
	}

	// 4. LatestItems & ResumeItems with ExtraPolicy & pagination
	if rec := doReq(http.MethodGet, "/Users/"+otherUser.ID+"/Items/Latest", strictAC); rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 on other user's Latest, got %d", rec.Code)
	}
	rec = doReq(http.MethodGet, "/Users/"+strictUser.ID+"/Items/Latest?Limit=1", strictAC)
	if rec.Code != http.StatusOK {
		t.Fatalf("Latest status=%d", rec.Code)
	}
	var latestItems []dto.BaseItemDto
	_ = json.Unmarshal(rec.Body.Bytes(), &latestItems)
	if len(latestItems) != 1 {
		t.Fatalf("expected 1 latest item with Limit=1, got %d", len(latestItems))
	}

	if rec := doReq(http.MethodGet, "/Users/"+otherUser.ID+"/Items/Resume", strictAC); rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 on other user's Resume, got %d", rec.Code)
	}
	rec = doReq(http.MethodGet, "/Users/"+strictUser.ID+"/Items/Resume?StartIndex=1&Limit=1", strictAC)
	_ = json.Unmarshal(rec.Body.Bytes(), &qRes)
	if qRes.TotalRecordCount != 2 || len(qRes.Items) != 1 {
		t.Fatalf("expected 2 resumable allowed items (movieWrongTag filtered out) and 1 paged item, got %+v", qRes)
	}
	rec = doReq(http.MethodGet, "/Users/"+strictUser.ID+"/Items/Resume?StartIndex=99&Limit=1", strictAC)
	_ = json.Unmarshal(rec.Body.Bytes(), &qRes)
	if len(qRes.Items) != 0 {
		t.Fatalf("expected empty slice when Resume StartIndex > total, got %d", len(qRes.Items))
	}

	// 5. ItemCounts with ExtraPolicy across all item types
	if rec := doReq(http.MethodGet, "/Items/Counts?UserId="+otherUser.ID, strictAC); rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 on other user's ItemCounts, got %d", rec.Code)
	}
	rec = doReq(http.MethodGet, "/Items/Counts?UserId="+strictUser.ID, strictAC)
	if rec.Code != http.StatusOK {
		t.Fatalf("ItemCounts status=%d", rec.Code)
	}
	var counts sqlite.ItemCounts
	_ = json.Unmarshal(rec.Body.Bytes(), &counts)
	if counts.MovieCount != 2 || counts.SeriesCount != 1 || counts.ArtistCount != 1 || counts.SongCount != 1 ||
		counts.AlbumCount != 1 || counts.MusicVideoCount != 1 || counts.BoxSetCount != 1 || counts.BookCount != 1 ||
		counts.TrailerCount != 1 || counts.ProgramCount != 1 {
		t.Fatalf("unexpected filtered counts: %+v", counts)
	}

	// 6. ShowSeasons, ShowEpisodes, NextUp with ExtraPolicy & post filters
	if rec := doReq(http.MethodGet, "/Shows/"+seriesOK.ID+"/Seasons?UserId="+otherUser.ID, strictAC); rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 cross-user Seasons, got %d", rec.Code)
	}
	if rec := doReq(http.MethodGet, "/Shows/"+seriesUnrated.ID+"/Seasons", strictAC); rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 on unrated series Seasons, got %d", rec.Code)
	}
	rec = doReq(http.MethodGet, "/Shows/"+seriesOK.ID+"/Seasons?IsSpecialSeason=false&StartIndex=0&Limit=1", strictAC)
	_ = json.Unmarshal(rec.Body.Bytes(), &qRes)
	if len(qRes.Items) != 1 || qRes.Items[0].Id != season1.ID {
		t.Fatalf("expected season1 for IsSpecialSeason=false, got %+v", qRes)
	}
	rec = doReq(http.MethodGet, "/Shows/"+seriesOK.ID+"/Seasons?StartIndex=1&Limit=1", strictAC)
	_ = json.Unmarshal(rec.Body.Bytes(), &qRes)
	if len(qRes.Items) != 1 {
		t.Fatalf("expected 1 season for StartIndex=1, got %d", len(qRes.Items))
	}
	rec = doReq(http.MethodGet, "/Shows/"+seriesOK.ID+"/Seasons?StartIndex=50&Limit=1", strictAC)
	_ = json.Unmarshal(rec.Body.Bytes(), &qRes)
	if len(qRes.Items) != 0 {
		t.Fatalf("expected 0 seasons for StartIndex=50, got %d", len(qRes.Items))
	}

	if rec := doReq(http.MethodGet, "/Shows/"+seriesOK.ID+"/Episodes?UserId="+otherUser.ID, strictAC); rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 cross-user Episodes, got %d", rec.Code)
	}
	if rec := doReq(http.MethodGet, "/Shows/"+seriesUnrated.ID+"/Episodes", strictAC); rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 on unrated series Episodes, got %d", rec.Code)
	}
	rec = doReq(http.MethodGet, "/Shows/"+seriesOK.ID+"/Episodes?Season=1&AdjacentTo="+ep2.ID, strictAC)
	_ = json.Unmarshal(rec.Body.Bytes(), &qRes)
	if len(qRes.Items) != 3 {
		t.Fatalf("expected 3 adjacent episodes around ep2, got %d", len(qRes.Items))
	}
	rec = doReq(http.MethodGet, "/Shows/"+seriesOK.ID+"/Episodes?StartItemId="+ep2.ID+"&StartIndex=1&Limit=1", strictAC)
	_ = json.Unmarshal(rec.Body.Bytes(), &qRes)
	if len(qRes.Items) != 1 || qRes.Items[0].Id != ep3.ID {
		t.Fatalf("expected ep3 after StartItemId=ep2 + StartIndex=1, got %+v", qRes)
	}
	rec = doReq(http.MethodGet, "/Shows/"+seriesOK.ID+"/Episodes?StartItemId="+guid.New(), strictAC)
	_ = json.Unmarshal(rec.Body.Bytes(), &qRes)
	if len(qRes.Items) != 0 {
		t.Fatal("expected empty episodes for unknown StartItemId")
	}
	rec = doReq(http.MethodGet, "/Shows/"+seriesOK.ID+"/Episodes?AdjacentTo="+guid.New(), strictAC)
	_ = json.Unmarshal(rec.Body.Bytes(), &qRes)
	if len(qRes.Items) != 0 {
		t.Fatal("expected empty episodes for unknown AdjacentTo")
	}
	rec = doReq(http.MethodGet, "/Shows/"+seriesOK.ID+"/Episodes?Season=1&StartIndex=50", strictAC)
	_ = json.Unmarshal(rec.Body.Bytes(), &qRes)
	if len(qRes.Items) != 0 {
		t.Fatal("expected empty episodes for StartIndex=50")
	}

	if rec := doReq(http.MethodGet, "/Shows/NextUp?UserId="+otherUser.ID, strictAC); rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 on other user's NextUp, got %d", rec.Code)
	}
	rec = doReq(http.MethodGet, "/Shows/NextUp?UserId="+strictUser.ID+"&Limit=1", strictAC)
	if rec.Code != http.StatusOK {
		t.Fatalf("NextUp status=%d", rec.Code)
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &qRes)
	if len(qRes.Items) != 1 || qRes.Items[0].Id != ep2.ID {
		t.Fatalf("expected ep2 as NextUp after ep1 played, got %+v", qRes)
	}

	// 7. SearchHints & Taxonomies post-processing
	if rec := doReq(http.MethodGet, "/Search/Hints?UserId="+otherUser.ID, strictAC); rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 on other user's SearchHints, got %d", rec.Code)
	}
	rec = doReq(http.MethodGet, "/Search/Hints?SearchTerm=SearchToken&Limit=1", strictAC)
	if rec.Code != http.StatusOK {
		t.Fatalf("SearchHints status=%d", rec.Code)
	}
	var hints dto.SearchHintResult
	_ = json.Unmarshal(rec.Body.Bytes(), &hints)
	if len(hints.SearchHints) != 1 {
		t.Fatalf("expected 1 capped search hint, got %+v", hints)
	}

	rec = doReq(http.MethodGet, "/Genres?SearchTerm=sci&NameStartsWith=s&SortOrder=Descending&StartIndex=0&Limit=5", strictAC)
	if rec.Code != http.StatusOK {
		t.Fatalf("Genres status=%d", rec.Code)
	}
	rec = doReq(http.MethodGet, "/Genres?SortBy=Random&StartIndex=99&Limit=5", strictAC)
	if rec.Code != http.StatusOK {
		t.Fatalf("Genres random status=%d", rec.Code)
	}

	// 8. MarkPlayed / MarkUnplayed / MarkFavorite / UnmarkFavorite RBAC (cross-user & blocked item) & DatePlayed
	for _, m := range []struct {
		method string
		path   string
	}{
		{http.MethodPost, "/Users/" + otherUser.ID + "/PlayedItems/" + movieSDRAllowed.ID},
		{http.MethodDelete, "/Users/" + otherUser.ID + "/PlayedItems/" + movieSDRAllowed.ID},
		{http.MethodPost, "/Users/" + otherUser.ID + "/FavoriteItems/" + movieSDRAllowed.ID},
		{http.MethodDelete, "/Users/" + otherUser.ID + "/FavoriteItems/" + movieSDRAllowed.ID},
		{http.MethodPost, "/Users/" + strictUser.ID + "/PlayedItems/" + movieUnrated.ID},
		{http.MethodDelete, "/Users/" + strictUser.ID + "/PlayedItems/" + movieUnrated.ID},
		{http.MethodPost, "/Users/" + strictUser.ID + "/FavoriteItems/" + movieUnrated.ID},
		{http.MethodDelete, "/Users/" + strictUser.ID + "/FavoriteItems/" + movieUnrated.ID},
		{http.MethodPost, "/Users/" + strictUser.ID + "/FavoriteItems/" + privPL.ID},
	} {
		if rec := doReq(m.method, m.path, strictAC); rec.Code != http.StatusForbidden {
			t.Fatalf("expected 403 on %s %s, got %d", m.method, m.path, rec.Code)
		}
	}
	if rec := doReq(http.MethodPost, "/Users/"+strictUser.ID+"/PlayedItems/"+movieSDRAllowed.ID+"?DatePlayed=2025-02-03", strictAC); rec.Code != http.StatusOK {
		t.Fatalf("expected 200 on MarkPlayed with DatePlayed, got %d", rec.Code)
	}
	// Fallback helper checks without AuthContext
	plainReq := httptest.NewRequest(http.MethodGet, "/Items", nil)
	if libs := h.allowedLibrariesForRequest(plainReq, strictUser.ID); len(libs) != 1 {
		t.Fatalf("expected fallbackUserID library resolution, got %v", libs)
	}
	if maxR, _ := h.userParentalPolicyForRequest(plainReq, guid.New()); maxR == nil || *maxR != 0 {
		t.Fatalf("expected fail-closed maxRating=0 for unknown fallback user, got %v", maxR)
	}
	if ok, err := (&Handler{}).canAccessItem(plainReq, "", guid.New()); !ok || err != nil {
		t.Fatalf("expected nil-repo Handler.canAccessItem to return true, got %v %v", ok, err)
	}
}

func TestItemCountsExcludesOtherUsersPrivatePlaylists(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmp, "counts_playlists.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	repo := sqlite.NewLibraryRepo(store)
	userRepo := sqlite.NewUserRepo(store)
	cfg := config.Default(tmp)

	libDir := filepath.Join(tmp, "movies")
	_ = os.MkdirAll(libDir, 0o755)
	lib, err := repo.CreateLibrary(ctx, "Movies", "movies", []string{libDir})
	if err != nil {
		t.Fatal(err)
	}

	mov := &sqlite.ItemRecord{
		ID:        guid.New(),
		LibraryID: lib.ID,
		ParentID:  lib.ID,
		Type:      "Movie",
		Name:      "Shared Film",
		Path:      filepath.Join(libDir, "shared.mkv"),
	}
	if err := repo.UpsertItem(ctx, mov); err != nil {
		t.Fatal(err)
	}

	admin := &sqlite.UserRecord{
		ID:      guid.New(),
		Name:    "admin",
		IsAdmin: true,
		Policy:  sqlite.DefaultUserPolicy(true),
		Config:  sqlite.DefaultUserConfig(),
	}
	userA := &sqlite.UserRecord{
		ID:     guid.New(),
		Name:   "userA",
		Policy: sqlite.DefaultUserPolicy(false),
		Config: sqlite.DefaultUserConfig(),
	}
	userB := &sqlite.UserRecord{
		ID:     guid.New(),
		Name:   "userB",
		Policy: sqlite.DefaultUserPolicy(false),
		Config: sqlite.DefaultUserConfig(),
	}
	for _, u := range []*sqlite.UserRecord{admin, userA, userB} {
		if err := userRepo.CreateUser(ctx, u); err != nil {
			t.Fatal(err)
		}
	}

	// Create a private playlist and a public playlist owned by userA.
	_, err = repo.CreatePlaylist(ctx, "UserA Private Mix", "Video", userA.ID, []string{mov.ID})
	if err != nil {
		t.Fatal(err)
	}
	pubPL, err := repo.CreatePlaylist(ctx, "UserA Public Mix", "Video", userA.ID, []string{mov.ID})
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.Writer.ExecContext(ctx,
		`INSERT INTO server_config (key, value_json, updated_at) VALUES (?, ?, 1)`,
		"item_visibility_"+guid.Normalize(pubPL.ID), `{"IsPublic":true}`,
	)
	if err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	h := New(repo, nil, cfg)
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

	getCounts := func(u *sqlite.UserRecord) sqlite.ItemCounts {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/Items/Counts", nil)
		req = req.WithContext(middleware.WithAuthContext(req.Context(), &middleware.AuthContext{User: u}))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET /Items/Counts for %s status=%d body=%s", u.Name, rec.Code, rec.Body.String())
		}
		var out sqlite.ItemCounts
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		return out
	}

	// Admin warms cachedItemCounts and sees Movie + UserA Private + UserA Public = 3 items.
	adminCounts := getCounts(admin)
	if adminCounts.MovieCount != 1 || adminCounts.ItemCount != 3 {
		t.Fatalf("expected admin counts MovieCount=1 ItemCount=3, got %+v", adminCounts)
	}

	// User A owns the private playlist, so User A also sees Movie + Private + Public = 3 items.
	userACounts := getCounts(userA)
	if userACounts.MovieCount != 1 || userACounts.ItemCount != 3 {
		t.Fatalf("expected userA counts MovieCount=1 ItemCount=3, got %+v", userACounts)
	}

	// User B must NOT see User A's private playlist (and must NOT receive admin's cachedItemCounts):
	// Movie + UserA Public = 2 items.
	userBCounts := getCounts(userB)
	if userBCounts.MovieCount != 1 || userBCounts.ItemCount != 2 {
		t.Fatalf("expected userB counts MovieCount=1 ItemCount=2 (excluding userA's private playlist), got %+v", userBCounts)
	}
}

