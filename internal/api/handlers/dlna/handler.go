package dlna

import (
	"bufio"
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
	"io"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ambra-media/ambra/internal/api/dto"
	"github.com/ambra-media/ambra/internal/api/httpx"
	"github.com/ambra-media/ambra/internal/api/middleware"
	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/core/transcode"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/pkg/netutil"
	"github.com/ambra-media/ambra/internal/plugins"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

const (
	profilesStoreKey          = "dlna:profiles"
	defaultProfileID          = "default"
	defaultSubTimeout         = 1800
	minSubTimeout             = 60
	maxSubTimeout             = 86400
	maxDLNASubscriptions      = 256
	maxDLNASubscriptionsPerIP = 16
	maxDLNACallbackLen        = 1024
	maxDLNASOAPPerIPPerMin    = 120
	dlnaSOAPRateWindow        = time.Minute
	maxDLNARateBuckets        = 4096
	maxDLNARequestedCount     = 500
	maxDLNATranscodes         = 16
	dlnaStreamTokenTTL        = 24 * time.Hour
	defaultServerHeader       = "Ambra/4.10.2.0 UPnP/1.0 DLNADOC/1.50"
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
	ClientIP  string
	Seq       uint32
	ExpiresAt time.Time
}

type dlnaRateWindow struct {
	count   int
	resetAt time.Time
}

// Handler implements Emby-compatible DLNA Profile management and UPnP MediaServer:1 endpoints.
type Handler struct {
	cfg                       *config.Config
	libRepo                   *sqlite.LibraryRepo
	store                     *sqlite.Store
	userRepo                  *sqlite.UserRepo
	transcodeMgr              *transcode.Manager
	defaultUserID             string
	auth                      AuthFuncs
	streamSecret              []byte
	streamResolverWithHeaders func(ctx context.Context, rawPath string) (*plugins.StreamResolveResult, error)
	proxyClient               *http.Client
	notifyClient              *http.Client

	soapRateMu sync.Mutex
	soapRates  map[string]*dlnaRateWindow

	mu            sync.RWMutex
	profiles      map[string]DlnaProfile
	subscriptions map[string]eventSubscription
	systemUpdate  uint32
	notifySem     chan struct{}
	transcodeSem  chan struct{}
}

// New constructs a DLNA handler backed by LibraryRepo and optional SQLite Store for profile persistence.
func New(cfg *config.Config, libRepo *sqlite.LibraryRepo, store *sqlite.Store) *Handler {
	if cfg == nil {
		cfg = config.Default("./data")
		cfg.EnableDLNA = true
	}
	randBytes := make([]byte, 32)
	_, _ = rand.Read(randBytes)
	snap := cfg.Snapshot()
	secretInput := append([]byte(snap.ServerID+":"), randBytes...)
	secretSum := sha256.Sum256(secretInput)

	h := &Handler{
		cfg:           cfg,
		libRepo:       libRepo,
		store:         store,
		streamSecret:  secretSum[:],
		proxyClient:   netutil.NewSafeHTTPClient(0, true),
		notifyClient:  newDLNALANCallbackClient(3 * time.Second),
		soapRates:     make(map[string]*dlnaRateWindow),
		profiles:      make(map[string]DlnaProfile),
		subscriptions: make(map[string]eventSubscription),
		systemUpdate:  1,
		notifySem:     make(chan struct{}, 8),
		transcodeSem:  make(chan struct{}, maxDLNATranscodes),
	}
	def := DefaultProfile()
	h.profiles[def.Id] = def
	h.loadPersistedProfiles(context.Background())
	return h
}

// SetUserRepo attaches a UserRepo for optional default-user DLNA policy filtering.
func (h *Handler) SetUserRepo(r *sqlite.UserRepo) {
	h.mu.Lock()
	h.userRepo = r
	h.mu.Unlock()
}

// SetDefaultUserID binds an optional default user whose folder/parental/tag policy is enforced for DLNA browsing & streaming.
func (h *Handler) SetDefaultUserID(userID string) {
	h.mu.Lock()
	h.defaultUserID = guid.Normalize(userID)
	h.mu.Unlock()
}

// SetTranscodeManager attaches an optional transcode Manager for global stream limit coordination.
func (h *Handler) SetTranscodeManager(mgr *transcode.Manager) {
	h.mu.Lock()
	h.transcodeMgr = mgr
	h.mu.Unlock()
}

// SetStreamResolverWithHeaders attaches a .strm resolver returning target URL and custom HTTP headers.
func (h *Handler) SetStreamResolverWithHeaders(fn func(ctx context.Context, rawPath string) (*plugins.StreamResolveResult, error)) {
	h.mu.Lock()
	h.streamResolverWithHeaders = fn
	h.mu.Unlock()
}

// SetProxyClient overrides the HTTP client used for server-side .strm reverse-proxying (primarily for tests).
func (h *Handler) SetProxyClient(client *http.Client) {
	if client != nil {
		h.mu.Lock()
		h.proxyClient = client
		h.mu.Unlock()
	}
}

// SetNotifyClient overrides the HTTP client used for UPnP GENA NOTIFY callbacks (primarily for tests).
func (h *Handler) SetNotifyClient(client *http.Client) {
	if client != nil {
		h.mu.Lock()
		h.notifyClient = client
		h.mu.Unlock()
	}
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
			if !h.checkDLNAAccess(w, r) {
				return
			}
			h.serveIcon(w, r, seg2)
		case seg2 == "description.xml" || seg2 == "description":
			if !h.checkDLNAAccess(w, r) {
				return
			}
			h.serveDescriptionXML(w, r, seg1)
		default:
			httpx.Error(w, http.StatusNotFound, "not found")
		}
	})

	// LAN-only UPnP SCPD documents (GET also matches HEAD in Go 1.22+ ServeMux)
	mux.HandleFunc("GET /Dlna/{UuId}/contentdirectory/contentdirectory.xml", h.requireDLNALANAccess(h.handleContentDirectoryXML))
	mux.HandleFunc("GET /Dlna/{UuId}/contentdirectory/contentdirectory", h.requireDLNALANAccess(h.handleContentDirectoryXML))
	mux.HandleFunc("GET /Dlna/{UuId}/connectionmanager/connectionmanager.xml", h.requireDLNALANAccess(h.handleConnectionManagerXML))
	mux.HandleFunc("GET /Dlna/{UuId}/connectionmanager/connectionmanager", h.requireDLNALANAccess(h.handleConnectionManagerXML))

	// LAN-only UPnP SOAP control endpoints
	mux.HandleFunc("POST /Dlna/{UuId}/contentdirectory/control", h.requireDLNALANAccess(h.handleContentDirectoryControl))
	mux.HandleFunc("POST /Dlna/{UuId}/connectionmanager/control", h.requireDLNALANAccess(h.handleConnectionManagerControl))

	// LAN-only server icons (3-segment form)
	mux.HandleFunc("GET /Dlna/{UuId}/icons/{Filename}", h.requireDLNALANAccess(h.handleIcon))

	// LAN-only signed DLNA media stream endpoint (validated via LAN origin + HMAC sig & exp query params)
	mux.HandleFunc("GET /Dlna/{UuId}/stream/{Id}", h.requireDLNALANAccess(h.handleDLNAStream))

	// LAN-only UPnP GENA event subscriptions (SUBSCRIBE / UNSUBSCRIBE)
	mux.HandleFunc("SUBSCRIBE /Dlna/{UuId}/contentdirectory/events", h.requireDLNALANAccess(h.handleEventSubscribe("contentdirectory")))
	mux.HandleFunc("UNSUBSCRIBE /Dlna/{UuId}/contentdirectory/events", h.requireDLNALANAccess(h.handleEventUnsubscribe("contentdirectory")))
	mux.HandleFunc("SUBSCRIBE /Dlna/{UuId}/connectionmanager/events", h.requireDLNALANAccess(h.handleEventSubscribe("connectionmanager")))
	mux.HandleFunc("UNSUBSCRIBE /Dlna/{UuId}/connectionmanager/events", h.requireDLNALANAccess(h.handleEventUnsubscribe("connectionmanager")))
}

