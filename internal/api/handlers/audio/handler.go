package audio

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/ambra-media/ambra/internal/api/httpx"
	"github.com/ambra-media/ambra/internal/api/middleware"
	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

const ticksPerSecond = 10_000_000

// AuthFuncs decouples route registration from the concrete middleware package.
type AuthFuncs struct {
	RequireAuth   func(http.Handler) http.Handler
	RequireAdmin  func(http.Handler) http.Handler
	UserIDFromCtx func(context.Context) string
}

// LyricMetadata holds parsed LRC header tags.
type LyricMetadata struct {
	Artist   string `json:"Artist,omitempty"`
	Album    string `json:"Album,omitempty"`
	Title    string `json:"Title,omitempty"`
	Author   string `json:"Author,omitempty"`
	IsSynced bool   `json:"IsSynced"`
}

// LyricLine represents a single timestamped lyric line in Emby/Jellyfin LyricDto format.
type LyricLine struct {
	Text  string `json:"Text"`
	Start *int64 `json:"Start,omitempty"` // Emby ticks (10,000,000 = 1s)
}

// LyricDto is the response payload for GET /Audio/{Id}/Lyrics.
type LyricDto struct {
	Metadata LyricMetadata `json:"Metadata"`
	Lyrics   []LyricLine   `json:"Lyrics"`
}

// Handler serves audio direct streams, universal audio streaming/transcoding, and lyrics.
type Handler struct {
	repo     *sqlite.LibraryRepo
	userRepo *sqlite.UserRepo
	cfg      *config.Config
	auth     AuthFuncs
}

// New constructs an Audio API handler.
func New(repo *sqlite.LibraryRepo, cfg *config.Config) *Handler {
	return &Handler{
		repo: repo,
		cfg:  cfg,
	}
}

// SetUserRepo attaches a UserRepo for per-user library access control on audio streams and lyrics.
func (h *Handler) SetUserRepo(userRepo *sqlite.UserRepo) {
	h.userRepo = userRepo
}

// CanAccessLibrary returns true if userID is permitted to access libraryID.
func (h *Handler) CanAccessLibrary(ctx context.Context, userID, libraryID string) bool {
	if h.userRepo == nil || userID == "" || libraryID == "" {
		return true
	}
	u, err := h.userRepo.GetUserByID(ctx, guid.Normalize(userID))
	if err != nil || u == nil {
		return true
	}
	if u.IsAdmin || u.Policy.IsAdministrator || u.Policy.EnableAllFolders {
		return true
	}
	normLib := guid.Normalize(libraryID)
	for _, fid := range u.Policy.EnabledFolders {
		if guid.Normalize(fid) == normLib {
			return true
		}
	}
	return false
}

func (h *Handler) canAccessItem(r *http.Request, itemID string) bool {
	if h.userRepo == nil || h.repo == nil {
		return true
	}
	var callerID string
	if ac, ok := middleware.AuthFromContext(r.Context()); ok && ac != nil && (ac.User != nil || ac.IsAPIKey) {
		if ac.IsAdmin() {
			return true
		}
		callerID = guid.Normalize(ac.UserID())
	} else if h.auth.UserIDFromCtx != nil {
		callerID = guid.Normalize(h.auth.UserIDFromCtx(r.Context()))
	}
	if callerID == "" {
		return true
	}
	rec, err := h.repo.GetItemRecordByID(r.Context(), itemID)
	if err != nil || rec == nil {
		return true
	}
	if rec.LibraryID != "" && !h.CanAccessLibrary(r.Context(), callerID, rec.LibraryID) {
		return false
	}
	u, err := h.userRepo.GetUserByID(r.Context(), callerID)
	if err != nil || u == nil || u.IsAdmin || u.Policy.IsAdministrator {
		return true
	}
	blocked, err := h.repo.IsItemBlockedByParentalPolicy(r.Context(), rec, u.Policy.MaxParentalRating, u.Policy.BlockedTags)
	if err != nil {
		return false
	}
	return !blocked
}

// RegisterRoutes mounts all /Audio/* endpoints on mux.
func (h *Handler) RegisterRoutes(mux *http.ServeMux, auth AuthFuncs) {
	h.auth = auth
	wrapAuth := auth.RequireAuth
	if wrapAuth == nil {
		wrapAuth = passthrough
	}

	// Note: In Go 1.22+ ServeMux, "GET ..." automatically matches both GET and HEAD.
	mux.Handle("GET /Audio/{Id}/Lyrics", wrapAuth(http.HandlerFunc(h.handleLyrics)))
	mux.Handle("GET /Audio/{Id}/{File}", wrapAuth(http.HandlerFunc(h.handleAudioFile)))
}

