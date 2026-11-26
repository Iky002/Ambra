package ops

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ambra-media/ambra/internal/api/httpx"
	"github.com/ambra-media/ambra/internal/pkg/eventbus"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/pkg/netutil"
)

const (
	webhookQueueSize           = 256
	webhookWorkerCount         = 4
	webhookDeliveryTimeout     = 10 * time.Second
	maxWebhooks                = 50
	maxBackupZipEntries        = 10000
	maxBackupRestoreTotalBytes = 2 << 30 // 2 GiB
	maxBackupConfigFileBytes   = 16 << 20
	maxBackupSingleMetaBytes   = 64 << 20
)

var (
	sqliteMagicHeader = []byte("SQLite format 3\x00")
	zipMagicHeader    = []byte("PK\x03\x04")
)

type webhookDispatchJob struct {
	cfg     WebhookConfig
	payload WebhookPayload
}

type WebhookConfig struct {
	Id          string   `json:"Id"`
	Name        string   `json:"Name"`
	Url         string   `json:"Url"`
	Events      []string `json:"Events"`
	Secret      string   `json:"Secret,omitempty"`
	ClearSecret bool     `json:"ClearSecret,omitempty"`
	HasSecret   bool     `json:"HasSecret,omitempty"`
	Enabled     bool     `json:"Enabled"`
}

type WebhookPayload struct {
	Event     string    `json:"Event"`
	ServerId  string    `json:"ServerId,omitempty"`
	Timestamp time.Time `json:"Timestamp"`
	Data      any       `json:"Data,omitempty"`
}

type WebhookTestResult struct {
	Success    bool   `json:"Success"`
	StatusCode int    `json:"StatusCode,omitempty"`
	DurationMs int64  `json:"DurationMs"`
	Error      string `json:"Error,omitempty"`
}

type BackupFileInfo struct {
	Name        string    `json:"Name"`
	Path        string    `json:"Path"`
	Size        int64     `json:"Size"`
	DateCreated time.Time `json:"DateCreated"`
}

type BackupInfoResponse struct {
	BackupDirectory string           `json:"BackupDirectory"`
	Backups         []BackupFileInfo `json:"Backups"`
}

// --- Webhooks SQLite Persistence & Auto-Migration ---

func (h *Handler) migrateAndLoadWebhooksLocked(ctx context.Context) {
	if h.store == nil || h.store.Reader == nil {
		return
	}
	dbItems, err := h.queryWebhooksFromDBLocked(ctx)
	if err != nil {
		return
	}
	if len(dbItems) == 0 && len(h.state.Webhooks) > 0 && h.store.Writer != nil {
		migrated := true
		for _, wh := range h.state.Webhooks {
			if wh.Id == "" {
				wh.Id = guid.New()
			}
			if wh.Events == nil {
				wh.Events = []string{"*"}
			}
			if err := h.upsertWebhookDBLocked(ctx, wh); err != nil {
				migrated = false
				break
			}
		}
		if migrated {
			if reloaded, rErr := h.queryWebhooksFromDBLocked(ctx); rErr == nil && len(reloaded) > 0 {
				dbItems = reloaded
			}
		}
	}
	if len(dbItems) > 0 {
		h.state.Webhooks = dbItems
	}
}

