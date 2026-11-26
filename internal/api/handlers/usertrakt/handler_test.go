package usertrakt

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ambra-media/ambra/internal/api/middleware"
	"github.com/ambra-media/ambra/internal/plugins/trakt"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestPersonalConfigPrivacyValidationAndPersistenceFailures(t *testing.T) {
	ctx := context.Background()
	db, err := sqlite.Open(filepath.Join(t.TempDir(), "personal.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	repo := sqlite.NewUserRepo(db)
	alice := &sqlite.UserRecord{ID: "alice", Name: "alice"}
	if err := repo.CreateUser(ctx, alice); err != nil {
		t.Fatal(err)
	}
	p := trakt.New()
	_ = p.Configure(map[string]any{"enabled": true})
	p.SetUserConfigStore(repo)
	h := New(p, repo)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux, func(next http.Handler) http.Handler { return next })
	call := func(method, body string, ac *middleware.AuthContext, want int) string {
		t.Helper()
		r := httptest.NewRequest(method, "/Users/Me/Trakt", strings.NewReader(body))
		if ac != nil {
			r = r.WithContext(middleware.WithAuthContext(r.Context(), ac))
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		if w.Code != want {
			t.Fatalf("%s: got=%d want=%d body=%s", method, w.Code, want, w.Body.String())
		}
		if w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("private account responses must not be cached")
		}
		return w.Body.String()
	}
	ac := &middleware.AuthContext{User: alice}
	call("GET", "", nil, 403)
	call("GET", "", &middleware.AuthContext{IsAPIKey: true}, 403)
	call("GET", "", &middleware.AuthContext{User: alice, IsAPIKey: true}, 403)
	call("GET", "", ac, 200)
	call("POST", `{`, ac, 400)
	call("POST", `{"ClientId":"app"}`, ac, 400)
	call("POST", `{"ClientId":"","AccessToken":"token"}`, ac, 400)
	call("POST", `{"ClientId":"app","AccessToken":"token","ScrobblePlayback":false,"SyncWatched":false}`, ac, 200)
	if body := call("GET", "", ac, 200); strings.Contains(body, `"token"`) {
		t.Fatal("saved token was returned")
	}
	call("POST", `{"AccessToken":"rotated-token"}`, ac, 200)
	call("POST", `{"ClientId":"other-app"}`, ac, 400)
	call("POST", `{"ClientId":"`+strings.Repeat("x", 257)+`","AccessToken":"token"}`, ac, 400)
	call("POST", `{"AccessToken":"`+strings.Repeat("x", 4097)+`"}`, ac, 400)
	call("POST", `{"AccessToken":"********"}`, ac, 400)
	call("POST", `{"AccessToken":""}`, ac, 400)
	call("POST", `{"AccessToken":"a\nb"}`, ac, 400)
	call("POST", `{"AccessToken":"`+strings.Repeat("x", 17000)+`"}`, ac, 400)
	_ = p.Configure(map[string]any{"enabled": false})
	call("POST", `{}`, ac, 403)
	call("GET", "", ac, 200)
	call("DELETE", "", ac, 204)
	call("DELETE", "", ac, 204)
	_ = p.Configure(map[string]any{"enabled": true})
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	call("GET", "", ac, 500)
	call("POST", `{"ClientId":"app","AccessToken":"token"}`, ac, 500)
	call("DELETE", "", ac, 500)
}

func TestSaveFailureDoesNotReturnCredentials(t *testing.T) {
	ctx := context.Background()
	db, err := sqlite.Open(filepath.Join(t.TempDir(), "failure.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	repo := sqlite.NewUserRepo(db)
	alice := &sqlite.UserRecord{ID: "alice", Name: "alice"}
	if err := repo.CreateUser(ctx, alice); err != nil {
		t.Fatal(err)
	}
	// Simulate disk/write failure after reading the existing settings.
	if _, err := db.Writer.Exec(`CREATE TRIGGER reject_trakt_insert BEFORE INSERT ON user_trakt BEGIN SELECT RAISE(FAIL, 'disk failure'); END`); err != nil {
		t.Fatal(err)
	}
	p := trakt.New()
	_ = p.Configure(map[string]any{"enabled": true})
	h := New(p, repo)
	r := httptest.NewRequest("POST", "/Users/Me/Trakt", strings.NewReader(`{"ClientId":"app","AccessToken":"never-return-this"}`))
	r = r.WithContext(middleware.WithAuthContext(ctx, &middleware.AuthContext{User: alice}))
	w := httptest.NewRecorder()
	h.handle(w, r)
	if w.Code != 500 || strings.Contains(w.Body.String(), "never-return-this") {
		t.Fatalf("write failure must stay private: %d %s", w.Code, w.Body.String())
	}
}
