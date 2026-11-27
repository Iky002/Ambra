package transcode

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/pkg/netutil"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

// ErrSessionClosed is returned when operations are attempted on a destroyed transcode session.
var ErrSessionClosed = errors.New("transcode session closed")

const (
	defaultIdleTimeout            = 60 * time.Second
	defaultCleanupTick            = 15 * time.Second
	defaultSeekThreshold          = 3
	defaultPollInterval           = 50 * time.Millisecond
	defaultSegmentWaitTimeout     = 90 * time.Second
	defaultMaxSessions            = 16
	defaultMaxSessionsPerUser     = 4
	maxStderrBytes                = 64 * 1024
	maxRemoteSubtitleBytes        = int64(16 << 20) // 16 MiB
	remoteSubtitleDownloadTimeout = 15 * time.Second
	defaultHTTPUserAgent      = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"
	softwareTonemapFilter     = "zscale=t=linear:npl=100,tonemap=hable,zscale=p=bt709:t=bt709:m=bt709,format=yuv420p"
	qsvSoftwareTonemapFilter  = "zscale=t=linear:npl=100,tonemap=hable,zscale=p=bt709:t=bt709:m=bt709,format=nv12"
)

type safeRingBuffer struct {
	mu   sync.Mutex
	data []byte
	max  int
}

func newSafeRingBuffer(maxBytes int) *safeRingBuffer {
	return &safeRingBuffer{max: maxBytes}
}

func (b *safeRingBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.max <= 0 {
		return len(p), nil
	}
	if len(p) >= b.max {
		b.data = append(b.data[:0], p[len(p)-b.max:]...)
		return len(p), nil
	}
	total := len(b.data) + len(p)
	if total > b.max {
		drop := total - b.max
		copy(b.data, b.data[drop:])
		b.data = b.data[:len(b.data)-drop]
	}
	b.data = append(b.data, p...)
	return len(p), nil
}

func (b *safeRingBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return string(b.data)
}

const (
	MaxVideoBitrate    = 200_000_000 // 200 Mbps cap prevents vBitrate*2 32-bit int overflow
	MaxAudioBitrate    = 1_536_000   // 1.536 Mbps cap
	MaxAudioChannels   = 8
	MaxSegmentDuration = 60
	MaxVideoDimension  = 16384
)

// SessionParams holds all parameters required to run a dynamic HLS FFmpeg transcode session.
type SessionParams struct {
	PlaySessionID         string
	DeviceID              string
	UserID                string
	ItemID                string
	MediaSourceID         string
	InputPath             string
	VideoCodec            string // "copy" for DirectStream, "h264"/"hevc" for Transcode
	AudioCodec            string // whitelist: aac, mp3, opus, ac3, eac3, flac, copy
	VideoBitrate          int    // bits/sec (e.g. 4000000)
	AudioBitrate          int    // bits/sec (e.g. 192000)
	AudioChannels         int    // default 2
	MaxWidth              int
	MaxHeight             int
	StartTimeTicks        int64 // optional initial seek offset in 100ns ticks
	AudioStreamIndex      *int
	SubtitleStreamIndex   *int
	SubtitleRelativeIndex int    // 0-based index among embedded subtitle streams (for FFmpeg subtitles filter :si=)
	SubtitleMethod        string // "Encode" for burn-in
	SubtitleCodec         string // e.g. "ass", "srt", "pgssub", "dvd_subtitle"
	SubtitlePath          string // external subtitle path when IsExternal
	IsHDR                 bool              // true when source video is HDR (enables tonemap filter when transcoding)
	SegmentDuration       int               // seconds, default 6
	SegmentContainer      string            // "ts" (default), "mp4", "m4s"
	DurationSec           float64
	InputHeaders          map[string]string // optional upstream HTTP headers (Cookie/Referer/User-Agent/Authorization)
	SoftwareCRF           int               // optional x264/x265 CRF override (1..51)
	SoftwarePreset        string            // optional x264/x265 preset override
	DisableToneMapping    bool              // disables HDR tonemapping when true
	ToneMappingAlgorithm  string            // optional tonemap algorithm override
	ToneMappingPeak       float64           // optional tonemap peak npl override
	FMP4InitFilename      string            // optional custom fMP4 init filename inside outDir
	AudioOnly             bool              // true for audio-only HLS (-vn)
}

func (p *SessionParams) normalize() {
	if p.SegmentDuration <= 0 {
		p.SegmentDuration = DefaultSegmentDuration
	} else if p.SegmentDuration > MaxSegmentDuration {
		p.SegmentDuration = MaxSegmentDuration
	}
	p.SegmentContainer = normalizeSegmentExt(p.SegmentContainer)
	if strings.TrimSpace(p.VideoCodec) == "" {
		p.VideoCodec = "h264"
	}
	switch strings.ToLower(strings.TrimSpace(p.AudioCodec)) {
	case "aac", "mp3", "opus", "ac3", "eac3", "flac", "copy":
		p.AudioCodec = strings.ToLower(strings.TrimSpace(p.AudioCodec))
	default:
		p.AudioCodec = "aac"
	}
	if p.VideoBitrate > MaxVideoBitrate {
		p.VideoBitrate = MaxVideoBitrate
	}
	if p.AudioBitrate <= 0 {
		p.AudioBitrate = 192_000
	} else if p.AudioBitrate > MaxAudioBitrate {
		p.AudioBitrate = MaxAudioBitrate
	}
	if p.AudioChannels <= 0 {
		p.AudioChannels = 2
	} else if p.AudioChannels > MaxAudioChannels {
		p.AudioChannels = MaxAudioChannels
	}
	if p.MaxWidth < 0 {
		p.MaxWidth = 0
	} else if p.MaxWidth > MaxVideoDimension {
		p.MaxWidth = MaxVideoDimension
	}
	if p.MaxHeight < 0 {
		p.MaxHeight = 0
	} else if p.MaxHeight > MaxVideoDimension {
		p.MaxHeight = MaxVideoDimension
	}
	if p.StartTimeTicks < 0 {
		p.StartTimeTicks = 0
	}
}

func formatFFmpegSeekSeconds(sec float64) string {
	rounded := math.Round(sec*1000) / 1000
	if rounded == math.Trunc(rounded) {
		return strconv.FormatInt(int64(rounded), 10)
	}
	return strconv.FormatFloat(rounded, 'f', -1, 64)
}

// SessionSnapshot is a read-only view of an active transcoding session.
type SessionSnapshot struct {
	Key           string    `json:"Key"`
	PlaySessionID string    `json:"PlaySessionId"`
	DeviceID      string    `json:"DeviceId"`
	ItemID        string    `json:"ItemId"`
	StartSegment  int       `json:"StartSegment"`
	Running       bool      `json:"Running"`
	LastAccess    time.Time `json:"LastAccess"`
	WorkDir       string    `json:"WorkDir"`
}

