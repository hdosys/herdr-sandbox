package sandbox

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
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

func TestCleanInstallerDataRemovesOwnedStateAndPreservesProjectFiles(t *testing.T) {
	root := t.TempDir()
	paths := installerCleanPaths{
		DataDirectory:          filepath.Join(root, "local", applicationName),
		ConfigurationDirectory: filepath.Join(root, "roaming", applicationName),
		CacheDirectory:         filepath.Join(root, "cache"),
		InstallDirectory:       filepath.Join(root, "install"),
		UserHome:               filepath.Join(root, "home"),
		SandboxExecutable:      filepath.Join(root, "WindowsSandbox.exe"),
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

	stopCalls := 0
	stop := func(_ context.Context, dataDirectory, executable string) (DownResult, error) {
		stopCalls++
		if dataDirectory != paths.DataDirectory || executable != paths.SandboxExecutable {
			t.Fatalf("stop paths = %q, %q", dataDirectory, executable)
		}
		for _, path := range []string{paths.DataDirectory, paths.ConfigurationDirectory, paths.CacheDirectory} {
			if _, err := os.Stat(path); err != nil {
				t.Fatalf("owned root was removed before stop: %s: %v", path, err)
			}
		}
		return DownResult{}, nil
	}
	if err := cleanInstallerDataAt(context.Background(), paths, true, stop, noInstallerSandboxProcesses); err != nil {
		t.Fatalf("cleanInstallerDataAt: %v", err)
	}
	if stopCalls != 1 {
		t.Fatalf("stop calls = %d", stopCalls)
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
		SandboxExecutable:      filepath.Join(root, "WindowsSandbox.exe"),
	}
	for _, path := range []string{
		filepath.Join(paths.DataDirectory, "unknown-state", "state.keep"),
		filepath.Join(paths.CacheDirectory, "unknown-cache", "cache.keep"),
		filepath.Join(paths.ConfigurationDirectory, globalConfigurationName),
		filepath.Join(paths.ConfigurationDirectory, userProvisioningName),
	} {
		writeUninstallFixture(t, path, "keep")
	}
	if err := cleanInstallerDataAt(context.Background(), paths, false, func(context.Context, string, string) (DownResult, error) {
		return DownResult{AlreadyStopped: true}, nil
	}, noInstallerSandboxProcesses); err != nil {
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

func TestCleanInstallerDataPreflightsBeforeStopOrDeletion(t *testing.T) {
	root := t.TempDir()
	paths := installerCleanPaths{
		DataDirectory:          filepath.Join(root, "local", applicationName),
		ConfigurationDirectory: filepath.Join(root, "roaming", applicationName),
		CacheDirectory:         filepath.Join(root, "cache"),
		InstallDirectory:       filepath.Join(root, "install"),
		UserHome:               filepath.Join(root, "home"),
		SandboxExecutable:      filepath.Join(root, "WindowsSandbox.exe"),
	}
	for _, path := range []string{paths.DataDirectory, paths.ConfigurationDirectory, paths.CacheDirectory} {
		writeUninstallFixture(t, filepath.Join(path, "keep.txt"), "keep")
	}
	writeUninstallFixture(t, filepath.Join(paths.UserHome, ".ssh", "config"), managedSSHIncludeStart+"\n")
	stopCalled := false
	err := cleanInstallerDataAt(context.Background(), paths, false, func(context.Context, string, string) (DownResult, error) {
		stopCalled = true
		return DownResult{}, nil
	}, noInstallerSandboxProcesses)
	if err == nil || stopCalled {
		t.Fatalf("preflight error = %v, stop called = %t", err, stopCalled)
	}
	for _, path := range []string{paths.DataDirectory, paths.ConfigurationDirectory, paths.CacheDirectory} {
		if _, err := os.Stat(filepath.Join(path, "keep.txt")); err != nil {
			t.Fatalf("state changed after failed preflight: %s: %v", path, err)
		}
	}
}

func TestCleanInstallerDataRejectsNestedReparseWithoutTouchingTarget(t *testing.T) {
	root := t.TempDir()
	paths := installerCleanPaths{
		DataDirectory:          filepath.Join(root, "local", applicationName),
		ConfigurationDirectory: filepath.Join(root, "roaming", applicationName),
		CacheDirectory:         filepath.Join(root, "cache"),
		InstallDirectory:       filepath.Join(root, "install"),
		UserHome:               filepath.Join(root, "home"),
		SandboxExecutable:      filepath.Join(root, "WindowsSandbox.exe"),
	}
	for _, path := range []string{paths.DataDirectory, paths.ConfigurationDirectory, paths.CacheDirectory} {
		writeUninstallFixture(t, filepath.Join(path, "keep.txt"), "keep")
	}
	outside := t.TempDir()
	outsideMarker := filepath.Join(outside, "outside.keep")
	writeUninstallFixture(t, outsideMarker, "outside")
	createTestDirectoryLink(t, filepath.Join(paths.CacheDirectory, "unsafe-link"), outside)
	stopCalled := false
	err := cleanInstallerDataAt(context.Background(), paths, false, func(context.Context, string, string) (DownResult, error) {
		stopCalled = true
		return DownResult{}, nil
	}, noInstallerSandboxProcesses)
	if err == nil || stopCalled || !strings.Contains(strings.ToLower(err.Error()), "reparse point") {
		t.Fatalf("reparse preflight error = %v, stop called = %t", err, stopCalled)
	}
	contents, readErr := os.ReadFile(outsideMarker)
	if readErr != nil || string(contents) != "outside" {
		t.Fatalf("external reparse target changed: %q, %v", contents, readErr)
	}
}

func TestCleanInstallerDataStopFailurePreservesAllState(t *testing.T) {
	root := t.TempDir()
	paths := installerCleanPaths{
		DataDirectory:          filepath.Join(root, "local", applicationName),
		ConfigurationDirectory: filepath.Join(root, "roaming", applicationName),
		CacheDirectory:         filepath.Join(root, "cache"),
		InstallDirectory:       filepath.Join(root, "install"),
		UserHome:               filepath.Join(root, "home"),
		SandboxExecutable:      filepath.Join(root, "WindowsSandbox.exe"),
	}
	for _, path := range []string{paths.DataDirectory, paths.ConfigurationDirectory, paths.CacheDirectory} {
		writeUninstallFixture(t, filepath.Join(path, "keep.txt"), "keep")
	}
	managedPath := filepath.Join(paths.DataDirectory, "ssh", "config")
	sshConfig := managedSSHIncludeStart + "\nInclude " + quoteSSHPath(managedPath) + "\n" + managedSSHIncludeEnd + "\n"
	sshPath := filepath.Join(paths.UserHome, ".ssh", "config")
	writeUninstallFixture(t, sshPath, sshConfig)
	err := cleanInstallerDataAt(context.Background(), paths, false, func(context.Context, string, string) (DownResult, error) {
		return DownResult{}, errors.New("fixture stop refusal")
	}, noInstallerSandboxProcesses)
	if err == nil || !strings.Contains(err.Error(), "fixture stop refusal") {
		t.Fatalf("stop error = %v", err)
	}
	for _, path := range []string{paths.DataDirectory, paths.ConfigurationDirectory, paths.CacheDirectory, sshPath} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("state changed after stop refusal: %s: %v", path, err)
		}
	}
}

func TestValidateInstallerCacheRemovalRejectsProjectAndInstallOverlap(t *testing.T) {
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
	} {
		t.Run(name, func(t *testing.T) {
			paths := base
			paths.CacheDirectory = cache
			if name == "workspace" {
				paths.Configuration.Workspaces = map[string]string{"project": filepath.Join(root, "project")}
			}
			if err := validateInstallerCacheRemoval(paths); err == nil {
				t.Fatal("unsafe cache overlap unexpectedly validated")
			}
		})
	}
}

