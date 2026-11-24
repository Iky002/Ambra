package scanner

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/pkg/eventbus"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/plugins"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestParseFFprobeJSONHasAttachedPic(t *testing.T) {
	withArt := `{
		"format": {"format_name": "mp3", "duration": "200.5"},
		"streams": [
			{"index": 0, "codec_type": "audio", "codec_name": "mp3", "channels": 2, "sample_rate": "44100"},
			{"index": 1, "codec_type": "video", "codec_name": "mjpeg", "width": 500, "height": 500, "disposition": {"attached_pic": 1}}
		]
	}`
	res, err := ParseFFprobeJSON([]byte(withArt))
	if err != nil {
		t.Fatal(err)
	}
	if !res.HasAttachedPic {
		t.Fatalf("expected HasAttachedPic=true")
	}
	if len(res.Streams) != 1 || res.Streams[0].StreamType != "Audio" {
		t.Fatalf("attached_pic should not be added as video stream, got %+v", res.Streams)
	}

	withoutArt := `{
		"format": {"format_name": "flac", "duration": "180.0"},
		"streams": [
			{"index": 0, "codec_type": "audio", "codec_name": "flac", "channels": 2, "sample_rate": "44100"}
		]
	}`
	res2, err := ParseFFprobeJSON([]byte(withoutArt))
	if err != nil {
		t.Fatal(err)
	}
	if res2.HasAttachedPic {
		t.Fatalf("expected HasAttachedPic=false")
	}
}

func TestMusicLocalNFOAndImagePaths(t *testing.T) {
	tmp := t.TempDir()
	artistDir := filepath.Join(tmp, "Pink Floyd")
	albumDir := filepath.Join(artistDir, "Dark Side of the Moon")
	if err := os.MkdirAll(albumDir, 0o755); err != nil {
		t.Fatal(err)
	}

	sc := New(nil, nil, nil, config.Default(tmp))
	ctx := context.Background()

	// Real directories
	artistRec := &sqlite.ItemRecord{Type: "MusicArtist", Path: artistDir}
	if p, ok := sc.resolveLocalImagePath(ctx, nil, artistRec, "Primary", ".jpg"); !ok || p != filepath.Join(artistDir, "folder.jpg") {
		t.Fatalf("artist Primary = (%q, %v)", p, ok)
	}
	if p, ok := sc.resolveLocalImagePath(ctx, nil, artistRec, "Backdrop", ".jpg"); !ok || p != filepath.Join(artistDir, "fanart.jpg") {
		t.Fatalf("artist Backdrop = (%q, %v)", p, ok)
	}
	if tag, nfo, ok := sc.resolveLocalNFOTarget(ctx, artistRec); !ok || tag != "artist" || nfo != filepath.Join(artistDir, "artist.nfo") {
		t.Fatalf("artist NFO = (%q, %q, %v)", tag, nfo, ok)
	}

	albumRec := &sqlite.ItemRecord{Type: "MusicAlbum", Path: albumDir}
	if p, ok := sc.resolveLocalImagePath(ctx, nil, albumRec, "Primary", ".jpg"); !ok || p != filepath.Join(albumDir, "cover.jpg") {
		t.Fatalf("album Primary = (%q, %v)", p, ok)
	}
	if p, ok := sc.resolveLocalImagePath(ctx, nil, albumRec, "Backdrop", ".png"); !ok || p != filepath.Join(albumDir, "fanart.png") {
		t.Fatalf("album Backdrop = (%q, %v)", p, ok)
	}
	if tag, nfo, ok := sc.resolveLocalNFOTarget(ctx, albumRec); !ok || tag != "album" || nfo != filepath.Join(albumDir, "album.nfo") {
		t.Fatalf("album NFO = (%q, %q, %v)", tag, nfo, ok)
	}

	// Virtual paths must be safely skipped
	virtualArtist := &sqlite.ItemRecord{Type: "MusicArtist", Path: "artist://lib1/pink floyd"}
	if _, ok := sc.resolveLocalImagePath(ctx, nil, virtualArtist, "Primary", ".jpg"); ok {
		t.Fatalf("virtual artist image should be skipped")
	}
	if _, _, ok := sc.resolveLocalNFOTarget(ctx, virtualArtist); ok {
		t.Fatalf("virtual artist NFO should be skipped")
	}

	virtualAlbum := &sqlite.ItemRecord{Type: "MusicAlbum", Path: "album://lib1/dark side"}
	if _, ok := sc.resolveLocalImagePath(ctx, nil, virtualAlbum, "Primary", ".jpg"); ok {
		t.Fatalf("virtual album image should be skipped")
	}
	if _, _, ok := sc.resolveLocalNFOTarget(ctx, virtualAlbum); ok {
		t.Fatalf("virtual album NFO should be skipped")
	}
}

func TestExtractAudioAttachedPic(t *testing.T) {
	tmp := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmp, "audio_art.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	// Create a fake ffmpeg script that writes a dummy JPEG to its last argument.
	fakeFFmpeg := filepath.Join(tmp, "fake-ffmpeg.sh")
	script := "#!/bin/sh\nfor last; do :; done\nprintf '\\377\\330\\377\\340FAKEJPEG' > \"$last\"\n"
	if err := os.WriteFile(fakeFFmpeg, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	cfg := config.Default(filepath.Join(tmp, "data"))
	cfg.FFmpegPath = fakeFFmpeg
	repo := sqlite.NewLibraryRepo(store)
	sc := New(repo, plugins.NewRegistry(), eventbus.New(), cfg)

	ctx := context.Background()
	albumDir := filepath.Join(tmp, "music", "Artist", "Album")
	if err := os.MkdirAll(albumDir, 0o755); err != nil {
		t.Fatal(err)
	}
	trackPath := filepath.Join(albumDir, "01 - Track.mp3")
	if err := os.WriteFile(trackPath, []byte("fake-mp3"), 0o644); err != nil {
		t.Fatal(err)
	}

	lib, err := repo.CreateLibrary(ctx, "Music", "music", []string{filepath.Join(tmp, "music")})
	if err != nil {
		t.Fatal(err)
	}

	albumRec := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: lib.ID, ParentID: lib.ID,
		Type: "MusicAlbum", IsFolder: true, Name: "Album", Path: albumDir,
	}
	trackRec := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: lib.ID, ParentID: albumRec.ID,
		Type: "Audio", MediaType: "Audio", Name: "Track", Path: trackPath,
		Extra: sqlite.ItemExtra{AlbumID: albumRec.ID},
	}
	_ = repo.UpsertItem(ctx, albumRec)
	_ = repo.UpsertItem(ctx, trackRec)

	if ok := sc.extractAudioAttachedPic(ctx, trackRec, trackPath); !ok {
		t.Fatalf("expected extractAudioAttachedPic to succeed")
	}

	wantCover := filepath.Join(albumDir, "cover.jpg")
	if _, err := os.Stat(wantCover); err != nil {
		t.Fatalf("expected local album cover.jpg at %s: %v", wantCover, err)
	}
	albumImg, err := repo.GetImageByItemAndType(ctx, albumRec.ID, "Primary", 0)
	if err != nil || albumImg == nil || albumImg.Path != wantCover {
		t.Fatalf("expected album Primary image at %s, got %+v (err=%v)", wantCover, albumImg, err)
	}
	trackImg, err := repo.GetImageByItemAndType(ctx, trackRec.ID, "Primary", 0)
	if err != nil || trackImg == nil || trackImg.Path != wantCover {
		t.Fatalf("expected track Primary image at %s, got %+v (err=%v)", wantCover, trackImg, err)
	}
}
