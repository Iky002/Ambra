package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
)

func TestEmbyPrefixDynamicSegmentsV0109(t *testing.T) {
	r := chi.NewRouter()
	r.Use(EmbyPrefixAndCase)

	// Artists (static + dynamic)
	r.Get("/Artists/AlbumArtists", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("static:AlbumArtists")) })
	r.Get("/Artists/Prefixes", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("static:Prefixes")) })
	r.Get("/Artists/InstantMix", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("static:InstantMix")) })
	r.Get("/Artists/{Name}", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("artist:" + chi.URLParam(r, "Name"))) })
	r.Get("/Artists/{Name}/Images/{Type}", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("artistimg:" + chi.URLParam(r, "Name") + ":" + chi.URLParam(r, "Type")))
	})

	// MusicGenres & GameGenres
	r.Get("/MusicGenres/InstantMix", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("static:MGInstantMix")) })
	r.Get("/MusicGenres/{Name}", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("mg:" + chi.URLParam(r, "Name"))) })
	r.Get("/MusicGenres/{Name}/InstantMix", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("mgmix:" + chi.URLParam(r, "Name"))) })
	r.Get("/GameGenres/{Name}", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("gg:" + chi.URLParam(r, "Name"))) })

	// Packages & Registrations
	r.Get("/Packages/Updates", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("static:PkgUpdates")) })
	r.Get("/Packages/{Name}", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("pkg:" + chi.URLParam(r, "Name"))) })
	r.Post("/Packages/Installed/{Name}", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("pkginst:" + chi.URLParam(r, "Name"))) })
	r.Post("/Packages/Installing/{Id}/Delete", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("pkgdel:" + chi.URLParam(r, "Id"))) })
	r.Get("/Registrations/{Name}", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("reg:" + chi.URLParam(r, "Name"))) })

	// System/Configuration & System/Logs
	r.Get("/System/Configuration/{Key}", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("cfg:" + chi.URLParam(r, "Key"))) })
	r.Get("/System/Logs/Query", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("static:LogsQuery")) })
	r.Get("/System/Logs/Log", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("static:LogsLog:" + r.URL.Query().Get("name"))) })
	r.Get("/System/Logs/{Name}", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("log:" + chi.URLParam(r, "Name"))) })
	r.Get("/System/Logs/{Name}/Lines", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("loglines:" + chi.URLParam(r, "Name"))) })

	// Users/{UserId}/TypedSettings/{Key}
	r.Get("/Users/{UserId}/TypedSettings/{Key}", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("typed:" + chi.URLParam(r, "UserId") + ":" + chi.URLParam(r, "Key")))
	})

	cases := []struct {
		method string
		path   string
		want   string
	}{
		{http.MethodGet, "/emby/artists/albumartists", "static:AlbumArtists"},
		{http.MethodGet, "/emby/artists/prefixes", "static:Prefixes"},
		{http.MethodGet, "/emby/artists/instantmix", "static:InstantMix"},
		{http.MethodGet, "/emby/artists/movies", "artist:movies"},
		{http.MethodGet, "/emby/artists/prefixes/images/primary", "artistimg:prefixes:primary"},
		{http.MethodGet, "/emby/musicgenres/instantmix", "static:MGInstantMix"},
		{http.MethodGet, "/emby/musicgenres/movies", "mg:movies"},
		{http.MethodGet, "/emby/musicgenres/movies/instantmix", "mgmix:movies"},
		{http.MethodGet, "/emby/gamegenres/search", "gg:search"},
		{http.MethodGet, "/emby/packages/updates", "static:PkgUpdates"},
		{http.MethodGet, "/emby/packages/movies", "pkg:movies"},
		{http.MethodPost, "/emby/packages/installed/movies", "pkginst:movies"},
		{http.MethodPost, "/emby/packages/installing/users/delete", "pkgdel:users"},
		{http.MethodGet, "/emby/registrations/movies", "reg:movies"},
		{http.MethodGet, "/emby/system/configuration/branding", "cfg:branding"},
		{http.MethodGet, "/emby/system/logs/query", "static:LogsQuery"},
		{http.MethodGet, "/emby/system/logs/log?name=ambra.log", "static:LogsLog:ambra.log"},
		{http.MethodGet, "/emby/system/logs/info", "log:info"},
		{http.MethodGet, "/emby/system/logs/ambra-2026.log", "log:ambra-2026.log"},
		{http.MethodGet, "/emby/system/logs/log/lines", "loglines:log"},
		{http.MethodGet, "/emby/users/u1/typedsettings/branding", "typed:u1:branding"},
	}

	for _, tc := range cases {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(tc.method, tc.path, nil))
		if rec.Code != http.StatusOK || rec.Body.String() != tc.want {
			t.Errorf("%s %s: got status=%d body=%q, want 200 %q", tc.method, tc.path, rec.Code, rec.Body.String(), tc.want)
		}
	}
}
