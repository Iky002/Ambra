package compatcore

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/ambra-media/ambra/internal/api/dto"
	"github.com/ambra-media/ambra/internal/api/httpx"
	"github.com/ambra-media/ambra/internal/api/middleware"
	"github.com/ambra-media/ambra/internal/api/ws"
	"github.com/ambra-media/ambra/internal/pkg/guid"
)

// --- /Sessions ---

func (h *Handler) recordRemoteCommand(sessionID, kind, command string, payload map[string]any) {
	h.mu.Lock()
	h.remoteCommands = append(h.remoteCommands, remoteCommandRecord{
		SessionID: sessionID,
		Kind:      kind,
		Command:   command,
		Payload:   payload,
		At:        time.Now().UTC(),
	})
	if len(h.remoteCommands) > 200 {
		h.remoteCommands = h.remoteCommands[len(h.remoteCommands)-200:]
	}
	h.mu.Unlock()
}

func (h *Handler) getSessionPlayQueue(w http.ResponseWriter, r *http.Request) {
	q := httpx.ParseQuery(r)
	filterID := strings.TrimSpace(q.Get("Id"))
	startIndex := q.Int("StartIndex", 0)
	if startIndex < 0 {
		startIndex = 0
	}
	limit := q.Int("Limit", 0)

	items := []dto.BaseItemDto{}
	if h.sessions != nil {
		isAdmin := h.isCallerAdmin(r)
		callerID := h.callerUserID(r)
		for _, s := range h.sessions.ListSessions() {
			if !isAdmin && (callerID == "" || guid.Normalize(s.UserId) != callerID) {
				continue
			}
			if filterID != "" && s.Id != filterID && s.DeviceId != filterID {
				continue
			}
			if s.NowPlayingItem != nil {
				items = append(items, *s.NowPlayingItem)
			}
		}
	}
	total := len(items)
	if startIndex > total {
		startIndex = total
	}
	end := total
	if limit > 0 && startIndex+limit < end {
		end = startIndex + limit
	}
	httpx.JSON(w, http.StatusOK, dto.QueryResult[dto.BaseItemDto]{
		Items:            items[startIndex:end],
		TotalRecordCount: total,
	})
}

func (h *Handler) authorizeSessionTarget(r *http.Request, sid string) (dto.SessionInfo, bool, bool) {
	if h.sessions == nil || sid == "" {
		return dto.SessionInfo{}, false, true
	}
	target, ok := h.sessions.GetSession(sid)
	if !ok {
		return dto.SessionInfo{}, false, true
	}
	if target.UserId != "" {
		if ac, hasAuth := middleware.AuthFromContext(r.Context()); hasAuth && ac != nil && (ac.User != nil || ac.IsAPIKey) {
			if !ac.IsAdmin() && guid.Normalize(ac.UserID()) != guid.Normalize(target.UserId) {
				return target, true, false
			}
		} else if h.auth.UserIDFromCtx != nil {
			callerID := guid.Normalize(h.auth.UserIDFromCtx(r.Context()))
			if callerID == "" || (callerID != guid.Normalize(target.UserId) && !h.isCallerAdmin(r)) {
				return target, true, false
			}
		} else {
			return target, true, false
		}
	}
	return target, true, true
}

func (h *Handler) pushSessionWS(target dto.SessionInfo, found bool, msgType string, data any) {
	if !found || h.wsHub == nil || target.DeviceId == "" {
		return
	}
	h.wsHub.SendToDevice(target.DeviceId, ws.Message{
		MessageType: msgType,
		Data:        data,
	})
}

