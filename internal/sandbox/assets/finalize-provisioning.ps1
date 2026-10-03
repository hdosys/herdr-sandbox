param(
    [Parameter(Mandatory = $true)][string]$WorkspaceManifestPath,
    [Parameter(Mandatory = $true)][string]$ProcessOwnerPath
)

Set-StrictMode -Version 2.0
$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'

function Invoke-WorkspaceCommand {
    param([Parameter(Mandatory = $true)][string[]]$Arguments)

    $spec = New-Object HerdrSandbox.ProvisioningProcessSpec
    $spec.Role = 'Herdr workspace provisioning'
    $spec.FilePath = $herdrExecutable
    $spec.Arguments = $Arguments
    $spec.WorkingDirectory = [IO.Path]::GetFullPath($env:USERPROFILE)
    $spec.TimeoutMilliseconds = 30000
    $spec.SuccessExitCodes = [int[]]@(0)
    $spec.TerminateDescendantsAfterRootExit = $true
    $result = [HerdrSandbox.ProvisioningProcess]::Run($spec)
    if (-not $result.Succeeded -or $result.OutputTruncated -or $result.OutputBytes -gt 65536) {
        throw "Herdr workspace command failed (exit $($result.ExitCode), timeout $($result.TimedOut)): $($result.Output)"
    }
    return ([string]$result.Output | ConvertFrom-Json)
}

if ($null -eq ('HerdrSandbox.ProvisioningProcess' -as [type])) {
    Add-Type -Path $ProcessOwnerPath
}
if ([HerdrSandbox.ProvisioningProcess]::ContractVersion -ne 3) {
    throw 'Provisioning process owner contract is invalid.'
}
$herdrExecutable = [Environment]::GetEnvironmentVariable('HERDR_SANDBOX_HERDR_EXE', 'Machine')
if ([string]::IsNullOrWhiteSpace($herdrExecutable) -or -not [IO.Path]::IsPathRooted($herdrExecutable) -or
    -not (Test-Path -LiteralPath $herdrExecutable -PathType Leaf) -or
    ((Get-Item -LiteralPath $herdrExecutable -Force).Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {
    throw 'The verified guest Herdr executable is unavailable.'
}
$herdrDirectory = Split-Path -Parent $herdrExecutable
$machinePath = [Environment]::GetEnvironmentVariable('Path', 'Machine')
$machinePathEntries = @($machinePath -split ';' | Where-Object { -not [string]::IsNullOrWhiteSpace($_) })
if ($machinePathEntries.Count -eq 0 -or
    $machinePathEntries[0].TrimEnd('\') -ine $herdrDirectory.TrimEnd('\') -or
    @($machinePathEntries | Where-Object { $_.TrimEnd('\') -ieq $herdrDirectory.TrimEnd('\') }).Count -ne 1) {
    throw 'Provisioned guest Herdr directory is not the unique first machine PATH entry.'
}
$workspacePath = @($machinePath, [Environment]::GetEnvironmentVariable('Path', 'User')) -join ';'
if ([string]::IsNullOrWhiteSpace($workspacePath) -or $workspacePath.Length -gt 32767) {
    throw 'Provisioned guest workspace PATH is invalid.'
}
$playwrightExtensionToken = [Environment]::GetEnvironmentVariable('PLAYWRIGHT_MCP_EXTENSION_TOKEN', 'Machine')
$manifest = [IO.File]::ReadAllText($WorkspaceManifestPath) | ConvertFrom-Json
if ((@($manifest.PSObject.Properties.Name | Sort-Object) -join '|') -cne 'activeWorkspace|schemaVersion|workspaces' -or
    $manifest.schemaVersion -isnot [int] -or $manifest.schemaVersion -ne 1) {
    throw 'Workspace manifest has an unsupported contract.'
}
$workspaces = @($manifest.workspaces)
if ($workspaces.Count -eq 0 -or $workspaces.Count -gt 16) { throw 'Workspace manifest count is invalid.' }
$names = @{}
$activeMatches = 0
foreach ($workspace in $workspaces) {
    $name = [string]$workspace.name
    $directory = [string]$workspace.directory
    if ((@($workspace.PSObject.Properties.Name | Sort-Object) -join '|') -cne 'directory|name' -or
        $name -notmatch '^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$' -or
        $directory -cne (Join-Path 'C:\Workspaces' $name) -or
        -not (Test-Path -LiteralPath $directory -PathType Container) -or $names.ContainsKey($name)) {
        throw "Workspace manifest entry is invalid: $name"
    }
    $names[$name] = $true
    if ($directory -ceq [string]$manifest.activeWorkspace) { $activeMatches += 1 }
}
if ($activeMatches -ne 1) { throw 'Workspace manifest has no unique active workspace.' }

function Initialize-GuestWorkspaces {
$listing = Invoke-WorkspaceCommand -Arguments @('workspace', 'list')
if ([string]$listing.result.type -cne 'workspace_list') { throw 'Herdr workspace listing is invalid.' }
$existing = @($listing.result.workspaces)
$usedWorkspaceIDs = @{}
$ordered = @($workspaces | Sort-Object `
    @{ Expression = { if ([string]$_.directory -ceq [string]$manifest.activeWorkspace) { 1 } else { 0 } } }, `
    @{ Expression = { [string]$_.name } })
foreach ($workspace in $ordered) {
    $name = [string]$workspace.name
    $directory = [string]$workspace.directory
    $existingMatches = @($existing | Where-Object { [string]$_.label -ceq $name })
    if ($existingMatches.Count -gt 1) { throw "Herdr has multiple workspaces named $name. Preserve them and resolve the ambiguity before retrying." }
    if ($existingMatches.Count -eq 1) {
        $workspaceID = [string]$existingMatches[0].workspace_id
        if ([string]::IsNullOrWhiteSpace($workspaceID)) { throw "Herdr workspace identity is missing for $name." }
        $panes = Invoke-WorkspaceCommand -Arguments @('pane', 'list', '--workspace', $workspaceID)
        if ([string]$panes.result.type -cne 'pane_list' -or
            @($panes.result.panes | Where-Object {
                [string]$_.workspace_id -ceq $workspaceID -and [string]$_.cwd -ieq $directory
            }).Count -eq 0) {
            throw "Existing Herdr workspace $name has no pane in $directory. It was preserved; correct it before retrying."
        }
    } else {
        $arguments = @('workspace', 'create', '--cwd', $directory, '--label', $name, '--no-focus',
            '--env', "PATH=$workspacePath", '--env', "HERDR_SANDBOX_HERDR_EXE=$herdrExecutable")
        if (-not [string]::IsNullOrWhiteSpace($playwrightExtensionToken)) {
            $arguments += @('--env', "PLAYWRIGHT_MCP_EXTENSION_TOKEN=$playwrightExtensionToken")
        }
        $created = Invoke-WorkspaceCommand -Arguments $arguments
        $workspaceID = [string]$created.result.workspace.workspace_id
        if ([string]::IsNullOrWhiteSpace($workspaceID) -or
            [string]::IsNullOrWhiteSpace([string]$created.result.root_pane.pane_id)) {
            throw "Herdr did not return a workspace and root pane for $name."
        }
    }
    if ($usedWorkspaceIDs.ContainsKey($workspaceID)) { throw 'Herdr returned one workspace identity for multiple projects.' }
    $usedWorkspaceIDs[$workspaceID] = $true
    if ($directory -ceq [string]$manifest.activeWorkspace) {
        Invoke-WorkspaceCommand -Arguments @('workspace', 'focus', $workspaceID) | Out-Null
    }
}
}
Initialize-GuestWorkspaces
