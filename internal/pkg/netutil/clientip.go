package netutil

import (
	"net"
	"net/http"
	"net/netip"
	"strings"
)

// IsTrustedProxyIP reports whether ip is a loopback, RFC1918/ULA private,
// or CGNAT (100.64.0.0/10) address typical of local/container reverse proxies.
func IsTrustedProxyIP(ip netip.Addr) bool {
	if !ip.IsValid() {
		return false
	}
	ip = ip.WithZone("").Unmap()
	if ip.IsLoopback() || ip.IsPrivate() {
		return true
	}
	if ip.Is4() && cgnatPrefix.Contains(ip) {
		return true
	}
	return false
}

// IsLANIP reports whether ip belongs to a local/LAN/VPN private network
// (loopback, RFC1918/ULA private, link-local unicast, or CGNAT 100.64.0.0/10).
func IsLANIP(ip netip.Addr) bool {
	if !ip.IsValid() {
		return false
	}
	ip = ip.WithZone("").Unmap()
	return IsTrustedProxyIP(ip) || ip.IsLinkLocalUnicast()
}

func parseAddrString(raw string) (netip.Addr, bool) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return netip.Addr{}, false
	}
	if host, _, err := net.SplitHostPort(s); err == nil {
		s = host
	}
	s = strings.Trim(s, "[]")
	addr, err := netip.ParseAddr(s)
	if err != nil {
		return netip.Addr{}, false
	}
	return addr.WithZone("").Unmap(), true
}

// ClientIP returns the effective client IP for rate-limiting and access checks.
// X-Forwarded-For and X-Real-IP headers are ONLY consulted when r.RemoteAddr is
// itself a trusted local/private reverse proxy; direct public connections cannot
// spoof forwarded headers.
func ClientIP(r *http.Request) string {
	if r == nil {
		return ""
	}
	remoteAddr, ok := parseAddrString(r.RemoteAddr)
	if !ok {
		return strings.TrimSpace(r.RemoteAddr)
	}
	if !IsTrustedProxyIP(remoteAddr) {
		return remoteAddr.String()
	}

	if xff := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); xff != "" {
		parts := strings.Split(xff, ",")
		var leftmostValid netip.Addr
		for i := len(parts) - 1; i >= 0; i-- {
			addr, valid := parseAddrString(parts[i])
			if !valid {
				continue
			}
			leftmostValid = addr
			if !IsTrustedProxyIP(addr) {
				return addr.String()
			}
		}
		if leftmostValid.IsValid() {
			return leftmostValid.String()
		}
	}

	if xri := strings.TrimSpace(r.Header.Get("X-Real-IP")); xri != "" {
		if addr, valid := parseAddrString(xri); valid {
			return addr.String()
		}
	}

	return remoteAddr.String()
}

// IsLocalOrLANRequest reports whether the request originates from a local or
// private LAN client (accounting for trusted reverse proxies via ClientIP).
func IsLocalOrLANRequest(r *http.Request) bool {
	if r == nil {
		return false
	}
	addr, ok := parseAddrString(ClientIP(r))
	if !ok {
		return false
	}
	return IsLANIP(addr)
}
