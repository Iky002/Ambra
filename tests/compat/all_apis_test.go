package compat_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/ambra-media/ambra/internal/api/dto"
	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/server"
)

func loadBaselineRoutes(t *testing.T) []baselineRoute {
	t.Helper()
	raw, err := os.ReadFile("emby_4.10.2.0_routes.json")
	if err != nil {
		t.Fatalf("read baseline routes: %v", err)
	}
	var routes []baselineRoute
	if err := json.Unmarshal(raw, &routes); err != nil {
		t.Fatalf("unmarshal baseline routes: %v", err)
	}
	if len(routes) != 582 {
		t.Fatalf("expected 582 baseline routes, got %d", len(routes))
	}
	return routes
}

func newCompatTestServer(t *testing.T) (*server.Server, *config.Config, string, string) {
	t.Helper()
	t.Setenv("AMBRA_ADMIN_USER", "admin")
	t.Setenv("AMBRA_ADMIN_PASS", "admin")
	tmpDir := t.TempDir()
	dataDir := filepath.Join(tmpDir, "data")
	cfg, err := config.Load(dataDir)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	srv, err := server.New(context.Background(), cfg)
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}
	t.Cleanup(func() { _ = srv.Close() })

	token, userID := loginUser(t, srv.Handler, "admin", "admin", "compat-admin-dev")
	return srv, cfg, token, userID
}

func loginUser(t *testing.T, handler http.Handler, username, pw, deviceID string) (string, string) {
	t.Helper()
	payload, _ := json.Marshal(map[string]string{
		"Username": username,
		"Pw":       pw,
	})
	req := httptest.NewRequest(http.MethodPost, "/emby/Users/AuthenticateByName", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Emby-Authorization", fmt.Sprintf(`MediaBrowser Client="AmbraCompatTest", Device="GoTest", DeviceId="%s", Version="4.10.2.0"`, deviceID))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("login %q failed: status=%d body=%s", username, rec.Code, rec.Body.String())
	}
	var authRes dto.AuthenticationResult
	if err := json.NewDecoder(rec.Body).Decode(&authRes); err != nil {
		t.Fatalf("decode auth result for %q: %v", username, err)
	}
	if authRes.AccessToken == "" || authRes.User.Id == "" {
		t.Fatalf("empty token or user id for %q: %+v", username, authRes)
	}
	return authRes.AccessToken, authRes.User.Id
}

func isUnregisteredResponse(w *httptest.ResponseRecorder) bool {
	if w.Code == http.StatusMethodNotAllowed {
		return true
	}
	if w.Code == http.StatusNotFound {
		ct := w.Header().Get("Content-Type")
		respBody := strings.TrimSpace(w.Body.String())
		if !strings.Contains(ct, "application/json") && (respBody == "404 page not found" || respBody == "") {
			return true
		}
	}
	return false
}

