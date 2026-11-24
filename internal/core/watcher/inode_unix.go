//go:build !windows

package watcher

import (
	"fmt"
	"os"
	"syscall"
)

func dirInodeKey(info os.FileInfo) (string, bool) {
	if info == nil {
		return "", false
	}
	if st, ok := info.Sys().(*syscall.Stat_t); ok && st != nil {
		return fmt.Sprintf("ino:%d:%d", st.Dev, st.Ino), true
	}
	return "", false
}
