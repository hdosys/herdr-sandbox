//go:build windows

package sandbox

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"
)

var getFinalPathNameByHandle = syscall.NewLazyDLL("kernel32.dll").NewProc("GetFinalPathNameByHandleW")

func fileInfoIsReparsePoint(info os.FileInfo) (bool, error) {
	attributes, ok := info.Sys().(*syscall.Win32FileAttributeData)
	if !ok {
		return false, fmt.Errorf("unexpected Windows file information type %T", info.Sys())
	}
	return attributes.FileAttributes&syscall.FILE_ATTRIBUTE_REPARSE_POINT != 0, nil
}

func mappedDirectoryPhysicalIdentity(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open mapped directory for physical identity: %w", err)
	}
	defer file.Close()

	const volumeNameNT = 0x2
	buffer := make([]uint16, 512)
	for {
		length, _, callErr := getFinalPathNameByHandle.Call(
			file.Fd(),
			uintptr(unsafe.Pointer(&buffer[0])),
			uintptr(len(buffer)),
			volumeNameNT,
		)
		if length == 0 {
			return "", fmt.Errorf("resolve mapped directory physical identity: %w", callErr)
		}
		if length < uintptr(len(buffer)) {
			identity := syscall.UTF16ToString(buffer[:length])
			return strings.ToLower(filepath.Clean(identity)), nil
		}
		buffer = make([]uint16, int(length)+1)
	}
}

func resolvedDirectoryPath(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open directory for final path: %w", err)
	}
	defer file.Close()
	buffer := make([]uint16, 512)
	for {
		length, _, callErr := getFinalPathNameByHandle.Call(
			file.Fd(),
			uintptr(unsafe.Pointer(&buffer[0])),
			uintptr(len(buffer)),
			0,
		)
		if length == 0 {
			return "", fmt.Errorf("resolve directory final path: %w", callErr)
		}
		if length < uintptr(len(buffer)) {
			resolved := syscall.UTF16ToString(buffer[:length])
			if strings.HasPrefix(resolved, `\\?\UNC\`) {
				resolved = `\\` + strings.TrimPrefix(resolved, `\\?\UNC\`)
			} else {
				resolved = strings.TrimPrefix(resolved, `\\?\`)
			}
			return filepath.Clean(resolved), nil
		}
		buffer = make([]uint16, int(length)+1)
	}
}
