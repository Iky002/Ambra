package strm

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"

	"github.com/ambra-media/ambra/internal/pkg/netutil"
	"github.com/ambra-media/ambra/internal/plugins"
)

const (
	maxStrmFileBytes = 64 * 1024
	defaultPriority  = 10
)

var _ plugins.DefaultConfigExporter = (*Resolver)(nil)

// Resolver implements plugins.StreamResolver for .strm files with optional 302 redirect.
type Resolver struct {
	enabled   atomic.Bool
	enable302 atomic.Bool
	priority  atomic.Int64
}

func New(enable302 bool) *Resolver {
	r := &Resolver{}
	r.enabled.Store(true)
	r.enable302.Store(enable302)
	r.priority.Store(defaultPriority)
	return r
}

func (r *Resolver) Manifest() plugins.Manifest {
	return plugins.Manifest{
		ID:          "ambra.builtin.strm",
		Name:        "STRM 302 Stream Resolver",
		Version:     "1.0.0",
		Description: "Resolves .strm files into direct HTTP/HTTPS URLs with native 302 redirect support",
		Author:      "Ambra",
		BuiltIn:     true,
		Enabled:     r.enabled.Load(),
		Priority:    int(r.priority.Load()),
		Kinds:       []plugins.Kind{plugins.KindStream},
	}
}

func (r *Resolver) DefaultConfig() map[string]any {
	return map[string]any{
		"enabled":    r.enabled.Load(),
		"enable_302": r.enable302.Load(),
		"priority":   int(r.priority.Load()),
	}
}

func (r *Resolver) Configure(cfg map[string]any) error {
	if cfg == nil {
		return nil
	}
	if v, ok := cfg["enabled"].(bool); ok {
		r.enabled.Store(v)
	} else if v, ok := cfg["Enabled"].(bool); ok {
		r.enabled.Store(v)
	}
	if v, ok := cfg["enable_302"].(bool); ok {
		r.enable302.Store(v)
	} else if v, ok := cfg["Enable302"].(bool); ok {
		r.enable302.Store(v)
	}
	for _, k := range []string{"priority", "Priority"} {
		if raw, exists := cfg[k]; exists {
			switch n := raw.(type) {
			case int:
				r.priority.Store(int64(n))
			case int64:
				r.priority.Store(n)
			case float64:
				r.priority.Store(int64(n))
			}
			break
		}
	}
	return nil
}

func resolveStrmSymlinkWithinDir(strmPath string) (string, bool) {
	baseDir := filepath.Dir(filepath.Clean(strmPath))
	realBase, err := filepath.EvalSymlinks(baseDir)
	if err != nil {
		realBase, err = filepath.Abs(baseDir)
		if err != nil {
			return "", false
		}
	}
	realTarget, err := filepath.EvalSymlinks(filepath.Clean(strmPath))
	if err != nil {
		return "", false
	}
	rel, err := filepath.Rel(realBase, realTarget)
	if err != nil {
		return "", false
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", false
	}
	return realTarget, true
}

func (r *Resolver) ResolveStream(_ context.Context, rawPath string) (*plugins.StreamResolveResult, error) {
	if !r.enabled.Load() {
		return nil, nil
	}
	if !strings.EqualFold(filepath.Ext(rawPath), ".strm") {
		return nil, nil
	}
	fi, err := os.Lstat(rawPath)
	if err != nil {
		return nil, err
	}
	if fi.IsDir() {
		return nil, fmt.Errorf("strm: path %q is a directory", rawPath)
	}
	openPath := rawPath
	if fi.Mode()&os.ModeSymlink != 0 {
		resolved, ok := resolveStrmSymlinkWithinDir(rawPath)
		if !ok {
			return nil, fmt.Errorf("strm: symlink %q escapes media directory", rawPath)
		}
		targetInfo, statErr := os.Stat(resolved)
		if statErr != nil {
			return nil, statErr
		}
		if !targetInfo.Mode().IsRegular() {
			return nil, fmt.Errorf("strm: symlink target %q is not a regular file", resolved)
		}
		openPath = resolved
	} else if !fi.Mode().IsRegular() {
		return nil, fmt.Errorf("strm: path %q is not a regular file", rawPath)
	}
	f, err := os.Open(openPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var target string
	sc := bufio.NewScanner(io.LimitReader(f, maxStrmFileBytes))
	for sc.Scan() {
		line := strings.TrimSpace(strings.TrimPrefix(sc.Text(), "\xef\xbb\xbf"))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		target = line
		break
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if target == "" {
		return nil, nil
	}

	// Strictly enforce http/https remote URL validation; never allow local file paths or unsafe protocols.
	if err := netutil.ValidateRemoteURL(target, true); err != nil {
		return nil, fmt.Errorf("strm: unsafe target url: %w", err)
	}

	return &plugins.StreamResolveResult{
		URL:   target,
		Is302: r.enable302.Load(),
	}, nil
}
