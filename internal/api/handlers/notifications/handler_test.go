package notifications

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/ambra-media/ambra/internal/api/middleware"
	"github.com/ambra-media/ambra/internal/pkg/eventbus"
	"github.com/ambra-media/ambra/internal/pkg/i18n"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func setupNotifTest(t *testing.T) (*Handler, *http.ServeMux, *sqlite.Store) {
	t.Helper()
	st, err := sqlite.Open(filepath.Join(t.TempDir(), "ambra.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	h := New(st)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux, AuthFuncs{})
	return h, mux, st
}

func doReq(t *testing.T, mux *http.ServeMux, method, target string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		switch v := body.(type) {
		case string:
			buf.WriteString(v)
		default:
			_ = json.NewEncoder(&buf).Encode(v)
		}
	}
	req := httptest.NewRequest(method, target, &buf)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func TestNotificationsFlow(t *testing.T) {
	_, mux, st := setupNotifTest(t)

	// Types & Services
	if r := doReq(t, mux, http.MethodGet, "/Notifications/Types", nil); r.Code != http.StatusOK {
		t.Fatalf("GET /Notifications/Types status=%d", r.Code)
	}
	if r := doReq(t, mux, http.MethodGet, "/Notifications/Services", nil); r.Code != http.StatusOK {
		t.Fatalf("GET /Notifications/Services status=%d", r.Code)
	}

	// Admin send validation
	if r := doReq(t, mux, http.MethodPost, "/Notifications/Admin", "{bad"); r.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", r.Code)
	}
	if r := doReq(t, mux, http.MethodPost, "/Notifications/Admin", map[string]any{"Name": "  "}); r.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 on empty name, got %d", r.Code)
	}

	// Send broadcast Normal, user-1 Warning, user-2 Error
	r1 := doReq(t, mux, http.MethodPost, "/Notifications/Admin", map[string]any{
		"Name":        "Maintenance",
		"Description": "Scheduled reboot",
		"Level":       "Normal",
	})
	if r1.Code != http.StatusOK {
		t.Fatalf("POST /Notifications/Admin status=%d", r1.Code)
	}
	var n1 Notification
	_ = json.Unmarshal(r1.Body.Bytes(), &n1)

	r2 := doReq(t, mux, http.MethodPost, "/Notifications/Admin", map[string]any{
		"Name":   "Disk Space Low",
		"Level":  "Warning",
		"UserId": "user-1",
	})
	var n2 Notification
	_ = json.Unmarshal(r2.Body.Bytes(), &n2)

	_ = doReq(t, mux, http.MethodPost, "/Notifications/Admin", map[string]any{
		"Name":   "Transcode Failed",
		"Level":  "Error",
		"UserId": "user-2",
	})

	// List for user-1 (should see broadcast + user-1 = 2)
	res := doReq(t, mux, http.MethodGet, "/Notifications/user-1?StartIndex=0&Limit=10", nil)
	if res.Code != http.StatusOK {
		t.Fatalf("GET /Notifications/{UserId} status=%d", res.Code)
	}
	var listRes NotificationResult
	_ = json.Unmarshal(res.Body.Bytes(), &listRes)
	if listRes.TotalRecordCount != 2 {
		t.Fatalf("expected 2 notifications for user-1, got %d", listRes.TotalRecordCount)
	}

	// Summary for user-1 -> 2 unread, max level Warning
	res = doReq(t, mux, http.MethodGet, "/Notifications/user-1/Summary", nil)
	var sum NotificationsSummary
	_ = json.Unmarshal(res.Body.Bytes(), &sum)
	if sum.UnreadCount != 2 || sum.MaxUnreadNotificationLevel != "Warning" {
		t.Fatalf("unexpected summary for user-1: %+v", sum)
	}

	// Summary for user-2 -> 2 unread, max level Error
	res = doReq(t, mux, http.MethodGet, "/Notifications/user-2/Summary", nil)
	_ = json.Unmarshal(res.Body.Bytes(), &sum)
	if sum.UnreadCount != 2 || sum.MaxUnreadNotificationLevel != "Error" {
		t.Fatalf("unexpected summary for user-2: %+v", sum)
	}

	// Mark n2 read via query param
	if r := doReq(t, mux, http.MethodPost, "/Notifications/user-1/Read?Ids="+n2.Id, nil); r.Code != http.StatusNoContent {
		t.Fatalf("POST Read status=%d", r.Code)
	}
	res = doReq(t, mux, http.MethodGet, "/Notifications/user-1/Summary", nil)
	_ = json.Unmarshal(res.Body.Bytes(), &sum)
	if sum.UnreadCount != 1 || sum.MaxUnreadNotificationLevel != "Normal" {
		t.Fatalf("expected 1 Normal unread after marking n2 read, got %+v", sum)
	}

	// Mark n2 unread via JSON body
	if r := doReq(t, mux, http.MethodPost, "/Notifications/user-1/Unread", map[string]any{"Ids": []string{n2.Id}}); r.Code != http.StatusNoContent {
		t.Fatalf("POST Unread status=%d", r.Code)
	}
	res = doReq(t, mux, http.MethodGet, "/Notifications/user-1/Summary", nil)
	_ = json.Unmarshal(res.Body.Bytes(), &sum)
	if sum.UnreadCount != 2 {
		t.Fatalf("expected 2 unread after marking n2 unread, got %+v", sum)
	}

	// Mark all read for user-1 (empty Ids)
	if r := doReq(t, mux, http.MethodPost, "/Notifications/user-1/Read", nil); r.Code != http.StatusNoContent {
		t.Fatalf("POST Read all status=%d", r.Code)
	}
	res = doReq(t, mux, http.MethodGet, "/Notifications/user-1/Summary", nil)
	_ = json.Unmarshal(res.Body.Bytes(), &sum)
	if sum.UnreadCount != 0 {
		t.Fatalf("expected 0 unread, got %+v", sum)
	}

	// Reload from SQLite
	reloaded := New(st)
	if len(reloaded.state.Items) != 3 {
		t.Fatalf("expected 3 persisted notifications, got %d", len(reloaded.state.Items))
	}

	// Nil store & paginate edge cases
	_ = New(nil)
	p, tot := paginate([]int{1, 2, 3}, -1, 2)
	if tot != 3 || len(p) != 2 {
		t.Fatalf("unexpected paginate: %v %d", p, tot)
	}
	p, tot = paginate([]int{1, 2, 3}, 10, 2)
	if tot != 3 || len(p) != 0 {
		t.Fatalf("unexpected paginate beyond end: %v %d", p, tot)
	}
}

