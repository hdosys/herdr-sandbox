package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"herdr-sandbox/internal/sandbox"
)

func runWithCleanup(ctx context.Context, args []string, stdin *bytes.Buffer, stdout, stderr *bytes.Buffer, cleanup staleCleanup) int {
	return runWithDependencies(ctx, args, stdin, stdout, stderr, cleanup, sandbox.InspectSession)
}

func TestRunPrintsHelp(t *testing.T) {
	var stdout bytes.Buffer
	code := Run(context.Background(), nil, &bytes.Buffer{}, &stdout, &bytes.Buffer{})
	if code != 0 {
		t.Fatalf("exit code = %d", code)
	}
	for _, required := range []string{
		"herdr-sandbox version", "herdr-sandbox plan", "herdr-sandbox init", "herdr-sandbox up", "--no-attach",
		"herdr-sandbox attach", "herdr-sandbox status", "herdr-sandbox down", "herdr-sandbox clean",
		"cacheDirectory (default <system-temp>\\herdr-sandbox\\cache)", "memoryMB (default 32768)",
		"no overall timeout unless --timeout is supplied", "workspaceDiscovery", "named folder mounts", "wingetPackages", "audio (output)", "audioInput (microphone)", "tailscale", "playwright-cli",
	} {
		if !strings.Contains(stdout.String(), required) {
			t.Fatalf("help is missing %q: %q", required, stdout.String())
		}
	}
	if strings.Contains(stdout.String(), "--timeout 20m") {
		t.Fatalf("help = %q", stdout.String())
	}
	if strings.Contains(stdout.String(), "__installer-") {
		t.Fatalf("installer-only commands leaked into help: %q", stdout.String())
	}
}

func TestRunPrintsVersionWithoutCrossingSandboxBoundary(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"version"}, &bytes.Buffer{}, &stdout, &stderr)
	if code != 0 || stderr.Len() != 0 || !strings.HasPrefix(stdout.String(), "herdr-sandbox ") {
		t.Fatalf("version code = %d, stdout = %q, stderr = %q", code, stdout.String(), stderr.String())
	}
}

func TestFlagCommandHelpPreservesStderr(t *testing.T) {
	for _, command := range []string{"init", "up"} {
		t.Run(command, func(t *testing.T) {
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			code := Run(context.Background(), []string{command, "--help"}, &bytes.Buffer{}, &stdout, &stderr)
			if code != 0 || stdout.Len() != 0 || stderr.String() != usage {
				t.Fatalf("code = %d, stdout = %q, stderr = %q", code, stdout.String(), stderr.String())
			}
		})
	}
}

func TestFlagParseErrorsUseProductPrefix(t *testing.T) {
	for _, command := range []string{"init", "up"} {
		t.Run(command, func(t *testing.T) {
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			code := Run(context.Background(), []string{command, "--unknown"}, &bytes.Buffer{}, &stdout, &stderr)
			if code != 2 || stdout.Len() != 0 || !strings.HasPrefix(stderr.String(), "herdr-sandbox: flag provided but not defined: -unknown\n\nUsage:") {
				t.Fatalf("code = %d, stdout = %q, stderr = %q", code, stdout.String(), stderr.String())
			}
		})
	}
}

func TestRunInstallerOnlyCommandsUseExactOwners(t *testing.T) {
	dependencies := defaultCommandDependencies()
	seedCalls := 0
	cleanCalls := []bool{}
	dependencies.seedInstaller = func() error {
		seedCalls++
		return nil
	}
	dependencies.cleanInstaller = func(ctx context.Context, deleteConfiguration bool) error {
		cleanCalls = append(cleanCalls, deleteConfiguration)
		deadline, ok := ctx.Deadline()
		remaining := time.Until(deadline)
		if !ok || remaining > installerCleanUninstallTimeout || remaining < installerCleanUninstallTimeout-time.Minute {
			t.Fatalf("installer cleanup deadline = %v, found = %t", deadline, ok)
		}
		return nil
	}
	for _, args := range [][]string{{"__installer-seed-configuration"}, {"__installer-clean-uninstall"}, {"__installer-clean-uninstall", "--delete-configuration"}} {
		if code := runWithCommandDependencies(context.Background(), args, &bytes.Buffer{}, &bytes.Buffer{}, &bytes.Buffer{}, dependencies); code != 0 {
			t.Fatalf("%v exit code = %d", args, code)
		}
	}
	if seedCalls != 1 || len(cleanCalls) != 2 || cleanCalls[0] || !cleanCalls[1] {
		t.Fatalf("installer owner calls = seed %d, clean %#v", seedCalls, cleanCalls)
	}
}