func (h *Handler) cfgSnapshot() config.Config {
	if h.cfg == nil {
		return config.Config{}
	}
	return h.cfg.Snapshot()
}

func (h *Handler) checkDLNAAccess(w http.ResponseWriter, r *http.Request) bool {
	if h.cfg != nil && !h.cfg.Snapshot().EnableDLNA {
		httpx.Error(w, http.StatusForbidden, "DLNA is disabled")
		return false
	}
	if !netutil.IsLocalOrLANRequest(r) {
		httpx.Error(w, http.StatusForbidden, "DLNA is restricted to local network")
		return false
	}
	return true
}

func (h *Handler) requireDLNALANAccess(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !h.checkDLNAAccess(w, r) {
			return
		}
		next(w, r)
	}
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

func canonicalProfileID(id string) string {
	trimmed := strings.TrimSpace(id)
	if strings.EqualFold(trimmed, defaultProfileID) {
		return defaultProfileID
	}
	return trimmed
}

func (h *Handler) serveProfileByID(w http.ResponseWriter, id string) {
	id = canonicalProfileID(id)
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
	if err := httpx.DecodeJSONLimit(r, &p, 0); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid dlna profile payload")
		return
	}
	if strings.TrimSpace(p.Name) == "" {
		httpx.Error(w, http.StatusBadRequest, "profile Name is required")
		return
	}
	p.Id = canonicalProfileID(p.Id)
	if p.Id == defaultProfileID {
		httpx.Error(w, http.StatusBadRequest, "cannot overwrite built-in default profile")
		return
	}
	if p.Id == "" {
		p.Id = guid.New()
	}
	if strings.TrimSpace(p.Type) == "" {
		p.Type = "User"
	}

	h.mu.Lock()
	prev, hadPrev := h.profiles[p.Id]
	h.profiles[p.Id] = p
	err := h.persistProfilesLocked(r.Context())
	if err != nil {
		if hadPrev {
			h.profiles[p.Id] = prev
		} else {
			delete(h.profiles, p.Id)
		}
	}
	h.mu.Unlock()
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("X-Profile-Id", p.Id)
	httpx.JSON(w, http.StatusOK, p)
}

func (h *Handler) handleUpdateProfile(w http.ResponseWriter, r *http.Request) {
	id := canonicalProfileID(r.PathValue("Id"))
	if id == defaultProfileID {
		httpx.Error(w, http.StatusBadRequest, "cannot overwrite built-in default profile")
		return
	}
	var p DlnaProfile
	if err := httpx.DecodeJSONLimit(r, &p, 0); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid dlna profile payload")
		return
	}
	p.Id = id
	if strings.TrimSpace(p.Name) == "" {
		httpx.Error(w, http.StatusBadRequest, "profile Name is required")
		return
	}

	h.mu.Lock()
	existing, ok := h.profiles[id]
	if !ok {
		h.mu.Unlock()
		httpx.Error(w, http.StatusNotFound, "dlna profile not found")
		return
	}
	if p.Type == "" {
		p.Type = existing.Type
	}
	if p.Type == "" {
		p.Type = "User"
	}
	h.profiles[id] = p
	err := h.persistProfilesLocked(r.Context())
	if err != nil {
		h.profiles[id] = existing
	}
	h.mu.Unlock()
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.NoContent(w)
}

