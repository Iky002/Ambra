package streaming

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/ambra-media/ambra/internal/api/middleware"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestPrivatePlaylistCoverACL(t *testing.T) {
	tmp := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmp, "streaming_acl.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	ctx := context.Background()
	libRepo := sqlite.NewLibraryRepo(store)
	userRepo := sqlite.NewUserRepo(store)

	alice := &sqlite.UserRecord{ID: guid.New(), Name: "alice", Policy: sqlite.DefaultUserPolicy(false)}
	if err := userRepo.CreateUser(ctx, alice); err != nil {
		t.Fatal(err)
	}
	bob := &sqlite.UserRecord{ID: guid.New(), Name: "bob", Policy: sqlite.DefaultUserPolicy(false)}
	if err := userRepo.CreateUser(ctx, bob); err != nil {
		t.Fatal(err)
	}
	admin := &sqlite.UserRecord{ID: guid.New(), Name: "admin", IsAdmin: true, Policy: sqlite.DefaultUserPolicy(true)}
	if err := userRepo.CreateUser(ctx, admin); err != nil {
		t.Fatal(err)
	}

	privPL, err := libRepo.CreatePlaylist(ctx, "Alice Private", "Audio", alice.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	pubPL, err := libRepo.CreatePlaylist(ctx, "Alice Public", "Audio", alice.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := userRepo.SetServerConfig(ctx, "item_visibility_"+guid.Normalize(pubPL.ID), `{"IsPublic":true}`); err != nil {
		t.Fatal(err)
	}

	h := New(libRepo, nil, nil)
	h.SetUserRepo(userRepo)

	reqAs := func(u *sqlite.UserRecord) *http.Request {
		r := httptest.NewRequest(http.MethodGet, "/Items/"+privPL.ID+"/Images/Primary", nil)
		return r.WithContext(middleware.WithAuthContext(r.Context(), &middleware.AuthContext{User: u}))
	}

	if h.canAccessItem(reqAs(bob), privPL.ID) {
		t.Fatal("expected non-owner Bob to be denied access to Alice's private playlist")
	}
	if !h.canAccessItem(reqAs(alice), privPL.ID) {
		t.Fatal("expected owner Alice to access her private playlist")
	}
	if !h.canAccessItem(reqAs(admin), privPL.ID) {
		t.Fatal("expected admin to access Alice's private playlist")
	}
	if !h.canAccessItem(reqAs(bob), pubPL.ID) {
		t.Fatal("expected Bob to access Alice's public playlist")
	}
}
