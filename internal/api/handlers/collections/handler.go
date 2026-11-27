package collections

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/ambra-media/ambra/internal/api/dto"
	"github.com/ambra-media/ambra/internal/api/httpx"
	"github.com/ambra-media/ambra/internal/api/middleware"
	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

// AuthFuncs decouples route registration from the concrete middleware package.
type AuthFuncs struct {
	RequireAuth   func(http.Handler) http.Handler
	RequireAdmin  func(http.Handler) http.Handler
	UserIDFromCtx func(context.Context) string
}

type creationResult struct {
	Id   string `json:"Id"`
	Name string `json:"Name"`
}

type Handler struct {
	repo     *sqlite.LibraryRepo
	userRepo *sqlite.UserRepo
	store    *sqlite.Store
	cfg      *config.Config
	auth     AuthFuncs
}

func New(repo *sqlite.LibraryRepo, cfg *config.Config) *Handler {
	return &Handler{repo: repo, cfg: cfg}
}

// SetUserRepo attaches a UserRepo for per-user library RBAC and parental control enforcement.
func (h *Handler) SetUserRepo(r *sqlite.UserRepo) {
	h.userRepo = r
}

// SetStore attaches a sqlite.Store for reading playlist visibility metadata (item_visibility_<id>).
func (h *Handler) SetStore(s *sqlite.Store) {
	h.store = s
}

func (h *Handler) RegisterRoutes(mux *http.ServeMux, auth AuthFuncs) {
	h.auth = auth
	wrapAuth := auth.RequireAuth
	if wrapAuth == nil {
		wrapAuth = passthrough
	}
	wrapAdmin := auth.RequireAdmin
	if wrapAdmin == nil {
		wrapAdmin = wrapAuth
	}

	handleAuth := func(pattern string, fn http.HandlerFunc) {
		mux.Handle(pattern, wrapAuth(fn))
	}
	handleAdmin := func(pattern string, fn http.HandlerFunc) {
		mux.Handle(pattern, wrapAdmin(fn))
	}

	// Collections (6 endpoints) — write/delete routes require Admin
	handleAuth("GET /Collections", h.handleListCollections)
	handleAdmin("POST /Collections", h.handleCreateCollection)
	handleAuth("GET /Collections/{Id}/Items", h.handleGetContainerItems("BoxSet", false))
	handleAdmin("POST /Collections/{Id}/Items", h.handleAddCollectionItems)
	handleAdmin("DELETE /Collections/{Id}/Items", h.handleRemoveCollectionItems)
	handleAdmin("DELETE /Collections/{Id}", h.handleDeleteCollection)

	// Playlists (8 endpoints) — write/delete routes enforce Admin or owner
	handleAuth("GET /Playlists", h.handleListPlaylists)
	handleAuth("POST /Playlists", h.handleCreatePlaylist)
	handleAuth("GET /Playlists/{Id}/Items", h.handleGetContainerItems("Playlist", true))
	handleAuth("POST /Playlists/{Id}/Items", h.handleAddPlaylistItems)
	handleAuth("DELETE /Playlists/{Id}/Items", h.handleRemovePlaylistItems)
	handleAuth("POST /Playlists/{Id}/Items/{ItemId}/Move/{NewIndex}", h.handleMovePlaylistItem)
	handleAuth("GET /Playlists/{Id}/InstantMix", h.handlePlaylistInstantMix)
	handleAuth("DELETE /Playlists/{Id}", h.handleDeletePlaylist)
}

func (h *Handler) serverID() string {
	if h.cfg != nil {
		return h.cfg.Snapshot().ServerID
	}
	return ""
}

func (h *Handler) resolveUserID(r *http.Request, q httpx.Query) string {
	if uid := q.Get("UserId"); uid != "" {
		return guid.Normalize(uid)
	}
	if h.auth.UserIDFromCtx != nil {
		if uid := guid.Normalize(h.auth.UserIDFromCtx(r.Context())); uid != "" {
			return uid
		}
	}
	if ac, ok := middleware.AuthFromContext(r.Context()); ok && ac != nil && ac.User != nil {
		return guid.Normalize(ac.UserID())
	}
	return ""
}

