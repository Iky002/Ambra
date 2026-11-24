package dto

import "time"

// QueryResult is the standard Emby paginated response wrapper.
type QueryResult[T any] struct {
	Items            []T `json:"Items"`
	TotalRecordCount int `json:"TotalRecordCount"`
}

// PublicSystemInfo matches GET /System/Info/Public.
type PublicSystemInfo struct {
	LocalAddress string `json:"LocalAddress"`
	ServerName   string `json:"ServerName"`
	Version      string `json:"Version"`
	AmbraVersion string `json:"AmbraVersion,omitempty"`
	ProductName  string `json:"ProductName"`
	Id           string `json:"Id"`
}

// SystemInfo matches GET /System/Info.
type SystemInfo struct {
	PublicSystemInfo
	OperatingSystem                 string `json:"OperatingSystem"`
	OperatingSystemDisplayName      string `json:"OperatingSystemDisplayName"`
	HasPendingRestart               bool   `json:"HasPendingRestart"`
	IsShuttingDown                  bool   `json:"IsShuttingDown"`
	SupportsLibraryMonitor          bool   `json:"SupportsLibraryMonitor"`
	WebSocketPortNumber             int    `json:"WebSocketPortNumber"`
	CanSelfRestart                  bool   `json:"CanSelfRestart"`
	CanSelfUpdate                   bool   `json:"CanSelfUpdate"`
	CanLaunchWebBrowser             bool   `json:"CanLaunchWebBrowser"`
	ProgramDataPath                 string `json:"ProgramDataPath"`
	CachePath                       string `json:"CachePath"`
	LogPath                         string `json:"LogPath"`
	InternalMetadataPath            string `json:"InternalMetadataPath"`
	TranscodingTempPath             string `json:"TranscodingTempPath"`
	HttpServerPortNumber            int    `json:"HttpServerPortNumber"`
	SupportsHttps                   bool   `json:"SupportsHttps"`
	HasUpdateAvailable              bool   `json:"HasUpdateAvailable"`
	EncoderLocationType             string `json:"EncoderLocationType"`
	SystemArchitecture              string `json:"SystemArchitecture"`
	StartupWizardCompleted          bool   `json:"StartupWizardCompleted"`
}

// UserPolicy matches Emby UserPolicy.
type UserPolicy struct {
	IsAdministrator                  bool     `json:"IsAdministrator"`
	IsHidden                         bool     `json:"IsHidden"`
	IsDisabled                       bool     `json:"IsDisabled"`
	EnableAllFolders                 bool     `json:"EnableAllFolders"`
	EnabledFolders                   []string `json:"EnabledFolders"`
	EnableMediaPlayback              bool     `json:"EnableMediaPlayback"`
	EnableAudioPlaybackTranscoding   bool     `json:"EnableAudioPlaybackTranscoding"`
	EnableVideoPlaybackTranscoding   bool     `json:"EnableVideoPlaybackTranscoding"`
	EnablePlaybackRemuxing           bool     `json:"EnablePlaybackRemuxing"`
	EnableContentDeletion            bool     `json:"EnableContentDeletion"`
	EnableContentDownloading         bool     `json:"EnableContentDownloading"`
	EnableRemoteAccess               bool     `json:"EnableRemoteAccess"`
	EnableLiveTvAccess               bool     `json:"EnableLiveTvAccess"`
	EnableLiveTvManagement           bool     `json:"EnableLiveTvManagement"`
	MaxParentalRating                *int     `json:"MaxParentalRating,omitempty"`
	BlockedTags                      []string `json:"BlockedTags"`
	AuthenticationProviderId         string   `json:"AuthenticationProviderId"`
}

