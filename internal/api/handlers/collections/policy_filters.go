package collections

import (
	"context"
	"net/http"
	"strings"

	"github.com/ambra-media/ambra/internal/api/dto"
	"github.com/ambra-media/ambra/internal/api/middleware"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

const (
	maxPlaylistsPerUser      = 500
	maxCollectionsQueryLimit = 5000
)

func clampCollectionsLimit(requested, defaultVal, maxVal int) int {
	if requested <= 0 {
		if defaultVal > 0 {
			return defaultVal
		}
		return maxVal
	}
	if maxVal > 0 && requested > maxVal {
		return maxVal
	}
	return requested
}

// userPolicyForRequest resolves the full dto.UserPolicy for the request caller or target user.
// Administrators, API keys, and unauthenticated unit-test requests return an unrestricted admin policy.
// Missing/deleted users fail closed with MaxParentalRating = 0 and EnableLiveTvAccess = false.
func (h *Handler) userPolicyForRequest(r *http.Request, fallbackUserID string) dto.UserPolicy {
	ac, hasAC := middleware.AuthFromContext(r.Context())
	if hasAC && ac != nil {
		if ac.IsAPIKey || ac.IsAdmin() {
			return sqlite.DefaultUserPolicy(true)
		}
	}
	uid := ""
	if h.auth.UserIDFromCtx != nil {
		uid = guid.Normalize(h.auth.UserIDFromCtx(r.Context()))
	}
	if uid == "" && hasAC && ac != nil {
		uid = guid.Normalize(ac.UserID())
	}
	if uid == "" {
		uid = guid.Normalize(fallbackUserID)
	}
	if h.userRepo != nil && uid != "" {
		u, err := h.userRepo.GetUserByID(r.Context(), uid)
		if err != nil || u == nil {
			zero := 0
			return dto.UserPolicy{
				MaxParentalRating:  &zero,
				EnableAllFolders:   false,
				EnabledFolders:     []string{},
				EnableLiveTvAccess: false,
			}
		}
		if u.IsAdmin || u.Policy.IsAdministrator {
			pol := u.Policy
			pol.IsAdministrator = true
			return pol
		}
		return u.Policy
	}
	if hasAC && ac != nil && ac.User != nil {
		if ac.User.IsAdmin || ac.User.Policy.IsAdministrator {
			pol := ac.User.Policy
			pol.IsAdministrator = true
			return pol
		}
		return ac.User.Policy
	}
	return sqlite.DefaultUserPolicy(true)
}

// playlistViewerForRequest resolves (viewerUserID, viewerIsAdmin) for SQL playlist visibility pushdown.
func (h *Handler) playlistViewerForRequest(r *http.Request, fallbackUserID string) (string, bool) {
	if ac, ok := middleware.AuthFromContext(r.Context()); ok && ac != nil && (ac.User != nil || ac.IsAPIKey) {
		if ac.IsAdmin() {
			return "", true
		}
		uid := guid.Normalize(ac.UserID())
		if uid == "" {
			uid = guid.Normalize(fallbackUserID)
		}
		if uid == "" {
			uid = "__anonymous__"
		}
		return uid, false
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
		allowSet := make(map[string]bool, len(pol.allowedTags))
		for _, t := range pol.allowedTags {
			if s := strings.ToLower(strings.TrimSpace(t)); s != "" {
				allowSet[s] = true
			}
		}
		if len(allowSet) > 0 && !h.recordHasAnyTag(ctx, rec.ID, rec.SeriesID, allowSet) {
			return true
		}
	}
	return false
}

func (h *Handler) isChildVisibleToUser(r *http.Request, itemRec *sqlite.ItemRecord, allowed []string, pol extendedParentalPolicy) bool {
	if itemRec == nil {
		return false
	}
	if strings.EqualFold(itemRec.Type, "Playlist") {
		if !h.canViewPlaylist(r, itemRec.ID, itemRec.ParentID) {
			return false
		}
	} else if allowed != nil && itemRec.LibraryID != "" && !isLibraryAllowed(itemRec.LibraryID, allowed) {
		return false
	}
	uPol := dto.UserPolicy{
		EnableAllFolders:   allowed == nil,
		EnabledFolders:     allowed,
		EnableLiveTvAccess: true,
		MaxParentalRating:  pol.maxRating,
		BlockedTags:        pol.blockedTags,
		BlockUnratedItems:  pol.blockUnratedItems,
		AllowedTags:        pol.allowedTags,
	}
	if blocked, bErr := h.repo.IsItemBlockedByPolicy(r.Context(), itemRec, uPol); bErr != nil || blocked {
		return false
	}
	return true
}
