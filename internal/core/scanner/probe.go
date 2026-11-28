package scanner

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ambra-media/ambra/internal/core/transcode"
	"github.com/ambra-media/ambra/internal/pkg/netutil"
	"github.com/ambra-media/ambra/internal/pkg/ticks"
)

const (
	defaultProbeTimeout          = 15 * time.Second
	maxProbeOutputBytes          = 16 << 20 // 16 MiB
	probeLocalProtocolWhitelist  = "file,pipe"
	probeRemoteProtocolWhitelist = "http,https,tcp,tls,crypto"
)

type limitedProbeBuffer struct {
	buf       bytes.Buffer
	max       int
	truncated bool
}

func (l *limitedProbeBuffer) Write(p []byte) (int, error) {
	if l.max <= 0 {
		l.truncated = true
		return len(p), nil
	}
	rem := l.max - l.buf.Len()
	if rem <= 0 {
		l.truncated = true
		return len(p), nil
	}
	if len(p) > rem {
		_, _ = l.buf.Write(p[:rem])
		l.truncated = true
		return len(p), nil
	}
	_, _ = l.buf.Write(p)
	return len(p), nil
}

// ProbedStream holds normalized metadata for a single embedded stream discovered by ffprobe.
type ProbedStream struct {
	Index        int
	StreamType   string // Video / Audio / Subtitle
	Codec        string
	Language     string
	SubVariant   string // chs / cht / bilingual / ""
	DisplayTitle string
	Title        string
	Width        int
	Height       int
	VideoRange   string // DolbyVision / HDR10 / HLG / SDR
	AudioLayout  string // atmos / 7.1 / 5.1 / stereo / mono / ...
	Channels     int
	SampleRate   int
	Bitrate      int
	IsDefault    bool
	IsForced     bool
	IsExternal   bool
}

// ProbedChapter holds a normalized video chapter discovered by ffprobe.
type ProbedChapter struct {
	StartPositionTicks int64
	Name               string
}

// ProbeResult holds the parsed container-level and stream-level media metadata from ffprobe.
type ProbeResult struct {
	Container      string
	RuntimeTicks   int64
	Bitrate        int
	Size           int64
	Tags           map[string]string
	Streams        []ProbedStream
	Chapters       []ProbedChapter
	HasAttachedPic bool
}

// Prober executes ffprobe to extract real media metadata from local video files.
type Prober struct {
	mu            sync.RWMutex
	ffprobePath   string
	timeout       time.Duration
	disabled      bool
	allowLoopback bool
}

// NewProber creates a Prober using the provided ffprobe executable path.
// If ffprobePath is empty, it defaults to "ffprobe".
func NewProber(ffprobePath string) *Prober {
	p := strings.TrimSpace(ffprobePath)
	if p == "" {
		p = "ffprobe"
	}
	return &Prober{
		ffprobePath: p,
		timeout:     defaultProbeTimeout,
	}
}

// NewDisabledProber returns a Prober that skips probing without error.
func NewDisabledProber() *Prober {
	return &Prober{disabled: true}
}

// IsEnabled reports whether this prober is non-nil and active.
func (p *Prober) IsEnabled() bool {
	if p == nil {
		return false
	}
	p.mu.RLock()
	enabled := !p.disabled && strings.TrimSpace(p.ffprobePath) != ""
	p.mu.RUnlock()
	return enabled
}

// SetTimeout overrides the per-file ffprobe execution timeout.
func (p *Prober) SetTimeout(d time.Duration) {
	if p != nil && d > 0 {
		p.mu.Lock()
		p.timeout = d
		p.mu.Unlock()
	}
}

