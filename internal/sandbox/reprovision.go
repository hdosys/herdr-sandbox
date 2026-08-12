package sandbox

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	reprovisionResultSchema        = 2
	explorerRestartStatusSchema    = 1
	explorerRestartStatusPending   = "pending"
	explorerRestartStatusSucceeded = "succeeded"
	explorerRestartStatusFailed    = "failed"
	explorerRestartStatusTimeout   = 90 * time.Second
	maximumWSBSize                 = 1024 * 1024
)

type reprovisionResult struct {
	SchemaVersion            int    `json:"schemaVersion"`
	ArchiveSHA256            string `json:"archiveSha256"`
	ProjectCount             int    `json:"projectCount"`
	ExplorerRestartScheduled bool   `json:"explorerRestartScheduled"`
	ExplorerRestartID        string `json:"explorerRestartId"`
	ExplorerRestartTaskName  string `json:"explorerRestartTaskName"`
}

type explorerRestartStatus struct {
	SchemaVersion int    `json:"schemaVersion"`
	RestartID     string `json:"restartId"`
	TaskName      string `json:"taskName"`
	State         string `json:"state"`
	SessionID     int    `json:"sessionId"`
	StoppedPIDs   []int  `json:"stoppedPids"`
	StartedPIDs   []int  `json:"startedPids"`
	Message       string `json:"message"`
}

func reprovisionReadySession(ctx context.Context, options Options, plan runPlan, ready readyStatus, provisioning provisioningPlan, hostHerdr HostHerdr) (connection Connection, resultErr error) {
	provisioning.Mounts = plan.Mounts
	provisioning.Workspaces = plan.Workspaces
	fmt.Fprintf(options.Output, "Existing ready Sandbox run %s; re-running current provisioning in place...\n", plan.ID)
	operation, err := startSessionOperation(plan.RunDirectory, plan.ID, operationKindReprovision,
		"preparing", "Preparing the current retained provisioning inputs.")
	if err != nil {
		return Connection{}, err
	}
	defer func() {
		state := operationStateSucceeded
		phase := "completed"
		message := "Retained provisioning and configuration verification succeeded."
		if resultErr != nil {
			state = operationFailureState(ctx, resultErr)
			phase = operation.Phase
			message = resultErr.Error()
		}
		finished, finishErr := finishSessionOperation(plan.RunDirectory, operation, state, phase, message)
		if finishErr != nil {
			if resultErr == nil {
				connection = Connection{}
				resultErr = finishErr
			} else {
				resultErr = fmt.Errorf("%w; additionally publish retained operation outcome: %v", resultErr, finishErr)
			}
			return
		}
		operation = finished
	}()
	updateOperation := func(phase, message string) error {
		updated, err := updateSessionOperation(plan.RunDirectory, operation, phase, message)
		if err != nil {
			return err
		}
		operation = updated
		return nil
	}

	snapshot, cleanupSnapshot, err := prepareRetainedProvisioningSnapshot(ctx, plan, provisioning)
	if err != nil {
		return Connection{}, err
	}
	defer cleanupSnapshot()
	plan.Workspaces = snapshot.Workspaces
	plan.ActiveWorkspace = snapshot.ActiveWorkspace
	plan.RequiresVisualStudioLayout = snapshot.RequiresVisualStudioLayout
	plan.TradingViewEnabled = snapshot.TradingViewEnabled
	if plan.RequiresVisualStudioLayout {
		if err := updateOperation("visual-studio-layout", "Preparing the required Visual Studio Build Tools layout on the host."); err != nil {
			return Connection{}, err
		}
		fmt.Fprintln(options.Output, "Preparing the required Visual Studio Build Tools layout on the host...")
		if err := prepareVisualStudioLayout(ctx, plan, options.Output); err != nil {
			return Connection{}, err
		}
	}

	if err := updateOperation("connection-verification", "Verifying the retained SSH and Herdr connection."); err != nil {
		return Connection{}, err
	}
	connection, err = writeRunConnection(plan, connectableStatus(connectionStatus(ready)), hostHerdr.commandPath)
	if err != nil {
		return Connection{}, err
	}

	if err := verifySSH(ctx, connection); err != nil {
		return Connection{}, err
	}
	if err := verifyGuestHerdr(ctx, connection); err != nil {
		return Connection{}, err
	}
	if err := updateOperation("development-provisioning", "Running the current Base, user, and project provisioning."); err != nil {
		return Connection{}, err
	}
	if err := runWithRetainedProgress(ctx, plan.StatusDirectory, options.Output, func(progress progressStatus) error {
		return updateOperation(progress.Phase, progress.Message)
	}, func(progressContext context.Context) error {
		return runRetainedProvisioning(progressContext, connection, snapshot)
	}); err != nil {
		return Connection{}, err
	}
	if plan.Tailscale {
		if err := updateOperation("tailscale-identity", "Capturing and verifying the retained Tailscale identity."); err != nil {
			return Connection{}, err
		}
		fmt.Fprintln(options.Output, "Capturing and verifying the retained Tailscale identity...")
		tailscaleContext, cancelTailscale := context.WithTimeout(ctx, tailscaleIdentityTimeout)
		err = captureAndStoreTailscale(tailscaleContext, connection, plan.DataDirectory)
		cancelTailscale()
		if err != nil {
			return Connection{}, err
		}
	}
	if err := updateOperation("configuration-sync", "Reapplying and verifying selected development configuration."); err != nil {
		return Connection{}, err
	}
	writeProvisioningConfiguration(options.Output, "Reapplying and verifying development configuration", plan.Packages, provisioning.CodingAgentSync)
	syncContext, cancelSync := context.WithTimeout(ctx, configurationSyncTimeout)
	err = syncDevelopmentConfiguration(syncContext, connection, plan.WindowsTerminal, plan.Packages, provisioning.CodingAgentSync, snapshot.TradingViewEnabled, plan.WorktreeDirectory != "", snapshot.Directory)
	cancelSync()
	if err != nil {
		return Connection{}, err
	}
	if len(plan.MobileSSHAuthorizedKeys) > 0 {
		if err := updateOperation("mobile-ssh-verification", "Verifying the retained private mobile Herdr endpoint."); err != nil {
			return Connection{}, err
		}
		access, err := verifyMobileSSH(ctx, connection, plan.DataDirectory, plan.MobileSSHAuthorizedKeys)
		if err != nil {
			return Connection{}, err
		}
		connection.MobileAccess = &access
		fmt.Fprintf(options.Output, "Mobile Herdr endpoint verified: %s\n", access.URI)
	}
	if err := updateOperation("ssh-alias", "Publishing the verified reusable SSH target."); err != nil {
		return Connection{}, err
	}
	if err := installRunConnectionAlias(plan.DataDirectory, connection); err != nil {
		return Connection{}, err
	}
	if err := updateOperation("verification", "Verifying the retained Herdr server after provisioning."); err != nil {
		return Connection{}, err
	}
	if err := verifyGuestHerdr(ctx, connection); err != nil {
		return Connection{}, fmt.Errorf("verify guest Herdr after retained provisioning: %w", err)
	}
	return connection, nil
}

