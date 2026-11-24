package server

import (
	"context"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/ambra-media/ambra/internal/api/handlers/audio"
	"github.com/ambra-media/ambra/internal/api/handlers/catalog"
	"github.com/ambra-media/ambra/internal/api/handlers/collections"
	"github.com/ambra-media/ambra/internal/api/handlers/compatcore"
	"github.com/ambra-media/ambra/internal/api/handlers/compatext"
	"github.com/ambra-media/ambra/internal/api/handlers/compatmedia"
	"github.com/ambra-media/ambra/internal/api/handlers/dlna"
	"github.com/ambra-media/ambra/internal/api/handlers/environment"
	"github.com/ambra-media/ambra/internal/api/handlers/hls"
	"github.com/ambra-media/ambra/internal/api/handlers/library"
	"github.com/ambra-media/ambra/internal/api/handlers/livetv"
	"github.com/ambra-media/ambra/internal/api/handlers/logs"
	"github.com/ambra-media/ambra/internal/api/handlers/notifications"
	"github.com/ambra-media/ambra/internal/api/handlers/ops"
	"github.com/ambra-media/ambra/internal/api/handlers/pluginadmin"
	"github.com/ambra-media/ambra/internal/api/handlers/streaming"
	syncapi "github.com/ambra-media/ambra/internal/api/handlers/sync"
	"github.com/ambra-media/ambra/internal/api/handlers/system"
	"github.com/ambra-media/ambra/internal/api/handlers/tasks"
	"github.com/ambra-media/ambra/internal/api/handlers/users"
	"github.com/ambra-media/ambra/internal/api/middleware"
	"github.com/ambra-media/ambra/internal/api/ws"
	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/core/auth"
	"github.com/ambra-media/ambra/internal/core/dvr"
	"github.com/ambra-media/ambra/internal/core/imaging"
	"github.com/ambra-media/ambra/internal/core/playback"
	"github.com/ambra-media/ambra/internal/core/rankings"
	"github.com/ambra-media/ambra/internal/core/scanner"
	"github.com/ambra-media/ambra/internal/core/scheduler"
	"github.com/ambra-media/ambra/internal/core/session"
	"github.com/ambra-media/ambra/internal/core/ssdp"
	"github.com/ambra-media/ambra/internal/core/transcode"
	"github.com/ambra-media/ambra/internal/pkg/eventbus"
	"github.com/ambra-media/ambra/internal/plugins"
	"github.com/ambra-media/ambra/internal/plugins/ext"
	"github.com/ambra-media/ambra/internal/plugins/nfo"
	"github.com/ambra-media/ambra/internal/plugins/strm"
	"github.com/ambra-media/ambra/internal/plugins/tmdb"
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
	Scheduler    *scheduler.Manager
	Rankings     *rankings.Service
	Playback     *playback.Engine
	Transcode    *transcode.Manager
	DVR          *dvr.Recorder
	SSDP         *ssdp.Broadcaster
	Imaging      *imaging.Service
	WSHub        *ws.Hub
	Handler      http.Handler
}

