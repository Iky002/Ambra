package compatmedia

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ambra-media/ambra/internal/api/httpx"
	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/core/imaging"
	"github.com/ambra-media/ambra/internal/core/scanner"
	"github.com/ambra-media/ambra/internal/pkg/eventbus"
	"github.com/ambra-media/ambra/internal/plugins"
	"github.com/ambra-media/ambra/internal/plugins/tmdb"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func makeValidPNG(t *testing.T, w, h int, c color.RGBA) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("png.Encode: %v", err)
	}
	return buf.Bytes()
}

func newTMDBTestEnv(t *testing.T) (*testEnv, *httptest.Server, []byte, []byte) {
	t.Helper()
	posterPNG := makeValidPNG(t, 20, 30, color.RGBA{R: 200, G: 40, B: 40, A: 255})
	backdropPNG := makeValidPNG(t, 40, 22, color.RGBA{R: 40, G: 80, B: 200, A: 255})

	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/search/movie":
			_, _ = w.Write([]byte(`{"results":[
				{"id":438631,"title":"沙丘","original_title":"Dune","overview":"保罗的旅程","release_date":"2021-10-22","vote_average":7.8,"poster_path":"/img/poster.png","backdrop_path":"/img/backdrop.png"},
				{"id":693134,"title":"沙丘2","original_title":"Dune: Part Two","overview":"续集","release_date":"2024-02-27","vote_average":8.3,"poster_path":"/img/poster2.png","backdrop_path":"/img/backdrop2.png"}
			]}`))
		case "/search/tv":
			_, _ = w.Write([]byte(`{"results":[
				{"id":1396,"name":"绝命毒师","original_name":"Breaking Bad","overview":"化学老师","first_air_date":"2008-01-20","vote_average":8.9,"poster_path":"/img/poster.png","backdrop_path":"/img/backdrop.png"}
			]}`))
		case "/movie/438631":
			_, _ = w.Write([]byte(`{
				"id":438631,"title":"沙丘","original_title":"Dune","overview":"完整中文简介","tagline":"恐惧是思维的杀手",
				"release_date":"2021-10-22","vote_average":7.9,"poster_path":"/img/poster.png","backdrop_path":"/img/backdrop.png",
				"genres":[{"name":"科幻"},{"name":"冒险"}],
				"production_companies":[{"name":"Legendary Pictures"}],
				"credits":{
					"cast":[{"name":"提莫西·查拉梅","character":"Paul","profile_path":"/img/poster.png"}],
					"crew":[{"name":"丹尼斯·维伦纽瓦","job":"Director","profile_path":""}]
				},
				"external_ids":{"imdb_id":"tt1160419"}
			}`))
		case "/movie/438631/images":
			_, _ = w.Write([]byte(`{
				"posters":[
					{"file_path":"/img/poster.png","width":1000,"height":1500,"iso_639_1":"zh","vote_average":5.5,"vote_count":10},
					{"file_path":"/img/poster2.png","width":1000,"height":1500,"iso_639_1":"en","vote_average":5.1,"vote_count":4}
				],
				"backdrops":[
					{"file_path":"/img/backdrop.png","width":1920,"height":1080,"iso_639_1":"en","vote_average":6.0,"vote_count":8}
				]
			}`))
		case "/img/poster.png", "/img/poster2.png":
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(posterPNG)
		case "/img/backdrop.png", "/img/backdrop2.png":
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(backdropPNG)
		case "/not-an-image":
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte("<html>hello</html>"))
		case "/empty":
			w.Header().Set("Content-Type", "image/png")
			w.WriteHeader(http.StatusOK)
		case "/octet-png":
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(posterPNG)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)

	tmpDir := t.TempDir()
	cfg, err := config.Load(filepath.Join(tmpDir, "data"))
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	store, err := sqlite.Open(cfg.DBPath())
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	repo := sqlite.NewLibraryRepo(store)
	reg := plugins.NewRegistry()
	tp := tmdb.New("test-key", "zh-CN")
	tp.SetHTTPClient(srv.Client())
	_ = tp.Configure(map[string]any{
		"base_url":       srv.URL,
		"image_base_url": srv.URL,
		"api_key":        "test-key",
	})
	reg.Register(tp)

	bus := eventbus.New()
	sc := scanner.New(repo, reg, bus, cfg)
	sc.SetProber(scanner.NewDisabledProber())
	sc.SetHTTPClient(srv.Client())
	img := imaging.New(repo).WithCacheDir(cfg.ImageCacheDir())

	h := New(repo, sc, img, reg, store, cfg)
	h.SetHTTPClient(srv.Client())
	r := h.BuildRouter(AuthFuncs{
		UserIDFromCtx: func(context.Context) string { return "user-1" },
	})

	env := &testEnv{
		store:   store,
		repo:    repo,
		scanner: sc,
		imaging: img,
		reg:     reg,
		cfg:     cfg,
		handler: h,
		router:  r,
		tmpDir:  tmpDir,
	}
	return env, srv, posterPNG, backdropPNG
}

