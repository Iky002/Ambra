package compatmedia

import (
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/go-chi/chi/v5"

	"github.com/ambra-media/ambra/internal/api/dto"
	"github.com/ambra-media/ambra/internal/api/httpx"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

// --- /Encoding/* ---

func defaultFullToneMapOptions() map[string]any {
	return map[string]any{
		"EnableToneMapping":       true,
		"ToneMappingAlgorithm":    "hable",
		"ToneMappingMode":         "auto",
		"ToneMappingDesat":        0.0,
		"ToneMappingParam":        0.0,
		"ToneMappingPeak":         100,
	}
}

func defaultPublicToneMapOptions() map[string]any {
	return map[string]any{
		"EnableToneMapping":    true,
		"ToneMappingAlgorithm": "hable",
	}
}

func defaultSubtitleOptions() map[string]any {
	return map[string]any{
		"SkipIfEmbeddedSubtitlesPresent": false,
		"SkipIfAudioTrackMatches":        false,
		"DownloadLanguages":              []string{"chi", "eng"},
		"DownloadMovieSubtitles":         false,
		"DownloadEpisodeSubtitles":       false,
		"IsOpenSubtitleVipAccount":       false,
		"RequireHashMatch":               false,
	}
}

func defaultFfmpegOptions() map[string]any {
	return map[string]any{
		"HardwareAccelerationMode":      1,
		"EnableThrottling":              true,
		"ThrottleBufferSize":            120,
		"ThrottleHysteresis":            8,
		"H264Crf":                       23,
		"H264Preset":                    "veryfast",
		"EnableHardwareEncoding":        true,
		"AllowHevcEncoding":             true,
		"EnableSubtitleExtraction":      true,
		"TranscodingTempPath":           "",
		"EncoderAppPathDisplay":         "ffmpeg",
	}
}

func (h *Handler) handleGetToneMapOptions(w http.ResponseWriter, r *http.Request) {
	full := defaultFullToneMapOptions()
	_, _ = h.getConfigJSON(r.Context(), "encoding_full_tonemap", &full)
	httpx.JSON(w, http.StatusOK, full)
}

func (h *Handler) handleGetEncodingConfig(key string, defFn func() map[string]any) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		val := defFn()
		if _, err := h.getConfigJSON(r.Context(), key, &val); err != nil {
			slog.Error("failed to load encoding config", "key", key, "error", err)
			httpx.Error(w, http.StatusInternalServerError, "failed to load encoding config")
			return
		}
		httpx.JSON(w, http.StatusOK, val)
	}
}

func (h *Handler) handleSetEncodingConfig(key string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if r.Body != nil {
			if err := httpx.DecodeJSONLimit(r, &body, 0); err != nil {
				body = map[string]any{}
			}
		}
		if body == nil {
			body = map[string]any{}
		}
		if err := h.setConfigJSON(r.Context(), key, body); err != nil {
			slog.Error("failed to save encoding config", "key", key, "error", err)
			httpx.Error(w, http.StatusInternalServerError, "failed to save encoding config")
			return
		}
		httpx.NoContent(w)
	}
}

func (h *Handler) handleGetCodecParameters(w http.ResponseWriter, r *http.Request) {
	q := httpx.ParseQuery(r)
	codecID := nonEmpty(q.Get("CodecId"), "default")
	key := "encoding_codec_params_" + strings.ToLower(codecID)
	val := map[string]any{
		"CodecId": codecID,
		"Type":    q.Get("Type"),
	}
	_, _ = h.getConfigJSON(r.Context(), key, &val)
	httpx.JSON(w, http.StatusOK, val)
}

func (h *Handler) handleSetCodecParameters(w http.ResponseWriter, r *http.Request) {
	q := httpx.ParseQuery(r)
	codecID := nonEmpty(q.Get("CodecId"), "default")
	key := "encoding_codec_params_" + strings.ToLower(codecID)
	var body map[string]any
	if r.Body != nil {
		_ = httpx.DecodeJSONLimit(r, &body, 0)
	}
	if body == nil {
		body = map[string]any{}
	}
	body["CodecId"] = codecID
	if err := h.setConfigJSON(r.Context(), key, body); err != nil {
		slog.Error("failed to save codec parameters", "key", key, "error", err)
		httpx.Error(w, http.StatusInternalServerError, "failed to save codec parameters")
		return
	}
	httpx.NoContent(w)
}

// --- /Libraries & /Library/* ---

func (h *Handler) handleLibraryAvailableOptions(w http.ResponseWriter, _ *http.Request) {
	httpx.JSON(w, http.StatusOK, map[string]any{
		"MetadataSavers": []map[string]any{
			{"Name": "Nfo", "DefaultEnabled": true},
		},
		"MetadataReaders": []map[string]any{
			{"Name": "Nfo", "DefaultEnabled": true},
		},
		"SubtitleFetchers": []map[string]any{},
		"LyricsFetchers":   []map[string]any{},
		"TypeOptions": []map[string]any{
			{
				"Type": "Movie",
				"MetadataFetchers": []map[string]any{
					{"Name": "TheMovieDb", "DefaultEnabled": true},
				},
				"ImageFetchers": []map[string]any{
					{"Name": "TheMovieDb", "DefaultEnabled": true},
				},
				"SupportedImageTypes": []string{"Primary", "Backdrop", "Logo", "Thumb"},
			},
			{
				"Type": "Series",
				"MetadataFetchers": []map[string]any{
					{"Name": "TheMovieDb", "DefaultEnabled": true},
				},
				"ImageFetchers": []map[string]any{
					{"Name": "TheMovieDb", "DefaultEnabled": true},
				},
				"SupportedImageTypes": []string{"Primary", "Backdrop", "Logo", "Banner"},
			},
		},
	})
}

func (h *Handler) handlePhysicalPaths(w http.ResponseWriter, r *http.Request) {
	libs, err := h.repo.ListLibraries(r.Context())
	if err != nil {
		slog.Error("internal error in compatmedia encoding handler", "error", err)
		httpx.Error(w, http.StatusInternalServerError, "internal server error")
		return
	}
	seen := map[string]bool{}
	paths := make([]string, 0)
	for _, lib := range libs {
		for _, p := range lib.Paths {
			if p != "" && !seen[p] {
				seen[p] = true
				paths = append(paths, p)
			}
		}
	}
	httpx.JSON(w, http.StatusOK, paths)
}

