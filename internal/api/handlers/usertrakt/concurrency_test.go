package usertrakt

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/ambra-media/ambra/internal/api/middleware"
	"github.com/ambra-media/ambra/internal/plugins/trakt"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestV0109Audit_PostDecodeDoesNotHoldMutex(t *testing.T) {
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "trakt_v0109.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	repo := sqlite.NewUserRepo(store)
	alice := &sqlite.UserRecord{ID: "alice-v0109", Name: "alice", CreatedAt: time.Now()}
	bob := &sqlite.UserRecord{ID: "bob-v0109", Name: "bob", CreatedAt: time.Now()}
	if err := repo.CreateUser(context.Background(), alice); err != nil {
		t.Fatal(err)
	}
	if err := repo.CreateUser(context.Background(), bob); err != nil {
		t.Fatal(err)
	}

	p := trakt.New()
	if err := p.Configure(map[string]any{"enabled": true}); err != nil {
		t.Fatal(err)
	}
	h := New(p, repo)

	// Start a slow POST whose body blocks mid-stream; it MUST NOT hold h.mu during body decoding.
	pr, pw := io.Pipe()
	postDone := make(chan int, 1)
	go func() {
		req := httptest.NewRequest(http.MethodPost, "/Users/Me/Trakt", pr)
		req = req.WithContext(middleware.WithAuthContext(req.Context(), &middleware.AuthContext{User: alice}))
		rec := httptest.NewRecorder()
		h.handle(rec, req)
		postDone <- rec.Code
	}()

	// Write opening brace so DecodeJSONLimit is actively reading from pipe
	_, _ = pw.Write([]byte(`{"ClientId":"cid1",`))

	// Concurrent GET by another user must complete promptly without waiting for the stalled POST body
	getDone := make(chan int, 1)
	go func() {
		req := httptest.NewRequest(http.MethodGet, "/Users/Me/Trakt", nil)
		req = req.WithContext(middleware.WithAuthContext(req.Context(), &middleware.AuthContext{User: bob}))
		rec := httptest.NewRecorder()
		h.handle(rec, req)
		getDone <- rec.Code
	}()

	select {
	case code := <-getDone:
		if code != http.StatusOK {
			t.Fatalf("concurrent GET expected 200, got %d", code)
		}
	case <-time.After(2 * time.Second):
		_ = pw.Close()
		t.Fatal("concurrent GET blocked while slow POST was decoding JSON — h.mu was held during DecodeJSONLimit")
	}

	// Finish the POST payload and ensure it succeeds
	_, _ = pw.Write([]byte(`"AccessToken":"tok1"}`))
	_ = pw.Close()

	select {
	case code := <-postDone:
		if code != http.StatusOK {
			t.Fatalf("slow POST expected 200 after completing body, got %d", code)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("slow POST did not finish")
	}
}
