# herdr-sandbox-installed-candidate-state-contract: 1
param(
    [Parameter(Mandatory = $true)]
    [ValidatePattern('^\{[A-F0-9-]{36}\}$')]
    [string]$UninstallKey
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version 2.0

$path = 'HKCU:\Software\Microsoft\Windows\CurrentVersion\Uninstall\' + $UninstallKey
$record = [ordered]@{
    schemaVersion = 1
    installed = $false
    displayName = ''
    displayVersion = ''
    installLocation = ''
    quietUninstallString = ''
}
if (Test-Path -LiteralPath $path) {
    $state = Get-ItemProperty -LiteralPath $path -ErrorAction Stop
    $record.installed = $true
    $record.displayName = [string]$state.DisplayName
    $record.displayVersion = [string]$state.DisplayVersion
    $record.installLocation = [string]$state.InstallLocation
    $record.quietUninstallString = [string]$state.QuietUninstallString
}
[Console]::Out.WriteLine(($record | ConvertTo-Json -Compress))
