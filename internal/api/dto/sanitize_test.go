package dto

import "testing"

func TestSanitizeItemPaths(t *testing.T) {
	SanitizeItemPaths(nil)
	SanitizePlaybackInfo(nil)

	item := BaseItemDto{
		Name: "Movie",
		Path: "/secret/media/movie.mkv",
		MediaStreams: []MediaStream{
			{Type: "Subtitle", Path: "/secret/media/movie.srt"},
		},
		MediaSources: []MediaSourceInfo{
			{
				Id:   "src1",
				Path: "/secret/media/movie.mkv",
				MediaStreams: []MediaStream{
					{Type: "Subtitle", Path: "/secret/media/movie.ass"},
				},
			},
		},
	}
	items := []BaseItemDto{item}
	SanitizeItemPathsSlice(items)

	if items[0].Path != "" {
		t.Fatalf("expected item.Path cleared, got %q", items[0].Path)
	}
	if items[0].MediaStreams[0].Path != "" {
		t.Fatalf("expected item.MediaStreams[0].Path cleared, got %q", items[0].MediaStreams[0].Path)
	}
	if items[0].MediaSources[0].Path != "" {
		t.Fatalf("expected MediaSources[0].Path cleared, got %q", items[0].MediaSources[0].Path)
	}
	if items[0].MediaSources[0].MediaStreams[0].Path != "" {
		t.Fatalf("expected MediaSources[0].MediaStreams[0].Path cleared, got %q", items[0].MediaSources[0].MediaStreams[0].Path)
	}

	pi := &PlaybackInfoResponse{
		MediaSources: []MediaSourceInfo{
			{Path: "/secret/a.mkv", MediaStreams: []MediaStream{{Path: "/secret/a.srt"}}},
		},
	}
	SanitizePlaybackInfo(pi)
	if pi.MediaSources[0].Path != "" || pi.MediaSources[0].MediaStreams[0].Path != "" {
		t.Fatalf("expected PlaybackInfo paths cleared: %+v", pi)
	}
}
