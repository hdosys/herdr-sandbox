//go:build windows

package sandbox

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestCleanInstallerDataPreservesLockedCacheAndRemovesRequiredState(t *testing.T) {
	root := t.TempDir()
	paths := installerCleanPaths{
		DataDirectory:          filepath.Join(root, "local", applicationName),
		ConfigurationDirectory: filepath.Join(root, "roaming", applicationName),
		CacheDirectory:         filepath.Join(root, "cache"),
		InstallDirectory:       filepath.Join(root, "install"),
		UserHome:               filepath.Join(root, "home"),
	}
	locked := filepath.Join(paths.CacheDirectory, "go-build.exe")
	writeUninstallFixture(t, locked, "active tool")
	writeUninstallFixture(t, filepath.Join(paths.DataDirectory, "runs", "state.json"), "state")

	name, err := syscall.UTF16PtrFromString(locked)
	if err != nil {
		t.Fatal(err)
	}
	handle, err := syscall.CreateFile(name, syscall.GENERIC_READ, 0, nil, syscall.OPEN_EXISTING, syscall.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer syscall.CloseHandle(handle)

	if err := cleanInstallerDataAt(context.Background(), paths, false); err != nil {
		t.Fatalf("locked disposable cache blocked uninstall cleanup: %v", err)
	}
	if _, err := os.Stat(locked); err != nil {
		t.Fatalf("locked cache file was not preserved: %v", err)
	}
	if _, err := os.Lstat(paths.DataDirectory); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("required machine-local state remains: %v", err)
	}
}

func TestCleanInstallerDataPreservesLockedMachineStateAndRemovesUnlockedCache(t *testing.T) {
	root := t.TempDir()
	paths := installerCleanPaths{
		DataDirectory:          filepath.Join(root, "local", applicationName),
		ConfigurationDirectory: filepath.Join(root, "roaming", applicationName),
		CacheDirectory:         filepath.Join(root, "cache"),
		InstallDirectory:       filepath.Join(root, "install"),
		UserHome:               filepath.Join(root, "home"),
	}
	locked := filepath.Join(paths.DataDirectory, "runs", "go-build.exe")
	writeUninstallFixture(t, locked, "active tool")
	writeUninstallFixture(t, filepath.Join(paths.CacheDirectory, "payload.zip"), "cache")

	name, err := syscall.UTF16PtrFromString(locked)
	if err != nil {
		t.Fatal(err)
	}
	handle, err := syscall.CreateFile(name, syscall.GENERIC_READ, 0, nil, syscall.OPEN_EXISTING, syscall.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer syscall.CloseHandle(handle)

	if err := cleanInstallerDataAt(context.Background(), paths, false); err != nil {
		t.Fatalf("locked machine-local state blocked uninstall cleanup: %v", err)
	}
	if _, err := os.Stat(locked); err != nil {
		t.Fatalf("locked machine-local state was not preserved: %v", err)
	}
	if _, err := os.Lstat(paths.CacheDirectory); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unlocked cache remains: %v", err)
	}
}