// BuildFFmpegArgs constructs the FFmpeg command-line arguments for a dynamic HLS session.
// It supports both DirectStream (-c:v copy) and full Transcode (hardware or software encoder),
// with -ss seeking, -start_number, and atomic temp_file segment writing.
func BuildFFmpegArgs(params SessionParams, startSeg int, outDir string, caps *HWCapabilities) []string {
	params.normalize()
	if startSeg < 0 {
		startSeg = 0
	}
	segDur := params.SegmentDuration
	startOffsetSec := float64(startSeg * segDur)
	if params.StartTimeTicks > 0 {
		startOffsetSec += float64(params.StartTimeTicks) / float64(TicksPerSecond)
	}
	hasStartSeek := startOffsetSec > 0
	startSeekArg := formatFFmpegSeekSeconds(startOffsetSec)
	ext := params.SegmentContainer

	args := []string{
		"-hide_banner",
		"-loglevel", "warning",
		"-y",
	}

	inputPath := strings.TrimSpace(params.InputPath)
	isRemoteHTTP := isHTTPInput(inputPath)
	if !isRemoteHTTP && inputPath != "" {
		if abs, err := filepath.Abs(inputPath); err == nil {
			inputPath = abs
		}
	}

	subPath := strings.TrimSpace(params.SubtitlePath)
	if subPath != "" && !isHTTPInput(subPath) {
		if abs, err := filepath.Abs(subPath); err == nil {
			subPath = abs
		}
	}

	burnIn := strings.EqualFold(strings.TrimSpace(params.SubtitleMethod), "Encode") &&
		(subPath != "" || (params.SubtitleStreamIndex != nil && *params.SubtitleStreamIndex >= 0))
	if burnIn && (hasControlChars(subPath) || (subPath == "" && hasControlChars(inputPath))) {
		burnIn = false
	}
	isBitmapSub := burnIn && isBitmapSubtitleCodec(params.SubtitleCodec) &&
		subPath == "" &&
		params.SubtitleStreamIndex != nil && *params.SubtitleStreamIndex >= 0
	// Never pass raw http(s):// URLs into FFmpeg's subtitles= filter (bypasses -max_redirects 0).
	if burnIn && !isBitmapSub && (isHTTPInput(subPath) || (subPath == "" && isHTTPInput(inputPath))) {
		burnIn = false
	}
	isAudioOnly := params.AudioOnly || strings.EqualFold(strings.TrimSpace(params.VideoCodec), "none")
	if isAudioOnly {
		burnIn = false
		isBitmapSub = false
	}
	textBurnInSeek := burnIn && !isBitmapSub && hasStartSeek

	isVideoCopy := !isAudioOnly && strings.EqualFold(params.VideoCodec, "copy") && !burnIn
	var encoder string
	if !isAudioOnly && !isVideoCopy {
		vCodec := params.VideoCodec
		if strings.EqualFold(vCodec, "copy") {
			vCodec = "h264"
		}
		encoder = caps.SelectVideoEncoder(vCodec)
		if strings.HasSuffix(encoder, "_vaapi") {
			vaapiDev := "/dev/dri/renderD128"
			if caps != nil && caps.VAAPIDevice != "" {
				vaapiDev = caps.VAAPIDevice
			}
			args = append(args, "-vaapi_device", vaapiDev)
		}
	}

	// Restrict FFmpeg input protocols explicitly before -ss/-i.
	if !isRemoteHTTP {
		args = append(args, "-protocol_whitelist", localProtocolWhitelist)
	}

	// Fast input seek when starting from a non-zero segment or StartTimeTicks offset.
	// For text subtitle burn-in with seek, preserve input timestamps (-copyts) so libass aligns cues,
	// then trim accurately with output-side -ss.
	if hasStartSeek {
		args = append(args, "-ss", startSeekArg)
		if textBurnInSeek {
			args = append(args, "-copyts")
		}
	}

	// Resilient HTTP(S) input flags for remote STRM / Alist / cloud drive URLs.
	// Disable FFmpeg HTTP redirect following so redirects cannot pivot to cloud metadata or disallowed schemes.
	if isRemoteHTTP {
		args = append(args,
			"-protocol_whitelist", "http,https,tcp,tls,crypto",
			"-max_redirects", "0",
			"-reconnect", "1",
			"-reconnect_streamed", "1",
			"-reconnect_delay_max", "5",
			"-rw_timeout", "15000000",
			"-user_agent", defaultHTTPUserAgent,
		)
	}

	args = append(args, "-i", inputPath)

	if textBurnInSeek {
		args = append(args, "-ss", startSeekArg)
	}

	// Build video filter parts for tonemapping, subtitle burn-in, scaling, and VAAPI hardware upload.
	var vfParts []string
	if !isAudioOnly && !isVideoCopy {
		var vaapiTonemap string
		if params.IsHDR && !params.DisableToneMapping {
			customTM := strings.TrimSpace(params.ToneMappingAlgorithm) != "" || params.ToneMappingPeak > 0
			swYuvTM := softwareTonemapFilter
			swNv12TM := qsvSoftwareTonemapFilter
			if customTM {
				swYuvTM = buildSoftwareTonemapFilter(params.ToneMappingAlgorithm, params.ToneMappingPeak, "yuv420p")
				swNv12TM = buildSoftwareTonemapFilter(params.ToneMappingAlgorithm, params.ToneMappingPeak, "nv12")
			}
			tm := caps.TonemapFilterForEncoder(encoder)
			switch {
			case strings.HasSuffix(encoder, "_vaapi") && strings.Contains(tm, "vaapi"):
				vaapiTonemap = tm
			case strings.HasSuffix(encoder, "_nvenc") && (tm == "" || strings.Contains(tm, "tonemap_cuda") || strings.Contains(tm, "_cuda") || customTM):
				// BuildFFmpegArgs uses CPU decode; use valid software zscale+tonemap before NVENC encode.
				vfParts = append(vfParts, swYuvTM)
			case strings.HasSuffix(encoder, "_qsv") && (tm == "" || strings.Contains(tm, "vpp_qsv") || strings.Contains(tm, "_qsv") || customTM):
				// BuildFFmpegArgs uses CPU decode; use valid software zscale+tonemap -> nv12 for QSV encode.
				vfParts = append(vfParts, swNv12TM)
			case customTM:
				vfParts = append(vfParts, swYuvTM)
			case tm != "":
				vfParts = append(vfParts, tm)
			}
		}
		if burnIn && !isBitmapSub {
			if subPath != "" {
				if escaped, err := escapeFFmpegFilterPath(subPath); err == nil {
					vfParts = append(vfParts, fmt.Sprintf("subtitles='%s'", escaped))
				}
			} else if params.SubtitleStreamIndex != nil && *params.SubtitleStreamIndex >= 0 {
				si := params.SubtitleRelativeIndex
				if si < 0 {
					si = 0
				}
				if escaped, err := escapeFFmpegFilterPath(inputPath); err == nil {
					vfParts = append(vfParts, fmt.Sprintf("subtitles='%s':si=%d", escaped, si))
				}
			}
		}
		switch {
		case params.MaxWidth > 0 && params.MaxHeight > 0:
			vfParts = append(vfParts, fmt.Sprintf("scale='min(%d,iw)':'min(%d,ih)':force_original_aspect_ratio=decrease:force_divisible_by=2", params.MaxWidth, params.MaxHeight))
		case params.MaxWidth > 0:
			vfParts = append(vfParts, fmt.Sprintf("scale='min(%d,iw)':-2", params.MaxWidth))
		case params.MaxHeight > 0:
			vfParts = append(vfParts, fmt.Sprintf("scale=-2:'min(%d,ih)'", params.MaxHeight))
		}
		if strings.HasSuffix(encoder, "_vaapi") {
			if vaapiTonemap != "" {
				vfParts = append(vfParts, "format=p010,hwupload", vaapiTonemap)
			} else {
				vfParts = append(vfParts, "format=nv12,hwupload")
			}
		}
	}

	// Stream mapping: video (or bitmap subtitle overlay) + selected (or first) audio stream; disable muxed subtitle tracks.
	if isAudioOnly {
		if params.AudioStreamIndex != nil && *params.AudioStreamIndex >= 0 {
			args = append(args, "-map", fmt.Sprintf("0:%d", *params.AudioStreamIndex))
		} else {
			args = append(args, "-map", "0:a:0")
		}
		args = append(args, "-vn", "-sn")
	} else {
		if isBitmapSub {
			filterGraph := fmt.Sprintf("[0:v:0][0:%d]overlay", *params.SubtitleStreamIndex)
			if len(vfParts) > 0 {
				filterGraph += "," + strings.Join(vfParts, ",")
			}
			filterGraph += "[v]"
			args = append(args, "-filter_complex", filterGraph, "-map", "[v]")
		} else {
			args = append(args, "-map", "0:v:0")
		}
		if params.AudioStreamIndex != nil && *params.AudioStreamIndex >= 0 {
			args = append(args, "-map", fmt.Sprintf("0:%d?", *params.AudioStreamIndex))
		} else {
			args = append(args, "-map", "0:a:0?")
		}
		args = append(args, "-sn")

		// Video encoding: DirectStream (-c:v copy) vs Transcode
		if isVideoCopy {
			args = append(args, "-c:v", "copy")
		} else {
			args = append(args, "-c:v", encoder)

			if !isBitmapSub && len(vfParts) > 0 {
				args = append(args, "-vf", strings.Join(vfParts, ","))
			}

			// Pixel format & encoder presets
			switch {
			case strings.HasPrefix(encoder, "libx"):
				preset := normalizeX264Preset(params.SoftwarePreset)
				args = append(args, "-preset", preset, "-pix_fmt", "yuv420p")
				if params.SoftwareCRF > 0 && params.SoftwareCRF <= 51 {
					args = append(args, "-crf", strconv.Itoa(params.SoftwareCRF))
				}
			case strings.HasSuffix(encoder, "_videotoolbox"):
				args = append(args, "-pix_fmt", "yuv420p")
			case strings.HasSuffix(encoder, "_nvenc"):
				args = append(args, "-preset", "p4", "-pix_fmt", "yuv420p")
			case strings.HasSuffix(encoder, "_qsv"):
				args = append(args, "-pix_fmt", "nv12")
			}

			vBitrate := params.VideoBitrate
			if vBitrate <= 0 {
				vBitrate = 4_000_000
			} else if vBitrate > MaxVideoBitrate {
				vBitrate = MaxVideoBitrate
			}
			keyframeExpr := fmt.Sprintf("expr:gte(t,n_forced*%d)", segDur)
			if textBurnInSeek && hasStartSeek {
				keyframeExpr = fmt.Sprintf("expr:gte(t-%s,n_forced*%d)", startSeekArg, segDur)
			}
			args = append(args,
				"-b:v", strconv.Itoa(vBitrate),
				"-maxrate", strconv.Itoa(vBitrate),
				"-bufsize", strconv.Itoa(vBitrate*2),
				"-force_key_frames", keyframeExpr,
			)
		}
	}

	// Audio encoding
	if strings.EqualFold(params.AudioCodec, "copy") {
		args = append(args, "-c:a", "copy")
	} else {
		aCodec := strings.ToLower(params.AudioCodec)
		if aCodec == "mp3" {
			aCodec = "libmp3lame"
		}
		args = append(args,
			"-c:a", aCodec,
			"-b:a", strconv.Itoa(params.AudioBitrate),
			"-ac", strconv.Itoa(params.AudioChannels),
		)
	}

	// HLS muxer configuration
	segPattern := filepath.Join(outDir, fmt.Sprintf("seg_%%d.%s", ext))
	playlistPath := filepath.Join(outDir, "live.m3u8")

	args = append(args,
		"-f", "hls",
		"-hls_time", strconv.Itoa(segDur),
		"-hls_list_size", "0",
		"-hls_playlist_type", "event",
		"-start_number", strconv.Itoa(startSeg),
		"-hls_flags", "temp_file",
	)
	if ext == "mp4" || ext == "m4s" {
		initName := filepath.Base(strings.TrimSpace(params.FMP4InitFilename))
		if initName == "" || initName == "." || initName == "/" {
			initName = "init.mp4"
		}
		args = append(args, "-hls_segment_type", "fmp4", "-hls_fmp4_init_filename", initName)
	}
	if hasStartSeek && ext == "ts" && !textBurnInSeek {
		args = append(args, "-output_ts_offset", startSeekArg)
	}
	args = append(args,
		"-hls_segment_filename", segPattern,
		playlistPath,
	)
	return args
}

