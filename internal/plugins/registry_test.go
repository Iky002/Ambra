package plugins

import (
	"context"
	"testing"
)

type stubPlugin struct {
	manifest Manifest
}

func (s *stubPlugin) Manifest() Manifest               { return s.manifest }
func (s *stubPlugin) Configure(_ map[string]any) error { return nil }

type stubMetadataPlugin struct {
	stubPlugin
}

func (s *stubMetadataPlugin) FetchMetadata(_ context.Context, _ MediaQuery) (*MetadataResult, error) {
	return &MetadataResult{Name: s.manifest.Name}, nil
}

type stubStreamPlugin struct {
	stubPlugin
}

func (s *stubStreamPlugin) ResolveStream(_ context.Context, rawPath string) (*StreamResolveResult, error) {
	return &StreamResolveResult{URL: rawPath, Is302: true}, nil
}

func TestRegistry(t *testing.T) {
	reg := NewRegistry()

	if len(reg.List()) != 0 {
		t.Fatal("expected empty registry")
	}
	if len(reg.MetadataProviders()) != 0 {
		t.Fatal("expected no metadata providers")
	}
	if len(reg.StreamResolvers()) != 0 {
		t.Fatal("expected no stream resolvers")
	}

	m1 := &stubMetadataPlugin{stubPlugin{Manifest{
		ID: "meta-b", Name: "Meta B", Priority: 20, Enabled: true, Kinds: []Kind{KindMetadata},
	}}}
	m2 := &stubMetadataPlugin{stubPlugin{Manifest{
		ID: "meta-a", Name: "Meta A", Priority: 10, Enabled: true, Kinds: []Kind{KindMetadata},
	}}}
	mDisabled := &stubMetadataPlugin{stubPlugin{Manifest{
		ID: "meta-off", Name: "Meta Off", Priority: 5, Enabled: false, Kinds: []Kind{KindMetadata},
	}}}
	s1 := &stubStreamPlugin{stubPlugin{Manifest{
		ID: "stream-2", Name: "Stream B", Priority: 20, Enabled: true, Kinds: []Kind{KindStream},
	}}}
	s2 := &stubStreamPlugin{stubPlugin{Manifest{
		ID: "stream-1", Name: "Stream A", Priority: 10, Enabled: true, Kinds: []Kind{KindStream},
	}}}
	sDisabled := &stubStreamPlugin{stubPlugin{Manifest{
		ID: "stream-off", Name: "Stream Off", Priority: 1, Enabled: false, Kinds: []Kind{KindStream},
	}}}
	plain := &stubPlugin{Manifest{
		ID: "plain", Name: "Alpha Plain", Priority: 20, Enabled: true, Kinds: []Kind{KindNotifier},
	}}

	for _, p := range []Plugin{m1, m2, mDisabled, s1, s2, sDisabled, plain} {
		reg.Register(p)
	}

	// List sorts by Priority asc, then Name asc.
	list := reg.List()
	if len(list) != 7 {
		t.Fatalf("expected 7 manifests, got %d", len(list))
	}
	wantIDs := []string{"stream-off", "meta-off", "meta-a", "stream-1", "plain", "meta-b", "stream-2"}
	for i, want := range wantIDs {
		if list[i].ID != want {
			t.Errorf("List()[%d].ID = %q, want %q", i, list[i].ID, want)
		}
	}

	// MetadataProviders returns only enabled MetadataProvider sorted by Priority.
	mps := reg.MetadataProviders()
	if len(mps) != 2 {
		t.Fatalf("expected 2 metadata providers, got %d", len(mps))
	}
	if mps[0].Manifest().ID != "meta-a" || mps[1].Manifest().ID != "meta-b" {
		t.Fatalf("unexpected metadata provider order: %s, %s", mps[0].Manifest().ID, mps[1].Manifest().ID)
	}

	// StreamResolvers returns only enabled StreamResolver sorted by Priority.
	srs := reg.StreamResolvers()
	if len(srs) != 2 {
		t.Fatalf("expected 2 stream resolvers, got %d", len(srs))
	}
	if srs[0].Manifest().ID != "stream-1" || srs[1].Manifest().ID != "stream-2" {
		t.Fatalf("unexpected stream resolver order: %s, %s", srs[0].Manifest().ID, srs[1].Manifest().ID)
	}
}

func TestRegistryBuiltInProtection(t *testing.T) {
	reg := NewRegistry()

	// External plugin trying to claim ambra.builtin.* prefix must be rejected.
	spoof := &stubPlugin{Manifest{
		ID: "ambra.builtin.evil", Name: "Evil", BuiltIn: false, Enabled: true,
	}}
	reg.Register(spoof)
	if _, ok := reg.Get("ambra.builtin.evil"); ok {
		t.Fatal("expected external plugin with ambra.builtin. prefix to be rejected")
	}

	// Legitimate built-in plugin registers fine.
	builtin := &stubPlugin{Manifest{
		ID: "ambra.builtin.tmdb", Name: "TMDB", BuiltIn: true, Enabled: true,
	}}
	reg.Register(builtin)
	if got, ok := reg.Get("ambra.builtin.tmdb"); !ok || got != builtin {
		t.Fatal("expected built-in plugin to register")
	}

	// External plugin with same ID cannot overwrite built-in plugin (even with non-prefixed built-in ID).
	customBuiltin := &stubPlugin{Manifest{
		ID: "core.custom", Name: "Core", BuiltIn: true, Enabled: true,
	}}
	reg.Register(customBuiltin)
	overwrite := &stubPlugin{Manifest{
		ID: "core.custom", Name: "Hijack", BuiltIn: false, Enabled: true,
	}}
	reg.Register(overwrite)
	if got, ok := reg.Get("core.custom"); !ok || got.Manifest().Name != "Core" {
		t.Fatalf("expected built-in plugin to remain intact, got %+v", got)
	}
}
