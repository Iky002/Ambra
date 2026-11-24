//go:build !windows

package governance

import (
	"math"
	"syscall"
)

func collectDiskHealth(path string) DiskHealth {
	out := DiskHealth{Path: path}
	if path == "" {
		return out
	}
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return out
	}
	bsize := uint64(st.Bsize)
	out.TotalBytes = st.Blocks * bsize
	out.FreeBytes = st.Bfree * bsize
	out.AvailableBytes = st.Bavail * bsize
	if out.TotalBytes > 0 {
		used := out.TotalBytes - out.FreeBytes
		pct := float64(used) / float64(out.TotalBytes) * 100
		out.UsedPercent = math.Round(pct*10) / 10
	}
	return out
}
