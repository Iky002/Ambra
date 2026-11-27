package netutil

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestM01_UntrustedProxyForwardingHeadersDenyLocalOrLAN(t *testing.T) {
	SetTrustedProxies(nil)
	SetLocalNetworkSubnets(nil)
	t.Cleanup(func() {
		SetTrustedProxies(nil)
		SetLocalNetworkSubnets(nil)
	})

	// Direct loopback & LAN requests WITHOUT forwarding headers remain local/LAN.
	for _, remoteAddr := range []string{"127.0.0.1:44444", "[::1]:44444", "192.168.1.25:55555"} {
		req := httptest.NewRequest(http.MethodGet, "/System/Info/Public", nil)
		req.RemoteAddr = remoteAddr
		if !IsLocalOrLANRequest(req) {
			t.Fatalf("expected direct peer %s without forwarding headers to be local/LAN", remoteAddr)
		}
	}

	// Untrusted Docker bridge / LAN reverse proxy carrying forwarding headers MUST return false!
	headers := []struct {
		key string
		val string
	}{
		{"X-Forwarded-For", "203.0.113.42"},
		{"X-Real-IP", "198.51.100.7"},
		{"Forwarded", "for=203.0.113.42;proto=https"},
		{"CF-Connecting-IP", "203.0.113.99"},
	}
	for _, peer := range []string{"172.17.0.2:50000", "192.168.1.1:50000"} {
		for _, hdr := range headers {
			req := httptest.NewRequest(http.MethodPost, "/Users/AuthenticateByName", nil)
			req.RemoteAddr = peer
			req.Header.Set(hdr.key, hdr.val)
			if IsLocalOrLANRequest(req) {
				t.Fatalf("expected IsLocalOrLANRequest=false when untrusted peer %s sends %s=%q", peer, hdr.key, hdr.val)
			}
			// ClientIP must still attribute to the direct TCP peer when proxy is untrusted.
			if got := ClientIP(req); got == "203.0.113.42" || got == "198.51.100.7" || got == "203.0.113.99" {
				t.Fatalf("ClientIP must ignore untrusted %s header from %s, got %q", hdr.key, peer, got)
			}
		}
	}

	// When trusted proxies are explicitly disabled ("none"), even loopback carrying forwarding headers is non-LAN
	// and ClientIP attributes to 127.0.0.1.
	SetTrustedProxies([]string{"none"})
	for _, hdr := range headers {
		req := httptest.NewRequest(http.MethodPost, "/Users/AuthenticateByName", nil)
		req.RemoteAddr = "127.0.0.1:50000"
		req.Header.Set(hdr.key, hdr.val)
		if IsLocalOrLANRequest(req) {
			t.Fatalf("expected IsLocalOrLANRequest=false on loopback with trustedProxies=none and %s", hdr.key)
		}
		if got := ClientIP(req); got != "127.0.0.1" {
			t.Fatalf("expected ClientIP=127.0.0.1 when trustedProxies=none, got %q", got)
		}
	}

	// Trusted proxy resolves RFC 7239 Forwarded / CF-Connecting-IP accurately.
	SetTrustedProxies([]string{"127.0.0.1"})

	reqRFCExt := httptest.NewRequest(http.MethodGet, "/", nil)
	reqRFCExt.RemoteAddr = "127.0.0.1:8080"
	reqRFCExt.Header.Set("Forwarded", `for="[2001:db8::1]:4711";proto=https`)
	if got := ClientIP(reqRFCExt); got != "2001:db8::1" {
		t.Fatalf("expected ClientIP=2001:db8::1 from RFC 7239 Forwarded, got %q", got)
	}
	if IsLocalOrLANRequest(reqRFCExt) {
		t.Fatal("expected external IP via trusted proxy Forwarded header to be non-LAN")
	}

	reqCF := httptest.NewRequest(http.MethodGet, "/", nil)
	reqCF.RemoteAddr = "127.0.0.1:8080"
	reqCF.Header.Set("CF-Connecting-IP", "192.168.10.20")
	if got := ClientIP(reqCF); got != "192.168.10.20" {
		t.Fatalf("expected ClientIP=192.168.10.20 from CF-Connecting-IP, got %q", got)
	}
	if !IsLocalOrLANRequest(reqCF) {
		t.Fatal("expected LAN IP via trusted proxy CF-Connecting-IP to be LAN")
	}

	// Trusted proxy sending malformed/unparseable forwarding header fails closed (non-LAN).
	reqMalformed := httptest.NewRequest(http.MethodGet, "/", nil)
	reqMalformed.RemoteAddr = "127.0.0.1:8080"
	reqMalformed.Header.Set("Forwarded", "for=unknown")
	if IsLocalOrLANRequest(reqMalformed) {
		t.Fatal("expected unparseable Forwarded header from trusted proxy to fail closed as non-LAN")
	}
}
