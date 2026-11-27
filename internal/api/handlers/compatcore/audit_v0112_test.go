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
	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/core/auth"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestH02H04H05M01M02And31_CompatCoreHardeningAndAlignment(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()

	authSvc := auth.NewService(env.userRepo, env.cfg)
	authMW := middleware.NewAuth(authSvc, env.sessions)
	authMW.SetConfig(env.cfg)
	env.handler.SetAuthService(authSvc)
	env.sessions.SetLibraryRepo(env.libRepo)

	// Create User A ("alice") and User B ("bob") plus a disabled user ("disabled_u")
	alice, err := authSvc.CreateUser(ctx, "alice", "alicepw", false)
	if err != nil {
		t.Fatal(err)
	}
	bob, err := authSvc.CreateUser(ctx, "bob", "bobpw", false)
	if err != nil {
		t.Fatal(err)
	}
	disabledU, err := authSvc.CreateUser(ctx, "disabled_u", "dispw", false)
	if err != nil {
		t.Fatal(err)
	}
	disPol := sqlite.DefaultUserPolicy(false)
	disPol.IsDisabled = true
	_ = env.userRepo.UpdateUserPolicy(ctx, disabledU.ID, disPol)

	// Configure Alice with EnableAllFolders=true, EnableLiveTvAccess=false, BlockUnratedItems=["Movie","Episode"], AllowedTags=["alice-ok"], EnableRemoteAccess=false
	alicePol := sqlite.DefaultUserPolicy(false)
	alicePol.EnableAllFolders = true
	alicePol.EnableLiveTvAccess = false
	alicePol.BlockUnratedItems = []string{"Movie", "Episode"}
	alicePol.AllowedTags = []string{"alice-ok"}
	alicePol.EnableRemoteAccess = false
	if err := env.userRepo.UpdateUserPolicy(ctx, alice.ID, alicePol); err != nil {
		t.Fatal(err)
	}
	authSvc.InvalidateUserCache(alice.ID)

	adminAuth, err := authSvc.AuthenticateByName(ctx, "admin", "admin", "", auth.ClientInfo{DeviceID: "d-adm"})
	if err != nil {
		t.Fatal(err)
	}
	aliceAuth, err := authSvc.AuthenticateByName(ctx, "alice", "alicepw", "", auth.ClientInfo{DeviceID: "d-alice"})
	if err != nil {
		t.Fatal(err)
	}
	aliceSess := env.sessions.TouchSession(alice, auth.ClientInfo{DeviceID: "d-alice"})

	// Wire 3.1-2 packageCanceller and pluginUninstaller
	cancelledPkg := ""
	env.handler.SetPackageCanceller(func(id string) bool {
		if id == "pkg-active-1" {
			cancelledPkg = id
			return true
		}
		return false
	})
	uninstalledPlugin := ""
	env.handler.SetPluginUninstaller(func(_ context.Context, id string) error {
		if id == "missing-plug" {
			return sqlite.ErrNotFound
		}
		uninstalledPlugin = id
		return nil
	})

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

	call := func(token, method, target, body, remoteAddr string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, target, strings.NewReader(body))
		if remoteAddr == "" {
			remoteAddr = "127.0.0.1:41000"
		}
		req.RemoteAddr = remoteAddr
		if token != "" {
			req.Header.Set("X-Emby-Token", token)
		}
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}

	// --- 1. H-02: Session AdditionalUsers Cross-User IDOR & Enumeration Prevention ---
	// Alice CANNOT add Bob, Admin, DisabledUser, or NonExistentUser to Alice's own session -> all return uniform 403!
	for _, targetUID := range []string{bob.ID, env.admin.ID, disabledU.ID, guid.New()} {
		if rec := call(aliceAuth.AccessToken, http.MethodPost, "/Sessions/"+aliceSess.Id+"/Users/"+targetUID, "", ""); rec.Code != http.StatusForbidden {
			t.Fatalf("H-02 violated: Alice adding %s expected 403, got %d body=%s", targetUID, rec.Code, rec.Body.String())
		}
		if rec := call(aliceAuth.AccessToken, http.MethodDelete, "/Sessions/"+aliceSess.Id+"/Users/"+targetUID, "", ""); rec.Code != http.StatusForbidden {
			t.Fatalf("H-02 violated: Alice removing %s expected 403, got %d body=%s", targetUID, rec.Code, rec.Body.String())
		}
	}
	// Alice CAN add/remove self (204), and Admin CAN add/remove Bob to Alice's session (204)!
	if rec := call(aliceAuth.AccessToken, http.MethodPost, "/Sessions/"+aliceSess.Id+"/Users/"+alice.ID, "", ""); rec.Code != http.StatusNoContent {
		t.Fatalf("expected Alice self-add 204, got %d", rec.Code)
	}
	if rec := call(aliceAuth.AccessToken, http.MethodDelete, "/Sessions/"+aliceSess.Id+"/Users/"+alice.ID, "", ""); rec.Code != http.StatusNoContent {
		t.Fatalf("expected Alice self-remove 204, got %d", rec.Code)
	}
	if rec := call(adminAuth.AccessToken, http.MethodPost, "/Sessions/"+aliceSess.Id+"/Users/"+bob.ID, "", ""); rec.Code != http.StatusNoContent {
		t.Fatalf("expected Admin add Bob 204, got %d", rec.Code)
	}
	if s, _ := env.sessions.GetSession(aliceSess.Id); len(s.AdditionalUsers) != 1 || guid.Normalize(s.AdditionalUsers[0].UserId) != guid.Normalize(bob.ID) {
		t.Fatalf("expected Bob in AdditionalUsers, got %+v", s.AdditionalUsers)
	}
	// Even after Admin attached Bob, Alice still CANNOT remove Bob (403); Admin CAN remove Bob (204)!
	if rec := call(aliceAuth.AccessToken, http.MethodDelete, "/Sessions/"+aliceSess.Id+"/Users/"+bob.ID, "", ""); rec.Code != http.StatusForbidden {
		t.Fatalf("expected Alice removing Bob 403, got %d", rec.Code)
	}
	if rec := call(adminAuth.AccessToken, http.MethodDelete, "/Sessions/"+aliceSess.Id+"/Users/"+bob.ID, "", ""); rec.Code != http.StatusNoContent {
		t.Fatalf("expected Admin removing Bob 204, got %d", rec.Code)
	}

	// --- 2. H-04 & H-05: LiveTV DVR Library Gate + BlockUnratedItems & AllowedTags ---
	liveLib, err := env.libRepo.CreateLibrary(ctx, "DVR Recordings", "livetv", []string{"/dvr"})
	if err != nil {
		t.Fatal(err)
	}
	movLib, err := env.libRepo.CreateLibrary(ctx, "Cinema", "movies", []string{"/cinema"})
	if err != nil {
		t.Fatal(err)
	}

	dvrMovie := &sqlite.ItemRecord{ID: guid.New(), LibraryID: liveLib.ID, Type: "Movie", Name: "DVR Leak", SortName: "dvr leak", Path: "/dvr/a.ts", MediaType: "Video", OfficialRating: "G"}
	unratedMov := &sqlite.ItemRecord{ID: guid.New(), LibraryID: movLib.ID, Type: "Movie", Name: "Unrated Leak", SortName: "unrated leak", Path: "/cinema/u.mkv", MediaType: "Video"}
	untaggedMov := &sqlite.ItemRecord{ID: guid.New(), LibraryID: movLib.ID, Type: "Movie", Name: "Untagged Leak", SortName: "untagged leak", Path: "/cinema/ut.mkv", MediaType: "Video", OfficialRating: "G"}
	safeMov1 := &sqlite.ItemRecord{ID: guid.New(), LibraryID: movLib.ID, Type: "Movie", Name: "Safe One", SortName: "safe one", Path: "/cinema/s1.mkv", MediaType: "Video", OfficialRating: "G"}
	safeMov2 := &sqlite.ItemRecord{ID: guid.New(), LibraryID: movLib.ID, Type: "Movie", Name: "Safe Two", SortName: "safe two", Path: "/cinema/s2.mkv", MediaType: "Video", OfficialRating: "G"}
	unratedTrailer := &sqlite.ItemRecord{ID: guid.New(), LibraryID: movLib.ID, ParentID: safeMov1.ID, Type: "Trailer", Extra: sqlite.ItemExtra{ExtraType: "Trailer"}, Name: "Unrated Trailer", SortName: "unrated trailer", Path: "/cinema/s1-trailer.mkv", MediaType: "Video"}
	safeTrailer := &sqlite.ItemRecord{ID: guid.New(), LibraryID: movLib.ID, ParentID: safeMov1.ID, Type: "Trailer", Extra: sqlite.ItemExtra{ExtraType: "Trailer"}, Name: "Safe Trailer", SortName: "safe trailer", Path: "/cinema/s1-safe-trailer.mkv", MediaType: "Video", OfficialRating: "G"}

	for _, rec := range []*sqlite.ItemRecord{dvrMovie, unratedMov, untaggedMov, safeMov1, safeMov2, unratedTrailer, safeTrailer} {
		if err := env.libRepo.UpsertItem(ctx, rec); err != nil {
			t.Fatal(err)
		}
	}
	// Give dvrMovie, unratedMov, safeMov1, safeMov2, safeTrailer the "alice-ok" tag (leaving untaggedMov & unratedTrailer missing tag!)
	for _, id := range []string{dvrMovie.ID, unratedMov.ID, safeMov1.ID, safeMov2.ID, safeTrailer.ID} {
		_ = env.libRepo.ReplaceItemTaxonomies(ctx, id, "Tag", []string{"alice-ok"})
	}
	// Seed resume progress on dvrMovie, unratedMov, untaggedMov, and safeMov1 so Resume section is tested!
	for _, id := range []string{dvrMovie.ID, unratedMov.ID, untaggedMov.ID, safeMov1.ID} {
		_, _ = env.userRepo.UpsertPlaybackProgress(ctx, alice.ID, id, 50_000_000, false, nil, nil)
	}

	// GroupingOptions excludes livetv library even though EnableAllFolders=true!
	recGroups := call(aliceAuth.AccessToken, http.MethodGet, "/Users/"+alice.ID+"/GroupingOptions", "", "")
	if strings.Contains(recGroups.Body.String(), liveLib.ID) || !strings.Contains(recGroups.Body.String(), movLib.ID) {
		t.Fatalf("H-04 violated in GroupingOptions: %s", recGroups.Body.String())
	}

	// Resume, Latest, Suggestions MUST NOT leak dvrMovie, unratedMov, or untaggedMov!
	for _, ep := range []string{
		"/Users/" + alice.ID + "/Sections/resume/Items",
		"/Users/" + alice.ID + "/Sections/latestmedia/Items",
		"/Users/" + alice.ID + "/Suggestions",
	} {
		rec := call(aliceAuth.AccessToken, http.MethodGet, ep, "", "")
		if rec.Code != http.StatusOK {
			t.Fatalf("%s status=%d body=%s", ep, rec.Code, rec.Body.String())
		}
		bodyStr := rec.Body.String()
		for _, forbidden := range []string{dvrMovie.ID, unratedMov.ID, untaggedMov.ID} {
			if strings.Contains(bodyStr, forbidden) {
				t.Fatalf("H-04/H-05 leak on %s: leaked restricted item %s in %s", ep, forbidden, bodyStr)
			}
		}
	}

	// LocalTrailers filters out extra missing AllowedTags (unratedTrailer) while returning safeTrailer;
	// and denies access to dvrMovie / unratedMov / untaggedMov with 403!
	for _, blockedID := range []string{dvrMovie.ID, unratedMov.ID, untaggedMov.ID} {
		if rec := call(aliceAuth.AccessToken, http.MethodGet, "/Users/"+alice.ID+"/Items/"+blockedID+"/LocalTrailers", "", ""); rec.Code != http.StatusForbidden {
			t.Fatalf("expected 403 on LocalTrailers for blocked item %s, got %d", blockedID, rec.Code)
		}
		if rec := call(aliceAuth.AccessToken, http.MethodPost, "/Users/"+alice.ID+"/PlayingItems/"+blockedID, "", ""); rec.Code != http.StatusForbidden {
			t.Fatalf("expected 403 on PlayingItems for blocked item %s, got %d", blockedID, rec.Code)
		}
	}
	recTrailers := call(aliceAuth.AccessToken, http.MethodGet, "/Users/"+alice.ID+"/Items/"+safeMov1.ID+"/LocalTrailers", "", "")
	if recTrailers.Code != http.StatusOK || strings.Contains(recTrailers.Body.String(), unratedTrailer.ID) || !strings.Contains(recTrailers.Body.String(), safeTrailer.ID) {
		t.Fatalf("H-05 LocalTrailers extra filtering failed: %d %s", recTrailers.Code, recTrailers.Body.String())
	}

	// --- 3. 3.1-10: Multi-item PlayQueue via POST /Sessions/{Id}/Playing and GET /Sessions/PlayQueue ---
	// Include safeMov2, unratedMov (blocked for Alice!), and safeMov1 -> PlayQueue for Alice returns [safeMov2, safeMov1] in order!
	recPlayCmd := call(aliceAuth.AccessToken, http.MethodPost,
		"/Sessions/"+aliceSess.Id+"/Playing?ItemIds="+safeMov2.ID+","+unratedMov.ID+","+safeMov1.ID+"&PlayCommand=PlayNow", "", "")
	if recPlayCmd.Code != http.StatusNoContent {
		t.Fatalf("POST /Sessions/{Id}/Playing expected 204, got %d", recPlayCmd.Code)
	}
	recPQ := call(aliceAuth.AccessToken, http.MethodGet, "/Sessions/PlayQueue?Id="+aliceSess.Id, "", "")
	var pq dto.QueryResult[dto.BaseItemDto]
	if err := json.Unmarshal(recPQ.Body.Bytes(), &pq); err != nil {
		t.Fatal(err)
	}
	if pq.TotalRecordCount != 2 || len(pq.Items) != 2 ||
		guid.Normalize(pq.Items[0].Id) != guid.Normalize(safeMov2.ID) ||
		guid.Normalize(pq.Items[1].Id) != guid.Normalize(safeMov1.ID) {
		t.Fatalf("expected ordered policy-filtered 2-item PlayQueue [safeMov2, safeMov1], got %+v", pq)
	}

	// --- 4. 3.1-2: POST /Packages/Installing/{Id}/Delete & POST /Plugins/{Id}/Uninstall ---
	if rec := call(adminAuth.AccessToken, http.MethodPost, "/Packages/Installing/pkg-active-1/Delete", "", ""); rec.Code != http.StatusNoContent || cancelledPkg != "pkg-active-1" {
		t.Fatalf("expected packageCanceller invoked with 204, got %d cancelled=%q", rec.Code, cancelledPkg)
	}
	if rec := call(adminAuth.AccessToken, http.MethodPost, "/Packages/Installing/unknown-pkg/Delete", "", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown installing package, got %d", rec.Code)
	}
	if rec := call(adminAuth.AccessToken, http.MethodPost, "/Plugins/ext-custom/Uninstall", "", ""); rec.Code != http.StatusNoContent || uninstalledPlugin != "ext-custom" {
		t.Fatalf("expected pluginUninstaller invoked with 204, got %d plugin=%q", rec.Code, uninstalledPlugin)
	}
	if rec := call(adminAuth.AccessToken, http.MethodPost, "/Plugins/missing-plug/Delete", "", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for missing plugin delete, got %d", rec.Code)
	}

	// --- 5. M-02 & M-01: Pre-Argon2id server remote access gate + loopback rate limit on /Users/{Id}/Authenticate ---
	env.cfg.Update(func(c *config.Config) {
		f := false
		c.EnableRemoteAccessGlobal = &f
	})
	if rec := call("", http.MethodPost, "/Users/"+alice.ID+"/Authenticate", `{invalid-json`, "203.0.113.88:55555"); rec.Code != http.StatusForbidden {
		t.Fatalf("M-02 expected 403 before JSON decode / Argon2id when server remote access disabled, got %d", rec.Code)
	}
	env.cfg.Update(func(c *config.Config) {
		tr := true
		c.EnableRemoteAccessGlobal = &tr
	})

	loopUser, err := authSvc.CreateUser(ctx, "loop_by_id", "", false)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < authByIDLoopbackRateLimit; i++ {
		if rec := call("", http.MethodPost, "/Users/"+loopUser.ID+"/Authenticate", `{"Pw":"wrong"}`, "127.0.0.1:49999"); rec.Code != http.StatusUnauthorized {
			t.Fatalf("loopback authByID attempt %d expected 401, got %d", i+1, rec.Code)
		}
	}
	if rec := call("", http.MethodPost, "/Users/"+loopUser.ID+"/Authenticate", `{"Pw":"wrong"}`, "127.0.0.1:49999"); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("M-01 expected 429 on loopback authByID brute-force after %d attempts, got %d", authByIDLoopbackRateLimit, rec.Code)
	}
}
