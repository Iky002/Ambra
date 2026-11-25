package session

import (
	"context"
	"errors"
	"log/slog"
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
	defaultPlayedThreshold       = 0.90
	progressPersistWindow        = 15 * time.Second
	maxSessions                  = 2048
	sessionStaleTTL              = 24 * time.Hour
	defaultActivityLogCooldown   = 30 * time.Second
	maxActivityLogThrottleKeys   = 4096
)

// ErrItemAccessDenied is returned when a playback report references a missing or unauthorized item.
var ErrItemAccessDenied = errors.New("session: item not found or access denied")

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
	ItemId              string `json:"ItemId"`
	MediaSourceId       string `json:"MediaSourceId"`
	PlaySessionId       string `json:"PlaySessionId"`
	PositionTicks       int64  `json:"PositionTicks"`
	RuntimeTicks        int64  `json:"RuntimeTicks,omitempty"`
	AudioStreamIndex    *int   `json:"AudioStreamIndex,omitempty"`
	SubtitleStreamIndex *int   `json:"SubtitleStreamIndex,omitempty"`
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
	RuntimeTicks  int64  `json:"RuntimeTicks,omitempty"`
	IsPaused      bool   `json:"IsPaused,omitempty"`
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

type activityThrottleKey struct {
	userID    string
	itemID    string
	eventType string
}

// Manager maintains active client sessions in memory and bridges playback progress to SQLite and EventBus.
type Manager struct {
	repo    *sqlite.UserRepo
	libRepo *sqlite.LibraryRepo
	cfg     *config.Config
	bus     *eventbus.Bus

	mu                  sync.RWMutex
	sessions            map[string]*sessionEntry // keyed by SessionInfo.Id
	itemAccessChecker   func(ctx context.Context, user *sqlite.UserRecord, itemID string) bool
	activityLogCooldown time.Duration
	activityLogThrottle map[activityThrottleKey]time.Time
}

func NewManager(repo *sqlite.UserRepo, cfg *config.Config, bus *eventbus.Bus) *Manager {
	var libRepo *sqlite.LibraryRepo
	if repo != nil && repo.Store() != nil {
		libRepo = sqlite.NewLibraryRepo(repo.Store())
	}
	return &Manager{
		repo:                repo,
		libRepo:             libRepo,
		cfg:                 cfg,
		bus:                 bus,
		sessions:            make(map[string]*sessionEntry),
		activityLogCooldown: defaultActivityLogCooldown,
		activityLogThrottle: make(map[activityThrottleKey]time.Time),
	}
}

// SetLibraryRepo overrides the library repository used to hydrate NowPlayingItem metadata.
func (m *Manager) SetLibraryRepo(libRepo *sqlite.LibraryRepo) {
	m.mu.Lock()
	m.libRepo = libRepo
	m.mu.Unlock()
}

// SetItemAccessChecker configures an optional RBAC callback verified on playback start/progress/stopped.
func (m *Manager) SetItemAccessChecker(fn func(ctx context.Context, user *sqlite.UserRecord, itemID string) bool) {
	m.mu.Lock()
	m.itemAccessChecker = fn
	m.mu.Unlock()
}

func (m *Manager) checkItemAccess(ctx context.Context, user *sqlite.UserRecord, itemID string) error {
	if itemID == "" {
		return nil
	}
	m.mu.RLock()
	checker := m.itemAccessChecker
	libRepo := m.libRepo
	m.mu.RUnlock()

	if checker != nil {
		if !checker(ctx, user, itemID) {
			return ErrItemAccessDenied
		}
		if libRepo != nil {
			rec, err := libRepo.GetItemRecordByID(ctx, itemID)
			if err != nil || rec == nil {
				return ErrItemAccessDenied
			}
		}
		return nil
	}
	if libRepo != nil {
		rec, err := libRepo.GetItemRecordByID(ctx, itemID)
		if err != nil || rec == nil {
			return ErrItemAccessDenied
		}
	}
	return nil
}

