package catalog

import (
	"context"
	"net/http"
	"strings"

	"github.com/ambra-media/ambra/internal/api/dto"
	"github.com/ambra-media/ambra/internal/api/middleware"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

type extendedParentalPolicy struct {
	maxRating         *int
	blockedTags       []string
	blockUnratedItems []string
	allowedTags       []string
}

func (p extendedParentalPolicy) hasExtraPolicy() bool {
	return len(p.blockUnratedItems) > 0 || len(p.allowedTags) > 0
}

func (h *Handler) userExtendedPolicyForRequest(r *http.Request, fallbackUserID string) extendedParentalPolicy {
	if ac, ok := middleware.AuthFromContext(r.Context()); ok && ac != nil {
		if ac.IsAPIKey || ac.IsAdmin() {
			return extendedParentalPolicy{}
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
			return extendedParentalPolicy{maxRating: &zero}
		}
		if u.IsAdmin || u.Policy.IsAdministrator {
			return extendedParentalPolicy{}
		}
		return extendedParentalPolicy{
			maxRating:         u.Policy.MaxParentalRating,
			blockedTags:       u.Policy.BlockedTags,
			blockUnratedItems: u.Policy.BlockUnratedItems,
			allowedTags:       u.Policy.AllowedTags,
		}
	}
	if ac, ok := middleware.AuthFromContext(r.Context()); ok && ac != nil && ac.User != nil {
		return extendedParentalPolicy{
			maxRating:         ac.User.Policy.MaxParentalRating,
			blockedTags:       ac.User.Policy.BlockedTags,
			blockUnratedItems: ac.User.Policy.BlockUnratedItems,
			allowedTags:       ac.User.Policy.AllowedTags,
		}
	}
	return extendedParentalPolicy{}
}

func matchesBlockUnratedType(itemType, mediaType string, blockUnrated []string) bool {
	if strings.EqualFold(itemType, "CollectionFolder") ||
		strings.EqualFold(itemType, "Folder") ||
		strings.EqualFold(itemType, "UserView") ||
		strings.EqualFold(itemType, "Person") ||
		strings.EqualFold(itemType, "Genre") ||
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
		if strings.EqualFold(t, "Music") && (strings.EqualFold(itemType, "Audio") || strings.EqualFold(itemType, "MusicAlbum") || strings.EqualFold(mediaType, "Audio")) {
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
		if h.userRepo != nil && h.userRepo.Store() != nil {
			rows, err := h.userRepo.Store().Reader.QueryContext(ctx, `
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

func (h *Handler) filterDTOItemsPost(ctx context.Context, items []dto.BaseItemDto, pol extendedParentalPolicy) []dto.BaseItemDto {
	if len(items) == 0 || !pol.hasExtraPolicy() {
		return items
	}
	allowSet := normalizeLowerSet(pol.allowedTags)
	out := make([]dto.BaseItemDto, 0, len(items))
	for _, it := range items {
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
		out = append(out, it)
	}
	return out
}
