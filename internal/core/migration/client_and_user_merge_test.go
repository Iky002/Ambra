package migration

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ambra-media/ambra/internal/api/dto"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestMigrationCovV0112_SelfListenTargetAndHTTPClientWrap(t *testing.T) {
	t.Cleanup(func() { SetSelfServerIdentity("", "") })

	mustURL := func(raw string) *url.URL {
		u, err := url.Parse(raw)
		if err != nil {
			t.Fatal(err)
		}
		return u
	}

	// 1. isSelfListenTarget edge cases
	if isSelfListenTarget(nil, ":8096") || isSelfListenTarget(mustURL("http://localhost:8096"), "") {
		t.Fatal("nil url or empty listenAddr must return false")
	}
	for _, badAddr := range []string{"no-port", ":notnum", ":0", ":-5"} {
		if isSelfListenTarget(mustURL("http://localhost:8096"), badAddr) {
			t.Fatalf("invalid listenAddr %q must return false", badAddr)
		}
	}
	if isSelfListenTarget(mustURL("http://localhost:9999"), ":8096") {
		t.Fatal("mismatched port must return false")
	}
	if !isSelfListenTarget(mustURL("http://localhost"), ":80") {
		t.Fatal("default http port 80 must match :80")
	}
	if !isSelfListenTarget(mustURL("https://localhost"), ":443") {
		t.Fatal("default https port 443 must match :443")
	}
	if !isSelfListenTarget(mustURL("http://ambra.lan:8096"), "ambra.lan:8096") {
		t.Fatal("matching hostname:port must return true")
	}
	if !isSelfListenTarget(mustURL("http://[::1]:8096"), "127.0.0.1:8096") {
		t.Fatal("IPv6 loopback target must match IPv4 loopback listener")
	}
	if !isSelfListenTarget(mustURL("http://0.0.0.0:8096"), ":8096") {
		t.Fatal("unspecified target on wildcard listener must return true")
	}
	if isSelfListenTarget(mustURL("http://93.184.216.34:8096"), ":8096") {
		t.Fatal("public external IP must not match wildcard listener")
	}

	// Also test local non-loopback interface IP matching against wildcard ":8096" if available
	if ifaces, err := net.Interfaces(); err == nil {
		for _, iface := range ifaces {
			addrs, _ := iface.Addrs()
			for _, raw := range addrs {
				if ipNet, ok := raw.(*net.IPNet); ok && ipNet.IP != nil && ipNet.IP.To4() != nil && !ipNet.IP.IsLoopback() {
					u := mustURL("http://" + ipNet.IP.String() + ":8096")
					if !isSelfListenTarget(u, "0.0.0.0:8096") {
						t.Fatalf("expected interface IP %s to match wildcard listener", ipNet.IP)
					}
				}
			}
		}
	}

	// 2. wrapSourceHTTPClient CheckRedirect branches
	c := wrapSourceHTTPClient(nil, "127.0.0.1:8096")
	reqSelf, _ := http.NewRequest(http.MethodGet, "http://localhost:8096/System/Info", nil)
	if err := c.CheckRedirect(reqSelf, nil); !errors.Is(err, ErrSelfSourceServer) {
		t.Fatalf("expected ErrSelfSourceServer on redirect to self, got %v", err)
	}
	if err := c.CheckRedirect(reqSelf, make([]*http.Request, 10)); err == nil || !strings.Contains(err.Error(), "stopped after 10 redirects") {
		t.Fatalf("expected 10-redirects error, got %v", err)
	}

	cBare := wrapSourceHTTPClient(&http.Client{}, "127.0.0.1:8096")
	origURL := mustURL("http://emby1.lan:8096/Users")
	crossReq, _ := http.NewRequest(http.MethodGet, "http://emby2.lan:8096/Users", nil)
	crossReq.Header.Set("Authorization", "Bearer abc")
	crossReq.Header.Set("Cookie", "c=1")
	crossReq.Header.Set("Proxy-Authorization", "Basic xyz")
	crossReq.Header.Set("X-Emby-Token", "secret-key")
	crossReq.Header.Set("X-MediaBrowser-Token", "secret-key-2")
	if err := cBare.CheckRedirect(crossReq, []*http.Request{{URL: origURL}}); err != nil {
		t.Fatalf("expected cross-origin redirect to succeed, got %v", err)
	}
	if crossReq.Header.Get("X-Emby-Token") != "" || crossReq.Header.Get("Authorization") != "" {
		t.Fatalf("expected auth tokens stripped on cross-origin redirect, got %+v", crossReq.Header)
	}

	sentinel := errors.New("custom redirect block")
	cCustom := wrapSourceHTTPClient(&http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error { return sentinel },
	}, "")
	if err := cCustom.CheckRedirect(crossReq, nil); !errors.Is(err, sentinel) {
		t.Fatalf("expected custom CheckRedirect error, got %v", err)
	}

	// 3. truncateErrorBody & APIError.Error()
	longBody := strings.Repeat("x", maxErrorBodyBytes+50)
	trunc := truncateErrorBody([]byte(longBody))
	if !strings.HasSuffix(trunc, "...") || len(trunc) != maxErrorBodyBytes+3 {
		t.Fatalf("unexpected truncated body len=%d", len(trunc))
	}
	if got := (&APIError{StatusCode: 502}).Error(); got != "source emby returned HTTP 502" {
		t.Fatalf("unexpected empty-message APIError: %q", got)
	}
	if got := (&APIError{StatusCode: 401, Message: "bad token"}).Error(); !strings.Contains(got, "bad token") {
		t.Fatalf("unexpected message APIError: %q", got)
	}
}

