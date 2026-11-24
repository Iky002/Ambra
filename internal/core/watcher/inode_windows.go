//go:build windows

package watcher

import "os"

func dirInodeKey(_ os.FileInfo) (string, bool) {
	return "", false
}
