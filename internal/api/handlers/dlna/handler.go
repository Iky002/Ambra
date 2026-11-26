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
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ambra-media/ambra/internal/api/dto"
	"github.com/ambra-media/ambra/internal/api/httpx"
	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/pkg/netutil"
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
	ExpiresAt time.Time
}

type dlnaRateWindow struct {
	count   int
	resetAt time.Time
}

// Handler implements Emby-compatible DLNA Profile management and UPnP MediaServer:1 endpoints.
type Handler struct {
	cfg           *config.Config
	libRepo       *sqlite.LibraryRepo
	store         *sqlite.Store
	userRepo      *sqlite.UserRepo
	defaultUserID string
	auth          AuthFuncs
	streamSecret  []byte

	soapRateMu sync.Mutex
	soapRates  map[string]*dlnaRateWindow

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
	snap := cfg.Snapshot()
	secretInput := append([]byte(snap.ServerID+":"), randBytes...)
	secretSum := sha256.Sum256(secretInput)

	h := &Handler{
		cfg:           cfg,
		libRepo:       libRepo,
		store:         store,
		streamSecret:  secretSum[:],
		soapRates:     make(map[string]*dlnaRateWindow),
		profiles:      make(map[string]DlnaProfile),
		subscriptions: make(map[string]eventSubscription),
		systemUpdate:  1,
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

	item, mediaPath, err := h.resolveDLNAMediaPath(r.Context(), itemID)
	if errors.Is(err, sqlite.ErrNotFound) {
		httpx.Error(w, http.StatusNotFound, "media item not found")
		return
	}
	if err != nil {
		slog.Error("dlna: failed to resolve media path", "itemId", itemID, "err", err)
		httpx.Error(w, http.StatusInternalServerError, "failed to load media item")
		return
	}
	if (item != nil && item.IsStrm) || strings.EqualFold(filepath.Ext(mediaPath), ".strm") {
		httpx.Error(w, http.StatusForbidden, "direct download of .strm files is not permitted")
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
	if strings.EqualFold(filepath.Ext(realPath), ".strm") {
		httpx.Error(w, http.StatusForbidden, "direct download of .strm files is not permitted")
		return
	}
	if !h.isPathWithinRegisteredLibraries(r.Context(), realPath) {
		httpx.Error(w, http.StatusForbidden, "media file path is outside registered libraries")
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
	if ct := dlnaMediaContentType(realPath); ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	w.Header().Set("SERVER", defaultServerHeader)
	http.ServeContent(w, r, stat.Name(), stat.ModTime(), f)
}

func (h *Handler) resolveDLNAMediaPath(ctx context.Context, itemID string) (*sqlite.ItemRecord, string, error) {
	item, itemErr := h.libRepo.GetItemRecordByID(ctx, itemID)
	if itemErr == nil && item != nil {
		if !h.isDLNAVisibleItem(ctx, item.Type, item.ID) || !h.isItemAllowedByDefaultPolicy(ctx, item) {
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

func (h *Handler) resolveDefaultUserPolicy(ctx context.Context) (policy *dto.UserPolicy, denyAll bool) {
	h.mu.RLock()
	uid := h.defaultUserID
	ur := h.userRepo
	h.mu.RUnlock()
	if uid == "" || ur == nil {
		return nil, false
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

func (h *Handler) isLibraryAllowedByPolicy(libID string, policy *dto.UserPolicy) bool {
	if policy == nil || policy.EnableAllFolders {
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

func (h *Handler) isItemAllowedByDefaultPolicy(ctx context.Context, rec *sqlite.ItemRecord) bool {
	if rec == nil {
		return false
	}
	policy, denyAll := h.resolveDefaultUserPolicy(ctx)
	if denyAll {
		return false
	}
	if policy == nil {
		return true
	}
	if rec.LibraryID != "" && !h.isLibraryAllowedByPolicy(rec.LibraryID, policy) {
		return false
	}
	if h.libRepo != nil && (policy.MaxParentalRating != nil || len(policy.BlockedTags) > 0) {
		if blocked, err := h.libRepo.IsItemBlockedByParentalPolicy(ctx, rec, policy.MaxParentalRating, policy.BlockedTags); err != nil || blocked {
			return false
		}
	}
	return true
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

func isValidDLNACallback(raw string) bool {
	s := strings.TrimSpace(raw)
	if s == "" || len(s) > maxDLNACallbackLen {
		return false
	}
	found := 0
	for len(s) > 0 {
		s = strings.TrimSpace(s)
		if !strings.HasPrefix(s, "<") {
			return false
		}
		end := strings.IndexByte(s, '>')
		if end <= 1 {
			return false
		}
		rawURL := strings.TrimSpace(s[1:end])
		u, err := url.Parse(rawURL)
		if err != nil {
			return false
		}
		scheme := strings.ToLower(u.Scheme)
		if (scheme != "http" && scheme != "https") || u.Host == "" {
			return false
		}
		found++
		s = s[end+1:]
	}
	return found > 0
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

// BumpSystemUpdate increments the UPnP ContentDirectory SystemUpdateID when library contents change.
func (h *Handler) BumpSystemUpdate() uint32 {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.systemUpdate++
	return h.systemUpdate
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