// ManagerOption customizes Manager behavior.
type ManagerOption func(*Manager)

// WithIdleTimeout configures how long an idle transcoding session stays alive before cleanup.
func WithIdleTimeout(d time.Duration) ManagerOption {
	return func(m *Manager) {
		if d > 0 {
			m.idleTimeout = d
		}
	}
}

// WithCleanupInterval configures the background cleanup ticker interval.
func WithCleanupInterval(d time.Duration) ManagerOption {
	return func(m *Manager) {
		if d > 0 {
			m.cleanupInterval = d
		}
	}
}

// WithCapabilities injects pre-probed hardware capabilities (useful in tests).
func WithCapabilities(caps *HWCapabilities) ManagerOption {
	return func(m *Manager) {
		m.detector.SetCapabilities(caps)
	}
}

// WithMaxSessions configures the maximum number of concurrent transcoding sessions.
func WithMaxSessions(maxSessions int) ManagerOption {
	return func(m *Manager) {
		if maxSessions > 0 {
			m.maxSessions = maxSessions
		}
	}
}

// WithMaxSessionsPerUser configures the maximum number of concurrent transcoding sessions per user.
func WithMaxSessionsPerUser(maxPerUser int) ManagerOption {
	return func(m *Manager) {
		if maxPerUser > 0 {
			m.maxSessionsPerUser = maxPerUser
		}
	}
}

// WithMaxCacheBytes configures the total disk byte cap across all transcode session working directories.
func WithMaxCacheBytes(maxBytes int64) ManagerOption {
	return func(m *Manager) {
		if maxBytes > 0 {
			m.maxCacheBytes = maxBytes
		}
	}
}

// WithThrottleConfig configures lead-buffer process throttling parameters.
func WithThrottleConfig(enabled bool, bufferSec, hysteresisSec int) ManagerOption {
	return func(m *Manager) {
		m.encOpts.EnableThrottling = enabled
		if bufferSec > 0 {
			m.encOpts.ThrottleBufferSize = bufferSec
		}
		if hysteresisSec >= 0 {
			m.encOpts.ThrottleHysteresis = hysteresisSec
		}
		m.throttleOverridden = true
	}
}

// Manager orchestrates active FFmpeg dynamic HLS transcoding sessions.
type Manager struct {
	cfg                *config.Config
	detector           *HardwareDetector
	idleTimeout        time.Duration
	cleanupInterval    time.Duration
	maxSessions        int
	maxSessionsPerUser int
	maxCacheBytes      int64

	encMu              sync.RWMutex
	store              *sqlite.Store
	encOpts            EncodingOptions
	encLoadedFromStore bool
	throttleOverridden bool

	mu              sync.Mutex
	sessions        map[string]*transcodeSession
	reservedSlots   map[string]int
	externalStreams atomic.Int64
	closed          bool

	stopCh   chan struct{}
	stopOnce sync.Once
	wg       sync.WaitGroup
}

// NewManager initializes the transcoding Manager and starts its idle-session reaper.
func NewManager(cfg *config.Config, opts ...ManagerOption) *Manager {
	ffmpegPath := "ffmpeg"
	if cfg != nil && cfg.FFmpegPath != "" {
		ffmpegPath = cfg.FFmpegPath
	}
	m := &Manager{
		cfg:                cfg,
		detector:           NewHardwareDetector(ffmpegPath),
		idleTimeout:        defaultIdleTimeout,
		cleanupInterval:    defaultCleanupTick,
		maxSessions:        defaultMaxSessions,
		maxSessionsPerUser: defaultMaxSessionsPerUser,
		maxCacheBytes:      defaultMaxTranscodeCacheSize,
		encOpts:            DefaultEncodingOptions(),
		sessions:           make(map[string]*transcodeSession),
		reservedSlots:      make(map[string]int),
		stopCh:             make(chan struct{}),
	}
	for _, opt := range opts {
		opt(m)
	}

	m.wg.Add(1)
	go m.reaperLoop()
	return m
}

// Capabilities returns the cached hardware acceleration capabilities (or software-only when
// hardware acceleration/encoding is disabled in /Encoding/FfmpegOptions).
func (m *Manager) Capabilities(ctx context.Context) *HWCapabilities {
	if !m.EncodingOptionsSnapshot().isHardwareAllowed() {
		return SoftwareOnlyCapabilities()
	}
	return m.detector.Detect(ctx)
}

