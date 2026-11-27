package library

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"

	"github.com/ambra-media/ambra/internal/api/dto"
	"github.com/ambra-media/ambra/internal/api/httpx"
	"github.com/ambra-media/ambra/internal/api/middleware"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func computeSyntheticLibraryCoverTag(libraryID, childTag string) string {
	normLib := guid.Normalize(libraryID)
	childTag = strings.TrimSpace(childTag)
	if normLib == "" || childTag == "" {
		return ""
	}
	sum := sha256.Sum256([]byte("ambra-libcover:" + normLib + ":" + childTag))
	return hex.EncodeToString(sum[:8])
}

type extendedParentalPolicy struct {
	maxRating         *int
	blockedTags       []string
	blockUnratedItems []string
	allowedTags       []string
}

func (p extendedParentalPolicy) hasExtraPolicy() bool {
	return len(p.blockUnratedItems) > 0 || len(p.allowedTags) > 0
}

const maxQueryItemsLimit = 5000

func clampQueryLimit(limit, defaultLimit, maxLimit int) int {
	if limit <= 0 {
		if defaultLimit > 0 {
			return defaultLimit
		}
		return maxLimit
	}
	if maxLimit > 0 && limit > maxLimit {
		return maxLimit
	}
	return limit
}

func (h *Handler) userPolicyForRequest(r *http.Request, fallbackUserID string) dto.UserPolicy {
	if ac, ok := middleware.AuthFromContext(r.Context()); ok && ac != nil {
		if ac.IsAPIKey || ac.IsAdmin() {
			return dto.UserPolicy{
				IsAdministrator:    true,
				EnableAllFolders:   true,
				EnableLiveTvAccess: true,
			}
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
	if h.userRepo != nil && uid != "" {
		u, err := h.userRepo.GetUserByID(r.Context(), uid)
		if err != nil || u == nil {
			zero := 0
			return dto.UserPolicy{
				EnableAllFolders:   false,
				EnabledFolders:     []string{},
				EnableLiveTvAccess: false,
				MaxParentalRating:  &zero,
			}
		}
		if u.IsAdmin || u.Policy.IsAdministrator {
			return dto.UserPolicy{
				IsAdministrator:    true,
				EnableAllFolders:   true,
				EnableLiveTvAccess: true,
			}
		}
		return u.Policy
	}
	if ac, ok := middleware.AuthFromContext(r.Context()); ok && ac != nil && ac.User != nil {
		if ac.User.IsAdmin || ac.User.Policy.IsAdministrator {
			return dto.UserPolicy{
				IsAdministrator:    true,
				EnableAllFolders:   true,
				EnableLiveTvAccess: true,
			}
		}
		return ac.User.Policy
	}
	return dto.UserPolicy{
		IsAdministrator:    true,
		EnableAllFolders:   true,
		EnableLiveTvAccess: true,
	}
}

func (h *Handler) playlistViewerForRequest(r *http.Request) (string, bool) {
	if ac, ok := middleware.AuthFromContext(r.Context()); ok && ac != nil && (ac.User != nil || ac.IsAPIKey) {
		if ac.IsAdmin() {
			return "", true
		}
		return guid.Normalize(ac.UserID()), false
	}
	if h.auth.UserIDFromCtx != nil {
		callerID := guid.Normalize(h.auth.UserIDFromCtx(r.Context()))
		if callerID != "" {
			if h.userRepo != nil {
				if u, err := h.userRepo.GetUserByID(r.Context(), callerID); err == nil && u != nil && (u.IsAdmin || u.Policy.IsAdministrator) {
					return callerID, true
				}
			}
			return callerID, false
		}
	}
	return "", true
}

func (h *Handler) userExtendedPolicyForRequest(r *http.Request, fallbackUserID string) extendedParentalPolicy {
	pol := h.userPolicyForRequest(r, fallbackUserID)
	if pol.IsAdministrator {
		return extendedParentalPolicy{}
	}
	return extendedParentalPolicy{
		maxRating:         pol.MaxParentalRating,
		blockedTags:       pol.BlockedTags,
		blockUnratedItems: pol.BlockUnratedItems,
		allowedTags:       pol.AllowedTags,
	}
}

func matchesBlockUnratedType(itemType, mediaType string, blockUnrated []string) bool {
	if strings.EqualFold(itemType, "CollectionFolder") ||
		strings.EqualFold(itemType, "Folder") ||
		strings.EqualFold(itemType, "UserView") ||
		strings.EqualFold(itemType, "Person") ||
		strings.EqualFold(itemType, "Genre") ||
		strings.EqualFold(itemType, "MusicGenre") ||
		strings.EqualFold(itemType, "GameGenre") ||
		strings.EqualFold(itemType, "Studio") {
		return false
	}
	for _, b := range blockUnrated {
		t := strings.TrimSpace(b)
		if t == "" {
			continue
		}
		if t == "*" || strings.EqualFold(t, "All") || strings.EqualFold(t, itemType) || (mediaType != "" && strings.EqualFold(t, mediaType)) {
			return true
		}
		if strings.EqualFold(t, "Series") && (strings.EqualFold(itemType, "Season") || strings.EqualFold(itemType, "Episode")) {
			return true
		}
		if strings.EqualFold(t, "Music") && (strings.EqualFold(itemType, "Audio") || strings.EqualFold(itemType, "MusicAlbum") || strings.EqualFold(itemType, "MusicArtist") || strings.EqualFold(mediaType, "Audio")) {
			return true
		}
	}
	return false
}

func (h *Handler) isRecordBlockedByExtraPolicy(ctx context.Context, rec *sqlite.ItemRecord, pol extendedParentalPolicy) bool {
	if rec == nil || h.repo == nil || !pol.hasExtraPolicy() {
		return false
	}
	if len(pol.blockUnratedItems) > 0 && matchesBlockUnratedType(rec.Type, rec.MediaType, pol.blockUnratedItems) {
		rating := strings.TrimSpace(rec.OfficialRating)
		if rating == "" && rec.SeriesID != "" {
			if sRec, err := h.repo.GetItemRecordByID(ctx, rec.SeriesID); err == nil && sRec != nil {
				rating = strings.TrimSpace(sRec.OfficialRating)
			}
		}
		if rating == "" {
			return true
		}
	}
	if len(pol.allowedTags) > 0 {
		allowSet := normalizeLowerSet(pol.allowedTags)
		if len(allowSet) > 0 && !h.recordHasAnyTag(ctx, rec.ID, rec.SeriesID, allowSet) {
			return true
		}
	}
	return false
}

func (h *Handler) recordHasAnyTag(ctx context.Context, itemID, seriesID string, allowSet map[string]bool) bool {
	checkID := func(id string) bool {
		norm := guid.Normalize(id)
		if norm == "" {
			return false
		}
		store := h.store
		if store == nil && h.userRepo != nil {
			store = h.userRepo.Store()
		}
		if store != nil {
			rows, err := store.Reader.QueryContext(ctx, `
				SELECT LOWER(t.name) FROM item_taxonomies it
				JOIN taxonomies t ON t.id = it.taxonomy_id
				WHERE it.item_id = ? AND LOWER(t.kind) = 'tag'`, norm)
			if err == nil {
				defer rows.Close()
				for rows.Next() {
					var name string
					if err := rows.Scan(&name); err == nil && allowSet[strings.TrimSpace(name)] {
						return true
					}
				}
				return false
			}
		}
		if h.repo != nil {
			if dtoItem, err := h.repo.GetItemByID(ctx, "", "", norm); err == nil && dtoItem != nil {
				for _, tg := range dtoItem.Tags {
					if allowSet[strings.ToLower(strings.TrimSpace(tg))] {
						return true
					}
				}
			}
		}
		return false
	}
	if checkID(itemID) {
		return true
	}
	if seriesID != "" && checkID(seriesID) {
		return true
	}
	return false
}

func normalizeLowerSet(vals []string) map[string]bool {
	if len(vals) == 0 {
		return nil
	}
	out := make(map[string]bool, len(vals))
	for _, v := range vals {
		if s := strings.ToLower(strings.TrimSpace(v)); s != "" {
			out[s] = true
		}
	}
	return out
}

func ensureFields(fields []string, required ...string) []string {
	existing := make(map[string]bool, len(fields))
	out := append([]string(nil), fields...)
	for _, f := range fields {
		existing[strings.ToLower(strings.TrimSpace(f))] = true
	}
	for _, req := range required {
		if !existing[strings.ToLower(req)] {
			out = append(out, req)
			existing[strings.ToLower(req)] = true
		}
	}
	return out
}

func parseVideoRangeAndHDRQuery(q httpx.Query, splitMulti func([]string) []string) (isHDR *bool, videoRanges []string, videoCodecs []string) {
	isHDR = q.Bool("IsHDR")
	videoRanges = append(videoRanges, splitMulti(q.List("VideoRanges"))...)
	videoRanges = append(videoRanges, splitMulti(q.List("ExtendedVideoTypes"))...)
	for _, vt := range splitMulti(q.List("VideoTypes")) {
		low := strings.ToLower(vt)
		switch low {
		case "hdr", "hdr10", "hdr10plus", "hdr10+", "hlg", "dovi", "dolbyvision", "sdr":
			videoRanges = append(videoRanges, vt)
		}
	}
	videoCodecs = append(videoCodecs, splitMulti(q.List("VideoCodecs"))...)
	videoCodecs = append(videoCodecs, splitMulti(q.List("VideoCodec"))...)
	videoCodecs = append(videoCodecs, splitMulti(q.List("Codec"))...)
	return isHDR, videoRanges, videoCodecs
}

func isHDRStreamRange(vr string) bool {
	u := strings.ToUpper(strings.TrimSpace(vr))
	if u == "" || u == "SDR" {
		return false
	}
	for _, kw := range []string{"HDR", "HLG", "DOVI", "DOLBY", "PQ", "BT2020", "SMPTEST2084", "ARIB"} {
		if strings.Contains(u, kw) {
			return true
		}
	}
	return false
}

func streamMatchesRangeToken(streamRange, token string) bool {
	sr := strings.ToUpper(strings.TrimSpace(streamRange))
	tk := strings.ToUpper(strings.TrimSpace(token))
	switch tk {
	case "SDR":
		return sr == "" || sr == "SDR"
	case "HDR":
		return isHDRStreamRange(sr)
	case "DOVI", "DOLBYVISION":
		return strings.Contains(sr, "DOVI") || strings.Contains(sr, "DOLBY")
	case "HDR10PLUS", "HDR10+":
		return strings.Contains(sr, "HDR10+") || strings.Contains(sr, "HDR10PLUS")
	default:
		return sr != "" && (sr == tk || strings.Contains(sr, tk))
	}
}

func normalizeCodecToken(c string) string {
	switch strings.ToLower(strings.TrimSpace(c)) {
	case "h265", "x265", "hevc":
		return "hevc"
	case "avc", "x264", "h264":
		return "h264"
	default:
		return strings.ToLower(strings.TrimSpace(c))
	}
}

func (h *Handler) filterDTOItemsPost(
	ctx context.Context,
	userID string,
	items []dto.BaseItemDto,
	pol extendedParentalPolicy,
	isLiked *bool,
	isHDR *bool,
	videoRanges []string,
	videoCodecs []string,
) []dto.BaseItemDto {
	if len(items) == 0 {
		return items
	}
	allowSet := normalizeLowerSet(pol.allowedTags)
	codecSet := make(map[string]bool, len(videoCodecs))
	for _, c := range videoCodecs {
		if nc := normalizeCodecToken(c); nc != "" {
			codecSet[nc] = true
		}
	}

	out := make([]dto.BaseItemDto, 0, len(items))
	for _, it := range items {
		// 1. BlockUnratedItems
		if len(pol.blockUnratedItems) > 0 && matchesBlockUnratedType(it.Type, it.MediaType, pol.blockUnratedItems) {
			rating := strings.TrimSpace(it.OfficialRating)
			if rating == "" && it.SeriesId != "" && h.repo != nil {
				if sRec, err := h.repo.GetItemRecordByID(ctx, it.SeriesId); err == nil && sRec != nil {
					rating = strings.TrimSpace(sRec.OfficialRating)
				}
			}
			if rating == "" {
				continue
			}
		}

		// 2. AllowedTags
		if len(allowSet) > 0 {
			matched := false
			for _, tg := range it.Tags {
				if allowSet[strings.ToLower(strings.TrimSpace(tg))] {
					matched = true
					break
				}
			}
			if !matched && h.recordHasAnyTag(ctx, it.Id, it.SeriesId, allowSet) {
				matched = true
			}
			if !matched {
				continue
			}
		}

		// 3. IsLiked
		if isLiked != nil {
			var likedPtr *bool
			if it.UserData != nil && it.UserData.Likes != nil {
				likedPtr = it.UserData.Likes
			} else if h.userRepo != nil && userID != "" {
				if ud, err := h.userRepo.GetUserItemData(ctx, userID, it.Id); err == nil && ud != nil {
					likedPtr = ud.Likes
				}
			}
			if likedPtr == nil || *likedPtr != *isLiked {
				continue
			}
		}

		// 4. Video streams checks (IsHDR / VideoRanges / VideoCodecs)
		if isHDR != nil || len(videoRanges) > 0 || len(codecSet) > 0 {
			streams := it.MediaStreams
			if len(streams) == 0 {
				for _, ms := range it.MediaSources {
					if len(ms.MediaStreams) > 0 {
						streams = ms.MediaStreams
						break
					}
				}
			}
			if len(streams) == 0 && h.repo != nil {
				if srcs, err := h.repo.GetMediaSourcesByItemID(ctx, it.Id); err == nil && len(srcs) > 0 {
					msDTOs := sqlite.MediaSourceRecordsToDTO(srcs)
					if len(msDTOs) > 0 {
						streams = msDTOs[0].MediaStreams
					}
				}
			}

			if isHDR != nil {
				foundHDR := false
				hasVideo := false
				for _, st := range streams {
					if strings.EqualFold(st.Type, "Video") {
						hasVideo = true
						if isHDRStreamRange(st.VideoRange) {
							foundHDR = true
							break
						}
					}
				}
				if *isHDR && !foundHDR {
					continue
				}
				if !*isHDR && (!hasVideo || foundHDR) {
					continue
				}
			}

			if len(videoRanges) > 0 {
				rangeOK := false
				for _, st := range streams {
					if !strings.EqualFold(st.Type, "Video") {
						continue
					}
					for _, vr := range videoRanges {
						if streamMatchesRangeToken(st.VideoRange, vr) {
							rangeOK = true
							break
						}
					}
					if rangeOK {
						break
					}
				}
				if !rangeOK {
					continue
				}
			}

			if len(codecSet) > 0 {
				codecOK := false
				for _, st := range streams {
					if strings.EqualFold(st.Type, "Video") && codecSet[normalizeCodecToken(st.Codec)] {
						codecOK = true
						break
					}
				}
				if !codecOK {
					continue
				}
			}
		}

		out = append(out, it)
	}
	return out
}
