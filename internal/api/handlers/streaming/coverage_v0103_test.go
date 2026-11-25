package streaming

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ambra-media/ambra/internal/api/middleware"
	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/core/imaging"
	"github.com/ambra-media/ambra/internal/core/playback"
	"github.com/ambra-media/ambra/internal/core/transcode"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/plugins"
	"github.com/ambra-media/ambra/internal/plugins/strm"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func TestStreamingAuthAndFailClosedCoverage(t *testing.T) {
	tmp := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmp, "auth_cov.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	ctx := context.Background()
	repo := sqlite.NewLibraryRepo(store)
	userRepo := sqlite.NewUserRepo(store)
	cfg := config.Default(tmp)

	lib, _ := repo.CreateLibrary(ctx, "Movies", "movies", []string{tmp})
	movie := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: lib.ID, ParentID: lib.ID,
		Type: "Movie", MediaType: "Video", Name: "M", SortName: "m",
		Path: filepath.Join(tmp, "m.mp4"),
	}
	_ = repo.UpsertItem(ctx, movie)

	adminUser := &sqlite.UserRecord{
		ID: guid.New(), Name: "admin", IsAdmin: true,
		Policy: sqlite.DefaultUserPolicy(true), Config: sqlite.DefaultUserConfig(),
	}
	_ = userRepo.CreateUser(ctx, adminUser)

	normalPolicy := sqlite.DefaultUserPolicy(false)
	normalPolicy.EnableAllFolders = true
	normalUser := &sqlite.UserRecord{
		ID: guid.New(), Name: "normal", IsAdmin: false,
		Policy: normalPolicy, Config: sqlite.DefaultUserConfig(),
	}
	_ = userRepo.CreateUser(ctx, normalUser)

	reg2 := plugins.NewRegistry()
	reg2.Register(strm.New(false))
	h := New(repo, playback.New(repo, reg2, cfg), imaging.New(repo))

	// SetProxyClient nil is ignored, non-nil sets client.
	origClient := h.httpClient
	h.SetProxyClient(nil)
	if h.httpClient != origClient {
		t.Fatalf("SetProxyClient(nil) should not change client")
	}
	customClient := &http.Client{}
	h.SetProxyClient(customClient)
	if h.httpClient != customClient {
		t.Fatalf("SetProxyClient(non-nil) should update client")
	}

	// CanAccessLibrary when userRepo == nil or libraryID == "" returns true.
	if !h.CanAccessLibrary(ctx, "", lib.ID) {
		t.Fatalf("expected CanAccessLibrary true when userRepo is nil")
	}
	h.SetUserRepo(userRepo)
	if !h.CanAccessLibrary(ctx, "", "") {
		t.Fatalf("expected CanAccessLibrary true when libraryID is empty")
	}

	// isAuthenticated & canAccessUserData with UserIDFromCtx
	var ctxUID string
	mux := http.NewServeMux()
	h.RegisterRoutes(mux, AuthFuncs{
		UserIDFromCtx: func(context.Context) string { return ctxUID },
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	if h.isAuthenticated(req) {
		t.Fatalf("expected unauthenticated when ctxUID empty")
	}
	ctxUID = "unknown-user-id"
	if !h.isAuthenticated(req) {
		t.Fatalf("expected authenticated via UserIDFromCtx")
	}
	if !h.canAccessUserData(req, "") {
		t.Fatalf("expected canAccessUserData true for empty target")
	}
	// canDownloadContent with unknown caller -> false
	if h.canDownloadContent(req) {
		t.Fatalf("expected canDownloadContent false for unknown user")
	}
	// canDownloadContent with DB admin via AuthContext with IsAdmin=false on stale struct
	reqAdminStale := httptest.NewRequest(http.MethodGet, "/", nil)
	reqAdminStale = reqAdminStale.WithContext(middleware.WithAuthContext(reqAdminStale.Context(), &middleware.AuthContext{
		User: &sqlite.UserRecord{ID: adminUser.ID, Name: adminUser.Name, IsAdmin: false},
	}))
	if !h.canDownloadContent(reqAdminStale) {
		t.Fatalf("expected canDownloadContent true for DB admin")
	}
	// canAccessItem with non-existent item -> true (so handler returns 404 instead of 403)
	if !h.canAccessItem(reqAdminStale, guid.New()) {
		t.Fatalf("expected canAccessItem true for missing item")
	}
	// canAccessItem with DB admin via stale AuthContext -> true
	if !h.canAccessItem(reqAdminStale, movie.ID) {
		t.Fatalf("expected canAccessItem true for DB admin")
	}
	// canAccessItem with unknown user via UserIDFromCtx -> false
	if h.canAccessItem(req, movie.ID) {
		t.Fatalf("expected canAccessItem false for unknown user")
	}

	// Closed DB -> canAccessItem fails closed (returns false)
	_ = store.Close()
	reqNormal := httptest.NewRequest(http.MethodGet, "/", nil)
	reqNormal = reqNormal.WithContext(middleware.WithAuthContext(reqNormal.Context(), &middleware.AuthContext{
		User: normalUser,
	}))
	if h.canAccessItem(reqNormal, movie.ID) {
		t.Fatalf("expected canAccessItem fail-closed on closed DB")
	}
}

func TestStreamingStrmSecurityAndProxyCoverage(t *testing.T) {
	tmp := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmp, "strm_cov.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	ctx := context.Background()
	repo := sqlite.NewLibraryRepo(store)
	cfg := config.Default(tmp)
	lib, _ := repo.CreateLibrary(ctx, "Movies", "movies", []string{tmp})

	makeItem := func(name, path string) *sqlite.ItemRecord {
		it := &sqlite.ItemRecord{
			ID: guid.New(), LibraryID: lib.ID, ParentID: lib.ID,
			Type: "Movie", MediaType: "Video", Name: name, SortName: strings.ToLower(name), Path: path,
		}
		_ = repo.UpsertItem(ctx, it)
		_ = repo.ReplaceMediaSourcesAndStreams(ctx, it.ID, []sqlite.MediaSourceRecord{{
			ID: guid.New(), ItemID: it.ID, Path: path, Container: "strm",
		}})
		return it
	}

	// 1. Missing .strm file -> ResolveStream fails -> 502
	missingStrm := makeItem("MissingStrm", filepath.Join(tmp, "missing.strm"))

	// 2. Cloud metadata / link-local IP .strm -> ValidateRemoteURL blocks -> 502
	privateStrmPath := filepath.Join(tmp, "private.strm")
	_ = os.WriteFile(privateStrmPath, []byte("http://169.254.169.254/latest/meta-data/secret.mp4\n"), 0o644)
	privateStrm := makeItem("PrivateStrm", privateStrmPath)

	// 3. Valid public IP .strm -> proxy mode
	publicStrmPath := filepath.Join(tmp, "public.strm")
	_ = os.WriteFile(publicStrmPath, []byte("https://93.184.216.34/media/video.mkv\n"), 0o644)
	publicStrm := makeItem("PublicStrm", publicStrmPath)

	// 4. Non-HTTP .strm -> invalid strm target url -> 502
	badSchemePath := filepath.Join(tmp, "badscheme.strm")
	_ = os.WriteFile(badSchemePath, []byte("file:///etc/passwd\n"), 0o644)
	badSchemeStrm := makeItem("BadScheme", badSchemePath)

	// Handler with nil engine -> 502 "strm resolver unavailable"
	hNilEngine := New(repo, nil, imaging.New(repo))
	muxNil := http.NewServeMux()
	hNilEngine.RegisterRoutes(muxNil, AuthFuncs{})
	rec := httptest.NewRecorder()
	muxNil.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/Videos/"+publicStrm.ID+"/stream", nil))
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("nil engine .strm: want 502, got %d", rec.Code)
	}
	if _, err := hNilEngine.resolveExtractionMediaPath(ctx, publicStrmPath); err == nil {
		t.Fatalf("expected error from resolveExtractionMediaPath with nil engine")
	}

	// Handler with real engine (with strm plugin registered) and mocked proxy transport
	reg := plugins.NewRegistry()
	reg.Register(strm.New(false))
	engine := playback.New(repo, reg, cfg)
	h := New(repo, engine, imaging.New(repo))
	h.SetProxyClient(&http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header), // empty Content-Type triggers fallback to videoContentType(u.Path)
				Body:       io.NopCloser(bytes.NewReader([]byte("mkv-bytes"))),
			}, nil
		}),
	})
	mux := http.NewServeMux()
	h.RegisterRoutes(mux, AuthFuncs{})

	for _, tc := range []struct {
		name string
		url  string
		want int
	}{
		{"missing strm", "/Videos/" + missingStrm.ID + "/stream", http.StatusBadGateway},
		{"private ip strm", "/Videos/" + privateStrm.ID + "/stream", http.StatusBadGateway},
		{"bad scheme strm", "/Videos/" + badSchemeStrm.ID + "/stream", http.StatusBadGateway},
		{"proxy public strm fallback content-type", "/Videos/" + publicStrm.ID + "/stream?proxy=true", http.StatusOK},
	} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tc.url, nil))
		if rec.Code != tc.want {
			t.Fatalf("%s: want %d, got %d body=%s", tc.name, tc.want, rec.Code, rec.Body.String())
		}
		if tc.want == http.StatusOK && rec.Header().Get("Content-Type") != "video/x-matroska" {
			t.Fatalf("%s: expected fallback Content-Type video/x-matroska, got %q", tc.name, rec.Header().Get("Content-Type"))
		}
	}

	// Direct proxyRemoteMedia SSRF block & invalid URL
	recBlock := httptest.NewRecorder()
	h.proxyRemoteMedia(recBlock, httptest.NewRequest(http.MethodGet, "/", nil), "http://169.254.169.254/a.mp4", false)
	if recBlock.Code != http.StatusBadGateway {
		t.Fatalf("proxyRemoteMedia metadata ip: want 502, got %d", recBlock.Code)
	}
	recBadURL := httptest.NewRecorder()
	h.proxyRemoteMedia(recBadURL, httptest.NewRequest(http.MethodGet, "/", nil), "https://93.184.216.34/\x7f", false)
	if recBadURL.Code != http.StatusBadGateway {
		t.Fatalf("proxyRemoteMedia invalid url: want 502, got %d", recBadURL.Code)
	}

	// Direct resolveExtractionMediaPath coverage
	if got, err := h.resolveExtractionMediaPath(ctx, "/tmp/local.mkv"); err != nil || got != "/tmp/local.mkv" {
		t.Fatalf("non-strm resolveExtractionMediaPath got %q err=%v", got, err)
	}
	if _, err := h.resolveExtractionMediaPath(ctx, filepath.Join(tmp, "nope.strm")); err == nil {
		t.Fatalf("expected error for missing strm")
	}
	if _, err := h.resolveExtractionMediaPath(ctx, badSchemePath); err == nil {
		t.Fatalf("expected error for non-http strm")
	}
	if _, err := h.resolveExtractionMediaPath(ctx, privateStrmPath); err == nil {
		t.Fatalf("expected error for private ip strm")
	}
	if got, err := h.resolveExtractionMediaPath(ctx, publicStrmPath); err != nil || got != "https://93.184.216.34/media/video.mkv" {
		t.Fatalf("valid public strm got %q err=%v", got, err)
	}
}