func (h *Handler) canAccessUserData(r *http.Request, targetUserID string) bool {
	targetID := guid.Normalize(targetUserID)
	if targetID == "" {
		return true
	}
	if ac, ok := middleware.AuthFromContext(r.Context()); ok && ac != nil && (ac.User != nil || ac.IsAPIKey) {
		return ac.IsAdmin() || guid.Normalize(ac.UserID()) == targetID
	}
	if h.auth.UserIDFromCtx != nil {
		callerID := guid.Normalize(h.auth.UserIDFromCtx(r.Context()))
		if callerID != "" {
			if callerID == targetID {
				return true
			}
			if h.userRepo != nil {
				if u, err := h.userRepo.GetUserByID(r.Context(), callerID); err == nil && u != nil {
					return u.IsAdmin || u.Policy.IsAdministrator
				}
			}
			return false
		}
	}
	// Unit tests without auth context
	return true
}

func (h *Handler) filterAccessibleItemIDs(r *http.Request, fallbackUserID string, ids []string) []string {
	if len(ids) == 0 {
		return ids
	}
	isNonAdmin := h.isNonAdminCaller(r)
	allowed := h.allowedLibrariesForRequest(r, fallbackUserID)
	userPol := h.userPolicyForRequest(r, fallbackUserID)
	restricted := isNonAdmin || !userPol.IsAdministrator || allowed != nil
	filtered := make([]string, 0, len(ids))
	for _, id := range ids {
		rec, err := h.repo.GetItemRecordByID(r.Context(), id)
		if err != nil || rec == nil {
			if !restricted {
				filtered = append(filtered, id)
			}
			continue
		}
		// Never allow nesting a Playlist inside another Playlist/Collection or adding another user's private Playlist.
		if strings.EqualFold(rec.Type, "Playlist") {
			continue
		}
		if allowed != nil && rec.LibraryID != "" && rec.LibraryID != "collections" && !isLibraryAllowed(rec.LibraryID, allowed) {
			continue
		}
		if !userPol.IsAdministrator {
			if blocked, bErr := h.repo.IsItemBlockedByPolicy(r.Context(), rec, userPol); bErr != nil || blocked {
				continue
			}
		}
		filtered = append(filtered, id)
	}
	return filtered
}

func (h *Handler) allowedLibrariesForRequest(r *http.Request, fallbackUserID string) []string {
	if ac, ok := middleware.AuthFromContext(r.Context()); ok && ac != nil && (ac.User != nil || ac.IsAPIKey) {
		if ac.IsAdmin() {
			return nil
		}
		if h.userRepo != nil {
			return h.allowedLibrariesForUser(r.Context(), guid.Normalize(ac.UserID()))
		}
		if ac.User != nil {
			if h.repo != nil {
				return h.repo.AllowedLibraryIDsForPolicy(r.Context(), ac.User.Policy)
			}
			if !ac.User.Policy.EnableAllFolders {
				allowed := make([]string, 0, len(ac.User.Policy.EnabledFolders))
				for _, fid := range ac.User.Policy.EnabledFolders {
					if norm := guid.Normalize(fid); norm != "" {
						allowed = append(allowed, norm)
					}
				}
				return allowed
			}
		}
		return nil
	}
	if h.userRepo == nil {
		return nil
	}
	uid := ""
	if h.auth.UserIDFromCtx != nil {
		uid = guid.Normalize(h.auth.UserIDFromCtx(r.Context()))
	}
	if uid == "" {
		uid = guid.Normalize(fallbackUserID)
	}
	return h.allowedLibrariesForUser(r.Context(), uid)
}

func (h *Handler) allowedLibrariesForUser(ctx context.Context, userID string) []string {
	if h.userRepo == nil || userID == "" {
		return nil
	}
	u, err := h.userRepo.GetUserByID(ctx, userID)
	if err != nil || u == nil {
		return []string{}
	}
	if u.IsAdmin || u.Policy.IsAdministrator {
		return nil
	}
	if h.repo != nil {
		return h.repo.AllowedLibraryIDsForPolicy(ctx, u.Policy)
	}
	if u.Policy.EnableAllFolders {
		return nil
	}
	allowed := make([]string, 0, len(u.Policy.EnabledFolders))
	for _, fid := range u.Policy.EnabledFolders {
		if norm := guid.Normalize(fid); norm != "" {
			allowed = append(allowed, norm)
		}
	}
	return allowed
}

