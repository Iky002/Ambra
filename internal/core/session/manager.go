package session

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/ambra-media/ambra/internal/api/dto"
	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/core/auth"
	"github.com/ambra-media/ambra/internal/pkg/eventbus"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

const (
	defaultPlayedThreshold = 0.90
	progressPersistWindow  = 15 * time.Second
)

type PlaybackStartRequest struct {
	ItemId              string           `json:"ItemId"`
	Item                *dto.BaseItemDto `json:"Item,omitempty"`
	MediaSourceId       string           `json:"MediaSourceId"`
	PlaySessionId       string           `json:"PlaySessionId"`
	PlayMethod          string           `json:"PlayMethod"`
	PositionTicks       int64            `json:"PositionTicks"`
	CanSeek             bool             `json:"CanSeek"`
	IsPaused            bool             `json:"IsPaused"`
	IsMuted             bool             `json:"IsMuted"`
	VolumeLevel         int              `json:"VolumeLevel"`
	AudioStreamIndex    *int             `json:"AudioStreamIndex"`
	SubtitleStreamIndex *int             `json:"SubtitleStreamIndex"`
	RepeatMode          string           `json:"RepeatMode"`
}

type PlaybackProgressRequest struct {
	ItemId              string `json:"ItemId"`
	MediaSourceId       string `json:"MediaSourceId"`
	PlaySessionId       string `json:"PlaySessionId"`
	PlayMethod          string `json:"PlayMethod"`
	PositionTicks       int64  `json:"PositionTicks"`
	CanSeek             bool   `json:"CanSeek"`
	IsPaused            bool   `json:"IsPaused"`
	IsMuted             bool   `json:"IsMuted"`
	VolumeLevel         int    `json:"VolumeLevel"`
	AudioStreamIndex    *int   `json:"AudioStreamIndex"`
	SubtitleStreamIndex *int   `json:"SubtitleStreamIndex"`
	RepeatMode          string `json:"RepeatMode"`
	EventName           string `json:"EventName"`
}

type PlaybackStoppedRequest struct {
	ItemId        string `json:"ItemId"`
	MediaSourceId string `json:"MediaSourceId"`
	PlaySessionId string `json:"PlaySessionId"`
	PositionTicks int64  `json:"PositionTicks"`
	RuntimeTicks  int64  `json:"RuntimeTicks,omitempty"`
}

type CapabilitiesRequest struct {
	PlayableMediaTypes    []string `json:"PlayableMediaTypes"`
	SupportedCommands     []string `json:"SupportedCommands"`
	SupportsRemoteControl bool     `json:"SupportsRemoteControl"`
}

type PlaybackEventPayload struct {
	SessionId     string `json:"SessionId"`
	UserId        string `json:"UserId"`
	UserName      string `json:"UserName"`
	DeviceId      string `json:"DeviceId"`
	ItemId        string `json:"ItemId"`
	MediaSourceId string `json:"MediaSourceId,omitempty"`
	PlaySessionId string `json:"PlaySessionId,omitempty"`
	PositionTicks int64  `json:"PositionTicks"`
	Played        bool   `json:"Played,omitempty"`
}

type UserDataChangedEntry struct {
	ItemId                string     `json:"ItemId"`
	PlaybackPositionTicks int64      `json:"PlaybackPositionTicks"`
	PlayCount             int        `json:"PlayCount"`
	IsFavorite            bool       `json:"IsFavorite"`
	Played                bool       `json:"Played"`
	LastPlayedDate        *time.Time `json:"LastPlayedDate,omitempty"`
}

type UserDataChangedPayload struct {
	UserId       string                 `json:"UserId"`
	UserDataList []UserDataChangedEntry `json:"UserDataList"`
}

type sessionEntry struct {
	info            dto.SessionInfo
	lastPersistAt   time.Time
	lastPausedState bool
	playSessionID   string
}

