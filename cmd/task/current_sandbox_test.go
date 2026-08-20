package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

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
