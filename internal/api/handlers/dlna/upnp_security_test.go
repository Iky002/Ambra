package dlna

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestDLNAV0107AuditSecurityHardening(t *testing.T) {
	h, mux, libRepo, store := setupDLNATestEnv(t)
	ctx := context.Background()

	// Seed a library with a regular movie, a private playlist, and a public playlist
	libDir := t.TempDir()
	lib, err := libRepo.CreateLibrary(ctx, "Movies", "movies", []string{libDir})
	if err != nil {
		t.Fatal(err)
	}
	mediaFile := filepath.Join(libDir, "movie.mp4")
	_ = os.WriteFile(mediaFile, []byte("MP4BYTES"), 0o644)

	movie := &sqlite.ItemRecord{
		ID:        guid.New(),
		LibraryID: lib.ID,
		ParentID:  lib.ID,
		Type:      "Movie",
		MediaType: "Video",
		Name:      "Visible Film",
		Path:      mediaFile,
	}
	_ = libRepo.UpsertItem(ctx, movie)

	privPl, err := libRepo.CreatePlaylist(ctx, "Secret Private Playlist", "Video", "user-1", []string{movie.ID})
	if err != nil {
		t.Fatal(err)
	}
	pubPl, err := libRepo.CreatePlaylist(ctx, "Shared Public Playlist", "Video", "user-1", []string{movie.ID})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = store.Writer.ExecContext(ctx,
		`INSERT INTO server_config (key, value_json, updated_at) VALUES (?, ?, ?)`,
		"item_visibility_"+guid.Normalize(pubPl.ID), `{"IsPublic":true}`, time.Now().UTC().Unix(),
	)

	sendSOAP := func(action, body string) *httptest.ResponseRecorder {
		t.Helper()
		envelope := fmt.Sprintf(`<?xml version="1.0"?><s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"><s:Body><u:%s xmlns:u="%s">%s</u:%s></s:Body></s:Envelope>`,
			action, contentDirectoryServiceType, body, action)
		req := httptest.NewRequest(http.MethodPost, "/Dlna/srv-1/contentdirectory/control", strings.NewReader(envelope))
		req.RemoteAddr = "127.0.0.1:12345"
		req.Header.Set("SOAPACTION", fmt.Sprintf(`"%s#%s"`, contentDirectoryServiceType, action))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}

	// 1. H-12: Private playlists MUST NOT appear in Search (*, dc:title, upnp:class container) or Browse
	recSearchAll := sendSOAP("Search", `<ContainerID>0</ContainerID><SearchCriteria>*</SearchCriteria><StartingIndex>0</StartingIndex><RequestedCount>0</RequestedCount>`)
	if recSearchAll.Code != http.StatusOK {
		t.Fatalf("Search * status=%d body=%s", recSearchAll.Code, recSearchAll.Body.String())
	}
	if strings.Contains(recSearchAll.Body.String(), "Secret Private Playlist") {
		t.Fatalf("private playlist leaked in Search *: %s", recSearchAll.Body.String())
	}
	if !strings.Contains(recSearchAll.Body.String(), "Visible Film") {
		t.Fatalf("expected Visible Film in Search *, got: %s", recSearchAll.Body.String())
	}

	// Search by title fallback targeting private playlist
	recSearchPriv := sendSOAP("Search", `<ContainerID>0</ContainerID><SearchCriteria>dc:title contains "Secret Private"</SearchCriteria><StartingIndex>0</StartingIndex><RequestedCount>10</RequestedCount>`)
	if strings.Contains(recSearchPriv.Body.String(), "Secret Private Playlist") {
		t.Fatalf("private playlist leaked in title Search: %s", recSearchPriv.Body.String())
	}

	// parseSearchCriteria object.container must not include Playlist
	_, containerTypes := parseSearchCriteria(`upnp:class derivedfrom "object.container"`)
	for _, ct := range containerTypes {
		if strings.EqualFold(ct, "Playlist") {
			t.Fatalf("parseSearchCriteria object.container must not include Playlist: %v", containerTypes)
		}
	}

	// Search inside private playlist ContainerID returns 0 items
	recSearchContainer := sendSOAP("Search", fmt.Sprintf(`<ContainerID>%s</ContainerID><SearchCriteria>*</SearchCriteria><StartingIndex>0</StartingIndex><RequestedCount>10</RequestedCount>`, privPl.ID))
	if recSearchContainer.Code != http.StatusOK || !strings.Contains(recSearchContainer.Body.String(), "<TotalMatches>0</TotalMatches>") {
		t.Fatalf("expected 0 matches searching private playlist container, got %d %s", recSearchContainer.Code, recSearchContainer.Body.String())
	}

	// BrowseMetadata and BrowseDirectChildren on private playlist -> SOAP fault (object not found)
	recBrowseMeta := sendSOAP("Browse", fmt.Sprintf(`<ObjectID>%s</ObjectID><BrowseFlag>BrowseMetadata</BrowseFlag><StartingIndex>0</StartingIndex><RequestedCount>10</RequestedCount>`, privPl.ID))
	if recBrowseMeta.Code == http.StatusOK {
		t.Fatalf("expected SOAP fault browsing private playlist metadata, got 200: %s", recBrowseMeta.Body.String())
	}
	recBrowseChildren := sendSOAP("Browse", fmt.Sprintf(`<ObjectID>%s</ObjectID><BrowseFlag>BrowseDirectChildren</BrowseFlag><StartingIndex>0</StartingIndex><RequestedCount>10</RequestedCount>`, privPl.ID))
	if recBrowseChildren.Code == http.StatusOK {
		t.Fatalf("expected SOAP fault browsing private playlist children, got 200: %s", recBrowseChildren.Body.String())
	}

	// BrowseMetadata on public playlist succeeds
	recBrowsePub := sendSOAP("Browse", fmt.Sprintf(`<ObjectID>%s</ObjectID><BrowseFlag>BrowseMetadata</BrowseFlag><StartingIndex>0</StartingIndex><RequestedCount>10</RequestedCount>`, pubPl.ID))
	if recBrowsePub.Code != http.StatusOK || !strings.Contains(recBrowsePub.Body.String(), "Shared Public Playlist") {
		t.Fatalf("expected public playlist metadata to succeed, got %d %s", recBrowsePub.Code, recBrowsePub.Body.String())
	}

	// 2. H-12: RequestedCount clamping (0 -> 500, >500 -> 500, valid preserved)
	if got := clampRequestedCount(0); got != maxDLNARequestedCount {
		t.Fatalf("clampRequestedCount(0) = %d, want %d", got, maxDLNARequestedCount)
	}
	if got := clampRequestedCount(-5); got != maxDLNARequestedCount {
		t.Fatalf("clampRequestedCount(-5) = %d, want %d", got, maxDLNARequestedCount)
	}
	if got := clampRequestedCount(99999); got != maxDLNARequestedCount {
		t.Fatalf("clampRequestedCount(99999) = %d, want %d", got, maxDLNARequestedCount)
	}
	if got := clampRequestedCount(25); got != 25 {
		t.Fatalf("clampRequestedCount(25) = %d, want 25", got)
	}

	// 3. H-12: handleUpdateProfile rejects "default" (400) and non-existent IDs (404), updates existing IDs (204)
	bodyUpd, _ := json.Marshal(DlnaProfile{Name: "Updated Profile"})
	recNoExist := httptest.NewRecorder()
	mux.ServeHTTP(recNoExist, httptest.NewRequest(http.MethodPost, "/Dlna/Profiles/does-not-exist", bytes.NewReader(bodyUpd)))
	if recNoExist.Code != http.StatusNotFound {
		t.Fatalf("expected 404 updating non-existent profile, got %d", recNoExist.Code)
	}
	bodyCreate, _ := json.Marshal(DlnaProfile{Id: "custom-p1", Name: "Custom P1"})
	recCreate := httptest.NewRecorder()
	mux.ServeHTTP(recCreate, httptest.NewRequest(http.MethodPost, "/Dlna/Profiles", bytes.NewReader(bodyCreate)))
	if recCreate.Code != http.StatusOK {
		t.Fatalf("create profile status=%d", recCreate.Code)
	}
	recUpdOK := httptest.NewRecorder()
	mux.ServeHTTP(recUpdOK, httptest.NewRequest(http.MethodPost, "/Dlna/Profiles/custom-p1", bytes.NewReader(bodyUpd)))
	if recUpdOK.Code != http.StatusNoContent {
		t.Fatalf("expected 204 updating existing profile, got %d", recUpdOK.Code)
	}

	// 4. H-12: baseURL trusts X-Forwarded-Proto ONLY from trusted proxy IPs
	reqUntrusted := httptest.NewRequest(http.MethodGet, "http://example.local:8096/desc", nil)
	reqUntrusted.RemoteAddr = "192.168.1.99:44444"
	reqUntrusted.Header.Set("X-Forwarded-Proto", "https")
	if got := h.baseURL(reqUntrusted); !strings.HasPrefix(got, "http://") {
		t.Fatalf("untrusted remote must not spoof https via X-Forwarded-Proto, got %q", got)
	}
	reqTrusted := httptest.NewRequest(http.MethodGet, "http://example.local:8096/desc", nil)
	reqTrusted.RemoteAddr = "127.0.0.1:44444"
	reqTrusted.Header.Set("X-Forwarded-Proto", "https")
	if got := h.baseURL(reqTrusted); !strings.HasPrefix(got, "https://") {
		t.Fatalf("trusted proxy loopback should honor X-Forwarded-Proto=https, got %q", got)
	}
	reqTLS := httptest.NewRequest(http.MethodGet, "https://example.local:8096/desc", nil)
	reqTLS.TLS = &tls.ConnectionState{}
	if got := h.baseURL(reqTLS); !strings.HasPrefix(got, "https://") {
		t.Fatalf("direct TLS should return https, got %q", got)
	}

	// 5. H-12: handleDLNAStream redacts absolute paths on 500 error (including os.ErrPermission)
	if os.Geteuid() != 0 {
		noPermFile := filepath.Join(libDir, "secret_no_perm_file.mp4")
		_ = os.WriteFile(noPermFile, []byte("SECRET"), 0o000)
		defer func() { _ = os.Chmod(noPermFile, 0o600) }()
		noPermItem := &sqlite.ItemRecord{
			ID:        guid.New(),
			LibraryID: lib.ID,
			ParentID:  lib.ID,
			Type:      "Movie",
			MediaType: "Video",
			Name:      "NoPerm",
			Path:      noPermFile,
		}
		_ = libRepo.UpsertItem(ctx, noPermItem)
		exp := time.Now().UTC().Add(time.Hour).Unix()
		sig := h.signStreamToken(noPermItem.ID, exp)
		reqStream := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/Dlna/srv-1/stream/%s?exp=%d&sig=%s", noPermItem.ID, exp, sig), nil)
		reqStream.RemoteAddr = "127.0.0.1:12345"
		recStream := httptest.NewRecorder()
		mux.ServeHTTP(recStream, reqStream)
		if recStream.Code != http.StatusInternalServerError {
			t.Fatalf("expected 500 for unreadable file, got %d", recStream.Code)
		}
		if strings.Contains(recStream.Body.String(), noPermFile) || strings.Contains(recStream.Body.String(), "secret_no_perm_file") {
			t.Fatalf("500 response must not expose absolute file path: %s", recStream.Body.String())
		}
	}

	// Private playlist stream returns 404 even with valid signature
	exp := time.Now().UTC().Add(time.Hour).Unix()
	sigPriv := h.signStreamToken(privPl.ID, exp)
	reqPrivStream := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/Dlna/srv-1/stream/%s?exp=%d&sig=%s", privPl.ID, exp, sigPriv), nil)
	reqPrivStream.RemoteAddr = "127.0.0.1:12345"
	recPrivStream := httptest.NewRecorder()
	mux.ServeHTTP(recPrivStream, reqPrivStream)
	if recPrivStream.Code != http.StatusNotFound {
		t.Fatalf("expected 404 streaming private playlist, got %d", recPrivStream.Code)
	}
}
