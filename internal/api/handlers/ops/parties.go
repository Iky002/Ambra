package ops

import (
	"crypto/subtle"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/ambra-media/ambra/internal/api/httpx"
	"github.com/ambra-media/ambra/internal/api/middleware"
	"github.com/ambra-media/ambra/internal/pkg/eventbus"
	"github.com/ambra-media/ambra/internal/pkg/guid"
)

const (
	maxParties            = 200
	maxPartiesPerUser     = 10
	minPlaybackRate       = 0.25
	maxPlaybackRate       = 4.0
	maxPartyNameLen       = 128
	maxPartyInviteCodeLen = 64
	maxPartyItemIDLen     = 128
	maxPartyJoinFails     = 5
	partyJoinFailWindow   = 10 * time.Minute
	maxPartyFailBuckets   = 4096
)

type partyFailWindow struct {
	count   int
	resetAt time.Time
}

type PartyState struct {
	ItemId        string    `json:"ItemId,omitempty"`
	PositionTicks int64     `json:"PositionTicks"`
	IsPaused      bool      `json:"IsPaused"`
	PlaybackRate  float64   `json:"PlaybackRate"`
	UpdatedAt     time.Time `json:"UpdatedAt"`
}

type Party struct {
	Id          string     `json:"Id"`
	Name        string     `json:"Name"`
	HostUserId  string     `json:"HostUserId,omitempty"`
	Members     []string   `json:"Members"`
	IsPublic    bool       `json:"IsPublic"`
	InviteCode  string     `json:"InviteCode,omitempty"`
	State       PartyState `json:"State"`
	DateCreated time.Time  `json:"DateCreated"`
}

func callerAuth(r *http.Request) (callerID string, isAdmin bool, hasAuth bool) {
	ac, ok := middleware.AuthFromContext(r.Context())
	if !ok || ac == nil || (ac.User == nil && !ac.IsAPIKey) {
		return "", false, false
	}
	return guid.Normalize(ac.UserID()), ac.IsAdmin(), true
}

func isPartyMember(members []string, userID string) bool {
	if userID == "" {
		return false
	}
	norm := guid.Normalize(userID)
	for _, m := range members {
		if guid.Normalize(m) == norm {
			return true
		}
	}
	return false
}

func (h *Handler) resolveUserID(r *http.Request, explicit string) string {
	// Never trust client-supplied user IDs over an authenticated caller from AuthFromContext.
	if ac, ok := middleware.AuthFromContext(r.Context()); ok && ac != nil && (ac.User != nil || ac.IsAPIKey) {
		if uid := guid.Normalize(ac.UserID()); uid != "" {
			return uid
		}
		if explicit != "" {
			return guid.Normalize(explicit)
		}
		return ""
	}
	// Fall back to explicit request field or UserIDFromCtx only when auth context is absent (e.g. unit tests).
	if explicit != "" {
		return guid.Normalize(explicit)
	}
	if h.auth.UserIDFromCtx != nil {
		return guid.Normalize(h.auth.UserIDFromCtx(r.Context()))
	}
	return ""
}

func sanitizePartyForCaller(p Party, callerID string, isAdmin, hasAuth bool) Party {
	if len(p.Members) > 0 {
		p.Members = append([]string(nil), p.Members...)
	}
	if hasAuth && !isAdmin && (callerID == "" || guid.Normalize(p.HostUserId) != callerID) {
		p.InviteCode = ""
	}
	return p
}

func sanitizePartyForEvent(p Party) Party {
	if len(p.Members) > 0 {
		p.Members = append([]string(nil), p.Members...)
	}
	p.InviteCode = ""
	return p
}

func (h *Handler) handleListParties(w http.ResponseWriter, r *http.Request) {
	callerID, isAdmin, hasAuth := callerAuth(r)
	h.mu.RLock()
	items := make([]Party, 0, len(h.state.Parties))
	for _, p := range h.state.Parties {
		if hasAuth && !isAdmin && !p.IsPublic && (callerID == "" || (guid.Normalize(p.HostUserId) != callerID && !isPartyMember(p.Members, callerID))) {
			continue
		}
		items = append(items, sanitizePartyForCaller(p, callerID, isAdmin, hasAuth))
	}
	h.mu.RUnlock()
	httpx.JSON(w, http.StatusOK, items)
}

