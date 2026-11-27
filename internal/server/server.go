package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
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
	"github.com/ambra-media/ambra/internal/api/handlers/usertrakt"
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

	ssdpMu           sync.Mutex
	ssdpCtx          context.Context
	ssdpCancel       context.CancelFunc
	ssdpDone         chan struct{}
	ssdpClosed       bool
	dvrCancel        context.CancelFunc
	dvrDone          chan struct{}
	stopTrakt        func()
	opsHandler       *ops.Handler
	notifHandler     *notifications.Handler
	syncHandler      *syncapi.Handler
	migrationHandler *migrationhandler.Handler
}

func New(ctx context.Context, cfg *config.Config) (*Server, error) {
	snap := cfg.Snapshot()
	netutil.SetTrustedProxies(snap.TrustedProxies)
	netutil.SetLocalNetworkSubnets(snap.LocalNetworkSubnets)
	migration.SetSelfServerIdentity(snap.HTTPAddr, snap.ServerID)
	if err := applyPendingDatabaseRestore(snap.DataDir, cfg.DBPath()); err != nil {
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
	registry.Register(tmdb.New(snap.TMDBApiKey, snap.TMDBLanguage).WithDefaultAPIKey(config.DefaultTMDBApiKey))
	registry.Register(tvdb.New())
	registry.Register(strm.New(snap.EnableSTRM302))
	traktPlugin := trakt.New()
	registry.Register(traktPlugin)

	// Discover external HTTP & WASM (wazero) plugins in data/plugins/*
	pluginsDir := filepath.Join(snap.DataDir, "plugins")
	extLoader := ext.NewLoader()
	if _, lErr := extLoader.LoadFromDir(pluginsDir, registry); lErr != nil {
		slog.Warn("some external HTTP plugins failed to load", "dir", pluginsDir, "error", lErr)
	}

	wHost, err := wasmhost.NewHost(ctx)
	if err == nil {
		if _, wErr := wHost.LoadFromDir(ctx, pluginsDir, registry); wErr != nil {
			slog.Warn("some WASM plugins failed to load", "dir", pluginsDir, "error", wErr)
		}
	} else {
		slog.Warn("failed to initialize WASM plugin host", "error", err)
	}

	userRepo := sqlite.NewUserRepo(store)
	traktPlugin.SetUserConfigStore(userRepo)
	libRepo := sqlite.NewLibraryRepo(store)
	stopTrakt := traktPlugin.SubscribeEvents(bus, libRepo)

	authSvc := auth.NewService(userRepo, cfg)
	if err := authSvc.EnsureDefaultAdmin(ctx); err != nil {
		if stopTrakt != nil {
			stopTrakt()
		}
		if wHost != nil {
			_ = wHost.Close(context.Background())
		}
		_ = store.Close()
		return nil, fmt.Errorf("ensure default admin: %w", err)
	}

	sessionMgr := session.NewManager(userRepo, cfg, bus)
	sessionMgr.SetItemAccessChecker(buildSessionItemAccessChecker(libRepo))
	authMW := middleware.NewAuth(authSvc, sessionMgr)
	authMW.SetConfig(cfg)
	wsHub := ws.NewHub(authSvc, sessionMgr, bus)
	wsHub.SetConfig(cfg)
	wsHub.SetAllowedOrigins(snap.AllowedOrigins)
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
		TMDBAPIKeyFunc: func() string {
			return cfg.Snapshot().TMDBApiKey
		},
		DefaultTMDBAPIKey: config.DefaultTMDBApiKey,
	})
	_ = rankingsSvc.LoadCache(ctx)
	sc.SetOnScanComplete(func(c context.Context, _ string, _ scanner.ScanStats) {
		_, _, _ = rankingsSvc.ApplyTagsToLibrary(c)
	})
	pb := playback.New(libRepo, registry, cfg)
	pb.SetUserRepo(userRepo)
	imgSvc := imaging.New(libRepo).WithCacheDir(cfg.ImageCacheDir()).WithFFmpegPath(snap.FFmpegPath)
	transcodeMgr := transcode.NewManager(cfg)
	transcodeMgr.SetStore(store)
	transcodeMgr.ReloadEncodingOptions(ctx)
	dvrRec := dvr.NewRecorder(dvr.Options{
		DataDir:    snap.DataDir,
		FFmpegPath: snap.FFmpegPath,
		Bus:        bus,
	})

	taskMgr := scheduler.New(bus)
	taskMgr.SetActiveTranscodeDirsFn(transcodeMgr.ActiveWorkDirs)
	taskMgr.SetTranscodeTempDirFn(transcodeMgr.CurrentTempDir)
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

	subExtractor := transcode.NewExtractor(cfg)
	taskMgr.Register(scheduler.TaskDefinition{
		ID:          "extract-chapter-images",
		Key:         "ExtractChapterImages",
		Name:        "提取章节缩略图",
		Category:    "媒体库",
		Description: "为包含章节信息的本地影片批量提取章节缩略图，保存至独立缓存目录（cache/chapters，请谨慎删除，删除后需重新运行本任务）。自动跳过已提取章节与 .strm 远程媒体。",
		Triggers:    []scheduler.TaskTriggerInfo{},
		Run: func(c context.Context, reportProgress func(float64)) error {
			_, err := transcode.ExtractAllChapterImages(c, libRepo, subExtractor, reportProgress)
			return err
		},
	})
	taskMgr.Register(scheduler.TaskDefinition{
		ID:          "generate-bif-thumbnails",
		Key:         "GenerateBifThumbnails",
		Name:        "生成视频预览图 (BIF)",
		Category:    "媒体库",
		Description: "为本地电影与剧集生成标准 Emby/Roku BIF 进度条预览图，保存至独立缓存目录（cache/bif，请谨慎删除）。自动跳过已生成或同目录已含 .bif 的影片及 .strm 远程媒体。",
		Triggers:    []scheduler.TaskTriggerInfo{},
		Run: func(c context.Context, reportProgress func(float64)) error {
			_, err := transcode.GenerateAllBIFThumbnails(c, libRepo, subExtractor, reportProgress)
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
	pluginAdminH.SetPluginsDir(pluginsDir)
	pluginAdminH.SetOnConfigSaved(func(pluginID string, saved map[string]any) {
		normSaved := make(map[string]any, len(saved))
		for k, v := range saved {
			nk := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(k), "_", ""))
			if nk != "" {
				normSaved[nk] = v
			}
		}
		switch pluginID {
		case "ambra.builtin.tmdb":
			cfg.Update(func(c *config.Config) {
				if v, ok := normSaved["apikey"].(string); ok {
					c.TMDBApiKey = strings.TrimSpace(v)
				}
				if v, ok := normSaved["language"].(string); ok && strings.TrimSpace(v) != "" {
					c.TMDBLanguage = strings.TrimSpace(v)
				}
			})
			_ = cfg.Save()
		case "ambra.builtin.strm":
			cfg.Update(func(c *config.Config) {
				if v, ok := normSaved["enable302"].(bool); ok {
					c.EnableSTRM302 = v
				} else if v, ok := normSaved["enablestrm302"].(bool); ok {
					c.EnableSTRM302 = v
				}
			})
			_ = cfg.Save()
		}
	})
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
	systemHandler.SetWSHub(wsHub)
	usersHandler := users.NewHandler(authSvc, userRepo, sessionMgr, wsHub, cfg)
	usersHandler.SetLibraryRepo(libRepo)

	userIDFromCtx := func(c context.Context) string {
		if ac, ok := middleware.AuthFromContext(c); ok && ac != nil {
			return ac.UserID()
		}
		return ""
	}

	mediaMux := http.NewServeMux()
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
	streamH.SetTranscodeManager(transcodeMgr)
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

	usertrakt.New(traktPlugin, userRepo).RegisterRoutes(mediaMux, authMW.RequireAuth)

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
	hlsH.SetStreamResolverWithHeaders(func(c context.Context, rawPath string) (string, map[string]string, error) {
		res, rErr := pb.ResolveStream(c, rawPath)
		if rErr != nil || res == nil {
			return "", nil, rErr
		}
		return res.URL, res.Headers, nil
	})
	hlsH.RegisterRoutes(mediaMux, hls.AuthFuncs{
		RequireAuth:   authMW.RequireAuth,
		RequireAdmin:  authMW.RequireAdmin,
		UserIDFromCtx: userIDFromCtx,
	})
	audioH := audio.New(libRepo, cfg)
	audioH.SetUserRepo(userRepo)
	audioH.SetTranscodeManager(transcodeMgr)
	audioH.SetStreamResolver(resolveStreamURL)
	audioH.SetStreamResolverFull(func(c context.Context, rawPath string) (string, bool, error) {
		res, rErr := pb.ResolveStream(c, rawPath)
		if rErr != nil || res == nil {
			return "", false, rErr
		}
		return res.URL, res.Is302, nil
	})
	audioH.SetStreamResolverWithHeaders(pb.ResolveStream)
	audioH.RegisterRoutes(mediaMux, audio.AuthFuncs{
		RequireAuth:   authMW.RequireAuth,
		RequireAdmin:  authMW.RequireAdmin,
		UserIDFromCtx: userIDFromCtx,
	})

	// Phase 4 & 5: LiveTV, Sync, Notifications, Startup, Packages, Webhooks, BackupRestore, OpenAPI, GenericUI, Parties, Connect
	livetvH := livetv.New(store, cfg)
	livetvH.SetUserRepo(userRepo)
	livetvH.SetLibraryRepo(libRepo)
	livetvH.SetTranscodeManager(transcodeMgr)
	livetvH.SetRecorder(dvrRec)
	dvrRec.SetOnFinished(livetvH.OnRecordingFinished)
	livetvH.RegisterRoutes(mediaMux, livetv.AuthFuncs{
		RequireAuth:   authMW.RequireAuth,
		RequireAdmin:  authMW.RequireAdmin,
		UserIDFromCtx: userIDFromCtx,
	})
	halfDayTicks := int64(12 * 3600 * 10_000_000)
	taskMgr.Register(scheduler.TaskDefinition{
		ID:          "refresh-livetv-guides",
		Key:         "RefreshGuide",
		Name:        "刷新直播电视节目单 (XMLTV EPG)",
		Category:    "LiveTV",
		Description: "定期重新同步已配置的 XMLTV 电子节目单并自动清理过期节目数据",
		Triggers: []scheduler.TaskTriggerInfo{
			{Type: "IntervalTrigger", IntervalTicks: &halfDayTicks},
		},
		Run: func(c context.Context, reportProgress func(float64)) error {
			reportProgress(10)
			err := livetvH.RefreshAllGuides(c)
			reportProgress(100)
			return err
		},
	})
	sixHoursTicks := int64(6 * 3600 * 10_000_000)
	taskMgr.Register(scheduler.TaskDefinition{
		ID:          "refresh-livetv-tuners",
		Key:         "RefreshTunerHosts",
		Name:        "刷新直播电视频道源 (M3U / HDHomeRun)",
		Category:    "LiveTV",
		Description: "定期重新同步已配置 URL 的 M3U 与 HDHomeRun 频道源，无损更新流地址并保留频道排序、禁用状态与 EPG 映射",
		Triggers: []scheduler.TaskTriggerInfo{
			{Type: "IntervalTrigger", IntervalTicks: &sixHoursTicks},
		},
		Run: func(c context.Context, reportProgress func(float64)) error {
			reportProgress(10)
			err := livetvH.RefreshAllTunerHosts(c)
			reportProgress(100)
			return err
		},
	})
	syncH := syncapi.New(store, cfg)
	syncH.SetLibraryRepo(libRepo)
	syncH.SetUserRepo(userRepo)
	syncH.SetBudget(bgBudget)
	syncH.SetFFmpegPath(snap.FFmpegPath)
	syncH.SetStreamResolverWithHeaders(pb.ResolveStream)
	syncH.SubscribeEvents(bus)
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
	opsH := ops.New(store, cfg, userRepo, bus)
	opsH.SetWSHub(wsHub)
	opsH.SetItemAccessChecker(buildSessionItemAccessChecker(libRepo))
	opsH.SetAuthDeps(authSvc, sessionMgr)
	opsH.RegisterRoutes(mediaMux, ops.AuthFuncs{
		RequireAuth:   authMW.RequireAuth,
		RequireAdmin:  authMW.RequireAdmin,
		UserIDFromCtx: userIDFromCtx,
	})

	var compatExtSvc *compatext.Handler
	purgeUserArtifacts := func(c context.Context, userID string) {
		notifH.PurgeUser(c, userID)
		opsH.PurgeUser(c, userID)
		syncH.PurgeUser(c, userID)
		if compatExtSvc != nil {
			compatExtSvc.PurgeUser(c, userID)
		}
	}
	usersHandler.SetOnUserDeleted(purgeUserArtifacts)

	// Phase 6: DLNA UPnP MediaServer, Rankings, Migration & Governance Admin APIs
	dlnaH := dlna.New(cfg, libRepo, store)
	dlnaH.SetUserRepo(userRepo)
	dlnaH.SetDefaultUserID(snap.DLNADefaultUserID)
	dlnaH.SetTranscodeManager(transcodeMgr)
	dlnaH.SetStreamResolverWithHeaders(pb.ResolveStream)
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
	migrator.SetSelfServer(snap.HTTPAddr, snap.ServerID)
	migrator.SetOnUserUpdated(authSvc.InvalidateUserCache)
	migrator.SetOnUserRevoked(func(c context.Context, userID string) {
		_ = userRepo.DeleteTokensByUser(c, userID)
		authSvc.InvalidateUserCache(userID)
		sessionMgr.RemoveSessionsByUser(userID)
		wsHub.DisconnectUser(userID)
	})
	migrationH := migrationhandler.New(migrator)
	migrationH.RegisterRoutes(mediaMux, migrationhandler.AuthFuncs{
		RequireAuth:  authMW.RequireAuth,
		RequireAdmin: authMW.RequireAdmin,
	})
	govSvc, err := governance.NewService(governance.Options{
		Store:       store,
		LibraryRepo: libRepo,
		Config:      cfg,
		Scheduler:   taskMgr,
		BudgetPool:  bgBudget,
	})
	if err != nil {
		notifH.Close()
		opsH.Close()
		livetvH.Close()
		if stopTrakt != nil {
			stopTrakt()
		}
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
	compatMediaSvc.SetStreamResolver(resolveStreamURL)
	compatMediaSvc.SetStreamResolverWithHeaders(pb.ResolveStream)
	compatMediaH := compatMediaSvc.BuildRouter(compatmedia.AuthFuncs{
		RequireAuth:   authMW.RequireAuth,
		RequireAdmin:  authMW.RequireAdmin,
		UserIDFromCtx: userIDFromCtx,
	})
	compatCoreSvc := compatcore.New(cfg, store, userRepo, libRepo, sessionMgr, registry)
	compatCoreSvc.SetAuthService(authSvc)
	compatCoreSvc.SetWSHub(wsHub)
	compatCoreSvc.SetOnUserDeleted(purgeUserArtifacts)
	compatCoreSvc.SetRestoreHandler(opsH.HandleRestoreBackup)
	compatCoreSvc.SetPartyHandlers(opsH.HandleLeaveCurrentParty, opsH.HandlePostPartyMessage)
	compatCoreSvc.SetPackageCanceller(opsH.CancelPackageInstall)
	compatCoreSvc.SetPluginUninstaller(pluginAdminH.UninstallPlugin)
	compatCoreSvc.SetLifecycleHooks(
		func() { _ = system.TriggerProcessRestart() },
		func() { _ = system.TriggerProcessShutdown() },
	)
	compatCoreSvc.SetTaskStopper(func(id string) bool {
		return taskMgr.Stop(id) == nil
	})
	compatCoreH := compatCoreSvc.BuildRouter(compatcore.AuthFuncs{
		RequireAuth:   authMW.RequireAuth,
		RequireAdmin:  authMW.RequireAdmin,
		UserIDFromCtx: userIDFromCtx,
	})
	compatExtSvc = compatext.New(store, cfg)
	compatExtSvc.SetLiveTV(livetvH)
	compatExtSvc.SetSyncHandler(syncH)
	compatExtSvc.SetTranscodeManager(transcodeMgr)
	compatExtSvc.SetDVR(dvrRec)
	compatExtSvc.SetLibraryRepo(libRepo)
	compatExtSvc.SetUserRepo(userRepo)
	compatExtSvc.SetNotificationTester(func(c context.Context, title, description string) error {
		callerID := userIDFromCtx(c)
		payload := plugins.NotificationPayload{
			Event:   "NotificationTest",
			Title:   title,
			Message: description,
			Level:   "Normal",
			UserID:  callerID,
		}
		for _, n := range registry.Notifiers() {
			if err := n.Notify(c, payload); err != nil {
				return err
			}
		}
		return notifH.SendTestNotification(c, callerID, title, description)
	})
	compatExtH := compatExtSvc.BuildRouter(compatext.AuthFuncs{
		RequireAuth:   authMW.RequireAuth,
		RequireAdmin:  authMW.RequireAdmin,
		UserIDFromCtx: userIDFromCtx,
	})

	fallbackHandler := dispatchMuxAndCompat(mediaMux, compatMediaH, compatCoreH, compatExtH)

	// Web client static file server (embedded dist or external WebDir override)
	webHandler := buildWebHandler(cfg.WebDir)

	r := chi.NewRouter()
	r.Use(middleware.CORSWithDynamicOrigins(func() []string { return cfg.Snapshot().AllowedOrigins }))
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
	syncH.StartWorker(dvrCtx)
	go func() {
		defer close(dvrDone)
		dvrRec.SchedulerLoop(dvrCtx, 15*time.Second, livetvH.DueRecordingJobs)
	}()

	srv := &Server{
		Config:           cfg,
		Store:            store,
		Bus:              bus,
		Registry:         registry,
		PluginLoader:     extLoader,
		WasmHost:         wHost,
		Auth:             authSvc,
		Sessions:         sessionMgr,
		Scanner:          sc,
		Watcher:          libWatcher,
		Budget:           bgBudget,
		Scheduler:        taskMgr,
		Rankings:         rankingsSvc,
		Migrator:         migrator,
		Governance:       govSvc,
		Playback:         pb,
		Transcode:        transcodeMgr,
		DVR:              dvrRec,
		LiveTV:           livetvH,
		Imaging:          imgSvc,
		Logs:             logsH,
		WSHub:            wsHub,
		Handler:          r,
		ssdpCtx:          dvrCtx,
		dvrCancel:        dvrCancel,
		dvrDone:          dvrDone,
		stopTrakt:        stopTrakt,
		opsHandler:       opsH,
		notifHandler:     notifH,
		syncHandler:      syncH,
		migrationHandler: migrationH,
	}
	srv.applyDLNAState(snap.EnableDLNA)
	systemHandler.SetOnDLNAConfigChanged(func(enabled bool, defaultUserID string) {
		dlnaH.SetDefaultUserID(defaultUserID)
		srv.applyDLNAState(enabled)
	})
	return srv, nil
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
	cleanupInvalidRestore := func() {
		_ = os.Remove(restorePath)
		_ = os.Remove(filepath.Join(dataDir, "config", "ambra.json.restore"))
		_ = os.RemoveAll(filepath.Join(dataDir, "metadata.restore"))
	}
	if !info.Mode().IsRegular() || info.Size() == 0 {
		slog.Warn("removing invalid, symlink, or empty pending database restore file", "path", restorePath, "size", info.Size())
		cleanupInvalidRestore()
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
		slog.Warn("removing pending database restore with invalid SQLite magic header", "path", restorePath, "error", readErr)
		cleanupInvalidRestore()
		return nil
	}

	backupPath := dbPath + ".pre-restore.bak"
	hadExistingDB := false
	movedSidecars := make([]string, 0, 2)
	if _, err := os.Stat(dbPath); err == nil {
		hadExistingDB = true
		_ = os.Remove(backupPath)
		if err := os.Rename(dbPath, backupPath); err != nil {
			slog.Warn("failed to back up existing database before restore", "dbPath", dbPath, "backupPath", backupPath, "error", err)
			return fmt.Errorf("backup existing db: %w", err)
		}
	} else if !os.IsNotExist(err) {
		slog.Warn("failed to stat existing database before restore", "dbPath", dbPath, "error", err)
		return fmt.Errorf("stat existing db: %w", err)
	}

	// Always back up / clear any lingering WAL/SHM sidecars regardless of whether dbPath existed,
	// so stale WAL frames can never corrupt the restored database.
	for _, suffix := range []string{"-wal", "-shm"} {
		sidecar := dbPath + suffix
		sidecarBak := backupPath + suffix
		_ = os.Remove(sidecarBak)
		if _, sErr := os.Stat(sidecar); sErr == nil {
			if rErr := os.Rename(sidecar, sidecarBak); rErr != nil {
				slog.Warn("failed to back up sqlite sidecar before restore; removing", "path", sidecar, "error", rErr)
				_ = os.Remove(sidecar)
			} else {
				movedSidecars = append(movedSidecars, suffix)
			}
		}
	}

	if err := os.Rename(restorePath, dbPath); err != nil {
		slog.Warn("failed to promote pending database restore", "restorePath", restorePath, "dbPath", dbPath, "error", err)
		if hadExistingDB {
			_ = os.Rename(backupPath, dbPath)
		}
		for _, suffix := range movedSidecars {
			_ = os.Rename(backupPath+suffix, dbPath+suffix)
		}
		return fmt.Errorf("promote restore db: %w", err)
	}

	slog.Info("applied pending database restore", "restorePath", restorePath, "dbPath", dbPath, "backupCreated", hadExistingDB, "backupPath", backupPath)
	return nil
}

func parseHTTPPort(addr string) int {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return 8096
	}
	_, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		return 8096
	}
	port, err := strconv.Atoi(portStr)
	if err != nil || port < 1 || port > 65535 {
		return 8096
	}
	return port
}

