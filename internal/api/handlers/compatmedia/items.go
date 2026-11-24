package compatmedia

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/ambra-media/ambra/internal/api/dto"
	"github.com/ambra-media/ambra/internal/api/httpx"
	"github.com/ambra-media/ambra/internal/core/playback"
	"github.com/ambra-media/ambra/internal/core/scanner"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/plugins"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

// --- Item Metadata Update & Editor ---

type itemUpdateRequest struct {
	Name              string            `json:"Name"`
	OriginalTitle     string            `json:"OriginalTitle"`
	SortName          string            `json:"SortName"`
	Overview          string            `json:"Overview"`
	Taglines          []string          `json:"Taglines"`
	OfficialRating    string            `json:"OfficialRating"`
	CommunityRating   *float64          `json:"CommunityRating"`
	CriticRating      *float64          `json:"CriticRating"`
	ProductionYear    *int              `json:"ProductionYear"`
	IndexNumber       *int              `json:"IndexNumber"`
	ParentIndexNumber *int              `json:"ParentIndexNumber"`
	PremiereDate      string            `json:"PremiereDate"`
	Genres            []string          `json:"Genres"`
	Tags              []string          `json:"Tags"`
	Studios           []dto.NameIdPair  `json:"Studios"`
	ProviderIds       map[string]string `json:"ProviderIds"`
}

func (h *Handler) handleUpdateItem(w http.ResponseWriter, r *http.Request) {
	itemID := guid.Normalize(chi.URLParam(r, "ItemId"))
	if itemID == "" {
		httpx.Error(w, http.StatusBadRequest, "item id is required")
		return
	}
	rec, err := h.repo.GetItemRecordByID(r.Context(), itemID)
	if errors.Is(err, sqlite.ErrNotFound) {
		httpx.Error(w, http.StatusNotFound, "item not found")
		return
	}
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	var req itemUpdateRequest
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}

	if s := strings.TrimSpace(req.Name); s != "" {
		rec.Name = s
		if req.SortName == "" {
			rec.SortName = strings.ToLower(s)
		}
	}
	if req.OriginalTitle != "" {
		rec.OriginalTitle = req.OriginalTitle
	}
	if req.SortName != "" {
		rec.SortName = req.SortName
	}
	if req.Overview != "" {
		rec.Extra.Overview = req.Overview
	}
	if req.Taglines != nil {
		rec.Extra.Taglines = req.Taglines
	}
	if req.OfficialRating != "" {
		rec.OfficialRating = req.OfficialRating
	}
	if req.CommunityRating != nil {
		rec.CommunityRating = req.CommunityRating
	}
	if req.CriticRating != nil {
		rec.CriticRating = req.CriticRating
	}
	if req.ProductionYear != nil {
		rec.ProductionYear = req.ProductionYear
	}
	if req.IndexNumber != nil {
		rec.IndexNumber = req.IndexNumber
	}
	if req.ParentIndexNumber != nil {
		rec.ParentIndexNumber = req.ParentIndexNumber
	}
	if req.PremiereDate != "" {
		if ts, err := parseEmbyDate(req.PremiereDate); err == nil {
			rec.PremiereDate = &ts
		}
	}
	if req.ProviderIds != nil {
		rec.Extra.ProviderIDs = req.ProviderIds
	}
	rec.DateModified = time.Now().Unix()
	rec.Etag = guid.New()[:16]

	if err := h.repo.UpsertItem(r.Context(), rec); err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if req.Genres != nil {
		_ = h.repo.ReplaceItemTaxonomies(r.Context(), rec.ID, "genre", req.Genres)
	}
	if req.Tags != nil {
		_ = h.repo.ReplaceItemTaxonomies(r.Context(), rec.ID, "tag", req.Tags)
	}
	if req.Studios != nil {
		names := make([]string, 0, len(req.Studios))
		for _, st := range req.Studios {
			if st.Name != "" {
				names = append(names, st.Name)
			}
		}
		_ = h.repo.ReplaceItemTaxonomies(r.Context(), rec.ID, "studio", names)
	}
	var peopleNames []string
	if people, err := h.repo.GetPeopleByItemID(r.Context(), rec.ID); err == nil {
		peopleNames = make([]string, 0, len(people))
		for _, p := range people {
			if p.Name != "" {
				peopleNames = append(peopleNames, p.Name)
			}
		}
	}
	titleRaw := strings.TrimSpace(rec.Name + " " + rec.OriginalTitle)
	titleTokens := scanner.TokenizeForFTS(rec.Name, rec.OriginalTitle, rec.Extra.SeriesName)
	peopleTokens := scanner.TokenizeForFTS(peopleNames...)
	_ = h.repo.UpsertSearchFTS(r.Context(), rec.ID, titleRaw, titleTokens, "", "", peopleTokens)
	httpx.NoContent(w)
}