func retainedRunPlan(active activeSession, provisioning provisioningPlan, memoryMB int) (runPlan, error) {
	plan, differences, err := retainedRunPlanDetails(active, provisioning, memoryMB)
	if err != nil {
		return runPlan{}, err
	}
	if len(differences) > 0 {
		if len(differences) == 1 && differences[0] == "Tailscale identity selection" {
			return runPlan{}, errors.New("current Tailscale identity selection differs from the ready Sandbox; run `sandbox down` before `up` to launch the changed plan")
		}
		return runPlan{}, fmt.Errorf("current %s differ from the ready Sandbox; run `sandbox down` before `up` to launch the changed plan", strings.Join(differences, ", "))
	}
	return plan, nil
}

func retainedRunPlanDetails(active activeSession, provisioning provisioningPlan, memoryMB int) (runPlan, []string, error) {
	differences := make([]string, 0, 5)
	if active.Tailscale != provisioning.Tailscale {
		differences = append(differences, "Tailscale identity selection")
	}
	dataDirectory := filepath.Dir(active.ConfigPath)
	runDirectory := dataDirectory
	dataDirectory = filepath.Dir(filepath.Dir(runDirectory))
	inputDirectory, err := canonicalMappedDirectory(filepath.Join(runDirectory, "input"))
	if err != nil {
		return runPlan{}, nil, err
	}
	statusDirectory, err := canonicalMappedDirectory(filepath.Join(runDirectory, "status"))
	if err != nil {
		return runPlan{}, nil, err
	}
	cacheDirectory, err := effectiveCacheDirectory(provisioning.CacheDirectory)
	if err != nil {
		return runPlan{}, nil, err
	}
	cacheDirectory, err = canonicalMappedDirectory(cacheDirectory)
	if err != nil {
		return runPlan{}, nil, err
	}
	worktreeDirectory := provisioning.WorktreeDirectory
	if worktreeDirectory != "" {
		worktreeDirectory, err = canonicalMappedDirectory(worktreeDirectory)
		if err != nil {
			return runPlan{}, nil, err
		}
	}
	mounts, err := canonicalMountPlans(provisioning.Mounts)
	if err != nil {
		return runPlan{}, nil, err
	}
	workspaces, err := canonicalWorkspacePlans(provisioning.Workspaces)
	if err != nil {
		return runPlan{}, nil, err
	}
	runningMobileSSHAuthorizedKeys, err := readMobileSSHAuthorizedKeysInput(inputDirectory)
	if err != nil {
		return runPlan{}, nil, err
	}
	if !sameMobileSSHAuthorizedKeys(runningMobileSSHAuthorizedKeys, provisioning.MobileSSHAuthorizedKeys) {
		differences = append(differences, "mobile SSH authorized keys")
	}
	if err := validatePhysicalMappings(dataDirectory, inputDirectory, statusDirectory, cacheDirectory, worktreeDirectory, mounts, workspaces); err != nil {
		return runPlan{}, nil, err
	}
	expectedConfig, err := renderConfigWithWorktreeDirectory(inputDirectory, statusDirectory, cacheDirectory, worktreeDirectory, mounts, workspaces, memoryMB, provisioning.AudioOutput, provisioning.AudioInput)
	if err != nil {
		return runPlan{}, nil, err
	}
	actualConfig, err := readRetainedWSB(active.ConfigPath)
	if err != nil {
		return runPlan{}, nil, err
	}
	if !bytes.Equal(actualConfig, expectedConfig) {
		launchDifferences, differenceErr := describeWSBLaunchDifferences(actualConfig, expectedConfig)
		if differenceErr != nil {
			return runPlan{}, nil, differenceErr
		}
		differences = append(differences, launchDifferences...)
	}
	return runPlan{
		ID:                      active.RunID,
		DataDirectory:           dataDirectory,
		RunDirectory:            runDirectory,
		InputDirectory:          inputDirectory,
		StatusDirectory:         statusDirectory,
		CacheDirectory:          cacheDirectory,
		WorktreeDirectory:       worktreeDirectory,
		Tailscale:               active.Tailscale,
		MobileSSHAuthorizedKeys: runningMobileSSHAuthorizedKeys,
		Packages:                provisioning.Packages,
		CodingAgentSync:         provisioning.CodingAgentSync,
		WindowsTerminal:         provisioning.WindowsTerminal,
		Mounts:                  mounts,
		ConfigPath:              active.ConfigPath,
		PrivateKeyPath:          filepath.Join(dataDirectory, "identity", "id_ed25519"),
		PublicKeyPath:           filepath.Join(dataDirectory, "identity", "id_ed25519.pub"),
		Workspaces:              workspaces,
		SandboxExecutable:       active.ExecutablePath,
	}, differences, nil
}

