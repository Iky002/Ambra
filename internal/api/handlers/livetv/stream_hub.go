package livetv

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	defaultStreamIdleTimeout = 30 * time.Second
	defaultStreamGracePeriod = 250 * time.Millisecond
	streamRingMaxChunks      = 8
	streamSubQueueDepth      = 128
	streamReadChunkBytes     = 32 * 1024
)

var errUpstreamIdleTimeout = errors.New("live stream upstream idle timeout exceeded")

// LiveStreamHub multiplexes concurrent viewers watching the same continuous live TV channel
// onto a single upstream HTTP connection with an in-memory ring buffer and idle read timeout.
type LiveStreamHub struct {
	mu          sync.Mutex
	streams     map[string]*liveStreamBroadcaster
	idleTimeout time.Duration
	gracePeriod time.Duration
	closed      bool
}

func newLiveStreamHub() *LiveStreamHub {
	return &LiveStreamHub{
		streams:     make(map[string]*liveStreamBroadcaster),
		idleTimeout: defaultStreamIdleTimeout,
		gracePeriod: defaultStreamGracePeriod,
	}
}

func (hub *LiveStreamHub) setIdleTimeout(d time.Duration) {
	hub.mu.Lock()
	if d > 0 {
		hub.idleTimeout = d
	}
	hub.mu.Unlock()
}

func (hub *LiveStreamHub) setGracePeriod(d time.Duration) {
	hub.mu.Lock()
	if d >= 0 {
		hub.gracePeriod = d
	}
	hub.mu.Unlock()
}

func (hub *LiveStreamHub) getTimings() (idleTimeout, gracePeriod time.Duration) {
	hub.mu.Lock()
	idleTimeout = hub.idleTimeout
	gracePeriod = hub.gracePeriod
	hub.mu.Unlock()
	if idleTimeout <= 0 {
		idleTimeout = defaultStreamIdleTimeout
	}
	return idleTimeout, gracePeriod
}

// Close immediately terminates all active upstream live stream connections.
func (hub *LiveStreamHub) Close() {
	if hub == nil {
		return
	}
	hub.mu.Lock()
	hub.closed = true
	list := make([]*liveStreamBroadcaster, 0, len(hub.streams))
	for k, b := range hub.streams {
		list = append(list, b)
		delete(hub.streams, k)
	}
	hub.mu.Unlock()

	for _, b := range list {
		b.stop()
		<-b.doneCh
	}
}

type liveStreamSubscriber struct {
	ch   chan []byte
	done chan struct{}
}

type liveStreamBroadcaster struct {
	hub         *LiveStreamHub
	key         string
	streamURL   string
	userAgent   string
	client      *http.Client
	idleTimeout time.Duration
	gracePeriod time.Duration

	readyCh     chan struct{}
	doneCh      chan struct{}
	contentType string
	baseURL     *url.URL
	isHLS       bool
	hlsRaw      []byte
	initErr     error
	statusErr   int

	mu          sync.Mutex
	subs        map[*liveStreamSubscriber]struct{}
	ring        [][]byte
	graceTimer  *time.Timer
	cancelUp    context.CancelFunc
	closed      bool
	upstreamEOF bool
}