func (h *Handler) handleCreateParty(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name          string `json:"Name"`
		HostUserId    string `json:"HostUserId"`
		ItemId        string `json:"ItemId"`
		PositionTicks int64  `json:"PositionTicks"`
		IsPaused      bool   `json:"IsPaused"`
		IsPublic      *bool  `json:"IsPublic"`
		InviteCode    string `json:"InviteCode"`
	}
	if err := httpx.DecodeJSONLimit(r, &req, 0); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid party payload")
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = "Watch Party"
	}
	if len([]rune(name)) > maxPartyNameLen {
		httpx.Error(w, http.StatusBadRequest, "party Name exceeds maximum length")
		return
	}
	inviteCode := strings.TrimSpace(req.InviteCode)
	if len([]rune(inviteCode)) > maxPartyInviteCodeLen {
		httpx.Error(w, http.StatusBadRequest, "party InviteCode exceeds maximum length")
		return
	}
	itemID := strings.TrimSpace(req.ItemId)
	if len([]rune(itemID)) > maxPartyItemIDLen {
		httpx.Error(w, http.StatusBadRequest, "party ItemId exceeds maximum length")
		return
	}
	if req.PositionTicks < 0 {
		httpx.Error(w, http.StatusBadRequest, "PositionTicks must be non-negative")
		return
	}
	_, isAdmin, _ := callerAuth(r)
	hostID := h.resolveUserID(r, req.HostUserId)
	members := []string{}
	if hostID != "" {
		members = append(members, hostID)
	}
	isPublic := true
	if req.IsPublic != nil {
		isPublic = *req.IsPublic
	} else if inviteCode != "" {
		isPublic = false
	}

	now := time.Now().UTC()
	party := Party{
		Id:         guid.New(),
		Name:       name,
		HostUserId: hostID,
		Members:    members,
		IsPublic:   isPublic,
		InviteCode: inviteCode,
		State: PartyState{
			ItemId:        itemID,
			PositionTicks: req.PositionTicks,
			IsPaused:      req.IsPaused,
			PlaybackRate:  1.0,
			UpdatedAt:     now,
		},
		DateCreated: now,
	}

	h.mu.Lock()
	if len(h.state.Parties) >= maxParties {
		h.mu.Unlock()
		httpx.Error(w, http.StatusTooManyRequests, "maximum number of active watch parties reached")
		return
	}
	if !isAdmin && hostID != "" {
		ownedCount := 0
		for _, existing := range h.state.Parties {
			if guid.Normalize(existing.HostUserId) == hostID {
				ownedCount++
			}
		}
		if ownedCount >= maxPartiesPerUser {
			h.mu.Unlock()
			httpx.Error(w, http.StatusTooManyRequests, "maximum number of active watch parties per user reached")
			return
		}
	}
	snapshot := cloneOpsState(h.state)
	h.state.Parties = append(h.state.Parties, party)
	err := h.saveStateLocked(r.Context())
	if err != nil {
		h.state = snapshot
	}
	h.mu.Unlock()
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "failed to persist party")
		return
	}

	httpx.JSON(w, http.StatusOK, party)
}

func (h *Handler) handleGetParty(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("Id")
	callerID, isAdmin, hasAuth := callerAuth(r)
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, p := range h.state.Parties {
		if p.Id == id {
			if hasAuth && !isAdmin && !p.IsPublic && (callerID == "" || (guid.Normalize(p.HostUserId) != callerID && !isPartyMember(p.Members, callerID))) {
				httpx.Error(w, http.StatusNotFound, "party not found")
				return
			}
			httpx.JSON(w, http.StatusOK, sanitizePartyForCaller(p, callerID, isAdmin, hasAuth))
			return
		}
	}
	httpx.Error(w, http.StatusNotFound, "party not found")
}

func (h *Handler) handleDeleteParty(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("Id")
	callerID, isAdmin, hasAuth := callerAuth(r)

	h.mu.Lock()
	for i, p := range h.state.Parties {
		if p.Id == id {
			if hasAuth && !isAdmin && (callerID == "" || guid.Normalize(p.HostUserId) != callerID) {
				h.mu.Unlock()
				httpx.Error(w, http.StatusForbidden, "only the party host or an administrator can delete this party")
				return
			}
			snapshot := cloneOpsState(h.state)
			h.state.Parties = append(h.state.Parties[:i], h.state.Parties[i+1:]...)
			err := h.saveStateLocked(r.Context())
			if err != nil {
				h.state = snapshot
				h.mu.Unlock()
				httpx.Error(w, http.StatusInternalServerError, "failed to persist party deletion")
				return
			}
			h.mu.Unlock()
			httpx.NoContent(w)
			return
		}
	}
	h.mu.Unlock()
	httpx.Error(w, http.StatusNotFound, "party not found")
}

