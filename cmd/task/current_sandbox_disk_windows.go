//go:build windows

package main

import (
	"fmt"
	"syscall"
	"unsafe"
)

var getDiskFreeSpaceExW = syscall.NewLazyDLL("kernel32.dll").NewProc("GetDiskFreeSpaceExW")

func currentSandboxAvailableDiskBytes(path string) (uint64, error) {
	pathPointer, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return 0, fmt.Errorf("encode disk path %s: %w", path, err)
	}
	var available uint64
	result, _, callErr := getDiskFreeSpaceExW.Call(
		uintptr(unsafe.Pointer(pathPointer)),
		uintptr(unsafe.Pointer(&available)),
		0,
		0,
	)
	if result == 0 {
		if callErr == syscall.Errno(0) {
			callErr = syscall.EINVAL
		}
		return 0, fmt.Errorf("query available disk space for %s: %w", path, callErr)
	}
	return available, nil
}
