package session

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/ambra-media/ambra/internal/api/dto"
	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/core/auth"
	"github.com/ambra-media/ambra/internal/pkg/eventbus"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestCloneSessionDeepCopiesMediaSourcesAndNoDataRace(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default(dir)
	mgr := NewManager(nil, cfg, eventbus.New())

	user := &sqlite.UserRecord{
		ID:        guid.New(),
		Name:      "viewer",
		Policy:    sqlite.DefaultUserPolicy(false),
		Config:    sqlite.DefaultUserConfig(),
		CreatedAt: time.Now().UTC(),
	}
	client := auth.ClientInfo{Client: "Web", Device: "Browser", DeviceID: "dev-clone"}
	audioIdx := 1
	subIdx := 2
	srcAudioIdx := 1
	srcSubIdx := 2

	item := &dto.BaseItemDto{
		Id:                      "item-1",
		Name:                    "Secret Movie",
		Path:                    "/secret/movies/movie.mkv",
		Taglines:                []string{"Line 1"},
		Tags:                    []string{"Tag1"},
		Genres:                  []string{"Sci-Fi"},
		GenreItems:              []dto.NameIdPair{{Id: "g1", Name: "Sci-Fi"}},
		ProviderIds:             map[string]string{"Tmdb": "123"},
		ParentBackdropImageTags: []string{"pb1"},
		Artists:                 []string{"Artist1"},
		Studios:                 []dto.NameIdPair{{Id: "s1", Name: "Studio1"}},
		People:                  []dto.BaseItemPerson{{Id: "p1", Name: "Actor1", Type: "Actor"}},
		Chapters:                []dto.ChapterInfo{{Name: "Ch1", StartPositionTicks: 100}},
		ImageTags:               map[string]string{"Primary": "img1"},
		BackdropImageTags:       []string{"bd1"},
		MediaSources: []dto.MediaSourceInfo{
			{
				Id:                         "ms-1",
				Path:                       "/secret/movies/movie.mkv",
				Protocol:                   "File",
				DefaultAudioStreamIndex:    &srcAudioIdx,
				DefaultSubtitleStreamIndex: &srcSubIdx,
				MediaStreams: []dto.MediaStream{
					{Index: 0, Type: "Video", Codec: "h264"},
					{Index: 2, Type: "Subtitle", Codec: "srt", IsExternal: true, Path: "/secret/movies/movie.srt"},
				},
			},
		},
		MediaStreams: []dto.MediaStream{
			{Index: 2, Type: "Subtitle", Codec: "srt", IsExternal: true, Path: "/secret/movies/movie.srt"},
		},
	}

	if err := mgr.ReportPlaybackStart(context.Background(), user, client, PlaybackStartRequest{
		ItemId:              "item-1",
		Item:                item,
		AudioStreamIndex:    &audioIdx,
		SubtitleStreamIndex: &subIdx,
	}); err != nil {
		t.Fatalf("ReportPlaybackStart: %v", err)
	}

	// Concurrent ListSessions + SanitizeItemPaths must not race or mutate stored session paths.
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				list := mgr.ListSessions()
				for k := range list {
					dto.SanitizeItemPaths(list[k].NowPlayingItem)
					if list[k].NowPlayingItem != nil {
						list[k].NowPlayingItem.Tags[0] = "mutated"
						list[k].NowPlayingItem.ProviderIds["Tmdb"] = "mutated"
						list[k].NowPlayingItem.ImageTags["Primary"] = "mutated"
					}
				}
			}
		}()
	}
	wg.Wait()

	// Stored session must remain completely intact!
	got, ok := mgr.GetSessionByUserDevice(user.ID, client.DeviceID)
	if !ok || got.NowPlayingItem == nil {
		t.Fatal("expected session with NowPlayingItem")
	}
	if got.NowPlayingItem.Path != "/secret/movies/movie.mkv" {
		t.Fatalf("stored NowPlayingItem.Path was polluted: %q", got.NowPlayingItem.Path)
	}
	if got.NowPlayingItem.MediaSources[0].Path != "/secret/movies/movie.mkv" {
		t.Fatalf("stored MediaSources[0].Path was polluted: %q", got.NowPlayingItem.MediaSources[0].Path)
	}
	if got.NowPlayingItem.MediaSources[0].MediaStreams[1].Path != "/secret/movies/movie.srt" {
		t.Fatalf("stored MediaSources[0].MediaStreams[1].Path was polluted: %q", got.NowPlayingItem.MediaSources[0].MediaStreams[1].Path)
	}
	if got.NowPlayingItem.MediaStreams[0].Path != "/secret/movies/movie.srt" {
		t.Fatalf("stored MediaStreams[0].Path was polluted: %q", got.NowPlayingItem.MediaStreams[0].Path)
	}
	if got.NowPlayingItem.Tags[0] != "Tag1" || got.NowPlayingItem.ProviderIds["Tmdb"] != "123" || got.NowPlayingItem.ImageTags["Primary"] != "img1" {
		t.Fatalf("stored slice/map fields were polluted: %+v", got.NowPlayingItem)
	}
	if cloneBaseItemDto(nil) != nil {
		t.Fatal("cloneBaseItemDto(nil) must return nil")
	}
}

