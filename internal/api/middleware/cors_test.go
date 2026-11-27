package middleware

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestH01_CORSWithDynamicOriginsHotReloadAndSameOrigin(t *testing.T) {
	var current atomic.Value
	current.Store([]string(nil))

	mw := CORSWithDynamicOrigins(func() []string {
		v, _ := current.Load().([]string)
		return v
	})
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// 1. Unconfigured: cross-origin rejected, same-origin (case-insensitive host) allowed.
	reqCross := httptest.NewRequest(http.MethodGet, "/System/Info", nil)
	reqCross.Host = "Media.Home.LAN:8096"
	reqCross.Header.Set("Origin", "https://attacker.example")
	recCross := httptest.NewRecorder()
	handler.ServeHTTP(recCross, reqCross)
	if got := recCross.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("expected cross-origin rejected when dynamic origins empty, got %q", got)
	}

	reqSame := httptest.NewRequest(http.MethodGet, "/System/Info", nil)
	reqSame.Host = "Media.Home.LAN:8096"
	reqSame.Header.Set("Origin", "http://media.home.lan:8096")
	recSame := httptest.NewRecorder()
	handler.ServeHTTP(recSame, reqSame)
	if got := recSame.Header().Get("Access-Control-Allow-Origin"); got != "http://media.home.lan:8096" {
		t.Fatalf("expected same-origin allowed case-insensitively, got %q", got)
	}

	// 2. Hot-reload specific origin -> immediately allowed with credentials, while other origins stay blocked.
	current.Store([]string{"https://portal.example.com"})
	reqPortal := httptest.NewRequest(http.MethodGet, "/System/Info", nil)
	reqPortal.Host = "media.home.lan:8096"
	reqPortal.Header.Set("Origin", "https://portal.example.com")
	recPortal := httptest.NewRecorder()
	handler.ServeHTTP(recPortal, reqPortal)
	if recPortal.Header().Get("Access-Control-Allow-Origin") != "https://portal.example.com" ||
		recPortal.Header().Get("Access-Control-Allow-Credentials") != "true" {
		t.Fatalf("expected hot-reloaded origin allowed with credentials, got ACAO=%q ACAC=%q",
			recPortal.Header().Get("Access-Control-Allow-Origin"),
			recPortal.Header().Get("Access-Control-Allow-Credentials"))
	}

	// 3. Hot-reload explicit wildcard "*" -> allows "*" without credentials.
	current.Store([]string{"*"})
	reqWild := httptest.NewRequest(http.MethodGet, "/System/Info", nil)
	reqWild.Host = "media.home.lan:8096"
	reqWild.Header.Set("Origin", "https://any.client.example")
	recWild := httptest.NewRecorder()
	handler.ServeHTTP(recWild, reqWild)
	if recWild.Header().Get("Access-Control-Allow-Origin") != "*" ||
		recWild.Header().Get("Access-Control-Allow-Credentials") != "" {
		t.Fatalf("expected wildcard ACAO=* without credentials, got ACAO=%q ACAC=%q",
			recWild.Header().Get("Access-Control-Allow-Origin"),
			recWild.Header().Get("Access-Control-Allow-Credentials"))
	}
}

func TestL01_ExtractClientInfoXEmbyTokenQueryFallback(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/Audio/item-1/universal?X-Emby-Token=emby-qs-tok-123&DeviceId=ios-1", nil)
	info := ExtractClientInfo(req)
	if info.Token != "emby-qs-tok-123" {
		t.Fatalf("expected X-Emby-Token query fallback token, got %q", info.Token)
	}
	if info.DeviceID != "ios-1" {
		t.Fatalf("expected DeviceID=ios-1, got %q", info.DeviceID)
	}
}

func TestIsSameOriginBehindTrustedTLSProxy(t *testing.T) {
	// 1. Trusted reverse proxy (loopback 127.0.0.1 by default) with X-Forwarded-Proto: https
	// MUST reject http:// Origin downgrade and allow https:// Origin.
	reqProxyHTTP := httptest.NewRequest(http.MethodPost, "/api/setup/admin", nil)
	reqProxyHTTP.Host = "example.com"
	reqProxyHTTP.RemoteAddr = "127.0.0.1:54321"
	reqProxyHTTP.Header.Set("X-Forwarded-Proto", "https")

	if IsSameOrigin("http://example.com", reqProxyHTTP) {
		t.Fatal("expected http:// Origin rejected when trusted proxy sends X-Forwarded-Proto: https")
	}
	if !IsSameOrigin("https://example.com", reqProxyHTTP) {
		t.Fatal("expected https:// Origin allowed when trusted proxy sends X-Forwarded-Proto: https")
	}
	if !IsSameOrigin("https://example.com:443", reqProxyHTTP) {
		t.Fatal("expected https://example.com:443 allowed when trusted proxy sends X-Forwarded-Proto: https")
	}

	// Comma-separated X-Forwarded-Proto header (e.g. "https, http") -> leftmost value is https.
	reqProxyMulti := httptest.NewRequest(http.MethodPost, "/api/setup/admin", nil)
	reqProxyMulti.Host = "example.com"
	reqProxyMulti.RemoteAddr = "[::1]:54321"
	reqProxyMulti.Header.Set("X-Forwarded-Proto", "https, http")
	if IsSameOrigin("http://example.com", reqProxyMulti) {
		t.Fatal("expected http:// Origin rejected when trusted IPv6 proxy sends X-Forwarded-Proto: https, http")
	}
	if !IsSameOrigin("https://example.com", reqProxyMulti) {
		t.Fatal("expected https:// Origin allowed when trusted IPv6 proxy sends X-Forwarded-Proto: https, http")
	}

	// 2. Untrusted peer MUST NOT be able to spoof X-Forwarded-Proto: https.
	reqUntrusted := httptest.NewRequest(http.MethodPost, "/api/setup/admin", nil)
	reqUntrusted.Host = "example.com"
	reqUntrusted.RemoteAddr = "203.0.113.50:54321"
	reqUntrusted.Header.Set("X-Forwarded-Proto", "https")

	if !IsSameOrigin("http://example.com", reqUntrusted) {
		t.Fatal("expected untrusted peer X-Forwarded-Proto: https to be ignored on plain HTTP request")
	}
}
