package compatmedia

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ambra-media/ambra/internal/api/middleware"
	"github.com/ambra-media/ambra/internal/core/auth"
	"github.com/ambra-media/ambra/internal/core/transcode"
	"github.com/ambra-media/ambra/internal/core/watcher"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/plugins"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

type compatRoundTripFunc func(*http.Request) (*http.Response, error)

func (f compatRoundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

type v0110SubtitlePlugin struct {
	id         string
	name       string
	searchErr  error
	hits       []plugins.RemoteSubtitleInfo
	downloads  map[string]*plugins.SubtitleDownloadResult
	dlErrors   map[string]error
}

func (p *v0110SubtitlePlugin) Manifest() plugins.Manifest {
	return plugins.Manifest{
		ID:       p.id,
		Name:     p.name,
		Version:  "1.0.0",
		Enabled:  true,
		Priority: 5,
		Kinds:    []plugins.Kind{plugins.KindSubtitle},
	}
}

func (p *v0110SubtitlePlugin) Configure(map[string]any) error { return nil }

func (p *v0110SubtitlePlugin) SearchSubtitles(_ context.Context, q plugins.SubtitleSearchQuery) ([]plugins.RemoteSubtitleInfo, error) {
	if p.searchErr != nil {
		return nil, p.searchErr
	}
	out := make([]plugins.RemoteSubtitleInfo, 0, len(p.hits))
	for _, h := range p.hits {
		h.Language = q.Language
		out = append(out, h)
	}
	return out, nil
}

func (p *v0110SubtitlePlugin) DownloadSubtitle(_ context.Context, subID string) (*plugins.SubtitleDownloadResult, error) {
	if err, ok := p.dlErrors[subID]; ok {
		return nil, err
	}
	if res, ok := p.downloads[subID]; ok {
		return res, nil
	}
	return nil, sqlite.ErrNotFound
}

func TestV0110_RemoteSubtitlesSearchDownloadAndProviderRoutes(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()

	userRepo := sqlite.NewUserRepo(env.store)
	authSvc := auth.NewService(userRepo, env.cfg)
	env.handler.SetUserRepo(userRepo)

	admin, _ := authSvc.CreateUser(ctx, "admin", "adminpass", true)
	restricted, _ := authSvc.CreateUser(ctx, "restricted", "pass1234", false)

	libDir := filepath.Join(env.tmpDir, "movies_lib")
	_ = os.MkdirAll(libDir, 0o755)
	lib, err := env.repo.CreateLibrary(ctx, "Movies", "movies", []string{libDir})
	if err != nil {
		t.Fatal(err)
	}
	otherLib, _ := env.repo.CreateLibrary(ctx, "Other", "movies", []string{t.TempDir()})

	resPol := sqlite.DefaultUserPolicy(false)
	resPol.EnableAllFolders = false
	resPol.EnabledFolders = []string{otherLib.ID}
	_ = userRepo.UpdateUserPolicy(ctx, restricted.ID, resPol)
	restricted.Policy = resPol

	videoPath := filepath.Join(libDir, "Avatar.2009.mkv")
	_ = os.WriteFile(videoPath, []byte("MKV"), 0o644)
	strmPath := filepath.Join(libDir, "CloudShow.strm")
	_ = os.WriteFile(strmPath, []byte("https://93.184.216.34/ep1.mp4\n"), 0o644)

	yr, sn, ep := 2009, 1, 2
	epItem := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: lib.ID, ParentID: lib.ID,
		Type: "Episode", MediaType: "Video", Name: "Avatar Ep", OriginalTitle: "Avatar Original",
		ProductionYear: &yr, ParentIndexNumber: &sn, IndexNumber: &ep,
		Path: videoPath,
	}
	strmItem := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: lib.ID, ParentID: lib.ID,
		Type: "Movie", MediaType: "Video", Name: "Cloud Movie", Path: strmPath, IsStrm: true,
	}
	_ = env.repo.UpsertItem(ctx, epItem)
	_ = env.repo.UpsertItem(ctx, strmItem)
	_ = env.repo.ReplaceMediaSourcesAndStreams(ctx, epItem.ID, []sqlite.MediaSourceRecord{
		{ID: epItem.ID, ItemID: epItem.ID, Path: videoPath, Container: "mkv"},
	})
	_ = env.repo.ReplaceMediaSourcesAndStreams(ctx, strmItem.ID, []sqlite.MediaSourceRecord{
		{ID: strmItem.ID, ItemID: strmItem.ID, Path: strmPath, Container: "strm"},
	})

	srtBytes := []byte("1\n00:00:01,000 --> 00:00:03,000\n你好世界\n")
	assBytes := []byte("[Script Info]\nTitle: Test\n")

	// Register one failing provider and one working provider
	env.reg.Register(&v0110SubtitlePlugin{
		id:        "failing-sub",
		name:      "FailingSub",
		searchErr: errors.New("upstream timeout"),
		dlErrors:  map[string]error{"boom": errors.New("download failed")},
	})
	env.reg.Register(&v0110SubtitlePlugin{
		id:   "opensub-mock",
		name: "OpenSubtitlesMock",
		hits: []plugins.RemoteSubtitleInfo{
			{ID: "sub-srt-1", Name: "Avatar.chs.srt", Format: "srt"},
			{ID: "opensub-mock:already-prefixed", Name: "Avatar.ass", ProviderName: "CustomName", Format: "ass"},
		},
		downloads: map[string]*plugins.SubtitleDownloadResult{
			"sub-srt-1":        {Language: "chi", Format: ".srt", Content: srtBytes},
			"sub-ass-unpref":   {Language: "eng", Format: "ass", Content: assBytes},
			"sub-empty":        {Language: "eng", Format: "srt", Content: nil},
			"sub-huge":         {Language: "eng", Format: "srt", Content: bytes.Repeat([]byte("x"), maxSubtitleUploadBytes+5)},
			"sub-badfmt":       {Language: "eng", Format: "exe", Content: []byte("MZ")},
			"sub-no-fmt":       {Language: "jpn", Format: "", Content: srtBytes},
		},
		dlErrors: map[string]error{
			"net-err": errors.New("connection reset"),
		},
	})

	router := env.handler.BuildRouter(AuthFuncs{})
	adminAC := &middleware.AuthContext{User: admin, Token: "adm-tok"}
	resAC := &middleware.AuthContext{User: restricted, Token: "res-tok"}

	doReq := func(ac *middleware.AuthContext, method, target string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, target, nil)
		if ac != nil {
			r = r.WithContext(middleware.WithAuthContext(r.Context(), ac))
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		return w
	}

	// 1. RemoteSearch Subtitles: missing item -> 200 [], forbidden -> 403, valid -> 200 with prefixed IDs
	wMiss := doReq(adminAC, http.MethodGet, "/Items/"+guid.New()+"/RemoteSearch/Subtitles/chi")
	if wMiss.Code != http.StatusOK || strings.TrimSpace(wMiss.Body.String()) != "[]" {
		t.Fatalf("expected 200 [] for missing item, got %d %s", wMiss.Code, wMiss.Body.String())
	}
	if code := doReq(resAC, http.MethodGet, "/Items/"+epItem.ID+"/RemoteSearch/Subtitles/chi").Code; code != http.StatusForbidden {
		t.Fatalf("expected 403 for restricted user, got %d", code)
	}
	wSearch := doReq(adminAC, http.MethodGet, "/Items/"+epItem.ID+"/RemoteSearch/Subtitles/chi")
	if wSearch.Code != http.StatusOK {
		t.Fatalf("RemoteSearch Subtitles failed: %d %s", wSearch.Code, wSearch.Body.String())
	}
	var hits []plugins.RemoteSubtitleInfo
	_ = json.Unmarshal(wSearch.Body.Bytes(), &hits)
	if len(hits) != 2 || hits[0].ID != "opensub-mock:sub-srt-1" || hits[0].ProviderName != "OpenSubtitlesMock" {
		t.Fatalf("unexpected subtitle hits: %+v", hits)
	}

	// 2. DownloadRemoteSubtitle: prefixed ID saves alongside media & registers stream; second download updates stream!
	wDL1 := doReq(adminAC, http.MethodPost, "/Items/"+epItem.ID+"/RemoteSearch/Subtitles/opensub-mock:sub-srt-1")
	if wDL1.Code != http.StatusNoContent {
		t.Fatalf("first subtitle download failed: %d %s", wDL1.Code, wDL1.Body.String())
	}
	expectedSidecar := filepath.Join(libDir, "Avatar.2009.chi.srt")
	if gotBytes, err := os.ReadFile(expectedSidecar); err != nil || !bytes.Equal(gotBytes, srtBytes) {
		t.Fatalf("downloaded subtitle not written to %s: err=%v", expectedSidecar, err)
	}
	// Second download to same path exercises UPDATE item_media_streams branch!
	wDL2 := doReq(adminAC, http.MethodPost, "/Items/"+epItem.ID+"/RemoteSearch/Subtitles/opensub-mock:sub-srt-1")
	if wDL2.Code != http.StatusNoContent {
		t.Fatalf("second subtitle download update failed: %d", wDL2.Code)
	}

	// Unprefixed subtitle ID on STRM item exercises fallback loop in downloadSubtitleFromRegistry + cacheDir fallback!
	wDLStrm := doReq(adminAC, http.MethodPost, "/Items/"+strmItem.ID+"/RemoteSearch/Subtitles/sub-no-fmt")
	if wDLStrm.Code != http.StatusNoContent {
		t.Fatalf("STRM cache-fallback subtitle download failed: %d %s", wDLStrm.Code, wDLStrm.Body.String())
	}

	// Error branches in handleDownloadRemoteSubtitle
	for _, tc := range []struct {
		ac   *middleware.AuthContext
		item string
		sub  string
		code int
	}{
		{adminAC, guid.New(), "opensub-mock:sub-srt-1", http.StatusNotFound},
		{resAC, epItem.ID, "opensub-mock:sub-srt-1", http.StatusForbidden},
		{adminAC, epItem.ID, "opensub-mock:nonexistent", http.StatusNotFound},
		{adminAC, epItem.ID, "opensub-mock:net-err", http.StatusBadGateway},
		{adminAC, epItem.ID, "opensub-mock:sub-empty", http.StatusBadGateway},
		{adminAC, epItem.ID, "opensub-mock:sub-huge", http.StatusRequestEntityTooLarge},
		{adminAC, epItem.ID, "opensub-mock:sub-badfmt", http.StatusBadRequest},
	} {
		if got := doReq(tc.ac, http.MethodPost, "/Items/"+tc.item+"/RemoteSearch/Subtitles/"+tc.sub).Code; got != tc.code {
			t.Fatalf("POST Subtitles/%s on %s got %d, want %d", tc.sub, tc.item, got, tc.code)
		}
	}

	// 3. GET /Providers/Subtitles/Subtitles/{Id}: hit (ass) vs fallback (WEBVTT)
	wProvHit := doReq(adminAC, http.MethodGet, "/Providers/Subtitles/Subtitles/sub-ass-unpref")
	if wProvHit.Code != http.StatusOK || !strings.Contains(wProvHit.Header().Get("Content-Type"), "text/x-ssa") || !bytes.Equal(wProvHit.Body.Bytes(), assBytes) {
		t.Fatalf("Provider subtitle download failed: %d ct=%s", wProvHit.Code, wProvHit.Header().Get("Content-Type"))
	}
	wProvMiss := doReq(adminAC, http.MethodGet, "/Providers/Subtitles/Subtitles/unknown-id")
	if wProvMiss.Code != http.StatusOK || !strings.HasPrefix(wProvMiss.Body.String(), "WEBVTT") {
		t.Fatalf("Provider subtitle fallback failed: %d %q", wProvMiss.Code, wProvMiss.Body.String())
	}
}