// HasSession reports whether a live session with the given key already exists.
func (m *Manager) HasSession(key string) bool {
	if m == nil {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	sess, ok := m.sessions[key]
	return ok && sess != nil && !sess.isDestroyed()
}

// ActiveSessionCount returns the number of currently tracked transcode sessions.
func (m *Manager) ActiveSessionCount() int {
	if m == nil {
		return 0
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.sessions)
}

func (m *Manager) pendingReservedCountLocked() int {
	if len(m.reservedSlots) == 0 {
		return 0
	}
	pending := 0
	for k, refs := range m.reservedSlots {
		if refs <= 0 {
			continue
		}
		if sess, ok := m.sessions[k]; ok && sess != nil && !sess.isDestroyed() {
			continue
		}
		pending++
	}
	return pending
}

// TotalActiveTranscodes returns the combined count of active HLS sessions, pending reserved session slots,
// and external stream transcodes (e.g. Audio / DirectStream).
func (m *Manager) TotalActiveTranscodes() int {
	if m == nil {
		return 0
	}
	m.mu.Lock()
	hlsCount := len(m.sessions) + m.pendingReservedCountLocked()
	m.mu.Unlock()
	ext := int(m.externalStreams.Load())
	if ext < 0 {
		ext = 0
	}
	return hlsCount + ext
}

// TryAcquireStreamSlot checks SimultaneousStreamLimit atomically across HLS sessions, in-flight session
// reservations, and external stream transcodes/deliveries.
// When existingSessionKey is non-empty (HLS), existing or already-reserved sessions pass without consuming
// an extra slot; brand-new session keys atomically reserve a slot until release() is called (or the session
// becomes active in m.sessions, where it is counted once).
// When existingSessionKey is empty (pipe transcodes / direct streams), an external slot is reserved until release() is called.
func (m *Manager) TryAcquireStreamSlot(limit int, existingSessionKey string) (release func(), allowed bool) {
	noop := func() {}
	if m == nil || limit <= 0 {
		return noop, true
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	ext := int(m.externalStreams.Load())
	if ext < 0 {
		ext = 0
	}

	if existingSessionKey != "" {
		if sess, ok := m.sessions[existingSessionKey]; ok && sess != nil && !sess.isDestroyed() {
			return noop, true
		}
		if m.reservedSlots == nil {
			m.reservedSlots = make(map[string]int)
		}
		if m.reservedSlots[existingSessionKey] > 0 {
			m.reservedSlots[existingSessionKey]++
			var once sync.Once
			return func() {
				once.Do(func() {
					m.mu.Lock()
					if m.reservedSlots[existingSessionKey] <= 1 {
						delete(m.reservedSlots, existingSessionKey)
					} else {
						m.reservedSlots[existingSessionKey]--
					}
					m.mu.Unlock()
				})
			}, true
		}
		if len(m.sessions)+ext+m.pendingReservedCountLocked() >= limit {
			return nil, false
		}
		m.reservedSlots[existingSessionKey] = 1
		var once sync.Once
		return func() {
			once.Do(func() {
				m.mu.Lock()
				if m.reservedSlots[existingSessionKey] <= 1 {
					delete(m.reservedSlots, existingSessionKey)
				} else {
					m.reservedSlots[existingSessionKey]--
				}
				m.mu.Unlock()
			})
		}, true
	}

	if len(m.sessions)+ext+m.pendingReservedCountLocked() >= limit {
		return nil, false
	}
	m.externalStreams.Add(1)
	var once sync.Once
	return func() {
		once.Do(func() {
			m.externalStreams.Add(-1)
		})
	}, true
}

// SessionKey derives a stable, user- and media-scoped session map key so that
// colliding PlaySessionIDs across users or media sources cannot share a session.
func SessionKey(p SessionParams) string {
	user := strings.TrimSpace(p.UserID)
	item := strings.TrimSpace(p.ItemID)
	ms := strings.TrimSpace(p.MediaSourceID)
	ps := strings.TrimSpace(p.PlaySessionID)
	dev := strings.TrimSpace(p.DeviceID)
	if ps != "" {
		return user + ":" + item + ":" + ms + ":" + ps
	}
	if dev != "" {
		return user + ":" + dev + ":" + item + ":" + ms
	}
	return user + ":" + item + ":" + ms
}

func validateSessionPaths(params SessionParams, requireInput bool) error {
	inputPath := strings.TrimSpace(params.InputPath)
	if requireInput && inputPath == "" {
		return errors.New("input path is empty")
	}
	if hasControlChars(params.InputPath) || hasControlChars(params.SubtitlePath) {
		return errors.New("invalid control character in media or subtitle path")
	}
	if isHTTPInput(inputPath) {
		if err := netutil.ValidateRemoteURL(inputPath, true); err != nil {
			return fmt.Errorf("invalid remote input url: %w", err)
		}
	}
	subPath := strings.TrimSpace(params.SubtitlePath)
	if isHTTPInput(subPath) {
		if err := netutil.ValidateRemoteURL(subPath, true); err != nil {
			return fmt.Errorf("invalid remote subtitle url: %w", err)
		}
	}
	return nil
}

// GetSegment returns the on-disk path of the requested HLS segment, starting or seeking
// the FFmpeg process as needed and waiting for the segment file to be atomically written.
func (m *Manager) GetSegment(ctx context.Context, params SessionParams, segIndex int) (string, error) {
	if segIndex < 0 {
		return "", errors.New("segment index must be non-negative")
	}
	params = m.applyEncodingOptionsToParams(params)
	params.normalize()
	if err := validateSessionPaths(params, true); err != nil {
		return "", err
	}

	sess, err := m.getOrCreateSession(params)
	if err != nil {
		return "", err
	}
	segPath, err := sess.getSegment(ctx, segIndex, func() *HWCapabilities {
		return m.Capabilities(ctx)
	})
	if err == nil && m.maxCacheBytes > 0 {
		m.EnforceCacheBytesLimit()
	}
	return segPath, err
}

// GetInitSegment ensures the fMP4 transcode session is running and waits for workDir/init.mp4 to be ready.
func (m *Manager) GetInitSegment(ctx context.Context, params SessionParams) (string, error) {
	params = m.applyEncodingOptionsToParams(params)
	params.normalize()
	if err := validateSessionPaths(params, true); err != nil {
		return "", err
	}

	sess, err := m.getOrCreateSession(params)
	if err != nil {
		return "", err
	}
	return sess.getInitSegment(ctx, func() *HWCapabilities {
		return m.Capabilities(ctx)
	})
}

func intPtrEqual(a, b *int) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}

func headersDiffer(a, b map[string]string) bool {
	if len(a) != len(b) {
		return true
	}
	for k, v := range a {
		if b[k] != v {
			return true
		}
	}
	return false
}

func sessionParamsDiffer(a, b SessionParams) bool {
	return strings.TrimSpace(a.UserID) != strings.TrimSpace(b.UserID) ||
		strings.TrimSpace(a.ItemID) != strings.TrimSpace(b.ItemID) ||
		strings.TrimSpace(a.MediaSourceID) != strings.TrimSpace(b.MediaSourceID) ||
		strings.TrimSpace(a.InputPath) != strings.TrimSpace(b.InputPath) ||
		a.VideoBitrate != b.VideoBitrate ||
		a.AudioBitrate != b.AudioBitrate ||
		a.AudioChannels != b.AudioChannels ||
		a.MaxWidth != b.MaxWidth ||
		a.MaxHeight != b.MaxHeight ||
		a.StartTimeTicks != b.StartTimeTicks ||
		a.IsHDR != b.IsHDR ||
		a.SubtitleRelativeIndex != b.SubtitleRelativeIndex ||
		a.SegmentDuration != b.SegmentDuration ||
		a.SoftwareCRF != b.SoftwareCRF ||
		a.SoftwarePreset != b.SoftwarePreset ||
		a.DisableToneMapping != b.DisableToneMapping ||
		a.ToneMappingAlgorithm != b.ToneMappingAlgorithm ||
		a.ToneMappingPeak != b.ToneMappingPeak ||
		a.AudioOnly != b.AudioOnly ||
		headersDiffer(a.InputHeaders, b.InputHeaders) ||
		!strings.EqualFold(strings.TrimSpace(a.SegmentContainer), strings.TrimSpace(b.SegmentContainer)) ||
		!strings.EqualFold(strings.TrimSpace(a.VideoCodec), strings.TrimSpace(b.VideoCodec)) ||
		!strings.EqualFold(strings.TrimSpace(a.AudioCodec), strings.TrimSpace(b.AudioCodec)) ||
		!intPtrEqual(a.AudioStreamIndex, b.AudioStreamIndex) ||
		!intPtrEqual(a.SubtitleStreamIndex, b.SubtitleStreamIndex) ||
		!strings.EqualFold(strings.TrimSpace(a.SubtitleMethod), strings.TrimSpace(b.SubtitleMethod)) ||
		!strings.EqualFold(strings.TrimSpace(a.SubtitleCodec), strings.TrimSpace(b.SubtitleCodec)) ||
		strings.TrimSpace(a.SubtitlePath) != strings.TrimSpace(b.SubtitlePath)
}

func sessionQuotaOwner(p SessionParams) string {
	if u := strings.TrimSpace(p.UserID); u != "" {
		return "user:" + u
	}
	if d := strings.TrimSpace(p.DeviceID); d != "" {
		return "device:" + d
	}
	return "__anonymous__"
}

func (m *Manager) getOrCreateSession(params SessionParams) (*transcodeSession, error) {
	key := SessionKey(params)

	for {
		m.mu.Lock()
		if m.closed {
			m.mu.Unlock()
			return nil, errors.New("transcode manager is closed")
		}
		if existing, ok := m.sessions[key]; ok {
			if !existing.isDestroyed() && !sessionParamsDiffer(existing.params, params) {
				existing.touch()
				m.mu.Unlock()
				return existing, nil
			}
			delete(m.sessions, key)
			m.mu.Unlock()
			existing.destroy()
			continue
		}

		// Enforce per-user (or per-device / __anonymous__ bucket when UserID is empty)
		// session quota first so no single principal can exhaust global slots.
		var evicted []*transcodeSession
		if m.maxSessionsPerUser > 0 {
			owner := sessionQuotaOwner(params)
			for {
				ownerCount := 0
				var oldestKey string
				var oldestSess *transcodeSession
				var oldestTime time.Time
				for k, s := range m.sessions {
					if sessionQuotaOwner(s.params) != owner {
						continue
					}
					ownerCount++
					t := s.lastAccessTime()
					if oldestSess == nil || t.Before(oldestTime) {
						oldestKey = k
						oldestSess = s
						oldestTime = t
					}
				}
				if ownerCount < m.maxSessionsPerUser || oldestSess == nil {
					break
				}
				delete(m.sessions, oldestKey)
				evicted = append(evicted, oldestSess)
			}
		}

		// Evict least-recently-used sessions if still at global capacity.
		if m.maxSessions > 0 && len(m.sessions) >= m.maxSessions {
			for len(m.sessions) >= m.maxSessions {
				var oldestKey string
				var oldestSess *transcodeSession
				var oldestTime time.Time
				for k, s := range m.sessions {
					t := s.lastAccessTime()
					if oldestSess == nil || t.Before(oldestTime) {
						oldestKey = k
						oldestSess = s
						oldestTime = t
					}
				}
				if oldestSess == nil {
					break
				}
				delete(m.sessions, oldestKey)
				evicted = append(evicted, oldestSess)
			}
		}

		baseDir := m.effectiveTranscodeBaseDir()
		if err := os.MkdirAll(baseDir, 0o700); err != nil {
			m.mu.Unlock()
			for _, ev := range evicted {
				ev.destroy()
			}
			return nil, fmt.Errorf("create transcode base dir: %w", err)
		}
		safeKey := sanitizePathSegment(key)
		if len(safeKey) > 80 {
			safeKey = safeKey[:80]
		}
		workDir, err := os.MkdirTemp(baseDir, safeKey+"-*")
		if err != nil {
			m.mu.Unlock()
			for _, ev := range evicted {
				ev.destroy()
			}
			return nil, fmt.Errorf("create transcode dir: %w", err)
		}

		ffmpegPath := "ffmpeg"
		if m.cfg != nil && m.cfg.FFmpegPath != "" {
			ffmpegPath = m.cfg.FFmpegPath
		}

		sessionCtx, sessionCancel := context.WithCancel(context.Background())
		sess := &transcodeSession{
			mgr:              m,
			key:              key,
			ffmpegPath:       ffmpegPath,
			params:           params,
			workDir:          workDir,
			sessionCtx:       sessionCtx,
			sessionCancel:    sessionCancel,
			startSeg:         -1,
			lastRequestedSeg: 0,
		}
		sess.lastAccess.Store(time.Now().UnixNano())
		m.sessions[key] = sess
		m.mu.Unlock()

		for _, ev := range evicted {
			ev.destroy()
		}
		return sess, nil
	}
}

// RegisterSession pre-registers a session without starting FFmpeg yet (e.g., on master.m3u8).
func (m *Manager) RegisterSession(params SessionParams) (*SessionSnapshot, error) {
	params = m.applyEncodingOptionsToParams(params)
	params.normalize()
	if err := validateSessionPaths(params, false); err != nil {
		return nil, err
	}
	sess, err := m.getOrCreateSession(params)
	if err != nil {
		return nil, err
	}
	snap := sess.snapshot()
	return &snap, nil
}

// StopSessionsScoped stops active transcoding sessions matching playSessionID or deviceID
// with strict caller authorization:
//   - If isAdmin is true, any session matching playSessionID or deviceID is stopped.
//   - If isAdmin is false, only sessions explicitly owned by userID (s.params.UserID != "" && s.params.UserID == userID)
//     are eligible (API-key / unowned sessions and other users' sessions are never stopped).
func (m *Manager) StopSessionsScoped(userID, playSessionID, deviceID string, isAdmin bool) int {
	userID = strings.TrimSpace(userID)
	playSessionID = strings.TrimSpace(playSessionID)
	deviceID = strings.TrimSpace(deviceID)
	if playSessionID == "" && deviceID == "" {
		return 0
	}
	if !isAdmin && userID == "" {
		return 0
	}

	var toStop []*transcodeSession
	m.mu.Lock()
	for key, sess := range m.sessions {
		if !isAdmin {
			owner := strings.TrimSpace(sess.params.UserID)
			if owner == "" || owner != userID {
				continue
			}
		}
		match := false
		if playSessionID != "" && (sess.params.PlaySessionID == playSessionID || key == playSessionID) {
			match = true
		} else if deviceID != "" && sess.params.DeviceID == deviceID {
			match = true
		}
		if !match {
			continue
		}
		toStop = append(toStop, sess)
		delete(m.sessions, key)
	}
	m.mu.Unlock()

	for _, sess := range toStop {
		sess.destroy()
	}
	return len(toStop)
}

// StopByDevice stops and cleans up all active transcoding sessions matching playSessionID or deviceID.
// Returns the number of sessions stopped.
func (m *Manager) StopByDevice(deviceID, playSessionID string) int {
	return m.StopByDeviceForUser(deviceID, playSessionID, "", true)
}

// StopByDeviceForUser stops and cleans up active transcoding sessions matching playSessionID or deviceID
// with strict caller authorization by delegating to StopSessionsScoped.
// Returns the number of sessions stopped.
func (m *Manager) StopByDeviceForUser(deviceID, playSessionID, callerUserID string, isAdmin bool) int {
	return m.StopSessionsScoped(callerUserID, playSessionID, deviceID, isAdmin)
}

// ActiveSessions returns snapshots of all currently tracked transcoding sessions.
func (m *Manager) ActiveSessions() []SessionSnapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]SessionSnapshot, 0, len(m.sessions))
	for _, s := range m.sessions {
		out = append(out, s.snapshot())
	}
	return out
}

