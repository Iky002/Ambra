package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/ambra-media/ambra/internal/api/handlers/audio"
	"github.com/ambra-media/ambra/internal/api/handlers/catalog"
	"github.com/ambra-media/ambra/internal/api/handlers/collections"
	"github.com/ambra-media/ambra/internal/api/handlers/compatcore"
	"github.com/ambra-media/ambra/internal/api/handlers/compatext"
	"github.com/ambra-media/ambra/internal/api/handlers/compatmedia"
	"github.com/ambra-media/ambra/internal/api/handlers/dlna"
	"github.com/ambra-media/ambra/internal/api/handlers/environment"
	govhandler "github.com/ambra-media/ambra/internal/api/handlers/governance"
	"github.com/ambra-media/ambra/internal/api/handlers/hls"
	"github.com/ambra-media/ambra/internal/api/handlers/library"
	"github.com/ambra-media/ambra/internal/api/handlers/livetv"
	"github.com/ambra-media/ambra/internal/api/handlers/logs"
	migrationhandler "github.com/ambra-media/ambra/internal/api/handlers/migration"
	"github.com/ambra-media/ambra/internal/api/handlers/notifications"
	"github.com/ambra-media/ambra/internal/api/handlers/ops"
	"github.com/ambra-media/ambra/internal/api/handlers/pluginadmin"
	"github.com/ambra-media/ambra/internal/api/handlers/quickconnect"
	"github.com/ambra-media/ambra/internal/api/handlers/streaming"
	syncapi "github.com/ambra-media/ambra/internal/api/handlers/sync"
	"github.com/ambra-media/ambra/internal/api/handlers/system"
	"github.com/ambra-media/ambra/internal/api/handlers/tasks"
	"github.com/ambra-media/ambra/internal/api/handlers/users"
	"github.com/ambra-media/ambra/internal/api/middleware"
	"github.com/ambra-media/ambra/internal/api/ws"
	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/core/auth"
	"github.com/ambra-media/ambra/internal/core/budget"
	"github.com/ambra-media/ambra/internal/core/dvr"
	"github.com/ambra-media/ambra/internal/core/governance"
	"github.com/ambra-media/ambra/internal/core/imaging"
	"github.com/ambra-media/ambra/internal/core/migration"
	"github.com/ambra-media/ambra/internal/core/playback"
	"github.com/ambra-media/ambra/internal/core/rankings"
	"github.com/ambra-media/ambra/internal/core/scanner"
	"github.com/ambra-media/ambra/internal/core/scheduler"
	"github.com/ambra-media/ambra/internal/core/session"
	"github.com/ambra-media/ambra/internal/core/ssdp"
	"github.com/ambra-media/ambra/internal/core/transcode"
	"github.com/ambra-media/ambra/internal/core/watcher"
	"github.com/ambra-media/ambra/internal/pkg/eventbus"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/pkg/netutil"
	"github.com/ambra-media/ambra/internal/plugins"
	"github.com/ambra-media/ambra/internal/plugins/ext"
	"github.com/ambra-media/ambra/internal/plugins/nfo"
	"github.com/ambra-media/ambra/internal/plugins/strm"
	"github.com/ambra-media/ambra/internal/plugins/tmdb"
	"github.com/ambra-media/ambra/internal/plugins/trakt"
	"github.com/ambra-media/ambra/internal/plugins/tvdb"
	"github.com/ambra-media/ambra/internal/plugins/wasmhost"
	"github.com/ambra-media/ambra/internal/store/sqlite"
	"github.com/ambra-media/ambra/web"
)

type Server struct {
	Config       *config.Config
	Store        *sqlite.Store
	Bus          *eventbus.Bus
	Registry     *plugins.Registry
	PluginLoader *ext.Loader
	WasmHost     *wasmhost.Host
	Auth         *auth.Service
	Sessions     *session.Manager
	Scanner      *scanner.Scanner
	Watcher      *watcher.Watcher
	Budget       *budget.Pool
	Scheduler    *scheduler.Manager
	Rankings     *rankings.Service
	Migrator     *migration.Migrator
	Governance   *governance.Service
	Playback     *playback.Engine
	Transcode    *transcode.Manager
	DVR          *dvr.Recorder
	LiveTV       *livetv.Handler
	SSDP         *ssdp.Broadcaster
	Imaging      *imaging.Service
	Logs         *logs.Handler
	WSHub        *ws.Hub
	Handler      http.Handler

	dvrCancel context.CancelFunc
	dvrDone   chan struct{}
}

