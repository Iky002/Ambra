package compatcore

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/go-chi/chi/v5"

	"github.com/ambra-media/ambra/internal/api/dto"
	"github.com/ambra-media/ambra/internal/api/httpx"
	"github.com/ambra-media/ambra/internal/api/middleware"
	"github.com/ambra-media/ambra/internal/core/auth"
	"github.com/ambra-media/ambra/internal/core/session"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

// --- User DTO helper ---

func (h *Handler) toUserDto(u *sqlite.UserRecord) dto.UserDto {
	hasPw := u.PasswordHash != ""
	var dataDir string
	if h.cfg != nil {
		dataDir = h.cfg.DataDir
	}
	return dto.UserDto{
		Name:                      u.Name,
		ServerId:                  h.serverID(),
		Id:                        u.ID,
		PrimaryImageTag:           auth.UserPrimaryImageTag(dataDir, u.ID),
		HasPassword:               hasPw,
		HasConfiguredPassword:     hasPw,
		HasConfiguredEasyPassword: false,
		LastLoginDate:             u.LastLoginAt,
		LastActivityDate:          u.LastActiveAt,
		Configuration:             u.Config,
		Policy:                    u.Policy,
	}
}

// --- /Users public & management ---

func (h *Handler) forgotPassword(w http.ResponseWriter, r *http.Request) {
	var body struct {
		EnteredUsername string `json:"EnteredUsername"`
	}
	_ = decodeOptionalJSON(r, &body)

	pinBytes := make([]byte, 3)
	_, _ = rand.Read(pinBytes)
	pin := strings.ToUpper(hex.EncodeToString(pinBytes))
	expiresAt := time.Now().UTC().Add(30 * time.Minute)

	uid := ""
	uname := strings.TrimSpace(body.EnteredUsername)
	if h.userRepo != nil && uname != "" {
		if u, err := h.userRepo.GetUserByName(r.Context(), uname); err == nil && u != nil {
			uid = u.ID
			uname = u.Name
		}
	}

	h.mu.Lock()
	h.forgotPins[pin] = forgotPinEntry{
		UserID:    uid,
		UserName:  uname,
		Pin:       pin,
		ExpiresAt: expiresAt,
	}
	h.mu.Unlock()

	pinFile := ""
	if h.cfg != nil && h.cfg.DataDir != "" {
		pinFile = filepath.Join(h.cfg.DataDir, "passwordreset-"+pin+".txt")
	}

	httpx.JSON(w, http.StatusOK, map[string]any{
		"Action":            "PinCode",
		"PinFile":           pinFile,
		"PinExpirationDate": expiresAt.Format(time.RFC3339),
	})
}

func (h *Handler) forgotPasswordPin(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Pin string `json:"Pin"`
	}
	_ = decodeOptionalJSON(r, &body)
	pin := strings.ToUpper(strings.TrimSpace(body.Pin))

	var resetUsers []string
	success := false
	h.mu.Lock()
	if entry, ok := h.forgotPins[pin]; ok && time.Now().UTC().Before(entry.ExpiresAt) {
		delete(h.forgotPins, pin)
		success = true
		if entry.UserName != "" {
			resetUsers = append(resetUsers, entry.UserName)
		}
	}
	h.mu.Unlock()
	if resetUsers == nil {
		resetUsers = []string{}
	}

	httpx.JSON(w, http.StatusOK, map[string]any{
		"Success":    success,
		"UsersReset": resetUsers,
	})
}

func (h *Handler) authenticateUserByID(w http.ResponseWriter, r *http.Request) {
	uid := h.resolveUserID(r, "Id")
	if h.userRepo == nil || uid == "" {
		writeJSONError(w, r, http.StatusNotFound, "user not found")
		return
	}
	var body struct {
		Pw       string `json:"Pw"`
		Password string `json:"Password"`
	}
	if err := decodeOptionalJSON(r, &body); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}
	pw := body.Pw
	if pw == "" && body.Password != "" {
		pw = body.Password
	}

	u, err := h.userRepo.GetUserByID(r.Context(), uid)
	if err != nil {
		writeJSONError(w, r, http.StatusNotFound, "user not found")
		return
	}
	if u.IsDisabled || u.Policy.IsDisabled {
		writeJSONError(w, r, http.StatusForbidden, "user is disabled")
		return
	}

	ok, err := auth.VerifyPassword(pw, u.PasswordHash)
	if err != nil || !ok {
		writeJSONError(w, r, http.StatusUnauthorized, "invalid username or password")
		return
	}

	client := middleware.ExtractClientInfo(r)
	if client.DeviceID == "" {
		client.DeviceID = guid.New()
	}
	tokenBytes := make([]byte, 16)
	_, _ = rand.Read(tokenBytes)
	token := hex.EncodeToString(tokenBytes)

	if err := h.userRepo.CreateAccessToken(r.Context(), &sqlite.AccessTokenRecord{
		Token:      token,
		UserID:     u.ID,
		DeviceID:   client.DeviceID,
		DeviceName: client.Device,
		AppName:    client.Client,
		AppVersion: client.Version,
	}); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "failed to create access token")
		return
	}
	_ = h.userRepo.UpdateLastLogin(r.Context(), u.ID, time.Now().UTC())

	var sessInfo dto.SessionInfo
	if h.sessions != nil {
		sessInfo = h.sessions.TouchSession(u, client)
	}

	httpx.JSON(w, http.StatusOK, dto.AuthenticationResult{
		User:        h.toUserDto(u),
		SessionInfo: sessInfo,
		AccessToken: token,
		ServerId:    h.serverID(),
	})
}

