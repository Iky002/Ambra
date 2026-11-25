package netutil

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
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

	// 2. Trusted loopback reverse proxy forwarding a public client (right-to-left skips loopback)
	req2 := httptest.NewRequest(http.MethodGet, "/", nil)
	req2.RemoteAddr = "127.0.0.1:54321"
	req2.Header.Set("X-Forwarded-For", "9.9.9.9, 198.51.100.77, 127.0.0.1")
	if got := ClientIP(req2); got != "198.51.100.77" {
		t.Fatalf("expected rightmost untrusted hop 198.51.100.77, got %q", got)
	}
	if IsLocalOrLANRequest(req2) {
		t.Fatal("public client behind local reverse proxy must not be treated as LAN")
	}

	// 3. Private Docker IP is NOT trusted as proxy by default unless explicitly configured
	req3Untrusted := httptest.NewRequest(http.MethodGet, "/", nil)
	req3Untrusted.RemoteAddr = "172.18.0.1:12345"
	req3Untrusted.Header.Set("X-Forwarded-For", "198.51.100.42")
	if got := ClientIP(req3Untrusted); got != "172.18.0.1" {
		t.Fatalf("unconfigured private IP must not be trusted as proxy, got %q", got)
	}

	// 3b. Explicitly configured private reverse proxy forwarding a LAN client
	SetTrustedProxies([]string{"172.18.0.0/16"})
	t.Cleanup(func() { SetTrustedProxies(nil) })
	req3 := httptest.NewRequest(http.MethodGet, "/", nil)
	req3.RemoteAddr = "172.18.0.1:12345"
	req3.Header.Set("X-Forwarded-For", "192.168.1.42")
	if got := ClientIP(req3); got != "192.168.1.42" {
		t.Fatalf("expected LAN client 192.168.1.42, got %q", got)
	}
	if !IsLocalOrLANRequest(req3) {
		t.Fatal("LAN client behind trusted proxy should be treated as LAN")
	}
	SetTrustedProxies(nil)

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

