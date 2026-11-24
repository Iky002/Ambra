package scanner

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/plugins"
	"github.com/ambra-media/ambra/internal/plugins/nfo"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestParseFFprobeJSON_PreservesTrueStreamIndexAndFormatTags(t *testing.T) {
	raw := []byte(`{
		"format": {
			"format_name": "matroska,webm",
			"duration": "120.0",
			"tags": {
				"TITLE": "Bohemian Rhapsody",
				"ARTIST": "Queen",
				"ALBUM": "A Night at the Opera",
				"DATE": "1975-10-31"
			}
		},
		"streams": [
			{"index": 0, "codec_type": "video", "codec_name": "hevc", "width": 1920, "height": 1080},
			{"index": 1, "codec_type": "video", "codec_name": "mjpeg", "disposition": {"attached_pic": 1}},
			{"index": 2, "codec_type": "attachment", "codec_name": "ttf"},
			{"index": 3, "codec_type": "audio", "codec_name": "aac", "channels": 2, "tags": {"language": "eng"}},
			{"index": 4, "codec_type": "subtitle", "codec_name": "ass", "tags": {"language": "chi", "title": "简体中文"}}
		]
	}`)

	res, err := ParseFFprobeJSON(raw)
	if err != nil {
		t.Fatalf("ParseFFprobeJSON: %v", err)
	}
	if len(res.Streams) != 3 {
		t.Fatalf("expected 3 non-attachment streams, got %d", len(res.Streams))
	}
	if res.Streams[0].Index != 0 || res.Streams[1].Index != 3 || res.Streams[2].Index != 4 {
		t.Fatalf("expected true FFprobe indices [0, 3, 4], got [%d, %d, %d]",
			res.Streams[0].Index, res.Streams[1].Index, res.Streams[2].Index)
	}
	if res.Tags["TITLE"] != "Bohemian Rhapsody" || res.Tags["ARTIST"] != "Queen" || res.Tags["ALBUM"] != "A Night at the Opera" {
		t.Fatalf("expected format tags parsed, got %+v", res.Tags)
	}

	// Verify buildMediaSource preserves true indices and starts external subtitles at maxEmbedded+1 (5, not 3).
	tmp := t.TempDir()
	mkvPath := filepath.Join(tmp, "TestMovie.mkv")
	_ = os.WriteFile(mkvPath, []byte("mkv"), 0o644)
	_ = os.WriteFile(filepath.Join(tmp, "TestMovie.eng.srt"), []byte("1\n00:00:00,000 --> 00:00:01,000\nHi\n"), 0o644)

	sc := New(nil, nil, nil, nil)
	ms := sc.buildMediaSource(&sqlite.ItemRecord{ID: "item1", Type: "Movie", MediaType: "Video"}, mkvPath, 100, 1000, false, "mkv", res)
	if len(ms.Streams) != 4 {
		t.Fatalf("expected 4 streams (3 embedded + 1 external), got %d: %+v", len(ms.Streams), ms.Streams)
	}
	if ms.Streams[0].StreamIndex != 0 || ms.Streams[1].StreamIndex != 3 || ms.Streams[2].StreamIndex != 4 || ms.Streams[3].StreamIndex != 5 {
		t.Fatalf("expected stream indices [0, 3, 4, 5], got [%d, %d, %d, %d]",
			ms.Streams[0].StreamIndex, ms.Streams[1].StreamIndex, ms.Streams[2].StreamIndex, ms.Streams[3].StreamIndex)
	}
	if !ms.Streams[3].IsExternal {
		t.Fatalf("expected stream at index 5 to be external subtitle")
	}
}

func TestApplyAudioProbeTags(t *testing.T) {
	rec := &sqlite.ItemRecord{
		ID:        "aud-1",
		Type:      "Audio",
		MediaType: "Audio",
		Name:      "01-unknown",
	}
	changed := applyAudioProbeTags(rec, map[string]string{
		"title":        "Starlight",
		"album":        "Black Holes",
		"artist":       "Muse",
		"album_artist": "Muse",
		"date":         "2006-07-03",
		"track":        "2/11",
		"disc":         "1/1",
	})
	if !changed {
		t.Fatalf("expected applyAudioProbeTags to report changed=true")
	}
	if rec.Name != "Starlight" || rec.Extra.Album != "Black Holes" || rec.Extra.AlbumArtist != "Muse" {
		t.Fatalf("unexpected enriched audio rec: %+v", rec)
	}
	if len(rec.Extra.Artists) != 1 || rec.Extra.Artists[0] != "Muse" {
		t.Fatalf("unexpected artists: %+v", rec.Extra.Artists)
	}
	if rec.ProductionYear == nil || *rec.ProductionYear != 2006 {
		t.Fatalf("unexpected ProductionYear: %v", rec.ProductionYear)
	}
	if rec.IndexNumber == nil || *rec.IndexNumber != 2 || rec.ParentIndexNumber == nil || *rec.ParentIndexNumber != 1 {
		t.Fatalf("unexpected track/disc: %v/%v", rec.IndexNumber, rec.ParentIndexNumber)
	}
}

