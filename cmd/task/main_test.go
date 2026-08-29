package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
)

func requireExternalBoundaryTest(t *testing.T, boundary string) {
	t.Helper()
	if os.Getenv(fastTestsEnvironment) == "1" {
		t.Skipf("%s boundary runs through `go run ./cmd/task test-integration`", boundary)
	}
}

func TestRunPrintsHelp(t *testing.T) {
	var stdout bytes.Buffer
	if err := run(context.Background(), nil, &stdout, &bytes.Buffer{}); err != nil {
		t.Fatalf("run help: %v", err)
	}
	if !strings.Contains(stdout.String(), "go run ./cmd/task") ||
		!strings.Contains(stdout.String(), "build intermediate CLI output") ||
		!strings.Contains(stdout.String(), "one canonical local installer or the versioned release pair") ||
		!strings.Contains(stdout.String(), "release VERSION") ||
		!strings.Contains(stdout.String(), "annotated tag") {
		t.Fatalf("help output = %q", stdout.String())
	}
}

func TestRunRejectsUnknownTask(t *testing.T) {
	err := run(context.Background(), []string{"unknown"}, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), `unknown task "unknown"`) {
		t.Fatalf("run error = %v", err)
	}
}

func TestRunRejectsArgumentsForFixedTasks(t *testing.T) {
	for _, task := range []string{"fmt", "build", "verify", "verify-integration", "provisioning-preflight", "native-current-sandbox", "native-all-stacks"} {
		err := run(context.Background(), []string{task, "unexpected"}, &bytes.Buffer{}, &bytes.Buffer{})
		if err == nil || !strings.Contains(err.Error(), "accepts no arguments") {
			t.Fatalf("run %s error = %v", task, err)
		}
	}
}

func TestRunRejectsInvalidReleaseArity(t *testing.T) {
	for _, args := range [][]string{{"release"}, {"release", "v0.0.1", "unexpected"}} {
		err := run(context.Background(), args, &bytes.Buffer{}, &bytes.Buffer{})
		if err == nil || !strings.Contains(err.Error(), "release requires one") {
			t.Fatalf("run %v error = %v", args, err)
		}
	}
}

func TestNativeAllStacksUsesExtendedTimeout(t *testing.T) {
	if got := taskTimeoutFor([]string{"native-all-stacks"}); got != nativeAllStacksTaskTimeout {
		t.Fatalf("native timeout = %s", got)
	}
	if got := taskTimeoutFor([]string{"native-current-sandbox"}); got != currentSandboxTaskTimeout {
		t.Fatalf("current-Sandbox timeout = %s", got)
	}
	if got := taskTimeoutFor([]string{"package-current-sandbox", "v0.0.1"}); got != currentPackageTaskTimeout {
		t.Fatalf("current-Sandbox package timeout = %s", got)
	}
	if got := taskTimeoutFor([]string{"release", "v0.0.1"}); got != releaseTaskTimeout {
		t.Fatalf("release timeout = %s", got)
	}
	if got := taskTimeoutFor([]string{"verify"}); got != taskTimeout {
		t.Fatalf("ordinary timeout = %s", got)
	}
	if got := taskTimeoutFor([]string{"verify-integration"}); got != integrationTaskTimeout {
		t.Fatalf("integration timeout = %s", got)
	}
}

func TestGoTestEnvironmentSelectsOneExplicitMode(t *testing.T) {
	t.Setenv(fastTestsEnvironment, "inherited")
	for fast, expected := range map[bool]bool{true: true, false: false} {
		found := false
		for _, entry := range goTestEnvironment(fast) {
			if entry == fastTestsEnvironment+"=1" {
				found = true
			}
			if strings.HasPrefix(strings.ToUpper(entry), strings.ToUpper(fastTestsEnvironment)+"=") && entry != fastTestsEnvironment+"=1" {
				t.Fatalf("test mode retained inherited value: %q", entry)
			}
		}
		if found != expected {
			t.Fatalf("fast=%t environment marker found=%t", fast, found)
		}
	}
}

func TestCommandTextQuotesWhitespace(t *testing.T) {
	got := commandText("tool", []string{"plain", `path with spaces`})
	if got != `tool plain "path with spaces"` {
		t.Fatalf("commandText = %q", got)
	}
}

func TestGoBuildArgsUseStrippedProductionBuild(t *testing.T) {
	want := []string{
		"build",
		"-trimpath",
		"-buildvcs=false",
		"-ldflags", "-s -w -X herdr-sandbox/internal/productidentity.Version=0.0.7 -X herdr-sandbox/internal/productidentity.Revision=0123456789abcdef0123456789abcdef01234567 -X herdr-sandbox/internal/productidentity.BuildFreshness=2026.08.28.0927Z",
		"-o", `build\bin\sandbox.exe`,
		"./cmd/sandbox",
	}
	got := goBuildArgs(`build\bin\sandbox.exe`, buildIdentity{Version: "0.0.7", Revision: "0123456789abcdef0123456789abcdef01234567", Freshness: "2026.08.28.0927Z"})
	if !slices.Equal(got, want) {
		t.Fatalf("goBuildArgs = %#v, want %#v", got, want)
	}
	if strings.Contains(strings.Join(got, " "), "-buildid=") {
		t.Fatalf("production build must keep Go's normal build ID: %#v", got)
	}
}

func TestNormalizeSourceRevisionRequiresFullSHA1(t *testing.T) {
	want := "0123456789abcdef0123456789abcdef01234567"
	if got, err := normalizeSourceRevision("  " + strings.ToUpper(want) + "\r\n"); err != nil || got != want {
		t.Fatalf("normalizeSourceRevision = %q, %v", got, err)
	}
	for _, revision := range []string{"", "abc", strings.Repeat("0", 39), strings.Repeat("g", 40), strings.Repeat("0", 41)} {
		if _, err := normalizeSourceRevision(revision); err == nil {
			t.Fatalf("invalid revision unexpectedly succeeded: %q", revision)
		}
	}
}

func TestPowerShellSyntaxCheckReportsEveryInvalidScript(t *testing.T) {
	requireExternalBoundaryTest(t, "Windows PowerShell syntax diagnostics")
	if runtime.GOOS != "windows" {
		t.Skip("Windows PowerShell 5.1 syntax boundary")
	}
	root := t.TempDir()
	first := filepath.Join(root, "first.ps1")
	second := filepath.Join(root, "second.ps1")
	for path, contents := range map[string]string{
		first:  "function Broken-First {\n",
		second: "if ($true) {\n",
	} {
		if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	powerShell := filepath.Join(os.Getenv("SystemRoot"), "System32", "WindowsPowerShell", "v1.0", "powershell.exe")
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	var stderr bytes.Buffer
	err := runPowerShellSyntaxCheck(ctx, powerShell, []string{first, second}, &bytes.Buffer{}, &stderr)
	if err == nil {
		t.Fatal("invalid PowerShell scripts unexpectedly passed")
	}
	for _, path := range []string{first, second} {
		if !strings.Contains(stderr.String(), path+":") {
			t.Fatalf("syntax diagnostics did not name %s: %q", path, stderr.String())
		}
	}
}
