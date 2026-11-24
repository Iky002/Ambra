package ssdp

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"
)

func TestBuildNotifyAndMSearchResponse(t *testing.T) {
	b := NewBroadcaster(Config{
		ServerID:      "srv-123",
		Host:          "192.168.1.50",
		Port:          8096,
		ServerVersion: "4.10.2.0",
		MaxAge:        1800,
	})

	if got := b.LocationURL(); got != "http://192.168.1.50:8096/Dlna/srv-123/description.xml" {
		t.Fatalf("unexpected LocationURL: %q", got)
	}
	if got := b.USN(STMediaServer); got != "uuid:srv-123::urn:schemas-upnp-org:device:MediaServer:1" {
		t.Fatalf("unexpected USN: %q", got)
	}
	if got := b.USN("uuid:srv-123"); got != "uuid:srv-123" {
		t.Fatalf("unexpected root uuid USN: %q", got)
	}

	alive := string(b.BuildNotify(STMediaServer, NTSAlive))
	for _, substr := range []string{
		"NOTIFY * HTTP/1.1\r\n",
		"HOST: 239.255.255.250:1900\r\n",
		"CACHE-CONTROL: max-age=1800\r\n",
		"LOCATION: http://192.168.1.50:8096/Dlna/srv-123/description.xml\r\n",
		"NT: urn:schemas-upnp-org:device:MediaServer:1\r\n",
		"NTS: ssdp:alive\r\n",
		"SERVER: Linux/1.0 UPnP/1.0 Ambra/4.10.2.0\r\n",
		"USN: uuid:srv-123::urn:schemas-upnp-org:device:MediaServer:1\r\n",
	} {
		if !strings.Contains(alive, substr) {
			t.Errorf("alive packet missing %q:\n%s", substr, alive)
		}
	}

	byebye := string(b.BuildNotify(STMediaServer, NTSByebye))
	if !strings.Contains(byebye, "NTS: ssdp:byebye\r\n") || strings.Contains(byebye, "LOCATION:") {
		t.Errorf("unexpected byebye packet:\n%s", byebye)
	}

	resp := string(b.BuildMSearchResponse(STAll))
	for _, substr := range []string{
		"HTTP/1.1 200 OK\r\n",
		"CACHE-CONTROL: max-age=1800\r\n",
		"EXT:\r\n",
		"LOCATION: http://192.168.1.50:8096/Dlna/srv-123/description.xml\r\n",
		"ST: urn:schemas-upnp-org:device:MediaServer:1\r\n",
		"USN: uuid:srv-123::urn:schemas-upnp-org:device:MediaServer:1\r\n",
	} {
		if !strings.Contains(resp, substr) {
			t.Errorf("M-SEARCH response missing %q:\n%s", substr, resp)
		}
	}
}

func TestParseMSearch(t *testing.T) {
	validTargets := []string{STAll, STRootDevice, STMediaServer, STContentDirectory}
	for _, st := range validTargets {
		pkt := "M-SEARCH * HTTP/1.1\r\n" +
			"HOST: 239.255.255.250:1900\r\n" +
			"MAN: \"ssdp:discover\"\r\n" +
			"MX: 3\r\n" +
			"ST: " + st + "\r\n" +
			"MalformedLineWithoutColon\r\n\r\n"

		req, ok := ParseMSearch([]byte(pkt))
		if !ok {
			t.Fatalf("expected valid M-SEARCH for ST=%q", st)
		}
		if req.ST != st || req.MX != 3 || req.Host != MulticastAddr {
			t.Fatalf("unexpected parsed request: %+v", req)
		}
	}

	invalidCases := []string{
		"",
		"NOTIFY * HTTP/1.1\r\nMAN: \"ssdp:discover\"\r\nST: ssdp:all\r\n\r\n",
		"M-SEARCH * HTTP/1.1\r\nHOST: 239.255.255.250:1900\r\nST: ssdp:all\r\n\r\n",
		"M-SEARCH * HTTP/1.1\r\nMAN: \"ssdp:discover\"\r\nST: urn:schemas-upnp-org:device:InternetGatewayDevice:1\r\n\r\n",
	}
	for i, pkt := range invalidCases {
		if req, ok := ParseMSearch([]byte(pkt)); ok {
			t.Errorf("case %d: expected invalid M-SEARCH, got %+v", i, req)
		}
	}
}

func TestBroadcasterDefaultsAndLocalIP(t *testing.T) {
	b := NewBroadcaster(Config{})
	if b.cfg.ServerID == "" || b.cfg.Port != 8096 || b.cfg.Host == "" {
		t.Fatalf("unexpected defaults: %+v", b.cfg)
	}
	ip := DetectLocalIP()
	if net.ParseIP(ip) == nil {
		t.Fatalf("DetectLocalIP returned invalid IP: %q", ip)
	}
}

