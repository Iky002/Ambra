package ext

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/ambra-media/ambra/internal/plugins"
)

// ExternalManifest is the JSON schema loaded from data/plugins/*/manifest.json.
type ExternalManifest struct {
	ID          string         `json:"Id"`
	Name        string         `json:"Name"`
	Version     string         `json:"Version"`
	Description string         `json:"Description"`
	Author      string         `json:"Author,omitempty"`
	Endpoint    string         `json:"Endpoint"`
	APIKey      string         `json:"ApiKey,omitempty"`
	TimeoutMs   int            `json:"TimeoutMs,omitempty"`
	Priority    int            `json:"Priority"`
	Enabled     *bool          `json:"Enabled,omitempty"`
	Kinds       []plugins.Kind `json:"Kinds"`
}

// Loader discovers external HTTP plugins from manifest.json files on disk.
type Loader struct {
	mu     sync.RWMutex
	loaded map[string]*HTTPPlugin
}

// NewLoader constructs an external plugin Loader.
func NewLoader() *Loader {
	return &Loader{
		loaded: make(map[string]*HTTPPlugin),
	}
}

// LoadFromDir scans <dir>/*/manifest.json, instantiates HTTPPlugin instances,
// and registers them into reg. Missing root dir is treated as 0 loaded with nil error.
func (l *Loader) LoadFromDir(dir string, reg *plugins.Registry) (int, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("read plugins dir %q: %w", dir, err)
	}

	var (
		count int
		errs  []error
	)

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		manifestPath := filepath.Join(dir, entry.Name(), "manifest.json")
		raw, err := os.ReadFile(manifestPath)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			errs = append(errs, fmt.Errorf("read %s: %w", manifestPath, err))
			continue
		}

		var m ExternalManifest
		if err := json.Unmarshal(raw, &m); err != nil {
			errs = append(errs, fmt.Errorf("parse %s: %w", manifestPath, err))
			continue
		}

		p, err := NewHTTPPlugin(m)
		if err != nil {
			errs = append(errs, fmt.Errorf("init plugin %s: %w", manifestPath, err))
			continue
		}
		if reg != nil {
			if existing, ok := reg.Get(p.Manifest().ID); ok && existing.Manifest().BuiltIn {
				errs = append(errs, fmt.Errorf("init plugin %s: cannot overwrite built-in plugin %q", manifestPath, p.Manifest().ID))
				continue
			}
		}

		l.mu.Lock()
		l.loaded[p.Manifest().ID] = p
		l.mu.Unlock()

		if reg != nil {
			reg.Register(p)
		}
		count++
	}

	return count, errors.Join(errs...)
}

// Loaded returns a snapshot of all external HTTP plugins discovered by this Loader.
func (l *Loader) Loaded() []*HTTPPlugin {
	l.mu.RLock()
	defer l.mu.RUnlock()
	out := make([]*HTTPPlugin, 0, len(l.loaded))
	for _, p := range l.loaded {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool {
		mi, mj := out[i].Manifest(), out[j].Manifest()
		if mi.Priority != mj.Priority {
			return mi.Priority < mj.Priority
		}
		return strings.ToLower(mi.Name) < strings.ToLower(mj.Name)
	})
	return out
}