func doReqWithAuth(t *testing.T, mux *http.ServeMux, ac *middleware.AuthContext, method, target string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(method, target, &buf)
	req.Header.Set("Content-Type", "application/json")
	if ac != nil {
		req = req.WithContext(middleware.WithAuthContext(req.Context(), ac))
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func TestNotificationsIDORAndPerUserReadBy(t *testing.T) {
	_, mux, st := setupNotifTest(t)

	user1Auth := &middleware.AuthContext{
		User: &sqlite.UserRecord{ID: "user-1", Name: "Alice", IsAdmin: false},
	}
	user2Auth := &middleware.AuthContext{
		User: &sqlite.UserRecord{ID: "user-2", Name: "Bob", IsAdmin: false},
	}
	adminAuth := &middleware.AuthContext{
		User: &sqlite.UserRecord{ID: "admin-1", Name: "Admin", IsAdmin: true},
	}
	apiKeyAuth := &middleware.AuthContext{IsAPIKey: true}

	// Admin sends a global broadcast (UserId == "", AdminOnly == false)
	rBroadcast := doReqWithAuth(t, mux, adminAuth, http.MethodPost, "/Notifications/Admin", map[string]any{
		"Name":      "Server Upgrade",
		"Level":     "Warning",
		"AdminOnly": false,
	})
	if rBroadcast.Code != http.StatusOK {
		t.Fatalf("broadcast status=%d", rBroadcast.Code)
	}
	var broadcast Notification
	_ = json.Unmarshal(rBroadcast.Body.Bytes(), &broadcast)

	// 1. IDOR checks: user-1 cannot access or mutate user-2's notifications
	for _, tc := range []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/Notifications/user-2"},
		{http.MethodGet, "/Notifications/user-2/Summary"},
		{http.MethodPost, "/Notifications/user-2/Read"},
		{http.MethodPost, "/Notifications/user-2/Unread"},
	} {
		rec := doReqWithAuth(t, mux, user1Auth, tc.method, tc.path, nil)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("IDOR %s %s as user-1: got %d, want 403", tc.method, tc.path, rec.Code)
		}
	}

	// Admin & API key can access user-2's notifications
	if rec := doReqWithAuth(t, mux, adminAuth, http.MethodGet, "/Notifications/user-2", nil); rec.Code != http.StatusOK {
		t.Fatalf("admin access user-2: got %d, want 200", rec.Code)
	}
	if rec := doReqWithAuth(t, mux, apiKeyAuth, http.MethodGet, "/Notifications/user-2/Summary", nil); rec.Code != http.StatusOK {
		t.Fatalf("apikey access user-2 summary: got %d, want 200", rec.Code)
	}

	// 2. Per-user ReadBy isolation on global broadcast
	// Initially both user-1 and user-2 have 1 unread
	checkSummary := func(ac *middleware.AuthContext, uid string, wantUnread int) {
		t.Helper()
		rec := doReqWithAuth(t, mux, ac, http.MethodGet, "/Notifications/"+uid+"/Summary", nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("summary for %s: %d", uid, rec.Code)
		}
		var s NotificationsSummary
		_ = json.Unmarshal(rec.Body.Bytes(), &s)
		if s.UnreadCount != wantUnread {
			t.Fatalf("summary for %s: got unread=%d, want %d", uid, s.UnreadCount, wantUnread)
		}
	}
	checkListIsRead := func(ac *middleware.AuthContext, uid string, wantRead bool) {
		t.Helper()
		rec := doReqWithAuth(t, mux, ac, http.MethodGet, "/Notifications/"+uid, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("list for %s: %d", uid, rec.Code)
		}
		var res NotificationResult
		_ = json.Unmarshal(rec.Body.Bytes(), &res)
		if len(res.Notifications) != 1 || res.Notifications[0].IsRead != wantRead {
			t.Fatalf("list for %s: expected 1 notification with IsRead=%v, got %+v", uid, wantRead, res.Notifications)
		}
	}

	checkSummary(user1Auth, "user-1", 1)
	checkSummary(user2Auth, "user-2", 1)

	// user-1 marks broadcast read -> user-1 sees read, user-2 STILL sees unread!
	if rec := doReqWithAuth(t, mux, user1Auth, http.MethodPost, "/Notifications/user-1/Read?Ids="+broadcast.Id, nil); rec.Code != http.StatusNoContent {
		t.Fatalf("user-1 mark read: %d", rec.Code)
	}
	checkSummary(user1Auth, "user-1", 0)
	checkListIsRead(user1Auth, "user-1", true)
	checkSummary(user2Auth, "user-2", 1)
	checkListIsRead(user2Auth, "user-2", false)

	// user-2 marks broadcast read -> both see read
	if rec := doReqWithAuth(t, mux, user2Auth, http.MethodPost, "/Notifications/user-2/Read?Ids="+broadcast.Id, nil); rec.Code != http.StatusNoContent {
		t.Fatalf("user-2 mark read: %d", rec.Code)
	}
	checkSummary(user2Auth, "user-2", 0)
	checkListIsRead(user2Auth, "user-2", true)

	// user-1 marks broadcast unread -> user-1 sees unread, user-2 STILL sees read!
	if rec := doReqWithAuth(t, mux, user1Auth, http.MethodPost, "/Notifications/user-1/Unread", map[string]any{"Ids": []string{broadcast.Id}}); rec.Code != http.StatusNoContent {
		t.Fatalf("user-1 mark unread: %d", rec.Code)
	}
	checkSummary(user1Auth, "user-1", 1)
	checkListIsRead(user1Auth, "user-1", false)
	checkSummary(user2Auth, "user-2", 0)
	checkListIsRead(user2Auth, "user-2", true)

	// Persistence check after reload
	reloaded := New(st)
	if len(reloaded.state.Items) != 1 || !containsString(reloaded.state.Items[0].ReadBy, "user2") || containsString(reloaded.state.Items[0].ReadBy, "user1") {
		t.Fatalf("unexpected persisted ReadBy: %+v", reloaded.state.Items)
	}
}

