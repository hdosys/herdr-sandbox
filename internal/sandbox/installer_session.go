package sandbox

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"herdr-sandbox/internal/installerartifact"
	"herdr-sandbox/internal/productidentity"
)

//go:embed assets/installer-receiver.ps1
var installerReceiverScript string

//go:embed assets/installer-probes.ps1
var installerProbeScript []byte

type installerVerification struct {
	SchemaVersion   int              `json:"schemaVersion"`
	InstallerSHA256 string           `json:"installerSHA256"`
	ProfileSHA256   string           `json:"profileSHA256"`
	ExitCode        int              `json:"exitCode"`
	Complete        bool             `json:"complete"`
	Passed          bool             `json:"passed"`
	Checks          []installerCheck `json:"checks"`
	WinGetVersion   string           `json:"wingetVersion"`
	HerdrVersion    string           `json:"herdrVersion"`
	HerdrClient     json.RawMessage  `json:"herdrClient"`
	ReportPath      string           `json:"reportPath"`
}

type installerCheck struct {
	Name   string `json:"name"`
	Passed bool   `json:"passed"`
}

func prepareInstallerRunInputs(ctx context.Context, plan *runPlan, provisioning provisioningPlan) error {
	if provisioning.TTSBundle != "" {
		return errors.New("installer acceptance cannot silently substitute the standalone installer's public TTS bundle for a configured local ttsBundle; this selection is not portable")
	}
	installer, err := filepath.Abs(plan.InstallerPath)
	if err != nil {
		return err
	}
	if !strings.EqualFold(filepath.Ext(installer), ".exe") {
		return errors.New("installer path must select the workstation setup executable")
	}
	sidecar := strings.TrimSuffix(installer, filepath.Ext(installer)) + ".json"
	for _, path := range []string{installer, sidecar} {
		if err := validateWorkstationLocalPath(path); err != nil {
			return err
		}
	}
	evidence, err := installerartifact.Read(sidecar)
	if err != nil {
		return fmt.Errorf("read workstation installer provenance: %w", err)
	}
	if evidence.Product != productidentity.WorkstationApplicationName {
		return errors.New("installer mode requires the workstation installer, not the Sandbox application installer")
	}
	profile, workspaces, err := workstationSelectionFromPlan(ctx, provisioning)
	if err != nil {
		return err
	}
	plan.Workspaces = workspaces
	applyWorkspaceRequirements(plan)
	plan.RequiresVisualStudioLayout = false
	plan.Tailscale = false
	plan.MobileSSHAuthorizedKeys = nil
	manifest, err := encodeGuestWorkspaceManifest(workspaces, plan.ActiveWorkspace)
	if err != nil {
		return err
	}
	publicProfile, err := json.Marshal(profile)
	if err != nil {
		return err
	}
	if _, err := decodeWorkstationProfile(publicProfile); err != nil {
		return err
	}
	input := filepath.Join(plan.InputDirectory, "installer")
	transport := filepath.Join(plan.InputDirectory, "provisioning")
	for _, directory := range []string{input, transport} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			return err
		}
	}
	if err := installerartifact.Snapshot(installer, filepath.Join(input, "setup.exe"), evidence); err != nil {
		return err
	}
	evidenceData, err := json.Marshal(evidence)
	if err != nil {
		return err
	}
	for path, data := range map[string][]byte{
		filepath.Join(input, "setup.json"):                evidenceData,
		filepath.Join(input, "selection.json"):            publicProfile,
		filepath.Join(input, "probes.ps1"):                installerProbeScript,
		filepath.Join(input, "workstation-root.cs"):       workstationRootSource,
		filepath.Join(transport, provisioningProcessName): provisioningProcessSource,
		filepath.Join(transport, workspaceManifestName):   manifest,
		filepath.Join(transport, guestFinalizationName):   guestFinalizationScript,
	} {
		if err := os.WriteFile(path, data, 0o600); err != nil {
			return err
		}
	}
	return nil
}

