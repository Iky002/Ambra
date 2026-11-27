package sqlite

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var blockedSystemLibraryDirs = []string{
	"/etc",
	"/proc",
	"/sys",
	"/dev",
	"/boot",
	"/root",
	"/bin",
	"/sbin",
	"/usr",
	"/lib",
	"/lib64",
	"/var/run",
	"/run",
	"/private/etc",
	"/private/var/run",
}

// ValidateLibraryMountPath validates a candidate media library directory path.
// It requires a non-empty absolute path free of control characters, resolves symlinks
// via EvalSymlinks (on the existing path or nearest existing ancestor + remaining suffix),
// and rejects filesystem roots, OS system directories, and any path equal to, parent of,
// or inside dataDir.
func ValidateLibraryMountPath(rawPath, dataDir string) (string, error) {
	for _, r := range rawPath {
		if r < 0x20 || r == 0x7f {
			return "", errors.New("library path contains invalid control characters")
		}
	}
	trimmed := strings.TrimSpace(rawPath)
	if trimmed == "" {
		return "", errors.New("library path is required")
	}
	if trimmed == "." || trimmed == ".." || isWindowsDriveRoot(trimmed) {
		return "", errors.New("library path must be a non-root absolute path")
	}
	if !filepath.IsAbs(trimmed) {
		return "", errors.New("library path must be an absolute path")
	}

	cleaned := filepath.Clean(trimmed)
	if isFilesystemRoot(cleaned) {
		return "", errors.New("library path cannot be the filesystem root")
	}
	if isBlockedSystemDir(cleaned) {
		return "", fmt.Errorf("library path %q points to a restricted system directory", rawPath)
	}

	resolved, err := resolvePathWithExistingAncestor(cleaned)
	if err != nil {
		return "", fmt.Errorf("invalid library path: %w", err)
	}
	if isFilesystemRoot(resolved) {
		return "", errors.New("library path cannot resolve to the filesystem root")
	}
	if isBlockedSystemDir(resolved) {
		return "", fmt.Errorf("library path %q resolves to a restricted system directory", rawPath)
	}

	if trimmedData := strings.TrimSpace(dataDir); trimmedData != "" {
		absData, err := filepath.Abs(trimmedData)
		if err != nil {
			return "", fmt.Errorf("invalid server data directory: %w", err)
		}
		cleanData := filepath.Clean(absData)
		if pathsOverlapHierarchy(cleaned, cleanData) {
			return "", errors.New("library path cannot overlap with the server data directory")
		}
		resolvedData, err := resolvePathWithExistingAncestor(cleanData)
		if err != nil {
			return "", fmt.Errorf("invalid server data directory: %w", err)
		}
		if pathsOverlapHierarchy(resolved, resolvedData) {
			return "", errors.New("library path cannot overlap with the server data directory")
		}
	}

	return resolved, nil
}

func resolvePathWithExistingAncestor(absCleanPath string) (string, error) {
	if resolved, err := filepath.EvalSymlinks(absCleanPath); err == nil {
		return filepath.Clean(resolved), nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}

	cur := absCleanPath
	var missing []string
	for {
		parent := filepath.Dir(cur)
		base := filepath.Base(cur)
		if parent == cur {
			break
		}
		missing = append(missing, base)
		cur = parent
		resolvedAncestor, err := filepath.EvalSymlinks(cur)
		if err == nil {
			out := resolvedAncestor
			for i := len(missing) - 1; i >= 0; i-- {
				out = filepath.Join(out, missing[i])
			}
			return filepath.Clean(out), nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
	}
	return filepath.Clean(absCleanPath), nil
}

func isFilesystemRoot(cleanPath string) bool {
	if cleanPath == "/" || cleanPath == `\` || cleanPath == "." {
		return true
	}
	if filepath.Dir(cleanPath) == cleanPath {
		return true
	}
	return isWindowsDriveRoot(cleanPath)
}

func isWindowsDriveRoot(p string) bool {
	if len(p) < 2 {
		return false
	}
	c := p[0]
	if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')) || p[1] != ':' {
		return false
	}
	rest := strings.Trim(p[2:], `/\`)
	return rest == ""
}

func isBlockedSystemDir(cleanPath string) bool {
	slash := filepath.ToSlash(cleanPath)
	for _, sysDir := range blockedSystemLibraryDirs {
		if slash == sysDir || strings.HasPrefix(slash, sysDir+"/") {
			return true
		}
	}
	return false
}

func pathsOverlapHierarchy(a, b string) bool {
	a = filepath.Clean(a)
	b = filepath.Clean(b)
	if a == b {
		return true
	}
	sep := string(filepath.Separator)
	aPrefix := a
	if !strings.HasSuffix(aPrefix, sep) {
		aPrefix += sep
	}
	bPrefix := b
	if !strings.HasSuffix(bPrefix, sep) {
		bPrefix += sep
	}
	// Reject if a is inside b OR a is a parent directory containing b.
	return strings.HasPrefix(a, bPrefix) || strings.HasPrefix(b, aPrefix)
}
