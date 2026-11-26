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
	MaxWidth            int            `json:"MaxWidth"`
	MaxHeight           int            `json:"MaxHeight"`
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
	userRepo *sqlite.UserRepo
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

// SetUserRepo attaches a UserRepo so playback decisions honor per-user UserConfiguration
// and per-item remembered audio/subtitle stream selections.
func (e *Engine) SetUserRepo(r *sqlite.UserRepo) {
	e.userRepo = r
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

	var userCfg *dto.UserConfiguration
	var userData *sqlite.UserItemDataRecord
	if e.userRepo != nil && strings.TrimSpace(req.UserID) != "" {
		if u, uErr := e.userRepo.GetUserByID(ctx, req.UserID); uErr == nil && u != nil {
			cfgCopy := u.Config
			userCfg = &cfgCopy
		}
		if ud, udErr := e.userRepo.GetUserItemData(ctx, req.UserID, item.ID); udErr == nil && ud != nil {
			userData = ud
		}
	}
	serverDefaultLang := ""
	if e.cfg != nil {
		serverDefaultLang = e.cfg.Snapshot().TMDBLanguage
	}

	sessionID := guid.New()
	var outSources []dto.MediaSourceInfo
	var details []DecisionDetail

	for _, src := range sources {
		if req.MediaSourceID != "" && !strings.EqualFold(guid.Normalize(req.MediaSourceID), guid.Normalize(src.ID)) {
			continue
		}
		msDTO, detail := e.decideOne(ctx, item, src, req, sessionID, userCfg, userData, serverDefaultLang)
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

func (e *Engine) decideOne(
	ctx context.Context,
	item *sqlite.ItemRecord,
	src sqlite.MediaSourceRecord,
	req PlaybackInfoRequest,
	sessionID string,
	userCfg *dto.UserConfiguration,
	userData *sqlite.UserItemDataRecord,
	serverDefaultLang string,
) (dto.MediaSourceInfo, DecisionDetail) {
	msDTO := sqlite.MediaSourceRecordsToDTO([]sqlite.MediaSourceRecord{src})[0]
	msDTO.Type = "Default"
	ApplyTrackSelection(&msDTO, userCfg, userData, serverDefaultLang)
	isAudio := strings.EqualFold(item.Type, "Audio") || strings.EqualFold(item.MediaType, "Audio")

	// Extract primary video/audio/subtitle codecs using unified default track selection
	var videoCodec, audioCodec string
	var selectedAudioCodec, defaultAudioCodec, firstAudioCodec string
	var videoWidth, videoHeight int
	var selectedSub *dto.MediaStream
	for i := range msDTO.MediaStreams {
		st := &msDTO.MediaStreams[i]
		switch st.Type {
		case "Video":
			if videoCodec == "" {
				videoCodec = strings.ToLower(st.Codec)
			}
			if videoWidth == 0 && st.Width > 0 {
				videoWidth = st.Width
			}
			if videoHeight == 0 && st.Height > 0 {
				videoHeight = st.Height
			}
		case "Audio":
			codecLower := strings.ToLower(st.Codec)
			if firstAudioCodec == "" {
				firstAudioCodec = codecLower
			}
			if req.AudioStreamIndex != nil && st.Index == *req.AudioStreamIndex {
				selectedAudioCodec = codecLower
			}
			if msDTO.DefaultAudioStreamIndex != nil && st.Index == *msDTO.DefaultAudioStreamIndex {
				defaultAudioCodec = codecLower
			}
		case "Subtitle":
			if req.SubtitleStreamIndex != nil {
				if st.Index == *req.SubtitleStreamIndex {
					selectedSub = st
				}
			} else if msDTO.DefaultSubtitleStreamIndex != nil && st.Index == *msDTO.DefaultSubtitleStreamIndex {
				selectedSub = st
			}
		}
	}
	if selectedAudioCodec != "" {
		audioCodec = selectedAudioCodec
	} else if defaultAudioCodec != "" {
		audioCodec = defaultAudioCodec
	} else {
		audioCodec = firstAudioCodec
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
		if strings.EqualFold(method, "External") {
			ext := subtitleStreamFormatForProfile(st.Codec, req.DeviceProfile)
			st.DeliveryUrl = fmt.Sprintf("/Videos/%s/%s/Subtitles/%d/Stream.%s", item.ID, msDTO.Id, st.Index, ext)
		}
		if selectedSub != nil && st.Index == selectedSub.Index && strings.EqualFold(method, "Encode") {
			subtitleNeedsBurnIn = true
		}
	}

	// Tier 1: STRM 302 DirectPlay
	if item.IsStrm || strings.HasSuffix(strings.ToLower(src.Path), ".strm") {
		if resolved, err := e.ResolveStream(ctx, src.Path); err == nil && resolved != nil && resolved.Is302 {
			msDTO.Protocol = "Http"
			msDTO.Path = resolved.URL
			msDTO.IsRemote = true
			msDTO.SupportsDirectPlay = true
			msDTO.SupportsDirectStream = true
			msDTO.SupportsTranscoding = false
			if isAudio {
				msDTO.DirectStreamUrl = buildDirectStreamURL("Audio", item.ID, src.Container, msDTO.Id, sessionID)
			} else {
				msDTO.DirectStreamUrl = buildDirectStreamURL("Videos", item.ID, src.Container, msDTO.Id, sessionID)
			}
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
	allowTranscoding := req.EnableTranscoding == nil || *req.EnableTranscoding

	maxBitrate := req.MaxStreamingBitrate
	if maxBitrate == 0 && req.DeviceProfile != nil {
		maxBitrate = req.DeviceProfile.MaxStreamingBitrate
	}
	bitrateOK := maxBitrate <= 0 || src.Bitrate <= 0 || src.Bitrate <= maxBitrate

	if isAudio {
		audioDirectPlayOK := checkAudioProfileCompatibility(src.Container, audioCodec, req.DeviceProfile)
		if allowDirectPlay && bitrateOK && audioDirectPlayOK {
			msDTO.SupportsDirectPlay = true
			msDTO.SupportsDirectStream = true
			msDTO.SupportsTranscoding = allowTranscoding
			msDTO.DirectStreamUrl = buildDirectStreamURL("Audio", item.ID, src.Container, msDTO.Id, sessionID)
			return msDTO, DecisionDetail{
				MediaSourceID: msDTO.Id,
				PlayMethod:    PlayMethodDirectPlay,
				Reason:        "Native audio direct play",
			}
		}
		if !allowTranscoding {
			msDTO.SupportsDirectPlay = false
			msDTO.SupportsDirectStream = false
			msDTO.SupportsTranscoding = false
			msDTO.TranscodingUrl = ""
			return msDTO, DecisionDetail{
				MediaSourceID: msDTO.Id,
				PlayMethod:    "",
				Reason:        "Transcoding disabled by request",
			}
		}
		msDTO.SupportsDirectPlay = false
		msDTO.SupportsDirectStream = false
		msDTO.SupportsTranscoding = true
		msDTO.TranscodingUrl = fmt.Sprintf("/Audio/%s/universal?MediaSourceId=%s&AudioCodec=aac&PlaySessionId=%s", item.ID, msDTO.Id, sessionID)
		return msDTO, DecisionDetail{
			MediaSourceID: msDTO.Id,
			PlayMethod:    PlayMethodTranscode,
			Reason:        "Server audio transcode",
		}
	}

	directPlayOK, videoOK := checkProfileCompatibility(src.Container, videoCodec, audioCodec, req.DeviceProfile)
	resOK := true
	if req.MaxWidth > 0 && videoWidth > req.MaxWidth {
		resOK = false
	}
	if req.MaxHeight > 0 && videoHeight > req.MaxHeight {
		resOK = false
	}

	// Tier 2: Native DirectPlay
	if allowDirectPlay && bitrateOK && resOK && directPlayOK && !subtitleNeedsBurnIn {
		msDTO.SupportsDirectPlay = true
		msDTO.SupportsDirectStream = true
		msDTO.SupportsTranscoding = allowTranscoding
		msDTO.DirectStreamUrl = buildDirectStreamURL("Videos", item.ID, src.Container, msDTO.Id, sessionID)
		return msDTO, DecisionDetail{
			MediaSourceID: msDTO.Id,
			PlayMethod:    PlayMethodDirectPlay,
			Reason:        "Native container/codec/subtitle direct play",
		}
	}

	// Tier 3: Client WASM Enhanced DirectPlay (e.g. Ambra Web ffmpeg.wasm + WebCodecs + JASSUB/PGS)
	if allowDirectPlay && bitrateOK && resOK && !subtitleNeedsBurnIn && supportsClientWASM(req.DeviceProfile) {
		msDTO.SupportsDirectPlay = true
		msDTO.SupportsDirectStream = true
		msDTO.SupportsTranscoding = allowTranscoding
		msDTO.DirectStreamUrl = buildDirectStreamURL("Videos", item.ID, src.Container, msDTO.Id, sessionID)
		return msDTO, DecisionDetail{
			MediaSourceID: msDTO.Id,
			PlayMethod:    PlayMethodClientWASM,
			Reason:        "Client WASM remux/decode direct stream",
		}
	}

	// Tier 4: Server DirectStream (video codec compatible, remux container or transcode audio only)
	if allowDirectStream && bitrateOK && resOK && videoOK && !subtitleNeedsBurnIn {
		msDTO.SupportsDirectPlay = false
		msDTO.SupportsDirectStream = true
		msDTO.SupportsTranscoding = allowTranscoding
		baseURL := fmt.Sprintf("/Videos/%s/master.m3u8?MediaSourceId=%s&PlaySessionId=%s&VideoCodec=copy&AudioCodec=aac", item.ID, msDTO.Id, sessionID)
		msDTO.TranscodingUrl = appendTranscodeQueryParams(baseURL, req, msDTO.DefaultAudioStreamIndex, maxBitrate)
		return msDTO, DecisionDetail{
			MediaSourceID: msDTO.Id,
			PlayMethod:    PlayMethodDirectStream,
			Reason:        "Video codec copy with server remux/audio transcode",
		}
	}

	// Tier 5: Server Transcode (full HLS transcoding)
	if !allowTranscoding {
		msDTO.SupportsDirectPlay = false
		msDTO.SupportsDirectStream = false
		msDTO.SupportsTranscoding = false
		msDTO.TranscodingUrl = ""
		return msDTO, DecisionDetail{
			MediaSourceID: msDTO.Id,
			PlayMethod:    "",
			Reason:        "Transcoding disabled by request",
		}
	}
	msDTO.SupportsDirectPlay = false
	msDTO.SupportsDirectStream = false
	msDTO.SupportsTranscoding = true
	baseURL := fmt.Sprintf("/Videos/%s/master.m3u8?MediaSourceId=%s&PlaySessionId=%s&VideoCodec=h264&AudioCodec=aac", item.ID, msDTO.Id, sessionID)
	transcodingURL := appendTranscodeQueryParams(baseURL, req, msDTO.DefaultAudioStreamIndex, maxBitrate)
	if subtitleNeedsBurnIn && selectedSub != nil {
		transcodingURL += fmt.Sprintf("&SubtitleStreamIndex=%d&SubtitleMethod=Encode", selectedSub.Index)
	}
	msDTO.TranscodingUrl = transcodingURL
	reason := "Full server HLS transcode"
	if subtitleNeedsBurnIn {
		reason = "Subtitle burn-in requested by client profile"
	} else if !resOK {
		reason = "Video resolution exceeds requested MaxWidth/MaxHeight"
	}
	return msDTO, DecisionDetail{
		MediaSourceID: msDTO.Id,
		PlayMethod:    PlayMethodTranscode,
		Reason:        reason,
	}
}

func sanitizeContainerExt(container string) string {
	ext := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(container), "."))
	if ext == "" || ext == "strm" || len(ext) > 16 {
		return ""
	}
	for i := 0; i < len(ext); i++ {
		c := ext[i]
		if !((c >= 'a' && c <= 'z') || (c >= '0' && c <= '9')) {
			return ""
		}
	}
	return ext
}

func buildDirectStreamURL(kind, itemID, container, mediaSourceID, sessionID string) string {
	if ext := sanitizeContainerExt(container); ext != "" {
		return fmt.Sprintf("/%s/%s/stream.%s?Static=true&MediaSourceId=%s&PlaySessionId=%s", kind, itemID, ext, mediaSourceID, sessionID)
	}
	return fmt.Sprintf("/%s/%s/stream?Static=true&MediaSourceId=%s&PlaySessionId=%s", kind, itemID, mediaSourceID, sessionID)
}

func appendTranscodeQueryParams(baseURL string, req PlaybackInfoRequest, defaultAudioIdx *int, maxBitrate int) string {
	const (
		defaultAudioBitrate = 192_000
		minVideoBitrate     = 256_000
	)
	out := baseURL
	audioIdx := req.AudioStreamIndex
	if audioIdx == nil {
		audioIdx = defaultAudioIdx
	}
	if audioIdx != nil && *audioIdx >= 0 {
		out += fmt.Sprintf("&AudioStreamIndex=%d", *audioIdx)
	}
	if maxBitrate > 0 {
		videoBitrate := maxBitrate - defaultAudioBitrate
		if videoBitrate < minVideoBitrate {
			videoBitrate = minVideoBitrate
		}
		out += fmt.Sprintf("&VideoBitrate=%d", videoBitrate)
	}
	if req.MaxWidth > 0 {
		out += fmt.Sprintf("&MaxWidth=%d", req.MaxWidth)
	}
	if req.MaxHeight > 0 {
		out += fmt.Sprintf("&MaxHeight=%d", req.MaxHeight)
	}
	return out
}

func checkAudioProfileCompatibility(container, audioCodec string, profile *DeviceProfile) bool {
	if profile == nil || len(profile.DirectPlayProfiles) == 0 {
		return true
	}
	container = strings.ToLower(container)
	audioCodec = strings.ToLower(audioCodec)

	for _, dp := range profile.DirectPlayProfiles {
		if dp.Type != "" && !strings.EqualFold(dp.Type, "Audio") {
			continue
		}
		cMatch := dp.Container == "" || csvContains(dp.Container, container)
		aMatch := dp.AudioCodec == "" || audioCodec == "" || csvContains(dp.AudioCodec, audioCodec)
		if cMatch && aMatch {
			return true
		}
	}
	return false
}

func checkProfileCompatibility(container, videoCodec, audioCodec string, profile *DeviceProfile) (directPlayOK, videoOK bool) {
	if profile == nil || len(profile.DirectPlayProfiles) == 0 {
		// No DeviceProfile provided -> trust client can DirectPlay
		return true, true
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
			return true, true
		}
		if vMatch {
			videoOK = true
		}
	}
	return false, videoOK
}

func isNativeNonWebClient(lowerName string) bool {
	for _, kw := range []string{
		"ios", "iphone", "ipad", "android", "tvos",
		"apple tv", "appletv", "android tv", "androidtv",
		"roku", "firetv", "kodi", "infuse", "mpv", "vlc", "swiftfin", " tv",
	} {
		if strings.Contains(lowerName, kw) {
			return true
		}
	}
	return strings.HasSuffix(lowerName, "tv")
}

func isWebBrowserProfile(profile *DeviceProfile) bool {
	if profile == nil {
		return false
	}
	lowerName := strings.ToLower(strings.TrimSpace(profile.Name))
	if lowerName == "" || isNativeNonWebClient(lowerName) {
		return false
	}
	return strings.Contains(lowerName, "web") ||
		strings.Contains(lowerName, "browser") ||
		strings.Contains(lowerName, "chrome") ||
		strings.Contains(lowerName, "safari") ||
		strings.Contains(lowerName, "firefox") ||
		strings.Contains(lowerName, "edge")
}

func supportsClientWASM(profile *DeviceProfile) bool {
	if profile == nil {
		return false
	}
	lowerName := strings.ToLower(strings.TrimSpace(profile.Name))
	if isNativeNonWebClient(lowerName) {
		return false
	}
	if profile.SupportsClientWASM {
		return true
	}
	if lowerName == "" {
		return false
	}
	return strings.Contains(lowerName, "ambra web") ||
		strings.Contains(lowerName, "wasm")
}

func profileSupportsExternalFormat(profile *DeviceProfile, normalizedCodec string) bool {
	if profile == nil {
		return false
	}
	for _, sp := range profile.SubtitleProfiles {
		if normalizeSubCodec(sp.Format) == normalizedCodec && (sp.Method == "" || strings.EqualFold(sp.Method, "External")) {
			return true
		}
	}
	return false
}

// subtitleStreamFormatForProfile selects the delivery extension for External subtitles.
// HTML5 browsers cannot natively parse .srt in <track>, so external SRT subtitles are served
// via Stream.vtt (on-the-fly WebVTT conversion) when the client is a Web browser or prefers VTT.
func subtitleStreamFormatForProfile(codec string, profile *DeviceProfile) string {
	ext := subtitleStreamFormat(codec)
	if ext == "srt" && profile != nil {
		if isWebBrowserProfile(profile) || (profileSupportsExternalFormat(profile, "vtt") && !profileSupportsExternalFormat(profile, "subrip")) {
			return "vtt"
		}
	}
	return ext
}

func normalizeSubtitleDeliveryMethod(method string) string {
	switch strings.ToLower(strings.TrimSpace(method)) {
	case "", "external":
		return "External"
	case "embed":
		return "Embed"
	case "encode":
		return "Encode"
	case "hls":
		return "Hls"
	case "drop":
		return "Drop"
	default:
		return strings.TrimSpace(method)
	}
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
			method := normalizeSubtitleDeliveryMethod(sp.Method)
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

	// Fallback: if client has a strict SubtitleProfiles list and explicitly omits bitmap or styled subtitles, burn in
	if codec == "pgssub" || codec == "dvdsub" || codec == "dvbsub" || codec == "ass" || codec == "ssa" {
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
	case "dvd_subtitle", "dvdsub", "vobsub":
		return "dvdsub"
	case "dvb_subtitle", "dvbsub":
		return "dvbsub"
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