func (h *Handler) handleMetadataEditor(w http.ResponseWriter, r *http.Request) {
	itemID := guid.Normalize(chi.URLParam(r, "ItemId"))
	var externalIDs []map[string]any
	if rec, err := h.repo.GetItemRecordByID(r.Context(), itemID); err == nil {
		externalIDs = buildExternalIDInfos(rec)
	} else {
		externalIDs = []map[string]any{}
	}
	httpx.JSON(w, http.StatusOK, map[string]any{
		"ParentalRatingOptions": []map[string]any{
			{"Name": "G", "Value": 1},
			{"Name": "PG", "Value": 5},
			{"Name": "PG-13", "Value": 7},
			{"Name": "R", "Value": 9},
			{"Name": "NC-17", "Value": 10},
		},
		"Countries": []map[string]any{
			{"Name": "United States", "DisplayName": "United States", "TwoLetterISORegionName": "US", "ThreeLetterISORegionName": "USA"},
			{"Name": "China", "DisplayName": "China", "TwoLetterISORegionName": "CN", "ThreeLetterISORegionName": "CHN"},
			{"Name": "Japan", "DisplayName": "Japan", "TwoLetterISORegionName": "JP", "ThreeLetterISORegionName": "JPN"},
		},
		"Cultures": []map[string]any{
			{"Name": "English", "DisplayName": "English", "TwoLetterISOLanguageName": "en", "ThreeLetterISOLanguageName": "eng"},
			{"Name": "Chinese", "DisplayName": "Chinese", "TwoLetterISOLanguageName": "zh", "ThreeLetterISOLanguageName": "zho"},
			{"Name": "Japanese", "DisplayName": "Japanese", "TwoLetterISOLanguageName": "ja", "ThreeLetterISOLanguageName": "jpn"},
		},
		"ExternalIdInfos": externalIDs,
	})
}

