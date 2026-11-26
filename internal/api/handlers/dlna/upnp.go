package dlna

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ambra-media/ambra/internal/api/dto"
	"github.com/ambra-media/ambra/internal/api/httpx"
	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

const (
	contentDirectoryServiceType  = "urn:schemas-upnp-org:service:ContentDirectory:1"
	connectionManagerServiceType = "urn:schemas-upnp-org:service:ConnectionManager:1"
	defaultProtocolInfoSource    = "http-get:*:video/mp4:*,http-get:*:video/x-matroska:*,http-get:*:video/webm:*,http-get:*:audio/mpeg:*,http-get:*:audio/flac:*,http-get:*:audio/mp4:*,http-get:*:image/jpeg:*,http-get:*:image/png:*"
)

func buildDescriptionXML(friendlyName, uuid, baseURL string) string {
	if strings.TrimSpace(friendlyName) == "" {
		friendlyName = config.ProductName
	}
	escapedName := xmlEscape(friendlyName)
	escapedUUID := xmlEscape(uuid)
	escapedBase := xmlEscape(baseURL)
	return fmt.Sprintf(`<?xml version="1.0" encoding="utf-8"?>
<root xmlns="urn:schemas-upnp-org:device-1-0" xmlns:dlna="urn:schemas-dlna-org:device-1-0">
  <specVersion>
    <major>1</major>
    <minor>0</minor>
  </specVersion>
  <device>
    <dlna:X_DLNADOC xmlns:dlna="urn:schemas-dlna-org:device-1-0">DMS-1.50</dlna:X_DLNADOC>
    <deviceType>urn:schemas-upnp-org:device:MediaServer:1</deviceType>
    <friendlyName>%s</friendlyName>
    <manufacturer>Ambra</manufacturer>
    <manufacturerURL>https://ambra.media</manufacturerURL>
    <modelDescription>Ambra Media Server</modelDescription>
    <modelName>%s</modelName>
    <modelNumber>%s</modelNumber>
    <modelURL>https://ambra.media</modelURL>
    <serialNumber>%s</serialNumber>
    <UDN>uuid:%s</UDN>
    <presentationURL>%s</presentationURL>
    <iconList>
      <icon>
        <mimetype>image/png</mimetype>
        <width>120</width>
        <height>120</height>
        <depth>24</depth>
        <url>/Dlna/%s/icons/logo120.png</url>
      </icon>
      <icon>
        <mimetype>image/svg+xml</mimetype>
        <width>120</width>
        <height>120</height>
        <depth>24</depth>
        <url>/Dlna/%s/icons/logo.svg</url>
      </icon>
    </iconList>
    <serviceList>
      <service>
        <serviceType>%s</serviceType>
        <serviceId>urn:upnp-org:serviceId:ContentDirectory</serviceId>
        <SCPDURL>/Dlna/%s/contentdirectory/contentdirectory.xml</SCPDURL>
        <controlURL>/Dlna/%s/contentdirectory/control</controlURL>
        <eventSubURL>/Dlna/%s/contentdirectory/events</eventSubURL>
      </service>
      <service>
        <serviceType>%s</serviceType>
        <serviceId>urn:upnp-org:serviceId:ConnectionManager</serviceId>
        <SCPDURL>/Dlna/%s/connectionmanager/connectionmanager.xml</SCPDURL>
        <controlURL>/Dlna/%s/connectionmanager/control</controlURL>
        <eventSubURL>/Dlna/%s/connectionmanager/events</eventSubURL>
      </service>
    </serviceList>
  </device>
</root>`,
		escapedName,
		xmlEscape(config.ProductName),
		xmlEscape(config.ServerVersion),
		escapedUUID,
		escapedUUID,
		escapedBase,
		escapedUUID,
		escapedUUID,
		contentDirectoryServiceType,
		escapedUUID,
		escapedUUID,
		escapedUUID,
		connectionManagerServiceType,
		escapedUUID,
		escapedUUID,
		escapedUUID,
	)
}

// --- SOAP Control Handlers ---

func (h *Handler) handleContentDirectoryControl(w http.ResponseWriter, r *http.Request) {
	if !h.allowSOAPRequest(r) {
		httpx.Error(w, http.StatusTooManyRequests, "too many DLNA SOAP requests")
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeSOAPFault(w, r, 501, "Action Failed")
		return
	}

	action, params, err := parseSOAPAction(r.Header.Get("SOAPACTION"), body)
	if err != nil {
		writeSOAPFault(w, r, 401, "Invalid Action")
		return
	}

	switch action {
	case "Browse":
		resp, err := h.handleBrowse(r.Context(), r, params)
		if err != nil {
			writeSOAPFault(w, r, 701, err.Error())
			return
		}
		writeXML(w, r, http.StatusOK, wrapSOAPResponse(contentDirectoryServiceType, "BrowseResponse", resp))
	case "Search":
		resp, err := h.handleSearch(r.Context(), r, params)
		if err != nil {
			writeSOAPFault(w, r, 708, err.Error())
			return
		}
		writeXML(w, r, http.StatusOK, wrapSOAPResponse(contentDirectoryServiceType, "SearchResponse", resp))
	case "GetSystemUpdateID":
		h.mu.RLock()
		uid := h.systemUpdate
		h.mu.RUnlock()
		writeXML(w, r, http.StatusOK, wrapSOAPResponse(contentDirectoryServiceType, "GetSystemUpdateIDResponse",
			fmt.Sprintf("<Id>%d</Id>", uid)))
	case "GetSearchCapabilities":
		writeXML(w, r, http.StatusOK, wrapSOAPResponse(contentDirectoryServiceType, "GetSearchCapabilitiesResponse",
			"<SearchCaps>dc:title,upnp:class</SearchCaps>"))
	case "GetSortCapabilities":
		writeXML(w, r, http.StatusOK, wrapSOAPResponse(contentDirectoryServiceType, "GetSortCapabilitiesResponse",
			"<SortCaps>dc:title,dc:date</SortCaps>"))
	default:
		writeSOAPFault(w, r, 401, "Invalid Action")
	}
}

