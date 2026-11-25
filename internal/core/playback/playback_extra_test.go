package playback

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ambra-media/ambra/internal/api/dto"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/plugins"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

type stubStreamResolver struct {
	err error
	res *plugins.StreamResolveResult
}

func (s *stubStreamResolver) Manifest() plugins.Manifest {
	return plugins.Manifest{ID: "stub.stream", Enabled: true, Kinds: []plugins.Kind{plugins.KindStream}}
}
func (s *stubStreamResolver) Configure(map[string]any) error { return nil }
func (s *stubStreamResolver) ResolveStream(context.Context, string) (*plugins.StreamResolveResult, error) {
	return s.res, s.err
}

func TestPlaybackEngineExtraBranches(t *testing.T) {
	repo, engine, tmp := setupPlaybackTest(t)
	ctx := context.Background()

	// ResolveStream with nil registry
	engNil := New(repo, nil, nil)
	if res, err := engNil.ResolveStream(ctx, "/a.strm"); err != nil || res != nil {
		t.Fatalf("expected nil,nil for nil registry")
	}

	// ResolveStream with erroring resolver
	regErr := plugins.NewRegistry()
	regErr.Register(&stubStreamResolver{err: errors.New("boom")})
	engErr := New(repo, regErr, nil)
	if res, err := engErr.ResolveStream(ctx, "/a.strm"); err != nil || res != nil {
		t.Fatalf("expected nil,nil when resolver errors")
	}

	// Decide on missing item
	if _, _, err := engine.Decide(ctx, guid.New(), PlaybackInfoRequest{}); err == nil {
		t.Fatalf("expected error for missing item")
	}

	// Item with Path but no MediaSources -> synthetic fallback source + MediaSourceID filter
	fallbackID := guid.New()
	_ = repo.UpsertItem(ctx, &sqlite.ItemRecord{
		ID:        fallbackID,
		LibraryID: "lib1",
		Type:      "Movie",
		Name:      "Fallback Movie",
		Container: "mp4",
		Path:      filepath.Join(tmp, "fallback.mp4"),
	})
	resp, details, err := engine.Decide(ctx, fallbackID, PlaybackInfoRequest{})
	if err != nil || len(resp.MediaSources) != 1 || details[0].PlayMethod != PlayMethodDirectPlay {
		t.Fatalf("expected synthetic fallback DirectPlay, got resp=%+v details=%+v err=%v", resp, details, err)
	}

	// Filter by non-matching MediaSourceID -> empty list
	respEmpty, _, err := engine.Decide(ctx, fallbackID, PlaybackInfoRequest{MediaSourceID: guid.New()})
	if err != nil || len(respEmpty.MediaSources) != 0 {
		t.Fatalf("expected empty MediaSources for non-matching MediaSourceID, got %+v err=%v", respEmpty, err)
	}

	// Default subtitle index + profile compatibility edge cases
	itemID := guid.New()
	msID := guid.New()
	_ = repo.UpsertItem(ctx, &sqlite.ItemRecord{
		ID:        itemID,
		LibraryID: "lib1",
		Type:      "Movie",
		Name:      "SubTest",
		Container: "mp4",
		Path:      filepath.Join(tmp, "sub.mp4"),
	})
	_ = repo.ReplaceMediaSourcesAndStreams(ctx, itemID, []sqlite.MediaSourceRecord{{
		ID:        msID,
		ItemID:    itemID,
		Name:      "SubTest",
		Path:      filepath.Join(tmp, "sub.mp4"),
		Container: "mp4",
		Bitrate:   10_000_000,
		Streams: []sqlite.MediaStreamRecord{
			{MediaSourceID: msID, ItemID: itemID, StreamIndex: 0, StreamType: "Video", Codec: "hevc"},
			{MediaSourceID: msID, ItemID: itemID, StreamIndex: 1, StreamType: "Audio", Codec: "dts"},
			{MediaSourceID: msID, ItemID: itemID, StreamIndex: 2, StreamType: "Subtitle", Codec: "srt", IsDefault: true, IsExternal: true},
		},
	}})

	// Bitrate exceeded -> Transcode
	respBR, detBR, err := engine.Decide(ctx, itemID, PlaybackInfoRequest{MaxStreamingBitrate: 1_000_000})
	if err != nil || detBR[0].PlayMethod != PlayMethodTranscode || respBR.MediaSources[0].DefaultSubtitleStreamIndex == nil {
		t.Fatalf("expected Transcode on bitrate limit with default sub index, got det=%+v ms=%+v", detBR, respBR.MediaSources[0])
	}

	// Profile with non-Video type + partial match + WASM via Name
	wasmByName := &DeviceProfile{
		Name: "My WASM Player",
		DirectPlayProfiles: []DirectPlayProfile{
			{Type: "Audio", Container: "mp3"},
			{Type: "Video", Container: "mp4", VideoCodec: "h264", AudioCodec: "dts"},
		},
	}
	_, detWasm, err := engine.Decide(ctx, itemID, PlaybackInfoRequest{DeviceProfile: wasmByName})
	if err != nil || detWasm[0].PlayMethod != PlayMethodClientWASM {
		t.Fatalf("expected ClientWASM via profile name, got %+v err=%v", detWasm, err)
	}

	// Helper unit tests
	if supportsClientWASM(nil) {
		t.Fatalf("expected false for nil profile")
	}
	if !csvContains("*", "anything") || !csvContains("mp4,mkv", "") {
		t.Fatalf("expected csvContains wildcard and empty target to match")
	}

	// decideSubtitleDeliveryMethod branches
	if m := decideSubtitleDeliveryMethod(&dto.MediaStream{Codec: "srt", IsExternal: false}, nil); m != "Embed" {
		t.Fatalf("expected Embed for nil profile embedded sub, got %s", m)
	}
	if m := decideSubtitleDeliveryMethod(&dto.MediaStream{Codec: "srt", IsExternal: true}, nil); m != "External" {
		t.Fatalf("expected External for nil profile external sub, got %s", m)
	}
	prof := &DeviceProfile{
		SubtitleProfiles: []SubtitleProfile{
			{Format: "ass", Method: ""},
			{Format: "ssa", Method: "Embed"},
			{Format: "vtt", Method: "External"},
		},
	}
	if m := decideSubtitleDeliveryMethod(&dto.MediaStream{Codec: "ass"}, prof); m != "External" {
		t.Fatalf("expected default External when Method empty, got %s", m)
	}
	if m := decideSubtitleDeliveryMethod(&dto.MediaStream{Codec: "ssa", IsExternal: true}, prof); m != "External" {
		t.Fatalf("expected External for external file even if Embed declared, got %s", m)
	}
	if m := decideSubtitleDeliveryMethod(&dto.MediaStream{Codec: "srt"}, prof); m != "External" {
		t.Fatalf("expected External via vtt fallback for srt, got %s", m)
	}
	if m := decideSubtitleDeliveryMethod(&dto.MediaStream{Codec: "mov_text"}, prof); m != "External" {
		t.Fatalf("expected External fallback for unknown text codec, got %s", m)
	}

	// normalizeSubCodec & subtitleStreamFormat
	for _, tc := range [][2]string{
		{"srt", "srt"},
		{"vtt", "vtt"},
		{"webvtt", "vtt"},
		{"ass", "ass"},
		{"ssa", "ssa"},
		{"pgssub", "sup"},
		{"hdmv_pgs_subtitle", "sup"},
		{"unknown", "srt"},
	} {
		if got := subtitleStreamFormat(tc[0]); got != tc[1] {
			t.Errorf("subtitleStreamFormat(%q) = %q, want %q", tc[0], got, tc[1])
		}
	}
}

