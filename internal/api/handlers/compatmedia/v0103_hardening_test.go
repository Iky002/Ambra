package compatmedia

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ambra-media/ambra/internal/api/middleware"
	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/core/auth"
	"github.com/ambra-media/ambra/internal/core/imaging"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/plugins"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestCompatMediaV0103Hardening(t *testing.T) {
	tmp := t.TempDir()
	cfg, err := config.Load(filepath.Join(tmp, "data"))
	if err != nil {
		t.Fatal(err)
	}
	store, err := sqlite.Open(cfg.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	ctx := context.Background()
	repo := sqlite.NewLibraryRepo(store)
	userRepo := sqlite.NewUserRepo(store)
	authSvc := auth.NewService(userRepo, cfg)

	admin, _ := authSvc.CreateUser(ctx, "admin", "admin123", true)
	alice, _ := authSvc.CreateUser(ctx, "alice", "alice123", false)

	libSafe, _ := repo.CreateLibrary(ctx, "Safe", "movies", []string{filepath.Join(tmp, "safe-dir")})
	libSecret, _ := repo.CreateLibrary(ctx, "Secret", "movies", []string{filepath.Join(tmp, "secret-dir")})

	ap := alice.Policy
	ap.EnableAllFolders = false
	ap.EnabledFolders = []string{libSafe.ID}
	maxPG := 5
	ap.MaxParentalRating = &maxPG
	_ = userRepo.UpdateUserPolicy(ctx, alice.ID, ap)
	authSvc.InvalidateUserCache(alice.ID)

	safeMovie := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: libSafe.ID, ParentID: libSafe.ID,
		Type: "Movie", MediaType: "Video", Name: "Safe Movie", Path: "/secret/path/safe.mkv", OfficialRating: "G",
	}
	secretMovie := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: libSecret.ID, ParentID: libSecret.ID,
		Type: "Movie", MediaType: "Video", Name: "Secret Movie", Path: "/secret/path/secret.mkv", OfficialRating: "R",
	}
	_ = repo.UpsertItem(ctx, safeMovie)
	_ = repo.UpsertItem(ctx, secretMovie)
	_ = repo.ReplaceItemPeople(ctx, safeMovie.ID, []sqlite.PersonInput{{Name: "Safe Actor", Type: "Actor"}})
	_ = repo.ReplaceItemPeople(ctx, secretMovie.ID, []sqlite.PersonInput{{Name: "Hidden Actor", Type: "Actor"}})

	pngBytes := makeValidPNG(t, 12, 12, color.RGBA{R: 10, G: 200, B: 50, A: 255})
	var jpgBuf bytes.Buffer
	jImg := image.NewRGBA(image.Rect(0, 0, 10, 10))
	_ = jpeg.Encode(&jpgBuf, jImg, nil)
	jpgBytes := jpgBuf.Bytes()

	remoteSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/xss.svg":
			w.Header().Set("Content-Type", "image/svg+xml")
			_, _ = w.Write([]byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`))
		case "/ok.png":
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(pngBytes)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(remoteSrv.Close)

	h := New(repo, nil, imaging.New(repo), plugins.NewRegistry(), store, cfg)
	h.SetUserRepo(userRepo)
	h.SetHTTPClient(remoteSrv.Client())
	authMW := middleware.NewAuth(authSvc, nil)
	router := h.BuildRouter(AuthFuncs{
		RequireAuth:  authMW.RequireAuth,
		RequireAdmin: authMW.RequireAdmin,
		UserIDFromCtx: func(ctx context.Context) string {
			if ac, ok := middleware.AuthFromContext(ctx); ok && ac != nil {
				return ac.UserID()
			}
			return ""
		},
	})

	adminAuth, _ := authSvc.AuthenticateByName(ctx, "admin", "admin123", "", auth.ClientInfo{DeviceID: "d1"})
	aliceAuth, _ := authSvc.AuthenticateByName(ctx, "alice", "alice123", "", auth.ClientInfo{DeviceID: "d2"})

	call := func(tok, method, path string, body []byte, ct string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, bytes.NewReader(body))
		if tok != "" {
			req.Header.Set("X-Emby-Token", tok)
		}
		if ct != "" {
			req.Header.Set("Content-Type", ct)
		}
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}

	// 1. RemoteImageProxy blocks SVG XSS and sets nosniff + CSP sandbox on valid PNG
	rec := call(aliceAuth.AccessToken, http.MethodGet, "/Images/Remote?imageUrl="+remoteSrv.URL+"/xss.svg", nil, "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for SVG remote image proxy, got %d", rec.Code)
	}
	rec = call(aliceAuth.AccessToken, http.MethodGet, "/Images/Remote?imageUrl="+remoteSrv.URL+"/ok.png", nil, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for PNG remote image proxy, got %d", rec.Code)
	}
	if rec.Header().Get("X-Content-Type-Options") != "nosniff" || rec.Header().Get("Content-Security-Policy") != "sandbox" {
		t.Fatalf("missing security headers: X-Content-Type-Options=%q CSP=%q",
			rec.Header().Get("X-Content-Type-Options"), rec.Header().Get("Content-Security-Policy"))
	}

	// 2. Upload image replaces old extension file cleanly (.jpg -> .png)
	if rec := call(adminAuth.AccessToken, http.MethodPost, "/Items/"+safeMovie.ID+"/Images/Primary", jpgBytes, "image/jpeg"); rec.Code != http.StatusNoContent {
		t.Fatalf("upload jpg failed: %d", rec.Code)
	}
	img1, err := repo.GetImageByItemAndType(ctx, safeMovie.ID, "Primary", 0)
	if err != nil || img1 == nil {
		t.Fatalf("expected jpg image record: %v", err)
	}
	if rec := call(adminAuth.AccessToken, http.MethodPost, "/Items/"+safeMovie.ID+"/Images/Primary", pngBytes, "image/png"); rec.Code != http.StatusNoContent {
		t.Fatalf("upload png replacement failed: %d", rec.Code)
	}
	img2, err := repo.GetImageByItemAndType(ctx, safeMovie.ID, "Primary", 0)
	if err != nil || img2 == nil || img2.Path == img1.Path {
		t.Fatalf("expected new png path, old=%q new=%+v", img1.Path, img2)
	}
	if _, err := os.Stat(img1.Path); !os.IsNotExist(err) {
		t.Fatalf("expected old .jpg file %q to be deleted after .png overwrite, err=%v", img1.Path, err)
	}

	// 3. RemoteImages/Download increments Backdrop index (0, 1)
	if rec := call(adminAuth.AccessToken, http.MethodPost, "/Items/"+safeMovie.ID+"/RemoteImages/Download?Type=Backdrop&ImageUrl="+remoteSrv.URL+"/ok.png", nil, ""); rec.Code != http.StatusNoContent {
		t.Fatalf("backdrop 0 download failed: %d", rec.Code)
	}
	if rec := call(adminAuth.AccessToken, http.MethodPost, "/Items/"+safeMovie.ID+"/RemoteImages/Download?Type=Backdrop&ImageUrl="+remoteSrv.URL+"/ok.png", nil, ""); rec.Code != http.StatusNoContent {
		t.Fatalf("backdrop 1 download failed: %d", rec.Code)
	}
	if b0, err := repo.GetImageByItemAndType(ctx, safeMovie.ID, "Backdrop", 0); err != nil || b0 == nil {
		t.Fatalf("expected Backdrop index 0: %v", err)
	}
	if b1, err := repo.GetImageByItemAndType(ctx, safeMovie.ID, "Backdrop", 1); err != nil || b1 == nil {
		t.Fatalf("expected Backdrop index 1 (incremented): %v", err)
	}

	// 4. GET /Items/{Id}/Images hides physical Path for non-admin, shows for admin
	rec = call(aliceAuth.AccessToken, http.MethodGet, "/Items/"+safeMovie.ID+"/Images", nil, "")
	var aliceImgs []imageInfoDTO
	_ = json.Unmarshal(rec.Body.Bytes(), &aliceImgs)
	if len(aliceImgs) == 0 || aliceImgs[0].Path != "" {
		t.Fatalf("expected non-admin /Images to hide Path, got %+v", aliceImgs)
	}
	rec = call(adminAuth.AccessToken, http.MethodGet, "/Items/"+safeMovie.ID+"/Images", nil, "")
	var adminImgs []imageInfoDTO
	_ = json.Unmarshal(rec.Body.Bytes(), &adminImgs)
	if len(adminImgs) == 0 || adminImgs[0].Path == "" {
		t.Fatalf("expected admin /Images to include Path (admin=%s), got %+v", admin.ID, adminImgs)
	}

	// 5. Person lookup enforces library RBAC + parental policy
	if rec := call(aliceAuth.AccessToken, http.MethodGet, "/Persons/Hidden%20Actor", nil, ""); rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for alice looking up person only in secret library, got %d", rec.Code)
	}
	if rec := call(aliceAuth.AccessToken, http.MethodGet, "/Persons/Safe%20Actor", nil, ""); rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for alice looking up person in safe library, got %d", rec.Code)
	}

	// 6. SelectableMediaFolders & VirtualFolders/Query hide physical paths for non-admin
	rec = call(aliceAuth.AccessToken, http.MethodGet, "/Library/SelectableMediaFolders", nil, "")
	if strings.Contains(rec.Body.String(), "safe-dir") {
		t.Fatalf("SelectableMediaFolders leaked physical path to non-admin: %s", rec.Body.String())
	}
	rec = call(aliceAuth.AccessToken, http.MethodGet, "/Library/VirtualFolders/Query", nil, "")
	if strings.Contains(rec.Body.String(), "safe-dir") {
		t.Fatalf("VirtualFolders/Query leaked physical path to non-admin: %s", rec.Body.String())
	}

	// 7. Resize dimension clamp <= 4096
	if got := clampImageDim(99999); got != 4096 {
		t.Fatalf("expected clampImageDim(99999)=4096, got %d", got)
	}
	if got := clampImageDim(-5); got != 0 {
		t.Fatalf("expected clampImageDim(-5)=0, got %d", got)
	}

	// 8. ChannelIds > 50 safely capped
	var ids []string
	for i := 0; i < 80; i++ {
		ids = append(ids, guid.New())
	}
	rec = call(aliceAuth.AccessToken, http.MethodGet, "/Channels/Items/Latest?ChannelIds="+strings.Join(ids, ","), nil, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("Channels/Items/Latest with 80 ChannelIds got %d", rec.Code)
	}
}