func (b *liveStreamBroadcaster) stop() {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return
	}
	b.closed = true
	if b.graceTimer != nil {
		b.graceTimer.Stop()
		b.graceTimer = nil
	}
	cancel := b.cancelUp
	for sub := range b.subs {
		delete(b.subs, sub)
		close(sub.done)
	}
	b.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (b *liveStreamBroadcaster) startUpstream() {
	defer close(b.doneCh)
	ctx, cancel := context.WithCancel(context.Background())
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		cancel()
		close(b.readyCh)
		return
	}
	b.cancelUp = cancel
	b.mu.Unlock()

	defer func() {
		cancel()
		b.hub.removeBroadcaster(b.key, b)
	}()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, b.streamURL, nil)
	if err != nil {
		b.initErr = err
		close(b.readyCh)
		b.stop()
		return
	}
	if b.userAgent != "" {
		req.Header.Set("User-Agent", b.userAgent)
	}

	// Enforce idle timeout even before headers/first bytes arrive via a watchdog timer.
	watchdog := newIdleWatchdog(b.idleTimeout, cancel)
	defer watchdog.Stop()

	resp, err := b.client.Do(req)
	if err != nil {
		if watchdog.TimedOut() {
			b.initErr = errUpstreamIdleTimeout
		} else {
			b.initErr = err
		}
		close(b.readyCh)
		b.stop()
		return
	}
	defer resp.Body.Close()
	watchdog.Touch()

	if resp.StatusCode >= 400 {
		b.statusErr = resp.StatusCode
		b.initErr = fmt.Errorf("upstream stream returned status %d", resp.StatusCode)
		close(b.readyCh)
		b.stop()
		return
	}

	baseURL := req.URL
	if resp.Request != nil && resp.Request.URL != nil {
		baseURL = resp.Request.URL
	}
	ct := resp.Header.Get("Content-Type")
	b.contentType = ct
	b.baseURL = baseURL

	// If upstream responded with an HLS playlist (.m3u8), read it once and share the snapshot with simultaneous callers,
	// then release the broadcaster so future playlist polls fetch fresh manifests.
	if isHLSPlaylistResponse(ct, baseURL) {
		b.isHLS = true
		raw, rErr := io.ReadAll(io.LimitReader(&watchdogReader{r: resp.Body, wd: watchdog}, maxHLSPlaylistBytes+1))
		if rErr != nil {
			if watchdog.TimedOut() {
				b.initErr = errUpstreamIdleTimeout
			} else {
				b.initErr = rErr
			}
			close(b.readyCh)
			b.stop()
			return
		}
		b.hlsRaw = raw
		b.hub.removeBroadcaster(b.key, b)
		close(b.readyCh)
		b.stop()
		return
	}

	close(b.readyCh)

	buf := make([]byte, streamReadChunkBytes)
	reader := &watchdogReader{r: resp.Body, wd: watchdog}
	for {
		n, readErr := reader.Read(buf)
		if n > 0 {
			chunk := make([]byte, n)
			copy(chunk, buf[:n])
			b.broadcastChunk(chunk)
		}
		if readErr != nil {
			b.mu.Lock()
			b.upstreamEOF = true
			b.mu.Unlock()
			b.stop()
			return
		}
	}
}

func (b *liveStreamBroadcaster) broadcastChunk(chunk []byte) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return
	}
	if len(b.ring) >= streamRingMaxChunks {
		copy(b.ring, b.ring[1:])
		b.ring[len(b.ring)-1] = chunk
	} else {
		b.ring = append(b.ring, chunk)
	}
	for sub := range b.subs {
		select {
		case sub.ch <- chunk:
		default:
			// Drop oldest buffered chunk for slow viewer so live upstream & healthy viewers never stall.
			select {
			case <-sub.ch:
			default:
			}
			select {
			case sub.ch <- chunk:
			default:
			}
		}
	}
}

func (b *liveStreamBroadcaster) addSubscriber() (*liveStreamSubscriber, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return nil, false
	}
	if b.graceTimer != nil {
		b.graceTimer.Stop()
		b.graceTimer = nil
	}
	sub := &liveStreamSubscriber{
		ch:   make(chan []byte, streamSubQueueDepth+len(b.ring)),
		done: make(chan struct{}),
	}
	for _, initial := range b.ring {
		sub.ch <- initial
	}
	b.subs[sub] = struct{}{}
	return sub, true
}

func (b *liveStreamBroadcaster) removeSubscriber(sub *liveStreamSubscriber) {
	b.mu.Lock()
	if _, ok := b.subs[sub]; !ok {
		b.mu.Unlock()
		return
	}
	delete(b.subs, sub)
	if len(b.subs) == 0 && !b.closed {
		if b.gracePeriod <= 0 {
			b.mu.Unlock()
			b.hub.removeBroadcaster(b.key, b)
			b.stop()
			return
		}
		if b.graceTimer != nil {
			b.graceTimer.Stop()
		}
		b.graceTimer = time.AfterFunc(b.gracePeriod, func() {
			b.mu.Lock()
			empty := len(b.subs) == 0 && !b.closed
			b.mu.Unlock()
			if empty {
				b.hub.removeBroadcaster(b.key, b)
				b.stop()
			}
		})
	}
	b.mu.Unlock()
}

