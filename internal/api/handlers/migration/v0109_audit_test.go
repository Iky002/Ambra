package migration

import (
	"strings"
	"testing"
)

func TestMigrationV0109AuditL15RedactPasswordWithAtSign(t *testing.T) {
	cases := []struct {
		in     string
		want   string
		forbid []string
	}{
		{
			in:     `dial failed: Get "https://admin:p@ss@w0rd@emby.example.com:8096/System/Info": EOF`,
			want:   `dial failed: Get "https://***@emby.example.com:8096/System/Info": EOF`,
			forbid: []string{"admin", "p@ss", "w0rd"},
		},
		{
			in:     `http://user:a@b@10.0.0.5/path?email=foo@bar.com`,
			want:   `http://***@10.0.0.5/path?email=foo@bar.com`,
			forbid: []string{"user:", "a@b"},
		},
	}
	for _, tc := range cases {
		got := redactURLCredentials(tc.in)
		if got != tc.want {
			t.Fatalf("L-15: redactURLCredentials(%q) = %q, want %q", tc.in, got, tc.want)
		}
		for _, bad := range tc.forbid {
			if strings.Contains(got, bad) {
				t.Fatalf("L-15: secret fragment %q leaked in %q", bad, got)
			}
		}
	}

	stripped := stripURLUserinfo("https://admin:p@ss@emby.example.com/emby")
	if strings.Contains(stripped, "p@ss") || strings.Contains(stripped, "admin") {
		t.Fatalf("L-15: stripURLUserinfo leaked credentials: %q", stripped)
	}
}
