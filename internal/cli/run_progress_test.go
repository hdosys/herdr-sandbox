package cli

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"herdr-sandbox/internal/sandbox"
)

func TestRunUpReportsPreparationBeforeHostInspection(t *testing.T) {
	dependencies := defaultCommandDependencies()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	dependencies.resolveHerdr = func(context.Context) (sandbox.HostHerdr, error) {
		if got := stdout.String(); got != "Preparing Sandbox...\n" {
			t.Fatalf("output before host inspection = %q", got)
		}
		return sandbox.HostHerdr{}, errors.New("host inspection fixture")
	}
	dependencies.pullHostConfigOnUp = func(context.Context) (sandbox.HostConfigurationPullResult, error) {
		if got := stdout.String(); got != "Preparing Sandbox...\n" {
			t.Fatalf("output before host configuration pull = %q", got)
		}
		return sandbox.HostConfigurationPullResult{}, nil
	}
	dependencies.cleanup = func(context.Context) (sandbox.CleanResult, error) {
		t.Fatal("failed host inspection reached cleanup")
		return sandbox.CleanResult{}, nil
	}
	dependencies.up = func(context.Context, sandbox.Options, sandbox.HostHerdr) (sandbox.Connection, error) {
		t.Fatal("failed host inspection reached provisioning")
		return sandbox.Connection{}, nil
	}

	code := runWithCommandDependencies(context.Background(), []string{"up", "--no-attach"}, &bytes.Buffer{}, &stdout, &stderr, dependencies)
	if code != 1 || stdout.String() != "Preparing Sandbox...\n" || !strings.Contains(stderr.String(), "host inspection fixture") {
		t.Fatalf("code = %d, stdout = %q, stderr = %q", code, stdout.String(), stderr.String())
	}
}
