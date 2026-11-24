package compatmedia

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/ambra-media/ambra/internal/api/dto"
	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/core/imaging"
	"github.com/ambra-media/ambra/internal/core/scanner"
	"github.com/ambra-media/ambra/internal/pkg/eventbus"
	"github.com/ambra-media/ambra/internal/plugins"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

type mockMetaProvider struct{}

func (m *mockMetaProvider) Manifest() plugins.Manifest {
	return plugins.Manifest{
		ID:       "mock-tmdb",
		Name:     "MockTMDb",
		Version:  "1.0.0",
		Enabled:  true,
		Priority: 10,
		Kinds:    []plugins.Kind{plugins.KindMetadata, plugins.KindImage},
	}
}

func (m *mockMetaProvider) Configure(map[string]any) error { return nil }

func (m *mockMetaProvider) FetchMetadata(_ context.Context, q plugins.MediaQuery) (*plugins.MetadataResult, error) {
	return &plugins.MetadataResult{
		Name:         q.Title + " (Matched)",
		Overview:     "Fetched overview",
		Year:         2024,
		PremiereDate: "2024-05-01",
		PosterURL:    "https://example.com/poster.jpg",
		ProviderIDs:  map[string]string{"Tmdb": "12345"},
	}, nil
}

type testEnv struct {
	store   *sqlite.Store
	repo    *sqlite.LibraryRepo
	scanner *scanner.Scanner
	imaging *imaging.Service
	reg     *plugins.Registry
	cfg     *config.Config
	handler *Handler
	router  http.Handler
	tmpDir  string
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	tmpDir := t.TempDir()
	cfg, err := config.Load(filepath.Join(tmpDir, "data"))
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	store, err := sqlite.Open(cfg.DBPath())
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	repo := sqlite.NewLibraryRepo(store)
	reg := plugins.NewRegistry()
	reg.Register(&mockMetaProvider{})
	bus := eventbus.New()
	sc := scanner.New(repo, reg, bus, cfg)
	sc.SetProber(scanner.NewDisabledProber())
	img := imaging.New(repo).WithCacheDir(cfg.ImageCacheDir())

	h := New(repo, sc, img, reg, store, cfg)
	r := h.BuildRouter(AuthFuncs{
		UserIDFromCtx: func(context.Context) string { return "user-1" },
	})
	return &testEnv{
		store:   store,
		repo:    repo,
		scanner: sc,
		imaging: img,
		reg:     reg,
		cfg:     cfg,
		handler: h,
		router:  r,
		tmpDir:  tmpDir,
	}
}