func TestReportPlaybackStartAuthoritativeMetadataAndConcurrentThreshold(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	cfg, err := config.Load(filepath.Join(tmp, "data"))
	if err != nil {
		t.Fatal(err)
	}
	store, err := sqlite.Open(cfg.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	userRepo := sqlite.NewUserRepo(store)
	libRepo := sqlite.NewLibraryRepo(store)
	mgr := NewManager(userRepo, cfg, eventbus.New())

	user := &sqlite.UserRecord{
		ID:        guid.New(),
		Name:      "alice",
		Policy:    sqlite.DefaultUserPolicy(false),
		Config:    sqlite.DefaultUserConfig(),
		CreatedAt: time.Now().UTC(),
	}
	if err := userRepo.CreateUser(ctx, user); err != nil {
		t.Fatal(err)
	}

	lib, err := libRepo.CreateLibrary(ctx, "Movies", "movies", []string{tmp})
	if err != nil {
		t.Fatal(err)
	}
	rt := int64(10_000_000_000)
	movieID := guid.New()
	if err := libRepo.UpsertItem(ctx, &sqlite.ItemRecord{
		ID:           movieID,
		LibraryID:    lib.ID,
		ParentID:     lib.ID,
		Type:         "Movie",
		MediaType:    "Video",
		Name:         "Authoritative Title",
		RuntimeTicks: &rt,
		Container:    "mkv",
		Path:         "/movies/real.mkv",
	}); err != nil {
		t.Fatal(err)
	}

	client := auth.ClientInfo{Client: "Test", Device: "PC", DeviceID: "dev-auth"}

	// Spoofed client req.Item MUST be ignored in favor of authoritative server metadata.
	spoofRT := int64(1)
	if err := mgr.ReportPlaybackStart(ctx, user, client, PlaybackStartRequest{
		ItemId: movieID,
		Item: &dto.BaseItemDto{
			Id:           movieID,
			Name:         "Spoofed Evil Title",
			Path:         "/etc/passwd",
			RunTimeTicks: &spoofRT,
		},
	}); err != nil {
		t.Fatalf("ReportPlaybackStart: %v", err)
	}

	sess, ok := mgr.GetSessionByUserDevice(user.ID, client.DeviceID)
	if !ok || sess.NowPlayingItem == nil {
		t.Fatal("expected active session")
	}
	if sess.NowPlayingItem.Name != "Authoritative Title" {
		t.Fatalf("expected server authoritative Name, got %q", sess.NowPlayingItem.Name)
	}
	if sess.NowPlayingItem.Path != "" {
		t.Fatalf("spoofed client Path must not leak into NowPlayingItem, got %q", sess.NowPlayingItem.Path)
	}
	if sess.NowPlayingItem.RunTimeTicks == nil || *sess.NowPlayingItem.RunTimeTicks != rt {
		t.Fatalf("expected authoritative RunTimeTicks=%d, got %v", rt, sess.NowPlayingItem.RunTimeTicks)
	}

	// Concurrent cfg.Update(PlayedThreshold) + ReportPlaybackStopped must not race.
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 30; i++ {
			cfg.Update(func(c *config.Config) {
				if i%2 == 0 {
					c.PlayedThreshold = 0.85
				} else {
					c.PlayedThreshold = 0.95
				}
			})
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 15; i++ {
			_ = mgr.ReportPlaybackStopped(ctx, user, client, PlaybackStoppedRequest{
				ItemId:        movieID,
				PositionTicks: 5_000_000_000,
			})
		}
	}()
	wg.Wait()

	// Nil config helpers safety.
	nilCfgMgr := NewManager(nil, nil, nil)
	if nilCfgMgr.serverID() != "" {
		t.Fatal("expected empty serverID for nil cfg")
	}
	if nilCfgMgr.playedThreshold() != defaultPlayedThreshold {
		t.Fatalf("expected defaultPlayedThreshold for nil cfg, got %v", nilCfgMgr.playedThreshold())
	}
}
