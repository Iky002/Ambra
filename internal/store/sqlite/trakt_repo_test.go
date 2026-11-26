package sqlite

import (
	"context"
	"path/filepath"
	"testing"
)

func TestPrivateTraktBindingsPersistAndCascadeWithUserDeletion(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "accounts.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	repo := NewUserRepo(s)
	for _, name := range []string{"alice", "bob"} {
		if err := repo.CreateUser(ctx, &UserRecord{ID: name, Name: name}); err != nil {
			t.Fatal(err)
		}
		c := TraktConfig{ClientID: name + "-app", AccessToken: name + "-secret", ScrobblePlayback: true, SyncWatched: true}
		if err := repo.SaveUserTraktConfig(ctx, name, c); err != nil {
			t.Fatal(err)
		}
	}
	if err := repo.SaveUserTraktConfig(ctx, "missing", TraktConfig{}); err == nil {
		t.Fatal("bindings must belong to existing users")
	}
	if _, err := s.Writer.Exec(`UPDATE users SET is_disabled=1 WHERE id='bob'`); err != nil {
		t.Fatal(err)
	}
	if c, err := repo.GetUserTraktConfig(ctx, "bob"); err != nil || c != nil {
		t.Fatal("disabled users must not have active bindings")
	}
	if err := repo.DeleteUser(ctx, "alice"); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := s.Reader.QueryRow(`SELECT COUNT(*) FROM user_trakt WHERE user_id='alice'`).Scan(&count); err != nil || count != 0 {
		t.Fatal("deleting a user must delete their private Trakt credentials")
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if _, err := reopened.Writer.Exec(`UPDATE users SET is_disabled=0 WHERE id='bob'`); err != nil {
		t.Fatal(err)
	}
	repo = NewUserRepo(reopened)
	bob, err := repo.GetUserTraktConfig(ctx, "bob")
	if err != nil || bob == nil || bob.AccessToken != "bob-secret" {
		t.Fatal("other users' bindings must persist across migrations and reopen")
	}
	if err := repo.DeleteUserTraktConfig(ctx, "bob"); err != nil {
		t.Fatal(err)
	}
	if c, err := repo.GetUserTraktConfig(ctx, "bob"); err != nil || c != nil {
		t.Fatal("disconnect must delete credentials")
	}
}