// UserConfiguration matches Emby UserConfiguration.
type UserConfiguration struct {
	PlayDefaultAudioTrack      bool     `json:"PlayDefaultAudioTrack"`
	SubtitleLanguagePreference string   `json:"SubtitleLanguagePreference,omitempty"`
	AudioLanguagePreference    string   `json:"AudioLanguagePreference,omitempty"`
	DisplayMissingEpisodes     bool     `json:"DisplayMissingEpisodes"`
	SubtitleMode               string   `json:"SubtitleMode"`
	EnableLocalPassword        bool     `json:"EnableLocalPassword"`
	OrderedViews               []string `json:"OrderedViews"`
	LatestItemsExcludes        []string `json:"LatestItemsExcludes"`
	MyMediaExcludes            []string `json:"MyMediaExcludes"`
	HidePlayedInLatest         bool     `json:"HidePlayedInLatest"`
	RememberAudioSelections    bool     `json:"RememberAudioSelections"`
	RememberSubtitleSelections bool     `json:"RememberSubtitleSelections"`
	EnableNextEpisodeAutoPlay  bool     `json:"EnableNextEpisodeAutoPlay"`
}

// UserDto matches Emby UserDto.
type UserDto struct {
	Name                      string            `json:"Name"`
	ServerId                  string            `json:"ServerId"`
	Id                        string            `json:"Id"`
	PrimaryImageTag           string            `json:"PrimaryImageTag,omitempty"`
	HasPassword               bool              `json:"HasPassword"`
	HasConfiguredPassword     bool              `json:"HasConfiguredPassword"`
	HasConfiguredEasyPassword bool              `json:"HasConfiguredEasyPassword"`
	LastLoginDate             *time.Time        `json:"LastLoginDate,omitempty"`
	LastActivityDate          *time.Time        `json:"LastActivityDate,omitempty"`
	Configuration             UserConfiguration `json:"Configuration"`
	Policy                    UserPolicy        `json:"Policy"`
}

type PlayerStateInfo struct {
	PositionTicks       int64  `json:"PositionTicks"`
	CanSeek             bool   `json:"CanSeek"`
	IsPaused            bool   `json:"IsPaused"`
	IsMuted             bool   `json:"IsMuted"`
	VolumeLevel         int    `json:"VolumeLevel"`
	AudioStreamIndex    *int   `json:"AudioStreamIndex,omitempty"`
	SubtitleStreamIndex *int   `json:"SubtitleStreamIndex,omitempty"`
	MediaSourceId       string `json:"MediaSourceId,omitempty"`
	PlayMethod          string `json:"PlayMethod,omitempty"`
	RepeatMode          string `json:"RepeatMode"`
}

type SessionInfo struct {
	Id                    string           `json:"Id"`
	ServerId              string           `json:"ServerId"`
	UserId                string           `json:"UserId"`
	UserName              string           `json:"UserName"`
	Client                string           `json:"Client"`
	LastActivityDate      time.Time        `json:"LastActivityDate"`
	DeviceName            string           `json:"DeviceName"`
	DeviceId              string           `json:"DeviceId"`
	ApplicationVersion    string           `json:"ApplicationVersion"`
	SupportsRemoteControl bool             `json:"SupportsRemoteControl"`
	PlayState             PlayerStateInfo  `json:"PlayState"`
	NowPlayingItem        *BaseItemDto     `json:"NowPlayingItem,omitempty"`
	PlayableMediaTypes    []string         `json:"PlayableMediaTypes"`
	SupportedCommands     []string         `json:"SupportedCommands"`
}

type AuthenticationResult struct {
	User        UserDto     `json:"User"`
	SessionInfo SessionInfo `json:"SessionInfo"`
	AccessToken string      `json:"AccessToken"`
	ServerId    string      `json:"ServerId"`
}

type UserItemDataDto struct {
	PlaybackPositionTicks int64      `json:"PlaybackPositionTicks"`
	PlayCount             int        `json:"PlayCount"`
	IsFavorite            bool       `json:"IsFavorite"`
	Played                bool       `json:"Played"`
	UnplayedItemCount     *int       `json:"UnplayedItemCount,omitempty"`
	PlayedPercentage      *float64   `json:"PlayedPercentage,omitempty"`
	LastPlayedDate        *time.Time `json:"LastPlayedDate,omitempty"`
	Key                   string     `json:"Key,omitempty"`
}

