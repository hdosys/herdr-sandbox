package sandbox

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"herdr-sandbox/internal/installerartifact"
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
	for _, name := range []string{"native report identity", "WinGet command", "selected authored configuration hashes", "package hdosys.herdr-win", "installer-owned Herdr and configuration"} {
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

func TestInstallerSettingsCaptureOnlyAuthoredAgentFilesAndPortableMCP(t *testing.T) {
	root := t.TempDir()
	for name, value := range map[string]string{
		"claude/settings.json": "{}", "claude/skills/demo/SKILL.md": "authored",
		"claude/debug/example.txt": "private", "claude/todos/example.json": "private",
		"claude/projects/example/memory/MEMORY.md": "private", "claude/.credentials.json": "private",
		"opencode/opencode.json": "{}", "opencode/projects/private.json": "private",
		"codex/config.toml": "", "codex/sessions/private.json": "private",
		"pi/settings.json": "{}", "pi/debug/private.txt": "private",
		".claude.json": `{"mcpServers":{"test":{"command":"fixture"}},"projects":{"private":"history"},"oauthAccount":{"secret":"private"}}`,
	} {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(value), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	files, err := exportWorkstationSettings(map[string]string{"claude": filepath.Join(root, "claude"), "claude-state": root, "opencode": filepath.Join(root, "opencode"), "codex": filepath.Join(root, "codex"), "pi": filepath.Join(root, "pi")})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 6 {
		t.Fatalf("unexpected selected authored files: %+v", files)
	}
	for _, file := range files {
		if bytes.Contains(file.Data, []byte("private")) {
			t.Fatalf("runtime/private state exported: %s/%s", file.Kind, file.Path)
		}
		if file.Kind == "claude-state" {
			var state map[string]json.RawMessage
			if err := json.Unmarshal(file.Data, &state); err != nil || len(state) != 1 || state["mcpServers"] == nil {
				t.Fatal("portable MCP extraction failed")
			}
			if !allowedWorkstationFile(file.Kind, file.Path) {
				t.Fatal("portable MCP profile rejected")
			}
			destination := t.TempDir()
			if err := importWorkstationFile(destination, file); err != nil {
				t.Fatal(err)
			}
			actual, err := os.ReadFile(filepath.Join(destination, ".claude.json"))
			if err != nil || !bytes.Equal(actual, file.Data) {
				t.Fatal("portable MCP import differs")
			}
		}
	}
}

// This opt-in uses the real framed receiver and exact NSIS candidate. Only a
// synthetic configuration is installed, never SSH, Herdr or personal settings.
func TestInstallerReceiverNativeConfigurationAndConflict(t *testing.T) {
	installer := os.Getenv("HERDR_WORKSTATION_TEST_INSTALLER")
	if runtime.GOOS != "windows" || installer == "" {
		t.Skip("set HERDR_WORKSTATION_TEST_INSTALLER for isolated native receiver acceptance")
	}
	evidence, err := installerartifact.Read(strings.TrimSuffix(installer, filepath.Ext(installer)) + ".json")
	if err != nil || evidence.Product != "herdr-workstation" {
		t.Fatalf("workstation candidate provenance: %v", err)
	}
	root := t.TempDir()
	bootstrap, status := filepath.Join(root, "bootstrap"), filepath.Join(root, "status")
	for _, directory := range []string{filepath.Join(bootstrap, "installer"), filepath.Join(bootstrap, "provisioning"), status} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := installerartifact.Snapshot(installer, filepath.Join(bootstrap, "installer", "setup.exe"), evidence); err != nil {
		t.Fatal(err)
	}
	for path, data := range map[string][]byte{
		filepath.Join(bootstrap, "installer", "workstation-root.cs"):      workstationRootSource,
		filepath.Join(bootstrap, "installer", "probes.ps1"):               installerProbeScript,
		filepath.Join(bootstrap, "provisioning", provisioningProcessName): provisioningProcessSource,
	} {
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	baseline := "$ErrorActionPreference='Stop'; $ProgressPreference='SilentlyContinue'; . '" + strings.ReplaceAll(filepath.Join(bootstrap, "installer", "probes.ps1"), "'", "''") + "'; Get-InstallerBaseline | ConvertTo-Json -Compress"
	query := hiddenCommandContext(t.Context(), mustWindowsPowerShellPath(t), "-NoProfile", "-NonInteractive", "-WindowStyle", "Hidden", "-ExecutionPolicy", "Bypass", "-EncodedCommand", encodePowerShell(baseline))
	baselineData, err := query.CombinedOutput()
	if err != nil {
		t.Fatalf("baseline: %v %s", err, baselineData)
	}
	if !json.Valid(bytes.TrimSpace(baselineData)) {
		t.Fatalf("baseline did not produce JSON: %q", baselineData)
	}
	if err := os.WriteFile(filepath.Join(status, "installer-baseline.json"), bytes.TrimSpace(baselineData), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	settings := []byte("{\"theme\":\"system\"}\n")
	profile := WorkstationProfile{SchemaVersion: 1, Packages: []wingetPackagePlanEntry{}, Stacks: []string{}, Tools: []resolvedToolVersion{}, Files: []WorkstationFile{{Kind: "opencode", Path: "opencode.json", SHA256: fmt.Sprintf("%x", sha256.Sum256(settings)), Data: settings}}}
	for _, conflict := range []bool{false, true} {
		t.Run(fmt.Sprintf("conflict=%t", conflict), func(t *testing.T) {
			if conflict {
				profile.Files[0].Data = []byte("{\"theme\":\"light\"}")
				profile.Files[0].SHA256 = fmt.Sprintf("%x", sha256.Sum256(profile.Files[0].Data))
			}
			data, err := json.Marshal(profile)
			if err != nil {
				t.Fatal(err)
			}
			digest := fmt.Sprintf("%x", sha256.Sum256(data))
			id, err := newRunID()
			if err != nil {
				t.Fatal(err)
			}
			for _, parent := range []string{"herdr-installer-input", "herdr-installer-state"} {
				path := filepath.Join(os.Getenv("ProgramData"), parent, id)
				if _, err := os.Lstat(path); !os.IsNotExist(err) {
					t.Fatalf("fixture root already exists: %s", path)
				}
				t.Cleanup(func() {
					if err := os.RemoveAll(path); err != nil {
						t.Errorf("owned fixture cleanup: %v", err)
					}
				})
			}
			launcher := buildInstallerProfileLauncher(id, evidence.InstallerSHA256, digest, len(data), bootstrap, status)
			guestRoot := filepath.Join(root, "transport")
			launcher = strings.ReplaceAll(launcher, "'"+guestRootDirectory+"\\staging'", "'"+strings.ReplaceAll(filepath.Join(guestRoot, "staging"), "'", "''")+"'")
			encoded := sshArchiveLauncherBytes(launcher)
			transport := buildSSHArchiveTransportCommand(digest, len(data), encoded, 40*time.Second, id)
			transport = strings.ReplaceAll(transport, "'"+guestRootDirectory+"\\staging'", "'"+strings.ReplaceAll(filepath.Join(guestRoot, "staging"), "'", "''")+"'")
			output, err := runLocalSSHTransport(t, transport, encoded, data, nil)
			if err != nil {
				t.Fatalf("real framed installer receiver: %v %s", err, output)
			}
			var result installerVerification
			if err := decodeStrictJSON(output, &result); err != nil {
				t.Fatalf("result: %v %s", err, output)
			}
			if err := validateInstallerVerification(result, evidence.InstallerSHA256, digest, profile); err != nil {
				t.Fatal(err)
			}
			if result.Passed == conflict || result.Complete == conflict {
				t.Fatalf("unexpected installer outcome: %+v", result)
			}
			actual, err := os.ReadFile(filepath.Join(root, "config", "opencode", "opencode.json"))
			if err != nil || !bytes.Equal(actual, settings) {
				t.Fatalf("installed configuration or conflict preservation: %v", err)
			}
			if _, err := os.Stat(filepath.Join(guestRoot, "staging", "installer-"+id)); !os.IsNotExist(err) {
				t.Fatal("probe staging remains")
			}
			t.Logf("candidate %s: passed=%t exit=%d checks=%+v", evidence.InstallerSHA256, result.Passed, result.ExitCode, result.Checks)
		})
	}
}