func sendCompatRequest(handler http.Handler, method, reqPath, token string, body []byte) *httptest.ResponseRecorder {
	var bodyReader io.Reader
	if body != nil {
		bodyReader = bytes.NewReader(body)
	} else if method == http.MethodPost || method == http.MethodPut || method == http.MethodPatch {
		bodyReader = strings.NewReader(`{}`)
	} else {
		bodyReader = strings.NewReader("")
	}
	req := httptest.NewRequest(method, reqPath, bodyReader)
	if token != "" {
		req.Header.Set("X-Emby-Token", token)
	}
	req.Header.Set("Content-Type", "application/json")
	if method == "SUBSCRIBE" {
		req.Header.Set("CALLBACK", "<http://127.0.0.1:9999/notify>")
		req.Header.Set("NT", "upnp:event")
	}
	lowerPath := strings.ToLower(reqPath)
	if method == http.MethodPost && strings.HasSuffix(lowerPath, "/contentdirectory/control") {
		req.Header.Set("Content-Type", `text/xml; charset="utf-8"`)
		req.Header.Set("SOAPACTION", `"urn:schemas-upnp-org:service:ContentDirectory:1#GetSystemUpdateID"`)
	} else if method == http.MethodPost && strings.HasSuffix(lowerPath, "/connectionmanager/control") {
		req.Header.Set("Content-Type", `text/xml; charset="utf-8"`)
		req.Header.Set("SOAPACTION", `"urn:schemas-upnp-org:service:ConnectionManager:1#GetProtocolInfo"`)
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	return w
}

func seedSampleMovieItem(t *testing.T, srv *server.Server, adminToken, adminUserID string) string {
	t.Helper()
	mediaDir := filepath.Join(t.TempDir(), "seed-movies")
	movieDir := filepath.Join(mediaDir, "Compat Seed Movie (2024)")
	if err := os.MkdirAll(movieDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(movieDir, "Compat Seed Movie (2024).mkv"), []byte("seed-video-bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	addLibBody, _ := json.Marshal(map[string]any{
		"Name":           "CompatSeedMovies",
		"CollectionType": "movies",
		"Paths":          []string{mediaDir},
	})
	w := sendCompatRequest(srv.Handler, http.MethodPost, "/emby/Library/VirtualFolders?name=CompatSeedMovies&collectionType=movies", adminToken, addLibBody)
	if w.Code != http.StatusOK && w.Code != http.StatusNoContent {
		t.Fatalf("seed virtual folder failed: status=%d body=%s", w.Code, w.Body.String())
	}
	if _, err := srv.Scanner.ScanAll(context.Background()); err != nil {
		t.Fatalf("seed ScanAll failed: %v", err)
	}
	w = sendCompatRequest(srv.Handler, http.MethodGet, "/emby/Users/"+adminUserID+"/Items?IncludeItemTypes=Movie", adminToken, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("seed query movies failed: status=%d", w.Code)
	}
	var res dto.QueryResult[dto.BaseItemDto]
	if err := json.NewDecoder(w.Body).Decode(&res); err != nil || len(res.Items) == 0 {
		t.Fatalf("seed movie not found: err=%v count=%d", err, len(res.Items))
	}
	return res.Items[0].Id
}

func isStaticRootRoute(routePath string) bool {
	p := strings.ToLower(strings.TrimPrefix(routePath, "/"))
	first := strings.SplitN(p, "/", 2)[0]
	return first == "web" || first == "favicon.ico" || first == "robots.txt"
}

// TestAll582APIsDualPrefixAndNoServerError verifies all 582 Emby 4.10.2.0 API routes
// (583 Method+Path combinations) with both bare and /emby prefixes:
// - 0 unregistered 404 / 405 responses
// - 0 5xx internal server errors / panics
// - identical status codes between bare and /emby-prefixed paths
func TestAll582APIsDualPrefixAndNoServerError(t *testing.T) {
	routes := loadBaselineRoutes(t)
	srv, cfg, adminToken, adminUserID := newCompatTestServer(t)
	realItemID := seedSampleMovieItem(t, srv, adminToken, adminUserID)

	var (
		totalChecks     int
		unregistered    []string
		serverErrors    []string
		prefixMismatch  []string
		staticRootCount int
	)

	for _, rt := range routes {
		for _, v := range strings.Split(rt.Verbs, ",") {
			method := strings.TrimSpace(strings.ToUpper(v))
			if method == "" {
				continue
			}
			totalChecks++

			itemID := "nonexistent-item-id"
			lowerRoute := strings.ToLower(rt.Path)
			if strings.Contains(lowerRoute, "/favoriteitems/") || strings.Contains(lowerRoute, "/playeditems/") {
				itemID = realItemID
			}
			path := materializePath(rt.Path, adminUserID, itemID, "src-0", cfg.ServerID)
			if !strings.HasPrefix(path, "/") {
				path = "/" + path
			}

			// Avoid revoking the shared admin session token when exercising /Sessions/Logout
			useToken := adminToken
			isLogout := strings.EqualFold(rt.Path, "/Sessions/Logout")
			if isLogout {
				useToken, _ = loginUser(t, srv.Handler, "admin", "admin", "compat-logout-bare")
			}

			wBare := sendCompatRequest(srv.Handler, method, path, useToken, nil)
			if isUnregisteredResponse(wBare) {
				unregistered = append(unregistered, fmt.Sprintf("BARE %s %s -> %d (%s)", method, path, wBare.Code, rt.ServiceName))
			}
			if wBare.Code >= 500 {
				serverErrors = append(serverErrors, fmt.Sprintf("BARE %s %s -> %d body=%q", method, path, wBare.Code, strings.TrimSpace(wBare.Body.String())))
			}

			if isStaticRootRoute(rt.Path) {
				staticRootCount++
				// Static root paths (/web/*, /favicon.ico, /robots.txt) are validated directly at root
				continue
			}

			embyToken := adminToken
			if isLogout {
				embyToken, _ = loginUser(t, srv.Handler, "admin", "admin", "compat-logout-emby")
			}
			embyPath := "/emby" + path
			wEmby := sendCompatRequest(srv.Handler, method, embyPath, embyToken, nil)
			if isUnregisteredResponse(wEmby) {
				unregistered = append(unregistered, fmt.Sprintf("EMBY %s %s -> %d (%s)", method, embyPath, wEmby.Code, rt.ServiceName))
			}
			if wEmby.Code >= 500 {
				serverErrors = append(serverErrors, fmt.Sprintf("EMBY %s %s -> %d body=%q", method, embyPath, wEmby.Code, strings.TrimSpace(wEmby.Body.String())))
			}
			if wBare.Code != wEmby.Code {
				prefixMismatch = append(prefixMismatch, fmt.Sprintf("%s %s: bare=%d vs /emby=%d", method, rt.Path, wBare.Code, wEmby.Code))
			}
		}
	}

	t.Logf("Dual-prefix audit: total=%d method+path combinations (static_root=%d, dual_checked=%d), unregistered=%d, 5xx=%d, prefix_mismatch=%d",
		totalChecks, staticRootCount, totalChecks-staticRootCount, len(unregistered), len(serverErrors), len(prefixMismatch))

	if totalChecks != 583 {
		t.Fatalf("expected 583 total Method+Path combinations, got %d", totalChecks)
	}
	if len(unregistered) > 0 {
		t.Errorf("unregistered routes (%d, first 20): %v", len(unregistered), unregistered[:min(20, len(unregistered))])
	}
	if len(serverErrors) > 0 {
		t.Errorf("5xx server errors (%d, first 20): %v", len(serverErrors), serverErrors[:min(20, len(serverErrors))])
	}
	if len(prefixMismatch) > 0 {
		t.Errorf("bare vs /emby status mismatches (%d, first 20): %v", len(prefixMismatch), prefixMismatch[:min(20, len(prefixMismatch))])
	}
}

// TestAll582APIsCaseInsensitiveRouting validates lowercase paths across a representative
// subset covering all 71 top-level API prefixes plus key Emby client endpoints.
func TestAll582APIsCaseInsensitiveRouting(t *testing.T) {
	routes := loadBaselineRoutes(t)
	srv, cfg, adminToken, adminUserID := newCompatTestServer(t)

	// Pick one representative route per top-level prefix (preferring GET and non-destructive routes)
	byPrefix := make(map[string]baselineRoute)
	for _, rt := range routes {
		seg := strings.Split(strings.TrimPrefix(rt.Path, "/"), "/")[0]
		existing, ok := byPrefix[seg]
		if !ok {
			byPrefix[seg] = rt
			continue
		}
		// Prefer GET over mutating verbs, and shorter/simpler paths
		existingIsGet := strings.Contains(strings.ToUpper(existing.Verbs), "GET")
		currIsGet := strings.Contains(strings.ToUpper(rt.Verbs), "GET")
		if (!existingIsGet && currIsGet) || (existingIsGet == currIsGet && len(rt.Path) < len(existing.Path)) {
			byPrefix[seg] = rt
		}
	}

	if len(byPrefix) != 71 {
		t.Fatalf("expected 71 unique top-level prefixes, got %d", len(byPrefix))
	}

	prefixes := make([]string, 0, len(byPrefix))
	for p := range byPrefix {
		prefixes = append(prefixes, p)
	}
	sort.Strings(prefixes)

	var failures []string
	checked := 0

	checkLower := func(method, rawPath string) {
		checked++
		mat := materializePath(rawPath, adminUserID, "nonexistent-item-id", "src-0", cfg.ServerID)
		if !strings.HasPrefix(mat, "/") {
			mat = "/" + mat
		}
		lowerPath := strings.ToLower(mat)
		if !isStaticRootRoute(rawPath) {
			lowerPath = "/emby" + lowerPath
		}
		w := sendCompatRequest(srv.Handler, method, lowerPath, adminToken, nil)
		if isUnregisteredResponse(w) || w.Code >= 500 {
			failures = append(failures, fmt.Sprintf("%s %s (from %s) -> %d body=%q", method, lowerPath, rawPath, w.Code, strings.TrimSpace(w.Body.String())))
		}
	}

	// Explicit key lowercase routes required by Emby clients
	explicitCases := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/System/Info/Public"},
		{http.MethodGet, "/LiveTv/Info"},
		{http.MethodGet, "/Dlna/ProfileInfos"},
		{http.MethodGet, "/Users/{UserId}/Views"},
	}
	for _, tc := range explicitCases {
		checkLower(tc.method, tc.path)
	}

	for _, p := range prefixes {
		rt := byPrefix[p]
		method := strings.TrimSpace(strings.ToUpper(strings.Split(rt.Verbs, ",")[0]))
		checkLower(method, rt.Path)
	}

	t.Logf("Case-insensitive routing audit: prefixes=%d, total_checks=%d, failures=%d", len(prefixes), checked, len(failures))
	if len(failures) > 0 {
		t.Fatalf("case-insensitive lowercase routing failed for %d/%d routes:\n  %s", len(failures), checked, strings.Join(failures, "\n  "))
	}
}

