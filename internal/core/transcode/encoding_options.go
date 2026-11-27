package transcode

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/ambra-media/ambra/internal/store/sqlite"
)

const (
	defaultThrottleBufferSec     = 120
	defaultThrottleHysteresisSec = 8
	defaultMaxTranscodeCacheSize = int64(8 << 30) // 8 GiB
)

// EncodingOptions represents the runtime-effective /Encoding/* settings loaded from server_config.
type EncodingOptions struct {
	HardwareAccelerationMode string
	EnableHardwareEncoding   bool
	AllowHevcEncoding        bool
	EnableThrottling         bool
	ThrottleBufferSize       int
	ThrottleHysteresis       int
	H264Crf                  int
	H264Preset               string
	TranscodingTempPath      string
	EnableToneMapping        bool
	ToneMappingAlgorithm     string
	ToneMappingPeak          float64
}

// DefaultEncodingOptions returns the baseline encoding options when no DB overrides are stored.
func DefaultEncodingOptions() EncodingOptions {
	return EncodingOptions{
		HardwareAccelerationMode: "1",
		EnableHardwareEncoding:   true,
		AllowHevcEncoding:        true,
		EnableThrottling:         true,
		ThrottleBufferSize:       defaultThrottleBufferSec,
		ThrottleHysteresis:       defaultThrottleHysteresisSec,
		H264Crf:                  0,
		H264Preset:               "veryfast",
		TranscodingTempPath:      "",
		EnableToneMapping:        true,
		ToneMappingAlgorithm:     "hable",
		ToneMappingPeak:          100,
	}
}

func (o EncodingOptions) isHardwareAllowed() bool {
	if !o.EnableHardwareEncoding {
		return false
	}
	mode := strings.ToLower(strings.TrimSpace(o.HardwareAccelerationMode))
	switch mode {
	case "0", "none", "false", "off", "disabled", "software":
		return false
	default:
		return true
	}
}

func normalizeX264Preset(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "ultrafast", "superfast", "veryfast", "faster", "fast", "medium", "slow", "slower", "veryslow":
		return strings.ToLower(strings.TrimSpace(raw))
	default:
		return "veryfast"
	}
}

func normalizeTonemapAlgorithm(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "hable", "mobius", "reinhard", "bt2390", "linear", "gamma", "clip":
		return strings.ToLower(strings.TrimSpace(raw))
	default:
		return "hable"
	}
}

func buildSoftwareTonemapFilter(algo string, peak float64, pixFmt string) string {
	a := normalizeTonemapAlgorithm(algo)
	p := peak
	if p <= 0 || math.IsNaN(p) || math.IsInf(p, 0) {
		p = 100
	}
	if pixFmt == "" {
		pixFmt = "yuv420p"
	}
	peakStr := strconv.FormatFloat(p, 'f', -1, 64)
	return fmt.Sprintf("zscale=t=linear:npl=%s,tonemap=%s,zscale=p=bt709:t=bt709:m=bt709,format=%s", peakStr, a, pixFmt)
}

// SetStore attaches the SQLite store to Manager and immediately loads persisted /Encoding/* options.
func (m *Manager) SetStore(store *sqlite.Store) {
	if m == nil {
		return
	}
	m.encMu.Lock()
	m.store = store
	m.encMu.Unlock()
	if store != nil {
		m.ReloadEncodingOptions(context.Background())
	}
}

// EncodingOptionsSnapshot returns a thread-safe copy of the active encoding options.
func (m *Manager) EncodingOptionsSnapshot() EncodingOptions {
	if m == nil {
		return DefaultEncodingOptions()
	}
	m.encMu.RLock()
	defer m.encMu.RUnlock()
	return m.encOpts
}

