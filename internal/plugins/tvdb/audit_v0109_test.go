package tvdb

import "testing"

func TestAuditV0109_M27_AtomicConfigure(t *testing.T) {
	p := New()
	if err := p.Configure(map[string]any{
		"enabled":  true,
		"api_key":  "orig-key",
		"pin":      "orig-pin",
		"language": "zh-CN",
		"priority": 25,
	}); err != nil {
		t.Fatalf("initial Configure: %v", err)
	}

	// Invalid base_url must not mutate enabled, api_key, pin, language, priority, or tokenEpoch.
	p.mu.RLock()
	origEpoch := p.tokenEpoch
	p.mu.RUnlock()

	err := p.Configure(map[string]any{
		"enabled":  false,
		"api_key":  "new-key",
		"pin":      "new-pin",
		"language": "en-US",
		"priority": 99,
		"base_url": "ftp://bad.example.com",
	})
	if err == nil {
		t.Fatal("expected error for invalid base_url")
	}

	snap := p.snapshot()
	p.mu.RLock()
	afterEpoch := p.tokenEpoch
	p.mu.RUnlock()
	if !snap.enabled || snap.apiKey != "orig-key" || snap.pin != "orig-pin" || snap.language != "zh-CN" || snap.priority != 25 || snap.baseURL != defaultBaseURL || afterEpoch != origEpoch {
		t.Fatalf("Configure partially mutated tvdb state on error: %+v epoch=%d->%d", snap, origEpoch, afterEpoch)
	}
}
