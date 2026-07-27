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

func TestResolveProvisioningDeduplicatesDOSShortWorkspacePath(t *testing.T) {
	root := t.TempDir()
	projects := filepath.Join(root, "Projects With Long Name")
	project := createWorkspaceFixture(t, projects, "Workspace With Long Name")
	shortProject, err := windowsShortPathForTest(project)
	if err != nil || strings.EqualFold(shortProject, project) {
		t.Skipf("no distinct DOS short path is available: %v", err)
	}
	defaults := filepath.Join(root, "defaults")
	global := filepath.Join(root, "global")
	if err := os.MkdirAll(defaults, 0o700); err != nil {
		t.Fatal(err)
	}
	writeWorkspaceDiscoveryConfig(t, global, &workspaceDiscoveryConfiguration{Root: projects, Exclude: []string{}}, map[string]string{"custom": shortProject})

	plan, err := resolveProvisioningAt(filepath.Join(project, "src"), global, defaults)
	if err != nil {
		t.Fatalf("resolveProvisioningAt: %v", err)
	}
	if len(plan.Workspaces) != 1 || plan.Workspaces[0].Name != "custom" || !plan.Workspaces[0].Active {
		t.Fatalf("DOS short-path workspaces = %#v", plan.Workspaces)
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