func TestNotificationsIsReadFilterCapAndEventBus(t *testing.T) {
	h, mux, _ := setupNotifTest(t)

	// Send 2 notifications for user-1, mark 1 read
	r1 := doReq(t, mux, http.MethodPost, "/Notifications/Admin", map[string]any{"Name": "N1", "UserId": "user-1"})
	r2 := doReq(t, mux, http.MethodPost, "/Notifications/Admin", map[string]any{"Name": "N2", "UserId": "user-1"})
	var n1, n2 Notification
	_ = json.Unmarshal(r1.Body.Bytes(), &n1)
	_ = json.Unmarshal(r2.Body.Bytes(), &n2)
	_ = doReq(t, mux, http.MethodPost, "/Notifications/user-1/Read?Ids="+n1.Id, nil)

	// IsRead=true returns only N1
	rec := doReq(t, mux, http.MethodGet, "/Notifications/user-1?IsRead=true", nil)
	var readRes NotificationResult
	_ = json.Unmarshal(rec.Body.Bytes(), &readRes)
	if readRes.TotalRecordCount != 1 || readRes.Notifications[0].Id != n1.Id {
		t.Fatalf("expected only read notification N1, got %+v", readRes)
	}

	// IsRead=false returns only N2
	rec = doReq(t, mux, http.MethodGet, "/Notifications/user-1?IsRead=false", nil)
	var unreadRes NotificationResult
	_ = json.Unmarshal(rec.Body.Bytes(), &unreadRes)
	if unreadRes.TotalRecordCount != 1 || unreadRes.Notifications[0].Id != n2.Id {
		t.Fatalf("expected only unread notification N2, got %+v", unreadRes)
	}

	// SubscribeEvents: nil bus safe, real bus receives LibraryChanged, TaskProgress(Failed), UserDataChanged
	h.SubscribeEvents(nil)
	bus := eventbus.New()
	h.SubscribeEvents(bus)

	bus.Publish(context.Background(), eventbus.Event{
		Type: eventbus.EventLibraryChanged,
		Payload: map[string]any{
			"ItemsAdded":   3,
			"ItemsUpdated": 1,
			"ItemsDeleted": 0,
		},
	})
	bus.Publish(context.Background(), eventbus.Event{
		Type: eventbus.EventTaskProgress,
		Payload: map[string]any{
			"Name":  "Scan Library",
			"State": "Idle",
			"LastExecutionResult": map[string]any{
				"Status":       "Failed",
				"ErrorMessage": "disk read error",
			},
		},
	})
	bus.Publish(context.Background(), eventbus.Event{
		Type: eventbus.EventUserDataChanged,
		Payload: map[string]any{
			"UserId":      "user-1",
			"Name":        "User Event",
			"Description": "Custom user alert",
			"Level":       "Warning",
		},
	})

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		h.mu.RLock()
		n := len(h.state.Items)
		h.mu.RUnlock()
		if n >= 5 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	h.mu.RLock()
	gotCount := len(h.state.Items)
	h.mu.RUnlock()
	if gotCount < 5 {
		t.Fatalf("expected >=5 notifications after eventbus events, got %d", gotCount)
	}

	// Cap at 500 entries
	for i := 0; i < 510; i++ {
		h.appendNotification(context.Background(), Notification{
			Name:  fmt.Sprintf("Bulk-%d", i),
			Level: "Normal",
		})
	}
	h.mu.RLock()
	finalLen := len(h.state.Items)
	newestName := h.state.Items[0].Name
	h.mu.RUnlock()
	if finalLen != maxNotifications {
		t.Fatalf("expected notifications capped at %d, got %d", maxNotifications, finalLen)
	}
	if newestName != "Bulk-509" {
		t.Fatalf("expected newest notification Bulk-509, got %q", newestName)
	}
}