func (h *Handler) handleConnectionManagerControl(w http.ResponseWriter, r *http.Request) {
	if !h.allowSOAPRequest(r) {
		httpx.Error(w, http.StatusTooManyRequests, "too many DLNA SOAP requests")
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeSOAPFault(w, r, 501, "Action Failed")
		return
	}

	action, _, err := parseSOAPAction(r.Header.Get("SOAPACTION"), body)
	if err != nil {
		writeSOAPFault(w, r, 401, "Invalid Action")
		return
	}

	switch action {
	case "GetProtocolInfo":
		inner := fmt.Sprintf("<Source>%s</Source><Sink></Sink>", xmlEscape(defaultProtocolInfoSource))
		writeXML(w, r, http.StatusOK, wrapSOAPResponse(connectionManagerServiceType, "GetProtocolInfoResponse", inner))
	case "GetCurrentConnectionIDs":
		writeXML(w, r, http.StatusOK, wrapSOAPResponse(connectionManagerServiceType, "GetCurrentConnectionIDsResponse",
			"<ConnectionIDs>0</ConnectionIDs>"))
	default:
		writeSOAPFault(w, r, 401, "Invalid Action")
	}
}

func clampRequestedCount(n int) int {
	if n <= 0 || n > maxDLNARequestedCount {
		return maxDLNARequestedCount
	}
	return n
}

func (h *Handler) isPublicPlaylist(ctx context.Context, itemID string) bool {
	if h.store == nil {
		return false
	}
	normID := guid.Normalize(itemID)
	if normID == "" {
		return false
	}
	var raw string
	err := h.store.Reader.QueryRowContext(ctx,
		`SELECT value_json FROM server_config WHERE key = ?`,
		"item_visibility_"+normID,
	).Scan(&raw)
	if err != nil {
		return false
	}
	var vis struct {
		IsPublic bool `json:"IsPublic"`
	}
	return json.Unmarshal([]byte(raw), &vis) == nil && vis.IsPublic
}

func (h *Handler) isDLNAVisibleItem(ctx context.Context, itemType, itemID string) bool {
	if strings.EqualFold(strings.TrimSpace(itemType), "Playlist") {
		return h.isPublicPlaylist(ctx, itemID)
	}
	return true
}

func (h *Handler) privatePlaylistIDs(ctx context.Context, _ string) []string {
	if h.store == nil {
		return nil
	}
	rows, err := h.store.Reader.QueryContext(ctx, `
		SELECT i.id, COALESCE(sc.value_json, '')
		FROM items i
		LEFT JOIN server_config sc
			ON sc.key = 'item_visibility_' || lower(replace(i.id, '-', ''))
		WHERE lower(i.type) = 'playlist'
	`)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var privateIDs []string
	for rows.Next() {
		var id, rawVis string
		if err := rows.Scan(&id, &rawVis); err != nil {
			continue
		}
		isPublic := false
		if rawVis != "" {
			var vis struct {
				IsPublic bool `json:"IsPublic"`
			}
			if json.Unmarshal([]byte(rawVis), &vis) == nil && vis.IsPublic {
				isPublic = true
			}
		}
		if !isPublic {
			privateIDs = append(privateIDs, id)
		}
	}
	return privateIDs
}

