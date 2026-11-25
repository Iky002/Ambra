package netutil

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClientIPTrustedProxyPolicy(t *testing.T) {
	// 1. Direct public connection: MUST ignore spoofed X-Forwarded-For and X-Real-IP
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "203.0.113.50:43210"
	req.Header.Set("X-Forwarded-For", "1.2.3.4")
	req.Header.Set("X-Real-IP", "5.6.7.8")
	if got := ClientIP(req); got != "203.0.113.50" {
		t.Fatalf("public RemoteAddr must ignore forwarded headers, got %q", got)
	}
	if IsLocalOrLANRequest(req) {
		t.Fatal("public client must not be treated as LAN")
	}

	// 2. Trusted loopback reverse proxy forwarding a public client (right-to-left skips spoofed leftmost)
	req2 := httptest.NewRequest(http.MethodGet, "/", nil)
	req2.RemoteAddr = "127.0.0.1:54321"
	req2.Header.Set("X-Forwarded-For", "9.9.9.9, 198.51.100.77, 172.17.0.2")
	if got := ClientIP(req2); got != "198.51.100.77" {
		t.Fatalf("expected rightmost untrusted hop 198.51.100.77, got %q", got)
	}
	if IsLocalOrLANRequest(req2) {
		t.Fatal("public client behind local reverse proxy must not be treated as LAN")
	}

	// 3. Trusted proxy forwarding a LAN client
	req3 := httptest.NewRequest(http.MethodGet, "/", nil)
	req3.RemoteAddr = "172.18.0.1:12345"
	req3.Header.Set("X-Forwarded-For", "192.168.1.42")
	if got := ClientIP(req3); got != "192.168.1.42" {
		t.Fatalf("expected LAN client 192.168.1.42, got %q", got)
	}
	if !IsLocalOrLANRequest(req3) {
		t.Fatal("LAN client behind trusted proxy should be treated as LAN")
	}

	// 4. Trusted proxy with X-Real-IP fallback
	req4 := httptest.NewRequest(http.MethodGet, "/", nil)
	req4.RemoteAddr = "[::1]:8080"
	req4.Header.Set("X-Real-IP", "10.0.0.9")
	if got := ClientIP(req4); got != "10.0.0.9" {
		t.Fatalf("expected X-Real-IP 10.0.0.9, got %q", got)
	}
	if !IsLocalOrLANRequest(req4) {
		t.Fatal("10.0.0.9 should be treated as LAN")
	}

	// 5. Direct loopback without headers
	req5 := httptest.NewRequest(http.MethodGet, "/", nil)
	req5.RemoteAddr = "127.0.0.1:9999"
	if !IsLocalOrLANRequest(req5) {
		t.Fatal("direct 127.0.0.1 should be LAN")
	}

	// 6. Nil request safety
	if ClientIP(nil) != "" || IsLocalOrLANRequest(nil) {
		t.Fatal("nil request should return empty/false")
	}
}
