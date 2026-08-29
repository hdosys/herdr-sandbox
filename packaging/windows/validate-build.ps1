param(
    [Parameter(Mandatory = $true)][string]$InstallerScript,
    [Parameter(Mandatory = $true)][string]$Version,
    [Parameter(Mandatory = $true)][string]$FixedVersion,
    [Parameter(Mandatory = $true)][string]$BuildFreshness,
    [Parameter(Mandatory = $true)][string]$BuildRevision,
    [Parameter(Mandatory = $true)][string]$BuildDisplay,
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
    [Parameter(Mandatory = $true)][string]$AssetsDirectory,
    [Parameter(Mandatory = $true)][string]$AppProductGuid,
    [Parameter(Mandatory = $true)][string]$AppUninstallKey,
    [Parameter(Mandatory = $true)][string]$AppInstallDirectory,
    [Parameter(Mandatory = $true)][string]$AppExecutable,
    [Parameter(Mandatory = $true)][string]$AppBaseScript,
    [Parameter(Mandatory = $true)][string]$AppStackScript,
    [Parameter(Mandatory = $true)][string]$AppLicense,
    [Parameter(Mandatory = $true)][string]$AppQuietUninstallHelper,
    [ValidateRange(1, 2100000000)][long]$MaximumEmbeddedBytes = 1800000000,
    [switch]$RequirePayloadSignature
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version 2.0

function Get-CanonicalPath {
    param([Parameter(Mandatory = $true)][string]$Path)

    if ([string]::IsNullOrWhiteSpace($Path)) {
        throw 'A required path is empty.'
    }
    if ($Path.IndexOfAny([char[]]@('*', '?')) -ge 0) {
        throw "Build paths must not contain wildcards: '$Path'."
    }
    return [IO.Path]::GetFullPath($Path)
}

function Assert-NoReparseInExistingPath {
    param([Parameter(Mandatory = $true)][string]$Path)

    $full = Get-CanonicalPath -Path $Path
    $root = [IO.Path]::GetPathRoot($full)
    $current = $root
    $relative = $full.Substring($root.Length)
    foreach ($part in $relative.Split([char[]]@('\', '/'), [StringSplitOptions]::RemoveEmptyEntries)) {
        $current = Join-Path $current $part
        if (-not (Test-Path -LiteralPath $current)) {
            break
        }
        $item = Get-Item -LiteralPath $current -Force
        if (($item.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {
            throw "Build input path traverses a reparse point: '$current'."
        }
    }
}

function Assert-SafeInstallerText {
    param([Parameter(Mandatory = $true)][string]$Name, [Parameter(Mandatory = $true)][string]$Value)

    if ([string]::IsNullOrWhiteSpace($Value) -or $Value.Length -gt 260) {
        throw "$Name must be nonempty and bounded."
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
    if ($Value.Length -gt 255) {
        throw "$Name exceeds the Win32 leaf-name limit."
    }
    if ($Value -ne $Value.Trim()) {
        throw "$Name must not start or end with whitespace."
    }
    if ($Value.EndsWith('.') -or $Value.EndsWith(' ')) {
        throw "$Name must not end with a period or space."
    }
    if ($Value -eq '.' -or $Value -eq '..' -or $Value -match '[<>:"/\\|?*\x00-\x1F]') {
        throw "$Name is not a safe Windows leaf filename."
    }
    if ($Value.IndexOf(';') -ge 0 -or $Value.IndexOf('$') -ge 0) {
        throw "$Name contains a character reserved by this installer build."
    }

    $baseName = $Value.Split('.')[0].ToUpperInvariant()
    $reserved = @(
        'CON', 'PRN', 'AUX', 'NUL', 'CLOCK$', 'CONIN$', 'CONOUT$',
        'COM1', 'COM2', 'COM3', 'COM4', 'COM5', 'COM6', 'COM7', 'COM8', 'COM9',
        'LPT1', 'LPT2', 'LPT3', 'LPT4', 'LPT5', 'LPT6', 'LPT7', 'LPT8', 'LPT9',
        ('COM' + [char]0x00B9), ('COM' + [char]0x00B2), ('COM' + [char]0x00B3),
        ('LPT' + [char]0x00B9), ('LPT' + [char]0x00B2), ('LPT' + [char]0x00B3)
    )
    if ($reserved -contains $baseName) {
        throw "$Name uses the reserved Windows device name '$baseName'."
    }
}

function Assert-ExistingRegularFile {
    param([Parameter(Mandatory = $true)][string]$Name, [Parameter(Mandatory = $true)][string]$Path)

    $full = Get-CanonicalPath -Path $Path
    Assert-NoReparseInExistingPath -Path $full
    $item = Get-Item -LiteralPath $full -Force
    if ($item.PSIsContainer -or (($item.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) -or
        $item.Length -le 0) {
        throw "$Name is not a nonempty regular file: $full"
    }
    return $item
}

function Assert-ExistingRegularDirectory {
    param([Parameter(Mandatory = $true)][string]$Name, [Parameter(Mandatory = $true)][string]$Path)

    $full = Get-CanonicalPath -Path $Path
    Assert-NoReparseInExistingPath -Path $full
    $item = Get-Item -LiteralPath $full -Force
    if (-not $item.PSIsContainer -or (($item.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0)) {
        throw "$Name is not a regular directory: $full"
    }
    return $item
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

function Assert-Amd64Pe {
    param([Parameter(Mandatory = $true)][string]$Path)

    $stream = [IO.File]::Open($Path, [IO.FileMode]::Open, [IO.FileAccess]::Read, [IO.FileShare]::Read)
    $reader = [IO.BinaryReader]::new($stream)
    try {
        if ($stream.Length -lt 256 -or $reader.ReadUInt16() -ne 0x5a4d) {
            throw "Application executable is not a valid PE file: '$Path'."
        }
        $stream.Position = 0x3c
        $peOffset = $reader.ReadInt32()
        if ($peOffset -lt 0 -or [long]$peOffset + 26 -gt $stream.Length) {
            throw "Application executable has an invalid PE header offset: '$Path'."
        }
        $stream.Position = $peOffset
        if ($reader.ReadUInt32() -ne 0x00004550) {
            throw "Application executable has no PE signature: '$Path'."
        }
        $machine = $reader.ReadUInt16()
        $stream.Position = $peOffset + 24
        $optionalMagic = $reader.ReadUInt16()
        if ($machine -ne 0x8664 -or $optionalMagic -ne 0x20b) {
            throw "Application executable must be an AMD64 PE32+ binary: '$Path'."
        }
    }
    finally {
        $reader.Dispose()
        $stream.Dispose()
    }
}

function Assert-Bitmap {
    param(
        [Parameter(Mandatory = $true)][string]$Path,
        [Parameter(Mandatory = $true)][int]$ExpectedWidth,
        [Parameter(Mandatory = $true)][int]$ExpectedHeight
    )

    $bytes = [IO.File]::ReadAllBytes($Path)
    if ($bytes.Length -lt 54 -or $bytes[0] -ne 0x42 -or $bytes[1] -ne 0x4d) {
        throw "Wizard image is not a Windows BMP: '$Path'."
    }
    $fileSize = [BitConverter]::ToUInt32($bytes, 2)
    $pixelOffset = [BitConverter]::ToUInt32($bytes, 10)
    $dibSize = [BitConverter]::ToInt32($bytes, 14)
    $width = [BitConverter]::ToInt32($bytes, 18)
    $height = [BitConverter]::ToInt32($bytes, 22)
    $planes = [BitConverter]::ToUInt16($bytes, 26)
    $bitsPerPixel = [BitConverter]::ToUInt16($bytes, 28)
    $compression = [BitConverter]::ToUInt32($bytes, 30)
    $pixelDataSize = [BitConverter]::ToUInt32($bytes, 34)
    $rowSize = ($ExpectedWidth * 3 + 3) -band (-bnot 3)
    $expectedPixelDataSize = $rowSize * $ExpectedHeight
    if ($fileSize -ne $bytes.Length -or $pixelOffset -ne 54 -or $dibSize -ne 40 -or
        $width -ne $ExpectedWidth -or $height -ne $ExpectedHeight -or $planes -ne 1 -or
        $bitsPerPixel -ne 24 -or $compression -ne 0 -or $pixelDataSize -ne $expectedPixelDataSize -or
        $bytes.Length -ne 54 + $expectedPixelDataSize) {
        throw "Wizard image is not an exact uncompressed 24-bit BMP3 at ${ExpectedWidth}x${ExpectedHeight}: '$Path'."
    }
}

function Assert-ValidSignature {
    param([Parameter(Mandatory = $true)][string]$Path)

    $signature = Get-AuthenticodeSignature -LiteralPath $Path
    if ($signature.Status -ne [System.Management.Automation.SignatureStatus]::Valid) {
        throw "Authenticode signature is not valid for '$Path': $($signature.Status)."
    }
}

foreach ($textValue in ([ordered]@{
    Version = $Version
    FixedVersion = $FixedVersion
    BuildFreshness = $BuildFreshness
    BuildRevision = $BuildRevision
    BuildDisplay = $BuildDisplay
    AppName = $AppName
    AppApplicationName = $AppApplicationName
    AppDisplayName = $AppDisplayName
    AppPublisher = $AppPublisher
    AppProductUrl = $AppProductUrl
    AppCopyright = $AppCopyright
    AppConfigFile = $AppConfigFile
    AppUserScript = $AppUserScript
    AppProjectDirectory = $AppProjectDirectory
    InstallerScript = $InstallerScript
    OutputFile = $OutputFile
    PackageDirectory = $PackageDirectory
    PathHelper = $PathHelper
    QuietUninstallHelper = $QuietUninstallHelper
    AssetsDirectory = $AssetsDirectory
}).GetEnumerator()) {
    Assert-SafeInstallerText -Name $textValue.Key -Value ([string]$textValue.Value)
}

$parsedFreshness = [DateTime]::MinValue
$freshnessStyles = [Globalization.DateTimeStyles]::AssumeUniversal -bor [Globalization.DateTimeStyles]::AdjustToUniversal
if ($BuildFreshness -cnotmatch '^\d{4}\.\d{2}\.\d{2}\.\d{4}Z$' -or
    -not [DateTime]::TryParseExact($BuildFreshness, 'yyyy.MM.dd.HHmmZ', [Globalization.CultureInfo]::InvariantCulture, $freshnessStyles, [ref]$parsedFreshness) -or
    $parsedFreshness.ToUniversalTime().ToString('yyyy.MM.dd.HHmmZ', [Globalization.CultureInfo]::InvariantCulture) -cne $BuildFreshness) {
    throw 'BuildFreshness must use a real UTC YYYY.MM.DD.HHMMZ timestamp.'
}
if ($BuildRevision -cnotmatch '^[0-9a-f]{12}$') {
    throw 'BuildRevision must contain exactly 12 lowercase hexadecimal characters.'
}
$expectedBuildDisplay = $Version + ' ' + $BuildFreshness + ' (' + $BuildRevision + ')'
if ($BuildDisplay -cne $expectedBuildDisplay) {
    throw "BuildDisplay must equal '$expectedBuildDisplay'."
}

$productUri = $null
if (-not [Uri]::TryCreate($AppProductUrl, [UriKind]::Absolute, [ref]$productUri) -or
    ($productUri.Scheme -ne 'https' -and $productUri.Scheme -ne 'http')) {
    throw 'AppProductUrl must be an absolute HTTP or HTTPS URL.'
}

$leafValues = [ordered]@{
    AppApplicationName = $AppApplicationName
    OutputFileName = $OutputFileName
    AppUninstallKey = $AppUninstallKey
    AppInstallDirectory = $AppInstallDirectory
    AppExecutable = $AppExecutable
    AppBaseScript = $AppBaseScript
    AppStackScript = $AppStackScript
    AppLicense = $AppLicense
    AppQuietUninstallHelper = $AppQuietUninstallHelper
}
foreach ($entry in $leafValues.GetEnumerator()) {
    Assert-SafeWindowsLeaf -Name $entry.Key -Value ([string]$entry.Value)
}

$payloadNames = [ordered]@{
    AppExecutable = $AppExecutable
    AppBaseScript = $AppBaseScript
    AppStackScript = $AppStackScript
    AppLicense = $AppLicense
    AppQuietUninstallHelper = $AppQuietUninstallHelper
    Uninstaller = 'uninstall.exe'
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
if (-not [Guid]::TryParseExact($AppProductGuid, 'D', [ref]$parsedGuid)) {
    throw 'AppProductGuid must be a canonical D-format GUID.'
}
$canonicalGuid = $parsedGuid.ToString('D').ToLowerInvariant()
if ($AppProductGuid -cne $canonicalGuid) {
    throw "AppProductGuid must use canonical lowercase D format: '$canonicalGuid'."
}
if ($AppUninstallKey -cne ('{' + $parsedGuid.ToString('D').ToUpperInvariant() + '}')) {
    throw 'AppUninstallKey must be the brace-wrapped uppercase product GUID.'
}

if ($FixedVersion -notmatch '^(\d+)\.(\d+)\.(\d+)\.(\d+)$') {
    throw 'FixedVersion must contain exactly four numeric components.'
}
$fixedParts = @()
foreach ($part in $FixedVersion.Split('.')) {
    $number = 0
    if (-not [int]::TryParse($part, [ref]$number) -or $number -lt 0 -or $number -gt 65535) {
        throw 'Every FixedVersion component must be between 0 and 65535.'
    }
    $fixedParts += $number
}
$versionCore = ($Version -split '[-+]')[0]
$versionParts = $versionCore.Split('.')
if ($versionParts.Count -lt 3 -or $versionParts.Count -gt 4) {
    throw 'Version must start with a three- or four-component numeric version.'
}
$normalizedVersion = @(0, 0, 0, 0)
for ($index = 0; $index -lt $versionParts.Count; $index += 1) {
    $number = 0
    if (-not [int]::TryParse($versionParts[$index], [ref]$number) -or $number -lt 0 -or $number -gt 65535) {
        throw 'Every numeric Version component must be between 0 and 65535.'
    }
    $normalizedVersion[$index] = $number
}
if ([string]::Join('.', $normalizedVersion) -ne [string]::Join('.', $fixedParts)) {
    throw "Version '$Version' and FixedVersion '$FixedVersion' do not describe the same numeric release."
}

$installerPath = Get-CanonicalPath -Path $InstallerScript
$outputPath = Get-CanonicalPath -Path $OutputFile
$packagePath = Get-CanonicalPath -Path $PackageDirectory
$assetsPath = Get-CanonicalPath -Path $AssetsDirectory
$pathHelperPath = Get-CanonicalPath -Path $PathHelper
$quietHelperPath = Get-CanonicalPath -Path $QuietUninstallHelper

Assert-ExistingRegularFile -Name InstallerScript -Path $installerPath | Out-Null
Assert-ExistingRegularDirectory -Name PackageDirectory -Path $packagePath | Out-Null
Assert-ExistingRegularDirectory -Name AssetsDirectory -Path $assetsPath | Out-Null
Assert-NoReparseInExistingPath -Path ([IO.Path]::GetDirectoryName($outputPath))

if ([IO.Path]::GetFileName($outputPath) -ine $OutputFileName) {
    throw 'OutputFileName does not match the basename of OutputFile.'
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
if ([IO.Path]::GetExtension($pathHelperPath) -ine '.ps1' -or
    [IO.Path]::GetExtension($quietHelperPath) -ine '.ps1') {
    throw 'Both helper source files must use the .ps1 extension.'
}

$pathHelperItem = Assert-ExistingRegularFile -Name PathHelper -Path $pathHelperPath
$quietHelperItem = Assert-ExistingRegularFile -Name QuietUninstallHelper -Path $quietHelperPath
Assert-PowerShellSyntax -Path $pathHelperPath
Assert-PowerShellSyntax -Path $quietHelperPath

$payloadItems = @()
foreach ($name in @($AppExecutable, $AppBaseScript, $AppStackScript, $AppLicense)) {
    $payloadItems += Assert-ExistingRegularFile -Name $name -Path (Join-Path $packagePath $name)
}
Assert-Amd64Pe -Path (Join-Path $packagePath $AppExecutable)
if ($RequirePayloadSignature) {
    Assert-ValidSignature -Path (Join-Path $packagePath $AppExecutable)
}

$bitmapSpecifications = [ordered]@{
    'installer-welcome-finish-164x314.bmp' = @(164, 314)
    'installer-welcome-finish-205x393.bmp' = @(205, 393)
    'installer-welcome-finish-246x471.bmp' = @(246, 471)
    'installer-welcome-finish-287x550.bmp' = @(287, 550)
    'installer-welcome-finish-328x628.bmp' = @(328, 628)
}
$assetItems = @()
foreach ($entry in $bitmapSpecifications.GetEnumerator()) {
    $assetPath = Join-Path $assetsPath $entry.Key
    $assetItems += Assert-ExistingRegularFile -Name $entry.Key -Path $assetPath
    Assert-Bitmap -Path $assetPath -ExpectedWidth $entry.Value[0] -ExpectedHeight $entry.Value[1]
}

$sourcePaths = @(
    $installerPath,
    $pathHelperPath,
    $quietHelperPath
)
$sourcePaths += @($payloadItems | ForEach-Object { $_.FullName })
$sourcePaths += @($assetItems | ForEach-Object { $_.FullName })
foreach ($sourcePath in $sourcePaths) {
    if ([string]$sourcePath -ieq $outputPath) {
        throw 'OutputFile must not overwrite an installer source or payload file.'
    }
}

$embeddedBytes = [long]$pathHelperItem.Length + [long]$quietHelperItem.Length
$embeddedBytes += ($payloadItems | Measure-Object -Property Length -Sum).Sum
$embeddedBytes += ($assetItems | Measure-Object -Property Length -Sum).Sum
if ($embeddedBytes -gt $MaximumEmbeddedBytes) {
    throw "Embedded source data is too large for the configured solid-compression safety limit: $embeddedBytes bytes."
}

[Console]::Out.WriteLine("Installer build inputs validated successfully. Embedded input bytes: $embeddedBytes")
