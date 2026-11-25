package dlna

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ambra-media/ambra/internal/api/httpx"
	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

const (
	profilesStoreKey     = "dlna:profiles"
	defaultProfileID     = "default"
	defaultSubTimeout    = 1800
	minSubTimeout        = 60
	maxSubTimeout        = 86400
	maxDLNASubscriptions = 256
	dlnaStreamTokenTTL   = 24 * time.Hour
	defaultServerHeader  = "Ambra/4.10.2.0 UPnP/1.0 DLNADOC/1.50"
)

// AuthFuncs decouples route registration from the concrete middleware package.
type AuthFuncs struct {
	RequireAuth   func(http.Handler) http.Handler
	RequireAdmin  func(http.Handler) http.Handler
	UserIDFromCtx func(context.Context) string
}

// DlnaProfile represents an Emby-compatible DLNA device profile.
type DlnaProfile struct {
	Id                          string           `json:"Id"`
	Name                        string           `json:"Name"`
	Type                        string           `json:"Type,omitempty"`
	FriendlyName                string           `json:"FriendlyName,omitempty"`
	Manufacturer                string           `json:"Manufacturer,omitempty"`
	ManufacturerUrl             string           `json:"ManufacturerUrl,omitempty"`
	ModelName                   string           `json:"ModelName,omitempty"`
	ModelDescription            string           `json:"ModelDescription,omitempty"`
	ModelNumber                 string           `json:"ModelNumber,omitempty"`
	ModelUrl                    string           `json:"ModelUrl,omitempty"`
	SerialNumber                string           `json:"SerialNumber,omitempty"`
	EnableAlbumArtInDidl        bool             `json:"EnableAlbumArtInDidl"`
	EnableSingleAlbumArtLimit   bool             `json:"EnableSingleAlbumArtLimit"`
	EnableSingleSubtitleLimit   bool             `json:"EnableSingleSubtitleLimit"`
	SupportedMediaTypes         string           `json:"SupportedMediaTypes,omitempty"`
	ProtocolInfo                string           `json:"ProtocolInfo,omitempty"`
	TimelineOffsetSeconds       int              `json:"TimelineOffsetSeconds"`
	RequiresPlainVideoItems     bool             `json:"RequiresPlainVideoItems"`
	RequiresPlainFolders        bool             `json:"RequiresPlainFolders"`
	MaxStreamingBitrate         *int64           `json:"MaxStreamingBitrate,omitempty"`
	MaxStaticMusicBitrate       *int             `json:"MaxStaticMusicBitrate,omitempty"`
	MaxAlbumArtWidth            int              `json:"MaxAlbumArtWidth,omitempty"`
	MaxAlbumArtHeight           int              `json:"MaxAlbumArtHeight,omitempty"`
	DirectPlayProfiles          []map[string]any `json:"DirectPlayProfiles,omitempty"`
	TranscodingProfiles         []map[string]any `json:"TranscodingProfiles,omitempty"`
	ContainerProfiles           []map[string]any `json:"ContainerProfiles,omitempty"`
	CodecProfiles               []map[string]any `json:"CodecProfiles,omitempty"`
	SubtitleProfiles            []map[string]any `json:"SubtitleProfiles,omitempty"`
	ResponseProfiles            []map[string]any `json:"ResponseProfiles,omitempty"`
	Identification              map[string]any   `json:"Identification,omitempty"`
}

type eventSubscription struct {
	SID       string
	Service   string
	UUID      string
	Callback  string
	ExpiresAt time.Time
}

// Handler implements Emby-compatible DLNA Profile management and UPnP MediaServer:1 endpoints.
type Handler struct {
	cfg          *config.Config
	libRepo      *sqlite.LibraryRepo
	store        *sqlite.Store
	auth         AuthFuncs
	streamSecret []byte

	mu            sync.RWMutex
	profiles      map[string]DlnaProfile
	subscriptions map[string]eventSubscription
	systemUpdate  uint32
}

