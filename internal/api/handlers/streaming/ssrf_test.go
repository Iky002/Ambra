package streaming

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/core/imaging"
	"github.com/ambra-media/ambra/internal/core/playback"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/plugins"
	"github.com/ambra-media/ambra/internal/plugins/strm"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestStreamingSTRMSSRFBlocking(t *testing.T) {
	// Redirector server that redirects to cloud metadata IP
	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://169.254.169.254/latest/meta-data/", http.StatusFound)
	}))
	defer redirector.Close()

	tmp := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmp, "ssrf.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	repo := sqlite.NewLibraryRepo(store)
	reg := plugins.NewRegistry()
	reg.Register(strm.New(false))
	cfg := config.Default(tmp)
	engine := playback.New(repo, reg, cfg)
	imgSvc := imaging.New(repo)

	mux := http.NewServeMux()
	h := New(repo, engine, imgSvc)
	h.RegisterRoutes(mux, AuthFuncs{})

	ctx := context.Background()
	cases := []struct {
		name string
		url  string
	}{
		{"cloud metadata ip", "http://169.254.169.254/latest/meta-data/"},
		{"ipv6 link-local", "http://[fe80::1]/secret"},
		{"unspecified 0.0.0.0", "http://0.0.0.0:8096/secret"},
		{"redirect to metadata", redirector.URL + "/movie.mp4"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			strmPath := filepath.Join(tmp, guid.New()+".strm")
			if err := os.WriteFile(strmPath, []byte(tc.url+"\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			itemID := guid.New()
			_ = repo.UpsertItem(ctx, &sqlite.ItemRecord{
				ID:        itemID,
				LibraryID: "lib1",
				Type:      "Movie",
				IsStrm:    true,
				Name:      tc.name,
				Container: "strm",
				Path:      strmPath,
			})
			_ = repo.ReplaceMediaSourcesAndStreams(ctx, itemID, []sqlite.MediaSourceRecord{{
				ID:        guid.New(),
				ItemID:    itemID,
				Name:      tc.name,
				Path:      strmPath,
				Protocol:  "Http",
				Container: "strm",
				IsRemote:  true,
			}})

			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/Videos/"+itemID+"/stream?proxy=1", nil))
			if rec.Code != http.StatusBadGateway {
				t.Fatalf("expected 502 Bad Gateway for %s (%s), got %d body=%q", tc.name, tc.url, rec.Code, rec.Body.String())
			}
		})
	}
}
