package catalog

import (
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

func TestV0110_CatalogPolicyFilterHelpers(t *testing.T) {
	if normalizeLowerSet(nil) != nil {
		t.Fatal("expected nil set for empty slice")
	}
	ns := normalizeLowerSet([]string{" Family ", "", "KIDS", "family"})
	if len(ns) != 2 || !ns["family"] || !ns["kids"] {
		t.Fatalf("unexpected normalizeLowerSet: %+v", ns)
	}

	ef := ensureFields([]string{"Overview", "Tags"}, "tags", "Genres")
	if len(ef) != 3 || ef[2] != "Genres" {
		t.Fatalf("unexpected ensureFields: %+v", ef)
	}

	for _, exempt := range []string{"CollectionFolder", "Folder", "UserView", "Person", "Genre", "Studio"} {
		if matchesBlockUnratedType(exempt, "Video", []string{"*"}) {
			t.Fatalf("expected %s exempt from matchesBlockUnratedType", exempt)
		}
	}
	if matchesBlockUnratedType("Movie", "Video", []string{"", " ", "Audio"}) {
		t.Fatal("unexpected match on Audio for Movie")
	}
	if !matchesBlockUnratedType("Movie", "Video", []string{"*"}) ||
		!matchesBlockUnratedType("Movie", "Video", []string{"All"}) ||
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

func TestV0110_CatalogExtendedPolicyAndEndpoints(t *testing.T) {
	tmp := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmp, "cat_v0110.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	ctx := context.Background()
	repo := sqlite.NewLibraryRepo(store)
	userRepo := sqlite.NewUserRepo(store)
	cfg := config.Default(tmp)

	libAllowed, _ := repo.CreateLibrary(ctx, "Allowed", "movies", []string{filepath.Join(tmp, "allowed")})
	libBlocked, _ := repo.CreateLibrary(ctx, "Blocked", "movies", []string{filepath.Join(tmp, "blocked")})

	y2024 := 2024
	movieOK1 := &sqlite.ItemRecord{
		ID:             guid.New(),
		LibraryID:      libAllowed.ID,
		ParentID:       libAllowed.ID,
		Type:           "Movie",
		MediaType:      "Video",
		Name:           "Approved Adventure 1",
		OfficialRating: "PG",
		ProductionYear: &y2024,
		Path:           filepath.Join(tmp, "allowed", "adv1.mkv"),
	}
	movieOK2 := &sqlite.ItemRecord{
		ID:             guid.New(),
		LibraryID:      libAllowed.ID,
		ParentID:       libAllowed.ID,
		Type:           "Movie",
		MediaType:      "Video",
		Name:           "Approved Adventure 2",
		OfficialRating: "G",
		ProductionYear: &y2024,
		Path:           filepath.Join(tmp, "allowed", "adv2.mkv"),
	}
	movieUnrated := &sqlite.ItemRecord{
		ID:        guid.New(),
		LibraryID: libAllowed.ID,
		ParentID:  libAllowed.ID,
		Type:      "Movie",
		MediaType: "Video",
		Name:      "Unrated Adventure",
		Path:      filepath.Join(tmp, "allowed", "unrated.mkv"),
	}
	movieWrongTag := &sqlite.ItemRecord{
		ID:             guid.New(),
		LibraryID:      libAllowed.ID,
		ParentID:       libAllowed.ID,
		Type:           "Movie",
		MediaType:      "Video",
		Name:           "WrongTag Adventure",
		OfficialRating: "G",
		Path:           filepath.Join(tmp, "allowed", "wrongtag.mkv"),
	}
	movieOtherLib := &sqlite.ItemRecord{
		ID:             guid.New(),
		LibraryID:      libBlocked.ID,
		ParentID:       libBlocked.ID,
		Type:           "Movie",
		MediaType:      "Video",
		Name:           "Secret Lib Movie",
		OfficialRating: "G",
		Path:           filepath.Join(tmp, "blocked", "secret.mkv"),
	}

	// Series + Episode (episode inherits rating & tag from Series)
	seriesRated := &sqlite.ItemRecord{
		ID:             guid.New(),
		LibraryID:      libAllowed.ID,
		ParentID:       libAllowed.ID,
		Type:           "Series",
		IsFolder:       true,
		Name:           "Rated Series",
		OfficialRating: "PG",
	}
	epInherited := &sqlite.ItemRecord{
		ID:        guid.New(),
		LibraryID: libAllowed.ID,
		ParentID:  seriesRated.ID,
		SeriesID:  seriesRated.ID,
		Type:      "Episode",
		MediaType: "Video",
		Name:      "Inherited Episode",
	}

	// Standalone trailers & attached local trailer extras
	standaloneTrailerOK := &sqlite.ItemRecord{
		ID:             guid.New(),
		LibraryID:      libAllowed.ID,
		ParentID:       libAllowed.ID,
		Type:           "Trailer",
		MediaType:      "Video",
		Name:           "Approved Standalone Trailer",
		OfficialRating: "G",
		Path:           filepath.Join(tmp, "allowed", "tr1.mp4"),
	}
	standaloneTrailerUnrated := &sqlite.ItemRecord{
		ID:        guid.New(),
		LibraryID: libAllowed.ID,
		ParentID:  libAllowed.ID,
		Type:      "Trailer",
		MediaType: "Video",
		Name:      "Unrated Standalone Trailer",
	}
	extraTrailerOK := &sqlite.ItemRecord{
		ID:             guid.New(),
		LibraryID:      libAllowed.ID,
		ParentID:       movieOK1.ID,
		Type:           "Video",
		Extra:          sqlite.ItemExtra{ExtraType: "trailer"},
		MediaType:      "Video",
		Name:           "Adv1 Official Trailer",
		OfficialRating: "PG",
		Path:           filepath.Join(tmp, "allowed", "adv1-trailer.mp4"),
	}
	extraTrailerUnrated := &sqlite.ItemRecord{
		ID:        guid.New(),
		LibraryID: libAllowed.ID,
		ParentID:  movieOK1.ID,
		Type:      "Video",
		Extra:     sqlite.ItemExtra{ExtraType: "trailer"},
		MediaType: "Video",
		Name:      "Adv1 Unrated Teaser",
	}

	// Songs for InstantMix
	songOK1 := &sqlite.ItemRecord{
		ID:             guid.New(),
		LibraryID:      libAllowed.ID,
		ParentID:       libAllowed.ID,
		Type:           "Audio",
		MediaType:      "Audio",
		Name:           "Song OK 1",
		OfficialRating: "G",
	}
	songOK2 := &sqlite.ItemRecord{
		ID:             guid.New(),
		LibraryID:      libAllowed.ID,
		ParentID:       libAllowed.ID,
		Type:           "Audio",
		MediaType:      "Audio",
		Name:           "Song OK 2",
		OfficialRating: "G",
	}
	songUnrated := &sqlite.ItemRecord{
		ID:        guid.New(),
		LibraryID: libAllowed.ID,
		ParentID:  libAllowed.ID,
		Type:      "Audio",
		MediaType: "Audio",
		Name:      "Song Unrated",
	}

	// AudioBooks & Channels for handleItemsByType and handleChannelItems
	bookOK1 := &sqlite.ItemRecord{
		ID:             guid.New(),
		LibraryID:      libAllowed.ID,
		ParentID:       libAllowed.ID,
		Type:           "AudioBook",
		MediaType:      "Audio",
		Name:           "Book 1",
		OfficialRating: "G",
	}
	bookOK2 := &sqlite.ItemRecord{
		ID:             guid.New(),
		LibraryID:      libAllowed.ID,
		ParentID:       libAllowed.ID,
		Type:           "AudioBook",
		MediaType:      "Audio",
		Name:           "Book 2",
		OfficialRating: "G",
	}
	bookUnrated := &sqlite.ItemRecord{
		ID:        guid.New(),
		LibraryID: libAllowed.ID,
		ParentID:  libAllowed.ID,
		Type:      "AudioBook",
		MediaType: "Audio",
		Name:      "Book Unrated",
	}

	channelOK := &sqlite.ItemRecord{
		ID:             guid.New(),
		LibraryID:      libAllowed.ID,
		ParentID:       libAllowed.ID,
		Type:           "Channel",
		IsFolder:       true,
		Name:           "Kids Channel",
		OfficialRating: "G",
	}
	channelBlocked := &sqlite.ItemRecord{
		ID:             guid.New(),
		LibraryID:      libBlocked.ID,
		ParentID:       libBlocked.ID,
		Type:           "Channel",
		IsFolder:       true,
		Name:           "Blocked Channel",
		OfficialRating: "G",
	}
	chItem1 := &sqlite.ItemRecord{
		ID:             guid.New(),
		LibraryID:      libAllowed.ID,
		ParentID:       channelOK.ID,
		Type:           "Video",
		MediaType:      "Video",
		Name:           "Channel Clip 1",
		OfficialRating: "G",
		Path:           filepath.Join(tmp, "allowed", "clip1.mp4"),
	}
	chItem2 := &sqlite.ItemRecord{
		ID:             guid.New(),
		LibraryID:      libAllowed.ID,
		ParentID:       channelOK.ID,
		Type:           "Video",
		MediaType:      "Video",
		Name:           "Channel Clip 2",
		OfficialRating: "G",
		Path:           filepath.Join(tmp, "allowed", "clip2.mp4"),
	}
	chItemUnrated := &sqlite.ItemRecord{
		ID:        guid.New(),
		LibraryID: libAllowed.ID,
		ParentID:  channelOK.ID,
		Type:      "Video",
		MediaType: "Video",
		Name:      "Channel Clip Unrated",
	}

	all := []*sqlite.ItemRecord{
		movieOK1, movieOK2, movieUnrated, movieWrongTag, movieOtherLib,
		seriesRated, epInherited,
		standaloneTrailerOK, standaloneTrailerUnrated, extraTrailerOK, extraTrailerUnrated,
		songOK1, songOK2, songUnrated,
		bookOK1, bookOK2, bookUnrated,
		channelOK, channelBlocked, chItem1, chItem2, chItemUnrated,
	}
	for _, it := range all {
		if err := repo.UpsertItem(ctx, it); err != nil {
			t.Fatal(err)
		}
		_ = repo.ReplaceItemAncestors(ctx, it.ID, []sqlite.AncestorLink{{AncestorID: it.LibraryID, Depth: 1}})
		_ = repo.UpsertSearchFTS(ctx, it.ID, it.Name, "", "", "", "")
	}

	for _, id := range []string{
		movieOK1.ID, movieOK2.ID, movieUnrated.ID, seriesRated.ID,
		standaloneTrailerOK.ID, extraTrailerOK.ID,
		songOK1.ID, songOK2.ID, bookOK1.ID, bookOK2.ID,
		channelOK.ID, chItem1.ID, chItem2.ID,
	} {
		_ = repo.ReplaceItemTaxonomies(ctx, id, "tag", []string{"SafeTag"})
	}
	_ = repo.ReplaceItemTaxonomies(ctx, movieWrongTag.ID, "tag", []string{"OtherTag"})
	for _, id := range []string{movieOK1.ID, movieOK2.ID, movieUnrated.ID, movieWrongTag.ID} {
		_ = repo.ReplaceItemTaxonomies(ctx, id, "genre", []string{"Adventure"})
	}
	for _, id := range []string{songOK1.ID, songOK2.ID, songUnrated.ID} {
		_ = repo.ReplaceItemTaxonomies(ctx, id, "genre", []string{"Synthpop"})
	}

	maxPG := 5
	kidPol := sqlite.DefaultUserPolicy(false)
	kidPol.EnableAllFolders = false
	kidPol.EnabledFolders = []string{libAllowed.ID}
	kidPol.MaxParentalRating = &maxPG
	kidPol.BlockUnratedItems = []string{"*"}
	kidPol.AllowedTags = []string{"safetag"}

	kid := &sqlite.UserRecord{
		ID:     guid.New(),
		Name:   "kid_v0110",
		Policy: kidPol,
		Config: sqlite.DefaultUserConfig(),
	}
	other := &sqlite.UserRecord{
		ID:     guid.New(),
		Name:   "other_v0110",
		Policy: sqlite.DefaultUserPolicy(false),
		Config: sqlite.DefaultUserConfig(),
	}
	_ = userRepo.CreateUser(ctx, kid)
	_ = userRepo.CreateUser(ctx, other)

	// Mark movieOK1 played for kid so movie recommendations generate categories
	_, _ = repo.MarkPlayed(ctx, kid.ID, movieOK1.ID)

	// Direct fallback checks when userRepo is nil (repo-only Handler)
	hNoUserRepo := New(repo, cfg)
	reqFallback := httptest.NewRequest(http.MethodGet, "/Movies/Recommendations", nil)
	reqFallback = reqFallback.WithContext(middleware.WithAuthContext(ctx, &middleware.AuthContext{User: kid}))
	polFallback := hNoUserRepo.userExtendedPolicyForRequest(reqFallback, "")
	if !polFallback.hasExtraPolicy() {
		t.Fatal("expected userExtendedPolicyForRequest fallback to read ac.User.Policy")
	}
	if !hNoUserRepo.recordHasAnyTag(ctx, movieOK1.ID, "", map[string]bool{"safetag": true}) {
		t.Fatal("expected recordHasAnyTag fallback via repo.GetItemByID")
	}
	if !hNoUserRepo.recordHasAnyTag(ctx, epInherited.ID, seriesRated.ID, map[string]bool{"safetag": true}) {
		t.Fatal("expected recordHasAnyTag fallback via SeriesID")
	}
	if hNoUserRepo.recordHasAnyTag(ctx, "", "", map[string]bool{"safetag": true}) {
		t.Fatal("expected false for empty ID in recordHasAnyTag")
	}
	if hNoUserRepo.isRecordBlockedByExtraPolicy(ctx, epInherited, polFallback) {
		t.Fatal("expected epInherited to inherit Series rating and tag and NOT be blocked")
	}
	if !hNoUserRepo.isRecordBlockedByExtraPolicy(ctx, movieUnrated, polFallback) {
		t.Fatal("expected movieUnrated blocked by extra policy")
	}
	if !hNoUserRepo.isRecordBlockedByExtraPolicy(ctx, movieWrongTag, polFallback) {
		t.Fatal("expected movieWrongTag blocked by AllowedTags")
	}
	filteredDTOs := hNoUserRepo.filterDTOItemsPost(ctx, []dto.BaseItemDto{
		{Id: epInherited.ID, SeriesId: seriesRated.ID, Type: "Episode", MediaType: "Video"},
		{Id: movieUnrated.ID, Type: "Movie", MediaType: "Video"},
		{Id: movieWrongTag.ID, Type: "Movie", MediaType: "Video", OfficialRating: "G", Tags: []string{"OtherTag"}},
		{Id: movieOK1.ID, Type: "Movie", MediaType: "Video", OfficialRating: "PG", Tags: []string{"SafeTag"}},
	}, polFallback)
	if len(filteredDTOs) != 2 {
		t.Fatalf("expected 2 items after filterDTOItemsPost, got %d", len(filteredDTOs))
	}

	// Full HTTP Router tests
	mux := http.NewServeMux()
	h := New(repo, cfg)
	h.SetUserRepo(userRepo)
	h.RegisterRoutes(mux, AuthFuncs{
		UserIDFromCtx: func(c context.Context) string {
			if ac, ok := middleware.AuthFromContext(c); ok && ac != nil {
				return ac.UserID()
			}
			return ""
		},
	})

	kidAC := &middleware.AuthContext{User: kid}
	doGet := func(target string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, target, nil)
		req = req.WithContext(middleware.WithAuthContext(req.Context(), kidAC))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}

	// Cross-user UserId forbidden checks across endpoints
	for _, p := range []string{
		"/Items/Filters?UserId=" + other.ID,
		"/Items/Filters2?UserId=" + other.ID,
		"/Movies/Recommendations?UserId=" + other.ID,
		"/Movies/" + movieOK1.ID + "/Similar?UserId=" + other.ID,
		"/Songs/" + songOK1.ID + "/InstantMix?UserId=" + other.ID,
		"/Trailers?UserId=" + other.ID,
		"/AudioBooks?UserId=" + other.ID,
		"/Channels/" + channelOK.ID + "/Items?UserId=" + other.ID,
		"/Tags?UserId=" + other.ID,
	} {
		if rec := doGet(p); rec.Code != http.StatusForbidden {
			t.Fatalf("expected 403 for cross-user request %s, got %d", p, rec.Code)
		}
	}

	// 1. MovieRecommendations with CategoryLimit/ItemLimit bounds & ExtraPolicy filtering
	rec := doGet("/Movies/Recommendations?CategoryLimit=99&ItemLimit=200")
	if rec.Code != http.StatusOK {
		t.Fatalf("Recommendations status=%d body=%s", rec.Code, rec.Body.String())
	}
	var cats []sqlite.RecommendationCategory
	_ = json.Unmarshal(rec.Body.Bytes(), &cats)
	for _, c := range cats {
		for _, it := range c.Items {
			if it.Id == movieUnrated.ID || it.Id == movieWrongTag.ID || it.Id == movieOtherLib.ID {
				t.Fatalf("Recommendations leaked extra-policy blocked item %s", it.Name)
			}
		}
	}
	rec = doGet("/Movies/Recommendations?CategoryLimit=-1&ItemLimit=-1")
	if rec.Code != http.StatusOK {
		t.Fatalf("Recommendations negative limit status=%d", rec.Code)
	}

	// 2. Similar: forbidden on unrated/wrong-tag seed, 200 with Limit=1 truncation on allowed seed
	if rec := doGet("/Movies/" + movieUnrated.ID + "/Similar"); rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 Similar on unrated seed, got %d", rec.Code)
	}
	if rec := doGet("/Movies/" + movieWrongTag.ID + "/Similar"); rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 Similar on wrong-tag seed, got %d", rec.Code)
	}
	rec = doGet("/Movies/" + movieOK1.ID + "/Similar?Limit=1")
	if rec.Code != http.StatusOK {
		t.Fatalf("Similar status=%d", rec.Code)
	}
	var qRes dto.QueryResult[dto.BaseItemDto]
	_ = json.Unmarshal(rec.Body.Bytes(), &qRes)
	if len(qRes.Items) != 1 || qRes.Items[0].Id != movieOK2.ID {
		t.Fatalf("expected Similar to return only movieOK2, got %+v", qRes)
	}

	// 3. InstantMix: forbidden on unrated song, 200 on songOK1 excluding songUnrated
	if rec := doGet("/Songs/" + songUnrated.ID + "/InstantMix"); rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 InstantMix on unrated seed, got %d", rec.Code)
	}
	rec = doGet("/Songs/" + songOK1.ID + "/InstantMix?Limit=1")
	if rec.Code != http.StatusOK {
		t.Fatalf("InstantMix status=%d", rec.Code)
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &qRes)
	if len(qRes.Items) != 1 {
		t.Fatalf("expected 1 InstantMix item with Limit=1, got %+v", qRes)
	}

	// 4. Trailers: merges standalone + attached local trailer extras, applies SearchTerm, ParentId, ExtraPolicy, StartIndex & Limit
	rec = doGet("/Trailers?ParentId=" + libAllowed.ID + "&SearchTerm=trailer&StartIndex=-1&Limit=600")
	if rec.Code != http.StatusOK {
		t.Fatalf("Trailers status=%d body=%s", rec.Code, rec.Body.String())
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &qRes)
	if qRes.TotalRecordCount != 2 {
		t.Fatalf("expected 2 allowed trailers (standalone + attached extra), got %d: %+v", qRes.TotalRecordCount, qRes.Items)
	}
	rec = doGet("/Trailers?StartIndex=0&Limit=1")
	_ = json.Unmarshal(rec.Body.Bytes(), &qRes)
	if len(qRes.Items) != 1 {
		t.Fatalf("expected 1 trailer with Limit=1, got %d", len(qRes.Items))
	}
	rec = doGet("/Trailers?StartIndex=99&Limit=1")
	_ = json.Unmarshal(rec.Body.Bytes(), &qRes)
	if len(qRes.Items) != 0 {
		t.Fatalf("expected 0 trailers when StartIndex >= total, got %d", len(qRes.Items))
	}

	// 5. handleItemsByType (/AudioBooks) with ExtraPolicy & pagination
	rec = doGet("/AudioBooks?StartIndex=1&Limit=1")
	if rec.Code != http.StatusOK {
		t.Fatalf("AudioBooks status=%d", rec.Code)
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &qRes)
	if qRes.TotalRecordCount != 2 || len(qRes.Items) != 1 {
		t.Fatalf("expected 2 allowed audiobooks (unrated excluded) and 1 paged item, got %+v", qRes)
	}
	rec = doGet("/AudioBooks?StartIndex=50&Limit=1")
	_ = json.Unmarshal(rec.Body.Bytes(), &qRes)
	if len(qRes.Items) != 0 {
		t.Fatalf("expected empty audiobooks when StartIndex > total")
	}

	// 6. handleChannelItems (/Channels/{Id}/Items): 404 not found, 404 wrong type, 403 restricted channel, 200 paged & filtered
	if rec := doGet("/Channels/" + guid.New() + "/Items"); rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown channel, got %d", rec.Code)
	}
	if rec := doGet("/Channels/" + movieOK1.ID + "/Items"); rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 when Id is a Movie instead of Channel, got %d", rec.Code)
	}
	if rec := doGet("/Channels/" + channelBlocked.ID + "/Items"); rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for restricted channel, got %d", rec.Code)
	}
	rec = doGet("/Channels/" + channelOK.ID + "/Items?StartIndex=1&Limit=1")
	if rec.Code != http.StatusOK {
		t.Fatalf("ChannelItems status=%d", rec.Code)
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &qRes)
	if qRes.TotalRecordCount != 2 || len(qRes.Items) != 1 {
		t.Fatalf("expected 2 allowed channel items and 1 paged item, got %+v", qRes)
	}
	rec = doGet("/Channels/" + channelOK.ID + "/Items?StartIndex=20&Limit=1")
	_ = json.Unmarshal(rec.Body.Bytes(), &qRes)
	if len(qRes.Items) != 0 {
		t.Fatalf("expected empty channel items when StartIndex > total")
	}
}