func TestV0110_PhysicalDeleteSidecarsVirtualItemsAndExtendedPolicy(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()

	userRepo := sqlite.NewUserRepo(env.store)
	authSvc := auth.NewService(userRepo, env.cfg)
	env.handler.SetUserRepo(userRepo)

	admin, _ := authSvc.CreateUser(ctx, "admin", "adminpass", true)
	deleter, _ := authSvc.CreateUser(ctx, "deleter", "pass1234", false)
	noDel, _ := authSvc.CreateUser(ctx, "nodel", "pass1234", false)

	libDir := filepath.Join(env.tmpDir, "tv_root")
	showDir := filepath.Join(libDir, "ShowA")
	_ = os.MkdirAll(showDir, 0o755)
	outsideDir := t.TempDir()
	outsideSecret := filepath.Join(outsideDir, "outside.mkv")
	_ = os.WriteFile(outsideSecret, []byte("DO-NOT-DELETE"), 0o644)

	lib, _ := env.repo.CreateLibrary(ctx, "TV", "tvshows", []string{libDir})

	delPol := sqlite.DefaultUserPolicy(false)
	delPol.EnableContentDeletion = true
	delPol.BlockUnratedItems = []string{"Movie", "Series", "Music"}
	delPol.AllowedTags = []string{"ok-tag"}
	_ = userRepo.UpdateUserPolicy(ctx, deleter.ID, delPol)
	deleter.Policy = delPol

	// Create episode media file + controlled sidecars + unrelated sibling + symlink escaping library
	epFile := filepath.Join(showDir, "S01E01.mkv")
	sidecars := []string{
		filepath.Join(showDir, "S01E01.nfo"),
		filepath.Join(showDir, "S01E01.zh.srt"),
		filepath.Join(showDir, "S01E01-poster.jpg"),
		filepath.Join(showDir, "S01E01-320-10.bif"),
	}
	unrelatedSibling := filepath.Join(showDir, "S01E02.mkv")
	for _, f := range append([]string{epFile, unrelatedSibling}, sidecars...) {
		_ = os.WriteFile(f, []byte("DATA"), 0o644)
	}
	escapeLink := filepath.Join(showDir, "S01E01.en.srt")
	_ = os.Symlink(outsideSecret, escapeLink)
	validInLibTarget := filepath.Join(showDir, "shared_sub.srt")
	_ = os.WriteFile(validInLibTarget, []byte("SUB"), 0o644)
	validInLibLink := filepath.Join(showDir, "S01E01.fr.srt")
	_ = os.Symlink(validInLibTarget, validInLibLink)

	series := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: lib.ID, ParentID: lib.ID,
		Type: "Series", Name: "ShowA", OfficialRating: "TV-PG", IsFolder: true, Path: showDir,
	}
	ep1 := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: lib.ID, ParentID: series.ID, SeriesID: series.ID,
		Type: "Episode", MediaType: "Video", Name: "Ep 1", OfficialRating: "", Path: epFile,
	}
	unratedMovie := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: lib.ID, ParentID: lib.ID,
		Type: "Movie", MediaType: "Video", Name: "Unrated", OfficialRating: "", Path: unrelatedSibling,
	}
	for _, it := range []*sqlite.ItemRecord{series, ep1, unratedMovie} {
		_ = env.repo.UpsertItem(ctx, it)
	}
	_ = env.repo.ReplaceItemTaxonomies(ctx, series.ID, "tag", []string{"ok-tag"})
	_ = env.repo.ReplaceMediaSourcesAndStreams(ctx, ep1.ID, []sqlite.MediaSourceRecord{
		{
			ID: ep1.ID, ItemID: ep1.ID, Path: epFile, Container: "mkv",
			Streams: []sqlite.MediaStreamRecord{
				{StreamIndex: 0, StreamType: "Subtitle", Codec: "srt", IsExternal: true, Path: sidecars[1]},
				{StreamIndex: 1, StreamType: "Subtitle", Codec: "srt", IsExternal: true, Path: escapeLink},
			},
		},
	})
	_, _ = env.store.Writer.ExecContext(ctx,
		`INSERT INTO item_ancestors (ancestor_id, descendant_id, depth) VALUES (?, ?, 1)`,
		guid.Normalize(series.ID), guid.Normalize(ep1.ID),
	)

	// Managed metadata image directory for series
	managedImgDir := filepath.Join(env.cfg.Snapshot().DataDir, "metadata", "images", guid.Normalize(series.ID))
	_ = os.MkdirAll(managedImgDir, 0o755)
	_ = os.WriteFile(filepath.Join(managedImgDir, "primary_0.jpg"), []byte("IMG"), 0o644)

	boxSet, _ := env.repo.CreateCollection(ctx, "VirtualBox", "", []string{ep1.ID})
	ownPlaylist, _ := env.repo.CreatePlaylist(ctx, "Deleter PL", "Video", deleter.ID, nil)
	_ = env.repo.ReplaceItemTaxonomies(ctx, ownPlaylist.ID, "tag", []string{"ok-tag"})
	otherPlaylist, _ := env.repo.CreatePlaylist(ctx, "Admin PL", "Video", admin.ID, nil)
	_ = env.repo.ReplaceItemTaxonomies(ctx, otherPlaylist.ID, "tag", []string{"ok-tag"})

	router := env.handler.BuildRouter(AuthFuncs{})
	adminAC := &middleware.AuthContext{User: admin, Token: "t-adm"}
	delAC := &middleware.AuthContext{User: deleter, Token: "t-del"}
	noDelAC := &middleware.AuthContext{User: noDel, Token: "t-nd"}

	doReq := func(ac *middleware.AuthContext, method, target, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, target, strings.NewReader(body))
		if body != "" {
			r.Header.Set("Content-Type", "application/json")
		}
		r = r.WithContext(middleware.WithAuthContext(r.Context(), ac))
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		return w
	}

	// 1. Extended policy & Delete RBAC checks
	if matchesBlockUnratedItemType("Person", "", []string{"*"}) || !matchesBlockUnratedItemType("Audio", "Audio", []string{"", "Music"}) {
		t.Fatalf("unexpected matchesBlockUnratedItemType behavior")
	}
	if code := doReq(noDelAC, http.MethodDelete, "/Items/"+ep1.ID, "").Code; code != http.StatusForbidden {
		t.Fatalf("expected 403 when EnableContentDeletion=false, got %d", code)
	}
	if code := doReq(delAC, http.MethodDelete, "/Items/"+unratedMovie.ID, "").Code; code != http.StatusForbidden {
		t.Fatalf("expected 403 deleting item blocked by extended policy, got %d", code)
	}
	if code := doReq(delAC, http.MethodDelete, "/Items/"+boxSet.ID, "").Code; code != http.StatusForbidden {
		t.Fatalf("expected 403 non-admin deleting virtual BoxSet, got %d", code)
	}
	if code := doReq(delAC, http.MethodGet, "/Items/"+boxSet.ID+"/DeleteInfo", "").Code; code != http.StatusForbidden {
		t.Fatalf("expected 403 non-admin DeleteInfo on virtual BoxSet, got %d", code)
	}
	if code := doReq(delAC, http.MethodDelete, "/Items/"+otherPlaylist.ID, "").Code; code != http.StatusForbidden {
		t.Fatalf("expected 403 non-admin deleting another user's playlist, got %d", code)
	}
	// Non-admin CAN delete their own playlist
	if code := doReq(delAC, http.MethodDelete, "/Items/"+ownPlaylist.ID, "").Code; code != http.StatusNoContent {
		t.Fatalf("expected 204 non-admin deleting own playlist, got %d", code)
	}

	// 2. DeleteInfo on Series returns descendant episode + controlled sidecars, never outside symlink target or sibling!
	wInfo := doReq(delAC, http.MethodGet, "/Items/"+series.ID+"/DeleteInfo", "")
	if wInfo.Code != http.StatusOK {
		t.Fatalf("DeleteInfo failed: %d %s", wInfo.Code, wInfo.Body.String())
	}
	var infoResp struct {
		Paths []string `json:"Paths"`
	}
	_ = json.Unmarshal(wInfo.Body.Bytes(), &infoResp)
	if len(infoResp.Paths) != 6 { // epFile + 4 controlled sidecars + 1 valid intra-library symlink
		t.Fatalf("expected 6 delete paths (media + 4 sidecars + 1 in-lib symlink), got %d: %v", len(infoResp.Paths), infoResp.Paths)
	}

	// 3. Batch delete Series + BoxSet as admin cleans up physical files & managed image dir while preserving outside file & unrelated sibling!
	wBatch := doReq(adminAC, http.MethodPost, "/Items/Delete", `{"Ids":["`+series.ID+`","`+boxSet.ID+`"]}`)
	if wBatch.Code != http.StatusNoContent {
		t.Fatalf("POST /Items/Delete failed: %d %s", wBatch.Code, wBatch.Body.String())
	}
	for _, deleted := range append([]string{epFile}, sidecars...) {
		if _, err := os.Stat(deleted); !os.IsNotExist(err) {
			t.Fatalf("expected %s to be deleted from disk", deleted)
		}
	}
	if _, err := os.Stat(managedImgDir); !os.IsNotExist(err) {
		t.Fatalf("expected managed metadata image dir to be deleted")
	}
	if _, err := os.Stat(unrelatedSibling); err != nil {
		t.Fatalf("unrelated sibling S01E02.mkv must NOT be deleted: %v", err)
	}
	if _, err := os.Stat(outsideSecret); err != nil {
		t.Fatalf("escaped symlink target outsideSecret must NOT be deleted: %v", err)
	}
}

