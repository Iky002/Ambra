package ext

import "testing"

func TestAuditV0109_M27_AtomicConfigure(t *testing.T) {
	p, err := NewHTTPPlugin(ExternalManifest{
		ID:        "ext.atomic",
		Name:      "Atomic Test",
		Endpoint:  "https://orig.example.com/api",
		APIKey:    "orig-key",
		Priority:  10,
		TimeoutMs: 5000,
	})
	if err != nil {
		t.Fatalf("NewHTTPPlugin: %v", err)
	}

	// Invalid TimeoutMs at the end must not leave APIKey, Endpoint, Enabled, or Priority mutated.
	err = p.Configure(map[string]any{
		"APIKey":    "new-key",
		"Endpoint":  "https://other.example.com/api",
		"Enabled":   false,
		"Priority":  99,
		"TimeoutMs": -1,
	})
	if err == nil {
		t.Fatal("expected error for negative TimeoutMs")
	}

	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.apiKey != "orig-key" || p.endpoint != "https://orig.example.com/api" || !p.enabled || p.priority != 10 {
		t.Fatalf("Configure partially mutated HTTPPlugin state: apiKey=%q endpoint=%q enabled=%v priority=%d",
			p.apiKey, p.endpoint, p.enabled, p.priority)
	}
}
