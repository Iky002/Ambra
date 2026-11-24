package compatmedia

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/ambra-media/ambra/internal/api/httpx"
	"github.com/ambra-media/ambra/internal/api/middleware"
	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/core/imaging"
	"github.com/ambra-media/ambra/internal/core/scanner"
	"github.com/ambra-media/ambra/internal/core/transcode"
	"github.com/ambra-media/ambra/internal/core/watcher"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/plugins"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

// AuthFuncs decouples route registration from the concrete middleware package.
type AuthFuncs struct {
	RequireAuth   func(http.Handler) http.Handler
	RequireAdmin  func(http.Handler) http.Handler
	UserIDFromCtx func(context.Context) string
}

// Handler implements the remaining Emby 4.10.2.0 media, item, metadata, image,
// encoding, library, taxonomy, audio, and video compatibility endpoints.
type Handler struct {
	repo         *sqlite.LibraryRepo
	userRepo     *sqlite.UserRepo
	scanner      *scanner.Scanner
	watcher      *watcher.Watcher
	imaging      *imaging.Service
	registry     *plugins.Registry
	store        *sqlite.Store
	cfg          *config.Config
	extractor    *transcode.Extractor
	transcodeMgr *transcode.Manager
	httpClient   *http.Client
	auth         AuthFuncs
}

// New creates a new compatmedia Handler.
func New(
	repo *sqlite.LibraryRepo,
	sc *scanner.Scanner,
	img *imaging.Service,
	reg *plugins.Registry,
	store *sqlite.Store,
	cfg *config.Config,
) *Handler {
	return &Handler{
		repo:      repo,
		scanner:   sc,
		imaging:   img,
		registry:  reg,
		store:     store,
		cfg:       cfg,
		extractor: transcode.NewExtractor(cfg),
	}
}

// SetWatcher attaches a filesystem Watcher so library path edits and deletions dynamically update watches.
func (h *Handler) SetWatcher(w *watcher.Watcher) {
	h.watcher = w
}

// SetUserRepo attaches a UserRepo for per-user library access control.
func (h *Handler) SetUserRepo(userRepo *sqlite.UserRepo) {
	h.userRepo = userRepo
}

// SetExtractor overrides the embedded subtitle & font extractor used by compatmedia endpoints.
func (h *Handler) SetExtractor(ext *transcode.Extractor) {
	h.extractor = ext
}

// SetTranscodeManager attaches a transcode Manager so ActiveEncodings/Delete stops active sessions.
func (h *Handler) SetTranscodeManager(mgr *transcode.Manager) {
	h.transcodeMgr = mgr
}

// SetHTTPClient overrides the HTTP client used for remote image downloading and proxying.
func (h *Handler) SetHTTPClient(c *http.Client) {
	h.httpClient = c
}

func (h *Handler) getHTTPClient() *http.Client {
	if h.httpClient != nil {
		return h.httpClient
	}
	return newSafeRemoteHTTPClient()
}

