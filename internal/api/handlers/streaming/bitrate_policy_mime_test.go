package streaming

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ambra-media/ambra/internal/api/dto"
	"github.com/ambra-media/ambra/internal/api/middleware"
	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/core/auth"
	"github.com/ambra-media/ambra/internal/core/imaging"
	"github.com/ambra-media/ambra/internal/core/playback"
	"github.com/ambra-media/ambra/internal/core/transcode"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/plugins"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

type streamRoundTripFunc func(*http.Request) (*http.Response, error)

func (f streamRoundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

type mockStrmPlugin struct {
	fn func(ctx context.Context, rawPath string) (*plugins.StreamResolveResult, error)
}

func (m *mockStrmPlugin) Manifest() plugins.Manifest {
	return plugins.Manifest{
		ID:       "mock-strm-v0110",
		Name:     "MockSTRM",
		Version:  "1.0.0",
		Enabled:  true,
		Priority: 1,
		Kinds:    []plugins.Kind{plugins.KindStream},
	}
}

func (m *mockStrmPlugin) Configure(map[string]any) error { return nil }

func (m *mockStrmPlugin) ResolveStream(ctx context.Context, rawPath string) (*plugins.StreamResolveResult, error) {
	return m.fn(ctx, rawPath)
}

func TestV0110_ExtendedPolicySubnetBitrateAndPlaybackInfo(t *testing.T) {
	tmp := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmp, "streaming_v0110.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	ctx := context.Background()
	libRepo := sqlite.NewLibraryRepo(store)
	userRepo := sqlite.NewUserRepo(store)
	cfg := config.Default(tmp)
	cfg.RemoteClientBitrateLimit = 2_000_000
	cfg.LocalNetworkSubnets = []string{"", "203.0.113.0/24", "198.51.100.42"}

	reg := plugins.NewRegistry()
	var resolveMode string
	reg.Register(&mockStrmPlugin{
		fn: func(ctx context.Context, rawPath string) (*plugins.StreamResolveResult, error) {
			switch resolveMode {
			case "302":
				return &plugins.StreamResolveResult{URL: "https://93.184.216.34/cdn/movie.mp4", Is302: true}, nil
			case "headers":
				return &plugins.StreamResolveResult{
					URL:     "https://93.184.216.34/cdn/movie.mkv",
					Headers: map[string]string{"Authorization": "Bearer strm-tok", "Bad\r\nKey": "x"},
					Is302:   true,
				}, nil
			case "blocked":
				return &plugins.StreamResolveResult{URL: "http://169.254.169.254/latest/meta-data/"}, nil
			case "invalid":
				return &plugins.StreamResolveResult{URL: "ftp://files.example/a.mp4"}, nil
			default:
				return nil, errors.New("strm Boom")
			}
		},
	})

	authSvc := auth.NewService(userRepo, cfg)
	engine := playback.New(libRepo, reg, cfg)
	imgSvc := imaging.New(libRepo).WithCacheDir(filepath.Join(tmp, "imgcache"))
	extractor := transcode.NewExtractor(cfg)

	h := New(libRepo, engine, imgSvc)
	h.SetUserRepo(userRepo)
	h.SetExtractor(extractor)

	mux := http.NewServeMux()
	h.RegisterRoutes(mux, AuthFuncs{})

	libA, _ := libRepo.CreateLibrary(ctx, "MainLib", "movies", []string{tmp})
	libB, _ := libRepo.CreateLibrary(ctx, "OtherLib", "movies", []string{tmp})

	videoFile := filepath.Join(tmp, "film.mp4")
	_ = os.WriteFile(videoFile, []byte("0123456789ABCDEF"), 0o644)
	strmFile := filepath.Join(tmp, "cloud.strm")
	_ = os.WriteFile(strmFile, []byte("https://93.184.216.34/cdn/movie.mp4\n"), 0o644)

	// Sidecar BIF file alongside film.mp4: <stem>-320-10.bif
	bifSidecar := filepath.Join(tmp, "film-320-10.bif")
	_ = os.WriteFile(bifSidecar, []byte("BIF-BINARY-INDEX"), 0o644)

	seriesRated := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: libA.ID, ParentID: libA.ID,
		Type: "Series", Name: "Parent Series", OfficialRating: "PG", IsFolder: true,
	}
	epInherited := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: libA.ID, ParentID: seriesRated.ID, SeriesID: seriesRated.ID,
		Type: "Episode", MediaType: "Video", Name: "Ep1", OfficialRating: "", Container: "mp4", Path: videoFile,
	}
	movieUnrated := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: libA.ID, ParentID: libA.ID,
		Type: "Movie", MediaType: "Video", Name: "Unrated Film", OfficialRating: "", Container: "mp4", Path: videoFile,
	}
	movieUntagged := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: libA.ID, ParentID: libA.ID,
		Type: "Movie", MediaType: "Video", Name: "Untagged Film", OfficialRating: "G", Container: "mp4", Path: videoFile,
	}
	movieStrm := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: libA.ID, ParentID: libA.ID,
		Type: "Movie", MediaType: "Video", Name: "Cloud Film", OfficialRating: "G", Container: "strm", Path: strmFile, IsStrm: true,
	}
	for _, it := range []*sqlite.ItemRecord{seriesRated, epInherited, movieUnrated, movieUntagged, movieStrm} {
		if err := libRepo.UpsertItem(ctx, it); err != nil {
			t.Fatal(err)
		}
	}
	_ = libRepo.ReplaceItemTaxonomies(ctx, seriesRated.ID, "tag", []string{"safe-tag"})
	_ = libRepo.ReplaceItemTaxonomies(ctx, movieStrm.ID, "tag", []string{"safe-tag"})
	_ = libRepo.ReplaceMediaSourcesAndStreams(ctx, epInherited.ID, []sqlite.MediaSourceRecord{
		{ID: epInherited.ID, ItemID: epInherited.ID, Path: videoFile, Container: "mp4", Bitrate: 5_000_000},
	})

	admin, _ := authSvc.CreateUser(ctx, "admin", "adminpass", true)
	strictUser, _ := authSvc.CreateUser(ctx, "strict", "pass1234", false)
	strictPol := sqlite.DefaultUserPolicy(false)
	strictPol.EnableAllFolders = false
	strictPol.EnabledFolders = []string{libA.ID}
	strictPol.BlockUnratedItems = []string{"Movie", "Series", "Music"}
	strictPol.AllowedTags = []string{"safe-tag"}
	strictPol.EnableVideoPlaybackTranscoding = false
	strictPol.EnablePlaybackRemuxing = false
	strictPol.EnableContentDownloading = true
	_ = userRepo.UpdateUserPolicy(ctx, strictUser.ID, strictPol)
	strictUser.Policy = strictPol

	adminAC := &middleware.AuthContext{User: admin, Token: "adm-tok"}
	strictAC := &middleware.AuthContext{User: strictUser, Token: "str-tok"}

	doAs := func(ac *middleware.AuthContext, method, target, body, remoteAddr string) *httptest.ResponseRecorder {
		var rdr io.Reader
		if body != "" {
			rdr = strings.NewReader(body)
		}
		req := httptest.NewRequest(method, target, rdr)
		if remoteAddr != "" {
			req.RemoteAddr = remoteAddr
		}
		if ac != nil {
			req = req.WithContext(middleware.WithAuthContext(req.Context(), ac))
		}
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}

	// 1. matchesBlockUnratedType unit checks
	for _, exempt := range []string{"Folder", "CollectionFolder", "UserView", "Person", "Genre", "MusicGenre", "GameGenre", "Studio"} {
		if matchesBlockUnratedType(exempt, "", []string{"*"}) {
			t.Fatalf("expected %s to be exempt from BlockUnratedItems", exempt)
		}
	}
	if !matchesBlockUnratedType("Audio", "Audio", []string{"", "Music"}) {
		t.Fatalf("expected Audio to match Music in matchesBlockUnratedType")
	}
	if !matchesBlockUnratedType("Movie", "Video", []string{"All"}) {
		t.Fatalf("expected Movie to match All in matchesBlockUnratedType")
	}

	// 2. Extended policy enforcement: unrated blocked, untagged blocked, episode inheriting Series rating+tag allowed!
	if code := doAs(strictAC, http.MethodGet, "/Videos/"+movieUnrated.ID+"/stream", "", "127.0.0.1:1234").Code; code != http.StatusForbidden {
		t.Fatalf("expected 403 for unrated movie, got %d", code)
	}
	if code := doAs(strictAC, http.MethodGet, "/Videos/"+movieUntagged.ID+"/stream", "", "127.0.0.1:1234").Code; code != http.StatusForbidden {
		t.Fatalf("expected 403 for untagged movie, got %d", code)
	}
	if code := doAs(strictAC, http.MethodGet, "/Videos/"+epInherited.ID+"/stream", "", "127.0.0.1:1234").Code; code != http.StatusOK {
		t.Fatalf("expected 200 for episode inheriting series rating & tag, got %d", code)
	}

	// Also test canAccessItem & callerPolicy when h.userRepo == nil (AuthContext-only path)
	hNoUR := New(libRepo, engine, imgSvc)
	muxNoUR := http.NewServeMux()
	hNoUR.RegisterRoutes(muxNoUR, AuthFuncs{})
	otherLibItem := &sqlite.ItemRecord{ID: guid.New(), LibraryID: libB.ID, Type: "Movie", MediaType: "Video", Name: "B", OfficialRating: "G", Path: videoFile}
	_ = libRepo.UpsertItem(ctx, otherLibItem)
	for _, blockedID := range []string{otherLibItem.ID, movieUnrated.ID, movieUntagged.ID} {
		r := httptest.NewRequest(http.MethodGet, "/Videos/"+blockedID+"/stream", nil)
		r = r.WithContext(middleware.WithAuthContext(r.Context(), strictAC))
		w := httptest.NewRecorder()
		muxNoUR.ServeHTTP(w, r)
		if w.Code != http.StatusForbidden {
			t.Fatalf("no-userRepo expected 403 for %s, got %d", blockedID, w.Code)
		}
	}
	//PlaybackInfo on no-userRepo with strictAC hits callerPolicy fallback to ac.User.Policy
	rPBNoUR := httptest.NewRequest(http.MethodPost, "/Items/"+epInherited.ID+"/PlaybackInfo", strings.NewReader(`{"DeviceProfile":{"MaxStreamingBitrate":90000000}}`))
	rPBNoUR.RemoteAddr = "8.8.8.8:40000"
	rPBNoUR = rPBNoUR.WithContext(middleware.WithAuthContext(rPBNoUR.Context(), strictAC))
	wPBNoUR := httptest.NewRecorder()
	muxNoUR.ServeHTTP(wPBNoUR, rPBNoUR)
	if wPBNoUR.Code != http.StatusOK {
		t.Fatalf("no-userRepo PlaybackInfo failed: %d %s", wPBNoUR.Code, wPBNoUR.Body.String())
	}

	// 3. isLocalOrSubnetRequest & PlaybackInfo cross-user check
	for _, tc := range []struct {
		remote string
		want   bool
	}{
		{"127.0.0.1:1234", true},
		{"203.0.113.99:1234", true},
		{"198.51.100.42:1234", true},
		{"8.8.4.4:1234", false},
		{"not-an-ip", false},
	} {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.RemoteAddr = tc.remote
		if got := isLocalOrSubnetRequest(r, cfg.LocalNetworkSubnets); got != tc.want {
			t.Fatalf("isLocalOrSubnetRequest(%q) = %v, want %v", tc.remote, got, tc.want)
		}
	}
	if code := doAs(strictAC, http.MethodGet, "/Items/"+epInherited.ID+"/PlaybackInfo?UserId="+admin.ID, "", "127.0.0.1:1234").Code; code != http.StatusForbidden {
		t.Fatalf("expected 403 accessing another user's PlaybackInfo, got %d", code)
	}

	// 4. Video BIF sidecar serving
	recBIF := doAs(adminAC, http.MethodGet, "/Videos/"+epInherited.ID+"/index.bif?Width=320", "", "127.0.0.1:1234")
	if recBIF.Code != http.StatusOK || recBIF.Body.String() != "BIF-BINARY-INDEX" {
		t.Fatalf("handleVideoBIF expected 200 BIF-BINARY-INDEX, got %d %q", recBIF.Code, recBIF.Body.String())
	}
	if code := doAs(strictAC, http.MethodGet, "/Videos/"+movieUnrated.ID+"/index.bif", "", "127.0.0.1:1234").Code; code != http.StatusForbidden {
		t.Fatalf("expected 403 on BIF for disallowed item, got %d", code)
	}

	// 5. STRM 302 redirect, forced proxy, custom headers proxy, attachment download, and error branches
	h.SetProxyClient(&http.Client{
		Transport: streamRoundTripFunc(func(r *http.Request) (*http.Response, error) {
			hdr := make(http.Header)
			hdr.Set("Content-Type", "text/html") // unsafe -> sanitized to application/octet-stream
			hdr.Set("ETag", `"e1"`)
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     hdr,
				Body:       io.NopCloser(strings.NewReader("REMOTE-MKV-STREAM")),
			}, nil
		}),
	})

	resolveMode = "302"
	rec302 := doAs(strictAC, http.MethodGet, "/Videos/"+movieStrm.ID+"/stream", "", "127.0.0.1:1234")
	if rec302.Code != http.StatusFound || rec302.Header().Get("Location") != "https://93.184.216.34/cdn/movie.mp4" {
		t.Fatalf("expected 302 redirect for STRM, got %d loc=%s", rec302.Code, rec302.Header().Get("Location"))
	}
	// ?proxy=true bypasses 302 and reverse-proxies
	recForcedProxy := doAs(strictAC, http.MethodGet, "/Videos/"+movieStrm.ID+"/stream?proxy=true", "", "127.0.0.1:1234")
	if recForcedProxy.Code != http.StatusOK || recForcedProxy.Body.String() != "REMOTE-MKV-STREAM" {
		t.Fatalf("expected 200 proxy with ?proxy=true, got %d %q", recForcedProxy.Code, recForcedProxy.Body.String())
	}
	// Download STRM with headers sets Content-Disposition attachment
	resolveMode = "headers"
	reqDL := httptest.NewRequest(http.MethodGet, "/Items/"+movieStrm.ID+"/Download?proxy=1", nil)
	reqDL.Header.Set("Range", "bytes=0-10")
	reqDL.Header.Set("If-Range", `"e1"`)
	reqDL = reqDL.WithContext(middleware.WithAuthContext(reqDL.Context(), strictAC))
	recDL := httptest.NewRecorder()
	mux.ServeHTTP(recDL, reqDL)
	if recDL.Code != http.StatusOK || !strings.Contains(recDL.Header().Get("Content-Disposition"), `filename="movie.mkv"`) {
		t.Fatalf("STRM download proxy failed: %d cd=%q", recDL.Code, recDL.Header().Get("Content-Disposition"))
	}

	for _, badMode := range []string{"blocked", "invalid", "error"} {
		resolveMode = badMode
		if code := doAs(adminAC, http.MethodGet, "/Videos/"+movieStrm.ID+"/stream", "", "127.0.0.1:1234").Code; code != http.StatusBadGateway {
			t.Fatalf("mode %s expected 502, got %d", badMode, code)
		}
	}

	// resolveExtractionMediaPath tests
	resolveMode = "headers"
	if u, err := h.resolveExtractionMediaPath(ctx, strmFile); err != nil || u != "https://93.184.216.34/cdn/movie.mkv" {
		t.Fatalf("resolveExtractionMediaPath failed: %q %v", u, err)
	}
	if _, _, err := hNoUR.resolveExtractionMediaTarget(ctx, videoFile); err != nil {
		t.Fatalf("non-strm resolveExtractionMediaTarget failed: %v", err)
	}
}