// ActiveWorkDirs returns the working directories of all currently active transcoding sessions.
func (m *Manager) ActiveWorkDirs() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, 0, len(m.sessions))
	for _, s := range m.sessions {
		if s.workDir != "" {
			out = append(out, s.workDir)
		}
	}
	return out
}

// Close gracefully terminates all active FFmpeg processes and stops the reaper goroutine.
func (m *Manager) Close() {
	m.stopOnce.Do(func() {
		close(m.stopCh)
	})
	m.wg.Wait()

	m.mu.Lock()
	m.closed = true
	all := make([]*transcodeSession, 0, len(m.sessions))
	for k, s := range m.sessions {
		all = append(all, s)
		delete(m.sessions, k)
	}
	m.mu.Unlock()

	for _, s := range all {
		s.destroy()
	}
}

func (m *Manager) reaperLoop() {
	defer m.wg.Done()
	ticker := time.NewTicker(m.cleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-m.stopCh:
			return
		case <-ticker.C:
			m.reapIdleSessions(time.Now())
			m.EnforceCacheBytesLimit()
		}
	}
}

// EnforceCacheBytesLimit enforces maxCacheBytes across all active transcode session directories,
// pruning already-played old segments first and evicting least-recently-used sessions if still over limit.
func (m *Manager) EnforceCacheBytesLimit() {
	if m == nil || m.maxCacheBytes <= 0 {
		return
	}
	type segCandidate struct {
		path    string
		size    int64
		segIdx  int
		modTime time.Time
	}
	type sessUsage struct {
		key        string
		sess       *transcodeSession
		lastAccess time.Time
		bytes      int64
	}

	m.mu.Lock()
	snapshotList := make([]sessUsage, 0, len(m.sessions))
	for k, s := range m.sessions {
		snapshotList = append(snapshotList, sessUsage{
			key:        k,
			sess:       s,
			lastAccess: s.lastAccessTime(),
		})
	}
	m.mu.Unlock()
	if len(snapshotList) == 0 {
		return
	}

	var (
		totalBytes int64
		oldSegs    []segCandidate
	)
	for i := range snapshotList {
		su := &snapshotList[i]
		su.sess.mu.Lock()
		workDir := su.sess.workDir
		lastReq := su.sess.lastRequestedSeg
		ext := su.sess.params.SegmentContainer
		su.sess.mu.Unlock()
		if workDir == "" {
			continue
		}
		suffix := "." + ext
		_ = filepath.WalkDir(workDir, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			info, statErr := d.Info()
			if statErr != nil || info.Mode()&os.ModeSymlink != 0 {
				return nil
			}
			sz := info.Size()
			totalBytes += sz
			su.bytes += sz

			name := d.Name()
			if strings.HasPrefix(name, "seg_") && strings.HasSuffix(name, suffix) {
				numStr := strings.TrimSuffix(strings.TrimPrefix(name, "seg_"), suffix)
				if idx, convErr := strconv.Atoi(numStr); convErr == nil && idx < lastReq-2 {
					oldSegs = append(oldSegs, segCandidate{
						path:    p,
						size:    sz,
						segIdx:  idx,
						modTime: info.ModTime(),
					})
				}
			}
			return nil
		})
	}

	if totalBytes <= m.maxCacheBytes {
		return
	}

	// Stage 1: delete already-played historical segments (idx < lastRequestedSeg - 2) oldest first.
	sort.Slice(oldSegs, func(i, j int) bool {
		if !oldSegs[i].modTime.Equal(oldSegs[j].modTime) {
			return oldSegs[i].modTime.Before(oldSegs[j].modTime)
		}
		return oldSegs[i].segIdx < oldSegs[j].segIdx
	})
	for _, seg := range oldSegs {
		if totalBytes <= m.maxCacheBytes {
			return
		}
		if err := os.Remove(seg.path); err == nil {
			totalBytes -= seg.size
		}
	}
	if totalBytes <= m.maxCacheBytes {
		return
	}

	// Stage 2: evict least-recently-used sessions (keeping the single most recently accessed session alive).
	sort.Slice(snapshotList, func(i, j int) bool {
		return snapshotList[i].lastAccess.Before(snapshotList[j].lastAccess)
	})
	var toDestroy []*transcodeSession
	m.mu.Lock()
	for i := 0; i < len(snapshotList)-1 && totalBytes > m.maxCacheBytes; i++ {
		cand := snapshotList[i]
		if cur, ok := m.sessions[cand.key]; ok && cur == cand.sess {
			delete(m.sessions, cand.key)
			toDestroy = append(toDestroy, cur)
			totalBytes -= cand.bytes
		}
	}
	m.mu.Unlock()
	for _, s := range toDestroy {
		s.destroy()
	}
}

func (m *Manager) reapIdleSessions(now time.Time) int {
	var expired []*transcodeSession
	m.mu.Lock()
	for k, s := range m.sessions {
		if now.Sub(s.lastAccessTime()) > m.idleTimeout {
			expired = append(expired, s)
			delete(m.sessions, k)
		}
	}
	m.mu.Unlock()

	for _, s := range expired {
		s.destroy()
	}
	return len(expired)
}

type transcodeSession struct {
	mgr                *Manager
	key                string
	ffmpegPath         string
	params             SessionParams
	workDir            string
	segmentWaitTimeout time.Duration
	sessionCtx         context.Context
	sessionCancel      context.CancelFunc

	mu               sync.Mutex
	cmd              *exec.Cmd
	inputProxy       *LoopbackStreamProxy
	cancel           context.CancelFunc
	doneCh           chan struct{}
	lastErr          error
	stderrBuf        *safeRingBuffer
	startSeg         int
	lastRequestedSeg int
	throttled        bool
	procGen          int
	activeInitGen    string
	running          bool
	destroyed        bool
	usingHW          bool
	forceSoftware    bool
	lastAccess       atomic.Int64
}

func (s *transcodeSession) isDestroyed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.destroyed
}

func (s *transcodeSession) IsThrottled() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.throttled
}

func (s *transcodeSession) initSegmentPath() string {
	return filepath.Join(s.workDir, "init.mp4")
}

func isMP4InitComplete(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil || fi.IsDir() || fi.Size() < 16 {
		return false
	}
	fileSize := fi.Size()
	var (
		offset  int64
		hasFtyp bool
		hasMoov bool
		hdr     [16]byte
	)
	for offset+8 <= fileSize {
		if _, err := f.ReadAt(hdr[:8], offset); err != nil {
			return false
		}
		boxSize := int64(binary.BigEndian.Uint32(hdr[0:4]))
		boxType := string(hdr[4:8])
		if boxSize == 1 {
			if offset+16 > fileSize {
				return false
			}
			if _, err := f.ReadAt(hdr[8:16], offset+8); err != nil {
				return false
			}
			boxSize = int64(binary.BigEndian.Uint64(hdr[8:16]))
		} else if boxSize == 0 {
			return false
		}
		if boxSize < 8 || offset+boxSize > fileSize {
			return false
		}
		switch boxType {
		case "ftyp", "styp":
			hasFtyp = true
		case "moov":
			hasMoov = true
		}
		offset += boxSize
		if hasFtyp && hasMoov {
			return true
		}
	}
	return false
}

func (s *transcodeSession) tryPromoteStagedInit() {
	s.mu.Lock()
	genName := s.activeInitGen
	workDir := s.workDir
	running := s.running
	startSeg := s.startSeg
	s.mu.Unlock()

	if genName == "" || workDir == "" {
		return
	}
	genPath := filepath.Join(workDir, genName)
	if !fileReady(genPath) {
		return
	}
	firstSegReady := startSeg >= 0 && fileReady(s.segmentPath(startSeg))
	if !running || firstSegReady || isMP4InitComplete(genPath) {
		_ = os.Rename(genPath, filepath.Join(workDir, "init.mp4"))
	}
}

func (s *transcodeSession) touch() {
	s.lastAccess.Store(time.Now().UnixNano())
}

func (s *transcodeSession) lastAccessTime() time.Time {
	ns := s.lastAccess.Load()
	if ns == 0 {
		return time.Time{}
	}
	return time.Unix(0, ns)
}

