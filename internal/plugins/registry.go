package plugins

import (
	"context"
	"log/slog"
	"sort"
	"strings"
	"sync"
)

// BuiltInIDPrefix is the reserved plugin ID prefix exclusively for built-in plugins.
const BuiltInIDPrefix = "ambra.builtin."

// IsBuiltInID reports whether id uses the reserved built-in plugin namespace.
func IsBuiltInID(id string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(id)), BuiltInIDPrefix)
}

// Kind identifies what capabilities a plugin provides.
type Kind string

const (
	KindMetadata Kind = "metadata"
	KindImage    Kind = "image"
	KindSubtitle Kind = "subtitle"
	KindStream   Kind = "stream"
	KindNotifier Kind = "notifier"
)

// Manifest describes a built-in or external (WASM / HTTP) plugin.
type Manifest struct {
	ID          string   `json:"Id"`
	Name        string   `json:"Name"`
	Version     string   `json:"Version"`
	Description string   `json:"Description"`
	Author      string   `json:"Author,omitempty"`
	BuiltIn     bool     `json:"BuiltIn"`
	Enabled     bool     `json:"Enabled"`
	Priority    int      `json:"Priority"`
	Kinds       []Kind   `json:"Kinds"`
}

// Plugin is the common interface implemented by all Ambra plugins.
type Plugin interface {
	Manifest() Manifest
	Configure(cfg map[string]any) error
}

// ServerConfigFilter limits which settings belong in global plugin configuration.
// Account credentials for plugins with personal bindings stay outside server_config.
type ServerConfigFilter interface {
	FilterServerConfig(map[string]any) map[string]any
}

// MediaQuery carries hints for metadata/image/subtitle providers.
type MediaQuery struct {
	ItemType      string // Movie / Series / Season / Episode
	Title         string
	OriginalTitle string
	Year          int
	SeasonNumber  int
	EpisodeNumber int
	Path          string
	ProviderIDs   map[string]string
	Language      string
}

type PersonCredit struct {
	Name     string
	Role     string
	Type     string // Actor / Director / Writer
	ImageURL string
}

type CollectionMeta struct {
	ID          string
	Name        string
	PosterURL   string
	BackdropURL string
}

type MetadataResult struct {
	Name            string
	OriginalTitle   string
	Overview        string
	Tagline         string
	Year            int
	PremiereDate    string
	CommunityRating float64
	OfficialRating  string
	Genres          []string
	Studios         []string
	Tags            []string
	People          []PersonCredit
	ProviderIDs     map[string]string
	PosterURL       string
	BackdropURL     string
	LogoURL         string
	LockData        bool
	Collection      *CollectionMeta
}

type MetadataProvider interface {
	Plugin
	FetchMetadata(ctx context.Context, q MediaQuery) (*MetadataResult, error)
}

type StreamResolveResult struct {
	URL     string
	Headers map[string]string
	Is302   bool
}

type StreamResolver interface {
	Plugin
	ResolveStream(ctx context.Context, rawPath string) (*StreamResolveResult, error)
}

// SubtitleSearchQuery describes parameters for searching remote subtitles.
type SubtitleSearchQuery struct {
	ItemID        string
	ItemType      string
	Title         string
	OriginalTitle string
	Year          int
	SeasonNumber  int
	EpisodeNumber int
	Language      string
	ProviderIDs   map[string]string
	MediaPath     string
}

// RemoteSubtitleInfo represents a subtitle search hit returned by a SubtitleProvider.
type RemoteSubtitleInfo struct {
	ID                string  `json:"Id"`
	Name              string  `json:"Name"`
	ProviderName      string  `json:"ProviderName"`
	Language          string  `json:"Language,omitempty"`
	ThreeLetterISOLang string `json:"ThreeLetterISOLanguageName,omitempty"`
	Format            string  `json:"Format,omitempty"`
	Comment           string  `json:"Comment,omitempty"`
	Author            string  `json:"Author,omitempty"`
	CommunityRating   float64 `json:"CommunityRating,omitempty"`
	DownloadCount     int     `json:"DownloadCount,omitempty"`
	IsHashMatch       bool    `json:"IsHashMatch,omitempty"`
}

// SubtitleDownloadResult holds the downloaded subtitle content and format.
type SubtitleDownloadResult struct {
	Language string
	Format   string // srt / ass / ssa / vtt
	Content  []byte
}