func (h *Handler) postSessionViewing(w http.ResponseWriter, r *http.Request) {
	sid := chi.URLParam(r, "Id")
	target, found, allowed := h.authorizeSessionTarget(r, sid)
	if !allowed {
		httpx.Error(w, http.StatusForbidden, "forbidden")
		return
	}
	q := httpx.ParseQuery(r)
	payload := map[string]any{
		"ItemId":   q.Get("ItemId"),
		"ItemName": q.Get("ItemName"),
	}
	h.recordRemoteCommand(sid, "Viewing", q.Get("ItemType"), payload)
	h.pushSessionWS(target, found, "GeneralCommand", map[string]any{
		"Name": "DisplayContent",
		"Arguments": map[string]any{
			"ItemId":   q.Get("ItemId"),
			"ItemName": q.Get("ItemName"),
			"ItemType": q.Get("ItemType"),
		},
	})
	httpx.NoContent(w)
}

func (h *Handler) postSessionPlaying(w http.ResponseWriter, r *http.Request) {
	sid := chi.URLParam(r, "Id")
	target, found, allowed := h.authorizeSessionTarget(r, sid)
	if !allowed {
		httpx.Error(w, http.StatusForbidden, "forbidden")
		return
	}
	q := httpx.ParseQuery(r)
	var body map[string]any
	_ = decodeOptionalJSON(r, &body)
	if body == nil {
		body = map[string]any{}
	}
	if ids := q.List("ItemIds"); len(ids) > 0 {
		body["ItemIds"] = ids
	}
	if cmd := q.Get("PlayCommand"); cmd != "" {
		body["PlayCommand"] = cmd
	}
	h.recordRemoteCommand(sid, "Playing", q.Get("PlayCommand"), body)
	h.pushSessionWS(target, found, "Play", body)
	httpx.NoContent(w)
}

func (h *Handler) postSessionPlayingCommand(w http.ResponseWriter, r *http.Request) {
	sid := chi.URLParam(r, "Id")
	target, found, allowed := h.authorizeSessionTarget(r, sid)
	if !allowed {
		httpx.Error(w, http.StatusForbidden, "forbidden")
		return
	}
	cmd := chi.URLParam(r, "Command")
	var body map[string]any
	_ = decodeOptionalJSON(r, &body)
	h.recordRemoteCommand(sid, "PlayingCommand", cmd, body)
	wsPayload := map[string]any{
		"Command": cmd,
	}
	for k, v := range body {
		wsPayload[k] = v
	}
	if seek := strings.TrimSpace(httpx.ParseQuery(r).Get("SeekPositionTicks")); seek != "" {
		if _, exists := wsPayload["SeekPositionTicks"]; !exists {
			if ticks, err := strconv.ParseInt(seek, 10, 64); err == nil {
				wsPayload["SeekPositionTicks"] = ticks
			}
		}
	}
	h.pushSessionWS(target, found, "Playstate", wsPayload)
	httpx.NoContent(w)
}

func (h *Handler) postSessionSystemCommand(w http.ResponseWriter, r *http.Request) {
	sid := chi.URLParam(r, "Id")
	target, found, allowed := h.authorizeSessionTarget(r, sid)
	if !allowed {
		httpx.Error(w, http.StatusForbidden, "forbidden")
		return
	}
	cmd := chi.URLParam(r, "Command")
	h.recordRemoteCommand(sid, "SystemCommand", cmd, nil)
	h.pushSessionWS(target, found, "GeneralCommand", map[string]any{
		"Name": cmd,
	})
	httpx.NoContent(w)
}

func (h *Handler) postSessionCommand(w http.ResponseWriter, r *http.Request) {
	sid := chi.URLParam(r, "Id")
	target, found, allowed := h.authorizeSessionTarget(r, sid)
	if !allowed {
		httpx.Error(w, http.StatusForbidden, "forbidden")
		return
	}
	cmd := chi.URLParam(r, "Command")
	var body map[string]any
	_ = decodeOptionalJSON(r, &body)
	if cmd == "" && body != nil {
		if name, ok := body["Name"].(string); ok {
			cmd = name
		}
	}
	h.recordRemoteCommand(sid, "GeneralCommand", cmd, body)
	wsPayload := map[string]any{}
	for k, v := range body {
		wsPayload[k] = v
	}
	if cmd != "" {
		wsPayload["Name"] = cmd
	}
	h.pushSessionWS(target, found, "GeneralCommand", wsPayload)
	httpx.NoContent(w)
}

