package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestRuntimeOverridesCLIAndAutoResolvedFFmpegPreserveDiskBaseline(t *testing.T) {
	origExec := executablePathFn
	t.Cleanup(func() { executablePathFn = origExec })

	// Nil safety for Mark* helpers.
	var nilCfg *Config
	nilCfg.MarkHTTPAddrRuntimeOverride()
	nilCfg.MarkWebDirRuntimeOverride()

	// Prepare sibling ffmpeg/ffprobe binaries next to fake executable so ffmpegbin.Resolve
	// auto-discovers absolute paths from default "ffmpeg"/"ffprobe" baseline.
	binDir := t.TempDir()
	fakeExe := filepath.Join(binDir, "ambra")
	executablePathFn = func() (string, error) { return fakeExe, nil }
	siblingFFmpeg := filepath.Join(binDir, "ffmpeg")
	siblingFFprobe := filepath.Join(binDir, "ffprobe")
	if err := os.WriteFile(siblingFFmpeg, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(siblingFFprobe, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	dataDir := t.TempDir()
	cfg, err := Load(dataDir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.FFmpegPath != siblingFFmpeg || cfg.FFprobePath != siblingFFprobe {
		t.Fatalf("expected auto-discovered sibling binaries in memory, got %q / %q", cfg.FFmpegPath, cfg.FFprobePath)
	}

	// Simulate CLI -addr / -web overrides.
	cfg.HTTPAddr = ":19096"
	cfg.MarkHTTPAddrRuntimeOverride()
	cfg.WebDir = "/tmp/cli-web"
	cfg.MarkWebDirRuntimeOverride()

	// Save after mutating an unrelated setting: CLI overrides and auto-discovered ffmpeg paths
	// MUST NOT overwrite disk baseline in ambra.json.
	cfg.Update(func(c *Config) {
		c.ServerName = "CLI Override Server"
	})
	if err := cfg.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	raw, err := os.ReadFile(filepath.Join(dataDir, "config", "ambra.json"))
	if err != nil {
		t.Fatal(err)
	}
	var disk Config
	if err := json.Unmarshal(raw, &disk); err != nil {
		t.Fatal(err)
	}
	if disk.ServerName != "CLI Override Server" {
		t.Fatalf("expected ServerName persisted, got %q", disk.ServerName)
	}
	if disk.HTTPAddr != ":8096" {
		t.Fatalf("CLI -addr leaked into ambra.json: got %q, want :8096", disk.HTTPAddr)
	}
	if disk.WebDir != "" {
		t.Fatalf("CLI -web leaked into ambra.json: got %q, want empty", disk.WebDir)
	}
	if disk.FFmpegPath != "ffmpeg" || disk.FFprobePath != "ffprobe" {
		t.Fatalf("auto-discovered ffmpeg paths leaked into ambra.json: got %q / %q", disk.FFmpegPath, disk.FFprobePath)
	}

	// Explicit Update() clears runtime overrides so Save() persists the new admin-configured values.
	cfg.Update(func(c *Config) {
		c.HTTPAddr = ":8888"
		c.WebDir = "/persisted/custom/web"
		c.FFmpegPath = "/opt/custom/ffmpeg"
		c.FFprobePath = "/opt/custom/ffprobe"
	})
	if err := cfg.Save(); err != nil {
		t.Fatalf("Save after explicit Update: %v", err)
	}
	raw2, err := os.ReadFile(filepath.Join(dataDir, "config", "ambra.json"))
	if err != nil {
		t.Fatal(err)
	}
	var disk2 Config
	if err := json.Unmarshal(raw2, &disk2); err != nil {
		t.Fatal(err)
	}
	if disk2.HTTPAddr != ":8888" || disk2.WebDir != "/persisted/custom/web" ||
		disk2.FFmpegPath != "/opt/custom/ffmpeg" || disk2.FFprobePath != "/opt/custom/ffprobe" {
		t.Fatalf("explicit Update values not persisted: %+v", &disk2)
	}
}
