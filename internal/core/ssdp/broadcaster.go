package ssdp

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	MulticastAddr = "239.255.255.250:1900"
	MulticastIP   = "239.255.255.250"
	MulticastPort = 1900

	STAll              = "ssdp:all"
	STRootDevice       = "upnp:rootdevice"
	STMediaServer      = "urn:schemas-upnp-org:device:MediaServer:1"
	STContentDirectory = "urn:schemas-upnp-org:service:ContentDirectory:1"

	NTSAlive  = "ssdp:alive"
	NTSByebye = "ssdp:byebye"
)

// Config configures the DLNA SSDP broadcaster.
type Config struct {
	ServerID        string
	ServerName      string
	ServerVersion   string
	Host            string
	Port            int
	ListenAddr      string
	MulticastTarget string
	MaxAge          int
	NotifyInterval  time.Duration
	Logger          *slog.Logger
}

// MSearchRequest represents a parsed UPnP SSDP M-SEARCH packet.
type MSearchRequest struct {
	Host string
	Man  string
	ST   string
	MX   int
}

// Broadcaster announces an Ambra DLNA MediaServer over UDP SSDP and responds to M-SEARCH discovery requests.
type Broadcaster struct {
	cfg Config

	mu        sync.RWMutex
	localAddr net.Addr
}

// NewBroadcaster creates a new SSDP Broadcaster with sensible UPnP 1.0 defaults.
func NewBroadcaster(cfg Config) *Broadcaster {
	if cfg.ServerID == "" {
		cfg.ServerID = "ambra-server"
	}
	if cfg.ServerVersion == "" {
		cfg.ServerVersion = "4.10.2.0"
	}
	if cfg.Host == "" {
		cfg.Host = DetectLocalIP()
	}
	if cfg.Port <= 0 {
		cfg.Port = 8096
	}
	if cfg.ListenAddr == "" {
		cfg.ListenAddr = fmt.Sprintf(":%d", MulticastPort)
	}
	if cfg.MulticastTarget == "" {
		cfg.MulticastTarget = MulticastAddr
	}
	if cfg.MaxAge <= 0 {
		cfg.MaxAge = 1800
	}
	if cfg.NotifyInterval <= 0 {
		cfg.NotifyInterval = 60 * time.Second
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	return &Broadcaster{cfg: cfg}
}

// LocationURL returns the UPnP device description URL announced by this server.
func (b *Broadcaster) LocationURL() string {
	return fmt.Sprintf("http://%s:%d/Dlna/%s/description.xml", b.cfg.Host, b.cfg.Port, b.cfg.ServerID)
}

// ServerHeader returns the UPnP SERVER header value.
func (b *Broadcaster) ServerHeader() string {
	return fmt.Sprintf("Linux/1.0 UPnP/1.0 Ambra/%s", b.cfg.ServerVersion)
}

// USN formats the Unique Service Name for a given notification type or search target.
func (b *Broadcaster) USN(nt string) string {
	rootUUID := "uuid:" + b.cfg.ServerID
	if nt == rootUUID {
		return rootUUID
	}
	return fmt.Sprintf("%s::%s", rootUUID, nt)
}

// LocalAddr returns the bound UDP address after Start has bound a socket, or nil if not bound.
func (b *Broadcaster) LocalAddr() net.Addr {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.localAddr
}

// NotificationTargets returns the standard UPnP 1.0 MediaServer notification targets.
func (b *Broadcaster) NotificationTargets() []string {
	return []string{
		STRootDevice,
		"uuid:" + b.cfg.ServerID,
		STMediaServer,
		STContentDirectory,
	}
}

// BuildNotify constructs a UPnP 1.0 NOTIFY packet for ssdp:alive or ssdp:byebye.
func (b *Broadcaster) BuildNotify(nt, nts string) []byte {
	var buf bytes.Buffer
	buf.WriteString("NOTIFY * HTTP/1.1\r\n")
	fmt.Fprintf(&buf, "HOST: %s\r\n", MulticastAddr)
	if nts != NTSByebye {
		fmt.Fprintf(&buf, "CACHE-CONTROL: max-age=%d\r\n", b.cfg.MaxAge)
		fmt.Fprintf(&buf, "LOCATION: %s\r\n", b.LocationURL())
		fmt.Fprintf(&buf, "SERVER: %s\r\n", b.ServerHeader())
	}
	fmt.Fprintf(&buf, "NT: %s\r\n", nt)
	fmt.Fprintf(&buf, "NTS: %s\r\n", nts)
	fmt.Fprintf(&buf, "USN: %s\r\n", b.USN(nt))
	buf.WriteString("\r\n")
	return buf.Bytes()
}

// BuildMSearchResponse constructs a UPnP 1.0 HTTP/1.1 200 OK response to an M-SEARCH request.
func (b *Broadcaster) BuildMSearchResponse(st string) []byte {
	if st == "" || st == STAll {
		st = STMediaServer
	}
	var buf bytes.Buffer
	buf.WriteString("HTTP/1.1 200 OK\r\n")
	fmt.Fprintf(&buf, "CACHE-CONTROL: max-age=%d\r\n", b.cfg.MaxAge)
	fmt.Fprintf(&buf, "DATE: %s\r\n", time.Now().UTC().Format(time.RFC1123))
	buf.WriteString("EXT:\r\n")
	fmt.Fprintf(&buf, "LOCATION: %s\r\n", b.LocationURL())
	fmt.Fprintf(&buf, "SERVER: %s\r\n", b.ServerHeader())
	fmt.Fprintf(&buf, "ST: %s\r\n", st)
	fmt.Fprintf(&buf, "USN: %s\r\n", b.USN(st))
	buf.WriteString("\r\n")
	return buf.Bytes()
}

// ParseMSearch parses an incoming UDP packet and reports whether it is a valid
// UPnP M-SEARCH request matching a DLNA MediaServer target.
func ParseMSearch(data []byte) (*MSearchRequest, bool) {
	scanner := bufio.NewScanner(bytes.NewReader(data))
	if !scanner.Scan() {
		return nil, false
	}
	reqLine := strings.TrimSpace(scanner.Text())
	if !strings.HasPrefix(strings.ToUpper(reqLine), "M-SEARCH * HTTP/1.") {
		return nil, false
	}

	req := &MSearchRequest{}
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			break
		}
		colon := strings.IndexByte(line, ':')
		if colon <= 0 {
			continue
		}
		key := strings.ToUpper(strings.TrimSpace(line[:colon]))
		val := strings.TrimSpace(line[colon+1:])
		switch key {
		case "HOST":
			req.Host = val
		case "MAN":
			req.Man = val
		case "ST":
			req.ST = val
		case "MX":
			if n, err := strconv.Atoi(val); err == nil && n >= 0 {
				req.MX = n
			}
		}
	}

	if !strings.EqualFold(strings.Trim(req.Man, `"`), "ssdp:discover") {
		return nil, false
	}
	if !IsMatchingTarget(req.ST) {
		return nil, false
	}
	return req, true
}