func TestRemoteSearchMultiCandidatesAndApply(t *testing.T) {
	env, _, _, _ := newTMDBTestEnv(t)
	ctx := context.Background()

	lib, err := env.repo.CreateLibrary(ctx, "Movies", "movies", []string{env.tmpDir})
	if err != nil {
		t.Fatal(err)
	}
	movie := &sqlite.ItemRecord{
		ID:        "mov-apply",
		LibraryID: lib.ID,
		ParentID:  lib.ID,
		Type:      "Movie",
		MediaType: "Video",
		Name:      "Unknown File",
		SortName:  "unknown file",
	}
	_ = env.repo.UpsertItem(ctx, movie)

	// 1) POST /Items/RemoteSearch/Movie -> returns 2 real TMDB candidates
	body := `{"SearchInfo":{"Name":"Dune","Year":2021}}`
	req := httptest.NewRequest(http.MethodPost, "/Items/RemoteSearch/Movie", strings.NewReader(body))
	rec := httptest.NewRecorder()
	env.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("RemoteSearch/Movie status=%d body=%s", rec.Code, rec.Body.String())
	}
	var cands []remoteSearchResult
	if err := json.Unmarshal(rec.Body.Bytes(), &cands); err != nil {
		t.Fatal(err)
	}
	if len(cands) != 2 || cands[0].Name != "沙丘" || cands[0].ProviderIds["tmdb"] != "438631" || cands[1].Name != "沙丘2" {
		t.Fatalf("unexpected candidates: %+v", cands)
	}

	// 2) POST /Items/RemoteSearch/Series with SearchProviderName="TheMovieDb"
	sReq := httptest.NewRequest(http.MethodPost, "/Items/RemoteSearch/Series", strings.NewReader(`{"SearchInfo":{"Name":"Breaking Bad"},"SearchProviderName":"TheMovieDb"}`))
	sRec := httptest.NewRecorder()
	env.router.ServeHTTP(sRec, sReq)
	if sRec.Code != http.StatusOK {
		t.Fatalf("RemoteSearch/Series status=%d", sRec.Code)
	}
	var sCands []remoteSearchResult
	_ = json.Unmarshal(sRec.Body.Bytes(), &sCands)
	if len(sCands) != 1 || sCands[0].ProviderIds["tmdb"] != "1396" {
		t.Fatalf("unexpected series candidates: %+v", sCands)
	}

	// 3) POST /Items/RemoteSearch/Apply/{Id} -> updates metadata, fetches cast/genres/studios, downloads poster & backdrop
	applyPayload, _ := json.Marshal(cands[0])
	aReq := httptest.NewRequest(http.MethodPost, "/Items/RemoteSearch/Apply/mov-apply?ReplaceAllImages=true", bytes.NewReader(applyPayload))
	aRec := httptest.NewRecorder()
	env.router.ServeHTTP(aRec, aReq)
	if aRec.Code != http.StatusNoContent {
		t.Fatalf("RemoteSearch/Apply status=%d body=%s", aRec.Code, aRec.Body.String())
	}

	updated, err := env.repo.GetItemRecordByID(ctx, "mov-apply")
	if err != nil {
		t.Fatal(err)
	}
	if updated.Name != "沙丘" || updated.Extra.Overview != "完整中文简介" || updated.Extra.ProviderIDs["imdb"] != "tt1160419" {
		t.Fatalf("expected full refreshed metadata after apply, got %+v", updated)
	}
	people, err := env.repo.GetPeopleByItemID(ctx, "mov-apply")
	if err != nil || len(people) < 2 {
		t.Fatalf("expected cast & director persisted after apply, got %+v err=%v", people, err)
	}
	pImg, err := env.repo.GetImageByItemAndType(ctx, "mov-apply", "Primary", 0)
	if err != nil || pImg == nil {
		t.Fatalf("expected primary poster downloaded after apply: %v", err)
	}
	bImg, err := env.repo.GetImageByItemAndType(ctx, "mov-apply", "Backdrop", 0)
	if err != nil || bImg == nil {
		t.Fatalf("expected backdrop downloaded after apply: %v", err)
	}

	// 4) Apply on non-existent item -> 404
	nfReq := httptest.NewRequest(http.MethodPost, "/Items/RemoteSearch/Apply/missing-id", bytes.NewReader(applyPayload))
	nfRec := httptest.NewRecorder()
	env.router.ServeHTTP(nfRec, nfReq)
	if nfRec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for missing item apply, got %d", nfRec.Code)
	}
}

