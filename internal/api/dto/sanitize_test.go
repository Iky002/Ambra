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
			{
				Protocol: "Http",
				IsRemote: true,
				Path:     "https://cdn.example.com/direct/movie.mkv",
				MediaStreams: []MediaStream{
					{Path: "/secret/local.srt"},
				},
			},
			{
				Protocol: "File",
				IsRemote: true,
				Path:     "/secret/local.strm",
			},
		},
	}
	SanitizePlaybackInfo(pi)
	if pi.MediaSources[0].Path != "" || pi.MediaSources[0].MediaStreams[0].Path != "" {
		t.Fatalf("expected local PlaybackInfo paths cleared: %+v", pi.MediaSources[0])
	}
	if pi.MediaSources[1].Path != "https://cdn.example.com/direct/movie.mkv" {
		t.Fatalf("expected remote HTTP STRM Path preserved, got %q", pi.MediaSources[1].Path)
	}
	if pi.MediaSources[1].MediaStreams[0].Path != "" {
		t.Fatalf("expected local MediaStream path cleared even on remote source, got %q", pi.MediaSources[1].MediaStreams[0].Path)
	}
	if pi.MediaSources[2].Path != "" {
		t.Fatalf("expected local .strm filesystem path cleared, got %q", pi.MediaSources[2].Path)
	}
}