func New(ctx context.Context, cfg *config.Config) (*Server, error) {
	netutil.SetTrustedProxies(cfg.TrustedProxies)
	if err := applyPendingDatabaseRestore(cfg.DataDir, cfg.DBPath()); err != nil {
		return nil, fmt.Errorf("apply pending database restore: %w", err)
	}
	store, err := sqlite.Open(cfg.DBPath())
	if err != nil {
		return nil, fmt.Errorf("open store: %w", err)
	}

	bus := eventbus.New()
	registry := plugins.NewRegistry()
	// Register built-in official plugins
	registry.Register(nfo.New())
	registry.Register(tmdb.New(cfg.TMDBApiKey, cfg.TMDBLanguage))
	registry.Register(tvdb.New())
	registry.Register(strm.New(cfg.EnableSTRM302))
	traktPlugin := trakt.New()
	registry.Register(traktPlugin)

	// Discover external HTTP & WASM (wazero) plugins in data/plugins/*
	pluginsDir := filepath.Join(cfg.DataDir, "plugins")
	extLoader := ext.NewLoader()
	_, _ = extLoader.LoadFromDir(pluginsDir, registry)

	wHost, err := wasmhost.NewHost(ctx)
	if err == nil {
		_, _ = wHost.LoadFromDir(ctx, pluginsDir, registry)
	}

	userRepo := sqlite.NewUserRepo(store)
	libRepo := sqlite.NewLibraryRepo(store)
	traktPlugin.SubscribeEvents(bus, libRepo)

	authSvc := auth.NewService(userRepo, cfg)
	if err := authSvc.EnsureDefaultAdmin(ctx); err != nil {
		_ = store.Close()
		return nil, fmt.Errorf("ensure default admin: %w", err)
	}

	sessionMgr := session.NewManager(userRepo, cfg, bus)
	sessionMgr.SetItemAccessChecker(buildSessionItemAccessChecker(libRepo))
	authMW := middleware.NewAuth(authSvc, sessionMgr)
	wsHub := ws.NewHub(authSvc, sessionMgr, bus)
	wsHub.SetAllowedOrigins(cfg.AllowedOrigins)
	wsHub.SetLibraryChangeFilter(buildWSLibraryChangeFilter(userRepo, libRepo))

	bgBudget := budget.Global()
	sc := scanner.New(libRepo, registry, bus, cfg)
	sc.SetBudget(bgBudget)
	libWatcher, _ := watcher.New(func(c context.Context, libraryID string, paths []string) {
		for _, p := range paths {
			_, _ = sc.ScanPath(c, libraryID, p)
		}
	})
	if libWatcher != nil {
		if libs, err := libRepo.ListLibraries(ctx); err == nil {
			for _, l := range libs {
				_ = libWatcher.WatchLibrary(l.ID, l.Paths)
			}
		}
	}
	rankingsSvc := rankings.New(store, bus, rankings.Options{
		TMDBAPIKey: cfg.TMDBApiKey,
	})
	_ = rankingsSvc.LoadCache(ctx)
	sc.SetOnScanComplete(func(c context.Context, _ string, _ scanner.ScanStats) {
		_, _, _ = rankingsSvc.ApplyTagsToLibrary(c)
	})
	pb := playback.New(libRepo, registry, cfg)
	imgSvc := imaging.New(libRepo).WithCacheDir(cfg.ImageCacheDir())
	transcodeMgr := transcode.NewManager(cfg)
	dvrRec := dvr.NewRecorder(dvr.Options{
		DataDir:    cfg.DataDir,
		FFmpegPath: cfg.FFmpegPath,
		Bus:        bus,
	})
	var ssdpB *ssdp.Broadcaster
	if cfg.EnableDLNA {
		ssdpB = ssdp.NewBroadcaster(ssdp.Config{
			ServerID:      cfg.ServerID,
			ServerName:    cfg.ServerName,
			ServerVersion: config.ServerVersion,
		})
	}

	taskMgr := scheduler.New(bus)
	taskMgr.SetActiveTranscodeDirsFn(transcodeMgr.ActiveWorkDirs)
	taskMgr.RegisterBuiltinTasks(
		func(c context.Context) error {
			if _, err := sc.ScanAll(c); err != nil {
				return err
			}
			_, _, _ = rankingsSvc.ApplyTagsToLibrary(c)
			return nil
		},
		cfg.TranscodeTempDir(),
		store.Writer,
	)
	dayTicks := int64(24 * 3600 * 10_000_000)
	taskMgr.Register(scheduler.TaskDefinition{
		ID:          "sync-rankings-top250",
		Key:         "SyncRankings",
		Name:        "同步豆瓣 Top 250、IMDb Top 250 与 TMDB 热门榜单",
		Category:    "Library",
		Description: "定期拉取豆瓣 Top 250、IMDb Top 250 与 TMDB 热门趋势榜单缓存至后端，并为匹配影片与剧集自动追加榜单标签",
		Triggers: []scheduler.TaskTriggerInfo{
			{Type: "IntervalTrigger", IntervalTicks: &dayTicks},
		},
		Run: func(c context.Context, reportProgress func(float64)) error {
			reportProgress(10)
			_, err := rankingsSvc.SyncAndApply(c)
			reportProgress(100)
			return err
		},
	})

	taskMgr.SetTriggerStore(
		func(c context.Context) (map[string][]scheduler.TaskTriggerInfo, error) {
			var raw string
			err := store.Reader.QueryRowContext(c, `SELECT value_json FROM server_config WHERE key = 'scheduler:triggers'`).Scan(&raw)
			if err != nil {
				return nil, nil
			}
			var out map[string][]scheduler.TaskTriggerInfo
			if err := json.Unmarshal([]byte(raw), &out); err != nil {
				return nil, err
			}
			return out, nil
		},
		func(c context.Context, triggers map[string][]scheduler.TaskTriggerInfo) error {
			raw, err := json.Marshal(triggers)
			if err != nil {
				return err
			}
			_, err = store.Writer.ExecContext(c,
				`INSERT INTO server_config (key, value_json, updated_at) VALUES ('scheduler:triggers', ?, ?)
				 ON CONFLICT(key) DO UPDATE SET value_json = excluded.value_json, updated_at = excluded.updated_at`,
				string(raw), time.Now().UTC().Unix(),
			)
			return err
		},
	)
	if err := taskMgr.LoadPersistedTriggers(ctx); err != nil {
		slog.Warn("failed to load persisted scheduler triggers", "error", err)
	}

	pluginAdminH := pluginadmin.New(registry, store)
	if err := pluginAdminH.ApplySavedConfigs(ctx); err != nil {
		slog.Warn("some saved plugin configs failed to apply", "error", err)
	}

	systemHandler := system.NewHandler(cfg, userRepo, registry).WithSetupDeps(system.SetupDeps{
		Auth: authSvc,
		TriggerScan: func(c context.Context) {
			_, _ = sc.ScanAll(c)
		},
		SyncRankings: func(c context.Context) {
			_, _ = rankingsSvc.SyncAndApply(c)
		},
	})
	usersHandler := users.NewHandler(authSvc, userRepo, sessionMgr, wsHub, cfg)
	usersHandler.SetLibraryRepo(libRepo)

	userIDFromCtx := func(c context.Context) string {
		if ac, ok := middleware.AuthFromContext(c); ok && ac != nil {
			return ac.UserID()
		}
		return ""
	}

	mediaMux := http.NewServeMux()
	subExtractor := transcode.NewExtractor(cfg)
	// Phase 1: Library & Direct Streaming
	libH := library.New(libRepo, sc, cfg)
	libH.SetUserRepo(userRepo)
	libH.SetWatcher(libWatcher)
	libH.SetStore(store)
	libH.RegisterRoutes(mediaMux, library.AuthFuncs{
		RequireAuth:   authMW.RequireAuth,
		RequireAdmin:  authMW.RequireAdmin,
		UserIDFromCtx: userIDFromCtx,
	})
	streamH := streaming.New(libRepo, pb, imgSvc)
	streamH.SetExtractor(subExtractor)
	streamH.SetUserRepo(userRepo)
	streamH.RegisterRoutes(mediaMux, streaming.AuthFuncs{
		RequireAuth:   authMW.RequireAuth,
		RequireAdmin:  authMW.RequireAdmin,
		UserIDFromCtx: userIDFromCtx,
	})

	// Phase 2: Environment, Collections/Playlists, Catalog/Filters, ScheduledTasks, Logs, PluginAdmin
	environment.New(cfg).RegisterRoutes(mediaMux, environment.AuthFuncs{
		RequireAuth:   authMW.RequireAuth,
		RequireAdmin:  authMW.RequireAdmin,
		UserIDFromCtx: userIDFromCtx,
	})
	collectionsH := collections.New(libRepo, cfg)
	collectionsH.SetUserRepo(userRepo)
	collectionsH.SetStore(store)
	collectionsH.RegisterRoutes(mediaMux, collections.AuthFuncs{
		RequireAuth:   authMW.RequireAuth,
		RequireAdmin:  authMW.RequireAdmin,
		UserIDFromCtx: userIDFromCtx,
	})
	catalogH := catalog.New(libRepo, cfg)
	catalogH.SetUserRepo(userRepo)
	catalogH.RegisterRoutes(mediaMux, catalog.AuthFuncs{
		RequireAuth:   authMW.RequireAuth,
		RequireAdmin:  authMW.RequireAdmin,
		UserIDFromCtx: userIDFromCtx,
	})
	tasks.New(taskMgr).RegisterRoutes(mediaMux, tasks.AuthFuncs{
		RequireAuth:   authMW.RequireAuth,
		RequireAdmin:  authMW.RequireAdmin,
		UserIDFromCtx: userIDFromCtx,
	})
	logsH := logs.New(cfg)
	logsH.RegisterRoutes(mediaMux, logs.AuthFuncs{
		RequireAuth:   authMW.RequireAuth,
		RequireAdmin:  authMW.RequireAdmin,
		UserIDFromCtx: userIDFromCtx,
	})
	pluginAdminH.RegisterRoutes(mediaMux, pluginadmin.AuthFuncs{
		RequireAuth:   authMW.RequireAuth,
		RequireAdmin:  authMW.RequireAdmin,
		UserIDFromCtx: userIDFromCtx,
	})

	// Phase 3: FFmpeg Dynamic HLS, Encoding, LiveStreams & Audio
	resolveStreamURL := func(c context.Context, rawPath string) (string, error) {
		res, err := pb.ResolveStream(c, rawPath)
		if err != nil || res == nil {
			return "", err
		}
		return res.URL, nil
	}
	hlsH := hls.New(libRepo, transcodeMgr, cfg)
	hlsH.SetUserRepo(userRepo)
	hlsH.SetStreamResolver(resolveStreamURL)
	hlsH.RegisterRoutes(mediaMux, hls.AuthFuncs{
		RequireAuth:   authMW.RequireAuth,
		RequireAdmin:  authMW.RequireAdmin,
		UserIDFromCtx: userIDFromCtx,
	})
	audioH := audio.New(libRepo, cfg)
	audioH.SetUserRepo(userRepo)
	audioH.SetStreamResolver(resolveStreamURL)
	audioH.RegisterRoutes(mediaMux, audio.AuthFuncs{
		RequireAuth:   authMW.RequireAuth,
		RequireAdmin:  authMW.RequireAdmin,
		UserIDFromCtx: userIDFromCtx,
	})

	// Phase 4 & 5: LiveTV, Sync, Notifications, Startup, Packages, Webhooks, BackupRestore, OpenAPI, GenericUI, Parties, Connect
	livetvH := livetv.New(store, cfg)
	livetvH.SetUserRepo(userRepo)
	livetvH.SetRecorder(dvrRec)
	dvrRec.SetOnFinished(livetvH.OnRecordingFinished)
	livetvH.RegisterRoutes(mediaMux, livetv.AuthFuncs{
		RequireAuth:   authMW.RequireAuth,
		RequireAdmin:  authMW.RequireAdmin,
		UserIDFromCtx: userIDFromCtx,
	})
	syncH := syncapi.New(store, cfg)
	syncH.SetLibraryRepo(libRepo)
	syncH.SetUserRepo(userRepo)
	syncH.RegisterRoutes(mediaMux, syncapi.AuthFuncs{
		RequireAuth:   authMW.RequireAuth,
		RequireAdmin:  authMW.RequireAdmin,
		UserIDFromCtx: userIDFromCtx,
	})
	notifH := notifications.New(store)
	notifH.SubscribeEvents(bus)
	notifH.RegisterRoutes(mediaMux, notifications.AuthFuncs{
		RequireAuth:   authMW.RequireAuth,
		RequireAdmin:  authMW.RequireAdmin,
		UserIDFromCtx: userIDFromCtx,
	})
	ops.New(store, cfg, userRepo, bus).RegisterRoutes(mediaMux, ops.AuthFuncs{
		RequireAuth:   authMW.RequireAuth,
		RequireAdmin:  authMW.RequireAdmin,
		UserIDFromCtx: userIDFromCtx,
	})

	// Phase 6: DLNA UPnP MediaServer, Rankings, Migration & Governance Admin APIs
	dlnaH := dlna.New(cfg, libRepo, store)
	bus.Subscribe(eventbus.EventLibraryChanged, func(context.Context, eventbus.Event) {
		dlnaH.BumpSystemUpdate()
	})
	dlnaH.RegisterRoutes(mediaMux, dlna.AuthFuncs{
		RequireAuth:   authMW.RequireAuth,
		RequireAdmin:  authMW.RequireAdmin,
		UserIDFromCtx: userIDFromCtx,
	})
	rankings.NewHandler(rankingsSvc).RegisterRoutes(mediaMux, rankings.AuthFuncs{
		RequireAuth:  authMW.RequireAuth,
		RequireAdmin: authMW.RequireAdmin,
	})
	migrator := migration.NewMigrator(store, userRepo)
	migrationhandler.New(migrator).RegisterRoutes(mediaMux, migrationhandler.AuthFuncs{
		RequireAuth:  authMW.RequireAuth,
		RequireAdmin: authMW.RequireAdmin,
	})
	govSvc, err := governance.NewService(governance.Options{
		Store:      store,
		Config:     cfg,
		Scheduler:  taskMgr,
		BudgetPool: bgBudget,
	})
	if err != nil {
		if libWatcher != nil {
			_ = libWatcher.Close()
		}
		if wHost != nil {
			_ = wHost.Close(context.Background())
		}
		_ = store.Close()
		return nil, fmt.Errorf("init governance service: %w", err)
	}
	govhandler.New(govSvc).RegisterRoutes(mediaMux, govhandler.AuthFuncs{
		RequireAuth:   authMW.RequireAuth,
		RequireAdmin:  authMW.RequireAdmin,
		UserIDFromCtx: userIDFromCtx,
	})
	qcH := quickconnect.New(authSvc, userRepo, sessionMgr)
	qcH.SetConfig(cfg)
	qcH.RegisterRoutes(mediaMux, quickconnect.AuthFuncs{
		RequireAuth:   authMW.RequireAuth,
		RequireAdmin:  authMW.RequireAdmin,
		UserIDFromCtx: userIDFromCtx,
	})

	// Emby 4.10.2.0 full 582-route compatibility routers (zero ServeMux conflicts)
	compatMediaSvc := compatmedia.New(libRepo, sc, imgSvc, registry, store, cfg)
	compatMediaSvc.SetExtractor(subExtractor)
	compatMediaSvc.SetWatcher(libWatcher)
	compatMediaSvc.SetTranscodeManager(transcodeMgr)
	compatMediaSvc.SetUserRepo(userRepo)
	compatMediaH := compatMediaSvc.BuildRouter(compatmedia.AuthFuncs{
		RequireAuth:   authMW.RequireAuth,
		RequireAdmin:  authMW.RequireAdmin,
		UserIDFromCtx: userIDFromCtx,
	})
	compatCoreSvc := compatcore.New(cfg, store, userRepo, libRepo, sessionMgr, registry)
	compatCoreSvc.SetAuthService(authSvc)
	compatCoreSvc.SetWSHub(wsHub)
	compatCoreSvc.SetTaskStopper(func(id string) bool {
		return taskMgr.Stop(id) == nil
	})
	compatCoreH := compatCoreSvc.BuildRouter(compatcore.AuthFuncs{
		RequireAuth:   authMW.RequireAuth,
		RequireAdmin:  authMW.RequireAdmin,
		UserIDFromCtx: userIDFromCtx,
	})
	compatExtSvc := compatext.New(store, cfg)
	compatExtSvc.SetLiveTV(livetvH)
	compatExtSvc.SetSyncHandler(syncH)
	compatExtSvc.SetDVR(dvrRec)
	compatExtSvc.SetLibraryRepo(libRepo)
	compatExtSvc.SetUserRepo(userRepo)
	compatExtH := compatExtSvc.BuildRouter(compatext.AuthFuncs{
		RequireAuth:   authMW.RequireAuth,
		RequireAdmin:  authMW.RequireAdmin,
		UserIDFromCtx: userIDFromCtx,
	})

	fallbackHandler := dispatchMuxAndCompat(mediaMux, compatMediaH, compatCoreH, compatExtH)

	// Web client static file server (embedded dist or external WebDir override)
	webHandler := buildWebHandler(cfg.WebDir)

	r := chi.NewRouter()
	r.Use(middleware.CORSWithOrigins(cfg.AllowedOrigins))
	r.Use(middleware.Gzip)
	r.Use(middleware.EmbyPrefixAndCase)
	r.Use(authMW.ParseAuth)

	wsHub.RegisterRoutes(r)
	system.RegisterRoutes(r, authMW, systemHandler)
	system.RegisterSetupRoutes(r, authMW, systemHandler)
	users.RegisterRoutes(r, authMW, usersHandler)

	// Serve Ambra vector favicon at root
	serveFavicon := func(w http.ResponseWriter, req *http.Request) {
		data, err := fs.ReadFile(web.DistFS, "dist/favicon.svg")
		if err != nil {
			http.NotFound(w, req)
			return
		}
		w.Header().Set("Content-Type", "image/svg+xml")
		w.Header().Set("Cache-Control", "public, max-age=86400")
		_, _ = w.Write(data)
	}
	r.Get("/favicon.ico", serveFavicon)
	r.Get("/favicon.svg", serveFavicon)

	// Serve modern Web client at / and /web/*
	r.Get("/", func(w http.ResponseWriter, req *http.Request) {
		http.Redirect(w, req, "/web/", http.StatusFound)
	})
	r.Handle("/web", http.RedirectHandler("/web/", http.StatusMovedPermanently))
	r.Handle("/web/*", http.StripPrefix("/web/", webHandler))

	// Delegate all library, streaming, Phase 2-6 & full 582-route compat APIs
	r.NotFound(fallbackHandler.ServeHTTP)
	r.MethodNotAllowed(fallbackHandler.ServeHTTP)

	dvrCtx, dvrCancel := context.WithCancel(ctx)
	dvrDone := make(chan struct{})
	taskMgr.Start(dvrCtx)
	go func() {
		defer close(dvrDone)
		dvrRec.SchedulerLoop(dvrCtx, 15*time.Second, livetvH.DueRecordingJobs)
	}()

	return &Server{
		Config:       cfg,
		Store:        store,
		Bus:          bus,
		Registry:     registry,
		PluginLoader: extLoader,
		WasmHost:     wHost,
		Auth:         authSvc,
		Sessions:     sessionMgr,
		Scanner:      sc,
		Watcher:      libWatcher,
		Budget:       bgBudget,
		Scheduler:    taskMgr,
		Rankings:     rankingsSvc,
		Migrator:     migrator,
		Governance:   govSvc,
		Playback:     pb,
		Transcode:    transcodeMgr,
		DVR:          dvrRec,
		LiveTV:       livetvH,
		SSDP:         ssdpB,
		Imaging:      imgSvc,
		Logs:         logsH,
		WSHub:        wsHub,
		Handler:      r,
		dvrCancel:    dvrCancel,
		dvrDone:      dvrDone,
	}, nil
}

