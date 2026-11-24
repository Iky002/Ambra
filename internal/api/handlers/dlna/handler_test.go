package dlna

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func setupDLNATestEnv(t *testing.T) (*Handler, *http.ServeMux, *sqlite.LibraryRepo, *sqlite.Store) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	store, err := sqlite.Open(dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	libRepo := sqlite.NewLibraryRepo(store)
	cfg := config.Default(t.TempDir())
	cfg.ServerName = "Ambra Test Server"
	cfg.ServerID = "server-uuid-1234"

	h := New(cfg, libRepo, store)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux, AuthFuncs{
		RequireAuth: func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("X-Auth") == "deny" {
					http.Error(w, "unauthorized", http.StatusUnauthorized)
					return
				}
				next.ServeHTTP(w, r)
			})
		},
		RequireAdmin: func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("X-Admin") == "deny" {
					http.Error(w, "forbidden", http.StatusForbidden)
					return
				}
				next.ServeHTTP(w, r)
			})
		},
		UserIDFromCtx: func(ctx context.Context) string { return "admin-1" },
	})
	return h, mux, libRepo, store
}

func TestProfileCRUDAndPersistence(t *testing.T) {
	_, mux, libRepo, store := setupDLNATestEnv(t)
	cfg := config.Default(t.TempDir())

	// 1. Admin middleware check
	req := httptest.NewRequest(http.MethodGet, "/Dlna/ProfileInfos", nil)
	req.Header.Set("X-Admin", "deny")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for non-admin, got %d", rec.Code)
	}

	// 2. Default profile
	req = httptest.NewRequest(http.MethodGet, "/Dlna/Profiles/Default", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /Dlna/Profiles/Default got %d", rec.Code)
	}
	var def DlnaProfile
	if err := json.Unmarshal(rec.Body.Bytes(), &def); err != nil {
		t.Fatalf("decode default profile: %v", err)
	}
	if def.Id != defaultProfileID || def.Name != "Default" {
		t.Fatalf("unexpected default profile: %+v", def)
	}

	// 3. Create profile validation errors
	req = httptest.NewRequest(http.MethodPost, "/Dlna/Profiles", strings.NewReader("{bad"))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid json, got %d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodPost, "/Dlna/Profiles", strings.NewReader(`{"Name":""}`))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty name, got %d", rec.Code)
	}

	// 4. Create two custom profiles
	for _, name := range []string{"Sony Bravia", "Apple TV"} {
		body, _ := json.Marshal(DlnaProfile{Name: name, FriendlyName: name + " DLNA"})
		req = httptest.NewRequest(http.MethodPost, "/Dlna/Profiles", bytes.NewReader(body))
		rec = httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("POST /Dlna/Profiles (%s) got %d: %s", name, rec.Code, rec.Body.String())
		}
		if rec.Header().Get("X-Profile-Id") == "" {
			t.Fatalf("expected X-Profile-Id header")
		}
	}

	// 5. List profiles (Default first, then alphabetical)
	req = httptest.NewRequest(http.MethodGet, "/Dlna/ProfileInfos", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /Dlna/ProfileInfos got %d", rec.Code)
	}
	var list []DlnaProfile
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(list) != 3 || list[0].Id != defaultProfileID || list[1].Name != "Apple TV" || list[2].Name != "Sony Bravia" {
		t.Fatalf("unexpected profile order: %+v", list)
	}

	sonyID := list[2].Id

	// 6. Get single profile & 404
	req = httptest.NewRequest(http.MethodGet, "/Dlna/Profiles/"+sonyID, nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /Dlna/Profiles/{Id} got %d", rec.Code)
	}
	req = httptest.NewRequest(http.MethodGet, "/Dlna/Profiles/nonexistent", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown profile, got %d", rec.Code)
	}

	// 7. Update profile (bad json, empty name, success)
	req = httptest.NewRequest(http.MethodPost, "/Dlna/Profiles/"+sonyID, strings.NewReader("{bad"))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 on update bad json, got %d", rec.Code)
	}
	req = httptest.NewRequest(http.MethodPost, "/Dlna/Profiles/"+sonyID, strings.NewReader(`{"Name":""}`))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 on update empty name, got %d", rec.Code)
	}

	updBody, _ := json.Marshal(DlnaProfile{Name: "Sony Bravia 4K", Manufacturer: "Sony"})
	req = httptest.NewRequest(http.MethodPost, "/Dlna/Profiles/"+sonyID, bytes.NewReader(updBody))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("POST /Dlna/Profiles/{Id} got %d", rec.Code)
	}

	// 8. Reload from SQLite store to verify persistence
	reloaded := New(cfg, libRepo, store)
	reloadedMux := http.NewServeMux()
	reloaded.RegisterRoutes(reloadedMux, AuthFuncs{})
	req = httptest.NewRequest(http.MethodGet, "/Dlna/Profiles/"+sonyID, nil)
	rec = httptest.NewRecorder()
	reloadedMux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("reloaded profile missing: %d", rec.Code)
	}
	var got DlnaProfile
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if got.Name != "Sony Bravia 4K" || got.Manufacturer != "Sony" {
		t.Fatalf("reloaded profile mismatch: %+v", got)
	}

	// 9. Delete profile (cannot delete default, 404 unknown, delete custom)
	req = httptest.NewRequest(http.MethodDelete, "/Dlna/Profiles/default", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 deleting default profile, got %d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodDelete, "/Dlna/Profiles/missing", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 deleting missing profile, got %d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodDelete, "/Dlna/Profiles/"+sonyID, nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("DELETE /Dlna/Profiles/{Id} got %d", rec.Code)
	}
}

