package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestEnvOverridesNotPersistedOnSaveAndClearedOnUpdate(t *testing.T) {
	dir := t.TempDir()
	// Create initial persisted config with known baseline values.
	initial, err := Load(dir)
	if err != nil {
		t.Fatalf("initial Load: %v", err)
	}
	initial.Update(func(c *Config) {
		c.HTTPAddr = ":8096"
		c.WebDir = "/persisted/web"
		c.TMDBApiKey = "persisted-tmdb"
		c.EnableDLNA = true
		c.EnableQuickConnect = true
		c.AllowedOrigins = []string{"https://persisted.example"}
		c.TrustedProxies = []string{"127.0.0.1"}
		c.FFmpegPath = "/persisted/ffmpeg"
		c.FFprobePath = "/persisted/ffprobe"
	})
	if err := initial.Save(); err != nil {
		t.Fatalf("initial Save: %v", err)
	}

	// Reload with all env overrides active.
	t.Setenv("AMBRA_HTTP_ADDR", ":9999")
	t.Setenv("AMBRA_WEB_DIR", "/env/web")
	t.Setenv("AMBRA_TMDB_API_KEY", "env-tmdb")
	t.Setenv("AMBRA_ENABLE_DLNA", "false")
	t.Setenv("AMBRA_ENABLE_QUICK_CONNECT", "false")
	t.Setenv("AMBRA_ALLOWED_ORIGINS", "https://env.example")
	t.Setenv("AMBRA_TRUSTED_PROXIES", "10.0.0.1")
	t.Setenv("AMBRA_FFMPEG_PATH", "/env/ffmpeg")
	t.Setenv("AMBRA_FFPROBE_PATH", "/env/ffprobe")

	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load with env overrides: %v", err)
	}
	if cfg.HTTPAddr != ":9999" || cfg.WebDir != "/env/web" || cfg.TMDBApiKey != "env-tmdb" ||
		cfg.EnableDLNA || cfg.EnableQuickConnect ||
		cfg.FFmpegPath != "/env/ffmpeg" || cfg.FFprobePath != "/env/ffprobe" {
		t.Fatalf("env overrides not applied in memory: %+v", cfg)
	}

	// Mutate an unrelated field and Save(): env overrides must NOT overwrite disk baseline.
	cfg.Update(func(c *Config) {
		c.ServerName = "Renamed Server"
	})
	if err := cfg.Save(); err != nil {
		t.Fatalf("Save with env overrides: %v", err)
	}

	raw, err := os.ReadFile(filepath.Join(dir, "config", "ambra.json"))
	if err != nil {
		t.Fatalf("read ambra.json: %v", err)
	}
	var disk Config
	if err := json.Unmarshal(raw, &disk); err != nil {
		t.Fatalf("unmarshal ambra.json: %v", err)
	}
	if disk.ServerName != "Renamed Server" {
		t.Fatalf("expected ServerName persisted, got %q", disk.ServerName)
	}
	if disk.HTTPAddr != ":8096" || disk.WebDir != "/persisted/web" || disk.TMDBApiKey != "persisted-tmdb" ||
		!disk.EnableDLNA || !disk.EnableQuickConnect ||
		len(disk.AllowedOrigins) != 1 || disk.AllowedOrigins[0] != "https://persisted.example" ||
		len(disk.TrustedProxies) != 1 || disk.TrustedProxies[0] != "127.0.0.1" ||
		disk.FFmpegPath != "/persisted/ffmpeg" || disk.FFprobePath != "/persisted/ffprobe" {
		t.Fatalf("env overrides leaked into ambra.json: %+v", &disk)
	}

	// Explicitly updating overridden fields via Update() clears their env flags so Save() persists them.
	cfg.Update(func(c *Config) {
		c.HTTPAddr = ":7777"
		c.WebDir = "/updated/web"
		c.TMDBApiKey = "updated-tmdb"
		c.EnableDLNA = true
		c.EnableQuickConnect = true
		c.AllowedOrigins = []string{"https://updated.example", "https://second.example"}
		c.TrustedProxies = []string{"192.168.1.1"}
		c.FFmpegPath = "/updated/ffmpeg"
		c.FFprobePath = "/updated/ffprobe"
	})
	if err := cfg.Save(); err != nil {
		t.Fatalf("Save after explicit Update: %v", err)
	}

	raw2, err := os.ReadFile(filepath.Join(dir, "config", "ambra.json"))
	if err != nil {
		t.Fatal(err)
	}
	var disk2 Config
	if err := json.Unmarshal(raw2, &disk2); err != nil {
		t.Fatal(err)
	}
	if disk2.HTTPAddr != ":7777" || disk2.WebDir != "/updated/web" || disk2.TMDBApiKey != "updated-tmdb" ||
		!disk2.EnableDLNA || !disk2.EnableQuickConnect ||
		len(disk2.AllowedOrigins) != 2 || disk2.TrustedProxies[0] != "192.168.1.1" ||
		disk2.FFmpegPath != "/updated/ffmpeg" || disk2.FFprobePath != "/updated/ffprobe" {
		t.Fatalf("explicit Update values not persisted: %+v", &disk2)
	}
}

func TestLoadEmptyServerIDPersistsGeneratedID(t *testing.T) {
	dir := t.TempDir()
	cfgDir := filepath.Join(dir, "config")
	if err := os.MkdirAll(cfgDir, 0o700); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(cfgDir, "ambra.json")
	if err := os.WriteFile(cfgPath, []byte(`{"server_id":"","server_name":"PersistID"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg1, err := Load(dir)
	if err != nil {
		t.Fatalf("first Load: %v", err)
	}
	if cfg1.ServerID == "" {
		t.Fatal("expected generated ServerID")
	}

	// Reload without calling Save() explicitly: the same ServerID must be persisted on disk.
	cfg2, err := Load(dir)
	if err != nil {
		t.Fatalf("second Load: %v", err)
	}
	if cfg2.ServerID != cfg1.ServerID {
		t.Fatalf("expected persisted ServerID %q across reloads, got %q", cfg1.ServerID, cfg2.ServerID)
	}

	// Verify stringSlicesEqual edge cases
	if stringSlicesEqual([]string{"a"}, []string{"a", "b"}) {
		t.Fatal("different lengths must not be equal")
	}
	if stringSlicesEqual([]string{"a"}, []string{"b"}) {
		t.Fatal("different elements must not be equal")
	}
	if !stringSlicesEqual([]string{"a", "b"}, []string{"a", "b"}) {
		t.Fatal("identical slices must be equal")
	}
}
