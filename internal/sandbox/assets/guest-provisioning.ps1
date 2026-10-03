$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'
try {
    $inputStream = [Console]::OpenStandardInput()
    $outputStream = [IO.File]::Open($archive, [IO.FileMode]::CreateNew, [IO.FileAccess]::Write, [IO.FileShare]::None)
    try {
        $remaining = $expectedArchiveLength
        $buffer = New-Object byte[] 65536
        while ($remaining -gt 0) {
            $requested = [int][Math]::Min([long]$buffer.Length, $remaining)
            $read = $inputStream.Read($buffer, 0, $requested)
            if ($read -le 0) { throw "Provisioning archive ended with $remaining bytes missing." }
            $outputStream.Write($buffer, 0, $read)
            $remaining -= $read
        }
        $outputStream.Flush($true)
    } finally {
        $outputStream.Dispose()
    }
    $digest = (Get-FileHash -LiteralPath $archive -Algorithm SHA256).Hash.ToLowerInvariant()
    if ($digest -cne $expectedArchiveDigest) { throw 'Provisioning archive SHA-256 mismatch.' }
    New-Item -ItemType Directory -Path $expanded -Force | Out-Null
    Expand-Archive -LiteralPath $archive -DestinationPath $expanded
    Assert-GuestArchiveTree
    foreach ($name in @('base.ps1', 'stacks.ps1', 'user.ps1', 'provisioning-process.cs', 'playwright-access.ps1', 'apify-access.ps1', 'finalize-provisioning.ps1', 'winget-packages.json', 'tool-versions.json', 'workspaces.json')) {
        if (-not (Test-Path -LiteralPath (Join-Path $expanded $name) -PathType Leaf)) {
            throw "Provisioning input is missing: $name"
        }
    }
    $projectsDirectory = Join-Path $expanded 'projects'
    New-Item -ItemType Directory -Path $projectsDirectory -Force | Out-Null
    $projects = @(Get-ChildItem -LiteralPath $projectsDirectory -File -Filter '*.ps1')
    if ($projects.Count -ne $expectedProjectCount) { throw "Provisioning project count is $($projects.Count)." }
    $env:HERDR_SANDBOX_STATUS_DIRECTORY = 'C:\SandboxStatus'
    $env:HERDR_SANDBOX_EXPLORER_RESTART_ID = $explorerRestartID
    $env:HERDR_SANDBOX_EXPLORER_RESTART_TASK_NAME = $explorerRestartTaskName
    Remove-Item Env:HERDR_SANDBOX_EXPLORER_RESTART_SCHEDULED -ErrorAction SilentlyContinue
    $captured = @()
    try {
        if ($provisioningPhase -ceq 'Finalize') {
            $captured = @(& (Join-Path $expanded 'finalize-provisioning.ps1') -WorkspaceManifestPath (Join-Path $expanded 'workspaces.json') -ProcessOwnerPath (Join-Path $expanded 'provisioning-process.cs') *>&1)
        } else {
            $captured = @(& (Join-Path $expanded 'base.ps1') -Phase 'Development' -ProjectProvisioningDirectory $projectsDirectory -WorkspacesDirectory 'C:\Workspaces' -PackagePlanPath (Join-Path $expanded 'winget-packages.json') -UserProvisioningPath (Join-Path $expanded 'user.ps1') -ProcessOwnerPath (Join-Path $expanded 'provisioning-process.cs') *>&1)
        }
    } catch {
        $detail = @($captured | Select-Object -Last 20 | ForEach-Object { [string]$_ })
        $detail += [string]$_.Exception.Message
        throw ($detail -join [Environment]::NewLine)
    }
    $explorerRestartScheduled = [string]$env:HERDR_SANDBOX_EXPLORER_RESTART_SCHEDULED -ceq '1'
    $restartID = if ($explorerRestartScheduled) { [string]$env:HERDR_SANDBOX_EXPLORER_RESTART_ID } else { '' }
    $restartTaskName = if ($explorerRestartScheduled) { [string]$env:HERDR_SANDBOX_EXPLORER_RESTART_TASK_NAME } else { '' }
    Write-Output ([ordered]@{ schemaVersion = 2; archiveSha256 = $digest; projectCount = $projects.Count; explorerRestartScheduled = $explorerRestartScheduled; explorerRestartId = $restartID; explorerRestartTaskName = $restartTaskName } | ConvertTo-Json -Compress)
} finally {
    Remove-Item Env:HERDR_SANDBOX_EXPLORER_RESTART_SCHEDULED -ErrorAction SilentlyContinue
    Remove-Item Env:HERDR_SANDBOX_EXPLORER_RESTART_ID -ErrorAction SilentlyContinue
    Remove-Item Env:HERDR_SANDBOX_EXPLORER_RESTART_TASK_NAME -ErrorAction SilentlyContinue
    Remove-Item Env:HERDR_SANDBOX_STATUS_DIRECTORY -ErrorAction SilentlyContinue
    Remove-GuestArchiveStaging
}
exit 0
