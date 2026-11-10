package cli

import (
	"bytes"
	"context"
	"testing"

	"herdr-sandbox/internal/sandbox"
)

func TestWorkstationTestRoutesToExactInstallerWithoutNormalProvisioning(t *testing.T) {
	// Simulate only the host CLI decision; native work is replaced by dependencies.
	t.Setenv("USERNAME", "fixture-host")
	dependencies := defaultCommandDependencies()
	dependencies.workstationInstaller = func(name string) (string, error) {
		if name != "MyPC" {
			t.Fatalf("wrong saved installation: %s", name)
		}
		return `C:\fixture\setup.exe`, nil
	}
	dependencies.cleanup = func(context.Context) (sandbox.CleanResult, error) { return sandbox.CleanResult{}, nil }
	dependencies.resolveHerdr = func(context.Context) (sandbox.HostHerdr, error) { return sandbox.HostHerdr{}, nil }
	dependencies.pullHostConfigOnUp = func(context.Context) (sandbox.HostConfigurationPullResult, error) {
		t.Fatal("installer test changed current host configuration")
		return sandbox.HostConfigurationPullResult{}, nil
	}
	called := false
	dependencies.up = func(_ context.Context, options sandbox.Options, _ sandbox.HostHerdr) (sandbox.Connection, error) {
		called = true
		if !options.InstallerProvisioning || options.InstallerPath != `C:\fixture\setup.exe` || options.Refresh {
			t.Fatalf("not exact installer-only mode: %+v", options)
		}
		return sandbox.Connection{}, nil
	}
	for _, args := range [][]string{{"workstation", "test", "MyPC"}, {"workstation", "test", "--installer", `C:\fixture\setup.exe`}} {
		called = false
		var stdout, stderr bytes.Buffer
		code := runWithCommandDependencies(t.Context(), args, &bytes.Buffer{}, &stdout, &stderr, dependencies)
		if code != 0 || !called {
			t.Fatalf("test flow failed: %d %s", code, stderr.String())
		}
	}
}
