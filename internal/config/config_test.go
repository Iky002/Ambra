package config

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sync"
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
	if cfg.EnableDLNA || cfg.EnableLoginUserList || !cfg.EnableQuickConnect {
		t.Fatalf("unexpected security defaults: DLNA=%v LoginUserList=%v QC=%v", cfg.EnableDLNA, cfg.EnableLoginUserList, cfg.EnableQuickConnect)
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
	if cfg2.ChapterCacheDir() != filepath.Join("/custom/dir", "cache", "chapters") {
		t.Fatalf("unexpected ChapterCacheDir: %q", cfg2.ChapterCacheDir())
	}
	if cfg2.BifCacheDir() != filepath.Join("/custom/dir", "cache", "bif") {
		t.Fatalf("unexpected BifCacheDir: %q", cfg2.BifCacheDir())
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

func TestLoadEmptyDataDirAndAllEnvOverrides(t *testing.T) {
	// Exercise Load("") in an isolated working directory so ./data is created safely.
	origWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	tmpWD := t.TempDir()
	if err := os.Chdir(tmpWD); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(origWD) })

	t.Setenv("AMBRA_ENABLE_DLNA", "false")
	t.Setenv("AMBRA_ENABLE_QUICK_CONNECT", "0")
	t.Setenv("AMBRA_ENABLE_LOGIN_USER_LIST", "true")
	t.Setenv("AMBRA_ALLOWED_ORIGINS", " https://a.example.com , , https://b.example.com ")
	t.Setenv("AMBRA_TRUSTED_PROXIES", "10.0.0.1, 172.16.0.0/12, ")
	t.Setenv("AMBRA_TMDB_API_KEY", "env-only-secret")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load(\"\") failed: %v", err)
	}
	if cfg.DataDir != "./data" {
		t.Fatalf("expected ./data, got %q", cfg.DataDir)
	}
	if cfg.EnableDLNA {
		t.Fatal("expected EnableDLNA=false from env")
	}
	if cfg.EnableQuickConnect {
		t.Fatal("expected EnableQuickConnect=false from env")
	}
	if !cfg.EnableLoginUserList {
		t.Fatal("expected EnableLoginUserList=true from env")
	}
	if len(cfg.AllowedOrigins) != 2 || cfg.AllowedOrigins[0] != "https://a.example.com" || cfg.AllowedOrigins[1] != "https://b.example.com" {
		t.Fatalf("unexpected AllowedOrigins: %#v", cfg.AllowedOrigins)
	}
	if len(cfg.TrustedProxies) != 2 || cfg.TrustedProxies[0] != "10.0.0.1" || cfg.TrustedProxies[1] != "172.16.0.0/12" {
		t.Fatalf("unexpected TrustedProxies: %#v", cfg.TrustedProxies)
	}
	if cfg.TMDBApiKey != "env-only-secret" {
		t.Fatalf("expected TMDBApiKey in memory, got %q", cfg.TMDBApiKey)
	}

	// Save MUST NOT persist env-provided TMDBApiKey or ephemeral AMBRA_ENABLE_LOGIN_USER_LIST to disk.
	if err := cfg.Save(); err != nil {
		t.Fatalf("Save failed: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(tmpWD, "data", "config", "ambra.json"))
	if err != nil {
		t.Fatalf("read saved ambra.json: %v", err)
	}
	if string(raw) == "" {
		t.Fatal("expected non-empty saved ambra.json")
	}
	if got := string(raw); filepath.Base(cfg.DBPath()) != "ambra.db" || containsString(got, "env-only-secret") || containsString(got, `"enable_login_user_list": true`) {
		t.Fatalf("env overrides must not be persisted to disk: %s", got)
	}

	// Verify positive/negative boolean env overrides on top of persisted baseline values.
	t.Setenv("AMBRA_ENABLE_DLNA", "yes")
	t.Setenv("AMBRA_ENABLE_QUICK_CONNECT", "on")
	t.Setenv("AMBRA_ENABLE_LOGIN_USER_LIST", "false")
	t.Setenv("AMBRA_TMDB_API_KEY", "")
	cfgTrue, err := Load("")
	if err != nil {
		t.Fatalf("reload Load(\"\") failed: %v", err)
	}
	if !cfgTrue.EnableDLNA || !cfgTrue.EnableQuickConnect || cfgTrue.EnableLoginUserList {
		t.Fatalf("expected DLNA=true QC=true LoginUserList=false, got DLNA=%v QC=%v LoginUserList=%v", cfgTrue.EnableDLNA, cfgTrue.EnableQuickConnect, cfgTrue.EnableLoginUserList)
	}
	cfgTrue.Update(func(c *Config) {
		c.EnableLoginUserList = true
	})
	if err := cfgTrue.Save(); err != nil {
		t.Fatalf("Save after Update EnableLoginUserList: %v", err)
	}
	if cfgTrue.TMDBApiKey != "" {
		t.Fatalf("expected empty TMDBApiKey after reloading without env, got %q", cfgTrue.TMDBApiKey)
	}

	// Check splitCSVEnv edge case: only commas/spaces.
	if parts := splitCSVEnv(" , , "); len(parts) != 0 {
		t.Fatalf("expected empty slice from blank CSV, got %#v", parts)
	}
}

func TestUpdateSnapshotAndAtomicSaveErrors(t *testing.T) {
	// Nil safety
	var nilCfg *Config
	nilCfg.Update(func(c *Config) { c.ServerName = "nope" })
	if snap := nilCfg.Snapshot(); snap.ServerName != "" {
		t.Fatalf("expected zero snapshot for nil Config, got %+v", snap)
	}

	dir := t.TempDir()
	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Update(nil) // nil fn no-op

	cfg.Update(func(c *Config) {
		c.ServerName = "Updated Server"
		c.TMDBApiKey = "saved-key"
		c.AllowedOrigins = []string{"https://one.example"}
		c.TrustedProxies = []string{"127.0.0.1"}
	})

	snap := cfg.Snapshot()
	if snap.ServerName != "Updated Server" || snap.TMDBApiKey != "saved-key" {
		t.Fatalf("unexpected snapshot: %+v", snap)
	}
	// Mutating snapshot slices must not affect original Config.
	snap.AllowedOrigins[0] = "https://mutated.example"
	snap.TrustedProxies[0] = "9.9.9.9"
	snap2 := cfg.Snapshot()
	if snap2.AllowedOrigins[0] != "https://one.example" || snap2.TrustedProxies[0] != "127.0.0.1" {
		t.Fatalf("snapshot slices must be deep-copied, got %+v", snap2)
	}

	// Atomic Save writes mode 0600 and leaves no temp files behind.
	if err := cfg.Save(); err != nil {
		t.Fatalf("Save failed: %v", err)
	}
	cfgPath := filepath.Join(dir, "config", "ambra.json")
	st, err := os.Stat(cfgPath)
	if err != nil {
		t.Fatalf("stat saved config: %v", err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("expected 0600 permissions on ambra.json, got %o", st.Mode().Perm())
	}
	entries, err := os.ReadDir(filepath.Join(dir, "config"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "ambra.json" {
		t.Fatalf("expected only ambra.json in config dir, got %v", entries)
	}

	// Save error: DataDir is a regular file so MkdirAll fails.
	fileAsDir := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(fileAsDir, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	badCfg := Default(fileAsDir)
	if err := badCfg.Save(); err == nil {
		t.Fatal("expected Save error when DataDir is a file")
	}

	// Save error: target ambra.json is a non-empty directory so Rename fails and temp file is cleaned up.
	dirRename := t.TempDir()
	targetDir := filepath.Join(dirRename, "config", "ambra.json")
	if err := os.MkdirAll(filepath.Join(targetDir, "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	renameFailCfg := Default(dirRename)
	if err := renameFailCfg.Save(); err == nil {
		t.Fatal("expected Save rename error when ambra.json is a directory")
	}

	// Save error: config dir is read-only so CreateTemp fails (also covers Load initial Save error).
	roDir := t.TempDir()
	roCfgDir := filepath.Join(roDir, "config")
	if err := os.MkdirAll(roCfgDir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(roCfgDir, 0o700) })
	roCfg := Default(roDir)
	if err := roCfg.Save(); err == nil {
		t.Fatal("expected Save error when config dir is read-only")
	}
	if _, err := Load(roDir); err == nil {
		t.Fatal("expected Load error when initial Save fails in read-only config dir")
	}

	// Save error: json.MarshalIndent fails when PlayedThreshold is NaN.
	nanCfg := Default(t.TempDir())
	nanCfg.PlayedThreshold = math.NaN()
	if err := nanCfg.Save(); err == nil {
		t.Fatal("expected Save error for NaN PlayedThreshold")
	}
}

func TestUpdateTMDBClearsEnvFlagAndConcurrentSave(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("AMBRA_TMDB_API_KEY", "env-initial-secret")

	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if err := cfg.Save(); err != nil {
		t.Fatalf("initial Save: %v", err)
	}
	cfgPath := filepath.Join(dir, "config", "ambra.json")
	raw, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read ambra.json: %v", err)
	}
	if containsString(string(raw), "env-initial-secret") {
		t.Fatalf("env TMDB key must not be written before explicit Update: %s", string(raw))
	}

	// Explicitly updating TMDBApiKey via Update clears envTMDBKeySet so Save persists the new key.
	cfg.Update(func(c *Config) {
		c.TMDBApiKey = "admin-updated-tmdb-key"
	})
	if err := cfg.Save(); err != nil {
		t.Fatalf("Save after Update: %v", err)
	}
	raw, err = os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read ambra.json after update: %v", err)
	}
	if !containsString(string(raw), "admin-updated-tmdb-key") {
		t.Fatalf("expected updated TMDB key persisted to disk, got: %s", string(raw))
	}

	// Concurrent Update + Save + Snapshot must not race or corrupt ambra.json.
	var wg sync.WaitGroup
	errCh := make(chan error, 32)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			for j := 0; j < 5; j++ {
				cfg.Update(func(c *Config) {
					c.ServerName = fmt.Sprintf("Server-%d-%d", idx, j)
				})
				_ = cfg.Snapshot()
				if err := cfg.Save(); err != nil {
					errCh <- err
					return
				}
			}
		}(i)
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Fatalf("concurrent Save failed: %v", err)
	}

	t.Setenv("AMBRA_TMDB_API_KEY", "")
	reloaded, err := Load(dir)
	if err != nil {
		t.Fatalf("reload after concurrent Save: %v", err)
	}
	if reloaded.TMDBApiKey != "admin-updated-tmdb-key" {
		t.Fatalf("expected reloaded TMDBApiKey=admin-updated-tmdb-key, got %q", reloaded.TMDBApiKey)
	}
}

func TestEffectiveTMDBApiKeyFallbackAndOverride(t *testing.T) {
	t.Setenv("AMBRA_TMDB_API_KEY", "")

	var nilCfg *Config
	if got := nilCfg.EffectiveTMDBApiKey(); got != DefaultTMDBApiKey {
		t.Fatalf("expected nil Config EffectiveTMDBApiKey=%q, got %q", DefaultTMDBApiKey, got)
	}

	dir := t.TempDir()
	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.TMDBApiKey != "" {
		t.Fatalf("expected empty user TMDBApiKey by default, got %q", cfg.TMDBApiKey)
	}
	if got := cfg.EffectiveTMDBApiKey(); got != DefaultTMDBApiKey {
		t.Fatalf("expected fallback DefaultTMDBApiKey=%q, got %q", DefaultTMDBApiKey, got)
	}

	// Default built-in key must not be persisted to ambra.json when user hasn't configured one.
	if err := cfg.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "config", "ambra.json"))
	if err != nil {
		t.Fatalf("read ambra.json: %v", err)
	}
	if containsString(string(raw), DefaultTMDBApiKey) {
		t.Fatalf("built-in DefaultTMDBApiKey must not be written to ambra.json: %s", string(raw))
	}

	// User custom key overrides built-in fallback.
	cfg.Update(func(c *Config) {
		c.TMDBApiKey = "  user-custom-key  "
	})
	if got := cfg.EffectiveTMDBApiKey(); got != "user-custom-key" {
		t.Fatalf("expected custom key override, got %q", got)
	}

	// Clearing user custom key falls back to built-in default again.
	cfg.Update(func(c *Config) {
		c.TMDBApiKey = ""
	})
	if got := cfg.EffectiveTMDBApiKey(); got != DefaultTMDBApiKey {
		t.Fatalf("expected fallback to DefaultTMDBApiKey after clear, got %q", got)
	}
}

func containsString(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
