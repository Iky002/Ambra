package collections

import (
	"context"
	"encoding/json"
	"errors"
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
	repo *sqlite.LibraryRepo
	cfg  *config.Config
	auth AuthFuncs
}

func New(repo *sqlite.LibraryRepo, cfg *config.Config) *Handler {
	return &Handler{repo: repo, cfg: cfg}
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
	handleAuth("GET /Collections/{Id}/Items", h.handleGetContainerItems)
	handleAdmin("POST /Collections/{Id}/Items", h.handleAddCollectionItems)
	handleAdmin("DELETE /Collections/{Id}/Items", h.handleRemoveCollectionItems)
	handleAdmin("DELETE /Collections/{Id}", h.handleDeleteCollection)

	// Playlists (8 endpoints) — write/delete routes enforce Admin or owner
	handleAuth("GET /Playlists", h.handleListPlaylists)
	handleAuth("POST /Playlists", h.handleCreatePlaylist)
	handleAuth("GET /Playlists/{Id}/Items", h.handleGetContainerItems)
	handleAuth("POST /Playlists/{Id}/Items", h.handleAddPlaylistItems)
	handleAuth("DELETE /Playlists/{Id}/Items", h.handleRemovePlaylistItems)
	handleAuth("POST /Playlists/{Id}/Items/{ItemId}/Move/{NewIndex}", h.handleMovePlaylistItem)
	handleAuth("GET /Playlists/{Id}/InstantMix", h.handlePlaylistInstantMix)
	handleAuth("DELETE /Playlists/{Id}", h.handleDeletePlaylist)
}

func (h *Handler) serverID() string {
	if h.cfg != nil {
		return h.cfg.ServerID
	}
	return ""
}

func (h *Handler) resolveUserID(r *http.Request, q httpx.Query) string {
	if uid := q.Get("UserId"); uid != "" {
		return guid.Normalize(uid)
	}
	if h.auth.UserIDFromCtx != nil {
		return guid.Normalize(h.auth.UserIDFromCtx(r.Context()))
	}
	return ""
}

// --- Collections ---

func (h *Handler) handleListCollections(w http.ResponseWriter, r *http.Request) {
	q := httpx.ParseQuery(r)
	items, total, err := h.repo.QueryItems(r.Context(), sqlite.ItemFilter{
		ServerID:         h.serverID(),
		UserID:           h.resolveUserID(r, q),
		IncludeItemTypes: []string{"BoxSet"},
		SearchTerm:       q.Get("SearchTerm"),
		SortBy:           q.List("SortBy"),
		SortOrder:        q.Get("SortOrder"),
		StartIndex:       q.Int("StartIndex", 0),
		Limit:            q.Int("Limit", 0),
		Fields:           q.List("Fields"),
	})
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
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
		_ = json.NewDecoder(r.Body).Decode(&body)
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

	rec, err := h.repo.CreateCollection(r.Context(), name, parentID, ids)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, creationResult{Id: rec.ID, Name: rec.Name})
}

func (h *Handler) handleAddCollectionItems(w http.ResponseWriter, r *http.Request) {
	containerID := r.PathValue("Id")
	if _, ok := h.requireContainer(w, r, containerID, "BoxSet", false); !ok {
		return
	}
	h.addItemsToContainer(w, r, true)
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
	items, total, err := h.repo.QueryItems(r.Context(), sqlite.ItemFilter{
		ServerID:         h.serverID(),
		UserID:           h.resolveUserID(r, q),
		IncludeItemTypes: []string{"Playlist"},
		SearchTerm:       q.Get("SearchTerm"),
		SortBy:           q.List("SortBy"),
		SortOrder:        q.Get("SortOrder"),
		StartIndex:       q.Int("StartIndex", 0),
		Limit:            q.Int("Limit", 0),
		Fields:           q.List("Fields"),
	})
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
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
		_ = json.NewDecoder(r.Body).Decode(&body)
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
	ids := q.List("Ids")
	if len(ids) == 0 {
		ids = body.Ids
	}

	rec, err := h.repo.CreatePlaylist(r.Context(), name, mediaType, userID, ids)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, creationResult{Id: rec.ID, Name: rec.Name})
}

func (h *Handler) handleAddPlaylistItems(w http.ResponseWriter, r *http.Request) {
	containerID := r.PathValue("Id")
	if _, ok := h.requireContainer(w, r, containerID, "Playlist", true); !ok {
		return
	}
	h.addItemsToContainer(w, r, false)
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
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.NoContent(w)
}

func (h *Handler) handlePlaylistInstantMix(w http.ResponseWriter, r *http.Request) {
	q := httpx.ParseQuery(r)
	playlistID := r.PathValue("Id")
	items, total, err := h.repo.GetInstantMix(
		r.Context(),
		h.serverID(),
		h.resolveUserID(r, q),
		playlistID,
		q.Int("Limit", 50),
		q.List("Fields"),
	)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, dto.QueryResult[dto.BaseItemDto]{
		Items:            items,
		TotalRecordCount: total,
	})
}

// --- Shared Container Operations ---

func (h *Handler) handleGetContainerItems(w http.ResponseWriter, r *http.Request) {
	q := httpx.ParseQuery(r)
	containerID := r.PathValue("Id")
	items, total, err := h.repo.GetContainerItems(
		r.Context(),
		h.serverID(),
		h.resolveUserID(r, q),
		containerID,
		q.Int("StartIndex", 0),
		q.Int("Limit", 0),
		q.List("Fields"),
	)
	if errors.Is(err, sqlite.ErrNotFound) {
		httpx.Error(w, http.StatusNotFound, "container not found")
		return
	}
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, dto.QueryResult[sqlite.PlaylistItemDto]{
		Items:            items,
		TotalRecordCount: total,
	})
}

func (h *Handler) addItemsToContainer(w http.ResponseWriter, r *http.Request, deduplicate bool) {
	q := httpx.ParseQuery(r)
	containerID := r.PathValue("Id")
	ids := q.List("Ids")
	if len(ids) == 0 && r.Body != nil {
		var body struct {
			Ids []string `json:"Ids"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err == nil {
			ids = body.Ids
		}
	}
	if _, err := h.repo.AddItemsToContainer(r.Context(), containerID, ids, deduplicate); err != nil {
		if errors.Is(err, sqlite.ErrNotFound) {
			httpx.Error(w, http.StatusNotFound, "container not found")
			return
		}
		httpx.Error(w, http.StatusInternalServerError, err.Error())
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
	if _, err := h.repo.RemoveItemsFromContainer(r.Context(), containerID, ids); err != nil {
		if errors.Is(err, sqlite.ErrNotFound) {
			httpx.Error(w, http.StatusNotFound, "container not found")
			return
		}
		httpx.Error(w, http.StatusInternalServerError, err.Error())
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
		httpx.Error(w, http.StatusInternalServerError, err.Error())
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
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.NoContent(w)
}

func passthrough(next http.Handler) http.Handler { return next }