// IsMatchingTarget reports whether the search target matches a DLNA MediaServer.
func IsMatchingTarget(st string) bool {
	switch st {
	case STAll, STRootDevice, STMediaServer, STContentDirectory:
		return true
	default:
		return false
	}
}

// Start opens the SSDP UDP listener, broadcasts ssdp:alive announcements,
// responds to incoming M-SEARCH packets, and broadcasts ssdp:byebye on shutdown.
// If binding the UDP port fails (e.g. sandboxed/container environments disallowing port 1900),
// Start logs a warning and degrades gracefully without returning an error.
func (b *Broadcaster) Start(ctx context.Context) error {
	conn, err := b.listenUDP()
	if err != nil {
		b.cfg.Logger.Warn("ssdp udp bind failed; degrading gracefully without dlna discovery",
			"addr", b.cfg.ListenAddr,
			"err", err,
		)
		return nil
	}

	b.mu.Lock()
	b.localAddr = conn.LocalAddr()
	b.mu.Unlock()

	defer func() {
		b.sendNotifications(conn, NTSByebye)
		_ = conn.Close()
		b.mu.Lock()
		b.localAddr = nil
		b.mu.Unlock()
	}()

	b.sendNotifications(conn, NTSAlive)

	done := make(chan struct{})
	go func() {
		defer close(done)
		b.readLoop(ctx, conn)
	}()

	ticker := time.NewTicker(b.cfg.NotifyInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			_ = conn.SetReadDeadline(time.Now())
			<-done
			return nil
		case <-ticker.C:
			b.sendNotifications(conn, NTSAlive)
		}
	}
}

func (b *Broadcaster) listenUDP() (*net.UDPConn, error) {
	if b.cfg.ListenAddr == fmt.Sprintf(":%d", MulticastPort) || b.cfg.ListenAddr == MulticastAddr {
		if maddr, err := net.ResolveUDPAddr("udp4", MulticastAddr); err == nil {
			if conn, err := net.ListenMulticastUDP("udp4", nil, maddr); err == nil {
				return conn, nil
			}
		}
	}
	laddr, err := net.ResolveUDPAddr("udp4", b.cfg.ListenAddr)
	if err != nil {
		return nil, err
	}
	return net.ListenUDP("udp4", laddr)
}

func (b *Broadcaster) sendNotifications(conn *net.UDPConn, nts string) {
	dst, err := net.ResolveUDPAddr("udp4", b.cfg.MulticastTarget)
	if err != nil {
		b.cfg.Logger.Warn("ssdp resolve multicast target failed", "target", b.cfg.MulticastTarget, "err", err)
		return
	}
	for _, nt := range b.NotificationTargets() {
		pkt := b.BuildNotify(nt, nts)
		if _, err := conn.WriteToUDP(pkt, dst); err != nil {
			b.cfg.Logger.Debug("ssdp notify send failed", "nts", nts, "nt", nt, "err", err)
		}
	}
}

func (b *Broadcaster) readLoop(ctx context.Context, conn *net.UDPConn) {
	buf := make([]byte, 2048)
	for {
		_ = conn.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
		n, remote, err := conn.ReadFromUDP(buf)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				continue
			}
			return
		}
		b.handlePacket(conn, remote, buf[:n])
	}
}

func (b *Broadcaster) handlePacket(conn *net.UDPConn, remote *net.UDPAddr, data []byte) {
	req, ok := ParseMSearch(data)
	if !ok {
		return
	}
	if req.ST == STAll {
		for _, target := range b.NotificationTargets() {
			_, _ = conn.WriteToUDP(b.BuildMSearchResponse(target), remote)
		}
		return
	}
	_, _ = conn.WriteToUDP(b.BuildMSearchResponse(req.ST), remote)
}

// DetectLocalIP returns the first non-loopback IPv4 address found on the host,
// falling back to 127.0.0.1 if none is available.
func DetectLocalIP() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return "127.0.0.1"
	}
	for _, a := range addrs {
		ipNet, ok := a.(*net.IPNet)
		if !ok || ipNet.IP == nil || ipNet.IP.IsLoopback() {
			continue
		}
		if ip4 := ipNet.IP.To4(); ip4 != nil {
			return ip4.String()
		}
	}
	return "127.0.0.1"
}
