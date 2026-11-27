package logs

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ambra-media/ambra/internal/api/dto"
	"github.com/ambra-media/ambra/internal/api/httpx"
	"github.com/ambra-media/ambra/internal/config"
)

var (
	reLogKVToken     = regexp.MustCompile(`(?i)\b(api_key|x-emby-token|x-mediabrowser-token|access_token|tmdb_api_key)(\s*[:=]\s*"?)([^\s&"',;\r\n]+)`)
	reLogAuthToken   = regexp.MustCompile(`(?i)(\bToken\s*=\s*")([^"]+)(")`)
	reLogJSONToken   = regexp.MustCompile(`(?i)"(AccessToken|X-Emby-Token|X-MediaBrowser-Token|api_key|Secret|Password|Pw)"(\s*:\s*")([^"]+)(")`)
	reLogBearer      = regexp.MustCompile(`(?i)\bBearer\s+[A-Za-z0-9._~+/=-]+`)
	reLogEmail       = regexp.MustCompile(`[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}`)
	reLogBracketIPv6 = regexp.MustCompile(`\[([0-9a-fA-F:.%]+)\](:\d{1,5})?`)
	reLogIPv4        = regexp.MustCompile(`\b(\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3})(:\d{1,5})?\b`)
	reLogRawIPv6     = regexp.MustCompile(`(?:[0-9a-fA-F]{0,4}:){2,7}[0-9a-fA-F]{0,4}(?:%[A-Za-z0-9_.-]+)?(?:\.[0-9]{1,3}){0,3}`)
)

// SanitizeLogLine redacts tokens, Bearer credentials, email addresses, and non-loopback IPv4/IPv6 addresses.
func SanitizeLogLine(line string) string {
	s := reLogKVToken.ReplaceAllString(line, "${1}${2}[REDACTED]")
	s = reLogAuthToken.ReplaceAllString(s, "${1}[REDACTED]${3}")
	s = reLogJSONToken.ReplaceAllString(s, `"${1}"${2}[REDACTED]${4}`)
	s = reLogBearer.ReplaceAllString(s, "Bearer [REDACTED]")
	s = reLogEmail.ReplaceAllString(s, "[REDACTED_EMAIL]")
	s = reLogBracketIPv6.ReplaceAllStringFunc(s, func(match string) string {
		sub := reLogBracketIPv6.FindStringSubmatch(match)
		if len(sub) < 2 {
			return match
		}
		addr, err := netip.ParseAddr(sub[1])
		if err != nil {
			return match
		}
		if addr.WithZone("").Unmap().IsLoopback() {
			return match
		}
		return "[REDACTED_IP]"
	})
	s = reLogRawIPv6.ReplaceAllStringFunc(s, func(match string) string {
		addr, err := netip.ParseAddr(match)
		if err != nil {
			return match
		}
		if addr.WithZone("").Unmap().IsLoopback() {
			return match
		}
		return "[REDACTED_IP]"
	})
	s = reLogIPv4.ReplaceAllStringFunc(s, func(match string) string {
		sub := reLogIPv4.FindStringSubmatch(match)
		if len(sub) < 2 {
			return match
		}
		ipPart := sub[1]
		if ipPart == config.ServerVersion {
			return match
		}
		addr, err := netip.ParseAddr(ipPart)
		if err != nil || addr.IsLoopback() {
			return match
		}
		return "[REDACTED_IP]"
	})
	return s
}

const (
	defaultRuntimeLogName = "ambra-runtime.log"
	maxLogLinesLimit      = 2000
	maxLogLineBytes       = 64 * 1024
)

var errInvalidLogName = errors.New("invalid log file name")

// AuthFuncs decouples route registration from the concrete middleware package.
type AuthFuncs struct {
	RequireAuth   func(http.Handler) http.Handler
	RequireAdmin  func(http.Handler) http.Handler
	UserIDFromCtx func(context.Context) string
}

