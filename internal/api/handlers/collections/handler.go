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

	handleAuth := func(pattern string, fn http.HandlerFunc) {
		mux.Handle(pattern, wrapAuth(fn))
	}

	// Collections (6 endpoints)
	handleAuth("GET /Collections", h.handleListCollections)
	handleAuth("POST /Collections", h.handleCreateCollection)
	handleAuth("GET /Collections/{Id}/Items", h.handleGetContainerItems)
	handleAuth("POST /Collections/{Id}/Items", h.handleAddCollectionItems)
	handleAuth("DELETE /Collections/{Id}/Items", h.handleRemoveContainerItems)
	handleAuth("DELETE /Collections/{Id}", h.handleDeleteContainer)

	// Playlists (8 endpoints)
	handleAuth("GET /Playlists", h.handleListPlaylists)
	handleAuth("POST /Playlists", h.handleCreatePlaylist)
	handleAuth("GET /Playlists/{Id}/Items", h.handleGetContainerItems)
	handleAuth("POST /Playlists/{Id}/Items", h.handleAddPlaylistItems)
	handleAuth("DELETE /Playlists/{Id}/Items", h.handleRemoveContainerItems)
	handleAuth("POST /Playlists/{Id}/Items/{ItemId}/Move/{NewIndex}", h.handleMovePlaylistItem)
	handleAuth("GET /Playlists/{Id}/InstantMix", h.handlePlaylistInstantMix)
	handleAuth("DELETE /Playlists/{Id}", h.handleDeleteContainer)
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
	h.addItemsToContainer(w, r, true)
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
	userID := h.resolveUserID(r, q)
	if userID == "" {
		userID = body.UserId
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
	h.addItemsToContainer(w, r, false)
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

func (h *Handler) handleDeleteContainer(w http.ResponseWriter, r *http.Request) {
	containerID := r.PathValue("Id")
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