// New constructs a DLNA handler backed by LibraryRepo and optional SQLite Store for profile persistence.
func New(cfg *config.Config, libRepo *sqlite.LibraryRepo, store *sqlite.Store) *Handler {
	if cfg == nil {
		cfg = config.Default("./data")
	}
	randBytes := make([]byte, 32)
	_, _ = rand.Read(randBytes)
	secretInput := append([]byte(cfg.ServerID+":"), randBytes...)
	secretSum := sha256.Sum256(secretInput)

	h := &Handler{
		cfg:           cfg,
		libRepo:       libRepo,
		store:         store,
		streamSecret:  secretSum[:],
		profiles:      make(map[string]DlnaProfile),
		subscriptions: make(map[string]eventSubscription),
		systemUpdate:  1,
	}
	def := DefaultProfile()
	h.profiles[def.Id] = def
	h.loadPersistedProfiles(context.Background())
	return h
}

// DefaultProfile returns the built-in default DLNA device profile.
func DefaultProfile() DlnaProfile {
	maxBitrate := int64(140000000)
	return DlnaProfile{
		Id:                        defaultProfileID,
		Name:                      "Default",
		Type:                      "System",
		FriendlyName:              "Ambra Generic DLNA Client",
		Manufacturer:              "Ambra",
		ModelName:                 "Generic Media Renderer",
		ModelDescription:          "Default DLNA Profile",
		EnableAlbumArtInDidl:      true,
		EnableSingleAlbumArtLimit: false,
		SupportedMediaTypes:       "Audio,Photo,Video",
		ProtocolInfo:              defaultProtocolInfoSource,
		MaxStreamingBitrate:       &maxBitrate,
		MaxAlbumArtWidth:          1024,
		MaxAlbumArtHeight:         1024,
		DirectPlayProfiles: []map[string]any{
			{"Container": "mp4,m4v,mkv,webm,mov", "Type": "Video"},
			{"Container": "mp3,flac,aac,ogg,wav", "Type": "Audio"},
			{"Container": "jpeg,jpg,png,webp", "Type": "Photo"},
		},
	}
}

// RegisterRoutes mounts all DLNA profile management and UPnP MediaServer routes onto mux.
func (h *Handler) RegisterRoutes(mux *http.ServeMux, auth AuthFuncs) {
	h.auth = auth
	wrapAuth := auth.RequireAuth
	if wrapAuth == nil {
		wrapAuth = passthrough
	}
	wrapAdmin := auth.RequireAdmin
	if wrapAdmin == nil {
		wrapAdmin = wrapAuth
	}

	handleAdmin := func(pattern string, fn http.HandlerFunc) {
		mux.Handle(pattern, wrapAdmin(fn))
	}

	// Admin profile management endpoints
	handleAdmin("GET /Dlna/ProfileInfos", h.handleListProfileInfos)
	handleAdmin("GET /Dlna/Profiles/Default", h.handleGetDefaultProfile)
	handleAdmin("POST /Dlna/Profiles", h.handleCreateProfile)
	handleAdmin("POST /Dlna/Profiles/{Id}", h.handleUpdateProfile)
	handleAdmin("DELETE /Dlna/Profiles/{Id}", h.handleDeleteProfile)

	// Unified 2-segment GET/HEAD dispatcher to avoid Go ServeMux ambiguity between
	// /Dlna/Profiles/{Id}, /Dlna/icons/{Filename}, and /Dlna/{UuId}/description(.xml).
	mux.HandleFunc("GET /Dlna/{seg1}/{seg2}", func(w http.ResponseWriter, r *http.Request) {
		seg1 := r.PathValue("seg1")
		seg2 := r.PathValue("seg2")
		switch {
		case seg1 == "Profiles":
			wrapAdmin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				h.serveProfileByID(w, seg2)
			})).ServeHTTP(w, r)
		case seg1 == "icons":
			h.serveIcon(w, r, seg2)
		case seg2 == "description.xml" || seg2 == "description":
			h.serveDescriptionXML(w, r, seg1)
		default:
			httpx.Error(w, http.StatusNotFound, "not found")
		}
	})

	// Public UPnP SCPD documents (GET also matches HEAD in Go 1.22+ ServeMux)
	mux.HandleFunc("GET /Dlna/{UuId}/contentdirectory/contentdirectory.xml", h.handleContentDirectoryXML)
	mux.HandleFunc("GET /Dlna/{UuId}/contentdirectory/contentdirectory", h.handleContentDirectoryXML)
	mux.HandleFunc("GET /Dlna/{UuId}/connectionmanager/connectionmanager.xml", h.handleConnectionManagerXML)
	mux.HandleFunc("GET /Dlna/{UuId}/connectionmanager/connectionmanager", h.handleConnectionManagerXML)

	// Public UPnP SOAP control endpoints
	mux.HandleFunc("POST /Dlna/{UuId}/contentdirectory/control", h.handleContentDirectoryControl)
	mux.HandleFunc("POST /Dlna/{UuId}/connectionmanager/control", h.handleConnectionManagerControl)

	// Public server icons (3-segment form)
	mux.HandleFunc("GET /Dlna/{UuId}/icons/{Filename}", h.handleIcon)

	// Signed DLNA media stream endpoint (no RequireAuth; validated via HMAC sig & exp query params)
	mux.HandleFunc("GET /Dlna/{UuId}/stream/{Id}", h.handleDLNAStream)

	// Public UPnP GENA event subscriptions (SUBSCRIBE / UNSUBSCRIBE)
	mux.HandleFunc("SUBSCRIBE /Dlna/{UuId}/contentdirectory/events", h.handleEventSubscribe("contentdirectory"))
	mux.HandleFunc("UNSUBSCRIBE /Dlna/{UuId}/contentdirectory/events", h.handleEventUnsubscribe("contentdirectory"))
	mux.HandleFunc("SUBSCRIBE /Dlna/{UuId}/connectionmanager/events", h.handleEventSubscribe("connectionmanager"))
	mux.HandleFunc("UNSUBSCRIBE /Dlna/{UuId}/connectionmanager/events", h.handleEventUnsubscribe("connectionmanager"))
}

