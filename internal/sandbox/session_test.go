package sandbox

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

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

func TestAttachEnvironmentIsolatesParentHerdrRuntime(t *testing.T) {
	environment := attachEnvironment([]string{
		`PATH=C:\Windows\System32`,
		`HERDR_ENV=1`,
		`Herdr_Session=host-session`,
		`HERDR_SOCKET_PATH=C:\host.sock`,
		`HOME=C:\Users\host`,
		`USERPROFILE=C:\Users\host`,
		`KEEP_ME=yes`,
	})

	for _, forbidden := range []string{
		`HERDR_ENV=1`,
		`Herdr_Session=host-session`,
		`HERDR_SOCKET_PATH=C:\host.sock`,
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
