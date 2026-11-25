package netutil

import (
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync"
)

var (
	trustedProxiesMu     sync.RWMutex
	customTrustedProxies []netip.Prefix
)

// SetTrustedProxies configures an explicit allowlist of trusted reverse proxy
// IPs or CIDRs. Passing an empty slice restores the default private/loopback
// proxy policy.
func SetTrustedProxies(entries []string) {
	var parsed []netip.Prefix
	for _, raw := range entries {
		s := strings.TrimSpace(raw)
		if s == "" {
			continue
		}
		if p, err := netip.ParsePrefix(s); err == nil {
			parsed = append(parsed, p)
			continue
		}
		if a, err := netip.ParseAddr(s); err == nil {
			a = a.WithZone("").Unmap()
			bits := 32
			if a.Is6() {
				bits = 128
			}
			parsed = append(parsed, netip.PrefixFrom(a, bits))
		}
	}
	trustedProxiesMu.Lock()
	customTrustedProxies = parsed
	trustedProxiesMu.Unlock()
}

// IsTrustedProxyIP reports whether ip is a configured trusted reverse proxy,
// or (when no explicit list is set) a loopback, RFC1918/ULA private, or CGNAT
// (100.64.0.0/10) address typical of local/container reverse proxies.
func IsTrustedProxyIP(ip netip.Addr) bool {
	if !ip.IsValid() {
		return false
	}
	ip = ip.WithZone("").Unmap()

	trustedProxiesMu.RLock()
	custom := customTrustedProxies
	trustedProxiesMu.RUnlock()
	if len(custom) > 0 {
		for _, p := range custom {
			if p.Contains(ip) {
				return true
			}
		}
		return false
	}

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
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() {
		return true
	}
	if ip.Is4() && cgnatPrefix.Contains(ip) {
		return true
	}
	return false
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
// itself a trusted reverse proxy; direct public connections cannot spoof
// forwarded headers. All X-Forwarded-For header instances are merged and scanned
// right-to-left to resist multi-header spoofing.
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

	var realIPAddr netip.Addr
	var hasRealIP bool
	if xri := strings.TrimSpace(r.Header.Get("X-Real-IP")); xri != "" {
		realIPAddr, hasRealIP = parseAddrString(xri)
	}

	if xffVals := r.Header.Values("X-Forwarded-For"); len(xffVals) > 0 {
		merged := strings.Join(xffVals, ",")
		parts := strings.Split(merged, ",")
		var rightmostValid netip.Addr
		for i := len(parts) - 1; i >= 0; i-- {
			addr, valid := parseAddrString(parts[i])
			if !valid {
				continue
			}
			if !rightmostValid.IsValid() {
				rightmostValid = addr
			}
			if !IsTrustedProxyIP(addr) {
				return addr.String()
			}
		}
		// If every XFF hop looked like a trusted proxy, do not fall back to a
		// spoofable leftmost 127.0.0.1 when X-Real-IP carries a non-trusted public IP.
		if hasRealIP && !IsTrustedProxyIP(realIPAddr) {
			return realIPAddr.String()
		}
		if rightmostValid.IsValid() {
			return rightmostValid.String()
		}
	}

	if hasRealIP {
		return realIPAddr.String()
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