// ReloadEncodingOptions refreshes cached encoding_ffmpeg_options and encoding_full_tonemap from SQLite.
func (m *Manager) ReloadEncodingOptions(ctx context.Context) {
	if m == nil {
		return
	}
	m.encMu.RLock()
	store := m.store
	overrideThrottle := m.throttleOverridden
	overrideOpts := m.encOpts
	m.encMu.RUnlock()

	if store == nil || store.Reader == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}

	opts := DefaultEncodingOptions()
	if overrideThrottle {
		opts.EnableThrottling = overrideOpts.EnableThrottling
		opts.ThrottleBufferSize = overrideOpts.ThrottleBufferSize
		opts.ThrottleHysteresis = overrideOpts.ThrottleHysteresis
	}

	if ffMap, ok := readServerConfigMap(ctx, store, "encoding_ffmpeg_options"); ok {
		if v, exists := ffMap["HardwareAccelerationMode"]; exists {
			opts.HardwareAccelerationMode = anyToString(v, opts.HardwareAccelerationMode)
		}
		if v, exists := ffMap["EnableHardwareEncoding"]; exists {
			opts.EnableHardwareEncoding = anyToBool(v, opts.EnableHardwareEncoding)
		}
		if v, exists := ffMap["AllowHevcEncoding"]; exists {
			opts.AllowHevcEncoding = anyToBool(v, opts.AllowHevcEncoding)
		}
		if v, exists := ffMap["EnableThrottling"]; exists && !overrideThrottle {
			opts.EnableThrottling = anyToBool(v, opts.EnableThrottling)
		}
		if v, exists := ffMap["ThrottleBufferSize"]; exists && !overrideThrottle {
			if iv, ok := anyToInt(v); ok && iv > 0 {
				opts.ThrottleBufferSize = iv
			}
		}
		if v, exists := ffMap["ThrottleHysteresis"]; exists && !overrideThrottle {
			if iv, ok := anyToInt(v); ok && iv >= 0 {
				opts.ThrottleHysteresis = iv
			}
		}
		if v, exists := ffMap["H264Crf"]; exists {
			if iv, ok := anyToInt(v); ok && iv >= 0 && iv <= 51 {
				opts.H264Crf = iv
			}
		}
		if v, exists := ffMap["H264Preset"]; exists {
			opts.H264Preset = normalizeX264Preset(anyToString(v, opts.H264Preset))
		}
		if v, exists := ffMap["TranscodingTempPath"]; exists {
			rawPath := strings.TrimSpace(anyToString(v, ""))
			if rawPath != "" {
				dataDir := ""
				if m.cfg != nil {
					dataDir = m.cfg.DataDir
				}
				if validated, err := ValidateTranscodingTempPath(rawPath, dataDir); err == nil {
					opts.TranscodingTempPath = validated
				}
			}
		}
	}

	// Load public tonemap first, then let full tonemap override if present.
	for _, key := range []string{"encoding_public_tonemap", "encoding_full_tonemap"} {
		if tmMap, ok := readServerConfigMap(ctx, store, key); ok {
			if v, exists := tmMap["EnableToneMapping"]; exists {
				opts.EnableToneMapping = anyToBool(v, opts.EnableToneMapping)
			}
			if v, exists := tmMap["ToneMappingAlgorithm"]; exists {
				opts.ToneMappingAlgorithm = normalizeTonemapAlgorithm(anyToString(v, opts.ToneMappingAlgorithm))
			}
			if v, exists := tmMap["ToneMappingPeak"]; exists {
				if fv, ok := anyToFloat(v); ok && fv > 0 {
					opts.ToneMappingPeak = fv
				}
			}
		}
	}

	m.encMu.Lock()
	m.encOpts = opts
	m.encLoadedFromStore = true
	m.encMu.Unlock()

	// Wake any throttled sessions immediately if throttling was disabled or thresholds increased.
	m.mu.Lock()
	sessions := make([]*transcodeSession, 0, len(m.sessions))
	for _, s := range m.sessions {
		sessions = append(sessions, s)
	}
	m.mu.Unlock()
	for _, s := range sessions {
		s.evaluateThrottle(opts)
	}
}

// ValidateTranscodingTempPath validates a configured transcode temp directory, rejecting relative paths,
// control characters, root (/), OS system directories, and paths equal to or containing DataDir.
func ValidateTranscodingTempPath(rawPath, dataDir string) (string, error) {
	trimmed := strings.TrimSpace(rawPath)
	if trimmed == "" {
		return "", errors.New("transcoding temp path is empty")
	}
	if hasControlChars(trimmed) {
		return "", errors.New("transcoding temp path contains forbidden control characters")
	}
	if !filepath.IsAbs(trimmed) {
		return "", errors.New("transcoding temp path must be an absolute path")
	}
	resolved, err := resolvePathOrNearestAncestor(filepath.Clean(trimmed))
	if err != nil {
		return "", fmt.Errorf("resolve transcoding temp path: %w", err)
	}
	if isForbiddenSystemDir(resolved) {
		return "", fmt.Errorf("transcoding temp path %q is a forbidden system directory", resolved)
	}
	if strings.TrimSpace(dataDir) != "" {
		if absData, absErr := filepath.Abs(strings.TrimSpace(dataDir)); absErr == nil {
			resolvedData, resErr := resolvePathOrNearestAncestor(filepath.Clean(absData))
			if resErr != nil {
				resolvedData = filepath.Clean(absData)
			}
			if resolved == resolvedData || strings.HasPrefix(resolvedData, resolved+string(filepath.Separator)) {
				return "", fmt.Errorf("transcoding temp path %q overlaps server data directory", resolved)
			}
		}
	}
	return resolved, nil
}

