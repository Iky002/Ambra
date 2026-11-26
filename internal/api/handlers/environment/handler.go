package environment

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/ambra-media/ambra/internal/api/httpx"
	"github.com/ambra-media/ambra/internal/config"
)

const maxDirectoryEntries = 2000

// AuthFuncs decouples route registration from the concrete middleware package.
type AuthFuncs struct {
	RequireAuth   func(http.Handler) http.Handler
	RequireAdmin  func(http.Handler) http.Handler
	UserIDFromCtx func(context.Context) string
}

// FileSystemEntryInfo matches Emby's FileSystemEntryInfo DTO.
type FileSystemEntryInfo struct {
	Name string `json:"Name"`
	Path string `json:"Path"`
	Type string `json:"Type"` // Directory / File / NetworkComputer / NetworkShare
}

// DefaultDirectoryBrowserInfo matches GET /Environment/DefaultDirectoryBrowser.
type DefaultDirectoryBrowserInfo struct {
	Path string `json:"Path"`
}

// ValidatePathRequest matches POST /Environment/ValidatePath.
type ValidatePathRequest struct {
	ValidateWritable bool   `json:"ValidateWritable"`
	Path             string `json:"Path"`
	IsFile           *bool  `json:"IsFile,omitempty"`
}

type Handler struct {
	cfg  *config.Config
	auth AuthFuncs
}

func New(cfg *config.Config) *Handler {
	return &Handler{cfg: cfg}
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

	handleAdmin("GET /Environment/Drives", h.handleDrives)
	handleAdmin("GET /Environment/DirectoryContents", h.handleDirectoryContents)
	handleAdmin("GET /Environment/ParentPath", h.handleParentPath)
	handleAdmin("GET /Environment/DefaultDirectoryBrowser", h.handleDefaultDirectoryBrowser)
	handleAdmin("GET /Environment/NetworkShares", h.handleNetworkShares)
	handleAdmin("POST /Environment/ValidatePath", h.handleValidatePath)
}

func (h *Handler) handleDrives(w http.ResponseWriter, r *http.Request) {
	var drives []FileSystemEntryInfo
	seen := make(map[string]bool)
	addDrive := func(name, p string) {
		clean := filepath.Clean(p)
		if seen[clean] {
			return
		}
		info, err := os.Stat(clean)
		if err != nil || !info.IsDir() {
			return
		}
		seen[clean] = true
		drives = append(drives, FileSystemEntryInfo{
			Name: name,
			Path: clean,
			Type: "Directory",
		})
	}

	addDrive("/", "/")
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		addDrive("Home ("+filepath.Base(home)+")", home)
	}
	if runtime.GOOS == "darwin" {
		if entries, err := os.ReadDir("/Volumes"); err == nil {
			for _, e := range entries {
				addDrive(e.Name(), filepath.Join("/Volumes", e.Name()))
			}
		}
	} else if runtime.GOOS == "linux" {
		for _, base := range []string{"/mnt", "/media"} {
			if entries, err := os.ReadDir(base); err == nil {
				for _, e := range entries {
					if e.IsDir() {
						addDrive(e.Name(), filepath.Join(base, e.Name()))
					}
				}
			}
		}
	}
	if drives == nil {
		drives = []FileSystemEntryInfo{}
	}
	httpx.JSON(w, http.StatusOK, drives)
}