// Manager maintains active client sessions in memory and bridges playback progress to SQLite and EventBus.
type Manager struct {
	repo *sqlite.UserRepo
	cfg  *config.Config
	bus  *eventbus.Bus

	mu       sync.RWMutex
	sessions map[string]*sessionEntry // keyed by SessionInfo.Id
}

func NewManager(repo *sqlite.UserRepo, cfg *config.Config, bus *eventbus.Bus) *Manager {
	return &Manager{
		repo:     repo,
		cfg:      cfg,
		bus:      bus,
		sessions: make(map[string]*sessionEntry),
	}
}

func (m *Manager) TouchSession(user *sqlite.UserRecord, client auth.ClientInfo) dto.SessionInfo {
	if user == nil {
		return dto.SessionInfo{}
	}
	now := time.Now().UTC()
	deviceID := client.DeviceID
	if deviceID == "" {
		deviceID = "default-device"
	}
	sid := auth.SessionID(user.ID, deviceID)

	m.mu.Lock()
	entry, ok := m.sessions[sid]
	if !ok {
		entry = &sessionEntry{
			info: dto.SessionInfo{
				Id:                 sid,
				ServerId:           m.cfg.ServerID,
				UserId:             user.ID,
				UserName:           user.Name,
				Client:             fallback(client.Client, "Ambra Client"),
				DeviceName:         fallback(client.Device, "Unknown Device"),
				DeviceId:           deviceID,
				ApplicationVersion: fallback(client.Version, "1.0.0"),
				LastActivityDate:   now,
				PlayState: dto.PlayerStateInfo{
					RepeatMode: "RepeatNone",
				},
				PlayableMediaTypes: []string{},
				SupportedCommands:  []string{},
			},
		}
		m.sessions[sid] = entry
	} else {
		entry.info.LastActivityDate = now
		entry.info.UserName = user.Name
		if client.Client != "" {
			entry.info.Client = client.Client
		}
		if client.Device != "" {
			entry.info.DeviceName = client.Device
		}
		if client.Version != "" {
			entry.info.ApplicationVersion = client.Version
		}
	}
	out := cloneSession(entry.info)
	m.mu.Unlock()
	return out
}

func (m *Manager) UpdateCapabilities(user *sqlite.UserRecord, client auth.ClientInfo, req CapabilitiesRequest) {
	if user == nil {
		return
	}
	info := m.TouchSession(user, client)

	m.mu.Lock()
	if entry, ok := m.sessions[info.Id]; ok {
		entry.info.SupportsRemoteControl = req.SupportsRemoteControl
		if req.PlayableMediaTypes != nil {
			entry.info.PlayableMediaTypes = append([]string(nil), req.PlayableMediaTypes...)
		}
		if req.SupportedCommands != nil {
			entry.info.SupportedCommands = append([]string(nil), req.SupportedCommands...)
		}
		entry.info.LastActivityDate = time.Now().UTC()
	}
	m.mu.Unlock()

	m.publish(context.Background(), eventbus.EventSessionUpdated, nil)
}

