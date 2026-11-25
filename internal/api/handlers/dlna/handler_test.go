package dlna

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func setupDLNATestEnv(t *testing.T) (*Handler, http.Handler, *sqlite.LibraryRepo, *sqlite.Store) {
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
	lanWrapper := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.RemoteAddr == "192.0.2.1:1234" {
			r.RemoteAddr = "127.0.0.1:12345"
		}
		mux.ServeHTTP(w, r)
	})
	return h, lanWrapper, libRepo, store
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
	h, mux, libRepo, _ := setupDLNATestEnv(t)
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
		!strings.Contains(rec.Body.String(), "/Dlna/srv/stream/"+movieItem.ID+"?exp=") ||
		!strings.Contains(rec.Body.String(), "sig=") ||
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

	// 7. GetSystemUpdateID / BumpSystemUpdate / GetSearchCapabilities / GetSortCapabilities / Unknown Action
	if uid := h.BumpSystemUpdate(); uid != 2 {
		t.Fatalf("expected BumpSystemUpdate() == 2, got %d", uid)
	}
	for _, tc := range []struct {
		action string
		want   string
	}{
		{"GetSystemUpdateID", "<Id>2</Id>"},
		{"GetSearchCapabilities", "<SearchCaps>dc:title,upnp:class</SearchCaps>"},
		{"GetSortCapabilities", "<SortCaps>dc:title,dc:date</SortCaps>"},
	} {
		rec = postSOAP("/Dlna/srv/contentdirectory/control", `"urn:schemas-upnp-org:service:ContentDirectory:1#`+tc.action+`"`, "")
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), tc.want) {
			t.Fatalf("%s failed: %d %s", tc.action, rec.Code, rec.Body.String())
		}
	}

	// Reject creating profile with default ID
	recDef := httptest.NewRecorder()
	mux.ServeHTTP(recDef, httptest.NewRequest(http.MethodPost, "/Dlna/Profiles", strings.NewReader(`{"Id":"default","Name":"Hijack"}`)))
	if recDef.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 when creating profile with Id=default, got %d", recDef.Code)
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
	req.RemoteAddr = "127.0.0.1:12345"
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "<TotalMatches>0</TotalMatches>") {
		t.Fatalf("Browse nil repo failed: %d %s", rec.Code, rec.Body.String())
	}

	// BrowseMetadata non-root with nil libRepo -> SOAP fault
	req = httptest.NewRequest(http.MethodPost, "/Dlna/srv/contentdirectory/control",
		strings.NewReader(`<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"><s:Body><u:Browse xmlns:u="urn:schemas-upnp-org:service:ContentDirectory:1"><ObjectID>abc</ObjectID><BrowseFlag>BrowseMetadata</BrowseFlag></u:Browse></s:Body></s:Envelope>`))
	req.RemoteAddr = "127.0.0.1:12345"
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

