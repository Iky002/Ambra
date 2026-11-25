package audio

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestAudioParentalControls(t *testing.T) {
	tmp := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmp, "audio_parental.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	ctx := context.Background()
	repo := sqlite.NewLibraryRepo(store)
	userRepo := sqlite.NewUserRepo(store)
	cfg := config.Default(tmp)

	lib, _ := repo.CreateLibrary(ctx, "Music", "music", []string{tmp})
	mp3Explicit := filepath.Join(tmp, "explicit.mp3")
	mp3Clean := filepath.Join(tmp, "clean.mp3")
	_ = os.WriteFile(mp3Explicit, []byte("ID3explicit"), 0o644)
	_ = os.WriteFile(mp3Clean, []byte("ID3clean"), 0o644)
	_ = os.WriteFile(filepath.Join(tmp, "explicit.lrc"), []byte("[00:01.00]Explicit\n"), 0o644)
	_ = os.WriteFile(filepath.Join(tmp, "clean.lrc"), []byte("[00:01.00]Clean\n"), 0o644)

	explicitSong := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: lib.ID, ParentID: lib.ID,
		Type: "Audio", MediaType: "Audio", Name: "Explicit Song", SortName: "explicit",
		Path: mp3Explicit, OfficialRating: "R",
	}
	taggedSong := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: lib.ID, ParentID: lib.ID,
		Type: "Audio", MediaType: "Audio", Name: "Tagged Song", SortName: "tagged",
		Path: mp3Explicit, OfficialRating: "G",
	}
	cleanSong := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: lib.ID, ParentID: lib.ID,
		Type: "Audio", MediaType: "Audio", Name: "Clean Song", SortName: "clean",
		Path: mp3Clean, OfficialRating: "G",
	}
	_ = repo.UpsertItem(ctx, explicitSong)
	_ = repo.UpsertItem(ctx, taggedSong)
	_ = repo.UpsertItem(ctx, cleanSong)
	_ = repo.ReplaceItemTaxonomies(ctx, taggedSong.ID, "tag", []string{"explicit"})

	maxRating := 5
	kidPolicy := sqlite.DefaultUserPolicy(false)
	kidPolicy.MaxParentalRating = &maxRating
	kidPolicy.BlockedTags = []string{"explicit"}
	kidUser := &sqlite.UserRecord{ID: guid.New(), Name: "kid", Policy: kidPolicy, Config: sqlite.DefaultUserConfig()}
	adminUser := &sqlite.UserRecord{ID: guid.New(), Name: "admin", IsAdmin: true, Policy: sqlite.DefaultUserPolicy(true), Config: sqlite.DefaultUserConfig()}
	_ = userRepo.CreateUser(ctx, kidUser)
	_ = userRepo.CreateUser(ctx, adminUser)

	currentCaller := kidUser.ID
	mux := http.NewServeMux()
	h := New(repo, cfg)
	h.SetUserRepo(userRepo)
	h.RegisterRoutes(mux, AuthFuncs{
		UserIDFromCtx: func(context.Context) string { return currentCaller },
	})

	// Kid blocked on rating-blocked and tag-blocked songs across stream, universal, Lyrics
	for _, blockedID := range []string{explicitSong.ID, taggedSong.ID} {
		for _, p := range []string{
			"/Audio/" + blockedID + "/stream",
			"/Audio/" + blockedID + "/universal?Container=mp3",
			"/Audio/" + blockedID + "/Lyrics",
		} {
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, p, nil))
			if rec.Code != http.StatusForbidden {
				t.Fatalf("kid GET %s: got %d, want 403", p, rec.Code)
			}
		}
	}

	// Kid allowed on clean song
	for _, p := range []string{
		"/Audio/" + cleanSong.ID + "/stream",
		"/Audio/" + cleanSong.ID + "/universal?Container=mp3",
		"/Audio/" + cleanSong.ID + "/Lyrics",
	} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, p, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("kid GET %s: got %d, want 200", p, rec.Code)
		}
	}

	// Admin allowed on explicit song
	currentCaller = adminUser.ID
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/Audio/"+explicitSong.ID+"/stream", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("admin GET explicit stream: got %d, want 200", rec.Code)
	}
}

func TestAudioTranscodeArgsAndMP4Muxing(t *testing.T) {
	// 1. m4a/mp4 target returns audio/mp4 + mp4 + aac
	for _, c := range []string{"m4a", "mp4"} {
		ct, muxFmt, enc := resolveAudioTranscodeTarget(c, "")
		if ct != "audio/mp4" || muxFmt != "mp4" || enc != "aac" {
			t.Fatalf("resolveAudioTranscodeTarget(%q) = (%q, %q, %q), want (audio/mp4, mp4, aac)", c, ct, muxFmt, enc)
		}
	}

	// 2. buildAudioFFmpegArgs places -ss before -i, honors -ac/-ar, and adds -movflags for mp4
	args := buildAudioFFmpegArgs("/music/track.flac", "mp4", "aac", 192000, 125_000_000, 2, 44100)
	joined := strings.Join(args, " ")
	ssIdx := strings.Index(joined, "-ss 12.500")
	iIdx := strings.Index(joined, "-i /music/track.flac")
	if ssIdx == -1 || iIdx == -1 || ssIdx > iIdx {
		t.Fatalf("expected -ss 12.500 before -i, got args: %s", joined)
	}
	for _, want := range []string{"-ac 2", "-ar 44100", "-movflags frag_keyframe+empty_moov", "-f mp4 pipe:1"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("expected %q in ffmpeg args, got: %s", want, joined)
		}
	}

	// 3. End-to-end mock ffmpeg captures query params through handleUniversal
	if runtime.GOOS == "windows" {
		return
	}
	tmp := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmp, "audio_args.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	ctx := context.Background()
	repo := sqlite.NewLibraryRepo(store)
	flacPath := filepath.Join(tmp, "song.flac")
	_ = os.WriteFile(flacPath, []byte("fake-flac"), 0o644)
	itemID := guid.New()
	_ = repo.UpsertItem(ctx, &sqlite.ItemRecord{ID: itemID, LibraryID: "lib1", Type: "Audio", Name: "Track", Path: flacPath})

	mockFF := filepath.Join(tmp, "echo_args.sh")
	_ = os.WriteFile(mockFF, []byte("#!/bin/sh\nprintf '%s' \"$*\"\n"), 0o755)
	cfg := config.Default(tmp)
	cfg.FFmpegPath = mockFF

	mux := http.NewServeMux()
	New(repo, cfg).RegisterRoutes(mux, AuthFuncs{})

	rec := httptest.NewRecorder()
	reqURL := "/Audio/" + itemID + "/universal.m4a?Container=mp3&TranscodingContainer=m4a&StartTimeTicks=300000000&AudioChannels=2&AudioSampleRate=48000&AudioBitrate=256000"
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, reqURL, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("universal.m4a status=%d body=%q", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "audio/mp4" {
		t.Fatalf("expected Content-Type audio/mp4, got %q", ct)
	}
	gotArgs := rec.Body.String()
	for _, want := range []string{"-ss 30.000 -i " + flacPath, "-ac 2", "-ar 48000", "-movflags frag_keyframe+empty_moov", "-f mp4 pipe:1"} {
		if !strings.Contains(gotArgs, want) {
			t.Fatalf("expected %q in captured ffmpeg args, got: %s", want, gotArgs)
		}
	}
}
