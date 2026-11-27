package ops

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ambra-media/ambra/internal/api/middleware"
	"github.com/ambra-media/ambra/internal/pkg/eventbus"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestOpsV0109AuditH12M23M28L15L16(t *testing.T) {
	h, _, st, _, _ := setupOpsTest(t)

	// 1. H-12: Close() cancels in-flight webhook requests immediately instead of blocking for minutes
	started := make(chan struct{}, 1)
	unblock := make(chan struct{})
	slowServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case started <- struct{}{}:
		default:
		}
		select {
		case <-r.Context().Done():
		case <-unblock:
		}
	}))
	defer func() {
		close(unblock)
		slowServer.Close()
	}()

	h.SetHTTPClient(slowServer.Client())
	h.mu.Lock()
	h.state.Webhooks = []WebhookConfig{{Id: "wh-slow", Name: "Slow", Url: slowServer.URL, Events: []string{"*"}, Enabled: true}}
	h.mu.Unlock()
	for i := 0; i < 10; i++ {
		h.dispatchWebhooks(context.Background(), eventbus.Event{Type: eventbus.EventPlaybackStart})
	}
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("webhook worker did not start request")
	}
	closeStart := time.Now()
	h.Close()
	if elapsed := time.Since(closeStart); elapsed > 2*time.Second {
		t.Fatalf("H-12: Close() took too long (%v), expected fast cancellation", elapsed)
	}

	// 2. M-23: verifySQLiteIntegrity handles paths containing '#' and '?' without double URL encoding
	specialDir := filepath.Join(t.TempDir(), "dir#hash?q")
	if err := os.MkdirAll(specialDir, 0o700); err != nil {
		t.Fatal(err)
	}
	specialDBPath := filepath.Join(specialDir, "backup#1?v=2.db")
	tmpStore, err := sqlite.Open(specialDBPath)
	if err != nil {
		t.Fatalf("open special sqlite db: %v", err)
	}
	_ = tmpStore.Close()
	if err := verifySQLiteIntegrity(context.Background(), specialDBPath); err != nil {
		t.Fatalf("M-23: verifySQLiteIntegrity failed on special-char path %q: %v", specialDBPath, err)
	}

	// 3. L-15: Updating webhook with changed host and empty Secret clears old Secret
	h2, mux2, st2, _, _ := setupOpsTest(t)
	defer h2.Close()
	recWh := doReq(t, mux2, http.MethodPost, "/Webhooks", map[string]any{
		"Id":      "wh-host-test",
		"Name":    "Hook",
		"Url":     "https://93.184.216.34/a",
		"Secret":  "top-secret-token",
		"Enabled": true,
	})
	if recWh.Code != http.StatusOK {
		t.Fatalf("create webhook failed: %d %s", recWh.Code, recWh.Body.String())
	}
	// Same scheme + host + path (even with query), empty secret -> preserves secret
	recSameOriginPath := doReq(t, mux2, http.MethodPost, "/Webhooks", map[string]any{
		"Id":      "wh-host-test",
		"Name":    "Hook",
		"Url":     "https://93.184.216.34/a?keep=1",
		"Enabled": true,
	})
	if recSameOriginPath.Code != http.StatusOK {
		t.Fatalf("update same-origin-path webhook failed: %d", recSameOriginPath.Code)
	}
	h2.mu.RLock()
	sameSecret := h2.state.Webhooks[0].Secret
	h2.mu.RUnlock()
	if sameSecret != "top-secret-token" {
		t.Fatalf("expected secret preserved on identical scheme+host+path update, got %q", sameSecret)
	}
	// Changed path, empty secret -> clears secret (M-04)
	recDiffPath := doReq(t, mux2, http.MethodPost, "/Webhooks", map[string]any{
		"Id":      "wh-host-test",
		"Name":    "Hook",
		"Url":     "https://93.184.216.34/b",
		"Enabled": true,
	})
	if recDiffPath.Code != http.StatusOK {
		t.Fatalf("update diff-path webhook failed: %d", recDiffPath.Code)
	}
	h2.mu.RLock()
	pathClearedSecret := h2.state.Webhooks[0].Secret
	h2.mu.RUnlock()
	if pathClearedSecret != "" {
		t.Fatalf("M-04: expected secret cleared on path change, got %q", pathClearedSecret)
	}
	// Re-arm secret, then verify https -> http downgrade clears secret
	_ = doReq(t, mux2, http.MethodPost, "/Webhooks", map[string]any{
		"Id":      "wh-host-test",
		"Name":    "Hook",
		"Url":     "https://93.184.216.34/a",
		"Secret":  "top-secret-token-2",
		"Enabled": true,
	})
	recDowngrade := doReq(t, mux2, http.MethodPost, "/Webhooks", map[string]any{
		"Id":      "wh-host-test",
		"Name":    "Hook",
		"Url":     "http://93.184.216.34/a",
		"Enabled": true,
	})
	if recDowngrade.Code != http.StatusOK {
		t.Fatalf("update scheme-downgrade webhook failed: %d", recDowngrade.Code)
	}
	h2.mu.RLock()
	downgradeSecret := h2.state.Webhooks[0].Secret
	h2.mu.RUnlock()
	if downgradeSecret != "" {
		t.Fatalf("M-04: expected secret cleared on https->http downgrade, got %q", downgradeSecret)
	}
	// Re-arm secret, then verify different host clears secret
	_ = doReq(t, mux2, http.MethodPost, "/Webhooks", map[string]any{
		"Id":      "wh-host-test",
		"Name":    "Hook",
		"Url":     "https://93.184.216.34/a",
		"Secret":  "top-secret-token-3",
		"Enabled": true,
	})
	recDiffHost := doReq(t, mux2, http.MethodPost, "/Webhooks", map[string]any{
		"Id":      "wh-host-test",
		"Name":    "Hook",
		"Url":     "https://93.184.216.35/a",
		"Enabled": true,
	})
	if recDiffHost.Code != http.StatusOK {
		t.Fatalf("update diff-host webhook failed: %d", recDiffHost.Code)
	}
	h2.mu.RLock()
	clearedSecret := h2.state.Webhooks[0].Secret
	h2.mu.RUnlock()
	if clearedSecret != "" {
		t.Fatalf("L-15: expected secret cleared on host change, got %q", clearedSecret)
	}

	// 4. L-16: Private party non-member GET returns 404 (unified with missing ID) & missing-ID Join is rate-limited
	host := &middleware.AuthContext{User: &sqlite.UserRecord{ID: "host-l16", Name: "host", Policy: sqlite.DefaultUserPolicy(false)}}
	outsider := &middleware.AuthContext{User: &sqlite.UserRecord{ID: "out-l16", Name: "out", Policy: sqlite.DefaultUserPolicy(false)}}
	recParty := doAuthReq(t, mux2, host, http.MethodPost, "/Parties", map[string]any{
		"Name":       "Secret L16",
		"IsPublic":   false,
		"InviteCode": "code123",
	})
	if recParty.Code != http.StatusOK {
		t.Fatalf("create party failed: %d", recParty.Code)
	}
	var party Party
	_ = json.Unmarshal(recParty.Body.Bytes(), &party)

	rPriv := doAuthReq(t, mux2, outsider, http.MethodGet, "/Parties/"+party.Id, nil)
	rMiss := doAuthReq(t, mux2, outsider, http.MethodGet, "/Parties/nonexistent-id", nil)
	if rPriv.Code != http.StatusNotFound || rMiss.Code != http.StatusNotFound {
		t.Fatalf("L-16: expected unified 404 for private party (%d) and missing party (%d)", rPriv.Code, rMiss.Code)
	}

	for i := 0; i < maxPartyJoinFails; i++ {
		r := doAuthReq(t, mux2, outsider, http.MethodPost, fmt.Sprintf("/Parties/missing-%d/Join", i), map[string]any{"InviteCode": "x"})
		if r.Code != http.StatusNotFound {
			t.Fatalf("missing join attempt %d expected 404, got %d", i, r.Code)
		}
	}
	if r := doAuthReq(t, mux2, outsider, http.MethodPost, "/Parties/missing-final/Join", map[string]any{"InviteCode": "x"}); r.Code != http.StatusTooManyRequests {
		t.Fatalf("L-16: expected 429 rate-limiting missing-ID party joins, got %d", r.Code)
	}

	// 5. L-16: Installations capped at maxInstallations (100), evicting oldest
	for i := 0; i < maxInstallations+15; i++ {
		r := doReq(t, mux2, http.MethodPost, "/Packages/Installed/ambra-tmdb", nil)
		if r.Code != http.StatusOK {
			t.Fatalf("install package #%d failed: %d", i, r.Code)
		}
	}
	h2.mu.RLock()
	instCount := len(h2.state.Installations)
	h2.mu.RUnlock()
	if instCount != maxInstallations {
		t.Fatalf("L-16: expected installations capped at %d, got %d", maxInstallations, instCount)
	}

	// 6. M-28: Rollback in-memory state and return 500 when saveStateLocked fails
	_ = st2.Close()
	if r := doReq(t, mux2, http.MethodPost, "/Webhooks", map[string]any{"Id": "wh-fail", "Url": "https://93.184.216.34/fail"}); r.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 on webhook create DB error, got %d", r.Code)
	}
	h2.mu.RLock()
	for _, wh := range h2.state.Webhooks {
		if wh.Id == "wh-fail" {
			h2.mu.RUnlock()
			t.Fatal("M-28: failed webhook create was not rolled back")
		}
	}
	h2.mu.RUnlock()

	if r := doReq(t, mux2, http.MethodDelete, "/Webhooks/wh-host-test", nil); r.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 on webhook delete DB error, got %d", r.Code)
	}
	h2.mu.RLock()
	if len(h2.state.Webhooks) != 1 || h2.state.Webhooks[0].Id != "wh-host-test" {
		h2.mu.RUnlock()
		t.Fatal("M-28: failed webhook delete was not rolled back")
	}
	h2.mu.RUnlock()

	if r := doAuthReq(t, mux2, host, http.MethodPost, "/Parties", map[string]any{"Name": "FailParty"}); r.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 on party create DB error, got %d", r.Code)
	}
	if r := doAuthReq(t, mux2, host, http.MethodPost, "/Parties/"+party.Id+"/State", map[string]any{"ItemId": "item-fail"}); r.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 on party state DB error, got %d", r.Code)
	}
	if r := doAuthReq(t, mux2, host, http.MethodDelete, "/Parties/"+party.Id, nil); r.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 on party delete DB error, got %d", r.Code)
	}
	h2.mu.RLock()
	if len(h2.state.Parties) != 1 || h2.state.Parties[0].State.ItemId == "item-fail" {
		h2.mu.RUnlock()
		t.Fatalf("M-28: party mutations were not rolled back on DB failure: %+v", h2.state.Parties)
	}
	h2.mu.RUnlock()
	_ = st
}