func (h *Handler) handleDeleteProfile(w http.ResponseWriter, r *http.Request) {
	id := canonicalProfileID(r.PathValue("Id"))
	if id == defaultProfileID {
		httpx.Error(w, http.StatusBadRequest, "cannot delete built-in default profile")
		return
	}

	h.mu.Lock()
	existing, ok := h.profiles[id]
	if !ok {
		h.mu.Unlock()
		httpx.Error(w, http.StatusNotFound, "dlna profile not found")
		return
	}
	delete(h.profiles, id)
	err := h.persistProfilesLocked(r.Context())
	if err != nil {
		h.profiles[id] = existing
	}
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
		id := canonicalProfileID(p.Id)
		if id != "" {
			p.Id = id
			h.profiles[id] = p
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
	snap := h.cfgSnapshot()
	if strings.TrimSpace(uuid) == "" {
		uuid = snap.ServerID
	}
	doc := buildDescriptionXML(snap.ServerName, uuid, h.baseURL(r))
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
	return h.signStreamTokenForMode(itemID, exp, false)
}

func (h *Handler) signStreamTokenForMode(itemID string, exp int64, transcode bool) string {
	mode := "0"
	if transcode {
		mode = "1"
	}
	mac := hmac.New(sha256.New, h.streamSecret)
	_, _ = fmt.Fprintf(mac, "%s:%d:tc=%s", guid.Normalize(itemID), exp, mode)
	return hex.EncodeToString(mac.Sum(nil))
}

func (h *Handler) verifyStreamToken(itemID, expStr, sig string) bool {
	return h.verifyStreamTokenForMode(itemID, expStr, sig, false, false)
}

func (h *Handler) verifyStreamTokenForMode(itemID, expStr, sig string, wantTranscode, allowLegacyUnscoped bool) bool {
	expStr = strings.TrimSpace(expStr)
	sig = strings.TrimSpace(sig)
	if expStr == "" || sig == "" {
		return false
	}
	exp, err := strconv.ParseInt(expStr, 10, 64)
	if err != nil || exp <= time.Now().UTC().Unix() {
		return false
	}
	sigLower := []byte(strings.ToLower(sig))
	scoped := h.signStreamTokenForMode(itemID, exp, wantTranscode)
	if hmac.Equal([]byte(scoped), sigLower) {
		return true
	}
	if allowLegacyUnscoped {
		mac := hmac.New(sha256.New, h.streamSecret)
		_, _ = fmt.Fprintf(mac, "%s:%d", guid.Normalize(itemID), exp)
		expected := hex.EncodeToString(mac.Sum(nil))
		return hmac.Equal([]byte(expected), sigLower)
	}
	return false
}

func isDLNAAudioItem(item *sqlite.ItemRecord) bool {
	if item == nil {
		return false
	}
	return strings.EqualFold(item.MediaType, "Audio") ||
		strings.EqualFold(item.Type, "Audio") ||
		strings.EqualFold(item.Type, "AudioBook")
}

func isDLNAPhotoItem(item *sqlite.ItemRecord) bool {
	if item == nil {
		return false
	}
	return strings.EqualFold(item.MediaType, "Photo") || strings.EqualFold(item.Type, "Photo")
}

func (h *Handler) canDLNATranscodeItem(r *http.Request, item *sqlite.ItemRecord) bool {
	if isDLNAPhotoItem(item) {
		return false
	}
	pol, denyAll := h.resolvePolicyForRequest(r.Context(), r)
	if denyAll {
		return false
	}
	if pol == nil || pol.IsAdministrator {
		return true
	}
	if !pol.EnableMediaPlayback {
		return false
	}
	if isDLNAAudioItem(item) {
		return pol.EnableAudioPlaybackTranscoding
	}
	return pol.EnableVideoPlaybackTranscoding
}

func (h *Handler) acquireDLNAStreamSlot(r *http.Request, itemID string, transcodeMode bool) (func(), bool) {
	noop := func() {}
	h.mu.RLock()
	mgr := h.transcodeMgr
	h.mu.RUnlock()
	if h.cfg == nil || mgr == nil {
		return noop, true
	}
	limit := h.cfg.Snapshot().SimultaneousStreamLimit
	if limit <= 0 {
		return noop, true
	}
	var slotKey string
	if transcodeMode {
		slotKey = "dlna:tc:" + guid.New()
	} else {
		slotKey = "dlna:" + netutil.ClientIP(r) + ":" + guid.Normalize(itemID)
	}
	return mgr.TryAcquireStreamSlot(limit, slotKey)
}

func (h *Handler) handleDLNAStream(w http.ResponseWriter, r *http.Request) {
	itemID := guid.Normalize(r.PathValue("Id"))
	q := r.URL.Query()
	explicitTranscode := q.Get("transcode") == "1" || strings.EqualFold(q.Get("transcode"), "true")
	validSig := h.verifyStreamTokenForMode(itemID, q.Get("exp"), q.Get("sig"), explicitTranscode, false)
	if !validSig && !explicitTranscode && q.Get("profile") != "" {
		validSig = h.verifyStreamTokenForMode(itemID, q.Get("exp"), q.Get("sig"), true, false)
	}
	if !validSig {
		httpx.Error(w, http.StatusForbidden, "invalid or expired stream signature")
		return
	}
	if h.libRepo == nil {
		httpx.Error(w, http.StatusNotFound, "media item not found")
		return
	}
	if pol, _ := h.resolvePolicyForRequest(r.Context(), r); pol != nil && !pol.IsAdministrator && !pol.EnableMediaPlayback {
		httpx.Error(w, http.StatusForbidden, "Media playback is disabled for this user")
		return
	}

	item, mediaPath, err := h.resolveDLNAMediaPathForRequest(r.Context(), r, itemID)
	if errors.Is(err, sqlite.ErrNotFound) {
		httpx.Error(w, http.StatusNotFound, "media item not found")
		return
	}
	if err != nil {
		slog.Error("dlna: failed to resolve media path", "itemId", itemID, "err", err)
		httpx.Error(w, http.StatusInternalServerError, "failed to load media item")
		return
	}

	realPath, err := filepath.EvalSymlinks(mediaPath)
	if err != nil {
		if os.IsNotExist(err) {
			httpx.Error(w, http.StatusNotFound, "media file missing on disk")
			return
		}
		httpx.Error(w, http.StatusForbidden, "media file path is not accessible")
		return
	}
	realPath = filepath.Clean(realPath)
	if !h.isPathWithinRegisteredLibraries(r.Context(), realPath) {
		httpx.Error(w, http.StatusForbidden, "media file path is outside registered libraries")
		return
	}

	wantTranscode := explicitTranscode
	if !wantTranscode && q.Get("profile") != "" {
		if !h.verifyStreamTokenForMode(itemID, q.Get("exp"), q.Get("sig"), true, false) {
			wantTranscode = false
		} else {
			wantTranscode = !h.profileSupportsContainer(q.Get("profile"), item, realPath)
		}
	}
	if wantTranscode && !h.canDLNATranscodeItem(r, item) {
		httpx.Error(w, http.StatusForbidden, "Playback transcoding is disabled for this user")
		return
	}

	// [H-02] Server-side safe reverse proxy for .strm files (never 302 redirect exposing signed upstream URLs).
	if (item != nil && item.IsStrm) || strings.EqualFold(filepath.Ext(mediaPath), ".strm") || strings.EqualFold(filepath.Ext(realPath), ".strm") {
		targetURL, headers, strmErr := h.resolveDLNAStrmTarget(r.Context(), realPath)
		if strmErr != nil {
			httpx.Error(w, http.StatusForbidden, "invalid or disallowed strm stream target")
			return
		}
		if wantTranscode {
			h.streamDLNATranscode(w, r, item, targetURL, headers)
			return
		}
		if r.Method != http.MethodHead {
			release, allowed := h.acquireDLNAStreamSlot(r, itemID, false)
			if !allowed {
				httpx.Error(w, http.StatusTooManyRequests, "simultaneous stream limit reached")
				return
			}
			defer release()
		}
		h.proxyDLNARemoteStream(w, r, targetURL, headers)
		return
	}

	if wantTranscode {
		fi, stErr := os.Stat(realPath)
		if stErr != nil || fi.IsDir() {
			httpx.Error(w, http.StatusNotFound, "media file not found")
			return
		}
		h.streamDLNATranscode(w, r, item, realPath, nil)
		return
	}

	f, err := os.Open(realPath)
	if err != nil {
		if os.IsNotExist(err) {
			httpx.Error(w, http.StatusNotFound, "media file missing on disk")
			return
		}
		if errors.Is(err, os.ErrPermission) {
			slog.Error("dlna: permission denied opening media file", "itemId", itemID, "uid", os.Getuid(), "gid", os.Getgid(), "err", err)
		} else {
			slog.Error("dlna: failed to open media file", "itemId", itemID, "err", err)
		}
		httpx.Error(w, http.StatusInternalServerError, "failed to open media file")
		return
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		slog.Error("dlna: failed to stat media file", "itemId", itemID, "err", err)
		httpx.Error(w, http.StatusInternalServerError, "failed to stat media file")
		return
	}
	if stat.IsDir() || !stat.Mode().IsRegular() {
		httpx.Error(w, http.StatusNotFound, "media file not found")
		return
	}
	if r.Method != http.MethodHead {
		release, allowed := h.acquireDLNAStreamSlot(r, itemID, false)
		if !allowed {
			httpx.Error(w, http.StatusTooManyRequests, "simultaneous stream limit reached")
			return
		}
		defer release()
	}
	if ct := dlnaMediaContentType(realPath); ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	w.Header().Set("SERVER", defaultServerHeader)
	http.ServeContent(w, r, stat.Name(), stat.ModTime(), f)
}

func (h *Handler) resolveDLNAStrmTarget(ctx context.Context, realStrmPath string) (string, map[string]string, error) {
	h.mu.RLock()
	resolver := h.streamResolverWithHeaders
	h.mu.RUnlock()

	var (
		targetURL string
		headers   map[string]string
	)
	if resolver != nil {
		res, err := resolver(ctx, realStrmPath)
		if err != nil || res == nil {
			return "", nil, errors.New("strm resolve failed")
		}
		targetURL = strings.TrimSpace(res.URL)
		headers = res.Headers
	} else {
		f, err := os.Open(realStrmPath)
		if err != nil {
			return "", nil, err
		}
		defer f.Close()
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			line := strings.TrimSpace(strings.TrimPrefix(sc.Text(), "\uFEFF"))
			if line != "" && !strings.HasPrefix(line, "#") {
				targetURL = strings.ReplaceAll(line, " ", "%20")
				break
			}
		}
	}
	parsed, err := url.Parse(targetURL)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return "", nil, errors.New("invalid strm url")
	}
	// Reject embedded URL userinfo credentials to prevent leaking or abusing basic-auth URLs.
	if parsed.User != nil {
		return "", nil, errors.New("embedded credentials in strm url are disallowed")
	}
	if err := netutil.ValidateRemoteURL(targetURL, true); err != nil {
		return "", nil, err
	}
	return targetURL, headers, nil
}

