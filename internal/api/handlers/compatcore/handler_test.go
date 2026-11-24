package compatcore

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ambra-media/ambra/internal/api/dto"
	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/core/auth"
	"github.com/ambra-media/ambra/internal/core/session"
	"github.com/ambra-media/ambra/internal/pkg/eventbus"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/plugins"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

type testEnv struct {
	cfg      *config.Config
	store    *sqlite.Store
	userRepo *sqlite.UserRepo
	libRepo  *sqlite.LibraryRepo
	sessions *session.Manager
	registry *plugins.Registry
	handler  *Handler
	router   http.Handler
	admin    *sqlite.UserRecord
	user2    *sqlite.UserRecord
	itemID   string
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	tmp := t.TempDir()
	cfg, err := config.Load(filepath.Join(tmp, "data"))
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	store, err := sqlite.Open(cfg.DBPath())
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	userRepo := sqlite.NewUserRepo(store)
	libRepo := sqlite.NewLibraryRepo(store)
	bus := eventbus.New()
	sessMgr := session.NewManager(userRepo, cfg, bus)
	reg := plugins.NewRegistry()

	ctx := context.Background()
	authSvc := auth.NewService(userRepo, cfg)
	admin, err := authSvc.CreateUser(ctx, "admin", "admin", true)
	if err != nil {
		t.Fatalf("CreateUser admin: %v", err)
	}
	user2, err := authSvc.CreateUser(ctx, "9guest", "pass", false)
	if err != nil {
		t.Fatalf("CreateUser guest: %v", err)
	}

	lib, err := libRepo.CreateLibrary(ctx, "Movies", "movies", []string{tmp})
	if err != nil {
		t.Fatalf("CreateLibrary: %v", err)
	}
	itemID := guid.New()
	err = libRepo.UpsertItem(ctx, &sqlite.ItemRecord{
		ID:        itemID,
		LibraryID: lib.ID,
		ParentID:  lib.ID,
		Type:      "Movie",
		MediaType: "Video",
		Name:      "Inception",
		SortName:  "inception",
		Etag:      "etag1",
	})
	if err != nil {
		t.Fatalf("UpsertItem: %v", err)
	}

	h := New(cfg, store, userRepo, libRepo, sessMgr, reg)
	router := h.BuildRouter(AuthFuncs{
		UserIDFromCtx: func(context.Context) string { return admin.ID },
	})

	return &testEnv{
		cfg:      cfg,
		store:    store,
		userRepo: userRepo,
		libRepo:  libRepo,
		sessions: sessMgr,
		registry: reg,
		handler:  h,
		router:   router,
		admin:    admin,
		user2:    user2,
		itemID:   itemID,
	}
}

