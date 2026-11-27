package library

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ambra-media/ambra/internal/api/dto"
	"github.com/ambra-media/ambra/internal/api/middleware"
	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/core/scanner"
	"github.com/ambra-media/ambra/internal/pkg/eventbus"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/plugins"
	"github.com/ambra-media/ambra/internal/plugins/nfo"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestAuditV0112_LiveTvViewsMountValidationMediaUpdatedAnd319Filters(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	dataDir := filepath.Join(tmp, "data")
	moviesDir := filepath.Join(tmp, "movies")
	otherDir := filepath.Join(tmp, "other-movies")
	liveDir := filepath.Join(tmp, "livetv")
	for _, d := range []string{dataDir, moviesDir, otherDir, liveDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	store, err := sqlite.Open(filepath.Join(dataDir, "lib_v0112.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	repo := sqlite.NewLibraryRepo(store)
	userRepo := sqlite.NewUserRepo(store)
	cfg := config.Default(dataDir)
	reg := plugins.NewRegistry()
	reg.Register(nfo.New())
	sc := scanner.New(repo, reg, eventbus.New(), cfg)

	movLib, _ := repo.CreateLibrary(ctx, "Movies", "movies", []string{moviesDir})
	otherLib, _ := repo.CreateLibrary(ctx, "OtherMovies", "movies", []string{otherDir})
	liveLib, _ := repo.CreateLibrary(ctx, "LiveTV", "livetv", []string{liveDir})

	admin := &sqlite.UserRecord{ID: guid.New(), Name: "admin_l", IsAdmin: true, Policy: sqlite.DefaultUserPolicy(true), Config: sqlite.DefaultUserConfig()}
	noLivePol := sqlite.DefaultUserPolicy(false)
	noLivePol.EnableAllFolders = true
	noLivePol.EnableLiveTvAccess = false
	maxPG := 5
	noLivePol.MaxParentalRating = &maxPG
	viewer := &sqlite.UserRecord{ID: guid.New(), Name: "viewer_l", Policy: noLivePol, Config: sqlite.DefaultUserConfig()}
	_ = userRepo.CreateUser(ctx, admin)
	_ = userRepo.CreateUser(ctx, viewer)

	liveChan := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: liveLib.ID, ParentID: liveLib.ID,
		Type: "TvChannel", MediaType: "Video", Name: "Channel 1", OfficialRating: "G",
	}
	_ = repo.UpsertItem(ctx, liveChan)

	crit90, crit40 := float64(90), float64(40)
	mov1 := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: movLib.ID, ParentID: movLib.ID,
		Type: "Movie", MediaType: "Video", Container: "mkv", Name: "HDR Masterpiece",
		SortName: "hdr masterpiece", OfficialRating: "G", CriticRating: &crit90,
	}
	mov2 := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: movLib.ID, ParentID: movLib.ID,
		Type: "Movie", MediaType: "Video", Container: "mp4", Name: "SDR Indie",
		SortName: "sdr indie", OfficialRating: "PG", CriticRating: &crit40,
	}
	folder1 := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: movLib.ID, ParentID: movLib.ID,
		Type: "Folder", IsFolder: true, Name: "SubFolder", SortName: "subfolder", OfficialRating: "G",
	}
	for _, it := range []*sqlite.ItemRecord{mov1, mov2, folder1} {
		_ = repo.UpsertItem(ctx, it)
	}
	_ = repo.ReplaceMediaSourcesAndStreams(ctx, mov1.ID, []sqlite.MediaSourceRecord{
		{
			ID: mov1.ID, ItemID: mov1.ID, Name: "hdr.mkv", Path: filepath.Join(moviesDir, "hdr.mkv"), Container: "mkv",
			Streams: []sqlite.MediaStreamRecord{
				{StreamIndex: 0, StreamType: "Video", Codec: "hevc", VideoRange: "HDR10"},
				{StreamIndex: 1, StreamType: "Audio", Codec: "truehd"},
				{StreamIndex: 2, StreamType: "Subtitle", Codec: "subrip"},
			},
		},
	})
	_ = repo.ReplaceMediaSourcesAndStreams(ctx, mov2.ID, []sqlite.MediaSourceRecord{
		{
			ID: mov2.ID, ItemID: mov2.ID, Name: "sdr.mp4", Path: filepath.Join(moviesDir, "sdr.mp4"), Container: "mp4",
			Streams: []sqlite.MediaStreamRecord{
				{StreamIndex: 0, StreamType: "Video", Codec: "h264", VideoRange: "SDR"},
				{StreamIndex: 1, StreamType: "Audio", Codec: "aac"},
			},
		},
	})
	_ = repo.ReplaceItemTaxonomies(ctx, mov2.ID, "tag", []string{"skip-me"})

	box, _ := repo.CreateCollection(ctx, "Masterpiece Box", "", []string{mov1.ID})
	_, _ = store.Writer.ExecContext(ctx,
		`INSERT OR REPLACE INTO user_item_data(user_id, item_id, played, play_count, playback_position_ticks, is_favorite, likes) VALUES(?, ?, 1, 5, 0, 0, 1)`,
		guid.Normalize(viewer.ID), guid.Normalize(mov1.ID),
	)
	_, _ = store.Writer.ExecContext(ctx,
		`INSERT OR REPLACE INTO user_item_data(user_id, item_id, played, play_count, playback_position_ticks, is_favorite, likes) VALUES(?, ?, 0, 1, 0, 0, 0)`,
		guid.Normalize(viewer.ID), guid.Normalize(mov2.ID),
	)

	h := New(repo, sc, cfg)
	h.SetUserRepo(userRepo)
	h.SetStore(store)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux, AuthFuncs{
		UserIDFromCtx: func(c context.Context) string {
			if ac, ok := middleware.AuthFromContext(c); ok && ac != nil {
				return ac.UserID()
			}
			return ""
		},
	})

	doAs := func(u *sqlite.UserRecord, method, target, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, target, bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		req = req.WithContext(middleware.WithAuthContext(req.Context(), &middleware.AuthContext{User: u}))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}

	// 1. H-04: /Users/{UserId}/Views excludes LiveTV library when EnableLiveTvAccess=false even with EnableAllFolders=true!
	viewsRec := doAs(viewer, http.MethodGet, "/Users/"+viewer.ID+"/Views", "")
	if viewsRec.Code != http.StatusOK {
		t.Fatalf("Views status=%d", viewsRec.Code)
	}
	var viewsRes dto.QueryResult[dto.BaseItemDto]
	_ = json.Unmarshal(viewsRec.Body.Bytes(), &viewsRes)
	if len(viewsRes.Items) != 2 {
		t.Fatalf("expected 2 non-livetv Views for viewer, got %d: %+v", len(viewsRes.Items), viewsRes.Items)
	}
	if getLiveRec := doAs(viewer, http.MethodGet, "/Items/"+liveChan.ID, ""); getLiveRec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 on LiveTV item when EnableLiveTvAccess=false, got %d", getLiveRec.Code)
	}

	// 2. H-09: POST /Library/VirtualFolders rejects /, /etc, relative path, and DataDir, while accepting valid disjoint path.
	for _, badBody := range []string{
		`{"Name":"RootLib","CollectionType":"movies","Paths":["/"]}`,
		`{"Name":"EtcLib","CollectionType":"movies","Paths":["/etc"]}`,
		`{"Name":"RelLib","CollectionType":"movies","Paths":["relative/dir"]}`,
		`{"Name":"DataLib","CollectionType":"movies","Paths":["` + dataDir + `"]}`,
	} {
		if r := doAs(admin, http.MethodPost, "/Library/VirtualFolders", badBody); r.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for unsafe mount path %s, got %d: %s", badBody, r.Code, r.Body.String())
		}
	}
	validMount := filepath.Join(tmp, "valid-anime")
	_ = os.MkdirAll(validMount, 0o755)
	if r := doAs(admin, http.MethodPost, "/Library/VirtualFolders", `{"Name":"Anime","CollectionType":"tvshows","Paths":["`+validMount+`"]}`); r.Code != http.StatusOK {
		t.Fatalf("expected 200 for valid disjoint mount path, got %d: %s", r.Code, r.Body.String())
	}

	// 3. M-13 & 3.1-9: SQL filters Likes, Dislikes, IsFolder, IsNotFolder, AudioCodecs, SubtitleCodecs, Containers, ExcludeTags, GroupItemsIntoCollections, SortBy=PlayCount/CriticRating/OfficialRating!
	qLikes := doAs(viewer, http.MethodGet, "/Users/"+viewer.ID+"/Items?Recursive=true&IncludeItemTypes=Movie&Filters=Likes&AudioCodecs=truehd&SubtitleCodecs=subrip&Containers=mkv&ExcludeTags=skip-me", "")
	var qr dto.QueryResult[dto.BaseItemDto]
	_ = json.Unmarshal(qLikes.Body.Bytes(), &qr)
	if qr.TotalRecordCount != 1 || len(qr.Items) != 1 || guid.Normalize(qr.Items[0].Id) != guid.Normalize(mov1.ID) {
		t.Fatalf("3.1-9 Likes+AudioCodecs+SubtitleCodecs+Containers+ExcludeTags expected mov1, got %+v", qr)
	}

	qFolder := doAs(viewer, http.MethodGet, "/Users/"+viewer.ID+"/Items?ParentId="+movLib.ID+"&Filters=IsFolder", "")
	_ = json.Unmarshal(qFolder.Body.Bytes(), &qr)
	if qr.TotalRecordCount != 1 || guid.Normalize(qr.Items[0].Id) != guid.Normalize(folder1.ID) {
		t.Fatalf("expected folder1 for Filters=IsFolder, got %+v", qr)
	}

	qGrouped := doAs(viewer, http.MethodGet, "/Users/"+viewer.ID+"/Items?Recursive=true&IncludeItemTypes=Movie,BoxSet&GroupItemsIntoCollections=true&SortBy=CriticRating,PlayCount,OfficialRating&SortOrder=Descending&Limit=999999", "")
	_ = json.Unmarshal(qGrouped.Body.Bytes(), &qr)
	// mov1 is collapsed into Masterpiece Box, leaving Masterpiece Box + mov2!
	foundBox, foundGroupedMov1 := false, false
	for _, it := range qr.Items {
		if guid.Normalize(it.Id) == guid.Normalize(box.ID) {
			foundBox = true
		}
		if guid.Normalize(it.Id) == guid.Normalize(mov1.ID) {
			foundGroupedMov1 = true
		}
	}
	if !foundBox || foundGroupedMov1 {
		t.Fatalf("expected GroupItemsIntoCollections=true to collapse mov1 into BoxSet %s, got %+v", box.ID, qr.Items)
	}

	// 4. 3.1-4: POST /Library/Media/Updated scans ONLY targeted path (not untouched otherLib)!
	targetedFile := filepath.Join(moviesDir, "TargetedHit (2024).mkv")
	untouchedFile := filepath.Join(otherDir, "UntouchedSibling (2024).mkv")
	_ = os.WriteFile(targetedFile, []byte("v1"), 0o644)
	_ = os.WriteFile(untouchedFile, []byte("v2"), 0o644)

	updRec := doAs(admin, http.MethodPost, "/Library/Media/Updated", `{"Updates":[{"Path":"`+targetedFile+`","UpdateType":"Created"}]}`)
	if updRec.Code != http.StatusNoContent {
		t.Fatalf("POST /Library/Media/Updated status=%d", updRec.Code)
	}
	waitCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	sc.WaitIdle(waitCtx)

	if _, err := repo.FindItemByMediaSourcePath(ctx, movLib.ID, targetedFile); err != nil {
		t.Fatalf("expected targeted file scanned by /Library/Media/Updated, err=%v", err)
	}
	if _, err := repo.FindItemByMediaSourcePath(ctx, otherLib.ID, untouchedFile); err == nil {
		t.Fatal("expected untouched sibling library NOT scanned when targeted Updates were provided")
	}
	_ = otherLib
}
