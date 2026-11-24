package main

import (
	"context"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/server"
)

func main() {
	dataDir := flag.String("data", "./data", "Ambra data directory")
	httpAddr := flag.String("addr", "", "HTTP listen address (overrides config, e.g. :8096)")
	webDir := flag.String("web", "", "External web UI directory (overrides embedded web UI)")
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	cfg, err := config.Load(*dataDir)
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}
	if *httpAddr != "" {
		cfg.HTTPAddr = *httpAddr
	}
	if *webDir != "" {
		cfg.WebDir = *webDir
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	srv, err := server.New(ctx, cfg)
	if err != nil {
		slog.Error("failed to initialize Ambra server", "error", err)
		os.Exit(1)
	}
	defer func() { _ = srv.Close() }()

	if srv.SSDP != nil {
		go func() { _ = srv.SSDP.Start(ctx) }()
	}

	httpSrv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           srv.Handler,
		ReadHeaderTimeout: 15 * time.Second,
	}

	go func() {
		slog.Info("Ambra Media Server started",
			"addr", cfg.HTTPAddr,
			"web_ui", "http://localhost"+cfg.HTTPAddr+"/web/",
			"emby_compat_version", config.ServerVersion,
			"server_id", cfg.ServerID,
		)
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("http server error", "error", err)
			stop()
		}
	}()

	<-ctx.Done()
	slog.Info("shutting down Ambra Media Server...")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = httpSrv.Shutdown(shutdownCtx)
}
