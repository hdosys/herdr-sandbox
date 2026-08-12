package sandbox

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRemoveManagedSSHIncludePreservesUnrelatedConfiguration(t *testing.T) {
	managedPath := filepath.Join(`C:\Users\Test User`, "AppData", "Local", applicationName, "ssh", "config")
	for _, newline := range []string{"\n", "\r\n"} {
		t.Run(strings.ReplaceAll(newline, "\r", "CR"), func(t *testing.T) {
			unrelated := "Host work" + newline + "    HostName work.example" + newline
			existing := strings.Join([]string{
				managedSSHIncludeStart,
				"Include " + quoteSSHPath(managedPath),
				managedSSHIncludeEnd,
				"",
				unrelated,
			}, newline)
			updated, changed, err := removeManagedSSHInclude(existing, managedPath)
			if err != nil || !changed || updated != unrelated {
				t.Fatalf("removeManagedSSHInclude = %q, %t, %v; want unrelated config", updated, changed, err)
			}
		})
	}

	unrelated := "Host work\n"
	updated, changed, err := removeManagedSSHInclude(unrelated, managedPath)
	if err != nil || changed || updated != unrelated {
		t.Fatalf("absent managed include = %q, %t, %v", updated, changed, err)
	}
}

func TestRemoveManagedSSHIncludeRejectsChangedOwnedBlock(t *testing.T) {
	managedPath := `C:\state\ssh\config`
	for name, existing := range map[string]string{
		"not first":  "Host work\n" + managedSSHIncludeStart + "\n" + managedSSHIncludeEnd + "\n",
		"wrong path": managedSSHIncludeStart + "\nInclude \"C:/other/config\"\n" + managedSSHIncludeEnd + "\n",
		"duplicate":  managedSSHIncludeStart + "\nInclude " + quoteSSHPath(managedPath) + "\n" + managedSSHIncludeEnd + "\n" + managedSSHIncludeStart + "\n" + managedSSHIncludeEnd,
	} {
		t.Run(name, func(t *testing.T) {
			if _, _, err := removeManagedSSHInclude(existing, managedPath); err == nil {
				t.Fatal("changed managed SSH block unexpectedly validated")
			}
		})
	}
}

func TestInstallerCleanupNeverOwnsSandboxTermination(t *testing.T) {
	source, err := os.ReadFile("uninstall.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{
		"downAtWithExecutable",
		"ensureNoRunningSandboxProcesses",
		"runningSandboxProcesses(",
		"windowsSandboxExecutable(",
		"expectedWindowsSandboxExecutable(",
	} {
		if strings.Contains(string(source), forbidden) {
			t.Errorf("installer cleanup retains Sandbox lifecycle path %q", forbidden)
		}
	}
}