func (h *Handler) handleBrowse(ctx context.Context, r *http.Request, params map[string]string) (string, error) {
	objectID := strings.TrimSpace(params["ObjectID"])
	if objectID == "" {
		objectID = "0"
	}
	browseFlag := strings.TrimSpace(params["BrowseFlag"])
	if browseFlag == "" {
		browseFlag = "BrowseDirectChildren"
	}
	startIdx, _ := strconv.Atoi(strings.TrimSpace(params["StartingIndex"]))
	if startIdx < 0 {
		startIdx = 0
	}
	reqCount, _ := strconv.Atoi(strings.TrimSpace(params["RequestedCount"]))
	reqCount = clampRequestedCount(reqCount)
	sortCriteria := strings.TrimSpace(params["SortCriteria"])
	baseURL := h.baseURL(r)
	snap := h.cfgSnapshot()
	serverUUID := strings.TrimSpace(r.PathValue("UuId"))
	if serverUUID == "" {
		serverUUID = snap.ServerID
	}

	var didlEntries []string
	totalMatches := 0

	switch browseFlag {
	case "BrowseMetadata":
		entry, err := h.browseMetadata(ctx, objectID, baseURL, serverUUID)
		if err != nil {
			return "", err
		}
		didlEntries = append(didlEntries, entry)
		totalMatches = 1
	default:
		entries, total, err := h.browseDirectChildren(ctx, objectID, startIdx, reqCount, sortCriteria, baseURL, serverUUID)
		if err != nil {
			return "", err
		}
		didlEntries = entries
		totalMatches = total
	}

	didl := fmt.Sprintf(
		`<DIDL-Lite xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:upnp="urn:schemas-upnp-org:metadata-1-0/upnp/" xmlns="urn:schemas-upnp-org:metadata-1-0/DIDL-Lite/">%s</DIDL-Lite>`,
		strings.Join(didlEntries, ""),
	)

	h.mu.RLock()
	uid := h.systemUpdate
	h.mu.RUnlock()

	return fmt.Sprintf(
		"<Result>%s</Result><NumberReturned>%d</NumberReturned><TotalMatches>%d</TotalMatches><UpdateID>%d</UpdateID>",
		xmlEscape(didl), len(didlEntries), totalMatches, uid,
	), nil
}

var (
	reSearchTitle = regexp.MustCompile(`(?i)\bdc:title\s+(?:contains|=)\s+["']([^"']+)["']`)
	reSearchClass = regexp.MustCompile(`(?i)\bupnp:class\s+(?:derivedfrom|=)\s+["']([^"']+)["']`)
)

