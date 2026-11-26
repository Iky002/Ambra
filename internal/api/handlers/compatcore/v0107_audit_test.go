package compatcore

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ambra-media/ambra/internal/api/middleware"
	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/core/auth"
)

func TestCompatCoreV0107AuditDeviceLockRateCapSnapshotAndKVLock(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()

	// 1. [M-6] Non-admin cannot spoof arbitrary DeviceId on Playing/Ping and PlayingItems/*
	guestAC := &middleware.AuthContext{
		User:   env.user2,
		Token:  "guest-tok",
		Client: auth.ClientInfo{DeviceID: "bound-guest-dev", Device: "Phone", Client: "Ambra"},
	}
	doGuestSpoof := func(method, target string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, target, nil)
		req.Header.Set("X-Emby-Authorization", `MediaBrowser Client="Evil", Device="Evil", DeviceId="spoofed-victim-dev", Version="1.0"`)
		req = req.WithContext(middleware.WithAuthContext(req.Context(), guestAC))
		rec := httptest.NewRecorder()
		env.router.ServeHTTP(rec, req)
		return rec
	}

	for _, tc := range []struct {
		method string
		path   string
	}{
		{http.MethodPost, "/Sessions/Playing/Ping"},
		{http.MethodPost, "/Users/" + env.user2.ID + "/PlayingItems/" + env.itemID + "?PositionTicks=100"},
		{http.MethodPost, "/Users/" + env.user2.ID + "/PlayingItems/" + env.itemID + "/Progress?PositionTicks=200"},
		{http.MethodDelete, "/Users/" + env.user2.ID + "/PlayingItems/" + env.itemID + "?PositionTicks=300"},
	} {
		rec := doGuestSpoof(tc.method, tc.path)
		if rec.Code != http.StatusNoContent {
			t.Fatalf("M-6 %s %s got %d body=%q", tc.method, tc.path, rec.Code, rec.Body.String())
		}
	}
	for _, s := range env.sessions.ListSessions() {
		if s.DeviceId == "spoofed-victim-dev" {
			t.Fatalf("M-6 spoofed DeviceId was registered in sessions: %+v", s)
		}
	}

	// 2. [M-8] Rate-limit maps cap eviction at maxRateLimitBuckets (4096)
	now := time.Now().UTC()
	env.handler.mu.Lock()
	for i := 0; i < maxRateLimitBuckets+50; i++ {
		key := fmt.Sprintf("198.51.%d.%d", i/256, i%256)
		appendRateBucketLocked(env.handler.forgotRates, key, now, maxRateLimitBuckets)
		appendRateBucketLocked(env.handler.authByIDRates, key, now, maxRateLimitBuckets)
		appendRateBucketLocked(env.handler.authByIDUserRates, "user-"+key, now, maxRateLimitBuckets)
	}
	if len(env.handler.forgotRates) > maxRateLimitBuckets {
		t.Fatalf("M-8 forgotRates exceeded cap: %d", len(env.handler.forgotRates))
	}
	if len(env.handler.authByIDRates) > maxRateLimitBuckets {
		t.Fatalf("M-8 authByIDRates exceeded cap: %d", len(env.handler.authByIDRates))
	}
	if len(env.handler.authByIDUserRates) > maxRateLimitBuckets {
		t.Fatalf("M-8 authByIDUserRates exceeded cap: %d", len(env.handler.authByIDUserRates))
	}
	// Also test checkIPRateLimit cap on forgotPinRates
	for i := 0; i < maxRateLimitBuckets+20; i++ {
		_ = env.handler.checkIPRateLimit(env.handler.forgotPinRates, fmt.Sprintf("203.0.%d.%d", i/256, i%256), now, time.Minute, 5)
	}
	if len(env.handler.forgotPinRates) > maxRateLimitBuckets {
		t.Fatalf("M-8 forgotPinRates exceeded cap: %d", len(env.handler.forgotPinRates))
	}
	// Also test pruneRateBucketLocked excess eviction directly
	over := make(map[string][]time.Time, maxRateLimitBuckets+10)
	for i := 0; i < maxRateLimitBuckets+10; i++ {
		over[fmt.Sprintf("k-%d", i)] = []time.Time{now}
	}
	pruneRateBucketLocked(over, now.Add(-time.Minute), maxRateLimitBuckets)
	if len(over) > maxRateLimitBuckets {
		t.Fatalf("M-8 pruneRateBucketLocked failed to cap excess: %d", len(over))
	}
	env.handler.mu.Unlock()

	// 3. [M-14] getSystemStorageInfo uses (&snap).TranscodeTempDir() and Snapshot() is race-free under concurrent cfg.Update
	adminAC := &middleware.AuthContext{User: env.admin, Client: auth.ClientInfo{DeviceID: "adm-dev"}}
	recStor := httptest.NewRecorder()
	reqStor := httptest.NewRequest(http.MethodGet, "/System/Info/Storage", nil)
	reqStor = reqStor.WithContext(middleware.WithAuthContext(reqStor.Context(), adminAC))
	env.router.ServeHTTP(recStor, reqStor)
	if recStor.Code != http.StatusOK {
		t.Fatalf("getSystemStorageInfo got %d", recStor.Code)
	}
	var stor map[string]map[string]any
	_ = json.Unmarshal(recStor.Body.Bytes(), &stor)
	snap := env.cfg.Snapshot()
	if got := stor["TranscodingTempFolder"]["Path"]; got != (&snap).TranscodeTempDir() {
		t.Fatalf("M-14 TranscodingTempFolder=%v, want %q", got, (&snap).TranscodeTempDir())
	}

	// Concurrent cfg.Update + handlers + KV RMW paths (-race verification)
	var wg sync.WaitGroup
	wg.Add(4)
	go func() {
		defer wg.Done()
		for i := 0; i < 20; i++ {
			env.cfg.Update(func(c *config.Config) {
				c.ServerName = fmt.Sprintf("Ambra-%d", i)
			})
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 15; i++ {
			r := httptest.NewRequest(http.MethodGet, "/System/Info/Storage", nil).WithContext(middleware.WithAuthContext(ctx, adminAC))
			env.router.ServeHTTP(httptest.NewRecorder(), r)
			_ = env.handler.toUserDto(env.admin)
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 15; i++ {
			r := httptest.NewRequest(http.MethodPost, "/Parties/Messages", strings.NewReader(`{"Text":"hi"}`)).
				WithContext(middleware.WithAuthContext(ctx, adminAC))
			env.router.ServeHTTP(httptest.NewRecorder(), r)
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 15; i++ {
			r := httptest.NewRequest(http.MethodPost, "/UserSettings/"+env.admin.ID+"/Partial", strings.NewReader(fmt.Sprintf(`{"k%d":%d}`, i, i))).
				WithContext(middleware.WithAuthContext(ctx, adminAC))
			env.router.ServeHTTP(httptest.NewRecorder(), r)
		}
	}()
	wg.Wait()

	// 4. [Task 1 / M-7] serveUserImage permission denied logs uid/gid and returns sanitized 403
	if runtime.GOOS != "windows" && os.Geteuid() != 0 {
		var logBuf bytes.Buffer
		oldLogger := slog.Default()
		slog.SetDefault(slog.New(slog.NewTextHandler(&logBuf, nil)))
		t.Cleanup(func() { slog.SetDefault(oldLogger) })

		imgPath := env.handler.userImagePath(env.admin.ID, "Primary", "0")
		_ = os.MkdirAll(filepath.Dir(imgPath), 0o755)
		_ = os.WriteFile(imgPath, []byte("IMG"), 0o644)
		_ = os.Chmod(imgPath, 0o000)
		t.Cleanup(func() { _ = os.Chmod(imgPath, 0o644) })

		recImg := httptest.NewRecorder()
		env.router.ServeHTTP(recImg, httptest.NewRequest(http.MethodGet, "/Users/"+env.admin.ID+"/Images/Primary", nil))
		if recImg.Code != http.StatusForbidden {
			t.Fatalf("serveUserImage permission denied expected 403, got %d body=%q", recImg.Code, recImg.Body.String())
		}
		if !strings.Contains(recImg.Body.String(), "permission denied reading media file") || strings.Contains(recImg.Body.String(), env.cfg.Snapshot().DataDir) {
			t.Fatalf("serveUserImage unexpected sanitized response: %q", recImg.Body.String())
		}
		logOut := logBuf.String()
		if !strings.Contains(logOut, "uid=") || !strings.Contains(logOut, "gid=") {
			t.Fatalf("serveUserImage expected uid/gid in slog output, got %q", logOut)
		}
	}

	// Verify serveUserItemExtras DB error is sanitized
	_ = env.store.Close()
	recEx := httptest.NewRecorder()
	reqEx := httptest.NewRequest(http.MethodGet, "/Users/"+env.admin.ID+"/Items/"+env.itemID+"/LocalTrailers", nil).
		WithContext(middleware.WithAuthContext(ctx, adminAC))
	env.router.ServeHTTP(recEx, reqEx)
	if recEx.Code != http.StatusInternalServerError || strings.Contains(recEx.Body.String(), "sql:") {
		t.Fatalf("expected sanitized 500 on closed DB, got %d %q", recEx.Code, recEx.Body.String())
	}
}
