param(
    [Parameter(Mandatory = $true)][string]$TestRoot,
    [Parameter(Mandatory = $true)][string]$CacheDirectory
)
$ErrorActionPreference = 'Stop'
Set-StrictMode -Version 2.0
if ($env:USERNAME -cne 'WDAGUtilityAccount') { throw 'This mutating acceptance is restricted to Windows Sandbox.' }
if (Test-Path -LiteralPath $TestRoot) { throw 'TestRoot must be new and task-owned.' }
if ((Get-PSDrive C).Free -lt 1GB) { throw 'Font acceptance requires 1 GiB free.' }
$repository = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..\..\..'))
$env:HERDR_PROVISIONING_ROOT = 'C:\HerdrSandbox'
$env:HERDR_SANDBOX_STATUS_DIRECTORY = $null
$env:HERDR_PROVISIONING_TARGET = 'workstation'
New-Item -ItemType Directory -Path $TestRoot | Out-Null
if (-not (Test-Path -LiteralPath $CacheDirectory -PathType Container)) { throw 'The verified package cache root must exist.' }
. (Join-Path $repository 'provisioning\base.ps1') -Phase Library `
    -ProcessOwnerPath (Join-Path $repository 'internal\sandbox\assets\provisioning-process.cs') -PackageCacheDirectory $CacheDirectory
$registry = 'HKLM:\SOFTWARE\Microsoft\Windows NT\CurrentVersion\Fonts'
$fontRoot = Join-Path $env:WINDIR 'Fonts'
$names = @(Get-ProvisioningGeistMonoExpectedFontNames)
$oldPaths = @{}
foreach ($name in $names) {
    $property = (Get-ItemProperty -LiteralPath $registry).PSObject.Properties[$name]
    if ($null -ne $property -and [string]$property.Value -ine (Join-Path $fontRoot $name)) {
        $oldPaths[[string]$property.Value] = (Get-FileHash -LiteralPath $property.Value).Hash
    }
}
$probe = Join-Path $TestRoot 'font-collection-probe.exe'
Add-Type -Path (Join-Path $PSScriptRoot 'font-collection-probe.cs') -OutputAssembly $probe -OutputType ConsoleApplication

Install-ProvisioningGeistMonoNerdFont
Invoke-ProvisioningNative -Role 'Fresh DirectWrite after installation' -FilePath $probe -ArgumentList @() | Write-Output
$before = @{}
foreach ($name in $names) {
    $path = Join-Path $fontRoot $name
    if ((Get-ItemPropertyValue -LiteralPath $registry -Name $name) -ine $path) { throw 'Nonstandard font registration.' }
    if (-not (Test-ProvisioningFontReadAccess -Path $path)) { throw 'Font ACL is not readable.' }
    $before[$name] = (Get-FileHash -LiteralPath $path -Algorithm SHA256).Hash
    Write-Output "$name SHA256=$($before[$name])"
    (Get-Acl -LiteralPath $path).AccessToString | Write-Output
}

foreach ($oldPath in $oldPaths.Keys) {
    if ((Get-FileHash -LiteralPath $oldPath).Hash -cne $oldPaths[$oldPath]) { throw 'Private font was changed.' }
    Write-Output "Private-path transition preserved: $oldPath"
}

# Reuse the same effect with the other target's storage/environment selection.
$env:HERDR_PROVISIONING_TARGET = 'sandbox'
Install-ProvisioningGeistMonoNerdFont
foreach ($name in $names) {
    $path = Join-Path $fontRoot $name
    if ((Get-ItemPropertyValue -LiteralPath $registry -Name $name) -ine $path -or
        (Get-FileHash -LiteralPath $path -Algorithm SHA256).Hash -cne $before[$name]) { throw 'Repeated installation changed identity.' }
}
Invoke-ProvisioningNative -Role 'Fresh DirectWrite after repeat' -FilePath $probe -ArgumentList @() | Write-Output
Write-Output 'Native font installation, access, fresh DirectWrite and repeat acceptance passed. Terminal rendering remains a separate visual check.'