func readRetainedWSB(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("inspect retained Sandbox configuration: %w", err)
	}
	reparse, err := fileInfoIsReparsePoint(info)
	if err != nil {
		return nil, fmt.Errorf("inspect retained Sandbox configuration reparse state: %w", err)
	}
	if reparse || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > maximumWSBSize {
		return nil, errors.New("retained Sandbox configuration is not one bounded regular non-reparse file")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read retained Sandbox configuration: %w", err)
	}
	return data, nil
}

func prepareRetainedProvisioningSnapshot(ctx context.Context, plan runPlan, provisioning provisioningPlan) (provisioningSnapshot, func(), error) {
	parent := filepath.Join(plan.RunDirectory, "reprovision")
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return provisioningSnapshot{}, func() {}, fmt.Errorf("create retained provisioning state directory: %w", err)
	}
	parent, err := canonicalMappedDirectory(parent)
	if err != nil {
		return provisioningSnapshot{}, func() {}, err
	}
	runID, err := newRunID()
	if err != nil {
		return provisioningSnapshot{}, func() {}, err
	}
	root := filepath.Join(parent, runID)
	if err := os.Mkdir(root, 0o700); err != nil {
		return provisioningSnapshot{}, func() {}, fmt.Errorf("create retained provisioning snapshot: %w", err)
	}
	cleanup := func() {
		_ = os.RemoveAll(root)
		_ = os.Remove(parent)
	}
	snapshot, err := prepareProvisioningSnapshot(ctx, root, filepath.Join(root, "provisioning"), provisioning)
	if err != nil {
		cleanup()
		return provisioningSnapshot{}, func() {}, err
	}
	return snapshot, cleanup, nil
}

