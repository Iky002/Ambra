package compatmedia

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/ambra-media/ambra/internal/api/dto"
	"github.com/ambra-media/ambra/internal/api/httpx"
	"github.com/ambra-media/ambra/internal/api/middleware"
	"github.com/ambra-media/ambra/internal/core/playback"
	"github.com/ambra-media/ambra/internal/core/scanner"
	"github.com/ambra-media/ambra/internal/core/transcode"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/pkg/netutil"
	"github.com/ambra-media/ambra/internal/plugins"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

// --- Item Metadata Update & Editor ---

type itemUpdateRequest struct {
	Name              string            `json:"Name"`
	OriginalTitle     *string           `json:"OriginalTitle"`
	SortName          string            `json:"SortName"`
	Overview          *string           `json:"Overview"`
	Taglines          []string          `json:"Taglines"`
	OfficialRating    *string           `json:"OfficialRating"`
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
	LockData          *bool             `json:"LockData"`
	LockedFields      []string          `json:"LockedFields"`
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
		slog.Error("internal error in compatmedia item handler", "error", err)
		httpx.Error(w, http.StatusInternalServerError, "internal server error")
		return
	}

	var req itemUpdateRequest
	if r.Body != nil {
		_ = httpx.DecodeJSONLimit(r, &req, 0)
	}

	if s := strings.TrimSpace(req.Name); s != "" {
		rec.Name = s
		if req.SortName == "" {
			rec.SortName = strings.ToLower(s)
		}
	}
	if req.OriginalTitle != nil {
		rec.OriginalTitle = *req.OriginalTitle
	}
	if req.SortName != "" {
		rec.SortName = req.SortName
	}
	if req.Overview != nil {
		rec.Extra.Overview = *req.Overview
	}
	if req.Taglines != nil {
		rec.Extra.Taglines = req.Taglines
	}
	if req.OfficialRating != nil {
		rec.OfficialRating = *req.OfficialRating
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
	if req.LockData != nil {
		rec.Extra.LockData = *req.LockData
	}
	if req.LockedFields != nil {
		rec.Extra.LockedFields = req.LockedFields
	}
	rec.DateModified = time.Now().Unix()
	rec.Etag = guid.New()[:16]

	if err := h.repo.UpsertItem(r.Context(), rec); err != nil {
		slog.Error("failed to update item", "item_id", rec.ID, "error", err)
		httpx.Error(w, http.StatusInternalServerError, "failed to update item")
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
	if h.scanner != nil {
		_ = h.scanner.SaveItemLocalNFO(r.Context(), rec.ID, true)
	}
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
			slog.Error("failed to refresh item", "item_id", itemID, "error", err)
			httpx.Error(w, http.StatusInternalServerError, "failed to refresh item")
			return
		}
	}
	httpx.NoContent(w)
}

func (h *Handler) handleMetadataReset(w http.ResponseWriter, r *http.Request) {
	q := httpx.ParseQuery(r)
	ids := q.List("ItemIds")
	if len(ids) == 0 {
		ids = q.List("Ids")
	}
	if r.Body != nil {
		var raw map[string]json.RawMessage
		if err := httpx.DecodeJSONLimit(r, &raw, 0); err == nil {
			for _, key := range []string{"ItemIds", "itemIds", "Ids", "ids"} {
				v, ok := raw[key]
				if !ok || len(v) == 0 {
					continue
				}
				var arr []string
				if err := json.Unmarshal(v, &arr); err == nil && len(arr) > 0 {
					ids = append(ids, arr...)
					break
				}
				var str string
				if err := json.Unmarshal(v, &str); err == nil && strings.TrimSpace(str) != "" {
					for _, p := range strings.Split(str, ",") {
						if p = strings.TrimSpace(p); p != "" {
							ids = append(ids, p)
						}
					}
					break
				}
			}
		}
	}
	seen := make(map[string]bool, len(ids))
	for _, rawID := range ids {
		id := guid.Normalize(rawID)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		if rec, err := h.repo.GetItemRecordByID(r.Context(), id); err == nil {
			rec.Extra.Overview = ""
			rec.Extra.Taglines = nil
			rec.Extra.LockedFields = nil
			rec.Extra.LockData = false
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
		slog.Error("failed to delete item", "item_id", itemID, "error", err)
		httpx.Error(w, http.StatusInternalServerError, "failed to delete item")
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
		_ = httpx.DecodeJSONLimit(r, &body, 0)
		ids = body.Ids
	}
	for _, id := range ids {
		_ = h.repo.DeleteItem(r.Context(), id)
	}
	httpx.NoContent(w)
}

func (h *Handler) handleItemDeleteInfo(w http.ResponseWriter, r *http.Request) {
	if h.isNonAdminCaller(r) {
		httpx.Error(w, http.StatusForbidden, "admin privileges required")
		return
	}
	itemID := guid.Normalize(chi.URLParam(r, "Id"))
	if !h.canAccessItemID(r, itemID) {
		httpx.Error(w, http.StatusForbidden, "access denied")
		return
	}
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
			_ = httpx.DecodeJSONLimit(r, &body, 0)
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
		rec, err := h.repo.GetItemRecordByID(r.Context(), itemID)
		if errors.Is(err, sqlite.ErrNotFound) {
			httpx.Error(w, http.StatusNotFound, "item not found")
			return
		}
		if err != nil {
			slog.Error("internal error in compatmedia item handler", "error", err)
			httpx.Error(w, http.StatusInternalServerError, "internal server error")
			return
		}
		callerID, isAdmin := h.callerIdentity(r)
		if callerID != "" || isAdmin {
			if !isAdmin {
				if strings.EqualFold(rec.Type, "Playlist") {
					ownerID := guid.Normalize(rec.ParentID)
					if ownerID == "" || ownerID != callerID {
						httpx.Error(w, http.StatusForbidden, "forbidden")
						return
					}
				} else {
					httpx.Error(w, http.StatusForbidden, "forbidden")
					return
				}
			}
		}
		_ = h.setConfigJSON(r.Context(), "item_visibility_"+itemID, map[string]any{"IsPublic": isPublic})
		httpx.NoContent(w)
	}
}

