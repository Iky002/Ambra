package catalog

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
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

type Handler struct {
	repo     *sqlite.LibraryRepo
	userRepo *sqlite.UserRepo
	cfg      *config.Config
	auth     AuthFuncs
}

func New(repo *sqlite.LibraryRepo, cfg *config.Config) *Handler {
	return &Handler{repo: repo, cfg: cfg}
}

// SetUserRepo attaches a UserRepo for per-user library RBAC and parental control enforcement.
func (h *Handler) SetUserRepo(userRepo *sqlite.UserRepo) {
	h.userRepo = userRepo
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

	// Emby P0 filter endpoints
	handleAuth("GET /Items/Filters", h.handleItemsFilters)
	handleAuth("GET /Items/Filters2", h.handleItemsFilters2)

	// Media category endpoints
	handleAuth("GET /Movies/Recommendations", h.handleMovieRecommendations)
	handleAuth("GET /Movies/{Id}/Similar", h.handleSimilar)
	handleAuth("GET /Trailers", h.handleTrailers)
	handleAuth("GET /Albums/{Id}/Similar", h.handleSimilar)
	handleAuth("GET /Albums/{Id}/InstantMix", h.handleInstantMix)
	handleAuth("GET /Songs/{Id}/InstantMix", h.handleInstantMix)
	handleAuth("GET /AudioBooks", h.handleItemsByType([]string{"AudioBook"}))
	handleAuth("GET /Games", h.handleItemsByType([]string{"Game"}))
	handleAuth("GET /Channels", h.handleItemsByType([]string{"Channel"}))
	handleAuth("GET /Channels/{Id}/Items", h.handleChannelItems)

	// Dynamic filter dimension endpoints
	handleAuth("GET /MusicGenres", h.handleFilterDimension("MusicGenre"))
	handleAuth("GET /GameGenres", h.handleFilterDimension("GameGenre"))
	handleAuth("GET /Tags", h.handleFilterDimension("Tag"))
	handleAuth("GET /OfficialRatings", h.handleFilterDimension("OfficialRating"))
	handleAuth("GET /ItemTypes", h.handleFilterDimension("ItemType"))
	handleAuth("GET /Containers", h.handleFilterDimension("Container"))
	handleAuth("GET /AudioCodecs", h.handleFilterDimension("AudioCodec"))
	handleAuth("GET /AudioLayouts", h.handleFilterDimension("AudioLayout"))
	handleAuth("GET /VideoCodecs", h.handleFilterDimension("VideoCodec"))
	handleAuth("GET /SubtitleCodecs", h.handleFilterDimension("SubtitleCodec"))
	handleAuth("GET /StreamLanguages", h.handleFilterDimension("StreamLanguage"))
	handleAuth("GET /ExtendedVideoTypes", h.handleFilterDimension("ExtendedVideoType"))
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
		return guid.Normalize(h.auth.UserIDFromCtx(r.Context()))
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
	pol := u.Policy
	pol.IsAdministrator = false
	if h.repo != nil {
		return h.repo.AllowedLibraryIDsForPolicy(ctx, pol)
	}
	if pol.EnableAllFolders {
		return nil
	}
	allowed := make([]string, 0, len(pol.EnabledFolders))
	for _, fid := range pol.EnabledFolders {
		if norm := guid.Normalize(fid); norm != "" {
			allowed = append(allowed, norm)
		}
	}
	return allowed
}