func makePNGBytes(t *testing.T) []byte {
	t.Helper()
	im := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			im.Set(x, y, color.RGBA{R: 200, G: 100, B: 50, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, im); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func doReq(r http.Handler, method, target string, body string) *httptest.ResponseRecorder {
	var reader *strings.Reader
	if body != "" {
		reader = strings.NewReader(body)
	} else {
		reader = strings.NewReader("")
	}
	req := httptest.NewRequest(method, target, reader)
	if method == http.MethodPost || method == http.MethodPut {
		req.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

var routeParamRe = regexp.MustCompile(`\{([^}]+)\}`)

func materializeTestPath(tmpl string) string {
	return routeParamRe.ReplaceAllStringFunc(tmpl, func(tok string) string {
		name := strings.ToLower(strings.TrimSuffix(strings.TrimPrefix(tok, "{"), "}"))
		switch {
		case name == "id" || name == "itemid":
			return "nonexistent-item-id"
		case name == "mediasourceid":
			return "src-0"
		case name == "type":
			return "Primary"
		case name == "index" || name == "segmentid" || name == "startpositionticks":
			return "0"
		case name == "format":
			return "vtt"
		case name == "segmentcontainer":
			return "ts"
		case name == "language":
			return "chi"
		case name == "name":
			return "default"
		case name == "playlistid":
			return "main"
		default:
			return "test"
		}
	})
}

func TestAll152MissingRoutesCovered(t *testing.T) {
	env := newTestEnv(t)

	routes := [][2]string{
		// /Artists (10)
		{"GET", "/Artists"},
		{"GET", "/Artists/AlbumArtists"},
		{"GET", "/Artists/{Name}"},
		{"GET", "/Artists/Prefixes"},
		{"GET", "/Artists/InstantMix"},
		{"GET", "/Artists/{Id}/Similar"},
		{"GET", "/Artists/{Name}/Images/{Type}"},
		{"GET", "/Artists/{Name}/Images/{Type}/{Index}"},
		{"HEAD", "/Artists/{Name}/Images/{Type}"},
		{"HEAD", "/Artists/{Name}/Images/{Type}/{Index}"},
		// /Audio (3)
		{"GET", "/Audio/{Id}/hls1/{PlaylistId}/{SegmentId}.{SegmentContainer}"},
		{"HEAD", "/Audio/{Id}/hls1/{PlaylistId}/{SegmentId}.{SegmentContainer}"},
		{"GET", "/Audio/{Id}/hls/{PlaylistId}/{SegmentId}.{SegmentContainer}"},
		// /AudioBooks (1)
		{"GET", "/AudioBooks/NextUp"},
		// /Channels (1)
		{"GET", "/Channels/Items/Latest"},
		// /Collections (3)
		{"GET", "/Collections/{Id}/Missing"},
		{"GET", "/Collections/{Id}/ProviderItems"},
		{"POST", "/Collections/{Id}/Items/Delete"},
		// /Encoding (11)
		{"GET", "/Encoding/ToneMapOptions"},
		{"GET", "/Encoding/FullToneMapOptions"},
		{"GET", "/Encoding/PublicToneMapOptions"},
		{"POST", "/Encoding/FullToneMapOptions"},
		{"POST", "/Encoding/PublicToneMapOptions"},
		{"GET", "/Encoding/SubtitleOptions"},
		{"POST", "/Encoding/SubtitleOptions"},
		{"GET", "/Encoding/FfmpegOptions"},
		{"POST", "/Encoding/FfmpegOptions"},
		{"GET", "/Encoding/CodecParameters"},
		{"POST", "/Encoding/CodecParameters"},
		// /GameGenres (5)
		{"GET", "/GameGenres/{Name}"},
		{"GET", "/GameGenres/{Name}/Images/{Type}"},
		{"GET", "/GameGenres/{Name}/Images/{Type}/{Index}"},
		{"HEAD", "/GameGenres/{Name}/Images/{Type}"},
		{"HEAD", "/GameGenres/{Name}/Images/{Type}/{Index}"},
		// /Games (1)
		{"GET", "/Games/{Id}/Similar"},
		// /Genres (5)
		{"GET", "/Genres/{Name}"},
		{"GET", "/Genres/{Name}/Images/{Type}"},
		{"GET", "/Genres/{Name}/Images/{Type}/{Index}"},
		{"HEAD", "/Genres/{Name}/Images/{Type}"},
		{"HEAD", "/Genres/{Name}/Images/{Type}/{Index}"},
		// /Images (1)
		{"GET", "/Images/Remote"},
		// /Items (60 + 3 bonus)
		{"GET", "/Items/{Id}/ExternalIdInfos"},
		{"POST", "/Items/RemoteSearch/Movie"},
		{"POST", "/Items/RemoteSearch/Trailer"},
		{"POST", "/Items/RemoteSearch/MusicVideo"},
		{"POST", "/Items/RemoteSearch/Series"},
		{"POST", "/Items/RemoteSearch/Game"},
		{"POST", "/Items/RemoteSearch/BoxSet"},
		{"POST", "/Items/RemoteSearch/MusicArtist"},
		{"POST", "/Items/RemoteSearch/MusicAlbum"},
		{"POST", "/Items/RemoteSearch/Person"},
		{"POST", "/Items/RemoteSearch/Book"},
		{"GET", "/Items/RemoteSearch/Image"},
		{"POST", "/Items/RemoteSearch/Apply/{Id}"},
		{"POST", "/Items/Metadata/Reset"},
		{"POST", "/Items/{Id}/Refresh"},
		{"POST", "/Items/{ItemId}"},
		{"GET", "/Items/{ItemId}/MetadataEditor"},
		{"POST", "/Items/{Id}/Tags/Add"},
		{"POST", "/Items/{Id}/Tags/Delete"},
		{"POST", "/Items/{Id}/MakePublic"},
		{"POST", "/Items/{Id}/MakePrivate"},
		{"POST", "/Items/Access"},
		{"POST", "/Items/Shared/Leave"},
		{"DELETE", "/Items/{Id}/Subtitles/{Index}"},
		{"POST", "/Items/{Id}/Subtitles/{Index}/Delete"},
		{"GET", "/Items/{Id}/RemoteSearch/Subtitles/{Language}"},
		{"POST", "/Items/{Id}/RemoteSearch/Subtitles/{SubtitleId}"},
		{"GET", "/Items/{Id}/{MediaSourceId}/Subtitles/{Index}/Stream.{Format}"},
		{"GET", "/Items/{Id}/{MediaSourceId}/Subtitles/{Index}/{StartPositionTicks}/Stream.{Format}"},
		{"HEAD", "/Items/{Id}/{MediaSourceId}/Subtitles/{Index}/Stream.{Format}"},
		{"HEAD", "/Items/{Id}/{MediaSourceId}/Subtitles/{Index}/{StartPositionTicks}/Stream.{Format}"},
		{"GET", "/Items/{Id}/InstantMix"},
		{"GET", "/Items/{Id}/CriticReviews"},
		{"GET", "/Items/{Id}/ThemeSongs"},
		{"GET", "/Items/{Id}/ThemeVideos"},
		{"GET", "/Items/{Id}/ThemeMedia"},
		{"GET", "/Items/{Id}/LocalTrailers"},
		{"GET", "/Items/{Id}/SpecialFeatures"},
		{"GET", "/Items/{Id}/Intros"},
		{"POST", "/Items/{Id}/Delete"},
		{"DELETE", "/Items/{Id}"},
		{"POST", "/Items/Delete"},
		{"DELETE", "/Items"},
		{"GET", "/Items/{Id}/Ancestors"},
		{"GET", "/Items/{Id}/File"},
		{"GET", "/Items/{Id}/Similar"},
		{"GET", "/Items/{Id}/DeleteInfo"},
		{"GET", "/Items/{Id}/Images"},
		{"GET", "/Items/{Id}/Images/{Type}/{Index}/{Tag}/{Format}/{MaxWidth}/{MaxHeight}/{PercentPlayed}/{UnPlayedCount}"},
		{"HEAD", "/Items/{Id}/Images/{Type}/{Index}/{Tag}/{Format}/{MaxWidth}/{MaxHeight}/{PercentPlayed}/{UnPlayedCount}"},
		{"POST", "/Items/{Id}/Images/{Type}/{Index}/Index"},
		{"POST", "/Items/{Id}/Images/{Type}/{Index}/Url"},
		{"DELETE", "/Items/{Id}/Images/{Type}"},
		{"DELETE", "/Items/{Id}/Images/{Type}/{Index}"},
		{"POST", "/Items/{Id}/Images/{Type}/Delete"},
		{"POST", "/Items/{Id}/Images/{Type}/{Index}/Delete"},
		{"POST", "/Items/{Id}/Images/{Type}"},
		{"POST", "/Items/{Id}/Images/{Type}/{Index}"},
		{"GET", "/Items/{Id}/RemoteImages"},
		{"GET", "/Items/{Id}/RemoteImages/Providers"},
		{"POST", "/Items/{Id}/RemoteImages/Download"},
		{"GET", "/Items/{Id}/thumbnails"},
		{"GET", "/Items/{Id}/ThumbnailSet"},
		// /Libraries (1)
		{"GET", "/Libraries/AvailableOptions"},
		// /Library (15)
		{"GET", "/Library/PhysicalPaths"},
		{"GET", "/Library/SelectableMediaFolders"},
		{"GET", "/Library/MediaFolders"},
		{"POST", "/Library/Series/Added"},
		{"POST", "/Library/Series/Updated"},
		{"POST", "/Library/Movies/Added"},
		{"POST", "/Library/Movies/Updated"},
		{"GET", "/Library/VirtualFolders/Query"},
		{"POST", "/Library/VirtualFolders/Delete"},
		{"POST", "/Library/VirtualFolders/Name"},
		{"POST", "/Library/VirtualFolders/Paths"},
		{"POST", "/Library/VirtualFolders/Paths/Update"},
		{"DELETE", "/Library/VirtualFolders/Paths"},
		{"POST", "/Library/VirtualFolders/Paths/Delete"},
		{"POST", "/Library/VirtualFolders/LibraryOptions"},
		// /MusicGenres (7)
		{"GET", "/MusicGenres/{Name}"},
		{"GET", "/MusicGenres/{Name}/InstantMix"},
		{"GET", "/MusicGenres/InstantMix"},
		{"GET", "/MusicGenres/{Name}/Images/{Type}"},
		{"GET", "/MusicGenres/{Name}/Images/{Type}/{Index}"},
		{"HEAD", "/MusicGenres/{Name}/Images/{Type}"},
		{"HEAD", "/MusicGenres/{Name}/Images/{Type}/{Index}"},
		// /Persons (6)
		{"GET", "/Persons/{Id}/Credits"},
		{"GET", "/Persons/{Name}"},
		{"GET", "/Persons/{Name}/Images/{Type}"},
		{"GET", "/Persons/{Name}/Images/{Type}/{Index}"},
		{"HEAD", "/Persons/{Name}/Images/{Type}"},
		{"HEAD", "/Persons/{Name}/Images/{Type}/{Index}"},
		// /Playlists (2)
		{"GET", "/Playlists/{Id}/AddToPlaylistInfo"},
		{"POST", "/Playlists/{Id}/Items/Delete"},
		// /Providers (1)
		{"GET", "/Providers/Subtitles/Subtitles/{Id}"},
		// /Shows (3)
		{"GET", "/Shows/Missing"},
		{"GET", "/Shows/Upcoming"},
		{"GET", "/Shows/{Id}/Similar"},
		// /Studios (5)
		{"GET", "/Studios/{Name}"},
		{"GET", "/Studios/{Name}/Images/{Type}"},
		{"GET", "/Studios/{Name}/Images/{Type}/{Index}"},
		{"HEAD", "/Studios/{Name}/Images/{Type}"},
		{"HEAD", "/Studios/{Name}/Images/{Type}/{Index}"},
		// /Trailers (1)
		{"GET", "/Trailers/{Id}/Similar"},
		// /Videos (10)
		{"DELETE", "/Videos/{Id}/AlternateSources"},
		{"POST", "/Videos/{Id}/AlternateSources/Delete"},
		{"POST", "/Videos/MergeVersions"},
		{"DELETE", "/Videos/{Id}/Subtitles/{Index}"},
		{"POST", "/Videos/{Id}/Subtitles/{Index}/Delete"},
		{"GET", "/Videos/{Id}/{MediaSourceId}/Subtitles/{Index}/{StartPositionTicks}/Stream.{Format}"},
		{"HEAD", "/Videos/{Id}/{MediaSourceId}/Subtitles/{Index}/{StartPositionTicks}/Stream.{Format}"},
		{"GET", "/Videos/{Id}/{MediaSourceId}/Attachments/{Index}/Stream"},
		{"POST", "/Videos/ActiveEncodings/Delete"},
		{"GET", "/Videos/{Id}/hls/{PlaylistId}/{SegmentId}.{SegmentContainer}"},
	}

	for _, rt := range routes {
		method, tmpl := rt[0], rt[1]
		path := materializeTestPath(tmpl)
		body := ""
		if method == http.MethodPost {
			body = `{}`
		}
		w := doReq(env.router, method, path, body)
		ct := w.Header().Get("Content-Type")
		respBody := strings.TrimSpace(w.Body.String())
		isUnregistered := w.Code == http.StatusMethodNotAllowed ||
			(w.Code == http.StatusNotFound && !strings.Contains(ct, "application/json") && (respBody == "404 page not found" || respBody == ""))
		if isUnregistered {
			t.Errorf("route %s %s (%s) appeared unregistered: status=%d ct=%q body=%q", method, tmpl, path, w.Code, ct, respBody)
		}
	}
}

func TestItemMetadataTagsRemoteSearchAndRelations(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()

	lib, err := env.repo.CreateLibrary(ctx, "Movies", "movies", []string{env.tmpDir})
	if err != nil {
		t.Fatal(err)
	}
	mediaFile := filepath.Join(env.tmpDir, "movie.mkv")
	if err := os.WriteFile(mediaFile, []byte("fake-video-bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	year := 2020
	movie := &sqlite.ItemRecord{
		ID:             "movie001",
		LibraryID:      lib.ID,
		ParentID:       lib.ID,
		Type:           "Movie",
		MediaType:      "Video",
		Name:           "Original Movie",
		ProductionYear: &year,
		Path:           mediaFile,
	}
	if err := env.repo.UpsertItem(ctx, movie); err != nil {
		t.Fatal(err)
	}
	series := &sqlite.ItemRecord{
		ID:        "series001",
		LibraryID: lib.ID,
		ParentID:  lib.ID,
		Type:      "Series",
		Name:      "Sample Series",
	}
	if err := env.repo.UpsertItem(ctx, series); err != nil {
		t.Fatal(err)
	}
	if err := env.repo.ReplaceItemAncestors(ctx, movie.ID, []sqlite.AncestorLink{
		{AncestorID: series.ID, Depth: 1},
		{AncestorID: lib.ID, Depth: 2},
	}); err != nil {
		t.Fatal(err)
	}

	// POST /Items/{ItemId} updates metadata
	w := doReq(env.router, http.MethodPost, "/Items/movie001", `{
		"Name":"Updated Movie",
		"OriginalTitle":"Orig Title",
		"Overview":"Great film",
		"Taglines":["One line"],
		"OfficialRating":"PG-13",
		"CommunityRating":8.5,
		"CriticRating":90,
		"ProductionYear":2024,
		"IndexNumber":1,
		"ParentIndexNumber":1,
		"PremiereDate":"2024-06-01",
		"Genres":["Sci-Fi"],
		"Tags":["4K"],
		"Studios":[{"Name":"StudioA","Id":"st1"}],
		"ProviderIds":{"Tmdb":"999"}
	}`)
	if w.Code != http.StatusNoContent {
		t.Fatalf("update item: expected 204, got %d (%s)", w.Code, w.Body.String())
	}
	updated, err := env.repo.GetItemRecordByID(ctx, "movie001")
	if err != nil {
		t.Fatal(err)
	}
	if updated.Name != "Updated Movie" || updated.Extra.Overview != "Great film" || *updated.ProductionYear != 2024 {
		t.Fatalf("unexpected updated item: %+v", updated)
	}

	// GET /Items/{ItemId}/MetadataEditor
	w = doReq(env.router, http.MethodGet, "/Items/movie001/MetadataEditor", "")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "TheMovieDb") {
		t.Fatalf("metadata editor failed: %d %s", w.Code, w.Body.String())
	}

	// GET /Items/{Id}/ExternalIdInfos (Movie & Series)
	w = doReq(env.router, http.MethodGet, "/Items/series001/ExternalIdInfos", "")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "TheTVDB") {
		t.Fatalf("series external ids failed: %d %s", w.Code, w.Body.String())
	}

	// Tags Add & Delete
	w = doReq(env.router, http.MethodPost, "/Items/movie001/Tags/Add", `{"Tags":[{"Name":"HDR"},{"Name":"Dolby"}]}`)
	if w.Code != http.StatusNoContent {
		t.Fatalf("tags add: %d", w.Code)
	}
	w = doReq(env.router, http.MethodPost, "/Items/movie001/Tags/Delete", `{"Tags":[{"Name":"HDR"}]}`)
	if w.Code != http.StatusNoContent {
		t.Fatalf("tags delete: %d", w.Code)
	}

	// Visibility & Access
	if w := doReq(env.router, http.MethodPost, "/Items/movie001/MakePublic", `{}`); w.Code != http.StatusNoContent {
		t.Fatalf("make public: %d", w.Code)
	}
	if w := doReq(env.router, http.MethodPost, "/Items/movie001/MakePrivate", `{}`); w.Code != http.StatusNoContent {
		t.Fatalf("make private: %d", w.Code)
	}
	if w := doReq(env.router, http.MethodPost, "/Items/Access", `{"ItemIds":["movie001"]}`); w.Code != http.StatusNoContent {
		t.Fatalf("items access: %d", w.Code)
	}

	// RemoteSearch & Apply
	w = doReq(env.router, http.MethodPost, "/Items/RemoteSearch/Movie", `{"SearchInfo":{"Name":"Inception","Year":2010}}`)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Inception (Matched)") {
		t.Fatalf("remote search failed: %d %s", w.Code, w.Body.String())
	}
	w = doReq(env.router, http.MethodPost, "/Items/RemoteSearch/Apply/movie001", `{"Name":"Inception","Overview":"Dreams","ProductionYear":2010,"ProviderIds":{"Tmdb":"27205"}}`)
	if w.Code != http.StatusNoContent {
		t.Fatalf("remote search apply: %d", w.Code)
	}

	// Ancestors, File, DeleteInfo (verify before async Refresh overwrites seeded ancestors)
	w = doReq(env.router, http.MethodGet, "/Items/movie001/Ancestors", "")
	if w.Code != http.StatusOK {
		t.Fatalf("ancestors: %d", w.Code)
	}
	var ancs []dto.BaseItemDto
	_ = json.NewDecoder(w.Body).Decode(&ancs)
	if len(ancs) != 2 {
		t.Fatalf("expected 2 ancestors, got %d", len(ancs))
	}

	w = doReq(env.router, http.MethodGet, "/Items/movie001/File", "")
	if w.Code != http.StatusOK || w.Body.String() != "fake-video-bytes" {
		t.Fatalf("item file failed: %d %q", w.Code, w.Body.String())
	}

	w = doReq(env.router, http.MethodGet, "/Items/movie001/DeleteInfo", "")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "movie.mkv") {
		t.Fatalf("delete info failed: %d %s", w.Code, w.Body.String())
	}

	// Metadata reset & Refresh
	if w := doReq(env.router, http.MethodPost, "/Items/Metadata/Reset?ItemIds=movie001", `{}`); w.Code != http.StatusNoContent {
		t.Fatalf("metadata reset: %d", w.Code)
	}
	if w := doReq(env.router, http.MethodPost, "/Items/movie001/Refresh", `{}`); w.Code != http.StatusNoContent {
		t.Fatalf("item refresh: %d", w.Code)
	}
	env.scanner.WaitIdle(ctx)

	// Delete single & batch
	if w := doReq(env.router, http.MethodDelete, "/Items/movie001", ""); w.Code != http.StatusNoContent {
		t.Fatalf("delete item: %d", w.Code)
	}
	if w := doReq(env.router, http.MethodPost, "/Items/Delete", `{"Ids":["series001"]}`); w.Code != http.StatusNoContent {
		t.Fatalf("delete items batch: %d", w.Code)
	}
}

func TestImagesUploadServeAndTaxonomyImages(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()

	lib, _ := env.repo.CreateLibrary(ctx, "Movies", "movies", []string{env.tmpDir})
	item := &sqlite.ItemRecord{
		ID:        "imgitem1",
		LibraryID: lib.ID,
		Type:      "Movie",
		Name:      "Image Movie",
	}
	if err := env.repo.UpsertItem(ctx, item); err != nil {
		t.Fatal(err)
	}
	pngBytes := makePNGBytes(t)

	// Upload base64 Primary image
	b64 := base64.StdEncoding.EncodeToString(pngBytes)
	req := httptest.NewRequest(http.MethodPost, "/Items/imgitem1/Images/Primary", strings.NewReader(b64))
	req.Header.Set("Content-Type", "image/png")
	w := httptest.NewRecorder()
	env.router.ServeHTTP(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("upload base64 image: %d %s", w.Code, w.Body.String())
	}

	// Upload raw binary Backdrop image at index 0
	req = httptest.NewRequest(http.MethodPost, "/Items/imgitem1/Images/Backdrop/0", bytes.NewReader(pngBytes))
	req.Header.Set("Content-Type", "image/png")
	w = httptest.NewRecorder()
	env.router.ServeHTTP(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("upload raw image: %d %s", w.Code, w.Body.String())
	}

	// List images
	w = doReq(env.router, http.MethodGet, "/Items/imgitem1/Images", "")
	if w.Code != http.StatusOK {
		t.Fatalf("list images: %d", w.Code)
	}
	var infos []imageInfoDTO
	_ = json.NewDecoder(w.Body).Decode(&infos)
	if len(infos) != 2 {
		t.Fatalf("expected 2 images, got %d", len(infos))
	}

	// Serve full image URL (GET & HEAD)
	w = doReq(env.router, http.MethodGet, "/Items/imgitem1/Images/Primary/0/tag1/png/100/100/0/0", "")
	if w.Code != http.StatusOK || !strings.HasPrefix(w.Header().Get("Content-Type"), "image/") {
		t.Fatalf("serve full image GET: %d ct=%s", w.Code, w.Header().Get("Content-Type"))
	}
	w = doReq(env.router, http.MethodHead, "/Items/imgitem1/Images/Primary/0/tag1/png/100/100/0/0", "")
	if w.Code != http.StatusOK {
		t.Fatalf("serve full image HEAD: %d", w.Code)
	}

	// Update index & URL
	if w := doReq(env.router, http.MethodPost, "/Items/imgitem1/Images/Backdrop/0/Index?newIndex=1", `{}`); w.Code != http.StatusNoContent {
		t.Fatalf("update image index: %d", w.Code)
	}
	if w := doReq(env.router, http.MethodPost, "/Items/imgitem1/Images/Primary/0/Url", `{"Url":"https://example.com/a.jpg"}`); w.Code != http.StatusNoContent {
		t.Fatalf("set image url: %d", w.Code)
	}

	// Remote images & providers & proxy
	w = doReq(env.router, http.MethodGet, "/Items/imgitem1/RemoteImages", "")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "MockTMDb") {
		t.Fatalf("remote images: %d %s", w.Code, w.Body.String())
	}
	w = doReq(env.router, http.MethodGet, "/Items/imgitem1/RemoteImages/Providers", "")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "MockTMDb") {
		t.Fatalf("remote image providers: %d %s", w.Code, w.Body.String())
	}
	w = doReq(env.router, http.MethodGet, "/Images/Remote?imageUrl=https://example.com/a.jpg", "")
	if w.Code != http.StatusNotFound {
		t.Fatalf("remote image proxy expected 404, got %d", w.Code)
	}

	// Delete image
	if w := doReq(env.router, http.MethodDelete, "/Items/imgitem1/Images/Primary", ""); w.Code != http.StatusNoContent {
		t.Fatalf("delete image: %d", w.Code)
	}

	// Artist entity image (existing item with image)
	artist := &sqlite.ItemRecord{
		ID:        "artist001",
		LibraryID: lib.ID,
		Type:      "MusicArtist",
		Name:      "Beatles",
	}
	_ = env.repo.UpsertItem(ctx, artist)
	imgPath := filepath.Join(env.tmpDir, "artist.png")
	_ = os.WriteFile(imgPath, pngBytes, 0o644)
	_ = env.repo.ReplaceItemImages(ctx, artist.ID, []sqlite.ImageRecord{
		{ItemID: artist.ID, ImageType: "Primary", ImageIndex: 0, Path: imgPath, Tag: "atag1"},
	})
	w = doReq(env.router, http.MethodGet, "/Artists/Beatles/Images/Primary", "")
	if w.Code != http.StatusOK {
		t.Fatalf("artist image GET: %d %s", w.Code, w.Body.String())
	}
	w = doReq(env.router, http.MethodHead, "/Artists/Beatles/Images/Primary/0", "")
	if w.Code != http.StatusOK {
		t.Fatalf("artist image HEAD: %d", w.Code)
	}
}

