package scanner

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// isPathWithinRoot resolves symlinks on both root and target and returns true iff
// the resolved target path is equal to or strictly inside the resolved root directory.
func isPathWithinRoot(root, target string) bool {
	if strings.TrimSpace(root) == "" || strings.TrimSpace(target) == "" {
		return false
	}
	realRoot, err := filepath.EvalSymlinks(filepath.Clean(root))
	if err != nil {
		realRoot, err = filepath.Abs(filepath.Clean(root))
		if err != nil {
			return false
		}
	}
	realTarget, err := filepath.EvalSymlinks(filepath.Clean(target))
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(realRoot, realTarget)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel))
}

// countSymlinkHopsUnderRoot counts how many path components strictly beneath root
// up to and including target are symbolic links.
func countSymlinkHopsUnderRoot(root, target string) int {
	cleanRoot := filepath.Clean(strings.TrimSpace(root))
	cleanTarget := filepath.Clean(strings.TrimSpace(target))
	if cleanRoot == "" || cleanTarget == "" {
		return 0
	}
	rel, err := filepath.Rel(cleanRoot, cleanTarget)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		if linfo, lerr := os.Lstat(cleanTarget); lerr == nil && linfo.Mode()&os.ModeSymlink != 0 {
			return 1
		}
		return 0
	}
	hops := 0
	cur := cleanRoot
	for _, part := range strings.Split(filepath.ToSlash(rel), "/") {
		if part == "" || part == "." {
			continue
		}
		cur = filepath.Join(cur, part)
		if linfo, lerr := os.Lstat(cur); lerr == nil && linfo.Mode()&os.ModeSymlink != 0 {
			hops++
		}
	}
	return hops
}

// isSymlinkAllowedInRoot validates whether target complies with policy and, when target
// is a symbolic link, verifies via EvalSymlinks that its destination stays inside root.
func isSymlinkAllowedInRoot(root, target string, policy SymlinkPolicy) (allowed bool, isSymlink bool) {
	linfo, err := os.Lstat(target)
	if err != nil {
		return false, false
	}
	if linfo.Mode()&os.ModeSymlink == 0 {
		return true, false
	}
	if policy == SymlinkIgnore {
		return false, true
	}
	return isPathWithinRoot(root, target), true
}

// hasSymlinkComponentUnderRoot reports whether any path component strictly beneath root
// up to and including target is a symbolic link.
func hasSymlinkComponentUnderRoot(root, target string) bool {
	return countSymlinkHopsUnderRoot(root, target) > 0
}

// SymlinkPolicy controls how WalkSafe handles symbolic links during directory traversal.
type SymlinkPolicy int

const (
	// SymlinkFollowDirect follows direct symlinks from real directories (depth 1)
	// while preventing cycles via directory inode tracking. This is the default.
	SymlinkFollowDirect SymlinkPolicy = iota
	// SymlinkIgnore skips all symbolic links (files and directories).
	SymlinkIgnore
	// SymlinkFollowAll follows symbolic links at any depth while preventing cycles
	// via directory inode tracking.
	SymlinkFollowAll
)

// WalkSafe traverses root safely according to policy, invoking fn for every visited
// directory and file (excluding root itself only when root is unreadable).
// Directory cycles introduced by symlinks are guaranteed not to loop infinitely:
// visited directories are deduplicated by Unix (Dev, Ino) with a real-path fallback.
func WalkSafe(root string, policy SymlinkPolicy, fn func(path string, info os.FileInfo) error) error {
	return WalkSafeWithStartDepth(root, 0, policy, fn)
}

// WalkSafeWithStartDepth behaves like WalkSafe while starting symlink depth tracking
// at startDepth (e.g. relative hop count from library root during incremental ScanPath).
func WalkSafeWithStartDepth(root string, startDepth int, policy SymlinkPolicy, fn func(path string, info os.FileInfo) error) error {
	return walkSafeInternal("", root, startDepth, policy, fn)
}