func applyPendingDatabaseRestore(dataDir, dbPath string) error {
	if strings.TrimSpace(dataDir) == "" || strings.TrimSpace(dbPath) == "" {
		return nil
	}
	restorePath := filepath.Join(dataDir, "ambra.db.restore")
	info, err := os.Lstat(restorePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		slog.Warn("failed to stat pending database restore file", "path", restorePath, "error", err)
		return fmt.Errorf("stat pending restore %s: %w", restorePath, err)
	}
	if !info.Mode().IsRegular() || info.Size() == 0 {
		slog.Warn("ignoring invalid, symlink, or empty pending database restore file", "path", restorePath, "size", info.Size())
		return nil
	}

	const sqliteMagicHeader = "SQLite format 3\x00"
	rf, err := os.Open(restorePath)
	if err != nil {
		slog.Warn("failed to open pending database restore file for header validation", "path", restorePath, "error", err)
		return nil
	}
	var magic [16]byte
	_, readErr := io.ReadFull(rf, magic[:])
	_ = rf.Close()
	if readErr != nil || string(magic[:]) != sqliteMagicHeader {
		slog.Warn("ignoring pending database restore with invalid SQLite magic header", "path", restorePath, "error", readErr)
		return nil
	}

	backupPath := dbPath + ".pre-restore.bak"
	hadExistingDB := false
	if _, err := os.Stat(dbPath); err == nil {
		hadExistingDB = true
		_ = os.Remove(backupPath)
		if err := os.Rename(dbPath, backupPath); err != nil {
			slog.Warn("failed to back up existing database before restore", "dbPath", dbPath, "backupPath", backupPath, "error", err)
			return fmt.Errorf("backup existing db: %w", err)
		}
		// Preserve WAL/SHM sidecars alongside backup so pre-restore backup stays intact.
		for _, suffix := range []string{"-wal", "-shm"} {
			sidecar := dbPath + suffix
			sidecarBak := backupPath + suffix
			_ = os.Remove(sidecarBak)
			if _, sErr := os.Stat(sidecar); sErr == nil {
				if rErr := os.Rename(sidecar, sidecarBak); rErr != nil {
					slog.Warn("failed to back up sqlite sidecar before restore", "path", sidecar, "error", rErr)
				}
			}
		}
	} else if !os.IsNotExist(err) {
		slog.Warn("failed to stat existing database before restore", "dbPath", dbPath, "error", err)
		return fmt.Errorf("stat existing db: %w", err)
	}

	if err := os.Rename(restorePath, dbPath); err != nil {
		slog.Warn("failed to promote pending database restore", "restorePath", restorePath, "dbPath", dbPath, "error", err)
		if hadExistingDB {
			_ = os.Rename(backupPath, dbPath)
			for _, suffix := range []string{"-wal", "-shm"} {
				_ = os.Rename(backupPath+suffix, dbPath+suffix)
			}
		}
		return fmt.Errorf("promote restore db: %w", err)
	}

	slog.Info("applied pending database restore", "restorePath", restorePath, "dbPath", dbPath, "backupCreated", hadExistingDB, "backupPath", backupPath)
	return nil
}