// SubtitleProvider searches and downloads external subtitles from remote services.
type SubtitleProvider interface {
	Plugin
	SearchSubtitles(ctx context.Context, q SubtitleSearchQuery) ([]RemoteSubtitleInfo, error)
	DownloadSubtitle(ctx context.Context, subtitleID string) (*SubtitleDownloadResult, error)
}

// RemoteImageHit represents an artwork candidate returned by an ImageProvider.
type RemoteImageHit struct {
	ProviderName string
	URL          string
	ThumbnailURL string
	Type         string // Primary / Backdrop / Logo / Thumb / Banner
	Language     string
	Width        int
	Height       int
	Rating       float64
}

// ImageProvider fetches additional remote artwork for media items.
type ImageProvider interface {
	Plugin
	FetchImages(ctx context.Context, q MediaQuery) ([]RemoteImageHit, error)
}

// NotificationPayload represents an outgoing notification event delivered to Notifier plugins.
type NotificationPayload struct {
	Event   string         `json:"event"`
	Title   string         `json:"title"`
	Message string         `json:"message"`
	Level   string         `json:"level,omitempty"`
	UserID  string         `json:"user_id,omitempty"`
	Data    map[string]any `json:"data,omitempty"`
}

// NotificationEvent is an alias for NotificationPayload.
type NotificationEvent = NotificationPayload

// Notifier delivers server events to external notification channels.
type Notifier interface {
	Plugin
	Notify(ctx context.Context, payload NotificationPayload) error
}

// DefaultConfigExporter can be implemented by plugins to expose their active sanitized default configuration.
type DefaultConfigExporter interface {
	DefaultConfig() map[string]any
}

// Registry manages built-in and external plugins with priority ordering.
type Registry struct {
	mu      sync.RWMutex
	plugins map[string]Plugin
}

func NewRegistry() *Registry {
	return &Registry{
		plugins: make(map[string]Plugin),
	}
}

func (r *Registry) Register(p Plugin) {
	if p == nil {
		return
	}
	m := p.Manifest()
	id := strings.TrimSpace(m.ID)
	if id == "" {
		return
	}
	if !m.BuiltIn && IsBuiltInID(id) {
		slog.Warn("plugins: refusing external plugin with reserved built-in prefix", "plugin_id", id)
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if existing, ok := r.plugins[id]; ok && existing.Manifest().BuiltIn && !m.BuiltIn {
		slog.Warn("plugins: refusing external plugin attempt to overwrite built-in plugin", "plugin_id", id)
		return
	}
	r.plugins[id] = p
}

// Unregister removes a non-built-in plugin from the registry. Built-in plugins cannot be unregistered.
func (r *Registry) Unregister(id string) bool {
	trimmed := strings.TrimSpace(id)
	if trimmed == "" || IsBuiltInID(trimmed) {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	existing, ok := r.plugins[trimmed]
	if !ok {
		for k, p := range r.plugins {
			if strings.EqualFold(k, trimmed) {
				trimmed = k
				existing = p
				ok = true
				break
			}
		}
	}
	if !ok || existing.Manifest().BuiltIn {
		return false
	}
	delete(r.plugins, trimmed)
	return true
}

func (r *Registry) Get(id string) (Plugin, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if p, ok := r.plugins[id]; ok {
		return p, true
	}
	for k, p := range r.plugins {
		if strings.EqualFold(k, id) {
			return p, true
		}
	}
	return nil, false
}

func (r *Registry) List() []Manifest {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Manifest, 0, len(r.plugins))
	for _, p := range r.plugins {
		out = append(out, p.Manifest())
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Priority != out[j].Priority {
			return out[i].Priority < out[j].Priority
		}
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].ID < out[j].ID
	})
	return out
}

func (r *Registry) MetadataProviders() []MetadataProvider {
	return r.MetadataProvidersWithFilter(nil, nil)
}