// Probe invokes ffprobe on filePath and parses its JSON output.
// If the prober is nil/disabled, it returns (nil, nil).
// If ffprobe is unavailable, times out, or filePath is not a valid media file, it returns (nil, err).
func (p *Prober) Probe(ctx context.Context, filePath string) (*ProbeResult, error) {
	if p == nil {
		return nil, nil
	}
	p.mu.RLock()
	disabled := p.disabled
	ffprobePath := p.ffprobePath
	timeout := p.timeout
	allowLoopback := p.allowLoopback
	p.mu.RUnlock()

	if disabled || strings.TrimSpace(ffprobePath) == "" {
		return nil, nil
	}
	trimmed := strings.TrimSpace(filePath)
	if trimmed == "" {
		return nil, errors.New("ffprobe: empty file path")
	}
	if strings.ContainsAny(trimmed, "\r\n\x00") {
		return nil, errors.New("ffprobe: file path contains forbidden control characters")
	}
	lower := strings.ToLower(trimmed)
	probeTarget := trimmed
	isRemote := strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://")
	whitelist := probeLocalProtocolWhitelist
	if isRemote {
		proxy, err := transcode.StartLoopbackStreamProxy(transcode.LoopbackProxyOptions{
			TargetURL:        trimmed,
			AllowPrivateLAN:  true,
			DisallowLoopback: !allowLoopback,
		})
		if err != nil {
			return nil, fmt.Errorf("ffprobe: invalid remote url %q: %w", filePath, err)
		}
		defer proxy.Close()
		probeTarget = proxy.URL()
		whitelist = probeRemoteProtocolWhitelist
	} else {
		absPath, err := filepath.Abs(trimmed)
		if err != nil {
			return nil, fmt.Errorf("ffprobe: resolve path %q: %w", filePath, err)
		}
		probeTarget = absPath
	}

	if timeout <= 0 {
		timeout = defaultProbeTimeout
	}
	probeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	args := []string{
		"-v", "quiet",
		"-protocol_whitelist", whitelist,
	}
	if isRemote {
		args = append(args, "-follow_redirects", "0", "-rw_timeout", "15000000")
	}
	args = append(args,
		"-print_format", "json",
		"-show_format",
		"-show_streams",
		"-show_chapters",
		"--",
		probeTarget,
	)
	args = netutil.FilterFFmpegArgs(ffprobePath, args)
	cmd := exec.CommandContext(probeCtx, ffprobePath, args...)
	stdout := &limitedProbeBuffer{max: maxProbeOutputBytes}
	cmd.Stdout = stdout
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("ffprobe %q: %w", filePath, err)
	}
	if stdout.truncated {
		return nil, fmt.Errorf("ffprobe %q: output exceeds maximum size of %d bytes", filePath, maxProbeOutputBytes)
	}
	return ParseFFprobeJSON(stdout.buf.Bytes())
}

type ffprobeOutput struct {
	Format   *ffprobeFormat   `json:"format"`
	Streams  []ffprobeStream  `json:"streams"`
	Chapters []ffprobeChapter `json:"chapters"`
}

type ffprobeChapter struct {
	ID        int               `json:"id"`
	TimeBase  string            `json:"time_base"`
	Start     int64             `json:"start"`
	StartTime string            `json:"start_time"`
	EndTime   string            `json:"end_time"`
	Tags      map[string]string `json:"tags"`
}

type ffprobeFormat struct {
	FormatName string            `json:"format_name"`
	Duration   string            `json:"duration"`
	Size       string            `json:"size"`
	BitRate    string            `json:"bit_rate"`
	Tags       map[string]string `json:"tags"`
}

type ffprobeStream struct {
	Index          int               `json:"index"`
	CodecType      string            `json:"codec_type"`
	CodecName      string            `json:"codec_name"`
	Profile        string            `json:"profile"`
	Width          int               `json:"width"`
	Height         int               `json:"height"`
	ColorTransfer  string            `json:"color_transfer"`
	ColorPrimaries string            `json:"color_primaries"`
	ColorSpace     string            `json:"color_space"`
	Channels       int               `json:"channels"`
	ChannelLayout  string            `json:"channel_layout"`
	SampleRate     string            `json:"sample_rate"`
	BitRate        string            `json:"bit_rate"`
	Duration       string            `json:"duration"`
	Disposition    map[string]int    `json:"disposition"`
	Tags           map[string]string `json:"tags"`
	SideDataList   []ffprobeSideData `json:"side_data_list"`
}

type ffprobeSideData struct {
	SideDataType string `json:"side_data_type"`
}