func TestCleanInstallerDataWithLockHeldDoesNotReacquireLifecycleLock(t *testing.T) {
	paths := installerCleanPaths{
		DataDirectory:          filepath.Join(t.TempDir(), "data"),
		ConfigurationDirectory: filepath.Join(t.TempDir(), "config"),
		CacheDirectory:         filepath.Join(t.TempDir(), "cache"),
		InstallDirectory:       filepath.Join(t.TempDir(), "install"),
		UserHome:               t.TempDir(),
	}
	for _, path := range []string{paths.DataDirectory, paths.ConfigurationDirectory, paths.CacheDirectory, paths.InstallDirectory} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	release, err := acquireLifecycleLock(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := cleanInstallerDataWithLockHeldAt(ctx, paths, false); err != nil {
		t.Fatalf("cleanup under parent-held lifecycle lock: %v", err)
	}
}

func TestCleanInstallerDataRemovesOwnedStateAndPreservesProjectFiles(t *testing.T) {
	root := t.TempDir()
	paths := installerCleanPaths{
		DataDirectory:          filepath.Join(root, "local", applicationName),
		ConfigurationDirectory: filepath.Join(root, "roaming", applicationName),
		CacheDirectory:         filepath.Join(root, "cache"),
		InstallDirectory:       filepath.Join(root, "install"),
		UserHome:               filepath.Join(root, "home"),
		Configuration:          globalConfiguration{Workspaces: map[string]string{}},
	}
	for _, file := range []string{
		filepath.Join(paths.DataDirectory, "identity", "id_ed25519"),
		filepath.Join(paths.DataDirectory, "runs", "20260730-120000-abcdef12", "status", "ready.json"),
		filepath.Join(paths.ConfigurationDirectory, globalConfigurationName),
		filepath.Join(paths.ConfigurationDirectory, userProvisioningName),
		filepath.Join(paths.CacheDirectory, "packages", "payload.bin"),
		filepath.Join(paths.InstallDirectory, "unrelated.keep"),
	} {
		writeUninstallFixture(t, file, "fixture")
	}
	projectProfile := filepath.Join(root, "project", projectConfigurationName, projectProvisioningName)
	writeUninstallFixture(t, projectProfile, "project-owned")
	managedPath := filepath.Join(paths.DataDirectory, "ssh", "config")
	unrelatedSSH := "Host work\n    HostName work.example\n"
	userSSH := strings.Join([]string{
		managedSSHIncludeStart,
		"Include " + quoteSSHPath(managedPath),
		managedSSHIncludeEnd,
		"",
		unrelatedSSH,
	}, "\n")
	writeUninstallFixture(t, filepath.Join(paths.UserHome, ".ssh", "config"), userSSH)

	if err := cleanInstallerDataAt(context.Background(), paths, true); err != nil {
		t.Fatalf("cleanInstallerDataAt: %v", err)
	}
	for _, path := range []string{paths.DataDirectory, paths.ConfigurationDirectory, paths.CacheDirectory} {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("owned root remains %s: %v", path, err)
		}
	}
	for _, path := range []string{projectProfile, filepath.Join(paths.InstallDirectory, "unrelated.keep")} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("unowned file was removed %s: %v", path, err)
		}
	}
	updatedSSH, err := os.ReadFile(filepath.Join(paths.UserHome, ".ssh", "config"))
	if err != nil || string(updatedSSH) != unrelatedSSH {
		t.Fatalf("user SSH config = %q, %v", updatedSSH, err)
	}
}

func TestCleanInstallerDataDefaultPreservesConfigurationAndRemovesAllState(t *testing.T) {
	root := t.TempDir()
	paths := installerCleanPaths{
		DataDirectory:          filepath.Join(root, "local", applicationName),
		ConfigurationDirectory: filepath.Join(root, "roaming", applicationName),
		CacheDirectory:         filepath.Join(root, "cache"),
		InstallDirectory:       filepath.Join(root, "install"),
		UserHome:               filepath.Join(root, "home"),
	}
	for _, path := range []string{
		filepath.Join(paths.DataDirectory, "unknown-state", "state.keep"),
		filepath.Join(paths.CacheDirectory, "unknown-cache", "cache.keep"),
		filepath.Join(paths.ConfigurationDirectory, globalConfigurationName),
		filepath.Join(paths.ConfigurationDirectory, userProvisioningName),
	} {
		writeUninstallFixture(t, path, "keep")
	}
	if err := cleanInstallerDataAt(context.Background(), paths, false); err != nil {
		t.Fatalf("default cleanInstallerDataAt: %v", err)
	}
	for _, path := range []string{paths.DataDirectory, paths.CacheDirectory} {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("state root remains after default uninstall %s: %v", path, err)
		}
	}
	for _, path := range []string{
		filepath.Join(paths.ConfigurationDirectory, globalConfigurationName),
		filepath.Join(paths.ConfigurationDirectory, userProvisioningName),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("default uninstall removed user configuration %s: %v", path, err)
		}
	}
}

