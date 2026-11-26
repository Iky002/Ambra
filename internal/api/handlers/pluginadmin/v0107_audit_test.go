package pluginadmin

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"

	"github.com/ambra-media/ambra/internal/plugins"
	"github.com/ambra-media/ambra/internal/plugins/tmdb"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestPluginAdminV0107AuditConcurrentConfigAndDeterministicMerge(t *testing.T) {
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "pluginadmin_v107.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	reg := plugins.NewRegistry()
	p := tmdb.New("", "zh-CN")
	reg.Register(p)
	h := New(reg, store)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux, AuthFuncs{})

	// 1. Concurrent GET and POST /Plugins/{Id}/Configuration has no race under cfgMu
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/Plugins/"+p.Manifest().ID+"/Configuration", nil))
			if rec.Code != http.StatusOK {
				t.Errorf("concurrent GET config failed: %d", rec.Code)
			}
		}()
		go func() {
			defer wg.Done()
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/Plugins/"+p.Manifest().ID+"/Configuration", bytes.NewReader([]byte(`{"api_key":"secret123","timeout":10}`))))
			if rec.Code != http.StatusOK {
				t.Errorf("concurrent POST config failed: %d", rec.Code)
			}
		}()
	}
	wg.Wait()

	// 2. Deterministic key ordering in mergePluginConfigInternal when colliding aliases appear in incoming map
	prev := map[string]any{}
	incoming := map[string]any{
		"apiKey":  "from-camel",
		"api_key": "from-snake",
	}
	first := mergePluginConfig(prev, incoming)
	for i := 0; i < 20; i++ {
		again := mergePluginConfig(prev, incoming)
		if len(again) != len(first) || again["apiKey"] != first["apiKey"] {
			t.Fatalf("non-deterministic mergePluginConfig: first=%+v again=%+v", first, again)
		}
	}
}