func TestNotificationsLocalization(t *testing.T) {
	h, mux, _ := setupNotifTest(t)
	ctx := context.Background()

	// Seed built-in system notifications and a custom notification
	h.appendNotification(ctx, Notification{
		Name:        "Library Updated",
		Description: "Media library scan completed",
		Level:       "Normal",
	})
	h.appendNotification(ctx, Notification{
		Name:        "Scheduled Task Failed",
		Description: "Scheduled Task failed",
		Level:       "Error",
	})
	h.appendNotification(ctx, Notification{
		Name:        "Scan Library Failed",
		Description: "Scan Library failed",
		Level:       "Error",
	})
	h.appendNotification(ctx, Notification{
		Name:        "Custom Admin Notice",
		Description: "Keep untouched",
		Level:       "Normal",
	})

	// Without Accept-Language, titles remain original
	rec := doReq(t, mux, http.MethodGet, "/Notifications/user-1", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /Notifications/user-1 status=%d", rec.Code)
	}
	var orig NotificationResult
	_ = json.Unmarshal(rec.Body.Bytes(), &orig)
	if len(orig.Notifications) != 4 {
		t.Fatalf("expected 4 notifications, got %d", len(orig.Notifications))
	}
	if orig.Notifications[3].Name != "Library Updated" {
		t.Fatalf("expected untranslated 'Library Updated' without Accept-Language, got %q", orig.Notifications[3].Name)
	}

	// With Accept-Language across multiple locales
	for _, loc := range []string{"zh-CN", "zh-TW", "en-US", "ja-JP", "th-TH"} {
		req := httptest.NewRequest(http.MethodGet, "/Notifications/user-1", nil)
		req.Header.Set("Accept-Language", loc)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET /Notifications/user-1 (%s) status=%d", loc, rec.Code)
		}
		var res NotificationResult
		if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
			t.Fatalf("unmarshal (%s): %v", loc, err)
		}
		if len(res.Notifications) != 4 {
			t.Fatalf("expected 4 notifications for %s, got %d", loc, len(res.Notifications))
		}
		// Order is newest-first: Custom, Scan Library Failed, Scheduled Task Failed, Library Updated
		if res.Notifications[0].Name != "Custom Admin Notice" || res.Notifications[0].Description != "Keep untouched" {
			t.Fatalf("locale %s custom notification mutated: %+v", loc, res.Notifications[0])
		}
		wantPrefixed := i18n.T(loc, "notif.task_failed.name") + " (Scan Library)"
		if res.Notifications[1].Name != wantPrefixed || res.Notifications[1].Description != i18n.T(loc, "notif.task_failed.desc") {
			t.Fatalf("locale %s prefixed task failed = %+v, want name %q", loc, res.Notifications[1], wantPrefixed)
		}
		if res.Notifications[2].Name != i18n.T(loc, "notif.task_failed.name") || res.Notifications[2].Description != i18n.T(loc, "notif.task_failed.desc") {
			t.Fatalf("locale %s task failed = %+v", loc, res.Notifications[2])
		}
		if localized := res.Notifications[3]; localized.Name != i18n.T(loc, "notif.library_updated.name") || localized.Description != i18n.T(loc, "notif.library_updated.desc") {
			t.Fatalf("locale %s library updated = %+v", loc, res.Notifications[3])
		}
	}
}

