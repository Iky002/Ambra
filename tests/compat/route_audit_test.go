package compat_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/ambra-media/ambra/internal/api/dto"
	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/server"
)

type baselineRoute struct {
	Path         string `json:"Path"`
	Verbs        string `json:"Verbs"`
	Summary      string `json:"Summary"`
	ServiceName  string `json:"ServiceName"`
	Assembly     string `json:"Assembly"`
	IsHidden     bool   `json:"IsHidden"`
	AuthRequired bool   `json:"AuthRequired"`
	Roles        string `json:"Roles"`
}

var pathParamRe = regexp.MustCompile(`\{([^}]+)\}`)

func materializePath(tmpl, userID, itemID, mediaSourceID, serverID string) string {
	return pathParamRe.ReplaceAllStringFunc(tmpl, func(tok string) string {
		name := strings.ToLower(strings.TrimSuffix(strings.TrimPrefix(tok, "{"), "}"))
		if idx := strings.IndexByte(name, ':'); idx >= 0 {
			name = name[:idx]
		}
		switch {
		case name == "userid":
			return userID
		case name == "id" || name == "itemid":
			return itemID
		case name == "mediasourceid":
			return mediaSourceID
		case name == "uuid":
			return serverID
		case name == "type":
			return "Primary"
		case name == "index" || name == "imageindex" || name == "newindex" || name == "segmentid" || name == "segment":
			return "0"
		case name == "container":
			return "mp4"
		case name == "format":
			return "vtt"
		case name == "language":
			return "chi"
		case name == "key":
			return "branding"
		case name == "name":
			return "default"
		case name == "filename" || name == "resource":
			return "icon.svg"
		case name == "playlistid":
			return "main"
		case name == "command":
			return "Pause"
		default:
			return "test"
		}
	})
}

func TestEmby4102RouteCoverageAudit(t *testing.T) {
	raw, err := os.ReadFile("emby_4.10.2.0_routes.json")
	if err != nil {
		t.Fatal(err)
	}
	var routes []baselineRoute
	if err := json.Unmarshal(raw, &routes); err != nil {
		t.Fatal(err)
	}
	if len(routes) != 582 {
		t.Fatalf("expected 582 baseline routes, got %d", len(routes))
	}

	t.Setenv("AMBRA_ADMIN_USER", "admin")
	t.Setenv("AMBRA_ADMIN_PASS", "admin")
	tmpDir := t.TempDir()
	dataDir := filepath.Join(tmpDir, "data")
	cfg, err := config.Load(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	srv, err := server.New(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = srv.Close() }()

	// Login default admin
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/emby/Users/AuthenticateByName", strings.NewReader(`{"Username":"admin","Pw":"admin"}`))
	req.Header.Set("Content-Type", "application/json")
	srv.Handler.ServeHTTP(rec, req)
	var authRes dto.AuthenticationResult
	_ = json.NewDecoder(rec.Body).Decode(&authRes)
	token := authRes.AccessToken
	userID := authRes.User.Id

	var missing []string
	missingByPrefix := map[string][]string{}
	covered := 0
	totalChecks := 0

	for _, rt := range routes {
		verbs := strings.Split(rt.Verbs, ",")
		for _, v := range verbs {
			method := strings.TrimSpace(strings.ToUpper(v))
			if method == "" {
				continue
			}
			totalChecks++
			path := materializePath(rt.Path, userID, "nonexistent-item-id", "src-0", cfg.ServerID)
			if !strings.HasPrefix(path, "/") {
				path = "/" + path
			}

			var body *strings.Reader
			if method == http.MethodPost || method == http.MethodPut || method == http.MethodPatch {
				body = strings.NewReader(`{}`)
			} else {
				body = strings.NewReader("")
			}
			r := httptest.NewRequest(method, path, body)
			r.Header.Set("X-Emby-Token", token)
			r.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			srv.Handler.ServeHTTP(w, r)

			// Distinguish true route-not-registered (plain text "404 page not found" or 405) vs handler-returned JSON 404 (e.g. item not found)
			ct := w.Header().Get("Content-Type")
			respBody := strings.TrimSpace(w.Body.String())
			isUnregistered := w.Code == http.StatusMethodNotAllowed ||
				(w.Code == http.StatusNotFound && !strings.Contains(ct, "application/json") && (respBody == "404 page not found" || respBody == ""))

			if isUnregistered {
				entry := fmt.Sprintf("%s %s (%s)", method, rt.Path, rt.ServiceName)
				missing = append(missing, entry)
				seg := strings.Split(strings.TrimPrefix(rt.Path, "/"), "/")[0]
				missingByPrefix[seg] = append(missingByPrefix[seg], entry)
			} else {
				covered++
			}
		}
	}

	t.Logf("Route Audit: covered=%d / total=%d (%.1f%%), missing=%d", covered, totalChecks, float64(covered)*100/float64(totalChecks), len(missing))
	if os.Getenv("AMBRA_DUMP_MISSING_ROUTES") == "1" {
		prefixes := make([]string, 0, len(missingByPrefix))
		for p := range missingByPrefix {
			prefixes = append(prefixes, p)
		}
		sort.Strings(prefixes)
		var out strings.Builder
		for _, p := range prefixes {
			fmt.Fprintf(&out, "\n=== /%s (%d) ===\n", p, len(missingByPrefix[p]))
			for _, m := range missingByPrefix[p] {
				fmt.Fprintf(&out, "  %s\n", m)
			}
		}
		_ = os.WriteFile("/tmp/ambra_missing_routes.txt", []byte(out.String()), 0o644)
		t.Logf("Dumped missing routes to /tmp/ambra_missing_routes.txt")
	}
	if len(missing) > 0 {
		t.Fatalf("582-route compatibility check failed: %d missing routes (first 20: %v)", len(missing), missing[:min(20, len(missing))])
	}
}
