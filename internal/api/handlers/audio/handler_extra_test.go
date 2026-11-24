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

func TestAudioHandlerExtraBranches(t *testing.T) {
	tmp := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmp, "audio_extra.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	ctx := context.Background()
	repo := sqlite.NewLibraryRepo(store)
	cfg := config.Default(tmp)

	// Nil repo check
	if _, _, err := New(nil, cfg).resolveAudioPath(ctx, "x", ""); err != sqlite.ErrNotFound {
		t.Fatalf("expected ErrNotFound for nil repo")
	}

	// Seed audio items:
	// 1) multiMSItem with 2 sources (ms1 flac, ms2 mp3)
	// 2) itemPathOnly (no MediaSources)
	// 3) emptyPathItem (no MediaSources, Path=="")
	// 4) missingDiskItem (Path points to missing file)
	flacPath := filepath.Join(tmp, "song.flac")
	mp3Path := filepath.Join(tmp, "song.mp3")
	_ = os.WriteFile(flacPath, []byte("fake-flac-data"), 0o644)
	_ = os.WriteFile(mp3Path, []byte("fake-mp3-data"), 0o644)

	multiID := guid.New()
	ms1 := guid.New()
	ms2 := guid.New()
	_ = repo.UpsertItem(ctx, &sqlite.ItemRecord{ID: multiID, LibraryID: "lib1", Type: "Audio", Name: "Multi", Path: flacPath})
	_ = repo.ReplaceMediaSourcesAndStreams(ctx, multiID, []sqlite.MediaSourceRecord{
		{ID: ms1, ItemID: multiID, Name: "FLAC", Path: flacPath, Container: "flac", Bitrate: 1_000_000},
		{ID: ms2, ItemID: multiID, Name: "MP3", Path: mp3Path, Container: "mp3", Bitrate: 320_000},
	})

	pathOnlyID := guid.New()
	_ = repo.UpsertItem(ctx, &sqlite.ItemRecord{ID: pathOnlyID, LibraryID: "lib1", Type: "Audio", Name: "PathOnly", Path: mp3Path})

	emptyPathID := guid.New()
	_ = repo.UpsertItem(ctx, &sqlite.ItemRecord{ID: emptyPathID, LibraryID: "lib1", Type: "Audio", Name: "EmptyPath"})

	missingDiskID := guid.New()
	_ = repo.UpsertItem(ctx, &sqlite.ItemRecord{ID: missingDiskID, LibraryID: "lib1", Type: "Audio", Name: "MissingDisk", Path: filepath.Join(tmp, "gone.mp3")})

	mux := http.NewServeMux()
	h := New(repo, cfg)
	h.RegisterRoutes(mux, AuthFuncs{})

	// 1. Unsupported audio endpoint -> 404; lowercase /lyrics via {File} route -> 404 (no lrc)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/Audio/"+multiID+"/unknown.bin", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unsupported audio endpoint, got %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/Audio/"+multiID+"/lyrics", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for missing lrc via lowercase lyrics, got %d", rec.Code)
	}

	// 2. Direct stream with MediaSourceId=ms2, itemPathOnly fallback, emptyPath (404), missingDisk (404), missingItem (404)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/Audio/"+multiID+"/stream?MediaSourceId="+ms2, nil))
	if rec.Code != http.StatusOK || rec.Body.String() != "fake-mp3-data" {
		t.Fatalf("expected mp3 stream for ms2, got %d %q", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/Audio/"+pathOnlyID+"/stream", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for pathOnlyID, got %d", rec.Code)
	}
	for _, path := range []string{
		"/Audio/" + emptyPathID + "/stream",
		"/Audio/" + missingDiskID + "/stream",
		"/Audio/" + guid.New() + "/stream",
		"/Audio/" + guid.New() + "/universal",
		"/Audio/" + guid.New() + "/Lyrics",
	} {
		rec = httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusNotFound {
			t.Errorf("GET %s expected 404, got %d", path, rec.Code)
		}
	}

	// 3. Universal transcoding branches:
	// - missing file on disk -> 404
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/Audio/"+missingDiskID+"/universal.mp3?Container=aac", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for missing disk file in universal transcode, got %d", rec.Code)
	}
	// - HEAD request on transcode -> 200 with Content-Type without spawning ffmpeg
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodHead, "/Audio/"+multiID+"/universal?Container=mp3&MaxStreamingBitrate=128000", nil))
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "audio/mpeg" {
		t.Fatalf("expected 200 audio/mpeg for HEAD transcode, got %d %s", rec.Code, rec.Header().Get("Content-Type"))
	}
	// - Missing ffmpeg binary -> 500
	cfgBad := config.Default(tmp)
	cfgBad.FFmpegPath = "/nonexistent/ffmpeg-audio-bin"
	muxBad := http.NewServeMux()
	New(repo, cfgBad).RegisterRoutes(muxBad, AuthFuncs{})
	rec = httptest.NewRecorder()
	muxBad.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/Audio/"+multiID+"/universal?Container=mp3&AudioBitrate=128000", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 for missing ffmpeg, got %d", rec.Code)
	}

	// - Mock ffmpeg script streaming audio bytes -> 200
	if runtime.GOOS != "windows" {
		mockFF := filepath.Join(tmp, "mock_ffmpeg.sh")
		_ = os.WriteFile(mockFF, []byte("#!/bin/sh\nprintf 'TRANSCODED_MP3'\n"), 0o755)
		cfgMock := config.Default(tmp)
		cfgMock.FFmpegPath = mockFF
		muxMock := http.NewServeMux()
		New(repo, cfgMock).RegisterRoutes(muxMock, AuthFuncs{})
		rec = httptest.NewRecorder()
		muxMock.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/Audio/"+multiID+"/universal?Container=mp3&MaxStreamingBitrate=128000", nil))
		if rec.Code != http.StatusOK || rec.Body.String() != "TRANSCODED_MP3" {
			t.Fatalf("expected TRANSCODED_MP3, got %d %q", rec.Code, rec.Body.String())
		}
	}

	// 4. Helper coverage: isContainerAllowed, resolveAudioTranscodeTarget, audioContentType, ParseLRC
	if !isContainerAllowed("flac", []string{"*"}) {
		t.Fatalf("expected wildcard container allowed")
	}
	for _, tc := range [][2]string{
		{"aac", ""}, {"m4a", ""}, {"ogg", "vorbis"}, {"opus", ""}, {"flac", ""}, {"wav", ""}, {"unknown", "aac"}, {"unknown", "mp3"},
	} {
		ct, muxFmt, enc := resolveAudioTranscodeTarget(tc[0], tc[1])
		if ct == "" || muxFmt == "" || enc == "" {
			t.Errorf("empty transcode target for %v", tc)
		}
	}
	for _, ext := range []string{".mp3", ".flac", ".m4a", ".aac", ".ogg", ".opus", ".wav", ".wma", ".other"} {
		if audioContentType("song"+ext) == "" {
			t.Errorf("empty content type for %s", ext)
		}
	}

	// ParseLRC: 1-digit/3-digit fractional seconds, negative offset clamping, [al:]/[au:], unsynced lyrics
	lrcSynced := "[al:MyAlbum]\n[au:AuthorX]\n[offset:-5000]\n[00:01.5]Line1\n[00:01.500]Line2\n[00:10.123]Line3\n"
	dtoSynced := ParseLRC(lrcSynced)
	if !dtoSynced.Metadata.IsSynced || dtoSynced.Metadata.Album != "MyAlbum" || dtoSynced.Metadata.Author != "AuthorX" || len(dtoSynced.Lyrics) != 3 {
		t.Fatalf("unexpected synced LRC: %+v", dtoSynced)
	}
	if *dtoSynced.Lyrics[0].Start != 0 {
		t.Fatalf("expected negative offset clamped to 0, got %d", *dtoSynced.Lyrics[0].Start)
	}

	dtoUnsynced := ParseLRC("Plain lyric line 1\nPlain lyric line 2\n")
	if dtoUnsynced.Metadata.IsSynced || len(dtoUnsynced.Lyrics) != 2 || !strings.Contains(dtoUnsynced.Lyrics[0].Text, "Plain") {
		t.Fatalf("unexpected unsynced LRC: %+v", dtoUnsynced)
	}

	// 5. Closed DB -> 500 paths
	_ = store.Close()
	for _, path := range []string{
		"/Audio/" + multiID + "/stream",
		"/Audio/" + multiID + "/universal",
		"/Audio/" + multiID + "/Lyrics",
	} {
		rec = httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusInternalServerError {
			t.Errorf("closed DB GET %s expected 500, got %d", path, rec.Code)
		}
	}
}