// --- Profile CRUD Handlers ---

func (h *Handler) handleListProfileInfos(w http.ResponseWriter, r *http.Request) {
	h.mu.RLock()
	out := make([]DlnaProfile, 0, len(h.profiles))
	for _, p := range h.profiles {
		out = append(out, p)
	}
	h.mu.RUnlock()

	sort.Slice(out, func(i, j int) bool {
		if out[i].Id == defaultProfileID {
			return true
		}
		if out[j].Id == defaultProfileID {
			return false
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	httpx.JSON(w, http.StatusOK, out)
}

func (h *Handler) handleGetDefaultProfile(w http.ResponseWriter, r *http.Request) {
	h.mu.RLock()
	p, ok := h.profiles[defaultProfileID]
	h.mu.RUnlock()
	if !ok {
		p = DefaultProfile()
	}
	httpx.JSON(w, http.StatusOK, p)
}

func (h *Handler) serveProfileByID(w http.ResponseWriter, id string) {
	h.mu.RLock()
	p, ok := h.profiles[id]
	h.mu.RUnlock()
	if !ok {
		httpx.Error(w, http.StatusNotFound, "dlna profile not found")
		return
	}
	httpx.JSON(w, http.StatusOK, p)
}

func (h *Handler) handleCreateProfile(w http.ResponseWriter, r *http.Request) {
	var p DlnaProfile
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid dlna profile payload")
		return
	}
	if strings.TrimSpace(p.Name) == "" {
		httpx.Error(w, http.StatusBadRequest, "profile Name is required")
		return
	}
	if strings.TrimSpace(p.Id) == "" {
		p.Id = guid.New()
	}
	if strings.TrimSpace(p.Type) == "" {
		p.Type = "User"
	}

	h.mu.Lock()
	h.profiles[p.Id] = p
	err := h.persistProfilesLocked(r.Context())
	h.mu.Unlock()
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("X-Profile-Id", p.Id)
	httpx.JSON(w, http.StatusOK, p)
}

func (h *Handler) handleUpdateProfile(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("Id")
	var p DlnaProfile
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid dlna profile payload")
		return
	}
	p.Id = id
	if strings.TrimSpace(p.Name) == "" {
		httpx.Error(w, http.StatusBadRequest, "profile Name is required")
		return
	}

	h.mu.Lock()
	if existing, ok := h.profiles[id]; ok && p.Type == "" {
		p.Type = existing.Type
	}
	if p.Type == "" {
		p.Type = "User"
	}
	h.profiles[id] = p
	err := h.persistProfilesLocked(r.Context())
	h.mu.Unlock()
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.NoContent(w)
}

func (h *Handler) handleDeleteProfile(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("Id")
	if id == defaultProfileID {
		httpx.Error(w, http.StatusBadRequest, "cannot delete built-in default profile")
		return
	}

	h.mu.Lock()
	if _, ok := h.profiles[id]; !ok {
		h.mu.Unlock()
		httpx.Error(w, http.StatusNotFound, "dlna profile not found")
		return
	}
	delete(h.profiles, id)
	err := h.persistProfilesLocked(r.Context())
	h.mu.Unlock()
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.NoContent(w)
}

func (h *Handler) loadPersistedProfiles(ctx context.Context) {
	if h.store == nil {
		return
	}
	var raw string
	err := h.store.Reader.QueryRowContext(ctx,
		`SELECT value_json FROM server_config WHERE key = ?`,
		profilesStoreKey,
	).Scan(&raw)
	if err != nil {
		return
	}
	var list []DlnaProfile
	if err := json.Unmarshal([]byte(raw), &list); err != nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, p := range list {
		if p.Id != "" {
			h.profiles[p.Id] = p
		}
	}
}

func (h *Handler) persistProfilesLocked(ctx context.Context) error {
	if h.store == nil {
		return nil
	}
	list := make([]DlnaProfile, 0, len(h.profiles))
	for _, p := range h.profiles {
		list = append(list, p)
	}
	raw, err := json.Marshal(list)
	if err != nil {
		return err
	}
	_, err = h.store.Writer.ExecContext(ctx, `
		INSERT INTO server_config (key, value_json, updated_at)
		VALUES (?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET
			value_json = excluded.value_json,
			updated_at = excluded.updated_at`,
		profilesStoreKey, string(raw), time.Now().UTC().Unix(),
	)
	return err
}

// --- UPnP Description / SCPD / Icons ---

func (h *Handler) serveDescriptionXML(w http.ResponseWriter, r *http.Request, uuid string) {
	if strings.TrimSpace(uuid) == "" {
		uuid = h.cfg.ServerID
	}
	doc := buildDescriptionXML(h.cfg.ServerName, uuid, h.baseURL(r))
	writeXML(w, r, http.StatusOK, doc)
}

func (h *Handler) handleContentDirectoryXML(w http.ResponseWriter, r *http.Request) {
	writeXML(w, r, http.StatusOK, contentDirectorySCPDXML)
}

func (h *Handler) handleConnectionManagerXML(w http.ResponseWriter, r *http.Request) {
	writeXML(w, r, http.StatusOK, connectionManagerSCPDXML)
}

func (h *Handler) handleIcon(w http.ResponseWriter, r *http.Request) {
	h.serveIcon(w, r, r.PathValue("Filename"))
}

func (h *Handler) serveIcon(w http.ResponseWriter, r *http.Request, filename string) {
	filename = strings.ToLower(strings.TrimSpace(filename))
	ext := filepath.Ext(filename)
	switch ext {
	case ".svg":
		w.Header().Set("Content-Type", "image/svg+xml")
		w.Header().Set("Cache-Control", "public, max-age=86400")
		w.WriteHeader(http.StatusOK)
		if r.Method != http.MethodHead {
			_, _ = w.Write([]byte(ambraLogoSVG))
		}
	case ".png":
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Cache-Control", "public, max-age=86400")
		w.WriteHeader(http.StatusOK)
		if r.Method != http.MethodHead {
			_, _ = w.Write(ambraLogoPNG)
		}
	default:
		httpx.Error(w, http.StatusNotFound, "icon not found")
	}
}

// --- Signed DLNA Media Streaming ---

func (h *Handler) signStreamToken(itemID string, exp int64) string {
	mac := hmac.New(sha256.New, h.streamSecret)
	_, _ = fmt.Fprintf(mac, "%s:%d", guid.Normalize(itemID), exp)
	return hex.EncodeToString(mac.Sum(nil))
}

func (h *Handler) verifyStreamToken(itemID, expStr, sig string) bool {
	expStr = strings.TrimSpace(expStr)
	sig = strings.TrimSpace(sig)
	if expStr == "" || sig == "" {
		return false
	}
	exp, err := strconv.ParseInt(expStr, 10, 64)
	if err != nil || exp <= time.Now().UTC().Unix() {
		return false
	}
	expected := h.signStreamToken(itemID, exp)
	return hmac.Equal([]byte(expected), []byte(strings.ToLower(sig)))
}

func (h *Handler) handleDLNAStream(w http.ResponseWriter, r *http.Request) {
	itemID := guid.Normalize(r.PathValue("Id"))
	q := r.URL.Query()
	if !h.verifyStreamToken(itemID, q.Get("exp"), q.Get("sig")) {
		httpx.Error(w, http.StatusForbidden, "invalid or expired stream signature")
		return
	}
	if h.libRepo == nil {
		httpx.Error(w, http.StatusNotFound, "media item not found")
		return
	}

	mediaPath, err := h.resolveDLNAMediaPath(r.Context(), itemID)
	if errors.Is(err, sqlite.ErrNotFound) {
		httpx.Error(w, http.StatusNotFound, "media item not found")
		return
	}
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	f, err := os.Open(mediaPath)
	if err != nil {
		if os.IsNotExist(err) {
			httpx.Error(w, http.StatusNotFound, "media file missing on disk")
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
	if ct := dlnaMediaContentType(mediaPath); ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	w.Header().Set("SERVER", defaultServerHeader)
	http.ServeContent(w, r, stat.Name(), stat.ModTime(), f)
}

func (h *Handler) resolveDLNAMediaPath(ctx context.Context, itemID string) (string, error) {
	sources, err := h.libRepo.GetMediaSourcesByItemID(ctx, itemID)
	if err == nil && len(sources) > 0 && strings.TrimSpace(sources[0].Path) != "" {
		return sources[0].Path, nil
	}
	item, err := h.libRepo.GetItemRecordByID(ctx, itemID)
	if err != nil {
		return "", err
	}
	if item == nil || strings.TrimSpace(item.Path) == "" {
		return "", sqlite.ErrNotFound
	}
	return item.Path, nil
}

func dlnaMediaContentType(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".mp4", ".m4v":
		return "video/mp4"
	case ".mkv":
		return "video/x-matroska"
	case ".webm":
		return "video/webm"
	case ".mov":
		return "video/quicktime"
	case ".avi":
		return "video/x-msvideo"
	case ".ts", ".m2ts":
		return "video/mp2t"
	case ".mp3":
		return "audio/mpeg"
	case ".flac":
		return "audio/flac"
	case ".m4a", ".alac":
		return "audio/mp4"
	case ".aac":
		return "audio/aac"
	case ".ogg", ".oga", ".opus":
		return "audio/ogg"
	case ".wav":
		return "audio/wav"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".webp":
		return "image/webp"
	default:
		return "application/octet-stream"
	}
}

// --- UPnP GENA Event Subscriptions ---

func (h *Handler) handleEventSubscribe(service string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		uuid := r.PathValue("UuId")
		timeoutSec := parseTimeoutHeader(r.Header.Get("TIMEOUT"))
		sid := strings.TrimSpace(r.Header.Get("SID"))
		now := time.Now().UTC()

		h.mu.Lock()
		defer h.mu.Unlock()

		// Prune expired subscriptions before processing renewal or creation.
		for k, sub := range h.subscriptions {
			if !sub.ExpiresAt.After(now) {
				delete(h.subscriptions, k)
			}
		}

		if sid != "" {
			// Renewal request
			sub, ok := h.subscriptions[sid]
			if !ok || sub.Service != service {
				http.Error(w, "subscription not found", http.StatusPreconditionFailed)
				return
			}
			sub.ExpiresAt = now.Add(time.Duration(timeoutSec) * time.Second)
			h.subscriptions[sid] = sub
		} else {
			// Initial subscription
			callback := strings.TrimSpace(r.Header.Get("CALLBACK"))
			if callback == "" {
				http.Error(w, "missing CALLBACK header", http.StatusPreconditionFailed)
				return
			}
			if len(h.subscriptions) >= maxDLNASubscriptions {
				http.Error(w, "too many active subscriptions", http.StatusServiceUnavailable)
				return
			}
			sid = "uuid:" + guid.New()
			h.subscriptions[sid] = eventSubscription{
				SID:       sid,
				Service:   service,
				UUID:      uuid,
				Callback:  callback,
				ExpiresAt: now.Add(time.Duration(timeoutSec) * time.Second),
			}
		}

		w.Header().Set("SID", sid)
		w.Header().Set("TIMEOUT", fmt.Sprintf("Second-%d", timeoutSec))
		w.Header().Set("SERVER", defaultServerHeader)
		w.Header().Set("DATE", now.Format(http.TimeFormat))
		w.WriteHeader(http.StatusOK)
	}
}

func (h *Handler) handleEventUnsubscribe(service string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sid := strings.TrimSpace(r.Header.Get("SID"))
		if sid == "" {
			http.Error(w, "missing SID header", http.StatusPreconditionFailed)
			return
		}
		h.mu.Lock()
		sub, ok := h.subscriptions[sid]
		if !ok || sub.Service != service {
			h.mu.Unlock()
			http.Error(w, "subscription not found", http.StatusPreconditionFailed)
			return
		}
		delete(h.subscriptions, sid)
		h.mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}
}