func sanitizeDLNAProxyContentType(rawCT, targetURL string) string {
	candidate := strings.TrimSpace(rawCT)
	if candidate == "" {
		if u, uErr := url.Parse(targetURL); uErr == nil {
			candidate = dlnaMediaContentType(u.Path)
		}
	}
	if candidate == "" {
		return "application/octet-stream"
	}
	mediaType, _, err := mime.ParseMediaType(candidate)
	if err != nil {
		return "application/octet-stream"
	}
	mediaType = strings.ToLower(strings.TrimSpace(mediaType))
	if strings.HasPrefix(mediaType, "video/") || strings.HasPrefix(mediaType, "audio/") || mediaType == "application/octet-stream" {
		return mediaType
	}
	return "application/octet-stream"
}

func (h *Handler) proxyDLNARemoteStream(w http.ResponseWriter, r *http.Request, targetURL string, headers map[string]string) {
	upReq, err := http.NewRequestWithContext(r.Context(), r.Method, targetURL, nil)
	if err != nil {
		httpx.Error(w, http.StatusBadGateway, "invalid upstream stream url")
		return
	}
	for k, v := range headers {
		key := strings.TrimSpace(k)
		val := strings.TrimSpace(v)
		if key == "" || val == "" || strings.ContainsAny(key, "\r\n\x00") || strings.ContainsAny(val, "\r\n\x00") {
			continue
		}
		switch strings.ToLower(key) {
		case "user-agent", "referer", "cookie", "authorization", "origin":
			upReq.Header.Set(key, val)
		}
	}
	if rng := r.Header.Get("Range"); rng != "" {
		upReq.Header.Set("Range", rng)
	}
	if ifRange := r.Header.Get("If-Range"); ifRange != "" {
		upReq.Header.Set("If-Range", ifRange)
	}
	if upReq.Header.Get("User-Agent") == "" {
		upReq.Header.Set("User-Agent", "Ambra-DLNA-Proxy/1.0")
	}

	h.mu.RLock()
	client := h.proxyClient
	h.mu.RUnlock()
	if client == nil {
		client = netutil.NewSafeHTTPClient(0, true)
		defer client.CloseIdleConnections()
	}

	resp, err := client.Do(upReq)
	if err != nil {
		slog.Error("dlna: upstream strm proxy failed", "err", err)
		httpx.Error(w, http.StatusBadGateway, "upstream stream error")
		return
	}
	defer resp.Body.Close()

	for _, hdr := range []string{"Content-Length", "Content-Range", "Accept-Ranges", "ETag", "Last-Modified"} {
		if val := resp.Header.Get(hdr); val != "" {
			w.Header().Set(hdr, val)
		}
	}
	w.Header().Set("Content-Type", sanitizeDLNAProxyContentType(resp.Header.Get("Content-Type"), targetURL))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "sandbox")
	w.Header().Set("SERVER", defaultServerHeader)
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(resp.StatusCode)
	if r.Method != http.MethodHead {
		_, _ = io.Copy(w, resp.Body)
	}
}