func (h *Handler) queryUsers(w http.ResponseWriter, r *http.Request) {
	q := httpx.ParseQuery(r)
	startIndex := q.Int("StartIndex", 0)
	limit := q.Int("Limit", 0)
	nameStartsWith := strings.ToLower(strings.TrimSpace(q.Get("NameStartsWithOrGreater")))

	var all []*sqlite.UserRecord
	if h.userRepo != nil {
		list, err := h.userRepo.ListUsers(r.Context())
		if err != nil {
			httpx.Error(w, http.StatusInternalServerError, "failed to query users")
			return
		}
		all = list
	}

	filtered := make([]dto.UserDto, 0, len(all))
	for _, u := range all {
		if nameStartsWith != "" && strings.ToLower(u.Name) < nameStartsWith {
			continue
		}
		filtered = append(filtered, h.toUserDto(u))
	}

	total := len(filtered)
	if startIndex < 0 {
		startIndex = 0
	}
	if startIndex > total {
		startIndex = total
	}
	end := total
	if limit > 0 && startIndex+limit < end {
		end = startIndex + limit
	}

	httpx.JSON(w, http.StatusOK, dto.QueryResult[dto.UserDto]{
		Items:            filtered[startIndex:end],
		TotalRecordCount: total,
	})
}

func (h *Handler) getUserPrefixes(w http.ResponseWriter, r *http.Request) {
	seen := map[string]bool{}
	if h.userRepo != nil {
		if list, err := h.userRepo.ListUsers(r.Context()); err == nil {
			for _, u := range list {
				runes := []rune(strings.TrimSpace(u.Name))
				if len(runes) == 0 {
					continue
				}
				ch := unicode.ToUpper(runes[0])
				if unicode.IsLetter(ch) {
					seen[string(ch)] = true
				} else {
					seen["#"] = true
				}
			}
		}
	}
	keys := make([]string, 0, len(seen))
	for k := range seen {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]map[string]string, 0, len(keys))
	for _, k := range keys {
		out = append(out, map[string]string{"Name": k})
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (h *Handler) updateUser(w http.ResponseWriter, r *http.Request) {
	uid := h.resolveUserID(r, "Id")
	if !h.canModifyUser(r, uid) {
		writeJSONError(w, r, http.StatusForbidden, "forbidden")
		return
	}
	if h.userRepo == nil || uid == "" {
		writeJSONError(w, r, http.StatusNotFound, "user not found")
		return
	}
	u, err := h.userRepo.GetUserByID(r.Context(), uid)
	if err != nil {
		writeJSONError(w, r, http.StatusNotFound, "user not found")
		return
	}

	var body struct {
		Name          string                 `json:"Name"`
		Configuration *dto.UserConfiguration `json:"Configuration"`
		Policy        *dto.UserPolicy        `json:"Policy"`
	}
	if err := decodeOptionalJSON(r, &body); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if body.Policy != nil && !h.isCallerAdmin(r) {
		writeJSONError(w, r, http.StatusForbidden, "admin privileges required to update user policy")
		return
	}
	if body.Name != "" && body.Name != u.Name && h.store != nil && h.store.Writer != nil {
		_, _ = h.store.Writer.ExecContext(r.Context(), `UPDATE users SET name = ? WHERE id = ?`, strings.TrimSpace(body.Name), u.ID)
	}
	if body.Configuration != nil {
		_ = h.userRepo.UpdateUserConfig(r.Context(), u.ID, *body.Configuration)
	}
	if body.Policy != nil {
		_ = h.userRepo.UpdateUserPolicy(r.Context(), u.ID, *body.Policy)
	}
	httpx.NoContent(w)
}

func (h *Handler) deleteUser(w http.ResponseWriter, r *http.Request) {
	uid := h.resolveUserID(r, "Id")
	if h.userRepo == nil || uid == "" {
		writeJSONError(w, r, http.StatusNotFound, "user not found")
		return
	}
	if err := h.userRepo.DeleteUser(r.Context(), uid); err != nil {
		if errors.Is(err, sqlite.ErrNotFound) {
			writeJSONError(w, r, http.StatusNotFound, "user not found")
			return
		}
		httpx.Error(w, http.StatusInternalServerError, "failed to delete user")
		return
	}
	httpx.NoContent(w)
}

func (h *Handler) updateUserConfigurationPartial(w http.ResponseWriter, r *http.Request) {
	uid := h.resolveUserID(r, "Id")
	if !h.canModifyUser(r, uid) {
		writeJSONError(w, r, http.StatusForbidden, "forbidden")
		return
	}
	if h.userRepo == nil || uid == "" {
		writeJSONError(w, r, http.StatusNotFound, "user not found")
		return
	}
	u, err := h.userRepo.GetUserByID(r.Context(), uid)
	if err != nil {
		writeJSONError(w, r, http.StatusNotFound, "user not found")
		return
	}

	var patch map[string]any
	if err := decodeOptionalJSON(r, &patch); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if len(patch) > 0 {
		rawCfg, _ := json.Marshal(u.Config)
		merged := map[string]any{}
		_ = json.Unmarshal(rawCfg, &merged)
		for k, v := range patch {
			merged[k] = v
		}
		updatedRaw, _ := json.Marshal(merged)
		var nextCfg dto.UserConfiguration
		_ = json.Unmarshal(updatedRaw, &nextCfg)
		_ = h.userRepo.UpdateUserConfig(r.Context(), u.ID, nextCfg)
	}
	httpx.NoContent(w)
}

func (h *Handler) copyUserData(w http.ResponseWriter, r *http.Request) {
	srcUID := h.resolveUserID(r, "UserId")
	var body struct {
		CopyFromUserId string   `json:"CopyFromUserId"`
		ToUserIds      []string `json:"ToUserIds"`
		CopyOptions    []string `json:"CopyOptions"`
	}
	if err := decodeOptionalJSON(r, &body); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if body.CopyFromUserId != "" {
		srcUID = guid.Normalize(body.CopyFromUserId)
	}
	if h.userRepo != nil && srcUID != "" {
		if src, err := h.userRepo.GetUserByID(r.Context(), srcUID); err == nil && src != nil {
			for _, target := range body.ToUserIds {
				tid := guid.Normalize(target)
				if tid == "" || tid == src.ID {
					continue
				}
				_ = h.userRepo.UpdateUserConfig(r.Context(), tid, src.Config)
			}
		}
	}
	httpx.NoContent(w)
}

func (h *Handler) deleteUserTrackSelections(w http.ResponseWriter, r *http.Request) {
	uid := h.resolveUserID(r, "Id")
	trackType := strings.ToLower(strings.TrimSpace(chi.URLParam(r, "TrackType")))
	if h.store != nil && h.store.Writer != nil && uid != "" {
		switch trackType {
		case "audio":
			_, _ = h.store.Writer.ExecContext(r.Context(), `UPDATE user_item_data SET audio_stream_index = NULL WHERE user_id = ?`, uid)
		case "subtitle":
			_, _ = h.store.Writer.ExecContext(r.Context(), `UPDATE user_item_data SET subtitle_stream_index = NULL WHERE user_id = ?`, uid)
		default:
			_, _ = h.store.Writer.ExecContext(r.Context(), `UPDATE user_item_data SET audio_stream_index = NULL, subtitle_stream_index = NULL WHERE user_id = ?`, uid)
		}
	}
	httpx.NoContent(w)
}

func (h *Handler) deleteUserRecentlySearched(w http.ResponseWriter, r *http.Request) {
	uid := h.resolveUserID(r, "Id")
	_ = h.deleteKV(r.Context(), "user_searched_items:"+uid)
	httpx.NoContent(w)
}

func (h *Handler) postUserSearchedItems(w http.ResponseWriter, r *http.Request) {
	uid := h.resolveUserID(r, "UserId")
	var payload map[string]any
	_ = decodeOptionalJSON(r, &payload)
	key := "user_searched_items:" + uid
	var items []map[string]any
	_, _ = h.loadKV(r.Context(), key, &items)
	if payload == nil {
		payload = map[string]any{}
	}
	payload["SearchedAt"] = time.Now().UTC().Format(time.RFC3339)
	items = append(items, payload)
	if len(items) > 50 {
		items = items[len(items)-50:]
	}
	_ = h.saveKV(r.Context(), key, items)
	httpx.NoContent(w)
}

func (h *Handler) getUserTypedSettings(w http.ResponseWriter, r *http.Request) {
	uid := h.resolveUserID(r, "UserId")
	settingKey := strings.TrimSpace(chi.URLParam(r, "Key"))
	out := map[string]any{}
	_, _ = h.loadKV(r.Context(), fmt.Sprintf("user_typed_settings:%s:%s", uid, settingKey), &out)
	httpx.JSON(w, http.StatusOK, out)
}

func (h *Handler) postUserTypedSettings(w http.ResponseWriter, r *http.Request) {
	uid := h.resolveUserID(r, "UserId")
	settingKey := strings.TrimSpace(chi.URLParam(r, "Key"))
	body := map[string]any{}
	if err := decodeOptionalJSON(r, &body); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}
	_ = h.saveKV(r.Context(), fmt.Sprintf("user_typed_settings:%s:%s", uid, settingKey), body)
	httpx.NoContent(w)
}

