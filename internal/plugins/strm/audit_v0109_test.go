package strm

import "testing"

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
