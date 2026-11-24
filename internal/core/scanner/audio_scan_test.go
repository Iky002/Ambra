package scanner

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/pkg/eventbus"
	"github.com/ambra-media/ambra/internal/plugins"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestIsAudioFile(t *testing.T) {
	supported := []string{
		"a.mp3", "b.FLAC", "c.m4a", "d.aac", "e.ogg", "f.opus",
		"g.wav", "h.wma", "i.alac", "j.ape", "k.dsf",
	}
	for _, f := range supported {
		if !IsAudioFile(f) {
			t.Errorf("expected IsAudioFile(%q) = true", f)
		}
	}
	unsupported := []string{"movie.mkv", "clip.mp4", "cover.jpg", "sub.srt", "notes.txt", "stream.strm"}
	for _, f := range unsupported {
		if IsAudioFile(f) {
			t.Errorf("expected IsAudioFile(%q) = false", f)
		}
	}
}

func TestParseAudioPath(t *testing.T) {
	root := filepath.Join("/media", "music")
	cases := []struct {
		name      string
		rel       string
		wantArt   string
		wantAlb   string
		wantYear  int
		wantDisc  int
		wantTrack int
		wantTitle string
	}{
		{
			name:      "artist_album_year_track",
			rel:       filepath.Join("Artist", "Album (2023)", "01 - Track Title.flac"),
			wantArt:   "Artist",
			wantAlb:   "Album",
			wantYear:  2023,
			wantDisc:  1,
			wantTrack: 1,
			wantTitle: "Track Title",
		},
		{
			name:      "disc_track_prefix",
			rel:       filepath.Join("Artist", "Album", "1-02 Track.mp3"),
			wantArt:   "Artist",
			wantAlb:   "Album",
			wantYear:  0,
			wantDisc:  1,
			wantTrack: 2,
			wantTitle: "Track",
		},
		{
			name:      "disc_subdir",
			rel:       filepath.Join("Artist", "Double Album (2020)", "Disc 2", "05 - Bonus_Track.wav"),
			wantArt:   "Artist",
			wantAlb:   "Double Album",
			wantYear:  2020,
			wantDisc:  2,
			wantTrack: 5,
			wantTitle: "Bonus Track",
		},
		{
			name:      "leading_year_album",
			rel:       filepath.Join("Band", "[2019] First Record", "04 Song Name.opus"),
			wantArt:   "Band",
			wantAlb:   "First Record",
			wantYear:  2019,
			wantDisc:  1,
			wantTrack: 4,
			wantTitle: "Song Name",
		},
		{
			name:      "bare_leading_year_album",
			rel:       filepath.Join("Band", "2018 - Early Demos", "2-03. Demo Track.ogg"),
			wantArt:   "Band",
			wantAlb:   "Early Demos",
			wantYear:  2018,
			wantDisc:  2,
			wantTrack: 3,
			wantTitle: "Demo Track",
		},
		{
			name:      "album_only_no_artist",
			rel:       filepath.Join("Album", "03. Mr. Brightside.m4a"),
			wantArt:   "",
			wantAlb:   "Album",
			wantYear:  0,
			wantDisc:  1,
			wantTrack: 3,
			wantTitle: "Mr. Brightside",
		},
		{
			name:      "flat_root_numbered",
			rel:       "01 Song.mp3",
			wantArt:   "",
			wantAlb:   "",
			wantYear:  0,
			wantDisc:  1,
			wantTrack: 1,
			wantTitle: "Song",
		},
		{
			name:      "flat_root_unnumbered",
			rel:       "Loose Track.flac",
			wantArt:   "",
			wantAlb:   "",
			wantYear:  0,
			wantDisc:  1,
			wantTrack: 0,
			wantTitle: "Loose Track",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ParseAudioPath(root, filepath.Join(root, tc.rel))
			if got.Artist != tc.wantArt {
				t.Errorf("Artist = %q, want %q", got.Artist, tc.wantArt)
			}
			if got.Album != tc.wantAlb {
				t.Errorf("Album = %q, want %q", got.Album, tc.wantAlb)
			}
			if got.Year != tc.wantYear {
				t.Errorf("Year = %d, want %d", got.Year, tc.wantYear)
			}
			if got.DiscNumber != tc.wantDisc {
				t.Errorf("DiscNumber = %d, want %d", got.DiscNumber, tc.wantDisc)
			}
			if got.TrackNumber != tc.wantTrack {
				t.Errorf("TrackNumber = %d, want %d", got.TrackNumber, tc.wantTrack)
			}
			if got.Title != tc.wantTitle {
				t.Errorf("Title = %q, want %q", got.Title, tc.wantTitle)
			}
		})
	}
}

