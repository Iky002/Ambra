package guid

import (
	"strings"

	"github.com/google/uuid"
)

// New returns a 32-char lowercase hex UUID without dashes (Emby standard format).
func New() string {
	return strings.ReplaceAll(uuid.NewString(), "-", "")
}

// Normalize strips dashes and lowercases an Emby GUID string.
func Normalize(id string) string {
	return strings.ToLower(strings.ReplaceAll(strings.TrimSpace(id), "-", ""))
}

// FormatDashed converts a 32-char hex GUID into standard 36-char dashed UUID if valid.
func FormatDashed(id string) string {
	n := Normalize(id)
	if len(n) != 32 {
		return id
	}
	u, err := uuid.Parse(n)
	if err != nil {
		return id
	}
	return u.String()
}