func (h *Handler) handleSearch(ctx context.Context, r *http.Request, params map[string]string) (string, error) {
	containerID := strings.TrimSpace(params["ContainerID"])
	if containerID == "" {
		containerID = "0"
	}
	searchCriteria := strings.TrimSpace(params["SearchCriteria"])
	startIdx, _ := strconv.Atoi(strings.TrimSpace(params["StartingIndex"]))
	if startIdx < 0 {
		startIdx = 0
	}
	reqCount, _ := strconv.Atoi(strings.TrimSpace(params["RequestedCount"]))
	reqCount = clampRequestedCount(reqCount)
	sortCriteria := strings.TrimSpace(params["SortCriteria"])
	baseURL := h.baseURL(r)
	snap := h.cfgSnapshot()
	serverUUID := strings.TrimSpace(r.PathValue("UuId"))
	if serverUUID == "" {
		serverUUID = snap.ServerID
	}

	var didlEntries []string
	totalMatches := 0

	if h.libRepo != nil {
		policy, denyAll := h.resolveDefaultUserPolicy(ctx)
		if denyAll {
			didl := `<DIDL-Lite xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:upnp="urn:schemas-upnp-org:metadata-1-0/upnp/" xmlns="urn:schemas-upnp-org:metadata-1-0/DIDL-Lite/"></DIDL-Lite>`
			h.mu.RLock()
			uid := h.systemUpdate
			h.mu.RUnlock()
			return fmt.Sprintf(
				"<Result>%s</Result><NumberReturned>0</NumberReturned><TotalMatches>0</TotalMatches><UpdateID>%d</UpdateID>",
				xmlEscape(didl), uid,
			), nil
		}
		if containerID != "0" {
			if rec, recErr := h.libRepo.GetItemRecordByID(ctx, containerID); recErr == nil && rec != nil {
				if !h.isDLNAVisibleItem(ctx, rec.Type, rec.ID) || !h.isItemAllowedByDefaultPolicy(ctx, rec) {
					didl := `<DIDL-Lite xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:upnp="urn:schemas-upnp-org:metadata-1-0/upnp/" xmlns="urn:schemas-upnp-org:metadata-1-0/DIDL-Lite/"></DIDL-Lite>`
					h.mu.RLock()
					uid := h.systemUpdate
					h.mu.RUnlock()
					return fmt.Sprintf(
						"<Result>%s</Result><NumberReturned>0</NumberReturned><TotalMatches>0</TotalMatches><UpdateID>%d</UpdateID>",
						xmlEscape(didl), uid,
					), nil
				}
			}
		}

		titleQuery, itemTypes := parseSearchCriteria(searchCriteria)
		sortBy, sortOrder := parseSortCriteria(sortCriteria)
		privatePlaylists := h.privatePlaylistIDs(ctx, snap.ServerID)

		filter := sqlite.ItemFilter{
			ServerID:         snap.ServerID,
			SearchTerm:       titleQuery,
			IncludeItemTypes: itemTypes,
			ExcludeItemIDs:   privatePlaylists,
			SortBy:           sortBy,
			SortOrder:        sortOrder,
			StartIndex:       startIdx,
			Limit:            reqCount,
		}
		if policy != nil {
			if !policy.EnableAllFolders {
				if len(policy.EnabledFolders) == 0 {
					didl := `<DIDL-Lite xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:upnp="urn:schemas-upnp-org:metadata-1-0/upnp/" xmlns="urn:schemas-upnp-org:metadata-1-0/DIDL-Lite/"></DIDL-Lite>`
					h.mu.RLock()
					uid := h.systemUpdate
					h.mu.RUnlock()
					return fmt.Sprintf(
						"<Result>%s</Result><NumberReturned>0</NumberReturned><TotalMatches>0</TotalMatches><UpdateID>%d</UpdateID>",
						xmlEscape(didl), uid,
					), nil
				}
				filter.AllowedLibraryIDs = policy.EnabledFolders
			}
			filter.MaxParentalRating = policy.MaxParentalRating
			filter.BlockedTags = policy.BlockedTags
		}
		if containerID != "0" {
			if _, libErr := h.libRepo.GetLibrary(ctx, containerID); libErr == nil {
				if !h.isLibraryAllowedByPolicy(containerID, policy) {
					didl := `<DIDL-Lite xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:upnp="urn:schemas-upnp-org:metadata-1-0/upnp/" xmlns="urn:schemas-upnp-org:metadata-1-0/DIDL-Lite/"></DIDL-Lite>`
					h.mu.RLock()
					uid := h.systemUpdate
					h.mu.RUnlock()
					return fmt.Sprintf(
						"<Result>%s</Result><NumberReturned>0</NumberReturned><TotalMatches>0</TotalMatches><UpdateID>%d</UpdateID>",
						xmlEscape(didl), uid,
					), nil
				}
				filter.LibraryID = containerID
			} else {
				filter.ParentID = containerID
				filter.Recursive = true
			}
		}

		items, total, err := h.libRepo.QueryItems(ctx, filter)
		if err != nil {
			return "", err
		}
		// Fallback to case-insensitive substring match on Name when FTS index is empty or misses mid-word substrings.
		if total == 0 && titleQuery != "" {
			fallbackFilter := filter
			fallbackFilter.SearchTerm = ""
			fallbackFilter.StartIndex = 0
			fallbackFilter.Limit = maxDLNARequestedCount * 2
			allItems, _, fbErr := h.libRepo.QueryItems(ctx, fallbackFilter)
			if fbErr != nil {
				return "", fbErr
			}
			needle := strings.ToLower(titleQuery)
			matched := make([]dto.BaseItemDto, 0, len(allItems))
			for _, it := range allItems {
				if !h.isDLNAVisibleItem(ctx, it.Type, it.Id) {
					continue
				}
				if strings.Contains(strings.ToLower(it.Name), needle) ||
					strings.Contains(strings.ToLower(it.OriginalTitle), needle) {
					matched = append(matched, it)
				}
			}
			total = len(matched)
			if startIdx >= total {
				items = nil
			} else {
				end := total
				if reqCount > 0 && startIdx+reqCount < end {
					end = startIdx + reqCount
				}
				items = matched[startIdx:end]
			}
		}
		didlEntries = make([]string, 0, len(items))
		for _, it := range items {
			if !h.isDLNAVisibleItem(ctx, it.Type, it.Id) {
				continue
			}
			didlEntries = append(didlEntries, h.itemToDIDL(it, baseURL, serverUUID))
		}
		totalMatches = total
	}

	didl := fmt.Sprintf(
		`<DIDL-Lite xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:upnp="urn:schemas-upnp-org:metadata-1-0/upnp/" xmlns="urn:schemas-upnp-org:metadata-1-0/DIDL-Lite/">%s</DIDL-Lite>`,
		strings.Join(didlEntries, ""),
	)

	h.mu.RLock()
	uid := h.systemUpdate
	h.mu.RUnlock()

	return fmt.Sprintf(
		"<Result>%s</Result><NumberReturned>%d</NumberReturned><TotalMatches>%d</TotalMatches><UpdateID>%d</UpdateID>",
		xmlEscape(didl), len(didlEntries), totalMatches, uid,
	), nil
}

func parseSearchCriteria(criteria string) (titleQuery string, itemTypes []string) {
	criteria = strings.TrimSpace(criteria)
	if criteria == "" || criteria == "*" {
		return "", nil
	}
	if m := reSearchTitle.FindStringSubmatch(criteria); len(m) == 2 {
		titleQuery = strings.TrimSpace(m[1])
	}
	if m := reSearchClass.FindStringSubmatch(criteria); len(m) == 2 {
		cls := strings.ToLower(strings.TrimSpace(m[1]))
		switch {
		case cls == "object.item.videoitem.movie":
			itemTypes = []string{"Movie"}
		case cls == "object.item.videoitem.videobroadcast":
			itemTypes = []string{"Episode"}
		case strings.HasPrefix(cls, "object.item.videoitem"):
			itemTypes = []string{"Movie", "Episode", "Video", "MusicVideo"}
		case strings.HasPrefix(cls, "object.item.audioitem"):
			itemTypes = []string{"Audio"}
		case strings.HasPrefix(cls, "object.item.imageitem"):
			itemTypes = []string{"Photo"}
		case cls == "object.container.album.musicalbum":
			itemTypes = []string{"MusicAlbum"}
		case strings.HasPrefix(cls, "object.container"):
			itemTypes = []string{"Folder", "Series", "Season", "MusicAlbum", "MusicArtist", "BoxSet"}
		}
	}
	return titleQuery, itemTypes
}