// BuildRouter builds an isolated chi.Router containing all 21 media-domain route prefixes
// with zero ServeMux wildcard conflicts.
func (h *Handler) BuildRouter(auth AuthFuncs) http.Handler {
	h.auth = auth
	wrapAuth := auth.RequireAuth
	if wrapAuth == nil {
		wrapAuth = passthrough
	}
	wrapAdmin := auth.RequireAdmin
	if wrapAdmin == nil {
		wrapAdmin = wrapAuth
	}

	r := chi.NewRouter()

	authFn := func(fn http.HandlerFunc) http.HandlerFunc {
		wrapped := wrapAuth(fn)
		return wrapped.ServeHTTP
	}
	adminFn := func(fn http.HandlerFunc) http.HandlerFunc {
		wrapped := wrapAdmin(fn)
		return wrapped.ServeHTTP
	}

	// --- /Artists (10 routes) ---
	r.Get("/Artists", authFn(h.handleListArtists(false)))
	r.Get("/Artists/AlbumArtists", authFn(h.handleListArtists(true)))
	r.Get("/Artists/Prefixes", authFn(h.handleArtistPrefixes))
	r.Get("/Artists/InstantMix", authFn(h.handleInstantMixByQuery))
	r.Get("/Artists/{Id}/Similar", authFn(h.handleSimilar))
	r.Get("/Artists/{Name}", authFn(h.handleGetTaxonomyByName("artist", "MusicArtist")))
	r.Get("/Artists/{Name}/Images/{Type}", h.handleEntityImage("artist"))
	r.Head("/Artists/{Name}/Images/{Type}", h.handleEntityImage("artist"))
	r.Get("/Artists/{Name}/Images/{Type}/{Index}", h.handleEntityImage("artist"))
	r.Head("/Artists/{Name}/Images/{Type}/{Index}", h.handleEntityImage("artist"))

	// --- /Audio (3 routes) ---
	r.Get("/Audio/{Id}/hls1/{PlaylistId}/{Segment}", authFn(h.handleAudioHlsSegment))
	r.Head("/Audio/{Id}/hls1/{PlaylistId}/{Segment}", authFn(h.handleAudioHlsSegment))
	r.Get("/Audio/{Id}/hls/{PlaylistId}/{Segment}", authFn(h.handleAudioHlsSegment))

	// --- /AudioBooks (1 route) ---
	r.Get("/AudioBooks/NextUp", authFn(h.handleAudioBooksNextUp))

	// --- /Channels (1 route) ---
	r.Get("/Channels/Items/Latest", authFn(h.handleChannelLatestItems))

	// --- /Collections (3 routes) ---
	r.Get("/Collections/{Id}/Missing", authFn(h.handleCollectionMissing))
	r.Get("/Collections/{Id}/ProviderItems", authFn(h.handleCollectionProviderItems))
	r.Post("/Collections/{Id}/Items/Delete", adminFn(h.handleContainerItemsDelete))

	// --- /Encoding (11 routes) ---
	r.Get("/Encoding/ToneMapOptions", adminFn(h.handleGetToneMapOptions))
	r.Get("/Encoding/FullToneMapOptions", adminFn(h.handleGetEncodingConfig("encoding_full_tonemap", defaultFullToneMapOptions)))
	r.Post("/Encoding/FullToneMapOptions", adminFn(h.handleSetEncodingConfig("encoding_full_tonemap")))
	r.Get("/Encoding/PublicToneMapOptions", adminFn(h.handleGetEncodingConfig("encoding_public_tonemap", defaultPublicToneMapOptions)))
	r.Post("/Encoding/PublicToneMapOptions", adminFn(h.handleSetEncodingConfig("encoding_public_tonemap")))
	r.Get("/Encoding/SubtitleOptions", adminFn(h.handleGetEncodingConfig("encoding_subtitle_options", defaultSubtitleOptions)))
	r.Post("/Encoding/SubtitleOptions", adminFn(h.handleSetEncodingConfig("encoding_subtitle_options")))
	r.Get("/Encoding/FfmpegOptions", adminFn(h.handleGetEncodingConfig("encoding_ffmpeg_options", defaultFfmpegOptions)))
	r.Post("/Encoding/FfmpegOptions", adminFn(h.handleSetEncodingConfig("encoding_ffmpeg_options")))
	r.Get("/Encoding/CodecParameters", adminFn(h.handleGetCodecParameters))
	r.Post("/Encoding/CodecParameters", adminFn(h.handleSetCodecParameters))

	// --- /GameGenres (5 routes) ---
	r.Get("/GameGenres/{Name}", authFn(h.handleGetTaxonomyByName("gamegenre", "GameGenre")))
	r.Get("/GameGenres/{Name}/Images/{Type}", h.handleEntityImage("gamegenre"))
	r.Head("/GameGenres/{Name}/Images/{Type}", h.handleEntityImage("gamegenre"))
	r.Get("/GameGenres/{Name}/Images/{Type}/{Index}", h.handleEntityImage("gamegenre"))
	r.Head("/GameGenres/{Name}/Images/{Type}/{Index}", h.handleEntityImage("gamegenre"))

	// --- /Games (1 route) ---
	r.Get("/Games/{Id}/Similar", authFn(h.handleSimilar))

	// --- /Genres (5 routes) ---
	r.Get("/Genres/{Name}", authFn(h.handleGetTaxonomyByName("genre", "Genre")))
	r.Get("/Genres/{Name}/Images/{Type}", h.handleEntityImage("genre"))
	r.Head("/Genres/{Name}/Images/{Type}", h.handleEntityImage("genre"))
	r.Get("/Genres/{Name}/Images/{Type}/{Index}", h.handleEntityImage("genre"))
	r.Head("/Genres/{Name}/Images/{Type}/{Index}", h.handleEntityImage("genre"))

	// --- /Images (1 route) ---
	r.Get("/Images/Remote", authFn(h.handleRemoteImageProxy))

	// --- /Items (60 missing + 3 extra item relation routes) ---
	r.Get("/Items/{Id}/ExternalIdInfos", authFn(h.handleExternalIDInfos))
	r.Post("/Items/RemoteSearch/Movie", adminFn(h.handleRemoteSearch("Movie")))
	r.Post("/Items/RemoteSearch/Trailer", adminFn(h.handleRemoteSearch("Trailer")))
	r.Post("/Items/RemoteSearch/MusicVideo", adminFn(h.handleRemoteSearch("MusicVideo")))
	r.Post("/Items/RemoteSearch/Series", adminFn(h.handleRemoteSearch("Series")))
	r.Post("/Items/RemoteSearch/Game", adminFn(h.handleRemoteSearch("Game")))
	r.Post("/Items/RemoteSearch/BoxSet", adminFn(h.handleRemoteSearch("BoxSet")))
	r.Post("/Items/RemoteSearch/MusicArtist", adminFn(h.handleRemoteSearch("MusicArtist")))
	r.Post("/Items/RemoteSearch/MusicAlbum", adminFn(h.handleRemoteSearch("MusicAlbum")))
	r.Post("/Items/RemoteSearch/Person", adminFn(h.handleRemoteSearch("Person")))
	r.Post("/Items/RemoteSearch/Book", adminFn(h.handleRemoteSearch("Book")))
	r.Get("/Items/RemoteSearch/Image", adminFn(h.handleRemoteImageProxy))
	r.Post("/Items/RemoteSearch/Apply/{Id}", adminFn(h.handleRemoteSearchApply))
	r.Post("/Items/Metadata/Reset", adminFn(h.handleMetadataReset))
	r.Post("/Items/{Id}/Refresh", adminFn(h.handleItemRefresh))
	r.Post("/Items/Delete", adminFn(h.handleDeleteItemsBatch))
	r.Delete("/Items", adminFn(h.handleDeleteItemsBatch))
	r.Post("/Items/Access", authFn(h.handleItemsAccess))
	r.Post("/Items/Shared/Leave", authFn(h.handleSharedLeave))
	r.Post("/Items/{ItemId}", adminFn(h.handleUpdateItem))
	r.Get("/Items/{ItemId}/MetadataEditor", adminFn(h.handleMetadataEditor))
	r.Post("/Items/{Id}/Tags/Add", adminFn(h.handleItemTags(true)))
	r.Post("/Items/{Id}/Tags/Delete", adminFn(h.handleItemTags(false)))
	r.Post("/Items/{Id}/MakePublic", authFn(h.handleItemVisibility(true)))
	r.Post("/Items/{Id}/MakePrivate", authFn(h.handleItemVisibility(false)))
	r.Post("/Items/{Id}/Subtitles", authFn(h.handleUploadSubtitle))
	r.Delete("/Items/{Id}/Subtitles/{Index}", adminFn(h.handleDeleteSubtitle))
	r.Post("/Items/{Id}/Subtitles/{Index}/Delete", adminFn(h.handleDeleteSubtitle))
	r.Get("/Items/{Id}/RemoteSearch/Subtitles/{Language}", authFn(h.handleRemoteSearchSubtitles))
	r.Post("/Items/{Id}/RemoteSearch/Subtitles/{SubtitleId}", authFn(h.handleDownloadRemoteSubtitle))
	r.Get("/Items/{Id}/{MediaSourceId}/Subtitles/{Index}/{StreamFile}", authFn(h.handleSubtitleStream))
	r.Head("/Items/{Id}/{MediaSourceId}/Subtitles/{Index}/{StreamFile}", authFn(h.handleSubtitleStream))
	r.Get("/Items/{Id}/{MediaSourceId}/Subtitles/{Index}/{StartPositionTicks}/{StreamFile}", authFn(h.handleSubtitleStream))
	r.Head("/Items/{Id}/{MediaSourceId}/Subtitles/{Index}/{StartPositionTicks}/{StreamFile}", authFn(h.handleSubtitleStream))
	r.Get("/Items/{Id}/InstantMix", authFn(h.handleItemInstantMix))
	r.Get("/Items/{Id}/CriticReviews", authFn(h.handleEmptyQueryResult))
	r.Get("/Items/{Id}/ThemeSongs", authFn(h.handleThemeMediaResult))
	r.Get("/Items/{Id}/ThemeVideos", authFn(h.handleThemeMediaResult))
	r.Get("/Items/{Id}/ThemeMedia", authFn(h.handleAllThemeMedia))
	r.Get("/Items/{Id}/LocalTrailers", authFn(h.handleEmptyItemArray))
	r.Get("/Items/{Id}/SpecialFeatures", authFn(h.handleEmptyItemArray))
	r.Get("/Items/{Id}/Intros", authFn(h.handleEmptyQueryResult))
	r.Post("/Items/{Id}/Delete", adminFn(h.handleDeleteItem))
	r.Delete("/Items/{Id}", adminFn(h.handleDeleteItem))
	r.Get("/Items/{Id}/Ancestors", authFn(h.handleItemAncestors))
	r.Get("/Items/{Id}/File", authFn(h.handleItemFile))
	r.Get("/Items/{Id}/Similar", authFn(h.handleSimilar))
	r.Get("/Items/{Id}/DeleteInfo", authFn(h.handleItemDeleteInfo))
	r.Get("/Items/{Id}/Images", authFn(h.handleListItemImages))
	r.Get("/Items/{Id}/Images/{Type}/{Index}/{Tag}/{Format}/{MaxWidth}/{MaxHeight}/{PercentPlayed}/{UnPlayedCount}", h.handleServeItemImageFull)
	r.Head("/Items/{Id}/Images/{Type}/{Index}/{Tag}/{Format}/{MaxWidth}/{MaxHeight}/{PercentPlayed}/{UnPlayedCount}", h.handleServeItemImageFull)
	r.Post("/Items/{Id}/Images/{Type}/{Index}/Index", adminFn(h.handleUpdateImageIndex))
	r.Post("/Items/{Id}/Images/{Type}/Url", adminFn(h.handleSetImageURL))
	r.Post("/Items/{Id}/Images/{Type}/{Index}/Url", adminFn(h.handleSetImageURL))
	r.Delete("/Items/{Id}/Images/{Type}", adminFn(h.handleDeleteItemImage))
	r.Delete("/Items/{Id}/Images/{Type}/{Index}", adminFn(h.handleDeleteItemImage))
	r.Post("/Items/{Id}/Images/{Type}/Delete", adminFn(h.handleDeleteItemImage))
	r.Post("/Items/{Id}/Images/{Type}/{Index}/Delete", adminFn(h.handleDeleteItemImage))
	r.Post("/Items/{Id}/Images/{Type}", adminFn(h.handleUploadItemImage))
	r.Post("/Items/{Id}/Images/{Type}/{Index}", adminFn(h.handleUploadItemImage))
	r.Get("/Items/{Id}/RemoteImages", authFn(h.handleRemoteImages))
	r.Get("/Items/{Id}/RemoteImages/Providers", authFn(h.handleRemoteImageProviders))
	r.Post("/Items/{Id}/RemoteImages/Download", adminFn(h.handleDownloadRemoteImage))
	r.Get("/Items/{Id}/thumbnails", authFn(h.handleItemThumbnails))
	r.Get("/Items/{Id}/ThumbnailSet", authFn(h.handleItemThumbnailSet))

	// --- /Libraries (1 route) ---
	r.Get("/Libraries/AvailableOptions", adminFn(h.handleLibraryAvailableOptions))

	// --- /Library (15 routes) ---
	r.Get("/Library/PhysicalPaths", adminFn(h.handlePhysicalPaths))
	r.Get("/Library/SelectableMediaFolders", authFn(h.handleSelectableMediaFolders))
	r.Get("/Library/MediaFolders", authFn(h.handleMediaFolders))
	r.Post("/Library/Series/Added", adminFn(h.handleLibraryMediaHook))
	r.Post("/Library/Series/Updated", adminFn(h.handleLibraryMediaHook))
	r.Post("/Library/Movies/Added", adminFn(h.handleLibraryMediaHook))
	r.Post("/Library/Movies/Updated", adminFn(h.handleLibraryMediaHook))
	r.Get("/Library/VirtualFolders/Query", authFn(h.handleQueryVirtualFolders))
	r.Post("/Library/VirtualFolders/Delete", adminFn(h.handleDeleteVirtualFolder))
	r.Post("/Library/VirtualFolders/Name", adminFn(h.handleRenameVirtualFolder))
	r.Post("/Library/VirtualFolders/Paths", adminFn(h.handleAddVirtualFolderPath))
	r.Post("/Library/VirtualFolders/Paths/Update", adminFn(h.handleUpdateVirtualFolderPath))
	r.Delete("/Library/VirtualFolders/Paths", adminFn(h.handleDeleteVirtualFolderPath))
	r.Post("/Library/VirtualFolders/Paths/Delete", adminFn(h.handleDeleteVirtualFolderPath))
	r.Post("/Library/VirtualFolders/LibraryOptions", adminFn(h.handleUpdateVirtualFolderOptions))

	// --- /MusicGenres (7 routes) ---
	r.Get("/MusicGenres/InstantMix", authFn(h.handleInstantMixByQuery))
	r.Get("/MusicGenres/{Name}", authFn(h.handleGetTaxonomyByName("musicgenre", "MusicGenre")))
	r.Get("/MusicGenres/{Name}/InstantMix", authFn(h.handleInstantMixByQuery))
	r.Get("/MusicGenres/{Name}/Images/{Type}", h.handleEntityImage("musicgenre"))
	r.Head("/MusicGenres/{Name}/Images/{Type}", h.handleEntityImage("musicgenre"))
	r.Get("/MusicGenres/{Name}/Images/{Type}/{Index}", h.handleEntityImage("musicgenre"))
	r.Head("/MusicGenres/{Name}/Images/{Type}/{Index}", h.handleEntityImage("musicgenre"))

	// --- /Persons (6 routes) ---
	r.Get("/Persons/{Id}/Credits", authFn(h.handlePersonCredits))
	r.Get("/Persons/{Name}", authFn(h.handleGetPersonByName))
	r.Get("/Persons/{Name}/Images/{Type}", h.handleEntityImage("person"))
	r.Head("/Persons/{Name}/Images/{Type}", h.handleEntityImage("person"))
	r.Get("/Persons/{Name}/Images/{Type}/{Index}", h.handleEntityImage("person"))
	r.Head("/Persons/{Name}/Images/{Type}/{Index}", h.handleEntityImage("person"))

	// --- /Playlists (2 routes) ---
	r.Get("/Playlists/{Id}/AddToPlaylistInfo", authFn(h.handleAddToPlaylistInfo))
	r.Post("/Playlists/{Id}/Items/Delete", authFn(h.handleContainerItemsDelete))

	// --- /Providers (1 route) ---
	r.Get("/Providers/Subtitles/Subtitles/{Id}", authFn(h.handleProviderSubtitleDownload))

	// --- /Shows (3 routes) ---
	r.Get("/Shows/Missing", authFn(h.handleEmptyQueryResult))
	r.Get("/Shows/Upcoming", authFn(h.handleShowsUpcoming))
	r.Get("/Shows/{Id}/Similar", authFn(h.handleSimilar))

	// --- /Studios (5 routes) ---
	r.Get("/Studios/{Name}", authFn(h.handleGetTaxonomyByName("studio", "Studio")))
	r.Get("/Studios/{Name}/Images/{Type}", h.handleEntityImage("studio"))
	r.Head("/Studios/{Name}/Images/{Type}", h.handleEntityImage("studio"))
	r.Get("/Studios/{Name}/Images/{Type}/{Index}", h.handleEntityImage("studio"))
	r.Head("/Studios/{Name}/Images/{Type}/{Index}", h.handleEntityImage("studio"))

	// --- /Trailers (1 route) ---
	r.Get("/Trailers/{Id}/Similar", authFn(h.handleSimilar))

	// --- /Videos (10 routes) ---
	r.Delete("/Videos/{Id}/AlternateSources", adminFn(h.handleDeleteAlternateSources))
	r.Post("/Videos/{Id}/AlternateSources/Delete", adminFn(h.handleDeleteAlternateSources))
	r.Post("/Videos/MergeVersions", adminFn(h.handleMergeVersions))
	r.Post("/Videos/{Id}/Subtitles", authFn(h.handleUploadSubtitle))
	r.Delete("/Videos/{Id}/Subtitles/{Index}", adminFn(h.handleDeleteSubtitle))
	r.Post("/Videos/{Id}/Subtitles/{Index}/Delete", adminFn(h.handleDeleteSubtitle))
	r.Get("/Videos/{Id}/{MediaSourceId}/Subtitles/{Index}/{StartPositionTicks}/{StreamFile}", authFn(h.handleSubtitleStream))
	r.Head("/Videos/{Id}/{MediaSourceId}/Subtitles/{Index}/{StartPositionTicks}/{StreamFile}", authFn(h.handleSubtitleStream))
	r.Get("/Videos/{Id}/{MediaSourceId}/Attachments", authFn(h.handleListVideoAttachments))
	r.Get("/Videos/{Id}/{MediaSourceId}/Attachments/{Index}", authFn(h.handleVideoAttachmentStream))
	r.Head("/Videos/{Id}/{MediaSourceId}/Attachments/{Index}", authFn(h.handleVideoAttachmentStream))
	r.Get("/Videos/{Id}/{MediaSourceId}/Attachments/{Index}/Stream", authFn(h.handleVideoAttachmentStream))
	r.Head("/Videos/{Id}/{MediaSourceId}/Attachments/{Index}/Stream", authFn(h.handleVideoAttachmentStream))
	r.Post("/Videos/ActiveEncodings/Delete", authFn(h.handleDeleteActiveEncodings))
	r.Get("/Videos/{Id}/hls/{PlaylistId}/{Segment}", authFn(h.handleVideoHlsSegment))

	return r
}

