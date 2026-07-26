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
)

const (
	reprovisionResultSchema = 1
	maximumWSBSize          = 1024 * 1024
)

type reprovisionResult struct {
	SchemaVersion int    `json:"schemaVersion"`
	ArchiveSHA256 string `json:"archiveSha256"`
	ProjectCount  int    `json:"projectCount"`
}

func reprovisionReadySession(ctx context.Context, options Options, active activeSession, provisioning provisioningPlan, memoryMB int, herdrExecutable string, release herdrRelease) (Connection, error) {
	plan, err := retainedRunPlan(active, provisioning, memoryMB)
	if err != nil {
		return Connection{}, err
	}
	provisioning.Workspaces = plan.Workspaces
	fmt.Fprintf(options.Output, "Existing ready Sandbox run %s; re-running current provisioning in place...\n", active.RunID)

	snapshot, cleanupSnapshot, err := prepareRetainedProvisioningSnapshot(ctx, plan, provisioning)
	if err != nil {
		return Connection{}, err
	}
	defer cleanupSnapshot()
	plan.Workspaces = snapshot.Workspaces
	plan.ActiveWorkspace = snapshot.ActiveWorkspace
	plan.RequiresVisualStudioLayout = snapshot.RequiresVisualStudioLayout
	if plan.RequiresVisualStudioLayout {
		fmt.Fprintln(options.Output, "Preparing the required Visual Studio Build Tools layout on the host...")
		if err := prepareVisualStudioLayout(ctx, plan, options.Output); err != nil {
			return Connection{}, err
		}
	}

	ready, found, err := readOptionalStatus[readyStatus](filepath.Join(plan.StatusDirectory, readyFileName))
	if err != nil {
		return Connection{}, fmt.Errorf("read retained Sandbox ready status: %w", err)
	}
	if !found {
		return Connection{}, errors.New("retained Sandbox ready status is missing")
	}
	if err := ready.validate(); err != nil {
		return Connection{}, fmt.Errorf("validate retained Sandbox ready status: %w", err)
	}
	if ready.HerdrVersion != release.Version || ready.HerdrProtocol != release.Protocol {
		return Connection{}, fmt.Errorf("retained guest Herdr identity = %q protocol %d, required %q protocol %d", ready.HerdrVersion, ready.HerdrProtocol, release.Version, release.Protocol)
	}
	connection, err := writeRunConnection(plan, connectableStatus(connectionStatus(ready)), herdrExecutable)
	if err != nil {
		return Connection{}, err
	}

	runContext, cancel := context.WithTimeout(ctx, options.Timeout)
	defer cancel()
	if err := verifySSH(runContext, connection); err != nil {
		return Connection{}, err
	}
	if err := verifyGuestHerdr(runContext, connection); err != nil {
		return Connection{}, err
	}
	if err := runRetainedProvisioning(runContext, connection, snapshot); err != nil {
		return Connection{}, err
	}
	if plan.Tailscale {
		fmt.Fprintln(options.Output, "Capturing and verifying the retained Tailscale identity...")
		tailscaleContext, cancelTailscale := context.WithTimeout(runContext, tailscaleIdentityTimeout)
		err = captureAndStoreTailscale(tailscaleContext, connection, plan.DataDirectory)
		cancelTailscale()
		if err != nil {
			return Connection{}, err
		}
	}
	fmt.Fprintf(options.Output, "Reapplying and verifying selected development configuration: %s...\n", provisioningConfigurationSummary(plan.Packages, provisioning.CodingAgentSync))
	syncContext, cancelSync := context.WithTimeout(runContext, configurationSyncTimeout)
	err = syncDevelopmentConfiguration(syncContext, connection, plan.WindowsTerminal, plan.Packages, provisioning.CodingAgentSync, snapshot.Directory)
	cancelSync()
	if err != nil {
		return Connection{}, err
	}
	if err := installRunConnectionAlias(plan.DataDirectory, connection); err != nil {
		return Connection{}, err
	}
	if err := verifyGuestHerdr(runContext, connection); err != nil {
		return Connection{}, fmt.Errorf("verify guest Herdr after retained provisioning: %w", err)
	}
	fmt.Fprintf(options.Output, "Retained provisioning verified for %d workspace(s).\n", len(snapshot.Workspaces))
	fmt.Fprintf(options.Output, "Remote attach: herdr --remote %s\n", connection.SSHTarget)
	return connection, nil
}