func (h *Handler) handleItemsAccess(w http.ResponseWriter, r *http.Request) {
	var body map[string]any
	if r.Body != nil {
		_ = httpx.DecodeJSONLimit(r, &body, 0)
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
			_ = httpx.DecodeJSONLimit(r, &req, 0)
		}
		results := make([]remoteSearchResult, 0)
		if h.registry != nil && (req.SearchInfo.Name != "" || len(req.SearchInfo.ProviderIds) > 0) {
			lang := "zh-CN"
			if h.cfg != nil {
				if cfgLang := h.cfg.Snapshot().TMDBLanguage; cfgLang != "" {
					lang = cfgLang
				}
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
		_ = httpx.DecodeJSONLimit(r, &res, 0)
	}
	rec, err := h.repo.GetItemRecordByID(r.Context(), itemID)
	if errors.Is(err, sqlite.ErrNotFound) {
		httpx.Error(w, http.StatusNotFound, "item not found")
		return
	}
	if err != nil {
		slog.Error("internal error in compatmedia item handler", "error", err)
		httpx.Error(w, http.StatusInternalServerError, "internal server error")
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
		slog.Error("failed to update item", "item_id", rec.ID, "error", err)
		httpx.Error(w, http.StatusInternalServerError, "failed to update item")
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
	if !h.canAccessItemID(r, itemID) {
		httpx.Error(w, http.StatusForbidden, "access denied")
		return
	}
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
	q := httpx.ParseQuery(r)
	if targetUID := q.Get("UserId"); targetUID != "" && !h.canAccessUserData(r, targetUID) {
		httpx.Error(w, http.StatusForbidden, "forbidden")
		return
	}
	itemID := guid.Normalize(chi.URLParam(r, "Id"))
	if !h.canAccessItemID(r, itemID) {
		httpx.Error(w, http.StatusForbidden, "access denied")
		return
	}
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
	h.sanitizeItemsPathsForCaller(r, ancestors)
	httpx.JSON(w, http.StatusOK, ancestors)
}

func (h *Handler) canAccessItem(r *http.Request, item *sqlite.ItemRecord) bool {
	if item == nil {
		return true
	}
	if strings.EqualFold(item.Type, "Playlist") {
		if !h.canViewPlaylist(r, item.ID, item.ParentID) {
			return false
		}
	} else {
		allowed := h.allowedLibrariesForRequest(r, "")
		if !isLibraryAllowed(item.LibraryID, allowed) {
			return false
		}
	}
	if h.repo != nil {
		maxRating, blockedTags := h.userParentalPolicyForRequest(r, "")
		if maxRating != nil || len(blockedTags) > 0 {
			if blocked, err := h.repo.IsItemBlockedByParentalPolicy(r.Context(), item, maxRating, blockedTags); err != nil || blocked {
				return false
			}
		}
	}
	return true
}

func (h *Handler) canAccessItemID(r *http.Request, itemID string) bool {
	if h.repo == nil || itemID == "" {
		return true
	}
	rec, err := h.repo.GetItemRecordByID(r.Context(), itemID)
	if errors.Is(err, sqlite.ErrNotFound) {
		return true
	}
	if err != nil || rec == nil {
		return false
	}
	return h.canAccessItem(r, rec)
}

func (h *Handler) callerIdentity(r *http.Request) (userID string, isAdmin bool) {
	if ac, ok := middleware.AuthFromContext(r.Context()); ok && ac != nil && (ac.User != nil || ac.IsAPIKey) {
		return guid.Normalize(ac.UserID()), ac.IsAdmin()
	}
	if h.auth.UserIDFromCtx != nil {
		uid := guid.Normalize(h.auth.UserIDFromCtx(r.Context()))
		if uid != "" {
			if h.userRepo != nil {
				if u, err := h.userRepo.GetUserByID(r.Context(), uid); err == nil && u != nil {
					return uid, u.IsAdmin || u.Policy.IsAdministrator
				}
			}
			return uid, false
		}
	}
	return "", false
}

func logAndWritePermissionDenied(w http.ResponseWriter, path string, err error) {
	slog.Error("permission denied reading media file",
		"path", path,
		"uid", os.Geteuid(),
		"gid", os.Getegid(),
		"error", err,
	)
	httpx.Error(w, http.StatusForbidden, "permission denied reading media file")
}

func sanitizeContentDispositionFilename(name string) string {
	name = strings.ReplaceAll(name, "\r", "")
	name = strings.ReplaceAll(name, "\n", "")
	name = strings.ReplaceAll(name, "\\", "\\\\")
	name = strings.ReplaceAll(name, `"`, `\"`)
	if name == "" {
		return "file"
	}
	return name
}

func (h *Handler) handleItemFile(w http.ResponseWriter, r *http.Request) {
	if !h.canDownloadContent(r) {
		httpx.Error(w, http.StatusForbidden, "content downloading is disabled")
		return
	}
	itemID := guid.Normalize(chi.URLParam(r, "Id"))
	rec, err := h.repo.GetItemRecordByID(r.Context(), itemID)
	if errors.Is(err, sqlite.ErrNotFound) || (err == nil && rec.Path == "") {
		httpx.Error(w, http.StatusNotFound, "item file not found")
		return
	}
	if err != nil {
		slog.Error("failed to load item for file download", "item_id", itemID, "error", err)
		httpx.Error(w, http.StatusInternalServerError, "failed to load item")
		return
	}
	if !h.canAccessItem(r, rec) {
		httpx.Error(w, http.StatusForbidden, "access denied")
		return
	}
	if rec.IsFolder {
		httpx.Error(w, http.StatusBadRequest, "cannot download folder")
		return
	}
	if rec.IsStrm || strings.EqualFold(filepath.Ext(rec.Path), ".strm") {
		httpx.Error(w, http.StatusForbidden, "downloading .strm files is not allowed")
		return
	}
	fi, err := os.Stat(rec.Path)
	if err != nil {
		if errors.Is(err, os.ErrPermission) {
			logAndWritePermissionDenied(w, rec.Path, err)
			return
		}
		httpx.Error(w, http.StatusNotFound, "file not found on disk")
		return
	}
	if fi.IsDir() {
		httpx.Error(w, http.StatusBadRequest, "cannot download directory")
		return
	}
	f, err := os.Open(rec.Path)
	if err != nil {
		if errors.Is(err, os.ErrPermission) {
			logAndWritePermissionDenied(w, rec.Path, err)
			return
		}
		if os.IsNotExist(err) {
			httpx.Error(w, http.StatusNotFound, "file not found on disk")
			return
		}
		slog.Error("failed to open item file", "path", rec.Path, "error", err)
		httpx.Error(w, http.StatusInternalServerError, "failed to open item file")
		return
	}
	defer f.Close()
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "sandbox")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, sanitizeContentDispositionFilename(filepath.Base(rec.Path))))
	http.ServeContent(w, r, fi.Name(), fi.ModTime(), f)
}

