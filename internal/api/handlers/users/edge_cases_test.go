package users

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/ambra-media/ambra/internal/api/dto"
	"github.com/ambra-media/ambra/internal/api/middleware"
	"github.com/ambra-media/ambra/internal/api/ws"
	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/core/auth"
	"github.com/ambra-media/ambra/internal/core/session"
	"github.com/ambra-media/ambra/internal/pkg/eventbus"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

type failingBodyReader struct{}

func (failingBodyReader) Read(_ []byte) (int, error) {
	return 0, errors.New("simulated body read failure")
}

func TestUsersCoverageBoostV0103(t *testing.T) {
	tmp := t.TempDir()
	cfg, err := config.Load(filepath.Join(tmp, "data"))
	if err != nil {
		t.Fatal(err)
	}
	store, err := sqlite.Open(cfg.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	ctx := context.Background()
	userRepo := sqlite.NewUserRepo(store)
	libRepo := sqlite.NewLibraryRepo(store)
	authSvc := auth.NewService(userRepo, cfg)
	bus := eventbus.New()
	sessMgr := session.NewManager(userRepo, cfg, bus)
	wsHub := ws.NewHub(authSvc, sessMgr, bus)

	admin1, err := authSvc.CreateUser(ctx, "admin1", "adminpass1", true)
	if err != nil {
		t.Fatal(err)
	}
	admin2, err := authSvc.CreateUser(ctx, "admin2", "adminpass2", true)
	if err != nil {
		t.Fatal(err)
	}
	viewer, err := authSvc.CreateUser(ctx, "viewer1", "viewerpass1", false)
	if err != nil {
		t.Fatal(err)
	}

	admin1Auth, err := authSvc.AuthenticateByName(ctx, "admin1", "adminpass1", "", auth.ClientInfo{DeviceID: "dev-adm1", Client: "Web"})
	if err != nil {
		t.Fatal(err)
	}
	viewerAuth, err := authSvc.AuthenticateByName(ctx, "viewer1", "viewerpass1", "", auth.ClientInfo{DeviceID: "dev-vw1", Client: "Web"})
	if err != nil {
		t.Fatal(err)
	}
	apiKeyRec, err := authSvc.CreateAPIKey(ctx, "Automation")
	if err != nil {
		t.Fatal(err)
	}

	lib1, _ := libRepo.CreateLibrary(ctx, "Movies", "movies", []string{filepath.Join(tmp, "movies")})
	movie := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: lib1.ID, ParentID: lib1.ID,
		Type: "Movie", MediaType: "Video", Name: "Allowed Movie", OfficialRating: "PG",
	}
	if err := libRepo.UpsertItem(ctx, movie); err != nil {
		t.Fatal(err)
	}

	h := NewHandler(authSvc, userRepo, sessMgr, wsHub, cfg)
	h.SetLibraryRepo(libRepo)

	r := chi.NewRouter()
	authMW := middleware.NewAuth(authSvc, sessMgr)
	RegisterRoutes(r, authMW, h)

	// 1. checkAuthRateLimit edge cases: empty IP, nil map init, pruning expired entries
	t.Run("checkAuthRateLimit_edge_cases", func(t *testing.T) {
		hLocal := &Handler{} // authRates == nil
		now := time.Now().UTC()
		if !hLocal.checkAuthRateLimit("", now) {
			t.Fatal("expected empty IP to bypass rate limit")
		}
		if !hLocal.checkAuthRateLimit("203.0.113.10", now) {
			t.Fatal("expected first attempt to succeed")
		}
		// Advance past window so old bucket is pruned & deleted
		if !hLocal.checkAuthRateLimit("203.0.113.11", now.Add(2*authByNameRateWindow)) {
			t.Fatal("expected attempt after window expiry to succeed")
		}
		if _, exists := hLocal.authRates["203.0.113.10"]; exists {
			t.Fatal("expected expired IP bucket to be pruned from authRates")
		}
	})

	// 2. canAccessItem direct branches: nil ac, admin ac, empty itemID, not found, nil User, DB error
	t.Run("canAccessItem_branches", func(t *testing.T) {
		if ok, err := h.canAccessItem(ctx, nil, movie.ID); err != nil || ok {
			t.Fatalf("expected (false, nil) for nil ac, got (%v, %v)", ok, err)
		}
		if ok, err := h.canAccessItem(ctx, &middleware.AuthContext{User: admin1}, movie.ID); err != nil || !ok {
			t.Fatalf("expected (true, nil) for admin, got (%v, %v)", ok, err)
		}
		if ok, err := h.canAccessItem(ctx, &middleware.AuthContext{User: viewer}, ""); err != nil || !ok {
			t.Fatalf("expected (true, nil) for empty itemID, got (%v, %v)", ok, err)
		}
		if ok, err := h.canAccessItem(ctx, &middleware.AuthContext{User: viewer}, guid.New()); err != nil || ok {
			t.Fatalf("expected (false, nil) for missing item, got (%v, %v)", ok, err)
		}
		if ok, err := h.canAccessItem(ctx, &middleware.AuthContext{User: nil, IsAPIKey: false}, movie.ID); err != nil || ok {
			t.Fatalf("expected (false, nil) for non-admin nil User, got (%v, %v)", ok, err)
		}
		// Cancelled context triggers DB error on GetItemRecordByID
		cancelledCtx, cancel := context.WithCancel(ctx)
		cancel()
		if ok, err := h.canAccessItem(cancelledCtx, &middleware.AuthContext{User: viewer}, movie.ID); err == nil || ok {
			t.Fatalf("expected error for cancelled ctx, got (%v, %v)", ok, err)
		}
	})

	// 3. disconnectUserSessions empty ID and countOtherActiveAdmins branches
	t.Run("disconnect_and_countOtherActiveAdmins", func(t *testing.T) {
		h.disconnectUserSessions(ctx, "") // early return

		hNoRepo := &Handler{}
		n, err := hNoRepo.countOtherActiveAdmins(ctx, admin1.ID)
		if err != nil || n != 0 {
			t.Fatalf("expected (0, nil) for nil userRepo, got (%d, %v)", n, err)
		}

		// With 2 active admins, excluding admin1 yields 1
		n, err = h.countOtherActiveAdmins(ctx, admin1.ID)
		if err != nil || n != 1 {
			t.Fatalf("expected 1 other active admin, got (%d, %v)", n, err)
		}

		cancelledCtx, cancel := context.WithCancel(ctx)
		cancel()
		if _, err := h.countOtherActiveAdmins(cancelledCtx, admin1.ID); err == nil {
			t.Fatal("expected error from countOtherActiveAdmins with cancelled ctx")
		}
	})

	// 4. Direct calls without AuthContext verify defense-in-depth 401/403 guards on all handlers
	t.Run("missing_auth_context_guards", func(t *testing.T) {
		unauthCases := []struct {
			name   string
			method string
			path   string
			body   string
			fn     http.HandlerFunc
			want   int
		}{
			{"ListUsers", http.MethodGet, "/Users", "", h.ListUsers, http.StatusUnauthorized},
			{"GetUser", http.MethodGet, "/Users/" + viewer.ID, "", h.GetUser, http.StatusUnauthorized},
			{"DeleteUser", http.MethodDelete, "/Users/" + viewer.ID, "", h.DeleteUser, http.StatusForbidden},
			{"UpdateUserPassword", http.MethodPost, "/Users/" + viewer.ID + "/Password", `{}`, h.UpdateUserPassword, http.StatusUnauthorized},
			{"UpdateUserConfiguration", http.MethodPost, "/Users/" + viewer.ID + "/Configuration", `{}`, h.UpdateUserConfiguration, http.StatusUnauthorized},
			{"ListSessions", http.MethodGet, "/Sessions", "", h.ListSessions, http.StatusUnauthorized},
			{"Logout", http.MethodPost, "/Sessions/Logout", "", h.Logout, http.StatusUnauthorized},
			{"UpdateCapabilities", http.MethodPost, "/Sessions/Capabilities", `{}`, h.UpdateCapabilities, http.StatusUnauthorized},
			{"ReportPlaybackStart", http.MethodPost, "/Sessions/Playing", `{}`, h.ReportPlaybackStart, http.StatusUnauthorized},
			{"ReportPlaybackProgress", http.MethodPost, "/Sessions/Playing/Progress", `{}`, h.ReportPlaybackProgress, http.StatusUnauthorized},
			{"ReportPlaybackStopped", http.MethodPost, "/Sessions/Playing/Stopped", `{}`, h.ReportPlaybackStopped, http.StatusUnauthorized},
			{"ListDevices", http.MethodGet, "/Devices", "", h.ListDevices, http.StatusUnauthorized},
			{"GetDisplayPreferences", http.MethodGet, "/DisplayPreferences/usersettings", "", h.GetDisplayPreferences, http.StatusUnauthorized},
			{"UpdateDisplayPreferences", http.MethodPost, "/DisplayPreferences/usersettings", `{}`, h.UpdateDisplayPreferences, http.StatusUnauthorized},
		}
		for _, tc := range unauthCases {
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			rec := httptest.NewRecorder()
			tc.fn(rec, req)
			if rec.Code != tc.want {
				t.Fatalf("%s without auth ctx: want %d, got %d (%s)", tc.name, tc.want, rec.Code, rec.Body.String())
			}
		}
	})

	// 5. AuthenticateByName with empty username (activity log defaults to "unknown")
	t.Run("authenticate_empty_username", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/Users/AuthenticateByName", strings.NewReader(`{"Username":"   ","Pw":"wrong"}`))
		req.RemoteAddr = "203.0.113.55:1234"
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401 for empty username, got %d", rec.Code)
		}
	})

	// 6. DeleteUser: delete second active admin succeeds, then deleting last admin via API key fails with 400
	t.Run("delete_admin_and_last_admin_guard", func(t *testing.T) {
		// Delete admin2 while admin1 is still active -> 204
		req := httptest.NewRequest(http.MethodDelete, "/Users/"+admin2.ID, nil)
		req.Header.Set("X-Emby-Token", admin1Auth.AccessToken)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusNoContent {
			t.Fatalf("expected 204 deleting second admin, got %d (%s)", rec.Code, rec.Body.String())
		}

		// Now admin1 is the ONLY active admin; try deleting admin1 using API key (not self-delete!) -> 400
		req2 := httptest.NewRequest(http.MethodDelete, "/Users/"+admin1.ID, nil)
		req2.Header.Set("X-Emby-Token", apiKeyRec.Token)
		rec2 := httptest.NewRecorder()
		r.ServeHTTP(rec2, req2)
		if rec2.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 deleting last admin via API key, got %d (%s)", rec2.Code, rec2.Body.String())
		}
		if !strings.Contains(rec2.Body.String(), "last administrator") {
			t.Fatalf("expected last administrator message, got %q", rec2.Body.String())
		}
	})

	// 7. UpdateUserPolicy: demoting a second admin succeeds and disconnects their sessions
	t.Run("demote_second_admin_succeeds", func(t *testing.T) {
		admin3, err := authSvc.CreateUser(ctx, "admin3", "adminpass3", true)
		if err != nil {
			t.Fatal(err)
		}
		sessMgr.TouchSession(admin3, auth.ClientInfo{DeviceID: "dev-adm3", Client: "Web"})
		req := httptest.NewRequest(http.MethodPost, "/Users/"+admin3.ID+"/Policy", strings.NewReader(`{"IsAdministrator":false,"IsDisabled":false,"EnableAllFolders":true}`))
		req.Header.Set("X-Emby-Token", admin1Auth.AccessToken)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusNoContent {
			t.Fatalf("expected 204 demoting admin3, got %d (%s)", rec.Code, rec.Body.String())
		}
	})

	// 8. ListSessions filtering by ControllableByUserId & ActiveWithinSeconds
	t.Run("list_sessions_filters", func(t *testing.T) {
		sessMgr.TouchSession(admin1, auth.ClientInfo{DeviceID: "dev-adm1", Client: "Web"})
		sessMgr.TouchSession(viewer, auth.ClientInfo{DeviceID: "dev-vw1", Client: "Web"})

		req := httptest.NewRequest(http.MethodGet, "/Sessions?ControllableByUserId="+viewer.ID+"&ActiveWithinSeconds=60", nil)
		req.Header.Set("X-Emby-Token", admin1Auth.AccessToken)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
		var sessions []dto.SessionInfo
		if err := json.Unmarshal(rec.Body.Bytes(), &sessions); err != nil {
			t.Fatal(err)
		}
		if len(sessions) != 1 || guid.Normalize(sessions[0].UserId) != guid.Normalize(viewer.ID) {
			t.Fatalf("expected only viewer session, got %+v", sessions)
		}
	})

	// 9. UpdateCapabilities query param SupportsRemoteControl fallback
	t.Run("update_capabilities_query_SupportsRemoteControl", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/Sessions/Capabilities?SupportsRemoteControl=true&PlayableMediaTypes=Video&SupportedCommands=Play", strings.NewReader(`{}`))
		req.Header.Set("X-Emby-Token", viewerAuth.AccessToken)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusNoContent {
			t.Fatalf("expected 204, got %d", rec.Code)
		}
	})

	// 10. ReportPlaybackStart nested Item.Id fallback & playback DB error handling (500)
	t.Run("playback_nested_item_and_db_error", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/Sessions/Playing", strings.NewReader(`{"Item":{"Id":"`+movie.ID+`"}}`))
		req.Header.Set("X-Emby-Token", viewerAuth.AccessToken)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusNoContent {
			t.Fatalf("expected 204 for nested Item.Id, got %d (%s)", rec.Code, rec.Body.String())
		}

		// Attach a separate closed store for libRepo to exercise 500 error branches in Playback Start/Progress/Stopped
		tmpStore2, err := sqlite.Open(filepath.Join(tmp, "closed_lib.db"))
		if err != nil {
			t.Fatal(err)
		}
		closedLibRepo := sqlite.NewLibraryRepo(tmpStore2)
		_ = tmpStore2.Close()

		h.SetLibraryRepo(closedLibRepo)
		defer h.SetLibraryRepo(libRepo)

		for _, ep := range []string{"/Sessions/Playing", "/Sessions/Playing/Progress", "/Sessions/Playing/Stopped"} {
			reqErr := httptest.NewRequest(http.MethodPost, ep, strings.NewReader(`{"ItemId":"`+movie.ID+`"}`))
			reqErr.Header.Set("X-Emby-Token", viewerAuth.AccessToken)
			recErr := httptest.NewRecorder()
			r.ServeHTTP(recErr, reqErr)
			if recErr.Code != http.StatusInternalServerError {
				t.Fatalf("expected 500 for %s with closed libRepo, got %d (%s)", ep, recErr.Code, recErr.Body.String())
			}
		}
	})

	// 11. UpdateDisplayPreferences body read error -> 400
	t.Run("update_display_preferences_body_read_error", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/DisplayPreferences/usersettings", failingBodyReader{})
		req.Header.Set("X-Emby-Token", viewerAuth.AccessToken)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for body read error, got %d (%s)", rec.Code, rec.Body.String())
		}
	})
}
