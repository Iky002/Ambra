package sqlite

import (
	"context"
	"fmt"
	"testing"
)

func TestChapterImageTagDeterministic(t *testing.T) {
	_, repo := openParityTestStore(t)
	ctx := context.Background()

	lib, err := repo.CreateLibrary(ctx, "Movies", "movies", []string{"/movies"})
	if err != nil {
		t.Fatal(err)
	}
	mov := &ItemRecord{
		ID: "ch-mov-1", LibraryID: lib.ID, ParentID: lib.ID,
		Type: "Movie", MediaType: "Video", Name: "Chapter Test", Etag: "fixed-etag-1234",
		Extra: ItemExtra{
			Chapters: []ChapterRecord{
				{StartPositionTicks: 0, Name: "Intro"},
				{StartPositionTicks: 600000000, Name: "Act 1"},
			},
		},
	}
	if err := repo.UpsertItem(ctx, mov); err != nil {
		t.Fatal(err)
	}

	dto1, err := repo.GetItemByID(ctx, "srv1", "", mov.ID)
	if err != nil {
		t.Fatal(err)
	}
	dto2, err := repo.GetItemByID(ctx, "srv1", "", mov.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(dto1.Chapters) != 2 {
		t.Fatalf("expected 2 chapters, got %d", len(dto1.Chapters))
	}
	if len(dto1.Chapters[0].ImageTag) != 12 || len(dto1.Chapters[1].ImageTag) != 12 {
		t.Fatalf("expected 12-char chapter ImageTags, got %q and %q", dto1.Chapters[0].ImageTag, dto1.Chapters[1].ImageTag)
	}
	if dto1.Chapters[0].ImageTag == dto1.Chapters[1].ImageTag {
		t.Fatalf("expected distinct ImageTags for different chapter indices, both were %q", dto1.Chapters[0].ImageTag)
	}
	if dto1.Chapters[0].ImageTag != dto2.Chapters[0].ImageTag {
		t.Fatalf("expected deterministic ImageTag across reads: %q vs %q", dto1.Chapters[0].ImageTag, dto2.Chapters[0].ImageTag)
	}
}

func TestMarkPlayedCascadeNoDoubleCountAndBoxSetSeriesRecursion(t *testing.T) {
	st, repo := openParityTestStore(t)
	ctx := context.Background()

	lib, err := repo.CreateLibrary(ctx, "TV", "tvshows", []string{"/tv"})
	if err != nil {
		t.Fatal(err)
	}
	u := &UserRecord{Name: "cascade-v090"}
	if err := NewUserRepo(st).CreateUser(ctx, u); err != nil {
		t.Fatal(err)
	}

	// BoxSet -> Series -> Season -> Ep1 (already played), Ep2 (unplayed)
	series := &ItemRecord{ID: "bsser1", LibraryID: lib.ID, ParentID: lib.ID, Type: "Series", IsFolder: true, Name: "Nested Show"}
	season := &ItemRecord{ID: "bssea1", LibraryID: lib.ID, ParentID: "bsser1", SeriesID: "bsser1", Type: "Season", IsFolder: true, Name: "Season 1"}
	ep1 := &ItemRecord{ID: "bsep1", LibraryID: lib.ID, ParentID: "bssea1", SeriesID: "bsser1", SeasonID: "bssea1", Type: "Episode", MediaType: "Video", Name: "Ep 1"}
	ep2 := &ItemRecord{ID: "bsep2", LibraryID: lib.ID, ParentID: "bssea1", SeriesID: "bsser1", SeasonID: "bssea1", Type: "Episode", MediaType: "Video", Name: "Ep 2"}
	for _, rec := range []*ItemRecord{series, season, ep1, ep2} {
		if err := repo.UpsertItem(ctx, rec); err != nil {
			t.Fatal(err)
		}
	}
	boxSet, err := repo.CreateCollection(ctx, "Sci-Fi Universe", "", []string{series.ID})
	if err != nil {
		t.Fatal(err)
	}

	// Mark ep1 played first -> play_count = 1
	if ud, err := repo.MarkPlayed(ctx, u.ID, ep1.ID); err != nil || ud.PlayCount != 1 {
		t.Fatalf("MarkPlayed ep1: ud=%+v err=%v", ud, err)
	}

	// Mark BoxSet played -> recurses BoxSet -> Series -> Season -> Ep1, Ep2
	if _, err := repo.MarkPlayed(ctx, u.ID, boxSet.ID); err != nil {
		t.Fatalf("MarkPlayed boxSet: %v", err)
	}

	// ep1 was ALREADY played: play_count must remain 1 (NOT incremented to 2!)
	udEp1, _ := repo.GetUserItemData(ctx, u.ID, ep1.ID)
	if !udEp1.Played || udEp1.PlayCount != 1 {
		t.Fatalf("expected already-played ep1 to keep PlayCount=1, got %+v", udEp1)
	}

	// ep2, season, series must now be played with play_count = 1
	for _, id := range []string{series.ID, season.ID, ep2.ID} {
		ud, err := repo.GetUserItemData(ctx, u.ID, id)
		if err != nil || !ud.Played || ud.PlayCount != 1 {
			t.Fatalf("expected %s played with PlayCount=1 after BoxSet MarkPlayed, got %+v err=%v", id, ud, err)
		}
	}

	// Mark BoxSet unplayed -> recurses all the way down to episodes
	if _, err := repo.MarkUnplayed(ctx, u.ID, boxSet.ID); err != nil {
		t.Fatalf("MarkUnplayed boxSet: %v", err)
	}
	for _, id := range []string{series.ID, season.ID, ep1.ID, ep2.ID} {
		ud, err := repo.GetUserItemData(ctx, u.ID, id)
		if err != nil || ud.Played {
			t.Fatalf("expected %s unplayed after BoxSet MarkUnplayed, got %+v err=%v", id, ud, err)
		}
	}
}

func TestBatchInChunkingLargeLibrary(t *testing.T) {
	_, repo := openParityTestStore(t)
	ctx := context.Background()

	lib, err := repo.CreateLibrary(ctx, "Large Movies", "movies", []string{"/large"})
	if err != nil {
		t.Fatal(err)
	}

	const count = 450 // > sqliteInChunkSize (400)
	for i := 0; i < count; i++ {
		id := fmt.Sprintf("bulkitem%04d", i)
		if err := repo.UpsertItem(ctx, &ItemRecord{
			ID: id, LibraryID: lib.ID, ParentID: lib.ID,
			Type: "Movie", MediaType: "Video", Name: fmt.Sprintf("Movie %04d", i),
		}); err != nil {
			t.Fatal(err)
		}
		if i == 0 || i == count-1 {
			_ = repo.UpsertImage(ctx, &ImageRecord{ItemID: id, ImageType: "Primary", ImageIndex: 0, Path: "/p.jpg", Tag: "tag-" + id})
			_ = repo.ReplaceItemTaxonomies(ctx, id, "genre", []string{"Action"})
			_ = repo.ReplaceItemPeople(ctx, id, []PersonInput{{Name: "Actor One", Type: "Actor"}})
		}
	}

	items, total, err := repo.QueryItems(ctx, ItemFilter{
		ServerID:         "srv1",
		LibraryID:        lib.ID,
		IncludeItemTypes: []string{"Movie"},
		Fields:           []string{"Genres", "Studios", "People"},
		Limit:            500,
	})
	if err != nil {
		t.Fatalf("QueryItems on 450 items failed: %v", err)
	}
	if total != count || len(items) != count {
		t.Fatalf("expected %d items, got total=%d len=%d", count, total, len(items))
	}
	// Verify both first chunk (idx 0) and second chunk (idx 449) hydrated properly
	for _, idx := range []int{0, count - 1} {
		it := items[idx]
		if it.ImageTags["Primary"] != "tag-"+it.Id {
			t.Fatalf("item %d missing Primary image tag: %+v", idx, it.ImageTags)
		}
		if len(it.Genres) != 1 || it.Genres[0] != "Action" {
			t.Fatalf("item %d missing Genres: %+v", idx, it.Genres)
		}
		if len(it.People) != 1 || it.People[0].Name != "Actor One" {
			t.Fatalf("item %d missing People: %+v", idx, it.People)
		}
	}
}

func TestGetItemExtrasAndPosterWallExclusion(t *testing.T) {
	_, repo := openParityTestStore(t)
	ctx := context.Background()

	lib, err := repo.CreateLibrary(ctx, "Movies", "movies", []string{"/movies"})
	if err != nil {
		t.Fatal(err)
	}
	movie := &ItemRecord{ID: "exmov", LibraryID: lib.ID, ParentID: lib.ID, Type: "Movie", MediaType: "Video", Name: "Dune"}
	trailer := &ItemRecord{ID: "extrl", LibraryID: lib.ID, ParentID: movie.ID, Type: "Video", MediaType: "Video", Name: "Trailer 1", Extra: ItemExtra{ExtraType: "Trailer"}}
	theme := &ItemRecord{ID: "exthm", LibraryID: lib.ID, ParentID: movie.ID, Type: "Audio", MediaType: "Audio", Name: "Theme", Extra: ItemExtra{ExtraType: "ThemeSong"}}
	for _, rec := range []*ItemRecord{movie, trailer, theme} {
		if err := repo.UpsertItem(ctx, rec); err != nil {
			t.Fatal(err)
		}
	}

	// Normal QueryItems must exclude extras
	all, total, err := repo.QueryItems(ctx, ItemFilter{ServerID: "srv1", LibraryID: lib.ID})
	if err != nil || total != 1 || len(all) != 1 || all[0].Id != movie.ID {
		t.Fatalf("expected only main movie in QueryItems, got total=%d items=%+v err=%v", total, all, err)
	}

	// ItemCounts must exclude extras (SongCount should be 0 despite ThemeSong Audio item)
	counts, err := repo.GetItemCounts(ctx)
	if err != nil || counts.MovieCount != 1 || counts.SongCount != 0 {
		t.Fatalf("expected MovieCount=1 SongCount=0, got %+v err=%v", counts, err)
	}

	// GetItemExtras returns matching extras
	trailers, err := repo.GetItemExtras(ctx, "srv1", "", movie.ID, []string{"Trailer"})
	if err != nil || len(trailers) != 1 || trailers[0].Id != trailer.ID || trailers[0].ExtraType != "Trailer" {
		t.Fatalf("unexpected GetItemExtras(Trailer): %+v err=%v", trailers, err)
	}
	themes, err := repo.GetItemExtras(ctx, "srv1", "", movie.ID, []string{"ThemeSong"})
	if err != nil || len(themes) != 1 || themes[0].Id != theme.ID || themes[0].ExtraType != "ThemeSong" {
		t.Fatalf("unexpected GetItemExtras(ThemeSong): %+v err=%v", themes, err)
	}
}
