package migration

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func intPtr(v int) *int { return &v }

func openTestStore(t *testing.T) (*sqlite.Store, *sqlite.UserRepo, *sqlite.LibraryRepo) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	st, err := sqlite.Open(dbPath)
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st, sqlite.NewUserRepo(st), sqlite.NewLibraryRepo(st)
}

type fakeEmbyServer struct {
	mu            sync.Mutex
	apiKey        string
	serverInfo    SourceServerInfo
	users         []SourceUser
	folders       []SourceVirtualFolder
	itemsByUser   map[string][]SourceItem
	failStatusSeq []int
	retryAfter    string
	itemReqHook   func(userID string, startIndex int)
	reqCount      atomic.Int32
}

func newFakeEmbyServer(t *testing.T, f *fakeEmbyServer) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.reqCount.Add(1)

		if f.apiKey != "" && r.Header.Get("X-Emby-Token") != f.apiKey {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte("invalid token"))
			return
		}

		f.mu.Lock()
		if len(f.failStatusSeq) > 0 {
			code := f.failStatusSeq[0]
			f.failStatusSeq = f.failStatusSeq[1:]
			ra := f.retryAfter
			f.mu.Unlock()
			if ra != "" {
				w.Header().Set("Retry-After", ra)
			}
			w.WriteHeader(code)
			_, _ = w.Write([]byte("transient error"))
			return
		}
		hook := f.itemReqHook
		f.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/System/Info":
			_ = json.NewEncoder(w).Encode(f.serverInfo)
		case r.URL.Path == "/Users":
			_ = json.NewEncoder(w).Encode(f.users)
		case r.URL.Path == "/Library/VirtualFolders":
			_ = json.NewEncoder(w).Encode(f.folders)
		case strings.HasPrefix(r.URL.Path, "/Users/") && strings.HasSuffix(r.URL.Path, "/Items"):
			trimmed := strings.TrimPrefix(r.URL.Path, "/Users/")
			userID := strings.TrimSuffix(trimmed, "/Items")
			startIndex, _ := strconv.Atoi(r.URL.Query().Get("StartIndex"))
			limit, _ := strconv.Atoi(r.URL.Query().Get("Limit"))
			if limit <= 0 {
				limit = 50
			}
			if hook != nil {
				hook(userID, startIndex)
			}
			f.mu.Lock()
			all := f.itemsByUser[userID]
			f.mu.Unlock()
			end := startIndex + limit
			if startIndex > len(all) {
				startIndex = len(all)
			}
			if end > len(all) {
				end = len(all)
			}
			page := SourceItemsPage{
				Items:            all[startIndex:end],
				TotalRecordCount: len(all),
			}
			_ = json.NewEncoder(w).Encode(page)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func seedLocalLibrary(t *testing.T, libRepo *sqlite.LibraryRepo) map[string]string {
	t.Helper()
	ctx := context.Background()
	lib, err := libRepo.CreateLibrary(ctx, "Movies & TV", "movies", []string{"/media"})
	if err != nil {
		t.Fatalf("create library: %v", err)
	}

	ids := map[string]string{}

	// 1. Movie matched by TMDB ID
	m1 := &sqlite.ItemRecord{
		LibraryID:      lib.ID,
		Type:           "Movie",
		Name:           "盗梦空间",
		OriginalTitle:  "Inception",
		ProductionYear: intPtr(2010),
		Extra:          sqlite.ItemExtra{ProviderIDs: map[string]string{"Tmdb": "27205", "Imdb": "tt1375666"}},
	}
	if err := libRepo.UpsertItem(ctx, m1); err != nil {
		t.Fatalf("upsert m1: %v", err)
	}
	ids["inception"] = m1.ID

	// 2. Movie matched by IMDb ID only
	m2 := &sqlite.ItemRecord{
		LibraryID:      lib.ID,
		Type:           "Movie",
		Name:           "星际穿越",
		ProductionYear: intPtr(2014),
		Extra:          sqlite.ItemExtra{ProviderIDs: map[string]string{"Imdb": "tt0816692"}},
	}
	if err := libRepo.UpsertItem(ctx, m2); err != nil {
		t.Fatalf("upsert m2: %v", err)
	}
	ids["interstellar"] = m2.ID

	// 3. Movie matched by normalized title + year fallback (no ProviderIDs)
	m3 := &sqlite.ItemRecord{
		LibraryID:      lib.ID,
		Type:           "Movie",
		Name:           "讓子彈飛",
		ProductionYear: intPtr(2010),
	}
	if err := libRepo.UpsertItem(ctx, m3); err != nil {
		t.Fatalf("upsert m3: %v", err)
	}
	ids["bullets"] = m3.ID

	// 4. Movie matched by title-only fallback (no year)
	m4 := &sqlite.ItemRecord{
		LibraryID: lib.ID,
		Type:      "Movie",
		Name:      "Spirited Away",
	}
	if err := libRepo.UpsertItem(ctx, m4); err != nil {
		t.Fatalf("upsert m4: %v", err)
	}
	ids["spirited"] = m4.ID

	// 5. Series + Episode matched by SeriesName + Season + Episode fallback
	series := &sqlite.ItemRecord{
		LibraryID:      lib.ID,
		Type:           "Series",
		Name:           "绝命毒师",
		OriginalTitle:  "Breaking Bad",
		ProductionYear: intPtr(2008),
		Extra:          sqlite.ItemExtra{ProviderIDs: map[string]string{"Tmdb": "1396"}},
	}
	if err := libRepo.UpsertItem(ctx, series); err != nil {
		t.Fatalf("upsert series: %v", err)
	}
	ids["series"] = series.ID

	ep := &sqlite.ItemRecord{
		LibraryID:         lib.ID,
		SeriesID:          series.ID,
		Type:              "Episode",
		Name:              "Pilot",
		ParentIndexNumber: intPtr(1),
		IndexNumber:       intPtr(1),
		Extra:             sqlite.ItemExtra{SeriesName: "绝命毒师"},
	}
	if err := libRepo.UpsertItem(ctx, ep); err != nil {
		t.Fatalf("upsert ep: %v", err)
	}
	ids["ep1"] = ep.ID

	return ids
}

