package compatmedia

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/core/imaging"
	"github.com/ambra-media/ambra/internal/core/scanner"
	"github.com/ambra-media/ambra/internal/core/watcher"
	"github.com/ambra-media/ambra/internal/pkg/eventbus"
	"github.com/ambra-media/ambra/internal/plugins"
	"github.com/ambra-media/ambra/internal/pkg/netutil"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestImageTypeAllowlistAndPathTraversalBlocked(t *testing.T) {
	env, srv, _, _ := newTMDBTestEnv(t)
	ctx := context.Background()

	lib, err := env.repo.CreateLibrary(ctx, "Movies", "movies", []string{env.tmpDir})
	if err != nil {
		t.Fatal(err)
	}
	_ = env.repo.UpsertItem(ctx, &sqlite.ItemRecord{
		ID:        "mov-sec",
		LibraryID: lib.ID,
		ParentID:  lib.ID,
		Type:      "Movie",
		MediaType: "Video",
		Name:      "Security Test",
	})

	// 1. Disallowed image types rejected with 400 on Upload, SetImageURL, and RemoteImages/Download
	for _, badType := range []string{"invalid", "..%2Fevil", "script"} {
		req := httptest.NewRequest(http.MethodPost, "/Items/mov-sec/Images/"+badType, strings.NewReader("abc"))
		rec := httptest.NewRecorder()
		env.router.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("upload bad imageType %q: expected 400, got %d", badType, rec.Code)
		}

		reqURL := httptest.NewRequest(http.MethodPost, "/Items/mov-sec/Images/"+badType+"/Url", strings.NewReader(`{"Url":"`+srv.URL+`/img/poster.png"}`))
		recURL := httptest.NewRecorder()
		env.router.ServeHTTP(recURL, reqURL)
		if recURL.Code != http.StatusBadRequest {
			t.Fatalf("SetImageURL bad imageType %q: expected 400, got %d", badType, recURL.Code)
		}
	}

	dlReq := httptest.NewRequest(http.MethodPost, "/Items/mov-sec/RemoteImages/Download?Type=../../etc&ImageUrl="+srv.URL+"/img/poster.png", nil)
	dlRec := httptest.NewRecorder()
	env.router.ServeHTTP(dlRec, dlReq)
	if dlRec.Code != http.StatusBadRequest {
		t.Fatalf("RemoteImages/Download bad Type: expected 400, got %d", dlRec.Code)
	}

	// 2. Verify all 7 allowlisted types normalize cleanly
	for _, allowed := range []string{"primary", "Backdrop", "LOGO", "thumb", "banner", "art", "disc"} {
		if _, ok := normalizeAllowedImageType(allowed); !ok {
			t.Fatalf("expected %q to be allowed", allowed)
		}
	}
	if _, ok := normalizeAllowedImageType("../escape"); ok {
		t.Fatal("expected traversal type to be rejected")
	}
	if _, err := safeImageFilePath(t.TempDir(), "../../escape", 0, ".jpg"); err == nil {
		t.Fatal("expected safeImageFilePath to reject escaping path")
	}
}

func TestSSRFProtectionBlocksPrivateAndLocalTargets(t *testing.T) {
	tmpDir := t.TempDir()
	cfg, err := config.Load(filepath.Join(tmpDir, "data"))
	if err != nil {
		t.Fatal(err)
	}
	store, err := sqlite.Open(cfg.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	repo := sqlite.NewLibraryRepo(store)
	reg := plugins.NewRegistry()
	bus := eventbus.New()
	sc := scanner.New(repo, reg, bus, cfg)
	img := imaging.New(repo).WithCacheDir(cfg.ImageCacheDir())

	// Default production Handler (no SetHTTPClient override -> SSRF guard active)
	h := New(repo, sc, img, reg, store, cfg)
	router := h.BuildRouter(AuthFuncs{
		UserIDFromCtx: func(context.Context) string { return "admin" },
	})

	ctx := context.Background()
	lib, _ := repo.CreateLibrary(ctx, "Movies", "movies", []string{tmpDir})
	_ = repo.UpsertItem(ctx, &sqlite.ItemRecord{
		ID: "mov-ssrf", LibraryID: lib.ID, ParentID: lib.ID, Type: "Movie", MediaType: "Video", Name: "SSRF",
	})

	blockedURLs := []string{
		"http://127.0.0.1:8096/secret.png",
		"http://169.254.169.254/latest/meta-data/",
		"http://10.0.0.1/internal.png",
		"http://172.16.0.1/internal.png",
		"http://192.168.1.1/router.png",
		"http://[::1]:8096/secret.png",
		"http://localhost:8096/secret.png",
		"http://sub.localhost/secret.png",
		"http://0.0.0.0/secret.png",
		"file:///etc/passwd",
	}
	for _, u := range blockedURLs {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/Images/Remote?imageUrl="+u, nil))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("proxy %s: expected 400 Bad Request, got %d (%s)", u, rec.Code, rec.Body.String())
		}

		dlRec := httptest.NewRecorder()
		router.ServeHTTP(dlRec, httptest.NewRequest(http.MethodPost, "/Items/mov-ssrf/RemoteImages/Download?Type=Primary&ImageUrl="+u, nil))
		if dlRec.Code != http.StatusBadRequest {
			t.Fatalf("download %s: expected 400 Bad Request, got %d", u, dlRec.Code)
		}

		setRec := httptest.NewRecorder()
		router.ServeHTTP(setRec, httptest.NewRequest(http.MethodPost, "/Items/mov-ssrf/Images/Primary/Url", strings.NewReader(`{"Url":"`+u+`"}`)))
		if setRec.Code != http.StatusBadRequest {
			t.Fatalf("setImageURL %s: expected 400 Bad Request, got %d", u, setRec.Code)
		}
	}

	// Verify netutil.ValidateIP & shared safe client DialContext / CheckRedirect directly
	for _, blocked := range []net.IP{
		nil,
		net.ParseIP("127.0.0.1"),
		net.ParseIP("10.1.2.3"),
		net.ParseIP("100.64.0.1"),
		net.ParseIP("0.0.0.1"),
		net.ParseIP("169.254.169.254"),
		net.ParseIP("::1"),
		net.ParseIP("fc00::1"),
	} {
		if err := netutil.ValidateIP(blocked, false); err == nil {
			t.Fatalf("expected private/local/reserved IP %v to be blocked", blocked)
		}
	}
	if err := netutil.ValidateIP(net.ParseIP("8.8.8.8"), false); err != nil {
		t.Fatalf("expected public IP 8.8.8.8 to be allowed, got %v", err)
	}

	safeClient := h.getHTTPClient()
	if safeClient != h.getHTTPClient() {
		t.Fatal("expected Handler.getHTTPClient() to reuse a single shared http.Client")
	}
	tr := safeClient.Transport.(*http.Transport)
	if _, err := tr.DialContext(ctx, "tcp", "127.0.0.1:80"); err == nil {
		t.Fatal("expected safe DialContext to block 127.0.0.1")
	}
	if _, err := tr.DialContext(ctx, "tcp", "100.64.0.1:80"); err == nil {
		t.Fatal("expected safe DialContext to block CGNAT 100.64.0.1")
	}
	if _, err := tr.DialContext(ctx, "tcp", "bad-addr-no-port"); err == nil {
		t.Fatal("expected safe DialContext to reject invalid addr")
	}
	redirReq, _ := http.NewRequest(http.MethodGet, "http://127.0.0.1/internal", nil)
	if err := safeClient.CheckRedirect(redirReq, []*http.Request{redirReq}); err == nil {
		t.Fatal("expected CheckRedirect to block redirect to 127.0.0.1")
	}
	pubReq, _ := http.NewRequest(http.MethodGet, "https://93.184.216.34/a.jpg", nil)
	if err := safeClient.CheckRedirect(pubReq, make([]*http.Request, 5)); err == nil {
		t.Fatal("expected CheckRedirect to reject >= 5 redirects")
	}
	if err := safeClient.CheckRedirect(pubReq, []*http.Request{pubReq}); err != nil {
		t.Fatalf("expected CheckRedirect to allow public redirect, got %v", err)
	}
}

