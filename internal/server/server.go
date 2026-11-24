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
	"github.com/ambra-media/ambra/internal/core/imaging"
	"github.com/ambra-media/ambra/internal/core/playback"
	"github.com/ambra-media/ambra/internal/core/scanner"
	"github.com/ambra-media/ambra/internal/core/scheduler"
	"github.com/ambra-media/ambra/internal/core/session"
	"github.com/ambra-media/ambra/internal/core/transcode"
	"github.com/ambra-media/ambra/internal/pkg/eventbus"
	"github.com/ambra-media/ambra/internal/plugins"
	"github.com/ambra-media/ambra/internal/plugins/ext"
	"github.com/ambra-media/ambra/internal/plugins/nfo"
	"github.com/ambra-media/ambra/internal/plugins/strm"
	"github.com/ambra-media/ambra/internal/plugins/tmdb"
	"github.com/ambra-media/ambra/internal/store/sqlite"
	"github.com/ambra-media/ambra/web"
)

type Server struct {
	Config       *config.Config
	Store        *sqlite.Store
	Bus          *eventbus.Bus
	Registry     *plugins.Registry
	PluginLoader *ext.Loader
	Auth         *auth.Service
	Sessions     *session.Manager
	Scanner      *scanner.Scanner
	Scheduler    *scheduler.Manager
	Playback     *playback.Engine
	Transcode    *transcode.Manager
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

	// Discover external manifest-based HTTP plugins in data/plugins/*
	extLoader := ext.NewLoader()
	_, _ = extLoader.LoadFromDir(filepath.Join(cfg.DataDir, "plugins"), registry)

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
	pb := playback.New(libRepo, registry, cfg)
	imgSvc := imaging.New(libRepo)
	transcodeMgr := transcode.NewManager(cfg)

	taskMgr := scheduler.New(bus)
	taskMgr.RegisterBuiltinTasks(
		func(c context.Context) error {
			_, err := sc.ScanAll(c)
			return err
		},
		cfg.TranscodeTempDir(),
		store.Writer,
	)

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

	// Phase 6: DLNA UPnP MediaServer
	dlna.New(cfg, libRepo, store).RegisterRoutes(mediaMux, dlna.AuthFuncs{
		RequireAuth:   authMW.RequireAuth,
		RequireAdmin:  authMW.RequireAdmin,
		UserIDFromCtx: userIDFromCtx,
	})

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

	// Delegate all library, streaming, Phase 2-6 API routes to mediaMux
	r.NotFound(mediaMux.ServeHTTP)
	r.MethodNotAllowed(mediaMux.ServeHTTP)

	return &Server{
		Config:       cfg,
		Store:        store,
		Bus:          bus,
		Registry:     registry,
		PluginLoader: extLoader,
		Auth:         authSvc,
		Sessions:     sessionMgr,
		Scanner:      sc,
		Scheduler:    taskMgr,
		Playback:     pb,
		Transcode:    transcodeMgr,
		Imaging:      imgSvc,
		WSHub:        wsHub,
		Handler:      r,
	}, nil
}

func (s *Server) Close() error {
	if s.Transcode != nil {
		s.Transcode.Close()
	}
	if s.Store != nil {
		return s.Store.Close()
	}
	return nil
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
