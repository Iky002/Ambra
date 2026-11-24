package library

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
	"github.com/ambra-media/ambra/internal/core/scanner"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

// AuthFuncs decouples route registration from the concrete middleware package.
type AuthFuncs struct {
	RequireAuth   func(http.Handler) http.Handler
	RequireAdmin  func(http.Handler) http.Handler
	UserIDFromCtx func(context.Context) string
}

type Handler struct {
	repo    *sqlite.LibraryRepo
	scanner *scanner.Scanner
	cfg     *config.Config
	auth    AuthFuncs
}

func New(repo *sqlite.LibraryRepo, sc *scanner.Scanner, cfg *config.Config) *Handler {
	return &Handler{
		repo:    repo,
		scanner: sc,
		cfg:     cfg,
	}
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

	// Views & Items
	handleAuth("GET /Users/{UserId}/Views", h.handleUserViews)
	handleAuth("GET /Users/{UserId}/Items", h.handleQueryItems)
	handleAuth("GET /Items", h.handleQueryItems)
	handleAuth("GET /Users/{UserId}/Items/Latest", h.handleLatestItems)
	handleAuth("GET /Users/{UserId}/Items/Resume", h.handleResumeItems)
	handleAuth("GET /Users/{UserId}/Items/{Id}", h.handleGetItem)
	handleAuth("GET /Items/{Id}", h.handleGetItem)
	handleAuth("GET /Items/Counts", h.handleItemCounts)

	// TV Shows
	handleAuth("GET /Shows/{Id}/Seasons", h.handleShowSeasons)
	handleAuth("GET /Shows/{Id}/Episodes", h.handleShowEpisodes)
	handleAuth("GET /Shows/NextUp", h.handleNextUp)

	// Search & Taxonomies
	handleAuth("GET /Search/Hints", h.handleSearchHints)
	handleAuth("GET /Genres", h.handleTaxonomies("genre"))
	handleAuth("GET /Studios", h.handleTaxonomies("studio"))
	handleAuth("GET /Persons", h.handleTaxonomies("person"))
	handleAuth("GET /Years", h.handleTaxonomies("year"))

	// UserData
	handleAuth("POST /Users/{UserId}/PlayedItems/{Id}", h.handleMarkPlayed)
	handleAuth("DELETE /Users/{UserId}/PlayedItems/{Id}", h.handleMarkUnplayed)
	handleAuth("POST /Users/{UserId}/FavoriteItems/{Id}", h.handleMarkFavorite)
	handleAuth("DELETE /Users/{UserId}/FavoriteItems/{Id}", h.handleUnmarkFavorite)

	// VirtualFolders & Library Refresh
	handleAuth("GET /Library/VirtualFolders", h.handleListVirtualFolders)
	handleAdmin("POST /Library/VirtualFolders", h.handleCreateVirtualFolder)
	handleAdmin("DELETE /Library/VirtualFolders", h.handleDeleteVirtualFolder)
	handleAdmin("POST /Library/Refresh", h.handleRefreshLibrary)
	handleAdmin("POST /Library/Media/Updated", h.handleMediaUpdated)
}

func (h *Handler) serverID() string {
	if h.cfg != nil {
		return h.cfg.ServerID
	}
	return ""
}

func (h *Handler) resolveUserID(r *http.Request, q httpx.Query) string {
	if uid := r.PathValue("UserId"); uid != "" {
		return guid.Normalize(uid)
	}
	if uid := q.Get("UserId"); uid != "" {
		return guid.Normalize(uid)
	}
	if h.auth.UserIDFromCtx != nil {
		return guid.Normalize(h.auth.UserIDFromCtx(r.Context()))
	}
	return ""
}

func (h *Handler) handleUserViews(w http.ResponseWriter, r *http.Request) {
	libs, err := h.repo.ListLibraries(r.Context())
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	items := make([]dto.BaseItemDto, 0, len(libs))
	for _, lib := range libs {
		items = append(items, dto.BaseItemDto{
			Id:                lib.ID,
			Name:              lib.Name,
			ServerId:          h.serverID(),
			Type:              "CollectionFolder",
			CollectionType:    lib.CollectionType,
			IsFolder:          true,
			ImageTags:         map[string]string{},
			BackdropImageTags: []string{},
		})
	}
	httpx.JSON(w, http.StatusOK, dto.QueryResult[dto.BaseItemDto]{
		Items:            items,
		TotalRecordCount: len(items),
	})
}

