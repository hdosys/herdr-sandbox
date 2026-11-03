param($Phase, $ProjectProvisioningDirectory, $WorkspacesDirectory, $PackagePlanPath, $UserProvisioningPath, $ProcessOwnerPath, $DevelopmentFailures, $OptionalFailures, $StepStatePath, [switch]$Resume)
$ErrorActionPreference = 'Stop'
$tokens = $null
$parseErrors = $null
$ast = [Management.Automation.Language.Parser]::ParseFile((Join-Path $PSScriptRoot 'stacks.ps1'), [ref]$tokens, [ref]$parseErrors)
if ($parseErrors.Count -ne 0) { throw $parseErrors[0].Message }
foreach ($name in @('Get-ProvisioningBoundedDiagnosticText', 'Invoke-ProvisioningIndependentStep', 'Invoke-ProvisioningExtensionScript',
        'Test-ProvisioningTerminalError', 'Get-ProvisioningStepFingerprint', 'Initialize-ProvisioningSteps', 'Save-ProvisioningSteps', 'Invoke-ProvisioningStep')) {
    $definition = $ast.Find({
        param($node)
        $node -is [Management.Automation.Language.FunctionDefinitionAst] -and $node.Name -ceq $name
    }.GetNewClosure(), $true)
    if ($null -eq $definition) { throw "Production function is missing: $name" }
    . ([scriptblock]::Create($definition.Extent.Text))
}
function Write-ProvisioningProgress { param($Message) }
Initialize-ProvisioningSteps -Path $StepStatePath -Resume $Resume.IsPresent -ImplementationFiles @((Join-Path $PSScriptRoot 'stacks.ps1'))
foreach ($workspaceName in @('project', 'working')) {
    Invoke-ProvisioningIndependentStep -Name "Project $workspaceName" -Failures $DevelopmentFailures `
        -InputFiles @((Join-Path $ProjectProvisioningDirectory ($workspaceName + '.ps1'))) -Action {
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
