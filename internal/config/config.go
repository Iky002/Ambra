package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"

	"github.com/ambra-media/ambra/internal/pkg/guid"
)

const (
	ServerVersion = "4.10.2.0"
	ProductName   = "Ambra"
)

type Config struct {
	mu sync.RWMutex

	ServerID                 string `json:"server_id"`
	ServerName               string `json:"server_name"`
	HTTPAddr                 string `json:"http_addr"`
	DataDir                  string `json:"data_dir"`
	WebDir                   string `json:"web_dir"`
	FFmpegPath               string `json:"ffmpeg_path"`
	FFprobePath              string `json:"ffprobe_path"`
	TMDBApiKey               string `json:"tmdb_api_key"`
	TMDBLanguage             string `json:"tmdb_language"`
	EnableSTRM302            bool   `json:"enable_strm_302"`
	PlayedThreshold          float64 `json:"played_threshold"`
	IsStartupWizardCompleted bool   `json:"is_startup_wizard_completed"`
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
		PlayedThreshold:          0.90,
		IsStartupWizardCompleted: false,
	}
}

func Load(dataDir string) (*Config, error) {
	cfg := Default(dataDir)
	for _, sub := range []string{
		"",
		"config",
		"cache/images",
		"cache/subtitles",
		"cache/transcoding-temp",
		"metadata",
		"logs",
		"plugins",
	} {
		if err := os.MkdirAll(filepath.Join(cfg.DataDir, sub), 0o755); err != nil {
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
		cfg.DataDir = dataDir
	} else if os.IsNotExist(err) {
		if err := cfg.Save(); err != nil {
			return nil, err
		}
	} else {
		return nil, err
	}

	// Environment variable overrides
	if v := os.Getenv("AMBRA_HTTP_ADDR"); v != "" {
		cfg.HTTPAddr = v
	}
	if v := os.Getenv("AMBRA_WEB_DIR"); v != "" {
		cfg.WebDir = v
	}
	if v := os.Getenv("AMBRA_TMDB_API_KEY"); v != "" {
		cfg.TMDBApiKey = v
	}
	return cfg, nil
}

func (c *Config) Save() error {
	c.mu.RLock()
	raw, err := json.MarshalIndent(c, "", "  ")
	dataDir := c.DataDir
	c.mu.RUnlock()
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dataDir, "config", "ambra.json"), raw, 0o644)
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
