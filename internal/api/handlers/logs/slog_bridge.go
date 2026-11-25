package logs

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// DailyLogFilePath returns the daily runtime log path `<dataDir>/logs/ambra-YYYY-MM-DD.log`.
func DailyLogFilePath(dataDir string, now time.Time) string {
	if strings.TrimSpace(dataDir) == "" {
		return ""
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	return filepath.Join(dataDir, "logs", fmt.Sprintf("ambra-%s.log", now.UTC().Format("2006-01-02")))
}

type multiLogSink struct {
	mu          sync.Mutex
	stdout      io.Writer
	logsHandler *Handler
	logFilePath string
	logDir      string
	currentDate string
	rotatable   bool
	file        *os.File
}

// MultiSlogHandler is a slog.Handler that formats every log record as
// `2026-11-24T12:00:00Z [INFO] message key=val ...` and writes it concurrently-safely to
// stdout, the in-memory logs.Handler runtime buffer, and a persistent log file.
type MultiSlogHandler struct {
	sink   *multiLogSink
	level  *atomic.Int64
	attrs  []slog.Attr
	groups []string
}

func parseDailyLogDate(logFilePath string) (string, bool) {
	base := filepath.Base(logFilePath)
	if !strings.HasPrefix(base, "ambra-") || !strings.HasSuffix(base, ".log") {
		return "", false
	}
	datePart := strings.TrimSuffix(strings.TrimPrefix(base, "ambra-"), ".log")
	if len(datePart) != len("2006-01-02") {
		return "", false
	}
	if _, err := time.Parse("2006-01-02", datePart); err != nil {
		return "", false
	}
	return datePart, true
}

// NewMultiSlogHandler creates a slog.Handler fanning out records to stdout, logsHandler, and logFilePath.
// Any of the three destinations may be nil/empty to disable that sink.
func NewMultiSlogHandler(stdout io.Writer, logsHandler *Handler, logFilePath string) *MultiSlogHandler {
	cleanPath := strings.TrimSpace(logFilePath)
	sink := &multiLogSink{
		stdout:      stdout,
		logsHandler: logsHandler,
		logFilePath: cleanPath,
	}
	if cleanPath != "" {
		sink.logDir = filepath.Dir(cleanPath)
		if datePart, ok := parseDailyLogDate(cleanPath); ok {
			sink.rotatable = true
			sink.currentDate = datePart
		}
	}
	lvl := &atomic.Int64{}
	lvl.Store(int64(slog.LevelInfo))
	return &MultiSlogHandler{
		sink:  sink,
		level: lvl,
	}
}

// WithLevel sets the minimum log level for this handler and returns it for chaining.
func (h *MultiSlogHandler) WithLevel(lvl slog.Leveler) *MultiSlogHandler {
	if lvl != nil && h.level != nil {
		h.level.Store(int64(lvl.Level()))
	}
	return h
}

// SetLogsHandler attaches or replaces the in-memory logs.Handler runtime buffer sink.
func (h *MultiSlogHandler) SetLogsHandler(lh *Handler) {
	h.sink.mu.Lock()
	h.sink.logsHandler = lh
	h.sink.mu.Unlock()
}

// Close closes the underlying persistent log file if it was opened.
func (h *MultiSlogHandler) Close() error {
	h.sink.mu.Lock()
	defer h.sink.mu.Unlock()
	if h.sink.file == nil {
		return nil
	}
	err := h.sink.file.Close()
	h.sink.file = nil
	return err
}

func (h *MultiSlogHandler) Enabled(_ context.Context, level slog.Level) bool {
	minLevel := slog.LevelInfo
	if h.level != nil {
		minLevel = slog.Level(h.level.Load())
	}
	return level >= minLevel
}

func (h *MultiSlogHandler) Handle(_ context.Context, r slog.Record) error {
	line := h.formatRecord(r)

	h.sink.mu.Lock()
	defer h.sink.mu.Unlock()

	if h.sink.stdout != nil {
		_, _ = io.WriteString(h.sink.stdout, line+"\n")
	}
	if h.sink.logsHandler != nil {
		h.sink.logsHandler.AppendRuntimeLog(line)
	}
	if h.sink.logFilePath != "" {
		recTime := r.Time
		if recTime.IsZero() {
			recTime = time.Now().UTC()
		}
		recDate := recTime.UTC().Format("2006-01-02")
		if h.sink.rotatable && recDate != h.sink.currentDate {
			if h.sink.file != nil {
				_ = h.sink.file.Close()
				h.sink.file = nil
			}
			h.sink.currentDate = recDate
			h.sink.logFilePath = filepath.Join(h.sink.logDir, fmt.Sprintf("ambra-%s.log", recDate))
		}
		if h.sink.file == nil {
			if err := os.MkdirAll(filepath.Dir(h.sink.logFilePath), 0o700); err != nil {
				return err
			}
			f, err := os.OpenFile(h.sink.logFilePath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
			if err != nil {
				return err
			}
			h.sink.file = f
		}
		_, err := io.WriteString(h.sink.file, line+"\n")
		return err
	}
	return nil
}

func (h *MultiSlogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	cp := *h
	cp.attrs = append(append([]slog.Attr(nil), h.attrs...), attrs...)
	return &cp
}

func (h *MultiSlogHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	cp := *h
	cp.groups = append(append([]string(nil), h.groups...), name)
	return &cp
}

func (h *MultiSlogHandler) formatRecord(r slog.Record) string {
	ts := r.Time
	if ts.IsZero() {
		ts = time.Now().UTC()
	} else {
		ts = ts.UTC()
	}

	var b strings.Builder
	b.WriteString(ts.Format(time.RFC3339))
	b.WriteString(" [")
	b.WriteString(r.Level.String())
	b.WriteString("] ")
	b.WriteString(escapeLogControlChars(r.Message))

	prefix := ""
	if len(h.groups) > 0 {
		prefix = strings.Join(h.groups, ".") + "."
	}

	var appendAttr func(pref string, a slog.Attr)
	appendAttr = func(pref string, a slog.Attr) {
		a.Value = a.Value.Resolve()
		if a.Equal(slog.Attr{}) {
			return
		}
		if a.Value.Kind() == slog.KindGroup {
			attrs := a.Value.Group()
			if len(attrs) == 0 {
				return
			}
			nextPref := pref
			if a.Key != "" {
				nextPref += escapeLogControlChars(a.Key) + "."
			}
			for _, ga := range attrs {
				appendAttr(nextPref, ga)
			}
			return
		}
		b.WriteByte(' ')
		b.WriteString(pref)
		b.WriteString(escapeLogControlChars(a.Key))
		b.WriteByte('=')
		b.WriteString(formatAttrValue(a.Value))
	}

	for _, a := range h.attrs {
		appendAttr(prefix, a)
	}
	r.Attrs(func(a slog.Attr) bool {
		appendAttr(prefix, a)
		return true
	})

	return b.String()
}

func escapeLogControlChars(s string) string {
	if !strings.ContainsAny(s, "\r\n") {
		return s
	}
	s = strings.ReplaceAll(s, "\r", `\r`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	return s
}

func formatAttrValue(v slog.Value) string {
	s := v.String()
	if s == "" || strings.ContainsAny(s, " \t\n\r\"") {
		return strconv.Quote(s)
	}
	return escapeLogControlChars(s)
}