func (m *Manager) ReportPlaybackStart(ctx context.Context, user *sqlite.UserRecord, client auth.ClientInfo, req PlaybackStartRequest) error {
	if user == nil {
		return nil
	}
	info := m.TouchSession(user, client)
	itemID := guid.Normalize(req.ItemId)
	now := time.Now().UTC()

	m.mu.Lock()
	entry := m.sessions[info.Id]
	entry.info.LastActivityDate = now
	entry.playSessionID = req.PlaySessionId
	entry.lastPausedState = req.IsPaused
	entry.lastPersistAt = now

	if req.Item != nil {
		itemCopy := *req.Item
		if itemCopy.Id == "" {
			itemCopy.Id = itemID
		}
		if itemCopy.ServerId == "" {
			itemCopy.ServerId = m.cfg.ServerID
		}
		entry.info.NowPlayingItem = &itemCopy
	} else if itemID != "" {
		entry.info.NowPlayingItem = &dto.BaseItemDto{
			Id:       itemID,
			ServerId: m.cfg.ServerID,
		}
	}

	repeatMode := req.RepeatMode
	if repeatMode == "" {
		repeatMode = "RepeatNone"
	}
	entry.info.PlayState = dto.PlayerStateInfo{
		PositionTicks:       req.PositionTicks,
		CanSeek:             req.CanSeek,
		IsPaused:            req.IsPaused,
		IsMuted:             req.IsMuted,
		VolumeLevel:         req.VolumeLevel,
		AudioStreamIndex:    req.AudioStreamIndex,
		SubtitleStreamIndex: req.SubtitleStreamIndex,
		MediaSourceId:       req.MediaSourceId,
		PlayMethod:          req.PlayMethod,
		RepeatMode:          repeatMode,
	}
	m.mu.Unlock()

	if m.repo != nil && itemID != "" {
		_ = m.repo.CreateActivityLog(ctx, &sqlite.ActivityLogRecord{
			Name:     user.Name + " started playing",
			Type:     "VideoPlayback",
			ItemID:   itemID,
			UserID:   user.ID,
			Severity: "Info",
		})
	}

	m.publish(ctx, eventbus.EventPlaybackStart, PlaybackEventPayload{
		SessionId:     info.Id,
		UserId:        user.ID,
		UserName:      user.Name,
		DeviceId:      info.DeviceId,
		ItemId:        itemID,
		MediaSourceId: req.MediaSourceId,
		PlaySessionId: req.PlaySessionId,
		PositionTicks: req.PositionTicks,
	})
	m.publish(ctx, eventbus.EventSessionUpdated, nil)
	return nil
}

func (m *Manager) ReportPlaybackProgress(ctx context.Context, user *sqlite.UserRecord, client auth.ClientInfo, req PlaybackProgressRequest) error {
	if user == nil {
		return nil
	}
	info := m.TouchSession(user, client)
	itemID := guid.Normalize(req.ItemId)
	now := time.Now().UTC()

	shouldPersist := false
	m.mu.Lock()
	entry := m.sessions[info.Id]
	entry.info.LastActivityDate = now
	if req.PlaySessionId != "" {
		entry.playSessionID = req.PlaySessionId
	}
	if itemID != "" && entry.info.NowPlayingItem == nil {
		entry.info.NowPlayingItem = &dto.BaseItemDto{
			Id:       itemID,
			ServerId: m.cfg.ServerID,
		}
	}
	if itemID == "" && entry.info.NowPlayingItem != nil {
		itemID = guid.Normalize(entry.info.NowPlayingItem.Id)
	}

	pauseChanged := entry.lastPausedState != req.IsPaused
	entry.lastPausedState = req.IsPaused
	if pauseChanged || now.Sub(entry.lastPersistAt) >= progressPersistWindow {
		shouldPersist = true
		entry.lastPersistAt = now
	}

	entry.info.PlayState.PositionTicks = req.PositionTicks
	entry.info.PlayState.CanSeek = req.CanSeek
	entry.info.PlayState.IsPaused = req.IsPaused
	entry.info.PlayState.IsMuted = req.IsMuted
	if req.VolumeLevel > 0 {
		entry.info.PlayState.VolumeLevel = req.VolumeLevel
	}
	if req.AudioStreamIndex != nil {
		entry.info.PlayState.AudioStreamIndex = req.AudioStreamIndex
	}
	if req.SubtitleStreamIndex != nil {
		entry.info.PlayState.SubtitleStreamIndex = req.SubtitleStreamIndex
	}
	if req.MediaSourceId != "" {
		entry.info.PlayState.MediaSourceId = req.MediaSourceId
	}
	if req.PlayMethod != "" {
		entry.info.PlayState.PlayMethod = req.PlayMethod
	}
	if req.RepeatMode != "" {
		entry.info.PlayState.RepeatMode = req.RepeatMode
	}
	m.mu.Unlock()

	if shouldPersist && m.repo != nil && itemID != "" {
		pos := req.PositionTicks
		if pos < 0 {
			pos = 0
		}
		if ud, err := m.repo.UpsertPlaybackProgress(ctx, user.ID, itemID, pos, false, req.AudioStreamIndex, req.SubtitleStreamIndex); err == nil && ud != nil {
			m.publishUserDataChanged(ctx, user.ID, ud)
		}
	}

	m.publish(ctx, eventbus.EventPlaybackProgress, PlaybackEventPayload{
		SessionId:     info.Id,
		UserId:        user.ID,
		UserName:      user.Name,
		DeviceId:      info.DeviceId,
		ItemId:        itemID,
		MediaSourceId: req.MediaSourceId,
		PlaySessionId: req.PlaySessionId,
		PositionTicks: req.PositionTicks,
	})
	m.publish(ctx, eventbus.EventSessionUpdated, nil)
	return nil
}