func selectedInstallerSettings(provisioning provisioningPlan) ([]WorkstationFile, error) {
	roots, err := workstationSettingsRoots()
	if err != nil {
		return nil, err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	sources, err := defaultCodingAgentConfigurationSources(home, provisioning.CodingAgentSync, credentialSyncConfiguration{})
	if err != nil {
		return nil, err
	}
	for kind, selected := range map[string]string{"opencode": sources.OpenCodeDirectory, "claude": sources.ClaudeCodeDirectory, "codex": sources.CodexDirectory, "pi": sources.PiDirectory, "copilot": sources.GitHubCopilotDirectory, "agent-skills": sources.SharedSkillsDirectory} {
		if selected == "" {
			delete(roots, kind)
		} else {
			roots[kind] = selected
		}
	}
	if sources.ClaudeCodeState == "" {
		delete(roots, "claude-state")
	} else {
		roots["claude-state"] = filepath.Dir(sources.ClaudeCodeState)
	}
	if !provisioning.Packages.enabled(packageGit) {
		delete(roots, "git")
		delete(roots, "git-config")
	}
	if !provisioning.Packages.enabled(packageStarship) {
		delete(roots, "starship")
	}
	if !provisioning.Packages.enabled(packageNushell) {
		delete(roots, "nushell")
	}
	if !provisioning.Packages.enabled(packageZed) {
		delete(roots, "zed")
	}
	if provisioning.WindowsTerminal.Edition == "preview" {
		delete(roots, "terminal")
	} else {
		delete(roots, "terminal-preview")
	}
	if !provisioning.Packages.enabled(packageTerminalStable) && !provisioning.Packages.enabled(packageTerminalPreview) {
		delete(roots, "terminal")
		delete(roots, "terminal-preview")
		delete(roots, "terminal-fragments")
	}
	return exportWorkstationSettings(roots)
}

func provisionInstallerSession(ctx context.Context, options Options, plan runPlan, provisioning provisioningPlan, hostHerdr HostHerdr) (connection Connection, resultErr error) {
	operation, err := startSessionOperation(plan.RunDirectory, plan.ID, operationKindReprovision, "installer-transport", "Waiting for transport-only SSH bootstrap.")
	if err != nil {
		return connection, err
	}
	defer func() {
		state, message := operationStateSucceeded, "Installer and independent acceptance passed. No normal development provisioning was run."
		if resultErr != nil {
			state = operationFailureState(ctx, resultErr)
			message = resultErr.Error()
		}
		_, err := finishSessionOperation(plan.RunDirectory, operation, state, "installer-verification", message)
		if err != nil {
			resultErr = errors.Join(resultErr, err)
		}
		if resultErr != nil {
			fmt.Fprintf(options.Output, "Installer test guest preserved. Inspect %s; do not run normal provisioning to repair its acceptance evidence.\n", plan.StatusDirectory)
		}
	}()
	connectable, err := waitForConnectable(ctx, plan.StatusDirectory, options.Output)
	if err != nil {
		return connection, err
	}
	if !connectable.InstallerProvisioning {
		return connection, errors.New("guest did not publish the selected transport-only bootstrap mode")
	}
	connection, err = writeRunConnection(plan, connectable, hostHerdr.commandPath)
	if err != nil {
		return connection, err
	}
	if err := verifySSH(ctx, connection); err != nil {
		return connection, err
	}
	if err := installRunConnectionAlias(plan.DataDirectory, connection); err != nil {
		return connection, err
	}
	input := filepath.Join(plan.InputDirectory, "installer")
	evidence, err := installerartifact.Read(filepath.Join(input, "setup.json"))
	if err != nil {
		return connection, err
	}
	selection, found, err := readBoundedRegularFile(filepath.Join(input, "selection.json"), workstationProfileLimit)
	if err != nil || !found {
		return connection, errors.New("frozen installer selection is unavailable")
	}
	profile, err := decodeWorkstationProfile(selection)
	clear(selection)
	if err != nil {
		return connection, err
	}
	profile.Files, err = selectedInstallerSettings(provisioning)
	if err != nil {
		return connection, err
	}
	defer func() {
		for _, file := range profile.Files {
			clear(file.Data)
		}
	}()
	data, err := json.Marshal(profile)
	if err != nil {
		return connection, err
	}
	defer clear(data)
	if _, err := decodeWorkstationProfile(data); err != nil {
		return connection, err
	}
	profileHash := fmt.Sprintf("%x", sha256.Sum256(data))
	fmt.Fprintln(options.Output, "Running the exact workstation installer; no Base, project provisioning, configuration-sync repair or Herdr deployment follows it.")
	var result installerVerification
	err = runWithRetainedProgress(ctx, plan.StatusDirectory, options.Output, nil, func(runContext context.Context) error {
		launcher := buildInstallerProfileLauncher(plan.ID, evidence.InstallerSHA256, profileHash, len(data), `C:\SandboxBootstrap`, `C:\SandboxStatus`)
		output, err := runSecretSSHArchivePowerShell(runContext, connection, data, launcher, "private installer profile and acceptance")
		if err != nil {
			return err
		}
		defer clear(output)
		if err := decodeStrictJSON(output, &result); err != nil {
			return fmt.Errorf("decode installer verification: %w", err)
		}
		return nil
	})
	if err != nil {
		return connection, err
	}
	if err := validateInstallerVerification(result, evidence.InstallerSHA256, profileHash, profile); err != nil {
		return connection, err
	}
	publicResult, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return connection, err
	}
	if err := writeFileAtomically(filepath.Join(plan.StatusDirectory, "installer-verification.json"), publicResult, 0o600); err != nil {
		return connection, err
	}
	for _, check := range result.Checks {
		fmt.Fprintf(options.Output, "  %s: %t\n", check.Name, check.Passed)
	}
	if !result.Passed {
		return connection, fmt.Errorf("installer acceptance is incomplete (installer exit %d); native report remains at %s", result.ExitCode, result.ReportPath)
	}
	client, err := parseHostHerdrClientStatus(result.HerdrClient)
	if err != nil {
		return connection, err
	}
	if result.HerdrVersion != hostHerdr.version || client.Version != hostHerdr.runtimeVersion || client.Protocol != hostHerdr.protocol {
		return connection, errors.New("installer tools/configuration passed, but installed guest Herdr does not match the existing host runtime; no runtime was replaced to hide this attach incompatibility")
	}
	if err := validateGuestHerdrBinary(client.Binary); err != nil {
		return connection, err
	}
	connection.guestHerdrPath = client.Binary
	connection.HerdrVersion = result.HerdrVersion
	connection.HerdrProtocol = client.Protocol
	connection.herdrRuntimeVersion = client.Version
	connection.WinGetVersion = result.WinGetVersion
	if err := publishGuestHerdrExecutable(ctx, connection, client.Binary); err != nil {
		return connection, err
	}
	// Starting the installed application and opening selected workspaces are target
	// activation, not a second installation/configuration path.
	start := guestHerdrCommandPowerShell() + "& $herdr server start; if ($LASTEXITCODE -ne 0) { throw 'Installed Herdr server did not start.' }"
	startContext, cancel := context.WithTimeout(ctx, 45*time.Second)
	_, err = runSSHPowerShell(startContext, connection, nil, start, "start installer-owned Herdr", maximumRemoteProvisionOutput)
	cancel()
	if err != nil {
		return connection, err
	}
	if err := verifyGuestHerdr(ctx, connection); err != nil {
		return connection, err
	}
	finalize := `& 'C:\SandboxBootstrap\provisioning\finalize-provisioning.ps1' -WorkspaceManifestPath 'C:\SandboxBootstrap\provisioning\workspaces.json' -ProcessOwnerPath 'C:\SandboxBootstrap\provisioning\provisioning-process.cs'`
	finalizeContext, stop := context.WithTimeout(ctx, 10*time.Minute)
	_, err = runSecretSSHPowerShell(finalizeContext, connection, nil, finalize, "activate selected installer workspaces", 64*1024)
	stop()
	if err != nil {
		return connection, err
	}
	if err := hostHerdr.verifyUnchanged(ctx); err != nil {
		return connection, err
	}
	ready := readyStatus(connectable)
	ready.SchemaVersion = readyStatusSchemaVersion
	ready.WinGetVersion = connection.WinGetVersion
	ready.HerdrVersion = connection.HerdrVersion
	ready.HerdrRuntimeVersion = connection.herdrRuntimeVersion
	ready.HerdrProtocol = connection.HerdrProtocol
	ready.HerdrBinary = connection.guestHerdrPath
	if err := writeReadyStatus(plan.StatusDirectory, ready); err != nil {
		return connection, err
	}
	return connection, nil
}

