package scanner

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/pkg/ticks"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestParseFFprobeJSON_HDRAndVideoRanges(t *testing.T) {
	raw := []byte(`{
		"format": {
			"format_name": "matroska,webm",
			"duration": "125.5",
			"size": "104857600",
			"bit_rate": "6680000"
		},
		"streams": [
			{
				"index": 0,
				"codec_type": "video",
				"codec_name": "hevc",
				"width": 3840,
				"height": 2160,
				"color_transfer": "smpte2084",
				"disposition": {"default": 1},
				"tags": {"BPS-eng": "25000000"},
				"side_data_list": [
					{"side_data_type": "DOVI configuration record"}
				]
			},
			{
				"index": 1,
				"codec_type": "video",
				"codec_name": "hevc",
				"width": 3840,
				"height": 2160,
				"color_transfer": "smpte2084",
				"bit_rate": "18000000"
			},
			{
				"index": 2,
				"codec_type": "video",
				"codec_name": "av1",
				"width": 1920,
				"height": 1080,
				"color_transfer": "arib-std-b67"
			},
			{
				"index": 3,
				"codec_type": "video",
				"codec_name": "vp9",
				"width": 1280,
				"height": 720,
				"color_transfer": "bt709"
			},
			{
				"index": 4,
				"codec_type": "video",
				"codec_name": "mjpeg",
				"width": 600,
				"height": 900,
				"disposition": {"attached_pic": 1}
			}
		]
	}`)

	res, err := ParseFFprobeJSON(raw)
	if err != nil {
		t.Fatalf("ParseFFprobeJSON failed: %v", err)
	}
	if res.Container != "mkv" {
		t.Errorf("Container = %q, want mkv", res.Container)
	}
	if wantTicks := ticks.FromSeconds(125.5); res.RuntimeTicks != wantTicks {
		t.Errorf("RuntimeTicks = %d, want %d", res.RuntimeTicks, wantTicks)
	}
	if res.Bitrate != 6680000 || res.Size != 104857600 {
		t.Errorf("Bitrate/Size = %d/%d, want 6680000/104857600", res.Bitrate, res.Size)
	}
	// attached_pic must be skipped, leaving 4 video streams
	if len(res.Streams) != 4 {
		t.Fatalf("len(Streams) = %d, want 4", len(res.Streams))
	}

	wantRanges := []string{"DolbyVision", "HDR10", "HLG", "SDR"}
	wantCodecs := []string{"hevc", "hevc", "av1", "vp9"}
	for i := range wantRanges {
		st := res.Streams[i]
		if st.StreamType != "Video" || st.VideoRange != wantRanges[i] || st.Codec != wantCodecs[i] {
			t.Errorf("stream[%d] = (%s, %s, %s), want (Video, %s, %s)",
				i, st.StreamType, st.Codec, st.VideoRange, wantCodecs[i], wantRanges[i])
		}
	}
	if res.Streams[0].Bitrate != 25000000 || !res.Streams[0].IsDefault {
		t.Errorf("stream[0] BPS tag or default mismatch: %+v", res.Streams[0])
	}
	if res.Streams[0].DisplayTitle != "2160p HEVC DolbyVision" {
		t.Errorf("stream[0] DisplayTitle = %q", res.Streams[0].DisplayTitle)
	}
	if res.Streams[3].DisplayTitle != "720p VP9" {
		t.Errorf("stream[3] DisplayTitle = %q", res.Streams[3].DisplayTitle)
	}
}