func (h *Handler) handleQueryItems(w http.ResponseWriter, r *http.Request) {
	q := httpx.ParseQuery(r)
	userID := h.resolveUserID(r, q)

	rec := false
	if b := q.Bool("Recursive"); b != nil {
		rec = *b
	}

	var years []int
	for _, ys := range q.List("Years") {
		if y, err := strconv.Atoi(ys); err == nil {
			years = append(years, y)
		}
	}

	isPlayed := q.Bool("IsPlayed")
	isUnplayed := q.Bool("IsUnplayed")
	isFav := q.Bool("IsFavorite")
	isResumable := q.Bool("IsResumable")
	for _, flt := range q.List("Filters") {
		t := true
		switch strings.ToLower(flt) {
		case "isplayed":
			isPlayed = &t
		case "isunplayed":
			isUnplayed = &t
		case "isfavorite":
			isFav = &t
		case "isresumable":
			isResumable = &t
		}
	}

	filter := sqlite.ItemFilter{
		ServerID:               h.serverID(),
		UserID:                 userID,
		ParentID:               q.Get("ParentId"),
		Recursive:              rec,
		IncludeItemTypes:       q.List("IncludeItemTypes"),
		ExcludeItemTypes:       q.List("ExcludeItemTypes"),
		Genres:                 q.List("Genres"),
		Studios:                q.List("Studios"),
		Tags:                   q.List("Tags"),
		PersonIDs:              q.List("PersonIds"),
		Persons:                q.List("Person"),
		Years:                  years,
		IsPlayed:               isPlayed,
		IsUnplayed:             isUnplayed,
		IsFavorite:             isFav,
		IsResumable:            isResumable,
		SearchTerm:             q.Get("SearchTerm"),
		SortBy:                 q.List("SortBy"),
		SortOrder:              q.Get("SortOrder"),
		StartIndex:             q.Int("StartIndex", 0),
		Limit:                  q.Int("Limit", 0),
		Fields:                 q.List("Fields"),
		EnableTotalRecordCount: q.Bool("EnableTotalRecordCount"),
	}

	items, total, err := h.repo.QueryItems(r.Context(), filter)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, dto.QueryResult[dto.BaseItemDto]{
		Items:            items,
		TotalRecordCount: total,
	})
}

func (h *Handler) handleLatestItems(w http.ResponseWriter, r *http.Request) {
	q := httpx.ParseQuery(r)
	userID := h.resolveUserID(r, q)
	items, err := h.repo.GetLatestItems(
		r.Context(),
		h.serverID(),
		userID,
		q.Get("ParentId"),
		q.List("IncludeItemTypes"),
		q.Int("Limit", 16),
		q.List("Fields"),
	)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	// Emby /Items/Latest returns a bare JSON array
	httpx.JSON(w, http.StatusOK, items)
}

func (h *Handler) handleResumeItems(w http.ResponseWriter, r *http.Request) {
	q := httpx.ParseQuery(r)
	userID := h.resolveUserID(r, q)
	items, total, err := h.repo.GetResumeItems(
		r.Context(),
		h.serverID(),
		userID,
		q.Get("ParentId"),
		q.List("IncludeItemTypes"),
		q.Int("StartIndex", 0),
		q.Int("Limit", 20),
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

func (h *Handler) handleGetItem(w http.ResponseWriter, r *http.Request) {
	q := httpx.ParseQuery(r)
	userID := h.resolveUserID(r, q)
	itemID := r.PathValue("Id")
	item, err := h.repo.GetItemByID(r.Context(), h.serverID(), userID, itemID)
	if errors.Is(err, sqlite.ErrNotFound) {
		httpx.Error(w, http.StatusNotFound, "item not found")
		return
	}
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, item)
}

func (h *Handler) handleItemCounts(w http.ResponseWriter, r *http.Request) {
	counts, err := h.repo.GetItemCounts(r.Context())
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, counts)
}

