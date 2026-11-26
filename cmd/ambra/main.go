package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/ambra-media/ambra/internal/api/handlers/logs"
	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/core/rankings"
	"github.com/ambra-media/ambra/internal/server"
)

func main() {
	os.Exit(run())
}

func run() int {
	dataDir := flag.String("data", "./data", "Ambra data directory")
	httpAddr := flag.String("addr", "", "HTTP listen address (overrides config, e.g. :8096)")
	webDir := flag.String("web", "", "External web UI directory (overrides embedded web UI)")
	showVersion := flag.Bool("version", false, "Print Ambra version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Printf("Ambra %s (Emby compat %s)\n", config.AmbraVersion, config.ServerVersion)
		return 0
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	cfg, err := config.Load(*dataDir)
	if err != nil {
		slog.Error("failed to load config", "error", err)
		return 1
	}
	if *httpAddr != "" {
		cfg.HTTPAddr = *httpAddr
		cfg.MarkHTTPAddrRuntimeOverride()
	}
	if *webDir != "" {
		cfg.WebDir = *webDir
		cfg.MarkWebDirRuntimeOverride()
	}

	logBridge := logs.NewMultiSlogHandler(os.Stdout, nil, logs.DailyLogFilePath(cfg.DataDir, time.Now().UTC()))
	defer func() { _ = logBridge.Close() }()
	slog.SetDefault(slog.New(logBridge))

	sigCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	bgCtx, bgCancel := context.WithCancel(context.Background())
	defer bgCancel()

	srv, err := server.New(bgCtx, cfg)
	if err != nil {
		slog.Error("failed to initialize Ambra server", "error", err)
		return 1
	}
	defer func() {
		bgCancel()
		_ = srv.Close()
	}()
	logBridge.SetLogsHandler(srv.Logs)

	if srv.SSDP != nil {
		go func() { _ = srv.SSDP.Start(bgCtx) }()
	}
	if srv.Rankings != nil {
		srv.Rankings.StartPeriodicSync(bgCtx, rankings.DefaultSyncInterval)
	}
	if srv.Watcher != nil {
		go func() { _ = srv.Watcher.Start(bgCtx) }()
	}

	httpSrv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           srv.Handler,
		ReadHeaderTimeout: 15 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}

	webURL := "http://localhost" + cfg.HTTPAddr + "/web/"
	if !strings.HasPrefix(cfg.HTTPAddr, ":") {
		webURL = "http://" + cfg.HTTPAddr + "/web/"
	}

	serveErrCh := make(chan error, 1)
	go func() {
		slog.Info("Ambra Media Server started",
			"ambra_version", config.AmbraVersion,
			"emby_compat_version", config.ServerVersion,
			"addr", cfg.HTTPAddr,
			"web_ui", webURL,
			"server_id", cfg.ServerID,
		)
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("http server error", "error", err)
			serveErrCh <- err
			stop()
		}
	}()

	<-sigCtx.Done()
	var serveErr error
	select {
	case serveErr = <-serveErrCh:
	default:
	}

	slog.Info("shutting down Ambra Media Server...")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = httpSrv.Shutdown(shutdownCtx)
	if serveErr != nil {
		return 1
	}
	return 0
}
