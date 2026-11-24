package compatmedia

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestSubtitleUploadAndRealDiskDeletion(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()

	libDir := filepath.Join(env.tmpDir, "movies")
	if err := os.MkdirAll(libDir, 0o755); err != nil {
		t.Fatal(err)
	}
	lib, err := env.repo.CreateLibrary(ctx, "Movies", "movies", []string{libDir})
	if err != nil {
		t.Fatal(err)
	}

	videoPath := filepath.Join(libDir, "Inception (2010).mkv")
	if err := os.WriteFile(videoPath, []byte("fake-video"), 0o644); err != nil {
		t.Fatal(err)
	}
	movie := &sqlite.ItemRecord{
		ID: "sub-mov-1", LibraryID: lib.ID, ParentID: lib.ID,
		Type: "Movie", MediaType: "Video", Name: "Inception", Path: videoPath,
	}
	if err := env.repo.UpsertItem(ctx, movie); err != nil {
		t.Fatal(err)
	}
	if err := env.repo.ReplaceMediaSourcesAndStreams(ctx, movie.ID, []sqlite.MediaSourceRecord{
		{
			ID: "ms-sub-1", ItemID: movie.ID, Name: "main", Path: videoPath, Container: "mkv",
			Streams: []sqlite.MediaStreamRecord{
				{MediaSourceID: "ms-sub-1", ItemID: movie.ID, StreamIndex: 0, StreamType: "Video", Codec: "h264"},
				{MediaSourceID: "ms-sub-1", ItemID: movie.ID, StreamIndex: 1, StreamType: "Subtitle", Codec: "subrip", IsExternal: false},
			},
		},
	}); err != nil {
		t.Fatal(err)
	}

	// 1. Upload external SRT subtitle via POST /Videos/{Id}/Subtitles
	srtContent := "1\n00:00:01,000 --> 00:00:03,000\nHello World\n"
	payload, _ := json.Marshal(map[string]any{
		"Language": "zh-cn",
		"Format":   "srt",
		"IsForced": true,
		"Data":     base64.StdEncoding.EncodeToString([]byte(srtContent)),
	})
	w := doReq(env.router, http.MethodPost, "/Videos/"+movie.ID+"/Subtitles", string(payload))
	if w.Code != http.StatusNoContent {
		t.Fatalf("upload subtitle via /Videos: status=%d body=%s", w.Code, w.Body.String())
	}

	expectedPath := filepath.Join(libDir, "Inception (2010).zh-cn.srt")
	gotBytes, err := os.ReadFile(expectedPath)
	if err != nil {
		t.Fatalf("expected subtitle file at %s: %v", expectedPath, err)
	}
	if string(gotBytes) != srtContent {
		t.Fatalf("unexpected subtitle content: %q", string(gotBytes))
	}

	// Verify DB row inserted with stream_index = maxStreamIndex + 1 = 2
	sources, err := env.repo.GetMediaSourcesByItemID(ctx, movie.ID)
	if err != nil || len(sources) != 1 || len(sources[0].Streams) != 3 {
		t.Fatalf("expected 3 streams after upload, got %+v (err=%v)", sources, err)
	}
	uploaded := sources[0].Streams[2]
	if uploaded.StreamIndex != 2 || !uploaded.IsExternal || !uploaded.IsForced || uploaded.Path != expectedPath {
		t.Fatalf("unexpected uploaded stream: %+v", uploaded)
	}

	// 2. Upload via POST /Items/{Id}/Subtitles with disallowed format -> 400
	badFmtPayload, _ := json.Marshal(map[string]any{
		"Language": "en",
		"Format":   "exe",
		"Data":     base64.StdEncoding.EncodeToString([]byte("nope")),
	})
	if w := doReq(env.router, http.MethodPost, "/Items/"+movie.ID+"/Subtitles", string(badFmtPayload)); w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for disallowed format, got %d", w.Code)
	}

	// 3. Upload > 5MB -> 413
	huge := bytes.Repeat([]byte("a"), 5*1024*1024+16)
	hugePayload, _ := json.Marshal(map[string]any{
		"Language": "en",
		"Format":   "srt",
		"Data":     base64.StdEncoding.EncodeToString(huge),
	})
	if w := doReq(env.router, http.MethodPost, "/Items/"+movie.ID+"/Subtitles", string(hugePayload)); w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413 for >5MB subtitle, got %d", w.Code)
	}

	// 4. Reject deleting embedded subtitle (index 1) -> 400
	if w := doReq(env.router, http.MethodDelete, "/Videos/"+movie.ID+"/Subtitles/1", ""); w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 when deleting embedded subtitle, got %d (%s)", w.Code, w.Body.String())
	}

	// 5. Reject deleting external subtitle whose path is outside library root -> 403
	outsidePath := filepath.Join(env.tmpDir, "outside.srt")
	_ = os.WriteFile(outsidePath, []byte("secret"), 0o644)
	_, err = env.store.Writer.ExecContext(ctx, `
		INSERT INTO item_media_streams (
			media_source_id, item_id, stream_index, stream_type, codec, is_external, path
		) VALUES ('ms-sub-1', ?, 9, 'Subtitle', 'srt', 1, ?)`, movie.ID, outsidePath)
	if err != nil {
		t.Fatal(err)
	}
	if w := doReq(env.router, http.MethodDelete, "/Videos/"+movie.ID+"/Subtitles/9", ""); w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for subtitle outside library root, got %d", w.Code)
	}
	if _, err := os.Stat(outsidePath); err != nil {
		t.Fatalf("outside file should NOT have been deleted: %v", err)
	}

	// 6. Delete uploaded external subtitle (index 2) -> 204, removes file on disk and DB row
	if w := doReq(env.router, http.MethodDelete, "/Videos/"+movie.ID+"/Subtitles/2", ""); w.Code != http.StatusNoContent {
		t.Fatalf("delete external subtitle: status=%d body=%s", w.Code, w.Body.String())
	}
	if _, err := os.Stat(expectedPath); !os.IsNotExist(err) {
		t.Fatalf("expected subtitle file to be removed from disk, stat err=%v", err)
	}
	sources, _ = env.repo.GetMediaSourcesByItemID(ctx, movie.ID)
	for _, st := range sources[0].Streams {
		if st.StreamIndex == 2 {
			t.Fatalf("expected DB row for stream index 2 to be deleted")
		}
	}
	// Embedded subtitle index 1 must still exist
	foundEmbedded := false
	for _, st := range sources[0].Streams {
		if st.StreamIndex == 1 && !st.IsExternal && strings.EqualFold(st.StreamType, "Subtitle") {
			foundEmbedded = true
		}
	}
	if !foundEmbedded {
		t.Fatalf("embedded subtitle stream should remain intact")
	}
}