// --- HomeSections ---

type homeSection struct {
	Id          string `json:"Id"`
	Name        string `json:"Name"`
	SectionType string `json:"SectionType"`
	SortOrder   int    `json:"SortOrder"`
}

func defaultHomeSections() []homeSection {
	return []homeSection{
		{Id: "smalllibrarytiles", Name: "My Media", SectionType: "LibraryTiles", SortOrder: 0},
		{Id: "resume", Name: "Continue Watching", SectionType: "Resume", SortOrder: 1},
		{Id: "latestmedia", Name: "Latest Media", SectionType: "LatestMedia", SortOrder: 2},
	}
}

func (h *Handler) getUserHomeSections(w http.ResponseWriter, r *http.Request) {
	uid := h.resolveUserID(r, "UserId")
	var sections []homeSection
	found, _ := h.loadKV(r.Context(), "user_home_sections:"+uid, &sections)
	if !found || len(sections) == 0 {
		sections = defaultHomeSections()
	}
	httpx.JSON(w, http.StatusOK, sections)
}

func (h *Handler) postUserHomeSections(w http.ResponseWriter, r *http.Request) {
	uid := h.resolveUserID(r, "UserId")
	var sec homeSection
	if err := decodeOptionalJSON(r, &sec); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}
	var sections []homeSection
	found, _ := h.loadKV(r.Context(), "user_home_sections:"+uid, &sections)
	if !found {
		sections = defaultHomeSections()
	}
	if sec.Id == "" {
		sec.Id = guid.New()
	}
	if sec.Name == "" {
		sec.Name = "Custom Section"
	}
	replaced := false
	for i := range sections {
		if sections[i].Id == sec.Id {
			sections[i] = sec
			replaced = true
			break
		}
	}
	if !replaced {
		sec.SortOrder = len(sections)
		sections = append(sections, sec)
	}
	_ = h.saveKV(r.Context(), "user_home_sections:"+uid, sections)
	httpx.JSON(w, http.StatusOK, sec)
}