func runRetainedProvisioning(ctx context.Context, connection Connection, snapshot provisioningSnapshot) error {
	restartID, err := newRunID()
	if err != nil {
		return fmt.Errorf("create retained Explorer restart identity: %w", err)
	}
	restartTaskName := "HerdrSandbox-ExplorerRestart-" + restartID
	restartStatusPath := filepath.Join(connection.StatusDirectory, "explorer-restart-"+restartID+".json")
	if err := removeExplorerRestartStatus(restartStatusPath); err != nil {
		return err
	}
	finishRestart := func(required bool) (bool, error) {
		_, found, readErr := readOptionalStatus[explorerRestartStatus](restartStatusPath)
		var waitErr error
		if readErr != nil {
			waitErr = fmt.Errorf("read retained Explorer restart status before wait: %w", readErr)
		} else if !found {
			if required {
				waitErr = errors.New("retained provisioning did not publish scheduled Explorer restart status")
			}
		} else {
			restartContext, cancelRestart := context.WithTimeout(context.WithoutCancel(ctx), explorerRestartStatusTimeout)
			_, waitErr = waitForExplorerRestartStatus(restartContext, restartStatusPath, restartID, restartTaskName)
			cancelRestart()
		}
		cleanupContext, cancelCleanup := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		cleanupErr := cleanupRetainedExplorerRestartTask(cleanupContext, connection, restartTaskName)
		cancelCleanup()
		return found, errors.Join(waitErr, cleanupErr)
	}
	finishError := func(cause error) error {
		_, finishErr := finishRestart(false)
		return errors.Join(cause, finishErr)
	}
	archive, err := buildReprovisionArchive(snapshot)
	if err != nil {
		return err
	}
	defer clear(archive)
	digest := fmt.Sprintf("%x", sha256.Sum256(archive))
	projectCount := workspaceProvisioningProfileCount(snapshot.Workspaces)
	launcher := buildReprovisionLauncher(digest, len(archive), projectCount, restartID, restartTaskName)
	output, err := runSSHArchivePowerShell(ctx, connection, archive, launcher, "run retained provisioning")
	if err != nil {
		return finishError(err)
	}
	result, err := decodeReprovisionResult(output)
	if err != nil {
		return finishError(err)
	}
	if result.SchemaVersion != reprovisionResultSchema || result.ArchiveSHA256 != digest || result.ProjectCount != projectCount {
		return finishError(fmt.Errorf("verify retained provisioning result: schema=%d digest=%q projects=%d", result.SchemaVersion, result.ArchiveSHA256, result.ProjectCount))
	}
	if !result.ExplorerRestartScheduled {
		if result.ExplorerRestartID != "" || result.ExplorerRestartTaskName != "" {
			return finishError(errors.New("retained provisioning returned Explorer restart identity without scheduling a restart"))
		}
		found, finishErr := finishRestart(false)
		if found {
			return errors.Join(errors.New("retained provisioning published Explorer restart status without scheduling a restart"), finishErr)
		}
		return finishErr
	}
	if result.ExplorerRestartID != restartID || result.ExplorerRestartTaskName != restartTaskName {
		return finishError(errors.New("retained provisioning returned unexpected Explorer restart identity"))
	}
	_, err = finishRestart(true)
	return err
}

