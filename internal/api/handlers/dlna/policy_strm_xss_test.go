package dlna

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/core/transcode"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/plugins"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestAuditWave1_H03_H04_H05_H08_DLNAUserPolicyAndStrmProxyXSS(t *testing.T) {
	tmp := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmp, "dlna_wave1.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	ctx := context.Background()
	libRepo := sqlite.NewLibraryRepo(store)
	userRepo := sqlite.NewUserRepo(store)
	cfg := config.Default(tmp)
	cfg.EnableDLNA = true

	moviesLib, _ := libRepo.CreateLibrary(ctx, "Movies", "movies", []string{tmp})
	dvrLib, _ := libRepo.CreateLibrary(ctx, "DVR", "livetv", []string{tmp})

	localMP4 := filepath.Join(tmp, "vid.mp4")
	_ = os.WriteFile(localMP4, []byte("DLNA-MP4"), 0o644)

	upstreamXSS := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/xss.html":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte("<script>alert(1)</script>"))
		case "/xss.svg":
			w.Header().Set("Content-Type", "image/svg+xml")
			_, _ = w.Write([]byte("<svg onload=alert(1)/>"))
		default:
			w.Header().Set("Content-Type", "video/mp4")
			_, _ = w.Write([]byte("REMOTE-MP4"))
		}
	}))
	defer upstreamXSS.Close()

	strmHTMLPath := filepath.Join(tmp, "evil_html.strm")
	_ = os.WriteFile(strmHTMLPath, []byte(upstreamXSS.URL+"/xss.html\n"), 0o644)
	strmSVGPath := filepath.Join(tmp, "evil_svg.strm")
	_ = os.WriteFile(strmSVGPath, []byte(upstreamXSS.URL+"/xss.svg\n"), 0o644)
	strmOKPath := filepath.Join(tmp, "ok.strm")
	_ = os.WriteFile(strmOKPath, []byte(upstreamXSS.URL+"/video.mp4\n"), 0o644)

	safeMovie := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: moviesLib.ID, ParentID: moviesLib.ID,
		Type: "Movie", MediaType: "Video", Name: "Safe", OfficialRating: "G", Container: "mp4", Path: localMP4,
	}
	dvrItem := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: dvrLib.ID, ParentID: dvrLib.ID,
		Type: "Video", MediaType: "Video", Name: "Rec", OfficialRating: "G", Container: "mp4", Path: localMP4,
	}
	unratedMovie := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: moviesLib.ID, ParentID: moviesLib.ID,
		Type: "Movie", MediaType: "Video", Name: "Unrated", OfficialRating: "", Container: "mp4", Path: localMP4,
	}
	strmHTMLItem := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: moviesLib.ID, ParentID: moviesLib.ID,
		Type: "Movie", MediaType: "Video", Name: "HTML Strm", OfficialRating: "G", IsStrm: true, Container: "strm", Path: strmHTMLPath,
	}
	strmSVGItem := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: moviesLib.ID, ParentID: moviesLib.ID,
		Type: "Movie", MediaType: "Video", Name: "SVG Strm", OfficialRating: "G", IsStrm: true, Container: "strm", Path: strmSVGPath,
	}
	strmOKItem := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: moviesLib.ID, ParentID: moviesLib.ID,
		Type: "Movie", MediaType: "Video", Name: "OK Strm", OfficialRating: "G", IsStrm: true, Container: "strm", Path: strmOKPath,
	}
	for _, it := range []*sqlite.ItemRecord{safeMovie, dvrItem, unratedMovie, strmHTMLItem, strmSVGItem, strmOKItem} {
		if err := libRepo.UpsertItem(ctx, it); err != nil {
			t.Fatal(err)
		}
	}

	noPlayPol := sqlite.DefaultUserPolicy(false)
	noPlayPol.EnableMediaPlayback = false
	noPlayUser := &sqlite.UserRecord{ID: guid.New(), Name: "noplay", Policy: noPlayPol, Config: sqlite.DefaultUserConfig()}

	restrictedPol := sqlite.DefaultUserPolicy(false)
	restrictedPol.EnableAllFolders = true
	restrictedPol.EnableLiveTvAccess = false
	restrictedPol.BlockUnratedItems = []string{"Movie"}
	restrictedUser := &sqlite.UserRecord{ID: guid.New(), Name: "restricted", Policy: restrictedPol, Config: sqlite.DefaultUserConfig()}

	for _, u := range []*sqlite.UserRecord{noPlayUser, restrictedUser} {
		_ = userRepo.CreateUser(ctx, u)
	}

	h := New(cfg, libRepo, store)
	h.SetUserRepo(userRepo)
	h.SetProxyClient(upstreamXSS.Client())
	h.SetStreamResolverWithHeaders(func(_ context.Context, rawPath string) (*plugins.StreamResolveResult, error) {
		b, err := os.ReadFile(rawPath)
		if err != nil {
			return nil, err
		}
		return &plugins.StreamResolveResult{URL: string(b[:len(b)-1])}, nil
	})

	mux := http.NewServeMux()
	h.RegisterRoutes(mux, AuthFuncs{})

	streamReq := func(itemID string) *httptest.ResponseRecorder {
		exp := time.Now().UTC().Add(10 * time.Minute).Unix()
		sig := h.signStreamToken(itemID, exp)
		u := fmt.Sprintf("/Dlna/srv-uuid/stream/%s?exp=%d&sig=%s", itemID, exp, sig)
		req := httptest.NewRequest(http.MethodGet, u, nil)
		req.RemoteAddr = "192.168.1.25:54321"
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}

	// 1. H-03: DLNA user with EnableMediaPlayback=false gets 403 Forbidden!
	h.SetDefaultUserID(noPlayUser.ID)
	if code := streamReq(safeMovie.ID).Code; code != http.StatusForbidden {
		t.Fatalf("H-03: expected 403 on DLNA stream when EnableMediaPlayback=false, got %d", code)
	}

	// 2. H-04 & H-05: DLNA user with EnableLiveTvAccess=false & BlockUnratedItems=[Movie] is denied (404) on livetv & unrated items!
	h.SetDefaultUserID(restrictedUser.ID)
	if code := streamReq(dvrItem.ID).Code; code != http.StatusNotFound {
		t.Fatalf("H-04: expected 404 hiding livetv library item on DLNA stream, got %d", code)
	}
	if code := streamReq(unratedMovie.ID).Code; code != http.StatusNotFound {
		t.Fatalf("H-05: expected 404 hiding unrated movie on DLNA stream, got %d", code)
	}

	// 3. H-08: .strm proxy sanitizes text/html & image/svg+xml to application/octet-stream and sets nosniff + CSP sandbox!
	for _, id := range []string{strmHTMLItem.ID, strmSVGItem.ID} {
		rec := streamReq(id)
		if rec.Code != http.StatusOK {
			t.Fatalf("H-08: expected 200 proxying .strm, got %d (%s)", rec.Code, rec.Body.String())
		}
		if ct := rec.Header().Get("Content-Type"); ct != "application/octet-stream" {
			t.Fatalf("H-08: expected Content-Type application/octet-stream for XSS payload, got %q", ct)
		}
		if rec.Header().Get("X-Content-Type-Options") != "nosniff" {
			t.Fatalf("H-08: missing X-Content-Type-Options: nosniff, got %q", rec.Header().Get("X-Content-Type-Options"))
		}
		if rec.Header().Get("Content-Security-Policy") != "sandbox" {
			t.Fatalf("H-08: missing Content-Security-Policy: sandbox, got %q", rec.Header().Get("Content-Security-Policy"))
		}
	}
	okRec := streamReq(strmOKItem.ID)
	if okRec.Code != http.StatusOK || okRec.Header().Get("Content-Type") != "video/mp4" || okRec.Header().Get("Content-Security-Policy") != "sandbox" {
		t.Fatalf("H-08: expected video/mp4 + CSP sandbox on valid video .strm, got code=%d ct=%q csp=%q",
			okRec.Code, okRec.Header().Get("Content-Type"), okRec.Header().Get("Content-Security-Policy"))
	}
}

