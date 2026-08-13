package sandbox

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestPrepareHostStateDirectoriesRejectsIdentityReparseBeforeMutation(t *testing.T) {
	dataDirectory := t.TempDir()
	outside := t.TempDir()
	marker := filepath.Join(outside, "keep.txt")
	if err := os.WriteFile(marker, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	createTestDirectoryLink(t, filepath.Join(dataDirectory, "identity"), outside)
	if _, err := prepareHostStateDirectories(dataDirectory); err == nil || !strings.Contains(strings.ToLower(err.Error()), "reparse point") {
		t.Fatalf("reparse identity root error = %v", err)
	}
	if contents, err := os.ReadFile(marker); err != nil || string(contents) != "keep" {
		t.Fatalf("outside identity target changed: %q, %v", contents, err)
	}
	if _, err := os.Stat(filepath.Join(dataDirectory, "runs")); !os.IsNotExist(err) {
		t.Fatalf("run state was created after unsafe identity detection: %v", err)
	}
}

func TestEnsurePhysicalDirectoryRejectsReparseAncestorBeforeCreation(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	alias := filepath.Join(root, "cache-alias")
	createTestDirectoryLink(t, alias, outside)
	target := filepath.Join(alias, "new-cache")
	if _, err := ensurePhysicalDirectory(target, "cache"); err == nil || !strings.Contains(strings.ToLower(err.Error()), "reparse point") {
		t.Fatalf("reparse cache ancestor error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(outside, "new-cache")); !os.IsNotExist(err) {
		t.Fatalf("cache directory was created through a reparse ancestor: %v", err)
	}
}

func TestDefaultOptionsHasNoOverallTimeout(t *testing.T) {
	if timeout := DefaultOptions().Timeout; timeout != 0 {
		t.Fatalf("default timeout = %s, want no timeout", timeout)
	}
}

func TestWithOptionalTimeoutAddsOnlyExplicitDeadline(t *testing.T) {
	unbounded, cancelUnbounded := withOptionalTimeout(context.Background(), 0)
	defer cancelUnbounded()
	if _, found := unbounded.Deadline(); found {
		t.Fatal("zero timeout added a deadline")
	}

	bounded, cancelBounded := withOptionalTimeout(context.Background(), time.Minute)
	defer cancelBounded()
	if _, found := bounded.Deadline(); !found {
		t.Fatal("explicit timeout did not add a deadline")
	}
}

func TestWithSandboxProcessExitCancelsProvisioning(t *testing.T) {
	exited := make(chan error, 1)
	ctx, stop := withSandboxProcessExit(context.Background(), exited)
	exited <- errors.New("launcher fixture")
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("Sandbox process exit did not cancel provisioning")
	}
	stop()
	if cause := context.Cause(ctx); cause == nil || !strings.Contains(cause.Error(), "Windows Sandbox exited before provisioning completed: launcher fixture") {
		t.Fatalf("process exit cause = %v", cause)
	}
}

func TestSandboxProcessExitCauseSurvivesPostLaunchFailure(t *testing.T) {
	ctx, cancel := context.WithCancelCause(context.Background())
	cause := fmt.Errorf("%w: launcher fixture", errSandboxExitedBeforeProvisioning)
	cancel(cause)
	phaseErr := errors.New("verify SSH: context canceled")
	got := preserveSandboxProcessExitCause(ctx, phaseErr)
	if !errors.Is(got, cause) || !errors.Is(got, phaseErr) || !strings.Contains(got.Error(), "launcher fixture") {
		t.Fatalf("preserved post-launch error = %v", got)
	}
}

func TestConfigurationHandoffTimeoutCoversBoundedHostPhases(t *testing.T) {
	minimum := tailscaleIdentityTimeout + configurationSyncTimeout + time.Minute
	if configurationHandoffTimeout < minimum {
		t.Fatalf("configuration handoff timeout = %s, want at least %s", configurationHandoffTimeout, minimum)
	}
}

func TestEffectiveCacheDirectoryUsesSystemTemporaryDefaultAndConfiguredOverride(t *testing.T) {
	got, err := effectiveCacheDirectory("")
	if err != nil {
		t.Fatalf("effectiveCacheDirectory default: %v", err)
	}
	want := filepath.Join(os.TempDir(), applicationName, "cache")
	if got != want {
		t.Fatalf("default cache directory = %q, want %q", got, want)
	}
	if !filepath.IsAbs(got) {
		t.Fatalf("default cache directory is not absolute: %q", got)
	}

	configured := filepath.Join(t.TempDir(), "configured-cache")
	got, err = effectiveCacheDirectory(configured)
	if err != nil {
		t.Fatalf("effectiveCacheDirectory configured: %v", err)
	}
	if got != configured {
		t.Fatalf("configured cache directory = %q, want %q", got, configured)
	}
}

func TestExpectedWindowsSandboxExecutableDoesNotRequireInstalledFeature(t *testing.T) {
	windowsDirectory := filepath.Join(t.TempDir(), "Windows")
	t.Setenv("WINDIR", windowsDirectory)
	want := filepath.Join(windowsDirectory, "System32", "WindowsSandbox.exe")
	got, err := expectedWindowsSandboxExecutable()
	if err != nil || got != want {
		t.Fatalf("expectedWindowsSandboxExecutable = %q, %v; want %q", got, err, want)
	}
	if _, err := windowsSandboxExecutable(); err == nil || !strings.Contains(err.Error(), "unavailable") {
		t.Fatalf("windowsSandboxExecutable missing-feature error = %v", err)
	}
}

func TestAttachEnvironmentIsolatesParentHerdrRuntime(t *testing.T) {
	environment := attachEnvironment([]string{
		`PATH=C:\Windows\System32`,
		`HERDR_ENV=1`,
		`Herdr_Session=host-session`,
		`HERDR_SOCKET_PATH=C:\host.sock`,
		`HERDR_CLIENT_SOCKET_PATH=C:\host-client.sock`,
		`HOME=C:\Users\host`,
		`USERPROFILE=C:\Users\host`,
		`KEEP_ME=yes`,
	})

	for _, forbidden := range []string{
		`HERDR_ENV=1`,
		`Herdr_Session=host-session`,
		`HERDR_SOCKET_PATH=C:\host.sock`,
		`HERDR_CLIENT_SOCKET_PATH=C:\host-client.sock`,
	} {
		if slices.Contains(environment, forbidden) {
			t.Fatalf("environment retained %q: %#v", forbidden, environment)
		}
	}
	for _, required := range []string{
		`PATH=C:\Windows\System32`,
		`KEEP_ME=yes`,
		`HOME=C:\Users\host`,
		`USERPROFILE=C:\Users\host`,
	} {
		if !slices.Contains(environment, required) {
			t.Fatalf("environment is missing %q: %#v", required, environment)
		}
	}
}