func (s *transcodeSession) snapshot() SessionSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	return SessionSnapshot{
		Key:           s.key,
		PlaySessionID: s.params.PlaySessionID,
		DeviceID:      s.params.DeviceID,
		ItemID:        s.params.ItemID,
		StartSegment:  s.startSeg,
		Running:       s.running,
		LastAccess:    s.lastAccessTime(),
		WorkDir:       s.workDir,
	}
}

func (s *transcodeSession) segmentPath(segIndex int) string {
	return filepath.Join(s.workDir, fmt.Sprintf("seg_%d.%s", segIndex, s.params.SegmentContainer))
}

// highestReadySegmentLocked scans contiguous ready segments starting from s.startSeg
// and stops immediately at the first missing/unready segment gap (H-10).
func (s *transcodeSession) highestReadySegmentLocked() int {
	if s.startSeg < 0 {
		return -1
	}
	cur := s.startSeg
	for {
		if !fileReady(s.segmentPath(cur)) {
			return cur - 1
		}
		cur++
	}
}

func (s *transcodeSession) onSegmentRequested(segIndex int) {
	var encOpts EncodingOptions
	if s.mgr != nil {
		encOpts = s.mgr.EncodingOptionsSnapshot()
	} else {
		encOpts = DefaultEncodingOptions()
	}
	s.touch()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastRequestedSeg = segIndex
	if s.throttled && s.running && s.cmd != nil {
		segDur := s.params.SegmentDuration
		if segDur <= 0 {
			segDur = DefaultSegmentDuration
		}
		head := s.highestReadySegmentLocked()
		leadSec := (head - segIndex) * segDur
		resumeThresh := encOpts.ThrottleBufferSize - encOpts.ThrottleHysteresis
		if resumeThresh < 0 {
			resumeThresh = 0
		}
		if !encOpts.EnableThrottling || segIndex < s.startSeg || segIndex > head || leadSec <= resumeThresh {
			_ = resumeProcess(s.cmd)
			s.throttled = false
		}
	}
}

func (s *transcodeSession) evaluateThrottle(encOpts EncodingOptions) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.destroyed || !s.running || s.cmd == nil {
		return
	}
	if !encOpts.EnableThrottling || encOpts.ThrottleBufferSize <= 0 {
		if s.throttled {
			_ = resumeProcess(s.cmd)
			s.throttled = false
		}
		return
	}
	segDur := s.params.SegmentDuration
	if segDur <= 0 {
		segDur = DefaultSegmentDuration
	}
	head := s.highestReadySegmentLocked()
	if head < s.startSeg {
		return
	}
	leadSec := (head - s.lastRequestedSeg) * segDur
	if !s.throttled {
		if leadSec > encOpts.ThrottleBufferSize {
			if suspendProcess(s.cmd) == nil {
				s.throttled = true
			}
		}
	} else {
		resumeThresh := encOpts.ThrottleBufferSize - encOpts.ThrottleHysteresis
		if resumeThresh < 0 {
			resumeThresh = 0
		}
		if leadSec <= resumeThresh {
			_ = resumeProcess(s.cmd)
			s.throttled = false
		}
	}
}

func (s *transcodeSession) throttleMonitorLoop(doneCh <-chan struct{}) {
	ticker := time.NewTicker(150 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-doneCh:
			return
		case <-ticker.C:
			s.tryPromoteStagedInit()
			if s.mgr != nil {
				s.evaluateThrottle(s.mgr.EncodingOptionsSnapshot())
			}
		}
	}
}

func (s *transcodeSession) getSegment(ctx context.Context, segIndex int, getCaps func() *HWCapabilities) (string, error) {
	if s.isDestroyed() {
		return "", ErrSessionClosed
	}
	s.onSegmentRequested(segIndex)
	targetPath := s.segmentPath(segIndex)

	// Fast path: segment already written on disk (atomic via -hls_flags temp_file)
	if fileReady(targetPath) {
		return targetPath, nil
	}

	var (
		caps       *HWCapabilities
		capsLoaded bool
	)
	s.mu.Lock()
	for {
		if s.destroyed {
			s.mu.Unlock()
			return "", ErrSessionClosed
		}
		// Re-check under lock (including after stopProcessLocked releases/re-acquires s.mu)
		if fileReady(targetPath) {
			s.mu.Unlock()
			return targetPath, nil
		}

		needRestart := !s.running
		if !needRestart {
			head := s.highestReadySegmentLocked()
			if head < s.startSeg {
				head = s.startSeg
			}
			// Seek detected: requested segment is before current start or far ahead of current head
			if segIndex < s.startSeg || segIndex > head+defaultSeekThreshold {
				needRestart = true
			}
		}

		if !needRestart {
			break
		}
		if s.running || s.cmd != nil || s.doneCh != nil {
			s.stopProcessLocked()
			// Re-evaluate after re-acquiring lock so two goroutines never spawn dual FFmpeg processes
			// and destroy() during stopProcessLocked is honored immediately.
			continue
		}
		if !capsLoaded {
			s.mu.Unlock()
			if getCaps != nil {
				caps = getCaps()
			}
			capsLoaded = true
			s.mu.Lock()
			continue
		}
		if err := s.startProcessUnlockRelock(ctx, segIndex, caps); err != nil {
			if errors.Is(err, errStartSuperseded) {
				continue
			}
			s.mu.Unlock()
			return "", err
		}
		break
	}
	if s.destroyed {
		s.mu.Unlock()
		return "", ErrSessionClosed
	}
	doneCh := s.doneCh
	s.mu.Unlock()
	if doneCh == nil {
		return "", ErrSessionClosed
	}

	return s.waitForFile(ctx, targetPath, segIndex, fmt.Sprintf("segment %d", segIndex), doneCh)
}

func (s *transcodeSession) getInitSegment(ctx context.Context, getCaps func() *HWCapabilities) (string, error) {
	if s.isDestroyed() {
		return "", ErrSessionClosed
	}
	s.touch()
	targetPath := s.initSegmentPath()

	if fileReady(targetPath) {
		return targetPath, nil
	}

	var (
		caps       *HWCapabilities
		capsLoaded bool
	)
	s.mu.Lock()
	for {
		if s.destroyed {
			s.mu.Unlock()
			return "", ErrSessionClosed
		}
		if fileReady(targetPath) {
			s.mu.Unlock()
			return targetPath, nil
		}
		if !s.running {
			if s.cmd != nil || s.doneCh != nil {
				s.stopProcessLocked()
				continue
			}
			if !capsLoaded {
				s.mu.Unlock()
				if getCaps != nil {
					caps = getCaps()
				}
				capsLoaded = true
				s.mu.Lock()
				continue
			}
			if err := s.startProcessUnlockRelock(ctx, 0, caps); err != nil {
				if errors.Is(err, errStartSuperseded) {
					continue
				}
				s.mu.Unlock()
				return "", err
			}
		}
		break
	}
	if s.destroyed {
		s.mu.Unlock()
		return "", ErrSessionClosed
	}
	doneCh := s.doneCh
	s.mu.Unlock()
	if doneCh == nil {
		return "", ErrSessionClosed
	}

	return s.waitForFile(ctx, targetPath, 0, "init.mp4", doneCh)
}

func (s *transcodeSession) waitForFile(ctx context.Context, targetPath string, restartSeg int, label string, doneCh chan struct{}) (string, error) {
	ticker := time.NewTicker(defaultPollInterval)
	defer ticker.Stop()

	waitTimeout := s.segmentWaitTimeout
	if waitTimeout <= 0 {
		waitTimeout = defaultSegmentWaitTimeout
	}
	waitTimer := time.NewTimer(waitTimeout)
	defer waitTimer.Stop()

	retriedSoftware := false
	for {
		if label == "init.mp4" {
			s.tryPromoteStagedInit()
		}
		if fileReady(targetPath) {
			s.touch()
			return targetPath, nil
		}
		if doneCh == nil || s.isDestroyed() {
			return "", ErrSessionClosed
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-waitTimer.C:
			return "", fmt.Errorf("timed out waiting for %s after %s", label, waitTimeout)
		case <-doneCh:
			if label == "init.mp4" {
				s.tryPromoteStagedInit()
			}
			// FFmpeg exited; check one final time if the file finished writing right before exit
			if fileReady(targetPath) {
				s.touch()
				return targetPath, nil
			}
			s.mu.Lock()
			if s.destroyed {
				s.mu.Unlock()
				return "", ErrSessionClosed
			}
			if s.doneCh != doneCh {
				// Another goroutine already restarted FFmpeg (e.g., HW->SW fallback or seek).
				if s.doneCh == nil {
					s.mu.Unlock()
					return "", ErrSessionClosed
				}
				if label != "init.mp4" && restartSeg < s.startSeg {
					restartedAt := s.startSeg
					s.mu.Unlock()
					return "", fmt.Errorf("transcode session restarted at segment %d before producing %s", restartedAt, label)
				}
				doneCh = s.doneCh
				s.mu.Unlock()
				continue
			}
			var errMsg string
			if s.stderrBuf != nil {
				errMsg = strings.TrimSpace(s.stderrBuf.String())
			}
			procErr := s.lastErr
			wasHW := s.usingHW
			if procErr != nil && wasHW && !retriedSoftware {
				retriedSoftware = true
				s.forceSoftware = true
				s.stopProcessLocked()
				if s.destroyed {
					s.mu.Unlock()
					return "", ErrSessionClosed
				}
				if s.running && s.doneCh != nil && s.doneCh != doneCh {
					doneCh = s.doneCh
					s.mu.Unlock()
					continue
				}
				if err := s.startProcessUnlockRelock(ctx, restartSeg, SoftwareOnlyCapabilities()); err != nil {
					if errors.Is(err, errStartSuperseded) {
						if s.destroyed || s.doneCh == nil {
							s.mu.Unlock()
							return "", ErrSessionClosed
						}
						doneCh = s.doneCh
						s.mu.Unlock()
						continue
					}
					s.mu.Unlock()
					return "", err
				}
				doneCh = s.doneCh
				s.mu.Unlock()
				continue
			}
			s.mu.Unlock()
			if procErr != nil {
				return "", fmt.Errorf("ffmpeg exited before producing %s: %w (%s)", label, procErr, errMsg)
			}
			return "", fmt.Errorf("%s not produced by ffmpeg (%s)", label, errMsg)
		case <-ticker.C:
			if s.isDestroyed() {
				return "", ErrSessionClosed
			}
			s.touch()
		}
	}
}