func (s *Server) Close() error {
	if s.Watcher != nil {
		_ = s.Watcher.Close()
	}
	if s.dvrCancel != nil {
		s.dvrCancel()
	}
	if s.dvrDone != nil {
		<-s.dvrDone
	}
	if s.DVR != nil {
		_ = s.DVR.Close()
	}
	if s.Transcode != nil {
		s.Transcode.Close()
	}
	if s.WasmHost != nil {
		_ = s.WasmHost.Close(context.Background())
	}
	if s.Store != nil {
		return s.Store.Close()
	}
	return nil
}

func dispatchMuxAndCompat(mux *http.ServeMux, routers ...http.Handler) http.Handler {
	chiRouters := make([]chi.Router, 0, len(routers))
	for _, h := range routers {
		if cr, ok := h.(chi.Router); ok {
			chiRouters = append(chiRouters, cr)
		}
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 1. Prefer exact pattern match on primary mediaMux
		if _, pattern := mux.Handler(r); pattern != "" {
			mux.ServeHTTP(w, r)
			return
		}
		// 2. Match against chi compat routers with clean route context
		for _, cr := range chiRouters {
			rctx := chi.NewRouteContext()
			if cr.Match(rctx, r.Method, r.URL.Path) {
				cr.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx)))
				return
			}
		}
		mux.ServeHTTP(w, r)
	})
}