func TestServeSubtitleExtraBranches(t *testing.T) {
	dir := t.TempDir()

	// Missing file -> error
	if _, _, err := ServeSubtitle(filepath.Join(dir, "nope.srt"), "vtt", 0); err == nil {
		t.Fatalf("expected error for missing subtitle file")
	}

	// Empty targetFormat -> passthrough
	srtPath := filepath.Join(dir, "test.srt")
	srtContent := "\xef\xbb\xbf1\r\n00:00:01,000 --> 00:00:03,000\r\nEarly\r\n\r\n2\r\nnot a timestamp\r\nignored\r\n\r\n3\r\n00:00:05,000 --> 00:00:10,000\r\nMain\r\n\r\nshortblock\r\n"
	_ = os.WriteFile(srtPath, []byte(srtContent), 0o644)

	ct, raw, err := ServeSubtitle(srtPath, "", 0)
	if err != nil || !strings.Contains(ct, "x-subrip") || len(raw) == 0 {
		t.Fatalf("expected srt passthrough, got ct=%s err=%v", ct, err)
	}

	// SRT -> VTT with startPositionTicks offset (drops cue 1, clamps cue 3 if needed)
	ct, vttData, err := ServeSubtitle(srtPath, "vtt", 6_000*10_000)
	if err != nil || !strings.Contains(ct, "text/vtt") || !strings.Contains(string(vttData), "00:00:00.000 --> 00:00:04.000") {
		t.Fatalf("unexpected offset VTT: ct=%s data=%s err=%v", ct, string(vttData), err)
	}

	// SRT -> unknown target format -> raw fallback
	ct, _, err = ServeSubtitle(srtPath, "xyz", 0)
	if err != nil || !strings.Contains(ct, "x-subrip") {
		t.Fatalf("expected raw fallback for unknown target format, got ct=%s err=%v", ct, err)
	}

	// Binary .sup passthrough
	supPath := filepath.Join(dir, "test.sup")
	_ = os.WriteFile(supPath, []byte{0x50, 0x47, 0x00, 0x01}, 0o644)
	ct, supData, err := ServeSubtitle(supPath, "vtt", 0)
	if err != nil || ct != "application/octet-stream" || len(supData) != 4 {
		t.Fatalf("expected binary sup passthrough, got ct=%s len=%d err=%v", ct, len(supData), err)
	}

	// Unknown source ext
	txtPath := filepath.Join(dir, "test.sub")
	_ = os.WriteFile(txtPath, []byte("raw"), 0o644)
	ct, _, err = ServeSubtitle(txtPath, "vtt", 0)
	if err != nil || !strings.Contains(ct, "text/plain") {
		t.Fatalf("expected text/plain for unknown source ext, got ct=%s err=%v", ct, err)
	}

	// ASS -> VTT and ASS -> JS (including malformed lines, tags, \N, offset clipping)
	assPath := filepath.Join(dir, "movie.ass")
	assContent := "\xef\xbb\xbf[Events]\n" +
		"Format: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text\n" +
		"Dialogue: bad,line\n" +
		"Dialogue: 0,0:00:01.00,0:00:02.00,Default,,0,0,0,,TooEarly\n" +
		"Dialogue: 0,0:00:04.50,0:00:08.25,Default,,0,0,0,,{\\an8}Hello\\NWorld\\nLine3\n" +
		"Dialogue: 0,0:00:09.00,0:00:10.00,Default,,0,0,0,,{\\pos(1,2)}\n"
	_ = os.WriteFile(assPath, []byte(assContent), 0o644)

	ct, vttASS, err := ServeSubtitle(assPath, "vtt", 5_000*10_000)
	if err != nil || !strings.Contains(ct, "text/vtt") || !strings.Contains(string(vttASS), "Hello\nWorld\nLine3") {
		t.Fatalf("ASS->VTT failed: ct=%s out=%q err=%v", ct, string(vttASS), err)
	}
	ct, jsASS, err := ServeSubtitle(assPath, "js", 0)
	if err != nil || !strings.Contains(ct, "application/json") {
		t.Fatalf("ASS->JS failed: ct=%s err=%v", ct, err)
	}
	var track SubtitleTrackInfo
	if err := json.Unmarshal(jsASS, &track); err != nil || len(track.TrackEvents) != 2 {
		t.Fatalf("unexpected ASS->JS track: %s err=%v", string(jsASS), err)
	}

	// Direct helper checks
	if parseASSTimeMs("invalid") != 0 || parseASSTimeMs("0:01:02") != 62000 {
		t.Fatalf("unexpected parseASSTimeMs results")
	}
	if formatVTTTime(-50) != "00:00:00.000" {
		t.Fatalf("expected clamped 00:00:00.000")
	}
	for _, ext := range []string{"vtt", "srt", "ass", "js", "sup", "other"} {
		if subtitleContentType(ext) == "" {
			t.Errorf("empty content type for %s", ext)
		}
	}
}

