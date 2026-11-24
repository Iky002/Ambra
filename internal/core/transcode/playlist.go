package transcode

import (
	"fmt"
	"math"
	"net/url"
	"strconv"
	"strings"
)

const (
	// TicksPerSecond is the Emby/.NET tick resolution (10,000,000 ticks = 1 second).
	TicksPerSecond = 10_000_000
	// DefaultSegmentDuration is the default HLS segment duration in seconds.
	DefaultSegmentDuration = 6
)

// SubtitleTrackInfo describes a WebVTT subtitle track exposed in an HLS master playlist.
type SubtitleTrackInfo struct {
	Index     int
	Language  string
	Title     string
	IsDefault bool
	IsForced  bool
	URI       string
}

// PlaylistParams configures virtual HLS master/media playlist generation.
type PlaylistParams struct {
	ItemID           string
	PlaylistID       string // default "main"
	DurationTicks    int64
	DurationSec      float64 // if > 0 overrides DurationTicks
	SegmentDuration  int     // seconds, default 6
	SegmentContainer string  // "ts" (default), "mp4", "m4s"
	Bandwidth        int     // bits/sec for master.m3u8
	Width            int
	Height           int
	VideoCodec       string
	AudioCodec       string
	Query            url.Values // preserved query parameters for child playlist / segment URLs
	Subtitles        []SubtitleTrackInfo
}

// SegmentDurations computes the exact segment count N = ceil(D / S) and per-segment durations.
// Each segment except the last has duration S; the last segment has the exact remainder.
func SegmentDurations(durationSec float64, segDur int) []float64 {
	if segDur <= 0 {
		segDur = DefaultSegmentDuration
	}
	if durationSec <= 0 {
		return nil
	}
	s := float64(segDur)
	count := int(math.Ceil(durationSec / s))
	if count <= 0 {
		return nil
	}
	out := make([]float64, 0, count)
	remaining := durationSec
	for i := 0; i < count; i++ {
		if i < count-1 {
			out = append(out, s)
			remaining -= s
		} else {
			if remaining < 0.001 {
				// Avoid emitting a near-zero trailing segment due to floating point drift
				if len(out) > 0 {
					break
				}
				remaining = s
			}
			out = append(out, math.Round(remaining*1_000_000)/1_000_000)
		}
	}
	return out
}

// BuildMasterPlaylist generates an HLS master.m3u8 pointing to main.m3u8.
func BuildMasterPlaylist(p PlaylistParams) string {
	bw := p.Bandwidth
	if bw <= 0 {
		bw = 4_000_000
	}
	codecs := hlsCodecsAttr(p.VideoCodec, p.AudioCodec)

	var b strings.Builder
	b.WriteString("#EXTM3U\n")
	b.WriteString("#EXT-X-VERSION:3\n")
	for _, sub := range p.Subtitles {
		name := strings.TrimSpace(sub.Title)
		if name == "" {
			name = strings.TrimSpace(sub.Language)
		}
		if name == "" {
			name = fmt.Sprintf("Track %d", sub.Index)
		}
		lang := strings.TrimSpace(sub.Language)
		if lang == "" {
			lang = "und"
		}
		defVal := "NO"
		if sub.IsDefault {
			defVal = "YES"
		}
		forcedVal := "NO"
		if sub.IsForced {
			forcedVal = "YES"
		}
		b.WriteString(fmt.Sprintf(
			"#EXT-X-MEDIA:TYPE=SUBTITLES,GROUP-ID=\"subs\",NAME=\"%s\",LANGUAGE=\"%s\",DEFAULT=%s,AUTOSELECT=YES,FORCED=%s,URI=\"%s\"\n",
			sanitizeHLSAttr(name),
			sanitizeHLSAttr(lang),
			defVal,
			forcedVal,
			sanitizeHLSAttr(sub.URI),
		))
	}
	b.WriteString(fmt.Sprintf("#EXT-X-STREAM-INF:BANDWIDTH=%d", bw))
	if p.Width > 0 && p.Height > 0 {
		b.WriteString(fmt.Sprintf(",RESOLUTION=%dx%d", p.Width, p.Height))
	}
	if codecs != "" {
		b.WriteString(fmt.Sprintf(",CODECS=\"%s\"", codecs))
	}
	if len(p.Subtitles) > 0 {
		b.WriteString(",SUBTITLES=\"subs\"")
	}
	b.WriteByte('\n')

	target := "main.m3u8"
	if q := encodeQuery(p.Query); q != "" {
		target += "?" + q
	}
	b.WriteString(target)
	b.WriteByte('\n')
	return b.String()
}

