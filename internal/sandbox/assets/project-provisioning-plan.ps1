param(
    [Parameter(Mandatory = $true)]
    [string]$UserProvisioningPath,
    [Parameter(Mandatory = $true)]
    [string]$ProjectsDirectory
)

Set-StrictMode -Version 2.0
$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'

if (-not (Test-Path -LiteralPath $ProjectsDirectory -PathType Container)) {
    throw "Project provisioning directory is missing: $ProjectsDirectory"
}

$knownStacks = @{
    'Install-AndroidStack' = 'android'
    'Install-BunStack' = 'bun'
    'Install-CargoNextest' = 'cargo-nextest'
    'Install-CppStack' = 'cpp'
    'Install-DotNetStack' = 'dotnet'
    'Install-GoStack' = 'go'
    'Install-HandyStack' = @('bun', 'handy', 'rust-msvc')
    'Install-HerdrStack' = @('bun', 'cargo-nextest', 'git-sh', 'just', 'python', 'rust-msvc', 'zig')
    'Install-HyperFramesStack' = 'hyperframes'
    'Install-JavaStack' = 'java'
    'Install-Just' = 'just'
    'Install-NodeStack' = 'node'
    'Install-NSISStack' = 'nsis'
    'Install-NushellStack' = 'nushell'
    'Install-PlaywrightCLIStack' = 'playwright-cli'
    'Install-PythonAIStack' = @('python', 'uv')
    'Install-PythonStack' = 'python'
    'Install-RustMSVCStack' = 'rust-msvc'
    'Install-TradingViewStack' = 'tradingview'
    'Install-Uv' = 'uv'
    'Install-ZigStack' = 'zig'
}

function Get-LiteralCommandParameter {
    param(
        [Parameter(Mandatory = $true)]
        [System.Management.Automation.Language.CommandAst]$Command,
        [Parameter(Mandatory = $true)]
        [string[]]$ParameterOrder,
        [Parameter(Mandatory = $true)]
        [string]$ParameterName,
        [Parameter(Mandatory = $true)]
        [string]$Role,
        [switch]$AllowProjectDirectoryVariable,
        [switch]$AllowProjectPlaywrightLockVariable
    )

    $bound = @{}
    $positionals = @()
    $elements = @($Command.CommandElements)
    for ($index = 1; $index -lt $elements.Count; $index += 1) {
        $element = $elements[$index]
        if ($element -is [System.Management.Automation.Language.VariableExpressionAst] -and $element.Splatted) {
            throw "$Role command $($Command.GetCommandName()) must not use splatting."
        }
        if ($element -is [System.Management.Automation.Language.CommandParameterAst]) {
            $canonical = @($ParameterOrder | Where-Object { $_ -ieq $element.ParameterName })
            if ($canonical.Count -ne 1) {
                throw "$Role command $($Command.GetCommandName()) has unsupported parameter -$($element.ParameterName)."
            }
            $name = [string]$canonical[0]
            if ($bound.ContainsKey($name)) {
                throw "$Role command $($Command.GetCommandName()) repeats parameter -$name."
            }
            $argument = $element.Argument
            if ($null -eq $argument) {
                if (($index + 1) -ge $elements.Count -or
                    $elements[$index + 1] -is [System.Management.Automation.Language.CommandParameterAst]) {
                    throw "$Role command $($Command.GetCommandName()) omits the value for -$name."
                }
                $index += 1
                $argument = $elements[$index]
            }
            $bound[$name] = $argument
            continue
        }
        $positionals += $element
    }
    $positionalIndex = 0
    foreach ($name in $ParameterOrder) {
        if ($bound.ContainsKey($name)) { continue }
        if ($positionalIndex -ge $positionals.Count) { break }
        $bound[$name] = $positionals[$positionalIndex]
        $positionalIndex += 1
    }
    if ($positionalIndex -ne $positionals.Count) {
        throw "$Role command $($Command.GetCommandName()) has too many positional arguments."
    }
    if (-not $bound.ContainsKey($ParameterName)) { return '' }
    $value = $bound[$ParameterName]
    if ($value -is [System.Management.Automation.Language.StringConstantExpressionAst]) {
        return [string]$value.Value
    }
    if ($value -is [System.Management.Automation.Language.ExpandableStringExpressionAst] -and
        @($value.NestedExpressions).Count -eq 0) {
        return [string]$value.Value
    }
    if ($AllowProjectDirectoryVariable -and
        $value -is [System.Management.Automation.Language.VariableExpressionAst] -and
        [string]$value.VariablePath.UserPath -ceq 'ProjectDirectory') {
        return '$ProjectDirectory'
    }
    if ($AllowProjectPlaywrightLockVariable -and $Role -ceq 'Project provisioning' -and
        $value -is [System.Management.Automation.Language.VariableExpressionAst] -and
        [string]$value.VariablePath.UserPath -ceq 'projectPlaywrightVersion') {
        return '$projectPlaywrightVersion'
    }
    throw "$Role command $($Command.GetCommandName()) parameter -$ParameterName must be one literal string."
}

