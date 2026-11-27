package main

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestCurrentSandboxGatesRejectHostBeforeExecution(t *testing.T) {
	t.Setenv("USERNAME", "host-fixture")
	for _, args := range [][]string{
		{"native-current-sandbox"},
		{"native-current-sandbox", "--stack", "oracle"},
		{"provisioning-preflight"},
	} {
		err := run(t.Context(), args, io.Discard, io.Discard)
		if err == nil || !strings.Contains(err.Error(), "inside the running Windows Sandbox, not on the host") {
			t.Fatalf("%v host rejection = %v", args, err)
		}
	}
}

func TestCurrentSandboxEnvironmentCannotSkipNativeChecks(t *testing.T) {
	t.Setenv(fastTestsEnvironment, "1")
	t.Setenv(currentSandboxStackEnvironment, "oracle")
	t.Setenv(currentSandboxPayloadEnvironment, "unrelated-payload")
	for _, value := range currentSandboxTestEnvironment("") {
		name, _, _ := strings.Cut(value, "=")
		if strings.EqualFold(name, fastTestsEnvironment) || strings.EqualFold(name, currentSandboxStackEnvironment) || strings.EqualFold(name, currentSandboxPayloadEnvironment) {
			t.Fatalf("native check inherited selection or skip state: %s", name)
		}
	}
}

func TestRestoreGlobalGitSafeDirectoriesPreservesExistingEntries(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows Git configuration regression")
	}
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skipf("git is unavailable: %v", err)
	}
	home := t.TempDir()
	config := filepath.Join(home, ".gitconfig")
	environment := append(os.Environ(), "HOME="+home, "USERPROFILE="+home, "GIT_CONFIG_GLOBAL="+config)
	run := func(args ...string) {
		t.Helper()
		command := exec.Command(git, args...)
		command.Env = environment
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, output)
		}
	}
	run("config", "--global", "--add", "safe.directory", "C:/Workspaces/sandbox")
	run("config", "--global", "--replace-all", "safe.directory", currentSandboxFixtureSafeDirectories[0])
	for _, fixture := range currentSandboxFixtureSafeDirectories[1:] {
		run("config", "--global", "--add", "safe.directory", fixture)
	}

	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("GIT_CONFIG_GLOBAL", config)
	if err := restoreGlobalGitSafeDirectories(context.Background(), []string{"C:/Workspaces/sandbox"}); err != nil {
		t.Fatal(err)
	}
	got, err := readGlobalGitSafeDirectories(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(got, "|") != "C:/Workspaces/sandbox" {
		t.Fatalf("safe directories = %v", got)
	}
}

func TestValidateCurrentSandboxReleaseCapacityRejectsInsufficientSpace(t *testing.T) {
	if err := validateCurrentSandboxReleaseCapacity(`C:\`, currentSandboxReleaseMinimumFreeBytes); err != nil {
		t.Fatalf("minimum release capacity: %v", err)
	}
	err := validateCurrentSandboxReleaseCapacity(`C:\`, currentSandboxReleaseMinimumFreeBytes-1)
	if err == nil || !strings.Contains(err.Error(), "requires at least 4.00 GiB") || !strings.Contains(err.Error(), "C:\\") {
		t.Fatalf("insufficient release capacity error = %v", err)
	}
}
