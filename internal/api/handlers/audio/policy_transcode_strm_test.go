package audio

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestAudioParentalControls(t *testing.T) {
	tmp := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmp, "audio_parental.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	ctx := context.Background()
	repo := sqlite.NewLibraryRepo(store)
	userRepo := sqlite.NewUserRepo(store)
	cfg := config.Default(tmp)

	lib, _ := repo.CreateLibrary(ctx, "Music", "music", []string{tmp})
	mp3Explicit := filepath.Join(tmp, "explicit.mp3")
	mp3Clean := filepath.Join(tmp, "clean.mp3")
	_ = os.WriteFile(mp3Explicit, []byte("ID3explicit"), 0o644)
	_ = os.WriteFile(mp3Clean, []byte("ID3clean"), 0o644)
	_ = os.WriteFile(filepath.Join(tmp, "explicit.lrc"), []byte("[00:01.00]Explicit\n"), 0o644)
	_ = os.WriteFile(filepath.Join(tmp, "clean.lrc"), []byte("[00:01.00]Clean\n"), 0o644)

	explicitSong := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: lib.ID, ParentID: lib.ID,
		Type: "Audio", MediaType: "Audio", Name: "Explicit Song", SortName: "explicit",
		Path: mp3Explicit, OfficialRating: "R",
	}
	taggedSong := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: lib.ID, ParentID: lib.ID,
		Type: "Audio", MediaType: "Audio", Name: "Tagged Song", SortName: "tagged",
		Path: mp3Explicit, OfficialRating: "G",
	}
	cleanSong := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: lib.ID, ParentID: lib.ID,
		Type: "Audio", MediaType: "Audio", Name: "Clean Song", SortName: "clean",
		Path: mp3Clean, OfficialRating: "G",
	}
	_ = repo.UpsertItem(ctx, explicitSong)
	_ = repo.UpsertItem(ctx, taggedSong)
	_ = repo.UpsertItem(ctx, cleanSong)
	_ = repo.ReplaceItemTaxonomies(ctx, taggedSong.ID, "tag", []string{"explicit"})

	maxRating := 5
	kidPolicy := sqlite.DefaultUserPolicy(false)
	kidPolicy.MaxParentalRating = &maxRating
	kidPolicy.BlockedTags = []string{"explicit"}
	kidUser := &sqlite.UserRecord{ID: guid.New(), Name: "kid", Policy: kidPolicy, Config: sqlite.DefaultUserConfig()}
	adminUser := &sqlite.UserRecord{ID: guid.New(), Name: "admin", IsAdmin: true, Policy: sqlite.DefaultUserPolicy(true), Config: sqlite.DefaultUserConfig()}
	_ = userRepo.CreateUser(ctx, kidUser)
	_ = userRepo.CreateUser(ctx, adminUser)

	currentCaller := kidUser.ID
	mux := http.NewServeMux()
	h := New(repo, cfg)
	h.SetUserRepo(userRepo)
	h.RegisterRoutes(mux, AuthFuncs{
		UserIDFromCtx: func(context.Context) string { return currentCaller },
	})

	// Kid blocked on rating-blocked and tag-blocked songs across stream, universal, Lyrics
	for _, blockedID := range []string{explicitSong.ID, taggedSong.ID} {
		for _, p := range []string{
			"/Audio/" + blockedID + "/stream",
			"/Audio/" + blockedID + "/universal?Container=mp3",
			"/Audio/" + blockedID + "/Lyrics",
		} {
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, p, nil))
			if rec.Code != http.StatusForbidden {
				t.Fatalf("kid GET %s: got %d, want 403", p, rec.Code)
			}
		}
	}

	// Kid allowed on clean song
	for _, p := range []string{
		"/Audio/" + cleanSong.ID + "/stream",
		"/Audio/" + cleanSong.ID + "/universal?Container=mp3",
		"/Audio/" + cleanSong.ID + "/Lyrics",
	} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, p, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("kid GET %s: got %d, want 200", p, rec.Code)
		}
	}

	// Admin allowed on explicit song
	currentCaller = adminUser.ID
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/Audio/"+explicitSong.ID+"/stream", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("admin GET explicit stream: got %d, want 200", rec.Code)
	}
}

