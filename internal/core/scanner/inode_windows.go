//go:build windows

package scanner

import "os"

func dirInodeIdentity(_ os.FileInfo) (string, bool) {
	return "", false
}
