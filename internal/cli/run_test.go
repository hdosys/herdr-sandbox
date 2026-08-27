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
		"sandbox version", "sandbox --version", "sandbox plan", "sandbox init", "sandbox up", "--no-attach",
		"sandbox attach", "sandbox status", "sandbox mobile", "sandbox pull-host-config", "sandbox down", "sandbox clean",
		"cacheDirectory (default <system-temp>\\herdr-sandbox\\cache)", "memoryMB (default 32768)",
		"four-hour launch-to-terminal-ready timeout", "--timeout replaces it for one run", "workspaceDiscovery", "credentialSync choices (all disabled by default)", "named folder mounts", "wingetPackages", "audio (output)", "audioInput (microphone)", "tailscale", "mobileSSHAuthorizedKeys", "android", "all", "cpp", "handy", "hyperframes", "java", "nsis", "nushell", "playwright-cli", "python-ai", "tradingview",
	} {
		if !strings.Contains(stdout.String(), required) {
			t.Fatalf("help is missing %q: %q", required, stdout.String())
		}
	}
	if strings.Contains(stdout.String(), "--timeout 20m") {
		t.Fatalf("help = %q", stdout.String())
	}
	for _, hidden := range []string{"--installer-"} {
		if strings.Contains(stdout.String(), hidden) {
			t.Fatalf("installer-only command %q leaked into help: %q", hidden, stdout.String())
		}
	}
}

func TestStackHelpListsGenericStacksBeforeMetaAndCheckoutShortcuts(t *testing.T) {
	generic := "android|audio|cpp|dotnet|go|hyperframes|java|node|nsis|nushell|playwright-cli|python|python-ai|rust|tradingview|zig"
	trailing := "all|handy|herdr"
	for name, text := range map[string]string{"usage": usage, "prompt": stackSelectionHelp} {
		if name == "usage" {
			for line := range strings.SplitSeq(text, "\n") {
				if strings.HasPrefix(strings.TrimSpace(line), "sandbox init ") {
					text = line
					break
				}
			}
		}
		previous := -1
		for stack := range strings.SplitSeq(generic+"|"+trailing, "|") {
			index := strings.Index(text, stack)
			if index <= previous {
				t.Fatalf("%s stack order is invalid at %q: %q", name, stack, text)
			}
			previous = index
		}
	}
}

func TestRunPrintsVersionWithoutCrossingSandboxBoundary(t *testing.T) {
	var expected string
	for _, argument := range []string{"version", "--version"} {
		var stdout, stderr bytes.Buffer
		code := Run(context.Background(), []string{argument}, &bytes.Buffer{}, &stdout, &stderr)
		if code != 0 || stderr.Len() != 0 || !strings.HasPrefix(stdout.String(), "sandbox ") {
			t.Fatalf("%s code = %d, stdout = %q, stderr = %q", argument, code, stdout.String(), stderr.String())
		}
		if expected == "" {
			expected = stdout.String()
		} else if stdout.String() != expected {
			t.Fatalf("%s output = %q, want %q", argument, stdout.String(), expected)
		}
	}
	for _, argument := range []string{"version", "--version"} {
		var stderr bytes.Buffer
		code := Run(context.Background(), []string{argument, "extra"}, &bytes.Buffer{}, &bytes.Buffer{}, &stderr)
		if code != 2 || !strings.Contains(stderr.String(), "version does not accept arguments") {
			t.Fatalf("%s extra argument code = %d, stderr = %q", argument, code, stderr.String())
		}
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
			if code != 2 || stdout.Len() != 0 || !strings.HasPrefix(stderr.String(), "sandbox: flag provided but not defined: -unknown\n\nUsage:") {
				t.Fatalf("code = %d, stdout = %q, stderr = %q", code, stdout.String(), stderr.String())
			}
		})
	}
}

