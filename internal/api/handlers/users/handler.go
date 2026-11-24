package users

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/ambra-media/ambra/internal/api/dto"
	"github.com/ambra-media/ambra/internal/api/httpx"
	"github.com/ambra-media/ambra/internal/api/middleware"
	"github.com/ambra-media/ambra/internal/api/ws"
	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/core/auth"
	"github.com/ambra-media/ambra/internal/core/session"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

type Handler struct {
	authSvc    *auth.Service
	userRepo   *sqlite.UserRepo
	sessionMgr *session.Manager
	wsHub      *ws.Hub
	cfg        *config.Config
}

func NewHandler(
	authSvc *auth.Service,
	userRepo *sqlite.UserRepo,
	sessionMgr *session.Manager,
	wsHub *ws.Hub,
	cfg *config.Config,
) *Handler {
	return &Handler{
		authSvc:    authSvc,
		userRepo:   userRepo,
		sessionMgr: sessionMgr,
		wsHub:      wsHub,
		cfg:        cfg,
	}
}

func RegisterRoutes(r chi.Router, authMW *middleware.Auth, h *Handler) {
	// Public authentication routes
	r.Get("/Users/Public", h.ListPublicUsers)
	r.Post("/Users/AuthenticateByName", h.AuthenticateByName)

	// Authenticated user, session, device & display preference routes
	r.Group(func(pr chi.Router) {
		pr.Use(authMW.RequireAuth)

		pr.Get("/Users", h.ListUsers)
		pr.Get("/Users/{Id}", h.GetUser)
		pr.Post("/Users/{Id}/Password", h.UpdateUserPassword)
		pr.Post("/Users/{Id}/Configuration", h.UpdateUserConfiguration)

		pr.Get("/Sessions", h.ListSessions)
		pr.Post("/Sessions/Logout", h.Logout)
		pr.Post("/Sessions/Capabilities", h.UpdateCapabilities)
		pr.Post("/Sessions/Capabilities/Full", h.UpdateCapabilities)
		pr.Post("/Sessions/Playing", h.ReportPlaybackStart)
		pr.Post("/Sessions/Playing/Progress", h.ReportPlaybackProgress)
		pr.Post("/Sessions/Playing/Stopped", h.ReportPlaybackStopped)

		pr.Get("/Devices", h.ListDevices)

		pr.Get("/DisplayPreferences/{Id}", h.GetDisplayPreferences)
		pr.Post("/DisplayPreferences/{Id}", h.UpdateDisplayPreferences)
	})

	// Admin-only routes
	r.Group(func(ar chi.Router) {
		ar.Use(authMW.RequireAdmin)

		ar.Post("/Users/New", h.CreateUser)
		ar.Delete("/Users/{Id}", h.DeleteUser)
		ar.Post("/Users/{Id}/Policy", h.UpdateUserPolicy)

		ar.Get("/Auth/Keys", h.ListAPIKeys)
		ar.Post("/Auth/Keys", h.CreateAPIKey)
		ar.Delete("/Auth/Keys/{Key}", h.DeleteAPIKey)

		ar.Delete("/Devices", h.DeleteDevice)
	})
}

// --- Users & Auth ---

func (h *Handler) ListPublicUsers(w http.ResponseWriter, r *http.Request) {
	users, err := h.userRepo.ListPublicUsers(r.Context())
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "failed to list public users")
		return
	}
	out := make([]dto.UserDto, 0, len(users))
	for _, u := range users {
		out = append(out, h.authSvc.ToUserDto(u))
	}
	httpx.JSON(w, http.StatusOK, out)
}

type authenticateByNameRequest struct {
	Username string `json:"Username"`
	Pw       string `json:"Pw"`
	Password string `json:"Password"` // Legacy SHA1 field
}

func (h *Handler) AuthenticateByName(w http.ResponseWriter, r *http.Request) {
	var req authenticateByNameRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}
	client := middleware.ExtractClientInfo(r)
	res, err := h.authSvc.AuthenticateByName(r.Context(), req.Username, req.Pw, req.Password, client)
	if err != nil {
		switch {
		case errors.Is(err, auth.ErrInvalidCredentials):
			httpx.Error(w, http.StatusUnauthorized, "Invalid username or password")
		case errors.Is(err, auth.ErrUserDisabled):
			httpx.Error(w, http.StatusForbidden, "User account is disabled")
		default:
			httpx.Error(w, http.StatusInternalServerError, "Authentication failed")
		}
		return
	}

	if h.sessionMgr != nil {
		if u, err := h.userRepo.GetUserByID(r.Context(), res.User.Id); err == nil {
			client.DeviceID = res.SessionInfo.DeviceId
			client.Device = res.SessionInfo.DeviceName
			client.Client = res.SessionInfo.Client
			client.Version = res.SessionInfo.ApplicationVersion
			res.SessionInfo = h.sessionMgr.TouchSession(u, client)
		}
	}

	_ = h.userRepo.CreateActivityLog(r.Context(), &sqlite.ActivityLogRecord{
		Name:     res.User.Name + " has logged in",
		Type:     "AuthenticationSucceeded",
		UserID:   res.User.Id,
		Severity: "Info",
	})

	httpx.JSON(w, http.StatusOK, res)
}

