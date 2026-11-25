package playback

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestDecide_AudioDirectPlayAndTranscodeURLs(t *testing.T) {
	tmp := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmp, "audio_playback.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	ctx := context.Background()
	repo := sqlite.NewLibraryRepo(store)
	eng := New(repo, nil, config.Default(tmp))

	itemID := guid.New()
	msID := guid.New()
	if err := repo.UpsertItem(ctx, &sqlite.ItemRecord{
		ID:        itemID,
		LibraryID: "music-lib",
		Type:      "Audio",
		MediaType: "Audio",
		Name:      "Lossless Track",
		Container: "flac",
		Path:      filepath.Join(tmp, "track.flac"),
	}); err != nil {
		t.Fatal(err)
	}
	if err := repo.ReplaceMediaSourcesAndStreams(ctx, itemID, []sqlite.MediaSourceRecord{{
		ID:        msID,
		ItemID:    itemID,
		Name:      "track.flac",
		Path:      filepath.Join(tmp, "track.flac"),
		Protocol:  "File",
		Container: "flac",
		Bitrate:   1_000_000,
		Streams: []sqlite.MediaStreamRecord{
			{StreamIndex: 0, StreamType: "Audio", Codec: "flac", Channels: 2, SampleRate: 44100, IsDefault: true},
		},
	}}); err != nil {
		t.Fatal(err)
	}

	// 1. Audio DirectPlay matches Type=="Audio" profile -> /Audio/{Id}/stream.flac?Static=true&MediaSourceId=...
	resp, details, err := eng.Decide(ctx, itemID, PlaybackInfoRequest{
		DeviceProfile: &DeviceProfile{
			Name: "Web Audio Client",
			DirectPlayProfiles: []DirectPlayProfile{
				{Type: "Video", Container: "mp4", VideoCodec: "h264", AudioCodec: "aac"},
				{Type: "Audio", Container: "flac,mp3", AudioCodec: "flac,mp3"},
			},
		},
	})
	if err != nil {
		t.Fatalf("Decide direct play: %v", err)
	}
	if len(details) != 1 || details[0].PlayMethod != PlayMethodDirectPlay {
		t.Fatalf("expected DirectPlay, got %+v", details)
	}
	wantDirectPrefix := "/Audio/" + itemID + "/stream.flac?Static=true&MediaSourceId=" + msID
	if !strings.HasPrefix(resp.MediaSources[0].DirectStreamUrl, wantDirectPrefix) {
		t.Fatalf("DirectStreamUrl = %q, want prefix %q", resp.MediaSources[0].DirectStreamUrl, wantDirectPrefix)
	}
	if resp.MediaSources[0].TranscodingUrl != "" {
		t.Fatalf("expected empty TranscodingUrl for audio DirectPlay, got %q", resp.MediaSources[0].TranscodingUrl)
	}

	// 2. Audio incompatible with Audio profile -> Transcode with /Audio/{Id}/universal?MediaSourceId=...&AudioCodec=aac
	resp2, details2, err := eng.Decide(ctx, itemID, PlaybackInfoRequest{
		DeviceProfile: &DeviceProfile{
			Name: "MP3-Only Client",
			DirectPlayProfiles: []DirectPlayProfile{
				{Type: "Video", Container: "mp4", VideoCodec: "h264", AudioCodec: "aac"},
				{Type: "Audio", Container: "mp3", AudioCodec: "mp3"},
			},
		},
	})
	if err != nil {
		t.Fatalf("Decide transcode: %v", err)
	}
	if len(details2) != 1 || details2[0].PlayMethod != PlayMethodTranscode {
		t.Fatalf("expected Transcode, got %+v", details2)
	}
	wantTranscodePrefix := "/Audio/" + itemID + "/universal?MediaSourceId=" + msID + "&AudioCodec=aac"
	if !strings.HasPrefix(resp2.MediaSources[0].TranscodingUrl, wantTranscodePrefix) {
		t.Fatalf("TranscodingUrl = %q, want prefix %q", resp2.MediaSources[0].TranscodingUrl, wantTranscodePrefix)
	}
	if strings.Contains(resp2.MediaSources[0].TranscodingUrl, "/Videos/") {
		t.Fatalf("Audio TranscodingUrl must not use /Videos/: %q", resp2.MediaSources[0].TranscodingUrl)
	}
}
