package compatmedia

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/core/scanner"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestHandleUpdateItem_ClearFieldsLocksAndLocalNFOSync(t *testing.T) {
	ctx := context.Background()
	dataDir := t.TempDir()
	mediaRoot := t.TempDir()
	movieDir := filepath.Join(mediaRoot, "EditTest (2023)")
	_ = os.MkdirAll(movieDir, 0o755)
	moviePath := filepath.Join(movieDir, "EditTest (2023).mkv")
	_ = os.WriteFile(moviePath, []byte("mkv"), 0o644)

	store, err := sqlite.Open(filepath.Join(dataDir, "compat_audit.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	cfg := config.Default(dataDir)
	repo := sqlite.NewLibraryRepo(store)
	sc := scanner.New(repo, nil, nil, cfg)
	h := New(repo, sc, nil, nil, store, cfg)
	router := h.BuildRouter(AuthFuncs{})

	lib, err := repo.CreateLibrary(ctx, "Movies", "movies", []string{mediaRoot})
	if err != nil {
		t.Fatal(err)
	}

	itemID := guid.New()
	if err := repo.UpsertItem(ctx, &sqlite.ItemRecord{
		ID:             itemID,
		LibraryID:      lib.ID,
		ParentID:       lib.ID,
		Type:           "Movie",
		MediaType:      "Video",
		Name:           "Original Name",
		OriginalTitle:  "Old Original",
		OfficialRating: "PG-13",
		Path:           moviePath,
		Extra: sqlite.ItemExtra{
			Overview: "Old Overview",
		},
	}); err != nil {
		t.Fatal(err)
	}

	// Clear Overview, OriginalTitle, OfficialRating to "" and set LockData=true + LockedFields.
	payload := map[string]any{
		"Name":           "Edited Title",
		"OriginalTitle":  "",
		"Overview":       "",
		"OfficialRating": "",
		"LockData":       true,
		"LockedFields":   []string{"Overview", "Genres"},
		"Genres":         []string{"Sci-Fi"},
	}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/Items/"+itemID, bytes.NewReader(body))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("POST /Items/%s status=%d body=%s", itemID, rec.Code, rec.Body.String())
	}

	updated, err := repo.GetItemRecordByID(ctx, itemID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Name != "Edited Title" {
		t.Fatalf("Name = %q, want Edited Title", updated.Name)
	}
	if updated.OriginalTitle != "" || updated.Extra.Overview != "" || updated.OfficialRating != "" {
		t.Fatalf("expected cleared fields to be empty, got orig=%q overview=%q rating=%q",
			updated.OriginalTitle, updated.Extra.Overview, updated.OfficialRating)
	}
	if !updated.Extra.LockData || len(updated.Extra.LockedFields) != 2 {
		t.Fatalf("expected LockData=true and LockedFields=[Overview, Genres], got %+v", updated.Extra)
	}

	// Verify local NFO was synced to media folder.
	nfoPath := filepath.Join(movieDir, "EditTest (2023).nfo")
	nfoBytes, err := os.ReadFile(nfoPath)
	if err != nil {
		t.Fatalf("expected local NFO synced at %s: %v", nfoPath, err)
	}
	if !strings.Contains(string(nfoBytes), "<title>Edited Title</title>") || !strings.Contains(string(nfoBytes), "<genre>Sci-Fi</genre>") {
		t.Fatalf("unexpected NFO contents:\n%s", string(nfoBytes))
	}
}

func TestHandleUploadAndDownloadItemImage_SavesLocallyWhenEnabled(t *testing.T) {
	ctx := context.Background()
	dataDir := t.TempDir()
	mediaRoot := t.TempDir()
	movieDir := filepath.Join(mediaRoot, "ArtTest (2023)")
	_ = os.MkdirAll(movieDir, 0o755)
	moviePath := filepath.Join(movieDir, "ArtTest (2023).mkv")
	_ = os.WriteFile(moviePath, []byte("mkv"), 0o644)

	store, err := sqlite.Open(filepath.Join(dataDir, "compat_img.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	cfg := config.Default(dataDir)
	repo := sqlite.NewLibraryRepo(store)
	sc := scanner.New(repo, nil, nil, cfg)
	h := New(repo, sc, nil, nil, store, cfg)

	imgSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write([]byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 'J', 'F', 'I', 'F'})
	}))
	defer imgSrv.Close()
	h.SetHTTPClient(imgSrv.Client())
	router := h.BuildRouter(AuthFuncs{})

	lib, err := repo.CreateLibrary(ctx, "Movies", "movies", []string{mediaRoot})
	if err != nil {
		t.Fatal(err)
	}
	itemID := guid.New()
	if err := repo.UpsertItem(ctx, &sqlite.ItemRecord{
		ID:        itemID,
		LibraryID: lib.ID,
		ParentID:  lib.ID,
		Type:      "Movie",
		MediaType: "Video",
		Name:      "ArtTest",
		Path:      moviePath,
	}); err != nil {
		t.Fatal(err)
	}

	// 1. Upload Primary image -> saved as poster.png in movieDir
	pngMagic := []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A, 0x00}
	req := httptest.NewRequest(http.MethodPost, "/Items/"+itemID+"/Images/Primary", bytes.NewReader(pngMagic))
	req.Header.Set("Content-Type", "image/png")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("upload image status=%d body=%s", rec.Code, rec.Body.String())
	}
	wantPoster := filepath.Join(movieDir, "poster.png")
	if _, err := os.Stat(wantPoster); err != nil {
		t.Fatalf("expected uploaded poster saved locally at %s: %v", wantPoster, err)
	}

	// 2. Download remote Backdrop image -> saved as fanart.jpg in movieDir
	req2 := httptest.NewRequest(http.MethodPost, "/Items/"+itemID+"/RemoteImages/Download?Type=Backdrop&ImageUrl="+imgSrv.URL+"/bg.jpg", nil)
	rec2 := httptest.NewRecorder()
	router.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusNoContent {
		t.Fatalf("download remote image status=%d body=%s", rec2.Code, rec2.Body.String())
	}
	wantFanart := filepath.Join(movieDir, "fanart.jpg")
	if _, err := os.Stat(wantFanart); err != nil {
		t.Fatalf("expected remote backdrop saved locally at %s: %v", wantFanart, err)
	}
}
