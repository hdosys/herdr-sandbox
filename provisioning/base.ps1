# herdr-sandbox-base-contract: 33
param(
    [ValidateSet('Registry', 'Development')]
    [string]$Phase = 'Development',
    [string]$ProjectProvisioningDirectory = '',
    [string]$WorkspacesDirectory = 'C:\Workspaces',
    [Parameter(Mandatory = $true)]
    [string]$PackagePlanPath,
    [Parameter(Mandatory = $true)]
    [string]$UserProvisioningPath,
    [switch]$AudioEnabled
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version 2.0

function Get-ProvisioningBoundedDiagnosticText {
    param(
        [AllowEmptyString()]
        [string]$Text,
        [Parameter(Mandatory = $true)]
        [int]$MaximumBytes
    )

    if ([string]::IsNullOrEmpty($Text)) {
        return ''
    }
    $encoding = [Text.Encoding]::UTF8
    $bytes = $encoding.GetBytes($Text)
    if ($bytes.Length -le $MaximumBytes) {
        return $Text
    }
    $marker = "`n... diagnostic truncated; original UTF-8 bytes: $($bytes.Length) ...`n"
    $markerBytes = $encoding.GetByteCount($marker)
    $contentBudget = $MaximumBytes - $markerBytes - 16
    $headLength = [int][Math]::Floor($contentBudget / 2)
    $tailLength = $contentBudget - $headLength
    return ($encoding.GetString($bytes, 0, $headLength) + $marker +
        $encoding.GetString($bytes, $bytes.Length - $tailLength, $tailLength))
}

function Write-ProvisioningProgress {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Message
    )

    Write-Host "[development-provisioning] $Message"
    $statusDirectory = [string]$env:HERDR_SANDBOX_STATUS_DIRECTORY
    if ([string]::IsNullOrWhiteSpace($statusDirectory)) { return }
    if (-not [IO.Path]::IsPathRooted($statusDirectory) -or
        -not (Test-Path -LiteralPath $statusDirectory -PathType Container)) {
        throw "Provisioning status directory is unavailable: $statusDirectory"
    }
    $progressPath = Join-Path $statusDirectory 'progress.json'
    $temporaryPath = $progressPath + '.' + [Guid]::NewGuid().ToString('N') + '.tmp'
    $progress = [ordered]@{
        schemaVersion = 1
        phase = 'development-provisioning'
        message = $Message
    } | ConvertTo-Json -Compress
    [IO.File]::WriteAllText($temporaryPath, $progress, (New-Object Text.UTF8Encoding($false)))
    $publicationError = $null
    for ($attempt = 1; $attempt -le 30; $attempt += 1) {
        $backupPath = $null
        try {
            if (Test-Path -LiteralPath $progressPath -PathType Leaf) {
                $backupPath = $progressPath + '.' + [Guid]::NewGuid().ToString('N') + '.bak'
                [IO.File]::Replace($temporaryPath, $progressPath, $backupPath, $true)
            } else {
                [IO.File]::Move($temporaryPath, $progressPath)
            }
            return
        } catch [IO.IOException] {
            $publicationError = $_.Exception
        } catch [UnauthorizedAccessException] {
            $publicationError = $_.Exception
        } finally {
            if (-not [string]::IsNullOrWhiteSpace($backupPath)) {
                try { [IO.File]::Delete($backupPath) } catch { }
            }
        }
        Start-Sleep -Milliseconds 100
    }
    throw "Progress status publication failed after 30 attempts: $($publicationError.Message)"
}

function Write-ProvisioningTiming {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Role,
        [Parameter(Mandatory = $true)]
        [double]$Seconds
    )

    Write-Host ('[timing] {0}: {1:N1}s' -f $Role, $Seconds)
    $statusDirectory = [string]$env:HERDR_SANDBOX_STATUS_DIRECTORY
    if ([string]::IsNullOrWhiteSpace($statusDirectory)) { return }
    try {
        if (-not [IO.Path]::IsPathRooted($statusDirectory) -or
            -not (Test-Path -LiteralPath $statusDirectory -PathType Container)) {
            throw "Timing status directory is unavailable: $statusDirectory"
        }
        $record = [ordered]@{
            schemaVersion = 1
            role = $Role
            elapsedMilliseconds = [long][Math]::Round($Seconds * 1000)
            recordedAtUTC = [DateTime]::UtcNow.ToString('o')
        } | ConvertTo-Json -Compress
        $timingPath = Join-Path $statusDirectory 'timings.jsonl'
        $existing = @()
        if (Test-Path -LiteralPath $timingPath -PathType Leaf) {
            $timingInfo = Get-Item -LiteralPath $timingPath -Force
            if (($timingInfo.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0 -or
                $timingInfo.Length -gt 262144) {
                throw "Provisioning timing file is unsafe or exceeds 262144 bytes: $timingPath"
            }
            $existing = @([IO.File]::ReadAllLines($timingPath) | Select-Object -Last 127)
        }
        $temporaryTimingPath = $timingPath + '.' + [Guid]::NewGuid().ToString('N') + '.tmp'
        $backupTimingPath = $timingPath + '.' + [Guid]::NewGuid().ToString('N') + '.bak'
        try {
            [IO.File]::WriteAllLines($temporaryTimingPath, [string[]](@($existing) + $record),
                (New-Object Text.UTF8Encoding($false)))
            if (Test-Path -LiteralPath $timingPath -PathType Leaf) {
                [IO.File]::Replace($temporaryTimingPath, $timingPath, $backupTimingPath, $true)
            } else {
                [IO.File]::Move($temporaryTimingPath, $timingPath)
            }
        } finally {
            foreach ($temporaryPath in @($temporaryTimingPath, $backupTimingPath)) {
                if (Test-Path -LiteralPath $temporaryPath) {
                    [IO.File]::Delete($temporaryPath)
                }
            }
        }
    } catch {
        Write-Warning "Provisioning timing persistence failed: $($_.Exception.Message)"
    }
}

$identity = [Security.Principal.WindowsIdentity]::GetCurrent()
$principal = New-Object Security.Principal.WindowsPrincipal($identity)
if (-not $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
    throw 'Provision mode requires an elevated Windows PowerShell process.'
}
foreach ($requiredPath in @($ProjectProvisioningDirectory, $WorkspacesDirectory, $PackagePlanPath)) {
    if ([string]::IsNullOrWhiteSpace($requiredPath)) {
        throw 'Provisioning requires ProjectProvisioningDirectory and WorkspacesDirectory.'
    }
}

function Read-ProvisioningPackagePlan {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Path
    )

    if (-not [IO.Path]::IsPathRooted($Path) -or -not (Test-Path -LiteralPath $Path -PathType Leaf)) {
        throw "WinGet package plan is missing: $Path"
    }
    $info = Get-Item -LiteralPath $Path -Force
    if (($info.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0 -or
        $info.Length -le 0 -or $info.Length -gt 65536) {
        throw "WinGet package plan is not a bounded regular file: $Path"
    }
    $plan = [IO.File]::ReadAllText($Path) | ConvertFrom-Json
    $properties = @($plan.PSObject.Properties.Name | Sort-Object)
    $defaults = @($plan.defaults)
    $additions = @($plan.additions)
    if (($properties -join '|') -cne 'additions|defaults|schemaVersion|windowsTerminalEdition' -or
        $plan.schemaVersion -isnot [int] -or $plan.windowsTerminalEdition -isnot [string] -or
        [int]$plan.schemaVersion -ne 1 -or
        [string]$plan.windowsTerminalEdition -notin @('stable', 'preview') -or
        $defaults.Count -eq 0 -or $defaults.Count -gt 13 -or $additions.Count -gt 64) {
        throw 'WinGet package plan has an unsupported contract.'
    }
    $known = @{}
    foreach ($id in @(
        'Microsoft.PowerShell',
        'Starship.Starship',
        'junegunn.fzf',
        'BurntSushi.ripgrep.MSVC',
        'Git.Git',
        'GitHub.cli',
        'Tailscale.Tailscale',
        'SST.opencode',
        'WinDirStat.WinDirStat',
        'Voidstar.FilePilot',
        'Microsoft.UI.Xaml.2.8',
        'Microsoft.WindowsTerminal',
        'Microsoft.WindowsTerminal.Preview'
    )) {
        $known[$id] = $true
    }
    $projectStackPackages = @{}
    foreach ($id in @('Microsoft.DotNet.SDK.10', 'GoLang.Go', 'OpenJS.NodeJS.LTS', 'zig.zig', 'Rustlang.Rustup',
        'nextest.cargo-nextest', 'Casey.Just')) {
        $projectStackPackages[$id] = $true
    }
    $enabled = @{}
    $versions = @{}
    foreach ($group in @(
        [pscustomobject]@{ Entries = $defaults; Defaults = $true },
        [pscustomobject]@{ Entries = $additions; Defaults = $false }
    )) {
        foreach ($entry in @($group.Entries)) {
            $entryProperties = @($entry.PSObject.Properties.Name | Sort-Object)
            $id = [string]$entry.id
            $version = [string]$entry.version
            if (($entryProperties -join '|') -cne 'id|version' -or
                $entry.id -isnot [string] -or $entry.version -isnot [string] -or
                $id -notmatch '^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$' -or
                (-not [string]::IsNullOrEmpty($version) -and
                    $version -notmatch '^[A-Za-z0-9][A-Za-z0-9._+-]{0,127}$') -or
                $enabled.ContainsKey($id) -or $known.ContainsKey($id) -ne [bool]$group.Defaults -or
                (-not [bool]$group.Defaults -and
                    ($projectStackPackages.ContainsKey($id) -or $id.StartsWith('Python.Python.', [StringComparison]::OrdinalIgnoreCase)))) {
                throw "WinGet package plan entry is invalid: $id"
            }
            $enabled[$id] = $true
            $versions[$id] = $version
        }
    }
    if (-not $enabled.ContainsKey('Microsoft.PowerShell')) {
        throw 'WinGet package plan is missing Core package Microsoft.PowerShell.'
    }
    $terminalID = 'Microsoft.WindowsTerminal'
    $otherTerminalID = 'Microsoft.WindowsTerminal.Preview'
    if ([string]$plan.windowsTerminalEdition -ceq 'preview') {
        $terminalID = 'Microsoft.WindowsTerminal.Preview'
        $otherTerminalID = 'Microsoft.WindowsTerminal'
    }
    $terminalEnabled = $enabled.ContainsKey($terminalID)
    if ($enabled.ContainsKey($otherTerminalID) -or
        $terminalEnabled -ne $enabled.ContainsKey('Microsoft.UI.Xaml.2.8')) {
        throw 'WinGet package plan Windows Terminal selection is inconsistent.'
    }
    return [pscustomobject]@{
        Data = $plan
        Enabled = $enabled
        Versions = $versions
        TerminalID = $terminalID
    }
}

$provisioningPackagePlan = Read-ProvisioningPackagePlan -Path $PackagePlanPath
$WindowsTerminalEdition = [string]$provisioningPackagePlan.Data.windowsTerminalEdition

function Test-ProvisioningPackageEnabled {
    param([Parameter(Mandatory = $true)][string]$Id)
    return $provisioningPackagePlan.Enabled.ContainsKey($Id)
}

function Get-ProvisioningPackageVersion {
    param([Parameter(Mandatory = $true)][string]$Id)
    if (-not (Test-ProvisioningPackageEnabled -Id $Id)) {
        throw "WinGet package is not enabled in the resolved plan: $Id"
    }
    return [string]$provisioningPackagePlan.Versions[$Id]
}

function Invoke-ProvisioningNative {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Role,
        [Parameter(Mandatory = $true)]
        [object]$FilePath,
        [Parameter(Mandatory = $true)]
        [string[]]$ArgumentList,
        [int[]]$SuccessExitCodes = @(0),
        [switch]$WaitForProcessTree
    )

    $acceptedExitCodes = @{}
    foreach ($successExitCode in @($SuccessExitCodes)) {
        if ($successExitCode -lt 0 -or $successExitCode -gt 65535 -or
            $acceptedExitCodes.ContainsKey([int]$successExitCode)) {
            throw "$Role success exit-code contract is empty, duplicate, or out of bounds."
        }
        $acceptedExitCodes[[int]$successExitCode] = $true
    }
    if ($acceptedExitCodes.Count -eq 0 -or $acceptedExitCodes.Count -gt 8) {
        throw "$Role success exit-code contract is empty, duplicate, or out of bounds."
    }

    $pathValues = @($FilePath)
    if ($pathValues.Count -ne 1) {
        throw "$Role executable resolved to $($pathValues.Count) values; expected exactly one."
    }
    $resolvedFilePath = [string]$pathValues[0]
    if ([string]::IsNullOrWhiteSpace($resolvedFilePath)) {
        throw "$Role executable path is empty."
    }
    $command = Get-Command $resolvedFilePath -CommandType Application -ErrorAction SilentlyContinue | Select-Object -First 1
    if ($null -eq $command) {
        throw "$Role executable is not available: $resolvedFilePath"
    }
    Write-ProvisioningProgress -Message $Role
    $stopwatch = [Diagnostics.Stopwatch]::StartNew()
    $previousErrorActionPreference = $ErrorActionPreference
    try {
        $ErrorActionPreference = 'Continue'
        if ($WaitForProcessTree) {
            $process = Start-Process -FilePath $command.Source -ArgumentList $ArgumentList `
                -WindowStyle Hidden -Wait -PassThru
            try {
                $exitCode = $process.ExitCode
            } finally {
                $process.Dispose()
            }
            $output = @()
        } else {
            $output = @(& $command.Source @ArgumentList 2>&1)
            $exitCode = $LASTEXITCODE
        }
    } finally {
        $ErrorActionPreference = $previousErrorActionPreference
        $stopwatch.Stop()
    }
    Write-ProvisioningTiming -Role $Role -Seconds $stopwatch.Elapsed.TotalSeconds
    if ($null -eq $exitCode) {
        throw "$Role did not return a process exit code."
    }
    if (-not $acceptedExitCodes.ContainsKey([int]$exitCode)) {
        $details = Get-ProvisioningBoundedDiagnosticText `
            -Text (($output -join [Environment]::NewLine).Trim()) -MaximumBytes 3000
        throw "$Role failed with exit code $exitCode. $details"
    }
    return $output
}

function Update-ProvisioningPath {
    $machinePath = [Environment]::GetEnvironmentVariable('Path', 'Machine')
    $userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
    $env:Path = @($machinePath, $userPath) -join ';'
}

function Wait-ProvisioningCommandAvailable {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Role,
        [Parameter(Mandatory = $true)]
        [string]$Name,
        [int]$TimeoutSeconds = 120,
        [int]$DelayMilliseconds = 500,
        [string]$CommandSourceExclusion = ''
    )

    if ($TimeoutSeconds -lt 1 -or $TimeoutSeconds -gt 300 -or
        $DelayMilliseconds -lt 25 -or $DelayMilliseconds -gt 2000) {
        throw "$Role command readiness settings are out of bounds."
    }
    $deadline = [DateTime]::UtcNow.AddSeconds($TimeoutSeconds)
    do {
        Update-ProvisioningPath
        $commands = @(Get-Command $Name -CommandType Application -ErrorAction SilentlyContinue)
        if (-not [string]::IsNullOrWhiteSpace($CommandSourceExclusion)) {
            $commands = @($commands | Where-Object { $_.Source -notlike $CommandSourceExclusion })
        }
        $command = $commands | Select-Object -First 1
        if ($null -ne $command) {
            return $command.Source
        }
        Start-Sleep -Milliseconds $DelayMilliseconds
    } while ([DateTime]::UtcNow -lt $deadline)
    throw "$Role command did not become available within $TimeoutSeconds seconds: $Name"
}

