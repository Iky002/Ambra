package nfo

import "testing"

func TestAuditV0109_L28_PascalCaseEnabled(t *testing.T) {
	p := New()
	if !p.Manifest().Enabled {
		t.Fatal("expected enabled by default")
	}
	if err := p.Configure(map[string]any{"Enabled": false}); err != nil {
		t.Fatalf("Configure Enabled=false: %v", err)
	}
	if p.Manifest().Enabled {
		t.Fatal("expected Enabled=false via PascalCase")
	}
	if err := p.Configure(map[string]any{"Enabled": true}); err != nil {
		t.Fatalf("Configure Enabled=true: %v", err)
	}
	if !p.Manifest().Enabled {
		t.Fatal("expected Enabled=true via PascalCase")
	}
}
