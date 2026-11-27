package migration

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ambra-media/ambra/internal/api/dto"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestAuditV0107_M19_PasswordlessAdminCreatedDisabledWithNotice(t *testing.T) {
	tmp := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmp, "mig.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	userRepo := sqlite.NewUserRepo(store)
	m := NewMigrator(store, userRepo)

	uid, matched, created, conflict, notice, err := m.resolveOrCreateUser(context.Background(), SourceUser{
		ID:          "src-admin-nopass",
		Name:        "migrated_admin",
		HasPassword: false,
		Policy: dto.UserPolicy{
			IsAdministrator: true,
			IsDisabled:      false,
		},
	}, true, false)
	if err != nil {
		t.Fatalf("resolveOrCreateUser: %v", err)
	}
	if matched || !created || conflict != nil {
		t.Fatalf("expected created=true matched=false conflict=nil, got matched=%v created=%v conflict=%v", matched, created, conflict)
	}
	if !strings.Contains(strings.ToLower(notice), "disabled") {
		t.Fatalf("expected notice explaining disabled passwordless admin, got %q", notice)
	}

	u, err := userRepo.GetUserByID(context.Background(), uid)
	if err != nil {
		t.Fatal(err)
	}
	if !u.IsAdmin || !u.IsDisabled || !u.Policy.IsDisabled {
		t.Fatalf("expected passwordless migrated admin to be IsAdmin=true & IsDisabled=true, got IsAdmin=%v IsDisabled=%v Policy.IsDisabled=%v",
			u.IsAdmin, u.IsDisabled, u.Policy.IsDisabled)
	}
}

