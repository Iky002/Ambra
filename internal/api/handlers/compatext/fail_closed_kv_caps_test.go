package compatext

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ambra-media/ambra/internal/api/middleware"
	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/core/auth"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestCompatExtV0104FailClosedAndKVCaps(t *testing.T) {
	tmp := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmp, "ce_v0104.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	ctx := context.Background()
	userRepo := sqlite.NewUserRepo(store)
	libRepo := sqlite.NewLibraryRepo(store)
	cfg := config.Default(tmp)
	authSvc := auth.NewService(userRepo, cfg)

	alice, _ := authSvc.CreateUser(ctx, "alice", "alice123", false)
	bob, _ := authSvc.CreateUser(ctx, "bob", "bob123", false)
	aliceAuth, _ := authSvc.AuthenticateByName(ctx, "alice", "alice123", "", auth.ClientInfo{DeviceID: "d-ali"})

	lib, _ := libRepo.CreateLibrary(ctx, "Movies", "movies", []string{tmp})
	movie := &sqlite.ItemRecord{ID: guid.New(), LibraryID: lib.ID, ParentID: lib.ID, Type: "Movie", Name: "M1"}
	_ = libRepo.UpsertItem(ctx, movie)
	bobPrivPl, _ := libRepo.CreatePlaylist(ctx, "BobSecret", "Audio", bob.ID, nil)

	h := New(store, cfg)
	h.SetUserRepo(userRepo)
	h.SetLibraryRepo(libRepo)

	authMW := middleware.NewAuth(authSvc, nil)
	router := h.BuildRouter(AuthFuncs{
		RequireAuth:  authMW.RequireAuth,
		RequireAdmin: authMW.RequireAdmin,
		UserIDFromCtx: func(c context.Context) string {
			if ac, ok := middleware.AuthFromContext(c); ok && ac != nil {
				return ac.UserID()
			}
			return ""
		},
	})

	// 1. [MH-08] Fail-closed on DB errors and private playlist check in canAccessItemID
	aliceReq := httptest.NewRequest(http.MethodGet, "/", nil).WithContext(
		middleware.WithAuthContext(ctx, &middleware.AuthContext{User: alice}),
	)
	if h.canAccessItemID(aliceReq, bobPrivPl.ID) {
		t.Fatal("expected canAccessItemID to deny access to another user's private playlist")
	}

	cancelledCtx, cancel := context.WithCancel(ctx)
	cancel()
	cancelledReq := httptest.NewRequest(http.MethodGet, "/", nil).WithContext(
		middleware.WithAuthContext(cancelledCtx, &middleware.AuthContext{User: alice}),
	)
	if h.canAccessItemID(cancelledReq, movie.ID) {
		t.Fatal("expected canAccessItemID fail-closed (false) on cancelled DB ctx")
	}
	if h.hasLiveTvAccess(cancelledReq) {
		t.Fatal("expected hasLiveTvAccess fail-closed (false) on cancelled DB ctx")
	}
	if h.canDownloadContent(cancelledReq) {
		t.Fatal("expected canDownloadContent fail-closed (false) on cancelled DB ctx")
	}

	// 2. RemovedJobItems: non-existent job items are NOT inserted; real job items enforce 500-entry cap
	h.mu.Lock()
	for i := 0; i < maxRemovedJobItems; i++ {
		h.state.RemovedJobItems[fmt.Sprintf("job-item-%d", i)] = true
	}
	h.mu.Unlock()

	reqGhost := httptest.NewRequest(http.MethodPost, "/Sync/JobItems/ghost-item/MarkForRemoval", nil)
	reqGhost.Header.Set("X-Emby-Token", aliceAuth.AccessToken)
	recGhost := httptest.NewRecorder()
	router.ServeHTTP(recGhost, reqGhost)
	if recGhost.Code != http.StatusNoContent {
		t.Fatalf("non-existent MarkForRemoval expected 204 no-op, got %d", recGhost.Code)
	}
	h.mu.RLock()
	if h.state.RemovedJobItems["ghost-item"] {
		h.mu.RUnlock()
		t.Fatal("non-existent job item must not be inserted into RemovedJobItems")
	}
	h.mu.RUnlock()

	// Seed a real sync job item owned by alice, then 501st RemovedJobItems entry returns 400
	h.writeConfigJSON(ctx, syncConfigKey, sharedSyncState{
		Jobs:     []sharedSyncJob{{Id: "j-ovf", UserId: alice.ID}},
		JobItems: []sharedSyncJobItem{{Id: "job-item-overflow", JobId: "j-ovf", ItemId: movie.ID}},
	})
	reqOverflow := httptest.NewRequest(http.MethodPost, "/Sync/JobItems/job-item-overflow/MarkForRemoval", nil)
	reqOverflow.Header.Set("X-Emby-Token", aliceAuth.AccessToken)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, reqOverflow)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("501st RemovedJobItems entry expected 400, got %d (%s)", rec.Code, rec.Body.String())
	}

	// 3. NotificationServices body size cap (64KiB) and 500-entry cap
	bigNotif := `{"Name":"` + strings.Repeat("N", maxNotificationBodyBytes+1024) + `"}`
	reqBig := httptest.NewRequest(http.MethodPost, "/Notifications/Services/Configured", strings.NewReader(bigNotif))
	reqBig.Header.Set("X-Emby-Token", aliceAuth.AccessToken)
	recBig := httptest.NewRecorder()
	router.ServeHTTP(recBig, reqBig)
	if recBig.Code != http.StatusRequestEntityTooLarge && recBig.Code != http.StatusBadRequest {
		t.Fatalf("oversized NotificationService expected 413/400, got %d", recBig.Code)
	}

	h.mu.Lock()
	h.state.NotificationServices = make([]NotificationServiceConfig, maxNotificationServices)
	for i := range h.state.NotificationServices {
		h.state.NotificationServices[i] = NotificationServiceConfig{Id: fmt.Sprintf("ns-%d", i), UserId: alice.ID}
	}
	h.mu.Unlock()

	reqNotifCap := httptest.NewRequest(http.MethodPost, "/Notifications/Services/Configured", strings.NewReader(`{"Id":"ns-overflow","Name":"TooMany"}`))
	reqNotifCap.Header.Set("X-Emby-Token", aliceAuth.AccessToken)
	recCap := httptest.NewRecorder()
	router.ServeHTTP(recCap, reqNotifCap)
	if recCap.Code != http.StatusBadRequest {
		t.Fatalf("501st NotificationService expected 400, got %d (%s)", recCap.Code, recCap.Body.String())
	}
}