func (m *Manager) ReportPlaybackStopped(ctx context.Context, user *sqlite.UserRecord, client auth.ClientInfo, req PlaybackStoppedRequest) error {
	if user == nil {
		return nil
	}
	info := m.TouchSession(user, client)
	itemID := guid.Normalize(req.ItemId)
	now := time.Now().UTC()

	var audioIdx, subIdx *int
	var runtimeTicks int64 = req.RuntimeTicks

	m.mu.Lock()
	entry := m.sessions[info.Id]
	entry.info.LastActivityDate = now
	if itemID == "" && entry.info.NowPlayingItem != nil {
		itemID = guid.Normalize(entry.info.NowPlayingItem.Id)
	}
	if runtimeTicks == 0 && entry.info.NowPlayingItem != nil && entry.info.NowPlayingItem.RunTimeTicks != nil {
		runtimeTicks = *entry.info.NowPlayingItem.RunTimeTicks
	}
	audioIdx = entry.info.PlayState.AudioStreamIndex
	subIdx = entry.info.PlayState.SubtitleStreamIndex

	entry.info.NowPlayingItem = nil
	entry.info.PlayState = dto.PlayerStateInfo{
		RepeatMode: "RepeatNone",
	}
	entry.playSessionID = ""
	m.mu.Unlock()

	if runtimeTicks == 0 && m.repo != nil && itemID != "" {
		if rt, err := m.repo.GetItemRuntimeTicks(ctx, itemID); err == nil && rt > 0 {
			runtimeTicks = rt
		}
	}

	threshold := m.cfg.PlayedThreshold
	if threshold <= 0 || threshold > 1 {
		threshold = defaultPlayedThreshold
	}

	pos := req.PositionTicks
	if pos < 0 {
		pos = 0
	}
	markPlayed := false
	if runtimeTicks > 0 && float64(pos)/float64(runtimeTicks) >= threshold {
		markPlayed = true
		pos = 0
	}

	var updatedData *sqlite.UserItemDataRecord
	if m.repo != nil && itemID != "" {
		ud, err := m.repo.UpsertPlaybackProgress(ctx, user.ID, itemID, pos, markPlayed, audioIdx, subIdx)
		if err == nil && ud != nil {
			updatedData = ud
		}
		_ = m.repo.CreateActivityLog(ctx, &sqlite.ActivityLogRecord{
			Name:     user.Name + " stopped playing",
			Type:     "VideoPlaybackStopped",
			ItemID:   itemID,
			UserID:   user.ID,
			Severity: "Info",
		})
	}

	if updatedData != nil {
		m.publishUserDataChanged(ctx, user.ID, updatedData)
	} else if itemID != "" {
		m.publish(ctx, eventbus.EventUserDataChanged, UserDataChangedPayload{
			UserId: user.ID,
			UserDataList: []UserDataChangedEntry{
				{
					ItemId:                itemID,
					PlaybackPositionTicks: pos,
					Played:                markPlayed,
				},
			},
		})
	}

	m.publish(ctx, eventbus.EventPlaybackStop, PlaybackEventPayload{
		SessionId:     info.Id,
		UserId:        user.ID,
		UserName:      user.Name,
		DeviceId:      info.DeviceId,
		ItemId:        itemID,
		MediaSourceId: req.MediaSourceId,
		PlaySessionId: req.PlaySessionId,
		PositionTicks: pos,
		Played:        markPlayed,
	})
	m.publish(ctx, eventbus.EventSessionUpdated, nil)
	return nil
}