func (h *Handler) ListUsers(w http.ResponseWriter, r *http.Request) {
	ac, _ := middleware.AuthFromContext(r.Context())
	if ac.IsAdmin() {
		users, err := h.userRepo.ListUsers(r.Context())
		if err != nil {
			httpx.Error(w, http.StatusInternalServerError, "failed to list users")
			return
		}
		out := make([]dto.UserDto, 0, len(users))
		for _, u := range users {
			out = append(out, h.authSvc.ToUserDto(u))
		}
		httpx.JSON(w, http.StatusOK, out)
		return
	}
	if ac.User != nil {
		httpx.JSON(w, http.StatusOK, []dto.UserDto{h.authSvc.ToUserDto(ac.User)})
		return
	}
	httpx.JSON(w, http.StatusOK, []dto.UserDto{})
}

func (h *Handler) GetUser(w http.ResponseWriter, r *http.Request) {
	ac, _ := middleware.AuthFromContext(r.Context())
	targetID := guid.Normalize(chi.URLParam(r, "Id"))
	if !ac.IsAdmin() && guid.Normalize(ac.UserID()) != targetID {
		httpx.Error(w, http.StatusForbidden, "Forbidden")
		return
	}
	u, err := h.userRepo.GetUserByID(r.Context(), targetID)
	if err != nil {
		if errors.Is(err, sqlite.ErrNotFound) {
			httpx.Error(w, http.StatusNotFound, "User not found")
			return
		}
		httpx.Error(w, http.StatusInternalServerError, "failed to get user")
		return
	}
	httpx.JSON(w, http.StatusOK, h.authSvc.ToUserDto(u))
}

type createUserRequest struct {
	Name     string `json:"Name"`
	Password string `json:"Password"`
	IsAdmin  bool   `json:"IsAdministrator"`
}

func (h *Handler) CreateUser(w http.ResponseWriter, r *http.Request) {
	var req createUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}
	u, err := h.authSvc.CreateUser(r.Context(), req.Name, req.Password, req.IsAdmin)
	if err != nil {
		if errors.Is(err, auth.ErrUserExists) {
			httpx.Error(w, http.StatusConflict, "User already exists")
			return
		}
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, h.authSvc.ToUserDto(u))
}

func (h *Handler) DeleteUser(w http.ResponseWriter, r *http.Request) {
	ac, _ := middleware.AuthFromContext(r.Context())
	targetID := guid.Normalize(chi.URLParam(r, "Id"))
	if ac.User != nil && guid.Normalize(ac.User.ID) == targetID {
		httpx.Error(w, http.StatusBadRequest, "Cannot delete currently authenticated user")
		return
	}
	if err := h.userRepo.DeleteUser(r.Context(), targetID); err != nil {
		if errors.Is(err, sqlite.ErrNotFound) {
			httpx.Error(w, http.StatusNotFound, "User not found")
			return
		}
		httpx.Error(w, http.StatusInternalServerError, "failed to delete user")
		return
	}
	h.authSvc.InvalidateUserCache(targetID)
	httpx.NoContent(w)
}

type updatePasswordRequest struct {
	CurrentPw     string `json:"CurrentPw"`
	NewPw         string `json:"NewPw"`
	ResetPassword bool   `json:"ResetPassword"`
}

