package migration

import (
	"testing"
)

func TestMigrationV0107AuditStripQueryFragmentAndClose(t *testing.T) {
	got := stripURLUserinfo("https://admin:s3cr3t@emby.example.com:8920/emby?api_key=leak123#frag")
	want := "https://emby.example.com:8920/emby"
	if got != want {
		t.Fatalf("stripURLUserinfo = %q, want %q", got, want)
	}
	if gotEmpty := stripURLUserinfo("   "); gotEmpty != "" {
		t.Fatalf("expected empty string, got %q", gotEmpty)
	}

	// Close is safe on nil handler / nil migrator and cancels active migrator cleanly
	var nilH *Handler
	nilH.Close()
	New(nil).Close()
}
