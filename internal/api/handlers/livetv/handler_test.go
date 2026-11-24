package livetv

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ambra-media/ambra/internal/api/dto"
	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/core/dvr"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func setupLiveTVTest(t *testing.T) (*Handler, *http.ServeMux, *sqlite.Store, *config.Config) {
	t.Helper()
	dir := t.TempDir()
	cfg := config.Default(dir)
	st, err := sqlite.Open(cfg.DBPath())
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	h := New(st, cfg)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux, AuthFuncs{})
	return h, mux, st, cfg
}

func doJSON(t *testing.T, mux *http.ServeMux, method, target string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		switch v := body.(type) {
		case string:
			buf.WriteString(v)
		default:
			if err := json.NewEncoder(&buf).Encode(v); err != nil {
				t.Fatalf("encode body: %v", err)
			}
		}
	}
	req := httptest.NewRequest(method, target, &buf)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func TestParseM3U(t *testing.T) {
	playlist := "\uFEFF#EXTM3U\n" +
		"#EXTINF:-1 tvg-id=\"cctv1\" tvg-name=\"CCTV-1\" tvg-logo=\"https://logo/1.png\" group-title=\"News\" tvg-chno=\"101\",CCTV 1 HD\n" +
		"http://stream.local/cctv1.m3u8\n" +
		"#EXTVLCOPT:http-user-agent=Ambra\n" +
		"#EXTINF:-1 tvg-name=\"CCTV-2\" channel-number=202,\n" +
		"http://stream.local/cctv2.m3u8\n" +
		"#EXTINF:-1 tvg-id=\"no-name\"\n" +
		"http://stream.local/raw.m3u8\n" +
		"http://stream.local/bare.m3u8\n"

	channels, err := ParseM3UString(playlist)
	if err != nil {
		t.Fatalf("ParseM3UString: %v", err)
	}
	if len(channels) != 4 {
		t.Fatalf("expected 4 channels, got %d", len(channels))
	}
	if channels[0].TvgID != "cctv1" || channels[0].Name != "CCTV 1 HD" || channels[0].GroupTitle != "News" || channels[0].Number != "101" || channels[0].TvgLogo != "https://logo/1.png" {
		t.Fatalf("unexpected channel[0]: %+v", channels[0])
	}
	if channels[1].Name != "CCTV-2" || channels[1].Number != "202" {
		t.Fatalf("unexpected channel[1]: %+v", channels[1])
	}
	if channels[2].Name != "http://stream.local/raw.m3u8" || channels[2].Number != "3" {
		t.Fatalf("unexpected channel[2]: %+v", channels[2])
	}
	if channels[3].URL != "http://stream.local/bare.m3u8" || channels[3].Number != "4" {
		t.Fatalf("unexpected channel[3]: %+v", channels[3])
	}
}

