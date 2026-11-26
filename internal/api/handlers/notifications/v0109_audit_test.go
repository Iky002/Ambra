package notifications

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/ambra-media/ambra/internal/api/middleware"
	"github.com/ambra-media/ambra/internal/core/session"
	"github.com/ambra-media/ambra/internal/pkg/eventbus"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestNotificationsV0109AuditM26M28(t *testing.T) {
	h, mux, st := setupNotifTest(t)
	ctx := context.Background()

	adminAuth := &middleware.AuthContext{User: &sqlite.UserRecord{ID: "admin-1", Name: "Admin", IsAdmin: true}}
	userAuth := &middleware.AuthContext{User: &sqlite.UserRecord{ID: "user-1", Name: "Alice", IsAdmin: false}}

	// 1. M-26: POST /Notifications/Admin defaults AdminOnly to true when omitted; explicit false broadcasts to non-admins
	rDefault := doReqWithAuth(t, mux, adminAuth, http.MethodPost, "/Notifications/Admin", map[string]any{
		"Name":  "Admin Secret Alert",
		"Level": "Error",
	})
	if rDefault.Code != http.StatusOK {
		t.Fatalf("POST /Notifications/Admin status=%d", rDefault.Code)
	}
	var nDef Notification
	_ = json.Unmarshal(rDefault.Body.Bytes(), &nDef)
	if !nDef.AdminOnly {
		t.Fatalf("M-26: expected AdminOnly=true by default, got %+v", nDef)
	}

	// Non-admin must NOT see default AdminOnly notification
	recUserList := doReqWithAuth(t, mux, userAuth, http.MethodGet, "/Notifications/user-1", nil)
	var listRes NotificationResult
	_ = json.Unmarshal(recUserList.Body.Bytes(), &listRes)
	if listRes.TotalRecordCount != 0 {
		t.Fatalf("M-26: non-admin saw default AdminOnly notification: %+v", listRes)
	}

	// Explicit AdminOnly=false is visible to non-admins
	rExplicit := doReqWithAuth(t, mux, adminAuth, http.MethodPost, "/Notifications/Admin", map[string]any{
		"Name":      "Public Notice",
		"Level":     "Normal",
		"AdminOnly": false,
	})
	if rExplicit.Code != http.StatusOK {
		t.Fatalf("POST /Notifications/Admin explicit false status=%d", rExplicit.Code)
	}
	var nPub Notification
	_ = json.Unmarshal(rExplicit.Body.Bytes(), &nPub)
	if nPub.AdminOnly {
		t.Fatalf("M-26: expected AdminOnly=false when explicitly set, got %+v", nPub)
	}

	// 2. M-26: EventUserDataChanged with empty UserId (struct & map branches) must be ignored and NOT broadcast
	bus := eventbus.New()
	h.SubscribeEvents(bus)
	h.mu.RLock()
	beforeCount := len(h.state.Items)
	h.mu.RUnlock()

	bus.Publish(ctx, eventbus.Event{
		Type: eventbus.EventUserDataChanged,
		Payload: session.UserDataChangedPayload{
			UserId:       "   ",
			UserDataList: []session.UserDataChangedEntry{{ItemId: "item-1", Played: true}},
		},
	})
	bus.Publish(ctx, eventbus.Event{
		Type: eventbus.EventUserDataChanged,
		Payload: map[string]any{
			"UserId": "",
			"Name":   "Leaked Broadcast",
		},
	})
	time.Sleep(100 * time.Millisecond)

	h.mu.RLock()
	afterCount := len(h.state.Items)
	h.mu.RUnlock()
	if afterCount != beforeCount {
		t.Fatalf("M-26: empty-UserId EventUserDataChanged must be ignored, before=%d after=%d", beforeCount, afterCount)
	}

	// 3. M-28: Rollback in-memory state and return 500 when persistence fails
	_ = st.Close()
	if r := doReqWithAuth(t, mux, adminAuth, http.MethodPost, "/Notifications/Admin", map[string]any{
		"Name": "Will Fail Persist",
	}); r.Code != http.StatusInternalServerError {
		t.Fatalf("M-28: expected 500 on append persistence failure, got %d", r.Code)
	}
	h.mu.RLock()
	if len(h.state.Items) != beforeCount {
		h.mu.RUnlock()
		t.Fatalf("M-28: appendNotification failed to roll back, got %d items", len(h.state.Items))
	}
	h.mu.RUnlock()

	if r := doReqWithAuth(t, mux, userAuth, http.MethodPost, "/Notifications/user-1/Read?Ids="+nPub.Id, nil); r.Code != http.StatusInternalServerError {
		t.Fatalf("M-28: expected 500 on mark-read persistence failure, got %d", r.Code)
	}
	h.mu.RLock()
	for _, it := range h.state.Items {
		if it.Id == nPub.Id && containsString(it.ReadBy, "user1") {
			h.mu.RUnlock()
			t.Fatal("M-28: mark-read failed to roll back ReadBy on DB failure")
		}
	}
	h.mu.RUnlock()
}
