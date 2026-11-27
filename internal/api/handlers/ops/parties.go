package ops

import (
	"context"
	"crypto/subtle"
	"errors"
	"io"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/ambra-media/ambra/internal/api/dto"
	"github.com/ambra-media/ambra/internal/api/httpx"
	"github.com/ambra-media/ambra/internal/api/middleware"
	"github.com/ambra-media/ambra/internal/api/ws"
	"github.com/ambra-media/ambra/internal/pkg/eventbus"
	"github.com/ambra-media/ambra/internal/pkg/guid"
)

const (
	maxParties              = 200
	maxPartiesPerUser       = 10
	minPlaybackRate         = 0.25
	maxPlaybackRate         = 4.0
	maxPartyNameLen         = 128
	maxPartyInviteCodeLen   = 64
	maxPartyItemIDLen       = 128
	maxPartyJoinFails       = 5
	partyJoinFailWindow     = 10 * time.Minute
	maxPartyFailBuckets     = 4096
	maxPartyMessagesPerRoom = 100
	maxPartyMessageBytes    = 64 << 10
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

type PartyMessage struct {
	Id       string    `json:"Id"`
	PartyId  string    `json:"PartyId"`
	UserId   string    `json:"UserId"`
	UserName string    `json:"UserName,omitempty"`
	Text     string    `json:"Text"`
	SentAt   time.Time `json:"SentAt"`
}

type Party struct {
	Id          string         `json:"Id"`
	Name        string         `json:"Name"`
	HostUserId  string         `json:"HostUserId,omitempty"`
	Members     []string       `json:"Members"`
	IsPublic    bool           `json:"IsPublic"`
	InviteCode  string         `json:"InviteCode,omitempty"`
	State       PartyState     `json:"State"`
	Messages    []PartyMessage `json:"Messages,omitempty"`
	DateCreated time.Time      `json:"DateCreated"`
}

func callerAuth(r *http.Request) (callerID string, isAdmin bool, hasAuth bool) {
	ac, ok := middleware.AuthFromContext(r.Context())
	if !ok || ac == nil || (ac.User == nil && !ac.IsAPIKey) {
		return "", false, false
	}
	return guid.Normalize(ac.UserID()), ac.IsAdmin(), true
}

func (h *Handler) canAccessPartyItem(r *http.Request, explicitUserID, itemID string) bool {
	trimmedItem := strings.TrimSpace(itemID)
	if trimmedItem == "" {
		return true
	}
	checker := h.getItemAccessChecker()
	if checker == nil {
		return true
	}
	if ac, ok := middleware.AuthFromContext(r.Context()); ok && ac != nil && (ac.User != nil || ac.IsAPIKey) {
		normExplicit := guid.Normalize(explicitUserID)
		if normExplicit != "" && normExplicit != guid.Normalize(ac.UserID()) {
			return h.canMemberAccessPartyItem(r, normExplicit, trimmedItem)
		}
		if ac.IsAdmin() {
			return true
		}
		if ac.User != nil {
			return checker(r.Context(), ac.User, trimmedItem)
		}
		return false
	}
	uid := h.resolveUserID(r, explicitUserID)
	if uid != "" && h.userRepo != nil {
		u, err := h.userRepo.GetUserByID(r.Context(), uid)
		if err != nil || u == nil {
			return false
		}
		if u.IsAdmin || u.Policy.IsAdministrator {
			return true
		}
		return checker(r.Context(), u, trimmedItem)
	}
	return true
}

func (h *Handler) canMemberAccessPartyItem(r *http.Request, memberUserID, itemID string) bool {
	trimmedItem := strings.TrimSpace(itemID)
	if trimmedItem == "" {
		return true
	}
	checker := h.getItemAccessChecker()
	if checker == nil {
		return true
	}
	uid := guid.Normalize(memberUserID)
	if uid == "" {
		return false
	}
	if h.userRepo != nil {
		u, err := h.userRepo.GetUserByID(r.Context(), uid)
		if err == nil && u != nil {
			if u.IsDisabled || u.Policy.IsDisabled {
				return false
			}
			if u.IsAdmin || u.Policy.IsAdministrator {
				return true
			}
			return checker(r.Context(), u, trimmedItem)
		}
	}
	if ac, ok := middleware.AuthFromContext(r.Context()); ok && ac != nil && ac.User != nil && guid.Normalize(ac.User.ID) == uid {
		if ac.IsAdmin() {
			return true
		}
		return checker(r.Context(), ac.User, trimmedItem)
	}
	return false
}

func callerUserName(r *http.Request) string {
	if ac, ok := middleware.AuthFromContext(r.Context()); ok && ac != nil && ac.User != nil {
		return ac.User.Name
	}
	return ""
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
	if len(p.Messages) > 0 {
		p.Messages = append([]PartyMessage(nil), p.Messages...)
	}
	if hasAuth && !isAdmin && (callerID == "" || guid.Normalize(p.HostUserId) != callerID) {
		p.InviteCode = ""
	}
	if hasAuth && !isAdmin && (callerID == "" || (guid.Normalize(p.HostUserId) != callerID && !isPartyMember(p.Members, callerID))) {
		p.Messages = nil
	}
	return p
}

func sanitizePartyForEvent(p Party) Party {
	if len(p.Members) > 0 {
		p.Members = append([]string(nil), p.Members...)
	}
	if len(p.Messages) > 0 {
		p.Messages = append([]PartyMessage(nil), p.Messages...)
	}
	p.InviteCode = ""
	return p
}

// PurgeUser removes a deleted user from all active Watch-Together parties and deletes empty rooms.
func (h *Handler) PurgeUser(ctx context.Context, userID string) {
	norm := guid.Normalize(userID)
	if norm == "" {
		return
	}
	h.mu.Lock()
	changed := false
	nextParties := make([]Party, 0, len(h.state.Parties))
	for _, p := range h.state.Parties {
		kept := make([]string, 0, len(p.Members))
		removedMember := false
		for _, m := range p.Members {
			if guid.Normalize(m) == norm {
				removedMember = true
				continue
			}
			kept = append(kept, m)
		}
		if removedMember || guid.Normalize(p.HostUserId) == norm {
			changed = true
			if len(kept) == 0 {
				continue
			}
			p.Members = kept
			if guid.Normalize(p.HostUserId) == norm {
				p.HostUserId = kept[0]
			}
		}
		nextParties = append(nextParties, p)
	}
	if changed {
		h.state.Parties = nextParties
		_ = h.saveStateLocked(ctx)
	}
	h.mu.Unlock()
}

func (h *Handler) pushPartyWS(members []string, msg ws.Message) {
	if hub := h.getWSHub(); hub != nil && len(members) > 0 {
		hub.SendToUsers(members, msg)
	}
}

func (h *Handler) pushPartyUpdatedWS(r *http.Request, members []string, eventParty Party) {
	hub := h.getWSHub()
	if hub == nil || len(members) == 0 {
		return
	}
	if strings.TrimSpace(eventParty.State.ItemId) != "" && h.getItemAccessChecker() != nil {
		var allowedMembers, scrubbedMembers []string
		for _, m := range members {
			if h.canMemberAccessPartyItem(r, m, eventParty.State.ItemId) {
				allowedMembers = append(allowedMembers, m)
			} else {
				scrubbedMembers = append(scrubbedMembers, m)
			}
		}
		if len(allowedMembers) > 0 {
			hub.SendToUsers(allowedMembers, ws.Message{
				MessageType: "PartyUpdated",
				Data:        eventParty,
			})
		}
		if len(scrubbedMembers) > 0 {
			scrubbed := eventParty
			scrubbed.State.ItemId = ""
			hub.SendToUsers(scrubbedMembers, ws.Message{
				MessageType: "PartyUpdated",
				Data:        scrubbed,
			})
		}
		return
	}
	hub.SendToUsers(members, ws.Message{
		MessageType: "PartyUpdated",
		Data:        eventParty,
	})
}

func (h *Handler) handleListParties(w http.ResponseWriter, r *http.Request) {
	callerID, isAdmin, hasAuth := callerAuth(r)
	h.mu.RLock()
	snapshot := make([]Party, len(h.state.Parties))
	copy(snapshot, h.state.Parties)
	h.mu.RUnlock()

	items := make([]Party, 0, len(snapshot))
	for _, p := range snapshot {
		isHostOrMember := callerID != "" && (guid.Normalize(p.HostUserId) == callerID || isPartyMember(p.Members, callerID))
		if hasAuth && !isAdmin && !p.IsPublic && !isHostOrMember {
			continue
		}
		if p.State.ItemId != "" && !h.canAccessPartyItem(r, callerID, p.State.ItemId) {
			if !isHostOrMember {
				continue
			}
			p.State.ItemId = ""
		}
		items = append(items, sanitizePartyForCaller(p, callerID, isAdmin, hasAuth))
	}
	httpx.JSON(w, http.StatusOK, items)
}

// HandleGetCurrentPartyInfo exposes GET /Parties/Info.
func (h *Handler) HandleGetCurrentPartyInfo(w http.ResponseWriter, r *http.Request) {
	h.handleGetCurrentPartyInfo(w, r)
}

func (h *Handler) handleGetCurrentPartyInfo(w http.ResponseWriter, r *http.Request) {
	callerID, isAdmin, hasAuth := callerAuth(r)
	userID := h.resolveUserID(r, httpx.ParseQuery(r).Get("UserId"))
	if callerID == "" {
		callerID = userID
	}

	h.mu.RLock()
	var found *Party
	for i := len(h.state.Parties) - 1; i >= 0; i-- {
		p := h.state.Parties[i]
		if userID != "" && (guid.Normalize(p.HostUserId) == userID || isPartyMember(p.Members, userID)) {
			san := sanitizePartyForCaller(p, callerID, isAdmin, hasAuth)
			found = &san
			break
		}
	}
	h.mu.RUnlock()
	if found != nil {
		if found.State.ItemId != "" && !h.canAccessPartyItem(r, userID, found.State.ItemId) {
			found.State.ItemId = ""
		}
		httpx.JSON(w, http.StatusOK, map[string]any{
			"InParty": true,
			"Party":   *found,
		})
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{
		"InParty": false,
		"Party":   nil,
	})
}

// HandleListPartyMessages exposes GET /Parties/Messages.
func (h *Handler) HandleListPartyMessages(w http.ResponseWriter, r *http.Request) {
	h.handleListPartyMessages(w, r)
}

func (h *Handler) handleListPartyMessages(w http.ResponseWriter, r *http.Request) {
	q := httpx.ParseQuery(r)
	partyID := strings.TrimSpace(q.Get("PartyId"))
	startIndex := q.Int("StartIndex", 0)
	if startIndex < 0 {
		startIndex = 0
	}
	limit := q.Int("Limit", 0)
	callerID, isAdmin, hasAuth := callerAuth(r)
	userID := h.resolveUserID(r, "")
	if callerID == "" {
		callerID = userID
	}

	h.mu.RLock()
	defer h.mu.RUnlock()
	for i := len(h.state.Parties) - 1; i >= 0; i-- {
		p := h.state.Parties[i]
		if partyID != "" && p.Id != partyID {
			continue
		}
		isMember := userID != "" && (guid.Normalize(p.HostUserId) == userID || isPartyMember(p.Members, userID))
		if partyID == "" && !isMember {
			continue
		}
		if hasAuth && !isAdmin && !isMember {
			httpx.Error(w, http.StatusForbidden, "only party members can view party messages")
			return
		}
		allMsgs := append([]PartyMessage(nil), p.Messages...)
		total := len(allMsgs)
		var msgs []PartyMessage
		if startIndex >= total {
			msgs = []PartyMessage{}
		} else {
			msgs = allMsgs[startIndex:]
			if limit > 0 && len(msgs) > limit {
				msgs = msgs[:limit]
			}
		}
		if msgs == nil {
			msgs = []PartyMessage{}
		}
		httpx.JSON(w, http.StatusOK, dto.QueryResult[PartyMessage]{
			Items:            msgs,
			TotalRecordCount: total,
		})
		return
	}
	if partyID != "" {
		httpx.Error(w, http.StatusNotFound, "party not found")
		return
	}
	httpx.JSON(w, http.StatusOK, dto.QueryResult[PartyMessage]{
		Items:            []PartyMessage{},
		TotalRecordCount: 0,
	})
}

// HandlePostPartyMessage exposes POST /Parties/Messages so compatcore can delegate to it.
func (h *Handler) HandlePostPartyMessage(w http.ResponseWriter, r *http.Request) {
	h.handlePostPartyMessage(w, r)
}

func (h *Handler) handlePostPartyMessage(w http.ResponseWriter, r *http.Request) {
	if r.ContentLength > maxPartyMessageBytes {
		httpx.Error(w, http.StatusRequestEntityTooLarge, "message too large")
		return
	}
	var req struct {
		PartyId string `json:"PartyId"`
		Text    string `json:"Text"`
		Message string `json:"Message"`
		UserId  string `json:"UserId"`
	}
	if r.Body != nil {
		err := httpx.DecodeJSONLimit(r, &req, maxPartyMessageBytes)
		if err != nil && !errors.Is(err, io.EOF) {
			httpx.Error(w, http.StatusBadRequest, "invalid or oversized message body")
			return
		}
	}
	q := httpx.ParseQuery(r)
	partyID := strings.TrimSpace(req.PartyId)
	if partyID == "" {
		partyID = strings.TrimSpace(q.Get("PartyId"))
	}
	text := strings.TrimSpace(req.Text)
	if text == "" {
		text = strings.TrimSpace(req.Message)
	}
	if text == "" {
		text = strings.TrimSpace(q.Get("Text"))
	}

	callerID, isAdmin, hasAuth := callerAuth(r)
	userID := h.resolveUserID(r, req.UserId)
	if callerID == "" {
		callerID = userID
	}
	userName := callerUserName(r)

	h.mu.Lock()
	targetIdx := -1
	for i := len(h.state.Parties) - 1; i >= 0; i-- {
		p := &h.state.Parties[i]
		if partyID != "" {
			if p.Id == partyID {
				targetIdx = i
				break
			}
			continue
		}
		if userID != "" && (guid.Normalize(p.HostUserId) == userID || isPartyMember(p.Members, userID)) {
			targetIdx = i
			break
		}
	}
	if targetIdx < 0 {
		h.mu.Unlock()
		if partyID != "" {
			httpx.Error(w, http.StatusNotFound, "party not found")
			return
		}
		httpx.NoContent(w)
		return
	}
	p := &h.state.Parties[targetIdx]
	isMember := userID != "" && (guid.Normalize(p.HostUserId) == userID || isPartyMember(p.Members, userID))
	if hasAuth && !isAdmin && !isMember {
		h.mu.Unlock()
		httpx.Error(w, http.StatusForbidden, "only party members can send messages")
		return
	}
	if text == "" {
		h.mu.Unlock()
		httpx.NoContent(w)
		return
	}

	msg := PartyMessage{
		Id:       guid.New(),
		PartyId:  p.Id,
		UserId:   userID,
		UserName: userName,
		Text:     text,
		SentAt:   time.Now().UTC(),
	}
	snapshot := cloneOpsState(h.state)
	p.Messages = append(p.Messages, msg)
	if len(p.Messages) > maxPartyMessagesPerRoom {
		p.Messages = append([]PartyMessage(nil), p.Messages[len(p.Messages)-maxPartyMessagesPerRoom:]...)
	}
	members := append([]string(nil), p.Members...)
	err := h.saveStateLocked(r.Context())
	if err != nil {
		h.state = snapshot
		h.mu.Unlock()
		httpx.Error(w, http.StatusInternalServerError, "failed to persist party message")
		return
	}
	h.mu.Unlock()

	h.pushPartyWS(members, ws.Message{
		MessageType: "GeneralCommand",
		Data: map[string]any{
			"Name": "DisplayMessage",
			"Arguments": map[string]any{
				"Header":    userName,
				"Text":      msg.Text,
				"PartyId":   msg.PartyId,
				"UserId":    msg.UserId,
				"SentAt":    msg.SentAt.Format(time.RFC3339),
				"TimeoutMs": "5000",
			},
		},
	})
	httpx.NoContent(w)
}

// HandleLeaveCurrentParty exposes POST /Parties/Leave so compatcore can delegate to it.
func (h *Handler) HandleLeaveCurrentParty(w http.ResponseWriter, r *http.Request) {
	h.handleLeaveCurrentParty(w, r)
}

func (h *Handler) handleLeaveCurrentParty(w http.ResponseWriter, r *http.Request) {
	var req struct {
		PartyId string `json:"PartyId"`
		UserId  string `json:"UserId"`
	}
	if r.Body != nil {
		_ = httpx.DecodeJSONLimit(r, &req, 64<<10)
	}
	partyID := strings.TrimSpace(req.PartyId)
	if partyID == "" {
		partyID = strings.TrimSpace(httpx.ParseQuery(r).Get("PartyId"))
	}
	userID := h.resolveUserID(r, req.UserId)
	if userID == "" {
		httpx.NoContent(w)
		return
	}

	h.mu.Lock()
	snapshot := cloneOpsState(h.state)
	changed := false
	var notifyLists []struct {
		members []string
		party   Party
	}
	nextParties := make([]Party, 0, len(h.state.Parties))
	for _, p := range h.state.Parties {
		if partyID != "" && p.Id != partyID {
			nextParties = append(nextParties, p)
			continue
		}
		if !isPartyMember(p.Members, userID) && guid.Normalize(p.HostUserId) != userID {
			nextParties = append(nextParties, p)
			continue
		}
		changed = true
		kept := make([]string, 0, len(p.Members))
		for _, m := range p.Members {
			if guid.Normalize(m) != userID {
				kept = append(kept, m)
			}
		}
		if len(kept) == 0 {
			continue
		}
		p.Members = kept
		if guid.Normalize(p.HostUserId) == userID {
			p.HostUserId = kept[0]
		}
		nextParties = append(nextParties, p)
		notifyLists = append(notifyLists, struct {
			members []string
			party   Party
		}{members: append([]string(nil), kept...), party: sanitizePartyForEvent(p)})
	}
	if changed {
		h.state.Parties = nextParties
		if err := h.saveStateLocked(r.Context()); err != nil {
			h.state = snapshot
			h.mu.Unlock()
			httpx.Error(w, http.StatusInternalServerError, "failed to persist party leave")
			return
		}
	}
	h.mu.Unlock()

	for _, n := range notifyLists {
		h.pushPartyUpdatedWS(r, n.members, n.party)
	}
	httpx.NoContent(w)
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
	if err := httpx.DecodeJSONLimit(r, &req, 64<<10); err != nil {
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
	if itemID != "" && !h.canAccessPartyItem(r, hostID, itemID) {
		httpx.Error(w, http.StatusForbidden, "access to party item denied")
		return
	}
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
	var found *Party
	for _, p := range h.state.Parties {
		if p.Id == id {
			cp := p
			found = &cp
			break
		}
	}
	h.mu.RUnlock()
	if found == nil {
		httpx.Error(w, http.StatusNotFound, "party not found")
		return
	}
	if hasAuth && !isAdmin && !found.IsPublic && (callerID == "" || (guid.Normalize(found.HostUserId) != callerID && !isPartyMember(found.Members, callerID))) {
		httpx.Error(w, http.StatusNotFound, "party not found")
		return
	}
	if found.State.ItemId != "" && !h.canAccessPartyItem(r, callerID, found.State.ItemId) {
		httpx.Error(w, http.StatusForbidden, "access to party item denied")
		return
	}
	httpx.JSON(w, http.StatusOK, sanitizePartyForCaller(*found, callerID, isAdmin, hasAuth))
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
		_ = httpx.DecodeJSONLimit(r, &req, 64<<10)
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
			const maxPartyJoinVerifyRetries = 4
			verifiedItemID := ""
			for attempt := 0; ; attempt++ {
				partyItemID := strings.TrimSpace(p.State.ItemId)
				if partyItemID == "" || partyItemID == verifiedItemID {
					break
				}
				if attempt >= maxPartyJoinVerifyRetries {
					h.mu.Unlock()
					httpx.Error(w, http.StatusConflict, "party item changed concurrently; please retry")
					return
				}
				if r.Context().Err() != nil {
					h.mu.Unlock()
					httpx.Error(w, http.StatusRequestTimeout, "request cancelled")
					return
				}
				h.mu.Unlock()
				if !h.canAccessPartyItem(r, userID, partyItemID) {
					httpx.Error(w, http.StatusForbidden, "access to party item denied")
					return
				}
				verifiedItemID = partyItemID
				h.mu.Lock()
				// Re-find party after releasing lock for item access check.
				foundIdx := -1
				for j := range h.state.Parties {
					if h.state.Parties[j].Id == id {
						foundIdx = j
						break
					}
				}
				if foundIdx < 0 {
					h.mu.Unlock()
					httpx.Error(w, http.StatusNotFound, "party not found")
					return
				}
				p = &h.state.Parties[foundIdx]
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
			eventParty := sanitizePartyForEvent(*p)
			members := append([]string(nil), p.Members...)
			h.mu.Unlock()

			if !exists {
				h.pushPartyUpdatedWS(r, members, eventParty)
			}
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
		_ = httpx.DecodeJSONLimit(r, &req, 64<<10)
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
			var eventParty *Party
			var notifyMembers []string
			if len(kept) == 0 {
				h.state.Parties = append(h.state.Parties[:i], h.state.Parties[i+1:]...)
			} else {
				h.state.Parties[i].Members = kept
				if guid.Normalize(p.HostUserId) == userID {
					h.state.Parties[i].HostUserId = kept[0]
				}
				ep := sanitizePartyForEvent(h.state.Parties[i])
				eventParty = &ep
				notifyMembers = append([]string(nil), kept...)
			}
			if err := h.saveStateLocked(r.Context()); err != nil {
				h.state = snapshot
				h.mu.Unlock()
				httpx.Error(w, http.StatusInternalServerError, "failed to persist party leave")
				return
			}
			h.mu.Unlock()

			if eventParty != nil {
				h.pushPartyUpdatedWS(r, notifyMembers, *eventParty)
			}
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
	if err := httpx.DecodeJSONLimit(r, &req, 64<<10); err != nil {
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
	actorID := h.resolveUserID(r, "")
	if callerID == "" {
		callerID = actorID
	}
	if itemID != "" && !h.canAccessPartyItem(r, callerID, itemID) {
		httpx.Error(w, http.StatusForbidden, "access to party item denied")
		return
	}

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
			stateCopy := *st
			respParty := sanitizePartyForCaller(*p, callerID, isAdmin, hasAuth)
			eventParty := sanitizePartyForEvent(*p)
			members := append([]string(nil), p.Members...)
			if err := h.saveStateLocked(r.Context()); err != nil {
				h.state = snapshot
				h.mu.Unlock()
				httpx.Error(w, http.StatusInternalServerError, "failed to persist party state")
				return
			}
			h.mu.Unlock()

			if !isAdmin && respParty.State.ItemId != "" && !h.canAccessPartyItem(r, callerID, respParty.State.ItemId) {
				respParty.State.ItemId = ""
			}

			if h.bus != nil {
				h.bus.Publish(r.Context(), eventbus.Event{
					Type:    eventbus.EventSessionUpdated,
					Payload: eventParty,
				})
			}
			playstateCmd := "Unpause"
			if stateCopy.IsPaused {
				playstateCmd = "Pause"
			} else if req.PositionTicks != nil {
				playstateCmd = "Seek"
			}
			makePlaystateMsg := func(msgItemID string) ws.Message {
				return ws.Message{
					MessageType: "Playstate",
					Data: map[string]any{
						"Command":           playstateCmd,
						"SeekPositionTicks": stateCopy.PositionTicks,
						"PositionTicks":     stateCopy.PositionTicks,
						"IsPaused":          stateCopy.IsPaused,
						"PlaybackRate":      stateCopy.PlaybackRate,
						"ItemId":            msgItemID,
						"PartyId":           id,
						"ControllingUserId": callerID,
					},
				}
			}
			if hub := h.getWSHub(); hub != nil && len(members) > 0 {
				if strings.TrimSpace(stateCopy.ItemId) != "" && h.getItemAccessChecker() != nil {
					var allowedMembers, scrubbedMembers []string
					for _, m := range members {
						if h.canMemberAccessPartyItem(r, m, stateCopy.ItemId) {
							allowedMembers = append(allowedMembers, m)
						} else {
							scrubbedMembers = append(scrubbedMembers, m)
						}
					}
					if len(allowedMembers) > 0 {
						hub.SendToUsers(allowedMembers, makePlaystateMsg(stateCopy.ItemId))
					}
					if len(scrubbedMembers) > 0 {
						hub.SendToUsers(scrubbedMembers, makePlaystateMsg(""))
					}
				} else {
					hub.SendToUsers(members, makePlaystateMsg(stateCopy.ItemId))
				}
			}
			httpx.JSON(w, http.StatusOK, respParty)
			return
		}
	}
	h.mu.Unlock()
	httpx.Error(w, http.StatusNotFound, "party not found")
}
