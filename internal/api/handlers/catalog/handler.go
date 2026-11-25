package catalog

import (
	"context"
	"errors"
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
	if h.userRepo == nil {
		return nil
	}
	if ac, ok := middleware.AuthFromContext(r.Context()); ok && ac != nil && (ac.User != nil || ac.IsAPIKey) {
		if ac.IsAdmin() {
			return nil
		}
		return h.allowedLibrariesForUser(r.Context(), guid.Normalize(ac.UserID()))
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

func (h *Handler) handleItemsFilters(w http.ResponseWriter, r *http.Request) {
	q := httpx.ParseQuery(r)
	if targetUID := q.Get("UserId"); targetUID != "" && !h.canAccessUserData(r, targetUID) {
		httpx.Error(w, http.StatusForbidden, "forbidden")
		return
	}
	userID := h.resolveUserID(r, q)
	allowed := h.allowedLibrariesForRequest(r, userID)
	maxRating, blockedTags := h.userParentalPolicyForRequest(r, userID)

	res, err := h.repo.GetQueryFilters(r.Context(), sqlite.QueryFiltersFilter{
		ParentID:          q.Get("ParentId"),
		IncludeItemTypes:  q.List("IncludeItemTypes"),
		AllowedLibraryIDs: allowed,
		MaxParentalRating: maxRating,
		BlockedTags:       blockedTags,
	})
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
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
	allowed := h.allowedLibrariesForRequest(r, userID)
	maxRating, blockedTags := h.userParentalPolicyForRequest(r, userID)

	res, err := h.repo.GetQueryFilters(r.Context(), sqlite.QueryFiltersFilter{
		ParentID:          q.Get("ParentId"),
		IncludeItemTypes:  q.List("IncludeItemTypes"),
		AllowedLibraryIDs: allowed,
		MaxParentalRating: maxRating,
		BlockedTags:       blockedTags,
	})
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
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
	maxRating, blockedTags := h.userParentalPolicyForRequest(r, userID)
	categoryLimit := q.Int("CategoryLimit", 5)
	if categoryLimit <= 0 {
		categoryLimit = 5
	}
	if categoryLimit > 50 {
		categoryLimit = 50
	}
	itemLimit := q.Int("ItemLimit", 8)
	if itemLimit <= 0 {
		itemLimit = 8
	}
	if itemLimit > 100 {
		itemLimit = 100
	}
	cats, err := h.repo.GetMovieRecommendationsWithPolicy(
		r.Context(),
		h.serverID(),
		userID,
		categoryLimit,
		itemLimit,
		q.List("Fields"),
		allowed,
		maxRating,
		blockedTags,
	)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
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
	allowed := h.allowedLibrariesForRequest(r, fallbackUserID)
	maxRating, blockedTags := h.userParentalPolicyForRequest(r, fallbackUserID)
	if allowed == nil && maxRating == nil && len(blockedTags) == 0 {
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
	if allowed != nil && rec.LibraryID != "" && !isLibraryAllowed(rec.LibraryID, allowed) {
		return false, nil
	}
	if maxRating != nil || len(blockedTags) > 0 {
		blocked, bErr := h.repo.IsItemBlockedByParentalPolicy(r.Context(), rec, maxRating, blockedTags)
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
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !ok {
		httpx.Error(w, http.StatusForbidden, "access denied")
		return
	}
	allowed := h.allowedLibrariesForRequest(r, userID)
	maxRating, blockedTags := h.userParentalPolicyForRequest(r, userID)
	items, total, err := h.repo.GetSimilarItemsWithPolicy(
		r.Context(),
		h.serverID(),
		userID,
		seedID,
		q.Int("Limit", 12),
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
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !ok {
		httpx.Error(w, http.StatusForbidden, "access denied")
		return
	}
	allowed := h.allowedLibrariesForRequest(r, userID)
	maxRating, blockedTags := h.userParentalPolicyForRequest(r, userID)
	items, total, err := h.repo.GetInstantMixWithPolicy(
		r.Context(),
		h.serverID(),
		userID,
		seedID,
		q.Int("Limit", 50),
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

func (h *Handler) handleTrailers(w http.ResponseWriter, r *http.Request) {
	q := httpx.ParseQuery(r)
	if targetUID := q.Get("UserId"); targetUID != "" && !h.canAccessUserData(r, targetUID) {
		httpx.Error(w, http.StatusForbidden, "forbidden")
		return
	}
	userID := h.resolveUserID(r, q)
	allowed := h.allowedLibrariesForRequest(r, userID)
	maxRating, blockedTags := h.userParentalPolicyForRequest(r, userID)
	parentID := q.Get("ParentId")
	genres := q.List("Genres")
	tags := q.List("Tags")
	searchTerm := strings.TrimSpace(q.Get("SearchTerm"))
	lowerSearch := strings.ToLower(searchTerm)
	startIndex := q.Int("StartIndex", 0)
	if startIndex < 0 {
		startIndex = 0
	}
	limit := q.Int("Limit", 0)
	if limit > 500 {
		limit = 500
	}
	fields := q.List("Fields")

	// 1. Standalone Trailer items (type = 'Trailer', extra_type = '')
	standalone, _, err := h.repo.QueryItems(r.Context(), sqlite.ItemFilter{
		ServerID:          h.serverID(),
		UserID:            userID,
		ParentID:          parentID,
		AllowedLibraryIDs: allowed,
		MaxParentalRating: maxRating,
		BlockedTags:       blockedTags,
		Recursive:         true,
		IncludeItemTypes:  []string{"Trailer"},
		Genres:            genres,
		Tags:              tags,
		SearchTerm:        searchTerm,
		SortBy:            q.List("SortBy"),
		SortOrder:         q.Get("SortOrder"),
		Fields:            fields,
	})
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	seen := make(map[string]bool, len(standalone))
	merged := make([]dto.BaseItemDto, 0, len(standalone))
	for _, it := range standalone {
		seen[it.Id] = true
		merged = append(merged, it)
	}

	// 2. Local trailer extras (extra_type = 'trailer') attached to accessible parent items
	disableTotal := false
	parents, _, pErr := h.repo.QueryItems(r.Context(), sqlite.ItemFilter{
		ServerID:               h.serverID(),
		UserID:                 userID,
		ParentID:               parentID,
		AllowedLibraryIDs:      allowed,
		MaxParentalRating:      maxRating,
		BlockedTags:            blockedTags,
		Recursive:              true,
		IncludeItemTypes:       []string{"Movie", "Series", "MusicVideo", "Video"},
		Genres:                 genres,
		Tags:                   tags,
		Limit:                  1000,
		EnableTotalRecordCount: &disableTotal,
	})
	if pErr == nil {
		for _, parent := range parents {
			extras, exErr := h.repo.GetItemExtras(r.Context(), h.serverID(), userID, parent.Id, []string{"Trailer"})
			if exErr != nil {
				continue
			}
			for _, tr := range extras {
				if seen[tr.Id] {
					continue
				}
				if lowerSearch != "" &&
					!strings.Contains(strings.ToLower(tr.Name), lowerSearch) &&
					!strings.Contains(strings.ToLower(tr.OriginalTitle), lowerSearch) {
					continue
				}
				seen[tr.Id] = true
				merged = append(merged, tr)
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
		maxRating, blockedTags := h.userParentalPolicyForRequest(r, userID)
		items, total, err := h.repo.QueryItems(r.Context(), sqlite.ItemFilter{
			ServerID:          h.serverID(),
			UserID:            userID,
			ParentID:          q.Get("ParentId"),
			AllowedLibraryIDs: allowed,
			MaxParentalRating: maxRating,
			BlockedTags:       blockedTags,
			Recursive:         true,
			IncludeItemTypes:  itemTypes,
			Genres:            q.List("Genres"),
			Tags:              q.List("Tags"),
			SearchTerm:        q.Get("SearchTerm"),
			SortBy:            q.List("SortBy"),
			SortOrder:         q.Get("SortOrder"),
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
}

func (h *Handler) handleChannelItems(w http.ResponseWriter, r *http.Request) {
	q := httpx.ParseQuery(r)
	if targetUID := q.Get("UserId"); targetUID != "" && !h.canAccessUserData(r, targetUID) {
		httpx.Error(w, http.StatusForbidden, "forbidden")
		return
	}
	userID := h.resolveUserID(r, q)
	allowed := h.allowedLibrariesForRequest(r, userID)
	maxRating, blockedTags := h.userParentalPolicyForRequest(r, userID)
	channelID := r.PathValue("Id")
	items, total, err := h.repo.QueryItems(r.Context(), sqlite.ItemFilter{
		ServerID:          h.serverID(),
		UserID:            userID,
		ParentID:          channelID,
		AllowedLibraryIDs: allowed,
		MaxParentalRating: maxRating,
		BlockedTags:       blockedTags,
		Recursive:         false,
		SortBy:            q.List("SortBy"),
		SortOrder:         q.Get("SortOrder"),
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

func (h *Handler) handleFilterDimension(dimension string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := httpx.ParseQuery(r)
		if targetUID := q.Get("UserId"); targetUID != "" && !h.canAccessUserData(r, targetUID) {
			httpx.Error(w, http.StatusForbidden, "forbidden")
			return
		}
		userID := h.resolveUserID(r, q)
		allowed := h.allowedLibrariesForRequest(r, userID)
		maxRating, blockedTags := h.userParentalPolicyForRequest(r, userID)
		items, total, err := h.repo.ListFilterValuesWithPolicy(
			r.Context(),
			h.serverID(),
			dimension,
			q.Get("ParentId"),
			q.List("IncludeItemTypes"),
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

func passthrough(next http.Handler) http.Handler { return next }
