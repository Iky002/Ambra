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
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestLibraryV0107AuditM9SearchHintsAndViewCoverParental(t *testing.T) {
	tmp := t.TempDir()
	st, err := sqlite.Open(filepath.Join(tmp, "lib_v0107.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	ctx := context.Background()
	repo := sqlite.NewLibraryRepo(st)
	userRepo := sqlite.NewUserRepo(st)
	cfg := config.Default(tmp)

	lib, _ := repo.CreateLibrary(ctx, "Movies", "movies", []string{tmp})

	alice := &sqlite.UserRecord{ID: guid.New(), Name: "alice", Policy: sqlite.DefaultUserPolicy(false), Config: sqlite.DefaultUserConfig()}
	bob := &sqlite.UserRecord{ID: guid.New(), Name: "bob", Policy: sqlite.DefaultUserPolicy(false), Config: sqlite.DefaultUserConfig()}
	maxG := 1
	kid := &sqlite.UserRecord{
		ID: guid.New(), Name: "kid",
		Policy: dto.UserPolicy{
			EnableAllFolders:  true,
			MaxParentalRating: &maxG,
			BlockedTags:       []string{"adult"},
		},
		Config: sqlite.DefaultUserConfig(),
	}
	alice.Policy.EnableAllFolders = true
	bob.Policy.EnableAllFolders = true
	for _, u := range []*sqlite.UserRecord{alice, bob, kid} {
		_ = userRepo.CreateUser(ctx, u)
	}

	// Create 3 Bob private playlists matching "Alpha" AND 2 Movies matching "Alpha"
	for _, name := range []string{"Alpha Private 1", "Alpha Private 2", "Alpha Private 3"} {
		pl, err := repo.CreatePlaylist(ctx, name, "Video", bob.ID, nil)
		if err != nil {
			t.Fatal(err)
		}
		_ = repo.UpsertSearchFTS(ctx, pl.ID, name, name, "", "", "")
	}
	movie1 := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: lib.ID, ParentID: lib.ID,
		Type: "Movie", MediaType: "Video", Name: "Alpha One", OfficialRating: "R",
	}
	movie2 := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: lib.ID, ParentID: lib.ID,
		Type: "Movie", MediaType: "Video", Name: "Alpha Two", OfficialRating: "G",
	}
	for _, m := range []*sqlite.ItemRecord{movie1, movie2} {
		_ = repo.UpsertItem(ctx, m)
		_ = repo.UpsertSearchFTS(ctx, m.ID, m.Name, m.Name, "", "", "")
	}
	// Give R-rated movie1 a Backdrop tag and G-rated movie2 a Primary tag (no backdrop on movie2)
	_ = repo.UpsertImage(ctx, &sqlite.ImageRecord{
		ItemID: movie1.ID, ImageType: "Backdrop", ImageIndex: 0, Path: filepath.Join(tmp, "r_backdrop.jpg"), Tag: "r-rated-tag",
	})
	_ = repo.UpsertImage(ctx, &sqlite.ImageRecord{
		ItemID: movie2.ID, ImageType: "Primary", ImageIndex: 0, Path: filepath.Join(tmp, "g_primary.jpg"), Tag: "g-safe-tag",
	})

	h := New(repo, nil, cfg)
	h.SetUserRepo(userRepo)
	h.SetStore(st)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux, AuthFuncs{})

	doAs := func(u *sqlite.UserRecord, target string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, target, nil)
		req = req.WithContext(middleware.WithAuthContext(req.Context(), &middleware.AuthContext{User: u}))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}

	// 1. [M-9] SearchHints with Limit=2 must NOT have Bob's 3 private playlists crowd out Alice's 2 movies or leak count
	recHints := doAs(alice, "/Search/Hints?SearchTerm=Alpha&Limit=2")
	if recHints.Code != http.StatusOK {
		t.Fatalf("SearchHints got %d body=%s", recHints.Code, recHints.Body.String())
	}
	var hintRes dto.SearchHintResult
	_ = json.Unmarshal(recHints.Body.Bytes(), &hintRes)
	if len(hintRes.SearchHints) != 2 || hintRes.TotalRecordCount != 2 {
		t.Fatalf("M-9 expected 2 visible hints and TotalRecordCount=2, got len=%d total=%d hints=%+v",
			len(hintRes.SearchHints), hintRes.TotalRecordCount, hintRes.SearchHints)
	}
	for _, sh := range hintRes.SearchHints {
		if sh.Type == "Playlist" {
			t.Fatalf("M-9 leaked Bob's private playlist in SearchHints: %+v", sh)
		}
	}

	// 2. [M-9] libraryToViewDto must respect parental policy so kid gets "g-safe-tag", NOT "r-rated-tag"
	recViews := doAs(kid, "/Users/"+kid.ID+"/Views")
	if recViews.Code != http.StatusOK {
		t.Fatalf("kid Views got %d", recViews.Code)
	}
	var viewsRes dto.QueryResult[dto.BaseItemDto]
	_ = json.Unmarshal(recViews.Body.Bytes(), &viewsRes)
	if len(viewsRes.Items) != 1 {
		t.Fatalf("expected 1 view, got %d", len(viewsRes.Items))
	}
	if tag := viewsRes.Items[0].ImageTags["Primary"]; tag != "g-safe-tag" {
		t.Fatalf("M-9 kid library view fallback cover leaked R-rated tag: got %q, want %q", tag, "g-safe-tag")
	}

	// Also verify GET /Items/{LibId} for kid respects parental policy on fallback cover
	recLibItem := doAs(kid, "/Items/"+lib.ID)
	if recLibItem.Code != http.StatusOK {
		t.Fatalf("kid GET /Items/{libID} got %d", recLibItem.Code)
	}
	var libDto dto.BaseItemDto
	_ = json.Unmarshal(recLibItem.Body.Bytes(), &libDto)
	if libDto.ImageTags["Primary"] != "g-safe-tag" {
		t.Fatalf("M-9 kid GET /Items/{libID} fallback cover got %q, want %q", libDto.ImageTags["Primary"], "g-safe-tag")
	}
}