func TestRunInstallerOnlyCommandsUseExactOwners(t *testing.T) {
	dependencies := defaultCommandDependencies()
	openCalls := 0
	seedCalls := 0
	stopCalls := 0
	cleanCalls := []bool{}
	lockedCleanCalls := []bool{}
	dependencies.openConfig = func() (string, bool, error) {
		openCalls++
		return `C:\Users\user\AppData\Roaming\herdr-sandbox\config.json`, false, nil
	}
	dependencies.seedInstaller = func() error {
		seedCalls++
		return nil
	}
	dependencies.stopInstallerProcesses = func(ctx context.Context) error {
		stopCalls++
		deadline, ok := ctx.Deadline()
		remaining := time.Until(deadline)
		if !ok || remaining > installerStopProcessesTimeout || remaining < installerStopProcessesTimeout-time.Second {
			t.Fatalf("installer process-stop deadline = %v, found = %t", deadline, ok)
		}
		return nil
	}
	dependencies.cleanInstaller = func(ctx context.Context, deleteConfiguration bool) error {
		cleanCalls = append(cleanCalls, deleteConfiguration)
		deadline, ok := ctx.Deadline()
		remaining := time.Until(deadline)
		if !ok || remaining > installerCleanUninstallTimeout || remaining < installerCleanUninstallTimeout-time.Second {
			t.Fatalf("installer cleanup deadline = %v, found = %t", deadline, ok)
		}
		return nil
	}
	dependencies.cleanInstallerWithLockHeld = func(ctx context.Context, deleteConfiguration bool) error {
		lockedCleanCalls = append(lockedCleanCalls, deleteConfiguration)
		deadline, ok := ctx.Deadline()
		remaining := time.Until(deadline)
		if !ok || remaining > installerCleanUninstallTimeout || remaining < installerCleanUninstallTimeout-time.Second {
			t.Fatalf("installer locked cleanup deadline = %v, found = %t", deadline, ok)
		}
		return nil
	}
	for _, args := range [][]string{{"--installer-open-configuration"}, {"--installer-seed-configuration"}, {"--installer-stop-processes"}, {"--installer-clean-uninstall", "--installer-lifecycle-lock-held"}, {"--installer-clean-uninstall", "--installer-lifecycle-lock-held", "--delete-configuration"}} {
		if code := runWithCommandDependencies(context.Background(), args, &bytes.Buffer{}, &bytes.Buffer{}, &bytes.Buffer{}, dependencies); code != 0 {
			t.Fatalf("%v exit code = %d", args, code)
		}
	}
	if openCalls != 1 || seedCalls != 1 || stopCalls != 1 || len(cleanCalls) != 0 || len(lockedCleanCalls) != 2 || lockedCleanCalls[0] || !lockedCleanCalls[1] {
		t.Fatalf("installer owner calls = open %d, seed %d, stop %d, clean %#v, locked clean %#v", openCalls, seedCalls, stopCalls, cleanCalls, lockedCleanCalls)
	}
}

