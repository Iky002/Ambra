package audio

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestV0109Audit_FFmpegHardeningAndExtensionlessUniversal302(t *testing.T) {
	// 1. [H-07] HTTP input enforces -protocol_whitelist, -follow_redirects 0, -rw_timeout 15000000
	httpArgs := strings.Join(buildAudioFFmpegArgs("https://93.184.216.34/audio/stream", "mp3", "libmp3lame", 192000, 0, 2, 44100), " ")
	for _, want := range []string{
		"-protocol_whitelist http,https,tcp,tls,crypto",
		"-follow_redirects 0",
		"-rw_timeout 15000000",
	} {
		if !strings.Contains(httpArgs, want) {
			t.Fatalf("expected %q in HTTP ffmpeg args, got: %s", want, httpArgs)
		}
	}
	if strings.Index(httpArgs, "-follow_redirects 0") > strings.Index(httpArgs, "-i ") {
		t.Fatalf("expected -follow_redirects before -i, got: %s", httpArgs)
	}

	// Local file input enforces -protocol_whitelist file,crypto,data
	localArgs := strings.Join(buildAudioFFmpegArgs("/music/track.flac", "mp3", "libmp3lame", 192000, 50_000_000, 2, 44100), " ")
	if !strings.Contains(localArgs, "-protocol_whitelist file,crypto,data") {
		t.Fatalf("expected local -protocol_whitelist in ffmpeg args, got: %s", localArgs)
	}
	if strings.Index(localArgs, "-protocol_whitelist file,crypto,data") > strings.Index(localArgs, "-i ") {
		t.Fatalf("expected local -protocol_whitelist before -i, got: %s", localArgs)
	}

	// 2. [M-16] resolveAudioPath returns Container and handleUniversal handles extensionless signed STRM URLs
	tmp := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmp, "audio_v0109.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	ctx := context.Background()
	repo := sqlite.NewLibraryRepo(store)
	cfg := config.Default(tmp)

	strmWithContainer := filepath.Join(tmp, "cloud_flac.strm")
	_ = os.WriteFile(strmWithContainer, []byte("https://93.184.216.34/download?token=abc123\n"), 0o644)
	itemWithContainer := guid.New()
	_ = repo.UpsertItem(ctx, &sqlite.ItemRecord{
		ID:        itemWithContainer,
		LibraryID: "lib1",
		Type:      "Audio",
		MediaType: "Audio",
		Name:      "Cloud FLAC",
		Path:      strmWithContainer,
		Container: "flac",
	})
	_ = repo.ReplaceMediaSourcesAndStreams(ctx, itemWithContainer, []sqlite.MediaSourceRecord{
		{ID: guid.New(), ItemID: itemWithContainer, Path: strmWithContainer, Container: "flac", Bitrate: 800_000},
	})

	strmUnknownExt := filepath.Join(tmp, "cloud_unknown.strm")
	_ = os.WriteFile(strmUnknownExt, []byte("https://93.184.216.34/d/signed-object-id\n"), 0o644)
	itemUnknownExt := guid.New()
	_ = repo.UpsertItem(ctx, &sqlite.ItemRecord{
		ID:        itemUnknownExt,
		LibraryID: "lib1",
		Type:      "Audio",
		MediaType: "Audio",
		Name:      "Cloud Unknown",
		Path:      strmUnknownExt,
	})

	h := New(repo, cfg)
	// Verify resolveAudioPath returns container
	_, _, gotContainer, err := h.resolveAudioPath(ctx, itemWithContainer, "")
	if err != nil || gotContainer != "flac" {
		t.Fatalf("resolveAudioPath expected container=flac, got %q err=%v", gotContainer, err)
	}

	signedURL := "https://93.184.216.34/d/signed-object-id?sig=xyz"
	h.SetStreamResolverFull(func(_ context.Context, _ string) (string, bool, error) {
		return signedURL, true, nil
	})
	mux := http.NewServeMux()
	h.RegisterRoutes(mux, AuthFuncs{})

	// 2a. Extensionless URL falls back to Container="flac" -> allowed when client accepts flac -> 302
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/Audio/"+itemWithContainer+"/universal?Container=mp3,flac", nil))
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != signedURL {
		t.Fatalf("expected 302 redirect via Container fallback, got %d loc=%q", rec.Code, rec.Header().Get("Location"))
	}

	// 2b. Extensionless URL with empty Container + is302=true + no seek/bitrate limit -> 302 redirect instead of transcode
	rec2 := httptest.NewRecorder()
	mux.ServeHTTP(rec2, httptest.NewRequest(http.MethodGet, "/Audio/"+itemUnknownExt+"/universal?Container=mp3,aac", nil))
	if rec2.Code != http.StatusFound || rec2.Header().Get("Location") != signedURL {
		t.Fatalf("expected 302 redirect for extensionless signed STRM URL, got %d loc=%q", rec2.Code, rec2.Header().Get("Location"))
	}

	// 2c. When StartTimeTicks > 0, must NOT 302 redirect (falls through to transcode; HEAD returns 200 audio/mpeg)
	recSeek := httptest.NewRecorder()
	mux.ServeHTTP(recSeek, httptest.NewRequest(http.MethodHead, "/Audio/"+itemUnknownExt+"/universal?Container=mp3&StartTimeTicks=100000000", nil))
	if recSeek.Code == http.StatusFound {
		t.Fatalf("expected seek request not to 302 redirect, got 302")
	}

	// 2d. When bitrate exceeds MaxStreamingBitrate, must NOT 302 redirect
	recBitrate := httptest.NewRecorder()
	mux.ServeHTTP(recBitrate, httptest.NewRequest(http.MethodHead, "/Audio/"+itemWithContainer+"/universal?Container=mp3,flac&MaxStreamingBitrate=128000", nil))
	if recBitrate.Code == http.StatusFound {
		t.Fatalf("expected bitrate-exceeded request not to 302 redirect, got 302")
	}
}