func (h *Handler) handleAudioFile(w http.ResponseWriter, r *http.Request) {
	fileSeg := strings.ToLower(r.PathValue("File"))
	switch {
	case fileSeg == "lyrics":
		h.handleLyrics(w, r)
	case fileSeg == "stream" || strings.HasPrefix(fileSeg, "stream."):
		h.handleDirectStream(w, r)
	case fileSeg == "universal" || strings.HasPrefix(fileSeg, "universal."):
		ext := ""
		if dot := strings.LastIndexByte(fileSeg, '.'); dot != -1 {
			ext = fileSeg[dot+1:]
		}
		h.handleUniversal(w, r, ext)
	default:
		httpx.Error(w, http.StatusNotFound, "unsupported audio endpoint")
	}
}

func (h *Handler) handleDirectStream(w http.ResponseWriter, r *http.Request) {
	itemID := guid.Normalize(r.PathValue("Id"))
	if !h.canAccessItem(r, itemID) {
		httpx.Error(w, http.StatusForbidden, "access denied")
		return
	}
	q := httpx.ParseQuery(r)
	mediaPath, _, err := h.resolveAudioPath(r.Context(), itemID, q.Get("MediaSourceId"))
	if errors.Is(err, sqlite.ErrNotFound) {
		httpx.Error(w, http.StatusNotFound, "audio item not found")
		return
	}
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	serveAudioFile(w, r, mediaPath)
}

func (h *Handler) handleUniversal(w http.ResponseWriter, r *http.Request, urlExt string) {
	itemID := guid.Normalize(r.PathValue("Id"))
	if !h.canAccessItem(r, itemID) {
		httpx.Error(w, http.StatusForbidden, "access denied")
		return
	}
	q := httpx.ParseQuery(r)
	mediaPath, srcBitrate, err := h.resolveAudioPath(r.Context(), itemID, q.Get("MediaSourceId"))
	if errors.Is(err, sqlite.ErrNotFound) {
		httpx.Error(w, http.StatusNotFound, "audio item not found")
		return
	}
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	srcExt := strings.ToLower(strings.TrimPrefix(filepath.Ext(mediaPath), "."))
	allowedContainers := q.List("Container")
	maxBitrate := q.Int("MaxStreamingBitrate", 0)
	if maxBitrate <= 0 {
		maxBitrate = q.Int("AudioBitrate", 0)
	}

	// Direct play if source container is supported by client and within bitrate limit
	bitrateOK := maxBitrate <= 0 || srcBitrate <= 0 || srcBitrate <= maxBitrate
	if bitrateOK && isContainerAllowed(srcExt, allowedContainers) && (urlExt == "" || urlExt == srcExt) {
		serveAudioFile(w, r, mediaPath)
		return
	}

	// Otherwise stream-transcode via FFmpeg to requested transcoding container/codec
	targetContainer := strings.ToLower(strings.TrimPrefix(q.Get("TranscodingContainer"), "."))
	if targetContainer == "" {
		targetContainer = urlExt
	}
	if targetContainer == "" {
		targetContainer = "mp3"
	}
	targetBitrate := q.Int("AudioBitrate", 192_000)
	if maxBitrate > 0 && maxBitrate < targetBitrate {
		targetBitrate = maxBitrate
	}
	if targetBitrate <= 0 {
		targetBitrate = 192_000
	}

	startTimeTicks := q.Int64("StartTimeTicks", 0)
	audioChannels := q.Int("AudioChannels", 0)
	if audioChannels <= 0 {
		audioChannels = q.Int("MaxAudioChannels", 0)
	}
	audioSampleRate := q.Int("AudioSampleRate", 0)

	h.streamTranscodeAudio(w, r, mediaPath, targetContainer, q.Get("AudioCodec"), targetBitrate, startTimeTicks, audioChannels, audioSampleRate)
}

func (h *Handler) streamTranscodeAudio(w http.ResponseWriter, r *http.Request, mediaPath, targetContainer, targetCodec string, bitrate int, startTimeTicks int64, audioChannels, audioSampleRate int) {
	if _, err := os.Stat(mediaPath); err != nil {
		if os.IsNotExist(err) {
			httpx.Error(w, http.StatusNotFound, "audio file missing on disk")
			return
		}
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	ct, muxFmt, encoder := resolveAudioTranscodeTarget(targetContainer, targetCodec)
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Cache-Control", "no-store")
	if r.Method == http.MethodHead {
		w.WriteHeader(http.StatusOK)
		return
	}

	ffmpegPath := "ffmpeg"
	if h.cfg != nil && h.cfg.FFmpegPath != "" {
		ffmpegPath = h.cfg.FFmpegPath
	}

	args := buildAudioFFmpegArgs(mediaPath, muxFmt, encoder, bitrate, startTimeTicks, audioChannels, audioSampleRate)
	cmd := exec.CommandContext(r.Context(), ffmpegPath, args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := cmd.Start(); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "failed to start ffmpeg: "+err.Error())
		return
	}

	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, stdout)
	_ = cmd.Wait()
}

