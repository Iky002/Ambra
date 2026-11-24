package catalog

import (
	"context"
	"net/http"

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

	// Media category endpoints
	handleAuth("GET /Movies/Recommendations", h.handleMovieRecommendations)
	handleAuth("GET /Movies/{Id}/Similar", h.handleSimilar)
	handleAuth("GET /Trailers", h.handleItemsByType([]string{"Trailer"}))
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

func (h *Handler) handleMovieRecommendations(w http.ResponseWriter, r *http.Request) {
	q := httpx.ParseQuery(r)
	cats, err := h.repo.GetMovieRecommendations(
		r.Context(),
		h.serverID(),
		h.resolveUserID(r, q),
		q.Int("CategoryLimit", 5),
		q.Int("ItemLimit", 8),
		q.List("Fields"),
	)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, cats)
}

func (h *Handler) handleSimilar(w http.ResponseWriter, r *http.Request) {
	q := httpx.ParseQuery(r)
	items, total, err := h.repo.GetSimilarItems(
		r.Context(),
		h.serverID(),
		h.resolveUserID(r, q),
		r.PathValue("Id"),
		q.Int("Limit", 12),
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

func (h *Handler) handleInstantMix(w http.ResponseWriter, r *http.Request) {
	q := httpx.ParseQuery(r)
	items, total, err := h.repo.GetInstantMix(
		r.Context(),
		h.serverID(),
		h.resolveUserID(r, q),
		r.PathValue("Id"),
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

func (h *Handler) handleItemsByType(itemTypes []string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := httpx.ParseQuery(r)
		items, total, err := h.repo.QueryItems(r.Context(), sqlite.ItemFilter{
			ServerID:         h.serverID(),
			UserID:           h.resolveUserID(r, q),
			ParentID:         q.Get("ParentId"),
			Recursive:        true,
			IncludeItemTypes: itemTypes,
			Genres:           q.List("Genres"),
			Tags:             q.List("Tags"),
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
}

func (h *Handler) handleChannelItems(w http.ResponseWriter, r *http.Request) {
	q := httpx.ParseQuery(r)
	channelID := r.PathValue("Id")
	items, total, err := h.repo.QueryItems(r.Context(), sqlite.ItemFilter{
		ServerID:   h.serverID(),
		UserID:     h.resolveUserID(r, q),
		ParentID:   channelID,
		Recursive:  false,
		SortBy:     q.List("SortBy"),
		SortOrder:  q.Get("SortOrder"),
		StartIndex: q.Int("StartIndex", 0),
		Limit:      q.Int("Limit", 0),
		Fields:     q.List("Fields"),
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
		items, total, err := h.repo.ListFilterValues(
			r.Context(),
			h.serverID(),
			dimension,
			q.Get("ParentId"),
			q.List("IncludeItemTypes"),
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

func passthrough(next http.Handler) http.Handler { return next }
