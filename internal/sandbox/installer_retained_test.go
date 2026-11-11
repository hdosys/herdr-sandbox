package sandbox

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"herdr-sandbox/internal/installerartifact"
)

// Explicit developer restart after the user ends/reboots the guest. Reuses the
// product bootstrap and connection owners without rerunning a known-failing
// installer while its shared source is being corrected.
func TestWorkstationStartSandboxTransport(t *testing.T) {
	if os.Getenv("HERDR_WORKSTATION_START_TRANSPORT") != "1" || IsCurrentWindowsSandbox() {
		t.Skip("requires explicit host-side transport startup")
	}
	installer := os.Getenv("HERDR_WORKSTATION_TEST_INSTALLER")
	if installer == "" {
		t.Fatal("an exact installer and sidecar are required to prepare test input")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	release, err := acquireLifecycleLock(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if err := ensureNoRunningSandbox(ctx); err != nil {
		t.Fatal(err)
	}
	configurationRoot, err := os.UserConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	repository, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	provisioning, err := resolveProvisioningReadOnlyAt("", filepath.Join(configurationRoot, applicationName), repository)
	if err != nil {
		t.Fatal(err)
	}
	provisioning.WindowsTerminal, err = detectHostWindowsTerminalConfiguration()
	if err != nil {
		t.Fatal(err)
	}
	provisioning.Packages, err = resolveWingetPackagePlan(provisioning.PackageConfiguration, provisioning.WindowsTerminal)
	if err != nil {
		t.Fatal(err)
	}
	provisioning.InstallerProvisioning, provisioning.InstallerPath = true, installer
	data, err := defaultDataDirectory()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := prepareHostStateDirectories(data); err != nil {
		t.Fatal(err)
	}
	plan, err := prepareRun(ctx, data, provisioning.MemoryMB, provisioning)
	if err != nil {
		t.Fatal(err)
	}
	exited, err := launchSandbox(ctx, plan)
	if err != nil {
		t.Fatal(err)
	}
	ctx, stop := withSandboxProcessExit(ctx, exited)
	defer stop()
	connectable, err := waitForConnectable(ctx, plan.StatusDirectory, os.Stdout)
	if err != nil {
		t.Fatal(err)
	}
	if !connectable.InstallerProvisioning {
		t.Fatal("bootstrap did not preserve installer-only transport mode")
	}
	herdr, err := exec.LookPath("herdr.exe")
	if err != nil {
		t.Fatal(err)
	}
	connection, err := writeRunConnection(plan, connectable, herdr)
	if err != nil {
		t.Fatal(err)
	}
	if err := verifySSH(ctx, connection); err != nil {
		t.Fatal(err)
	}
	if err := installRunConnectionAlias(plan.DataDirectory, connection); err != nil {
		t.Fatal(err)
	}
	t.Logf("Transport-only Sandbox ready: %s. No software installation or configuration acceptance claimed.", plan.RunDirectory)
}

func TestWorkstationStandardUserSandboxProbe(t *testing.T) {
	run := os.Getenv("HERDR_WORKSTATION_ACCEPTANCE_RUN")
	if run == "" || IsCurrentWindowsSandbox() {
		t.Skip("requires explicit host-side retained test run")
	}
	fixture, err := os.ReadFile(filepath.Join("testdata", "provisioning-standard-user.ps1"))
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(map[string]string{"process": string(provisioningProcessSource), "boundary": string(workstationRootSource), "fixture": string(fixture)})
	if err != nil {
		t.Fatal(err)
	}
	id, err := newRunID()
	if err != nil {
		t.Fatal(err)
	}
	launcher := `$ErrorActionPreference='Stop'
$ProgressPreference='SilentlyContinue'
$payload=(New-Object IO.StreamReader([Console]::OpenStandardInput())).ReadToEnd()|ConvertFrom-Json
Add-Type -TypeDefinition $payload.boundary
$root='C:\ProgramData\herdr-native-check\` + id + `'
[HerdrWorkstation.Directories]::Ensure($root)
[IO.File]::WriteAllText((Join-Path $root 'process.cs'),$payload.process)
[IO.File]::WriteAllText((Join-Path $root 'probe.ps1'),$payload.fixture)
& (Join-Path $root 'probe.ps1') -ProcessOwnerPath (Join-Path $root 'process.cs') -Interactive
`
	connection := Connection{RunDirectory: run, SSHConfigPath: filepath.Join(run, ".ssh", "config"), SSHTarget: sshTargetName}
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	output, err := runSecretSSHArchivePowerShell(ctx, connection, payload, launcher, "standard-user process boundary probe")
	if err != nil || !strings.Contains(string(output), "Verified same user/session/profile, non-administrator, medium integrity, and restricted privileges.") {
		t.Fatalf("standard-user boundary: %v: %s", err, output)
	}
	t.Log(string(output))
}

// Explicit in-place acceptance uses the existing verified SSH transport. It never
// starts, closes, or repairs a Sandbox and does not claim pristine-guest evidence.
func TestWorkstationRetainedSandboxAcceptance(t *testing.T) {
	run := os.Getenv("HERDR_WORKSTATION_ACCEPTANCE_RUN")
	installer := os.Getenv("HERDR_WORKSTATION_TEST_INSTALLER")
	if run == "" || installer == "" || IsCurrentWindowsSandbox() {
		t.Skip("requires explicit host-side retained test run and private installer")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Minute)
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
	if err := installerartifact.SnapshotData(filepath.Join(filepath.Dir(installer), evidence.DataFileName), filepath.Join(input, "installer", evidence.DataFileName), evidence); err != nil {
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