func buildAudioFFmpegArgs(mediaPath, muxFmt, encoder string, bitrate int, startTimeTicks int64, audioChannels, audioSampleRate int) []string {
	args := []string{"-hide_banner", "-loglevel", "error"}
	if startTimeTicks > 0 {
		secs := float64(startTimeTicks) / float64(ticksPerSecond)
		args = append(args, "-ss", strconv.FormatFloat(secs, 'f', 3, 64))
	}
	args = append(args,
		"-i", mediaPath,
		"-vn",
		"-c:a", encoder,
		"-b:a", strconv.Itoa(bitrate),
	)
	if audioChannels > 0 {
		args = append(args, "-ac", strconv.Itoa(audioChannels))
	}
	if audioSampleRate > 0 {
		args = append(args, "-ar", strconv.Itoa(audioSampleRate))
	}
	if muxFmt == "mp4" {
		args = append(args, "-movflags", "frag_keyframe+empty_moov")
	}
	args = append(args, "-f", muxFmt, "pipe:1")
	return args
}

func (h *Handler) handleLyrics(w http.ResponseWriter, r *http.Request) {
	itemID := guid.Normalize(r.PathValue("Id"))
	if !h.canAccessItem(r, itemID) {
		httpx.Error(w, http.StatusForbidden, "access denied")
		return
	}
	q := httpx.ParseQuery(r)
	mediaPath, _, err := h.resolveAudioPath(r.Context(), itemID, q.Get("MediaSourceId"))
	if errors.Is(err, sqlite.ErrNotFound) {
		httpx.Error(w, http.StatusNotFound, "audio item not found")
		return
	}
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	lrcPath := findLRCFile(mediaPath)
	if lrcPath == "" {
		httpx.Error(w, http.StatusNotFound, "lyrics not found")
		return
	}
	raw, err := os.ReadFile(lrcPath)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	dto := ParseLRC(string(raw))
	httpx.JSON(w, http.StatusOK, dto)
}

func (h *Handler) resolveAudioPath(ctx context.Context, itemID, mediaSourceID string) (string, int, error) {
	if h.repo == nil {
		return "", 0, sqlite.ErrNotFound
	}
	sources, err := h.repo.GetMediaSourcesByItemID(ctx, itemID)
	if err == nil && len(sources) > 0 {
		normMS := guid.Normalize(mediaSourceID)
		if normMS != "" {
			for _, s := range sources {
				if guid.Normalize(s.ID) == normMS {
					return s.Path, s.Bitrate, nil
				}
			}
		}
		return sources[0].Path, sources[0].Bitrate, nil
	}
	item, err := h.repo.GetItemRecordByID(ctx, itemID)
	if err != nil {
		return "", 0, err
	}
	if item.Path == "" {
		return "", 0, sqlite.ErrNotFound
	}
	return item.Path, 0, nil
}

func serveAudioFile(w http.ResponseWriter, r *http.Request, mediaPath string) {
	f, err := os.Open(mediaPath)
	if err != nil {
		if os.IsNotExist(err) {
			httpx.Error(w, http.StatusNotFound, "audio file missing on disk")
			return
		}
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if ct := audioContentType(mediaPath); ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	http.ServeContent(w, r, stat.Name(), stat.ModTime(), f)
}

func isContainerAllowed(srcExt string, allowed []string) bool {
	if len(allowed) == 0 {
		return true
	}
	srcExt = strings.ToLower(strings.TrimPrefix(srcExt, "."))
	for _, a := range allowed {
		a = strings.ToLower(strings.TrimSpace(a))
		if a == "*" || a == srcExt {
			return true
		}
	}
	return false
}

func resolveAudioTranscodeTarget(container, codec string) (contentType, muxFmt, encoder string) {
	switch strings.ToLower(strings.TrimSpace(container)) {
	case "aac", "adts":
		return "audio/aac", "adts", "aac"
	case "m4a", "mp4":
		return "audio/mp4", "mp4", "aac"
	case "ogg", "opus":
		if strings.EqualFold(codec, "vorbis") {
			return "audio/ogg", "ogg", "libvorbis"
		}
		return "audio/ogg", "ogg", "libopus"
	case "flac":
		return "audio/flac", "flac", "flac"
	case "wav":
		return "audio/wav", "wav", "pcm_s16le"
	default:
		if strings.EqualFold(codec, "aac") {
			return "audio/aac", "adts", "aac"
		}
		return "audio/mpeg", "mp3", "libmp3lame"
	}
}

func audioContentType(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".mp3":
		return "audio/mpeg"
	case ".flac":
		return "audio/flac"
	case ".m4a", ".mp4", ".alac":
		return "audio/mp4"
	case ".aac":
		return "audio/aac"
	case ".ogg", ".oga":
		return "audio/ogg"
	case ".opus":
		return "audio/opus"
	case ".wav":
		return "audio/wav"
	case ".wma":
		return "audio/x-ms-wma"
	default:
		return "application/octet-stream"
	}
}

