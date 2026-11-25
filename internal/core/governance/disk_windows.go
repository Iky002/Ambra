//go:build windows

package governance

import (
	"math"
	"syscall"
	"unsafe"
)

var (
	kernel32DLL         = syscall.NewLazyDLL("kernel32.dll")
	procGetDiskFreeExW  = kernel32DLL.NewProc("GetDiskFreeSpaceExW")
)

func collectDiskHealth(path string) DiskHealth {
	out := DiskHealth{Path: path}
	if path == "" {
		return out
	}
	ptr, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return out
	}
	var freeBytesAvailable, totalBytes, totalFreeBytes uint64
	r1, _, _ := procGetDiskFreeExW.Call(
		uintptr(unsafe.Pointer(ptr)),
		uintptr(unsafe.Pointer(&freeBytesAvailable)),
		uintptr(unsafe.Pointer(&totalBytes)),
		uintptr(unsafe.Pointer(&totalFreeBytes)),
	)
	if r1 == 0 {
		return out
	}
	out.TotalBytes = totalBytes
	out.FreeBytes = totalFreeBytes
	out.AvailableBytes = freeBytesAvailable
	if out.TotalBytes > 0 && out.TotalBytes >= out.FreeBytes {
		used := out.TotalBytes - out.FreeBytes
		pct := float64(used) / float64(out.TotalBytes) * 100
		out.UsedPercent = math.Round(pct*10) / 10
	}
	return out
}