// WalkSafeWithinRoot traverses walkRoot while enforcing that every symbolic link encountered
// resolves within boundaryRoot (when boundaryRoot is non-empty).
func WalkSafeWithinRoot(boundaryRoot, walkRoot string, startDepth int, policy SymlinkPolicy, fn func(path string, info os.FileInfo) error) error {
	return walkSafeInternal(boundaryRoot, walkRoot, startDepth, policy, fn)
}

func walkSafeInternal(boundaryRoot, root string, startDepth int, policy SymlinkPolicy, fn func(path string, info os.FileInfo) error) error {
	if fn == nil {
		return nil
	}
	if startDepth < 0 {
		startDepth = 0
	}
	root = filepath.Clean(root)
	cleanBoundary := strings.TrimSpace(boundaryRoot)
	if cleanBoundary != "" {
		cleanBoundary = filepath.Clean(cleanBoundary)
	}

	var rootInfo os.FileInfo
	var err error
	if policy == SymlinkIgnore {
		rootInfo, err = os.Lstat(root)
		if err != nil {
			return err
		}
		if rootInfo.Mode()&os.ModeSymlink != 0 {
			return nil
		}
	} else {
		if cleanBoundary != "" {
			if linfo, lerr := os.Lstat(root); lerr == nil && linfo.Mode()&os.ModeSymlink != 0 {
				if !isPathWithinRoot(cleanBoundary, root) {
					return nil
				}
			}
		}
		rootInfo, err = os.Stat(root)
		if err != nil {
			return err
		}
	}

	if err := fn(root, rootInfo); err != nil {
		if errors.Is(err, filepath.SkipDir) {
			return nil
		}
		return err
	}
	if !rootInfo.IsDir() {
		return nil
	}

	visited := map[string]struct{}{
		dirIdentity(root, rootInfo): {},
	}
	return walkDirSafe(cleanBoundary, root, startDepth, policy, visited, fn)
}

func walkDirSafe(boundaryRoot, dir string, symlinkDepth int, policy SymlinkPolicy, visited map[string]struct{}, fn func(path string, info os.FileInfo) error) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		fullPath := filepath.Join(dir, entry.Name())
		isSymlink := entry.Type()&os.ModeSymlink != 0

		var info os.FileInfo
		if isSymlink {
			if policy == SymlinkIgnore {
				continue
			}
			if policy == SymlinkFollowDirect && symlinkDepth >= 1 {
				continue
			}
			if boundaryRoot != "" && !isPathWithinRoot(boundaryRoot, fullPath) {
				continue
			}
			info, err = os.Stat(fullPath)
			if err != nil {
				// Broken or unreadable symlink: skip without aborting the walk.
				continue
			}
		} else {
			info, err = entry.Info()
			if err != nil {
				continue
			}
		}

		if info.IsDir() {
			id := dirIdentity(fullPath, info)
			if _, seen := visited[id]; seen {
				continue
			}
			visited[id] = struct{}{}

			err = fn(fullPath, info)
			if err != nil {
				if errors.Is(err, filepath.SkipDir) {
					continue
				}
				return err
			}

			nextDepth := symlinkDepth
			if isSymlink {
				nextDepth++
			}
			if err := walkDirSafe(boundaryRoot, fullPath, nextDepth, policy, visited, fn); err != nil {
				if errors.Is(err, filepath.SkipDir) {
					continue
				}
				return err
			}
			continue
		}

		if err := fn(fullPath, info); err != nil {
			if errors.Is(err, filepath.SkipDir) {
				return nil
			}
			return err
		}
	}
	return nil
}

// dirIdentity returns a unique identifier for a directory based on its (Dev, Ino)
// on Unix systems, falling back to EvalSymlinks real path when Stat_t is unavailable.
func dirIdentity(path string, info os.FileInfo) string {
	if id, ok := dirInodeIdentity(info); ok {
		return id
	}
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return "path:" + filepath.Clean(resolved)
	}
	return "path:" + filepath.Clean(path)
}