// ParseFFprobeJSON parses the JSON output of `ffprobe -show_format -show_streams`.
func ParseFFprobeJSON(raw []byte) (*ProbeResult, error) {
	if len(raw) == 0 {
		return nil, errors.New("ffprobe: empty output")
	}
	var payload ffprobeOutput
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("ffprobe: invalid json: %w", err)
	}
	if payload.Format == nil && len(payload.Streams) == 0 {
		return nil, errors.New("ffprobe: no format or stream metadata")
	}

	res := &ProbeResult{}
	if payload.Format != nil {
		res.Container = normalizeProbeContainer(payload.Format.FormatName)
		if sec, err := strconv.ParseFloat(strings.TrimSpace(payload.Format.Duration), 64); err == nil && sec > 0 {
			res.RuntimeTicks = ticks.FromSeconds(sec)
		}
		if br, err := strconv.Atoi(strings.TrimSpace(payload.Format.BitRate)); err == nil && br > 0 {
			res.Bitrate = br
		}
		if sz, err := strconv.ParseInt(strings.TrimSpace(payload.Format.Size), 10, 64); err == nil && sz > 0 {
			res.Size = sz
		}
		if len(payload.Format.Tags) > 0 {
			res.Tags = make(map[string]string, len(payload.Format.Tags))
			for k, v := range payload.Format.Tags {
				res.Tags[k] = strings.TrimSpace(v)
			}
		}
	}

	var maxStreamDur float64
	for _, st := range payload.Streams {
		if sec, err := strconv.ParseFloat(strings.TrimSpace(st.Duration), 64); err == nil && sec > maxStreamDur {
			maxStreamDur = sec
		}

		codecType := strings.ToLower(strings.TrimSpace(st.CodecType))
		codec := strings.ToLower(strings.TrimSpace(st.CodecName))

		switch codecType {
		case "video":
			// Skip embedded cover art (attached_pic) so it is not treated as a video track.
			if st.Disposition["attached_pic"] == 1 {
				res.HasAttachedPic = true
				continue
			}
			vRange := detectVideoRange(st)
			br := parseStreamBitrate(st)
			res.Streams = append(res.Streams, ProbedStream{
				Index:        st.Index,
				StreamType:   "Video",
				Codec:        codec,
				Title:        tagValue(st.Tags, "title"),
				DisplayTitle: buildVideoDisplayTitle(st.Height, codec, vRange),
				Width:        st.Width,
				Height:       st.Height,
				VideoRange:   vRange,
				Bitrate:      br,
				IsDefault:    st.Disposition["default"] == 1,
				IsExternal:   false,
			})

		case "audio":
			lang := strings.ToLower(tagValue(st.Tags, "language"))
			if lang == "" {
				lang = "und"
			}
			title := tagValue(st.Tags, "title")
			layout := detectAudioLayout(st, title)
			sr, _ := strconv.Atoi(strings.TrimSpace(st.SampleRate))
			br := parseStreamBitrate(st)
			res.Streams = append(res.Streams, ProbedStream{
				Index:        st.Index,
				StreamType:   "Audio",
				Codec:        codec,
				Language:     lang,
				Title:        title,
				DisplayTitle: buildAudioDisplayTitle(lang, title, codec, layout),
				AudioLayout:  layout,
				Channels:     st.Channels,
				SampleRate:   sr,
				Bitrate:      br,
				IsDefault:    st.Disposition["default"] == 1,
				IsExternal:   false,
			})

		case "subtitle":
			rawLang := strings.ToLower(tagValue(st.Tags, "language"))
			title := tagValue(st.Tags, "title")
			hint := sanitizeSubtitleHint(title, rawLang)
			cls := ClassifySubtitle(hint, codec)

			lang := cls.Language
			if lang == "und" && rawLang != "" {
				lang = rawLang
			}
			displayTitle := toEmbeddedSubtitleTitle(cls.DisplayTitle)

			res.Streams = append(res.Streams, ProbedStream{
				Index:        st.Index,
				StreamType:   "Subtitle",
				Codec:        cls.Codec,
				Language:     lang,
				SubVariant:   cls.SubVariant,
				DisplayTitle: displayTitle,
				Title:        title,
				IsDefault:    st.Disposition["default"] == 1 || cls.IsDefault,
				IsForced:     st.Disposition["forced"] == 1 || cls.IsForced,
				IsExternal:   false,
			})
		}
	}

	if res.RuntimeTicks == 0 && maxStreamDur > 0 {
		res.RuntimeTicks = ticks.FromSeconds(maxStreamDur)
	}

	if len(payload.Chapters) > 0 {
		chapters := make([]ProbedChapter, 0, len(payload.Chapters))
		for idx, ch := range payload.Chapters {
			var startSec float64
			if s, err := strconv.ParseFloat(strings.TrimSpace(ch.StartTime), 64); err == nil && s >= 0 {
				startSec = s
			} else if ch.Start > 0 && strings.Contains(ch.TimeBase, "/") {
				parts := strings.SplitN(ch.TimeBase, "/", 2)
				num, err1 := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
				den, err2 := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
				if err1 == nil && err2 == nil && den > 0 {
					startSec = float64(ch.Start) * num / den
				}
			}
			name := tagValue(ch.Tags, "title")
			if name == "" {
				name = fmt.Sprintf("Chapter %d", idx+1)
			}
			chapters = append(chapters, ProbedChapter{
				StartPositionTicks: ticks.FromSeconds(startSec),
				Name:               name,
			})
		}
		res.Chapters = chapters
	}
	return res, nil
}

