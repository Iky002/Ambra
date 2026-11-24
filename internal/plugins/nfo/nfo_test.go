package nfo

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/ambra-media/ambra/internal/plugins"
)

func TestParseNFOMovie(t *testing.T) {
	xmlData := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<movie>
  <title>流浪地球2</title>
  <originaltitle>The Wandering Earth II</originaltitle>
  <plot>太阳即将毁灭，人类在地球表面建造出巨大的推进器。</plot>
  <tagline>再见太阳系</tagline>
  <year>2023</year>
  <premiered>2023-01-22</premiered>
  <rating>8.3</rating>
  <mpaa>PG-13</mpaa>
  <genre>科幻</genre>
  <genre>冒险</genre>
  <studio>中国电影股份有限公司</studio>
  <director>郭帆</director>
  <actor>
    <name>吴京</name>
    <role>刘培强</role>
  </actor>
  <actor>
    <name>刘德华</name>
    <role>图恒宇</role>
  </actor>
  <tmdbid>842675</tmdbid>
  <imdbid>tt13539646</imdbid>
</movie>`

	res, err := ParseNFO([]byte(xmlData))
	if err != nil {
		t.Fatalf("ParseNFO failed: %v", err)
	}
	if res.Name != "流浪地球2" || res.OriginalTitle != "The Wandering Earth II" {
		t.Fatalf("unexpected titles: %+v", res)
	}
	if res.Year != 2023 || res.CommunityRating != 8.3 || res.OfficialRating != "PG-13" {
		t.Fatalf("unexpected metadata: year=%d rating=%v mpaa=%s", res.Year, res.CommunityRating, res.OfficialRating)
	}
	if res.ProviderIDs["tmdb"] != "842675" || res.ProviderIDs["imdb"] != "tt13539646" {
		t.Fatalf("unexpected provider IDs: %+v", res.ProviderIDs)
	}
	if len(res.Genres) != 2 || len(res.People) != 3 {
		t.Fatalf("unexpected genres/people: genres=%v people=%v", res.Genres, res.People)
	}
}

func TestParseNFOTVShowAndEpisode(t *testing.T) {
	dir := t.TempDir()
	tvNFO := `<tvshow><title>三体</title><year>2023</year><uniqueid type="tmdb" default="true">203419</uniqueid></tvshow>`
	if err := os.WriteFile(filepath.Join(dir, "tvshow.nfo"), []byte(tvNFO), 0o644); err != nil {
		t.Fatal(err)
	}

	p := New()
	res, err := p.FetchMetadata(context.Background(), plugins.MediaQuery{
		ItemType: "Series",
		Path:     dir,
	})
	if err != nil || res == nil {
		t.Fatalf("FetchMetadata series failed: res=%v err=%v", res, err)
	}
	if res.Name != "三体" || res.ProviderIDs["tmdb"] != "203419" {
		t.Fatalf("unexpected tvshow result: %+v", res)
	}

	epPath := filepath.Join(dir, "S01E01.mkv")
	epNFO := `<episodedetails><title>第一集：科学边界</title><aired>2023-01-15</aired><plot>纳米材料专家汪淼受邀调查。</plot></episodedetails>`
	if err := os.WriteFile(filepath.Join(dir, "S01E01.nfo"), []byte(epNFO), 0o644); err != nil {
		t.Fatal(err)
	}
	epRes, err := p.FetchMetadata(context.Background(), plugins.MediaQuery{
		ItemType: "Episode",
		Path:     epPath,
	})
	if err != nil || epRes == nil {
		t.Fatalf("FetchMetadata episode failed: res=%v err=%v", epRes, err)
	}
	if epRes.Name != "第一集：科学边界" || epRes.Year != 2023 {
		t.Fatalf("unexpected episode result: %+v", epRes)
	}
}
