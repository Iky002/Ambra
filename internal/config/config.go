package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/ambra-media/ambra/internal/core/ffmpegbin"
	"github.com/ambra-media/ambra/internal/pkg/guid"
)

var executablePathFn = os.Executable

const (
	ServerVersion = "4.10.2.0"
	ProductName   = "Ambra"
	// DefaultTMDBApiKey is the built-in fallback TMDB v3 API key used when the user
	// has not configured a custom key in ambra.json or AMBRA_TMDB_API_KEY.
	DefaultTMDBApiKey = "a3b1c06319c480a67f9a491a54cd555d"
	// ChapterCacheReadmeName is written into cache/chapters to warn users before manual deletion.
	ChapterCacheReadmeName = "DO_NOT_DELETE_README.txt"
)

// AmbraVersion is the Ambra application release version (overridden at build time via -ldflags).
var AmbraVersion = "v0.10.7"

type envOverrideFlags struct {
	httpAddr           bool
	webDir             bool
	tmdbKey            bool
	enableDLNA         bool
	enableQuickConnect bool
	allowedOrigins     bool
	trustedProxies     bool
	ffmpegPath         bool
	ffprobePath        bool
}

type diskBaseline struct {
	httpAddr           string
	webDir             string
	tmdbKey            string
	enableDLNA         bool
	enableQuickConnect bool
	allowedOrigins     []string
	trustedProxies     []string
	ffmpegPath         string
	ffprobePath        string
}

type Config struct {
	mu       sync.RWMutex
	saveMu   sync.Mutex
	envFlags envOverrideFlags
	baseline diskBaseline

	ServerID                 string   `json:"server_id"`
	ServerName               string   `json:"server_name"`
	HTTPAddr                 string   `json:"http_addr"`
	DataDir                  string   `json:"data_dir"`
	WebDir                   string   `json:"web_dir"`
	FFmpegPath               string   `json:"ffmpeg_path"`
	FFprobePath              string   `json:"ffprobe_path"`
	TMDBApiKey               string   `json:"tmdb_api_key"`
	TMDBLanguage             string   `json:"tmdb_language"`
	EnableSTRM302            bool     `json:"enable_strm_302"`
	EnableDLNA               bool     `json:"enable_dlna"`
	EnableQuickConnect       bool     `json:"enable_quick_connect"`
	AllowedOrigins           []string `json:"allowed_origins,omitempty"`
	TrustedProxies           []string `json:"trusted_proxies,omitempty"`
	PlayedThreshold          float64  `json:"played_threshold"`
	IsStartupWizardCompleted bool     `json:"is_startup_wizard_completed"`
}

func Default(dataDir string) *Config {
	if dataDir == "" {
		dataDir = "./data"
	}
	return &Config{
		ServerID:                 guid.New(),
		ServerName:               "Ambra Media Server",
		HTTPAddr:                 ":8096",
		DataDir:                  dataDir,
		FFmpegPath:               "ffmpeg",
		FFprobePath:              "ffprobe",
		TMDBLanguage:             "zh-CN",
		EnableSTRM302:            true,
		EnableDLNA:               true,
		EnableQuickConnect:       true,
		PlayedThreshold:          0.90,
		IsStartupWizardCompleted: false,
	}
}

