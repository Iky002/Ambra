package playback

import (
	"context"
	"fmt"
	"strings"

	"github.com/ambra-media/ambra/internal/api/dto"
	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/plugins"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

// PlayMethod enumerates the 5-tier playback decision outcomes.
type PlayMethod string

const (
	PlayMethodSTRM302      PlayMethod = "STRM302"
	PlayMethodDirectPlay   PlayMethod = "DirectPlay"
	PlayMethodClientWASM   PlayMethod = "ClientWASM"
	PlayMethodDirectStream PlayMethod = "DirectStream"
	PlayMethodTranscode    PlayMethod = "Transcode"
)

type DirectPlayProfile struct {
	Container  string `json:"Container"`
	AudioCodec string `json:"AudioCodec"`
	VideoCodec string `json:"VideoCodec"`
	Type       string `json:"Type"`
}

type SubtitleProfile struct {
	Format string `json:"Format"`
	Method string `json:"Method"` // External / Embed / Encode / Hls / Drop
}

type TranscodingProfile struct {
	Container  string `json:"Container"`
	Type       string `json:"Type"`
	VideoCodec string `json:"VideoCodec"`
	AudioCodec string `json:"AudioCodec"`
	Protocol   string `json:"Protocol"`
}

type DeviceProfile struct {
	Name                string               `json:"Name"`
	MaxStreamingBitrate int                  `json:"MaxStreamingBitrate"`
	SupportsClientWASM  bool                 `json:"SupportsClientWasm"`
	DirectPlayProfiles  []DirectPlayProfile  `json:"DirectPlayProfiles"`
	SubtitleProfiles    []SubtitleProfile    `json:"SubtitleProfiles"`
	TranscodingProfiles []TranscodingProfile `json:"TranscodingProfiles"`
}

type PlaybackInfoRequest struct {
	UserID              string         `json:"UserId"`
	MediaSourceID       string         `json:"MediaSourceId"`
	MaxStreamingBitrate int            `json:"MaxStreamingBitrate"`
	StartTimeTicks      int64          `json:"StartTimeTicks"`
	AudioStreamIndex    *int           `json:"AudioStreamIndex"`
	SubtitleStreamIndex *int           `json:"SubtitleStreamIndex"`
	EnableDirectPlay    *bool          `json:"EnableDirectPlay"`
	EnableDirectStream  *bool          `json:"EnableDirectStream"`
	EnableTranscoding   *bool          `json:"EnableTranscoding"`
	DeviceProfile       *DeviceProfile `json:"DeviceProfile"`
}

// DecisionDetail exposes the selected PlayMethod for testing/observability alongside the Emby DTO.
type DecisionDetail struct {
	MediaSourceID string
	PlayMethod    PlayMethod
	RedirectURL   string
	Reason        string
}

type Engine struct {
	repo     *sqlite.LibraryRepo
	registry *plugins.Registry
	cfg      *config.Config
}

func New(repo *sqlite.LibraryRepo, registry *plugins.Registry, cfg *config.Config) *Engine {
	return &Engine{
		repo:     repo,
		registry: registry,
		cfg:      cfg,
	}
}

// ResolveStream runs registered StreamResolvers (e.g. STRM 302) against a media path.
func (e *Engine) ResolveStream(ctx context.Context, rawPath string) (*plugins.StreamResolveResult, error) {
	if e.registry == nil {
		return nil, nil
	}
	for _, r := range e.registry.StreamResolvers() {
		res, err := r.ResolveStream(ctx, rawPath)
		if err != nil {
			continue
		}
		if res != nil && res.URL != "" {
			return res, nil
		}
	}
	return nil, nil
}

// Decide evaluates the 5-tier playback strategy for all (or the requested) MediaSources of an item.
func (e *Engine) Decide(ctx context.Context, itemID string, req PlaybackInfoRequest) (*dto.PlaybackInfoResponse, []DecisionDetail, error) {
	itemID = guid.Normalize(itemID)
	item, err := e.repo.GetItemRecordByID(ctx, itemID)
	if err != nil {
		return nil, nil, err
	}
	sources, err := e.repo.GetMediaSourcesByItemID(ctx, itemID)
	if err != nil {
		return nil, nil, err
	}
	if len(sources) == 0 && item.Path != "" {
		sources = []sqlite.MediaSourceRecord{{
			ID:                   item.ID,
			ItemID:               item.ID,
			Name:                 item.Name,
			Path:                 item.Path,
			Protocol:             "File",
			Container:            item.Container,
			IsRemote:             item.IsStrm,
			SupportsDirectPlay:   true,
			SupportsDirectStream: true,
			SupportsTranscoding:  true,
		}}
	}

	sessionID := guid.New()
	var outSources []dto.MediaSourceInfo
	var details []DecisionDetail

	for _, src := range sources {
		if req.MediaSourceID != "" && !strings.EqualFold(guid.Normalize(req.MediaSourceID), guid.Normalize(src.ID)) {
			continue
		}
		msDTO, detail := e.decideOne(ctx, item, src, req, sessionID)
		outSources = append(outSources, msDTO)
		details = append(details, detail)
	}

	if outSources == nil {
		outSources = []dto.MediaSourceInfo{}
	}
	return &dto.PlaybackInfoResponse{
		MediaSources:  outSources,
		PlaySessionId: sessionID,
	}, details, nil
}

func (e *Engine) decideOne(ctx context.Context, item *sqlite.ItemRecord, src sqlite.MediaSourceRecord, req PlaybackInfoRequest, sessionID string) (dto.MediaSourceInfo, DecisionDetail) {
	msDTO := sqlite.MediaSourceRecordsToDTO([]sqlite.MediaSourceRecord{src})[0]
	msDTO.Type = "Default"

	// Find default audio & subtitle indices and extract primary video/audio/subtitle codecs
	var videoCodec, audioCodec string
	var selectedSub *dto.MediaStream
	for i := range msDTO.MediaStreams {
		st := &msDTO.MediaStreams[i]
		switch st.Type {
		case "Video":
			if videoCodec == "" {
				videoCodec = strings.ToLower(st.Codec)
			}
		case "Audio":
			if audioCodec == "" {
				audioCodec = strings.ToLower(st.Codec)
			}
			if msDTO.DefaultAudioStreamIndex == nil && st.IsDefault {
				idx := st.Index
				msDTO.DefaultAudioStreamIndex = &idx
			}
		case "Subtitle":
			if msDTO.DefaultSubtitleStreamIndex == nil && st.IsDefault {
				idx := st.Index
				msDTO.DefaultSubtitleStreamIndex = &idx
			}
			if req.SubtitleStreamIndex != nil && st.Index == *req.SubtitleStreamIndex {
				selectedSub = st
			}
		}
	}

	// Assign subtitle delivery method and DeliveryUrl for every subtitle stream
	subtitleNeedsBurnIn := false
	for i := range msDTO.MediaStreams {
		st := &msDTO.MediaStreams[i]
		if st.Type != "Subtitle" {
			continue
		}
		method := decideSubtitleDeliveryMethod(st, req.DeviceProfile)
		st.DeliveryMethod = method
		if method == "External" {
			ext := subtitleStreamFormat(st.Codec)
			st.DeliveryUrl = fmt.Sprintf("/Videos/%s/%s/Subtitles/%d/Stream.%s", item.ID, msDTO.Id, st.Index, ext)
		}
		if selectedSub != nil && st.Index == selectedSub.Index && method == "Encode" {
			subtitleNeedsBurnIn = true
		}
	}

	// Tier 1: STRM 302 DirectPlay
	if item.IsStrm || strings.HasSuffix(strings.ToLower(src.Path), ".strm") {
		if resolved, err := e.ResolveStream(ctx, src.Path); err == nil && resolved != nil && resolved.Is302 {
			msDTO.Protocol = "Http"
			msDTO.IsRemote = true
			msDTO.SupportsDirectPlay = true
			msDTO.SupportsDirectStream = true
			msDTO.SupportsTranscoding = false
			msDTO.DirectStreamUrl = fmt.Sprintf("/Videos/%s/stream?Static=true&MediaSourceId=%s&PlaySessionId=%s", item.ID, msDTO.Id, sessionID)
			return msDTO, DecisionDetail{
				MediaSourceID: msDTO.Id,
				PlayMethod:    PlayMethodSTRM302,
				RedirectURL:   resolved.URL,
				Reason:        "STRM 302 direct redirect enabled",
			}
		}
	}

	allowDirectPlay := req.EnableDirectPlay == nil || *req.EnableDirectPlay
	allowDirectStream := req.EnableDirectStream == nil || *req.EnableDirectStream

	maxBitrate := req.MaxStreamingBitrate
	if maxBitrate == 0 && req.DeviceProfile != nil {
		maxBitrate = req.DeviceProfile.MaxStreamingBitrate
	}
	bitrateOK := maxBitrate <= 0 || src.Bitrate <= 0 || src.Bitrate <= maxBitrate

	containerOK, videoOK, audioOK := checkProfileCompatibility(src.Container, videoCodec, audioCodec, req.DeviceProfile)

	// Tier 2: Native DirectPlay
	if allowDirectPlay && bitrateOK && containerOK && videoOK && audioOK && !subtitleNeedsBurnIn {
		msDTO.SupportsDirectPlay = true
		msDTO.SupportsDirectStream = true
		msDTO.SupportsTranscoding = true
		msDTO.DirectStreamUrl = fmt.Sprintf("/Videos/%s/stream?Static=true&MediaSourceId=%s&PlaySessionId=%s", item.ID, msDTO.Id, sessionID)
		return msDTO, DecisionDetail{
			MediaSourceID: msDTO.Id,
			PlayMethod:    PlayMethodDirectPlay,
			Reason:        "Native container/codec/subtitle direct play",
		}
	}

	// Tier 3: Client WASM Enhanced DirectPlay (e.g. Ambra Web ffmpeg.wasm + WebCodecs + JASSUB/PGS)
	if allowDirectPlay && bitrateOK && !subtitleNeedsBurnIn && supportsClientWASM(req.DeviceProfile) {
		msDTO.SupportsDirectPlay = true
		msDTO.SupportsDirectStream = true
		msDTO.SupportsTranscoding = true
		msDTO.DirectStreamUrl = fmt.Sprintf("/Videos/%s/stream?Static=true&MediaSourceId=%s&PlaySessionId=%s", item.ID, msDTO.Id, sessionID)
		return msDTO, DecisionDetail{
			MediaSourceID: msDTO.Id,
			PlayMethod:    PlayMethodClientWASM,
			Reason:        "Client WASM remux/decode direct stream",
		}
	}

	// Tier 4: Server DirectStream (video codec compatible, remux container or transcode audio only)
	if allowDirectStream && bitrateOK && videoOK && !subtitleNeedsBurnIn {
		msDTO.SupportsDirectPlay = false
		msDTO.SupportsDirectStream = true
		msDTO.SupportsTranscoding = true
		msDTO.TranscodingUrl = fmt.Sprintf("/Videos/%s/master.m3u8?MediaSourceId=%s&PlaySessionId=%s&VideoCodec=copy&AudioCodec=aac", item.ID, msDTO.Id, sessionID)
		return msDTO, DecisionDetail{
			MediaSourceID: msDTO.Id,
			PlayMethod:    PlayMethodDirectStream,
			Reason:        "Video codec copy with server remux/audio transcode",
		}
	}

	// Tier 5: Server Transcode (full HLS transcoding)
	msDTO.SupportsDirectPlay = false
	msDTO.SupportsDirectStream = false
	msDTO.SupportsTranscoding = true
	msDTO.TranscodingUrl = fmt.Sprintf("/Videos/%s/master.m3u8?MediaSourceId=%s&PlaySessionId=%s&VideoCodec=h264&AudioCodec=aac", item.ID, msDTO.Id, sessionID)
	reason := "Full server HLS transcode"
	if subtitleNeedsBurnIn {
		reason = "Subtitle burn-in requested by client profile"
	}
	return msDTO, DecisionDetail{
		MediaSourceID: msDTO.Id,
		PlayMethod:    PlayMethodTranscode,
		Reason:        reason,
	}
}

func checkProfileCompatibility(container, videoCodec, audioCodec string, profile *DeviceProfile) (containerOK, videoOK, audioOK bool) {
	if profile == nil || len(profile.DirectPlayProfiles) == 0 {
		// No DeviceProfile provided -> trust client can DirectPlay
		return true, true, true
	}
	container = strings.ToLower(container)
	videoCodec = strings.ToLower(videoCodec)
	audioCodec = strings.ToLower(audioCodec)

	for _, dp := range profile.DirectPlayProfiles {
		if dp.Type != "" && !strings.EqualFold(dp.Type, "Video") {
			continue
		}
		cMatch := dp.Container == "" || csvContains(dp.Container, container)
		vMatch := dp.VideoCodec == "" || videoCodec == "" || csvContains(dp.VideoCodec, videoCodec)
		aMatch := dp.AudioCodec == "" || audioCodec == "" || csvContains(dp.AudioCodec, audioCodec)
		if cMatch && vMatch && aMatch {
			return true, true, true
		}
		if vMatch {
			videoOK = true
		}
		if cMatch {
			containerOK = true
		}
		if aMatch {
			audioOK = true
		}
	}
	return containerOK, videoOK, audioOK
}

func supportsClientWASM(profile *DeviceProfile) bool {
	if profile == nil {
		return false
	}
	if profile.SupportsClientWASM {
		return true
	}
	lowerName := strings.ToLower(profile.Name)
	return strings.Contains(lowerName, "ambra") || strings.Contains(lowerName, "wasm")
}

// decideSubtitleDeliveryMethod inspects DeviceProfile.SubtitleProfiles so that clients declaring
// ass/ssa/pgssub as External (e.g. JASSUB / PGS Canvas) NEVER trigger video transcoding.
func decideSubtitleDeliveryMethod(st *dto.MediaStream, profile *DeviceProfile) string {
	codec := normalizeSubCodec(st.Codec)
	if profile == nil || len(profile.SubtitleProfiles) == 0 {
		if st.IsExternal {
			return "External"
		}
		return "Embed"
	}

	for _, sp := range profile.SubtitleProfiles {
		if normalizeSubCodec(sp.Format) == codec {
			method := sp.Method
			if method == "" {
				method = "External"
			}
			// Prefer External for external subtitle files when client declares External or Embed
			if st.IsExternal && strings.EqualFold(method, "Embed") {
				return "External"
			}
			return method
		}
	}

	// Text subtitles (srt/vtt) can always be delivered as External VTT if client supports vtt
	if codec == "subrip" || codec == "vtt" {
		for _, sp := range profile.SubtitleProfiles {
			if normalizeSubCodec(sp.Format) == "vtt" && strings.EqualFold(sp.Method, "External") {
				return "External"
			}
		}
	}

	// Fallback: if client has a strict SubtitleProfiles list and explicitly omits ass/pgssub, burn in
	if codec == "pgssub" || codec == "ass" || codec == "ssa" {
		return "Encode"
	}
	return "External"
}

func normalizeSubCodec(c string) string {
	switch strings.ToLower(strings.TrimSpace(c)) {
	case "srt", "subrip":
		return "subrip"
	case "vtt", "webvtt":
		return "vtt"
	case "ass":
		return "ass"
	case "ssa":
		return "ssa"
	case "sup", "pgs", "pgssub", "hdmv_pgs_subtitle":
		return "pgssub"
	default:
		return strings.ToLower(strings.TrimSpace(c))
	}
}

func subtitleStreamFormat(codec string) string {
	switch normalizeSubCodec(codec) {
	case "ass":
		return "ass"
	case "ssa":
		return "ssa"
	case "pgssub":
		return "sup"
	case "vtt":
		return "vtt"
	default:
		return "srt"
	}
}

func csvContains(csv, target string) bool {
	if target == "" {
		return true
	}
	for _, part := range strings.Split(csv, ",") {
		p := strings.ToLower(strings.TrimSpace(part))
		if p == "*" || p == target {
			return true
		}
	}
	return false
}