func TestRunInstallerOnlyCommandsRejectArgumentsAndFailures(t *testing.T) {
	dependencies := defaultCommandDependencies()
	dependencies.openConfig = func() (string, bool, error) {
		return "", false, errors.New("open fixture")
	}
	dependencies.seedInstaller = func() error { return errors.New("seed fixture") }
	dependencies.stopInstallerProcesses = func(context.Context) error { return errors.New("stop fixture") }
	dependencies.cleanInstaller = func(context.Context, bool) error { return errors.New("clean fixture") }
	dependencies.cleanInstallerWithLockHeld = func(context.Context, bool) error { return errors.New("locked clean fixture") }
	for _, test := range []struct {
		args     []string
		wantCode int
		wantText string
	}{
		{args: []string{"--installer-open-configuration", "extra"}, wantCode: 2, wantText: "does not accept arguments"},
		{args: []string{"--installer-seed-configuration", "extra"}, wantCode: 2, wantText: "does not accept arguments"},
		{args: []string{"--installer-stop-processes", "extra"}, wantCode: 2, wantText: "does not accept arguments"},
		{args: []string{"--installer-clean-uninstall"}, wantCode: 2, wantText: "requires --installer-lifecycle-lock-held"},
		{args: []string{"--installer-clean-uninstall", "extra"}, wantCode: 2, wantText: "requires --installer-lifecycle-lock-held"},
		{args: []string{"--installer-clean-uninstall", "--delete-configuration"}, wantCode: 2, wantText: "requires --installer-lifecycle-lock-held"},
		{args: []string{"--installer-open-configuration"}, wantCode: 1, wantText: "open fixture"},
		{args: []string{"--installer-seed-configuration"}, wantCode: 1, wantText: "seed fixture"},
		{args: []string{"--installer-stop-processes"}, wantCode: 1, wantText: "stop fixture"},
		{args: []string{"--installer-clean-uninstall", "--installer-lock-held"}, wantCode: 2, wantText: "requires --installer-lifecycle-lock-held"},
		{args: []string{"--installer-clean-uninstall", "--installer-lifecycle-lock-held", "extra"}, wantCode: 2, wantText: "accepts only --delete-configuration"},
		{args: []string{"--installer-clean-uninstall", "--installer-lifecycle-lock-held"}, wantCode: 1, wantText: "locked clean fixture"},
		{args: []string{"--installer-clean-uninstall", "--installer-lifecycle-lock-held", "--delete-configuration"}, wantCode: 1, wantText: "locked clean fixture"},
	} {
		var stderr bytes.Buffer
		code := runWithCommandDependencies(context.Background(), test.args, &bytes.Buffer{}, &bytes.Buffer{}, &stderr, dependencies)
		if code != test.wantCode || !strings.Contains(stderr.String(), test.wantText) {
			t.Fatalf("args = %v, code = %d, stderr = %q", test.args, code, stderr.String())
		}
	}
}

func TestRunRejectsLifecycleArgumentsBeforeNativeWork(t *testing.T) {
	for _, command := range []string{"plan", "attach", "status", "mobile", "pull-host-config", "down", "clean"} {
		var stderr bytes.Buffer
		code := Run(context.Background(), []string{command, "extra"}, &bytes.Buffer{}, &bytes.Buffer{}, &stderr)
		if code != 2 || !strings.Contains(stderr.String(), "does not accept arguments") {
			t.Fatalf("command = %s, exit code = %d, stderr = %q", command, code, stderr.String())
		}
	}
}