func TestCompatMediaWatcherSyncOnVirtualFolderMutations(t *testing.T) {
	tmpDir := t.TempDir()
	cfg, err := config.Load(filepath.Join(tmpDir, "data"))
	if err != nil {
		t.Fatal(err)
	}
	store, err := sqlite.Open(cfg.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	repo := sqlite.NewLibraryRepo(store)
	w, err := watcher.New(func(context.Context, string, []string) {})
	if err != nil {
		t.Fatalf("watcher.New: %v", err)
	}
	t.Cleanup(func() { _ = w.Close() })

	h := New(repo, nil, nil, nil, store, cfg)
	h.SetWatcher(w)
	router := h.BuildRouter(AuthFuncs{
		UserIDFromCtx: func(context.Context) string { return "admin" },
	})

	dir1 := filepath.Join(tmpDir, "lib1")
	dir2 := filepath.Join(tmpDir, "lib2")
	dir3 := filepath.Join(tmpDir, "lib3")
	_ = os.MkdirAll(dir1, 0o755)
	_ = os.MkdirAll(dir2, 0o755)
	_ = os.MkdirAll(dir3, 0o755)

	ctx := context.Background()
	lib, err := repo.CreateLibrary(ctx, "Movies", "movies", []string{dir1})
	if err != nil {
		t.Fatal(err)
	}

	// Add path -> WatchLibrary
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/Library/VirtualFolders/Paths", strings.NewReader(`{"Id":"`+lib.ID+`","Path":"`+dir2+`"}`)))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("AddVirtualFolderPath status=%d", rec.Code)
	}

	// Update path -> WatchLibrary
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/Library/VirtualFolders/Paths/Update", strings.NewReader(`{"Id":"`+lib.ID+`","PathInfo":{"Path":"`+dir3+`"}}`)))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("UpdateVirtualFolderPath status=%d", rec.Code)
	}

	// Delete one path -> WatchLibrary with remaining paths
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/Library/VirtualFolders/Paths/Delete", strings.NewReader(`{"Id":"`+lib.ID+`","Path":"`+dir3+`"}`)))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("DeleteVirtualFolderPath status=%d", rec.Code)
	}

	// Delete remaining paths -> UnwatchLibrary
	for _, p := range []string{dir1, dir2} {
		rec = httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/Library/VirtualFolders/Paths?id="+lib.ID+"&path="+p, nil))
		if rec.Code != http.StatusNoContent {
			t.Fatalf("DeleteVirtualFolderPath %s status=%d", p, rec.Code)
		}
	}

	// Delete library by name -> UnwatchLibrary
	lib2, err := repo.CreateLibrary(ctx, "ByNameLib", "movies", []string{dir1})
	if err != nil {
		t.Fatal(err)
	}
	_ = w.WatchLibrary(lib2.ID, lib2.Paths)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/Library/VirtualFolders/Delete?name=ByNameLib", nil))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("DeleteVirtualFolder by name status=%d", rec.Code)
	}

	// Delete library by id -> UnwatchLibrary
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/Library/VirtualFolders/Delete?id="+lib.ID, nil))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("DeleteVirtualFolder by id status=%d", rec.Code)
	}
}