func TestDefaultAudioCodecForContainer(t *testing.T) {
	cases := map[string]string{
		"mp3":  "mp3",
		"flac": "flac",
		"m4a":  "aac",
		"aac":  "aac",
		"ogg":  "vorbis",
		"opus": "opus",
		"wav":  "pcm_s16le",
		"wma":  "wmav2",
		"alac": "alac",
		"ape":  "ape",
		"dsf":  "dsd_lsbf",
		"xyz":  "xyz",
		"":     "aac",
	}
	for in, want := range cases {
		if got := defaultAudioCodecForContainer(in); got != want {
			t.Errorf("defaultAudioCodecForContainer(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestScanMusicLibraryEndToEnd(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	cfg, err := config.Load(filepath.Join(tmp, "data"))
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	store, err := sqlite.Open(cfg.DBPath())
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	libRepo := sqlite.NewLibraryRepo(store)
	reg := plugins.NewRegistry()
	bus := eventbus.New()
	sc := New(libRepo, reg, bus, cfg)
	sc.SetProber(NewDisabledProber())

	musicRoot := filepath.Join(tmp, "music")
	artistDir := filepath.Join(musicRoot, "周杰伦")
	albumDir := filepath.Join(artistDir, "范特西 (2001)")
	compDir := filepath.Join(musicRoot, "Compilation (2023)")
	for _, d := range []string{albumDir, compDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("MkdirAll %s: %v", d, err)
		}
	}

	track1 := filepath.Join(albumDir, "01 - 爱在西元前.flac")
	track2 := filepath.Join(albumDir, "02 - 简单爱.mp3")
	track3 := filepath.Join(compDir, "03. Mr. Brightside.m4a")
	flatTrack := filepath.Join(musicRoot, "01 Loose Single.opus")

	files := map[string][]byte{
		filepath.Join(artistDir, "artist.jpg"): []byte("artist-img"),
		filepath.Join(albumDir, "cover.jpg"):   []byte("album-cover"),
		filepath.Join(albumDir, "fanart.jpg"):  []byte("album-backdrop"),
		filepath.Join(compDir, "front.png"):    []byte("comp-cover"),
		track1:                                 []byte("fake-flac-data"),
		track2:                                 []byte("fake-mp3-data"),
		track3:                                 []byte("fake-m4a-data"),
		flatTrack:                              []byte("fake-opus-data"),
	}
	for p, data := range files {
		if err := os.WriteFile(p, data, 0o644); err != nil {
			t.Fatalf("WriteFile %s: %v", p, err)
		}
	}

	lib, err := libRepo.CreateLibrary(ctx, "Music", "music", []string{musicRoot})
	if err != nil {
		t.Fatalf("CreateLibrary: %v", err)
	}

	stats, err := sc.ScanLibrary(ctx, lib.ID)
	if err != nil {
		t.Fatalf("ScanLibrary: %v", err)
	}
	if stats.ItemsAdded != 4 || stats.Errors != 0 {
		t.Fatalf("unexpected first scan stats: %+v", stats)
	}

	// Verify Artist -> Album -> Audio hierarchy
	artistRec, err := libRepo.FindItemByPath(ctx, lib.ID, artistDir)
	if err != nil || artistRec == nil {
		t.Fatalf("FindItemByPath artist: %v", err)
	}
	if artistRec.Type != "MusicArtist" || artistRec.Name != "周杰伦" || artistRec.ParentID != lib.ID {
		t.Fatalf("unexpected artist record: %+v", artistRec)
	}
	artistImg, err := libRepo.GetImageByItemAndType(ctx, artistRec.ID, "Primary", 0)
	if err != nil || artistImg == nil || filepath.Base(artistImg.Path) != "artist.jpg" {
		t.Fatalf("expected artist Primary=artist.jpg, got %+v err=%v", artistImg, err)
	}

	albumRec, err := libRepo.FindItemByPath(ctx, lib.ID, albumDir)
	if err != nil || albumRec == nil {
		t.Fatalf("FindItemByPath album: %v", err)
	}
	if albumRec.Type != "MusicAlbum" || albumRec.Name != "范特西" || albumRec.ParentID != artistRec.ID {
		t.Fatalf("unexpected album record: %+v", albumRec)
	}
	if albumRec.ProductionYear == nil || *albumRec.ProductionYear != 2001 {
		t.Fatalf("expected album ProductionYear=2001, got %v", albumRec.ProductionYear)
	}
	if albumRec.Extra.AlbumArtist != "周杰伦" {
		t.Fatalf("expected album AlbumArtist=周杰伦, got %q", albumRec.Extra.AlbumArtist)
	}
	if albumRec.Extra.PrimaryImageAspectRatio == nil || *albumRec.Extra.PrimaryImageAspectRatio != 1.0 {
		t.Fatalf("expected album PrimaryImageAspectRatio=1.0, got %v", albumRec.Extra.PrimaryImageAspectRatio)
	}
	albumCover, err := libRepo.GetImageByItemAndType(ctx, albumRec.ID, "Primary", 0)
	if err != nil || albumCover == nil || filepath.Base(albumCover.Path) != "cover.jpg" {
		t.Fatalf("expected album Primary=cover.jpg, got %+v err=%v", albumCover, err)
	}
	albumBackdrop, err := libRepo.GetImageByItemAndType(ctx, albumRec.ID, "Backdrop", 0)
	if err != nil || albumBackdrop == nil || filepath.Base(albumBackdrop.Path) != "fanart.jpg" {
		t.Fatalf("expected album Backdrop=fanart.jpg, got %+v err=%v", albumBackdrop, err)
	}

	// Verify Audio track metadata, media source, and audio-only stream
	t1Rec, err := libRepo.FindItemByMediaSourcePath(ctx, lib.ID, track1)
	if err != nil || t1Rec == nil {
		t.Fatalf("FindItemByMediaSourcePath track1: %v", err)
	}
	if t1Rec.Type != "Audio" || t1Rec.MediaType != "Audio" || t1Rec.ParentID != albumRec.ID {
		t.Fatalf("unexpected track1 record: %+v", t1Rec)
	}
	if t1Rec.IndexNumber == nil || *t1Rec.IndexNumber != 1 {
		t.Fatalf("expected track1 IndexNumber=1, got %v", t1Rec.IndexNumber)
	}
	if t1Rec.ParentIndexNumber == nil || *t1Rec.ParentIndexNumber != 1 {
		t.Fatalf("expected track1 ParentIndexNumber=1, got %v", t1Rec.ParentIndexNumber)
	}
	if t1Rec.Extra.Album != "范特西" || t1Rec.Extra.AlbumArtist != "周杰伦" || len(t1Rec.Extra.Artists) != 1 || t1Rec.Extra.Artists[0] != "周杰伦" {
		t.Fatalf("unexpected track1 Extra: %+v", t1Rec.Extra)
	}
	sources, err := libRepo.GetMediaSourcesByItemID(ctx, t1Rec.ID)
	if err != nil || len(sources) != 1 {
		t.Fatalf("expected 1 media source for track1, got %d err=%v", len(sources), err)
	}
	if sources[0].Container != "flac" || len(sources[0].Streams) != 1 || sources[0].Streams[0].StreamType != "Audio" || sources[0].Streams[0].Codec != "flac" {
		t.Fatalf("unexpected track1 media source: %+v", sources[0])
	}

	// Verify album-without-artist has ParentID=lib.ID and front.png cover
	compRec, err := libRepo.FindItemByPath(ctx, lib.ID, compDir)
	if err != nil || compRec == nil || compRec.ParentID != lib.ID {
		t.Fatalf("unexpected compilation album: %+v err=%v", compRec, err)
	}
	compImg, err := libRepo.GetImageByItemAndType(ctx, compRec.ID, "Primary", 0)
	if err != nil || compImg == nil || filepath.Base(compImg.Path) != "front.png" {
		t.Fatalf("expected comp Primary=front.png, got %+v err=%v", compImg, err)
	}

	// Verify Chinese & Pinyin FTS search across Artist, Album, Audio
	for _, q := range []string{"周杰伦", "fantexi", "爱在西元前", "jda"} {
		items, total, err := libRepo.QueryItems(ctx, sqlite.ItemFilter{
			ParentID:   lib.ID,
			Recursive:  true,
			SearchTerm: q,
		})
		if err != nil {
			t.Fatalf("QueryItems(%q): %v", q, err)
		}
		if total == 0 || len(items) == 0 {
			t.Fatalf("expected FTS hit for %q, got 0", q)
		}
	}

	// Verify DTO hydration exposes Album, AlbumArtist, Artists, PrimaryImageAspectRatio
	albumDTO, err := libRepo.GetItemByID(ctx, "srv", "", albumRec.ID)
	if err != nil || albumDTO == nil {
		t.Fatalf("GetItemByID album: %v", err)
	}
	if albumDTO.AlbumArtist != "周杰伦" || albumDTO.PrimaryImageAspectRatio == nil || *albumDTO.PrimaryImageAspectRatio != 1.0 {
		t.Fatalf("unexpected album DTO: %+v", albumDTO)
	}

	// Verify incremental skip on unchanged rescan
	rescanStats, err := sc.ScanLibrary(ctx, lib.ID)
	if err != nil {
		t.Fatalf("rescan ScanLibrary: %v", err)
	}
	if rescanStats.ItemsAdded != 0 || rescanStats.ItemsSkipped != 4 || rescanStats.ItemsDeleted != 0 {
		t.Fatalf("expected 4 skipped on unchanged rescan, got %+v", rescanStats)
	}

	// Verify deleted track cleanup via full rescan
	if err := os.Remove(track2); err != nil {
		t.Fatalf("Remove track2: %v", err)
	}
	delStats, err := sc.ScanLibrary(ctx, lib.ID)
	if err != nil {
		t.Fatalf("ScanLibrary after delete: %v", err)
	}
	if delStats.ItemsDeleted != 1 {
		t.Fatalf("expected ItemsDeleted=1 after deleting track2, got %+v", delStats)
	}
	if _, err := libRepo.FindItemByMediaSourcePath(ctx, lib.ID, track2); err == nil {
		t.Fatalf("expected deleted track2 to be removed from DB")
	}

	// Verify ScanPath for a newly added audio file and its deletion
	track4 := filepath.Join(albumDir, "03 - 忍者.flac")
	if err := os.WriteFile(track4, []byte("fake-flac-4"), 0o644); err != nil {
		t.Fatalf("WriteFile track4: %v", err)
	}
	spStats, err := sc.ScanPath(ctx, lib.ID, track4)
	if err != nil || spStats.ItemsAdded != 1 {
		t.Fatalf("ScanPath add track4: stats=%+v err=%v", spStats, err)
	}
	if err := os.Remove(track4); err != nil {
		t.Fatalf("Remove track4: %v", err)
	}
	spDelStats, err := sc.ScanPath(ctx, lib.ID, track4)
	if err != nil || spDelStats.ItemsDeleted != 1 {
		t.Fatalf("ScanPath delete track4: stats=%+v err=%v", spDelStats, err)
	}
}

func TestScanAudioPhaseBProbeEnrichment(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script fake ffprobe not applicable on windows")
	}
	ctx := context.Background()
	tmp := t.TempDir()
	cfg, err := config.Load(filepath.Join(tmp, "data"))
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	store, err := sqlite.Open(cfg.DBPath())
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	fakeFFprobe := filepath.Join(tmp, "fake-ffprobe.sh")
	script := `#!/bin/sh
cat <<'EOF'
{
  "format": {
    "format_name": "flac",
    "duration": "245.500000",
    "size": "32000000",
    "bit_rate": "1042000"
  },
  "streams": [
    {
      "index": 0,
      "codec_type": "audio",
      "codec_name": "flac",
      "channels": 2,
      "channel_layout": "stereo",
      "sample_rate": "96000",
      "bit_rate": "1042000",
      "disposition": {"default": 1}
    }
  ]
}
EOF
`
	if err := os.WriteFile(fakeFFprobe, []byte(script), 0o755); err != nil {
		t.Fatalf("WriteFile fake ffprobe: %v", err)
	}

	libRepo := sqlite.NewLibraryRepo(store)
	sc := New(libRepo, plugins.NewRegistry(), eventbus.New(), cfg)
	sc.SetProber(NewProber(fakeFFprobe))

	musicRoot := filepath.Join(tmp, "music")
	albumDir := filepath.Join(musicRoot, "Pink Floyd", "Wish You Were Here (1975)")
	if err := os.MkdirAll(albumDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	trackPath := filepath.Join(albumDir, "01 - Shine On You Crazy Diamond.flac")
	if err := os.WriteFile(trackPath, []byte("fake-flac"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	lib, err := libRepo.CreateLibrary(ctx, "HiRes Music", "music", []string{musicRoot})
	if err != nil {
		t.Fatalf("CreateLibrary: %v", err)
	}
	stats, err := sc.ScanLibrary(ctx, lib.ID)
	if err != nil {
		t.Fatalf("ScanLibrary: %v", err)
	}
	if stats.ItemsAdded != 1 || stats.ItemsUpdated != 1 {
		t.Fatalf("expected ItemsAdded=1, ItemsUpdated=1 from Phase B probe, got %+v", stats)
	}

	rec, err := libRepo.FindItemByMediaSourcePath(ctx, lib.ID, trackPath)
	if err != nil || rec == nil {
		t.Fatalf("FindItemByMediaSourcePath: %v", err)
	}
	if rec.RuntimeTicks == nil || *rec.RuntimeTicks <= 0 {
		t.Fatalf("expected RuntimeTicks populated by Phase B probe, got %v", rec.RuntimeTicks)
	}
	srcs, err := libRepo.GetMediaSourcesByItemID(ctx, rec.ID)
	if err != nil || len(srcs) != 1 || len(srcs[0].Streams) != 1 {
		t.Fatalf("unexpected media sources: %+v err=%v", srcs, err)
	}
	if srcs[0].Streams[0].SampleRate != 96000 || srcs[0].Streams[0].Channels != 2 || srcs[0].Bitrate != 1042000 {
		t.Fatalf("unexpected probed audio stream: %+v", srcs[0])
	}
}
