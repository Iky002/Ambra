package rankings

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAuditV0109_L23_TMDBBearerCheckAndErrorRedaction(t *testing.T) {
	// 1. isTMDBBearerToken requires BOTH eyJ prefix AND 2 dots (JWT).
	if isTMDBBearerToken("eyJNotAJWTWithoutDots") {
		t.Fatal("eyJ prefix without 2 dots must not be treated as JWT bearer token")
	}
	if isTMDBBearerToken("abc.def.ghi") {
		t.Fatal("non-eyJ string with 2 dots must not be treated as JWT bearer token")
	}
	if !isTMDBBearerToken("eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxIn0.sig") {
		t.Fatal("valid JWT should be recognized as bearer token")
	}

	// 2. fetchTMDBTrendingPage must redact apiKey from returned transport errors.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	closedURL := srv.URL
	srv.Close()

	f := NewFetcher(Options{HTTPClient: srv.Client()})
	secretKey := "super-secret-tmdb-v3-key-98765"
	_, err := f.fetchTMDBTrendingPage(context.Background(), closedURL, "/trending/movie/week", "movie", secretKey)
	if err == nil {
		t.Fatal("expected transport error against closed server")
	}
	if strings.Contains(err.Error(), secretKey) {
		t.Fatalf("TMDB apiKey leaked in error message: %s", err.Error())
	}
}