func TestRunMobilePrintsOnlyReadySecretFreeConnectionProfile(t *testing.T) {
	access := sandbox.MobileAccess{
		URI:                "ssh://WDAGUtilityAccount@herdr-sandbox.example.ts.net:2222",
		DNSName:            "herdr-sandbox.example.ts.net",
		IPv4:               "100.64.0.10",
		SSHUser:            "WDAGUtilityAccount",
		Port:               2222,
		HostKeyFingerprint: "SHA256:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
		AuthorizedKeyCount: 1,
	}
	dependencies := defaultCommandDependencies()
	dependencies.inspect = func(context.Context) (sandbox.SessionStatus, error) {
		return sandbox.SessionStatus{State: sandbox.SessionReady, MobileAccess: &access}, nil
	}
	var stdout, stderr bytes.Buffer
	code := runWithCommandDependencies(context.Background(), []string{"mobile"}, &bytes.Buffer{}, &stdout, &stderr, dependencies)
	if code != 0 || stderr.Len() != 0 || !strings.Contains(stdout.String(), access.URI) ||
		!strings.Contains(stdout.String(), "never a key or password") || !strings.Contains(stdout.String(), "████") {
		t.Fatalf("mobile code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	dependencies.inspect = func(context.Context) (sandbox.SessionStatus, error) {
		return sandbox.SessionStatus{State: sandbox.SessionReady}, nil
	}
	stdout.Reset()
	stderr.Reset()
	code = runWithCommandDependencies(context.Background(), []string{"mobile"}, &bytes.Buffer{}, &stdout, &stderr, dependencies)
	if code != 1 || !strings.Contains(stderr.String(), "mobile access is not ready") {
		t.Fatalf("unconfigured mobile code=%d stderr=%q", code, stderr.String())
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
		{name: "connectable", status: sandbox.SessionStatus{State: sandbox.SessionStarting, RunID: "20260724-123456-abcdef12", PID: 1234, Phase: "connectable", Message: "SSH is ready; applying verified host configuration and provisioning Herdr"}, want: "Sandbox\n  State: starting\n  Run: 20260724-123456-abcdef12\n  PID: 1234\n  Phase: connectable\n  Message: SSH is ready; applying verified host configuration and provisioning Herdr\n"},
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
			dependencies.pullHostConfigOnDown = func(context.Context) (sandbox.HostConfigurationPullResult, error) {
				order = append(order, "pull")
				return sandbox.HostConfigurationPullResult{}, nil
			}
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			code := runWithCommandDependencies(context.Background(), []string{"down"}, &bytes.Buffer{}, &stdout, &stderr, dependencies)
			output := stdout.String()
			unexpectedOutput := stderr.String()
			if test.downError != nil {
				output, unexpectedOutput = stderr.String(), stdout.String()
			}
			wantOrder := "cleanup|down|pull"
			if test.downError != nil {
				wantOrder = "cleanup|down"
			}
			if code != test.wantCode || strings.Join(order, "|") != wantOrder || unexpectedOutput != "" || !strings.Contains(output, test.wantOutput) {
				t.Fatalf("code = %d, order = %v, stdout = %q, stderr = %q", code, order, stdout.String(), stderr.String())
			}
		})
	}
}

func TestRunDownStopsBeforePullAndReportsPostStopPullFailure(t *testing.T) {
	dependencies := defaultCommandDependencies()
	order := []string{}
	dependencies.cleanup = func(context.Context) (sandbox.CleanResult, error) {
		order = append(order, "cleanup")
		return sandbox.CleanResult{}, nil
	}
	dependencies.down = func(context.Context) (sandbox.DownResult, error) {
		order = append(order, "down")
		return sandbox.DownResult{RunID: "run"}, nil
	}
	dependencies.pullHostConfigOnDown = func(context.Context) (sandbox.HostConfigurationPullResult, error) {
		order = append(order, "pull")
		return sandbox.HostConfigurationPullResult{Pulled: []string{"OpenCode configuration"}}, errors.New("diverged")
	}
	var stdout, stderr bytes.Buffer
	code := runWithCommandDependencies(context.Background(), []string{"down"}, &bytes.Buffer{}, &stdout, &stderr, dependencies)
	if code != 1 || strings.Join(order, "|") != "cleanup|down|pull" || !strings.Contains(stdout.String(), "Result: stopped") ||
		!strings.Contains(stdout.String(), "OpenCode configuration") || !strings.Contains(stderr.String(), "Sandbox is stopped") {
		t.Fatalf("code = %d, order = %v, stdout = %q, stderr = %q", code, order, stdout.String(), stderr.String())
	}
}

func TestRunPullHostConfigUsesExplicitOwner(t *testing.T) {
	dependencies := defaultCommandDependencies()
	calls := 0
	dependencies.pullHostConfig = func(context.Context) (sandbox.HostConfigurationPullResult, error) {
		calls++
		return sandbox.HostConfigurationPullResult{
			Pulled:  []string{"OpenCode configuration"},
			Skipped: []string{"Herdr configuration: not a Git repository"},
		}, nil
	}
	var stdout, stderr bytes.Buffer
	code := runWithCommandDependencies(context.Background(), []string{"pull-host-config"}, &bytes.Buffer{}, &stdout, &stderr, dependencies)
	if code != 0 || calls != 1 || stderr.Len() != 0 || !strings.Contains(stdout.String(), "Pulled:") ||
		!strings.Contains(stdout.String(), "OpenCode configuration") || !strings.Contains(stdout.String(), "not a Git repository") {
		t.Fatalf("code = %d, calls = %d, stdout = %q, stderr = %q", code, calls, stdout.String(), stderr.String())
	}
}

