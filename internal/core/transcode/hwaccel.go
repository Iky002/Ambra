package transcode

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

// checkFileFn verifies whether a hardware device path or glob pattern exists on the host.
// It returns sorted matching paths when found, or nil otherwise. Injectable for unit tests.
var checkFileFn = defaultCheckFile

func defaultCheckFile(pattern string) []string {
	if strings.ContainsAny(pattern, "*?[") {
		matches, err := filepath.Glob(pattern)
		if err != nil || len(matches) == 0 {
			return nil
		}
		sort.Strings(matches)
		return matches
	}
	if _, err := os.Stat(pattern); err == nil {
		return []string{pattern}
	}
	return nil
}

// CodecInfo mirrors Emby's /Encoding/CodecInformation/Video entry structure.
type CodecInfo struct {
	Name                   string   `json:"Name"`
	CodecType              string   `json:"CodecType"` // VideoEncoder / VideoDecoder
	ShortName              string   `json:"ShortName"`
	VideoMediaType         string   `json:"VideoMediaType"` // h264 / hevc
	Direction              string   `json:"Direction"`      // Encoder / Decoder
	FrameworkCodec         string   `json:"FrameworkCodec"`
	IsHardwareCodec        bool     `json:"IsHardwareCodec"`
	SecondaryFramework     string   `json:"SecondaryFramework,omitempty"` // VideoToolbox / NVENC / QSV / VAAPI / AMF
	TonemapFilter          string   `json:"TonemapFilter,omitempty"`
	SupportedColorFormats  []string `json:"SupportedColorFormats,omitempty"`
	MinWidth               int      `json:"MinWidth,omitempty"`
	MaxWidth               int      `json:"MaxWidth,omitempty"`
	MinHeight              int      `json:"MinHeight,omitempty"`
	MaxHeight              int      `json:"MaxHeight,omitempty"`
	IsEnabledByDefault     bool     `json:"IsEnabledByDefault"`
}

// HWCapabilities summarizes detected FFmpeg hardware acceleration and encoder capabilities.
type HWCapabilities struct {
	FFmpegAvailable      bool            `json:"FFmpegAvailable"`
	Platform             string          `json:"Platform"`
	AvailableHWAccels    []string        `json:"AvailableHWAccels"`
	AvailableEncoders    map[string]bool `json:"AvailableEncoders"`
	PreferredHWAccel     string          `json:"PreferredHWAccel"`
	PreferredH264Encoder string          `json:"PreferredH264Encoder"`
	PreferredHEVCEncoder string          `json:"PreferredHEVCEncoder"`
	VAAPIDevice          string          `json:"VAAPIDevice,omitempty"`
	Codecs               []CodecInfo     `json:"Codecs"`
	ProbedAt             time.Time       `json:"ProbedAt"`
}

// SoftwareOnlyCapabilities returns a software-only HWCapabilities (libx264 / libx265)
// used as a safe fallback when hardware encoders are unavailable or fail at runtime.
func SoftwareOnlyCapabilities() *HWCapabilities {
	caps := BuildHWCapabilities(nil, map[string]bool{"libx264": true, "libx265": true}, runtime.GOOS)
	caps.FFmpegAvailable = true
	return caps
}

// SelectVideoEncoder chooses the best available encoder for the requested target codec ("h264" or "hevc").
func (c *HWCapabilities) SelectVideoEncoder(targetCodec string) string {
	switch strings.ToLower(strings.TrimSpace(targetCodec)) {
	case "hevc", "h265":
		if c != nil && c.PreferredHEVCEncoder != "" {
			return c.PreferredHEVCEncoder
		}
		return "libx265"
	default:
		if c != nil && c.PreferredH264Encoder != "" {
			return c.PreferredH264Encoder
		}
		return "libx264"
	}
}