func (h *Handler) handleSimilar(w http.ResponseWriter, r *http.Request) {
	q := httpx.ParseQuery(r)
	if targetUID := q.Get("UserId"); targetUID != "" && !h.canAccessUserData(r, targetUID) {
		httpx.Error(w, http.StatusForbidden, "forbidden")
		return
	}
	seedID := guid.Normalize(chi.URLParam(r, "Id"))
	if !h.canAccessItemID(r, seedID) {
		httpx.Error(w, http.StatusForbidden, "access denied")
		return
	}
	userID := h.resolveUserID(r, q)
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
		slog.Error("internal error in compatmedia item handler", "error", err)
		httpx.Error(w, http.StatusInternalServerError, "internal server error")
		return
	}
	h.sanitizeItemsPathsForCaller(r, items)
	httpx.JSON(w, http.StatusOK, dto.QueryResult[dto.BaseItemDto]{
		Items:            items,
		TotalRecordCount: total,
	})
}

func (h *Handler) handleItemInstantMix(w http.ResponseWriter, r *http.Request) {
	q := httpx.ParseQuery(r)
	if targetUID := q.Get("UserId"); targetUID != "" && !h.canAccessUserData(r, targetUID) {
		httpx.Error(w, http.StatusForbidden, "forbidden")
		return
	}
	seedID := guid.Normalize(chi.URLParam(r, "Id"))
	if !h.canAccessItemID(r, seedID) {
		httpx.Error(w, http.StatusForbidden, "access denied")
		return
	}
	userID := h.resolveUserID(r, q)
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
		slog.Error("internal error in compatmedia item handler", "error", err)
		httpx.Error(w, http.StatusInternalServerError, "internal server error")
		return
	}
	h.sanitizeItemsPathsForCaller(r, items)
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

var specialFeatureExtraTypes = []string{
	"Clip", "Featurette", "BehindTheScenes", "DeletedScene", "Interview", "Scene", "Short", "Extra",
}

func (h *Handler) filterExtrasByParentalPolicy(ctx context.Context, items []dto.BaseItemDto, maxRating *int, blockedTags []string) []dto.BaseItemDto {
	if h.repo == nil || (maxRating == nil && len(blockedTags) == 0) || len(items) == 0 {
		return items
	}
	filtered := make([]dto.BaseItemDto, 0, len(items))
	for _, it := range items {
		rec, err := h.repo.GetItemRecordByID(ctx, it.Id)
		if err != nil || rec == nil {
			continue
		}
		if blocked, bErr := h.repo.IsItemBlockedByParentalPolicy(ctx, rec, maxRating, blockedTags); bErr != nil || blocked {
			continue
		}
		filtered = append(filtered, it)
	}
	return filtered
}

func (h *Handler) handleLocalTrailers(w http.ResponseWriter, r *http.Request) {
	q := httpx.ParseQuery(r)
	if targetUID := q.Get("UserId"); targetUID != "" && !h.canAccessUserData(r, targetUID) {
		httpx.Error(w, http.StatusForbidden, "forbidden")
		return
	}
	itemID := guid.Normalize(chi.URLParam(r, "Id"))
	if !h.canAccessItemID(r, itemID) {
		httpx.Error(w, http.StatusForbidden, "access denied")
		return
	}
	if h.repo == nil || itemID == "" {
		httpx.JSON(w, http.StatusOK, []dto.BaseItemDto{})
		return
	}
	userID := h.resolveUserID(r, q)
	items, err := h.repo.GetItemExtras(r.Context(), h.serverID(), userID, itemID, []string{"Trailer"})
	if err != nil {
		slog.Error("internal error in compatmedia item handler", "error", err)
		httpx.Error(w, http.StatusInternalServerError, "internal server error")
		return
	}
	maxRating, blockedTags := h.userParentalPolicyForRequest(r, userID)
	items = h.filterExtrasByParentalPolicy(r.Context(), items, maxRating, blockedTags)
	if items == nil {
		items = []dto.BaseItemDto{}
	}
	h.sanitizeItemsPathsForCaller(r, items)
	httpx.JSON(w, http.StatusOK, items)
}

func (h *Handler) handleSpecialFeatures(w http.ResponseWriter, r *http.Request) {
	q := httpx.ParseQuery(r)
	if targetUID := q.Get("UserId"); targetUID != "" && !h.canAccessUserData(r, targetUID) {
		httpx.Error(w, http.StatusForbidden, "forbidden")
		return
	}
	itemID := guid.Normalize(chi.URLParam(r, "Id"))
	if !h.canAccessItemID(r, itemID) {
		httpx.Error(w, http.StatusForbidden, "access denied")
		return
	}
	if h.repo == nil || itemID == "" {
		httpx.JSON(w, http.StatusOK, []dto.BaseItemDto{})
		return
	}
	userID := h.resolveUserID(r, q)
	items, err := h.repo.GetItemExtras(r.Context(), h.serverID(), userID, itemID, specialFeatureExtraTypes)
	if err != nil {
		slog.Error("internal error in compatmedia item handler", "error", err)
		httpx.Error(w, http.StatusInternalServerError, "internal server error")
		return
	}
	maxRating, blockedTags := h.userParentalPolicyForRequest(r, userID)
	items = h.filterExtrasByParentalPolicy(r.Context(), items, maxRating, blockedTags)
	if items == nil {
		items = []dto.BaseItemDto{}
	}
	h.sanitizeItemsPathsForCaller(r, items)
	httpx.JSON(w, http.StatusOK, items)
}

