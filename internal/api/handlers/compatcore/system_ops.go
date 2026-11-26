package compatcore

import (
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/ambra-media/ambra/internal/api/dto"
	"github.com/ambra-media/ambra/internal/api/httpx"
	"github.com/ambra-media/ambra/internal/api/ws"
	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/pkg/netutil"
)

// --- /System ---

func (h *Handler) postSystemRestart(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	h.restartRequested = true
	cb := h.onRestart
	wsHub := h.wsHub
	h.mu.Unlock()
	if wsHub != nil {
		if bc, ok := wsHub.(interface{ Broadcast(ws.Message) }); ok {
			bc.Broadcast(ws.Message{MessageType: "ServerRestarting", Data: ""})
		}
	}
	if cb != nil {
		go cb()
	}
	httpx.NoContent(w)
}

func (h *Handler) postSystemShutdown(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	h.shutdownRequested = true
	cb := h.onShutdown
	wsHub := h.wsHub
	h.mu.Unlock()
	if wsHub != nil {
		if bc, ok := wsHub.(interface{ Broadcast(ws.Message) }); ok {
			bc.Broadcast(ws.Message{MessageType: "ServerShuttingDown", Data: ""})
		}
	}
	if cb != nil {
		go cb()
	}
	httpx.NoContent(w)
}

func (h *Handler) getSystemEndpoint(w http.ResponseWriter, r *http.Request) {
	clientIP := netutil.ClientIP(r)
	isLocal := false
	if addr, err := netip.ParseAddr(strings.TrimSpace(clientIP)); err == nil {
		addr = addr.WithZone("").Unmap()
		isLocal = addr.IsLoopback()
	}
	isInNetwork := netutil.IsLocalOrLANRequest(r)
	httpx.JSON(w, http.StatusOK, map[string]any{
		"IsLocal":     isLocal,
		"IsInNetwork": isInNetwork,
	})
}

func (h *Handler) getSystemWakeOnLanInfo(w http.ResponseWriter, r *http.Request) {
	if !h.isCallerAdmin(r) {
		writeJSONError(w, r, http.StatusForbidden, "forbidden")
		return
	}
	out := []map[string]any{}
	if ifaces, err := net.Interfaces(); err == nil {
		for _, iface := range ifaces {
			if len(iface.HardwareAddr) == 0 || (iface.Flags&net.FlagLoopback) != 0 {
				continue
			}
			out = append(out, map[string]any{
				"MacAddress": strings.ToUpper(iface.HardwareAddr.String()),
				"Port":       9,
			})
		}
	}
	httpx.JSON(w, http.StatusOK, out)
}

type releaseNote struct {
	Name        string `json:"name"`
	VersionStr  string `json:"versionStr"`
	ReleaseDate string `json:"releaseDate"`
	Description string `json:"description"`
}

func (h *Handler) getSystemReleaseNotesVersions(w http.ResponseWriter, r *http.Request) {
	httpx.JSON(w, http.StatusOK, []releaseNote{
		{
			Name:        config.ProductName + " " + config.ServerVersion,
			VersionStr:  config.ServerVersion,
			ReleaseDate: "2026-01-01T00:00:00Z",
			Description: "Emby 4.10.2.0 compatible Ambra Media Server release.",
		},
	})
}

func (h *Handler) getSystemReleaseNotes(w http.ResponseWriter, r *http.Request) {
	httpx.JSON(w, http.StatusOK, releaseNote{
		Name:        config.ProductName + " " + config.ServerVersion,
		VersionStr:  config.ServerVersion,
		ReleaseDate: "2026-01-01T00:00:00Z",
		Description: "Emby 4.10.2.0 compatible Ambra Media Server release.",
	})
}

func storageFolderInfo(path string) map[string]any {
	info := map[string]any{"Path": path}
	probe := path
	for probe != "" {
		if free, used, ok := diskUsage(probe); ok {
			info["FreeSpace"] = free
			info["UsedSpace"] = used
			return info
		}
		parent := filepath.Dir(probe)
		if parent == probe {
			break
		}
		probe = parent
	}
	return info
}

func (h *Handler) getSystemStorageInfo(w http.ResponseWriter, r *http.Request) {
	dataDir := "./data"
	transcodeTempDir := filepath.Join(dataDir, "cache", "transcoding-temp")
	if h.cfg != nil {
		snap := h.cfg.Snapshot()
		if snap.DataDir != "" {
			dataDir = snap.DataDir
			transcodeTempDir = (&snap).TranscodeTempDir()
		}
	}
	httpx.JSON(w, http.StatusOK, map[string]any{
		"ProgramDataFolder":      storageFolderInfo(dataDir),
		"CacheFolder":            storageFolderInfo(filepath.Join(dataDir, "cache")),
		"LogFolder":              storageFolderInfo(filepath.Join(dataDir, "logs")),
		"InternalMetadataFolder": storageFolderInfo(filepath.Join(dataDir, "metadata")),
		"TranscodingTempFolder":  storageFolderInfo(transcodeTempDir),
	})
}

// --- /Plugins ---

