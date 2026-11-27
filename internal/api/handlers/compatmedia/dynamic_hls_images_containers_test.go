package compatmedia

import (
	"context"
	"encoding/json"
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
	"github.com/ambra-media/ambra/internal/core/scanner"
	"github.com/ambra-media/ambra/internal/core/transcode"
	"github.com/ambra-media/ambra/internal/pkg/eventbus"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/plugins"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

type covMockImageProvider struct{}

func (covMockImageProvider) Manifest() plugins.Manifest {
	return plugins.Manifest{
		ID:      "cov-fanart-tmdb",
		Name:    "CovFanart",
		Version: "1.0.0",
		Enabled: true,
		Kinds:   []plugins.Kind{plugins.KindImage},
	}
}
func (covMockImageProvider) Configure(map[string]any) error { return nil }
func (covMockImageProvider) FetchImages(_ context.Context, _ plugins.MediaQuery) ([]plugins.RemoteImageHit, error) {
	return []plugins.RemoteImageHit{
		{URL: "https://img.example.test/p1.jpg", Type: "Primary", Width: 1000, Height: 1500, Rating: 8.5},
		{URL: "https://img.example.test/p1.jpg", Type: "Primary", ProviderName: "CovFanart"}, // duplicate URL
		{URL: "https://img.example.test/b1.jpg", Type: "Backdrop", ProviderName: "OtherProv"},
	}, nil
}

func TestCovV0112_CompatPureHelpersAndExtendedPolicy(t *testing.T) {
	// matchesBlockUnratedItemType
	for _, exempt := range []string{"CollectionFolder", "Folder", "UserView", "Person", "Genre", "MusicGenre", "GameGenre", "Studio"} {
		if matchesBlockUnratedItemType(exempt, "Video", []string{"*"}) {
			t.Fatalf("expected exempt type %s not blocked", exempt)
		}
	}
	if !matchesBlockUnratedItemType("Episode", "Video", []string{"", "Series"}) {
		t.Fatal("expected Series rule to match Episode")
	}
	if !matchesBlockUnratedItemType("Season", "", []string{"Series"}) {
		t.Fatal("expected Series rule to match Season")
	}
	if !matchesBlockUnratedItemType("MusicAlbum", "", []string{"Music"}) {
		t.Fatal("expected Music rule to match MusicAlbum")
	}
	if !matchesBlockUnratedItemType("Movie", "Video", []string{"All"}) {
		t.Fatal("expected All rule to match Movie")
	}
	if matchesBlockUnratedItemType("Movie", "Video", []string{"Book"}) {
		t.Fatal("expected Book rule not to match Movie")
	}

	// Codec & segment sanitizers
	videoCases := map[string]string{
		"": "","h264": "h264", "avc": "h264", "hevc": "hevc", "h265": "hevc",
		"av1": "av1", "vp9": "vp9", "copy": "copy", "mpeg2video": "h264",
	}
	for in, want := range videoCases {
		if got := sanitizeCompatVideoCodec(in); got != want {
			t.Fatalf("sanitizeCompatVideoCodec(%q) = %q, want %q", in, got, want)
		}
	}
	for _, ac := range []string{"aac", "mp3", "opus", "flac", "ac3", "eac3", "vorbis", "copy"} {
		if got := sanitizeCompatAudioCodec(ac); got != ac {
			t.Fatalf("sanitizeCompatAudioCodec(%q) = %q", ac, got)
		}
	}
	if got := sanitizeCompatAudioCodec("wmav2"); got != "aac" {
		t.Fatalf("expected fallback aac, got %q", got)
	}

	if idx, ext, isInit, ok := parseCompatHLSSegmentName("init.mp4"); !ok || !isInit || idx != 0 || ext != "m4s" {
		t.Fatalf("unexpected init.mp4 parse: %d %s %v %v", idx, ext, isInit, ok)
	}
	if idx, ext, isInit, ok := parseCompatHLSSegmentName("segment5.aac"); !ok || isInit || idx != 5 || ext != "ts" {
		t.Fatalf("unexpected segment5.aac parse: %d %s %v %v", idx, ext, isInit, ok)
	}
	for _, bad := range []string{"nodot", ".ts", "0.bad", "seg_-1.ts", "abc.mp4"} {
		if _, _, _, ok := parseCompatHLSSegmentName(bad); ok {
			t.Fatalf("expected %q to fail parseCompatHLSSegmentName", bad)
		}
	}

	// isCompatRemoteClientRequest
	lanReq := httptest.NewRequest(http.MethodGet, "/", nil)
	lanReq.RemoteAddr = "127.0.0.1:12345"
	if isCompatRemoteClientRequest(lanReq, nil) {
		t.Fatal("expected loopback not remote")
	}
	pubReq := httptest.NewRequest(http.MethodGet, "/", nil)
	pubReq.RemoteAddr = "8.8.8.8:44321"
	if !isCompatRemoteClientRequest(pubReq, nil) {
		t.Fatal("expected 8.8.8.8 remote when cfg is nil")
	}
	cfg, err := config.Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cfg.LocalNetworkSubnets = []string{"8.8.8.0/24", "9.9.9.9"}
	if isCompatRemoteClientRequest(pubReq, cfg) {
		t.Fatal("expected 8.8.8.8 treated as LAN via CIDR")
	}
	exactReq := httptest.NewRequest(http.MethodGet, "/", nil)
	exactReq.RemoteAddr = "9.9.9.9:5555"
	if isCompatRemoteClientRequest(exactReq, cfg) {
		t.Fatal("expected 9.9.9.9 treated as LAN via exact IP")
	}
	otherPubReq := httptest.NewRequest(http.MethodGet, "/", nil)
	otherPubReq.RemoteAddr = "1.1.1.1:5555"
	if !isCompatRemoteClientRequest(otherPubReq, cfg) {
		t.Fatal("expected 1.1.1.1 treated as remote")
	}

	// Misc pure helpers
	if m := defaultEncodingConfigForKey("unknown_custom_key"); len(m) != 0 {
		t.Fatalf("expected empty map for unknown key, got %v", m)
	}
	if got := sanitizeContentDispositionFilename(""); got != "file" {
		t.Fatalf("expected fallback 'file', got %q", got)
	}
	if got := sanitizeContentDispositionFilename("a\r\n\"b\\c.mkv"); strings.ContainsAny(got, "\r\n") {
		t.Fatalf("expected CRLF stripped, got %q", got)
	}
	if got := scopePlaySessionID("ps1", "u1", false); got != "u1:ps1" {
		t.Fatalf("expected u1:ps1, got %q", got)
	}
	if got := scopePlaySessionID("u1:ps1", "u1", false); got != "u1:ps1" {
		t.Fatalf("expected idempotent u1:ps1, got %q", got)
	}
	if _, err := parseEmbyDate("2025-02-03"); err != nil {
		t.Fatalf("parseEmbyDate failed: %v", err)
	}
	if _, err := parseEmbyDate("not-a-date"); err == nil {
		t.Fatal("expected invalid date error")
	}

	// isItemBlockedByExtendedPolicy & filterExtrasByParentalPolicy & repo-nil allowedLibraries branches
	ctx := context.Background()
	store, err := sqlite.Open(cfg.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	repo := sqlite.NewLibraryRepo(store)
	userRepo := sqlite.NewUserRepo(store)
	mediaDir := filepath.Join(t.TempDir(), "media")
	_ = os.MkdirAll(mediaDir, 0o755)
	lib, _ := repo.CreateLibrary(ctx, "TV", "tvshows", []string{mediaDir})

	seriesRated := &sqlite.ItemRecord{ID: guid.New(), LibraryID: lib.ID, ParentID: lib.ID, Type: "Series", IsFolder: true, Name: "S1", OfficialRating: "TV-PG"}
	seriesUnrated := &sqlite.ItemRecord{ID: guid.New(), LibraryID: lib.ID, ParentID: lib.ID, Type: "Series", IsFolder: true, Name: "S2", OfficialRating: ""}
	epInherited := &sqlite.ItemRecord{ID: guid.New(), LibraryID: lib.ID, ParentID: seriesRated.ID, SeriesID: seriesRated.ID, Type: "Episode", MediaType: "Video", Name: "E1"}
	epUnrated := &sqlite.ItemRecord{ID: guid.New(), LibraryID: lib.ID, ParentID: seriesUnrated.ID, SeriesID: seriesUnrated.ID, Type: "Episode", MediaType: "Video", Name: "E2"}
	extraR := &sqlite.ItemRecord{ID: guid.New(), LibraryID: lib.ID, ParentID: seriesRated.ID, Type: "Video", Extra: sqlite.ItemExtra{ExtraType: "Trailer"}, Name: "RedBand", OfficialRating: "R"}
	extraG := &sqlite.ItemRecord{ID: guid.New(), LibraryID: lib.ID, ParentID: seriesRated.ID, Type: "Video", Extra: sqlite.ItemExtra{ExtraType: "Trailer"}, Name: "GreenBand", OfficialRating: "G"}
	for _, it := range []*sqlite.ItemRecord{seriesRated, seriesUnrated, epInherited, epUnrated, extraR, extraG} {
		_ = repo.UpsertItem(ctx, it)
	}
	_ = repo.ReplaceItemTaxonomies(ctx, seriesRated.ID, "tag", []string{"kids-approved"})

	h := New(repo, nil, nil, nil, store, cfg)
	h.SetUserRepo(userRepo)

	if h.isItemBlockedByExtendedPolicy(ctx, nil, dto.UserPolicy{}) {
		t.Fatal("nil item should not be blocked")
	}
	pol := dto.UserPolicy{
		BlockUnratedItems: []string{"Series"},
		AllowedTags:       []string{"", "kids-approved"},
	}
	if h.isItemBlockedByExtendedPolicy(ctx, epInherited, pol) {
		t.Fatal("expected epInherited allowed via series rating + series tag")
	}
	if !h.isItemBlockedByExtendedPolicy(ctx, epUnrated, pol) {
		t.Fatal("expected epUnrated blocked by unrated series")
	}
	if !h.isItemBlockedByExtendedPolicy(ctx, extraG, pol) {
		t.Fatal("expected untagged extraG blocked by AllowedTags")
	}

	maxPG := 5
	extrasIn := []dto.BaseItemDto{{Id: extraR.ID, Name: "Red"}, {Id: extraG.ID, Name: "Green"}}
	if got := h.filterExtrasByParentalPolicy(ctx, extrasIn, nil, nil); len(got) != 2 {
		t.Fatalf("expected unfiltered extras when policy empty, got %d", len(got))
	}
	if got := h.filterExtrasByParentalPolicy(ctx, extrasIn, &maxPG, []string{"nsfw"}); len(got) != 1 || got[0].Id != extraG.ID {
		t.Fatalf("expected only extraG after parental filter, got %+v", got)
	}

	// Handler without repo / without userRepo fallback branches
	uScoped := &sqlite.UserRecord{
		ID: guid.New(), Name: "scoped_u",
		Policy: dto.UserPolicy{EnableAllFolders: false, EnabledFolders: []string{lib.ID}},
	}
	_ = userRepo.CreateUser(ctx, uScoped)
	uAll := &sqlite.UserRecord{
		ID: guid.New(), Name: "all_u",
		Policy: dto.UserPolicy{EnableAllFolders: true},
	}
	_ = userRepo.CreateUser(ctx, uAll)

	hNoRepo := &Handler{userRepo: userRepo}
	if got := hNoRepo.allowedLibrariesForUser(ctx, uScoped.ID); len(got) != 1 {
		t.Fatalf("expected 1 allowed folder without repo, got %v", got)
	}
	if got := hNoRepo.allowedLibrariesForUser(ctx, uAll.ID); got != nil {
		t.Fatalf("expected nil allowed folders for EnableAllFolders without repo, got %v", got)
	}
	if hNoRepo.isPlaylistPublic(ctx, "") {
		t.Fatal("empty playlist ID cannot be public")
	}
	_ = userRepo.SetServerConfig(ctx, "item_visibility_"+guid.Normalize(epInherited.ID), `{"IsPublic":true}`)
	if !hNoRepo.isPlaylistPublic(ctx, epInherited.ID) {
		t.Fatal("expected fallback userRepo.GetServerConfig to detect public playlist")
	}

	hNoUserRepo := &Handler{repo: repo}
	disabledCtx := middleware.WithAuthContext(ctx, &middleware.AuthContext{
		User: &sqlite.UserRecord{ID: guid.New(), IsDisabled: true},
	})
	if got := hNoUserRepo.allowedLibrariesForRequest(httptest.NewRequest(http.MethodGet, "/", nil).WithContext(disabledCtx), ""); len(got) != 0 {
		t.Fatalf("expected empty slice for disabled user without userRepo, got %v", got)
	}
	activeCtx := middleware.WithAuthContext(ctx, &middleware.AuthContext{
		User: &sqlite.UserRecord{ID: guid.New(), Policy: dto.UserPolicy{EnableAllFolders: false, EnabledFolders: []string{lib.ID}}},
	})
	if got := hNoUserRepo.allowedLibrariesForRequest(httptest.NewRequest(http.MethodGet, "/", nil).WithContext(activeCtx), ""); len(got) != 1 {
		t.Fatalf("expected 1 library from repo policy fallback, got %v", got)
	}
}

func TestCovV0112_CompatRoutesHLSImagesAudioBooksAndContainers(t *testing.T) {
	tmp := t.TempDir()
	dataDir := filepath.Join(tmp, "data")
	mediaRoot := filepath.Join(tmp, "media")
	tempTranscodeDir := filepath.Join(tmp, "tc-custom")
	_ = os.MkdirAll(mediaRoot, 0o755)
	_ = os.MkdirAll(tempTranscodeDir, 0o755)

	cfg, err := config.Load(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	cfg.FFmpegPath = "/nonexistent/ffmpeg-cov"
	cfg.RemoteClientBitrateLimit = 400_000
	cfg.SimultaneousStreamLimit = 3

	store, err := sqlite.Open(cfg.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	ctx := context.Background()
	repo := sqlite.NewLibraryRepo(store)
	userRepo := sqlite.NewUserRepo(store)
	authSvc := auth.NewService(userRepo, cfg)

	adminUser, _ := authSvc.CreateUser(ctx, "adm", "adm123", true)
	alice, _ := authSvc.CreateUser(ctx, "alice", "alice123", false)
	bob, _ := authSvc.CreateUser(ctx, "bob", "bob123", false)

	maxPG := 5
	alicePol := alice.Policy
	alicePol.EnableAllFolders = true
	alicePol.EnableMediaPlayback = true
	alicePol.EnableAudioPlaybackTranscoding = false
	alicePol.EnableVideoPlaybackTranscoding = false
	alicePol.EnablePlaybackRemuxing = false
	alicePol.MaxParentalRating = &maxPG
	alicePol.BlockUnratedItems = []string{"AudioBook"}
	alicePol.AllowedTags = []string{"ok"}
	_ = userRepo.UpdateUserPolicy(ctx, alice.ID, alicePol)

	admLogin, _ := authSvc.AuthenticateByName(ctx, "adm", "adm123", "", auth.ClientInfo{DeviceID: "d-adm"})
	aliceLogin, _ := authSvc.AuthenticateByName(ctx, "alice", "alice123", "", auth.ClientInfo{DeviceID: "d-alice"})
	bobLogin, _ := authSvc.AuthenticateByName(ctx, "bob", "bob123", "", auth.ClientInfo{DeviceID: "d-bob"})

	lib, _ := repo.CreateLibrary(ctx, "Media", "movies", []string{mediaRoot})
	mediaFile := filepath.Join(mediaRoot, "video.mp4")
	_ = os.WriteFile(mediaFile, []byte("fake-mp4-data"), 0o644)

	yr, sNum, eNum := 2024, 1, 2
	mov := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: lib.ID, ParentID: lib.ID,
		Type: "Movie", MediaType: "Video", Name: "Cov Movie", OfficialRating: "G",
		ProductionYear: &yr, ParentIndexNumber: &sNum, IndexNumber: &eNum,
		Path: mediaFile, Extra: sqlite.ItemExtra{ProviderIDs: map[string]string{"tmdb": "550"}},
	}
	trailer := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: lib.ID, ParentID: mov.ID,
		Type: "Video", Extra: sqlite.ItemExtra{ExtraType: "Trailer"}, MediaType: "Video", Name: "Cov Trailer", OfficialRating: "G", Path: mediaFile,
	}
	feature := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: lib.ID, ParentID: mov.ID,
		Type: "Video", Extra: sqlite.ItemExtra{ExtraType: "Featurette"}, MediaType: "Video", Name: "Cov Featurette", OfficialRating: "G", Path: mediaFile,
	}
	ab1 := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: lib.ID, ParentID: lib.ID,
		Type: "AudioBook", MediaType: "Audio", Name: "Book 1", OfficialRating: "G", Path: mediaFile,
	}
	ab2 := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: lib.ID, ParentID: lib.ID,
		Type: "AudioBook", MediaType: "Audio", Name: "Book 2", OfficialRating: "G", Path: mediaFile,
	}
	abUnrated := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: lib.ID, ParentID: lib.ID,
		Type: "AudioBook", MediaType: "Audio", Name: "Book Unrated", OfficialRating: "", Path: mediaFile,
	}
	for _, it := range []*sqlite.ItemRecord{mov, trailer, feature, ab1, ab2, abUnrated} {
		if err := repo.UpsertItem(ctx, it); err != nil {
			t.Fatal(err)
		}
		_ = repo.ReplaceItemTaxonomies(ctx, it.ID, "tag", []string{"ok"})
	}
	for _, bk := range []*sqlite.ItemRecord{ab1, ab2, abUnrated} {
		_, _ = store.Writer.ExecContext(ctx,
			`INSERT OR REPLACE INTO user_item_data(user_id, item_id, played, play_count, playback_position_ticks, is_favorite) VALUES(?, ?, 0, 0, 90000, 0)`,
			guid.Normalize(alice.ID), guid.Normalize(bk.ID),
		)
	}

	_ = repo.ReplaceItemPeople(ctx, mov.ID, []sqlite.PersonInput{{
		Name: "Keanu Reeves", Type: "Actor",
	}})
	personID := guid.New()

	reg := plugins.NewRegistry()
	reg.Register(covMockImageProvider{})
	sc := scanner.New(repo, reg, eventbus.New(), cfg)
	imgSvc := imaging.New(repo).WithCacheDir(cfg.ImageCacheDir())
	tcMgr := transcode.NewManager(cfg, transcode.WithCapabilities(transcode.SoftwareOnlyCapabilities()))
	tcMgr.SetStore(store)
	defer tcMgr.Close()

	h := New(repo, sc, imgSvc, reg, store, cfg)
	h.SetUserRepo(userRepo)
	h.SetTranscodeManager(tcMgr)

	authMW := middleware.NewAuth(authSvc, nil)
	router := h.BuildRouter(AuthFuncs{
		RequireAuth:  authMW.RequireAuth,
		RequireAdmin: authMW.RequireAdmin,
	})

	doReq := func(token, remoteAddr, method, target, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, target, strings.NewReader(body))
		if remoteAddr != "" {
			req.RemoteAddr = remoteAddr
		}
		req.Header.Set("X-Emby-Token", token)
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}

	// 1. Dynamic HLS permission & HEAD/GET/remote-bitrate/slot branches
	if code := doReq(aliceLogin.AccessToken, "", http.MethodGet, "/Audio/"+mov.ID+"/hls1/main/0.ts", "").Code; code != http.StatusForbidden {
		t.Fatalf("expected 403 when EnableAudioPlaybackTranscoding=false, got %d", code)
	}
	if code := doReq(aliceLogin.AccessToken, "", http.MethodGet, "/Videos/"+mov.ID+"/hls/main/0.ts?VideoCodec=copy", "").Code; code != http.StatusForbidden {
		t.Fatalf("expected 403 when EnablePlaybackRemuxing=false, got %d", code)
	}
	if code := doReq(aliceLogin.AccessToken, "", http.MethodGet, "/Videos/"+mov.ID+"/hls/main/0.ts?VideoCodec=h264", "").Code; code != http.StatusForbidden {
		t.Fatalf("expected 403 when EnableVideoPlaybackTranscoding=false, got %d", code)
	}
	if code := doReq(admLogin.AccessToken, "", http.MethodGet, "/Videos/"+mov.ID+"/hls/main/bad.xyz", "").Code; code != http.StatusNotFound {
		t.Fatalf("expected 404 on invalid segment name, got %d", code)
	}
	if code := doReq(admLogin.AccessToken, "", http.MethodGet, "/Videos/"+guid.New()+"/hls/main/0.ts", "").Code; code != http.StatusNotFound {
		t.Fatalf("expected 404 on unknown item, got %d", code)
	}
	if r := doReq(admLogin.AccessToken, "", http.MethodHead, "/Videos/"+mov.ID+"/hls/main/init.mp4", ""); r.Code != http.StatusOK || r.Header().Get("Content-Type") != "video/mp4" {
		t.Fatalf("expected HEAD video init.mp4 -> 200 video/mp4, got %d %q", r.Code, r.Header().Get("Content-Type"))
	}
	if r := doReq(admLogin.AccessToken, "", http.MethodHead, "/Audio/"+mov.ID+"/hls1/main/init.mp4", ""); r.Code != http.StatusOK || r.Header().Get("Content-Type") != "audio/mp4" {
		t.Fatalf("expected HEAD audio init.mp4 -> 200 audio/mp4, got %d %q", r.Code, r.Header().Get("Content-Type"))
	}
	// Remote client IP exercises RemoteClientBitrateLimit + SimultaneousStreamLimit + GetInitSegment/GetSegment
	_ = doReq(admLogin.AccessToken, "8.8.4.4:43210", http.MethodGet, "/Videos/"+mov.ID+"/hls/main/init.mp4?AudioBitrate=450000&VideoBitrate=2000000", "")
	_ = doReq(admLogin.AccessToken, "8.8.4.4:43210", http.MethodGet, "/Audio/"+mov.ID+"/hls1/main/0.ts?AudioBitrate=450000", "")

	// 2. Encoding/FfmpegOptions TranscodingTempPath validation & reload
	if code := doReq(admLogin.AccessToken, "", http.MethodPost, "/Encoding/FfmpegOptions", `{"TranscodingTempPath":"relative/bad"}`).Code; code != http.StatusBadRequest {
		t.Fatalf("expected 400 on relative TranscodingTempPath, got %d", code)
	}
	if code := doReq(admLogin.AccessToken, "", http.MethodPost, "/Encoding/FfmpegOptions", `{"TranscodingTempPath":"`+tempTranscodeDir+`"}`).Code; code != http.StatusNoContent {
		t.Fatalf("expected 204 on valid TranscodingTempPath, got %d", code)
	}
	if code := doReq(admLogin.AccessToken, "", http.MethodPost, "/Encoding/FfmpegOptions", `{"TranscodingTempPath":"   "}`).Code; code != http.StatusNoContent {
		t.Fatalf("expected 204 clearing TranscodingTempPath, got %d", code)
	}

	// 3. Image index swap & non-overlapping move
	_, _ = store.Writer.ExecContext(ctx,
		`INSERT INTO item_images (item_id, image_type, image_index, path, tag, width, height) VALUES (?, 'Backdrop', 0, ?, 't0', 100, 100), (?, 'Backdrop', 1, ?, 't1', 100, 100)`,
		guid.Normalize(mov.ID), mediaFile, guid.Normalize(mov.ID), mediaFile,
	)
	if code := doReq(admLogin.AccessToken, "", http.MethodPost, "/Items/"+mov.ID+"/Images/Backdrop/bad/Index?newIndex=1", "").Code; code != http.StatusBadRequest {
		t.Fatalf("expected 400 on invalid Index, got %d", code)
	}
	if code := doReq(admLogin.AccessToken, "", http.MethodPost, "/Items/"+mov.ID+"/Images/Backdrop/0/Index?newIndex=-2", "").Code; code != http.StatusBadRequest {
		t.Fatalf("expected 400 on negative newIndex, got %d", code)
	}
	if code := doReq(admLogin.AccessToken, "", http.MethodPost, "/Items/"+mov.ID+"/Images/Backdrop/0/Index?newIndex=1", "").Code; code != http.StatusNoContent {
		t.Fatalf("expected 204 swapping image index 0 <-> 1, got %d", code)
	}
	if code := doReq(admLogin.AccessToken, "", http.MethodPost, "/Items/"+mov.ID+"/Images/Backdrop/1/Index?newIndex=4", "").Code; code != http.StatusNoContent {
		t.Fatalf("expected 204 moving image index 1 -> 4, got %d", code)
	}

	// 4. RemoteImages with ImageProvider + Type/ProviderName filters
	if code := doReq(admLogin.AccessToken, "", http.MethodGet, "/Items/"+guid.New()+"/RemoteImages", "").Code; code != http.StatusNotFound {
		t.Fatalf("expected 404 on missing item RemoteImages, got %d", code)
	}
	remRec := doReq(admLogin.AccessToken, "", http.MethodGet, "/Items/"+mov.ID+"/RemoteImages?Type=Primary&ProviderName=CovFanart", "")
	if remRec.Code != http.StatusOK {
		t.Fatalf("expected 200 on RemoteImages, got %d", remRec.Code)
	}

	// 5. AudioBooks/NextUp post-filtering + pagination & cross-user RBAC
	if code := doReq(aliceLogin.AccessToken, "", http.MethodGet, "/AudioBooks/NextUp?UserId="+bob.ID, "").Code; code != http.StatusForbidden {
		t.Fatalf("expected 403 cross-user AudioBooks/NextUp, got %d", code)
	}
	abRec := doReq(aliceLogin.AccessToken, "", http.MethodGet, "/AudioBooks/NextUp?StartIndex=1&Limit=1", "")
	if abRec.Code != http.StatusOK {
		t.Fatalf("expected 200 on AudioBooks/NextUp, got %d", abRec.Code)
	}
	var abRes dto.QueryResult[dto.BaseItemDto]
	_ = json.Unmarshal(abRec.Body.Bytes(), &abRes)
	if abRes.TotalRecordCount != 2 || len(abRes.Items) != 1 {
		t.Fatalf("expected TotalRecordCount=2 len=1 after post-filter pagination, got total=%d len=%d", abRes.TotalRecordCount, len(abRes.Items))
	}
	if r := doReq(aliceLogin.AccessToken, "", http.MethodGet, "/AudioBooks/NextUp?StartIndex=99&Limit=5", ""); r.Code != http.StatusOK {
		t.Fatalf("expected 200 on high StartIndex AudioBooks/NextUp, got %d", r.Code)
	}

	// 6. PersonCredits, LocalTrailers, SpecialFeatures, ThemeSongs, ThemeVideos
	if code := doReq(aliceLogin.AccessToken, "", http.MethodGet, "/Persons/"+personID+"/Credits?UserId="+bob.ID, "").Code; code != http.StatusForbidden {
		t.Fatalf("expected 403 cross-user PersonCredits, got %d", code)
	}
	if code := doReq(aliceLogin.AccessToken, "", http.MethodGet, "/Persons/"+personID+"/Credits", "").Code; code != http.StatusOK {
		t.Fatalf("expected 200 PersonCredits, got %d", code)
	}
	for _, ep := range []string{"/LocalTrailers", "/SpecialFeatures", "/ThemeSongs", "/ThemeVideos", "/ThemeMedia"} {
		if code := doReq(aliceLogin.AccessToken, "", http.MethodGet, "/Items/"+mov.ID+ep+"?UserId="+bob.ID, "").Code; code != http.StatusForbidden {
			t.Fatalf("expected 403 cross-user %s, got %d", ep, code)
		}
		if code := doReq(aliceLogin.AccessToken, "", http.MethodGet, "/Items/"+mov.ID+ep, "").Code; code != http.StatusOK {
			t.Fatalf("expected 200 %s, got %d", ep, code)
		}
	}

	// 7. Collections ProviderItems & Playlists Items/Delete + AddToPlaylistInfo
	box, _ := repo.CreateCollection(ctx, "Cov Box", "", []string{mov.ID, abUnrated.ID})
	box.OfficialRating = "G"
	_ = repo.UpsertItem(ctx, box)
	_ = repo.ReplaceItemTaxonomies(ctx, box.ID, "tag", []string{"ok"})

	if code := doReq(aliceLogin.AccessToken, "", http.MethodGet, "/Collections/"+mov.ID+"/ProviderItems", "").Code; code != http.StatusNotFound {
		t.Fatalf("expected 404 on non-BoxSet ProviderItems, got %d", code)
	}
	if r := doReq(aliceLogin.AccessToken, "", http.MethodGet, "/Collections/"+box.ID+"/ProviderItems?StartIndex=0&Limit=1", ""); r.Code != http.StatusOK {
		t.Fatalf("expected 200 on Collection ProviderItems, got %d", r.Code)
	}
	if r := doReq(aliceLogin.AccessToken, "", http.MethodGet, "/Collections/"+box.ID+"/ProviderItems?StartIndex=50&Limit=5", ""); r.Code != http.StatusOK {
		t.Fatalf("expected 200 on out-of-range Collection ProviderItems, got %d", r.Code)
	}

	pl, _ := repo.CreatePlaylist(ctx, "Alice PL", "Video", alice.ID, []string{mov.ID})
	_ = repo.ReplaceItemTaxonomies(ctx, pl.ID, "tag", []string{"ok"})
	pl.OfficialRating = "G"
	_ = repo.UpsertItem(ctx, pl)

	if code := doReq(bobLogin.AccessToken, "", http.MethodPost, "/Playlists/"+pl.ID+"/Items/Delete", `{"Ids":["`+mov.ID+`"]}`).Code; code != http.StatusForbidden {
		t.Fatalf("expected 403 when non-owner deletes playlist items, got %d", code)
	}
	if code := doReq(aliceLogin.AccessToken, "", http.MethodGet, "/Playlists/"+pl.ID+"/AddToPlaylistInfo", "").Code; code != http.StatusOK {
		t.Fatalf("expected 200 on AddToPlaylistInfo, got %d", code)
	}
	if code := doReq(aliceLogin.AccessToken, "", http.MethodPost, "/Playlists/"+pl.ID+"/Items/Delete", `{"Ids":["`+mov.ID+`"]}`).Code; code != http.StatusNoContent {
		t.Fatalf("expected 204 when owner deletes playlist items via JSON body, got %d", code)
	}

	// 8. Channels/Items/Latest (empty, single, multi-channel merge + sort + pagination)
	ch1ID, ch2ID := guid.New(), guid.New()
	chItem1 := &sqlite.ItemRecord{ID: guid.New(), LibraryID: lib.ID, ParentID: ch1ID, Type: "ChannelVideoItem", MediaType: "Video", Name: "ChVid1", OfficialRating: "G", DateCreated: 1000}
	chItem2 := &sqlite.ItemRecord{ID: guid.New(), LibraryID: lib.ID, ParentID: ch2ID, Type: "ChannelVideoItem", MediaType: "Video", Name: "ChVid2", OfficialRating: "G", DateCreated: 2000}
	_ = repo.UpsertItem(ctx, chItem1)
	_ = repo.UpsertItem(ctx, chItem2)
	_ = repo.ReplaceItemTaxonomies(ctx, chItem1.ID, "tag", []string{"ok"})
	_ = repo.ReplaceItemTaxonomies(ctx, chItem2.ID, "tag", []string{"ok"})

	if code := doReq(aliceLogin.AccessToken, "", http.MethodGet, "/Channels/Items/Latest", "").Code; code != http.StatusOK {
		t.Fatalf("expected 200 empty Channels/Items/Latest, got %d", code)
	}
	if code := doReq(aliceLogin.AccessToken, "", http.MethodGet, "/Channels/Items/Latest?ChannelIds="+ch1ID, "").Code; code != http.StatusOK {
		t.Fatalf("expected 200 single Channels/Items/Latest, got %d", code)
	}
	if code := doReq(aliceLogin.AccessToken, "", http.MethodGet, "/Channels/Items/Latest?ChannelIds="+ch1ID+","+ch2ID+"&StartIndex=1&Limit=1", "").Code; code != http.StatusOK {
		t.Fatalf("expected 200 multi Channels/Items/Latest, got %d", code)
	}
	if code := doReq(aliceLogin.AccessToken, "", http.MethodGet, "/Channels/Items/Latest?ChannelIds="+ch1ID+","+ch2ID+"&StartIndex=50", "").Code; code != http.StatusOK {
		t.Fatalf("expected 200 high StartIndex Channels/Items/Latest, got %d", code)
	}

	// 9. RenameVirtualFolder & ItemFile download branches + pure JSON/image helpers
	if code := doReq(admLogin.AccessToken, "", http.MethodPost, "/Library/VirtualFolders/Name", `{}`).Code; code != http.StatusBadRequest {
		t.Fatalf("expected 400 on missing rename params, got %d", code)
	}
	if code := doReq(admLogin.AccessToken, "", http.MethodPost, "/Library/VirtualFolders/Name?id="+guid.New()+"&newName=Ghost", "").Code; code != http.StatusNoContent {
		t.Fatalf("expected 204 renaming unknown id, got %d", code)
	}
	if code := doReq(admLogin.AccessToken, "", http.MethodPost, "/Library/VirtualFolders/Name?name=GhostOld&newName=GhostNew", "").Code; code != http.StatusNoContent {
		t.Fatalf("expected 204 renaming unknown name, got %d", code)
	}
	if code := doReq(admLogin.AccessToken, "", http.MethodPost, "/Library/VirtualFolders/Name?name=Media&newName=MediaRenamed", "").Code; code != http.StatusNoContent {
		t.Fatalf("expected 204 renaming existing library, got %d", code)
	}

	folderRec := &sqlite.ItemRecord{ID: guid.New(), LibraryID: lib.ID, ParentID: lib.ID, Type: "Folder", IsFolder: true, Name: "Dir", Path: mediaRoot}
	strmRec := &sqlite.ItemRecord{ID: guid.New(), LibraryID: lib.ID, ParentID: lib.ID, Type: "Movie", IsStrm: true, Name: "Strm", Path: filepath.Join(mediaRoot, "a.strm")}
	missingFileRec := &sqlite.ItemRecord{ID: guid.New(), LibraryID: lib.ID, ParentID: lib.ID, Type: "Movie", Name: "Gone", Path: filepath.Join(mediaRoot, "missing.mp4")}
	for _, it := range []*sqlite.ItemRecord{folderRec, strmRec, missingFileRec} {
		_ = repo.UpsertItem(ctx, it)
	}
	if code := doReq(admLogin.AccessToken, "", http.MethodGet, "/Items/"+folderRec.ID+"/File", "").Code; code != http.StatusBadRequest {
		t.Fatalf("expected 400 downloading folder, got %d", code)
	}
	if code := doReq(admLogin.AccessToken, "", http.MethodGet, "/Items/"+strmRec.ID+"/File", "").Code; code != http.StatusForbidden {
		t.Fatalf("expected 403 downloading .strm, got %d", code)
	}
	if code := doReq(admLogin.AccessToken, "", http.MethodGet, "/Items/"+missingFileRec.ID+"/File", "").Code; code != http.StatusNotFound {
		t.Fatalf("expected 404 downloading missing file, got %d", code)
	}
	if code := doReq(admLogin.AccessToken, "", http.MethodGet, "/Items/"+mov.ID+"/File", "").Code; code != http.StatusOK {
		t.Fatalf("expected 200 downloading valid media file, got %d", code)
	}

	// Pure JSON & raster image helpers
	if _, ok := rawJSONKeyPresent(nil, "Name"); ok {
		t.Fatal("expected false on empty raw map")
	}
	rawMap := map[string]json.RawMessage{"overview": json.RawMessage(`"hi"`)}
	if _, ok := rawJSONKeyPresent(rawMap, "Overview"); !ok {
		t.Fatal("expected case-insensitive rawJSONKeyPresent match")
	}
	if _, ok := rawJSONKeyPresent(rawMap, "Missing"); ok {
		t.Fatal("expected false on missing key")
	}
	if _, ok := safeRasterImageType(nil); ok {
		t.Fatal("expected empty bytes rejected")
	}
	if _, ok := safeRasterImageType([]byte("<html>svg</html>")); ok {
		t.Fatal("expected HTML rejected")
	}
	if ct, ok := safeRasterImageType([]byte("GIF89a\x01\x00\x01\x00\x00\x00\x00;")); !ok || ct != "image/gif" {
		t.Fatalf("expected image/gif, got (%q, %v)", ct, ok)
	}
	managedImg := filepath.Join(dataDir, "metadata", "images", "old.jpg")
	_ = os.MkdirAll(filepath.Dir(managedImg), 0o755)
	_ = os.WriteFile(managedImg, []byte("img"), 0o644)
	h.cleanupReplacedImageFile("", "")
	h.cleanupReplacedImageFile(managedImg, filepath.Join(dataDir, "metadata", "images", "new.jpg"))
	if _, stErr := os.Stat(managedImg); !os.IsNotExist(stErr) {
		t.Fatal("expected managed old image removed")
	}
	if err := (&Handler{}).saveLibraryPaths(httptest.NewRequest(http.MethodPost, "/", nil), lib.ID, []string{mediaRoot}); err != nil {
		t.Fatalf("expected nil store saveLibraryPaths to return nil, got %v", err)
	}
	_ = adminUser
}
