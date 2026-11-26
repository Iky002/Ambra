package middleware

import (
	"net/http"
	"net/url"
	"strings"
)

// canonicalSegments maps lowercase Emby static route segments (generated from all 582 Emby 4.10.2.0 routes)
// to their canonical casing so static paths match case-insensitively across chi and ServeMux.
var canonicalSegments = map[string]string{
	"me": "Me",
	"trakt": "Trakt",
	"access": "Access",
	"activeencodings": "ActiveEncodings",
	"activitylog": "ActivityLog",
	"add": "Add",
	"added": "Added",
	"additionalfiles": "AdditionalFiles",
	"additionalparts": "AdditionalParts",
	"addtoplaylistinfo": "AddToPlaylistInfo",
	"admin": "Admin",
	"albumartists": "AlbumArtists",
	"albums": "Albums",
	"alternatesources": "AlternateSources",
	"ancestors": "Ancestors",
	"apply": "Apply",
	"appstore": "Appstore",
	"artists": "Artists",
	"attachments": "Attachments",
	"audio": "Audio",
	"audiobooks": "AudioBooks",
	"audiocodecs": "AudioCodecs",
	"audiolayouts": "AudioLayouts",
	"auth": "Auth",
	"authenticate": "Authenticate",
	"authenticatebyname": "AuthenticateByName",
	"authenticatewithquickconnect": "AuthenticateWithQuickConnect",
	"authorize": "Authorize",
	"available": "Available",
	"availableoptions": "AvailableOptions",
	"availablerecordingoptions": "AvailableRecordingOptions",
	"backup": "Backup",
	"backupinfo": "BackupInfo",
	"backuprestore": "BackupRestore",
	"bitratetest": "BitrateTest",
	"book": "Book",
	"boxset": "BoxSet",
	"branding": "Branding",
	"camerauploads": "CameraUploads",
	"cancel": "Cancel",
	"capabilities": "Capabilities",
	"channelmappingoptions": "ChannelMappingOptions",
	"channelmappings": "ChannelMappings",
	"channels": "Channels",
	"channeltags": "ChannelTags",
	"close": "Close",
	"codecconfiguration": "CodecConfiguration",
	"codecinformation": "CodecInformation",
	"codecparameters": "CodecParameters",
	"collections": "Collections",
	"command": "Command",
	"complete": "Complete",
	"configuration": "Configuration",
	"configurationpage": "ConfigurationPage",
	"configurationpages": "ConfigurationPages",
	"configured": "Configured",
	"connect": "Connect",
	"connectionmanager": "connectionmanager",
	"connectionmanager.xml": "connectionmanager.xml",
	"containers": "Containers",
	"contentdirectory": "contentdirectory",
	"contentdirectory.xml": "contentdirectory.xml",
	"control": "control",
	"copydata": "CopyData",
	"copydataoptions": "CopyDataOptions",
	"countries": "Countries",
	"counts": "Counts",
	"credits": "Credits",
	"criticreviews": "CriticReviews",
	"css": "Css",
	"css.css": "Css.css",
	"cultures": "Cultures",
	"data": "Data",
	"default": "Default",
	"defaultdirectorybrowser": "DefaultDirectoryBrowser",
	"defaults": "Defaults",
	"delete": "Delete",
	"deleteinfo": "DeleteInfo",
	"description": "description",
	"description.xml": "description.xml",
	"devices": "Devices",
	"directorycontents": "DirectoryContents",
	"disabled": "Disabled",
	"discover": "Discover",
	"discvover": "Discvover",
	"displaypreferences": "DisplayPreferences",
	"dlna": "Dlna",
	"download": "Download",
	"drives": "Drives",
	"embywebsocket": "embywebsocket",
	"enable": "Enable",
	"enabled": "Enabled",
	"encoding": "Encoding",
	"endpoint": "Endpoint",
	"entries": "Entries",
	"environment": "Environment",
	"epg": "EPG",
	"episodes": "Episodes",
	"events": "events",
	"exchange": "Exchange",
	"extendedvideotypes": "ExtendedVideoTypes",
	"externalidinfos": "ExternalIdInfos",
	"favicon.ico": "favicon.ico",
	"favoriteitems": "FavoriteItems",
	"features": "Features",
	"ffmpegoptions": "FfmpegOptions",
	"file": "File",
	"filters": "Filters",
	"filters2": "Filters2",
	"folder": "Folder",
	"folders": "Folders",
	"forgotpassword": "ForgotPassword",
	"full": "Full",
	"fulltonemapoptions": "FullToneMapOptions",
	"game": "Game",
	"gamegenres": "GameGenres",
	"games": "Games",
	"genres": "Genres",
	"groupingoptions": "GroupingOptions",
	"groups": "Groups",
	"guideinfo": "GuideInfo",
	"hidefromresume": "HideFromResume",
	"hints": "Hints",
	"hls": "hls",
	"hls1": "hls1",
	"homesections": "HomeSections",
	"icons": "icons",
	"image": "Image",
	"images": "Images",
	"index": "Index",
	"index.bif": "index.bif",
	"info": "Info",
	"initiate": "Initiate",
	"installed": "Installed",
	"installing": "Installing",
	"instantmix": "InstantMix",
	"intros": "Intros",
	"itemaccess": "ItemAccess",
	"items": "Items",
	"itemtypes": "ItemTypes",
	"jobitems": "JobItems",
	"jobs": "Jobs",
	"join": "Join",
	"keys": "Keys",
	"latest": "Latest",
	"leave": "Leave",
	"libraries": "Libraries",
	"library": "Library",
	"libraryoptions": "LibraryOptions",
	"lines": "Lines",
	"lineups": "Lineups",
	"link": "Link",
	"listingproviders": "ListingProviders",
	"live.m3u8": "live.m3u8",
	"live_local.m3u8": "live_local.m3u8",
	"live_subtitles.m3u8": "live_subtitles.m3u8",
	"liverecordings": "LiveRecordings",
	"livestreamfiles": "LiveStreamFiles",
	"livestreams": "LiveStreams",
	"livetv": "LiveTv",
	"localization": "Localization",
	"localtrailers": "LocalTrailers",
	"log": "Log",
	"logout": "Logout",
	"logs": "Logs",
	"lyrics": "Lyrics",
	"main.m3u8": "main.m3u8",
	"makeprivate": "MakePrivate",
	"makepublic": "MakePublic",
	"manage": "Manage",
	"markforremoval": "MarkForRemoval",
	"master.m3u8": "master.m3u8",
	"master_local.m3u8": "master_local.m3u8",
	"media": "Media",
	"mediafolders": "MediaFolders",
	"mediainfo": "MediaInfo",
	"mergeversions": "MergeVersions",
	"message": "Message",
	"messages": "Messages",
	"metadata": "Metadata",
	"metadataeditor": "MetadataEditor",
	"missing": "Missing",
	"move": "Move",
	"movie": "Movie",
	"movies": "Movies",
	"musicalbum": "MusicAlbum",
	"musicartist": "MusicArtist",
	"musicgenres": "MusicGenres",
	"musicvideo": "MusicVideo",
	"name": "Name",
	"networkdevices": "NetworkDevices",
	"networkshares": "NetworkShares",
	"new": "New",
	"nextup": "NextUp",
	"notifications": "Notifications",
	"officialratings": "OfficialRatings",
	"offlineactions": "OfflineActions",
	"open": "Open",
	"openapi": "openapi",
	"openapi.json": "openapi.json",
	"options": "Options",
	"packages": "Packages",
	"parentalratings": "ParentalRatings",
	"parentpath": "ParentPath",
	"partial": "Partial",
	"parties": "Parties",
	"password": "Password",
	"paths": "Paths",
	"pending": "Pending",
	"person": "Person",
	"persons": "Persons",
	"physicalpaths": "PhysicalPaths",
	"pin": "Pin",
	"ping": "Ping",
	"playback": "Playback",
	"playbackinfo": "PlaybackInfo",
	"playeditems": "PlayedItems",
	"playing": "Playing",
	"playingitems": "PlayingItems",
	"playlists": "Playlists",
	"playqueue": "PlayQueue",
	"plugins": "Plugins",
	"policy": "Policy",
	"prefixes": "Prefixes",
	"profileinfos": "ProfileInfos",
	"profiles": "Profiles",
	"programs": "Programs",
	"progress": "Progress",
	"provideritems": "ProviderItems",
	"providers": "Providers",
	"public": "Public",
	"publictonemapoptions": "PublicToneMapOptions",
	"query": "Query",
	"quickconnect": "QuickConnect",
	"rankings": "Rankings",
	"rating": "Rating",
	"read": "Read",
	"ready": "Ready",
	"recentlysearched": "RecentlySearched",
	"recommendations": "Recommendations",
	"recommended": "Recommended",
	"recordings": "Recordings",
	"refresh": "Refresh",
	"register": "Register",
	"registrationrecords": "RegistrationRecords",
	"registrations": "Registrations",
	"releasenotes": "ReleaseNotes",
	"remote": "Remote",
	"remoteaccess": "RemoteAccess",
	"remoteimages": "RemoteImages",
	"remotesearch": "RemoteSearch",
	"reset": "Reset",
	"restart": "Restart",
	"restore": "Restore",
	"restoredata": "RestoreData",
	"resume": "Resume",
	"robots.txt": "robots.txt",
	"root": "Root",
	"running": "Running",
	"sample": "Sample",
	"scheduledtasks": "ScheduledTasks",
	"search": "Search",
	"searcheditems": "SearchedItems",
	"seasons": "Seasons",
	"sections": "Sections",
	"securityinfo": "SecurityInfo",
	"selectablemediafolders": "SelectableMediaFolders",
	"series": "Series",
	"seriestimers": "SeriesTimers",
	"services": "Services",
	"sessions": "Sessions",
	"shared": "Shared",
	"shows": "Shows",
	"shutdown": "Shutdown",
	"similar": "Similar",
	"socket": "socket",
	"songs": "Songs",
	"sortindex": "SortIndex",
	"specialfeatures": "SpecialFeatures",
	"startup": "Startup",
	"state": "State",
	"status": "Status",
	"stopped": "Stopped",
	"streamlanguages": "StreamLanguages",
	"strings": "strings",
	"stringset": "stringset",
	"studios": "Studios",
	"subtitlecodecs": "SubtitleCodecs",
	"subtitleoptions": "SubtitleOptions",
	"subtitles": "Subtitles",
	"subtitles.m3u8": "subtitles.m3u8",
	"suggestions": "Suggestions",
	"summary": "Summary",
	"swagger": "swagger",
	"swagger.json": "swagger.json",
	"sync": "Sync",
	"system": "System",
	"tags": "Tags",
	"targets": "Targets",
	"test": "Test",
	"thememedia": "ThemeMedia",
	"themesongs": "ThemeSongs",
	"themevideos": "ThemeVideos",
	"thumb": "Thumb",
	"thumbnails": "thumbnails",
	"thumbnailset": "ThumbnailSet",
	"timers": "Timers",
	"tokens": "Tokens",
	"tonemapoptions": "ToneMapOptions",
	"trackselections": "TrackSelections",
	"trailer": "Trailer",
	"trailers": "Trailers",
	"transferred": "Transferred",
	"triggers": "Triggers",
	"tunerhosts": "TunerHosts",
	"tuners": "Tuners",
	"typedsettings": "TypedSettings",
	"types": "Types",
	"ui": "UI",
	"universal": "universal",
	"unmarkforremoval": "UnmarkForRemoval",
	"unread": "Unread",
	"upcoming": "Upcoming",
	"update": "Update",
	"updated": "Updated",
	"updates": "Updates",
	"url": "Url",
	"user": "User",
	"userdata": "UserData",
	"users": "Users",
	"usersettings": "UserSettings",
	"validatepath": "ValidatePath",
	"versions": "Versions",
	"video": "Video",
	"videocodecs": "VideoCodecs",
	"videos": "Videos",
	"view": "View",
	"viewing": "Viewing",
	"views": "Views",
	"virtualfolders": "VirtualFolders",
	"wakeonlaninfo": "WakeOnLanInfo",
	"webhooks": "Webhooks",
	"years": "Years",
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
		origRawPath := r.URL.RawPath
		hasEscapedSlash := strings.Contains(strings.ToLower(origRawPath), "%2f")

		// Normalize /WEB (any casing) to /web and pass through without further segment rewriting.
		if lower == "/web" || strings.HasPrefix(lower, "/web/") {
			newPath := p
			if lower == "/web" {
				newPath = "/web"
			} else {
				newPath = "/web/" + p[len("/web/"):]
			}
			if newPath == p {
				next.ServeHTTP(w, r)
				return
			}
			r2 := r.Clone(r.Context())
			r2.URL.Path = newPath
			if hasEscapedSlash {
				rawLower := strings.ToLower(origRawPath)
				if rawLower == "/web" {
					r2.URL.RawPath = "/web"
				} else if strings.HasPrefix(rawLower, "/web/") {
					r2.URL.RawPath = "/web/" + origRawPath[len("/web/"):]
				}
			} else if r2.URL.RawPath != "" {
				r2.URL.RawPath = ""
			}
			next.ServeHTTP(w, r2)
			return
		}

		// Preserve Ambra native /api/ routes verbatim.
		if strings.HasPrefix(lower, "/api/") {
			next.ServeHTTP(w, r)
			return
		}

		// Strip /emby prefix (but do NOT strip /embywebsocket).
		rawP := origRawPath
		if lower == "/emby" {
			p = "/"
			if hasEscapedSlash {
				rawP = "/"
			}
		} else if strings.HasPrefix(lower, "/emby/") {
			p = p[len("/emby"):]
			if hasEscapedSlash && strings.HasPrefix(strings.ToLower(rawP), "/emby/") {
				rawP = rawP[len("/emby"):]
			}
		}

		// Re-check /web and /api/ exemptions after stripping /emby prefix so /emby/web/... and /emby/api/... are not canonicalized.
		lowerAfterStrip := strings.ToLower(p)
		if lowerAfterStrip == "/web" {
			p = "/web"
			if hasEscapedSlash {
				rawP = "/web"
			}
		} else if strings.HasPrefix(lowerAfterStrip, "/web/") {
			p = "/web/" + p[len("/web/"):]
			if hasEscapedSlash && strings.HasPrefix(strings.ToLower(rawP), "/web/") {
				rawP = "/web/" + rawP[len("/web/"):]
			}
		} else if !strings.HasPrefix(lowerAfterStrip, "/api/") {
			if hasEscapedSlash && len(rawP) > 1 {
				rawP = canonicalizePathSegments(rawP)
				if unescaped, err := url.PathUnescape(rawP); err == nil {
					p = unescaped
				} else {
					p = canonicalizePathSegments(p)
				}
			} else if len(p) > 1 {
				p = canonicalizePathSegments(p)
			}
		}

		if p != r.URL.Path || (hasEscapedSlash && rawP != origRawPath) {
			r2 := r.Clone(r.Context())
			r2.URL.Path = p
			if hasEscapedSlash {
				r2.URL.RawPath = rawP
			} else if r2.URL.RawPath != "" {
				r2.URL.RawPath = ""
			}
			next.ServeHTTP(w, r2)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func isDynamicSegment(parts []string, idx int) bool {
	if idx < 2 || len(parts) <= idx {
		return false
	}
	p1 := strings.ToLower(parts[1])
	switch idx {
	case 2:
		switch p1 {
		case "genres", "studios", "persons", "displaypreferences", "gamegenres", "registrations":
			return true
		case "artists":
			seg := strings.ToLower(parts[2])
			return len(parts) > 3 || (seg != "albumartists" && seg != "prefixes" && seg != "instantmix")
		case "musicgenres":
			seg := strings.ToLower(parts[2])
			return len(parts) > 3 || seg != "instantmix"
		case "packages":
			seg := strings.ToLower(parts[2])
			return seg != "updates" && seg != "installed" && seg != "installing"
		case "plugins":
			seg := strings.ToLower(parts[2])
			return seg != "securityinfo" && seg != "registrationrecords"
		}
	case 3:
		p2 := strings.ToLower(parts[2])
		if p1 == "auth" && p2 == "keys" {
			return true
		}
		if p1 == "plugins" && p2 == "registrationrecords" {
			return true
		}
		if p1 == "packages" && (p2 == "installed" || p2 == "installing") {
			return true
		}
		if p1 == "system" && p2 == "configuration" {
			return true
		}
		if p1 == "system" && p2 == "logs" {
			seg := strings.ToLower(parts[3])
			return len(parts) > 4 || (seg != "query" && seg != "log")
		}
	case 4:
		if p1 == "users" && strings.ToLower(parts[3]) == "typedsettings" {
			return true
		}
	}
	return false
}

func canonicalizePathSegments(p string) string {
	parts := strings.Split(p, "/")
	changed := false
	for i := 1; i < len(parts); i++ {
		if parts[i] == "" || isDynamicSegment(parts, i) {
			continue
		}
		if canon, ok := canonicalSegments[strings.ToLower(parts[i])]; ok && canon != parts[i] {
			parts[i] = canon
			changed = true
		}
	}
	if !changed {
		return p
	}
	return strings.Join(parts, "/")
}