func TestMigrationCovV0112_DoJSONRetriesAndErrors(t *testing.T) {
	var mode atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch mode.Load() {
		case 1:
			w.Header().Set("Retry-After", "999")
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte("busy"))
		case 2:
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{invalid-json`))
		case 3:
			http.Redirect(w, r, "http://127.0.0.1:18096/System/Info", http.StatusFound)
		default:
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`null`))
		}
	}))
	defer srv.Close()

	sc, err := NewSourceClient(
		srv.URL,
		"tok",
		WithHTTPClient(srv.Client()),
		WithMaxRetries(1),
		WithBaseBackoff(time.Millisecond),
		WithSelfListenAddr("127.0.0.1:18096"),
		WithSelfServerID("self-srv-1"),
		WithSleepFunc(func(context.Context, time.Duration) error { return nil }),
	)
	if err != nil {
		t.Fatal(err)
	}

	// 0. null JSON slices normalize to empty non-nil slices; empty userID rejected
	users, err := sc.ListUsers(context.Background())
	if err != nil || users == nil || len(users) != 0 {
		t.Fatalf("expected empty non-nil users slice, got %v err=%v", users, err)
	}
	folders, err := sc.ListVirtualFolders(context.Background())
	if err != nil || folders == nil || len(folders) != 0 {
		t.Fatalf("expected empty non-nil folders slice, got %v err=%v", folders, err)
	}
	if _, err := sc.FetchUserItemsPage(context.Background(), "  ", -5, 0); err == nil {
		t.Fatal("expected error for empty userID")
	}

	// 1. 503 exhausting retries with Retry-After clamped
	mode.Store(1)
	if _, err := sc.TestConnection(context.Background()); err == nil {
		t.Fatal("expected 503 error after exhausting retries")
	}

	// 2. Malformed JSON response
	mode.Store(2)
	if _, err := sc.TestConnection(context.Background()); err == nil || !strings.Contains(err.Error(), "decode /System/Info response") {
		t.Fatalf("expected JSON decode error, got %v", err)
	}

	// 3. Redirect to self server returns ErrSelfSourceServer immediately
	mode.Store(3)
	if _, err := sc.TestConnection(context.Background()); !errors.Is(err, ErrSelfSourceServer) {
		t.Fatalf("expected ErrSelfSourceServer on redirect, got %v", err)
	}

	// 4. Cancelled context before request
	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := sc.TestConnection(canceledCtx); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

func TestMigrationCovV0112_MigratorUserMergeFolderMapAndHelpers(t *testing.T) {
	t.Cleanup(func() { SetSelfServerIdentity("", "") })

	// 1. Nil receiver guards
	var nilMig *Migrator
	nilMig.SetOnUserUpdated(func(string) {})
	nilMig.SetSelfServer(":8096", "srv1")

	// 2. appendBoundedString & appendBoundedConflict cap enforcement
	strs := make([]string, maxReportEntries)
	if got := appendBoundedString(strs, "extra"); len(got) != maxReportEntries {
		t.Fatalf("expected appendBoundedString capped at %d, got %d", maxReportEntries, len(got))
	}
	confs := make([]ConflictEntry, maxReportEntries)
	if got := appendBoundedConflict(confs, ConflictEntry{Reason: "x"}); len(got) != maxReportEntries {
		t.Fatalf("expected appendBoundedConflict capped at %d, got %d", maxReportEntries, len(got))
	}

	// 3. NewMigrator with nil userRepo + SetSelfServer + default clientFactory rejecting self URL
	st, userRepo, libRepo := openTestStore(t)
	m := NewMigrator(st, nil)
	var updatedUsers []string
	m.SetOnUserUpdated(func(uid string) { updatedUsers = append(updatedUsers, uid) })
	m.SetSelfServer("127.0.0.1:28096", "srv-abc")
	if _, err := m.TestSource(context.Background(), "http://localhost:28096", "k"); !errors.Is(err, ErrSelfSourceServer) {
		t.Fatalf("expected ErrSelfSourceServer via default clientFactory, got %v", err)
	}

	ctx := context.Background()

	// 4. writeUserItemBatch empty slice no-op
	if err := m.writeUserItemBatch(ctx, nil); err != nil {
		t.Fatalf("writeUserItemBatch(nil) failed: %v", err)
	}

	// 5. resolveOrCreateUser branches:
	// a) Empty name -> conflict
	if _, _, _, conf, _, err := m.resolveOrCreateUser(ctx, SourceUser{ID: "u0", Name: "   "}, true, false); err != nil || conf == nil {
		t.Fatalf("expected conflict for empty user name, got conf=%v err=%v", conf, err)
	}
	// b) migrateUsers=false for unknown user -> conflict
	if _, _, _, conf, _, err := m.resolveOrCreateUser(ctx, SourceUser{ID: "u1", Name: "MissingUser"}, false, false); err != nil || conf == nil {
		t.Fatalf("expected conflict when migrateUsers=false, got conf=%v err=%v", conf, err)
	}
	// c) dryRun=true for new user -> synthetic dryrun ID
	if id, _, created, _, _, err := m.resolveOrCreateUser(ctx, SourceUser{ID: "u2", Name: "DryUser"}, true, true); err != nil || !created || !strings.HasPrefix(id, "dryrun-") {
		t.Fatalf("expected dryrun user creation, got id=%q created=%v err=%v", id, created, err)
	}
	// d) Passwordless source administrator -> created disabled with security notice
	if _, _, created, _, notice, err := m.resolveOrCreateUser(ctx, SourceUser{
		ID:          "u3",
		Name:        "SrcNoPassAdmin",
		HasPassword: false,
		Policy:      dto.UserPolicy{IsAdministrator: true},
	}, true, false); err != nil || !created || !strings.Contains(notice, "created disabled for security") {
		t.Fatalf("expected passwordless admin disabled notice, got created=%v notice=%q err=%v", created, notice, err)
	}
	// e) Existing local admin protected during merge; existing local non-admin blocked from admin elevation
	adminID := guid.New()
	_ = userRepo.CreateUser(ctx, &sqlite.UserRecord{
		ID:        adminID,
		Name:      "LocalBoss",
		IsAdmin:   true,
		Policy:    dto.UserPolicy{IsAdministrator: true},
		Config:    sqlite.DefaultUserConfig(),
		CreatedAt: time.Now().UTC(),
	})
	if id, matched, _, _, notice, err := m.resolveOrCreateUser(ctx, SourceUser{
		ID:     "src-boss",
		Name:   "LocalBoss",
		Policy: dto.UserPolicy{IsDisabled: true},
	}, true, false); err != nil || !matched || id != adminID || !strings.Contains(notice, "protected during merge") {
		t.Fatalf("expected local admin protected notice, got id=%q matched=%v notice=%q err=%v", id, matched, notice, err)
	}

	memberID := guid.New()
	_ = userRepo.CreateUser(ctx, &sqlite.UserRecord{
		ID:        memberID,
		Name:      "LocalMember",
		IsAdmin:   false,
		Policy:    dto.UserPolicy{IsAdministrator: false, AuthenticationProviderId: "CustomAuth"},
		Config:    sqlite.DefaultUserConfig(),
		CreatedAt: time.Now().UTC(),
	})
	if id, matched, _, _, notice, err := m.resolveOrCreateUser(ctx, SourceUser{
		ID:     "src-member",
		Name:   "LocalMember",
		Policy: dto.UserPolicy{IsAdministrator: true},
	}, true, false); err != nil || !matched || id != memberID || !strings.Contains(notice, "admin elevation skipped") {
		t.Fatalf("expected admin elevation skipped notice, got id=%q matched=%v notice=%q err=%v", id, matched, notice, err)
	}

	// 6. buildLibraryFolderMapper (name fallback) + mapEnabledFolders (nil, empty, bySourceKey[lower], localIDs[norm], unmapped)
	var nilMapper *libraryFolderMapper
	if mapped, unm := nilMapper.mapEnabledFolders([]string{"a"}); len(mapped) != 0 || unm != 0 {
		t.Fatalf("expected empty result on nil mapper, got %v %d", mapped, unm)
	}

	localLib, err := libRepo.CreateLibrary(ctx, "Anime Vault", "movies", []string{"/mnt/local/anime"})
	if err != nil {
		t.Fatal(err)
	}
	localLibID := guid.Normalize(localLib.ID)

	fm := m.buildLibraryFolderMapper(ctx, []SourceVirtualFolder{{
		Name:      "Anime Vault",
		ItemID:    "src-folder-raw-id",
		Locations: []string{"/different/remote/path"},
	}})
	mapped, unmapped := fm.mapEnabledFolders([]string{
		"",
		"Anime Vault",       // matches bySourceKey[lower]
		localLib.ID,         // matches localIDs[norm]
		"nonexistent-lib-9", // unmapped
	})
	if len(mapped) != 1 || guid.Normalize(mapped[0]) != localLibID || unmapped != 1 {
		t.Fatalf("unexpected mapEnabledFolders result: mapped=%v unmapped=%d", mapped, unmapped)
	}
}
