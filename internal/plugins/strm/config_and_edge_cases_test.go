package strm

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestAuditV0109_L28_PascalCaseConfigure(t *testing.T) {
	r := New(true)
	if !r.Manifest().Enabled || !r.enable302.Load() {
		t.Fatal("expected enabled=true and enable302=true initially")
	}
	if err := r.Configure(map[string]any{"Enabled": false, "Enable302": false}); err != nil {
		t.Fatalf("Configure PascalCase false: %v", err)
	}
	if r.Manifest().Enabled || r.enable302.Load() {
		t.Fatalf("expected enabled=false enable302=false, got enabled=%v enable302=%v", r.Manifest().Enabled, r.enable302.Load())
	}
	if err := r.Configure(map[string]any{"Enabled": true, "Enable302": true}); err != nil {
		t.Fatalf("Configure PascalCase true: %v", err)
	}
	if !r.Manifest().Enabled || !r.enable302.Load() {
		t.Fatalf("expected enabled=true enable302=true, got enabled=%v enable302=%v", r.Manifest().Enabled, r.enable302.Load())
	}
}

func TestStrmCovV0112_ConfigAndEdgeCases(t *testing.T) {
	r := New(true)

	def := r.DefaultConfig()
	if def["enabled"] != true || def["enable_302"] != true || def["priority"] != defaultPriority {
		t.Fatalf("unexpected DefaultConfig: %+v", def)
	}

	// Nil config is a no-op.
	if err := r.Configure(nil); err != nil {
		t.Fatalf("Configure(nil) failed: %v", err)
	}

	// Lowercase keys + int64 priority.
	if err := r.Configure(map[string]any{
		"enabled":    false,
		"enable_302": false,
		"priority":   int64(42),
	}); err != nil {
		t.Fatalf("Configure lowercase failed: %v", err)
	}
	m := r.Manifest()
	if m.Enabled || m.Priority != 42 {
		t.Fatalf("unexpected manifest after lowercase Configure: %+v", m)
	}

	// Disabled resolver returns (nil, nil) even for valid .strm path.
	dir := t.TempDir()
	validStrm := filepath.Join(dir, "valid.strm")
	if err := os.WriteFile(validStrm, []byte("\xef\xbb\xbf  https://93.184.216.34/video.mp4 \n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := r.ResolveStream(context.Background(), validStrm)
	if err != nil || res != nil {
		t.Fatalf("expected (nil, nil) when disabled, got (%+v, %v)", res, err)
	}

	// Uppercase keys + float64 priority, then int priority.
	if err := r.Configure(map[string]any{
		"Enabled":   true,
		"Enable302": true,
		"Priority":  float64(77),
	}); err != nil {
		t.Fatalf("Configure uppercase failed: %v", err)
	}
	if r.Manifest().Priority != 77 || !r.Manifest().Enabled {
		t.Fatalf("unexpected manifest after uppercase Configure: %+v", r.Manifest())
	}
	if err := r.Configure(map[string]any{"priority": 15}); err != nil {
		t.Fatal(err)
	}
	if r.Manifest().Priority != 15 {
		t.Fatalf("expected priority 15, got %d", r.Manifest().Priority)
	}

	// BOM-prefixed .strm resolves cleanly.
	res, err = r.ResolveStream(context.Background(), validStrm)
	if err != nil || res == nil || res.URL != "https://93.184.216.34/video.mp4" || !res.Is302 {
		t.Fatalf("unexpected BOM resolve result: res=%+v err=%v", res, err)
	}

	// Missing .strm file returns error.
	if _, err := r.ResolveStream(context.Background(), filepath.Join(dir, "missing.strm")); err == nil {
		t.Fatal("expected error for missing .strm file")
	}

	// Comment/blank-only .strm returns (nil, nil).
	emptyStrm := filepath.Join(dir, "empty.strm")
	if err := os.WriteFile(emptyStrm, []byte("# comment\n   \n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err = r.ResolveStream(context.Background(), emptyStrm)
	if err != nil || res != nil {
		t.Fatalf("expected (nil, nil) for comment-only .strm, got (%+v, %v)", res, err)
	}

	// Unsafe local/scheme target in .strm returns error.
	unsafeStrm := filepath.Join(dir, "unsafe.strm")
	if err := os.WriteFile(unsafeStrm, []byte("file:///etc/passwd\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := r.ResolveStream(context.Background(), unsafeStrm); err == nil {
		t.Fatal("expected error for unsafe file:// URL in .strm")
	}
}