func isHardwareEncoder(enc string) bool {
	if enc == "" || strings.EqualFold(enc, "copy") {
		return false
	}
	return !strings.HasPrefix(strings.ToLower(enc), "libx")
}

func safeBurnSubExt(subPath, subCodec string) string {
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(strings.TrimSpace(subPath)), "."))
	switch ext {
	case "srt", "ass", "ssa", "vtt":
		return "." + ext
	}
	switch strings.ToLower(strings.TrimSpace(subCodec)) {
	case "ass":
		return ".ass"
	case "ssa":
		return ".ssa"
	case "vtt", "webvtt":
		return ".vtt"
	default:
		return ".srt"
	}
}

func safeMediaExt(inputPath string) string {
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(strings.TrimSpace(inputPath)), "."))
	if ext != "" && len(ext) <= 8 {
		valid := true
		for _, r := range ext {
			if !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')) {
				valid = false
				break
			}
		}
		if valid {
			return "." + ext
		}
	}
	return ".mkv"
}

func needsBurnInStaging(p string) bool {
	return strings.ContainsAny(p, `'"[]():,;=\ `)
}

func copyFileAtomic(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		_ = os.Remove(dst)
		return err
	}
	return out.Close()
}

var errStartSuperseded = errors.New("transcode start superseded")

func downloadRemoteSubtitleAtomic(rawURL, dst string, client *http.Client) error {
	return downloadRemoteSubtitleAtomicCtx(context.Background(), rawURL, dst, client)
}