func doReq(t *testing.T, router http.Handler, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	var req *http.Request
	if body != "" {
		req = httptest.NewRequest(method, target, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	} else {
		req = httptest.NewRequest(method, target, nil)
	}
	req.RemoteAddr = "127.0.0.1:12345"
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func assertRegistered(t *testing.T, method, path string, rec *httptest.ResponseRecorder) {
	t.Helper()
	ct := rec.Header().Get("Content-Type")
	respBody := strings.TrimSpace(rec.Body.String())
	isUnregistered := rec.Code == http.StatusMethodNotAllowed ||
		(rec.Code == http.StatusNotFound && !strings.Contains(ct, "application/json") && (respBody == "404 page not found" || respBody == ""))
	if isUnregistered {
		t.Fatalf("route %s %s appears unregistered: code=%d ct=%q body=%q", method, path, rec.Code, ct, respBody)
	}
}

func TestAll93MissingRoutesCovered(t *testing.T) {
	env := newTestEnv(t)
	uid := env.admin.ID
	iid := env.itemID

	routes := []struct {
		method string
		path   string
		body   string
	}{
		// /Appstore (1)
		{"POST", "/Appstore/Register", `{"Key":"abc"}`},
		// /Auth (2)
		{"GET", "/Auth/Providers", ""},
		{"POST", "/Auth/Keys/testkey/Delete", ""},
		// /BackupRestore (1)
		{"POST", "/BackupRestore/RestoreData", `{"RestoreUsers":true}`},
		// /Connect (1)
		{"GET", "/Connect/Exchange?ConnectUserId=c1", ""},
		// /Devices (6)
		{"GET", "/Devices/Info?Id=dev1", ""},
		{"GET", "/Devices/Options?Id=dev1", ""},
		{"POST", "/Devices/Options?Id=dev1", `{"CustomName":"Living Room TV"}`},
		{"GET", "/Devices/CameraUploads?DeviceId=dev1", ""},
		{"POST", "/Devices/CameraUploads?DeviceId=dev1&Id=f1&Name=a.jpg&Album=Photos", ""},
		{"POST", "/Devices/Delete?Id=dev1", ""},
		// /Environment (2)
		{"POST", "/Environment/DirectoryContents", `{"Path":"` + env.cfg.DataDir + `","IncludeFiles":true,"IncludeDirectories":true}`},
		{"GET", "/Environment/NetworkDevices", ""},
		// /Packages (1)
		{"POST", "/Packages/Installing/pkg1/Delete", ""},
		// /Parties (2)
		{"POST", "/Parties/Leave", `{}`},
		{"POST", "/Parties/Messages", `{"Text":"hello"}`},
		// /Playback (1)
		{"GET", "/Playback/BitrateTest?Size=1024", ""},
		// /Plugins (5)
		{"GET", "/Plugins/plug1/Thumb", ""},
		{"POST", "/Plugins/plug1/Delete", ""},
		{"GET", "/Plugins/SecurityInfo", ""},
		{"POST", "/Plugins/SecurityInfo", `{"SupporterKey":"k1"}`},
		{"GET", "/Plugins/RegistrationRecords/TestFeature", ""},
		// /ScheduledTasks (1)
		{"POST", "/ScheduledTasks/Running/task1/Delete", ""},
		// /Sessions (12)
		{"GET", "/Sessions/PlayQueue", ""},
		{"POST", "/Sessions/s1/Viewing?ItemId=" + iid + "&ItemType=Movie", ""},
		{"POST", "/Sessions/s1/Playing?ItemIds=" + iid + "&PlayCommand=PlayNow", `{}`},
		{"POST", "/Sessions/s1/Playing/Pause", `{}`},
		{"POST", "/Sessions/s1/System/Restart", ""},
		{"POST", "/Sessions/s1/Command/GoHome", `{}`},
		{"POST", "/Sessions/s1/Command", `{"Name":"DisplayMessage"}`},
		{"POST", "/Sessions/s1/Message?Header=Hi&Text=World", `{}`},
		{"POST", "/Sessions/s1/Users/" + env.user2.ID, ""},
		{"DELETE", "/Sessions/s1/Users/" + env.user2.ID, ""},
		{"POST", "/Sessions/s1/Users/" + env.user2.ID + "/Delete", ""},
		{"POST", "/Sessions/Playing/Ping", ""},
		// /System (6)
		{"POST", "/System/Restart", ""},
		{"POST", "/System/Shutdown", ""},
		{"GET", "/System/Endpoint", ""},
		{"GET", "/System/WakeOnLanInfo", ""},
		{"GET", "/System/ReleaseNotes/Versions", ""},
		{"GET", "/System/ReleaseNotes", ""},
		// /UserSettings (3)
		{"POST", "/UserSettings/" + uid, `{"theme":"dark"}`},
		{"POST", "/UserSettings/" + uid + "/Partial", `{"lang":"zh-CN"}`},
		{"GET", "/UserSettings/" + uid, ""},
		// /Users (48)
		{"GET", "/Users/" + uid + "/Suggestions", ""},
		{"POST", "/Users/" + uid + "/CopyData", `{"ToUserIds":["` + env.user2.ID + `"]}`},
		{"DELETE", "/Users/" + uid + "/TrackSelections/Audio", ""},
		{"POST", "/Users/" + uid + "/TrackSelections/Subtitle/Delete", ""},
		{"DELETE", "/Users/" + uid + "/RecentlySearched", ""},
		{"POST", "/Users/" + uid + "/RecentlySearched/Delete", ""},
		{"POST", "/Users/" + uid + "/Authenticate", `{}`},
		{"POST", "/Users/" + uid, `{"Name":"admin"}`},
		{"POST", "/Users/" + uid + "/Configuration/Partial", `{"SubtitleMode":"Always"}`},
		{"POST", "/Users/ForgotPassword", `{"EnteredUsername":"admin"}`},
		{"POST", "/Users/ForgotPassword/Pin", `{"Pin":"INVALID"}`},
		{"GET", "/Users/" + uid + "/TypedSettings/display", ""},
		{"POST", "/Users/" + uid + "/TypedSettings/display", `{"grid":true}`},
		{"GET", "/Users/" + uid + "/HomeSections", ""},
		{"POST", "/Users/" + uid + "/HomeSections", `{"Id":"custom1","Name":"Custom"}`},
		{"POST", "/Users/" + uid + "/HomeSections/Move?Id=custom1&NewIndex=0", `{}`},
		{"POST", "/Users/" + uid + "/HomeSections/Delete?Ids=custom1", `{}`},
		{"GET", "/Users/" + uid + "/Sections/latest/Items", ""},
		{"GET", "/Users/" + uid + "/Items/" + iid + "/Intros", ""},
		{"POST", "/Users/" + uid + "/SearchedItems/", `{"ItemId":"` + iid + `"}`},
		{"POST", "/Users/" + uid + "/FavoriteItems/" + iid + "/Delete", ""},
		{"POST", "/Users/" + uid + "/Items/" + iid + "/Rating?Likes=true", ""},
		{"DELETE", "/Users/" + uid + "/Items/" + iid + "/Rating", ""},
		{"POST", "/Users/" + uid + "/Items/" + iid + "/Rating/Delete", ""},
		{"POST", "/Users/" + uid + "/Items/" + iid + "/HideFromResume?Hide=true", ""},
		{"GET", "/Users/" + uid + "/Items/" + iid + "/LocalTrailers", ""},
		{"GET", "/Users/" + uid + "/Items/" + iid + "/SpecialFeatures", ""},
		{"GET", "/Users/" + uid + "/GroupingOptions", ""},
		{"GET", "/Users/" + uid + "/Images/Primary", ""},
		{"GET", "/Users/" + uid + "/Images/Primary/0", ""},
		{"HEAD", "/Users/" + uid + "/Images/Primary", ""},
		{"HEAD", "/Users/" + uid + "/Images/Primary/0", ""},
		{"POST", "/Users/" + uid + "/Images/Primary", base64.StdEncoding.EncodeToString([]byte("fake-png-bytes"))},
		{"POST", "/Users/" + uid + "/Images/Primary/0", "raw-image-bytes"},
		{"DELETE", "/Users/" + uid + "/Images/Primary", ""},
		{"DELETE", "/Users/" + uid + "/Images/Primary/0", ""},
		{"POST", "/Users/" + uid + "/Images/Primary/Delete", ""},
		{"POST", "/Users/" + uid + "/Images/Primary/0/Delete", ""},
		{"POST", "/Users/" + uid + "/Connect/Link?ConnectUsername=embyuser", ""},
		{"DELETE", "/Users/" + uid + "/Connect/Link", ""},
		{"POST", "/Users/" + uid + "/Connect/Link/Delete", ""},
		{"POST", "/Users/" + uid + "/Items/" + iid + "/UserData", `{"Played":true,"IsFavorite":true,"PlaybackPositionTicks":500}`},
		{"POST", "/Users/" + uid + "/PlayedItems/" + iid + "/Delete", ""},
		{"POST", "/Users/" + uid + "/PlayingItems/" + iid + "?CanSeek=true&PositionTicks=100", ""},
		{"POST", "/Users/" + uid + "/PlayingItems/" + iid + "/Progress?IsPaused=true&PositionTicks=200", ""},
		{"DELETE", "/Users/" + uid + "/PlayingItems/" + iid + "?PositionTicks=300", ""},
		{"POST", "/Users/" + uid + "/PlayingItems/" + iid + "/Delete?PositionTicks=300", ""},
		{"POST", "/Users/" + env.user2.ID + "/Delete", ""},
		// /Webhooks (1)
		{"GET", "/Webhooks/Sample", ""},
		// Extra compatibility routes
		{"GET", "/Users/Query?StartIndex=0&Limit=10", ""},
		{"GET", "/Users/Prefix", ""},
		{"GET", "/Users/" + uid + "/Items/Root", ""},
		{"GET", "/System/Info/Storage", ""},
		{"POST", "/Plugins/plug1/Uninstall", ""},
	}

	if len(routes) != 98 {
		t.Fatalf("expected 98 route checks (93 missing + 5 extra), got %d", len(routes))
	}

	for _, rt := range routes {
		rec := doReq(t, env.router, rt.method, rt.path, rt.body)
		assertRegistered(t, rt.method, rt.path, rec)
	}
}

func TestUserSettingsAndTypedSettingsPersistence(t *testing.T) {
	env := newTestEnv(t)
	uid := env.admin.ID

	rec := doReq(t, env.router, "POST", "/UserSettings/"+uid, `{"theme":"dark","fontSize":16}`)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("POST UserSettings got %d", rec.Code)
	}
	rec = doReq(t, env.router, "POST", "/UserSettings/"+uid+"/Partial", `{"lang":"zh-CN"}`)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("POST UserSettings Partial got %d", rec.Code)
	}
	rec = doReq(t, env.router, "GET", "/UserSettings/"+uid, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET UserSettings got %d", rec.Code)
	}
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got["theme"] != "dark" || got["lang"] != "zh-CN" {
		t.Fatalf("unexpected settings: %#v", got)
	}

	// Typed settings
	rec = doReq(t, env.router, "POST", "/Users/"+uid+"/TypedSettings/ui", `{"compact":true}`)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("POST TypedSettings got %d", rec.Code)
	}
	rec = doReq(t, env.router, "GET", "/Users/"+uid+"/TypedSettings/ui", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"compact":true`) {
		t.Fatalf("GET TypedSettings got %d %s", rec.Code, rec.Body.String())
	}
}

func TestUserImagesLifecycle(t *testing.T) {
	env := newTestEnv(t)
	uid := env.admin.ID

	// Missing image returns JSON 404 for both GET and HEAD
	rec := doReq(t, env.router, "GET", "/Users/"+uid+"/Images/Primary", "")
	if rec.Code != http.StatusNotFound || !strings.Contains(rec.Header().Get("Content-Type"), "application/json") {
		t.Fatalf("expected JSON 404, got %d %s", rec.Code, rec.Header().Get("Content-Type"))
	}
	rec = doReq(t, env.router, "HEAD", "/Users/"+uid+"/Images/Primary", "")
	if rec.Code != http.StatusNotFound || !strings.Contains(rec.Header().Get("Content-Type"), "application/json") {
		t.Fatalf("expected JSON 404 on HEAD, got %d %s", rec.Code, rec.Header().Get("Content-Type"))
	}

	// Upload base64 image
	payload := []byte("\x89PNG\r\n\x1a\nfake-png-content")
	b64 := base64.StdEncoding.EncodeToString(payload)
	rec = doReq(t, env.router, "POST", "/Users/"+uid+"/Images/Primary", b64)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("POST image got %d", rec.Code)
	}

	// GET & HEAD should return 200
	rec = doReq(t, env.router, "GET", "/Users/"+uid+"/Images/Primary", "")
	if rec.Code != http.StatusOK || !bytes.Equal(rec.Body.Bytes(), payload) {
		t.Fatalf("GET image got %d len=%d", rec.Code, rec.Body.Len())
	}
	rec = doReq(t, env.router, "HEAD", "/Users/"+uid+"/Images/Primary", "")
	if rec.Code != http.StatusOK || rec.Body.Len() != 0 {
		t.Fatalf("HEAD image got %d len=%d", rec.Code, rec.Body.Len())
	}

	// DELETE removes image
	rec = doReq(t, env.router, "DELETE", "/Users/"+uid+"/Images/Primary", "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("DELETE image got %d", rec.Code)
	}
	rec = doReq(t, env.router, "GET", "/Users/"+uid+"/Images/Primary", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 after delete, got %d", rec.Code)
	}
}

func TestForgotPasswordAndPinRedeem(t *testing.T) {
	env := newTestEnv(t)

	rec := doReq(t, env.router, "POST", "/Users/ForgotPassword", `{"EnteredUsername":"admin"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("ForgotPassword got %d", rec.Code)
	}
	var res map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &res)
	if res["Action"] != "PinCode" {
		t.Fatalf("unexpected ForgotPassword result: %#v", res)
	}

	// Extract generated PIN from handler state and redeem it
	env.handler.mu.RLock()
	var validPin string
	for k := range env.handler.forgotPins {
		validPin = k
		break
	}
	env.handler.mu.RUnlock()
	if validPin == "" {
		t.Fatal("expected a generated forgot pin")
	}

	rec = doReq(t, env.router, "POST", "/Users/ForgotPassword/Pin", `{"Pin":"`+validPin+`"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("ForgotPassword/Pin got %d", rec.Code)
	}
	var pinRes struct {
		Success    bool     `json:"Success"`
		UsersReset []string `json:"UsersReset"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &pinRes)
	if !pinRes.Success || len(pinRes.UsersReset) != 1 || pinRes.UsersReset[0] != "admin" {
		t.Fatalf("unexpected pin redeem result: %+v", pinRes)
	}
}

