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
	"github.com/ambra-media/ambra/internal/plugins"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

type roundTripFuncDLNA func(*http.Request) (*http.Response, error)

func (f roundTripFuncDLNA) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

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

	// 4. M-22 & H-02: Unauthenticated least-privilege intersection when DLNADefaultUserID is NOT set!
	userRepo := sqlite.NewUserRepo(store)
	adminUser := &sqlite.UserRecord{
		Name:    "admin_dlna",
		IsAdmin: true,
		Policy:  sqlite.DefaultUserPolicy(true),
		Config:  sqlite.DefaultUserConfig(),
	}
	kidUser := &sqlite.UserRecord{
		Name:   "kid_dlna",
		Policy: sqlite.DefaultUserPolicy(false),
		Config: sqlite.DefaultUserConfig(),
	}
	kidUser.Policy.EnableAllFolders = false
	kidUser.Policy.EnabledFolders = []string{libAllowed.ID}
	maxPG := 5
	kidUser.Policy.MaxParentalRating = &maxPG
	_ = userRepo.CreateUser(ctx, adminUser)
	_ = userRepo.CreateUser(ctx, kidUser)
	h.SetUserRepo(userRepo)
	// Note: even BEFORE SetDefaultUserID is called, H-02 intersection policy restricts unauthenticated DLNA to Kids only!

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

	recIntersect := sendSOAP("192.168.1.39", "Browse", `<ObjectID>0</ObjectID><BrowseFlag>BrowseDirectChildren</BrowseFlag><StartingIndex>0</StartingIndex><RequestedCount>10</RequestedCount>`)
	if recIntersect.Code != http.StatusOK || !strings.Contains(recIntersect.Body.String(), "Kids") || strings.Contains(recIntersect.Body.String(), "Adults") {
		t.Fatalf("H-02: expected strictest intersection policy (without DLNADefaultUserID) to expose Kids only, got %d %s", recIntersect.Code, recIntersect.Body.String())
	}

	// Explicit SetDefaultUserID also continues to work
	h.SetDefaultUserID(kidUser.ID)

	recRoot := sendSOAP("192.168.1.40", "Browse", `<ObjectID>0</ObjectID><BrowseFlag>BrowseDirectChildren</BrowseFlag><StartingIndex>0</StartingIndex><RequestedCount>10</RequestedCount>`)
	if recRoot.Code != http.StatusOK || !strings.Contains(recRoot.Body.String(), "Kids") || strings.Contains(recRoot.Body.String(), "Adults") {
		t.Fatalf("M-22: expected default user policy to filter root libraries to Kids only, got %d %s", recRoot.Code, recRoot.Body.String())
	}
	if rec := streamItem(adultItem.ID); rec.Code != http.StatusNotFound {
		t.Fatalf("M-22: expected 404 streaming restricted library item under default user policy, got %d", rec.Code)
	}

	// 4b. [H-02 & L-01] .strm server-side Range reverse-proxy (no 302 leak), albumArtURI, BoxSet/Playlist browse, and GENA NOTIFY
	upstreamPayload := []byte("0123456789STRMPROXY")
	upstreamStrmSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeContent(w, r, "cloud.mp4", time.Now(), bytes.NewReader(upstreamPayload))
	}))
	defer upstreamStrmSrv.Close()
	h.SetProxyClient(upstreamStrmSrv.Client())
	h.SetStreamResolverWithHeaders(func(_ context.Context, rawPath string) (*plugins.StreamResolveResult, error) {
		if strings.HasSuffix(rawPath, "valid_cloud.strm") {
			return &plugins.StreamResolveResult{URL: upstreamStrmSrv.URL + "/signed/cloud.mp4?token=secret", Is302: true}, nil
		}
		return nil, fmt.Errorf("unexpected strm")
	})
	// Note: ValidateRemoteURL blocks loopback unless resolved or tested; use public IP URL with custom RoundTripper
	h.SetStreamResolverWithHeaders(func(_ context.Context, rawPath string) (*plugins.StreamResolveResult, error) {
		if strings.HasSuffix(rawPath, "valid_cloud.strm") {
			return &plugins.StreamResolveResult{URL: "https://93.184.216.34/signed/cloud.mp4?token=secret", Is302: true}, nil
		}
		return nil, fmt.Errorf("unexpected strm")
	})
	h.SetProxyClient(&http.Client{
		Transport: roundTripFuncDLNA(func(req *http.Request) (*http.Response, error) {
			rec := httptest.NewRecorder()
			http.ServeContent(rec, req, "cloud.mp4", time.Unix(1700000000, 0), bytes.NewReader(upstreamPayload))
			return rec.Result(), nil
		}),
	})
	validStrmPath := filepath.Join(libDir, "valid_cloud.strm")
	_ = os.WriteFile(validStrmPath, []byte("https://93.184.216.34/signed/cloud.mp4?token=secret\n"), 0o644)
	validStrmItem := &sqlite.ItemRecord{ID: guid.New(), LibraryID: libAllowed.ID, ParentID: libAllowed.ID, Type: "Movie", MediaType: "Video", Name: "Cloud Film", OfficialRating: "G", Path: validStrmPath, IsStrm: true}
	_ = libRepo.UpsertItem(ctx, validStrmItem)
	_ = libRepo.UpsertImage(ctx, &sqlite.ImageRecord{ItemID: okItem.ID, ImageType: "Primary", ImageIndex: 0, Path: okFile, Tag: "arttag99"})

	exp := time.Now().UTC().Add(time.Hour).Unix()
	sigCloud := h.signStreamToken(validStrmItem.ID, exp)
	reqCloud := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/Dlna/srv-1/stream/%s?exp=%d&sig=%s", validStrmItem.ID, exp, sigCloud), nil)
	reqCloud.RemoteAddr = "192.168.1.50:23456"
	reqCloud.Header.Set("Range", "bytes=10-18")
	recCloud := httptest.NewRecorder()
	mux.ServeHTTP(recCloud, reqCloud)
	if recCloud.Code != http.StatusPartialContent || recCloud.Body.String() != "STRMPROXY" || recCloud.Header().Get("Location") != "" {
		t.Fatalf("H-02: expected 206 server-side reverse proxy without 302 Location, got code=%d loc=%q body=%q", recCloud.Code, recCloud.Header().Get("Location"), recCloud.Body.String())
	}

	// BoxSet BrowseDirectChildren + albumArtURI with tag
	box, _ := libRepo.CreateCollection(ctx, "Kids Box", "", []string{okItem.ID, adultItem.ID})
	recBox := sendSOAP("192.168.1.42", "Browse", fmt.Sprintf(`<ObjectID>%s</ObjectID><BrowseFlag>BrowseDirectChildren</BrowseFlag><StartingIndex>0</StartingIndex><RequestedCount>10</RequestedCount>`, box.ID))
	if recBox.Code != http.StatusOK || !strings.Contains(recBox.Body.String(), "Kids Film") || strings.Contains(recBox.Body.String(), "Adult Film") || !strings.Contains(recBox.Body.String(), "albumArtURI") || !strings.Contains(recBox.Body.String(), "tag=arttag99") {
		t.Fatalf("L-01: expected BoxSet browse to return Kids Film with albumArtURI tag and hide Adult Film, got %d %s", recBox.Code, recBox.Body.String())
	}

	// GENA NOTIFY callback delivery & loopback/public callback rejection
	for _, rejectedCB := range []string{"<http://127.0.0.1:9000/cb>", "<http://93.184.216.34:9000/cb>"} {
		if isValidDLNACallback(rejectedCB) {
			t.Fatalf("L-01: expected callback %s to be rejected (only private LAN IPs allowed)", rejectedCB)
		}
	}
	notifyCh := make(chan string, 2)
	h.SetNotifyClient(&http.Client{
		Transport: roundTripFuncDLNA(func(req *http.Request) (*http.Response, error) {
			if req.Method == "NOTIFY" && req.Header.Get("NT") == "upnp:event" && req.Header.Get("NTS") == "upnp:propchange" {
				var buf bytes.Buffer
				_, _ = buf.ReadFrom(req.Body)
				notifyCh <- buf.String()
			}
			rec := httptest.NewRecorder()
			rec.WriteHeader(http.StatusOK)
			return rec.Result(), nil
		}),
	})
	reqNotifySub := httptest.NewRequest("SUBSCRIBE", "/Dlna/srv-1/contentdirectory/events", nil)
	reqNotifySub.RemoteAddr = "192.168.1.88:5000"
	reqNotifySub.Header.Set("CALLBACK", "<http://192.168.1.88:8888/upnp/event>")
	recNotifySub := httptest.NewRecorder()
	mux.ServeHTTP(recNotifySub, reqNotifySub)
	if recNotifySub.Code != http.StatusOK {
		t.Fatalf("SUBSCRIBE for notify test got %d", recNotifySub.Code)
	}
	newUID := h.BumpSystemUpdate()
	select {
	case notifyBody := <-notifyCh:
		if !strings.Contains(notifyBody, fmt.Sprintf("<SystemUpdateID>%d</SystemUpdateID>", newUID)) {
			t.Fatalf("unexpected GENA NOTIFY XML body: %s", notifyBody)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for async GENA NOTIFY callback")
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