// BuildSubtitlePlaylist generates a valid VOD WebVTT HLS subtitle playlist.
func BuildSubtitlePlaylist(durationSec float64, vttURI string) string {
	if durationSec <= 0 {
		durationSec = float64(DefaultSegmentDuration)
	}
	targetDur := int(math.Ceil(durationSec))
	if targetDur < 1 {
		targetDur = 1
	}

	var b strings.Builder
	b.WriteString("#EXTM3U\n")
	b.WriteString("#EXT-X-VERSION:3\n")
	b.WriteString(fmt.Sprintf("#EXT-X-TARGETDURATION:%d\n", targetDur))
	b.WriteString("#EXT-X-MEDIA-SEQUENCE:0\n")
	b.WriteString("#EXT-X-PLAYLIST-TYPE:VOD\n")
	b.WriteString(fmt.Sprintf("#EXTINF:%.6f,\n", durationSec))
	b.WriteString(vttURI)
	b.WriteByte('\n')
	b.WriteString("#EXT-X-ENDLIST\n")
	return b.String()
}

func sanitizeHLSAttr(s string) string {
	s = strings.ReplaceAll(s, `"`, `'`)
	s = strings.ReplaceAll(s, "\r", "")
	s = strings.ReplaceAll(s, "\n", "")
	return s
}

// BuildMediaPlaylist generates a complete virtual VOD M3U8 playlist (#EXT-X-ENDLIST)
// so clients can seek immediately to any timestamp without waiting for full transcoding.
func BuildMediaPlaylist(p PlaylistParams) string {
	segDur := p.SegmentDuration
	if segDur <= 0 {
		segDur = DefaultSegmentDuration
	}
	playlistID := p.PlaylistID
	if playlistID == "" {
		playlistID = "main"
	}
	ext := normalizeSegmentExt(p.SegmentContainer)

	durSec := p.DurationSec
	if durSec <= 0 && p.DurationTicks > 0 {
		durSec = float64(p.DurationTicks) / float64(TicksPerSecond)
	}
	// Fallback for unknown duration (e.g., live stream or unprobed media): 1 segment
	if durSec <= 0 {
		durSec = float64(segDur)
	}

	durs := SegmentDurations(durSec, segDur)
	queryStr := encodeQuery(p.Query)

	var b strings.Builder
	b.WriteString("#EXTM3U\n")
	b.WriteString("#EXT-X-VERSION:3\n")
	b.WriteString(fmt.Sprintf("#EXT-X-TARGETDURATION:%d\n", segDur))
	b.WriteString("#EXT-X-MEDIA-SEQUENCE:0\n")
	b.WriteString("#EXT-X-PLAYLIST-TYPE:VOD\n")

	for idx, d := range durs {
		b.WriteString(fmt.Sprintf("#EXTINF:%.6f,\n", d))
		segURL := fmt.Sprintf("hls1/%s/%d.%s", playlistID, idx, ext)
		if queryStr != "" {
			segURL += "?" + queryStr
		}
		b.WriteString(segURL)
		b.WriteByte('\n')
	}

	b.WriteString("#EXT-X-ENDLIST\n")
	return b.String()
}

func hlsCodecsAttr(videoCodec, audioCodec string) string {
	v := "avc1.640028"
	switch strings.ToLower(strings.TrimSpace(videoCodec)) {
	case "hevc", "h265":
		v = "hvc1.1.6.L120.90"
	case "av1":
		v = "av01.0.08M.08"
	}
	a := "mp4a.40.2"
	switch strings.ToLower(strings.TrimSpace(audioCodec)) {
	case "ac3":
		a = "ac-3"
	case "eac3":
		a = "ec-3"
	case "mp3":
		a = "mp4a.40.34"
	case "opus":
		a = "opus"
	}
	return v + "," + a
}

func normalizeSegmentExt(container string) string {
	switch strings.ToLower(strings.TrimPrefix(strings.TrimSpace(container), ".")) {
	case "mp4", "fmp4":
		return "mp4"
	case "m4s":
		return "m4s"
	default:
		return "ts"
	}
}

func encodeQuery(q url.Values) string {
	if len(q) == 0 {
		return ""
	}
	return q.Encode()
}

// ParseSegmentID parses a segment filename like "12.ts", "seg_12.ts", or "0.m4s" into index and ext.
func ParseSegmentID(raw string) (int, string, error) {
	raw = strings.TrimSpace(raw)
	dot := strings.LastIndexByte(raw, '.')
	if dot <= 0 || dot == len(raw)-1 {
		return 0, "", fmt.Errorf("invalid segment filename: %q", raw)
	}
	name := raw[:dot]
	ext := strings.ToLower(raw[dot+1:])
	name = strings.TrimPrefix(name, "seg_")
	idx, err := strconv.Atoi(name)
	if err != nil || idx < 0 {
		return 0, "", fmt.Errorf("invalid segment index in %q", raw)
	}
	return idx, ext, nil
}
