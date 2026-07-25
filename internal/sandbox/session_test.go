package sandbox

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

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