func parseSortCriteria(criteria string) (sortBy []string, sortOrder string) {
	criteria = strings.TrimSpace(criteria)
	if criteria == "" {
		return nil, ""
	}
	for _, token := range strings.Split(criteria, ",") {
		token = strings.TrimSpace(token)
		if token == "" {
			continue
		}
		dir := "Ascending"
		switch token[0] {
		case '+':
			dir = "Ascending"
			token = strings.TrimSpace(token[1:])
		case '-':
			dir = "Descending"
			token = strings.TrimSpace(token[1:])
		}
		switch strings.ToLower(token) {
		case "dc:title":
			return []string{"SortName"}, dir
		case "dc:date":
			return []string{"PremiereDate", "DateCreated"}, dir
		}
	}
	return nil, ""
}

func (h *Handler) browseMetadata(ctx context.Context, objectID, baseURL, serverUUID string) (string, error) {
	snap := h.cfgSnapshot()
	if objectID == "0" {
		return fmt.Sprintf(
			`<container id="0" parentID="-1" restricted="1"><dc:title>%s</dc:title><upnp:class>object.container.storageFolder</upnp:class></container>`,
			xmlEscape(snap.ServerName),
		), nil
	}
	if h.libRepo == nil {
		return "", fmt.Errorf("object not found")
	}
	policy, denyAll := h.resolveDefaultUserPolicy(ctx)
	if denyAll {
		return "", fmt.Errorf("object not found")
	}
	if lib, err := h.libRepo.GetLibrary(ctx, objectID); err == nil {
		if !h.isLibraryAllowedByPolicy(lib.ID, policy) {
			return "", fmt.Errorf("object not found")
		}
		return libraryToDIDL(*lib), nil
	}
	if rec, recErr := h.libRepo.GetItemRecordByID(ctx, objectID); recErr == nil && rec != nil {
		if !h.isItemAllowedByDefaultPolicy(ctx, rec) {
			return "", fmt.Errorf("object not found")
		}
	}
	item, err := h.libRepo.GetItemByID(ctx, snap.ServerID, "", objectID)
	if err != nil {
		return "", err
	}
	if !h.isDLNAVisibleItem(ctx, item.Type, item.Id) {
		return "", fmt.Errorf("object not found")
	}
	return h.itemToDIDL(*item, baseURL, serverUUID), nil
}

func (h *Handler) browseDirectChildren(ctx context.Context, objectID string, startIdx, reqCount int, sortCriteria, baseURL, serverUUID string) ([]string, int, error) {
	if h.libRepo == nil {
		return nil, 0, nil
	}

	policy, denyAll := h.resolveDefaultUserPolicy(ctx)
	if denyAll {
		return nil, 0, nil
	}

	snap := h.cfgSnapshot()
	reqCount = clampRequestedCount(reqCount)
	sortBy, sortOrder := parseSortCriteria(sortCriteria)

	if objectID == "0" {
		allLibs, err := h.libRepo.ListLibraries(ctx)
		if err != nil {
			return nil, 0, err
		}
		libs := make([]sqlite.LibraryRecord, 0, len(allLibs))
		for _, lib := range allLibs {
			if h.isLibraryAllowedByPolicy(lib.ID, policy) {
				libs = append(libs, lib)
			}
		}
		if len(sortBy) > 0 && sortBy[0] == "SortName" {
			desc := strings.EqualFold(sortOrder, "Descending")
			sort.Slice(libs, func(i, j int) bool {
				li, lj := strings.ToLower(libs[i].Name), strings.ToLower(libs[j].Name)
				if desc {
					return li > lj
				}
				return li < lj
			})
		}
		total := len(libs)
		if startIdx >= total {
			return nil, total, nil
		}
		end := total
		if reqCount > 0 && startIdx+reqCount < end {
			end = startIdx + reqCount
		}
		out := make([]string, 0, end-startIdx)
		for _, lib := range libs[startIdx:end] {
			out = append(out, libraryToDIDL(lib))
		}
		return out, total, nil
	}

	if _, libErr := h.libRepo.GetLibrary(ctx, objectID); libErr == nil && !h.isLibraryAllowedByPolicy(objectID, policy) {
		return nil, 0, fmt.Errorf("object not found")
	}
	if rec, recErr := h.libRepo.GetItemRecordByID(ctx, objectID); recErr == nil && rec != nil {
		if !h.isDLNAVisibleItem(ctx, rec.Type, rec.ID) || !h.isItemAllowedByDefaultPolicy(ctx, rec) {
			return nil, 0, fmt.Errorf("object not found")
		}
	}
	privatePlaylists := h.privatePlaylistIDs(ctx, snap.ServerID)

	baseFilter := sqlite.ItemFilter{
		ServerID:       snap.ServerID,
		ParentID:       objectID,
		ExcludeItemIDs: privatePlaylists,
		SortBy:         sortBy,
		SortOrder:      sortOrder,
		StartIndex:     startIdx,
		Limit:          reqCount,
	}
	if policy != nil {
		if !policy.EnableAllFolders {
			if len(policy.EnabledFolders) == 0 {
				return nil, 0, nil
			}
			baseFilter.AllowedLibraryIDs = policy.EnabledFolders
		}
		baseFilter.MaxParentalRating = policy.MaxParentalRating
		baseFilter.BlockedTags = policy.BlockedTags
	}

	items, total, err := h.libRepo.QueryItems(ctx, baseFilter)
	if err != nil {
		return nil, 0, err
	}
	// Fallback: if browsing a library root whose top-level items only set library_id
	if total == 0 {
		if _, libErr := h.libRepo.GetLibrary(ctx, objectID); libErr == nil {
			libFilter := baseFilter
			libFilter.ParentID = ""
			libFilter.LibraryID = objectID
			items, total, err = h.libRepo.QueryItems(ctx, libFilter)
			if err != nil {
				return nil, 0, err
			}
		}
	}

	out := make([]string, 0, len(items))
	for _, it := range items {
		if !h.isDLNAVisibleItem(ctx, it.Type, it.Id) {
			continue
		}
		out = append(out, h.itemToDIDL(it, baseURL, serverUUID))
	}
	return out, total, nil
}