func TestParseFFprobeJSON_AudioLayoutsAndTracks(t *testing.T) {
	raw := []byte(`{
		"format": {"format_name": "mov,mp4,m4a,3gp,3g2,mj2"},
		"streams": [
			{
				"index": 0,
				"codec_type": "audio",
				"codec_name": "truehd",
				"profile": "Dolby TrueHD + Dolby Atmos",
				"channels": 8,
				"channel_layout": "7.1",
				"sample_rate": "48000",
				"bit_rate": "4200000",
				"duration": "42.0",
				"disposition": {"default": 1},
				"tags": {"language": "eng", "title": "English TrueHD"}
			},
			{
				"index": 1,
				"codec_type": "audio",
				"codec_name": "dts",
				"channels": 8,
				"channel_layout": "7.1(side)",
				"sample_rate": "48000",
				"tags": {"LANGUAGE": "chi"}
			},
			{
				"index": 2,
				"codec_type": "audio",
				"codec_name": "ac3",
				"channels": 6,
				"channel_layout": "5.1(side)",
				"sample_rate": "48000"
			},
			{
				"index": 3,
				"codec_type": "audio",
				"codec_name": "aac",
				"channels": 2,
				"channel_layout": "stereo",
				"sample_rate": "44100"
			},
			{
				"index": 4,
				"codec_type": "audio",
				"codec_name": "aac",
				"channels": 1,
				"channel_layout": "mono"
			},
			{
				"index": 5,
				"codec_type": "audio",
				"codec_name": "opus",
				"channels": 4,
				"channel_layout": "quad"
			},
			{
				"index": 6,
				"codec_type": "audio",
				"codec_name": "flac",
				"channels": 8
			},
			{
				"index": 7,
				"codec_type": "audio",
				"codec_name": "flac",
				"channels": 6
			},
			{
				"index": 8,
				"codec_type": "audio",
				"codec_name": "flac",
				"channels": 2
			},
			{
				"index": 9,
				"codec_type": "audio",
				"codec_name": "flac",
				"channels": 1
			},
			{
				"index": 10,
				"codec_type": "audio",
				"codec_name": "flac",
				"channels": 4
			},
			{
				"index": 11,
				"codec_type": "audio",
				"codec_name": "",
				"channels": 0
			}
		]
	}`)

	res, err := ParseFFprobeJSON(raw)
	if err != nil {
		t.Fatalf("ParseFFprobeJSON failed: %v", err)
	}
	if res.Container != "mp4" {
		t.Errorf("Container = %q, want mp4", res.Container)
	}
	// Format had no duration -> fell back to stream duration 42.0s
	if want := ticks.FromSeconds(42.0); res.RuntimeTicks != want {
		t.Errorf("RuntimeTicks = %d, want %d", res.RuntimeTicks, want)
	}

	wantLayouts := []string{"atmos", "7.1", "5.1", "stereo", "mono", "quad", "7.1", "5.1", "stereo", "mono", "4ch", ""}
	if len(res.Streams) != len(wantLayouts) {
		t.Fatalf("len(Streams) = %d, want %d", len(res.Streams), len(wantLayouts))
	}
	for i, wl := range wantLayouts {
		if res.Streams[i].AudioLayout != wl {
			t.Errorf("audio[%d].AudioLayout = %q, want %q", i, res.Streams[i].AudioLayout, wl)
		}
	}
	if res.Streams[0].Language != "eng" || res.Streams[0].SampleRate != 48000 || res.Streams[0].Bitrate != 4200000 || !res.Streams[0].IsDefault {
		t.Errorf("unexpected audio[0]: %+v", res.Streams[0])
	}
	if res.Streams[1].Language != "chi" {
		t.Errorf("case-insensitive tag lookup failed: %+v", res.Streams[1])
	}
	if res.Streams[2].Language != "und" {
		t.Errorf("default language und failed: %+v", res.Streams[2])
	}
	if res.Streams[11].DisplayTitle != "Audio" {
		t.Errorf("empty audio DisplayTitle = %q, want Audio", res.Streams[11].DisplayTitle)
	}
}

func TestParseFFprobeJSON_EmbeddedSubtitles(t *testing.T) {
	raw := []byte(`{
		"format": {"format_name": "matroska"},
		"streams": [
			{
				"index": 0,
				"codec_type": "subtitle",
				"codec_name": "ass",
				"disposition": {"default": 1, "forced": 0},
				"tags": {"language": "chi", "title": "简体中文 / 特效"}
			},
			{
				"index": 1,
				"codec_type": "subtitle",
				"codec_name": "hdmv_pgs_subtitle",
				"disposition": {"default": 0, "forced": 0},
				"tags": {"language": "chi", "title": "繁體中文 (PGS)"}
			},
			{
				"index": 2,
				"codec_type": "subtitle",
				"codec_name": "subrip",
				"disposition": {"default": 0, "forced": 1},
				"tags": {"language": "chi", "title": "简繁双语"}
			},
			{
				"index": 3,
				"codec_type": "subtitle",
				"codec_name": "subrip",
				"tags": {"language": "fre", "title": "Français"}
			}
		]
	}`)

	res, err := ParseFFprobeJSON(raw)
	if err != nil {
		t.Fatalf("ParseFFprobeJSON failed: %v", err)
	}
	if len(res.Streams) != 4 {
		t.Fatalf("len(Streams) = %d, want 4", len(res.Streams))
	}

	s0 := res.Streams[0]
	if s0.SubVariant != "chs" || s0.DisplayTitle != "简体中文 (内封 ASS)" || !s0.IsDefault || s0.IsExternal {
		t.Errorf("sub[0] mismatch: %+v", s0)
	}
	s1 := res.Streams[1]
	if s1.SubVariant != "cht" || s1.DisplayTitle != "繁体中文 (内封 PGS)" || s1.IsExternal {
		t.Errorf("sub[1] mismatch: %+v", s1)
	}
	s2 := res.Streams[2]
	if s2.SubVariant != "bilingual" || s2.DisplayTitle != "简繁双语 (内封 SRT)" || !s2.IsForced || s2.IsExternal {
		t.Errorf("sub[2] mismatch: %+v", s2)
	}
	s3 := res.Streams[3]
	if s3.Language != "fre" || s3.DisplayTitle != "Subtitle (内封 SRT)" {
		t.Errorf("sub[3] mismatch: %+v", s3)
	}
}

