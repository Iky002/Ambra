package audio

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestAudioV0104ProxySecurityHeadersAndMIMECleanup(t *testing.T) {
	tmp := t.TempDir()
	st, err := sqlite.Open(filepath.Join(tmp, "audio_v0104.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/evil.html":
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte("<html>xss</html>"))
		case "/evil.svg":
			w.Header().Set("Content-Type", "image/svg+xml")
			_, _ = w.Write([]byte("<svg/>"))
		default:
			w.Header().Set("Content-Type", "audio/mpeg")
			_, _ = w.Write([]byte("MP3_BYTES"))
		}
	}))
	defer upstream.Close()

	ctx := context.Background()
	repo := sqlite.NewLibraryRepo(st)
	strmPath := filepath.Join(tmp, "track.strm")
	_ = os.WriteFile(strmPath, []byte("http://93.184.216.34/a.mp3\n"), 0o644)
	itemID := guid.New()
	_ = repo.UpsertItem(ctx, &sqlite.ItemRecord{
		ID: itemID, LibraryID: "lib1", Type: "Audio", MediaType: "Audio", Name: "Remote", Path: strmPath,
	})

	h := New(repo, config.Default(tmp))
	h.SetProxyClient(upstream.Client())
	mux := http.NewServeMux()
	h.RegisterRoutes(mux, AuthFuncs{})

	for _, tc := range []struct {
		path   string
		wantCT string
	}{
		{"/evil.html", "application/octet-stream"},
		{"/evil.svg", "application/octet-stream"},
		{"/good.mp3", "audio/mpeg"},
	} {
		h.SetStreamResolver(func(_ context.Context, _ string) (string, error) {
			return upstream.URL + tc.path, nil
		})
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/Audio/"+itemID+"/stream", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("audio proxy %s status=%d", tc.path, rec.Code)
		}
		if rec.Header().Get("X-Content-Type-Options") != "nosniff" {
			t.Fatalf("audio proxy %s missing nosniff", tc.path)
		}
		if rec.Header().Get("Content-Security-Policy") != "sandbox" {
			t.Fatalf("audio proxy %s missing sandbox CSP", tc.path)
		}
		if ct := rec.Header().Get("Content-Type"); ct != tc.wantCT {
			t.Fatalf("audio proxy %s Content-Type = %q, want %q", tc.path, ct, tc.wantCT)
		}
	}
}
