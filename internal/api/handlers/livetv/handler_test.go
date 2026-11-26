package livetv

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ambra-media/ambra/internal/api/dto"
	"github.com/ambra-media/ambra/internal/api/middleware"
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
	h.mu.Lock()
	h.state.Channels = append(h.state.Channels, ChannelInfo{Id: "ch1", Name: "Channel 1", Type: "TvChannel", ChannelType: "TV"})
	h.mu.Unlock()
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
	// Invalid/unreachable XMLTV path must fail with 400 instead of silently succeeding
	if r := doJSON(t, mux, http.MethodPost, "/LiveTv/ListingProviders", map[string]any{
		"Path": "http://169.254.169.254/guide.xml",
	}); r.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 on blocked xmltv path, got %d", r.Code)
	}
	// Empty path saves gracefully
	res = doJSON(t, mux, http.MethodPost, "/LiveTv/ListingProviders", map[string]any{
		"Type": "xmltv",
	})
	if res.Code != http.StatusOK {
		t.Fatalf("POST /LiveTv/ListingProviders status=%d body=%s", res.Code, res.Body.String())
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
	h, mux, _, cfg := setupLiveTVTest(t)

	// Loopback HTTP server MUST be blocked by default (H-04 no-loopback SSRF protection)
	loopbackSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("#EXTM3U\n#EXTINF:-1,LAN Channel\nhttp://192.168.1.50/stream.m3u8\n"))
	}))
	defer loopbackSrv.Close()

	loopRec := doJSON(t, mux, http.MethodPost, "/LiveTv/TunerHosts", map[string]any{
		"Type": "m3u",
		"Url":  loopbackSrv.URL + "/playlist.m3u",
	})
	if loopRec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 blocking loopback M3U tuner by default, got %d body=%s", loopRec.Code, loopRec.Body.String())
	}

	// With explicit test HTTP client override, local test server succeeds
	h.SetHTTPClient(loopbackSrv.Client())
	lanRec := doJSON(t, mux, http.MethodPost, "/LiveTv/TunerHosts", map[string]any{
		"Type": "m3u",
		"Url":  loopbackSrv.URL + "/playlist.m3u",
	})
	if lanRec.Code != http.StatusOK {
		t.Fatalf("expected 200 with custom test client, got %d body=%s", lanRec.Code, lanRec.Body.String())
	}
	h.customHTTPClient = false

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
		{"cloud metadata", "http://169.254.169.254/latest/meta-data/"},
		{"ipv4-mapped cloud metadata", "http://[::ffff:169.254.169.254]/latest/meta-data/"},
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
	h.SetStreamClient(upstream.Client())

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

	// Seed two channels: one valid LAN URL and one blocked cloud metadata URL
	m3u := "#EXTM3U\n" +
		"#EXTINF:-1 tvg-id=\"good\",Good TV\nhttp://192.168.1.50:18080/live.ts\n" +
		"#EXTINF:-1 tvg-id=\"bad\",Bad TV\nhttp://169.254.169.254/live.ts\n"
	res := doJSON(t, mux, http.MethodPost, "/LiveTv/TunerHosts", map[string]any{
		"Type":                "m3u",
		"AllowPrivateNetwork": true,
		"M3UContent":          m3u,
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

func TestHLSPlaylistURIRewritingAndTargetSSRF(t *testing.T) {
	h, mux, _, _ := setupLiveTVTest(t)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/hls/master.m3u8":
			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			_, _ = w.Write([]byte("#EXTM3U\n" +
				"#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID=\"aud\",NAME=\"Eng\",URI=\"audio/eng.m3u8\"\n" +
				"#EXT-X-STREAM-INF:BANDWIDTH=2000000\n" +
				"sub/720p.m3u8\n"))
		case "/hls/sub/720p.m3u8":
			w.Header().Set("Content-Type", "application/x-mpegURL")
			_, _ = w.Write([]byte("#EXTM3U\n" +
				"#EXT-X-KEY:METHOD=AES-128,URI=\"../keys/enc.key\"\n" +
				"#EXT-X-MAP:URI=\"init.mp4\"\n" +
				"#EXTINF:6.0,\n" +
				"seg0.ts\n" +
				"#EXTINF:6.0,\n" +
				"/cdn/seg1.ts\n"))
		case "/hls/sub/seg0.ts":
			w.Header().Set("Content-Type", "video/mp2t")
			_, _ = w.Write([]byte("SEGMENT_0_BYTES"))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer upstream.Close()
	h.SetStreamClient(upstream.Client())

	m3u := "#EXTM3U\n#EXTINF:-1 tvg-id=\"hls-ch\",HLS Channel\n" + upstream.URL + "/hls/master.m3u8\n"
	if r := doJSON(t, mux, http.MethodPost, "/LiveTv/TunerHosts", map[string]any{
		"Type":       "m3u",
		"M3UContent": m3u,
	}); r.Code != http.StatusOK {
		t.Fatalf("seed tuner: %d %s", r.Code, r.Body.String())
	}

	var chRes dto.QueryResult[ChannelInfo]
	_ = json.Unmarshal(doJSON(t, mux, http.MethodGet, "/LiveTv/Channels", nil).Body.Bytes(), &chRes)
	chID := chRes.Items[0].Id

	// 1. Request master playlist with api_key query param -> relative signed Stream?target=...&exp=...&lan=1&sig=... WITHOUT leaking primary api_key
	req := httptest.NewRequest(http.MethodGet, "/LiveTv/Channels/"+chID+"/Stream?api_key=tok-query", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("master m3u8 status=%d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	audioTarget := upstream.URL + "/hls/audio/eng.m3u8"
	subTarget := upstream.URL + "/hls/sub/720p.m3u8"
	if !strings.Contains(body, `URI="Stream?target=`+url.QueryEscape(audioTarget)+`&exp=`) ||
		!strings.Contains(body, `&lan=1&sig=`) {
		t.Fatalf("expected rewritten EXT-X-MEDIA URI in:\n%s", body)
	}
	if strings.Contains(body, "api_key=tok-query") {
		t.Fatalf("rewritten HLS playlist must NOT leak primary api_key in segment URLs:\n%s", body)
	}
	if !strings.Contains(body, "Stream?target="+url.QueryEscape(subTarget)+"&exp=") {
		t.Fatalf("expected rewritten sub-playlist URI in:\n%s", body)
	}
	if strings.Contains(body, "/emby/") {
		t.Fatalf("expected relative rewrite without /emby/ prefix, got:\n%s", body)
	}

	exp := time.Now().UTC().Add(time.Hour).Unix()
	expStr := strconv.FormatInt(exp, 10)

	// 2. Request sub-playlist via signed target query param + X-Emby-Token header -> signed URIs without leaking header token
	subReq := httptest.NewRequest(http.MethodGet, "/LiveTv/Channels/"+chID+"/Stream?target="+url.QueryEscape(subTarget)+"&exp="+expStr+"&lan=1&sig="+h.signProxyTarget(chID, subTarget, true, exp), nil)
	subReq.Header.Set("X-Emby-Token", "tok-header")
	subRec := httptest.NewRecorder()
	mux.ServeHTTP(subRec, subReq)
	if subRec.Code != http.StatusOK {
		t.Fatalf("sub m3u8 status=%d body=%s", subRec.Code, subRec.Body.String())
	}
	subBody := subRec.Body.String()
	keyTarget := upstream.URL + "/hls/keys/enc.key"
	mapTarget := upstream.URL + "/hls/sub/init.mp4"
	seg0Target := upstream.URL + "/hls/sub/seg0.ts"
	seg1Target := upstream.URL + "/cdn/seg1.ts"
	for _, want := range []string{
		`URI="Stream?target=` + url.QueryEscape(keyTarget) + `&exp=`,
		`URI="Stream?target=` + url.QueryEscape(mapTarget) + `&exp=`,
		"Stream?target=" + url.QueryEscape(seg0Target) + "&exp=",
		"Stream?target=" + url.QueryEscape(seg1Target) + "&exp=",
	} {
		if !strings.Contains(subBody, want) {
			t.Fatalf("expected %q in rewritten sub-playlist:\n%s", want, subBody)
		}
	}
	if strings.Contains(subBody, "tok-header") {
		t.Fatalf("rewritten sub-playlist must NOT leak X-Emby-Token:\n%s", subBody)
	}

	// 3. Request with Authorization header token -> succeeds without leaking Authorization token into rewritten URLs
	authReq := httptest.NewRequest(http.MethodGet, "/LiveTv/Channels/"+chID+"/Stream", nil)
	authReq.Header.Set("Authorization", `MediaBrowser Client="Test", Token="tok-auth"`)
	authRec := httptest.NewRecorder()
	mux.ServeHTTP(authRec, authReq)
	if authRec.Code != http.StatusOK || strings.Contains(authRec.Body.String(), "tok-auth") || !strings.Contains(authRec.Body.String(), "&sig=") {
		t.Fatalf("expected signed HLS rewrite without leaking Authorization token, got %d:\n%s", authRec.Code, authRec.Body.String())
	}

	// 4. Proxy actual TS segment via signed target param
	segReq := httptest.NewRequest(http.MethodGet, "/LiveTv/Channels/"+chID+"/Stream?target="+url.QueryEscape(seg0Target)+"&exp="+expStr+"&lan=1&sig="+h.signProxyTarget(chID, seg0Target, true, exp), nil)
	segRec := httptest.NewRecorder()
	mux.ServeHTTP(segRec, segReq)
	if segRec.Code != http.StatusOK || segRec.Body.String() != "SEGMENT_0_BYTES" {
		t.Fatalf("expected segment bytes, got %d %q", segRec.Code, segRec.Body.String())
	}

	// 5. Missing, forged, expired, cross-scope, or cross-channel HMAC signature on target param -> 403 Forbidden
	expiredExp := time.Now().UTC().Add(-time.Minute).Unix()
	expiredStr := strconv.FormatInt(expiredExp, 10)
	for _, tc := range []struct {
		name  string
		query string
	}{
		{"missing sig", "target=" + url.QueryEscape(seg0Target) + "&exp=" + expStr + "&lan=1"},
		{"missing exp", "target=" + url.QueryEscape(seg0Target) + "&lan=1&sig=" + h.signProxyTarget(chID, seg0Target, true, exp)},
		{"expired sig", "target=" + url.QueryEscape(seg0Target) + "&exp=" + expiredStr + "&lan=1&sig=" + h.signProxyTarget(chID, seg0Target, true, expiredExp)},
		{"missing lan", "target=" + url.QueryEscape(seg0Target) + "&exp=" + expStr + "&sig=" + h.signProxyTarget(chID, seg0Target, true, exp)},
		{"tampered lan scope", "target=" + url.QueryEscape(seg0Target) + "&exp=" + expStr + "&lan=1&sig=" + h.signProxyTarget(chID, seg0Target, false, exp)},
		{"invalid hex sig", "target=" + url.QueryEscape(seg0Target) + "&exp=" + expStr + "&lan=1&sig=not-hex"},
		{"wrong sig", "target=" + url.QueryEscape(seg0Target) + "&exp=" + expStr + "&lan=1&sig=" + strings.Repeat("a", 64)},
		{"wrong channel sig", "target=" + url.QueryEscape(seg0Target) + "&exp=" + expStr + "&lan=1&sig=" + h.signProxyTarget("other-channel", seg0Target, true, exp)},
		{"unsigned ssrf target", "target=" + url.QueryEscape("http://169.254.169.254/latest/meta-data/")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			badReq := httptest.NewRequest(http.MethodGet, "/LiveTv/Channels/"+chID+"/Stream?"+tc.query, nil)
			badRec := httptest.NewRecorder()
			mux.ServeHTTP(badRec, badReq)
			if badRec.Code != http.StatusForbidden {
				t.Fatalf("expected 403 for %s, got %d body=%s", tc.name, badRec.Code, badRec.Body.String())
			}
		})
	}

	// 6. Defense-in-depth: even with a valid signature, SSRF/unsafe targets are blocked with 400
	for _, unsafeTarget := range []string{
		"http://169.254.169.254/latest/meta-data/",
		"file:///etc/passwd",
	} {
		sig := h.signProxyTarget(chID, unsafeTarget, true, exp)
		badReq := httptest.NewRequest(http.MethodGet, "/LiveTv/Channels/"+chID+"/Stream?target="+url.QueryEscape(unsafeTarget)+"&exp="+expStr+"&lan=1&sig="+sig, nil)
		badRec := httptest.NewRecorder()
		mux.ServeHTTP(badRec, badReq)
		if badRec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for signed unsafe target %q, got %d body=%s", unsafeTarget, badRec.Code, badRec.Body.String())
		}
	}

	// 7. Unknown channel ID with target must still return 404
	notFoundReq := httptest.NewRequest(http.MethodGet, "/LiveTv/Channels/missing/Stream?target="+url.QueryEscape(seg0Target), nil)
	notFoundRec := httptest.NewRecorder()
	mux.ServeHTTP(notFoundRec, notFoundReq)
	if notFoundRec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown channel with target, got %d", notFoundRec.Code)
	}
}

func TestDVRCompletionCallbackUpdatesTimerAndCreatesRecording(t *testing.T) {
	h, mux, st, cfg := setupLiveTVTest(t)

	rec := dvr.NewRecorder(dvr.Options{
		DataDir:    cfg.DataDir,
		FFmpegPath: "/bin/sh",
		ArgsBuilder: func(job dvr.RecordingJob, outFile string) []string {
			if job.TimerID == "fail-timer" {
				return []string{"-c", "exit 1"}
			}
			return []string{"-c", "printf 'recorded-bytes' > \"$1\"", "--", outFile}
		},
	})
	defer func() { _ = rec.Close() }()
	h.SetRecorder(rec)
	rec.SetOnFinished(h.OnRecordingFinished)

	// Create two timers: one will succeed, one will fail
	h.mu.Lock()
	h.state.Channels = []ChannelInfo{
		{Id: "ch-1", Name: "CCTV 1", Type: "TvChannel", ChannelType: "TV"},
		{Id: "ch-2", Name: "CCTV 2", Type: "TvChannel", ChannelType: "TV"},
	}
	h.mu.Unlock()
	r1 := doJSON(t, mux, http.MethodPost, "/LiveTv/Timers", map[string]any{
		"Id":          "ok-timer",
		"Name":        "Evening News",
		"ChannelId":   "ch-1",
		"ChannelName": "CCTV 1",
		"Status":      "InProgress",
	})
	if r1.Code != http.StatusOK {
		t.Fatalf("create ok-timer: %d", r1.Code)
	}
	r2 := doJSON(t, mux, http.MethodPost, "/LiveTv/Timers", map[string]any{
		"Id":        "fail-timer",
		"Name":      "Broken Stream",
		"ChannelId": "ch-2",
		"Status":    "InProgress",
	})
	if r2.Code != http.StatusOK {
		t.Fatalf("create fail-timer: %d", r2.Code)
	}

	if _, err := rec.StartRecording(context.Background(), dvr.RecordingJob{
		TimerID:     "ok-timer",
		ChannelName: "CCTV 1",
		ProgramName: "Evening News",
		StreamURL:   "http://192.168.1.50:9999/ok.ts",
	}); err != nil {
		t.Fatalf("start ok-timer: %v", err)
	}
	if _, err := rec.StartRecording(context.Background(), dvr.RecordingJob{
		TimerID:     "fail-timer",
		ChannelName: "CCTV 2",
		ProgramName: "Broken Stream",
		StreamURL:   "http://192.168.1.50:9999/fail.ts",
	}); err != nil {
		t.Fatalf("start fail-timer: %v", err)
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		h.mu.RLock()
		okDone := false
		failDone := false
		for _, tm := range h.state.Timers {
			if tm.Id == "ok-timer" && tm.Status == "Completed" {
				okDone = true
			}
			if tm.Id == "fail-timer" && tm.Status == "Failed" {
				failDone = true
			}
		}
		recCount := len(h.state.Recordings)
		h.mu.RUnlock()
		if okDone && failDone && recCount == 1 {
			break
		}
		time.Sleep(15 * time.Millisecond)
	}

	// Reload from SQLite to confirm persistence
	reloaded := New(st, cfg)
	if len(reloaded.state.Recordings) != 1 {
		t.Fatalf("expected 1 persisted recording, got %d", len(reloaded.state.Recordings))
	}
	gotRec := reloaded.state.Recordings[0]
	if gotRec.ChannelId != "ch-1" || gotRec.Name != "Evening News" || gotRec.Status != "Completed" || gotRec.Path == "" {
		t.Fatalf("unexpected recording info: %+v", gotRec)
	}
	if _, err := os.Stat(gotRec.Path); err != nil {
		t.Fatalf("expected recorded file at %q: %v", gotRec.Path, err)
	}

	statuses := map[string]string{}
	for _, tm := range reloaded.state.Timers {
		statuses[tm.Id] = tm.Status
	}
	if statuses["ok-timer"] != "Completed" || statuses["fail-timer"] != "Failed" {
		t.Fatalf("unexpected timer statuses: %+v", statuses)
	}
}

func TestXMLTVParserAndListingProviderImport(t *testing.T) {
	h, mux, _, cfg := setupLiveTVTest(t)

	// Seed 3 channels: one matched by TvgId, one matched by display-name, one unmatched
	m3u := "#EXTM3U\n" +
		"#EXTINF:-1 tvg-id=\"cctv1.cn\",CCTV 1\nhttp://tv/1.m3u8\n" +
		"#EXTINF:-1 tvg-id=\"other\",HBO HD\nhttp://tv/2.m3u8\n" +
		"#EXTINF:-1 tvg-id=\"keep-me\",Unmatched TV\nhttp://tv/3.m3u8\n"
	if r := doJSON(t, mux, http.MethodPost, "/LiveTv/TunerHosts", map[string]any{
		"Type":       "m3u",
		"M3UContent": m3u,
	}); r.Code != http.StatusOK {
		t.Fatalf("seed tuner: %d", r.Code)
	}

	var chRes dto.QueryResult[ChannelInfo]
	_ = json.Unmarshal(doJSON(t, mux, http.MethodGet, "/LiveTv/Channels", nil).Body.Bytes(), &chRes)
	chByName := make(map[string]ChannelInfo, len(chRes.Items))
	for _, ch := range chRes.Items {
		chByName[ch.Name] = ch
	}

	xmltvDoc := `<?xml version="1.0" encoding="UTF-8"?>
<tv>
  <channel id="cctv1.cn">
    <display-name>CCTV 1</display-name>
  </channel>
  <channel id="hbo-xml-id">
    <display-name>HBO HD</display-name>
  </channel>
  <programme start="20260501200000 +0800" stop="20260501210000 +0800" channel="cctv1.cn">
    <title>Xinwen Lianbo</title>
    <desc>Evening news broadcast</desc>
    <category>News</category>
  </programme>
  <programme start="20260501140000" stop="20260501160000" channel="hbo-xml-id">
    <title>Inception</title>
    <desc>Sci-fi thriller</desc>
    <category>Movie</category>
  </programme>
</tv>`

	// 1. Test gzip-compressed .xml.gz file inside cfg.DataDir
	gzPath := filepath.Join(cfg.DataDir, "guide.xml.gz")
	var gzBuf bytes.Buffer
	gzw := gzip.NewWriter(&gzBuf)
	_, _ = gzw.Write([]byte(xmltvDoc))
	_ = gzw.Close()
	if err := os.WriteFile(gzPath, gzBuf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}

	rec := doJSON(t, mux, http.MethodPost, "/LiveTv/ListingProviders", map[string]any{
		"Type": "xmltv",
		"Path": gzPath,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("save xml.gz listing provider: %d %s", rec.Code, rec.Body.String())
	}

	var progRes dto.QueryResult[ProgramInfo]
	_ = json.Unmarshal(doJSON(t, mux, http.MethodGet, "/LiveTv/Programs", nil).Body.Bytes(), &progRes)
	if progRes.TotalRecordCount != 3 {
		t.Fatalf("expected 3 programs (2 XMLTV + 1 unmatched placeholder), got %d: %+v", progRes.TotalRecordCount, progRes.Items)
	}

	foundNews, foundMovie, foundUnmatched := false, false, false
	for _, p := range progRes.Items {
		switch p.Name {
		case "Xinwen Lianbo":
			foundNews = true
			if p.ChannelId != chByName["CCTV 1"].Id || p.Overview != "Evening news broadcast" {
				t.Fatalf("unexpected news program: %+v", p)
			}
			wantStart := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
			if !p.StartDate.Equal(wantStart) {
				t.Fatalf("expected UTC start %v, got %v", wantStart, p.StartDate)
			}
		case "Inception":
			foundMovie = true
			if p.ChannelId != chByName["HBO HD"].Id || !p.IsMovie {
				t.Fatalf("unexpected movie program: %+v", p)
			}
		case "Unmatched TV Live":
			foundUnmatched = true
		}
	}
	if !foundNews || !foundMovie || !foundUnmatched {
		t.Fatalf("missing expected programs: news=%v movie=%v unmatched=%v", foundNews, foundMovie, foundUnmatched)
	}

	// 2. Outside DataDir XML, SSRF URL, and malformed XML return 400 Bad Request
	outsideXML := filepath.Join(t.TempDir(), "evil.xml")
	_ = os.WriteFile(outsideXML, []byte(`<tv><programme start="20260501100000" stop="20260501110000" channel="cctv1.cn"><title>Evil</title></programme></tv>`), 0o600)
	invalidXML := filepath.Join(cfg.DataDir, "broken.xml")
	_ = os.WriteFile(invalidXML, []byte(`<tv><programme><unclosed>`), 0o600)
	for _, badPath := range []string{
		outsideXML,
		invalidXML,
		"http://169.254.169.254/guide.xml",
		"file:///etc/passwd",
	} {
		if r := doJSON(t, mux, http.MethodPost, "/LiveTv/ListingProviders", map[string]any{
			"Type": "xmltv",
			"Path": badPath,
		}); r.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for bad xmltv path %q, got %d body=%s", badPath, r.Code, r.Body.String())
		}
	}
	var progAfter dto.QueryResult[ProgramInfo]
	_ = json.Unmarshal(doJSON(t, mux, http.MethodGet, "/LiveTv/Programs", nil).Body.Bytes(), &progAfter)
	for _, p := range progAfter.Items {
		if p.Name == "Evil" {
			t.Fatalf("outside DataDir xmltv should not have been imported")
		}
	}

	// 3. HTTP XMLTV feed with custom HTTP client
	epgSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<tv><programme start="20260502100000 +0000" stop="20260502110000 +0000" channel="cctv1.cn"><title>Morning Show</title><category>Series</category></programme></tv>`))
	}))
	defer epgSrv.Close()
	h.SetHTTPClient(epgSrv.Client())

	if r := doJSON(t, mux, http.MethodPost, "/LiveTv/ListingProviders", map[string]any{
		"Type": "xmltv",
		"Path": epgSrv.URL + "/epg.xml",
	}); r.Code != http.StatusOK {
		t.Fatalf("save HTTP xmltv listing provider: %d", r.Code)
	}
	var progHTTP dto.QueryResult[ProgramInfo]
	_ = json.Unmarshal(doJSON(t, mux, http.MethodGet, "/LiveTv/Programs?ChannelIds="+chByName["CCTV 1"].Id, nil).Body.Bytes(), &progHTTP)
	if progHTTP.TotalRecordCount != 1 || progHTTP.Items[0].Name != "Morning Show" || !progHTTP.Items[0].IsSeries {
		t.Fatalf("expected HTTP XMLTV to replace CCTV 1 program, got %+v", progHTTP.Items)
	}
}

func TestXMLTVGzipBombAndProgrammeCap(t *testing.T) {
	// 1. Gzip bomb exceeding decompressed byte cap must fail
	var rawBuf bytes.Buffer
	rawBuf.WriteString("<tv>")
	for i := 0; i < 64; i++ {
		rawBuf.WriteString(`<programme start="20260501100000" stop="20260501110000" channel="ch1"><title>Show `)
		rawBuf.WriteString(strings.Repeat("A", 128))
		rawBuf.WriteString(`</title></programme>`)
	}
	rawBuf.WriteString("</tv>")

	var gzBuf bytes.Buffer
	gzw := gzip.NewWriter(&gzBuf)
	_, _ = gzw.Write(rawBuf.Bytes())
	_ = gzw.Close()

	if _, err := parseXMLTVWithLimits(bytes.NewReader(gzBuf.Bytes()), 1024, 200000); err == nil || !strings.Contains(err.Error(), "limit") {
		t.Fatalf("expected decompressed size limit error, got %v", err)
	}

	// 2. Programme count cap truncates cleanly at maxProgrammes
	guide, err := parseXMLTVWithLimits(bytes.NewReader(rawBuf.Bytes()), 10<<20, 5)
	if err != nil {
		t.Fatalf("expected capped parse to succeed, got %v", err)
	}
	if len(guide.Programmes) != 5 {
		t.Fatalf("expected 5 capped programmes, got %d", len(guide.Programmes))
	}
}

func TestDVRStartFailureTransitionsTimerToFailed(t *testing.T) {
	h, mux, st, cfg := setupLiveTVTest(t)

	rec := dvr.NewRecorder(dvr.Options{
		DataDir:    cfg.DataDir,
		FFmpegPath: filepath.Join(cfg.DataDir, "nonexistent-ffmpeg-binary"),
	})
	defer func() { _ = rec.Close() }()
	h.SetRecorder(rec)
	rec.SetOnFinished(h.OnRecordingFinished)

	h.mu.Lock()
	h.state.Channels = []ChannelInfo{{Id: "ch-1", Name: "CCTV 1", Type: "TvChannel", ChannelType: "TV"}}
	h.mu.Unlock()
	res := doJSON(t, mux, http.MethodPost, "/LiveTv/Timers", map[string]any{
		"Id":        "start-fail-timer",
		"Name":      "Missing Binary Show",
		"ChannelId": "ch-1",
		"Status":    "InProgress",
	})
	if res.Code != http.StatusOK {
		t.Fatalf("create timer: %d", res.Code)
	}

	if _, err := rec.StartRecording(context.Background(), dvr.RecordingJob{
		TimerID:     "start-fail-timer",
		ChannelName: "CCTV 1",
		ProgramName: "Missing Binary Show",
		StreamURL:   "http://192.168.1.50:9999/live.ts",
	}); err == nil {
		t.Fatal("expected StartRecording error for missing ffmpeg binary")
	}

	reloaded := New(st, cfg)
	found := false
	for _, tm := range reloaded.state.Timers {
		if tm.Id == "start-fail-timer" {
			found = true
			if tm.Status != "Failed" {
				t.Fatalf("expected timer status Failed after start failure, got %q", tm.Status)
			}
		}
	}
	if !found {
		t.Fatal("start-fail-timer not found in persisted state")
	}
}

func TestLiveTVExportedDelegationHelpers(t *testing.T) {
	h, mux, st, _ := setupLiveTVTest(t)
	ctx := context.Background()
	userRepo := sqlite.NewUserRepo(st)
	h.SetUserRepo(userRepo)

	m3u := "#EXTM3U\n#EXTINF:-1,ChanA\nhttp://192.168.1.5/a.ts\n"
	doJSON(t, mux, http.MethodPost, "/LiveTv/TunerHosts", map[string]any{"Type": "m3u", "M3UContent": m3u})
	var chRes dto.QueryResult[ChannelInfo]
	_ = json.Unmarshal(doJSON(t, mux, http.MethodGet, "/LiveTv/Channels", nil).Body.Bytes(), &chRes)
	chID := chRes.Items[0].Id

	if err := h.SetChannelDisabled(ctx, chID, true); err != nil {
		t.Fatalf("expected SetChannelDisabled nil, got %v", err)
	}
	if err := h.SetChannelSortIndex(ctx, chID, 7); err != nil {
		t.Fatalf("expected SetChannelSortIndex nil, got %v", err)
	}
	if err := h.SetChannelDisabled(ctx, "missing", true); err == nil {
		t.Fatal("expected error for missing channel disabled")
	}
	if err := h.SetChannelSortIndex(ctx, "missing", 1); err == nil {
		t.Fatal("expected error for missing channel sort index")
	}
	ch, ok := h.GetChannelByID(chID)
	if !ok || !ch.Disabled || ch.SortIndex != 7 {
		t.Fatalf("unexpected channel after update: %+v", ch)
	}

	adminCtx := makeLiveTVAuth("admin-1", "admin", true, true)
	doLiveTVAuthReq(t, mux, adminCtx, http.MethodPost, "/LiveTv/Timers", map[string]any{"Id": "del-tm", "Name": "T", "ChannelId": chID})
	doLiveTVAuthReq(t, mux, adminCtx, http.MethodPost, "/LiveTv/SeriesTimers", map[string]any{"Id": "del-st", "Name": "S", "ChannelId": chID})
	if !h.DeleteTimerByID(ctx, "del-tm") {
		t.Fatal("expected DeleteTimerByID true")
	}
	if !h.DeleteSeriesTimerByID(ctx, "del-st") {
		t.Fatal("expected DeleteSeriesTimerByID true")
	}
	if h.DeleteTimerByID(ctx, "nonexistent") {
		t.Fatal("expected DeleteTimerByID false for nonexistent")
	}

	req := httptest.NewRequest(http.MethodGet, "/LiveTv/Timers", nil)
	if h.HasLiveTvManagement(ctx, req) {
		t.Fatal("expected unauthenticated HasLiveTvManagement=false (fail-closed)")
	}
	if !h.HasLiveTvManagement(ctx, req.WithContext(middleware.WithAuthContext(req.Context(), adminCtx))) {
		t.Fatal("expected admin HasLiveTvManagement=true")
	}
}
