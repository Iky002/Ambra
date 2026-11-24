package logs

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMultiSlogHandlerDailyLogRotation(t *testing.T) {
	tmp := t.TempDir()
	day1 := time.Date(2026, 11, 24, 23, 59, 50, 0, time.UTC)
	day2 := time.Date(2026, 11, 25, 0, 0, 10, 0, time.UTC)

	day1Path := DailyLogFilePath(tmp, day1)
	day2Path := DailyLogFilePath(tmp, day2)

	var stdout bytes.Buffer
	h := NewMultiSlogHandler(&stdout, nil, day1Path)
	defer func() { _ = h.Close() }()

	r1 := slog.NewRecord(day1, slog.LevelInfo, "before midnight", 0)
	if err := h.Handle(context.Background(), r1); err != nil {
		t.Fatalf("Handle day1: %v", err)
	}

	r2 := slog.NewRecord(day2, slog.LevelInfo, "after midnight", 0)
	if err := h.Handle(context.Background(), r2); err != nil {
		t.Fatalf("Handle day2: %v", err)
	}

	data1, err := os.ReadFile(day1Path)
	if err != nil {
		t.Fatalf("read day1 log: %v", err)
	}
	data2, err := os.ReadFile(day2Path)
	if err != nil {
		t.Fatalf("read day2 log: %v", err)
	}
	if !strings.Contains(string(data1), "before midnight") || strings.Contains(string(data1), "after midnight") {
		t.Fatalf("unexpected day1 log content: %q", string(data1))
	}
	if !strings.Contains(string(data2), "after midnight") || strings.Contains(string(data2), "before midnight") {
		t.Fatalf("unexpected day2 log content: %q", string(data2))
	}

	// Verify parseDailyLogDate edge cases
	if _, ok := parseDailyLogDate(filepath.Join(tmp, "custom.log")); ok {
		t.Fatal("expected custom.log not to be parsed as daily log")
	}
	if _, ok := parseDailyLogDate(filepath.Join(tmp, "ambra-notadate.log")); ok {
		t.Fatal("expected invalid date not to be parsed as daily log")
	}
	if _, ok := parseDailyLogDate(filepath.Join(tmp, "ambra-9999-99-99.log")); ok {
		t.Fatal("expected out-of-range date not to be parsed as daily log")
	}
}