func (h *Handler) handleDirectoryContents(w http.ResponseWriter, r *http.Request) {
	q := httpx.ParseQuery(r)
	rawPath := strings.TrimSpace(q.Get("path"))
	if rawPath == "" {
		httpx.Error(w, http.StatusBadRequest, "path is required")
		return
	}
	cleanPath := filepath.Clean(rawPath)
	if !filepath.IsAbs(cleanPath) {
		httpx.Error(w, http.StatusBadRequest, "absolute path is required")
		return
	}

	incFilesPtr := q.Bool("includeFiles")
	incDirsPtr := q.Bool("includeDirectories")
	includeFiles := false
	includeDirs := false
	if incFilesPtr == nil && incDirsPtr == nil {
		includeFiles = true
		includeDirs = true
	} else {
		if incFilesPtr != nil {
			includeFiles = *incFilesPtr
		}
		if incDirsPtr != nil {
			includeDirs = *incDirsPtr
		}
	}

	info, err := os.Stat(cleanPath)
	if err != nil {
		if os.IsNotExist(err) {
			httpx.Error(w, http.StatusNotFound, "directory not found")
			return
		}
		httpx.Error(w, http.StatusForbidden, err.Error())
		return
	}
	if !info.IsDir() {
		httpx.Error(w, http.StatusBadRequest, "path is not a directory")
		return
	}

	entries, err := os.ReadDir(cleanPath)
	if err != nil {
		httpx.Error(w, http.StatusForbidden, err.Error())
		return
	}

	var dirs, files []FileSystemEntryInfo
	for _, entry := range entries {
		name := entry.Name()
		fullPath := filepath.Join(cleanPath, name)
		isDir := entry.IsDir()
		if !isDir && entry.Type()&os.ModeSymlink != 0 {
			if st, err := os.Stat(fullPath); err == nil && st.IsDir() {
				isDir = true
			}
		}
		if isDir {
			if !includeDirs {
				continue
			}
			dirs = append(dirs, FileSystemEntryInfo{
				Name: name,
				Path: fullPath,
				Type: "Directory",
			})
		} else {
			if !includeFiles {
				continue
			}
			files = append(files, FileSystemEntryInfo{
				Name: name,
				Path: fullPath,
				Type: "File",
			})
		}
	}

	sort.Slice(dirs, func(i, j int) bool {
		return strings.ToLower(dirs[i].Name) < strings.ToLower(dirs[j].Name)
	})
	sort.Slice(files, func(i, j int) bool {
		return strings.ToLower(files[i].Name) < strings.ToLower(files[j].Name)
	})

	out := make([]FileSystemEntryInfo, 0, len(dirs)+len(files))
	out = append(out, dirs...)
	out = append(out, files...)
	if len(out) > maxDirectoryEntries {
		out = out[:maxDirectoryEntries]
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (h *Handler) handleParentPath(w http.ResponseWriter, r *http.Request) {
	q := httpx.ParseQuery(r)
	rawPath := strings.TrimSpace(q.Get("path"))
	if rawPath == "" {
		httpx.JSON(w, http.StatusOK, "")
		return
	}
	clean := filepath.Clean(rawPath)
	if !filepath.IsAbs(clean) {
		httpx.Error(w, http.StatusBadRequest, "absolute path is required")
		return
	}
	parent := filepath.Dir(clean)
	if parent == clean {
		httpx.JSON(w, http.StatusOK, "")
		return
	}
	httpx.JSON(w, http.StatusOK, parent)
}

func (h *Handler) handleDefaultDirectoryBrowser(w http.ResponseWriter, r *http.Request) {
	p := "/"
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		p = home
	}
	httpx.JSON(w, http.StatusOK, DefaultDirectoryBrowserInfo{Path: p})
}

func (h *Handler) handleNetworkShares(w http.ResponseWriter, r *http.Request) {
	httpx.JSON(w, http.StatusOK, []FileSystemEntryInfo{})
}

func (h *Handler) handleValidatePath(w http.ResponseWriter, r *http.Request) {
	var body ValidatePathRequest
	if err := httpx.DecodeJSONLimit(r, &body, 0); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}
	p := strings.TrimSpace(body.Path)
	if p == "" {
		httpx.Error(w, http.StatusBadRequest, "path is required")
		return
	}
	clean := filepath.Clean(p)
	if !filepath.IsAbs(clean) {
		httpx.Error(w, http.StatusBadRequest, "absolute path is required")
		return
	}
	info, err := os.Stat(clean)
	if err != nil {
		if os.IsNotExist(err) {
			httpx.Error(w, http.StatusNotFound, "path does not exist")
			return
		}
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}

	if body.IsFile != nil {
		if *body.IsFile && info.IsDir() {
			httpx.Error(w, http.StatusBadRequest, "expected file path, got directory")
			return
		}
		if !*body.IsFile && !info.IsDir() {
			httpx.Error(w, http.StatusBadRequest, "expected directory path, got file")
			return
		}
	}

	if body.ValidateWritable {
		if info.IsDir() {
			probe, err := os.CreateTemp(clean, ".ambra-write-check-*")
			if err != nil {
				httpx.Error(w, http.StatusForbidden, "path is not writable")
				return
			}
			probeName := probe.Name()
			_ = probe.Close()
			_ = os.Remove(probeName)
		} else {
			if !info.Mode().IsRegular() {
				httpx.Error(w, http.StatusBadRequest, "path is not a regular file")
				return
			}
			f, err := os.OpenFile(clean, os.O_WRONLY|os.O_APPEND, 0)
			if err != nil {
				httpx.Error(w, http.StatusForbidden, "path is not writable")
				return
			}
			_ = f.Close()
		}
	}

	httpx.NoContent(w)
}

func passthrough(next http.Handler) http.Handler { return next }