func (h *Handler) serverID() string {
	if h.cfg != nil {
		return h.cfg.ServerID
	}
	return ""
}

func (h *Handler) resolveUserID(r *http.Request, q httpx.Query) string {
	if uid := chi.URLParam(r, "UserId"); uid != "" {
		return guid.Normalize(uid)
	}
	if uid := q.Get("UserId"); uid != "" {
		return guid.Normalize(uid)
	}
	if h.auth.UserIDFromCtx != nil {
		return guid.Normalize(h.auth.UserIDFromCtx(r.Context()))
	}
	return ""
}

// allowedLibrariesForRequest returns nil if the caller has unrestricted access to all libraries,
// or a non-nil slice of normalized library IDs (possibly empty) if restricted.
func (h *Handler) allowedLibrariesForRequest(r *http.Request, fallbackUserID string) []string {
	if h.userRepo == nil {
		return nil
	}
	if ac, ok := middleware.AuthFromContext(r.Context()); ok && ac != nil && (ac.User != nil || ac.IsAPIKey) {
		if ac.IsAdmin() {
			return nil
		}
		return h.allowedLibrariesForUser(r.Context(), guid.Normalize(ac.UserID()))
	}
	uid := ""
	if h.auth.UserIDFromCtx != nil {
		uid = guid.Normalize(h.auth.UserIDFromCtx(r.Context()))
	}
	if uid == "" {
		uid = guid.Normalize(fallbackUserID)
	}
	return h.allowedLibrariesForUser(r.Context(), uid)
}