// ActiveSubscriptionCount returns the number of active UPnP event subscriptions.
func (h *Handler) ActiveSubscriptionCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.subscriptions)
}

func parseTimeoutHeader(raw string) int {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return defaultSubTimeout
	}
	lower := strings.ToLower(raw)
	if strings.HasPrefix(lower, "second-") {
		val := strings.TrimSpace(raw[len("second-"):])
		if strings.EqualFold(val, "infinite") {
			return maxSubTimeout
		}
		if n, err := strconv.Atoi(val); err == nil {
			if n < minSubTimeout {
				return minSubTimeout
			}
			if n > maxSubTimeout {
				return maxSubTimeout
			}
			return n
		}
	}
	return defaultSubTimeout
}

func (h *Handler) baseURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		scheme = "https"
	}
	host := r.Host
	if host == "" {
		host = "127.0.0.1:8096"
	}
	return scheme + "://" + host
}

func writeXML(w http.ResponseWriter, r *http.Request, status int, body string) {
	w.Header().Set("Content-Type", "text/xml; charset=utf-8")
	w.Header().Set("SERVER", defaultServerHeader)
	w.WriteHeader(status)
	if r.Method != http.MethodHead {
		_, _ = w.Write([]byte(body))
	}
}

func xmlEscape(s string) string {
	var b strings.Builder
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}

