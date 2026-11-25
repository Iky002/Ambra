package sqlite

import (
	"context"
	"path/filepath"
	"testing"
)

func TestMediaSourceRecordsToDTODefaultTrackIndices(t *testing.T) {
	dtos := MediaSourceRecordsToDTO([]MediaSourceRecord{
		{
			ID: "s1",
			Streams: []MediaStreamRecord{
				{StreamIndex: 0, StreamType: "Video", Codec: "h264"},
				{StreamIndex: 1, StreamType: "Audio", Codec: "aac", Language: "eng"},
				{StreamIndex: 2, StreamType: "Audio", Codec: "aac", Language: "chi", IsDefault: true},
				{StreamIndex: 3, StreamType: "Subtitle", Codec: "srt", Language: "eng"},
				{StreamIndex: 4, StreamType: "Subtitle", Codec: "ass", Language: "chi", IsDefault: true},
			},
		},
		{
			ID: "s2",
			Streams: []MediaStreamRecord{
				{StreamIndex: 0, StreamType: "Video", Codec: "h264"},
				{StreamIndex: 1, StreamType: "Audio", Codec: "aac", Language: "jpn"},
				{StreamIndex: 2, StreamType: "Subtitle", Codec: "srt", Language: "chi"},
			},
		},
	})

	if dtos[0].DefaultAudioStreamIndex == nil || *dtos[0].DefaultAudioStreamIndex != 2 {
		t.Fatalf("expected s1 DefaultAudioStreamIndex=2, got %v", dtos[0].DefaultAudioStreamIndex)
	}
	if dtos[0].DefaultSubtitleStreamIndex == nil || *dtos[0].DefaultSubtitleStreamIndex != 4 {
		t.Fatalf("expected s1 DefaultSubtitleStreamIndex=4, got %v", dtos[0].DefaultSubtitleStreamIndex)
	}
	if dtos[1].DefaultAudioStreamIndex == nil || *dtos[1].DefaultAudioStreamIndex != 1 {
		t.Fatalf("expected s2 fallback first audio=1, got %v", dtos[1].DefaultAudioStreamIndex)
	}
	if dtos[1].DefaultSubtitleStreamIndex != nil {
		t.Fatalf("expected s2 nil DefaultSubtitleStreamIndex when no IsDefault subtitle, got %d", *dtos[1].DefaultSubtitleStreamIndex)
	}
}

func TestUpsertPlaybackProgressSubtitleMinusOnePersistence(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "sub_minus_one.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	userRepo := NewUserRepo(store)
	libRepo := NewLibraryRepo(store)
	ctx := context.Background()

	u := &UserRecord{ID: "u-sub", Name: "alice", Policy: DefaultUserPolicy(false), Config: DefaultUserConfig()}
	if err := userRepo.CreateUser(ctx, u); err != nil {
		t.Fatal(err)
	}
	lib, err := libRepo.CreateLibrary(ctx, "Movies", "movies", []string{"/m"})
	if err != nil {
		t.Fatal(err)
	}
	if err := libRepo.UpsertItem(ctx, &ItemRecord{ID: "i1", LibraryID: lib.ID, Type: "Movie", Name: "Test"}); err != nil {
		t.Fatal(err)
	}

	sub3 := 3
	audio1 := 1
	if _, err := userRepo.UpsertPlaybackProgress(ctx, u.ID, "i1", 10_000_000, false, &audio1, &sub3); err != nil {
		t.Fatal(err)
	}
	ud, err := userRepo.GetUserItemData(ctx, u.ID, "i1")
	if err != nil || ud.SubtitleStreamIndex == nil || *ud.SubtitleStreamIndex != 3 {
		t.Fatalf("expected saved subtitle 3, got ud=%+v err=%v", ud, err)
	}

	// Explicitly turning off subtitles persists -1
	subOff := -1
	if _, err := userRepo.UpsertPlaybackProgress(ctx, u.ID, "i1", 20_000_000, false, &audio1, &subOff); err != nil {
		t.Fatal(err)
	}
	ud2, err := userRepo.GetUserItemData(ctx, u.ID, "i1")
	if err != nil || ud2.SubtitleStreamIndex == nil || *ud2.SubtitleStreamIndex != -1 {
		t.Fatalf("expected persisted subtitle -1, got ud=%+v err=%v", ud2, err)
	}

	// Subsequent nil update preserves -1
	if _, err := userRepo.UpsertPlaybackProgress(ctx, u.ID, "i1", 30_000_000, false, nil, nil); err != nil {
		t.Fatal(err)
	}
	ud3, err := userRepo.GetUserItemData(ctx, u.ID, "i1")
	if err != nil || ud3.SubtitleStreamIndex == nil || *ud3.SubtitleStreamIndex != -1 {
		t.Fatalf("expected preserved subtitle -1 after nil update, got ud=%+v err=%v", ud3, err)
	}
}
