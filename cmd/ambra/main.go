package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
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
	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/core/migration"
	"github.com/ambra-media/ambra/internal/core/rankings"
	"github.com/ambra-media/ambra/internal/pkg/netutil"
	"github.com/ambra-media/ambra/internal/server"
)

const sqliteRestoreMagicHeader = "SQLite format 3\x00"

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

type pendingRestoreState struct {
	dataDir string

	hasPendingDB   bool
	dbPath         string
	dbBakPath      string
	hadExistingDB  bool
	hadExistingWAL bool
	hadExistingSHM bool

	promotedCfg    bool
	cfgPath        string
	cfgBakPath     string
	hadExistingCfg bool

	promotedUsersMeta    bool
	usersMetaPath        string
	usersMetaBakPath     string
	hadExistingUsersMeta bool
}

func (s *pendingRestoreState) commit() {
	if s == nil {
		return
	}
	if s.usersMetaBakPath != "" {
		_ = os.RemoveAll(s.usersMetaBakPath)
	}
}

func (s *pendingRestoreState) rollback() {
	if s == nil {
		return
	}
	if s.promotedCfg {
		_ = os.Remove(s.cfgPath)
		if s.hadExistingCfg {
			_ = os.Rename(s.cfgBakPath, s.cfgPath)
		} else {
			_ = os.Remove(s.cfgBakPath)
		}
	}
	if s.promotedUsersMeta {
		_ = os.RemoveAll(s.usersMetaPath)
		if s.hadExistingUsersMeta {
			_ = os.Rename(s.usersMetaBakPath, s.usersMetaPath)
		} else {
			_ = os.RemoveAll(s.usersMetaBakPath)
		}
	}
	if s.hasPendingDB {
		restoreDBPath := filepath.Join(s.dataDir, "ambra.db.restore")
		if _, err := os.Lstat(restoreDBPath); err == nil {
			// server.New did not promote ambra.db.restore yet; discard the staged restore file.
			_ = os.Remove(restoreDBPath)
		} else if os.IsNotExist(err) {
			// ambra.db.restore was promoted over ambra.db before server.New failed; restore backup.
			_ = os.Remove(s.dbPath)
			_ = os.Remove(s.dbPath + "-wal")
			_ = os.Remove(s.dbPath + "-shm")
			if s.hadExistingDB {
				_ = os.Rename(s.dbBakPath, s.dbPath)
			}
			if s.hadExistingWAL {
				_ = os.Rename(s.dbBakPath+"-wal", s.dbPath+"-wal")
			} else {
				_ = os.Remove(s.dbBakPath + "-wal")
			}
			if s.hadExistingSHM {
				_ = os.Rename(s.dbBakPath+"-shm", s.dbPath+"-shm")
			} else {
				_ = os.Remove(s.dbBakPath + "-shm")
			}
		}
	}
	slog.Warn("rolled back pending backup restore after server initialization failure", "dataDir", s.dataDir)
}

func validatePendingDBRestoreFile(restoreDBPath string) (present bool, valid bool) {
	info, err := os.Lstat(restoreDBPath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, true
		}
		return true, false
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 {
		return true, false
	}
	f, err := os.Open(restoreDBPath)
	if err != nil {
		return true, false
	}
	defer f.Close()
	var hdr [16]byte
	if _, err := io.ReadFull(f, hdr[:]); err != nil {
		return true, false
	}
	return true, string(hdr[:]) == sqliteRestoreMagicHeader
}

func isExecutablePathConfigKey(k string) bool {
	switch strings.ToLower(strings.TrimSpace(k)) {
	case "ffmpeg_path", "ffprobe_path", "ffmpegpath", "ffprobepath",
		"web_dir", "webdir", "data_dir", "datadir":
		return true
	default:
		return false
	}
}