// LogFileDto matches Emby's LogFile DTO.
type LogFileDto struct {
	DateCreated  time.Time `json:"DateCreated"`
	DateModified time.Time `json:"DateModified"`
	Size         int64     `json:"Size"`
	Name         string    `json:"Name"`
}

type Handler struct {
	cfg  *config.Config
	auth AuthFuncs

	mu           sync.RWMutex
	startedAt    time.Time
	runtimeLines []string
}

func New(cfg *config.Config) *Handler {
	now := time.Now().UTC()
	h := &Handler{
		cfg:       cfg,
		startedAt: now,
		runtimeLines: []string{
			fmt.Sprintf("%s [INFO] Ambra Media Server %s initialized", now.Format(time.RFC3339), config.ServerVersion),
		},
	}
	return h
}

// AppendRuntimeLog appends a line to the in-memory runtime log buffer.
func (h *Handler) AppendRuntimeLog(line string) {
	line = escapeLogControlChars(line)
	h.mu.Lock()
	defer h.mu.Unlock()
	h.runtimeLines = append(h.runtimeLines, line)
	if len(h.runtimeLines) > 2000 {
		h.runtimeLines = h.runtimeLines[len(h.runtimeLines)-2000:]
	}
}

func (h *Handler) RegisterRoutes(mux *http.ServeMux, auth AuthFuncs) {
	h.auth = auth
	wrapAuth := auth.RequireAuth
	if wrapAuth == nil {
		wrapAuth = passthrough
	}
	wrapAdmin := auth.RequireAdmin
	if wrapAdmin == nil {
		wrapAdmin = wrapAuth
	}

	handleAdmin := func(pattern string, fn http.HandlerFunc) {
		mux.Handle(pattern, wrapAdmin(fn))
	}

	handleAdmin("GET /System/Logs", h.handleListLogs)
	handleAdmin("GET /System/Logs/Query", h.handleQueryLogs)
	handleAdmin("GET /System/Logs/Log", h.handleGetLogByQuery)
	handleAdmin("GET /System/Logs/{Name}", h.handleGetLog)
	handleAdmin("GET /System/Logs/{Name}/Lines", h.handleGetLogLines)
}

func (h *Handler) logDir() string {
	if h.cfg == nil {
		return ""
	}
	dataDir := h.cfg.Snapshot().DataDir
	if dataDir == "" {
		return ""
	}
	return filepath.Join(dataDir, "logs")
}

func (h *Handler) listLogFiles() []LogFileDto {
	dir := h.logDir()
	var files []LogFileDto
	if dir != "" {
		if entries, err := os.ReadDir(dir); err == nil {
			for _, e := range entries {
				if e.IsDir() || e.Type()&os.ModeSymlink != 0 {
					continue
				}
				fi, err := os.Lstat(filepath.Join(dir, e.Name()))
				if err != nil || fi.IsDir() || fi.Mode()&os.ModeSymlink != 0 {
					continue
				}
				mod := fi.ModTime().UTC()
				files = append(files, LogFileDto{
					DateCreated:  mod,
					DateModified: mod,
					Size:         fi.Size(),
					Name:         e.Name(),
				})
			}
		}
	}

	if len(files) == 0 {
		h.mu.RLock()
		content := strings.Join(h.runtimeLines, "\n") + "\n"
		started := h.startedAt
		h.mu.RUnlock()
		files = append(files, LogFileDto{
			DateCreated:  started,
			DateModified: time.Now().UTC(),
			Size:         int64(len(content)),
			Name:         defaultRuntimeLogName,
		})
	}

	sort.Slice(files, func(i, j int) bool {
		if !files[i].DateModified.Equal(files[j].DateModified) {
			return files[i].DateModified.After(files[j].DateModified)
		}
		return files[i].Name < files[j].Name
	})
	return files
}

