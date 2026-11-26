//go:build embed_ffmpeg

package ffmpegbin

import (
	"embed"
	"path"
	"runtime"
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
	candidates := []string{prefix, prefix + ".exe"}
	if runtime.GOOS == "windows" {
		candidates = []string{prefix + ".exe", prefix}
	}
	for _, name := range candidates {
		data, err := fsys.ReadFile(path.Join("bin", name))
		if err == nil && len(data) > 0 {
			return data
		}
	}
	return nil
}