func TestDLNASearchSortCriteriaAndPNGDimensions(t *testing.T) {
	_, mux, libRepo, _ := setupDLNATestEnv(t)
	ctx := context.Background()

	// 1. Verify PNG icon actual dimensions match description.xml (120x120)
	req := httptest.NewRequest(http.MethodGet, "/Dlna/icons/logo120.png", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET logo120.png status=%d", rec.Code)
	}
	imgCfg, err := png.DecodeConfig(bytes.NewReader(rec.Body.Bytes()))
	if err != nil {
		t.Fatalf("decode png config: %v", err)
	}
	if imgCfg.Width != 120 || imgCfg.Height != 120 {
		t.Fatalf("expected 120x120 PNG icon, got %dx%d", imgCfg.Width, imgCfg.Height)
	}

	// 2. Seed libraries and items with distinct names & dates
	movieLib, err := libRepo.CreateLibrary(ctx, "Movies", "movies", []string{"/media/movies"})
	if err != nil {
		t.Fatal(err)
	}
	musicLib, err := libRepo.CreateLibrary(ctx, "Audio", "music", []string{"/media/music"})
	if err != nil {
		t.Fatal(err)
	}

	d2010 := time.Date(2010, 7, 16, 0, 0, 0, 0, time.UTC).Unix()
	d2023 := time.Date(2023, 7, 21, 0, 0, 0, 0, time.UTC).Unix()
	y2010, y2023 := 2010, 2023

	_ = libRepo.UpsertItem(ctx, &sqlite.ItemRecord{
		LibraryID:      movieLib.ID,
		ParentID:       movieLib.ID,
		Type:           "Movie",
		MediaType:      "Video",
		Name:           "Alpha Movie",
		SortName:       "Alpha Movie",
		Container:      "mp4",
		ProductionYear: &y2010,
		PremiereDate:   &d2010,
	})
	_ = libRepo.UpsertItem(ctx, &sqlite.ItemRecord{
		LibraryID:      movieLib.ID,
		ParentID:       movieLib.ID,
		Type:           "Movie",
		MediaType:      "Video",
		Name:           "Zulu Inception",
		SortName:       "Zulu Inception",
		Container:      "mkv",
		ProductionYear: &y2023,
		PremiereDate:   &d2023,
	})
	_ = libRepo.UpsertItem(ctx, &sqlite.ItemRecord{
		LibraryID: musicLib.ID,
		ParentID:  musicLib.ID,
		Type:      "Audio",
		MediaType: "Audio",
		Name:      "Inception OST",
		SortName:  "Inception OST",
		Container: "flac",
	})

	postSOAP := func(action, inner string) *httptest.ResponseRecorder {
		body := `<?xml version="1.0" encoding="utf-8"?><s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"><s:Body><u:` + action + ` xmlns:u="urn:schemas-upnp-org:service:ContentDirectory:1">` + inner + `</u:` + action + `></s:Body></s:Envelope>`
		req := httptest.NewRequest(http.MethodPost, "/Dlna/srv/contentdirectory/control", strings.NewReader(body))
		req.Host = "127.0.0.1:8096"
		req.Header.Set("SOAPACTION", `"urn:schemas-upnp-org:service:ContentDirectory:1#`+action+`"`)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}

	// 3. Browse with -dc:title -> Zulu before Alpha
	rec = postSOAP("Browse", "<ObjectID>"+movieLib.ID+"</ObjectID><BrowseFlag>BrowseDirectChildren</BrowseFlag><StartingIndex>0</StartingIndex><RequestedCount>10</RequestedCount><SortCriteria>-dc:title</SortCriteria>")
	if rec.Code != http.StatusOK {
		t.Fatalf("Browse -dc:title status=%d", rec.Code)
	}
	bDesc := rec.Body.String()
	if strings.Index(bDesc, "Zulu Inception") > strings.Index(bDesc, "Alpha Movie") {
		t.Fatalf("expected Zulu Inception before Alpha Movie for -dc:title, got %s", bDesc)
	}

	// 4. Browse with +dc:date -> 2010 Alpha before 2023 Zulu; -dc:date -> 2023 Zulu before 2010 Alpha
	rec = postSOAP("Browse", "<ObjectID>"+movieLib.ID+"</ObjectID><BrowseFlag>BrowseDirectChildren</BrowseFlag><StartingIndex>0</StartingIndex><RequestedCount>10</RequestedCount><SortCriteria>+dc:date</SortCriteria>")
	bDateAsc := rec.Body.String()
	if strings.Index(bDateAsc, "Alpha Movie") > strings.Index(bDateAsc, "Zulu Inception") {
		t.Fatalf("expected Alpha Movie (2010) before Zulu Inception (2023) for +dc:date, got %s", bDateAsc)
	}
	rec = postSOAP("Browse", "<ObjectID>"+movieLib.ID+"</ObjectID><BrowseFlag>BrowseDirectChildren</BrowseFlag><StartingIndex>0</StartingIndex><RequestedCount>10</RequestedCount><SortCriteria>-dc:date</SortCriteria>")
	bDateDesc := rec.Body.String()
	if strings.Index(bDateDesc, "Zulu Inception") > strings.Index(bDateDesc, "Alpha Movie") {
		t.Fatalf("expected Zulu Inception (2023) before Alpha Movie (2010) for -dc:date, got %s", bDateDesc)
	}

	// 5. Browse root "0" with -dc:title -> Movies before Audio
	rec = postSOAP("Browse", "<ObjectID>0</ObjectID><BrowseFlag>BrowseDirectChildren</BrowseFlag><StartingIndex>0</StartingIndex><RequestedCount>10</RequestedCount><SortCriteria>-dc:title</SortCriteria>")
	bRootDesc := rec.Body.String()
	if strings.Index(bRootDesc, "Movies") > strings.Index(bRootDesc, "Audio") {
		t.Fatalf("expected Movies before Audio for root -dc:title, got %s", bRootDesc)
	}

	// 6. Search by dc:title
	rec = postSOAP("Search", `<ContainerID>0</ContainerID><SearchCriteria>dc:title contains "Inception"</SearchCriteria><StartingIndex>0</StartingIndex><RequestedCount>10</RequestedCount><SortCriteria>+dc:title</SortCriteria>`)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "<TotalMatches>2</TotalMatches>") ||
		!strings.Contains(rec.Body.String(), "Zulu Inception") || !strings.Contains(rec.Body.String(), "Inception OST") {
		t.Fatalf("Search by dc:title unexpected: %d %s", rec.Code, rec.Body.String())
	}

	// 7. Search by upnp:class (audio only)
	rec = postSOAP("Search", `<ContainerID>0</ContainerID><SearchCriteria>upnp:class derivedfrom "object.item.audioItem"</SearchCriteria><StartingIndex>0</StartingIndex><RequestedCount>10</RequestedCount>`)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "<TotalMatches>1</TotalMatches>") ||
		!strings.Contains(rec.Body.String(), "Inception OST") {
		t.Fatalf("Search by upnp:class audio unexpected: %d %s", rec.Code, rec.Body.String())
	}

	// 8. Search combined dc:title + upnp:class scoped to movieLib
	rec = postSOAP("Search", `<ContainerID>`+movieLib.ID+`</ContainerID><SearchCriteria>(dc:title contains "Inception") and (upnp:class derivedfrom "object.item.videoItem.movie")</SearchCriteria><StartingIndex>0</StartingIndex><RequestedCount>10</RequestedCount>`)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "<TotalMatches>1</TotalMatches>") ||
		!strings.Contains(rec.Body.String(), "Zulu Inception") {
		t.Fatalf("Search combined unexpected: %d %s", rec.Code, rec.Body.String())
	}
}