func (h *Handler) queryWebhooksFromDBLocked(ctx context.Context) ([]WebhookConfig, error) {
	if h.store == nil || h.store.Reader == nil {
		return nil, nil
	}
	rows, err := h.store.Reader.QueryContext(ctx, `
		SELECT id, name, url, secret, events_json, enabled
		FROM webhooks
		ORDER BY created_at ASC, id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []WebhookConfig
	for rows.Next() {
		var (
			wh         WebhookConfig
			eventsJSON string
			enabledInt int
		)
		if err := rows.Scan(&wh.Id, &wh.Name, &wh.Url, &wh.Secret, &eventsJSON, &enabledInt); err != nil {
			return nil, err
		}
		if eventsJSON != "" {
			_ = json.Unmarshal([]byte(eventsJSON), &wh.Events)
		}
		if wh.Events == nil {
			wh.Events = []string{"*"}
		}
		wh.Enabled = enabledInt != 0
		wh.HasSecret = wh.Secret != ""
		out = append(out, wh)
	}
	return out, rows.Err()
}

func (h *Handler) upsertWebhookDBLocked(ctx context.Context, wh WebhookConfig) error {
	if h.store == nil || h.store.Writer == nil {
		return nil
	}
	eventsRaw, err := json.Marshal(wh.Events)
	if err != nil {
		return err
	}
	enabledInt := 0
	if wh.Enabled {
		enabledInt = 1
	}
	nowUnix := time.Now().UTC().Unix()
	_, err = h.store.Writer.ExecContext(ctx, `
		INSERT INTO webhooks (id, name, url, secret, events_json, enabled, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			name = excluded.name,
			url = excluded.url,
			secret = excluded.secret,
			events_json = excluded.events_json,
			enabled = excluded.enabled,
			updated_at = excluded.updated_at`,
		wh.Id, wh.Name, wh.Url, wh.Secret, string(eventsRaw), enabledInt, nowUnix, nowUnix,
	)
	return err
}

func (h *Handler) deleteWebhookDBLocked(ctx context.Context, id string) error {
	if h.store == nil || h.store.Writer == nil {
		return nil
	}
	_, err := h.store.Writer.ExecContext(ctx, `DELETE FROM webhooks WHERE id = ?`, id)
	return err
}

// --- Webhooks ---

func (h *Handler) subscribeWebhooks(bus *eventbus.Bus) {
	allEvents := []eventbus.EventType{
		eventbus.EventLibraryChanged,
		eventbus.EventUserDataChanged,
		eventbus.EventPlaybackStart,
		eventbus.EventPlaybackProgress,
		eventbus.EventPlaybackStop,
		eventbus.EventSessionUpdated,
		eventbus.EventTaskProgress,
	}
	for _, evType := range allEvents {
		bus.Subscribe(evType, func(ctx context.Context, e eventbus.Event) {
			h.dispatchWebhooks(ctx, e)
		})
	}
}

func (h *Handler) ensureWebhookWorkersLocked() {
	if h.webhookCh != nil || h.webhookClosed {
		return
	}
	if h.webhookCtx == nil {
		h.webhookCtx, h.webhookCancel = context.WithCancel(context.Background())
	}
	ctx := h.webhookCtx
	ch := make(chan webhookDispatchJob, webhookQueueSize)
	h.webhookCh = ch
	h.webhookWG.Add(webhookWorkerCount)
	for i := 0; i < webhookWorkerCount; i++ {
		go func() {
			defer h.webhookWG.Done()
			for job := range ch {
				if ctx.Err() != nil {
					return
				}
				_, _ = h.sendWebhook(ctx, job.cfg, job.payload)
			}
		}()
	}
}

// Close gracefully shuts down webhook dispatcher workers. It is idempotent and safe to call multiple times.
func (h *Handler) Close() {
	h.webhookMu.Lock()
	if h.webhookClosed {
		h.webhookMu.Unlock()
		return
	}
	h.webhookClosed = true
	cancel := h.webhookCancel
	h.webhookCancel = nil
	ch := h.webhookCh
	h.webhookCh = nil
	h.webhookMu.Unlock()

	if cancel != nil {
		cancel()
	}
	if ch != nil {
		close(ch)
	}
	h.webhookWG.Wait()
}

func sanitizeWebhookConfig(wh WebhookConfig) WebhookConfig {
	if wh.Secret != "" {
		wh.HasSecret = true
		wh.Secret = ""
	}
	return wh
}

func (h *Handler) validateWebhookURL(rawURL string) error {
	if h.isCustomHTTPClient() {
		return netutil.ValidateRemoteURL(rawURL, true)
	}
	return netutil.ValidateRemoteURLNoLoopback(rawURL, true)
}

func isSensitivePathFieldKey(k string) bool {
	switch strings.ToLower(strings.TrimSpace(k)) {
	case "path", "output_path", "outputpath", "file_path", "filepath",
		"programdatapath", "cachepath", "logpath", "internalmetadatapath",
		"transcodingtemppath", "locations":
		return true
	default:
		return false
	}
}

func stripSensitivePathsValue(v any) any {
	switch typed := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for k, val := range typed {
			if isSensitivePathFieldKey(k) {
				continue
			}
			out[k] = stripSensitivePathsValue(val)
		}
		return out
	case map[string]string:
		out := make(map[string]string, len(typed))
		for k, val := range typed {
			if isSensitivePathFieldKey(k) {
				continue
			}
			out[k] = val
		}
		return out
	case []any:
		out := make([]any, len(typed))
		for i, elem := range typed {
			out[i] = stripSensitivePathsValue(elem)
		}
		return out
	default:
		return v
	}
}

func sanitizeWebhookPayloadData(data any) any {
	switch v := data.(type) {
	case nil:
		return nil
	case Party:
		return sanitizePartyForEvent(v)
	case *Party:
		if v == nil {
			return nil
		}
		cp := sanitizePartyForEvent(*v)
		return &cp
	case string, bool, int, int64, float64:
		return data
	default:
		raw, err := json.Marshal(data)
		if err != nil {
			return data
		}
		var generic any
		if err := json.Unmarshal(raw, &generic); err != nil {
			return data
		}
		return stripSensitivePathsValue(generic)
	}
}

func (h *Handler) dispatchWebhooks(ctx context.Context, e eventbus.Event) {
	h.mu.RLock()
	targets := make([]WebhookConfig, 0, len(h.state.Webhooks))
	for _, wh := range h.state.Webhooks {
		if !wh.Enabled || wh.Url == "" {
			continue
		}
		if matchesWebhookEvent(wh.Events, string(e.Type)) {
			targets = append(targets, wh)
		}
	}
	h.mu.RUnlock()

	if len(targets) == 0 {
		return
	}

	payload := WebhookPayload{
		Event:     string(e.Type),
		ServerId:  h.serverID(),
		Timestamp: time.Now().UTC(),
		Data:      sanitizeWebhookPayloadData(e.Payload),
	}

	h.webhookMu.Lock()
	if h.webhookClosed {
		h.webhookMu.Unlock()
		return
	}
	h.ensureWebhookWorkersLocked()
	ch := h.webhookCh
	for _, wh := range targets {
		select {
		case ch <- webhookDispatchJob{cfg: wh, payload: payload}:
		default:
			slog.Warn("ops: webhook dispatch queue full, dropping event", "webhookId", wh.Id, "event", payload.Event)
		}
	}
	h.webhookMu.Unlock()
}

func matchesWebhookEvent(events []string, eventType string) bool {
	if len(events) == 0 {
		return true
	}
	for _, ev := range events {
		if ev == "*" || strings.EqualFold(ev, eventType) {
			return true
		}
	}
	return false
}

func (h *Handler) sendWebhook(ctx context.Context, wh WebhookConfig, payload WebhookPayload) (int, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil {
		return 0, ctx.Err()
	}
	reqCtx, cancel := context.WithTimeout(ctx, webhookDeliveryTimeout)
	defer cancel()

	body, err := json.Marshal(payload)
	if err != nil {
		return 0, err
	}
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, wh.Url, bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Ambra-Event", payload.Event)
	if wh.Secret != "" {
		mac := hmac.New(sha256.New, []byte(wh.Secret))
		mac.Write(body)
		req.Header.Set("X-Ambra-Signature", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	}
	resp, err := h.getHTTPClient().Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	return resp.StatusCode, nil
}

func (h *Handler) handleListWebhooks(w http.ResponseWriter, r *http.Request) {
	h.mu.RLock()
	items := make([]WebhookConfig, len(h.state.Webhooks))
	for i, wh := range h.state.Webhooks {
		items[i] = sanitizeWebhookConfig(wh)
	}
	h.mu.RUnlock()
	httpx.JSON(w, http.StatusOK, items)
}

func webhookURLSameOriginAndPath(oldRaw, newRaw string) bool {
	uOld, err1 := url.Parse(strings.TrimSpace(oldRaw))
	uNew, err2 := url.Parse(strings.TrimSpace(newRaw))
	if err1 != nil || err2 != nil {
		return false
	}
	oldScheme := strings.ToLower(strings.TrimSpace(uOld.Scheme))
	newScheme := strings.ToLower(strings.TrimSpace(uNew.Scheme))
	if oldScheme == "" || newScheme == "" || oldScheme != newScheme {
		return false
	}
	oldHost := strings.ToLower(strings.TrimSpace(uOld.Host))
	newHost := strings.ToLower(strings.TrimSpace(uNew.Host))
	if oldHost == "" || newHost == "" || oldHost != newHost {
		return false
	}
	return uOld.Path == uNew.Path
}

func (h *Handler) handleCreateWebhook(w http.ResponseWriter, r *http.Request) {
	var wh WebhookConfig
	if err := httpx.DecodeJSONLimit(r, &wh, 64<<10); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid webhook payload")
		return
	}
	if strings.TrimSpace(wh.Url) == "" {
		httpx.Error(w, http.StatusBadRequest, "webhook Url is required")
		return
	}
	if err := h.validateWebhookURL(wh.Url); err != nil {
		httpx.Error(w, http.StatusBadRequest, fmt.Sprintf("unsafe webhook url: %v", err))
		return
	}
	if wh.Id == "" {
		wh.Id = guid.New()
	}
	if wh.Name == "" {
		wh.Name = wh.Url
	}
	if wh.Events == nil {
		wh.Events = []string{"*"}
	}

	h.mu.Lock()
	snapshot := cloneOpsState(h.state)
	replaced := false
	clearSecret := wh.ClearSecret
	wh.ClearSecret = false
	for i, existing := range h.state.Webhooks {
		if existing.Id == wh.Id {
			if clearSecret {
				wh.Secret = ""
			} else if wh.Secret == "" {
				if webhookURLSameOriginAndPath(existing.Url, wh.Url) {
					wh.Secret = existing.Secret
				} else {
					wh.Secret = ""
				}
			}
			wh.HasSecret = wh.Secret != ""
			h.state.Webhooks[i] = wh
			replaced = true
			break
		}
	}
	if !replaced {
		if len(h.state.Webhooks) >= maxWebhooks {
			h.mu.Unlock()
			httpx.Error(w, http.StatusBadRequest, fmt.Sprintf("maximum number of webhooks (%d) reached", maxWebhooks))
			return
		}
		if clearSecret {
			wh.Secret = ""
		}
		wh.HasSecret = wh.Secret != ""
		h.state.Webhooks = append(h.state.Webhooks, wh)
	}
	err := h.upsertWebhookDBLocked(r.Context(), wh)
	if err == nil {
		err = h.saveStateLocked(r.Context())
	}
	if err != nil {
		h.state = snapshot
	}
	h.mu.Unlock()
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "failed to persist webhook")
		return
	}

	httpx.JSON(w, http.StatusOK, sanitizeWebhookConfig(wh))
}

func (h *Handler) handleDeleteWebhook(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("Id")
	h.mu.Lock()
	for i, wh := range h.state.Webhooks {
		if wh.Id == id {
			snapshot := cloneOpsState(h.state)
			h.state.Webhooks = append(h.state.Webhooks[:i], h.state.Webhooks[i+1:]...)
			err := h.deleteWebhookDBLocked(r.Context(), id)
			if err == nil {
				err = h.saveStateLocked(r.Context())
			}
			if err != nil {
				h.state = snapshot
				h.mu.Unlock()
				httpx.Error(w, http.StatusInternalServerError, "failed to persist webhook deletion")
				return
			}
			h.mu.Unlock()
			httpx.NoContent(w)
			return
		}
	}
	h.mu.Unlock()
	httpx.Error(w, http.StatusNotFound, "webhook not found")
}

func (h *Handler) handleTestWebhook(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Id     string `json:"Id"`
		Url    string `json:"Url"`
		Secret string `json:"Secret"`
	}
	if err := httpx.DecodeJSONLimit(r, &req, 64<<10); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid webhook test payload")
		return
	}
	target := WebhookConfig{Url: req.Url, Secret: req.Secret, Enabled: true}
	if req.Id != "" {
		h.mu.RLock()
		for _, wh := range h.state.Webhooks {
			if wh.Id == req.Id {
				target = wh
				break
			}
		}
		h.mu.RUnlock()
	}
	if strings.TrimSpace(target.Url) == "" {
		httpx.Error(w, http.StatusBadRequest, "webhook Id or Url is required")
		return
	}

	start := time.Now()
	if err := h.validateWebhookURL(target.Url); err != nil {
		httpx.JSON(w, http.StatusOK, WebhookTestResult{
			Success:    false,
			DurationMs: time.Since(start).Milliseconds(),
			Error:      err.Error(),
		})
		return
	}

	statusCode, err := h.sendWebhook(r.Context(), target, WebhookPayload{
		Event:     "WebhookTest",
		ServerId:  h.serverID(),
		Timestamp: time.Now().UTC(),
		Data:      map[string]any{"Message": "Ambra webhook connectivity test"},
	})
	if err != nil {
		httpx.JSON(w, http.StatusOK, WebhookTestResult{
			Success:    false,
			StatusCode: statusCode,
			DurationMs: time.Since(start).Milliseconds(),
			Error:      err.Error(),
		})
		return
	}
	ok := statusCode >= 200 && statusCode < 300
	errMsg := ""
	if !ok {
		errMsg = fmt.Sprintf("HTTP %d", statusCode)
	}
	httpx.JSON(w, http.StatusOK, WebhookTestResult{
		Success:    ok,
		StatusCode: statusCode,
		DurationMs: time.Since(start).Milliseconds(),
		Error:      errMsg,
	})
}

// --- BackupRestore ---

func (h *Handler) handleBackupInfo(w http.ResponseWriter, r *http.Request) {
	dir := h.backupsDir()
	entries, err := os.ReadDir(dir)
	backups := make([]BackupFileInfo, 0)
	if err == nil {
		for _, entry := range entries {
			if entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
				continue
			}
			info, err := entry.Info()
			if err != nil {
				continue
			}
			backups = append(backups, BackupFileInfo{
				Name:        entry.Name(),
				Path:        filepath.Join(dir, entry.Name()),
				Size:        info.Size(),
				DateCreated: info.ModTime().UTC(),
			})
		}
		sort.Slice(backups, func(i, j int) bool {
			return backups[i].DateCreated.After(backups[j].DateCreated)
		})
	}
	httpx.JSON(w, http.StatusOK, BackupInfoResponse{
		BackupDirectory: dir,
		Backups:         backups,
	})
}

func writeFileIntoZip(zw *zip.Writer, archiveName, srcPath string) error {
	info, err := os.Lstat(srcPath)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil
	}
	in, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer in.Close()

	hdr, err := zip.FileInfoHeader(info)
	if err != nil {
		return err
	}
	hdr.Name = filepath.ToSlash(archiveName)
	hdr.Method = zip.Deflate
	w, err := zw.CreateHeader(hdr)
	if err != nil {
		return err
	}
	_, err = io.Copy(w, in)
	return err
}

func (h *Handler) handleCreateBackup(w http.ResponseWriter, r *http.Request) {
	if (h.store == nil || h.store.Writer == nil) && h.cfg == nil {
		httpx.Error(w, http.StatusInternalServerError, "no database store or path configured")
		return
	}

	h.backupMu.Lock()
	defer h.backupMu.Unlock()

	dir := h.backupsDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		httpx.Error(w, http.StatusInternalServerError, fmt.Sprintf("create backup dir: %v", err))
		return
	}
	_ = os.Chmod(dir, 0o700)

	var randSuffix [4]byte
	_, _ = rand.Read(randSuffix[:])
	stamp := fmt.Sprintf("%s-%s", time.Now().UTC().Format("20060102-150405.000"), hex.EncodeToString(randSuffix[:]))
	tmpDBPath := filepath.Join(dir, fmt.Sprintf(".ambra-snapshot-%s.db.tmp", stamp))
	_ = os.Remove(tmpDBPath)
	defer func() { _ = os.Remove(tmpDBPath) }()

	if h.store != nil && h.store.Writer != nil {
		escaped := strings.ReplaceAll(tmpDBPath, "'", "''")
		if _, err := h.store.Writer.ExecContext(r.Context(), fmt.Sprintf("VACUUM INTO '%s'", escaped)); err != nil {
			httpx.Error(w, http.StatusInternalServerError, fmt.Sprintf("sqlite vacuum backup failed: %v", err))
			return
		}
		_ = os.Chmod(tmpDBPath, 0o600)
	} else {
		snap := h.cfg.Snapshot()
		if err := copyFileAtomic(snap.DBPath(), tmpDBPath, 0o600); err != nil {
			httpx.Error(w, http.StatusInternalServerError, fmt.Sprintf("file backup failed: %v", err))
			return
		}
	}

	name := fmt.Sprintf("ambra-backup-%s.zip", stamp)
	dst := filepath.Join(dir, name)
	tmpZip, err := os.CreateTemp(dir, ".ambra-backup-*.zip.tmp")
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, fmt.Sprintf("create backup archive failed: %v", err))
		return
	}
	tmpZipPath := tmpZip.Name()
	cleanupZip := true
	defer func() {
		if cleanupZip {
			_ = os.Remove(tmpZipPath)
		}
	}()
	if err := tmpZip.Chmod(0o600); err != nil {
		_ = tmpZip.Close()
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	zw := zip.NewWriter(tmpZip)
	if err := writeFileIntoZip(zw, "ambra.db", tmpDBPath); err != nil {
		_ = zw.Close()
		_ = tmpZip.Close()
		httpx.Error(w, http.StatusInternalServerError, fmt.Sprintf("archive sqlite snapshot failed: %v", err))
		return
	}

	dataDir := h.dataDir()
	cfgFile := filepath.Join(dataDir, "config", "ambra.json")
	if info, statErr := os.Lstat(cfgFile); statErr == nil && info.Mode().IsRegular() {
		if err := writeFileIntoZip(zw, "config/ambra.json", cfgFile); err != nil {
			_ = zw.Close()
			_ = tmpZip.Close()
			httpx.Error(w, http.StatusInternalServerError, fmt.Sprintf("archive config failed: %v", err))
			return
		}
	}

	usersMetaDir := filepath.Join(dataDir, "metadata", "users")
	if info, statErr := os.Lstat(usersMetaDir); statErr == nil && info.IsDir() && info.Mode()&os.ModeSymlink == 0 {
		_ = filepath.WalkDir(usersMetaDir, func(curPath string, d os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return nil
			}
			if d.Type()&os.ModeSymlink != 0 {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if d.IsDir() {
				return nil
			}
			rel, relErr := filepath.Rel(dataDir, curPath)
			if relErr != nil {
				return nil
			}
			return writeFileIntoZip(zw, filepath.ToSlash(rel), curPath)
		})
	}

	if err := zw.Close(); err != nil {
		_ = tmpZip.Close()
		httpx.Error(w, http.StatusInternalServerError, fmt.Sprintf("finalize backup archive failed: %v", err))
		return
	}
	if err := tmpZip.Sync(); err != nil {
		_ = tmpZip.Close()
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := tmpZip.Close(); err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := os.Rename(tmpZipPath, dst); err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	cleanupZip = false

	info, err := os.Stat(dst)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, BackupFileInfo{
		Name:        name,
		Path:        dst,
		Size:        info.Size(),
		DateCreated: info.ModTime().UTC(),
	})
}

// HandleRestoreBackup exposes backup restoration so compatcore (/BackupRestore/RestoreData) can delegate to it.
func (h *Handler) HandleRestoreBackup(w http.ResponseWriter, r *http.Request) {
	h.handleRestoreBackup(w, r)
}

func (h *Handler) handleRestoreBackup(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name       string `json:"Name"`
		BackupName string `json:"BackupName"`
		Path       string `json:"Path"`
	}
	if err := httpx.DecodeJSONLimit(r, &req, 64<<10); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid restore payload")
		return
	}
	backupName := strings.TrimSpace(req.Name)
	if backupName == "" {
		backupName = strings.TrimSpace(req.BackupName)
	}
	if backupName == "" && strings.TrimSpace(req.Path) != "" {
		backupName = filepath.Base(req.Path)
	}
	if backupName == "" || strings.Contains(backupName, "..") || strings.ContainsAny(backupName, `/\`) {
		httpx.Error(w, http.StatusBadRequest, "valid backup Name is required")
		return
	}

	h.backupMu.Lock()
	defer h.backupMu.Unlock()

	src := filepath.Join(h.backupsDir(), backupName)
	info, err := os.Lstat(src)
	if err != nil {
		httpx.Error(w, http.StatusNotFound, "backup file not found")
		return
	}
	if info.Mode()&os.ModeSymlink != 0 {
		httpx.Error(w, http.StatusBadRequest, "backup symlinks are not allowed")
		return
	}
	if info.IsDir() {
		httpx.Error(w, http.StatusNotFound, "backup file not found")
		return
	}

	isZip, err := isZipArchive(src, backupName)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, fmt.Sprintf("invalid backup file: %v", err))
		return
	}
	if isZip {
		stagedDB, err := h.restoreZipBackupLocked(r.Context(), src)
		if err != nil {
			var statusErr *restoreValidationError
			if errors.As(err, &statusErr) {
				httpx.Error(w, http.StatusBadRequest, statusErr.Error())
				return
			}
			httpx.Error(w, http.StatusInternalServerError, fmt.Sprintf("stage restore failed: %v", err))
			return
		}
		httpx.JSON(w, http.StatusOK, map[string]any{
			"Restored":        true,
			"BackupName":      backupName,
			"StagedPath":      stagedDB,
			"RequiresRestart": true,
		})
		return
	}

	if err := verifySQLiteHeader(src); err != nil {
		httpx.Error(w, http.StatusBadRequest, fmt.Sprintf("invalid sqlite backup file: %v", err))
		return
	}
	if err := verifySQLiteIntegrity(r.Context(), src); err != nil {
		httpx.Error(w, http.StatusBadRequest, fmt.Sprintf("invalid sqlite backup file: %v", err))
		return
	}

	stagedDst := filepath.Join(h.dataDir(), "ambra.db.restore")
	err = copyFileAtomic(src, stagedDst, 0o600)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, fmt.Sprintf("stage restore failed: %v", err))
		return
	}

	httpx.JSON(w, http.StatusOK, map[string]any{
		"Restored":        true,
		"BackupName":      backupName,
		"StagedPath":      stagedDst,
		"RequiresRestart": true,
	})
}

type restoreValidationError struct {
	msg string
}

func (e *restoreValidationError) Error() string { return e.msg }

func isZipArchive(filePath, backupName string) (bool, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return false, err
	}
	defer f.Close()
	hdr := make([]byte, 4)
	n, _ := io.ReadFull(f, hdr)
	if n == 4 && bytes.Equal(hdr, zipMagicHeader) {
		return true, nil
	}
	if strings.HasSuffix(strings.ToLower(backupName), ".zip") {
		return true, nil
	}
	return false, nil
}

func validateSafeZipEntryName(raw string) (string, error) {
	if raw == "" || strings.ContainsRune(raw, 0) {
		return "", &restoreValidationError{msg: "archive contains invalid entry name"}
	}
	slashed := strings.ReplaceAll(raw, "\\", "/")
	if strings.HasPrefix(slashed, "/") {
		return "", &restoreValidationError{msg: "archive contains absolute path entry"}
	}
	for _, part := range strings.Split(slashed, "/") {
		if part == ".." {
			return "", &restoreValidationError{msg: "archive contains path traversal entry"}
		}
	}
	clean := path.Clean(slashed)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || strings.HasPrefix(clean, "/") {
		return "", &restoreValidationError{msg: "archive contains unsafe entry path"}
	}
	return clean, nil
}

func extractZipEntryToFile(zf *zip.File, dstPath string, perm os.FileMode, maxBytes int64, totalRead *int64) error {
	if err := os.MkdirAll(filepath.Dir(dstPath), 0o700); err != nil {
		return err
	}
	rc, err := zf.Open()
	if err != nil {
		return &restoreValidationError{msg: fmt.Sprintf("open archive entry %q: %v", zf.Name, err)}
	}
	defer rc.Close()

	out, err := os.OpenFile(dstPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, perm)
	if err != nil {
		return err
	}
	limited := io.LimitReader(rc, maxBytes+1)
	written, copyErr := io.Copy(out, limited)
	syncErr := out.Sync()
	closeErr := out.Close()
	if copyErr != nil {
		_ = os.Remove(dstPath)
		return &restoreValidationError{msg: fmt.Sprintf("read archive entry %q: %v", zf.Name, copyErr)}
	}
	if written > maxBytes {
		_ = os.Remove(dstPath)
		return &restoreValidationError{msg: fmt.Sprintf("archive entry %q exceeds size limit", zf.Name)}
	}
	*totalRead += written
	if *totalRead > maxBackupRestoreTotalBytes {
		_ = os.Remove(dstPath)
		return &restoreValidationError{msg: "backup archive exceeds maximum total uncompressed size"}
	}
	if syncErr != nil {
		_ = os.Remove(dstPath)
		return syncErr
	}
	if closeErr != nil {
		_ = os.Remove(dstPath)
		return closeErr
	}
	return nil
}

func (h *Handler) restoreZipBackupLocked(ctx context.Context, srcZip string) (string, error) {
	zr, err := zip.OpenReader(srcZip)
	if err != nil {
		return "", &restoreValidationError{msg: fmt.Sprintf("invalid zip backup archive: %v", err)}
	}
	defer zr.Close()

	if len(zr.File) == 0 || len(zr.File) > maxBackupZipEntries {
		return "", &restoreValidationError{msg: "invalid zip backup entry count"}
	}

	dataDir := h.dataDir()
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return "", err
	}
	stageRoot, err := os.MkdirTemp(dataDir, ".ambra-restore-stage-*")
	if err != nil {
		return "", err
	}
	defer func() { _ = os.RemoveAll(stageRoot) }()

	var (
		totalRead   int64
		hasDB       bool
		hasConfig   bool
		hasMetadata bool
		stageDB     = filepath.Join(stageRoot, "ambra.db")
		stageCfg    = filepath.Join(stageRoot, "ambra.json")
		stageMeta   = filepath.Join(stageRoot, "metadata")
	)

	for _, zf := range zr.File {
		if zf.Mode()&os.ModeSymlink != 0 {
			return "", &restoreValidationError{msg: "backup archive symlinks are not allowed"}
		}
		cleanName, err := validateSafeZipEntryName(zf.Name)
		if err != nil {
			return "", err
		}
		if zf.FileInfo().IsDir() {
			continue
		}
		switch {
		case cleanName == "ambra.db":
			if hasDB {
				return "", &restoreValidationError{msg: "duplicate ambra.db entry in backup archive"}
			}
			if err := extractZipEntryToFile(zf, stageDB, 0o600, maxBackupRestoreTotalBytes, &totalRead); err != nil {
				return "", err
			}
			hasDB = true
		case cleanName == "config/ambra.json":
			if hasConfig {
				return "", &restoreValidationError{msg: "duplicate config/ambra.json entry in backup archive"}
			}
			if err := extractZipEntryToFile(zf, stageCfg, 0o600, maxBackupConfigFileBytes, &totalRead); err != nil {
				return "", err
			}
			rawCfg, readErr := os.ReadFile(stageCfg)
			if readErr != nil || !json.Valid(rawCfg) {
				return "", &restoreValidationError{msg: "invalid config/ambra.json in backup archive"}
			}
			hasConfig = true
		case strings.HasPrefix(cleanName, "metadata/users/"):
			relMeta := strings.TrimPrefix(cleanName, "metadata/")
			targetMeta := filepath.Join(stageMeta, filepath.FromSlash(relMeta))
			relCheck, relErr := filepath.Rel(stageMeta, targetMeta)
			if relErr != nil || strings.HasPrefix(relCheck, "..") {
				return "", &restoreValidationError{msg: "unsafe metadata entry path in backup archive"}
			}
			if err := extractZipEntryToFile(zf, targetMeta, 0o644, maxBackupSingleMetaBytes, &totalRead); err != nil {
				return "", err
			}
			hasMetadata = true
		default:
			return "", &restoreValidationError{msg: fmt.Sprintf("disallowed entry %q in backup archive", cleanName)}
		}
	}

	if !hasDB {
		return "", &restoreValidationError{msg: "backup archive missing ambra.db"}
	}
	if err := verifySQLiteHeader(stageDB); err != nil {
		return "", &restoreValidationError{msg: fmt.Sprintf("invalid sqlite database in archive: %v", err)}
	}
	if err := verifySQLiteIntegrity(ctx, stageDB); err != nil {
		return "", &restoreValidationError{msg: fmt.Sprintf("corrupted sqlite database in archive: %v", err)}
	}

	stagedDBDst := filepath.Join(dataDir, "ambra.db.restore")
	if err := copyFileAtomic(stageDB, stagedDBDst, 0o600); err != nil {
		return "", err
	}
	if hasConfig {
		stagedCfgDst := filepath.Join(dataDir, "config", "ambra.json.restore")
		if err := copyFileAtomic(stageCfg, stagedCfgDst, 0o600); err != nil {
			return "", err
		}
	}
	if hasMetadata {
		stagedMetaDst := filepath.Join(dataDir, "metadata.restore")
		_ = os.RemoveAll(stagedMetaDst)
		if err := os.Rename(stageMeta, stagedMetaDst); err != nil {
			return "", err
		}
	}
	return stagedDBDst, nil
}

func verifySQLiteHeader(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	hdr := make([]byte, len(sqliteMagicHeader))
	if _, err := io.ReadFull(f, hdr); err != nil {
		return errors.New("file too short")
	}
	if !bytes.Equal(hdr, sqliteMagicHeader) {
		return errors.New("missing SQLite magic header")
	}
	return nil
}

func verifySQLiteIntegrity(ctx context.Context, path string) error {
	u := url.URL{
		Scheme:   "file",
		Path:     filepath.ToSlash(path),
		RawQuery: "mode=ro&_pragma=immutable(1)",
	}
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return fmt.Errorf("open backup sqlite: %w", err)
	}
	defer db.Close()

	rows, err := db.QueryContext(ctx, "PRAGMA quick_check")
	if err != nil {
		return fmt.Errorf("quick_check failed: %w", err)
	}
	defer rows.Close()

	checked := false
	for rows.Next() {
		var result string
		if err := rows.Scan(&result); err != nil {
			return fmt.Errorf("quick_check scan failed: %w", err)
		}
		checked = true
		if !strings.EqualFold(strings.TrimSpace(result), "ok") {
			return fmt.Errorf("quick_check reported: %s", result)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("quick_check iteration failed: %w", err)
	}
	if !checked {
		return errors.New("quick_check returned no result")
	}
	return nil
}

func copyFileAtomic(src, dst string, perm os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	dir := filepath.Dir(dst)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmpFile, err := os.CreateTemp(dir, ".ambra-copy-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmpFile.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()
	if err := tmpFile.Chmod(perm); err != nil {
		_ = tmpFile.Close()
		return err
	}
	if _, err := io.Copy(tmpFile, in); err != nil {
		_ = tmpFile.Close()
		return err
	}
	if err := tmpFile.Sync(); err != nil {
		_ = tmpFile.Close()
		return err
	}
	if err := tmpFile.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, dst); err != nil {
		return err
	}
	cleanup = false
	return nil
}