func TestLockDataAndLockedRelationsPreservedInEnrichRefreshAndNFO(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	mediaRoot := t.TempDir()
	movieDir := filepath.Join(mediaRoot, "LockedMovie (2022)")
	_ = os.MkdirAll(movieDir, 0o755)
	moviePath := filepath.Join(movieDir, "LockedMovie (2022).mkv")
	_ = os.WriteFile(moviePath, []byte("mkv"), 0o644)

	store, err := sqlite.Open(filepath.Join(tmp, "lock_test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	repo := sqlite.NewLibraryRepo(store)
	lib, err := repo.CreateLibrary(ctx, "Movies", "movies", []string{mediaRoot})
	if err != nil {
		t.Fatal(err)
	}

	providerCalls := 0
	reg := plugins.NewRegistry()
	reg.Register(&localSaveStubProvider{
		fn: func(q plugins.MediaQuery) *plugins.MetadataResult {
			providerCalls++
			return &plugins.MetadataResult{
				Name:     "Online Title",
				Overview: "Online Overview",
				Genres:   []string{"OnlineGenre"},
				Studios:  []string{"OnlineStudio"},
				Tags:     []string{"OnlineTag"},
				People:   []plugins.PersonCredit{{Name: "Online Actor", Type: "Actor"}},
			}
		},
	})

	sc := New(repo, reg, nil, &config.Config{DataDir: tmp})

	// 1. LockData=true: enrichItem & RefreshItem must skip online metadata, relations, images, and NFO writes.
	lockedID := guid.New()
	lockedRec := &sqlite.ItemRecord{
		ID:        lockedID,
		LibraryID: lib.ID,
		ParentID:  lib.ID,
		Type:      "Movie",
		MediaType: "Video",
		Name:      "User Locked Title",
		Path:      moviePath,
		Extra: sqlite.ItemExtra{
			Overview: "User Locked Overview",
			LockData: true,
		},
	}
	if err := repo.UpsertItem(ctx, lockedRec); err != nil {
		t.Fatal(err)
	}
	_ = repo.ReplaceItemTaxonomies(ctx, lockedID, "genre", []string{"CustomGenre"})

	if updated := sc.enrichItem(ctx, enrichTask{
		ItemID:         lockedID,
		LibraryID:      lib.ID,
		ItemType:       "Movie",
		MediaPath:      moviePath,
		NeedOnlineMeta: true,
		Query:          plugins.MediaQuery{ItemType: "Movie", Title: "LockedMovie"},
	}); updated {
		t.Fatalf("expected enrichItem to return false for LockData=true item")
	}
	if err := sc.RefreshItem(ctx, lockedID, true, true); err != nil {
		t.Fatalf("RefreshItem: %v", err)
	}
	if providerCalls != 0 {
		t.Fatalf("expected 0 online provider calls for LockData=true item, got %d", providerCalls)
	}
	if _, statErr := os.Stat(filepath.Join(movieDir, "LockedMovie (2022).nfo")); !os.IsNotExist(statErr) {
		t.Fatalf("expected no NFO written for LockData=true item")
	}

	// 2. Partial lock (Genres locked): RefreshItem must preserve persisted Genres in SQLite AND in rewritten .nfo.
	lockedRec.Extra.LockData = false
	lockedRec.Extra.LockedFields = []string{"Genres"}
	if err := repo.UpsertItem(ctx, lockedRec); err != nil {
		t.Fatal(err)
	}
	if err := sc.RefreshItem(ctx, lockedID, true, false); err != nil {
		t.Fatalf("RefreshItem partial lock: %v", err)
	}
	genres, studios, _, err := repo.GetTaxonomiesByItemID(ctx, lockedID)
	if err != nil {
		t.Fatal(err)
	}
	if len(genres) != 1 || genres[0] != "CustomGenre" {
		t.Fatalf("expected locked Genres=[CustomGenre] preserved, got %+v", genres)
	}
	if len(studios) != 1 || studios[0] != "OnlineStudio" {
		t.Fatalf("expected unlocked Studios=[OnlineStudio] updated, got %+v", studios)
	}

	nfoBytes, err := os.ReadFile(filepath.Join(movieDir, "LockedMovie (2022).nfo"))
	if err != nil {
		t.Fatalf("expected NFO file written: %v", err)
	}
	parsedNFO, err := nfo.ParseNFO(nfoBytes)
	if err != nil {
		t.Fatalf("ParseNFO: %v", err)
	}
	if len(parsedNFO.Genres) != 1 || parsedNFO.Genres[0] != "CustomGenre" {
		t.Fatalf("expected NFO to preserve locked genre CustomGenre, got %+v\n%s", parsedNFO.Genres, string(nfoBytes))
	}
	if !strings.Contains(string(nfoBytes), "<studio>OnlineStudio</studio>") {
		t.Fatalf("expected NFO to include unlocked studio OnlineStudio:\n%s", string(nfoBytes))
	}
}
