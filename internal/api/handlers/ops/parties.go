package ops

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/ambra-media/ambra/internal/api/httpx"
	"github.com/ambra-media/ambra/internal/api/middleware"
	"github.com/ambra-media/ambra/internal/pkg/eventbus"
	"github.com/ambra-media/ambra/internal/pkg/guid"
)

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

func (h *Handler) handleListParties(w http.ResponseWriter, r *http.Request) {
	callerID, isAdmin, hasAuth := callerAuth(r)
	h.mu.RLock()
	items := make([]Party, 0, len(h.state.Parties))
	for _, p := range h.state.Parties {
		if hasAuth && !isAdmin && (callerID == "" || (guid.Normalize(p.HostUserId) != callerID && !isPartyMember(p.Members, callerID))) {
			continue
		}
		items = append(items, p)
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
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid party payload")
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = "Watch Party"
	}
	hostID := h.resolveUserID(r, req.HostUserId)
	members := []string{}
	if hostID != "" {
		members = append(members, hostID)
	}

	now := time.Now().UTC()
	party := Party{
		Id:         guid.New(),
		Name:       name,
		HostUserId: hostID,
		Members:    members,
		State: PartyState{
			ItemId:        req.ItemId,
			PositionTicks: req.PositionTicks,
			IsPaused:      req.IsPaused,
			PlaybackRate:  1.0,
			UpdatedAt:     now,
		},
		DateCreated: now,
	}

	h.mu.Lock()
	h.state.Parties = append(h.state.Parties, party)
	h.saveStateLocked(r.Context())
	h.mu.Unlock()

	httpx.JSON(w, http.StatusOK, party)
}

func (h *Handler) handleGetParty(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("Id")
	callerID, isAdmin, hasAuth := callerAuth(r)
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, p := range h.state.Parties {
		if p.Id == id {
			if hasAuth && !isAdmin && (callerID == "" || (guid.Normalize(p.HostUserId) != callerID && !isPartyMember(p.Members, callerID))) {
				httpx.Error(w, http.StatusForbidden, "only party members, host, or an administrator can view this party")
				return
			}
			httpx.JSON(w, http.StatusOK, p)
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
			h.state.Parties = append(h.state.Parties[:i], h.state.Parties[i+1:]...)
			h.saveStateLocked(r.Context())
			h.mu.Unlock()
			httpx.NoContent(w)
			return
		}
	}
	h.mu.Unlock()
	httpx.Error(w, http.StatusNotFound, "party not found")
}

func (h *Handler) handleJoinParty(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("Id")
	var req struct {
		UserId string `json:"UserId"`
	}
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}
	userID := h.resolveUserID(r, req.UserId)
	if userID == "" {
		httpx.Error(w, http.StatusBadRequest, "UserId is required")
		return
	}

	h.mu.Lock()
	for i := range h.state.Parties {
		if h.state.Parties[i].Id == id {
			exists := false
			for _, m := range h.state.Parties[i].Members {
				if m == userID {
					exists = true
					break
				}
			}
			if !exists {
				h.state.Parties[i].Members = append(h.state.Parties[i].Members, userID)
				h.saveStateLocked(r.Context())
			}
			party := h.state.Parties[i]
			h.mu.Unlock()
			httpx.JSON(w, http.StatusOK, party)
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
		_ = json.NewDecoder(r.Body).Decode(&req)
	}
	userID := h.resolveUserID(r, req.UserId)
	if userID == "" {
		httpx.Error(w, http.StatusBadRequest, "UserId is required")
		return
	}

	h.mu.Lock()
	for i := range h.state.Parties {
		if h.state.Parties[i].Id == id {
			kept := make([]string, 0, len(h.state.Parties[i].Members))
			for _, m := range h.state.Parties[i].Members {
				if m != userID {
					kept = append(kept, m)
				}
			}
			h.state.Parties[i].Members = kept
			h.saveStateLocked(r.Context())
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
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid party state payload")
		return
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
			st := &p.State
			if req.ItemId != "" {
				st.ItemId = req.ItemId
			}
			if req.PositionTicks != nil {
				st.PositionTicks = *req.PositionTicks
			}
			if req.IsPaused != nil {
				st.IsPaused = *req.IsPaused
			}
			if req.PlaybackRate != nil && *req.PlaybackRate > 0 {
				st.PlaybackRate = *req.PlaybackRate
			}
			st.UpdatedAt = time.Now().UTC()
			party := *p
			h.saveStateLocked(r.Context())
			h.mu.Unlock()

			if h.bus != nil {
				h.bus.Publish(r.Context(), eventbus.Event{
					Type:    eventbus.EventSessionUpdated,
					Payload: party,
				})
			}
			httpx.JSON(w, http.StatusOK, party)
			return
		}
	}
	h.mu.Unlock()
	httpx.Error(w, http.StatusNotFound, "party not found")
}
