package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
)

func TestEmbyPrefixPreservesDynamicSegmentCase(t *testing.T) {
	r := chi.NewRouter()
	r.Use(EmbyPrefixAndCase)

	r.Get("/Plugins/SecurityInfo", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("static:SecurityInfo"))
	})
	r.Get("/Plugins/RegistrationRecords/{Name}", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("reg:" + chi.URLParam(r, "Name")))
	})
	r.Get("/Plugins/{Id}/Configuration", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("plugin:" + chi.URLParam(r, "Id")))
	})
	r.Post("/Auth/Keys/{Key}/Delete", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("key:" + chi.URLParam(r, "Key")))
	})
	r.Get("/Genres/{Name}", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("genre:" + chi.URLParam(r, "Name")))
	})
	r.Get("/Studios/{Name}", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("studio:" + chi.URLParam(r, "Name")))
	})
	r.Get("/Persons/{Name}", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("person:" + chi.URLParam(r, "Name")))
	})
	r.Get("/DisplayPreferences/{Id}", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("dp:" + chi.URLParam(r, "Id")))
	})

	cases := []struct {
		method string
		path   string
		want   string
	}{
		// Static /Plugins subroutes still canonicalize
		{http.MethodGet, "/emby/plugins/securityinfo", "static:SecurityInfo"},
		{http.MethodGet, "/emby/plugins/registrationrecords/movies", "reg:movies"},
		// Dynamic segments whose lowercase matches canonicalSegments must preserve original casing
		{http.MethodGet, "/emby/plugins/movies/configuration", "plugin:movies"},
		{http.MethodGet, "/emby/plugins/search/configuration", "plugin:search"},
		{http.MethodPost, "/emby/auth/keys/users/delete", "key:users"},
		{http.MethodGet, "/emby/genres/movies", "genre:movies"},
		{http.MethodGet, "/emby/studios/search", "studio:search"},
		{http.MethodGet, "/emby/persons/admin", "person:admin"},
		{http.MethodGet, "/emby/displaypreferences/usersettings", "dp:usersettings"},
	}

	for _, tc := range cases {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(tc.method, tc.path, nil))
		if rec.Code != http.StatusOK || rec.Body.String() != tc.want {
			t.Errorf("%s %s: got status=%d body=%q, want 200 %q", tc.method, tc.path, rec.Code, rec.Body.String(), tc.want)
		}
	}
}