func (h *Handler) deleteUserHomeSections(w http.ResponseWriter, r *http.Request) {
	uid := h.resolveUserID(r, "UserId")
	q := httpx.ParseQuery(r)
	ids := q.List("Ids")
	var body struct {
		Ids []string `json:"Ids"`
	}
	_ = decodeOptionalJSON(r, &body)
	ids = append(ids, body.Ids...)

	removeSet := make(map[string]bool, len(ids))
	for _, id := range ids {
		removeSet[id] = true
	}

	var sections []homeSection
	found, _ := h.loadKV(r.Context(), "user_home_sections:"+uid, &sections)
	if !found {
		sections = defaultHomeSections()
	}
	next := make([]homeSection, 0, len(sections))
	for _, s := range sections {
		if !removeSet[s.Id] {
			next = append(next, s)
		}
	}
	_ = h.saveKV(r.Context(), "user_home_sections:"+uid, next)
	httpx.NoContent(w)
}

func (h *Handler) moveUserHomeSection(w http.ResponseWriter, r *http.Request) {
	uid := h.resolveUserID(r, "UserId")
	q := httpx.ParseQuery(r)
	var body struct {
		Id       string `json:"Id"`
		NewIndex int    `json:"NewIndex"`
	}
	_ = decodeOptionalJSON(r, &body)
	if body.Id == "" {
		body.Id = q.Get("Id")
	}
	if q.Has("NewIndex") {
		body.NewIndex = q.Int("NewIndex", body.NewIndex)
	}

	var sections []homeSection
	found, _ := h.loadKV(r.Context(), "user_home_sections:"+uid, &sections)
	if !found {
		sections = defaultHomeSections()
	}
	idx := -1
	for i, s := range sections {
		if s.Id == body.Id {
			idx = i
			break
		}
	}
	if idx >= 0 {
		item := sections[idx]
		sections = append(sections[:idx], sections[idx+1:]...)
		newIdx := body.NewIndex
		if newIdx < 0 {
			newIdx = 0
		}
		if newIdx > len(sections) {
			newIdx = len(sections)
		}
		sections = append(sections[:newIdx], append([]homeSection{item}, sections[newIdx:]...)...)
		for i := range sections {
			sections[i].SortOrder = i
		}
		_ = h.saveKV(r.Context(), "user_home_sections:"+uid, sections)
	}
	httpx.NoContent(w)
}