// TestAll582APIsAuthEnforcementMatrix verifies:
// 1. Protected endpoints return 401 Unauthorized without a token
// 2. Admin-only endpoints return 403 Forbidden for a normal non-admin user
// 3. Public endpoints return 200 OK anonymously
func TestAll582APIsAuthEnforcementMatrix(t *testing.T) {
	srv, cfg, adminToken, adminUserID := newCompatTestServer(t)

	// 1. Unauthenticated requests to protected endpoints -> 401 Unauthorized
	protectedEndpoints := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/emby/Users"},
		{http.MethodGet, "/emby/Users/" + adminUserID + "/Items"},
		{http.MethodGet, "/emby/Users/" + adminUserID + "/Views"},
		{http.MethodGet, "/emby/Items"},
		{http.MethodGet, "/emby/Library/VirtualFolders"},
		{http.MethodGet, "/emby/Sessions"},
		{http.MethodGet, "/emby/Devices"},
		{http.MethodGet, "/emby/Auth/Keys"},
		{http.MethodGet, "/emby/Environment/Drives"},
		{http.MethodGet, "/emby/ScheduledTasks"},
		{http.MethodGet, "/emby/System/Info"},
		{http.MethodGet, "/emby/System/Logs/Query"},
		{http.MethodGet, "/emby/LiveTv/Info"},
		{http.MethodGet, "/emby/Sync/Jobs"},
		{http.MethodGet, "/emby/Notifications/" + adminUserID + "/Summary"},
		{http.MethodGet, "/emby/BackupRestore/BackupInfo"},
		{http.MethodGet, "/emby/Webhooks"},
		{http.MethodGet, "/emby/Dlna/ProfileInfos"},
		{http.MethodGet, "/emby/Encoding/CodecInformation/Video"},
	}

	for _, ep := range protectedEndpoints {
		w := sendCompatRequest(srv.Handler, ep.method, ep.path, "", nil)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("anonymous %s %s: expected 401 Unauthorized, got %d (body=%q)", ep.method, ep.path, w.Code, strings.TrimSpace(w.Body.String()))
		}
	}

	// 2. Create a normal non-admin user and verify Admin-only endpoints -> 403 Forbidden
	createBody := []byte(`{"Name":"alice","Password":"alice-pass","IsAdministrator":false}`)
	wCreate := sendCompatRequest(srv.Handler, http.MethodPost, "/emby/Users/New", adminToken, createBody)
	if wCreate.Code != http.StatusOK {
		t.Fatalf("create normal user failed: status=%d body=%s", wCreate.Code, wCreate.Body.String())
	}
	aliceToken, _ := loginUser(t, srv.Handler, "alice", "alice-pass", "compat-alice-dev")

	adminOnlyEndpoints := []struct {
		method string
		path   string
		body   []byte
	}{
		{http.MethodPost, "/emby/Users/New", []byte(`{"Name":"bob","Password":"bob"}`)},
		{http.MethodGet, "/emby/Auth/Keys", nil},
		{http.MethodPost, "/emby/Auth/Keys?App=SneakyApp", nil},
		{http.MethodGet, "/emby/Environment/Drives", nil},
		{http.MethodPost, "/emby/Library/VirtualFolders?name=HackLib&collectionType=movies", []byte(`{"Name":"HackLib"}`)},
		{http.MethodGet, "/emby/System/Logs/Query", nil},
		{http.MethodPost, "/emby/BackupRestore/Backup", nil},
		{http.MethodGet, "/emby/BackupRestore/BackupInfo", nil},
		{http.MethodGet, "/emby/Dlna/ProfileInfos", nil},
		{http.MethodGet, "/emby/Webhooks", nil},
		{http.MethodPost, "/emby/Notifications/Admin", []byte(`{"Name":"Alert"}`)},
		{http.MethodGet, "/emby/Startup/Configuration", nil},
	}

	for _, ep := range adminOnlyEndpoints {
		w := sendCompatRequest(srv.Handler, ep.method, ep.path, aliceToken, ep.body)
		if w.Code != http.StatusForbidden {
			t.Errorf("non-admin %s %s: expected 403 Forbidden, got %d (body=%q)", ep.method, ep.path, w.Code, strings.TrimSpace(w.Body.String()))
		}
	}

	// 3. Public endpoints -> 200 OK anonymously
	publicEndpoints := []string{
		"/emby/System/Info/Public",
		"/emby/System/Ping",
		"/emby/Users/Public",
		"/emby/Branding/Configuration",
		"/emby/openapi.json",
		"/robots.txt",
	}

	for _, ep := range publicEndpoints {
		w := sendCompatRequest(srv.Handler, http.MethodGet, ep, "", nil)
		if w.Code != http.StatusOK {
			t.Errorf("public GET %s: expected 200 OK, got %d (body=%q)", ep, w.Code, strings.TrimSpace(w.Body.String()))
		}
	}

	// 4. DLNA description.xml defaults to disabled (403 even on LAN), and once enabled is restricted to LAN (403 from public IP, 200 from LAN)
	dlnaDescPath := "/Dlna/" + cfg.ServerID + "/description.xml"
	lanReq := httptest.NewRequest(http.MethodGet, dlnaDescPath, nil)
	lanReq.RemoteAddr = "127.0.0.1:12345"
	lanW := httptest.NewRecorder()
	srv.Handler.ServeHTTP(lanW, lanReq)
	if lanW.Code != http.StatusForbidden {
		t.Errorf("LAN GET %s when DLNA disabled by default: expected 403 Forbidden, got %d", dlnaDescPath, lanW.Code)
	}
	cfg.Update(func(c *config.Config) { c.EnableDLNA = true })
	if w := sendCompatRequest(srv.Handler, http.MethodGet, dlnaDescPath, "", nil); w.Code != http.StatusForbidden {
		t.Errorf("public IP GET %s: expected 403 Forbidden, got %d", dlnaDescPath, w.Code)
	}
	lanW = httptest.NewRecorder()
	srv.Handler.ServeHTTP(lanW, lanReq)
	if lanW.Code != http.StatusOK {
		t.Errorf("LAN GET %s: expected 200 OK, got %d", dlnaDescPath, lanW.Code)
	}
}