const webContentSecurityPolicy = "default-src 'self'; script-src 'self' 'wasm-unsafe-eval' blob:; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob: https:; media-src 'self' blob: https: http:; connect-src 'self' ws: wss: https:; worker-src 'self' blob:; font-src 'self' data:; object-src 'none'; frame-ancestors 'self'; base-uri 'self'"

func setWebSecurityHeaders(w http.ResponseWriter) {
	h := w.Header()
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("X-Frame-Options", "SAMEORIGIN")
	h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
	h.Set("Content-Security-Policy", webContentSecurityPolicy)
}

func userCanAccessLibrary(user *sqlite.UserRecord, libraryID string) bool {
	if user == nil || user.IsAdmin || user.Policy.IsAdministrator || user.Policy.EnableAllFolders {
		return true
	}
	normLib := guid.Normalize(libraryID)
	if normLib == "" || normLib == "collections" || normLib == "playlists" {
		return true
	}
	for _, allowed := range user.Policy.EnabledFolders {
		if guid.Normalize(allowed) == normLib {
			return true
		}
	}
	return false
}

func buildSessionItemAccessChecker(libRepo *sqlite.LibraryRepo) func(ctx context.Context, user *sqlite.UserRecord, itemID string) bool {
	return func(ctx context.Context, user *sqlite.UserRecord, itemID string) bool {
		if libRepo == nil || itemID == "" {
			return true
		}
		rec, err := libRepo.GetItemRecordByID(ctx, itemID)
		if err != nil || rec == nil {
			return false
		}
		if user == nil || user.IsAdmin || user.Policy.IsAdministrator {
			return true
		}
		if !userCanAccessLibrary(user, rec.LibraryID) {
			return false
		}
		blocked, err := libRepo.IsItemBlockedByParentalPolicy(ctx, rec, user.Policy.MaxParentalRating, user.Policy.BlockedTags)
		return err == nil && !blocked
	}
}