func TestUPnPDescriptionSCPDAndIcons(t *testing.T) {
	_, mux, _, _ := setupDLNATestEnv(t)

	// Description XML (GET + HEAD + alias without .xml + X-Forwarded-Proto)
	for _, tc := range []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/Dlna/my-uuid/description.xml"},
		{http.MethodHead, "/Dlna/my-uuid/description.xml"},
		{http.MethodGet, "/Dlna/my-uuid/description"},
	} {
		req := httptest.NewRequest(tc.method, tc.path, nil)
		req.Host = "192.168.1.5:8096"
		req.Header.Set("X-Forwarded-Proto", "https")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s %s got %d", tc.method, tc.path, rec.Code)
		}
		if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "text/xml") {
			t.Fatalf("expected text/xml, got %q", ct)
		}
		if tc.method == http.MethodGet {
			body := rec.Body.String()
			for _, want := range []string{
				"urn:schemas-upnp-org:device:MediaServer:1",
				"<friendlyName>Ambra Test Server</friendlyName>",
				"<UDN>uuid:my-uuid</UDN>",
				"https://192.168.1.5:8096",
				"urn:schemas-upnp-org:service:ContentDirectory:1",
				"urn:schemas-upnp-org:service:ConnectionManager:1",
			} {
				if !strings.Contains(body, want) {
					t.Fatalf("description.xml missing %q", want)
				}
			}
			// Verify valid XML
			var parsed any
			if err := xml.Unmarshal([]byte(body), &parsed); err != nil {
				t.Fatalf("description.xml invalid XML: %v", err)
			}
		} else if rec.Body.Len() != 0 {
			t.Fatalf("HEAD should have empty body")
		}
	}

	// ContentDirectory & ConnectionManager SCPD XML
	for _, path := range []string{
		"/Dlna/my-uuid/contentdirectory/contentdirectory.xml",
		"/Dlna/my-uuid/contentdirectory/contentdirectory",
		"/Dlna/my-uuid/connectionmanager/connectionmanager.xml",
		"/Dlna/my-uuid/connectionmanager/connectionmanager",
	} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s got %d", path, rec.Code)
		}
		var parsed any
		if err := xml.Unmarshal(rec.Body.Bytes(), &parsed); err != nil {
			t.Fatalf("%s invalid XML: %v", path, err)
		}
	}

	// Icons: SVG, PNG, global path, and unsupported extension
	req := httptest.NewRequest(http.MethodGet, "/Dlna/my-uuid/icons/logo.svg", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "image/svg+xml" || !strings.Contains(rec.Body.String(), "<svg") {
		t.Fatalf("unexpected svg icon response: %d %s", rec.Code, rec.Header().Get("Content-Type"))
	}

	req = httptest.NewRequest(http.MethodGet, "/Dlna/icons/logo120.png", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "image/png" || !bytes.HasPrefix(rec.Body.Bytes(), []byte{0x89, 0x50, 0x4E, 0x47}) {
		t.Fatalf("unexpected png icon response: %d %s", rec.Code, rec.Header().Get("Content-Type"))
	}

	req = httptest.NewRequest(http.MethodGet, "/Dlna/my-uuid/icons/logo.bmp", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unsupported icon, got %d", rec.Code)
	}

	// Unknown 2-segment route -> 404
	req = httptest.NewRequest(http.MethodGet, "/Dlna/my-uuid/unknown", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown 2-segment route, got %d", rec.Code)
	}
}

func TestContentDirectoryAndConnectionManagerSOAP(t *testing.T) {
	_, mux, libRepo, _ := setupDLNATestEnv(t)
	ctx := context.Background()

	// Seed real libraries and items into SQLite LibraryRepo
	movieLib, err := libRepo.CreateLibrary(ctx, "Movies", "movies", []string{"/media/movies"})
	if err != nil {
		t.Fatalf("create movie lib: %v", err)
	}
	musicLib, err := libRepo.CreateLibrary(ctx, "Music", "music", []string{"/media/music"})
	if err != nil {
		t.Fatalf("create music lib: %v", err)
	}

	rt := int64(5400 * 10_000_000) // 1:30:00
	movieItem := &sqlite.ItemRecord{
		LibraryID:    movieLib.ID,
		ParentID:     movieLib.ID,
		Type:         "Movie",
		MediaType:    "Video",
		Name:         "Inception & Friends",
		Container:    "mkv",
		RuntimeTicks: &rt,
	}
	if err := libRepo.UpsertItem(ctx, movieItem); err != nil {
		t.Fatalf("upsert movie: %v", err)
	}
	if err := libRepo.ReplaceMediaSourcesAndStreams(ctx, movieItem.ID, []sqlite.MediaSourceRecord{
		{Name: "Inception.mkv", Path: "/media/movies/Inception.mkv", Container: "mkv", Size: 4096},
	}); err != nil {
		t.Fatalf("upsert media source: %v", err)
	}

	// Episode, Audio, Photo, Folder (MusicAlbum) for branch coverage
	epItem := &sqlite.ItemRecord{
		LibraryID: movieLib.ID,
		ParentID:  movieLib.ID,
		Type:      "Episode",
		MediaType: "Video",
		Name:      "Pilot",
		Container: "webm",
	}
	_ = libRepo.UpsertItem(ctx, epItem)

	albumFolder := &sqlite.ItemRecord{
		LibraryID: musicLib.ID,
		Type:      "MusicAlbum",
		IsFolder:  true,
		Name:      "Greatest Hits",
	}
	_ = libRepo.UpsertItem(ctx, albumFolder)

	songItem := &sqlite.ItemRecord{
		LibraryID: musicLib.ID,
		ParentID:  albumFolder.ID,
		Type:      "Audio",
		MediaType: "Audio",
		Name:      "Track 01",
		Container: "flac",
	}
	_ = libRepo.UpsertItem(ctx, songItem)

	photoItem := &sqlite.ItemRecord{
		LibraryID: musicLib.ID,
		ParentID:  albumFolder.ID,
		Type:      "Photo",
		MediaType: "Photo",
		Name:      "Cover",
	}
	_ = libRepo.UpsertItem(ctx, photoItem)

	postSOAP := func(path, soapAction, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
		req.Host = "127.0.0.1:8096"
		if soapAction != "" {
			req.Header.Set("SOAPACTION", soapAction)
		}
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}

	// 1. Browse root ("0") DirectChildren
	browseRootBody := `<?xml version="1.0" encoding="utf-8"?>
<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/">
  <s:Body>
    <u:Browse xmlns:u="urn:schemas-upnp-org:service:ContentDirectory:1">
      <ObjectID>0</ObjectID>
      <BrowseFlag>BrowseDirectChildren</BrowseFlag>
      <Filter>*</Filter>
      <StartingIndex>0</StartingIndex>
      <RequestedCount>10</RequestedCount>
      <SortCriteria></SortCriteria>
    </u:Browse>
  </s:Body>
</s:Envelope>`
	rec := postSOAP("/Dlna/srv/contentdirectory/control", `"urn:schemas-upnp-org:service:ContentDirectory:1#Browse"`, browseRootBody)
	if rec.Code != http.StatusOK {
		t.Fatalf("Browse root got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "<NumberReturned>2</NumberReturned>") ||
		!strings.Contains(rec.Body.String(), "<TotalMatches>2</TotalMatches>") ||
		!strings.Contains(rec.Body.String(), "Movies") {
		t.Fatalf("unexpected Browse root response: %s", rec.Body.String())
	}

	// 2. Browse root with StartingIndex >= total and RequestedCount slicing
	browseSliceBody := strings.Replace(browseRootBody, "<RequestedCount>10</RequestedCount>", "<RequestedCount>1</RequestedCount>", 1)
	rec = postSOAP("/Dlna/srv/contentdirectory/control", "", browseSliceBody)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "<NumberReturned>1</NumberReturned>") {
		t.Fatalf("unexpected Browse slice response: %d %s", rec.Code, rec.Body.String())
	}

	browseOOBBody := strings.Replace(browseRootBody, "<StartingIndex>0</StartingIndex>", "<StartingIndex>99</StartingIndex>", 1)
	rec = postSOAP("/Dlna/srv/contentdirectory/control", "", browseOOBBody)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "<NumberReturned>0</NumberReturned>") {
		t.Fatalf("unexpected Browse OOB response: %d %s", rec.Code, rec.Body.String())
	}

	// 3. Browse movie library children (returns Movie + Episode with res URLs & escaped title)
	browseMoviesBody := strings.Replace(browseRootBody, "<ObjectID>0</ObjectID>", "<ObjectID>"+movieLib.ID+"</ObjectID>", 1)
	rec = postSOAP("/Dlna/srv/contentdirectory/control", "", browseMoviesBody)
	if rec.Code != http.StatusOK {
		t.Fatalf("Browse movies got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "video/x-matroska") ||
		!strings.Contains(rec.Body.String(), "/Videos/"+movieItem.ID+"/stream?static=true") ||
		!strings.Contains(rec.Body.String(), "duration=&#34;1:30:00&#34;") {
		t.Fatalf("unexpected Browse movies DIDL: %s", rec.Body.String())
	}

	// 4. Browse music library fallback (albumFolder has empty ParentID, only LibraryID)
	browseMusicBody := strings.Replace(browseRootBody, "<ObjectID>0</ObjectID>", "<ObjectID>"+musicLib.ID+"</ObjectID>", 1)
	rec = postSOAP("/Dlna/srv/contentdirectory/control", "", browseMusicBody)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "object.container.album.musicAlbum") {
		t.Fatalf("unexpected Browse music fallback DIDL: %d %s", rec.Code, rec.Body.String())
	}

	// 5. Browse album children (Audio + Photo)
	browseAlbumBody := strings.Replace(browseRootBody, "<ObjectID>0</ObjectID>", "<ObjectID>"+albumFolder.ID+"</ObjectID>", 1)
	rec = postSOAP("/Dlna/srv/contentdirectory/control", "", browseAlbumBody)
	if rec.Code != http.StatusOK ||
		!strings.Contains(rec.Body.String(), "audio/flac") ||
		!strings.Contains(rec.Body.String(), "object.item.imageItem.photo") {
		t.Fatalf("unexpected Browse album DIDL: %d %s", rec.Code, rec.Body.String())
	}

	// 6. BrowseMetadata for root, library, item, and missing item
	for _, oid := range []string{"0", movieLib.ID, movieItem.ID} {
		metaBody := strings.Replace(browseRootBody, "<ObjectID>0</ObjectID>", "<ObjectID>"+oid+"</ObjectID>", 1)
		metaBody = strings.Replace(metaBody, "BrowseDirectChildren", "BrowseMetadata", 1)
		rec = postSOAP("/Dlna/srv/contentdirectory/control", "", metaBody)
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "<NumberReturned>1</NumberReturned>") {
			t.Fatalf("BrowseMetadata(%s) failed: %d %s", oid, rec.Code, rec.Body.String())
		}
	}
	metaMissing := strings.Replace(browseRootBody, "<ObjectID>0</ObjectID>", "<ObjectID>does-not-exist</ObjectID>", 1)
	metaMissing = strings.Replace(metaMissing, "BrowseDirectChildren", "BrowseMetadata", 1)
	rec = postSOAP("/Dlna/srv/contentdirectory/control", "", metaMissing)
	if rec.Code != http.StatusInternalServerError || !strings.Contains(rec.Body.String(), "UPnPError") {
		t.Fatalf("expected SOAP fault for missing metadata object, got %d", rec.Code)
	}

	// 7. GetSystemUpdateID / GetSearchCapabilities / GetSortCapabilities / Unknown Action
	for _, tc := range []struct {
		action string
		want   string
	}{
		{"GetSystemUpdateID", "<Id>1</Id>"},
		{"GetSearchCapabilities", "<SearchCaps>dc:title,upnp:class</SearchCaps>"},
		{"GetSortCapabilities", "<SortCaps>dc:title,dc:date</SortCaps>"},
	} {
		rec = postSOAP("/Dlna/srv/contentdirectory/control", `"urn:schemas-upnp-org:service:ContentDirectory:1#`+tc.action+`"`, "")
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), tc.want) {
			t.Fatalf("%s failed: %d %s", tc.action, rec.Code, rec.Body.String())
		}
	}

	rec = postSOAP("/Dlna/srv/contentdirectory/control", `"urn:schemas-upnp-org:service:ContentDirectory:1#BogusAction"`, "")
	if rec.Code != http.StatusInternalServerError || !strings.Contains(rec.Body.String(), "<errorCode>401</errorCode>") {
		t.Fatalf("expected 500 SOAP fault for unknown CD action, got %d", rec.Code)
	}
	rec = postSOAP("/Dlna/srv/contentdirectory/control", "", "<not-valid-xml")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 for malformed XML, got %d", rec.Code)
	}

	// 8. ConnectionManager SOAP: GetProtocolInfo, GetCurrentConnectionIDs, Unknown
	rec = postSOAP("/Dlna/srv/connectionmanager/control", `"urn:schemas-upnp-org:service:ConnectionManager:1#GetProtocolInfo"`, "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "video/mp4") {
		t.Fatalf("GetProtocolInfo failed: %d %s", rec.Code, rec.Body.String())
	}

	rec = postSOAP("/Dlna/srv/connectionmanager/control", `"urn:schemas-upnp-org:service:ConnectionManager:1#GetCurrentConnectionIDs"`, "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "<ConnectionIDs>0</ConnectionIDs>") {
		t.Fatalf("GetCurrentConnectionIDs failed: %d %s", rec.Code, rec.Body.String())
	}

	rec = postSOAP("/Dlna/srv/connectionmanager/control", `"urn:schemas-upnp-org:service:ConnectionManager:1#Unknown"`, "")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 for unknown CM action, got %d", rec.Code)
	}
	rec = postSOAP("/Dlna/srv/connectionmanager/control", "", "")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 for empty CM action, got %d", rec.Code)
	}
}

