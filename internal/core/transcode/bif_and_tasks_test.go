package transcode

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func createTinyJPEG(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 16, 16))
	img.Set(0, 0, color.RGBA{R: 255, G: 128, B: 0, A: 255})
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestBIFWriteReadAndGenerate(t *testing.T) {
	tmp := t.TempDir()
	jpgBytes := createTinyJPEG(t)
	sampleJPG := filepath.Join(tmp, "sample.jpg")
	if err := os.WriteFile(sampleJPG, jpgBytes, 0o644); err != nil {
		t.Fatal(err)
	}

	// WriteBIF & ReadBIFMetadata round-trip
	bifPath := filepath.Join(tmp, "out", "test.bif")
	if err := WriteBIF(bifPath, 0, nil); err == nil {
		t.Fatal("expected error writing 0-frame BIF")
	}
	if err := WriteBIF(bifPath, 10000, [][]byte{jpgBytes, jpgBytes, jpgBytes}); err != nil {
		t.Fatalf("WriteBIF: %v", err)
	}
	meta, err := ReadBIFMetadata(bifPath)
	if err != nil {
		t.Fatalf("ReadBIFMetadata: %v", err)
	}
	if meta.Version != 0 || meta.ImageCount != 3 || meta.TimestampMultiplierMs != 10000 {
		t.Fatalf("unexpected BIF metadata: %+v", meta)
	}
	if len(meta.FrameTimestampsMs) != 3 || meta.FrameTimestampsMs[0] != 0 || meta.FrameTimestampsMs[2] != 20000 {
		t.Fatalf("unexpected frame timestamps: %v", meta.FrameTimestampsMs)
	}

	// Invalid magic check
	badBif := filepath.Join(tmp, "bad.bif")
	_ = os.WriteFile(badBif, make([]byte, 64), 0o644)
	if _, err := ReadBIFMetadata(badBif); err == nil {
		t.Fatal("expected error for invalid BIF magic")
	}

	// ParseBIFWidthParam clamping
	if got := ParseBIFWidthParam(""); got != DefaultBIFWidth {
		t.Fatalf("expected default width, got %d", got)
	}
	if got := ParseBIFWidthParam("50"); got != 120 {
		t.Fatalf("expected min clamped width 120, got %d", got)
	}
	if got := ParseBIFWidthParam("4000"); got != 1280 {
		t.Fatalf("expected max clamped width 1280, got %d", got)
	}
	if got := ParseBIFWidthParam("480"); got != 480 {
		t.Fatalf("expected 480, got %d", got)
	}

	// Fake ffmpeg script that generates 2 JPEG frames into the frame pattern directory
	callLog := filepath.Join(tmp, "ffmpeg_bif_calls.log")
	fakeFFmpeg := filepath.Join(tmp, "fake-ffmpeg-bif.sh")
	script := fmt.Sprintf(`#!/bin/sh
echo "$@" >> %q
for last; do true; done
dir=$(dirname "$last")
cp %q "$dir/frame_000001.jpg"
cp %q "$dir/frame_000002.jpg"
exit 0
`, callLog, sampleJPG, sampleJPG)
	if err := os.WriteFile(fakeFFmpeg, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	cfg := config.Default(tmp)
	cfg.FFmpegPath = fakeFFmpeg
	ext := NewExtractor(cfg)

	// Reject remote/strm/empty/missing media
	if _, err := ext.GenerateBIF(context.Background(), "", "id1", 320, 10); err == nil {
		t.Fatal("expected error for empty path")
	}
	if _, err := ext.GenerateBIF(context.Background(), "https://example.com/v.mkv", "id1", 320, 10); err == nil {
		t.Fatal("expected error for HTTP URL")
	}
	if _, err := ext.GenerateBIF(context.Background(), filepath.Join(tmp, "remote.strm"), "id1", 320, 10); err == nil {
		t.Fatal("expected error for .strm file")
	}
	if _, err := ext.GenerateBIF(context.Background(), filepath.Join(tmp, "missing.mkv"), "id1", 320, 10); err == nil {
		t.Fatal("expected error for missing local file")
	}

	moviePath := filepath.Join(tmp, "movie.mkv")
	_ = os.WriteFile(moviePath, []byte("local-video"), 0o644)

	// GenerateBIF creates index_320.bif in cache/bif/<itemID>/
	outPath, err := ext.GenerateBIF(context.Background(), moviePath, "mov-bif-1", 0, 0)
	if err != nil {
		t.Fatalf("GenerateBIF: %v", err)
	}
	wantOut := filepath.Join(cfg.BifCacheDir(), "mov-bif-1", "index_320.bif")
	if outPath != wantOut {
		t.Fatalf("expected %q, got %q", wantOut, outPath)
	}
	meta2, err := ReadBIFMetadata(outPath)
	if err != nil || meta2.ImageCount != 2 {
		t.Fatalf("expected 2-frame BIF, got meta=%+v err=%v", meta2, err)
	}

	// Second call is idempotent (hits cache without re-running ffmpeg)
	if _, err := ext.GenerateBIF(context.Background(), moviePath, "mov-bif-1", 320, 10); err != nil {
		t.Fatalf("second GenerateBIF: %v", err)
	}
	rawCalls, _ := os.ReadFile(callLog)
	if n := len(strings.Split(strings.TrimSpace(string(rawCalls)), "\n")); n != 1 {
		t.Fatalf("expected 1 ffmpeg invocation, got %d", n)
	}

	// FindExistingBIF resolves cached BIF and prefers sidecar BIF when present
	if found := ext.FindExistingBIF(moviePath, "mov-bif-1", 480); found != wantOut {
		t.Fatalf("expected fallback to cached index_320.bif, got %q", found)
	}
	sidecar := filepath.Join(tmp, "movie-320-10.bif")
	_ = os.WriteFile(sidecar, []byte("sidecar-bif"), 0o644)
	if found := ext.FindExistingBIF(moviePath, "mov-bif-1", 320); found != sidecar {
		t.Fatalf("expected sidecar BIF priority, got %q", found)
	}
}

func TestScheduledChapterAndBIFTasks(t *testing.T) {
	tmp := t.TempDir()
	cfg, err := config.Load(tmp)
	if err != nil {
		t.Fatal(err)
	}
	// Verify dedicated chapter cache README warning is created on config.Load
	readmePath := filepath.Join(cfg.ChapterCacheDir(), config.ChapterCacheReadmeName)
	if raw, err := os.ReadFile(readmePath); err != nil || !strings.Contains(string(raw), "谨慎删除") {
		t.Fatalf("expected chapter cache README warning at %s, err=%v", readmePath, err)
	}

	store, err := sqlite.Open(cfg.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	repo := sqlite.NewLibraryRepo(store)

	jpgBytes := createTinyJPEG(t)
	sampleJPG := filepath.Join(tmp, "sample.jpg")
	_ = os.WriteFile(sampleJPG, jpgBytes, 0o644)

	callLog := filepath.Join(tmp, "task_ffmpeg.log")
	fakeFFmpeg := filepath.Join(tmp, "fake-ffmpeg-tasks.sh")
	script := fmt.Sprintf(`#!/bin/sh
echo "$@" >> %q
for last; do true; done
case "$last" in
  *frame_%%06d.jpg)
    dir=$(dirname "$last")
    cp %q "$dir/frame_000001.jpg"
    ;;
  *)
    cp %q "$last"
    ;;
esac
exit 0
`, callLog, sampleJPG, sampleJPG)
	if err := os.WriteFile(fakeFFmpeg, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg.FFmpegPath = fakeFFmpeg
	ext := NewExtractor(cfg)

	ctx := context.Background()
	localMoviePath := filepath.Join(tmp, "local_movie.mkv")
	_ = os.WriteFile(localMoviePath, []byte("mkv"), 0o644)
	strmPath := filepath.Join(tmp, "remote.strm")
	_ = os.WriteFile(strmPath, []byte("https://cdn.example.com/movie.mkv"), 0o644)

	localID := guid.New()
	strmID := guid.New()
	_ = repo.UpsertItem(ctx, &sqlite.ItemRecord{
		ID: localID, LibraryID: "lib1", Type: "Movie", MediaType: "Video", Name: "Local Movie", Path: localMoviePath,
		Extra: sqlite.ItemExtra{
			Chapters: []sqlite.ChapterRecord{
				{Name: "Opening", StartPositionTicks: 0},
				{Name: "Climax", StartPositionTicks: 600_000_000},
			},
		},
	})
	_ = repo.UpsertItem(ctx, &sqlite.ItemRecord{
		ID: strmID, LibraryID: "lib1", Type: "Movie", MediaType: "Video", Name: "Remote STRM", Path: strmPath, IsStrm: true,
		Extra: sqlite.ItemExtra{
			Chapters: []sqlite.ChapterRecord{
				{Name: "Remote Ch1", StartPositionTicks: 0},
			},
		},
	})

	// 1. Run ExtractAllChapterImages: extracts 2 local chapters into cache/chapters, skips STRM
	var lastPct float64
	chSum, err := ExtractAllChapterImages(ctx, repo, ext, func(p float64) { lastPct = p })
	if err != nil {
		t.Fatalf("ExtractAllChapterImages: %v", err)
	}
	if chSum.Extracted != 2 || chSum.Skipped != 1 || chSum.Failed != 0 || lastPct != 100 {
		t.Fatalf("unexpected chapter task summary: %+v lastPct=%v", chSum, lastPct)
	}
	img0, err := repo.GetImageByItemAndType(ctx, localID, "Chapter", 0)
	if err != nil || !strings.HasPrefix(img0.Path, cfg.ChapterCacheDir()) {
		t.Fatalf("expected chapter image stored under ChapterCacheDir %q, got %+v err=%v", cfg.ChapterCacheDir(), img0, err)
	}

	// Re-running ExtractAllChapterImages skips all already-extracted chapters
	chSum2, err := ExtractAllChapterImages(ctx, repo, ext, nil)
	if err != nil {
		t.Fatalf("second ExtractAllChapterImages: %v", err)
	}
	if chSum2.Extracted != 0 || chSum2.Skipped != 3 {
		t.Fatalf("expected incremental skip on second run, got %+v", chSum2)
	}

	// 2. Run GenerateAllBIFThumbnails: generates 1 local BIF into cache/bif, skips STRM
	bifSum, err := GenerateAllBIFThumbnails(ctx, repo, ext, func(p float64) { lastPct = p })
	if err != nil {
		t.Fatalf("GenerateAllBIFThumbnails: %v", err)
	}
	if bifSum.Generated != 1 || bifSum.Skipped != 1 || bifSum.Failed != 0 || lastPct != 100 {
		t.Fatalf("unexpected bif task summary: %+v lastPct=%v", bifSum, lastPct)
	}
	if found := ext.FindExistingBIF(localMoviePath, localID, DefaultBIFWidth); found == "" {
		t.Fatal("expected generated BIF to be discoverable")
	}

	// Re-running GenerateAllBIFThumbnails skips existing BIF
	bifSum2, err := GenerateAllBIFThumbnails(ctx, repo, ext, nil)
	if err != nil {
		t.Fatalf("second GenerateAllBIFThumbnails: %v", err)
	}
	if bifSum2.Generated != 0 || bifSum2.Skipped != 2 {
		t.Fatalf("expected incremental skip on second BIF run, got %+v", bifSum2)
	}

	// Context cancellation stops tasks cleanly
	cancelCtx, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := ExtractAllChapterImages(cancelCtx, repo, ext, nil); err == nil {
		t.Fatal("expected context canceled error for ExtractAllChapterImages")
	}
	if _, err := GenerateAllBIFThumbnails(cancelCtx, repo, ext, nil); err == nil {
		t.Fatal("expected context canceled error for GenerateAllBIFThumbnails")
	}
}
