package playback

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/plugins"
	"github.com/ambra-media/ambra/internal/plugins/strm"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func setupPlaybackTest(t *testing.T) (*sqlite.LibraryRepo, *Engine, string) {
	t.Helper()
	tmp := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmp, "pb.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	repo := sqlite.NewLibraryRepo(store)
	reg := plugins.NewRegistry()
	reg.Register(strm.New(true))
	cfg := config.Default(tmp)
	return repo, New(repo, reg, cfg), tmp
}

func TestFiveTierPlaybackDecisions(t *testing.T) {
	repo, engine, tmp := setupPlaybackTest(t)
	ctx := context.Background()

	// 1. STRM 302 DirectPlay
	strmFile := filepath.Join(tmp, "remote.strm")
	_ = os.WriteFile(strmFile, []byte("https://cdn.example.com/4k.mkv\n"), 0o644)
	strmItemID := guid.New()
	_ = repo.UpsertItem(ctx, &sqlite.ItemRecord{
		ID:        strmItemID,
		LibraryID: "lib1",
		Type:      "Movie",
		IsStrm:    true,
		Name:      "Remote 4K",
		Container: "strm",
		Path:      strmFile,
	})
	_ = repo.ReplaceMediaSourcesAndStreams(ctx, strmItemID, []sqlite.MediaSourceRecord{{
		ID:        guid.New(),
		ItemID:    strmItemID,
		Name:      "Remote 4K",
		Path:      strmFile,
		Protocol:  "Http",
		Container: "strm",
		IsRemote:  true,
	}})

	resp, details, err := engine.Decide(ctx, strmItemID, PlaybackInfoRequest{})
	if err != nil || len(details) != 1 {
		t.Fatalf("STRM decide failed: err=%v details=%+v", err, details)
	}
	if details[0].PlayMethod != PlayMethodSTRM302 || !resp.MediaSources[0].SupportsDirectPlay || resp.MediaSources[0].SupportsTranscoding {
		t.Fatalf("expected STRM302 DirectPlay, got detail=%+v ms=%+v", details[0], resp.MediaSources[0])
	}

	// 2. Movie with ASS and PGS subtitles: Ambra Web profile declares ass/pgssub as External -> MUST DirectPlay (never transcode!)
	movieID := guid.New()
	msID := guid.New()
	_ = repo.UpsertItem(ctx, &sqlite.ItemRecord{
		ID:        movieID,
		LibraryID: "lib1",
		Type:      "Movie",
		Name:      "Anime Movie",
		Container: "mp4",
		Path:      filepath.Join(tmp, "anime.mp4"),
	})
	_ = repo.ReplaceMediaSourcesAndStreams(ctx, movieID, []sqlite.MediaSourceRecord{{
		ID:        msID,
		ItemID:    movieID,
		Name:      "Anime Movie",
		Path:      filepath.Join(tmp, "anime.mp4"),
		Protocol:  "File",
		Container: "mp4",
		Bitrate:   5_000_000,
		Streams: []sqlite.MediaStreamRecord{
			{MediaSourceID: msID, ItemID: movieID, StreamIndex: 0, StreamType: "Video", Codec: "h264"},
			{MediaSourceID: msID, ItemID: movieID, StreamIndex: 1, StreamType: "Audio", Codec: "aac", IsDefault: true},
			{MediaSourceID: msID, ItemID: movieID, StreamIndex: 2, StreamType: "Subtitle", Codec: "ass", SubVariant: "chs", DisplayTitle: "简体中文 (ASS)", IsExternal: true, Path: "/tmp/a.ass"},
			{MediaSourceID: msID, ItemID: movieID, StreamIndex: 3, StreamType: "Subtitle", Codec: "pgssub", SubVariant: "cht", DisplayTitle: "繁体中文 (PGS)", IsExternal: true, Path: "/tmp/a.sup"},
		},
	}})

	ambraWebProfile := &DeviceProfile{
		Name:                "Ambra Web",
		MaxStreamingBitrate: 40_000_000,
		SupportsClientWASM:  true,
		DirectPlayProfiles: []DirectPlayProfile{
			{Container: "mp4,webm", VideoCodec: "h264,hevc", AudioCodec: "aac,mp3", Type: "Video"},
		},
		SubtitleProfiles: []SubtitleProfile{
			{Format: "ass", Method: "External"},
			{Format: "ssa", Method: "External"},
			{Format: "pgssub", Method: "External"},
			{Format: "vtt", Method: "External"},
		},
	}

	assIdx := 2
	resp2, details2, err := engine.Decide(ctx, movieID, PlaybackInfoRequest{
		SubtitleStreamIndex: &assIdx,
		DeviceProfile:       ambraWebProfile,
	})
	if err != nil {
		t.Fatal(err)
	}
	if details2[0].PlayMethod != PlayMethodDirectPlay || !resp2.MediaSources[0].SupportsDirectPlay {
		t.Fatalf("ASS subtitle must NOT trigger transcode; got detail=%+v", details2[0])
	}
	if !strings.Contains(resp2.MediaSources[0].MediaStreams[2].DeliveryUrl, "/Subtitles/2/Stream.ass") {
		t.Fatalf("unexpected ASS DeliveryUrl: %s", resp2.MediaSources[0].MediaStreams[2].DeliveryUrl)
	}

	pgsIdx := 3
	resp3, details3, err := engine.Decide(ctx, movieID, PlaybackInfoRequest{
		SubtitleStreamIndex: &pgsIdx,
		DeviceProfile:       ambraWebProfile,
	})
	if err != nil {
		t.Fatal(err)
	}
	if details3[0].PlayMethod != PlayMethodDirectPlay || !resp3.MediaSources[0].SupportsDirectPlay {
		t.Fatalf("PGS subtitle must NOT trigger transcode when profile declares External; got detail=%+v", details3[0])
	}
	if !strings.Contains(resp3.MediaSources[0].MediaStreams[3].DeliveryUrl, "/Subtitles/3/Stream.sup") {
		t.Fatalf("unexpected PGS DeliveryUrl: %s", resp3.MediaSources[0].MediaStreams[3].DeliveryUrl)
	}

	// 3. MKV container + Client WASM -> ClientWASM (zero server transcode)
	mkvID := guid.New()
	mkvMS := guid.New()
	_ = repo.UpsertItem(ctx, &sqlite.ItemRecord{
		ID:        mkvID,
		LibraryID: "lib1",
		Type:      "Movie",
		Name:      "MKV Movie",
		Container: "mkv",
		Path:      filepath.Join(tmp, "movie.mkv"),
	})
	_ = repo.ReplaceMediaSourcesAndStreams(ctx, mkvID, []sqlite.MediaSourceRecord{{
		ID:        mkvMS,
		ItemID:    mkvID,
		Name:      "MKV Movie",
		Path:      filepath.Join(tmp, "movie.mkv"),
		Container: "mkv",
		Streams: []sqlite.MediaStreamRecord{
			{MediaSourceID: mkvMS, ItemID: mkvID, StreamIndex: 0, StreamType: "Video", Codec: "h264"},
			{MediaSourceID: mkvMS, ItemID: mkvID, StreamIndex: 1, StreamType: "Audio", Codec: "ac3"},
		},
	}})

	_, detailsWASM, err := engine.Decide(ctx, mkvID, PlaybackInfoRequest{DeviceProfile: ambraWebProfile})
	if err != nil || detailsWASM[0].PlayMethod != PlayMethodClientWASM {
		t.Fatalf("expected ClientWASM for MKV on Ambra Web, got %+v err=%v", detailsWASM, err)
	}

	// 4. MKV with h264 on non-WASM browser (mp4 only) -> DirectStream (VideoCodec=copy)
	legacyProfile := &DeviceProfile{
		Name: "Legacy Browser",
		DirectPlayProfiles: []DirectPlayProfile{
			{Container: "mp4", VideoCodec: "h264", AudioCodec: "aac", Type: "Video"},
		},
		SubtitleProfiles: []SubtitleProfile{
			{Format: "vtt", Method: "External"},
		},
	}
	respDS, detailsDS, err := engine.Decide(ctx, mkvID, PlaybackInfoRequest{DeviceProfile: legacyProfile})
	if err != nil || detailsDS[0].PlayMethod != PlayMethodDirectStream || !strings.Contains(respDS.MediaSources[0].TranscodingUrl, "VideoCodec=copy") {
		t.Fatalf("expected DirectStream, got detail=%+v url=%s", detailsDS, respDS.MediaSources[0].TranscodingUrl)
	}

	// 5. Legacy client selecting PGS (not in SubtitleProfiles) -> Transcode burn-in
	respTC, detailsTC, err := engine.Decide(ctx, movieID, PlaybackInfoRequest{
		SubtitleStreamIndex: &pgsIdx,
		DeviceProfile:       legacyProfile,
	})
	if err != nil || detailsTC[0].PlayMethod != PlayMethodTranscode || !strings.Contains(respTC.MediaSources[0].TranscodingUrl, "VideoCodec=h264") {
		t.Fatalf("expected Transcode for unsupported PGS on legacy client, got detail=%+v url=%s", detailsTC, respTC.MediaSources[0].TranscodingUrl)
	}
}

