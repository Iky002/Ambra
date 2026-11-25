package netutil

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestValidateRemoteURL(t *testing.T) {
	tests := []struct {
		name            string
		rawURL          string
		allowPrivateLAN bool
		wantErr         bool
	}{
		// Scheme and host checks
		{"empty", "", true, true},
		{"file scheme", "file:///etc/passwd", true, true},
		{"ftp scheme", "ftp://example.com/file", true, true},
		{"gopher scheme", "gopher://127.0.0.1:6379/_INFO", true, true},
		{"missing host", "http:///path", true, true},
		{"uppercase HTTP public IP", "HTTP://93.184.216.34/video.mp4", false, false},
		{"uppercase HTTPS public IP", "HTTPS://1.1.1.1/playlist.m3u", false, false},

		// Always blocked regardless of allowPrivateLAN
		{"aws metadata allowLAN=true", "http://169.254.169.254/latest/meta-data/", true, true},
		{"aws metadata allowLAN=false", "http://169.254.169.254/latest/meta-data/", false, true},
		{"ipv4-mapped metadata", "http://[::ffff:169.254.169.254]/latest/meta-data/", true, true},
		{"ipv6 link-local", "http://[fe80::1]/secret", true, true},
		{"unspecified ipv4", "http://0.0.0.0:8096/", true, true},
		{"this network ipv4", "http://0.1.2.3/", true, true},
		{"unspecified ipv6", "http://[::]:8096/", true, true},
		{"multicast ipv4", "http://224.0.0.1/", true, true},
		{"multicast ipv6", "http://[ff02::1]/", true, true},

		// Private / loopback behavior
		{"loopback ipv4 allowLAN=false", "http://127.0.0.1:8096/", false, true},
		{"loopback ipv4 allowLAN=true", "http://127.0.0.1:8096/", true, false},
		{"ipv4-mapped loopback allowLAN=false", "http://[::ffff:127.0.0.1]:8096/", false, true},
		{"loopback ipv6 allowLAN=false", "http://[::1]:8096/", false, true},
		{"loopback ipv6 allowLAN=true", "http://[::1]:8096/", true, false},
		{"localhost allowLAN=false", "http://localhost:8096/", false, true},
		{"rfc1918 10/8 allowLAN=false", "http://10.0.0.1/nas.mp4", false, true},
		{"rfc1918 10/8 allowLAN=true", "http://10.0.0.1/nas.mp4", true, false},
		{"rfc1918 172.16/12 allowLAN=false", "http://172.16.0.5/nas.mp4", false, true},
		{"rfc1918 172.16/12 allowLAN=true", "http://172.16.0.5/nas.mp4", true, false},
		{"rfc1918 192.168/16 allowLAN=false", "http://192.168.1.100:5244/d/movie.mkv", false, true},
		{"rfc1918 192.168/16 allowLAN=true", "http://192.168.1.100:5244/d/movie.mkv", true, false},
		{"ipv6 ULA allowLAN=false", "http://[fd00::1]/nas", false, true},
		{"ipv6 ULA allowLAN=true", "http://[fd00::1]/nas", true, false},
		{"cgnat allowLAN=false", "http://100.64.0.1/tailscale", false, true},
		{"cgnat allowLAN=true", "http://100.64.0.1/tailscale", true, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateRemoteURL(tc.rawURL, tc.allowPrivateLAN)
			if (err != nil) != tc.wantErr {
				t.Fatalf("ValidateRemoteURL(%q, %v) err=%v, wantErr=%v", tc.rawURL, tc.allowPrivateLAN, err, tc.wantErr)
			}
		})
	}
}

