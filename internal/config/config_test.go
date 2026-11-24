package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefault(t *testing.T) {
	cfg := Default("")
	if cfg.DataDir != "./data" {
		t.Fatalf("expected default dataDir ./data, got %q", cfg.DataDir)
	}
	if cfg.ServerID == "" {
		t.Fatal("expected non-empty ServerID")
	}
	if cfg.HTTPAddr != ":8096" || cfg.FFmpegPath != "ffmpeg" || cfg.FFprobePath != "ffprobe" {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
	if cfg.TMDBLanguage != "zh-CN" || !cfg.EnableSTRM302 || cfg.PlayedThreshold != 0.90 {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}

	cfg2 := Default("/custom/dir")
	if cfg2.DataDir != "/custom/dir" {
		t.Fatalf("expected /custom/dir, got %q", cfg2.DataDir)
	}
	if cfg2.DBPath() != filepath.Join("/custom/dir", "ambra.db") {
		t.Fatalf("unexpected DBPath: %q", cfg2.DBPath())
	}
	if cfg2.ImageCacheDir() != filepath.Join("/custom/dir", "cache", "images") {
		t.Fatalf("unexpected ImageCacheDir: %q", cfg2.ImageCacheDir())
	}
	if cfg2.SubtitleCacheDir() != filepath.Join("/custom/dir", "cache", "subtitles") {
		t.Fatalf("unexpected SubtitleCacheDir: %q", cfg2.SubtitleCacheDir())
	}
	if cfg2.TranscodeTempDir() != filepath.Join("/custom/dir", "cache", "transcoding-temp") {
		t.Fatalf("unexpected TranscodeTempDir: %q", cfg2.TranscodeTempDir())
	}
}

func TestLoadCreateAndReloadAndEnvOverrides(t *testing.T) {
	dir := t.TempDir()

	t.Setenv("AMBRA_HTTP_ADDR", ":9999")
	t.Setenv("AMBRA_WEB_DIR", "/tmp/web")
	t.Setenv("AMBRA_TMDB_API_KEY", "secret-key")

	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if cfg.HTTPAddr != ":9999" || cfg.WebDir != "/tmp/web" || cfg.TMDBApiKey != "secret-key" {
		t.Fatalf("env overrides not applied: %+v", cfg)
	}
	// Verify file was created
	cfgFile := filepath.Join(dir, "config", "ambra.json")
	if _, err := os.Stat(cfgFile); err != nil {
		t.Fatalf("expected config file to exist: %v", err)
	}

	// Modify and save, then reload
	cfg.ServerName = "Custom Name"
	cfg.IsStartupWizardCompleted = true
	if err := cfg.Save(); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	t.Setenv("AMBRA_HTTP_ADDR", "")
	t.Setenv("AMBRA_WEB_DIR", "")
	t.Setenv("AMBRA_TMDB_API_KEY", "")

	reloaded, err := Load(dir)
	if err != nil {
		t.Fatalf("Reload failed: %v", err)
	}
	if reloaded.ServerName != "Custom Name" || !reloaded.IsStartupWizardCompleted {
		t.Fatalf("reloaded config mismatch: %+v", reloaded)
	}
	if reloaded.ServerID != cfg.ServerID {
		t.Fatalf("expected ServerID %q, got %q", cfg.ServerID, reloaded.ServerID)
	}
}

func TestLoadEmptyServerIDAndErrors(t *testing.T) {
	// Empty server_id in existing JSON should generate a new ServerID.
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "config"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(dir, "config", "ambra.json")
	if err := os.WriteFile(cfgPath, []byte(`{"server_id":"","server_name":"NoID"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load with empty server_id failed: %v", err)
	}
	if cfg.ServerID == "" {
		t.Fatal("expected generated ServerID when empty in file")
	}
	if cfg.ServerName != "NoID" {
		t.Fatalf("expected ServerName NoID, got %q", cfg.ServerName)
	}

	// Malformed JSON should return error.
	if err := os.WriteFile(cfgPath, []byte(`{invalid-json`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(dir); err == nil {
		t.Fatal("expected error for invalid JSON")
	}

	// Read error when ambra.json is a directory.
	dir2 := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir2, "config", "ambra.json"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(dir2); err == nil {
		t.Fatal("expected error when ambra.json is a directory")
	}

	// MkdirAll error when dataDir is a regular file.
	filePath := filepath.Join(t.TempDir(), "notadir")
	if err := os.WriteFile(filePath, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(filePath); err == nil {
		t.Fatal("expected error when dataDir is a file")
	}
}

func TestFFmpegEnvOverridesAndSiblingAutoDetect(t *testing.T) {
	origExec := executablePathFn
	t.Cleanup(func() { executablePathFn = origExec })

	binDir := t.TempDir()
	fakeExe := filepath.Join(binDir, "ambra")
	executablePathFn = func() (string, error) { return fakeExe, nil }

	// 1. No sibling binaries -> falls back to default names
	dataDir := t.TempDir()
	cfg, err := Load(dataDir)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if cfg.FFmpegPath != "ffmpeg" || cfg.FFprobePath != "ffprobe" {
		t.Fatalf("expected default ffmpeg/ffprobe, got %q / %q", cfg.FFmpegPath, cfg.FFprobePath)
	}

	// 2. Sibling ffmpeg/ffprobe next to executable -> auto-detected
	siblingFFmpeg := filepath.Join(binDir, "ffmpeg")
	siblingFFprobe := filepath.Join(binDir, "ffprobe")
	if err := os.WriteFile(siblingFFmpeg, []byte("bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(siblingFFprobe, []byte("bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfgDetected, err := Load(t.TempDir())
	if err != nil {
		t.Fatalf("Load with sibling binaries failed: %v", err)
	}
	if cfgDetected.FFmpegPath != siblingFFmpeg || cfgDetected.FFprobePath != siblingFFprobe {
		t.Fatalf("expected sibling binaries %q / %q, got %q / %q",
			siblingFFmpeg, siblingFFprobe, cfgDetected.FFmpegPath, cfgDetected.FFprobePath)
	}

	// 3. Env overrides take precedence over sibling auto-detect
	t.Setenv("AMBRA_FFMPEG_PATH", "/custom/bin/ffmpeg")
	t.Setenv("AMBRA_FFPROBE_PATH", "/custom/bin/ffprobe")
	cfgEnv, err := Load(t.TempDir())
	if err != nil {
		t.Fatalf("Load with ffmpeg env override failed: %v", err)
	}
	if cfgEnv.FFmpegPath != "/custom/bin/ffmpeg" || cfgEnv.FFprobePath != "/custom/bin/ffprobe" {
		t.Fatalf("expected env overrides, got %q / %q", cfgEnv.FFmpegPath, cfgEnv.FFprobePath)
	}

	// 4. Explicit custom path in config is preserved over sibling auto-detect
	customDataDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(customDataDir, "config"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(customDataDir, "config", "ambra.json"),
		[]byte(`{"ffmpeg_path":"/opt/ffmpeg","ffprobe_path":"/opt/ffprobe"}`),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AMBRA_FFMPEG_PATH", "")
	t.Setenv("AMBRA_FFPROBE_PATH", "")
	cfgCustom, err := Load(customDataDir)
	if err != nil {
		t.Fatalf("Load with custom config paths failed: %v", err)
	}
	if cfgCustom.FFmpegPath != "/opt/ffmpeg" || cfgCustom.FFprobePath != "/opt/ffprobe" {
		t.Fatalf("expected custom paths preserved, got %q / %q", cfgCustom.FFmpegPath, cfgCustom.FFprobePath)
	}

	// 5. Managed <DataDir>/bin/ binaries are detected when no sibling exists
	emptyExeDir := t.TempDir()
	executablePathFn = func() (string, error) { return filepath.Join(emptyExeDir, "ambra"), nil }
	managedDataDir := t.TempDir()
	managedBinDir := filepath.Join(managedDataDir, "bin")
	if err := os.MkdirAll(managedBinDir, 0o755); err != nil {
		t.Fatal(err)
	}
	managedFFmpeg := filepath.Join(managedBinDir, "ffmpeg")
	managedFFprobe := filepath.Join(managedBinDir, "ffprobe")
	if err := os.WriteFile(managedFFmpeg, []byte("bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(managedFFprobe, []byte("bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfgManaged, err := Load(managedDataDir)
	if err != nil {
		t.Fatalf("Load with managed bin failed: %v", err)
	}
	if cfgManaged.FFmpegPath != managedFFmpeg || cfgManaged.FFprobePath != managedFFprobe {
		t.Fatalf("expected managed bin %q / %q, got %q / %q",
			managedFFmpeg, managedFFprobe, cfgManaged.FFmpegPath, cfgManaged.FFprobePath)
	}

	// 6. executablePathFn failure falls back cleanly to default names
	executablePathFn = func() (string, error) { return "", os.ErrNotExist }
	cfgFallback, err := Load(t.TempDir())
	if err != nil {
		t.Fatalf("Load with executable error failed: %v", err)
	}
	if cfgFallback.FFmpegPath != "ffmpeg" || cfgFallback.FFprobePath != "ffprobe" {
		t.Fatalf("expected fallback to default names, got %q / %q", cfgFallback.FFmpegPath, cfgFallback.FFprobePath)
	}
}