func (h *Handler) postSessionMessage(w http.ResponseWriter, r *http.Request) {
	sid := chi.URLParam(r, "Id")
	target, found, allowed := h.authorizeSessionTarget(r, sid)
	if !allowed {
		httpx.Error(w, http.StatusForbidden, "forbidden")
		return
	}
	q := httpx.ParseQuery(r)
	var body map[string]any
	_ = decodeOptionalJSON(r, &body)
	if body == nil {
		body = map[string]any{}
	}
	if hdr := q.Get("Header"); hdr != "" {
		body["Header"] = hdr
	}
	if txt := q.Get("Text"); txt != "" {
		body["Text"] = txt
	}
	h.recordRemoteCommand(sid, "Message", "DisplayMessage", body)
	h.pushSessionWS(target, found, "GeneralCommand", map[string]any{
		"Name":      "DisplayMessage",
		"Arguments": body,
	})
	httpx.NoContent(w)
}

func (h *Handler) postSessionAddUser(w http.ResponseWriter, r *http.Request) {
	sid := chi.URLParam(r, "Id")
	if _, _, allowed := h.authorizeSessionTarget(r, sid); !allowed {
		httpx.Error(w, http.StatusForbidden, "forbidden")
		return
	}
	uid := guid.Normalize(chi.URLParam(r, "UserId"))
	h.mu.Lock()
	if h.sessionUsers[sid] == nil {
		if len(h.sessionUsers) >= maxSessionUsersEntries {
			// Evict an entry to bound memory usage.
			for k := range h.sessionUsers {
				delete(h.sessionUsers, k)
				break
			}
		}
		h.sessionUsers[sid] = make(map[string]bool)
	}
	if len(h.sessionUsers[sid]) < maxSessionUsersEntries || h.sessionUsers[sid][uid] {
		h.sessionUsers[sid][uid] = true
	}
	h.mu.Unlock()
	httpx.NoContent(w)
}

func (h *Handler) deleteSessionUser(w http.ResponseWriter, r *http.Request) {
	sid := chi.URLParam(r, "Id")
	if _, _, allowed := h.authorizeSessionTarget(r, sid); !allowed {
		httpx.Error(w, http.StatusForbidden, "forbidden")
		return
	}
	uid := guid.Normalize(chi.URLParam(r, "UserId"))
	h.mu.Lock()
	if users, ok := h.sessionUsers[sid]; ok {
		delete(users, uid)
	}
	h.mu.Unlock()
	httpx.NoContent(w)
}

func (h *Handler) postSessionPlayingPing(w http.ResponseWriter, r *http.Request) {
	uid := h.resolveUserID(r, "")
	if h.sessions != nil && h.userRepo != nil && uid != "" {
		if u, err := h.userRepo.GetUserByID(r.Context(), uid); err == nil && u != nil {
			h.sessions.TouchSession(u, middleware.ExtractClientInfo(r))
		}
	}
	httpx.NoContent(w)
}

// --- /Devices ---

func (h *Handler) canAccessDevice(r *http.Request, deviceID string) bool {
	if h.isCallerAdmin(r) {
		return true
	}
	callerID := h.callerUserID(r)
	if callerID == "" || deviceID == "" {
		return false
	}
	// Check authoritative device/session ownership first so a spoofed or stale
	// client DeviceID can never bypass ownership of another user's device.
	if h.userRepo != nil {
		if list, err := h.userRepo.ListDevices(r.Context()); err == nil {
			for _, d := range list {
				if d.ID == deviceID {
					return guid.Normalize(d.LastUserID) == callerID
				}
			}
		}
	}
	if h.sessions != nil {
		for _, s := range h.sessions.ListSessions() {
			if s.DeviceId == deviceID {
				return guid.Normalize(s.UserId) == callerID
			}
		}
	}
	if ac, ok := middleware.AuthFromContext(r.Context()); ok && ac != nil && ac.Client.DeviceID == deviceID {
		return true
	}
	return false
}

