package notifications

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ambra-media/ambra/internal/api/middleware"
	"github.com/ambra-media/ambra/internal/pkg/eventbus"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestNotificationsV0107AuditBucketIsolationAndLibraryChangedAdminOnly(t *testing.T) {
	h, mux, _ := setupNotifTest(t)
	ctx := context.Background()

	// 1. H-14: AdminOnly alerts cannot be evicted by a flood of user notifications
	_ = h.appendNotification(ctx, Notification{
		Id:        "critical-admin-alert",
		Name:      "Disk Failure",
		Level:     "Error",
		AdminOnly: true,
	})
	for i := 0; i < maxNotifications+100; i++ {
		_ = h.appendNotification(ctx, Notification{
			UserId: "user-spammer",
			Name:   fmt.Sprintf("Spam-%d", i),
			Level:  "Normal",
		})
	}
	h.mu.RLock()
	foundAdminAlert := false
	for _, n := range h.state.Items {
		if n.Id == "critical-admin-alert" {
			foundAdminAlert = true
			break
		}
	}
	totalLen := len(h.state.Items)
	h.mu.RUnlock()
	if !foundAdminAlert {
		t.Fatal("AdminOnly alert was evicted by non-admin user notification flood")
	}
	if totalLen > maxNotifications {
		t.Fatalf("expected total notifications <= %d, got %d", maxNotifications, totalLen)
	}

	// AdminOnly bucket itself is also capped at maxAdminNotifications (200)
	for i := 0; i < maxAdminNotifications+20; i++ {
		_ = h.appendNotification(ctx, Notification{
			Name:      fmt.Sprintf("Admin-%d", i),
			Level:     "Warning",
			AdminOnly: true,
		})
	}
	h.mu.RLock()
	adminCount := 0
	for _, n := range h.state.Items {
		if n.AdminOnly {
			adminCount++
		}
	}
	h.mu.RUnlock()
	if adminCount != maxAdminNotifications {
		t.Fatalf("expected AdminOnly bucket capped at %d, got %d", maxAdminNotifications, adminCount)
	}

	// 2. H-14: EventLibraryChanged is AdminOnly=true and toInt supports []string / []any
	if toInt([]string{"a", "b", "c"}) != 3 {
		t.Fatalf("expected toInt([]string) == 3")
	}
	if toInt([]any{"x", "y"}) != 2 {
		t.Fatalf("expected toInt([]any) == 2")
	}

	h.mu.Lock()
	h.state.Items = nil
	h.mu.Unlock()
	bus := eventbus.New()
	h.SubscribeEvents(bus)
	bus.Publish(ctx, eventbus.Event{
		Type: eventbus.EventLibraryChanged,
		Payload: map[string]any{
			"ItemsAdded":   []string{"id1", "id2"},
			"ItemsUpdated": []string{"id3"},
			"ItemsDeleted": []any{},
		},
	})
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		h.mu.RLock()
		n := len(h.state.Items)
		h.mu.RUnlock()
		if n > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	h.mu.RLock()
	if len(h.state.Items) != 1 || !h.state.Items[0].AdminOnly || !strings.Contains(h.state.Items[0].Description, "2 added, 1 updated, 0 deleted") {
		h.mu.RUnlock()
		t.Fatalf("unexpected LibraryChanged notification: %+v", h.state.Items)
	}
	h.mu.RUnlock()

	// Non-admin user cannot see EventLibraryChanged summary notification
	viewer := &sqlite.UserRecord{ID: "u-viewer", Name: "viewer", Policy: sqlite.DefaultUserPolicy(false)}
	req := httptest.NewRequest(http.MethodGet, "/Notifications/u-viewer", nil).WithContext(
		middleware.WithAuthContext(ctx, &middleware.AuthContext{User: viewer}),
	)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	var list NotificationResult
	_ = json.Unmarshal(rec.Body.Bytes(), &list)
	if list.TotalRecordCount != 0 {
		t.Fatalf("non-admin must not see AdminOnly LibraryChanged notification, got %+v", list)
	}
}
