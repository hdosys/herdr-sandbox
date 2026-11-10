package sandbox

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"herdr-sandbox/internal/installerartifact"
)

// Explicit in-place acceptance uses the existing verified SSH transport. It never
// starts, closes, or repairs a Sandbox and does not claim pristine-guest evidence.
func TestWorkstationRetainedSandboxAcceptance(t *testing.T) {
	run := os.Getenv("HERDR_WORKSTATION_ACCEPTANCE_RUN")
	installer := os.Getenv("HERDR_WORKSTATION_TEST_INSTALLER")
	if run == "" || installer == "" || IsCurrentWindowsSandbox() {
		t.Skip("requires explicit host-side retained test run and private installer")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Minute)
	defer cancel()
	evidence, err := installerartifact.Read(strings.TrimSuffix(installer, filepath.Ext(installer)) + ".json")
	if err != nil {
		t.Fatal(err)
	}
	id, err := newRunID()
	if err != nil {
		t.Fatal(err)
	}
	input := filepath.Join(run, "input", "acceptance-"+id)
	status := filepath.Join(run, "status", "acceptance-"+id)
	for _, directory := range []string{filepath.Join(input, "installer"), filepath.Join(input, "provisioning"), status} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := installerartifact.Snapshot(installer, filepath.Join(input, "installer", "setup.exe"), evidence); err != nil {
		t.Fatal(err)
	}
	for path, data := range map[string][]byte{
		filepath.Join(input, "installer", "workstation-root.cs"):      workstationRootSource,
		filepath.Join(input, "installer", "probes.ps1"):               installerProbeScript,
		filepath.Join(input, "installer", "interactive.ps1"):          installerInteractiveScript,
		filepath.Join(input, "provisioning", provisioningProcessName): provisioningProcessSource,
	} {
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	connection := Connection{RunDirectory: run, SSHConfigPath: filepath.Join(run, ".ssh", "config"), SSHTarget: sshTargetName}
	bootstrapRoot := `C:\SandboxBootstrap\acceptance-` + id
	statusRoot := `C:\SandboxStatus\acceptance-` + id
	baseline := fmt.Sprintf(". '%s\\installer\\probes.ps1'; Get-InstallerBaseline | ConvertTo-Json -Compress", bootstrapRoot)
	baselineContext, stop := context.WithTimeout(ctx, 30*time.Second)
	data, err := runSSHPowerShell(baselineContext, connection, nil, baseline, "retained guest prerequisite observation", 4096)
	stop()
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid(data) {
		t.Fatal("invalid retained guest baseline")
	}
	if err := os.WriteFile(filepath.Join(status, "installer-baseline.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	launcher := buildInstallerProfileLauncher(id, evidence.InstallerSHA256, evidence.ProfileSHA256, 64, bootstrapRoot, statusRoot)
	t.Logf("In-place acceptance %s, source %s, installer %s", id, evidence.Identity.Revision, evidence.InstallerSHA256)
	output, err := runSecretSSHArchivePowerShell(ctx, connection, []byte(evidence.ProfileSHA256), launcher, "retained guest installer acceptance")
	if err != nil {
		t.Fatalf("%v; inspect guest installer state %s", err, id)
	}
	defer clear(output)
	var result installerVerification
	if err := decodeStrictJSON(output, &result); err != nil {
		t.Fatal(err)
	}
	profile := WorkstationProfile{}
	for _, id := range evidence.PackageIDs {
		profile.Packages = append(profile.Packages, wingetPackagePlanEntry{ID: id})
	}
	if err := validateInstallerVerification(result, evidence.InstallerSHA256, evidence.ProfileSHA256, profile); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(status, "installer-verification.json"), output, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, check := range result.Checks {
		t.Logf("%s: %t", check.Name, check.Passed)
	}
	if !result.Passed {
		t.Fatalf("installer exit %d; guest report %s", result.ExitCode, result.ReportPath)
	}
}
