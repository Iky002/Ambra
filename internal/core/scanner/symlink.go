package scanner

import (
	"errors"
	"os"
	"path/filepath"
)

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
	if fn == nil {
		return nil
	}
	root = filepath.Clean(root)

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
	return walkDirSafe(root, 0, policy, visited, fn)
}

func walkDirSafe(dir string, symlinkDepth int, policy SymlinkPolicy, visited map[string]struct{}, fn func(path string, info os.FileInfo) error) error {
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
			if err := walkDirSafe(fullPath, nextDepth, policy, visited, fn); err != nil {
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