func isLibraryAllowed(libID string, allowed []string) bool {
	if allowed == nil {
		return true
	}
	norm := guid.Normalize(libID)
	for _, id := range allowed {
		if id == norm {
			return true
		}
	}
	return false
}

func (h *Handler) userParentalPolicyForRequest(r *http.Request, fallbackUserID string) (*int, []string) {
	pol := h.userPolicyForRequest(r, fallbackUserID)
	if pol.IsAdministrator {
		return nil, nil
	}
	return pol.MaxParentalRating, pol.BlockedTags
}

func (h *Handler) isPlaylistPublic(ctx context.Context, playlistID string) bool {
	if h.store == nil || playlistID == "" {
		return false
	}
	var raw string
	err := h.store.Reader.QueryRowContext(ctx,
		`SELECT value_json FROM server_config WHERE key = ?`,
		"item_visibility_"+guid.Normalize(playlistID),
	).Scan(&raw)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false
		}
		return false
	}
	var vis struct {
		IsPublic bool `json:"IsPublic"`
	}
	if err := json.Unmarshal([]byte(raw), &vis); err != nil {
		return false
	}
	return vis.IsPublic
}

func (h *Handler) canViewPlaylist(r *http.Request, playlistID, parentID string) bool {
	ownerID := guid.Normalize(parentID)
	if ac, ok := middleware.AuthFromContext(r.Context()); ok && ac != nil && (ac.User != nil || ac.IsAPIKey) {
		if ac.IsAdmin() {
			return true
		}
		if ownerID == "" || h.isPlaylistPublic(r.Context(), playlistID) {
			return true
		}
		callerID := guid.Normalize(ac.UserID())
		return callerID != "" && callerID == ownerID
	}
	if h.auth.UserIDFromCtx != nil {
		callerID := guid.Normalize(h.auth.UserIDFromCtx(r.Context()))
		if callerID != "" {
			if h.userRepo != nil {
				if u, err := h.userRepo.GetUserByID(r.Context(), callerID); err == nil && u != nil && (u.IsAdmin || u.Policy.IsAdministrator) {
					return true
				}
			}
			if ownerID == "" || h.isPlaylistPublic(r.Context(), playlistID) {
				return true
			}
			return callerID == ownerID
		}
	}
	// Unit tests without auth context
	return true
}

func (h *Handler) isNonAdminCaller(r *http.Request) bool {
	if ac, ok := middleware.AuthFromContext(r.Context()); ok && ac != nil && (ac.User != nil || ac.IsAPIKey) {
		return !ac.IsAdmin()
	}
	if h.auth.UserIDFromCtx != nil {
		callerID := guid.Normalize(h.auth.UserIDFromCtx(r.Context()))
		if callerID != "" {
			if h.userRepo != nil {
				if u, err := h.userRepo.GetUserByID(r.Context(), callerID); err == nil && u != nil && (u.IsAdmin || u.Policy.IsAdministrator) {
					return false
				}
			}
			return true
		}
	}
	return false
}

// --- Collections ---