func TestEncodingLibraryTaxonomiesSubtitlesAndContainers(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()

	// --- Encoding options persistence ---
	for _, ep := range []struct {
		path string
		body string
		key  string
	}{
		{"/Encoding/FullToneMapOptions", `{"EnableToneMapping":false,"ToneMappingAlgorithm":"reinhard"}`, "reinhard"},
		{"/Encoding/PublicToneMapOptions", `{"EnableToneMapping":true,"ToneMappingAlgorithm":"mobius"}`, "mobius"},
		{"/Encoding/SubtitleOptions", `{"DownloadMovieSubtitles":true}`, "DownloadMovieSubtitles"},
		{"/Encoding/FfmpegOptions", `{"H264Crf":20}`, "20"},
		{"/Encoding/CodecParameters?CodecId=h264", `{"Profile":"high"}`, "high"},
	} {
		if w := doReq(env.router, http.MethodPost, ep.path, ep.body); w.Code != http.StatusNoContent {
			t.Fatalf("POST %s: %d", ep.path, w.Code)
		}
		w := doReq(env.router, http.MethodGet, ep.path, "")
		if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), ep.key) {
			t.Fatalf("GET %s: %d %s", ep.path, w.Code, w.Body.String())
		}
	}
	if w := doReq(env.router, http.MethodGet, "/Encoding/ToneMapOptions", ""); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "reinhard") {
		t.Fatalf("GET ToneMapOptions: %d %s", w.Code, w.Body.String())
	}

	// --- Library & VirtualFolders ---
	lib, err := env.repo.CreateLibrary(ctx, "Films", "movies", []string{"/media/films"})
	if err != nil {
		t.Fatal(err)
	}
	if w := doReq(env.router, http.MethodGet, "/Libraries/AvailableOptions", ""); w.Code != http.StatusOK {
		t.Fatalf("available options: %d", w.Code)
	}
	if w := doReq(env.router, http.MethodGet, "/Library/PhysicalPaths", ""); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "/media/films") {
		t.Fatalf("physical paths: %d %s", w.Code, w.Body.String())
	}
	if w := doReq(env.router, http.MethodGet, "/Library/SelectableMediaFolders", ""); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Films") {
		t.Fatalf("selectable folders: %d", w.Code)
	}
	if w := doReq(env.router, http.MethodGet, "/Library/MediaFolders", ""); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Films") {
		t.Fatalf("media folders: %d", w.Code)
	}
	if w := doReq(env.router, http.MethodGet, "/Library/VirtualFolders/Query?StartIndex=0&Limit=10", ""); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Films") {
		t.Fatalf("vf query: %d", w.Code)
	}
	if w := doReq(env.router, http.MethodPost, "/Library/VirtualFolders/Paths", `{"Name":"Films","Path":"/media/films2"}`); w.Code != http.StatusNoContent {
		t.Fatalf("vf add path: %d %s", w.Code, w.Body.String())
	}
	if w := doReq(env.router, http.MethodPost, "/Library/VirtualFolders/Paths/Update", `{"Name":"Films","PathInfo":{"Path":"/media/films3"}}`); w.Code != http.StatusNoContent {
		t.Fatalf("vf update path: %d", w.Code)
	}
	if w := doReq(env.router, http.MethodDelete, "/Library/VirtualFolders/Paths?name=Films&path=/media/films2", ""); w.Code != http.StatusNoContent {
		t.Fatalf("vf delete path: %d %s", w.Code, w.Body.String())
	}
	if w := doReq(env.router, http.MethodPost, "/Library/VirtualFolders/LibraryOptions", `{"Id":"`+lib.ID+`","LibraryOptions":{"EnablePhotos":true}}`); w.Code != http.StatusNoContent {
		t.Fatalf("vf options: %d", w.Code)
	}
	if w := doReq(env.router, http.MethodPost, "/Library/VirtualFolders/Name", `{"Id":"`+lib.ID+`","NewName":"Cinema"}`); w.Code != http.StatusNoContent {
		t.Fatalf("vf rename: %d", w.Code)
	}
	renamed, _ := env.repo.GetLibrary(ctx, lib.ID)
	if renamed.Name != "Cinema" {
		t.Fatalf("expected renamed library Cinema, got %s", renamed.Name)
	}

	// --- Taxonomies, Artists, Persons, Collections, Playlists, Subtitles ---
	movie := &sqlite.ItemRecord{ID: "m1", LibraryID: lib.ID, Type: "Movie", MediaType: "Video", Name: "Matrix"}
	_ = env.repo.UpsertItem(ctx, movie)
	_ = env.repo.ReplaceItemTaxonomies(ctx, "m1", "genre", []string{"Action"})
	_ = env.repo.ReplaceItemTaxonomies(ctx, "m1", "studio", []string{"Warner"})
	_ = env.repo.ReplaceItemPeople(ctx, "m1", []sqlite.PersonInput{{Name: "Keanu Reeves", Type: "Actor", Role: "Neo"}})

	artist := &sqlite.ItemRecord{ID: "a1", LibraryID: lib.ID, Type: "MusicArtist", Name: "Daft Punk"}
	_ = env.repo.UpsertItem(ctx, artist)

	if w := doReq(env.router, http.MethodGet, "/Artists", ""); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Daft Punk") {
		t.Fatalf("list artists: %d %s", w.Code, w.Body.String())
	}
	if w := doReq(env.router, http.MethodGet, "/Artists/Prefixes", ""); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"D"`) {
		t.Fatalf("artist prefixes: %d %s", w.Code, w.Body.String())
	}
	if w := doReq(env.router, http.MethodGet, "/Artists/Daft%20Punk", ""); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Daft Punk") {
		t.Fatalf("artist by name: %d %s", w.Code, w.Body.String())
	}
	if w := doReq(env.router, http.MethodGet, "/Artists/InstantMix?Id=a1", ""); w.Code != http.StatusOK {
		t.Fatalf("artists instant mix: %d", w.Code)
	}
	if w := doReq(env.router, http.MethodGet, "/Genres/Action", ""); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Action") {
		t.Fatalf("genre by name: %d %s", w.Code, w.Body.String())
	}
	if w := doReq(env.router, http.MethodGet, "/Studios/Warner", ""); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Warner") {
		t.Fatalf("studio by name: %d %s", w.Code, w.Body.String())
	}
	w := doReq(env.router, http.MethodGet, "/Persons/Keanu%20Reeves", "")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Keanu Reeves") {
		t.Fatalf("person by name: %d %s", w.Code, w.Body.String())
	}
	var personDTO dto.BaseItemDto
	_ = json.NewDecoder(w.Body).Decode(&personDTO)
	if w := doReq(env.router, http.MethodGet, "/Persons/"+personDTO.Id+"/Credits", ""); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Matrix") {
		t.Fatalf("person credits: %d %s", w.Code, w.Body.String())
	}

	// Collections & Playlists
	col, _ := env.repo.CreateCollection(ctx, "SciFi Box", "", []string{"m1"})
	if w := doReq(env.router, http.MethodGet, "/Collections/"+col.ID+"/ProviderItems", ""); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Matrix") {
		t.Fatalf("collection provider items: %d %s", w.Code, w.Body.String())
	}
	if w := doReq(env.router, http.MethodPost, "/Collections/"+col.ID+"/Items/Delete?Ids=m1", `{}`); w.Code != http.StatusNoContent {
		t.Fatalf("collection items delete: %d", w.Code)
	}
	pl, _ := env.repo.CreatePlaylist(ctx, "My List", "Video", "user-1", []string{"m1"})
	if w := doReq(env.router, http.MethodGet, "/Playlists/"+pl.ID+"/AddToPlaylistInfo?Ids=m1", ""); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"ContainsDuplicates":true`) {
		t.Fatalf("playlist info: %d %s", w.Code, w.Body.String())
	}
	if w := doReq(env.router, http.MethodPost, "/Playlists/"+pl.ID+"/Items/Delete", `{"Ids":["m1"]}`); w.Code != http.StatusNoContent {
		t.Fatalf("playlist items delete: %d", w.Code)
	}

	// Subtitles stream serving & deletion
	subPath := filepath.Join(env.tmpDir, "sub.vtt")
	_ = os.WriteFile(subPath, []byte("WEBVTT\n\n00:00:01.000 --> 00:00:02.000\nHello\n"), 0o644)
	_ = env.repo.ReplaceMediaSourcesAndStreams(ctx, "m1", []sqlite.MediaSourceRecord{
		{
			ID: "ms1", ItemID: "m1", Name: "main", Path: "/tmp/m1.mkv",
			Streams: []sqlite.MediaStreamRecord{
				{MediaSourceID: "ms1", ItemID: "m1", StreamIndex: 2, StreamType: "Subtitle", Codec: "vtt", IsExternal: true, Path: subPath},
			},
		},
	})
	w = doReq(env.router, http.MethodGet, "/Items/m1/ms1/Subtitles/2/Stream.vtt", "")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Hello") {
		t.Fatalf("subtitle GET: %d %s", w.Code, w.Body.String())
	}
	w = doReq(env.router, http.MethodHead, "/Videos/m1/ms1/Subtitles/2/0/Stream.srt", "")
	if w.Code != http.StatusOK {
		t.Fatalf("subtitle HEAD: %d", w.Code)
	}
	if w := doReq(env.router, http.MethodDelete, "/Items/m1/Subtitles/2", ""); w.Code != http.StatusNoContent {
		t.Fatalf("subtitle DELETE: %d", w.Code)
	}

	// Delete virtual folder by name
	if w := doReq(env.router, http.MethodPost, "/Library/VirtualFolders/Delete?name=Cinema", `{}`); w.Code != http.StatusNoContent {
		t.Fatalf("vf delete by name: %d", w.Code)
	}
}