func cleanupRetainedExplorerRestartTask(ctx context.Context, connection Connection, taskName string) error {
	if !strings.HasPrefix(taskName, "HerdrSandbox-ExplorerRestart-") || len(taskName) > 128 {
		return errors.New("retained Explorer restart task name is invalid")
	}
	script := fmt.Sprintf(`$ErrorActionPreference = 'Stop'
$service = New-Object -ComObject 'Schedule.Service'
$service.Connect()
$root = $service.GetFolder('\')
$tasks = @($root.GetTasks(1) | Where-Object { [string]$_.Name -ceq '%s' })
if ($tasks.Count -gt 1) { throw 'Explorer restart task identity is ambiguous.' }
if ($tasks.Count -eq 1) {
    foreach ($instance in @($tasks[0].GetInstances(0))) { $instance.Stop() }
    $root.DeleteTask('%s', 0)
}
$remaining = @($root.GetTasks(1) | Where-Object { [string]$_.Name -ceq '%s' })
if ($remaining.Count -ne 0) { throw 'Explorer restart task cleanup verification failed.' }
Write-Output 'verified'`, taskName, taskName, taskName)
	output, err := runSSHPowerShell(ctx, connection, bytes.NewReader(nil), script, "clean retained Explorer restart task", 1024)
	if err != nil {
		return err
	}
	if strings.TrimSpace(string(output)) != "verified" {
		return fmt.Errorf("clean retained Explorer restart task returned unexpected output: %s", boundedText(output))
	}
	return nil
}

func removeExplorerRestartStatus(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect retained Explorer restart status: %w", err)
	}
	reparse, err := fileInfoIsReparsePoint(info)
	if err != nil {
		return fmt.Errorf("inspect retained Explorer restart status reparse state: %w", err)
	}
	if reparse || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > maxStatusFileBytes {
		return errors.New("retained Explorer restart status is not one bounded regular non-reparse file")
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("remove prior retained Explorer restart status: %w", err)
	}
	return nil
}