func TestNotificationsSecurityHardeningV0103(t *testing.T) {
	h, mux, _ := setupNotifTest(t)

	// 1. Reject javascript: / data: URLs in handleSendAdmin
	for _, bad := range []string{"javascript:alert(1)", "data:text/html,<h1>x</h1>", "//evil.example.com/a"} {
		if r := doReq(t, mux, http.MethodPost, "/Notifications/Admin", map[string]any{"Name": "XSS", "Url": bad}); r.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for dangerous Url %q, got %d", bad, r.Code)
		}
		if r := doReq(t, mux, http.MethodPost, "/Notifications/Admin", map[string]any{"Name": "XSS", "ImageUrl": bad}); r.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for dangerous ImageUrl %q, got %d", bad, r.Code)
		}
	}

	// 2. ReadBy must be omitted from list responses
	_ = doReq(t, mux, http.MethodPost, "/Notifications/Admin", map[string]any{"Name": "Broadcast", "Url": "/web/index.html"})
	_ = doReq(t, mux, http.MethodPost, "/Notifications/user-1/Read", nil)
	rec := doReq(t, mux, http.MethodGet, "/Notifications/user-1", nil)
	var listRes NotificationResult
	_ = json.Unmarshal(rec.Body.Bytes(), &listRes)
	if len(listRes.Notifications) != 1 || !listRes.Notifications[0].IsRead || len(listRes.Notifications[0].ReadBy) != 0 {
		t.Fatalf("expected ReadBy hidden while IsRead preserved, got %+v", listRes.Notifications)
	}

	// 3. TaskFailed notification triggers only on State=Idle and deduplicates by (Id, EndTimeUtc)
	bus := eventbus.New()
	h.SubscribeEvents(bus)
	before := len(h.state.Items)

	// Running state with previous Failed result must NOT trigger notification
	bus.Publish(context.Background(), eventbus.Event{
		Type: eventbus.EventTaskProgress,
		Payload: map[string]any{
			"Id":    "task-1",
			"Name":  "Backup",
			"State": "Running",
			"LastExecutionResult": map[string]any{
				"Status":     "Failed",
				"EndTimeUtc": "2026-11-25T08:00:00Z",
			},
		},
	})
	// Idle state with same (Id, EndTimeUtc) published twice must produce only 1 notification
	for i := 0; i < 2; i++ {
		bus.Publish(context.Background(), eventbus.Event{
			Type: eventbus.EventTaskProgress,
			Payload: map[string]any{
				"Id":    "task-1",
				"Name":  "Backup",
				"State": "Idle",
				"LastExecutionResult": map[string]any{
					"Status":     "Failed",
					"EndTimeUtc": "2026-11-25T08:00:00Z",
				},
			},
		})
	}
	time.Sleep(100 * time.Millisecond)
	h.mu.RLock()
	after := len(h.state.Items)
	h.mu.RUnlock()
	if after != before+1 {
		t.Fatalf("expected exactly 1 deduplicated task failure notification, before=%d after=%d", before, after)
	}
}

