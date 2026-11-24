package compatmedia

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/ambra-media/ambra/internal/api/middleware"
	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/core/imaging"
	"github.com/ambra-media/ambra/internal/core/transcode"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/plugins"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestCompatMediaItemFileSubtitleAttachmentRBAC(t *testing.T) {
	tmp := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmp, "cm_item_rbac.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	ctx := context.Background()
	repo := sqlite.NewLibraryRepo(store)
	userRepo := sqlite.NewUserRepo(store)
	cfg := config.Default(tmp)

	libAllowed, _ := repo.CreateLibrary(ctx, "Allowed", "movies", []string{filepath.Join(tmp, "a")})
	libForbidden, _ := repo.CreateLibrary(ctx, "Forbidden", "movies", []string{filepath.Join(tmp, "b")})

	secretFile := filepath.Join(tmp, "secret.mkv")
	_ = os.WriteFile(secretFile, []byte("secret-video"), 0o644)
	secretSub := filepath.Join(tmp, "secret.srt")
	_ = os.WriteFile(secretSub, []byte("1\n00:00:01,000 --> 00:00:02,000\nSecret\n"), 0o644)

	secretItem := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: libForbidden.ID, ParentID: libForbidden.ID,
		Type: "Movie", MediaType: "Video", Name: "Secret", Path: secretFile,
	}
	_ = repo.UpsertItem(ctx, secretItem)
	_ = repo.UpsertMediaSourceAndStreams(ctx, secretItem.ID, sqlite.MediaSourceRecord{
		ID: secretItem.ID, ItemID: secretItem.ID, Path: secretFile, Container: "mkv",
		Streams: []sqlite.MediaStreamRecord{
			{StreamIndex: 2, StreamType: "Subtitle", Codec: "srt", IsExternal: true, Path: secretSub},
		},
	})

	pol := sqlite.DefaultUserPolicy(false)
	pol.EnableAllFolders = false
	pol.EnabledFolders = []string{libAllowed.ID}
	restricted := &sqlite.UserRecord{ID: guid.New(), Name: "restricted", Policy: pol, Config: sqlite.DefaultUserConfig()}
	_ = userRepo.CreateUser(ctx, restricted)

	admin := &sqlite.UserRecord{ID: guid.New(), Name: "admin", IsAdmin: true, Policy: sqlite.DefaultUserPolicy(true), Config: sqlite.DefaultUserConfig()}
	_ = userRepo.CreateUser(ctx, admin)

	h := New(repo, nil, imaging.New(repo), plugins.NewRegistry(), store, cfg)
	h.SetUserRepo(userRepo)
	router := h.BuildRouter(AuthFuncs{})

	getAs := func(u *sqlite.UserRecord, path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req = req.WithContext(middleware.WithAuthContext(req.Context(), &middleware.AuthContext{User: u}))
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}

	endpoints := []string{
		"/Items/" + secretItem.ID + "/File",
		"/Items/" + secretItem.ID + "/" + secretItem.ID + "/Subtitles/2/Stream.vtt",
		"/Videos/" + secretItem.ID + "/" + secretItem.ID + "/Attachments",
		"/Videos/" + secretItem.ID + "/" + secretItem.ID + "/Attachments/0",
	}
	for _, ep := range endpoints {
		rec := getAs(restricted, ep)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("GET %s as restricted: got %d, want 403", ep, rec.Code)
		}
	}

	// Admin can access file and subtitle.
	if rec := getAs(admin, "/Items/"+secretItem.ID+"/File"); rec.Code != http.StatusOK {
		t.Fatalf("admin GET /File: got %d, want 200", rec.Code)
	}
	if rec := getAs(admin, "/Items/"+secretItem.ID+"/"+secretItem.ID+"/Subtitles/2/Stream.vtt"); rec.Code != http.StatusOK {
		t.Fatalf("admin GET subtitle: got %d, want 200", rec.Code)
	}
}

func TestCompatMediaActiveEncodingsDeleteOwnership(t *testing.T) {
	tmp := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmp, "cm_enc_rbac.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	ctx := context.Background()
	repo := sqlite.NewLibraryRepo(store)
	userRepo := sqlite.NewUserRepo(store)
	cfg := config.Default(tmp)
	mgr := transcode.NewManager(cfg, transcode.WithCapabilities(transcode.SoftwareOnlyCapabilities()))
	defer mgr.Close()

	userA := &sqlite.UserRecord{ID: guid.New(), Name: "userA", Policy: sqlite.DefaultUserPolicy(false), Config: sqlite.DefaultUserConfig()}
	userB := &sqlite.UserRecord{ID: guid.New(), Name: "userB", Policy: sqlite.DefaultUserPolicy(false), Config: sqlite.DefaultUserConfig()}
	admin := &sqlite.UserRecord{ID: guid.New(), Name: "admin", IsAdmin: true, Policy: sqlite.DefaultUserPolicy(true), Config: sqlite.DefaultUserConfig()}
	_ = userRepo.CreateUser(ctx, userA)
	_ = userRepo.CreateUser(ctx, userB)
	_ = userRepo.CreateUser(ctx, admin)

	h := New(repo, nil, imaging.New(repo), plugins.NewRegistry(), store, cfg)
	h.SetUserRepo(userRepo)
	h.SetTranscodeManager(mgr)
	router := h.BuildRouter(AuthFuncs{})

	_, _ = mgr.RegisterSession(transcode.SessionParams{
		UserID: userA.ID, DeviceID: "dev-a", PlaySessionID: "ps-a", ItemID: "i1", MediaSourceID: "m1", InputPath: filepath.Join(tmp, "a.mkv"),
	})

	postAs := func(u *sqlite.UserRecord, path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, path, nil)
		req = req.WithContext(middleware.WithAuthContext(req.Context(), &middleware.AuthContext{User: u}))
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}

	// userB cannot stop userA's session.
	if rec := postAs(userB, "/Videos/ActiveEncodings/Delete?DeviceId=dev-a&PlaySessionId=ps-a"); rec.Code != http.StatusNoContent {
		t.Fatalf("got %d, want 204", rec.Code)
	}
	if len(mgr.ActiveSessions()) != 1 {
		t.Fatalf("userB should not have stopped userA's session")
	}

	// admin can stop userA's session.
	if rec := postAs(admin, "/Videos/ActiveEncodings/Delete?DeviceId=dev-a&PlaySessionId=ps-a"); rec.Code != http.StatusNoContent {
		t.Fatalf("got %d, want 204", rec.Code)
	}
	if len(mgr.ActiveSessions()) != 0 {
		t.Fatalf("admin should have stopped userA's session")
	}
}
