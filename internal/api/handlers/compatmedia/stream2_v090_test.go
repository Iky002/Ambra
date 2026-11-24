package compatmedia

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ambra-media/ambra/internal/api/dto"
	"github.com/ambra-media/ambra/internal/api/middleware"
	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/core/imaging"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/plugins"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestSubtitleUploadAdminRBACAndDeduplication(t *testing.T) {
	tmp := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmp, "sub_rbac_dedup.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	ctx := context.Background()
	repo := sqlite.NewLibraryRepo(store)
	userRepo := sqlite.NewUserRepo(store)
	cfg := config.Default(tmp)

	libDir := filepath.Join(tmp, "movies")
	if err := os.MkdirAll(libDir, 0o755); err != nil {
		t.Fatal(err)
	}
	lib, err := repo.CreateLibrary(ctx, "Movies", "movies", []string{libDir})
	if err != nil {
		t.Fatal(err)
	}
	videoPath := filepath.Join(libDir, "Dune (2021).mkv")
	_ = os.WriteFile(videoPath, []byte("fake-video"), 0o644)

	movie := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: lib.ID, ParentID: lib.ID,
		Type: "Movie", MediaType: "Video", Name: "Dune", Path: videoPath,
	}
	_ = repo.UpsertItem(ctx, movie)
	_ = repo.ReplaceMediaSourcesAndStreams(ctx, movie.ID, []sqlite.MediaSourceRecord{
		{
			ID: "ms-dune", ItemID: movie.ID, Name: "main", Path: videoPath, Container: "mkv",
			Streams: []sqlite.MediaStreamRecord{
				{MediaSourceID: "ms-dune", ItemID: movie.ID, StreamIndex: 0, StreamType: "Video", Codec: "h264"},
			},
		},
	})

	normalUser := &sqlite.UserRecord{ID: guid.New(), Name: "viewer", IsAdmin: false, Policy: sqlite.DefaultUserPolicy(false), Config: sqlite.DefaultUserConfig()}
	adminUser := &sqlite.UserRecord{ID: guid.New(), Name: "admin", IsAdmin: true, Policy: sqlite.DefaultUserPolicy(true), Config: sqlite.DefaultUserConfig()}
	_ = userRepo.CreateUser(ctx, normalUser)
	_ = userRepo.CreateUser(ctx, adminUser)

	h := New(repo, nil, imaging.New(repo), plugins.NewRegistry(), store, cfg)
	h.SetUserRepo(userRepo)
	requireAdmin := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ac, ok := middleware.AuthFromContext(r.Context())
			if !ok || ac == nil || !ac.IsAdmin() {
				http.Error(w, "admin required", http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
	router := h.BuildRouter(AuthFuncs{RequireAdmin: requireAdmin})

	postAs := func(u *sqlite.UserRecord, path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req = req.WithContext(middleware.WithAuthContext(req.Context(), &middleware.AuthContext{User: u}))
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}

	payloadV1, _ := json.Marshal(map[string]any{
		"Language": "zh-cn",
		"Format":   "srt",
		"IsForced": true,
		"Data":     base64.StdEncoding.EncodeToString([]byte("1\n00:00:01,000 --> 00:00:02,000\nV1\n")),
	})

	// 1. Non-admin MUST receive 403 on both /Videos/{Id}/Subtitles and /Items/{Id}/Subtitles
	for _, ep := range []string{"/Videos/" + movie.ID + "/Subtitles", "/Items/" + movie.ID + "/Subtitles"} {
		if rec := postAs(normalUser, ep, string(payloadV1)); rec.Code != http.StatusForbidden {
			t.Fatalf("non-admin POST %s: got %d, want 403", ep, rec.Code)
		}
	}

	// 2. Admin upload succeeds -> inserts stream_index 1
	if rec := postAs(adminUser, "/Videos/"+movie.ID+"/Subtitles", string(payloadV1)); rec.Code != http.StatusNoContent {
		t.Fatalf("admin upload V1: got %d body=%s", rec.Code, rec.Body.String())
	}
	sources, err := repo.GetMediaSourcesByItemID(ctx, movie.ID)
	if err != nil || len(sources) != 1 || len(sources[0].Streams) != 2 {
		t.Fatalf("expected 2 streams after first upload, got %+v err=%v", sources, err)
	}
	firstIdx := sources[0].Streams[1].StreamIndex
	if !sources[0].Streams[1].IsForced {
		t.Fatalf("expected IsForced=true on V1")
	}

	// 3. Admin re-uploads same language/format (overwrite) -> MUST UPDATE existing row, NOT insert duplicate
	payloadV2, _ := json.Marshal(map[string]any{
		"Language": "zh-cn",
		"Format":   "srt",
		"IsForced": false,
		"Data":     base64.StdEncoding.EncodeToString([]byte("1\n00:00:01,000 --> 00:00:02,000\nV2\n")),
	})
	if rec := postAs(adminUser, "/Items/"+movie.ID+"/Subtitles", string(payloadV2)); rec.Code != http.StatusNoContent {
		t.Fatalf("admin upload V2: got %d body=%s", rec.Code, rec.Body.String())
	}
	sources, err = repo.GetMediaSourcesByItemID(ctx, movie.ID)
	if err != nil || len(sources) != 1 || len(sources[0].Streams) != 2 {
		t.Fatalf("expected STILL 2 streams after overwrite (no duplicate), got %d streams", len(sources[0].Streams))
	}
	if sources[0].Streams[1].StreamIndex != firstIdx {
		t.Fatalf("expected preserved stream_index %d, got %d", firstIdx, sources[0].Streams[1].StreamIndex)
	}
	if sources[0].Streams[1].IsForced {
		t.Fatalf("expected IsForced updated to false on V2")
	}
	gotContent, _ := os.ReadFile(sources[0].Streams[1].Path)
	if !strings.Contains(string(gotContent), "V2") {
		t.Fatalf("expected overwritten file content to contain V2, got %q", string(gotContent))
	}
}

func TestLocalTrailersSpecialFeaturesAndThemeMediaEndpoints(t *testing.T) {
	tmp := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmp, "extras_api.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	ctx := context.Background()
	repo := sqlite.NewLibraryRepo(store)
	userRepo := sqlite.NewUserRepo(store)
	cfg := config.Default(tmp)

	libAllowed, _ := repo.CreateLibrary(ctx, "Allowed", "movies", []string{filepath.Join(tmp, "a")})
	libForbidden, _ := repo.CreateLibrary(ctx, "Forbidden", "movies", []string{filepath.Join(tmp, "b")})

	seedMovieWithExtras := func(libID string) string {
		m := &sqlite.ItemRecord{ID: guid.New(), LibraryID: libID, ParentID: libID, Type: "Movie", MediaType: "Video", Name: "Main Movie"}
		_ = repo.UpsertItem(ctx, m)
		extras := []struct {
			name, itemType, mediaType, extraType string
		}{
			{"Official Trailer", "Video", "Video", "Trailer"},
			{"Making Of", "Video", "Video", "Featurette"},
			{"On Set", "Video", "Video", "BehindTheScenes"},
			{"Main Theme", "Audio", "Audio", "ThemeSong"},
			{"Backdrop Loop", "Video", "Video", "ThemeVideo"},
		}
		for _, ex := range extras {
			rec := &sqlite.ItemRecord{
				ID: guid.New(), LibraryID: libID, ParentID: m.ID,
				Type: ex.itemType, MediaType: ex.mediaType, Name: ex.name,
				Extra: sqlite.ItemExtra{ExtraType: ex.extraType},
			}
			_ = repo.UpsertItem(ctx, rec)
			_ = repo.UpsertMediaSourceAndStreams(ctx, rec.ID, sqlite.MediaSourceRecord{
				ID: rec.ID, ItemID: rec.ID, Name: ex.name, Path: "/fake/" + ex.name, Container: "mp4",
			})
		}
		return m.ID
	}

	allowedMovieID := seedMovieWithExtras(libAllowed.ID)
	forbiddenMovieID := seedMovieWithExtras(libForbidden.ID)

	pol := sqlite.DefaultUserPolicy(false)
	pol.EnableAllFolders = false
	pol.EnabledFolders = []string{libAllowed.ID}
	restricted := &sqlite.UserRecord{ID: guid.New(), Name: "restricted", Policy: pol, Config: sqlite.DefaultUserConfig()}
	_ = userRepo.CreateUser(ctx, restricted)

	h := New(repo, nil, imaging.New(repo), plugins.NewRegistry(), store, cfg)
	h.SetUserRepo(userRepo)
	router := h.BuildRouter(AuthFuncs{})

	getAs := func(u *sqlite.UserRecord, path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req = req.WithContext(middleware.WithAuthContext(req.Context(), &middleware.AuthContext{User: u}))
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}

	// 1. Restricted user gets 403 on all 5 extra endpoints for forbidden library item
	for _, suffix := range []string{"/LocalTrailers", "/SpecialFeatures", "/ThemeSongs", "/ThemeVideos", "/ThemeMedia"} {
		rec := getAs(restricted, "/Items/"+forbiddenMovieID+suffix)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("GET /Items/%s%s as restricted: got %d, want 403", forbiddenMovieID, suffix, rec.Code)
		}
	}

	// 2. LocalTrailers returns array with 1 Trailer + MediaSources
	rec := getAs(restricted, "/Items/"+allowedMovieID+"/LocalTrailers")
	if rec.Code != http.StatusOK {
		t.Fatalf("LocalTrailers: status=%d body=%s", rec.Code, rec.Body.String())
	}
	var trailers []dto.BaseItemDto
	if err := json.Unmarshal(rec.Body.Bytes(), &trailers); err != nil || len(trailers) != 1 || trailers[0].ExtraType != "Trailer" || len(trailers[0].MediaSources) != 1 {
		t.Fatalf("unexpected LocalTrailers response: %+v err=%v", trailers, err)
	}

	// 3. SpecialFeatures returns array with 2 items (Featurette + BehindTheScenes)
	rec = getAs(restricted, "/Items/"+allowedMovieID+"/SpecialFeatures")
	if rec.Code != http.StatusOK {
		t.Fatalf("SpecialFeatures: status=%d", rec.Code)
	}
	var specials []dto.BaseItemDto
	if err := json.Unmarshal(rec.Body.Bytes(), &specials); err != nil || len(specials) != 2 {
		t.Fatalf("unexpected SpecialFeatures response: len=%d err=%v", len(specials), err)
	}

	// 4. ThemeSongs & ThemeVideos return ThemeMediaResult with 1 item each
	for _, tc := range []struct {
		suffix string
		wantET string
	}{
		{"/ThemeSongs", "ThemeSong"},
		{"/ThemeVideos", "ThemeVideo"},
	} {
		rec = getAs(restricted, "/Items/"+allowedMovieID+tc.suffix)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status=%d", tc.suffix, rec.Code)
		}
		var body struct {
			Items            []dto.BaseItemDto `json:"Items"`
			TotalRecordCount int               `json:"TotalRecordCount"`
			OwnerId          string            `json:"OwnerId"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body.TotalRecordCount != 1 || len(body.Items) != 1 || body.Items[0].ExtraType != tc.wantET || body.OwnerId != allowedMovieID {
			t.Fatalf("unexpected %s response: %+v err=%v", tc.suffix, body, err)
		}
	}

	// 5. ThemeMedia aggregates both ThemeSongsResult and ThemeVideosResult
	rec = getAs(restricted, "/Items/"+allowedMovieID+"/ThemeMedia")
	if rec.Code != http.StatusOK {
		t.Fatalf("ThemeMedia: status=%d", rec.Code)
	}
	var allTheme struct {
		ThemeSongsResult  dto.QueryResult[dto.BaseItemDto] `json:"ThemeSongsResult"`
		ThemeVideosResult dto.QueryResult[dto.BaseItemDto] `json:"ThemeVideosResult"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &allTheme); err != nil ||
		allTheme.ThemeSongsResult.TotalRecordCount != 1 ||
		allTheme.ThemeVideosResult.TotalRecordCount != 1 {
		t.Fatalf("unexpected ThemeMedia response: %+v err=%v", allTheme, err)
	}
}