func (h *Handler) handleItemRefresh(w http.ResponseWriter, r *http.Request) {
	itemID := guid.Normalize(chi.URLParam(r, "Id"))
	q := httpx.ParseQuery(r)
	replaceMeta := strings.EqualFold(q.Get("MetadataRefreshMode"), "FullRefresh")
	if b := q.Bool("ReplaceAllMetadata"); b != nil && *b {
		replaceMeta = true
	}
	replaceImages := strings.EqualFold(q.Get("ImageRefreshMode"), "FullRefresh")
	if b := q.Bool("ReplaceAllImages"); b != nil && *b {
		replaceImages = true
	}

	if h.scanner != nil {
		refreshCtx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
		defer cancel()
		if err := h.scanner.RefreshItem(refreshCtx, itemID, replaceMeta, replaceImages); err != nil {
			if errors.Is(err, sqlite.ErrNotFound) {
				httpx.Error(w, http.StatusNotFound, "item not found")
				return
			}
			httpx.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	httpx.NoContent(w)
}

func (h *Handler) handleMetadataReset(w http.ResponseWriter, r *http.Request) {
	q := httpx.ParseQuery(r)
	ids := q.List("ItemIds")
	for _, id := range ids {
		if rec, err := h.repo.GetItemRecordByID(r.Context(), id); err == nil {
			rec.Extra.Overview = ""
			rec.Extra.Taglines = nil
			rec.DateModified = time.Now().Unix()
			_ = h.repo.UpsertItem(r.Context(), rec)
		}
	}
	httpx.NoContent(w)
}

// --- Delete & DeleteInfo ---

func (h *Handler) handleDeleteItem(w http.ResponseWriter, r *http.Request) {
	itemID := guid.Normalize(chi.URLParam(r, "Id"))
	if err := h.repo.DeleteItem(r.Context(), itemID); errors.Is(err, sqlite.ErrNotFound) {
		httpx.Error(w, http.StatusNotFound, "item not found")
		return
	} else if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.NoContent(w)
}

func (h *Handler) handleDeleteItemsBatch(w http.ResponseWriter, r *http.Request) {
	q := httpx.ParseQuery(r)
	ids := q.List("Ids")
	if len(ids) == 0 && r.Body != nil {
		var body struct {
			Ids []string `json:"Ids"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		ids = body.Ids
	}
	for _, id := range ids {
		_ = h.repo.DeleteItem(r.Context(), id)
	}
	httpx.NoContent(w)
}

func (h *Handler) handleItemDeleteInfo(w http.ResponseWriter, r *http.Request) {
	itemID := guid.Normalize(chi.URLParam(r, "Id"))
	paths := []string{}
	if rec, err := h.repo.GetItemRecordByID(r.Context(), itemID); err == nil && rec.Path != "" {
		paths = append(paths, rec.Path)
	}
	httpx.JSON(w, http.StatusOK, map[string]any{
		"Paths": paths,
	})
}

// --- Tags & Sharing/Access ---

func (h *Handler) handleItemTags(add bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		itemID := guid.Normalize(chi.URLParam(r, "Id"))
		if _, err := h.repo.GetItemRecordByID(r.Context(), itemID); errors.Is(err, sqlite.ErrNotFound) {
			httpx.Error(w, http.StatusNotFound, "item not found")
			return
		}
		var body struct {
			Tags []struct {
				Name string `json:"Name"`
			} `json:"Tags"`
		}
		if r.Body != nil {
			_ = json.NewDecoder(r.Body).Decode(&body)
		}
		existing := h.getItemTagNames(r, itemID)
		tagSet := make(map[string]bool, len(existing))
		for _, t := range existing {
			tagSet[t] = true
		}
		for _, t := range body.Tags {
			name := strings.TrimSpace(t.Name)
			if name == "" {
				continue
			}
			if add {
				tagSet[name] = true
			} else {
				delete(tagSet, name)
			}
		}
		updated := make([]string, 0, len(tagSet))
		for t := range tagSet {
			updated = append(updated, t)
		}
		_ = h.repo.ReplaceItemTaxonomies(r.Context(), itemID, "tag", updated)
		httpx.NoContent(w)
	}
}

func (h *Handler) getItemTagNames(r *http.Request, itemID string) []string {
	if h.store == nil {
		return nil
	}
	rows, err := h.store.Reader.QueryContext(r.Context(), `
		SELECT t.name FROM item_taxonomies it
		JOIN taxonomies t ON t.id = it.taxonomy_id
		WHERE it.item_id = ? AND t.kind = 'tag'`, itemID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err == nil {
			out = append(out, name)
		}
	}
	return out
}

func (h *Handler) handleItemVisibility(isPublic bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		itemID := guid.Normalize(chi.URLParam(r, "Id"))
		_ = h.setConfigJSON(r.Context(), "item_visibility_"+itemID, map[string]any{"IsPublic": isPublic})
		httpx.NoContent(w)
	}
}

func (h *Handler) handleItemsAccess(w http.ResponseWriter, r *http.Request) {
	var body map[string]any
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&body)
	}
	if len(body) > 0 {
		_ = h.setConfigJSON(r.Context(), "items_access_last", body)
	}
	httpx.NoContent(w)
}

func (h *Handler) handleSharedLeave(w http.ResponseWriter, r *http.Request) {
	httpx.NoContent(w)
}

// --- RemoteSearch ---

type remoteSearchQuery struct {
	SearchInfo struct {
		Name        string            `json:"Name"`
		Year        int               `json:"Year"`
		ProviderIds map[string]string `json:"ProviderIds"`
	} `json:"SearchInfo"`
	ItemId                  string `json:"ItemId"`
	SearchProviderName      string `json:"SearchProviderName"`
	IncludeDisabledProviders bool  `json:"IncludeDisabledProviders"`
}

type remoteSearchResult struct {
	Name                string            `json:"Name"`
	ProductionYear      *int              `json:"ProductionYear,omitempty"`
	PremiereDate        string            `json:"PremiereDate,omitempty"`
	ImageUrl            string            `json:"ImageUrl,omitempty"`
	SearchProviderName  string            `json:"SearchProviderName"`
	Overview            string            `json:"Overview,omitempty"`
	ProviderIds         map[string]string `json:"ProviderIds,omitempty"`
}

type candidateSearcher interface {
	SearchCandidates(ctx context.Context, itemType, query string, year int, lang string) ([]plugins.MetadataResult, error)
}

func (h *Handler) handleRemoteSearch(itemType string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req remoteSearchQuery
		if r.Body != nil {
			_ = json.NewDecoder(r.Body).Decode(&req)
		}
		results := make([]remoteSearchResult, 0)
		if h.registry != nil && (req.SearchInfo.Name != "" || len(req.SearchInfo.ProviderIds) > 0) {
			lang := "zh-CN"
			if h.cfg != nil && h.cfg.TMDBLanguage != "" {
				lang = h.cfg.TMDBLanguage
			}
			mq := plugins.MediaQuery{
				ItemType:    itemType,
				Title:       req.SearchInfo.Name,
				Year:        req.SearchInfo.Year,
				ProviderIDs: req.SearchInfo.ProviderIds,
				Language:    lang,
			}
			for _, p := range h.registry.MetadataProviders() {
				manifest := p.Manifest()
				if req.SearchProviderName != "" &&
					!strings.EqualFold(req.SearchProviderName, manifest.Name) &&
					!(strings.EqualFold(req.SearchProviderName, "TheMovieDb") && strings.Contains(strings.ToLower(manifest.ID), "tmdb")) {
					continue
				}
				if cs, ok := p.(candidateSearcher); ok && req.SearchInfo.Name != "" && len(req.SearchInfo.ProviderIds) == 0 {
					candidates, err := cs.SearchCandidates(r.Context(), itemType, req.SearchInfo.Name, req.SearchInfo.Year, lang)
					if err == nil && len(candidates) > 0 {
						for _, c := range candidates {
							if len(results) >= 10 {
								break
							}
							var yr *int
							if c.Year > 0 {
								y := c.Year
								yr = &y
							}
							results = append(results, remoteSearchResult{
								Name:               nonEmpty(c.Name, req.SearchInfo.Name),
								ProductionYear:     yr,
								PremiereDate:       c.PremiereDate,
								ImageUrl:           c.PosterURL,
								SearchProviderName: manifest.Name,
								Overview:           c.Overview,
								ProviderIds:        c.ProviderIDs,
							})
						}
						continue
					}
				}
				meta, err := p.FetchMetadata(r.Context(), mq)
				if err != nil || meta == nil {
					continue
				}
				var yr *int
				if meta.Year > 0 {
					y := meta.Year
					yr = &y
				}
				results = append(results, remoteSearchResult{
					Name:               nonEmpty(meta.Name, req.SearchInfo.Name),
					ProductionYear:     yr,
					PremiereDate:       meta.PremiereDate,
					ImageUrl:           meta.PosterURL,
					SearchProviderName: manifest.Name,
					Overview:           meta.Overview,
					ProviderIds:        meta.ProviderIDs,
				})
			}
		}
		httpx.JSON(w, http.StatusOK, results)
	}
}

func (h *Handler) handleRemoteSearchApply(w http.ResponseWriter, r *http.Request) {
	itemID := guid.Normalize(chi.URLParam(r, "Id"))
	q := httpx.ParseQuery(r)
	replaceAllImages := true
	if b := q.Bool("ReplaceAllImages"); b != nil {
		replaceAllImages = *b
	}

	var res remoteSearchResult
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&res)
	}
	rec, err := h.repo.GetItemRecordByID(r.Context(), itemID)
	if errors.Is(err, sqlite.ErrNotFound) {
		httpx.Error(w, http.StatusNotFound, "item not found")
		return
	}
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	if res.Name != "" {
		rec.Name = res.Name
		rec.SortName = strings.ToLower(res.Name)
	}
	if res.Overview != "" {
		rec.Extra.Overview = res.Overview
	}
	if res.ProductionYear != nil {
		rec.ProductionYear = res.ProductionYear
	}
	if len(res.ProviderIds) > 0 {
		rec.Extra.ProviderIDs = res.ProviderIds
	}
	rec.DateModified = time.Now().Unix()
	rec.Etag = guid.New()[:16]
	if err := h.repo.UpsertItem(r.Context(), rec); err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	if h.scanner != nil {
		refreshCtx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
		defer cancel()
		_ = h.scanner.RefreshItem(refreshCtx, itemID, true, replaceAllImages)
	}
	httpx.NoContent(w)
}

// --- Item Relations & Details ---

func (h *Handler) handleExternalIDInfos(w http.ResponseWriter, r *http.Request) {
	itemID := guid.Normalize(chi.URLParam(r, "Id"))
	rec, err := h.repo.GetItemRecordByID(r.Context(), itemID)
	if err != nil {
		httpx.JSON(w, http.StatusOK, []map[string]any{})
		return
	}
	httpx.JSON(w, http.StatusOK, buildExternalIDInfos(rec))
}

func buildExternalIDInfos(rec *sqlite.ItemRecord) []map[string]any {
	out := []map[string]any{
		{"Name": "TheMovieDb", "Key": "Tmdb", "UrlFormatString": "https://www.themoviedb.org/movie/{0}"},
		{"Name": "IMDb", "Key": "Imdb", "UrlFormatString": "https://www.imdb.com/title/{0}"},
	}
	if rec != nil && strings.EqualFold(rec.Type, "Series") {
		out[0]["UrlFormatString"] = "https://www.themoviedb.org/tv/{0}"
		out = append(out, map[string]any{
			"Name":            "TheTVDB",
			"Key":             "Tvdb",
			"UrlFormatString": "https://thetvdb.com/?tab=series&id={0}",
		})
	}
	return out
}

func (h *Handler) handleItemAncestors(w http.ResponseWriter, r *http.Request) {
	itemID := guid.Normalize(chi.URLParam(r, "Id"))
	q := httpx.ParseQuery(r)
	userID := h.resolveUserID(r, q)

	ancestors := make([]dto.BaseItemDto, 0)
	if h.store != nil {
		rows, err := h.store.Reader.QueryContext(r.Context(),
			`SELECT ancestor_id FROM item_ancestors WHERE descendant_id = ? ORDER BY depth ASC`, itemID)
		if err == nil {
			var ids []string
			for rows.Next() {
				var aid string
				if err := rows.Scan(&aid); err == nil && aid != itemID {
					ids = append(ids, aid)
				}
			}
			rows.Close()
			for _, aid := range ids {
				if it, err := h.repo.GetItemByID(r.Context(), h.serverID(), userID, aid); err == nil && it != nil {
					ancestors = append(ancestors, *it)
				} else if lib, err := h.repo.GetLibrary(r.Context(), aid); err == nil && lib != nil {
					ancestors = append(ancestors, dto.BaseItemDto{
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
			}
		}
	}
	httpx.JSON(w, http.StatusOK, ancestors)
}

func (h *Handler) handleItemFile(w http.ResponseWriter, r *http.Request) {
	itemID := guid.Normalize(chi.URLParam(r, "Id"))
	rec, err := h.repo.GetItemRecordByID(r.Context(), itemID)
	if errors.Is(err, sqlite.ErrNotFound) || rec.Path == "" {
		httpx.Error(w, http.StatusNotFound, "item file not found")
		return
	}
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if _, err := os.Stat(rec.Path); err != nil {
		httpx.Error(w, http.StatusNotFound, "file not found on disk")
		return
	}
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename=%q`, filepath.Base(rec.Path)))
	http.ServeFile(w, r, rec.Path)
}

func (h *Handler) handleSimilar(w http.ResponseWriter, r *http.Request) {
	q := httpx.ParseQuery(r)
	items, total, err := h.repo.GetSimilarItems(
		r.Context(),
		h.serverID(),
		h.resolveUserID(r, q),
		chi.URLParam(r, "Id"),
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

func (h *Handler) handleItemInstantMix(w http.ResponseWriter, r *http.Request) {
	q := httpx.ParseQuery(r)
	items, total, err := h.repo.GetInstantMix(
		r.Context(),
		h.serverID(),
		h.resolveUserID(r, q),
		chi.URLParam(r, "Id"),
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

func (h *Handler) handleEmptyQueryResult(w http.ResponseWriter, _ *http.Request) {
	httpx.JSON(w, http.StatusOK, dto.QueryResult[dto.BaseItemDto]{
		Items:            []dto.BaseItemDto{},
		TotalRecordCount: 0,
	})
}

func (h *Handler) handleEmptyItemArray(w http.ResponseWriter, _ *http.Request) {
	httpx.JSON(w, http.StatusOK, []dto.BaseItemDto{})
}

func (h *Handler) handleThemeMediaResult(w http.ResponseWriter, r *http.Request) {
	httpx.JSON(w, http.StatusOK, map[string]any{
		"Items":            []dto.BaseItemDto{},
		"TotalRecordCount": 0,
		"OwnerId":          guid.Normalize(chi.URLParam(r, "Id")),
	})
}

func (h *Handler) handleAllThemeMedia(w http.ResponseWriter, r *http.Request) {
	ownerID := guid.Normalize(chi.URLParam(r, "Id"))
	empty := map[string]any{
		"Items":            []dto.BaseItemDto{},
		"TotalRecordCount": 0,
		"OwnerId":          ownerID,
	}
	httpx.JSON(w, http.StatusOK, map[string]any{
		"ThemeVideosResult": empty,
		"ThemeSongsResult":  empty,
		"SoundtrackSongsResult": empty,
	})
}

func (h *Handler) handleItemThumbnails(w http.ResponseWriter, _ *http.Request) {
	httpx.JSON(w, http.StatusOK, map[string]any{
		"AspectRatio": 1.777,
		"Thumbnails":  []any{},
	})
}

func (h *Handler) handleItemThumbnailSet(w http.ResponseWriter, _ *http.Request) {
	httpx.JSON(w, http.StatusOK, map[string]any{
		"AspectRatio": 1.777,
		"Thumbnails":  []any{},
	})
}

// --- Subtitles, Videos & Audio HLS ---

func (h *Handler) handleDeleteSubtitle(w http.ResponseWriter, r *http.Request) {
	itemID := guid.Normalize(chi.URLParam(r, "Id"))
	idx, err := strconv.Atoi(chi.URLParam(r, "Index"))
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid subtitle index")
		return
	}
	if h.store != nil {
		_, _ = h.store.Writer.ExecContext(r.Context(),
			`DELETE FROM item_media_streams WHERE item_id = ? AND stream_index = ? AND LOWER(stream_type) = 'subtitle'`,
			itemID, idx,
		)
	}
	httpx.NoContent(w)
}

func (h *Handler) handleRemoteSearchSubtitles(w http.ResponseWriter, _ *http.Request) {
	httpx.JSON(w, http.StatusOK, []any{})
}

func (h *Handler) handleDownloadRemoteSubtitle(w http.ResponseWriter, _ *http.Request) {
	httpx.NoContent(w)
}

func (h *Handler) handleProviderSubtitleDownload(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/vtt; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("WEBVTT\n\n"))
}

func (h *Handler) handleSubtitleStream(w http.ResponseWriter, r *http.Request) {
	itemID := guid.Normalize(chi.URLParam(r, "Id"))
	mediaSourceID := guid.Normalize(chi.URLParam(r, "MediaSourceId"))
	idx, err := strconv.Atoi(chi.URLParam(r, "Index"))
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid subtitle index")
		return
	}
	streamFile := strings.ToLower(chi.URLParam(r, "StreamFile"))
	if !strings.HasPrefix(streamFile, "stream.") {
		httpx.Error(w, http.StatusNotFound, "invalid subtitle stream path")
		return
	}
	format := strings.TrimPrefix(streamFile, "stream.")

	var startTicks int64
	if rawTicks := chi.URLParam(r, "StartPositionTicks"); rawTicks != "" {
		startTicks, _ = strconv.ParseInt(rawTicks, 10, 64)
	} else {
		startTicks = httpx.ParseQuery(r).Int64("StartPositionTicks", 0)
	}

	if h.store == nil {
		httpx.Error(w, http.StatusNotFound, "subtitle stream not found")
		return
	}
	var (
		p          sql.NullString
		isExternal int
		codec      sql.NullString
		rowMSID    sql.NullString
	)
	err = h.store.Reader.QueryRowContext(r.Context(), `
		SELECT path, is_external, codec, media_source_id FROM item_media_streams
		WHERE item_id = ? AND stream_index = ? AND LOWER(stream_type) = 'subtitle'
		  AND (? = '' OR media_source_id = ?)
		LIMIT 1`, itemID, idx, mediaSourceID, mediaSourceID).Scan(&p, &isExternal, &codec, &rowMSID)
	if err != nil {
		httpx.Error(w, http.StatusNotFound, "subtitle stream not found")
		return
	}

	subPath := p.String
	effectiveMSID := mediaSourceID
	if effectiveMSID == "" && rowMSID.Valid {
		effectiveMSID = rowMSID.String
	}

	if isExternal == 0 && subPath == "" {
		if h.extractor == nil {
			httpx.Error(w, http.StatusNotFound, "subtitle stream not found")
			return
		}
		mediaPath := h.resolveMediaSourcePath(r.Context(), itemID, effectiveMSID)
		if mediaPath == "" {
			httpx.Error(w, http.StatusNotFound, "media file not found for subtitle extraction")
			return
		}
		extractExt := format
		if extractExt == "" {
			extractExt = codec.String
		}
		extractedPath, err := h.extractor.ExtractSubtitle(r.Context(), mediaPath, itemID, effectiveMSID, idx, extractExt)
		if err != nil {
			httpx.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		subPath = extractedPath
	}
	if subPath == "" {
		httpx.Error(w, http.StatusNotFound, "subtitle stream not found")
		return
	}

	ct, data, err := playback.ServeSubtitle(subPath, format, startTicks)
	if err != nil {
		if os.IsNotExist(err) {
			httpx.Error(w, http.StatusNotFound, "subtitle file missing")
			return
		}
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.Header().Set("Cache-Control", "public, max-age=3600")
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = w.Write(data)
	}
}

func subtitleContentType(format string) string {
	switch strings.ToLower(format) {
	case "vtt", "webvtt":
		return "text/vtt; charset=utf-8"
	case "srt", "subrip":
		return "application/x-subrip; charset=utf-8"
	case "ass", "ssa":
		return "text/x-ssa; charset=utf-8"
	default:
		return "text/plain; charset=utf-8"
	}
}

type videoAttachmentDTO struct {
	Index    int    `json:"Index"`
	FileName string `json:"FileName"`
	MimeType string `json:"MimeType"`
	Url      string `json:"Url"`
}

func fontContentType(filePath string) string {
	switch strings.ToLower(filepath.Ext(filePath)) {
	case ".ttf":
		return "font/ttf"
	case ".otf":
		return "font/otf"
	case ".woff2":
		return "font/woff2"
	case ".woff":
		return "font/woff"
	case ".ttc":
		return "font/collection"
	default:
		return "application/octet-stream"
	}
}

func (h *Handler) resolveMediaSourcePath(ctx context.Context, itemID, mediaSourceID string) string {
	if h.store != nil {
		var p sql.NullString
		err := h.store.Reader.QueryRowContext(ctx, `
			SELECT path FROM item_media_sources
			WHERE item_id = ? AND (? = '' OR id = ?)
			ORDER BY rowid ASC LIMIT 1`, itemID, mediaSourceID, mediaSourceID).Scan(&p)
		if err == nil && p.Valid && strings.TrimSpace(p.String) != "" {
			return p.String
		}
	}
	if h.repo != nil {
		if rec, err := h.repo.GetItemRecordByID(ctx, itemID); err == nil && rec != nil && strings.TrimSpace(rec.Path) != "" {
			return rec.Path
		}
	}
	return ""
}

func (h *Handler) handleListVideoAttachments(w http.ResponseWriter, r *http.Request) {
	itemID := guid.Normalize(chi.URLParam(r, "Id"))
	mediaSourceID := guid.Normalize(chi.URLParam(r, "MediaSourceId"))
	mediaPath := h.resolveMediaSourcePath(r.Context(), itemID, mediaSourceID)
	if mediaPath == "" || h.extractor == nil {
		httpx.JSON(w, http.StatusOK, []videoAttachmentDTO{})
		return
	}
	fonts, err := h.extractor.ExtractFonts(r.Context(), mediaPath, itemID, mediaSourceID)
	if err != nil || len(fonts) == 0 {
		httpx.JSON(w, http.StatusOK, []videoAttachmentDTO{})
		return
	}
	out := make([]videoAttachmentDTO, 0, len(fonts))
	for i, fontPath := range fonts {
		name := filepath.Base(fontPath)
		out = append(out, videoAttachmentDTO{
			Index:    i,
			FileName: name,
			MimeType: fontContentType(name),
			Url:      fmt.Sprintf("/Videos/%s/%s/Attachments/%d", itemID, mediaSourceID, i),
		})
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (h *Handler) handleVideoAttachmentStream(w http.ResponseWriter, r *http.Request) {
	itemID := guid.Normalize(chi.URLParam(r, "Id"))
	mediaSourceID := guid.Normalize(chi.URLParam(r, "MediaSourceId"))
	idx, err := strconv.Atoi(chi.URLParam(r, "Index"))
	if err != nil || idx < 0 {
		httpx.Error(w, http.StatusBadRequest, "invalid attachment index")
		return
	}
	mediaPath := h.resolveMediaSourcePath(r.Context(), itemID, mediaSourceID)
	if mediaPath == "" || h.extractor == nil {
		httpx.Error(w, http.StatusNotFound, "attachment stream not found")
		return
	}
	fonts, err := h.extractor.ExtractFonts(r.Context(), mediaPath, itemID, mediaSourceID)
	if err != nil || idx >= len(fonts) {
		httpx.Error(w, http.StatusNotFound, "attachment stream not found")
		return
	}
	fontPath := fonts[idx]
	data, err := os.ReadFile(fontPath)
	if err != nil {
		httpx.Error(w, http.StatusNotFound, "attachment file not found")
		return
	}
	sum := sha256.Sum256(data)
	etag := fmt.Sprintf(`"%x"`, sum[:8])

	w.Header().Set("Content-Type", fontContentType(fontPath))
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.Header().Set("ETag", etag)
	if inm := strings.TrimSpace(r.Header.Get("If-None-Match")); inm != "" && inm == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = w.Write(data)
	}
}

func (h *Handler) handleDeleteAlternateSources(w http.ResponseWriter, r *http.Request) {
	itemID := guid.Normalize(chi.URLParam(r, "Id"))
	_, _ = h.repo.SplitItemVersions(r.Context(), itemID)
	httpx.NoContent(w)
}

func (h *Handler) handleMergeVersions(w http.ResponseWriter, r *http.Request) {
	q := httpx.ParseQuery(r)
	if ids := q.List("Ids"); len(ids) >= 2 {
		_, _, _ = h.repo.MergeItemVersions(r.Context(), ids)
	}
	httpx.NoContent(w)
}

func (h *Handler) handleDeleteActiveEncodings(w http.ResponseWriter, r *http.Request) {
	q := httpx.ParseQuery(r)
	deviceID := q.Get("DeviceId")
	playSessionID := q.Get("PlaySessionId")
	if h.transcodeMgr != nil {
		h.transcodeMgr.StopByDevice(deviceID, playSessionID)
	}
	httpx.NoContent(w)
}

func (h *Handler) handleVideoHlsSegment(w http.ResponseWriter, r *http.Request) {
	itemID := guid.Normalize(chi.URLParam(r, "Id"))
	if _, err := h.repo.GetItemRecordByID(r.Context(), itemID); errors.Is(err, sqlite.ErrNotFound) {
		httpx.Error(w, http.StatusNotFound, "video item not found")
		return
	}
	httpx.Error(w, http.StatusNotFound, "hls segment not available")
}

func (h *Handler) handleAudioHlsSegment(w http.ResponseWriter, r *http.Request) {
	itemID := guid.Normalize(chi.URLParam(r, "Id"))
	if _, err := h.repo.GetItemRecordByID(r.Context(), itemID); errors.Is(err, sqlite.ErrNotFound) {
		httpx.Error(w, http.StatusNotFound, "audio item not found")
		return
	}
	httpx.Error(w, http.StatusNotFound, "audio hls segment not available")
}

func parseEmbyDate(raw string) (int64, error) {
	for _, layout := range []string{time.RFC3339, "2006-01-02T15:04:05", "2006-01-02"} {
		if t, err := time.Parse(layout, raw); err == nil {
			return t.UTC().Unix(), nil
		}
	}
	return 0, fmt.Errorf("unsupported date format: %s", raw)
}