func (h *Handler) profileSupportsContainer(profileID string, item *sqlite.ItemRecord, mediaPath string) bool {
	h.mu.RLock()
	p, ok := h.profiles[canonicalProfileID(profileID)]
	h.mu.RUnlock()
	if !ok {
		p = DefaultProfile()
	}
	if len(p.DirectPlayProfiles) == 0 {
		return true
	}
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(mediaPath), "."))
	if ext == "" && item != nil {
		ext = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(item.Container), "."))
	}
	if ext == "" {
		return true
	}
	mediaType := "Video"
	if item != nil && strings.TrimSpace(item.MediaType) != "" {
		mediaType = item.MediaType
	}
	for _, dp := range p.DirectPlayProfiles {
		dpType, _ := dp["Type"].(string)
		if dpType != "" && !strings.EqualFold(dpType, mediaType) {
			continue
		}
		containers, _ := dp["Container"].(string)
		if containers == "" {
			return true
		}
		for _, c := range strings.Split(containers, ",") {
			c = strings.ToLower(strings.TrimSpace(c))
			if c == "*" || c == ext {
				return true
			}
		}
	}
	return false
}

func (h *Handler) streamDLNATranscode(w http.ResponseWriter, r *http.Request, item *sqlite.ItemRecord, mediaInput string, headers map[string]string) {
	if !h.canDLNATranscodeItem(r, item) {
		httpx.Error(w, http.StatusForbidden, "Playback transcoding is disabled for this user")
		return
	}
	isAudio := isDLNAAudioItem(item)
	ct := "video/mp2t"
	if isAudio {
		ct = "audio/mpeg"
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("SERVER", defaultServerHeader)
	w.Header().Set("Cache-Control", "no-store")
	if r.Method == http.MethodHead {
		w.WriteHeader(http.StatusOK)
		return
	}

	if h.transcodeSem != nil {
		select {
		case h.transcodeSem <- struct{}{}:
			defer func() { <-h.transcodeSem }()
		default:
			httpx.Error(w, http.StatusTooManyRequests, "dlna transcode concurrency limit reached")
			return
		}
	}

	itemID := ""
	if item != nil {
		itemID = item.ID
	}
	release, allowed := h.acquireDLNAStreamSlot(r, itemID, true)
	if !allowed {
		httpx.Error(w, http.StatusTooManyRequests, "simultaneous stream limit reached")
		return
	}
	defer release()

	ffInput := mediaInput
	if strings.HasPrefix(strings.ToLower(mediaInput), "http://") || strings.HasPrefix(strings.ToLower(mediaInput), "https://") {
		proxy, err := transcode.StartLoopbackStreamProxy(transcode.LoopbackProxyOptions{
			TargetURL:       mediaInput,
			Headers:         headers,
			AllowPrivateLAN: true,
		})
		if err != nil {
			httpx.Error(w, http.StatusBadGateway, "blocked upstream stream url")
			return
		}
		defer proxy.Close()
		ffInput = proxy.URL()
	}

	ffmpegPath := "ffmpeg"
	if h.cfg != nil && h.cfg.Snapshot().FFmpegPath != "" {
		ffmpegPath = h.cfg.Snapshot().FFmpegPath
	}

	var args []string
	if strings.HasPrefix(strings.ToLower(ffInput), "http://") || strings.HasPrefix(strings.ToLower(ffInput), "https://") {
		args = append(args, "-hide_banner", "-loglevel", "error", "-protocol_whitelist", "http,https,tcp,tls,crypto", "-max_redirects", "0", "-rw_timeout", "15000000", "-i", ffInput)
	} else {
		args = append(args, "-hide_banner", "-loglevel", "error", "-protocol_whitelist", "file,crypto,data", "-i", ffInput)
	}
	if isAudio {
		args = append(args, "-vn", "-c:a", "libmp3lame", "-b:a", "192000", "-f", "mp3", "pipe:1")
	} else {
		args = append(args, "-sn", "-c:v", "libx264", "-preset", "veryfast", "-pix_fmt", "yuv420p", "-b:v", "4000000", "-c:a", "aac", "-b:a", "192000", "-ac", "2", "-f", "mpegts", "pipe:1")
	}

	args = netutil.FilterFFmpegArgs(ffmpegPath, args)
	procCtx, cancel := context.WithCancel(r.Context())
	defer cancel()
	cmd := exec.CommandContext(procCtx, ffmpegPath, args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "failed to start dlna transcode")
		return
	}
	if err := cmd.Start(); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "failed to start dlna transcode")
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, stdout)
	cancel()
	_ = stdout.Close()
	_ = cmd.Wait()
}

func (h *Handler) resolveDLNAMediaPath(ctx context.Context, itemID string) (*sqlite.ItemRecord, string, error) {
	return h.resolveDLNAMediaPathForRequest(ctx, nil, itemID)
}

func (h *Handler) resolveDLNAMediaPathForRequest(ctx context.Context, r *http.Request, itemID string) (*sqlite.ItemRecord, string, error) {
	item, itemErr := h.libRepo.GetItemRecordByID(ctx, itemID)
	if itemErr == nil && item != nil {
		policy, denyAll := h.resolvePolicyForRequest(ctx, r)
		if denyAll || !h.isDLNAVisibleItem(ctx, item.Type, item.ID) || !h.isItemAllowedByPolicy(ctx, item, policy) {
			return nil, "", sqlite.ErrNotFound
		}
	}
	sources, err := h.libRepo.GetMediaSourcesByItemID(ctx, itemID)
	if err == nil && len(sources) > 0 && strings.TrimSpace(sources[0].Path) != "" {
		return item, sources[0].Path, nil
	}
	if itemErr != nil {
		return nil, "", itemErr
	}
	if item == nil || strings.TrimSpace(item.Path) == "" {
		return nil, "", sqlite.ErrNotFound
	}
	return item, item.Path, nil
}