function New-ToolRequirement {
    param(
        [Parameter(Mandatory = $true)][string]$Tool,
        [string]$Version = '',
        [string]$Series = '',
        [Parameter(Mandatory = $true)][string]$Source,
        [string]$ProjectDirectory = ''
    )
    return [pscustomobject]@{ tool = $Tool; version = $Version; series = $Series; source = $Source; projectDirectory = $ProjectDirectory }
}

function Get-CommandToolRequirements {
    param(
        [Parameter(Mandatory = $true)]
        [System.Management.Automation.Language.CommandAst]$Command,
        [Parameter(Mandatory = $true)]
        [string]$Role
    )

    $name = $Command.GetCommandName()
    $version = ''
    $series = ''
    switch ($name) {
        'Install-BunStack' {
            $version = Get-LiteralCommandParameter $Command @('Version') 'Version' $Role
            return @(New-ToolRequirement 'Oven-sh.Bun' $version '' 'bun')
        }
        'Install-CargoNextest' {
            $version = Get-LiteralCommandParameter $Command @('Version') 'Version' $Role
            return @(New-ToolRequirement 'nextest.cargo-nextest' $version '' 'cargo-nextest')
        }
        'Install-DotNetStack' {
            $version = Get-LiteralCommandParameter $Command @('Version') 'Version' $Role
            return @(New-ToolRequirement 'Microsoft.DotNet.SDK.10' $version '' 'dotnet')
        }
        'Install-GoStack' {
            $version = Get-LiteralCommandParameter $Command @('Version') 'Version' $Role
            return @(New-ToolRequirement 'GoLang.Go' $version '' 'go')
        }
        'Install-HandyStack' {
            $projectDirectory = Get-LiteralCommandParameter $Command @('ProjectDirectory') 'ProjectDirectory' $Role -AllowProjectDirectoryVariable
            return @(
                (New-ToolRequirement 'Kitware.CMake' '' '' 'handy'),
                (New-ToolRequirement 'KhronosGroup.VulkanSDK' '1.4.309.0' '' 'handy'),
                (New-ToolRequirement 'Microsoft.EdgeWebView2Runtime' '' '' 'handy'),
                (New-ToolRequirement 'Oven-sh.Bun' '' '' 'handy'),
                (New-ToolRequirement 'Rustlang.Rustup' '' '' 'handy'),
                (New-ToolRequirement 'rust-toolchain' '' '' 'handy' $projectDirectory)
            )
        }
        'Install-HerdrStack' {
            $projectDirectory = Get-LiteralCommandParameter $Command @('ProjectDirectory') 'ProjectDirectory' $Role -AllowProjectDirectoryVariable
            return @(
                (New-ToolRequirement 'Python' '' '3.13' 'herdr'),
                (New-ToolRequirement 'zig.zig' '0.15.2' '' 'herdr'),
                (New-ToolRequirement 'Rustlang.Rustup' '' '' 'herdr'),
                (New-ToolRequirement 'rust-toolchain' '' '' 'herdr' $projectDirectory),
                (New-ToolRequirement 'Oven-sh.Bun' '' '' 'herdr'),
                (New-ToolRequirement 'nextest.cargo-nextest' '' '' 'herdr'),
                (New-ToolRequirement 'Casey.Just' '' '' 'herdr')
            )
        }
        'Install-HyperFramesStack' {
            $node = Get-LiteralCommandParameter $Command @('NodeVersion', 'FFmpegVersion', 'Version') 'NodeVersion' $Role
            $ffmpeg = Get-LiteralCommandParameter $Command @('NodeVersion', 'FFmpegVersion', 'Version') 'FFmpegVersion' $Role
            $version = Get-LiteralCommandParameter $Command @('NodeVersion', 'FFmpegVersion', 'Version') 'Version' $Role
            return @(
                (New-ToolRequirement 'OpenJS.NodeJS.LTS' $node '' 'hyperframes'),
                (New-ToolRequirement 'Gyan.FFmpeg' $ffmpeg '' 'hyperframes'),
                (New-ToolRequirement 'hyperframes' $version '' 'hyperframes')
            )
        }
        'Install-JavaStack' {
            $version = Get-LiteralCommandParameter $Command @('Version') 'Version' $Role
            return @(New-ToolRequirement 'Microsoft.OpenJDK.25' $version '' 'java')
        }
        'Install-Just' {
            $version = Get-LiteralCommandParameter $Command @('Version') 'Version' $Role
            return @(New-ToolRequirement 'Casey.Just' $version '' 'just')
        }
        'Install-NodeStack' {
            $version = Get-LiteralCommandParameter $Command @('Version', 'PlaywrightVersion') 'Version' $Role
            $playwright = Get-LiteralCommandParameter $Command @('Version', 'PlaywrightVersion') 'PlaywrightVersion' $Role -AllowProjectPlaywrightLockVariable
            $playwrightSource = 'node'
            if ($playwright -ceq '$projectPlaywrightVersion') {
                $playwright = ''
                $playwrightSource = 'node-project-lock'
            }
            return @(
                (New-ToolRequirement 'OpenJS.NodeJS.LTS' $version '' 'node'),
                (New-ToolRequirement 'playwright' $playwright '' $playwrightSource)
            )
        }
        'Install-NSISStack' {
            $version = Get-LiteralCommandParameter $Command @('Version') 'Version' $Role
            return @(New-ToolRequirement 'NSIS.NSIS' $version '' 'nsis')
        }
        'Install-NushellStack' {
            $version = Get-LiteralCommandParameter $Command @('Version') 'Version' $Role
            return @(New-ToolRequirement 'Nushell.Nushell' $version '' 'nushell')
        }
        'Install-PlaywrightCLIStack' {
            $node = Get-LiteralCommandParameter $Command @('NodeVersion', 'Version') 'NodeVersion' $Role
            $version = Get-LiteralCommandParameter $Command @('NodeVersion', 'Version') 'Version' $Role
            if ([string]::IsNullOrWhiteSpace($version)) { $version = '0.1.17' }
            return @(
                (New-ToolRequirement 'OpenJS.NodeJS.LTS' $node '' 'playwright-cli'),
                (New-ToolRequirement '@playwright/cli' $version '' 'playwright-cli')
            )
        }
        'Install-PythonAIStack' {
            return @(
                (New-ToolRequirement 'Python' '' '3.13' 'python-ai'),
                (New-ToolRequirement 'astral-sh.uv' '' '' 'python-ai')
            )
        }
        'Install-PythonStack' {
            $series = Get-LiteralCommandParameter $Command @('Series', 'Version') 'Series' $Role
            $version = Get-LiteralCommandParameter $Command @('Series', 'Version') 'Version' $Role
            return @(New-ToolRequirement 'Python' $version $series 'python')
        }
        'Install-RustMSVCStack' {
            $projectDirectory = Get-LiteralCommandParameter $Command @('ProjectDirectory', 'Toolchain') 'ProjectDirectory' $Role -AllowProjectDirectoryVariable
            $version = Get-LiteralCommandParameter $Command @('ProjectDirectory', 'Toolchain') 'Toolchain' $Role
            return @(
                (New-ToolRequirement 'Rustlang.Rustup' '' '' 'rust-msvc'),
                (New-ToolRequirement 'rust-toolchain' $version '' 'rust-msvc' $projectDirectory)
            )
        }
        'Install-TradingViewStack' {
            $node = Get-LiteralCommandParameter $Command @('NodeVersion', 'TVControlVersion', 'DesktopVersion') 'NodeVersion' $Role
            $control = Get-LiteralCommandParameter $Command @('NodeVersion', 'TVControlVersion', 'DesktopVersion') 'TVControlVersion' $Role
            $desktop = Get-LiteralCommandParameter $Command @('NodeVersion', 'TVControlVersion', 'DesktopVersion') 'DesktopVersion' $Role
            return @(
                (New-ToolRequirement 'OpenJS.NodeJS.LTS' $node '' 'tradingview'),
                (New-ToolRequirement '@ferroxlabs/tvcontrol' $control '' 'tradingview'),
                (New-ToolRequirement 'TradingView.TradingViewDesktop' $desktop '' 'tradingview')
            )
        }
        'Install-Uv' {
            $version = Get-LiteralCommandParameter $Command @('Version') 'Version' $Role
            return @(New-ToolRequirement 'astral-sh.uv' $version '' 'uv')
        }
        'Install-ZigStack' {
            $version = Get-LiteralCommandParameter $Command @('Version') 'Version' $Role
            return @(New-ToolRequirement 'zig.zig' $version '' 'zig')
        }
        default { return @() }
    }
}