func TestRunInstallerOnlyCommandsRejectArgumentsAndFailures(t *testing.T) {
	dependencies := defaultCommandDependencies()
	dependencies.seedInstaller = func() error { return errors.New("seed fixture") }
	dependencies.cleanInstaller = func(context.Context, bool) error { return errors.New("clean fixture") }
	for _, test := range []struct {
		args     []string
		wantCode int
		wantText string
	}{
		{args: []string{"__installer-seed-configuration", "extra"}, wantCode: 2, wantText: "does not accept arguments"},
		{args: []string{"__installer-clean-uninstall", "extra"}, wantCode: 2, wantText: "accepts only --delete-configuration"},
		{args: []string{"__installer-seed-configuration"}, wantCode: 1, wantText: "seed fixture"},
		{args: []string{"__installer-clean-uninstall"}, wantCode: 1, wantText: "clean fixture"},
		{args: []string{"__installer-clean-uninstall", "--delete-configuration"}, wantCode: 1, wantText: "clean fixture"},
	} {
		var stderr bytes.Buffer
		code := runWithCommandDependencies(context.Background(), test.args, &bytes.Buffer{}, &bytes.Buffer{}, &stderr, dependencies)
		if code != test.wantCode || !strings.Contains(stderr.String(), test.wantText) {
			t.Fatalf("args = %v, code = %d, stderr = %q", test.args, code, stderr.String())
		}
	}
}

func TestRunRejectsLifecycleArgumentsBeforeNativeWork(t *testing.T) {
	for _, command := range []string{"plan", "attach", "status", "down", "clean"} {
		var stderr bytes.Buffer
		code := Run(context.Background(), []string{command, "extra"}, &bytes.Buffer{}, &bytes.Buffer{}, &stderr)
		if code != 2 || !strings.Contains(stderr.String(), "does not accept arguments") {
			t.Fatalf("command = %s, exit code = %d, stderr = %q", command, code, stderr.String())
		}
	}
}

func TestPrintSessionStatus(t *testing.T) {
	tests := []struct {
		name   string
		status sandbox.SessionStatus
		want   string
	}{
		{name: "stopped", status: sandbox.SessionStatus{State: sandbox.SessionStopped}, want: "Sandbox\n  State: stopped\n"},
		{name: "unmanaged", status: sandbox.SessionStatus{State: sandbox.SessionUnmanaged, Processes: []string{"WindowsSandboxClient:34", "WindowsSandbox:12"}}, want: "Sandbox\n  State: unmanaged\n\nProcesses\n  - WindowsSandbox:12\n  - WindowsSandboxClient:34\n"},
		{name: "starting", status: sandbox.SessionStatus{State: sandbox.SessionStarting, RunID: "20260724-123456-abcdef12", PID: 1234, Phase: "winget", Message: "Installing"}, want: "Sandbox\n  State: starting\n  Run: 20260724-123456-abcdef12\n  PID: 1234\n  Phase: winget\n  Message: Installing\n"},
		{name: "connectable", status: sandbox.SessionStatus{State: sandbox.SessionStarting, RunID: "20260724-123456-abcdef12", PID: 1234, Phase: "connectable", Message: "SSH and Herdr server are ready; applying verified host configuration"}, want: "Sandbox\n  State: starting\n  Run: 20260724-123456-abcdef12\n  PID: 1234\n  Phase: connectable\n  Message: SSH and Herdr server are ready; applying verified host configuration\n"},
		{name: "ready", status: sandbox.SessionStatus{State: sandbox.SessionReady, RunID: "20260724-123456-abcdef12", PID: 1234, GuestIP: "172.24.1.2", HerdrVersion: "herdr 1.0.0"}, want: "Sandbox\n  State: ready\n  Run: 20260724-123456-abcdef12\n  PID: 1234\n  Guest IP: 172.24.1.2\n  Herdr: herdr 1.0.0\n  Attach: herdr --remote sandbox\n"},
		{name: "failed", status: sandbox.SessionStatus{State: sandbox.SessionFailed, RunID: "20260724-123456-abcdef12", PID: 1234, Phase: "base", Message: "failed"}, want: "Sandbox\n  State: failed\n  Run: 20260724-123456-abcdef12\n  PID: 1234\n  Phase: base\n  Message: failed\n"},
		{name: "stale", status: sandbox.SessionStatus{State: sandbox.SessionStale, RunID: "20260724-123456-abcdef12", PID: 1234, Message: "recorded Windows Sandbox process is no longer running"}, want: "Sandbox\n  State: stale\n  Run: 20260724-123456-abcdef12\n  PID: 1234\n  Message: recorded Windows Sandbox process is no longer running\n"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var output bytes.Buffer
			printSessionStatus(&output, test.status)
			if output.String() != test.want {
				t.Fatalf("status = %q, want %q", output.String(), test.want)
			}
		})
	}
}