func (h *Handler) getUserSectionItems(w http.ResponseWriter, r *http.Request) {
	uid := h.resolveUserID(r, "UserId")
	q := httpx.ParseQuery(r)
	limit := q.Int("Limit", 20)
	if h.libRepo == nil {
		httpx.JSON(w, http.StatusOK, dto.QueryResult[dto.BaseItemDto]{Items: []dto.BaseItemDto{}, TotalRecordCount: 0})
		return
	}
	items, total, err := h.libRepo.QueryItems(r.Context(), sqlite.ItemFilter{
		ServerID:   h.serverID(),
		UserID:     uid,
		Recursive:  true,
		StartIndex: q.Int("StartIndex", 0),
		Limit:      limit,
	})
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "failed to query section items")
		return
	}
	if items == nil {
		items = []dto.BaseItemDto{}
	}
	httpx.JSON(w, http.StatusOK, dto.QueryResult[dto.BaseItemDto]{Items: items, TotalRecordCount: total})
}

func (h *Handler) getUserSuggestions(w http.ResponseWriter, r *http.Request) {
	uid := h.resolveUserID(r, "UserId")
	q := httpx.ParseQuery(r)
	if h.libRepo == nil {
		httpx.JSON(w, http.StatusOK, dto.QueryResult[dto.BaseItemDto]{Items: []dto.BaseItemDto{}, TotalRecordCount: 0})
		return
	}
	items, total, err := h.libRepo.QueryItems(r.Context(), sqlite.ItemFilter{
		ServerID:         h.serverID(),
		UserID:           uid,
		Recursive:        true,
		IncludeItemTypes: q.List("Type"),
		StartIndex:       q.Int("StartIndex", 0),
		Limit:            q.Int("Limit", 20),
	})
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "failed to load suggestions")
		return
	}
	if items == nil {
		items = []dto.BaseItemDto{}
	}
	httpx.JSON(w, http.StatusOK, dto.QueryResult[dto.BaseItemDto]{Items: items, TotalRecordCount: total})
}

