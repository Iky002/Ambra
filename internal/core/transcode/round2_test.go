package transcode

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/ambra-media/ambra/internal/config"
)

func TestStopByDeviceForUser(t *testing.T) {
	tmp := t.TempDir()
	cfg := config.Default(tmp)
	mgr := NewManager(cfg, WithCapabilities(SoftwareOnlyCapabilities()))
	defer mgr.Close()

	register := func(userID, devID, psID string) {
		if _, err := mgr.RegisterSession(SessionParams{
			UserID:        userID,
			DeviceID:      devID,
			PlaySessionID: psID,
			ItemID:        "item-" + psID,
			MediaSourceID: "ms-" + psID,
			InputPath:     filepath.Join(tmp, psID+".mkv"),
		}); err != nil {
			t.Fatalf("RegisterSession(%s): %v", psID, err)
		}
	}

	register("user-a", "dev-a", "ps-a")
	register("user-b", "dev-b", "ps-b")
	register("", "dev-anon", "ps-anon")

	// Non-admin user-b cannot stop user-a's session.
	if stopped := mgr.StopByDeviceForUser("dev-a", "ps-a", "user-b", false); stopped != 0 {
		t.Fatalf("expected 0 stopped for cross-user non-admin, got %d", stopped)
	}
	if len(mgr.ActiveSessions()) != 3 {
		t.Fatalf("expected 3 active sessions, got %d", len(mgr.ActiveSessions()))
	}

	// Non-admin user-a can stop their own session.
	if stopped := mgr.StopByDeviceForUser("dev-a", "ps-a", "user-a", false); stopped != 1 {
		t.Fatalf("expected 1 stopped for owner, got %d", stopped)
	}

	// Non-admin can stop an unowned session (UserID == "").
	if stopped := mgr.StopByDeviceForUser("dev-anon", "ps-anon", "user-a", false); stopped != 1 {
		t.Fatalf("expected 1 stopped for unowned session, got %d", stopped)
	}

	// Admin can stop user-b's session.
	if stopped := mgr.StopByDeviceForUser("dev-b", "ps-b", "admin-1", true); stopped != 1 {
		t.Fatalf("expected 1 stopped for admin, got %d", stopped)
	}
	if len(mgr.ActiveSessions()) != 0 {
		t.Fatalf("expected 0 active sessions, got %d", len(mgr.ActiveSessions()))
	}

	// Legacy StopByDevice still stops owned sessions.
	register("user-c", "dev-c", "ps-c")
	if stopped := mgr.StopByDevice("dev-c", ""); stopped != 1 {
		t.Fatalf("expected StopByDevice to stop 1 session, got %d", stopped)
	}
}

func TestBuildFFmpegArgsSubtitleBurnIn(t *testing.T) {
	t.Run("external text subtitle with VAAPI ordering", func(t *testing.T) {
		caps := &HWCapabilities{
			PreferredH264Encoder: "h264_vaapi",
			VAAPIDevice:          "/dev/dri/renderD128",
			Codecs: []CodecInfo{
				{ShortName: "h264_vaapi", VideoMediaType: "h264", IsHardwareCodec: true},
			},
		}
		args := BuildFFmpegArgs(SessionParams{
			InputPath:      "/media/movie.mkv",
			VideoCodec:     "h264",
			AudioCodec:     "aac",
			MaxWidth:       1280,
			SubtitleMethod: "Encode",
			SubtitleCodec:  "ass",
			SubtitlePath:   "/media/My [Show]:1's/sub.ass",
		}, 0, "/tmp/out", caps)

		joined := strings.Join(args, " ")
		if !strings.Contains(joined, "-sn") {
			t.Fatalf("expected -sn in args: %s", joined)
		}
		if !strings.Contains(joined, "-map 0:v:0") {
			t.Fatalf("expected -map 0:v:0 in args: %s", joined)
		}
		vfIdx := indexOf(args, "-vf")
		if vfIdx < 0 || vfIdx+1 >= len(args) {
			t.Fatalf("expected -vf flag in args: %v", args)
		}
		vf := args[vfIdx+1]
		wantSub := `subtitles='/media/My \[Show\]\:1\'s/sub.ass'`
		if !strings.HasPrefix(vf, wantSub) {
			t.Fatalf("expected -vf to start with %q, got %q", wantSub, vf)
		}
		hwIdx := strings.Index(vf, "format=nv12,hwupload")
		subIdx := strings.Index(vf, "subtitles=")
		if hwIdx < 0 || subIdx >= hwIdx {
			t.Fatalf("expected subtitles filter before VAAPI hwupload, got %q", vf)
		}
	})

	t.Run("embedded text subtitle uses input si", func(t *testing.T) {
		subIdx := 2
		args := BuildFFmpegArgs(SessionParams{
			InputPath:           "/media/anime.mkv",
			VideoCodec:          "h264",
			AudioCodec:          "aac",
			SubtitleStreamIndex: &subIdx,
			SubtitleMethod:      "Encode",
			SubtitleCodec:       "srt",
		}, 0, "/tmp/out", SoftwareOnlyCapabilities())

		vfIdx := indexOf(args, "-vf")
		if vfIdx < 0 || vfIdx+1 >= len(args) {
			t.Fatalf("expected -vf in args: %v", args)
		}
		want := "subtitles='/media/anime.mkv':si=2"
		if args[vfIdx+1] != want {
			t.Fatalf("got -vf %q, want %q", args[vfIdx+1], want)
		}
	})

	t.Run("bitmap subtitle uses filter_complex overlay", func(t *testing.T) {
		for _, codec := range []string{"pgssub", "hdmv_pgs_subtitle", "dvd_subtitle", "dvdsub"} {
			subIdx := 3
			args := BuildFFmpegArgs(SessionParams{
				InputPath:           "/media/bluray.mkv",
				VideoCodec:          "h264",
				AudioCodec:          "aac",
				SubtitleStreamIndex: &subIdx,
				SubtitleMethod:      "Encode",
				SubtitleCodec:       codec,
			}, 0, "/tmp/out", SoftwareOnlyCapabilities())

			fcIdx := indexOf(args, "-filter_complex")
			if fcIdx < 0 || fcIdx+1 >= len(args) {
				t.Fatalf("[%s] expected -filter_complex in args: %v", codec, args)
			}
			if args[fcIdx+1] != "[0:v:0][0:3]overlay[v]" {
				t.Fatalf("[%s] got -filter_complex %q", codec, args[fcIdx+1])
			}
			if indexOf(args, "-vf") >= 0 {
				t.Fatalf("[%s] did not expect -vf alongside -filter_complex: %v", codec, args)
			}
			joined := strings.Join(args, " ")
			if !strings.Contains(joined, "-map [v]") {
				t.Fatalf("[%s] expected -map [v], got: %s", codec, joined)
			}
			if strings.Contains(joined, "-map 0:v:0") {
				t.Fatalf("[%s] should not contain -map 0:v:0, got: %s", codec, joined)
			}
			if !strings.Contains(joined, "-sn") {
				t.Fatalf("[%s] expected -sn, got: %s", codec, joined)
			}
		}
	})
}

func indexOf(slice []string, target string) int {
	for i, s := range slice {
		if s == target {
			return i
		}
	}
	return -1
}