// TestCoreAdminAndMediaWorkflowEndToEnd verifies the full end-to-end administrative
// and media lifecycle against a live server.New instance.
func TestCoreAdminAndMediaWorkflowEndToEnd(t *testing.T) {
	srv, _, adminToken, adminUserID := newCompatTestServer(t)

	// Prepare local media directory with 2 sample movies for library scan, collection & playlist ordering
	mediaDir := filepath.Join(t.TempDir(), "movies")
	for _, m := range []struct {
		dir  string
		file string
	}{
		{"Alpha Movie (2022)", "Alpha Movie (2022).mkv"},
		{"Beta Movie (2023)", "Beta Movie (2023).mkv"},
	} {
		dir := filepath.Join(mediaDir, m.dir)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, m.file), []byte("fake-mkv-payload-0123456789"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// 1. Create normal user
	w := sendCompatRequest(srv.Handler, http.MethodPost, "/emby/Users/New", adminToken, []byte(`{"Name":"workflow_user","Password":"init-password","IsAdministrator":false}`))
	if w.Code != http.StatusOK {
		t.Fatalf("create user: expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	var createdUser dto.UserDto
	if err := json.NewDecoder(w.Body).Decode(&createdUser); err != nil || createdUser.Id == "" {
		t.Fatalf("decode created user: err=%v user=%+v", err, createdUser)
	}
	normalUserID := createdUser.Id

	// 2. Change user password and verify login with new password
	w = sendCompatRequest(srv.Handler, http.MethodPost, "/emby/Users/"+normalUserID+"/Password", adminToken, []byte(`{"CurrentPw":"init-password","NewPw":"new-secret-pw"}`))
	if w.Code != http.StatusNoContent {
		t.Fatalf("change password: expected 204, got %d body=%s", w.Code, w.Body.String())
	}
	normalToken, _ := loginUser(t, srv.Handler, "workflow_user", "new-secret-pw", "workflow-user-dev")

	// 3. Update UserPolicy and verify persisted change
	maxRating := 13
	policyPayload, _ := json.Marshal(dto.UserPolicy{
		IsAdministrator:       false,
		EnableAllFolders:      true,
		EnableContentDeletion: false,
		EnableRemoteAccess:    true,
		MaxParentalRating:     &maxRating,
	})
	w = sendCompatRequest(srv.Handler, http.MethodPost, "/emby/Users/"+normalUserID+"/Policy", adminToken, policyPayload)
	if w.Code != http.StatusNoContent {
		t.Fatalf("update user policy: expected 204, got %d body=%s", w.Code, w.Body.String())
	}
	w = sendCompatRequest(srv.Handler, http.MethodGet, "/emby/Users/"+normalUserID, normalToken, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("get user after policy update: expected 200, got %d", w.Code)
	}
	var updatedUser dto.UserDto
	if err := json.NewDecoder(w.Body).Decode(&updatedUser); err != nil {
		t.Fatalf("decode updated user: %v", err)
	}
	if updatedUser.Policy.MaxParentalRating == nil || *updatedUser.Policy.MaxParentalRating != 13 {
		t.Fatalf("expected updated policy MaxParentalRating=13, got %+v", updatedUser.Policy)
	}

	// 4. Create API Key and invoke an authenticated endpoint using the API Key
	w = sendCompatRequest(srv.Handler, http.MethodPost, "/emby/Auth/Keys?App=WorkflowAutomation", adminToken, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("create api key: expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	var apiKeyResp struct {
		AccessToken string `json:"AccessToken"`
		AppName     string `json:"AppName"`
	}
	if err := json.NewDecoder(w.Body).Decode(&apiKeyResp); err != nil || apiKeyResp.AccessToken == "" {
		t.Fatalf("decode api key: err=%v resp=%+v", err, apiKeyResp)
	}
	apiKey := apiKeyResp.AccessToken

	w = sendCompatRequest(srv.Handler, http.MethodGet, "/emby/System/Info?api_key="+apiKey, "", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("call /emby/System/Info with API key: expected 200, got %d body=%s", w.Code, w.Body.String())
	}

	// 5. Create VirtualFolder library & scan media
	addLibBody, _ := json.Marshal(map[string]any{
		"Name":           "WorkflowMovies",
		"CollectionType": "movies",
		"Paths":          []string{mediaDir},
	})
	w = sendCompatRequest(srv.Handler, http.MethodPost, "/emby/Library/VirtualFolders?name=WorkflowMovies&collectionType=movies", apiKey, addLibBody)
	if w.Code != http.StatusOK && w.Code != http.StatusNoContent {
		t.Fatalf("create virtual folder: expected 200/204, got %d body=%s", w.Code, w.Body.String())
	}
	if _, err := srv.Scanner.ScanAll(context.Background()); err != nil {
		t.Fatalf("ScanAll: %v", err)
	}

	w = sendCompatRequest(srv.Handler, http.MethodGet, "/emby/Users/"+adminUserID+"/Items?IncludeItemTypes=Movie&SortBy=SortName", adminToken, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("query movies: expected 200, got %d", w.Code)
	}
	var moviesRes dto.QueryResult[dto.BaseItemDto]
	if err := json.NewDecoder(w.Body).Decode(&moviesRes); err != nil {
		t.Fatalf("decode movies: %v", err)
	}
	if len(moviesRes.Items) < 2 {
		t.Fatalf("expected >= 2 scanned movies, got %d", len(moviesRes.Items))
	}
	movie1ID := moviesRes.Items[0].Id
	movie2ID := moviesRes.Items[1].Id

	// 6. Create Collection & Playlist, then reorder playlist items
	colBody, _ := json.Marshal(map[string]any{
		"Name": "Sci-Fi Classics",
		"Ids":  []string{movie1ID, movie2ID},
	})
	w = sendCompatRequest(srv.Handler, http.MethodPost, "/emby/Collections", adminToken, colBody)
	if w.Code != http.StatusOK {
		t.Fatalf("create collection: expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	var colRes struct {
		Id string `json:"Id"`
	}
	if err := json.NewDecoder(w.Body).Decode(&colRes); err != nil || colRes.Id == "" {
		t.Fatalf("decode collection: err=%v res=%+v", err, colRes)
	}

	plBody, _ := json.Marshal(map[string]any{
		"Name":      "Weekend Watchlist",
		"MediaType": "Video",
		"UserId":    adminUserID,
		"Ids":       []string{movie1ID, movie2ID},
	})
	w = sendCompatRequest(srv.Handler, http.MethodPost, "/emby/Playlists", adminToken, plBody)
	if w.Code != http.StatusOK {
		t.Fatalf("create playlist: expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	var plRes struct {
		Id string `json:"Id"`
	}
	if err := json.NewDecoder(w.Body).Decode(&plRes); err != nil || plRes.Id == "" {
		t.Fatalf("decode playlist: err=%v res=%+v", err, plRes)
	}

	// Move second movie (index 1) to index 0
	w = sendCompatRequest(srv.Handler, http.MethodPost, "/emby/Playlists/"+plRes.Id+"/Items/"+movie2ID+"/Move/0", adminToken, nil)
	if w.Code != http.StatusNoContent {
		t.Fatalf("move playlist item: expected 204, got %d body=%s", w.Code, w.Body.String())
	}
	w = sendCompatRequest(srv.Handler, http.MethodGet, "/emby/Playlists/"+plRes.Id+"/Items", adminToken, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("get playlist items: expected 200, got %d", w.Code)
	}
	var plItems struct {
		Items []struct {
			Id string `json:"Id"`
		} `json:"Items"`
		TotalRecordCount int `json:"TotalRecordCount"`
	}
	if err := json.NewDecoder(w.Body).Decode(&plItems); err != nil {
		t.Fatalf("decode playlist items: %v", err)
	}
	if plItems.TotalRecordCount != 2 || len(plItems.Items) != 2 || plItems.Items[0].Id != movie2ID {
		t.Fatalf("expected reordered playlist with %s first, got %+v", movie2ID, plItems)
	}

	// 7. Create Live TV M3U TunerHost and verify automatic channel import
	m3u := "#EXTM3U\n" +
		"#EXTINF:-1 tvg-id=\"cctv1\" tvg-chno=\"1\" group-title=\"News\",CCTV-1\n" +
		"http://stream.example.com/cctv1.m3u8\n" +
		"#EXTINF:-1 tvg-id=\"cctv5\" tvg-chno=\"5\" group-title=\"Sports\",CCTV-5\n" +
		"http://stream.example.com/cctv5.m3u8\n"
	tunerBody, _ := json.Marshal(map[string]any{
		"Type":         "m3u",
		"FriendlyName": "Workflow IPTV",
		"M3UContent":   m3u,
	})
	w = sendCompatRequest(srv.Handler, http.MethodPost, "/emby/LiveTv/TunerHosts", adminToken, tunerBody)
	if w.Code != http.StatusOK {
		t.Fatalf("create tuner host: expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	var tunerResp struct {
		Id                   string `json:"Id"`
		ImportedChannelCount int    `json:"ImportedChannelCount"`
	}
	if err := json.NewDecoder(w.Body).Decode(&tunerResp); err != nil || tunerResp.ImportedChannelCount != 2 {
		t.Fatalf("expected 2 imported channels from M3U, got err=%v resp=%+v", err, tunerResp)
	}

	w = sendCompatRequest(srv.Handler, http.MethodGet, "/emby/LiveTv/Channels", adminToken, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("list livetv channels: expected 200, got %d", w.Code)
	}
	var channelsResp struct {
		TotalRecordCount int `json:"TotalRecordCount"`
	}
	if err := json.NewDecoder(w.Body).Decode(&channelsResp); err != nil || channelsResp.TotalRecordCount < 2 {
		t.Fatalf("expected >= 2 livetv channels, got err=%v resp=%+v", err, channelsResp)
	}

	// 8. Create Sync job
	syncBody, _ := json.Marshal(map[string]any{
		"TargetId": "ipad-pro",
		"Name":     "Offline Movies",
		"Quality":  "original",
		"ItemIds":  []string{movie1ID, movie2ID},
	})
	w = sendCompatRequest(srv.Handler, http.MethodPost, "/emby/Sync/Jobs", adminToken, syncBody)
	if w.Code != http.StatusOK {
		t.Fatalf("create sync job: expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	var syncResp struct {
		Job struct {
			Id        string `json:"Id"`
			ItemCount int    `json:"ItemCount"`
		} `json:"Job"`
	}
	if err := json.NewDecoder(w.Body).Decode(&syncResp); err != nil || syncResp.Job.Id == "" || syncResp.Job.ItemCount != 2 {
		t.Fatalf("expected sync job with 2 items, got err=%v resp=%+v", err, syncResp)
	}

	// 9. Send admin notification and mark as read
	notifBody, _ := json.Marshal(map[string]any{
		"Name":        "Library Ready",
		"Description": "Workflow movies imported",
		"Level":       "Normal",
		"UserId":      normalUserID,
	})
	w = sendCompatRequest(srv.Handler, http.MethodPost, "/emby/Notifications/Admin", adminToken, notifBody)
	if w.Code != http.StatusOK {
		t.Fatalf("send notification: expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	var notifResp struct {
		Id string `json:"Id"`
	}
	if err := json.NewDecoder(w.Body).Decode(&notifResp); err != nil || notifResp.Id == "" {
		t.Fatalf("decode notification: err=%v resp=%+v", err, notifResp)
	}

	w = sendCompatRequest(srv.Handler, http.MethodGet, "/emby/Notifications/"+normalUserID+"/Summary", normalToken, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("get notification summary: expected 200, got %d", w.Code)
	}
	var summaryBefore struct {
		UnreadCount int `json:"UnreadCount"`
	}
	_ = json.NewDecoder(w.Body).Decode(&summaryBefore)
	if summaryBefore.UnreadCount < 1 {
		t.Fatalf("expected at least 1 unread notification, got %d", summaryBefore.UnreadCount)
	}

	w = sendCompatRequest(srv.Handler, http.MethodPost, "/emby/Notifications/"+normalUserID+"/Read?Ids="+notifResp.Id, normalToken, nil)
	if w.Code != http.StatusNoContent {
		t.Fatalf("mark notification read: expected 204, got %d body=%s", w.Code, w.Body.String())
	}
	w = sendCompatRequest(srv.Handler, http.MethodGet, "/emby/Notifications/"+normalUserID+"/Summary", normalToken, nil)
	var summaryAfter struct {
		UnreadCount int `json:"UnreadCount"`
	}
	_ = json.NewDecoder(w.Body).Decode(&summaryAfter)
	if summaryAfter.UnreadCount != summaryBefore.UnreadCount-1 {
		t.Fatalf("expected unread notifications to decrement from %d to %d after marking read, got %d", summaryBefore.UnreadCount, summaryBefore.UnreadCount-1, summaryAfter.UnreadCount)
	}

	// 10. Create Webhook: loopback SSRF target blocked (400), public target allowed (200) with masked Secret
	hookReceiver := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		rw.WriteHeader(http.StatusOK)
	}))
	defer hookReceiver.Close()

	loopbackBody, _ := json.Marshal(map[string]any{
		"Name":    "Blocked Loopback Hook",
		"Url":     hookReceiver.URL,
		"Events":  []string{"*"},
		"Secret":  "top-secret",
		"Enabled": true,
	})
	w = sendCompatRequest(srv.Handler, http.MethodPost, "/emby/Webhooks", adminToken, loopbackBody)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("loopback webhook expected 400, got %d body=%s", w.Code, w.Body.String())
	}

	whBody, _ := json.Marshal(map[string]any{
		"Name":    "Workflow Hook",
		"Url":     "https://93.184.216.34/webhook",
		"Events":  []string{"*"},
		"Secret":  "top-secret",
		"Enabled": true,
	})
	w = sendCompatRequest(srv.Handler, http.MethodPost, "/emby/Webhooks", adminToken, whBody)
	if w.Code != http.StatusOK {
		t.Fatalf("create webhook: expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	var whResp struct {
		Id        string `json:"Id"`
		Secret    string `json:"Secret"`
		HasSecret bool   `json:"HasSecret"`
	}
	if err := json.NewDecoder(w.Body).Decode(&whResp); err != nil || whResp.Id == "" || whResp.Secret != "" || !whResp.HasSecret {
		t.Fatalf("decode webhook expected masked secret & HasSecret=true: err=%v resp=%+v", err, whResp)
	}

	// 11. Create SQLite hot backup via VACUUM INTO
	w = sendCompatRequest(srv.Handler, http.MethodPost, "/emby/BackupRestore/Backup", adminToken, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("create sqlite backup: expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	var backupResp struct {
		Name string `json:"Name"`
		Path string `json:"Path"`
		Size int64  `json:"Size"`
	}
	if err := json.NewDecoder(w.Body).Decode(&backupResp); err != nil || backupResp.Size <= 0 {
		t.Fatalf("expected non-empty backup file, got err=%v resp=%+v", err, backupResp)
	}
	if _, err := os.Stat(backupResp.Path); err != nil {
		t.Fatalf("backup file does not exist at %s: %v", backupResp.Path, err)
	}

	// 12. Revoke API Key & delete user, then verify both are invalidated
	w = sendCompatRequest(srv.Handler, http.MethodDelete, "/emby/Auth/Keys/"+apiKey, adminToken, nil)
	if w.Code != http.StatusNoContent {
		t.Fatalf("revoke api key: expected 204, got %d body=%s", w.Code, w.Body.String())
	}
	w = sendCompatRequest(srv.Handler, http.MethodGet, "/emby/System/Info?api_key="+apiKey, "", nil)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected revoked API key to return 401, got %d", w.Code)
	}

	w = sendCompatRequest(srv.Handler, http.MethodDelete, "/emby/Users/"+normalUserID, adminToken, nil)
	if w.Code != http.StatusNoContent {
		t.Fatalf("delete user: expected 204, got %d body=%s", w.Code, w.Body.String())
	}
	w = sendCompatRequest(srv.Handler, http.MethodGet, "/emby/Users/"+normalUserID, adminToken, nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected deleted user lookup to return 404, got %d", w.Code)
	}
}