func Load(dataDir string) (*Config, error) {
	cfg := Default(dataDir)
	resolvedDataDir := cfg.DataDir
	for _, sub := range []string{
		"",
		"bin",
		"config",
		"cache/images",
		"cache/chapters",
		"cache/bif",
		"cache/subtitles",
		"cache/transcoding-temp",
		"metadata",
		"logs",
		"plugins",
	} {
		perm := os.FileMode(0o755)
		if sub == "config" || sub == "logs" {
			perm = 0o700
		}
		if err := os.MkdirAll(filepath.Join(cfg.DataDir, sub), perm); err != nil {
			return nil, err
		}
	}

	// Write a reminder file into the dedicated chapter thumbnail cache directory so users
	// do not accidentally wipe pre-extracted chapter images when cleaning temporary caches.
	chapterReadme := filepath.Join(cfg.ChapterCacheDir(), ChapterCacheReadmeName)
	if _, err := os.Stat(chapterReadme); os.IsNotExist(err) {
		const notice = "Ambra 章节缩略图独立缓存目录（请谨慎删除）\n" +
			"==========================================\n" +
			"此目录用于持久保存由计划任务「提取章节缩略图」生成的章节预览图。\n" +
			"手动删除后，影片详情页将不再显示章节图片，需重新在管理后台运行「提取章节缩略图」计划任务方可恢复。\n"
		_ = os.WriteFile(chapterReadme, []byte(notice), 0o644)
	}

	cfgPath := filepath.Join(cfg.DataDir, "config", "ambra.json")
	if raw, err := os.ReadFile(cfgPath); err == nil {
		if err := json.Unmarshal(raw, cfg); err != nil {
			return nil, err
		}
		cfg.DataDir = resolvedDataDir
		if cfg.ServerID == "" {
			cfg.ServerID = guid.New()
			if err := cfg.Save(); err != nil {
				return nil, err
			}
		}
	} else if os.IsNotExist(err) {
		if err := cfg.Save(); err != nil {
			return nil, err
		}
	} else {
		return nil, err
	}

	// Capture persisted disk baseline before applying ephemeral environment overrides
	// so Save() does not permanently bake temporary env vars into ambra.json.
	cfg.baseline = diskBaseline{
		httpAddr:           cfg.HTTPAddr,
		webDir:             cfg.WebDir,
		tmdbKey:            cfg.TMDBApiKey,
		enableDLNA:         cfg.EnableDLNA,
		enableQuickConnect: cfg.EnableQuickConnect,
		allowedOrigins:     append([]string(nil), cfg.AllowedOrigins...),
		trustedProxies:     append([]string(nil), cfg.TrustedProxies...),
		ffmpegPath:         cfg.FFmpegPath,
		ffprobePath:        cfg.FFprobePath,
	}

	// Environment variable overrides (highest priority)
	if v := os.Getenv("AMBRA_HTTP_ADDR"); v != "" {
		cfg.HTTPAddr = v
		cfg.envFlags.httpAddr = true
	}
	if v := os.Getenv("AMBRA_WEB_DIR"); v != "" {
		cfg.WebDir = v
		cfg.envFlags.webDir = true
	}
	if v := os.Getenv("AMBRA_TMDB_API_KEY"); v != "" {
		cfg.TMDBApiKey = v
		cfg.envFlags.tmdbKey = true
	}
	if v := strings.TrimSpace(os.Getenv("AMBRA_ENABLE_DLNA")); v != "" {
		switch strings.ToLower(v) {
		case "false", "0", "off", "no":
			cfg.EnableDLNA = false
			cfg.envFlags.enableDLNA = true
		case "true", "1", "on", "yes":
			cfg.EnableDLNA = true
			cfg.envFlags.enableDLNA = true
		}
	}
	if v := strings.TrimSpace(os.Getenv("AMBRA_ENABLE_QUICK_CONNECT")); v != "" {
		switch strings.ToLower(v) {
		case "false", "0", "off", "no":
			cfg.EnableQuickConnect = false
			cfg.envFlags.enableQuickConnect = true
		case "true", "1", "on", "yes":
			cfg.EnableQuickConnect = true
			cfg.envFlags.enableQuickConnect = true
		}
	}
	if v := strings.TrimSpace(os.Getenv("AMBRA_ALLOWED_ORIGINS")); v != "" {
		cfg.AllowedOrigins = splitCSVEnv(v)
		cfg.envFlags.allowedOrigins = true
	}
	if v := strings.TrimSpace(os.Getenv("AMBRA_TRUSTED_PROXIES")); v != "" {
		cfg.TrustedProxies = splitCSVEnv(v)
		cfg.envFlags.trustedProxies = true
	}

	// Resolve ffmpeg/ffprobe paths via ffmpegbin manager:
	// env override > custom config path > embedded binary > sibling dir > <DataDir>/bin > PATH.
	envFFmpeg := os.Getenv("AMBRA_FFMPEG_PATH")
	envFFprobe := os.Getenv("AMBRA_FFPROBE_PATH")
	if envFFmpeg != "" {
		cfg.envFlags.ffmpegPath = true
	}
	if envFFprobe != "" {
		cfg.envFlags.ffprobePath = true
	}
	exePath, _ := executablePathFn()
	cfg.FFmpegPath, cfg.FFprobePath = ffmpegbin.Resolve(ffmpegbin.ResolveOptions{
		DataDir:        cfg.DataDir,
		ConfigFFmpeg:   cfg.FFmpegPath,
		ConfigFFprobe:  cfg.FFprobePath,
		EnvFFmpeg:      envFFmpeg,
		EnvFFprobe:     envFFprobe,
		ExecutablePath: exePath,
	})
	return cfg, nil
}