func TestAuthEnforcementAndHelpers(t *testing.T) {
	env := newTestEnv(t)
	rejectAuth := func(http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
		})
	}
	rejectAdmin := func(http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusForbidden)
		})
	}
	secured := env.handler.BuildRouter(AuthFuncs{
		RequireAuth:  rejectAuth,
		RequireAdmin: rejectAdmin,
	})

	// Auth-required route returns 401
	if w := doReq(secured, http.MethodGet, "/Artists", ""); w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for /Artists, got %d", w.Code)
	}
	// Admin-required route returns 403
	if w := doReq(secured, http.MethodGet, "/Encoding/FfmpegOptions", ""); w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for /Encoding/FfmpegOptions, got %d", w.Code)
	}
	// Public image route bypasses auth and returns JSON 404
	if w := doReq(secured, http.MethodGet, "/Genres/Rock/Images/Primary", ""); w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for public genre image, got %d", w.Code)
	}

	// Helper edge cases
	if subtitleContentType("ass") != "text/x-ssa; charset=utf-8" || subtitleContentType("txt") != "text/plain; charset=utf-8" {
		t.Fatalf("unexpected subtitle content type")
	}
	if imageExtForData([]byte("GIF89a"), "") != ".gif" || imageExtForData(nil, "image/webp") != ".webp" || imageExtForData(nil, "") != ".jpg" {
		t.Fatalf("unexpected image ext")
	}
	if _, err := parseEmbyDate("not-a-date"); err == nil {
		t.Fatalf("expected parseEmbyDate error")
	}
}

