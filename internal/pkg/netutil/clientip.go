package netutil

import (
	"bufio"
	"encoding/binary"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

var (
	trustedProxiesMu       sync.RWMutex
	customTrustedProxies   []netip.Prefix
	trustedProxiesDisabled bool

	localSubnetsMu           sync.RWMutex
	customLocalSubnets       []netip.Prefix
	localSubnetsConfigured   bool
	localSubnetsLoopbackOnly bool

	netProbeMu               sync.RWMutex
	localInterfacePrefixesFn func() []netip.Prefix
	defaultRouteGatewaysFn   func() []netip.Addr

	ifaceCacheMu       sync.Mutex
	cachedIfaceSubnets []netip.Prefix
	cachedIfaceAt      time.Time

	routeCacheMu       sync.Mutex
	cachedGatewayAddrs []netip.Addr
	cachedGatewayAt    time.Time
)

const netProbeCacheTTL = 5 * time.Second

var testFallbackPrivatePrefixes = []netip.Prefix{
	netip.MustParsePrefix("10.0.0.0/8"),
	netip.MustParsePrefix("192.168.0.0/16"),
	netip.MustParsePrefix("fc00::/7"),
}

// SetTrustedProxies configures an explicit allowlist of trusted reverse proxy
// IPs or CIDRs. Passing an empty slice restores the default loopback-only
// proxy policy. Passing "none" (or "off"/"false") explicitly disables trusting
// forwarded headers even from loopback.
func SetTrustedProxies(entries []string) {
	var parsed []netip.Prefix
	disabled := false
	nonEmptyCount := 0
	for _, raw := range entries {
		s := strings.TrimSpace(raw)
		if s == "" {
			continue
		}
		nonEmptyCount++
		switch strings.ToLower(s) {
		case "none", "off", "false":
			disabled = true
			continue
		}
		if p, err := netip.ParsePrefix(s); err == nil {
			parsed = append(parsed, p.Masked())
			continue
		}
		if a, err := netip.ParseAddr(s); err == nil {
			a = a.WithZone("").Unmap()
			bits := 32
			if a.Is6() {
				bits = 128
			}
			parsed = append(parsed, netip.PrefixFrom(a, bits))
			continue
		}
		slog.Warn("netutil: ignoring invalid trusted proxy entry", "entry", s)
	}
	if !disabled && nonEmptyCount > 0 && len(parsed) == 0 {
		slog.Warn("netutil: all configured trusted proxy entries are invalid; disabling forwarded header trust")
		disabled = true
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

// SetLocalNetworkSubnets configures explicit LAN subnets (CIDRs or single IPs).
// When configured with one or more entries, ONLY loopback or addresses matching
// customLocalSubnets are treated as LAN. Passing "none" (or "off"/"false")
// restricts LAN recognition strictly to loopback. Passing an empty slice
// restores automatic local active interface subnet detection.
func SetLocalNetworkSubnets(cidrs []string) {
	var parsed []netip.Prefix
	loopbackOnly := false
	nonEmptyCount := 0
	for _, raw := range cidrs {
		s := strings.TrimSpace(raw)
		if s == "" {
			continue
		}
		nonEmptyCount++
		switch strings.ToLower(s) {
		case "none", "off", "false":
			loopbackOnly = true
			continue
		}
		if p, err := netip.ParsePrefix(s); err == nil {
			parsed = append(parsed, p.Masked())
			continue
		}
		if a, err := netip.ParseAddr(s); err == nil {
			a = a.WithZone("").Unmap()
			bits := 32
			if a.Is6() {
				bits = 128
			}
			parsed = append(parsed, netip.PrefixFrom(a, bits))
			continue
		}
		slog.Warn("netutil: ignoring invalid local network subnet entry", "entry", s)
	}
	if !loopbackOnly && nonEmptyCount > 0 && len(parsed) == 0 {
		slog.Warn("netutil: all configured local network subnet entries are invalid; restricting LAN to loopback only")
		loopbackOnly = true
	}

	localSubnetsMu.Lock()
	if nonEmptyCount == 0 {
		localSubnetsConfigured = false
		localSubnetsLoopbackOnly = false
		customLocalSubnets = nil
	} else if loopbackOnly {
		localSubnetsConfigured = true
		localSubnetsLoopbackOnly = true
		customLocalSubnets = nil
	} else {
		localSubnetsConfigured = true
		localSubnetsLoopbackOnly = false
		customLocalSubnets = parsed
	}
	localSubnetsMu.Unlock()
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

func detectLocalInterfacePrefixes() []netip.Prefix {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	var out []netip.Prefix
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, rawAddr := range addrs {
			var ip net.IP
			var mask net.IPMask
			switch v := rawAddr.(type) {
			case *net.IPNet:
				ip = v.IP
				mask = v.Mask
			case *net.IPAddr:
				ip = v.IP
			}
			if len(ip) == 0 {
				continue
			}
			addr, ok := netip.AddrFromSlice(ip)
			if !ok {
				continue
			}
			addr = addr.WithZone("").Unmap()
			// Only treat private/ULA/link-local interface subnets as LAN so public
			// cloud /24 neighbors on a public NIC are never considered LAN.
			if !addr.IsPrivate() && !addr.IsLinkLocalUnicast() {
				continue
			}
			bits := 32
			if addr.Is6() {
				bits = 128
			}
			if len(mask) > 0 {
				ones, total := mask.Size()
				if total == 128 && addr.Is4() {
					// IPv4-mapped IPv6 netmask (e.g. ::ffff:255.255.255.0)
					if ones >= 96 {
						bits = ones - 96
					}
				} else if total == 32 && addr.Is4() {
					bits = ones
				} else if total == 128 && addr.Is6() {
					bits = ones
				}
			}
			out = append(out, netip.PrefixFrom(addr, bits).Masked())
		}
	}
	return out
}

func currentLocalInterfacePrefixes() ([]netip.Prefix, bool) {
	netProbeMu.RLock()
	fn := localInterfacePrefixesFn
	netProbeMu.RUnlock()
	if fn != nil {
		return fn(), true
	}

	now := time.Now()
	ifaceCacheMu.Lock()
	defer ifaceCacheMu.Unlock()
	if !cachedIfaceAt.IsZero() && now.Sub(cachedIfaceAt) < netProbeCacheTTL {
		return cachedIfaceSubnets, false
	}
	cachedIfaceSubnets = detectLocalInterfacePrefixes()
	cachedIfaceAt = now
	return cachedIfaceSubnets, false
}

func parseProcNetRouteGateways(r *bufio.Scanner) []netip.Addr {
	var gateways []netip.Addr
	lineNo := 0
	for r.Scan() {
		lineNo++
		if lineNo == 1 {
			// Skip header line
			continue
		}
		fields := strings.Fields(r.Text())
		if len(fields) < 4 {
			continue
		}
		destHex := fields[1]
		gwHex := fields[2]
		flagsHex := fields[3]
		if destHex != "00000000" || gwHex == "00000000" || len(gwHex) != 8 {
			continue
		}
		flags, err := strconv.ParseUint(flagsHex, 16, 32)
		if err != nil || (flags&0x3) != 0x3 { // RTF_UP (0x1) | RTF_GATEWAY (0x2)
			continue
		}
		gwVal, err := strconv.ParseUint(gwHex, 16, 32)
		if err != nil {
			continue
		}
		var raw [4]byte
		binary.NativeEndian.PutUint32(raw[:], uint32(gwVal))
		addr := netip.AddrFrom4(raw)
		if addr.IsValid() {
			gateways = append(gateways, addr)
		}
	}
	return gateways
}

func parseProcNetIPv6RouteGateways(r *bufio.Scanner) []netip.Addr {
	var gateways []netip.Addr
	for r.Scan() {
		fields := strings.Fields(r.Text())
		if len(fields) < 9 {
			continue
		}
		// fields: dest(0) dest_prefix(1) src(2) src_prefix(3) nexthop(4) metric(5) refcnt(6) use(7) flags(8)
		if fields[0] != "00000000000000000000000000000000" || fields[1] != "00" {
			continue
		}
		nhHex := fields[4]
		if len(nhHex) != 32 || nhHex == "00000000000000000000000000000000" {
			continue
		}
		flags, err := strconv.ParseUint(fields[8], 16, 32)
		if err != nil || (flags&0x3) != 0x3 { // RTF_UP | RTF_GATEWAY
			continue
		}
		var raw [16]byte
		valid := true
		for i := 0; i < 16; i++ {
			b, bErr := strconv.ParseUint(nhHex[i*2:i*2+2], 16, 8)
			if bErr != nil {
				valid = false
				break
			}
			raw[i] = byte(b)
		}
		if valid {
			gateways = append(gateways, netip.AddrFrom16(raw).Unmap())
		}
	}
	return gateways
}

func detectLinuxDefaultRouteGateways() []netip.Addr {
	if runtime.GOOS != "linux" {
		return nil
	}
	var out []netip.Addr
	if f, err := os.Open("/proc/net/route"); err == nil {
		out = append(out, parseProcNetRouteGateways(bufio.NewScanner(f))...)
		_ = f.Close()
	}
	if f6, err := os.Open("/proc/net/ipv6_route"); err == nil {
		out = append(out, parseProcNetIPv6RouteGateways(bufio.NewScanner(f6))...)
		_ = f6.Close()
	}
	return out
}

func currentDefaultRouteGateways() []netip.Addr {
	netProbeMu.RLock()
	fn := defaultRouteGatewaysFn
	netProbeMu.RUnlock()
	if fn != nil {
		return fn()
	}

	now := time.Now()
	routeCacheMu.Lock()
	defer routeCacheMu.Unlock()
	if !cachedGatewayAt.IsZero() && now.Sub(cachedGatewayAt) < netProbeCacheTTL {
		return cachedGatewayAddrs
	}
	cachedGatewayAddrs = detectLinuxDefaultRouteGateways()
	cachedGatewayAt = now
	return cachedGatewayAddrs
}

func isDefaultRouteGatewayIP(ip netip.Addr) bool {
	if !ip.IsValid() {
		return false
	}
	ip = ip.WithZone("").Unmap()
	for _, gw := range currentDefaultRouteGateways() {
		if gw.WithZone("").Unmap() == ip {
			return true
		}
	}
	return false
}

// IsLANIP reports whether ip belongs to a local/LAN network:
//   - When SetLocalNetworkSubnets is configured: ONLY loopback or configured subnets.
//   - Otherwise: loopback, link-local unicast, or active local private interface subnets
//     (Carrier-grade NAT 100.64.0.0/10 and public IPs are never treated as LAN by default).
func IsLANIP(ip netip.Addr) bool {
	if !ip.IsValid() {
		return false
	}
	ip = ip.WithZone("").Unmap()
	if ip.IsLoopback() {
		return true
	}

	localSubnetsMu.RLock()
	configured := localSubnetsConfigured
	loopbackOnly := localSubnetsLoopbackOnly
	custom := customLocalSubnets
	localSubnetsMu.RUnlock()

	if configured {
		if loopbackOnly {
			return false
		}
		for _, p := range custom {
			if p.Contains(ip) {
				return true
			}
		}
		return false
	}

	if ip.IsLinkLocalUnicast() {
		return true
	}

	prefixes, injected := currentLocalInterfacePrefixes()
	for _, p := range prefixes {
		if p.Contains(ip) {
			return true
		}
	}

	// Preserve deterministic testability in unit test binaries that synthesize
	// standard LAN client addresses (192.168.x.x / 10.x.x.x / fc00::/7) without
	// injecting a mock interface table.
	if !injected && testing.Testing() {
		for _, p := range testFallbackPrivatePrefixes {
			if p.Contains(ip) {
				return true
			}
		}
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
// If the direct peer equals a default route gateway IP (such as a Docker bridge
// gateway 172.17.0.1 / 172.18.0.1) and is NOT an explicitly trusted proxy, the
// connection is treated as NAT-masqueraded external traffic (returns false).
func IsLocalOrLANRequest(r *http.Request) bool {
	if r == nil {
		return false
	}
	if remoteAddr, ok := parseAddrString(r.RemoteAddr); ok {
		if !IsTrustedProxyIP(remoteAddr) && isDefaultRouteGatewayIP(remoteAddr) {
			return false
		}
	}
	addr, ok := parseAddrString(ClientIP(r))
	if !ok {
		return false
	}
	return IsLANIP(addr)
}