type MediaStream struct {
	Codec                  string  `json:"Codec,omitempty"`
	Language               string  `json:"Language,omitempty"`
	Title                  string  `json:"Title,omitempty"`
	DisplayTitle           string  `json:"DisplayTitle,omitempty"`
	DisplayLanguage        string  `json:"DisplayLanguage,omitempty"`
	IsInterlaced           bool    `json:"IsInterlaced"`
	BitRate                int     `json:"BitRate,omitempty"`
	Channels               int     `json:"Channels,omitempty"`
	SampleRate             int     `json:"SampleRate,omitempty"`
	Height                 int     `json:"Height,omitempty"`
	Width                  int     `json:"Width,omitempty"`
	VideoRange             string  `json:"VideoRange,omitempty"`
	IsDefault              bool    `json:"IsDefault"`
	IsForced               bool    `json:"IsForced"`
	IsExternal             bool    `json:"IsExternal"`
	Type                   string  `json:"Type"` // Video / Audio / Subtitle
	Index                  int     `json:"Index"`
	Path                   string  `json:"Path,omitempty"`
	DeliveryMethod         string  `json:"DeliveryMethod,omitempty"`
	DeliveryUrl            string  `json:"DeliveryUrl,omitempty"`
	IsExternalUrl          bool    `json:"IsExternalUrl,omitempty"`
	SubVariant             string  `json:"SubVariant,omitempty"` // Ambra extension: chs / cht / bilingual
}

type MediaSourceInfo struct {
	Protocol               string        `json:"Protocol"`
	Id                     string        `json:"Id"`
	Path                   string        `json:"Path,omitempty"`
	Type                   string        `json:"Type"`
	Container              string        `json:"Container,omitempty"`
	Size                   int64         `json:"Size,omitempty"`
	Name                   string        `json:"Name"`
	IsRemote               bool          `json:"IsRemote"`
	RunTimeTicks           int64         `json:"RunTimeTicks,omitempty"`
	Bitrate                int           `json:"Bitrate,omitempty"`
	SupportsTranscoding    bool          `json:"SupportsTranscoding"`
	SupportsDirectStream   bool          `json:"SupportsDirectStream"`
	SupportsDirectPlay     bool          `json:"SupportsDirectPlay"`
	IsInfiniteStream       bool          `json:"IsInfiniteStream"`
	RequiresOpening        bool          `json:"RequiresOpening"`
	RequiresClosing        bool          `json:"RequiresClosing"`
	DirectStreamUrl        string        `json:"DirectStreamUrl,omitempty"`
	TranscodingUrl         string        `json:"TranscodingUrl,omitempty"`
	MediaStreams           []MediaStream `json:"MediaStreams"`
	DefaultAudioStreamIndex    *int      `json:"DefaultAudioStreamIndex,omitempty"`
	DefaultSubtitleStreamIndex *int      `json:"DefaultSubtitleStreamIndex,omitempty"`
}

type BaseItemPerson struct {
	Name            string `json:"Name"`
	Id              string `json:"Id"`
	Role            string `json:"Role,omitempty"`
	Type            string `json:"Type"`
	PrimaryImageTag string `json:"PrimaryImageTag,omitempty"`
}

type NameIdPair struct {
	Name string `json:"Name"`
	Id   string `json:"Id"`
}

type ChapterInfo struct {
	StartPositionTicks int64  `json:"StartPositionTicks"`
	Name               string `json:"Name,omitempty"`
	ImageTag           string `json:"ImageTag,omitempty"`
}

