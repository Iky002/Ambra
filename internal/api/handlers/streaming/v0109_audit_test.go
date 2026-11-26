package streaming

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/ambra-media/ambra/internal/api/middleware"
	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/core/auth"
	"github.com/ambra-media/ambra/internal/core/imaging"
	"github.com/ambra-media/ambra/internal/core/playback"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/plugins"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestV0109Audit_LibraryCoverPolicyTagAndPlaybackInfoCacheControl(t *testing.T) {
	tmp := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmp, "streaming_v0109.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	ctx := context.Background()
	libRepo := sqlite.NewLibraryRepo(store)
	userRepo := sqlite.NewUserRepo(store)
	cfg := config.Default(tmp)
	authSvc := auth.NewService(userRepo, cfg)
	engine := playback.New(libRepo, plugins.NewRegistry(), cfg)
	imgSvc := imaging.New(libRepo).WithCacheDir(filepath.Join(tmp, "imgcache"))

	libA, err := libRepo.CreateLibrary(ctx, "Movies A", "movies", []string{tmp})
	if err != nil {
		t.Fatal(err)
	}
	libB, err := libRepo.CreateLibrary(ctx, "Movies B", "movies", []string{tmp})
	if err != nil {
		t.Fatal(err)
	}

	// Create 3 distinct image files so we can tell which movie cover was returned
	rImgPath := filepath.Join(tmp, "r_backdrop.jpg")
	nsfwImgPath := filepath.Join(tmp, "nsfw_backdrop.jpg")
	gImgPath := filepath.Join(tmp, "g_backdrop.jpg")
	bImgPath := filepath.Join(tmp, "b_backdrop.jpg")
	rPayload := []byte("R-RATED-IMAGE-BYTES")
	nsfwPayload := []byte("NSFW-TAG-IMAGE-BYTES")
	gPayload := []byte("G-RATED-SAFE-IMAGE-BYTES")
	bPayload := []byte("LIB-B-IMAGE-BYTES")
	_ = os.WriteFile(rImgPath, rPayload, 0o644)
	_ = os.WriteFile(nsfwImgPath, nsfwPayload, 0o644)
	_ = os.WriteFile(gImgPath, gPayload, 0o644)
	_ = os.WriteFile(bImgPath, bPayload, 0o644)

	videoFile := filepath.Join(tmp, "safe.mp4")
	_ = os.WriteFile(videoFile, []byte("fake-mp4"), 0o644)

	movieR := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: libA.ID, ParentID: libA.ID,
		Type: "Movie", MediaType: "Video", Name: "AAA Adult R", SortName: "aaa adult r",
		OfficialRating: "R", Path: videoFile,
	}
	movieNSFW := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: libA.ID, ParentID: libA.ID,
		Type: "Movie", MediaType: "Video", Name: "AAB Tagged NSFW", SortName: "aab tagged nsfw",
		OfficialRating: "G", Path: videoFile,
	}
	movieG := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: libA.ID, ParentID: libA.ID,
		Type: "Movie", MediaType: "Video", Name: "ZZZ Family Safe", SortName: "zzz family safe",
		OfficialRating: "G", Path: videoFile,
	}
	movieB := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: libB.ID, ParentID: libB.ID,
		Type: "Movie", MediaType: "Video", Name: "Secret LibB", SortName: "secret libb",
		OfficialRating: "G", Path: videoFile,
	}
	for _, it := range []*sqlite.ItemRecord{movieR, movieNSFW, movieG, movieB} {
		if err := libRepo.UpsertItem(ctx, it); err != nil {
			t.Fatal(err)
		}
	}
	_ = libRepo.ReplaceItemTaxonomies(ctx, movieNSFW.ID, "tag", []string{"nsfw"})

	_ = libRepo.ReplaceItemImages(ctx, movieR.ID, []sqlite.ImageRecord{{ItemID: movieR.ID, ImageType: "Backdrop", ImageIndex: 0, Path: rImgPath, Tag: "tag-r"}})
	_ = libRepo.ReplaceItemImages(ctx, movieNSFW.ID, []sqlite.ImageRecord{{ItemID: movieNSFW.ID, ImageType: "Backdrop", ImageIndex: 0, Path: nsfwImgPath, Tag: "tag-nsfw"}})
	_ = libRepo.ReplaceItemImages(ctx, movieG.ID, []sqlite.ImageRecord{{ItemID: movieG.ID, ImageType: "Backdrop", ImageIndex: 0, Path: gImgPath, Tag: "tag-g"}})
	_ = libRepo.ReplaceItemImages(ctx, movieB.ID, []sqlite.ImageRecord{{ItemID: movieB.ID, ImageType: "Backdrop", ImageIndex: 0, Path: bImgPath, Tag: "tag-b"}})

	kid, err := authSvc.CreateUser(ctx, "kid", "kidpass", false)
	if err != nil {
		t.Fatal(err)
	}
	maxPG := 5
	kidPolicy := sqlite.DefaultUserPolicy(false)
	kidPolicy.EnableAllFolders = false
	kidPolicy.EnabledFolders = []string{libA.ID}
	kidPolicy.MaxParentalRating = &maxPG
	kidPolicy.BlockedTags = []string{"nsfw"}
	if err := userRepo.UpdateUserPolicy(ctx, kid.ID, kidPolicy); err != nil {
		t.Fatal(err)
	}
	kid.Policy = kidPolicy

	h := New(libRepo, engine, imgSvc)
	h.SetUserRepo(userRepo)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux, AuthFuncs{})

	kidAC := &middleware.AuthContext{User: kid, Token: "kid-token-secret"}

	// 1. [M-09] Authenticated kid requesting libA cover without tag gets ONLY G-rated cover (never R or NSFW)
	req := httptest.NewRequest(http.MethodGet, "/Items/"+libA.ID+"/Images/Primary", nil)
	req = req.WithContext(middleware.WithAuthContext(req.Context(), kidAC))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !bytes.Equal(rec.Body.Bytes(), gPayload) {
		t.Fatalf("expected G-rated cover for kid, got status=%d body=%q", rec.Code, rec.Body.String())
	}

	// 2. [M-09] Authenticated kid explicitly passing ?tag=tag-r or ?tag=tag-nsfw still never gets blocked cover
	for _, blockedTag := range []string{"tag-r", "tag-nsfw"} {
		reqBlocked := httptest.NewRequest(http.MethodGet, "/Items/"+libA.ID+"/Images/Primary?tag="+blockedTag, nil)
		reqBlocked = reqBlocked.WithContext(middleware.WithAuthContext(reqBlocked.Context(), kidAC))
		recBlocked := httptest.NewRecorder()
		mux.ServeHTTP(recBlocked, reqBlocked)
		if recBlocked.Code != http.StatusOK || !bytes.Equal(recBlocked.Body.Bytes(), gPayload) {
			t.Fatalf("kid with tag=%s must never receive blocked cover, got status=%d body=%q", blockedTag, recBlocked.Code, recBlocked.Body.String())
		}
	}

	// 3. [M-09] Authenticated kid requesting cover of disallowed libB -> 404
	reqLibB := httptest.NewRequest(http.MethodGet, "/Items/"+libB.ID+"/Images/Primary", nil)
	reqLibB = reqLibB.WithContext(middleware.WithAuthContext(reqLibB.Context(), kidAC))
	recLibB := httptest.NewRecorder()
	mux.ServeHTTP(recLibB, reqLibB)
	if recLibB.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for disallowed library cover, got %d", recLibB.Code)
	}

	// 4. [M-09] Unauthenticated request with ?tag=tag-g prioritizes matching child cover instead of R-rated first item
	reqTagG := httptest.NewRequest(http.MethodGet, "/Items/"+libA.ID+"/Images/Primary?tag=tag-g", nil)
	recTagG := httptest.NewRecorder()
	mux.ServeHTTP(recTagG, reqTagG)
	if recTagG.Code != http.StatusOK || !bytes.Equal(recTagG.Body.Bytes(), gPayload) {
		t.Fatalf("expected tag-g matched cover, got status=%d body=%q", recTagG.Code, recTagG.Body.String())
	}

	// 5. [M-14] PlaybackInfo sets Cache-Control: private, no-store
	reqPI := httptest.NewRequest(http.MethodPost, "/Items/"+movieG.ID+"/PlaybackInfo", bytes.NewBufferString(`{}`))
	reqPI = reqPI.WithContext(middleware.WithAuthContext(reqPI.Context(), kidAC))
	recPI := httptest.NewRecorder()
	mux.ServeHTTP(recPI, reqPI)
	if recPI.Code != http.StatusOK {
		t.Fatalf("PlaybackInfo expected 200, got %d: %s", recPI.Code, recPI.Body.String())
	}
	if cc := recPI.Header().Get("Cache-Control"); cc != "private, no-store" {
		t.Fatalf("expected PlaybackInfo Cache-Control=private, no-store, got %q", cc)
	}
}