func (h *Handler) handleThemeSongs(w http.ResponseWriter, r *http.Request) {
	h.serveThemeMediaByTypes(w, r, []string{"ThemeSong"})
}

func (h *Handler) handleThemeVideos(w http.ResponseWriter, r *http.Request) {
	h.serveThemeMediaByTypes(w, r, []string{"ThemeVideo"})
}

func (h *Handler) serveThemeMediaByTypes(w http.ResponseWriter, r *http.Request, extraTypes []string) {
	q := httpx.ParseQuery(r)
	if targetUID := q.Get("UserId"); targetUID != "" && !h.canAccessUserData(r, targetUID) {
		httpx.Error(w, http.StatusForbidden, "forbidden")
		return
	}
	ownerID := guid.Normalize(chi.URLParam(r, "Id"))
	if !h.canAccessItemID(r, ownerID) {
		httpx.Error(w, http.StatusForbidden, "access denied")
		return
	}
	items := []dto.BaseItemDto{}
	if h.repo != nil && ownerID != "" {
		userID := h.resolveUserID(r, q)
		var err error
		items, err = h.repo.GetItemExtras(r.Context(), h.serverID(), userID, ownerID, extraTypes)
		if err != nil {
			slog.Error("internal error in compatmedia item handler", "error", err)
			httpx.Error(w, http.StatusInternalServerError, "internal server error")
			return
		}
		maxRating, blockedTags := h.userParentalPolicyForRequest(r, userID)
		items = h.filterExtrasByParentalPolicy(r.Context(), items, maxRating, blockedTags)
		if items == nil {
			items = []dto.BaseItemDto{}
		}
	}
	h.sanitizeItemsPathsForCaller(r, items)
	httpx.JSON(w, http.StatusOK, map[string]any{
		"Items":            items,
		"TotalRecordCount": len(items),
		"OwnerId":          ownerID,
	})
}

func (h *Handler) handleAllThemeMedia(w http.ResponseWriter, r *http.Request) {
	q := httpx.ParseQuery(r)
	if targetUID := q.Get("UserId"); targetUID != "" && !h.canAccessUserData(r, targetUID) {
		httpx.Error(w, http.StatusForbidden, "forbidden")
		return
	}
	ownerID := guid.Normalize(chi.URLParam(r, "Id"))
	if !h.canAccessItemID(r, ownerID) {
		httpx.Error(w, http.StatusForbidden, "access denied")
		return
	}
	themeSongs := []dto.BaseItemDto{}
	themeVideos := []dto.BaseItemDto{}
	if h.repo != nil && ownerID != "" {
		userID := h.resolveUserID(r, q)
		allThemes, err := h.repo.GetItemExtras(r.Context(), h.serverID(), userID, ownerID, []string{"ThemeSong", "ThemeVideo"})
		if err != nil {
			slog.Error("internal error in compatmedia item handler", "error", err)
			httpx.Error(w, http.StatusInternalServerError, "internal server error")
			return
		}
		maxRating, blockedTags := h.userParentalPolicyForRequest(r, userID)
		allThemes = h.filterExtrasByParentalPolicy(r.Context(), allThemes, maxRating, blockedTags)
		h.sanitizeItemsPathsForCaller(r, allThemes)
		for _, it := range allThemes {
			if strings.EqualFold(it.ExtraType, "ThemeSong") {
				themeSongs = append(themeSongs, it)
			} else if strings.EqualFold(it.ExtraType, "ThemeVideo") {
				themeVideos = append(themeVideos, it)
			}
		}
	}
	httpx.JSON(w, http.StatusOK, map[string]any{
		"ThemeVideosResult": map[string]any{
			"Items":            themeVideos,
			"TotalRecordCount": len(themeVideos),
			"OwnerId":          ownerID,
		},
		"ThemeSongsResult": map[string]any{
			"Items":            themeSongs,
			"TotalRecordCount": len(themeSongs),
			"OwnerId":          ownerID,
		},
		"SoundtrackSongsResult": map[string]any{
			"Items":            []dto.BaseItemDto{},
			"TotalRecordCount": 0,
			"OwnerId":          ownerID,
		},
	})
}

func (h *Handler) buildThumbnailSetPayload(r *http.Request) (map[string]any, bool) {
	itemID := guid.Normalize(chi.URLParam(r, "Id"))
	if itemID != "" && !h.canAccessItemID(r, itemID) {
		return nil, false
	}
	q := httpx.ParseQuery(r)
	mediaSourceID := guid.Normalize(q.Get("MediaSourceId"))
	width := transcode.ParseBIFWidthParam(q.Get("Width"))
	thumbs := []map[string]any{}
	aspectRatio := 1.777

	if h.repo != nil && itemID != "" {
		if sources, err := h.repo.GetMediaSourcesByItemID(r.Context(), itemID); err == nil && len(sources) > 0 {
			var selected *sqlite.MediaSourceRecord
			if mediaSourceID != "" {
				for i := range sources {
					if guid.Normalize(sources[i].ID) == mediaSourceID {
						selected = &sources[i]
						break
					}
				}
			}
			if selected == nil {
				selected = &sources[0]
			}
			for _, st := range selected.Streams {
				if strings.EqualFold(st.StreamType, "Video") && st.Width > 0 && st.Height > 0 {
					aspectRatio = float64(st.Width) / float64(st.Height)
					break
				}
			}
		}
	}

	if h.extractor != nil && itemID != "" {
		mediaPath := h.resolveMediaSourcePath(r.Context(), itemID, mediaSourceID)
		if bifPath := h.extractor.FindExistingBIF(mediaPath, itemID, width); bifPath != "" {
			if meta, err := transcode.ReadBIFMetadata(bifPath); err == nil && len(meta.FrameTimestampsMs) > 0 {
				thumbs = make([]map[string]any, 0, len(meta.FrameTimestampsMs))
				for _, ms := range meta.FrameTimestampsMs {
					thumbs = append(thumbs, map[string]any{
						"PositionTicks": int64(ms) * 10_000,
						"ImageTag":      fmt.Sprintf("bif-%d", width),
					})
				}
			}
		}
	}
	return map[string]any{
		"AspectRatio": aspectRatio,
		"Thumbnails":  thumbs,
	}, true
}