func TestLiveTVTunerHostsChannelsPrograms(t *testing.T) {
	h, mux, st, cfg := setupLiveTVTest(t)

	// Info before tuners
	rec := doJSON(t, mux, http.MethodGet, "/LiveTv/Info", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /LiveTv/Info status=%d", rec.Code)
	}

	// Tuner types
	rec = doJSON(t, mux, http.MethodGet, "/LiveTv/TunerHosts/Types", nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "m3u") {
		t.Fatalf("unexpected tuner types: %d %s", rec.Code, rec.Body.String())
	}

	// Bad JSON
	if r := doJSON(t, mux, http.MethodPost, "/LiveTv/TunerHosts", "{bad"); r.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", r.Code)
	}

	// Inline M3U tuner host
	m3u := "#EXTM3U\n#EXTINF:-1 tvg-id=\"ch1\" group-title=\"Sports\",ESPN HD\nhttp://tv/espn.m3u8\n#EXTINF:-1 tvg-id=\"ch2\" group-title=\"Movies\",HBO\nhttp://tv/hbo.m3u8\n"
	rec = doJSON(t, mux, http.MethodPost, "/LiveTv/TunerHosts", map[string]any{
		"FriendlyName": "My IPTV",
		"M3UContent":   m3u,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /LiveTv/TunerHosts status=%d body=%s", rec.Code, rec.Body.String())
	}
	var host TunerHostInfo
	_ = json.Unmarshal(rec.Body.Bytes(), &host)
	if host.ImportedChannelCount != 2 || host.Id == "" {
		t.Fatalf("unexpected host: %+v", host)
	}

	// Update existing tuner host via HTTP M3U server
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/fail" {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_, _ = w.Write([]byte("#EXTM3U\n#EXTINF:-1 tvg-id=\"ch3\" group-title=\"News\",CNN\nhttp://tv/cnn.m3u8\n"))
	}))
	defer ts.Close()
	h.SetHTTPClient(ts.Client())

	// Failing HTTP M3U
	if r := doJSON(t, mux, http.MethodPost, "/LiveTv/TunerHosts", map[string]any{
		"Type": "m3u",
		"Url":  ts.URL + "/fail",
	}); r.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 on failed m3u fetch, got %d", r.Code)
	}

	// Successful HTTP M3U replacing previous host channels
	rec = doJSON(t, mux, http.MethodPost, "/LiveTv/TunerHosts", map[string]any{
		"Id":        host.Id,
		"Type":      "m3u",
		"Url":       ts.URL + "/playlist.m3u",
		"UserAgent": "AmbraTest/1.0",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("update host via HTTP status=%d body=%s", rec.Code, rec.Body.String())
	}

	// Local file M3U inside cfg.DataDir
	m3uFile := filepath.Join(cfg.DataDir, "local.m3u")
	_ = os.WriteFile(m3uFile, []byte("#EXTM3U\n#EXTINF:-1 group-title=\"Kids\",Disney\nhttp://tv/disney.m3u8\n"), 0o644)
	rec = doJSON(t, mux, http.MethodPost, "/LiveTv/TunerHosts", map[string]any{
		"Type": "m3u",
		"Url":  m3uFile,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("file m3u status=%d body=%s", rec.Code, rec.Body.String())
	}
	var fileHost TunerHostInfo
	_ = json.Unmarshal(rec.Body.Bytes(), &fileHost)

	// Missing local file
	if r := doJSON(t, mux, http.MethodPost, "/LiveTv/TunerHosts", map[string]any{
		"Type": "m3u",
		"Url":  filepath.Join(t.TempDir(), "missing.m3u"),
	}); r.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 on missing file, got %d", r.Code)
	}

	// List channels with filters & pagination
	rec = doJSON(t, mux, http.MethodGet, "/LiveTv/Channels?SearchTerm=cnn&GroupTitle=news&Type=tv&StartIndex=0&Limit=10", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /LiveTv/Channels status=%d", rec.Code)
	}
	var chRes dto.QueryResult[ChannelInfo]
	_ = json.Unmarshal(rec.Body.Bytes(), &chRes)
	if chRes.TotalRecordCount != 1 || chRes.Items[0].Name != "CNN" {
		t.Fatalf("unexpected channels: %+v", chRes)
	}

	// Get single channel & 404
	chID := chRes.Items[0].Id
	if r := doJSON(t, mux, http.MethodGet, "/LiveTv/Channels/"+chID, nil); r.Code != http.StatusOK {
		t.Fatalf("GET /LiveTv/Channels/{Id} status=%d", r.Code)
	}
	if r := doJSON(t, mux, http.MethodGet, "/LiveTv/Channels/nonexistent", nil); r.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", r.Code)
	}

	// Programs GET, POST, Recommended, Get by ID
	rec = doJSON(t, mux, http.MethodGet, "/LiveTv/Programs?ChannelIds="+chID+"&SearchTerm=CNN&IsMovie=false&IsSeries=false", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /LiveTv/Programs status=%d", rec.Code)
	}
	var progRes dto.QueryResult[ProgramInfo]
	_ = json.Unmarshal(rec.Body.Bytes(), &progRes)
	if progRes.TotalRecordCount != 1 {
		t.Fatalf("expected 1 program, got %d", progRes.TotalRecordCount)
	}
	progID := progRes.Items[0].Id

	rec = doJSON(t, mux, http.MethodPost, "/LiveTv/Programs", map[string]any{
		"ChannelIds": []string{chID},
		"Limit":      5,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /LiveTv/Programs status=%d", rec.Code)
	}
	if r := doJSON(t, mux, http.MethodPost, "/LiveTv/Programs", "{bad"); r.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", r.Code)
	}

	if r := doJSON(t, mux, http.MethodGet, "/LiveTv/Programs/Recommended?Limit=5", nil); r.Code != http.StatusOK {
		t.Fatalf("GET /LiveTv/Programs/Recommended status=%d", r.Code)
	}
	if r := doJSON(t, mux, http.MethodGet, "/LiveTv/Programs/"+progID, nil); r.Code != http.StatusOK {
		t.Fatalf("GET /LiveTv/Programs/{Id} status=%d", r.Code)
	}
	if r := doJSON(t, mux, http.MethodGet, "/LiveTv/Programs/missing", nil); r.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", r.Code)
	}

	// Timer defaults with programId
	rec = doJSON(t, mux, http.MethodGet, "/LiveTv/Timers/Defaults?programId="+progID, nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), progID) {
		t.Fatalf("unexpected defaults: %d %s", rec.Code, rec.Body.String())
	}

	// GuideInfo
	if r := doJSON(t, mux, http.MethodGet, "/LiveTv/GuideInfo", nil); r.Code != http.StatusOK {
		t.Fatalf("GET /LiveTv/GuideInfo status=%d", r.Code)
	}

	// Info after tuners
	if r := doJSON(t, mux, http.MethodGet, "/LiveTv/Info", nil); r.Code != http.StatusOK || !strings.Contains(r.Body.String(), `"IsEnabled":true`) {
		t.Fatalf("expected IsEnabled true, got %s", r.Body.String())
	}

	// Reload from SQLite to confirm persistence
	reloaded := New(st, cfg)
	if len(reloaded.state.Channels) != 2 {
		t.Fatalf("expected 2 persisted channels, got %d", len(reloaded.state.Channels))
	}

	// Delete tuner host (missing id, nonexistent, valid)
	if r := doJSON(t, mux, http.MethodDelete, "/LiveTv/TunerHosts", nil); r.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", r.Code)
	}
	if r := doJSON(t, mux, http.MethodDelete, "/LiveTv/TunerHosts?id=missing", nil); r.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", r.Code)
	}
	if r := doJSON(t, mux, http.MethodDelete, "/LiveTv/TunerHosts?id="+fileHost.Id, nil); r.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", r.Code)
	}
}

