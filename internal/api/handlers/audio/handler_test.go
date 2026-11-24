package audio_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/ambra-media/ambra/internal/api/handlers/audio"
	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func setupAudioTest(t *testing.T, audioFile string) (*http.ServeMux, string) {
	t.Helper()
	tmpDir := t.TempDir()
	cfg := config.Default(tmpDir)
	st, err := sqlite.Open(cfg.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	repo := sqlite.NewLibraryRepo(st)
	ctx := context.Background()
	lib, err := repo.CreateLibrary(ctx, "Music", "music", []string{filepath.Dir(audioFile)})
	if err != nil {
		t.Fatal(err)
	}
	item := &sqlite.ItemRecord{
		LibraryID: lib.ID,
		Type:      "Audio",
		MediaType: "Audio",
		Name:      "Test Track",
		Container: "mp3",
		Path:      audioFile,
	}
	if err := repo.UpsertItem(ctx, item); err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	h := audio.New(repo, cfg)
	h.RegisterRoutes(mux, audio.AuthFuncs{})
	return mux, item.ID
}

func TestAudioRangeDirectStreamAndLyrics(t *testing.T) {
	mediaDir := t.TempDir()
	audioPath := filepath.Join(mediaDir, "track.mp3")
	content := []byte("0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ")
	if err := os.WriteFile(audioPath, content, 0o644); err != nil {
		t.Fatal(err)
	}

	lrcContent := `[ti:Night Sky]
[ar:Ambra Band]
[al:First Album]
[offset:500]
[00:10.00][01:00.25]Chorus Line
[00:05.50]First Verse
`
	if err := os.WriteFile(filepath.Join(mediaDir, "track.lrc"), []byte(lrcContent), 0o644); err != nil {
		t.Fatal(err)
	}

	mux, itemID := setupAudioTest(t, audioPath)

	// 1. Range request on /Audio/{Id}/stream.mp3
	req := httptest.NewRequest(http.MethodGet, "/Audio/"+itemID+"/stream.mp3", nil)
	req.Header.Set("Range", "bytes=10-19")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusPartialContent {
		t.Fatalf("expected 206 Partial Content, got %d", rec.Code)
	}
	if rec.Body.String() != "ABCDEFGHIJ" {
		t.Fatalf("unexpected range body: %q", rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "audio/mpeg" {
		t.Fatalf("expected audio/mpeg, got %q", ct)
	}

	// 2. HEAD /Audio/{Id}/stream
	req = httptest.NewRequest(http.MethodHead, "/Audio/"+itemID+"/stream", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Body.Len() != 0 {
		t.Fatalf("HEAD stream failed: code=%d len=%d", rec.Code, rec.Body.Len())
	}

	// 3. Universal direct play when Container includes mp3
	req = httptest.NewRequest(http.MethodGet, "/Audio/"+itemID+"/universal?Container=mp3,aac", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Body.String() != string(content) {
		t.Fatalf("universal direct play failed: code=%d body=%q", rec.Code, rec.Body.String())
	}

	// 4. GET /Audio/{Id}/Lyrics
	req = httptest.NewRequest(http.MethodGet, "/Audio/"+itemID+"/Lyrics", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("Lyrics failed: code=%d body=%s", rec.Code, rec.Body.String())
	}
	var lyric audio.LyricDto
	if err := json.Unmarshal(rec.Body.Bytes(), &lyric); err != nil {
		t.Fatalf("unmarshal lyrics: %v", err)
	}
	if !lyric.Metadata.IsSynced || lyric.Metadata.Title != "Night Sky" || lyric.Metadata.Artist != "Ambra Band" {
		t.Fatalf("unexpected lyric metadata: %+v", lyric.Metadata)
	}
	if len(lyric.Lyrics) != 3 {
		t.Fatalf("expected 3 sorted lyric lines, got %d: %+v", len(lyric.Lyrics), lyric.Lyrics)
	}
	// First Verse: 5.5s + 0.5s offset = 6.0s = 60,000,000 ticks
	if lyric.Lyrics[0].Text != "First Verse" || *lyric.Lyrics[0].Start != 60_000_000 {
		t.Fatalf("unexpected first lyric line: text=%q start=%d", lyric.Lyrics[0].Text, *lyric.Lyrics[0].Start)
	}
	// Chorus Line #1: 10.0s + 0.5s = 10.5s = 105,000,000 ticks
	if lyric.Lyrics[1].Text != "Chorus Line" || *lyric.Lyrics[1].Start != 105_000_000 {
		t.Fatalf("unexpected second lyric line: text=%q start=%d", lyric.Lyrics[1].Text, *lyric.Lyrics[1].Start)
	}
	// Chorus Line #2: 60.25s + 0.5s = 60.75s = 607,500,000 ticks
	if lyric.Lyrics[2].Text != "Chorus Line" || *lyric.Lyrics[2].Start != 607_500_000 {
		t.Fatalf("unexpected third lyric line: text=%q start=%d", lyric.Lyrics[2].Text, *lyric.Lyrics[2].Start)
	}
}

func TestAudioUniversalTranscodeWithFFmpeg(t *testing.T) {
	ffmpegPath, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg not installed; skipping audio universal transcode test")
	}

	mediaDir := t.TempDir()
	wavPath := filepath.Join(mediaDir, "tone.wav")
	gen := exec.Command(ffmpegPath,
		"-hide_banner", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=1",
		wavPath,
	)
	if out, err := gen.CombinedOutput(); err != nil {
		t.Fatalf("generate wav failed: %v (%s)", err, string(out))
	}

	mux, itemID := setupAudioTest(t, wavPath)

	// Request universal with Container=mp3 (wav is not allowed -> live FFmpeg transcode to mp3)
	req := httptest.NewRequest(http.MethodGet, "/Audio/"+itemID+"/universal.mp3?Container=mp3&TranscodingContainer=mp3&AudioBitrate=128000", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("universal transcode failed: code=%d body=%s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "audio/mpeg" {
		t.Fatalf("expected audio/mpeg, got %q", ct)
	}
	if rec.Body.Len() < 1000 {
		t.Fatalf("expected transcoded mp3 bytes, got only %d bytes", rec.Body.Len())
	}
}