func (h *Handler) handleItemThumbnails(w http.ResponseWriter, r *http.Request) {
	payload, ok := h.buildThumbnailSetPayload(r)
	if !ok {
		httpx.Error(w, http.StatusForbidden, "access denied")
		return
	}
	httpx.JSON(w, http.StatusOK, payload)
}

func (h *Handler) handleItemThumbnailSet(w http.ResponseWriter, r *http.Request) {
	payload, ok := h.buildThumbnailSetPayload(r)
	if !ok {
		httpx.Error(w, http.StatusForbidden, "access denied")
		return
	}
	httpx.JSON(w, http.StatusOK, payload)
}

// --- Subtitles, Videos & Audio HLS ---

const maxSubtitleUploadBytes = 5 * 1024 * 1024 // 5MB

var allowedSubtitleFormats = map[string]bool{
	"srt": true,
	"ass": true,
	"ssa": true,
	"vtt": true,
	"sub": true,
}

func sanitizeSubtitleLang(raw string) string {
	raw = strings.ToLower(strings.TrimSpace(raw))
	var b strings.Builder
	for _, r := range raw {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		}
	}
	if b.Len() == 0 {
		return "und"
	}
	return b.String()
}

func resolvePathForContainment(p string) string {
	clean := filepath.Clean(strings.TrimSpace(p))
	if clean == "" || clean == "." {
		return ""
	}
	if resolved, err := filepath.EvalSymlinks(clean); err == nil {
		return filepath.Clean(resolved)
	}
	// If the target file does not exist yet, resolve its parent directory.
	dir := filepath.Dir(clean)
	if resolvedDir, err := filepath.EvalSymlinks(dir); err == nil {
		return filepath.Join(filepath.Clean(resolvedDir), filepath.Base(clean))
	}
	return clean
}

func (h *Handler) isPathWithinLibraryRoots(ctx context.Context, targetPath string) bool {
	if h.repo == nil {
		return false
	}
	target := resolvePathForContainment(targetPath)
	if target == "" {
		return false
	}
	libs, err := h.repo.ListLibraries(ctx)
	if err != nil {
		return false
	}
	for _, lib := range libs {
		for _, rootPath := range lib.Paths {
			root := resolvePathForContainment(rootPath)
			if root == "" {
				continue
			}
			rel, err := filepath.Rel(root, target)
			if err != nil {
				continue
			}
			if rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel) {
				return true
			}
		}
	}
	return false
}