func TestPrintDownResult(t *testing.T) {
	tests := []struct {
		name   string
		result sandbox.DownResult
		want   string
	}{
		{name: "stopped", result: sandbox.DownResult{RunID: "20260724-123456-abcdef12"}, want: "Sandbox\n  Result: stopped\n  Run: 20260724-123456-abcdef12\n"},
		{name: "already", result: sandbox.DownResult{AlreadyStopped: true}, want: "Sandbox\n  Result: already stopped\n"},
		{name: "stale", result: sandbox.DownResult{RunID: "20260724-123456-abcdef12", AlreadyStopped: true}, want: "Sandbox\n  Result: already stopped\n  Cleared stale run: 20260724-123456-abcdef12\n"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var output bytes.Buffer
			printDownResult(&output, test.result)
			if output.String() != test.want {
				t.Fatalf("down = %q, want %q", output.String(), test.want)
			}
		})
	}
}

func TestPrintCleanResult(t *testing.T) {
	tests := []struct {
		name   string
		result sandbox.CleanResult
		want   string
	}{
		{name: "empty", result: sandbox.CleanResult{}, want: "Cleanup\n  Removed: no inactive run workspaces\n"},
		{name: "active", result: sandbox.CleanResult{ActiveRunID: "20260725-121936-0d9549e4"}, want: "Cleanup\n  Removed: no inactive run workspaces\n  Preserved active run: 20260725-121936-0d9549e4\n"},
		{name: "one", result: sandbox.CleanResult{RemovedRuns: 1}, want: "Cleanup\n  Removed: 1 inactive run workspace\n"},
		{name: "many", result: sandbox.CleanResult{RemovedRuns: 3, ActiveRunID: "20260725-121936-0d9549e4"}, want: "Cleanup\n  Removed: 3 inactive run workspaces\n  Preserved active run: 20260725-121936-0d9549e4\n"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var output bytes.Buffer
			printCleanResult(&output, test.result)
			if output.String() != test.want {
				t.Fatalf("clean = %q, want %q", output.String(), test.want)
			}
		})
	}
}

func TestRunRejectsUnknownCommand(t *testing.T) {
	var stderr bytes.Buffer
	code := Run(context.Background(), []string{"unknown"}, &bytes.Buffer{}, &bytes.Buffer{}, &stderr)
	if code != 2 || !strings.Contains(stderr.String(), `unknown command "unknown"`) {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr.String())
	}
}