func findLRCFile(audioPath string) string {
	base := strings.TrimSuffix(audioPath, filepath.Ext(audioPath))
	for _, ext := range []string{".lrc", ".LRC"} {
		candidate := base + ext
		if fi, err := os.Stat(candidate); err == nil && !fi.IsDir() {
			return candidate
		}
	}
	return ""
}

var (
	lrcTimeRe = regexp.MustCompile(`\[(\d{1,3}):(\d{2})(?:[.:](\d{1,3}))?\]`)
	lrcTagRe  = regexp.MustCompile(`^\[([a-zA-Z]+):(.*)\]$`)
)

// ParseLRC parses standard LRC lyrics text into an Emby-compatible LyricDto.
// Supports multiple timestamps per line (e.g. [00:12.00][01:20.50]Chorus), metadata tags,
// and [offset:ms] adjustment.
func ParseLRC(content string) *LyricDto {
	content = strings.TrimPrefix(content, "\ufeff")
	out := &LyricDto{
		Lyrics: []LyricLine{},
	}

	var offsetMs int64
	type rawEntry struct {
		ticks int64
		text  string
		order int
	}
	var synced []rawEntry
	var unsynced []LyricLine
	order := 0

	for _, rawLine := range strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" {
			continue
		}

		matches := lrcTimeRe.FindAllStringSubmatchIndex(line, -1)
		if len(matches) > 0 {
			text := strings.TrimSpace(lrcTimeRe.ReplaceAllString(line, ""))
			for _, m := range matches {
				sub := lrcTimeRe.FindStringSubmatch(line[m[0]:m[1]])
				if len(sub) < 3 {
					continue
				}
				mins, _ := strconv.ParseInt(sub[1], 10, 64)
				secs, _ := strconv.ParseInt(sub[2], 10, 64)
				var ms int64
				if len(sub) >= 4 && sub[3] != "" {
					frac := sub[3]
					switch len(frac) {
					case 1:
						v, _ := strconv.ParseInt(frac, 10, 64)
						ms = v * 100
					case 2:
						v, _ := strconv.ParseInt(frac, 10, 64)
						ms = v * 10
					default:
						v, _ := strconv.ParseInt(frac[:3], 10, 64)
						ms = v
					}
				}
				totalMs := mins*60_000 + secs*1000 + ms
				synced = append(synced, rawEntry{
					ticks: totalMs * (ticksPerSecond / 1000),
					text:  text,
					order: order,
				})
				order++
			}
			continue
		}

		// Metadata tag like [ar:Artist] or [offset:+500]
		if tag := lrcTagRe.FindStringSubmatch(line); len(tag) == 3 {
			key := strings.ToLower(strings.TrimSpace(tag[1]))
			val := strings.TrimSpace(tag[2])
			switch key {
			case "ar":
				out.Metadata.Artist = val
			case "ti":
				out.Metadata.Title = val
			case "al":
				out.Metadata.Album = val
			case "by", "au":
				out.Metadata.Author = val
			case "offset":
				if v, err := strconv.ParseInt(val, 10, 64); err == nil {
					offsetMs = v
				}
			}
			continue
		}

		unsynced = append(unsynced, LyricLine{Text: line})
	}

	if len(synced) > 0 {
		out.Metadata.IsSynced = true
		offsetTicks := offsetMs * (ticksPerSecond / 1000)
		for i := range synced {
			synced[i].ticks += offsetTicks
			if synced[i].ticks < 0 {
				synced[i].ticks = 0
			}
		}
		sort.SliceStable(synced, func(i, j int) bool {
			if synced[i].ticks == synced[j].ticks {
				return synced[i].order < synced[j].order
			}
			return synced[i].ticks < synced[j].ticks
		})
		for _, e := range synced {
			t := e.ticks
			out.Lyrics = append(out.Lyrics, LyricLine{
				Text:  e.text,
				Start: &t,
			})
		}
	} else if len(unsynced) > 0 {
		out.Lyrics = unsynced
	}

	return out
}

func passthrough(next http.Handler) http.Handler { return next }