func TestV0110_ChapterImageTagAnonymousCoverAndMIMEHelpers(t *testing.T) {
	tmp := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmp, "streaming_img_v0110.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	ctx := context.Background()
	libRepo := sqlite.NewLibraryRepo(store)
	cfg := config.Default(tmp)
	engine := playback.New(libRepo, plugins.NewRegistry(), cfg)
	imgSvc := imaging.New(libRepo).WithCacheDir(filepath.Join(tmp, "imgcache"))
	h := New(libRepo, engine, imgSvc)

	mux := http.NewServeMux()
	h.RegisterRoutes(mux, AuthFuncs{})

	lib, _ := libRepo.CreateLibrary(ctx, "CoverLib", "movies", []string{tmp})
	imgFile := filepath.Join(tmp, "chap0.jpg")
	_ = os.WriteFile(imgFile, []byte("CHAPTER-JPEG-BYTES"), 0o644)

	movie := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: lib.ID, ParentID: lib.ID,
		Type: "Movie", MediaType: "Video", Name: "Chaptered Movie", OfficialRating: "G",
		Extra: sqlite.ItemExtra{
			Chapters: []sqlite.ChapterRecord{
				{Name: "Intro", StartPositionTicks: 300_000_000},
			},
		},
	}
	_ = libRepo.UpsertItem(ctx, movie)
	_ = libRepo.ReplaceItemImages(ctx, movie.ID, []sqlite.ImageRecord{
		{ItemID: movie.ID, ImageType: "Chapter", ImageIndex: 0, Path: imgFile, Tag: "db-stored-tag"},
		{ItemID: movie.ID, ImageType: "Primary", ImageIndex: 0, Path: imgFile, Tag: "prim-tag-1"},
	})

	// 1. Anonymous Chapter image request matching deterministic computeChapterImageTag!
	chTag := computeChapterImageTag(movie.ID, 0, 300_000_000)
	reqCh := httptest.NewRequest(http.MethodGet, "/Items/"+movie.ID+"/Images/Chapter/0?tag="+chTag, nil)
	recCh := httptest.NewRecorder()
	mux.ServeHTTP(recCh, reqCh)
	if recCh.Code != http.StatusOK || recCh.Body.String() != "CHAPTER-JPEG-BYTES" {
		t.Fatalf("anonymous Chapter image with computed tag failed: %d %q", recCh.Code, recCh.Body.String())
	}
	// Wrong tag -> 404
	reqBad := httptest.NewRequest(http.MethodGet, "/Items/"+movie.ID+"/Images/Chapter/0?tag=wrongtag00000000", nil)
	recBad := httptest.NewRecorder()
	mux.ServeHTTP(recBad, reqBad)
	if recBad.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for wrong chapter tag, got %d", recBad.Code)
	}

	// 2. Anonymous synthetic library cover matching Primary image tag (when child has no Backdrop)
	synPrim := computeSyntheticLibraryCoverTag(lib.ID, "prim-tag-1")
	reqCov := httptest.NewRequest(http.MethodGet, "/Items/"+lib.ID+"/Images/Primary?tag="+synPrim, nil)
	recCov := httptest.NewRecorder()
	mux.ServeHTTP(recCov, reqCov)
	if recCov.Code != http.StatusOK || recCov.Body.String() != "CHAPTER-JPEG-BYTES" {
		t.Fatalf("anonymous synthetic Primary library cover failed: %d %q", recCov.Code, recCov.Body.String())
	}
	// Anonymous wrong library cover tag -> 404
	reqCovBad := httptest.NewRequest(http.MethodGet, "/Items/"+lib.ID+"/Images/Primary?tag=deadbeefdeadbeef", nil)
	recCovBad := httptest.NewRecorder()
	mux.ServeHTTP(recCovBad, reqCovBad)
	if recCovBad.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for wrong anonymous library cover tag, got %d", recCovBad.Code)
	}

	// Authenticated library cover fallback to Primary without tag + post-filtering by ExtendedPolicy
	polUser := &sqlite.UserRecord{
		ID: guid.New(), Name: "viewer",
		Policy: dto.UserPolicy{
			EnableAllFolders:  false,
			EnabledFolders:    []string{lib.ID},
			BlockUnratedItems: []string{"Movie"},
		},
	}
	hAuth := New(libRepo, engine, imgSvc)
	muxAuth := http.NewServeMux()
	hAuth.RegisterRoutes(muxAuth, AuthFuncs{})
	reqAuthCov := httptest.NewRequest(http.MethodGet, "/Items/"+lib.ID+"/Images/Primary", nil)
	reqAuthCov = reqAuthCov.WithContext(middleware.WithAuthContext(ctx, &middleware.AuthContext{User: polUser}))
	recAuthCov := httptest.NewRecorder()
	muxAuth.ServeHTTP(recAuthCov, reqAuthCov)
	if recAuthCov.Code != http.StatusOK {
		t.Fatalf("authenticated Primary library cover failed: %d", recAuthCov.Code)
	}

	// 3. privateImageResponseWriter.Write header override & MIME helpers
	rawW := httptest.NewRecorder()
	rawW.Header().Set("Cache-Control", "public, max-age=3600")
	pw := &privateImageResponseWriter{ResponseWriter: rawW}
	_, _ = pw.Write([]byte("img"))
	if !strings.HasPrefix(rawW.Header().Get("Cache-Control"), "private") {
		t.Fatalf("expected private Cache-Control override, got %q", rawW.Header().Get("Cache-Control"))
	}

	for _, mt := range []string{"", "video/mp4", "application/vnd.apple.mpegurl", "text/vtt", "text/html", "bad;;mime"} {
		_ = sanitizeProxyMediaContentType(mt, "video/mp4")
	}
	if sanitizeContentDispositionFilename("") != "media" {
		t.Fatalf("expected fallback filename 'media'")
	}
	if clampImageDim(-5) != 0 || clampImageDim(99999) != maxImageDim {
		t.Fatalf("unexpected clampImageDim result")
	}
	for _, ext := range []string{".mp4", ".mkv", ".webm", ".mov", ".avi", ".ts", ".unknown"} {
		_ = videoContentType("v" + ext)
	}
}