func (h *Handler) handleUploadSubtitle(w http.ResponseWriter, r *http.Request) {
	itemID := guid.Normalize(chi.URLParam(r, "Id"))
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
		slog.Error("internal error in compatmedia item handler", "error", err)
		httpx.Error(w, http.StatusInternalServerError, "internal server error")
		return
	}
	if !h.canAccessItem(r, rec) {
		httpx.Error(w, http.StatusForbidden, "access denied")
		return
	}

	var req struct {
		Language string `json:"Language"`
		Format   string `json:"Format"`
		IsForced bool   `json:"IsForced"`
		Data     string `json:"Data"`
	}
	if r.Body == nil {
		httpx.Error(w, http.StatusBadRequest, "request body is required")
		return
	}
	// Limit JSON payload (~8MB accommodates 5MB base64-encoded subtitle + JSON envelope)
	if err := httpx.DecodeJSONLimit(r, &req, 8*1024*1024); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid json body")
		return
	}

	format := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(req.Format)), ".")
	if !allowedSubtitleFormats[format] {
		httpx.Error(w, http.StatusBadRequest, "unsupported subtitle format")
		return
	}

	rawData := strings.TrimSpace(req.Data)
	if rawData == "" {
		httpx.Error(w, http.StatusBadRequest, "subtitle data is required")
		return
	}
	decoded, err := base64.StdEncoding.DecodeString(rawData)
	if err != nil {
		decoded, err = base64.RawStdEncoding.DecodeString(rawData)
	}
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid base64 subtitle data")
		return
	}
	if len(decoded) == 0 {
		httpx.Error(w, http.StatusBadRequest, "subtitle data is empty")
		return
	}
	if len(decoded) > maxSubtitleUploadBytes {
		httpx.Error(w, http.StatusRequestEntityTooLarge, "subtitle exceeds 5MB limit")
		return
	}

	q := httpx.ParseQuery(r)
	requestedMSID := guid.Normalize(q.Get("MediaSourceId"))
	var (
		mediaSourceID string
		mediaPath     string
	)
	if requestedMSID != "" && h.store != nil {
		var p sql.NullString
		msErr := h.store.Reader.QueryRowContext(r.Context(),
			`SELECT id, path FROM item_media_sources WHERE item_id = ? AND id = ? LIMIT 1`,
			itemID, requestedMSID,
		).Scan(&mediaSourceID, &p)
		if errors.Is(msErr, sql.ErrNoRows) {
			if requestedMSID == itemID && strings.TrimSpace(rec.Path) != "" {
				mediaSourceID = itemID
				mediaPath = rec.Path
			} else {
				httpx.Error(w, http.StatusNotFound, "media source not found")
				return
			}
		} else if msErr != nil {
			slog.Error("failed to look up media source for subtitle upload", "item_id", itemID, "media_source_id", requestedMSID, "error", msErr)
			httpx.Error(w, http.StatusInternalServerError, "failed to load media source")
			return
		} else {
			mediaPath = strings.TrimSpace(p.String)
		}
	} else {
		mediaPath = h.resolveMediaSourcePath(r.Context(), itemID, requestedMSID)
	}
	if strings.TrimSpace(mediaPath) == "" {
		httpx.Error(w, http.StatusBadRequest, "item has no media file path")
		return
	}

	lang := sanitizeSubtitleLang(req.Language)
	videoDir := filepath.Dir(mediaPath)
	videoBase := strings.TrimSuffix(filepath.Base(mediaPath), filepath.Ext(mediaPath))
	destPath := filepath.Join(videoDir, fmt.Sprintf("%s.%s.%s", videoBase, lang, format))

	if !h.isPathWithinLibraryRoots(r.Context(), destPath) {
		httpx.Error(w, http.StatusForbidden, "target subtitle path is outside configured library roots")
		return
	}

	tmpPath := destPath + ".tmp." + guid.New()[:8]
	if err := os.WriteFile(tmpPath, decoded, 0o644); err != nil {
		slog.Error("failed to write uploaded subtitle", "path", tmpPath, "error", err)
		httpx.Error(w, http.StatusInternalServerError, "failed to save subtitle file")
		return
	}
	if err := os.Rename(tmpPath, destPath); err != nil {
		_ = os.Remove(tmpPath)
		slog.Error("failed to rename uploaded subtitle", "path", destPath, "error", err)
		httpx.Error(w, http.StatusInternalServerError, "failed to save subtitle file")
		return
	}

	if h.store != nil {
		if mediaSourceID == "" {
			_ = h.store.Reader.QueryRowContext(r.Context(),
				`SELECT id FROM item_media_sources WHERE item_id = ? ORDER BY rowid ASC LIMIT 1`,
				itemID,
			).Scan(&mediaSourceID)
		}
		if mediaSourceID == "" {
			mediaSourceID = itemID
		}

		cls := scanner.ClassifySubtitle(filepath.Base(destPath), format)
		streamLang := lang
		if cls.Language != "" && cls.Language != "und" {
			streamLang = cls.Language
		}
		isForcedInt := 0
		if req.IsForced || cls.IsForced {
			isForcedInt = 1
		}

		tx, txErr := h.store.Writer.BeginTx(r.Context(), nil)
		if txErr != nil {
			slog.Error("failed to begin subtitle metadata transaction", "item_id", itemID, "error", txErr)
			httpx.Error(w, http.StatusInternalServerError, "failed to save subtitle metadata")
			return
		}
		defer func() { _ = tx.Rollback() }()

		var existingIdx int
		qErr := tx.QueryRowContext(r.Context(), `
			SELECT stream_index FROM item_media_streams
			WHERE media_source_id = ? AND path = ? AND LOWER(stream_type) = 'subtitle' AND is_external = 1
			LIMIT 1`,
			mediaSourceID, destPath,
		).Scan(&existingIdx)

		if qErr == nil {
			if _, err := tx.ExecContext(r.Context(), `
				UPDATE item_media_streams
				SET codec = ?, language = ?, sub_variant = ?, display_title = ?, title = ?, is_forced = ?
				WHERE media_source_id = ? AND stream_index = ? AND path = ?`,
				format, streamLang, cls.SubVariant, cls.DisplayTitle, filepath.Base(destPath), isForcedInt,
				mediaSourceID, existingIdx, destPath,
			); err != nil {
				slog.Error("failed to update subtitle stream record", "item_id", itemID, "error", err)
				httpx.Error(w, http.StatusInternalServerError, "failed to save subtitle metadata")
				return
			}
		} else if errors.Is(qErr, sql.ErrNoRows) {
			var maxIdx int
			if err := tx.QueryRowContext(r.Context(),
				`SELECT COALESCE(MAX(stream_index), -1) FROM item_media_streams WHERE item_id = ?`,
				itemID,
			).Scan(&maxIdx); err != nil {
				slog.Error("failed to query max subtitle stream index", "item_id", itemID, "error", err)
				httpx.Error(w, http.StatusInternalServerError, "failed to save subtitle metadata")
				return
			}
			nextIdx := maxIdx + 1

			if _, err := tx.ExecContext(r.Context(), `
				INSERT INTO item_media_streams (
					media_source_id, item_id, stream_index, stream_type, codec, language, sub_variant,
					display_title, title, width, height, video_range, audio_layout, channels,
					sample_rate, bitrate, is_default, is_forced, is_external, path
				) VALUES (?, ?, ?, 'Subtitle', ?, ?, ?, ?, ?, 0, 0, NULL, NULL, 0, 0, 0, 0, ?, 1, ?)`,
				mediaSourceID, itemID, nextIdx, format, streamLang, cls.SubVariant,
				cls.DisplayTitle, filepath.Base(destPath), isForcedInt, destPath,
			); err != nil {
				slog.Error("failed to insert subtitle stream record", "item_id", itemID, "error", err)
				httpx.Error(w, http.StatusInternalServerError, "failed to save subtitle metadata")
				return
			}
		} else {
			slog.Error("failed to query existing subtitle stream", "item_id", itemID, "error", qErr)
			httpx.Error(w, http.StatusInternalServerError, "failed to load subtitle metadata")
			return
		}
		if err := tx.Commit(); err != nil {
			slog.Error("failed to commit subtitle stream record", "item_id", itemID, "error", err)
			httpx.Error(w, http.StatusInternalServerError, "failed to save subtitle metadata")
			return
		}
	}

	httpx.NoContent(w)
}