func (h *Handler) getDeviceInfo(w http.ResponseWriter, r *http.Request) {
	q := httpx.ParseQuery(r)
	deviceID := strings.TrimSpace(q.Get("Id"))
	if !h.canAccessDevice(r, deviceID) {
		httpx.Error(w, http.StatusForbidden, "forbidden")
		return
	}

	if h.userRepo != nil && deviceID != "" {
		if list, err := h.userRepo.ListDevices(r.Context()); err == nil {
			for _, d := range list {
				if d.ID == deviceID {
					info := map[string]any{
						"Id":               d.ID,
						"Name":             d.Name,
						"AppName":          d.AppName,
						"AppVersion":       d.AppVersion,
						"LastUserId":       d.LastUserID,
						"LastUserName":     d.LastUserName,
						"DateLastActivity": d.DateLastActivity.Format(time.RFC3339),
					}
					var opts map[string]any
					if found, _ := h.loadKV(r.Context(), "device_options:"+deviceID, &opts); found {
						if cn, ok := opts["CustomName"].(string); ok && strings.TrimSpace(cn) != "" {
							info["CustomName"] = strings.TrimSpace(cn)
						}
					}
					httpx.JSON(w, http.StatusOK, info)
					return
				}
			}
		}
	}

	writeJSONError(w, r, http.StatusNotFound, "device not found")
}

func (h *Handler) getDeviceOptions(w http.ResponseWriter, r *http.Request) {
	q := httpx.ParseQuery(r)
	deviceID := strings.TrimSpace(q.Get("Id"))
	if !h.canAccessDevice(r, deviceID) {
		httpx.Error(w, http.StatusForbidden, "forbidden")
		return
	}
	opts := map[string]any{"CustomName": ""}
	_, _ = h.loadKV(r.Context(), "device_options:"+deviceID, &opts)
	httpx.JSON(w, http.StatusOK, opts)
}

func (h *Handler) postDeviceOptions(w http.ResponseWriter, r *http.Request) {
	q := httpx.ParseQuery(r)
	deviceID := strings.TrimSpace(q.Get("Id"))
	if !h.canAccessDevice(r, deviceID) {
		httpx.Error(w, http.StatusForbidden, "forbidden")
		return
	}
	body := map[string]any{}
	if err := decodeOptionalJSON(r, &body); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}
	_ = h.saveKV(r.Context(), "device_options:"+deviceID, body)
	httpx.NoContent(w)
}

func (h *Handler) postDevicesDelete(w http.ResponseWriter, r *http.Request) {
	q := httpx.ParseQuery(r)
	deviceID := strings.TrimSpace(q.Get("Id"))
	if deviceID == "" {
		var body struct {
			Id string `json:"Id"`
		}
		_ = decodeOptionalJSON(r, &body)
		deviceID = strings.TrimSpace(body.Id)
	}
	if deviceID != "" {
		if h.authSvc != nil {
			_ = h.authSvc.RevokeDevice(r.Context(), deviceID)
		} else if h.userRepo != nil {
			_ = h.userRepo.DeleteTokensByDevice(r.Context(), deviceID)
		}
		if h.sessions != nil {
			h.sessions.RemoveSessionByDevice(deviceID)
		}
		if h.wsHub != nil {
			if disc, ok := h.wsHub.(interface{ DisconnectDevice(string) }); ok {
				disc.DisconnectDevice(deviceID)
			}
		}
	}
	httpx.NoContent(w)
}

func (h *Handler) getDeviceCameraUploads(w http.ResponseWriter, r *http.Request) {
	q := httpx.ParseQuery(r)
	deviceID := strings.TrimSpace(q.Get("DeviceId"))
	if !h.canAccessDevice(r, deviceID) {
		httpx.Error(w, http.StatusForbidden, "forbidden")
		return
	}
	var files []map[string]any
	_, _ = h.loadKV(r.Context(), "device_camera_uploads:"+deviceID, &files)
	if files == nil {
		files = []map[string]any{}
	}
	httpx.JSON(w, http.StatusOK, map[string]any{
		"DeviceId":      deviceID,
		"FilesUploaded": files,
	})
}

