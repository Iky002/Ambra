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
)

// AmbraVersion is the Ambra application release version (overridden at build time via -ldflags).
var AmbraVersion = "v0.10.5"

type Config struct {
	mu            sync.RWMutex
	saveMu        sync.Mutex
	envTMDBKeySet bool

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

	cfgPath := filepath.Join(cfg.DataDir, "config", "ambra.json")
	if raw, err := os.ReadFile(cfgPath); err == nil {
		if err := json.Unmarshal(raw, cfg); err != nil {
			return nil, err
		}
		if cfg.ServerID == "" {
			cfg.ServerID = guid.New()
		}
		cfg.DataDir = resolvedDataDir
	} else if os.IsNotExist(err) {
		if err := cfg.Save(); err != nil {
			return nil, err
		}
	} else {
		return nil, err
	}

	// Environment variable overrides (highest priority)
	if v := os.Getenv("AMBRA_HTTP_ADDR"); v != "" {
		cfg.HTTPAddr = v
	}
	if v := os.Getenv("AMBRA_WEB_DIR"); v != "" {
		cfg.WebDir = v
	}
	if v := os.Getenv("AMBRA_TMDB_API_KEY"); v != "" {
		cfg.TMDBApiKey = v
		cfg.envTMDBKeySet = true
	}
	if v := strings.TrimSpace(os.Getenv("AMBRA_ENABLE_DLNA")); v != "" {
		switch strings.ToLower(v) {
		case "false", "0", "off", "no":
			cfg.EnableDLNA = false
		case "true", "1", "on", "yes":
			cfg.EnableDLNA = true
		}
	}
	if v := strings.TrimSpace(os.Getenv("AMBRA_ENABLE_QUICK_CONNECT")); v != "" {
		switch strings.ToLower(v) {
		case "false", "0", "off", "no":
			cfg.EnableQuickConnect = false
		case "true", "1", "on", "yes":
			cfg.EnableQuickConnect = true
		}
	}
	if v := strings.TrimSpace(os.Getenv("AMBRA_ALLOWED_ORIGINS")); v != "" {
		cfg.AllowedOrigins = splitCSVEnv(v)
	}
	if v := strings.TrimSpace(os.Getenv("AMBRA_TRUSTED_PROXIES")); v != "" {
		cfg.TrustedProxies = splitCSVEnv(v)
	}

	// Resolve ffmpeg/ffprobe paths via ffmpegbin manager:
	// env override > custom config path > embedded binary > sibling dir > <DataDir>/bin > PATH.
	exePath, _ := executablePathFn()
	cfg.FFmpegPath, cfg.FFprobePath = ffmpegbin.Resolve(ffmpegbin.ResolveOptions{
		DataDir:        cfg.DataDir,
		ConfigFFmpeg:   cfg.FFmpegPath,
		ConfigFFprobe:  cfg.FFprobePath,
		EnvFFmpeg:      os.Getenv("AMBRA_FFMPEG_PATH"),
		EnvFFprobe:     os.Getenv("AMBRA_FFPROBE_PATH"),
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

// Update safely mutates configuration fields under the internal mutex.
func (c *Config) Update(fn func(*Config)) {
	if c == nil || fn == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	prevTMDB := c.TMDBApiKey
	fn(c)
	if c.TMDBApiKey != prevTMDB {
		c.envTMDBKeySet = false
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
	if c.envTMDBKeySet {
		toSave.TMDBApiKey = ""
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

func (c *Config) DBPath() string {
	return filepath.Join(c.DataDir, "ambra.db")
}

func (c *Config) ImageCacheDir() string {
	return filepath.Join(c.DataDir, "cache", "images")
}

func (c *Config) SubtitleCacheDir() string {
	return filepath.Join(c.DataDir, "cache", "subtitles")
}

func (c *Config) TranscodeTempDir() string {
	return filepath.Join(c.DataDir, "cache", "transcoding-temp")
}