func TestParseFFprobeJSON_ErrorsAndHelpers(t *testing.T) {
	if _, err := ParseFFprobeJSON(nil); err == nil {
		t.Errorf("expected error for empty input")
	}
	if _, err := ParseFFprobeJSON([]byte("{bad json")); err == nil {
		t.Errorf("expected error for invalid json")
	}
	if _, err := ParseFFprobeJSON([]byte("{}")); err == nil {
		t.Errorf("expected error for empty json object")
	}

	containerCases := map[string]string{
		"":              "",
		"mpegts":        "ts",
		"avi":           "avi",
		"webm":          "webm",
		"flv,something": "flv",
		"ogg":           "ogg",
	}
	for in, want := range containerCases {
		if got := normalizeProbeContainer(in); got != want {
			t.Errorf("normalizeProbeContainer(%q) = %q, want %q", in, got, want)
		}
	}

	if got := buildVideoDisplayTitle(0, "", "SDR"); got != "Video" {
		t.Errorf("buildVideoDisplayTitle empty = %q, want Video", got)
	}
	if got := toEmbeddedSubtitleTitle("简体中文"); got != "简体中文 (内封)" {
		t.Errorf("toEmbeddedSubtitleTitle = %q", got)
	}
	if got := tagValue(nil, "language"); got != "" {
		t.Errorf("tagValue(nil) = %q", got)
	}
}

func TestProber_GracefulDegradationAndTimeout(t *testing.T) {
	ctx := context.Background()

	// Nil and disabled prober return (nil, nil)
	var nilProber *Prober
	if res, err := nilProber.Probe(ctx, "/tmp/x.mkv"); res != nil || err != nil {
		t.Fatalf("nil prober expected (nil, nil), got (%+v, %v)", res, err)
	}
	if res, err := NewDisabledProber().Probe(ctx, "/tmp/x.mkv"); res != nil || err != nil {
		t.Fatalf("disabled prober expected (nil, nil), got (%+v, %v)", res, err)
	}

	// Empty path returns error
	pDefault := NewProber("")
	if _, err := pDefault.Probe(ctx, ""); err == nil {
		t.Fatalf("expected error for empty file path")
	}

	// Missing binary returns error
	pMissing := NewProber("/nonexistent/path/to/ffprobe-not-here")
	if _, err := pMissing.Probe(ctx, "/tmp/x.mkv"); err == nil {
		t.Fatalf("expected error for nonexistent ffprobe binary")
	}

	// Timeout enforcement using a fake slow ffprobe script
	tmp := t.TempDir()
	slowScript := filepath.Join(tmp, "slow-ffprobe.sh")
	_ = os.WriteFile(slowScript, []byte("#!/bin/sh\nexec sleep 5\n"), 0o755)
	pSlow := NewProber(slowScript)
	pSlow.SetTimeout(80 * time.Millisecond)
	start := time.Now()
	if _, err := pSlow.Probe(ctx, filepath.Join(tmp, "fake.mkv")); err == nil {
		t.Fatalf("expected timeout error from slow ffprobe")
	}
	if time.Since(start) > 1500*time.Millisecond {
		t.Fatalf("probe did not respect short timeout; elapsed=%v", time.Since(start))
	}

	// Corrupt 10-byte fake mkv gracefully fails probe without panicking
	fakeMKV := filepath.Join(tmp, "corrupt.mkv")
	_ = os.WriteFile(fakeMKV, []byte("0123456789"), 0o644)
	if _, err := NewProber("ffprobe").Probe(ctx, fakeMKV); err == nil {
		t.Fatalf("expected error probing 10-byte corrupt mkv")
	}
}