// TonemapFilterForEncoder returns the HDR-to-SDR tonemap filter chain for the selected encoder.
func (c *HWCapabilities) TonemapFilterForEncoder(encoder string) string {
	if c != nil {
		for _, ci := range c.Codecs {
			if strings.EqualFold(ci.ShortName, encoder) && strings.TrimSpace(ci.TonemapFilter) != "" {
				return ci.TonemapFilter
			}
		}
	}
	return "zscale=t=linear:npl=100,tonemap=hable,zscale=p=bt709:t=bt709:m=bt709,format=yuv420p"
}

// HardwareDetector probes and caches local FFmpeg hardware acceleration capabilities.
type HardwareDetector struct {
	ffmpegPath string
	mu         sync.RWMutex
	cached     *HWCapabilities
}

// NewHardwareDetector creates a detector using the given ffmpeg executable path.
func NewHardwareDetector(ffmpegPath string) *HardwareDetector {
	if strings.TrimSpace(ffmpegPath) == "" {
		ffmpegPath = "ffmpeg"
	}
	return &HardwareDetector{ffmpegPath: ffmpegPath}
}

// Detect returns cached hardware capabilities, probing FFmpeg on first call.
// Transient errors or caller context cancellation are not permanently cached.
func (d *HardwareDetector) Detect(ctx context.Context) *HWCapabilities {
	d.mu.RLock()
	if d.cached != nil {
		caps := d.cached
		d.mu.RUnlock()
		return caps
	}
	d.mu.RUnlock()

	d.mu.Lock()
	defer d.mu.Unlock()
	if d.cached != nil {
		return d.cached
	}
	caps := ProbeHardware(context.WithoutCancel(ctx), d.ffmpegPath)
	if caps != nil && caps.FFmpegAvailable {
		d.cached = caps
	}
	return caps
}

// SetCapabilities injects capabilities directly (useful for tests or manual overrides).
func (d *HardwareDetector) SetCapabilities(caps *HWCapabilities) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.cached = caps
}

// ProbeHardware runs `ffmpeg -hwaccels` and `ffmpeg -encoders` to detect available acceleration.
func ProbeHardware(ctx context.Context, ffmpegPath string) *HWCapabilities {
	if strings.TrimSpace(ffmpegPath) == "" {
		ffmpegPath = "ffmpeg"
	}
	probeCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()

	hwOut, err1 := runFFmpegOutput(probeCtx, ffmpegPath, "-hide_banner", "-hwaccels")
	encOut, err2 := runFFmpegOutput(probeCtx, ffmpegPath, "-hide_banner", "-encoders")

	if err1 != nil && err2 != nil {
		caps := BuildHWCapabilities(nil, nil, runtime.GOOS)
		caps.FFmpegAvailable = false
		return caps
	}

	accels := ParseHWAccels(hwOut)
	encoders := ParseEncoders(encOut)
	caps := BuildHWCapabilities(accels, encoders, runtime.GOOS)
	caps.FFmpegAvailable = true
	return caps
}

func runFFmpegOutput(ctx context.Context, ffmpegPath string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, ffmpegPath, args...)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	return out.String(), err
}

// ParseHWAccels parses the output of `ffmpeg -hwaccels`.
func ParseHWAccels(output string) []string {
	var accels []string
	seen := make(map[string]struct{})
	inList := false
	for _, rawLine := range strings.Split(output, "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" {
			continue
		}
		lower := strings.ToLower(line)
		if strings.HasPrefix(lower, "hardware acceleration methods") {
			inList = true
			continue
		}
		if !inList {
			continue
		}
		if _, ok := seen[lower]; ok {
			continue
		}
		seen[lower] = struct{}{}
		accels = append(accels, lower)
	}
	return accels
}

// ParseEncoders parses the output of `ffmpeg -encoders` and returns a set of encoder names.
func ParseEncoders(output string) map[string]bool {
	encoders := make(map[string]bool)
	for _, rawLine := range strings.Split(output, "\n") {
		line := strings.TrimSpace(rawLine)
		if len(line) < 8 {
			continue
		}
		// FFmpeg encoder lines start with 6 flag chars, e.g. " V..... libx264 ..."
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		flags := fields[0]
		if len(flags) != 6 {
			continue
		}
		// First flag is V (video), A (audio), or S (subtitle)
		if flags[0] != 'V' && flags[0] != 'A' && flags[0] != 'S' {
			continue
		}
		name := strings.ToLower(fields[1])
		encoders[name] = true
	}
	return encoders
}

