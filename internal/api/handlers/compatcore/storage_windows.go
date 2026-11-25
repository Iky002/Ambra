//go:build windows

package compatcore

import (
	"syscall"
	"unsafe"
)

var (
	kernel32DLL         = syscall.NewLazyDLL("kernel32.dll")
	procGetDiskFreeExW  = kernel32DLL.NewProc("GetDiskFreeSpaceExW")
)

func diskUsage(path string) (free, used uint64, ok bool) {
	ptr, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return 0, 0, false
	}
	var freeBytesAvailable, totalBytes, totalFreeBytes uint64
	r1, _, _ := procGetDiskFreeExW.Call(
		uintptr(unsafe.Pointer(ptr)),
		uintptr(unsafe.Pointer(&freeBytesAvailable)),
		uintptr(unsafe.Pointer(&totalBytes)),
		uintptr(unsafe.Pointer(&totalFreeBytes)),
	)
	if r1 == 0 {
		return 0, 0, false
	}
	free = freeBytesAvailable
	if totalBytes >= free {
		used = totalBytes - free
	}
	return free, used, true
}