func TestEdgeCasesForHighCoverage(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()

	lib1, _ := env.repo.CreateLibrary(ctx, "LibOne", "movies", []string{"/p1"})
	lib2, _ := env.repo.CreateLibrary(ctx, "LibTwo", "music", []string{"/p2"})

	// VirtualFolders pagination & edge cases
	if w := doReq(env.router, http.MethodGet, "/Library/VirtualFolders/Query?StartIndex=1&Limit=1", ""); w.Code != http.StatusOK {
		t.Fatalf("vf query slice: %d", w.Code)
	}
	if w := doReq(env.router, http.MethodGet, "/Library/VirtualFolders/Query?StartIndex=99", ""); w.Code != http.StatusOK {
		t.Fatalf("vf query beyond end: %d", w.Code)
	}
	if w := doReq(env.router, http.MethodPost, "/Library/VirtualFolders/Name", `{"Name":"LibOne","NewName":"LibRenamed"}`); w.Code != http.StatusNoContent {
		t.Fatalf("vf rename by name: %d", w.Code)
	}
	if w := doReq(env.router, http.MethodPost, "/Library/VirtualFolders/Paths", `{"Id":"`+lib2.ID+`","Path":"/p3"}`); w.Code != http.StatusNoContent {
		t.Fatalf("vf add path by id: %d", w.Code)
	}
	if w := doReq(env.router, http.MethodPost, "/Library/VirtualFolders/Paths", `{"Name":"NoSuchLib","Path":"/p3"}`); w.Code != http.StatusNotFound {
		t.Fatalf("vf add path missing lib: %d", w.Code)
	}
	if w := doReq(env.router, http.MethodPost, "/Library/VirtualFolders/Paths/Delete", `{"Name":"NoSuchLib","Path":"/p3"}`); w.Code != http.StatusNotFound {
		t.Fatalf("vf del path missing lib: %d", w.Code)
	}
	if w := doReq(env.router, http.MethodPost, "/Library/VirtualFolders/Delete", `{"Id":"`+lib1.ID+`"}`); w.Code != http.StatusNoContent {
		t.Fatalf("vf delete by id: %d", w.Code)
	}

	// Encoding invalid JSON body
	if w := doReq(env.router, http.MethodPost, "/Encoding/SubtitleOptions", `{invalid-json`); w.Code != http.StatusNoContent {
		t.Fatalf("encoding invalid json: %d", w.Code)
	}

	// Artist numeric prefix & UserId query resolution
	numArtist := &sqlite.ItemRecord{ID: "numart1", LibraryID: lib2.ID, Type: "MusicArtist", Name: "50 Cent"}
	_ = env.repo.UpsertItem(ctx, numArtist)
	if w := doReq(env.router, http.MethodGet, "/Artists/Prefixes?UserId=user-9", ""); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"#"`) {
		t.Fatalf("numeric artist prefix: %d %s", w.Code, w.Body.String())
	}

	// Missing file on disk, existing item HLS segments, subtitle bad index & bad path
	missingFileItem := &sqlite.ItemRecord{ID: "missfile1", LibraryID: lib2.ID, Type: "Movie", Name: "MissingFile", Path: filepath.Join(env.tmpDir, "does-not-exist.mkv")}
	_ = env.repo.UpsertItem(ctx, missingFileItem)
	if w := doReq(env.router, http.MethodGet, "/Items/missfile1/File", ""); w.Code != http.StatusNotFound {
		t.Fatalf("missing file on disk expected 404, got %d", w.Code)
	}
	if w := doReq(env.router, http.MethodGet, "/Videos/missfile1/hls/main/0.ts", ""); w.Code != http.StatusNotFound {
		t.Fatalf("video hls existing item expected 404, got %d", w.Code)
	}
	if w := doReq(env.router, http.MethodGet, "/Audio/missfile1/hls/main/0.ts", ""); w.Code != http.StatusNotFound {
		t.Fatalf("audio hls existing item expected 404, got %d", w.Code)
	}
	if w := doReq(env.router, http.MethodDelete, "/Items/missfile1/Subtitles/notanint", ""); w.Code != http.StatusBadRequest {
		t.Fatalf("subtitle delete bad index expected 400, got %d", w.Code)
	}
	if w := doReq(env.router, http.MethodGet, "/Items/missfile1/ms1/Subtitles/notanint/Stream.vtt", ""); w.Code != http.StatusBadRequest {
		t.Fatalf("subtitle stream bad index expected 400, got %d", w.Code)
	}
	if w := doReq(env.router, http.MethodGet, "/Items/missfile1/ms1/Subtitles/0/invalid.vtt", ""); w.Code != http.StatusNotFound {
		t.Fatalf("subtitle stream bad file expected 404, got %d", w.Code)
	}

	// Image upload empty body & raw non-base64 & JPEG magic
	req := httptest.NewRequest(http.MethodPost, "/Items/missfile1/Images/Primary", strings.NewReader(""))
	w := httptest.NewRecorder()
	env.router.ServeHTTP(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("empty image upload: %d", w.Code)
	}
	req = httptest.NewRequest(http.MethodPost, "/Items/missfile1/Images/Logo", strings.NewReader("not-base64!!!"))
	req.Header.Set("Content-Type", "image/jpeg")
	w = httptest.NewRecorder()
	env.router.ServeHTTP(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("raw image upload: %d", w.Code)
	}
	if got := decodeImageBody([]byte("raw-text!"), "text/plain"); string(got) != "raw-text!" {
		t.Fatalf("decodeImageBody fallback failed: %q", string(got))
	}

	// Entity image for person & genre present in DB (no image record -> 404)
	_ = env.repo.ReplaceItemTaxonomies(ctx, "missfile1", "genre", []string{"Drama"})
	_ = env.repo.ReplaceItemPeople(ctx, "missfile1", []sqlite.PersonInput{{Name: "Tom Hanks", Type: "Actor"}})
	if w := doReq(env.router, http.MethodGet, "/Genres/Drama/Images/Primary", ""); w.Code != http.StatusNotFound {
		t.Fatalf("genre image 404: %d", w.Code)
	}
	if w := doReq(env.router, http.MethodGet, "/Persons/Tom%20Hanks/Images/Primary", ""); w.Code != http.StatusNotFound {
		t.Fatalf("person image 404: %d", w.Code)
	}

	// Playlist EntryIds body delete
	pl, _ := env.repo.CreatePlaylist(ctx, "PL2", "Video", "user-1", []string{"missfile1"})
	if w := doReq(env.router, http.MethodPost, "/Playlists/"+pl.ID+"/Items/Delete", `{"EntryIds":["missfile1"]}`); w.Code != http.StatusNoContent {
		t.Fatalf("playlist delete entryIds: %d", w.Code)
	}

	// Nil dependency safety checks (nil cfg, nil store, nil imaging)
	nilH := New(env.repo, nil, nil, nil, nil, nil)
	if nilH.serverID() != "" {
		t.Fatalf("expected empty serverID")
	}
	if ok, err := nilH.getConfigJSON(ctx, "k", &map[string]any{}); ok || err != nil {
		t.Fatalf("expected nil store getConfigJSON false,nil")
	}
	if err := nilH.setConfigJSON(ctx, "k", map[string]any{}); err != nil {
		t.Fatalf("expected nil store setConfigJSON nil")
	}
	nilRouter := nilH.BuildRouter(AuthFuncs{})
	if w := doReq(nilRouter, http.MethodGet, "/Items/missfile1/Images/Primary/0/t/jpg/100/100/0/0", ""); w.Code != http.StatusNotFound {
		t.Fatalf("nil imaging service expected 404, got %d", w.Code)
	}
}