func buildWSLibraryChangeFilter(userRepo *sqlite.UserRepo, libRepo *sqlite.LibraryRepo) func(ctx context.Context, userID string, payload any) (any, bool) {
	itemChecker := buildSessionItemAccessChecker(libRepo)
	return func(ctx context.Context, userID string, payload any) (any, bool) {
		if userRepo == nil || userID == "" {
			return nil, false
		}
		user, err := userRepo.GetUserByID(ctx, userID)
		if err != nil || user == nil || user.IsDisabled {
			return nil, false
		}
		if user.IsAdmin || user.Policy.IsAdministrator {
			return payload, true
		}
		m, ok := payload.(map[string]any)
		if !ok {
			return map[string]any{"LibraryChanged": true}, true
		}

		// Block DVR path leakage and enforce Live TV access policy.
		if src, _ := m["source"].(string); strings.EqualFold(src, "dvr") {
			if !user.Policy.EnableLiveTvAccess {
				return nil, false
			}
			return map[string]any{"Source": "dvr"}, true
		}
		if _, hasOutputPath := m["output_path"]; hasOutputPath {
			if !user.Policy.EnableLiveTvAccess {
				return nil, false
			}
			return map[string]any{"LibraryChanged": true}, true
		}

		libID := ""
		if v, ok := m["LibraryId"].(string); ok {
			libID = guid.Normalize(v)
		} else if v, ok := m["library_id"].(string); ok {
			libID = guid.Normalize(v)
		}
		if libID != "" && !userCanAccessLibrary(user, libID) {
			return nil, false
		}

		// Filter any explicit item ID slices so restricted item IDs are never disclosed.
		hasIDLists := false
		filteredLists := make(map[string][]string)
		anyVisibleID := false
		for _, key := range []string{"ItemsAdded", "ItemsUpdated", "ItemsRemoved"} {
			rawIDs, ok := m[key].([]string)
			if !ok {
				continue
			}
			hasIDLists = true
			kept := make([]string, 0, len(rawIDs))
			for _, id := range rawIDs {
				if itemChecker(ctx, user, id) {
					kept = append(kept, id)
				}
			}
			filteredLists[key] = kept
			if len(kept) > 0 {
				anyVisibleID = true
			}
		}
		if hasIDLists {
			if !anyVisibleID {
				return nil, false
			}
			out := make(map[string]any, len(filteredLists)+1)
			if libID != "" {
				out["LibraryId"] = libID
			}
			for k, v := range filteredLists {
				out[k] = v
			}
			return out, true
		}

		// For library-scoped scan summaries, emit only safe metadata without restricted counts when parental/tag policies apply.
		if libID != "" {
			hasContentPolicy := user.Policy.MaxParentalRating != nil || len(user.Policy.BlockedTags) > 0
			if hasContentPolicy {
				out := map[string]any{"LibraryId": libID}
				if phase, ok := m["Phase"].(string); ok && phase != "" {
					out["Phase"] = phase
				}
				return out, true
			}
			return m, true
		}

		return map[string]any{"LibraryChanged": true}, true
	}
}