func waitForExplorerRestartStatus(ctx context.Context, path, restartID, taskName string) (explorerRestartStatus, error) {
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		status, found, err := readOptionalStatus[explorerRestartStatus](path)
		if err != nil {
			return explorerRestartStatus{}, fmt.Errorf("read retained Explorer restart status: %w", err)
		}
		if found {
			if err := status.validate(); err != nil {
				return explorerRestartStatus{}, fmt.Errorf("validate retained Explorer restart status: %w", err)
			}
			if status.RestartID != restartID || status.TaskName != taskName {
				return explorerRestartStatus{}, errors.New("retained Explorer restart status identity does not match the request")
			}
			switch status.State {
			case explorerRestartStatusSucceeded:
				return status, nil
			case explorerRestartStatusFailed:
				return explorerRestartStatus{}, fmt.Errorf("retained Explorer restart failed: %s", status.Message)
			}
		}
		select {
		case <-ctx.Done():
			return explorerRestartStatus{}, fmt.Errorf("wait for retained Explorer restart status: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}

func (status explorerRestartStatus) validate() error {
	if status.SchemaVersion != explorerRestartStatusSchema || status.SessionID <= 0 ||
		strings.TrimSpace(status.RestartID) != status.RestartID || status.RestartID == "" || len(status.RestartID) > 128 ||
		status.TaskName != "HerdrSandbox-ExplorerRestart-"+status.RestartID {
		return errors.New("Explorer restart status schema or session is invalid")
	}
	validatePIDs := func(name string, values []int) error {
		seen := make(map[int]bool, len(values))
		for _, value := range values {
			if value <= 0 || seen[value] {
				return fmt.Errorf("Explorer restart status %s PIDs are invalid", name)
			}
			seen[value] = true
		}
		return nil
	}
	if err := validatePIDs("stopped", status.StoppedPIDs); err != nil {
		return err
	}
	if err := validatePIDs("started", status.StartedPIDs); err != nil {
		return err
	}
	switch status.State {
	case explorerRestartStatusPending:
		if len(status.StoppedPIDs) == 0 || len(status.StartedPIDs) != 0 || status.Message != "" {
			return errors.New("pending Explorer restart status has terminal data")
		}
	case explorerRestartStatusSucceeded:
		if len(status.StoppedPIDs) == 0 || len(status.StartedPIDs) == 0 || status.Message != "" {
			return errors.New("succeeded Explorer restart status is incomplete")
		}
		stopped := make(map[int]bool, len(status.StoppedPIDs))
		for _, value := range status.StoppedPIDs {
			stopped[value] = true
		}
		for _, value := range status.StartedPIDs {
			if stopped[value] {
				return errors.New("Explorer restart status reused one stopped PID")
			}
		}
	case explorerRestartStatusFailed:
		if strings.TrimSpace(status.Message) == "" || len(status.Message) > 4096 {
			return errors.New("failed Explorer restart status message is invalid")
		}
	default:
		return fmt.Errorf("Explorer restart status state is invalid: %q", status.State)
	}
	return nil
}

func buildReprovisionArchive(snapshot provisioningSnapshot) ([]byte, error) {
	var buffer bytes.Buffer
	archive := zip.NewWriter(&buffer)
	total := int64(0)
	add := func(source, destination string) error {
		info, err := os.Lstat(source)
		if err != nil {
			return err
		}
		reparse, err := fileInfoIsReparsePoint(info)
		if err != nil {
			return err
		}
		if reparse || !info.Mode().IsRegular() || info.Size() > maximumConfigurationFileSize || total+info.Size() > maximumConfigurationSize {
			return fmt.Errorf("retained provisioning source is unsafe or too large: %s", source)
		}
		data, err := os.ReadFile(source)
		if err != nil {
			return err
		}
		writer, err := archive.Create(strings.ReplaceAll(destination, `\`, "/"))
		if err != nil {
			return err
		}
		if _, err := writer.Write(data); err != nil {
			return err
		}
		total += info.Size()
		return nil
	}
	files := []struct {
		source      string
		destination string
	}{
		{filepath.Join(snapshot.Directory, baseProvisioningName), baseProvisioningName},
		{filepath.Join(snapshot.Directory, stackProvisioningName), stackProvisioningName},
		{filepath.Join(snapshot.Directory, userProvisioningName), userProvisioningName},
		{snapshot.ProcessOwnerPath, provisioningProcessName},
		{snapshot.PackagePlanPath, wingetPackagePlanFileName},
		{snapshot.WorkspaceManifestPath, workspaceManifestName},
	}
	for _, workspace := range snapshot.Workspaces {
		if workspace.ProvisioningPath == "" {
			continue
		}
		files = append(files, struct {
			source      string
			destination string
		}{filepath.Join(snapshot.ProjectScriptsDirectory, workspace.Name+".ps1"), filepath.Join("projects", workspace.Name+".ps1")})
	}
	for _, file := range files {
		if err := add(file.source, file.destination); err != nil {
			_ = archive.Close()
			return nil, fmt.Errorf("archive retained provisioning input %s: %w", file.destination, err)
		}
	}
	if err := archive.Close(); err != nil {
		return nil, fmt.Errorf("finalize retained provisioning archive: %w", err)
	}
	return buffer.Bytes(), nil
}

func workspaceProvisioningProfileCount(workspaces []workspacePlan) int {
	count := 0
	for _, workspace := range workspaces {
		if workspace.ProvisioningPath != "" {
			count++
		}
	}
	return count
}

func buildReprovisionLauncher(expectedDigest string, archiveLength, projectCount int, explorerRestartID, explorerRestartTaskName string) string {
	staging := guestArchiveStagingPowerShell("reprovision-"+expectedDigest[:16], "Retained provisioning")
	return fmt.Sprintf(`$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'
%s
$expectedArchiveLength = [long]%d
try {
    $inputStream = [Console]::OpenStandardInput()
    $outputStream = [IO.File]::Open($archive, [IO.FileMode]::CreateNew, [IO.FileAccess]::Write, [IO.FileShare]::None)
    try {
        $remaining = $expectedArchiveLength
        $buffer = New-Object byte[] 65536
        while ($remaining -gt 0) {
            $requested = [int][Math]::Min([long]$buffer.Length, $remaining)
            $read = $inputStream.Read($buffer, 0, $requested)
            if ($read -le 0) { throw "Retained provisioning archive ended with $remaining bytes missing." }
            $outputStream.Write($buffer, 0, $read)
            $remaining -= $read
        }
        $outputStream.Flush($true)
    } finally {
        $outputStream.Dispose()
    }
    $digest = (Get-FileHash -LiteralPath $archive -Algorithm SHA256).Hash.ToLowerInvariant()
    if ($digest -cne '%s') { throw 'Retained provisioning archive SHA-256 mismatch.' }
    New-Item -ItemType Directory -Path $expanded -Force | Out-Null
    Expand-Archive -LiteralPath $archive -DestinationPath $expanded
    Assert-GuestArchiveTree
    foreach ($name in @('base.ps1', 'stacks.ps1', 'user.ps1', 'provisioning-process.cs', 'winget-packages.json', 'workspaces.json')) {
        if (-not (Test-Path -LiteralPath (Join-Path $expanded $name) -PathType Leaf)) {
            throw "Retained provisioning input is missing: $name"
        }
    }
    $projectsDirectory = Join-Path $expanded 'projects'
    New-Item -ItemType Directory -Path $projectsDirectory -Force | Out-Null
    $projects = @(Get-ChildItem -LiteralPath $projectsDirectory -File -Filter '*.ps1')
    if ($projects.Count -ne %d) { throw "Retained provisioning project count is $($projects.Count)." }
    $reparse = @(Get-ChildItem -LiteralPath $expanded -Force -Recurse | Where-Object { ($_.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0 })
    if ($reparse.Count -ne 0) { throw 'Retained provisioning input contains a reparse point.' }
    $env:HERDR_SANDBOX_STATUS_DIRECTORY = 'C:\SandboxStatus'
    $env:HERDR_SANDBOX_EXPLORER_RESTART_ID = '%s'
    $env:HERDR_SANDBOX_EXPLORER_RESTART_TASK_NAME = '%s'
    Remove-Item Env:HERDR_SANDBOX_EXPLORER_RESTART_SCHEDULED -ErrorAction SilentlyContinue
    $captured = @()
    try {
        $captured = @(& (Join-Path $expanded 'base.ps1') -Phase 'Development' -ProjectProvisioningDirectory $projectsDirectory -WorkspacesDirectory 'C:\Workspaces' -PackagePlanPath (Join-Path $expanded 'winget-packages.json') -UserProvisioningPath (Join-Path $expanded 'user.ps1') -ProcessOwnerPath (Join-Path $expanded 'provisioning-process.cs') *>&1)
    } catch {
        $detail = @($captured | Select-Object -Last 20 | ForEach-Object { [string]$_ })
        $detail += [string]$_.Exception.Message
        [Console]::Error.WriteLine(($detail -join [Environment]::NewLine))
        throw
    }
    $explorerRestartScheduled = [string]$env:HERDR_SANDBOX_EXPLORER_RESTART_SCHEDULED -ceq '1'
    $explorerRestartID = if ($explorerRestartScheduled) { [string]$env:HERDR_SANDBOX_EXPLORER_RESTART_ID } else { '' }
    $explorerRestartTaskName = if ($explorerRestartScheduled) { [string]$env:HERDR_SANDBOX_EXPLORER_RESTART_TASK_NAME } else { '' }
    Write-Output ([ordered]@{ schemaVersion = %d; archiveSha256 = $digest; projectCount = $projects.Count; explorerRestartScheduled = $explorerRestartScheduled; explorerRestartId = $explorerRestartID; explorerRestartTaskName = $explorerRestartTaskName } | ConvertTo-Json -Compress)
} finally {
    Remove-Item Env:HERDR_SANDBOX_EXPLORER_RESTART_SCHEDULED -ErrorAction SilentlyContinue
    Remove-Item Env:HERDR_SANDBOX_EXPLORER_RESTART_ID -ErrorAction SilentlyContinue
    Remove-Item Env:HERDR_SANDBOX_EXPLORER_RESTART_TASK_NAME -ErrorAction SilentlyContinue
    Remove-Item Env:HERDR_SANDBOX_STATUS_DIRECTORY -ErrorAction SilentlyContinue
    Remove-GuestArchiveStaging
}
exit 0`, staging, archiveLength, expectedDigest, projectCount, explorerRestartID, explorerRestartTaskName, reprovisionResultSchema)
}

func decodeReprovisionResult(data []byte) (reprovisionResult, error) {
	data = bytes.TrimSpace(data)
	if err := validateExactJSONObjectShape(data, "retained provisioning result", []string{
		"schemaVersion", "archiveSha256", "projectCount", "explorerRestartScheduled", "explorerRestartId", "explorerRestartTaskName",
	}); err != nil {
		return reprovisionResult{}, err
	}
	var result reprovisionResult
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil {
		return reprovisionResult{}, fmt.Errorf("decode retained provisioning result: %w: %s", err, boundedText(data))
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return reprovisionResult{}, errors.New("decode retained provisioning result: trailing JSON data")
	}
	return result, nil
}
