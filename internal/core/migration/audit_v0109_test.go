package migration

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestAuditV0109_L29_TVDBProviderMatchAndAsyncWait(t *testing.T) {
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "mig.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	libRepo := sqlite.NewLibraryRepo(store)
	ctx := context.Background()
	lib, err := libRepo.CreateLibrary(ctx, "TV", "tvshows", []string{"/media"})
	if err != nil {
		t.Fatal(err)
	}

	series := &sqlite.ItemRecord{
		ID:        "series-tvdb-1",
		LibraryID: lib.ID,
		Type:      "Series",
		Name:      "Local Name Differs",
		Extra: sqlite.ItemExtra{
			ProviderIDs: map[string]string{"Tvdb": "81189"},
		},
	}
	if err := libRepo.UpsertItem(ctx, series); err != nil {
		t.Fatal(err)
	}

	m := NewMigrator(store, nil)
	idx, err := m.loadLocalIndex(ctx)
	if err != nil {
		t.Fatalf("loadLocalIndex: %v", err)
	}
	localID, conflict := idx.match(SourceItem{
		ID:          "src-1",
		Name:        "Different Source Name",
		Type:        "Series",
		ProviderIDs: map[string]string{"Tvdb": "81189"},
	})
	if conflict != "" || localID != series.ID {
		t.Fatalf("expected TVDB match %q, got id=%q conflict=%q", series.ID, localID, conflict)
	}

	// Verify Wait() blocks until StartAsync background goroutine finishes cleanly.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/System/Info":
			time.Sleep(40 * time.Millisecond)
			_, _ = w.Write([]byte(`{"ServerName":"Legacy","Version":"4.8"}`))
		case "/Users":
			_, _ = w.Write([]byte(`[]`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	mAsync := NewMigrator(store, nil, WithClientFactory(func(baseURL, apiKey string) (*SourceClient, error) {
		return NewSourceClient(baseURL, apiKey, WithHTTPClient(srv.Client()))
	}))
	if _, err := mAsync.StartAsync(MigrationRequest{
		SourceURL: srv.URL,
		APIKey:    "k",
	}); err != nil {
		t.Fatalf("StartAsync: %v", err)
	}
	mAsync.Wait()
	if st := mAsync.Status(); st.Status != StatusCompleted {
		t.Fatalf("expected Completed status after Wait(), got %+v", st)
	}
}