func TestAuditV0109_L29_TVDBProviderMatchAndAsyncWait(t *testing.T) {
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "mig.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	libRepo := sqlite.NewLibraryRepo(store)
	ctx := context.Background()
	lib, err := libRepo.CreateLibrary(ctx, "TV", "tvshows", []string{"/media"})
	if err != nil {
		t.Fatal(err)
	}

	series := &sqlite.ItemRecord{
		ID:        "series-tvdb-1",
		LibraryID: lib.ID,
		Type:      "Series",
		Name:      "Local Name Differs",
		Extra: sqlite.ItemExtra{
			ProviderIDs: map[string]string{"Tvdb": "81189"},
		},
	}
	if err := libRepo.UpsertItem(ctx, series); err != nil {
		t.Fatal(err)
	}

	m := NewMigrator(store, nil)
	idx, err := m.loadLocalIndex(ctx)
	if err != nil {
		t.Fatalf("loadLocalIndex: %v", err)
	}
	localID, conflict := idx.match(SourceItem{
		ID:          "src-1",
		Name:        "Different Source Name",
		Type:        "Series",
		ProviderIDs: map[string]string{"Tvdb": "81189"},
	})
	if conflict != "" || localID != series.ID {
		t.Fatalf("expected TVDB match %q, got id=%q conflict=%q", series.ID, localID, conflict)
	}

	// Verify Wait() blocks until StartAsync background goroutine finishes cleanly.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/System/Info":
			time.Sleep(40 * time.Millisecond)
			_, _ = w.Write([]byte(`{"ServerName":"Legacy","Version":"4.8"}`))
		case "/Users":
			_, _ = w.Write([]byte(`[]`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	mAsync := NewMigrator(store, nil, WithClientFactory(func(baseURL, apiKey string) (*SourceClient, error) {
		return NewSourceClient(baseURL, apiKey, WithHTTPClient(srv.Client()))
	}))
	if _, err := mAsync.StartAsync(MigrationRequest{
		SourceURL: srv.URL,
		APIKey:    "k",
	}); err != nil {
		t.Fatalf("StartAsync: %v", err)
	}
	mAsync.Wait()
	if st := mAsync.Status(); st.Status != StatusCompleted {
		t.Fatalf("expected Completed status after Wait(), got %+v", st)
	}
}

func TestAuditV0112_SelfRedirectBlockedAndLocalAdminPolicyConfigPreserved(t *testing.T) {
	ctx := context.Background()

	// 1. M-12: SourceClient blocks 302 redirect pointing back to selfListenAddr with ErrSelfSourceServer.
	selfSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("self server should never receive redirected request")
		w.WriteHeader(http.StatusOK)
	}))
	defer selfSrv.Close()
	selfParsed, _ := url.Parse(selfSrv.URL)

	redirectingSource := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, selfSrv.URL+"/System/Info", http.StatusFound)
	}))
	defer redirectingSource.Close()

	sc, err := NewSourceClient(
		redirectingSource.URL,
		"src-api-key",
		WithHTTPClient(redirectingSource.Client()),
		WithSelfListenAddr(selfParsed.Host),
	)
	if err != nil {
		t.Fatalf("NewSourceClient unexpected error on non-self initial URL: %v", err)
	}
	if _, err := sc.TestConnection(ctx); !errors.Is(err, ErrSelfSourceServer) {
		t.Fatalf("expected ErrSelfSourceServer on 302 redirect to selfListenAddr, got %v", err)
	}

	// 2. L-10: Merging source user into an existing local admin preserves ALL fields of local admin's Policy and Configuration.
	st, userRepo, _ := openTestStore(t)
	localAdminPol := sqlite.DefaultUserPolicy(true)
	localAdminPol.EnableRemoteAccess = true
	localAdminPol.EnableContentDownloading = true
	localAdminCfg := sqlite.DefaultUserConfig()
	localAdminCfg.AudioLanguagePreference = "jpn"
	localAdminCfg.SubtitleMode = "Always"

	localAdmin := &sqlite.UserRecord{
		Name:    "AdminBoss",
		IsAdmin: true,
		Policy:  localAdminPol,
		Config:  localAdminCfg,
	}
	if err := userRepo.CreateUser(ctx, localAdmin); err != nil {
		t.Fatal(err)
	}

	zero := 0
	hostileSrcPolicy := dto.UserPolicy{
		IsAdministrator:          false,
		IsDisabled:               true,
		EnableRemoteAccess:       false,
		EnableContentDownloading: false,
		EnableAllFolders:         false,
		EnabledFolders:           []string{},
		MaxParentalRating:        &zero,
		BlockedTags:              []string{"everything"},
	}
	hostileSrcCfg := dto.UserConfiguration{
		AudioLanguagePreference: "eng",
		SubtitleMode:            "None",
	}

	fake := &fakeEmbyServer{
		apiKey:     "k",
		serverInfo: SourceServerInfo{ID: "other-srv", ServerName: "OldEmby"},
		users: []SourceUser{
			{ID: "src-1", Name: "AdminBoss", Policy: hostileSrcPolicy, Configuration: hostileSrcCfg},
		},
		itemsByUser: map[string][]SourceItem{"src-1": {}},
	}
	srv := newFakeEmbyServer(t, fake)

	m := NewMigrator(st, userRepo, WithClientFactory(func(baseURL, apiKey string) (*SourceClient, error) {
		return NewSourceClient(baseURL, apiKey, WithHTTPClient(srv.Client()))
	}))

	rep, err := m.Run(ctx, MigrationRequest{
		SourceURL:    srv.URL,
		APIKey:       "k",
		MigrateUsers: true,
	})
	if err != nil || rep.Status != StatusCompleted {
		t.Fatalf("Run failed: rep=%+v err=%v", rep, err)
	}

	after, err := userRepo.GetUserByName(ctx, "AdminBoss")
	if err != nil {
		t.Fatal(err)
	}
	if !after.Policy.EnableRemoteAccess || !after.Policy.EnableContentDownloading || !after.Policy.EnableAllFolders || after.Policy.MaxParentalRating != nil || len(after.Policy.BlockedTags) != 0 {
		t.Fatalf("local admin Policy was overwritten by source user! got %+v", after.Policy)
	}
	if after.Config.AudioLanguagePreference != "jpn" || after.Config.SubtitleMode != "Always" {
		t.Fatalf("local admin Configuration was overwritten by source user! got %+v", after.Config)
	}
}