func TestCleanInstallerDataPreflightsBeforeDeletion(t *testing.T) {
	root := t.TempDir()
	paths := installerCleanPaths{
		DataDirectory:          filepath.Join(root, "local", applicationName),
		ConfigurationDirectory: filepath.Join(root, "roaming", applicationName),
		CacheDirectory:         filepath.Join(root, "cache"),
		InstallDirectory:       filepath.Join(root, "install"),
		UserHome:               filepath.Join(root, "home"),
	}
	for _, path := range []string{paths.DataDirectory, paths.ConfigurationDirectory, paths.CacheDirectory} {
		writeUninstallFixture(t, filepath.Join(path, "keep.txt"), "keep")
	}
	writeUninstallFixture(t, filepath.Join(paths.UserHome, ".ssh", "config"), managedSSHIncludeStart+"\n")
	err := cleanInstallerDataAt(context.Background(), paths, false)
	if err == nil {
		t.Fatalf("preflight error = %v", err)
	}
	for _, path := range []string{paths.DataDirectory, paths.ConfigurationDirectory, paths.CacheDirectory} {
		if _, err := os.Stat(filepath.Join(path, "keep.txt")); err != nil {
			t.Fatalf("state changed after failed preflight: %s: %v", path, err)
		}
	}
}

func TestCleanInstallerDataPreservesNestedReparseCacheAndRemovesRequiredState(t *testing.T) {
	root := t.TempDir()
	paths := installerCleanPaths{
		DataDirectory:          filepath.Join(root, "local", applicationName),
		ConfigurationDirectory: filepath.Join(root, "roaming", applicationName),
		CacheDirectory:         filepath.Join(root, "cache"),
		InstallDirectory:       filepath.Join(root, "install"),
		UserHome:               filepath.Join(root, "home"),
	}
	for _, path := range []string{paths.DataDirectory, paths.ConfigurationDirectory, paths.CacheDirectory} {
		writeUninstallFixture(t, filepath.Join(path, "keep.txt"), "keep")
	}
	outside := t.TempDir()
	outsideMarker := filepath.Join(outside, "outside.keep")
	writeUninstallFixture(t, outsideMarker, "outside")
	createTestDirectoryLink(t, filepath.Join(paths.CacheDirectory, "unsafe-link"), outside)
	if err := cleanInstallerDataAt(context.Background(), paths, false); err != nil {
		t.Fatalf("reparse-bearing disposable cache blocked required cleanup: %v", err)
	}
	contents, readErr := os.ReadFile(outsideMarker)
	if readErr != nil || string(contents) != "outside" {
		t.Fatalf("external reparse target changed: %q, %v", contents, readErr)
	}
	if _, err := os.Stat(filepath.Join(paths.CacheDirectory, "keep.txt")); err != nil {
		t.Fatalf("unsafe cache was not preserved: %v", err)
	}
	if _, err := os.Lstat(paths.DataDirectory); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("required machine-local state remains: %v", err)
	}
	if _, err := os.Stat(filepath.Join(paths.ConfigurationDirectory, "keep.txt")); err != nil {
		t.Fatalf("default cleanup removed configuration: %v", err)
	}
}

func TestValidateInstallerRemovalSafetyRejectsMappedAndOwnedOverlap(t *testing.T) {
	root := t.TempDir()
	base := installerCleanPaths{
		DataDirectory:          filepath.Join(root, "local", applicationName),
		ConfigurationDirectory: filepath.Join(root, "roaming", applicationName),
		InstallDirectory:       filepath.Join(root, "install"),
		UserHome:               filepath.Join(root, "home"),
		Configuration:          globalConfiguration{Workspaces: map[string]string{}},
	}
	for name, cache := range map[string]string{
		"configuration": filepath.Join(base.ConfigurationDirectory, "cache"),
		"install":       base.InstallDirectory,
		"workspace":     filepath.Join(root, "project", "cache"),
		"folder mount":  filepath.Join(root, "shared", "cache"),
	} {
		t.Run(name, func(t *testing.T) {
			paths := base
			paths.CacheDirectory = cache
			if name == "workspace" {
				paths.Configuration.Workspaces = map[string]string{"project": filepath.Join(root, "project")}
			}
			if name == "folder mount" {
				paths.Configuration.Mounts = map[string]mountConfiguration{
					"shared": {Path: filepath.Join(root, "shared"), ReadOnly: false},
				}
			}
			if err := validateInstallerRemovalSafety(paths, false); err == nil {
				t.Fatal("unsafe cache overlap unexpectedly validated")
			}
		})
	}
}

