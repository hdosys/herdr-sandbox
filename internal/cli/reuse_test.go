package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"herdr-sandbox/internal/sandbox"
)

func TestUpReusesReadyEnvironmentAndKeepsRefreshExplicit(t *testing.T) {
	for _, scenario := range []struct {
		name, operation, kind     string
		refresh, failedConnection bool
		wantReady, wantProvision  bool
	}{
		{name: "ready", operation: "succeeded", wantReady: true},
		{name: "ready without operation record", wantReady: true},
		{name: "failed refresh stays usable", operation: "failed", kind: "reprovision", wantReady: true},
		{name: "interrupted refresh stays usable", operation: "interrupted", kind: "reprovision", wantReady: true},
		{name: "incomplete initial setup", operation: "failed", kind: "setup", wantProvision: true},
		{name: "explicit refresh", operation: "succeeded", refresh: true, wantProvision: true},
		{name: "failed connection never reprovisions", operation: "succeeded", failedConnection: true, wantReady: true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			deps := defaultCommandDependencies()
			deps.firstUse = func(string) (sandbox.FirstUse, error) { return sandbox.FirstUse{}, nil }
			deps.resolveHerdr = func(context.Context) (sandbox.HostHerdr, error) { return sandbox.HostHerdr{}, nil }
			deps.cleanup = func(context.Context) (sandbox.CleanResult, error) { return sandbox.CleanResult{}, nil }
			deps.inspect = func(context.Context) (sandbox.SessionStatus, error) {
				status := sandbox.SessionStatus{State: sandbox.SessionReady}
				if scenario.operation != "" {
					status.Operation = &sandbox.SessionOperation{State: scenario.operation, Kind: scenario.kind}
				}
				return status, nil
			}
			ready, provisioned, pulled := false, false, false
			deps.openReady = func(context.Context, io.Writer, sandbox.HostHerdr) (sandbox.Connection, error) {
				ready = true
				if scenario.failedConnection {
					return sandbox.Connection{}, errors.New("verified endpoint unavailable")
				}
				return sandbox.Connection{}, nil
			}
			deps.pullHostConfigOnUp = func(context.Context) (sandbox.HostConfigurationPullResult, error) {
				pulled = true
				return sandbox.HostConfigurationPullResult{}, nil
			}
			deps.up = func(context.Context, sandbox.Options, sandbox.HostHerdr) (sandbox.Connection, error) {
				provisioned = true
				return sandbox.Connection{}, nil
			}
			args := []string{"up", "--no-attach"}
			if scenario.refresh {
				args[0] = "refresh"
			}
			var output, stderr bytes.Buffer
			code := runWithCommandDependencies(t.Context(), args, &bytes.Buffer{}, &output, &stderr, deps)
			wantCode := 0
			if scenario.failedConnection {
				wantCode = 1
			}
			if code != wantCode || ready != scenario.wantReady || provisioned != scenario.wantProvision || pulled != scenario.wantProvision {
				t.Fatalf("code=%d ready=%t provisioning=%t pull=%t stderr=%s", code, ready, provisioned, pulled, &stderr)
			}
		})
	}
}

func TestStatusSeparatesUsabilityFromIncompleteSetup(t *testing.T) {
	deps := defaultCommandDependencies()
	deps.inspect = func(context.Context) (sandbox.SessionStatus, error) {
		return sandbox.SessionStatus{State: sandbox.SessionReady, PID: 1234, Operation: &sandbox.SessionOperation{State: "failed", Message: "Oracle failed"}}, nil
	}
	for _, verbose := range []bool{false, true} {
		args := []string{"status"}
		if verbose {
			args = append(args, "--verbose")
		}
		var output, stderr bytes.Buffer
		if code := runWithCommandDependencies(t.Context(), args, &bytes.Buffer{}, &output, &stderr, deps); code != 0 {
			t.Fatalf("ready guest incorrectly reported unusable: %s", &stderr)
		}
		if !strings.Contains(output.String(), "Oracle failed") || strings.Contains(output.String(), "PID: 1234") != verbose {
			t.Fatalf("wrong summary/detail output: %s", &output)
		}
		if !verbose && (!strings.Contains(output.String(), "Work now: sandbox up") || !strings.Contains(output.String(), "Resume the update: sandbox refresh")) {
			t.Fatalf("missing recovery action: %s", &output)
		}
	}
}