type hwCandidate struct {
	accel     string
	framework string
	h264Enc   string
	hevcEnc   string
	tonemap   string
	onlyOS    string // optional GOOS filter
}

var hwCandidates = []hwCandidate{
	{
		accel:     "videotoolbox",
		framework: "VideoToolbox",
		h264Enc:   "h264_videotoolbox",
		hevcEnc:   "hevc_videotoolbox",
		tonemap:   "zscale=t=linear:npl=100,tonemap=hable,zscale=p=bt709:t=bt709:m=bt709,format=yuv420p",
		onlyOS:    "darwin",
	},
	{
		accel:     "cuda",
		framework: "NVENC",
		h264Enc:   "h264_nvenc",
		hevcEnc:   "hevc_nvenc",
		tonemap:   "zscale=t=linear:npl=100,tonemap=hable,zscale=p=bt709:t=bt709:m=bt709,format=yuv420p",
	},
	{
		accel:     "qsv",
		framework: "QSV",
		h264Enc:   "h264_qsv",
		hevcEnc:   "hevc_qsv",
		tonemap:   "zscale=t=linear:npl=100,tonemap=hable,zscale=p=bt709:t=bt709:m=bt709,format=nv12",
	},
	{
		accel:     "vaapi",
		framework: "VAAPI",
		h264Enc:   "h264_vaapi",
		hevcEnc:   "hevc_vaapi",
		tonemap:   "tonemap_vaapi=format=nv12:matrix=bt709:primaries=bt709:transfer=bt709",
	},
	{
		accel:     "d3d11va",
		framework: "AMF",
		h264Enc:   "h264_amf",
		hevcEnc:   "hevc_amf",
		tonemap:   "zscale=t=linear:npl=100,tonemap=hable,zscale=p=bt709:t=bt709:m=bt709,format=yuv420p",
		onlyOS:    "windows",
	},
}