func TestDLNATranscodePolicyModeTokenAndDirectStreamLimit(t *testing.T) {
	tmp := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmp, "dlna_tc_limits.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	ctx := context.Background()
	libRepo := sqlite.NewLibraryRepo(store)
	userRepo := sqlite.NewUserRepo(store)
	cfg := config.Default(tmp)
	cfg.EnableDLNA = true

	mgr := transcode.NewManager(cfg, transcode.WithCapabilities(transcode.SoftwareOnlyCapabilities()))
	defer mgr.Close()

	mediaLib, _ := libRepo.CreateLibrary(ctx, "Media", "movies", []string{tmp})

	vidFile := filepath.Join(tmp, "clip.mp4")
	_ = os.WriteFile(vidFile, []byte("LOCAL-VIDEO-BYTES"), 0o644)
	audFile := filepath.Join(tmp, "song.mp3")
	_ = os.WriteFile(audFile, []byte("LOCAL-AUDIO-BYTES"), 0o644)
	photoFile := filepath.Join(tmp, "pic.jpg")
	_ = os.WriteFile(photoFile, []byte("LOCAL-PHOTO-BYTES"), 0o644)

	upstreamStrm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "video/mp4")
		_, _ = w.Write([]byte("REMOTE-STRM-BYTES"))
	}))
	defer upstreamStrm.Close()
	strmFile := filepath.Join(tmp, "remote.strm")
	_ = os.WriteFile(strmFile, []byte(upstreamStrm.URL+"/video.mp4\n"), 0o644)

	vidItem := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: mediaLib.ID, ParentID: mediaLib.ID,
		Type: "Movie", MediaType: "Video", Name: "Clip", OfficialRating: "G", Container: "mp4", Path: vidFile,
	}
	audItem := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: mediaLib.ID, ParentID: mediaLib.ID,
		Type: "Audio", MediaType: "Audio", Name: "Song", OfficialRating: "G", Container: "mp3", Path: audFile,
	}
	photoItem := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: mediaLib.ID, ParentID: mediaLib.ID,
		Type: "Photo", MediaType: "Photo", Name: "Pic", OfficialRating: "G", Container: "jpg", Path: photoFile,
	}
	strmItem := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: mediaLib.ID, ParentID: mediaLib.ID,
		Type: "Movie", MediaType: "Video", Name: "Remote", OfficialRating: "G", IsStrm: true, Container: "strm", Path: strmFile,
	}
	for _, it := range []*sqlite.ItemRecord{vidItem, audItem, photoItem, strmItem} {
		if err := libRepo.UpsertItem(ctx, it); err != nil {
			t.Fatal(err)
		}
	}

	noVidTC := sqlite.DefaultUserPolicy(false)
	noVidTC.EnableVideoPlaybackTranscoding = false
	noVidUser := &sqlite.UserRecord{ID: guid.New(), Name: "no_vid_tc", Policy: noVidTC, Config: sqlite.DefaultUserConfig()}

	noAudTC := sqlite.DefaultUserPolicy(false)
	noAudTC.EnableAudioPlaybackTranscoding = false
	noAudUser := &sqlite.UserRecord{ID: guid.New(), Name: "no_aud_tc", Policy: noAudTC, Config: sqlite.DefaultUserConfig()}

	_ = userRepo.CreateUser(ctx, noVidUser)
	_ = userRepo.CreateUser(ctx, noAudUser)

	h := New(cfg, libRepo, store)
	h.SetUserRepo(userRepo)
	h.SetTranscodeManager(mgr)
	h.SetProxyClient(upstreamStrm.Client())
	h.SetStreamResolverWithHeaders(func(_ context.Context, _ string) (*plugins.StreamResolveResult, error) {
		return &plugins.StreamResolveResult{URL: upstreamStrm.URL + "/video.mp4"}, nil
	})
	mux := http.NewServeMux()
	h.RegisterRoutes(mux, AuthFuncs{})

	exp := time.Now().UTC().Add(15 * time.Minute).Unix()
	doDLNA := func(method, itemID, sig, extraQuery string) *httptest.ResponseRecorder {
		u := fmt.Sprintf("/Dlna/srv-uuid/stream/%s?exp=%d&sig=%s%s", itemID, exp, sig, extraQuery)
		req := httptest.NewRequest(method, u, nil)
		req.RemoteAddr = "192.168.1.30:54321"
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}

	// 1. EnableVideoPlaybackTranscoding=false blocks video transcode=1 (403) while allowing direct video & audio transcode HEAD.
	h.SetDefaultUserID(noVidUser.ID)
	vidSig := h.signStreamToken(vidItem.ID, exp)
	vidTCSig := h.signStreamTokenForMode(vidItem.ID, exp, true)
	if rec := doDLNA(http.MethodGet, vidItem.ID, vidSig, ""); rec.Code != http.StatusOK {
		t.Fatalf("expected 200 direct video when video transcoding disabled, got %d", rec.Code)
	}
	if rec := doDLNA(http.MethodGet, vidItem.ID, vidTCSig, "&transcode=1"); rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 on video transcode=1 when EnableVideoPlaybackTranscoding=false, got %d", rec.Code)
	}
	audDirectSig := h.signStreamToken(audItem.ID, exp)
	audTCSig := h.signStreamTokenForMode(audItem.ID, exp, true)
	if rec := doDLNA(http.MethodHead, audItem.ID, audTCSig, "&transcode=1"); rec.Code != http.StatusOK {
		t.Fatalf("expected 200 HEAD on audio transcode=1 when EnableAudioPlaybackTranscoding=true, got %d", rec.Code)
	}

	// 2. EnableAudioPlaybackTranscoding=false blocks audio transcode=1 (403) while allowing direct audio.
	h.SetDefaultUserID(noAudUser.ID)
	if rec := doDLNA(http.MethodGet, audItem.ID, audDirectSig, ""); rec.Code != http.StatusOK {
		t.Fatalf("expected 200 direct audio when audio transcoding disabled, got %d", rec.Code)
	}
	if rec := doDLNA(http.MethodGet, audItem.ID, audTCSig, "&transcode=1"); rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 on audio transcode=1 when EnableAudioPlaybackTranscoding=false, got %d", rec.Code)
	}

	// 3. Photo item rejects transcode=1 with 403 even for unrestricted admin/default policy.
	h.SetDefaultUserID("")
	h.SetUserRepo(nil)
	photoSig := h.signStreamTokenForMode(photoItem.ID, exp, true)
	if rec := doDLNA(http.MethodGet, photoItem.ID, photoSig, "&transcode=1"); rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 on photo transcode=1, got %d", rec.Code)
	}

	// 4. Mode-scoped stream token enforces exact transcode flag when strict verification is used.
	directOnlySig := h.signStreamTokenForMode(vidItem.ID, exp, false)
	if !h.verifyStreamTokenForMode(vidItem.ID, fmt.Sprint(exp), directOnlySig, false, false) {
		t.Fatal("expected direct-scoped token to pass direct verification")
	}
	if h.verifyStreamTokenForMode(vidItem.ID, fmt.Sprint(exp), directOnlySig, true, false) {
		t.Fatal("expected direct-scoped token to reject transcode=true verification")
	}
	if rec := doDLNA(http.MethodGet, vidItem.ID, directOnlySig, "&transcode=1"); rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 when direct-only scoped token is used with transcode=1, got %d", rec.Code)
	}

	// 5. SimultaneousStreamLimit enforced on non-HEAD direct local file and .strm streams, while HEAD still passes.
	cfg.SimultaneousStreamLimit = 1
	relOccupied, ok := mgr.TryAcquireStreamSlot(1, "occupied-slot-1")
	if !ok {
		t.Fatal("failed to pre-acquire stream slot")
	}
	strmSig := h.signStreamToken(strmItem.ID, exp)
	if rec := doDLNA(http.MethodGet, vidItem.ID, vidSig, ""); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 on DLNA direct file GET when SimultaneousStreamLimit=1 full, got %d", rec.Code)
	}
	if rec := doDLNA(http.MethodGet, strmItem.ID, strmSig, ""); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 on DLNA .strm proxy GET when SimultaneousStreamLimit=1 full, got %d", rec.Code)
	}
	if rec := doDLNA(http.MethodHead, vidItem.ID, vidSig, ""); rec.Code != http.StatusOK {
		t.Fatalf("expected 200 on DLNA direct file HEAD even when stream slots full, got %d", rec.Code)
	}
	if rec := doDLNA(http.MethodHead, strmItem.ID, strmSig, ""); rec.Code != http.StatusOK {
		t.Fatalf("expected 200 on DLNA .strm proxy HEAD even when stream slots full, got %d", rec.Code)
	}
	relOccupied()

	if rec := doDLNA(http.MethodGet, vidItem.ID, vidSig, ""); rec.Code != http.StatusOK {
		t.Fatalf("expected 200 on DLNA direct file GET after slot released, got %d", rec.Code)
	}
	if rec := doDLNA(http.MethodGet, strmItem.ID, strmSig, ""); rec.Code != http.StatusOK {
		t.Fatalf("expected 200 on DLNA .strm proxy GET after slot released, got %d", rec.Code)
	}
}

