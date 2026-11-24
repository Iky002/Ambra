package nfo

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/ambra-media/ambra/internal/plugins"
)

func TestProviderManifestAndConfigure(t *testing.T) {
	p := New()
	m := p.Manifest()
	if m.ID != "ambra.builtin.nfo" || !m.Enabled || !m.BuiltIn {
		t.Fatalf("unexpected manifest: %+v", m)
	}
	if err := p.Configure(map[string]any{"enabled": false}); err != nil {
		t.Fatalf("Configure failed: %v", err)
	}
	if p.Manifest().Enabled {
		t.Fatalf("expected Enabled=false after Configure")
	}
	if err := p.Configure(map[string]any{"enabled": true}); err != nil {
		t.Fatalf("Configure failed: %v", err)
	}
	if !p.Manifest().Enabled {
		t.Fatalf("expected Enabled=true after Configure")
	}
}

func TestFetchMetadataBranches(t *testing.T) {
	p := New()
	ctx := context.Background()

	// Empty path
	if res, err := p.FetchMetadata(ctx, plugins.MediaQuery{}); err != nil || res != nil {
		t.Fatalf("expected nil,nil for empty path; got res=%v err=%v", res, err)
	}

	dir := t.TempDir()

	// Series pointing to a file instead of a dir -> looks in parent dir for tvshow.nfo
	fileInDir := filepath.Join(dir, "S01E01.mkv")
	_ = os.WriteFile(fileInDir, []byte("x"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "tvshow.nfo"), []byte(`<tvshow><title>ShowFromFile</title></tvshow>`), 0o644)
	res, err := p.FetchMetadata(ctx, plugins.MediaQuery{ItemType: "series", Path: fileInDir})
	if err != nil || res == nil || res.Name != "ShowFromFile" {
		t.Fatalf("expected ShowFromFile, got res=%+v err=%v", res, err)
	}

	// Season dir with season.nfo
	seasonDir := filepath.Join(dir, "Season 01")
	_ = os.MkdirAll(seasonDir, 0o755)
	_ = os.WriteFile(filepath.Join(seasonDir, "season.nfo"), []byte(`<season><title>Season 1</title></season>`), 0o644)
	res, err = p.FetchMetadata(ctx, plugins.MediaQuery{ItemType: "Season", Path: seasonDir})
	if err != nil || res == nil || res.Name != "Season 1" {
		t.Fatalf("expected Season 1, got res=%+v err=%v", res, err)
	}

	// Movie dir with movie.nfo
	movieDir := filepath.Join(dir, "MovieDir")
	_ = os.MkdirAll(movieDir, 0o755)
	_ = os.WriteFile(filepath.Join(movieDir, "movie.nfo"), []byte(`<movie><title>DirMovie</title></movie>`), 0o644)
	res, err = p.FetchMetadata(ctx, plugins.MediaQuery{ItemType: "Movie", Path: movieDir})
	if err != nil || res == nil || res.Name != "DirMovie" {
		t.Fatalf("expected DirMovie, got res=%+v err=%v", res, err)
	}

	// Movie file where <file>.nfo is invalid XML, fallback to movie.nfo
	movieFile := filepath.Join(movieDir, "film.mkv")
	_ = os.WriteFile(movieFile, []byte("x"), 0o644)
	_ = os.WriteFile(filepath.Join(movieDir, "film.nfo"), []byte(`<movie><broken`), 0o644)
	res, err = p.FetchMetadata(ctx, plugins.MediaQuery{ItemType: "Movie", Path: movieFile})
	if err != nil || res == nil || res.Name != "DirMovie" {
		t.Fatalf("expected fallback to movie.nfo, got res=%+v err=%v", res, err)
	}

	// No NFO exists at all
	emptyDir := filepath.Join(dir, "Empty")
	_ = os.MkdirAll(emptyDir, 0o755)
	res, err = p.FetchMetadata(ctx, plugins.MediaQuery{ItemType: "Movie", Path: emptyDir})
	if err != nil || res != nil {
		t.Fatalf("expected nil,nil when no NFO found; got res=%+v err=%v", res, err)
	}

	// Episode with dir path -> no candidates
	res, err = p.FetchMetadata(ctx, plugins.MediaQuery{ItemType: "Episode", Path: emptyDir})
	if err != nil || res != nil {
		t.Fatalf("expected nil,nil for episode dir; got res=%+v err=%v", res, err)
	}
}

func TestParseNFOExtraBranches(t *testing.T) {
	// Invalid XML
	if _, err := ParseNFO([]byte(`<movie><unclosed>`)); err == nil {
		t.Fatalf("expected XML error")
	}

	// UTF-8 BOM + outline fallback + releasedate year + ratings default + tags/writers/credits/tvdbid/id/uniqueid
	bomXML := "\xef\xbb\xbf" + `<movie>
		<title>  Test Film  </title>
		<outline>  Outline overview  </outline>
		<releasedate>2019-05-01</releasedate>
		<ratings>
			<rating name="imdb"><value>7.1</value></rating>
			<rating name="tmdb" default="true"><value>8.5</value></rating>
		</ratings>
		<tag>TagA</tag>
		<tag>   </tag>
		<writer>WriterOne</writer>
		<credits>CreditTwo</credits>
		<tvdbid>9999</tvdbid>
		<id>tt0001234</id>
		<uniqueid type="douban">123456</uniqueid>
	</movie>`
	res, err := ParseNFO([]byte(bomXML))
	if err != nil {
		t.Fatalf("ParseNFO failed: %v", err)
	}
	if res.Name != "Test Film" || res.Overview != "Outline overview" || res.Year != 2019 {
		t.Fatalf("unexpected basic fields: %+v", res)
	}
	if res.CommunityRating != 8.5 {
		t.Fatalf("expected default rating 8.5, got %v", res.CommunityRating)
	}
	if len(res.Tags) != 1 || res.Tags[0] != "TagA" {
		t.Fatalf("unexpected tags: %v", res.Tags)
	}
	if len(res.People) != 2 || res.People[0].Type != "Writer" || res.People[1].Name != "CreditTwo" {
		t.Fatalf("unexpected people: %+v", res.People)
	}
	if res.ProviderIDs["tvdb"] != "9999" || res.ProviderIDs["imdb"] != "tt0001234" || res.ProviderIDs["douban"] != "123456" {
		t.Fatalf("unexpected provider IDs: %+v", res.ProviderIDs)
	}

	// Ratings without default -> fallback to first rating
	noDefaultXML := `<movie>
		<title>FallbackRating</title>
		<ratings>
			<rating name="imdb"><value>6.7</value></rating>
		</ratings>
	</movie>`
	res2, err := ParseNFO([]byte(noDefaultXML))
	if err != nil || res2.CommunityRating != 6.7 {
		t.Fatalf("expected fallback rating 6.7, got res=%+v err=%v", res2, err)
	}
}