func (h *Handler) allowedLibrariesForUser(ctx context.Context, userID string) []string {
	if h.userRepo == nil || userID == "" {
		return nil
	}
	u, err := h.userRepo.GetUserByID(ctx, userID)
	if err != nil || u == nil {
		return nil
	}
	if u.IsAdmin || u.Policy.IsAdministrator || u.Policy.EnableAllFolders {
		return nil
	}
	allowed := make([]string, 0, len(u.Policy.EnabledFolders))
	for _, fid := range u.Policy.EnabledFolders {
		if norm := guid.Normalize(fid); norm != "" {
			allowed = append(allowed, norm)
		}
	}
	return allowed
}

func isLibraryAllowed(libID string, allowed []string) bool {
	if allowed == nil {
		return true
	}
	norm := guid.Normalize(libID)
	for _, id := range allowed {
		if id == norm {
			return true
		}
	}
	return false
}

func (h *Handler) getConfigJSON(ctx context.Context, key string, dst any) (bool, error) {
	if h.store == nil {
		return false, nil
	}
	var raw string
	err := h.store.Reader.QueryRowContext(ctx, `SELECT value_json FROM server_config WHERE key = ?`, key).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if err := json.Unmarshal([]byte(raw), dst); err != nil {
		return false, err
	}
	return true, nil
}

func (h *Handler) setConfigJSON(ctx context.Context, key string, val any) error {
	if h.store == nil {
		return nil
	}
	raw, err := json.Marshal(val)
	if err != nil {
		return err
	}
	_, err = h.store.Writer.ExecContext(ctx,
		`INSERT INTO server_config (key, value_json, updated_at) VALUES (?, ?, ?)
		 ON CONFLICT(key) DO UPDATE SET value_json = excluded.value_json, updated_at = excluded.updated_at`,
		key, string(raw), time.Now().Unix(),
	)
	return err
}

func passthrough(next http.Handler) http.Handler { return next }

func nonEmpty(vals ...string) string {
	for _, v := range vals {
		if s := strings.TrimSpace(v); s != "" {
			return s
		}
	}
	return ""
}
