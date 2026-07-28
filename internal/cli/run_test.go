package cli

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"herdr-sandbox/internal/sandbox"
)

func TestRunPrintsHelp(t *testing.T) {
	var stdout bytes.Buffer
	code := Run(context.Background(), nil, &bytes.Buffer{}, &stdout, &bytes.Buffer{})
	if code != 0 {
		t.Fatalf("exit code = %d", code)
	}
	if !strings.Contains(stdout.String(), "herdr-sandbox up") || !strings.Contains(stdout.String(), "herdr-sandbox status") || !strings.Contains(stdout.String(), "herdr-sandbox down") || !strings.Contains(stdout.String(), "herdr-sandbox clean") || !strings.Contains(stdout.String(), "cacheDirectory (default <system-temp>\\herdr-sandbox\\cache)") || !strings.Contains(stdout.String(), "memoryMB (default 32768)") || !strings.Contains(stdout.String(), "no overall timeout unless --timeout is supplied") || !strings.Contains(stdout.String(), "workspaceDiscovery") || !strings.Contains(stdout.String(), "wingetPackages") || strings.Contains(stdout.String(), "--timeout 20m") {
		t.Fatalf("help = %q", stdout.String())
	}
}

func TestRunRejectsLifecycleArgumentsBeforeNativeWork(t *testing.T) {
	for _, command := range []string{"status", "down", "clean"} {
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
		{name: "stopped", status: sandbox.SessionStatus{State: sandbox.SessionStopped}, want: "state: stopped\n"},
		{name: "unmanaged", status: sandbox.SessionStatus{State: sandbox.SessionUnmanaged, Processes: []string{"WindowsSandbox:12", "WindowsSandboxClient:34"}}, want: "state: unmanaged\nprocesses: WindowsSandbox:12, WindowsSandboxClient:34\n"},
		{name: "starting", status: sandbox.SessionStatus{State: sandbox.SessionStarting, RunID: "20260724-123456-abcdef12", PID: 1234, Phase: "winget", Message: "Installing"}, want: "state: starting\nrun: 20260724-123456-abcdef12\npid: 1234\nphase: winget\nmessage: Installing\n"},
		{name: "connectable", status: sandbox.SessionStatus{State: sandbox.SessionStarting, RunID: "20260724-123456-abcdef12", PID: 1234, Phase: "connectable", Message: "SSH and Herdr server are ready; applying verified host configuration"}, want: "state: starting\nrun: 20260724-123456-abcdef12\npid: 1234\nphase: connectable\nmessage: SSH and Herdr server are ready; applying verified host configuration\n"},
		{name: "ready", status: sandbox.SessionStatus{State: sandbox.SessionReady, RunID: "20260724-123456-abcdef12", PID: 1234, GuestIP: "172.24.1.2", HerdrVersion: "herdr 1.0.0"}, want: "state: ready\nrun: 20260724-123456-abcdef12\npid: 1234\nip: 172.24.1.2\nherdr: herdr 1.0.0\nattach: herdr --remote sandbox\n"},
		{name: "failed", status: sandbox.SessionStatus{State: sandbox.SessionFailed, RunID: "20260724-123456-abcdef12", PID: 1234, Phase: "base", Message: "failed"}, want: "state: failed\nrun: 20260724-123456-abcdef12\npid: 1234\nphase: base\nmessage: failed\n"},
		{name: "stale", status: sandbox.SessionStatus{State: sandbox.SessionStale, RunID: "20260724-123456-abcdef12", PID: 1234, Message: "recorded Windows Sandbox process is no longer running"}, want: "state: stale\nrun: 20260724-123456-abcdef12\npid: 1234\nmessage: recorded Windows Sandbox process is no longer running\n"},
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
		{name: "stopped", result: sandbox.DownResult{RunID: "20260724-123456-abcdef12"}, want: "herdr-sandbox: stopped run 20260724-123456-abcdef12\n"},
		{name: "already", result: sandbox.DownResult{AlreadyStopped: true}, want: "herdr-sandbox: already stopped\n"},
		{name: "stale", result: sandbox.DownResult{RunID: "20260724-123456-abcdef12", AlreadyStopped: true}, want: "herdr-sandbox: already stopped (stale run 20260724-123456-abcdef12 cleared)\n"},
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
		{name: "empty", result: sandbox.CleanResult{}, want: "herdr-sandbox: no inactive run workspaces\n"},
		{name: "active", result: sandbox.CleanResult{ActiveRunID: "20260725-121936-0d9549e4"}, want: "herdr-sandbox: no inactive run workspaces; preserved active run 20260725-121936-0d9549e4\n"},
		{name: "one", result: sandbox.CleanResult{RemovedRuns: 1}, want: "herdr-sandbox: removed 1 inactive run workspace\n"},
		{name: "many", result: sandbox.CleanResult{RemovedRuns: 3, ActiveRunID: "20260725-121936-0d9549e4"}, want: "herdr-sandbox: removed 3 inactive run workspaces; preserved active run 20260725-121936-0d9549e4\n"},
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
	for _, args := range [][]string{{"down"}, {"clean"}, {"up"}} {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			called := false
			cleanup := func(context.Context) (sandbox.CleanResult, error) {
				called = true
				return sandbox.CleanResult{RemovedRuns: 1}, errors.New("cleanup fixture")
			}
			var stderr bytes.Buffer
			code := runWithCleanup(context.Background(), args, &bytes.Buffer{}, &bytes.Buffer{}, &stderr, cleanup)
			if !called || code != 1 || !strings.Contains(stderr.String(), "removed 1 inactive run workspace") ||
				!strings.Contains(stderr.String(), "stale-state cleanup incomplete: cleanup fixture") {
				t.Fatalf("called = %t, code = %d, stderr = %q", called, code, stderr.String())
			}
		})
	}
}

func TestRunStatusReportsPreservedStateWhenCleanupIsIncomplete(t *testing.T) {
	cleanup := func(context.Context) (sandbox.CleanResult, error) {
		return sandbox.CleanResult{}, errors.New("ownership is uncertain")
	}
	inspect := func(context.Context) (sandbox.SessionStatus, error) {
		return sandbox.SessionStatus{
			State:   sandbox.SessionStale,
			RunID:   "20260724-123456-abcdef12",
			Message: "recorded Windows Sandbox process identity changed",
		}, nil
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := runWithDependencies(context.Background(), []string{"status"}, &bytes.Buffer{}, &stdout, &stderr, cleanup, inspect)
	if code != 0 || !strings.Contains(stderr.String(), "stale-state cleanup incomplete: ownership is uncertain") ||
		!strings.Contains(stdout.String(), "state: stale") || !strings.Contains(stdout.String(), "recorded Windows Sandbox process identity changed") {
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
	if code != 0 || calls != 1 || stderr.Len() != 0 || !strings.Contains(stdout.String(), "removed 2 inactive run workspaces") {
		t.Fatalf("code = %d, calls = %d, stdout = %q, stderr = %q", code, calls, stdout.String(), stderr.String())
	}
}