// SSDPBroadcaster returns the currently active SSDP broadcaster under lock, or nil when DLNA is disabled.
func (s *Server) SSDPBroadcaster() *ssdp.Broadcaster {
	s.ssdpMu.Lock()
	defer s.ssdpMu.Unlock()
	return s.SSDP
}

func (s *Server) applyDLNAState(enabled bool) {
	s.ssdpMu.Lock()
	defer s.ssdpMu.Unlock()
	if s.ssdpClosed {
		return
	}
	if !enabled {
		if s.ssdpCancel != nil {
			s.ssdpCancel()
			s.ssdpCancel = nil
		}
		if s.ssdpDone != nil {
			<-s.ssdpDone
			s.ssdpDone = nil
		}
		s.SSDP = nil
		return
	}
	if s.ssdpCancel != nil && s.SSDP != nil {
		return
	}
	parentCtx := s.ssdpCtx
	if parentCtx == nil {
		parentCtx = context.Background()
	}
	if parentCtx.Err() != nil {
		return
	}
	var snap config.Config
	if s.Config != nil {
		snap = s.Config.Snapshot()
	}
	b := ssdp.NewBroadcaster(ssdp.Config{
		ServerID:      snap.ServerID,
		ServerName:    snap.ServerName,
		ServerVersion: config.ServerVersion,
		Port:          parseHTTPPort(snap.HTTPAddr),
	})
	instCtx, instCancel := context.WithCancel(parentCtx)
	doneCh := make(chan struct{})
	s.SSDP = b
	s.ssdpCancel = instCancel
	s.ssdpDone = doneCh
	go func() {
		defer close(doneCh)
		_ = b.Start(instCtx)
	}()
}