func TestRunRejectsInvalidUpOptionsBeforeNativeWork(t *testing.T) {
	tests := [][]string{
		{"up", "--memory-mb", "0"},
		{"up", "--memory-mb", "1024"},
		{"up", "--timeout", "0s"},
		{"up", "extra"},
	}
	for _, args := range tests {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			code := Run(context.Background(), args, &bytes.Buffer{}, &bytes.Buffer{}, &bytes.Buffer{})
			if code != 2 {
				t.Fatalf("exit code = %d", code)
			}
		})
	}
}

func TestRunInformationalAndInvalidInputDoesNotCleanup(t *testing.T) {
	tests := [][]string{
		nil,
		{"help"},
		{"--help"},
		{"plan", "--help"},
		{"init", "--help"},
		{"attach", "--help"},
		{"status", "--help"},
		{"down", "--help"},
		{"clean", "--help"},
		{"up", "--help"},
		{"unknown"},
		{"status", "extra"},
		{"up", "--memory-mb", "1024"},
	}
	for _, args := range tests {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			called := false
			cleanup := func(context.Context) (sandbox.CleanResult, error) {
				called = true
				return sandbox.CleanResult{}, nil
			}
			runWithCleanup(context.Background(), args, &bytes.Buffer{}, &bytes.Buffer{}, &bytes.Buffer{}, cleanup)
			if called {
				t.Fatalf("cleanup ran for args %v", args)
			}
		})
	}
}

func TestRunValidCommandsAttemptCleanupBeforeNativeWork(t *testing.T) {
	for _, args := range [][]string{{"down"}, {"clean"}, {"up", "--no-attach"}} {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			called := false
			cleanup := func(context.Context) (sandbox.CleanResult, error) {
				called = true
				return sandbox.CleanResult{RemovedRuns: 1}, errors.New("cleanup fixture")
			}
			dependencies := defaultCommandDependencies()
			dependencies.cleanup = cleanup
			if args[0] == "up" {
				dependencies.resolveHerdr = func(context.Context) (sandbox.HostHerdr, error) {
					return sandbox.HostHerdr{}, nil
				}
			}
			var stderr bytes.Buffer
			code := runWithCommandDependencies(context.Background(), args, &bytes.Buffer{}, &bytes.Buffer{}, &stderr, dependencies)
			if !called || code != 1 || !strings.Contains(stderr.String(), "Removed: 1 inactive run workspace") ||
				!strings.Contains(stderr.String(), "stale-state cleanup incomplete: cleanup fixture") {
				t.Fatalf("called = %t, code = %d, stderr = %q", called, code, stderr.String())
			}
		})
	}
}