func (m *Manager) shouldRecordActivityLog(userID, itemID, eventType string, now time.Time) bool {
	key := activityThrottleKey{
		userID:    guid.Normalize(userID),
		itemID:    guid.Normalize(itemID),
		eventType: eventType,
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	cooldown := m.activityLogCooldown
	if cooldown <= 0 {
		cooldown = defaultActivityLogCooldown
	}
	if m.activityLogThrottle == nil {
		m.activityLogThrottle = make(map[activityThrottleKey]time.Time)
	}
	if last, ok := m.activityLogThrottle[key]; ok && now.Sub(last) < cooldown {
		return false
	}
	if len(m.activityLogThrottle) >= maxActivityLogThrottleKeys {
		for k, ts := range m.activityLogThrottle {
			if now.Sub(ts) >= cooldown {
				delete(m.activityLogThrottle, k)
			}
		}
		if len(m.activityLogThrottle) >= maxActivityLogThrottleKeys {
			m.activityLogThrottle = make(map[activityThrottleKey]time.Time)
		}
	}
	m.activityLogThrottle[key] = now
	return true
}

func (m *Manager) touchSessionLocked(user *sqlite.UserRecord, client auth.ClientInfo, now time.Time) *sessionEntry {
	deviceID := client.DeviceID
	if deviceID == "" {
		deviceID = "default-device"
	}
	sid := auth.SessionID(user.ID, deviceID)

	entry, ok := m.sessions[sid]
	if !ok {
		m.pruneSessionsLocked(now)
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
	return entry
}

func (m *Manager) pruneSessionsLocked(now time.Time) {
	for sid, entry := range m.sessions {
		if now.Sub(entry.info.LastActivityDate) > sessionStaleTTL {
			delete(m.sessions, sid)
		}
	}
	for len(m.sessions) >= maxSessions {
		var oldestID string
		var oldestTime time.Time
		for sid, entry := range m.sessions {
			if oldestID == "" || entry.info.LastActivityDate.Before(oldestTime) {
				oldestID = sid
				oldestTime = entry.info.LastActivityDate
			}
		}
		if oldestID == "" {
			break
		}
		delete(m.sessions, oldestID)
	}
}

func (m *Manager) TouchSession(user *sqlite.UserRecord, client auth.ClientInfo) dto.SessionInfo {
	if user == nil {
		return dto.SessionInfo{}
	}
	now := time.Now().UTC()

	m.mu.Lock()
	entry := m.touchSessionLocked(user, client, now)
	out := cloneSession(entry.info)
	m.mu.Unlock()
	return out
}

func (m *Manager) UpdateCapabilities(user *sqlite.UserRecord, client auth.ClientInfo, req CapabilitiesRequest) {
	if user == nil {
		return
	}
	now := time.Now().UTC()

	m.mu.Lock()
	entry := m.touchSessionLocked(user, client, now)
	entry.info.SupportsRemoteControl = req.SupportsRemoteControl
	if req.PlayableMediaTypes != nil {
		entry.info.PlayableMediaTypes = append([]string(nil), req.PlayableMediaTypes...)
	}
	if req.SupportedCommands != nil {
		entry.info.SupportedCommands = append([]string(nil), req.SupportedCommands...)
	}
	entry.info.LastActivityDate = now
	m.mu.Unlock()

	m.publish(context.Background(), eventbus.EventSessionUpdated, nil)
}

func userCanAccessLibrary(user *sqlite.UserRecord, libraryID string) bool {
	if user == nil || user.IsAdmin || user.Policy.IsAdministrator || user.Policy.EnableAllFolders {
		return true
	}
	normLib := guid.Normalize(libraryID)
	if normLib == "" || normLib == "collections" || normLib == "playlists" {
		return true
	}
	for _, allowed := range user.Policy.EnabledFolders {
		if guid.Normalize(allowed) == normLib {
			return true
		}
	}
	return false
}

func (m *Manager) hydrateNowPlayingItem(ctx context.Context, user *sqlite.UserRecord, dst *dto.BaseItemDto, itemID string) {
	if dst == nil || itemID == "" {
		return
	}
	m.mu.RLock()
	libRepo := m.libRepo
	m.mu.RUnlock()
	if libRepo == nil {
		return
	}
	rec, err := libRepo.GetItemRecordByID(ctx, itemID)
	if err != nil || rec == nil {
		return
	}
	if user != nil && !user.IsAdmin && !user.Policy.IsAdministrator {
		if !userCanAccessLibrary(user, rec.LibraryID) {
			return
		}
		blocked, err := libRepo.IsItemBlockedByParentalPolicy(ctx, rec, user.Policy.MaxParentalRating, user.Policy.BlockedTags)
		if err != nil || blocked {
			return
		}
	}
	if dst.Name == "" {
		dst.Name = rec.Name
	}
	if dst.Type == "" {
		dst.Type = rec.Type
	}
	if dst.MediaType == "" {
		dst.MediaType = rec.MediaType
	}
	if dst.SeriesName == "" {
		dst.SeriesName = rec.Extra.SeriesName
	}
	if dst.SeriesId == "" {
		dst.SeriesId = rec.SeriesID
	}
	if dst.SeasonId == "" {
		dst.SeasonId = rec.SeasonID
	}
	if dst.RunTimeTicks == nil && rec.RuntimeTicks != nil {
		rt := *rec.RuntimeTicks
		dst.RunTimeTicks = &rt
	}
	if dst.ProductionYear == nil && rec.ProductionYear != nil {
		yr := *rec.ProductionYear
		dst.ProductionYear = &yr
	}
	if dst.IndexNumber == nil && rec.IndexNumber != nil {
		idx := *rec.IndexNumber
		dst.IndexNumber = &idx
	}
	if dst.ParentIndexNumber == nil && rec.ParentIndexNumber != nil {
		pidx := *rec.ParentIndexNumber
		dst.ParentIndexNumber = &pidx
	}
	if dst.Container == "" {
		dst.Container = rec.Container
	}
}

func (m *Manager) ReportPlaybackStart(ctx context.Context, user *sqlite.UserRecord, client auth.ClientInfo, req PlaybackStartRequest) error {
	if user == nil {
		return nil
	}
	itemID := guid.Normalize(req.ItemId)
	if itemID == "" && req.Item != nil {
		itemID = guid.Normalize(req.Item.Id)
	}
	if err := m.checkItemAccess(ctx, user, itemID); err != nil {
		return err
	}
	now := time.Now().UTC()

	var nowPlaying *dto.BaseItemDto
	if req.Item != nil {
		itemCopy := *req.Item
		if itemCopy.Id == "" {
			itemCopy.Id = itemID
		} else if itemID == "" {
			itemID = guid.Normalize(itemCopy.Id)
		}
		if itemCopy.ServerId == "" {
			itemCopy.ServerId = m.cfg.ServerID
		}
		if itemCopy.Name == "" && itemID != "" {
			m.hydrateNowPlayingItem(ctx, user, &itemCopy, itemID)
		}
		nowPlaying = &itemCopy
	} else if itemID != "" {
		nowPlaying = &dto.BaseItemDto{
			Id:       itemID,
			ServerId: m.cfg.ServerID,
		}
		m.hydrateNowPlayingItem(ctx, user, nowPlaying, itemID)
	}

	var runtimeTicks int64
	if nowPlaying != nil && nowPlaying.RunTimeTicks != nil {
		runtimeTicks = *nowPlaying.RunTimeTicks
	}

	m.mu.Lock()
	entry := m.touchSessionLocked(user, client, now)
	sessionID := entry.info.Id
	deviceID := entry.info.DeviceId
	entry.info.LastActivityDate = now
	entry.playSessionID = req.PlaySessionId
	entry.lastPausedState = req.IsPaused
	entry.lastPersistAt = now
	if nowPlaying != nil {
		entry.info.NowPlayingItem = nowPlaying
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

	if m.repo != nil && itemID != "" && m.shouldRecordActivityLog(user.ID, itemID, "VideoPlayback", now) {
		if err := m.repo.CreateActivityLog(ctx, &sqlite.ActivityLogRecord{
			Name:     user.Name + " started playing",
			Type:     "VideoPlayback",
			ItemID:   itemID,
			UserID:   user.ID,
			Severity: "Info",
		}); err != nil {
			slog.Warn("session: failed to record playback start activity log", "user_id", user.ID, "item_id", itemID, "err", err)
		}
	}

	m.publish(ctx, eventbus.EventPlaybackStart, PlaybackEventPayload{
		SessionId:     sessionID,
		UserId:        user.ID,
		UserName:      user.Name,
		DeviceId:      deviceID,
		ItemId:        itemID,
		MediaSourceId: req.MediaSourceId,
		PlaySessionId: req.PlaySessionId,
		PositionTicks: req.PositionTicks,
		RuntimeTicks:  runtimeTicks,
		IsPaused:      req.IsPaused,
	})
	m.publish(ctx, eventbus.EventSessionUpdated, nil)
	return nil
}

func (m *Manager) ReportPlaybackProgress(ctx context.Context, user *sqlite.UserRecord, client auth.ClientInfo, req PlaybackProgressRequest) error {
	if user == nil {
		return nil
	}
	itemID := guid.Normalize(req.ItemId)
	if itemID == "" {
		deviceID := client.DeviceID
		if deviceID == "" {
			deviceID = "default-device"
		}
		sid := auth.SessionID(user.ID, deviceID)
		m.mu.RLock()
		if existing, ok := m.sessions[sid]; ok && existing.info.NowPlayingItem != nil {
			itemID = guid.Normalize(existing.info.NowPlayingItem.Id)
		}
		m.mu.RUnlock()
	}
	if err := m.checkItemAccess(ctx, user, itemID); err != nil {
		return err
	}
	now := time.Now().UTC()

	shouldPersist := false
	var runtimeTicks int64
	m.mu.Lock()
	entry := m.touchSessionLocked(user, client, now)
	sessionID := entry.info.Id
	deviceID := entry.info.DeviceId
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
	if entry.info.NowPlayingItem != nil && entry.info.NowPlayingItem.RunTimeTicks != nil {
		runtimeTicks = *entry.info.NowPlayingItem.RunTimeTicks
	}

	pauseChanged := entry.lastPausedState != req.IsPaused
	entry.lastPausedState = req.IsPaused
	audioChanged := req.AudioStreamIndex != nil && (entry.info.PlayState.AudioStreamIndex == nil || *entry.info.PlayState.AudioStreamIndex != *req.AudioStreamIndex)
	subChanged := req.SubtitleStreamIndex != nil && (entry.info.PlayState.SubtitleStreamIndex == nil || *entry.info.PlayState.SubtitleStreamIndex != *req.SubtitleStreamIndex)
	trackEvent := req.EventName == "AudioTrackChange" || req.EventName == "SubtitleTrackChange"
	if pauseChanged || audioChanged || subChanged || trackEvent || now.Sub(entry.lastPersistAt) >= progressPersistWindow {
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
		ud, err := m.repo.UpsertPlaybackProgress(ctx, user.ID, itemID, pos, false, req.AudioStreamIndex, req.SubtitleStreamIndex)
		if err != nil {
			slog.Warn("session: failed to persist playback progress", "user_id", user.ID, "item_id", itemID, "err", err)
		} else if ud != nil {
			m.publishUserDataChanged(ctx, user.ID, ud)
		}
	}

	m.publish(ctx, eventbus.EventPlaybackProgress, PlaybackEventPayload{
		SessionId:     sessionID,
		UserId:        user.ID,
		UserName:      user.Name,
		DeviceId:      deviceID,
		ItemId:        itemID,
		MediaSourceId: req.MediaSourceId,
		PlaySessionId: req.PlaySessionId,
		PositionTicks: req.PositionTicks,
		RuntimeTicks:  runtimeTicks,
		IsPaused:      req.IsPaused,
	})
	m.publish(ctx, eventbus.EventSessionUpdated, nil)
	return nil
}

func (m *Manager) ReportPlaybackStopped(ctx context.Context, user *sqlite.UserRecord, client auth.ClientInfo, req PlaybackStoppedRequest) error {
	if user == nil {
		return nil
	}
	itemID := guid.Normalize(req.ItemId)
	if itemID == "" {
		deviceID := client.DeviceID
		if deviceID == "" {
			deviceID = "default-device"
		}
		sid := auth.SessionID(user.ID, deviceID)
		m.mu.RLock()
		if existing, ok := m.sessions[sid]; ok && existing.info.NowPlayingItem != nil {
			itemID = guid.Normalize(existing.info.NowPlayingItem.Id)
		}
		m.mu.RUnlock()
	}
	if err := m.checkItemAccess(ctx, user, itemID); err != nil {
		return err
	}
	now := time.Now().UTC()

	var audioIdx, subIdx *int
	var nowPlayingRuntime int64

	m.mu.Lock()
	entry := m.touchSessionLocked(user, client, now)
	sessionID := entry.info.Id
	deviceID := entry.info.DeviceId
	entry.info.LastActivityDate = now
	if itemID == "" && entry.info.NowPlayingItem != nil {
		itemID = guid.Normalize(entry.info.NowPlayingItem.Id)
	}
	if entry.info.NowPlayingItem != nil && entry.info.NowPlayingItem.RunTimeTicks != nil {
		nowPlayingRuntime = *entry.info.NowPlayingItem.RunTimeTicks
	}
	audioIdx = entry.info.PlayState.AudioStreamIndex
	if req.AudioStreamIndex != nil {
		audioIdx = req.AudioStreamIndex
	}
	subIdx = entry.info.PlayState.SubtitleStreamIndex
	if req.SubtitleStreamIndex != nil {
		subIdx = req.SubtitleStreamIndex
	}

	entry.info.NowPlayingItem = nil
	entry.info.PlayState = dto.PlayerStateInfo{
		RepeatMode: "RepeatNone",
	}
	entry.playSessionID = ""
	m.mu.Unlock()

	// Prefer authoritative DB runtime_ticks when available instead of trusting client-supplied req.RuntimeTicks.
	var runtimeTicks int64
	if m.repo != nil && itemID != "" {
		if rt, err := m.repo.GetItemRuntimeTicks(ctx, itemID); err == nil && rt > 0 {
			runtimeTicks = rt
		}
	}
	if runtimeTicks <= 0 && nowPlayingRuntime > 0 {
		runtimeTicks = nowPlayingRuntime
	}
	if runtimeTicks <= 0 && req.RuntimeTicks > 0 {
		runtimeTicks = req.RuntimeTicks
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
		if err != nil {
			slog.Warn("session: failed to persist playback stop progress", "user_id", user.ID, "item_id", itemID, "err", err)
		} else if ud != nil {
			updatedData = ud
		}
		if m.shouldRecordActivityLog(user.ID, itemID, "VideoPlaybackStopped", now) {
			if err := m.repo.CreateActivityLog(ctx, &sqlite.ActivityLogRecord{
				Name:     user.Name + " stopped playing",
				Type:     "VideoPlaybackStopped",
				ItemID:   itemID,
				UserID:   user.ID,
				Severity: "Info",
			}); err != nil {
				slog.Warn("session: failed to record playback stop activity log", "user_id", user.ID, "item_id", itemID, "err", err)
			}
		}
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
		SessionId:     sessionID,
		UserId:        user.ID,
		UserName:      user.Name,
		DeviceId:      deviceID,
		ItemId:        itemID,
		MediaSourceId: req.MediaSourceId,
		PlaySessionId: req.PlaySessionId,
		PositionTicks: pos,
		RuntimeTicks:  runtimeTicks,
		IsPaused:      false,
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
	if !ok {
		m.mu.RUnlock()
		return dto.SessionInfo{}, false
	}
	out := cloneSession(entry.info)
	m.mu.RUnlock()
	return out, true
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

// RemoveSessionsByUser removes all active in-memory sessions belonging to userID.
func (m *Manager) RemoveSessionsByUser(userID string) {
	norm := guid.Normalize(userID)
	if norm == "" {
		return
	}
	removed := false
	m.mu.Lock()
	for sid, entry := range m.sessions {
		if guid.Normalize(entry.info.UserId) == norm {
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