func TestNewSafeHTTPClientDialAndRedirects(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ok":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("ok"))
		case "/redir-metadata":
			http.Redirect(w, r, "http://169.254.169.254/latest/meta-data/", http.StatusFound)
		case "/redir-file":
			http.Redirect(w, r, "file:///etc/passwd", http.StatusFound)
		case "/redir-loop":
			http.Redirect(w, r, "/redir-loop", http.StatusFound)
		case "/redir-ok":
			http.Redirect(w, r, "/ok", http.StatusFound)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	// 1. Strict client (allowPrivateLAN=false) blocks loopback httptest server
	strictClient := NewSafeHTTPClient(5*time.Second, false)
	if resp, err := strictClient.Get(srv.URL + "/ok"); err == nil {
		_ = resp.Body.Close()
		t.Fatal("expected strict client to block loopback server")
	}

	// 2. LAN client (allowPrivateLAN=true) allows loopback server and safe redirect
	lanClient := NewSafeHTTPClient(5*time.Second, true)
	resp, err := lanClient.Get(srv.URL + "/redir-ok")
	if err != nil {
		t.Fatalf("expected LAN client to allow safe redirect, got err=%v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	// 3. LAN client blocks redirect to cloud metadata IP
	if resp, err := lanClient.Get(srv.URL + "/redir-metadata"); err == nil {
		_ = resp.Body.Close()
		t.Fatal("expected redirect to 169.254.169.254 to be blocked")
	}

	// 4. LAN client blocks redirect to file:// scheme
	if resp, err := lanClient.Get(srv.URL + "/redir-file"); err == nil {
		_ = resp.Body.Close()
		t.Fatal("expected redirect to file:// to be blocked")
	}

	// 5. LAN client stops after max 5 redirects
	if resp, err := lanClient.Get(srv.URL + "/redir-loop"); err == nil {
		_ = resp.Body.Close()
		t.Fatal("expected redirect loop to be stopped after 5 redirects")
	} else if fmt.Sprint(err) == "" {
		t.Fatal("expected non-empty error")
	}
}

func TestValidateIPAndEdgeCases(t *testing.T) {
	if err := ValidateIP(nil, true); err == nil {
		t.Fatal("expected nil IP error")
	}
	if err := ValidateIP(net.IP{1, 2, 3}, true); err == nil {
		t.Fatal("expected invalid length IP error")
	}
	if err := ValidateIP(net.ParseIP("1.1.1.1"), false); err != nil {
		t.Fatalf("expected public IP allowed, got %v", err)
	}
	if err := ValidateIP(net.ParseIP("127.0.0.1"), false); err == nil {
		t.Fatal("expected loopback IP blocked when allowPrivateLAN=false")
	}
	if err := ValidateIP(net.ParseIP("127.0.0.1"), true); err != nil {
		t.Fatalf("expected loopback IP allowed when allowPrivateLAN=true, got %v", err)
	}
	if err := ValidateIP(net.ParseIP("192.168.1.10"), false); err == nil {
		t.Fatal("expected private IP blocked when allowPrivateLAN=false")
	}
	if err := ValidateIP(net.ParseIP("169.254.169.254"), true); err == nil {
		t.Fatal("expected link-local metadata IP always blocked")
	}
	if err := ValidateRemoteURL("http://[::1", true); err == nil {
		t.Fatal("expected malformed URL error")
	}
	if err := ValidateRemoteURL("http://localhost:8096/", true); err != nil {
		t.Fatalf("expected localhost allowed when allowPrivateLAN=true, got %v", err)
	}
	if err := ValidateRemoteURL("http://sub.localhost/", false); err == nil {
		t.Fatal("expected sub.localhost blocked when allowPrivateLAN=false")
	}
}

func TestSafeURLSpecialPrefixesAndRedirectHardening(t *testing.T) {
	// 1. Always-blocked special/transition/metadata/deprecated IPv4-compat prefixes
	blockedURLs := []struct {
		name string
		url  string
	}{
		{"NAT64 well-known", "http://[64:ff9b::1.2.3.4]/"},
		{"NAT64 local-use", "http://[64:ff9b:1::1]/"},
		{"6to4", "http://[2002:c000:0201::1]/"},
		{"Teredo", "http://[2001:0000:4136:e378::1]/"},
		{"IPv4 reserved 240/4", "http://240.0.0.1/"},
		{"Benchmark 198.18/15", "http://198.18.0.1/"},
		{"AWS IPv6 IMDS", "http://[fd00:ec2::254]/latest/meta-data/"},
		{"Alibaba Cloud metadata", "http://100.100.100.200/latest/meta-data/"},
		{"IPv4-mapped Alibaba metadata", "http://[::ffff:100.100.100.200]/"},
		{"Deprecated IPv4-compat", "http://[::127.0.0.1]/"},
		{"Deprecated IPv4-compat public", "http://[::1.2.3.4]/"},
	}
	for _, tc := range blockedURLs {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidateRemoteURL(tc.url, true); err == nil {
				t.Fatalf("expected %s (%s) to be blocked even with allowPrivateLAN=true", tc.name, tc.url)
			}
			if err := ValidateRemoteURL(tc.url, false); err == nil {
				t.Fatalf("expected %s (%s) to be blocked with allowPrivateLAN=false", tc.name, tc.url)
			}
		})
	}

	// Invalid netip.Addr directly in validateAddr
	if err := validateAddr(netip.Addr{}, true); err == nil {
		t.Fatal("expected error for invalid netip.Addr")
	}

	// DNS resolution failure on guaranteed non-existent domain
	if err := ValidateRemoteURL("http://nonexistent.invalid/", true); err == nil {
		t.Fatal("expected DNS resolution error for nonexistent.invalid")
	}

	// 2. Cross-host redirect strips all sensitive auth headers while preserving non-sensitive ones
	var receivedHeaders http.Header
	targetSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeaders = r.Header.Clone()
		w.WriteHeader(http.StatusOK)
	}))
	defer targetSrv.Close()

	originSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, targetSrv.URL+"/landed", http.StatusFound)
	}))
	defer originSrv.Close()

	client := NewSafeHTTPClient(5*time.Second, true)
	req, err := http.NewRequest(http.MethodGet, originSrv.URL+"/start", nil)
	if err != nil {
		t.Fatal(err)
	}
	sensitive := []string{
		"Authorization", "Proxy-Authorization", "Cookie",
		"X-Emby-Token", "X-MediaBrowser-Token",
		"X-Emby-Authorization", "X-Api-Key", "trakt-api-key",
	}
	for _, h := range sensitive {
		req.Header.Set(h, "secret-value")
	}
	req.Header.Set("X-Non-Sensitive", "keep-me")

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("cross-host redirect request failed: %v", err)
	}
	_ = resp.Body.Close()
	for _, h := range sensitive {
		if got := receivedHeaders.Get(h); got != "" {
			t.Fatalf("expected sensitive header %q stripped on cross-host redirect, got %q", h, got)
		}
	}
	if got := receivedHeaders.Get("X-Non-Sensitive"); got != "keep-me" {
		t.Fatalf("expected non-sensitive header preserved, got %q", got)
	}

	// 3. HTTPS -> HTTP redirect downgrade must be rejected
	prevReq := &http.Request{URL: &url.URL{Scheme: "https", Host: "127.0.0.1:8443", Path: "/a"}}
	nextReq := &http.Request{URL: &url.URL{Scheme: "http", Host: "127.0.0.1:8080", Path: "/b"}, Header: make(http.Header)}
	if err := client.CheckRedirect(nextReq, []*http.Request{prevReq}); err == nil || !strings.Contains(err.Error(), "downgrade") {
		t.Fatalf("expected https->http downgrade rejection, got %v", err)
	}

	// Redirect with nil URL and empty host
	if err := client.CheckRedirect(&http.Request{}, nil); err == nil {
		t.Fatal("expected error for nil redirect URL")
	}
	if err := client.CheckRedirect(&http.Request{URL: &url.URL{Scheme: "http", Host: "", Path: "/x"}}, nil); err == nil {
		t.Fatal("expected error for empty redirect host")
	}

	// 4. Transport DialContext error paths: invalid host:port and closed port dial error
	tr := client.Transport.(*http.Transport)
	if _, err := tr.DialContext(context.Background(), "tcp", "missing-port"); err == nil {
		t.Fatal("expected SplitHostPort error from DialContext")
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	closedAddr := ln.Addr().String()
	_ = ln.Close()
	if _, err := tr.DialContext(context.Background(), "tcp", closedAddr); err == nil {
		t.Fatal("expected dial error for closed loopback port")
	}

	// 5. NoLoopback variants block loopback even when allowPrivateLAN=true, while allowing RFC1918
	if err := ValidateRemoteURLNoLoopback("http://127.0.0.1:8096/hook", true); err == nil {
		t.Fatal("expected ValidateRemoteURLNoLoopback to block 127.0.0.1")
	}
	if err := ValidateRemoteURLNoLoopback("http://[::1]:8096/hook", true); err == nil {
		t.Fatal("expected ValidateRemoteURLNoLoopback to block ::1")
	}
	if err := ValidateRemoteURLNoLoopback("http://localhost:8096/hook", true); err == nil {
		t.Fatal("expected ValidateRemoteURLNoLoopback to block localhost")
	}
	if err := ValidateRemoteURLNoLoopback("http://192.168.1.50:8080/hook", true); err != nil {
		t.Fatalf("expected ValidateRemoteURLNoLoopback to allow RFC1918 LAN when allowPrivateLAN=true, got %v", err)
	}
	if err := ValidateRemoteURLNoLoopback("http://192.168.1.50:8080/hook", false); err == nil {
		t.Fatal("expected ValidateRemoteURLNoLoopback to block RFC1918 LAN when allowPrivateLAN=false")
	}
	noLoopClient := NewSafeHTTPClientNoLoopback(5*time.Second, true)
	if resp, err := noLoopClient.Get(targetSrv.URL + "/landed"); err == nil {
		_ = resp.Body.Close()
		t.Fatal("expected NewSafeHTTPClientNoLoopback to block dialing loopback server")
	}
}