func (h *Handler) handleListCollections(w http.ResponseWriter, r *http.Request) {
	q := httpx.ParseQuery(r)
	if targetUID := q.Get("UserId"); targetUID != "" && !h.canAccessUserData(r, targetUID) {
		httpx.Error(w, http.StatusForbidden, "forbidden")
		return
	}
	userID := h.resolveUserID(r, q)
	allowed := h.allowedLibrariesForRequest(r, userID)
	userPol := h.userPolicyForRequest(r, userID)
	startIndex := max(0, q.Int("StartIndex", 0))
	limit := clampCollectionsLimit(q.Int("Limit", 0), maxCollectionsQueryLimit, maxCollectionsQueryLimit)

	var maxRating *int
	var blockUnrated, blockedTags, allowedTags []string
	if !userPol.IsAdministrator {
		maxRating = userPol.MaxParentalRating
		blockUnrated = userPol.BlockUnratedItems
		blockedTags = userPol.BlockedTags
		allowedTags = userPol.AllowedTags
	}

	items, total, err := h.repo.QueryItems(r.Context(), sqlite.ItemFilter{
		ServerID:          h.serverID(),
		UserID:            userID,
		AllowedLibraryIDs: allowed,
		MaxParentalRating: maxRating,
		BlockUnratedItems: blockUnrated,
		BlockedTags:       blockedTags,
		AllowedTags:       allowedTags,
		IncludeItemTypes:  []string{"BoxSet"},
		SearchTerm:        q.Get("SearchTerm"),
		SortBy:            q.List("SortBy"),
		SortOrder:         q.Get("SortOrder"),
		StartIndex:        startIndex,
		Limit:             limit,
		Fields:            q.List("Fields"),
	})
	if err != nil {
		slog.Error("internal error in collections handler", "error", err)
		httpx.Error(w, http.StatusInternalServerError, "internal server error")
		return
	}
	if h.isNonAdminCaller(r) {
		dto.SanitizeItemPathsSlice(items)
	}
	httpx.JSON(w, http.StatusOK, dto.QueryResult[dto.BaseItemDto]{
		Items:            items,
		TotalRecordCount: total,
	})
}

func (h *Handler) handleCreateCollection(w http.ResponseWriter, r *http.Request) {
	q := httpx.ParseQuery(r)
	var body struct {
		Name     string   `json:"Name"`
		ParentId string   `json:"ParentId"`
		Ids      []string `json:"Ids"`
	}
	if r.Body != nil {
		_ = httpx.DecodeJSONLimit(r, &body, 0)
	}

	name := strings.TrimSpace(q.Get("Name"))
	if name == "" {
		name = strings.TrimSpace(body.Name)
	}
	if name == "" {
		httpx.Error(w, http.StatusBadRequest, "collection name is required")
		return
	}
	parentID := q.Get("ParentId")
	if parentID == "" {
		parentID = body.ParentId
	}
	ids := q.List("Ids")
	if len(ids) == 0 {
		ids = body.Ids
	}
	ids = h.filterAccessibleItemIDs(r, "", ids)

	rec, err := h.repo.CreateCollection(r.Context(), name, parentID, ids)
	if err != nil {
		slog.Error("internal error in collections handler", "error", err)
		httpx.Error(w, http.StatusInternalServerError, "internal server error")
		return
	}
	httpx.JSON(w, http.StatusOK, creationResult{Id: rec.ID, Name: rec.Name})
}

func (h *Handler) handleAddCollectionItems(w http.ResponseWriter, r *http.Request) {
	containerID := r.PathValue("Id")
	if _, ok := h.requireContainer(w, r, containerID, "BoxSet", false); !ok {
		return
	}
	h.addItemsToContainerWithResp(w, r, true, false)
}

func (h *Handler) handleRemoveCollectionItems(w http.ResponseWriter, r *http.Request) {
	containerID := r.PathValue("Id")
	if _, ok := h.requireContainer(w, r, containerID, "BoxSet", false); !ok {
		return
	}
	h.handleRemoveContainerItems(w, r)
}

func (h *Handler) handleDeleteCollection(w http.ResponseWriter, r *http.Request) {
	h.handleDeleteContainer(w, r, "BoxSet", false)
}

// --- Playlists ---