func passthrough(next http.Handler) http.Handler { return next }

var errUnknownAction = errors.New("unknown UPnP action")

// ambraLogoSVG is a minimal Ambra brand icon served for DLNA renderers.
const ambraLogoSVG = `<?xml version="1.0" encoding="UTF-8"?>
<svg xmlns="http://www.w3.org/2000/svg" width="120" height="120" viewBox="0 0 120 120">
  <rect width="120" height="120" rx="24" fill="#1E293B"/>
  <polygon points="44,32 92,60 44,88" fill="#38BDF8"/>
</svg>`

// ambraLogoPNG is a valid 120x120 24-bit RGB PNG matching the dimensions declared in description.xml.
var ambraLogoPNG = buildAmbraLogoPNG()

func buildAmbraLogoPNG() []byte {
	const size = 120
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	bg := color.RGBA{R: 0x1E, G: 0x29, B: 0x3B, A: 0xFF}
	fg := color.RGBA{R: 0x38, G: 0xBD, B: 0xF8, A: 0xFF}

	// Fill background and draw centered play triangle matching ambraLogoSVG: (44,32), (92,60), (44,88).
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			c := bg
			if x >= 44 && y >= 32 && y <= 88 {
				// Triangle right edge x(y) = 44 + (48 * (28 - |y - 60|)) / 28
				dy := y - 60
				if dy < 0 {
					dy = -dy
				}
				maxX := 44 + (48*(28-dy))/28
				if x <= maxX {
					c = fg
				}
			}
			img.SetRGBA(x, y, c)
		}
	}

	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}
