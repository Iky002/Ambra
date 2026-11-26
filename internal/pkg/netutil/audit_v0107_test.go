package netutil

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
)

func TestSetTrustedProxiesAllInvalidDisablesForwarding(t *testing.T) {
	t.Cleanup(func() { SetTrustedProxies(nil) })

	SetTrustedProxies([]string{"not-an-ip", "999.999.999.999/32", "bad/cidr"})
	if IsTrustedProxyIP(netip.MustParseAddr("127.0.0.1")) {
		t.Fatal("all-invalid trusted proxy config must disable trust instead of falling back to loopback")
	}
	if IsTrustedProxyIP(netip.MustParseAddr("::1")) {
		t.Fatal("all-invalid trusted proxy config must disable IPv6 loopback trust")
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "127.0.0.1:41234"
	req.Header.Set("X-Forwarded-For", "198.51.100.88")
	if got := ClientIP(req); got != "127.0.0.1" {
		t.Fatalf("expected RemoteAddr 127.0.0.1 when all proxy entries invalid, got %q", got)
	}
}