func TestSetTrustedProxiesAndAdvancedHeaders(t *testing.T) {
	t.Cleanup(func() { SetTrustedProxies(nil) })

	// Invalid netip.Addr checks
	if IsTrustedProxyIP(netip.Addr{}) {
		t.Fatal("invalid addr must not be trusted proxy")
	}
	if IsLANIP(netip.Addr{}) {
		t.Fatal("invalid addr must not be LAN")
	}
	// Default policy: loopback only for trusted proxies; CGNAT excluded from both trusted proxies and LAN
	if !IsTrustedProxyIP(netip.MustParseAddr("127.0.0.1")) || !IsTrustedProxyIP(netip.MustParseAddr("::1")) {
		t.Fatal("expected loopback to be trusted under default policy")
	}
	if IsTrustedProxyIP(netip.MustParseAddr("192.168.1.1")) || IsTrustedProxyIP(netip.MustParseAddr("100.64.0.1")) {
		t.Fatal("expected private and CGNAT NOT to be trusted proxies by default")
	}
	if IsLANIP(netip.MustParseAddr("100.64.0.1")) {
		t.Fatal("expected CGNAT NOT to be treated as LAN")
	}
	if !IsLANIP(netip.MustParseAddr("fe80::1")) || !IsLANIP(netip.MustParseAddr("192.168.1.10")) {
		t.Fatal("expected link-local and RFC1918 to be LAN IPs")
	}
	if IsLANIP(netip.MustParseAddr("8.8.8.8")) {
		t.Fatal("public IP must not be LAN")
	}

	// Explicit "none" disables trusting even loopback
	SetTrustedProxies([]string{"none"})
	if IsTrustedProxyIP(netip.MustParseAddr("127.0.0.1")) {
		t.Fatal("expected SetTrustedProxies(none) to disable loopback trust")
	}
	reqNone := httptest.NewRequest(http.MethodGet, "/", nil)
	reqNone.RemoteAddr = "127.0.0.1:44321"
	reqNone.Header.Set("X-Forwarded-For", "198.51.100.99")
	if got := ClientIP(reqNone); got != "127.0.0.1" {
		t.Fatalf("expected direct remote addr when trusted proxies disabled, got %q", got)
	}
	SetTrustedProxies(nil)

	// Custom trusted proxy allowlist: CIDR, IPv4, IPv6, empty & invalid entries
	SetTrustedProxies([]string{"", "  ", "203.0.113.0/24", "198.51.100.10", "2001:db8::1", "not-an-ip"})
	if !IsTrustedProxyIP(netip.MustParseAddr("203.0.113.42")) {
		t.Fatal("expected IP in custom CIDR to be trusted")
	}
	if !IsTrustedProxyIP(netip.MustParseAddr("198.51.100.10")) {
		t.Fatal("expected custom IPv4 to be trusted")
	}
	if !IsTrustedProxyIP(netip.MustParseAddr("2001:db8::1")) {
		t.Fatal("expected custom IPv6 to be trusted")
	}
	if IsTrustedProxyIP(netip.MustParseAddr("127.0.0.1")) {
		t.Fatal("loopback must NOT be trusted when custom allowlist excludes it")
	}

	// Restore default policy via empty slice
	SetTrustedProxies(nil)
	if !IsTrustedProxyIP(netip.MustParseAddr("127.0.0.1")) {
		t.Fatal("expected default policy restored after SetTrustedProxies(nil)")
	}

	// Unparseable RemoteAddr
	reqBad := httptest.NewRequest(http.MethodGet, "/", nil)
	reqBad.RemoteAddr = "  unix-socket  "
	if got := ClientIP(reqBad); got != "unix-socket" {
		t.Fatalf("expected trimmed fallback RemoteAddr, got %q", got)
	}
	if IsLocalOrLANRequest(reqBad) {
		t.Fatal("unparseable RemoteAddr must not be treated as LAN")
	}

	// Multiple X-Forwarded-For headers + invalid token inside XFF
	reqMulti := httptest.NewRequest(http.MethodGet, "/", nil)
	reqMulti.RemoteAddr = "127.0.0.1:44321"
	reqMulti.Header.Add("X-Forwarded-For", "1.1.1.1, garbage")
	reqMulti.Header.Add("X-Forwarded-For", "198.51.100.99, 127.0.0.1, invalid")
	if got := ClientIP(reqMulti); got != "198.51.100.99" {
		t.Fatalf("expected rightmost untrusted IP across multi XFF headers, got %q", got)
	}

	// All XFF hops are trusted proxies + X-Real-IP carries public IP -> prefer X-Real-IP
	reqAllTrustedWithReal := httptest.NewRequest(http.MethodGet, "/", nil)
	reqAllTrustedWithReal.RemoteAddr = "127.0.0.1:1111"
	reqAllTrustedWithReal.Header.Set("X-Forwarded-For", "127.0.0.1, ::1")
	reqAllTrustedWithReal.Header.Set("X-Real-IP", "198.51.100.55")
	if got := ClientIP(reqAllTrustedWithReal); got != "198.51.100.55" {
		t.Fatalf("expected public X-Real-IP when all XFF hops are trusted, got %q", got)
	}

	// All XFF hops are trusted proxies + no public X-Real-IP -> return rightmost valid XFF hop
	reqAllTrustedNoReal := httptest.NewRequest(http.MethodGet, "/", nil)
	reqAllTrustedNoReal.RemoteAddr = "127.0.0.1:1111"
	reqAllTrustedNoReal.Header.Set("X-Forwarded-For", "127.0.0.1, ::1")
	if got := ClientIP(reqAllTrustedNoReal); got != "::1" {
		t.Fatalf("expected rightmost valid hop ::1, got %q", got)
	}

	// XFF has only invalid entries, falls back to X-Real-IP
	reqInvalidXFF := httptest.NewRequest(http.MethodGet, "/", nil)
	reqInvalidXFF.RemoteAddr = "127.0.0.1:1111"
	reqInvalidXFF.Header.Set("X-Forwarded-For", "not-an-ip, ???")
	reqInvalidXFF.Header.Set("X-Real-IP", "[2001:db8::9]")
	if got := ClientIP(reqInvalidXFF); got != "2001:db8::9" {
		t.Fatalf("expected fallback to X-Real-IP 2001:db8::9, got %q", got)
	}

	// parseAddrString empty string check
	if _, ok := parseAddrString("   "); ok {
		t.Fatal("expected false for blank addr string")
	}
}