// resolveSafeLogPath prevents path traversal and symlink escapes by requiring a single-segment regular file inside logDir.
func (h *Handler) resolveSafeLogPath(name string) (string, error) {
	if name == "" ||
		name == "." ||
		name == ".." ||
		strings.Contains(name, "/") ||
		strings.Contains(name, "\\") ||
		strings.Contains(name, "..") ||
		filepath.Base(name) != name {
		return "", errInvalidLogName
	}
	dir := h.logDir()
	if dir == "" {
		return "", os.ErrNotExist
	}
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	realDir, err := filepath.EvalSymlinks(absDir)
	if err != nil {
		return "", err
	}
	realDir = filepath.Clean(realDir)

	target := filepath.Join(absDir, name)
	absTarget, err := filepath.Abs(target)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(absDir, absTarget)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
		return "", os.ErrPermission
	}

	fi, err := os.Lstat(absTarget)
	if err != nil {
		return "", err
	}
	if fi.Mode()&os.ModeSymlink != 0 || fi.IsDir() {
		return "", os.ErrPermission
	}

	realTarget, err := filepath.EvalSymlinks(absTarget)
	if err != nil {
		return "", err
	}
	realTarget = filepath.Clean(realTarget)
	realRel, err := filepath.Rel(realDir, realTarget)
	if err != nil || realRel == "." || strings.HasPrefix(realRel, "..") || filepath.IsAbs(realRel) {
		return "", os.ErrPermission
	}
	return realTarget, nil
}

func (h *Handler) readLogLinesPage(name string, start, limit int, sanitize bool) ([]string, int, error) {
	if start < 0 {
		start = 0
	}
	if limit <= 0 || limit > maxLogLinesLimit {
		limit = maxLogLinesLimit
	}

	serveRuntime := func() ([]string, int, error) {
		h.mu.RLock()
		total := len(h.runtimeLines)
		var page []string
		if start >= total {
			page = []string{}
		} else {
			end := total
			if start+limit < end {
				end = start + limit
			}
			page = append([]string(nil), h.runtimeLines[start:end]...)
		}
		h.mu.RUnlock()
		if sanitize {
			for i := range page {
				page[i] = SanitizeLogLine(page[i])
			}
		}
		return page, total, nil
	}

	safePath, err := h.resolveSafeLogPath(name)
	if err != nil {
		if os.IsNotExist(err) && name == defaultRuntimeLogName {
			return serveRuntime()
		}
		return nil, 0, err
	}
	f, err := os.Open(safePath)
	if err != nil {
		if os.IsNotExist(err) && name == defaultRuntimeLogName {
			return serveRuntime()
		}
		return nil, 0, err
	}
	defer f.Close()

	page := make([]string, 0)
	total := 0
	reader := bufio.NewReaderSize(f, 64*1024)
	for {
		line, hasLine, readErr := readBoundedLine(reader, maxLogLineBytes)
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			return nil, 0, readErr
		}
		if hasLine {
			if total >= start && len(page) < limit {
				clean := escapeLogControlChars(line)
				if sanitize {
					clean = SanitizeLogLine(clean)
				}
				page = append(page, clean)
			}
			total++
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
	}
	return page, total, nil
}

func readBoundedLine(r *bufio.Reader, maxBytes int) (string, bool, error) {
	var buf []byte
	gotData := false
	for {
		chunk, isPrefix, err := r.ReadLine()
		if len(chunk) > 0 {
			gotData = true
			if len(buf) < maxBytes {
				rem := maxBytes - len(buf)
				if len(chunk) > rem {
					buf = append(buf, chunk[:rem]...)
				} else {
					buf = append(buf, chunk...)
				}
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return string(buf), gotData, io.EOF
			}
			return "", false, err
		}
		if !isPrefix {
			return string(buf), true, nil
		}
	}
}

func (h *Handler) handleListLogs(w http.ResponseWriter, r *http.Request) {
	httpx.JSON(w, http.StatusOK, h.listLogFiles())
}