func TestV0110_ImagesEntityHLSSegmentsEncodingAndVirtualFolders(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()

	userRepo := sqlite.NewUserRepo(env.store)
	authSvc := auth.NewService(userRepo, env.cfg)
	mgr := transcode.NewManager(env.cfg, transcode.WithCapabilities(transcode.SoftwareOnlyCapabilities()))
	defer mgr.Close()
	wch, wErr := watcher.New(func(context.Context, string, []string) {})
	if wErr == nil {
		defer wch.Close()
		env.handler.SetWatcher(wch)
	}

	env.handler.SetUserRepo(userRepo)
	env.handler.SetTranscodeManager(mgr)
	env.handler.SetStreamResolverWithHeaders(func(ctx context.Context, rawPath string) (*plugins.StreamResolveResult, error) {
		return &plugins.StreamResolveResult{
			URL:     "https://93.184.216.34/media/live.mp4",
			Headers: map[string]string{"Authorization": "Bearer compat-tok"},
		}, nil
	})

	pngBytes := makePNGBytes(t)
	env.handler.SetHTTPClient(&http.Client{
		Transport: compatRoundTripFunc(func(r *http.Request) (*http.Response, error) {
			if strings.Contains(r.URL.Path, "html") {
				hdr := make(http.Header)
				hdr.Set("Content-Type", "text/html")
				return &http.Response{StatusCode: 200, Header: hdr, Body: io.NopCloser(strings.NewReader("<html></html>"))}, nil
			}
			hdr := make(http.Header)
			hdr.Set("Content-Type", "image/png")
			return &http.Response{StatusCode: 200, Header: hdr, Body: io.NopCloser(bytes.NewReader(pngBytes))}, nil
		}),
	})

	admin, _ := authSvc.CreateUser(ctx, "admin", "adminpass", true)
	viewer, _ := authSvc.CreateUser(ctx, "viewer", "pass1234", false)
	noTransUser, _ := authSvc.CreateUser(ctx, "notrans", "pass1234", false)

	libDir1 := filepath.Join(env.tmpDir, "vf_dir1")
	libDir2 := filepath.Join(env.tmpDir, "vf_dir2")
	_ = os.MkdirAll(libDir1, 0o755)
	_ = os.MkdirAll(libDir2, 0o755)
	lib, _ := env.repo.CreateLibrary(ctx, "Cinema", "movies", []string{libDir1})
	secretLib, _ := env.repo.CreateLibrary(ctx, "SecretCinema", "movies", []string{libDir2})

	vPol := sqlite.DefaultUserPolicy(false)
	vPol.EnableAllFolders = false
	vPol.EnabledFolders = []string{lib.ID}
	_ = userRepo.UpdateUserPolicy(ctx, viewer.ID, vPol)
	viewer.Policy = vPol

	ntPol := sqlite.DefaultUserPolicy(false)
	ntPol.EnableAudioPlaybackTranscoding = false
	ntPol.EnableVideoPlaybackTranscoding = false
	_ = userRepo.UpdateUserPolicy(ctx, noTransUser.ID, ntPol)
	noTransUser.Policy = ntPol

	router := env.handler.BuildRouter(AuthFuncs{})
	adminAC := &middleware.AuthContext{User: admin, Token: "t-adm"}
	viewerAC := &middleware.AuthContext{User: viewer, Token: "t-view"}
	ntAC := &middleware.AuthContext{User: noTransUser, Token: "t-nt"}

	doReq := func(ac *middleware.AuthContext, method, target, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, target, strings.NewReader(body))
		if body != "" && !strings.HasPrefix(target, "/Items/") {
			r.Header.Set("Content-Type", "application/json")
		}
		if ac != nil {
			r = r.WithContext(middleware.WithAuthContext(r.Context(), ac))
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		return w
	}

	// 1. Seed movie with chapters, images, person, genre, artist -> test 10-seg full image route, index swap, remote download, entity images, MediaFolders cover tag
	imgPath := filepath.Join(libDir1, "art.png")
	_ = os.WriteFile(imgPath, pngBytes, 0o644)
	mediaPath := filepath.Join(libDir1, "movie.mp4")
	_ = os.WriteFile(mediaPath, []byte("MP4-BYTES"), 0o644)

	movie := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: lib.ID, ParentID: lib.ID,
		Type: "Movie", MediaType: "Video", Name: "Visual Film", OfficialRating: "G", Path: mediaPath,
		Extra: sqlite.ItemExtra{
			Chapters: []sqlite.ChapterRecord{{Name: "Ch1", StartPositionTicks: 50_000_000}},
		},
	}
	artist := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: lib.ID, ParentID: lib.ID,
		Type: "MusicArtist", Name: "DaftPunk", OfficialRating: "G",
	}
	secretMovie := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: secretLib.ID, ParentID: secretLib.ID,
		Type: "Movie", MediaType: "Video", Name: "Hidden", OfficialRating: "G", Path: mediaPath,
	}
	for _, it := range []*sqlite.ItemRecord{movie, artist, secretMovie} {
		_ = env.repo.UpsertItem(ctx, it)
	}
	_ = env.repo.ReplaceItemTaxonomies(ctx, movie.ID, "genre", []string{"SciFi"})
	_ = env.repo.ReplaceItemTaxonomies(ctx, secretMovie.ID, "studio", []string{"SecretStudio"})
	_ = env.repo.ReplaceItemPeople(ctx, movie.ID, []sqlite.PersonInput{{Name: "KeanuReeves", Type: "Actor", Role: "Neo"}})
	seededPeople, pErr := env.repo.GetPeopleByItemID(ctx, movie.ID)
	if pErr != nil || len(seededPeople) == 0 {
		t.Fatalf("expected seeded KeanuReeves, got %+v err=%v", seededPeople, pErr)
	}
	personID := seededPeople[0].Id

	var genreID, secretStudioID string
	_ = env.store.Reader.QueryRowContext(ctx, `SELECT id FROM taxonomies WHERE LOWER(name) = 'scifi'`).Scan(&genreID)
	_ = env.store.Reader.QueryRowContext(ctx, `SELECT id FROM taxonomies WHERE LOWER(name) = 'secretstudio'`).Scan(&secretStudioID)

	for _, imgRec := range []sqlite.ImageRecord{
		{ItemID: movie.ID, ImageType: "Primary", ImageIndex: 0, Path: imgPath, Tag: "m-prim-0"},
		{ItemID: movie.ID, ImageType: "Backdrop", ImageIndex: 0, Path: imgPath, Tag: "m-bd-0"},
		{ItemID: movie.ID, ImageType: "Backdrop", ImageIndex: 1, Path: imgPath, Tag: "m-bd-1"},
		{ItemID: movie.ID, ImageType: "Chapter", ImageIndex: 0, Path: imgPath, Tag: "m-ch-db"},
		{ItemID: artist.ID, ImageType: "Primary", ImageIndex: 0, Path: imgPath, Tag: "art-tag"},
		{ItemID: personID, ImageType: "Primary", ImageIndex: 0, Path: imgPath, Tag: "per-tag"},
		{ItemID: genreID, ImageType: "Primary", ImageIndex: 0, Path: imgPath, Tag: "gen-tag"},
		{ItemID: secretStudioID, ImageType: "Primary", ImageIndex: 0, Path: imgPath, Tag: "sec-tag"},
	} {
		rCopy := imgRec
		_ = env.repo.UpsertImage(ctx, &rCopy)
	}

	// MediaFolders computes synthetic cover tag from child Backdrop/Primary
	wMF := doReq(viewerAC, http.MethodGet, "/Library/MediaFolders", "")
	if wMF.Code != http.StatusOK || !strings.Contains(wMF.Body.String(), computeSyntheticLibraryCoverTag(lib.ID, "m-bd-0")) {
		t.Fatalf("MediaFolders synthetic cover tag missing: %d %s", wMF.Code, wMF.Body.String())
	}

	// 10-segment full image route: anonymous without tag (404), anonymous with computed chapter tag (200), authenticated webp resize (200), forbidden (403)
	if code := doReq(nil, http.MethodGet, "/Items/"+movie.ID+"/Images/Primary/0/null/jpg/320/240/0/0", "").Code; code != http.StatusNotFound {
		t.Fatalf("expected 404 on anonymous full image route with 'null' tag, got %d", code)
	}
	chTag := computeChapterImageTag(movie.ID, 0, 50_000_000)
	if code := doReq(nil, http.MethodGet, "/Items/"+movie.ID+"/Images/Chapter/0/"+chTag+"/png/120/120/0/0", "").Code; code != http.StatusOK {
		t.Fatalf("expected 200 on anonymous Chapter full image route with computed tag, got %d", code)
	}
	if code := doReq(viewerAC, http.MethodGet, "/Items/"+movie.ID+"/Images/Primary/0/m-prim-0/webp/100/100/0/0", "").Code; code != http.StatusOK {
		t.Fatalf("expected 200 on authenticated full image route, got %d", code)
	}
	if code := doReq(viewerAC, http.MethodGet, "/Items/"+secretMovie.ID+"/Images/Primary/0/any/jpg/100/100/0/0", "").Code; code != http.StatusForbidden {
		t.Fatalf("expected 403 on forbidden item full image route, got %d", code)
	}

	// Swap image index (0 <-> 1) and move image index (1 -> 3)
	if code := doReq(adminAC, http.MethodPost, "/Items/"+movie.ID+"/Images/Backdrop/0/Index?newIndex=1", "").Code; code != http.StatusNoContent {
		t.Fatalf("swap image index failed: %d", code)
	}
	if code := doReq(adminAC, http.MethodPost, "/Items/"+movie.ID+"/Images/Backdrop/1/Index?newIndex=3", "").Code; code != http.StatusNoContent {
		t.Fatalf("move image index failed: %d", code)
	}
	if code := doReq(adminAC, http.MethodPost, "/Items/"+movie.ID+"/Images/Backdrop/0/Index?newIndex=-1", "").Code; code != http.StatusBadRequest {
		t.Fatalf("expected 400 for negative newIndex, got %d", code)
	}

	// Remote image download (Backdrop auto-increment + HTML rejection + base64 upload replacement)
	if code := doReq(adminAC, http.MethodPost, "/Items/"+movie.ID+"/RemoteImages/Download?Type=Backdrop&ImageUrl=https://img.example/bg.png", "").Code; code != http.StatusNoContent {
		t.Fatalf("RemoteImages/Download Backdrop failed: %d", code)
	}
	if code := doReq(adminAC, http.MethodPost, "/Items/"+movie.ID+"/RemoteImages/Download?Type=Primary&ImageUrl=https://img.example/html", "").Code; code != http.StatusBadGateway {
		t.Fatalf("expected 502 for HTML remote image, got %d", code)
	}
	b64PNG := base64.StdEncoding.EncodeToString(pngBytes)
	for i := 0; i < 2; i++ {
		rUp := httptest.NewRequest(http.MethodPost, "/Items/"+movie.ID+"/Images/Logo/0", strings.NewReader(b64PNG))
		rUp.Header.Set("Content-Type", "image/png")
		rUp = rUp.WithContext(middleware.WithAuthContext(ctx, adminAC))
		wUp := httptest.NewRecorder()
		router.ServeHTTP(wUp, rUp)
		if wUp.Code != http.StatusNoContent {
			t.Fatalf("base64 upload Logo #%d failed: %d", i, wUp.Code)
		}
	}

	// Entity image routes: anonymous with tag (200), anonymous without tag (404), restricted viewer allowed (person/artist/genre) vs denied (secretStudio)
	if code := doReq(nil, http.MethodGet, "/Persons/KeanuReeves/Images/Primary?tag=per-tag", "").Code; code != http.StatusOK {
		t.Fatalf("anonymous Person image with tag failed: %d", code)
	}
	if code := doReq(nil, http.MethodGet, "/Persons/KeanuReeves/Images/Primary", "").Code; code != http.StatusNotFound {
		t.Fatalf("expected 404 anonymous Person image without tag, got %d", code)
	}
	for _, okPath := range []string{
		"/Persons/KeanuReeves/Images/Primary",
		"/Artists/DaftPunk/Images/Primary",
		"/Genres/SciFi/Images/Primary/0",
	} {
		if code := doReq(viewerAC, http.MethodGet, okPath, "").Code; code != http.StatusOK {
			t.Fatalf("viewer entity image %s expected 200, got %d", okPath, code)
		}
	}
	if code := doReq(viewerAC, http.MethodGet, "/Studios/SecretStudio/Images/Primary", "").Code; code != http.StatusNotFound {
		t.Fatalf("viewer entity image for SecretStudio expected 404, got %d", code)
	}
	if code := doReq(viewerAC, http.MethodGet, "/Genres/SciFi/Images/Primary/-1", "").Code; code != http.StatusBadRequest {
		t.Fatalf("expected 400 for negative entity image index, got %d", code)
	}

	// 2. Dynamic compat HLS segment routes (/Audio/{Id}/hls1/... and /Videos/{Id}/hls/...)
	for _, tc := range []struct {
		in     string
		wantOK bool
	}{
		{"init.mp4", true},
		{"seg_0.aac", true},
		{"segment2.m4s", true},
		{"noext", false},
		{"seg_0.mkv", false},
		{"seg_bad.ts", false},
	} {
		if _, _, _, ok := parseCompatHLSSegmentName(tc.in); ok != tc.wantOK {
			t.Fatalf("parseCompatHLSSegmentName(%q) ok=%v, want %v", tc.in, ok, tc.wantOK)
		}
	}

	if code := doReq(ntAC, http.MethodGet, "/Audio/"+movie.ID+"/hls1/ps1/seg_0.ts", "").Code; code != http.StatusForbidden {
		t.Fatalf("expected 403 when audio transcoding disabled, got %d", code)
	}
	if code := doReq(ntAC, http.MethodGet, "/Videos/"+movie.ID+"/hls/ps1/seg_0.ts", "").Code; code != http.StatusForbidden {
		t.Fatalf("expected 403 when video transcoding disabled, got %d", code)
	}

	// Pre-populate cached segments for AudioOnly (fMP4 + TS) and Video (fMP4 + TS)
	adminNormID := guid.Normalize(admin.ID)
	audSnap, _ := mgr.RegisterSession(transcode.SessionParams{
		PlaySessionID: "aud-ps", UserID: adminNormID, ItemID: movie.ID, MediaSourceID: movie.ID, InputPath: mediaPath,
		VideoCodec: "none", AudioCodec: "aac", AudioBitrate: 192_000, AudioChannels: 2,
		SegmentDuration: transcode.DefaultSegmentDuration, SegmentContainer: "m4s", AudioOnly: true,
	})
	_ = os.WriteFile(filepath.Join(audSnap.WorkDir, "init.mp4"), []byte("AUD-INIT"), 0o644)
	audSnapTS, _ := mgr.RegisterSession(transcode.SessionParams{
		PlaySessionID: "aud-ps-ts", UserID: adminNormID, ItemID: movie.ID, MediaSourceID: movie.ID, InputPath: mediaPath,
		VideoCodec: "none", AudioCodec: "aac", AudioBitrate: 192_000, AudioChannels: 2,
		SegmentDuration: transcode.DefaultSegmentDuration, SegmentContainer: "ts", AudioOnly: true,
	})
	_ = os.WriteFile(filepath.Join(audSnapTS.WorkDir, "seg_0.ts"), []byte("AUD-TS"), 0o644)

	vidSnap, _ := mgr.RegisterSession(transcode.SessionParams{
		PlaySessionID: "vid-ps", UserID: adminNormID, ItemID: movie.ID, MediaSourceID: movie.ID, InputPath: mediaPath,
		AudioCodec: "aac", AudioBitrate: 192_000, AudioChannels: 2,
		SegmentDuration: transcode.DefaultSegmentDuration, SegmentContainer: "m4s", AudioOnly: false,
	})
	_ = os.WriteFile(filepath.Join(vidSnap.WorkDir, "init.mp4"), []byte("VID-INIT"), 0o644)
	_ = os.WriteFile(filepath.Join(vidSnap.WorkDir, "seg_0.m4s"), []byte("VID-M4S"), 0o644)

	vidSnapTS, _ := mgr.RegisterSession(transcode.SessionParams{
		PlaySessionID: "vid-ps-ts", UserID: adminNormID, ItemID: movie.ID, MediaSourceID: movie.ID, InputPath: mediaPath,
		AudioCodec: "aac", AudioBitrate: 192_000, AudioChannels: 2,
		SegmentDuration: transcode.DefaultSegmentDuration, SegmentContainer: "ts", AudioOnly: false,
	})
	_ = os.WriteFile(filepath.Join(vidSnapTS.WorkDir, "seg_0.ts"), []byte("VID-TS"), 0o644)

	wAudInit := doReq(adminAC, http.MethodGet, "/Audio/"+movie.ID+"/hls1/aud-ps/init.mp4", "")
	if wAudInit.Code != http.StatusOK || wAudInit.Header().Get("Content-Type") != "audio/mp4" || wAudInit.Body.String() != "AUD-INIT" {
		t.Fatalf("Audio compat init.mp4 failed: %d ct=%s", wAudInit.Code, wAudInit.Header().Get("Content-Type"))
	}
	wAudAAC := doReq(adminAC, http.MethodGet, "/Audio/"+movie.ID+"/hls/aud-ps-ts/seg_0.aac", "")
	if wAudAAC.Code != http.StatusOK || wAudAAC.Header().Get("Content-Type") != "audio/mp2t" || wAudAAC.Body.String() != "AUD-TS" {
		t.Fatalf("Audio compat .aac->ts segment failed: %d ct=%s", wAudAAC.Code, wAudAAC.Header().Get("Content-Type"))
	}
	wVidInit := doReq(adminAC, http.MethodGet, "/Videos/"+movie.ID+"/hls/vid-ps/init.mp4", "")
	if wVidInit.Code != http.StatusOK || wVidInit.Header().Get("Content-Type") != "video/mp4" || wVidInit.Body.String() != "VID-INIT" {
		t.Fatalf("Video compat init.mp4 failed: %d ct=%s", wVidInit.Code, wVidInit.Header().Get("Content-Type"))
	}
	wVidM4S := doReq(adminAC, http.MethodGet, "/Videos/"+movie.ID+"/hls/vid-ps/seg_0.m4s", "")
	if wVidM4S.Code != http.StatusOK || wVidM4S.Header().Get("Content-Type") != "video/mp4" || wVidM4S.Body.String() != "VID-M4S" {
		t.Fatalf("Video compat .m4s segment failed: %d ct=%s", wVidM4S.Code, wVidM4S.Header().Get("Content-Type"))
	}
	wVidTS := doReq(adminAC, http.MethodGet, "/Videos/"+movie.ID+"/hls/vid-ps-ts/seg_0.ts", "")
	if wVidTS.Code != http.StatusOK || wVidTS.Header().Get("Content-Type") != "video/mp2t" || wVidTS.Body.String() != "VID-TS" {
		t.Fatalf("Video compat .ts segment failed: %d ct=%s", wVidTS.Code, wVidTS.Header().Get("Content-Type"))
	}
	// HLS segment error paths: forbidden item, missing item, bad segment name, SimultaneousStreamLimit 429
	if code := doReq(viewerAC, http.MethodGet, "/Videos/"+secretMovie.ID+"/hls/vid-ps/seg_0.ts", "").Code; code != http.StatusNotFound {
		t.Fatalf("expected 404 for inaccessible item on compat HLS, got %d", code)
	}
	if code := doReq(adminAC, http.MethodGet, "/Videos/"+guid.New()+"/hls/vid-ps/seg_0.ts", "").Code; code != http.StatusNotFound {
		t.Fatalf("expected 404 for missing item on compat HLS, got %d", code)
	}
	if code := doReq(adminAC, http.MethodGet, "/Videos/"+movie.ID+"/hls/vid-ps/badname", "").Code; code != http.StatusNotFound {
		t.Fatalf("expected 404 for invalid segment name on compat HLS, got %d", code)
	}
	env.cfg.SimultaneousStreamLimit = 1
	if code := doReq(adminAC, http.MethodGet, "/Videos/"+movie.ID+"/hls/brand-new-ps/seg_0.ts", "").Code; code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 when stream limit reached on compat HLS, got %d", code)
	}
	env.cfg.SimultaneousStreamLimit = 0

	// 3. /Encoding/* read/write + transcodeMgr reload + VirtualFolder rename/update/delete paths with watcher
	for _, ep := range []struct{ getPath, postPath string }{
		{"/Encoding/ToneMapOptions", "/Encoding/FullToneMapOptions"},
		{"/Encoding/FullToneMapOptions", "/Encoding/FullToneMapOptions"},
		{"/Encoding/PublicToneMapOptions", "/Encoding/PublicToneMapOptions"},
		{"/Encoding/SubtitleOptions", "/Encoding/SubtitleOptions"},
		{"/Encoding/FfmpegOptions", "/Encoding/FfmpegOptions"},
		{"/Encoding/CodecParameters?CodecId=h264&Type=Video", "/Encoding/CodecParameters?CodecId=h264"},
	} {
		if code := doReq(adminAC, http.MethodPost, ep.postPath, `{"H264Crf":20}`).Code; code != http.StatusNoContent {
			t.Fatalf("POST %s failed: %d", ep.postPath, code)
		}
		if code := doReq(adminAC, http.MethodGet, ep.getPath, "").Code; code != http.StatusOK {
			t.Fatalf("GET %s failed: %d", ep.getPath, code)
		}
	}
	// Malformed JSON body fallback in SetEncodingConfig
	if code := doReq(adminAC, http.MethodPost, "/Encoding/SubtitleOptions", `{bad`).Code; code != http.StatusNoContent {
		t.Fatalf("expected 204 fallback on malformed encoding JSON, got %d", code)
	}

	// VirtualFolder path operations (Add, Update via OldPath/NewPath, Update via Paths, Delete path, Rename by name, Delete by name)
	extraPath := filepath.Join(env.tmpDir, "vf_extra")
	_ = os.MkdirAll(extraPath, 0o755)
	if code := doReq(adminAC, http.MethodPost, "/Library/VirtualFolders/Paths", `{"Name":"Cinema","Path":"`+extraPath+`"}`).Code; code != http.StatusNoContent {
		t.Fatalf("AddVirtualFolderPath by name failed: %d", code)
	}
	if code := doReq(adminAC, http.MethodPost, "/Library/VirtualFolders/Paths/Update", `{"Name":"Cinema","OldPath":"`+extraPath+`","NewPath":"`+libDir2+`"}`).Code; code != http.StatusNoContent {
		t.Fatalf("UpdateVirtualFolderPath OldPath->NewPath failed: %d", code)
	}
	if code := doReq(adminAC, http.MethodPost, "/Library/VirtualFolders/Paths/Update", `{"Name":"Cinema","Paths":["`+libDir1+`","`+extraPath+`"]}`).Code; code != http.StatusNoContent {
		t.Fatalf("UpdateVirtualFolderPath Paths slice failed: %d", code)
	}
	if code := doReq(adminAC, http.MethodDelete, "/Library/VirtualFolders/Paths?name=Cinema&path="+extraPath, "").Code; code != http.StatusNoContent {
		t.Fatalf("DeleteVirtualFolderPath failed: %d", code)
	}
	if code := doReq(adminAC, http.MethodPost, "/Library/VirtualFolders/Name?name=Cinema&newName=CinemaHD", "").Code; code != http.StatusNoContent {
		t.Fatalf("RenameVirtualFolder by name failed: %d", code)
	}

	// STRM extraction resolution via SetStreamResolverWithHeaders
	strmP := filepath.Join(libDir1, "test.strm")
	_ = os.WriteFile(strmP, []byte("https://93.184.216.34/media/live.mp4\n"), 0o644)
	if gotU, gotH, err := env.handler.resolveExtractionMediaTarget(ctx, strmP); err != nil || gotU == "" || gotH["Authorization"] == "" {
		t.Fatalf("resolveExtractionMediaTarget with headers failed: u=%q h=%v err=%v", gotU, gotH, err)
	}

	// Extra edge coverage: ItemRefresh FullRefresh+LockData, ItemFile folder/strm/missing, AddToPlaylistInfo, sidecar helpers, Subtitle delete embedded
	lockedMovie := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: lib.ID, ParentID: lib.ID,
		Type: "Movie", MediaType: "Video", Name: "Locked Film", OfficialRating: "G", Path: mediaPath,
		Extra: sqlite.ItemExtra{LockData: true},
	}
	folderItem := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: lib.ID, ParentID: lib.ID,
		Type: "Folder", Name: "SubFolder", IsFolder: true, Path: libDir1,
	}
	strmDLItem := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: lib.ID, ParentID: lib.ID,
		Type: "Movie", MediaType: "Video", Name: "StrmDL", Path: strmP, IsStrm: true,
	}
	for _, it := range []*sqlite.ItemRecord{lockedMovie, folderItem, strmDLItem} {
		_ = env.repo.UpsertItem(ctx, it)
	}
	_ = env.repo.ReplaceMediaSourcesAndStreams(ctx, lockedMovie.ID, []sqlite.MediaSourceRecord{
		{
			ID: lockedMovie.ID, ItemID: lockedMovie.ID, Path: mediaPath, Container: "mp4",
			Streams: []sqlite.MediaStreamRecord{
				{StreamIndex: 0, StreamType: "Subtitle", Codec: "subrip", IsExternal: false},
			},
		},
	})

	if code := doReq(adminAC, http.MethodPost, "/Items/"+lockedMovie.ID+"/Refresh", `{"MetadataRefreshMode":"FullRefresh","ImageRefreshMode":"FullRefresh","ReplaceAllMetadata":true,"ReplaceAllImages":true}`).Code; code != http.StatusNoContent {
		t.Fatalf("ItemRefresh FullRefresh failed: %d", code)
	}
	if code := doReq(adminAC, http.MethodPost, "/Items/"+guid.New()+"/Refresh?ReplaceAllMetadata=true", "").Code; code != http.StatusNotFound {
		t.Fatalf("ItemRefresh missing expected 404, got %d", code)
	}
	if code := doReq(adminAC, http.MethodGet, "/Items/"+folderItem.ID+"/File", "").Code; code != http.StatusBadRequest {
		t.Fatalf("ItemFile folder expected 400, got %d", code)
	}
	if code := doReq(adminAC, http.MethodGet, "/Items/"+strmDLItem.ID+"/File", "").Code; code != http.StatusForbidden {
		t.Fatalf("ItemFile .strm expected 403, got %d", code)
	}
	if code := doReq(adminAC, http.MethodDelete, "/Items/"+lockedMovie.ID+"/Subtitles/0", "").Code; code != http.StatusBadRequest {
		t.Fatalf("DeleteSubtitle embedded expected 400, got %d", code)
	}

	pl, _ := env.repo.CreatePlaylist(ctx, "DupCheckPL", "Video", viewer.ID, []string{movie.ID, secretMovie.ID})
	wDup := doReq(viewerAC, http.MethodGet, "/Playlists/"+pl.ID+"/AddToPlaylistInfo?Ids="+movie.ID+","+secretMovie.ID, "")
	if wDup.Code != http.StatusOK || !strings.Contains(wDup.Body.String(), `"DuplicateCount":1`) {
		t.Fatalf("AddToPlaylistInfo failed: %d %s", wDup.Code, wDup.Body.String())
	}

	if p, err := env.handler.resolveItemMediaPath(ctx, lockedMovie.ID, lockedMovie); err != nil || p != mediaPath {
		t.Fatalf("resolveItemMediaPath failed: %q %v", p, err)
	}
	if _, err := env.handler.resolveItemMediaPath(ctx, guid.New(), nil); err == nil {
		t.Fatalf("expected error from resolveItemMediaPath on empty")
	}
	for _, fn := range []string{"mov.nfo", "mov.en.srt", "mov-fanart.png", "mov-320.bif", "other.mkv", "mov-unknown.jpg"} {
		_ = isControlledSidecarFilename(fn, "mov")
	}
	rawRec := httptest.NewRecorder()
	rawRec.Header().Set("Cache-Control", "public")
	pw := &privateImageResponseWriter{ResponseWriter: rawRec}
	_, _ = pw.Write([]byte("x"))

	// Cross-user 403 and inaccessible item 403 across LocalTrailers, SpecialFeatures, ThemeSongs, Similar, InstantMix, PersonCredits
	for _, p := range []string{
		"/Items/" + movie.ID + "/LocalTrailers?UserId=" + admin.ID,
		"/Items/" + movie.ID + "/SpecialFeatures?UserId=" + admin.ID,
		"/Items/" + movie.ID + "/ThemeSongs?UserId=" + admin.ID,
		"/Items/" + movie.ID + "/ThemeMedia?UserId=" + admin.ID,
		"/Items/" + movie.ID + "/Similar?UserId=" + admin.ID,
		"/Items/" + movie.ID + "/InstantMix?UserId=" + admin.ID,
		"/Persons/" + personID + "/Credits?UserId=" + admin.ID,
		"/Items/" + secretMovie.ID + "/LocalTrailers",
		"/Items/" + secretMovie.ID + "/SpecialFeatures",
		"/Items/" + secretMovie.ID + "/ThemeSongs",
		"/Items/" + secretMovie.ID + "/Similar",
		"/Items/" + secretMovie.ID + "/InstantMix",
	} {
		if code := doReq(viewerAC, http.MethodGet, p, "").Code; code != http.StatusForbidden {
			t.Fatalf("GET %s expected 403, got %d", p, code)
		}
	}

	// resolveExtractionMediaTarget fallback chains (SetStreamResolver, readFirstLineStrmURL, blocked HTTP URL) & canDeleteItems without userRepo
	env.handler.SetStreamResolverWithHeaders(nil)
	env.handler.SetStreamResolver(func(ctx context.Context, rawPath string) (string, error) {
		return "https://93.184.216.34/media/fallback.mp4", nil
	})
	if u, err := env.handler.resolveExtractionMediaPath(ctx, strmP); err != nil || u != "https://93.184.216.34/media/fallback.mp4" {
		t.Fatalf("SetStreamResolver fallback failed: %q %v", u, err)
	}
	env.handler.SetStreamResolver(nil)
	if u, err := env.handler.resolveExtractionMediaPath(ctx, strmP); err != nil || u != "https://93.184.216.34/media/live.mp4" {
		t.Fatalf("readFirstLineStrmURL fallback failed: %q %v", u, err)
	}
	if _, err := env.handler.resolveExtractionMediaPath(ctx, "http://169.254.169.254/bad.mkv"); err == nil {
		t.Fatalf("expected error on blocked SSRF extraction URL")
	}
	for _, ctFmt := range []string{"vtt", "srt", "ass", "txt"} {
		_ = subtitleContentType(ctFmt)
	}
	for _, fExt := range []string{".ttf", ".otf", ".woff2", ".woff", ".ttc", ".bin"} {
		_ = fontContentType("f" + fExt)
	}

	hNoUR := New(env.repo, nil, nil, nil, env.store, env.cfg)
	_ = hNoUR.BuildRouter(AuthFuncs{UserIDFromCtx: func(context.Context) string { return "u-ctx" }})
	rNoUR := httptest.NewRequest(http.MethodDelete, "/Items/x", nil)
	if ok, isAdm := hNoUR.canDeleteItems(rNoUR); !ok || !isAdm {
		t.Fatalf("expected canDeleteItems=(true,true) via UserIDFromCtx without userRepo")
	}
	rACUser := rNoUR.WithContext(middleware.WithAuthContext(ctx, &middleware.AuthContext{User: viewer}))
	if ok, _ := hNoUR.canDeleteItems(rACUser); ok {
		t.Fatalf("expected canDeleteItems=false when ac.User.Policy.EnableContentDeletion=false without userRepo")
	}

	// Additional high-yield branches:
	// (a) UploadSubtitle duplicate (UPDATE stream), MediaSourceId=itemID fallback, MediaSourceId=unknown 404, oversized 413
	subB64 := base64.StdEncoding.EncodeToString([]byte("1\n00:00:01,000 --> 00:00:02,000\nSub\n"))
	for i := 0; i < 2; i++ {
		wSubUp := doReq(adminAC, http.MethodPost, "/Items/"+lockedMovie.ID+"/Subtitles", `{"Language":"eng","Format":"srt","IsForced":true,"Data":"`+subB64+`"}`)
		if wSubUp.Code != http.StatusNoContent {
			t.Fatalf("UploadSubtitle #%d failed: %d %s", i, wSubUp.Code, wSubUp.Body.String())
		}
	}
	if code := doReq(adminAC, http.MethodPost, "/Items/"+lockedMovie.ID+"/Subtitles?MediaSourceId="+lockedMovie.ID, `{"Language":"jpn","Format":"srt","Data":"`+subB64+`"}`).Code; code != http.StatusNoContent {
		t.Fatalf("UploadSubtitle with explicit MediaSourceId failed: %d", code)
	}
	// Also exercise requestedMSID == itemID when item has no media_sources row (reaches FK error -> 500)
	_ = doReq(adminAC, http.MethodPost, "/Items/"+movie.ID+"/Subtitles?MediaSourceId="+movie.ID, `{"Language":"kor","Format":"srt","Data":"`+subB64+`"}`)
	if code := doReq(adminAC, http.MethodPost, "/Items/"+movie.ID+"/Subtitles?MediaSourceId="+guid.New(), `{"Language":"jpn","Format":"srt","Data":"`+subB64+`"}`).Code; code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown MediaSourceId in UploadSubtitle, got %d", code)
	}

	// (b) ItemFile 200 download, missing file 404, permission denied 403, and downloading disabled 403
	wFileOK := doReq(adminAC, http.MethodGet, "/Items/"+movie.ID+"/File", "")
	if wFileOK.Code != http.StatusOK || wFileOK.Body.String() != "MP4-BYTES" {
		t.Fatalf("ItemFile 200 failed: %d %q", wFileOK.Code, wFileOK.Body.String())
	}
	noDLPol := viewer.Policy
	noDLPol.EnableContentDownloading = false
	_ = userRepo.UpdateUserPolicy(ctx, viewer.ID, noDLPol)
	if code := doReq(viewerAC, http.MethodGet, "/Items/"+movie.ID+"/File", "").Code; code != http.StatusForbidden {
		t.Fatalf("expected 403 when EnableContentDownloading=false, got %d", code)
	}
	goneMovie := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: lib.ID, ParentID: lib.ID,
		Type: "Movie", MediaType: "Video", Name: "Gone", OfficialRating: "G", Path: filepath.Join(libDir1, "missing.mp4"),
	}
	_ = env.repo.UpsertItem(ctx, goneMovie)
	if code := doReq(adminAC, http.MethodGet, "/Items/"+goneMovie.ID+"/File", "").Code; code != http.StatusNotFound {
		t.Fatalf("expected 404 for missing ItemFile on disk, got %d", code)
	}
	if os.Geteuid() != 0 {
		noPermImg := filepath.Join(libDir1, "noperm.png")
		_ = os.WriteFile(noPermImg, pngBytes, 0o644)
		_ = os.Chmod(noPermImg, 0o000)
		t.Cleanup(func() { _ = os.Chmod(noPermImg, 0o644) })

		noPermItem := &sqlite.ItemRecord{
			ID: guid.New(), LibraryID: lib.ID, ParentID: lib.ID,
			Type: "Movie", MediaType: "Video", Name: "NoPerm", OfficialRating: "G", Path: noPermImg,
		}
		_ = env.repo.UpsertItem(ctx, noPermItem)
		_ = env.repo.UpsertImage(ctx, &sqlite.ImageRecord{ItemID: noPermItem.ID, ImageType: "Primary", ImageIndex: 0, Path: noPermImg, Tag: "np-tag"})
		_ = env.repo.ReplaceItemTaxonomies(ctx, noPermItem.ID, "studio", []string{"NoPermStudio"})
		var npStudioID string
		_ = env.store.Reader.QueryRowContext(ctx, `SELECT id FROM taxonomies WHERE LOWER(name) = 'nopermstudio'`).Scan(&npStudioID)
		_ = env.repo.UpsertImage(ctx, &sqlite.ImageRecord{ItemID: npStudioID, ImageType: "Primary", ImageIndex: 0, Path: noPermImg, Tag: "nps-tag"})

		if code := doReq(adminAC, http.MethodGet, "/Items/"+noPermItem.ID+"/File", "").Code; code != http.StatusForbidden {
			t.Fatalf("expected 403 on unreadable ItemFile, got %d", code)
		}
		if code := doReq(adminAC, http.MethodGet, "/Items/"+noPermItem.ID+"/Images/Primary/0/np-tag/png/0/0/0/0", "").Code; code != http.StatusForbidden {
			t.Fatalf("expected 403 on unreadable full item image, got %d", code)
		}
		if code := doReq(adminAC, http.MethodGet, "/Studios/NoPermStudio/Images/Primary", "").Code; code != http.StatusForbidden {
			t.Fatalf("expected 403 on unreadable entity image, got %d", code)
		}
	}

	// (c) ServeItemImageFull & EntityImage edge cases (negative index, query ?tag=, restricted person/artist 404)
	if code := doReq(nil, http.MethodGet, "/Items/"+movie.ID+"/Images/Primary/-1/m-prim-0/png/0/0/0/0", "").Code; code != http.StatusBadRequest {
		t.Fatalf("expected 400 for negative index on full image route, got %d", code)
	}
	if code := doReq(nil, http.MethodGet, "/Items/"+movie.ID+"/Images/Primary/0/0/png/0/0/0/0?tag=m-prim-0", "").Code; code != http.StatusOK {
		t.Fatalf("expected 200 using ?tag= query on full image route, got %d", code)
	}
	if code := doReq(nil, http.MethodGet, "/Items/"+movie.ID+"/Images/Primary/0/wrongtag00000000/png/0/0/0/0", "").Code; code != http.StatusNotFound {
		t.Fatalf("expected 404 for wrong tag on full image route, got %d", code)
	}
	secretArtist := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: secretLib.ID, ParentID: secretLib.ID,
		Type: "MusicArtist", Name: "SecretArtist", OfficialRating: "G",
	}
	_ = env.repo.UpsertItem(ctx, secretArtist)
	_ = env.repo.UpsertImage(ctx, &sqlite.ImageRecord{ItemID: secretArtist.ID, ImageType: "Primary", ImageIndex: 0, Path: imgPath, Tag: "sa-tag"})
	_ = env.repo.ReplaceItemPeople(ctx, secretMovie.ID, []sqlite.PersonInput{{Name: "SecretActor", Type: "Actor"}})
	secPeople, _ := env.repo.GetPeopleByItemID(ctx, secretMovie.ID)
	if len(secPeople) > 0 {
		_ = env.repo.UpsertImage(ctx, &sqlite.ImageRecord{ItemID: secPeople[0].Id, ImageType: "Primary", ImageIndex: 0, Path: imgPath, Tag: "sp-tag"})
		if code := doReq(viewerAC, http.MethodGet, "/Persons/SecretActor/Images/Primary", "").Code; code != http.StatusNotFound {
			t.Fatalf("expected 404 for restricted person entity image, got %d", code)
		}
	}
	if code := doReq(viewerAC, http.MethodGet, "/Artists/SecretArtist/Images/Primary", "").Code; code != http.StatusNotFound {
		t.Fatalf("expected 404 for restricted artist entity image, got %d", code)
	}

	// (d) canDeleteItems & callerIdentity via UserIDFromCtx with userRepo attached, APIKey, and anonymous
	var dynUID string
	routerCtx := env.handler.BuildRouter(AuthFuncs{UserIDFromCtx: func(context.Context) string { return dynUID }})
	rTest := httptest.NewRequest(http.MethodDelete, "/Items/x", nil)
	if ok, _ := env.handler.canDeleteItems(rTest); ok {
		t.Fatalf("expected canDeleteItems=false when unauthenticated with userRepo")
	}
	dynUID = admin.ID
	if ok, adm := env.handler.canDeleteItems(rTest); !ok || !adm {
		t.Fatalf("expected canDeleteItems=(true,true) for admin via UserIDFromCtx")
	}
	dynUID = viewer.ID
	if ok, _ := env.handler.canDeleteItems(rTest); ok {
		t.Fatalf("expected canDeleteItems=false for viewer via UserIDFromCtx")
	}
	dynUID = "missing-uid"
	if ok, _ := env.handler.canDeleteItems(rTest); ok {
		t.Fatalf("expected canDeleteItems=false for missing user via UserIDFromCtx")
	}
	if ok, adm := env.handler.canDeleteItems(rTest.WithContext(middleware.WithAuthContext(ctx, &middleware.AuthContext{IsAPIKey: true}))); !ok || !adm {
		t.Fatalf("expected canDeleteItems=(true,true) for APIKey")
	}
	// Public playlist check via UserIDFromCtx on routerCtx
	dynUID = admin.ID
	wMakePub := httptest.NewRecorder()
	routerCtx.ServeHTTP(wMakePub, httptest.NewRequest(http.MethodPost, "/Items/"+pl.ID+"/MakePublic", nil))
	dynUID = noTransUser.ID
	wViewPub := httptest.NewRecorder()
	routerCtx.ServeHTTP(wViewPub, httptest.NewRequest(http.MethodGet, "/Playlists/"+pl.ID+"/AddToPlaylistInfo", nil))
	if wViewPub.Code != http.StatusOK {
		t.Fatalf("expected public playlist AddToPlaylistInfo 200 via UserIDFromCtx, got %d", wViewPub.Code)
	}

	// Finally delete SecretCinema by name (exercises DeleteVirtualFolder by name + watcher.UnwatchLibrary)
	// Before deleting SecretCinema, exercise CollectionProviderItems filtering/pagination, ContainerItemsDelete via EntryIds JSON, and InstantMix by Artist/MusicGenre
	colTest, _ := env.repo.CreateCollection(ctx, "FilteredBox", "", []string{movie.ID, secretMovie.ID, pl.ID})
	wCol := doReq(viewerAC, http.MethodGet, "/Collections/"+colTest.ID+"/ProviderItems?StartIndex=0&Limit=1", "")
	if wCol.Code != http.StatusOK {
		t.Fatalf("CollectionProviderItems filtered failed: %d", wCol.Code)
	}
	rDelCol := httptest.NewRequest(http.MethodPost, "/Collections/"+colTest.ID+"/Items/Delete", strings.NewReader(`{"EntryIds":["`+movie.ID+`"]}`))
	rDelCol.Header.Set("Content-Type", "application/json")
	rDelCol = rDelCol.WithContext(middleware.WithAuthContext(ctx, adminAC))
	wDelCol := httptest.NewRecorder()
	router.ServeHTTP(wDelCol, rDelCol)
	if wDelCol.Code != http.StatusNoContent {
		t.Fatalf("Collection Items/Delete EntryIds failed: %d", wDelCol.Code)
	}

	audItem := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: lib.ID, ParentID: artist.ID,
		Type: "Audio", MediaType: "Audio", Name: "One More Time", OfficialRating: "G", Path: mediaPath,
	}
	_ = env.repo.UpsertItem(ctx, audItem)
	_ = env.repo.ReplaceItemTaxonomies(ctx, audItem.ID, "genre", []string{"SciFi"})
	if code := doReq(viewerAC, http.MethodGet, "/Artists/InstantMix?ArtistIds="+artist.ID+"&Limit=1", "").Code; code != http.StatusOK {
		t.Fatalf("Artists InstantMix failed: %d", code)
	}
	if code := doReq(viewerAC, http.MethodGet, "/MusicGenres/SciFi/InstantMix?MusicGenreIds="+genreID+"&Limit=1", "").Code; code != http.StatusOK {
		t.Fatalf("MusicGenres InstantMix failed: %d", code)
	}

	// Push compatmedia past >= 90%: saveSubtitleBytesAndRegisterStream, readFirstLineStrmURL, Visibility RBAC, ChannelLatest multi-channel, nil imaging
	if err := env.handler.saveSubtitleBytesAndRegisterStream(ctx, movie.ID, "", "", filepath.Join(t.TempDir(), "out.mkv"), "!!!", ".weird", true, []byte("s"), false); !errors.Is(err, errDeleteForbidden) {
		t.Fatalf("expected errDeleteForbidden for outside path without cache fallback, got %v", err)
	}
	if err := env.handler.saveSubtitleBytesAndRegisterStream(ctx, movie.ID, "", "", "", "eng", "srt", false, []byte("s"), false); !errors.Is(err, errDeleteForbidden) {
		t.Fatalf("expected errDeleteForbidden for empty path without cache fallback, got %v", err)
	}
	badStrm := filepath.Join(libDir1, "bad_proto.strm")
	_ = os.WriteFile(badStrm, []byte("# comment\nftp://bad.example/file.mkv\n"), 0o644)
	if _, err := readFirstLineStrmURL(badStrm); err == nil {
		t.Fatalf("expected error for non-http strm")
	}
	emptyStrm := filepath.Join(libDir1, "empty.strm")
	_ = os.WriteFile(emptyStrm, []byte("# only comment\n"), 0o644)
	if _, err := readFirstLineStrmURL(emptyStrm); err == nil {
		t.Fatalf("expected error for empty strm")
	}
	if _, err := readFirstLineStrmURL(filepath.Join(libDir1, "nofile.strm")); err == nil {
		t.Fatalf("expected error for missing strm")
	}
	if computeSyntheticLibraryCoverTag("", "") != "" || constantTimeTagEqual("", "") {
		t.Fatalf("unexpected empty tag helper result")
	}
	if code := doReq(nil, http.MethodGet, "/Items/"+guid.New()+"/Images/Primary/0/sometag123456789/jpg/0/0/0/0", "").Code; code != http.StatusNotFound {
		t.Fatalf("expected 404 on full image route for missing item, got %d", code)
	}
	wNoImg := httptest.NewRecorder()
	hNoUR.BuildRouter(AuthFuncs{}).ServeHTTP(wNoImg, httptest.NewRequest(http.MethodGet, "/Items/"+movie.ID+"/Images/Primary/0/tag/jpg/0/0/0/0", nil))
	if wNoImg.Code != http.StatusNotFound {
		t.Fatalf("expected 404 when imaging service is nil, got %d", wNoImg.Code)
	}

	// Visibility RBAC (404 missing, 403 non-playlist, 403 non-owner playlist)
	if code := doReq(viewerAC, http.MethodPost, "/Items/"+guid.New()+"/MakePublic", "").Code; code != http.StatusNotFound {
		t.Fatalf("expected 404 on MakePublic missing item, got %d", code)
	}
	if code := doReq(viewerAC, http.MethodPost, "/Items/"+movie.ID+"/MakePublic", "").Code; code != http.StatusForbidden {
		t.Fatalf("expected 403 on non-admin MakePublic movie, got %d", code)
	}
	adminPL, _ := env.repo.CreatePlaylist(ctx, "AdminOnlyPL", "Video", admin.ID, nil)
	if code := doReq(viewerAC, http.MethodPost, "/Items/"+adminPL.ID+"/MakePrivate", "").Code; code != http.StatusForbidden {
		t.Fatalf("expected 403 on non-owner MakePrivate playlist, got %d", code)
	}

	// Multi-channel /Channels/Items/Latest merge & sort
	ch1, ch2 := guid.New(), guid.New()
	chItem1 := &sqlite.ItemRecord{ID: guid.New(), LibraryID: lib.ID, ParentID: ch1, Type: "ChannelVideoItem", MediaType: "Video", Name: "C1", OfficialRating: "G"}
	chItem2 := &sqlite.ItemRecord{ID: guid.New(), LibraryID: lib.ID, ParentID: ch2, Type: "ChannelVideoItem", MediaType: "Video", Name: "C2", OfficialRating: "G"}
	_ = env.repo.UpsertItem(ctx, chItem1)
	_ = env.repo.UpsertItem(ctx, chItem2)
	if code := doReq(viewerAC, http.MethodGet, "/Channels/Items/Latest?ChannelIds="+ch1+","+ch2+"&StartIndex=0&Limit=1", "").Code; code != http.StatusOK {
		t.Fatalf("multi-channel Latest failed: %d", code)
	}

	// Batch delete 403 when non-admin includes forbidden item
	delViewerPol := viewer.Policy
	delViewerPol.EnableContentDeletion = true
	_ = userRepo.UpdateUserPolicy(ctx, viewer.ID, delViewerPol)
	if code := doReq(viewerAC, http.MethodDelete, "/Items?Ids="+secretMovie.ID, "").Code; code != http.StatusForbidden {
		t.Fatalf("expected 403 on batch delete of inaccessible item, got %d", code)
	}

	// RemoteSearch/Apply (404 + 204 with ProductionYear/ProviderIds), Shows/Upcoming, AudioBooks/NextUp, Attachments & Ancestors 400/403
	if code := doReq(adminAC, http.MethodPost, "/Items/RemoteSearch/Apply/"+guid.New(), `{}`).Code; code != http.StatusNotFound {
		t.Fatalf("expected 404 on RemoteSearch/Apply missing item, got %d", code)
	}
	rApply := httptest.NewRequest(http.MethodPost, "/Items/RemoteSearch/Apply/"+movie.ID+"?ReplaceAllImages=false",
		strings.NewReader(`{"Name":"Applied Film","Overview":"Applied Overview","ProductionYear":2025,"ProviderIds":{"Tmdb":"777"}}`))
	rApply.Header.Set("Content-Type", "application/json")
	rApply = rApply.WithContext(middleware.WithAuthContext(ctx, adminAC))
	wApply := httptest.NewRecorder()
	router.ServeHTTP(wApply, rApply)
	if wApply.Code != http.StatusNoContent {
		t.Fatalf("RemoteSearch/Apply expected 204, got %d", wApply.Code)
	}

	futureTS := int64(4102444800) // 2100-01-01 UTC
	futureEp := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: lib.ID, ParentID: lib.ID,
		Type: "Episode", MediaType: "Video", Name: "Future Ep", OfficialRating: "G", PremiereDate: &futureTS,
	}
	_ = env.repo.UpsertItem(ctx, futureEp)
	if code := doReq(viewerAC, http.MethodGet, "/Shows/Upcoming?StartIndex=0&Limit=1", "").Code; code != http.StatusOK {
		t.Fatalf("Shows/Upcoming expected 200, got %d", code)
	}
	for _, forbiddenURL := range []string{
		"/Shows/Upcoming?UserId=" + admin.ID,
		"/AudioBooks/NextUp?UserId=" + admin.ID,
		"/Items/" + movie.ID + "/Ancestors?UserId=" + admin.ID,
		"/Items/" + secretMovie.ID + "/Ancestors",
		"/Videos/" + secretMovie.ID + "/ms1/Attachments",
		"/Videos/" + secretMovie.ID + "/ms1/Attachments/0",
	} {
		if code := doReq(viewerAC, http.MethodGet, forbiddenURL, "").Code; code != http.StatusForbidden {
			t.Fatalf("GET %s expected 403, got %d", forbiddenURL, code)
		}
	}
	if code := doReq(adminAC, http.MethodGet, "/Videos/"+movie.ID+"/ms1/Attachments/-1", "").Code; code != http.StatusBadRequest {
		t.Fatalf("expected 400 on negative attachment index, got %d", code)
	}

	dynUID = admin.ID
	wDelVF := httptest.NewRecorder()
	routerCtx.ServeHTTP(wDelVF, httptest.NewRequest(http.MethodPost, "/Library/VirtualFolders/Delete?name=SecretCinema", nil))
	if wDelVF.Code != http.StatusNoContent {
		t.Fatalf("DeleteVirtualFolder by name failed: %d", wDelVF.Code)
	}
}

