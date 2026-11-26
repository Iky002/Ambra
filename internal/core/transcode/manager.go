package transcode

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/pkg/netutil"
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
	AudioStreamIndex      *int
	SubtitleStreamIndex   *int
	SubtitleRelativeIndex int    // 0-based index among embedded subtitle streams (for FFmpeg subtitles filter :si=)
	SubtitleMethod        string // "Encode" for burn-in
	SubtitleCodec         string // e.g. "ass", "srt", "pgssub", "dvd_subtitle"
	SubtitlePath          string // external subtitle path when IsExternal
	IsHDR                 bool   // true when source video is HDR (enables tonemap filter when transcoding)
	SegmentDuration       int    // seconds, default 6
	SegmentContainer      string // "ts" (default), "mp4", "m4s"
	DurationSec           float64
}

func (p *SessionParams) normalize() {
	if p.SegmentDuration <= 0 {
		p.SegmentDuration = DefaultSegmentDuration
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
	if p.AudioBitrate <= 0 {
		p.AudioBitrate = 192_000
	}
	if p.AudioChannels <= 0 {
		p.AudioChannels = 2
	}
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
	startSec := startSeg * segDur
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
	// Never pass raw http(s):// URLs into FFmpeg's subtitles= filter (bypasses -follow_redirects 0).
	if burnIn && !isBitmapSub && (isHTTPInput(subPath) || (subPath == "" && isHTTPInput(inputPath))) {
		burnIn = false
	}
	textBurnInSeek := burnIn && !isBitmapSub && startSec > 0

	isVideoCopy := strings.EqualFold(params.VideoCodec, "copy") && !burnIn
	var encoder string
	if !isVideoCopy {
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

	// Fast input seek when starting from a non-zero segment.
	// For text subtitle burn-in with seek, preserve input timestamps (-copyts) so libass aligns cues,
	// then trim accurately with output-side -ss.
	if startSec > 0 {
		args = append(args, "-ss", strconv.Itoa(startSec))
		if textBurnInSeek {
			args = append(args, "-copyts")
		}
	}

	// Resilient HTTP(S) input flags for remote STRM / Alist / cloud drive URLs.
	// Disable FFmpeg HTTP redirect following so redirects cannot pivot to cloud metadata or disallowed schemes.
	if isRemoteHTTP {
		args = append(args,
			"-protocol_whitelist", "http,https,tcp,tls,crypto",
			"-follow_redirects", "0",
			"-reconnect", "1",
			"-reconnect_streamed", "1",
			"-reconnect_delay_max", "5",
			"-rw_timeout", "15000000",
			"-user_agent", defaultHTTPUserAgent,
		)
	}

	args = append(args, "-i", inputPath)

	if textBurnInSeek {
		args = append(args, "-ss", strconv.Itoa(startSec))
	}

	// Build video filter parts for tonemapping, subtitle burn-in, scaling, and VAAPI hardware upload.
	var vfParts []string
	if !isVideoCopy {
		var vaapiTonemap string
		if params.IsHDR {
			tm := caps.TonemapFilterForEncoder(encoder)
			switch {
			case strings.HasSuffix(encoder, "_vaapi") && strings.Contains(tm, "vaapi"):
				vaapiTonemap = tm
			case strings.HasSuffix(encoder, "_nvenc") && (tm == "" || strings.Contains(tm, "tonemap_cuda") || strings.Contains(tm, "_cuda")):
				// BuildFFmpegArgs uses CPU decode; use valid software zscale+tonemap before NVENC encode.
				vfParts = append(vfParts, softwareTonemapFilter)
			case strings.HasSuffix(encoder, "_qsv") && (tm == "" || strings.Contains(tm, "vpp_qsv") || strings.Contains(tm, "_qsv")):
				// BuildFFmpegArgs uses CPU decode; use valid software zscale+tonemap -> nv12 for QSV encode.
				vfParts = append(vfParts, qsvSoftwareTonemapFilter)
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
			args = append(args, "-preset", "veryfast", "-pix_fmt", "yuv420p")
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
		}
		keyframeExpr := fmt.Sprintf("expr:gte(t,n_forced*%d)", segDur)
		if textBurnInSeek && startSec > 0 {
			keyframeExpr = fmt.Sprintf("expr:gte(t-%d,n_forced*%d)", startSec, segDur)
		}
		args = append(args,
			"-b:v", strconv.Itoa(vBitrate),
			"-maxrate", strconv.Itoa(vBitrate),
			"-bufsize", strconv.Itoa(vBitrate*2),
			"-force_key_frames", keyframeExpr,
		)
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
		args = append(args, "-hls_segment_type", "fmp4", "-hls_fmp4_init_filename", "init.mp4")
	}
	if startSec > 0 && ext == "ts" && !textBurnInSeek {
		args = append(args, "-output_ts_offset", strconv.Itoa(startSec))
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

// Manager orchestrates active FFmpeg dynamic HLS transcoding sessions.
type Manager struct {
	cfg                *config.Config
	detector           *HardwareDetector
	idleTimeout        time.Duration
	cleanupInterval    time.Duration
	maxSessions        int
	maxSessionsPerUser int

	mu       sync.Mutex
	sessions map[string]*transcodeSession
	closed   bool

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
		sessions:           make(map[string]*transcodeSession),
		stopCh:             make(chan struct{}),
	}
	for _, opt := range opts {
		opt(m)
	}

	m.wg.Add(1)
	go m.reaperLoop()
	return m
}

// Capabilities returns the cached hardware acceleration capabilities.
func (m *Manager) Capabilities(ctx context.Context) *HWCapabilities {
	return m.detector.Detect(ctx)
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
	params.normalize()
	if err := validateSessionPaths(params, true); err != nil {
		return "", err
	}

	sess, err := m.getOrCreateSession(params)
	if err != nil {
		return "", err
	}
	return sess.getSegment(ctx, segIndex, func() *HWCapabilities {
		return m.Capabilities(ctx)
	})
}

// GetInitSegment ensures the fMP4 transcode session is running and waits for workDir/init.mp4 to be ready.
func (m *Manager) GetInitSegment(ctx context.Context, params SessionParams) (string, error) {
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
		a.IsHDR != b.IsHDR ||
		a.SubtitleRelativeIndex != b.SubtitleRelativeIndex ||
		a.SegmentDuration != b.SegmentDuration ||
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

		baseDir := filepath.Join(os.TempDir(), "ambra-transcode")
		if m.cfg != nil {
			baseDir = m.cfg.TranscodeTempDir()
		}
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

		sess := &transcodeSession{
			key:        key,
			ffmpegPath: ffmpegPath,
			params:     params,
			workDir:    workDir,
			startSeg:   -1,
			lastAccess: time.Now(),
		}
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
		}
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
	key                string
	ffmpegPath         string
	params             SessionParams
	workDir            string
	segmentWaitTimeout time.Duration

	mu            sync.Mutex
	cmd           *exec.Cmd
	cancel        context.CancelFunc
	doneCh        chan struct{}
	lastErr       error
	stderrBuf     *safeRingBuffer
	startSeg      int
	running       bool
	destroyed     bool
	usingHW       bool
	forceSoftware bool
	lastAccess    time.Time
}

func (s *transcodeSession) isDestroyed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.destroyed
}

func (s *transcodeSession) initSegmentPath() string {
	return filepath.Join(s.workDir, "init.mp4")
}

func (s *transcodeSession) touch() {
	s.mu.Lock()
	s.lastAccess = time.Now()
	s.mu.Unlock()
}

func (s *transcodeSession) lastAccessTime() time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastAccess
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
		LastAccess:    s.lastAccess,
		WorkDir:       s.workDir,
	}
}