func TestRunStatusReportsPreservedStateWhenCleanupIsIncomplete(t *testing.T) {
	cleanup := func(context.Context) (sandbox.CleanResult, error) {
		t.Fatal("status called the separate cleanup owner")
		return sandbox.CleanResult{}, nil
	}
	inspect := func(context.Context) (sandbox.SessionStatus, error) {
		return sandbox.SessionStatus{
			State:    sandbox.SessionStale,
			RunID:    "20260724-123456-abcdef12",
			Message:  "recorded Windows Sandbox process identity changed",
			Warnings: []string{"Stale-state cleanup incomplete: ownership is uncertain"},
		}, nil
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := runWithDependencies(context.Background(), []string{"status"}, &bytes.Buffer{}, &stdout, &stderr, cleanup, inspect)
	if code != 0 || stderr.Len() != 0 || !strings.Contains(stdout.String(), "Warnings\n  - Stale-state cleanup incomplete: ownership is uncertain") ||
		!strings.Contains(stdout.String(), "State: stale") || !strings.Contains(stdout.String(), "recorded Windows Sandbox process identity changed") {
		t.Fatalf("code = %d, stdout = %q, stderr = %q", code, stdout.String(), stderr.String())
	}
}

func TestRunCleanUsesOneCanonicalCleanupResult(t *testing.T) {
	calls := 0
	cleanup := func(context.Context) (sandbox.CleanResult, error) {
		calls++
		return sandbox.CleanResult{RemovedRuns: 2}, nil
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := runWithCleanup(context.Background(), []string{"clean"}, &bytes.Buffer{}, &stdout, &stderr, cleanup)
	if code != 0 || calls != 1 || stderr.Len() != 0 || !strings.Contains(stdout.String(), "Removed: 2 inactive run workspaces") {
		t.Fatalf("code = %d, calls = %d, stdout = %q, stderr = %q", code, calls, stdout.String(), stderr.String())
	}
}

func TestRunDownUsesInjectedOwner(t *testing.T) {
	for _, test := range []struct {
		name       string
		result     sandbox.DownResult
		downError  error
		wantCode   int
		wantOutput string
	}{
		{
			name:       "success",
			result:     sandbox.DownResult{RunID: "20260724-123456-abcdef12"},
			wantOutput: "Result: stopped",
		},
		{
			name:       "failure",
			downError:  errors.New("down fixture"),
			wantCode:   1,
			wantOutput: "down fixture",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			dependencies := defaultCommandDependencies()
			order := []string{}
			dependencies.cleanup = func(context.Context) (sandbox.CleanResult, error) {
				order = append(order, "cleanup")
				return sandbox.CleanResult{}, nil
			}
			dependencies.down = func(context.Context) (sandbox.DownResult, error) {
				order = append(order, "down")
				return test.result, test.downError
			}
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			code := runWithCommandDependencies(context.Background(), []string{"down"}, &bytes.Buffer{}, &stdout, &stderr, dependencies)
			output := stdout.String()
			unexpectedOutput := stderr.String()
			if test.downError != nil {
				output, unexpectedOutput = stderr.String(), stdout.String()
			}
			if code != test.wantCode || strings.Join(order, "|") != "cleanup|down" || unexpectedOutput != "" || !strings.Contains(output, test.wantOutput) {
				t.Fatalf("code = %d, order = %v, stdout = %q, stderr = %q", code, order, stdout.String(), stderr.String())
			}
		})
	}
}

func TestRunUpRejectsNoninteractiveAttachBeforeCleanupOrProvisioning(t *testing.T) {
	dependencies := defaultCommandDependencies()
	cleanupCalled := false
	resolveCalled := false
	upCalled := false
	dependencies.validateAttach = func(io.Reader, io.Writer, io.Writer) error { return errors.New("console streams required") }
	dependencies.cleanup = func(context.Context) (sandbox.CleanResult, error) {
		cleanupCalled = true
		return sandbox.CleanResult{}, nil
	}
	dependencies.resolveHerdr = func(context.Context) (sandbox.HostHerdr, error) {
		resolveCalled = true
		return sandbox.HostHerdr{}, nil
	}
	dependencies.up = func(context.Context, sandbox.Options, sandbox.HostHerdr) (sandbox.Connection, error) {
		upCalled = true
		return sandbox.Connection{}, nil
	}
	var stderr bytes.Buffer
	code := runWithCommandDependencies(context.Background(), []string{"up"}, &bytes.Buffer{}, &bytes.Buffer{}, &stderr, dependencies)
	if code != 1 || cleanupCalled || resolveCalled || upCalled || !strings.Contains(stderr.String(), "--no-attach") {
		t.Fatalf("code = %d, cleanup = %t, resolve = %t, up = %t, stderr = %q", code, cleanupCalled, resolveCalled, upCalled, stderr.String())
	}
}

func TestRunUpNoAttachSkipsStreamValidationAndInteractiveAttach(t *testing.T) {
	dependencies := defaultCommandDependencies()
	validated := false
	attached := false
	var order []string
	dependencies.validateAttach = func(io.Reader, io.Writer, io.Writer) error {
		validated = true
		return errors.New("unexpected validation")
	}
	dependencies.resolveHerdr = func(context.Context) (sandbox.HostHerdr, error) {
		order = append(order, "host-herdr")
		return sandbox.HostHerdr{}, nil
	}
	dependencies.cleanup = func(context.Context) (sandbox.CleanResult, error) {
		order = append(order, "cleanup")
		return sandbox.CleanResult{}, nil
	}
	dependencies.up = func(context.Context, sandbox.Options, sandbox.HostHerdr) (sandbox.Connection, error) {
		order = append(order, "up")
		return sandbox.Connection{}, nil
	}
	dependencies.attach = func(context.Context, sandbox.Connection, io.Reader, io.Writer, io.Writer) error {
		attached = true
		return nil
	}
	var stdout bytes.Buffer
	code := runWithCommandDependencies(context.Background(), []string{"up", "--no-attach"}, &bytes.Buffer{}, &stdout, &bytes.Buffer{}, dependencies)
	if code != 0 || validated || attached || strings.Join(order, "|") != "host-herdr|cleanup|up" || !strings.Contains(stdout.String(), "Next: run `herdr-sandbox attach`") {
		t.Fatalf("code = %d, validated = %t, attached = %t, order = %v, stdout = %q", code, validated, attached, order, stdout.String())
	}
}

func TestRunUpRejectsIncompatibleHostHerdrBeforeCleanup(t *testing.T) {
	dependencies := defaultCommandDependencies()
	dependencies.resolveHerdr = func(context.Context) (sandbox.HostHerdr, error) {
		return sandbox.HostHerdr{}, errors.New("remote mode is unsupported; install a compatible Windows Herdr build")
	}
	dependencies.cleanup = func(context.Context) (sandbox.CleanResult, error) {
		t.Fatal("incompatible host Herdr reached cleanup")
		return sandbox.CleanResult{}, nil
	}
	dependencies.up = func(context.Context, sandbox.Options, sandbox.HostHerdr) (sandbox.Connection, error) {
		t.Fatal("incompatible host Herdr reached up")
		return sandbox.Connection{}, nil
	}
	var stderr bytes.Buffer
	code := runWithCommandDependencies(context.Background(), []string{"up", "--no-attach"}, &bytes.Buffer{}, &bytes.Buffer{}, &stderr, dependencies)
	if code != 1 || !strings.Contains(strings.ToLower(stderr.String()), "unsupported") {
		t.Fatalf("code = %d, stderr = %q", code, stderr.String())
	}
}

func TestRunAttachOpensExactReadyConnectionWithoutProvisioning(t *testing.T) {
	dependencies := defaultCommandDependencies()
	opened := false
	attached := false
	resolved := false
	dependencies.validateAttach = func(io.Reader, io.Writer, io.Writer) error { return nil }
	dependencies.resolveHerdr = func(context.Context) (sandbox.HostHerdr, error) {
		resolved = true
		return sandbox.HostHerdr{}, nil
	}
	dependencies.openReady = func(context.Context, io.Writer, sandbox.HostHerdr) (sandbox.Connection, error) {
		opened = true
		return sandbox.Connection{}, nil
	}
	dependencies.attach = func(context.Context, sandbox.Connection, io.Reader, io.Writer, io.Writer) error {
		attached = true
		return nil
	}
	dependencies.up = func(context.Context, sandbox.Options, sandbox.HostHerdr) (sandbox.Connection, error) {
		t.Fatal("attach invoked provisioning")
		return sandbox.Connection{}, nil
	}
	code := runWithCommandDependencies(context.Background(), []string{"attach"}, &bytes.Buffer{}, &bytes.Buffer{}, &bytes.Buffer{}, dependencies)
	if code != 0 || !resolved || !opened || !attached {
		t.Fatalf("code = %d, resolved = %t, opened = %t, attached = %t", code, resolved, opened, attached)
	}
}

func TestRunPlanUsesOnlyReadOnlyResolver(t *testing.T) {
	dependencies := defaultCommandDependencies()
	resolved := false
	dependencies.resolvePlan = func(context.Context, string) (sandbox.EffectivePlan, error) {
		resolved = true
		return sandbox.EffectivePlan{MemoryMB: 32768, CacheDirectory: `C:\cache`, NextAction: "Run up."}, nil
	}
	dependencies.cleanup = func(context.Context) (sandbox.CleanResult, error) {
		t.Fatal("plan invoked cleanup")
		return sandbox.CleanResult{}, nil
	}
	dependencies.up = func(context.Context, sandbox.Options, sandbox.HostHerdr) (sandbox.Connection, error) {
		t.Fatal("plan invoked up")
		return sandbox.Connection{}, nil
	}
	var stdout bytes.Buffer
	code := runWithCommandDependencies(context.Background(), []string{"plan"}, &bytes.Buffer{}, &stdout, &bytes.Buffer{}, dependencies)
	if code != 0 || !resolved || !strings.Contains(stdout.String(), "Memory: 32768 MB") || !strings.Contains(stdout.String(), "Next: Run up.") {
		t.Fatalf("code = %d, resolved = %t, stdout = %q", code, resolved, stdout.String())
	}
}

func TestPrintEffectivePlanUsesReadableSortedSections(t *testing.T) {
	plan := sandbox.EffectivePlan{
		ConfigurationPath:   `C:\Users\user\AppData\Roaming\herdr-sandbox\config.json`,
		ConfigurationExists: true,
		UserScriptPath:      `C:\Users\user\AppData\Roaming\herdr-sandbox\user.ps1`,
		CacheDirectory:      `D:\herdr-cache`,
		MemoryMB:            32768,
		WindowsTerminal:     "stable",
		CodingAgents:        []string{"OpenCode", "Claude Code"},
		GlobalStacks:        []string{"rust", "go"},
		Packages: []sandbox.EffectivePackage{
			{ID: "Git.Git", Version: "latest during provisioning", Source: "base"},
		},
		StackPackages: []sandbox.EffectiveStackPackage{{Stack: "go", PackageOwner: "GoLang.Go"}},
		Mounts: []sandbox.EffectiveMount{
			{Name: "reference", HostDirectory: `E:\reference`, GuestDirectory: `C:\Mounts\reference`, ReadOnly: true},
			{Name: "worktrees", HostDirectory: `E:\worktrees`, GuestDirectory: `C:\Mounts\worktrees`},
		},
		Workspaces: []sandbox.EffectiveWorkspace{
			{Name: "project", HostDirectory: `D:\project`, GuestDirectory: `C:\Workspaces\project`, Active: true, Stacks: []string{"rust", "go"}},
			{Name: "shared", HostDirectory: `D:\shared`, GuestDirectory: `C:\Workspaces\shared`},
		},
		ReadyChanges: []string{"memory: 16384 -> 32768", "workspaces changed"},
		NextAction:   "Run `herdr-sandbox up` to apply this plan.",
	}
	var output bytes.Buffer
	printEffectivePlan(&output, plan)
	for _, required := range []string{
		"Effective plan\n\nConfiguration", "Memory: 32768 MB", "Audio output: disabled", "Microphone input: disabled",
		"Coding agents\n  - Claude Code\n  - OpenCode", "Global stacks\n  - go\n  - rust",
		"Packages\n  - Git.Git\n    Version: latest during provisioning\n    Source: base",
		"Folder mounts\n  - reference\n    Host: E:\\reference\n    Guest: C:\\Mounts\\reference\n    Access: read-only",
		"  - worktrees\n    Host: E:\\worktrees\n    Guest: C:\\Mounts\\worktrees\n    Access: read/write",
		"Workspaces\n  * project (active)\n    Host: D:\\project\n    Guest: C:\\Workspaces\\project\n    Stacks:\n      - go\n      - rust",
		"  - shared\n    Host: D:\\shared", "Ready Sandbox changes\n  - memory: 16384 -> 32768\n  - workspaces changed",
		"Next: Run `herdr-sandbox up` to apply this plan.",
	} {
		if !strings.Contains(output.String(), required) {
			t.Fatalf("plan is missing %q:\n%s", required, output.String())
		}
	}
	for _, packed := range []string{"OpenCode, Claude Code", "rust, go", "workspace:", "workspace(s)"} {
		if strings.Contains(output.String(), packed) {
			t.Fatalf("plan contains packed or placeholder output %q:\n%s", packed, output.String())
		}
	}
}

func TestRunInitAcceptsRepeatedFlagsAndGuidedSelection(t *testing.T) {
	tests := []struct {
		name  string
		args  []string
		input string
		want  string
	}{
		{name: "flags", args: []string{"init", "--stack", "go", "--stack", "dotnet"}, want: "go|dotnet"},
		{name: "herdr virtual", args: []string{"init", "--stack", "herdr"}, want: "herdr"},
		{name: "playwright cli", args: []string{"init", "--stack", "playwright-cli"}, want: "playwright-cli"},
		{name: "guided", args: []string{"init"}, input: "python, rust\n", want: "python|rust"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dependencies := defaultCommandDependencies()
			got := ""
			dependencies.initialize = func(_ string, stacks []string) (sandbox.ProjectInitResult, error) {
				got = strings.Join(stacks, "|")
				return sandbox.ProjectInitResult{Path: `C:\project\.herdr-sandbox\provision.ps1`, Stacks: stacks}, nil
			}
			var stdout bytes.Buffer
			code := runWithCommandDependencies(context.Background(), test.args, bytes.NewBufferString(test.input), &stdout, &bytes.Buffer{}, dependencies)
			if code != 0 || got != test.want || !strings.Contains(stdout.String(), "Project profile created") {
				t.Fatalf("code = %d, stacks = %q, stdout = %q", code, got, stdout.String())
			}
		})
	}
}

