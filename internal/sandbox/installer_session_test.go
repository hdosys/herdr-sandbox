package sandbox

import (
	"bytes"
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
	baseline := "$ErrorActionPreference='Stop'; . '" + strings.ReplaceAll(filepath.Join(bootstrap, "installer", "probes.ps1"), "'", "''") + "'; Get-InstallerBaseline | ConvertTo-Json -Compress"
	query := hiddenCommandContext(t.Context(), mustWindowsPowerShellPath(t), "-NoProfile", "-NonInteractive", "-WindowStyle", "Hidden", "-EncodedCommand", encodePowerShell(baseline))
	baselineData, err := query.CombinedOutput()
	if err != nil {
		t.Fatalf("baseline: %v %s", err, baselineData)
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
			launcher = strings.ReplaceAll(launcher, guestRootDirectory, strings.ReplaceAll(guestRoot, "'", "''"))
			encoded := sshArchiveLauncherBytes(launcher)
			transport := buildSSHArchiveTransportCommand(digest, len(data), encoded, 40*time.Second, id)
			transport = strings.ReplaceAll(transport, guestRootDirectory, strings.ReplaceAll(guestRoot, "'", "''"))
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