func TestV0110_ClosedStoreGracefulErrorHandling(t *testing.T) {
	env := newTestEnv(t)
	_ = env.store.Close()

	for _, tc := range []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodPost, "/Items/id1/MakePublic", ""},
		{http.MethodPost, "/Items/RemoteSearch/Apply/id1", `{}`},
		{http.MethodGet, "/Items/id1/Similar", ""},
		{http.MethodGet, "/Items/id1/InstantMix", ""},
		{http.MethodGet, "/Items/id1/LocalTrailers", ""},
		{http.MethodGet, "/Items/id1/SpecialFeatures", ""},
		{http.MethodGet, "/Items/id1/ThemeSongs", ""},
		{http.MethodGet, "/Items/id1/ThemeMedia", ""},
		{http.MethodDelete, "/Items/id1/Subtitles/0", ""},
		{http.MethodGet, "/Items/id1/RemoteSearch/Subtitles/eng", ""},
		{http.MethodPost, "/Items/id1/RemoteSearch/Subtitles/opensub-mock:sub-1", ""},
		{http.MethodGet, "/Items/id1/RemoteImages", ""},
		{http.MethodPost, "/Items/id1/RemoteImages/Download?Type=Primary&ImageUrl=https://93.184.216.34/a.png", ""},
		{http.MethodPost, "/Items/id1/Images/Primary", ""},
		{http.MethodPost, "/Items/id1/Subtitles", `{"Language":"eng","Format":"srt","Data":"YWJj"}`},
		{http.MethodGet, "/Items/id1/File", ""},
		{http.MethodGet, "/Collections/id1/ProviderItems", ""},
		{http.MethodPost, "/Collections/id1/Items/Delete", `{}`},
		{http.MethodGet, "/Playlists/id1/AddToPlaylistInfo", ""},
		{http.MethodGet, "/Items/id1/Images", ""},
		{http.MethodGet, "/Items/id1/Images/Primary/0/tag123/jpg/0/0/0/0", ""},
	} {
		r := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
		if tc.body != "" {
			r.Header.Set("Content-Type", "application/json")
		}
		w := httptest.NewRecorder()
		env.router.ServeHTTP(w, r)
		if w.Code < 400 && w.Code != http.StatusOK && w.Code != http.StatusNoContent {
			t.Fatalf("%s %s on closed store got unexpected %d", tc.method, tc.path, w.Code)
		}
	}
}