func TestBitrateTestSizes(t *testing.T) {
	env := newTestEnv(t)

	rec := doReq(t, env.router, "GET", "/Playback/BitrateTest?Size=4096", "")
	if rec.Code != http.StatusOK || rec.Body.Len() != 4096 {
		t.Fatalf("BitrateTest 4096 got code=%d len=%d", rec.Code, rec.Body.Len())
	}

	rec = doReq(t, env.router, "GET", "/Playback/BitrateTest?Size=0", "")
	if rec.Code != http.StatusOK || rec.Body.Len() != defaultBitrateTestSize {
		t.Fatalf("BitrateTest default got code=%d len=%d", rec.Code, rec.Body.Len())
	}

	rec = doReq(t, env.router, "GET", "/Playback/BitrateTest?Size=999999999", "")
	if rec.Code != http.StatusOK || rec.Body.Len() != maxBitrateTestSize {
		t.Fatalf("BitrateTest capped got code=%d len=%d", rec.Code, rec.Body.Len())
	}
}

func TestDevicesSessionsAndAuthLifecycle(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()

	// Seed a device token and API key
	_ = env.userRepo.CreateAccessToken(ctx, &sqlite.AccessTokenRecord{
		Token:      "tok-dev-1",
		UserID:     env.admin.ID,
		DeviceID:   "dev-real-1",
		DeviceName: "Apple TV",
		AppName:    "Infuse",
		AppVersion: "7.8",
	})
	_ = env.userRepo.CreateAPIKey(ctx, &sqlite.APIKeyRecord{
		Token:   "api-key-1",
		AppName: "Sonarr",
	})

	// Device Info finds seeded device
	rec := doReq(t, env.router, "GET", "/Devices/Info?Id=dev-real-1", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Apple TV") {
		t.Fatalf("GET Devices/Info got %d %s", rec.Code, rec.Body.String())
	}

	// Device Options roundtrip
	rec = doReq(t, env.router, "POST", "/Devices/Options?Id=dev-real-1", `{"CustomName":"Theater"}`)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("POST Devices/Options got %d", rec.Code)
	}
	rec = doReq(t, env.router, "GET", "/Devices/Options?Id=dev-real-1", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Theater") {
		t.Fatalf("GET Devices/Options got %d %s", rec.Code, rec.Body.String())
	}

	// Device CameraUploads roundtrip
	rec = doReq(t, env.router, "POST", "/Devices/CameraUploads?DeviceId=dev-real-1&Id=p1&Name=img.jpg&Album=Trip", "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("POST CameraUploads got %d", rec.Code)
	}
	rec = doReq(t, env.router, "GET", "/Devices/CameraUploads?DeviceId=dev-real-1", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "img.jpg") {
		t.Fatalf("GET CameraUploads got %d %s", rec.Code, rec.Body.String())
	}

	// Delete device via body
	rec = doReq(t, env.router, "POST", "/Devices/Delete", `{"Id":"dev-real-1"}`)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("POST Devices/Delete got %d", rec.Code)
	}

	// Delete API key
	rec = doReq(t, env.router, "POST", "/Auth/Keys/api-key-1/Delete", "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("POST Auth/Keys/Delete got %d", rec.Code)
	}
	if _, err := env.userRepo.GetAPIKey(ctx, "api-key-1"); err == nil {
		t.Fatal("expected api key to be deleted")
	}

	// Session PlayQueue after PlayingItems start
	rec = doReq(t, env.router, "POST", "/Users/"+env.admin.ID+"/PlayingItems/"+env.itemID, "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("POST PlayingItems got %d", rec.Code)
	}
	rec = doReq(t, env.router, "GET", "/Sessions/PlayQueue", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET PlayQueue got %d", rec.Code)
	}
	var pq dto.QueryResult[dto.BaseItemDto]
	_ = json.Unmarshal(rec.Body.Bytes(), &pq)
	if pq.TotalRecordCount == 0 {
		t.Fatalf("expected non-empty play queue")
	}
}

