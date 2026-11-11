package sandbox

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestInstallerCapabilityProbesInCurrentGuest(t *testing.T) {
	if runtime.GOOS != "windows" || os.Getenv("HERDR_WORKSTATION_TEST_INSTALLER") == "" {
		t.Skip("explicit installed-compiler probe acceptance")
	}
	root := t.TempDir()
	for name, data := range map[string][]byte{"probes.ps1": installerProbeScript, "process.cs": provisioningProcessSource} {
		if err := os.WriteFile(filepath.Join(root, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	script := `$ErrorActionPreference='Stop'; $ProgressPreference='SilentlyContinue'
$root='` + strings.ReplaceAll(root, "'", "''") + `'
Add-Type -Path (Join-Path $root 'process.cs')
. (Join-Path $root 'probes.ps1')
$profile=@{packages=@();stacks=@('go','python','nsis','rust-msvc','cpp');files=@()}
$report=@{schemaVersion=1;profileSha256='fixture'}
$result=Test-InstallerEffects -Profile $profile -Report $report -ExpectedProfileHash 'fixture' -ProbeRoot $root -StateRoot $root
$result|ConvertTo-Json -Depth 5 -Compress
if(@($result.checks|Where-Object{-not $_.passed}).Count -ne 0){exit 1}`
	ctx, cancel := context.WithTimeout(t.Context(), 4*time.Minute)
	defer cancel()
	command := hiddenCommandContext(ctx, mustWindowsPowerShellPath(t), "-NoProfile", "-NonInteractive", "-WindowStyle", "Hidden", "-ExecutionPolicy", "Bypass", "-EncodedCommand", encodePowerShell(script))
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("real installed capability probes: %v %s", err, output)
	}
	t.Logf("installed capabilities: %s", output)
}

func TestInstallerVerificationRejectsMissingAndContradictoryEvidence(t *testing.T) {
	profile := WorkstationProfile{Packages: []wingetPackagePlanEntry{{ID: "hdosys.herdr-win"}}}
	result := installerVerification{SchemaVersion: 1, InstallerSHA256: "installer", ProfileSHA256: "profile", Complete: true, Passed: true, ReportPath: `C:\ProgramData\fixture\report.html`}
	for _, name := range []string{"native report identity", "WinGet command", "selected authored configuration hashes", "interactive SSH shell", "package hdosys.herdr-win", "installer-owned Herdr and configuration"} {
		result.Checks = append(result.Checks, installerCheck{name, true})
	}
	if err := validateInstallerVerification(result, "installer", "profile", profile); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*installerVerification){
		func(value *installerVerification) { value.ProfileSHA256 = "other" },
		func(value *installerVerification) { value.Checks = value.Checks[:3] },
		func(value *installerVerification) { value.ExitCode = 1 },
		func(value *installerVerification) { value.Complete = false },
		func(value *installerVerification) { value.Checks = append(value.Checks, value.Checks[0]) },
	} {
		invalid := result
		mutate(&invalid)
		if err := validateInstallerVerification(invalid, "installer", "profile", profile); err == nil {
			t.Fatal("invalid installer acceptance evidence accepted")
		}
	}
}

func TestInstallerSettingsCaptureResolvedUnpackagedTerminal(t *testing.T) {
	root := t.TempDir()
	t.Setenv("APPDATA", filepath.Join(root, "roaming"))
	t.Setenv("HERDR_CONFIG_PATH", filepath.Join(root, "herdr.toml"))
	terminal := testStableWindowsTerminalConfiguration()
	terminal.SettingsPath = filepath.Join(root, "unpackaged", "settings.json")
	terminal.FragmentsPath = filepath.Join(root, "fragments")
	for _, directory := range []string{filepath.Dir(terminal.SettingsPath), terminal.FragmentsPath} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	writeTestFile(t, terminal.SettingsPath, `{"copyOnSelect":true}`)
	writeTestFile(t, filepath.Join(terminal.FragmentsPath, "fixture.json"), "{}")
	packages, err := resolveWingetPackagePlan(wingetPackageConfiguration{Remove: []string{packageGit, packageGitHubCLI, packageStarship, packageZed}, Add: []string{}, Versions: map[string]string{}}, terminal)
	if err != nil {
		t.Fatal(err)
	}
	files, err := captureInstallerConfiguration(t.Context(), provisioningPlan{Packages: packages, WindowsTerminal: terminal}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].Kind != "configuration-archive" {
		t.Fatal("shared configuration archive missing")
	}
	archive, err := zip.NewReader(bytes.NewReader(files[0].Data), int64(len(files[0].Data)))
	if err != nil {
		t.Fatal(err)
	}
	settings, err := archive.Open("windows-terminal/settings.json")
	if err != nil {
		t.Fatal(err)
	}
	defer settings.Close()
	data, err := io.ReadAll(settings)
	if err != nil {
		t.Fatal(err)
	}
	var captured struct {
		CopyOnSelect bool `json:"copyOnSelect"`
	}
	if err := json.Unmarshal(data, &captured); err != nil || !captured.CopyOnSelect {
		t.Fatal("unpackaged Terminal selection not preserved")
	}
}
