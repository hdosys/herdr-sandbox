package cli

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	"herdr-sandbox/internal/sandbox"
)

func TestGuidedFirstUpRequiresFolderApprovalBeforeEffects(t *testing.T) {
	for _, approval := range []string{"n\n", "y\n"} {
		deps := defaultCommandDependencies()
		deps.interactive = func(io.Reader) bool { return true }
		deps.firstUse = func(string) (sandbox.FirstUse, error) {
			return sandbox.FirstUse{Directory: `C:\Projects\demo`, NeedsProject: true}, nil
		}
		deps.inspect = func(context.Context) (sandbox.SessionStatus, error) {
			return sandbox.SessionStatus{State: sandbox.SessionStopped}, nil
		}
		deps.resolveHerdr = func(context.Context) (sandbox.HostHerdr, error) { return sandbox.HostHerdr{}, nil }
		initialized, provisioned, cleaned := false, false, false
		deps.initialize = func(path string, stacks []string) (sandbox.ProjectInitResult, error) {
			if path != `C:\Projects\demo` || len(stacks) != 1 || stacks[0] != "go" {
				t.Fatalf("unexpected selection: %s %v", path, stacks)
			}
			initialized = true
			return sandbox.ProjectInitResult{Path: "profile"}, nil
		}
		deps.cleanup = func(context.Context) (sandbox.CleanResult, error) { cleaned = true; return sandbox.CleanResult{}, nil }
		deps.pullHostConfigOnUp = func(context.Context) (sandbox.HostConfigurationPullResult, error) {
			return sandbox.HostConfigurationPullResult{}, nil
		}
		deps.up = func(_ context.Context, options sandbox.Options, _ sandbox.HostHerdr) (sandbox.Connection, error) {
			if !initialized || options.ProjectDirectory != `C:\Projects\demo` {
				t.Fatal("unapproved or lost project selection")
			}
			provisioned = true
			return sandbox.Connection{}, nil
		}
		var out, stderr bytes.Buffer
		code := runWithCommandDependencies(t.Context(), []string{"up", "--no-attach"}, strings.NewReader("\ngo\nn\n"+approval), &out, &stderr, deps)
		approved := approval == "y\n"
		if initialized != approved || provisioned != approved || cleaned != approved || approved && code != 0 {
			t.Fatalf("approval=%q code=%d initialized=%t provisioned=%t cleanup=%t err=%s", approval, code, initialized, provisioned, cleaned, &stderr)
		}
	}
}

func TestDownRefusalDoesNotMutateAndExplainsHostEdits(t *testing.T) {
	for _, input := range []string{"", "n\n", "invalid\n"} {
		deps := defaultCommandDependencies()
		deps.cleanup = func(context.Context) (sandbox.CleanResult, error) {
			t.Fatal("declined down mutated state")
			return sandbox.CleanResult{}, nil
		}
		deps.down = func(context.Context) (sandbox.DownResult, error) {
			t.Fatal("declined down stopped a guest")
			return sandbox.DownResult{}, nil
		}
		var out, stderr bytes.Buffer
		code := runWithCommandDependencies(t.Context(), []string{"down"}, strings.NewReader(input), &out, &stderr, deps)
		if code != 0 || !strings.Contains(out.String(), "NOT rolled back") || !strings.Contains(out.String(), "saved device keys remain") {
			t.Fatalf("code=%d %s %s", code, &out, &stderr)
		}
	}
}

func TestBareOverviewAndScopedHelpAreReadOnly(t *testing.T) {
	deps := defaultCommandDependencies()
	deps.inspect = func(context.Context) (sandbox.SessionStatus, error) {
		return sandbox.SessionStatus{State: sandbox.SessionStopped, NextAction: "Run sandbox up"}, nil
	}
	deps.cleanup = func(context.Context) (sandbox.CleanResult, error) {
		t.Fatal("overview mutated state")
		return sandbox.CleanResult{}, nil
	}
	var out, stderr bytes.Buffer
	if code := runWithCommandDependencies(t.Context(), nil, &bytes.Buffer{}, &out, &stderr, deps); code != 0 || !strings.Contains(out.String(), "Sandbox: stopped") {
		t.Fatalf("code=%d %s %s", code, &out, &stderr)
	}
	out.Reset()
	if code := runWithCommandDependencies(t.Context(), []string{"remote", "--help"}, &bytes.Buffer{}, &out, &stderr, deps); code != 0 || !strings.Contains(out.String(), "sandbox remote add") || strings.Contains(out.String(), "sandbox workstation") {
		t.Fatalf("unscoped help: %d %s", code, &out)
	}
}
