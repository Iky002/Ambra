package library

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ambra-media/ambra/internal/api/dto"
	"github.com/ambra-media/ambra/internal/api/httpx"
	"github.com/ambra-media/ambra/internal/api/middleware"
	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/core/scanner"
	"github.com/ambra-media/ambra/internal/core/watcher"
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
	repo     *sqlite.LibraryRepo
	userRepo *sqlite.UserRepo
	scanner  *scanner.Scanner
	watcher  *watcher.Watcher
	cfg      *config.Config
	auth     AuthFuncs
}

func New(repo *sqlite.LibraryRepo, sc *scanner.Scanner, cfg *config.Config) *Handler {
	return &Handler{
		repo:    repo,
		scanner: sc,
		cfg:     cfg,
	}
}

// SetUserRepo attaches a UserRepo for per-user library access control (Policy.EnableAllFolders / EnabledFolders).
func (h *Handler) SetUserRepo(userRepo *sqlite.UserRepo) {
	h.userRepo = userRepo
}

// SetWatcher attaches a filesystem Watcher so library creation/deletion dynamically updates watches.
func (h *Handler) SetWatcher(w *watcher.Watcher) {
	h.watcher = w
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
	handleAdmin("GET /api/admin/scanner/progress", h.handleScannerProgress)
	handleAdmin("POST /api/admin/scanner/pause", h.handleScannerPause)
	handleAdmin("POST /api/admin/scanner/resume", h.handleScannerResume)
	handleAdmin("POST /api/admin/scanner/cancel", h.handleScannerCancel)
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

// allowedLibrariesForRequest always evaluates library permissions against the authenticated
// caller from context (preventing spoofed UserId query/path bypasses). Only when no caller
// is in context (e.g. unit tests without auth middleware) does it fall back to fallbackUserID.
func (h *Handler) allowedLibrariesForRequest(r *http.Request, fallbackUserID string) []string {
	uid := ""
	if h.auth.UserIDFromCtx != nil {
		uid = guid.Normalize(h.auth.UserIDFromCtx(r.Context()))
	}
	if uid == "" {
		uid = guid.Normalize(fallbackUserID)
	}
	return h.allowedLibrariesForUser(r.Context(), uid)
}

// allowedLibrariesForUser returns nil if the user has unrestricted access to all libraries,
// or a non-nil slice of normalized library IDs (possibly empty) if restricted.
func (h *Handler) allowedLibrariesForUser(ctx context.Context, userID string) []string {
	if h.userRepo == nil || userID == "" {
		return nil
	}
	u, err := h.userRepo.GetUserByID(ctx, userID)
	if err != nil || u == nil {
		return nil
	}
	if u.IsAdmin || u.Policy.IsAdministrator || u.Policy.EnableAllFolders {
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

// userParentalPolicyForRequest resolves parental control restrictions (MaxParentalRating, BlockedTags)
// for the authenticated caller, exempting administrators and API keys.
func (h *Handler) userParentalPolicyForRequest(r *http.Request, fallbackUserID string) (*int, []string) {
	if ac, ok := middleware.AuthFromContext(r.Context()); ok && ac != nil {
		if ac.IsAPIKey || ac.IsAdmin() {
			return nil, nil
		}
	}
	uid := ""
	if h.auth.UserIDFromCtx != nil {
		uid = guid.Normalize(h.auth.UserIDFromCtx(r.Context()))
	}
	if uid == "" {
		if ac, ok := middleware.AuthFromContext(r.Context()); ok && ac != nil {
			uid = guid.Normalize(ac.UserID())
		}
	}
	if uid == "" {
		uid = guid.Normalize(fallbackUserID)
	}
	if h.userRepo == nil || uid == "" {
		return nil, nil
	}
	u, err := h.userRepo.GetUserByID(r.Context(), uid)
	if err != nil || u == nil {
		return nil, nil
	}
	if u.IsAdmin || u.Policy.IsAdministrator {
		return nil, nil
	}
	return u.Policy.MaxParentalRating, u.Policy.BlockedTags
}

func (h *Handler) handleUserViews(w http.ResponseWriter, r *http.Request) {
	q := httpx.ParseQuery(r)
	userID := h.resolveUserID(r, q)
	allowed := h.allowedLibrariesForRequest(r, userID)

	libs, err := h.repo.ListLibraries(r.Context())
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	items := make([]dto.BaseItemDto, 0, len(libs))
	for _, lib := range libs {
		if !isLibraryAllowed(lib.ID, allowed) {
			continue
		}
		items = append(items, h.libraryToViewDto(r.Context(), lib))
	}
	httpx.JSON(w, http.StatusOK, dto.QueryResult[dto.BaseItemDto]{
		Items:            items,
		TotalRecordCount: len(items),
	})
}

func (h *Handler) libraryToViewDto(ctx context.Context, lib sqlite.LibraryRecord) dto.BaseItemDto {
	imageTags := map[string]string{}
	if img, err := h.repo.GetImageByItemAndType(ctx, lib.ID, "Primary", 0); err == nil && img != nil && img.Tag != "" {
		imageTags["Primary"] = img.Tag
	}
	if imageTags["Primary"] == "" {
		if children, _, err := h.repo.QueryItems(ctx, sqlite.ItemFilter{
			LibraryID:        lib.ID,
			IncludeItemTypes: []string{"Movie", "Series", "MusicAlbum", "Video"},
			Recursive:        true,
			Limit:            12,
		}); err == nil {
			for _, it := range children {
				if len(it.BackdropImageTags) > 0 && it.BackdropImageTags[0] != "" {
					imageTags["Primary"] = it.BackdropImageTags[0]
					break
				}
			}
			if imageTags["Primary"] == "" {
				for _, it := range children {
					if tag := it.ImageTags["Primary"]; tag != "" {
						imageTags["Primary"] = tag
						break
					}
				}
			}
		}
	}
	return dto.BaseItemDto{
		Id:                lib.ID,
		Name:              lib.Name,
		ServerId:          h.serverID(),
		Type:              "CollectionFolder",
		CollectionType:    lib.CollectionType,
		IsFolder:          true,
		ImageTags:         imageTags,
		BackdropImageTags: []string{},
	}
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

	var minCommunityRating *float64
	if rawMin := strings.TrimSpace(q.Get("MinCommunityRating")); rawMin != "" {
		if v, err := strconv.ParseFloat(rawMin, 64); err == nil {
			minCommunityRating = &v
		}
	}

	splitMulti := func(vals []string) []string {
		if len(vals) == 0 {
			return nil
		}
		var out []string
		for _, v := range vals {
			for _, part := range strings.Split(v, "|") {
				if p := strings.TrimSpace(part); p != "" {
					out = append(out, p)
				}
			}
		}
		return out
	}

	maxRating, blockedTags := h.userParentalPolicyForRequest(r, userID)
	filter := sqlite.ItemFilter{
		ServerID:                h.serverID(),
		UserID:                  userID,
		ParentID:                q.Get("ParentId"),
		AllowedLibraryIDs:       h.allowedLibrariesForRequest(r, userID),
		MaxParentalRating:       maxRating,
		BlockedTags:             blockedTags,
		Recursive:               rec,
		IncludeItemTypes:        q.List("IncludeItemTypes"),
		ExcludeItemTypes:        q.List("ExcludeItemTypes"),
		IDs:                     splitMulti(q.List("Ids")),
		ExcludeItemIDs:          splitMulti(q.List("ExcludeItemIds")),
		Genres:                  splitMulti(q.List("Genres")),
		GenreIDs:                splitMulti(q.List("GenreIds")),
		Studios:                 splitMulti(q.List("Studios")),
		StudioIDs:               splitMulti(q.List("StudioIds")),
		Tags:                    splitMulti(q.List("Tags")),
		OfficialRatings:         splitMulti(q.List("OfficialRatings")),
		MinCommunityRating:      minCommunityRating,
		PersonIDs:               q.List("PersonIds"),
		Persons:                 q.List("Person"),
		Years:                   years,
		IsHD:                    q.Bool("IsHD"),
		Is4K:                    q.Bool("Is4K"),
		HasSubtitles:            q.Bool("HasSubtitles"),
		IsPlayed:                isPlayed,
		IsUnplayed:              isUnplayed,
		IsFavorite:              isFav,
		IsResumable:             isResumable,
		SearchTerm:              q.Get("SearchTerm"),
		NameStartsWith:          q.Get("NameStartsWith"),
		NameStartsWithOrGreater: q.Get("NameStartsWithOrGreater"),
		NameLessThan:            q.Get("NameLessThan"),
		SortBy:                  q.List("SortBy"),
		SortOrder:               q.Get("SortOrder"),
		StartIndex:              q.Int("StartIndex", 0),
		Limit:                   q.Int("Limit", 0),
		Fields:                  q.List("Fields"),
		EnableTotalRecordCount:  q.Bool("EnableTotalRecordCount"),
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
	allowed := h.allowedLibrariesForRequest(r, userID)
	maxRating, blockedTags := h.userParentalPolicyForRequest(r, userID)
	items, err := h.repo.GetLatestItemsWithPolicy(
		r.Context(),
		h.serverID(),
		userID,
		q.Get("ParentId"),
		q.List("IncludeItemTypes"),
		q.Int("Limit", 16),
		q.List("Fields"),
		allowed,
		maxRating,
		blockedTags,
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
	allowed := h.allowedLibrariesForRequest(r, userID)
	maxRating, blockedTags := h.userParentalPolicyForRequest(r, userID)
	items, total, err := h.repo.GetResumeItemsWithPolicy(
		r.Context(),
		h.serverID(),
		userID,
		q.Get("ParentId"),
		q.List("IncludeItemTypes"),
		q.Int("StartIndex", 0),
		q.Int("Limit", 20),
		q.List("Fields"),
		allowed,
		maxRating,
		blockedTags,
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
	allowed := h.allowedLibrariesForRequest(r, userID)
	maxRating, blockedTags := h.userParentalPolicyForRequest(r, userID)
	item, err := h.repo.GetItemByID(r.Context(), h.serverID(), userID, itemID)
	if errors.Is(err, sqlite.ErrNotFound) {
		if lib, libErr := h.repo.GetLibrary(r.Context(), itemID); libErr == nil && lib != nil {
			if !isLibraryAllowed(lib.ID, allowed) {
				httpx.Error(w, http.StatusForbidden, "access denied")
				return
			}
			httpx.JSON(w, http.StatusOK, h.libraryToViewDto(r.Context(), *lib))
			return
		}
		httpx.Error(w, http.StatusNotFound, "item not found")
		return
	}
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if allowed != nil || maxRating != nil || len(blockedTags) > 0 {
		if rec, recErr := h.repo.GetItemRecordByID(r.Context(), itemID); recErr == nil && rec != nil {
			if allowed != nil && rec.LibraryID != "" && !isLibraryAllowed(rec.LibraryID, allowed) {
				httpx.Error(w, http.StatusForbidden, "access denied")
				return
			}
			if blocked, blockErr := h.repo.IsItemBlockedByParentalPolicy(r.Context(), rec, maxRating, blockedTags); blockErr != nil {
				httpx.Error(w, http.StatusInternalServerError, blockErr.Error())
				return
			} else if blocked {
				httpx.Error(w, http.StatusForbidden, "access denied")
				return
			}
		}
	}
	httpx.JSON(w, http.StatusOK, item)
}

func (h *Handler) handleItemCounts(w http.ResponseWriter, r *http.Request) {
	q := httpx.ParseQuery(r)
	userID := h.resolveUserID(r, q)
	allowed := h.allowedLibrariesForRequest(r, userID)
	counts, err := h.repo.GetItemCountsWithAllowedLibraries(r.Context(), allowed)
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
	allowed := h.allowedLibrariesForRequest(r, userID)
	maxRating, blockedTags := h.userParentalPolicyForRequest(r, userID)
	if allowed != nil {
		if rec, recErr := h.repo.GetItemRecordByID(r.Context(), seriesID); recErr == nil && rec != nil && rec.LibraryID != "" {
			if !isLibraryAllowed(rec.LibraryID, allowed) {
				httpx.Error(w, http.StatusForbidden, "access denied")
				return
			}
		}
	}
	items, total, err := h.repo.QueryItems(r.Context(), sqlite.ItemFilter{
		ServerID:          h.serverID(),
		UserID:            userID,
		SeriesID:          seriesID,
		AllowedLibraryIDs: allowed,
		MaxParentalRating: maxRating,
		BlockedTags:       blockedTags,
		IncludeItemTypes:  []string{"Season"},
		SortBy:            []string{"IndexNumber"},
		SortOrder:         "Ascending",
		Fields:            q.List("Fields"),
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
	allowed := h.allowedLibrariesForRequest(r, userID)
	maxRating, blockedTags := h.userParentalPolicyForRequest(r, userID)
	if allowed != nil {
		if rec, recErr := h.repo.GetItemRecordByID(r.Context(), seriesID); recErr == nil && rec != nil && rec.LibraryID != "" {
			if !isLibraryAllowed(rec.LibraryID, allowed) {
				httpx.Error(w, http.StatusForbidden, "access denied")
				return
			}
		}
	}
	items, total, err := h.repo.QueryItems(r.Context(), sqlite.ItemFilter{
		ServerID:          h.serverID(),
		UserID:            userID,
		SeriesID:          seriesID,
		SeasonID:          q.Get("SeasonId"),
		AllowedLibraryIDs: allowed,
		MaxParentalRating: maxRating,
		BlockedTags:       blockedTags,
		IncludeItemTypes:  []string{"Episode"},
		SortBy:            []string{"IndexNumber"},
		SortOrder:         "Ascending",
		StartIndex:        q.Int("StartIndex", 0),
		Limit:             q.Int("Limit", 0),
		Fields:            q.List("Fields"),
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
	maxRating, blockedTags := h.userParentalPolicyForRequest(r, userID)
	items, total, err := h.repo.GetNextUpEpisodesWithPolicy(
		r.Context(),
		h.serverID(),
		userID,
		q.Get("SeriesId"),
		q.Int("Limit", 20),
		q.List("Fields"),
		h.allowedLibrariesForRequest(r, userID),
		maxRating,
		blockedTags,
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
	userID := h.resolveUserID(r, q)
	maxRating, blockedTags := h.userParentalPolicyForRequest(r, userID)
	hints, total, err := h.repo.SearchHints(r.Context(), sqlite.SearchHintFilter{
		UserID:            userID,
		ParentID:          q.Get("ParentId"),
		AllowedLibraryIDs: h.allowedLibrariesForRequest(r, userID),
		MaxParentalRating: maxRating,
		BlockedTags:       blockedTags,
		SearchTerm:        q.Get("SearchTerm"),
		IncludeItemTypes:  q.List("IncludeItemTypes"),
		Limit:             q.Int("Limit", 24),
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
		userID := h.resolveUserID(r, q)
		allowed := h.allowedLibrariesForRequest(r, userID)
		maxRating, blockedTags := h.userParentalPolicyForRequest(r, userID)
		items, total, err := h.repo.ListTaxonomiesWithPolicy(
			r.Context(),
			h.serverID(),
			kind,
			q.Get("ParentId"),
			q.Int("StartIndex", 0),
			q.Int("Limit", 200),
			allowed,
			maxRating,
			blockedTags,
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

func (h *Handler) canModifyUser(r *http.Request, targetUserID string) bool {
	targetID := guid.Normalize(targetUserID)
	if ac, ok := middleware.AuthFromContext(r.Context()); ok && ac != nil && (ac.User != nil || ac.IsAPIKey) {
		return ac.IsAdmin() || (targetID != "" && guid.Normalize(ac.UserID()) == targetID)
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
	// Unit tests without auth middleware or UserIDFromCtx
	return true
}

func (h *Handler) handleMarkPlayed(w http.ResponseWriter, r *http.Request) {
	if !h.canModifyUser(r, r.PathValue("UserId")) {
		httpx.Error(w, http.StatusForbidden, "forbidden")
		return
	}
	ud, err := h.repo.MarkPlayed(r.Context(), r.PathValue("UserId"), r.PathValue("Id"))
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, ud)
}

func (h *Handler) handleMarkUnplayed(w http.ResponseWriter, r *http.Request) {
	if !h.canModifyUser(r, r.PathValue("UserId")) {
		httpx.Error(w, http.StatusForbidden, "forbidden")
		return
	}
	ud, err := h.repo.MarkUnplayed(r.Context(), r.PathValue("UserId"), r.PathValue("Id"))
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, ud)
}

func (h *Handler) handleMarkFavorite(w http.ResponseWriter, r *http.Request) {
	if !h.canModifyUser(r, r.PathValue("UserId")) {
		httpx.Error(w, http.StatusForbidden, "forbidden")
		return
	}
	ud, err := h.repo.MarkFavorite(r.Context(), r.PathValue("UserId"), r.PathValue("Id"))
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, ud)
}

func (h *Handler) handleUnmarkFavorite(w http.ResponseWriter, r *http.Request) {
	if !h.canModifyUser(r, r.PathValue("UserId")) {
		httpx.Error(w, http.StatusForbidden, "forbidden")
		return
	}
	ud, err := h.repo.UnmarkFavorite(r.Context(), r.PathValue("UserId"), r.PathValue("Id"))
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, ud)
}

type virtualFolderDTO struct {
	Name           string         `json:"Name"`
	Locations      []string       `json:"Locations"`
	CollectionType string         `json:"CollectionType"`
	ItemId         string         `json:"ItemId"`
	LibraryOptions map[string]any `json:"LibraryOptions,omitempty"`
}

func (h *Handler) handleListVirtualFolders(w http.ResponseWriter, r *http.Request) {
	q := httpx.ParseQuery(r)
	userID := h.resolveUserID(r, q)
	allowed := h.allowedLibrariesForRequest(r, userID)
	libs, err := h.repo.ListLibraries(r.Context())
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]virtualFolderDTO, 0, len(libs))
	for _, lib := range libs {
		if !isLibraryAllowed(lib.ID, allowed) {
			continue
		}
		out = append(out, virtualFolderDTO{
			Name:           lib.Name,
			Locations:      lib.Paths,
			CollectionType: lib.CollectionType,
			ItemId:         lib.ID,
			LibraryOptions: sqlite.ParseLibraryOptions(lib.OptionsJSON),
		})
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (h *Handler) handleCreateVirtualFolder(w http.ResponseWriter, r *http.Request) {
	q := httpx.ParseQuery(r)
	var body struct {
		Name           string         `json:"Name"`
		CollectionType string         `json:"CollectionType"`
		Paths          []string       `json:"Paths"`
		LibraryOptions map[string]any `json:"LibraryOptions"`
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
	if len(paths) == 0 && body.LibraryOptions != nil {
		if rawInfos, ok := body.LibraryOptions["PathInfos"].([]any); ok {
			for _, entry := range rawInfos {
				if m, ok := entry.(map[string]any); ok {
					if p, ok := m["Path"].(string); ok {
						if trimmed := strings.TrimSpace(p); trimmed != "" {
							paths = append(paths, trimmed)
						}
					}
				}
			}
		}
	}

	lib, err := h.repo.CreateLibrary(r.Context(), name, colType, paths)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if len(body.LibraryOptions) > 0 {
		optsToSave := make(map[string]any, len(body.LibraryOptions))
		for k, v := range body.LibraryOptions {
			if k == "PathInfos" {
				continue
			}
			optsToSave[k] = v
		}
		if len(optsToSave) > 0 {
			if updated, optErr := h.repo.UpdateLibraryOptions(r.Context(), lib.ID, optsToSave); optErr == nil && updated != nil {
				lib = updated
			}
		}
	}
	if h.watcher != nil {
		_ = h.watcher.WatchLibrary(lib.ID, lib.Paths)
	}
	if h.scanner != nil && q.Bool("refreshLibrary") != nil && *q.Bool("refreshLibrary") {
		h.scanner.ScanLibraryAsync(lib.ID)
	}
	httpx.JSON(w, http.StatusOK, virtualFolderDTO{
		Name:           lib.Name,
		Locations:      lib.Paths,
		CollectionType: lib.CollectionType,
		ItemId:         lib.ID,
		LibraryOptions: sqlite.ParseLibraryOptions(lib.OptionsJSON),
	})
}

func (h *Handler) handleDeleteVirtualFolder(w http.ResponseWriter, r *http.Request) {
	q := httpx.ParseQuery(r)
	if id := q.Get("id"); id != "" {
		if err := h.repo.DeleteLibrary(r.Context(), id); err != nil {
			httpx.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		if h.watcher != nil {
			_ = h.watcher.UnwatchLibrary(guid.Normalize(id))
		}
		httpx.NoContent(w)
		return
	}
	if name := q.Get("name"); name != "" {
		var watchID string
		if h.watcher != nil {
			if libs, err := h.repo.ListLibraries(r.Context()); err == nil {
				for _, lib := range libs {
					if lib.Name == name {
						watchID = lib.ID
						break
					}
				}
			}
		}
		if err := h.repo.DeleteLibraryByName(r.Context(), name); err != nil && !errors.Is(err, sqlite.ErrNotFound) {
			httpx.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		if h.watcher != nil && watchID != "" {
			_ = h.watcher.UnwatchLibrary(watchID)
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

func (h *Handler) writeScannerProgress(w http.ResponseWriter) {
	if h.scanner == nil {
		httpx.JSON(w, http.StatusOK, scanner.ScanProgressSnapshot{
			Running:   false,
			Phase:     "idle",
			UpdatedAt: time.Now(),
		})
		return
	}
	httpx.JSON(w, http.StatusOK, h.scanner.Progress())
}

func (h *Handler) handleScannerProgress(w http.ResponseWriter, r *http.Request) {
	h.writeScannerProgress(w)
}

func (h *Handler) handleScannerPause(w http.ResponseWriter, r *http.Request) {
	if h.scanner != nil {
		h.scanner.Pause()
	}
	h.writeScannerProgress(w)
}

func (h *Handler) handleScannerResume(w http.ResponseWriter, r *http.Request) {
	if h.scanner != nil {
		h.scanner.Resume()
	}
	h.writeScannerProgress(w)
}

func (h *Handler) handleScannerCancel(w http.ResponseWriter, r *http.Request) {
	if h.scanner != nil {
		h.scanner.CancelAll()
	}
	h.writeScannerProgress(w)
}

func passthrough(next http.Handler) http.Handler { return next }