func TestStreamingSubtitleAndChapterEdgeCoverage(t *testing.T) {
	tmp := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmp, "sub_chap_cov.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	ctx := context.Background()
	repo := sqlite.NewLibraryRepo(store)
	cfg := config.Default(tmp)
	cfg.FFmpegPath = filepath.Join(tmp, "nonexistent-ffmpeg")
	lib, _ := repo.CreateLibrary(ctx, "Movies", "movies", []string{tmp})

	// Item with no media path and embedded subtitle -> 404 media file not found for subtitle extraction
	itNoMedia := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: lib.ID, ParentID: lib.ID,
		Type: "Movie", MediaType: "Video", Name: "NoMedia", SortName: "nomedia", Path: "",
		Extra: sqlite.ItemExtra{Chapters: []sqlite.ChapterRecord{{Name: "Ch1", StartPositionTicks: 1000}}},
	}
	_ = repo.UpsertItem(ctx, itNoMedia)
	msNoMedia := guid.New()
	_ = repo.ReplaceMediaSourcesAndStreams(ctx, itNoMedia.ID, []sqlite.MediaSourceRecord{{
		ID: msNoMedia, ItemID: itNoMedia.ID, Path: "", Container: "mkv",
		Streams: []sqlite.MediaStreamRecord{
			{StreamIndex: 2, StreamType: "Subtitle", Codec: "srt", IsExternal: false, Path: ""},
			{StreamIndex: 3, StreamType: "Subtitle", Codec: "srt", IsExternal: true, Path: ""},
			{StreamIndex: 4, StreamType: "Subtitle", Codec: "srt", IsExternal: true, Path: tmp}, // dir -> ReadFile error -> 500
		},
	}})

	// Item with blocked .strm media path for subtitle & chapter extraction
	privateStrmPath := filepath.Join(tmp, "blocked.strm")
	_ = os.WriteFile(privateStrmPath, []byte("http://169.254.169.254/video.mkv\n"), 0o644)
	itStrm := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: lib.ID, ParentID: lib.ID,
		Type: "Movie", MediaType: "Video", Name: "BlockedStrm", SortName: "blockedstrm", Path: privateStrmPath,
		Extra: sqlite.ItemExtra{Chapters: []sqlite.ChapterRecord{{Name: "Ch1", StartPositionTicks: 1000}}},
	}
	_ = repo.UpsertItem(ctx, itStrm)
	msStrm := guid.New()
	_ = repo.ReplaceMediaSourcesAndStreams(ctx, itStrm.ID, []sqlite.MediaSourceRecord{{
		ID: msStrm, ItemID: itStrm.ID, Path: privateStrmPath, Container: "strm",
		Streams: []sqlite.MediaStreamRecord{
			{StreamIndex: 2, StreamType: "Subtitle", Codec: "srt", IsExternal: false, Path: ""},
		},
	}})

	// Item with local file and failing ffmpeg for chapter & subtitle extraction (no targetFormat extension)
	localVid := filepath.Join(tmp, "local.mkv")
	_ = os.WriteFile(localVid, []byte("dummy"), 0o644)
	itLocal := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: lib.ID, ParentID: lib.ID,
		Type: "Movie", MediaType: "Video", Name: "Local", SortName: "local", Path: localVid,
		Extra: sqlite.ItemExtra{Chapters: []sqlite.ChapterRecord{{Name: "Ch1", StartPositionTicks: 1000}}},
	}
	_ = repo.UpsertItem(ctx, itLocal)
	msLocal := guid.New()
	_ = repo.ReplaceMediaSourcesAndStreams(ctx, itLocal.ID, []sqlite.MediaSourceRecord{{
		ID: msLocal, ItemID: itLocal.ID, Path: localVid, Container: "mkv",
		Streams: []sqlite.MediaStreamRecord{
			{StreamIndex: 2, StreamType: "Subtitle", Codec: "srt", IsExternal: false, Path: ""},
		},
	}})

	reg2 := plugins.NewRegistry()
	reg2.Register(strm.New(false))
	h := New(repo, playback.New(repo, reg2, cfg), imaging.New(repo))
	h.SetExtractor(transcode.NewExtractor(cfg))
	mux := http.NewServeMux()
	h.RegisterRoutes(mux, AuthFuncs{
		UserIDFromCtx: func(context.Context) string { return "user-1" },
	})

	// 1. Embedded sub with empty mediaPath -> 404
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/Videos/"+itNoMedia.ID+"/"+msNoMedia+"/Subtitles/2/Stream.vtt", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("empty mediaPath sub: want 404, got %d", rec.Code)
	}
	// 2. External sub with empty path -> 404
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/Videos/"+itNoMedia.ID+"/"+msNoMedia+"/Subtitles/3/Stream.vtt", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("empty external sub path: want 404, got %d", rec.Code)
	}
	// 3. External sub pointing to directory -> 500
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/Videos/"+itNoMedia.ID+"/"+msNoMedia+"/Subtitles/4/Stream.vtt", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("dir external sub path: want 500, got %d", rec.Code)
	}
	// 4. Embedded sub on blocked .strm -> 502
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/Videos/"+itStrm.ID+"/"+msStrm+"/Subtitles/2/Stream.vtt", nil))
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("blocked strm sub: want 502, got %d", rec.Code)
	}
	// 5. Embedded sub with no extension in StreamFile (falls back to foundStream.Codec) -> 500 (missing ffmpeg)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/Videos/"+itLocal.ID+"/"+msLocal+"/Subtitles/2/Stream", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("no-ext sub extract fail: want 500, got %d", rec.Code)
	}

	// 6. Chapter thumbnail extraction edge cases: idx < 0, empty mediaPath, blocked .strm, failing ffmpeg
	if h.tryExtractAndServeChapterImage(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil), itLocal.ID, -1, imaging.Options{}) {
		t.Fatalf("expected false for negative chapter index")
	}
	for _, id := range []string{itNoMedia.ID, itStrm.ID, itLocal.ID} {
		rec = httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/Items/"+id+"/Images/Chapter/0?maxWidth=99999&maxHeight=-5", nil))
		if rec.Code != http.StatusNotFound {
			t.Fatalf("chapter image %s: want 404, got %d", id, rec.Code)
		}
	}

	// 7. Library cover fallback when children have no images -> 404 (covers tryServeLibraryCover return false)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/Items/"+lib.ID+"/Images/Primary", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("library cover without child images: want 404, got %d", rec.Code)
	}
}
