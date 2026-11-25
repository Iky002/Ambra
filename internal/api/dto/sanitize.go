package dto

// SanitizeMediaStreams clears server filesystem paths from MediaStream entries
// for non-administrator responses.
func SanitizeMediaStreams(streams []MediaStream) {
	for i := range streams {
		streams[i].Path = ""
	}
}

// SanitizeMediaSources clears server filesystem paths from MediaSourceInfo
// entries and their nested MediaStreams for non-administrator responses.
func SanitizeMediaSources(sources []MediaSourceInfo) {
	for i := range sources {
		sources[i].Path = ""
		SanitizeMediaStreams(sources[i].MediaStreams)
	}
}

// SanitizeItemPaths strips server-local filesystem paths from a BaseItemDto
// (including nested MediaSources and MediaStreams) for non-administrator callers.
func SanitizeItemPaths(item *BaseItemDto) {
	if item == nil {
		return
	}
	item.Path = ""
	SanitizeMediaSources(item.MediaSources)
	SanitizeMediaStreams(item.MediaStreams)
}

// SanitizeItemPathsSlice strips server-local filesystem paths from a slice of BaseItemDto.
func SanitizeItemPathsSlice(items []BaseItemDto) {
	for i := range items {
		SanitizeItemPaths(&items[i])
	}
}

// SanitizePlaybackInfo strips server-local filesystem paths from a PlaybackInfoResponse.
func SanitizePlaybackInfo(resp *PlaybackInfoResponse) {
	if resp == nil {
		return
	}
	SanitizeMediaSources(resp.MediaSources)
}