func TestLiveTVRecordingsTimersSeriesTimersAndListingProviders(t *testing.T) {
	h, mux, _, _ := setupLiveTVTest(t)

	// Recordings
	recItem := h.AddRecording(context.Background(), RecordingInfo{
		Name:      "Evening News",
		ChannelId: "ch1",
	})
	if r := doJSON(t, mux, http.MethodGet, "/LiveTv/Recordings?ChannelId=ch1&Status=Completed", nil); r.Code != http.StatusOK {
		t.Fatalf("GET /LiveTv/Recordings status=%d", r.Code)
	}
	if r := doJSON(t, mux, http.MethodGet, "/LiveTv/Recordings/Folders", nil); r.Code != http.StatusOK {
		t.Fatalf("GET /LiveTv/Recordings/Folders status=%d", r.Code)
	}
	if r := doJSON(t, mux, http.MethodGet, "/LiveTv/Recordings/"+recItem.Id, nil); r.Code != http.StatusOK {
		t.Fatalf("GET /LiveTv/Recordings/{Id} status=%d", r.Code)
	}
	if r := doJSON(t, mux, http.MethodGet, "/LiveTv/Recordings/missing", nil); r.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", r.Code)
	}
	if r := doJSON(t, mux, http.MethodDelete, "/LiveTv/Recordings/"+recItem.Id, nil); r.Code != http.StatusNoContent {
		t.Fatalf("DELETE recording status=%d", r.Code)
	}
	if r := doJSON(t, mux, http.MethodDelete, "/LiveTv/Recordings/"+recItem.Id, nil); r.Code != http.StatusNotFound {
		t.Fatalf("expected 404 on second delete, got %d", r.Code)
	}

	// Timers CRUD
	if r := doJSON(t, mux, http.MethodPost, "/LiveTv/Timers", "{bad"); r.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", r.Code)
	}
	res := doJSON(t, mux, http.MethodPost, "/LiveTv/Timers", map[string]any{
		"Name":      "Record Match",
		"ChannelId": "ch1",
	})
	if res.Code != http.StatusOK {
		t.Fatalf("POST /LiveTv/Timers status=%d", res.Code)
	}
	var timer TimerInfo
	_ = json.Unmarshal(res.Body.Bytes(), &timer)

	if r := doJSON(t, mux, http.MethodGet, "/LiveTv/Timers?ChannelId=ch1", nil); r.Code != http.StatusOK {
		t.Fatalf("GET /LiveTv/Timers status=%d", r.Code)
	}
	if r := doJSON(t, mux, http.MethodGet, "/LiveTv/Timers/"+timer.Id, nil); r.Code != http.StatusOK {
		t.Fatalf("GET /LiveTv/Timers/{Id} status=%d", r.Code)
	}
	if r := doJSON(t, mux, http.MethodGet, "/LiveTv/Timers/missing", nil); r.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", r.Code)
	}
	if r := doJSON(t, mux, http.MethodPost, "/LiveTv/Timers/"+timer.Id, "{bad"); r.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", r.Code)
	}
	if r := doJSON(t, mux, http.MethodPost, "/LiveTv/Timers/"+timer.Id, map[string]any{
		"Name":      "Record Match Updated",
		"ChannelId": "ch1",
	}); r.Code != http.StatusOK {
		t.Fatalf("POST /LiveTv/Timers/{Id} status=%d", r.Code)
	}
	if r := doJSON(t, mux, http.MethodPost, "/LiveTv/Timers/missing", map[string]any{"Name": "x"}); r.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", r.Code)
	}
	if r := doJSON(t, mux, http.MethodDelete, "/LiveTv/Timers/"+timer.Id, nil); r.Code != http.StatusNoContent {
		t.Fatalf("DELETE /LiveTv/Timers/{Id} status=%d", r.Code)
	}
	if r := doJSON(t, mux, http.MethodDelete, "/LiveTv/Timers/"+timer.Id, nil); r.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", r.Code)
	}

	// SeriesTimers CRUD
	if r := doJSON(t, mux, http.MethodPost, "/LiveTv/SeriesTimers", "{bad"); r.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", r.Code)
	}
	res = doJSON(t, mux, http.MethodPost, "/LiveTv/SeriesTimers", map[string]any{
		"Name":          "Weekly Drama",
		"RecordNewOnly": true,
	})
	if res.Code != http.StatusOK {
		t.Fatalf("POST /LiveTv/SeriesTimers status=%d", res.Code)
	}
	var st SeriesTimerInfo
	_ = json.Unmarshal(res.Body.Bytes(), &st)

	if r := doJSON(t, mux, http.MethodGet, "/LiveTv/SeriesTimers", nil); r.Code != http.StatusOK {
		t.Fatalf("GET /LiveTv/SeriesTimers status=%d", r.Code)
	}
	if r := doJSON(t, mux, http.MethodGet, "/LiveTv/SeriesTimers/"+st.Id, nil); r.Code != http.StatusOK {
		t.Fatalf("GET /LiveTv/SeriesTimers/{Id} status=%d", r.Code)
	}
	if r := doJSON(t, mux, http.MethodGet, "/LiveTv/SeriesTimers/missing", nil); r.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", r.Code)
	}
	if r := doJSON(t, mux, http.MethodPost, "/LiveTv/SeriesTimers/"+st.Id, "{bad"); r.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", r.Code)
	}
	if r := doJSON(t, mux, http.MethodPost, "/LiveTv/SeriesTimers/"+st.Id, map[string]any{"Name": "Updated"}); r.Code != http.StatusOK {
		t.Fatalf("POST /LiveTv/SeriesTimers/{Id} status=%d", r.Code)
	}
	if r := doJSON(t, mux, http.MethodPost, "/LiveTv/SeriesTimers/missing", map[string]any{"Name": "x"}); r.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", r.Code)
	}
	if r := doJSON(t, mux, http.MethodDelete, "/LiveTv/SeriesTimers/"+st.Id, nil); r.Code != http.StatusNoContent {
		t.Fatalf("DELETE /LiveTv/SeriesTimers/{Id} status=%d", r.Code)
	}
	if r := doJSON(t, mux, http.MethodDelete, "/LiveTv/SeriesTimers/"+st.Id, nil); r.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", r.Code)
	}

	// ListingProviders CRUD
	if r := doJSON(t, mux, http.MethodPost, "/LiveTv/ListingProviders", "{bad"); r.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", r.Code)
	}
	res = doJSON(t, mux, http.MethodPost, "/LiveTv/ListingProviders", map[string]any{
		"Path": "http://epg.local/guide.xml",
	})
	if res.Code != http.StatusOK {
		t.Fatalf("POST /LiveTv/ListingProviders status=%d", res.Code)
	}
	var lp ListingProviderInfo
	_ = json.Unmarshal(res.Body.Bytes(), &lp)
	// Update existing
	if r := doJSON(t, mux, http.MethodPost, "/LiveTv/ListingProviders", map[string]any{
		"Id":      lp.Id,
		"Type":    "xmltv",
		"Country": "CN",
	}); r.Code != http.StatusOK {
		t.Fatalf("update listing provider status=%d", r.Code)
	}
	if r := doJSON(t, mux, http.MethodDelete, "/LiveTv/ListingProviders", nil); r.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", r.Code)
	}
	if r := doJSON(t, mux, http.MethodDelete, "/LiveTv/ListingProviders?id=missing", nil); r.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", r.Code)
	}
	if r := doJSON(t, mux, http.MethodDelete, "/LiveTv/ListingProviders?id="+lp.Id, nil); r.Code != http.StatusNoContent {
		t.Fatalf("DELETE /LiveTv/ListingProviders status=%d", r.Code)
	}

	// Paginate edge cases & nil store/cfg
	nilH := New(nil, nil)
	if nilH.serverID() != "" {
		t.Fatalf("expected empty serverID")
	}
	p, tot := paginate([]int{1, 2, 3}, -1, 2)
	if tot != 3 || len(p) != 2 {
		t.Fatalf("unexpected paginate: %v %d", p, tot)
	}
	p, tot = paginate([]int{1, 2, 3}, 10, 2)
	if tot != 3 || len(p) != 0 {
		t.Fatalf("unexpected paginate beyond end: %v %d", p, tot)
	}
}

