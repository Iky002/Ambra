package middleware

import (
	"net/http"
	"strings"
)

// canonicalSegments maps lowercase Emby static route segments to their canonical PascalCase form
// so that chi routes registered in Emby's standard casing match case-insensitively.
var canonicalSegments = map[string]string{
	"system":             "System",
	"info":               "Info",
	"public":             "Public",
	"ping":               "Ping",
	"configuration":      "Configuration",
	"activitylog":        "ActivityLog",
	"entries":            "Entries",
	"branding":           "Branding",
	"css":                "Css",
	"css.css":            "Css.css",
	"localization":       "Localization",
	"cultures":           "Cultures",
	"countries":          "Countries",
	"options":            "Options",
	"parentalratings":    "ParentalRatings",
	"registrations":      "Registrations",
	"features":           "Features",
	"plugins":            "Plugins",
	"scheduledtasks":     "ScheduledTasks",
	"users":              "Users",
	"authenticatebyname": "AuthenticateByName",
	"new":                "New",
	"password":           "Password",
	"policy":             "Policy",
	"auth":               "Auth",
	"keys":               "Keys",
	"sessions":           "Sessions",
	"logout":             "Logout",
	"capabilities":       "Capabilities",
	"full":               "Full",
	"playing":            "Playing",
	"progress":           "Progress",
	"stopped":            "Stopped",
	"devices":            "Devices",
	"displaypreferences": "DisplayPreferences",
	"items":              "Items",
	"views":              "Views",
	"latest":             "Latest",
	"resume":             "Resume",
	"nextup":             "NextUp",
	"similar":            "Similar",
	"genres":             "Genres",
	"studios":            "Studios",
	"persons":            "Persons",
	"artists":            "Artists",
	"search":             "Search",
	"hints":              "Hints",
	"videos":             "Videos",
	"audio":              "Audio",
	"images":             "Images",
	"playbackinfo":       "PlaybackInfo",
	"library":            "Library",
	"virtualfolders":     "VirtualFolders",
	"refresh":            "Refresh",
	"shows":              "Shows",
	"seasons":            "Seasons",
	"episodes":           "Episodes",
	"movies":             "Movies",
	"recommendations":    "Recommendations",
	"collections":        "Collections",
	"playlists":          "Playlists",
	"subtitles":          "Subtitles",
	"playeditems":        "PlayedItems",
	"favoriteitems":      "FavoriteItems",
	"userdata":           "UserData",
	"counts":             "Counts",
	"filters":            "Filters",
	"filters2":           "Filters2",
	"livetv":             "LiveTv",
	"channels":           "Channels",
	"programs":           "Programs",
	"embywebsocket":      "embywebsocket",
	"socket":             "socket",
}

// EmbyPrefixAndCase transparently strips the optional /emby prefix (preserving /web and /embywebsocket)
// and canonicalizes known static Emby route segments so static paths match case-insensitively.
func EmbyPrefixAndCase(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		if p == "" {
			next.ServeHTTP(w, r)
			return
		}

		lower := strings.ToLower(p)

		// Preserve /web static UI routes verbatim.
		if lower == "/web" || strings.HasPrefix(lower, "/web/") {
			next.ServeHTTP(w, r)
			return
		}

		// Strip /emby prefix (but do NOT strip /embywebsocket).
		if lower == "/emby" {
			p = "/"
		} else if strings.HasPrefix(lower, "/emby/") {
			p = p[len("/emby"):]
		}

		// Canonicalize static path segments while leaving dynamic IDs/parameters intact.
		if len(p) > 1 {
			parts := strings.Split(p, "/")
			changed := false
			for i := 1; i < len(parts); i++ {
				if parts[i] == "" {
					continue
				}
				if canon, ok := canonicalSegments[strings.ToLower(parts[i])]; ok && canon != parts[i] {
					parts[i] = canon
					changed = true
				}
			}
			if changed {
				p = strings.Join(parts, "/")
			}
		}

		if p != r.URL.Path {
			r2 := r.Clone(r.Context())
			r2.URL.Path = p
			if r2.URL.RawPath != "" {
				r2.URL.RawPath = ""
			}
			next.ServeHTTP(w, r2)
			return
		}

		next.ServeHTTP(w, r)
	})
}
