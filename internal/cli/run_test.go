package cli

import (
	"bytes"
	"context"
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
	if !strings.Contains(stdout.String(), "herdr-sandbox up") || !strings.Contains(stdout.String(), "herdr-sandbox status") || !strings.Contains(stdout.String(), "herdr-sandbox down") || !strings.Contains(stdout.String(), "cacheDirectory (default <system-temp>\\herdr-sandbox\\cache)") || !strings.Contains(stdout.String(), "memoryMB (default 32768)") || !strings.Contains(stdout.String(), "wingetPackages") {
		t.Fatalf("help = %q", stdout.String())
	}
}

func TestRunRejectsLifecycleArgumentsBeforeNativeWork(t *testing.T) {
	for _, command := range []string{"status", "down"} {
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