func (h *Handler) getUserGroupingOptions(w http.ResponseWriter, r *http.Request) {
	out := []dto.NameIdPair{}
	if h.libRepo != nil {
		if libs, err := h.libRepo.ListLibraries(r.Context()); err == nil {
			for _, lib := range libs {
				out = append(out, dto.NameIdPair{Name: lib.Name, Id: lib.ID})
			}
		}
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (h *Handler) getUserRootItem(w http.ResponseWriter, r *http.Request) {
	httpx.JSON(w, http.StatusOK, dto.BaseItemDto{
		Name:              "Media Folders",
		ServerId:          h.serverID(),
		Id:                guid.Normalize("00000000-0000-0000-0000-000000000001"),
		IsFolder:          true,
		Type:              "UserRootFolder",
		ImageTags:         map[string]string{},
		BackdropImageTags: []string{},
	})
}

func (h *Handler) getUserItemIntros(w http.ResponseWriter, r *http.Request) {
	httpx.JSON(w, http.StatusOK, dto.QueryResult[dto.BaseItemDto]{
		Items:            []dto.BaseItemDto{},
		TotalRecordCount: 0,
	})
}

func (h *Handler) getUserItemLocalTrailers(w http.ResponseWriter, r *http.Request) {
	httpx.JSON(w, http.StatusOK, []dto.BaseItemDto{})
}

func (h *Handler) getUserItemSpecialFeatures(w http.ResponseWriter, r *http.Request) {
	httpx.JSON(w, http.StatusOK, []dto.BaseItemDto{})
}

// --- User Item Data / Rating / Favorite / Played / PlayingItems ---

func (h *Handler) deleteUserFavoriteItem(w http.ResponseWriter, r *http.Request) {
	uid := h.resolveUserID(r, "UserId")
	itemID := guid.Normalize(chi.URLParam(r, "Id"))
	if h.libRepo != nil && uid != "" && itemID != "" {
		if ud, err := h.libRepo.UnmarkFavorite(r.Context(), uid, itemID); err == nil && ud != nil {
			httpx.JSON(w, http.StatusOK, ud)
			return
		}
	}
	httpx.JSON(w, http.StatusOK, dto.UserItemDataDto{IsFavorite: false, Key: itemID})
}

func (h *Handler) postUserItemRating(w http.ResponseWriter, r *http.Request) {
	uid := h.resolveUserID(r, "UserId")
	itemID := guid.Normalize(chi.URLParam(r, "Id"))
	q := httpx.ParseQuery(r)
	likes := q.Bool("Likes")
	val := true
	if likes != nil {
		val = *likes
	}
	_ = h.saveKV(r.Context(), fmt.Sprintf("user_rating:%s:%s", uid, itemID), map[string]any{"Likes": val})

	ud := dto.UserItemDataDto{Key: itemID}
	if h.libRepo != nil && uid != "" && itemID != "" {
		if existing, err := h.libRepo.GetUserItemData(r.Context(), uid, itemID); err == nil && existing != nil {
			ud = *existing
		}
	}
	httpx.JSON(w, http.StatusOK, ud)
}

func (h *Handler) deleteUserItemRating(w http.ResponseWriter, r *http.Request) {
	uid := h.resolveUserID(r, "UserId")
	itemID := guid.Normalize(chi.URLParam(r, "Id"))
	_ = h.deleteKV(r.Context(), fmt.Sprintf("user_rating:%s:%s", uid, itemID))

	ud := dto.UserItemDataDto{Key: itemID}
	if h.libRepo != nil && uid != "" && itemID != "" {
		if existing, err := h.libRepo.GetUserItemData(r.Context(), uid, itemID); err == nil && existing != nil {
			ud = *existing
		}
	}
	httpx.JSON(w, http.StatusOK, ud)
}

func (h *Handler) postUserItemHideFromResume(w http.ResponseWriter, r *http.Request) {
	uid := h.resolveUserID(r, "UserId")
	itemID := guid.Normalize(chi.URLParam(r, "Id"))
	q := httpx.ParseQuery(r)
	hide := true
	if b := q.Bool("Hide"); b != nil {
		hide = *b
	}
	_ = h.saveKV(r.Context(), fmt.Sprintf("user_hide_resume:%s:%s", uid, itemID), map[string]any{"Hide": hide})
	if hide && h.userRepo != nil && uid != "" && itemID != "" {
		_, _ = h.userRepo.UpsertPlaybackProgress(r.Context(), uid, itemID, 0, false, nil, nil)
	}
	ud := dto.UserItemDataDto{Key: itemID}
	if h.libRepo != nil && uid != "" && itemID != "" {
		if existing, err := h.libRepo.GetUserItemData(r.Context(), uid, itemID); err == nil && existing != nil {
			ud = *existing
		}
	}
	httpx.JSON(w, http.StatusOK, ud)
}

func (h *Handler) postUserItemUserData(w http.ResponseWriter, r *http.Request) {
	uid := h.resolveUserID(r, "UserId")
	if !h.canModifyUser(r, uid) {
		writeJSONError(w, r, http.StatusForbidden, "forbidden")
		return
	}
	itemID := guid.Normalize(chi.URLParam(r, "ItemId"))
	var body struct {
		Played                *bool  `json:"Played"`
		IsFavorite            *bool  `json:"IsFavorite"`
		PlaybackPositionTicks *int64 `json:"PlaybackPositionTicks"`
	}
	if err := decodeOptionalJSON(r, &body); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if h.libRepo != nil && uid != "" && itemID != "" {
		if body.Played != nil {
			if *body.Played {
				_, _ = h.libRepo.MarkPlayed(r.Context(), uid, itemID)
			} else {
				_, _ = h.libRepo.MarkUnplayed(r.Context(), uid, itemID)
			}
		}
		if body.IsFavorite != nil {
			if *body.IsFavorite {
				_, _ = h.libRepo.MarkFavorite(r.Context(), uid, itemID)
			} else {
				_, _ = h.libRepo.UnmarkFavorite(r.Context(), uid, itemID)
			}
		}
	}
	if body.PlaybackPositionTicks != nil && h.userRepo != nil && uid != "" && itemID != "" {
		_, _ = h.userRepo.UpsertPlaybackProgress(r.Context(), uid, itemID, *body.PlaybackPositionTicks, false, nil, nil)
	}
	httpx.NoContent(w)
}

func (h *Handler) deleteUserPlayedItem(w http.ResponseWriter, r *http.Request) {
	uid := h.resolveUserID(r, "UserId")
	itemID := guid.Normalize(chi.URLParam(r, "Id"))
	if h.libRepo != nil && uid != "" && itemID != "" {
		if ud, err := h.libRepo.MarkUnplayed(r.Context(), uid, itemID); err == nil && ud != nil {
			httpx.JSON(w, http.StatusOK, ud)
			return
		}
	}
	httpx.JSON(w, http.StatusOK, dto.UserItemDataDto{Played: false, Key: itemID})
}

func (h *Handler) postUserPlayingItemStart(w http.ResponseWriter, r *http.Request) {
	uid := h.resolveUserID(r, "UserId")
	itemID := guid.Normalize(chi.URLParam(r, "Id"))
	q := httpx.ParseQuery(r)
	if h.sessions != nil && h.userRepo != nil && uid != "" {
		if u, err := h.userRepo.GetUserByID(r.Context(), uid); err == nil && u != nil {
			client := middleware.ExtractClientInfo(r)
			_ = h.sessions.ReportPlaybackStart(r.Context(), u, client, session.PlaybackStartRequest{
				ItemId:        itemID,
				MediaSourceId: q.Get("MediaSourceId"),
				PlaySessionId: q.Get("PlaySessionId"),
				PlayMethod:    q.Get("PlayMethod"),
				PositionTicks: q.Int64("PositionTicks", 0),
				CanSeek:       q.Bool("CanSeek") != nil && *q.Bool("CanSeek"),
			})
		}
	}
	httpx.NoContent(w)
}

func (h *Handler) postUserPlayingItemProgress(w http.ResponseWriter, r *http.Request) {
	uid := h.resolveUserID(r, "UserId")
	itemID := guid.Normalize(chi.URLParam(r, "Id"))
	q := httpx.ParseQuery(r)
	if h.sessions != nil && h.userRepo != nil && uid != "" {
		if u, err := h.userRepo.GetUserByID(r.Context(), uid); err == nil && u != nil {
			client := middleware.ExtractClientInfo(r)
			isPaused := q.Bool("IsPaused") != nil && *q.Bool("IsPaused")
			isMuted := q.Bool("IsMuted") != nil && *q.Bool("IsMuted")
			_ = h.sessions.ReportPlaybackProgress(r.Context(), u, client, session.PlaybackProgressRequest{
				ItemId:        itemID,
				MediaSourceId: q.Get("MediaSourceId"),
				PlaySessionId: q.Get("PlaySessionId"),
				PlayMethod:    q.Get("PlayMethod"),
				PositionTicks: q.Int64("PositionTicks", 0),
				IsPaused:      isPaused,
				IsMuted:       isMuted,
				VolumeLevel:   q.Int("VolumeLevel", 100),
				EventName:     q.Get("EventName"),
			})
		}
	}
	httpx.NoContent(w)
}

func (h *Handler) deleteUserPlayingItem(w http.ResponseWriter, r *http.Request) {
	uid := h.resolveUserID(r, "UserId")
	itemID := guid.Normalize(chi.URLParam(r, "Id"))
	q := httpx.ParseQuery(r)
	if h.sessions != nil && h.userRepo != nil && uid != "" {
		if u, err := h.userRepo.GetUserByID(r.Context(), uid); err == nil && u != nil {
			client := middleware.ExtractClientInfo(r)
			_ = h.sessions.ReportPlaybackStopped(r.Context(), u, client, session.PlaybackStoppedRequest{
				ItemId:        itemID,
				MediaSourceId: q.Get("MediaSourceId"),
				PlaySessionId: q.Get("PlaySessionId"),
				PositionTicks: q.Int64("PositionTicks", 0),
			})
		}
	}
	httpx.NoContent(w)
}

// --- User Images ---

func (h *Handler) userImagePath(uid, imgType, idx string) string {
	baseDir := os.TempDir()
	if h.cfg != nil && h.cfg.DataDir != "" {
		baseDir = filepath.Join(h.cfg.DataDir, "metadata", "users")
	}
	if idx == "" {
		idx = "0"
	}
	safeType := sanitizeFilename(strings.ToLower(imgType))
	safeIdx := sanitizeFilename(idx)
	safeUID := sanitizeFilename(uid)
	return filepath.Join(baseDir, safeUID, safeType+"_"+safeIdx+".img")
}

func sanitizeFilename(s string) string {
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		}
	}
	if b.Len() == 0 {
		return "default"
	}
	return b.String()
}