func libraryToDIDL(lib sqlite.LibraryRecord) string {
	return fmt.Sprintf(
		`<container id="%s" parentID="0" restricted="1"><dc:title>%s</dc:title><upnp:class>object.container.storageFolder</upnp:class></container>`,
		xmlEscape(lib.ID),
		xmlEscape(lib.Name),
	)
}

func (h *Handler) itemToDIDL(it dto.BaseItemDto, baseURL, serverUUID string) string {
	parentID := it.ParentId
	if parentID == "" {
		parentID = "0"
	}
	if it.IsFolder {
		upnpClass := "object.container.storageFolder"
		if strings.EqualFold(it.Type, "MusicAlbum") {
			upnpClass = "object.container.album.musicAlbum"
		}
		return fmt.Sprintf(
			`<container id="%s" parentID="%s" restricted="1"><dc:title>%s</dc:title><upnp:class>%s</upnp:class></container>`,
			xmlEscape(it.Id),
			xmlEscape(parentID),
			xmlEscape(it.Name),
			upnpClass,
		)
	}

	upnpClass, mimeType, _ := mediaMetadataForItem(it)
	if strings.TrimSpace(serverUUID) == "" {
		serverUUID = h.cfgSnapshot().ServerID
	}
	exp := time.Now().UTC().Add(dlnaStreamTokenTTL).Unix()
	sig := h.signStreamToken(it.Id, exp)
	resURL := fmt.Sprintf("%s/Dlna/%s/stream/%s?exp=%d&sig=%s", baseURL, url.PathEscape(serverUUID), url.PathEscape(it.Id), exp, sig)
	attrs := fmt.Sprintf(`protocolInfo="http-get:*:%s:*"`, mimeType)
	if it.RunTimeTicks != nil && *it.RunTimeTicks > 0 {
		attrs += fmt.Sprintf(` duration="%s"`, formatUPnPDuration(*it.RunTimeTicks))
	}
	if len(it.MediaSources) > 0 && it.MediaSources[0].Size > 0 {
		attrs += fmt.Sprintf(` size="%d"`, it.MediaSources[0].Size)
	}

	return fmt.Sprintf(
		`<item id="%s" parentID="%s" restricted="1"><dc:title>%s</dc:title><upnp:class>%s</upnp:class><res %s>%s</res></item>`,
		xmlEscape(it.Id),
		xmlEscape(parentID),
		xmlEscape(it.Name),
		upnpClass,
		attrs,
		xmlEscape(resURL),
	)
}

func mediaMetadataForItem(it dto.BaseItemDto) (upnpClass, mimeType, streamPath string) {
	container := strings.ToLower(strings.TrimSpace(it.Container))
	switch strings.ToLower(it.MediaType) {
	case "audio":
		upnpClass = "object.item.audioItem.musicTrack"
		mimeType = audioMimeType(container)
		streamPath = fmt.Sprintf("/Audio/%s/stream?static=true", it.Id)
	case "photo":
		upnpClass = "object.item.imageItem.photo"
		mimeType = "image/jpeg"
		streamPath = fmt.Sprintf("/Items/%s/Images/Primary", it.Id)
	default:
		upnpClass = "object.item.videoItem.movie"
		if strings.EqualFold(it.Type, "Episode") {
			upnpClass = "object.item.videoItem.videoBroadcast"
		}
		mimeType = videoMimeType(container)
		streamPath = fmt.Sprintf("/Videos/%s/stream?static=true", it.Id)
	}
	return upnpClass, mimeType, streamPath
}

func videoMimeType(container string) string {
	switch container {
	case "mkv":
		return "video/x-matroska"
	case "webm":
		return "video/webm"
	case "mov":
		return "video/quicktime"
	case "avi":
		return "video/x-msvideo"
	default:
		return "video/mp4"
	}
}

func audioMimeType(container string) string {
	switch container {
	case "flac":
		return "audio/flac"
	case "aac", "m4a":
		return "audio/mp4"
	case "ogg", "opus":
		return "audio/ogg"
	case "wav":
		return "audio/wav"
	default:
		return "audio/mpeg"
	}
}

