package compatcore

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ambra-media/ambra/internal/api/dto"
	"github.com/ambra-media/ambra/internal/api/middleware"
	"github.com/ambra-media/ambra/internal/core/auth"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestV0109Audit_ExtrasParentalRemoteAccessKVBoundsSessionAddAndDeleteKV(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()

	authSvc := auth.NewService(env.userRepo, env.cfg)
	env.handler.SetAuthService(authSvc)

	kid, err := authSvc.CreateUser(ctx, "kidv0109", "kidpass", false)
	if err != nil {
		t.Fatal(err)
	}
	maxPG := 5
	kidPol := sqlite.DefaultUserPolicy(false)
	kidPol.MaxParentalRating = &maxPG
	kidPol.BlockedTags = []string{"spoiler"}
	if err := env.userRepo.UpdateUserPolicy(ctx, kid.ID, kidPol); err != nil {
		t.Fatal(err)
	}
	kid.Policy = kidPol

	lanOnly, err := authSvc.CreateUser(ctx, "lanv0109", "lanpass", false)
	if err != nil {
		t.Fatal(err)
	}
	lanPol := sqlite.DefaultUserPolicy(false)
	lanPol.EnableRemoteAccess = false
	if err := env.userRepo.UpdateUserPolicy(ctx, lanOnly.ID, lanPol); err != nil {
		t.Fatal(err)
	}
	lanOnly.Policy = lanPol

	adminAC := &middleware.AuthContext{User: env.admin, Token: "admin-tok-v0109"}
	kidAC := &middleware.AuthContext{User: kid, Token: "kid-tok-v0109"}

	callAs := func(ac *middleware.AuthContext, method, path, body, remoteAddr string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
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

	// --- 1. [H-09] serveUserItemExtras filters extras by MaxParentalRating and BlockedTags ---
	lib, err := env.libRepo.CreateLibrary(ctx, "ExtrasLib", "movies", []string{t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	movieID := guid.New()
	trSafeID := guid.New()
	trRatedID := guid.New()
	trTaggedID := guid.New()
	sfSafeID := guid.New()
	sfRatedID := guid.New()
	for _, rec := range []*sqlite.ItemRecord{
		{ID: movieID, LibraryID: lib.ID, ParentID: lib.ID, Type: "Movie", MediaType: "Video", Name: "Family Movie", OfficialRating: "G"},
		{ID: trSafeID, LibraryID: lib.ID, ParentID: movieID, Type: "Video", MediaType: "Video", Name: "Safe Trailer", OfficialRating: "G", Extra: sqlite.ItemExtra{ExtraType: "Trailer"}},
		{ID: trRatedID, LibraryID: lib.ID, ParentID: movieID, Type: "Video", MediaType: "Video", Name: "Red Band Trailer", OfficialRating: "R", Extra: sqlite.ItemExtra{ExtraType: "Trailer"}},
		{ID: trTaggedID, LibraryID: lib.ID, ParentID: movieID, Type: "Video", MediaType: "Video", Name: "Spoiler Trailer", OfficialRating: "G", Extra: sqlite.ItemExtra{ExtraType: "Trailer"}},
		{ID: sfSafeID, LibraryID: lib.ID, ParentID: movieID, Type: "Video", MediaType: "Video", Name: "Safe Clip", OfficialRating: "G", Extra: sqlite.ItemExtra{ExtraType: "Featurette"}},
		{ID: sfRatedID, LibraryID: lib.ID, ParentID: movieID, Type: "Video", MediaType: "Video", Name: "Adult BTS", OfficialRating: "NC-17", Extra: sqlite.ItemExtra{ExtraType: "BehindTheScenes"}},
	} {
		if err := env.libRepo.UpsertItem(ctx, rec); err != nil {
			t.Fatal(err)
		}
	}
	_ = env.libRepo.ReplaceItemTaxonomies(ctx, trTaggedID, "tag", []string{"spoiler"})

	recTr := callAs(kidAC, http.MethodGet, "/Users/"+kid.ID+"/Items/"+movieID+"/LocalTrailers", "", "")
	if recTr.Code != http.StatusOK {
		t.Fatalf("LocalTrailers status=%d body=%s", recTr.Code, recTr.Body.String())
	}
	var trItems []dto.BaseItemDto
	if err := json.Unmarshal(recTr.Body.Bytes(), &trItems); err != nil {
		t.Fatal(err)
	}
	if len(trItems) != 1 || trItems[0].Id != trSafeID {
		t.Fatalf("expected only safe trailer for kid, got %+v", trItems)
	}

	recSF := callAs(kidAC, http.MethodGet, "/Users/"+kid.ID+"/Items/"+movieID+"/SpecialFeatures", "", "")
	if recSF.Code != http.StatusOK {
		t.Fatalf("SpecialFeatures status=%d body=%s", recSF.Code, recSF.Body.String())
	}
	var sfItems []dto.BaseItemDto
	if err := json.Unmarshal(recSF.Body.Bytes(), &sfItems); err != nil {
		t.Fatal(err)
	}
	if len(sfItems) != 1 || sfItems[0].Id != sfSafeID {
		t.Fatalf("expected only safe special feature for kid, got %+v", sfItems)
	}

	// --- 2. [M-11] & [M-14] POST /Users/{Id}/Authenticate remote access + Cache-Control ---
	// Wrong password from public IP -> 401
	recAuthWrong := callAs(nil, http.MethodPost, "/Users/"+lanOnly.ID+"/Authenticate", `{"Pw":"wrong"}`, "203.0.113.5:9999")
	if recAuthWrong.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for wrong pw on remote-disabled user, got %d", recAuthWrong.Code)
	}
	// Correct password from public IP for per-user remote-disabled user -> uniform 401 + no token minted + Cache-Control: private, no-store (M-02)
	recAuthRemote := callAs(nil, http.MethodPost, "/Users/"+lanOnly.ID+"/Authenticate", `{"Pw":"lanpass"}`, "203.0.113.5:9999")
	if recAuthRemote.Code != http.StatusUnauthorized {
		t.Fatalf("expected uniform 401 for remote-disabled user from public IP, got %d body=%s", recAuthRemote.Code, recAuthRemote.Body.String())
	}
	if cc := recAuthRemote.Header().Get("Cache-Control"); cc != "private, no-store" {
		t.Fatalf("expected Cache-Control=private, no-store on authenticate, got %q", cc)
	}
	var tokCnt int
	_ = env.store.Reader.QueryRowContext(ctx, `SELECT COUNT(*) FROM access_tokens WHERE user_id = ?`, lanOnly.ID).Scan(&tokCnt)
	if tokCnt != 0 {
		t.Fatalf("expected 0 tokens minted for rejected remote authenticate, got %d", tokCnt)
	}
	// Correct password from LAN IP -> 200 + Cache-Control: private, no-store
	recAuthLAN := callAs(nil, http.MethodPost, "/Users/"+lanOnly.ID+"/Authenticate", `{"Pw":"lanpass"}`, "127.0.0.1:9999")
	if recAuthLAN.Code != http.StatusOK {
		t.Fatalf("expected 200 for remote-disabled user from LAN IP, got %d body=%s", recAuthLAN.Code, recAuthLAN.Body.String())
	}
	if cc := recAuthLAN.Header().Get("Cache-Control"); cc != "private, no-store" {
		t.Fatalf("expected Cache-Control=private, no-store on LAN authenticate, got %q", cc)
	}

	// Fallback path (authSvc == nil) also enforces uniform 401 for per-user EnableRemoteAccess=false
	hNoSvc := New(env.cfg, env.store, env.userRepo, env.libRepo, env.sessions, env.registry)
	rNoSvc := hNoSvc.BuildRouter(AuthFuncs{})
	reqNoSvc := httptest.NewRequest(http.MethodPost, "/Users/"+lanOnly.ID+"/Authenticate", bytes.NewBufferString(`{"Pw":"lanpass"}`))
	reqNoSvc.RemoteAddr = "203.0.113.6:1234"
	recNoSvc := httptest.NewRecorder()
	rNoSvc.ServeHTTP(recNoSvc, reqNoSvc)
	if recNoSvc.Code != http.StatusUnauthorized {
		t.Fatalf("expected uniform 401 in fallback authSvc==nil path for remote-disabled user, got %d", recNoSvc.Code)
	}

	// [M-14] GET /Connect/Exchange sets Cache-Control: private, no-store
	_ = env.handler.saveKV(ctx, "user_connect_link:"+env.admin.ID, map[string]any{"UserId": env.admin.ID, "ConnectUsername": "embyconn"})
	recCE := callAs(adminAC, http.MethodGet, "/Connect/Exchange?ConnectUserId=embyconn", "", "")
	if recCE.Code != http.StatusOK || recCE.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatalf("Connect/Exchange expected 200 private, no-store, got %d cc=%q", recCE.Code, recCE.Header().Get("Cache-Control"))
	}

	// --- 3. [M-13] KV body size & key bounds ---
	oversized := `{"pad":"` + strings.Repeat("a", maxKVJSONBodyBytes+100) + `"}`
	for _, ep := range []string{
		"/Users/" + kid.ID + "/TypedSettings/k1",
		"/UserSettings/" + kid.ID,
		"/UserSettings/" + kid.ID + "/Partial",
		"/Devices/Options?Id=dev1",
	} {
		recBig := callAs(adminAC, http.MethodPost, ep, oversized, "")
		if recBig.Code != http.StatusRequestEntityTooLarge && recBig.Code != http.StatusBadRequest {
			t.Fatalf("POST %s with oversized body expected 413/400, got %d", ep, recBig.Code)
		}
	}

	// postUserSettingsPartial cumulative size > maxKVJSONBodyBytes -> 413 without overwriting
	halfVal := strings.Repeat("x", 40*1024)
	if r1 := callAs(kidAC, http.MethodPost, "/UserSettings/"+kid.ID, `{"k1":"`+halfVal+`"}`, ""); r1.Code != http.StatusNoContent {
		t.Fatalf("seed user settings got %d", r1.Code)
	}
	if r2 := callAs(kidAC, http.MethodPost, "/UserSettings/"+kid.ID+"/Partial", `{"k2":"`+halfVal+`"}`, ""); r2.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413 when cumulative partial settings exceed 64KiB, got %d", r2.Code)
	}

	// postUserTypedSettings key length > 128 -> 400
	longKey := strings.Repeat("k", maxTypedSettingKeyLen+1)
	if rLong := callAs(kidAC, http.MethodPost, "/Users/"+kid.ID+"/TypedSettings/"+longKey, `{"a":1}`, ""); rLong.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for >128 char typed setting key, got %d", rLong.Code)
	}
	// postUserTypedSettings key count > 100 -> 400, while updating existing key still returns 204
	for i := 0; i < maxUserTypedSettingsKeys; i++ {
		if r := callAs(kidAC, http.MethodPost, fmt.Sprintf("/Users/%s/TypedSettings/key-%d", kid.ID, i), `{"v":1}`, ""); r.Code != http.StatusNoContent {
			t.Fatalf("typed setting key-%d expected 204, got %d", i, r.Code)
		}
	}
	// Updating an existing key still works
	if rUpd := callAs(kidAC, http.MethodPost, "/Users/"+kid.ID+"/TypedSettings/key-0", `{"v":2}`, ""); rUpd.Code != http.StatusNoContent {
		t.Fatalf("updating existing typed setting key expected 204, got %d", rUpd.Code)
	}
	// 101st distinct key rejected
	if rOver := callAs(kidAC, http.MethodPost, "/Users/"+kid.ID+"/TypedSettings/key-overflow", `{"v":1}`, ""); rOver.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for 101st distinct typed setting key, got %d", rOver.Code)
	}

	// --- 4. [L-07] postSessionAddUser rejects nonexistent session with 404 ---
	missingSID := "nonexistent-session-v0109"
	if rMissing := callAs(adminAC, http.MethodPost, "/Sessions/"+missingSID+"/Users/"+kid.ID, "", ""); rMissing.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for nonexistent session in postSessionAddUser, got %d", rMissing.Code)
	}

	// --- 5. [L-09] deleteKV falls back to h.userRepo.Store() when h.store == nil ---
	hNilStore := New(env.cfg, nil, env.userRepo, env.libRepo, env.sessions, env.registry)
	if err := hNilStore.saveKV(ctx, "test_delete_fallback", map[string]any{"x": 1}); err != nil {
		t.Fatal(err)
	}
	if err := hNilStore.deleteKV(ctx, "test_delete_fallback"); err != nil {
		t.Fatal(err)
	}
	var chk map[string]any
	if found, err := hNilStore.loadKV(ctx, "test_delete_fallback", &chk); err != nil || found {
		t.Fatalf("expected key deleted via userRepo.Store() fallback, found=%v err=%v", found, err)
	}
}
