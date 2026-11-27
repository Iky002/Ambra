package sqlite

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/ambra-media/ambra/internal/pkg/guid"
)

func TestGetMovieRecommendations_PersonalizedAndPolicy(t *testing.T) {
	tmp := t.TempDir()
	store, err := Open(filepath.Join(tmp, "recs.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	ctx := context.Background()
	repo := NewLibraryRepo(store)
	libA, _ := repo.CreateLibrary(ctx, "LibA", "movies", []string{"/a"})
	libB, _ := repo.CreateLibrary(ctx, "LibB", "movies", []string{"/b"})

	// Seed 2 Sci-Fi movies in libA, 1 R-rated Sci-Fi movie in libA, 1 Sci-Fi movie in libB,
	// and 3 Comedy movies in libA so Comedy is the top library genre by count.
	watchedSciFi := &ItemRecord{ID: guid.New(), LibraryID: libA.ID, ParentID: libA.ID, Type: "Movie", MediaType: "Video", Name: "Arrival", OfficialRating: "PG-13"}
	recSciFi := &ItemRecord{ID: guid.New(), LibraryID: libA.ID, ParentID: libA.ID, Type: "Movie", MediaType: "Video", Name: "Contact", OfficialRating: "PG"}
	blockedSciFi := &ItemRecord{ID: guid.New(), LibraryID: libA.ID, ParentID: libA.ID, Type: "Movie", MediaType: "Video", Name: "Alien R", OfficialRating: "R"}
	otherLibSciFi := &ItemRecord{ID: guid.New(), LibraryID: libB.ID, ParentID: libB.ID, Type: "Movie", MediaType: "Video", Name: "OtherLib SciFi", OfficialRating: "PG"}
	c1 := &ItemRecord{ID: guid.New(), LibraryID: libA.ID, ParentID: libA.ID, Type: "Movie", MediaType: "Video", Name: "Comedy 1", OfficialRating: "G"}
	c2 := &ItemRecord{ID: guid.New(), LibraryID: libA.ID, ParentID: libA.ID, Type: "Movie", MediaType: "Video", Name: "Comedy 2", OfficialRating: "G"}
	c3 := &ItemRecord{ID: guid.New(), LibraryID: libA.ID, ParentID: libA.ID, Type: "Movie", MediaType: "Video", Name: "Comedy 3", OfficialRating: "G"}

	for _, it := range []*ItemRecord{watchedSciFi, recSciFi, blockedSciFi, otherLibSciFi, c1, c2, c3} {
		if err := repo.UpsertItem(ctx, it); err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []string{watchedSciFi.ID, recSciFi.ID, blockedSciFi.ID, otherLibSciFi.ID} {
		_ = repo.ReplaceItemTaxonomies(ctx, id, "genre", []string{"Sci-Fi"})
	}
	for _, id := range []string{c1.ID, c2.ID, c3.ID} {
		_ = repo.ReplaceItemTaxonomies(ctx, id, "genre", []string{"Comedy"})
	}

	userRepo := NewUserRepo(store)
	userWithHistory := &UserRecord{
		ID:     guid.New(),
		Name:   "history_user",
		Policy: DefaultUserPolicy(false),
		Config: DefaultUserConfig(),
	}
	if err := userRepo.CreateUser(ctx, userWithHistory); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.MarkPlayed(ctx, userWithHistory.ID, watchedSciFi.ID); err != nil {
		t.Fatal(err)
	}

	// 1. Personalized: first category should be based on watchedSciFi ("Arrival"),
	// excluding watchedSciFi itself, blockedSciFi (R > PG-13=7), and otherLibSciFi (libB).
	maxPG13 := 7
	cats, err := repo.GetMovieRecommendationsWithPolicy(ctx, "srv", userWithHistory.ID, 5, 8, nil, []string{libA.ID}, &maxPG13, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(cats) == 0 {
		t.Fatalf("expected non-empty recommendations")
	}
	if cats[0].BaselineItemName != "Arrival" {
		t.Fatalf("expected first personalized category BaselineItemName=Arrival, got %q", cats[0].BaselineItemName)
	}
	if len(cats[0].Items) != 1 || cats[0].Items[0].Id != recSciFi.ID {
		t.Fatalf("expected only Contact in Arrival category, got %+v", cats[0].Items)
	}

	// 2. Fallback for user without history: top library genre should be Comedy (count=3).
	freshUser := guid.New()
	fallbackCats, err := repo.GetMovieRecommendationsWithPolicy(ctx, "srv", freshUser, 2, 8, nil, []string{libA.ID}, &maxPG13, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(fallbackCats) == 0 || fallbackCats[0].BaselineItemName != "Comedy" {
		t.Fatalf("expected fallback top genre Comedy, got %+v", fallbackCats)
	}
}