func TestValidateInstallerCacheRemovalAllowsWorkspaceBesideDefaultCache(t *testing.T) {
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
	if err := validateInstallerCacheRemoval(paths); err != nil {
		t.Fatalf("workspace beside default cache was rejected: %v", err)
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
	if err := cleanInstallerDataAt(context.Background(), paths, false, func(context.Context, string, string) (DownResult, error) {
		return DownResult{AlreadyStopped: true}, nil
	}, noInstallerSandboxProcesses); err != nil {
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
	if err := cleanInstallerDataAt(context.Background(), paths, false, func(context.Context, string, string) (DownResult, error) {
		return DownResult{AlreadyStopped: true}, nil
	}, noInstallerSandboxProcesses); err != nil {
		t.Fatalf("cleanInstallerDataAt empty cache parent: %v", err)
	}
	if _, err := os.Lstat(cacheParent); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("empty default cache parent remains: %v", err)
	}
}

func TestCleanInstallerDataRefusesRemainingSandboxBeforeMutation(t *testing.T) {
	root := t.TempDir()
	paths := installerCleanPaths{
		DataDirectory:          filepath.Join(root, "local", applicationName),
		ConfigurationDirectory: filepath.Join(root, "roaming", applicationName),
		CacheDirectory:         filepath.Join(root, "cache"),
		InstallDirectory:       filepath.Join(root, "install"),
		UserHome:               filepath.Join(root, "home"),
		SandboxExecutable:      filepath.Join(root, "WindowsSandbox.exe"),
	}
	for _, path := range []string{paths.DataDirectory, paths.ConfigurationDirectory, paths.CacheDirectory} {
		writeUninstallFixture(t, filepath.Join(path, "keep.txt"), "keep")
	}
	err := cleanInstallerDataAt(context.Background(), paths, true, func(context.Context, string, string) (DownResult, error) {
		return DownResult{}, nil
	}, func(context.Context) error {
		return errors.New("fixture unmanaged Sandbox remains")
	})
	if err == nil || !strings.Contains(err.Error(), "unmanaged Sandbox remains") {
		t.Fatalf("remaining-Sandbox error = %v", err)
	}
	for _, path := range []string{paths.DataDirectory, paths.ConfigurationDirectory, paths.CacheDirectory} {
		if _, err := os.Stat(filepath.Join(path, "keep.txt")); err != nil {
			t.Fatalf("state changed after remaining-Sandbox refusal: %s: %v", path, err)
		}
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

func noInstallerSandboxProcesses(context.Context) error { return nil }