func TestItemRefreshAndManualUpdatePreservesFTSPeople(t *testing.T) {
	env, _, _, _ := newTMDBTestEnv(t)
	ctx := context.Background()

	lib, err := env.repo.CreateLibrary(ctx, "Movies", "movies", []string{env.tmpDir})
	if err != nil {
		t.Fatal(err)
	}
	movie := &sqlite.ItemRecord{
		ID:        "mov-ref",
		LibraryID: lib.ID,
		ParentID:  lib.ID,
		Type:      "Movie",
		MediaType: "Video",
		Name:      "Dune",
		SortName:  "dune",
		Extra: sqlite.ItemExtra{
			Overview:    "old overview",
			ProviderIDs: map[string]string{"tmdb": "438631"},
		},
	}
	_ = env.repo.UpsertItem(ctx, movie)

	// POST /Items/{Id}/Refresh?ReplaceAllMetadata=true&ReplaceAllImages=true
	req := httptest.NewRequest(http.MethodPost, "/Items/mov-ref/Refresh?ReplaceAllMetadata=true&ReplaceAllImages=true", nil)
	rec := httptest.NewRecorder()
	env.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("Refresh status=%d body=%s", rec.Code, rec.Body.String())
	}
	got, _ := env.repo.GetItemRecordByID(ctx, "mov-ref")
	if got.Name != "沙丘" || got.Extra.Overview != "完整中文简介" {
		t.Fatalf("expected refreshed metadata, got %+v", got)
	}

	// Refresh missing item -> 404
	nfReq := httptest.NewRequest(http.MethodPost, "/Items/no-such-item/Refresh?MetadataRefreshMode=FullRefresh", nil)
	nfRec := httptest.NewRecorder()
	env.router.ServeHTTP(nfRec, nfReq)
	if nfRec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 on missing item refresh, got %d", nfRec.Code)
	}

	// P2-4: POST /Items/{ItemId} manual edit must preserve existing people in search_fts
	editReq := httptest.NewRequest(http.MethodPost, "/Items/mov-ref", strings.NewReader(`{"Name":"沙丘 特别版","Overview":"手动编辑简介"}`))
	editRec := httptest.NewRecorder()
	env.router.ServeHTTP(editRec, editReq)
	if editRec.Code != http.StatusNoContent {
		t.Fatalf("UpdateItem status=%d", editRec.Code)
	}
	var peopleTokens string
	if err := env.store.Reader.QueryRowContext(ctx, `SELECT people_names FROM item_search_fts WHERE item_id = ?`, "movref").Scan(&peopleTokens); err != nil {
		t.Fatalf("query item_search_fts: %v", err)
	}
	if !strings.Contains(peopleTokens, "提莫西") {
		t.Fatalf("expected existing people preserved in item_search_fts after manual update, got %q", peopleTokens)
	}
}

