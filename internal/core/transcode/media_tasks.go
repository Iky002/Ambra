package transcode

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/ambra-media/ambra/internal/store/sqlite"
)

// ChapterTaskSummary records the counters of an ExtractAllChapterImages scheduled task run.
type ChapterTaskSummary struct {
	ItemsScanned int
	Extracted    int
	Skipped      int
	Failed       int
}

// BIFTaskSummary records the counters of a GenerateAllBIFThumbnails scheduled task run.
type BIFTaskSummary struct {
	ItemsScanned int
	Generated    int
	Skipped      int
	Failed       int
}

func isLocalMediaFile(mediaPath string) bool {
	p := strings.TrimSpace(mediaPath)
	if p == "" || isHTTPInput(p) || strings.EqualFold(filepath.Ext(p), ".strm") {
		return false
	}
	info, err := os.Stat(p)
	return err == nil && !info.IsDir()
}

// ExtractAllChapterImages runs as a scheduled background task to extract chapter thumbnails
// for all local video items into the dedicated chapter cache directory (<DataDir>/cache/chapters).
// Existing valid chapter images are skipped incrementally; remote/.strm media are skipped.
func ExtractAllChapterImages(
	ctx context.Context,
	repo *sqlite.LibraryRepo,
	ext *Extractor,
	reportProgress func(float64),
) (ChapterTaskSummary, error) {
	var summary ChapterTaskSummary
	if repo == nil || ext == nil {
		if reportProgress != nil {
			reportProgress(100)
		}
		return summary, nil
	}

	candidates, err := repo.ListChapterExtractionCandidates(ctx)
	if err != nil {
		return summary, fmt.Errorf("list chapter extraction candidates: %w", err)
	}
	summary.ItemsScanned = len(candidates)

	totalChapters := 0
	for _, c := range candidates {
		totalChapters += len(c.Chapters)
	}
	if totalChapters == 0 {
		if reportProgress != nil {
			reportProgress(100)
		}
		return summary, nil
	}

	doneChapters := 0
	emitProgress := func() {
		if reportProgress != nil {
			reportProgress(float64(doneChapters) / float64(totalChapters) * 100)
		}
	}

	for _, item := range candidates {
		select {
		case <-ctx.Done():
			return summary, ctx.Err()
		default:
		}
		if !isLocalMediaFile(item.MediaPath) {
			summary.Skipped += len(item.Chapters)
			doneChapters += len(item.Chapters)
			emitProgress()
			continue
		}

		for idx, ch := range item.Chapters {
			select {
			case <-ctx.Done():
				return summary, ctx.Err()
			default:
			}

			// Skip if DB record already exists and the cached file is still present on disk.
			if existing, imgErr := repo.GetImageByItemAndType(ctx, item.ItemID, "Chapter", idx); imgErr == nil && existing != nil && fileReady(existing.Path) {
				summary.Skipped++
				doneChapters++
				emitProgress()
				continue
			}

			thumbPath, extErr := ext.ExtractChapterThumbnail(ctx, item.MediaPath, item.ItemID, idx, ch.StartPositionTicks)
			if extErr != nil {
				if ctx.Err() != nil {
					return summary, ctx.Err()
				}
				summary.Failed++
				slog.Warn("scheduled chapter thumbnail extraction failed",
					"item_id", item.ItemID, "chapter_index", idx, "error", extErr)
				doneChapters++
				emitProgress()
				continue
			}

			sum := sha256.Sum256([]byte(fmt.Sprintf("chapter:%s:%d:%d", item.ItemID, idx, ch.StartPositionTicks)))
			tag := hex.EncodeToString(sum[:8])
			if err := repo.UpsertImage(ctx, &sqlite.ImageRecord{
				ItemID:     item.ItemID,
				ImageType:  "Chapter",
				ImageIndex: idx,
				Path:       thumbPath,
				Tag:        tag,
			}); err != nil {
				summary.Failed++
				slog.Warn("failed to persist extracted chapter image record",
					"item_id", item.ItemID, "chapter_index", idx, "error", err)
			} else {
				summary.Extracted++
			}
			doneChapters++
			emitProgress()
		}
	}

	if reportProgress != nil {
		reportProgress(100)
	}
	return summary, nil
}

// GenerateAllBIFThumbnails runs as a scheduled background task to generate standard Roku/Emby
// BIF seeking preview files into the dedicated BIF cache directory (<DataDir>/cache/bif).
// Items that already have a sidecar or cached BIF, and remote/.strm media, are skipped.
func GenerateAllBIFThumbnails(
	ctx context.Context,
	repo *sqlite.LibraryRepo,
	ext *Extractor,
	reportProgress func(float64),
) (BIFTaskSummary, error) {
	var summary BIFTaskSummary
	if repo == nil || ext == nil {
		if reportProgress != nil {
			reportProgress(100)
		}
		return summary, nil
	}

	candidates, err := repo.ListBIFCandidates(ctx)
	if err != nil {
		return summary, fmt.Errorf("list bif candidates: %w", err)
	}
	summary.ItemsScanned = len(candidates)
	if len(candidates) == 0 {
		if reportProgress != nil {
			reportProgress(100)
		}
		return summary, nil
	}

	for i, item := range candidates {
		select {
		case <-ctx.Done():
			return summary, ctx.Err()
		default:
		}

		if !isLocalMediaFile(item.MediaPath) {
			summary.Skipped++
		} else if existing := ext.FindExistingBIF(item.MediaPath, item.ItemID, DefaultBIFWidth); existing != "" {
			summary.Skipped++
		} else {
			if _, genErr := ext.GenerateBIF(ctx, item.MediaPath, item.ItemID, DefaultBIFWidth, DefaultBIFIntervalSec); genErr != nil {
				if ctx.Err() != nil {
					return summary, ctx.Err()
				}
				summary.Failed++
				slog.Warn("scheduled BIF generation failed",
					"item_id", item.ItemID, "path", item.MediaPath, "error", genErr)
			} else {
				summary.Generated++
			}
		}

		if reportProgress != nil {
			reportProgress(float64(i+1) / float64(len(candidates)) * 100)
		}
	}

	if reportProgress != nil {
		reportProgress(100)
	}
	return summary, nil
}