func (h *Handler) handleQueryLogs(w http.ResponseWriter, r *http.Request) {
	q := httpx.ParseQuery(r)
	all := h.listLogFiles()
	total := len(all)
	start := q.Int("StartIndex", 0)
	limit := q.Int("Limit", 0)
	if start < 0 {
		start = 0
	}
	var page []LogFileDto
	if start >= total {
		page = []LogFileDto{}
	} else {
		end := total
		if limit > 0 && start+limit < end {
			end = start + limit
		}
		page = all[start:end]
	}
	httpx.JSON(w, http.StatusOK, dto.QueryResult[LogFileDto]{
		Items:            page,
		TotalRecordCount: total,
	})
}

func (h *Handler) handleGetLogByQuery(w http.ResponseWriter, r *http.Request) {
	q := httpx.ParseQuery(r)
	name := strings.TrimSpace(q.Get("name"))
	if name == "" {
		httpx.Error(w, http.StatusBadRequest, "missing log file name")
		return
	}
	sanitize := q.Bool("Sanitize") != nil && *q.Bool("Sanitize")
	h.serveLogByName(w, name, sanitize)
}

func (h *Handler) handleGetLog(w http.ResponseWriter, r *http.Request) {
	q := httpx.ParseQuery(r)
	sanitize := q.Bool("Sanitize") != nil && *q.Bool("Sanitize")
	h.serveLogByName(w, r.PathValue("Name"), sanitize)
}

func (h *Handler) serveLogByName(w http.ResponseWriter, name string, sanitize bool) {
	serveRuntime := func() {
		h.mu.RLock()
		lines := append([]string(nil), h.runtimeLines...)
		h.mu.RUnlock()
		if sanitize {
			for i := range lines {
				lines[i] = SanitizeLogLine(lines[i])
			}
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(strings.Join(lines, "\n")))
		if len(lines) > 0 {
			_, _ = w.Write([]byte("\n"))
		}
	}

	safePath, err := h.resolveSafeLogPath(name)
	if err != nil {
		if os.IsNotExist(err) && name == defaultRuntimeLogName {
			serveRuntime()
			return
		}
		writeLogError(w, err)
		return
	}
	f, err := os.Open(safePath)
	if err != nil {
		if os.IsNotExist(err) && name == defaultRuntimeLogName {
			serveRuntime()
			return
		}
		writeLogError(w, err)
		return
	}
	defer f.Close()

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	if !sanitize {
		_, _ = io.Copy(w, f)
		return
	}
	reader := bufio.NewReaderSize(f, 64*1024)
	for {
		line, hasLine, readErr := readBoundedLine(reader, maxLogLineBytes)
		if hasLine {
			_, _ = io.WriteString(w, SanitizeLogLine(line)+"\n")
		}
		if readErr != nil {
			break
		}
	}
}

func (h *Handler) handleGetLogLines(w http.ResponseWriter, r *http.Request) {
	q := httpx.ParseQuery(r)
	name := r.PathValue("Name")
	start := q.Int("StartIndex", 0)
	limit := q.Int("Limit", 100)
	sanitize := q.Bool("Sanitize") != nil && *q.Bool("Sanitize")
	page, total, err := h.readLogLinesPage(name, start, limit, sanitize)
	if err != nil {
		writeLogError(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, dto.QueryResult[string]{
		Items:            page,
		TotalRecordCount: total,
	})
}

func writeLogError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errInvalidLogName):
		httpx.Error(w, http.StatusBadRequest, "invalid log file name")
	case os.IsPermission(err):
		httpx.Error(w, http.StatusForbidden, "access to log file denied")
	case os.IsNotExist(err):
		httpx.Error(w, http.StatusNotFound, "log file not found")
	default:
		httpx.Error(w, http.StatusInternalServerError, err.Error())
	}
}

func passthrough(next http.Handler) http.Handler { return next }
