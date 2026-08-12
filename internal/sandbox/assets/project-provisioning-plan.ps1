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
    'Install-BunStack' = 'bun'
    'Install-CargoNextest' = 'cargo-nextest'
    'Install-CppStack' = 'cpp'
    'Install-DotNetStack' = 'dotnet'
    'Install-GoStack' = 'go'
    'Install-HandyStack' = @('bun', 'handy', 'rust-msvc')
    'Install-HerdrStack' = @('bun', 'cargo-nextest', 'git-sh', 'just', 'python', 'rust-msvc', 'zig')
    'Install-JavaStack' = 'java'
    'Install-Just' = 'just'
    'Install-NodeStack' = 'node'
    'Install-NSISStack' = 'nsis'
    'Install-PlaywrightCLIStack' = 'playwright-cli'
    'Install-PythonAIStack' = @('python', 'uv')
    'Install-PythonStack' = 'python'
    'Install-RustMSVCStack' = 'rust-msvc'
    'Install-TradingViewStack' = 'tradingview'
    'Install-Uv' = 'uv'
    'Install-ZigStack' = 'zig'
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
    }
    return @($selected.Keys | Sort-Object)
}

$userScript = Get-Item -LiteralPath $UserProvisioningPath -Force
$userText = [IO.File]::ReadAllText($userScript.FullName)
if (-not $userText.Contains('# herdr-sandbox-user-contract: 1')) {
    throw "User provisioning contract is unsupported: $($userScript.FullName)"
}
$userStacks = @(Get-SelectedProvisioningStacks -Script $userScript -Role 'User provisioning' -RejectParamBlock)

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

        $projects += [pscustomobject]@{
            name = $name
            stacks = @(Get-SelectedProvisioningStacks -Script $script -Role 'Project provisioning')
        }
    } catch {
        $projectErrors += "$($script.Name): $([string]$_.Exception.Message)"
    }
}
if ($projectErrors.Count -ne 0) {
    throw ("Project provisioning validation failed:`n" + ($projectErrors -join "`n"))
}

[pscustomobject]@{
    schemaVersion = 2
    userStacks = @($userStacks)
    projects = @($projects)
} | ConvertTo-Json -Compress -Depth 4
