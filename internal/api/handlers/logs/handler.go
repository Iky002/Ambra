package logs

import (
	"bufio"
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ambra-media/ambra/internal/api/dto"
	"github.com/ambra-media/ambra/internal/api/httpx"
	"github.com/ambra-media/ambra/internal/config"
)

const defaultRuntimeLogName = "ambra-runtime.log"

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
	if h.cfg == nil || h.cfg.DataDir == "" {
		return ""
	}
	return filepath.Join(h.cfg.DataDir, "logs")
}

func (h *Handler) listLogFiles() []LogFileDto {
	dir := h.logDir()
	var files []LogFileDto
	if dir != "" {
		if entries, err := os.ReadDir(dir); err == nil {
			for _, e := range entries {
				if e.IsDir() {
					continue
				}
				info, err := e.Info()
				if err != nil {
					continue
				}
				mod := info.ModTime().UTC()
				files = append(files, LogFileDto{
					DateCreated:  mod,
					DateModified: mod,
					Size:         info.Size(),
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

// resolveSafeLogPath prevents path traversal by requiring a single-segment filename inside logDir.
func (h *Handler) resolveSafeLogPath(name string) (string, error) {
	if name == "" ||
		name == "." ||
		name == ".." ||
		strings.Contains(name, "/") ||
		strings.Contains(name, "\\") ||
		strings.Contains(name, "..") ||
		filepath.Base(name) != name {
		return "", os.ErrPermission
	}
	dir := h.logDir()
	if dir == "" {
		return "", os.ErrNotExist
	}
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	target := filepath.Join(absDir, name)
	absTarget, err := filepath.Abs(target)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(absDir, absTarget)
	if err != nil || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
		return "", os.ErrPermission
	}
	return absTarget, nil
}

func (h *Handler) readLogLines(name string) ([]string, error) {
	safePath, err := h.resolveSafeLogPath(name)
	if err != nil {
		if os.IsNotExist(err) && name == defaultRuntimeLogName {
			h.mu.RLock()
			cp := append([]string(nil), h.runtimeLines...)
			h.mu.RUnlock()
			return cp, nil
		}
		return nil, err
	}
	f, err := os.Open(safePath)
	if err != nil {
		if os.IsNotExist(err) && name == defaultRuntimeLogName {
			h.mu.RLock()
			cp := append([]string(nil), h.runtimeLines...)
			h.mu.RUnlock()
			return cp, nil
		}
		return nil, err
	}
	defer f.Close()

	var lines []string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if lines == nil {
		lines = []string{}
	}
	return lines, nil
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
	h.serveLogByName(w, name)
}

func (h *Handler) handleGetLog(w http.ResponseWriter, r *http.Request) {
	h.serveLogByName(w, r.PathValue("Name"))
}

func (h *Handler) serveLogByName(w http.ResponseWriter, name string) {
	lines, err := h.readLogLines(name)
	if err != nil {
		if os.IsPermission(err) {
			httpx.Error(w, http.StatusBadRequest, "invalid log file name")
			return
		}
		if os.IsNotExist(err) {
			httpx.Error(w, http.StatusNotFound, "log file not found")
			return
		}
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(strings.Join(lines, "\n")))
	if len(lines) > 0 {
		_, _ = w.Write([]byte("\n"))
	}
}

func (h *Handler) handleGetLogLines(w http.ResponseWriter, r *http.Request) {
	q := httpx.ParseQuery(r)
	name := r.PathValue("Name")
	lines, err := h.readLogLines(name)
	if err != nil {
		if os.IsPermission(err) {
			httpx.Error(w, http.StatusBadRequest, "invalid log file name")
			return
		}
		if os.IsNotExist(err) {
			httpx.Error(w, http.StatusNotFound, "log file not found")
			return
		}
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	total := len(lines)
	start := q.Int("StartIndex", 0)
	limit := q.Int("Limit", 100)
	if start < 0 {
		start = 0
	}
	var page []string
	if start >= total {
		page = []string{}
	} else {
		end := total
		if limit > 0 && start+limit < end {
			end = start + limit
		}
		page = lines[start:end]
	}
	httpx.JSON(w, http.StatusOK, dto.QueryResult[string]{
		Items:            page,
		TotalRecordCount: total,
	})
}

func passthrough(next http.Handler) http.Handler { return next }