func sanitizeRestoredConfigJSON(raw []byte, existingCfgPath string) ([]byte, bool) {
	if !json.Valid(raw) {
		return nil, false
	}
	var cfgMap map[string]any
	if err := json.Unmarshal(raw, &cfgMap); err != nil || cfgMap == nil {
		return nil, false
	}
	// Security (M-03 / H-02): Strip any host executable or directory paths from restored backup config.
	for k := range cfgMap {
		if isExecutablePathConfigKey(k) {
			delete(cfgMap, k)
		}
	}
	if info, err := os.Lstat(existingCfgPath); err == nil && info.Mode().IsRegular() {
		if existingRaw, rErr := os.ReadFile(existingCfgPath); rErr == nil {
			var existingMap map[string]any
			if json.Unmarshal(existingRaw, &existingMap) == nil && existingMap != nil {
				var keepFFmpeg, keepFFprobe, keepWebDir, keepDataDir string
				for k, v := range existingMap {
					s, ok := v.(string)
					if !ok || strings.TrimSpace(s) == "" {
						continue
					}
					switch strings.ToLower(strings.TrimSpace(k)) {
					case "ffmpeg_path", "ffmpegpath":
						keepFFmpeg = s
					case "ffprobe_path", "ffprobepath":
						keepFFprobe = s
					case "web_dir", "webdir":
						keepWebDir = s
					case "data_dir", "datadir":
						keepDataDir = s
					}
				}
				if keepFFmpeg != "" {
					cfgMap["ffmpeg_path"] = keepFFmpeg
				}
				if keepFFprobe != "" {
					cfgMap["ffprobe_path"] = keepFFprobe
				}
				if keepWebDir != "" {
					cfgMap["web_dir"] = keepWebDir
				}
				if keepDataDir != "" {
					cfgMap["data_dir"] = keepDataDir
				}
			}
		}
	}
	out, err := json.MarshalIndent(cfgMap, "", "  ")
	if err != nil {
		return nil, false
	}
	return out, true
}

func writeConfigFileAtomic(targetPath string, content []byte) error {
	dir := filepath.Dir(targetPath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".ambra-restore-cfg-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(content); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, targetPath); err != nil {
		return err
	}
	cleanup = false
	return nil
}

