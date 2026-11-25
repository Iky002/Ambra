package scanner

import (
	"testing"
)

func TestParseMovieName_MoviePilotRules(t *testing.T) {
	cases := []struct {
		name          string
		input         string
		wantTitle     string
		wantOrigTitle string
		wantYear      int
		wantTMDB      string
		wantIMDb      string
	}{
		{
			name:      "The Legend of 1900 (1998) consecutive years",
			input:     "The.Legend.of.1900.1998.1080p.strm",
			wantTitle: "The Legend of 1900",
			wantYear:  1998,
		},
		{
			name:      "Blade Runner 2049 (2017) future year in title",
			input:     "Blade.Runner.2049.2017.2160p.strm",
			wantTitle: "Blade Runner 2049",
			wantYear:  2017,
		},
		{
			name:      "2001 A Space Odyssey (1968) leading year in title",
			input:     "2001.A.Space.Odyssey.1968.2160p.UHD.BluRay.REMUX.mkv",
			wantTitle: "2001 A Space Odyssey",
			wantYear:  1968,
		},
		{
			name:      "1917 (2019) year-only title",
			input:     "1917.2019.1080p.BluRay.x265.10bit.mkv",
			wantTitle: "1917",
			wantYear:  2019,
		},
		{
			name:      "2012 (2009) year-only title",
			input:     "2012.2009.1080p.WEB-DL.H264.AAC.mp4",
			wantTitle: "2012",
			wantYear:  2009,
		},
		{
			name:      "Wonder Woman 1984 (2020)",
			input:     "Wonder.Woman.1984.2020.2160p.HDR10+.DV.Atmos.mkv",
			wantTitle: "Wonder Woman 1984",
			wantYear:  2020,
		},
		{
			name:      "1984 (1984) identical year title",
			input:     "1984.1984.1080p.BluRay.mkv",
			wantTitle: "1984",
			wantYear:  1984,
		},
		{
			name:      "Full-width Chinese parentheses in folder name",
			input:     "海上钢琴师 （1998）",
			wantTitle: "海上钢琴师",
			wantYear:  1998,
		},
		{
			name:      "Chinese title with future year and ASCII parens",
			input:     "银翼杀手2049 (2017)",
			wantTitle: "银翼杀手2049",
			wantYear:  2017,
		},
		{
			name:          "Bilingual Chinese + English title with release tags",
			input:         "[高清电影]肖申克的救赎.The.Shawshank.Redemption.1994.1080p.国粤双语.中英双字.mkv",
			wantTitle:     "肖申克的救赎",
			wantOrigTitle: "The Shawshank Redemption",
			wantYear:      1994,
		},
		{
			name:      "Inline TMDB & IMDb provider tags",
			input:     "海上钢琴师 (1998) {tmdb-10376} [imdb=tt0120731].mkv",
			wantTitle: "海上钢琴师",
			wantYear:  1998,
			wantTMDB:  "10376",
			wantIMDb:  "tt0120731",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ParseMovieName(tc.input)
			if got.Title != tc.wantTitle {
				t.Errorf("Title = %q, want %q (input=%q)", got.Title, tc.wantTitle, tc.input)
			}
			if got.OriginalTitle != tc.wantOrigTitle {
				t.Errorf("OriginalTitle = %q, want %q (input=%q)", got.OriginalTitle, tc.wantOrigTitle, tc.input)
			}
			if got.Year != tc.wantYear {
				t.Errorf("Year = %d, want %d (input=%q)", got.Year, tc.wantYear, tc.input)
			}
			if tc.wantTMDB != "" && got.ProviderIDs["tmdb"] != tc.wantTMDB {
				t.Errorf("ProviderIDs[tmdb] = %q, want %q", got.ProviderIDs["tmdb"], tc.wantTMDB)
			}
			if tc.wantIMDb != "" && got.ProviderIDs["imdb"] != tc.wantIMDb {
				t.Errorf("ProviderIDs[imdb] = %q, want %q", got.ProviderIDs["imdb"], tc.wantIMDb)
			}
		})
	}
}

func TestParseMoviePath_CrossEnrichesFolderAndFilename(t *testing.T) {
	t.Run("Legend of 1900 with Chinese folder full-width parens", func(t *testing.T) {
		got := ParseMoviePath("/media/movies", "/media/movies/海上钢琴师 （1998）/The.Legend.of.1900.1998.1080p.strm")
		if got.Title != "海上钢琴师" || got.OriginalTitle != "The Legend of 1900" || got.Year != 1998 {
			t.Fatalf("unexpected ParseMoviePath result: %+v", got)
		}
	})

	t.Run("Blade Runner 2049 with Chinese folder", func(t *testing.T) {
		got := ParseMoviePath("/media/movies", "/media/movies/银翼杀手2049 (2017)/Blade.Runner.2049.2017.2160p.strm")
		if got.Title != "银翼杀手2049" || got.OriginalTitle != "Blade Runner 2049" || got.Year != 2017 {
			t.Fatalf("unexpected ParseMoviePath result: %+v", got)
		}
	})

	t.Run("Folder provides TMDB ID and year when filename lacks them", func(t *testing.T) {
		got := ParseMoviePath("/media/movies", "/media/movies/星际穿越 (2014) {tmdb-157336}/Interstellar.2160p.strm")
		if got.Title != "星际穿越" || got.OriginalTitle != "Interstellar" || got.Year != 2014 || got.ProviderIDs["tmdb"] != "157336" {
			t.Fatalf("unexpected ParseMoviePath result: %+v", got)
		}
	})

	t.Run("Generic library root folder is ignored", func(t *testing.T) {
		got := ParseMoviePath("/media/movies", "/media/movies/The.Legend.of.1900.1998.1080p.strm")
		if got.Title != "The Legend of 1900" || got.Year != 1998 {
			t.Fatalf("unexpected flat movie result: %+v", got)
		}
	})
}
