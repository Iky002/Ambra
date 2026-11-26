package streaming

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ambra-media/ambra/internal/api/dto"
	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/core/imaging"
	"github.com/ambra-media/ambra/internal/core/playback"
	"github.com/ambra-media/ambra/internal/core/transcode"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/plugins"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestStreamingV0107AuditBIFPrivateCachePlaybackInfoQueryAndPermSanitize(t *testing.T) {
	tmp := t.TempDir()
	st, err := sqlite.Open(filepath.Join(tmp, "stream_v0107.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	ctx := context.Background()
	repo := sqlite.NewLibraryRepo(st)
	cfg := config.Default(tmp)
	reg := plugins.NewRegistry()
	engine := playback.New(repo, reg, cfg)
	imgSvc := imaging.New(repo).WithCacheDir(cfg.ImageCacheDir())
	ext := transcode.NewExtractor(cfg)

	lib, _ := repo.CreateLibrary(ctx, "Movies", "movies", []string{tmp})
	videoPath := filepath.Join(tmp, "secret_movie.mp4")
	subPath := filepath.Join(tmp, "secret_sub.srt")
	imgPath := filepath.Join(tmp, "secret_poster.jpg")
	if err := os.WriteFile(videoPath, []byte("MP4VIDEO"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(subPath, []byte("1\n00:00:01,000 --> 00:00:02,000\nHi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var jpgBuf bytes.Buffer
	im := image.NewRGBA(image.Rect(0, 0, 8, 8))
	im.Set(0, 0, color.RGBA{R: 255, A: 255})
	_ = jpeg.Encode(&jpgBuf, im, nil)
	if err := os.WriteFile(imgPath, jpgBuf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}

	itemID := guid.New()
	msID := guid.New()
	_ = repo.UpsertItem(ctx, &sqlite.ItemRecord{
		ID: itemID, LibraryID: lib.ID, ParentID: lib.ID,
		Type: "Movie", MediaType: "Video", Name: "Audit Movie", Path: videoPath, Container: "mp4",
	})
	_ = repo.ReplaceMediaSourcesAndStreams(ctx, itemID, []sqlite.MediaSourceRecord{{
		ID: msID, ItemID: itemID, Name: "secret_movie.mp4", Path: videoPath, Container: "mp4",
		SupportsDirectPlay: true, SupportsDirectStream: true, SupportsTranscoding: true,
		Streams: []sqlite.MediaStreamRecord{
			{MediaSourceID: msID, ItemID: itemID, StreamIndex: 0, StreamType: "Video", Codec: "h264", Width: 1920, Height: 1080},
			{MediaSourceID: msID, ItemID: itemID, StreamIndex: 1, StreamType: "Audio", Codec: "aac"},
			{MediaSourceID: msID, ItemID: itemID, StreamIndex: 2, StreamType: "Subtitle", Codec: "srt", IsExternal: true, Path: subPath},
		},
	}})
	_ = repo.UpsertImage(ctx, &sqlite.ImageRecord{
		ItemID: itemID, ImageType: "Primary", ImageIndex: 0, Path: imgPath, Tag: "tag1", Width: 8, Height: 8,
	})

	// Pre-create BIF sidecar file
	bifPath := filepath.Join(tmp, "secret_movie-320-10.bif")
	if err := transcode.WriteBIF(bifPath, 10000, [][]byte{jpgBuf.Bytes()}); err != nil {
		t.Fatal(err)
	}

	h := New(repo, engine, imgSvc)
	h.SetExtractor(ext)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux, AuthFuncs{})

	// 1. [M-5] index.bif uses Cache-Control: private
	recBIF := httptest.NewRecorder()
	mux.ServeHTTP(recBIF, httptest.NewRequest(http.MethodGet, "/Videos/"+itemID+"/index.bif?Width=320", nil))
	if recBIF.Code != http.StatusOK {
		t.Fatalf("GET index.bif got %d body=%s", recBIF.Code, recBIF.Body.String())
	}
	if cc := recBIF.Header().Get("Cache-Control"); cc != "private, max-age=86400" {
		t.Fatalf("expected private Cache-Control on index.bif, got %q", cc)
	}

	// 2. [M-10] PlaybackInfo query flags EnableDirectPlay / EnableDirectStream / EnableTranscoding
	recPI := httptest.NewRecorder()
	mux.ServeHTTP(recPI, httptest.NewRequest(http.MethodGet, "/Items/"+itemID+"/PlaybackInfo?EnableDirectPlay=false&EnableDirectStream=false&EnableTranscoding=true", nil))
	if recPI.Code != http.StatusOK {
		t.Fatalf("PlaybackInfo got %d body=%s", recPI.Code, recPI.Body.String())
	}
	var piResp dto.PlaybackInfoResponse
	if err := json.Unmarshal(recPI.Body.Bytes(), &piResp); err != nil {
		t.Fatal(err)
	}
	if len(piResp.MediaSources) == 0 {
		t.Fatalf("expected media source in PlaybackInfo")
	}
	if piResp.MediaSources[0].SupportsDirectPlay || piResp.MediaSources[0].SupportsDirectStream || !piResp.MediaSources[0].SupportsTranscoding {
		t.Fatalf("expected query flags respected: dp=%v ds=%v tc=%v",
			piResp.MediaSources[0].SupportsDirectPlay, piResp.MediaSources[0].SupportsDirectStream, piResp.MediaSources[0].SupportsTranscoding)
	}

	// 3. [Task 1 / M-7] Permission denied on media/bif/subtitle/image logs uid/gid and sanitizes HTTP response
	if runtime.GOOS != "windows" && os.Geteuid() != 0 {
		var logBuf bytes.Buffer
		oldLogger := slog.Default()
		slog.SetDefault(slog.New(slog.NewTextHandler(&logBuf, nil)))
		t.Cleanup(func() { slog.SetDefault(oldLogger) })

		for _, p := range []string{videoPath, subPath, imgPath, bifPath} {
			if err := os.Chmod(p, 0o000); err != nil {
				t.Fatal(err)
			}
			pCopy := p
			t.Cleanup(func() { _ = os.Chmod(pCopy, 0o644) })
		}

		for _, tc := range []struct {
			name string
			url  string
			path string
		}{
			{"video stream", "/Videos/" + itemID + "/stream", videoPath},
			{"bif", "/Videos/" + itemID + "/index.bif?Width=320", bifPath},
			{"subtitle", "/Videos/" + itemID + "/" + msID + "/Subtitles/2/Stream.vtt", subPath},
			{"image", "/Items/" + itemID + "/Images/Primary?tag=tag1", imgPath},
		} {
			logBuf.Reset()
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tc.url, nil))
			if rec.Code != http.StatusForbidden {
				t.Fatalf("%s permission denied expected 403, got %d body=%q", tc.name, rec.Code, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), "permission denied reading media file") {
				t.Fatalf("%s expected sanitized permission message, got %q", tc.name, rec.Body.String())
			}
			if strings.Contains(rec.Body.String(), tmp) {
				t.Fatalf("%s leaked disk path in HTTP response: %q", tc.name, rec.Body.String())
			}
			logOut := logBuf.String()
			if !strings.Contains(logOut, "uid=") || !strings.Contains(logOut, "gid=") || !strings.Contains(logOut, tc.path) {
				t.Fatalf("%s expected uid/gid/path in slog output, got %q", tc.name, logOut)
			}
		}
	}
}