func buildWebHandler(webDir string) http.Handler {
	var fsys fs.FS
	if webDir != "" {
		if st, err := os.Stat(webDir); err == nil && st.IsDir() {
			fsys = os.DirFS(webDir)
		}
	}
	if fsys == nil {
		sub, err := fs.Sub(web.DistFS, "dist")
		if err == nil {
			fsys = sub
		}
	}
	if fsys == nil {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			setWebSecurityHeaders(w)
			http.Error(w, "web UI unavailable", http.StatusServiceUnavailable)
		})
	}
	fileServer := http.FileServer(http.FS(fsys))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		setWebSecurityHeaders(w)
		cleaned := path.Clean("/" + r.URL.Path)
		p := strings.TrimPrefix(cleaned, "/")
		if p == "_headers" || p == "_redirects" {
			http.NotFound(w, r)
			return
		}
		if p == "" || p == "index.html" {
			fileServer.ServeHTTP(w, r)
			return
		}
		if _, err := fs.Stat(fsys, p); err == nil {
			fileServer.ServeHTTP(w, r)
			return
		}
		if p == "assets" || strings.HasPrefix(p, "assets/") || p == "ffmpeg" || strings.HasPrefix(p, "ffmpeg/") {
			http.NotFound(w, r)
			return
		}
		// SPA fallback to index.html
		r2 := r.Clone(r.Context())
		r2.URL.Path = "/"
		fileServer.ServeHTTP(w, r2)
	})
}
