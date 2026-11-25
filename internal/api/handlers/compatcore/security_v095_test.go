package compatcore

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ambra-media/ambra/internal/api/dto"
	"github.com/ambra-media/ambra/internal/api/middleware"
	"github.com/ambra-media/ambra/internal/api/ws"
	"github.com/ambra-media/ambra/internal/core/auth"
	"github.com/ambra-media/ambra/internal/core/session"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

type trackingWSHub struct {
	disconnected []string
}

func (t *trackingWSHub) SendToDevice(string, ws.Message) {}
func (t *trackingWSHub) DisconnectDevice(deviceID string) {
	t.disconnected = append(t.disconnected, deviceID)
}

func TestCompatCoreAuthCacheInvalidationAndSelfDelete(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()

	authSvc := auth.NewService(env.userRepo, env.cfg)
	authMW := middleware.NewAuth(authSvc, env.sessions)
	env.handler.SetAuthService(authSvc)
	wsHub := &trackingWSHub{}
	env.handler.SetWSHub(wsHub)

	adminAuth, err := authSvc.AuthenticateByName(ctx, "admin", "admin", "", auth.ClientInfo{DeviceID: "admin-dev"})
	if err != nil {
		t.Fatal(err)
	}
	guestAuth, err := authSvc.AuthenticateByName(ctx, "9guest", "pass", "", auth.ClientInfo{DeviceID: "guest-dev"})
	if err != nil {
		t.Fatal(err)
	}
	// Prime caches in authSvc
	if _, err := authSvc.ValidateToken(ctx, guestAuth.AccessToken); err != nil {
		t.Fatal(err)
	}
	apiKey, err := authSvc.CreateAPIKey(ctx, "TestApp")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := authSvc.ValidateToken(ctx, apiKey.Token); err != nil {
		t.Fatal(err)
	}

	router := env.handler.BuildRouter(AuthFuncs{
		RequireAuth:  authMW.RequireAuth,
		RequireAdmin: authMW.RequireAdmin,
		UserIDFromCtx: func(c context.Context) string {
			if ac, ok := middleware.AuthFromContext(c); ok && ac != nil {
				return ac.UserID()
			}
			return ""
		},
	})

	doAuthReq := func(token, method, target, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, target, strings.NewReader(body))
		if token != "" {
			req.Header.Set("X-Emby-Token", token)
		}
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}

	// 1. POST /Devices/Delete revokes cached device token and disconnects WS device
	if rec := doAuthReq(adminAuth.AccessToken, http.MethodPost, "/Devices/Delete?Id=guest-dev", ""); rec.Code != http.StatusNoContent {
		t.Fatalf("POST /Devices/Delete expected 204, got %d", rec.Code)
	}
	if _, err := authSvc.ValidateToken(ctx, guestAuth.AccessToken); err == nil {
		t.Fatal("expected guest token to be invalidated immediately after POST /Devices/Delete")
	}
	if len(wsHub.disconnected) != 1 || wsHub.disconnected[0] != "guest-dev" {
		t.Fatalf("expected DisconnectDevice(guest-dev), got %+v", wsHub.disconnected)
	}

	// 2. POST /Auth/Keys/{Key}/Delete revokes cached API key
	if rec := doAuthReq(adminAuth.AccessToken, http.MethodPost, "/Auth/Keys/"+apiKey.Token+"/Delete", ""); rec.Code != http.StatusNoContent {
		t.Fatalf("POST /Auth/Keys/{Key}/Delete expected 204, got %d", rec.Code)
	}
	if _, err := authSvc.ValidateToken(ctx, apiKey.Token); err == nil {
		t.Fatal("expected API key to be invalidated immediately after POST /Auth/Keys/{Key}/Delete")
	}

	// 3. Re-login guest, update policy via POST /Users/{Id}, verify user cache invalidated
	guestAuth2, err := authSvc.AuthenticateByName(ctx, "9guest", "pass", "", auth.ClientInfo{DeviceID: "guest-dev-2"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := authSvc.ValidateToken(ctx, guestAuth2.AccessToken); err != nil {
		t.Fatal(err)
	}
	if rec := doAuthReq(adminAuth.AccessToken, http.MethodPost, "/Users/"+env.user2.ID, `{"Policy":{"IsDisabled":true}}`); rec.Code != http.StatusNoContent {
		t.Fatalf("POST /Users/{Id} disable expected 204, got %d", rec.Code)
	}
	if _, err := authSvc.ValidateToken(ctx, guestAuth2.AccessToken); err == nil {
		t.Fatal("expected disabled user token to fail ValidateToken after cache invalidation")
	}

	// Re-enable and test Configuration/Partial invalidates cache
	if rec := doAuthReq(adminAuth.AccessToken, http.MethodPost, "/Users/"+env.user2.ID, `{"Policy":{"IsDisabled":false}}`); rec.Code != http.StatusNoContent {
		t.Fatalf("re-enable expected 204, got %d", rec.Code)
	}
	if rec := doAuthReq(adminAuth.AccessToken, http.MethodPost, "/Users/"+env.user2.ID+"/Configuration/Partial", `{"SubtitleMode":"Always"}`); rec.Code != http.StatusNoContent {
		t.Fatalf("Configuration/Partial expected 204, got %d", rec.Code)
	}

	// 4. POST /Users/{admin.ID}/Delete prevents deleting currently authenticated admin -> 400
	if rec := doAuthReq(adminAuth.AccessToken, http.MethodPost, "/Users/"+env.admin.ID+"/Delete", ""); rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 when admin deletes self, got %d (%s)", rec.Code, rec.Body.String())
	}

	// 5. POST /Users/{user2.ID}/Delete succeeds and invalidates cache
	if rec := doAuthReq(adminAuth.AccessToken, http.MethodPost, "/Users/"+env.user2.ID+"/Delete", ""); rec.Code != http.StatusNoContent {
		t.Fatalf("POST /Users/{user2}/Delete expected 204, got %d", rec.Code)
	}
	if _, err := authSvc.ValidateToken(ctx, guestAuth2.AccessToken); err == nil {
		t.Fatal("expected deleted user token to fail ValidateToken")
	}
}

