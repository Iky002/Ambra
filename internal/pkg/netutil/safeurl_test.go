package netutil

import (
	"fmt"
	"net/http"
	"net/http/httptest"
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