func (h *Handler) userParentalPolicyForRequest(r *http.Request, fallbackUserID string) (*int, []string) {
	pol := h.userPolicyForRequest(r, fallbackUserID)
	if pol.IsAdministrator {
		return nil, nil
	}
	return pol.MaxParentalRating, pol.BlockedTags
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

func (h *Handler) isPlaylistPublic(ctx context.Context, playlistID string) bool {
	normID := guid.Normalize(playlistID)
	if normID == "" || h.userRepo == nil {
		return false
	}
	raw, err := h.userRepo.GetServerConfig(ctx, "item_visibility_"+normID)
	if err != nil || raw == "" {
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
	return true
}

type queryFiltersLegacyResponse struct {
	Genres          []string `json:"Genres"`
	Tags            []string `json:"Tags"`
	OfficialRatings []string `json:"OfficialRatings"`
	Years           []int    `json:"Years"`
}

type queryFilters2Response struct {
	Genres          []dto.NameIdPair `json:"Genres"`
	Tags            []string         `json:"Tags"`
	OfficialRatings []string         `json:"OfficialRatings"`
	Years           []int            `json:"Years"`
	Studios         []dto.NameIdPair `json:"Studios"`
}

func (h *Handler) buildQueryFiltersFilter(r *http.Request, q httpx.Query, userID string) sqlite.QueryFiltersFilter {
	allowed := h.allowedLibrariesForRequest(r, userID)
	userPol := h.userPolicyForRequest(r, userID)
	f := sqlite.QueryFiltersFilter{
		ParentID:          q.Get("ParentId"),
		IncludeItemTypes:  q.List("IncludeItemTypes"),
		AllowedLibraryIDs: allowed,
	}
	if !userPol.IsAdministrator {
		f.MaxParentalRating = userPol.MaxParentalRating
		f.BlockUnratedItems = userPol.BlockUnratedItems
		f.BlockedTags = userPol.BlockedTags
		f.AllowedTags = userPol.AllowedTags
	}
	return f
}

func (h *Handler) handleItemsFilters(w http.ResponseWriter, r *http.Request) {
	q := httpx.ParseQuery(r)
	if targetUID := q.Get("UserId"); targetUID != "" && !h.canAccessUserData(r, targetUID) {
		httpx.Error(w, http.StatusForbidden, "forbidden")
		return
	}
	userID := h.resolveUserID(r, q)

	res, err := h.repo.GetQueryFilters(r.Context(), h.buildQueryFiltersFilter(r, q, userID))
	if err != nil {
		slog.Error("internal error in catalog handler", "error", err)
		httpx.Error(w, http.StatusInternalServerError, "internal server error")
		return
	}
	genreNames := make([]string, 0, len(res.Genres))
	for _, g := range res.Genres {
		genreNames = append(genreNames, g.Name)
	}
	httpx.JSON(w, http.StatusOK, queryFiltersLegacyResponse{
		Genres:          genreNames,
		Tags:            res.Tags,
		OfficialRatings: res.OfficialRatings,
		Years:           res.Years,
	})
}

func (h *Handler) handleItemsFilters2(w http.ResponseWriter, r *http.Request) {
	q := httpx.ParseQuery(r)
	if targetUID := q.Get("UserId"); targetUID != "" && !h.canAccessUserData(r, targetUID) {
		httpx.Error(w, http.StatusForbidden, "forbidden")
		return
	}
	userID := h.resolveUserID(r, q)

	res, err := h.repo.GetQueryFilters(r.Context(), h.buildQueryFiltersFilter(r, q, userID))
	if err != nil {
		slog.Error("internal error in catalog handler", "error", err)
		httpx.Error(w, http.StatusInternalServerError, "internal server error")
		return
	}
	httpx.JSON(w, http.StatusOK, queryFilters2Response{
		Genres:          res.Genres,
		Tags:            res.Tags,
		OfficialRatings: res.OfficialRatings,
		Years:           res.Years,
		Studios:         res.Studios,
	})
}

func (h *Handler) handleMovieRecommendations(w http.ResponseWriter, r *http.Request) {
	q := httpx.ParseQuery(r)
	if targetUID := q.Get("UserId"); targetUID != "" && !h.canAccessUserData(r, targetUID) {
		httpx.Error(w, http.StatusForbidden, "forbidden")
		return
	}
	userID := h.resolveUserID(r, q)
	allowed := h.allowedLibrariesForRequest(r, userID)
	userPol := h.userPolicyForRequest(r, userID)
	categoryLimit := clampCatalogLimit(q.Int("CategoryLimit", 5), 5, 50)
	itemLimit := clampCatalogLimit(q.Int("ItemLimit", 8), 8, 100)
	cats, err := h.repo.GetMovieRecommendationsWithUserPolicy(
		r.Context(),
		h.serverID(),
		userID,
		categoryLimit,
		itemLimit,
		q.List("Fields"),
		allowed,
		userPol,
	)
	if err != nil {
		slog.Error("internal error in catalog handler", "error", err)
		httpx.Error(w, http.StatusInternalServerError, "internal server error")
		return
	}
	if h.isNonAdminCaller(r) {
		for i := range cats {
			dto.SanitizeItemPathsSlice(cats[i].Items)
		}
	}
	httpx.JSON(w, http.StatusOK, cats)
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

func (h *Handler) canAccessItem(r *http.Request, fallbackUserID, itemID string) (bool, error) {
	if h.repo == nil {
		return true, nil
	}
	allowed := h.allowedLibrariesForRequest(r, fallbackUserID)
	userPol := h.userPolicyForRequest(r, fallbackUserID)
	nonAdmin := h.isNonAdminCaller(r)
	if !nonAdmin && allowed == nil && userPol.IsAdministrator {
		return true, nil
	}
	rec, err := h.repo.GetItemRecordByID(r.Context(), itemID)
	if errors.Is(err, sqlite.ErrNotFound) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	if rec == nil {
		return true, nil
	}
	if strings.EqualFold(rec.Type, "Playlist") {
		if !h.canViewPlaylist(r, rec.ID, rec.ParentID) {
			return false, nil
		}
	} else if allowed != nil && rec.LibraryID != "" && rec.LibraryID != "collections" && !isLibraryAllowed(rec.LibraryID, allowed) {
		return false, nil
	}
	if !userPol.IsAdministrator {
		blocked, bErr := h.repo.IsItemBlockedByPolicy(r.Context(), rec, userPol)
		if bErr != nil {
			return false, bErr
		}
		if blocked {
			return false, nil
		}
	}
	return true, nil
}

func (h *Handler) handleSimilar(w http.ResponseWriter, r *http.Request) {
	q := httpx.ParseQuery(r)
	if targetUID := q.Get("UserId"); targetUID != "" && !h.canAccessUserData(r, targetUID) {
		httpx.Error(w, http.StatusForbidden, "forbidden")
		return
	}
	userID := h.resolveUserID(r, q)
	seedID := r.PathValue("Id")
	ok, err := h.canAccessItem(r, userID, seedID)
	if err != nil {
		slog.Error("internal error in catalog handler", "error", err)
		httpx.Error(w, http.StatusInternalServerError, "internal server error")
		return
	}
	if !ok {
		httpx.Error(w, http.StatusForbidden, "access denied")
		return
	}
	allowed := h.allowedLibrariesForRequest(r, userID)
	userPol := h.userPolicyForRequest(r, userID)
	reqLimit := clampCatalogLimit(q.Int("Limit", 12), 12, maxCatalogQueryLimit)
	items, total, err := h.repo.GetSimilarItemsWithUserPolicy(
		r.Context(),
		h.serverID(),
		userID,
		seedID,
		reqLimit,
		q.List("Fields"),
		allowed,
		userPol,
	)
	if err != nil {
		slog.Error("internal error in catalog handler", "error", err)
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

func (h *Handler) handleInstantMix(w http.ResponseWriter, r *http.Request) {
	q := httpx.ParseQuery(r)
	if targetUID := q.Get("UserId"); targetUID != "" && !h.canAccessUserData(r, targetUID) {
		httpx.Error(w, http.StatusForbidden, "forbidden")
		return
	}
	userID := h.resolveUserID(r, q)
	seedID := r.PathValue("Id")
	ok, err := h.canAccessItem(r, userID, seedID)
	if err != nil {
		slog.Error("internal error in catalog handler", "error", err)
		httpx.Error(w, http.StatusInternalServerError, "internal server error")
		return
	}
	if !ok {
		httpx.Error(w, http.StatusForbidden, "access denied")
		return
	}
	allowed := h.allowedLibrariesForRequest(r, userID)
	userPol := h.userPolicyForRequest(r, userID)
	reqLimit := clampCatalogLimit(q.Int("Limit", 50), 50, maxCatalogQueryLimit)
	items, total, err := h.repo.GetInstantMixWithUserPolicy(
		r.Context(),
		h.serverID(),
		userID,
		seedID,
		reqLimit,
		q.List("Fields"),
		allowed,
		userPol,
	)
	if err != nil {
		slog.Error("internal error in catalog handler", "error", err)
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

func (h *Handler) handleTrailers(w http.ResponseWriter, r *http.Request) {
	q := httpx.ParseQuery(r)
	if targetUID := q.Get("UserId"); targetUID != "" && !h.canAccessUserData(r, targetUID) {
		httpx.Error(w, http.StatusForbidden, "forbidden")
		return
	}
	userID := h.resolveUserID(r, q)
	allowed := h.allowedLibrariesForRequest(r, userID)
	userPol := h.userPolicyForRequest(r, userID)
	parentID := q.Get("ParentId")
	genres := q.List("Genres")
	tags := q.List("Tags")
	searchTerm := strings.TrimSpace(q.Get("SearchTerm"))
	lowerSearch := strings.ToLower(searchTerm)
	startIndex := max(0, q.Int("StartIndex", 0))
	limit := q.Int("Limit", 0)
	if limit > 500 {
		limit = 500
	}
	fields := q.List("Fields")

	const (
		maxStandaloneTrailers = 500
		maxTrailerParentsScan = 200
		maxMergedTrailers     = 500
	)

	var maxRating *int
	var blockUnrated, blockedTags, allowedTags []string
	if !userPol.IsAdministrator {
		maxRating = userPol.MaxParentalRating
		blockUnrated = userPol.BlockUnratedItems
		blockedTags = userPol.BlockedTags
		allowedTags = userPol.AllowedTags
	}

	// 1. Standalone Trailer items (type = 'Trailer', extra_type = '')
	standalone, _, err := h.repo.QueryItems(r.Context(), sqlite.ItemFilter{
		ServerID:          h.serverID(),
		UserID:            userID,
		ParentID:          parentID,
		AllowedLibraryIDs: allowed,
		MaxParentalRating: maxRating,
		BlockUnratedItems: blockUnrated,
		BlockedTags:       blockedTags,
		AllowedTags:       allowedTags,
		Recursive:         true,
		IncludeItemTypes:  []string{"Trailer"},
		Genres:            genres,
		Tags:              tags,
		SearchTerm:        searchTerm,
		SortBy:            q.List("SortBy"),
		SortOrder:         q.Get("SortOrder"),
		Limit:             maxStandaloneTrailers,
		Fields:            fields,
	})
	if err != nil {
		slog.Error("failed to query standalone trailers", "error", err)
		httpx.Error(w, http.StatusInternalServerError, "failed to query trailers")
		return
	}

	seen := make(map[string]bool, len(standalone))
	merged := make([]dto.BaseItemDto, 0, len(standalone))
	for _, it := range standalone {
		seen[it.Id] = true
		merged = append(merged, it)
	}

	// 2. Local trailer extras (extra_type = 'trailer') attached to accessible parent items.
	var trailerParentIDs []string
	hasPreFilteredParents := false
	if h.userRepo != nil && h.userRepo.Store() != nil {
		if allowed != nil && len(allowed) == 0 {
			hasPreFilteredParents = true
		} else {
			var (
				where []string
				args  []any
			)
			where = append(where, "LOWER(i.extra_type) = 'trailer'", "i.parent_id != ''")
			if len(allowed) > 0 {
				placeholders := strings.TrimSuffix(strings.Repeat("?,", len(allowed)), ",")
				where = append(where, "i.library_id IN ("+placeholders+")")
				for _, lid := range allowed {
					args = append(args, lid)
				}
			}
			if normParent := guid.Normalize(parentID); normParent != "" {
				where = append(where, "(i.library_id = ? OR i.parent_id = ? OR EXISTS (SELECT 1 FROM item_ancestors ia WHERE ia.descendant_id = i.parent_id AND ia.ancestor_id = ?))")
				args = append(args, normParent, normParent, normParent)
			}
			hasPostFilter := !userPol.IsAdministrator || len(genres) > 0 || len(tags) > 0 || lowerSearch != ""
			querySQL := "SELECT DISTINCT i.parent_id FROM items i WHERE " + strings.Join(where, " AND ")
			if !hasPostFilter {
				querySQL += " LIMIT ?"
				args = append(args, maxTrailerParentsScan)
			}
			rows, qErr := h.userRepo.Store().Reader.QueryContext(r.Context(), querySQL, args...)
			if qErr == nil {
				hasPreFilteredParents = true
				for rows.Next() {
					var pid string
					if sErr := rows.Scan(&pid); sErr == nil && pid != "" {
						trailerParentIDs = append(trailerParentIDs, pid)
					}
				}
				rows.Close()
			}
		}
	}

	if !hasPreFilteredParents || len(trailerParentIDs) > 0 {
		disableTotal := false
		parentFilter := sqlite.ItemFilter{
			ServerID:               h.serverID(),
			UserID:                 userID,
			ParentID:               parentID,
			AllowedLibraryIDs:      allowed,
			MaxParentalRating:      maxRating,
			BlockUnratedItems:      blockUnrated,
			BlockedTags:            blockedTags,
			AllowedTags:            allowedTags,
			Recursive:              true,
			IncludeItemTypes:       []string{"Movie", "Series", "MusicVideo", "Video"},
			Genres:                 genres,
			Tags:                   tags,
			Limit:                  maxTrailerParentsScan,
			EnableTotalRecordCount: &disableTotal,
		}
		if hasPreFilteredParents {
			parentFilter.IDs = trailerParentIDs
		}
		parents, _, pErr := h.repo.QueryItems(r.Context(), parentFilter)
		if pErr == nil {
			for _, parent := range parents {
				if len(merged) >= maxMergedTrailers {
					break
				}
				extras, exErr := h.repo.GetItemExtras(r.Context(), h.serverID(), userID, parent.Id, []string{"Trailer"})
				if exErr != nil {
					continue
				}
				for _, tr := range extras {
					if len(merged) >= maxMergedTrailers {
						break
					}
					if seen[tr.Id] {
						continue
					}
					if lowerSearch != "" &&
						!strings.Contains(strings.ToLower(tr.Name), lowerSearch) &&
						!strings.Contains(strings.ToLower(tr.OriginalTitle), lowerSearch) {
						continue
					}
					if !userPol.IsAdministrator {
						trRec, rErr := h.repo.GetItemRecordByID(r.Context(), tr.Id)
						if rErr != nil || trRec == nil {
							continue
						}
						if blocked, bErr := h.repo.IsItemBlockedByPolicy(r.Context(), trRec, userPol); bErr != nil || blocked {
							continue
						}
					}
					seen[tr.Id] = true
					merged = append(merged, tr)
				}
			}
		}
	}

	total := len(merged)
	if startIndex >= total {
		merged = []dto.BaseItemDto{}
	} else {
		end := total
		if limit > 0 && startIndex+limit < end {
			end = startIndex + limit
		}
		merged = merged[startIndex:end]
	}

	if h.isNonAdminCaller(r) {
		dto.SanitizeItemPathsSlice(merged)
	}
	httpx.JSON(w, http.StatusOK, dto.QueryResult[dto.BaseItemDto]{
		Items:            merged,
		TotalRecordCount: total,
	})
}

func (h *Handler) handleItemsByType(itemTypes []string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := httpx.ParseQuery(r)
		if targetUID := q.Get("UserId"); targetUID != "" && !h.canAccessUserData(r, targetUID) {
			httpx.Error(w, http.StatusForbidden, "forbidden")
			return
		}
		userID := h.resolveUserID(r, q)
		allowed := h.allowedLibrariesForRequest(r, userID)
		userPol := h.userPolicyForRequest(r, userID)
		reqStart := max(0, q.Int("StartIndex", 0))
		reqLimit := clampCatalogLimit(q.Int("Limit", 0), 0, maxCatalogQueryLimit)

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
			ParentID:          q.Get("ParentId"),
			AllowedLibraryIDs: allowed,
			MaxParentalRating: maxRating,
			BlockUnratedItems: blockUnrated,
			BlockedTags:       blockedTags,
			AllowedTags:       allowedTags,
			Recursive:         true,
			IncludeItemTypes:  itemTypes,
			Genres:            q.List("Genres"),
			Tags:              q.List("Tags"),
			SearchTerm:        q.Get("SearchTerm"),
			SortBy:            q.List("SortBy"),
			SortOrder:         q.Get("SortOrder"),
			StartIndex:        reqStart,
			Limit:             reqLimit,
			Fields:            q.List("Fields"),
		})
		if err != nil {
			slog.Error("internal error in catalog handler", "error", err)
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
}

func (h *Handler) handleChannelItems(w http.ResponseWriter, r *http.Request) {
	q := httpx.ParseQuery(r)
	if targetUID := q.Get("UserId"); targetUID != "" && !h.canAccessUserData(r, targetUID) {
		httpx.Error(w, http.StatusForbidden, "forbidden")
		return
	}
	userID := h.resolveUserID(r, q)
	channelID := guid.Normalize(r.PathValue("Id"))
	chRec, err := h.repo.GetItemRecordByID(r.Context(), channelID)
	if errors.Is(err, sqlite.ErrNotFound) {
		httpx.Error(w, http.StatusNotFound, "channel not found")
		return
	}
	if err != nil {
		slog.Error("internal error in catalog handler", "error", err)
		httpx.Error(w, http.StatusInternalServerError, "internal server error")
		return
	}
	if chRec == nil || !strings.EqualFold(chRec.Type, "Channel") {
		httpx.Error(w, http.StatusNotFound, "channel not found")
		return
	}
	ok, accessErr := h.canAccessItem(r, userID, chRec.ID)
	if accessErr != nil {
		slog.Error("failed to verify channel access", "channel_id", chRec.ID, "error", accessErr)
		httpx.Error(w, http.StatusInternalServerError, "failed to verify item access")
		return
	}
	if !ok {
		httpx.Error(w, http.StatusForbidden, "access denied")
		return
	}
	allowed := h.allowedLibrariesForRequest(r, userID)
	userPol := h.userPolicyForRequest(r, userID)
	reqStart := max(0, q.Int("StartIndex", 0))
	reqLimit := clampCatalogLimit(q.Int("Limit", 0), 0, maxCatalogQueryLimit)

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
		ParentID:          chRec.ID,
		AllowedLibraryIDs: allowed,
		MaxParentalRating: maxRating,
		BlockUnratedItems: blockUnrated,
		BlockedTags:       blockedTags,
		AllowedTags:       allowedTags,
		Recursive:         false,
		SortBy:            q.List("SortBy"),
		SortOrder:         q.Get("SortOrder"),
		StartIndex:        reqStart,
		Limit:             reqLimit,
		Fields:            q.List("Fields"),
	})
	if err != nil {
		slog.Error("internal error in catalog handler", "error", err)
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

func (h *Handler) handleFilterDimension(dimension string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := httpx.ParseQuery(r)
		if targetUID := q.Get("UserId"); targetUID != "" && !h.canAccessUserData(r, targetUID) {
			httpx.Error(w, http.StatusForbidden, "forbidden")
			return
		}
		userID := h.resolveUserID(r, q)
		allowed := h.allowedLibrariesForRequest(r, userID)
		userPol := h.userPolicyForRequest(r, userID)
		items, total, err := h.repo.ListFilterValuesWithUserPolicy(
			r.Context(),
			h.serverID(),
			dimension,
			q.Get("ParentId"),
			q.List("IncludeItemTypes"),
			max(0, q.Int("StartIndex", 0)),
			clampCatalogLimit(q.Int("Limit", 200), 200, maxCatalogQueryLimit),
			allowed,
			userPol,
		)
		if err != nil {
			slog.Error("internal error in catalog handler", "error", err)
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
}

func passthrough(next http.Handler) http.Handler { return next }