function Get-SelectedProvisioningStacks {
    param(
        [Parameter(Mandatory = $true)]
        [IO.FileInfo]$Script,
        [Parameter(Mandatory = $true)]
        [string]$Role,
        [switch]$RejectParamBlock
    )

    if ($Script.Length -le 0 -or $Script.Length -gt 1048576) {
        throw "$Role script size is invalid: $($Script.Name)"
    }
    $tokens = $null
    $parseErrors = $null
    $ast = [System.Management.Automation.Language.Parser]::ParseFile(
        $Script.FullName, [ref]$tokens, [ref]$parseErrors)
    if ($parseErrors.Count -ne 0) {
        $first = $parseErrors[0]
        throw "$Role script parse failed for $($Script.Name) at line $($first.Extent.StartLineNumber): $($first.Message)"
    }
    if ($RejectParamBlock -and $null -ne $ast.ParamBlock) {
        throw "$Role script must not declare a script-level param block: $($Script.Name)"
    }
    $selected = @{}
    $tools = @()
    $commands = @($ast.FindAll({
        param($node)
        if ($node -isnot [System.Management.Automation.Language.CommandAst] -or
            $node.InvocationOperator -ne [System.Management.Automation.Language.TokenKind]::Unknown) {
            return $false
        }
        $commandName = $node.GetCommandName()
        return $null -ne $commandName -and $knownStacks.ContainsKey($commandName)
    }, $true))
    foreach ($command in $commands) {
        foreach ($stack in @($knownStacks[$command.GetCommandName()])) {
            $selected[[string]$stack] = $true
        }
        $tools += @(Get-CommandToolRequirements -Command $command -Role $Role)
    }
    return [pscustomobject]@{
        stacks = @($selected.Keys | Sort-Object)
        tools = @($tools | Sort-Object tool, version, series, source)
    }
}

