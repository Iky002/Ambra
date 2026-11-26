package compatcore

import (
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/go-chi/chi/v5"

	"github.com/ambra-media/ambra/internal/api/dto"
	"github.com/ambra-media/ambra/internal/api/httpx"
	"github.com/ambra-media/ambra/internal/api/middleware"
	"github.com/ambra-media/ambra/internal/api/ws"
	"github.com/ambra-media/ambra/internal/core/auth"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

var allowedCameraUploadExts = map[string]bool{
	".jpg":  true,
	".jpeg": true,
	".png":  true,
	".webp": true,
	".gif":  true,
	".heic": true,
	".heif": true,
	".bmp":  true,
	".avif": true,
	".tif":  true,
	".tiff": true,
	".dng":  true,
	".mp4":  true,
	".mov":  true,
	".m4v":  true,
	".mkv":  true,
	".webm": true,
	".avi":  true,
	".3gp":  true,
	".mts":  true,
	".m2ts": true,
}

// --- /Sessions ---

func (h *Handler) getSessionPlayQueue(w http.ResponseWriter, r *http.Request) {
	q := httpx.ParseQuery(r)
	filterID := strings.TrimSpace(q.Get("Id"))
	startIndex := q.Int("StartIndex", 0)
	if startIndex < 0 {
		startIndex = 0
	}
	limit := q.Int("Limit", 0)

	items := []dto.BaseItemDto{}
	isAdmin := h.isCallerAdmin(r)
	if h.sessions != nil {
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
	page := items[startIndex:end]
	if !isAdmin {
		dto.SanitizeItemPathsSlice(page)
	}
	httpx.JSON(w, http.StatusOK, dto.QueryResult[dto.BaseItemDto]{
		Items:            page,
		TotalRecordCount: total,
	})
}

func (h *Handler) authorizeSessionTarget(r *http.Request, sid string) (dto.SessionInfo, bool, bool) {
	if h.sessions == nil {
		return dto.SessionInfo{}, false, true
	}
	sid = strings.TrimSpace(sid)
	if sid == "" {
		if h.isCallerAdmin(r) {
			return dto.SessionInfo{}, false, true
		}
		return dto.SessionInfo{}, false, false
	}
	target, ok := h.sessions.GetSession(sid)
	if !ok {
		if h.isCallerAdmin(r) {
			return dto.SessionInfo{}, false, true
		}
		return dto.SessionInfo{}, false, false
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
	wsHub := h.getWSHub()
	if !found || wsHub == nil || target.DeviceId == "" || target.UserId == "" {
		return
	}
	wsHub.SendToUserDevice(target.UserId, target.DeviceId, ws.Message{
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
	if h.sessions != nil && !found {
		writeJSONError(w, r, http.StatusNotFound, "session not found")
		return
	}
	q := httpx.ParseQuery(r)
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
	if h.sessions != nil && !found {
		writeJSONError(w, r, http.StatusNotFound, "session not found")
		return
	}
	if r.ContentLength > maxSessionCommandBodyBytes {
		httpx.Error(w, http.StatusRequestEntityTooLarge, "request body too large")
		return
	}
	q := httpx.ParseQuery(r)
	var body map[string]any
	if err := decodeOptionalJSONLimit(r, &body, maxSessionCommandBodyBytes); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid or oversized request body")
		return
	}
	if body == nil {
		body = map[string]any{}
	}
	if ids := q.List("ItemIds"); len(ids) > 0 {
		body["ItemIds"] = ids
	}
	if cmd := q.Get("PlayCommand"); cmd != "" {
		body["PlayCommand"] = cmd
	}
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
	if h.sessions != nil && !found {
		writeJSONError(w, r, http.StatusNotFound, "session not found")
		return
	}
	if r.ContentLength > maxSessionCommandBodyBytes {
		httpx.Error(w, http.StatusRequestEntityTooLarge, "request body too large")
		return
	}
	cmd := chi.URLParam(r, "Command")
	var body map[string]any
	if err := decodeOptionalJSONLimit(r, &body, maxSessionCommandBodyBytes); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid or oversized request body")
		return
	}
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
	if h.sessions != nil && !found {
		writeJSONError(w, r, http.StatusNotFound, "session not found")
		return
	}
	cmd := chi.URLParam(r, "Command")
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
	if h.sessions != nil && !found {
		writeJSONError(w, r, http.StatusNotFound, "session not found")
		return
	}
	if r.ContentLength > maxSessionCommandBodyBytes {
		httpx.Error(w, http.StatusRequestEntityTooLarge, "request body too large")
		return
	}
	cmd := chi.URLParam(r, "Command")
	var body map[string]any
	if err := decodeOptionalJSONLimit(r, &body, maxSessionCommandBodyBytes); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid or oversized request body")
		return
	}
	if cmd == "" && body != nil {
		if name, ok := body["Name"].(string); ok {
			cmd = name
		}
	}
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
	if h.sessions != nil && !found {
		writeJSONError(w, r, http.StatusNotFound, "session not found")
		return
	}
	if r.ContentLength > maxSessionCommandBodyBytes {
		httpx.Error(w, http.StatusRequestEntityTooLarge, "request body too large")
		return
	}
	q := httpx.ParseQuery(r)
	var body map[string]any
	if err := decodeOptionalJSONLimit(r, &body, maxSessionCommandBodyBytes); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid or oversized request body")
		return
	}
	if body == nil {
		body = map[string]any{}
	}
	if hdr := q.Get("Header"); hdr != "" {
		body["Header"] = hdr
	}
	if txt := q.Get("Text"); txt != "" {
		body["Text"] = txt
	}
	h.pushSessionWS(target, found, "GeneralCommand", map[string]any{
		"Name":      "DisplayMessage",
		"Arguments": body,
	})
	httpx.NoContent(w)
}

func (h *Handler) postSessionAddUser(w http.ResponseWriter, r *http.Request) {
	sid := chi.URLParam(r, "Id")
	_, found, allowed := h.authorizeSessionTarget(r, sid)
	if !allowed {
		httpx.Error(w, http.StatusForbidden, "forbidden")
		return
	}
	if h.sessions != nil && !found {
		writeJSONError(w, r, http.StatusNotFound, "session not found")
		return
	}
	uid := guid.Normalize(chi.URLParam(r, "UserId"))
	if uid == "" {
		httpx.Error(w, http.StatusBadRequest, "UserId is required")
		return
	}
	if h.sessions != nil {
		foundSess, err := h.sessions.AddAdditionalUser(r.Context(), sid, uid)
		if err != nil {
			if errors.Is(err, sqlite.ErrNotFound) {
				writeJSONError(w, r, http.StatusNotFound, "user not found")
				return
			}
			if errors.Is(err, auth.ErrUserDisabled) {
				httpx.Error(w, http.StatusForbidden, "user is disabled")
				return
			}
			httpx.Error(w, http.StatusInternalServerError, "failed to add additional user to session")
			return
		}
		if !foundSess {
			writeJSONError(w, r, http.StatusNotFound, "session not found")
			return
		}
	}
	httpx.NoContent(w)
}

func (h *Handler) deleteSessionUser(w http.ResponseWriter, r *http.Request) {
	sid := chi.URLParam(r, "Id")
	_, found, allowed := h.authorizeSessionTarget(r, sid)
	if !allowed {
		httpx.Error(w, http.StatusForbidden, "forbidden")
		return
	}
	if h.sessions != nil && !found {
		writeJSONError(w, r, http.StatusNotFound, "session not found")
		return
	}
	uid := guid.Normalize(chi.URLParam(r, "UserId"))
	if h.sessions != nil && uid != "" {
		_ = h.sessions.RemoveAdditionalUser(sid, uid)
	}
	httpx.NoContent(w)
}

func (h *Handler) postSessionPlayingPing(w http.ResponseWriter, r *http.Request) {
	uid := h.resolveUserID(r, "")
	if h.sessions != nil && h.userRepo != nil && uid != "" {
		if u, err := h.userRepo.GetUserByID(r.Context(), uid); err == nil && u != nil {
			h.sessions.TouchSession(u, h.clientInfoForRequest(r))
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
	if r.ContentLength > maxKVJSONBodyBytes {
		httpx.Error(w, http.StatusRequestEntityTooLarge, "request body too large")
		return
	}
	body := map[string]any{}
	if err := decodeOptionalJSONLimit(r, &body, maxKVJSONBodyBytes); err != nil {
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
		wsHub := h.getWSHub()
		if !h.isCallerAdmin(r) {
			callerID := h.callerUserID(r)
			if callerID == "" || !h.canAccessDevice(r, deviceID) {
				httpx.Error(w, http.StatusForbidden, "forbidden")
				return
			}
			if h.store != nil && h.store.Writer != nil {
				_, _ = h.store.Writer.ExecContext(r.Context(),
					`DELETE FROM access_tokens WHERE user_id = ? AND device_id = ?`, callerID, deviceID)
			}
			if h.authSvc != nil {
				h.authSvc.InvalidateUserCache(callerID)
			}
			if h.sessions != nil {
				h.sessions.RemoveSession(callerID, deviceID)
			}
			if wsHub != nil {
				if disc, ok := wsHub.(interface{ DisconnectUserDevice(string, string) }); ok {
					disc.DisconnectUserDevice(callerID, deviceID)
				}
			}
			httpx.NoContent(w)
			return
		}
		if h.authSvc != nil {
			_ = h.authSvc.RevokeDevice(r.Context(), deviceID)
		} else if h.userRepo != nil {
			_ = h.userRepo.DeleteTokensByDevice(r.Context(), deviceID)
		}
		if h.userRepo != nil {
			_ = h.userRepo.DeleteDeviceArtifacts(r.Context(), deviceID)
		}
		if h.sessions != nil {
			h.sessions.RemoveSessionByDevice(deviceID)
		}
		if wsHub != nil {
			if disc, ok := wsHub.(interface{ DisconnectDevice(string) }); ok {
				disc.DisconnectDevice(deviceID)
			}
		}
	}
	httpx.NoContent(w)
}

func sanitizeCameraSegment(raw string, maxRunes int) (string, bool) {
	s := strings.TrimSpace(raw)
	if s == "" || s == "." || s == ".." || strings.Contains(s, "..") || strings.ContainsAny(s, `/\`) {
		return "", false
	}
	var b strings.Builder
	count := 0
	for _, r := range s {
		if unicode.IsControl(r) || r == 0 {
			return "", false
		}
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_' || r == '.' || r == ' ':
			b.WriteRune(r)
			count++
		default:
			b.WriteRune('_')
			count++
		}
		if maxRunes > 0 && count > maxRunes {
			return "", false
		}
	}
	out := strings.Trim(strings.TrimSpace(b.String()), ".")
	if out == "" || out == "." || out == ".." {
		return "", false
	}
	return out, true
}

func validateCameraUploadFilename(rawName string) (string, string, error) {
	trimmed := strings.TrimSpace(rawName)
	if trimmed == "" || strings.Contains(trimmed, "..") || strings.ContainsAny(trimmed, `/\`) {
		return "", "", errors.New("invalid upload file Name")
	}
	for _, r := range trimmed {
		if unicode.IsControl(r) || r == 0 {
			return "", "", errors.New("upload file Name contains control characters")
		}
	}
	ext := strings.ToLower(filepath.Ext(trimmed))
	if !allowedCameraUploadExts[ext] {
		return "", "", fmt.Errorf("unsupported camera upload file extension %q", ext)
	}
	stem := strings.TrimSuffix(trimmed, filepath.Ext(trimmed))
	safeStem, ok := sanitizeCameraSegment(stem, 180)
	if !ok {
		return "", "", errors.New("invalid upload file Name")
	}
	return safeStem + ext, ext, nil
}

func (h *Handler) cameraUploadsRootDir() string {
	if h.cfg != nil {
		if dd := strings.TrimSpace(h.cfg.Snapshot().DataDir); dd != "" {
			return filepath.Join(dd, "camera-uploads")
		}
	}
	return filepath.Join(os.TempDir(), "ambra-camera-uploads")
}

func (h *Handler) getDeviceCameraUploads(w http.ResponseWriter, r *http.Request) {
	q := httpx.ParseQuery(r)
	deviceID := strings.TrimSpace(q.Get("DeviceId"))
	if deviceID == "" {
		httpx.Error(w, http.StatusBadRequest, "DeviceId is required")
		return
	}
	if !h.canAccessDevice(r, deviceID) {
		httpx.Error(w, http.StatusForbidden, "forbidden")
		return
	}
	callerUID := h.callerUserID(r)
	if callerUID == "" {
		callerUID = h.resolveUserID(r, "")
	}
	if callerUID == "" {
		writeJSONError(w, r, http.StatusUnauthorized, "authentication required")
		return
	}

	files := []map[string]any{}
	if h.userRepo != nil {
		records, err := h.userRepo.ListCameraUploads(r.Context(), callerUID, deviceID)
		if err != nil {
			httpx.Error(w, http.StatusInternalServerError, "failed to list camera uploads")
			return
		}
		files = make([]map[string]any, 0, len(records))
		for _, rec := range records {
			files = append(files, map[string]any{
				"Id":         rec.FileID,
				"Name":       rec.Name,
				"Album":      rec.Album,
				"MimeType":   rec.MimeType,
				"Size":       rec.FileSize,
				"UploadedAt": rec.CreatedAt.UTC().Format(time.RFC3339),
			})
		}
	} else {
		_, _ = h.loadKV(r.Context(), "device_camera_uploads:"+callerUID+":"+deviceID, &files)
		if files == nil {
			files = []map[string]any{}
		}
	}

	httpx.JSON(w, http.StatusOK, map[string]any{
		"DeviceId":      deviceID,
		"FilesUploaded": files,
	})
}

func (h *Handler) postDeviceCameraUploads(w http.ResponseWriter, r *http.Request) {
	q := httpx.ParseQuery(r)
	deviceID := strings.TrimSpace(q.Get("DeviceId"))
	if deviceID == "" {
		httpx.Error(w, http.StatusBadRequest, "DeviceId is required")
		return
	}
	if !h.canAccessDevice(r, deviceID) {
		httpx.Error(w, http.StatusForbidden, "forbidden")
		return
	}
	callerUID := h.callerUserID(r)
	if callerUID == "" {
		callerUID = h.resolveUserID(r, "")
	}
	if callerUID == "" {
		writeJSONError(w, r, http.StatusUnauthorized, "authentication required")
		return
	}

	safeName, ext, err := validateCameraUploadFilename(q.Get("Name"))
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	rawAlbum := strings.TrimSpace(q.Get("Album"))
	if rawAlbum == "" {
		rawAlbum = "Camera Roll"
	}
	safeAlbum, ok := sanitizeCameraSegment(rawAlbum, 128)
	if !ok {
		httpx.Error(w, http.StatusBadRequest, "invalid camera upload Album")
		return
	}
	fileID := strings.TrimSpace(q.Get("Id"))
	if fileID == "" {
		fileID = safeName
	}
	if len([]rune(fileID)) > 256 || authControlCharsPresent(fileID) {
		httpx.Error(w, http.StatusBadRequest, "invalid camera upload Id")
		return
	}

	if r.ContentLength > maxCameraUploadBytes {
		httpx.Error(w, http.StatusRequestEntityTooLarge, "camera upload exceeds 512 MiB limit")
		return
	}
	if r.Body == nil {
		httpx.Error(w, http.StatusBadRequest, "camera upload body cannot be empty")
		return
	}

	safeUserDir := sanitizeFilename(callerUID)
	rootDir := h.cameraUploadsRootDir()
	albumDir := filepath.Join(rootDir, safeUserDir, safeAlbum)
	relCheck, relErr := filepath.Rel(rootDir, albumDir)
	if relErr != nil || strings.HasPrefix(relCheck, "..") {
		httpx.Error(w, http.StatusBadRequest, "unsafe upload destination")
		return
	}
	if err := os.MkdirAll(albumDir, 0o750); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "failed to prepare camera upload directory")
		return
	}

	tmpFile, err := os.CreateTemp(albumDir, ".upload-*.tmp")
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "failed to create temporary upload file")
		return
	}
	tmpPath := tmpFile.Name()
	cleanupTmp := true
	defer func() {
		if cleanupTmp {
			_ = os.Remove(tmpPath)
		}
	}()
	_ = tmpFile.Chmod(0o600)

	limitedBody := http.MaxBytesReader(w, r.Body, maxCameraUploadBytes)
	written, copyErr := io.Copy(tmpFile, limitedBody)
	syncErr := tmpFile.Sync()
	closeErr := tmpFile.Close()
	if copyErr != nil {
		var maxErr *http.MaxBytesError
		if errors.As(copyErr, &maxErr) {
			httpx.Error(w, http.StatusRequestEntityTooLarge, "camera upload exceeds 512 MiB limit")
			return
		}
		httpx.Error(w, http.StatusBadRequest, "failed to read camera upload stream")
		return
	}
	if written <= 0 {
		httpx.Error(w, http.StatusBadRequest, "camera upload body cannot be empty")
		return
	}
	if syncErr != nil || closeErr != nil {
		httpx.Error(w, http.StatusInternalServerError, "failed to flush camera upload file")
		return
	}

	finalPath := filepath.Join(albumDir, safeName)
	relFileCheck, relFileErr := filepath.Rel(albumDir, finalPath)
	if relFileErr != nil || strings.HasPrefix(relFileCheck, "..") || strings.ContainsAny(relFileCheck, `/\`) {
		httpx.Error(w, http.StatusBadRequest, "unsafe upload file name")
		return
	}
	if err := os.Rename(tmpPath, finalPath); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "failed to finalize camera upload file")
		return
	}
	cleanupTmp = false

	contentType := strings.TrimSpace(r.Header.Get("Content-Type"))
	if contentType == "" || strings.HasPrefix(strings.ToLower(contentType), "application/json") {
		if guessed := mime.TypeByExtension(ext); guessed != "" {
			contentType = guessed
		} else {
			contentType = "application/octet-stream"
		}
	}
	relPath := filepath.ToSlash(filepath.Join("camera-uploads", safeUserDir, safeAlbum, safeName))

	if h.userRepo != nil {
		if err := h.userRepo.UpsertCameraUpload(r.Context(), &sqlite.CameraUploadRecord{
			UserID:   callerUID,
			DeviceID: deviceID,
			FileID:   fileID,
			Name:     safeName,
			Album:    safeAlbum,
			MimeType: contentType,
			FilePath: relPath,
			FileSize: written,
		}); err != nil {
			_ = os.Remove(finalPath)
			httpx.Error(w, http.StatusInternalServerError, "failed to record camera upload")
			return
		}
	} else {
		kvKey := "device_camera_uploads:" + callerUID + ":" + deviceID
		entry := map[string]any{
			"Id":         fileID,
			"Name":       safeName,
			"Album":      safeAlbum,
			"MimeType":   contentType,
			"Size":       written,
			"UploadedAt": time.Now().UTC().Format(time.RFC3339),
		}
		h.mu.Lock()
		var files []map[string]any
		_, _ = h.loadKVLocked(r.Context(), kvKey, &files)
		files = append(files, entry)
		if len(files) > maxCameraUploadsEntries {
			files = files[len(files)-maxCameraUploadsEntries:]
		}
		_ = h.saveKVLocked(r.Context(), kvKey, files)
		h.mu.Unlock()
	}

	httpx.NoContent(w)
}

func authControlCharsPresent(s string) bool {
	for _, r := range s {
		if unicode.IsControl(r) {
			return true
		}
	}
	return false
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
		var deletedToken string
		if h.userRepo != nil {
			deletedToken, _ = h.userRepo.DeleteAPIKeyByIDOrToken(r.Context(), key)
		} else if h.authSvc != nil {
			_ = h.authSvc.RevokeAPIKey(r.Context(), key)
			deletedToken = key
		}
		evictTok := deletedToken
		if evictTok == "" {
			evictTok = key
		}
		if h.authSvc != nil {
			h.authSvc.InvalidateAPIKey(evictTok)
		}
		if wsHub := h.getWSHub(); wsHub != nil {
			if disc, ok := wsHub.(interface{ DisconnectToken(string) }); ok {
				disc.DisconnectToken(evictTok)
			}
		}
	}
	httpx.NoContent(w)
}

// --- /Connect ---

func (h *Handler) connectExchange(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
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
