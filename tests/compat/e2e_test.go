package compat_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ambra-media/ambra/internal/api/dto"
	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/server"
)

func TestAmbraEndToEndEmbyCompatibility(t *testing.T) {
	t.Setenv("AMBRA_ADMIN_USER", "admin")
	t.Setenv("AMBRA_ADMIN_PASS", "admin")
	tmpDir := t.TempDir()
	dataDir := filepath.Join(tmpDir, "data")
	mediaDir := filepath.Join(tmpDir, "movies")
	if err := os.MkdirAll(filepath.Join(mediaDir, "The Wandering Earth (2019)"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(mediaDir, "Cloud Movie (2024)"), 0o755); err != nil {
		t.Fatal(err)
	}

	// Create sample local movie + NFO + Simplified ASS & Traditional PGS subtitles
	movieDir := filepath.Join(mediaDir, "The Wandering Earth (2019)")
	videoContent := []byte("0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ")
	if err := os.WriteFile(filepath.Join(movieDir, "The Wandering Earth (2019).mkv"), videoContent, 0o644); err != nil {
		t.Fatal(err)
	}
	nfoXML := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<movie>
  <title>流浪地球</title>
  <originaltitle>The Wandering Earth</originaltitle>
  <year>2019</year>
  <plot>太阳即将毁灭，人类开启流浪地球计划。</plot>
  <rating>8.2</rating>
  <genre>科幻</genre>
  <actor><name>吴京</name><role>刘培强</role></actor>
</movie>`
	if err := os.WriteFile(filepath.Join(movieDir, "The Wandering Earth (2019).nfo"), []byte(nfoXML), 0o644); err != nil {
		t.Fatal(err)
	}
	assSub := "[Script Info]\nTitle: CHS\n[Events]\nDialogue: 0,0:00:01.00,0:00:04.00,Default,,0,0,0,,道路千万条\n"
	if err := os.WriteFile(filepath.Join(movieDir, "The Wandering Earth (2019).chs.ass"), []byte(assSub), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(movieDir, "The Wandering Earth (2019).cht.sup"), []byte("PG\x00\x00\x00\x00\x00\x00\x00\x00\x80\x00\x00"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Create sample STRM movie
	strmDir := filepath.Join(mediaDir, "Cloud Movie (2024)")
	if err := os.WriteFile(filepath.Join(strmDir, "Cloud Movie (2024).strm"), []byte("https://93.184.216.34/cloud-movie.mkv\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := config.Load(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	srv, err := server.New(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = srv.Close() }()

	ts := httptest.NewServer(srv.Handler)
	defer ts.Close()

	// Disable automatic redirect following for 302 test
	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	// 1. Embedded Modern Web Client serves at /web/
	resp, err := client.Get(ts.URL + "/web/")
	if err != nil {
		t.Fatal(err)
	}
	bodyBytes, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(bodyBytes), "<html") {
		t.Fatalf("expected embedded web index.html at /web/, got status=%d", resp.StatusCode)
	}

	// 2. Public System Info via /emby prefix & case-insensitive path
	resp, err = client.Get(ts.URL + "/emby/system/info/public")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 on /emby/system/info/public, got %d", resp.StatusCode)
	}
	var pubInfo dto.PublicSystemInfo
	_ = json.NewDecoder(resp.Body).Decode(&pubInfo)
	resp.Body.Close()
	if pubInfo.Version != config.ServerVersion || pubInfo.Id != cfg.ServerID {
		t.Fatalf("unexpected public info: %+v", pubInfo)
	}

	// 3. Authenticate default admin
	authReqBody := `{"Username":"admin","Pw":"admin"}`
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/emby/Users/AuthenticateByName", strings.NewReader(authReqBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Emby-Authorization", `MediaBrowser Client="AmbraWeb", Device="Chrome", DeviceId="dev-e2e-1", Version="0.1.0"`)
	resp, err = client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 on auth, got %d", resp.StatusCode)
	}
	var authRes dto.AuthenticationResult
	_ = json.NewDecoder(resp.Body).Decode(&authRes)
	resp.Body.Close()
	if authRes.AccessToken == "" || authRes.User.Id == "" {
		t.Fatalf("missing token or user id: %+v", authRes)
	}
	token := authRes.AccessToken
	userID := authRes.User.Id

	doAuth := func(method, path string, body io.Reader) *http.Response {
		r, _ := http.NewRequest(method, ts.URL+path, body)
		r.Header.Set("X-Emby-Token", token)
		if body != nil {
			r.Header.Set("Content-Type", "application/json")
		}
		res, err := client.Do(r)
		if err != nil {
			t.Fatalf("%s %s failed: %v", method, path, err)
		}
		return res
	}

	// 4. Verify registered built-in plugins via /emby/Plugins
	resp = doAuth(http.MethodGet, "/emby/Plugins", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 on /emby/Plugins, got %d", resp.StatusCode)
	}
	var pluginList []map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&pluginList)
	resp.Body.Close()
	if len(pluginList) < 3 {
		t.Fatalf("expected >= 3 built-in plugins (nfo/tmdb/strm), got %d", len(pluginList))
	}

	// 5. Add VirtualFolder and run synchronous scan
	addLibPayload, _ := json.Marshal(map[string]any{
		"Name":           "Movies",
		"CollectionType": "movies",
		"Paths":          []string{mediaDir},
	})
	resp = doAuth(http.MethodPost, "/emby/Library/VirtualFolders?name=Movies&collectionType=movies", bytes.NewReader(addLibPayload))
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 200/204 creating virtual folder, got %d", resp.StatusCode)
	}
	if _, err := srv.Scanner.ScanAll(context.Background()); err != nil {
		t.Fatalf("ScanAll failed: %v", err)
	}

	// 6. Query Views & Items
	resp = doAuth(http.MethodGet, "/emby/Users/"+userID+"/Views", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 on Views, got %d", resp.StatusCode)
	}
	var views dto.QueryResult[dto.BaseItemDto]
	_ = json.NewDecoder(resp.Body).Decode(&views)
	resp.Body.Close()
	if views.TotalRecordCount != 1 {
		t.Fatalf("expected 1 library view, got %d", views.TotalRecordCount)
	}

	// 7. Chinese out-of-order search via /emby/Search/Hints
	resp = doAuth(http.MethodGet, "/emby/Search/Hints?searchTerm=%E5%9C%B0%E7%90%83+%E6%B5%81%E6%B5%AA", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 on Search/Hints, got %d", resp.StatusCode)
	}
	var hints dto.SearchHintResult
	_ = json.NewDecoder(resp.Body).Decode(&hints)
	resp.Body.Close()
	if len(hints.SearchHints) == 0 || hints.SearchHints[0].Name != "流浪地球" {
		t.Fatalf("expected Chinese out-of-order search to hit 流浪地球, got %+v", hints.SearchHints)
	}
	localMovieID := hints.SearchHints[0].Id

	// 8. PlaybackInfo with Web client ASS/PGS external profile -> must DirectPlay without transcoding
	pbReq := `{
		"DeviceProfile": {
			"DirectPlayProfiles": [{"Type":"Video","Container":"mkv,mp4","VideoCodec":"h264,hevc","AudioCodec":"aac,ac3"}],
			"SubtitleProfiles": [{"Format":"ass","Method":"External"},{"Format":"pgssub","Method":"External"}]
		}
	}`
	resp = doAuth(http.MethodPost, "/emby/Items/"+localMovieID+"/PlaybackInfo", strings.NewReader(pbReq))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 on PlaybackInfo, got %d", resp.StatusCode)
	}
	var pbResp dto.PlaybackInfoResponse
	_ = json.NewDecoder(resp.Body).Decode(&pbResp)
	resp.Body.Close()
	if len(pbResp.MediaSources) == 0 || !pbResp.MediaSources[0].SupportsDirectPlay || pbResp.MediaSources[0].TranscodingUrl != "" {
		t.Fatalf("expected DirectPlay without transcoding for ASS/PGS, got %+v", pbResp)
	}

	// Verify ASS subtitle stream direct delivery
	var assDeliveryURL string
	for _, st := range pbResp.MediaSources[0].MediaStreams {
		if st.Type == "Subtitle" && strings.EqualFold(st.Codec, "ass") {
			assDeliveryURL = st.DeliveryUrl
			if st.SubVariant != "chs" {
				t.Fatalf("expected chs sub variant, got %+v", st)
			}
		}
	}
	if assDeliveryURL == "" {
		t.Fatalf("expected ASS subtitle delivery URL in MediaStreams: %+v", pbResp.MediaSources[0].MediaStreams)
	}
	resp = doAuth(http.MethodGet, "/emby"+assDeliveryURL, nil)
	subBody, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(subBody), "道路千万条") {
		t.Fatalf("expected ASS subtitle content, got status=%d body=%s", resp.StatusCode, string(subBody))
	}

	// 9. Local video HTTP Range stream
	rangeReq, _ := http.NewRequest(http.MethodGet, ts.URL+"/emby/Videos/"+localMovieID+"/stream", nil)
	rangeReq.Header.Set("X-Emby-Token", token)
	rangeReq.Header.Set("Range", "bytes=0-9")
	resp, err = client.Do(rangeReq)
	if err != nil {
		t.Fatal(err)
	}
	chunk, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusPartialContent || string(chunk) != "0123456789" {
		t.Fatalf("expected 206 Partial Content '0123456789', got status=%d chunk=%q", resp.StatusCode, string(chunk))
	}

	// 10. STRM 302 redirect verification
	resp = doAuth(http.MethodGet, "/emby/Users/"+userID+"/Items?IncludeItemTypes=Movie", nil)
	var allMovies dto.QueryResult[dto.BaseItemDto]
	_ = json.NewDecoder(resp.Body).Decode(&allMovies)
	resp.Body.Close()
	var strmMovieID string
	for _, m := range allMovies.Items {
		if strings.Contains(m.Name, "Cloud Movie") {
			strmMovieID = m.Id
		}
	}
	if strmMovieID == "" {
		t.Fatalf("STRM movie not found in library items: %+v", allMovies.Items)
	}
	resp = doAuth(http.MethodGet, "/emby/Videos/"+strmMovieID+"/stream", nil)
	loc := resp.Header.Get("Location")
	resp.Body.Close()
	if resp.StatusCode != http.StatusFound || loc != "https://93.184.216.34/cloud-movie.mkv" {
		t.Fatalf("expected 302 redirect to CDN URL, got status=%d location=%q", resp.StatusCode, loc)
	}

	// 11. Brand SVG favicon at /favicon.ico and /web/favicon.svg
	for _, favPath := range []string{"/favicon.ico", "/web/favicon.svg"} {
		resp, err = client.Get(ts.URL + favPath)
		if err != nil {
			t.Fatal(err)
		}
		favBytes, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK || !strings.Contains(string(favBytes), "<svg") {
			t.Fatalf("expected SVG logo at %s, got status=%d", favPath, resp.StatusCode)
		}
	}

	// 12. Phase 2 endpoints: Environment, ScheduledTasks, Logs, Collections, PluginConfig
	resp = doAuth(http.MethodGet, "/emby/Environment/Drives", nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 on /emby/Environment/Drives, got %d", resp.StatusCode)
	}
	resp = doAuth(http.MethodGet, "/emby/ScheduledTasks", nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 on /emby/ScheduledTasks, got %d", resp.StatusCode)
	}
	resp = doAuth(http.MethodGet, "/emby/System/Logs/Query", nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 on /emby/System/Logs/Query, got %d", resp.StatusCode)
	}

	// 13. Phase 3 endpoints: Encoding CodecInfo & HLS master.m3u8
	resp = doAuth(http.MethodGet, "/emby/Encoding/CodecInformation/Video", nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 on /emby/Encoding/CodecInformation/Video, got %d", resp.StatusCode)
	}
	resp = doAuth(http.MethodGet, "/emby/Videos/"+localMovieID+"/master.m3u8", nil)
	m3u8Bytes, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(m3u8Bytes), "#EXTM3U") {
		t.Fatalf("expected valid #EXTM3U on master.m3u8, got status=%d body=%s", resp.StatusCode, string(m3u8Bytes))
	}

	// 14. Phase 4-6 endpoints: LiveTV, Sync, Notifications, Startup, Webhooks, BackupRestore, OpenAPI, Parties, DLNA
	for _, ep := range []string{
		"/emby/LiveTv/Info",
		"/emby/Sync/Targets",
		"/emby/Notifications/" + userID + "/Summary",
		"/emby/Startup/Configuration",
		"/emby/Webhooks",
		"/emby/BackupRestore/BackupInfo",
		"/emby/openapi.json",
		"/emby/Parties",
		"/emby/Dlna/ProfileInfos",
		"/Dlna/" + cfg.ServerID + "/description.xml",
	} {
		resp = doAuth(http.MethodGet, ep, nil)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200 on %s, got %d", ep, resp.StatusCode)
		}
	}

	// UPnP SUBSCRIBE verb on DLNA events
	subReq, _ := http.NewRequest("SUBSCRIBE", ts.URL+"/Dlna/"+cfg.ServerID+"/contentdirectory/events", nil)
	subReq.Header.Set("CALLBACK", "<http://127.0.0.1:9999/notify>")
	subReq.Header.Set("NT", "upnp:event")
	resp, err = client.Do(subReq)
	if err != nil {
		t.Fatal(err)
	}
	sid := resp.Header.Get("SID")
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || sid == "" {
		t.Fatalf("expected 200 + SID on DLNA SUBSCRIBE, got status=%d sid=%q", resp.StatusCode, sid)
	}
}