// BaseItemDto is the central Emby media representation.
type BaseItemDto struct {
	Name                    string            `json:"Name"`
	OriginalTitle           string            `json:"OriginalTitle,omitempty"`
	ServerId                string            `json:"ServerId"`
	Id                      string            `json:"Id"`
	Etag                    string            `json:"Etag,omitempty"`
	DateCreated             *time.Time        `json:"DateCreated,omitempty"`
	Container               string            `json:"Container,omitempty"`
	SortName                string            `json:"SortName,omitempty"`
	PremiereDate            *time.Time        `json:"PremiereDate,omitempty"`
	Path                    string            `json:"Path,omitempty"`
	OfficialRating          string            `json:"OfficialRating,omitempty"`
	Overview                string            `json:"Overview,omitempty"`
	Taglines                []string          `json:"Taglines,omitempty"`
	Tags                    []string          `json:"Tags,omitempty"`
	Genres                  []string          `json:"Genres,omitempty"`
	GenreItems              []NameIdPair      `json:"GenreItems,omitempty"`
	CommunityRating         *float64          `json:"CommunityRating,omitempty"`
	RunTimeTicks            *int64            `json:"RunTimeTicks,omitempty"`
	ProductionYear          *int              `json:"ProductionYear,omitempty"`
	IndexNumber             *int              `json:"IndexNumber,omitempty"`
	ParentIndexNumber       *int              `json:"ParentIndexNumber,omitempty"`
	ProviderIds             map[string]string `json:"ProviderIds,omitempty"`
	IsFolder                bool              `json:"IsFolder"`
	ParentId                string            `json:"ParentId,omitempty"`
	Type                    string            `json:"Type"`
	MediaType               string            `json:"MediaType,omitempty"`
	CollectionType          string            `json:"CollectionType,omitempty"`
	SeriesName              string            `json:"SeriesName,omitempty"`
	SeriesId                string            `json:"SeriesId,omitempty"`
	SeriesPrimaryImageTag   string            `json:"SeriesPrimaryImageTag,omitempty"`
	SeasonId                string            `json:"SeasonId,omitempty"`
	SeasonName              string            `json:"SeasonName,omitempty"`
	ParentBackdropItemId    string            `json:"ParentBackdropItemId,omitempty"`
	ParentBackdropImageTags []string          `json:"ParentBackdropImageTags,omitempty"`
	Album                   string            `json:"Album,omitempty"`
	AlbumId                 string            `json:"AlbumId,omitempty"`
	AlbumArtist             string            `json:"AlbumArtist,omitempty"`
	Artists                 []string          `json:"Artists,omitempty"`
	Studios                 []NameIdPair      `json:"Studios,omitempty"`
	People                  []BaseItemPerson  `json:"People,omitempty"`
	UserData                *UserItemDataDto  `json:"UserData,omitempty"`
	ChildCount              *int              `json:"ChildCount,omitempty"`
	RecursiveItemCount      *int              `json:"RecursiveItemCount,omitempty"`
	Chapters                []ChapterInfo     `json:"Chapters,omitempty"`
	MediaSources            []MediaSourceInfo `json:"MediaSources,omitempty"`
	MediaStreams            []MediaStream     `json:"MediaStreams,omitempty"`
	ImageTags               map[string]string `json:"ImageTags"`
	BackdropImageTags       []string          `json:"BackdropImageTags"`
	PrimaryImageAspectRatio *float64          `json:"PrimaryImageAspectRatio,omitempty"`
	LocationType            string            `json:"LocationType,omitempty"`
}

type PlaybackInfoResponse struct {
	MediaSources  []MediaSourceInfo `json:"MediaSources"`
	PlaySessionId string            `json:"PlaySessionId"`
}

type SearchHint struct {
	ItemId          string   `json:"ItemId"`
	Id              string   `json:"Id"`
	Name            string   `json:"Name"`
	MatchedTerm     string   `json:"MatchedTerm,omitempty"`
	ProductionYear  *int     `json:"ProductionYear,omitempty"`
	PrimaryImageTag string   `json:"PrimaryImageTag,omitempty"`
	Type            string   `json:"Type"`
	MediaType       string   `json:"MediaType,omitempty"`
	RunTimeTicks    *int64   `json:"RunTimeTicks,omitempty"`
	Series          string   `json:"Series,omitempty"`
	SeriesId        string   `json:"SeriesId,omitempty"`
	IndexNumber     *int     `json:"IndexNumber,omitempty"`
	ParentIndexNumber *int   `json:"ParentIndexNumber,omitempty"`
	BackdropImageTag string  `json:"BackdropImageTag,omitempty"`
	BackdropImageItemId string `json:"BackdropImageItemId,omitempty"`
}

type SearchHintResult struct {
	SearchHints      []SearchHint `json:"SearchHints"`
	TotalRecordCount int          `json:"TotalRecordCount"`
}

type RegistrationInfo struct {
	Name             string `json:"Name"`
	ExpirationDate   string `json:"ExpirationDate"`
	IsTrial          bool   `json:"IsTrial"`
	IsRegistered     bool   `json:"IsRegistered"`
}