func normalizeProbeContainer(formatName string) string {
	lower := strings.ToLower(strings.TrimSpace(formatName))
	if lower == "" {
		return ""
	}
	switch {
	case strings.Contains(lower, "matroska"):
		return "mkv"
	case strings.Contains(lower, "mp4") || strings.Contains(lower, "mov"):
		return "mp4"
	case strings.Contains(lower, "mpegts"):
		return "ts"
	case strings.Contains(lower, "avi"):
		return "avi"
	case strings.Contains(lower, "webm"):
		return "webm"
	}
	if idx := strings.IndexByte(lower, ','); idx > 0 {
		return lower[:idx]
	}
	return lower
}

func detectVideoRange(st ffprobeStream) string {
	for _, sd := range st.SideDataList {
		t := strings.ToLower(sd.SideDataType)
		if strings.Contains(t, "dovi") || strings.Contains(t, "dolby vision") {
			return "DolbyVision"
		}
	}
	switch strings.ToLower(strings.TrimSpace(st.ColorTransfer)) {
	case "smpte2084":
		return "HDR10"
	case "arib-std-b67":
		return "HLG"
	default:
		return "SDR"
	}
}

func detectAudioLayout(st ffprobeStream, title string) string {
	profileLower := strings.ToLower(st.Profile)
	titleLower := strings.ToLower(title)
	if strings.Contains(profileLower, "atmos") || strings.Contains(titleLower, "atmos") {
		return "atmos"
	}

	cl := strings.ToLower(strings.TrimSpace(st.ChannelLayout))
	switch {
	case strings.Contains(cl, "7.1"):
		return "7.1"
	case strings.Contains(cl, "5.1"):
		return "5.1"
	case cl == "stereo":
		return "stereo"
	case cl == "mono":
		return "mono"
	case cl != "":
		return cl
	}

	switch st.Channels {
	case 8:
		return "7.1"
	case 6:
		return "5.1"
	case 2:
		return "stereo"
	case 1:
		return "mono"
	default:
		if st.Channels > 0 {
			return fmt.Sprintf("%dch", st.Channels)
		}
		return ""
	}
}

func parseStreamBitrate(st ffprobeStream) int {
	if br, err := strconv.Atoi(strings.TrimSpace(st.BitRate)); err == nil && br > 0 {
		return br
	}
	for k, v := range st.Tags {
		kl := strings.ToLower(k)
		if kl == "bps" || strings.HasPrefix(kl, "bps-") {
			if br, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && br > 0 {
				return br
			}
		}
	}
	return 0
}

func tagValue(tags map[string]string, key string) string {
	if len(tags) == 0 {
		return ""
	}
	if v, ok := tags[key]; ok {
		return strings.TrimSpace(v)
	}
	for k, v := range tags {
		if strings.EqualFold(k, key) {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func buildVideoDisplayTitle(height int, codec, videoRange string) string {
	var parts []string
	if height > 0 {
		parts = append(parts, fmt.Sprintf("%dp", height))
	}
	if codec != "" {
		parts = append(parts, strings.ToUpper(codec))
	}
	if videoRange != "" && videoRange != "SDR" {
		parts = append(parts, videoRange)
	}
	if len(parts) == 0 {
		return "Video"
	}
	return strings.Join(parts, " ")
}

func buildAudioDisplayTitle(lang, title, codec, layout string) string {
	var parts []string
	if title != "" {
		parts = append(parts, title)
	} else if lang != "" && lang != "und" {
		parts = append(parts, strings.ToUpper(lang))
	}
	if codec != "" {
		parts = append(parts, strings.ToUpper(codec))
	}
	if layout != "" {
		parts = append(parts, layout)
	}
	if len(parts) == 0 {
		return "Audio"
	}
	return strings.Join(parts, " ")
}

func sanitizeSubtitleHint(title, lang string) string {
	// Replace path separators so filepath.Base inside ClassifySubtitle does not truncate title tokens.
	cleanTitle := strings.NewReplacer("/", " ", "\\", " ").Replace(title)
	return strings.TrimSpace(cleanTitle + " " + lang)
}

func toEmbeddedSubtitleTitle(displayTitle string) string {
	if idx := strings.LastIndex(displayTitle, " ("); idx >= 0 {
		return displayTitle[:idx] + " (内封 " + displayTitle[idx+2:]
	}
	return displayTitle + " (内封)"
}
