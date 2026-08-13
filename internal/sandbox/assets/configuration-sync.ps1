$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'
$archive = [string]$env:HERDR_SANDBOX_CONFIGURATION_ARCHIVE
$expanded = [string]$env:HERDR_SANDBOX_CONFIGURATION_EXPANDED
if ([string]::IsNullOrWhiteSpace($archive) -or [string]::IsNullOrWhiteSpace($expanded) -or
    -not [IO.Path]::IsPathRooted($archive) -or -not [IO.Path]::IsPathRooted($expanded) -or
    -not (Test-Path -LiteralPath $archive -PathType Leaf) -or
    -not (Test-Path -LiteralPath $expanded -PathType Container)) {
    throw 'Development configuration launcher state is invalid.'
}
$script:CopiedConfigurationFiles = 0
$script:Utf8NoBom = New-Object Text.UTF8Encoding($false)
function Assert-ConfigurationDestinationPath {
    param([Parameter(Mandatory = $true)][string]$Path)
    $fullPath = [IO.Path]::GetFullPath($Path)
    if (-not [IO.Path]::IsPathRooted($fullPath)) {
        throw "Configuration destination is not absolute: $Path"
    }
    $root = [IO.Path]::GetPathRoot($fullPath).TrimEnd('\')
    $current = $fullPath
    while ($current.Length -ge $root.Length) {
        if (Test-Path -LiteralPath $current) {
            $item = Get-Item -LiteralPath $current -Force
            if (($item.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {
                throw "Configuration destination contains a reparse point: $current"
            }
        }
        if ($current -ieq $root) { break }
        $parent = Split-Path -Parent $current
        if ([string]::IsNullOrWhiteSpace($parent) -or $parent -ieq $current) {
            throw "Configuration destination parent resolution failed: $fullPath"
        }
        $current = $parent.TrimEnd('\')
    }
}
function Copy-VerifiedConfigurationFile {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Source,
        [Parameter(Mandatory = $true)]
        [string]$Destination
    )
    if (-not (Test-Path -LiteralPath $Source -PathType Leaf)) {
        throw "Configuration source file is missing: $Source"
    }
    Assert-ConfigurationDestinationPath -Path $Destination
    $destinationDirectory = Split-Path -Parent $Destination
    New-Item -ItemType Directory -Path $destinationDirectory -Force | Out-Null
    Copy-Item -LiteralPath $Source -Destination $Destination -Force
    $expected = (Get-FileHash -LiteralPath $Source -Algorithm SHA256).Hash
    $actual = (Get-FileHash -LiteralPath $Destination -Algorithm SHA256).Hash
    if ($actual -ne $expected) {
        throw "Configuration destination hash mismatch: $Destination"
    }
    $script:CopiedConfigurationFiles += 1
}
function Set-AtomicConfigurationFile {
    param(
        [Parameter(Mandatory = $true)][string]$Source,
        [Parameter(Mandatory = $true)][string]$Destination
    )
    if (-not (Test-Path -LiteralPath $Source -PathType Leaf)) {
        throw "Configuration source file is missing: $Source"
    }
    Assert-ConfigurationDestinationPath -Path $Destination
    $destinationDirectory = Split-Path -Parent $Destination
    New-Item -ItemType Directory -Path $destinationDirectory -Force | Out-Null
    $temporary = Join-Path $destinationDirectory ('.herdr-sandbox-config-' + [Guid]::NewGuid().ToString('N') + '.tmp')
    $backup = $null
    try {
        [IO.File]::Copy($Source, $temporary, $false)
        if (Test-Path -LiteralPath $Destination -PathType Leaf) {
            $backup = Join-Path $destinationDirectory ('.herdr-sandbox-config-' + [Guid]::NewGuid().ToString('N') + '.bak')
            [IO.File]::Replace($temporary, $Destination, $backup, $true)
        } else {
            [IO.File]::Move($temporary, $Destination)
        }
    } finally {
        if (Test-Path -LiteralPath $temporary) { [IO.File]::Delete($temporary) }
        if ($null -ne $backup -and (Test-Path -LiteralPath $backup)) { [IO.File]::Delete($backup) }
    }
    $expected = (Get-FileHash -LiteralPath $Source -Algorithm SHA256).Hash
    $actual = (Get-FileHash -LiteralPath $Destination -Algorithm SHA256).Hash
    if ($actual -ne $expected) {
        throw "Atomic configuration destination hash mismatch: $Destination"
    }
    $script:CopiedConfigurationFiles += 1
}
function Copy-VerifiedConfigurationTree {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Source,
        [Parameter(Mandatory = $true)]
        [string]$Destination
    )
    if (-not (Test-Path -LiteralPath $Source -PathType Container)) {
        throw "Configuration source directory is missing: $Source"
    }
    $sourceItem = Get-Item -LiteralPath $Source -Force
    if (($sourceItem.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {
        throw "Configuration source contains a reparse point: $Source"
    }
    Assert-ConfigurationDestinationPath -Path $Destination
    New-Item -ItemType Directory -Path $Destination -Force | Out-Null
    foreach ($entry in @(Get-ChildItem -LiteralPath $Source -Force)) {
        if (($entry.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {
            throw "Configuration source contains a reparse point: $($entry.FullName)"
        }
        $destinationEntry = Join-Path $Destination $entry.Name
        if ($entry.PSIsContainer) {
            Copy-VerifiedConfigurationTree -Source $entry.FullName -Destination $destinationEntry
        } else {
            Copy-VerifiedConfigurationFile -Source $entry.FullName -Destination $destinationEntry
        }
    }
}
function Sync-VerifiedConfigurationRoot {
    param(
        [Parameter(Mandatory = $true)][string]$Source,
        [Parameter(Mandatory = $true)][string]$Destination
    )
    if (-not (Test-Path -LiteralPath $Source -PathType Container)) { return }
    Copy-VerifiedConfigurationTree -Source $Source -Destination $Destination
}
function Remove-VerifiedTrackedConfigurationFiles {
    param(
        [Parameter(Mandatory = $true)][string]$Destination,
        [Parameter(Mandatory = $true)][AllowEmptyCollection()][object[]]$Paths
    )
    $destinationRoot = [IO.Path]::GetFullPath($Destination).TrimEnd('\') + '\'
    foreach ($pathValue in $Paths) {
        if ($pathValue -isnot [string]) {
            throw 'Tracked configuration deletion path is not a string.'
        }
        $relative = [string]$pathValue
        $segments = @($relative -split '/')
        if ([string]::IsNullOrWhiteSpace($relative) -or $relative.Length -gt 32767 -or
            [IO.Path]::IsPathRooted($relative) -or $relative.Contains('\') -or $relative.Contains(':') -or
            $relative -match '[\x00\r\n]' -or $segments.Count -eq 0 -or
            @($segments | Where-Object { [string]::IsNullOrWhiteSpace($_) -or $_ -in @('.', '..') }).Count -ne 0) {
            throw "Tracked configuration deletion path is unsafe: $relative"
        }
        $target = [IO.Path]::GetFullPath((Join-Path $Destination ($relative.Replace('/', '\'))))
        if (-not $target.StartsWith($destinationRoot, [StringComparison]::OrdinalIgnoreCase)) {
            throw "Tracked configuration deletion escapes its destination: $relative"
        }
        Assert-ConfigurationDestinationPath -Path $target
        if (Test-Path -LiteralPath $target) {
            $item = Get-Item -LiteralPath $target -Force
            if ($item.PSIsContainer -or ($item.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {
                throw "Tracked configuration deletion target is unsafe: $target"
            }
            Remove-Item -LiteralPath $target -Force
            if (Test-Path -LiteralPath $target) {
                throw "Tracked configuration deletion failed: $target"
            }
        }
    }
}
function Sync-OptionalConfigurationFile {
    param(
        [Parameter(Mandatory = $true)][string]$Source,
        [Parameter(Mandatory = $true)][string]$Destination
    )
    if (-not (Test-Path -LiteralPath $Source -PathType Leaf)) { return }
    Copy-VerifiedConfigurationFile -Source $Source -Destination $Destination
}
function Sync-ClaudeCodeUserState {
    param(
        [Parameter(Mandatory = $true)][string]$Source,
        [Parameter(Mandatory = $true)][string]$Destination
    )
    if (-not (Test-Path -LiteralPath $Source -PathType Leaf)) { return }
    $incoming = [IO.File]::ReadAllText($Source) | ConvertFrom-Json
    $incomingProperties = @($incoming.PSObject.Properties.Name)
    if (($incomingProperties -join '|') -cne 'mcpServers') {
        throw 'Claude Code user-state input has an unsupported shape.'
    }
    $destinationState = [pscustomobject]@{}
    if (Test-Path -LiteralPath $Destination -PathType Leaf) {
        $destinationState = [IO.File]::ReadAllText($Destination) | ConvertFrom-Json
    }
    $destinationState | Add-Member -MemberType NoteProperty -Name mcpServers -Value $incoming.mcpServers -Force
    $destinationDirectory = Split-Path -Parent $Destination
    Assert-ConfigurationDestinationPath -Path $Destination
    New-Item -ItemType Directory -Path $destinationDirectory -Force | Out-Null
    $encoded = $destinationState | ConvertTo-Json -Depth 100
    [IO.File]::WriteAllText($Destination, $encoded + [Environment]::NewLine, (New-Object Text.UTF8Encoding($false)))
    $verified = [IO.File]::ReadAllText($Destination) | ConvertFrom-Json
    $expectedMCP = $incoming.mcpServers | ConvertTo-Json -Depth 100 -Compress
    $actualMCP = $verified.mcpServers | ConvertTo-Json -Depth 100 -Compress
    if ($actualMCP -cne $expectedMCP) {
        throw 'Claude Code user MCP configuration verification failed.'
    }
    $script:CopiedConfigurationFiles += 1
}
function Set-ManagedAgentWorktreeInstructions {
    param(
        [Parameter(Mandatory = $true)][string]$Source,
        [Parameter(Mandatory = $true)][string[]]$Destinations
    )
    if (-not (Test-Path -LiteralPath $Source -PathType Leaf)) {
        throw 'Agent worktree instruction source is missing.'
    }
    $block = [IO.File]::ReadAllText($Source).Replace("`r`n", "`n").TrimEnd("`r", "`n")
    $startMarker = '<!-- herdr-sandbox:worktrees:start -->'
    $endMarker = '<!-- herdr-sandbox:worktrees:end -->'
    if (-not $block.StartsWith($startMarker, [StringComparison]::Ordinal) -or
        -not $block.EndsWith($endMarker, [StringComparison]::Ordinal) -or
        ([regex]::Matches($block, [regex]::Escape($startMarker))).Count -ne 1 -or
        ([regex]::Matches($block, [regex]::Escape($endMarker))).Count -ne 1) {
        throw 'Agent worktree instruction source has invalid ownership markers.'
    }
    foreach ($destination in $Destinations) {
        Assert-ConfigurationDestinationPath -Path $destination
        $existing = ''
        if (Test-Path -LiteralPath $destination -PathType Leaf) {
            $existing = [IO.File]::ReadAllText($destination).Replace("`r`n", "`n")
        }
        $startMatches = [regex]::Matches($existing, [regex]::Escape($startMarker))
        $endMatches = [regex]::Matches($existing, [regex]::Escape($endMarker))
        if ($startMatches.Count -ne $endMatches.Count -or $startMatches.Count -gt 1) {
            throw "Agent worktree instruction destination has invalid ownership markers: $destination"
        }
        if ($startMatches.Count -eq 1) {
            if ($endMatches[0].Index -le $startMatches[0].Index) {
                throw "Agent worktree instruction destination has invalid marker ordering: $destination"
            }
            $after = $endMatches[0].Index + $endMatches[0].Length
            $existing = $existing.Remove($startMatches[0].Index, $after - $startMatches[0].Index)
        }
        $existing = $existing.TrimStart("`r", "`n")
        $updated = $block + "`n"
        if (-not [string]::IsNullOrWhiteSpace($existing)) {
            $updated += "`n" + $existing.TrimEnd("`r", "`n") + "`n"
        }
        $destinationDirectory = Split-Path -Parent $destination
        New-Item -ItemType Directory -Path $destinationDirectory -Force | Out-Null
        [IO.File]::WriteAllText($destination, $updated, $script:Utf8NoBom)
        if ([IO.File]::ReadAllText($destination).Replace("`r`n", "`n") -cne $updated) {
            throw "Agent worktree instruction destination verification failed: $destination"
        }
    }
}
function Get-OpenCodeAllowAllPermissions {
    return [ordered]@{
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
}
function Install-OpenCodeAllowAllPolicy {
    $programData = [string]$env:ProgramData
    if ([string]::IsNullOrWhiteSpace($programData) -or -not [IO.Path]::IsPathRooted($programData)) {
        throw 'OpenCode managed policy requires an absolute ProgramData path.'
    }
    $permissions = Get-OpenCodeAllowAllPermissions
    $managedDirectory = Join-Path $programData 'opencode'
    $managedPluginPath = Join-Path $managedDirectory 'sandbox-allow-all.js'
    $managedConfigPath = Join-Path $managedDirectory 'opencode.json'
    foreach ($path in @($managedPluginPath, $managedConfigPath)) {
        Assert-ConfigurationDestinationPath -Path $path
    }
    New-Item -ItemType Directory -Path $managedDirectory -Force | Out-Null

    $managedPluginURI = 'file:///' + $managedPluginPath.Replace('\', '/')
    $allowAllJSON = $permissions | ConvertTo-Json -Compress
    $managedPlugin = @"
const permissions = $allowAllJSON
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
    $managedConfig = ([ordered]@{
        '$schema' = 'https://opencode.ai/config.json'
        permission = $permissions
        plugin = @($managedPluginURI)
    } | ConvertTo-Json -Depth 6) + [Environment]::NewLine
    $utf8NoBom = New-Object Text.UTF8Encoding($false)
    foreach ($managedFile in @(
        [pscustomobject]@{ Path = $managedPluginPath; Contents = $managedPlugin },
        [pscustomobject]@{ Path = $managedConfigPath; Contents = $managedConfig }
    )) {
        if (-not (Test-Path -LiteralPath $managedFile.Path -PathType Leaf) -or
            [IO.File]::ReadAllText($managedFile.Path) -cne $managedFile.Contents) {
            [IO.File]::WriteAllText($managedFile.Path, $managedFile.Contents, $utf8NoBom)
        }
        if ([IO.File]::ReadAllText($managedFile.Path) -cne $managedFile.Contents) {
            throw "OpenCode managed file verification failed: $($managedFile.Path)"
        }
    }

    $verified = [IO.File]::ReadAllText($managedConfigPath) | ConvertFrom-Json
    if (@($verified.plugin).Count -ne 1 -or [string]$verified.plugin[0] -cne $managedPluginURI) {
        throw 'OpenCode managed plugin configuration was not written correctly.'
    }
    foreach ($permissionName in $permissions.Keys) {
        $property = $verified.permission.PSObject.Properties[$permissionName]
        if ($null -eq $property -or [string]$property.Value -cne 'allow') {
            throw "OpenCode managed permission is not allow: $permissionName"
        }
    }
}
function Invoke-GuestGitHubCLI {
    param(
        [Parameter(Mandatory = $true)][string]$Role,
        [Parameter(Mandatory = $true)][string[]]$Arguments,
        [AllowEmptyString()][string]$InputText = '',
        [switch]$UseStandardInput
    )
    $previousErrorActionPreference = $ErrorActionPreference
    try {
        $ErrorActionPreference = 'Continue'
        if ($UseStandardInput) {
            $output = @($InputText | & $script:GitHubCLICommand @Arguments 2>&1)
        } else {
            $output = @(& $script:GitHubCLICommand @Arguments 2>&1)
        }
        $exitCode = $LASTEXITCODE
    } finally {
        $ErrorActionPreference = $previousErrorActionPreference
    }
    if ($exitCode -ne 0) {
        throw "$Role failed with exit code $exitCode."
    }
    return $output
}
function Invoke-OpenCodeJSON {
    param(
        [Parameter(Mandatory = $true)][string]$Role,
        [Parameter(Mandatory = $true)][string[]]$Arguments
    )
    $previousErrorActionPreference = $ErrorActionPreference
    try {
        $ErrorActionPreference = 'Continue'
        $output = @(& $script:OpenCodeCommand @Arguments 2>&1)
        $exitCode = $LASTEXITCODE
    } finally {
        $ErrorActionPreference = $previousErrorActionPreference
    }
    if ($exitCode -ne 0) {
        throw "$Role failed with exit code $exitCode."
    }
    try {
        return (($output | ForEach-Object { [string]$_ }) -join [Environment]::NewLine) | ConvertFrom-Json
    } catch {
        throw "$Role returned invalid JSON."
    }
}
function Assert-OpenCodeAllowAll {
    $allowAllPermissions = Get-OpenCodeAllowAllPermissions
    $requiredPermissions = @($allowAllPermissions.Keys | Where-Object { $_ -cne '*' })
    $resolvedConfig = Invoke-OpenCodeJSON -Role 'OpenCode effective configuration inspection' -Arguments @('debug', 'config')
    $resolvedPermissionNames = @($resolvedConfig.permission.PSObject.Properties.Name | Sort-Object)
    $expectedPermissionNames = @($allowAllPermissions.Keys | Sort-Object)
    if (($resolvedPermissionNames -join '|') -cne ($expectedPermissionNames -join '|')) {
        throw 'OpenCode effective permissions were not replaced by the Sandbox allow-all policy.'
    }
    foreach ($permissionName in $allowAllPermissions.Keys) {
        $property = $resolvedConfig.permission.PSObject.Properties[$permissionName]
        if ($null -eq $property -or [string]$property.Value -cne 'allow') {
            throw "OpenCode effective permission is not allow: $permissionName"
        }
    }
    $agentNames = @('build', 'plan', 'general', 'explore', 'compaction', 'title', 'summary')
    if ($null -ne $resolvedConfig.agent) {
        $agentNames += @($resolvedConfig.agent.PSObject.Properties.Name)
    }
    foreach ($agentName in @($agentNames | Sort-Object -Unique)) {
        $agent = Invoke-OpenCodeJSON -Role "OpenCode agent permission inspection ($agentName)" -Arguments @('debug', 'agent', [string]$agentName)
        $rules = @($agent.permission)
        $lastAllowAll = -1
        for ($index = 0; $index -lt $rules.Count; $index++) {
            if ([string]$rules[$index].permission -ceq '*' -and
                [string]$rules[$index].pattern -ceq '*' -and
                [string]$rules[$index].action -ceq 'allow') {
                $lastAllowAll = $index
            }
        }
        if ($lastAllowAll -lt 0) {
            throw "OpenCode agent lacks a final allow-all rule: $agentName"
        }
        for ($index = $lastAllowAll + 1; $index -lt $rules.Count; $index++) {
            if ([string]$rules[$index].action -cne 'allow') {
                throw "OpenCode agent has a restrictive rule after allow-all: $agentName"
            }
        }
        foreach ($permissionName in $requiredPermissions) {
            $matches = @($rules | Where-Object {
                ([string]$_.permission -ceq '*' -or [string]$_.permission -ceq $permissionName) -and
                [string]$_.pattern -ceq '*'
            })
            if ($matches.Count -eq 0 -or [string]$matches[-1].action -cne 'allow') {
                throw "OpenCode agent permission is not allow: $agentName/$permissionName"
            }
        }
    }
}
function Enable-OpenCodeAllowAllPolicy {
    param([Parameter(Mandatory = $true)][bool]$RequireExecutable)
    Install-OpenCodeAllowAllPolicy
    $openCodeCommand = Get-Command 'opencode.exe' -CommandType Application -ErrorAction SilentlyContinue | Select-Object -First 1
    if ($null -eq $openCodeCommand) {
        if ($RequireExecutable) {
            throw 'OpenCode is selected but opencode.exe is unavailable during permission verification.'
        }
        return $false
    }
    $script:OpenCodeCommand = [string]$openCodeCommand.Source
    Assert-OpenCodeAllowAll
    return $true
}
function Test-TradingViewJSONInteger {
    param([AllowNull()][object]$Value)
    return $Value -is [int] -or $Value -is [long]
}
function Test-TradingViewCookieHost {
    param([AllowEmptyString()][string]$HostKey)
    if ([string]::IsNullOrWhiteSpace($HostKey) -or $HostKey.Length -gt 512 -or
        $HostKey.Trim() -cne $HostKey -or $HostKey -match '[\x00\r\n]') {
        return $false
    }
    $folded = $HostKey.ToLowerInvariant()
    return $folded -ceq 'tradingview.com' -or $folded -ceq '.tradingview.com' -or
        $folded.EndsWith('.tradingview.com', [StringComparison]::Ordinal)
}
function Test-TradingViewCookieValue {
    param([AllowEmptyString()][string]$Value)
    return -not [string]::IsNullOrEmpty($Value) -and
        [Text.Encoding]::UTF8.GetByteCount($Value) -le 16384 -and
        $Value -match '^[\x21\x23-\x2B\x2D-\x3A\x3C-\x5B\x5D-\x7E]+$'
}
$digest = (Get-FileHash -LiteralPath $archive -Algorithm SHA256).Hash.ToLowerInvariant()

    $packagePlanPath = Join-Path $expanded 'herdr-sandbox\winget-packages.json'
    if (-not (Test-Path -LiteralPath $packagePlanPath -PathType Leaf)) {
        throw 'Resolved WinGet package plan is missing from the configuration archive.'
    }
    $packagePlan = [IO.File]::ReadAllText($packagePlanPath) | ConvertFrom-Json
    $packagePlanProperties = @($packagePlan.PSObject.Properties.Name | Sort-Object)
    $packageEntries = @($packagePlan.defaults) + @($packagePlan.additions)
    if (($packagePlanProperties -join '|') -cne 'additions|defaults|schemaVersion|windowsTerminalEdition' -or
        $packagePlan.schemaVersion -isnot [int] -or $packagePlan.windowsTerminalEdition -isnot [string] -or
        [int]$packagePlan.schemaVersion -ne 1 -or
        [string]$packagePlan.windowsTerminalEdition -notin @('stable', 'preview') -or
        $packageEntries.Count -eq 0 -or $packageEntries.Count -gt 75) {
        throw 'Resolved WinGet package plan has an unsupported configuration-sync contract.'
    }
    $enabledPackages = @{}
    foreach ($entry in $packageEntries) {
        $entryProperties = @($entry.PSObject.Properties.Name | Sort-Object)
        $id = [string]$entry.id
        $version = [string]$entry.version
        if (($entryProperties -join '|') -cne 'id|version' -or
            $entry.id -isnot [string] -or $entry.version -isnot [string] -or
            $id -notmatch '^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$' -or
            (-not [string]::IsNullOrEmpty($version) -and
                $version -notmatch '^[A-Za-z0-9][A-Za-z0-9._+-]{0,127}$') -or
            $enabledPackages.ContainsKey($id)) {
            throw "Resolved WinGet package plan entry is invalid: $id"
        }
        $enabledPackages[$id] = $true
    }
    if (-not $enabledPackages.ContainsKey('Microsoft.PowerShell')) {
        throw 'Resolved WinGet package plan is missing Core PowerShell 7.'
    }
    $agentSyncPath = Join-Path $expanded 'herdr-sandbox\coding-agent-sync.json'
    if (-not (Test-Path -LiteralPath $agentSyncPath -PathType Leaf)) {
        throw 'Coding-agent sync manifest is missing from the configuration archive.'
    }
    $agentSync = [IO.File]::ReadAllText($agentSyncPath) | ConvertFrom-Json
    $agentSyncProperties = @($agentSync.PSObject.Properties.Name | Sort-Object)
    if (($agentSyncProperties -join '|') -cne 'claudeCode|codex|githubCopilot|gitTrackedDeletions|opencode|pi|schemaVersion' -or
        $agentSync.schemaVersion -isnot [int] -or [int]$agentSync.schemaVersion -ne 2 -or
        $agentSync.opencode -isnot [bool] -or $agentSync.claudeCode -isnot [bool] -or
        $agentSync.codex -isnot [bool] -or $agentSync.githubCopilot -isnot [bool] -or
        $agentSync.pi -isnot [bool] -or $null -eq $agentSync.gitTrackedDeletions) {
        throw 'Coding-agent sync manifest has an unsupported contract.'
    }
    $allowedGitDeletionRoots = @{
        'opencode' = [bool]$agentSync.opencode
        'claude-code' = [bool]$agentSync.claudeCode
        'codex' = [bool]$agentSync.codex
        'github-copilot' = [bool]$agentSync.githubCopilot
        'pi' = [bool]$agentSync.pi
        'shared-agent-skills' = ([bool]$agentSync.codex -or [bool]$agentSync.githubCopilot -or [bool]$agentSync.pi)
    }
    $gitDeletionCount = 0
    foreach ($property in @($agentSync.gitTrackedDeletions.PSObject.Properties)) {
        $rootName = [string]$property.Name
        $paths = @($property.Value)
        if (-not $allowedGitDeletionRoots.ContainsKey($rootName) -or -not [bool]$allowedGitDeletionRoots[$rootName] -or $paths.Count -eq 0) {
            throw "Coding-agent Git deletion root is invalid: $rootName"
        }
        foreach ($pathValue in $paths) {
            if ($pathValue -isnot [string]) {
                throw "Coding-agent Git deletion path is invalid for $rootName."
            }
            $gitDeletionCount += 1
        }
    }
    if ($gitDeletionCount -gt 4096) {
        throw 'Coding-agent Git deletion count exceeds its limit.'
    }
    function Get-AgentGitTrackedDeletions {
        param([Parameter(Mandatory = $true)][string]$Name)
        $property = $agentSync.gitTrackedDeletions.PSObject.Properties[$Name]
        if ($null -eq $property) { return @() }
        return @($property.Value)
    }
    $gitEnabled = $enabledPackages.ContainsKey('Git.Git')
    $githubCLIEnabled = $enabledPackages.ContainsKey('GitHub.cli')
    $openCodeEnabled = $enabledPackages.ContainsKey('SST.opencode')
    $starshipEnabled = $enabledPackages.ContainsKey('Starship.Starship')
    $terminalPackageID = 'Microsoft.WindowsTerminal'
    if ([string]$packagePlan.windowsTerminalEdition -ceq 'preview') {
        $terminalPackageID = 'Microsoft.WindowsTerminal.Preview'
    }
    $windowsTerminalEnabled = $enabledPackages.ContainsKey($terminalPackageID)
    $githubAccounts = @()
    $githubAuthenticationVerified = $false
    $openCodePermissionVerified = $false
    $starshipPreset = ''
    $starshipConfigured = $false
    $terminalEdition = ''
    $tradingViewAuthenticatedCookies = 0
    $tradingViewAuthenticationVerified = $false

    if ([bool]$agentSync.opencode) {
        [Console]::Error.WriteLine('[config-sync] apply-opencode')
        $openCodeSource = Join-Path $expanded 'opencode'
        $openCodeDestination = Join-Path $env:USERPROFILE '.config\opencode'
        Sync-VerifiedConfigurationRoot -Source $openCodeSource -Destination $openCodeDestination
        Remove-VerifiedTrackedConfigurationFiles -Destination $openCodeDestination -Paths @(Get-AgentGitTrackedDeletions -Name 'opencode')
        Sync-OptionalConfigurationFile -Source (Join-Path $expanded 'opencode-auth\auth.json') -Destination (Join-Path $env:USERPROFILE '.local\share\opencode\auth.json')
    }

    $openCodeInstalled = $null -ne (Get-Command 'opencode.exe' -CommandType Application -ErrorAction SilentlyContinue |
        Select-Object -First 1)
    if ([bool]$agentSync.opencode -or $openCodeEnabled -or $openCodeInstalled) {
        [Console]::Error.WriteLine('[config-sync] enforce-opencode-allow-all')
        $openCodePermissionVerified = [bool](Enable-OpenCodeAllowAllPolicy `
            -RequireExecutable ($openCodeEnabled -or $openCodeInstalled))
    }

    if ([bool]$agentSync.claudeCode) {
        [Console]::Error.WriteLine('[config-sync] apply-claude-code')
        $claudeDestination = Join-Path $env:USERPROFILE '.claude'
        Sync-VerifiedConfigurationRoot -Source (Join-Path $expanded 'claude-code') -Destination $claudeDestination
        Remove-VerifiedTrackedConfigurationFiles -Destination $claudeDestination -Paths @(Get-AgentGitTrackedDeletions -Name 'claude-code')
        Sync-OptionalConfigurationFile -Source (Join-Path $expanded 'claude-code-auth\.credentials.json') -Destination (Join-Path $claudeDestination '.credentials.json')
        Sync-ClaudeCodeUserState -Source (Join-Path $expanded 'claude-code-state\.claude.json') -Destination (Join-Path $env:USERPROFILE '.claude.json')
    }

    if ([bool]$agentSync.codex) {
        [Console]::Error.WriteLine('[config-sync] apply-codex')
        $codexDestination = Join-Path $env:USERPROFILE '.codex'
        Sync-VerifiedConfigurationRoot -Source (Join-Path $expanded 'codex') -Destination $codexDestination
        Remove-VerifiedTrackedConfigurationFiles -Destination $codexDestination -Paths @(Get-AgentGitTrackedDeletions -Name 'codex')
        Sync-OptionalConfigurationFile -Source (Join-Path $expanded 'codex-auth\auth.json') -Destination (Join-Path $codexDestination 'auth.json')
        Sync-OptionalConfigurationFile -Source (Join-Path $expanded 'codex-auth\.credentials.json') -Destination (Join-Path $codexDestination '.credentials.json')
    }

    if ([bool]$agentSync.githubCopilot) {
        [Console]::Error.WriteLine('[config-sync] apply-github-copilot')
        $copilotDestination = Join-Path $env:USERPROFILE '.copilot'
        Sync-VerifiedConfigurationRoot -Source (Join-Path $expanded 'github-copilot') -Destination $copilotDestination
        Remove-VerifiedTrackedConfigurationFiles -Destination $copilotDestination -Paths @(Get-AgentGitTrackedDeletions -Name 'github-copilot')
    }

    if ([bool]$agentSync.pi) {
        [Console]::Error.WriteLine('[config-sync] apply-pi')
        $piDestination = Join-Path $env:USERPROFILE '.pi\agent'
        Sync-VerifiedConfigurationRoot -Source (Join-Path $expanded 'pi') -Destination $piDestination
        Remove-VerifiedTrackedConfigurationFiles -Destination $piDestination -Paths @(Get-AgentGitTrackedDeletions -Name 'pi')
        Sync-OptionalConfigurationFile -Source (Join-Path $expanded 'pi-auth\auth.json') -Destination (Join-Path $piDestination 'auth.json')
    }

    if ([bool]$agentSync.codex -or [bool]$agentSync.githubCopilot -or [bool]$agentSync.pi) {
        [Console]::Error.WriteLine('[config-sync] apply-shared-agent-skills')
        $sharedSkillsDestination = Join-Path $env:USERPROFILE '.agents\skills'
        $sharedSkillsSource = Join-Path $expanded 'shared-agent-skills'
        if (Test-Path -LiteralPath $sharedSkillsSource -PathType Container) {
            Copy-VerifiedConfigurationTree -Source $sharedSkillsSource -Destination $sharedSkillsDestination
            Remove-VerifiedTrackedConfigurationFiles -Destination $sharedSkillsDestination -Paths @(Get-AgentGitTrackedDeletions -Name 'shared-agent-skills')
        }
    }

    $worktreeDirectoryPath = Join-Path $expanded 'herdr-sandbox\worktree-directory.txt'
    $agentWorktreeInstructions = Join-Path $expanded 'herdr-sandbox\agent-worktree-instructions.md'
    $worktreeDirectoryConfigured = Test-Path -LiteralPath $worktreeDirectoryPath -PathType Leaf
    $agentWorktreeInstructionsAvailable = Test-Path -LiteralPath $agentWorktreeInstructions -PathType Leaf
    if ($worktreeDirectoryConfigured -ne $agentWorktreeInstructionsAvailable) {
        throw 'Agent worktree instructions do not match the worktree-directory contract.'
    }
    if ($worktreeDirectoryConfigured) {
        $worktreeDirectory = [IO.File]::ReadAllText($worktreeDirectoryPath).Trim()
        if ($worktreeDirectory -cne 'C:\Worktrees' -or
            -not (Test-Path -LiteralPath $worktreeDirectory -PathType Container)) {
            throw 'Herdr worktree directory metadata is invalid during configuration sync.'
        }
        [Console]::Error.WriteLine('[config-sync] apply-agent-worktree-instructions')
        $agentWorktreeDestinations = @()
        if ([bool]$agentSync.opencode) {
            $agentWorktreeDestinations += (Join-Path $env:USERPROFILE '.config\opencode\AGENTS.md')
        }
        if ([bool]$agentSync.claudeCode) {
            $agentWorktreeDestinations += (Join-Path $env:USERPROFILE '.claude\CLAUDE.md')
        }
        if ([bool]$agentSync.codex) {
            $agentWorktreeDestinations += (Join-Path $env:USERPROFILE '.codex\AGENTS.md')
        }
        if ([bool]$agentSync.githubCopilot) {
            $agentWorktreeDestinations += (Join-Path $env:USERPROFILE '.copilot\instructions\herdr-sandbox-worktrees.instructions.md')
        }
        if ([bool]$agentSync.pi) {
            $agentWorktreeDestinations += (Join-Path $env:USERPROFILE '.pi\agent\AGENTS.md')
        }
        if ($agentWorktreeDestinations.Count -gt 0) {
            Set-ManagedAgentWorktreeInstructions -Source $agentWorktreeInstructions -Destinations $agentWorktreeDestinations
        }
    }

    if ($gitEnabled) {
        [Console]::Error.WriteLine('[config-sync] apply-git')
        $gitConfig = Join-Path $expanded 'git\.gitconfig'
        if (Test-Path -LiteralPath $gitConfig -PathType Leaf) {
            Copy-VerifiedConfigurationFile -Source $gitConfig -Destination (Join-Path $env:USERPROFILE '.gitconfig')
        }
        $gitConfigSource = Join-Path $expanded 'git\config'
        if (Test-Path -LiteralPath $gitConfigSource -PathType Container) {
            $gitConfigDestination = Join-Path $env:USERPROFILE '.config\git'
            Copy-VerifiedConfigurationTree -Source $gitConfigSource -Destination $gitConfigDestination
        }
        foreach ($name in @('.gitignore_global', '.gitattributes')) {
            $source = Join-Path (Join-Path $expanded 'git') $name
            if (Test-Path -LiteralPath $source -PathType Leaf) {
                Copy-VerifiedConfigurationFile -Source $source -Destination (Join-Path $env:USERPROFILE $name)
            }
        }

        [Console]::Error.WriteLine('[config-sync] apply-git-safe-directories')
        $workspaceManifestPath = Join-Path $expanded 'herdr-sandbox\workspaces.json'
        if (-not (Test-Path -LiteralPath $workspaceManifestPath -PathType Leaf)) {
            throw 'Workspace manifest is missing from the configuration archive.'
        }
        $workspaceManifest = [IO.File]::ReadAllText($workspaceManifestPath) | ConvertFrom-Json
        $manifestProperties = @($workspaceManifest.PSObject.Properties.Name | Sort-Object)
        $workspaceEntries = @($workspaceManifest.workspaces)
        if (($manifestProperties -join '|') -cne 'activeWorkspace|schemaVersion|workspaces' -or
            [int]$workspaceManifest.schemaVersion -ne 1 -or $workspaceEntries.Count -eq 0 -or
            $workspaceEntries.Count -gt 16) {
            throw 'Workspace manifest has an unsupported configuration-sync contract.'
        }
        $safeDirectories = @()
        $seenWorkspaces = @{}
        $activeWorkspace = [string]$workspaceManifest.activeWorkspace
        $activeWorkspaceMatches = 0
        foreach ($workspace in $workspaceEntries) {
            $entryProperties = @($workspace.PSObject.Properties.Name | Sort-Object)
            $workspaceName = [string]$workspace.name
            $workspaceDirectory = [string]$workspace.directory
            $expectedDirectory = Join-Path 'C:\Workspaces' $workspaceName
            if (($entryProperties -join '|') -cne 'directory|name' -or
                $workspaceName -notmatch '^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$' -or
                $workspaceDirectory -cne $expectedDirectory -or
                -not (Test-Path -LiteralPath $workspaceDirectory -PathType Container) -or
                $seenWorkspaces.ContainsKey($workspaceName.ToLowerInvariant())) {
                throw "Workspace manifest entry is invalid during configuration sync: $workspaceName"
            }
            $seenWorkspaces[$workspaceName.ToLowerInvariant()] = $true
            if ($workspaceDirectory -ceq $activeWorkspace) { $activeWorkspaceMatches += 1 }
            $safeDirectories += $workspaceDirectory.Replace('\', '/')
        }
        if ($activeWorkspaceMatches -ne 1) {
            throw 'Workspace manifest active workspace is invalid during configuration sync.'
        }
        if ($worktreeDirectoryConfigured) {
            $safeDirectories += 'C:/Worktrees/*'
        }
        $gitCommand = (Get-Command 'git.exe' -CommandType Application -ErrorAction Stop | Select-Object -First 1).Source
        $null = & $gitCommand 'config' '--global' '--replace-all' 'safe.directory' $safeDirectories[0]
        if ($LASTEXITCODE -ne 0) { throw "Git safe-directory reset failed with exit code $LASTEXITCODE." }
        foreach ($safeDirectory in @($safeDirectories | Select-Object -Skip 1)) {
            $null = & $gitCommand 'config' '--global' '--add' 'safe.directory' $safeDirectory
            if ($LASTEXITCODE -ne 0) { throw "Git safe-directory addition failed with exit code $LASTEXITCODE." }
        }
        $verifiedSafeDirectories = @(& $gitCommand 'config' '--global' '--get-all' 'safe.directory')
        if ($LASTEXITCODE -ne 0 -or ($verifiedSafeDirectories -join '|') -cne ($safeDirectories -join '|')) {
            throw 'Git safe-directory verification failed after configuration copy.'
        }
    }

    if ($githubCLIEnabled) {
    [Console]::Error.WriteLine('[config-sync] apply-github-cli')
    $githubCLISource = Join-Path $expanded 'github-cli'
    $githubCLIDestination = Join-Path $env:APPDATA 'GitHub CLI'
    foreach ($name in @('config.yml', 'hosts.yml')) {
        $source = Join-Path $githubCLISource $name
        if (Test-Path -LiteralPath $source -PathType Leaf) {
            Copy-VerifiedConfigurationFile -Source $source -Destination (Join-Path $githubCLIDestination $name)
        }
    }
    foreach ($name in @('GH_TOKEN', 'GITHUB_TOKEN', 'GH_ENTERPRISE_TOKEN', 'GITHUB_ENTERPRISE_TOKEN')) {
        Remove-Item -LiteralPath ("Env:" + $name) -ErrorAction SilentlyContinue
    }
    $env:GH_CONFIG_DIR = $githubCLIDestination
    $env:GH_PROMPT_DISABLED = '1'
    $env:NO_COLOR = '1'
    $githubCLICommand = Get-Command 'gh.exe' -CommandType Application -ErrorAction SilentlyContinue | Select-Object -First 1
    if ($null -eq $githubCLICommand) {
        throw 'Guest GitHub CLI command is unavailable: gh.exe'
    }
    $script:GitHubCLICommand = [string]$githubCLICommand.Source
    $githubAuthenticationPath = Join-Path $githubCLISource 'authentication.json'
    if (-not (Test-Path -LiteralPath $githubAuthenticationPath -PathType Leaf)) {
        throw 'GitHub CLI authentication input is missing.'
    }
    $githubAuthentication = [IO.File]::ReadAllText($githubAuthenticationPath) | ConvertFrom-Json
    $authenticationProperties = @($githubAuthentication.PSObject.Properties.Name | Sort-Object)
    if (($authenticationProperties -join '|') -cne 'accounts|schemaVersion' -or
        [int]$githubAuthentication.schemaVersion -ne 1) {
        throw 'GitHub CLI authentication input has an unsupported contract.'
    }
    $githubAccounts = @($githubAuthentication.accounts)
    if ($githubAccounts.Count -gt 32) {
        throw 'GitHub CLI authentication input contains too many accounts.'
    }
    if ($githubAccounts.Count -gt 0 -and -not $gitEnabled) {
        throw 'GitHub CLI authenticated-account import requires the Git.Git package.'
    }
    [Console]::Error.WriteLine('[config-sync] apply-github-authentication')
    $githubAccountIdentities = @{}
    $githubAccountHosts = @{}
    foreach ($account in $githubAccounts) {
        $accountProperties = @($account.PSObject.Properties.Name | Sort-Object)
        $hostname = [string]$account.hostname
        $login = [string]$account.login
        $protocol = [string]$account.gitProtocol
        $token = [string]$account.token
        if (($accountProperties -join '|') -cne 'active|gitProtocol|hostname|login|token' -or
            [Uri]::CheckHostName($hostname) -eq [UriHostNameType]::Unknown -or
            [string]::IsNullOrWhiteSpace($login) -or $login.Length -gt 256 -or $login -match '[\x00\r\n]' -or
            $protocol -notin @('https', 'ssh') -or [string]::IsNullOrWhiteSpace($token) -or
            $token.Length -gt 16384 -or $token -match '[\x00\r\n]') {
            throw 'GitHub CLI authentication input contains invalid account data.'
        }
        $identity = ($hostname + '/' + $login).ToLowerInvariant()
        if ($githubAccountIdentities.ContainsKey($identity)) {
            throw 'GitHub CLI authentication input contains duplicate account metadata.'
        }
        $githubAccountIdentities[$identity] = $true
        $githubAccountHosts[$hostname] = $true
        $loginArguments = @('auth', 'login', '--hostname', $hostname, '--git-protocol', $protocol,
            '--with-token', '--insecure-storage', '--skip-ssh-key')
        $loginOutput = @(Invoke-GuestGitHubCLI -Role 'GitHub CLI authentication import' -Arguments $loginArguments -InputText $token -UseStandardInput)
        $account.token = ''
    }
    foreach ($account in @($githubAccounts | Where-Object { [bool]$_.active })) {
        $switchOutput = @(Invoke-GuestGitHubCLI -Role 'GitHub CLI active-account selection' -Arguments @('auth', 'switch', '--hostname', [string]$account.hostname, '--user', [string]$account.login))
    }
    foreach ($hostname in @($githubAccountHosts.Keys | Sort-Object)) {
        $setupGitOutput = @(Invoke-GuestGitHubCLI -Role 'GitHub CLI Git credential-helper setup' -Arguments @('auth', 'setup-git', '--hostname', [string]$hostname))
        $credentialHelperKey = "credential.https://$hostname.helper"
        $credentialHelpers = @(& $gitCommand 'config' '--global' '--get-all' $credentialHelperKey)
        if ($LASTEXITCODE -ne 0 -or $credentialHelpers.Count -ne 2 -or
            [string]$credentialHelpers[0] -cne '') {
            throw "GitHub CLI Git credential helper is missing for $hostname."
        }
        $credentialHelper = [string]$credentialHelpers[-1]
        $expectedCredentialHelper = "!'" + $script:GitHubCLICommand + "' auth git-credential"
        if (-not [string]::Equals($credentialHelper, $expectedCredentialHelper, [StringComparison]::Ordinal)) {
            throw "GitHub CLI Git credential helper is invalid for $hostname."
        }
    }
    $githubAuthenticationVerified = $true
    if ($githubAccounts.Count -gt 0) {
        $statusOutput = @(Invoke-GuestGitHubCLI -Role 'GitHub CLI authentication verification' -Arguments @('auth', 'status', '--json', 'hosts'))
        $githubStatus = (($statusOutput | ForEach-Object { [string]$_ }) -join [Environment]::NewLine) | ConvertFrom-Json
        foreach ($account in $githubAccounts) {
            $hostProperties = @($githubStatus.hosts.PSObject.Properties | Where-Object { $_.Name -ceq [string]$account.hostname })
            if ($hostProperties.Count -ne 1) {
                throw 'GitHub CLI authentication verification is missing one expected host.'
            }
            $matches = @($hostProperties[0].Value | Where-Object { [string]$_.login -ceq [string]$account.login })
            if ($matches.Count -ne 1 -or [string]$matches[0].state -cne 'success' -or
                [bool]$matches[0].active -ne [bool]$account.active -or
                -not [string]::Equals([string]$matches[0].tokenSource, (Join-Path $githubCLIDestination 'hosts.yml'), [StringComparison]::OrdinalIgnoreCase)) {
                throw 'GitHub CLI authentication verification failed for one expected account.'
            }
        }
    }
    Remove-Item -LiteralPath $githubAuthenticationPath -Force
    }

    $tradingViewAuthenticationPath = Join-Path $expanded 'tradingview\authentication.json'
    if (Test-Path -LiteralPath $tradingViewAuthenticationPath -PathType Leaf) {
    [Console]::Error.WriteLine('[config-sync] apply-tradingview-authentication')
    $tradingViewAuthenticationFile = Get-Item -LiteralPath $tradingViewAuthenticationPath -Force
    if (($tradingViewAuthenticationFile.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0 -or
        $tradingViewAuthenticationFile.Length -le 0 -or $tradingViewAuthenticationFile.Length -gt 65536) {
        throw 'TradingView authentication input is not one bounded regular file.'
    }
    try {
        $tradingViewAuthentication = [IO.File]::ReadAllText($tradingViewAuthenticationPath) | ConvertFrom-Json
    } catch {
        throw 'TradingView authentication input is not valid JSON.'
    }
    $tradingViewAuthenticationProperties = @($tradingViewAuthentication.PSObject.Properties.Name | Sort-Object)
    if (($tradingViewAuthenticationProperties -join '|') -cne 'cookies|schemaVersion' -or
        $tradingViewAuthentication.schemaVersion -isnot [int] -or
        [int]$tradingViewAuthentication.schemaVersion -ne 1 -or
        $tradingViewAuthentication.cookies -isnot [array]) {
        throw 'TradingView authentication input has an unsupported contract.'
    }
    $tradingViewCookies = @($tradingViewAuthentication.cookies)
    if ($tradingViewCookies.Count -gt 4) {
        throw 'TradingView authentication input contains too many cookies.'
    }
    $tradingViewCookieIdentities = @{}
    $expectedTradingViewCookieProperties = @(
        'creationUtc', 'hostKey', 'topFrameSiteKey', 'name', 'value', 'path', 'expiresUtc',
        'secure', 'httpOnly', 'lastAccessUtc', 'hasExpires', 'persistent', 'priority', 'sameSite',
        'sourceScheme', 'sourcePort', 'lastUpdateUtc', 'sourceType', 'crossSiteAncestor'
    ) | Sort-Object
    foreach ($cookie in $tradingViewCookies) {
        $cookieProperties = @($cookie.PSObject.Properties.Name | Sort-Object)
        if (($cookieProperties -join '|') -cne ($expectedTradingViewCookieProperties -join '|') -or
            $cookie.hostKey -isnot [string] -or -not (Test-TradingViewCookieHost -HostKey ([string]$cookie.hostKey)) -or
            $cookie.topFrameSiteKey -isnot [string] -or [string]$cookie.topFrameSiteKey -cne '' -or
            $cookie.name -isnot [string] -or [string]$cookie.name -cne 'sessionid' -or
            $cookie.value -isnot [string] -or -not (Test-TradingViewCookieValue -Value ([string]$cookie.value)) -or
            $cookie.path -isnot [string] -or [string]::IsNullOrEmpty([string]$cookie.path) -or
            -not ([string]$cookie.path).StartsWith('/', [StringComparison]::Ordinal) -or
            [Text.Encoding]::UTF8.GetByteCount([string]$cookie.path) -gt 1024 -or [string]$cookie.path -match '[\x00\r\n]' -or
            $cookie.secure -isnot [bool] -or $cookie.httpOnly -isnot [bool] -or
            $cookie.hasExpires -isnot [bool] -or $cookie.persistent -isnot [bool] -or
            $cookie.crossSiteAncestor -isnot [bool] -or [bool]$cookie.crossSiteAncestor -or
            -not (Test-TradingViewJSONInteger $cookie.creationUtc) -or [long]$cookie.creationUtc -lt 0 -or
            -not (Test-TradingViewJSONInteger $cookie.expiresUtc) -or [long]$cookie.expiresUtc -lt 0 -or
            -not (Test-TradingViewJSONInteger $cookie.lastAccessUtc) -or [long]$cookie.lastAccessUtc -lt 0 -or
            -not (Test-TradingViewJSONInteger $cookie.lastUpdateUtc) -or [long]$cookie.lastUpdateUtc -lt 0 -or
            -not (Test-TradingViewJSONInteger $cookie.priority) -or [int]$cookie.priority -lt 0 -or [int]$cookie.priority -gt 2 -or
            -not (Test-TradingViewJSONInteger $cookie.sameSite) -or [int]$cookie.sameSite -lt -1 -or [int]$cookie.sameSite -gt 2 -or
            -not (Test-TradingViewJSONInteger $cookie.sourceScheme) -or [int]$cookie.sourceScheme -lt 0 -or [int]$cookie.sourceScheme -gt 2 -or
            -not (Test-TradingViewJSONInteger $cookie.sourcePort) -or [int]$cookie.sourcePort -lt -1 -or [int]$cookie.sourcePort -gt 65535 -or
            -not (Test-TradingViewJSONInteger $cookie.sourceType) -or [int]$cookie.sourceType -lt 0 -or [int]$cookie.sourceType -gt 3 -or
            [bool]$cookie.hasExpires -ne [bool]$cookie.persistent -or
            ([bool]$cookie.hasExpires -and [long]$cookie.expiresUtc -eq 0)) {
            throw 'TradingView authentication input contains invalid cookie data.'
        }
        $identity = ([string]$cookie.hostKey).ToLowerInvariant() + "`0" + [string]$cookie.topFrameSiteKey + "`0" +
            [string]$cookie.name + "`0" + [string]$cookie.path + "`0" + [int]$cookie.sourceScheme + "`0" +
            [int]$cookie.sourcePort + "`0" + [bool]$cookie.crossSiteAncestor
        if ($tradingViewCookieIdentities.ContainsKey($identity)) {
            throw 'TradingView authentication input contains duplicate cookies.'
        }
        $tradingViewCookieIdentities[$identity] = $true
    }
    if ($tradingViewCookies.Count -gt 0) {
        $runningTradingView = @(Get-Process -Name 'TradingView' -ErrorAction SilentlyContinue)
        if ($runningTradingView.Count -ne 0) {
            throw 'TradingView Desktop is running in the guest; close it before reapplying authentication.'
        }
        $tradingViewAdapterPath = Join-Path $expanded 'herdr-sandbox\tradingview-cookie-sync.cs'
        if (-not (Test-Path -LiteralPath $tradingViewAdapterPath -PathType Leaf)) {
            throw 'TradingView cookie sync adapter is missing.'
        }
        $null = Add-Type -Path $tradingViewAdapterPath
        $tradingViewRecordType = 'HerdrSandbox.TradingViewCookieRecord' -as [type]
        if ($null -eq $tradingViewRecordType) {
            throw 'TradingView cookie sync adapter type is unavailable.'
        }
        $typedTradingViewCookies = [Array]::CreateInstance($tradingViewRecordType, $tradingViewCookies.Count)
        for ($index = 0; $index -lt $tradingViewCookies.Count; $index++) {
            $cookie = $tradingViewCookies[$index]
            $record = New-Object 'HerdrSandbox.TradingViewCookieRecord'
            $record.CreationUtc = [long]$cookie.creationUtc
            $record.HostKey = [string]$cookie.hostKey
            $record.TopFrameSiteKey = [string]$cookie.topFrameSiteKey
            $record.Name = [string]$cookie.name
            $record.Value = [string]$cookie.value
            $record.Path = [string]$cookie.path
            $record.ExpiresUtc = [long]$cookie.expiresUtc
            $record.Secure = [bool]$cookie.secure
            $record.HttpOnly = [bool]$cookie.httpOnly
            $record.LastAccessUtc = [long]$cookie.lastAccessUtc
            $record.HasExpires = [bool]$cookie.hasExpires
            $record.Persistent = [bool]$cookie.persistent
            $record.Priority = [int]$cookie.priority
            $record.SameSite = [int]$cookie.sameSite
            $record.SourceScheme = [int]$cookie.sourceScheme
            $record.SourcePort = [int]$cookie.sourcePort
            $record.LastUpdateUtc = [long]$cookie.lastUpdateUtc
            $record.SourceType = [int]$cookie.sourceType
            $record.CrossSiteAncestor = [bool]$cookie.crossSiteAncestor
            $typedTradingViewCookies.SetValue($record, $index)
        }
        $tradingViewCookieDatabase = Join-Path $env:APPDATA 'TradingView\Network\Cookies'
        Assert-ConfigurationDestinationPath -Path $tradingViewCookieDatabase
        Remove-Item -LiteralPath $tradingViewAuthenticationPath -Force
        try {
            $tradingViewAuthenticatedCookies = [HerdrSandbox.TradingViewCookieSync]::Import(
                $tradingViewCookieDatabase, $typedTradingViewCookies)
        } finally {
            foreach ($record in $typedTradingViewCookies) { $record.Value = [string]::Empty }
            foreach ($cookie in $tradingViewCookies) { $cookie.value = [string]::Empty }
        }
    } else {
        Remove-Item -LiteralPath $tradingViewAuthenticationPath -Force
    }
    if (Test-Path -LiteralPath $tradingViewAuthenticationPath) {
        throw 'TradingView authentication input cleanup failed.'
    }
    $tradingViewAuthenticationVerified = $true
    }

    [Console]::Error.WriteLine('[config-sync] apply-herdr')
    $herdrConfigSource = Join-Path $expanded 'herdr\config.toml'
    $herdrConfigDestination = Join-Path $env:APPDATA 'herdr\config.toml'
    Set-AtomicConfigurationFile -Source $herdrConfigSource -Destination $herdrConfigDestination

    if ($starshipEnabled) {
    [Console]::Error.WriteLine('[config-sync] apply-starship')
    $starshipPresetPath = Join-Path $expanded 'starship\preset.txt'
    if (-not (Test-Path -LiteralPath $starshipPresetPath -PathType Leaf)) {
        throw 'Starship preset metadata is missing from the configuration archive.'
    }
    $starshipPreset = [IO.File]::ReadAllText($starshipPresetPath).Trim()
    switch ($starshipPreset) {
        'pastel-powerline' {
            $upstreamStarshipPreset = 'pastel-powerline'
            $useCatppuccinLatte = $false
        }
        'catppuccin-powerline-latte' {
            $upstreamStarshipPreset = 'catppuccin-powerline'
            $useCatppuccinLatte = $true
        }
        default { throw "Unsupported Starship preset metadata: $starshipPreset" }
    }
    $starshipCommand = Get-Command 'starship.exe' -CommandType Application -ErrorAction SilentlyContinue | Select-Object -First 1
    if ($null -eq $starshipCommand) {
        throw 'Guest Starship command is unavailable: starship.exe'
    }
    $starshipConfigDirectory = Join-Path $env:USERPROFILE '.config'
    $starshipConfigPath = Join-Path $starshipConfigDirectory 'starship.toml'
    New-Item -ItemType Directory -Path $starshipConfigDirectory -Force | Out-Null
    $previousErrorActionPreference = $ErrorActionPreference
    try {
        $ErrorActionPreference = 'Continue'
        $starshipOutput = @(& $starshipCommand.Source 'preset' $upstreamStarshipPreset '-o' $starshipConfigPath '--force' 2>&1)
        $starshipExitCode = $LASTEXITCODE
    } finally {
        $ErrorActionPreference = $previousErrorActionPreference
    }
    if ($starshipExitCode -ne 0) {
        $starshipDetail = ($starshipOutput -join [Environment]::NewLine).Trim()
        if ($starshipDetail.Length -gt 1600) { $starshipDetail = $starshipDetail.Substring($starshipDetail.Length - 1600) }
        throw "Guest Starship preset generation failed with exit code $starshipExitCode. $starshipDetail"
    }
    $starshipConfig = Get-Item -LiteralPath $starshipConfigPath -ErrorAction Stop
    if ($starshipConfig.Length -le 0 -or $starshipConfig.Length -gt 1048576) {
        throw "Guest Starship configuration has an invalid size: $($starshipConfig.Length)"
    }
    if ($useCatppuccinLatte) {
        $starshipConfigText = [IO.File]::ReadAllText($starshipConfigPath)
        $mochaSelector = "palette = 'catppuccin_mocha'"
        $mochaIndex = $starshipConfigText.IndexOf($mochaSelector, [StringComparison]::Ordinal)
        if ($mochaIndex -lt 0 -or $mochaIndex -ne $starshipConfigText.LastIndexOf($mochaSelector, [StringComparison]::Ordinal)) {
            throw 'Catppuccin Powerline preset does not contain exactly one default Mocha palette selector.'
        }
        $latteSelector = "palette = 'catppuccin_latte'"
        $starshipConfigText = $starshipConfigText.Substring(0, $mochaIndex) + $latteSelector +
            $starshipConfigText.Substring($mochaIndex + $mochaSelector.Length)
        [IO.File]::WriteAllText($starshipConfigPath, $starshipConfigText, (New-Object Text.UTF8Encoding($false)))
    }
    $verifiedStarshipConfig = [IO.File]::ReadAllText($starshipConfigPath)
    if ($useCatppuccinLatte -and ($verifiedStarshipConfig -notmatch "(?m)^palette = 'catppuccin_latte'\r?$" -or
        $verifiedStarshipConfig -match "(?m)^palette = 'catppuccin_mocha'\r?$")) {
        throw 'Catppuccin Latte palette verification failed.'
    }
    $previousStarshipConfig = [Environment]::GetEnvironmentVariable('STARSHIP_CONFIG', 'Process')
    try {
        $env:STARSHIP_CONFIG = $starshipConfigPath
        $previousErrorActionPreference = $ErrorActionPreference
        try {
            $ErrorActionPreference = 'Continue'
            $starshipValidationOutput = @(& $starshipCommand.Source 'prompt' 2>&1)
            $starshipValidationExitCode = $LASTEXITCODE
        } finally {
            $ErrorActionPreference = $previousErrorActionPreference
        }
    } finally {
        if ($null -eq $previousStarshipConfig) {
            Remove-Item Env:STARSHIP_CONFIG -ErrorAction SilentlyContinue
        } else {
            $env:STARSHIP_CONFIG = $previousStarshipConfig
        }
    }
    if ($starshipValidationExitCode -ne 0) {
        throw "Guest Starship configuration validation failed with exit code $starshipValidationExitCode."
    }
    $starshipConfigured = $true
    }

    if ($windowsTerminalEnabled) {
    [Console]::Error.WriteLine('[config-sync] apply-windows-terminal')
    $terminalSource = Join-Path $expanded 'windows-terminal'
    $terminalEditionPath = Join-Path $terminalSource 'edition.txt'
    if (-not (Test-Path -LiteralPath $terminalEditionPath -PathType Leaf)) {
        throw 'Windows Terminal edition metadata is missing from the configuration archive.'
    }
    $terminalEdition = [IO.File]::ReadAllText($terminalEditionPath).Trim()
    switch ($terminalEdition) {
        'stable' { $terminalPackageFamily = 'Microsoft.WindowsTerminal_8wekyb3d8bbwe' }
        'preview' { $terminalPackageFamily = 'Microsoft.WindowsTerminalPreview_8wekyb3d8bbwe' }
        default { throw "Unsupported Windows Terminal edition metadata: $terminalEdition" }
    }
    $terminalPackageRoot = Join-Path $env:LOCALAPPDATA (Join-Path 'Packages' $terminalPackageFamily)
    if (-not (Test-Path -LiteralPath $terminalPackageRoot -PathType Container)) {
        throw "Selected Windows Terminal package is not registered in the guest: $terminalPackageFamily"
    }
    $settingsSource = Join-Path $terminalSource 'settings.json'
    if (Test-Path -LiteralPath $settingsSource -PathType Leaf) {
        $terminalLocalState = Join-Path $terminalPackageRoot 'LocalState'
        Copy-VerifiedConfigurationFile -Source $settingsSource -Destination (Join-Path $terminalLocalState 'settings.json')
    }
    $fragmentsSource = Join-Path $terminalSource 'Fragments'
    if (Test-Path -LiteralPath $fragmentsSource -PathType Container) {
        $fragmentsDestination = Join-Path $env:LOCALAPPDATA 'Microsoft\Windows Terminal\Fragments'
        Copy-VerifiedConfigurationTree -Source $fragmentsSource -Destination $fragmentsDestination
    }
    }
Write-Output ([ordered]@{
    schemaVersion = 8
    archiveSha256 = $digest
    copiedFiles = $script:CopiedConfigurationFiles
    openCodePermissionVerified = $openCodePermissionVerified
    windowsTerminalEdition = $terminalEdition
    starshipPreset = $starshipPreset
    starshipConfigured = $starshipConfigured
    githubAuthenticatedAccounts = $githubAccounts.Count
    githubAuthenticationVerified = $githubAuthenticationVerified
    herdrConfigurationPublished = $true
    tradingViewAuthenticatedCookies = $tradingViewAuthenticatedCookies
    tradingViewAuthenticationVerified = $tradingViewAuthenticationVerified
} | ConvertTo-Json -Compress)
