package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/ambra-media/ambra/internal/api/handlers/logs"
	"github.com/ambra-media/ambra/internal/api/handlers/system"
	"github.com/ambra-media/ambra/internal/api/ws"
	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/core/migration"
	"github.com/ambra-media/ambra/internal/core/rankings"
	"github.com/ambra-media/ambra/internal/pkg/netutil"
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

	sigCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	for {
		restartCh := make(chan struct{}, 1)
		shutdownCh := make(chan struct{}, 1)
		unregister := system.RegisterProcessLifecycleController(
			func() {
				select {
				case restartCh <- struct{}{}:
				default:
				}
			},
			func() {
				select {
				case shutdownCh <- struct{}{}:
				default:
				}
			},
		)

		shouldRestart, exitCode := runServerInstance(sigCtx, *dataDir, *httpAddr, *webDir, restartCh, shutdownCh)
		unregister()
		if !shouldRestart {
			return exitCode
		}
		if sigCtx.Err() != nil {
			return 0
		}
		slog.Info("reloading Ambra Media Server configuration and database...")
	}
}

func applyPendingConfigAndMetadataRestore(dataDir string) {
	if strings.TrimSpace(dataDir) == "" {
		return
	}
	cfgDir := filepath.Join(dataDir, "config")
	restoreCfg := filepath.Join(cfgDir, "ambra.json.restore")
	if info, err := os.Lstat(restoreCfg); err == nil && info.Mode().IsRegular() && info.Size() > 0 && info.Size() <= (4<<20) {
		if raw, readErr := os.ReadFile(restoreCfg); readErr == nil && json.Valid(raw) {
			target := filepath.Join(cfgDir, "ambra.json")
			_ = os.MkdirAll(cfgDir, 0o700)
			if _, stErr := os.Stat(target); stErr == nil {
				_ = os.Rename(target, target+".pre-restore.bak")
			}
			if renErr := os.Rename(restoreCfg, target); renErr == nil {
				_ = os.Chmod(target, 0o600)
				slog.Info("applied pending configuration restore", "path", target)
			}
		} else {
			_ = os.Remove(restoreCfg)
		}
	}

	restoreMeta := filepath.Join(dataDir, "metadata.restore")
	if info, err := os.Lstat(restoreMeta); err == nil && info.IsDir() && info.Mode()&os.ModeSymlink == 0 {
		targetUsersMeta := filepath.Join(dataDir, "metadata", "users")
		stagedUsersMeta := filepath.Join(restoreMeta, "users")
		if sInfo, sErr := os.Lstat(stagedUsersMeta); sErr == nil && sInfo.IsDir() && sInfo.Mode()&os.ModeSymlink == 0 {
			_ = os.MkdirAll(filepath.Join(dataDir, "metadata"), 0o755)
			bakDir := targetUsersMeta + ".pre-restore.bak"
			_ = os.RemoveAll(bakDir)
			if _, tErr := os.Lstat(targetUsersMeta); tErr == nil {
				_ = os.Rename(targetUsersMeta, bakDir)
			}
			if renErr := os.Rename(stagedUsersMeta, targetUsersMeta); renErr == nil {
				_ = os.RemoveAll(bakDir)
				slog.Info("applied pending user metadata restore", "path", targetUsersMeta)
			}
		}
		_ = os.RemoveAll(restoreMeta)
	}
}

func runServerInstance(
	sigCtx context.Context,
	dataDir, cliHTTPAddr, cliWebDir string,
	restartCh, shutdownCh <-chan struct{},
) (bool, int) {
	applyPendingConfigAndMetadataRestore(dataDir)

	cfg, err := config.Load(dataDir)
	if err != nil {
		slog.Error("failed to load config", "error", err)
		return false, 1
	}
	if cliHTTPAddr != "" {
		cfg.HTTPAddr = cliHTTPAddr
		cfg.MarkHTTPAddrRuntimeOverride()
	}
	if cliWebDir != "" {
		cfg.WebDir = cliWebDir
		cfg.MarkWebDirRuntimeOverride()
	}

	snap := cfg.Snapshot()
	netutil.SetTrustedProxies(snap.TrustedProxies)
	netutil.SetLocalNetworkSubnets(snap.LocalNetworkSubnets)
	migration.SetSelfServerIdentity(snap.HTTPAddr, snap.ServerID)

	logBridge := logs.NewMultiSlogHandler(os.Stdout, nil, logs.DailyLogFilePath(snap.DataDir, time.Now().UTC()))
	defer func() { _ = logBridge.Close() }()
	slog.SetDefault(slog.New(logBridge))

	bgCtx, bgCancel := context.WithCancel(context.Background())
	defer bgCancel()

	srv, err := server.New(bgCtx, cfg)
	if err != nil {
		slog.Error("failed to initialize Ambra server", "error", err)
		return false, 1
	}
	defer func() {
		bgCancel()
		_ = srv.Close()
	}()
	logBridge.SetLogsHandler(srv.Logs)
	if srv.Migrator != nil {
		srv.Migrator.SetSelfServer(snap.HTTPAddr, snap.ServerID)
		if srv.Auth != nil {
			srv.Migrator.SetOnUserUpdated(srv.Auth.InvalidateUserCache)
		}
	}

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
		Addr:              snap.HTTPAddr,
		Handler:           srv.Handler,
		ReadHeaderTimeout: 15 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}

	webURL := "http://localhost" + snap.HTTPAddr + "/web/"
	if !strings.HasPrefix(snap.HTTPAddr, ":") {
		webURL = "http://" + snap.HTTPAddr + "/web/"
	}

	serveErrCh := make(chan error, 1)
	go func() {
		slog.Info("Ambra Media Server started",
			"ambra_version", config.AmbraVersion,
			"emby_compat_version", config.ServerVersion,
			"addr", snap.HTTPAddr,
			"web_ui", webURL,
			"server_id", snap.ServerID,
		)
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("http server error", "error", err)
			serveErrCh <- err
		}
	}()

	var shouldRestart bool
	var serveErr error

	select {
	case <-restartCh:
		shouldRestart = true
		slog.Info("restarting Ambra Media Server...")
		if srv.WSHub != nil {
			srv.WSHub.Broadcast(ws.Message{MessageType: "ServerRestarting"})
		}
	case <-shutdownCh:
		slog.Info("shutting down Ambra Media Server via API...")
		if srv.WSHub != nil {
			srv.WSHub.Broadcast(ws.Message{MessageType: "ServerShuttingDown"})
		}
	case <-sigCtx.Done():
		slog.Info("shutting down Ambra Media Server...")
	case serveErr = <-serveErrCh:
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = httpSrv.Shutdown(shutdownCtx)
	if serveErr != nil {
		return false, 1
	}
	return shouldRestart, 0
}
