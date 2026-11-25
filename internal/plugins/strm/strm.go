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

const maxStrmFileBytes = 64 * 1024

// Resolver implements plugins.StreamResolver for .strm files with optional 302 redirect.
type Resolver struct {
	enabled   atomic.Bool
	enable302 atomic.Bool
}

func New(enable302 bool) *Resolver {
	r := &Resolver{}
	r.enabled.Store(true)
	r.enable302.Store(enable302)
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
		Priority:    10,
		Kinds:       []plugins.Kind{plugins.KindStream},
	}
}

func (r *Resolver) Configure(cfg map[string]any) error {
	if v, ok := cfg["enabled"].(bool); ok {
		r.enabled.Store(v)
	}
	if v, ok := cfg["enable_302"].(bool); ok {
		r.enable302.Store(v)
	}
	return nil
}

func (r *Resolver) ResolveStream(_ context.Context, rawPath string) (*plugins.StreamResolveResult, error) {
	if !r.enabled.Load() {
		return nil, nil
	}
	if !strings.EqualFold(filepath.Ext(rawPath), ".strm") {
		return nil, nil
	}
	f, err := os.Open(rawPath)
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