func TestValidateInstallerRemovalSafetyAllowsWorkspaceBesideDefaultCache(t *testing.T) {
	root := t.TempDir()
	cacheParent := filepath.Join(root, applicationName)
	paths := installerCleanPaths{
		DataDirectory:          filepath.Join(root, "local", applicationName),
		ConfigurationDirectory: filepath.Join(root, "roaming", applicationName),
		CacheDirectory:         filepath.Join(cacheParent, "cache"),
		DefaultCacheParent:     cacheParent,
		InstallDirectory:       filepath.Join(root, "install"),
		UserHome:               filepath.Join(root, "home"),
		Configuration: globalConfiguration{Workspaces: map[string]string{
			"project": filepath.Join(cacheParent, "project"),
		}},
	}
	if err := validateInstallerRemovalSafety(paths, false); err != nil {
		t.Fatalf("workspace beside default cache was rejected: %v", err)
	}
}

func TestValidateInstallerRemovalSafetyPreservesWorktreeDirectory(t *testing.T) {
	root := t.TempDir()
	paths := installerCleanPaths{
		DataDirectory:          filepath.Join(root, "local", applicationName),
		ConfigurationDirectory: filepath.Join(root, "roaming", applicationName),
		CacheDirectory:         filepath.Join(root, "cache"),
		WorktreeDirectory:      filepath.Join(root, "worktrees"),
		InstallDirectory:       filepath.Join(root, "install"),
		UserHome:               filepath.Join(root, "home"),
		Configuration:          globalConfiguration{Workspaces: map[string]string{}},
	}
	for _, directory := range []string{paths.DataDirectory, paths.ConfigurationDirectory, paths.CacheDirectory, paths.WorktreeDirectory, paths.InstallDirectory, paths.UserHome} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := validateInstallerRemovalSafety(paths, true); err != nil {
		t.Fatalf("dedicated worktreeDirectory was not preserved: %v", err)
	}
	paths.WorktreeDirectory = filepath.Join(paths.CacheDirectory, "worktrees")
	if err := validateInstallerRemovalSafety(paths, false); err == nil || !strings.Contains(err.Error(), "worktreeDirectory") {
		t.Fatalf("unsafe worktreeDirectory error = %v", err)
	}
}

func TestValidateInstallerRemovalSafetyPreservesFolderMounts(t *testing.T) {
	root := t.TempDir()
	base := installerCleanPaths{
		DataDirectory:          filepath.Join(root, "local", applicationName),
		ConfigurationDirectory: filepath.Join(root, "roaming", applicationName),
		CacheDirectory:         filepath.Join(root, "cache"),
		InstallDirectory:       filepath.Join(root, "install"),
		UserHome:               filepath.Join(root, "home"),
		Configuration:          globalConfiguration{Mounts: map[string]mountConfiguration{}},
	}
	tests := []struct {
		name                string
		mount               string
		deleteConfiguration bool
		wantError           bool
	}{
		{name: "machine-local state", mount: filepath.Join(base.DataDirectory, "shared"), wantError: true},
		{name: "configuration preserved", mount: filepath.Join(base.ConfigurationDirectory, "shared")},
		{name: "configuration deleted", mount: filepath.Join(base.ConfigurationDirectory, "shared"), deleteConfiguration: true, wantError: true},
		{name: "separate folder", mount: filepath.Join(root, "shared")},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			paths := base
			paths.Configuration.Mounts = map[string]mountConfiguration{"shared": {Path: test.mount, ReadOnly: false}}
			err := validateInstallerRemovalSafety(paths, test.deleteConfiguration)
			if test.wantError && err == nil {
				t.Fatal("folder mount inside a recursive removal root unexpectedly validated")
			}
			if !test.wantError && err != nil {
				t.Fatalf("safe folder mount was rejected: %v", err)
			}
		})
	}
}

