package compatmedia

import (
	"database/sql"
	"encoding/json"
	"errors"
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
			httpx.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		httpx.JSON(w, http.StatusOK, val)
	}
}

func (h *Handler) handleSetEncodingConfig(key string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if r.Body != nil {
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				body = map[string]any{}
			}
		}
		if body == nil {
			body = map[string]any{}
		}
		if err := h.setConfigJSON(r.Context(), key, body); err != nil {
			httpx.Error(w, http.StatusInternalServerError, err.Error())
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
		_ = json.NewDecoder(r.Body).Decode(&body)
	}
	if body == nil {
		body = map[string]any{}
	}
	body["CodecId"] = codecID
	if err := h.setConfigJSON(r.Context(), key, body); err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
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
		httpx.Error(w, http.StatusInternalServerError, err.Error())
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
	libs, err := h.repo.ListLibraries(r.Context())
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]map[string]any, 0, len(libs))
	for _, lib := range libs {
		subFolders := make([]map[string]any, 0, len(lib.Paths))
		for _, p := range lib.Paths {
			subFolders = append(subFolders, map[string]any{"Name": p, "Path": p})
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
	libs, err := h.repo.ListLibraries(r.Context())
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
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

	items := make([]virtualFolderInfo, 0, len(libs))
	for _, lib := range libs {
		var opts map[string]any
		_ = json.Unmarshal([]byte(lib.OptionsJSON), &opts)
		items = append(items, virtualFolderInfo{
			Name:           lib.Name,
			Locations:      lib.Paths,
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
		_ = json.NewDecoder(r.Body).Decode(&body)
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
		_ = json.NewDecoder(r.Body).Decode(&body)
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
		_ = json.NewDecoder(r.Body).Decode(&body)
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
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	updated, err := h.repo.AddLibraryPath(r.Context(), lib.ID, pathVal)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if h.watcher != nil && updated != nil {
		_ = h.watcher.WatchLibrary(updated.ID, updated.Paths)
	}
	httpx.NoContent(w)
}

func (h *Handler) handleUpdateVirtualFolderPath(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Id       string `json:"Id"`
		Name     string `json:"Name"`
		PathInfo struct {
			Path        string `json:"Path"`
			NetworkPath string `json:"NetworkPath"`
		} `json:"PathInfo"`
	}
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&body)
	}
	if body.Id == "" && body.Name == "" {
		httpx.NoContent(w)
		return
	}
	if lib, err := h.findLibraryByIDOrName(r, body.Id, body.Name); err == nil && body.PathInfo.Path != "" {
		if updated, addErr := h.repo.AddLibraryPath(r.Context(), lib.ID, body.PathInfo.Path); addErr == nil && h.watcher != nil && updated != nil {
			_ = h.watcher.WatchLibrary(updated.ID, updated.Paths)
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
		_ = json.NewDecoder(r.Body).Decode(&body)
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
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	filtered := make([]string, 0, len(lib.Paths))
	for _, p := range lib.Paths {
		if p != pathVal {
			filtered = append(filtered, p)
		}
	}
	if err := h.saveLibraryPaths(r, lib.ID, filtered); err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
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
		_ = json.NewDecoder(r.Body).Decode(&body)
	}
	if body.Id != "" && body.LibraryOptions != nil && h.store != nil {
		raw, _ := json.Marshal(body.LibraryOptions)
		_, _ = h.store.Writer.ExecContext(r.Context(),
			`UPDATE libraries SET options_json = ?, updated_at = ? WHERE id = ?`,
			string(raw), time.Now().Unix(), guid.Normalize(body.Id))
	}
	httpx.NoContent(w)
}

// --- /Artists, /Genres, /MusicGenres, /GameGenres, /Persons, /Studios ---

func (h *Handler) handleListArtists(albumArtistsOnly bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := httpx.ParseQuery(r)
		types := []string{"MusicArtist"}
		if !albumArtistsOnly {
			types = append(types, "Artist")
		}
		items, total, err := h.repo.QueryItems(r.Context(), sqlite.ItemFilter{
			ServerID:         h.serverID(),
			UserID:           h.resolveUserID(r, q),
			ParentID:         q.Get("ParentId"),
			Recursive:        true,
			IncludeItemTypes: types,
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

func (h *Handler) handleArtistPrefixes(w http.ResponseWriter, r *http.Request) {
	items, _, err := h.repo.QueryItems(r.Context(), sqlite.ItemFilter{
		ServerID:         h.serverID(),
		Recursive:        true,
		IncludeItemTypes: []string{"MusicArtist", "Artist"},
	})
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
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
	seedID := q.Get("Id")
	if seedID != "" {
		items, total, err := h.repo.GetInstantMix(
			r.Context(),
			h.serverID(),
			h.resolveUserID(r, q),
			seedID,
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
		return
	}
	items, total, err := h.repo.QueryItems(r.Context(), sqlite.ItemFilter{
		ServerID:         h.serverID(),
		UserID:           h.resolveUserID(r, q),
		Recursive:        true,
		IncludeItemTypes: []string{"Audio"},
		SortBy:           []string{"Random"},
		Limit:            q.Int("Limit", 50),
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

func (h *Handler) handleGetTaxonomyByName(kind, dtoType string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := chi.URLParam(r, "Name")
		if h.store != nil {
			// Check items table first for artists
			if kind == "artist" {
				var id string
				err := h.store.Reader.QueryRowContext(r.Context(),
					`SELECT id FROM items WHERE LOWER(type) IN ('musicartist','artist') AND LOWER(name) = LOWER(?) LIMIT 1`, name,
				).Scan(&id)
				if err == nil {
					if it, err := h.repo.GetItemByID(r.Context(), h.serverID(), h.resolveUserID(r, httpx.ParseQuery(r)), id); err == nil {
						httpx.JSON(w, http.StatusOK, it)
						return
					}
				}
			}
			var id, foundName string
			err := h.store.Reader.QueryRowContext(r.Context(),
				`SELECT id, name FROM taxonomies WHERE LOWER(kind) = LOWER(?) AND LOWER(name) = LOWER(?) LIMIT 1`,
				kind, name,
			).Scan(&id, &foundName)
			if err == nil {
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

func (h *Handler) handleGetPersonByName(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "Name")
	if h.store != nil {
		var id, foundName, primaryTag string
		err := h.store.Reader.QueryRowContext(r.Context(), `
			SELECT p.id, p.name, COALESCE(img.tag, '')
			FROM people p
			LEFT JOIN item_images img ON img.item_id = p.id AND LOWER(img.image_type) = 'primary' AND img.image_index = 0
			WHERE LOWER(p.name) = LOWER(?) LIMIT 1`, name,
		).Scan(&id, &foundName, &primaryTag)
		if err == nil {
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
			httpx.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	httpx.Error(w, http.StatusNotFound, "person not found")
}

func (h *Handler) handlePersonCredits(w http.ResponseWriter, r *http.Request) {
	q := httpx.ParseQuery(r)
	personID := guid.Normalize(chi.URLParam(r, "Id"))
	items, total, err := h.repo.QueryItems(r.Context(), sqlite.ItemFilter{
		ServerID:   h.serverID(),
		UserID:     h.resolveUserID(r, q),
		Recursive:  true,
		PersonIDs:  []string{personID},
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

// --- /AudioBooks, /Channels, /Collections, /Playlists, /Shows ---

func (h *Handler) handleAudioBooksNextUp(w http.ResponseWriter, r *http.Request) {
	q := httpx.ParseQuery(r)
	items, total, err := h.repo.GetResumeItems(
		r.Context(),
		h.serverID(),
		h.resolveUserID(r, q),
		q.Get("ParentId"),
		[]string{"AudioBook"},
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

func (h *Handler) handleChannelLatestItems(w http.ResponseWriter, r *http.Request) {
	q := httpx.ParseQuery(r)
	items, total, err := h.repo.QueryItems(r.Context(), sqlite.ItemFilter{
		ServerID:         h.serverID(),
		UserID:           h.resolveUserID(r, q),
		ParentID:         q.Get("ChannelIds"),
		Recursive:        true,
		IncludeItemTypes: []string{"ChannelVideoItem", "ChannelAudioItem", "Video", "Audio"},
		SortBy:           []string{"DateCreated"},
		SortOrder:        "Descending",
		StartIndex:       q.Int("StartIndex", 0),
		Limit:            q.Int("Limit", 20),
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

func (h *Handler) handleCollectionMissing(w http.ResponseWriter, _ *http.Request) {
	httpx.JSON(w, http.StatusOK, dto.QueryResult[dto.BaseItemDto]{
		Items:            []dto.BaseItemDto{},
		TotalRecordCount: 0,
	})
}

func (h *Handler) handleCollectionProviderItems(w http.ResponseWriter, r *http.Request) {
	q := httpx.ParseQuery(r)
	items, total, err := h.repo.GetContainerItems(
		r.Context(),
		h.serverID(),
		h.resolveUserID(r, q),
		chi.URLParam(r, "Id"),
		q.Int("StartIndex", 0),
		q.Int("Limit", 0),
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
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, dto.QueryResult[sqlite.PlaylistItemDto]{
		Items:            items,
		TotalRecordCount: total,
	})
}

func (h *Handler) handleContainerItemsDelete(w http.ResponseWriter, r *http.Request) {
	containerID := guid.Normalize(chi.URLParam(r, "Id"))
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
		_ = json.NewDecoder(r.Body).Decode(&body)
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
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.NoContent(w)
}

func (h *Handler) handleAddToPlaylistInfo(w http.ResponseWriter, r *http.Request) {
	playlistID := guid.Normalize(chi.URLParam(r, "Id"))
	q := httpx.ParseQuery(r)
	items, _, err := h.repo.GetContainerItems(r.Context(), h.serverID(), h.resolveUserID(r, q), playlistID, 0, 0, nil)
	existing := make(map[string]bool)
	if err == nil {
		for _, it := range items {
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
		"ItemCount":           len(requested),
		"ContainsDuplicates":  dups > 0,
		"DuplicateCount":      dups,
	})
}

func (h *Handler) handleShowsUpcoming(w http.ResponseWriter, r *http.Request) {
	q := httpx.ParseQuery(r)
	items, total, err := h.repo.QueryItems(r.Context(), sqlite.ItemFilter{
		ServerID:         h.serverID(),
		UserID:           h.resolveUserID(r, q),
		ParentID:         q.Get("ParentId"),
		Recursive:        true,
		IncludeItemTypes: []string{"Episode"},
		SortBy:           []string{"PremiereDate", "DateCreated"},
		SortOrder:        "Descending",
		StartIndex:       q.Int("StartIndex", 0),
		Limit:            q.Int("Limit", 20),
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