func downloadRemoteSubtitleAtomicCtx(parentCtx context.Context, rawURL, dst string, client *http.Client) error {
	if parentCtx == nil {
		parentCtx = context.Background()
	}
	if client == nil {
		if err := netutil.ValidateRemoteURL(rawURL, true); err != nil {
			return err
		}
		client = netutil.NewSafeHTTPClient(remoteSubtitleDownloadTimeout, true)
		defer client.CloseIdleConnections()
	}
	ctx, cancel := context.WithTimeout(parentCtx, remoteSubtitleDownloadTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", defaultHTTPUserAgent)
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("remote subtitle HTTP status %d", resp.StatusCode)
	}

	tmpFile, err := os.CreateTemp(filepath.Dir(dst), ".burn-sub-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmpFile.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()

	written, err := io.Copy(tmpFile, io.LimitReader(resp.Body, maxRemoteSubtitleBytes+1))
	if err != nil {
		_ = tmpFile.Close()
		return err
	}
	if err := tmpFile.Close(); err != nil {
		return err
	}
	if written <= 0 {
		return errors.New("remote subtitle response is empty")
	}
	if written > maxRemoteSubtitleBytes {
		return fmt.Errorf("remote subtitle exceeds %d bytes", maxRemoteSubtitleBytes)
	}
	if err := os.Rename(tmpPath, dst); err != nil {
		return err
	}
	cleanup = false
	return nil
}

func extractRemoteEmbeddedSubtitle(ffmpegPath, mediaURL string, headers map[string]string, streamIndex int, subCodec, dst string) error {
	return extractRemoteEmbeddedSubtitleCtx(context.Background(), ffmpegPath, mediaURL, headers, streamIndex, subCodec, dst)
}

func extractRemoteEmbeddedSubtitleCtx(parentCtx context.Context, ffmpegPath, mediaURL string, headers map[string]string, streamIndex int, subCodec, dst string) error {
	if parentCtx == nil {
		parentCtx = context.Background()
	}
	if strings.TrimSpace(ffmpegPath) == "" {
		ffmpegPath = "ffmpeg"
	}
	if err := netutil.ValidateRemoteURL(mediaURL, true); err != nil {
		return err
	}
	ext := normalizeSubtitleExt(subCodec)
	tmpFile, err := os.CreateTemp(filepath.Dir(dst), ".burn-emb-*."+ext)
	if err != nil {
		return err
	}
	tmpPath := tmpFile.Name()
	_ = tmpFile.Close()
	defer os.Remove(tmpPath)

	ffInput, closeProxy, err := startExtractorInputProxy(mediaURL, headers)
	if err != nil {
		return err
	}
	defer closeProxy()

	ctx, cancel := context.WithTimeout(parentCtx, defaultExtractTimeout)
	defer cancel()
	args := netutil.FilterFFmpegArgs(ffmpegPath, BuildSubtitleExtractArgs(ffInput, streamIndex, ext, tmpPath))
	cmd := exec.CommandContext(ctx, ffmpegPath, args...)
	if err := cmd.Run(); err != nil {
		return err
	}
	if !fileReady(tmpPath) {
		return errors.New("extracted remote embedded subtitle is empty")
	}
	return os.Rename(tmpPath, dst)
}

// stageBurnInFiles stages external subtitle files (copy or safe HTTP download) or embedded-subtitle
// media files into workDir under safe alphanumeric filenames so FFmpeg's subtitles= filter never
// receives raw HTTP(S) URLs or chokes on special path characters.
func stageBurnInFiles(params SessionParams, workDir string) SessionParams {
	return stageBurnInFilesWithDeps(params, workDir, "ffmpeg", nil)
}

func stageBurnInFilesWithDeps(params SessionParams, workDir, ffmpegPath string, httpClient *http.Client) SessionParams {
	return stageBurnInFilesWithContext(context.Background(), params, workDir, ffmpegPath, httpClient)
}

func stageBurnInFilesWithContext(ctx context.Context, params SessionParams, workDir, ffmpegPath string, httpClient *http.Client) SessionParams {
	if !strings.EqualFold(strings.TrimSpace(params.SubtitleMethod), "Encode") {
		return params
	}
	subPath := strings.TrimSpace(params.SubtitlePath)
	isBitmap := isBitmapSubtitleCodec(params.SubtitleCodec) &&
		subPath == "" &&
		params.SubtitleStreamIndex != nil && *params.SubtitleStreamIndex >= 0
	if isBitmap {
		return params
	}

	if subPath != "" {
		if isHTTPInput(subPath) {
			dst := filepath.Join(workDir, "burn_sub"+safeBurnSubExt(subPath, params.SubtitleCodec))
			if fileReady(dst) {
				params.SubtitlePath = dst
				return params
			}
			if err := downloadRemoteSubtitleAtomicCtx(ctx, subPath, dst, httpClient); err == nil {
				params.SubtitlePath = dst
				return params
			}
			// Never pass raw HTTP(S) subtitle URL into FFmpeg's subtitles= filter.
			params.SubtitlePath = ""
			params.SubtitleMethod = ""
			return params
		}
		absSub := subPath
		if a, err := filepath.Abs(subPath); err == nil {
			absSub = a
		}
		if fi, err := os.Stat(absSub); err == nil && !fi.IsDir() {
			dst := filepath.Join(workDir, "burn_sub"+safeBurnSubExt(absSub, params.SubtitleCodec))
			if err := copyFileAtomic(absSub, dst); err == nil {
				params.SubtitlePath = dst
			}
		}
		return params
	}

	if params.SubtitleStreamIndex != nil && *params.SubtitleStreamIndex >= 0 {
		inputPath := strings.TrimSpace(params.InputPath)
		if isHTTPInput(inputPath) {
			dst := filepath.Join(workDir, "burn_sub"+safeBurnSubExt("", params.SubtitleCodec))
			if fileReady(dst) {
				params.SubtitlePath = dst
				return params
			}
			if err := extractRemoteEmbeddedSubtitleCtx(ctx, ffmpegPath, inputPath, params.InputHeaders, *params.SubtitleStreamIndex, params.SubtitleCodec, dst); err == nil {
				params.SubtitlePath = dst
				return params
			}
			// Never pass raw HTTP(S) media URL into FFmpeg's subtitles= filter.
			params.SubtitleMethod = ""
			return params
		}
		if inputPath != "" && needsBurnInStaging(inputPath) {
			absIn := inputPath
			if a, err := filepath.Abs(inputPath); err == nil {
				absIn = a
			}
			if fi, err := os.Stat(absIn); err == nil && !fi.IsDir() {
				linkPath := filepath.Join(workDir, "burn_input"+safeMediaExt(absIn))
				_ = os.Remove(linkPath)
				if err := os.Symlink(absIn, linkPath); err == nil {
					params.InputPath = linkPath
				} else if err := os.Link(absIn, linkPath); err == nil {
					params.InputPath = linkPath
				}
			}
		}
	}
	return params
}

// startProcessUnlockRelock stages burn-in subtitles and starts any required LoopbackStreamProxy
// OUTSIDE s.mu so blocking network/disk I/O never stalls unrelated session operations, then
// re-acquires s.mu and verifies the session was neither destroyed nor superseded.
// Caller MUST hold s.mu on entry; s.mu is always held on return.
func (s *transcodeSession) startProcessUnlockRelock(ctx context.Context, startSeg int, caps *HWCapabilities) error {
	if s.destroyed {
		return ErrSessionClosed
	}
	expectedGen := s.procGen
	params := s.params
	workDir := s.workDir
	ffmpegPath := s.ffmpegPath
	sessCtx := s.sessionCtx
	s.mu.Unlock()

	if ctx == nil {
		ctx = context.Background()
	}
	stageCtx, stageCancel := context.WithCancel(ctx)
	var stopSessWatch func() bool
	if sessCtx != nil {
		stopSessWatch = context.AfterFunc(sessCtx, stageCancel)
	}
	defer func() {
		if stopSessWatch != nil {
			stopSessWatch()
		}
		stageCancel()
	}()

	if err := os.MkdirAll(workDir, 0o755); err != nil {
		s.mu.Lock()
		if s.destroyed {
			return ErrSessionClosed
		}
		return err
	}

	effectiveParams := stageBurnInFilesWithContext(stageCtx, params, workDir, ffmpegPath, nil)
	if err := stageCtx.Err(); err != nil {
		s.mu.Lock()
		if s.destroyed {
			return ErrSessionClosed
		}
		return err
	}

	// Route ALL remote HTTP(S) inputs (including .m3u8 HLS manifests) through LoopbackStreamProxy (H-06).
	var streamProxy *LoopbackStreamProxy
	rawIn := strings.TrimSpace(effectiveParams.InputPath)
	if isHTTPInput(rawIn) {
		proxy, proxyErr := StartLoopbackStreamProxy(LoopbackProxyOptions{
			TargetURL:       rawIn,
			Headers:         effectiveParams.InputHeaders,
			AllowPrivateLAN: true,
		})
		if proxyErr != nil {
			s.mu.Lock()
			if s.destroyed {
				return ErrSessionClosed
			}
			return fmt.Errorf("start loopback stream proxy: %w", proxyErr)
		}
		streamProxy = proxy
		effectiveParams.InputPath = proxy.URL()
	}

	s.mu.Lock()
	if s.destroyed {
		if streamProxy != nil {
			_ = streamProxy.Close()
		}
		return ErrSessionClosed
	}
	if s.procGen != expectedGen || s.running || s.cmd != nil || s.doneCh != nil {
		if streamProxy != nil {
			_ = streamProxy.Close()
		}
		return errStartSuperseded
	}
	return s.startProcessWithStagedLocked(startSeg, caps, effectiveParams, streamProxy)
}

func (s *transcodeSession) startProcessLocked(startSeg int, caps *HWCapabilities) error {
	if s.destroyed {
		return ErrSessionClosed
	}
	if err := os.MkdirAll(s.workDir, 0o755); err != nil {
		return err
	}
	effectiveParams := stageBurnInFilesWithDeps(s.params, s.workDir, s.ffmpegPath, nil)
	var streamProxy *LoopbackStreamProxy
	rawIn := strings.TrimSpace(effectiveParams.InputPath)
	if isHTTPInput(rawIn) {
		proxy, proxyErr := StartLoopbackStreamProxy(LoopbackProxyOptions{
			TargetURL:       rawIn,
			Headers:         effectiveParams.InputHeaders,
			AllowPrivateLAN: true,
		})
		if proxyErr != nil {
			return fmt.Errorf("start loopback stream proxy: %w", proxyErr)
		}
		streamProxy = proxy
		effectiveParams.InputPath = proxy.URL()
	}
	return s.startProcessWithStagedLocked(startSeg, caps, effectiveParams, streamProxy)
}

func (s *transcodeSession) startProcessWithStagedLocked(startSeg int, caps *HWCapabilities, effectiveParams SessionParams, streamProxy *LoopbackStreamProxy) error {
	if s.destroyed {
		if streamProxy != nil {
			_ = streamProxy.Close()
		}
		return ErrSessionClosed
	}
	s.procGen++
	ext := s.params.SegmentContainer
	suffix := "." + ext

	// Clean any stale .tmp files, old staged init_gen_*.mp4 files, and stale residual segments idx >= startSeg (H-10).
	if entries, err := os.ReadDir(s.workDir); err == nil {
		for _, e := range entries {
			name := e.Name()
			if strings.HasSuffix(name, ".tmp") || strings.HasPrefix(name, "init_gen_") {
				_ = os.Remove(filepath.Join(s.workDir, name))
				continue
			}
			if strings.HasPrefix(name, "seg_") && strings.HasSuffix(name, suffix) {
				numStr := strings.TrimSuffix(strings.TrimPrefix(name, "seg_"), suffix)
				if idx, convErr := strconv.Atoi(numStr); convErr == nil && idx >= startSeg {
					_ = os.Remove(filepath.Join(s.workDir, name))
				}
			}
		}
	}

	effectiveCaps := caps
	if s.forceSoftware {
		effectiveCaps = SoftwareOnlyCapabilities()
	}
	vCodec := s.params.VideoCodec
	if strings.EqualFold(vCodec, "copy") {
		vCodec = "h264"
	}
	usingHW := !s.params.AudioOnly && !strings.EqualFold(s.params.VideoCodec, "none") &&
		(!strings.EqualFold(s.params.VideoCodec, "copy") || strings.EqualFold(strings.TrimSpace(s.params.SubtitleMethod), "Encode")) &&
		isHardwareEncoder(effectiveCaps.SelectVideoEncoder(vCodec))

	// Avoid in-place O_TRUNC overwriting an active init.mp4 being read concurrently by clients (H-10).
	s.activeInitGen = ""
	if ext == "mp4" || ext == "m4s" {
		s.activeInitGen = fmt.Sprintf("init_gen_%d.mp4", s.procGen)
		effectiveParams.FMP4InitFilename = s.activeInitGen
	}

	args := netutil.FilterFFmpegArgs(s.ffmpegPath, BuildFFmpegArgs(effectiveParams, startSeg, s.workDir, effectiveCaps))
	procCtx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(procCtx, s.ffmpegPath, args...)

	stderrBuf := newSafeRingBuffer(maxStderrBytes)
	cmd.Stderr = stderrBuf

	if err := cmd.Start(); err != nil {
		if streamProxy != nil {
			_ = streamProxy.Close()
		}
		cancel()
		return fmt.Errorf("start ffmpeg: %w", err)
	}

	doneCh := make(chan struct{})
	s.cmd = cmd
	s.inputProxy = streamProxy
	s.cancel = cancel
	s.doneCh = doneCh
	s.stderrBuf = stderrBuf
	s.startSeg = startSeg
	s.throttled = false
	s.running = true
	s.usingHW = usingHW
	s.lastErr = nil
	s.touch()

	go s.throttleMonitorLoop(doneCh)

	go func() {
		err := cmd.Wait()
		if streamProxy != nil {
			_ = streamProxy.Close()
		}
		s.mu.Lock()
		if s.cmd == cmd {
			s.running = false
			s.throttled = false
			s.lastErr = err
		}
		s.mu.Unlock()
		s.tryPromoteStagedInit()
		close(doneCh)
	}()

	return nil
}

func (s *transcodeSession) stopProcessLocked() {
	if s.throttled && s.cmd != nil {
		_ = resumeProcess(s.cmd)
		s.throttled = false
	}
	if s.inputProxy != nil {
		_ = s.inputProxy.Close()
		s.inputProxy = nil
	}
	if s.cancel != nil {
		s.cancel()
	}
	if s.cmd != nil && s.cmd.Process != nil {
		_ = s.cmd.Process.Kill()
	}
	doneCh := s.doneCh
	s.cmd = nil
	s.cancel = nil
	s.doneCh = nil
	s.running = false
	if doneCh != nil {
		// Release lock briefly while waiting for Wait() goroutine to finish
		s.mu.Unlock()
		select {
		case <-doneCh:
		case <-time.After(2 * time.Second):
		}
		s.mu.Lock()
	}
}

func (s *transcodeSession) destroy() {
	s.mu.Lock()
	s.destroyed = true
	sessCancel := s.sessionCancel
	if sessCancel != nil {
		sessCancel()
	}
	s.stopProcessLocked()
	workDir := s.workDir
	s.mu.Unlock()
	if workDir != "" {
		_ = os.RemoveAll(workDir)
	}
}

func isHTTPInput(inputPath string) bool {
	lower := strings.ToLower(strings.TrimSpace(inputPath))
	return strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://")
}

func isBitmapSubtitleCodec(codec string) bool {
	switch strings.ToLower(strings.TrimSpace(codec)) {
	case "pgssub", "pgs", "sup", "hdmv_pgs_subtitle", "dvd_subtitle", "dvdsub", "vobsub", "dvb_subtitle", "dvbsub":
		return true
	default:
		return false
	}
}

func hasControlChars(s string) bool {
	return strings.ContainsAny(s, "\r\n\x00")
}

// escapeFFmpegFilterPath performs two-level FFmpeg filtergraph escaping for paths placed inside
// single quotes in subtitles='<escaped>'. In particular, single quotes must emit '\'' so stage 1
// (avfilter_graph_parse2) produces \' and stage 2 (av_opt_set_from_string) unescapes it to '.
func escapeFFmpegFilterPath(p string) (string, error) {
	if hasControlChars(p) {
		return "", errors.New("path contains forbidden control character")
	}
	r := strings.NewReplacer(
		`\`, `\\`,
		`:`, `\:`,
		`'`, `'\\\''`,
		`[`, `\[`,
		`]`, `\]`,
		`,`, `\,`,
		`;`, `\;`,
		`=`, `\=`,
	)
	return r.Replace(p), nil
}

func fileReady(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && !fi.IsDir() && fi.Size() > 0
}

func sanitizePathSegment(s string) string {
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	if b.Len() == 0 {
		return "session"
	}
	return b.String()
}