func (h *Handler) postDeviceCameraUploads(w http.ResponseWriter, r *http.Request) {
	q := httpx.ParseQuery(r)
	deviceID := strings.TrimSpace(q.Get("DeviceId"))
	if !h.canAccessDevice(r, deviceID) {
		httpx.Error(w, http.StatusForbidden, "forbidden")
		return
	}
	key := "device_camera_uploads:" + deviceID
	var files []map[string]any
	_, _ = h.loadKV(r.Context(), key, &files)
	entry := map[string]any{
		"Id":         q.Get("Id"),
		"Name":       q.Get("Name"),
		"Album":      q.Get("Album"),
		"UploadedAt": time.Now().UTC().Format(time.RFC3339),
	}
	files = append(files, entry)
	if len(files) > maxCameraUploadsEntries {
		files = files[len(files)-maxCameraUploadsEntries:]
	}
	_ = h.saveKV(r.Context(), key, files)
	httpx.NoContent(w)
}

// --- /Auth ---

func (h *Handler) getAuthProviders(w http.ResponseWriter, r *http.Request) {
	httpx.JSON(w, http.StatusOK, []dto.NameIdPair{
		{Name: "Default", Id: "Ambra.LocalAuthenticationProvider"},
	})
}

func (h *Handler) deleteAuthKey(w http.ResponseWriter, r *http.Request) {
	key := strings.TrimSpace(chi.URLParam(r, "Key"))
	if key != "" {
		if h.authSvc != nil {
			_ = h.authSvc.RevokeAPIKey(r.Context(), key)
		} else if h.userRepo != nil {
			_ = h.userRepo.DeleteAPIKey(r.Context(), key)
		}
	}
	httpx.NoContent(w)
}

// --- /Connect ---

func (h *Handler) connectExchange(w http.ResponseWriter, r *http.Request) {
	q := httpx.ParseQuery(r)
	connectUserID := strings.TrimSpace(q.Get("ConnectUserId"))
	localUserID := h.resolveUserID(r, "")
	if connectUserID == "" || localUserID == "" {
		writeJSONError(w, r, http.StatusUnauthorized, "connect credentials required")
		return
	}
	var link struct {
		UserId          string `json:"UserId"`
		ConnectUsername string `json:"ConnectUsername"`
	}
	found, _ := h.loadKV(r.Context(), "user_connect_link:"+localUserID, &link)
	if !found || strings.TrimSpace(link.ConnectUsername) == "" || !strings.EqualFold(strings.TrimSpace(link.ConnectUsername), connectUserID) {
		writeJSONError(w, r, http.StatusUnauthorized, "invalid or unlinked connect credentials")
		return
	}
	ac, ok := middleware.AuthFromContext(r.Context())
	if !ok || ac == nil || strings.TrimSpace(ac.Token) == "" {
		writeJSONError(w, r, http.StatusUnauthorized, "valid access token required")
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{
		"LocalUserId":   localUserID,
		"AccessToken":   ac.Token,
		"ConnectUserId": connectUserID,
	})
}

// --- /Playback ---

const (
	defaultBitrateTestSize = 102400
	maxBitrateTestSize     = 10 << 20 // 10 MiB
)

func (h *Handler) getPlaybackBitrateTest(w http.ResponseWriter, r *http.Request) {
	q := httpx.ParseQuery(r)
	size := q.Int("Size", defaultBitrateTestSize)
	if size <= 0 {
		size = defaultBitrateTestSize
	}
	if size > maxBitrateTestSize {
		size = maxBitrateTestSize
	}

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", fmt.Sprintf("%d", size))
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)

	chunk := make([]byte, 32*1024)
	for i := range chunk {
		chunk[i] = byte(i % 251)
	}
	remaining := size
	for remaining > 0 {
		n := len(chunk)
		if remaining < n {
			n = remaining
		}
		if _, err := w.Write(chunk[:n]); err != nil {
			return
		}
		remaining -= n
	}
}
