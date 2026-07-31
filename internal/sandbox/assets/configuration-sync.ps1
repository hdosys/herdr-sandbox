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
    $requiredPermissions = @(
        'read', 'edit', 'glob', 'grep', 'list', 'bash', 'task', 'external_directory',
        'todowrite', 'question', 'webfetch', 'websearch', 'lsp', 'doom_loop', 'skill',
        'plan_enter', 'plan_exit'
    )
    $resolvedConfig = Invoke-OpenCodeJSON -Role 'OpenCode effective configuration inspection' -Arguments @('debug', 'config')
    foreach ($permissionName in @('*') + $requiredPermissions) {
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
    if (($agentSyncProperties -join '|') -cne 'claudeCode|codex|githubCopilot|opencode|pi|schemaVersion' -or
        $agentSync.schemaVersion -isnot [int] -or [int]$agentSync.schemaVersion -ne 1 -or
        $agentSync.opencode -isnot [bool] -or $agentSync.claudeCode -isnot [bool] -or
        $agentSync.codex -isnot [bool] -or $agentSync.githubCopilot -isnot [bool] -or
        $agentSync.pi -isnot [bool]) {
        throw 'Coding-agent sync manifest has an unsupported contract.'
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

    if ([bool]$agentSync.opencode) {
        [Console]::Error.WriteLine('[config-sync] apply-opencode')
        $openCodeSource = Join-Path $expanded 'opencode'
        $openCodeDestination = Join-Path $env:USERPROFILE '.config\opencode'
        Sync-VerifiedConfigurationRoot -Source $openCodeSource -Destination $openCodeDestination
        Sync-OptionalConfigurationFile -Source (Join-Path $expanded 'opencode-auth\auth.json') -Destination (Join-Path $env:USERPROFILE '.local\share\opencode\auth.json')
    }

    if ([bool]$agentSync.claudeCode) {
        [Console]::Error.WriteLine('[config-sync] apply-claude-code')
        $claudeDestination = Join-Path $env:USERPROFILE '.claude'
        Sync-VerifiedConfigurationRoot -Source (Join-Path $expanded 'claude-code') -Destination $claudeDestination
        Sync-OptionalConfigurationFile -Source (Join-Path $expanded 'claude-code-auth\.credentials.json') -Destination (Join-Path $claudeDestination '.credentials.json')
        Sync-ClaudeCodeUserState -Source (Join-Path $expanded 'claude-code-state\.claude.json') -Destination (Join-Path $env:USERPROFILE '.claude.json')
    }

    if ([bool]$agentSync.codex) {
        [Console]::Error.WriteLine('[config-sync] apply-codex')
        $codexDestination = Join-Path $env:USERPROFILE '.codex'
        Sync-VerifiedConfigurationRoot -Source (Join-Path $expanded 'codex') -Destination $codexDestination
        Sync-OptionalConfigurationFile -Source (Join-Path $expanded 'codex-auth\auth.json') -Destination (Join-Path $codexDestination 'auth.json')
        Sync-OptionalConfigurationFile -Source (Join-Path $expanded 'codex-auth\.credentials.json') -Destination (Join-Path $codexDestination '.credentials.json')
    }

    if ([bool]$agentSync.githubCopilot) {
        [Console]::Error.WriteLine('[config-sync] apply-github-copilot')
        $copilotDestination = Join-Path $env:USERPROFILE '.copilot'
        Sync-VerifiedConfigurationRoot -Source (Join-Path $expanded 'github-copilot') -Destination $copilotDestination
    }

    if ([bool]$agentSync.pi) {
        [Console]::Error.WriteLine('[config-sync] apply-pi')
        $piDestination = Join-Path $env:USERPROFILE '.pi\agent'
        Sync-VerifiedConfigurationRoot -Source (Join-Path $expanded 'pi') -Destination $piDestination
        Sync-OptionalConfigurationFile -Source (Join-Path $expanded 'pi-auth\auth.json') -Destination (Join-Path $piDestination 'auth.json')
    }

    if ([bool]$agentSync.codex -or [bool]$agentSync.githubCopilot -or [bool]$agentSync.pi) {
        [Console]::Error.WriteLine('[config-sync] apply-shared-agent-skills')
        $sharedSkillsDestination = Join-Path $env:USERPROFILE '.agents\skills'
        $sharedSkillsSource = Join-Path $expanded 'shared-agent-skills'
        if (Test-Path -LiteralPath $sharedSkillsSource -PathType Container) {
            Copy-VerifiedConfigurationTree -Source $sharedSkillsSource -Destination $sharedSkillsDestination
        }
    }

    if ($openCodeEnabled) {
        $openCodeCommand = Get-Command 'opencode.exe' -CommandType Application -ErrorAction Stop | Select-Object -First 1
        $script:OpenCodeCommand = [string]$openCodeCommand.Source
        Assert-OpenCodeAllowAll
        $openCodePermissionVerified = $true
    }

    if ($gitEnabled) {
        [Console]::Error.WriteLine('[config-sync] apply-git')
        $gitConfig = Join-Path $expanded 'git\.gitconfig'
        Copy-VerifiedConfigurationFile -Source $gitConfig -Destination (Join-Path $env:USERPROFILE '.gitconfig')
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
    [Console]::Error.WriteLine('[config-sync] apply-github-authentication')
    $githubAccountIdentities = @{}
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
        $loginArguments = @('auth', 'login', '--hostname', $hostname, '--git-protocol', $protocol,
            '--with-token', '--skip-ssh-key')
        $loginOutput = @(Invoke-GuestGitHubCLI -Role 'GitHub CLI authentication import' -Arguments $loginArguments -InputText $token -UseStandardInput)
        $account.token = ''
    }
    foreach ($account in @($githubAccounts | Where-Object { [bool]$_.active })) {
        $switchOutput = @(Invoke-GuestGitHubCLI -Role 'GitHub CLI active-account selection' -Arguments @('auth', 'switch', '--hostname', [string]$account.hostname, '--user', [string]$account.login))
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
                [bool]$matches[0].active -ne [bool]$account.active) {
                throw 'GitHub CLI authentication verification failed for one expected account.'
            }
        }
    }
    Remove-Item -LiteralPath $githubAuthenticationPath -Force
    }

    [Console]::Error.WriteLine('[config-sync] apply-herdr')
    $herdrConfigSource = Join-Path $expanded 'herdr\config.toml'
    $herdrConfigDestination = Join-Path $env:APPDATA 'herdr\config.toml'
    Copy-VerifiedConfigurationFile -Source $herdrConfigSource -Destination $herdrConfigDestination
    $guestHerdrCommand = Get-Command -Name 'herdr.exe' -CommandType Application -ErrorAction Stop | Select-Object -First 1
    $guestHerdr = [string]$guestHerdrCommand.Source
    if ([string]::IsNullOrWhiteSpace($guestHerdr) -or -not (Test-Path -LiteralPath $guestHerdr -PathType Leaf)) {
        throw "Guest PATH did not resolve a Herdr executable: $guestHerdr"
    }
    $previousErrorActionPreference = $ErrorActionPreference
    try {
        $ErrorActionPreference = 'Continue'
        $reloadOutput = @(& $guestHerdr 'server' 'reload-config' 2>&1)
        $reloadExitCode = $LASTEXITCODE
    } finally {
        $ErrorActionPreference = $previousErrorActionPreference
    }
    if ($reloadExitCode -ne 0) {
        $reloadDetail = ($reloadOutput -join [Environment]::NewLine).Trim()
        if ($reloadDetail.Length -gt 1600) { $reloadDetail = $reloadDetail.Substring($reloadDetail.Length - 1600) }
        throw "Guest Herdr configuration reload failed with exit code $reloadExitCode. $reloadDetail"
    }

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
    schemaVersion = 6
    archiveSha256 = $digest
    copiedFiles = $script:CopiedConfigurationFiles
    openCodePermissionVerified = $openCodePermissionVerified
    windowsTerminalEdition = $terminalEdition
    starshipPreset = $starshipPreset
    starshipConfigured = $starshipConfigured
    githubAuthenticatedAccounts = $githubAccounts.Count
    githubAuthenticationVerified = $githubAuthenticationVerified
    herdrConfigurationReloaded = $true
} | ConvertTo-Json -Compress)