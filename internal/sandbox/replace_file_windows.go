//go:build windows

package sandbox

import (
	"errors"
	"fmt"
	"syscall"
	"unsafe"
)

var replaceFileW = syscall.NewLazyDLL("kernel32.dll").NewProc("ReplaceFileW")

func replaceFileAtomically(target, replacement, backup string) error {
	targetPointer, err := syscall.UTF16PtrFromString(target)
	if err != nil {
		return fmt.Errorf("encode replaced file path: %w", err)
	}
	replacementPointer, err := syscall.UTF16PtrFromString(replacement)
	if err != nil {
		return fmt.Errorf("encode replacement file path: %w", err)
	}
	var backupPointer *uint16
	if backup != "" {
		backupPointer, err = syscall.UTF16PtrFromString(backup)
		if err != nil {
			return fmt.Errorf("encode replacement backup path: %w", err)
		}
	}
	result, _, callErr := replaceFileW.Call(
		uintptr(unsafe.Pointer(targetPointer)),
		uintptr(unsafe.Pointer(replacementPointer)),
		uintptr(unsafe.Pointer(backupPointer)),
		0,
		0,
		0,
	)
	if result == 0 {
		if callErr != nil && callErr != syscall.Errno(0) {
			return fmt.Errorf("ReplaceFileW: %w", callErr)
		}
		return errors.New("ReplaceFileW failed without a Windows error")
	}
	return nil
}