func formatUPnPDuration(ticks int64) string {
	totalSeconds := ticks / 10_000_000
	hours := totalSeconds / 3600
	minutes := (totalSeconds % 3600) / 60
	seconds := totalSeconds % 60
	return fmt.Sprintf("%d:%02d:%02d", hours, minutes, seconds)
}

// parseSOAPAction extracts the UPnP action name and child parameter map from SOAPACTION header and XML envelope.
func parseSOAPAction(soapActionHeader string, body []byte) (string, map[string]string, error) {
	action := ""
	header := strings.Trim(strings.TrimSpace(soapActionHeader), `"`)
	if idx := strings.LastIndex(header, "#"); idx >= 0 && idx+1 < len(header) {
		action = strings.TrimSpace(header[idx+1:])
	}

	params := make(map[string]string)
	if len(bytes.TrimSpace(body)) > 0 {
		dec := xml.NewDecoder(bytes.NewReader(body))
		inBody := false
		bodyAction := ""
		depth := 0
		for {
			tok, err := dec.Token()
			if err == io.EOF {
				break
			}
			if err != nil {
				return "", nil, err
			}
			switch t := tok.(type) {
			case xml.StartElement:
				if !inBody {
					if strings.EqualFold(t.Name.Local, "Body") {
						inBody = true
					}
					continue
				}
				if bodyAction == "" {
					bodyAction = t.Name.Local
					depth = 1
					continue
				}
				if depth == 1 {
					var val string
					if err := dec.DecodeElement(&val, &t); err == nil {
						params[t.Name.Local] = val
					}
					continue
				}
				depth++
			case xml.EndElement:
				if inBody && bodyAction != "" {
					if t.Name.Local == bodyAction {
						inBody = false
					} else if depth > 1 {
						depth--
					}
				}
			}
		}
		if action == "" {
			action = bodyAction
		}
	}

	if action == "" {
		return "", nil, errUnknownAction
	}
	return action, params, nil
}

func wrapSOAPResponse(serviceType, responseTag, innerXML string) string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="utf-8"?>
<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/" s:encodingStyle="http://schemas.xmlsoap.org/soap/encoding/">
  <s:Body>
    <u:%s xmlns:u="%s">%s</u:%s>
  </s:Body>
</s:Envelope>`, responseTag, serviceType, innerXML, responseTag)
}

func writeSOAPFault(w http.ResponseWriter, r *http.Request, code int, desc string) {
	fault := fmt.Sprintf(`<?xml version="1.0" encoding="utf-8"?>
<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/" s:encodingStyle="http://schemas.xmlsoap.org/soap/encoding/">
  <s:Body>
    <s:Fault>
      <faultcode>s:Client</faultcode>
      <faultstring>UPnPError</faultstring>
      <detail>
        <UPnPError xmlns="urn:schemas-upnp-org:control-1-0">
          <errorCode>%d</errorCode>
          <errorDescription>%s</errorDescription>
        </UPnPError>
      </detail>
    </s:Fault>
  </s:Body>
