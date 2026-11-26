package netutil

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
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

func TestDockerBridgeGatewayNATAndLocalNetworkSubnets(t *testing.T) {
	t.Cleanup(func() {
		SetTrustedProxies(nil)
		SetLocalNetworkSubnets(nil)
		netProbeMu.Lock()
		localInterfacePrefixesFn = nil
		defaultRouteGatewaysFn = nil
		netProbeMu.Unlock()
	})

	// Simulate a Docker container whose eth0 is 172.17.0.2/16 and default gateway is 172.17.0.1
	gw4Hex := fmt.Sprintf("%08X", binary.NativeEndian.Uint32([]byte{172, 17, 0, 1}))
	procRoute := "Iface\tDestination\tGateway \tFlags\tRefCnt\tUse\tMetric\tMask\t\tMTU\tWindow\tIRTT\n" +
		"eth0\t00000000\t" + gw4Hex + "\t0003\t0\t0\t0\t00000000\t0\t0\t0\n" +
		"eth0\t000011AC\t00000000\t0001\t0\t0\t0\t0000FFFF\t0\t0\t0\n"
	parsedGateways := parseProcNetRouteGateways(bufio.NewScanner(strings.NewReader(procRoute)))
	if len(parsedGateways) != 1 || parsedGateways[0] != netip.MustParseAddr("172.17.0.1") {
		t.Fatalf("expected parsed gateway 172.17.0.1, got %v", parsedGateways)
	}

	// Also verify IPv6 proc route gateway parser
	procRoute6 := "00000000000000000000000000000000 00 00000000000000000000000000000000 00 fd000000000000000000000000000001 00000400 00000001 00000000 00000003 eth0\n"
	parsedGW6 := parseProcNetIPv6RouteGateways(bufio.NewScanner(strings.NewReader(procRoute6)))
	if len(parsedGW6) != 1 || parsedGW6[0] != netip.MustParseAddr("fd00::1") {
		t.Fatalf("expected parsed IPv6 gateway fd00::1, got %v", parsedGW6)
	}

	netProbeMu.Lock()
	localInterfacePrefixesFn = func() []netip.Prefix {
		return []netip.Prefix{netip.MustParsePrefix("172.17.0.0/16")}
	}
	defaultRouteGatewaysFn = func() []netip.Addr {
		return []netip.Addr{
			netip.MustParseAddr("172.17.0.1"),
			netip.MustParseAddr("172.18.0.1"),
		}
	}
	netProbeMu.Unlock()

	// 1. NAT-forwarded external request arriving from Docker bridge gateway 172.17.0.1 MUST NOT be LAN
	reqNAT := httptest.NewRequest(http.MethodGet, "/", nil)
	reqNAT.RemoteAddr = "172.17.0.1:49812"
	if IsLocalOrLANRequest(reqNAT) {
		t.Fatal("untrusted Docker bridge gateway 172.17.0.1 MUST NOT be treated as LAN (C-01)")
	}

	// 2. Sibling container on the same bridge subnet (172.17.0.5 != gateway) IS on local subnet
	reqSibling := httptest.NewRequest(http.MethodGet, "/", nil)
	reqSibling.RemoteAddr = "172.17.0.5:51000"
	if !IsLocalOrLANRequest(reqSibling) {
		t.Fatal("direct sibling container 172.17.0.5 on eth0 subnet should be treated as LAN")
	}

	// 3. Unrelated private subnet NOT present on container NICs (e.g. 192.168.99.10) is NOT LAN when NIC table is known
	reqOtherRFC1918 := httptest.NewRequest(http.MethodGet, "/", nil)
	reqOtherRFC1918.RemoteAddr = "192.168.99.10:51000"
	if IsLocalOrLANRequest(reqOtherRFC1918) {
		t.Fatal("non-attached RFC1918 subnet should not match when active interfaces are enumerated")
	}

	// 4. When gateway IS explicitly configured in TrustedProxies, real client IP from XFF decides LAN vs WAN
	SetTrustedProxies([]string{"172.17.0.1"})
	reqProxyWAN := httptest.NewRequest(http.MethodGet, "/", nil)
	reqProxyWAN.RemoteAddr = "172.17.0.1:52000"
	reqProxyWAN.Header.Set("X-Forwarded-For", "203.0.113.88")
	if IsLocalOrLANRequest(reqProxyWAN) {
		t.Fatal("public client behind trusted Docker gateway proxy must NOT be LAN")
	}

	SetLocalNetworkSubnets([]string{"192.168.50.0/24", "10.20.30.40", "fd12:3456::1"})
	reqProxyLAN := httptest.NewRequest(http.MethodGet, "/", nil)
	reqProxyLAN.RemoteAddr = "172.17.0.1:52001"
	reqProxyLAN.Header.Set("X-Forwarded-For", "192.168.50.9")
	if !IsLocalOrLANRequest(reqProxyLAN) {
		t.Fatal("configured custom LAN subnet client behind trusted Docker gateway proxy MUST be LAN")
	}
	if !IsLANIP(netip.MustParseAddr("10.20.30.40")) || !IsLANIP(netip.MustParseAddr("fd12:3456::1")) {
		t.Fatal("single-IP IPv4/IPv6 entries in SetLocalNetworkSubnets must match")
	}
	if IsLANIP(netip.MustParseAddr("172.17.0.5")) || IsLANIP(netip.MustParseAddr("fe80::1")) {
		t.Fatal("when custom LocalNetworkSubnets is set, ONLY loopback and configured subnets count as LAN")
	}
	if !IsLANIP(netip.MustParseAddr("127.0.0.1")) {
		t.Fatal("loopback must always remain local even with custom LocalNetworkSubnets")
	}

	// 5. All-invalid or "none" LocalNetworkSubnets restricts LAN strictly to loopback
	SetLocalNetworkSubnets([]string{"invalid-cidr", "999.1.1.1"})
	if IsLANIP(netip.MustParseAddr("192.168.50.9")) {
		t.Fatal("all-invalid LocalNetworkSubnets must fail closed to loopback-only")
	}
	SetLocalNetworkSubnets([]string{"none"})
	if IsLANIP(netip.MustParseAddr("192.168.50.9")) || !IsLANIP(netip.MustParseAddr("::1")) {
		t.Fatal("SetLocalNetworkSubnets(none) must allow only loopback")
	}

	// 6. Exercise live OS interface & route detectors without panics
	_ = detectLocalInterfacePrefixes()
	_ = detectLinuxDefaultRouteGateways()
}