func (h *Handler) handleShowSeasons(w http.ResponseWriter, r *http.Request) {
	q := httpx.ParseQuery(r)
	seriesID := r.PathValue("Id")
	userID := h.resolveUserID(r, q)
	items, total, err := h.repo.QueryItems(r.Context(), sqlite.ItemFilter{
		ServerID:         h.serverID(),
		UserID:           userID,
		SeriesID:         seriesID,
		IncludeItemTypes: []string{"Season"},
		SortBy:           []string{"IndexNumber"},
		SortOrder:        "Ascending",
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

func (h *Handler) handleShowEpisodes(w http.ResponseWriter, r *http.Request) {
	q := httpx.ParseQuery(r)
	seriesID := r.PathValue("Id")
	userID := h.resolveUserID(r, q)
	items, total, err := h.repo.QueryItems(r.Context(), sqlite.ItemFilter{
		ServerID:         h.serverID(),
		UserID:           userID,
		SeriesID:         seriesID,
		SeasonID:         q.Get("SeasonId"),
		IncludeItemTypes: []string{"Episode"},
		SortBy:           []string{"IndexNumber"},
		SortOrder:        "Ascending",
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

func (h *Handler) handleNextUp(w http.ResponseWriter, r *http.Request) {
	q := httpx.ParseQuery(r)
	userID := h.resolveUserID(r, q)
	items, total, err := h.repo.GetNextUpEpisodes(
		r.Context(),
		h.serverID(),
		userID,
		q.Get("SeriesId"),
		q.Int("Limit", 20),
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

func (h *Handler) handleSearchHints(w http.ResponseWriter, r *http.Request) {
	q := httpx.ParseQuery(r)
	hints, total, err := h.repo.SearchHints(r.Context(), sqlite.SearchHintFilter{
		UserID:           h.resolveUserID(r, q),
		ParentID:         q.Get("ParentId"),
		SearchTerm:       q.Get("SearchTerm"),
		IncludeItemTypes: q.List("IncludeItemTypes"),
		Limit:            q.Int("Limit", 24),
	})
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, dto.SearchHintResult{
		SearchHints:      hints,
		TotalRecordCount: total,
	})
}

func (h *Handler) handleTaxonomies(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := httpx.ParseQuery(r)
		items, total, err := h.repo.ListTaxonomies(
			r.Context(),
			h.serverID(),
			kind,
			q.Get("ParentId"),
			q.Int("StartIndex", 0),
			q.Int("Limit", 200),
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
}

func (h *Handler) handleMarkPlayed(w http.ResponseWriter, r *http.Request) {
	ud, err := h.repo.MarkPlayed(r.Context(), r.PathValue("UserId"), r.PathValue("Id"))
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, ud)
}

func (h *Handler) handleMarkUnplayed(w http.ResponseWriter, r *http.Request) {
	ud, err := h.repo.MarkUnplayed(r.Context(), r.PathValue("UserId"), r.PathValue("Id"))
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, ud)
}

func (h *Handler) handleMarkFavorite(w http.ResponseWriter, r *http.Request) {
	ud, err := h.repo.MarkFavorite(r.Context(), r.PathValue("UserId"), r.PathValue("Id"))
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, ud)
}

func (h *Handler) handleUnmarkFavorite(w http.ResponseWriter, r *http.Request) {
	ud, err := h.repo.UnmarkFavorite(r.Context(), r.PathValue("UserId"), r.PathValue("Id"))
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, ud)
}

type virtualFolderDTO struct {
	Name           string   `json:"Name"`
	Locations      []string `json:"Locations"`
	CollectionType string   `json:"CollectionType"`
	ItemId         string   `json:"ItemId"`
}

func (h *Handler) handleListVirtualFolders(w http.ResponseWriter, r *http.Request) {
	libs, err := h.repo.ListLibraries(r.Context())
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]virtualFolderDTO, 0, len(libs))
	for _, lib := range libs {
		out = append(out, virtualFolderDTO{
			Name:           lib.Name,
			Locations:      lib.Paths,
			CollectionType: lib.CollectionType,
			ItemId:         lib.ID,
		})
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (h *Handler) handleCreateVirtualFolder(w http.ResponseWriter, r *http.Request) {
	q := httpx.ParseQuery(r)
	var body struct {
		Name           string   `json:"Name"`
		CollectionType string   `json:"CollectionType"`
		Paths          []string `json:"Paths"`
		LibraryOptions struct {
			PathInfos []struct {
				Path string `json:"Path"`
			} `json:"PathInfos"`
		} `json:"LibraryOptions"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)

	name := strings.TrimSpace(q.Get("name"))
	if name == "" {
		name = strings.TrimSpace(body.Name)
	}
	if name == "" {
		httpx.Error(w, http.StatusBadRequest, "library name is required")
		return
	}
	colType := strings.TrimSpace(q.Get("collectionType"))
	if colType == "" {
		colType = strings.TrimSpace(body.CollectionType)
	}
	paths := q.List("paths")
	if len(paths) == 0 {
		paths = body.Paths
	}
	if len(paths) == 0 {
		for _, pi := range body.LibraryOptions.PathInfos {
			if p := strings.TrimSpace(pi.Path); p != "" {
				paths = append(paths, p)
			}
		}
	}

	lib, err := h.repo.CreateLibrary(r.Context(), name, colType, paths)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if h.scanner != nil && q.Bool("refreshLibrary") != nil && *q.Bool("refreshLibrary") {
		h.scanner.ScanLibraryAsync(lib.ID)
	}
	httpx.JSON(w, http.StatusOK, virtualFolderDTO{
		Name:           lib.Name,
		Locations:      lib.Paths,
		CollectionType: lib.CollectionType,
		ItemId:         lib.ID,
	})
}

func (h *Handler) handleDeleteVirtualFolder(w http.ResponseWriter, r *http.Request) {
	q := httpx.ParseQuery(r)
	if id := q.Get("id"); id != "" {
		if err := h.repo.DeleteLibrary(r.Context(), id); err != nil {
			httpx.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		httpx.NoContent(w)
		return
	}
	if name := q.Get("name"); name != "" {
		if err := h.repo.DeleteLibraryByName(r.Context(), name); err != nil && !errors.Is(err, sqlite.ErrNotFound) {
			httpx.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		httpx.NoContent(w)
		return
	}
	httpx.Error(w, http.StatusBadRequest, "id or name is required")
}

func (h *Handler) handleRefreshLibrary(w http.ResponseWriter, r *http.Request) {
	q := httpx.ParseQuery(r)
	if h.scanner != nil {
		h.scanner.ScanLibraryAsync(q.Get("ParentId"))
	}
	httpx.NoContent(w)
}

func (h *Handler) handleMediaUpdated(w http.ResponseWriter, r *http.Request) {
	if h.scanner != nil {
		h.scanner.ScanLibraryAsync("")
	}
	httpx.NoContent(w)
}

func passthrough(next http.Handler) http.Handler { return next }
