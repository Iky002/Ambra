package watcher

import (
	"testing"
)

func TestIsTempFileName(t *testing.T) {
	ignored := []string{
		"movie.nfo.tmp",
		"poster.jpg.TMP",
		"download.mkv.part",
		"video.mp4.crdownload",
		"tvshow.nfo~",
	}
	for _, name := range ignored {
		if !isTempFileName(name) {
			t.Errorf("expected %q to be treated as temporary file", name)
		}
	}

	kept := []string{
		"movie.mkv",
		"movie.nfo",
		"poster.jpg",
		"subtitle.ass",
	}
	for _, name := range kept {
		if isTempFileName(name) {
			t.Errorf("expected %q NOT to be treated as temporary file", name)
		}
	}
}
