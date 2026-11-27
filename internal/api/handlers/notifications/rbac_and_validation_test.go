package notifications

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ambra-media/ambra/internal/api/middleware"
	"github.com/ambra-media/ambra/internal/core/session"
	"github.com/ambra-media/ambra/internal/pkg/eventbus"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestNotificationsV0104AdminOnlyUserDataChangedAndMalformedRead(t *testing.T) {
	h, mux, _ := setupNotifTest(t)

	bus := eventbus.New()
	h.SubscribeEvents(bus)

	user1Auth := &middleware.AuthContext{
		User: &sqlite.UserRecord{ID: "user-1", Name: "Alice", IsAdmin: false},
	}
	adminAuth := &middleware.AuthContext{
		User: &sqlite.UserRecord{ID: "admin-1", Name: "Admin", IsAdmin: true},
	}

	// 1. Task failure notification is marked AdminOnly=true
	bus.Publish(context.Background(), eventbus.Event{
		Type: eventbus.EventTaskProgress,
		Payload: map[string]any{
			"Id":    "task-secret",
			"Name":  "Database Vacuum",
			"State": "Idle",
			"LastExecutionResult": map[string]any{
				"Status":       "Failed",
				"ErrorMessage": "internal sqlite error at /var/lib/ambra/ambra.db",
				"EndTimeUtc":   "2026-11-25T10:00:00Z",
			},
		},
	})

	// 2. EventUserDataChanged handles value, pointer, nil pointer, and map[string]any
	bus.Publish(context.Background(), eventbus.Event{
		Type: eventbus.EventUserDataChanged,
		Payload: session.UserDataChangedPayload{
			UserId: "user-1",
			UserDataList: []session.UserDataChangedEntry{
				{ItemId: "movie-1", Played: true},
			},
		},
	})
	bus.Publish(context.Background(), eventbus.Event{
		Type: eventbus.EventUserDataChanged,
		Payload: &session.UserDataChangedPayload{
			UserId: "user-1",
			UserDataList: []session.UserDataChangedEntry{
				{ItemId: "movie-2", IsFavorite: true},
			},
		},
	})
	bus.Publish(context.Background(), eventbus.Event{
		Type:    eventbus.EventUserDataChanged,
		Payload: (*session.UserDataChangedPayload)(nil),
	})
	bus.Publish(context.Background(), eventbus.Event{
		Type: eventbus.EventUserDataChanged,
		Payload: session.UserDataChangedPayload{
			UserId: "user-1",
			UserDataList: []session.UserDataChangedEntry{
				{ItemId: "movie-3"},
				{ItemId: "movie-4"},
			},
		},
	})

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		h.mu.RLock()
		n := len(h.state.Items)
		h.mu.RUnlock()
		if n >= 4 {
			break
		}
		time.Sleep(15 * time.Millisecond)
	}

	// Non-admin user-1 MUST see only the 3 UserDataChanged notifications, NOT the AdminOnly task failure!
	recListUser := doReqWithAuth(t, mux, user1Auth, http.MethodGet, "/Notifications/user-1", nil)
	if recListUser.Code != http.StatusOK {
		t.Fatalf("user-1 list status=%d", recListUser.Code)
	}
	var userList NotificationResult
	_ = json.Unmarshal(recListUser.Body.Bytes(), &userList)
	if userList.TotalRecordCount != 3 {
		t.Fatalf("expected 3 non-admin notifications for user-1, got %d (%+v)", userList.TotalRecordCount, userList.Notifications)
	}
	for _, n := range userList.Notifications {
		if n.AdminOnly {
			t.Fatalf("AdminOnly notification leaked to non-admin: %+v", n)
		}
	}

	recSumUser := doReqWithAuth(t, mux, user1Auth, http.MethodGet, "/Notifications/user-1/Summary", nil)
	var userSum NotificationsSummary
	_ = json.Unmarshal(recSumUser.Body.Bytes(), &userSum)
	if userSum.UnreadCount != 3 || userSum.MaxUnreadNotificationLevel != "Normal" {
		t.Fatalf("expected user-1 summary UnreadCount=3 Normal (excluding AdminOnly Error), got %+v", userSum)
	}

	// Admin CAN see the AdminOnly task failure notification in list and summary
	recListAdmin := doReqWithAuth(t, mux, adminAuth, http.MethodGet, "/Notifications/admin-1", nil)
	var adminList NotificationResult
	_ = json.Unmarshal(recListAdmin.Body.Bytes(), &adminList)
	if adminList.TotalRecordCount != 1 || !adminList.Notifications[0].AdminOnly {
		t.Fatalf("expected admin to see 1 AdminOnly notification, got %+v", adminList)
	}
	recSumAdmin := doReqWithAuth(t, mux, adminAuth, http.MethodGet, "/Notifications/admin-1/Summary", nil)
	var adminSum NotificationsSummary
	_ = json.Unmarshal(recSumAdmin.Body.Bytes(), &adminSum)
	if adminSum.UnreadCount != 1 || adminSum.MaxUnreadNotificationLevel != "Error" {
		t.Fatalf("expected admin summary UnreadCount=1 Error, got %+v", adminSum)
	}

	// 3. POST /Notifications/{UserId}/Read rejects invalid JSON with 400 and does NOT silently mark all read!
	for _, badBody := range []string{"{bad-json", `{"Ids": "not-an-array"}`} {
		req := doReqWithAuthRaw(t, mux, user1Auth, http.MethodPost, "/Notifications/user-1/Read", badBody)
		if req.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for malformed JSON %q, got %d", badBody, req.Code)
		}
	}
	// Verify all 3 notifications for user-1 are STILL unread!
	recSumAfterBad := doReqWithAuth(t, mux, user1Auth, http.MethodGet, "/Notifications/user-1/Summary", nil)
	userSum = NotificationsSummary{}
	_ = json.Unmarshal(recSumAfterBad.Body.Bytes(), &userSum)
	if userSum.UnreadCount != 3 {
		t.Fatalf("malformed JSON must not mark notifications read; got UnreadCount=%d", userSum.UnreadCount)
	}

	// Empty body DOES mark all visible notifications read (204)
	if r := doReqWithAuthRaw(t, mux, user1Auth, http.MethodPost, "/Notifications/user-1/Read", ""); r.Code != http.StatusNoContent {
		t.Fatalf("expected 204 for empty body mark-all-read, got %d", r.Code)
	}
	recSumAfterAll := doReqWithAuth(t, mux, user1Auth, http.MethodGet, "/Notifications/user-1/Summary", nil)
	userSum = NotificationsSummary{}
	_ = json.Unmarshal(recSumAfterAll.Body.Bytes(), &userSum)
	if userSum.UnreadCount != 0 {
		t.Fatalf("expected 0 unread after empty-body Read, got %+v", userSum)
	}
}

func doReqWithAuthRaw(t *testing.T, mux *http.ServeMux, ac *middleware.AuthContext, method, target, rawBody string) *httptest.ResponseRecorder {
	t.Helper()
	var req *http.Request
	if rawBody == "" {
		req = httptest.NewRequest(method, target, nil)
	} else {
		req = httptest.NewRequest(method, target, bytes.NewBufferString(rawBody))
	}
	req.Header.Set("Content-Type", "application/json")
	if ac != nil {
		req = req.WithContext(middleware.WithAuthContext(req.Context(), ac))
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}
