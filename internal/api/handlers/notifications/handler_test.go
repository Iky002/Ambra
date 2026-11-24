package notifications

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

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
