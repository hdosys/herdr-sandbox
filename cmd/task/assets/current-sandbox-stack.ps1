param(
    [Parameter(Mandatory = $true)][string]$SnapshotDirectory,
    [Parameter(Mandatory = $true)][string]$ProjectScript,
    [Parameter(Mandatory = $true)][string]$ProjectDirectory
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version 2.0
if ($env:USERNAME -ine 'WDAGUtilityAccount' -or $PSVersionTable.PSVersion.Major -ne 5) {
    throw 'Run stack verification inside Windows Sandbox with Windows PowerShell 5.1.'
}
$identity = [Security.Principal.WindowsIdentity]::GetCurrent()
$principal = New-Object Security.Principal.WindowsPrincipal($identity)
if (-not $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
    throw 'Stack verification requires the elevated Sandbox account.'
}

# Use the same immutable inputs, libraries, step executor and project invocation
# as provisioning. Library mode deliberately excludes desktop and SSH setup.
$basePath = Join-Path $SnapshotDirectory 'base.ps1'
$stacksPath = Join-Path $SnapshotDirectory 'stacks.ps1'
$processPath = Join-Path $SnapshotDirectory 'provisioning-process.cs'
. $basePath -Phase Library -ProcessOwnerPath $processPath
. $stacksPath
$provisioningPackagePlan = Read-ProvisioningPackagePlan -Path (Join-Path $SnapshotDirectory 'winget-packages.json')
$global:HerdrSandboxToolVersionPlan = Read-ProvisioningToolVersionPlan -Path (Join-Path $SnapshotDirectory 'tool-versions.json')
$DevelopmentFailures = New-Object 'Collections.Generic.List[string]'
$OptionalFailures = New-Object 'Collections.Generic.List[string]'
Initialize-ProvisioningSteps -Path '' -Resume $false -ImplementationFiles @($basePath, $processPath)
$global:HerdrSandboxStepState.StackImplementation = Get-ProvisioningStepFingerprint -Files @($stacksPath)
Update-ProvisioningPath

# Shared package prerequisites only. Never replay the Base desktop, shell,
# authentication or workspace configuration in a focused stack check.
Invoke-ProvisioningIndependentStep -Name 'WinGet' -Failures $DevelopmentFailures -Action { Install-ProvisioningWinGet }
$packages = @([pscustomobject]@{ id = 'Git.Git'; version = (Get-ProvisioningPackageVersion -Id 'Git.Git') })
foreach ($package in @(Get-ProvisioningPackageJobs -Packages $packages -IncludeVCRuntime)) {
    Invoke-ProvisioningIndependentStep -Name ('Package ' + $package.ID) -Failures $DevelopmentFailures -Requires $package.Requires `
        -InputValues @($package.Version) -Action { Install-ProvisioningPackage -Id $package.ID -Version $package.Version }
}
Invoke-ProvisioningIndependentStep -Name 'Selected project' -Failures $DevelopmentFailures -Action {
    Invoke-ProvisioningExtensionScript -Kind Project -Path $ProjectScript -ProjectDirectory $ProjectDirectory
}
if ($DevelopmentFailures.Count -gt 0 -or $OptionalFailures.Count -gt 0) {
    throw ("Stack verification incomplete:`n" + (@($DevelopmentFailures) + @($OptionalFailures) -join "`n"))
}
Write-Output 'Selected stack provisioning and its capability checks passed.'
