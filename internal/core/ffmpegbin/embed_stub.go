//go:build !embed_ffmpeg

package ffmpegbin

// EmbeddedFFmpeg holds the embedded ffmpeg binary bytes when built with -tags embed_ffmpeg.
var EmbeddedFFmpeg []byte

// EmbeddedFFprobe holds the embedded ffprobe binary bytes when built with -tags embed_ffmpeg.
var EmbeddedFFprobe []byte

// HasEmbedded reports whether ffmpeg or ffprobe binaries are embedded in the executable.
func HasEmbedded() bool {
	return len(EmbeddedFFmpeg) > 0 || len(EmbeddedFFprobe) > 0
}
