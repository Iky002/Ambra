package ops

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
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
	webhookQueueSize   = 256
	webhookWorkerCount = 4
)

var sqliteMagicHeader = []byte("SQLite format 3\x00")

type webhookDispatchJob struct {
	cfg     WebhookConfig
	payload WebhookPayload
}

type WebhookConfig struct {
	Id        string   `json:"Id"`
	Name      string   `json:"Name"`
	Url       string   `json:"Url"`
	Events    []string `json:"Events"`
	Secret    string   `json:"Secret,omitempty"`
	HasSecret bool     `json:"HasSecret,omitempty"`
	Enabled   bool     `json:"Enabled"`
}

type WebhookPayload struct {
	Event     string    `json:"Event"`
	ServerId  string    `json:"ServerId,omitempty"`
	Timestamp time.Time `json:"Timestamp"`
	Data      any       `json:"Data,omitempty"`
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

func (h *Handler) ensureWebhookWorkers() {
	h.webhookOnce.Do(func() {
		h.webhookCh = make(chan webhookDispatchJob, webhookQueueSize)
		for i := 0; i < webhookWorkerCount; i++ {
			go func() {
				for job := range h.webhookCh {
					_, _ = h.sendWebhook(context.Background(), job.cfg, job.payload)
				}
			}()
		}
	})
}

func sanitizeWebhookConfig(wh WebhookConfig) WebhookConfig {
	if wh.Secret != "" {
		wh.HasSecret = true
		wh.Secret = ""
	}
	return wh
}

func (h *Handler) validateWebhookURL(rawURL string) error {
	if err := netutil.ValidateRemoteURL(rawURL, true); err != nil {
		return err
	}
	if h.isCustomHTTPClient() {
		return nil
	}
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return err
	}
	host := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(u.Hostname()), "."))
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return fmt.Errorf("blocked loopback hostname %q", host)
	}
	if addr, err := netip.ParseAddr(host); err == nil {
		if addr.Unmap().IsLoopback() {
			return fmt.Errorf("blocked loopback address %s", addr.Unmap())
		}
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return err
	}
	for _, ia := range ips {
		if addr, ok := netip.AddrFromSlice(ia.IP); ok && addr.Unmap().IsLoopback() {
			return fmt.Errorf("host %q resolved to loopback address %s", host, addr.Unmap())
		}
	}
	return nil
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
	h.ensureWebhookWorkers()

	payload := WebhookPayload{
		Event:     string(e.Type),
		ServerId:  h.serverID(),
		Timestamp: time.Now().UTC(),
		Data:      e.Payload,
	}
	for _, wh := range targets {
		select {
		case h.webhookCh <- webhookDispatchJob{cfg: wh, payload: payload}:
		default:
			slog.Warn("ops: webhook dispatch queue full, dropping event", "webhookId", wh.Id, "event", payload.Event)
		}
	}
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
	body, err := json.Marshal(payload)
	if err != nil {
		return 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, wh.Url, bytes.NewReader(body))
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
	_, _ = io.Copy(io.Discard, resp.Body)
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

func (h *Handler) handleCreateWebhook(w http.ResponseWriter, r *http.Request) {
	var wh WebhookConfig
	if err := httpx.DecodeJSONLimit(r, &wh, 0); err != nil {
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
	replaced := false
	for i, existing := range h.state.Webhooks {
		if existing.Id == wh.Id {
			if wh.Secret == "" {
				wh.Secret = existing.Secret
			}
			wh.HasSecret = wh.Secret != ""
			h.state.Webhooks[i] = wh
			replaced = true
			break
		}
	}
	if !replaced {
		wh.HasSecret = wh.Secret != ""
		h.state.Webhooks = append(h.state.Webhooks, wh)
	}
	err := h.saveStateLocked(r.Context())
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
			h.state.Webhooks = append(h.state.Webhooks[:i], h.state.Webhooks[i+1:]...)
			_ = h.saveStateLocked(r.Context())
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
	if err := httpx.DecodeJSONLimit(r, &req, 0); err != nil {
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
	if err := h.validateWebhookURL(target.Url); err != nil {
		httpx.Error(w, http.StatusBadRequest, fmt.Sprintf("unsafe webhook url: %v", err))
		return
	}

	start := time.Now()
	statusCode, err := h.sendWebhook(r.Context(), target, WebhookPayload{
		Event:     "WebhookTest",
		ServerId:  h.serverID(),
		Timestamp: time.Now().UTC(),
		Data:      map[string]any{"Message": "Ambra webhook connectivity test"},
	})
	if err != nil {
		httpx.Error(w, http.StatusBadGateway, fmt.Sprintf("webhook delivery failed: %v", err))
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{
		"StatusCode": statusCode,
		"DurationMs": time.Since(start).Milliseconds(),
		"Success":    statusCode >= 200 && statusCode < 300,
	})
}

// --- BackupRestore ---

func (h *Handler) handleBackupInfo(w http.ResponseWriter, r *http.Request) {
	dir := h.backupsDir()
	entries, err := os.ReadDir(dir)
	backups := make([]BackupFileInfo, 0)
	if err == nil {
		for _, entry := range entries {
			if entry.IsDir() {
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
	name := fmt.Sprintf("ambra-backup-%s-%s.db", time.Now().UTC().Format("20060102-150405.000"), hex.EncodeToString(randSuffix[:]))
	dst := filepath.Join(dir, name)
	_ = os.Remove(dst)

	if h.store != nil && h.store.Writer != nil {
		escaped := strings.ReplaceAll(dst, "'", "''")
		if _, err := h.store.Writer.ExecContext(r.Context(), fmt.Sprintf("VACUUM INTO '%s'", escaped)); err != nil {
			httpx.Error(w, http.StatusInternalServerError, fmt.Sprintf("sqlite vacuum backup failed: %v", err))
			return
		}
		_ = os.Chmod(dst, 0o600)
	} else {
		if err := copyFileAtomic(h.cfg.DBPath(), dst, 0o600); err != nil {
			httpx.Error(w, http.StatusInternalServerError, fmt.Sprintf("file backup failed: %v", err))
			return
		}
	}

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

func (h *Handler) handleRestoreBackup(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"Name"`
		Path string `json:"Path"`
	}
	if err := httpx.DecodeJSONLimit(r, &req, 0); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid restore payload")
		return
	}
	backupName := strings.TrimSpace(req.Name)
	if backupName == "" && strings.TrimSpace(req.Path) != "" {
		backupName = filepath.Base(req.Path)
	}
	if backupName == "" || strings.Contains(backupName, "..") || strings.ContainsAny(backupName, `/\`) {
		httpx.Error(w, http.StatusBadRequest, "valid backup Name is required")
		return
	}

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
	if err := verifySQLiteHeader(src); err != nil {
		httpx.Error(w, http.StatusBadRequest, fmt.Sprintf("invalid sqlite backup file: %v", err))
		return
	}

	h.backupMu.Lock()
	stagedDst := filepath.Join(h.dataDir(), "ambra.db.restore")
	err = copyFileAtomic(src, stagedDst, 0o600)
	h.backupMu.Unlock()
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
