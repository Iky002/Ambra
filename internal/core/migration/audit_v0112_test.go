package migration

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/ambra-media/ambra/internal/api/dto"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

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