func TestCompatCoreRBACPlayQueueSessionUsersDevicesAndSettings(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()

	authSvc := auth.NewService(env.userRepo, env.cfg)
	authMW := middleware.NewAuth(authSvc, env.sessions)
	env.handler.SetAuthService(authSvc)

	adminAuth, err := authSvc.AuthenticateByName(ctx, "admin", "admin", "", auth.ClientInfo{DeviceID: "admin-dev", Device: "AdminPC", Client: "Web"})
	if err != nil {
		t.Fatal(err)
	}
	guestAuth, err := authSvc.AuthenticateByName(ctx, "9guest", "pass", "", auth.ClientInfo{DeviceID: "guest-dev", Device: "GuestPhone", Client: "Web"})
	if err != nil {
		t.Fatal(err)
	}

	// Create active sessions with NowPlayingItem for both users
	adminSess := env.sessions.TouchSession(env.admin, auth.ClientInfo{DeviceID: "admin-dev", Device: "AdminPC", Client: "Web"})
	guestSess := env.sessions.TouchSession(env.user2, auth.ClientInfo{DeviceID: "guest-dev", Device: "GuestPhone", Client: "Web"})
	_ = env.sessions.ReportPlaybackStart(ctx, env.admin, auth.ClientInfo{DeviceID: "admin-dev"}, session.PlaybackStartRequest{ItemId: env.itemID})
	_ = env.sessions.ReportPlaybackStart(ctx, env.user2, auth.ClientInfo{DeviceID: "guest-dev"}, session.PlaybackStartRequest{ItemId: env.itemID})

	router := env.handler.BuildRouter(AuthFuncs{
		RequireAuth:  authMW.RequireAuth,
		RequireAdmin: authMW.RequireAdmin,
		UserIDFromCtx: func(c context.Context) string {
			if ac, ok := middleware.AuthFromContext(c); ok && ac != nil {
				return ac.UserID()
			}
			return ""
		},
	})

	doAuthReq := func(token, method, target, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, target, strings.NewReader(body))
		if token != "" {
			req.Header.Set("X-Emby-Token", token)
		}
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}

	// 1. GET /Sessions/PlayQueue: guest sees only 1 item (own), admin sees 2
	rec := doAuthReq(guestAuth.AccessToken, http.MethodGet, "/Sessions/PlayQueue", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("guest PlayQueue expected 200, got %d", rec.Code)
	}
	var guestPQ dto.QueryResult[dto.BaseItemDto]
	_ = json.Unmarshal(rec.Body.Bytes(), &guestPQ)
	if guestPQ.TotalRecordCount != 1 {
		t.Fatalf("guest PlayQueue expected 1 item, got %d", guestPQ.TotalRecordCount)
	}

	rec = doAuthReq(adminAuth.AccessToken, http.MethodGet, "/Sessions/PlayQueue", "")
	var adminPQ dto.QueryResult[dto.BaseItemDto]
	_ = json.Unmarshal(rec.Body.Bytes(), &adminPQ)
	if adminPQ.TotalRecordCount != 2 {
		t.Fatalf("admin PlayQueue expected 2 items, got %d", adminPQ.TotalRecordCount)
	}

	// 2. POST/DELETE /Sessions/{Id}/Users/{UserId}: guest cannot modify admin's session -> 403
	if rec := doAuthReq(guestAuth.AccessToken, http.MethodPost, "/Sessions/"+adminSess.Id+"/Users/"+env.user2.ID, ""); rec.Code != http.StatusForbidden {
		t.Fatalf("guest add user to admin session expected 403, got %d", rec.Code)
	}
	if rec := doAuthReq(guestAuth.AccessToken, http.MethodDelete, "/Sessions/"+adminSess.Id+"/Users/"+env.user2.ID, ""); rec.Code != http.StatusForbidden {
		t.Fatalf("guest delete user from admin session expected 403, got %d", rec.Code)
	}
	// Guest can modify own session -> 204
	if rec := doAuthReq(guestAuth.AccessToken, http.MethodPost, "/Sessions/"+guestSess.Id+"/Users/"+env.user2.ID, ""); rec.Code != http.StatusNoContent {
		t.Fatalf("guest add user to own session expected 204, got %d", rec.Code)
	}
	if rec := doAuthReq(guestAuth.AccessToken, http.MethodDelete, "/Sessions/"+guestSess.Id+"/Users/"+env.user2.ID, ""); rec.Code != http.StatusNoContent {
		t.Fatalf("guest delete user from own session expected 204, got %d", rec.Code)
	}

	// 3. GET /Devices/Info & POST /Devices/Options: guest cannot access admin's device -> 403
	if rec := doAuthReq(guestAuth.AccessToken, http.MethodGet, "/Devices/Info?Id=admin-dev", ""); rec.Code != http.StatusForbidden {
		t.Fatalf("guest GET admin Device Info expected 403, got %d", rec.Code)
	}
	if rec := doAuthReq(guestAuth.AccessToken, http.MethodPost, "/Devices/Options?Id=admin-dev", `{"CustomName":"Hacked"}`); rec.Code != http.StatusForbidden {
		t.Fatalf("guest POST admin Device Options expected 403, got %d", rec.Code)
	}
	// Guest can access own device -> 200/204
	if rec := doAuthReq(guestAuth.AccessToken, http.MethodGet, "/Devices/Info?Id=guest-dev", ""); rec.Code != http.StatusOK {
		t.Fatalf("guest GET own Device Info expected 200, got %d", rec.Code)
	}
	if rec := doAuthReq(guestAuth.AccessToken, http.MethodPost, "/Devices/Options?Id=guest-dev", `{"CustomName":"MyPhone"}`); rec.Code != http.StatusNoContent {
		t.Fatalf("guest POST own Device Options expected 204, got %d", rec.Code)
	}
	// Admin can access guest's device -> 200/204
	if rec := doAuthReq(adminAuth.AccessToken, http.MethodGet, "/Devices/Info?Id=guest-dev", ""); rec.Code != http.StatusOK {
		t.Fatalf("admin GET guest Device Info expected 200, got %d", rec.Code)
	}
	if rec := doAuthReq(adminAuth.AccessToken, http.MethodPost, "/Devices/Options?Id=guest-dev", `{"CustomName":"AdminRenamed"}`); rec.Code != http.StatusNoContent {
		t.Fatalf("admin POST guest Device Options expected 204, got %d", rec.Code)
	}

	// 4. GET TypedSettings, HomeSections, UserSettings: guest cannot read admin's -> 403; own -> 200; admin reading guest -> 200
	for _, path := range []string{
		"/Users/" + env.admin.ID + "/TypedSettings/theme",
		"/Users/" + env.admin.ID + "/HomeSections",
		"/UserSettings/" + env.admin.ID,
	} {
		if rec := doAuthReq(guestAuth.AccessToken, http.MethodGet, path, ""); rec.Code != http.StatusForbidden {
			t.Fatalf("guest GET %s expected 403, got %d", path, rec.Code)
		}
	}
	for _, path := range []string{
		"/Users/" + env.user2.ID + "/TypedSettings/theme",
		"/Users/" + env.user2.ID + "/HomeSections",
		"/UserSettings/" + env.user2.ID,
	} {
		if rec := doAuthReq(guestAuth.AccessToken, http.MethodGet, path, ""); rec.Code != http.StatusOK {
			t.Fatalf("guest GET own %s expected 200, got %d", path, rec.Code)
		}
		if rec := doAuthReq(adminAuth.AccessToken, http.MethodGet, path, ""); rec.Code != http.StatusOK {
			t.Fatalf("admin GET guest %s expected 200, got %d", path, rec.Code)
		}
	}
}

