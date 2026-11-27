package playback

import (
	"context"
	"strings"
	"testing"

	"github.com/ambra-media/ambra/internal/api/dto"
	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/plugins"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestAuditV0107_M8_DefaultSubtitleBurnInAndDefaultAudioTranscodeParam(t *testing.T) {
	tmp := t.TempDir()
	store, err := sqlite.Open(tmp + "/test.db")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	repo := sqlite.NewLibraryRepo(store)
	userRepo := sqlite.NewUserRepo(store)
	ctx := context.Background()

	lib, err := repo.CreateLibrary(ctx, "Movies", "movies", []string{"/movies"})
	if err != nil {
		t.Fatal(err)
	}
	item := &sqlite.ItemRecord{
		ID: "mov1", LibraryID: lib.ID, Type: "Movie", MediaType: "Video",
		Name: "Anime", Path: "/movies/anime.mkv", Container: "mkv",
	}
	if err := repo.UpsertItem(ctx, item); err != nil {
		t.Fatal(err)
	}
	ms := sqlite.MediaSourceRecord{
		ID: "ms1", ItemID: item.ID, Name: "1080p", Path: "/movies/anime.mkv",
		Protocol: "File", Container: "mkv",
		SupportsDirectPlay: true, SupportsDirectStream: true, SupportsTranscoding: true,
		Streams: []sqlite.MediaStreamRecord{
			{MediaSourceID: "ms1", ItemID: item.ID, StreamIndex: 0, StreamType: "Video", Codec: "h264", Width: 1920, Height: 1080},
			{MediaSourceID: "ms1", ItemID: item.ID, StreamIndex: 1, StreamType: "Audio", Codec: "aac", Language: "eng"},
			{MediaSourceID: "ms1", ItemID: item.ID, StreamIndex: 2, StreamType: "Audio", Codec: "aac", Language: "jpn", IsDefault: true},
			{MediaSourceID: "ms1", ItemID: item.ID, StreamIndex: 3, StreamType: "Subtitle", Codec: "dvd_subtitle", Language: "chi", IsDefault: true},
		},
	}
	if err := repo.UpsertMediaSourceAndStreams(ctx, item.ID, ms); err != nil {
		t.Fatal(err)
	}

	eng := New(repo, plugins.NewRegistry(), config.Default(tmp))
	eng.SetUserRepo(userRepo)

	// M-8 + M-18: req.SubtitleStreamIndex == nil and req.AudioStreamIndex == nil
	// Default subtitle (dvd_subtitle) is selected via DefaultSubtitleStreamIndex, requires Encode -> triggers burn-in transcode
	// and TranscodingUrl includes DefaultAudioStreamIndex (2) & SubtitleStreamIndex=3&SubtitleMethod=Encode.
	resp, details, err := eng.Decide(ctx, item.ID, PlaybackInfoRequest{
		DeviceProfile: &DeviceProfile{
			Name: "Web Browser",
			DirectPlayProfiles: []DirectPlayProfile{
				{Type: "Video", Container: "mkv", VideoCodec: "h264", AudioCodec: "aac"},
			},
			SubtitleProfiles: []SubtitleProfile{
				{Format: "vtt", Method: "External"},
			},
		},
	})
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if len(details) != 1 || details[0].PlayMethod != PlayMethodTranscode {
		t.Fatalf("expected Transcode for default dvd_subtitle burn-in, got %+v", details)
	}
	tcURL := resp.MediaSources[0].TranscodingUrl
	if !strings.Contains(tcURL, "SubtitleStreamIndex=3&SubtitleMethod=Encode") {
		t.Fatalf("expected default subtitle burn-in params in TranscodingUrl, got %q", tcURL)
	}
	if !strings.Contains(tcURL, "AudioStreamIndex=2") {
		t.Fatalf("expected fallback DefaultAudioStreamIndex=2 in TranscodingUrl, got %q", tcURL)
	}
}

func TestAuditV0107_M9_AudioLocaleAndSubtitleFalsePositive(t *testing.T) {
	// M-9a: selectAudioTrack normalizes BCP-47 locale preferences like "zh-CN" and "ja-JP"
	res := SelectDefaultTracks(TrackSelectionInput{
		Streams: []dto.MediaStream{
			{Index: 1, Type: "Audio", Language: "eng", IsDefault: true},
			{Index: 2, Type: "Audio", Language: "jpn"},
		},
		UserConfig: &dto.UserConfiguration{
			AudioLanguagePreference: "ja-JP",
		},
	})
	if res.AudioStreamIndex == nil || *res.AudioStreamIndex != 2 {
		t.Fatalf("expected ja-JP preference to select jpn audio track 2, got %v", res.AudioStreamIndex)
	}

	// M-9b: detectSubChineseVariant ignores parent dir tokens & en-GB markers
	cases := []struct {
		name string
		st   dto.MediaStream
		want string
	}{
		{
			name: "en-GB language and filename must not match gb/chs",
			st:   dto.MediaStream{Language: "en-GB", Path: "/media/GB_Archive/Movie.en-GB.srt"},
			want: "",
		},
		{
			name: "parent directory SC/TC must not taint english subtitle",
			st:   dto.MediaStream{Language: "eng", Path: "/media/SC/TC/Movie.eng.srt"},
			want: "",
		},
		{
			name: "untyped filename en_gb must not match gb/chs",
			st:   dto.MediaStream{Path: "/media/Movie.en_gb.srt"},
			want: "",
		},
		{
			name: "genuine chinese sc subtitle still detected",
			st:   dto.MediaStream{Language: "chi", Path: "/media/Movie.sc.ass"},
			want: "chs",
		},
		{
			name: "genuine chinese gb subtitle still detected",
			st:   dto.MediaStream{Path: "/media/Movie.gb.srt"},
			want: "chs",
		},
		{
			name: "genuine chinese tc subtitle still detected",
			st:   dto.MediaStream{Path: "/media/Movie.tc.ass"},
			want: "cht",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := detectSubChineseVariant(tc.st); got != tc.want {
				t.Fatalf("detectSubChineseVariant(%+v) = %q, want %q", tc.st, got, tc.want)
			}
		})
	}
}

func TestAuditV0107_M18_BitmapSubtitleDeliveryDefaultsToEncode(t *testing.T) {
	strictProfile := &DeviceProfile{
		Name: "Generic Web",
		SubtitleProfiles: []SubtitleProfile{
			{Format: "vtt", Method: "External"},
		},
	}
	for _, codec := range []string{"dvd_subtitle", "dvdsub", "vobsub", "dvb_subtitle", "dvbsub"} {
		if m := decideSubtitleDeliveryMethod(&dto.MediaStream{Codec: codec}, strictProfile); m != "Encode" {
			t.Fatalf("codec %q with strict profile = %q, want Encode", codec, m)
		}
	}
	// When client explicitly declares external support, honor it.
	extProfile := &DeviceProfile{
		Name: "Custom Player",
		SubtitleProfiles: []SubtitleProfile{
			{Format: "dvdsub", Method: "External"},
			{Format: "dvb_subtitle", Method: "External"},
		},
	}
	if m := decideSubtitleDeliveryMethod(&dto.MediaStream{Codec: "vobsub"}, extProfile); m != "External" {
		t.Fatalf("expected External when profile declares dvdsub External, got %q", m)
	}
	if m := decideSubtitleDeliveryMethod(&dto.MediaStream{Codec: "dvbsub"}, extProfile); m != "External" {
		t.Fatalf("expected External when profile declares dvb_subtitle External, got %q", m)
	}
}