const pluginThumbSVG = `<svg xmlns="http://www.w3.org/2000/svg" width="128" height="128" viewBox="0 0 128 128"><rect width="128" height="128" rx="20" fill="#1e293b"/><text x="64" y="74" font-family="sans-serif" font-size="36" fill="#38bdf8" text-anchor="middle">AM</text></svg>`

func (h *Handler) getPluginThumb(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "image/svg+xml")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(pluginThumbSVG))
}

func (h *Handler) deletePlugin(w http.ResponseWriter, r *http.Request) {
	pluginID := strings.TrimSpace(chi.URLParam(r, "Id"))
	if pluginID != "" {
		_ = h.saveKV(r.Context(), "plugin_uninstalled:"+pluginID, map[string]any{
			"Id":            pluginID,
			"UninstalledAt": time.Now().UTC().Format(time.RFC3339),
		})
	}
	httpx.NoContent(w)
}

func (h *Handler) getPluginSecurityInfo(w http.ResponseWriter, r *http.Request) {
	info := map[string]any{
		"SupporterKey":  "",
		"IsMBSupporter": true,
	}
	_, _ = h.loadKV(r.Context(), "plugins:security_info", &info)
	httpx.JSON(w, http.StatusOK, info)
}

func (h *Handler) postPluginSecurityInfo(w http.ResponseWriter, r *http.Request) {
	body := map[string]any{}
	if err := decodeOptionalJSON(r, &body); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if _, ok := body["IsMBSupporter"]; !ok {
		body["IsMBSupporter"] = true
	}
	_ = h.saveKV(r.Context(), "plugins:security_info", body)
	httpx.NoContent(w)
}

func (h *Handler) getPluginRegistrationRecord(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSpace(chi.URLParam(r, "Name"))
	httpx.JSON(w, http.StatusOK, dto.RegistrationInfo{
		Name:           name,
		ExpirationDate: "2099-12-31T00:00:00Z",
		IsTrial:        false,
		IsRegistered:   true,
	})
}

// --- /Appstore ---

func (h *Handler) postAppstoreRegister(w http.ResponseWriter, r *http.Request) {
	body := map[string]any{}
	if err := decodeOptionalJSON(r, &body); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}
	body["RegisteredAt"] = time.Now().UTC().Format(time.RFC3339)
	_ = h.saveKV(r.Context(), "appstore:registration", body)
	httpx.NoContent(w)
}

// --- /BackupRestore ---

func (h *Handler) postBackupRestoreData(w http.ResponseWriter, r *http.Request) {
	h.mu.RLock()
	restoreFn := h.restoreHandler
	h.mu.RUnlock()
	if restoreFn != nil {
		restoreFn(w, r)
		return
	}
	body := map[string]any{}
	if err := decodeOptionalJSON(r, &body); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}
	name, _ := body["Name"].(string)
	if name == "" {
		name, _ = body["BackupName"].(string)
	}
	if name == "" {
		if p, ok := body["Path"].(string); ok && strings.TrimSpace(p) != "" {
			name = filepath.Base(strings.TrimSpace(p))
		}
	}
	if strings.TrimSpace(name) == "" {
		// Compatibility probe when no backup archive name was supplied
		body["RestoredAt"] = time.Now().UTC().Format(time.RFC3339)
		_ = h.saveKV(r.Context(), "backup:last_restore", body)
		httpx.NoContent(w)
		return
	}
	httpx.Error(w, http.StatusNotFound, "backup restore handler not configured")
}

// --- /Environment ---

type fileSystemEntry struct {
	Name string `json:"Name"`
	Path string `json:"Path"`
	Type string `json:"Type"`
}