// BuildHWCapabilities constructs HWCapabilities and Emby CodecInfo entries from parsed FFmpeg outputs.
func BuildHWCapabilities(accels []string, encoders map[string]bool, goos string) *HWCapabilities {
	if encoders == nil {
		encoders = map[string]bool{}
	}
	accelSet := make(map[string]bool, len(accels))
	for _, a := range accels {
		accelSet[strings.ToLower(a)] = true
	}

	caps := &HWCapabilities{
		Platform:             goos,
		AvailableHWAccels:    accels,
		AvailableEncoders:    encoders,
		PreferredH264Encoder: "libx264",
		PreferredHEVCEncoder: "libx265",
		ProbedAt:             time.Now().UTC(),
	}
	if caps.AvailableHWAccels == nil {
		caps.AvailableHWAccels = []string{}
	}

	// Register hardware encoders in priority order
	for _, cand := range hwCandidates {
		if cand.onlyOS != "" && cand.onlyOS != goos {
			continue
		}
		if goos == "linux" {
			switch cand.accel {
			case "vaapi", "qsv":
				nodes := checkFileFn("/dev/dri/renderD*")
				if len(nodes) == 0 {
					continue
				}
				if caps.VAAPIDevice == "" {
					caps.VAAPIDevice = nodes[0]
				}
			case "cuda":
				if len(checkFileFn("/dev/nvidia0")) == 0 && len(checkFileFn("/dev/nvidiactl")) == 0 {
					continue
				}
			}
		}
		hasAccel := accelSet[cand.accel] ||
			(cand.accel == "cuda" && accelSet["nvdec"]) ||
			(cand.framework == "AMF" && (accelSet["amf"] || accelSet["dxva2"]))
		hasH264 := encoders[cand.h264Enc]
		hasHEVC := encoders[cand.hevcEnc]

		// Require encoder to exist; for videotoolbox/nvenc/qsv/vaapi also accept if encoder is present when accel matches
		if hasH264 && (hasAccel || len(accels) == 0) {
			if caps.PreferredHWAccel == "" {
				caps.PreferredHWAccel = cand.accel
			}
			if caps.PreferredH264Encoder == "libx264" {
				caps.PreferredH264Encoder = cand.h264Enc
			}
			caps.Codecs = append(caps.Codecs, CodecInfo{
				Name:                  cand.framework + " H.264",
				CodecType:             "VideoEncoder",
				ShortName:             cand.h264Enc,
				VideoMediaType:        "h264",
				Direction:             "Encoder",
				FrameworkCodec:        cand.h264Enc,
				IsHardwareCodec:       true,
				SecondaryFramework:    cand.framework,
				TonemapFilter:         cand.tonemap,
				SupportedColorFormats: []string{"nv12", "yuv420p"},
				MinWidth:              64,
				MaxWidth:              4096,
				MinHeight:             64,
				MaxHeight:             2160,
				IsEnabledByDefault:    true,
			})
		}
		if hasHEVC && (hasAccel || len(accels) == 0) {
			if caps.PreferredHWAccel == "" {
				caps.PreferredHWAccel = cand.accel
			}
			if caps.PreferredHEVCEncoder == "libx265" {
				caps.PreferredHEVCEncoder = cand.hevcEnc
			}
			caps.Codecs = append(caps.Codecs, CodecInfo{
				Name:                  cand.framework + " H.265 (HEVC)",
				CodecType:             "VideoEncoder",
				ShortName:             cand.hevcEnc,
				VideoMediaType:        "hevc",
				Direction:             "Encoder",
				FrameworkCodec:        cand.hevcEnc,
				IsHardwareCodec:       true,
				SecondaryFramework:    cand.framework,
				TonemapFilter:         cand.tonemap,
				SupportedColorFormats: []string{"nv12", "p010le", "yuv420p"},
				MinWidth:              64,
				MaxWidth:              7680,
				MinHeight:             64,
				MaxHeight:             4320,
				IsEnabledByDefault:    true,
			})
		}
	}

	// Always register software fallback encoders (libx264 / libx265)
	if len(encoders) == 0 || encoders["libx264"] {
		caps.Codecs = append(caps.Codecs, CodecInfo{
			Name:                  "x264 Software",
			CodecType:             "VideoEncoder",
			ShortName:             "libx264",
			VideoMediaType:        "h264",
			Direction:             "Encoder",
			FrameworkCodec:        "libx264",
			IsHardwareCodec:       false,
			TonemapFilter:         "zscale=t=linear:npl=100,tonemap=hable,zscale=p=bt709:t=bt709:m=bt709,format=yuv420p",
			SupportedColorFormats: []string{"yuv420p"},
			MinWidth:              64,
			MaxWidth:              4096,
			MinHeight:             64,
			MaxHeight:             2160,
			IsEnabledByDefault:    true,
		})
	}
	if len(encoders) == 0 || encoders["libx265"] {
		caps.Codecs = append(caps.Codecs, CodecInfo{
			Name:                  "x265 Software",
			CodecType:             "VideoEncoder",
			ShortName:             "libx265",
			VideoMediaType:        "hevc",
			Direction:             "Encoder",
			FrameworkCodec:        "libx265",
			IsHardwareCodec:       false,
			TonemapFilter:         "zscale=t=linear:npl=100,tonemap=hable,zscale=p=bt709:t=bt709:m=bt709,format=yuv420p",
			SupportedColorFormats: []string{"yuv420p", "yuv420p10le"},
			MinWidth:              64,
			MaxWidth:              7680,
			MinHeight:             64,
			MaxHeight:             4320,
			IsEnabledByDefault:    false,
		})
	}

	return caps
}
