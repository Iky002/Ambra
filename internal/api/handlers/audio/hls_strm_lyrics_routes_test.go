package audio

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ambra-media/ambra/internal/api/dto"
	"github.com/ambra-media/ambra/internal/api/httpx"
	"github.com/ambra-media/ambra/internal/api/middleware"
	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/core/transcode"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/plugins"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestCovV0112_AudioPureHelpersAndRBACBranches(t *testing.T) {
	// audioHLSCodecAttr
	codecMap := map[string]string{
		"mp3":  "mp4a.40.34",
		"opus": "opus",
		"flac": "fLaC",
		"ac3":  "ac-3",
		"eac3": "ec-3",
		"aac":  "mp4a.40.2",
	}
	for k, want := range codecMap {
		if got := audioHLSCodecAttr(k); got != want {
			t.Fatalf("audioHLSCodecAttr(%q) = %q, want %q", k, got, want)
		}
	}

	// resolveAudioRequestToken & preserveAudioQueryWithToken
	r1 := httptest.NewRequest(http.MethodGet, "/Audio/1/master.m3u8?api_key=qtok", nil)
	if got := resolveAudioRequestToken(r1, httpx.ParseQuery(r1)); got != "qtok" {
		t.Fatalf("expected qtok, got %q", got)
	}
	r2 := httptest.NewRequest(http.MethodGet, "/Audio/1/master.m3u8", nil)
	r2 = r2.WithContext(middleware.WithAuthContext(r2.Context(), &middleware.AuthContext{Token: "ctxtok"}))
	if got := resolveAudioRequestToken(r2, httpx.ParseQuery(r2)); got != "ctxtok" {
		t.Fatalf("expected ctxtok, got %q", got)
	}
	r3 := httptest.NewRequest(http.MethodGet, "/Audio/1/master.m3u8", nil)
	r3.Header.Set("X-MediaBrowser-Token", "mbtok")
	if got := resolveAudioRequestToken(r3, httpx.ParseQuery(r3)); got != "mbtok" {
		t.Fatalf("expected mbtok, got %q", got)
	}
	r4 := httptest.NewRequest(http.MethodGet, "/Audio/1/master.m3u8", nil)
	r4.Header.Set("Authorization", "Bearer beartok")
	if got := resolveAudioRequestToken(r4, httpx.ParseQuery(r4)); got != "beartok" {
		t.Fatalf("expected beartok, got %q", got)
	}
	r5 := httptest.NewRequest(http.MethodGet, "/Audio/1/master.m3u8", nil)
	r5.Header.Set("X-Emby-Authorization", `MediaBrowser Client="Web", Token="embytok"`)
	if got := resolveAudioRequestToken(r5, httpx.ParseQuery(r5)); got != "embytok" {
		t.Fatalf("expected embytok, got %q", got)
	}
	r6 := httptest.NewRequest(http.MethodGet, "/Audio/1/master.m3u8", nil)
	if got := resolveAudioRequestToken(r6, httpx.ParseQuery(r6)); got != "" {
		t.Fatalf("expected empty token, got %q", got)
	}

	preserved := preserveAudioQueryWithToken(url.Values{"api_key": []string{"existing"}}, "newtok")
	if preserved.Get("api_key") != "existing" {
		t.Fatalf("expected existing api_key preserved, got %q", preserved.Get("api_key"))
	}

	// matchesBlockUnratedAudio & isContainerAllowed & isLocalOrSubnetAudioRequest
	if !matchesBlockUnratedAudio("MusicAlbum", "", []string{"", "Music"}) {
		t.Fatal("expected Music rule to match MusicAlbum")
	}
	if !matchesBlockUnratedAudio("Audio", "Audio", []string{"All"}) {
		t.Fatal("expected All rule to match Audio")
	}
	if matchesBlockUnratedAudio("Audio", "Audio", []string{"Movie"}) {
		t.Fatal("expected Movie rule not to match Audio")
	}
	if !isContainerAllowed(".flac", nil) || !isContainerAllowed(".flac", []string{"*"}) || isContainerAllowed(".ogg", []string{"mp3", "flac"}) {
		t.Fatal("unexpected isContainerAllowed results")
	}

	lanR := httptest.NewRequest(http.MethodGet, "/", nil)
	lanR.RemoteAddr = "127.0.0.1:9000"
	if !isLocalOrSubnetAudioRequest(lanR, nil) {
		t.Fatal("expected loopback local")
	}
	badR := httptest.NewRequest(http.MethodGet, "/", nil)
	badR.RemoteAddr = "invalid-addr"
	if isLocalOrSubnetAudioRequest(badR, []string{"8.8.8.0/24"}) {
		t.Fatal("expected invalid addr false")
	}
	pubR := httptest.NewRequest(http.MethodGet, "/", nil)
	pubR.RemoteAddr = "8.8.8.8:9000"
	if isLocalOrSubnetAudioRequest(pubR, nil) {
		t.Fatal("expected public false without subnets")
	}
	if !isLocalOrSubnetAudioRequest(pubR, []string{"", "8.8.8.0/24"}) {
		t.Fatal("expected CIDR match true")
	}
	exactR := httptest.NewRequest(http.MethodGet, "/", nil)
	exactR.RemoteAddr = "9.9.9.9:9000"
	if !isLocalOrSubnetAudioRequest(exactR, []string{"9.9.9.9"}) {
		t.Fatal("expected exact IP match true")
	}
	missR := httptest.NewRequest(http.MethodGet, "/", nil)
	missR.RemoteAddr = "1.1.1.1:9000"
	if isLocalOrSubnetAudioRequest(missR, []string{"8.8.8.0/24", "9.9.9.9"}) {
		t.Fatal("expected non-matching public IP false")
	}

	// CanAccessLibrary & canAccessItem fallback branches (nil repo / nil userRepo)
	tmp := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmp, "audio_cov.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	ctx := context.Background()
	libRepo := sqlite.NewLibraryRepo(store)
	userRepo := sqlite.NewUserRepo(store)
	lib, _ := libRepo.CreateLibrary(ctx, "Music", "music", []string{tmp})
	otherLib, _ := libRepo.CreateLibrary(ctx, "Other", "music", []string{tmp})

	adminU := &sqlite.UserRecord{ID: guid.New(), Name: "adm", IsAdmin: true, Policy: sqlite.DefaultUserPolicy(true)}
	scopedU := &sqlite.UserRecord{ID: guid.New(), Name: "sc", Policy: dto.UserPolicy{EnableAllFolders: false, EnabledFolders: []string{lib.ID}}}
	allU := &sqlite.UserRecord{ID: guid.New(), Name: "all", Policy: dto.UserPolicy{EnableAllFolders: true}}
	for _, u := range []*sqlite.UserRecord{adminU, scopedU, allU} {
		_ = userRepo.CreateUser(ctx, u)
	}

	hNoRepo := &Handler{userRepo: userRepo}
	if !hNoRepo.CanAccessLibrary(ctx, "", "") {
		t.Fatal("expected empty libraryID allowed")
	}
	if hNoRepo.CanAccessLibrary(ctx, "", lib.ID) || hNoRepo.CanAccessLibrary(ctx, guid.New(), lib.ID) {
		t.Fatal("expected empty/unknown user denied")
	}
	if !hNoRepo.CanAccessLibrary(ctx, adminU.ID, lib.ID) || !hNoRepo.CanAccessLibrary(ctx, allU.ID, lib.ID) {
		t.Fatal("expected admin and EnableAllFolders allowed")
	}
	if !hNoRepo.CanAccessLibrary(ctx, scopedU.ID, lib.ID) || hNoRepo.CanAccessLibrary(ctx, scopedU.ID, otherLib.ID) {
		t.Fatal("unexpected scoped CanAccessLibrary result")
	}
	if !hNoRepo.canAccessItem(httptest.NewRequest(http.MethodGet, "/", nil), guid.New()) {
		t.Fatal("expected nil repo canAccessItem true")
	}

	song := &sqlite.ItemRecord{ID: guid.New(), LibraryID: lib.ID, ParentID: lib.ID, Type: "Audio", MediaType: "Audio", Name: "Track", OfficialRating: "G"}
	otherSong := &sqlite.ItemRecord{ID: guid.New(), LibraryID: otherLib.ID, ParentID: otherLib.ID, Type: "Audio", MediaType: "Audio", Name: "Other", OfficialRating: "G"}
	_ = libRepo.UpsertItem(ctx, song)
	_ = libRepo.UpsertItem(ctx, otherSong)

	hNoUserRepo := &Handler{repo: libRepo}
	scopedReq := httptest.NewRequest(http.MethodGet, "/", nil)
	scopedReq = scopedReq.WithContext(middleware.WithAuthContext(ctx, &middleware.AuthContext{User: scopedU}))
	if !hNoUserRepo.canAccessItem(scopedReq, guid.New()) {
		t.Fatal("expected missing item true in canAccessItem")
	}
	if !hNoUserRepo.canAccessItem(scopedReq, song.ID) || hNoUserRepo.canAccessItem(scopedReq, otherSong.ID) {
		t.Fatal("unexpected hNoUserRepo canAccessItem result")
	}

	// serveAudioFile missing / directory / valid
	recMissing := httptest.NewRecorder()
	serveAudioFile(recMissing, httptest.NewRequest(http.MethodGet, "/", nil), filepath.Join(tmp, "nope.mp3"))
	if recMissing.Code != http.StatusNotFound {
		t.Fatalf("expected 404 on missing audio file, got %d", recMissing.Code)
	}
	recDir := httptest.NewRecorder()
	serveAudioFile(recDir, httptest.NewRequest(http.MethodGet, "/", nil), tmp)
	if recDir.Code != http.StatusNotFound {
		t.Fatalf("expected 404 on directory audio path, got %d", recDir.Code)
	}
}