func TestUPnPEventSubscriptions(t *testing.T) {
	h, mux, _, _ := setupDLNATestEnv(t)

	// Missing CALLBACK -> 412
	req := httptest.NewRequest("SUBSCRIBE", "/Dlna/srv/contentdirectory/events", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusPreconditionFailed {
		t.Fatalf("expected 412 without CALLBACK, got %d", rec.Code)
	}

	// Valid SUBSCRIBE
	req = httptest.NewRequest("SUBSCRIBE", "/Dlna/srv/contentdirectory/events", nil)
	req.Header.Set("CALLBACK", "<http://192.168.1.50:9000/notify>")
	req.Header.Set("NT", "upnp:event")
	req.Header.Set("TIMEOUT", "Second-600")
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("SUBSCRIBE got %d", rec.Code)
	}
	sid := rec.Header().Get("SID")
	if !strings.HasPrefix(sid, "uuid:") {
		t.Fatalf("expected uuid: SID, got %q", sid)
	}
	if rec.Header().Get("TIMEOUT") != "Second-600" {
		t.Fatalf("expected Second-600, got %q", rec.Header().Get("TIMEOUT"))
	}
	if h.ActiveSubscriptionCount() != 1 {
		t.Fatalf("expected 1 active subscription, got %d", h.ActiveSubscriptionCount())
	}

	// Renew with valid SID
	req = httptest.NewRequest("SUBSCRIBE", "/Dlna/srv/contentdirectory/events", nil)
	req.Header.Set("SID", sid)
	req.Header.Set("TIMEOUT", "Second-infinite")
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Header().Get("TIMEOUT") != "Second-86400" {
		t.Fatalf("renew failed: %d %s", rec.Code, rec.Header().Get("TIMEOUT"))
	}

	// Renew on wrong service -> 412
	req = httptest.NewRequest("SUBSCRIBE", "/Dlna/srv/connectionmanager/events", nil)
	req.Header.Set("SID", sid)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusPreconditionFailed {
		t.Fatalf("expected 412 renewing on wrong service, got %d", rec.Code)
	}

	// UNSUBSCRIBE without SID -> 412
	req = httptest.NewRequest("UNSUBSCRIBE", "/Dlna/srv/contentdirectory/events", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusPreconditionFailed {
		t.Fatalf("expected 412 without SID, got %d", rec.Code)
	}

	// UNSUBSCRIBE valid SID -> 200
	req = httptest.NewRequest("UNSUBSCRIBE", "/Dlna/srv/contentdirectory/events", nil)
	req.Header.Set("SID", sid)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("UNSUBSCRIBE got %d", rec.Code)
	}
	if h.ActiveSubscriptionCount() != 0 {
		t.Fatalf("expected 0 active subscriptions, got %d", h.ActiveSubscriptionCount())
	}

	// UNSUBSCRIBE already-removed SID -> 412
	req = httptest.NewRequest("UNSUBSCRIBE", "/Dlna/srv/contentdirectory/events", nil)
	req.Header.Set("SID", sid)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusPreconditionFailed {
		t.Fatalf("expected 412 for removed SID, got %d", rec.Code)
	}
}