func splitCSVEnv(v string) []string {
	var out []string
	for _, p := range strings.Split(v, ",") {
		if s := strings.TrimSpace(p); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func stringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// Update safely mutates configuration fields under the internal mutex.
func (c *Config) Update(fn func(*Config)) {
	if c == nil || fn == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	prevHTTPAddr := c.HTTPAddr
	prevWebDir := c.WebDir
	prevTMDB := c.TMDBApiKey
	prevDLNA := c.EnableDLNA
	prevQC := c.EnableQuickConnect
	prevOrigins := append([]string(nil), c.AllowedOrigins...)
	prevProxies := append([]string(nil), c.TrustedProxies...)
	prevFFmpeg := c.FFmpegPath
	prevFFprobe := c.FFprobePath

	fn(c)

	if c.HTTPAddr != prevHTTPAddr {
		c.envFlags.httpAddr = false
	}
	if c.WebDir != prevWebDir {
		c.envFlags.webDir = false
	}
	if c.TMDBApiKey != prevTMDB {
		c.envFlags.tmdbKey = false
	}
	if c.EnableDLNA != prevDLNA {
		c.envFlags.enableDLNA = false
	}
	if c.EnableQuickConnect != prevQC {
		c.envFlags.enableQuickConnect = false
	}
	if !stringSlicesEqual(c.AllowedOrigins, prevOrigins) {
		c.envFlags.allowedOrigins = false
	}
	if !stringSlicesEqual(c.TrustedProxies, prevProxies) {
		c.envFlags.trustedProxies = false
	}
	if c.FFmpegPath != prevFFmpeg {
		c.envFlags.ffmpegPath = false
	}
	if c.FFprobePath != prevFFprobe {
		c.envFlags.ffprobePath = false
	}
}

// Snapshot returns a thread-safe value copy of the current configuration.
func (c *Config) Snapshot() Config {
	if c == nil {
		return Config{}
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	cp := Config{
		ServerID:                 c.ServerID,
		ServerName:               c.ServerName,
		HTTPAddr:                 c.HTTPAddr,
		DataDir:                  c.DataDir,
		WebDir:                   c.WebDir,
		FFmpegPath:               c.FFmpegPath,
		FFprobePath:              c.FFprobePath,
		TMDBApiKey:               c.TMDBApiKey,
		TMDBLanguage:             c.TMDBLanguage,
		EnableSTRM302:            c.EnableSTRM302,
		EnableDLNA:               c.EnableDLNA,
		EnableQuickConnect:       c.EnableQuickConnect,
		PlayedThreshold:          c.PlayedThreshold,
		IsStartupWizardCompleted: c.IsStartupWizardCompleted,
	}
	if len(c.AllowedOrigins) > 0 {
		cp.AllowedOrigins = append([]string(nil), c.AllowedOrigins...)
	}
	if len(c.TrustedProxies) > 0 {
		cp.TrustedProxies = append([]string(nil), c.TrustedProxies...)
	}
	return cp
}

func (c *Config) Save() error {
	c.saveMu.Lock()
	defer c.saveMu.Unlock()

	c.mu.RLock()
	toSave := Config{
		ServerID:                 c.ServerID,
		ServerName:               c.ServerName,
		HTTPAddr:                 c.HTTPAddr,
		DataDir:                  c.DataDir,
		WebDir:                   c.WebDir,
		FFmpegPath:               c.FFmpegPath,
		FFprobePath:              c.FFprobePath,
		TMDBApiKey:               c.TMDBApiKey,
		TMDBLanguage:             c.TMDBLanguage,
		EnableSTRM302:            c.EnableSTRM302,
		EnableDLNA:               c.EnableDLNA,
		EnableQuickConnect:       c.EnableQuickConnect,
		AllowedOrigins:           append([]string(nil), c.AllowedOrigins...),
		TrustedProxies:           append([]string(nil), c.TrustedProxies...),
		PlayedThreshold:          c.PlayedThreshold,
		IsStartupWizardCompleted: c.IsStartupWizardCompleted,
	}
	if c.envFlags.httpAddr {
		toSave.HTTPAddr = c.baseline.httpAddr
	}
	if c.envFlags.webDir {
		toSave.WebDir = c.baseline.webDir
	}
	if c.envFlags.tmdbKey {
		toSave.TMDBApiKey = c.baseline.tmdbKey
	}
	if c.envFlags.enableDLNA {
		toSave.EnableDLNA = c.baseline.enableDLNA
	}
	if c.envFlags.enableQuickConnect {
		toSave.EnableQuickConnect = c.baseline.enableQuickConnect
	}
	if c.envFlags.allowedOrigins {
		toSave.AllowedOrigins = append([]string(nil), c.baseline.allowedOrigins...)
	}
	if c.envFlags.trustedProxies {
		toSave.TrustedProxies = append([]string(nil), c.baseline.trustedProxies...)
	}
	if c.envFlags.ffmpegPath {
		toSave.FFmpegPath = c.baseline.ffmpegPath
	}
	if c.envFlags.ffprobePath {
		toSave.FFprobePath = c.baseline.ffprobePath
	}
	dataDir := c.DataDir
	c.mu.RUnlock()

	raw, err := json.MarshalIndent(&toSave, "", "  ")
	if err != nil {
		return err
	}
	cfgDir := filepath.Join(dataDir, "config")
	if err := os.MkdirAll(cfgDir, 0o700); err != nil {
		return err
	}
	tmpFile, err := os.CreateTemp(cfgDir, ".ambra-*.json.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmpFile.Name()
	if err := tmpFile.Chmod(0o600); err != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmpPath)
		return err
	}
	if _, err := tmpFile.Write(raw); err != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmpPath)
		return err
	}
	if err := tmpFile.Sync(); err != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmpPath)
		return err
	}
	if err := tmpFile.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	target := filepath.Join(cfgDir, "ambra.json")
	if err := os.Rename(tmpPath, target); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	return nil
}

