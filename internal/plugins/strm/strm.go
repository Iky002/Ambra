package strm

import (
	"bufio"
	"context"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/ambra-media/ambra/internal/plugins"
)

// Resolver implements plugins.StreamResolver for .strm files with optional 302 redirect.
type Resolver struct {
	enabled   bool
	enable302 bool
}

func New(enable302 bool) *Resolver {
	return &Resolver{
		enabled:   true,
		enable302: enable302,
	}
}

func (r *Resolver) Manifest() plugins.Manifest {
	return plugins.Manifest{
		ID:          "ambra.builtin.strm",
		Name:        "STRM 302 Stream Resolver",
		Version:     "1.0.0",
		Description: "Resolves .strm files into direct HTTP/HTTPS URLs with native 302 redirect support",
		Author:      "Ambra",
		BuiltIn:     true,
		Enabled:     r.enabled,
		Priority:    10,
		Kinds:       []plugins.Kind{plugins.KindStream},
	}
}

func (r *Resolver) Configure(cfg map[string]any) error {
	if v, ok := cfg["enabled"].(bool); ok {
		r.enabled = v
	}
	if v, ok := cfg["enable_302"].(bool); ok {
		r.enable302 = v
	}
	return nil
}

func (r *Resolver) ResolveStream(_ context.Context, rawPath string) (*plugins.StreamResolveResult, error) {
	if !strings.EqualFold(filepath.Ext(rawPath), ".strm") {
		return nil, nil
	}
	f, err := os.Open(rawPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var target string
	sc := bufio.NewScanner(f)
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

	isRemote := false
	if u, err := url.Parse(target); err == nil && (strings.EqualFold(u.Scheme, "http") || strings.EqualFold(u.Scheme, "https")) {
		isRemote = true
	}

	return &plugins.StreamResolveResult{
		URL:   target,
		Is302: r.enable302 && isRemote,
	}, nil
}
