param(
    [Parameter(Mandatory=$true)][string]$SelectionPath,
    [Parameter(Mandatory=$true)][string]$BasePath,
    [Parameter(Mandatory=$true)][string]$StackPath,
    [Parameter(Mandatory=$true)][string]$ProcessOwnerPath,
    [Parameter(Mandatory=$true)][string]$CacheDirectory,
    [Parameter(Mandatory=$true)][string]$WorkDirectory,
    [Parameter(Mandatory=$true)][string]$ResultPath,
    [switch]$PackagesOnly
)
$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'
$env:PSModulePath = $PSHOME + '\Modules'
Import-Module Microsoft.PowerShell.Utility -ErrorAction Stop
$env:HERDR_PROVISIONING_ROOT = $WorkDirectory
$env:HERDR_PROVISIONING_TARGET = ''
. $BasePath -Phase Prepare -PackageCacheDirectory $CacheDirectory -ProcessOwnerPath $ProcessOwnerPath
. $StackPath
Assert-ProvisioningPathAncestors $SelectionPath
Assert-ProvisioningPathAncestors $ResultPath
$selection = [IO.File]::ReadAllText($SelectionPath) | ConvertFrom-Json
if ($selection.schemaVersion -ne 1 -or @($selection.packages).Count -gt 64 -or @($selection.stacks).Count -gt 32) {
    throw 'Materialization selection has an unsupported shape.'
}
foreach ($directory in @($CacheDirectory, (Get-ProvisioningPath 'staging'), (Get-ProvisioningPath 'tools'))) {
    Assert-ProvisioningPathAncestors $directory
    New-Item -ItemType Directory -Path $directory -Force | Out-Null
}
$provisioningPackagePlan = [pscustomobject]@{Enabled=@{};Versions=@{}}
foreach ($package in @($selection.packages)) {
    $provisioningPackagePlan.Enabled[[string]$package.id] = $true
    $provisioningPackagePlan.Versions[[string]$package.id] = [string]$package.version
}
foreach ($tool in @($selection.tools)) {
    $global:HerdrSandboxToolVersionPlan.Versions[[string]$tool.tool] = [string]$tool.version
    $global:HerdrSandboxToolVersionPlan.Series[[string]$tool.tool] = [string]$tool.series
    $global:HerdrSandboxToolVersionPlan.Sources[[string]$tool.tool] = [string]$tool.source
    $global:HerdrSandboxToolVersionPlan.Owners[[string]$tool.tool] = @($tool.owners)
}
Prepare-ProvisioningWinGet
foreach ($package in @($selection.packages)) {
    Install-ProvisioningPackage -Id ([string]$package.id) -Version ([string]$package.version)
}
Install-ProvisioningPackage -Id 'Microsoft.VCRedist.2015+.x64'
if (-not $PackagesOnly) {
    foreach ($stack in @($selection.stacks)) {
        switch ([string]$stack) {
            'go' { Install-GoStack }
            'python' { Install-PythonStack }
            'dotnet' { Install-DotNetStack }
            'nsis' { Install-NSISStack }
            'zig' { Install-ZigStack }
            'just' { Install-Just }
            'cargo-nextest' { Install-CargoNextest }
            'opensrc' { Install-ProvisioningOpenSrc }
            'font' { Install-ProvisioningGeistMonoNerdFont }
            'node' { Install-NodeStack }
            'rust-msvc' { Install-RustMSVCStack }
            'tradingview' { Install-TradingViewStack }
            default { throw "Materialization for selected stack $stack has not completed its shared preparation contract." }
        }
    }
}
$resolvedTools = @()
foreach ($tool in @($global:HerdrSandboxToolVersionPlan.Versions.Keys | Sort-Object)) {
    $resolvedTools += [ordered]@{
        tool=$tool;version=[string]$global:HerdrSandboxToolVersionPlan.Versions[$tool]
        series=$(if($global:HerdrSandboxToolVersionPlan.Series.ContainsKey($tool)){[string]$global:HerdrSandboxToolVersionPlan.Series[$tool]}else{''})
        source=$(if($global:HerdrSandboxToolVersionPlan.Sources.ContainsKey($tool)){[string]$global:HerdrSandboxToolVersionPlan.Sources[$tool]}else{'stack-default'})
        owners=@($(if($global:HerdrSandboxToolVersionPlan.Owners.ContainsKey($tool)){$global:HerdrSandboxToolVersionPlan.Owners[$tool]}else{'private-installer'}))
    }
}
$result = [ordered]@{
    schemaVersion=1
    packages=@($script:PreparedPackageMetadata.Values | Sort-Object { $_.metadata.Id }, { $_.metadata.InstallerType }, { $_.metadata.Scope })
    tools=$resolvedTools
    rust=$(if($null -ne (Get-Variable -Name PreparedRustDistribution -Scope Script -ErrorAction SilentlyContinue)){$script:PreparedRustDistribution}else{$null})
}
$temporary = $ResultPath + '.tmp'
[IO.File]::WriteAllText($temporary, ($result | ConvertTo-Json -Depth 12), (New-Object Text.UTF8Encoding($false)))
if (Test-Path -LiteralPath $ResultPath) { [IO.File]::Replace($temporary,$ResultPath,[NullString]::Value) }
else { [IO.File]::Move($temporary,$ResultPath) }
Write-Output ('Materialized package records: ' + $result.packages.Count)