func retainedRunPlan(active activeSession, provisioning provisioningPlan, memoryMB int) (runPlan, error) {
	if active.Tailscale != provisioning.Tailscale {
		return runPlan{}, errors.New("current Tailscale identity selection differs from the ready Sandbox; run `herdr-sandbox down` before `up` to launch the changed plan")
	}
	dataDirectory := filepath.Dir(active.ConfigPath)
	runDirectory := dataDirectory
	dataDirectory = filepath.Dir(filepath.Dir(runDirectory))
	inputDirectory, err := canonicalMappedDirectory(filepath.Join(runDirectory, "input"))
	if err != nil {
		return runPlan{}, err
	}
	statusDirectory, err := canonicalMappedDirectory(filepath.Join(runDirectory, "status"))
	if err != nil {
		return runPlan{}, err
	}
	cacheDirectory, err := effectiveCacheDirectory(provisioning.CacheDirectory)
	if err != nil {
		return runPlan{}, err
	}
	cacheDirectory, err = canonicalMappedDirectory(cacheDirectory)
	if err != nil {
		return runPlan{}, err
	}
	workspaces := append([]workspacePlan(nil), provisioning.Workspaces...)
	for index := range workspaces {
		workspaces[index].HostDirectory, err = canonicalMappedDirectory(workspaces[index].HostDirectory)
		if err != nil {
			return runPlan{}, fmt.Errorf("workspace %q: %w", workspaces[index].Name, err)
		}
	}
	expectedConfig, err := renderConfig(inputDirectory, statusDirectory, cacheDirectory, workspaces, memoryMB)
	if err != nil {
		return runPlan{}, err
	}
	actualConfig, err := readRetainedWSB(active.ConfigPath)
	if err != nil {
		return runPlan{}, err
	}
	if !bytes.Equal(actualConfig, expectedConfig) {
		return runPlan{}, errors.New("current memory, cache, or workspace mappings differ from the ready Sandbox; run `herdr-sandbox down` before `up` to launch the changed plan")
	}
	return runPlan{
		ID:                active.RunID,
		DataDirectory:     dataDirectory,
		RunDirectory:      runDirectory,
		InputDirectory:    inputDirectory,
		StatusDirectory:   statusDirectory,
		CacheDirectory:    cacheDirectory,
		Tailscale:         active.Tailscale,
		Packages:          provisioning.Packages,
		CodingAgentSync:   provisioning.CodingAgentSync,
		WindowsTerminal:   provisioning.WindowsTerminal,
		ConfigPath:        active.ConfigPath,
		PrivateKeyPath:    filepath.Join(dataDirectory, "identity", "id_ed25519"),
		PublicKeyPath:     filepath.Join(dataDirectory, "identity", "id_ed25519.pub"),
		Workspaces:        workspaces,
		SandboxExecutable: active.ExecutablePath,
	}, nil
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
	archive, err := buildReprovisionArchive(snapshot)
	if err != nil {
		return err
	}
	digest := fmt.Sprintf("%x", sha256.Sum256(archive))
	launcher := buildReprovisionLauncher(digest, len(archive), len(snapshot.Workspaces))
	output, err := runSSHArchivePowerShell(ctx, connection, archive, launcher, "run retained provisioning")
	if err != nil {
		return err
	}
	result, err := decodeReprovisionResult(output)
	if err != nil {
		return err
	}
	if result.SchemaVersion != reprovisionResultSchema || result.ArchiveSHA256 != digest || result.ProjectCount != len(snapshot.Workspaces) {
		return fmt.Errorf("verify retained provisioning result: schema=%d digest=%q projects=%d", result.SchemaVersion, result.ArchiveSHA256, result.ProjectCount)
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
		{snapshot.PackagePlanPath, wingetPackagePlanFileName},
		{snapshot.WorkspaceManifestPath, workspaceManifestName},
	}
	for _, workspace := range snapshot.Workspaces {
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

func buildReprovisionLauncher(expectedDigest string, archiveLength, projectCount int) string {
	return fmt.Sprintf(`$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'
$archive = Join-Path $env:TEMP 'herdr-sandbox-reprovision.zip'
$expanded = Join-Path 'C:\HerdrSandbox\staging' 'reprovision-%s'
$expectedArchiveLength = [long]%d
Remove-Item -LiteralPath $archive -Force -ErrorAction SilentlyContinue
Remove-Item -LiteralPath $expanded -Recurse -Force -ErrorAction SilentlyContinue
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
    foreach ($name in @('base.ps1', 'stacks.ps1', 'winget-packages.json', 'workspaces.json')) {
        if (-not (Test-Path -LiteralPath (Join-Path $expanded $name) -PathType Leaf)) {
            throw "Retained provisioning input is missing: $name"
        }
    }
    $projects = @(Get-ChildItem -LiteralPath (Join-Path $expanded 'projects') -File -Filter '*.ps1')
    if ($projects.Count -ne %d) { throw "Retained provisioning project count is $($projects.Count)." }
    $reparse = @(Get-ChildItem -LiteralPath $expanded -Force -Recurse | Where-Object { ($_.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0 })
    if ($reparse.Count -ne 0) { throw 'Retained provisioning input contains a reparse point.' }
    $env:HERDR_SANDBOX_STATUS_DIRECTORY = 'C:\SandboxStatus'
    $captured = @()
    try {
        $captured = @(& (Join-Path $expanded 'base.ps1') -Phase 'Development' -ProjectProvisioningDirectory (Join-Path $expanded 'projects') -WorkspacesDirectory 'C:\Workspaces' -PackagePlanPath (Join-Path $expanded 'winget-packages.json') *>&1)
    } catch {
        $detail = @($captured | Select-Object -Last 20 | ForEach-Object { [string]$_ })
        $detail += [string]$_.Exception.Message
        [Console]::Error.WriteLine(($detail -join [Environment]::NewLine))
        throw
    }
    Write-Output ([ordered]@{ schemaVersion = %d; archiveSha256 = $digest; projectCount = $projects.Count } | ConvertTo-Json -Compress)
} finally {
    Remove-Item Env:HERDR_SANDBOX_STATUS_DIRECTORY -ErrorAction SilentlyContinue
    Remove-Item -LiteralPath $archive -Force -ErrorAction SilentlyContinue
    Remove-Item -LiteralPath $expanded -Recurse -Force -ErrorAction SilentlyContinue
}
exit 0`, expectedDigest[:16], archiveLength, expectedDigest, projectCount, reprovisionResultSchema)
}

func decodeReprovisionResult(data []byte) (reprovisionResult, error) {
	var result reprovisionResult
	decoder := json.NewDecoder(bytes.NewReader(bytes.TrimSpace(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil {
		return reprovisionResult{}, fmt.Errorf("decode retained provisioning result: %w: %s", err, boundedText(data))
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return reprovisionResult{}, errors.New("decode retained provisioning result: trailing JSON data")
	}
	return result, nil
}