func (h *Handler) handleDeleteSubtitle(w http.ResponseWriter, r *http.Request) {
	itemID := guid.Normalize(chi.URLParam(r, "Id"))
	if !h.canAccessItemID(r, itemID) {
		httpx.Error(w, http.StatusForbidden, "access denied")
		return
	}
	idx, err := strconv.Atoi(chi.URLParam(r, "Index"))
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid subtitle index")
		return
	}
	if h.store == nil {
		httpx.Error(w, http.StatusNotFound, "subtitle stream not found")
		return
	}

	q := httpx.ParseQuery(r)
	mediaSourceID := guid.Normalize(q.Get("MediaSourceId"))

	var (
		subPath    sql.NullString
		isExternal int
	)
	err = h.store.Reader.QueryRowContext(r.Context(), `
		SELECT path, is_external FROM item_media_streams
		WHERE item_id = ? AND stream_index = ? AND LOWER(stream_type) = 'subtitle'
		  AND (? = '' OR media_source_id = ?)
		LIMIT 1`, itemID, idx, mediaSourceID, mediaSourceID,
	).Scan(&subPath, &isExternal)
	if errors.Is(err, sql.ErrNoRows) {
		httpx.Error(w, http.StatusNotFound, "subtitle stream not found")
		return
	}
	if err != nil {
		slog.Error("internal error in compatmedia item handler", "error", err)
		httpx.Error(w, http.StatusInternalServerError, "internal server error")
		return
	}
	if isExternal == 0 {
		httpx.Error(w, http.StatusBadRequest, "cannot delete embedded subtitle stream")
		return
	}

	cleanPath := strings.TrimSpace(subPath.String)
	if cleanPath != "" {
		if !h.isPathWithinLibraryRoots(r.Context(), cleanPath) {
			httpx.Error(w, http.StatusForbidden, "subtitle path outside configured library roots")
			return
		}
		if err := os.Remove(cleanPath); err != nil && !os.IsNotExist(err) {
			slog.Error("failed to delete subtitle file", "path", cleanPath, "error", err)
			httpx.Error(w, http.StatusInternalServerError, "failed to delete subtitle file")
			return
		}
	}

	if _, err := h.store.Writer.ExecContext(r.Context(), `
		DELETE FROM item_media_streams
		WHERE item_id = ? AND stream_index = ? AND LOWER(stream_type) = 'subtitle' AND is_external = 1
		  AND (? = '' OR media_source_id = ?)`,
		itemID, idx, mediaSourceID, mediaSourceID,
	); err != nil {
		slog.Error("failed to delete subtitle stream record", "item_id", itemID, "error", err)
		httpx.Error(w, http.StatusInternalServerError, "failed to delete subtitle metadata")
		return
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
	if !h.canAccessItemID(r, itemID) {
		httpx.Error(w, http.StatusForbidden, "access denied")
		return
	}
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
		mediaPath, err = h.resolveExtractionMediaPath(r.Context(), mediaPath)
		if err != nil {
			slog.Error("failed to resolve strm for subtitle extraction", "item_id", itemID, "error", err)
			httpx.Error(w, http.StatusBadGateway, "failed to resolve strm stream")
			return
		}
		extractExt := format
		if extractExt == "" {
			extractExt = codec.String
		}
		extractedPath, err := h.extractor.ExtractSubtitle(r.Context(), mediaPath, itemID, effectiveMSID, idx, extractExt)
		if err != nil {
			if errors.Is(err, os.ErrPermission) {
				logAndWritePermissionDenied(w, mediaPath, err)
				return
			}
			slog.Error("failed to extract subtitle", "item_id", itemID, "path", mediaPath, "error", err)
			httpx.Error(w, http.StatusInternalServerError, "failed to extract subtitle")
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
		if errors.Is(err, os.ErrPermission) {
			logAndWritePermissionDenied(w, subPath, err)
			return
		}
		if os.IsNotExist(err) {
			httpx.Error(w, http.StatusNotFound, "subtitle file missing")
			return
		}
		slog.Error("failed to serve subtitle", "path", subPath, "error", err)
		httpx.Error(w, http.StatusInternalServerError, "failed to serve subtitle")
		return
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.Header().Set("Cache-Control", "private, max-age=3600")
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

func isHTTPURL(s string) bool {
	lower := strings.ToLower(strings.TrimSpace(s))
	return strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://")
}

func readFirstLineStrmURL(strmPath string) (string, error) {
	f, err := os.Open(strmPath)
	if err != nil {
		return "", err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimPrefix(sc.Text(), "\uFEFF")
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") {
			candidate := strings.ReplaceAll(line, " ", "%20")
			if !isHTTPURL(candidate) {
				return "", errors.New("strm target must be an http/https url")
			}
			if _, parseErr := url.Parse(candidate); parseErr != nil {
				return "", parseErr
			}
			return candidate, nil
		}
	}
	if err := sc.Err(); err != nil {
		return "", err
	}
	return "", errors.New("empty strm file")
}

func (h *Handler) resolveExtractionMediaPath(ctx context.Context, mediaPath string) (string, error) {
	if strings.EqualFold(filepath.Ext(mediaPath), ".strm") {
		var (
			resolved string
			err      error
		)
		if h.streamResolver != nil {
			resolved, err = h.streamResolver(ctx, mediaPath)
		} else {
			resolved, err = readFirstLineStrmURL(mediaPath)
		}
		if err != nil {
			return "", err
		}
		resolved = strings.TrimSpace(resolved)
		if !isHTTPURL(resolved) {
			return "", errors.New("invalid strm target url")
		}
		if err := netutil.ValidateRemoteURL(resolved, true); err != nil {
			return "", err
		}
		return resolved, nil
	}
	if isHTTPURL(mediaPath) {
		if err := netutil.ValidateRemoteURL(mediaPath, true); err != nil {
			return "", err
		}
	}
	return mediaPath, nil
}

func (h *Handler) handleListVideoAttachments(w http.ResponseWriter, r *http.Request) {
	itemID := guid.Normalize(chi.URLParam(r, "Id"))
	mediaSourceID := guid.Normalize(chi.URLParam(r, "MediaSourceId"))
	if !h.canAccessItemID(r, itemID) {
		httpx.Error(w, http.StatusForbidden, "access denied")
		return
	}
	mediaPath := h.resolveMediaSourcePath(r.Context(), itemID, mediaSourceID)
	if mediaPath == "" || h.extractor == nil {
		httpx.JSON(w, http.StatusOK, []videoAttachmentDTO{})
		return
	}
	mediaPath, err := h.resolveExtractionMediaPath(r.Context(), mediaPath)
	if err != nil {
		slog.Error("failed to resolve strm for attachment listing", "item_id", itemID, "error", err)
		httpx.Error(w, http.StatusBadGateway, "failed to resolve strm stream")
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
	if !h.canAccessItemID(r, itemID) {
		httpx.Error(w, http.StatusForbidden, "access denied")
		return
	}
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
	mediaPath, err = h.resolveExtractionMediaPath(r.Context(), mediaPath)
	if err != nil {
		slog.Error("failed to resolve strm for attachment stream", "item_id", itemID, "error", err)
		httpx.Error(w, http.StatusBadGateway, "failed to resolve strm stream")
		return
	}
	fonts, err := h.extractor.ExtractFonts(r.Context(), mediaPath, itemID, mediaSourceID)
	if err != nil || idx >= len(fonts) {
		httpx.Error(w, http.StatusNotFound, "attachment stream not found")
		return
	}
	fontPath := fonts[idx]
	f, err := os.Open(fontPath)
	if err != nil {
		if errors.Is(err, os.ErrPermission) {
			logAndWritePermissionDenied(w, fontPath, err)
			return
		}
		httpx.Error(w, http.StatusNotFound, "attachment file not found")
		return
	}
	defer f.Close()
	stat, err := f.Stat()
	if err != nil || stat.IsDir() {
		httpx.Error(w, http.StatusNotFound, "attachment file not found")
		return
	}
	etag := fmt.Sprintf(`"%x-%x"`, stat.ModTime().UnixNano(), stat.Size())

	w.Header().Set("Content-Type", fontContentType(fontPath))
	w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	w.Header().Set("ETag", etag)
	http.ServeContent(w, r, stat.Name(), stat.ModTime(), f)
}

func (h *Handler) handleDeleteAlternateSources(w http.ResponseWriter, r *http.Request) {
	itemID := guid.Normalize(chi.URLParam(r, "Id"))
	if _, err := h.repo.SplitItemVersions(r.Context(), itemID); err != nil {
		if errors.Is(err, sqlite.ErrNotFound) {
			httpx.Error(w, http.StatusNotFound, "item not found")
			return
		}
		slog.Error("failed to split item versions", "item_id", itemID, "error", err)
		httpx.Error(w, http.StatusInternalServerError, "failed to split item versions")
		return
	}
	httpx.NoContent(w)
}

func (h *Handler) handleMergeVersions(w http.ResponseWriter, r *http.Request) {
	q := httpx.ParseQuery(r)
	ids := q.List("Ids")
	if len(ids) < 2 {
		httpx.Error(w, http.StatusBadRequest, "at least two item ids are required")
		return
	}
	if _, _, err := h.repo.MergeItemVersions(r.Context(), ids); err != nil {
		if errors.Is(err, sqlite.ErrNotFound) {
			httpx.Error(w, http.StatusNotFound, "item not found")
			return
		}
		lowerMsg := strings.ToLower(err.Error())
		if strings.Contains(lowerMsg, "at least 2") ||
			strings.Contains(lowerMsg, "cannot merge") {
			httpx.Error(w, http.StatusBadRequest, "invalid items for version merge")
			return
		}
		slog.Error("failed to merge item versions", "ids", ids, "error", err)
		httpx.Error(w, http.StatusInternalServerError, "failed to merge item versions")
		return
	}
	httpx.NoContent(w)
}

func (h *Handler) resolveDeviceID(r *http.Request, q httpx.Query) string {
	queryDev := strings.TrimSpace(q.Get("DeviceId"))
	_, isAdmin := h.callerIdentity(r)
	if isAdmin && queryDev != "" {
		return queryDev
	}
	if ac, ok := middleware.AuthFromContext(r.Context()); ok && ac != nil {
		if boundDev := strings.TrimSpace(ac.Client.DeviceID); boundDev != "" {
			return boundDev
		}
	}
	return queryDev
}

func scopePlaySessionID(playSessionID, callerUserID string, isAdmin bool) string {
	playSessionID = strings.TrimSpace(playSessionID)
	if playSessionID == "" || isAdmin || callerUserID == "" {
		return playSessionID
	}
	prefix := callerUserID + ":"
	if strings.HasPrefix(playSessionID, prefix) {
		return playSessionID
	}
	return prefix + playSessionID
}

func (h *Handler) handleDeleteActiveEncodings(w http.ResponseWriter, r *http.Request) {
	q := httpx.ParseQuery(r)
	callerUserID, isAdmin := h.callerIdentity(r)
	rawPlaySessionID := strings.TrimSpace(q.Get("PlaySessionId"))
	rawDeviceID := strings.TrimSpace(q.Get("DeviceId"))
	deviceID := ""
	if rawPlaySessionID == "" || rawDeviceID != "" {
		deviceID = h.resolveDeviceID(r, q)
	}
	playSessionID := scopePlaySessionID(rawPlaySessionID, callerUserID, isAdmin)
	if h.transcodeMgr != nil {
		h.transcodeMgr.StopSessionsScoped(callerUserID, playSessionID, deviceID, isAdmin)
		if isAdmin && rawPlaySessionID != "" {
			suffix := ":" + rawPlaySessionID
			for _, snap := range h.transcodeMgr.ActiveSessions() {
				if strings.HasSuffix(snap.PlaySessionID, suffix) {
					h.transcodeMgr.StopSessionsScoped(callerUserID, snap.PlaySessionID, "", true)
				}
			}
		}
	}
	httpx.NoContent(w)
}

func (h *Handler) handleVideoHlsSegment(w http.ResponseWriter, r *http.Request) {
	itemID := guid.Normalize(chi.URLParam(r, "Id"))
	if !h.canAccessItemID(r, itemID) {
		httpx.Error(w, http.StatusNotFound, "hls segment not available")
		return
	}
	if _, err := h.repo.GetItemRecordByID(r.Context(), itemID); errors.Is(err, sqlite.ErrNotFound) {
		httpx.Error(w, http.StatusNotFound, "hls segment not available")
		return
	}
	httpx.Error(w, http.StatusNotFound, "hls segment not available")
}

func (h *Handler) handleAudioHlsSegment(w http.ResponseWriter, r *http.Request) {
	itemID := guid.Normalize(chi.URLParam(r, "Id"))
	if !h.canAccessItemID(r, itemID) {
		httpx.Error(w, http.StatusNotFound, "audio hls segment not available")
		return
	}
	if _, err := h.repo.GetItemRecordByID(r.Context(), itemID); errors.Is(err, sqlite.ErrNotFound) {
		httpx.Error(w, http.StatusNotFound, "audio hls segment not available")
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