func TestPrintSessionStatusIncludesOperationDiagnosticsTimingsAndNextAction(t *testing.T) {
	status := sandbox.SessionStatus{
		State:           sandbox.SessionReady,
		RunID:           "20260729-120000-abcdef12",
		StartedAtUTC:    "2026-07-29T12:00:00Z",
		WinGetVersion:   "v1.29.0",
		HerdrVersion:    "herdr 1.0.0",
		HerdrProtocol:   18,
		DiagnosticsPath: `C:\state\status`,
		Workspaces: []sandbox.SessionWorkspace{{
			Name: "project", Directory: `C:\Workspaces\project`, Active: true,
		}},
		Operation: &sandbox.SessionOperation{
			Kind: "reprovision", State: "failed", Phase: "configuration-sync",
			Message: "copy failed", UpdatedAtUTC: "2026-07-29T12:05:00Z",
		},
		Timings:    []sandbox.SessionTiming{{Role: "Go package total", ElapsedMilliseconds: 1250}},
		Warnings:   []string{"diagnostic warning"},
		NextAction: "Run `herdr-sandbox attach`.",
	}
	var output bytes.Buffer
	printSessionStatus(&output, status)
	for _, required := range []string{
		"Started: 2026-07-29T12:00:00Z", "WinGet: v1.29.0", "Herdr protocol: 18",
		"* project (active)", "Operation\n  Kind: reprovision\n  State: failed", "Phase: configuration-sync",
		"Diagnostics\n  Path: C:\\state\\status", "- Go package total: 1.25s",
		"Warnings\n  - diagnostic warning", "Next: Run `herdr-sandbox attach`.",
	} {
		if !strings.Contains(output.String(), required) {
			t.Fatalf("status is missing %q: %q", required, output.String())
		}
	}
}

func TestPrintSessionStatusDoesNotAdvertiseAttachUntilReadyAndIdle(t *testing.T) {
	for name, status := range map[string]sandbox.SessionStatus{
		"connectable": {
			State: sandbox.SessionStarting, HerdrVersion: "herdr 1.0.0", HerdrProtocol: 18,
		},
		"reprovisioning": {
			State: sandbox.SessionReady, HerdrVersion: "herdr 1.0.0", HerdrProtocol: 18,
			Operation: &sandbox.SessionOperation{Kind: "reprovision", State: "running", Phase: "configuration-sync", Message: "Copying", UpdatedAtUTC: "2026-07-29T12:00:00Z"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			var output bytes.Buffer
			printSessionStatus(&output, status)
			if strings.Contains(output.String(), "Attach: herdr --remote sandbox") {
				t.Fatalf("nonattachable status advertised attach: %q", output.String())
			}
		})
	}
}
