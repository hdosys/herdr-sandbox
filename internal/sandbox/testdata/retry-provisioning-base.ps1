param($Phase, $ProjectProvisioningDirectory, $WorkspacesDirectory, $PackagePlanPath, $UserProvisioningPath, $ProcessOwnerPath, $DevelopmentFailures)
$ErrorActionPreference = 'Stop'
$tokens = $null
$parseErrors = $null
$ast = [Management.Automation.Language.Parser]::ParseFile((Join-Path $PSScriptRoot 'stacks.ps1'), [ref]$tokens, [ref]$parseErrors)
if ($parseErrors.Count -ne 0) { throw $parseErrors[0].Message }
foreach ($name in @('Get-ProvisioningBoundedDiagnosticText', 'Invoke-ProvisioningIndependentStep', 'Invoke-ProvisioningExtensionScript')) {
    $definition = $ast.Find({
        param($node)
        $node -is [Management.Automation.Language.FunctionDefinitionAst] -and $node.Name -ceq $name
    }.GetNewClosure(), $true)
    if ($null -eq $definition) { throw "Production function is missing: $name" }
    . ([scriptblock]::Create($definition.Extent.Text))
}
foreach ($workspaceName in @('project', 'working')) {
    Invoke-ProvisioningIndependentStep -Name "Project $workspaceName" -Failures $DevelopmentFailures -Action {
        Invoke-ProvisioningExtensionScript -Kind Project -WorkspaceName $workspaceName `
            -Path (Join-Path $ProjectProvisioningDirectory ($workspaceName + '.ps1')) `
            -ProjectDirectory (Join-Path $WorkspacesDirectory $workspaceName)
    }
}
$failureCount = $DevelopmentFailures.Count
$cancelled = $false
try {
    Invoke-ProvisioningIndependentStep -Name 'Cancellation' -Failures $DevelopmentFailures -Action {
        throw [OperationCanceledException]::new('intentional cancellation')
    }
} catch [OperationCanceledException] { $cancelled = $true }
if (-not $cancelled -or $DevelopmentFailures.Count -ne $failureCount) {
    throw 'Cancellation was incorrectly recorded as an independent failure.'
}
& $UserProvisioningPath
