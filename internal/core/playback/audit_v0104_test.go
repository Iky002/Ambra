package playback

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestWebBrowserExternalSRTUsesStreamVTTAndNativeKeepsSRT(t *testing.T) {
	repo, engine, tmp := setupPlaybackTest(t)
	ctx := context.Background()

	movieID := guid.New()
	msID := guid.New()
	_ = repo.UpsertItem(ctx, &sqlite.ItemRecord{
		ID:        movieID,
		LibraryID: "lib1",
		Type:      "Movie",
		Name:      "SRT Subtitle Movie",
		Container: "mp4",
		Path:      filepath.Join(tmp, "movie.mp4"),
	})
	_ = repo.ReplaceMediaSourcesAndStreams(ctx, movieID, []sqlite.MediaSourceRecord{{
		ID:        msID,
		ItemID:    movieID,
		Name:      "SRT Subtitle Movie",
		Path:      filepath.Join(tmp, "movie.mp4"),
		Protocol:  "File",
		Container: "mp4",
		Streams: []sqlite.MediaStreamRecord{
			{MediaSourceID: msID, ItemID: movieID, StreamIndex: 0, StreamType: "Video", Codec: "h264"},
			{MediaSourceID: msID, ItemID: movieID, StreamIndex: 1, StreamType: "Audio", Codec: "aac", IsDefault: true},
			{MediaSourceID: msID, ItemID: movieID, StreamIndex: 2, StreamType: "Subtitle", Codec: "srt", IsExternal: true, Path: filepath.Join(tmp, "movie.chs.srt")},
		},
	}})

	// 1. HTML5 Web browser profile -> external .srt MUST be delivered as Stream.vtt
	webProfile := &DeviceProfile{
		Name: "Ambra Web",
		DirectPlayProfiles: []DirectPlayProfile{
			{Container: "mp4", VideoCodec: "h264", AudioCodec: "aac", Type: "Video"},
		},
		SubtitleProfiles: []SubtitleProfile{
			{Format: "vtt", Method: "External"},
			{Format: "srt", Method: "External"},
		},
	}
	subIdx := 2
	respWeb, detailsWeb, err := engine.Decide(ctx, movieID, PlaybackInfoRequest{
		SubtitleStreamIndex: &subIdx,
		DeviceProfile:       webProfile,
	})
	if err != nil {
		t.Fatalf("Decide web: %v", err)
	}
	if detailsWeb[0].PlayMethod != PlayMethodDirectPlay {
		t.Fatalf("expected DirectPlay for web SRT->VTT, got %+v", detailsWeb[0])
	}
	webURL := respWeb.MediaSources[0].MediaStreams[2].DeliveryUrl
	if !strings.HasSuffix(webURL, "/Subtitles/2/Stream.vtt") {
		t.Fatalf("expected HTML5 Web browser external .srt DeliveryUrl to end with Stream.vtt, got %q", webURL)
	}

	// 2. Native player profile declaring srt External -> keeps Stream.srt
	nativeProfile := &DeviceProfile{
		Name: "Infuse iOS",
		DirectPlayProfiles: []DirectPlayProfile{
			{Container: "mp4", VideoCodec: "h264", AudioCodec: "aac", Type: "Video"},
		},
		SubtitleProfiles: []SubtitleProfile{
			{Format: "srt", Method: "External"},
		},
	}
	respNative, _, err := engine.Decide(ctx, movieID, PlaybackInfoRequest{
		SubtitleStreamIndex: &subIdx,
		DeviceProfile:       nativeProfile,
	})
	if err != nil {
		t.Fatalf("Decide native: %v", err)
	}
	nativeURL := respNative.MediaSources[0].MediaStreams[2].DeliveryUrl
	if !strings.HasSuffix(nativeURL, "/Subtitles/2/Stream.srt") {
		t.Fatalf("expected native player external .srt DeliveryUrl to end with Stream.srt, got %q", nativeURL)
	}
}

func TestServeSubtitleSupportsShortWebVTTTimestamps(t *testing.T) {
	dir := t.TempDir()
	vttPath := filepath.Join(dir, "short.vtt")
	vttContent := "WEBVTT\n\n01:02.345 --> 01:05.678\nShort timestamp line\n\n00:02:10.000 --> 00:02:12.500\nLong timestamp line\n"
	if err := os.WriteFile(vttPath, []byte(vttContent), 0o644); err != nil {
		t.Fatal(err)
	}

	// Convert WebVTT with MM:SS.mmm to Emby .js JSON and verify both cues are preserved.
	ct, jsBytes, err := ServeSubtitle(vttPath, "js", 0)
	if err != nil || !strings.Contains(ct, "application/json") {
		t.Fatalf("ServeSubtitle vtt->js failed: ct=%q err=%v", ct, err)
	}
	var track SubtitleTrackInfo
	if err := json.Unmarshal(jsBytes, &track); err != nil {
		t.Fatalf("unmarshal TrackEvents: %v", err)
	}
	if len(track.TrackEvents) != 2 {
		t.Fatalf("expected 2 cues (including MM:SS.mmm), got %d: %+v", len(track.TrackEvents), track.TrackEvents)
	}
	// 01:02.345 = 62.345s = 623_450_000 ticks
	if track.TrackEvents[0].StartPositionTicks != 623_450_000 {
		t.Fatalf("unexpected start ticks for 01:02.345: got %d, want 623450000", track.TrackEvents[0].StartPositionTicks)
	}
	if track.TrackEvents[0].Text != "Short timestamp line" {
		t.Fatalf("unexpected first cue text: %q", track.TrackEvents[0].Text)
	}
}

func TestSupportsClientWASMOnlyForWebClients(t *testing.T) {
	webCases := []*DeviceProfile{
		{Name: "Ambra Web"},
		{Name: "Emby Web"},
		{Name: "Chrome Browser", SupportsClientWASM: true},
		{Name: "Custom WASM Player"},
	}
	for _, p := range webCases {
		if !supportsClientWASM(p) {
			t.Errorf("expected supportsClientWASM(%+v) == true", p)
		}
	}

	nativeCases := []*DeviceProfile{
		nil,
		{Name: ""},
		{Name: "Ambra iOS", SupportsClientWASM: true},
		{Name: "Ambra Android", SupportsClientWASM: true},
		{Name: "Apple TV", SupportsClientWASM: true},
		{Name: "Android TV"},
		{Name: "tvOS"},
		{Name: "Infuse"},
		{Name: "Kodi"},
		{Name: "Roku"},
		{Name: "Swiftfin"},
		{Name: "VLC"},
	}
	for _, p := range nativeCases {
		if supportsClientWASM(p) {
			t.Errorf("expected supportsClientWASM(%+v) == false for native/non-web client", p)
		}
	}
}