func (h *Handler) handleListPlaylists(w http.ResponseWriter, r *http.Request) {
	q := httpx.ParseQuery(r)
	if targetUID := q.Get("UserId"); targetUID != "" && !h.canAccessUserData(r, targetUID) {
		httpx.Error(w, http.StatusForbidden, "forbidden")
		return
	}
	userID := h.resolveUserID(r, q)
	userPol := h.userPolicyForRequest(r, userID)
	plViewerUID, plViewerAdmin := h.playlistViewerForRequest(r, userID)
	startIndex := max(0, q.Int("StartIndex", 0))
	limit := clampCollectionsLimit(q.Int("Limit", 0), maxCollectionsQueryLimit, maxCollectionsQueryLimit)

	var maxRating *int
	var blockUnrated, blockedTags, allowedTags []string
	if !userPol.IsAdministrator {
		maxRating = userPol.MaxParentalRating
		blockUnrated = userPol.BlockUnratedItems
		blockedTags = userPol.BlockedTags
		allowedTags = userPol.AllowedTags
	}

	items, total, err := h.repo.QueryItems(r.Context(), sqlite.ItemFilter{
		ServerID:              h.serverID(),
		UserID:                userID,
		PlaylistViewerUserID:  plViewerUID,
		PlaylistViewerIsAdmin: plViewerAdmin,
		MaxParentalRating:     maxRating,
		BlockUnratedItems:     blockUnrated,
		BlockedTags:           blockedTags,
		AllowedTags:           allowedTags,
		IncludeItemTypes:      []string{"Playlist"},
		SearchTerm:            q.Get("SearchTerm"),
		SortBy:                q.List("SortBy"),
		SortOrder:             q.Get("SortOrder"),
		StartIndex:            startIndex,
		Limit:                 limit,
		Fields:                q.List("Fields"),
	})
	if err != nil {
		slog.Error("internal error in collections handler", "error", err)
		httpx.Error(w, http.StatusInternalServerError, "internal server error")
		return
	}
	if h.isNonAdminCaller(r) {
		dto.SanitizeItemPathsSlice(items)
	}
	httpx.JSON(w, http.StatusOK, dto.QueryResult[dto.BaseItemDto]{
		Items:            items,
		TotalRecordCount: total,
	})
}

func (h *Handler) handleCreatePlaylist(w http.ResponseWriter, r *http.Request) {
	q := httpx.ParseQuery(r)
	var body struct {
		Name      string   `json:"Name"`
		MediaType string   `json:"MediaType"`
		UserId    string   `json:"UserId"`
		Ids       []string `json:"Ids"`
	}
	if r.Body != nil {
		_ = httpx.DecodeJSONLimit(r, &body, 0)
	}

	name := strings.TrimSpace(q.Get("Name"))
	if name == "" {
		name = strings.TrimSpace(body.Name)
	}
	if name == "" {
		httpx.Error(w, http.StatusBadRequest, "playlist name is required")
		return
	}
	mediaType := q.Get("MediaType")
	if mediaType == "" {
		mediaType = body.MediaType
	}
	requestedUserID := guid.Normalize(q.Get("UserId"))
	if requestedUserID == "" {
		requestedUserID = guid.Normalize(body.UserId)
	}
	userID := requestedUserID
	if ac, ok := middleware.AuthFromContext(r.Context()); ok && ac != nil && (ac.User != nil || ac.IsAPIKey) {
		callerID := guid.Normalize(ac.UserID())
		if !ac.IsAdmin() {
			if requestedUserID != "" && requestedUserID != callerID {
				httpx.Error(w, http.StatusForbidden, "forbidden")
				return
			}
			userID = callerID
		} else if userID == "" {
			userID = callerID
		}
	} else if userID == "" && h.auth.UserIDFromCtx != nil {
		userID = guid.Normalize(h.auth.UserIDFromCtx(r.Context()))
	}
	if h.isNonAdminCaller(r) && userID != "" {
		ownedCount, cntErr := h.repo.CountPlaylistsByOwner(r.Context(), userID)
		if cntErr != nil {
			slog.Error("failed to check user playlist quota", "user_id", userID, "error", cntErr)
			httpx.Error(w, http.StatusInternalServerError, "internal server error")
			return
		}
		if ownedCount >= maxPlaylistsPerUser {
			httpx.Error(w, http.StatusTooManyRequests, "playlist quota exceeded")
			return
		}
	}
	ids := q.List("Ids")
	if len(ids) == 0 {
		ids = body.Ids
	}
	ids = h.filterAccessibleItemIDs(r, userID, ids)

	rec, err := h.repo.CreatePlaylist(r.Context(), name, mediaType, userID, ids)
	if err != nil {
		slog.Error("internal error in collections handler", "error", err)
		httpx.Error(w, http.StatusInternalServerError, "internal server error")
		return
	}
	httpx.JSON(w, http.StatusOK, creationResult{Id: rec.ID, Name: rec.Name})
}

