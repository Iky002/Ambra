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

func (r *Registry) Get(id string) (Plugin, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.plugins[id]
	return p, ok
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
		return out[i].Name < out[j].Name
	})
	return out
}

func (r *Registry) MetadataProviders() []MetadataProvider {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []MetadataProvider
	for _, p := range r.plugins {
		if !p.Manifest().Enabled {
			continue
		}
		if mp, ok := p.(MetadataProvider); ok {
			out = append(out, mp)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Manifest().Priority < out[j].Manifest().Priority
	})
	return out
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
		return out[i].Manifest().Priority < out[j].Manifest().Priority
	})
	return out
}