func TestAudioTranscodeArgsAndMP4Muxing(t *testing.T) {
	// 1. m4a/mp4 target returns audio/mp4 + mp4 + aac
	for _, c := range []string{"m4a", "mp4"} {
		ct, muxFmt, enc := resolveAudioTranscodeTarget(c, "")
		if ct != "audio/mp4" || muxFmt != "mp4" || enc != "aac" {
			t.Fatalf("resolveAudioTranscodeTarget(%q) = (%q, %q, %q), want (audio/mp4, mp4, aac)", c, ct, muxFmt, enc)
		}
	}

	// 2. buildAudioFFmpegArgs places -ss before -i, honors -ac/-ar, and adds -movflags for mp4
	args := buildAudioFFmpegArgs("/music/track.flac", "mp4", "aac", 192000, 125_000_000, 2, 44100)
	joined := strings.Join(args, " ")
	ssIdx := strings.Index(joined, "-ss 12.500")
	iIdx := strings.Index(joined, "-i /music/track.flac")
	if ssIdx == -1 || iIdx == -1 || ssIdx > iIdx {
		t.Fatalf("expected -ss 12.500 before -i, got args: %s", joined)
	}
	for _, want := range []string{"-ac 2", "-ar 44100", "-movflags frag_keyframe+empty_moov", "-f mp4 pipe:1"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("expected %q in ffmpeg args, got: %s", want, joined)
		}
	}

	// 3. End-to-end mock ffmpeg captures query params through handleUniversal
	if runtime.GOOS == "windows" {
		return
	}
	tmp := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmp, "audio_args.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	ctx := context.Background()
	repo := sqlite.NewLibraryRepo(store)
	flacPath := filepath.Join(tmp, "song.flac")
	_ = os.WriteFile(flacPath, []byte("fake-flac"), 0o644)
	itemID := guid.New()
	_ = repo.UpsertItem(ctx, &sqlite.ItemRecord{ID: itemID, LibraryID: "lib1", Type: "Audio", Name: "Track", Path: flacPath})

	mockFF := filepath.Join(tmp, "echo_args.sh")
	_ = os.WriteFile(mockFF, []byte("#!/bin/sh\nprintf '%s' \"$*\"\n"), 0o755)
	cfg := config.Default(tmp)
	cfg.FFmpegPath = mockFF

	mux := http.NewServeMux()
	New(repo, cfg).RegisterRoutes(mux, AuthFuncs{})

	rec := httptest.NewRecorder()
	reqURL := "/Audio/" + itemID + "/universal.m4a?Container=mp3&TranscodingContainer=m4a&StartTimeTicks=300000000&AudioChannels=2&AudioSampleRate=48000&AudioBitrate=256000"
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, reqURL, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("universal.m4a status=%d body=%q", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "audio/mp4" {
		t.Fatalf("expected Content-Type audio/mp4, got %q", ct)
	}
	gotArgs := rec.Body.String()
	for _, want := range []string{"-ss 30.000 -i " + flacPath, "-ac 2", "-ar 48000", "-movflags frag_keyframe+empty_moov", "-f mp4 pipe:1"} {
		if !strings.Contains(gotArgs, want) {
			t.Fatalf("expected %q in captured ffmpeg args, got: %s", want, gotArgs)
		}
	}
}