func (h *Handler) postEnvironmentDirectoryContents(w http.ResponseWriter, r *http.Request) {
	q := httpx.ParseQuery(r)
	var body struct {
		Path               string `json:"Path"`
		IncludeFiles       bool   `json:"IncludeFiles"`
		IncludeDirectories bool   `json:"IncludeDirectories"`
	}
	body.IncludeDirectories = true
	if err := decodeOptionalJSONLimit(r, &body, maxKVJSONBodyBytes); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}
	explicitTarget := strings.TrimSpace(body.Path)
	if explicitTarget == "" {
		explicitTarget = strings.TrimSpace(q.Get("Path"))
	}
	if b := q.Bool("IncludeFiles"); b != nil {
		body.IncludeFiles = *b
	}
	if b := q.Bool("IncludeDirectories"); b != nil {
		body.IncludeDirectories = *b
	}

	target := explicitTarget
	if target != "" {
		if !filepath.IsAbs(target) {
			httpx.Error(w, http.StatusBadRequest, "Path must be an absolute path")
			return
		}
		target = filepath.Clean(target)
	} else {
		if h.cfg != nil {
			if dd := strings.TrimSpace(h.cfg.Snapshot().DataDir); dd != "" {
				if absDD, err := filepath.Abs(dd); err == nil {
					target = absDD
				} else {
					target = dd
				}
			}
		}
		if target == "" {
			target = "/"
		}
	}

	entries, err := os.ReadDir(target)
	if err != nil {
		httpx.JSON(w, http.StatusOK, []fileSystemEntry{})
		return
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})
	capLen := len(entries)
	if capLen > maxDirectoryContentsEntries {
		capLen = maxDirectoryContentsEntries
	}
	out := make([]fileSystemEntry, 0, capLen)
	for _, e := range entries {
		fullPath := filepath.Join(target, e.Name())
		isDir := e.IsDir()
		if !isDir && e.Type()&os.ModeSymlink != 0 {
			if st, statErr := os.Stat(fullPath); statErr == nil && st.IsDir() {
				isDir = true
			}
		}
		if isDir && !body.IncludeDirectories {
			continue
		}
		if !isDir && !body.IncludeFiles {
			continue
		}
		kind := "File"
		if isDir {
			kind = "Directory"
		}
		out = append(out, fileSystemEntry{
			Name: e.Name(),
			Path: fullPath,
			Type: kind,
		})
		if len(out) >= maxDirectoryContentsEntries {
			break
		}
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (h *Handler) getEnvironmentNetworkDevices(w http.ResponseWriter, r *http.Request) {
	out := []map[string]any{}
	if ifaces, err := net.Interfaces(); err == nil {
		for _, iface := range ifaces {
			addrs, _ := iface.Addrs()
			addrStrs := make([]string, 0, len(addrs))
			for _, a := range addrs {
				addrStrs = append(addrStrs, a.String())
			}
			out = append(out, map[string]any{
				"Name":        iface.Name,
				"MacAddress":  strings.ToUpper(iface.HardwareAddr.String()),
				"Addresses":   addrStrs,
				"IsUp":        (iface.Flags & net.FlagUp) != 0,
				"Description": fmt.Sprintf("Interface %s", iface.Name),
			})
		}
	}
	httpx.JSON(w, http.StatusOK, out)
}

// --- /Packages ---

func (h *Handler) deleteInstallingPackage(w http.ResponseWriter, r *http.Request) {
	pkgID := strings.TrimSpace(chi.URLParam(r, "Id"))
	if pkgID != "" {
		_ = h.deleteKV(r.Context(), "package_installing:"+pkgID)
	}
	httpx.NoContent(w)
}

// --- /Parties ---

func (h *Handler) postPartiesLeave(w http.ResponseWriter, r *http.Request) {
	h.mu.RLock()
	leaveFn := h.partyLeaveHandler
	h.mu.RUnlock()
	if leaveFn != nil {
		leaveFn(w, r)
		return
	}
	uid := h.resolveUserID(r, "")
	_ = h.saveKV(r.Context(), "party:last_leave:"+uid, map[string]any{
		"UserId": uid,
		"LeftAt": time.Now().UTC().Format(time.RFC3339),
	})
	httpx.NoContent(w)
}

func (h *Handler) postPartiesMessages(w http.ResponseWriter, r *http.Request) {
	h.mu.RLock()
	msgFn := h.partyMsgHandler
	h.mu.RUnlock()
	if msgFn != nil {
		msgFn(w, r)
		return
	}
	if r.ContentLength > maxPartyMessageBytes {
		httpx.Error(w, http.StatusRequestEntityTooLarge, "message too large")
		return
	}
	uid := h.resolveUserID(r, "")
	body := map[string]any{}
	if err := decodeOptionalJSONLimit(r, &body, maxPartyMessageBytes); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid or oversized message body")
		return
	}
	body["UserId"] = uid
	body["SentAt"] = time.Now().UTC().Format(time.RFC3339)
	h.mu.Lock()
	var msgs []map[string]any
	_, _ = h.loadKVLocked(r.Context(), "party:messages", &msgs)
	msgs = append(msgs, body)
	if len(msgs) > 100 {
		msgs = msgs[len(msgs)-100:]
	}
	_ = h.saveKVLocked(r.Context(), "party:messages", msgs)
	h.mu.Unlock()
	httpx.NoContent(w)
}

// --- /ScheduledTasks ---

func (h *Handler) deleteRunningScheduledTask(w http.ResponseWriter, r *http.Request) {
	taskID := strings.TrimSpace(chi.URLParam(r, "Id"))
	if taskID != "" {
		h.mu.RLock()
		stopFn := h.taskStopper
		h.mu.RUnlock()
		if stopFn != nil {
			stopFn(taskID)
		}
		_ = h.saveKV(r.Context(), "scheduled_task:stopped:"+taskID, map[string]any{
			"Id":        taskID,
			"StoppedAt": time.Now().UTC().Format(time.RFC3339),
		})
	}
	httpx.NoContent(w)
}

// --- /Webhooks ---

func (h *Handler) getWebhooksSample(w http.ResponseWriter, r *http.Request) {
	httpx.JSON(w, http.StatusOK, map[string]any{
		"Title":       "Sample Webhook Notification",
		"Date":        time.Now().UTC().Format(time.RFC3339),
		"Event":       "system.notificationtest",
		"Description": "This is a sample webhook payload from Ambra Media Server.",
		"Severity":    "Info",
		"Server": map[string]any{
			"Name":    config.ProductName,
			"Id":      h.serverID(),
			"Version": config.ServerVersion,
		},
	})
}
