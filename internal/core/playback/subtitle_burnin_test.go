package playback

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/plugins"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestDecideSubtitleBurnInTranscodingURL(t *testing.T) {
	tmp := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmp, "burnin.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	ctx := context.Background()
	repo := sqlite.NewLibraryRepo(store)
	lib, err := repo.CreateLibrary(ctx, "Movies", "movies", []string{tmp})
	if err != nil {
		t.Fatal(err)
	}

	itemID := guid.New()
	msID := guid.New()
	if err := repo.UpsertItem(ctx, &sqlite.ItemRecord{
		ID:        itemID,
		LibraryID: lib.ID,
		ParentID:  lib.ID,
		Type:      "Movie",
		MediaType: "Video",
		Name:      "BurnInMovie",
		Path:      filepath.Join(tmp, "movie.mkv"),
		Container: "mkv",
	}); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpsertMediaSourceAndStreams(ctx, itemID, sqlite.MediaSourceRecord{
		ID:        msID,
		ItemID:    itemID,
		Name:      "movie.mkv",
		Path:      filepath.Join(tmp, "movie.mkv"),
		Protocol:  "File",
		Container: "mkv",
		Streams: []sqlite.MediaStreamRecord{
			{StreamIndex: 0, StreamType: "Video", Codec: "h264", Width: 1920, Height: 1080},
			{StreamIndex: 1, StreamType: "Audio", Codec: "aac", Language: "eng", IsDefault: true},
			{StreamIndex: 2, StreamType: "Audio", Codec: "aac", Language: "jpn"},
			{StreamIndex: 3, StreamType: "Subtitle", Codec: "pgssub", Language: "chi"},
		},
	}); err != nil {
		t.Fatal(err)
	}

	eng := New(repo, plugins.NewRegistry(), config.Default(tmp))
	audioIdx := 2
	subIdx := 3
	resp, details, err := eng.Decide(ctx, itemID, PlaybackInfoRequest{
		AudioStreamIndex:    &audioIdx,
		SubtitleStreamIndex: &subIdx,
		DeviceProfile: &DeviceProfile{
			Name: "StrictWeb",
			DirectPlayProfiles: []DirectPlayProfile{
				{Type: "Video", Container: "mkv,mp4", VideoCodec: "h264", AudioCodec: "aac"},
			},
			SubtitleProfiles: []SubtitleProfile{
				{Format: "vtt", Method: "External"},
				{Format: "pgssub", Method: "Encode"},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(details) != 1 || details[0].PlayMethod != PlayMethodTranscode {
		t.Fatalf("expected PlayMethodTranscode, got %+v", details)
	}
	url := resp.MediaSources[0].TranscodingUrl
	for _, want := range []string{
		"SubtitleStreamIndex=3",
		"SubtitleMethod=Encode",
		"AudioStreamIndex=2",
	} {
		if !strings.Contains(url, want) {
			t.Fatalf("expected TranscodingUrl %q to contain %q", url, want)
		}
	}
}

func TestDecideTranscodingURLParamsDirectStreamAndTranscode(t *testing.T) {
	tmp := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmp, "params.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	ctx := context.Background()
	repo := sqlite.NewLibraryRepo(store)
	lib, err := repo.CreateLibrary(ctx, "Movies", "movies", []string{tmp})
	if err != nil {
		t.Fatal(err)
	}

	itemID := guid.New()
	msID := guid.New()
	_ = repo.UpsertItem(ctx, &sqlite.ItemRecord{
		ID: itemID, LibraryID: lib.ID, ParentID: lib.ID,
		Type: "Movie", MediaType: "Video", Name: "ParamsMovie",
		Path: filepath.Join(tmp, "movie.mkv"), Container: "mkv",
	})
	_ = repo.UpsertMediaSourceAndStreams(ctx, itemID, sqlite.MediaSourceRecord{
		ID: msID, ItemID: itemID, Name: "movie.mkv",
		Path: filepath.Join(tmp, "movie.mkv"), Protocol: "File", Container: "mkv", Bitrate: 8_000_000,
		Streams: []sqlite.MediaStreamRecord{
			{StreamIndex: 0, StreamType: "Video", Codec: "h264", Width: 1920, Height: 1080},
			{StreamIndex: 1, StreamType: "Audio", Codec: "dts", Language: "eng", IsDefault: true},
			{StreamIndex: 2, StreamType: "Audio", Codec: "dts", Language: "jpn"},
		},
	})

	eng := New(repo, plugins.NewRegistry(), config.Default(tmp))
	audioIdx := 2

	// Tier 4 DirectStream (h264 OK, mkv/dts not direct-playable, bitrate & resolution OK)
	respDS, detDS, err := eng.Decide(ctx, itemID, PlaybackInfoRequest{
		AudioStreamIndex:    &audioIdx,
		MaxStreamingBitrate: 10_000_000,
		MaxWidth:            1920,
		MaxHeight:           1080,
		DeviceProfile: &DeviceProfile{
			Name: "WebBrowser",
			DirectPlayProfiles: []DirectPlayProfile{
				{Type: "Video", Container: "mp4", VideoCodec: "h264", AudioCodec: "aac"},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(detDS) != 1 || detDS[0].PlayMethod != PlayMethodDirectStream {
		t.Fatalf("expected DirectStream, got %+v", detDS)
	}
	dsURL := respDS.MediaSources[0].TranscodingUrl
	for _, want := range []string{
		"VideoCodec=copy",
		"AudioStreamIndex=2",
		"VideoBitrate=9808000",
		"MaxWidth=1920",
		"MaxHeight=1080",
	} {
		if !strings.Contains(dsURL, want) {
			t.Fatalf("DirectStream TranscodingUrl %q missing %q", dsURL, want)
		}
	}

	// Tier 5 Transcode (bitrate exceeded + low bitrate floor)
	respTC, detTC, err := eng.Decide(ctx, itemID, PlaybackInfoRequest{
		AudioStreamIndex:    &audioIdx,
		MaxStreamingBitrate: 300_000,
		MaxWidth:            640,
		DeviceProfile: &DeviceProfile{
			Name: "WebBrowser",
			DirectPlayProfiles: []DirectPlayProfile{
				{Type: "Video", Container: "mp4", VideoCodec: "h264", AudioCodec: "aac"},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(detTC) != 1 || detTC[0].PlayMethod != PlayMethodTranscode {
		t.Fatalf("expected Transcode, got %+v", detTC)
	}
	tcURL := respTC.MediaSources[0].TranscodingUrl
	for _, want := range []string{
		"VideoCodec=h264",
		"AudioStreamIndex=2",
		"VideoBitrate=256000",
		"MaxWidth=640",
	} {
		if !strings.Contains(tcURL, want) {
			t.Fatalf("Transcode TranscodingUrl %q missing %q", tcURL, want)
		}
	}
	if strings.Contains(tcURL, "MaxHeight=") {
		t.Fatalf("did not expect MaxHeight in %q", tcURL)
	}
}