func (h *Handler) recordPartyJoinFailureLocked(failKey string, now time.Time) bool {
	if h.partyJoinFails == nil {
		h.partyJoinFails = make(map[string]*partyFailWindow)
	}
	fw := h.partyJoinFails[failKey]
	if fw == nil || !now.Before(fw.resetAt) {
		if fw == nil && len(h.partyJoinFails) >= maxPartyFailBuckets {
			return false
		}
		fw = &partyFailWindow{count: 0, resetAt: now.Add(partyJoinFailWindow)}
		h.partyJoinFails[failKey] = fw
	}
	fw.count++
	return true
}

func (h *Handler) handleJoinParty(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("Id")
	var req struct {
		UserId     string `json:"UserId"`
		InviteCode string `json:"InviteCode"`
	}
	if r.Body != nil {
		_ = httpx.DecodeJSONLimit(r, &req, 0)
	}
	inviteCode := strings.TrimSpace(req.InviteCode)
	if inviteCode == "" {
		inviteCode = strings.TrimSpace(httpx.ParseQuery(r).Get("InviteCode"))
	}
	if len([]rune(inviteCode)) > maxPartyInviteCodeLen {
		httpx.Error(w, http.StatusBadRequest, "InviteCode exceeds maximum length")
		return
	}
	userID := h.resolveUserID(r, req.UserId)
	if userID == "" {
		httpx.Error(w, http.StatusBadRequest, "UserId is required")
		return
	}
	callerID, isAdmin, hasAuth := callerAuth(r)
	failKey := userID + ":" + id
	missingKey := userID + ":__missing__"
	now := time.Now().UTC()

	h.mu.Lock()
	for k, fw := range h.partyJoinFails {
		if !now.Before(fw.resetAt) {
			delete(h.partyJoinFails, k)
		}
	}
	for i := range h.state.Parties {
		if h.state.Parties[i].Id == id {
			p := &h.state.Parties[i]
			isHostOrMember := (callerID != "" && (guid.Normalize(p.HostUserId) == callerID || isPartyMember(p.Members, callerID))) ||
				guid.Normalize(p.HostUserId) == userID || isPartyMember(p.Members, userID)
			if !isAdmin && !isHostOrMember && (!p.IsPublic || p.InviteCode != "") {
				if fw := h.partyJoinFails[failKey]; fw != nil && now.Before(fw.resetAt) && fw.count >= maxPartyJoinFails {
					h.mu.Unlock()
					httpx.Error(w, http.StatusTooManyRequests, "too many failed party join attempts")
					return
				}
				if p.InviteCode == "" || subtle.ConstantTimeCompare([]byte(inviteCode), []byte(p.InviteCode)) != 1 {
					if !h.recordPartyJoinFailureLocked(failKey, now) {
						h.mu.Unlock()
						httpx.Error(w, http.StatusTooManyRequests, "too many failed party join attempts")
						return
					}
					h.mu.Unlock()
					httpx.Error(w, http.StatusForbidden, "valid InviteCode is required to join this party")
					return
				}
				delete(h.partyJoinFails, failKey)
			}
			exists := false
			for _, m := range p.Members {
				if guid.Normalize(m) == userID {
					exists = true
					break
				}
			}
			if !exists {
				snapshot := cloneOpsState(h.state)
				p.Members = append(p.Members, userID)
				if err := h.saveStateLocked(r.Context()); err != nil {
					h.state = snapshot
					h.mu.Unlock()
					httpx.Error(w, http.StatusInternalServerError, "failed to persist party membership")
					return
				}
			}
			party := sanitizePartyForCaller(*p, callerID, isAdmin, hasAuth)
			h.mu.Unlock()
			httpx.JSON(w, http.StatusOK, party)
			return
		}
	}
	if !isAdmin {
		if fw := h.partyJoinFails[missingKey]; fw != nil && now.Before(fw.resetAt) && fw.count >= maxPartyJoinFails {
			h.mu.Unlock()
			httpx.Error(w, http.StatusTooManyRequests, "too many failed party join attempts")
			return
		}
		if !h.recordPartyJoinFailureLocked(missingKey, now) {
			h.mu.Unlock()
			httpx.Error(w, http.StatusTooManyRequests, "too many failed party join attempts")
			return
		}
	}
	h.mu.Unlock()
	httpx.Error(w, http.StatusNotFound, "party not found")
}

