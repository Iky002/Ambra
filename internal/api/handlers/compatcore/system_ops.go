package compatcore

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/ambra-media/ambra/internal/api/dto"
	"github.com/ambra-media/ambra/internal/api/httpx"
	"github.com/ambra-media/ambra/internal/config"
)

// --- /System ---

func (h *Handler) postSystemRestart(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	h.restartRequested = true
	cb := h.onRestart
	h.mu.Unlock()
	if cb != nil {
		go cb()
	}
	httpx.NoContent(w)
}

func (h *Handler) postSystemShutdown(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	h.shutdownRequested = true
	cb := h.onShutdown
	h.mu.Unlock()
	if cb != nil {
		go cb()
	}
	httpx.NoContent(w)
}

func (h *Handler) getSystemEndpoint(w http.ResponseWriter, r *http.Request) {
	host := r.RemoteAddr
	if hPart, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		host = hPart
	}
	ip := net.ParseIP(strings.TrimSpace(host))
	isLocal := ip != nil && ip.IsLoopback()
	isInNetwork := isLocal || (ip != nil && ip.IsPrivate())
	httpx.JSON(w, http.StatusOK, map[string]any{
		"IsLocal":     isLocal,
		"IsInNetwork": isInNetwork,
	})
}

func (h *Handler) getSystemWakeOnLanInfo(w http.ResponseWriter, r *http.Request) {
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
	if h.cfg != nil && h.cfg.DataDir != "" {
		dataDir = h.cfg.DataDir
	}
	httpx.JSON(w, http.StatusOK, map[string]any{
		"ProgramDataFolder":      storageFolderInfo(dataDir),
		"CacheFolder":            storageFolderInfo(filepath.Join(dataDir, "cache")),
		"LogFolder":              storageFolderInfo(filepath.Join(dataDir, "logs")),
		"InternalMetadataFolder": storageFolderInfo(filepath.Join(dataDir, "metadata")),
		"TranscodingTempFolder":  storageFolderInfo(filepath.Join(dataDir, "cache", "transcoding-temp")),
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
	body := map[string]any{}
	if err := decodeOptionalJSON(r, &body); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}
	body["RestoredAt"] = time.Now().UTC().Format(time.RFC3339)
	_ = h.saveKV(r.Context(), "backup:last_restore", body)
	httpx.NoContent(w)
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
	_ = decodeOptionalJSON(r, &body)
	target := strings.TrimSpace(body.Path)
	if target == "" {
		target = strings.TrimSpace(q.Get("Path"))
	}
	if b := q.Bool("IncludeFiles"); b != nil {
		body.IncludeFiles = *b
	}
	if b := q.Bool("IncludeDirectories"); b != nil {
		body.IncludeDirectories = *b
	}
	if target == "" {
		if h.cfg != nil && h.cfg.DataDir != "" {
			target = h.cfg.DataDir
		} else {
			target = "/"
		}
	}

	entries, err := os.ReadDir(target)
	if err != nil {
		httpx.JSON(w, http.StatusOK, []fileSystemEntry{})
		return
	}
	out := make([]fileSystemEntry, 0, len(entries))
	for _, e := range entries {
		isDir := e.IsDir()
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
			Path: filepath.Join(target, e.Name()),
			Type: kind,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Name < out[j].Name
	})
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
	uid := h.resolveUserID(r, "")
	_ = h.saveKV(r.Context(), "party:last_leave:"+uid, map[string]any{
		"UserId": uid,
		"LeftAt": time.Now().UTC().Format(time.RFC3339),
	})
	httpx.NoContent(w)
}

func (h *Handler) postPartiesMessages(w http.ResponseWriter, r *http.Request) {
	uid := h.resolveUserID(r, "")
	body := map[string]any{}
	_ = decodeOptionalJSON(r, &body)
	body["UserId"] = uid
	body["SentAt"] = time.Now().UTC().Format(time.RFC3339)
	var msgs []map[string]any
	_, _ = h.loadKV(r.Context(), "party:messages", &msgs)
	msgs = append(msgs, body)
	if len(msgs) > 100 {
		msgs = msgs[len(msgs)-100:]
	}
	_ = h.saveKV(r.Context(), "party:messages", msgs)
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