func TestDLNASignedStreamAndGENACap(t *testing.T) {
	h, mux, libRepo, _ := setupDLNATestEnv(t)
	ctx := context.Background()

	tmp := t.TempDir()
	mediaFile := filepath.Join(tmp, "sample.mp4")
	payload := []byte("0123456789abcdef-dlna-video-stream")
	if err := os.WriteFile(mediaFile, payload, 0o644); err != nil {
		t.Fatal(err)
	}
	lib, err := libRepo.CreateLibrary(ctx, "Movies", "movies", []string{tmp})
	if err != nil {
		t.Fatal(err)
	}
	item := &sqlite.ItemRecord{
		LibraryID: lib.ID, ParentID: lib.ID,
		Type: "Movie", MediaType: "Video", Name: "Signed Movie", Container: "mp4", Path: mediaFile,
	}
	if err := libRepo.UpsertItem(ctx, item); err != nil {
		t.Fatal(err)
	}

	// 1. Valid signed stream URL serves full content and supports Range requests
	exp := time.Now().UTC().Add(time.Hour).Unix()
	sig := h.signStreamToken(item.ID, exp)
	streamURL := "/Dlna/srv/stream/" + item.ID + "?exp=" + strconv.FormatInt(exp, 10) + "&sig=" + sig

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, streamURL, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("signed DLNA stream got %d: %s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Content-Type") != "video/mp4" {
		t.Fatalf("expected video/mp4 Content-Type, got %q", rec.Header().Get("Content-Type"))
	}
	if !bytes.Equal(rec.Body.Bytes(), payload) {
		t.Fatalf("payload mismatch: got %q", rec.Body.String())
	}

	// Range request -> 206 Partial Content
	rangeReq := httptest.NewRequest(http.MethodGet, streamURL, nil)
	rangeReq.Header.Set("Range", "bytes=0-3")
	recRange := httptest.NewRecorder()
	mux.ServeHTTP(recRange, rangeReq)
	if recRange.Code != http.StatusPartialContent || recRange.Body.String() != "0123" {
		t.Fatalf("expected 206 '0123', got %d %q", recRange.Code, recRange.Body.String())
	}

	// 2. Missing, tampered, wrong-item, and expired signatures -> 403 Forbidden
	expiredExp := time.Now().UTC().Add(-time.Minute).Unix()
	expiredSig := h.signStreamToken(item.ID, expiredExp)
	for _, badURL := range []string{
		"/Dlna/srv/stream/" + item.ID,
		"/Dlna/srv/stream/" + item.ID + "?exp=" + strconv.FormatInt(exp, 10) + "&sig=deadbeef",
		"/Dlna/srv/stream/other-item-id?exp=" + strconv.FormatInt(exp, 10) + "&sig=" + sig,
		"/Dlna/srv/stream/" + item.ID + "?exp=" + strconv.FormatInt(expiredExp, 10) + "&sig=" + expiredSig,
	} {
		recBad := httptest.NewRecorder()
		mux.ServeHTTP(recBad, httptest.NewRequest(http.MethodGet, badURL, nil))
		if recBad.Code != http.StatusForbidden {
			t.Fatalf("GET %s expected 403, got %d", badURL, recBad.Code)
		}
	}

	// 2b. Valid signature for missing item / missing disk file / nil repo -> 404
	missingID := "nonexistent-item"
	missingSig := h.signStreamToken(missingID, exp)
	recMissing := httptest.NewRecorder()
	mux.ServeHTTP(recMissing, httptest.NewRequest(http.MethodGet, "/Dlna/srv/stream/"+missingID+"?exp="+strconv.FormatInt(exp, 10)+"&sig="+missingSig, nil))
	if recMissing.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for missing item, got %d", recMissing.Code)
	}

	goneItem := &sqlite.ItemRecord{
		LibraryID: lib.ID, ParentID: lib.ID,
		Type: "Movie", MediaType: "Video", Name: "Gone", Path: filepath.Join(tmp, "gone.mkv"),
	}
	_ = libRepo.UpsertItem(ctx, goneItem)
	goneSig := h.signStreamToken(goneItem.ID, exp)
	recGone := httptest.NewRecorder()
	mux.ServeHTTP(recGone, httptest.NewRequest(http.MethodGet, "/Dlna/srv/stream/"+goneItem.ID+"?exp="+strconv.FormatInt(exp, 10)+"&sig="+goneSig, nil))
	if recGone.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for missing disk file, got %d", recGone.Code)
	}

	nilHandler := New(nil, nil, nil)
	nilMux := http.NewServeMux()
	nilHandler.RegisterRoutes(nilMux, AuthFuncs{})
	nilSig := nilHandler.signStreamToken("abc", exp)
	recNil := httptest.NewRecorder()
	nilReq := httptest.NewRequest(http.MethodGet, "/Dlna/srv/stream/abc?exp="+strconv.FormatInt(exp, 10)+"&sig="+nilSig, nil)
	nilReq.RemoteAddr = "127.0.0.1:12345"
	nilMux.ServeHTTP(recNil, nilReq)
	if recNil.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for nil repo, got %d", recNil.Code)
	}

	// 2c. dlnaMediaContentType coverage across video, audio, image, and fallback extensions
	for ext, want := range map[string]string{
		"a.mp4": "video/mp4", "a.mkv": "video/x-matroska", "a.webm": "video/webm",
		"a.mov": "video/quicktime", "a.avi": "video/x-msvideo", "a.ts": "video/mp2t",
		"a.mp3": "audio/mpeg", "a.flac": "audio/flac", "a.m4a": "audio/mp4",
		"a.aac": "audio/aac", "a.ogg": "audio/ogg", "a.wav": "audio/wav",
		"a.jpg": "image/jpeg", "a.png": "image/png", "a.webp": "image/webp",
		"a.bin": "application/octet-stream",
	} {
		if got := dlnaMediaContentType(ext); got != want {
			t.Fatalf("dlnaMediaContentType(%q) = %q, want %q", ext, got, want)
		}
	}

	// 3. GENA subscription cap (256) and expiry pruning
	var firstSID string
	for i := 0; i < maxDLNASubscriptions; i++ {
		req := httptest.NewRequest("SUBSCRIBE", "/Dlna/srv/contentdirectory/events", nil)
		req.Header.Set("CALLBACK", "<http://192.168.1.50:9000/notify>")
		req.Header.Set("TIMEOUT", "Second-600")
		recSub := httptest.NewRecorder()
		mux.ServeHTTP(recSub, req)
		if recSub.Code != http.StatusOK {
			t.Fatalf("subscription %d failed: %d", i, recSub.Code)
		}
		if i == 0 {
			firstSID = recSub.Header().Get("SID")
		}
	}
	if h.ActiveSubscriptionCount() != maxDLNASubscriptions {
		t.Fatalf("expected %d subscriptions, got %d", maxDLNASubscriptions, h.ActiveSubscriptionCount())
	}

	// 257th subscription must be rejected with 503 Service Unavailable
	reqOverflow := httptest.NewRequest("SUBSCRIBE", "/Dlna/srv/contentdirectory/events", nil)
	reqOverflow.Header.Set("CALLBACK", "<http://192.168.1.50:9000/notify>")
	recOverflow := httptest.NewRecorder()
	mux.ServeHTTP(recOverflow, reqOverflow)
	if recOverflow.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 when subscriptions full, got %d", recOverflow.Code)
	}

	// Expire one subscription and verify next SUBSCRIBE prunes it and succeeds
	h.mu.Lock()
	sub := h.subscriptions[firstSID]
	sub.ExpiresAt = time.Now().UTC().Add(-time.Second)
	h.subscriptions[firstSID] = sub
	h.mu.Unlock()

	reqAfterPrune := httptest.NewRequest("SUBSCRIBE", "/Dlna/srv/contentdirectory/events", nil)
	reqAfterPrune.Header.Set("CALLBACK", "<http://192.168.1.50:9000/notify>")
	recAfterPrune := httptest.NewRecorder()
	mux.ServeHTTP(recAfterPrune, reqAfterPrune)
	if recAfterPrune.Code != http.StatusOK {
		t.Fatalf("expected 200 after pruning expired subscription, got %d", recAfterPrune.Code)
	}
	if h.ActiveSubscriptionCount() != maxDLNASubscriptions {
		t.Fatalf("expected %d active subscriptions after prune+add, got %d", maxDLNASubscriptions, h.ActiveSubscriptionCount())
	}
}