func (h *Handler) getUserImage(w http.ResponseWriter, r *http.Request) {
	h.serveUserImage(w, r, false)
}

func (h *Handler) headUserImage(w http.ResponseWriter, r *http.Request) {
	h.serveUserImage(w, r, true)
}

func isSafeRasterImageType(ct string) bool {
	switch strings.ToLower(strings.TrimSpace(ct)) {
	case "image/jpeg", "image/png", "image/gif", "image/webp":
		return true
	default:
		return false
	}
}

func (h *Handler) serveUserImage(w http.ResponseWriter, r *http.Request, headOnly bool) {
	uid := h.resolveUserID(r, "Id")
	imgType := chi.URLParam(r, "Type")
	idx := chi.URLParam(r, "Index")
	p := h.userImagePath(uid, imgType, idx)

	data, err := os.ReadFile(p)
	if err != nil {
		writeJSONError(w, r, http.StatusNotFound, "user image not found")
		return
	}
	ct := http.DetectContentType(data)
	if !isSafeRasterImageType(ct) {
		ct = "application/octet-stream"
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "sandbox")
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(data)))
	w.WriteHeader(http.StatusOK)
	if !headOnly {
		_, _ = w.Write(data)
	}
}

func (h *Handler) isCallerAdmin(r *http.Request) bool {
	if ac, ok := middleware.AuthFromContext(r.Context()); ok && ac != nil {
		return ac.IsAdmin()
	}
	if h.auth.UserIDFromCtx != nil {
		callerID := guid.Normalize(h.auth.UserIDFromCtx(r.Context()))
		if callerID != "" && h.userRepo != nil {
			if u, err := h.userRepo.GetUserByID(r.Context(), callerID); err == nil && u != nil {
				return u.IsAdmin || u.Policy.IsAdministrator
			}
		}
	}
	return false
}