func TestHomeSectionsAndUserQueriesAndEdgeCases(t *testing.T) {
	env := newTestEnv(t)
	uid := env.admin.ID

	// Default home sections
	rec := doReq(t, env.router, "GET", "/Users/"+uid+"/HomeSections", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "smalllibrarytiles") {
		t.Fatalf("GET HomeSections got %d %s", rec.Code, rec.Body.String())
	}

	// Add, replace, move, delete home sections
	rec = doReq(t, env.router, "POST", "/Users/"+uid+"/HomeSections", `{"Id":"secA","Name":"A"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST HomeSections add got %d", rec.Code)
	}
	rec = doReq(t, env.router, "POST", "/Users/"+uid+"/HomeSections", `{"Id":"secA","Name":"A2"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST HomeSections replace got %d", rec.Code)
	}
	rec = doReq(t, env.router, "POST", "/Users/"+uid+"/HomeSections/Move", `{"Id":"secA","NewIndex":99}`)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("POST HomeSections/Move got %d", rec.Code)
	}
	rec = doReq(t, env.router, "POST", "/Users/"+uid+"/HomeSections/Move", `{"Id":"secA","NewIndex":-1}`)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("POST HomeSections/Move negative got %d", rec.Code)
	}
	rec = doReq(t, env.router, "POST", "/Users/"+uid+"/HomeSections/Delete", `{"Ids":["secA"]}`)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("POST HomeSections/Delete got %d", rec.Code)
	}

	// Query users with filters & bounds
	rec = doReq(t, env.router, "GET", "/Users/Query?NameStartsWithOrGreater=a&StartIndex=-1&Limit=1", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET Users/Query got %d", rec.Code)
	}
	rec = doReq(t, env.router, "GET", "/Users/Query?StartIndex=999", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET Users/Query out of bounds got %d", rec.Code)
	}

	// Update user with config & policy
	rec = doReq(t, env.router, "POST", "/Users/"+uid, `{"Name":"admin2","Configuration":{"PlayDefaultAudioTrack":true},"Policy":{"IsAdministrator":true}}`)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("POST Users/{Id} got %d", rec.Code)
	}

	// Track selections default branch
	rec = doReq(t, env.router, "DELETE", "/Users/"+uid+"/TrackSelections/All", "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("DELETE TrackSelections All got %d", rec.Code)
	}

	// UserData false branches
	rec = doReq(t, env.router, "POST", "/Users/"+uid+"/Items/"+env.itemID+"/UserData", `{"Played":false,"IsFavorite":false}`)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("POST UserData false got %d", rec.Code)
	}

	// Disabled user cannot authenticate
	disabledPolicy := sqlite.DefaultUserPolicy(false)
	disabledPolicy.IsDisabled = true
	_ = env.userRepo.UpdateUserPolicy(context.Background(), env.user2.ID, disabledPolicy)
	rec = doReq(t, env.router, "POST", "/Users/"+env.user2.ID+"/Authenticate", `{}`)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for disabled user, got %d", rec.Code)
	}

	// Nonexistent user endpoints return JSON 404
	for _, tc := range []struct{ m, p string }{
		{"POST", "/Users/missing-id/Authenticate"},
		{"POST", "/Users/missing-id"},
		{"POST", "/Users/missing-id/Delete"},
		{"POST", "/Users/missing-id/Configuration/Partial"},
	} {
		rec = doReq(t, env.router, tc.m, tc.p, `{}`)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404 for %s %s, got %d", tc.m, tc.p, rec.Code)
		}
	}

	// Invalid JSON returns 400
	for _, tc := range []struct{ m, p string }{
		{"POST", "/Users/" + uid},
		{"POST", "/Users/" + uid + "/Configuration/Partial"},
		{"POST", "/Users/" + uid + "/CopyData"},
		{"POST", "/Users/" + uid + "/TypedSettings/k"},
		{"POST", "/Users/" + uid + "/HomeSections"},
		{"POST", "/Users/" + uid + "/Items/" + env.itemID + "/UserData"},
		{"POST", "/UserSettings/" + uid},
		{"POST", "/UserSettings/" + uid + "/Partial"},
		{"POST", "/Devices/Options?Id=d1"},
		{"POST", "/Plugins/SecurityInfo"},
		{"POST", "/Appstore/Register"},
		{"POST", "/BackupRestore/RestoreData"},
	} {
		rec = doReq(t, env.router, tc.m, tc.p, `{bad-json`)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for %s %s, got %d", tc.m, tc.p, rec.Code)
		}
	}

	// Environment directory contents: file filtering & missing dir
	tmpDir := t.TempDir()
	_ = os.WriteFile(filepath.Join(tmpDir, "a.txt"), []byte("hi"), 0o644)
	_ = os.Mkdir(filepath.Join(tmpDir, "sub"), 0o755)
	rec = doReq(t, env.router, "POST", "/Environment/DirectoryContents?IncludeFiles=true&IncludeDirectories=false", `{"Path":"`+tmpDir+`"}`)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "a.txt") || strings.Contains(rec.Body.String(), "sub") {
		t.Fatalf("DirectoryContents files only got %d %s", rec.Code, rec.Body.String())
	}
	rec = doReq(t, env.router, "POST", "/Environment/DirectoryContents?Path=/nonexistent-path-ambra-test", "")
	if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != "[]" {
		t.Fatalf("DirectoryContents missing got %d %s", rec.Code, rec.Body.String())
	}
}

