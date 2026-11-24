package netutil

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"
)

const (
	maxRedirects      = 5
	dnsLookupTimeout  = 5 * time.Second
	dialTimeout       = 10 * time.Second
	respHeaderTimeout = 30 * time.Second
)

var (
	// cgnatPrefix is RFC 6598 Shared Address Space (100.64.0.0/10).
	cgnatPrefix = netip.MustParsePrefix("100.64.0.0/10")
)

// ValidateRemoteURL validates that rawURL uses http or https, has a non-empty host,
// and resolves only to permitted IP addresses.
//
// When allowPrivateLAN is true, loopback and RFC1918/ULA private addresses are allowed
// (for self-hosted LAN services such as NAS/Alist), while cloud metadata / link-local
// (169.254.0.0/16, fe80::/10), unspecified (0.0.0.0/8, ::), and multicast addresses
// are always blocked.
//
// When allowPrivateLAN is false, loopback (127.0.0.0/8, ::1), private RFC1918/ULA
// (10.0.0.0/8, 172.16.0.0/12, 192.168.0.0/16, fc00::/7), and CGNAT (100.64.0.0/10)
// addresses are also blocked.
func ValidateRemoteURL(rawURL string, allowPrivateLAN bool) error {
	trimmed := strings.TrimSpace(rawURL)
	if trimmed == "" {
		return errors.New("empty url")
	}
	u, err := url.Parse(trimmed)
	if err != nil {
		return fmt.Errorf("invalid url: %w", err)
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return fmt.Errorf("unsupported url scheme %q", u.Scheme)
	}
	host := strings.TrimSpace(u.Hostname())
	if host == "" {
		return errors.New("url host is empty")
	}

	ctx, cancel := context.WithTimeout(context.Background(), dnsLookupTimeout)
	defer cancel()
	_, err = resolveAndValidateHost(ctx, host, allowPrivateLAN)
	return err
}

// ValidateIP checks whether ip is allowed under the allowPrivateLAN policy.
func ValidateIP(ip net.IP, allowPrivateLAN bool) error {
	if ip == nil {
		return errors.New("nil ip address")
	}
	addr, ok := netip.AddrFromSlice(ip)
	if !ok {
		return fmt.Errorf("invalid ip address %q", ip.String())
	}
	return validateAddr(addr, allowPrivateLAN)
}

func validateAddr(addr netip.Addr, allowPrivateLAN bool) error {
	if !addr.IsValid() {
		return errors.New("invalid ip address")
	}
	// Strip IPv6 zone and unmap IPv4-mapped IPv6 (e.g. ::ffff:169.254.169.254, ::ffff:127.0.0.1).
	addr = addr.WithZone("").Unmap()

	// Always block unspecified (0.0.0.0, ::), IPv4 "this network" (0.0.0.0/8),
	// link-local unicast (169.254.0.0/16 cloud metadata, fe80::/10), and multicast.
	if addr.IsUnspecified() || (addr.Is4() && addr.As4()[0] == 0) {
		return fmt.Errorf("blocked unspecified address %s", addr)
	}
	if addr.IsLinkLocalUnicast() {
		return fmt.Errorf("blocked link-local address %s", addr)
	}
	if addr.IsMulticast() || addr.IsLinkLocalMulticast() || addr.IsInterfaceLocalMulticast() {
		return fmt.Errorf("blocked multicast address %s", addr)
	}

	if !allowPrivateLAN {
		if addr.IsLoopback() {
			return fmt.Errorf("blocked loopback address %s", addr)
		}
		if addr.IsPrivate() {
			return fmt.Errorf("blocked private address %s", addr)
		}
		if addr.Is4() && cgnatPrefix.Contains(addr) {
			return fmt.Errorf("blocked shared/cgnat address %s", addr)
		}
	}
	return nil
}

func resolveAndValidateHost(ctx context.Context, host string, allowPrivateLAN bool) ([]netip.Addr, error) {
	// Check IP literal first (handles both IPv4 and IPv6, with or without zone).
	if addr, err := netip.ParseAddr(host); err == nil {
		if err := validateAddr(addr, allowPrivateLAN); err != nil {
			return nil, err
		}
		return []netip.Addr{addr.Unmap()}, nil
	}

	lowerHost := strings.ToLower(strings.TrimSuffix(host, "."))
	if !allowPrivateLAN && (lowerHost == "localhost" || strings.HasSuffix(lowerHost, ".localhost")) {
		return nil, fmt.Errorf("blocked loopback hostname %q", host)
	}

	ipAddrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, fmt.Errorf("resolve host %q: %w", host, err)
	}
	if len(ipAddrs) == 0 {
		return nil, fmt.Errorf("host %q resolved to no ip addresses", host)
	}

	validated := make([]netip.Addr, 0, len(ipAddrs))
	for _, ia := range ipAddrs {
		addr, ok := netip.AddrFromSlice(ia.IP)
		if !ok {
			return nil, fmt.Errorf("host %q resolved to invalid ip %q", host, ia.IP.String())
		}
		if err := validateAddr(addr, allowPrivateLAN); err != nil {
			return nil, fmt.Errorf("host %q: %w", host, err)
		}
		validated = append(validated, addr.Unmap())
	}
	return validated, nil
}

// NewSafeHTTPClient returns an *http.Client that validates every resolved IP before dialing
// (preventing DNS rebinding) and validates every redirect target (max 5 redirects, http/https only).
func NewSafeHTTPClient(timeout time.Duration, allowPrivateLAN bool) *http.Client {
	dialer := &net.Dialer{
		Timeout:   dialTimeout,
		KeepAlive: 30 * time.Second,
	}

	transport := &http.Transport{
		ResponseHeaderTimeout: respHeaderTimeout,
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, fmt.Errorf("invalid dial address %q: %w", addr, err)
			}
			ips, err := resolveAndValidateHost(ctx, host, allowPrivateLAN)
			if err != nil {
				return nil, err
			}
			var lastErr error
			for _, ip := range ips {
				conn, dialErr := dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
				if dialErr == nil {
					return conn, nil
				}
				lastErr = dialErr
			}
			if lastErr != nil {
				return nil, lastErr
			}
			return nil, fmt.Errorf("unable to dial %q", addr)
		},
	}

	return &http.Client{
		Timeout:   timeout,
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= maxRedirects {
				return fmt.Errorf("stopped after %d redirects", maxRedirects)
			}
			if req.URL == nil {
				return errors.New("redirect target url is nil")
			}
			if err := ValidateRemoteURL(req.URL.String(), allowPrivateLAN); err != nil {
				return fmt.Errorf("blocked redirect target: %w", err)
			}
			return nil
		},
	}
}