func (hub *LiveStreamHub) removeBroadcaster(key string, target *liveStreamBroadcaster) {
	hub.mu.Lock()
	if cur, ok := hub.streams[key]; ok && cur == target {
		delete(hub.streams, key)
	}
	hub.mu.Unlock()
}

func (hub *LiveStreamHub) acquireOrCreate(key, streamURL, userAgent string, client *http.Client) (*liveStreamBroadcaster, *liveStreamSubscriber, error) {
	idleTimeout, gracePeriod := hub.getTimings()

	for attempt := 0; attempt < 3; attempt++ {
		hub.mu.Lock()
		if hub.closed {
			hub.mu.Unlock()
			return nil, nil, errors.New("live stream hub is closed")
		}
		b, exists := hub.streams[key]
		if !exists {
			b = &liveStreamBroadcaster{
				hub:         hub,
				key:         key,
				streamURL:   streamURL,
				userAgent:   userAgent,
				client:      client,
				idleTimeout: idleTimeout,
				gracePeriod: gracePeriod,
				readyCh:     make(chan struct{}),
				doneCh:      make(chan struct{}),
				subs:        make(map[*liveStreamSubscriber]struct{}),
			}
			sub := &liveStreamSubscriber{
				ch:   make(chan []byte, streamSubQueueDepth+streamRingMaxChunks),
				done: make(chan struct{}),
			}
			b.subs[sub] = struct{}{}
			hub.streams[key] = b
			hub.mu.Unlock()

			go b.startUpstream()
			return b, sub, nil
		}
		hub.mu.Unlock()

		if sub, ok := b.addSubscriber(); ok {
			return b, sub, nil
		}
		// Existing broadcaster just closed concurrently; retry to start a fresh one.
		hub.removeBroadcaster(key, b)
	}
	return nil, nil, errors.New("failed to attach to live stream broadcaster")
}

type idleWatchdog struct {
	timeout  time.Duration
	cancel   context.CancelFunc
	mu       sync.Mutex
	timer    *time.Timer
	timedOut bool
	stopped  bool
}

func newIdleWatchdog(timeout time.Duration, cancel context.CancelFunc) *idleWatchdog {
	if timeout <= 0 {
		timeout = defaultStreamIdleTimeout
	}
	wd := &idleWatchdog{
		timeout: timeout,
		cancel:  cancel,
	}
	wd.timer = time.AfterFunc(timeout, func() {
		wd.mu.Lock()
		if wd.stopped {
			wd.mu.Unlock()
			return
		}
		wd.timedOut = true
		wd.mu.Unlock()
		if cancel != nil {
			cancel()
		}
	})
	return wd
}

func (wd *idleWatchdog) Touch() {
	wd.mu.Lock()
	if !wd.stopped && !wd.timedOut && wd.timer != nil {
		wd.timer.Reset(wd.timeout)
	}
	wd.mu.Unlock()
}

func (wd *idleWatchdog) TimedOut() bool {
	wd.mu.Lock()
	t := wd.timedOut
	wd.mu.Unlock()
	return t
}

func (wd *idleWatchdog) Stop() {
	wd.mu.Lock()
	wd.stopped = true
	if wd.timer != nil {
		wd.timer.Stop()
	}
	wd.mu.Unlock()
}

type watchdogReader struct {
	r  io.Reader
	wd *idleWatchdog
}

func (wr *watchdogReader) Read(p []byte) (int, error) {
	n, err := wr.r.Read(p)
	if n > 0 && wr.wd != nil {
		wr.wd.Touch()
	}
	if err != nil && wr.wd != nil && wr.wd.TimedOut() {
		return n, errUpstreamIdleTimeout
	}
	return n, err
}

func streamKeyForChannel(channelID, streamURL string, allowPrivateLAN bool) string {
	lan := "0"
	if allowPrivateLAN {
		lan = "1"
	}
	return strings.TrimSpace(channelID) + "\x00" + lan + "\x00" + strings.TrimSpace(streamURL)
}
