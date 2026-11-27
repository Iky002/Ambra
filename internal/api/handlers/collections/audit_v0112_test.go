package collections

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/ambra-media/ambra/internal/api/middleware"
	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestAuditV0112_PlaylistQuotaAndItemAddedCountResponse(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	dataDir := filepath.Join(tmp, "data")
	movDir := filepath.Join(tmp, "movies")
	_ = os.MkdirAll(dataDir, 0o755)
	_ = os.MkdirAll(movDir, 0o755)

	store, err := sqlite.Open(filepath.Join(dataDir, "col_v0112.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	repo := sqlite.NewLibraryRepo(store)
	userRepo := sqlite.NewUserRepo(store)
	cfg := config.Default(dataDir)

	lib, _ := repo.CreateLibrary(ctx, "Movies", "movies", []string{movDir})
	m1 := &sqlite.ItemRecord{ID: guid.New(), LibraryID: lib.ID, ParentID: lib.ID, Type: "Movie", Name: "M1", OfficialRating: "G"}
	m2 := &sqlite.ItemRecord{ID: guid.New(), LibraryID: lib.ID, ParentID: lib.ID, Type: "Movie", Name: "M2", OfficialRating: "G"}
	_ = repo.UpsertItem(ctx, m1)
	_ = repo.UpsertItem(ctx, m2)

	alice := &sqlite.UserRecord{ID: guid.New(), Name: "alice_q", Policy: sqlite.DefaultUserPolicy(false), Config: sqlite.DefaultUserConfig()}
	admin := &sqlite.UserRecord{ID: guid.New(), Name: "admin_q", IsAdmin: true, Policy: sqlite.DefaultUserPolicy(true), Config: sqlite.DefaultUserConfig()}
	_ = userRepo.CreateUser(ctx, alice)
	_ = userRepo.CreateUser(ctx, admin)

	h := New(repo, cfg)
	h.SetUserRepo(userRepo)
	h.SetStore(store)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux, AuthFuncs{
		UserIDFromCtx: func(c context.Context) string {
			if ac, ok := middleware.AuthFromContext(c); ok && ac != nil {
				return ac.UserID()
			}
			return ""
		},
	})

	doAs := func(u *sqlite.UserRecord, method, target, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, target, bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		req = req.WithContext(middleware.WithAuthContext(req.Context(), &middleware.AuthContext{User: u}))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}

	// 1. 3.1-10: POST /Playlists/{Id}/Items returns 200 OK with {"ItemAddedCount": 2}!
	createRec := doAs(alice, http.MethodPost, "/Playlists", `{"Name":"Alice First"}`)
	if createRec.Code != http.StatusOK {
		t.Fatalf("create playlist failed: %d", createRec.Code)
	}
	var created creationResult
	_ = json.Unmarshal(createRec.Body.Bytes(), &created)

	addRec := doAs(alice, http.MethodPost, "/Playlists/"+created.Id+"/Items", `{"Ids":["`+m1.ID+`","`+m2.ID+`"]}`)
	if addRec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK on POST /Playlists/{Id}/Items, got %d: %s", addRec.Code, addRec.Body.String())
	}
	var addBody struct {
		ItemAddedCount int `json:"ItemAddedCount"`
	}
	if err := json.Unmarshal(addRec.Body.Bytes(), &addBody); err != nil || addBody.ItemAddedCount != 2 {
		t.Fatalf("expected {\"ItemAddedCount\":2}, got body=%s err=%v", addRec.Body.String(), err)
	}

	// 2. H-10: Seed 499 additional playlists for Alice (total 500) -> 501st POST /Playlists returns 429 Too Many Requests!
	tx, err := store.Writer.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	normAlice := guid.Normalize(alice.ID)
	for i := 0; i < maxPlaylistsPerUser-1; i++ {
		pid := fmt.Sprintf("aaaa00000000000000000000%08x", i+1)
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO items (id, library_id, parent_id, type, media_type, is_folder, name, sort_name, etag, extra_json, date_created, date_modified)
			 VALUES (?, 'playlists', ?, 'Playlist', 'Video', 1, ?, ?, 'e', '{}', 1, 1)`,
			pid, normAlice, fmt.Sprintf("P%d", i), fmt.Sprintf("p%d", i),
		); err != nil {
			_ = tx.Rollback()
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	overQuotaRec := doAs(alice, http.MethodPost, "/Playlists", `{"Name":"Over Quota 501"}`)
	if overQuotaRec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 Too Many Requests when non-admin exceeds %d playlists, got %d: %s", maxPlaylistsPerUser, overQuotaRec.Code, overQuotaRec.Body.String())
	}

	// Admin is not blocked by non-admin user playlist quota.
	adminRec := doAs(admin, http.MethodPost, "/Playlists", `{"Name":"Admin Playlist"}`)
	if adminRec.Code != http.StatusOK {
		t.Fatalf("expected admin POST /Playlists to succeed, got %d", adminRec.Code)
	}
}
