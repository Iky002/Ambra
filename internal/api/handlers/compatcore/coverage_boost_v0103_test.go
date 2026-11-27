package compatcore

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/ambra-media/ambra/internal/api/dto"
	"github.com/ambra-media/ambra/internal/api/middleware"
	"github.com/ambra-media/ambra/internal/core/auth"
	"github.com/ambra-media/ambra/internal/core/session"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

type ccCtxKey string

const testCCUIDKey ccCtxKey = "cc_uid"

func TestCompatCoreCoverageBoostV0103(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()

	adminAC := &middleware.AuthContext{User: env.admin, Token: "admin-tok"}
	user2AC := &middleware.AuthContext{User: env.user2, Token: "user2-tok", Client: auth.ClientInfo{DeviceID: "dev-client-only"}}

	callAs := func(ac *middleware.AuthContext, method, path, body, remoteAddr string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		if remoteAddr != "" {
			req.RemoteAddr = remoteAddr
		}
		if ac != nil {
			req = req.WithContext(middleware.WithAuthContext(req.Context(), ac))
		}
		rec := httptest.NewRecorder()
		env.router.ServeHTTP(rec, req)
		return rec
	}

	// 1. compareTimePtr: all 6 branches
	t.Run("compareTimePtr_all_branches", func(t *testing.T) {
		t1 := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
		t2 := t1.Add(time.Hour)
		if compareTimePtr(nil, nil) != 0 {
			t.Fatal("nil vs nil should be 0")
		}
		if compareTimePtr(nil, &t1) != -1 {
			t.Fatal("nil vs non-nil should be -1")
		}
		if compareTimePtr(&t1, nil) != 1 {
			t.Fatal("non-nil vs nil should be 1")
		}
		if compareTimePtr(&t1, &t2) != -1 {
			t.Fatal("earlier vs later should be -1")
		}
		if compareTimePtr(&t2, &t1) != 1 {
			t.Fatal("later vs earlier should be 1")
		}
		if compareTimePtr(&t1, &t1) != 0 {
			t.Fatal("equal times should be 0")
		}
	})

	// 2. allowedLibrariesForRequest & allowedLibrariesForUser & userPolicyForRequest branches
	t.Run("allowed_libraries_and_policy_branches", func(t *testing.T) {
		// Restrict user2 to a specific folder and max parental rating PG (5)
		recItem, err := env.libRepo.GetItemRecordByID(ctx, env.itemID)
		if err != nil {
			t.Fatal(err)
		}
		p := env.user2.Policy
		p.EnableAllFolders = false
		p.EnabledFolders = []string{recItem.LibraryID}
		maxPG := 5
		p.MaxParentalRating = &maxPG
		if err := env.userRepo.UpdateUserPolicy(ctx, env.user2.ID, p); err != nil {
			t.Fatal(err)
		}

		reqBase := httptest.NewRequest(http.MethodGet, "/", nil)
		// userRepo == nil
		hNil := &Handler{}
		if got := hNil.allowedLibrariesForRequest(reqBase, env.user2.ID); got != nil {
			t.Fatalf("expected nil when userRepo is nil, got %v", got)
		}
		if got := hNil.allowedLibrariesForUser(ctx, env.user2.ID); got != nil {
			t.Fatalf("expected nil from allowedLibrariesForUser when userRepo is nil, got %v", got)
		}
		if libs, maxR, tags := hNil.userPolicyForRequest(reqBase, env.user2.ID); libs != nil || maxR != nil || tags != nil {
			t.Fatalf("expected all nil when userRepo is nil")
		}

		// With AuthContext: admin -> nil, non-admin -> [recItem.LibraryID]
		reqAdm := reqBase.WithContext(middleware.WithAuthContext(ctx, adminAC))
		if got := env.handler.allowedLibrariesForRequest(reqAdm, ""); got != nil {
			t.Fatalf("expected nil for admin, got %v", got)
		}
		reqU2 := reqBase.WithContext(middleware.WithAuthContext(ctx, user2AC))
		if got := env.handler.allowedLibrariesForRequest(reqU2, ""); len(got) != 1 {
			t.Fatalf("expected 1 allowed lib for user2, got %v", got)
		}

		// Without AuthContext: UserIDFromCtx and fallbackUserID
		hWithUIDFn := &Handler{
			userRepo: env.userRepo,
			auth: AuthFuncs{
				UserIDFromCtx: func(c context.Context) string {
					if v, ok := c.Value(testCCUIDKey).(string); ok {
						return v
					}
					return ""
				},
			},
		}
		reqUID := reqBase.WithContext(context.WithValue(ctx, testCCUIDKey, env.user2.ID))
		if got := hWithUIDFn.allowedLibrariesForRequest(reqUID, ""); len(got) != 1 {
			t.Fatalf("expected 1 allowed lib via UserIDFromCtx, got %v", got)
		}
		if got := hWithUIDFn.allowedLibrariesForRequest(reqBase, env.user2.ID); len(got) != 1 {
			t.Fatalf("expected 1 allowed lib via fallbackUserID, got %v", got)
		}

		// allowedLibrariesForUser empty / unknown user
		if got := env.handler.allowedLibrariesForUser(ctx, ""); len(got) != 0 {
			t.Fatalf("expected empty slice for empty userID, got %v", got)
		}
		if got := env.handler.allowedLibrariesForUser(ctx, guid.New()); len(got) != 0 {
			t.Fatalf("expected empty slice for unknown userID, got %v", got)
		}

		// userPolicyForRequest empty / unknown uid
		if libs, _, _ := env.handler.userPolicyForRequest(reqBase, ""); libs == nil || len(libs) != 0 {
			t.Fatalf("expected empty non-nil slice for empty uid, got %#v", libs)
		}
		if libs, _, _ := env.handler.userPolicyForRequest(reqBase, guid.New()); libs == nil || len(libs) != 0 {
			t.Fatalf("expected empty non-nil slice for unknown uid, got %#v", libs)
		}
	})

	// 3. canAccessDevice, authorizeSessionTarget, getSessionPlayQueue, connectExchange, postSessionAddUser
	t.Run("sessions_and_devices_branches", func(t *testing.T) {
		hNoAuth := New(env.cfg, env.store, env.userRepo, env.libRepo, env.sessions, env.registry)
		reqNoAuth := httptest.NewRequest(http.MethodGet, "/", nil)
		// canAccessDevice: empty callerID / empty deviceID -> false
		if hNoAuth.canAccessDevice(reqNoAuth, "some-dev") {
			t.Fatal("expected false for empty callerID")
		}
		reqU2 := reqNoAuth.WithContext(middleware.WithAuthContext(ctx, user2AC))
		if env.handler.canAccessDevice(reqU2, "") {
			t.Fatal("expected false for empty deviceID")
		}
		// Session-only device (not in userRepo.ListDevices)
		env.sessions.TouchSession(env.user2, auth.ClientInfo{DeviceID: "sess-only-u2", Client: "TV"})
		env.sessions.TouchSession(env.admin, auth.ClientInfo{DeviceID: "sess-only-adm", Client: "TV"})
		if !env.handler.canAccessDevice(reqU2, "sess-only-u2") {
			t.Fatal("expected user2 to access their session-only device")
		}
		if env.handler.canAccessDevice(reqU2, "sess-only-adm") {
			t.Fatal("expected user2 to be denied admin's session-only device")
		}
		// Client DeviceID fallback when device is in neither DB nor sessions
		if !env.handler.canAccessDevice(reqU2, "dev-client-only") {
			t.Fatal("expected client DeviceID fallback to allow")
		}
		if env.handler.canAccessDevice(reqU2, "completely-unknown-dev") {
			t.Fatal("expected false for unknown device")
		}

		// authorizeSessionTarget: empty sid -> allowed; UserIDFromCtx mismatch -> denied; nil auth -> denied
		if _, _, ok := env.handler.authorizeSessionTarget(reqNoAuth, ""); !ok {
			t.Fatal("expected empty sid to be allowed")
		}
		admSessions := env.sessions.ListSessions()
		var admSID string
		for _, s := range admSessions {
			if guid.Normalize(s.UserId) == guid.Normalize(env.admin.ID) {
				admSID = s.Id
				break
			}
		}
		hUIDOnly := &Handler{
			sessions: env.sessions,
			userRepo: env.userRepo,
			auth: AuthFuncs{
				UserIDFromCtx: func(c context.Context) string {
					if v, ok := c.Value(testCCUIDKey).(string); ok {
						return v
					}
					return ""
				},
			},
		}
		reqU2UID := reqNoAuth.WithContext(context.WithValue(ctx, testCCUIDKey, env.user2.ID))
		if _, _, ok := hUIDOnly.authorizeSessionTarget(reqU2UID, admSID); ok {
			t.Fatal("expected non-admin UserIDFromCtx to be denied admin session")
		}
		hNoAuthFuncs := &Handler{sessions: env.sessions}
		if _, _, ok := hNoAuthFuncs.authorizeSessionTarget(reqNoAuth, admSID); ok {
			t.Fatal("expected nil auth handler to deny targeted user session")
		}

		// getSessionPlayQueue: StartIndex<0, StartIndex>total, Limit>0
		_ = env.sessions.ReportPlaybackStart(ctx, env.admin, auth.ClientInfo{DeviceID: "sess-only-adm"}, session.PlaybackStartRequest{
			ItemId: env.itemID,
			Item:   &dto.BaseItemDto{Id: env.itemID, Name: "Playing1"},
		})
		_ = env.sessions.ReportPlaybackStart(ctx, env.user2, auth.ClientInfo{DeviceID: "sess-only-u2"}, session.PlaybackStartRequest{
			ItemId: env.itemID,
			Item:   &dto.BaseItemDto{Id: env.itemID, Name: "Playing2"},
		})
		recQ1 := callAs(adminAC, http.MethodGet, "/Sessions/PlayQueue?StartIndex=-5&Limit=1", "", "")
		if recQ1.Code != http.StatusOK {
			t.Fatalf("PlayQueue got %d", recQ1.Code)
		}
		recQ2 := callAs(adminAC, http.MethodGet, "/Sessions/PlayQueue?StartIndex=99", "", "")
		if recQ2.Code != http.StatusOK {
			t.Fatalf("PlayQueue StartIndex=99 got %d", recQ2.Code)
		}

		// connectExchange: missing ConnectUserId -> 401; linked user with empty ac.Token -> 401
		recCE1 := callAs(adminAC, http.MethodGet, "/Connect/Exchange", "", "")
		if recCE1.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401 for missing ConnectUserId, got %d", recCE1.Code)
		}
		_ = env.handler.saveKV(ctx, "user_connect_link:"+env.admin.ID, map[string]any{
			"UserId":          env.admin.ID,
			"ConnectUsername": "linked-admin",
		})
		adminNoTokAC := &middleware.AuthContext{User: env.admin, Token: ""}
		recCE2 := callAs(adminNoTokAC, http.MethodGet, "/Connect/Exchange?ConnectUserId=linked-admin", "", "")
		if recCE2.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401 for empty access token in connectExchange, got %d", recCE2.Code)
		}

		// postSessionAddUser: nonexistent session -> 404; existing session attaches AdditionalUsers
		recMissing := callAs(adminAC, http.MethodPost, "/Sessions/new-evict-sid/Users/"+env.user2.ID, "", "")
		if recMissing.Code != http.StatusNotFound {
			t.Fatalf("expected 404 on postSessionAddUser for nonexistent session, got %d", recMissing.Code)
		}
		evictSess := env.sessions.TouchSession(env.admin, auth.ClientInfo{DeviceID: "evict-dev", Client: "Web"})
		recAdd := callAs(adminAC, http.MethodPost, "/Sessions/"+evictSess.Id+"/Users/"+env.user2.ID, "", "")
		if recAdd.Code != http.StatusNoContent {
			t.Fatalf("expected 204 on postSessionAddUser, got %d", recAdd.Code)
		}
		if gotSess, ok := env.sessions.GetSession(evictSess.Id); !ok || len(gotSess.AdditionalUsers) != 1 {
			t.Fatalf("expected 1 AdditionalUser in session, got %+v", gotSess.AdditionalUsers)
		}
	})

	// 4. queryUsers & getUserPrefixes: filters, sort modes (LastActivityDate, LastLoginDate, IsAdministrator), pagination, unauth 401
	t.Run("queryUsers_and_prefixes_full_matrix", func(t *testing.T) {
		now := time.Now().UTC()
		_ = env.userRepo.UpdateLastLogin(ctx, env.admin.ID, now)
		_ = env.userRepo.UpdateLastLogin(ctx, env.user2.ID, now.Add(-time.Hour))

		for _, qs := range []string{
			"SearchTerm=admin&IsHidden=false&IsDisabled=false&Limit=1",
			"SortBy=LastActivityDate,Name&SortOrder=Ascending",
			"SortBy=LastLoginDate&SortOrder=Descending",
			"SortBy=IsAdministrator&SortOrder=Ascending",
			"SortBy=IsAdministrator&SortOrder=Descending",
		} {
			rec := callAs(adminAC, http.MethodGet, "/Users/Query?"+qs, "", "")
			if rec.Code != http.StatusOK {
				t.Fatalf("Users/Query?%s got %d: %s", qs, rec.Code, rec.Body.String())
			}
		}

		// Direct call without auth context on handler with nil UserIDFromCtx -> 401 on queryUsers and getUserPrefixes
		hNoAuth := New(env.cfg, env.store, env.userRepo, env.libRepo, env.sessions, env.registry)
		reqUnauth := httptest.NewRequest(http.MethodGet, "/Users/Query", nil)
		recUnauth := httptest.NewRecorder()
		hNoAuth.queryUsers(recUnauth, reqUnauth)
		if recUnauth.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401 for unauth queryUsers, got %d", recUnauth.Code)
		}
		recPrefUnauth := httptest.NewRecorder()
		hNoAuth.getUserPrefixes(recPrefUnauth, reqUnauth)
		if recPrefUnauth.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401 for unauth getUserPrefixes, got %d", recPrefUnauth.Code)
		}
	})

	// 5. forgotPassword eviction, forgotPasswordPin expired pruning, checkIPRateLimit, authenticateUserByID rate limit & missing Id
	t.Run("forgot_password_eviction_and_rate_limits", func(t *testing.T) {
		// checkIPRateLimit empty IP + expiry pruning
		bucket := map[string][]time.Time{}
		now := time.Now().UTC()
		if !env.handler.checkIPRateLimit(bucket, "", now, time.Minute, 5) {
			t.Fatal("empty IP should pass")
		}
		_ = env.handler.checkIPRateLimit(bucket, "198.51.100.1", now, time.Minute, 5)
		_ = env.handler.checkIPRateLimit(bucket, "198.51.100.2", now.Add(2*time.Minute), time.Minute, 5)
		if _, ok := bucket["198.51.100.1"]; ok {
			t.Fatal("expected expired IP bucket to be deleted")
		}

		// Pre-fill forgotPins to max entries with an oldest pin file so forgotPassword evicts it
		oldFile := filepath.Join(env.cfg.DataDir, "old-pin-file.txt")
		_ = os.WriteFile(oldFile, []byte("old"), 0o600)
		env.handler.mu.Lock()
		env.handler.forgotRates = nil
		env.handler.forgotPins = map[string]forgotPinEntry{}
		for i := 0; i < forgotPinsMaxEntries; i++ {
			k := guid.New()[:8]
			exp := time.Now().UTC().Add(time.Duration(i+1) * time.Minute)
			pf := ""
			if i == 0 {
				pf = oldFile
			}
			env.handler.forgotPins[k] = forgotPinEntry{
				UserID:    env.admin.ID,
				UserName:  env.admin.Name,
				Pin:       k,
				PinFile:   pf,
				ExpiresAt: exp,
			}
		}
		env.handler.mu.Unlock()

		recFP := callAs(nil, http.MethodPost, "/Users/ForgotPassword", `{"EnteredUsername":"admin"}`, "127.0.0.1:1111")
		if recFP.Code != http.StatusOK {
			t.Fatalf("ForgotPassword got %d: %s", recFP.Code, recFP.Body.String())
		}

		// forgotPasswordPin with nil forgotPinRates and an expired pin entry having a file
		expFile := filepath.Join(env.cfg.DataDir, "expired-pin.txt")
		_ = os.WriteFile(expFile, []byte("exp"), 0o600)
		env.handler.mu.Lock()
		env.handler.forgotPinRates = nil
		env.handler.forgotPins["999991"] = forgotPinEntry{
			UserID:    env.admin.ID,
			UserName:  env.admin.Name,
			Pin:       "999991",
			PinFile:   expFile,
			ExpiresAt: time.Now().UTC().Add(-time.Minute),
		}
		env.handler.mu.Unlock()
		recPin := callAs(nil, http.MethodPost, "/Users/ForgotPassword/Pin", `{"Pin":"999991"}`, "127.0.0.1:1111")
		if recPin.Code != http.StatusOK {
			t.Fatalf("ForgotPassword/Pin got %d: %s", recPin.Code, recPin.Body.String())
		}

		// authenticateUserByID: empty Id -> 401; rate limit exceeded -> 429
		reqEmptyID := httptest.NewRequest(http.MethodPost, "/Users//Authenticate", strings.NewReader(`{}`))
		reqEmptyID.RemoteAddr = "198.51.100.79:1111"
		env.handler.mu.Lock()
		env.handler.authByIDRates = nil
		env.handler.mu.Unlock()
		recEmpty := httptest.NewRecorder()
		env.handler.authenticateUserByID(recEmpty, reqEmptyID)
		if recEmpty.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401 for empty Id, got %d", recEmpty.Code)
		}

		env.handler.mu.Lock()
		env.handler.authByIDRates["198.51.100.80"] = make([]time.Time, authByIDRateLimit)
		for i := range env.handler.authByIDRates["198.51.100.80"] {
			env.handler.authByIDRates["198.51.100.80"][i] = time.Now().UTC()
		}
		env.handler.mu.Unlock()
		recRL := callAs(nil, http.MethodPost, "/Users/"+env.admin.ID+"/Authenticate", `{"Pw":"wrong"}`, "198.51.100.80:1111")
		if recRL.Code != http.StatusTooManyRequests {
			t.Fatalf("expected 429 when authByID rate limit exceeded, got %d", recRL.Code)
		}
	})

	// 6. serveUserItemExtras, canAccessItem, preferredGenresForUser, getUserSuggestions, getUserSectionItems
	t.Run("user_extras_suggestions_and_sections", func(t *testing.T) {
		recItem, _ := env.libRepo.GetItemRecordByID(ctx, env.itemID)
		// Add an R-rated item in allowed library so parental check blocks it for user2
		rItem := &sqlite.ItemRecord{
			ID: guid.New(), LibraryID: recItem.LibraryID, ParentID: recItem.LibraryID,
			Type: "Movie", MediaType: "Video", Name: "Adult Film", OfficialRating: "R",
		}
		_ = env.libRepo.UpsertItem(ctx, rItem)

		// canAccessItem direct checks: empty itemID -> true; missing item (admin true, user2 false); parental blocked -> false
		reqU2 := httptest.NewRequest(http.MethodGet, "/", nil).WithContext(middleware.WithAuthContext(ctx, user2AC))
		reqAdm := httptest.NewRequest(http.MethodGet, "/", nil).WithContext(middleware.WithAuthContext(ctx, adminAC))
		if !env.handler.canAccessItem(reqU2, env.user2.ID, "") {
			t.Fatal("expected true for empty itemID")
		}
		if !env.handler.canAccessItem(reqAdm, env.admin.ID, guid.New()) {
			t.Fatal("expected admin to pass on missing item")
		}
		if env.handler.canAccessItem(reqU2, env.user2.ID, guid.New()) {
			t.Fatal("expected non-admin to fail on missing item")
		}
		if env.handler.canAccessItem(reqU2, env.user2.ID, rItem.ID) {
			t.Fatal("expected parental-blocked item to be denied")
		}

		// serveUserItemExtras: parental blocked -> 403; nil libRepo -> 200 []
		recEx403 := callAs(user2AC, http.MethodGet, "/Users/"+env.user2.ID+"/Items/"+rItem.ID+"/LocalTrailers", "", "")
		if recEx403.Code != http.StatusForbidden {
			t.Fatalf("expected 403 for parental-blocked extras, got %d", recEx403.Code)
		}
		hNoLib := &Handler{userRepo: env.userRepo}
		reqExNil := httptest.NewRequest(http.MethodGet, "/Users/"+env.admin.ID+"/Items/"+env.itemID+"/LocalTrailers", nil)
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("UserId", env.admin.ID)
		rctx.URLParams.Add("Id", env.itemID)
		reqExNil = reqExNil.WithContext(context.WithValue(middleware.WithAuthContext(ctx, adminAC), chi.RouteCtxKey, rctx))
		recExNil := httptest.NewRecorder()
		hNoLib.getUserItemLocalTrailers(recExNil, reqExNil)
		if recExNil.Code != http.StatusOK {
			t.Fatalf("expected 200 [] when libRepo is nil, got %d", recExNil.Code)
		}

		// preferredGenresForUser: empty uid -> nil; >5 genres sorted & capped at 5
		if got := env.handler.preferredGenresForUser(ctx, "", nil, dto.UserPolicy{}); got != nil {
			t.Fatalf("expected nil for empty uid, got %v", got)
		}
		_ = env.libRepo.ReplaceItemTaxonomies(ctx, env.itemID, "Genre", []string{"Action", "SciFi", "Drama", "Comedy", "Thriller", "Horror", "Fantasy"})
		_, _ = env.libRepo.MarkFavorite(ctx, env.admin.ID, env.itemID)
		genres := env.handler.preferredGenresForUser(ctx, env.admin.ID, nil, dto.UserPolicy{})
		if len(genres) != 5 {
			t.Fatalf("expected preferred genres capped at 5, got %v", genres)
		}

		// getUserSuggestions: negative StartIndex & Limit<=0, and StartIndex > total with preferred genres
		recSug1 := callAs(adminAC, http.MethodGet, "/Users/"+env.admin.ID+"/Suggestions?StartIndex=-1&Limit=0", "", "")
		if recSug1.Code != http.StatusOK {
			t.Fatalf("Suggestions -1/0 got %d", recSug1.Code)
		}
		recSug2 := callAs(adminAC, http.MethodGet, "/Users/"+env.admin.ID+"/Suggestions?StartIndex=99&Limit=5", "", "")
		if recSug2.Code != http.StatusOK {
			t.Fatalf("Suggestions StartIndex=99 got %d", recSug2.Code)
		}
		// User with NO preferred genres and ALL items played -> triggers total==0 fallback query
		_, _ = env.libRepo.MarkPlayed(ctx, env.user2.ID, env.itemID)
		// Clear genres on env.itemID so user2 has no preferred genres
		_ = env.libRepo.ReplaceItemTaxonomies(ctx, env.itemID, "Genre", nil)
		recSugFB := callAs(user2AC, http.MethodGet, "/Users/"+env.user2.ID+"/Suggestions?Type=Movie", "", "")
		if recSugFB.Code != http.StatusOK {
			t.Fatalf("Suggestions fallback got %d: %s", recSugFB.Code, recSugFB.Body.String())
		}

		// getUserSectionItems bounds clamping & nextup StartIndex >= len
		for _, path := range []string{
			"/Users/" + env.admin.ID + "/Sections/latest/Items?StartIndex=-5&Limit=0",
			"/Users/" + env.admin.ID + "/Sections/latest/Items?StartIndex=20000&Limit=999",
			"/Users/" + env.admin.ID + "/Sections/nextup/Items?StartIndex=50&Limit=10",
		} {
			recSec := callAs(adminAC, http.MethodGet, path, "", "")
			if recSec.Code != http.StatusOK {
				t.Fatalf("%s got %d: %s", path, recSec.Code, recSec.Body.String())
			}
		}
	})

	// 7. updateUserConfigurationPartial invalid field type (400), updateUser/deleteUser nil userRepo (404), copyUserData CopyFromUserId, loadKV error branches
	t.Run("user_mutations_and_kv_edge_cases", func(t *testing.T) {
		recBadPatch := callAs(adminAC, http.MethodPost, "/Users/"+env.admin.ID+"/Configuration/Partial", `{"PlayDefaultAudioTrack":"not-a-bool"}`, "")
		if recBadPatch.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for invalid partial config type, got %d (%s)", recBadPatch.Code, recBadPatch.Body.String())
		}

		// Direct calls with nil userRepo -> 404
		hNilRepo := &Handler{}
		for _, tc := range []struct {
			name string
			fn   http.HandlerFunc
		}{
			{"updateUser", hNilRepo.updateUser},
			{"deleteUser", hNilRepo.deleteUser},
			{"updateUserConfigurationPartial", hNilRepo.updateUserConfigurationPartial},
		} {
			req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{}`))
			req = req.WithContext(middleware.WithAuthContext(ctx, adminAC))
			rec := httptest.NewRecorder()
			tc.fn(rec, req)
			if rec.Code != http.StatusNotFound {
				t.Fatalf("%s with nil userRepo: want 404, got %d", tc.name, rec.Code)
			}
		}

		// copyUserData with explicit CopyFromUserId
		recCopy := callAs(adminAC, http.MethodPost, "/Users/"+env.admin.ID+"/CopyData",
			`{"CopyFromUserId":"`+env.admin.ID+`","ToUserIds":["`+env.user2.ID+`"],"CopyOptions":["Configuration"]}`, "")
		if recCopy.Code != http.StatusNoContent {
			t.Fatalf("copyUserData got %d", recCopy.Code)
		}

		// loadKV: nil store, empty key, corrupt JSON
		var dst map[string]any
		if found, _ := hNilRepo.loadKV(ctx, "k", &dst); found {
			t.Fatal("expected false for nil store")
		}
		if found, _ := env.handler.loadKV(ctx, "", &dst); found {
			t.Fatal("expected false for empty key")
		}
		_, _ = env.store.Writer.ExecContext(ctx, `INSERT OR REPLACE INTO server_config(key, value_json, updated_at) VALUES('corrupt_kv_key', '{bad', 1)`)
		if found, err := env.handler.loadKV(ctx, "corrupt_kv_key", &dst); found || err == nil {
			t.Fatalf("expected (false, err) for corrupt JSON in loadKV, got (%v, %v)", found, err)
		}

		// postPartiesMessages with invalid JSON (400)
		recPM := callAs(adminAC, http.MethodPost, "/Parties/Messages", `{invalid`, "")
		if recPM.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for invalid party message JSON, got %d", recPM.Code)
		}

		// Verify JSON response helpers on closed libRepo for serveUserItemExtras / getUserSuggestions / getUserSectionItems
		closedStore, err := sqlite.Open(filepath.Join(t.TempDir(), "closed.db"))
		if err != nil {
			t.Fatal(err)
		}
		closedLib := sqlite.NewLibraryRepo(closedStore)
		_ = closedStore.Close()
		hClosedLib := &Handler{userRepo: env.userRepo, libRepo: closedLib, cfg: env.cfg}

		// serveUserItemExtras DB error -> 500
		reqExErr := httptest.NewRequest(http.MethodGet, "/", nil)
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("UserId", env.admin.ID)
		rctx.URLParams.Add("Id", env.itemID)
		reqExErr = reqExErr.WithContext(context.WithValue(middleware.WithAuthContext(ctx, adminAC), chi.RouteCtxKey, rctx))
		recExErr := httptest.NewRecorder()
		hClosedLib.getUserItemLocalTrailers(recExErr, reqExErr)
		if recExErr.Code != http.StatusInternalServerError {
			t.Fatalf("expected 500 for serveUserItemExtras on closed libRepo, got %d", recExErr.Code)
		}

		// getUserSuggestions DB error -> 500
		recSugErr := httptest.NewRecorder()
		hClosedLib.getUserSuggestions(recSugErr, reqExErr)
		if recSugErr.Code != http.StatusInternalServerError {
			t.Fatalf("expected 500 for getUserSuggestions on closed libRepo, got %d", recSugErr.Code)
		}

		// getUserSectionItems DB error -> 500
		rctx2 := chi.NewRouteContext()
		rctx2.URLParams.Add("UserId", env.admin.ID)
		rctx2.URLParams.Add("SectionId", "latest")
		reqSecErr := httptest.NewRequest(http.MethodGet, "/", nil).WithContext(context.WithValue(middleware.WithAuthContext(ctx, adminAC), chi.RouteCtxKey, rctx2))
		recSecErr := httptest.NewRecorder()
		hClosedLib.getUserSectionItems(recSecErr, reqSecErr)
		if recSecErr.Code != http.StatusInternalServerError {
			t.Fatalf("expected 500 for getUserSectionItems on closed libRepo, got %d", recSecErr.Code)
		}

		// canAccessItem closed libRepo -> false
		if hClosedLib.canAccessItem(reqExErr, env.admin.ID, env.itemID) {
			t.Fatal("expected false for canAccessItem on closed libRepo")
		}
		_ = json.Valid([]byte("{}"))
	})
}