func TestRemoteImagesListDownloadSetURLAndProxy(t *testing.T) {
	env, srv, posterPNG, backdropPNG := newTMDBTestEnv(t)
	ctx := context.Background()

	lib, err := env.repo.CreateLibrary(ctx, "Movies", "movies", []string{env.tmpDir})
	if err != nil {
		t.Fatal(err)
	}
	movie := &sqlite.ItemRecord{
		ID:        "mov-img",
		LibraryID: lib.ID,
		ParentID:  lib.ID,
		Type:      "Movie",
		MediaType: "Video",
		Name:      "沙丘",
		Extra: sqlite.ItemExtra{
			ProviderIDs: map[string]string{"Tmdb": "438631"},
		},
	}
	_ = env.repo.UpsertItem(ctx, movie)

	// 1) GET /Items/{Id}/RemoteImages (all)
	req := httptest.NewRequest(http.MethodGet, "/Items/mov-img/RemoteImages", nil)
	rec := httptest.NewRecorder()
	env.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("RemoteImages status=%d body=%s", rec.Code, rec.Body.String())
	}
	var imgResp struct {
		Images           []tmdb.RemoteImageCandidate `json:"Images"`
		TotalRecordCount int                         `json:"TotalRecordCount"`
		Providers        []string                    `json:"Providers"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &imgResp); err != nil {
		t.Fatal(err)
	}
	if imgResp.TotalRecordCount != 3 || len(imgResp.Images) != 3 {
		t.Fatalf("expected 3 remote images, got total=%d len=%d", imgResp.TotalRecordCount, len(imgResp.Images))
	}

	// Filter by Type=Primary + pagination
	fReq := httptest.NewRequest(http.MethodGet, "/Items/mov-img/RemoteImages?Type=Primary&StartIndex=1&Limit=1", nil)
	fRec := httptest.NewRecorder()
	env.router.ServeHTTP(fRec, fReq)
	_ = json.Unmarshal(fRec.Body.Bytes(), &imgResp)
	if imgResp.TotalRecordCount != 2 || len(imgResp.Images) != 1 || imgResp.Images[0].Type != "Primary" {
		t.Fatalf("unexpected filtered remote images: total=%d imgs=%+v", imgResp.TotalRecordCount, imgResp.Images)
	}

	// Missing item -> 404
	nfReq := httptest.NewRequest(http.MethodGet, "/Items/missing/RemoteImages", nil)
	nfRec := httptest.NewRecorder()
	env.router.ServeHTTP(nfRec, nfReq)
	if nfRec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for missing item RemoteImages, got %d", nfRec.Code)
	}

	// 2) POST /Items/{Id}/RemoteImages/Download via query params
	dlReq := httptest.NewRequest(http.MethodPost, "/Items/mov-img/RemoteImages/Download?Type=Primary&ImageUrl="+srv.URL+"/img/poster.png", nil)
	dlRec := httptest.NewRecorder()
	env.router.ServeHTTP(dlRec, dlReq)
	if dlRec.Code != http.StatusNoContent {
		t.Fatalf("RemoteImages/Download status=%d body=%s", dlRec.Code, dlRec.Body.String())
	}
	savedPrimary, err := env.repo.GetImageByItemAndType(ctx, "mov-img", "Primary", 0)
	if err != nil || savedPrimary == nil || savedPrimary.Width != 20 || savedPrimary.Height != 30 {
		t.Fatalf("expected downloaded primary image with 20x30, got %+v err=%v", savedPrimary, err)
	}
	diskBytes, err := os.ReadFile(savedPrimary.Path)
	if err != nil || !bytes.Equal(diskBytes, posterPNG) {
		t.Fatalf("expected saved file to match posterPNG")
	}

	// Download via JSON body (Backdrop)
	dlBody := `{"Type":"Backdrop","ImageUrl":"` + srv.URL + `/img/backdrop.png"}`
	dlReq2 := httptest.NewRequest(http.MethodPost, "/Items/mov-img/RemoteImages/Download", strings.NewReader(dlBody))
	dlRec2 := httptest.NewRecorder()
	env.router.ServeHTTP(dlRec2, dlReq2)
	if dlRec2.Code != http.StatusNoContent {
		t.Fatalf("RemoteImages/Download JSON status=%d body=%s", dlRec2.Code, dlRec2.Body.String())
	}
	savedBD, err := env.repo.GetImageByItemAndType(ctx, "mov-img", "Backdrop", 0)
	if err != nil || savedBD == nil || savedBD.Width != 40 || savedBD.Height != 22 {
		t.Fatalf("expected downloaded backdrop 40x22, got %+v err=%v", savedBD, err)
	}

	// Download error cases: invalid URL (400), non-image (502), remote 404 (502), missing item (404)
	for _, tc := range []struct {
		url  string
		want int
	}{
		{"/Items/mov-img/RemoteImages/Download?ImageUrl=ftp://bad", http.StatusBadRequest},
		{"/Items/mov-img/RemoteImages/Download?ImageUrl=" + srv.URL + "/not-an-image", http.StatusBadGateway},
		{"/Items/mov-img/RemoteImages/Download?ImageUrl=" + srv.URL + "/missing.png", http.StatusBadGateway},
		{"/Items/no-item/RemoteImages/Download?ImageUrl=" + srv.URL + "/img/poster.png", http.StatusNotFound},
	} {
		r := httptest.NewRequest(http.MethodPost, tc.url, nil)
		w := httptest.NewRecorder()
		env.router.ServeHTTP(w, r)
		if w.Code != tc.want {
			t.Fatalf("Download %s: expected %d, got %d", tc.url, tc.want, w.Code)
		}
	}

	// 3) POST /Items/{Id}/Images/{Type}/Url (index=0 alias) and /{Type}/{Index}/Url
	urlReq1 := httptest.NewRequest(http.MethodPost, "/Items/mov-img/Images/Primary/Url", strings.NewReader(`{"Url":"`+srv.URL+`/img/backdrop.png"}`))
	urlRec1 := httptest.NewRecorder()
	env.router.ServeHTTP(urlRec1, urlReq1)
	if urlRec1.Code != http.StatusNoContent {
		t.Fatalf("SetImageURL alias status=%d body=%s", urlRec1.Code, urlRec1.Body.String())
	}
	replacedPrimary, _ := env.repo.GetImageByItemAndType(ctx, "mov-img", "Primary", 0)
	if replacedPrimary == nil || replacedPrimary.Width != 40 {
		t.Fatalf("expected Primary replaced via /Images/Primary/Url, got %+v", replacedPrimary)
	}

	urlReq2 := httptest.NewRequest(http.MethodPost, "/Items/mov-img/Images/Backdrop/1/Url?Url="+srv.URL+"/img/backdrop.png", nil)
	urlRec2 := httptest.NewRecorder()
	env.router.ServeHTTP(urlRec2, urlReq2)
	if urlRec2.Code != http.StatusNoContent {
		t.Fatalf("SetImageURL indexed status=%d", urlRec2.Code)
	}
	bd1, _ := env.repo.GetImageByItemAndType(ctx, "mov-img", "Backdrop", 1)
	if bd1 == nil || bd1.Width != 40 {
		t.Fatalf("expected Backdrop index 1 saved, got %+v", bd1)
	}

	// SetImageURL error cases
	badURLReq := httptest.NewRequest(http.MethodPost, "/Items/mov-img/Images/Primary/Url", strings.NewReader(`{"Url":"not-a-url"}`))
	badURLRec := httptest.NewRecorder()
	env.router.ServeHTTP(badURLRec, badURLReq)
	if badURLRec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid SetImageURL, got %d", badURLRec.Code)
	}
	nfURLReq := httptest.NewRequest(http.MethodPost, "/Items/missing/Images/Primary/Url", strings.NewReader(`{"Url":"`+srv.URL+`/img/poster.png"}`))
	nfURLRec := httptest.NewRecorder()
	env.router.ServeHTTP(nfURLRec, nfURLReq)
	if nfURLRec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for missing item SetImageURL, got %d", nfURLRec.Code)
	}

	// 4) GET /Images/Remote & GET /Items/RemoteSearch/Image proxy
	for _, path := range []string{
		"/Images/Remote?imageUrl=" + srv.URL + "/img/backdrop.png",
		"/Items/RemoteSearch/Image?imageUrl=" + srv.URL + "/img/backdrop.png",
	} {
		pReq := httptest.NewRequest(http.MethodGet, path, nil)
		pRec := httptest.NewRecorder()
		env.router.ServeHTTP(pRec, pReq)
		if pRec.Code != http.StatusOK {
			t.Fatalf("proxy %s status=%d body=%s", path, pRec.Code, pRec.Body.String())
		}
		if pRec.Header().Get("Content-Type") != "image/png" || !bytes.Equal(pRec.Body.Bytes(), backdropPNG) {
			t.Fatalf("proxy %s unexpected content-type=%q len=%d", path, pRec.Header().Get("Content-Type"), pRec.Body.Len())
		}
	}

	// Proxy error cases: missing URL (400), non-image HTML (400), remote 404 (404), empty (502), octet-stream PNG (200)
	for _, tc := range []struct {
		path string
		want int
	}{
		{"/Images/Remote", http.StatusBadRequest},
		{"/Images/Remote?imageUrl=" + srv.URL + "/not-an-image", http.StatusBadRequest},
		{"/Images/Remote?imageUrl=" + srv.URL + "/missing.png", http.StatusNotFound},
		{"/Images/Remote?imageUrl=" + srv.URL + "/empty", http.StatusBadGateway},
		{"/Images/Remote?imageUrl=" + srv.URL + "/octet-png", http.StatusOK},
	} {
		pReq := httptest.NewRequest(http.MethodGet, tc.path, nil)
		pRec := httptest.NewRecorder()
		env.router.ServeHTTP(pRec, pReq)
		if pRec.Code != tc.want {
			t.Fatalf("proxy %s: expected %d, got %d", tc.path, tc.want, pRec.Code)
		}
	}

	// Additional edge cases: StartIndex beyond len, SetImageURL remote 404 -> 502, Download empty -> 502
	overReq := httptest.NewRequest(http.MethodGet, "/Items/mov-img/RemoteImages?StartIndex=99", nil)
	overRec := httptest.NewRecorder()
	env.router.ServeHTTP(overRec, overReq)
	if overRec.Code != http.StatusOK {
		t.Fatalf("RemoteImages StartIndex=99 status=%d", overRec.Code)
	}

	failURLReq := httptest.NewRequest(http.MethodPost, "/Items/mov-img/Images/Primary/Url", strings.NewReader(`{"Url":"`+srv.URL+`/missing.png"}`))
	failURLRec := httptest.NewRecorder()
	env.router.ServeHTTP(failURLRec, failURLReq)
	if failURLRec.Code != http.StatusBadGateway {
		t.Fatalf("expected 502 for SetImageURL 404, got %d", failURLRec.Code)
	}

	emptyDLReq := httptest.NewRequest(http.MethodPost, "/Items/mov-img/RemoteImages/Download?ImageUrl="+srv.URL+"/empty", nil)
	emptyDLRec := httptest.NewRecorder()
	env.router.ServeHTTP(emptyDLRec, emptyDLReq)
	if emptyDLRec.Code != http.StatusBadGateway {
		t.Fatalf("expected 502 for empty remote image, got %d", emptyDLRec.Code)
	}

	// RemoteSearch with ProviderIds (fallback to FetchMetadata) and non-matching SearchProviderName
	byIDReq := httptest.NewRequest(http.MethodPost, "/Items/RemoteSearch/Movie", strings.NewReader(`{"SearchInfo":{"ProviderIds":{"tmdb":"438631"}}}`))
	byIDRec := httptest.NewRecorder()
	env.router.ServeHTTP(byIDRec, byIDReq)
	if byIDRec.Code != http.StatusOK || !strings.Contains(byIDRec.Body.String(), "沙丘") {
		t.Fatalf("RemoteSearch by ProviderIds: %d %s", byIDRec.Code, byIDRec.Body.String())
	}

	skipReq := httptest.NewRequest(http.MethodPost, "/Items/RemoteSearch/Movie", strings.NewReader(`{"SearchInfo":{"Name":"Dune"},"SearchProviderName":"OtherProvider"}`))
	skipRec := httptest.NewRecorder()
	env.router.ServeHTTP(skipRec, skipReq)
	if skipRec.Code != http.StatusOK || strings.TrimSpace(skipRec.Body.String()) != "[]" {
		t.Fatalf("RemoteSearch skipped provider: %d %s", skipRec.Code, skipRec.Body.String())
	}

	// Unit checks for helpers & default HTTP client
	defaultH := &Handler{}
	if defaultH.getHTTPClient() == nil {
		t.Fatal("expected non-nil default HTTP client")
	}
	if lookupProviderID(nil, "tmdb") != "" || lookupProviderID(map[string]string{"tmdb": "1"}, "tmdb") != "1" || lookupProviderID(map[string]string{"Tmdb": "2"}, "tmdb") != "2" || lookupProviderID(map[string]string{"imdb": "tt1"}, "tmdb") != "" {
		t.Fatal("unexpected lookupProviderID behavior")
	}
	if len(ensureProviderName([]string{"TheMovieDb"}, "TheMovieDb")) != 1 || len(ensureProviderName([]string{"Other"}, "TheMovieDb")) != 2 {
		t.Fatal("unexpected ensureProviderName behavior")
	}
	if normalizeContentType("image/png; charset=utf-8") != "image/png" || normalizeContentType("invalid/type;=") == "" {
		t.Fatal("unexpected normalizeContentType behavior")
	}

	// Network error on proxy & download (closed server)
	closedSrv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	closedURL := closedSrv.URL + "/img.png"
	closedSrv.Close()

	netProxyReq := httptest.NewRequest(http.MethodGet, "/Images/Remote?imageUrl="+closedURL, nil)
	netProxyRec := httptest.NewRecorder()
	env.router.ServeHTTP(netProxyRec, netProxyReq)
	if netProxyRec.Code != http.StatusBadGateway {
		t.Fatalf("expected 502 on unreachable proxy URL, got %d", netProxyRec.Code)
	}

	netDLReq := httptest.NewRequest(http.MethodPost, "/Items/mov-img/RemoteImages/Download?ImageUrl="+closedURL, nil)
	netDLRec := httptest.NewRecorder()
	env.router.ServeHTTP(netDLRec, netDLReq)
	if netDLRec.Code != http.StatusBadGateway {
		t.Fatalf("expected 502 on unreachable download URL, got %d", netDLRec.Code)
	}

	// 404 cases on update & delete item
	nfUpdReq := httptest.NewRequest(http.MethodPost, "/Items/nonexistent-id", strings.NewReader(`{"Name":"X"}`))
	nfUpdRec := httptest.NewRecorder()
	env.router.ServeHTTP(nfUpdRec, nfUpdReq)
	if nfUpdRec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 on update missing item, got %d", nfUpdRec.Code)
	}
	nfDelReq := httptest.NewRequest(http.MethodDelete, "/Items/nonexistent-id", nil)
	nfDelRec := httptest.NewRecorder()
	env.router.ServeHTTP(nfDelRec, nfDelReq)
	if nfDelRec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 on delete missing item, got %d", nfDelRec.Code)
	}

	// Upload missing item (404) & empty body upload (204)
	nfUpReq := httptest.NewRequest(http.MethodPost, "/Items/nonexistent-id/Images/Primary", strings.NewReader("abc"))
	nfUpRec := httptest.NewRecorder()
	env.router.ServeHTTP(nfUpRec, nfUpReq)
	if nfUpRec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 on upload to missing item, got %d", nfUpRec.Code)
	}
	emptyUpReq := httptest.NewRequest(http.MethodPost, "/Items/mov-img/Images/Primary", nil)
	emptyUpRec := httptest.NewRecorder()
	env.router.ServeHTTP(emptyUpRec, emptyUpReq)
	if emptyUpRec.Code != http.StatusNoContent {
		t.Fatalf("expected 204 on empty image upload, got %d", emptyUpRec.Code)
	}

	// Cover resolveUserID via query param & nil-auth fallback
	ancReq := httptest.NewRequest(http.MethodGet, "/Items/mov-img/Ancestors?UserId=user-1", nil)
	ancRec := httptest.NewRecorder()
	env.router.ServeHTTP(ancRec, ancReq)
	if ancRec.Code != http.StatusOK {
		t.Fatalf("expected 200 on Ancestors, got %d", ancRec.Code)
	}
	simReq := httptest.NewRequest(http.MethodGet, "/Items/mov-img/Similar?Limit=5", nil)
	simRec := httptest.NewRecorder()
	env.router.ServeHTTP(simRec, simReq)
	if simRec.Code != http.StatusOK {
		t.Fatalf("expected 200 on Similar, got %d", simRec.Code)
	}

	// Cover imageExtForData / decodeImageBody / resolveUserID nil-fallback branches
	if imageExtForData(nil, "image/gif") != ".gif" || imageExtForData(nil, "image/webp") != ".webp" || imageExtForData(nil, "image/jpeg") != ".jpg" {
		t.Fatal("unexpected imageExtForData")
	}
	if len(decodeImageBody(nil, "")) != 0 || len(decodeImageBody([]byte("GIF89a"), "image/gif")) == 0 || len(decodeImageBody([]byte("not-base64!!!"), "image/jpeg")) == 0 {
		t.Fatal("unexpected decodeImageBody")
	}
	nilAuthH := &Handler{}
	if nilAuthH.resolveUserID(httptest.NewRequest(http.MethodGet, "/", nil), httpx.ParseQuery(httptest.NewRequest(http.MethodGet, "/", nil))) != "" {
		t.Fatal("expected empty userID")
	}
	if nilAuthH.serverID() != "" {
		t.Fatal("expected empty serverID")
	}
}