func buildInstallerProfileLauncher(runID, installerHash, profileHash string, length int, bootstrapRoot, statusRoot string) string {
	staging := guestArchiveStagingPowerShell("installer-"+runID, "installer profile", false)
	return fmt.Sprintf("$ErrorActionPreference = 'Stop'\n%s\n$bootstrapRoot = '%s'\n$statusRoot = '%s'\n$installerDigest = '%s'\n$profileDigest = '%s'\n$profileLength = [long]%d\n$runID = '%s'\n%s", staging, strings.ReplaceAll(bootstrapRoot, "'", "''"), strings.ReplaceAll(statusRoot, "'", "''"), installerHash, profileHash, length, runID, installerReceiverScript)
}

func validateInstallerVerification(result installerVerification, installerHash, profileHash string, profile WorkstationProfile) error {
	if result.SchemaVersion != 1 || result.InstallerSHA256 != installerHash || result.ProfileSHA256 != profileHash || len(result.Checks) == 0 || len(result.Checks) > 256 {
		return errors.New("installer verification identity or shape is invalid")
	}
	all := result.Complete && result.ExitCode == 0
	seen := map[string]bool{}
	for _, check := range result.Checks {
		if err := validateTerminalText("installer check", check.Name, 256); err != nil {
			return err
		}
		if seen[check.Name] {
			return errors.New("duplicate installer check")
		}
		seen[check.Name] = true
		all = all && check.Passed
	}
	required := []string{"native report identity", "WinGet command", "selected authored configuration hashes"}
	for _, entry := range profile.Packages {
		required = append(required, "package "+entry.ID)
		if strings.EqualFold(entry.ID, "hdosys.herdr-win") {
			required = append(required, "installer-owned Herdr and configuration")
		}
	}
	for _, name := range required {
		if !seen[name] {
			return fmt.Errorf("installer verification omitted required check %q", name)
		}
	}
	if all != result.Passed {
		return errors.New("installer verification summary contradicts its individual results")
	}
	if err := validateTerminalText("reportPath", result.ReportPath, 1024); err != nil {
		return err
	}
	return nil
}