</s:Envelope>`, code, xmlEscape(desc))
	writeXML(w, r, http.StatusInternalServerError, fault)
}

const contentDirectorySCPDXML = `<?xml version="1.0" encoding="utf-8"?>
<scpd xmlns="urn:schemas-upnp-org:service-1-0">
  <specVersion><major>1</major><minor>0</minor></specVersion>
  <actionList>
    <action>
      <name>Browse</name>
      <argumentList>
        <argument><name>ObjectID</name><direction>in</direction><relatedStateVariable>A_ARG_TYPE_ObjectID</relatedStateVariable></argument>
        <argument><name>BrowseFlag</name><direction>in</direction><relatedStateVariable>A_ARG_TYPE_BrowseFlag</relatedStateVariable></argument>
        <argument><name>Filter</name><direction>in</direction><relatedStateVariable>A_ARG_TYPE_Filter</relatedStateVariable></argument>
        <argument><name>StartingIndex</name><direction>in</direction><relatedStateVariable>A_ARG_TYPE_Index</relatedStateVariable></argument>
        <argument><name>RequestedCount</name><direction>in</direction><relatedStateVariable>A_ARG_TYPE_Count</relatedStateVariable></argument>
        <argument><name>SortCriteria</name><direction>in</direction><relatedStateVariable>A_ARG_TYPE_SortCriteria</relatedStateVariable></argument>
        <argument><name>Result</name><direction>out</direction><relatedStateVariable>A_ARG_TYPE_Result</relatedStateVariable></argument>
        <argument><name>NumberReturned</name><direction>out</direction><relatedStateVariable>A_ARG_TYPE_Count</relatedStateVariable></argument>
        <argument><name>TotalMatches</name><direction>out</direction><relatedStateVariable>A_ARG_TYPE_Count</relatedStateVariable></argument>
        <argument><name>UpdateID</name><direction>out</direction><relatedStateVariable>A_ARG_TYPE_UpdateID</relatedStateVariable></argument>
      </argumentList>
    </action>
    <action>
      <name>Search</name>
      <argumentList>
        <argument><name>ContainerID</name><direction>in</direction><relatedStateVariable>A_ARG_TYPE_ObjectID</relatedStateVariable></argument>
        <argument><name>SearchCriteria</name><direction>in</direction><relatedStateVariable>A_ARG_TYPE_SearchCriteria</relatedStateVariable></argument>
        <argument><name>Filter</name><direction>in</direction><relatedStateVariable>A_ARG_TYPE_Filter</relatedStateVariable></argument>
        <argument><name>StartingIndex</name><direction>in</direction><relatedStateVariable>A_ARG_TYPE_Index</relatedStateVariable></argument>
        <argument><name>RequestedCount</name><direction>in</direction><relatedStateVariable>A_ARG_TYPE_Count</relatedStateVariable></argument>
        <argument><name>SortCriteria</name><direction>in</direction><relatedStateVariable>A_ARG_TYPE_SortCriteria</relatedStateVariable></argument>
        <argument><name>Result</name><direction>out</direction><relatedStateVariable>A_ARG_TYPE_Result</relatedStateVariable></argument>
        <argument><name>NumberReturned</name><direction>out</direction><relatedStateVariable>A_ARG_TYPE_Count</relatedStateVariable></argument>
        <argument><name>TotalMatches</name><direction>out</direction><relatedStateVariable>A_ARG_TYPE_Count</relatedStateVariable></argument>
        <argument><name>UpdateID</name><direction>out</direction><relatedStateVariable>A_ARG_TYPE_UpdateID</relatedStateVariable></argument>
      </argumentList>
    </action>
    <action>
      <name>GetSystemUpdateID</name>
      <argumentList>
        <argument><name>Id</name><direction>out</direction><relatedStateVariable>SystemUpdateID</relatedStateVariable></argument>
      </argumentList>
    </action>
    <action>
      <name>GetSearchCapabilities</name>
      <argumentList>
        <argument><name>SearchCaps</name><direction>out</direction><relatedStateVariable>SearchCapabilities</relatedStateVariable></argument>
      </argumentList>
    </action>
    <action>
      <name>GetSortCapabilities</name>
      <argumentList>
        <argument><name>SortCaps</name><direction>out</direction><relatedStateVariable>SortCapabilities</relatedStateVariable></argument>
      </argumentList>
    </action>
  </actionList>
  <serviceStateTable>
    <stateVariable sendEvents="yes"><name>SystemUpdateID</name><dataType>ui4</dataType></stateVariable>
    <stateVariable sendEvents="no"><name>SearchCapabilities</name><dataType>string</dataType></stateVariable>
    <stateVariable sendEvents="no"><name>SortCapabilities</name><dataType>string</dataType></stateVariable>
    <stateVariable sendEvents="no"><name>A_ARG_TYPE_ObjectID</name><dataType>string</dataType></stateVariable>
    <stateVariable sendEvents="no"><name>A_ARG_TYPE_BrowseFlag</name><dataType>string</dataType></stateVariable>
    <stateVariable sendEvents="no"><name>A_ARG_TYPE_SearchCriteria</name><dataType>string</dataType></stateVariable>
    <stateVariable sendEvents="no"><name>A_ARG_TYPE_Filter</name><dataType>string</dataType></stateVariable>
    <stateVariable sendEvents="no"><name>A_ARG_TYPE_Index</name><dataType>ui4</dataType></stateVariable>
    <stateVariable sendEvents="no"><name>A_ARG_TYPE_Count</name><dataType>ui4</dataType></stateVariable>
    <stateVariable sendEvents="no"><name>A_ARG_TYPE_SortCriteria</name><dataType>string</dataType></stateVariable>
    <stateVariable sendEvents="no"><name>A_ARG_TYPE_Result</name><dataType>string</dataType></stateVariable>
    <stateVariable sendEvents="no"><name>A_ARG_TYPE_UpdateID</name><dataType>ui4</dataType></stateVariable>
  </serviceStateTable>
</scpd>`

const connectionManagerSCPDXML = `<?xml version="1.0" encoding="utf-8"?>
<scpd xmlns="urn:schemas-upnp-org:service-1-0">
  <specVersion><major>1</major><minor>0</minor></specVersion>
  <actionList>
    <action>
      <name>GetProtocolInfo</name>
      <argumentList>
        <argument><name>Source</name><direction>out</direction><relatedStateVariable>SourceProtocolInfo</relatedStateVariable></argument>
        <argument><name>Sink</name><direction>out</direction><relatedStateVariable>SinkProtocolInfo</relatedStateVariable></argument>
      </argumentList>
    </action>
    <action>
      <name>GetCurrentConnectionIDs</name>
      <argumentList>
        <argument><name>ConnectionIDs</name><direction>out</direction><relatedStateVariable>CurrentConnectionIDs</relatedStateVariable></argument>
      </argumentList>
    </action>
  </actionList>
  <serviceStateTable>
    <stateVariable sendEvents="yes"><name>SourceProtocolInfo</name><dataType>string</dataType></stateVariable>
    <stateVariable sendEvents="yes"><name>SinkProtocolInfo</name><dataType>string</dataType></stateVariable>
    <stateVariable sendEvents="yes"><name>CurrentConnectionIDs</name><dataType>string</dataType></stateVariable>
  </serviceStateTable>
</scpd>`
