package audio

import (
	"bytes"
	"context"
	"log/slog"
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

func TestAudioV0107AuditStrm302StartTimeSeekAndPermissionDenied(t *testing.T) {
	tmp := t.TempDir()
	st, err := sqlite.Open(filepath.Join(tmp, "audio_v0107.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "audio/mpeg")
		_, _ = w.Write([]byte("PROXIED_MP3"))
	}))
	defer upstream.Close()

	ctx := context.Background()
	repo := sqlite.NewLibraryRepo(st)
	cfg := config.Default(tmp)

	// Fake ffmpeg binary that echoes its argv so we can verify -ss transcoding on StartTimeTicks > 0
	if runtime.GOOS != "windows" {
		fakeFFmpeg := filepath.Join(tmp, "fake-ffmpeg.sh")
		_ = os.WriteFile(fakeFFmpeg, []byte("#!/bin/sh\necho \"$@\"\n"), 0o755)
		cfg.FFmpegPath = fakeFFmpeg
	}

	mp3Path := filepath.Join(tmp, "track.mp3")
	lrcPath := filepath.Join(tmp, "track.lrc")
	strmPath := filepath.Join(tmp, "remote.strm")
	_ = os.WriteFile(mp3Path, []byte("LOCAL_MP3_BYTES"), 0o644)
	_ = os.WriteFile(lrcPath, []byte("[00:01.00]Hello\n"), 0o644)
	_ = os.WriteFile(strmPath, []byte("https://93.184.216.34/song.mp3\n"), 0o644)

	localID := guid.New()
	strmID := guid.New()
	_ = repo.UpsertItem(ctx, &sqlite.ItemRecord{
		ID: localID, LibraryID: "lib1", Type: "Audio", MediaType: "Audio", Name: "Local Song", Path: mp3Path,
	})
	_ = repo.UpsertItem(ctx, &sqlite.ItemRecord{
		ID: strmID, LibraryID: "lib1", Type: "Audio", MediaType: "Audio", Name: "Remote Song", Path: strmPath, IsStrm: true,
	})

	h := New(repo, cfg)
	h.SetProxyClient(upstream.Client())
	mux := http.NewServeMux()
	h.RegisterRoutes(mux, AuthFuncs{})

	// 1. [M-12] Default .strm with EnableSTRM302=true returns 302 Found + Cache-Control: no-store
	for _, ep := range []string{
		"/Audio/" + strmID + "/stream",
		"/Audio/" + strmID + "/universal?Container=mp3",
	} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, ep, nil))
		if rec.Code != http.StatusFound {
			t.Fatalf("GET %s expected 302 Found, got %d body=%q", ep, rec.Code, rec.Body.String())
		}
		if loc := rec.Header().Get("Location"); loc != "https://93.184.216.34/song.mp3" {
			t.Fatalf("GET %s unexpected Location %q", ep, loc)
		}
		if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
			t.Fatalf("GET %s expected Cache-Control: no-store, got %q", ep, cc)
		}
	}

	// SetStreamResolverFull with is302=true redirects, and ?proxy=1 forces proxy instead of 302
	h.SetStreamResolverFull(func(_ context.Context, _ string) (string, bool, error) {
		return upstream.URL + "/song.mp3", true, nil
	})
	rec302 := httptest.NewRecorder()
	mux.ServeHTTP(rec302, httptest.NewRequest(http.MethodGet, "/Audio/"+strmID+"/stream", nil))
	if rec302.Code != http.StatusFound || rec302.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("SetStreamResolverFull(is302=true) expected 302 no-store, got %d cc=%q", rec302.Code, rec302.Header().Get("Cache-Control"))
	}
	recProxy := httptest.NewRecorder()
	mux.ServeHTTP(recProxy, httptest.NewRequest(http.MethodGet, "/Audio/"+strmID+"/stream?proxy=1", nil))
	if recProxy.Code != http.StatusOK || recProxy.Body.String() != "PROXIED_MP3" {
		t.Fatalf("?proxy=1 expected 200 PROXIED_MP3, got %d %q", recProxy.Code, recProxy.Body.String())
	}
	recUnivProxy := httptest.NewRecorder()
	mux.ServeHTTP(recUnivProxy, httptest.NewRequest(http.MethodGet, "/Audio/"+strmID+"/universal?Container=mp3&proxy=1", nil))
	if recUnivProxy.Code != http.StatusOK || recUnivProxy.Body.String() != "PROXIED_MP3" {
		t.Fatalf("universal ?proxy=1 expected 200 PROXIED_MP3, got %d %q", recUnivProxy.Code, recUnivProxy.Body.String())
	}
	// ResolverFull error -> 502
	h.SetStreamResolverFull(func(_ context.Context, _ string) (string, bool, error) {
		return "", false, os.ErrNotExist
	})
	recErr := httptest.NewRecorder()
	mux.ServeHTTP(recErr, httptest.NewRequest(http.MethodGet, "/Audio/"+strmID+"/stream", nil))
	if recErr.Code != http.StatusBadGateway {
		t.Fatalf("SetStreamResolverFull error expected 502, got %d", recErr.Code)
	}
	h.SetStreamResolverFull(nil)

	// 2. [M-12] handleUniversal with StartTimeTicks > 0 skips direct play and invokes FFmpeg -ss
	if runtime.GOOS != "windows" {
		recSeek := httptest.NewRecorder()
		mux.ServeHTTP(recSeek, httptest.NewRequest(http.MethodGet, "/Audio/"+localID+"/universal?Container=mp3&StartTimeTicks=150000000", nil))
		if recSeek.Code != http.StatusOK {
			t.Fatalf("universal StartTimeTicks expected 200, got %d body=%q", recSeek.Code, recSeek.Body.String())
		}
		if !strings.Contains(recSeek.Body.String(), "-ss 15.000") {
			t.Fatalf("expected FFmpeg transcode with -ss 15.000 when StartTimeTicks > 0, got %q", recSeek.Body.String())
		}
	}

	// 3. [Task 1 / M-7] Permission denied on audio stream, transcode, and lyrics
	if runtime.GOOS != "windows" && os.Geteuid() != 0 {
		var logBuf bytes.Buffer
		oldLogger := slog.Default()
		slog.SetDefault(slog.New(slog.NewTextHandler(&logBuf, nil)))
		t.Cleanup(func() { slog.SetDefault(oldLogger) })

		_ = os.Chmod(mp3Path, 0o000)
		_ = os.Chmod(lrcPath, 0o000)
		t.Cleanup(func() {
			_ = os.Chmod(mp3Path, 0o644)
			_ = os.Chmod(lrcPath, 0o644)
		})

		for _, tc := range []struct {
			name string
			url  string
			path string
		}{
			{"direct stream", "/Audio/" + localID + "/stream", mp3Path},
			{"lyrics", "/Audio/" + localID + "/Lyrics", lrcPath},
		} {
			logBuf.Reset()
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tc.url, nil))
			if rec.Code != http.StatusForbidden {
				t.Fatalf("%s permission denied expected 403, got %d body=%q", tc.name, rec.Code, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), "permission denied reading media file") || strings.Contains(rec.Body.String(), tmp) {
				t.Fatalf("%s unexpected sanitized response: %q", tc.name, rec.Body.String())
			}
			logOut := logBuf.String()
			if !strings.Contains(logOut, "uid=") || !strings.Contains(logOut, "gid=") || !strings.Contains(logOut, tc.path) {
				t.Fatalf("%s expected uid/gid/path in slog output, got %q", tc.name, logOut)
			}
		}

		// Directory path on serveAudioFile -> 404, missing file -> 404
		dirRec := httptest.NewRecorder()
		serveAudioFile(dirRec, httptest.NewRequest(http.MethodGet, "/Audio/x/stream", nil), tmp)
		if dirRec.Code != http.StatusNotFound {
			t.Fatalf("expected 404 for directory audio file, got %d", dirRec.Code)
		}
		missRec := httptest.NewRecorder()
		serveAudioFile(missRec, httptest.NewRequest(http.MethodGet, "/Audio/x/stream", nil), filepath.Join(tmp, "nope.mp3"))
		if missRec.Code != http.StatusNotFound {
			t.Fatalf("expected 404 for missing audio file, got %d", missRec.Code)
		}

		// streamTranscodeAudio edge cases: blocked SSRF URL (502), missing local (404),
		// permission denied on unreadable parent dir (403), HEAD (200), semaphore full (503), bad ffmpeg (500)
		ssrfRec := httptest.NewRecorder()
		h.streamTranscodeAudio(ssrfRec, httptest.NewRequest(http.MethodGet, "/", nil), "http://169.254.169.254/a.mp3", "mp3", "mp3", 128000, 0, 2, 44100)
		if ssrfRec.Code != http.StatusBadGateway {
			t.Fatalf("streamTranscodeAudio SSRF expected 502, got %d", ssrfRec.Code)
		}
		missTransRec := httptest.NewRecorder()
		h.streamTranscodeAudio(missTransRec, httptest.NewRequest(http.MethodGet, "/", nil), filepath.Join(tmp, "missing.flac"), "mp3", "mp3", 128000, 0, 2, 44100)
		if missTransRec.Code != http.StatusNotFound {
			t.Fatalf("streamTranscodeAudio missing expected 404, got %d", missTransRec.Code)
		}
		noExecDir := filepath.Join(tmp, "lockeddir")
		_ = os.MkdirAll(noExecDir, 0o755)
		lockedSub := filepath.Join(noExecDir, "a.flac")
		_ = os.WriteFile(lockedSub, []byte("FLAC"), 0o644)
		_ = os.Chmod(noExecDir, 0o000)
		t.Cleanup(func() { _ = os.Chmod(noExecDir, 0o755) })
		permTransRec := httptest.NewRecorder()
		h.streamTranscodeAudio(permTransRec, httptest.NewRequest(http.MethodGet, "/", nil), lockedSub, "mp3", "mp3", 128000, 0, 2, 44100)
		if permTransRec.Code != http.StatusForbidden {
			t.Fatalf("streamTranscodeAudio permission denied expected 403, got %d", permTransRec.Code)
		}
		_ = os.Chmod(mp3Path, 0o644)
		headTransRec := httptest.NewRecorder()
		h.streamTranscodeAudio(headTransRec, httptest.NewRequest(http.MethodHead, "/", nil), mp3Path, "mp3", "mp3", 128000, 0, 2, 44100)
		if headTransRec.Code != http.StatusOK {
			t.Fatalf("streamTranscodeAudio HEAD expected 200, got %d", headTransRec.Code)
		}
		// Fill semaphore -> 503
		for i := 0; i < cap(h.transcodeSem); i++ {
			h.transcodeSem <- struct{}{}
		}
		semRec := httptest.NewRecorder()
		h.streamTranscodeAudio(semRec, httptest.NewRequest(http.MethodGet, "/", nil), mp3Path, "mp3", "mp3", 128000, 0, 2, 44100)
		if semRec.Code != http.StatusServiceUnavailable {
			t.Fatalf("streamTranscodeAudio full semaphore expected 503, got %d", semRec.Code)
		}
		for i := 0; i < cap(h.transcodeSem); i++ {
			<-h.transcodeSem
		}
		// Bad ffmpeg path -> sanitized 500
		cfg.Update(func(c *config.Config) { c.FFmpegPath = filepath.Join(tmp, "no-such-ffmpeg") })
		badFFRec := httptest.NewRecorder()
		h.streamTranscodeAudio(badFFRec, httptest.NewRequest(http.MethodGet, "/", nil), mp3Path, "mp3", "mp3", 128000, 0, 2, 44100)
		if badFFRec.Code != http.StatusInternalServerError || strings.Contains(badFFRec.Body.String(), "no-such-ffmpeg") {
			t.Fatalf("streamTranscodeAudio bad ffmpeg expected sanitized 500, got %d %q", badFFRec.Code, badFFRec.Body.String())
		}
	}

	// sanitizeProxyAudioContentType & isContainerAllowed edge cases
	if sanitizeProxyAudioContentType("audio/mpeg; charset=utf-8", "") != "audio/mpeg" ||
		sanitizeProxyAudioContentType("text/html", "") != "application/octet-stream" ||
		sanitizeProxyAudioContentType("", "audio/flac") != "audio/flac" ||
		sanitizeProxyAudioContentType("", "") != "application/octet-stream" ||
		sanitizeProxyAudioContentType("invalid/type;=", "") != "application/octet-stream" ||
		sanitizeProxyAudioContentType("application/ogg", "") != "application/ogg" {
		t.Fatal("unexpected sanitizeProxyAudioContentType behavior")
	}
	if !isContainerAllowed(".mp3", []string{"*"}) || isContainerAllowed(".flac", []string{"mp3"}) {
		t.Fatal("unexpected isContainerAllowed behavior")
	}

	// 4. DB closed error is sanitized (no raw sqlite error leaked)
	st.Close()
	recDBErr := httptest.NewRecorder()
	mux.ServeHTTP(recDBErr, httptest.NewRequest(http.MethodGet, "/Audio/"+localID+"/stream", nil))
	if recDBErr.Code != http.StatusInternalServerError || strings.Contains(recDBErr.Body.String(), "sql:") {
		t.Fatalf("expected sanitized 500 on closed DB, got %d %q", recDBErr.Code, recDBErr.Body.String())
	}
	recUnivDBErr := httptest.NewRecorder()
	mux.ServeHTTP(recUnivDBErr, httptest.NewRequest(http.MethodGet, "/Audio/"+localID+"/universal", nil))
	if recUnivDBErr.Code != http.StatusInternalServerError || strings.Contains(recUnivDBErr.Body.String(), "sql:") {
		t.Fatalf("expected sanitized 500 on closed DB universal, got %d %q", recUnivDBErr.Code, recUnivDBErr.Body.String())
	}
	recLrcDBErr := httptest.NewRecorder()
	mux.ServeHTTP(recLrcDBErr, httptest.NewRequest(http.MethodGet, "/Audio/"+localID+"/Lyrics", nil))
	if recLrcDBErr.Code != http.StatusInternalServerError || strings.Contains(recLrcDBErr.Body.String(), "sql:") {
		t.Fatalf("expected sanitized 500 on closed DB lyrics, got %d %q", recLrcDBErr.Code, recLrcDBErr.Body.String())
	}
}
