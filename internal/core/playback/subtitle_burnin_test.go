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
