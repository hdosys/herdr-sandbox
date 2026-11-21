param([Parameter(Mandatory = $true)][string]$TestRoot)
$ErrorActionPreference = 'Stop'
Set-StrictMode -Version 2.0
if ($env:USERNAME -cne 'WDAGUtilityAccount') { throw 'This mutating acceptance is restricted to Windows Sandbox.' }
if (Test-Path -LiteralPath $TestRoot) { throw 'TestRoot must be new and task-owned.' }
if ((Get-PSDrive C).Free -lt 1GB) { throw 'Font acceptance requires 1 GiB free.' }
$repository = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..\..\..'))
$env:HERDR_PROVISIONING_ROOT = $TestRoot
$env:HERDR_SANDBOX_STATUS_DIRECTORY = $null
$env:HERDR_PROVISIONING_TARGET = 'workstation'
New-Item -ItemType Directory -Path $TestRoot | Out-Null
$cache = Join-Path $TestRoot 'cache'
New-Item -ItemType Directory -Path $cache | Out-Null
. (Join-Path $repository 'provisioning\base.ps1') -Phase Library `
    -ProcessOwnerPath (Join-Path $repository 'internal\sandbox\assets\provisioning-process.cs') -PackageCacheDirectory $cache
$registry = 'HKLM:\SOFTWARE\Microsoft\Windows NT\CurrentVersion\Fonts'
$fontRoot = Join-Path $env:WINDIR 'Fonts'
$names = @(Get-ProvisioningGeistMonoExpectedFontNames)
$canSimulateTransition = @($names | Where-Object { Test-Path -LiteralPath (Join-Path $fontRoot $_) }).Count -eq 0
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

if ($canSimulateTransition) {
    # Only registrations created by this test may be redirected. Preserve the
    # private files, including an unrelated sentinel, through the repair.
    $oldRoot = Join-Path (Get-ProvisioningPath 'tools') (Get-ProvisioningSafeCacheName 'NerdFonts.GeistMono')
    New-Item -ItemType Directory -Path $oldRoot -Force | Out-Null
    $sentinel = Join-Path $oldRoot 'unrelated.txt'
    [IO.File]::WriteAllText($sentinel, 'preserve')
    foreach ($name in $names) {
        Copy-Item -LiteralPath (Join-Path $fontRoot $name) -Destination (Join-Path $oldRoot $name)
        Set-ItemProperty -LiteralPath $registry -Name $name -Value (Join-Path $oldRoot $name)
    }
    Install-ProvisioningGeistMonoNerdFont
    foreach ($name in $names) {
        if (-not (Test-Path -LiteralPath (Join-Path $oldRoot $name))) { throw 'Private font was deleted.' }
    }
    if ([IO.File]::ReadAllText($sentinel) -cne 'preserve') { throw 'Unrelated file changed.' }
    Invoke-ProvisioningNative -Role 'Fresh DirectWrite after private-path repair' -FilePath $probe -ArgumentList @() | Write-Output
} else {
    Write-Output 'Existing installation preserved; destructive transition fixture not applied.'
}

Install-ProvisioningGeistMonoNerdFont
foreach ($name in $names) {
    $path = Join-Path $fontRoot $name
    if ((Get-ItemPropertyValue -LiteralPath $registry -Name $name) -ine $path -or
        (Get-FileHash -LiteralPath $path -Algorithm SHA256).Hash -cne $before[$name]) { throw 'Repeated installation changed identity.' }
}
Invoke-ProvisioningNative -Role 'Fresh DirectWrite after repeat' -FilePath $probe -ArgumentList @() | Write-Output
Write-Output 'Native font installation, access, fresh DirectWrite and repeat acceptance passed. Terminal rendering remains a separate visual check.'
