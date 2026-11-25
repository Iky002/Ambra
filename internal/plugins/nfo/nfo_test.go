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

func TestParseNFOLockDataAndSet(t *testing.T) {
	xmlStructuredSet := `<?xml version="1.0" encoding="UTF-8"?>
<movie>
  <title>哈利·波特与魔法石</title>
  <year>2001</year>
  <lockdata>true</lockdata>
  <set>
    <name>哈利·波特系列</name>
    <overview>魔法世界合集</overview>
  </set>
</movie>`
	res, err := ParseNFO([]byte(xmlStructuredSet))
	if err != nil {
		t.Fatalf("ParseNFO structured set failed: %v", err)
	}
	if !res.LockData {
		t.Fatalf("expected LockData=true, got false")
	}
	if res.Collection == nil || res.Collection.Name != "哈利·波特系列" {
		t.Fatalf("expected Collection='哈利·波特系列', got %+v", res.Collection)
	}

	xmlInlineSet := `<movie><title>指环王：护戒使者</title><lockdata>1</lockdata><set>指环王三部曲</set></movie>`
	res2, err := ParseNFO([]byte(xmlInlineSet))
	if err != nil {
		t.Fatalf("ParseNFO inline set failed: %v", err)
	}
	if !res2.LockData || res2.Collection == nil || res2.Collection.Name != "指环王三部曲" {
		t.Fatalf("unexpected inline set result: lock=%v collection=%+v", res2.LockData, res2.Collection)
	}

	xmlUnlocked := `<movie><title>Unlocked</title><lockdata>false</lockdata></movie>`
	res3, err := ParseNFO([]byte(xmlUnlocked))
	if err != nil {
		t.Fatalf("ParseNFO unlocked failed: %v", err)
	}
	if res3.LockData {
		t.Fatalf("expected LockData=false")
	}
}