func resolvePathOrNearestAncestor(cleanAbs string) (string, error) {
	cur := cleanAbs
	var tail []string
	for {
		if realCur, err := filepath.EvalSymlinks(cur); err == nil {
			out := realCur
			for i := len(tail) - 1; i >= 0; i-- {
				out = filepath.Join(out, tail[i])
			}
			return filepath.Clean(out), nil
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return filepath.Clean(cleanAbs), nil
		}
		tail = append(tail, filepath.Base(cur))
		cur = parent
	}
}

func isForbiddenSystemDir(cleanResolved string) bool {
	p := filepath.ToSlash(filepath.Clean(cleanResolved))
	if p == "/" || p == "." {
		return true
	}
	for _, sys := range []string{
		"/etc", "/proc", "/sys", "/dev", "/boot", "/root",
		"/bin", "/sbin", "/usr", "/lib", "/lib64",
		"/private/etc",
	} {
		if p == sys || strings.HasPrefix(p, sys+"/") {
			return true
		}
	}
	return false
}

// CurrentTempDir returns the currently effective transcode base directory (honoring runtime TranscodingTempPath).
func (m *Manager) CurrentTempDir() string {
	if m == nil {
		return filepath.Join(os.TempDir(), "ambra-transcode")
	}
	return m.effectiveTranscodeBaseDir()
}

func (m *Manager) applyEncodingOptionsToParams(p SessionParams) SessionParams {
	m.encMu.RLock()
	opts := m.encOpts
	loaded := m.encLoadedFromStore
	m.encMu.RUnlock()

	if !opts.AllowHevcEncoding {
		vc := strings.ToLower(strings.TrimSpace(p.VideoCodec))
		if vc == "hevc" || vc == "h265" {
			p.VideoCodec = "h264"
		}
	}
	if loaded {
		if p.SoftwareCRF <= 0 && opts.H264Crf > 0 {
			p.SoftwareCRF = opts.H264Crf
		}
		if strings.TrimSpace(p.SoftwarePreset) == "" && opts.H264Preset != "" {
			p.SoftwarePreset = opts.H264Preset
		}
		if !opts.EnableToneMapping {
			p.DisableToneMapping = true
		}
		if strings.TrimSpace(p.ToneMappingAlgorithm) == "" && opts.ToneMappingAlgorithm != "" {
			p.ToneMappingAlgorithm = opts.ToneMappingAlgorithm
		}
		if p.ToneMappingPeak <= 0 && opts.ToneMappingPeak > 0 {
			p.ToneMappingPeak = opts.ToneMappingPeak
		}
	}
	return p
}

func (m *Manager) effectiveTranscodeBaseDir() string {
	m.encMu.RLock()
	customPath := m.encOpts.TranscodingTempPath
	m.encMu.RUnlock()

	if customPath != "" {
		if err := os.MkdirAll(customPath, 0o700); err == nil {
			if fi, statErr := os.Stat(customPath); statErr == nil && fi.IsDir() {
				return customPath
			}
		}
	}
	if m.cfg != nil {
		return m.cfg.TranscodeTempDir()
	}
	return filepath.Join(os.TempDir(), "ambra-transcode")
}

func readServerConfigMap(ctx context.Context, store *sqlite.Store, key string) (map[string]any, bool) {
	var raw string
	err := store.Reader.QueryRowContext(ctx, `SELECT value_json FROM server_config WHERE key = ?`, key).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) || err != nil || strings.TrimSpace(raw) == "" {
		return nil, false
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(raw), &out); err != nil || out == nil {
		return nil, false
	}
	return out, true
}

func anyToString(v any, def string) string {
	switch val := v.(type) {
	case string:
		return val
	case float64:
		return strconv.FormatFloat(val, 'f', -1, 64)
	case int:
		return strconv.Itoa(val)
	case int64:
		return strconv.FormatInt(val, 10)
	case bool:
		if val {
			return "true"
		}
		return "false"
	default:
		return def
	}
}

func anyToBool(v any, def bool) bool {
	switch val := v.(type) {
	case bool:
		return val
	case float64:
		return val != 0
	case int:
		return val != 0
	case string:
		switch strings.ToLower(strings.TrimSpace(val)) {
		case "true", "1", "yes", "on":
			return true
		case "false", "0", "no", "off":
			return false
		}
	}
	return def
}

func anyToInt(v any) (int, bool) {
	switch val := v.(type) {
	case float64:
		return int(val), true
	case int:
		return val, true
	case int64:
		return int(val), true
	case string:
		if iv, err := strconv.Atoi(strings.TrimSpace(val)); err == nil {
			return iv, true
		}
	}
	return 0, false
}

func anyToFloat(v any) (float64, bool) {
	switch val := v.(type) {
	case float64:
		return val, true
	case int:
		return float64(val), true
	case int64:
		return float64(val), true
	case string:
		if fv, err := strconv.ParseFloat(strings.TrimSpace(val), 64); err == nil {
			return fv, true
		}
	}
	return 0, false
}