func TestBroadcasterLifecycleUDP(t *testing.T) {
	// Simulated multicast receiver on localhost.
	mcastSink, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatalf("listen sink udp: %v", err)
	}
	defer mcastSink.Close()

	b := NewBroadcaster(Config{
		ServerID:        "test-dlna-id",
		Host:            "127.0.0.1",
		Port:            8096,
		ListenAddr:      "127.0.0.1:0",
		MulticastTarget: mcastSink.LocalAddr().String(),
		NotifyInterval:  60 * time.Millisecond,
	})

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- b.Start(ctx)
	}()

	// Wait for Broadcaster to bind.
	var bAddr net.Addr
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if a := b.LocalAddr(); a != nil {
			bAddr = a
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if bAddr == nil {
		t.Fatal("broadcaster did not bind UDP socket in time")
	}

	// Verify initial ssdp:alive notifications arrived at sink.
	_ = mcastSink.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 2048)
	n, _, err := mcastSink.ReadFromUDP(buf)
	if err != nil {
		t.Fatalf("expected ssdp:alive packet: %v", err)
	}
	if !strings.Contains(string(buf[:n]), "NTS: ssdp:alive") {
		t.Fatalf("expected ssdp:alive in packet, got:\n%s", string(buf[:n]))
	}

	// Send M-SEARCH packets from a client socket and verify 200 OK responses.
	client, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatalf("listen client udp: %v", err)
	}
	defer client.Close()

	targetAddr := bAddr.(*net.UDPAddr)

	// 1. Non-matching packet should be ignored.
	_, _ = client.WriteToUDP([]byte("NOT-SSDP\r\n\r\n"), targetAddr)

	// 2. Specific MediaServer M-SEARCH should return 1 response.
	searchMedia := "M-SEARCH * HTTP/1.1\r\nHOST: 239.255.255.250:1900\r\nMAN: \"ssdp:discover\"\r\nMX: 1\r\nST: " + STMediaServer + "\r\n\r\n"
	if _, err := client.WriteToUDP([]byte(searchMedia), targetAddr); err != nil {
		t.Fatalf("write M-SEARCH: %v", err)
	}
	_ = client.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, _, err = client.ReadFromUDP(buf)
	if err != nil {
		t.Fatalf("expected 200 OK response: %v", err)
	}
	respStr := string(buf[:n])
	if !strings.Contains(respStr, "HTTP/1.1 200 OK") || !strings.Contains(respStr, "test-dlna-id") {
		t.Fatalf("unexpected M-SEARCH response:\n%s", respStr)
	}

	// 3. ssdp:all should return 4 responses (one per notification target).
	searchAll := "M-SEARCH * HTTP/1.1\r\nHOST: 239.255.255.250:1900\r\nMAN: \"ssdp:discover\"\r\nMX: 1\r\nST: ssdp:all\r\n\r\n"
	if _, err := client.WriteToUDP([]byte(searchAll), targetAddr); err != nil {
		t.Fatalf("write ssdp:all: %v", err)
	}
	for i := 0; i < 4; i++ {
		_ = client.SetReadDeadline(time.Now().Add(2 * time.Second))
		n, _, err = client.ReadFromUDP(buf)
		if err != nil {
			t.Fatalf("expected response %d/4 for ssdp:all: %v", i+1, err)
		}
		if !strings.Contains(string(buf[:n]), "HTTP/1.1 200 OK") {
			t.Fatalf("unexpected ssdp:all response %d: %s", i+1, string(buf[:n]))
		}
	}

	// Wait for at least one periodic notify tick, then cancel and expect ssdp:byebye.
	time.Sleep(80 * time.Millisecond)
	cancel()

	if err := <-errCh; err != nil {
		t.Fatalf("Broadcaster.Start returned error: %v", err)
	}

	foundByebye := false
	_ = mcastSink.SetReadDeadline(time.Now().Add(1 * time.Second))
	for i := 0; i < 32; i++ {
		n, _, err = mcastSink.ReadFromUDP(buf)
		if err != nil {
			break
		}
		if strings.Contains(string(buf[:n]), "NTS: ssdp:byebye") {
			foundByebye = true
			break
		}
	}
	if !foundByebye {
		t.Fatal("expected ssdp:byebye notification on shutdown")
	}
}

func TestBroadcasterGracefulDegradationOnBindFailure(t *testing.T) {
	b := NewBroadcaster(Config{
		ServerID:   "degrade-test",
		ListenAddr: "invalid-host-name-:::99999",
	})
	if err := b.Start(context.Background()); err != nil {
		t.Fatalf("expected graceful degradation returning nil, got %v", err)
	}
	if b.LocalAddr() != nil {
		t.Fatalf("expected nil LocalAddr after bind failure, got %v", b.LocalAddr())
	}
}