func TestRunPullHostConfigAndUpReportPartialResultsBeforeFailure(t *testing.T) {
	for _, command := range []string{"pull-host-config", "up"} {
		t.Run(command, func(t *testing.T) {
			dependencies := defaultCommandDependencies()
			result := sandbox.HostConfigurationPullResult{Pulled: []string{"OpenCode configuration"}}
			failure := errors.New("later repository diverged")
			dependencies.pullHostConfig = func(context.Context) (sandbox.HostConfigurationPullResult, error) { return result, failure }
			dependencies.pullHostConfigOnUp = func(context.Context) (sandbox.HostConfigurationPullResult, error) { return result, failure }
			dependencies.resolveHerdr = func(context.Context) (sandbox.HostHerdr, error) { return sandbox.HostHerdr{}, nil }
			dependencies.cleanup = func(context.Context) (sandbox.CleanResult, error) { return sandbox.CleanResult{}, nil }
			dependencies.up = func(context.Context, sandbox.Options, sandbox.HostHerdr) (sandbox.Connection, error) {
				t.Fatal("failed pull reached up")
				return sandbox.Connection{}, nil
			}
			args := []string{command}
			if command == "up" {
				args = append(args, "--no-attach")
			}
			var stdout, stderr bytes.Buffer
			code := runWithCommandDependencies(context.Background(), args, &bytes.Buffer{}, &stdout, &stderr, dependencies)
			if code != 1 || !strings.Contains(stdout.String(), "OpenCode configuration") || !strings.Contains(stderr.String(), failure.Error()) {
				t.Fatalf("code = %d, stdout = %q, stderr = %q", code, stdout.String(), stderr.String())
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
	dependencies.pullHostConfigOnUp = func(context.Context) (sandbox.HostConfigurationPullResult, error) {
		t.Fatal("noninteractive attach validation reached host configuration pull")
		return sandbox.HostConfigurationPullResult{}, nil
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
	var upOptions sandbox.Options
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
	dependencies.pullHostConfigOnUp = func(context.Context) (sandbox.HostConfigurationPullResult, error) {
		order = append(order, "pull")
		return sandbox.HostConfigurationPullResult{}, nil
	}
	dependencies.up = func(_ context.Context, options sandbox.Options, _ sandbox.HostHerdr) (sandbox.Connection, error) {
		order = append(order, "up")
		upOptions = options
		return sandbox.Connection{}, nil
	}
	dependencies.attach = func(context.Context, sandbox.Connection, io.Reader, io.Writer, io.Writer) error {
		attached = true
		return nil
	}
	var stdout bytes.Buffer
	code := runWithCommandDependencies(context.Background(), []string{"up", "--no-attach"}, &bytes.Buffer{}, &stdout, &bytes.Buffer{}, dependencies)
	if code != 0 || validated || attached || upOptions.Timeout != 4*time.Hour || strings.Join(order, "|") != "host-herdr|cleanup|pull|up" || !strings.Contains(stdout.String(), "Timing: preparation") || !strings.Contains(stdout.String(), "; provisioning") || !strings.Contains(stdout.String(), "; total") || !strings.Contains(stdout.String(), "Next: run `sandbox attach`") {
		t.Fatalf("code = %d, validated = %t, attached = %t, timeout = %s, order = %v, stdout = %q", code, validated, attached, upOptions.Timeout, order, stdout.String())
	}
}

func TestRunUpExplicitTimeoutOverridesDefault(t *testing.T) {
	dependencies := defaultCommandDependencies()
	dependencies.resolveHerdr = func(context.Context) (sandbox.HostHerdr, error) { return sandbox.HostHerdr{}, nil }
	dependencies.cleanup = func(context.Context) (sandbox.CleanResult, error) { return sandbox.CleanResult{}, nil }
	dependencies.pullHostConfigOnUp = func(context.Context) (sandbox.HostConfigurationPullResult, error) {
		return sandbox.HostConfigurationPullResult{}, nil
	}
	var timeout time.Duration
	dependencies.up = func(_ context.Context, options sandbox.Options, _ sandbox.HostHerdr) (sandbox.Connection, error) {
		timeout = options.Timeout
		return sandbox.Connection{}, nil
	}
	code := runWithCommandDependencies(context.Background(), []string{"up", "--no-attach", "--timeout", "45m"}, &bytes.Buffer{}, &bytes.Buffer{}, &bytes.Buffer{}, dependencies)
	if code != 0 || timeout != 45*time.Minute {
		t.Fatalf("code = %d, timeout = %s", code, timeout)
	}
}

func TestRunUpReportsCancellationAndReturnsStandardExitCode(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	dependencies := defaultCommandDependencies()
	dependencies.resolveHerdr = func(context.Context) (sandbox.HostHerdr, error) { return sandbox.HostHerdr{}, nil }
	dependencies.cleanup = func(context.Context) (sandbox.CleanResult, error) { return sandbox.CleanResult{}, nil }
	dependencies.pullHostConfigOnUp = func(context.Context) (sandbox.HostConfigurationPullResult, error) {
		return sandbox.HostConfigurationPullResult{}, nil
	}
	dependencies.up = func(context.Context, sandbox.Options, sandbox.HostHerdr) (sandbox.Connection, error) {
		cancel()
		return sandbox.Connection{}, errors.New("retained provisioning cancelled; the ready Sandbox was preserved. Run `sandbox status` to inspect it")
	}
	var stderr bytes.Buffer
	code := runWithCommandDependencies(ctx, []string{"up", "--no-attach"}, &bytes.Buffer{}, &bytes.Buffer{}, &stderr, dependencies)
	if code != 130 || !strings.Contains(stderr.String(), "provisioning cancelled") || !strings.Contains(stderr.String(), "sandbox status") {
		t.Fatalf("cancelled up code = %d, stderr = %q", code, stderr.String())
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
		ConfigurationPath:             `C:\Users\user\AppData\Roaming\herdr-sandbox\config.json`,
		ConfigurationExists:           true,
		UserScriptPath:                `C:\Users\user\AppData\Roaming\herdr-sandbox\user.ps1`,
		CacheDirectory:                `D:\herdr-cache`,
		WorktreeDirectory:             `E:\herdr-worktrees`,
		ModelsDirectory:               `F:\models`,
		MemoryMB:                      32768,
		WindowsTerminal:               "stable",
		PullHostGitRepositoriesOnUp:   true,
		PullHostGitRepositoriesOnDown: true,
		CodingAgents:                  []string{"OpenCode", "Claude Code"},
		CredentialTransfers:           []string{"TradingView", "OpenCode"},
		GlobalStacks:                  []string{"rust", "go"},
		Packages: []sandbox.EffectivePackage{
			{ID: "Git.Git", Version: "latest during provisioning", Source: "base"},
		},
		StackPackages: []sandbox.EffectiveStackPackage{{Stack: "go", PackageOwner: "GoLang.Go"}},
		ToolVersions: []sandbox.EffectiveToolVersion{{
			Tool: "GoLang.Go", Selection: "1.26.5", Source: "explicit provisioning value or stack constraint", Owners: []string{`project "project" (go)`},
		}},
		Mounts: []sandbox.EffectiveMount{
			{Name: "reference", HostDirectory: `E:\reference`, GuestDirectory: `C:\Mounts\reference`, ReadOnly: true},
			{Name: "worktrees", HostDirectory: `E:\worktrees`, GuestDirectory: `C:\Mounts\worktrees`},
		},
		Workspaces: []sandbox.EffectiveWorkspace{
			{Name: "project", HostDirectory: `D:\project`, GuestDirectory: `C:\Workspaces\project`, Active: true, Stacks: []string{"rust", "go"}},
			{Name: "shared", HostDirectory: `D:\shared`, GuestDirectory: `C:\Workspaces\shared`},
		},
		ReadyChanges: []string{"memory: 16384 -> 32768", "workspaces changed"},
		NextAction:   "Run `sandbox up` to apply this plan.",
	}
	var output bytes.Buffer
	printEffectivePlan(&output, plan)
	for _, required := range []string{
		"Effective plan\n\nConfiguration", "Worktree directory: E:\\herdr-worktrees", "Models directory: F:\\models", "Memory: 32768 MB", "Audio output: disabled", "Microphone input: disabled",
		"Mobile SSH authorized keys: 0", "Pull host Git repositories on up: enabled", "Pull host Git repositories on down: enabled",
		"Coding agents\n  - Claude Code\n  - OpenCode", "Credential transfers\n  - OpenCode\n  - TradingView", "Global stacks\n  - go\n  - rust",
		"Packages\n  - Git.Git\n    Version: latest during provisioning\n    Source: base",
		"Resolved stack tools\n  Precedence: explicit provisioning (including a selected project file) > optional project version file > stack default/latest stable; incompatible stack constraints fail\n  - GoLang.Go\n    Selection: 1.26.5\n    Source: explicit provisioning value or stack constraint\n    Owners:\n      - project \"project\" (go)",
		"Folder mounts\n  - reference\n    Host: E:\\reference\n    Guest: C:\\Mounts\\reference\n    Access: read-only",
		"  - worktrees\n    Host: E:\\worktrees\n    Guest: C:\\Mounts\\worktrees\n    Access: read/write",
		"Workspaces\n  * project (active)\n    Host: D:\\project\n    Guest: C:\\Workspaces\\project\n    Stacks:\n      - go\n      - rust",
		"  - shared\n    Host: D:\\shared", "Ready Sandbox changes\n  - memory: 16384 -> 32768\n  - workspaces changed",
		"Next: Run `sandbox up` to apply this plan.",
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
	plan.CredentialTransfers = nil
	output.Reset()
	printEffectivePlan(&output, plan)
	if !strings.Contains(output.String(), "Secure default: credential transfer is disabled") {
		t.Fatalf("plan has no disabled-credential guidance:\n%s", output.String())
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
		{name: "all", args: []string{"init", "--stack", "all"}, want: "all"},
		{name: "C and C++", args: []string{"init", "--stack", "cpp"}, want: "cpp"},
		{name: "android", args: []string{"init", "--stack", "android"}, want: "android"},
		{name: "audio virtual", args: []string{"init", "--stack", "audio"}, want: "audio"},
		{name: "handy virtual", args: []string{"init", "--stack", "handy"}, want: "handy"},
		{name: "herdr virtual", args: []string{"init", "--stack", "herdr"}, want: "herdr"},
		{name: "java", args: []string{"init", "--stack", "java"}, want: "java"},
		{name: "NSIS", args: []string{"init", "--stack", "nsis"}, want: "nsis"},
		{name: "Nushell", args: []string{"init", "--stack", "nushell"}, want: "nushell"},
		{name: "playwright cli", args: []string{"init", "--stack", "playwright-cli"}, want: "playwright-cli"},
		{name: "python ai", args: []string{"init", "--stack", "python-ai"}, want: "python-ai"},
		{name: "tradingview", args: []string{"init", "--stack", "tradingview"}, want: "tradingview"},
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
		NextAction: "Run `sandbox attach`.",
	}
	var output bytes.Buffer
	printSessionStatus(&output, status)
	for _, required := range []string{
		"Started: 2026-07-29T12:00:00Z", "WinGet: v1.29.0", "Herdr protocol: 18",
		"* project (active)", "Operation\n  Kind: reprovision\n  State: failed", "Phase: configuration-sync",
		"Diagnostics\n  Path: C:\\state\\status", "- Go package total: 1.25s",
		"Warnings\n  - diagnostic warning", "Next: Run `sandbox attach`.",
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