func (s *transcodeSession) segmentPath(segIndex int) string {
	return filepath.Join(s.workDir, fmt.Sprintf("seg_%d.%s", segIndex, s.params.SegmentContainer))
}

func (s *transcodeSession) highestReadySegmentLocked() int {
	highest := s.startSeg - 1
	entries, err := os.ReadDir(s.workDir)
	if err != nil {
		return highest
	}
	suffix := "." + s.params.SegmentContainer
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, "seg_") || !strings.HasSuffix(name, suffix) {
			continue
		}
		numStr := strings.TrimSuffix(strings.TrimPrefix(name, "seg_"), suffix)
		if idx, err := strconv.Atoi(numStr); err == nil && idx > highest {
			highest = idx
		}
	}
	return highest
}

func (s *transcodeSession) getSegment(ctx context.Context, segIndex int, getCaps func() *HWCapabilities) (string, error) {
	if s.isDestroyed() {
		return "", ErrSessionClosed
	}
	s.touch()
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
		if err := s.startProcessLocked(segIndex, caps); err != nil {
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
			if err := s.startProcessLocked(0, caps); err != nil {
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
				if err := s.startProcessLocked(restartSeg, SoftwareOnlyCapabilities()); err != nil {
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

func downloadRemoteSubtitleAtomic(rawURL, dst string, client *http.Client) error {
	if client == nil {
		if err := netutil.ValidateRemoteURL(rawURL, true); err != nil {
			return err
		}
		client = netutil.NewSafeHTTPClient(remoteSubtitleDownloadTimeout, true)
	}
	ctx, cancel := context.WithTimeout(context.Background(), remoteSubtitleDownloadTimeout)
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

func extractRemoteEmbeddedSubtitle(ffmpegPath, mediaURL string, streamIndex int, subCodec, dst string) error {
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

	ctx, cancel := context.WithTimeout(context.Background(), defaultExtractTimeout)
	defer cancel()
	args := BuildSubtitleExtractArgs(mediaURL, streamIndex, ext, tmpPath)
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
			if err := downloadRemoteSubtitleAtomic(subPath, dst, httpClient); err == nil {
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
			if err := extractRemoteEmbeddedSubtitle(ffmpegPath, inputPath, *params.SubtitleStreamIndex, params.SubtitleCodec, dst); err == nil {
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

func (s *transcodeSession) startProcessLocked(startSeg int, caps *HWCapabilities) error {
	if s.destroyed {
		return ErrSessionClosed
	}
	if err := os.MkdirAll(s.workDir, 0o755); err != nil {
		return err
	}
	// Clean any stale .tmp files from previous interrupted runs
	if entries, err := os.ReadDir(s.workDir); err == nil {
		for _, e := range entries {
			if strings.HasSuffix(e.Name(), ".tmp") {
				_ = os.Remove(filepath.Join(s.workDir, e.Name()))
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
	usingHW := (!strings.EqualFold(s.params.VideoCodec, "copy") || strings.EqualFold(strings.TrimSpace(s.params.SubtitleMethod), "Encode")) &&
		isHardwareEncoder(effectiveCaps.SelectVideoEncoder(vCodec))

	effectiveParams := stageBurnInFilesWithDeps(s.params, s.workDir, s.ffmpegPath, nil)
	args := BuildFFmpegArgs(effectiveParams, startSeg, s.workDir, effectiveCaps)
	procCtx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(procCtx, s.ffmpegPath, args...)

	stderrBuf := newSafeRingBuffer(maxStderrBytes)
	cmd.Stderr = stderrBuf

	if err := cmd.Start(); err != nil {
		cancel()
		return fmt.Errorf("start ffmpeg: %w", err)
	}

	doneCh := make(chan struct{})
	s.cmd = cmd
	s.cancel = cancel
	s.doneCh = doneCh
	s.stderrBuf = stderrBuf
	s.startSeg = startSeg
	s.running = true
	s.usingHW = usingHW
	s.lastErr = nil
	s.lastAccess = time.Now()

	go func() {
		err := cmd.Wait()
		s.mu.Lock()
		if s.cmd == cmd {
			s.running = false
			s.lastErr = err
		}
		s.mu.Unlock()
		close(doneCh)
	}()

	return nil
}

func (s *transcodeSession) stopProcessLocked() {
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
