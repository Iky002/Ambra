package library

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ambra-media/ambra/internal/api/dto"
	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/core/scanner"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/plugins"
	"github.com/ambra-media/ambra/internal/plugins/nfo"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestLibraryHandlerAllEndpointsExtra(t *testing.T) {
	tmp := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmp, "lib_extra.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	repo := sqlite.NewLibraryRepo(store)
	reg := plugins.NewRegistry()
	reg.Register(nfo.New())
	cfg := config.Default(filepath.Join(tmp, "data"))
	sc := scanner.New(repo, reg, nil, cfg)

	// Nil cfg serverID check
	hNilCfg := New(repo, sc, nil)
	if hNilCfg.serverID() != "" {
		t.Fatalf("expected empty serverID for nil cfg")
	}

	userID := guid.New()
	userRepo := sqlite.NewUserRepo(store)
	if err := userRepo.CreateUser(context.Background(), &sqlite.UserRecord{
		ID:           userID,
		Name:         "testuser",
		PasswordHash: "hash",
	}); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	h := New(repo, sc, cfg)
	h.RegisterRoutes(mux, AuthFuncs{
		UserIDFromCtx: func(context.Context) string { return userID },
	})

	// Seed a movie and a TV series/season/episode
	ctx := context.Background()
	mediaDir := filepath.Join(tmp, "tv")
	epDir := filepath.Join(mediaDir, "ShowX (2023)", "Season 01")
	_ = os.MkdirAll(epDir, 0o755)
	_ = os.WriteFile(filepath.Join(epDir, "ShowX.S01E01.mkv"), []byte("ep1"), 0o644)
	_ = os.WriteFile(filepath.Join(mediaDir, "ShowX (2023)", "tvshow.nfo"), []byte(`<tvshow><title>ShowX</title><year>2023</year><genre>Sci-Fi</genre><studio>HBO</studio><director>DirX</director></tvshow>`), 0o644)

	// POST /Library/VirtualFolders error (missing name)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/Library/VirtualFolders", bytes.NewReader([]byte(`{}`))))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing name, got %d", rec.Code)
	}

	// POST /Library/VirtualFolders via LibraryOptions.PathInfos + refreshLibrary=true
	vfBody, _ := json.Marshal(map[string]any{
		"Name":           "TVLib",
		"CollectionType": "tvshows",
		"LibraryOptions": map[string]any{
			"PathInfos": []map[string]string{{"Path": mediaDir}},
		},
	})
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/Library/VirtualFolders?refreshLibrary=true", bytes.NewReader(vfBody)))
	if rec.Code != http.StatusOK {
		t.Fatalf("POST VirtualFolders failed: %d %s", rec.Code, rec.Body.String())
	}
	var vf virtualFolderDTO
	_ = json.Unmarshal(rec.Body.Bytes(), &vf)

	// Wait for async refreshLibrary scan to finish, then ensure items exist via sync scan
	sc.WaitIdle(ctx)
	if _, err := sc.ScanLibrary(ctx, vf.ItemId); err != nil {
		t.Fatal(err)
	}

	// GET /Library/VirtualFolders
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/Library/VirtualFolders", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET VirtualFolders status=%d", rec.Code)
	}

	// GET /Items with full query options (Recursive, Years, Filters, UserId)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/Items?Recursive=true&Years=2023&IncludeItemTypes=Series&UserId="+userID, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /Items status=%d body=%s", rec.Code, rec.Body.String())
	}
	var seriesRes dto.QueryResult[dto.BaseItemDto]
	_ = json.Unmarshal(rec.Body.Bytes(), &seriesRes)
	if len(seriesRes.Items) != 1 {
		t.Fatalf("expected 1 series, got %+v", seriesRes)
	}
	seriesID := seriesRes.Items[0].Id

	// GET /Items with Filters=IsPlayed,IsUnplayed,IsFavorite,IsResumable (via ctx userID)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/Items?Recursive=true&Filters=IsPlayed,IsUnplayed,IsFavorite,IsResumable", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /Items filters status=%d", rec.Code)
	}

	// GET /Shows/{Id}/Seasons & Episodes & NextUp
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/Shows/"+seriesID+"/Seasons", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET Seasons status=%d", rec.Code)
	}

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/Shows/"+seriesID+"/Episodes", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET Episodes status=%d", rec.Code)
	}
	var epRes dto.QueryResult[dto.BaseItemDto]
	_ = json.Unmarshal(rec.Body.Bytes(), &epRes)
	if len(epRes.Items) != 1 {
		t.Fatalf("expected 1 episode, got %+v", epRes)
	}
	epID := epRes.Items[0].Id

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/Shows/NextUp?SeriesId="+seriesID, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET NextUp status=%d", rec.Code)
	}

	// GET /Users/{UserId}/Items/Latest & Resume & GetItem (200 and 404)
	for _, path := range []string{
		"/Users/" + userID + "/Items/Latest",
		"/Users/" + userID + "/Items/Resume",
		"/Users/" + userID + "/Items/" + epID,
		"/Genres", "/Studios", "/Persons", "/Years",
	} {
		rec = httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s status=%d body=%s", path, rec.Code, rec.Body.String())
		}
	}

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/Items/"+guid.New(), nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for missing item, got %d", rec.Code)
	}

	// UserData: Played / Unplayed / Favorite / Unfavorite
	for _, tc := range [][2]string{
		{http.MethodPost, "/Users/" + userID + "/PlayedItems/" + epID},
		{http.MethodDelete, "/Users/" + userID + "/PlayedItems/" + epID},
		{http.MethodPost, "/Users/" + userID + "/FavoriteItems/" + epID},
		{http.MethodDelete, "/Users/" + userID + "/FavoriteItems/" + epID},
	} {
		rec = httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(tc[0], tc[1], nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("%s %s status=%d body=%s", tc[0], tc[1], rec.Code, rec.Body.String())
		}
	}

	// Refresh & Media Updated (wait for async scans to finish before deleting/closing DB)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/Library/Refresh?ParentId="+vf.ItemId, nil))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("POST /Library/Refresh status=%d", rec.Code)
	}
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/Library/Media/Updated", nil))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("POST /Library/Media/Updated status=%d", rec.Code)
	}
	time.Sleep(20 * time.Millisecond)
	sc.WaitIdle(ctx)

	// DELETE /Library/VirtualFolders: missing param (400), by name (204), by id (204)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/Library/VirtualFolders", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing id/name, got %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/Library/VirtualFolders?name=NonExistent", nil))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204 for DeleteLibraryByName, got %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/Library/VirtualFolders?id="+vf.ItemId, nil))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204 for DeleteLibrary by id, got %d", rec.Code)
	}

	// Close DB and verify 500 error handling across key endpoints
	_ = store.Close()
	for _, tc := range [][2]string{
		{http.MethodGet, "/Users/" + userID + "/Views"},
		{http.MethodGet, "/Items"},
		{http.MethodGet, "/Users/" + userID + "/Items/Latest"},
		{http.MethodGet, "/Users/" + userID + "/Items/Resume"},
		{http.MethodGet, "/Items/" + epID},
		{http.MethodGet, "/Items/Counts"},
		{http.MethodGet, "/Shows/" + seriesID + "/Seasons"},
		{http.MethodGet, "/Shows/" + seriesID + "/Episodes"},
		{http.MethodGet, "/Shows/NextUp"},
		{http.MethodGet, "/Search/Hints?SearchTerm=x"},
		{http.MethodGet, "/Genres"},
		{http.MethodPost, "/Users/" + userID + "/PlayedItems/" + epID},
		{http.MethodDelete, "/Users/" + userID + "/PlayedItems/" + epID},
		{http.MethodPost, "/Users/" + userID + "/FavoriteItems/" + epID},
		{http.MethodDelete, "/Users/" + userID + "/FavoriteItems/" + epID},
		{http.MethodGet, "/Library/VirtualFolders"},
		{http.MethodDelete, "/Library/VirtualFolders?id=abc"},
		{http.MethodDelete, "/Library/VirtualFolders?name=abc"},
	} {
		rec = httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(tc[0], tc[1], nil))
		if rec.Code != http.StatusInternalServerError {
			t.Errorf("closed DB %s %s expected 500, got %d", tc[0], tc[1], rec.Code)
		}
	}
}