func (h *Handler) handleAddPlaylistItems(w http.ResponseWriter, r *http.Request) {
	containerID := r.PathValue("Id")
	if _, ok := h.requireContainer(w, r, containerID, "Playlist", true); !ok {
		return
	}
	h.addItemsToContainerWithResp(w, r, false, true)
}

func (h *Handler) handleRemovePlaylistItems(w http.ResponseWriter, r *http.Request) {
	containerID := r.PathValue("Id")
	if _, ok := h.requireContainer(w, r, containerID, "Playlist", true); !ok {
		return
	}
	h.handleRemoveContainerItems(w, r)
}

func (h *Handler) handleDeletePlaylist(w http.ResponseWriter, r *http.Request) {
	h.handleDeleteContainer(w, r, "Playlist", true)
}

func (h *Handler) handleMovePlaylistItem(w http.ResponseWriter, r *http.Request) {
	playlistID := r.PathValue("Id")
	itemID := r.PathValue("ItemId")
	newIndexStr := r.PathValue("NewIndex")

	newIndex, err := strconv.Atoi(newIndexStr)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid NewIndex")
		return
	}
	if _, ok := h.requireContainer(w, r, playlistID, "Playlist", true); !ok {
		return
	}
	if err := h.repo.MoveContainerItem(r.Context(), playlistID, itemID, newIndex); err != nil {
		if errors.Is(err, sqlite.ErrNotFound) {
			httpx.Error(w, http.StatusNotFound, "playlist or item not found")
			return
		}
		slog.Error("internal error in collections container operation", "error", err)
		httpx.Error(w, http.StatusInternalServerError, "internal server error")
		return
	}
	httpx.NoContent(w)
}

func (h *Handler) handlePlaylistInstantMix(w http.ResponseWriter, r *http.Request) {
	q := httpx.ParseQuery(r)
	if targetUID := q.Get("UserId"); targetUID != "" && !h.canAccessUserData(r, targetUID) {
		httpx.Error(w, http.StatusForbidden, "forbidden")
		return
	}
	playlistID := r.PathValue("Id")
	rec, err := h.repo.GetItemRecordByID(r.Context(), playlistID)
	if errors.Is(err, sqlite.ErrNotFound) {
		httpx.Error(w, http.StatusNotFound, "playlist not found")
		return
	}
	if err != nil {
		slog.Error("internal error in collections handler", "error", err)
		httpx.Error(w, http.StatusInternalServerError, "internal server error")
		return
	}
	if !strings.EqualFold(rec.Type, "Playlist") {
		httpx.Error(w, http.StatusNotFound, "playlist not found")
		return
	}
	if !h.canViewPlaylist(r, rec.ID, rec.ParentID) {
		httpx.Error(w, http.StatusForbidden, "forbidden")
		return
	}
	userID := h.resolveUserID(r, q)
	allowed := h.allowedLibrariesForRequest(r, userID)
	userPol := h.userPolicyForRequest(r, userID)
	if !userPol.IsAdministrator {
		blocked, bErr := h.repo.IsItemBlockedByPolicy(r.Context(), rec, userPol)
		if bErr != nil {
			slog.Error("failed to verify parental policy", "error", bErr)
			httpx.Error(w, http.StatusInternalServerError, "failed to verify item access")
			return
		}
		if blocked {
			httpx.Error(w, http.StatusForbidden, "access denied")
			return
		}
	}
	reqLimit := clampCollectionsLimit(q.Int("Limit", 50), 50, maxCollectionsQueryLimit)
	items, total, err := h.repo.GetInstantMixWithUserPolicy(
		r.Context(),
		h.serverID(),
		userID,
		playlistID,
		reqLimit,
		q.List("Fields"),
		allowed,
		userPol,
	)
	if err != nil {
		slog.Error("internal error in collections handler", "error", err)
		httpx.Error(w, http.StatusInternalServerError, "internal server error")
		return
	}
	if h.isNonAdminCaller(r) {
		dto.SanitizeItemPathsSlice(items)
	}
	httpx.JSON(w, http.StatusOK, dto.QueryResult[dto.BaseItemDto]{
		Items:            items,
		TotalRecordCount: total,
	})
}

