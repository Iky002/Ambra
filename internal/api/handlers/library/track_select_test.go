package library

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/ambra-media/ambra/internal/api/dto"
	"github.com/ambra-media/ambra/internal/api/middleware"
	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestGetItemAppliesUnifiedTrackSelection(t *testing.T) {
	dir := t.TempDir()
	store, err := sqlite.Open(filepath.Join(dir, "lib_tracks.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	libRepo := sqlite.NewLibraryRepo(store)
	userRepo := sqlite.NewUserRepo(store)
	cfg := config.Default(dir)
	cfg.TMDBLanguage = "zh-CN"
	ctx := context.Background()

	u := &sqlite.UserRecord{
		ID: "user-1", Name: "viewer", Policy: sqlite.DefaultUserPolicy(false), Config: sqlite.DefaultUserConfig(),
	}
	if err := userRepo.CreateUser(ctx, u); err != nil {
		t.Fatal(err)
	}
	lib, err := libRepo.CreateLibrary(ctx, "Movies", "movies", []string{dir})
	if err != nil {
		t.Fatal(err)
	}
	if err := libRepo.UpsertItem(ctx, &sqlite.ItemRecord{
		ID: "movie-1", LibraryID: lib.ID, Type: "Movie", MediaType: "Video", Name: "Matrix",
	}); err != nil {
		t.Fatal(err)
	}
	if err := libRepo.ReplaceMediaSourcesAndStreams(ctx, "movie-1", []sqlite.MediaSourceRecord{{
		ID: "ms-1", ItemID: "movie-1", Name: "Main", Protocol: "File", Container: "mkv",
		Streams: []sqlite.MediaStreamRecord{
			{MediaSourceID: "ms-1", ItemID: "movie-1", StreamIndex: 0, StreamType: "Video", Codec: "h264"},
			{MediaSourceID: "ms-1", ItemID: "movie-1", StreamIndex: 1, StreamType: "Audio", Codec: "aac", Language: "eng", IsDefault: true},
			{MediaSourceID: "ms-1", ItemID: "movie-1", StreamIndex: 2, StreamType: "Audio", Codec: "aac", Language: "chi"},
			{MediaSourceID: "ms-1", ItemID: "movie-1", StreamIndex: 3, StreamType: "Subtitle", Codec: "srt", Language: "eng", IsDefault: true},
			{MediaSourceID: "ms-1", ItemID: "movie-1", StreamIndex: 4, StreamType: "Subtitle", Codec: "ass", Language: "chi", Title: "简体中文字幕", SubVariant: "chs"},
		},
	}}); err != nil {
		t.Fatal(err)
	}

	h := New(libRepo, nil, cfg)
	h.SetUserRepo(userRepo)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux, AuthFuncs{})

	fetchItem := func() dto.BaseItemDto {
		req := httptest.NewRequest(http.MethodGet, "/Users/user-1/Items/movie-1", nil)
		req = req.WithContext(middleware.WithAuthContext(req.Context(), &middleware.AuthContext{User: u}))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET item status=%d body=%s", rec.Code, rec.Body.String())
		}
		var out dto.BaseItemDto
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		return out
	}

	// 1. Default user on zh-CN -> audio IsDefault (1), subtitle chs (4)
	item1 := fetchItem()
	ms1 := item1.MediaSources[0]
	if ms1.DefaultAudioStreamIndex == nil || *ms1.DefaultAudioStreamIndex != 1 {
		t.Fatalf("expected default audio 1, got %v", ms1.DefaultAudioStreamIndex)
	}
	if ms1.DefaultSubtitleStreamIndex == nil || *ms1.DefaultSubtitleStreamIndex != 4 {
		t.Fatalf("expected zh-CN chs subtitle 4, got %v", ms1.DefaultSubtitleStreamIndex)
	}

	// 2. Update UserConfiguration to media default for subtitle & chi for audio
	uCfg := u.Config
	uCfg.AudioLanguagePreference = "chi"
	uCfg.SubtitleLanguagePreference = "media"
	if err := userRepo.UpdateUserConfig(ctx, u.ID, uCfg); err != nil {
		t.Fatal(err)
	}
	item2 := fetchItem()
	ms2 := item2.MediaSources[0]
	if ms2.DefaultAudioStreamIndex == nil || *ms2.DefaultAudioStreamIndex != 2 {
		t.Fatalf("expected chi audio 2, got %v", ms2.DefaultAudioStreamIndex)
	}
	if ms2.DefaultSubtitleStreamIndex == nil || *ms2.DefaultSubtitleStreamIndex != 3 {
		t.Fatalf("expected media default subtitle 3, got %v", ms2.DefaultSubtitleStreamIndex)
	}

	// 3. Per-item memory subtitle=-1 overrides preference
	subOff := -1
	if _, err := userRepo.UpsertPlaybackProgress(ctx, u.ID, "movie-1", 50_000_000, false, nil, &subOff); err != nil {
		t.Fatal(err)
	}
	item3 := fetchItem()
	if item3.MediaSources[0].DefaultSubtitleStreamIndex != nil {
		t.Fatalf("expected nil subtitle when remembered=-1, got %d", *item3.MediaSources[0].DefaultSubtitleStreamIndex)
	}
}