func TestNilDepsFallback(t *testing.T) {
	h := New(nil, nil, nil, nil, nil, nil)
	r := h.BuildRouter(AuthFuncs{})

	// Exercise in-memory KV fallback and nil repo safe branches
	rec := doReq(t, r, "POST", "/UserSettings/u1", `{"a":1}`)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("memKV save got %d", rec.Code)
	}
	rec = doReq(t, r, "GET", "/UserSettings/u1", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"a":1`) {
		t.Fatalf("memKV load got %d %s", rec.Code, rec.Body.String())
	}
	rec = doReq(t, r, "DELETE", "/Users/u1/Connect/Link", "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("memKV delete got %d", rec.Code)
	}
	rec = doReq(t, r, "GET", "/Users/u1/Suggestions", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("nil libRepo Suggestions got %d", rec.Code)
	}
	rec = doReq(t, r, "GET", "/Users/u1/Sections/s1/Items", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("nil libRepo SectionItems got %d", rec.Code)
	}
	rec = doReq(t, r, "GET", "/System/Info/Storage", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("nil cfg StorageInfo got %d", rec.Code)
	}
	rec = doReq(t, r, "POST", "/Environment/DirectoryContents", `{}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("nil cfg DirectoryContents got %d", rec.Code)
	}
	if sanitizeFilename("!!!") != "default" {
		t.Fatal("expected default sanitized filename")
	}
}