$userScript = Get-Item -LiteralPath $UserProvisioningPath -Force
$userText = [IO.File]::ReadAllText($userScript.FullName)
if (-not $userText.Contains('# herdr-sandbox-user-contract: 1')) {
    throw "User provisioning contract is unsupported: $($userScript.FullName)"
}
$userSelection = Get-SelectedProvisioningStacks -Script $userScript -Role 'User provisioning' -RejectParamBlock

$scripts = @(Get-ChildItem -LiteralPath $ProjectsDirectory -File -Filter '*.ps1' | Sort-Object Name)
if ($scripts.Count -gt 16) {
    throw "Project provisioning script count is invalid: $($scripts.Count)"
}

$projects = @()
$projectErrors = @()
foreach ($script in $scripts) {
    try {
        $name = [IO.Path]::GetFileNameWithoutExtension($script.Name)
        if ($name -notmatch '^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$' -or
            $script.Length -le 0 -or $script.Length -gt 1048576) {
            throw "Project provisioning script identity is invalid: $($script.Name)"
        }

        $selection = Get-SelectedProvisioningStacks -Script $script -Role 'Project provisioning'
        $projects += [pscustomobject]@{
            name = $name
            stacks = @($selection.stacks)
            tools = @($selection.tools)
        }
    } catch {
        $projectErrors += "$($script.Name): $([string]$_.Exception.Message)"
    }
}
if ($projectErrors.Count -ne 0) {
    throw ("Project provisioning validation failed:`n" + ($projectErrors -join "`n"))
}

[pscustomobject]@{
    schemaVersion = 3
    userStacks = @($userSelection.stacks)
    userTools = @($userSelection.tools)
    projects = @($projects)
} | ConvertTo-Json -Compress -Depth 4