// MetadataProvidersWithFilter returns enabled MetadataProviders, optionally filtered by enabledNamesOrIDs
// (when non-nil; an empty non-nil slice disables all online providers unless NFO is included) and ordered by
// orderedNamesOrIDs before falling back to plugin Priority.
func (r *Registry) MetadataProvidersWithFilter(enabledNamesOrIDs []string, orderedNamesOrIDs []string) []MetadataProvider {
	r.mu.RLock()
	defer r.mu.RUnlock()

	hasFilter := enabledNamesOrIDs != nil
	allowMap := make(map[string]bool, len(enabledNamesOrIDs))
	for _, item := range enabledNamesOrIDs {
		norm := normalizeProviderToken(item)
		if norm != "" {
			allowMap[norm] = true
		}
	}
	orderRank := make(map[string]int, len(orderedNamesOrIDs))
	for idx, item := range orderedNamesOrIDs {
		norm := normalizeProviderToken(item)
		if norm != "" {
			if _, exists := orderRank[norm]; !exists {
				orderRank[norm] = idx
			}
		}
	}

	var out []MetadataProvider
	for _, p := range r.plugins {
		mf := p.Manifest()
		if !mf.Enabled {
			continue
		}
		mp, ok := p.(MetadataProvider)
		if !ok {
			continue
		}
		if hasFilter && !matchesProviderMap(mf, allowMap) {
			continue
		}
		out = append(out, mp)
	}
	sort.Slice(out, func(i, j int) bool {
		mi, mj := out[i].Manifest(), out[j].Manifest()
		if len(orderRank) > 0 {
			ri, oki := lookupProviderRank(mi, orderRank)
			rj, okj := lookupProviderRank(mj, orderRank)
			if oki && okj && ri != rj {
				return ri < rj
			}
			if oki != okj {
				return oki
			}
		}
		if mi.Priority != mj.Priority {
			return mi.Priority < mj.Priority
		}
		if mi.Name != mj.Name {
			return mi.Name < mj.Name
		}
		return mi.ID < mj.ID
	})
	return out
}

func normalizeProviderToken(s string) string {
	low := strings.ToLower(strings.TrimSpace(s))
	switch low {
	case "themoviedb", "tmdb", "ambra.builtin.tmdb":
		return "tmdb"
	case "thetvdb", "tvdb", "ambra.builtin.tvdb":
		return "tvdb"
	case "nfo", "localnfo", "ambra.builtin.nfo":
		return "nfo"
	default:
		return low
	}
}

func matchesProviderMap(mf Manifest, m map[string]bool) bool {
	if m[normalizeProviderToken(mf.ID)] || m[normalizeProviderToken(mf.Name)] || m[strings.ToLower(mf.ID)] || m[strings.ToLower(mf.Name)] {
		return true
	}
	return false
}

func lookupProviderRank(mf Manifest, ranks map[string]int) (int, bool) {
	for _, key := range []string{normalizeProviderToken(mf.ID), normalizeProviderToken(mf.Name), strings.ToLower(mf.ID), strings.ToLower(mf.Name)} {
		if r, ok := ranks[key]; ok {
			return r, true
		}
	}
	return 0, false
}

func manifestLess(mi, mj Manifest) bool {
	if mi.Priority != mj.Priority {
		return mi.Priority < mj.Priority
	}
	if mi.Name != mj.Name {
		return mi.Name < mj.Name
	}
	return mi.ID < mj.ID
}

func (r *Registry) StreamResolvers() []StreamResolver {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []StreamResolver
	for _, p := range r.plugins {
		if !p.Manifest().Enabled {
			continue
		}
		if sr, ok := p.(StreamResolver); ok {
			out = append(out, sr)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return manifestLess(out[i].Manifest(), out[j].Manifest())
	})
	return out
}

func (r *Registry) SubtitleProviders() []SubtitleProvider {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []SubtitleProvider
	for _, p := range r.plugins {
		if !p.Manifest().Enabled {
			continue
		}
		if sp, ok := p.(SubtitleProvider); ok {
			out = append(out, sp)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return manifestLess(out[i].Manifest(), out[j].Manifest())
	})
	return out
}

func (r *Registry) ImageProviders() []ImageProvider {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []ImageProvider
	for _, p := range r.plugins {
		if !p.Manifest().Enabled {
			continue
		}
		if ip, ok := p.(ImageProvider); ok {
			out = append(out, ip)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return manifestLess(out[i].Manifest(), out[j].Manifest())
	})
	return out
}

func (r *Registry) Notifiers() []Notifier {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []Notifier
	for _, p := range r.plugins {
		if !p.Manifest().Enabled {
			continue
		}
		if np, ok := p.(Notifier); ok {
			out = append(out, np)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return manifestLess(out[i].Manifest(), out[j].Manifest())
	})
	return out
}
