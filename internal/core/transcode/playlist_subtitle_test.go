package transcode

import (
	"strings"
	"testing"
)

func TestBuildMasterPlaylistWithSubtitles(t *testing.T) {
	m3u8 := BuildMasterPlaylist(PlaylistParams{
		ItemID:     "item1",
		Bandwidth:  4_000_000,
		Width:      1920,
		Height:     1080,
		VideoCodec: "h264",
		AudioCodec: "aac",
		Subtitles: []SubtitleTrackInfo{
			{
				Index:     2,
				Language:  "chi",
				Title:     `Chinese "Simplified"`,
				IsDefault: true,
				IsForced:  false,
				URI:       "/Videos/item1/subtitles/ms1/2/subtitles.m3u8?api_key=tok",
			},
			{
				Index:     3,
				Language:  "eng",
				Title:     "",
				IsDefault: false,
				IsForced:  true,
				URI:       "/Videos/item1/subtitles/ms1/3/subtitles.m3u8",
			},
			{
				Index: 4,
				URI:   "/Videos/item1/subtitles/ms1/4/subtitles.m3u8",
			},
		},
	})

	wantLines := []string{
		`#EXT-X-MEDIA:TYPE=SUBTITLES,GROUP-ID="subs",NAME="Chinese 'Simplified'",LANGUAGE="chi",DEFAULT=YES,AUTOSELECT=YES,FORCED=NO,URI="/Videos/item1/subtitles/ms1/2/subtitles.m3u8?api_key=tok"`,
		`#EXT-X-MEDIA:TYPE=SUBTITLES,GROUP-ID="subs",NAME="eng",LANGUAGE="eng",DEFAULT=NO,AUTOSELECT=YES,FORCED=YES,URI="/Videos/item1/subtitles/ms1/3/subtitles.m3u8"`,
		`#EXT-X-MEDIA:TYPE=SUBTITLES,GROUP-ID="subs",NAME="Track 4",LANGUAGE="und",DEFAULT=NO,AUTOSELECT=YES,FORCED=NO,URI="/Videos/item1/subtitles/ms1/4/subtitles.m3u8"`,
		`,SUBTITLES="subs"`,
	}
	for _, want := range wantLines {
		if !strings.Contains(m3u8, want) {
			t.Errorf("master playlist missing %q:\n%s", want, m3u8)
		}
	}

	// Without subtitles, SUBTITLES group must not be emitted.
	plain := BuildMasterPlaylist(PlaylistParams{ItemID: "item1"})
	if strings.Contains(plain, "EXT-X-MEDIA") || strings.Contains(plain, `SUBTITLES="subs"`) {
		t.Errorf("expected no subtitle tags when Subtitles is empty:\n%s", plain)
	}
}

func TestBuildSubtitlePlaylist(t *testing.T) {
	m3u8 := BuildSubtitlePlaylist(125.5, "/Videos/item1/ms1/Subtitles/2/Stream.vtt?api_key=abc")
	for _, want := range []string{
		"#EXTM3U",
		"#EXT-X-VERSION:3",
		"#EXT-X-TARGETDURATION:126",
		"#EXT-X-MEDIA-SEQUENCE:0",
		"#EXT-X-PLAYLIST-TYPE:VOD",
		"#EXTINF:125.500000,",
		"/Videos/item1/ms1/Subtitles/2/Stream.vtt?api_key=abc",
		"#EXT-X-ENDLIST",
	} {
		if !strings.Contains(m3u8, want) {
			t.Errorf("subtitle playlist missing %q:\n%s", want, m3u8)
		}
	}

	// Non-positive duration falls back to DefaultSegmentDuration.
	fallback := BuildSubtitlePlaylist(0, "/Videos/item1/ms1/Subtitles/2/Stream.vtt")
	if !strings.Contains(fallback, "#EXT-X-TARGETDURATION:6") || !strings.Contains(fallback, "#EXTINF:6.000000,") {
		t.Errorf("unexpected fallback subtitle playlist:\n%s", fallback)
	}
}
