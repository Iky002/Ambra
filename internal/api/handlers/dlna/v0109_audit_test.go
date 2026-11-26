package dlna

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestDLNAV0109AuditM21M22M28(t *testing.T) {
	h, mux, libRepo, store := setupDLNATestEnv(t)
	ctx := context.Background()

	// 1. M-21: handleEventSubscribe CALLBACK validation (format & length <= 1024) and per-IP cap (16)
	badCallbacks := []string{
		"",
		"http://192.168.1.50/cb",                       // missing angle brackets
		"<ftp://192.168.1.50/cb>",                      // invalid scheme
		"<http://>",                                    // missing host
		"<http://192.168.1.50/" + strings.Repeat("a", maxDLNACallbackLen) + ">", // too long
	}
	for _, cb := range badCallbacks {
		req := httptest.NewRequest("SUBSCRIBE", "/Dlna/srv-1/contentdirectory/events", nil)
		req.RemoteAddr = "192.168.1.20:5000"
		if cb != "" {
			req.Header.Set("CALLBACK", cb)
		}
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusPreconditionFailed {
			t.Fatalf("M-21: expected 412 for invalid CALLBACK %q, got %d", cb, rec.Code)
		}
	}

	// Valid per-IP subscriptions up to maxDLNASubscriptionsPerIP succeed, next from same IP gets 429
	for i := 0; i < maxDLNASubscriptionsPerIP; i++ {
		req := httptest.NewRequest("SUBSCRIBE", "/Dlna/srv-1/contentdirectory/events", nil)
		req.RemoteAddr = "192.168.1.30:5000"
		req.Header.Set("CALLBACK", fmt.Sprintf("<http://192.168.1.30:8080/cb/%d>", i))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("M-21: subscription #%d expected 200, got %d", i, rec.Code)
		}
	}
	reqOver := httptest.NewRequest("SUBSCRIBE", "/Dlna/srv-1/contentdirectory/events", nil)
	reqOver.RemoteAddr = "192.168.1.30:5001"
	reqOver.Header.Set("CALLBACK", "<http://192.168.1.30:8080/cb/over>")
	recOver := httptest.NewRecorder()
	mux.ServeHTTP(recOver, reqOver)
	if recOver.Code != http.StatusTooManyRequests {
		t.Fatalf("M-21: expected 429 on 17th subscription from same IP, got %d", recOver.Code)
	}

	// 2. M-21: Batch privatePlaylistIDs query returns private playlists and excludes public ones
	libDir := t.TempDir()
	outsideDir := t.TempDir()
	libAllowed, _ := libRepo.CreateLibrary(ctx, "Kids", "movies", []string{libDir})
	libRestricted, _ := libRepo.CreateLibrary(ctx, "Adults", "movies", []string{t.TempDir()})

	privPl, _ := libRepo.CreatePlaylist(ctx, "Priv", "Video", "u1", nil)
	pubPl, _ := libRepo.CreatePlaylist(ctx, "Pub", "Video", "u1", nil)
	_, _ = store.Writer.ExecContext(ctx,
		`INSERT INTO server_config (key, value_json, updated_at) VALUES (?, ?, ?)`,
		"item_visibility_"+guid.Normalize(pubPl.ID), `{"IsPublic":true}`, time.Now().UTC().Unix(),
	)
	privIDs := h.privatePlaylistIDs(ctx, "srv-1")
	foundPriv, foundPub := false, false
	for _, id := range privIDs {
		if guid.Normalize(id) == guid.Normalize(privPl.ID) {
			foundPriv = true
		}
		if guid.Normalize(id) == guid.Normalize(pubPl.ID) {
			foundPub = true
		}
	}
	if !foundPriv || foundPub {
		t.Fatalf("M-21: privatePlaylistIDs expected only privPl, got %v", privIDs)
	}

	// 3. M-22: handleDLNAStream checks !IsStrm, !IsDir, and EvalSymlinks within registered libraries
	okFile := filepath.Join(libDir, "ok.mp4")
	_ = os.WriteFile(okFile, []byte("MP4OK"), 0o644)
	outsideFile := filepath.Join(outsideDir, "secret.mp4")
	_ = os.WriteFile(outsideFile, []byte("SECRET"), 0o644)
	escapeSym := filepath.Join(libDir, "escape.mp4")
	_ = os.Symlink(outsideFile, escapeSym)
	subDir := filepath.Join(libDir, "adir")
	_ = os.MkdirAll(subDir, 0o755)

	okItem := &sqlite.ItemRecord{ID: guid.New(), LibraryID: libAllowed.ID, ParentID: libAllowed.ID, Type: "Movie", MediaType: "Video", Name: "Kids Film", Path: okFile}
	isStrmFlagItem := &sqlite.ItemRecord{ID: guid.New(), LibraryID: libAllowed.ID, ParentID: libAllowed.ID, Type: "Movie", MediaType: "Video", Name: "FlagStrm", Path: okFile, IsStrm: true}
	escapeItem := &sqlite.ItemRecord{ID: guid.New(), LibraryID: libAllowed.ID, ParentID: libAllowed.ID, Type: "Movie", MediaType: "Video", Name: "Escape", Path: escapeSym}
	dirItem := &sqlite.ItemRecord{ID: guid.New(), LibraryID: libAllowed.ID, ParentID: libAllowed.ID, Type: "Movie", MediaType: "Video", Name: "Dir", Path: subDir}
	adultFile := filepath.Join(libRestricted.Paths[0], "adult.mp4")
	_ = os.WriteFile(adultFile, []byte("ADULT"), 0o644)
	adultItem := &sqlite.ItemRecord{ID: guid.New(), LibraryID: libRestricted.ID, ParentID: libRestricted.ID, Type: "Movie", MediaType: "Video", Name: "Adult Film", Path: adultFile}
	for _, rec := range []*sqlite.ItemRecord{okItem, isStrmFlagItem, escapeItem, dirItem, adultItem} {
		_ = libRepo.UpsertItem(ctx, rec)
	}

	streamItem := func(id string) *httptest.ResponseRecorder {
		exp := time.Now().UTC().Add(time.Hour).Unix()
		sig := h.signStreamToken(id, exp)
		req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/Dlna/srv-1/stream/%s?exp=%d&sig=%s", id, exp, sig), nil)
		req.RemoteAddr = "127.0.0.1:12345"
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}

	if rec := streamItem(okItem.ID); rec.Code != http.StatusOK || rec.Body.String() != "MP4OK" {
		t.Fatalf("M-22: valid stream failed: %d %q", rec.Code, rec.Body.String())
	}
	if rec := streamItem(isStrmFlagItem.ID); rec.Code != http.StatusForbidden {
		t.Fatalf("M-22: expected 403 for IsStrm=true item, got %d", rec.Code)
	}
	if rec := streamItem(escapeItem.ID); rec.Code != http.StatusForbidden {
		t.Fatalf("M-22: expected 403 for symlink outside library, got %d", rec.Code)
	}
	if rec := streamItem(dirItem.ID); rec.Code != http.StatusNotFound {
		t.Fatalf("M-22: expected 404 for directory target, got %d", rec.Code)
	}

	// 4. M-22: Optional default user policy filtering (EnabledFolders & BlockedTags)
	userRepo := sqlite.NewUserRepo(store)
	kidUser := &sqlite.UserRecord{
		Name:   "kid_dlna",
		Policy: sqlite.DefaultUserPolicy(false),
		Config: sqlite.DefaultUserConfig(),
	}
	kidUser.Policy.EnableAllFolders = false
	kidUser.Policy.EnabledFolders = []string{libAllowed.ID}
	_ = userRepo.CreateUser(ctx, kidUser)
	h.SetUserRepo(userRepo)
	h.SetDefaultUserID(kidUser.ID)

	sendSOAP := func(ip, action, body string) *httptest.ResponseRecorder {
		envelope := fmt.Sprintf(`<?xml version="1.0"?><s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"><s:Body><u:%s xmlns:u="%s">%s</u:%s></s:Body></s:Envelope>`,
			action, contentDirectoryServiceType, body, action)
		req := httptest.NewRequest(http.MethodPost, "/Dlna/srv-1/contentdirectory/control", strings.NewReader(envelope))
		req.RemoteAddr = ip + ":12345"
		req.Header.Set("SOAPACTION", fmt.Sprintf(`"%s#%s"`, contentDirectoryServiceType, action))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}

	recRoot := sendSOAP("192.168.1.40", "Browse", `<ObjectID>0</ObjectID><BrowseFlag>BrowseDirectChildren</BrowseFlag><StartingIndex>0</StartingIndex><RequestedCount>10</RequestedCount>`)
	if recRoot.Code != http.StatusOK || !strings.Contains(recRoot.Body.String(), "Kids") || strings.Contains(recRoot.Body.String(), "Adults") {
		t.Fatalf("M-22: expected default user policy to filter root libraries to Kids only, got %d %s", recRoot.Code, recRoot.Body.String())
	}
	if rec := streamItem(adultItem.ID); rec.Code != http.StatusNotFound {
		t.Fatalf("M-22: expected 404 streaming restricted library item under default user policy, got %d", rec.Code)
	}

	// 5. M-21: SOAP per-IP rate limiting
	for i := 0; i < maxDLNASOAPPerIPPerMin; i++ {
		rec := sendSOAP("192.168.1.41", "GetSystemUpdateID", "")
		if rec.Code != http.StatusOK {
			t.Fatalf("SOAP call #%d expected 200, got %d", i, rec.Code)
		}
	}
	if rec := sendSOAP("192.168.1.41", "GetSystemUpdateID", ""); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("M-21: expected 429 after exceeding SOAP per-IP rate limit, got %d", rec.Code)
	}

	// 6. M-28: Rollback in-memory profiles on DB write failure for create & delete
	bodyCreate, _ := json.Marshal(DlnaProfile{Id: "p-existing", Name: "Existing"})
	recSeed := httptest.NewRecorder()
	mux.ServeHTTP(recSeed, httptest.NewRequest(http.MethodPost, "/Dlna/Profiles", bytes.NewReader(bodyCreate)))
	if recSeed.Code != http.StatusOK {
		t.Fatalf("seed profile status=%d", recSeed.Code)
	}

	_ = store.Close()

	bodyNew, _ := json.Marshal(DlnaProfile{Id: "p-fail-create", Name: "FailCreate"})
	recFailCreate := httptest.NewRecorder()
	mux.ServeHTTP(recFailCreate, httptest.NewRequest(http.MethodPost, "/Dlna/Profiles", bytes.NewReader(bodyNew)))
	if recFailCreate.Code != http.StatusInternalServerError {
		t.Fatalf("M-28: expected 500 on create profile DB failure, got %d", recFailCreate.Code)
	}
	h.mu.RLock()
	_, leakedNew := h.profiles["p-fail-create"]
	h.mu.RUnlock()
	if leakedNew {
		t.Fatal("M-28: failed create profile was not rolled back")
	}

	recFailDel := httptest.NewRecorder()
	mux.ServeHTTP(recFailDel, httptest.NewRequest(http.MethodDelete, "/Dlna/Profiles/p-existing", nil))
	if recFailDel.Code != http.StatusInternalServerError {
		t.Fatalf("M-28: expected 500 on delete profile DB failure, got %d", recFailDel.Code)
	}
	h.mu.RLock()
	_, stillExists := h.profiles["p-existing"]
	h.mu.RUnlock()
	if !stillExists {
		t.Fatal("M-28: failed delete profile was not rolled back")
	}
}
