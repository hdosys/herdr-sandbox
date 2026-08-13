param(
    [Parameter(Mandatory = $true)][string]$Version,
    [Parameter(Mandatory = $true)][string]$FixedVersion,
    [Parameter(Mandatory = $true)][string]$AppName,
    [Parameter(Mandatory = $true)][string]$AppApplicationName,
    [Parameter(Mandatory = $true)][string]$AppDisplayName,
    [Parameter(Mandatory = $true)][string]$AppPublisher,
    [Parameter(Mandatory = $true)][string]$AppProductUrl,
    [Parameter(Mandatory = $true)][string]$AppCopyright,
    [Parameter(Mandatory = $true)][string]$AppConfigFile,
    [Parameter(Mandatory = $true)][string]$AppUserScript,
    [Parameter(Mandatory = $true)][string]$AppProjectDirectory,
    [Parameter(Mandatory = $true)][string]$OutputFile,
    [Parameter(Mandatory = $true)][string]$OutputFileName,
    [Parameter(Mandatory = $true)][string]$PackageDirectory,
    [Parameter(Mandatory = $true)][string]$PathHelper,
    [Parameter(Mandatory = $true)][string]$QuietUninstallHelper,
    [Parameter(Mandatory = $true)][string]$AppProductGuid,
    [Parameter(Mandatory = $true)][string]$AppUninstallKey,
    [Parameter(Mandatory = $true)][string]$AppInstallDirectory,
    [Parameter(Mandatory = $true)][string]$AppExecutable,
    [Parameter(Mandatory = $true)][string]$AppBaseScript,
    [Parameter(Mandatory = $true)][string]$AppStackScript,
    [Parameter(Mandatory = $true)][string]$AppLicense,
    [Parameter(Mandatory = $true)][string]$AppInstallerMarker,
    [Parameter(Mandatory = $true)][string]$AppQuietUninstallHelper,
    [string]$AppReplacedExecutable = '',
    [Parameter(Mandatory = $true)][string]$AssetsDirectory
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version 2.0

function Assert-SafeInstallerText {
    param([Parameter(Mandatory = $true)][string]$Name, [Parameter(Mandatory = $true)][string]$Value)

    if ([string]::IsNullOrWhiteSpace($Value)) {
        throw "$Name must not be empty or whitespace."
    }
    if ($Value.IndexOf('$') -ge 0 -or $Value.IndexOf('"') -ge 0) {
        throw "$Name contains a character that cannot be injected safely into this NSIS script."
    }
    foreach ($character in $Value.ToCharArray()) {
        if ([int][char]$character -lt 32) {
            throw "$Name contains a control character."
        }
    }
}

function Assert-SafeWindowsLeaf {
    param([Parameter(Mandatory = $true)][string]$Name, [Parameter(Mandatory = $true)][string]$Value)

    if ([string]::IsNullOrWhiteSpace($Value)) {
        throw "$Name must not be empty or whitespace."
    }
    if ($Value -ne $Value.Trim()) {
        throw "$Name must not start or end with whitespace."
    }
    if ($Value.EndsWith('.')) {
        throw "$Name must not end with a period."
    }
    if ($Value -eq '.' -or $Value -eq '..' -or $Value.IndexOfAny([IO.Path]::GetInvalidFileNameChars()) -ge 0) {
        throw "$Name is not a safe Windows leaf filename."
    }
    if ($Value.IndexOf(';') -ge 0 -or $Value.IndexOf('$') -ge 0) {
        throw "$Name contains a character reserved by this installer build."
    }
    foreach ($character in $Value.ToCharArray()) {
        if ([int][char]$character -lt 32) {
            throw "$Name contains an ASCII control character."
        }
    }
    $baseName = $Value.Split('.')[0].ToUpperInvariant()
    $reserved = @('CON', 'PRN', 'AUX', 'NUL', 'CLOCK$', 'CONIN$', 'CONOUT$')
    $reserved += 1..9 | ForEach-Object { 'COM' + $_ }
    $reserved += 1..9 | ForEach-Object { 'LPT' + $_ }
    if ($reserved -contains $baseName) {
        throw "$Name uses the reserved Windows device name '$baseName'."
    }
}

function Assert-ExistingRegularFile {
    param([Parameter(Mandatory = $true)][string]$Name, [Parameter(Mandatory = $true)][string]$Path)

    $item = Get-Item -LiteralPath $Path -Force
    if ($item.PSIsContainer -or (($item.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) -or $item.Length -le 0) {
        throw "$Name is not a nonempty regular file: $Path"
    }
}

function Assert-PowerShellSyntax {
    param([Parameter(Mandatory = $true)][string]$Path)

    $tokens = $null
    $errors = $null
    [void][System.Management.Automation.Language.Parser]::ParseFile($Path, [ref]$tokens, [ref]$errors)
    if ($errors.Count -ne 0) {
        throw "PowerShell syntax error in '$Path': $($errors[0].Message)"
    }
}

function Assert-BitmapDimensions {
    param(
        [Parameter(Mandatory = $true)][string]$Path,
        [Parameter(Mandatory = $true)][int]$Width,
        [Parameter(Mandatory = $true)][int]$Height
    )

    $bytes = [IO.File]::ReadAllBytes($Path)
    if ($bytes.Length -lt 54 -or [char]$bytes[0] -cne 'B' -or [char]$bytes[1] -cne 'M' -or
        [BitConverter]::ToInt32($bytes, 14) -ne 40 -or [BitConverter]::ToInt32($bytes, 18) -ne $Width -or
        [BitConverter]::ToInt32($bytes, 22) -ne $Height -or [BitConverter]::ToInt16($bytes, 26) -ne 1 -or
        [BitConverter]::ToInt16($bytes, 28) -ne 24 -or [BitConverter]::ToInt32($bytes, 30) -ne 0) {
        throw "Installer bitmap is not an exact uncompressed 24-bit BMP3 at ${Width}x${Height}: $Path"
    }
}

foreach ($textValue in ([ordered]@{
    Version = $Version
    FixedVersion = $FixedVersion
    AppName = $AppName
    AppApplicationName = $AppApplicationName
    AppDisplayName = $AppDisplayName
    AppPublisher = $AppPublisher
    AppProductUrl = $AppProductUrl
    AppCopyright = $AppCopyright
    AppConfigFile = $AppConfigFile
    AppUserScript = $AppUserScript
    AppProjectDirectory = $AppProjectDirectory
    OutputFile = $OutputFile
    PackageDirectory = $PackageDirectory
    PathHelper = $PathHelper
    QuietUninstallHelper = $QuietUninstallHelper
    AssetsDirectory = $AssetsDirectory
}).GetEnumerator()) {
    Assert-SafeInstallerText -Name $textValue.Key -Value ([string]$textValue.Value)
}

$productUri = $null
if (-not [Uri]::TryCreate($AppProductUrl, [UriKind]::Absolute, [ref]$productUri) -or
    ($productUri.Scheme -ne 'https' -and $productUri.Scheme -ne 'http')) {
    throw 'AppProductUrl must be an absolute HTTP or HTTPS URL.'
}

$leafValues = [ordered]@{
    OutputFileName = $OutputFileName
    AppApplicationName = $AppApplicationName
    AppUninstallKey = $AppUninstallKey
    AppInstallDirectory = $AppInstallDirectory
    AppExecutable = $AppExecutable
    AppBaseScript = $AppBaseScript
    AppStackScript = $AppStackScript
    AppLicense = $AppLicense
    AppInstallerMarker = $AppInstallerMarker
    AppQuietUninstallHelper = $AppQuietUninstallHelper
}
if (-not [string]::IsNullOrWhiteSpace($AppReplacedExecutable)) {
    $leafValues['AppReplacedExecutable'] = $AppReplacedExecutable
}
foreach ($entry in $leafValues.GetEnumerator()) {
    Assert-SafeWindowsLeaf -Name $entry.Key -Value ([string]$entry.Value)
}

$payloadNames = [ordered]@{
    AppExecutable = $AppExecutable
    AppBaseScript = $AppBaseScript
    AppStackScript = $AppStackScript
    AppLicense = $AppLicense
    AppInstallerMarker = $AppInstallerMarker
    AppQuietUninstallHelper = $AppQuietUninstallHelper
    Uninstaller = 'uninstall.exe'
}
if (-not [string]::IsNullOrWhiteSpace($AppReplacedExecutable)) {
    $payloadNames['AppReplacedExecutable'] = $AppReplacedExecutable
}
$seen = @{}
foreach ($entry in $payloadNames.GetEnumerator()) {
    $key = ([string]$entry.Value).ToUpperInvariant()
    if ($seen.ContainsKey($key)) {
        throw "$($entry.Key) collides with $($seen[$key]) as '$($entry.Value)'."
    }
    $seen[$key] = $entry.Key
}

$parsedGuid = [Guid]::Empty
if (-not [Guid]::TryParseExact($AppProductGuid, 'D', [ref]$parsedGuid) -or
    $parsedGuid.ToString('D') -cne $AppProductGuid) {
    throw 'AppProductGuid must be a canonical lowercase GUID.'
}
if ($AppUninstallKey -cne ('{' + $parsedGuid.ToString('D').ToUpperInvariant() + '}')) {
    throw 'AppUninstallKey must be the brace-wrapped uppercase product GUID.'
}

if ($FixedVersion -notmatch '^(\d+)\.(\d+)\.(\d+)\.(\d+)$') {
    throw 'FixedVersion must contain exactly four numeric components.'
}
$fixedParts = $FixedVersion.Split('.') | ForEach-Object { [int]$_ }
if (@($fixedParts | Where-Object { $_ -lt 0 -or $_ -gt 65535 }).Count -ne 0) {
    throw 'Every FixedVersion component must be between 0 and 65535.'
}
$versionCore = ($Version -split '[-+]')[0]
$versionParts = $versionCore.Split('.')
if ($versionParts.Count -lt 3 -or $versionParts.Count -gt 4 -or @($versionParts | Where-Object { $_ -notmatch '^\d+$' }).Count -ne 0) {
    throw 'Version must start with a three- or four-component numeric version.'
}
$normalizedVersion = @([int]$versionParts[0], [int]$versionParts[1], [int]$versionParts[2], 0)
if ($versionParts.Count -eq 4) {
    $normalizedVersion[3] = [int]$versionParts[3]
}
if ([string]::Join('.', $normalizedVersion) -ne [string]::Join('.', $fixedParts)) {
    throw "Version '$Version' and FixedVersion '$FixedVersion' do not describe the same numeric release."
}

if ([IO.Path]::GetFileName($OutputFile) -ine $OutputFileName) {
    throw 'OutputFileName does not match the basename of OutputFile.'
}

$packageItem = Get-Item -LiteralPath $PackageDirectory -Force
if (-not $packageItem.PSIsContainer -or (($packageItem.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0)) {
    throw 'PackageDirectory is not a regular directory.'
}
$assetItem = Get-Item -LiteralPath $AssetsDirectory -Force
if (-not $assetItem.PSIsContainer -or (($assetItem.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0)) {
    throw 'AssetsDirectory is not a regular directory.'
}

if ([IO.Path]::GetExtension($OutputFileName) -ine '.exe') {
    throw 'OutputFileName must use the .exe extension.'
}
if ([IO.Path]::GetExtension($AppExecutable) -ine '.exe') {
    throw 'AppExecutable must use the .exe extension.'
}
if ([IO.Path]::GetExtension($AppQuietUninstallHelper) -ine '.ps1') {
    throw 'AppQuietUninstallHelper must use the .ps1 extension.'
}
if ([IO.Path]::GetExtension($PathHelper) -ine '.ps1' -or
    [IO.Path]::GetExtension($QuietUninstallHelper) -ine '.ps1') {
    throw 'Both helper source files must use the .ps1 extension.'
}

Assert-ExistingRegularFile -Name PathHelper -Path $PathHelper
Assert-ExistingRegularFile -Name QuietUninstallHelper -Path $QuietUninstallHelper
Assert-PowerShellSyntax -Path $PathHelper
Assert-PowerShellSyntax -Path $QuietUninstallHelper

foreach ($name in @($AppExecutable, $AppBaseScript, $AppStackScript, $AppLicense)) {
    Assert-ExistingRegularFile -Name $name -Path (Join-Path $PackageDirectory $name)
}
foreach ($asset in @(
    @{ Name = 'installer-welcome-finish-164x314.bmp'; Width = 164; Height = 314 },
    @{ Name = 'installer-welcome-finish-205x393.bmp'; Width = 205; Height = 393 },
    @{ Name = 'installer-welcome-finish-246x471.bmp'; Width = 246; Height = 471 },
    @{ Name = 'installer-welcome-finish-287x550.bmp'; Width = 287; Height = 550 },
    @{ Name = 'installer-welcome-finish-328x628.bmp'; Width = 328; Height = 628 }
)) {
    $assetPath = Join-Path $AssetsDirectory $asset.Name
    Assert-ExistingRegularFile -Name $asset.Name -Path $assetPath
    Assert-BitmapDimensions -Path $assetPath -Width $asset.Width -Height $asset.Height
}

[Console]::Out.WriteLine('Installer build inputs validated successfully.')
