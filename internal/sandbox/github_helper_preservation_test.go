package sandbox

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestGitHubHelperSurvivesFinalConfigurationWithoutCredentialTransfer(t *testing.T) {
	requireExternalBoundaryTest(t, "real Git configuration preservation")
	root := t.TempDir()
	setup := provisioningPowerShellFunctionSetup(t, provisioningPowerShellFunctionSource{
		path:  filepath.Join("assets", "configuration-sync.ps1"),
		names: []string{"Assert-ConfigurationDestinationPath", "Copy-VerifiedConfigurationFile", "Get-GuestGitHubCredentialHelpers", "Restore-GuestGitHubCredentialHelpers"},
	})
	fixture, err := os.ReadFile(filepath.Join("testdata", "preserve-github-helper.ps1"))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "preserve-github-helper.ps1")
	script := "$fixtureRoot = '" + strings.ReplaceAll(root, "'", "''") + "'\n" + setup + string(fixture)
	if err := os.WriteFile(path, []byte(script), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	command := hiddenCommandContext(ctx, mustWindowsPowerShellPath(t), "-NoLogo", "-NoProfile", "-NonInteractive", "-WindowStyle", "Hidden", "-ExecutionPolicy", "Bypass", "-File", path)
	if output, err := command.CombinedOutput(); err != nil || !strings.Contains(string(output), "verified preserved GitHub helper without credential transfer") {
		t.Fatalf("guest Git helper preservation: %v: %s", err, output)
	}
}