// --- Shared Container Operations ---

func (h *Handler) handleGetContainerItems(expectedType string, checkPlaylistAccess bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := httpx.ParseQuery(r)
		if targetUID := q.Get("UserId"); targetUID != "" && !h.canAccessUserData(r, targetUID) {
			httpx.Error(w, http.StatusForbidden, "forbidden")
			return
		}
		containerID := r.PathValue("Id")
		rec, err := h.repo.GetItemRecordByID(r.Context(), containerID)
		if errors.Is(err, sqlite.ErrNotFound) {
			httpx.Error(w, http.StatusNotFound, "container not found")
			return
		}
		if err != nil {
			slog.Error("failed to load container", "container_id", containerID, "error", err)
			httpx.Error(w, http.StatusInternalServerError, "failed to load container")
			return
		}
		if !strings.EqualFold(rec.Type, expectedType) {
			httpx.Error(w, http.StatusNotFound, "container not found")
			return
		}
		if checkPlaylistAccess && !h.canViewPlaylist(r, rec.ID, rec.ParentID) {
			httpx.Error(w, http.StatusForbidden, "forbidden")
			return
		}

		userID := h.resolveUserID(r, q)
		allowed := h.allowedLibrariesForRequest(r, userID)
		userPol := h.userPolicyForRequest(r, userID)
		if !userPol.IsAdministrator {
			blocked, bErr := h.repo.IsItemBlockedByPolicy(r.Context(), rec, userPol)
			if bErr != nil {
				slog.Error("failed to check container parental policy", "container_id", containerID, "error", bErr)
				httpx.Error(w, http.StatusInternalServerError, "failed to verify item access")
				return
			}
			if blocked {
				httpx.Error(w, http.StatusForbidden, "access denied")
				return
			}
		}
		startIndex := max(0, q.Int("StartIndex", 0))
		limit := clampCollectionsLimit(q.Int("Limit", 0), maxCollectionsQueryLimit, maxCollectionsQueryLimit)

		queryUserID := userID
		if !userPol.IsAdministrator && queryUserID == "" {
			plUID, _ := h.playlistViewerForRequest(r, userID)
			queryUserID = plUID
		}

		items, total, err := h.repo.GetContainerItemsWithUserPolicy(
			r.Context(),
			h.serverID(),
			queryUserID,
			containerID,
			startIndex,
			limit,
			q.List("Fields"),
			allowed,
			userPol,
		)
		if errors.Is(err, sqlite.ErrNotFound) {
			httpx.Error(w, http.StatusNotFound, "container not found")
			return
		}
		if err != nil {
			slog.Error("failed to get container items", "container_id", containerID, "error", err)
			httpx.Error(w, http.StatusInternalServerError, "failed to get container items")
			return
		}
		if h.isNonAdminCaller(r) {
			for i := range items {
				dto.SanitizeItemPaths(&items[i].BaseItemDto)
			}
		}
		httpx.JSON(w, http.StatusOK, dto.QueryResult[sqlite.PlaylistItemDto]{
			Items:            items,
			TotalRecordCount: total,
		})
	}
}

type playlistAddResult struct {
	ItemAddedCount int `json:"ItemAddedCount"`
}

func (h *Handler) addItemsToContainer(w http.ResponseWriter, r *http.Request, deduplicate bool) {
	h.addItemsToContainerWithResp(w, r, deduplicate, false)
}

func (h *Handler) addItemsToContainerWithResp(w http.ResponseWriter, r *http.Request, deduplicate bool, returnAddedCount bool) {
	q := httpx.ParseQuery(r)
	containerID := r.PathValue("Id")
	ids := q.List("Ids")
	if len(ids) == 0 && r.Body != nil {
		var body struct {
			Ids []string `json:"Ids"`
		}
		if err := httpx.DecodeJSONLimit(r, &body, 0); err == nil {
			ids = body.Ids
		}
	}
	ids = h.filterAccessibleItemIDs(r, "", ids)
	added, err := h.repo.AddItemsToContainer(r.Context(), containerID, ids, deduplicate)
	if err != nil {
		if errors.Is(err, sqlite.ErrNotFound) {
			httpx.Error(w, http.StatusNotFound, "container not found")
			return
		}
		slog.Error("internal error in collections container operation", "error", err)
		httpx.Error(w, http.StatusInternalServerError, "internal server error")
		return
	}
	if returnAddedCount {
		httpx.JSON(w, http.StatusOK, playlistAddResult{ItemAddedCount: added})
		return
	}
	httpx.NoContent(w)
}