func TestSourceClientValidationAndRetryBackoff(t *testing.T) {
	if _, err := NewSourceClient("", "k"); err == nil {
		t.Fatal("expected error for empty URL")
	}
	if _, err := NewSourceClient("http://localhost:8096", ""); err == nil {
		t.Fatal("expected error for empty apiKey")
	}
	if _, err := NewSourceClient("ftp://invalid", "k"); err == nil {
		t.Fatal("expected error for invalid scheme")
	}

	fake := &fakeEmbyServer{
		apiKey: "secret-key",
		serverInfo: SourceServerInfo{
			ID:          "srv-1",
			ServerName:  "Legacy Emby",
			Version:     "4.8.10.0",
			ProductName: "Emby Server",
		},
		users:         []SourceUser{{ID: "u1", Name: "Alice"}},
		folders:       []SourceVirtualFolder{{Name: "Movies", ItemID: "f1", CollectionType: "movies", Locations: []string{"/movies"}}},
		failStatusSeq: []int{http.StatusTooManyRequests, http.StatusServiceUnavailable},
		retryAfter:    "1",
	}
	srv := newFakeEmbyServer(t, fake)

	var sleeps []time.Duration
	client, err := NewSourceClient(
		srv.URL+"/",
		"secret-key",
		WithHTTPClient(srv.Client()),
		WithMaxRetries(3),
		WithBaseBackoff(10*time.Millisecond),
		WithSleepFunc(func(_ context.Context, d time.Duration) error {
			sleeps = append(sleeps, d)
			return nil
		}),
	)
	if err != nil {
		t.Fatalf("NewSourceClient: %v", err)
	}
	if client.BaseURL() != srv.URL {
		t.Fatalf("expected trimmed baseURL %s, got %s", srv.URL, client.BaseURL())
	}

	ctx := context.Background()
	info, err := client.TestConnection(ctx)
	if err != nil {
		t.Fatalf("TestConnection after retries failed: %v", err)
	}
	if info.ServerName != "Legacy Emby" || info.Status != "ok" {
		t.Fatalf("unexpected info: %+v", info)
	}
	if len(sleeps) != 2 {
		t.Fatalf("expected 2 backoff sleeps, got %d (%v)", len(sleeps), sleeps)
	}
	if sleeps[0] != time.Second {
		t.Fatalf("expected Retry-After 1s respected, got %v", sleeps[0])
	}

	users, err := client.ListUsers(ctx)
	if err != nil || len(users) != 1 {
		t.Fatalf("ListUsers failed: %v, len=%d", err, len(users))
	}
	folders, err := client.ListVirtualFolders(ctx)
	if err != nil || len(folders) != 1 {
		t.Fatalf("ListVirtualFolders failed: %v, len=%d", err, len(folders))
	}

	if _, err := client.FetchUserItemsPage(ctx, "", 0, 10); err == nil {
		t.Fatal("expected error for empty userID")
	}

	// Exhaust retries on 429
	fake.mu.Lock()
	fake.failStatusSeq = []int{http.StatusTooManyRequests, http.StatusTooManyRequests, http.StatusTooManyRequests, http.StatusTooManyRequests}
	fake.retryAfter = ""
	fake.mu.Unlock()
	if _, err := client.TestConnection(ctx); err == nil {
		t.Fatal("expected error when retries exhausted")
	} else {
		var apiErr *APIError
		if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusTooManyRequests {
			t.Fatalf("expected 429 APIError, got %v", err)
		}
		if !strings.Contains(apiErr.Error(), "429") {
			t.Fatalf("unexpected error string: %s", apiErr.Error())
		}
	}

	// 401 should fail immediately without retry
	badClient, _ := NewSourceClient(srv.URL, "wrong-key", WithMaxRetries(3))
	before := fake.reqCount.Load()
	if _, err := badClient.TestConnection(ctx); err == nil {
		t.Fatal("expected 401 error")
	}
	if fake.reqCount.Load()-before != 1 {
		t.Fatalf("expected no retries on 401, got %d requests", fake.reqCount.Load()-before)
	}

	// Context cancellation during sleep
	fake.mu.Lock()
	fake.failStatusSeq = []int{http.StatusTooManyRequests}
	fake.mu.Unlock()
	cancelCtx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := client.TestConnection(cancelCtx); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

func TestMigratorFullMigrationDryRunAndIdempotency(t *testing.T) {
	st, userRepo, libRepo := openTestStore(t)
	localIDs := seedLocalLibrary(t, libRepo)

	// Pre-create Alice locally so she matches existing user
	existingAlice := &sqlite.UserRecord{
		Name:    "Alice",
		IsAdmin: true,
		Policy:  sqlite.DefaultUserPolicy(true),
		Config:  sqlite.DefaultUserConfig(),
	}
	if err := userRepo.CreateUser(context.Background(), existingAlice); err != nil {
		t.Fatalf("create local Alice: %v", err)
	}

	playedTime := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	alicePolicy := sqlite.DefaultUserPolicy(true)
	alicePolicy.EnableContentDownloading = false
	bobPolicy := sqlite.DefaultUserPolicy(false)
	bobPolicy.EnableRemoteAccess = false

	fake := &fakeEmbyServer{
		apiKey: "emby-key",
		serverInfo: SourceServerInfo{
			ID:         "legacy-1",
			ServerName: "Old Emby NAS",
			Version:    "4.8.8.0",
		},
		users: []SourceUser{
			{ID: "src-alice", Name: "Alice", Policy: alicePolicy, Configuration: sqlite.DefaultUserConfig()},
			{ID: "src-bob", Name: "Bob", Policy: bobPolicy, Configuration: sqlite.DefaultUserConfig()},
		},
		folders: []SourceVirtualFolder{
			{Name: "Movies", ItemID: "lib1", CollectionType: "movies", Locations: []string{"/movies"}},
		},
		itemsByUser: map[string][]SourceItem{
			"src-alice": {
				{
					ID:          "s-m1",
					Name:        "Inception",
					Type:        "Movie",
					ProviderIDs: map[string]string{"Tmdb": "27205"},
					UserData:    &SourceUserData{Played: true, PlayCount: 2, IsFavorite: true, LastPlayedDate: &playedTime},
				},
				{
					ID:          "s-m2",
					Name:        "Interstellar",
					Type:        "Movie",
					ProviderIDs: map[string]string{"Imdb": "tt0816692"},
					UserData:    &SourceUserData{Played: false, PlaybackPositionTicks: 1234560000},
				},
				{
					ID:             "s-m3",
					Name:           "让子弹飞",
					Type:           "Movie",
					ProductionYear: intPtr(2010),
					UserData:       &SourceUserData{Played: true, PlayCount: 0}, // PlayCount 0 -> normalized to 1
				},
				{
					ID:       "s-m4",
					Name:     "Spirited Away",
					Type:     "Movie",
					UserData: &SourceUserData{IsFavorite: true},
				},
				{
					ID:                "s-ep1",
					Name:              "Pilot",
					Type:              "Episode",
					SeriesName:        "Breaking Bad",
					ParentIndexNumber: intPtr(1),
					IndexNumber:       intPtr(1),
					UserData:          &SourceUserData{Played: true, PlayCount: 1, PlaybackPositionTicks: 900000},
				},
				{
					ID:       "s-unmatched",
					Name:     "Nonexistent Movie",
					Type:     "Movie",
					UserData: &SourceUserData{Played: true},
				},
				{
					ID:       "s-nodata",
					Name:     "Inception",
					Type:     "Movie",
					UserData: &SourceUserData{}, // Nothing to migrate
				},
			},
			"src-bob": {
				{
					ID:          "b-m1",
					Name:        "Inception",
					Type:        "Movie",
					ProviderIDs: map[string]string{"tmdb": "27205"},
					UserData:    &SourceUserData{IsFavorite: true, PlaybackPositionTicks: 5000000},
				},
			},
		},
	}
	srv := newFakeEmbyServer(t, fake)

	migrator := NewMigrator(st, userRepo, WithPageSize(3), WithClientFactory(func(baseURL, apiKey string) (*SourceClient, error) {
		return NewSourceClient(baseURL, apiKey, WithHTTPClient(srv.Client()), WithBaseBackoff(time.Millisecond))
	}))

	ctx := context.Background()

	// 1. TestSource
	testRes, err := migrator.TestSource(ctx, srv.URL, "emby-key")
	if err != nil {
		t.Fatalf("TestSource: %v", err)
	}
	if testRes.Server.ServerName != "Old Emby NAS" || len(testRes.Users) != 2 || len(testRes.VirtualFolders) != 1 {
		t.Fatalf("unexpected TestSource result: %+v", testRes)
	}

	// 2. Dry-Run preview should NOT mutate DB
	dryReq := MigrationRequest{
		SourceURL:        srv.URL,
		APIKey:           "emby-key",
		MigrateUsers:     true,
		MigratePlayed:    true,
		MigrateFavorites: true,
		MigrateResume:    true,
		DryRun:           true,
		BatchSize:        2,
	}
	dryReport, err := migrator.Run(ctx, dryReq)
	if err != nil {
		t.Fatalf("DryRun failed: %v", err)
	}
	if !dryReport.DryRun || dryReport.Status != StatusCompleted {
		t.Fatalf("unexpected dry run status: %+v", dryReport)
	}
	if dryReport.UsersMatched != 1 || dryReport.UsersCreated != 1 {
		t.Fatalf("expected 1 matched + 1 created user in dry run, got matched=%d created=%d", dryReport.UsersMatched, dryReport.UsersCreated)
	}
	if dryReport.ItemsScanned != 8 || dryReport.ItemsMatched != 6 || dryReport.ItemsSkipped != 2 {
		t.Fatalf("unexpected dry run item counts: scanned=%d matched=%d skipped=%d", dryReport.ItemsScanned, dryReport.ItemsMatched, dryReport.ItemsSkipped)
	}
	// Bob must not exist after dry run
	if _, err := userRepo.GetUserByName(ctx, "Bob"); !errors.Is(err, sqlite.ErrNotFound) {
		t.Fatalf("Bob should not exist after dry-run, err=%v", err)
	}

	// 3. Live migration
	liveReq := dryReq
	liveReq.DryRun = false
	liveReport, err := migrator.Run(ctx, liveReq)
	if err != nil {
		t.Fatalf("Live migration failed: %v", err)
	}
	if liveReport.Status != StatusCompleted || liveReport.DryRun {
		t.Fatalf("unexpected live report: %+v", liveReport)
	}
	if liveReport.PlayedMigrated != 3 || liveReport.FavoritesMigrated != 3 || liveReport.ResumeMigrated != 3 {
		t.Fatalf("unexpected migrated counters: played=%d fav=%d resume=%d",
			liveReport.PlayedMigrated, liveReport.FavoritesMigrated, liveReport.ResumeMigrated)
	}

	// Verify local admin Alice policy preserved (L-10) & user_item_data rows migrated
	aliceRec, err := userRepo.GetUserByName(ctx, "Alice")
	if err != nil {
		t.Fatalf("get Alice: %v", err)
	}
	if !aliceRec.Policy.EnableContentDownloading {
		t.Fatalf("expected local admin Alice policy preserved (EnableContentDownloading=true)")
	}
	ud1, err := userRepo.GetUserItemData(ctx, aliceRec.ID, localIDs["inception"])
	if err != nil || !ud1.Played || ud1.PlayCount != 2 || !ud1.IsFavorite || ud1.LastPlayedDate == nil {
		t.Fatalf("unexpected ud1: %+v, err=%v", ud1, err)
	}
	ud2, err := userRepo.GetUserItemData(ctx, aliceRec.ID, localIDs["interstellar"])
	if err != nil || ud2.Played || ud2.PlaybackPositionTicks != 1234560000 {
		t.Fatalf("unexpected ud2: %+v, err=%v", ud2, err)
	}
	ud3, err := userRepo.GetUserItemData(ctx, aliceRec.ID, localIDs["bullets"])
	if err != nil || !ud3.Played || ud3.PlayCount != 1 {
		t.Fatalf("unexpected ud3: %+v, err=%v", ud3, err)
	}
	ud4, err := userRepo.GetUserItemData(ctx, aliceRec.ID, localIDs["spirited"])
	if err != nil || !ud4.IsFavorite {
		t.Fatalf("unexpected ud4: %+v, err=%v", ud4, err)
	}
	udEp, err := userRepo.GetUserItemData(ctx, aliceRec.ID, localIDs["ep1"])
	if err != nil || !udEp.Played || udEp.PlaybackPositionTicks != 900000 {
		t.Fatalf("unexpected udEp: %+v, err=%v", udEp, err)
	}

	// Verify Bob created & data migrated
	bobRec, err := userRepo.GetUserByName(ctx, "Bob")
	if err != nil {
		t.Fatalf("get Bob: %v", err)
	}
	if bobRec.Policy.EnableRemoteAccess != false {
		t.Fatalf("expected Bob policy persisted")
	}
	bobUd, err := userRepo.GetUserItemData(ctx, bobRec.ID, localIDs["inception"])
	if err != nil || !bobUd.IsFavorite || bobUd.PlaybackPositionTicks != 5000000 {
		t.Fatalf("unexpected bobUd: %+v, err=%v", bobUd, err)
	}

	// 4. Idempotent re-run: play_count must stay 2 (not 4), no duplicate users
	rerunReport, err := migrator.Run(ctx, liveReq)
	if err != nil {
		t.Fatalf("Idempotent re-run failed: %v", err)
	}
	if rerunReport.UsersMatched != 2 || rerunReport.UsersCreated != 0 {
		t.Fatalf("expected 2 matched, 0 created on rerun, got matched=%d created=%d", rerunReport.UsersMatched, rerunReport.UsersCreated)
	}
	ud1After, err := userRepo.GetUserItemData(ctx, aliceRec.ID, localIDs["inception"])
	if err != nil || ud1After.PlayCount != 2 {
		t.Fatalf("idempotency violated: play_count=%d, err=%v", ud1After.PlayCount, err)
	}
}

func TestMigratorCancelAndCheckpointResume(t *testing.T) {
	st, userRepo, libRepo := openTestStore(t)
	_ = seedLocalLibrary(t, libRepo)

	var migrator *Migrator
	var cancelOnce sync.Once
	var secondUserCalls atomic.Int32

	fake := &fakeEmbyServer{
		apiKey:     "k",
		serverInfo: SourceServerInfo{ID: "s1", ServerName: "Old"},
		users: []SourceUser{
			{ID: "u1", Name: "UserOne", Policy: sqlite.DefaultUserPolicy(false), Configuration: sqlite.DefaultUserConfig()},
			{ID: "u2", Name: "UserTwo", Policy: sqlite.DefaultUserPolicy(false), Configuration: sqlite.DefaultUserConfig()},
		},
		itemsByUser: map[string][]SourceItem{
			"u1": {
				{ID: "i1", Name: "Inception", Type: "Movie", ProviderIDs: map[string]string{"Tmdb": "27205"}, UserData: &SourceUserData{Played: true}},
			},
			"u2": {
				{ID: "i2", Name: "Interstellar", Type: "Movie", ProviderIDs: map[string]string{"Imdb": "tt0816692"}, UserData: &SourceUserData{IsFavorite: true}},
			},
		},
		itemReqHook: func(userID string, _ int) {
			if userID == "u2" {
				secondUserCalls.Add(1)
				cancelOnce.Do(func() {
					if migrator != nil {
						migrator.Cancel()
					}
				})
			}
		},
	}
	srv := newFakeEmbyServer(t, fake)

	migrator = NewMigrator(st, userRepo, WithPageSize(1), WithClientFactory(func(baseURL, apiKey string) (*SourceClient, error) {
		return NewSourceClient(baseURL, apiKey, WithHTTPClient(srv.Client()), WithBaseBackoff(time.Millisecond))
	}))

	req := MigrationRequest{
		SourceURL:        srv.URL,
		APIKey:           "k",
		UserIDs:          []string{"u1", "u2"},
		MigrateUsers:     true,
		MigratePlayed:    true,
		MigrateFavorites: true,
	}

	// First run gets cancelled right as u2 begins, after u1 is checkpointed
	report1, err := migrator.Run(context.Background(), req)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	if report1.Status != StatusCancelled {
		t.Fatalf("expected cancelled status, got %s", report1.Status)
	}

	// Checkpoint must exist in server_config
	rawCP, err := userRepo.GetServerConfig(context.Background(), report1.CheckpointKey)
	if err != nil || !strings.Contains(rawCP, "u1") {
		t.Fatalf("expected checkpoint containing u1, got raw=%q err=%v", rawCP, err)
	}

	// Second run resumes from checkpoint (skips u1, migrates u2)
	report2, err := migrator.Run(context.Background(), req)
	if err != nil {
		t.Fatalf("resume run failed: %v", err)
	}
	if !report2.ResumedFromCheckpoint || report2.Status != StatusCompleted {
		t.Fatalf("expected resumed completed report, got %+v", report2)
	}
	if report2.ItemsMatched != 1 || report2.FavoritesMigrated != 1 {
		t.Fatalf("expected only u2's 1 item migrated on resume, got matched=%d fav=%d", report2.ItemsMatched, report2.FavoritesMigrated)
	}

	// Checkpoint should be cleared after completion
	if _, err := userRepo.GetServerConfig(context.Background(), report2.CheckpointKey); !errors.Is(err, sqlite.ErrNotFound) {
		t.Fatalf("expected checkpoint cleared on completion, got err=%v", err)
	}

	// ResetCheckpoint flag clears any stale checkpoint before run
	_ = userRepo.SetServerConfig(context.Background(), report2.CheckpointKey, `{"completed_user_ids":["u1"]}`)
	req.ResetCheckpoint = true
	report3, err := migrator.Run(context.Background(), req)
	if err != nil || report3.ResumedFromCheckpoint {
		t.Fatalf("expected clean run with ResetCheckpoint, got resumed=%v err=%v", report3.ResumedFromCheckpoint, err)
	}
}

func TestMigratorConflictsConcurrentAndAsync(t *testing.T) {
	st, userRepo, libRepo := openTestStore(t)
	ctx := context.Background()
	lib, _ := libRepo.CreateLibrary(ctx, "Movies", "movies", []string{"/movies"})

	// Insert two duplicate movies with same TMDB ID & same title+year to trigger ambiguity conflict
	for i := 0; i < 2; i++ {
		_ = libRepo.UpsertItem(ctx, &sqlite.ItemRecord{
			LibraryID:      lib.ID,
			Type:           "Movie",
			Name:           "Duplicate Film",
			ProductionYear: intPtr(2020),
			Extra:          sqlite.ItemExtra{ProviderIDs: map[string]string{"Tmdb": "99999"}},
		})
	}

	blockCh := make(chan struct{})
	startedCh := make(chan struct{}, 1)
	fake := &fakeEmbyServer{
		apiKey:     "k",
		serverInfo: SourceServerInfo{ID: "s", ServerName: "Old"},
		users: []SourceUser{
			{ID: "u-missing", Name: "NotInAmbra"},
			{ID: "u-empty", Name: ""},
		},
		itemsByUser: map[string][]SourceItem{
			"u-missing": {
				{ID: "dup1", Name: "Duplicate Film", Type: "Movie", ProviderIDs: map[string]string{"Tmdb": "99999"}, UserData: &SourceUserData{Played: true}},
			},
		},
	}
	srv := newFakeEmbyServer(t, fake)

	migrator := NewMigrator(st, nil, WithClientFactory(func(baseURL, apiKey string) (*SourceClient, error) {
		return NewSourceClient(baseURL, apiKey, WithHTTPClient(srv.Client()))
	}))

	// Cancel when idle returns false
	if migrator.Cancel() {
		t.Fatal("Cancel on idle migrator should return false")
	}

	// Run with MigrateUsers=false -> conflicts for missing user and empty name
	rep, err := migrator.Run(ctx, MigrationRequest{
		SourceURL:     srv.URL,
		APIKey:        "k",
		MigrateUsers:  false,
		MigratePlayed: true,
	})
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if len(rep.Conflicts) != 2 {
		t.Fatalf("expected 2 user conflicts, got %+v", rep.Conflicts)
	}

	// Now create NotInAmbra and test ambiguous provider match conflict
	_ = userRepo.CreateUser(ctx, &sqlite.UserRecord{Name: "NotInAmbra", Policy: sqlite.DefaultUserPolicy(false), Config: sqlite.DefaultUserConfig()})
	rep2, err := migrator.Run(ctx, MigrationRequest{
		SourceURL:     srv.URL,
		APIKey:        "k",
		UserIDs:       []string{"u-missing"},
		MigrateUsers:  false,
		MigratePlayed: true,
	})
	if err != nil {
		t.Fatalf("run2 failed: %v", err)
	}
	if len(rep2.Conflicts) != 1 || !strings.Contains(rep2.Conflicts[0].Reason, "ambiguous provider match") {
		t.Fatalf("expected ambiguous provider conflict, got %+v", rep2.Conflicts)
	}

	// Test StartAsync + ErrMigrationRunning concurrency guard
	fake.mu.Lock()
	fake.itemReqHook = func(_ string, _ int) {
		select {
		case startedCh <- struct{}{}:
		default:
		}
		<-blockCh
	}
	fake.mu.Unlock()

	asyncStatus, err := migrator.StartAsync(MigrationRequest{
		SourceURL:     srv.URL,
		APIKey:        "k",
		UserIDs:       []string{"u-missing"},
		MigratePlayed: true,
	})
	if err != nil || asyncStatus.Status != StatusRunning {
		t.Fatalf("StartAsync failed: status=%+v err=%v", asyncStatus, err)
	}
	<-startedCh

	if _, err := migrator.Run(ctx, MigrationRequest{SourceURL: srv.URL, APIKey: "k"}); !errors.Is(err, ErrMigrationRunning) {
		t.Fatalf("expected ErrMigrationRunning while active, got %v", err)
	}
	close(blockCh)

	// Wait briefly for background goroutine to finish
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if migrator.Status().Status != StatusRunning {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if migrator.Status().Status != StatusCompleted {
		t.Fatalf("expected completed after unblock, got %s", migrator.Status().Status)
	}
}

func TestSourceClientEdgeCasesAndDefaultSleep(t *testing.T) {
	// APIError without message
	e := &APIError{StatusCode: http.StatusInternalServerError}
	if !strings.Contains(e.Error(), "500") {
		t.Fatalf("unexpected error string: %s", e.Error())
	}

	// defaultSleep normal completion & cancellation
	if err := defaultSleep(context.Background(), time.Millisecond); err != nil {
		t.Fatalf("defaultSleep failed: %v", err)
	}
	cctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := defaultSleep(cctx, time.Second); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled from defaultSleep, got %v", err)
	}

	// backoffDelay caps at maxBackoffDelay
	c, err := NewSourceClient("http://127.0.0.1:8096", "k", WithBaseBackoff(time.Second))
	if err != nil {
		t.Fatalf("NewSourceClient: %v", err)
	}
	if d := c.backoffDelay(10, ""); d != maxBackoffDelay {
		t.Fatalf("expected capped delay %v, got %v", maxBackoffDelay, d)
	}
	if d := c.backoffDelay(0, "999"); d != maxBackoffDelay {
		t.Fatalf("expected capped Retry-After %v, got %v", maxBackoffDelay, d)
	}

	// Server returning null arrays & invalid JSON & retry with defaultSleep
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/System/Info":
			if calls.Add(1) == 1 {
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			_, _ = w.Write([]byte(`{invalid-json`))
		case "/Users":
			_, _ = w.Write([]byte(`null`))
		case "/Library/VirtualFolders":
			_, _ = w.Write([]byte(`null`))
		case "/Users/u1/Items":
			_, _ = w.Write([]byte(`{"Items":null,"TotalRecordCount":0}`))
		default:
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer srv.Close()

	client, _ := NewSourceClient(srv.URL, "k", WithHTTPClient(srv.Client()), WithMaxRetries(1), WithBaseBackoff(time.Millisecond))
	if _, err := client.TestConnection(context.Background()); err == nil || !strings.Contains(err.Error(), "decode") {
		t.Fatalf("expected decode error after retry, got %v", err)
	}
	users, err := client.ListUsers(context.Background())
	if err != nil || users == nil || len(users) != 0 {
		t.Fatalf("expected empty non-nil users, got %v err=%v", users, err)
	}
	folders, err := client.ListVirtualFolders(context.Background())
	if err != nil || folders == nil || len(folders) != 0 {
		t.Fatalf("expected empty non-nil folders, got %v err=%v", folders, err)
	}
	page, err := client.FetchUserItemsPage(context.Background(), "u1", -1, 0)
	if err != nil || page.Items == nil || len(page.Items) != 0 {
		t.Fatalf("expected empty non-nil page items, got %+v err=%v", page, err)
	}

	// Network failure with closed server triggers retry exhaustion
	closedSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	closedURL := closedSrv.URL
	closedSrv.Close()
	netClient, _ := NewSourceClient(closedURL, "k", WithMaxRetries(1), WithBaseBackoff(time.Millisecond))
	if _, err := netClient.TestConnection(context.Background()); err == nil {
		t.Fatal("expected network error against closed server")
	}
}

func TestMigratorEdgeCasesErrorsAndAmbiguousFallbacks(t *testing.T) {
	// Nil store guard
	nilMigrator := NewMigrator(nil, nil)
	if _, err := nilMigrator.Run(context.Background(), MigrationRequest{SourceURL: "http://127.0.0.1:8096", APIKey: "k"}); err == nil {
		t.Fatal("expected error for nil store in Run")
	}
	if _, err := nilMigrator.StartAsync(MigrationRequest{SourceURL: "http://127.0.0.1:8096", APIKey: "k"}); err == nil {
		t.Fatal("expected error for nil store in StartAsync")
	}

	st, userRepo, libRepo := openTestStore(t)
	ctx := context.Background()
	lib, _ := libRepo.CreateLibrary(ctx, "Lib", "movies", []string{"/m"})

	// Create ambiguous title+year movies, ambiguous title-only movies, and ambiguous episodes
	for i := 0; i < 2; i++ {
		_ = libRepo.UpsertItem(ctx, &sqlite.ItemRecord{
			LibraryID:      lib.ID,
			Type:           "Movie",
			Name:           "Ambiguous Title",
			ProductionYear: intPtr(2022),
		})
		_ = libRepo.UpsertItem(ctx, &sqlite.ItemRecord{
			LibraryID:         lib.ID,
			Type:              "Episode",
			Name:              "EpDup",
			ParentIndexNumber: intPtr(2),
			IndexNumber:       intPtr(3),
			Extra:             sqlite.ItemExtra{SeriesName: "Dup Show"},
		})
	}

	// TestSource error paths (invalid URL, /System/Info fail, /Users fail, /Library/VirtualFolders fail)
	var failStage atomic.Int32
	errSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		stage := failStage.Load()
		switch r.URL.Path {
		case "/System/Info":
			if stage == 1 {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			_ = json.NewEncoder(w).Encode(SourceServerInfo{ID: "s", ServerName: "ErrSrv"})
		case "/Users":
			if stage == 2 {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			_ = json.NewEncoder(w).Encode([]SourceUser{{ID: "u1", Name: "Tester"}})
		case "/Library/VirtualFolders":
			if stage == 3 {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			_ = json.NewEncoder(w).Encode([]SourceVirtualFolder{})
		case "/Users/u1/Items":
			if stage == 4 {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			start, _ := strconv.Atoi(r.URL.Query().Get("StartIndex"))
			all := []SourceItem{
				{ID: "a1", Name: "Ambiguous Title", Type: "Movie", ProductionYear: intPtr(2022), UserData: &SourceUserData{Played: true}},
				{ID: "a2", Name: "Ambiguous Title", Type: "Movie", UserData: &SourceUserData{Played: true}},
				{ID: "a3", Name: "EpDup", Type: "Episode", SeriesName: "Dup Show", ParentIndexNumber: intPtr(2), IndexNumber: intPtr(3), UserData: &SourceUserData{Played: true}},
			}
			if start >= len(all) {
				_ = json.NewEncoder(w).Encode(SourceItemsPage{Items: []SourceItem{}, TotalRecordCount: 0})
				return
			}
			_ = json.NewEncoder(w).Encode(SourceItemsPage{Items: all[start:], TotalRecordCount: 0})
		}
	}))
	defer errSrv.Close()

	m := NewMigrator(st, userRepo, WithPageSize(10))
	if _, err := m.TestSource(ctx, "bad://url", "k"); err == nil {
		t.Fatal("expected invalid url error in TestSource")
	}
	if _, err := m.Run(ctx, MigrationRequest{SourceURL: "bad://url", APIKey: "k"}); err == nil {
		t.Fatal("expected invalid url error in Run")
	}
	if _, err := m.StartAsync(MigrationRequest{SourceURL: "bad://url", APIKey: "k"}); err == nil {
		t.Fatal("expected invalid url error in StartAsync")
	}

	failStage.Store(1)
	if _, err := m.TestSource(ctx, errSrv.URL, "k"); err == nil {
		t.Fatal("expected /System/Info error in TestSource")
	}
	if rep, err := m.Run(ctx, MigrationRequest{SourceURL: errSrv.URL, APIKey: "k"}); err == nil || rep.Status != StatusFailed {
		t.Fatalf("expected failed run on /System/Info error, rep=%+v err=%v", rep, err)
	}

	failStage.Store(2)
	if _, err := m.TestSource(ctx, errSrv.URL, "k"); err == nil {
		t.Fatal("expected /Users error in TestSource")
	}
	if rep, err := m.Run(ctx, MigrationRequest{SourceURL: errSrv.URL, APIKey: "k"}); err == nil || rep.Status != StatusFailed {
		t.Fatalf("expected failed run on /Users error, rep=%+v err=%v", rep, err)
	}

	failStage.Store(3)
	if _, err := m.TestSource(ctx, errSrv.URL, "k"); err == nil {
		t.Fatal("expected /Library/VirtualFolders error in TestSource")
	}

	failStage.Store(4)
	if rep, err := m.Run(ctx, MigrationRequest{SourceURL: errSrv.URL, APIKey: "k", MigrateUsers: true, MigratePlayed: true}); err == nil || rep.Status != StatusFailed {
		t.Fatalf("expected failed run on /Items error, rep=%+v err=%v", rep, err)
	}

	// Corrupt checkpoint JSON is gracefully ignored
	cpKey := checkpointKeyFor(errSrv.URL, nil)
	_ = userRepo.SetServerConfig(ctx, cpKey, "{corrupt-json")
	if _, ok := m.loadCheckpoint(ctx, cpKey); ok {
		t.Fatal("expected corrupt checkpoint to return ok=false")
	}

	// Resume from mid-user NextStartIndex = 1 (skips first item, hits title-only & episode ambiguity)
	_ = m.saveCheckpoint(ctx, cpKey, checkpointState{CurrentUserID: "u1", NextStartIndex: 1})
	failStage.Store(0)
	rep, err := m.Run(ctx, MigrationRequest{
		SourceURL:     errSrv.URL,
		APIKey:        "k",
		MigrateUsers:  true,
		MigratePlayed: true,
	})
	if err != nil || !rep.ResumedFromCheckpoint {
		t.Fatalf("expected resumed run, rep=%+v err=%v", rep, err)
	}
	if len(rep.Conflicts) != 2 {
		t.Fatalf("expected 2 ambiguity conflicts (title-only & episode), got %+v", rep.Conflicts)
	}
	if normalizeTitle("") != "" {
		t.Fatal("expected empty normalized title")
	}
}

func TestMigratorProtectsExistingLocalAdminFromDowngradeAndDisable(t *testing.T) {
	st, userRepo, _ := openTestStore(t)
	ctx := context.Background()

	// Seed an active local administrator account.
	localAdmin := &sqlite.UserRecord{
		Name:       "SuperAdmin",
		IsAdmin:    true,
		IsDisabled: false,
		Policy:     sqlite.DefaultUserPolicy(true),
		Config:     sqlite.DefaultUserConfig(),
	}
	if err := userRepo.CreateUser(ctx, localAdmin); err != nil {
		t.Fatalf("CreateUser localAdmin: %v", err)
	}

	// Source server has a user with the same name who is NOT admin and IS disabled.
	srcPolicy := sqlite.DefaultUserPolicy(false)
	srcPolicy.IsAdministrator = false
	srcPolicy.IsDisabled = true

	fake := &fakeEmbyServer{
		apiKey:     "k",
		serverInfo: SourceServerInfo{ID: "s1", ServerName: "Legacy"},
		users: []SourceUser{
			{ID: "src-admin", Name: "SuperAdmin", Policy: srcPolicy, Configuration: sqlite.DefaultUserConfig()},
		},
		itemsByUser: map[string][]SourceItem{
			"src-admin": {},
		},
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
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if rep.Status != StatusCompleted || rep.UsersMatched != 1 {
		t.Fatalf("unexpected report: %+v", rep)
	}

	after, err := userRepo.GetUserByName(ctx, "SuperAdmin")
	if err != nil {
		t.Fatalf("GetUserByName: %v", err)
	}
	if !after.IsAdmin || !after.Policy.IsAdministrator {
		t.Fatalf("local admin was downgraded! IsAdmin=%v Policy.IsAdministrator=%v", after.IsAdmin, after.Policy.IsAdministrator)
	}
	if after.IsDisabled || after.Policy.IsDisabled {
		t.Fatalf("local admin was disabled! IsDisabled=%v Policy.IsDisabled=%v", after.IsDisabled, after.Policy.IsDisabled)
	}

	foundProtectionNotice := false
	for _, n := range rep.Notices {
		if strings.Contains(n, "protected") {
			foundProtectionNotice = true
			break
		}
	}
	if !foundProtectionNotice {
		t.Fatalf("expected protection notice in Notices, got %+v", rep.Notices)
	}
}

func TestMigratorM05SelfLoopEnabledFoldersMappingBatchClampAndCaps(t *testing.T) {
	st, userRepo, libRepo := openTestStore(t)
	ctx := context.Background()

	localMovies, err := libRepo.CreateLibrary(ctx, "MoviesLib", "movies", []string{"/mnt/nas/movies"})
	if err != nil {
		t.Fatal(err)
	}
	localAnime, err := libRepo.CreateLibrary(ctx, "AnimeLib", "tvshows", []string{"/mnt/nas/anime"})
	if err != nil {
		t.Fatal(err)
	}

	userPolicy := sqlite.DefaultUserPolicy(false)
	userPolicy.EnableAllFolders = false
	userPolicy.EnabledFolders = []string{"src-guid-movies", "src-guid-anime", "src-guid-unmapped"}

	// Create 520 ambiguous items to verify 500-entry report cap on Conflicts
	for i := 0; i < 2; i++ {
		_ = libRepo.UpsertItem(ctx, &sqlite.ItemRecord{
			LibraryID: localMovies.ID,
			Type:      "Movie",
			Name:      "DupCapMovie",
			Extra:     sqlite.ItemExtra{ProviderIDs: map[string]string{"Tmdb": "777777"}},
		})
	}
	manyItems := make([]SourceItem, 520)
	for i := range manyItems {
		manyItems[i] = SourceItem{
			ID:          "src-cap-" + strconv.Itoa(i),
			Name:        "DupCapMovie",
			Type:        "Movie",
			ProviderIDs: map[string]string{"Tmdb": "777777"},
			UserData:    &SourceUserData{Played: true},
		}
	}

	fake := &fakeEmbyServer{
		apiKey:     "k",
		serverInfo: SourceServerInfo{ID: "src-srv-m05", ServerName: "SourceEmby"},
		users: []SourceUser{
			{ID: "u-mapped", Name: "MappedUser", Policy: userPolicy, Configuration: sqlite.DefaultUserConfig()},
		},
		folders: []SourceVirtualFolder{
			{Name: "DifferentName", ItemID: "src-guid-movies", CollectionType: "movies", Locations: []string{"/mnt/nas/movies"}},
			{Name: "animelib", ItemID: "src-guid-anime", CollectionType: "tvshows", Locations: []string{"/old/path/anime"}},
			{Name: "UnmappedVault", ItemID: "src-guid-unmapped", CollectionType: "movies", Locations: []string{"/nonexistent/vault"}},
		},
		itemsByUser: map[string][]SourceItem{
			"u-mapped": manyItems,
		},
	}
	srv := newFakeEmbyServer(t, fake)

	// 1. Self-connection prevention by listen address and by ServerID
	if _, err := NewSourceClient(srv.URL, "k", WithHTTPClient(srv.Client()), WithSelfListenAddr(strings.TrimPrefix(srv.URL, "http://"))); !errors.Is(err, ErrSelfSourceServer) {
		t.Fatalf("expected ErrSelfSourceServer on self listen address, got %v", err)
	}
	selfIDClient, err := NewSourceClient(srv.URL, "k", WithHTTPClient(srv.Client()), WithSelfServerID("src-srv-m05"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := selfIDClient.TestConnection(ctx); !errors.Is(err, ErrSelfSourceServer) {
		t.Fatalf("expected ErrSelfSourceServer on matching ServerID, got %v", err)
	}

	// 2. EnabledFolders mapping + SetOnUserUpdated callback + BatchSize > 1000 clamp + 500-conflict cap
	var updatedUsers []string
	m := NewMigrator(st, userRepo, WithClientFactory(func(baseURL, apiKey string) (*SourceClient, error) {
		return NewSourceClient(baseURL, apiKey, WithHTTPClient(srv.Client()))
	}))
	m.SetSelfServer(":65531", "ambra-local-id")
	t.Cleanup(func() { SetSelfServerIdentity("", "") })
	m.SetOnUserUpdated(func(uid string) {
		updatedUsers = append(updatedUsers, uid)
	})

	rep, err := m.Run(ctx, MigrationRequest{
		SourceURL:     srv.URL,
		APIKey:        "k",
		MigrateUsers:  true,
		MigratePlayed: true,
		BatchSize:     99999,
	})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if len(rep.Conflicts) != maxReportEntries {
		t.Fatalf("expected Conflicts capped at %d, got %d", maxReportEntries, len(rep.Conflicts))
	}

	uRec, err := userRepo.GetUserByName(ctx, "MappedUser")
	if err != nil {
		t.Fatal(err)
	}
	if uRec.Policy.EnableAllFolders {
		t.Fatal("expected EnableAllFolders=false preserved")
	}
	if len(uRec.Policy.EnabledFolders) != 2 || uRec.Policy.EnabledFolders[0] != localMovies.ID || uRec.Policy.EnabledFolders[1] != localAnime.ID {
		t.Fatalf("expected EnabledFolders mapped to [%s, %s], got %v", localMovies.ID, localAnime.ID, uRec.Policy.EnabledFolders)
	}

	// Re-run against existing user to trigger UpdateUserPolicy + SetOnUserUpdated
	updatedUsers = nil
	if _, err := m.Run(ctx, MigrationRequest{
		SourceURL:    srv.URL,
		APIKey:       "k",
		MigrateUsers: true,
	}); err != nil {
		t.Fatal(err)
	}
	if len(updatedUsers) != 1 || updatedUsers[0] != uRec.ID {
		t.Fatalf("expected SetOnUserUpdated invoked with %s, got %v", uRec.ID, updatedUsers)
	}
}
