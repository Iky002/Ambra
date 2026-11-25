package notifications

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/ambra-media/ambra/internal/api/middleware"
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

	// Admin sends a global broadcast (UserId == "")
	rBroadcast := doReqWithAuth(t, mux, adminAuth, http.MethodPost, "/Notifications/Admin", map[string]any{
		"Name":  "Server Upgrade",
		"Level": "Warning",
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