func stagePendingRestore(dataDir string) *pendingRestoreState {
	if strings.TrimSpace(dataDir) == "" {
		return nil
	}
	cfgDir := filepath.Join(dataDir, "config")
	restoreCfg := filepath.Join(cfgDir, "ambra.json.restore")
	restoreMeta := filepath.Join(dataDir, "metadata.restore")
	restoreDBPath := filepath.Join(dataDir, "ambra.db.restore")
	hasPendingDB, dbValid := validatePendingDBRestoreFile(restoreDBPath)
	if hasPendingDB && !dbValid {
		slog.Warn("aborting pending configuration and metadata restore: invalid ambra.db.restore", "path", restoreDBPath)
		_ = os.Remove(restoreDBPath)
		_ = os.Remove(restoreCfg)
		_ = os.RemoveAll(restoreMeta)
		return nil
	}

	st := &pendingRestoreState{dataDir: dataDir}
	if hasPendingDB {
		st.hasPendingDB = true
		st.dbPath = filepath.Join(dataDir, "ambra.db")
		st.dbBakPath = st.dbPath + ".pre-restore.bak"
		if _, err := os.Lstat(st.dbPath); err == nil {
			st.hadExistingDB = true
		}
		if _, err := os.Lstat(st.dbPath + "-wal"); err == nil {
			st.hadExistingWAL = true
		}
		if _, err := os.Lstat(st.dbPath + "-shm"); err == nil {
			st.hadExistingSHM = true
		}
	}

	if info, err := os.Lstat(restoreCfg); err == nil {
		if !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > (4<<20) {
			slog.Warn("removing invalid or oversized pending configuration restore file", "path", restoreCfg, "mode", info.Mode().String(), "size", info.Size())
			_ = os.Remove(restoreCfg)
		} else {
			target := filepath.Join(cfgDir, "ambra.json")
			if raw, readErr := os.ReadFile(restoreCfg); readErr == nil {
				if sanitized, ok := sanitizeRestoredConfigJSON(raw, target); ok {
					_ = os.MkdirAll(cfgDir, 0o700)
					bakPath := target + ".pre-restore.bak"
					_ = os.Remove(bakPath)
					hadExistingCfg := false
					if _, stErr := os.Lstat(target); stErr == nil {
						if renErr := os.Rename(target, bakPath); renErr == nil {
							hadExistingCfg = true
						}
					}
					if wErr := writeConfigFileAtomic(target, sanitized); wErr == nil {
						_ = os.Remove(restoreCfg)
						st.promotedCfg = true
						st.cfgPath = target
						st.cfgBakPath = bakPath
						st.hadExistingCfg = hadExistingCfg
						slog.Info("applied pending configuration restore", "path", target)
					} else if hadExistingCfg {
						_ = os.Rename(bakPath, target)
					}
				} else {
					_ = os.Remove(restoreCfg)
				}
			} else {
				_ = os.Remove(restoreCfg)
			}
		}
	}

	if info, err := os.Lstat(restoreMeta); err == nil && info.IsDir() && info.Mode()&os.ModeSymlink == 0 {
		targetUsersMeta := filepath.Join(dataDir, "metadata", "users")
		stagedUsersMeta := filepath.Join(restoreMeta, "users")
		if sInfo, sErr := os.Lstat(stagedUsersMeta); sErr == nil && sInfo.IsDir() && sInfo.Mode()&os.ModeSymlink == 0 {
			_ = os.MkdirAll(filepath.Join(dataDir, "metadata"), 0o755)
			bakDir := targetUsersMeta + ".pre-restore.bak"
			_ = os.RemoveAll(bakDir)
			hadExistingUsers := false
			if _, tErr := os.Lstat(targetUsersMeta); tErr == nil {
				if renErr := os.Rename(targetUsersMeta, bakDir); renErr == nil {
					hadExistingUsers = true
				}
			}
			if renErr := os.Rename(stagedUsersMeta, targetUsersMeta); renErr == nil {
				st.promotedUsersMeta = true
				st.usersMetaPath = targetUsersMeta
				st.usersMetaBakPath = bakDir
				st.hadExistingUsersMeta = hadExistingUsers
				slog.Info("applied pending user metadata restore", "path", targetUsersMeta)
			} else if hadExistingUsers {
				_ = os.Rename(bakDir, targetUsersMeta)
			}
		}
		_ = os.RemoveAll(restoreMeta)
	}

	if !st.hasPendingDB && !st.promotedCfg && !st.promotedUsersMeta {
		return nil
	}
	return st
}

func runServerInstance(
	sigCtx context.Context,
	dataDir, cliHTTPAddr, cliWebDir string,
	restartCh, shutdownCh <-chan struct{},
) (bool, int) {
	restoreState := stagePendingRestore(dataDir)

	cfg, err := config.Load(dataDir)
	if err != nil {
		if restoreState != nil {
			restoreState.rollback()
		}
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

	fallbackLogger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	logBridge := logs.NewMultiSlogHandler(os.Stdout, nil, logs.DailyLogFilePath(snap.DataDir, time.Now().UTC()))
	defer func() {
		slog.SetDefault(fallbackLogger)
		_ = logBridge.Close()
	}()
	slog.SetDefault(slog.New(logBridge))

	bgCtx, bgCancel := context.WithCancel(context.Background())
	defer bgCancel()

	srv, err := server.New(bgCtx, cfg)
	if err != nil {
		if restoreState != nil {
			restoreState.rollback()
		}
		slog.Error("failed to initialize Ambra server", "error", err)
		return false, 1
	}
	if restoreState != nil {
		restoreState.commit()
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
		ReadTimeout:       60 * time.Second,
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
	case <-shutdownCh:
		slog.Info("shutting down Ambra Media Server via API...")
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
