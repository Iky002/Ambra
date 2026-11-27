package streaming

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/ambra-media/ambra/internal/api/dto"
	"github.com/ambra-media/ambra/internal/api/middleware"
	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/core/imaging"
	"github.com/ambra-media/ambra/internal/core/playback"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/plugins"
	"github.com/ambra-media/ambra/internal/plugins/strm"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestStreamingV0104PathSanitizeProxyHeadersAndPrivateSubtitleCache(t *testing.T) {
	tmp := t.TempDir()
	st, err := sqlite.Open(filepath.Join(tmp, "stream_v0104.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	ctx := context.Background()
	repo := sqlite.NewLibraryRepo(st)
	userRepo := sqlite.NewUserRepo(st)
	reg := plugins.NewRegistry()
	reg.Register(strm.New(false))
	cfg := config.Default(tmp)

	lib, _ := repo.CreateLibrary(ctx, "Movies", "movies", []string{tmp})
	videoPath := filepath.Join(tmp, "movie.mp4")
	subPath := filepath.Join(tmp, "movie.srt")
	_ = os.WriteFile(videoPath, []byte("MP4BYTES"), 0o644)
	_ = os.WriteFile(subPath, []byte("1\n00:00:01,000 --> 00:00:02,000\nHello\n"), 0o644)

	itemID := guid.New()
	msID := guid.New()
	_ = repo.UpsertItem(ctx, &sqlite.ItemRecord{
		ID: itemID, LibraryID: lib.ID, ParentID: lib.ID,
		Type: "Movie", MediaType: "Video", Name: "SecretPathMovie", Path: videoPath, Container: "mp4",
	})
	_ = repo.ReplaceMediaSourcesAndStreams(ctx, itemID, []sqlite.MediaSourceRecord{{
		ID: msID, ItemID: itemID, Name: "movie.mp4", Path: videoPath, Container: "mp4",
		Streams: []sqlite.MediaStreamRecord{
			{MediaSourceID: msID, ItemID: itemID, StreamIndex: 0, StreamType: "Video", Codec: "h264"},
			{MediaSourceID: msID, ItemID: itemID, StreamIndex: 1, StreamType: "Subtitle", Codec: "subrip", IsExternal: true, Path: subPath},
		},
	}})

	viewer := &sqlite.UserRecord{ID: guid.New(), Name: "viewer", Policy: sqlite.DefaultUserPolicy(false), Config: sqlite.DefaultUserConfig()}
	admin := &sqlite.UserRecord{ID: guid.New(), Name: "admin", IsAdmin: true, Policy: sqlite.DefaultUserPolicy(true), Config: sqlite.DefaultUserConfig()}
	_ = userRepo.CreateUser(ctx, viewer)
	_ = userRepo.CreateUser(ctx, admin)

	h := New(repo, playback.New(repo, reg, cfg), imaging.New(repo))
	h.SetUserRepo(userRepo)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux, AuthFuncs{})

	// 1. PlaybackInfo strips MediaSources[].Path and MediaStreams[].Path for non-admin, preserves for admin
	reqViewer := httptest.NewRequest(http.MethodGet, "/Items/"+itemID+"/PlaybackInfo", nil)
	reqViewer = reqViewer.WithContext(middleware.WithAuthContext(reqViewer.Context(), &middleware.AuthContext{User: viewer}))
	recViewer := httptest.NewRecorder()
	mux.ServeHTTP(recViewer, reqViewer)
	if recViewer.Code != http.StatusOK {
		t.Fatalf("viewer PlaybackInfo status=%d", recViewer.Code)
	}
	var piViewer dto.PlaybackInfoResponse
	_ = json.Unmarshal(recViewer.Body.Bytes(), &piViewer)
	if len(piViewer.MediaSources) == 0 || piViewer.MediaSources[0].Path != "" {
		t.Fatalf("expected MediaSource.Path stripped for non-admin, got %+v", piViewer.MediaSources)
	}
	for _, ms := range piViewer.MediaSources[0].MediaStreams {
		if ms.Path != "" {
			t.Fatalf("expected MediaStream.Path stripped for non-admin, got %q", ms.Path)
		}
	}

	reqAdmin := httptest.NewRequest(http.MethodGet, "/Items/"+itemID+"/PlaybackInfo", nil)
	reqAdmin = reqAdmin.WithContext(middleware.WithAuthContext(reqAdmin.Context(), &middleware.AuthContext{User: admin}))
	recAdmin := httptest.NewRecorder()
	mux.ServeHTTP(recAdmin, reqAdmin)
	var piAdmin dto.PlaybackInfoResponse
	_ = json.Unmarshal(recAdmin.Body.Bytes(), &piAdmin)
	if len(piAdmin.MediaSources) == 0 || piAdmin.MediaSources[0].Path != videoPath {
		t.Fatalf("expected admin PlaybackInfo to retain Path, got %+v", piAdmin.MediaSources)
	}

	// 2. Subtitle response sets Cache-Control: private, max-age=3600
	reqSub := httptest.NewRequest(http.MethodGet, "/Videos/"+itemID+"/"+msID+"/Subtitles/1/Stream.vtt", nil)
	reqSub = reqSub.WithContext(middleware.WithAuthContext(reqSub.Context(), &middleware.AuthContext{User: viewer}))
	recSub := httptest.NewRecorder()
	mux.ServeHTTP(recSub, reqSub)
	if recSub.Code != http.StatusOK {
		t.Fatalf("subtitle status=%d", recSub.Code)
	}
	if cc := recSub.Header().Get("Cache-Control"); cc != "private, max-age=3600" {
		t.Fatalf("expected private subtitle Cache-Control, got %q", cc)
	}

	// 3. Remote proxy sets nosniff + sandbox CSP and rewrites unsafe HTML/SVG Content-Type to application/octet-stream
	evilUp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/xss.html":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte("<script>alert(1)</script>"))
		case "/xss.svg":
			w.Header().Set("Content-Type", "image/svg+xml")
			_, _ = w.Write([]byte("<svg/>"))
		default:
			w.Header().Set("Content-Type", "video/mp4")
			_, _ = w.Write([]byte("OK_MP4"))
		}
	}))
	defer evilUp.Close()

	for _, tc := range []struct {
		path   string
		wantCT string
	}{
		{"/xss.html", "application/octet-stream"},
		{"/xss.svg", "application/octet-stream"},
		{"/good.mp4", "video/mp4"},
	} {
		strmFile := filepath.Join(tmp, guid.New()+".strm")
		_ = os.WriteFile(strmFile, []byte(evilUp.URL+tc.path+"\n"), 0o644)
		sid := guid.New()
		_ = repo.UpsertItem(ctx, &sqlite.ItemRecord{
			ID: sid, LibraryID: lib.ID, ParentID: lib.ID,
			Type: "Movie", IsStrm: true, Name: "STRM", Path: strmFile, Container: "strm",
		})
		_ = repo.ReplaceMediaSourcesAndStreams(ctx, sid, []sqlite.MediaSourceRecord{{
			ID: guid.New(), ItemID: sid, Path: strmFile, Protocol: "Http", Container: "strm", IsRemote: true,
		}})

		req := httptest.NewRequest(http.MethodGet, "/Videos/"+sid+"/stream?proxy=1", nil)
		req = req.WithContext(middleware.WithAuthContext(req.Context(), &middleware.AuthContext{User: viewer}))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("proxy %s status=%d", tc.path, rec.Code)
		}
		if rec.Header().Get("X-Content-Type-Options") != "nosniff" {
			t.Fatalf("proxy %s missing nosniff header", tc.path)
		}
		if rec.Header().Get("Content-Security-Policy") != "sandbox" {
			t.Fatalf("proxy %s missing sandbox CSP header", tc.path)
		}
		if ct := rec.Header().Get("Content-Type"); ct != tc.wantCT {
			t.Fatalf("proxy %s Content-Type = %q, want %q", tc.path, ct, tc.wantCT)
		}
	}
}
