package hls

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/ambra-media/ambra/internal/api/middleware"
	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/core/auth"
	"github.com/ambra-media/ambra/internal/core/transcode"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestV0109Audit_ActiveEncodingsPlaylistCacheControlAndPrivatePlaylist(t *testing.T) {
	tmp := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmp, "hls_v0109.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	ctx := context.Background()
	libRepo := sqlite.NewLibraryRepo(store)
	userRepo := sqlite.NewUserRepo(store)
	cfg := config.Default(tmp)
	authSvc := auth.NewService(userRepo, cfg)
	mgr := transcode.NewManager(cfg, transcode.WithCapabilities(transcode.SoftwareOnlyCapabilities()))
	defer mgr.Close()

	admin, _ := authSvc.CreateUser(ctx, "admin", "adminpass", true)
	alice, _ := authSvc.CreateUser(ctx, "alice", "alicepass", false)
	bob, _ := authSvc.CreateUser(ctx, "bob", "bobpass", false)

	h := New(libRepo, mgr, cfg)
	h.SetUserRepo(userRepo)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux, AuthFuncs{})

	adminAC := &middleware.AuthContext{
		User:   admin,
		Token:  "admin-tok",
		Client: auth.ClientInfo{DeviceID: "dev-admin"},
	}
	aliceAC := &middleware.AuthContext{
		User:   alice,
		Token:  "alice-tok",
		Client: auth.ClientInfo{DeviceID: "dev-alice"},
	}
	bobAC := &middleware.AuthContext{
		User:   bob,
		Token:  "bob-tok",
		Client: auth.ClientInfo{DeviceID: "dev-bob"},
	}

	doAs := func(ac *middleware.AuthContext, method, target, body string) *httptest.ResponseRecorder {
		var rBody *bytes.Buffer
		if body != "" {
			rBody = bytes.NewBufferString(body)
		} else {
			rBody = &bytes.Buffer{}
		}
		req := httptest.NewRequest(method, target, rBody)
		req = req.WithContext(middleware.WithAuthContext(req.Context(), ac))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}

	// --- [M-10] ActiveEncodings device/session scoping ---
	// Register two sessions for Alice on the same device, one for Admin on dev-admin, one for Bob on dev-bob
	_, _ = mgr.RegisterSession(transcode.SessionParams{
		UserID: alice.ID, PlaySessionID: alice.ID + ":ps-tab1", DeviceID: "dev-alice", ItemID: "m1", InputPath: "/m/1.mkv",
	})
	_, _ = mgr.RegisterSession(transcode.SessionParams{
		UserID: alice.ID, PlaySessionID: alice.ID + ":ps-tab2", DeviceID: "dev-alice", ItemID: "m2", InputPath: "/m/2.mkv",
	})
	_, _ = mgr.RegisterSession(transcode.SessionParams{
		UserID: admin.ID, PlaySessionID: "ps-admin", DeviceID: "dev-admin", ItemID: "m3", InputPath: "/m/3.mkv",
	})
	_, _ = mgr.RegisterSession(transcode.SessionParams{
		UserID: bob.ID, PlaySessionID: bob.ID + ":ps-bob", DeviceID: "dev-bob", ItemID: "m4", InputPath: "/m/4.mkv",
	})

	// Alice stops PlaySessionId=ps-tab1 without DeviceId -> MUST NOT kill ps-tab2 on same dev-alice
	rec := doAs(aliceAC, http.MethodDelete, "/Videos/ActiveEncodings?PlaySessionId=ps-tab1", "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", rec.Code)
	}
	hasSession := func(psid string) bool {
		for _, s := range mgr.ActiveSessions() {
			if s.PlaySessionID == psid {
				return true
			}
		}
		return false
	}
	if hasSession(alice.ID + ":ps-tab1") {
		t.Fatalf("expected alice ps-tab1 to be stopped")
	}
	if !hasSession(alice.ID + ":ps-tab2") {
		t.Fatalf("expected alice ps-tab2 on same device to survive PlaySessionId-only delete")
	}

	// Alice spoofing DeviceId=dev-bob must NOT stop Bob's session
	_ = doAs(aliceAC, http.MethodDelete, "/Videos/ActiveEncodings?DeviceId=dev-bob", "")
	if !hasSession(bob.ID + ":ps-bob") {
		t.Fatalf("non-admin must not stop another user's device session")
	}

	// Admin with explicit DeviceId=dev-bob stops Bob's session, NOT admin's own dev-admin session
	_ = doAs(adminAC, http.MethodDelete, "/Videos/ActiveEncodings?DeviceId=dev-bob", "")
	if hasSession(bob.ID + ":ps-bob") {
		t.Fatalf("admin should be able to stop target DeviceId=dev-bob")
	}
	if !hasSession("ps-admin") {
		t.Fatalf("admin targeting DeviceId=dev-bob must not kill admin's own dev-admin session")
	}

	// Admin with explicit PlaySessionId=ps-bob2 (no DeviceId) stops target without killing dev-admin
	_, _ = mgr.RegisterSession(transcode.SessionParams{
		UserID: bob.ID, PlaySessionID: bob.ID + ":ps-bob2", DeviceID: "dev-bob", ItemID: "m5", InputPath: "/m/5.mkv",
	})
	_ = doAs(adminAC, http.MethodDelete, "/Videos/ActiveEncodings?PlaySessionId=ps-bob2", "")
	if hasSession(bob.ID + ":ps-bob2") {
		t.Fatalf("admin should stop ps-bob2")
	}
	if !hasSession("ps-admin") {
		t.Fatalf("admin stopping PlaySessionId=ps-bob2 must not kill admin's own dev-admin session")
	}

	// --- [M-14] All m3u8 playlists set Cache-Control: private, no-store ---
	videoPath := filepath.Join(tmp, "movie.mkv")
	_ = os.WriteFile(videoPath, []byte("fake"), 0o644)
	itemID := guid.New()
	msID := guid.New()
	rt := int64(600_000_000)
	_ = libRepo.UpsertItem(ctx, &sqlite.ItemRecord{
		ID: itemID, LibraryID: "lib1", Type: "Movie", MediaType: "Video", Name: "M", Path: videoPath, RuntimeTicks: &rt,
	})
	_ = libRepo.ReplaceMediaSourcesAndStreams(ctx, itemID, []sqlite.MediaSourceRecord{
		{
			ID: msID, ItemID: itemID, Path: videoPath, RuntimeTicks: rt,
			Streams: []sqlite.MediaStreamRecord{
				{MediaSourceID: msID, ItemID: itemID, StreamIndex: 0, StreamType: "Video", Codec: "h264", Width: 1920, Height: 1080},
				{MediaSourceID: msID, ItemID: itemID, StreamIndex: 2, StreamType: "Subtitle", Codec: "srt", Language: "eng"},
			},
		},
	})
	for _, ep := range []string{
		"/Videos/" + itemID + "/master.m3u8",
		"/Videos/" + itemID + "/main.m3u8",
		"/Videos/" + itemID + "/live.m3u8",
		"/Videos/" + itemID + "/" + msID + "/Subtitles/2/subtitles.m3u8",
	} {
		r := doAs(aliceAC, http.MethodGet, ep, "")
		if r.Code != http.StatusOK {
			t.Fatalf("GET %s expected 200, got %d body=%q", ep, r.Code, r.Body.String())
		}
		if cc := r.Header().Get("Cache-Control"); cc != "private, no-store" {
			t.Fatalf("GET %s expected Cache-Control=private, no-store, got %q", ep, cc)
		}
	}

	// --- [M-15] Private Playlist access control in hls.canAccessItem / LiveStreams/Open ---
	plRec, err := libRepo.CreatePlaylist(ctx, "Alice Private", "Video", alice.ID, []string{itemID})
	if err != nil {
		t.Fatal(err)
	}
	// Bob cannot open LiveStream or master.m3u8 for Alice's private playlist -> 403
	if r := doAs(bobAC, http.MethodPost, "/LiveStreams/Open", `{"ItemId":"`+plRec.ID+`"}`); r.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for bob LiveStreams/Open on alice private playlist, got %d", r.Code)
	}
	if r := doAs(bobAC, http.MethodGet, "/Videos/"+plRec.ID+"/master.m3u8", ""); r.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for bob master.m3u8 on alice private playlist, got %d", r.Code)
	}
	// Owner Alice and Admin can access
	if r := doAs(aliceAC, http.MethodPost, "/LiveStreams/Open", `{"ItemId":"`+plRec.ID+`"}`); r.Code != http.StatusOK {
		t.Fatalf("expected 200 for owner alice LiveStreams/Open on own playlist, got %d", r.Code)
	}
	if r := doAs(adminAC, http.MethodPost, "/LiveStreams/Open", `{"ItemId":"`+plRec.ID+`"}`); r.Code != http.StatusOK {
		t.Fatalf("expected 200 for admin LiveStreams/Open on private playlist, got %d", r.Code)
	}
	// Marking playlist public allows Bob
	_ = userRepo.SetServerConfig(ctx, "item_visibility_"+guid.Normalize(plRec.ID), `{"IsPublic":true}`)
	if r := doAs(bobAC, http.MethodPost, "/LiveStreams/Open", `{"ItemId":"`+plRec.ID+`"}`); r.Code != http.StatusOK {
		t.Fatalf("expected 200 for bob LiveStreams/Open on public playlist, got %d", r.Code)
	}
}
