//go:build windows

package sandbox

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"unsafe"
)

var getShortPathNameForTest = syscall.NewLazyDLL("kernel32.dll").NewProc("GetShortPathNameW")

func TestCanonicalMappedDirectoryAcceptsDOSShortPath(t *testing.T) {
	var expected string
	var shortPath string
	for _, candidate := range []string{t.TempDir(), os.Getenv("ProgramFiles")} {
		resolved, err := filepath.EvalSymlinks(candidate)
		if err != nil {
			continue
		}
		short, err := windowsShortPathForTest(resolved)
		if err == nil && !strings.EqualFold(short, resolved) {
			expected = resolved
			shortPath = short
			break
		}
	}
	if shortPath == "" {
		t.Skip("no directory with a distinct DOS short path is available")
	}
	canonical, err := canonicalMappedDirectory(shortPath)
	if err != nil {
		t.Fatalf("canonicalMappedDirectory(%q): %v", shortPath, err)
	}
	if !strings.EqualFold(canonical, filepath.Clean(expected)) {
		t.Fatalf("canonical short directory = %q, want %q", canonical, expected)
	}
}

func windowsShortPathForTest(path string) (string, error) {
	pointer, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return "", err
	}
	buffer := make([]uint16, 512)
	for {
		length, _, callErr := getShortPathNameForTest.Call(
			uintptr(unsafe.Pointer(pointer)),
			uintptr(unsafe.Pointer(&buffer[0])),
			uintptr(len(buffer)),
		)
		if length == 0 {
			return "", fmt.Errorf("GetShortPathNameW: %w", callErr)
		}
		if length < uintptr(len(buffer)) {
			return filepath.Clean(syscall.UTF16ToString(buffer[:length])), nil
		}
		buffer = make([]uint16, int(length)+1)
	}
}
