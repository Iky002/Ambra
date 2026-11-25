//go:build !windows

package compatcore

import "syscall"

func diskUsage(path string) (free, used uint64, ok bool) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, 0, false
	}
	bsize := uint64(st.Bsize)
	total := st.Blocks * bsize
	free = st.Bavail * bsize
	if total >= free {
		used = total - free
	}
	return free, used, true
}