func TestDLNALANRestrictionAndFeatureFlag(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "dlna_sec.db")
	store, err := sqlite.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	ctx := context.Background()
	tmp := t.TempDir()
	strmFile := filepath.Join(tmp, "secret.strm")
	_ = os.WriteFile(strmFile, []byte("https://user:pass@upstream.example/live.m3u8\n"), 0o600)

	libRepo := sqlite.NewLibraryRepo(store)
	lib, _ := libRepo.CreateLibrary(ctx, "Movies", "movies", []string{tmp})
	strmItem := &sqlite.ItemRecord{
		LibraryID: lib.ID, ParentID: lib.ID,
		Type: "Movie", MediaType: "Video", Name: "Secret STRM", Path: strmFile, IsStrm: true,
	}
	_ = libRepo.UpsertItem(ctx, strmItem)

	cfg := config.Default(tmp)
	h := New(cfg, libRepo, store)
	rawMux := http.NewServeMux()
	h.RegisterRoutes(rawMux, AuthFuncs{})

	exp := time.Now().UTC().Add(time.Hour).Unix()
	strmSig := h.signStreamToken(strmItem.ID, exp)
	strmURL := "/Dlna/srv/stream/" + strmItem.ID + "?exp=" + strconv.FormatInt(exp, 10) + "&sig=" + strmSig

	// 1. Public direct IP -> 403 on description.xml, SOAP Browse, and signed stream
	for _, tc := range []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/Dlna/srv/description.xml"},
		{http.MethodPost, "/Dlna/srv/contentdirectory/control"},
		{http.MethodGet, strmURL},
	} {
		req := httptest.NewRequest(tc.method, tc.path, nil)
		req.RemoteAddr = "203.0.113.55:44444"
		rec := httptest.NewRecorder()
		rawMux.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("public %s %s expected 403, got %d", tc.method, tc.path, rec.Code)
		}
	}

	// 2. Public client forwarded through local reverse proxy -> 403
	reqProxy := httptest.NewRequest(http.MethodGet, "/Dlna/srv/description.xml", nil)
	reqProxy.RemoteAddr = "127.0.0.1:44444"
	reqProxy.Header.Set("X-Forwarded-For", "198.51.100.20")
	recProxy := httptest.NewRecorder()
	rawMux.ServeHTTP(recProxy, reqProxy)
	if recProxy.Code != http.StatusForbidden {
		t.Fatalf("public client behind local proxy expected 403, got %d", recProxy.Code)
	}

	// 3. LAN request for .strm item -> 403 (never leak .strm credentials)
	reqStrm := httptest.NewRequest(http.MethodGet, strmURL, nil)
	reqStrm.RemoteAddr = "192.168.1.50:55555"
	recStrm := httptest.NewRecorder()
	rawMux.ServeHTTP(recStrm, reqStrm)
	if recStrm.Code != http.StatusForbidden {
		t.Fatalf("LAN .strm download expected 403, got %d", recStrm.Code)
	}

	// 4. EnableDLNA=false blocks even loopback LAN requests -> 403
	cfg.EnableDLNA = false
	reqDisabled := httptest.NewRequest(http.MethodGet, "/Dlna/srv/description.xml", nil)
	reqDisabled.RemoteAddr = "127.0.0.1:55555"
	recDisabled := httptest.NewRecorder()
	rawMux.ServeHTTP(recDisabled, reqDisabled)
	if recDisabled.Code != http.StatusForbidden {
		t.Fatalf("disabled DLNA expected 403, got %d", recDisabled.Code)
	}
}