func TestHelpersAndNilDependencies(t *testing.T) {
	// New with nil cfg/libRepo/store should not panic
	h := New(nil, nil, nil)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux, AuthFuncs{})

	// Browse with nil libRepo
	req := httptest.NewRequest(http.MethodPost, "/Dlna/srv/contentdirectory/control",
		strings.NewReader(`<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"><s:Body><u:Browse xmlns:u="urn:schemas-upnp-org:service:ContentDirectory:1"><ObjectID>0</ObjectID></u:Browse></s:Body></s:Envelope>`))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "<TotalMatches>0</TotalMatches>") {
		t.Fatalf("Browse nil repo failed: %d %s", rec.Code, rec.Body.String())
	}

	// BrowseMetadata non-root with nil libRepo -> SOAP fault
	req = httptest.NewRequest(http.MethodPost, "/Dlna/srv/contentdirectory/control",
		strings.NewReader(`<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"><s:Body><u:Browse xmlns:u="urn:schemas-upnp-org:service:ContentDirectory:1"><ObjectID>abc</ObjectID><BrowseFlag>BrowseMetadata</BrowseFlag></u:Browse></s:Body></s:Envelope>`))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 for nil repo metadata, got %d", rec.Code)
	}

	// parseTimeoutHeader edge cases
	if got := parseTimeoutHeader(""); got != defaultSubTimeout {
		t.Fatalf("empty timeout got %d", got)
	}
	if got := parseTimeoutHeader("Second-5"); got != minSubTimeout {
		t.Fatalf("low timeout got %d", got)
	}
	if got := parseTimeoutHeader("Second-999999"); got != maxSubTimeout {
		t.Fatalf("high timeout got %d", got)
	}
	if got := parseTimeoutHeader("invalid"); got != defaultSubTimeout {
		t.Fatalf("invalid timeout got %d", got)
	}

	// MIME helpers
	for k, want := range map[string]string{
		"mkv": "video/x-matroska", "webm": "video/webm", "mov": "video/quicktime", "avi": "video/x-msvideo", "mp4": "video/mp4",
	} {
		if got := videoMimeType(k); got != want {
			t.Fatalf("videoMimeType(%s)=%s want %s", k, got, want)
		}
	}
	for k, want := range map[string]string{
		"flac": "audio/flac", "aac": "audio/mp4", "ogg": "audio/ogg", "wav": "audio/wav", "mp3": "audio/mpeg",
	} {
		if got := audioMimeType(k); got != want {
			t.Fatalf("audioMimeType(%s)=%s want %s", k, got, want)
		}
	}
}