// EffectiveTMDBApiKey returns the user/env-configured TMDB API key when set,
// falling back to DefaultTMDBApiKey otherwise.
func (c *Config) EffectiveTMDBApiKey() string {
	if c != nil {
		if k := strings.TrimSpace(c.Snapshot().TMDBApiKey); k != "" {
			return k
		}
	}
	return DefaultTMDBApiKey
}

func (c *Config) DBPath() string {
	return filepath.Join(c.DataDir, "ambra.db")
}

func (c *Config) ImageCacheDir() string {
	return filepath.Join(c.DataDir, "cache", "images")
}

// ChapterCacheDir returns the dedicated persistent cache directory for chapter thumbnails.
// Kept separate from ImageCacheDir so automatic image resize cache pruning never removes them.
func (c *Config) ChapterCacheDir() string {
	return filepath.Join(c.DataDir, "cache", "chapters")
}

// BifCacheDir returns the dedicated persistent cache directory for generated Emby/Roku BIF files.
func (c *Config) BifCacheDir() string {
	return filepath.Join(c.DataDir, "cache", "bif")
}

func (c *Config) SubtitleCacheDir() string {
	return filepath.Join(c.DataDir, "cache", "subtitles")
}

func (c *Config) TranscodeTempDir() string {
	return filepath.Join(c.DataDir, "cache", "transcoding-temp")
}