func (h *Handler) isPathWithinRegisteredLibraries(ctx context.Context, realTarget string) bool {
	if h.libRepo == nil || realTarget == "" {
		return false
	}
	libs, err := h.libRepo.ListLibraries(ctx)
	if err != nil {
		return false
	}
	cleanTarget := filepath.Clean(realTarget)
	for _, lib := range libs {
		for _, rawRoot := range lib.Paths {
			rawRoot = strings.TrimSpace(rawRoot)
			if rawRoot == "" {
				continue
			}
			realRoot, err := filepath.EvalSymlinks(rawRoot)
			if err != nil {
				realRoot, err = filepath.Abs(rawRoot)
				if err != nil {
					continue
				}
			}
			realRoot = filepath.Clean(realRoot)
			rel, err := filepath.Rel(realRoot, cleanTarget)
			if err != nil {
				continue
			}
			if rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel) {
				return true
			}
		}
	}
	return false
}

func isRestrictedUserPolicy(p dto.UserPolicy) bool {
	return !p.EnableAllFolders ||
		!p.EnableMediaPlayback ||
		!p.EnableLiveTvAccess ||
		!p.EnableVideoPlaybackTranscoding ||
		!p.EnableAudioPlaybackTranscoding ||
		p.MaxParentalRating != nil ||
		len(p.BlockedTags) > 0 ||
		len(p.BlockUnratedItems) > 0 ||
		len(p.AllowedTags) > 0
}

// buildStrictestIntersectionPolicy computes the least-privilege intersection across all active users:
//   - Only libraries accessible to ALL active users are allowed;
//   - Minimum MaxParentalRating across all rated users;
//   - Union of all BlockedTags and BlockUnratedItems;
//   - Intersection of non-empty AllowedTags sets.
func buildStrictestIntersectionPolicy(users []*sqlite.UserRecord) (*dto.UserPolicy, bool) {
	var (
		hasRestricted          bool
		activeCount            int
		enableMediaPlayback    = true
		enableLiveTvAccess     = true
		enableVideoTranscoding = true
		enableAudioTranscoding = true
		folderInit             bool
		folderSet              map[string]bool
		minRating              *int
		blockedTags            = make(map[string]string)
		blockUnrated           = make(map[string]string)
		allowedInit            bool
		allowedSet             map[string]string
	)
	for _, u := range users {
		if u == nil || u.IsDisabled {
			continue
		}
		activeCount++
		if u.IsAdmin || u.Policy.IsAdministrator {
			continue
		}
		if !isRestrictedUserPolicy(u.Policy) {
			continue
		}
		hasRestricted = true
		p := u.Policy
		if !p.EnableMediaPlayback {
			enableMediaPlayback = false
		}
		if !p.EnableLiveTvAccess {
			enableLiveTvAccess = false
		}
		if !p.EnableVideoPlaybackTranscoding {
			enableVideoTranscoding = false
		}
		if !p.EnableAudioPlaybackTranscoding {
			enableAudioTranscoding = false
		}
		if !p.EnableAllFolders {
			cur := make(map[string]bool, len(p.EnabledFolders))
			for _, fid := range p.EnabledFolders {
				if norm := guid.Normalize(fid); norm != "" {
					cur[norm] = true
				}
			}
			if !folderInit {
				folderSet = cur
				folderInit = true
			} else {
				for k := range folderSet {
					if !cur[k] {
						delete(folderSet, k)
					}
				}
			}
		}
		if p.MaxParentalRating != nil {
			val := *p.MaxParentalRating
			if minRating == nil || val < *minRating {
				vCopy := val
				minRating = &vCopy
			}
		}
		for _, t := range p.BlockedTags {
			if s := strings.TrimSpace(t); s != "" {
				blockedTags[strings.ToLower(s)] = s
			}
		}
		for _, b := range p.BlockUnratedItems {
			if s := strings.TrimSpace(b); s != "" {
				blockUnrated[strings.ToLower(s)] = s
			}
		}
		if len(p.AllowedTags) > 0 {
			curAllow := make(map[string]string, len(p.AllowedTags))
			for _, at := range p.AllowedTags {
				if s := strings.TrimSpace(at); s != "" {
					curAllow[strings.ToLower(s)] = s
				}
			}
			if !allowedInit {
				allowedSet = curAllow
				allowedInit = true
			} else {
				for k := range allowedSet {
					if _, ok := curAllow[k]; !ok {
						delete(allowedSet, k)
					}
				}
			}
		}
	}
	if activeCount == 0 || !hasRestricted {
		return nil, false
	}
	if allowedInit && len(allowedSet) == 0 {
		return nil, true
	}

	out := dto.UserPolicy{
		EnableMediaPlayback:            enableMediaPlayback,
		EnableLiveTvAccess:             enableLiveTvAccess,
		EnableVideoPlaybackTranscoding: enableVideoTranscoding,
		EnableAudioPlaybackTranscoding: enableAudioTranscoding,
		EnableAllFolders:               !folderInit,
		MaxParentalRating:              minRating,
	}
	if folderInit {
		out.EnabledFolders = make([]string, 0, len(folderSet))
		for k := range folderSet {
			out.EnabledFolders = append(out.EnabledFolders, k)
		}
		sort.Strings(out.EnabledFolders)
	}
	for _, v := range blockedTags {
		out.BlockedTags = append(out.BlockedTags, v)
	}
	sort.Strings(out.BlockedTags)
	for _, v := range blockUnrated {
		out.BlockUnratedItems = append(out.BlockUnratedItems, v)
	}
	sort.Strings(out.BlockUnratedItems)
	if allowedInit {
		for _, v := range allowedSet {
			out.AllowedTags = append(out.AllowedTags, v)
		}
		sort.Strings(out.AllowedTags)
	}
	return &out, false
}

func (h *Handler) resolvePolicyForRequest(ctx context.Context, r *http.Request) (policy *dto.UserPolicy, denyAll bool) {
	// 1. Check explicit authenticated token/user in request context first.
	if r != nil {
		if ac, ok := middleware.AuthFromContext(r.Context()); ok && ac != nil && (ac.User != nil || ac.IsAPIKey) {
			if ac.IsAdmin() {
				return nil, false
			}
			h.mu.RLock()
			ur := h.userRepo
			h.mu.RUnlock()
			if ur != nil {
				u, err := ur.GetUserByID(ctx, guid.Normalize(ac.UserID()))
				if err != nil || u == nil || u.IsDisabled {
					return nil, true
				}
				if u.IsAdmin || u.Policy.IsAdministrator {
					return nil, false
				}
				p := u.Policy
				return &p, false
			}
			if ac.User != nil {
				if ac.User.IsDisabled {
					return nil, true
				}
				p := ac.User.Policy
				return &p, false
			}
		}
	}
	return h.resolveDefaultUserPolicy(ctx)
}

