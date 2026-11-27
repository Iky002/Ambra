package compat_test

import (
	"net/http"
	"strings"
	"testing"
)

func isIntentionalAnonymousReadRoute(method, tmpl string) bool {
	if method != http.MethodGet && method != http.MethodHead {
		return false
	}
	// Image GET/HEAD routes remain accessible without Authorization headers for browser <img> tags,
	// guarded by per-image unguessable ?tag= validation (returning 404 when missing/mismatched).
	if strings.Contains(tmpl, "/Images/") {
		return true
	}
	// Static localization dictionaries and client registration status probes.
	if strings.HasPrefix(tmpl, "/Localization/") || strings.HasPrefix(tmpl, "/Registrations/") {
		return true
	}
	return false
}

func isScopedSelfServiceRoute(method, tmpl string) bool {
	if method != http.MethodGet {
		return false
	}
	// Handler-scoped endpoints that restrict non-admin callers to their own user/device records,
	// or return read-only static external provider metadata descriptors.
	switch tmpl {
	case "/Users", "/Devices", "/Items/{Id}/ExternalIdInfos", "/Items/Intros":
		return true
	default:
		return false
	}
}

func isHardenedAdminOnlyRoute(method, tmpl string) bool {
	if method == http.MethodGet && tmpl == "/Providers/Subtitles/Subtitles/{Id}" {
		return true
	}
	return false
}

func TestRBACBaselineEnforcement(t *testing.T) {
	srv, cfg, adminToken, adminID := newCompatTestServer(t)
	defer srv.Close()

	create := sendCompatRequest(srv.Handler, http.MethodPost, "/Users/New", adminToken, []byte(`{"Name":"rbac-reader","Password":"rbac-test-password","IsAdministrator":false}`))
	if create.Code != http.StatusOK {
		t.Fatalf("create user failed: %d %s", create.Code, create.Body.String())
	}
	readerToken, _ := loginUser(t, srv.Handler, "rbac-reader", "rbac-test-password", "rbac-baseline-device")

	for _, rt := range loadBaselineRoutes(t) {
		for _, v := range strings.Split(rt.Verbs, ",") {
			method := strings.TrimSpace(strings.ToUpper(v))
			if method == "" {
				continue
			}
			path := materializePath(rt.Path, adminID, "nonexistent-item-id", "src-0", cfg.ServerID)

			if rt.AuthRequired && !isIntentionalAnonymousReadRoute(method, rt.Path) {
				w := sendCompatRequest(srv.Handler, method, path, "", nil)
				if w.Code != http.StatusUnauthorized {
					t.Errorf("ANON %s %s (roles=%q): expected 401, got %d (%s)", method, rt.Path, rt.Roles, w.Code, strings.TrimSpace(w.Body.String()))
				}
			}

			requiresAdmin := (strings.Contains(strings.ToLower(rt.Roles), "admin") && !isScopedSelfServiceRoute(method, rt.Path)) ||
				isHardenedAdminOnlyRoute(method, rt.Path)
			if requiresAdmin {
				w := sendCompatRequest(srv.Handler, method, path, readerToken, nil)
				if w.Code != http.StatusForbidden {
					t.Errorf("NON-ADMIN %s %s (roles=%q): expected 403, got %d (%s)", method, rt.Path, rt.Roles, w.Code, strings.TrimSpace(w.Body.String()))
				}
			}
		}
	}
}
