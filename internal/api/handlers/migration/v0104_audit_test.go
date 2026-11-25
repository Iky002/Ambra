package migration

import (
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode/utf8"

	coremigration "github.com/ambra-media/ambra/internal/core/migration"
)

func TestMigrationV0104CredentialRedactionAndUTF8SafeTruncation(t *testing.T) {
	// 1. Redact URL credentials in error messages & reports
	rawErr := `Get "https://admin:s3cr3tP@ss@emby.internal:8096/System/Info": dial tcp failed; fallback http://user:pw@192.168.1.5:8096/emby`
	// Note: password without '@'
	rawErrSimple := `Get "https://admin:supersecret@emby.internal:8096/System/Info": dial tcp failed; fallback http://user:pw@192.168.1.5:8096/emby`
	redacted := redactURLCredentials(rawErrSimple)
	if strings.Contains(redacted, "supersecret") || strings.Contains(redacted, "user:pw") {
		t.Fatalf("expected credentials redacted, got %q", redacted)
	}
	if !strings.Contains(redacted, "https://***@emby.internal:8096/System/Info") || !strings.Contains(redacted, "http://***@192.168.1.5:8096/emby") {
		t.Fatalf("unexpected redacted format: %q", redacted)
	}
	_ = rawErr

	// sanitizeReport redacts credentials in Errors slice and SourceURL
	rep := sanitizeReport(coremigration.MigrationReport{
		SourceURL: "https://admin:pw@emby.example.com:8096",
		Errors:    []string{rawErrSimple},
	})
	if strings.Contains(rep.SourceURL, "pw") {
		t.Fatalf("SourceURL leaked credential: %q", rep.SourceURL)
	}
	if strings.Contains(rep.Errors[0], "supersecret") || !strings.Contains(rep.Errors[0], "https://***@emby.internal:8096") {
		t.Fatalf("report Error leaked credential: %q", rep.Errors[0])
	}

	// writeMigrationError also redacts URL credentials
	rec := httptest.NewRecorder()
	writeMigrationError(rec, errors.New(`failed calling https://alice:mySecret123@old-emby.local:8096/Users`))
	if strings.Contains(rec.Body.String(), "mySecret123") || !strings.Contains(rec.Body.String(), "https://***@old-emby.local:8096/Users") {
		t.Fatalf("HTTP error response leaked credential: %s", rec.Body.String())
	}

	// 2. Rune-safe truncation preserves valid UTF-8 on multi-byte Chinese & emoji characters
	zhMsg := strings.Repeat("迁移错误提示🔥", 100) // 700 runes, > 2000 bytes
	trunc := truncateErrorMsg(zhMsg, 20)
	if !utf8.ValidString(trunc) {
		t.Fatalf("truncated string is invalid UTF-8: %q", trunc)
	}
	if len([]rune(trunc)) != 20 || !strings.HasSuffix(trunc, "...") {
		t.Fatalf("expected 20 runes ending in '...', got %d runes: %q", len([]rune(trunc)), trunc)
	}
	if got := truncateErrorMsg("中文测试", 2); !utf8.ValidString(got) || got != "中文" {
		t.Fatalf("expected small rune truncation '中文', got %q", got)
	}
	if got := truncateErrorMsg("中文", 0); got != "" {
		t.Fatalf("expected empty string for maxRunes<=0, got %q", got)
	}
}
