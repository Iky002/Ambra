package users

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/ambra-media/ambra/internal/api/middleware"
	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/core/auth"
	"github.com/ambra-media/ambra/internal/core/session"
	"github.com/ambra-media/ambra/internal/pkg/eventbus"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestH04H05M01M02_UsersPlaybackPolicyRemoteAccessAndLoopbackRateLimit(t *testing.T) {
	dir := t.TempDir()
	st, err := sqlite.Open(filepath.Join(dir, "users_audit_v0112.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	ctx := context.Background()
	cfg := config.Default(dir)
	userRepo := sqlite.NewUserRepo(st)
	libRepo := sqlite.NewLibraryRepo(st)
	authSvc := auth.NewService(userRepo, cfg)
	sessMgr := session.NewManager(userRepo, cfg, eventbus.New())
	sessMgr.SetLibraryRepo(libRepo)
	authMW := middleware.NewAuth(authSvc, sessMgr)
	authMW.SetConfig(cfg)

	h := NewHandler(authSvc, userRepo, sessMgr, nil, cfg)
	h.SetLibraryRepo(libRepo)
	r := chi.NewRouter()
	RegisterRoutes(r, authMW, h)

	// Libraries: livetv + movies
	liveLib, err := libRepo.CreateLibrary(ctx, "DVR", "livetv", []string{"/dvr"})
	if err != nil {
		t.Fatal(err)
	}
	movLib, err := libRepo.CreateLibrary(ctx, "Movies", "movies", []string{"/movies"})
	if err != nil {
		t.Fatal(err)
	}

	dvrRec := &sqlite.ItemRecord{ID: guid.New(), LibraryID: liveLib.ID, Type: "Movie", Name: "DVR1", SortName: "dvr1", Path: "/dvr/1.ts", MediaType: "Video"}
	unratedRec := &sqlite.ItemRecord{ID: guid.New(), LibraryID: movLib.ID, Type: "Movie", Name: "Unrated", SortName: "unrated", Path: "/movies/u.mkv", MediaType: "Video"}
	untaggedRec := &sqlite.ItemRecord{ID: guid.New(), LibraryID: movLib.ID, Type: "Movie", Name: "Untagged", SortName: "untagged", Path: "/movies/ut.mkv", MediaType: "Video", OfficialRating: "G"}
	okRec := &sqlite.ItemRecord{ID: guid.New(), LibraryID: movLib.ID, Type: "Movie", Name: "Safe", SortName: "safe", Path: "/movies/ok.mkv", MediaType: "Video", OfficialRating: "G"}
	for _, it := range []*sqlite.ItemRecord{dvrRec, unratedRec, untaggedRec, okRec} {
		if err := libRepo.UpsertItem(ctx, it); err != nil {
			t.Fatal(err)
		}
	}
	_ = libRepo.ReplaceItemTaxonomies(ctx, okRec.ID, "Tag", []string{"family"})

	kid, err := authSvc.CreateUser(ctx, "kid", "kidpass", false)
	if err != nil {
		t.Fatal(err)
	}
	pol := sqlite.DefaultUserPolicy(false)
	pol.EnableAllFolders = true
	pol.EnableLiveTvAccess = false
	pol.BlockUnratedItems = []string{"Movie"}
	pol.AllowedTags = []string{"family"}
	pol.EnableRemoteAccess = false
	if err := userRepo.UpdateUserPolicy(ctx, kid.ID, pol); err != nil {
		t.Fatal(err)
	}
	authSvc.InvalidateUserCache(kid.ID)

	kidAuth, err := authSvc.AuthenticateByName(ctx, "kid", "kidpass", "", auth.ClientInfo{DeviceID: "kid-tv"})
	if err != nil {
		t.Fatal(err)
	}

	// 1. H-04 & H-05: /Sessions/Playing rejects DVR item (EnableAllFolders=true, EnableLiveTvAccess=false),
	// unrated movie (BlockUnratedItems), and movie missing AllowedTags with 403, while allowing okRec!
	for _, blockedID := range []string{dvrRec.ID, unratedRec.ID, untaggedRec.ID} {
		req := httptest.NewRequest(http.MethodPost, "/Sessions/Playing", bytes.NewBufferString(`{"ItemId":"`+blockedID+`"}`))
		req.RemoteAddr = "127.0.0.1:40000"
		req.Header.Set("X-Emby-Token", kidAuth.AccessToken)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("expected 403 for restricted item %s, got %d", blockedID, rec.Code)
		}
	}
	reqOK := httptest.NewRequest(http.MethodPost, "/Sessions/Playing", bytes.NewBufferString(`{"ItemId":"`+okRec.ID+`"}`))
	reqOK.RemoteAddr = "127.0.0.1:40000"
	reqOK.Header.Set("X-Emby-Token", kidAuth.AccessToken)
	recOK := httptest.NewRecorder()
	r.ServeHTTP(recOK, reqOK)
	if recOK.Code != http.StatusNoContent {
		t.Fatalf("expected 204 for allowed item, got %d", recOK.Code)
	}

	// 2. M-02: Server-wide EnableRemoteAccess=false returns 403 BEFORE reading/decoding body or running Argon2id!
	cfg.Update(func(c *config.Config) {
		f := false
		c.EnableRemoteAccessGlobal = &f
	})
	reqGlobalOff := httptest.NewRequest(http.MethodPost, "/Users/AuthenticateByName", bytes.NewBufferString(`{not-even-valid-json`))
	reqGlobalOff.RemoteAddr = "203.0.113.55:50000"
	recGlobalOff := httptest.NewRecorder()
	r.ServeHTTP(recGlobalOff, reqGlobalOff)
	if recGlobalOff.Code != http.StatusForbidden {
		t.Fatalf("expected 403 before JSON decode / Argon2id when server remote access disabled, got %d", recGlobalOff.Code)
	}

	// Re-enable server remote access; per-user EnableRemoteAccess=false MUST return 401 (never 403) and count toward rate limit!
	cfg.Update(func(c *config.Config) {
		tr := true
		c.EnableRemoteAccessGlobal = &tr
	})
	for i := 0; i < authByNameRateLimit; i++ {
		req := httptest.NewRequest(http.MethodPost, "/Users/AuthenticateByName", bytes.NewBufferString(`{"Username":"kid","Pw":"kidpass"}`))
		req.RemoteAddr = "203.0.113.77:50000"
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d expected uniform 401 on per-user remote-disabled login, got %d", i+1, rec.Code)
		}
	}
	reqLimited := httptest.NewRequest(http.MethodPost, "/Users/AuthenticateByName", bytes.NewBufferString(`{"Username":"kid","Pw":"kidpass"}`))
	reqLimited.RemoteAddr = "203.0.113.77:50000"
	recLimited := httptest.NewRecorder()
	r.ServeHTTP(recLimited, reqLimited)
	if recLimited.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 after repeated remote-disabled login attempts, got %d", recLimited.Code)
	}

	// 3. M-01: Loopback tunnel brute-force hits 429 after authByNameLoopbackRateLimit (60) failures!
	if _, err := authSvc.CreateUser(ctx, "loopuser", "", false); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < authByNameLoopbackRateLimit; i++ {
		req := httptest.NewRequest(http.MethodPost, "/Users/AuthenticateByName", bytes.NewBufferString(`{"Username":"loopuser","Pw":"wrong"}`))
		req.RemoteAddr = "127.0.0.1:50000"
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("loopback attempt %d expected 401, got %d", i+1, rec.Code)
		}
	}
	reqLoop429 := httptest.NewRequest(http.MethodPost, "/Users/AuthenticateByName", bytes.NewBufferString(`{"Username":"loopuser","Pw":"wrong"}`))
	reqLoop429.RemoteAddr = "127.0.0.1:50000"
	recLoop429 := httptest.NewRecorder()
	r.ServeHTTP(recLoop429, reqLoop429)
	if recLoop429.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 on loopback tunnel brute-force after %d attempts, got %d", authByNameLoopbackRateLimit, recLoop429.Code)
	}
}