func (h *Handler) canModifyUser(r *http.Request, targetUID string) bool {
	targetID := guid.Normalize(targetUID)
	if ac, ok := middleware.AuthFromContext(r.Context()); ok && ac != nil {
		return ac.IsAdmin() || (targetID != "" && guid.Normalize(ac.UserID()) == targetID)
	}
	if h.auth.UserIDFromCtx != nil {
		callerID := guid.Normalize(h.auth.UserIDFromCtx(r.Context()))
		if callerID == "" {
			return false
		}
		if callerID == targetID {
			return true
		}
		if h.userRepo != nil {
			if u, err := h.userRepo.GetUserByID(r.Context(), callerID); err == nil && u != nil {
				return u.IsAdmin || u.Policy.IsAdministrator
			}
		}
	}
	return false
}

func (h *Handler) postUserImage(w http.ResponseWriter, r *http.Request) {
	uid := h.resolveUserID(r, "Id")
	if !h.canModifyUser(r, uid) {
		writeJSONError(w, r, http.StatusForbidden, "forbidden")
		return
	}
	imgType := chi.URLParam(r, "Type")
	idx := chi.URLParam(r, "Index")
	p := h.userImagePath(uid, imgType, idx)

	raw, err := io.ReadAll(io.LimitReader(r.Body, 10<<20))
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "failed to read image body")
		return
	}
	// Emby clients often POST base64-encoded image payloads.
	if decoded, decErr := base64.StdEncoding.DecodeString(strings.TrimSpace(string(raw))); decErr == nil && len(decoded) > 0 {
		raw = decoded
	}
	if len(raw) == 0 || !isSafeRasterImageType(http.DetectContentType(raw)) {
		httpx.Error(w, http.StatusBadRequest, "unsupported image format")
		return
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "failed to prepare user image directory")
		return
	}
	if err := os.WriteFile(p, raw, 0o644); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "failed to save user image")
		return
	}
	httpx.NoContent(w)
}

func (h *Handler) deleteUserImage(w http.ResponseWriter, r *http.Request) {
	uid := h.resolveUserID(r, "Id")
	if !h.canModifyUser(r, uid) {
		writeJSONError(w, r, http.StatusForbidden, "forbidden")
		return
	}
	imgType := chi.URLParam(r, "Type")
	idx := chi.URLParam(r, "Index")
	p := h.userImagePath(uid, imgType, idx)
	_ = os.Remove(p)
	httpx.NoContent(w)
}

// --- User Connect Link ---

func (h *Handler) postUserConnectLink(w http.ResponseWriter, r *http.Request) {
	uid := h.resolveUserID(r, "Id")
	q := httpx.ParseQuery(r)
	connectUser := q.Get("ConnectUsername")
	var body struct {
		ConnectUsername string `json:"ConnectUsername"`
	}
	_ = decodeOptionalJSON(r, &body)
	if body.ConnectUsername != "" {
		connectUser = body.ConnectUsername
	}
	_ = h.saveKV(r.Context(), "user_connect_link:"+uid, map[string]any{
		"UserId":          uid,
		"ConnectUsername": connectUser,
		"LinkedAt":        time.Now().UTC().Format(time.RFC3339),
	})
	httpx.JSON(w, http.StatusOK, map[string]any{
		"GuestDisplayName": connectUser,
		"IsPending":        false,
		"IsNewUserInvitation": false,
	})
}

func (h *Handler) deleteUserConnectLink(w http.ResponseWriter, r *http.Request) {
	uid := h.resolveUserID(r, "Id")
	_ = h.deleteKV(r.Context(), "user_connect_link:"+uid)
	httpx.NoContent(w)
}

// --- /UserSettings ---

func (h *Handler) getUserSettings(w http.ResponseWriter, r *http.Request) {
	uid := h.resolveUserID(r, "UserId")
	out := map[string]any{}
	_, _ = h.loadKV(r.Context(), "user_settings:"+uid, &out)
	httpx.JSON(w, http.StatusOK, out)
}

func (h *Handler) postUserSettings(w http.ResponseWriter, r *http.Request) {
	uid := h.resolveUserID(r, "UserId")
	if !h.canModifyUser(r, uid) {
		writeJSONError(w, r, http.StatusForbidden, "forbidden")
		return
	}
	body := map[string]any{}
	if err := decodeOptionalJSON(r, &body); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}
	_ = h.saveKV(r.Context(), "user_settings:"+uid, body)
	httpx.NoContent(w)
}

func (h *Handler) postUserSettingsPartial(w http.ResponseWriter, r *http.Request) {
	uid := h.resolveUserID(r, "UserId")
	if !h.canModifyUser(r, uid) {
		writeJSONError(w, r, http.StatusForbidden, "forbidden")
		return
	}
	patch := map[string]any{}
	if err := decodeOptionalJSON(r, &patch); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}
	current := map[string]any{}
	_, _ = h.loadKV(r.Context(), "user_settings:"+uid, &current)
	for k, v := range patch {
		current[k] = v
	}
	_ = h.saveKV(r.Context(), "user_settings:"+uid, current)
	httpx.NoContent(w)
}