func TestServeSubtitleConversions(t *testing.T) {
	dir := t.TempDir()
	srtPath := filepath.Join(dir, "sample.chs.srt")
	srt := "1\n00:00:05,000 --> 00:00:08,500\n你好，世界！\n\n2\n00:00:10,000 --> 00:00:12,000\n第二行字幕\n"
	_ = os.WriteFile(srtPath, []byte(srt), 0o644)

	// SRT -> VTT
	ct, vttBytes, err := ServeSubtitle(srtPath, "vtt", 0)
	if err != nil || !strings.Contains(ct, "text/vtt") {
		t.Fatalf("SRT->VTT failed: ct=%s err=%v", ct, err)
	}
	vtt := string(vttBytes)
	if !strings.HasPrefix(vtt, "WEBVTT") || !strings.Contains(vtt, "00:00:05.000 --> 00:00:08.500") || !strings.Contains(vtt, "你好，世界！") {
		t.Fatalf("unexpected VTT output:\n%s", vtt)
	}

	// SRT -> Emby .js JSON
	ctJS, jsBytes, err := ServeSubtitle(srtPath, "js", 0)
	if err != nil || !strings.Contains(ctJS, "application/json") {
		t.Fatalf("SRT->JS failed: ct=%s err=%v", ctJS, err)
	}
	var track SubtitleTrackInfo
	if err := json.Unmarshal(jsBytes, &track); err != nil || len(track.TrackEvents) != 2 {
		t.Fatalf("unexpected JS subtitle: %s err=%v", string(jsBytes), err)
	}

	// ASS passthrough
	assPath := filepath.Join(dir, "sample.chs.ass")
	assContent := "[Script Info]\nTitle: Test\n"
	_ = os.WriteFile(assPath, []byte(assContent), 0o644)
	ctASS, assOut, err := ServeSubtitle(assPath, "ass", 0)
	if err != nil || !strings.Contains(ctASS, "text/x-ssa") || string(assOut) != assContent {
		t.Fatalf("ASS passthrough failed: ct=%s out=%q err=%v", ctASS, string(assOut), err)
	}
}