func (s *Server) stopSSDP() {
	s.ssdpMu.Lock()
	defer s.ssdpMu.Unlock()
	s.ssdpClosed = true
	if s.ssdpCancel != nil {
		s.ssdpCancel()
		s.ssdpCancel = nil
	}
	if s.ssdpDone != nil {
		<-s.ssdpDone
		s.ssdpDone = nil
	}
	s.SSDP = nil
}

func (s *Server) Close() error {
	s.stopSSDP()
	if s.WSHub != nil {
		s.WSHub.Close()
	}
	if s.opsHandler != nil {
		s.opsHandler.Close()
	}
	if s.notifHandler != nil {
		s.notifHandler.Close()
	}
	if s.syncHandler != nil {
		s.syncHandler.StopWorker()
	}
	if s.LiveTV != nil {
		s.LiveTV.Close()
	}
	if s.migrationHandler != nil {
		s.migrationHandler.Close()
	}
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
	if s.stopTrakt != nil {
		s.stopTrakt()
	}
	if s.Scheduler != nil {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = s.Scheduler.Shutdown(shutdownCtx)
		cancel()
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

const webContentSecurityPolicy = "default-src 'self'; script-src 'self' 'wasm-unsafe-eval' blob:; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob: https:; media-src 'self' blob: https: http:; connect-src 'self' ws: wss: https: blob: data:; worker-src 'self' blob:; font-src 'self' data: blob:; object-src 'none'; frame-ancestors 'self'; base-uri 'self'"

func setWebSecurityHeaders(w http.ResponseWriter) {
	h := w.Header()
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("X-Frame-Options", "SAMEORIGIN")
	h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
	h.Set("Content-Security-Policy", webContentSecurityPolicy)
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
		if !session.UserCanAccessLibrary(ctx, libRepo, user, rec.LibraryID) {
			return false
		}
		blocked, err := libRepo.IsItemBlockedByPolicy(ctx, rec, user.Policy)
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
		if libID != "" && !session.UserCanAccessLibrary(ctx, libRepo, user, libID) {
			return nil, false
		}

		hasContentPolicy := user.Policy.MaxParentalRating != nil ||
			len(user.Policy.BlockedTags) > 0 ||
			len(user.Policy.BlockUnratedItems) > 0 ||
			len(user.Policy.AllowedTags) > 0

		// Filter any explicit item ID slices so restricted item IDs are never disclosed.
		// ItemsRemoved cannot be checked via itemChecker because the rows are already deleted;
		// preserve removed IDs only when the user has library access and no parental/tag restrictions,
		// otherwise clear ItemsRemoved while preserving the library-level refresh signal.
		hasIDLists := false
		filteredLists := make(map[string][]string)
		anyVisibleID := false
		keepRemovedSignal := false
		for _, key := range []string{"ItemsAdded", "ItemsUpdated"} {
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
		if rawRemoved, ok := m["ItemsRemoved"].([]string); ok {
			hasIDLists = true
			canKnowLibraryScope := libID != "" || user.Policy.EnableAllFolders
			if len(rawRemoved) > 0 && canKnowLibraryScope {
				if !hasContentPolicy {
					filteredLists["ItemsRemoved"] = append([]string(nil), rawRemoved...)
					anyVisibleID = true
				} else {
					filteredLists["ItemsRemoved"] = []string{}
					keepRemovedSignal = true
				}
			} else {
				filteredLists["ItemsRemoved"] = []string{}
			}
		}
		if hasIDLists {
			if !anyVisibleID && !keepRemovedSignal {
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
		if sub, err := fs.Sub(web.DistFS, "dist"); err == nil {
			if _, statErr := fs.Stat(sub, "index.html"); statErr == nil {
				fsys = sub
			}
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