func TestValidateInstallerRemovalSafetyRejectsFolderMountAlias(t *testing.T) {
	root := t.TempDir()
	cache := filepath.Join(root, "cache")
	if err := os.MkdirAll(cache, 0o700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(root, "cache-alias")
	createTestDirectoryLink(t, alias, cache)
	paths := installerCleanPaths{
		DataDirectory:          filepath.Join(root, "local", applicationName),
		ConfigurationDirectory: filepath.Join(root, "roaming", applicationName),
		CacheDirectory:         cache,
		InstallDirectory:       filepath.Join(root, "install"),
		UserHome:               filepath.Join(root, "home"),
		Configuration: globalConfiguration{Mounts: map[string]mountConfiguration{
			"alias": {Path: alias, ReadOnly: true},
		}},
	}
	if err := validateInstallerRemovalSafety(paths, false); err == nil {
		t.Fatal("folder mount alias of the recursive cache root unexpectedly validated")
	}
}

func TestCleanInstallerDataRemovesExactDefaultCacheAndPreservesSibling(t *testing.T) {
	root := t.TempDir()
	cacheParent := filepath.Join(root, applicationName)
	paths := installerCleanPaths{
		DataDirectory:          filepath.Join(root, "local", applicationName),
		ConfigurationDirectory: filepath.Join(root, "roaming", applicationName),
		CacheDirectory:         filepath.Join(cacheParent, "cache"),
		DefaultCacheParent:     cacheParent,
		InstallDirectory:       filepath.Join(root, "install"),
		UserHome:               filepath.Join(root, "home"),
	}
	writeUninstallFixture(t, filepath.Join(paths.CacheDirectory, "payload.bin"), "cache")
	sibling := filepath.Join(cacheParent, "project", "keep.txt")
	writeUninstallFixture(t, sibling, "project")
	if err := cleanInstallerDataAt(context.Background(), paths, false); err != nil {
		t.Fatalf("cleanInstallerDataAt exact cache: %v", err)
	}
	if _, err := os.Lstat(paths.CacheDirectory); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("default cache remains: %v", err)
	}
	if contents, err := os.ReadFile(sibling); err != nil || string(contents) != "project" {
		t.Fatalf("default-cache sibling changed: %q, %v", contents, err)
	}
}

func TestCleanInstallerDataRemovesEmptyDefaultCacheParent(t *testing.T) {
	root := t.TempDir()
	cacheParent := filepath.Join(root, applicationName)
	paths := installerCleanPaths{
		DataDirectory:          filepath.Join(root, "local", applicationName),
		ConfigurationDirectory: filepath.Join(root, "roaming", applicationName),
		CacheDirectory:         filepath.Join(cacheParent, "cache"),
		DefaultCacheParent:     cacheParent,
		InstallDirectory:       filepath.Join(root, "install"),
		UserHome:               filepath.Join(root, "home"),
	}
	writeUninstallFixture(t, filepath.Join(paths.CacheDirectory, "payload.bin"), "cache")
	if err := cleanInstallerDataAt(context.Background(), paths, false); err != nil {
		t.Fatalf("cleanInstallerDataAt empty cache parent: %v", err)
	}
	if _, err := os.Lstat(cacheParent); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("empty default cache parent remains: %v", err)
	}
}

func TestInstallerRemovalRootsConsolidatesNestedCache(t *testing.T) {
	configuration := filepath.Join(t.TempDir(), "config")
	roots := installerRemovalRoots(installerCleanPaths{
		CacheDirectory:         filepath.Join(configuration, "cache"),
		ConfigurationDirectory: configuration,
		DataDirectory:          filepath.Join(t.TempDir(), "data"),
	}, true)
	if len(roots) != 2 {
		t.Fatalf("removal roots = %#v, want consolidated config and data roots", roots)
	}
	for _, root := range roots {
		if strings.EqualFold(root.path, filepath.Join(configuration, "cache")) {
			t.Fatal("nested cache retained as a duplicate removal root")
		}
	}
}

func writeUninstallFixture(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}
