package compatmedia

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"log/slog"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ambra-media/ambra/internal/api/middleware"
	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/core/auth"
	"github.com/ambra-media/ambra/internal/core/imaging"
	"github.com/ambra-media/ambra/internal/core/transcode"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/plugins"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestCompatMediaV0107AuditSecurityAndFixes(t *testing.T) {
	tmp := t.TempDir()
	cfg := config.Default(tmp)
	st, err := sqlite.Open(cfg.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	ctx := context.Background()
	repo := sqlite.NewLibraryRepo(st)
	userRepo := sqlite.NewUserRepo(st)
	imgSvc := imaging.New(repo).WithCacheDir(cfg.ImageCacheDir())
	mgr := transcode.NewManager(cfg, transcode.WithCapabilities(transcode.SoftwareOnlyCapabilities()))
	defer mgr.Close()

	libDir := filepath.Join(tmp, "media")
	_ = os.MkdirAll(libDir, 0o755)
	lib, _ := repo.CreateLibrary(ctx, "Movies", "movies", []string{libDir})
	lib2, _ := repo.CreateLibrary(ctx, "Other", "movies", []string{libDir})

	alice := &sqlite.UserRecord{ID: guid.New(), Name: "alice", Policy: sqlite.DefaultUserPolicy(false), Config: sqlite.DefaultUserConfig()}
	bob := &sqlite.UserRecord{ID: guid.New(), Name: "bob", Policy: sqlite.DefaultUserPolicy(false), Config: sqlite.DefaultUserConfig()}
	admin := &sqlite.UserRecord{ID: guid.New(), Name: "admin", IsAdmin: true, Policy: sqlite.DefaultUserPolicy(true), Config: sqlite.DefaultUserConfig()}
	alice.Policy.EnableAllFolders = true
	alice.Policy.EnableContentDownloading = true
	bob.Policy.EnableAllFolders = false
	bob.Policy.EnabledFolders = []string{lib2.ID}
	for _, u := range []*sqlite.UserRecord{alice, bob, admin} {
		_ = userRepo.CreateUser(ctx, u)
	}

	h := New(repo, nil, imgSvc, plugins.NewRegistry(), st, cfg)
	h.SetUserRepo(userRepo)
	h.SetTranscodeManager(mgr)
	router := h.BuildRouter(AuthFuncs{})

	doAs := func(ac *middleware.AuthContext, method, target, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, target, strings.NewReader(body))
		if ac != nil {
			req = req.WithContext(middleware.WithAuthContext(req.Context(), ac))
		}
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}

	aliceAC := &middleware.AuthContext{User: alice, Client: auth.ClientInfo{DeviceID: "alice-dev"}}
	bobAC := &middleware.AuthContext{User: bob, Client: auth.ClientInfo{DeviceID: "bob-dev"}}
	adminAC := &middleware.AuthContext{User: admin, Client: auth.ClientInfo{DeviceID: "admin-dev"}}

	// --- H-1: handleItemFile rejects folders/directories, sets nosniff+CSP sandbox, strips CRLF ---
	subDir := filepath.Join(libDir, "folder_dir")
	_ = os.MkdirAll(subDir, 0o755)
	folderItem := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: lib.ID, ParentID: lib.ID,
		Type: "Folder", IsFolder: true, Name: "Folder", Path: subDir,
	}
	dirNonFlagItem := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: lib.ID, ParentID: lib.ID,
		Type: "Movie", IsFolder: false, Name: "DirPath", Path: subDir,
	}
	realFile := filepath.Join(libDir, "normal.mkv")
	_ = os.WriteFile(realFile, []byte("MKVDATA"), 0o644)
	fileItem := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: lib.ID, ParentID: lib.ID,
		Type: "Movie", MediaType: "Video", Name: "Normal", Path: realFile,
	}
	for _, it := range []*sqlite.ItemRecord{folderItem, dirNonFlagItem, fileItem} {
		_ = repo.UpsertItem(ctx, it)
	}

	if rec := doAs(aliceAC, http.MethodGet, "/Items/"+folderItem.ID+"/File", ""); rec.Code != http.StatusBadRequest {
		t.Fatalf("H-1 expected 400 for IsFolder item, got %d", rec.Code)
	}
	if rec := doAs(aliceAC, http.MethodGet, "/Items/"+dirNonFlagItem.ID+"/File", ""); rec.Code != http.StatusBadRequest {
		t.Fatalf("H-1 expected 400 for directory path item, got %d", rec.Code)
	}
	recFile := doAs(aliceAC, http.MethodGet, "/Items/"+fileItem.ID+"/File", "")
	if recFile.Code != http.StatusOK {
		t.Fatalf("H-1 expected 200 for regular file, got %d", recFile.Code)
	}
	if recFile.Header().Get("X-Content-Type-Options") != "nosniff" || recFile.Header().Get("Content-Security-Policy") != "sandbox" {
		t.Fatalf("H-1 missing security headers: nosniff=%q csp=%q",
			recFile.Header().Get("X-Content-Type-Options"), recFile.Header().Get("Content-Security-Policy"))
	}
	if cleaned := sanitizeContentDispositionFilename("evil\r\nX-Inject: 1.mkv"); strings.ContainsAny(cleaned, "\r\n") {
		t.Fatalf("H-1 CRLF not stripped from filename: %q", cleaned)
	}

	// --- H-2: POST /Videos/ActiveEncodings/Delete aligns with hls StopSessionsScoped + DeviceID + admin suffix ---
	_, _ = mgr.RegisterSession(transcode.SessionParams{
		UserID: alice.ID, DeviceID: "alice-dev", PlaySessionID: alice.ID + ":ps-alice",
		ItemID: fileItem.ID, MediaSourceID: "ms1", InputPath: realFile,
	})
	_, _ = mgr.RegisterSession(transcode.SessionParams{
		UserID: "", DeviceID: "api-dev", PlaySessionID: "ps-api",
		ItemID: fileItem.ID, MediaSourceID: "ms2", InputPath: realFile,
	})
	// Bob spoofing alice-dev/ps-alice or api-dev/ps-api cannot stop them
	_ = doAs(bobAC, http.MethodPost, "/Videos/ActiveEncodings/Delete?DeviceId=alice-dev&PlaySessionId=ps-alice", "")
	_ = doAs(bobAC, http.MethodPost, "/Videos/ActiveEncodings/Delete?DeviceId=api-dev&PlaySessionId=ps-api", "")
	if len(mgr.ActiveSessions()) != 2 {
		t.Fatalf("H-2 bob must not stop alice/api sessions, got %d", len(mgr.ActiveSessions()))
	}
	// Alice can stop her own session via raw PlaySessionId=ps-alice (auto-prefixed with alice.ID+":")
	_ = doAs(aliceAC, http.MethodPost, "/Videos/ActiveEncodings/Delete?PlaySessionId=ps-alice", "")
	if len(mgr.ActiveSessions()) != 1 {
		t.Fatalf("H-2 alice should stop her own scoped session, remaining=%d", len(mgr.ActiveSessions()))
	}
	// Re-register alice session and verify admin can stop by raw PlaySessionId suffix
	_, _ = mgr.RegisterSession(transcode.SessionParams{
		UserID: alice.ID, DeviceID: "alice-dev", PlaySessionID: alice.ID + ":ps-shared",
		ItemID: fileItem.ID, MediaSourceID: "ms1", InputPath: realFile,
	})
	_ = doAs(adminAC, http.MethodPost, "/Videos/ActiveEncodings/Delete?PlaySessionId=ps-shared", "")
	if len(mgr.ActiveSessions()) != 1 {
		t.Fatalf("H-2 admin should stop scoped session by suffix, remaining=%d", len(mgr.ActiveSessions()))
	}
	_ = doAs(adminAC, http.MethodPost, "/Videos/ActiveEncodings/Delete?PlaySessionId=ps-api", "")
	if len(mgr.ActiveSessions()) != 0 {
		t.Fatalf("H-2 admin should stop api session, remaining=%d", len(mgr.ActiveSessions()))
	}

	// --- M-5 + M-11: Subtitle private cache, STRM resolution + SSRF block, MediaSourceId subtitle upload ---
	ver1Path := filepath.Join(libDir, "Movie - 1080p.mkv")
	ver2Path := filepath.Join(libDir, "Movie - 4K.mkv")
	sub1Path := filepath.Join(libDir, "Movie - 1080p.en.srt")
	_ = os.WriteFile(ver1Path, []byte("V1"), 0o644)
	_ = os.WriteFile(ver2Path, []byte("V2"), 0o644)
	_ = os.WriteFile(sub1Path, []byte("1\n00:00:01,000 --> 00:00:02,000\nHello\n"), 0o644)
	multiItem := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: lib.ID, ParentID: lib.ID,
		Type: "Movie", MediaType: "Video", Name: "Multi Movie", Path: ver1Path,
	}
	_ = repo.UpsertItem(ctx, multiItem)
	ms1ID := guid.New()
	ms2ID := guid.New()
	_ = repo.ReplaceMediaSourcesAndStreams(ctx, multiItem.ID, []sqlite.MediaSourceRecord{
		{
			ID: ms1ID, ItemID: multiItem.ID, Name: "1080p", Path: ver1Path, Container: "mkv",
			Streams: []sqlite.MediaStreamRecord{
				{MediaSourceID: ms1ID, ItemID: multiItem.ID, StreamIndex: 0, StreamType: "Video", Codec: "h264", Width: 2560, Height: 1080},
				{MediaSourceID: ms1ID, ItemID: multiItem.ID, StreamIndex: 1, StreamType: "Subtitle", Codec: "srt", IsExternal: true, Path: sub1Path},
			},
		},
		{
			ID: ms2ID, ItemID: multiItem.ID, Name: "4K", Path: ver2Path, Container: "mkv",
			Streams: []sqlite.MediaStreamRecord{
				{MediaSourceID: ms2ID, ItemID: multiItem.ID, StreamIndex: 0, StreamType: "Video", Codec: "hevc", Width: 800, Height: 600},
			},
		},
	})

	// M-5: handleSubtitleStream returns private Cache-Control
	recSub := doAs(aliceAC, http.MethodGet, "/Items/"+multiItem.ID+"/"+ms1ID+"/Subtitles/1/Stream.vtt", "")
	if recSub.Code != http.StatusOK || recSub.Header().Get("Cache-Control") != "private, max-age=3600" {
		t.Fatalf("M-5 subtitle expected 200 private cache, got %d cc=%q", recSub.Code, recSub.Header().Get("Cache-Control"))
	}

	// M-11: handleUploadSubtitle with MediaSourceId targets version 2 (4K)
	subPayload, _ := json.Marshal(map[string]any{
		"Language": "zh",
		"Format":   "srt",
		"Data":     base64.StdEncoding.EncodeToString([]byte("1\n00:00:01,000 --> 00:00:02,000\n4K sub\n")),
	})
	recUp := doAs(adminAC, http.MethodPost, "/Videos/"+multiItem.ID+"/Subtitles?MediaSourceId="+ms2ID, string(subPayload))
	if recUp.Code != http.StatusNoContent {
		t.Fatalf("M-11 upload subtitle with MediaSourceId got %d body=%q", recUp.Code, recUp.Body.String())
	}
	want4KSub := filepath.Join(libDir, "Movie - 4K.zh.srt")
	if _, err := os.Stat(want4KSub); err != nil {
		t.Fatalf("M-11 expected subtitle next to 4K media source %s: %v", want4KSub, err)
	}
	// Unknown MediaSourceId -> 404
	recUpMiss := doAs(adminAC, http.MethodPost, "/Videos/"+multiItem.ID+"/Subtitles?MediaSourceId="+guid.New(), string(subPayload))
	if recUpMiss.Code != http.StatusNotFound {
		t.Fatalf("M-11 expected 404 for unknown MediaSourceId, got %d", recUpMiss.Code)
	}

	// M-11: STRM SSRF blocking in handleSubtitleStream, handleListVideoAttachments, handleVideoAttachmentStream
	strmFile := filepath.Join(libDir, "bad.strm")
	_ = os.WriteFile(strmFile, []byte("http://169.254.169.254/latest/meta-data/\n"), 0o644)
	strmItem := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: lib.ID, ParentID: lib.ID,
		Type: "Movie", MediaType: "Video", Name: "STRM", Path: strmFile, IsStrm: true,
	}
	_ = repo.UpsertItem(ctx, strmItem)
	strmMSID := guid.New()
	_ = repo.ReplaceMediaSourcesAndStreams(ctx, strmItem.ID, []sqlite.MediaSourceRecord{{
		ID: strmMSID, ItemID: strmItem.ID, Name: "bad.strm", Path: strmFile, Container: "strm", IsRemote: true,
		Streams: []sqlite.MediaStreamRecord{
			{MediaSourceID: strmMSID, ItemID: strmItem.ID, StreamIndex: 2, StreamType: "Subtitle", Codec: "srt", IsExternal: false},
		},
	}})

	for _, ep := range []string{
		"/Items/" + strmItem.ID + "/" + strmMSID + "/Subtitles/2/Stream.vtt",
		"/Videos/" + strmItem.ID + "/" + strmMSID + "/Attachments",
		"/Videos/" + strmItem.ID + "/" + strmMSID + "/Attachments/0",
	} {
		rec := doAs(aliceAC, http.MethodGet, ep, "")
		if rec.Code != http.StatusBadGateway {
			t.Fatalf("M-11 GET %s expected 502 for SSRF .strm, got %d body=%q", ep, rec.Code, rec.Body.String())
		}
	}
	// Custom SetStreamResolver returning safe URL is invoked
	resolvedCalled := false
	h.SetStreamResolver(func(_ context.Context, rawPath string) (string, error) {
		resolvedCalled = true
		return "https://93.184.216.34/video.mkv", nil
	})
	if u, err := h.resolveExtractionMediaPath(ctx, strmFile); err != nil || !resolvedCalled || u != "https://93.184.216.34/video.mkv" {
		t.Fatalf("M-11 SetStreamResolver failed: called=%v u=%q err=%v", resolvedCalled, u, err)
	}

	// --- L-15: ThumbnailSet 403 + real AspectRatio; AlternateSources/MergeVersions errors; DeleteItemImage filepath.Rel ---
	// Bob cannot access lib1 -> 403 on ThumbnailSet and thumbnails
	for _, ep := range []string{"/Items/" + multiItem.ID + "/ThumbnailSet", "/Items/" + multiItem.ID + "/thumbnails"} {
		if rec := doAs(bobAC, http.MethodGet, ep, ""); rec.Code != http.StatusForbidden {
			t.Fatalf("L-15 bob expected 403 on %s, got %d", ep, rec.Code)
		}
	}
	// Alice gets real aspect ratio: ms1 (2560/1080) and ms2 (800/600)
	recTS1 := doAs(aliceAC, http.MethodGet, "/Items/"+multiItem.ID+"/ThumbnailSet?MediaSourceId="+ms1ID, "")
	var ts1 struct {
		AspectRatio float64 `json:"AspectRatio"`
	}
	_ = json.Unmarshal(recTS1.Body.Bytes(), &ts1)
	if math.Abs(ts1.AspectRatio-(2560.0/1080.0)) > 0.01 {
		t.Fatalf("L-15 expected aspect ratio ~2.370, got %v", ts1.AspectRatio)
	}
	recTS2 := doAs(aliceAC, http.MethodGet, "/Items/"+multiItem.ID+"/ThumbnailSet?MediaSourceId="+ms2ID, "")
	var ts2 struct {
		AspectRatio float64 `json:"AspectRatio"`
	}
	_ = json.Unmarshal(recTS2.Body.Bytes(), &ts2)
	if math.Abs(ts2.AspectRatio-(800.0/600.0)) > 0.01 {
		t.Fatalf("L-15 expected aspect ratio ~1.333, got %v", ts2.AspectRatio)
	}

	// L-15: handleDeleteAlternateSources returns 404 for unknown item
	if rec := doAs(adminAC, http.MethodDelete, "/Videos/"+guid.New()+"/AlternateSources", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("L-15 expected 404 on SplitItemVersions unknown item, got %d", rec.Code)
	}
	// L-15: handleMergeVersions returns 400 on <2 ids, 404 on missing primary, 400 on folder merge, 204 on valid split
	if rec := doAs(adminAC, http.MethodPost, "/Videos/MergeVersions?Ids=onlyone", ""); rec.Code != http.StatusBadRequest {
		t.Fatalf("L-15 expected 400 on <2 ids MergeVersions, got %d", rec.Code)
	}
	if rec := doAs(adminAC, http.MethodPost, "/Videos/MergeVersions?Ids="+guid.New()+","+guid.New(), ""); rec.Code != http.StatusNotFound {
		t.Fatalf("L-15 expected 404 on missing primary MergeVersions, got %d", rec.Code)
	}
	if rec := doAs(adminAC, http.MethodPost, "/Videos/MergeVersions?Ids="+folderItem.ID+","+fileItem.ID, ""); rec.Code != http.StatusBadRequest {
		t.Fatalf("L-15 expected 400 on folder MergeVersions, got %d", rec.Code)
	}
	if rec := doAs(adminAC, http.MethodDelete, "/Videos/"+multiItem.ID+"/AlternateSources", ""); rec.Code != http.StatusNoContent {
		t.Fatalf("L-15 expected 204 on valid SplitItemVersions, got %d body=%q", rec.Code, rec.Body.String())
	}

	// L-15: handleDeleteItemImage uses filepath.Rel (does not delete sibling prefix dir metadata/images-evil)
	evilDir := filepath.Join(tmp, "metadata", "images-evil")
	_ = os.MkdirAll(evilDir, 0o755)
	evilFile := filepath.Join(evilDir, "keep.jpg")
	_ = os.WriteFile(evilFile, []byte("KEEP"), 0o644)
	_ = repo.UpsertImage(ctx, &sqlite.ImageRecord{
		ItemID: fileItem.ID, ImageType: "Primary", ImageIndex: 0, Path: evilFile, Tag: "t-evil",
	})
	if rec := doAs(adminAC, http.MethodDelete, "/Items/"+fileItem.ID+"/Images/Primary", ""); rec.Code != http.StatusNoContent {
		t.Fatalf("DeleteItemImage expected 204, got %d", rec.Code)
	}
	if _, err := os.Stat(evilFile); err != nil {
		t.Fatalf("L-15 DeleteItemImage deleted sibling prefix dir file %s!", evilFile)
	}

	// --- Task 1 / M-7: Permission denied on item file, subtitle, and image ---
	if runtime.GOOS != "windows" && os.Geteuid() != 0 {
		var logBuf bytes.Buffer
		oldLogger := slog.Default()
		slog.SetDefault(slog.New(slog.NewTextHandler(&logBuf, nil)))
		t.Cleanup(func() { slog.SetDefault(oldLogger) })

		var jpgBuf bytes.Buffer
		im := image.NewRGBA(image.Rect(0, 0, 4, 4))
		im.Set(0, 0, color.RGBA{G: 200, A: 255})
		_ = jpeg.Encode(&jpgBuf, im, nil)
		lockedImg := filepath.Join(libDir, "locked.jpg")
		_ = os.WriteFile(lockedImg, jpgBuf.Bytes(), 0o644)
		_ = repo.UpsertImage(ctx, &sqlite.ImageRecord{
			ItemID: fileItem.ID, ImageType: "Primary", ImageIndex: 0, Path: lockedImg, Tag: "tlock", Width: 4, Height: 4,
		})
		_ = os.Chmod(realFile, 0o000)
		_ = os.Chmod(sub1Path, 0o000)
		_ = os.Chmod(lockedImg, 0o000)
		t.Cleanup(func() {
			_ = os.Chmod(realFile, 0o644)
			_ = os.Chmod(sub1Path, 0o644)
			_ = os.Chmod(lockedImg, 0o644)
		})

		for _, tc := range []struct {
			name string
			url  string
			path string
		}{
			{"item file", "/Items/" + fileItem.ID + "/File", realFile},
			{"subtitle", "/Items/" + multiItem.ID + "/" + ms1ID + "/Subtitles/1/Stream.vtt", sub1Path},
			{"item image full", "/Items/" + fileItem.ID + "/Images/Primary/0/tlock/jpg/0/0/0/0", lockedImg},
		} {
			logBuf.Reset()
			rec := doAs(aliceAC, http.MethodGet, tc.url, "")
			if rec.Code != http.StatusForbidden {
				t.Fatalf("%s permission denied expected 403, got %d body=%q", tc.name, rec.Code, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), "permission denied reading media file") || strings.Contains(rec.Body.String(), tmp) {
				t.Fatalf("%s unexpected sanitized response: %q", tc.name, rec.Body.String())
			}
			logOut := logBuf.String()
			if !strings.Contains(logOut, "uid=") || !strings.Contains(logOut, "gid=") || !strings.Contains(logOut, tc.path) {
				t.Fatalf("%s expected uid/gid/path in slog output, got %q", tc.name, logOut)
			}
		}
	}

	// L-15: handleItemFile missing file (404) & forbidden library (403) & empty strm helper
	if rec := doAs(bobAC, http.MethodGet, "/Items/"+fileItem.ID+"/File", ""); rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for bob downloading lib1 file, got %d", rec.Code)
	}
	missingFileItem := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: lib.ID, ParentID: lib.ID,
		Type: "Movie", MediaType: "Video", Name: "Missing", Path: filepath.Join(libDir, "does-not-exist.mkv"),
	}
	_ = repo.UpsertItem(ctx, missingFileItem)
	if rec := doAs(aliceAC, http.MethodGet, "/Items/"+missingFileItem.ID+"/File", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for missing file on disk, got %d", rec.Code)
	}
	emptyStrm := filepath.Join(libDir, "empty.strm")
	_ = os.WriteFile(emptyStrm, []byte("# only comment\n\n"), 0o644)
	if _, err := readFirstLineStrmURL(emptyStrm); err == nil {
		t.Fatal("expected error for empty/comment-only .strm")
	}
	if _, err := readFirstLineStrmURL(filepath.Join(libDir, "missing.strm")); err == nil {
		t.Fatal("expected error for missing .strm")
	}

	// L-15: handleUploadItemImage / handleDeleteAlternateSources / handleMergeVersions return sanitized 500 on closed DB
	st.Close()
	if rec := doAs(adminAC, http.MethodPost, "/Items/"+fileItem.ID+"/Images/Primary", "dummy"); rec.Code != http.StatusInternalServerError || strings.Contains(rec.Body.String(), "sql:") {
		t.Fatalf("L-15 handleUploadItemImage on closed DB expected sanitized 500, got %d %q", rec.Code, rec.Body.String())
	}
	if rec := doAs(adminAC, http.MethodDelete, "/Videos/"+fileItem.ID+"/AlternateSources", ""); rec.Code != http.StatusInternalServerError || strings.Contains(rec.Body.String(), "sql:") {
		t.Fatalf("L-15 handleDeleteAlternateSources on closed DB expected sanitized 500, got %d %q", rec.Code, rec.Body.String())
	}
	if rec := doAs(adminAC, http.MethodPost, "/Videos/MergeVersions?Ids="+fileItem.ID+","+multiItem.ID, ""); rec.Code != http.StatusInternalServerError || strings.Contains(rec.Body.String(), "sql:") {
		t.Fatalf("L-15 handleMergeVersions on closed DB expected sanitized 500, got %d %q", rec.Code, rec.Body.String())
	}
}