func TestEndToEnd_RealFFmpegAndFFprobeScan(t *testing.T) {
	ffmpegPath, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg not found in PATH")
	}
	ffprobePath, err := exec.LookPath("ffprobe")
	if err != nil {
		t.Skip("ffprobe not found in PATH")
	}

	tmp := t.TempDir()

	// Create a real 1-second SRT subtitle file
	srtPath := filepath.Join(tmp, "sub.srt")
	srtContent := "1\n00:00:00,000 --> 00:00:00,900\n你好世界\n"
	if err := os.WriteFile(srtPath, []byte(srtContent), 0o644); err != nil {
		t.Fatal(err)
	}

	// Generate a real 1-second MKV with H.264 video + AAC audio + embedded CHS SRT subtitle
	moviesDir := filepath.Join(tmp, "movies")
	movieDir := filepath.Join(moviesDir, "RealProbeMovie (2024)")
	if err := os.MkdirAll(movieDir, 0o755); err != nil {
		t.Fatal(err)
	}
	realMKV := filepath.Join(movieDir, "RealProbeMovie.2024.mkv")
	cmd := exec.Command(
		ffmpegPath,
		"-y",
		"-f", "lavfi", "-i", "testsrc=duration=1:size=320x240:rate=24",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=1:sample_rate=48000",
		"-i", srtPath,
		"-map", "0:v:0",
		"-map", "1:a:0",
		"-map", "2:s:0",
		"-c:v", "libx264", "-pix_fmt", "yuv420p",
		"-c:a", "aac", "-ac", "2",
		"-c:s", "subrip",
		"-metadata:s:a:0", "language=eng",
		"-metadata:s:s:0", "language=chi",
		"-metadata:s:s:0", "title=简体中文",
		"-disposition:s:0", "default",
		realMKV,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("ffmpeg generate mkv failed: %v\n%s", err, string(out))
	}

	// Add an external Traditional Chinese ASS subtitle alongside the MKV
	extSub := filepath.Join(movieDir, "RealProbeMovie.2024.cht.ass")
	if err := os.WriteFile(extSub, []byte("[Script Info]\nTitle: CHT\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Also copy the real MKV as a TV episode to exercise indexEpisode probing
	tvDir := filepath.Join(tmp, "tv")
	epDir := filepath.Join(tvDir, "ProbeShow (2024)", "Season 01")
	if err := os.MkdirAll(epDir, 0o755); err != nil {
		t.Fatal(err)
	}
	rawMKV, err := os.ReadFile(realMKV)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(epDir, "ProbeShow.S01E01.mkv"), rawMKV, 0o644); err != nil {
		t.Fatal(err)
	}

	// Open SQLite store and run real Scanner
	store, err := sqlite.Open(filepath.Join(tmp, "probe_e2e.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	repo := sqlite.NewLibraryRepo(store)
	cfg := config.Default(tmp)
	cfg.FFprobePath = ffprobePath
	ctx := context.Background()

	movieLib, err := repo.CreateLibrary(ctx, "Movies", "movies", []string{moviesDir})
	if err != nil {
		t.Fatal(err)
	}
	tvLib, err := repo.CreateLibrary(ctx, "TV", "tvshows", []string{tvDir})
	if err != nil {
		t.Fatal(err)
	}

	sc := New(repo, nil, nil, cfg)
	if _, err := sc.ScanLibrary(ctx, movieLib.ID); err != nil {
		t.Fatalf("ScanLibrary movies failed: %v", err)
	}
	if _, err := sc.ScanLibrary(ctx, tvLib.ID); err != nil {
		t.Fatalf("ScanLibrary tv failed: %v", err)
	}

	// Query movie with MediaSources
	items, total, err := repo.QueryItems(ctx, sqlite.ItemFilter{
		ServerID: cfg.ServerID,
		ParentID: movieLib.ID,
		Fields:   []string{"MediaSources"},
	})
	if err != nil || total != 1 || len(items) != 1 {
		t.Fatalf("expected 1 movie, got total=%d len=%d err=%v", total, len(items), err)
	}
	movie := items[0]
	if movie.RunTimeTicks == nil || *movie.RunTimeTicks < ticks.FromSeconds(0.5) || *movie.RunTimeTicks > ticks.FromSeconds(3.0) {
		t.Fatalf("expected real ~1s RunTimeTicks, got %v", movie.RunTimeTicks)
	}
	if len(movie.MediaSources) != 1 {
		t.Fatalf("expected 1 MediaSource, got %d", len(movie.MediaSources))
	}
	ms := movie.MediaSources[0]
	if ms.Bitrate <= 0 || ms.RunTimeTicks <= 0 {
		t.Fatalf("expected positive Bitrate and RunTimeTicks, got bitrate=%d ticks=%d", ms.Bitrate, ms.RunTimeTicks)
	}
	// Expect 4 streams: Video (0), Audio (1), Embedded CHS SRT (2), External CHT ASS (3)
	if len(ms.MediaStreams) != 4 {
		t.Fatalf("expected 4 streams (V+A+embeddedS+extS), got %d: %+v", len(ms.MediaStreams), ms.MediaStreams)
	}

	v := ms.MediaStreams[0]
	if v.Type != "Video" || v.Codec != "h264" || v.Width != 320 || v.Height != 240 || v.VideoRange != "SDR" {
		t.Errorf("unexpected probed video stream: %+v", v)
	}
	a := ms.MediaStreams[1]
	if a.Type != "Audio" || a.Codec != "aac" || a.Channels != 2 || a.SampleRate != 48000 || a.Language != "eng" {
		t.Errorf("unexpected probed audio stream: %+v", a)
	}
	embSub := ms.MediaStreams[2]
	if embSub.Type != "Subtitle" || embSub.Index != 2 || embSub.IsExternal || embSub.SubVariant != "chs" || embSub.DisplayTitle != "简体中文 (内封 SRT)" {
		t.Errorf("unexpected embedded subtitle stream: %+v", embSub)
	}
	ext := ms.MediaStreams[3]
	if ext.Type != "Subtitle" || ext.Index != 3 || !ext.IsExternal || ext.SubVariant != "cht" || ext.DisplayTitle != "繁体中文 (ASS)" {
		t.Errorf("unexpected external subtitle stream: %+v", ext)
	}

	// Verify episode also received real RuntimeTicks & 320x240 video stream
	eps, epTotal, err := repo.QueryItems(ctx, sqlite.ItemFilter{
		ServerID:         cfg.ServerID,
		ParentID:         tvLib.ID,
		Recursive:        true,
		IncludeItemTypes: []string{"Episode"},
		Fields:           []string{"MediaSources"},
	})
	if err != nil || epTotal != 1 || len(eps) != 1 {
		t.Fatalf("expected 1 episode, got total=%d err=%v", epTotal, err)
	}
	if eps[0].RunTimeTicks == nil || *eps[0].RunTimeTicks <= 0 || len(eps[0].MediaSources) != 1 || eps[0].MediaSources[0].MediaStreams[0].Width != 320 {
		t.Fatalf("expected probed episode metadata, got %+v", eps[0])
	}
}

func TestScanner_NeverProbesStrmFiles(t *testing.T) {
	tmp := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmp, "strm.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	// Create a sentinel ffprobe wrapper that creates a marker file if ever invoked
	marker := filepath.Join(tmp, "ffprobe-was-called")
	wrapper := filepath.Join(tmp, "sentinel-ffprobe.sh")
	script := "#!/bin/sh\ntouch \"" + marker + "\"\nexit 1\n"
	if err := os.WriteFile(wrapper, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	strmDir := filepath.Join(tmp, "cloud")
	if err := os.MkdirAll(strmDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(strmDir, "CloudMovie (2024).strm"), []byte("https://cdn.example.com/movie.mkv"), 0o644); err != nil {
		t.Fatal(err)
	}

	repo := sqlite.NewLibraryRepo(store)
	ctx := context.Background()
	lib, err := repo.CreateLibrary(ctx, "Cloud", "movies", []string{strmDir})
	if err != nil {
		t.Fatal(err)
	}

	sc := New(repo, nil, nil, config.Default(tmp))
	sc.SetProber(NewProber(wrapper))

	stats, err := sc.ScanLibrary(ctx, lib.ID)
	if err != nil || stats.ItemsAdded != 1 {
		t.Fatalf("ScanLibrary strm failed: stats=%+v err=%v", stats, err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("ffprobe must NOT be invoked for .strm files, but marker file exists")
	}
}
