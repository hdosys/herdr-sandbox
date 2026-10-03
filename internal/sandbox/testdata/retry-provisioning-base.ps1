param($Phase, $ProjectProvisioningDirectory, $WorkspacesDirectory, $PackagePlanPath, $UserProvisioningPath, $ProcessOwnerPath)
$ErrorActionPreference = 'Stop'
$tokens = $null
$parseErrors = $null
$ast = [Management.Automation.Language.Parser]::ParseFile((Join-Path $PSScriptRoot 'stacks.ps1'), [ref]$tokens, [ref]$parseErrors)
if ($parseErrors.Count -ne 0) { throw $parseErrors[0].Message }
$definition = $ast.Find({
    param($node)
    $node -is [Management.Automation.Language.FunctionDefinitionAst] -and $node.Name -ceq 'Invoke-ProvisioningExtensionScript'
}, $true)
if ($null -eq $definition) { throw 'Production profile invocation is missing.' }
Invoke-Expression $definition.Extent.Text
Invoke-ProvisioningExtensionScript -Kind Project -WorkspaceName 'project' `
    -Path (Join-Path $ProjectProvisioningDirectory 'project.ps1') `
    -ProjectDirectory (Join-Path $WorkspacesDirectory 'project')
