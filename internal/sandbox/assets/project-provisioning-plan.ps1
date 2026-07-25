param(
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
    'Install-CargoNextest' = 'cargo-nextest'
    'Install-GoStack' = 'go'
    'Install-Just' = 'just'
    'Install-NodeStack' = 'node'
    'Install-PythonStack' = 'python'
    'Install-RustMSVCStack' = 'rust-msvc'
    'Install-ZigStack' = 'zig'
}

$scripts = @(Get-ChildItem -LiteralPath $ProjectsDirectory -File -Filter '*.ps1' | Sort-Object Name)
if ($scripts.Count -eq 0 -or $scripts.Count -gt 16) {
    throw "Project provisioning script count is invalid: $($scripts.Count)"
}

$projects = @()
foreach ($script in $scripts) {
    $name = [IO.Path]::GetFileNameWithoutExtension($script.Name)
    if ($name -notmatch '^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$' -or
        $script.Length -le 0 -or $script.Length -gt 1048576) {
        throw "Project provisioning script identity is invalid: $($script.Name)"
    }

    $tokens = $null
    $parseErrors = $null
    $ast = [System.Management.Automation.Language.Parser]::ParseFile(
        $script.FullName, [ref]$tokens, [ref]$parseErrors)
    if ($parseErrors.Count -ne 0) {
        $first = $parseErrors[0]
        throw "Project provisioning script parse failed for $($script.Name) at line $($first.Extent.StartLineNumber): $($first.Message)"
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
        $selected[[string]$knownStacks[$command.GetCommandName()]] = $true
    }

    $projects += [pscustomobject]@{
        name = $name
        stacks = @($selected.Keys | Sort-Object)
    }
}

[pscustomobject]@{
    schemaVersion = 1
    projects = @($projects)
} | ConvertTo-Json -Compress -Depth 4