func TestCompatCoreSuggestionsAndSectionsPolicyAndPersonalization(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()

	authSvc := auth.NewService(env.userRepo, env.cfg)
	authMW := middleware.NewAuth(authSvc, env.sessions)

	libAllowed, _ := env.libRepo.CreateLibrary(ctx, "FamilyLib", "movies", []string{t.TempDir()})
	libBlocked, _ := env.libRepo.CreateLibrary(ctx, "PrivateLib", "movies", []string{t.TempDir()})

	favSciFi := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: libAllowed.ID, ParentID: libAllowed.ID,
		Type: "Movie", MediaType: "Video", Name: "FavSciFi", SortName: "favscifi", OfficialRating: "PG",
	}
	unplayedSciFi := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: libAllowed.ID, ParentID: libAllowed.ID,
		Type: "Movie", MediaType: "Video", Name: "UnplayedSciFi", SortName: "unplayedscifi", OfficialRating: "PG",
	}
	unplayedComedy := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: libAllowed.ID, ParentID: libAllowed.ID,
		Type: "Movie", MediaType: "Video", Name: "UnplayedComedy", SortName: "unplayedcomedy", OfficialRating: "PG",
	}
	rRatedSciFi := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: libAllowed.ID, ParentID: libAllowed.ID,
		Type: "Movie", MediaType: "Video", Name: "RRatedSciFi", SortName: "rratedscifi", OfficialRating: "R",
	}
	nsfwSciFi := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: libAllowed.ID, ParentID: libAllowed.ID,
		Type: "Movie", MediaType: "Video", Name: "NSFWSciFi", SortName: "nsfwscifi", OfficialRating: "PG",
	}
	blockedLibItem := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: libBlocked.ID, ParentID: libBlocked.ID,
		Type: "Movie", MediaType: "Video", Name: "SecretSciFi", SortName: "secretscifi", OfficialRating: "PG",
	}

	for _, it := range []*sqlite.ItemRecord{favSciFi, unplayedSciFi, unplayedComedy, rRatedSciFi, nsfwSciFi, blockedLibItem} {
		_ = env.libRepo.UpsertItem(ctx, it)
	}
	for _, id := range []string{favSciFi.ID, unplayedSciFi.ID, rRatedSciFi.ID, nsfwSciFi.ID, blockedLibItem.ID} {
		_ = env.libRepo.ReplaceItemTaxonomies(ctx, id, "genre", []string{"Sci-Fi"})
	}
	_ = env.libRepo.ReplaceItemTaxonomies(ctx, unplayedComedy.ID, "genre", []string{"Comedy"})
	_ = env.libRepo.ReplaceItemTaxonomies(ctx, nsfwSciFi.ID, "tag", []string{"NSFW"})

	// Mark favSciFi as played & favorite for user2
	_, _ = env.libRepo.MarkFavorite(ctx, env.user2.ID, favSciFi.ID)
	_, _ = env.libRepo.MarkPlayed(ctx, env.user2.ID, favSciFi.ID)

	// Restrict user2 to libAllowed, MaxParentalRating=7 (PG-13), BlockedTags=["NSFW"]
	maxPG13 := 7
	pol := sqlite.DefaultUserPolicy(false)
	pol.EnableAllFolders = false
	pol.EnabledFolders = []string{libAllowed.ID}
	pol.MaxParentalRating = &maxPG13
	pol.BlockedTags = []string{"NSFW"}
	_ = env.userRepo.UpdateUserPolicy(ctx, env.user2.ID, pol)

	guestAuth, err := authSvc.AuthenticateByName(ctx, "9guest", "pass", "", auth.ClientInfo{DeviceID: "guest-dev"})
	if err != nil {
		t.Fatal(err)
	}

	router := env.handler.BuildRouter(AuthFuncs{
		RequireAuth:  authMW.RequireAuth,
		RequireAdmin: authMW.RequireAdmin,
		UserIDFromCtx: func(c context.Context) string {
			if ac, ok := middleware.AuthFromContext(c); ok && ac != nil {
				return ac.UserID()
			}
			return ""
		},
	})

	doAuthReq := func(token, path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("X-Emby-Token", token)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}

	// 1. GET /Users/{UserId}/Suggestions
	rec := doAuthReq(guestAuth.AccessToken, "/Users/"+env.user2.ID+"/Suggestions?Type=Movie&Limit=10")
	if rec.Code != http.StatusOK {
		t.Fatalf("Suggestions expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var sugg dto.QueryResult[dto.BaseItemDto]
	_ = json.Unmarshal(rec.Body.Bytes(), &sugg)

	forbiddenIDs := map[string]bool{
		rRatedSciFi.ID:    true,
		nsfwSciFi.ID:      true,
		blockedLibItem.ID: true,
		favSciFi.ID:       true, // already played
	}
	if len(sugg.Items) < 2 {
		t.Fatalf("expected at least 2 unplayed allowed suggestions, got %+v", sugg.Items)
	}
	for _, it := range sugg.Items {
		if forbiddenIDs[it.Id] {
			t.Fatalf("unexpected restricted/played item in Suggestions: %s (%s)", it.Name, it.Id)
		}
	}
	// Preferred genre (Sci-Fi) unplayed item must come first before Comedy fallback.
	if sugg.Items[0].Id != unplayedSciFi.ID {
		t.Fatalf("expected preferred genre item UnplayedSciFi first, got %s", sugg.Items[0].Name)
	}

	// 2. GET /Users/{UserId}/Sections/{SectionId}/Items
	rec = doAuthReq(guestAuth.AccessToken, "/Users/"+env.user2.ID+"/Sections/latest/Items?Limit=20")
	if rec.Code != http.StatusOK {
		t.Fatalf("Sections Items expected 200, got %d", rec.Code)
	}
	var secItems dto.QueryResult[dto.BaseItemDto]
	_ = json.Unmarshal(rec.Body.Bytes(), &secItems)
	for _, it := range secItems.Items {
		if it.Id == rRatedSciFi.ID || it.Id == nsfwSciFi.ID || it.Id == blockedLibItem.ID {
			t.Fatalf("unexpected restricted item in SectionItems: %s (%s)", it.Name, it.Id)
		}
	}
	if secItems.TotalRecordCount != 3 {
		t.Fatalf("expected 3 allowed items in SectionItems (favSciFi, unplayedSciFi, unplayedComedy), got %d", secItems.TotalRecordCount)
	}
}