func TestAudioStrmResolutionAndFFmpegClamp(t *testing.T) {
	// 1. FFmpeg parameter clamping
	args := buildAudioFFmpegArgs("/music/track.flac", "mp3", "libmp3lame", 99_000_000, 0, 64, 999_999)
	joined := strings.Join(args, " ")
	for _, want := range []string{"-b:a 1536000", "-ac 8", "-ar 192000"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("expected clamped arg %q, got: %s", want, joined)
		}
	}

	// 2. STRM audio resolution via SetStreamResolver + safe HTTP proxy
	tmp := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmp, "audio_strm.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/no-ct.flac" {
			w.Header().Del("Content-Type")
			w.WriteHeader(http.StatusOK)
			if r.Method != http.MethodHead {
				_, _ = w.Write([]byte("FLAC-data"))
			}
			return
		}
		w.Header().Set("Content-Type", "audio/mpeg")
		w.Header().Set("Accept-Ranges", "bytes")
		w.WriteHeader(http.StatusOK)
		if r.Method != http.MethodHead {
			_, _ = w.Write([]byte("ID3-remote-mp3-data"))
		}
	}))
	defer upstream.Close()

	strmPath := filepath.Join(tmp, "remote.strm")
	_ = os.WriteFile(strmPath, []byte("http://169.254.169.254/latest/meta-data/\n"), 0o644)

	ctx := context.Background()
	repo := sqlite.NewLibraryRepo(store)
	itemID := guid.New()
	_ = repo.UpsertItem(ctx, &sqlite.ItemRecord{
		ID: itemID, LibraryID: "lib1", Type: "Audio", MediaType: "Audio", Name: "Remote Track", Path: strmPath,
	})

	cfg := config.Default(tmp)
	h := New(repo, cfg)
	h.SetProxyClient(upstream.Client())
	mux := http.NewServeMux()
	h.RegisterRoutes(mux, AuthFuncs{})

	// Without custom resolver, link-local metadata IP in .strm is blocked with 502 (direct & universal transcode)
	recBlocked := httptest.NewRecorder()
	mux.ServeHTTP(recBlocked, httptest.NewRequest(http.MethodGet, "/Audio/"+itemID+"/stream", nil))
	if recBlocked.Code != http.StatusBadGateway {
		t.Fatalf("expected 502 for blocked metadata strm url, got %d body=%q", recBlocked.Code, recBlocked.Body.String())
	}
	recBlockedTranscode := httptest.NewRecorder()
	mux.ServeHTTP(recBlockedTranscode, httptest.NewRequest(http.MethodGet, "/Audio/"+itemID+"/universal.mp3?Container=flac", nil))
	if recBlockedTranscode.Code != http.StatusBadGateway {
		t.Fatalf("expected 502 for blocked metadata strm transcode, got %d", recBlockedTranscode.Code)
	}

	// Empty/invalid strm file -> 502
	emptyStrm := filepath.Join(tmp, "empty.strm")
	_ = os.WriteFile(emptyStrm, []byte("# comment only\n"), 0o644)
	if _, err := h.resolveStrmAudio(ctx, emptyStrm); err == nil {
		t.Fatalf("expected error for empty strm")
	}
	if got := readFirstLineAudioURL(filepath.Join(tmp, "nonexistent.strm")); got != "" {
		t.Fatalf("expected empty url for missing strm, got %q", got)
	}

	// Custom resolver returning error -> 502
	h.SetStreamResolver(func(_ context.Context, _ string) (string, error) {
		return "", os.ErrNotExist
	})
	recResErr := httptest.NewRecorder()
	mux.ServeHTTP(recResErr, httptest.NewRequest(http.MethodGet, "/Audio/"+itemID+"/stream", nil))
	if recResErr.Code != http.StatusBadGateway {
		t.Fatalf("expected 502 on resolver error, got %d", recResErr.Code)
	}
	recUnivResErr := httptest.NewRecorder()
	mux.ServeHTTP(recUnivResErr, httptest.NewRequest(http.MethodGet, "/Audio/"+itemID+"/universal", nil))
	if recUnivResErr.Code != http.StatusBadGateway {
		t.Fatalf("expected 502 on universal resolver error, got %d", recUnivResErr.Code)
	}

	// With SetStreamResolver pointing to upstream test server, direct stream & universal proxy remote audio
	h.SetStreamResolver(func(_ context.Context, rawPath string) (string, error) {
		if rawPath != strmPath {
			t.Errorf("unexpected rawPath %q", rawPath)
		}
		return upstream.URL + "/song.mp3", nil
	})

	reqStream := httptest.NewRequest(http.MethodGet, "/Audio/"+itemID+"/stream", nil)
	reqStream.Header.Set("Range", "bytes=0-10")
	reqStream.Header.Set("If-Range", `"etag1"`)
	recStream := httptest.NewRecorder()
	mux.ServeHTTP(recStream, reqStream)
	if recStream.Code != http.StatusOK || recStream.Body.String() != "ID3-remote-mp3-data" {
		t.Fatalf("expected proxied remote mp3, got %d %q", recStream.Code, recStream.Body.String())
	}

	recUniv := httptest.NewRecorder()
	mux.ServeHTTP(recUniv, httptest.NewRequest(http.MethodGet, "/Audio/"+itemID+"/universal?Container=mp3", nil))
	if recUniv.Code != http.StatusOK || recUniv.Body.String() != "ID3-remote-mp3-data" {
		t.Fatalf("expected universal proxied remote mp3, got %d %q", recUniv.Code, recUniv.Body.String())
	}

	// Upstream without Content-Type falls back to audioContentType(url.Path), and HEAD omits body
	h.SetStreamResolver(func(_ context.Context, _ string) (string, error) {
		return upstream.URL + "/no-ct.flac", nil
	})
	recHead := httptest.NewRecorder()
	mux.ServeHTTP(recHead, httptest.NewRequest(http.MethodHead, "/Audio/"+itemID+"/stream", nil))
	if recHead.Code != http.StatusOK || recHead.Header().Get("Content-Type") != "audio/flac" || recHead.Body.Len() != 0 {
		t.Fatalf("expected HEAD 200 audio/flac empty body, got %d ct=%q len=%d", recHead.Code, recHead.Header().Get("Content-Type"), recHead.Body.Len())
	}

	// Remote HTTP input includes -protocol_whitelist
	remoteArgs := buildAudioFFmpegArgs("https://93.184.216.34/song.flac", "mp3", "libmp3lame", 192000, 0, 2, 44100)
	if !strings.Contains(strings.Join(remoteArgs, " "), "-protocol_whitelist http,https,tcp,tls,crypto") {
		t.Fatalf("expected -protocol_whitelist for remote audio input, got: %v", remoteArgs)
	}

	// Custom resolver returning local file path must be rejected (no os.Open fallback)
	h.SetStreamResolver(func(_ context.Context, _ string) (string, error) {
		return "/etc/passwd", nil
	})
	recLocal := httptest.NewRecorder()
	mux.ServeHTTP(recLocal, httptest.NewRequest(http.MethodGet, "/Audio/"+itemID+"/stream", nil))
	if recLocal.Code != http.StatusBadGateway {
		t.Fatalf("expected 502 when resolver returns local path, got %d", recLocal.Code)
	}

	// Concurrency semaphore exhaustion returns 503
	for i := 0; i < cap(h.transcodeSem); i++ {
		h.transcodeSem <- struct{}{}
	}
	defer func() {
		for len(h.transcodeSem) > 0 {
			<-h.transcodeSem
		}
	}()
	localFlac := filepath.Join(tmp, "busy.flac")
	_ = os.WriteFile(localFlac, []byte("fake"), 0o644)
	busyID := guid.New()
	_ = repo.UpsertItem(ctx, &sqlite.ItemRecord{ID: busyID, LibraryID: "lib1", Type: "Audio", Name: "Busy", Path: localFlac})
	recBusy := httptest.NewRecorder()
	mux.ServeHTTP(recBusy, httptest.NewRequest(http.MethodGet, "/Audio/"+busyID+"/universal.mp3?Container=mp3", nil))
	if recBusy.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 when audio transcode concurrency limit reached, got %d", recBusy.Code)
	}
}