func (h *Handler) handleLeaveParty(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("Id")
	var req struct {
		UserId string `json:"UserId"`
	}
	if r.Body != nil {
		_ = httpx.DecodeJSONLimit(r, &req, 0)
	}
	userID := h.resolveUserID(r, req.UserId)
	if userID == "" {
		httpx.Error(w, http.StatusBadRequest, "UserId is required")
		return
	}
	callerID, isAdmin, hasAuth := callerAuth(r)

	h.mu.Lock()
	for i := range h.state.Parties {
		if h.state.Parties[i].Id == id {
			p := &h.state.Parties[i]
			isMemberOrHost := isPartyMember(p.Members, userID) || guid.Normalize(p.HostUserId) == userID
			if !isAdmin {
				if hasAuth && (callerID == "" || (guid.Normalize(p.HostUserId) != callerID && !isPartyMember(p.Members, callerID))) {
					h.mu.Unlock()
					httpx.Error(w, http.StatusForbidden, "only party members, host, or an administrator can leave this party")
					return
				}
				if !isMemberOrHost {
					h.mu.Unlock()
					httpx.Error(w, http.StatusForbidden, "caller is not a member of this party")
					return
				}
			}
			snapshot := cloneOpsState(h.state)
			kept := make([]string, 0, len(p.Members))
			for _, m := range p.Members {
				if guid.Normalize(m) != userID {
					kept = append(kept, m)
				}
			}
			if len(kept) == 0 {
				h.state.Parties = append(h.state.Parties[:i], h.state.Parties[i+1:]...)
			} else {
				h.state.Parties[i].Members = kept
				if guid.Normalize(p.HostUserId) == userID {
					h.state.Parties[i].HostUserId = kept[0]
				}
			}
			if err := h.saveStateLocked(r.Context()); err != nil {
				h.state = snapshot
				h.mu.Unlock()
				httpx.Error(w, http.StatusInternalServerError, "failed to persist party leave")
				return
			}
			h.mu.Unlock()
			httpx.NoContent(w)
			return
		}
	}
	h.mu.Unlock()
	httpx.Error(w, http.StatusNotFound, "party not found")
}

func (h *Handler) handleUpdatePartyState(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("Id")
	var req struct {
		ItemId        string   `json:"ItemId"`
		PositionTicks *int64   `json:"PositionTicks"`
		IsPaused      *bool    `json:"IsPaused"`
		PlaybackRate  *float64 `json:"PlaybackRate"`
	}
	if err := httpx.DecodeJSONLimit(r, &req, 0); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid party state payload")
		return
	}
	itemID := strings.TrimSpace(req.ItemId)
	if len([]rune(itemID)) > maxPartyItemIDLen {
		httpx.Error(w, http.StatusBadRequest, "party ItemId exceeds maximum length")
		return
	}
	if req.PositionTicks != nil && *req.PositionTicks < 0 {
		httpx.Error(w, http.StatusBadRequest, "PositionTicks must be non-negative")
		return
	}
	if req.PlaybackRate != nil {
		rate := *req.PlaybackRate
		if math.IsNaN(rate) || math.IsInf(rate, 0) || rate < minPlaybackRate || rate > maxPlaybackRate {
			httpx.Error(w, http.StatusBadRequest, "PlaybackRate must be finite and between 0.25 and 4.0")
			return
		}
	}

	callerID, isAdmin, hasAuth := callerAuth(r)

	h.mu.Lock()
	for i := range h.state.Parties {
		if h.state.Parties[i].Id == id {
			p := &h.state.Parties[i]
			if hasAuth && !isAdmin && (callerID == "" || (guid.Normalize(p.HostUserId) != callerID && !isPartyMember(p.Members, callerID))) {
				h.mu.Unlock()
				httpx.Error(w, http.StatusForbidden, "only party members, host, or an administrator can update party state")
				return
			}
			snapshot := cloneOpsState(h.state)
			st := &p.State
			if itemID != "" {
				st.ItemId = itemID
			}
			if req.PositionTicks != nil {
				st.PositionTicks = *req.PositionTicks
			}
			if req.IsPaused != nil {
				st.IsPaused = *req.IsPaused
			}
			if req.PlaybackRate != nil {
				st.PlaybackRate = *req.PlaybackRate
			}
			st.UpdatedAt = time.Now().UTC()
			respParty := sanitizePartyForCaller(*p, callerID, isAdmin, hasAuth)
			eventParty := sanitizePartyForEvent(*p)
			if err := h.saveStateLocked(r.Context()); err != nil {
				h.state = snapshot
				h.mu.Unlock()
				httpx.Error(w, http.StatusInternalServerError, "failed to persist party state")
				return
			}
			h.mu.Unlock()

			if h.bus != nil {
				h.bus.Publish(r.Context(), eventbus.Event{
					Type:    eventbus.EventSessionUpdated,
					Payload: eventParty,
				})
			}
			httpx.JSON(w, http.StatusOK, respParty)
			return
		}
	}
	h.mu.Unlock()
	httpx.Error(w, http.StatusNotFound, "party not found")
}
