//go:build embed_ffmpeg

package ffmpegbin

import (
	"embed"
	"io/fs"
	"path"
)

//go:embed bin/ffmpeg*
var embeddedFFmpegFS embed.FS

//go:embed bin/ffprobe*
var embeddedFFprobeFS embed.FS

// EmbeddedFFmpeg holds the embedded ffmpeg binary bytes compiled into the executable.
var EmbeddedFFmpeg = loadFirstEmbedded(embeddedFFmpegFS, "ffmpeg")

// EmbeddedFFprobe holds the embedded ffprobe binary bytes compiled into the executable.
var EmbeddedFFprobe = loadFirstEmbedded(embeddedFFprobeFS, "ffprobe")

// HasEmbedded reports whether ffmpeg or ffprobe binaries are embedded in the executable.
func HasEmbedded() bool {
	return len(EmbeddedFFmpeg) > 0 || len(EmbeddedFFprobe) > 0
}

func loadFirstEmbedded(fsys embed.FS, prefix string) []byte {
	entries, err := fs.ReadDir(fsys, "bin")
	if err != nil {
		return nil
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if name != prefix && name != prefix+".exe" {
			continue
		}
		data, err := fsys.ReadFile(path.Join("bin", name))
		if err == nil && len(data) > 0 {
			return data
		}
	}
	return nil
}