func (h *Handler) resolveDefaultUserPolicy(ctx context.Context) (policy *dto.UserPolicy, denyAll bool) {
	h.mu.RLock()
	uid := h.defaultUserID
	ur := h.userRepo
	h.mu.RUnlock()
	if uid == "" {
		uid = guid.Normalize(h.cfgSnapshot().DLNADefaultUserID)
	}
	if uid != "" {
		if ur == nil {
			return nil, true
		}
		u, err := ur.GetUserByID(ctx, uid)
		if err != nil || u == nil || u.IsDisabled {
			return nil, true
		}
		if u.IsAdmin || u.Policy.IsAdministrator {
			return nil, false
		}
		p := u.Policy
		return &p, false
	}
	// [H-02] Unauthenticated DLNA without configured DLNADefaultUserID:
	// inspect all users and enforce the strictest intersection if any restricted non-admin user exists.
	if ur == nil {
		return nil, false
	}
	users, err := ur.ListUsers(ctx)
	if err != nil {
		return nil, true
	}
	return buildStrictestIntersectionPolicy(users)
}

func (h *Handler) isLibraryAllowedByPolicy(libID string, policy *dto.UserPolicy) bool {
	return h.isLibraryAllowedByPolicyCtx(context.Background(), libID, policy)
}

func (h *Handler) isLibraryAllowedByPolicyCtx(ctx context.Context, libID string, policy *dto.UserPolicy) bool {
	if policy == nil || policy.IsAdministrator {
		return true
	}
	if h.libRepo != nil {
		return h.libRepo.CanUserAccessLibrary(ctx, *policy, libID)
	}
	if policy.EnableAllFolders {
		return true
	}
	normLib := guid.Normalize(libID)
	if normLib == "" {
		return false
	}
	for _, fid := range policy.EnabledFolders {
		if guid.Normalize(fid) == normLib {
			return true
		}
	}
	return false
}

func matchesDLNABlockUnrated(itemType, mediaType string, blockUnrated []string) bool {
	if strings.EqualFold(itemType, "CollectionFolder") ||
		strings.EqualFold(itemType, "Folder") ||
		strings.EqualFold(itemType, "UserView") {
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
		if strings.EqualFold(t, "Music") && (strings.EqualFold(itemType, "Audio") || strings.EqualFold(itemType, "MusicAlbum") || strings.EqualFold(itemType, "MusicArtist") || strings.EqualFold(mediaType, "Audio")) {
			return true
		}
	}
	return false
}

func (h *Handler) isItemAllowedByPolicy(ctx context.Context, rec *sqlite.ItemRecord, policy *dto.UserPolicy) bool {
	if rec == nil {
		return false
	}
	if policy == nil || policy.IsAdministrator {
		return true
	}
	if rec.LibraryID != "" && !strings.EqualFold(rec.Type, "Playlist") && !strings.EqualFold(rec.Type, "BoxSet") && !h.isLibraryAllowedByPolicyCtx(ctx, rec.LibraryID, policy) {
		return false
	}
	if h.libRepo != nil {
		blocked, err := h.libRepo.IsItemBlockedByPolicy(ctx, rec, *policy)
		if err != nil || blocked {
			return false
		}
	}
	return true
}

