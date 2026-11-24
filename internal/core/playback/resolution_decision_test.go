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

func TestDecideMaxResolutionForcesTranscode(t *testing.T) {
	tmp := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmp, "res_decision.db"))
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
		Name:      "HDMovie",
		Path:      filepath.Join(tmp, "hd.mp4"),
		Container: "mp4",
	}); err != nil {
		t.Fatal(err)
	}
	// Bitrate is 0 (unknown) and container/codecs are fully DirectPlay-compatible.
	if err := repo.UpsertMediaSourceAndStreams(ctx, itemID, sqlite.MediaSourceRecord{
		ID:        msID,
		ItemID:    itemID,
		Name:      "hd.mp4",
		Path:      filepath.Join(tmp, "hd.mp4"),
		Protocol:  "File",
		Container: "mp4",
		Bitrate:   0,
		Streams: []sqlite.MediaStreamRecord{
			{StreamIndex: 0, StreamType: "Video", Codec: "h264", Width: 1920, Height: 1080},
			{StreamIndex: 1, StreamType: "Audio", Codec: "aac", Language: "eng", IsDefault: true},
		},
	}); err != nil {
		t.Fatal(err)
	}

	eng := New(repo, plugins.NewRegistry(), config.Default(tmp))
	profile := &DeviceProfile{
		Name:               "AmbraWeb",
		SupportsClientWASM: true,
		DirectPlayProfiles: []DirectPlayProfile{
			{Type: "Video", Container: "mp4,mkv", VideoCodec: "h264", AudioCodec: "aac"},
		},
	}

	// 1. MaxWidth < source width -> must force Tier 5 Transcode even with unknown bitrate & WASM support.
	resp, details, err := eng.Decide(ctx, itemID, PlaybackInfoRequest{
		MaxWidth:      1280,
		DeviceProfile: profile,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(details) != 1 || details[0].PlayMethod != PlayMethodTranscode {
		t.Fatalf("expected PlayMethodTranscode for MaxWidth=1280 on 1920x1080, got %+v", details)
	}
	if !strings.Contains(resp.MediaSources[0].TranscodingUrl, "MaxWidth=1280") {
		t.Fatalf("expected MaxWidth=1280 in TranscodingUrl, got %q", resp.MediaSources[0].TranscodingUrl)
	}

	// 2. MaxHeight < source height -> must force Tier 5 Transcode even when bitrate is lower than MaxStreamingBitrate.
	resp, details, err = eng.Decide(ctx, itemID, PlaybackInfoRequest{
		MaxStreamingBitrate: 50_000_000,
		MaxHeight:           720,
		DeviceProfile:       profile,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(details) != 1 || details[0].PlayMethod != PlayMethodTranscode {
		t.Fatalf("expected PlayMethodTranscode for MaxHeight=720 on 1920x1080, got %+v", details)
	}
	if !strings.Contains(resp.MediaSources[0].TranscodingUrl, "MaxHeight=720") {
		t.Fatalf("expected MaxHeight=720 in TranscodingUrl, got %q", resp.MediaSources[0].TranscodingUrl)
	}

	// 3. MaxWidth/MaxHeight >= source resolution -> DirectPlay allowed.
	_, details, err = eng.Decide(ctx, itemID, PlaybackInfoRequest{
		MaxWidth:      1920,
		MaxHeight:     1080,
		DeviceProfile: profile,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(details) != 1 || details[0].PlayMethod != PlayMethodDirectPlay {
		t.Fatalf("expected PlayMethodDirectPlay when within resolution bounds, got %+v", details)
	}
}
