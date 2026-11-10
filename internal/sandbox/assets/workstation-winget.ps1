$ErrorActionPreference = 'Stop'
Set-StrictMode -Version 2.0
$env:PSModulePath = $PSHOME + '\Modules'
Import-Module Microsoft.PowerShell.Utility -ErrorAction Stop
[Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12

function Initialize-WorkstationWinGetSource {
    param([string]$Command)
    # App Installer deployment alone does not install the community source index.
    # Resolve that prerequisite here, before declaring dependent packages ready.
    Invoke-ProvisioningNative -Role 'WinGet community source initialization' -FilePath $Command -ArgumentList @(
        'source','update','--name','winget','--disable-interactivity') -TimeoutSeconds 180
    Invoke-ProvisioningNative -Role 'WinGet community source verification' -FilePath $Command -ArgumentList @(
        'show','--id','Microsoft.VCRedist.2015+.x64','--exact','--source','winget',
        '--accept-source-agreements','--disable-interactivity') -TimeoutSeconds 60 | Out-Null
}

# Retain an already functional supported prerequisite. Only a missing/older
# manager is deployed, using the newest official stable release below.
$existing = Get-Command winget.exe -CommandType Application -ErrorAction SilentlyContinue
if ($null -ne $existing) {
    $installed = (& $existing.Source --version) -join ' '
    if ($LASTEXITCODE -eq 0 -and $installed.Trim() -match '^v(?<version>\d+\.\d+\.\d+)$' -and [Version]$Matches['version'] -ge [Version]'1.29.0') {
        $capabilities = (& $existing.Source download --help) -join "`n"
        if ($LASTEXITCODE -eq 0 -and $capabilities.Contains('--skip-dependencies') -and $capabilities.Contains('--download-directory')) {
            Initialize-WorkstationWinGetSource -Command $existing.Source
            Write-Output "Existing WinGet $installed supports verified selected-manifest downloads."
            exit 0
        }
    }
}
$release = Invoke-RestMethod -Uri 'https://api.github.com/repos/microsoft/winget-cli/releases/latest' `
    -Headers @{ Accept = 'application/vnd.github+json'; 'User-Agent' = 'herdr-workstation' } -TimeoutSec 45
if ([bool]$release.draft -or [bool]$release.prerelease -or [string]$release.tag_name -notmatch '^v\d+\.\d+\.\d+$') {
    throw 'WinGet release metadata is not a stable official release.'
}
$stage = Join-Path $PSScriptRoot ('winget-' + [Guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $stage | Out-Null
try {
    foreach ($name in @('Microsoft.DesktopAppInstaller_8wekyb3d8bbwe.msixbundle', 'DesktopAppInstaller_Dependencies.zip')) {
        $assets = @($release.assets | Where-Object { [string]$_.name -ceq $name })
        if ($assets.Count -ne 1 -or [string]$assets[0].digest -notmatch '^sha256:(?<sha>[0-9a-f]{64})$') {
            throw "WinGet release does not provide one hash-bound $name."
        }
        $digest = $Matches['sha']
        $expectedURL = 'https://github.com/microsoft/winget-cli/releases/download/' + [string]$release.tag_name + '/' + $name
        if ([string]$assets[0].browser_download_url -cne $expectedURL) { throw 'Unexpected WinGet asset origin.' }
        $destination = Join-Path $stage $name
        Invoke-WebRequest -UseBasicParsing -Uri $expectedURL -OutFile $destination -TimeoutSec 180
        if ((Get-FileHash -LiteralPath $destination -Algorithm SHA256).Hash -ine $digest) { throw 'WinGet asset hash mismatch.' }
    }
    $dependencies = Join-Path $stage 'dependencies'
    Expand-Archive -LiteralPath (Join-Path $stage 'DesktopAppInstaller_Dependencies.zip') -DestinationPath $dependencies
    $packages = @(Get-ChildItem -LiteralPath (Join-Path $dependencies 'x64') -Filter '*.appx' -File | ForEach-Object { $_.FullName })
    if ($packages.Count -eq 0 -or $packages.Count -gt 16) { throw 'WinGet x64 dependencies are unavailable or unexpected.' }
    # Appx deployment enforces the Microsoft package signatures; no bypass flags.
    Add-AppxPackage -Path (Join-Path $stage 'Microsoft.DesktopAppInstaller_8wekyb3d8bbwe.msixbundle') -DependencyPath $packages
    $command = Get-Command winget.exe -CommandType Application -ErrorAction Stop
    & $command.Source --version
    if ($LASTEXITCODE -ne 0) { throw 'WinGet did not become functional after installation.' }
    Initialize-WorkstationWinGetSource -Command $command.Source
} finally {
    if (Test-Path -LiteralPath $stage) { Remove-Item -LiteralPath $stage -Recurse -Force }
}