func TestPlaybackDecisionSingleProfileAudioSwitchAndDisabledTranscode(t *testing.T) {
	repo, engine, tmp := setupPlaybackTest(t)
	ctx := context.Background()

	// 1. Single-profile joint matching: Profile 1 has mp4+h264+aac, Profile 2 has mkv+vp9+opus.
	// Media is mkv+h264+aac -> MUST NOT mix container from Profile 2 with codecs from Profile 1.
	jointID := guid.New()
	jointMS := guid.New()
	_ = repo.UpsertItem(ctx, &sqlite.ItemRecord{
		ID: jointID, LibraryID: "lib1", Type: "Movie", Name: "JointMatch", Container: "mkv", Path: filepath.Join(tmp, "joint.mkv"),
	})
	_ = repo.ReplaceMediaSourcesAndStreams(ctx, jointID, []sqlite.MediaSourceRecord{{
		ID: jointMS, ItemID: jointID, Name: "JointMatch", Path: filepath.Join(tmp, "joint.mkv"), Container: "mkv", Bitrate: 4_000_000,
		Streams: []sqlite.MediaStreamRecord{
			{MediaSourceID: jointMS, ItemID: jointID, StreamIndex: 0, StreamType: "Video", Codec: "h264", Width: 1920, Height: 1080},
			{MediaSourceID: jointMS, ItemID: jointID, StreamIndex: 1, StreamType: "Audio", Codec: "aac", IsDefault: true},
			{MediaSourceID: jointMS, ItemID: jointID, StreamIndex: 2, StreamType: "Audio", Codec: "dts", IsDefault: false},
		},
	}})

	splitProfile := &DeviceProfile{
		Name: "Split Profiles Client",
		DirectPlayProfiles: []DirectPlayProfile{
			{Type: "Video", Container: "mp4", VideoCodec: "h264", AudioCodec: "aac"},
			{Type: "Video", Container: "mkv", VideoCodec: "vp9", AudioCodec: "opus"},
		},
	}
	_, detJoint, err := engine.Decide(ctx, jointID, PlaybackInfoRequest{DeviceProfile: splitProfile})
	if err != nil || len(detJoint) != 1 || detJoint[0].PlayMethod != PlayMethodDirectStream {
		t.Fatalf("expected DirectStream (no cross-profile DirectPlay), got det=%+v err=%v", detJoint, err)
	}

	// 2. AudioStreamIndex switching on an mp4 item:
	// default audio (index 1, aac) -> DirectPlay; switching to AudioStreamIndex=2 (dts) -> DirectStream with AudioStreamIndex=2.
	multiAudioID := guid.New()
	multiAudioMS := guid.New()
	_ = repo.UpsertItem(ctx, &sqlite.ItemRecord{
		ID: multiAudioID, LibraryID: "lib1", Type: "Movie", Name: "DualAudio", Container: "mp4", Path: filepath.Join(tmp, "dual.mp4"),
	})
	_ = repo.ReplaceMediaSourcesAndStreams(ctx, multiAudioID, []sqlite.MediaSourceRecord{{
		ID: multiAudioMS, ItemID: multiAudioID, Name: "DualAudio", Path: filepath.Join(tmp, "dual.mp4"), Container: "mp4", Bitrate: 4_000_000,
		Streams: []sqlite.MediaStreamRecord{
			{MediaSourceID: multiAudioMS, ItemID: multiAudioID, StreamIndex: 0, StreamType: "Video", Codec: "h264", Width: 1920, Height: 1080},
			{MediaSourceID: multiAudioMS, ItemID: multiAudioID, StreamIndex: 1, StreamType: "Audio", Codec: "aac", IsDefault: true},
			{MediaSourceID: multiAudioMS, ItemID: multiAudioID, StreamIndex: 2, StreamType: "Audio", Codec: "dts", IsDefault: false},
		},
	}})
	mp4AACProfile := &DeviceProfile{
		Name: "MP4 AAC Only",
		DirectPlayProfiles: []DirectPlayProfile{
			{Type: "Video", Container: "mp4", VideoCodec: "h264", AudioCodec: "aac"},
		},
	}
	_, detDef, err := engine.Decide(ctx, multiAudioID, PlaybackInfoRequest{DeviceProfile: mp4AACProfile})
	if err != nil || detDef[0].PlayMethod != PlayMethodDirectPlay {
		t.Fatalf("expected DirectPlay with default aac track, got %+v err=%v", detDef, err)
	}
	dtsIdx := 2
	respDTS, detDTS, err := engine.Decide(ctx, multiAudioID, PlaybackInfoRequest{
		AudioStreamIndex: &dtsIdx,
		DeviceProfile:    mp4AACProfile,
	})
	if err != nil || detDTS[0].PlayMethod != PlayMethodDirectStream {
		t.Fatalf("expected DirectStream when switching to dts track, got %+v err=%v", detDTS, err)
	}
	if !strings.Contains(respDTS.MediaSources[0].TranscodingUrl, "AudioStreamIndex=2") {
		t.Fatalf("expected AudioStreamIndex=2 in TranscodingUrl, got %q", respDTS.MediaSources[0].TranscodingUrl)
	}

	// 3. EnableTranscoding=false for Video when transcoding is required (bitrate limit exceeded)
	falseVal := false
	respNoTC, detNoTC, err := engine.Decide(ctx, multiAudioID, PlaybackInfoRequest{
		MaxStreamingBitrate: 500_000,
		EnableTranscoding:   &falseVal,
		DeviceProfile:       mp4AACProfile,
	})
	if err != nil || detNoTC[0].PlayMethod != "" || respNoTC.MediaSources[0].SupportsTranscoding || respNoTC.MediaSources[0].TranscodingUrl != "" {
		t.Fatalf("expected empty PlayMethod & disabled transcoding for video, got det=%+v ms=%+v err=%v", detNoTC, respNoTC.MediaSources[0], err)
	}

	// 4. Audio item: STRM 302 Audio URL, nil profile audio direct play, and EnableTranscoding=false for Audio
	audioStrmPath := filepath.Join(tmp, "song.strm")
	_ = os.WriteFile(audioStrmPath, []byte("https://192.168.1.10/song.flac\n"), 0o644)
	audioStrmID := guid.New()
	_ = repo.UpsertItem(ctx, &sqlite.ItemRecord{
		ID: audioStrmID, LibraryID: "music", Type: "Audio", MediaType: "Audio", IsStrm: true, Name: "Remote Song", Container: "strm", Path: audioStrmPath,
	})
	respAS, detAS, err := engine.Decide(ctx, audioStrmID, PlaybackInfoRequest{})
	if err != nil || detAS[0].PlayMethod != PlayMethodSTRM302 || !strings.HasPrefix(respAS.MediaSources[0].DirectStreamUrl, "/Audio/") {
		t.Fatalf("expected Audio STRM302 DirectStreamUrl, got det=%+v ms=%+v err=%v", detAS, respAS.MediaSources[0], err)
	}

	audioID := guid.New()
	audioMS := guid.New()
	_ = repo.UpsertItem(ctx, &sqlite.ItemRecord{
		ID: audioID, LibraryID: "music", Type: "Audio", MediaType: "Audio", Name: "FLAC", Container: "flac", Path: filepath.Join(tmp, "a.flac"),
	})
	_ = repo.ReplaceMediaSourcesAndStreams(ctx, audioID, []sqlite.MediaSourceRecord{{
		ID: audioMS, ItemID: audioID, Name: "a.flac", Path: filepath.Join(tmp, "a.flac"), Container: "flac", Bitrate: 1_000_000,
		Streams: []sqlite.MediaStreamRecord{{MediaSourceID: audioMS, ItemID: audioID, StreamIndex: 0, StreamType: "Audio", Codec: "flac"}},
	}})
	// Nil profile -> checkAudioProfileCompatibility returns true -> DirectPlay
	_, detAudNil, err := engine.Decide(ctx, audioID, PlaybackInfoRequest{})
	if err != nil || detAudNil[0].PlayMethod != PlayMethodDirectPlay {
		t.Fatalf("expected audio DirectPlay with nil profile, got %+v err=%v", detAudNil, err)
	}
	// Incompatible audio profile with EnableTranscoding=false
	respAudNoTC, detAudNoTC, err := engine.Decide(ctx, audioID, PlaybackInfoRequest{
		EnableTranscoding: &falseVal,
		DeviceProfile: &DeviceProfile{
			Name:               "MP3 Only",
			DirectPlayProfiles: []DirectPlayProfile{{Type: "Audio", Container: "mp3", AudioCodec: "mp3"}},
		},
	})
	if err != nil || detAudNoTC[0].PlayMethod != "" || respAudNoTC.MediaSources[0].SupportsTranscoding {
		t.Fatalf("expected disabled audio transcoding, got det=%+v ms=%+v err=%v", detAudNoTC, respAudNoTC.MediaSources[0], err)
	}
}