func (h *Handler) UpdateUserPassword(w http.ResponseWriter, r *http.Request) {
	ac, _ := middleware.AuthFromContext(r.Context())
	targetID := guid.Normalize(chi.URLParam(r, "Id"))
	isSelf := ac.User != nil && guid.Normalize(ac.User.ID) == targetID
	if !ac.IsAdmin() && !isSelf {
		httpx.Error(w, http.StatusForbidden, "Forbidden")
		return
	}

	var req updatePasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}
	newPw := req.NewPw
	if req.ResetPassword {
		newPw = ""
	}
	err := h.authSvc.ChangePassword(r.Context(), targetID, req.CurrentPw, newPw, ac.IsAdmin() && !isSelf)
	if err != nil {
		switch {
		case errors.Is(err, auth.ErrUserNotFound):
			httpx.Error(w, http.StatusNotFound, "User not found")
		case errors.Is(err, auth.ErrInvalidCredentials):
			httpx.Error(w, http.StatusUnauthorized, "Current password is incorrect")
		default:
			httpx.Error(w, http.StatusInternalServerError, "failed to update password")
		}
		return
	}
	httpx.NoContent(w)
}

func (h *Handler) UpdateUserPolicy(w http.ResponseWriter, r *http.Request) {
	targetID := guid.Normalize(chi.URLParam(r, "Id"))
	var policy dto.UserPolicy
	if err := json.NewDecoder(r.Body).Decode(&policy); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid policy body")
		return
	}
	if err := h.userRepo.UpdateUserPolicy(r.Context(), targetID, policy); err != nil {
		if errors.Is(err, sqlite.ErrNotFound) {
			httpx.Error(w, http.StatusNotFound, "User not found")
			return
		}
		httpx.Error(w, http.StatusInternalServerError, "failed to update user policy")
		return
	}
	h.authSvc.InvalidateUserCache(targetID)
	httpx.NoContent(w)
}

func (h *Handler) UpdateUserConfiguration(w http.ResponseWriter, r *http.Request) {
	ac, _ := middleware.AuthFromContext(r.Context())
	targetID := guid.Normalize(chi.URLParam(r, "Id"))
	if !ac.IsAdmin() && guid.Normalize(ac.UserID()) != targetID {
		httpx.Error(w, http.StatusForbidden, "Forbidden")
		return
	}
	var cfg dto.UserConfiguration
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid configuration body")
		return
	}
	if err := h.userRepo.UpdateUserConfig(r.Context(), targetID, cfg); err != nil {
		if errors.Is(err, sqlite.ErrNotFound) {
			httpx.Error(w, http.StatusNotFound, "User not found")
			return
		}
		httpx.Error(w, http.StatusInternalServerError, "failed to update user configuration")
		return
	}
	h.authSvc.InvalidateUserCache(targetID)
	httpx.NoContent(w)
}

// --- API Keys ---

type APIKeyDto struct {
	AccessToken string    `json:"AccessToken"`
	AppName     string    `json:"AppName"`
	DateCreated time.Time `json:"DateCreated"`
	IsActive    bool      `json:"IsActive"`
}

func (h *Handler) ListAPIKeys(w http.ResponseWriter, r *http.Request) {
	keys, err := h.authSvc.ListAPIKeys(r.Context())
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "failed to list api keys")
		return
	}
	items := make([]APIKeyDto, 0, len(keys))
	for _, k := range keys {
		items = append(items, APIKeyDto{
			AccessToken: k.Token,
			AppName:     k.AppName,
			DateCreated: k.CreatedAt,
			IsActive:    true,
		})
	}
	httpx.JSON(w, http.StatusOK, dto.QueryResult[APIKeyDto]{
		Items:            items,
		TotalRecordCount: len(items),
	})
}