function Add-ProvisioningMachinePath {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Directory
    )

    if (-not (Test-Path -LiteralPath $Directory -PathType Container)) {
        throw "PATH directory is missing: $Directory"
    }
    $machinePath = [Environment]::GetEnvironmentVariable('Path', 'Machine')
    $entries = @($machinePath -split ';' | Where-Object { -not [string]::IsNullOrWhiteSpace($_) })
    if (-not ($entries | Where-Object { $_.TrimEnd('\') -ieq $Directory.TrimEnd('\') })) {
        [Environment]::SetEnvironmentVariable('Path', ((@($Directory) + $entries) -join ';'), 'Machine')
    }
    Update-ProvisioningPath
}

function Assert-ProvisioningCachePath {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Path
    )

    $cacheRoot = [IO.Path]::GetFullPath('C:\HerdrSandbox\cache').TrimEnd('\')
    $fullPath = [IO.Path]::GetFullPath($Path).TrimEnd('\')
    if ($fullPath -ine $cacheRoot -and
        -not $fullPath.StartsWith($cacheRoot + '\', [StringComparison]::OrdinalIgnoreCase)) {
        throw "Cache path escapes C:\HerdrSandbox\cache: $fullPath"
    }
    $current = $fullPath
    while ($current.Length -ge $cacheRoot.Length) {
        if (Test-Path -LiteralPath $current) {
            $item = Get-Item -LiteralPath $current -Force
            if (($item.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {
                throw "Cache path contains a reparse point: $current"
            }
        }
        if ($current -ieq $cacheRoot) {
            break
        }
        $parent = Split-Path -Parent $current
        if ([string]::IsNullOrWhiteSpace($parent) -or $parent -ieq $current) {
            throw "Cache path parent resolution failed: $fullPath"
        }
        $current = $parent.TrimEnd('\')
    }
}

function Assert-ProvisioningCacheTree {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Path
    )

    Assert-ProvisioningCachePath -Path $Path
    if (-not (Test-Path -LiteralPath $Path -PathType Container)) {
        return
    }
    $pending = New-Object 'System.Collections.Generic.List[string]'
    $pending.Add([IO.Path]::GetFullPath($Path)) | Out-Null
    while ($pending.Count -gt 0) {
        $index = $pending.Count - 1
        $directory = $pending[$index]
        $pending.RemoveAt($index)
        foreach ($item in @(Get-ChildItem -LiteralPath $directory -Force)) {
            Assert-ProvisioningCachePath -Path $item.FullName
            if (($item.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {
                throw "Cache tree contains a reparse point: $($item.FullName)"
            }
            if ($item.PSIsContainer) {
                $pending.Add($item.FullName) | Out-Null
            }
        }
    }
}

function Remove-ProvisioningGuestPackageStage {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Path,
        [int]$Attempts = 20,
        [int]$DelayMilliseconds = 500,
        [string]$StageRoot = 'C:\HerdrSandbox\staging\packages',
        [switch]$BestEffort
    )

    $stageRoot = [IO.Path]::GetFullPath($StageRoot).TrimEnd('\')
    $fullPath = [IO.Path]::GetFullPath($Path).TrimEnd('\')
    if (-not $fullPath.StartsWith($stageRoot + '\', [StringComparison]::OrdinalIgnoreCase)) {
        throw "Guest package stage path escapes $stageRoot`: $fullPath"
    }
    if ($Attempts -lt 1 -or $Attempts -gt 60 -or $DelayMilliseconds -lt 0 -or $DelayMilliseconds -gt 2000) {
        throw 'Guest package stage cleanup retry settings are out of bounds.'
    }
    $lastFailure = $null
    for ($attempt = 1; $attempt -le $Attempts; $attempt++) {
        try {
            if (-not (Test-Path -LiteralPath $fullPath)) {
                return $true
            }
            Remove-Item -LiteralPath $fullPath -Recurse -Force -ErrorAction Stop
            if (Test-Path -LiteralPath $fullPath) {
                throw "Guest package stage still exists after removal: $fullPath"
            }
            return $true
        } catch {
            $lastFailure = $_
            if ($attempt -lt $Attempts) {
                Start-Sleep -Milliseconds $DelayMilliseconds
            }
        }
    }
    $details = Get-ProvisioningBoundedDiagnosticText -Text ([string]$lastFailure.Exception.Message) -MaximumBytes 1200
    $message = "Guest package stage cleanup failed after $Attempts attempts: $fullPath. $details"
    if ($BestEffort) {
        Write-Warning $message
        return $false
    }
    throw $message
}

function Get-ProvisioningMetadataValue {
    param(
        [Parameter(Mandatory = $true)]
        [AllowEmptyString()]
        [string[]]$Lines,
        [Parameter(Mandatory = $true)]
        [string]$Pattern,
        [Parameter(Mandatory = $true)]
        [string]$Name
    )

    $values = @()
    foreach ($line in $Lines) {
        if ($line -match $Pattern) {
            $values += [string]$Matches[1]
        }
    }
    if ($values.Count -ne 1 -or [string]::IsNullOrWhiteSpace($values[0])) {
        throw "WinGet metadata field $Name resolved to $($values.Count) values; expected exactly one."
    }
    return $values[0].Trim()
}

function Search-ProvisioningWinGetPackages {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Role,
        [Parameter(Mandatory = $true)]
        [string]$IdQuery,
        [switch]$Exact
    )

    $arguments = @(
        'search', '--id', $IdQuery, '--source', 'winget', '--count', '1000',
        '--accept-source-agreements', '--disable-interactivity', '--no-progress'
    )
    if ($Exact) { $arguments += '--exact' }
    $lines = @(Invoke-ProvisioningNative -Role "$Role package search" -FilePath 'winget.exe' -ArgumentList $arguments |
        ForEach-Object { ([string]$_).TrimEnd() } | Where-Object { -not [string]::IsNullOrWhiteSpace($_) })
    $header = if ($lines.Count -gt 0) { [string]$lines[0] } else { '' }
    $idColumn = $header.IndexOf('Id', [StringComparison]::Ordinal)
    $versionColumn = $header.IndexOf('Version', [StringComparison]::Ordinal)
    if ($lines.Count -lt 3 -or -not $header.StartsWith('Name', [StringComparison]::Ordinal) -or
        $idColumn -le 4 -or $versionColumn -le ($idColumn + 2) -or
        $header.Substring(4, $idColumn - 4).Trim().Length -ne 0 -or
        $header.Substring($idColumn + 2, $versionColumn - ($idColumn + 2)).Trim().Length -ne 0 -or
        $lines[1].Length -lt ($versionColumn + 'Version'.Length) -or $lines[1] -cnotmatch '^-+$') {
        throw "$Role WinGet search output header is unsupported: lines=$($lines.Count) header=[$header]"
    }
    $results = @()
    $seen = @{}
    foreach ($line in @($lines | Select-Object -Skip 2)) {
        if ($line -match '[^\x20-\x7E]') {
            throw "$Role WinGet search output contains a control or non-ASCII character."
        }
        if ($line.Length -le $versionColumn) {
            throw "$Role WinGet search output row is unsupported: $line"
        }
        $name = $line.Substring(0, $idColumn).Trim()
        $id = $line.Substring($idColumn, $versionColumn - $idColumn).Trim()
        $version = $line.Substring($versionColumn).Trim()
        if ([string]::IsNullOrWhiteSpace($name) -or $id -notmatch '^[A-Za-z0-9._-]+$' -or
            [string]::IsNullOrWhiteSpace($version) -or $version -match '\s') {
            throw "$Role WinGet search output row is unsupported: $line"
        }
        if ($seen.ContainsKey($id)) {
            throw "$Role WinGet search returned duplicate package $id."
        }
        $seen[$id] = $true
        $results += [pscustomobject]@{
            Name = $name
            Id = $id
            Version = $version
        }
    }
    if ($results.Count -eq 0) {
        throw "$Role WinGet search returned no packages."
    }
    return @($results)
}

function Get-ProvisioningWinGetMetadata {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Role,
        [Parameter(Mandatory = $true)]
        [string]$Id,
        [string]$Version = '',
        [Parameter(Mandatory = $true)]
        [string]$InstallerType,
        [string]$Scope = ''
    )

    $arguments = @(
        'show', '--id', $Id, '--exact', '--source', 'winget',
        '--architecture', 'x64', '--installer-type', $InstallerType,
        '--accept-source-agreements', '--disable-interactivity'
    )
    if (-not [string]::IsNullOrWhiteSpace($Version)) {
        $arguments += @('--version', $Version)
    }
    if (-not [string]::IsNullOrWhiteSpace($Scope)) {
        $arguments += @('--scope', $Scope)
    }
    $lines = @(Invoke-ProvisioningNative -Role "$Role metadata resolution" -FilePath 'winget.exe' -ArgumentList $arguments |
        ForEach-Object { [string]$_ })
    $resolvedID = Get-ProvisioningMetadataValue -Lines $lines -Name 'PackageIdentifier' `
        -Pattern '^Found .+ \[([A-Za-z0-9._-]+)\](?: Version .+)?$'
    $resolvedVersion = Get-ProvisioningMetadataValue -Lines $lines -Name 'PackageVersion' `
        -Pattern '^Version:\s*(\S(?:.*\S)?)\s*$'
    $installerURL = Get-ProvisioningMetadataValue -Lines $lines -Name 'InstallerUrl' `
        -Pattern '^\s*Installer Url:\s*(https://\S+)\s*$'
    $installerSHA256 = (Get-ProvisioningMetadataValue -Lines $lines -Name 'InstallerSha256' `
        -Pattern '^\s*Installer SHA256:\s*([A-Fa-f0-9]{64})\s*$').ToUpperInvariant()
    if ($resolvedID -cne $Id) {
        throw "$Role metadata resolved package $resolvedID instead of $Id."
    }
    if (-not [string]::IsNullOrWhiteSpace($Version) -and $resolvedVersion -cne $Version) {
        throw "$Role metadata resolved version $resolvedVersion instead of $Version."
    }
    $uri = [Uri]$installerURL
    if ($uri.Scheme -cne 'https' -or [string]::IsNullOrWhiteSpace($uri.Host)) {
        throw "$Role installer URL is not a valid HTTPS URL: $installerURL"
    }
    $extension = [IO.Path]::GetExtension($uri.AbsolutePath).ToLowerInvariant()
    if ($extension -notin @('.exe', '.msi', '.zip', '.msix', '.msixbundle', '.appx', '.appxbundle')) {
        throw "$Role installer URL has an unsupported extension: $extension"
    }
    return [pscustomobject]@{
        Id = $resolvedID
        Version = $resolvedVersion
        Architecture = 'x64'
        InstallerType = $InstallerType
        Scope = $Scope
        Url = $installerURL
        Sha256 = $installerSHA256
        PayloadName = 'payload' + $extension
    }
}

function Assert-ProvisioningMergedManifestField {
    param(
        [Parameter(Mandatory = $true)]
        [AllowEmptyString()]
        [string[]]$Lines,
        [Parameter(Mandatory = $true)]
        [string]$Name,
        [Parameter(Mandatory = $true)]
        [string]$Expected,
        [switch]$TopLevel
    )

    $prefix = $Name + ':'
    $values = @()
    foreach ($line in $Lines) {
        if ($TopLevel -and $line.Length -ne $line.TrimStart().Length) {
            continue
        }
        $trimmed = $line.Trim()
        if ($trimmed.StartsWith('- ', [StringComparison]::Ordinal)) {
            $trimmed = $trimmed.Substring(2).TrimStart()
        }
        if ($trimmed.StartsWith($prefix, [StringComparison]::Ordinal)) {
            $value = $trimmed.Substring($prefix.Length).Trim()
            if (($value.StartsWith("'") -and $value.EndsWith("'")) -or
                ($value.StartsWith('"') -and $value.EndsWith('"'))) {
                $value = $value.Substring(1, $value.Length - 2)
            }
            $values += $value
        }
    }
    if ($values.Count -ne 1 -or $values[0] -ine $Expected) {
        throw "Downloaded WinGet manifest field $Name did not equal $Expected."
    }
}

function Assert-ProvisioningDownloadedManifest {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Path,
        [Parameter(Mandatory = $true)]
        [object]$Metadata
    )

    $lines = [IO.File]::ReadAllLines($Path)
    Assert-ProvisioningMergedManifestField -Lines $lines -Name 'ManifestType' -Expected 'merged' -TopLevel
    $manifestVersions = @()
    foreach ($line in $lines) {
        if ($line.Length -ne $line.TrimStart().Length) {
            continue
        }
        $trimmed = $line.Trim()
        if ($trimmed.StartsWith('ManifestVersion:', [StringComparison]::Ordinal)) {
            $manifestVersions += $trimmed.Substring('ManifestVersion:'.Length).Trim().Trim("'").Trim('"')
        }
    }
    $supportedManifestVersions = @('1.0.0', '1.1.0', '1.2.0', '1.4.0', '1.5.0', '1.6.0', '1.7.0', '1.9.0', '1.10.0', '1.12.0', '1.28.0')
    if ($manifestVersions.Count -ne 1 -or $manifestVersions[0] -notin $supportedManifestVersions) {
        throw "Downloaded WinGet manifest has an unsupported ManifestVersion: $($manifestVersions -join ', ')"
    }
    Assert-ProvisioningMergedManifestField -Lines $lines -Name 'PackageIdentifier' -Expected $Metadata.Id -TopLevel
    Assert-ProvisioningMergedManifestField -Lines $lines -Name 'PackageVersion' -Expected $Metadata.Version -TopLevel
    Assert-ProvisioningMergedManifestField -Lines $lines -Name 'Architecture' -Expected $Metadata.Architecture
    Assert-ProvisioningMergedManifestField -Lines $lines -Name 'InstallerType' -Expected $Metadata.InstallerType
    Assert-ProvisioningMergedManifestField -Lines $lines -Name 'InstallerUrl' -Expected $Metadata.Url
    Assert-ProvisioningMergedManifestField -Lines $lines -Name 'InstallerSha256' -Expected $Metadata.Sha256
    if (-not [string]::IsNullOrWhiteSpace($Metadata.Scope)) {
        Assert-ProvisioningMergedManifestField -Lines $lines -Name 'Scope' -Expected $Metadata.Scope
    }
}

function Get-ProvisioningSafeCacheName {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Value
    )

    $safe = $Value -replace '[^A-Za-z0-9._+-]', '_'
    if ([string]::IsNullOrWhiteSpace($safe)) {
        throw "Cache identity is empty after normalization: $Value"
    }
    return $safe
}

function Test-ProvisioningPackageCacheEntry {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Directory,
        [Parameter(Mandatory = $true)]
        [object]$Metadata
    )

    try {
        if (-not (Test-Path -LiteralPath $Directory -PathType Container)) {
            return $false
        }
        $directoryInfo = Get-Item -LiteralPath $Directory -Force
        if (($directoryInfo.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {
            return $false
        }
        $items = @(Get-ChildItem -LiteralPath $Directory -Force)
        if ($items.Count -ne 2 -or @($items | Where-Object { $_.PSIsContainer }).Count -ne 0) {
            return $false
        }
        $descriptorPath = Join-Path $Directory 'complete.json'
        $payloadPath = Join-Path $Directory $Metadata.PayloadName
        if (-not (Test-Path -LiteralPath $descriptorPath -PathType Leaf) -or
            -not (Test-Path -LiteralPath $payloadPath -PathType Leaf)) {
            return $false
        }
        $payloadInfo = Get-Item -LiteralPath $payloadPath -Force
        $descriptorInfo = Get-Item -LiteralPath $descriptorPath -Force
        if (($payloadInfo.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0 -or
            ($descriptorInfo.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {
            return $false
        }
        $descriptor = [IO.File]::ReadAllText($descriptorPath) | ConvertFrom-Json
        $expectedProperties = @('architecture', 'id', 'installerType', 'payloadName', 'schemaVersion', 'scope', 'sha256', 'url', 'version')
        $actualProperties = @($descriptor.PSObject.Properties.Name | Sort-Object)
        if (($actualProperties -join '|') -cne ($expectedProperties -join '|')) {
            return $false
        }
        if ([int]$descriptor.schemaVersion -ne 1 -or
            [string]$descriptor.id -cne $Metadata.Id -or
            [string]$descriptor.version -cne $Metadata.Version -or
            [string]$descriptor.architecture -cne $Metadata.Architecture -or
            [string]$descriptor.installerType -cne $Metadata.InstallerType -or
            [string]$descriptor.scope -cne $Metadata.Scope -or
            [string]$descriptor.url -cne $Metadata.Url -or
            [string]$descriptor.sha256 -cne $Metadata.Sha256 -or
            [string]$descriptor.payloadName -cne $Metadata.PayloadName) {
            return $false
        }
        $actualHash = (Get-FileHash -LiteralPath $payloadPath -Algorithm SHA256).Hash.ToUpperInvariant()
        return $actualHash -ceq $Metadata.Sha256
    } catch {
        return $false
    }
}

function Copy-ProvisioningPackageToGuest {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Source,
        [Parameter(Mandatory = $true)]
        [string]$Destination,
        [Parameter(Mandatory = $true)]
        [string]$ExpectedSHA256
    )

    Copy-Item -LiteralPath $Source -Destination $Destination -Force
    $actualHash = (Get-FileHash -LiteralPath $Destination -Algorithm SHA256).Hash.ToUpperInvariant()
    if ($actualHash -cne $ExpectedSHA256) {
        throw "Guest-local package copy hash mismatch: $Destination"
    }
}

function Get-ProvisioningDownloadedPackage {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Role,
        [Parameter(Mandatory = $true)]
        [object]$Metadata,
        [Parameter(Mandatory = $true)]
        [string]$DownloadDirectory,
        [Parameter(Mandatory = $true)]
        [string]$GuestPayloadPath
    )

    $arguments = @(
        'download', '--id', $Metadata.Id, '--exact', '--source', 'winget',
        '--version', $Metadata.Version, '--architecture', $Metadata.Architecture,
        '--installer-type', $Metadata.InstallerType, '--skip-dependencies',
        '--download-directory', $DownloadDirectory, '--accept-package-agreements',
        '--accept-source-agreements', '--disable-interactivity'
    )
    if (-not [string]::IsNullOrWhiteSpace($Metadata.Scope)) {
        $arguments += @('--scope', $Metadata.Scope)
    }
    Invoke-ProvisioningNative -Role "$Role package download" -FilePath 'winget.exe' -ArgumentList $arguments | Out-Null
    $directories = @(Get-ChildItem -LiteralPath $DownloadDirectory -Directory -Force)
    if ($directories.Count -ne 0) {
        throw "$Role package download produced unexpected dependency directories."
    }
    $files = @(Get-ChildItem -LiteralPath $DownloadDirectory -File -Force)
    $manifests = @($files | Where-Object { $_.Extension -ieq '.yaml' })
    $payloads = @($files | Where-Object { $_.Extension -ine '.yaml' })
    if ($manifests.Count -ne 1 -or $payloads.Count -ne 1) {
        throw "$Role package download produced $($manifests.Count) manifests and $($payloads.Count) payloads; expected one each."
    }
    Assert-ProvisioningDownloadedManifest -Path $manifests[0].FullName -Metadata $Metadata
    $actualHash = (Get-FileHash -LiteralPath $payloads[0].FullName -Algorithm SHA256).Hash.ToUpperInvariant()
    if ($actualHash -cne $Metadata.Sha256) {
        throw "$Role downloaded package hash mismatch: $actualHash"
    }
    Copy-ProvisioningPackageToGuest -Source $payloads[0].FullName -Destination $GuestPayloadPath `
        -ExpectedSHA256 $Metadata.Sha256
}

function Publish-ProvisioningPackageCacheEntry {
    param(
        [Parameter(Mandatory = $true)]
        [string]$PackageRoot,
        [Parameter(Mandatory = $true)]
        [string]$EntryDirectory,
        [Parameter(Mandatory = $true)]
        [string]$GuestPayloadPath,
        [Parameter(Mandatory = $true)]
        [object]$Metadata
    )

    Assert-ProvisioningCachePath -Path $PackageRoot
    $staging = Join-Path $PackageRoot ('.stage-' + [Guid]::NewGuid().ToString('N'))
    New-Item -ItemType Directory -Path $staging | Out-Null
    Assert-ProvisioningCachePath -Path $staging
    $displaced = ''
    $promotionSucceeded = $false
    $primaryFailure = $null
    $cleanupFailure = $null
    try {
        $stagedPayload = Join-Path $staging $Metadata.PayloadName
        Copy-ProvisioningPackageToGuest -Source $GuestPayloadPath -Destination $stagedPayload `
            -ExpectedSHA256 $Metadata.Sha256
        $descriptor = [ordered]@{
            schemaVersion = 1
            id = $Metadata.Id
            version = $Metadata.Version
            architecture = $Metadata.Architecture
            installerType = $Metadata.InstallerType
            scope = $Metadata.Scope
            url = $Metadata.Url
            sha256 = $Metadata.Sha256
            payloadName = $Metadata.PayloadName
        } | ConvertTo-Json -Compress
        [IO.File]::WriteAllText((Join-Path $staging 'complete.json'), $descriptor, (New-Object Text.UTF8Encoding($false)))
        if (-not (Test-ProvisioningPackageCacheEntry -Directory $staging -Metadata $Metadata)) {
            throw "Staged package cache validation failed: $staging"
        }
        if (Test-Path -LiteralPath $EntryDirectory) {
            Assert-ProvisioningCachePath -Path $EntryDirectory
            $displaced = Join-Path $PackageRoot ('.invalid-' + [Guid]::NewGuid().ToString('N'))
            Move-Item -LiteralPath $EntryDirectory -Destination $displaced
        }
        try {
            Move-Item -LiteralPath $staging -Destination $EntryDirectory
        } catch {
            $promotionFailure = $_
            $rollbackFailure = $null
            try {
                if (-not [string]::IsNullOrWhiteSpace($displaced) -and
                    (Test-Path -LiteralPath $displaced) -and
                    -not (Test-Path -LiteralPath $EntryDirectory)) {
                    Move-Item -LiteralPath $displaced -Destination $EntryDirectory
                    $displaced = ''
                }
            } catch {
                $rollbackFailure = $_
            }
            if ($null -ne $rollbackFailure) {
                Write-Warning "Package cache rollback also failed: $($rollbackFailure.Exception.Message)"
            }
            throw $promotionFailure
        }
        if (-not (Test-ProvisioningPackageCacheEntry -Directory $EntryDirectory -Metadata $Metadata)) {
            throw "Published package cache validation failed: $EntryDirectory"
        }
        $promotionSucceeded = $true
    } catch {
        $primaryFailure = $_
    } finally {
        try {
            if (Test-Path -LiteralPath $staging) {
                Assert-ProvisioningCacheTree -Path $staging
                Remove-Item -LiteralPath $staging -Recurse -Force
            }
        } catch {
            $cleanupFailure = $_
        }
        try {
            if ($promotionSucceeded -and -not [string]::IsNullOrWhiteSpace($displaced) -and
                (Test-Path -LiteralPath $displaced)) {
                Assert-ProvisioningCacheTree -Path $displaced
                Remove-Item -LiteralPath $displaced -Recurse -Force
            }
        } catch {
            if ($null -eq $cleanupFailure) {
                $cleanupFailure = $_
            }
        }
    }
    if ($null -ne $primaryFailure) {
        if ($null -ne $cleanupFailure) {
            Write-Warning "Package cache cleanup also failed: $($cleanupFailure.Exception.Message)"
        }
        throw $primaryFailure
    }
    if ($null -ne $cleanupFailure) {
        throw $cleanupFailure
    }
}

function Assert-ProvisioningAuthenticodeSignature {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Role,
        [Parameter(Mandatory = $true)]
        [string]$Path
    )

    $signature = Get-AuthenticodeSignature -LiteralPath $Path
    if ($signature.Status -ne [System.Management.Automation.SignatureStatus]::Valid) {
        throw "$Role Authenticode signature is not valid: $($signature.Status)"
    }
}

function Test-ProvisioningWinGetListOutput {
    param(
        [Parameter(Mandatory = $true)]
        [AllowEmptyString()]
        [string[]]$Lines,
        [Parameter(Mandatory = $true)]
        [object]$Metadata
    )

    $linePattern = '(?:^|\s)' + [Regex]::Escape([string]$Metadata.Id) + '\s+' +
        [Regex]::Escape([string]$Metadata.Version) + '(?:\s|$)'
    return @($Lines | Where-Object { $_ -match $linePattern }).Count -eq 1
}

function Test-ProvisioningWinGetPackageInstalled {
    param(
        [Parameter(Mandatory = $true)]
        [object]$Metadata
    )

    $previousErrorActionPreference = $ErrorActionPreference
    try {
        $ErrorActionPreference = 'Continue'
        $lines = @(& winget.exe list --id $Metadata.Id --exact --source winget `
            --accept-source-agreements --disable-interactivity 2>&1 |
            ForEach-Object { [string]$_ })
        $exitCode = $LASTEXITCODE
    } finally {
        $ErrorActionPreference = $previousErrorActionPreference
    }
    if ($exitCode -ne 0) {
        return $false
    }
    return Test-ProvisioningWinGetListOutput -Lines $lines -Metadata $Metadata
}

function Test-ProvisioningPortablePackageInstalled {
    param(
        [Parameter(Mandatory = $true)]
        [object]$Metadata,
        [Parameter(Mandatory = $true)]
        [string]$ExecutableName
    )

    if ([string]::IsNullOrWhiteSpace($ExecutableName)) {
        return $false
    }
    $toolRoot = Join-Path 'C:\HerdrSandbox\tools' (Get-ProvisioningSafeCacheName -Value $Metadata.Id)
    try {
        if (-not (Test-Path -LiteralPath $toolRoot -PathType Container)) {
            return $false
        }
        $root = Get-Item -LiteralPath $toolRoot -Force
        if (($root.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {
            return $false
        }
        $items = @(Get-ChildItem -LiteralPath $toolRoot -Recurse -Force)
        if (@($items | Where-Object {
                    ($_.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0
                }).Count -ne 0) {
            return $false
        }
        $commands = @($items | Where-Object { -not $_.PSIsContainer -and $_.Name -ieq $ExecutableName })
        if ($commands.Count -ne 1) {
            return $false
        }
        $previousErrorActionPreference = $ErrorActionPreference
        try {
            $ErrorActionPreference = 'Continue'
            $versionOutput = @(& $commands[0].FullName --version 2>&1 | ForEach-Object { [string]$_ })
            $exitCode = $LASTEXITCODE
        } finally {
            $ErrorActionPreference = $previousErrorActionPreference
        }
        $versionPattern = '(?<![0-9A-Za-z])' + [Regex]::Escape([string]$Metadata.Version) + '(?![0-9A-Za-z])'
        if ($exitCode -ne 0 -or ($versionOutput -join [Environment]::NewLine) -notmatch $versionPattern) {
            return $false
        }
        Add-ProvisioningMachinePath -Directory $commands[0].Directory.FullName
        $resolvedCommands = @(Get-Command $ExecutableName -CommandType Application -ErrorAction SilentlyContinue)
        return @($resolvedCommands | Where-Object { $_.Source -ieq $commands[0].FullName }).Count -eq 1
    } catch {
        return $false
    }
}

function Test-ProvisioningRustupInstalled {
    param(
        [Parameter(Mandatory = $true)]
        [object]$Metadata
    )

    if ([string]::IsNullOrWhiteSpace($env:CARGO_HOME)) {
        return $false
    }
    $executable = Join-Path $env:CARGO_HOME 'bin\rustup.exe'
    try {
        if (-not (Test-Path -LiteralPath $executable -PathType Leaf)) {
            return $false
        }
        $file = Get-Item -LiteralPath $executable -Force
        if (($file.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {
            return $false
        }
        $previousErrorActionPreference = $ErrorActionPreference
        try {
            $ErrorActionPreference = 'Continue'
            $versionOutput = @(& $executable --version 2>&1 | ForEach-Object { [string]$_ })
            $exitCode = $LASTEXITCODE
        } finally {
            $ErrorActionPreference = $previousErrorActionPreference
        }
        $versionPattern = '^rustup ' + [Regex]::Escape([string]$Metadata.Version) +
            ' \([0-9a-f]{7,40} [0-9]{4}-[0-9]{2}-[0-9]{2}\)$'
        return $exitCode -eq 0 -and @($versionOutput | Where-Object { $_ -match $versionPattern }).Count -eq 1
    } catch {
        return $false
    }
}

function Get-ProvisioningGeistMonoExpectedFontNames {
    return @(
        'GeistMonoNerdFont-Black.otf',
        'GeistMonoNerdFont-Bold.otf',
        'GeistMonoNerdFont-Light.otf',
        'GeistMonoNerdFont-Medium.otf',
        'GeistMonoNerdFont-Regular.otf',
        'GeistMonoNerdFont-SemiBold.otf',
        'GeistMonoNerdFont-Thin.otf',
        'GeistMonoNerdFont-UltraBlack.otf',
        'GeistMonoNerdFont-UltraLight.otf'
    )
}

function Initialize-ProvisioningFontNativeMethods {
    if ($null -ne ('HerdrSandboxFontNativeMethods' -as [type])) { return }
    Add-Type -TypeDefinition @'
using System;
using System.Runtime.InteropServices;

public static class HerdrSandboxFontNativeMethods
{
    [StructLayout(LayoutKind.Sequential, CharSet = CharSet.Unicode)]
    private struct LOGFONT
    {
        public int lfHeight;
        public int lfWidth;
        public int lfEscapement;
        public int lfOrientation;
        public int lfWeight;
        public byte lfItalic;
        public byte lfUnderline;
        public byte lfStrikeOut;
        public byte lfCharSet;
        public byte lfOutPrecision;
        public byte lfClipPrecision;
        public byte lfQuality;
        public byte lfPitchAndFamily;

        [MarshalAs(UnmanagedType.ByValTStr, SizeConst = 32)]
        public string lfFaceName;
    }

    [UnmanagedFunctionPointer(CallingConvention.Winapi)]
    private delegate int FontEnumerationCallback(
        IntPtr logFont, IntPtr textMetric, uint fontType, IntPtr parameter);

    [DllImport("gdi32.dll", CharSet = CharSet.Unicode)]
    public static extern int AddFontResourceExW(string name, uint flags, IntPtr reserved);

    [DllImport("gdi32.dll")]
    private static extern IntPtr CreateCompatibleDC(IntPtr deviceContext);

    [DllImport("gdi32.dll")]
    [return: MarshalAs(UnmanagedType.Bool)]
    private static extern bool DeleteDC(IntPtr deviceContext);

    [DllImport("gdi32.dll", EntryPoint = "EnumFontFamiliesExW", CharSet = CharSet.Unicode)]
    private static extern int EnumFontFamiliesExW(
        IntPtr deviceContext, ref LOGFONT filter, FontEnumerationCallback callback,
        IntPtr parameter, uint flags);

    [DllImport("user32.dll", CharSet = CharSet.Unicode, SetLastError = true)]
    public static extern IntPtr SendMessageTimeoutW(
        IntPtr window, uint message, UIntPtr wParam, IntPtr lParam,
        uint flags, uint timeoutMilliseconds, out UIntPtr result);

    public static bool HasFamily(string family)
    {
        LOGFONT filter = new LOGFONT();
        filter.lfCharSet = 1;
        filter.lfFaceName = family;
        IntPtr deviceContext = CreateCompatibleDC(IntPtr.Zero);
        if (deviceContext == IntPtr.Zero)
        {
            throw new InvalidOperationException("CreateCompatibleDC failed.");
        }
        bool found = false;
        FontEnumerationCallback callback = delegate(
            IntPtr logFont, IntPtr textMetric, uint fontType, IntPtr parameter)
        {
            found = true;
            return 0;
        };
        try
        {
            EnumFontFamiliesExW(deviceContext, ref filter, callback, IntPtr.Zero, 0);
            GC.KeepAlive(callback);
            return found;
        }
        finally
        {
            if (!DeleteDC(deviceContext))
            {
                throw new InvalidOperationException("DeleteDC failed.");
            }
        }
    }
}
'@
}

function Test-ProvisioningGeistMonoFontPayload {
    param(
        [Parameter(Mandatory = $true)]
        [object]$Metadata
    )

    if ([string]$Metadata.Id -cne 'NerdFonts.GeistMono' -or [string]$Metadata.Version -cne '3.4.0' -or
        [string]$Metadata.Sha256 -cne 'A9F61B7B7F0429DB4FA9A526940F71190127ED95DBE3533163D80D7CAFDB3EC9') {
        return $false
    }
    $toolRoot = Join-Path 'C:\HerdrSandbox\tools' (Get-ProvisioningSafeCacheName -Value $Metadata.Id)
    try {
        if (-not (Test-Path -LiteralPath $toolRoot -PathType Container)) {
            return $false
        }
        $items = @(Get-ChildItem -LiteralPath $toolRoot -Recurse -Force)
        if ($items.Count -ne 29 -or @($items | Where-Object {
                    $_.PSIsContainer -or $_.DirectoryName -cne $toolRoot -or
                    ($_.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0
                }).Count -ne 0) {
            return $false
        }
        $familyFonts = @($items | Where-Object { $_.Name -like 'GeistMonoNerdFont-*.otf' } | Sort-Object Name)
        $expectedFontNames = @(Get-ProvisioningGeistMonoExpectedFontNames)
        if (($familyFonts.Name -join '|') -cne ($expectedFontNames -join '|')) {
            return $false
        }
        return $true
    } catch {
        return $false
    }
}

function Test-ProvisioningGeistMonoFontInstalled {
    param(
        [Parameter(Mandatory = $true)]
        [object]$Metadata
    )

    if (-not (Test-ProvisioningGeistMonoFontPayload -Metadata $Metadata)) {
        return $false
    }
    try {
        Initialize-ProvisioningFontNativeMethods
        return [HerdrSandboxFontNativeMethods]::HasFamily('GeistMono NF')
    } catch {
        return $false
    }
}

function Test-ProvisioningPackageInstalled {
    param(
        [Parameter(Mandatory = $true)]
        [object]$Metadata,
        [Parameter(Mandatory = $true)]
        [string]$Adapter,
        [string]$ExecutableName = ''
    )

    switch ($Adapter) {
        'Portable' {
            return Test-ProvisioningPortablePackageInstalled -Metadata $Metadata -ExecutableName $ExecutableName
        }
        'GeistMonoFont' {
            return Test-ProvisioningGeistMonoFontInstalled -Metadata $Metadata
        }
        'Rustup' {
            return Test-ProvisioningRustupInstalled -Metadata $Metadata
        }
        { $_ -in @('Inno', 'MSI', 'Burn', 'MSIX') } {
            return Test-ProvisioningWinGetPackageInstalled -Metadata $Metadata
        }
        default {
            return $false
        }
    }
}

function Install-ProvisioningGeistMonoFontPayload {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Role,
        [Parameter(Mandatory = $true)]
        [object]$Metadata,
        [Parameter(Mandatory = $true)]
        [string]$PayloadPath
    )

    if ([string]$Metadata.Id -cne 'NerdFonts.GeistMono' -or [string]$Metadata.Version -cne '3.4.0' -or
        [string]$Metadata.Architecture -cne 'neutral' -or [string]$Metadata.InstallerType -cne 'zip') {
        throw "$Role metadata does not match the pinned GeistMono Nerd Font contract."
    }
    $toolRoot = Join-Path 'C:\HerdrSandbox\tools' (Get-ProvisioningSafeCacheName -Value $Metadata.Id)
    if (-not (Test-ProvisioningGeistMonoFontPayload -Metadata $Metadata)) {
        if (Test-Path -LiteralPath $toolRoot) {
            Remove-Item -LiteralPath $toolRoot -Recurse -Force
        }
        New-Item -ItemType Directory -Path $toolRoot | Out-Null
        $tar = Join-Path $env:SystemRoot 'System32\tar.exe'
        $archiveEntries = @(Invoke-ProvisioningNative -Role "$Role archive inspection" -FilePath $tar `
            -ArgumentList @('-tf', $PayloadPath) | ForEach-Object { [string]$_ })
        if ($archiveEntries.Count -ne 29 -or @($archiveEntries | Where-Object {
                [string]::IsNullOrWhiteSpace($_) -or $_ -match '[/\\]' }).Count -ne 0 -or
            $archiveEntries -notcontains 'LICENSE' -or $archiveEntries -notcontains 'README.md' -or
            @($archiveEntries | Where-Object { $_ -like '*.otf' }).Count -ne 27) {
            throw "$Role archive does not match the flat 27-OTF release contract."
        }
        Invoke-ProvisioningNative -Role "$Role cached extraction" -FilePath $tar `
            -ArgumentList @('-xf', $PayloadPath, '-C', $toolRoot) | Out-Null
        $extractedItems = @(Get-ChildItem -LiteralPath $toolRoot -Recurse -Force)
        foreach ($item in $extractedItems) {
            if (($item.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {
                throw "$Role archive produced a reparse point: $($item.FullName)"
            }
            if ($item.PSIsContainer -or $item.DirectoryName -cne $toolRoot) {
                throw "$Role archive produced a non-flat entry: $($item.FullName)"
            }
        }
        if ($extractedItems.Count -ne 29) {
            throw "$Role extracted $($extractedItems.Count) entries; expected 29."
        }
    } else {
        Write-Host "$Role payload already matches; loading it into the current Windows session."
    }
    $expectedFontNames = @(Get-ProvisioningGeistMonoExpectedFontNames)
    $familyFonts = @(Get-ChildItem -LiteralPath $toolRoot -File -Filter 'GeistMonoNerdFont-*.otf' |
        Sort-Object Name)
    if (($familyFonts.Name -join '|') -cne ($expectedFontNames -join '|')) {
        throw "$Role selected font files do not match the GeistMono Nerd Font family contract."
    }
    Initialize-ProvisioningFontNativeMethods
    foreach ($font in $familyFonts) {
        $added = [HerdrSandboxFontNativeMethods]::AddFontResourceExW($font.FullName, 0, [IntPtr]::Zero)
        if ($added -le 0) {
            throw "$Role failed to add font resource: $($font.Name)"
        }
    }
    $messageResult = [UIntPtr]::Zero
    $messageSent = [HerdrSandboxFontNativeMethods]::SendMessageTimeoutW(
        [IntPtr]0xffff, 0x001d, [UIntPtr]::Zero, [IntPtr]::Zero, 0x0002, 5000, [ref]$messageResult)
    if ($messageSent -eq [IntPtr]::Zero) {
        throw "$Role failed to broadcast WM_FONTCHANGE."
    }
    if (-not [HerdrSandboxFontNativeMethods]::HasFamily('GeistMono NF')) {
        throw "$Role GDI family read-back failed for GeistMono NF."
    }
}

function Install-ProvisioningPackagePayload {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Role,
        [Parameter(Mandatory = $true)]
        [object]$Metadata,
        [Parameter(Mandatory = $true)]
        [string]$PayloadPath,
        [Parameter(Mandatory = $true)]
        [ValidateSet('Inno', 'MSI', 'Burn', 'MSIX', 'Portable', 'Rustup', 'GeistMonoFont')]
        [string]$Adapter,
        [string]$ExecutableName = '',
        [string[]]$InstallerArguments = @(),
        [int[]]$InstallerSuccessExitCodes = @(0),
        [string]$CommandSourceExclusion = '',
        [switch]$DeferCommandReadiness,
        [switch]$RequireAuthenticodeSignature
    )

    if ($RequireAuthenticodeSignature) {
        Assert-ProvisioningAuthenticodeSignature -Role $Role -Path $PayloadPath
    }
    switch ($Adapter) {
        'Inno' {
            if ([string]::IsNullOrWhiteSpace($ExecutableName)) {
                throw "$Role Inno adapter requires ExecutableName."
            }
            Invoke-ProvisioningNative -Role "$Role cached installation" -FilePath $PayloadPath `
                -ArgumentList @('/SP-', '/VERYSILENT', '/SUPPRESSMSGBOXES', '/NORESTART') `
                -WaitForProcessTree | Out-Null
        }
        'MSI' {
            Invoke-ProvisioningNative -Role "$Role cached installation" -FilePath "$env:SystemRoot\System32\msiexec.exe" `
                -ArgumentList (@('/i', $PayloadPath, '/quiet', '/norestart') + $InstallerArguments) `
                -WaitForProcessTree | Out-Null
        }
        'Burn' {
            $burnArguments = if ($InstallerArguments.Count -eq 0) {
                @('/quiet', '/norestart', 'InstallAllUsers=1', 'PrependPath=1')
            } else {
                @($InstallerArguments)
            }
            Invoke-ProvisioningNative -Role "$Role cached installation" -FilePath $PayloadPath `
                -ArgumentList $burnArguments `
                -SuccessExitCodes $InstallerSuccessExitCodes `
                -WaitForProcessTree | Out-Null
        }
        'MSIX' {
            Add-AppxPackage -Path $PayloadPath -ErrorAction Stop | Out-Null
        }
        'Portable' {
            if ([string]::IsNullOrWhiteSpace($ExecutableName)) {
                throw "$Role portable adapter requires ExecutableName."
            }
            $toolRoot = Join-Path 'C:\HerdrSandbox\tools' (Get-ProvisioningSafeCacheName -Value $Metadata.Id)
            if (Test-Path -LiteralPath $toolRoot) {
                Remove-Item -LiteralPath $toolRoot -Recurse -Force
            }
            New-Item -ItemType Directory -Path $toolRoot | Out-Null
            $tar = Join-Path $env:SystemRoot 'System32\tar.exe'
            Invoke-ProvisioningNative -Role "$Role cached extraction" -FilePath $tar `
                -ArgumentList @('-xf', $PayloadPath, '-C', $toolRoot) | Out-Null
            foreach ($item in @(Get-ChildItem -LiteralPath $toolRoot -Recurse -Force)) {
                if (($item.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {
                    throw "$Role archive produced a reparse point: $($item.FullName)"
                }
            }
            $commands = @(Get-ChildItem -LiteralPath $toolRoot -File -Recurse -Filter $ExecutableName)
            if ($commands.Count -ne 1) {
                throw "$Role archive contains $($commands.Count) $ExecutableName files; expected one."
            }
            if (($commands[0].Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {
                throw "$Role executable is a reparse point: $($commands[0].FullName)"
            }
            Add-ProvisioningMachinePath -Directory $commands[0].Directory.FullName
        }
        'Rustup' {
            $rustupInstaller = Join-Path (Split-Path -Parent $PayloadPath) 'rustup-init.exe'
            if ($rustupInstaller -ine $PayloadPath) {
                if (Test-Path -LiteralPath $rustupInstaller) {
                    throw "$Role guest stage already contains rustup-init.exe."
                }
                Copy-Item -LiteralPath $PayloadPath -Destination $rustupInstaller
                $rustupInstallerHash = (Get-FileHash -LiteralPath $rustupInstaller -Algorithm SHA256).Hash.ToUpperInvariant()
                if ($rustupInstallerHash -cne [string]$Metadata.Sha256) {
                    throw "$Role guest installer copy hash mismatch: $rustupInstallerHash"
                }
            }
            Invoke-ProvisioningNative -Role "$Role cached installation" -FilePath $rustupInstaller `
                -ArgumentList $InstallerArguments | Out-Null
        }
        'GeistMonoFont' {
            Install-ProvisioningGeistMonoFontPayload -Role $Role -Metadata $Metadata -PayloadPath $PayloadPath
        }
    }
    Update-ProvisioningPath
    if (-not $DeferCommandReadiness -and -not [string]::IsNullOrWhiteSpace($ExecutableName)) {
        Wait-ProvisioningCommandAvailable -Role $Role -Name $ExecutableName `
            -CommandSourceExclusion $CommandSourceExclusion | Out-Null
    }
}

function Get-ProvisioningDirectPackage {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Role,
        [Parameter(Mandatory = $true)]
        [object]$Metadata,
        [Parameter(Mandatory = $true)]
        [string]$GuestPayloadPath
    )

    $downloadStopwatch = [Diagnostics.Stopwatch]::StartNew()
    try {
        Write-ProvisioningProgress -Message "$Role package download"
        Invoke-WebRequest -Uri $Metadata.Url -OutFile $GuestPayloadPath
    } finally {
        $downloadStopwatch.Stop()
        Write-ProvisioningTiming -Role "$Role package download" -Seconds $downloadStopwatch.Elapsed.TotalSeconds
    }
    $actualHash = (Get-FileHash -LiteralPath $GuestPayloadPath -Algorithm SHA256).Hash.ToUpperInvariant()
    if ($actualHash -cne [string]$Metadata.Sha256) {
        throw "$Role downloaded package hash mismatch: $actualHash"
    }
}

function Install-ProvisioningCachedPackage {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Role,
        [Parameter(Mandatory = $true)]
        [object]$Metadata,
        [Parameter(Mandatory = $true)]
        [ValidateSet('WinGet', 'Direct')]
        [string]$DownloadSource,
        [Parameter(Mandatory = $true)]
        [ValidateSet('Inno', 'MSI', 'Burn', 'MSIX', 'Portable', 'Rustup', 'GeistMonoFont')]
        [string]$Adapter,
        [string]$ExecutableName = '',
        [string[]]$InstallerArguments = @(),
        [int[]]$InstallerSuccessExitCodes = @(0),
        [string]$CommandSourceExclusion = '',
        [switch]$DeferCommandReadiness,
        [switch]$RequireAuthenticodeSignature
    )

    $packageStopwatch = [Diagnostics.Stopwatch]::StartNew()
    $metadata = $Metadata
    Update-ProvisioningPath
    if (Test-ProvisioningPackageInstalled -Metadata $metadata -Adapter $Adapter -ExecutableName $ExecutableName) {
        Write-Host "$Role already matches requested version: $($metadata.Version)"
        $packageStopwatch.Stop()
        Write-ProvisioningTiming -Role "$Role package total" -Seconds $packageStopwatch.Elapsed.TotalSeconds
        return
    }
    $cacheRoot = 'C:\HerdrSandbox\cache\packages'
    if (-not (Test-Path -LiteralPath 'C:\HerdrSandbox\cache' -PathType Container)) {
        throw 'The writable guest package cache mapping is missing: C:\HerdrSandbox\cache'
    }
    Assert-ProvisioningCachePath -Path 'C:\HerdrSandbox\cache'
    New-Item -ItemType Directory -Path $cacheRoot -Force | Out-Null
    Assert-ProvisioningCachePath -Path $cacheRoot
    $packageRoot = Join-Path $cacheRoot (Get-ProvisioningSafeCacheName -Value $metadata.Id)
    New-Item -ItemType Directory -Path $packageRoot -Force | Out-Null
    Assert-ProvisioningCachePath -Path $packageRoot
    $entryName = (Get-ProvisioningSafeCacheName -Value $metadata.Version) + '-' + $metadata.Sha256.Substring(0, 16).ToLowerInvariant()
    $entryDirectory = Join-Path $packageRoot $entryName
    $lockPath = Join-Path $packageRoot '.lock'
    $lock = $null
    $guestStage = Join-Path 'C:\HerdrSandbox\staging\packages' ([Guid]::NewGuid().ToString('N'))
    New-Item -ItemType Directory -Path $guestStage -Force | Out-Null
    $guestPayload = Join-Path $guestStage $metadata.PayloadName
    $primaryFailure = $null
    $cleanupFailure = $null
    try {
        Assert-ProvisioningCachePath -Path $lockPath
        $lock = [IO.File]::Open($lockPath, [IO.FileMode]::OpenOrCreate, [IO.FileAccess]::ReadWrite, [IO.FileShare]::None)
        if (Test-Path -LiteralPath $entryDirectory) {
            Assert-ProvisioningCachePath -Path $entryDirectory
        }
        $cacheHit = Test-ProvisioningPackageCacheEntry -Directory $entryDirectory -Metadata $metadata
        if ($cacheHit) {
            Write-Output "$Role package cache hit: $($metadata.Version)"
            Copy-ProvisioningPackageToGuest -Source (Join-Path $entryDirectory $metadata.PayloadName) `
                -Destination $guestPayload -ExpectedSHA256 $metadata.Sha256
        } else {
            Write-Output "$Role package cache miss: $($metadata.Version)"
            if ($DownloadSource -eq 'WinGet') {
                $downloadDirectory = Join-Path $guestStage 'download'
                New-Item -ItemType Directory -Path $downloadDirectory | Out-Null
                Get-ProvisioningDownloadedPackage -Role $Role -Metadata $metadata `
                    -DownloadDirectory $downloadDirectory -GuestPayloadPath $guestPayload
            } else {
                Get-ProvisioningDirectPackage -Role $Role -Metadata $metadata -GuestPayloadPath $guestPayload
            }
        }
        Write-ProvisioningProgress -Message "$Role cached installation"
        Install-ProvisioningPackagePayload -Role $Role -Metadata $metadata -PayloadPath $guestPayload `
            -Adapter $Adapter -ExecutableName $ExecutableName -InstallerArguments $InstallerArguments `
            -InstallerSuccessExitCodes $InstallerSuccessExitCodes `
            -CommandSourceExclusion $CommandSourceExclusion `
            -DeferCommandReadiness:$DeferCommandReadiness `
            -RequireAuthenticodeSignature:$RequireAuthenticodeSignature
        if (-not (Test-ProvisioningPackageInstalled -Metadata $metadata -Adapter $Adapter `
                -ExecutableName $ExecutableName)) {
            throw "$Role installed package does not match resolved version $($metadata.Version)."
        }
        if (-not $cacheHit) {
            Publish-ProvisioningPackageCacheEntry -PackageRoot $packageRoot -EntryDirectory $entryDirectory `
                -GuestPayloadPath $guestPayload -Metadata $metadata
        }
        foreach ($directory in @(Get-ChildItem -LiteralPath $packageRoot -Directory -Force)) {
            if ($directory.Name -ine $entryName) {
                Assert-ProvisioningCacheTree -Path $directory.FullName
                Remove-Item -LiteralPath $directory.FullName -Recurse -Force
            }
        }
    } catch {
        $primaryFailure = $_
    } finally {
        if ($null -ne $lock) {
            try {
                $lock.Dispose()
            } catch {
                $cleanupFailure = $_
            }
        }
        try {
            if (Test-Path -LiteralPath $guestStage) {
                Remove-ProvisioningGuestPackageStage -Path $guestStage -Attempts 1 `
                    -DelayMilliseconds 0 -BestEffort | Out-Null
            }
        } catch {
            if ($null -eq $cleanupFailure) {
                $cleanupFailure = $_
            }
        }
        $packageStopwatch.Stop()
        Write-ProvisioningTiming -Role "$Role package total" -Seconds $packageStopwatch.Elapsed.TotalSeconds
    }
    if ($null -ne $primaryFailure) {
        if ($null -ne $cleanupFailure) {
            Write-Warning "$Role package cleanup also failed: $($cleanupFailure.Exception.Message)"
        }
        throw $primaryFailure
    }
    if ($null -ne $cleanupFailure) {
        throw $cleanupFailure
    }
}

function Install-ProvisioningWinGetPackage {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Role,
        [Parameter(Mandatory = $true)]
        [string]$Id,
        [string]$Version = '',
        [Parameter(Mandatory = $true)]
        [string]$InstallerType,
        [string]$Scope = '',
        [Parameter(Mandatory = $true)]
        [ValidateSet('Inno', 'MSI', 'Burn', 'MSIX', 'Portable', 'Rustup')]
        [string]$Adapter,
        [string]$ExecutableName = '',
        [string[]]$InstallerArguments = @(),
        [string]$CommandSourceExclusion = '',
        [switch]$DeferCommandReadiness,
        [switch]$RequireAuthenticodeSignature
    )

    $metadata = Get-ProvisioningWinGetMetadata -Role $Role -Id $Id -Version $Version `
        -InstallerType $InstallerType -Scope $Scope
    Install-ProvisioningCachedPackage -Role $Role -Metadata $metadata -DownloadSource 'WinGet' `
        -Adapter $Adapter -ExecutableName $ExecutableName -InstallerArguments $InstallerArguments `
        -CommandSourceExclusion $CommandSourceExclusion `
        -DeferCommandReadiness:$DeferCommandReadiness `
        -RequireAuthenticodeSignature:$RequireAuthenticodeSignature
}

function Install-ProvisioningGeistMonoNerdFont {
    $metadata = [pscustomobject]@{
        Id = 'NerdFonts.GeistMono'
        Version = '3.4.0'
        Architecture = 'neutral'
        InstallerType = 'zip'
        Scope = ''
        Url = 'https://github.com/ryanoasis/nerd-fonts/releases/download/v3.4.0/GeistMono.zip'
        Sha256 = 'A9F61B7B7F0429DB4FA9A526940F71190127ED95DBE3533163D80D7CAFDB3EC9'
        PayloadName = 'payload.zip'
    }
    $uri = [Uri]$metadata.Url
    if ($uri.Scheme -cne 'https' -or $uri.Host -cne 'github.com' -or
        $uri.AbsolutePath -cne '/ryanoasis/nerd-fonts/releases/download/v3.4.0/GeistMono.zip' -or
        [string]$metadata.Sha256 -notmatch '^[A-F0-9]{64}$') {
        throw 'Pinned GeistMono Nerd Font metadata is invalid.'
    }
    Install-ProvisioningCachedPackage -Role 'GeistMono Nerd Font' -Metadata $metadata `
        -DownloadSource 'Direct' -Adapter 'GeistMonoFont'
}

function Install-ProvisioningOnlineWinGetPackage {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Role,
        [Parameter(Mandatory = $true)]
        [string]$Id,
        [string]$Version = '',
        [string]$Override = ''
    )

    $resolvedVersion = $Version
    if ([string]::IsNullOrWhiteSpace($resolvedVersion)) {
        $matches = @(Search-ProvisioningWinGetPackages -Role $Role -IdQuery $Id -Exact)
        if ($matches.Count -ne 1 -or [string]$matches[0].Id -cne $Id -or
            [string]::IsNullOrWhiteSpace([string]$matches[0].Version)) {
            throw "$Role latest WinGet version did not resolve one exact package $Id."
        }
        $resolvedVersion = [string]$matches[0].Version
    }
    $metadata = [pscustomobject]@{ Id = $Id; Version = $resolvedVersion }
    if (Test-ProvisioningWinGetPackageInstalled -Metadata $metadata) {
        Write-Host "$Role online package already matches requested version: $resolvedVersion"
        return
    }
    $arguments = @(
        'install', '--id', $Id, '--exact', '--source', 'winget', '--silent',
        '--version', $resolvedVersion,
        '--accept-package-agreements', '--accept-source-agreements', '--disable-interactivity'
    )
    if (-not [string]::IsNullOrWhiteSpace($Override)) {
        $arguments += @('--override', $Override)
    }
    Invoke-ProvisioningNative -Role "$Role online installation" -FilePath 'winget.exe' -ArgumentList $arguments | Out-Null
    Update-ProvisioningPath
    if (-not (Test-ProvisioningWinGetPackageInstalled -Metadata $metadata)) {
        throw "$Role installed package does not match resolved version $resolvedVersion."
    }
}

function Assert-ProvisioningCommand {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Role,
        [Parameter(Mandatory = $true)]
        [string]$Name,
        [Parameter(Mandatory = $true)]
        [string[]]$VersionArguments,
        [Parameter(Mandatory = $true)]
        [string]$ExpectedPattern
    )

    $command = Get-Command $Name -CommandType Application -ErrorAction SilentlyContinue | Select-Object -First 1
    if ($null -eq $command) {
        throw "$Role command is not available on PATH: $Name"
    }
    $output = Invoke-ProvisioningNative -Role "$Role version check" -FilePath $command.Source -ArgumentList $VersionArguments
    $version = ($output -join [Environment]::NewLine).Trim()
    if ($version -notmatch $ExpectedPattern) {
        throw "$Role version output is unexpected: $version"
    }
    return $version
}

function Get-ProvisioningPowerShell7Installation {
    $packages = @(Get-AppxPackage -Name 'Microsoft.PowerShell' -ErrorAction SilentlyContinue)
    if ($packages.Count -ne 1) {
        throw "Expected one registered stable PowerShell 7 package, found $($packages.Count)."
    }
    $package = $packages[0]
    $packageVersion = [string]$package.Version
    if ([string]$package.Name -cne 'Microsoft.PowerShell' -or
        [string]$package.Architecture -cne 'X64' -or
        [string]$package.Publisher -cne 'CN=Microsoft Corporation, O=Microsoft Corporation, L=Redmond, S=Washington, C=US' -or
        $packageVersion -notmatch '^\d+\.\d+\.\d+\.\d+$') {
        throw "PowerShell 7 package identity is unexpected: name=$($package.Name) version=$packageVersion architecture=$($package.Architecture)"
    }
    $version = [Version]$packageVersion
    $displayVersion = "$($version.Major).$($version.Minor).$($version.Build)"
    $executable = Join-Path ([string]$package.InstallLocation) 'pwsh.exe'
    if (-not (Test-Path -LiteralPath $executable -PathType Leaf)) {
        throw "PowerShell 7 package is missing pwsh.exe: $executable"
    }
    $file = Get-Item -LiteralPath $executable -Force
    if (($file.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0 -or
        -not ([string]$file.VersionInfo.FileVersion).StartsWith($displayVersion + '.', [StringComparison]::Ordinal) -or
        -not ([string]$file.VersionInfo.ProductVersion).StartsWith($displayVersion + ' ', [StringComparison]::Ordinal)) {
        throw "PowerShell 7 executable metadata does not match package version $packageVersion."
    }
    $signature = Get-AuthenticodeSignature -LiteralPath $executable
    if ($signature.Status -ne [System.Management.Automation.SignatureStatus]::Valid -or
        $null -eq $signature.SignerCertificate -or
        $signature.SignerCertificate.Subject -notmatch '(^|,\s*)O=Microsoft Corporation(,|$)') {
        throw "PowerShell 7 executable signature is invalid: $($signature.Status)"
    }
    return [pscustomobject]@{
        PackageVersion = $packageVersion
        DisplayVersion = $displayVersion
        Executable = $executable
    }
}

function Ensure-ProvisioningRegistryKey {
    [CmdletBinding()]
    param([Parameter(Mandatory = $true)][string]$Path)

    if (Test-Path -LiteralPath $Path) { return }
    $parent = Split-Path -Path $Path -Parent
    if ([string]::IsNullOrWhiteSpace($parent) -or $parent -eq $Path) {
        throw "Cannot resolve registry parent for: $Path"
    }
    if (-not (Test-Path -LiteralPath $parent)) {
        Ensure-ProvisioningRegistryKey -Path $parent
    }
    try {
        New-Item -Path $Path -ErrorAction Stop | Out-Null
    } catch {
        if (-not (Test-Path -LiteralPath $Path)) { throw }
    }
    if (-not (Test-Path -LiteralPath $Path)) {
        throw "Registry key creation did not materialize: $Path"
    }
}

function Set-ProvisioningRegistryValue {
    [CmdletBinding()]
    param(
        [Parameter(Mandatory = $true)]
        [string]$Path,
        [Parameter(Mandatory = $true)]
        [AllowEmptyString()]
        [string]$Name,
        [Parameter(Mandatory = $true)]
        [object]$Value,
        [Parameter(Mandatory = $true)]
        [ValidateSet('DWord', 'String')]
        [string]$PropertyType
    )

    Ensure-ProvisioningRegistryKey -Path $Path
    $displayName = $Name
    if ([string]::IsNullOrEmpty($displayName)) { $displayName = '(Default)' }
    if ($Path.StartsWith('HKCU:\', [StringComparison]::OrdinalIgnoreCase)) {
        $baseKey = [Microsoft.Win32.Registry]::CurrentUser
    } elseif ($Path.StartsWith('HKLM:\', [StringComparison]::OrdinalIgnoreCase)) {
        $baseKey = [Microsoft.Win32.Registry]::LocalMachine
    } else {
        throw "Unsupported registry hive for typed value: $Path"
    }
    $registryKey = $baseKey.OpenSubKey($Path.Substring(6), $true)
    if ($null -eq $registryKey) {
        throw "Registry key could not be opened for typed value: $Path"
    }
    $operation = 'inspection'
    try {
        $matches = $false
        if (@($registryKey.GetValueNames()) -contains $Name) {
            $currentKind = [string]$registryKey.GetValueKind($Name)
            $currentValue = $registryKey.GetValue(
                $Name, $null, [Microsoft.Win32.RegistryValueOptions]::DoNotExpandEnvironmentNames)
            if ($currentKind -ceq $PropertyType) {
                if ($PropertyType -eq 'String') {
                    $matches = [string]$currentValue -ceq [string]$Value
                } else {
                    $matches = [int]$currentValue -eq [int]$Value
                }
            }
        }
        if ($matches) {
            return $false
        }
        $operation = 'write'
        if ($PropertyType -eq 'String') {
            $registryKey.SetValue($Name, [string]$Value, [Microsoft.Win32.RegistryValueKind]::String)
        } else {
            $registryKey.SetValue($Name, [int]$Value, [Microsoft.Win32.RegistryValueKind]::DWord)
        }
        $operation = 'read-back'
        $verifiedKind = [string]$registryKey.GetValueKind($Name)
        $verifiedValue = $registryKey.GetValue(
            $Name, $null, [Microsoft.Win32.RegistryValueOptions]::DoNotExpandEnvironmentNames)
        if ($verifiedKind -cne $PropertyType -or
            ($PropertyType -eq 'String' -and [string]$verifiedValue -cne [string]$Value) -or
            ($PropertyType -eq 'DWord' -and [int]$verifiedValue -ne [int]$Value)) {
            throw "Registry value verification failed: $Path::$displayName"
        }
        return $true
    } catch {
        throw "Registry value $operation failed: $Path::$displayName`: $($_.Exception.Message)"
    } finally {
        $registryKey.Dispose()
    }
}

function Restart-ProvisioningExplorerShell {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Role
    )

    Write-Host "Stopping all Explorer processes after $Role..."
    $stoppedExplorerProcessIDs = @()
    $explorerStopDeadline = [DateTime]::UtcNow.AddSeconds(30)
    do {
        $explorerProcesses = @(Get-Process -Name explorer -ErrorAction SilentlyContinue)
        if ($explorerProcesses.Count -eq 0) { break }
        $stoppedExplorerProcessIDs += @($explorerProcesses | ForEach-Object { [int]$_.Id })
        $explorerProcesses | Stop-Process -Force -ErrorAction Stop
        if ([DateTime]::UtcNow -ge $explorerStopDeadline) {
            throw "Explorer processes did not stop within 30 seconds: $($explorerProcesses.Id -join ', ')"
        }
        Start-Sleep -Milliseconds 250
    } while ($true)
    if ($stoppedExplorerProcessIDs.Count -eq 0) {
        throw "Explorer shell was not running before the required $Role restart."
    }

    Write-Host 'Starting one fresh Explorer shell...'
    Start-Process -FilePath (Join-Path $env:WINDIR 'explorer.exe') | Out-Null
    $explorerStartDeadline = [DateTime]::UtcNow.AddSeconds(30)
    do {
        $newExplorerProcesses = @(Get-Process -Name explorer -ErrorAction SilentlyContinue |
            Where-Object { $stoppedExplorerProcessIDs -notcontains [int]$_.Id })
        if ($newExplorerProcesses.Count -gt 0) { break }
        if ([DateTime]::UtcNow -ge $explorerStartDeadline) {
            throw 'Explorer shell did not restart within 30 seconds.'
        }
        Start-Sleep -Milliseconds 250
    } while ($true)
    Write-Host "Explorer shell restarted: $($stoppedExplorerProcessIDs -join ', ') -> $($newExplorerProcesses.Id -join ', ')"
}

function Ensure-ProvisioningFilePilotStartShortcut {
    $filePilotExecutable = Join-Path $env:LOCALAPPDATA `
        'Microsoft\WinGet\Packages\Voidstar.FilePilot_Microsoft.Winget.Source_8wekyb3d8bbwe\FPilot.exe'
    if (-not (Test-Path -LiteralPath $filePilotExecutable -PathType Leaf)) {
        throw "File Pilot portable executable is missing: $filePilotExecutable"
    }
    $filePilotExecutableInfo = Get-Item -LiteralPath $filePilotExecutable -Force
    if (($filePilotExecutableInfo.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {
        throw "File Pilot portable executable is a reparse point: $filePilotExecutable"
    }

    $shortcutDirectory = Join-Path $env:APPDATA 'Microsoft\Windows\Start Menu\Programs'
    New-Item -ItemType Directory -Path $shortcutDirectory -Force | Out-Null
    $shortcutDirectoryInfo = Get-Item -LiteralPath $shortcutDirectory -Force
    if (($shortcutDirectoryInfo.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {
        throw "File Pilot Start shortcut directory is a reparse point: $shortcutDirectory"
    }
    $shortcutPath = Join-Path $shortcutDirectory 'File Pilot.lnk'
    $workingDirectory = Split-Path -Parent $filePilotExecutable
    $shell = New-Object -ComObject WScript.Shell
    $shortcut = $shell.CreateShortcut($shortcutPath)
    $matches = (Test-Path -LiteralPath $shortcutPath -PathType Leaf) -and
        [string]$shortcut.TargetPath -ieq $filePilotExecutable -and
        [string]$shortcut.WorkingDirectory -ieq $workingDirectory -and
        [string]::IsNullOrEmpty([string]$shortcut.Arguments)
    if (-not $matches) {
        $shortcut.TargetPath = $filePilotExecutable
        $shortcut.WorkingDirectory = $workingDirectory
        $shortcut.Arguments = ''
        $shortcut.Description = 'File Pilot'
        $shortcut.Save()
    }
    $shortcutInfo = Get-Item -LiteralPath $shortcutPath -Force
    $verifiedShortcut = $shell.CreateShortcut($shortcutPath)
    if (($shortcutInfo.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0 -or
        [string]$verifiedShortcut.TargetPath -ine $filePilotExecutable -or
        [string]$verifiedShortcut.WorkingDirectory -ine $workingDirectory -or
        -not [string]::IsNullOrEmpty([string]$verifiedShortcut.Arguments)) {
        throw 'File Pilot Start shortcut read-back did not match the installed portable executable.'
    }
    Write-Host "File Pilot Start shortcut ready: $shortcutPath"
}

function Ensure-ProvisioningTaskbarPins {
    param(
        [Parameter(Mandatory = $true)]
        [ValidateSet('stable', 'preview')]
        [string]$Edition,
        [string]$TerminalPackageFamily = ''
    )

    $pinElements = New-Object 'System.Collections.Generic.List[string]'
    $pinNames = New-Object 'System.Collections.Generic.List[string]'
    $terminalPackageID = [string]$provisioningPackagePlan.TerminalID
    if (Test-ProvisioningPackageEnabled -Id $terminalPackageID) {
        if ([string]::IsNullOrWhiteSpace($TerminalPackageFamily)) {
            throw "Windows Terminal $Edition taskbar pin requires its package family."
        }
        $terminalAUMID = $TerminalPackageFamily + '!App'
        $terminalStartApps = @(Get-StartApps -ErrorAction Stop |
            Where-Object { [string]$_.AppID -ceq $terminalAUMID })
        if ($terminalStartApps.Count -ne 1) {
            throw "Windows Terminal $Edition AUMID resolved to $($terminalStartApps.Count) Start applications: $terminalAUMID"
        }
        $pinElements.Add('<taskbar:UWA AppUserModelID="' + $terminalAUMID + '" />') | Out-Null
        $pinNames.Add("Windows Terminal $Edition") | Out-Null
    }
    if (Test-ProvisioningPackageEnabled -Id 'WinDirStat.WinDirStat') {
        $winDirStatStartApps = @(Get-StartApps -ErrorAction Stop |
            Where-Object { [string]$_.AppID -ceq 'WinDirStat' })
        if ($winDirStatStartApps.Count -ne 1) {
            throw "WinDirStat desktop application ID resolved to $($winDirStatStartApps.Count) Start applications."
        }
        $pinElements.Add('<taskbar:DesktopApp DesktopApplicationID="WinDirStat" />') | Out-Null
        $pinNames.Add('WinDirStat') | Out-Null
    }
    if (Test-ProvisioningPackageEnabled -Id 'Voidstar.FilePilot') {
        $filePilotShortcut = Join-Path $env:APPDATA 'Microsoft\Windows\Start Menu\Programs\File Pilot.lnk'
        if (-not (Test-Path -LiteralPath $filePilotShortcut -PathType Leaf) -or
            ((Get-Item -LiteralPath $filePilotShortcut -Force).Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {
            throw "File Pilot taskbar shortcut is missing or unsafe: $filePilotShortcut"
        }
        $pinElements.Add('<taskbar:DesktopApp DesktopApplicationLinkPath="%APPDATA%\Microsoft\Windows\Start Menu\Programs\File Pilot.lnk" />') | Out-Null
        $pinNames.Add('File Pilot') | Out-Null
    }
    if ($pinElements.Count -eq 0) {
        Write-Host 'No selected taskbar applications; taskbar policy skipped.'
        return
    }
    $layout = '<?xml version="1.0" encoding="utf-8"?>' +
        '<LayoutModificationTemplate xmlns="http://schemas.microsoft.com/Start/2014/LayoutModification" ' +
        'xmlns:defaultlayout="http://schemas.microsoft.com/Start/2014/FullDefaultLayout" ' +
        'xmlns:start="http://schemas.microsoft.com/Start/2014/StartLayout" ' +
        'xmlns:taskbar="http://schemas.microsoft.com/Start/2014/TaskbarLayout" Version="1">' +
        '<CustomTaskbarLayoutCollection><defaultlayout:TaskbarLayout><taskbar:TaskbarPinList>' +
        ($pinElements -join '') +
        '</taskbar:TaskbarPinList></defaultlayout:TaskbarLayout></CustomTaskbarLayoutCollection>' +
        '</LayoutModificationTemplate>'
    try {
        [xml]$layoutDocument = $layout
    } catch {
        throw "Taskbar layout XML is invalid: $($_.Exception.Message)"
    }
    if ($layoutDocument.DocumentElement.LocalName -cne 'LayoutModificationTemplate') {
        throw 'Taskbar layout root is invalid.'
    }

    $namespace = 'root\cimv2\mdm\dmmap'
    $className = 'MDM_Policy_User_Config01_Start02'
    $parentID = './Vendor/MSFT/Policy/Config'
    $instanceID = 'Start'
    $instances = @(Get-CimInstance -Namespace $namespace -ClassName $className -ErrorAction Stop |
        Where-Object { [string]$_.ParentID -ceq $parentID -and [string]$_.InstanceID -ceq $instanceID })
    if ($instances.Count -gt 1) {
        throw "Taskbar policy resolved to $($instances.Count) matching instances."
    }
    if ($instances.Count -eq 1 -and [string]$instances[0].StartLayout -ceq $layout) {
        Write-Host "Taskbar pins already match: $($pinNames -join ', ')"
        return
    }

    $encodedLayout = [Net.WebUtility]::HtmlEncode($layout)
    if ($instances.Count -eq 0) {
        New-CimInstance -Namespace $namespace -ClassName $className -Property @{
            ParentID = $parentID
            InstanceID = $instanceID
            StartLayout = $encodedLayout
        } -ErrorAction Stop | Out-Null
    } else {
        Set-CimInstance -CimInstance $instances[0] -Property @{ StartLayout = $encodedLayout } `
            -ErrorAction Stop | Out-Null
    }
    $verifiedInstances = @(Get-CimInstance -Namespace $namespace -ClassName $className -ErrorAction Stop |
        Where-Object { [string]$_.ParentID -ceq $parentID -and [string]$_.InstanceID -ceq $instanceID })
    if ($verifiedInstances.Count -ne 1 -or [string]$verifiedInstances[0].StartLayout -cne $layout) {
        throw 'Taskbar policy read-back did not match the canonical decoded layout.'
    }
    Restart-ProvisioningExplorerShell -Role 'taskbar policy change'
    Write-Host "Taskbar pins applied: $($pinNames -join ', ')"
}

function Initialize-ProvisioningAudioEndpointType {
    if ($null -ne ('HerdrSandbox.AudioPolicy' -as [type])) { return }

    $source = @'
using System;
using System.Runtime.InteropServices;

namespace HerdrSandbox
{
    [ComImport]
    [Guid("BCDE0395-E52F-467C-8E3D-C4579291692E")]
    internal class MMDeviceEnumeratorComObject
    {
    }

    [ComImport]
    [Guid("A95664D2-9614-4F35-A746-DE8DB63617E6")]
    [InterfaceType(ComInterfaceType.InterfaceIsIUnknown)]
    internal interface IMMDeviceEnumerator
    {
        [PreserveSig] int EnumAudioEndpoints(int dataFlow, uint stateMask, out IntPtr devices);
        [PreserveSig] int GetDefaultAudioEndpoint(int dataFlow, int role, out IMMDevice endpoint);
        [PreserveSig] int GetDevice([MarshalAs(UnmanagedType.LPWStr)] string id, out IMMDevice device);
        [PreserveSig] int RegisterEndpointNotificationCallback(IntPtr client);
        [PreserveSig] int UnregisterEndpointNotificationCallback(IntPtr client);
    }

    [ComImport]
    [Guid("D666063F-1587-4E43-81F1-B948E807363F")]
    [InterfaceType(ComInterfaceType.InterfaceIsIUnknown)]
    internal interface IMMDevice
    {
        [PreserveSig] int Activate(ref Guid interfaceId, uint classContext, IntPtr activationParameters,
            [MarshalAs(UnmanagedType.IUnknown)] out object instance);
        [PreserveSig] int OpenPropertyStore(uint storageAccess, out IntPtr properties);
        [PreserveSig] int GetId(out IntPtr id);
        [PreserveSig] int GetState(out uint state);
    }

    [ComImport]
    [Guid("5CDF2C82-841E-4546-9722-0CF74078229A")]
    [InterfaceType(ComInterfaceType.InterfaceIsIUnknown)]
    internal interface IAudioEndpointVolume
    {
        [PreserveSig] int RegisterControlChangeNotify(IntPtr notify);
        [PreserveSig] int UnregisterControlChangeNotify(IntPtr notify);
        [PreserveSig] int GetChannelCount(out uint channelCount);
        [PreserveSig] int SetMasterVolumeLevel(float levelDB, IntPtr eventContext);
        [PreserveSig] int SetMasterVolumeLevelScalar(float level, IntPtr eventContext);
        [PreserveSig] int GetMasterVolumeLevel(out float levelDB);
        [PreserveSig] int GetMasterVolumeLevelScalar(out float level);
        [PreserveSig] int SetChannelVolumeLevel(uint channel, float levelDB, IntPtr eventContext);
        [PreserveSig] int SetChannelVolumeLevelScalar(uint channel, float level, IntPtr eventContext);
        [PreserveSig] int GetChannelVolumeLevel(uint channel, out float levelDB);
        [PreserveSig] int GetChannelVolumeLevelScalar(uint channel, out float level);
        [PreserveSig] int SetMute([MarshalAs(UnmanagedType.Bool)] bool muted, IntPtr eventContext);
        [PreserveSig] int GetMute([MarshalAs(UnmanagedType.Bool)] out bool muted);
        [PreserveSig] int GetVolumeStepInfo(out uint step, out uint stepCount);
        [PreserveSig] int VolumeStepUp(IntPtr eventContext);
        [PreserveSig] int VolumeStepDown(IntPtr eventContext);
        [PreserveSig] int QueryHardwareSupport(out uint hardwareSupportMask);
        [PreserveSig] int GetVolumeRange(out float minimumDB, out float maximumDB, out float incrementDB);
    }

    public static class AudioPolicy
    {
        private const int ENotFound = unchecked((int)0x80070490);
        private const uint ClassContextAll = 0x17;

        public static bool SilenceDefaultRenderEndpoint()
        {
            IMMDeviceEnumerator enumerator = null;
            IMMDevice device = null;
            IAudioEndpointVolume endpointVolume = null;
            try
            {
                enumerator = (IMMDeviceEnumerator)(object)new MMDeviceEnumeratorComObject();
                int result = enumerator.GetDefaultAudioEndpoint(0, 1, out device);
                if (result == ENotFound)
                {
                    return false;
                }
                Marshal.ThrowExceptionForHR(result);
                if (device == null)
                {
                    throw new InvalidOperationException("Default render endpoint resolved to null.");
                }

                Guid endpointVolumeId = new Guid("5CDF2C82-841E-4546-9722-0CF74078229A");
                object instance;
                result = device.Activate(ref endpointVolumeId, ClassContextAll, IntPtr.Zero, out instance);
                Marshal.ThrowExceptionForHR(result);
                endpointVolume = instance as IAudioEndpointVolume;
                if (endpointVolume == null)
                {
                    throw new InvalidOperationException("Default render endpoint volume interface is unavailable.");
                }

                Marshal.ThrowExceptionForHR(endpointVolume.SetMute(true, IntPtr.Zero));
                Marshal.ThrowExceptionForHR(endpointVolume.SetMasterVolumeLevelScalar(0.0f, IntPtr.Zero));
                bool muted;
                float scalarVolume;
                Marshal.ThrowExceptionForHR(endpointVolume.GetMute(out muted));
                Marshal.ThrowExceptionForHR(endpointVolume.GetMasterVolumeLevelScalar(out scalarVolume));
                if (!muted || scalarVolume != 0.0f)
                {
                    throw new InvalidOperationException("Default render endpoint mute/volume read-back did not match.");
                }
                return true;
            }
            finally
            {
                Release(endpointVolume);
                Release(device);
                Release(enumerator);
            }
        }

        private static void Release(object value)
        {
            if (value != null && Marshal.IsComObject(value))
            {
                Marshal.FinalReleaseComObject(value);
            }
        }
    }
}
'@
    Add-Type -TypeDefinition $source -Language CSharp -ErrorAction Stop
    if ($null -eq ('HerdrSandbox.AudioPolicy' -as [type])) {
        throw 'Core Audio endpoint type did not load.'
    }
}

function Disable-ProvisioningAudioPlayback {
    $failures = New-Object 'Collections.Generic.List[string]'
    $schemeChanged = $false
    try {
        $schemeChanged = Set-ProvisioningRegistryValue -Path 'HKCU:\AppEvents\Schemes' `
            -Name '' -Value '.None' -PropertyType 'String'
    } catch {
        [void]$failures.Add('No Sounds scheme: ' +
            (Get-ProvisioningBoundedDiagnosticText -Text $_.Exception.Message -MaximumBytes 1024))
    }

    $endpointFound = $false
    try {
        Initialize-ProvisioningAudioEndpointType
        $endpointFound = [bool][HerdrSandbox.AudioPolicy]::SilenceDefaultRenderEndpoint()
    } catch {
        [void]$failures.Add('default render endpoint: ' +
            (Get-ProvisioningBoundedDiagnosticText -Text $_.Exception.Message -MaximumBytes 1024))
    }

    $serviceNames = @('Audiosrv', 'AudioEndpointBuilder')
    foreach ($serviceName in $serviceNames) {
        try {
            Set-Service -Name $serviceName -StartupType Disabled -ErrorAction Stop
        } catch {
            [void]$failures.Add("disable $serviceName startup: " +
                (Get-ProvisioningBoundedDiagnosticText -Text $_.Exception.Message -MaximumBytes 1024))
        }
    }
    foreach ($serviceName in $serviceNames) {
        $controller = $null
        try {
            $controller = New-Object System.ServiceProcess.ServiceController -ArgumentList @($serviceName)
            $controller.Refresh()
            if ($controller.Status -ne [System.ServiceProcess.ServiceControllerStatus]::Stopped) {
                if (-not $controller.CanStop) {
                    throw "$serviceName cannot be stopped."
                }
                $controller.Stop()
                $controller.WaitForStatus(
                    [System.ServiceProcess.ServiceControllerStatus]::Stopped,
                    [TimeSpan]::FromSeconds(15))
                $controller.Refresh()
            }
            if ($controller.Status -ne [System.ServiceProcess.ServiceControllerStatus]::Stopped) {
                throw "$serviceName status is $($controller.Status), expected Stopped."
            }
        } catch {
            [void]$failures.Add("stop ${serviceName}: " +
                (Get-ProvisioningBoundedDiagnosticText -Text $_.Exception.Message -MaximumBytes 1024))
        } finally {
            if ($null -ne $controller) { $controller.Dispose() }
        }
    }
    foreach ($serviceName in $serviceNames) {
        try {
            $service = Get-Service -Name $serviceName -ErrorAction Stop
            $wmiServices = @(Get-CimInstance -ClassName Win32_Service -Filter "Name='$serviceName'" -ErrorAction Stop)
            if ($wmiServices.Count -ne 1 -or
                [string]$service.Status -cne 'Stopped' -or
                [string]$service.StartType -cne 'Disabled' -or
                [string]$wmiServices[0].State -cne 'Stopped' -or
                [bool]$wmiServices[0].Started -or
                [string]$wmiServices[0].StartMode -cne 'Disabled') {
                throw "$serviceName service read-back did not match Disabled/Stopped."
            }
        } catch {
            [void]$failures.Add("verify ${serviceName}: " +
                (Get-ProvisioningBoundedDiagnosticText -Text $_.Exception.Message -MaximumBytes 1024))
        }
    }

    if ($failures.Count -ne 0) {
        throw ('Audio disable policy failed: ' + ($failures -join '; '))
    }
    if ($endpointFound) {
        Write-Host 'Audio playback disabled: default render endpoint muted at volume 0; services stopped.'
    } else {
        Write-Host 'Audio playback disabled: no default render endpoint was present; services stopped.'
    }
    return [bool]$schemeChanged
}

$provisioningStopwatch = [Diagnostics.Stopwatch]::StartNew()
if ($Phase -eq 'Registry') {
$registryStateChanged = $false
if (-not $AudioEnabled) {
    Write-Output 'Disabling Sandbox audio playback...'
    if (Disable-ProvisioningAudioPlayback) {
        $registryStateChanged = $true
    }
} else {
    Write-Output 'Sandbox audio playback enabled by config; microphone input remains disabled.'
}
Write-Output 'Applying machine and selected per-user policies for Microsoft Edge...'
# Policy values derived from Just the Browser, MIT License, copyright 2026 Corbin Davenport.
# Source: https://github.com/corbindavenport/just-the-browser/tree/d80167f949947eed45b9b19b13e233f875ae9a6a/edge
$edgeMachinePolicyPath = 'HKLM:\SOFTWARE\Policies\Microsoft\Edge'
$edgeUserPolicyPath = 'HKCU:\SOFTWARE\Policies\Microsoft\Edge'
$edgeMachineStringPolicies = @{
    NewTabPageSearchBox = 'redirect'
}
$edgeMachineDwordPolicies = @{
    HideFirstRunExperience                       = 1
    GenAILocalFoundationalModelSettings          = 1
    NewTabPageHideDefaultTopSites                = 1
    AutoImportAtFirstRun                         = 4
    ShowPDFDefaultRecommendationsEnabled         = 0
    SpotlightExperiencesAndRecommendationsEnabled = 0
    WebToBrowserSignInEnabled                    = 0
    StartupBoostEnabled                          = 0
    NewTabPageBingChatEnabled                    = 0
    NewTabPageContentEnabled                     = 0
    AIGenThemesEnabled                           = 0
    ImportOnEachLaunch                           = 0
    BuiltInAIAPIsEnabled                         = 0
    BuiltInDnsClientEnabled                      = 0
    ComposeInlineEnabled                         = 0
    CopilotPageContext                           = 0
    DefaultBrowserSettingEnabled                 = 0
    DefaultBrowserSettingsCampaignEnabled        = 0
    DiagnosticData                               = 0
    Microsoft365CopilotChatIconEnabled           = 0
    ShowAcrobatSubscriptionButton                = 0
    ShowMicrosoftRewards                         = 0
    ShowRecommendationsEnabled                   = 0
    TabServicesEnabled                           = 0
    TextPredictionEnabled                        = 0
    VisualSearchEnabled                          = 0
    EdgeHistoryAISearchEnabled                   = 0
    CopilotAddressBarSuggestionsEnabled          = 0
    AllowBrowsingWithCopilot                     = 0
    SearchbarAllowed                             = 0
    PinningWizardAllowed                         = 0
}
$edgeUserDwordPolicies = [ordered]@{
    AddressBarMicrosoftSearchInBingProviderEnabled = 0
    AlternateErrorPagesEnabled                     = 0
    AutofillAddressEnabled                          = 0
    AutofillCreditCardEnabled                       = 0
    BrowserSignin                                   = 0
    ConfigureDoNotTrack                             = 1
    EdgeShoppingAssistantEnabled                    = 0
    HubsSidebarEnabled                              = 0
    LocalProvidersEnabled                           = 0
    MetricsReportingEnabled                         = 0
    MicrosoftEditorProofingEnabled                  = 0
    NetworkPredictionOptions                        = 2
    PasswordManagerEnabled                          = 0
    PaymentMethodQueryEnabled                       = 0
    PersonalizationReportingEnabled                 = 0
    ResolveNavigationErrorsUseWebService            = 0
    SearchSuggestEnabled                            = 0
    SendSiteInfoToImproveServices                   = 0
    SiteSafetyServicesEnabled                       = 0
    SmartScreenEnabled                              = 0
    TyposquattingCheckerEnabled                     = 0
    UserFeedbackAllowed                             = 0
    WebWidgetAllowed                                = 0
}
foreach ($entry in $edgeMachineStringPolicies.GetEnumerator()) {
    if (Set-ProvisioningRegistryValue -Path $edgeMachinePolicyPath -Name $entry.Key -Value $entry.Value `
            -PropertyType 'String') {
        $registryStateChanged = $true
    }
}
foreach ($entry in $edgeMachineDwordPolicies.GetEnumerator()) {
    if (Set-ProvisioningRegistryValue -Path $edgeMachinePolicyPath -Name $entry.Key -Value $entry.Value `
            -PropertyType 'DWord') {
        $registryStateChanged = $true
    }
}
foreach ($path in @($edgeMachinePolicyPath, $edgeUserPolicyPath)) {
    foreach ($entry in $edgeUserDwordPolicies.GetEnumerator()) {
        if (Set-ProvisioningRegistryValue -Path $path -Name $entry.Key -Value $entry.Value `
                -PropertyType 'DWord') {
            $registryStateChanged = $true
        }
    }
}

Write-Output 'Applying reviewed Windows UI and privacy settings...'
$privacyRegistryGroups = @(
    [ordered]@{ Path = 'HKCU:\Control Panel\International\User Profile'; Values = [ordered]@{
        HttpAcceptLanguageOptOut = 1
    } },
    [ordered]@{ Path = 'HKCU:\Software\Microsoft\Clipboard'; Values = [ordered]@{
        EnableClipboardHistory = 0
    } },
    [ordered]@{ Path = 'HKCU:\Software\Microsoft\Input\TIPC'; Values = [ordered]@{ Enabled = 0 } },
    [ordered]@{ Path = 'HKCU:\Software\Microsoft\InputPersonalization'; Values = [ordered]@{
        RestrictImplicitInkCollection = 1; RestrictImplicitTextCollection = 1
    } },
    [ordered]@{ Path = 'HKCU:\Software\Microsoft\InputPersonalization\TrainedDataStore'; Values = [ordered]@{
        HarvestContacts = 0
    } },
    [ordered]@{ Path = 'HKCU:\Software\Microsoft\MediaPlayer\Preferences'; Values = [ordered]@{ UsageTracking = 0 } },
    [ordered]@{ Path = 'HKCU:\Software\Microsoft\Personalization\Settings'; Values = [ordered]@{
        AcceptedPrivacyPolicy = 0
    } },
    [ordered]@{ Path = 'HKCU:\Software\Microsoft\Siuf\Rules'; Values = [ordered]@{
        NumberOfSIUFInPeriod = 0; PeriodInNanoSeconds = 0
    } },
    [ordered]@{ Path = 'HKCU:\Software\Microsoft\Speech_OneCore\Settings\VoiceActivation\UserPreferenceForAllApps'; Values = [ordered]@{
        AgentActivationEnabled = 0; AgentActivationOnLockScreenEnabled = 0
    } },
    [ordered]@{ Path = 'HKCU:\Software\Microsoft\TabletTip\1.7'; Values = [ordered]@{ EnableTextPrediction = 0 } },
    [ordered]@{ Path = 'HKCU:\Software\Microsoft\Windows\CurrentVersion\AdvertisingInfo'; Values = [ordered]@{ Enabled = 0 } },
    [ordered]@{ Path = 'HKLM:\SOFTWARE\Microsoft\Windows\CurrentVersion\AdvertisingInfo'; Values = [ordered]@{ Enabled = 0 } },
    [ordered]@{ Path = 'HKCU:\Software\Microsoft\Windows\CurrentVersion\AppHost'; Values = [ordered]@{
        EnableWebContentEvaluation = 0
    } },
    [ordered]@{ Path = 'HKCU:\Software\Microsoft\Windows\CurrentVersion\ContentDeliveryManager'; Values = [ordered]@{
        RotatingLockScreenEnabled = 0; RotatingLockScreenOverlayEnabled = 0; SilentInstalledAppsEnabled = 0
        SoftLandingEnabled = 0; 'SubscribedContent-338387Enabled' = 0; 'SubscribedContent-338388Enabled' = 0
        'SubscribedContent-338389Enabled' = 0; 'SubscribedContent-338393Enabled' = 0
        'SubscribedContent-353694Enabled' = 0; 'SubscribedContent-353696Enabled' = 0
        'SubscribedContent-353698Enabled' = 0; SystemPaneSuggestionsEnabled = 0
    } },
    [ordered]@{ Path = 'HKCU:\Software\Microsoft\Windows\CurrentVersion\Privacy'; Values = [ordered]@{
        TailoredExperiencesWithDiagnosticDataEnabled = 0
    } },
    [ordered]@{ Path = 'HKCU:\Software\Microsoft\Windows\CurrentVersion\UserProfileEngagement'; Values = [ordered]@{
        ScoobeSystemSettingEnabled = 0
    } },
    [ordered]@{ Path = 'HKCU:\Software\Microsoft\Windows\CurrentVersion\Explorer'; Values = [ordered]@{
        ShowFrequent = 0
    } },
    [ordered]@{ Path = 'HKCU:\Software\Microsoft\Windows\CurrentVersion\Explorer\Advanced'; Values = [ordered]@{
        Hidden = 1; HideFileExt = 0; HideMergeConflicts = 0; LaunchTo = 1
        NavPaneExpandToCurrentFolder = 1; NavPaneShowAllFolders = 1; ShowEncryptCompressedColor = 1
        ShowSuperHidden = 1; ShowSyncProviderNotifications = 0; ShowTaskViewButton = 0
        Start_TrackDocs = 0; Start_TrackProgs = 0
    } },
    [ordered]@{ Path = 'HKCU:\Software\Microsoft\Windows\CurrentVersion\Explorer\Advanced\People'; Values = [ordered]@{
        PeopleBand = 0
    } },
    [ordered]@{ Path = 'HKCU:\Software\Microsoft\Windows\CurrentVersion\Explorer\CabinetState'; Values = [ordered]@{
        FullPath = 1
    } },
    [ordered]@{ Path = 'HKCU:\Software\Policies\Microsoft\Windows\Explorer'; Values = [ordered]@{
        DisableSearchBoxSuggestions = 1
    } },
    [ordered]@{ Path = 'HKCU:\Software\Microsoft\Windows\CurrentVersion\Search'; Values = [ordered]@{
        SearchboxTaskbarMode = 0; TraySearchBoxVisible = 0; TraySearchBoxVisibleOnAnyMonitor = 0
    } },
    [ordered]@{ Path = 'HKCU:\Software\Microsoft\Windows\CurrentVersion\SearchSettings'; Values = [ordered]@{
        IsAADCloudSearchEnabled = 0; IsMSACloudSearchEnabled = 0
    } },
    [ordered]@{ Path = 'HKCU:\Software\Microsoft\Windows\CurrentVersion\Windows Search'; Values = [ordered]@{
        CortanaConsent = 0
    } },
    [ordered]@{ Path = 'HKCU:\Software\Microsoft\Windows\CurrentVersion\DeliveryOptimization'; Values = [ordered]@{
        SystemSettingsDownloadMode = 0
    } },
    [ordered]@{ Path = 'HKLM:\SOFTWARE\Microsoft\Windows\CurrentVersion\DeliveryOptimization\Config'; Values = [ordered]@{
        DODownloadMode = 0
    } },
    [ordered]@{ Path = 'HKCU:\Software\Microsoft\Windows\CurrentVersion\Notifications\Settings'; Values = [ordered]@{
        NOC_GLOBAL_SETTING_ALLOW_TOASTS_ABOVE_LOCK = 0
    } },
    [ordered]@{ Path = 'HKCU:\Software\Microsoft\Windows\CurrentVersion\SettingSync'; Values = [ordered]@{
        SyncPolicy = 5
    } },
    [ordered]@{ Path = 'HKCU:\Software\Microsoft\Windows\CurrentVersion\SettingSync\Groups\Accessibility'; Values = [ordered]@{ Enabled = 0 } },
    [ordered]@{ Path = 'HKCU:\Software\Microsoft\Windows\CurrentVersion\SettingSync\Groups\BrowserSettings'; Values = [ordered]@{ Enabled = 0 } },
    [ordered]@{ Path = 'HKCU:\Software\Microsoft\Windows\CurrentVersion\SettingSync\Groups\Credentials'; Values = [ordered]@{ Enabled = 0 } },
    [ordered]@{ Path = 'HKCU:\Software\Microsoft\Windows\CurrentVersion\SettingSync\Groups\Language'; Values = [ordered]@{ Enabled = 0 } },
    [ordered]@{ Path = 'HKCU:\Software\Microsoft\Windows\CurrentVersion\SettingSync\Groups\Personalization'; Values = [ordered]@{ Enabled = 0 } },
    [ordered]@{ Path = 'HKCU:\Software\Microsoft\Windows\CurrentVersion\SettingSync\Groups\Windows'; Values = [ordered]@{ Enabled = 0 } },
    [ordered]@{ Path = 'HKCU:\Software\Microsoft\Windows\CurrentVersion\StorageSense\Parameters\StoragePolicy'; Values = [ordered]@{
        '01' = 0
    } },
    [ordered]@{ Path = 'HKLM:\SOFTWARE\Microsoft\OneDrive'; Values = [ordered]@{
        PreventNetworkTrafficPreUserSignIn = 1
    } },
    [ordered]@{ Path = 'HKLM:\SOFTWARE\Microsoft\PCHC'; Values = [ordered]@{
        PreviousUninstall = 1
    } },
    [ordered]@{ Path = 'HKLM:\SOFTWARE\Microsoft\PolicyManager\current\device\Bluetooth'; Values = [ordered]@{
        AllowAdvertising = 0
    } },
    [ordered]@{ Path = 'HKLM:\SOFTWARE\Microsoft\PolicyManager\current\device\Browser'; Values = [ordered]@{
        AllowAddressBarDropdown = 0
    } },
    [ordered]@{ Path = 'HKLM:\SOFTWARE\Microsoft\PolicyManager\current\device\System'; Values = [ordered]@{
        AllowExperimentation = 0
    } },
    [ordered]@{ Path = 'HKLM:\SOFTWARE\Microsoft\Speech_OneCore\Preferences'; Values = [ordered]@{
        ModelDownloadAllowed = 0
    } },
    [ordered]@{ Path = 'HKLM:\SOFTWARE\Microsoft\Windows\CurrentVersion\Device Metadata'; Values = [ordered]@{
        PreventDeviceMetadataFromNetwork = 1
    } },
    [ordered]@{ Path = 'HKLM:\SOFTWARE\Microsoft\Windows\CurrentVersion\Policies\DataCollection'; Values = [ordered]@{
        AllowTelemetry = 0
    } },
    [ordered]@{ Path = 'HKLM:\SOFTWARE\Microsoft\Windows\CurrentVersion\Policies\Ext\CLSID'; Values = [ordered]@{
        '{1FD49718-1D00-4B19-AF5F-070AF6D5D54C}' = 0
    } },
    [ordered]@{ Path = 'HKLM:\SOFTWARE\Microsoft\Windows\CurrentVersion\WindowsStore\WindowsUpdate'; Values = [ordered]@{
        AutoDownload = 2
    } },
    [ordered]@{ Path = 'HKLM:\SOFTWARE\Microsoft\Windows\CurrentVersion\WindowsUpdate\Services\7971f918-a847-4430-9279-4a52d1efe18d'; Values = [ordered]@{
        RegisteredWithAU = 0
    } },
    [ordered]@{ Path = 'HKLM:\SOFTWARE\Microsoft\Windows\Windows Error Reporting'; Values = [ordered]@{
        Disabled = 1
    } },
    [ordered]@{ Path = 'HKLM:\SOFTWARE\Policies\Microsoft\Biometrics'; Values = [ordered]@{ Enabled = 0 } },
    [ordered]@{ Path = 'HKLM:\SOFTWARE\Policies\Microsoft\MicrosoftEdge\Main'; Values = [ordered]@{
        AllowPrelaunch = 0
    } },
    [ordered]@{ Path = 'HKLM:\SOFTWARE\Policies\Microsoft\MicrosoftEdge\TabPreloader'; Values = [ordered]@{
        AllowTabPreloading = 0
    } },
    [ordered]@{ Path = 'HKLM:\SOFTWARE\Policies\Microsoft\MRT'; Values = [ordered]@{
        DontReportInfectionInformation = 1
    } },
    [ordered]@{ Path = 'HKLM:\SOFTWARE\Policies\Microsoft\Windows\AppPrivacy'; Values = [ordered]@{
        LetAppsAccessLocation = 2
    } },
    [ordered]@{ Path = 'HKLM:\SOFTWARE\Policies\Microsoft\Windows Defender'; Values = [ordered]@{
        DisableAntiSpyware = 1
    } },
    [ordered]@{ Path = 'HKLM:\SOFTWARE\Policies\Microsoft\Windows Defender\Spynet'; Values = [ordered]@{
        SpyNetReporting = 0; SubmitSamplesConsent = 2
    } },
    [ordered]@{ Path = 'HKLM:\SOFTWARE\Policies\Microsoft\Windows NT\CurrentVersion\Software Protection Platform'; Values = [ordered]@{
        NoGenTicket = 1
    } },
    [ordered]@{ Path = 'HKLM:\SOFTWARE\Policies\Microsoft\WMDRM'; Values = [ordered]@{
        DisableOnline = 1
    } },
    [ordered]@{ Path = 'HKLM:\SOFTWARE\Policies\Microsoft\Windows\LocationAndSensors'; Values = [ordered]@{
        DisableLocation = 1; DisableLocationScripting = 1; DisableSensors = 1; DisableWindowsLocationProvider = 1
    } },
    [ordered]@{ Path = 'HKLM:\SOFTWARE\Policies\Microsoft\Dsh'; Values = [ordered]@{
        AllowNewsAndInterests = 0
    } },
    [ordered]@{ Path = 'HKLM:\SOFTWARE\Policies\Microsoft\Windows\System'; Values = [ordered]@{
        PublishUserActivities = 0; EnableActivityFeed = 0; UploadUserActivities = 0
        AllowClipboardHistory = 0; AllowCrossDeviceClipboard = 0; EnableMmx = 0
    } },
    [ordered]@{ Path = 'HKLM:\SOFTWARE\Policies\Microsoft\Windows\Windows Search'; Values = [ordered]@{
        AllowSearchToUseLocation = 0; AllowCloudSearch = 0; AllowCortanaAboveLock = 0
        ConnectedSearchUseWeb = 0; DisableWebSearch = 1; EnableDynamicContentInWSB = 0
    } },
    [ordered]@{ Path = 'HKLM:\SOFTWARE\Policies\Microsoft\Windows\DeliveryOptimization'; Values = [ordered]@{
        DODownloadMode = 0
    } },
    [ordered]@{ Path = 'HKLM:\SOFTWARE\Policies\Microsoft\Windows\WindowsUpdate'; Values = [ordered]@{
        DeferUpdatePeriod = 0; DeferUpgrade = 1; DeferUpgradePeriod = 1
        ExcludeWUDriversInQualityUpdate = 1
    } },
    [ordered]@{ Path = 'HKLM:\SOFTWARE\Policies\Microsoft\Windows\WindowsUpdate\AU'; Values = [ordered]@{
        NoAutoUpdate = 1
    } },
    [ordered]@{ Path = 'HKLM:\SOFTWARE\Policies\Microsoft\Windows\Windows Feeds'; Values = [ordered]@{
        EnableFeeds = 0
    } },
    [ordered]@{ Path = 'HKLM:\SOFTWARE\Policies\Microsoft\Windows\DataCollection'; Values = [ordered]@{
        AllowTelemetry = 0; DisableOneSettingsDownloads = 1; DoNotShowFeedbackNotifications = 1
        LimitDiagnosticLogCollection = 1
    } },
    [ordered]@{ Path = 'HKLM:\SOFTWARE\Policies\Microsoft\InputPersonalization'; Values = [ordered]@{
        AllowInputPersonalization = 0
    } },
    [ordered]@{ Path = 'HKLM:\SOFTWARE\Policies\Microsoft\Speech'; Values = [ordered]@{
        AllowSpeechModelUpdate = 0
    } },
    [ordered]@{ Path = 'HKLM:\SOFTWARE\Policies\Microsoft\Windows NT\Terminal Services'; Values = [ordered]@{
        fAllowToGetHelp = 0
    } },
    [ordered]@{ Path = 'HKLM:\SOFTWARE\Policies\Microsoft\Windows\AppCompat'; Values = [ordered]@{
        AITEnable = 0; DisableInventory = 1; DisableUAR = 1
    } },
    [ordered]@{ Path = 'HKLM:\SOFTWARE\Policies\Microsoft\Windows\CredUI'; Values = [ordered]@{
        DisablePasswordReveal = 1
    } },
    [ordered]@{ Path = 'HKLM:\SOFTWARE\Policies\Microsoft\Windows\HandwritingErrorReports'; Values = [ordered]@{
        PreventHandwritingErrorReports = 1
    } },
    [ordered]@{ Path = 'HKLM:\SOFTWARE\Policies\Microsoft\Windows\TabletPC'; Values = [ordered]@{
        PreventHandwritingDataSharing = 1
    } },
    [ordered]@{ Path = 'HKLM:\SOFTWARE\Policies\Microsoft\Windows\Maps'; Values = [ordered]@{
        AllowUntriggeredNetworkTrafficOnSettingsPage = 0; AutoDownloadAndUpdateMapData = 0
    } },
    [ordered]@{ Path = 'HKLM:\SOFTWARE\Policies\Microsoft\Windows\Messaging'; Values = [ordered]@{
        AllowMessageSync = 0
    } },
    [ordered]@{ Path = 'HKLM:\SOFTWARE\Policies\Microsoft\Windows\OneDrive'; Values = [ordered]@{
        DisableFileSyncNGSC = 1
    } },
    [ordered]@{ Path = 'HKLM:\SOFTWARE\Policies\Microsoft\Windows\Personalization'; Values = [ordered]@{
        NoLockScreenCamera = 1
    } },
    [ordered]@{ Path = 'HKCU:\Software\Classes\Local Settings\Software\Microsoft\Windows\CurrentVersion\AppContainer\Storage\microsoft.microsoftedge_8wekyb3d8bbwe\MicrosoftEdge\FlipAhead'; Values = [ordered]@{
        FPEnabled = 0
    } },
    [ordered]@{ Path = 'HKCU:\Software\Classes\Local Settings\Software\Microsoft\Windows\CurrentVersion\AppContainer\Storage\microsoft.microsoftedge_8wekyb3d8bbwe\MicrosoftEdge\Main'; Values = [ordered]@{
        DoNotTrack = 1; OptimizeWindowsSearchResultsForScreenReaders = 0; ShowSearchSuggestionsGlobal = 0
        'Use FormSuggest' = 'no'
    } },
    [ordered]@{ Path = 'HKCU:\Software\Classes\Local Settings\Software\Microsoft\Windows\CurrentVersion\AppContainer\Storage\microsoft.microsoftedge_8wekyb3d8bbwe\MicrosoftEdge\PhishingFilter'; Values = [ordered]@{
        EnabledV9 = 0
    } },
    [ordered]@{ Path = 'HKCU:\Software\Classes\Local Settings\Software\Microsoft\Windows\CurrentVersion\AppContainer\Storage\microsoft.microsoftedge_8wekyb3d8bbwe\MicrosoftEdge\Privacy'; Values = [ordered]@{
        EnableEncryptedMediaExtensions = 0
    } },
    [ordered]@{ Path = 'HKCU:\Software\Classes\Local Settings\Software\Microsoft\Windows\CurrentVersion\AppContainer\Storage\microsoft.microsoftedge_8wekyb3d8bbwe\MicrosoftEdge\ServiceUI'; Values = [ordered]@{
        EnableCortana = 0
    } },
    [ordered]@{ Path = 'HKCU:\Software\Classes\Local Settings\Software\Microsoft\Windows\CurrentVersion\AppContainer\Storage\microsoft.microsoftedge_8wekyb3d8bbwe\MicrosoftEdge\ServiceUI\ShowSearchHistory'; Values = [ordered]@{
        '' = 0
    } },
    [ordered]@{ Path = 'HKLM:\SYSTEM\CurrentControlSet\Services\lfsvc\Service\Configuration'; Values = [ordered]@{
        Status = 0
    } },
    [ordered]@{ Path = 'HKLM:\SYSTEM\CurrentControlSet\Services\NlaSvc\Parameters\Internet'; Values = [ordered]@{
        EnableActiveProbing = 0
    } }
)
$privacyConsentCapabilities = @(
    'activity', 'appDiagnostics', 'appointments', 'bluetoothSync', 'broadFileSystemAccess', 'cellularData',
    'chat', 'contacts', 'documentsLibrary', 'email', 'gazeInput', 'location', 'microphone', 'phoneCall',
    'phoneCallHistory', 'picturesLibrary', 'radios', 'userAccountInformation', 'userDataTasks',
    'userNotificationListener', 'videosLibrary', 'webcam'
)
foreach ($scope in @(
    'HKCU:\Software\Microsoft\Windows\CurrentVersion\CapabilityAccessManager\ConsentStore',
    'HKLM:\SOFTWARE\Microsoft\Windows\CurrentVersion\CapabilityAccessManager\ConsentStore'
)) {
    foreach ($capability in $privacyConsentCapabilities) {
        $privacyRegistryGroups += [ordered]@{
            Path = $scope + '\' + $capability
            Values = [ordered]@{ Value = 'Deny' }
        }
    }
}
foreach ($group in $privacyRegistryGroups) {
    foreach ($entry in $group.Values.GetEnumerator()) {
        if ($entry.Value -is [string]) {
            $propertyType = 'String'
            $propertyValue = [string]$entry.Value
        } elseif ($entry.Value -is [int]) {
            $propertyType = 'DWord'
            $propertyValue = [int]$entry.Value
        } else {
            throw "Unsupported privacy registry value type: $($group.Path)::$($entry.Key) ($($entry.Value.GetType().FullName))"
        }
        try {
            if (Set-ProvisioningRegistryValue -Path $group.Path -Name ([string]$entry.Key) `
                    -Value $propertyValue -PropertyType $propertyType) {
                $registryStateChanged = $true
            }
        } catch {
            throw "Privacy registry write failed: $($group.Path)::$($entry.Key): $($_.Exception.Message)"
        }
    }
}

if ($registryStateChanged) {
    Restart-ProvisioningExplorerShell -Role 'registry changes'
} else {
    Write-Host 'Registry state already matches; Explorer restart skipped.'
}
$provisioningStopwatch.Stop()
Write-ProvisioningTiming -Role 'early registry and Explorer customization' -Seconds $provisioningStopwatch.Elapsed.TotalSeconds
return
}

Write-Output 'Installing PowerShell 7...'
Install-ProvisioningWinGetPackage -Role 'PowerShell 7' -Id 'Microsoft.PowerShell' `
    -Version (Get-ProvisioningPackageVersion -Id 'Microsoft.PowerShell') `
    -InstallerType 'msix' -Adapter 'MSIX' -ExecutableName 'pwsh.exe'
$powerShell7 = Get-ProvisioningPowerShell7Installation
$powerShellVersion = "PowerShell $($powerShell7.DisplayVersion)"
Write-Output "PowerShell 7 ready: $powerShellVersion"

if (Test-ProvisioningPackageEnabled -Id 'Starship.Starship') {
    Write-Output 'Installing Starship...'
    Install-ProvisioningWinGetPackage -Role 'Starship' -Id 'Starship.Starship' `
        -Version (Get-ProvisioningPackageVersion -Id 'Starship.Starship') `
        -InstallerType 'zip' -Adapter 'Portable' -ExecutableName 'starship.exe'
    $starshipVersion = Assert-ProvisioningCommand -Role 'Starship' -Name 'starship.exe' `
        -VersionArguments @('--version') -ExpectedPattern '^starship \d+\.\d+\.\d+'
    $powerShellProfilePath = [IO.Path]::GetFullPath((Join-Path $env:USERPROFILE 'Documents\PowerShell\profile.ps1'))
    $expectedProfileRoot = [IO.Path]::GetFullPath($env:USERPROFILE).TrimEnd('\') + '\'
    if (-not $powerShellProfilePath.StartsWith($expectedProfileRoot, [StringComparison]::OrdinalIgnoreCase) -or
        -not $powerShellProfilePath.EndsWith('\PowerShell\profile.ps1', [StringComparison]::OrdinalIgnoreCase)) {
        throw "PowerShell 7 all-host profile path is outside the expected guest user profile: $powerShellProfilePath"
    }
    $powerShellProfileDirectory = Split-Path -Parent $powerShellProfilePath
    New-Item -ItemType Directory -Path $powerShellProfileDirectory -Force | Out-Null
    $starshipInitialization = 'Invoke-Expression (&starship init powershell)' + [Environment]::NewLine
    if (-not (Test-Path -LiteralPath $powerShellProfilePath -PathType Leaf) -or
        [IO.File]::ReadAllText($powerShellProfilePath) -cne $starshipInitialization) {
        [IO.File]::WriteAllText($powerShellProfilePath, $starshipInitialization, (New-Object Text.UTF8Encoding($false)))
    }
    if ([IO.File]::ReadAllText($powerShellProfilePath) -cne $starshipInitialization) {
        throw 'PowerShell 7 Starship profile verification failed.'
    }
    Write-Output "Starship ready: $starshipVersion"
}

if (Test-ProvisioningPackageEnabled -Id 'junegunn.fzf') {
    Write-Output 'Installing fzf...'
    Install-ProvisioningWinGetPackage -Role 'fzf' -Id 'junegunn.fzf' `
        -Version (Get-ProvisioningPackageVersion -Id 'junegunn.fzf') `
        -InstallerType 'zip' -Adapter 'Portable' -ExecutableName 'fzf.exe'
    $fzfVersion = Assert-ProvisioningCommand -Role 'fzf' -Name 'fzf.exe' `
        -VersionArguments @('--version') -ExpectedPattern '^\d+\.\d+\.\d+'
    Write-Output "fzf ready: $fzfVersion"
}

if (Test-ProvisioningPackageEnabled -Id 'BurntSushi.ripgrep.MSVC') {
    Write-Output 'Installing ripgrep...'
    Install-ProvisioningWinGetPackage -Role 'ripgrep' -Id 'BurntSushi.ripgrep.MSVC' `
        -Version (Get-ProvisioningPackageVersion -Id 'BurntSushi.ripgrep.MSVC') `
        -InstallerType 'zip' -Adapter 'Portable' -ExecutableName 'rg.exe'
    $ripgrepVersion = Assert-ProvisioningCommand -Role 'ripgrep' -Name 'rg.exe' `
        -VersionArguments @('--version') -ExpectedPattern '^ripgrep \d+\.\d+\.\d+'
    Write-Output "ripgrep ready: $ripgrepVersion"
}

if (Test-ProvisioningPackageEnabled -Id 'Git.Git') {
    Write-Output 'Installing Git...'
    Install-ProvisioningWinGetPackage -Role 'Git' -Id 'Git.Git' `
        -Version (Get-ProvisioningPackageVersion -Id 'Git.Git') -InstallerType 'inno' `
        -Scope 'machine' -Adapter 'Inno' -ExecutableName 'git.exe' -DeferCommandReadiness `
        -RequireAuthenticodeSignature
}

if (Test-ProvisioningPackageEnabled -Id 'GitHub.cli') {
    Write-Output 'Installing GitHub CLI...'
    Install-ProvisioningWinGetPackage -Role 'GitHub CLI' -Id 'GitHub.cli' `
        -Version (Get-ProvisioningPackageVersion -Id 'GitHub.cli') `
        -InstallerType 'wix' -Scope 'machine' -Adapter 'MSI' -ExecutableName 'gh.exe'
    $githubCLIVersion = Assert-ProvisioningCommand -Role 'GitHub CLI' -Name 'gh.exe' `
        -VersionArguments @('--version') `
        -ExpectedPattern '^gh version (?<v>\d+\.\d+\.\d+(?:-[\w.]+)?) \(\d{4}-\d{2}-\d{2}\)\r?\nhttps://github\.com/cli/cli/releases/tag/v\k<v>$'
    Write-Output "GitHub CLI ready: $githubCLIVersion"
}

if (Test-ProvisioningPackageEnabled -Id 'Tailscale.Tailscale') {
    Write-Output 'Installing Tailscale...'
    Install-ProvisioningWinGetPackage -Role 'Tailscale' -Id 'Tailscale.Tailscale' `
        -Version (Get-ProvisioningPackageVersion -Id 'Tailscale.Tailscale') `
        -InstallerType 'wix' -Scope 'machine' -Adapter 'MSI' -ExecutableName 'tailscale.exe' `
        -InstallerArguments @('TS_NOLAUNCH=1')
    $tailscaleVersion = Assert-ProvisioningCommand -Role 'Tailscale' -Name 'tailscale.exe' `
        -VersionArguments @('version') -ExpectedPattern '^\d+\.\d+\.\d+(?:-[\w.]+)?'
    Write-Output "Tailscale ready: $tailscaleVersion"
}

if (Test-ProvisioningPackageEnabled -Id 'SST.opencode') {
    Write-Output 'Installing OpenCode...'
    Install-ProvisioningWinGetPackage -Role 'OpenCode' -Id 'SST.opencode' `
        -Version (Get-ProvisioningPackageVersion -Id 'SST.opencode') `
        -InstallerType 'zip' -Adapter 'Portable' -ExecutableName 'opencode.exe'
    $openCodeVersion = Assert-ProvisioningCommand -Role 'OpenCode' -Name 'opencode.exe' `
        -VersionArguments @('--version') -ExpectedPattern '^\d+\.\d+\.\d+'
    $openCodeManagedDirectory = Join-Path $env:ProgramData 'opencode'
    New-Item -ItemType Directory -Path $openCodeManagedDirectory -Force | Out-Null
    $openCodeAllowAllPermissions = [ordered]@{
        '*' = 'allow'
        read = 'allow'
        edit = 'allow'
        glob = 'allow'
        grep = 'allow'
        list = 'allow'
        bash = 'allow'
        task = 'allow'
        external_directory = 'allow'
        todowrite = 'allow'
        question = 'allow'
        webfetch = 'allow'
        websearch = 'allow'
        lsp = 'allow'
        doom_loop = 'allow'
        skill = 'allow'
        plan_enter = 'allow'
        plan_exit = 'allow'
    }
    $openCodeManagedPluginPath = Join-Path $openCodeManagedDirectory 'sandbox-allow-all.js'
    if ($openCodeManagedPluginPath -notmatch '^[A-Za-z]:\\') {
        throw "OpenCode managed plugin path is not a local absolute path: $openCodeManagedPluginPath"
    }
    $openCodeManagedPluginURI = 'file:///' + $openCodeManagedPluginPath.Replace('\', '/')
    $openCodeAllowAllJSON = $openCodeAllowAllPermissions | ConvertTo-Json -Compress
    $openCodeManagedPlugin = @"
const permissions = $openCodeAllowAllJSON
const allowAll = () => ({ ...permissions })

export default async () => ({
  config: async (config) => {
    config.permission = allowAll()
    for (const agent of Object.values(config.agent ?? {})) {
      agent.permission = allowAll()
    }
  },
})
"@
    $openCodeManagedConfig = ([ordered]@{
        '$schema' = 'https://opencode.ai/config.json'
        permission = $openCodeAllowAllPermissions
        plugin = @($openCodeManagedPluginURI)
    } | ConvertTo-Json -Depth 6) + [Environment]::NewLine
    $utf8NoBom = New-Object Text.UTF8Encoding($false)
    $openCodeManagedPath = Join-Path $openCodeManagedDirectory 'opencode.json'
    foreach ($managedFile in @(
        [pscustomobject]@{ Path = $openCodeManagedPluginPath; Contents = $openCodeManagedPlugin },
        [pscustomobject]@{ Path = $openCodeManagedPath; Contents = $openCodeManagedConfig }
    )) {
        if (-not (Test-Path -LiteralPath $managedFile.Path -PathType Leaf) -or
            [IO.File]::ReadAllText($managedFile.Path) -cne $managedFile.Contents) {
            [IO.File]::WriteAllText($managedFile.Path, $managedFile.Contents, $utf8NoBom)
        }
        if ([IO.File]::ReadAllText($managedFile.Path) -cne $managedFile.Contents) {
            throw "OpenCode managed file verification failed: $($managedFile.Path)"
        }
    }
    $verifiedManagedConfig = [IO.File]::ReadAllText($openCodeManagedPath) | ConvertFrom-Json
    if (@($verifiedManagedConfig.plugin).Count -ne 1 -or
        [string]$verifiedManagedConfig.plugin[0] -cne $openCodeManagedPluginURI) {
        throw 'OpenCode managed plugin configuration was not written correctly.'
    }
    foreach ($permissionName in $openCodeAllowAllPermissions.Keys) {
        $property = $verifiedManagedConfig.permission.PSObject.Properties[$permissionName]
        if ($null -eq $property -or [string]$property.Value -cne 'allow') {
            throw "OpenCode managed permission is not allow: $permissionName"
        }
    }
    $openCodeCommand = Get-Command 'opencode.exe' -CommandType Application -ErrorAction Stop | Select-Object -First 1
    $previousErrorActionPreference = $ErrorActionPreference
    try {
        $ErrorActionPreference = 'Continue'
        $resolvedConfigOutput = @(& $openCodeCommand.Source 'debug' 'config' 2>&1)
        $resolvedConfigExitCode = $LASTEXITCODE
    } finally {
        $ErrorActionPreference = $previousErrorActionPreference
    }
    if ($resolvedConfigExitCode -ne 0) {
        throw "OpenCode managed configuration validation failed with exit code $resolvedConfigExitCode."
    }
    try {
        $resolvedOpenCodeConfig = (($resolvedConfigOutput | ForEach-Object { [string]$_ }) -join [Environment]::NewLine) | ConvertFrom-Json
    } catch {
        throw 'OpenCode managed configuration validation returned invalid JSON.'
    }
    foreach ($permissionName in $openCodeAllowAllPermissions.Keys) {
        $property = $resolvedOpenCodeConfig.permission.PSObject.Properties[$permissionName]
        if ($null -eq $property -or [string]$property.Value -cne 'allow') {
            throw "OpenCode effective managed permission is not allow: $permissionName"
        }
    }
    Write-Output "OpenCode ready: $openCodeVersion"
}

if (Test-ProvisioningPackageEnabled -Id 'WinDirStat.WinDirStat') {
    Write-Output 'Installing WinDirStat...'
    Install-ProvisioningWinGetPackage -Role 'WinDirStat' -Id 'WinDirStat.WinDirStat' `
        -Version (Get-ProvisioningPackageVersion -Id 'WinDirStat.WinDirStat') `
        -InstallerType 'wix' -Scope 'machine' -Adapter 'MSI'
    $winDirStatExecutable = Join-Path $env:ProgramFiles 'WinDirStat\WinDirStat.exe'
    if (-not (Test-Path -LiteralPath $winDirStatExecutable -PathType Leaf) -or
        ((Get-Item -LiteralPath $winDirStatExecutable -Force).Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {
        throw "WinDirStat executable is missing or unsafe: $winDirStatExecutable"
    }
    Write-Output "WinDirStat ready: $winDirStatExecutable"
}

if (Test-ProvisioningPackageEnabled -Id 'Voidstar.FilePilot') {
    Write-Output 'Installing File Pilot...'
    Install-ProvisioningOnlineWinGetPackage -Role 'File Pilot' -Id 'Voidstar.FilePilot' `
        -Version (Get-ProvisioningPackageVersion -Id 'Voidstar.FilePilot')
    Ensure-ProvisioningFilePilotStartShortcut
    Write-Output 'File Pilot ready: FPilot.exe'
}

$terminalPackageID = [string]$provisioningPackagePlan.TerminalID
$terminalPackageFamily = ''
if (Test-ProvisioningPackageEnabled -Id $terminalPackageID) {
    Write-Output 'Installing GeistMono Nerd Font...'
    Install-ProvisioningGeistMonoNerdFont

    Write-Output 'Installing Windows Terminal...'
    $terminalFrameworkID = 'Microsoft.UI.Xaml.2.8'
    Install-ProvisioningWinGetPackage -Role 'Windows UI Xaml 2.8' -Id $terminalFrameworkID `
        -Version (Get-ProvisioningPackageVersion -Id $terminalFrameworkID) `
        -InstallerType 'msix' -Adapter 'MSIX'
    $terminalFrameworks = @(Get-AppxPackage -Name $terminalFrameworkID -ErrorAction SilentlyContinue |
        Where-Object { [string]$_.Architecture -in @('X64', 'Neutral') -and $_.Version -ge [Version]'8.2306.22001.0' })
    if ($terminalFrameworks.Count -lt 1) {
        throw 'Windows UI Xaml 2.8 framework registration is missing or too old.'
    }
    $terminalPackageFamily = 'Microsoft.WindowsTerminal_8wekyb3d8bbwe'
    $terminalScope = 'user'
    if ($WindowsTerminalEdition -eq 'preview') {
        $terminalPackageFamily = 'Microsoft.WindowsTerminalPreview_8wekyb3d8bbwe'
        $terminalScope = ''
    }
    Install-ProvisioningWinGetPackage -Role "Windows Terminal $WindowsTerminalEdition" -Id $terminalPackageID `
        -Version (Get-ProvisioningPackageVersion -Id $terminalPackageID) `
        -InstallerType 'msix' -Scope $terminalScope -Adapter 'MSIX' -ExecutableName 'wt.exe'
    $terminalPackageRoot = Join-Path $env:LOCALAPPDATA (Join-Path 'Packages' $terminalPackageFamily)
    if (-not (Test-Path -LiteralPath $terminalPackageRoot -PathType Container)) {
        throw "Windows Terminal $WindowsTerminalEdition package was not registered at $terminalPackageRoot"
    }
    $terminalCommand = Get-Command 'wt.exe' -CommandType Application -ErrorAction SilentlyContinue | Select-Object -First 1
    if ($null -eq $terminalCommand) {
        throw 'Windows Terminal command is not available on PATH: wt.exe'
    }
    Write-Output 'GeistMono Nerd Font ready: GeistMono Nerd Font'
    Write-Output "Windows Terminal $WindowsTerminalEdition ready: $($terminalCommand.Source)"
}

Ensure-ProvisioningTaskbarPins -Edition $WindowsTerminalEdition `
    -TerminalPackageFamily $terminalPackageFamily

foreach ($package in @($provisioningPackagePlan.Data.additions)) {
    $packageID = [string]$package.id
    Install-ProvisioningOnlineWinGetPackage -Role "additional WinGet package $packageID" `
        -Id $packageID -Version ([string]$package.version)
}

$workspaceManifestPath = Join-Path (Split-Path -Parent $ProjectProvisioningDirectory) 'workspaces.json'
if (-not (Test-Path -LiteralPath $workspaceManifestPath -PathType Leaf)) {
    throw "Workspace manifest is missing: $workspaceManifestPath"
}
$workspaceManifest = [IO.File]::ReadAllText($workspaceManifestPath) | ConvertFrom-Json
$workspaceManifestProperties = @($workspaceManifest.PSObject.Properties.Name | Sort-Object)
$provisioningWorkspaces = @($workspaceManifest.workspaces)
if (($workspaceManifestProperties -join '|') -cne 'activeWorkspace|schemaVersion|workspaces' -or
    [int]$workspaceManifest.schemaVersion -ne 1 -or $provisioningWorkspaces.Count -eq 0 -or
    $provisioningWorkspaces.Count -gt 16) {
    throw 'Workspace manifest has an unsupported Base contract.'
}
$guestSafeDirectories = @()
$provisioningWorkspaceNames = @{}
$activeWorkspace = [string]$workspaceManifest.activeWorkspace
$activeWorkspaceMatches = 0
foreach ($workspace in $provisioningWorkspaces) {
    $entryProperties = @($workspace.PSObject.Properties.Name | Sort-Object)
    $workspaceName = [string]$workspace.name
    $projectDirectory = [string]$workspace.directory
    $expectedDirectory = Join-Path $WorkspacesDirectory $workspaceName
    $workspaceIdentity = $workspaceName.ToLowerInvariant()
    if (($entryProperties -join '|') -cne 'directory|name' -or
        $workspaceName -notmatch '^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$' -or
        $projectDirectory -cne $expectedDirectory -or
        -not (Test-Path -LiteralPath $projectDirectory -PathType Container) -or
        -not (Test-Path -LiteralPath (Join-Path $ProjectProvisioningDirectory ($workspaceName + '.ps1')) -PathType Leaf) -or
        $provisioningWorkspaceNames.ContainsKey($workspaceIdentity)) {
        throw "Workspace manifest entry is invalid for Base provisioning: $workspaceName"
    }
    $provisioningWorkspaceNames[$workspaceIdentity] = $true
    if ($projectDirectory -ceq $activeWorkspace) { $activeWorkspaceMatches += 1 }
    $guestSafeDirectories += $projectDirectory.Replace('\', '/')
}
if ($activeWorkspaceMatches -ne 1) {
    throw 'Workspace manifest active workspace is invalid for Base provisioning.'
}
if (Test-ProvisioningPackageEnabled -Id 'Git.Git') {
    Wait-ProvisioningCommandAvailable -Role 'Git' -Name 'git.exe' | Out-Null
    $gitVersion = Assert-ProvisioningCommand -Role 'Git' -Name 'git.exe' `
        -VersionArguments @('--version') -ExpectedPattern '^git version '
    $gitCommand = (Get-Command 'git.exe' -CommandType Application -ErrorAction Stop | Select-Object -First 1).Source
    $previousErrorActionPreference = $ErrorActionPreference
    try {
        $ErrorActionPreference = 'Continue'
        $existingSafeDirectories = @(& $gitCommand config --global --get-all safe.directory 2>&1 |
            ForEach-Object { [string]$_ })
        $safeDirectoryExitCode = $LASTEXITCODE
    } finally {
        $ErrorActionPreference = $previousErrorActionPreference
    }
    if ($safeDirectoryExitCode -notin @(0, 1)) {
        throw "Git safe-directory inspection failed with exit code $safeDirectoryExitCode."
    }
    if ($safeDirectoryExitCode -eq 1) {
        $existingSafeDirectories = @()
    }
    if (($existingSafeDirectories -join '|') -cne ($guestSafeDirectories -join '|')) {
        Invoke-ProvisioningNative -Role 'Git safe-directory reset' -FilePath $gitCommand `
            -ArgumentList @('config', '--global', '--replace-all', 'safe.directory', $guestSafeDirectories[0]) | Out-Null
        foreach ($safeDirectory in @($guestSafeDirectories | Select-Object -Skip 1)) {
            Invoke-ProvisioningNative -Role 'Git safe-directory addition' -FilePath $gitCommand `
                -ArgumentList @('config', '--global', '--add', 'safe.directory', $safeDirectory) | Out-Null
        }
    }
    $verifiedSafeDirectories = @(Invoke-ProvisioningNative -Role 'Git safe-directory verification' -FilePath $gitCommand `
        -ArgumentList @('config', '--global', '--get-all', 'safe.directory'))
    if (($verifiedSafeDirectories -join '|') -cne ($guestSafeDirectories -join '|')) {
        throw "Git safe-directory verification failed: $($verifiedSafeDirectories -join ', ')"
    }
    Write-Output "Git ready: $gitVersion"
}

if (-not (Test-Path -LiteralPath $ProjectProvisioningDirectory -PathType Container)) {
    throw "Project provisioning directory is missing: $ProjectProvisioningDirectory"
}
$stackProvisioning = Join-Path $PSScriptRoot 'stacks.ps1'
if (-not (Test-Path -LiteralPath $stackProvisioning -PathType Leaf)) {
    throw "App-owned stack provisioning library is missing: $stackProvisioning"
}
. $stackProvisioning
$userProvisioning = Get-Item -LiteralPath $UserProvisioningPath -Force
if (-not $userProvisioning.PSIsContainer -and $userProvisioning.Length -gt 0 -and
    $userProvisioning.Length -le 1048576) {
    $userProvisioningText = [IO.File]::ReadAllText($userProvisioning.FullName)
    if (-not $userProvisioningText.Contains('# herdr-sandbox-user-contract: 1') -or
        $userProvisioningText.Contains('# herdr-sandbox-base-contract:')) {
        throw "User provisioning contract is invalid: $UserProvisioningPath"
    }
    Write-Output 'Running global user provisioning'
    & $userProvisioning.FullName
} else {
    throw "User provisioning script identity is invalid: $UserProvisioningPath"
}
foreach ($workspace in @($provisioningWorkspaces | Sort-Object name)) {
    $workspaceName = [string]$workspace.name
    $projectDirectory = [string]$workspace.directory
    $projectScript = Get-Item -LiteralPath (Join-Path $ProjectProvisioningDirectory ($workspaceName + '.ps1'))
    Write-Output "Running project provisioning for $workspaceName"
    & $projectScript.FullName -ProjectDirectory $projectDirectory
}

$packageStageRoot = 'C:\HerdrSandbox\staging\packages'
if (Test-Path -LiteralPath $packageStageRoot -PathType Container) {
    foreach ($stageDirectory in @(Get-ChildItem -LiteralPath $packageStageRoot -Directory -Force)) {
        Remove-ProvisioningGuestPackageStage -Path $stageDirectory.FullName -Attempts 1 `
            -DelayMilliseconds 0 -BestEffort | Out-Null
    }
}
$provisioningStopwatch.Stop()
Write-ProvisioningTiming -Role 'complete development provisioning' -Seconds $provisioningStopwatch.Elapsed.TotalSeconds