func TestSubtitleShiftASSRenderSRTAndLimits(t *testing.T) {
	dir := t.TempDir()

	// 1. shiftASS with BOM, header lines, malformed Dialogue, early dropped cue, clamped crossing cue, and shifted cue
	assPath := filepath.Join(dir, "shift.ass")
	assContent := "\xef\xbb\xbf[Script Info]\nTitle: Shift Test\n[Events]\n" +
		"Format: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text\n" +
		"Dialogue: broken,line\n" +
		"Dialogue: 0,0:00:01.00,0:00:04.00,Default,,0,0,0,,DroppedBeforeOffset\n" +
		"Dialogue: 0,0:00:03.00,0:00:08.50,Default,,0,0,0,,ClampedStart\n" +
		"Dialogue: 0,0:00:10.25,0:00:15.75,Default,,0,0,0,,NormalShifted\n"
	if err := os.WriteFile(assPath, []byte(assContent), 0o644); err != nil {
		t.Fatal(err)
	}

	// ASS -> SSA with startPositionTicks <= 0 hits passthrough branch
	ctSSA, ssaRaw, err := ServeSubtitle(assPath, "ssa", 0)
	if err != nil || !strings.Contains(ctSSA, "text/x-ssa") || string(ssaRaw) != assContent {
		t.Fatalf("expected ASS->SSA passthrough without offset, ct=%s err=%v", ctSSA, err)
	}

	// ASS -> ASS with startPositionTicks = 5s (50_000_000 ticks)
	ctShifted, shiftedBytes, err := ServeSubtitle(assPath, "ass", 5_000*10_000)
	if err != nil || !strings.Contains(ctShifted, "text/x-ssa") {
		t.Fatalf("ASS shift failed: ct=%s err=%v", ctShifted, err)
	}
	shifted := string(shiftedBytes)
	if !strings.HasPrefix(shifted, "\xef\xbb\xbf[Script Info]") {
		t.Fatalf("expected BOM and header preserved: %q", shifted)
	}
	if strings.Contains(shifted, "DroppedBeforeOffset") {
		t.Fatalf("expected early cue dropped, got:\n%s", shifted)
	}
	if !strings.Contains(shifted, "Dialogue: 0,0:00:00.00,0:00:03.50,Default,,0,0,0,,ClampedStart") {
		t.Fatalf("expected clamped cue in shifted ASS, got:\n%s", shifted)
	}
	if !strings.Contains(shifted, "Dialogue: 0,0:00:05.25,0:00:10.75,Default,,0,0,0,,NormalShifted") {
		t.Fatalf("expected shifted cue in shifted ASS, got:\n%s", shifted)
	}
	if !strings.Contains(shifted, "Dialogue: broken,line") {
		t.Fatalf("expected malformed dialogue line preserved verbatim, got:\n%s", shifted)
	}

	// Direct negative clamping checks for formatASSTime and formatSRTTime
	if got := formatASSTime(-100); got != "0:00:00.00" {
		t.Fatalf("formatASSTime(-100) = %q, want 0:00:00.00", got)
	}
	if got := formatSRTTime(-100); got != "00:00:00,000" {
		t.Fatalf("formatSRTTime(-100) = %q, want 00:00:00,000", got)
	}

	// 2. renderSRT via ServeSubtitle (ASS -> SRT with startPositionTicks > 0, and VTT -> SRT)
	ctSRT, srtBytes, err := ServeSubtitle(assPath, "srt", 5_000*10_000)
	if err != nil || !strings.Contains(ctSRT, "application/x-subrip") {
		t.Fatalf("ASS->SRT failed: ct=%s err=%v", ctSRT, err)
	}
	srtOut := string(srtBytes)
	if !strings.Contains(srtOut, "1\n00:00:00,000 --> 00:00:03,500\nClampedStart") ||
		!strings.Contains(srtOut, "2\n00:00:05,250 --> 00:00:10,750\nNormalShifted") {
		t.Fatalf("unexpected renderSRT output:\n%s", srtOut)
	}

	// 3. Oversized line (> maxSubtitleLineSize) rejected by both parseASS and shiftASS (and via ServeSubtitle)
	overlongLine := "Dialogue: 0,0:00:01.00,0:00:02.00,Default,,0,0,0,," + strings.Repeat("A", maxSubtitleLineSize+128) + "\n"
	if _, err := parseASS(overlongLine, 0); err == nil {
		t.Fatal("expected parseASS error for line exceeding maxSubtitleLineSize")
	}
	if _, err := shiftASS(overlongLine, 10_000); err == nil {
		t.Fatal("expected shiftASS error for line exceeding maxSubtitleLineSize")
	}
	overlongPath := filepath.Join(dir, "overlong.ass")
	if err := os.WriteFile(overlongPath, []byte(overlongLine), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ServeSubtitle(overlongPath, "vtt", 0); err == nil {
		t.Fatal("expected ServeSubtitle ASS->VTT error for overlong ASS line")
	}
	if _, _, err := ServeSubtitle(overlongPath, "ass", 10_000); err == nil {
		t.Fatal("expected ServeSubtitle ASS shift error for overlong ASS line")
	}

	// 4. Oversized file (> maxSubtitleFileSize) rejected by readLimitedSubtitleFile using a sparse file
	bigPath := filepath.Join(dir, "huge.srt")
	f, err := os.Create(bigPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(maxSubtitleFileSize + 1); err != nil {
		_ = f.Close()
		t.Fatal(err)
	}
	_ = f.Close()
	if _, _, err := ServeSubtitle(bigPath, "vtt", 0); err == nil || !strings.Contains(err.Error(), "exceeds maximum size") {
		t.Fatalf("expected oversized subtitle file error, got %v", err)
	}
}
