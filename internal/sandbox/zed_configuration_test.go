package sandbox

import (
	"os"
	"path/filepath"
	"testing"
)

func TestZedConfigurationDiscoveryAndUnsafeSource(t *testing.T) {
	root := t.TempDir()
	t.Setenv("APPDATA", root)
	terminal := testStableWindowsTerminalConfiguration()
	packages, err := resolveWingetPackagePlan(defaultWingetPackageConfiguration(), terminal)
	if err != nil {
		t.Fatal(err)
	}
	sources, err := defaultHostConfigurationSources(terminal, packages, codingAgentSyncConfiguration{}, credentialSyncConfiguration{}, false)
	if err != nil || sources.ZedConfiguration != filepath.Join(root, "Zed") {
		t.Fatalf("Zed discovery = %q: %v", sources.ZedConfiguration, err)
	}
	if err := os.MkdirAll(sources.ZedConfiguration, 0o700); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(root, "private.json")
	writeTestFile(t, outside, "must not transfer")
	if err := os.Symlink(outside, filepath.Join(sources.ZedConfiguration, "settings.json")); err != nil {
		t.Skipf("cannot create symbolic-link fixture: %v", err)
	}
	copied := false
	err = addZedConfiguration(sources.ZedConfiguration, func(_, _ string) error {
		copied = true
		return nil
	})
	if err == nil || copied {
		t.Fatalf("unsafe Zed source was not rejected before copy: %v", err)
	}
}
