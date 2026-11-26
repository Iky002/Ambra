package plugins

import (
	"context"
	"testing"
)

type stubSubtitlePlugin struct {
	stubPlugin
}

func (s *stubSubtitlePlugin) SearchSubtitles(_ context.Context, q SubtitleSearchQuery) ([]RemoteSubtitleInfo, error) {
	return []RemoteSubtitleInfo{{ID: "sub-1", Name: q.Title, ProviderName: s.manifest.Name}}, nil
}

func (s *stubSubtitlePlugin) DownloadSubtitle(_ context.Context, subtitleID string) (*SubtitleDownloadResult, error) {
	return &SubtitleDownloadResult{Language: "zh", Format: "srt", Content: []byte(subtitleID)}, nil
}

type stubImagePlugin struct {
	stubPlugin
}

func (s *stubImagePlugin) FetchImages(_ context.Context, _ MediaQuery) ([]RemoteImageHit, error) {
	return []RemoteImageHit{{ProviderName: s.manifest.Name, Type: "Primary"}}, nil
}

type stubNotifierPlugin struct {
	stubPlugin
}

func (s *stubNotifierPlugin) Notify(_ context.Context, _ NotificationPayload) error {
	return nil
}

func TestRegistryV0110Coverage(t *testing.T) {
	reg := NewRegistry()

	// Register nil or blank ID should be no-ops.
	reg.Register(nil)
	reg.Register(&stubPlugin{Manifest{ID: "   ", Name: "Blank"}})
	if len(reg.List()) != 0 {
		t.Fatalf("expected empty registry after nil/blank registration, got %d", len(reg.List()))
	}

	// Register built-in and external plugins for Get & Unregister tests.
	builtin := &stubMetadataPlugin{stubPlugin{Manifest{
		ID: "ambra.builtin.tmdb", Name: "TheMovieDb", BuiltIn: true, Enabled: true, Priority: 10, Kinds: []Kind{KindMetadata},
	}}}
	tvdb := &stubMetadataPlugin{stubPlugin{Manifest{
		ID: "ambra.builtin.tvdb", Name: "TheTVDB", BuiltIn: true, Enabled: true, Priority: 20, Kinds: []Kind{KindMetadata},
	}}}
	nfo := &stubMetadataPlugin{stubPlugin{Manifest{
		ID: "ambra.builtin.nfo", Name: "LocalNFO", BuiltIn: true, Enabled: true, Priority: 5, Kinds: []Kind{KindMetadata},
	}}}
	customBuiltIn := &stubPlugin{Manifest{
		ID: "core.internal", Name: "Core Internal", BuiltIn: true, Enabled: true, Priority: 1,
	}}
	extOne := &stubMetadataPlugin{stubPlugin{Manifest{
		ID: "Ext.MetaOne", Name: "Alpha Custom", BuiltIn: false, Enabled: true, Priority: 30, Kinds: []Kind{KindMetadata},
	}}}
	extTwo := &stubMetadataPlugin{stubPlugin{Manifest{
		ID: "Ext.MetaTwo", Name: "Beta Custom", BuiltIn: false, Enabled: true, Priority: 30, Kinds: []Kind{KindMetadata},
	}}}
	extDisabled := &stubMetadataPlugin{stubPlugin{Manifest{
		ID: "Ext.MetaOff", Name: "Off Custom", BuiltIn: false, Enabled: false, Priority: 1, Kinds: []Kind{KindMetadata},
	}}}

	for _, p := range []Plugin{builtin, tvdb, nfo, customBuiltIn, extOne, extTwo, extDisabled} {
		reg.Register(p)
	}

	// Case-insensitive Get and missing Get.
	if got, ok := reg.Get("ext.metaone"); !ok || got != extOne {
		t.Fatalf("expected case-insensitive Get to find Ext.MetaOne, got %v, ok=%v", got, ok)
	}
	if _, ok := reg.Get("does.not.exist"); ok {
		t.Fatal("expected Get(does.not.exist) to return false")
	}

	// Unregister guards: empty, built-in prefix, non-existent, custom built-in, and valid case-insensitive external.
	if reg.Unregister("   ") {
		t.Fatal("expected Unregister(blank) to return false")
	}
	if reg.Unregister("ambra.builtin.tmdb") {
		t.Fatal("expected Unregister(built-in prefix) to return false")
	}
	if reg.Unregister("missing.plugin") {
		t.Fatal("expected Unregister(missing) to return false")
	}
	if reg.Unregister("CORE.INTERNAL") {
		t.Fatal("expected Unregister(BuiltIn=true) to return false")
	}

	// MetadataProvidersWithFilter: explicit filter + explicit order + duplicate/empty tokens.
	filtered := reg.MetadataProvidersWithFilter(
		[]string{"tvdb", "themoviedb", "   "},
		[]string{"thetvdb", "tmdb", "thetvdb", ""},
	)
	if len(filtered) != 2 {
		t.Fatalf("expected 2 filtered metadata providers, got %d", len(filtered))
	}
	if filtered[0].Manifest().ID != "ambra.builtin.tvdb" || filtered[1].Manifest().ID != "ambra.builtin.tmdb" {
		t.Fatalf("unexpected ordered providers: %s, %s", filtered[0].Manifest().ID, filtered[1].Manifest().ID)
	}

	// MetadataProvidersWithFilter: nil filter (all enabled), partial order (ranked before unranked, tie-break by Priority then Name).
	orderedAll := reg.MetadataProvidersWithFilter(nil, []string{"beta custom", "nfo"})
	if len(orderedAll) != 5 {
		t.Fatalf("expected 5 enabled metadata providers, got %d", len(orderedAll))
	}
	wantOrder := []string{"Ext.MetaTwo", "ambra.builtin.nfo", "ambra.builtin.tmdb", "ambra.builtin.tvdb", "Ext.MetaOne"}
	for i, want := range wantOrder {
		if orderedAll[i].Manifest().ID != want {
			t.Fatalf("orderedAll[%d] = %s, want %s", i, orderedAll[i].Manifest().ID, want)
		}
	}

	// Tie-break by Name when Priority is equal and neither is in orderRank.
	reg.Register(&stubMetadataPlugin{stubPlugin{Manifest{
		ID: "Ext.MetaZero", Name: "AAA Custom", BuiltIn: false, Enabled: true, Priority: 30, Kinds: []Kind{KindMetadata},
	}}})
	tieBroken := reg.MetadataProvidersWithFilter([]string{"Ext.MetaOne", "Ext.MetaZero"}, nil)
	if len(tieBroken) != 2 || tieBroken[0].Manifest().ID != "Ext.MetaZero" || tieBroken[1].Manifest().ID != "Ext.MetaOne" {
		t.Fatalf("unexpected Name tie-break order: %+v", tieBroken)
	}

	// Empty non-nil filter disables all providers.
	if empty := reg.MetadataProvidersWithFilter([]string{}, nil); len(empty) != 0 {
		t.Fatalf("expected 0 providers for empty non-nil filter, got %d", len(empty))
	}

	// Case-insensitive Unregister of external plugin and exact-match Unregister.
	if !reg.Unregister("ext.metaone") {
		t.Fatal("expected case-insensitive Unregister(ext.metaone) to succeed")
	}
	if _, ok := reg.Get("Ext.MetaOne"); ok {
		t.Fatal("expected Ext.MetaOne to be removed")
	}
	if !reg.Unregister("Ext.MetaTwo") {
		t.Fatal("expected exact Unregister(Ext.MetaTwo) to succeed")
	}

	// SubtitleProviders, ImageProviders, Notifiers sorting & enabled filtering.
	sub1 := &stubSubtitlePlugin{stubPlugin{Manifest{ID: "sub.b", Name: "Sub B", Enabled: true, Priority: 20, Kinds: []Kind{KindSubtitle}}}}
	sub2 := &stubSubtitlePlugin{stubPlugin{Manifest{ID: "sub.a", Name: "Sub A", Enabled: true, Priority: 10, Kinds: []Kind{KindSubtitle}}}}
	subOff := &stubSubtitlePlugin{stubPlugin{Manifest{ID: "sub.off", Name: "Sub Off", Enabled: false, Priority: 1, Kinds: []Kind{KindSubtitle}}}}

	img1 := &stubImagePlugin{stubPlugin{Manifest{ID: "img.b", Name: "Img B", Enabled: true, Priority: 25, Kinds: []Kind{KindImage}}}}
	img2 := &stubImagePlugin{stubPlugin{Manifest{ID: "img.a", Name: "Img A", Enabled: true, Priority: 5, Kinds: []Kind{KindImage}}}}
	imgOff := &stubImagePlugin{stubPlugin{Manifest{ID: "img.off", Name: "Img Off", Enabled: false, Priority: 1, Kinds: []Kind{KindImage}}}}

	ntf1 := &stubNotifierPlugin{stubPlugin{Manifest{ID: "ntf.b", Name: "Ntf B", Enabled: true, Priority: 30, Kinds: []Kind{KindNotifier}}}}
	ntf2 := &stubNotifierPlugin{stubPlugin{Manifest{ID: "ntf.a", Name: "Ntf A", Enabled: true, Priority: 15, Kinds: []Kind{KindNotifier}}}}
	ntfOff := &stubNotifierPlugin{stubPlugin{Manifest{ID: "ntf.off", Name: "Ntf Off", Enabled: false, Priority: 1, Kinds: []Kind{KindNotifier}}}}

	for _, p := range []Plugin{sub1, sub2, subOff, img1, img2, imgOff, ntf1, ntf2, ntfOff} {
		reg.Register(p)
	}

	subs := reg.SubtitleProviders()
	if len(subs) != 2 || subs[0].Manifest().ID != "sub.a" || subs[1].Manifest().ID != "sub.b" {
		t.Fatalf("unexpected SubtitleProviders: %+v", subs)
	}
	if hits, err := subs[0].SearchSubtitles(context.Background(), SubtitleSearchQuery{Title: "Test"}); err != nil || len(hits) != 1 {
		t.Fatalf("SearchSubtitles failed: %v, %+v", err, hits)
	}
	if dl, err := subs[0].DownloadSubtitle(context.Background(), "s1"); err != nil || string(dl.Content) != "s1" {
		t.Fatalf("DownloadSubtitle failed: %v, %+v", err, dl)
	}

	imgs := reg.ImageProviders()
	if len(imgs) != 2 || imgs[0].Manifest().ID != "img.a" || imgs[1].Manifest().ID != "img.b" {
		t.Fatalf("unexpected ImageProviders: %+v", imgs)
	}
	if ih, err := imgs[0].FetchImages(context.Background(), MediaQuery{Title: "Test"}); err != nil || len(ih) != 1 {
		t.Fatalf("FetchImages failed: %v, %+v", err, ih)
	}

	ntfs := reg.Notifiers()
	if len(ntfs) != 2 || ntfs[0].Manifest().ID != "ntf.a" || ntfs[1].Manifest().ID != "ntf.b" {
		t.Fatalf("unexpected Notifiers: %+v", ntfs)
	}
	if err := ntfs[0].Notify(context.Background(), NotificationPayload{Event: "test"}); err != nil {
		t.Fatalf("Notify failed: %v", err)
	}
}