func TestNotificationsPurgeUserAndConfiguredHTTPChannelDelivery(t *testing.T) {
	h, mux, st := setupNotifTest(t)
	t.Cleanup(h.Close)
	ctx := context.Background()

	var receivedBody bytes.Buffer
	var receivedSig string
	doneCh := make(chan struct{}, 2)
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := json.Marshal(r.Header.Get("X-Ambra-Signature"))
		_ = json.Unmarshal(raw, &receivedSig)
		_, _ = receivedBody.ReadFrom(r.Body)
		select {
		case doneCh <- struct{}{}:
		default:
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer receiver.Close()

	h.SetHTTPClient(receiver.Client())

	// Seed compatext:state with an enabled HTTP notification service
	svcState := fmt.Sprintf(`{"notification_services":[{"Id":"svc-1","Name":"Webhook","Enabled":true,"Events":["LibraryChanged","AdminNotification"],"Options":{"Url":%q,"Secret":"notif-secret"}}]}`, receiver.URL)
	if _, err := st.Writer.ExecContext(ctx, `INSERT OR REPLACE INTO server_config(key, value_json, updated_at) VALUES('compatext:state', ?, 1)`, svcState); err != nil {
		t.Fatal(err)
	}

	bus := eventbus.New()
	h.SubscribeEvents(bus)
	bus.Publish(ctx, eventbus.Event{
		Type:    eventbus.EventLibraryChanged,
		Payload: map[string]any{"ItemsAdded": 5},
	})

	select {
	case <-doneCh:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for HTTP notification delivery from EventBus")
	}
	if !bytes.HasPrefix([]byte(receivedSig), []byte("sha256=")) {
		t.Fatalf("expected HMAC sha256 signature on notification HTTP POST, got %q", receivedSig)
	}

	// Test PurgeUser removes user-targeted notifications and strips ReadBy
	_ = doReq(t, mux, http.MethodPost, "/Notifications/Admin", map[string]any{"Name": "GlobalAlert", "AdminOnly": false})
	_ = doReq(t, mux, http.MethodPost, "/Notifications/Admin", map[string]any{"Name": "PrivateToDel", "UserId": "del-user"})
	_ = doReq(t, mux, http.MethodPost, "/Notifications/del-user/Read", nil)

	h.PurgeUser(ctx, "del-user")
	h.mu.RLock()
	for _, item := range h.state.Items {
		if item.UserId == "deluser" {
			h.mu.RUnlock()
			t.Fatal("expected deleted user's private notifications purged")
		}
		if containsString(item.ReadBy, "deluser") {
			h.mu.RUnlock()
			t.Fatal("expected deleted user stripped from ReadBy")
		}
	}
	h.mu.RUnlock()
}