func (m *Manager) ListSessions() []dto.SessionInfo {
	m.mu.RLock()
	out := make([]dto.SessionInfo, 0, len(m.sessions))
	for _, entry := range m.sessions {
		out = append(out, cloneSession(entry.info))
	}
	m.mu.RUnlock()

	sort.Slice(out, func(i, j int) bool {
		return out[i].LastActivityDate.After(out[j].LastActivityDate)
	})
	return out
}

func (m *Manager) GetSession(sessionID string) (dto.SessionInfo, bool) {
	m.mu.RLock()
	entry, ok := m.sessions[sessionID]
	m.mu.RUnlock()
	if !ok {
		return dto.SessionInfo{}, false
	}
	return cloneSession(entry.info), true
}

func (m *Manager) GetSessionByUserDevice(userID, deviceID string) (dto.SessionInfo, bool) {
	return m.GetSession(auth.SessionID(userID, deviceID))
}

func (m *Manager) RemoveSession(userID, deviceID string) {
	sid := auth.SessionID(userID, deviceID)
	m.mu.Lock()
	_, existed := m.sessions[sid]
	delete(m.sessions, sid)
	m.mu.Unlock()
	if existed {
		m.publish(context.Background(), eventbus.EventSessionUpdated, nil)
	}
}

func (m *Manager) RemoveSessionByDevice(deviceID string) {
	removed := false
	m.mu.Lock()
	for sid, entry := range m.sessions {
		if entry.info.DeviceId == deviceID {
			delete(m.sessions, sid)
			removed = true
		}
	}
	m.mu.Unlock()
	if removed {
		m.publish(context.Background(), eventbus.EventSessionUpdated, nil)
	}
}

func (m *Manager) publish(ctx context.Context, evType eventbus.EventType, payload any) {
	if m.bus == nil {
		return
	}
	m.bus.Publish(ctx, eventbus.Event{
		Type:    evType,
		Payload: payload,
	})
}

func (m *Manager) publishUserDataChanged(ctx context.Context, userID string, ud *sqlite.UserItemDataRecord) {
	var lastPlayed *time.Time
	if ud.LastPlayedDate != nil {
		t := time.Unix(*ud.LastPlayedDate, 0).UTC()
		lastPlayed = &t
	}
	m.publish(ctx, eventbus.EventUserDataChanged, UserDataChangedPayload{
		UserId: userID,
		UserDataList: []UserDataChangedEntry{
			{
				ItemId:                ud.ItemID,
				PlaybackPositionTicks: ud.PlaybackPositionTicks,
				PlayCount:             ud.PlayCount,
				IsFavorite:            ud.IsFavorite,
				Played:                ud.Played,
				LastPlayedDate:        lastPlayed,
			},
		},
	})
}

func cloneSession(in dto.SessionInfo) dto.SessionInfo {
	out := in
	if in.NowPlayingItem != nil {
		itemCopy := *in.NowPlayingItem
		out.NowPlayingItem = &itemCopy
	}
	if in.PlayableMediaTypes != nil {
		out.PlayableMediaTypes = append([]string(nil), in.PlayableMediaTypes...)
	}
	if in.SupportedCommands != nil {
		out.SupportedCommands = append([]string(nil), in.SupportedCommands...)
	}
	return out
}

func fallback(v, def string) string {
	if v == "" {
		return def
	}
	return v
}