func (h *Handler) isItemAllowedByDefaultPolicy(ctx context.Context, rec *sqlite.ItemRecord) bool {
	if rec == nil {
		return false
	}
	policy, denyAll := h.resolveDefaultUserPolicy(ctx)
	if denyAll {
		return false
	}
	return h.isItemAllowedByPolicy(ctx, rec, policy)
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

func normalizeDLNARateIP(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	addr, err := netip.ParseAddr(s)
	if err != nil {
		return s
	}
	addr = addr.WithZone("").Unmap()
	if addr.Is6() {
		return netip.PrefixFrom(addr, 64).Masked().String()
	}
	return addr.String()
}

func parseCallbackURLs(raw string) ([]string, bool) {
	s := strings.TrimSpace(raw)
	if s == "" || len(s) > maxDLNACallbackLen {
		return nil, false
	}
	var urls []string
	for len(s) > 0 {
		s = strings.TrimSpace(s)
		if !strings.HasPrefix(s, "<") {
			return nil, false
		}
		end := strings.IndexByte(s, '>')
		if end <= 1 {
			return nil, false
		}
		rawURL := strings.TrimSpace(s[1:end])
		u, err := url.Parse(rawURL)
		if err != nil {
			return nil, false
		}
		scheme := strings.ToLower(u.Scheme)
		if (scheme != "http" && scheme != "https") || u.Host == "" || u.User != nil {
			return nil, false
		}
		host := strings.Trim(strings.TrimSpace(u.Hostname()), "[]")
		if host == "" || strings.EqualFold(host, "localhost") || strings.HasSuffix(strings.ToLower(host), ".localhost") {
			return nil, false
		}
		if addr, addrErr := netip.ParseAddr(host); addrErr == nil {
			if err := validateDLNALANCallbackAddr(addr); err != nil {
				return nil, false
			}
		}
		urls = append(urls, rawURL)
		s = s[end+1:]
	}
	return urls, len(urls) > 0
}

func isValidDLNACallback(raw string) bool {
	_, ok := parseCallbackURLs(raw)
	return ok
}

// validateDLNALANCallbackAddr enforces LAN-only UPnP GENA callbacks:
// strictly requires private RFC1918 / IPv6 ULA addresses, blocking loopback, link-local metadata, multicast, and public IPs.
func validateDLNALANCallbackAddr(addr netip.Addr) error {
	addr = addr.WithZone("").Unmap()
	if !addr.IsValid() || addr.IsUnspecified() || addr.IsLoopback() || addr.IsMulticast() {
		return errors.New("dlna callback address cannot be loopback, unspecified, or multicast")
	}
	// Block cloud metadata 169.254.169.254 explicitly; allow only RFC1918 / RFC4193 private LAN addresses.
	if !addr.IsPrivate() {
		return errors.New("dlna callback address must be a private LAN IP")
	}
	return nil
}

func newDLNALANCallbackClient(timeout time.Duration) *http.Client {
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	dialer := &net.Dialer{Timeout: timeout}
	tr := &http.Transport{
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(address)
			if err != nil {
				return nil, err
			}
			ips, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
			if err != nil || len(ips) == 0 {
				return nil, fmt.Errorf("resolve callback host: %w", err)
			}
			for _, ip := range ips {
				if vErr := validateDLNALANCallbackAddr(ip); vErr != nil {
					return nil, vErr
				}
			}
			target := net.JoinHostPort(ips[0].Unmap().String(), port)
			return dialer.DialContext(ctx, network, target)
		},
		TLSHandshakeTimeout:   timeout,
		ResponseHeaderTimeout: timeout,
		DisableKeepAlives:     true,
	}
	return &http.Client{
		Timeout:   timeout,
		Transport: tr,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

func (h *Handler) allowSOAPRequest(r *http.Request) bool {
	ip := normalizeDLNARateIP(netutil.ClientIP(r))
	if ip == "" {
		return true
	}
	now := time.Now().UTC()
	h.soapRateMu.Lock()
	defer h.soapRateMu.Unlock()
	if h.soapRates == nil {
		h.soapRates = make(map[string]*dlnaRateWindow)
	}
	for k, w := range h.soapRates {
		if !now.Before(w.resetAt) {
			delete(h.soapRates, k)
		}
	}
	w := h.soapRates[ip]
	if w == nil || !now.Before(w.resetAt) {
		if w == nil && len(h.soapRates) >= maxDLNARateBuckets {
			return false
		}
		w = &dlnaRateWindow{count: 0, resetAt: now.Add(dlnaSOAPRateWindow)}
		h.soapRates[ip] = w
	}
	if w.count >= maxDLNASOAPPerIPPerMin {
		return false
	}
	w.count++
	return true
}

func (h *Handler) handleEventSubscribe(service string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		uuid := r.PathValue("UuId")
		timeoutSec := parseTimeoutHeader(r.Header.Get("TIMEOUT"))
		sid := strings.TrimSpace(r.Header.Get("SID"))
		clientIP := normalizeDLNARateIP(netutil.ClientIP(r))
		now := time.Now().UTC()

		h.mu.Lock()
		defer h.mu.Unlock()

		// Prune expired subscriptions before checking maxDLNASubscriptions or processing renewal.
		for k, sub := range h.subscriptions {
			if now.After(sub.ExpiresAt) || sub.ExpiresAt.Equal(now) {
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
			if !isValidDLNACallback(callback) {
				http.Error(w, "invalid or missing CALLBACK header", http.StatusPreconditionFailed)
				return
			}
			if len(h.subscriptions) >= maxDLNASubscriptions {
				http.Error(w, "too many active subscriptions", http.StatusServiceUnavailable)
				return
			}
			if clientIP != "" {
				ipCount := 0
				for _, sub := range h.subscriptions {
					if sub.ClientIP == clientIP {
						ipCount++
					}
				}
				if ipCount >= maxDLNASubscriptionsPerIP {
					http.Error(w, "too many active subscriptions for this ip", http.StatusTooManyRequests)
					return
				}
			}
			sid = "uuid:" + guid.New()
			h.subscriptions[sid] = eventSubscription{
				SID:       sid,
				Service:   service,
				UUID:      uuid,
				Callback:  callback,
				ClientIP:  clientIP,
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

// BumpSystemUpdate increments the UPnP ContentDirectory SystemUpdateID when library contents change
// and asynchronously dispatches UPnP GENA NOTIFY XML messages to active LAN ContentDirectory subscribers.
func (h *Handler) BumpSystemUpdate() uint32 {
	now := time.Now().UTC()
	h.mu.Lock()
	h.systemUpdate++
	newID := h.systemUpdate
	var targets []eventSubscription
	for sid, sub := range h.subscriptions {
		if now.After(sub.ExpiresAt) || sub.ExpiresAt.Equal(now) {
			delete(h.subscriptions, sid)
			continue
		}
		if sub.Service == "contentdirectory" {
			currSeq := sub.Seq
			sub.Seq++
			h.subscriptions[sid] = sub
			subCopy := sub
			subCopy.Seq = currSeq
			targets = append(targets, subCopy)
		}
	}
	client := h.notifyClient
	h.mu.Unlock()

	if len(targets) > 0 {
		if client == nil {
			client = newDLNALANCallbackClient(3 * time.Second)
		}
		payload := fmt.Sprintf(`<?xml version="1.0" encoding="utf-8"?>`+"\n"+
			`<e:propertyset xmlns:e="urn:schemas-upnp-org:event-1-0">`+"\n"+
			`  <e:property><SystemUpdateID>%d</SystemUpdateID></e:property>`+"\n"+
			`</e:propertyset>`, newID)
		for _, sub := range targets {
			cbURLs, ok := parseCallbackURLs(sub.Callback)
			if !ok || len(cbURLs) == 0 {
				continue
			}
			targetURL := cbURLs[0]
			subCopy := sub
			go func() {
				if h.notifySem != nil {
					select {
					case h.notifySem <- struct{}{}:
						defer func() { <-h.notifySem }()
					default:
						return
					}
				}
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				defer cancel()
				req, err := http.NewRequestWithContext(ctx, "NOTIFY", targetURL, strings.NewReader(payload))
				if err != nil {
					return
				}
				req.Header.Set("CONTENT-TYPE", `text/xml; charset="utf-8"`)
				req.Header.Set("NT", "upnp:event")
				req.Header.Set("NTS", "upnp:propchange")
				req.Header.Set("SID", subCopy.SID)
				req.Header.Set("SEQ", strconv.FormatUint(uint64(subCopy.Seq), 10))
				resp, err := client.Do(req)
				if err == nil && resp != nil {
					_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
					_ = resp.Body.Close()
				}
			}()
		}
	}
	return newID
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
	if r.TLS != nil {
		scheme = "https"
	} else if strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		if addr, ok := parseRemoteAddr(r.RemoteAddr); ok && netutil.IsTrustedProxyIP(addr) {
			scheme = "https"
		}
	}
	host := r.Host
	if host == "" {
		host = "127.0.0.1:8096"
	}
	return scheme + "://" + host
}

func parseRemoteAddr(raw string) (netip.Addr, bool) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return netip.Addr{}, false
	}
	if host, _, err := net.SplitHostPort(s); err == nil {
		s = host
	}
	s = strings.Trim(s, "[]")
	addr, err := netip.ParseAddr(s)
	if err != nil {
		return netip.Addr{}, false
	}
	return addr.WithZone("").Unmap(), true
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
