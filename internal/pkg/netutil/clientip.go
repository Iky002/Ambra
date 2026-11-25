package netutil

import (
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync"
)

var (
	trustedProxiesMu       sync.RWMutex
	customTrustedProxies   []netip.Prefix
	trustedProxiesDisabled bool
)

// SetTrustedProxies configures an explicit allowlist of trusted reverse proxy
// IPs or CIDRs. Passing an empty slice restores the default loopback-only
// proxy policy. Passing "none" (or "off"/"false") explicitly disables trusting
// forwarded headers even from loopback.
func SetTrustedProxies(entries []string) {
	var parsed []netip.Prefix
	disabled := false
	for _, raw := range entries {
		s := strings.TrimSpace(raw)
		if s == "" {
			continue
		}
		switch strings.ToLower(s) {
		case "none", "off", "false":
			disabled = true
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
	if disabled {
		trustedProxiesDisabled = true
		customTrustedProxies = nil
	} else {
		trustedProxiesDisabled = false
		customTrustedProxies = parsed
	}
	trustedProxiesMu.Unlock()
}

// IsTrustedProxyIP reports whether ip is a configured trusted reverse proxy,
// or (when no explicit list is set and not disabled) a loopback address.
func IsTrustedProxyIP(ip netip.Addr) bool {
	if !ip.IsValid() {
		return false
	}
	ip = ip.WithZone("").Unmap()

	trustedProxiesMu.RLock()
	disabled := trustedProxiesDisabled
	custom := customTrustedProxies
	trustedProxiesMu.RUnlock()
	if disabled {
		return false
	}
	if len(custom) > 0 {
		for _, p := range custom {
			if p.Contains(ip) {
				return true
			}
		}
		return false
	}

	return ip.IsLoopback()
}

// IsLANIP reports whether ip belongs to a local/LAN private network
// (loopback, RFC1918/ULA private, or link-local unicast).
// Carrier-grade NAT (100.64.0.0/10) is intentionally excluded from default LAN trust.
func IsLANIP(ip netip.Addr) bool {
	if !ip.IsValid() {
		return false
	}
	ip = ip.WithZone("").Unmap()
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast()
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
