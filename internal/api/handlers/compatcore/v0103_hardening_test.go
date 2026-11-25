package compatcore

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ambra-media/ambra/internal/api/middleware"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestCompatCoreV0103Hardening(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()

	// Create a restricted library + restricted item and a normal non-admin user
	secretLib, err := env.libRepo.CreateLibrary(ctx, "Secret", "movies", []string{"/secret"})
	if err != nil {
		t.Fatal(err)
	}
	secretItem := &sqlite.ItemRecord{
		ID:             guid.New(),
		LibraryID:      secretLib.ID,
		ParentID:       secretLib.ID,
		Type:           "Movie",
		MediaType:      "Video",
		Name:           "Secret Movie",
		SortName:       "secret movie",
		OfficialRating: "R",
	}
	if err := env.libRepo.UpsertItem(ctx, secretItem); err != nil {
		t.Fatal(err)
	}
	// Restrict user2 to only env.itemID's library and max parental rating PG (5)
	allowedRec, err := env.libRepo.GetItemRecordByID(ctx, env.itemID)
	if err != nil {
		t.Fatal(err)
	}
	p := env.user2.Policy
	p.EnableAllFolders = false
	p.EnabledFolders = []string{allowedRec.LibraryID}
	maxPG := 5
	p.MaxParentalRating = &maxPG
	if err := env.userRepo.UpdateUserPolicy(ctx, env.user2.ID, p); err != nil {
		t.Fatal(err)
	}
	user2Fresh, err := env.userRepo.GetUserByID(ctx, env.user2.ID)
	if err != nil {
		t.Fatal(err)
	}
	user2AC := &middleware.AuthContext{User: user2Fresh}
	adminAC := &middleware.AuthContext{User: env.admin}

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

	// 1. getUserSuggestions extreme StartIndex/Limit must not OOM or panic
	rec := callAs(adminAC, http.MethodGet, "/Users/"+env.admin.ID+"/Suggestions?StartIndex=99999999&Limit=99999999", "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("Suggestions with extreme bounds got %d: %s", rec.Code, rec.Body.String())
	}

	// 2. postUserItemUserData with Played=true AND PlaybackPositionTicks must only increment play_count once
	rec = callAs(adminAC, http.MethodPost, "/Users/"+env.admin.ID+"/Items/"+env.itemID+"/UserData",
		`{"Played":true,"PlaybackPositionTicks":5000000}`, "")
	if rec.Code != http.StatusOK && rec.Code != http.StatusNoContent {
		t.Fatalf("postUserItemUserData got %d: %s", rec.Code, rec.Body.String())
	}
	itDTO, err := env.libRepo.GetItemByID(ctx, env.cfg.ServerID, env.admin.ID, env.itemID)
	if err != nil || itDTO == nil || itDTO.UserData == nil {
		t.Fatalf("GetItemByID after postUserItemUserData failed: %+v err=%v", itDTO, err)
	}
	if !itDTO.UserData.Played || itDTO.UserData.PlayCount != 1 {
		t.Fatalf("expected Played=true PlayCount=1 (no double increment), got Played=%v PlayCount=%d", itDTO.UserData.Played, itDTO.UserData.PlayCount)
	}

	// 3. Item RBAC + parental checks on user-item mutation & playback routes
	for _, tc := range []struct{ method, path, body string }{
		{http.MethodPost, "/Users/" + user2Fresh.ID + "/FavoriteItems/" + secretItem.ID + "/Delete", ""},
		{http.MethodPost, "/Users/" + user2Fresh.ID + "/Items/" + secretItem.ID + "/Rating?Likes=true", ""},
		{http.MethodDelete, "/Users/" + user2Fresh.ID + "/Items/" + secretItem.ID + "/Rating", ""},
		{http.MethodPost, "/Users/" + user2Fresh.ID + "/Items/" + secretItem.ID + "/HideFromResume?Hide=true", ""},
		{http.MethodPost, "/Users/" + user2Fresh.ID + "/Items/" + secretItem.ID + "/UserData", `{"IsFavorite":true}`},
		{http.MethodPost, "/Users/" + user2Fresh.ID + "/PlayedItems/" + secretItem.ID + "/Delete", ""},
		{http.MethodPost, "/Users/" + user2Fresh.ID + "/PlayingItems/" + secretItem.ID, ""},
		{http.MethodPost, "/Users/" + user2Fresh.ID + "/PlayingItems/" + secretItem.ID + "/Progress", ""},
		{http.MethodDelete, "/Users/" + user2Fresh.ID + "/PlayingItems/" + secretItem.ID, ""},
		{http.MethodGet, "/Users/" + user2Fresh.ID + "/Items/" + secretItem.ID + "/LocalTrailers", ""},
		{http.MethodGet, "/Users/" + user2Fresh.ID + "/Items/" + secretItem.ID + "/SpecialFeatures", ""},
	} {
		rec = callAs(user2AC, tc.method, tc.path, tc.body, "")
		if rec.Code != http.StatusForbidden {
			t.Fatalf("expected 403 for restricted user %s %s, got %d (%s)", tc.method, tc.path, rec.Code, rec.Body.String())
		}
	}

	// 4. POST /Users/{Id}/Images/{Type}[/{Index}] only allows Primary index 0
	rec = callAs(adminAC, http.MethodPost, "/Users/"+env.admin.ID+"/Images/Backdrop", "abc", "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for non-Primary user image type, got %d", rec.Code)
	}
	rec = callAs(adminAC, http.MethodPost, "/Users/"+env.admin.ID+"/Images/Primary/2", "abc", "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for non-zero user image index, got %d", rec.Code)
	}

	// 5. Non-admin cannot rename username via POST /Users/{Id}
	rec = callAs(user2AC, http.MethodPost, "/Users/"+user2Fresh.ID, `{"Name":"hackedname"}`, "")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 when non-admin tries to rename username, got %d", rec.Code)
	}
	// Admin cannot set overly long username (>64 runes)
	rec = callAs(adminAC, http.MethodPost, "/Users/"+user2Fresh.ID, `{"Name":"`+strings.Repeat("a", 65)+`"}`, "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for username > 64 chars, got %d", rec.Code)
	}

	// 6. GET /System/Endpoint uses netutil.ClientIP
	rec = callAs(adminAC, http.MethodGet, "/System/Endpoint", "", "203.0.113.55:4444")
	if rec.Code != http.StatusOK {
		t.Fatalf("System/Endpoint got %d", rec.Code)
	}
	var ep struct {
		IsLocal     bool `json:"IsLocal"`
		IsInNetwork bool `json:"IsInNetwork"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &ep)
	if ep.IsLocal || ep.IsInNetwork {
		t.Fatalf("expected public IP to report IsLocal=false IsInNetwork=false, got %+v", ep)
	}

	// 7. GET /System/WakeOnLanInfo rejects non-admin
	rec = callAs(user2AC, http.MethodGet, "/System/WakeOnLanInfo", "", "")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for non-admin WakeOnLanInfo, got %d", rec.Code)
	}

	// 8. POST /Parties/Messages enforces 64KB body cap
	bigPayload := `{"Message":"` + string(bytes.Repeat([]byte("x"), 70<<10)) + `"}`
	rec = callAs(adminAC, http.MethodPost, "/Parties/Messages", bigPayload, "")
	if rec.Code != http.StatusBadRequest && rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 400/413 for >64KB party message, got %d", rec.Code)
	}
}