func TestCovV0112_AudioHLSSegmentsStrmAndLyricsRoutes(t *testing.T) {
	tmp := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmp, "audio_routes_cov.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	ctx := context.Background()
	libRepo := sqlite.NewLibraryRepo(store)
	userRepo := sqlite.NewUserRepo(store)
	cfg := config.Default(tmp)
	cfg.FFmpegPath = "/nonexistent/ffmpeg-cov"
	cfg.RemoteClientBitrateLimit = 96_000
	cfg.SimultaneousStreamLimit = 2

	mgr := transcode.NewManager(cfg, transcode.WithCapabilities(transcode.SoftwareOnlyCapabilities()))
	defer mgr.Close()

	lib, _ := libRepo.CreateLibrary(ctx, "Music", "music", []string{tmp})
	mp3Path := filepath.Join(tmp, "track1.mp3")
	lrcPath := filepath.Join(tmp, "track1.lrc")
	strmPath := filepath.Join(tmp, "remote.strm")
	plainPath := filepath.Join(tmp, "nolyrics.mp3")
	_ = os.WriteFile(mp3Path, []byte("ID3-FAKE"), 0o644)
	_ = os.WriteFile(lrcPath, []byte("[00:01.00]Hello world\n"), 0o644)
	_ = os.WriteFile(strmPath, []byte("https://cdn.example.test/live.aac\n"), 0o644)
	_ = os.WriteFile(plainPath, []byte("plain"), 0o644)

	songLRC := &sqlite.ItemRecord{ID: guid.New(), LibraryID: lib.ID, ParentID: lib.ID, Type: "Audio", MediaType: "Audio", Name: "With LRC", OfficialRating: "G", Path: mp3Path}
	songStrm := &sqlite.ItemRecord{ID: guid.New(), LibraryID: lib.ID, ParentID: lib.ID, Type: "Audio", MediaType: "Audio", Name: "Strm Song", OfficialRating: "G", Path: strmPath, IsStrm: true}
	songPlain := &sqlite.ItemRecord{ID: guid.New(), LibraryID: lib.ID, ParentID: lib.ID, Type: "Audio", MediaType: "Audio", Name: "No Lyrics", OfficialRating: "G", Path: plainPath}
	for _, it := range []*sqlite.ItemRecord{songLRC, songStrm, songPlain} {
		_ = libRepo.UpsertItem(ctx, it)
	}

	noTranscodePol := sqlite.DefaultUserPolicy(false)
	noTranscodePol.EnableAudioPlaybackTranscoding = false
	noTCUser := &sqlite.UserRecord{ID: guid.New(), Name: "notc", Policy: noTranscodePol, Config: sqlite.DefaultUserConfig()}
	viewer := &sqlite.UserRecord{ID: guid.New(), Name: "viewer", Policy: sqlite.DefaultUserPolicy(false), Config: sqlite.DefaultUserConfig()}
	_ = userRepo.CreateUser(ctx, noTCUser)
	_ = userRepo.CreateUser(ctx, viewer)

	resolvedURL := "https://unresolvable.invalid/live.aac"
	h := New(libRepo, cfg)
	h.SetUserRepo(userRepo)
	h.SetStreamResolverWithHeaders(func(_ context.Context, _ string) (*plugins.StreamResolveResult, error) {
		return &plugins.StreamResolveResult{
			URL:     resolvedURL,
			Headers: map[string]string{"X-Stream": "1"},
		}, nil
	})
	mux := http.NewServeMux()
	h.RegisterRoutes(mux, AuthFuncs{})

	doAs := func(u *sqlite.UserRecord, remoteAddr, method, target string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, target, nil)
		if remoteAddr != "" {
			req.RemoteAddr = remoteAddr
		}
		req = req.WithContext(middleware.WithAuthContext(req.Context(), &middleware.AuthContext{User: u, Token: "tok1"}))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}

	// Segment when transcodeMgr == nil -> 404
	if code := doAs(viewer, "", http.MethodGet, "/Audio/"+songLRC.ID+"/hls1/main/0.ts").Code; code != http.StatusNotFound {
		t.Fatalf("expected 404 when transcodeMgr is nil, got %d", code)
	}
	h.SetTranscodeManager(mgr)

	// EnableAudioPlaybackTranscoding=false -> 403 on playlist & segment
	if code := doAs(noTCUser, "", http.MethodGet, "/Audio/"+songLRC.ID+"/master.m3u8").Code; code != http.StatusForbidden {
		t.Fatalf("expected 403 on playlist when audio transcoding disabled, got %d", code)
	}
	if code := doAs(noTCUser, "", http.MethodGet, "/Audio/"+songLRC.ID+"/hls1/main/0.ts").Code; code != http.StatusForbidden {
		t.Fatalf("expected 403 on segment when audio transcoding disabled, got %d", code)
	}

	// Unresolvable .strm -> 502 on playlist & segment
	if code := doAs(viewer, "", http.MethodGet, "/Audio/"+songStrm.ID+"/master.m3u8").Code; code != http.StatusBadGateway {
		t.Fatalf("expected 502 on unresolvable .strm playlist, got %d", code)
	}
	if code := doAs(viewer, "", http.MethodGet, "/Audio/"+songStrm.ID+"/hls1/main/0.ts").Code; code != http.StatusBadGateway {
		t.Fatalf("expected 502 on unresolvable .strm segment, got %d", code)
	}

	// Valid public IP .strm master.m3u8 with remote bitrate clamping & default fallback params
	resolvedURL = "http://93.184.216.34/live.aac"
	strmMaster := doAs(viewer, "8.8.4.4:9999", http.MethodGet, "/Audio/"+songStrm.ID+"/master.m3u8?AudioBitrate=-1&AudioChannels=-1&SegmentLength=1&AudioCodec=wmav2")
	if strmMaster.Code != http.StatusOK || !strings.Contains(strmMaster.Body.String(), "BANDWIDTH=96000") {
		t.Fatalf("expected 200 clamped BANDWIDTH=96000 on .strm master.m3u8, got %d:\n%s", strmMaster.Code, strmMaster.Body.String())
	}

	// Segment validation, init.mp4 HEAD -> audio/mp4, and GET segment execution
	if code := doAs(viewer, "", http.MethodGet, "/Audio/"+songLRC.ID+"/hls1/main/nodot").Code; code != http.StatusBadRequest {
		t.Fatalf("expected 400 on nodot segment, got %d", code)
	}
	if code := doAs(viewer, "", http.MethodGet, "/Audio/"+songLRC.ID+"/hls1/main/seg_-1.ts").Code; code != http.StatusBadRequest {
		t.Fatalf("expected 400 on negative segment index, got %d", code)
	}
	if r := doAs(viewer, "", http.MethodHead, "/Audio/"+songLRC.ID+"/hls1/main/init.mp4"); r.Code != http.StatusOK || r.Header().Get("Content-Type") != "audio/mp4" {
		t.Fatalf("expected 200 audio/mp4 on HEAD init.mp4, got %d %q", r.Code, r.Header().Get("Content-Type"))
	}
	_ = doAs(viewer, "", http.MethodGet, "/Audio/"+songLRC.ID+"/hls1/main/init.mp4")
	_ = doAs(viewer, "", http.MethodGet, "/Audio/"+songLRC.ID+"/hls1/main/0.ts")

	// Lyrics: sidecar .lrc -> 200, missing lyrics -> 404, missing item -> 404
	if code := doAs(viewer, "", http.MethodGet, "/Audio/"+songLRC.ID+"/Lyrics").Code; code != http.StatusOK {
		t.Fatalf("expected 200 on .lrc sidecar lyrics, got %d", code)
	}
	if code := doAs(viewer, "", http.MethodGet, "/Audio/"+songPlain.ID+"/Lyrics").Code; code != http.StatusNotFound {
		t.Fatalf("expected 404 when no lyrics exist, got %d", code)
	}
	if code := doAs(viewer, "", http.MethodGet, "/Audio/"+guid.New()+"/Lyrics").Code; code != http.StatusNotFound {
		t.Fatalf("expected 404 on missing item lyrics, got %d", code)
	}
}