func (h *Handler) handleRemoveContainerItems(w http.ResponseWriter, r *http.Request) {
	q := httpx.ParseQuery(r)
	containerID := r.PathValue("Id")
	ids := q.List("EntryIds")
	if len(ids) == 0 {
		ids = q.List("Ids")
	}
	if len(ids) == 0 && r.Body != nil {
		var body struct {
			EntryIds []string `json:"EntryIds"`
			Ids      []string `json:"Ids"`
		}
		if err := httpx.DecodeJSONLimit(r, &body, 0); err == nil {
			if len(body.EntryIds) > 0 {
				ids = body.EntryIds
			} else if len(body.Ids) > 0 {
				ids = body.Ids
			}
		}
	}
	if _, err := h.repo.RemoveItemsFromContainer(r.Context(), containerID, ids); err != nil {
		if errors.Is(err, sqlite.ErrNotFound) {
			httpx.Error(w, http.StatusNotFound, "container not found")
			return
		}
		slog.Error("internal error in collections container operation", "error", err)
		httpx.Error(w, http.StatusInternalServerError, "internal server error")
		return
	}
	httpx.NoContent(w)
}

func (h *Handler) canModifyPlaylist(r *http.Request, rec *sqlite.ItemRecord) bool {
	ownerID := guid.Normalize(rec.ParentID)
	if ac, ok := middleware.AuthFromContext(r.Context()); ok && ac != nil && (ac.User != nil || ac.IsAPIKey) {
		if ac.IsAdmin() {
			return true
		}
		callerID := guid.Normalize(ac.UserID())
		return callerID != "" && ownerID != "" && callerID == ownerID
	}
	if h.auth.UserIDFromCtx != nil {
		callerID := guid.Normalize(h.auth.UserIDFromCtx(r.Context()))
		if callerID != "" {
			return ownerID != "" && callerID == ownerID
		}
	}
	// Unit tests without auth context
	return true
}

func (h *Handler) requireContainer(w http.ResponseWriter, r *http.Request, containerID, expectedType string, checkOwner bool) (*sqlite.ItemRecord, bool) {
	rec, err := h.repo.GetItemRecordByID(r.Context(), containerID)
	if errors.Is(err, sqlite.ErrNotFound) {
		httpx.Error(w, http.StatusNotFound, "container not found")
		return nil, false
	}
	if err != nil {
		slog.Error("internal error in collections container operation", "error", err)
		httpx.Error(w, http.StatusInternalServerError, "internal server error")
		return nil, false
	}
	if !strings.EqualFold(rec.Type, expectedType) {
		httpx.Error(w, http.StatusNotFound, "container not found")
		return nil, false
	}
	if checkOwner && !h.canModifyPlaylist(r, rec) {
		httpx.Error(w, http.StatusForbidden, "forbidden")
		return nil, false
	}
	return rec, true
}

func (h *Handler) handleDeleteContainer(w http.ResponseWriter, r *http.Request, expectedType string, checkOwner bool) {
	containerID := r.PathValue("Id")
	if _, ok := h.requireContainer(w, r, containerID, expectedType, checkOwner); !ok {
		return
	}
	if err := h.repo.DeleteItem(r.Context(), containerID); err != nil {
		if errors.Is(err, sqlite.ErrNotFound) {
			httpx.Error(w, http.StatusNotFound, "container not found")
			return
		}
		slog.Error("internal error in collections container operation", "error", err)
		httpx.Error(w, http.StatusInternalServerError, "internal server error")
		return
	}
	httpx.NoContent(w)
}

func passthrough(next http.Handler) http.Handler { return next }