func (h *Handler) handleSelectableMediaFolders(w http.ResponseWriter, r *http.Request) {
	q := httpx.ParseQuery(r)
	if targetUID := q.Get("UserId"); targetUID != "" && !h.canAccessUserData(r, targetUID) {
		httpx.Error(w, http.StatusForbidden, "forbidden")
		return
	}
	allowed := h.allowedLibrariesForRequest(r, h.resolveUserID(r, q))
	libs, err := h.repo.ListLibraries(r.Context())
	if err != nil {
		slog.Error("internal error in compatmedia encoding handler", "error", err)
		httpx.Error(w, http.StatusInternalServerError, "internal server error")
		return
	}
	hidePaths := h.isNonAdminCaller(r)
	out := make([]map[string]any, 0, len(libs))
	for _, lib := range libs {
		if !isLibraryAllowed(lib.ID, allowed) {
			continue
		}
		subFolders := make([]map[string]any, 0, len(lib.Paths))
		if !hidePaths {
			for _, p := range lib.Paths {
				subFolders = append(subFolders, map[string]any{"Name": p, "Path": p})
			}
		}
		out = append(out, map[string]any{
			"Name":       lib.Name,
			"Id":         lib.ID,
			"SubFolders": subFolders,
		})
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (h *Handler) handleMediaFolders(w http.ResponseWriter, r *http.Request) {
	q := httpx.ParseQuery(r)
	if targetUID := q.Get("UserId"); targetUID != "" && !h.canAccessUserData(r, targetUID) {
		httpx.Error(w, http.StatusForbidden, "forbidden")
		return
	}
	allowed := h.allowedLibrariesForRequest(r, h.resolveUserID(r, q))
	libs, err := h.repo.ListLibraries(r.Context())
	if err != nil {
		slog.Error("internal error in compatmedia encoding handler", "error", err)
		httpx.Error(w, http.StatusInternalServerError, "internal server error")
		return
	}
	items := make([]dto.BaseItemDto, 0, len(libs))
	for _, lib := range libs {
		if !isLibraryAllowed(lib.ID, allowed) {
			continue
		}
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

func (h *Handler) handleLibraryMediaHook(w http.ResponseWriter, _ *http.Request) {
	if h.scanner != nil {
		h.scanner.ScanLibraryAsync("")
	}
	httpx.NoContent(w)
}

type virtualFolderInfo struct {
	Name           string         `json:"Name"`
	Locations      []string       `json:"Locations"`
	CollectionType string         `json:"CollectionType"`
	ItemId         string         `json:"ItemId"`
	LibraryOptions map[string]any `json:"LibraryOptions,omitempty"`
}

func (h *Handler) handleQueryVirtualFolders(w http.ResponseWriter, r *http.Request) {
	q := httpx.ParseQuery(r)
	if targetUID := q.Get("UserId"); targetUID != "" && !h.canAccessUserData(r, targetUID) {
		httpx.Error(w, http.StatusForbidden, "forbidden")
		return
	}
	allowed := h.allowedLibrariesForRequest(r, h.resolveUserID(r, q))
	allLibs, err := h.repo.ListLibraries(r.Context())
	if err != nil {
		slog.Error("internal error in compatmedia encoding handler", "error", err)
		httpx.Error(w, http.StatusInternalServerError, "internal server error")
		return
	}
	libs := make([]sqlite.LibraryRecord, 0, len(allLibs))
	for _, lib := range allLibs {
		if !isLibraryAllowed(lib.ID, allowed) {
			continue
		}
		libs = append(libs, lib)
	}
	total := len(libs)
	start := q.Int("StartIndex", 0)
	limit := q.Int("Limit", 0)
	if start > len(libs) {
		libs = nil
	} else if start > 0 {
		libs = libs[start:]
	}
	if limit > 0 && limit < len(libs) {
		libs = libs[:limit]
	}

	hidePaths := h.isNonAdminCaller(r)
	items := make([]virtualFolderInfo, 0, len(libs))
	for _, lib := range libs {
		locations := lib.Paths
		opts := sqlite.ParseLibraryOptions(lib.OptionsJSON)
		if hidePaths {
			locations = []string{}
			if opts != nil {
				delete(opts, "PathInfos")
			}
		}
		items = append(items, virtualFolderInfo{
			Name:           lib.Name,
			Locations:      locations,
			CollectionType: lib.CollectionType,
			ItemId:         lib.ID,
			LibraryOptions: opts,
		})
	}
	httpx.JSON(w, http.StatusOK, dto.QueryResult[virtualFolderInfo]{
		Items:            items,
		TotalRecordCount: total,
	})
}

func (h *Handler) handleDeleteVirtualFolder(w http.ResponseWriter, r *http.Request) {
	q := httpx.ParseQuery(r)
	var body struct {
		Id   string `json:"Id"`
		Name string `json:"Name"`
	}
	if r.Body != nil {
		_ = httpx.DecodeJSONLimit(r, &body, 0)
	}
	if id := nonEmpty(q.Get("id"), body.Id); id != "" {
		_ = h.repo.DeleteLibrary(r.Context(), id)
		if h.watcher != nil {
			_ = h.watcher.UnwatchLibrary(guid.Normalize(id))
		}
		httpx.NoContent(w)
		return
	}
	if name := nonEmpty(q.Get("name"), body.Name); name != "" {
		var watchID string
		if h.watcher != nil {
			if lib, err := h.findLibraryByIDOrName(r, "", name); err == nil && lib != nil {
				watchID = lib.ID
			}
		}
		_ = h.repo.DeleteLibraryByName(r.Context(), name)
		if h.watcher != nil && watchID != "" {
			_ = h.watcher.UnwatchLibrary(watchID)
		}
		httpx.NoContent(w)
		return
	}
	httpx.Error(w, http.StatusBadRequest, "id or name is required")
}

func (h *Handler) handleRenameVirtualFolder(w http.ResponseWriter, r *http.Request) {
	q := httpx.ParseQuery(r)
	var body struct {
		Id      string `json:"Id"`
		Name    string `json:"Name"`
		NewName string `json:"NewName"`
	}
	if r.Body != nil {
		_ = httpx.DecodeJSONLimit(r, &body, 0)
	}
	newName := nonEmpty(q.Get("newName"), body.NewName)
	id := nonEmpty(q.Get("id"), body.Id)
	oldName := nonEmpty(q.Get("name"), body.Name)
	if newName == "" || (id == "" && oldName == "") {
		httpx.Error(w, http.StatusBadRequest, "id/name and newName are required")
		return
	}
	if h.store != nil {
		now := time.Now().Unix()
		if id != "" {
			_, _ = h.store.Writer.ExecContext(r.Context(),
				`UPDATE libraries SET name = ?, updated_at = ? WHERE id = ?`, newName, now, guid.Normalize(id))
		} else {
			_, _ = h.store.Writer.ExecContext(r.Context(),
				`UPDATE libraries SET name = ?, updated_at = ? WHERE name = ?`, newName, now, oldName)
		}
	}
	httpx.NoContent(w)
}

func (h *Handler) findLibraryByIDOrName(r *http.Request, id, name string) (*sqlite.LibraryRecord, error) {
	if id != "" {
		return h.repo.GetLibrary(r.Context(), id)
	}
	libs, err := h.repo.ListLibraries(r.Context())
	if err != nil {
		return nil, err
	}
	for i := range libs {
		if strings.EqualFold(libs[i].Name, name) {
			return &libs[i], nil
		}
	}
	return nil, sqlite.ErrNotFound
}

func (h *Handler) saveLibraryPaths(r *http.Request, libID string, paths []string) error {
	if h.store == nil {
		return nil
	}
	raw, err := json.Marshal(paths)
	if err != nil {
		return err
	}
	_, err = h.store.Writer.ExecContext(r.Context(),
		`UPDATE libraries SET paths_json = ?, updated_at = ? WHERE id = ?`,
		string(raw), time.Now().Unix(), libID,
	)
	return err
}

func (h *Handler) handleAddVirtualFolderPath(w http.ResponseWriter, r *http.Request) {
	q := httpx.ParseQuery(r)
	var body struct {
		Id       string `json:"Id"`
		Name     string `json:"Name"`
		Path     string `json:"Path"`
		PathInfo struct {
			Path string `json:"Path"`
		} `json:"PathInfo"`
	}
	if r.Body != nil {
		_ = httpx.DecodeJSONLimit(r, &body, 0)
	}
	pathVal := nonEmpty(q.Get("path"), body.Path, body.PathInfo.Path)
	id := nonEmpty(q.Get("id"), body.Id)
	name := nonEmpty(q.Get("name"), body.Name)
	if pathVal == "" || (id == "" && name == "") {
		httpx.Error(w, http.StatusBadRequest, "library id/name and path are required")
		return
	}
	lib, err := h.findLibraryByIDOrName(r, id, name)
	if errors.Is(err, sqlite.ErrNotFound) {
		httpx.Error(w, http.StatusNotFound, "library not found")
		return
	}
	if err != nil {
		slog.Error("internal error in compatmedia encoding handler", "error", err)
		httpx.Error(w, http.StatusInternalServerError, "internal server error")
		return
	}
	updated, err := h.repo.AddLibraryPath(r.Context(), lib.ID, pathVal)
	if err != nil {
		slog.Error("internal error in compatmedia encoding handler", "error", err)
		httpx.Error(w, http.StatusInternalServerError, "internal server error")
		return
	}
	if h.watcher != nil && updated != nil {
		_ = h.watcher.WatchLibrary(updated.ID, updated.Paths)
	}
	httpx.NoContent(w)
}

func (h *Handler) handleUpdateVirtualFolderPath(w http.ResponseWriter, r *http.Request) {
	q := httpx.ParseQuery(r)
	var body struct {
		Id       string   `json:"Id"`
		Name     string   `json:"Name"`
		OldPath  string   `json:"OldPath"`
		NewPath  string   `json:"NewPath"`
		Path     string   `json:"Path"`
		Paths    []string `json:"Paths"`
		PathInfo struct {
			Path        string `json:"Path"`
			NetworkPath string `json:"NetworkPath"`
		} `json:"PathInfo"`
	}
	if r.Body != nil {
		_ = httpx.DecodeJSONLimit(r, &body, 0)
	}
	id := nonEmpty(q.Get("id"), body.Id)
	name := nonEmpty(q.Get("name"), body.Name)
	if id == "" && name == "" {
		httpx.Error(w, http.StatusBadRequest, "id or name is required")
		return
	}
	lib, err := h.findLibraryByIDOrName(r, id, name)
	if errors.Is(err, sqlite.ErrNotFound) {
		httpx.Error(w, http.StatusNotFound, "library not found")
		return
	}
	if err != nil {
		slog.Error("internal error in compatmedia encoding handler", "error", err)
		httpx.Error(w, http.StatusInternalServerError, "internal server error")
		return
	}

	var updatedPaths []string
	oldPath := nonEmpty(q.Get("oldPath"), body.OldPath)
	newPath := nonEmpty(q.Get("newPath"), body.NewPath, body.PathInfo.Path, body.Path)

	switch {
	case len(body.Paths) > 0:
		seen := make(map[string]bool, len(body.Paths))
		for _, p := range body.Paths {
			if p = strings.TrimSpace(p); p != "" && !seen[p] {
				seen[p] = true
				updatedPaths = append(updatedPaths, p)
			}
		}
	case oldPath != "" && newPath != "":
		replaced := false
		seen := make(map[string]bool, len(lib.Paths)+1)
		for _, p := range lib.Paths {
			target := p
			if p == oldPath {
				target = newPath
				replaced = true
			}
			if target != "" && !seen[target] {
				seen[target] = true
				updatedPaths = append(updatedPaths, target)
			}
		}
		if !replaced && !seen[newPath] {
			updatedPaths = append(updatedPaths, newPath)
		}
	case newPath != "":
		if len(lib.Paths) == 1 {
			updatedPaths = []string{newPath}
		} else {
			updatedPaths = append([]string(nil), lib.Paths...)
			exists := false
			for _, p := range updatedPaths {
				if p == newPath {
					exists = true
					break
				}
			}
			if !exists {
				updatedPaths = append(updatedPaths, newPath)
			}
		}
	default:
		httpx.NoContent(w)
		return
	}

	if err := h.saveLibraryPaths(r, lib.ID, updatedPaths); err != nil {
		slog.Error("failed to update library paths", "library_id", lib.ID, "error", err)
		httpx.Error(w, http.StatusInternalServerError, "failed to update library paths")
		return
	}
	if h.watcher != nil {
		if len(updatedPaths) == 0 {
			_ = h.watcher.UnwatchLibrary(lib.ID)
		} else {
			_ = h.watcher.WatchLibrary(lib.ID, updatedPaths)
		}
	}
	httpx.NoContent(w)
}

func (h *Handler) handleDeleteVirtualFolderPath(w http.ResponseWriter, r *http.Request) {
	q := httpx.ParseQuery(r)
	var body struct {
		Id   string `json:"Id"`
		Name string `json:"Name"`
		Path string `json:"Path"`
	}
	if r.Body != nil {
		_ = httpx.DecodeJSONLimit(r, &body, 0)
	}
	pathVal := nonEmpty(q.Get("path"), body.Path)
	id := nonEmpty(q.Get("id"), body.Id)
	name := nonEmpty(q.Get("name"), body.Name)
	if pathVal == "" || (id == "" && name == "") {
		httpx.Error(w, http.StatusBadRequest, "library id/name and path are required")
		return
	}
	lib, err := h.findLibraryByIDOrName(r, id, name)
	if errors.Is(err, sqlite.ErrNotFound) {
		httpx.Error(w, http.StatusNotFound, "library not found")
		return
	}
	if err != nil {
		slog.Error("internal error in compatmedia encoding handler", "error", err)
		httpx.Error(w, http.StatusInternalServerError, "internal server error")
		return
	}
	filtered := make([]string, 0, len(lib.Paths))
	for _, p := range lib.Paths {
		if p != pathVal {
			filtered = append(filtered, p)
		}
	}
	if err := h.saveLibraryPaths(r, lib.ID, filtered); err != nil {
		slog.Error("failed to delete library path", "library_id", lib.ID, "error", err)
		httpx.Error(w, http.StatusInternalServerError, "failed to update library paths")
		return
	}
	if h.watcher != nil {
		if len(filtered) == 0 {
			_ = h.watcher.UnwatchLibrary(lib.ID)
		} else {
			_ = h.watcher.WatchLibrary(lib.ID, filtered)
		}
	}
	httpx.NoContent(w)
}

func (h *Handler) handleUpdateVirtualFolderOptions(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Id             string         `json:"Id"`
		LibraryOptions map[string]any `json:"LibraryOptions"`
	}
	if r.Body != nil {
		_ = httpx.DecodeJSONLimit(r, &body, 0)
	}
	if body.Id != "" && body.LibraryOptions != nil && h.repo != nil {
		if _, err := h.repo.UpdateLibraryOptions(r.Context(), body.Id, body.LibraryOptions); err != nil && !errors.Is(err, sqlite.ErrNotFound) {
			slog.Error("failed to update library options", "library_id", body.Id, "error", err)
			httpx.Error(w, http.StatusInternalServerError, "failed to update library options")
			return
		}
	}
	httpx.NoContent(w)
}

// --- /Artists, /Genres, /MusicGenres, /GameGenres, /Persons, /Studios ---

func (h *Handler) handleListArtists(albumArtistsOnly bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := httpx.ParseQuery(r)
		if targetUID := q.Get("UserId"); targetUID != "" && !h.canAccessUserData(r, targetUID) {
			httpx.Error(w, http.StatusForbidden, "forbidden")
			return
		}
		userID := h.resolveUserID(r, q)
		allowed := h.allowedLibrariesForRequest(r, userID)
		maxRating, blockedTags := h.userParentalPolicyForRequest(r, userID)
		types := []string{"MusicArtist"}
		if !albumArtistsOnly {
			types = append(types, "Artist")
		}
		items, total, err := h.repo.QueryItems(r.Context(), sqlite.ItemFilter{
			ServerID:          h.serverID(),
			UserID:            userID,
			ParentID:          q.Get("ParentId"),
			AllowedLibraryIDs: allowed,
			MaxParentalRating: maxRating,
			BlockedTags:       blockedTags,
			Recursive:         true,
			IncludeItemTypes:  types,
			SearchTerm:        q.Get("SearchTerm"),
			SortBy:            q.List("SortBy"),
			SortOrder:         q.Get("SortOrder"),
			StartIndex:        q.Int("StartIndex", 0),
			Limit:             q.Int("Limit", 0),
			Fields:            q.List("Fields"),
		})
		if err != nil {
			slog.Error("internal error in compatmedia encoding handler", "error", err)
			httpx.Error(w, http.StatusInternalServerError, "internal server error")
			return
		}
		h.sanitizeItemsPathsForCaller(r, items)
		httpx.JSON(w, http.StatusOK, dto.QueryResult[dto.BaseItemDto]{
			Items:            items,
			TotalRecordCount: total,
		})
	}
}

func (h *Handler) handleArtistPrefixes(w http.ResponseWriter, r *http.Request) {
	q := httpx.ParseQuery(r)
	if targetUID := q.Get("UserId"); targetUID != "" && !h.canAccessUserData(r, targetUID) {
		httpx.Error(w, http.StatusForbidden, "forbidden")
		return
	}
	userID := h.resolveUserID(r, q)
	allowed := h.allowedLibrariesForRequest(r, userID)
	maxRating, blockedTags := h.userParentalPolicyForRequest(r, userID)
	items, _, err := h.repo.QueryItems(r.Context(), sqlite.ItemFilter{
		ServerID:          h.serverID(),
		UserID:            userID,
		ParentID:          q.Get("ParentId"),
		AllowedLibraryIDs: allowed,
		MaxParentalRating: maxRating,
		BlockedTags:       blockedTags,
		Recursive:         true,
		IncludeItemTypes:  []string{"MusicArtist", "Artist"},
	})
	if err != nil {
		slog.Error("internal error in compatmedia encoding handler", "error", err)
		httpx.Error(w, http.StatusInternalServerError, "internal server error")
		return
	}
	seen := map[string]bool{}
	var prefixes []string
	for _, it := range items {
		if it.Name == "" {
			continue
		}
		ru := []rune(it.Name)[0]
		p := "#"
		if unicode.IsLetter(ru) {
			p = strings.ToUpper(string(ru))
		}
		if !seen[p] {
			seen[p] = true
			prefixes = append(prefixes, p)
		}
	}
	sort.Strings(prefixes)
	out := make([]map[string]string, 0, len(prefixes))
	for _, p := range prefixes {
		out = append(out, map[string]string{"Name": p})
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (h *Handler) handleInstantMixByQuery(w http.ResponseWriter, r *http.Request) {
	q := httpx.ParseQuery(r)
	if targetUID := q.Get("UserId"); targetUID != "" && !h.canAccessUserData(r, targetUID) {
		httpx.Error(w, http.StatusForbidden, "forbidden")
		return
	}
	userID := h.resolveUserID(r, q)
	allowed := h.allowedLibrariesForRequest(r, userID)
	maxRating, blockedTags := h.userParentalPolicyForRequest(r, userID)
	limit := q.Int("Limit", 50)
	fields := q.List("Fields")

	if seedID := q.Get("Id"); seedID != "" {
		if !h.canAccessItemID(r, seedID) {
			httpx.Error(w, http.StatusForbidden, "access denied")
			return
		}
		items, total, err := h.repo.GetInstantMixWithPolicy(
			r.Context(),
			h.serverID(),
			userID,
			seedID,
			limit,
			fields,
			allowed,
			maxRating,
			blockedTags,
		)
		if err != nil {
			slog.Error("internal error in compatmedia encoding handler", "error", err)
			httpx.Error(w, http.StatusInternalServerError, "internal server error")
			return
		}
		h.sanitizeItemsPathsForCaller(r, items)
		httpx.JSON(w, http.StatusOK, dto.QueryResult[dto.BaseItemDto]{
			Items:            items,
			TotalRecordCount: total,
		})
		return
	}

	lowerPath := strings.ToLower(r.URL.Path)
	isArtistRoute := strings.HasPrefix(lowerPath, "/artists")
	isMusicGenreRoute := strings.HasPrefix(lowerPath, "/musicgenres")
	reqName := strings.TrimSpace(nonEmpty(chi.URLParam(r, "Name"), q.Get("Name")))
	artistIDs := q.List("ArtistIds")
	musicGenreIDs := append(q.List("MusicGenreIds"), q.List("GenreIds")...)

	if isArtistRoute && (reqName != "" || len(artistIDs) > 0) {
		targetArtistIDs := make(map[string]bool)
		targetArtistNames := make(map[string]bool)
		if reqName != "" {
			targetArtistNames[strings.ToLower(reqName)] = true
		}
		for _, rawID := range artistIDs {
			aid := guid.Normalize(rawID)
			if aid == "" {
				continue
			}
			if rec, err := h.repo.GetItemRecordByID(r.Context(), aid); err == nil && rec != nil {
				if !h.canAccessItem(r, rec) {
					continue
				}
				targetArtistIDs[aid] = true
				if n := strings.ToLower(strings.TrimSpace(rec.Name)); n != "" {
					targetArtistNames[n] = true
				}
			} else {
				targetArtistIDs[aid] = true
			}
		}

		ancestorMatchedIDs := make(map[string]bool)
		if h.store != nil && len(targetArtistIDs) > 0 {
			for aid := range targetArtistIDs {
				rows, qErr := h.store.Reader.QueryContext(r.Context(),
					`SELECT descendant_id FROM item_ancestors WHERE ancestor_id = ?`, aid)
				if qErr == nil {
					for rows.Next() {
						var did string
						if err := rows.Scan(&did); err == nil {
							ancestorMatchedIDs[guid.Normalize(did)] = true
						}
					}
					rows.Close()
				}
			}
		}

		allAudio, _, err := h.repo.QueryItems(r.Context(), sqlite.ItemFilter{
			ServerID:          h.serverID(),
			UserID:            userID,
			AllowedLibraryIDs: allowed,
			MaxParentalRating: maxRating,
			BlockedTags:       blockedTags,
			Recursive:         true,
			IncludeItemTypes:  []string{"Audio"},
			SortBy:            []string{"Random"},
			Fields:            fields,
		})
		if err != nil {
			slog.Error("internal error in compatmedia encoding handler", "error", err)
			httpx.Error(w, http.StatusInternalServerError, "internal server error")
			return
		}
		matched := make([]dto.BaseItemDto, 0, len(allAudio))
		for _, it := range allAudio {
			ok := ancestorMatchedIDs[guid.Normalize(it.Id)] || targetArtistIDs[guid.Normalize(it.ParentId)]
			if !ok && it.AlbumArtist != "" && targetArtistNames[strings.ToLower(strings.TrimSpace(it.AlbumArtist))] {
				ok = true
			}
			if !ok {
				for _, a := range it.Artists {
					if targetArtistNames[strings.ToLower(strings.TrimSpace(a))] {
						ok = true
						break
					}
				}
			}
			if ok {
				matched = append(matched, it)
			}
		}
		total := len(matched)
		if limit > 0 && len(matched) > limit {
			matched = matched[:limit]
		}
		h.sanitizeItemsPathsForCaller(r, matched)
		httpx.JSON(w, http.StatusOK, dto.QueryResult[dto.BaseItemDto]{
			Items:            matched,
			TotalRecordCount: total,
		})
		return
	}

	if isMusicGenreRoute && (reqName != "" || len(musicGenreIDs) > 0) {
		targetGenreNames := make(map[string]bool)
		targetGenreIDs := make(map[string]bool)
		if reqName != "" {
			targetGenreNames[strings.ToLower(reqName)] = true
		}
		for _, rawID := range musicGenreIDs {
			gid := guid.Normalize(rawID)
			if gid == "" {
				continue
			}
			targetGenreIDs[gid] = true
			if h.store != nil {
				var gName string
				if err := h.store.Reader.QueryRowContext(r.Context(),
					`SELECT name FROM taxonomies WHERE id = ? LIMIT 1`, gid,
				).Scan(&gName); err == nil && strings.TrimSpace(gName) != "" {
					targetGenreNames[strings.ToLower(strings.TrimSpace(gName))] = true
				}
			}
		}

		matchedItemIDs := make(map[string]bool)
		if h.store != nil {
			rows, qErr := h.store.Reader.QueryContext(r.Context(), `
				SELECT it.item_id, tx.id, tx.name
				FROM item_taxonomies it
				JOIN taxonomies tx ON tx.id = it.taxonomy_id
				WHERE LOWER(tx.kind) IN ('genre', 'musicgenre')`)
			if qErr == nil {
				for rows.Next() {
					var iid, tid, tname string
					if err := rows.Scan(&iid, &tid, &tname); err == nil {
						if targetGenreIDs[guid.Normalize(tid)] || targetGenreNames[strings.ToLower(strings.TrimSpace(tname))] {
							matchedItemIDs[guid.Normalize(iid)] = true
						}
					}
				}
				rows.Close()
			}
		}

		allAudio, _, err := h.repo.QueryItems(r.Context(), sqlite.ItemFilter{
			ServerID:          h.serverID(),
			UserID:            userID,
			AllowedLibraryIDs: allowed,
			MaxParentalRating: maxRating,
			BlockedTags:       blockedTags,
			Recursive:         true,
			IncludeItemTypes:  []string{"Audio"},
			SortBy:            []string{"Random"},
			Fields:            append(append([]string(nil), fields...), "Genres"),
		})
		if err != nil {
			slog.Error("internal error in compatmedia encoding handler", "error", err)
			httpx.Error(w, http.StatusInternalServerError, "internal server error")
			return
		}
		matched := make([]dto.BaseItemDto, 0, len(allAudio))
		for _, it := range allAudio {
			ok := matchedItemIDs[guid.Normalize(it.Id)]
			if !ok {
				for _, g := range it.Genres {
					if targetGenreNames[strings.ToLower(strings.TrimSpace(g))] {
						ok = true
						break
					}
				}
			}
			if ok {
				matched = append(matched, it)
			}
		}
		total := len(matched)
		if limit > 0 && len(matched) > limit {
			matched = matched[:limit]
		}
		h.sanitizeItemsPathsForCaller(r, matched)
		httpx.JSON(w, http.StatusOK, dto.QueryResult[dto.BaseItemDto]{
			Items:            matched,
			TotalRecordCount: total,
		})
		return
	}

	items, total, err := h.repo.QueryItems(r.Context(), sqlite.ItemFilter{
		ServerID:          h.serverID(),
		UserID:            userID,
		AllowedLibraryIDs: allowed,
		MaxParentalRating: maxRating,
		BlockedTags:       blockedTags,
		Recursive:         true,
		IncludeItemTypes:  []string{"Audio"},
		SortBy:            []string{"Random"},
		Limit:             limit,
		Fields:            fields,
	})
	if err != nil {
		slog.Error("internal error in compatmedia encoding handler", "error", err)
		httpx.Error(w, http.StatusInternalServerError, "internal server error")
		return
	}
	h.sanitizeItemsPathsForCaller(r, items)
	httpx.JSON(w, http.StatusOK, dto.QueryResult[dto.BaseItemDto]{
		Items:            items,
		TotalRecordCount: total,
	})
}

func (h *Handler) isTaxonomyAccessible(r *http.Request, taxonomyID string, allowed []string, maxRating *int, blockedTags []string) bool {
	if allowed == nil && maxRating == nil && len(blockedTags) == 0 {
		return true
	}
	if h.store == nil || h.repo == nil {
		return false
	}
	rows, err := h.store.Reader.QueryContext(r.Context(),
		`SELECT item_id FROM item_taxonomies WHERE taxonomy_id = ?`, taxonomyID)
	if err != nil {
		return false
	}
	defer rows.Close()
	for rows.Next() {
		var iid string
		if err := rows.Scan(&iid); err != nil {
			continue
		}
		rec, recErr := h.repo.GetItemRecordByID(r.Context(), iid)
		if recErr != nil || rec == nil {
			continue
		}
		if allowed != nil && rec.LibraryID != "" && !isLibraryAllowed(rec.LibraryID, allowed) {
			continue
		}
		if maxRating != nil || len(blockedTags) > 0 {
			if blocked, bErr := h.repo.IsItemBlockedByParentalPolicy(r.Context(), rec, maxRating, blockedTags); bErr != nil || blocked {
				continue
			}
		}
		return true
	}
	return false
}

func (h *Handler) handleGetTaxonomyByName(kind, dtoType string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := httpx.ParseQuery(r)
		if targetUID := q.Get("UserId"); targetUID != "" && !h.canAccessUserData(r, targetUID) {
			httpx.Error(w, http.StatusForbidden, "forbidden")
			return
		}
		userID := h.resolveUserID(r, q)
		allowed := h.allowedLibrariesForRequest(r, userID)
		maxRating, blockedTags := h.userParentalPolicyForRequest(r, userID)
		name := chi.URLParam(r, "Name")
		if h.store != nil {
			// Check items table first for artists (enforcing library RBAC + parental controls)
			if kind == "artist" {
				rows, qErr := h.store.Reader.QueryContext(r.Context(),
					`SELECT id FROM items WHERE LOWER(type) IN ('musicartist','artist') AND LOWER(name) = LOWER(?)`, name,
				)
				if qErr == nil {
					var matchedIDs []string
					for rows.Next() {
						var id string
						if err := rows.Scan(&id); err == nil {
							matchedIDs = append(matchedIDs, id)
						}
					}
					rows.Close()
					for _, id := range matchedIDs {
						rec, recErr := h.repo.GetItemRecordByID(r.Context(), id)
						if recErr != nil || rec == nil {
							continue
						}
						if allowed != nil && rec.LibraryID != "" && !isLibraryAllowed(rec.LibraryID, allowed) {
							continue
						}
						if maxRating != nil || len(blockedTags) > 0 {
							if blocked, bErr := h.repo.IsItemBlockedByParentalPolicy(r.Context(), rec, maxRating, blockedTags); bErr != nil || blocked {
								continue
							}
						}
						if it, err := h.repo.GetItemByID(r.Context(), h.serverID(), userID, id); err == nil {
							h.sanitizeItemPathsForCaller(r, it)
							httpx.JSON(w, http.StatusOK, it)
							return
						}
					}
				}
			}
			var id, foundName string
			err := h.store.Reader.QueryRowContext(r.Context(),
				`SELECT id, name FROM taxonomies WHERE LOWER(kind) = LOWER(?) AND LOWER(name) = LOWER(?) LIMIT 1`,
				kind, name,
			).Scan(&id, &foundName)
			if err == nil && h.isTaxonomyAccessible(r, id, allowed, maxRating, blockedTags) {
				httpx.JSON(w, http.StatusOK, dto.BaseItemDto{
					Id:                id,
					Name:              foundName,
					ServerId:          h.serverID(),
					Type:              dtoType,
					ImageTags:         map[string]string{},
					BackdropImageTags: []string{},
				})
				return
			}
		}
		httpx.Error(w, http.StatusNotFound, dtoType+" not found")
	}
}

func (h *Handler) isPersonAccessible(r *http.Request, personID string, allowed []string, maxRating *int, blockedTags []string) bool {
	if allowed == nil && maxRating == nil && len(blockedTags) == 0 {
		return true
	}
	if h.store == nil || h.repo == nil {
		return false
	}
	rows, err := h.store.Reader.QueryContext(r.Context(),
		`SELECT item_id FROM item_people WHERE person_id = ?`, personID)
	if err != nil {
		return false
	}
	defer rows.Close()
	for rows.Next() {
		var iid string
		if err := rows.Scan(&iid); err != nil {
			continue
		}
		rec, recErr := h.repo.GetItemRecordByID(r.Context(), iid)
		if recErr != nil || rec == nil {
			continue
		}
		if allowed != nil && rec.LibraryID != "" && !isLibraryAllowed(rec.LibraryID, allowed) {
			continue
		}
		if maxRating != nil || len(blockedTags) > 0 {
			if blocked, bErr := h.repo.IsItemBlockedByParentalPolicy(r.Context(), rec, maxRating, blockedTags); bErr != nil || blocked {
				continue
			}
		}
		return true
	}
	return false
}

func (h *Handler) handleGetPersonByName(w http.ResponseWriter, r *http.Request) {
	q := httpx.ParseQuery(r)
	if targetUID := q.Get("UserId"); targetUID != "" && !h.canAccessUserData(r, targetUID) {
		httpx.Error(w, http.StatusForbidden, "forbidden")
		return
	}
	userID := h.resolveUserID(r, q)
	allowed := h.allowedLibrariesForRequest(r, userID)
	maxRating, blockedTags := h.userParentalPolicyForRequest(r, userID)
	name := chi.URLParam(r, "Name")
	normID := guid.Normalize(name)
	if h.store != nil {
		var id, foundName, primaryTag string
		err := h.store.Reader.QueryRowContext(r.Context(), `
			SELECT p.id, p.name, COALESCE(img.tag, '')
			FROM people p
			LEFT JOIN item_images img ON img.item_id = p.id AND LOWER(img.image_type) = 'primary' AND img.image_index = 0
			WHERE LOWER(p.name) = LOWER(?) OR p.id = ? LIMIT 1`, name, normID,
		).Scan(&id, &foundName, &primaryTag)
		if err == nil {
			if !h.isPersonAccessible(r, id, allowed, maxRating, blockedTags) {
				httpx.Error(w, http.StatusNotFound, "person not found")
				return
			}
			imageTags := map[string]string{}
			if primaryTag != "" {
				imageTags["Primary"] = primaryTag
			}
			httpx.JSON(w, http.StatusOK, dto.BaseItemDto{
				Id:                id,
				Name:              foundName,
				ServerId:          h.serverID(),
				Type:              "Person",
				ImageTags:         imageTags,
				BackdropImageTags: []string{},
			})
			return
		}
		if !errors.Is(err, sql.ErrNoRows) {
			slog.Error("failed to query person", "name", name, "error", err)
			httpx.Error(w, http.StatusInternalServerError, "failed to query person")
			return
		}
	}
	httpx.Error(w, http.StatusNotFound, "person not found")
}

func (h *Handler) handlePersonCredits(w http.ResponseWriter, r *http.Request) {
	q := httpx.ParseQuery(r)
	if targetUID := q.Get("UserId"); targetUID != "" && !h.canAccessUserData(r, targetUID) {
		httpx.Error(w, http.StatusForbidden, "forbidden")
		return
	}
	userID := h.resolveUserID(r, q)
	allowed := h.allowedLibrariesForRequest(r, userID)
	maxRating, blockedTags := h.userParentalPolicyForRequest(r, userID)
	personID := guid.Normalize(chi.URLParam(r, "Id"))
	items, total, err := h.repo.QueryItems(r.Context(), sqlite.ItemFilter{
		ServerID:          h.serverID(),
		UserID:            userID,
		AllowedLibraryIDs: allowed,
		MaxParentalRating: maxRating,
		BlockedTags:       blockedTags,
		Recursive:         true,
		PersonIDs:         []string{personID},
		StartIndex:        q.Int("StartIndex", 0),
		Limit:             q.Int("Limit", 0),
		Fields:            q.List("Fields"),
	})
	if err != nil {
		slog.Error("internal error in compatmedia encoding handler", "error", err)
		httpx.Error(w, http.StatusInternalServerError, "internal server error")
		return
	}
	h.sanitizeItemsPathsForCaller(r, items)
	httpx.JSON(w, http.StatusOK, dto.QueryResult[dto.BaseItemDto]{
		Items:            items,
		TotalRecordCount: total,
	})
}

// --- /AudioBooks, /Channels, /Collections, /Playlists, /Shows ---

func (h *Handler) handleAudioBooksNextUp(w http.ResponseWriter, r *http.Request) {
	q := httpx.ParseQuery(r)
	if targetUID := q.Get("UserId"); targetUID != "" && !h.canAccessUserData(r, targetUID) {
		httpx.Error(w, http.StatusForbidden, "forbidden")
		return
	}
	userID := h.resolveUserID(r, q)
	allowed := h.allowedLibrariesForRequest(r, userID)
	maxRating, blockedTags := h.userParentalPolicyForRequest(r, userID)
	items, total, err := h.repo.GetResumeItemsWithPolicy(
		r.Context(),
		h.serverID(),
		userID,
		q.Get("ParentId"),
		[]string{"AudioBook"},
		q.Int("StartIndex", 0),
		q.Int("Limit", 20),
		q.List("Fields"),
		allowed,
		maxRating,
		blockedTags,
	)
	if err != nil {
		slog.Error("internal error in compatmedia encoding handler", "error", err)
		httpx.Error(w, http.StatusInternalServerError, "internal server error")
		return
	}
	h.sanitizeItemsPathsForCaller(r, items)
	httpx.JSON(w, http.StatusOK, dto.QueryResult[dto.BaseItemDto]{
		Items:            items,
		TotalRecordCount: total,
	})
}

func (h *Handler) handleChannelLatestItems(w http.ResponseWriter, r *http.Request) {
	q := httpx.ParseQuery(r)
	if targetUID := q.Get("UserId"); targetUID != "" && !h.canAccessUserData(r, targetUID) {
		httpx.Error(w, http.StatusForbidden, "forbidden")
		return
	}
	channelIDs := q.List("ChannelIds")
	if len(channelIDs) == 0 {
		httpx.JSON(w, http.StatusOK, dto.QueryResult[dto.BaseItemDto]{
			Items:            []dto.BaseItemDto{},
			TotalRecordCount: 0,
		})
		return
	}
	if len(channelIDs) > maxChannelLatestIDs {
		channelIDs = channelIDs[:maxChannelLatestIDs]
	}
	userID := h.resolveUserID(r, q)
	allowed := h.allowedLibrariesForRequest(r, userID)
	maxRating, blockedTags := h.userParentalPolicyForRequest(r, userID)
	startIndex := q.Int("StartIndex", 0)
	limit := q.Int("Limit", 20)
	fields := q.List("Fields")

	if len(channelIDs) == 1 {
		items, total, err := h.repo.QueryItems(r.Context(), sqlite.ItemFilter{
			ServerID:          h.serverID(),
			UserID:            userID,
			ParentID:          channelIDs[0],
			AllowedLibraryIDs: allowed,
			MaxParentalRating: maxRating,
			BlockedTags:       blockedTags,
			Recursive:         true,
			IncludeItemTypes:  []string{"ChannelVideoItem", "ChannelAudioItem", "Video", "Audio"},
			SortBy:            []string{"DateCreated"},
			SortOrder:         "Descending",
			StartIndex:        startIndex,
			Limit:             limit,
			Fields:            fields,
		})
		if err != nil {
			slog.Error("internal error in compatmedia encoding handler", "error", err)
			httpx.Error(w, http.StatusInternalServerError, "internal server error")
			return
		}
		h.sanitizeItemsPathsForCaller(r, items)
		httpx.JSON(w, http.StatusOK, dto.QueryResult[dto.BaseItemDto]{
			Items:            items,
			TotalRecordCount: total,
		})
		return
	}

	seen := make(map[string]bool)
	merged := make([]dto.BaseItemDto, 0)
	for _, cid := range channelIDs {
		part, _, err := h.repo.QueryItems(r.Context(), sqlite.ItemFilter{
			ServerID:          h.serverID(),
			UserID:            userID,
			ParentID:          cid,
			AllowedLibraryIDs: allowed,
			MaxParentalRating: maxRating,
			BlockedTags:       blockedTags,
			Recursive:         true,
			IncludeItemTypes:  []string{"ChannelVideoItem", "ChannelAudioItem", "Video", "Audio"},
			SortBy:            []string{"DateCreated"},
			SortOrder:         "Descending",
			Fields:            fields,
		})
		if err != nil {
			slog.Error("internal error in compatmedia encoding handler", "error", err)
			httpx.Error(w, http.StatusInternalServerError, "internal server error")
			return
		}
		for _, it := range part {
			if !seen[it.Id] {
				seen[it.Id] = true
				merged = append(merged, it)
			}
		}
	}
	sort.Slice(merged, func(i, j int) bool {
		ti, tj := merged[i].DateCreated, merged[j].DateCreated
		if ti == nil && tj == nil {
			return false
		}
		if ti == nil {
			return false
		}
		if tj == nil {
			return true
		}
		return ti.After(*tj)
	})
	total := len(merged)
	if startIndex > len(merged) {
		merged = []dto.BaseItemDto{}
	} else if startIndex > 0 {
		merged = merged[startIndex:]
	}
	if limit > 0 && len(merged) > limit {
		merged = merged[:limit]
	}
	h.sanitizeItemsPathsForCaller(r, merged)
	httpx.JSON(w, http.StatusOK, dto.QueryResult[dto.BaseItemDto]{
		Items:            merged,
		TotalRecordCount: total,
	})
}

func (h *Handler) handleCollectionMissing(w http.ResponseWriter, _ *http.Request) {
	httpx.JSON(w, http.StatusOK, dto.QueryResult[dto.BaseItemDto]{
		Items:            []dto.BaseItemDto{},
		TotalRecordCount: 0,
	})
}

func (h *Handler) handleCollectionProviderItems(w http.ResponseWriter, r *http.Request) {
	q := httpx.ParseQuery(r)
	if targetUID := q.Get("UserId"); targetUID != "" && !h.canAccessUserData(r, targetUID) {
		httpx.Error(w, http.StatusForbidden, "forbidden")
		return
	}
	containerID := guid.Normalize(chi.URLParam(r, "Id"))
	containerRec, err := h.repo.GetItemRecordByID(r.Context(), containerID)
	if errors.Is(err, sqlite.ErrNotFound) {
		httpx.Error(w, http.StatusNotFound, "collection not found")
		return
	}
	if err != nil {
		slog.Error("internal error in compatmedia encoding handler", "error", err)
		httpx.Error(w, http.StatusInternalServerError, "internal server error")
		return
	}
	if !strings.EqualFold(containerRec.Type, "BoxSet") {
		httpx.Error(w, http.StatusNotFound, "collection not found")
		return
	}
	userID := h.resolveUserID(r, q)
	allowed := h.allowedLibrariesForRequest(r, userID)
	maxRating, blockedTags := h.userParentalPolicyForRequest(r, userID)
	if maxRating != nil || len(blockedTags) > 0 {
		blocked, bErr := h.repo.IsItemBlockedByParentalPolicy(r.Context(), containerRec, maxRating, blockedTags)
		if bErr != nil {
			slog.Error("failed to check parental policy", "container_id", containerID, "error", bErr)
			httpx.Error(w, http.StatusInternalServerError, "failed to verify item access")
			return
		}
		if blocked {
			httpx.Error(w, http.StatusForbidden, "access denied")
			return
		}
	}
	startIndex := q.Int("StartIndex", 0)
	limit := q.Int("Limit", 0)
	needFilter := h.isNonAdminCaller(r) || allowed != nil || maxRating != nil || len(blockedTags) > 0

	fetchStart, fetchLimit := startIndex, limit
	if needFilter {
		fetchStart, fetchLimit = 0, 0
	}

	items, total, err := h.repo.GetContainerItems(
		r.Context(),
		h.serverID(),
		userID,
		containerID,
		fetchStart,
		fetchLimit,
		q.List("Fields"),
	)
	if errors.Is(err, sqlite.ErrNotFound) {
		httpx.JSON(w, http.StatusOK, dto.QueryResult[sqlite.PlaylistItemDto]{
			Items:            []sqlite.PlaylistItemDto{},
			TotalRecordCount: 0,
		})
		return
	}
	if err != nil {
		slog.Error("internal error in compatmedia encoding handler", "error", err)
		httpx.Error(w, http.StatusInternalServerError, "internal server error")
		return
	}
	if needFilter {
		filtered := make([]sqlite.PlaylistItemDto, 0, len(items))
		for _, it := range items {
			rec, recErr := h.repo.GetItemRecordByID(r.Context(), it.Id)
			if recErr != nil || rec == nil {
				continue
			}
			if strings.EqualFold(rec.Type, "Playlist") {
				if !h.canViewPlaylist(r, rec.ID, rec.ParentID) {
					continue
				}
			} else if allowed != nil && rec.LibraryID != "" && !isLibraryAllowed(rec.LibraryID, allowed) {
				continue
			}
			if maxRating != nil || len(blockedTags) > 0 {
				if blocked, bErr := h.repo.IsItemBlockedByParentalPolicy(r.Context(), rec, maxRating, blockedTags); bErr != nil || blocked {
					continue
				}
			}
			filtered = append(filtered, it)
		}
		total = len(filtered)
		if startIndex > len(filtered) {
			filtered = []sqlite.PlaylistItemDto{}
		} else if startIndex > 0 {
			filtered = filtered[startIndex:]
		}
		if limit > 0 && len(filtered) > limit {
			filtered = filtered[:limit]
		}
		items = filtered
	}
	h.sanitizePlaylistItemsPathsForCaller(r, items)
	httpx.JSON(w, http.StatusOK, dto.QueryResult[sqlite.PlaylistItemDto]{
		Items:            items,
		TotalRecordCount: total,
	})
}

func (h *Handler) handleContainerItemsDelete(expectedType string, checkOwner bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		containerID := guid.Normalize(chi.URLParam(r, "Id"))
		rec, err := h.repo.GetItemRecordByID(r.Context(), containerID)
		if errors.Is(err, sqlite.ErrNotFound) {
			httpx.Error(w, http.StatusNotFound, "container not found")
			return
		}
		if err != nil {
			slog.Error("internal error in compatmedia encoding handler", "error", err)
			httpx.Error(w, http.StatusInternalServerError, "internal server error")
			return
		}
		if !strings.EqualFold(rec.Type, expectedType) {
			httpx.Error(w, http.StatusNotFound, "container not found")
			return
		}
		if checkOwner {
			callerID, isAdmin := h.callerIdentity(r)
			if callerID != "" || isAdmin {
				if !isAdmin {
					ownerID := guid.Normalize(rec.ParentID)
					if ownerID == "" || callerID != ownerID {
						httpx.Error(w, http.StatusForbidden, "forbidden")
						return
					}
				}
			}
		}

		q := httpx.ParseQuery(r)
		ids := q.List("Ids")
		if len(ids) == 0 {
			ids = q.List("EntryIds")
		}
		if len(ids) == 0 && r.Body != nil {
			var body struct {
				Ids      []string `json:"Ids"`
				EntryIds []string `json:"EntryIds"`
			}
			_ = httpx.DecodeJSONLimit(r, &body, 0)
			if len(body.Ids) > 0 {
				ids = body.Ids
			} else {
				ids = body.EntryIds
			}
		}
		if _, err := h.repo.RemoveItemsFromContainer(r.Context(), containerID, ids); errors.Is(err, sqlite.ErrNotFound) {
			httpx.Error(w, http.StatusNotFound, "container not found")
			return
		} else if err != nil {
			slog.Error("failed to remove items from container", "container_id", containerID, "error", err)
			httpx.Error(w, http.StatusInternalServerError, "failed to remove items from container")
			return
		}
		httpx.NoContent(w)
	}
}

func (h *Handler) handleAddToPlaylistInfo(w http.ResponseWriter, r *http.Request) {
	playlistID := guid.Normalize(chi.URLParam(r, "Id"))
	q := httpx.ParseQuery(r)
	if targetUID := q.Get("UserId"); targetUID != "" && !h.canAccessUserData(r, targetUID) {
		httpx.Error(w, http.StatusForbidden, "forbidden")
		return
	}
	userID := h.resolveUserID(r, q)
	allowed := h.allowedLibrariesForRequest(r, userID)
	maxRating, blockedTags := h.userParentalPolicyForRequest(r, userID)

	rec, err := h.repo.GetItemRecordByID(r.Context(), playlistID)
	if errors.Is(err, sqlite.ErrNotFound) || (err == nil && (rec == nil || !strings.EqualFold(rec.Type, "Playlist"))) {
		httpx.Error(w, http.StatusNotFound, "playlist not found")
		return
	}
	if err != nil {
		slog.Error("internal error in compatmedia encoding handler", "error", err)
		httpx.Error(w, http.StatusInternalServerError, "internal server error")
		return
	}
	if !h.canViewPlaylist(r, rec.ID, rec.ParentID) {
		httpx.Error(w, http.StatusForbidden, "forbidden")
		return
	}
	if maxRating != nil || len(blockedTags) > 0 {
		blocked, bErr := h.repo.IsItemBlockedByParentalPolicy(r.Context(), rec, maxRating, blockedTags)
		if bErr != nil {
			slog.Error("failed to verify parental policy", "playlist_id", playlistID, "error", bErr)
			httpx.Error(w, http.StatusInternalServerError, "failed to verify item access")
			return
		}
		if blocked {
			httpx.Error(w, http.StatusForbidden, "access denied")
			return
		}
	}

	items, _, err := h.repo.GetContainerItems(r.Context(), h.serverID(), userID, playlistID, 0, 0, nil)
	existing := make(map[string]bool)
	if err == nil {
		needFilter := allowed != nil || maxRating != nil || len(blockedTags) > 0
		for _, it := range items {
			if needFilter {
				itemRec, recErr := h.repo.GetItemRecordByID(r.Context(), it.Id)
				if recErr != nil || itemRec == nil {
					continue
				}
				if allowed != nil && itemRec.LibraryID != "" && !isLibraryAllowed(itemRec.LibraryID, allowed) {
					continue
				}
				if maxRating != nil || len(blockedTags) > 0 {
					if blocked, bErr := h.repo.IsItemBlockedByParentalPolicy(r.Context(), itemRec, maxRating, blockedTags); bErr != nil || blocked {
						continue
					}
				}
			}
			existing[it.Id] = true
		}
	}
	requested := q.List("Ids")
	dups := 0
	for _, id := range requested {
		if existing[guid.Normalize(id)] {
			dups++
		}
	}
	httpx.JSON(w, http.StatusOK, map[string]any{
		"ItemCount":          len(requested),
		"ContainsDuplicates": dups > 0,
		"DuplicateCount":     dups,
	})
}

func (h *Handler) handleShowsUpcoming(w http.ResponseWriter, r *http.Request) {
	q := httpx.ParseQuery(r)
	if targetUID := q.Get("UserId"); targetUID != "" && !h.canAccessUserData(r, targetUID) {
		httpx.Error(w, http.StatusForbidden, "forbidden")
		return
	}
	userID := h.resolveUserID(r, q)
	allowed := h.allowedLibrariesForRequest(r, userID)
	maxRating, blockedTags := h.userParentalPolicyForRequest(r, userID)
	startIndex := q.Int("StartIndex", 0)
	limit := q.Int("Limit", 20)

	items, _, err := h.repo.QueryItems(r.Context(), sqlite.ItemFilter{
		ServerID:          h.serverID(),
		UserID:            userID,
		ParentID:          q.Get("ParentId"),
		AllowedLibraryIDs: allowed,
		MaxParentalRating: maxRating,
		BlockedTags:       blockedTags,
		Recursive:         true,
		IncludeItemTypes:  []string{"Episode"},
		SortBy:            []string{"PremiereDate", "DateCreated"},
		SortOrder:         "Ascending",
		Fields:            q.List("Fields"),
	})
	if err != nil {
		slog.Error("internal error in compatmedia encoding handler", "error", err)
		httpx.Error(w, http.StatusInternalServerError, "internal server error")
		return
	}
	now := time.Now().UTC()
	upcoming := make([]dto.BaseItemDto, 0)
	for _, it := range items {
		if it.PremiereDate != nil && !it.PremiereDate.Before(now) {
			upcoming = append(upcoming, it)
		}
	}
	total := len(upcoming)
	if startIndex > len(upcoming) {
		upcoming = []dto.BaseItemDto{}
	} else if startIndex > 0 {
		upcoming = upcoming[startIndex:]
	}
	if limit > 0 && len(upcoming) > limit {
		upcoming = upcoming[:limit]
	}
	h.sanitizeItemsPathsForCaller(r, upcoming)
	httpx.JSON(w, http.StatusOK, dto.QueryResult[dto.BaseItemDto]{
		Items:            upcoming,
		TotalRecordCount: total,
	})
}
