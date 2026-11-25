package playback

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/ambra-media/ambra/internal/api/dto"
	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func intPtr(v int) *int { return &v }

func TestLocaleToTrackLang(t *testing.T) {
	cases := map[string]string{
		"":         "",
		"zh-CN":    "chs",
		"zh_SG":    "chs",
		"zh-Hans":  "chs",
		"zh-TW":    "cht",
		"zh-HK":    "cht",
		"zh-Hant":  "cht",
		"zh":       "chs",
		"en-US":    "eng",
		"ja-JP":    "jpn",
		"ko-KR":    "kor",
		"es-ES":    "spa",
		"de-DE":    "ger",
		"fr-FR":    "fre",
		"pt-BR":    "por",
		"th-TH":    "tha",
		"ru-RU":    "ru",
		"custom":   "custom",
	}
	for in, want := range cases {
		if got := LocaleToTrackLang(in); got != want {
			t.Errorf("LocaleToTrackLang(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSelectDefaultTracksAudioAndSubtitlePriorities(t *testing.T) {
	streams := []dto.MediaStream{
		{Type: "Video", Index: 0, Codec: "h264"},
		{Type: "Audio", Index: 1, Codec: "aac", Language: "eng", Title: "English", IsDefault: true},
		{Type: "Audio", Index: 2, Codec: "aac", Language: "jpn", Title: "Japanese"},
		{Type: "Audio", Index: 3, Codec: "aac", Language: "chi", Title: "国语简体"},
		{Type: "Subtitle", Index: 4, Codec: "pgssub", Language: "eng", Title: "English", IsDefault: true},
		{Type: "Subtitle", Index: 5, Codec: "pgssub", Language: "chi", Title: "简体中文 PGS", SubVariant: "chs"},
		{Type: "Subtitle", Index: 6, Codec: "ass", Language: "chi", Title: "简英双语", SubVariant: "bilingual"},
		{Type: "Subtitle", Index: 7, Codec: "srt", Language: "chi", Title: "繁体中文", SubVariant: "cht"},
		{Type: "Subtitle", Index: 8, Codec: "srt", Language: "chi", Title: "简体外挂", SubVariant: "chs", IsExternal: true},
		{Type: "Subtitle", Index: 9, Codec: "srt", Language: "chi", Title: "强制字幕", IsForced: true},
	}

	// 1. Default user on zh-CN server: audio -> IsDefault (1), subtitle -> external chs (8)
	res := SelectDefaultTracks(TrackSelectionInput{
		Streams:           streams,
		UserConfig:        &dto.UserConfiguration{SubtitleMode: "Default"},
		ServerDefaultLang: "zh-CN",
	})
	if res.AudioStreamIndex == nil || *res.AudioStreamIndex != 1 {
		t.Fatalf("expected default audio 1, got %v", res.AudioStreamIndex)
	}
	if res.SubtitleStreamIndex == nil || *res.SubtitleStreamIndex != 8 {
		t.Fatalf("expected external chs subtitle 8, got %v", res.SubtitleStreamIndex)
	}

	// 2. SubtitlePreference="media" -> picks IsDefault subtitle (4)
	resMedia := SelectDefaultTracks(TrackSelectionInput{
		Streams: streams,
		UserConfig: &dto.UserConfiguration{
			AudioLanguagePreference:    "media",
			SubtitleLanguagePreference: "media",
			SubtitleMode:               "Default",
		},
		ServerDefaultLang: "zh-CN",
	})
	if resMedia.AudioStreamIndex == nil || *resMedia.AudioStreamIndex != 1 {
		t.Fatalf("expected media default audio 1, got %v", resMedia.AudioStreamIndex)
	}
	if resMedia.SubtitleStreamIndex == nil || *resMedia.SubtitleStreamIndex != 4 {
		t.Fatalf("expected media default subtitle 4, got %v", resMedia.SubtitleStreamIndex)
	}

	// 3. AudioLanguagePreference="jpn" & SubtitleLanguagePreference="cht" -> audio 2, subtitle 7 (cht) or external chs?
	// Note: external chs (8) still matches chi; let's verify cht prefers cht over embedded chs when both embedded!
	embeddedSubs := []dto.MediaStream{
		{Type: "Audio", Index: 1, Codec: "aac", Language: "eng", IsDefault: true},
		{Type: "Audio", Index: 2, Codec: "aac", Language: "jpn"},
		{Type: "Subtitle", Index: 5, Codec: "pgssub", Language: "chi", SubVariant: "chs"},
		{Type: "Subtitle", Index: 6, Codec: "ass", Language: "chi", SubVariant: "chs"},
		{Type: "Subtitle", Index: 7, Codec: "srt", Language: "chi", SubVariant: "cht"},
	}
	resJP := SelectDefaultTracks(TrackSelectionInput{
		Streams: embeddedSubs,
		UserConfig: &dto.UserConfiguration{
			AudioLanguagePreference:    "jpn",
			SubtitleLanguagePreference: "cht",
			SubtitleMode:               "Default",
		},
		ServerDefaultLang: "zh-CN",
	})
	if resJP.AudioStreamIndex == nil || *resJP.AudioStreamIndex != 2 {
		t.Fatalf("expected jpn audio 2, got %v", resJP.AudioStreamIndex)
	}
	if resJP.SubtitleStreamIndex == nil || *resJP.SubtitleStreamIndex != 7 {
		t.Fatalf("expected cht subtitle 7, got %v", resJP.SubtitleStreamIndex)
	}

	// 4. Text format beats bitmap PGS for same language (ass 6 > pgssub 5)
	resText := SelectDefaultTracks(TrackSelectionInput{
		Streams: embeddedSubs,
		UserConfig: &dto.UserConfiguration{
			SubtitleLanguagePreference: "chs",
			SubtitleMode:               "Default",
		},
		ServerDefaultLang: "zh-CN",
	})
	if resText.SubtitleStreamIndex == nil || *resText.SubtitleStreamIndex != 6 {
		t.Fatalf("expected text chs ass subtitle 6 over pgssub 5, got %v", resText.SubtitleStreamIndex)
	}

	// 5. SubtitleMode == "None" or pref == "off" disables subtitle
	resNone := SelectDefaultTracks(TrackSelectionInput{
		Streams:           streams,
		UserConfig:        &dto.UserConfiguration{SubtitleMode: "None"},
		ServerDefaultLang: "zh-CN",
	})
	if resNone.SubtitleStreamIndex != nil {
		t.Fatalf("expected nil subtitle for SubtitleMode=None, got %d", *resNone.SubtitleStreamIndex)
	}
	resOff := SelectDefaultTracks(TrackSelectionInput{
		Streams:           streams,
		UserConfig:        &dto.UserConfiguration{SubtitleLanguagePreference: "off"},
		ServerDefaultLang: "zh-CN",
	})
	if resOff.SubtitleStreamIndex != nil {
		t.Fatalf("expected nil subtitle for pref=off, got %d", *resOff.SubtitleStreamIndex)
	}

	// 6. Per-item memory overrides preference; -1 explicitly disables subtitle
	resMem := SelectDefaultTracks(TrackSelectionInput{
		Streams: streams,
		UserConfig: &dto.UserConfiguration{
			RememberAudioSelections:    true,
			RememberSubtitleSelections: true,
			SubtitleLanguagePreference: "auto",
		},
		UserData: &sqlite.UserItemDataRecord{
			AudioStreamIndex:    intPtr(2),
			SubtitleStreamIndex: intPtr(-1),
		},
		ServerDefaultLang: "zh-CN",
	})
	if resMem.AudioStreamIndex == nil || *resMem.AudioStreamIndex != 2 {
		t.Fatalf("expected remembered audio 2, got %v", resMem.AudioStreamIndex)
	}
	if resMem.SubtitleStreamIndex != nil {
		t.Fatalf("expected remembered subtitle -1 to disable subtitles, got %d", *resMem.SubtitleStreamIndex)
	}

	resMemSub := SelectDefaultTracks(TrackSelectionInput{
		Streams: streams,
		UserConfig: &dto.UserConfiguration{
			RememberSubtitleSelections: true,
			SubtitleLanguagePreference: "auto",
		},
		UserData: &sqlite.UserItemDataRecord{
			SubtitleStreamIndex: intPtr(7),
		},
		ServerDefaultLang: "zh-CN",
	})
	if resMemSub.SubtitleStreamIndex == nil || *resMemSub.SubtitleStreamIndex != 7 {
		t.Fatalf("expected remembered subtitle 7, got %v", resMemSub.SubtitleStreamIndex)
	}

	// 7. OnlyForced mode picks forced subtitle (9)
	resForced := SelectDefaultTracks(TrackSelectionInput{
		Streams:           streams,
		UserConfig:        &dto.UserConfiguration{SubtitleMode: "OnlyForced"},
		ServerDefaultLang: "zh-CN",
	})
	if resForced.SubtitleStreamIndex == nil || *resForced.SubtitleStreamIndex != 9 {
		t.Fatalf("expected forced subtitle 9, got %v", resForced.SubtitleStreamIndex)
	}

	// 8. Smart mode: Chinese audio + Chinese target -> only forced (9); English audio + Chinese target -> full sub (8)
	resSmartSame := SelectDefaultTracks(TrackSelectionInput{
		Streams: streams,
		UserConfig: &dto.UserConfiguration{
			AudioLanguagePreference:    "auto",
			SubtitleLanguagePreference: "auto",
			SubtitleMode:               "Smart",
		},
		ServerDefaultLang: "zh-CN",
	})
	if resSmartSame.AudioStreamIndex == nil || *resSmartSame.AudioStreamIndex != 3 {
		t.Fatalf("expected auto zh-CN audio 3, got %v", resSmartSame.AudioStreamIndex)
	}
	if resSmartSame.SubtitleStreamIndex == nil || *resSmartSame.SubtitleStreamIndex != 9 {
		t.Fatalf("expected Smart mode with Chinese audio to select forced subtitle 9, got %v", resSmartSame.SubtitleStreamIndex)
	}

	resSmartDiff := SelectDefaultTracks(TrackSelectionInput{
		Streams: streams,
		UserConfig: &dto.UserConfiguration{
			AudioLanguagePreference:    "eng",
			SubtitleLanguagePreference: "chs",
			SubtitleMode:               "Smart",
		},
		ServerDefaultLang: "zh-CN",
	})
	if resSmartDiff.SubtitleStreamIndex == nil || *resSmartDiff.SubtitleStreamIndex != 8 {
		t.Fatalf("expected Smart mode with English audio to select chs subtitle 8, got %v", resSmartDiff.SubtitleStreamIndex)
	}

	// 9. Always mode falls back to first subtitle when language doesn't match and no IsDefault
	noDefaultSubs := []dto.MediaStream{
		{Type: "Audio", Index: 1, Codec: "aac", Language: "eng"},
		{Type: "Subtitle", Index: 5, Codec: "srt", Language: "spa"},
	}
	resAlways := SelectDefaultTracks(TrackSelectionInput{
		Streams: noDefaultSubs,
		UserConfig: &dto.UserConfiguration{
			SubtitleLanguagePreference: "jpn",
			SubtitleMode:               "Always",
		},
	})
	if resAlways.SubtitleStreamIndex == nil || *resAlways.SubtitleStreamIndex != 5 {
		t.Fatalf("expected Always mode fallback to first subtitle 5, got %v", resAlways.SubtitleStreamIndex)
	}
	if resAlways.AudioStreamIndex == nil || *resAlways.AudioStreamIndex != 1 {
		t.Fatalf("expected first audio fallback 1, got %v", resAlways.AudioStreamIndex)
	}

	// 10. Nil safety & empty streams
	ApplyTrackSelection(nil, nil, nil, "zh-CN")
	resEmpty := SelectDefaultTracks(TrackSelectionInput{})
	if resEmpty.AudioStreamIndex != nil || resEmpty.SubtitleStreamIndex != nil {
		t.Fatalf("expected nil indices for empty streams, got %+v", resEmpty)
	}
}

func TestEngineDecideUsesUserConfigAndMemory(t *testing.T) {
	dir := t.TempDir()
	store, err := sqlite.Open(filepath.Join(dir, "pb_track.db"))
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
		ID:      "user-track-1",
		Name:    "viewer",
		IsAdmin: false,
		Policy:  sqlite.DefaultUserPolicy(false),
		Config:  sqlite.DefaultUserConfig(),
	}
	u.Config.AudioLanguagePreference = "jpn"
	u.Config.SubtitleLanguagePreference = "auto"
	if err := userRepo.CreateUser(ctx, u); err != nil {
		t.Fatal(err)
	}

	lib, err := libRepo.CreateLibrary(ctx, "Movies", "movies", []string{dir})
	if err != nil {
		t.Fatal(err)
	}
	item := &sqlite.ItemRecord{
		ID: "item1", LibraryID: lib.ID, Type: "Movie", MediaType: "Video",
		Name: "Anime Movie", Container: "mkv", Path: filepath.Join(dir, "anime.mkv"),
	}
	if err := libRepo.UpsertItem(ctx, item); err != nil {
		t.Fatal(err)
	}
	if err := libRepo.ReplaceMediaSourcesAndStreams(ctx, item.ID, []sqlite.MediaSourceRecord{{
		ID: "src1", ItemID: item.ID, Name: "Main", Path: item.Path, Protocol: "File", Container: "mkv",
		SupportsDirectPlay: true, SupportsDirectStream: true, SupportsTranscoding: true,
		Streams: []sqlite.MediaStreamRecord{
			{MediaSourceID: "src1", ItemID: item.ID, StreamIndex: 0, StreamType: "Video", Codec: "h264"},
			{MediaSourceID: "src1", ItemID: item.ID, StreamIndex: 1, StreamType: "Audio", Codec: "aac", Language: "eng", IsDefault: true},
			{MediaSourceID: "src1", ItemID: item.ID, StreamIndex: 2, StreamType: "Audio", Codec: "aac", Language: "jpn"},
			{MediaSourceID: "src1", ItemID: item.ID, StreamIndex: 3, StreamType: "Subtitle", Codec: "srt", Language: "eng", IsDefault: true},
			{MediaSourceID: "src1", ItemID: item.ID, StreamIndex: 4, StreamType: "Subtitle", Codec: "ass", Language: "chi", Title: "简体中文", SubVariant: "chs"},
		},
	}}); err != nil {
		t.Fatal(err)
	}

	eng := New(libRepo, nil, cfg)
	eng.SetUserRepo(userRepo)

	resp, _, err := eng.Decide(ctx, item.ID, PlaybackInfoRequest{UserID: u.ID})
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	ms := resp.MediaSources[0]
	if ms.DefaultAudioStreamIndex == nil || *ms.DefaultAudioStreamIndex != 2 {
		t.Fatalf("expected user preferred jpn audio 2, got %v", ms.DefaultAudioStreamIndex)
	}
	if ms.DefaultSubtitleStreamIndex == nil || *ms.DefaultSubtitleStreamIndex != 4 {
		t.Fatalf("expected auto zh-CN subtitle 4, got %v", ms.DefaultSubtitleStreamIndex)
	}

	// Persist per-item subtitle_stream_index = -1 (user explicitly turned off subtitles)
	if _, err := userRepo.UpsertPlaybackProgress(ctx, u.ID, item.ID, 120_000_000, false, intPtr(1), intPtr(-1)); err != nil {
		t.Fatal(err)
	}
	resp2, _, err := eng.Decide(ctx, item.ID, PlaybackInfoRequest{UserID: u.ID})
	if err != nil {
		t.Fatalf("Decide after memory: %v", err)
	}
	ms2 := resp2.MediaSources[0]
	if ms2.DefaultAudioStreamIndex == nil || *ms2.DefaultAudioStreamIndex != 1 {
		t.Fatalf("expected remembered audio 1, got %v", ms2.DefaultAudioStreamIndex)
	}
	if ms2.DefaultSubtitleStreamIndex != nil {
		t.Fatalf("expected remembered subtitle -1 to yield nil default subtitle, got %d", *ms2.DefaultSubtitleStreamIndex)
	}
}
