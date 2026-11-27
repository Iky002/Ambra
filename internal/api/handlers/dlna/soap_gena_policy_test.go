package dlna

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ambra-media/ambra/internal/api/dto"
	"github.com/ambra-media/ambra/internal/api/middleware"
	"github.com/ambra-media/ambra/internal/core/auth"
	"github.com/ambra-media/ambra/internal/core/transcode"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/plugins"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func createDLNAFakeFFmpeg(t *testing.T, payload string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "fake-ffmpeg.sh")
	script := "#!/bin/sh\nprintf '%s' '" + payload + "'\n"
	if err := os.WriteFile(p, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func soapEnvelope(serviceType, action, innerXML string) string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="utf-8"?>
<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/">
  <s:Body>
    <u:%s xmlns:u="%s">%s</u:%s>
  </s:Body>
</s:Envelope>`, action, serviceType, innerXML, action)
}

func TestV0110_DLNAPolicyInheritanceAndLeastPrivilege(t *testing.T) {
	h, mux, libRepo, store := setupDLNATestEnv(t)
	ctx := context.Background()
	userRepo := sqlite.NewUserRepo(store)
	authSvc := auth.NewService(userRepo, h.cfg)

	libDirA := t.TempDir()
	libDirB := t.TempDir()
	libA, _ := libRepo.CreateLibrary(ctx, "FamilyLib", "movies", []string{libDirA})
	libB, _ := libRepo.CreateLibrary(ctx, "SecretLib", "movies", []string{libDirB})

	fileSafe := filepath.Join(libDirA, "safe.mp4")
	fileUnrated := filepath.Join(libDirA, "unrated.mp4")
	fileAdult := filepath.Join(libDirA, "adult.mp4")
	fileSecret := filepath.Join(libDirB, "secret.mp4")
	for _, p := range []string{fileSafe, fileUnrated, fileAdult, fileSecret} {
		_ = os.WriteFile(p, []byte("VIDEO-BYTES"), 0o644)
	}

	seriesRated := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: libA.ID, ParentID: libA.ID,
		Type: "Series", Name: "Rated Show", OfficialRating: "G", IsFolder: true,
	}
	epInheritsSeries := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: libA.ID, ParentID: seriesRated.ID, SeriesID: seriesRated.ID,
		Type: "Episode", MediaType: "Video", Name: "Episode 1", OfficialRating: "", Container: "mp4", Path: fileSafe,
	}
	movieSafe := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: libA.ID, ParentID: libA.ID,
		Type: "Movie", MediaType: "Video", Name: "Safe Kid Movie", OfficialRating: "G", Container: "mp4", Path: fileSafe,
	}
	movieUnrated := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: libA.ID, ParentID: libA.ID,
		Type: "Movie", MediaType: "Video", Name: "Mystery Unrated", OfficialRating: "", Container: "mp4", Path: fileUnrated,
	}
	movieAdult := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: libA.ID, ParentID: libA.ID,
		Type: "Movie", MediaType: "Video", Name: "Adult R Movie", OfficialRating: "R", Container: "mp4", Path: fileAdult,
	}
	movieSecret := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: libB.ID, ParentID: libB.ID,
		Type: "Movie", MediaType: "Video", Name: "Secret Movie", OfficialRating: "G", Container: "mp4", Path: fileSecret,
	}
	for _, rec := range []*sqlite.ItemRecord{seriesRated, epInheritsSeries, movieSafe, movieUnrated, movieAdult, movieSecret} {
		if err := libRepo.UpsertItem(ctx, rec); err != nil {
			t.Fatal(err)
		}
	}
	_ = libRepo.ReplaceItemTaxonomies(ctx, movieSafe.ID, "tag", []string{"family", "kids"})
	_ = libRepo.ReplaceItemTaxonomies(ctx, seriesRated.ID, "tag", []string{"family"})
	_ = libRepo.ReplaceItemTaxonomies(ctx, movieAdult.ID, "tag", []string{"family"})
	_ = libRepo.UpsertSearchFTS(ctx, movieSafe.ID, "Safe Kid Movie", "safe kid movie", "", "", "")
	_ = libRepo.UpsertSearchFTS(ctx, movieUnrated.ID, "Mystery Unrated", "mystery unrated", "", "", "")

	// 1. DefaultUserID set when userRepo == nil -> denyAll
	h.SetDefaultUserID("missing-user")
	if _, denyAll := h.resolveDefaultUserPolicy(ctx); !denyAll {
		t.Fatalf("expected denyAll when DefaultUserID set without userRepo")
	}
	if h.isItemAllowedByDefaultPolicy(ctx, movieSafe) {
		t.Fatalf("expected isItemAllowedByDefaultPolicy=false under denyAll")
	}

	h.SetUserRepo(userRepo)
	admin, _ := authSvc.CreateUser(ctx, "admin", "adminpass", true)
	u1, _ := authSvc.CreateUser(ctx, "user1", "pass1234", false)
	u2, _ := authSvc.CreateUser(ctx, "user2", "pass1234", false)
	disabledUser := &sqlite.UserRecord{
		ID:         guid.New(),
		Name:       "disabled",
		IsDisabled: true,
		Policy:     sqlite.DefaultUserPolicy(false),
		Config:     sqlite.DefaultUserConfig(),
	}
	if err := userRepo.CreateUser(ctx, disabledUser); err != nil {
		t.Fatal(err)
	}

	// 2. Explicit DLNADefaultUserID: disabled user -> denyAll; admin -> unrestricted; restricted user -> enforced
	h.SetDefaultUserID(disabledUser.ID)
	if _, denyAll := h.resolveDefaultUserPolicy(ctx); !denyAll {
		t.Fatalf("expected denyAll for disabled DLNADefaultUserID")
	}
	h.SetDefaultUserID(admin.ID)
	if pol, denyAll := h.resolveDefaultUserPolicy(ctx); denyAll || pol != nil {
		t.Fatalf("expected nil unrestricted policy for admin DLNADefaultUserID")
	}

	maxPG13 := 7
	maxG := 5
	pol1 := sqlite.DefaultUserPolicy(false)
	pol1.EnableAllFolders = false
	pol1.EnabledFolders = []string{libA.ID, libB.ID}
	pol1.MaxParentalRating = &maxPG13
	pol1.BlockedTags = []string{"gore"}
	pol1.BlockUnratedItems = []string{"Movie", "Series", "Music"}
	pol1.AllowedTags = []string{"family", "action"}
	_ = userRepo.UpdateUserPolicy(ctx, u1.ID, pol1)
	u1.Policy = pol1

	pol2 := sqlite.DefaultUserPolicy(false)
	pol2.EnableAllFolders = false
	pol2.EnabledFolders = []string{libA.ID}
	pol2.MaxParentalRating = &maxG
	pol2.BlockedTags = []string{"nsfw"}
	pol2.BlockUnratedItems = []string{"All"}
	pol2.AllowedTags = []string{"family"}
	_ = userRepo.UpdateUserPolicy(ctx, u2.ID, pol2)
	u2.Policy = pol2

	h.SetDefaultUserID(u1.ID)
	if !h.isItemAllowedByDefaultPolicy(ctx, movieSafe) {
		t.Fatalf("expected movieSafe allowed for u1")
	}
	if !h.isItemAllowedByDefaultPolicy(ctx, epInheritsSeries) {
		t.Fatalf("expected epInheritsSeries allowed via Series rating & tag inheritance")
	}
	if h.isItemAllowedByDefaultPolicy(ctx, movieUnrated) {
		t.Fatalf("expected movieUnrated blocked by BlockUnratedItems")
	}
	if h.isItemAllowedByDefaultPolicy(ctx, movieAdult) {
		t.Fatalf("expected movieAdult blocked by MaxParentalRating")
	}

	// 3. Unconfigured DLNADefaultUserID -> least-privilege intersection across u1 & u2!
	h.SetDefaultUserID("")
	interPol, denyAll := h.resolveDefaultUserPolicy(ctx)
	if denyAll || interPol == nil {
		t.Fatalf("expected valid intersection policy, got pol=%+v denyAll=%v", interPol, denyAll)
	}
	if len(interPol.EnabledFolders) != 1 || interPol.EnabledFolders[0] != guid.Normalize(libA.ID) {
		t.Fatalf("expected intersection folder [libA], got %v", interPol.EnabledFolders)
	}
	if interPol.MaxParentalRating == nil || *interPol.MaxParentalRating != maxG {
		t.Fatalf("expected min parental rating %d, got %v", maxG, interPol.MaxParentalRating)
	}
	if len(interPol.AllowedTags) != 1 || interPol.AllowedTags[0] != "family" {
		t.Fatalf("expected AllowedTags intersection [family], got %v", interPol.AllowedTags)
	}

	// Verify SOAP Browse & Search under intersection policy only expose allowed items (movieSafe, seriesRated, epInheritsSeries)
	browseRootBody := soapEnvelope(contentDirectoryServiceType, "Browse",
		`<ObjectID>0</ObjectID><BrowseFlag>BrowseDirectChildren</BrowseFlag><StartingIndex>0</StartingIndex><RequestedCount>50</RequestedCount><SortCriteria>-dc:title</SortCriteria>`)
	req := httptest.NewRequest(http.MethodPost, "/Dlna/srv-1/contentdirectory/control", strings.NewReader(browseRootBody))
	req.RemoteAddr = "192.168.1.10:50000"
	req.Header.Set("SOAPACTION", `"`+contentDirectoryServiceType+`#Browse"`)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "FamilyLib") || strings.Contains(rec.Body.String(), "SecretLib") {
		t.Fatalf("Browse root under least-privilege failed: %d %s", rec.Code, rec.Body.String())
	}

	// Search with FTS miss fallback (mid-word substring "afe Kid") under extra policy filtering
	searchBody := soapEnvelope(contentDirectoryServiceType, "Search",
		`<ContainerID>`+libA.ID+`</ContainerID><SearchCriteria>dc:title contains "afe Kid" and upnp:class = "object.item.videoItem.movie"</SearchCriteria><StartingIndex>0</StartingIndex><RequestedCount>10</RequestedCount><SortCriteria>+dc:title</SortCriteria>`)
	reqSearch := httptest.NewRequest(http.MethodPost, "/Dlna/srv-1/contentdirectory/control", strings.NewReader(searchBody))
	reqSearch.RemoteAddr = "192.168.1.10:50000"
	reqSearch.Header.Set("SOAPACTION", `"`+contentDirectoryServiceType+`#Search"`)
	recSearch := httptest.NewRecorder()
	mux.ServeHTTP(recSearch, reqSearch)
	if recSearch.Code != http.StatusOK || !strings.Contains(recSearch.Body.String(), "Safe Kid Movie") || strings.Contains(recSearch.Body.String(), "Adult R Movie") {
		t.Fatalf("SOAP Search with intersection policy failed: %d %s", recSearch.Code, recSearch.Body.String())
	}

	// Direct FTS hit + needExtraFilter pagination (startIdx=0 reqCount=1, and startIdx=99 >= total) in both Search & BrowseDirectChildren
	sReq := httptest.NewRequest(http.MethodPost, "/Dlna/srv-1/contentdirectory/control", nil)
	sReq.RemoteAddr = "192.168.1.10:50000"
	if res, err := h.handleSearch(ctx, sReq, map[string]string{"ContainerID": "0", "SearchCriteria": `dc:title contains "Safe"`, "StartingIndex": "0", "RequestedCount": "1"}); err != nil || !strings.Contains(res, "Safe Kid Movie") {
		t.Fatalf("FTS Search page 1 failed: %v %s", err, res)
	}
	if res, err := h.handleSearch(ctx, sReq, map[string]string{"ContainerID": "0", "SearchCriteria": `*`, "StartingIndex": "99", "RequestedCount": "1"}); err != nil || !strings.Contains(res, "<NumberReturned>0</NumberReturned>") {
		t.Fatalf("Search out-of-range page failed: %v %s", err, res)
	}
	if ch, tot, err := h.browseDirectChildrenForRequest(ctx, sReq, libA.ID, 0, 1, "+dc:title", "http://192.168.1.5:8096", "srv-1"); err != nil || tot < 1 || len(ch) != 1 {
		t.Fatalf("browseDirectChildrenForRequest page 1 under needExtraFilter failed: tot=%d len=%d err=%v", tot, len(ch), err)
	}
	if ch, _, err := h.browseDirectChildrenForRequest(ctx, sReq, libA.ID, 99, 5, "", "http://192.168.1.5:8096", "srv-1"); err != nil || len(ch) != 0 {
		t.Fatalf("browseDirectChildrenForRequest out-of-range under needExtraFilter failed: len=%d err=%v", len(ch), err)
	}
	if ch, _, err := h.browseDirectChildrenForRequest(ctx, sReq, "0", 99, 1, "+dc:title", "http://192.168.1.5:8096", "srv-1"); err != nil || len(ch) != 0 {
		t.Fatalf("browseDirectChildrenForRequest root out-of-range failed: len=%d err=%v", len(ch), err)
	}

	// Disjoint AllowedTags -> buildStrictestIntersectionPolicy returns denyAll=true
	_, denyDisjoint := buildStrictestIntersectionPolicy([]*sqlite.UserRecord{
		{ID: "a", Policy: dto.UserPolicy{EnableAllFolders: true, AllowedTags: []string{"tag1"}}},
		{ID: "b", Policy: dto.UserPolicy{EnableAllFolders: true, AllowedTags: []string{"tag2"}}},
	})
	if !denyDisjoint {
		t.Fatalf("expected denyAll when AllowedTags intersection is empty")
	}

	// Request-scoped AuthContext checks in resolvePolicyForRequest
	reqAdmin := httptest.NewRequest(http.MethodGet, "/Dlna/srv-1/description.xml", nil)
	reqAdmin = reqAdmin.WithContext(middleware.WithAuthContext(ctx, &middleware.AuthContext{User: admin}))
	if pol, deny := h.resolvePolicyForRequest(ctx, reqAdmin); deny || pol != nil {
		t.Fatalf("expected unrestricted for admin AuthContext")
	}
	reqDisabled := httptest.NewRequest(http.MethodGet, "/Dlna/srv-1/description.xml", nil)
	reqDisabled = reqDisabled.WithContext(middleware.WithAuthContext(ctx, &middleware.AuthContext{User: disabledUser}))
	if _, deny := h.resolvePolicyForRequest(ctx, reqDisabled); !deny {
		t.Fatalf("expected denyAll for disabled user AuthContext")
	}
	reqU1 := httptest.NewRequest(http.MethodGet, "/Dlna/srv-1/description.xml", nil)
	reqU1 = reqU1.WithContext(middleware.WithAuthContext(ctx, &middleware.AuthContext{User: u1}))
	if pol, deny := h.resolvePolicyForRequest(ctx, reqU1); deny || pol == nil {
		t.Fatalf("expected user policy for u1 AuthContext")
	}
	// Also test resolvePolicyForRequest when userRepo is nil
	h.SetUserRepo(nil)
	if _, deny := h.resolvePolicyForRequest(ctx, reqDisabled); !deny {
		t.Fatalf("expected denyAll for disabled ac.User without userRepo")
	}
	if pol, deny := h.resolvePolicyForRequest(ctx, reqU1); deny || pol == nil {
		t.Fatalf("expected ac.User.Policy without userRepo")
	}

	// matchesDLNABlockUnrated branches
	if matchesDLNABlockUnrated("Folder", "", []string{"*"}) {
		t.Fatalf("Folder should not match unrated block")
	}
	if !matchesDLNABlockUnrated("Audio", "Audio", []string{"Music"}) {
		t.Fatalf("Audio should match Music unrated block")
	}
}

func TestV0110_SOAPContainersTranscodeStreamAndGENANotify(t *testing.T) {
	h, mux, libRepo, store := setupDLNATestEnv(t)
	ctx := context.Background()
	h.cfg.FFmpegPath = createDLNAFakeFFmpeg(t, "FAKE-DLNA-MPEGTS-DATA")
	mgr := transcode.NewManager(h.cfg, transcode.WithCapabilities(transcode.SoftwareOnlyCapabilities()))
	defer mgr.Close()
	h.SetTranscodeManager(mgr)

	libDir := t.TempDir()
	lib, _ := libRepo.CreateLibrary(ctx, "MediaLib", "movies", []string{libDir})

	// Seed Album with image, Audio in uncommon container (.wma), Video in uncommon container (.avi), Photo, BoxSet, Public Playlist
	imgPath := filepath.Join(libDir, "cover.jpg")
	_ = os.WriteFile(imgPath, []byte("JPG"), 0o644)
	wmaPath := filepath.Join(libDir, "track.wma")
	_ = os.WriteFile(wmaPath, []byte("WMA-RAW"), 0o644)
	aviPath := filepath.Join(libDir, "clip.avi")
	_ = os.WriteFile(aviPath, []byte("AVI-RAW"), 0o644)
	strmPath := filepath.Join(libDir, "live.strm")
	_ = os.WriteFile(strmPath, []byte("https://93.184.216.34/stream/video.mkv\n"), 0o644)

	album := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: lib.ID, ParentID: lib.ID,
		Type: "MusicAlbum", Name: "Greatest Hits", IsFolder: true,
	}
	rt := int64(3725 * 10_000_000)
	trackWMA := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: lib.ID, ParentID: album.ID, Extra: sqlite.ItemExtra{AlbumID: album.ID},
		Type: "Audio", MediaType: "Audio", Name: "Rare WMA", Container: "wma", Path: wmaPath, RuntimeTicks: &rt,
	}
	clipAVI := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: lib.ID, ParentID: "", // empty ParentID exercises library root fallback in browseDirectChildren!
		Type: "Movie", MediaType: "Video", Name: "Classic AVI", Container: "avi", Path: aviPath, RuntimeTicks: &rt,
	}
	photoItem := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: lib.ID, ParentID: lib.ID,
		Type: "Photo", MediaType: "Photo", Name: "Sunset", Container: "jpg", Path: imgPath,
	}
	strmItem := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: lib.ID, ParentID: lib.ID,
		Type: "Movie", MediaType: "Video", Name: "Cloud Stream", Container: "mkv", Path: strmPath, IsStrm: true,
	}
	for _, it := range []*sqlite.ItemRecord{album, trackWMA, clipAVI, photoItem, strmItem} {
		if err := libRepo.UpsertItem(ctx, it); err != nil {
			t.Fatal(err)
		}
	}
	_ = libRepo.ReplaceItemImages(ctx, album.ID, []sqlite.ImageRecord{
		{ItemID: album.ID, ImageType: "Primary", ImageIndex: 0, Path: imgPath, Tag: "albtag1"},
	})

	boxSet, _ := libRepo.CreateCollection(ctx, "My BoxSet", "", []string{clipAVI.ID, trackWMA.ID})
	pubPl, _ := libRepo.CreatePlaylist(ctx, "Public Mix", "Audio", "u1", []string{trackWMA.ID, clipAVI.ID})
	_, _ = store.Writer.ExecContext(ctx,
		`INSERT INTO server_config (key, value_json, updated_at) VALUES (?, ?, ?)`,
		"item_visibility_"+guid.Normalize(pubPl.ID), `{"IsPublic":true}`, time.Now().UTC().Unix(),
	)

	// 1. Direct browseMetadata & browseDirectChildren wrappers + BoxSet/Playlist children sorting
	metaRoot, err := h.browseMetadata(ctx, "0", "http://192.168.1.5:8096", "srv-1")
	if err != nil || !strings.Contains(metaRoot, "object.container.storageFolder") {
		t.Fatalf("browseMetadata(0) failed: %v %s", err, metaRoot)
	}
	metaTrack, err := h.browseMetadata(ctx, trackWMA.ID, "http://192.168.1.5:8096", "srv-1")
	if err != nil || !strings.Contains(metaTrack, "albtag1") || !strings.Contains(metaTrack, "transcode=1") {
		t.Fatalf("expected albumArt fallback and transcode=1 fallback res on WMA track, got: %s (err=%v)", metaTrack, err)
	}
	metaAVI, err := h.browseMetadata(ctx, clipAVI.ID, "http://192.168.1.5:8096", "srv-1")
	if err != nil || !strings.Contains(metaAVI, "video/x-msvideo") || !strings.Contains(metaAVI, "video/mp2t") {
		t.Fatalf("expected AVI primary + MPEGTS fallback res, got: %s", metaAVI)
	}
	metaPhoto, err := h.browseMetadata(ctx, photoItem.ID, "http://192.168.1.5:8096", "srv-1")
	if err != nil || !strings.Contains(metaPhoto, "object.item.imageItem.photo") {
		t.Fatalf("expected photo DIDL class, got: %s", metaPhoto)
	}

	bsChildren, bsTotal, err := h.browseDirectChildren(ctx, boxSet.ID, 0, 10, "-dc:title", "http://192.168.1.5:8096", "srv-1")
	if err != nil || bsTotal != 2 || len(bsChildren) != 2 || !strings.Contains(bsChildren[0], "Rare WMA") {
		t.Fatalf("BoxSet browseDirectChildren descending failed: total=%d err=%v children=%v", bsTotal, err, bsChildren)
	}
	plChildren, plTotal, err := h.browseDirectChildren(ctx, pubPl.ID, 0, 1, "+dc:title", "http://192.168.1.5:8096", "srv-1")
	if err != nil || plTotal != 2 || len(plChildren) != 1 || !strings.Contains(plChildren[0], "Classic AVI") {
		t.Fatalf("Playlist browseDirectChildren ascending page 1 failed: total=%d len=%d", plTotal, len(plChildren))
	}

	// Empty sub-folder library triggers library_id fallback in browseDirectChildren
	emptyLib, _ := libRepo.CreateLibrary(ctx, "OrphanParentLib", "movies", []string{libDir})
	orphanItem := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: emptyLib.ID, ParentID: "",
		Type: "Movie", MediaType: "Video", Name: "Rootless Movie", Container: "mp4", Path: aviPath,
	}
	_ = libRepo.UpsertItem(ctx, orphanItem)
	orpChildren, orpTotal, err := h.browseDirectChildren(ctx, emptyLib.ID, 0, 10, "+dc:date", "http://192.168.1.5:8096", "srv-1")
	if err != nil || orpTotal != 1 || len(orpChildren) != 1 {
		t.Fatalf("expected library_id fallback to return 1 item, got %d err=%v", orpTotal, err)
	}

	// 2. parseSearchCriteria & parseSortCriteria full table
	for _, cls := range []string{
		`upnp:class = "object.item.videoItem.movie"`,
		`upnp:class = "object.item.videoItem.videoBroadcast"`,
		`upnp:class derivedfrom "object.item.videoItem"`,
		`upnp:class derivedfrom "object.item.audioItem"`,
		`upnp:class derivedfrom "object.item.imageItem"`,
		`upnp:class = "object.container.album.musicAlbum"`,
		`upnp:class derivedfrom "object.container"`,
	} {
		if _, types := parseSearchCriteria(cls); len(types) == 0 {
			t.Fatalf("parseSearchCriteria(%q) returned empty types", cls)
		}
	}
	if sb, dir := parseSortCriteria("-dc:date"); len(sb) == 0 || dir != "Descending" {
		t.Fatalf("parseSortCriteria(-dc:date) = %v %s", sb, dir)
	}

	// 3. ConnectionManager & ContentDirectory capabilities & faults
	for _, tc := range []struct {
		endpoint string
		svc      string
		action   string
		wantCode int
		wantSub  string
	}{
		{"/Dlna/srv-1/contentdirectory/control", contentDirectoryServiceType, "GetSystemUpdateID", 200, "<Id>"},
		{"/Dlna/srv-1/contentdirectory/control", contentDirectoryServiceType, "GetSearchCapabilities", 200, "dc:title"},
		{"/Dlna/srv-1/contentdirectory/control", contentDirectoryServiceType, "GetSortCapabilities", 200, "dc:date"},
		{"/Dlna/srv-1/contentdirectory/control", contentDirectoryServiceType, "UnknownAction", 500, "401"},
		{"/Dlna/srv-1/connectionmanager/control", connectionManagerServiceType, "GetProtocolInfo", 200, "http-get:*:video/mp4:*"},
		{"/Dlna/srv-1/connectionmanager/control", connectionManagerServiceType, "GetCurrentConnectionIDs", 200, "<ConnectionIDs>0</ConnectionIDs>"},
		{"/Dlna/srv-1/connectionmanager/control", connectionManagerServiceType, "Bogus", 500, "401"},
	} {
		body := soapEnvelope(tc.svc, tc.action, "")
		r := httptest.NewRequest(http.MethodPost, tc.endpoint, strings.NewReader(body))
		r.RemoteAddr = "192.168.1.15:40000"
		// Omit SOAPACTION header on half to exercise XML body action extraction in parseSOAPAction!
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		if w.Code != tc.wantCode || !strings.Contains(w.Body.String(), tc.wantSub) {
			t.Fatalf("%s %s got %d: %s", tc.endpoint, tc.action, w.Code, w.Body.String())
		}
	}

	// 4. Signed DLNA Stream: custom profile container check -> live transcode (video + audio + HEAD + 429) & STRM reverse proxy
	mp4OnlyProfile := DlnaProfile{
		Id:   "mp4-only-tv",
		Name: "MP4 Only TV",
		DirectPlayProfiles: []map[string]any{
			{"Type": "Video", "Container": "mp4"},
			{"Type": "Audio", "Container": "mp3"},
		},
	}
	pBytes, _ := json.Marshal(mp4OnlyProfile)
	rCreate := httptest.NewRequest(http.MethodPost, "/Dlna/Profiles", bytes.NewReader(pBytes))
	wCreate := httptest.NewRecorder()
	mux.ServeHTTP(wCreate, rCreate)
	if wCreate.Code != http.StatusOK {
		t.Fatalf("create mp4-only profile failed: %d", wCreate.Code)
	}

	exp := time.Now().UTC().Add(time.Hour).Unix()
	expStr := strconv.FormatInt(exp, 10)

	// AVI with ?profile=mp4-only-tv triggers automatic transcoding to video/mp2t
	sigAVI := h.signStreamTokenForMode(clipAVI.ID, exp, true)
	rStreamV := httptest.NewRequest(http.MethodGet, "/Dlna/srv-1/stream/"+clipAVI.ID+"?exp="+expStr+"&sig="+sigAVI+"&profile=mp4-only-tv", nil)
	rStreamV.RemoteAddr = "192.168.1.20:50000"
	wStreamV := httptest.NewRecorder()
	mux.ServeHTTP(wStreamV, rStreamV)
	if wStreamV.Code != http.StatusOK || wStreamV.Header().Get("Content-Type") != "video/mp2t" || wStreamV.Body.String() != "FAKE-DLNA-MPEGTS-DATA" {
		t.Fatalf("DLNA video transcode failed: %d ct=%s body=%q", wStreamV.Code, wStreamV.Header().Get("Content-Type"), wStreamV.Body.String())
	}

	// WMA audio with transcode=1 (HEAD and GET)
	sigWMA := h.signStreamTokenForMode(trackWMA.ID, exp, true)
	rHeadA := httptest.NewRequest(http.MethodHead, "/Dlna/srv-1/stream/"+trackWMA.ID+"?exp="+expStr+"&sig="+sigWMA+"&transcode=1", nil)
	rHeadA.RemoteAddr = "192.168.1.20:50000"
	wHeadA := httptest.NewRecorder()
	mux.ServeHTTP(wHeadA, rHeadA)
	if wHeadA.Code != http.StatusOK || wHeadA.Header().Get("Content-Type") != "audio/mpeg" || wHeadA.Body.Len() != 0 {
		t.Fatalf("DLNA audio HEAD transcode failed: %d ct=%s", wHeadA.Code, wHeadA.Header().Get("Content-Type"))
	}
	rGetA := httptest.NewRequest(http.MethodGet, "/Dlna/srv-1/stream/"+trackWMA.ID+"?exp="+expStr+"&sig="+sigWMA+"&transcode=true", nil)
	rGetA.RemoteAddr = "192.168.1.20:50000"
	wGetA := httptest.NewRecorder()
	mux.ServeHTTP(wGetA, rGetA)
	if wGetA.Code != http.StatusOK || wGetA.Body.String() != "FAKE-DLNA-MPEGTS-DATA" {
		t.Fatalf("DLNA audio GET transcode failed: %d body=%q", wGetA.Code, wGetA.Body.String())
	}

	// STRM reverse-proxy with custom headers & Range
	h.SetStreamResolverWithHeaders(func(ctx context.Context, rawPath string) (*plugins.StreamResolveResult, error) {
		return &plugins.StreamResolveResult{
			URL:     "https://93.184.216.34/stream/video.mkv",
			Headers: map[string]string{"Referer": "https://cloud.example/", "Cookie": "sid=1"},
		}, nil
	})
	h.SetProxyClient(&http.Client{
		Transport: roundTripFuncDLNA(func(r *http.Request) (*http.Response, error) {
			hdr := make(http.Header)
			hdr.Set("Accept-Ranges", "bytes")
			return &http.Response{
				StatusCode: http.StatusPartialContent,
				Header:     hdr,
				Body:       io.NopCloser(strings.NewReader("STRM-MKV-CHUNK")),
			}, nil
		}),
	})
	sigStrm := h.signStreamToken(strmItem.ID, exp)
	rStrm := httptest.NewRequest(http.MethodGet, "/Dlna/srv-1/stream/"+strmItem.ID+"?exp="+expStr+"&sig="+sigStrm, nil)
	rStrm.RemoteAddr = "192.168.1.20:50000"
	rStrm.Header.Set("Range", "bytes=0-13")
	rStrm.Header.Set("If-Range", `"v1"`)
	wStrm := httptest.NewRecorder()
	mux.ServeHTTP(wStrm, rStrm)
	if wStrm.Code != http.StatusPartialContent || wStrm.Header().Get("Content-Type") != "video/x-matroska" || wStrm.Body.String() != "STRM-MKV-CHUNK" {
		t.Fatalf("DLNA STRM proxy failed: %d ct=%s body=%q", wStrm.Code, wStrm.Header().Get("Content-Type"), wStrm.Body.String())
	}

	if _, p, err := h.resolveDLNAMediaPath(ctx, clipAVI.ID); err != nil || p != aviPath {
		t.Fatalf("resolveDLNAMediaPath failed: %q %v", p, err)
	}

	// 5. GENA SUBSCRIBE, renewal, BumpSystemUpdate NOTIFY dispatch, UNSUBSCRIBE, and icons/access
	var notifyHits atomic.Int32
	notifyDone := make(chan string, 2)
	h.SetNotifyClient(&http.Client{
		Transport: roundTripFuncDLNA(func(r *http.Request) (*http.Response, error) {
			b, _ := io.ReadAll(r.Body)
			notifyHits.Add(1)
			select {
			case notifyDone <- r.Method + ":" + r.Header.Get("NT") + ":" + string(b):
			default:
			}
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("OK"))}, nil
		}),
	})

	rSub := httptest.NewRequest("SUBSCRIBE", "/Dlna/srv-1/contentdirectory/events", nil)
	rSub.RemoteAddr = "192.168.1.88:50000"
	rSub.Header.Set("CALLBACK", "<http://192.168.1.88:9090/notify>")
	rSub.Header.Set("TIMEOUT", "Second-infinite")
	wSub := httptest.NewRecorder()
	mux.ServeHTTP(wSub, rSub)
	if wSub.Code != http.StatusOK || wSub.Header().Get("TIMEOUT") != "Second-86400" {
		t.Fatalf("SUBSCRIBE failed: %d timeout=%s", wSub.Code, wSub.Header().Get("TIMEOUT"))
	}
	sid := wSub.Header().Get("SID")

	// Renewal with clamp to minSubTimeout (Second-5 -> Second-60)
	rRenew := httptest.NewRequest("SUBSCRIBE", "/Dlna/srv-1/contentdirectory/events", nil)
	rRenew.RemoteAddr = "192.168.1.88:50000"
	rRenew.Header.Set("SID", sid)
	rRenew.Header.Set("TIMEOUT", "Second-5")
	wRenew := httptest.NewRecorder()
	mux.ServeHTTP(wRenew, rRenew)
	if wRenew.Code != http.StatusOK || wRenew.Header().Get("TIMEOUT") != "Second-60" {
		t.Fatalf("SUBSCRIBE renewal failed: %d timeout=%s", wRenew.Code, wRenew.Header().Get("TIMEOUT"))
	}

	// Renewal on wrong service -> 412
	rWrongSvc := httptest.NewRequest("SUBSCRIBE", "/Dlna/srv-1/connectionmanager/events", nil)
	rWrongSvc.RemoteAddr = "192.168.1.88:50000"
	rWrongSvc.Header.Set("SID", sid)
	wWrongSvc := httptest.NewRecorder()
	mux.ServeHTTP(wWrongSvc, rWrongSvc)
	if wWrongSvc.Code != http.StatusPreconditionFailed {
		t.Fatalf("expected 412 renewing on wrong service, got %d", wWrongSvc.Code)
	}

	// BumpSystemUpdate dispatches NOTIFY to subscriber
	newUpdateID := h.BumpSystemUpdate()
	select {
	case msg := <-notifyDone:
		if !strings.Contains(msg, "NOTIFY:upnp:event:") || !strings.Contains(msg, fmt.Sprintf("<SystemUpdateID>%d</SystemUpdateID>", newUpdateID)) {
			t.Fatalf("unexpected GENA NOTIFY message: %s", msg)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for GENA NOTIFY")
	}

	// Unsubscribe missing SID (412), unknown SID (412), valid SID (200)
	for _, hdrSID := range []string{"", "uuid:missing"} {
		ru := httptest.NewRequest("UNSUBSCRIBE", "/Dlna/srv-1/contentdirectory/events", nil)
		ru.RemoteAddr = "192.168.1.88:50000"
		if hdrSID != "" {
			ru.Header.Set("SID", hdrSID)
		}
		wu := httptest.NewRecorder()
		mux.ServeHTTP(wu, ru)
		if wu.Code != http.StatusPreconditionFailed {
			t.Fatalf("expected 412 for SID %q, got %d", hdrSID, wu.Code)
		}
	}
	rUnsub := httptest.NewRequest("UNSUBSCRIBE", "/Dlna/srv-1/contentdirectory/events", nil)
	rUnsub.RemoteAddr = "192.168.1.88:50000"
	rUnsub.Header.Set("SID", sid)
	wUnsub := httptest.NewRecorder()
	mux.ServeHTTP(wUnsub, rUnsub)
	if wUnsub.Code != http.StatusOK || h.ActiveSubscriptionCount() != 0 {
		t.Fatalf("UNSUBSCRIBE failed: code=%d count=%d", wUnsub.Code, h.ActiveSubscriptionCount())
	}

	// Icons (2-segment & 3-segment, SVG, PNG, HEAD, 404), TLS baseURL, EnableDLNA=false, WAN IP 403, helper functions
	for _, p := range []string{"/Dlna/icons/logo.svg", "/Dlna/srv-1/icons/logo120.png"} {
		ri := httptest.NewRequest(http.MethodGet, p, nil)
		ri.RemoteAddr = "192.168.1.88:50000"
		wi := httptest.NewRecorder()
		mux.ServeHTTP(wi, ri)
		if wi.Code != http.StatusOK {
			t.Fatalf("GET %s failed: %d", p, wi.Code)
		}
	}
	rIcon404 := httptest.NewRequest(http.MethodGet, "/Dlna/icons/missing.bmp", nil)
	rIcon404.RemoteAddr = "192.168.1.88:50000"
	wIcon404 := httptest.NewRecorder()
	mux.ServeHTTP(wIcon404, rIcon404)
	if wIcon404.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unsupported icon ext, got %d", wIcon404.Code)
	}

	rTLS := httptest.NewRequest(http.MethodGet, "/Dlna/srv-1/description.xml", nil)
	rTLS.TLS = &tls.ConnectionState{}
	rTLS.Host = ""
	if gotBase := h.baseURL(rTLS); gotBase != "https://127.0.0.1:8096" {
		t.Fatalf("unexpected TLS empty-host baseURL: %q", gotBase)
	}
	if normalizeDLNARateIP("2001:db8:abcd:1234::5") != "2001:db8:abcd:1234::/64" {
		t.Fatalf("unexpected IPv6 rate IP normalization: %s", normalizeDLNARateIP("2001:db8:abcd:1234::5"))
	}
	if validateDLNALANCallbackAddr(netip.MustParseAddr("127.0.0.1")) == nil ||
		validateDLNALANCallbackAddr(netip.MustParseAddr("8.8.8.8")) == nil ||
		validateDLNALANCallbackAddr(netip.MustParseAddr("192.168.1.50")) != nil {
		t.Fatalf("validateDLNALANCallbackAddr unexpected result")
	}
	for _, ext := range []string{".mp4", ".mkv", ".webm", ".mov", ".avi", ".ts", ".mp3", ".flac", ".m4a", ".aac", ".ogg", ".wav", ".jpg", ".png", ".webp", ".bin"} {
		if dlnaMediaContentType("f"+ext) == "" {
			t.Fatalf("empty dlnaMediaContentType for %s", ext)
		}
	}

	// Additional Search, BrowseDirectChildren, Transcode, Callback dialer, and SOAP rate-limit coverage
	lanClient := newDLNALANCallbackClient(0)
	if _, dialErr := lanClient.Get("http://127.0.0.1:1/cb"); dialErr == nil {
		t.Fatalf("expected LAN callback client to reject loopback address")
	}
	if _, ok := parseRemoteAddr(""); ok {
		t.Fatalf("expected false for empty remote addr")
	}
	if _, ok := parseRemoteAddr("bad-host"); ok {
		t.Fatalf("expected false for invalid remote addr")
	}

	// SimultaneousStreamLimit 429 on DLNA transcode & STRM transcode branch
	h.cfg.SimultaneousStreamLimit = 1
	relHold, ok := mgr.TryAcquireStreamSlot(1, "")
	if !ok {
		t.Fatal("failed to acquire hold slot")
	}
	rLim := httptest.NewRequest(http.MethodGet, "/Dlna/srv-1/stream/"+clipAVI.ID+"?exp="+expStr+"&sig="+sigAVI+"&transcode=1", nil)
	rLim.RemoteAddr = "192.168.1.20:50000"
	wLim := httptest.NewRecorder()
	mux.ServeHTTP(wLim, rLim)
	if wLim.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 when SimultaneousStreamLimit reached, got %d", wLim.Code)
	}
	relHold()
	h.cfg.SimultaneousStreamLimit = 0

	// STRM with transcode=1 exercises loopback proxy + HTTP ffmpeg args in streamDLNATranscode
	h.SetStreamResolverWithHeaders(func(ctx context.Context, rawPath string) (*plugins.StreamResolveResult, error) {
		return &plugins.StreamResolveResult{URL: "http://192.168.1.250:65530/live.ts"}, nil
	})
	sigStrmTC := h.signStreamTokenForMode(strmItem.ID, exp, true)
	rStrmTC := httptest.NewRequest(http.MethodGet, "/Dlna/srv-1/stream/"+strmItem.ID+"?exp="+expStr+"&sig="+sigStrmTC+"&transcode=1", nil)
	rStrmTC.RemoteAddr = "192.168.1.20:50000"
	wStrmTC := httptest.NewRecorder()
	mux.ServeHTTP(wStrmTC, rStrmTC)
	if wStrmTC.Code != http.StatusOK {
		t.Fatalf("STRM transcode=1 expected 200 with fake ffmpeg, got %d %s", wStrmTC.Code, wStrmTC.Body.String())
	}

	// profileSupportsContainer fallback branches: unknown profile -> DefaultProfile, wildcard container
	if !h.profileSupportsContainer("nonexistent-prof", clipAVI, filepath.Join(libDir, "v.mp4")) {
		t.Fatalf("expected DefaultProfile to support mp4")
	}
	h.mu.Lock()
	h.profiles["wild-prof"] = DlnaProfile{Id: "wild-prof", Name: "Wild", DirectPlayProfiles: []map[string]any{{"Type": "Video", "Container": "*"}}}
	h.profiles["empty-dp"] = DlnaProfile{Id: "empty-dp", Name: "EmptyDP"}
	h.mu.Unlock()
	if !h.profileSupportsContainer("wild-prof", clipAVI, aviPath) || !h.profileSupportsContainer("empty-dp", clipAVI, "") {
		t.Fatalf("expected wildcard and empty DirectPlayProfiles to return true")
	}

	// Search & BrowseDirectChildren policy edge cases: denyAll, empty EnabledFolders, disallowed container, pagination
	uRepo2 := sqlite.NewUserRepo(store)
	h.SetUserRepo(uRepo2)
	noFolderUser := &sqlite.UserRecord{
		ID:     guid.New(),
		Name:   "nofolders",
		Policy: dto.UserPolicy{EnableAllFolders: false, EnabledFolders: []string{}},
		Config: sqlite.DefaultUserConfig(),
	}
	_ = uRepo2.CreateUser(ctx, noFolderUser)
	h.SetDefaultUserID(noFolderUser.ID)

	dummyReq := httptest.NewRequest(http.MethodPost, "/Dlna/srv-1/contentdirectory/control", nil)
	dummyReq.RemoteAddr = "192.168.1.20:50000"

	if res, sErr := h.handleSearch(ctx, dummyReq, map[string]string{"ContainerID": "0", "SearchCriteria": "*"}); sErr != nil || !strings.Contains(res, "<TotalMatches>0</TotalMatches>") {
		t.Fatalf("expected 0 matches for empty EnabledFolders Search, got %s (%v)", res, sErr)
	}
	if res, sErr := h.handleSearch(ctx, dummyReq, map[string]string{"ContainerID": lib.ID, "SearchCriteria": "*"}); sErr != nil || !strings.Contains(res, "<TotalMatches>0</TotalMatches>") {
		t.Fatalf("expected 0 matches for disallowed library Search, got %s (%v)", res, sErr)
	}
	if res, sErr := h.handleSearch(ctx, dummyReq, map[string]string{"ContainerID": clipAVI.ID, "SearchCriteria": "*"}); sErr != nil || !strings.Contains(res, "<TotalMatches>0</TotalMatches>") {
		t.Fatalf("expected 0 matches for disallowed item container Search, got %s (%v)", res, sErr)
	}
	if _, _, bErr := h.browseDirectChildren(ctx, lib.ID, 0, 10, "", "http://127.0.0.1:8096", "srv-1"); bErr == nil {
		t.Fatalf("expected error browsing disallowed library")
	}
	if children, tot, bErr := h.browseDirectChildren(ctx, album.ID, 0, 10, "", "http://127.0.0.1:8096", "srv-1"); bErr == nil && (len(children) != 0 || tot != 0) {
		t.Fatalf("expected disallowed album browse error or empty")
	}

	// Missing DefaultUserID -> denyAll in handleSearch and browseDirectChildren
	h.SetDefaultUserID("missing-uid-999")
	if res, sErr := h.handleSearch(ctx, dummyReq, map[string]string{"ContainerID": "0", "SearchCriteria": "*"}); sErr != nil || !strings.Contains(res, "<TotalMatches>0</TotalMatches>") {
		t.Fatalf("expected 0 matches under denyAll Search, got %s (%v)", res, sErr)
	}
	if entries, tot, bErr := h.browseDirectChildren(ctx, "0", 0, 10, "", "http://127.0.0.1:8096", "srv-1"); bErr != nil || len(entries) != 0 || tot != 0 {
		t.Fatalf("expected empty browseDirectChildren under denyAll")
	}
	h.SetDefaultUserID("")
	h.SetUserRepo(nil)

	// Invalid XML on ConnectionManager -> 500 SOAP Fault (401) & SOAP per-IP rate limit -> 429
	rBadXML := httptest.NewRequest(http.MethodPost, "/Dlna/srv-1/connectionmanager/control", strings.NewReader("<broken"))
	rBadXML.RemoteAddr = "192.168.1.99:40000"
	wBadXML := httptest.NewRecorder()
	mux.ServeHTTP(wBadXML, rBadXML)
	if wBadXML.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 SOAP fault for invalid XML on ConnectionManager, got %d", wBadXML.Code)
	}
	for i := 0; i < maxDLNASOAPPerIPPerMin; i++ {
		rr := httptest.NewRequest(http.MethodPost, "/Dlna/srv-1/connectionmanager/control", strings.NewReader(soapEnvelope(connectionManagerServiceType, "GetCurrentConnectionIDs", "")))
		rr.RemoteAddr = "192.168.1.222:40000"
		ww := httptest.NewRecorder()
		mux.ServeHTTP(ww, rr)
	}
	rRateOver := httptest.NewRequest(http.MethodPost, "/Dlna/srv-1/connectionmanager/control", strings.NewReader(soapEnvelope(connectionManagerServiceType, "GetCurrentConnectionIDs", "")))
	rRateOver.RemoteAddr = "192.168.1.222:40001"
	wRateOver := httptest.NewRecorder()
	mux.ServeHTTP(wRateOver, rRateOver)
	if wRateOver.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 after exceeding maxDLNASOAPPerIPPerMin, got %d", wRateOver.Code)
	}

	// SeriesPrimaryImageTag fallback in buildDIDLAlbumArtXML + stream 403/404 + proxy HEAD/502 + update profile 404/default
	artXML := h.buildDIDLAlbumArtXML(dto.BaseItemDto{Id: "ep1", SeriesId: "ser1", SeriesPrimaryImageTag: "sertag"}, "http://192.168.1.5:8096")
	if !strings.Contains(artXML, "ser1") || !strings.Contains(artXML, "sertag") {
		t.Fatalf("expected SeriesPrimaryImageTag fallback in buildDIDLAlbumArtXML, got %s", artXML)
	}
	rNoSig := httptest.NewRequest(http.MethodGet, "/Dlna/srv-1/stream/"+clipAVI.ID, nil)
	rNoSig.RemoteAddr = "192.168.1.20:50000"
	wNoSig := httptest.NewRecorder()
	mux.ServeHTTP(wNoSig, rNoSig)
	if wNoSig.Code != http.StatusForbidden {
		t.Fatalf("expected 403 without stream signature, got %d", wNoSig.Code)
	}
	sigMissing := h.signStreamToken("missing-item-id", exp)
	rMissItem := httptest.NewRequest(http.MethodGet, "/Dlna/srv-1/stream/missing-item-id?exp="+expStr+"&sig="+sigMissing, nil)
	rMissItem.RemoteAddr = "192.168.1.20:50000"
	wMissItem := httptest.NewRecorder()
	mux.ServeHTTP(wMissItem, rMissItem)
	if wMissItem.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for missing stream item, got %d", wMissItem.Code)
	}
	for _, updTarget := range []string{"/Dlna/Profiles/default", "/Dlna/Profiles/unknown-id"} {
		ru := httptest.NewRequest(http.MethodPost, updTarget, strings.NewReader(`{"Name":"X"}`))
		wu := httptest.NewRecorder()
		mux.ServeHTTP(wu, ru)
		if wu.Code != http.StatusBadRequest && wu.Code != http.StatusNotFound {
			t.Fatalf("POST %s got unexpected %d", updTarget, wu.Code)
		}
	}

	// Proxy upstream failure -> 502, empty description friendlyName/uuid fallbacks, nil cfgSnapshot
	h.SetProxyClient(&http.Client{
		Transport: roundTripFuncDLNA(func(r *http.Request) (*http.Response, error) {
			return nil, fmt.Errorf("upstream network down")
		}),
	})
	wProxyErr := httptest.NewRecorder()
	h.proxyDLNARemoteStream(wProxyErr, httptest.NewRequest(http.MethodGet, "/", nil), "https://93.184.216.34/stream/video.mkv", nil)
	if wProxyErr.Code != http.StatusBadGateway {
		t.Fatalf("expected 502 on upstream proxy error, got %d", wProxyErr.Code)
	}
	wDescFallback := httptest.NewRecorder()
	h.serveDescriptionXML(wDescFallback, httptest.NewRequest(http.MethodGet, "/Dlna//description.xml", nil), "")
	if wDescFallback.Code != http.StatusOK {
		t.Fatalf("serveDescriptionXML empty uuid failed: %d", wDescFallback.Code)
	}
	_ = buildDescriptionXML("", "u-1", "http://127.0.0.1:8096")
	_ = (&Handler{}).cfgSnapshot()

	_ = New(nil, nil, nil)
	h.cfg.EnableDLNA = false
	rDis := httptest.NewRequest(http.MethodGet, "/Dlna/srv-1/description.xml", nil)
	rDis.RemoteAddr = "192.168.1.88:50000"
	wDis := httptest.NewRecorder()
	mux.ServeHTTP(wDis, rDis)
	if wDis.Code != http.StatusForbidden {
		t.Fatalf("expected 403 when EnableDLNA=false, got %d", wDis.Code)
	}
}