func New(ctx context.Context, cfg *config.Config) (*Server, error) {
	store, err := sqlite.Open(cfg.DBPath())
	if err != nil {
		return nil, fmt.Errorf("open store: %w", err)
	}

	bus := eventbus.New()
	registry := plugins.NewRegistry()
	// Register built-in official plugins
	registry.Register(nfo.New())
	registry.Register(tmdb.New(cfg.TMDBApiKey, cfg.TMDBLanguage))
	registry.Register(strm.New(cfg.EnableSTRM302))

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

	authSvc := auth.NewService(userRepo, cfg)
	if err := authSvc.EnsureDefaultAdmin(ctx); err != nil {
		_ = store.Close()
		return nil, fmt.Errorf("ensure default admin: %w", err)
	}

	sessionMgr := session.NewManager(userRepo, cfg, bus)
	authMW := middleware.NewAuth(authSvc, sessionMgr)
	wsHub := ws.NewHub(authSvc, sessionMgr, bus)

	sc := scanner.New(libRepo, registry, bus, cfg)
	rankingsSvc := rankings.New(store, bus, rankings.Options{})
	_ = rankingsSvc.LoadCache(ctx)
	pb := playback.New(libRepo, registry, cfg)
	imgSvc := imaging.New(libRepo).WithCacheDir(cfg.ImageCacheDir())
	transcodeMgr := transcode.NewManager(cfg)
	dvrRec := dvr.NewRecorder(dvr.Options{
		DataDir:    cfg.DataDir,
		FFmpegPath: cfg.FFmpegPath,
		Bus:        bus,
	})
	ssdpB := ssdp.NewBroadcaster(ssdp.Config{
		ServerID:      cfg.ServerID,
		ServerName:    cfg.ServerName,
		ServerVersion: config.ServerVersion,
	})

	taskMgr := scheduler.New(bus)
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
		Name:        "同步豆瓣 Top 250 与 IMDb Top 250 榜单",
		Category:    "Library",
		Description: "定期拉取豆瓣 Top 250 与 IMDb Top 250 榜单缓存至后端，并为匹配影片自动追加榜单标签",
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

	pluginAdminH := pluginadmin.New(registry, store)
	_ = pluginAdminH.ApplySavedConfigs(ctx)

	systemHandler := system.NewHandler(cfg, userRepo, registry)
	usersHandler := users.NewHandler(authSvc, userRepo, sessionMgr, wsHub, cfg)

	userIDFromCtx := func(c context.Context) string {
		if ac, ok := middleware.AuthFromContext(c); ok && ac != nil {
			return ac.UserID()
		}
		return ""
	}

	mediaMux := http.NewServeMux()
	// Phase 1: Library & Direct Streaming
	library.New(libRepo, sc, cfg).RegisterRoutes(mediaMux, library.AuthFuncs{
		RequireAuth:   authMW.RequireAuth,
		RequireAdmin:  authMW.RequireAdmin,
		UserIDFromCtx: userIDFromCtx,
	})
	streaming.New(libRepo, pb, imgSvc).RegisterRoutes(mediaMux, streaming.AuthFuncs{
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
	collections.New(libRepo, cfg).RegisterRoutes(mediaMux, collections.AuthFuncs{
		RequireAuth:   authMW.RequireAuth,
		RequireAdmin:  authMW.RequireAdmin,
		UserIDFromCtx: userIDFromCtx,
	})
	catalog.New(libRepo, cfg).RegisterRoutes(mediaMux, catalog.AuthFuncs{
		RequireAuth:   authMW.RequireAuth,
		RequireAdmin:  authMW.RequireAdmin,
		UserIDFromCtx: userIDFromCtx,
	})
	tasks.New(taskMgr).RegisterRoutes(mediaMux, tasks.AuthFuncs{
		RequireAuth:   authMW.RequireAuth,
		RequireAdmin:  authMW.RequireAdmin,
		UserIDFromCtx: userIDFromCtx,
	})
	logs.New(cfg).RegisterRoutes(mediaMux, logs.AuthFuncs{
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
	hls.New(libRepo, transcodeMgr, cfg).RegisterRoutes(mediaMux, hls.AuthFuncs{
		RequireAuth:   authMW.RequireAuth,
		RequireAdmin:  authMW.RequireAdmin,
		UserIDFromCtx: userIDFromCtx,
	})
	audio.New(libRepo, cfg).RegisterRoutes(mediaMux, audio.AuthFuncs{
		RequireAuth:   authMW.RequireAuth,
		RequireAdmin:  authMW.RequireAdmin,
		UserIDFromCtx: userIDFromCtx,
	})

	// Phase 4 & 5: LiveTV, Sync, Notifications, Startup, Packages, Webhooks, BackupRestore, OpenAPI, GenericUI, Parties, Connect
	livetv.New(store, cfg).RegisterRoutes(mediaMux, livetv.AuthFuncs{
		RequireAuth:   authMW.RequireAuth,
		RequireAdmin:  authMW.RequireAdmin,
		UserIDFromCtx: userIDFromCtx,
	})
	syncapi.New(store, cfg).RegisterRoutes(mediaMux, syncapi.AuthFuncs{
		RequireAuth:   authMW.RequireAuth,
		RequireAdmin:  authMW.RequireAdmin,
		UserIDFromCtx: userIDFromCtx,
	})
	notifications.New(store).RegisterRoutes(mediaMux, notifications.AuthFuncs{
		RequireAuth:   authMW.RequireAuth,
		RequireAdmin:  authMW.RequireAdmin,
		UserIDFromCtx: userIDFromCtx,
	})
	ops.New(store, cfg, userRepo, bus).RegisterRoutes(mediaMux, ops.AuthFuncs{
		RequireAuth:   authMW.RequireAuth,
		RequireAdmin:  authMW.RequireAdmin,
		UserIDFromCtx: userIDFromCtx,
	})

	// Phase 6: DLNA UPnP MediaServer & Rankings Sync API
	dlna.New(cfg, libRepo, store).RegisterRoutes(mediaMux, dlna.AuthFuncs{
		RequireAuth:   authMW.RequireAuth,
		RequireAdmin:  authMW.RequireAdmin,
		UserIDFromCtx: userIDFromCtx,
	})
	rankings.NewHandler(rankingsSvc).RegisterRoutes(mediaMux, rankings.AuthFuncs{
		RequireAuth:  authMW.RequireAuth,
		RequireAdmin: authMW.RequireAdmin,
	})

	// Emby 4.10.2.0 full 582-route compatibility routers (zero ServeMux conflicts)
	compatMediaH := compatmedia.New(libRepo, sc, imgSvc, registry, store, cfg).BuildRouter(compatmedia.AuthFuncs{
		RequireAuth:   authMW.RequireAuth,
		RequireAdmin:  authMW.RequireAdmin,
		UserIDFromCtx: userIDFromCtx,
	})
	compatCoreH := compatcore.New(cfg, store, userRepo, libRepo, sessionMgr, registry).BuildRouter(compatcore.AuthFuncs{
		RequireAuth:   authMW.RequireAuth,
		RequireAdmin:  authMW.RequireAdmin,
		UserIDFromCtx: userIDFromCtx,
	})
	compatExtH := compatext.New(store, cfg).BuildRouter(compatext.AuthFuncs{
		RequireAuth:   authMW.RequireAuth,
		RequireAdmin:  authMW.RequireAdmin,
		UserIDFromCtx: userIDFromCtx,
	})

	fallbackHandler := dispatchMuxAndCompat(mediaMux, compatMediaH, compatCoreH, compatExtH)

	// Web client static file server (embedded dist or external WebDir override)
	webHandler := buildWebHandler(cfg.WebDir)

	r := chi.NewRouter()
	r.Use(middleware.CORS)
	r.Use(middleware.EmbyPrefixAndCase)
	r.Use(authMW.ParseAuth)

	wsHub.RegisterRoutes(r)
	system.RegisterRoutes(r, authMW, systemHandler)
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
		Scheduler:    taskMgr,
		Rankings:     rankingsSvc,
		Playback:     pb,
		Transcode:    transcodeMgr,
		DVR:          dvrRec,
		SSDP:         ssdpB,
		Imaging:      imgSvc,
		WSHub:        wsHub,
		Handler:      r,
	}, nil
}

func (s *Server) Close() error {
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
	fileServer := http.FileServer(http.FS(fsys))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(r.URL.Path, "/")
		if p == "" || p == "index.html" {
			fileServer.ServeHTTP(w, r)
			return
		}
		if _, err := fs.Stat(fsys, p); err == nil {
			fileServer.ServeHTTP(w, r)
			return
		}
		// SPA fallback to index.html
		r2 := r.Clone(r.Context())
		r2.URL.Path = "/"
		fileServer.ServeHTTP(w, r2)
	})
}