func (h *Handler) CreateAPIKey(w http.ResponseWriter, r *http.Request) {
	q := httpx.ParseQuery(r)
	appName := q.Get("App")
	if appName == "" && r.Body != nil {
		var body struct {
			App     string `json:"App"`
			AppName string `json:"AppName"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err == nil {
			if body.App != "" {
				appName = body.App
			} else if body.AppName != "" {
				appName = body.AppName
			}
		}
	}
	rec, err := h.authSvc.CreateAPIKey(r.Context(), appName)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "failed to create api key")
		return
	}
	httpx.JSON(w, http.StatusOK, APIKeyDto{
		AccessToken: rec.Token,
		AppName:     rec.AppName,
		DateCreated: rec.CreatedAt,
		IsActive:    true,
	})
}

func (h *Handler) DeleteAPIKey(w http.ResponseWriter, r *http.Request) {
	key := chi.URLParam(r, "Key")
	if err := h.authSvc.RevokeAPIKey(r.Context(), key); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "failed to delete api key")
		return
	}
	httpx.NoContent(w)
}

// --- Sessions & Playback ---

func (h *Handler) ListSessions(w http.ResponseWriter, r *http.Request) {
	if h.sessionMgr == nil {
		httpx.JSON(w, http.StatusOK, []dto.SessionInfo{})
		return
	}
	ac, _ := middleware.AuthFromContext(r.Context())
	isAdmin := ac == nil || ac.IsAdmin()
	callerID := ""
	if ac != nil {
		callerID = guid.Normalize(ac.UserID())
	}

	q := httpx.ParseQuery(r)
	deviceID := q.Get("DeviceId")
	userID := guid.Normalize(q.Get("ControllableByUserId"))

	all := h.sessionMgr.ListSessions()
	out := make([]dto.SessionInfo, 0, len(all))
	for _, s := range all {
		if !isAdmin && guid.Normalize(s.UserId) != callerID {
			continue
		}
		if deviceID != "" && s.DeviceId != deviceID {
			continue
		}
		if userID != "" && guid.Normalize(s.UserId) != userID {
			continue
		}
		out = append(out, s)
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	ac, _ := middleware.AuthFromContext(r.Context())
	if ac != nil && ac.Token != "" && !ac.IsAPIKey {
		_ = h.authSvc.RevokeToken(r.Context(), ac.Token)
		if h.sessionMgr != nil && ac.User != nil {
			h.sessionMgr.RemoveSession(ac.User.ID, ac.Client.DeviceID)
		}
	}
	httpx.NoContent(w)
}

func (h *Handler) UpdateCapabilities(w http.ResponseWriter, r *http.Request) {
	ac, _ := middleware.AuthFromContext(r.Context())
	var req session.CapabilitiesRequest
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}
	if h.sessionMgr != nil && ac != nil && ac.User != nil {
		h.sessionMgr.UpdateCapabilities(ac.User, ac.Client, req)
	}
	httpx.NoContent(w)
}

func (h *Handler) ReportPlaybackStart(w http.ResponseWriter, r *http.Request) {
	ac, _ := middleware.AuthFromContext(r.Context())
	var req session.PlaybackStartRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid playback start payload")
		return
	}
	if h.sessionMgr != nil && ac != nil && ac.User != nil {
		_ = h.sessionMgr.ReportPlaybackStart(r.Context(), ac.User, ac.Client, req)
	}
	httpx.NoContent(w)
}

func (h *Handler) ReportPlaybackProgress(w http.ResponseWriter, r *http.Request) {
	ac, _ := middleware.AuthFromContext(r.Context())
	var req session.PlaybackProgressRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid playback progress payload")
		return
	}
	if h.sessionMgr != nil && ac != nil && ac.User != nil {
		_ = h.sessionMgr.ReportPlaybackProgress(r.Context(), ac.User, ac.Client, req)
	}
	httpx.NoContent(w)
}

func (h *Handler) ReportPlaybackStopped(w http.ResponseWriter, r *http.Request) {
	ac, _ := middleware.AuthFromContext(r.Context())
	var req session.PlaybackStoppedRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid playback stopped payload")
		return
	}
	if h.sessionMgr != nil && ac != nil && ac.User != nil {
		_ = h.sessionMgr.ReportPlaybackStopped(r.Context(), ac.User, ac.Client, req)
	}
	httpx.NoContent(w)
}

// --- Devices ---

type DeviceInfoDto struct {
	Name             string    `json:"Name"`
	Id               string    `json:"Id"`
	LastUserName     string    `json:"LastUserName"`
	AppName          string    `json:"AppName"`
	AppVersion       string    `json:"AppVersion"`
	LastUserId       string    `json:"LastUserId"`
	DateLastActivity time.Time `json:"DateLastActivity"`
}

func (h *Handler) ListDevices(w http.ResponseWriter, r *http.Request) {
	ac, _ := middleware.AuthFromContext(r.Context())
	isAdmin := ac == nil || ac.IsAdmin()
	callerID := ""
	if ac != nil {
		callerID = guid.Normalize(ac.UserID())
	}

	devices, err := h.userRepo.ListDevices(r.Context())
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "failed to list devices")
		return
	}
	items := make([]DeviceInfoDto, 0, len(devices))
	for _, d := range devices {
		if !isAdmin && guid.Normalize(d.LastUserID) != callerID {
			continue
		}
		items = append(items, DeviceInfoDto{
			Name:             d.Name,
			Id:               d.ID,
			LastUserName:     d.LastUserName,
			AppName:          d.AppName,
			AppVersion:       d.AppVersion,
			LastUserId:       d.LastUserID,
			DateLastActivity: d.DateLastActivity,
		})
	}
	httpx.JSON(w, http.StatusOK, dto.QueryResult[DeviceInfoDto]{
		Items:            items,
		TotalRecordCount: len(items),
	})
}

func (h *Handler) DeleteDevice(w http.ResponseWriter, r *http.Request) {
	q := httpx.ParseQuery(r)
	deviceID := strings.TrimSpace(q.Get("Id"))
	if deviceID == "" {
		httpx.Error(w, http.StatusBadRequest, "missing Id query parameter")
		return
	}
	if err := h.authSvc.RevokeDevice(r.Context(), deviceID); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "failed to revoke device")
		return
	}
	if h.sessionMgr != nil {
		h.sessionMgr.RemoveSessionByDevice(deviceID)
	}
	if h.wsHub != nil {
		h.wsHub.DisconnectDevice(deviceID)
	}
	httpx.NoContent(w)
}

// --- Display Preferences ---

type DisplayPreferencesDto struct {
	Id                  string            `json:"Id"`
	SortBy              string            `json:"SortBy"`
	SortOrder           string            `json:"SortOrder"`
	Client              string            `json:"Client"`
	CustomPrefs         map[string]string `json:"CustomPrefs"`
	PrimaryImageHeight  int               `json:"PrimaryImageHeight"`
	PrimaryImageWidth   int               `json:"PrimaryImageWidth"`
	ScrollDirection     string            `json:"ScrollDirection"`
	ShowBackdrop        bool              `json:"ShowBackdrop"`
	RememberIndexing    bool              `json:"RememberIndexing"`
	RememberSorting     bool              `json:"RememberSorting"`
	ShowSidebar         bool              `json:"ShowSidebar"`
}

func (h *Handler) GetDisplayPreferences(w http.ResponseWriter, r *http.Request) {
	ac, _ := middleware.AuthFromContext(r.Context())
	id := chi.URLParam(r, "Id")
	q := httpx.ParseQuery(r)
	userID := guid.Normalize(q.Get("userId"))
	callerID := ""
	if ac != nil {
		callerID = guid.Normalize(ac.UserID())
	}
	if userID == "" {
		userID = callerID
	}
	if ac != nil && !ac.IsAdmin() && userID != "" && userID != callerID {
		httpx.Error(w, http.StatusForbidden, "Forbidden")
		return
	}
	client := q.Get("client")
	if client == "" && ac != nil {
		client = ac.Client.Client
	}
	if client == "" {
		client = "emby"
	}

	rec, err := h.userRepo.GetDisplayPreferences(r.Context(), id, userID, client)
	if err == nil && rec != nil && rec.DataJSON != "" && rec.DataJSON != "{}" {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(rec.DataJSON))
		return
	}
	if err != nil && !errors.Is(err, sqlite.ErrNotFound) {
		httpx.Error(w, http.StatusInternalServerError, "failed to get display preferences")
		return
	}

	httpx.JSON(w, http.StatusOK, DisplayPreferencesDto{
		Id:                 id,
		SortBy:             "SortName",
		SortOrder:          "Ascending",
		Client:             client,
		CustomPrefs:        map[string]string{},
		PrimaryImageHeight: 250,
		PrimaryImageWidth:  250,
		ScrollDirection:    "Horizontal",
		ShowBackdrop:       true,
	})
}

func (h *Handler) UpdateDisplayPreferences(w http.ResponseWriter, r *http.Request) {
	ac, _ := middleware.AuthFromContext(r.Context())
	id := chi.URLParam(r, "Id")
	q := httpx.ParseQuery(r)
	userID := guid.Normalize(q.Get("userId"))
	if userID == "" {
		userID = guid.Normalize(ac.UserID())
	}
	if ac != nil && !ac.IsAdmin() && userID != guid.Normalize(ac.UserID()) {
		httpx.Error(w, http.StatusForbidden, "Forbidden")
		return
	}
	client := q.Get("client")
	if client == "" {
		client = ac.Client.Client
	}
	if client == "" {
		client = "emby"
	}

	raw, err := io.ReadAll(io.LimitReader(r.Body, 512<<10))
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "failed to read body")
		return
	}
	if len(raw) == 0 {
		raw = []byte("{}")
	}
	if !json.Valid(raw) {
		httpx.Error(w, http.StatusBadRequest, "invalid json body")
		return
	}

	err = h.userRepo.UpsertDisplayPreferences(r.Context(), &sqlite.DisplayPreferencesRecord{
		ID:       id,
		UserID:   userID,
		Client:   client,
		DataJSON: string(raw),
	})
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "failed to save display preferences")
		return
	}
	httpx.NoContent(w)
}