func TestLiveTVSSRFAndLocalFileBlocking(t *testing.T) {
	_, mux, _, cfg := setupLiveTVTest(t)

	// Local HTTP server on loopback must be blocked by default safe client
	loopbackSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("#EXTM3U\n#EXTINF:-1,Secret\nhttp://internal/stream.m3u8\n"))
	}))
	defer loopbackSrv.Close()

	// Outside directory files for path traversal & symlink escape tests
	outsideDir := t.TempDir()
	outsideM3U := filepath.Join(outsideDir, "outside.m3u")
	_ = os.WriteFile(outsideM3U, []byte("#EXTM3U\n#EXTINF:-1,Outside\nhttp://tv/out.m3u8\n"), 0o644)

	wrongExtInData := filepath.Join(cfg.DataDir, "secret.txt")
	_ = os.WriteFile(wrongExtInData, []byte("#EXTM3U\n#EXTINF:-1,Txt\nhttp://tv/txt.m3u8\n"), 0o644)

	symlinkEscape := filepath.Join(cfg.DataDir, "escape.m3u")
	_ = os.Symlink(outsideM3U, symlinkEscape)

	blockedSources := []struct {
		name string
		src  string
	}{
		{"loopback http", loopbackSrv.URL + "/playlist.m3u"},
		{"uppercase HTTP loopback", "HTTP://127.0.0.1:8096/playlist.m3u"},
		{"cloud metadata", "http://169.254.169.254/latest/meta-data/"},
		{"private LAN", "http://192.168.1.1/playlist.m3u"},
		{"file scheme", "file:///etc/passwd"},
		{"etc passwd", "/etc/passwd"},
		{"wrong extension inside DataDir", wrongExtInData},
		{"m3u outside DataDir", outsideM3U},
		{"symlink escape to outside m3u", symlinkEscape},
	}

	for _, tc := range blockedSources {
		t.Run(tc.name, func(t *testing.T) {
			rec := doJSON(t, mux, http.MethodPost, "/LiveTv/TunerHosts", map[string]any{
				"Type": "m3u",
				"Url":  tc.src,
			})
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("expected 400 for blocked source %q, got %d body=%s", tc.src, rec.Code, rec.Body.String())
			}
		})
	}

	// Valid .m3u8 file inside DataDir must succeed
	validM3U8 := filepath.Join(cfg.DataDir, "valid.m3u8")
	_ = os.WriteFile(validM3U8, []byte("#EXTM3U\n#EXTINF:-1,Allowed Channel\nhttp://tv/allowed.m3u8\n"), 0o644)
	rec := doJSON(t, mux, http.MethodPost, "/LiveTv/TunerHosts", map[string]any{
		"Type": "m3u",
		"Url":  validM3U8,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for valid .m3u8 in DataDir, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestLiveTVChannelStreamProxy(t *testing.T) {
	h, mux, _, _ := setupLiveTVTest(t)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/live.ts":
			w.Header().Set("Content-Type", "video/mp2t")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("LIVE_TS_STREAM_PAYLOAD"))
		case "/error":
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer upstream.Close()

	m3u := "#EXTM3U\n" +
		"#EXTINF:-1 tvg-id=\"ch-ok\",Live OK\n" + upstream.URL + "/live.ts\n" +
		"#EXTINF:-1 tvg-id=\"ch-err\",Live Err\n" + upstream.URL + "/error\n" +
		"#EXTINF:-1 tvg-id=\"ch-ssrf\",SSRF Metadata\nhttp://169.254.169.254/latest/meta-data/\n" +
		"#EXTINF:-1 tvg-id=\"ch-file\",File Scheme\nfile:///etc/passwd\n"

	rec := doJSON(t, mux, http.MethodPost, "/LiveTv/TunerHosts", map[string]any{
		"Type":       "m3u",
		"M3UContent": m3u,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("save tuner host: %d %s", rec.Code, rec.Body.String())
	}

	// Collect channel IDs by TvgId
	rec = doJSON(t, mux, http.MethodGet, "/LiveTv/Channels", nil)
	var chRes dto.QueryResult[ChannelInfo]
	_ = json.Unmarshal(rec.Body.Bytes(), &chRes)
	byTvg := make(map[string]ChannelInfo, len(chRes.Items))
	for _, ch := range chRes.Items {
		byTvg[ch.TvgId] = ch
	}

	// Verify GetChannelByID
	if ch, ok := h.GetChannelByID(byTvg["ch-ok"].Id); !ok || ch.Name != "Live OK" {
		t.Fatalf("GetChannelByID failed: ok=%v ch=%+v", ok, ch)
	}
	if _, ok := h.GetChannelByID("missing"); ok {
		t.Fatalf("expected missing channel to return false")
	}

	// 1. Valid stream proxy
	w := doJSON(t, mux, http.MethodGet, "/LiveTv/Channels/"+byTvg["ch-ok"].Id+"/Stream", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for valid stream, got %d body=%s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct != "video/mp2t" {
		t.Fatalf("expected Content-Type video/mp2t, got %q", ct)
	}
	if w.Body.String() != "LIVE_TS_STREAM_PAYLOAD" {
		t.Fatalf("unexpected stream body: %q", w.Body.String())
	}

	// 2. Not found channel -> 404
	if w := doJSON(t, mux, http.MethodGet, "/LiveTv/Channels/nonexistent/Stream", nil); w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown channel, got %d", w.Code)
	}

	// 3. Upstream 500 -> 502
	if w := doJSON(t, mux, http.MethodGet, "/LiveTv/Channels/"+byTvg["ch-err"].Id+"/Stream", nil); w.Code != http.StatusBadGateway {
		t.Fatalf("expected 502 for upstream error, got %d", w.Code)
	}

	// 4. SSRF cloud metadata blocked -> 400
	if w := doJSON(t, mux, http.MethodGet, "/LiveTv/Channels/"+byTvg["ch-ssrf"].Id+"/Stream", nil); w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for SSRF metadata URL, got %d body=%s", w.Code, w.Body.String())
	}

	// 5. file:// scheme blocked -> 400
	if w := doJSON(t, mux, http.MethodGet, "/LiveTv/Channels/"+byTvg["ch-file"].Id+"/Stream", nil); w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for file:// URL, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestDueRecordingJobsAndDVRIntegration(t *testing.T) {
	h, mux, st, cfg := setupLiveTVTest(t)

	rec := dvr.NewRecorder(dvr.Options{
		DataDir:    cfg.DataDir,
		FFmpegPath: "/bin/sleep",
		ArgsBuilder: func(job dvr.RecordingJob, outFile string) []string {
			return []string{"30"}
		},
	})
	defer func() { _ = rec.Close() }()
	h.SetRecorder(rec)

	// Seed two channels: one valid LAN/loopback URL and one blocked cloud metadata URL
	m3u := "#EXTM3U\n" +
		"#EXTINF:-1 tvg-id=\"good\",Good TV\nhttp://127.0.0.1:18080/live.ts\n" +
		"#EXTINF:-1 tvg-id=\"bad\",Bad TV\nhttp://169.254.169.254/live.ts\n"
	res := doJSON(t, mux, http.MethodPost, "/LiveTv/TunerHosts", map[string]any{
		"Type":       "m3u",
		"M3UContent": m3u,
	})
	if res.Code != http.StatusOK {
		t.Fatalf("seed tuner: %d", res.Code)
	}
	var chRes dto.QueryResult[ChannelInfo]
	_ = json.Unmarshal(doJSON(t, mux, http.MethodGet, "/LiveTv/Channels", nil).Body.Bytes(), &chRes)
	var goodCh, badCh ChannelInfo
	for _, ch := range chRes.Items {
		if ch.TvgId == "good" {
			goodCh = ch
		} else if ch.TvgId == "bad" {
			badCh = ch
		}
	}

	now := time.Now().UTC()
	// Timer 1: due via PrePaddingSeconds (starts in 30s, pre-padding 120s)
	r1 := doJSON(t, mux, http.MethodPost, "/LiveTv/Timers", map[string]any{
		"Name":               "Due Show",
		"ChannelId":          goodCh.Id,
		"StartDate":          now.Add(30 * time.Second),
		"EndDate":            now.Add(10 * time.Minute),
		"PrePaddingSeconds":  120,
		"PostPaddingSeconds": 60,
	})
	var dueTimer TimerInfo
	_ = json.Unmarshal(r1.Body.Bytes(), &dueTimer)

	// Timer 2: future (not due yet)
	doJSON(t, mux, http.MethodPost, "/LiveTv/Timers", map[string]any{
		"Name":      "Future Show",
		"ChannelId": goodCh.Id,
		"StartDate": now.Add(2 * time.Hour),
		"EndDate":   now.Add(3 * time.Hour),
	})

	// Timer 3: due window but unsafe SSRF channel URL -> must be skipped
	r3 := doJSON(t, mux, http.MethodPost, "/LiveTv/Timers", map[string]any{
		"Name":      "Unsafe Show",
		"ChannelId": badCh.Id,
		"StartDate": now.Add(-1 * time.Minute),
		"EndDate":   now.Add(10 * time.Minute),
	})
	var unsafeTimer TimerInfo
	_ = json.Unmarshal(r3.Body.Bytes(), &unsafeTimer)

	// DueRecordingJobs should return only dueTimer
	jobs, err := h.DueRecordingJobs(context.Background())
	if err != nil {
		t.Fatalf("DueRecordingJobs: %v", err)
	}
	if len(jobs) != 1 || jobs[0].TimerID != dueTimer.Id || jobs[0].StreamURL != goodCh.StreamUrl || jobs[0].ChannelName != "Good TV" {
		t.Fatalf("unexpected due jobs: %+v", jobs)
	}
	if jobs[0].Duration <= 0 {
		t.Fatalf("expected positive duration, got %v", jobs[0].Duration)
	}

	// Second call should return nothing (dueTimer is now InProgress, unsafeTimer still blocked)
	jobs2, err := h.DueRecordingJobs(context.Background())
	if err != nil || len(jobs2) != 0 {
		t.Fatalf("expected 0 jobs on second poll, got %+v err=%v", jobs2, err)
	}

	// Verify InProgress persisted in SQLite
	reloaded := New(st, cfg)
	foundInProgress := false
	for _, tm := range reloaded.state.Timers {
		if tm.Id == dueTimer.Id && tm.Status == "InProgress" {
			foundInProgress = true
		}
		if tm.Id == unsafeTimer.Id && tm.Status != "New" {
			t.Fatalf("unsafe timer should remain New, got %q", tm.Status)
		}
	}
	if !foundInProgress {
		t.Fatalf("expected dueTimer to be persisted as InProgress")
	}

	// Start active recording on recorder, then DELETE timer -> must cancel active recording
	if _, err := rec.StartRecording(context.Background(), jobs[0]); err != nil {
		t.Fatalf("StartRecording: %v", err)
	}
	if len(rec.ActiveRecordings()) != 1 {
		t.Fatalf("expected 1 active recording")
	}
	if w := doJSON(t, mux, http.MethodDelete, "/LiveTv/Timers/"+dueTimer.Id, nil); w.Code != http.StatusNoContent {
		t.Fatalf("DELETE timer: %d", w.Code)
	}
	// Wait briefly for cancelled process to finish
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		state, ok := rec.GetRecording(dueTimer.Id)
		if ok && state.Status == dvr.StatusCancelled {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if state, ok := rec.GetRecording(dueTimer.Id); !ok || state.Status != dvr.StatusCancelled {
		t.Fatalf("expected recording to be cancelled on timer delete, got ok=%v state=%+v", ok, state)
	}

	// DELETE recording removes file inside RecordingsDir, but refuses to remove outside file
	if err := os.MkdirAll(rec.RecordingsDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	insideFile := filepath.Join(rec.RecordingsDir(), "show.mkv")
	if err := os.WriteFile(insideFile, []byte("video"), 0o600); err != nil {
		t.Fatal(err)
	}
	outsideFile := filepath.Join(t.TempDir(), "keep.mkv")
	if err := os.WriteFile(outsideFile, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}

	recInside := h.AddRecording(context.Background(), RecordingInfo{Name: "Inside", Path: insideFile})
	recOutside := h.AddRecording(context.Background(), RecordingInfo{Name: "Outside", Path: outsideFile})

	if w := doJSON(t, mux, http.MethodDelete, "/LiveTv/Recordings/"+recInside.Id, nil); w.Code != http.StatusNoContent {
		t.Fatalf("DELETE inside recording: %d", w.Code)
	}
	if _, err := os.Stat(insideFile); !os.IsNotExist(err) {
		t.Fatalf("expected inside recording file to be deleted, stat err=%v", err)
	}

	if w := doJSON(t, mux, http.MethodDelete, "/LiveTv/Recordings/"+recOutside.Id, nil); w.Code != http.StatusNoContent {
		t.Fatalf("DELETE outside recording: %d", w.Code)
	}
	if _, err := os.Stat(outsideFile); err != nil {
		t.Fatalf("expected outside file to remain untouched, stat err=%v", err)
	}
}
