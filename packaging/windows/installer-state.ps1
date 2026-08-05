param(
    [Parameter(Mandatory = $true)]
    [ValidateSet('Install', 'RollbackInstall', 'CommitInstall', 'InspectUninstall', 'MarkCleanupComplete', 'FinishUninstall')]
    [string]$Action,
    [Parameter(Mandatory = $true)]
    [ValidateNotNullOrEmpty()]
    [string]$DefinitionPath,
    [Parameter(Mandatory = $true)]
    [ValidateNotNullOrEmpty()]
    [string]$InstallDirectory,
    [string]$PackageDirectory = ''
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version 2.0

$script:Utf8NoBom = New-Object Text.UTF8Encoding($false)
$script:UninstallRegistryRoot = 'Software\Microsoft\Windows\CurrentVersion\Uninstall'
$script:ManagedRegistryValues = @(
    'DisplayName',
    'DisplayVersion',
    'Publisher',
    'DisplayIcon',
    'InstallLocation',
    'URLInfoAbout',
    'UninstallString',
    'QuietUninstallString',
    'NoModify',
    'NoRepair',
    'ProductGuid',
    'InstallationId',
    'InstallerSchemaVersion',
    'PathAdded',
    'PathEntry',
    'UninstallPhase'
)

Add-Type -TypeDefinition @'
using System;
using System.Runtime.InteropServices;
public static class InstallerFileNative {
    [DllImport("kernel32.dll", CharSet = CharSet.Unicode, SetLastError = true)]
    public static extern bool MoveFileEx(string existing, string replacement, int flags);
}
'@

function Move-FileReplace {
    param(
        [Parameter(Mandatory = $true)][string]$Source,
        [Parameter(Mandatory = $true)][string]$Destination
    )

    if (-not [InstallerFileNative]::MoveFileEx($Source, $Destination, 0x9)) {
        $errorCode = [Runtime.InteropServices.Marshal]::GetLastWin32Error()
        throw (New-Object ComponentModel.Win32Exception($errorCode, "Atomically replace $Destination"))
    }
}

function Copy-FileDurable {
    param(
        [Parameter(Mandatory = $true)][string]$Source,
        [Parameter(Mandatory = $true)][string]$Destination
    )

    [void](Assert-RegularFile -Path $Source -Role 'durable copy source')
    $input = [IO.File]::Open($Source, [IO.FileMode]::Open, [IO.FileAccess]::Read, [IO.FileShare]::Read)
    try {
        $output = [IO.FileStream]::new(
            $Destination,
            [IO.FileMode]::CreateNew,
            [IO.FileAccess]::Write,
            [IO.FileShare]::None,
            81920,
            [IO.FileOptions]::WriteThrough
        )
        try {
            $input.CopyTo($output)
            $output.Flush($true)
        }
        finally {
            $output.Dispose()
        }
    }
    finally {
        $input.Dispose()
    }
}

function Write-InstallerLog {
    param([Parameter(Mandatory = $true)][string]$Message)

    try {
        $name = ([string]$script:Definition.applicationName) + '-installer.log'
        $path = Join-Path $env:TEMP $name
        if (Test-Path -LiteralPath $path -PathType Leaf) {
            $item = Get-Item -LiteralPath $path -Force
            if ($item.Length -gt 262144) {
                $previous = $path + '.previous'
                if (Test-Path -LiteralPath $previous) {
                    Remove-Item -LiteralPath $previous -Force
                }
                Move-Item -LiteralPath $path -Destination $previous -Force
            }
        }
        $record = [ordered]@{
            ts = [DateTime]::UtcNow.ToString('o')
            schema = 1
            level = $(if ($Message.StartsWith('failure:', [StringComparison]::Ordinal)) { 'error' } else { 'info' })
            operation = $Action.ToLowerInvariant()
            transactionId = $(if ($null -ne $script:CurrentTransactionId) { [string]$script:CurrentTransactionId } else { $null })
            productGuid = $(if ($null -ne $script:Definition) { [string]$script:Definition.productGuid } else { $null })
            message = $Message
        }
        $line = ($record | ConvertTo-Json -Compress) + [Environment]::NewLine
        [IO.File]::AppendAllText($path, $line, $script:Utf8NoBom)
    }
    catch {
        # Diagnostics are best effort and never widen installer ownership.
    }
}

function Assert-ExactProperties {
    param(
        [Parameter(Mandatory = $true)]$Object,
        [Parameter(Mandatory = $true)][string[]]$Expected,
        [Parameter(Mandatory = $true)][string]$Role
    )

    if ($null -eq $Object) {
        throw "$Role is missing."
    }
    $actual = @($Object.PSObject.Properties.Name | Sort-Object)
    $wanted = @($Expected | Sort-Object)
    if ([string]::Join("`n", $actual) -cne [string]::Join("`n", $wanted)) {
        throw "$Role properties are invalid: $([string]::Join(', ', $actual))."
    }
}

function Assert-LeafName {
    param(
        [Parameter(Mandatory = $true)][string]$Value,
        [Parameter(Mandatory = $true)][string]$Role
    )

    if ([string]::IsNullOrWhiteSpace($Value) -or $Value -cne $Value.Trim() -or
        $Value.Length -gt 120 -or $Value -eq '.' -or $Value -eq '..' -or
        $Value.IndexOfAny([char[]]'<>:"/\|?*') -ge 0 -or
        $Value.EndsWith('.') -or $Value.EndsWith(' ')) {
        throw "$Role is not a safe bounded leaf name."
    }
    foreach ($character in $Value.ToCharArray()) {
        if ([int]$character -lt 32) {
            throw "$Role contains a control character."
        }
    }
    $base = [IO.Path]::GetFileNameWithoutExtension($Value).ToUpperInvariant()
    if ($base -in @('CON', 'PRN', 'AUX', 'NUL') -or $base -match '^(COM|LPT)[1-9]$') {
        throw "$Role uses a reserved Windows device name."
    }
}

function Get-NormalizedPath {
    param([Parameter(Mandatory = $true)][string]$Path)
    return [IO.Path]::GetFullPath($Path).TrimEnd([char[]]@('\'))
}

function Test-PathsEqual {
    param([string]$Left, [string]$Right)
    return [string]::Equals(
        (Get-NormalizedPath -Path $Left),
        (Get-NormalizedPath -Path $Right),
        [StringComparison]::OrdinalIgnoreCase
    )
}

function Assert-RegularFile {
    param(
        [Parameter(Mandatory = $true)][string]$Path,
        [Parameter(Mandatory = $true)][string]$Role
    )

    $item = Get-Item -LiteralPath $Path -Force
    if ($item -isnot [IO.FileInfo] -or
        (($item.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) -or
        $item.Length -le 0) {
        throw "$Role is not a nonempty regular non-reparse file."
    }
    return $item
}

function Assert-NoReparsePath {
    param(
        [Parameter(Mandatory = $true)][string]$Path,
        [Parameter(Mandatory = $true)][string]$Boundary
    )

    $candidate = Get-NormalizedPath -Path $Path
    $root = Get-NormalizedPath -Path $Boundary
    while ($true) {
        if (-not ($candidate -ieq $root) -and -not $candidate.StartsWith($root + '\', [StringComparison]::OrdinalIgnoreCase)) {
            throw "Path escaped its installer boundary."
        }
        if (Test-Path -LiteralPath $candidate) {
            $item = Get-Item -LiteralPath $candidate -Force
            if (($item.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {
                throw "Installer path contains a reparse point: $candidate"
            }
        }
        if ($candidate -ieq $root) {
            break
        }
        $parent = Split-Path -Parent $candidate
        if ([string]::IsNullOrEmpty($parent) -or $parent -eq $candidate) {
            throw 'Installer path boundary could not be reached.'
        }
        $candidate = $parent
    }
}

function Get-FileSHA256 {
    param([Parameter(Mandatory = $true)][string]$Path)

    $stream = [IO.File]::Open($Path, [IO.FileMode]::Open, [IO.FileAccess]::Read, [IO.FileShare]::Read)
    try {
        $hasher = [Security.Cryptography.SHA256]::Create()
        try {
            return ([BitConverter]::ToString($hasher.ComputeHash($stream))).Replace('-', '').ToLowerInvariant()
        }
        finally {
            $hasher.Dispose()
        }
    }
    finally {
        $stream.Dispose()
    }
}

function Write-JsonAtomic {
    param(
        [Parameter(Mandatory = $true)][string]$Path,
        [Parameter(Mandatory = $true)]$Value
    )

    $directory = Split-Path -Parent $Path
    if (-not (Test-Path -LiteralPath $directory -PathType Container)) {
        throw "JSON owner directory is missing: $directory"
    }
    $temporary = Join-Path $directory ('.json-' + [Guid]::NewGuid().ToString('N') + '.tmp')
    try {
        $json = $Value | ConvertTo-Json -Depth 12
        $bytes = $script:Utf8NoBom.GetBytes($json + [Environment]::NewLine)
        $stream = [IO.FileStream]::new(
            $temporary,
            [IO.FileMode]::CreateNew,
            [IO.FileAccess]::Write,
            [IO.FileShare]::None,
            4096,
            [IO.FileOptions]::WriteThrough
        )
        try {
            $stream.Write($bytes, 0, $bytes.Length)
            $stream.Flush($true)
        }
        finally {
            $stream.Dispose()
        }
        Move-FileReplace -Source $temporary -Destination $Path
    }
    finally {
        if (Test-Path -LiteralPath $temporary) {
            Remove-Item -LiteralPath $temporary -Force
        }
    }
}

function Read-JsonFile {
    param(
        [Parameter(Mandatory = $true)][string]$Path,
        [Parameter(Mandatory = $true)][string]$Role
    )

    [void](Assert-RegularFile -Path $Path -Role $Role)
    $raw = [IO.File]::ReadAllText($Path, [Text.Encoding]::UTF8)
    try {
        return $raw | ConvertFrom-Json
    }
    catch {
        throw "$Role is not valid JSON: $($_.Exception.Message)"
    }
}

function Read-Definition {
    param([Parameter(Mandatory = $true)][string]$Path)

    $definition = Read-JsonFile -Path $Path -Role 'installer definition'
    Assert-ExactProperties -Object $definition -Role 'installer definition' -Expected @(
        'schemaVersion', 'installerSchemaVersion', 'productGuid', 'applicationName',
        'displayName', 'version', 'publisher', 'productUrl', 'installDirectoryName',
        'registryKeyName', 'executableName', 'markerFileName',
        'quietUninstallHelperName', 'uninstallerName', 'outputFileName', 'ownedFiles', 'legacy'
    )
    if ([int]$definition.schemaVersion -ne 1 -or [int]$definition.installerSchemaVersion -ne 1) {
        throw 'Installer definition schema is unsupported.'
    }
    $productGuid = [Guid]::Empty
    if (-not [Guid]::TryParseExact([string]$definition.productGuid, 'D', [ref]$productGuid) -or
        $productGuid.ToString('D') -cne [string]$definition.productGuid) {
        throw 'Installer product GUID is invalid or noncanonical.'
    }
    $expectedRegistryKey = '{' + $productGuid.ToString('D').ToUpperInvariant() + '}'
    if ([string]$definition.registryKeyName -cne $expectedRegistryKey) {
        throw 'Installer registry key is not the fixed product GUID.'
    }
    foreach ($property in @('applicationName', 'installDirectoryName', 'executableName', 'markerFileName', 'quietUninstallHelperName', 'uninstallerName', 'outputFileName')) {
        Assert-LeafName -Value ([string]$definition.$property) -Role "installer definition $property"
    }
    foreach ($property in @('displayName', 'version', 'publisher', 'productUrl')) {
        $value = [string]$definition.$property
        if ([string]::IsNullOrWhiteSpace($value) -or $value -cne $value.Trim() -or $value.Length -gt 200) {
            throw "Installer definition $property is invalid."
        }
    }
    $candidateVersion = $null
    if (-not [Version]::TryParse([string]$definition.version, [ref]$candidateVersion) -or $candidateVersion.ToString(3) -cne [string]$definition.version) {
        throw 'Installer version is invalid or noncanonical.'
    }
    $owned = @([string[]]$definition.ownedFiles)
    if ($owned.Count -lt 3 -or $owned.Count -gt 16) {
        throw 'Installer owned-file set is outside its bounded contract.'
    }
    $seen = @{}
    foreach ($name in $owned + @([string]$definition.markerFileName)) {
        Assert-LeafName -Value $name -Role 'installer owned file'
        $key = $name.ToLowerInvariant()
        if ($seen.ContainsKey($key)) {
            throw "Installer owned-file names collide: $name"
        }
        $seen[$key] = $true
    }
    foreach ($required in @([string]$definition.executableName, [string]$definition.quietUninstallHelperName, [string]$definition.uninstallerName)) {
        if (-not ($owned -ccontains $required)) {
            throw "Installer owned-file set is missing $required."
        }
    }
    Assert-ExactProperties -Object $definition.legacy -Role 'legacy installer definition' -Expected @('version', 'registryKeyName', 'files')
    Assert-LeafName -Value ([string]$definition.legacy.registryKeyName) -Role 'legacy registry key'
    $legacyVersion = $null
    if (-not [Version]::TryParse([string]$definition.legacy.version, [ref]$legacyVersion)) {
        throw 'Legacy installer version is invalid.'
    }
    foreach ($file in @($definition.legacy.files)) {
        Assert-ExactProperties -Object $file -Role 'legacy installer file' -Expected @('name', 'sha256')
        Assert-LeafName -Value ([string]$file.name) -Role 'legacy installer file'
        if ([string]$file.sha256 -cnotmatch '^[0-9a-f]{64}$') {
            throw 'Legacy installer SHA-256 is invalid.'
        }
    }
    return $definition
}

function Get-RegistryPath {
    param([Parameter(Mandatory = $true)][string]$KeyName)
    return $script:UninstallRegistryRoot + '\' + $KeyName
}

function Get-RegistryValueRecord {
    param(
        [Parameter(Mandatory = $true)]$Key,
        [Parameter(Mandatory = $true)][string]$Name
    )

    if (-not (@($Key.GetValueNames()) -icontains $Name)) {
        return [pscustomobject]@{ name = $Name; exists = $false; kind = ''; data = $null }
    }
    $kind = $Key.GetValueKind($Name)
    $value = $Key.GetValue($Name, $null, [Microsoft.Win32.RegistryValueOptions]::DoNotExpandEnvironmentNames)
    $data = switch ([string]$kind) {
        'String' { [string]$value }
        'ExpandString' { [string]$value }
        'DWord' { [string]([uint32]$value) }
        'QWord' { [string]([uint64]$value) }
        'MultiString' { @([string[]]$value) }
        'Binary' { [Convert]::ToBase64String([byte[]]$value) }
        'None' { [Convert]::ToBase64String([byte[]]$value) }
        default { throw "Registry value $Name has unsupported kind $kind." }
    }
    return [pscustomobject]@{ name = $Name; exists = $true; kind = [string]$kind; data = $data }
}

function Get-RegistryKeySnapshot {
    param([Parameter(Mandatory = $true)][string]$KeyName)

    $path = Get-RegistryPath -KeyName $KeyName
    $key = [Microsoft.Win32.Registry]::CurrentUser.OpenSubKey($path, $false)
    if ($null -eq $key) {
        return [pscustomobject]@{ exists = $false; values = @() }
    }
    try {
        $values = @()
        foreach ($name in $script:ManagedRegistryValues) {
            $values += Get-RegistryValueRecord -Key $key -Name $name
        }
        return [pscustomobject]@{ exists = $true; values = $values }
    }
    finally {
        $key.Dispose()
    }
}

function Set-RegistryValueRecord {
    param(
        [Parameter(Mandatory = $true)]$Key,
        [Parameter(Mandatory = $true)]$Record
    )

    if (-not [bool]$Record.exists) {
        $Key.DeleteValue([string]$Record.name, $false)
        return
    }
    $kind = [Microsoft.Win32.RegistryValueKind][Enum]::Parse(
        [Microsoft.Win32.RegistryValueKind],
        [string]$Record.kind
    )
    $value = switch ([string]$Record.kind) {
        'String' { [string]$Record.data }
        'ExpandString' { [string]$Record.data }
        'DWord' { [uint32]::Parse([string]$Record.data, [Globalization.CultureInfo]::InvariantCulture) }
        'QWord' { [uint64]::Parse([string]$Record.data, [Globalization.CultureInfo]::InvariantCulture) }
        'MultiString' { [string[]]$Record.data }
        'Binary' { [Convert]::FromBase64String([string]$Record.data) }
        'None' { [Convert]::FromBase64String([string]$Record.data) }
        default { throw "Registry snapshot kind is unsupported: $($Record.kind)." }
    }
    $Key.SetValue([string]$Record.name, $value, $kind)
}

function Remove-RegistryKeyIfEmpty {
    param([Parameter(Mandatory = $true)][string]$KeyName)

    $path = Get-RegistryPath -KeyName $KeyName
    $key = [Microsoft.Win32.Registry]::CurrentUser.OpenSubKey($path, $false)
    if ($null -eq $key) {
        return $false
    }
    try {
        if ($key.GetValueNames().Count -ne 0 -or $key.GetSubKeyNames().Count -ne 0) {
            return $true
        }
    }
    finally {
        $key.Dispose()
    }
    [Microsoft.Win32.Registry]::CurrentUser.DeleteSubKey($path, $false)
    return $false
}

function Restore-RegistryKeySnapshot {
    param(
        [Parameter(Mandatory = $true)][string]$KeyName,
        [Parameter(Mandatory = $true)]$Snapshot
    )

    $path = Get-RegistryPath -KeyName $KeyName
    $key = [Microsoft.Win32.Registry]::CurrentUser.OpenSubKey($path, $true)
    if ([bool]$Snapshot.exists -and $null -eq $key) {
        $key = [Microsoft.Win32.Registry]::CurrentUser.CreateSubKey($path)
    }
    if ($null -ne $key) {
        try {
            foreach ($record in @($Snapshot.values)) {
                Set-RegistryValueRecord -Key $key -Record $record
            }
            if (-not [bool]$Snapshot.exists) {
                foreach ($name in $script:ManagedRegistryValues) {
                    $key.DeleteValue($name, $false)
                }
            }
        }
        finally {
            $key.Dispose()
        }
    }
    if (-not [bool]$Snapshot.exists) {
        [void](Remove-RegistryKeyIfEmpty -KeyName $KeyName)
    }
}

function Get-RequiredRegistryValue {
    param(
        [Parameter(Mandatory = $true)]$Key,
        [Parameter(Mandatory = $true)][string]$Name,
        [Parameter(Mandatory = $true)][Microsoft.Win32.RegistryValueKind]$Kind
    )

    if (-not (@($Key.GetValueNames()) -icontains $Name)) {
        throw "Installer registration is missing $Name."
    }
    $actualKind = $Key.GetValueKind($Name)
    if ($actualKind -ne $Kind) {
        throw "Installer registration $Name has kind $actualKind, want $Kind."
    }
    return $Key.GetValue($Name, $null, [Microsoft.Win32.RegistryValueOptions]::DoNotExpandEnvironmentNames)
}

function Get-PathSnapshot {
    $key = [Microsoft.Win32.Registry]::CurrentUser.OpenSubKey('Environment', $false)
    if ($null -eq $key) {
        return [pscustomobject]@{ keyExists = $false; exists = $false; kind = ''; data = '' }
    }
    try {
        if (-not (@($key.GetValueNames()) -icontains 'Path')) {
            return [pscustomobject]@{ keyExists = $true; exists = $false; kind = ''; data = '' }
        }
        $kind = $key.GetValueKind('Path')
        if ($kind -ne [Microsoft.Win32.RegistryValueKind]::String -and
            $kind -ne [Microsoft.Win32.RegistryValueKind]::ExpandString) {
            throw "The current-user Path has unsupported registry kind '$kind'."
        }
        $value = [string]$key.GetValue('Path', '', [Microsoft.Win32.RegistryValueOptions]::DoNotExpandEnvironmentNames)
        return [pscustomobject]@{ keyExists = $true; exists = $true; kind = [string]$kind; data = $value }
    }
    finally {
        $key.Dispose()
    }
}

function Set-PathSnapshot {
    param([Parameter(Mandatory = $true)]$Snapshot)

    $key = [Microsoft.Win32.Registry]::CurrentUser.OpenSubKey('Environment', $true)
    if ($null -eq $key -and [bool]$Snapshot.exists) {
        $key = [Microsoft.Win32.Registry]::CurrentUser.CreateSubKey('Environment')
    }
    if ($null -eq $key) {
        return
    }
    try {
        if ([bool]$Snapshot.exists) {
            $kind = [Microsoft.Win32.RegistryValueKind][Enum]::Parse(
                [Microsoft.Win32.RegistryValueKind],
                [string]$Snapshot.kind
            )
            $key.SetValue('Path', [string]$Snapshot.data, $kind)
        }
        else {
            $key.DeleteValue('Path', $false)
        }
    }
    finally {
        $key.Dispose()
    }
}

function Test-PathEntry {
    param(
        [string]$Entry,
        [string]$Expected,
        [bool]$ExpandVariables
    )

    $candidate = $Entry.Trim().Trim([char[]]@('"'))
    if ([string]::IsNullOrWhiteSpace($candidate)) {
        return $false
    }
    try {
        if ($ExpandVariables) {
            $candidate = [Environment]::ExpandEnvironmentVariables($candidate)
        }
        return (Get-NormalizedPath -Path $candidate) -ieq $Expected
    }
    catch {
        return $candidate.TrimEnd([char[]]@('\')) -ieq $Expected
    }
}

function Resolve-UserPathUpdate {
    param(
        [AllowEmptyString()][string]$Current,
        [Parameter(Mandatory = $true)][string]$Expected,
        [Parameter(Mandatory = $true)][ValidateSet('Add', 'Remove')][string]$RequestedAction,
        [bool]$ExpandVariables
    )

    $entries = @([regex]::Split($Current, ';'))
    $matches = @($entries | Where-Object { Test-PathEntry -Entry $_ -Expected $Expected -ExpandVariables $ExpandVariables })
    if ($RequestedAction -eq 'Add') {
        if ($matches.Count -gt 0) {
            return [pscustomobject]@{ Changed = $false; Value = $Current }
        }
        $updated = if ([string]::IsNullOrEmpty($Current)) { $Expected } else { $Current + ';' + $Expected }
        return [pscustomobject]@{ Changed = $true; Value = $updated }
    }
    $removeIndex = -1
    for ($index = $entries.Count - 1; $index -ge 0; $index -= 1) {
        if ([string]$entries[$index] -ceq $Expected) {
            $removeIndex = $index
            break
        }
    }
    if ($removeIndex -lt 0) {
        return [pscustomobject]@{ Changed = $false; Value = $Current }
    }
    $kept = New-Object 'Collections.Generic.List[string]'
    for ($index = 0; $index -lt $entries.Count; $index += 1) {
        if ($index -ne $removeIndex) {
            [void]$kept.Add([string]$entries[$index])
        }
    }
    return [pscustomobject]@{ Changed = $true; Value = [string]::Join(';', [string[]]$kept) }
}

function Set-CurrentUserPath {
    param(
        [Parameter(Mandatory = $true)][string]$Action,
        [Parameter(Mandatory = $true)][string]$Expected
    )

    for ($attempt = 1; $attempt -le 3; $attempt += 1) {
        $snapshot = Get-PathSnapshot
        $kind = if ([bool]$snapshot.exists) {
            [Microsoft.Win32.RegistryValueKind][Enum]::Parse(
                [Microsoft.Win32.RegistryValueKind],
                [string]$snapshot.kind
            )
        }
        else {
            [Microsoft.Win32.RegistryValueKind]::ExpandString
        }
        $current = if ([bool]$snapshot.exists) { [string]$snapshot.data } else { '' }
        $update = Resolve-UserPathUpdate -Current $current -Expected $Expected -RequestedAction $Action `
            -ExpandVariables ($kind -eq [Microsoft.Win32.RegistryValueKind]::ExpandString)
        if (-not [bool]$update.Changed) {
            return $update
        }

        $latest = Get-PathSnapshot
        if (($snapshot | ConvertTo-Json -Compress -Depth 4) -cne ($latest | ConvertTo-Json -Compress -Depth 4)) {
            continue
        }
        $key = [Microsoft.Win32.Registry]::CurrentUser.OpenSubKey('Environment', $true)
        if ($null -eq $key) {
            $key = [Microsoft.Win32.Registry]::CurrentUser.CreateSubKey('Environment')
        }
        if ($null -eq $key) {
            throw 'Could not open the current-user Environment registry key.'
        }
        try {
            $key.SetValue('Path', [string]$update.Value, $kind)
        }
        finally {
            $key.Dispose()
        }
        $readback = Get-PathSnapshot
        if (-not [bool]$readback.exists -or [string]$readback.kind -cne [string]$kind -or
            [string]$readback.data -cne [string]$update.Value) {
            throw 'Current-user PATH write did not pass exact type/data read-back.'
        }
        return $update
    }
    throw 'Current-user PATH changed concurrently three times; no installer PATH write was applied.'
}

function Get-Marker {
    param(
        [Parameter(Mandatory = $true)][string]$Path,
        [Parameter(Mandatory = $true)]$Definition,
        [switch]$AllowMissingOwnedFiles
    )

    $marker = Read-JsonFile -Path $Path -Role 'installer ownership marker'
    Assert-ExactProperties -Object $marker -Role 'installer ownership marker' -Expected @(
        'schemaVersion', 'productGuid', 'installationId', 'installLocation', 'installedVersion', 'ownedFiles'
    )
    if ([int]$marker.schemaVersion -ne [int]$Definition.installerSchemaVersion -or
        [string]$marker.productGuid -cne [string]$Definition.productGuid -or
        -not (Test-PathsEqual -Left ([string]$marker.installLocation) -Right $script:InstallDirectory)) {
        throw 'Installer ownership marker identity is invalid.'
    }
    $installationId = [Guid]::Empty
    if (-not [Guid]::TryParseExact([string]$marker.installationId, 'D', [ref]$installationId) -or
        $installationId.ToString('D') -cne [string]$marker.installationId) {
        throw 'Installer ownership marker installation ID is invalid.'
    }
    $version = $null
    if (-not [Version]::TryParse([string]$marker.installedVersion, [ref]$version)) {
        throw 'Installer ownership marker version is invalid.'
    }
    $records = @($marker.ownedFiles)
    if ($records.Count -ne @($Definition.ownedFiles).Count) {
        throw 'Installer ownership marker file count is invalid.'
    }
    $expected = @([string[]]$Definition.ownedFiles | Sort-Object)
    $actual = @($records | ForEach-Object { [string]$_.name } | Sort-Object)
    if ([string]::Join("`n", $actual) -cne [string]::Join("`n", $expected)) {
        throw 'Installer ownership marker file names are invalid.'
    }
    foreach ($record in $records) {
        Assert-ExactProperties -Object $record -Role 'installer ownership file record' -Expected @('name', 'sha256', 'size')
        Assert-LeafName -Value ([string]$record.name) -Role 'installer ownership file'
        if ([string]$record.sha256 -cnotmatch '^[0-9a-f]{64}$' -or [int64]$record.size -le 0) {
            throw 'Installer ownership file hash is invalid.'
        }
        $installedPath = Join-Path $script:InstallDirectory ([string]$record.name)
        if (-not (Test-Path -LiteralPath $installedPath)) {
            if ($AllowMissingOwnedFiles) {
                continue
            }
            throw "Installer-owned file is missing: $($record.name)"
        }
        $installed = Assert-RegularFile -Path $installedPath -Role 'installer-owned file'
        if ([int64]$installed.Length -ne [int64]$record.size) {
            throw "Installer-owned file has an unexpected size: $($record.name)"
        }
        if ((Get-FileSHA256 -Path $installedPath) -cne [string]$record.sha256) {
            throw "Installer-owned file has unknown content: $($record.name)"
        }
    }
    return $marker
}

function Get-RegistrationState {
    param([Parameter(Mandatory = $true)]$Definition)

    $currentPath = Get-RegistryPath -KeyName ([string]$Definition.registryKeyName)
    $legacyPath = Get-RegistryPath -KeyName ([string]$Definition.legacy.registryKeyName)
    $current = [Microsoft.Win32.Registry]::CurrentUser.OpenSubKey($currentPath, $false)
    $legacy = [Microsoft.Win32.Registry]::CurrentUser.OpenSubKey($legacyPath, $false)
    try {
        if ($null -ne $current -and $null -ne $legacy) {
            throw 'Current and legacy installer registrations both exist.'
        }
        if ($null -ne $current) {
            $productGuid = [string](Get-RequiredRegistryValue -Key $current -Name 'ProductGuid' -Kind String)
            $installationId = [string](Get-RequiredRegistryValue -Key $current -Name 'InstallationId' -Kind String)
            $schema = [uint32](Get-RequiredRegistryValue -Key $current -Name 'InstallerSchemaVersion' -Kind DWord)
            $location = [string](Get-RequiredRegistryValue -Key $current -Name 'InstallLocation' -Kind String)
            $displayName = [string](Get-RequiredRegistryValue -Key $current -Name 'DisplayName' -Kind String)
            $publisher = [string](Get-RequiredRegistryValue -Key $current -Name 'Publisher' -Kind String)
            $displayVersion = [string](Get-RequiredRegistryValue -Key $current -Name 'DisplayVersion' -Kind String)
            $displayIcon = [string](Get-RequiredRegistryValue -Key $current -Name 'DisplayIcon' -Kind String)
            $productUrl = [string](Get-RequiredRegistryValue -Key $current -Name 'URLInfoAbout' -Kind String)
            $uninstallString = [string](Get-RequiredRegistryValue -Key $current -Name 'UninstallString' -Kind String)
            $quietString = [string](Get-RequiredRegistryValue -Key $current -Name 'QuietUninstallString' -Kind String)
            $pathAdded = [uint32](Get-RequiredRegistryValue -Key $current -Name 'PathAdded' -Kind DWord)
            $phase = [string](Get-RequiredRegistryValue -Key $current -Name 'UninstallPhase' -Kind String)
            $uninstaller = Join-Path $script:InstallDirectory ([string]$Definition.uninstallerName)
            $quietHelper = Join-Path $script:InstallDirectory ([string]$Definition.quietUninstallHelperName)
            $powershell = Join-Path $env:SystemRoot 'System32\WindowsPowerShell\v1.0\powershell.exe'
            $expectedQuiet = '"{0}" -NoLogo -NoProfile -NonInteractive -WindowStyle Hidden -ExecutionPolicy Bypass -File "{1}" -Uninstaller "{2}" -InstallDirectory "{3}"' -f $powershell, $quietHelper, $uninstaller, $script:InstallDirectory
            if ($productGuid -cne [string]$Definition.productGuid -or $schema -ne [int]$Definition.installerSchemaVersion -or
                -not (Test-PathsEqual -Left $location -Right $script:InstallDirectory) -or
                $displayName -cne [string]$Definition.displayName -or $publisher -cne [string]$Definition.publisher -or
                $displayIcon -cne ('"' + (Join-Path $script:InstallDirectory ([string]$Definition.executableName)) + '",0') -or
                $productUrl -cne [string]$Definition.productUrl -or $uninstallString -cne ('"' + $uninstaller + '"') -or
                $quietString -cne $expectedQuiet -or
                [uint32](Get-RequiredRegistryValue -Key $current -Name 'NoModify' -Kind DWord) -ne 1 -or
                [uint32](Get-RequiredRegistryValue -Key $current -Name 'NoRepair' -Kind DWord) -ne 1 -or
                $pathAdded -notin @(0, 1) -or $phase -notin @('Ready', 'CleanupComplete')) {
                throw 'Installer registration identity is invalid or corrupt.'
            }
            if ($pathAdded -eq 1) {
                $pathEntry = [string](Get-RequiredRegistryValue -Key $current -Name 'PathEntry' -Kind String)
                if (-not (Test-PathsEqual -Left $pathEntry -Right $script:InstallDirectory)) {
                    throw 'Installer PATH ownership registration is invalid.'
                }
            }
            elseif (@($current.GetValueNames()) -icontains 'PathEntry') {
                throw 'Installer registration contains an unowned PATH entry value.'
            }
            $markerPath = Join-Path $script:InstallDirectory ([string]$Definition.markerFileName)
            $marker = Get-Marker -Path $markerPath -Definition $Definition -AllowMissingOwnedFiles:($phase -eq 'CleanupComplete')
            if ([string]$marker.installationId -cne $installationId -or [string]$marker.installedVersion -cne $displayVersion) {
                throw 'Installer registry and directory identities disagree.'
            }
            return [pscustomobject]@{
                kind = 'Owned'; installationId = $installationId; version = $displayVersion
                pathAdded = [int]$pathAdded; uninstallPhase = $phase; marker = $marker
            }
        }
        if ($null -ne $legacy) {
            $legacyNames = @(
                'DisplayName', 'DisplayVersion', 'Publisher', 'DisplayIcon', 'InstallLocation',
                'URLInfoAbout', 'UninstallString', 'QuietUninstallString', 'NoModify', 'NoRepair', 'PathAdded'
            )
            $actualNames = @($legacy.GetValueNames() | Sort-Object)
            $expectedNames = @($legacyNames | Sort-Object)
            if ([string]::Join("`n", $actualNames) -cne [string]::Join("`n", $expectedNames) -or $legacy.GetSubKeyNames().Count -ne 0) {
                throw 'Legacy installer registration contains unknown state and was preserved.'
            }
            $displayName = [string](Get-RequiredRegistryValue -Key $legacy -Name 'DisplayName' -Kind String)
            $displayVersion = [string](Get-RequiredRegistryValue -Key $legacy -Name 'DisplayVersion' -Kind String)
            $publisher = [string](Get-RequiredRegistryValue -Key $legacy -Name 'Publisher' -Kind String)
            $location = [string](Get-RequiredRegistryValue -Key $legacy -Name 'InstallLocation' -Kind String)
            $displayIcon = [string](Get-RequiredRegistryValue -Key $legacy -Name 'DisplayIcon' -Kind String)
            $productUrl = [string](Get-RequiredRegistryValue -Key $legacy -Name 'URLInfoAbout' -Kind String)
            $uninstallString = [string](Get-RequiredRegistryValue -Key $legacy -Name 'UninstallString' -Kind String)
            $quietString = [string](Get-RequiredRegistryValue -Key $legacy -Name 'QuietUninstallString' -Kind String)
            $pathAdded = [uint32](Get-RequiredRegistryValue -Key $legacy -Name 'PathAdded' -Kind DWord)
            if ($displayName -cne [string]$Definition.displayName -or $displayVersion -cne [string]$Definition.legacy.version -or
                $publisher -cne [string]$Definition.publisher -or -not (Test-PathsEqual -Left $location -Right $script:InstallDirectory) -or
                $displayIcon -cne ('"' + (Join-Path $script:InstallDirectory ([string]$Definition.executableName)) + '",0') -or
                $productUrl -cne [string]$Definition.productUrl -or
                $uninstallString -cne ('"' + (Join-Path $script:InstallDirectory ([string]$Definition.uninstallerName)) + '"') -or
                $quietString -cne ('"' + (Join-Path $script:InstallDirectory ([string]$Definition.uninstallerName)) + '" /S') -or
                $pathAdded -notin @(0, 1) -or
                [uint32](Get-RequiredRegistryValue -Key $legacy -Name 'NoModify' -Kind DWord) -ne 1 -or
                [uint32](Get-RequiredRegistryValue -Key $legacy -Name 'NoRepair' -Kind DWord) -ne 1) {
                throw 'Legacy installer registration is not the exact supported v0.0.9 state.'
            }
            if (-not (Test-Path -LiteralPath $script:InstallDirectory -PathType Container)) {
                throw 'Legacy installer directory is missing.'
            }
            Assert-NoReparsePath -Path $script:InstallDirectory -Boundary $script:LocalAppData
            foreach ($file in @($Definition.legacy.files)) {
                $path = Join-Path $script:InstallDirectory ([string]$file.name)
                if (-not (Test-Path -LiteralPath $path)) {
                    continue
                }
                [void](Assert-RegularFile -Path $path -Role 'legacy installer file')
                if ((Get-FileSHA256 -Path $path) -cne [string]$file.sha256) {
                    throw "Legacy installer file failed published v0.0.9 verification: $($file.name)"
                }
            }
            $legacyNames = @($Definition.legacy.files | ForEach-Object { [string]$_.name }) + @([string]$Definition.uninstallerName)
            $reservedNames = @([string[]]$Definition.ownedFiles) + @([string]$Definition.markerFileName)
            foreach ($name in $reservedNames) {
                if ($legacyNames -ccontains $name) {
                    continue
                }
                if (Test-Path -LiteralPath (Join-Path $script:InstallDirectory $name)) {
                    throw "Legacy installer directory contains an unowned file at reserved installer path $name and was preserved."
                }
            }
            $uninstallerPath = Join-Path $script:InstallDirectory ([string]$Definition.uninstallerName)
            if (Test-Path -LiteralPath $uninstallerPath) {
                [void](Assert-RegularFile -Path $uninstallerPath -Role 'legacy uninstaller')
            }
            return [pscustomobject]@{
                kind = 'Legacy'; installationId = [Guid]::NewGuid().ToString('D'); version = $displayVersion
                pathAdded = [int]$pathAdded; uninstallPhase = 'Ready'; marker = $null
            }
        }
        if (Test-Path -LiteralPath $script:InstallDirectory) {
            $item = Get-Item -LiteralPath $script:InstallDirectory -Force
            if ($item -isnot [IO.DirectoryInfo] -or (($item.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0)) {
                throw 'Preexisting install path is not an owned regular directory.'
            }
            if (@(Get-ChildItem -LiteralPath $script:InstallDirectory -Force).Count -ne 0) {
                throw 'Preexisting unmarked install directory is not owned and was preserved.'
            }
        }
        return [pscustomobject]@{
            kind = 'Fresh'; installationId = [Guid]::NewGuid().ToString('D'); version = ''
            pathAdded = 0; uninstallPhase = 'Ready'; marker = $null
        }
    }
    finally {
        if ($null -ne $current) { $current.Dispose() }
        if ($null -ne $legacy) { $legacy.Dispose() }
    }
}

function Get-FileRecordsFromMarker {
    param([Parameter(Mandatory = $true)]$Marker)

    $records = @()
    foreach ($record in @($Marker.ownedFiles)) {
        $records += [pscustomobject]@{ name = [string]$record.name; sha256 = [string]$record.sha256; size = [int64]$record.size }
    }
    $markerPath = Join-Path $script:InstallDirectory ([string]$script:Definition.markerFileName)
    $records += [pscustomobject]@{
        name = [string]$script:Definition.markerFileName
        sha256 = Get-FileSHA256 -Path $markerPath
        size = (Get-Item -LiteralPath $markerPath -Force).Length
    }
    return $records
}

function Get-LegacyFileRecords {
    $records = @()
    foreach ($record in @($script:Definition.legacy.files)) {
        $path = Join-Path $script:InstallDirectory ([string]$record.name)
        if (-not (Test-Path -LiteralPath $path)) {
            continue
        }
        $records += [pscustomobject]@{ name = [string]$record.name; sha256 = [string]$record.sha256; size = (Get-Item -LiteralPath $path -Force).Length }
    }
    $uninstallerPath = Join-Path $script:InstallDirectory ([string]$script:Definition.uninstallerName)
    if (Test-Path -LiteralPath $uninstallerPath) {
        $records += [pscustomobject]@{
            name = [string]$script:Definition.uninstallerName
            sha256 = Get-FileSHA256 -Path $uninstallerPath
            size = (Get-Item -LiteralPath $uninstallerPath -Force).Length
        }
    }
    return $records
}

function Remove-SafeTransactionDirectory {
    if (-not (Test-Path -LiteralPath $script:TransactionDirectory)) {
        return
    }
    Assert-NoReparsePath -Path $script:TransactionDirectory -Boundary $script:LocalAppData
    foreach ($item in @(Get-ChildItem -LiteralPath $script:TransactionDirectory -Force -Recurse)) {
        if (($item.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {
            throw 'Installer transaction contains a reparse point and was preserved.'
        }
    }
    [IO.Directory]::Delete($script:TransactionDirectory, $true)
}

function Save-Transaction {
    param([Parameter(Mandatory = $true)]$State)
    $script:CurrentTransactionId = [string]$State.transactionId
    Write-JsonAtomic -Path $script:TransactionStatePath -Value $State
}

function Assert-BooleanValue {
    param($Value, [Parameter(Mandatory = $true)][string]$Role)
    if ($Value -isnot [bool]) {
        throw "$Role must be a JSON boolean."
    }
}

function Assert-RegistrySnapshot {
    param(
        [Parameter(Mandatory = $true)]$Snapshot,
        [Parameter(Mandatory = $true)][string]$Role
    )

    Assert-ExactProperties -Object $Snapshot -Role $Role -Expected @('exists', 'values')
    Assert-BooleanValue -Value $Snapshot.exists -Role "$Role exists"
    $records = @($Snapshot.values)
    if (-not [bool]$Snapshot.exists) {
        if ($records.Count -ne 0) {
            throw "$Role for a missing key must not contain values."
        }
        return
    }
    if ($records.Count -ne $script:ManagedRegistryValues.Count) {
        throw "$Role must contain every managed registry value record exactly once."
    }
    $seen = @{}
    foreach ($record in $records) {
        Assert-ExactProperties -Object $record -Role "$Role value" -Expected @('name', 'exists', 'kind', 'data')
        if ($record.name -isnot [string] -or $script:ManagedRegistryValues -cnotcontains [string]$record.name) {
            throw "$Role contains an unknown managed registry value."
        }
        $name = [string]$record.name
        if ($seen.ContainsKey($name)) {
            throw "$Role contains duplicate registry value $name."
        }
        $seen[$name] = $true
        Assert-BooleanValue -Value $record.exists -Role "$Role $name exists"
        if (-not [bool]$record.exists) {
            if ($record.kind -isnot [string] -or [string]$record.kind -cne '' -or $null -ne $record.data) {
                throw "$Role missing value $name has unexpected state."
            }
            continue
        }
        if ($record.kind -isnot [string] -or [string]$record.kind -notin @('String', 'ExpandString', 'DWord', 'QWord', 'MultiString', 'Binary', 'None')) {
            throw "$Role value $name has an unsupported kind."
        }
        switch ([string]$record.kind) {
            { $_ -in @('String', 'ExpandString') } {
                if ($record.data -isnot [string]) { throw "$Role value $name must contain string data." }
            }
            'DWord' {
                $number = [uint32]0
                if ($record.data -isnot [string] -or -not [uint32]::TryParse([string]$record.data, [ref]$number)) {
                    throw "$Role value $name has invalid DWORD data."
                }
            }
            'QWord' {
                $number = [uint64]0
                if ($record.data -isnot [string] -or -not [uint64]::TryParse([string]$record.data, [ref]$number)) {
                    throw "$Role value $name has invalid QWORD data."
                }
            }
            'MultiString' {
                if ($record.data -is [string]) { throw "$Role value $name must contain an array." }
                foreach ($entry in @($record.data)) {
                    if ($entry -isnot [string]) { throw "$Role value $name has invalid multi-string data." }
                }
            }
            { $_ -in @('Binary', 'None') } {
                if ($record.data -isnot [string]) { throw "$Role value $name must contain base64 data." }
                try { [void][Convert]::FromBase64String([string]$record.data) }
                catch { throw "$Role value $name has invalid base64 data." }
            }
        }
    }
}

function Assert-PathSnapshot {
    param(
        [Parameter(Mandatory = $true)]$Snapshot,
        [Parameter(Mandatory = $true)][string]$Role
    )

    Assert-ExactProperties -Object $Snapshot -Role $Role -Expected @('keyExists', 'exists', 'kind', 'data')
    Assert-BooleanValue -Value $Snapshot.keyExists -Role "$Role keyExists"
    Assert-BooleanValue -Value $Snapshot.exists -Role "$Role exists"
    if ([bool]$Snapshot.exists) {
        if (-not [bool]$Snapshot.keyExists -or $Snapshot.kind -isnot [string] -or
            [string]$Snapshot.kind -notin @('String', 'ExpandString') -or $Snapshot.data -isnot [string]) {
            throw "$Role contains invalid PATH value state."
        }
    }
    elseif ($Snapshot.kind -isnot [string] -or [string]$Snapshot.kind -cne '' -or
        $Snapshot.data -isnot [string] -or [string]$Snapshot.data -cne '') {
        throw "$Role missing PATH value has unexpected state."
    }
}

function Assert-TransactionFileRecords {
    param(
        $Records,
        [Parameter(Mandatory = $true)][AllowEmptyCollection()][string[]]$ExpectedNames,
        [Parameter(Mandatory = $true)][ValidateSet('Backup', 'New', 'Uninstall')][string]$Kind,
        [Parameter(Mandatory = $true)][string]$Role,
        [switch]$AllowMissing
    )

    $items = @($Records)
    $actualNames = @($items | ForEach-Object { [string]$_.name } | Sort-Object)
    $wantedNames = @($ExpectedNames | Sort-Object)
    $namesValid = if ($AllowMissing) {
        $actualNames.Count -eq @($actualNames | Select-Object -Unique).Count -and
            @($actualNames | Where-Object { $wantedNames -cnotcontains $_ }).Count -eq 0
    }
    else {
        $items.Count -eq $ExpectedNames.Count -and
            [string]::Join("`n", $actualNames) -ceq [string]::Join("`n", $wantedNames)
    }
    if (-not $namesValid) {
        throw "$Role file names do not match the exact expected set."
    }
    $backupNames = @{}
    foreach ($record in $items) {
        if ($Kind -eq 'New') {
            Assert-ExactProperties -Object $record -Role "$Role file" -Expected @('name', 'sha256', 'size')
        }
        else {
            Assert-ExactProperties -Object $record -Role "$Role file" -Expected @('name', 'sha256', 'backupName')
        }
        Assert-LeafName -Value ([string]$record.name) -Role "$Role file name"
        if ($record.sha256 -isnot [string] -or [string]$record.sha256 -cnotmatch '^[0-9a-f]{64}$') {
            throw "$Role file hash is invalid."
        }
        if ($Kind -eq 'New') {
            if ((($record.size -isnot [int]) -and ($record.size -isnot [long])) -or [int64]$record.size -le 0) {
                throw "$Role file size is invalid."
            }
        }
        elseif ($record.backupName -isnot [string]) {
            throw "$Role backup name is invalid."
        }
        elseif ($Kind -eq 'Backup') {
            $backupName = [string]$record.backupName
            if ($backupName -cnotmatch '^[0-9]{2}\.bin$' -or $backupNames.ContainsKey($backupName)) {
                throw "$Role backup name is invalid or duplicated."
            }
            $backupNames[$backupName] = $true
        }
        elseif ([string]$record.backupName -cne '') {
            throw "$Role uninstall file must not name a backup."
        }
    }
}

function Assert-TransactionState {
    param([Parameter(Mandatory = $true)]$State)

    if ($State.schemaVersion -isnot [int] -or [int]$State.schemaVersion -ne 1 -or
        $State.installDirectory -isnot [string] -or
        -not (Test-PathsEqual -Left ([string]$State.installDirectory) -Right $script:InstallDirectory)) {
        throw 'Installer transaction identity is invalid.'
    }
    foreach ($property in @('transactionId', 'kind', 'phase', 'installationId', 'installationState')) {
        if ($State.$property -isnot [string]) { throw "Installer transaction $property must be a string." }
    }
    foreach ($property in @('transactionId', 'installationId')) {
        $identifier = [Guid]::Empty
        if (-not [Guid]::TryParseExact([string]$State.$property, 'D', [ref]$identifier) -or
            $identifier.ToString('D') -cne [string]$State.$property) {
            throw "Installer transaction $property is invalid."
        }
    }
    Assert-RegistrySnapshot -Snapshot $State.currentRegistry -Role 'current registry snapshot'
    Assert-RegistrySnapshot -Snapshot $State.legacyRegistry -Role 'legacy registry snapshot'
    Assert-PathSnapshot -Snapshot $State.pathBefore -Role 'PATH before snapshot'
    if ($null -ne $State.pathAfter) { Assert-PathSnapshot -Snapshot $State.pathAfter -Role 'PATH after snapshot' }
    Assert-BooleanValue -Value $State.pathChanged -Role 'installer transaction pathChanged'
    Assert-BooleanValue -Value $State.cleanupComplete -Role 'installer transaction cleanupComplete'

    $currentExists = [bool]$State.currentRegistry.exists
    $legacyExists = [bool]$State.legacyRegistry.exists
    if ([string]$State.kind -eq 'Install') {
        if ([string]$State.phase -notin @('Prepared', 'FilesApplied', 'PathApplied', 'Applied', 'Committed') -or
            [string]$State.installationState -notin @('Fresh', 'Owned', 'Legacy') -or [bool]$State.cleanupComplete) {
            throw 'Install transaction phase or installation state is invalid.'
        }
        $oldNames = @(switch ([string]$State.installationState) {
            'Fresh' {
                if ($currentExists -or $legacyExists) { throw 'Fresh transaction unexpectedly snapshots a registration.' }
                @()
            }
            'Owned' {
                if (-not $currentExists -or $legacyExists) { throw 'Owned transaction registration snapshots disagree.' }
                @([string[]]$script:Definition.ownedFiles) + @([string]$script:Definition.markerFileName)
            }
            'Legacy' {
                if ($currentExists -or -not $legacyExists) { throw 'Legacy transaction registration snapshots disagree.' }
                @($script:Definition.legacy.files | ForEach-Object { [string]$_.name }) + @([string]$script:Definition.uninstallerName)
            }
        })
        Assert-TransactionFileRecords -Records $State.oldFiles -ExpectedNames $oldNames -Kind Backup -Role 'install prior files' -AllowMissing:([string]$State.installationState -eq 'Legacy')
        $newNames = @([string[]]$script:Definition.ownedFiles) + @([string]$script:Definition.markerFileName)
        Assert-TransactionFileRecords -Records $State.newFiles -ExpectedNames $newNames -Kind New -Role 'install candidate files'
        if ([string]$State.phase -in @('Prepared', 'FilesApplied')) {
            if ($null -ne $State.pathAfter -or [bool]$State.pathChanged) {
                throw 'Pre-PATH install transaction contains post-PATH state.'
            }
        }
        elseif ($null -eq $State.pathAfter) {
            throw 'Post-PATH install transaction is missing its PATH snapshot.'
        }
        return
    }
    if ([string]$State.kind -eq 'Uninstall') {
        if ([string]$State.phase -cne 'CleanupComplete' -or [string]$State.installationState -cne 'Owned' -or
            -not [bool]$State.cleanupComplete -or $null -ne $State.pathAfter -or [bool]$State.pathChanged -or
            -not $currentExists -or $legacyExists) {
            throw 'Uninstall transaction phase or installation state is invalid.'
        }
        $oldNames = @([string[]]$script:Definition.ownedFiles) + @([string]$script:Definition.markerFileName)
        Assert-TransactionFileRecords -Records $State.oldFiles -ExpectedNames $oldNames -Kind Uninstall -Role 'uninstall files'
        Assert-TransactionFileRecords -Records $State.newFiles -ExpectedNames @() -Kind New -Role 'uninstall candidate files'
        return
    }
    throw 'Installer transaction kind is unknown and was preserved.'
}

function Read-Transaction {
    $state = Read-JsonFile -Path $script:TransactionStatePath -Role 'installer transaction'
    Assert-ExactProperties -Object $state -Role 'installer transaction' -Expected @(
        'schemaVersion', 'transactionId', 'kind', 'phase', 'installDirectory', 'installationId', 'installationState',
        'currentRegistry', 'legacyRegistry', 'pathBefore', 'pathAfter', 'pathChanged',
        'oldFiles', 'newFiles', 'cleanupComplete'
    )
    Assert-TransactionState -State $state
    $script:CurrentTransactionId = [string]$state.transactionId
    return $state
}

function Read-ExistingTransaction {
    if (-not (Test-Path -LiteralPath $script:TransactionDirectory)) {
        return $null
    }
    Assert-NoReparsePath -Path $script:TransactionDirectory -Boundary $script:LocalAppData
    if (Test-Path -LiteralPath $script:TransactionStatePath -PathType Leaf) {
        return Read-Transaction
    }
    if (Test-Path -LiteralPath $script:TransactionStatePath) {
        throw 'Installer transaction state path is not a regular file and was preserved.'
    }

    # No installed state is mutated before the atomic initial journal publish.
    # A crash while durably copying preparation files can therefore discard only
    # this exact validated transaction namespace and leave the installation alone.
    Remove-SafeTransactionDirectory
    Write-InstallerLog -Message 'discarded interrupted pre-journal installer preparation'
    return $null
}

function Move-FileAtomic {
    param(
        [Parameter(Mandatory = $true)][string]$Source,
        [Parameter(Mandatory = $true)][string]$Destination
    )

    [void](Assert-RegularFile -Path $Source -Role 'activation source')
    if (Test-Path -LiteralPath $Destination) {
        [void](Assert-RegularFile -Path $Destination -Role 'replace destination')
    }
    Move-FileReplace -Source $Source -Destination $Destination
}

function Restore-PathFromTransaction {
    param([Parameter(Mandatory = $true)]$State)

    $current = Get-PathSnapshot
    $currentJson = $current | ConvertTo-Json -Compress -Depth 4
    $beforeJson = $State.pathBefore | ConvertTo-Json -Compress -Depth 4
    if ($currentJson -ceq $beforeJson) {
        return
    }
    if ($null -ne $State.pathAfter) {
        $afterJson = $State.pathAfter | ConvertTo-Json -Compress -Depth 4
        if ($currentJson -ceq $afterJson) {
            Set-PathSnapshot -Snapshot $State.pathBefore
            return
        }
    }
    $beforeValue = if ([bool]$State.pathBefore.exists) { [string]$State.pathBefore.data } else { '' }
    $beforeHasExact = @([regex]::Split($beforeValue, ';') | Where-Object { [string]$_ -ceq $script:InstallDirectory }).Count -gt 0
    if (-not $beforeHasExact) {
        $update = Set-CurrentUserPath -Action Remove -Expected $script:InstallDirectory
        if ([bool]$update.Changed) {
            Write-InstallerLog -Message 'rollback preserved a concurrent PATH edit and removed only the exact installer entry'
            return
        }
    }
    throw 'PATH changed outside the installer transaction; exact rollback was preserved for inspection.'
}

function Invoke-InstallRollback {
    param([Parameter(Mandatory = $true)]$State)

    if ([string]$State.kind -ne 'Install') {
        throw 'Cannot apply install rollback to another transaction kind.'
    }
    Write-InstallerLog -Message 'install rollback started'
    $errors = New-Object 'Collections.Generic.List[string]'
    try { Restore-PathFromTransaction -State $State } catch { [void]$errors.Add($_.Exception.Message) }
    try { Restore-RegistryKeySnapshot -KeyName ([string]$script:Definition.registryKeyName) -Snapshot $State.currentRegistry } catch { [void]$errors.Add($_.Exception.Message) }
    try { Restore-RegistryKeySnapshot -KeyName ([string]$script:Definition.legacy.registryKeyName) -Snapshot $State.legacyRegistry } catch { [void]$errors.Add($_.Exception.Message) }

    $oldByName = @{}
    foreach ($record in @($State.oldFiles)) {
        $oldByName[([string]$record.name).ToLowerInvariant()] = $record
    }
    $newByName = @{}
    foreach ($record in @($State.newFiles)) {
        $newByName[([string]$record.name).ToLowerInvariant()] = $record
    }
    $names = @($oldByName.Keys + $newByName.Keys | Sort-Object -Unique)
    $executableKey = ([string]$script:Definition.executableName).ToLowerInvariant()
    # Keep the gated candidate executable in place while support files roll back,
    # then restore any prior (including ungated legacy) executable last.
    $names = @($names | Sort-Object { if ($_ -eq $executableKey) { 1 } else { 0 } }, { $_ })
    foreach ($key in $names) {
        try {
            $name = if ($oldByName.ContainsKey($key)) { [string]$oldByName[$key].name } else { [string]$newByName[$key].name }
            $destination = Join-Path $script:InstallDirectory $name
            $currentIsOld = $false
            if (Test-Path -LiteralPath $destination) {
                [void](Assert-RegularFile -Path $destination -Role 'rollback destination')
                $currentHash = Get-FileSHA256 -Path $destination
                $currentIsOld = $oldByName.ContainsKey($key) -and $currentHash -ceq [string]$oldByName[$key].sha256
                $known = ($newByName.ContainsKey($key) -and $currentHash -ceq [string]$newByName[$key].sha256) -or
                    $currentIsOld
                if (-not $known) {
                    throw "Rollback preserved unknown file content: $name"
                }
            }
            if ($oldByName.ContainsKey($key)) {
                if ($currentIsOld) {
                    continue
                }
                $backup = Join-Path (Join-Path $script:TransactionDirectory 'backup') ([string]$oldByName[$key].backupName)
                [void](Assert-RegularFile -Path $backup -Role 'rollback backup')
                if ((Get-FileSHA256 -Path $backup) -cne [string]$oldByName[$key].sha256) {
                    throw "Rollback backup hash changed: $name"
                }
                if (-not (Test-Path -LiteralPath $script:InstallDirectory -PathType Container)) {
                    [void][IO.Directory]::CreateDirectory($script:InstallDirectory)
                }
                Move-FileAtomic -Source $backup -Destination $destination
            }
            elseif (Test-Path -LiteralPath $destination) {
                [IO.File]::Delete($destination)
            }
        }
        catch {
            [void]$errors.Add($_.Exception.Message)
        }
    }
    if ($errors.Count -ne 0) {
        Write-InstallerLog -Message 'install rollback incomplete'
        throw ('Rollback was incomplete: ' + [string]::Join(' | ', [string[]]$errors))
    }
    if ((Test-Path -LiteralPath $script:InstallDirectory -PathType Container) -and
        @(Get-ChildItem -LiteralPath $script:InstallDirectory -Force).Count -eq 0) {
        [IO.Directory]::Delete($script:InstallDirectory, $false)
    }
    Remove-SafeTransactionDirectory
    Write-InstallerLog -Message 'install rollback completed'
}

function Set-Registration {
    param(
        [Parameter(Mandatory = $true)]$State,
        [Parameter(Mandatory = $true)][bool]$PathOwned
    )

    $path = Get-RegistryPath -KeyName ([string]$script:Definition.registryKeyName)
    $key = [Microsoft.Win32.Registry]::CurrentUser.CreateSubKey($path)
    if ($null -eq $key) {
        throw 'Could not create the installer registration key.'
    }
    try {
        $uninstaller = Join-Path $script:InstallDirectory ([string]$script:Definition.uninstallerName)
        $quietHelper = Join-Path $script:InstallDirectory ([string]$script:Definition.quietUninstallHelperName)
        $powershell = Join-Path $env:SystemRoot 'System32\WindowsPowerShell\v1.0\powershell.exe'
        $key.SetValue('DisplayName', [string]$script:Definition.displayName, [Microsoft.Win32.RegistryValueKind]::String)
        $key.SetValue('DisplayVersion', [string]$script:Definition.version, [Microsoft.Win32.RegistryValueKind]::String)
        $key.SetValue('Publisher', [string]$script:Definition.publisher, [Microsoft.Win32.RegistryValueKind]::String)
        $key.SetValue('DisplayIcon', ('"' + (Join-Path $script:InstallDirectory ([string]$script:Definition.executableName)) + '",0'), [Microsoft.Win32.RegistryValueKind]::String)
        $key.SetValue('InstallLocation', $script:InstallDirectory, [Microsoft.Win32.RegistryValueKind]::String)
        $key.SetValue('URLInfoAbout', [string]$script:Definition.productUrl, [Microsoft.Win32.RegistryValueKind]::String)
        $key.SetValue('UninstallString', ('"' + $uninstaller + '"'), [Microsoft.Win32.RegistryValueKind]::String)
        $quiet = '"{0}" -NoLogo -NoProfile -NonInteractive -WindowStyle Hidden -ExecutionPolicy Bypass -File "{1}" -Uninstaller "{2}" -InstallDirectory "{3}"' -f $powershell, $quietHelper, $uninstaller, $script:InstallDirectory
        $key.SetValue('QuietUninstallString', $quiet, [Microsoft.Win32.RegistryValueKind]::String)
        $key.SetValue('NoModify', 1, [Microsoft.Win32.RegistryValueKind]::DWord)
        $key.SetValue('NoRepair', 1, [Microsoft.Win32.RegistryValueKind]::DWord)
        $key.SetValue('ProductGuid', [string]$script:Definition.productGuid, [Microsoft.Win32.RegistryValueKind]::String)
        $key.SetValue('InstallationId', [string]$State.installationId, [Microsoft.Win32.RegistryValueKind]::String)
        $key.SetValue('InstallerSchemaVersion', [int]$script:Definition.installerSchemaVersion, [Microsoft.Win32.RegistryValueKind]::DWord)
        $key.SetValue('PathAdded', $(if ($PathOwned) { 1 } else { 0 }), [Microsoft.Win32.RegistryValueKind]::DWord)
        if ($PathOwned) {
            $key.SetValue('PathEntry', $script:InstallDirectory, [Microsoft.Win32.RegistryValueKind]::String)
        }
        else {
            $key.DeleteValue('PathEntry', $false)
        }
        $key.SetValue('UninstallPhase', 'Ready', [Microsoft.Win32.RegistryValueKind]::String)
    }
    finally {
        $key.Dispose()
    }
}

function Remove-ManagedRegistration {
    param([Parameter(Mandatory = $true)][string]$KeyName)

    $path = Get-RegistryPath -KeyName $KeyName
    $key = [Microsoft.Win32.Registry]::CurrentUser.OpenSubKey($path, $true)
    if ($null -eq $key) {
        return $false
    }
    try {
        foreach ($name in $script:ManagedRegistryValues) {
            $key.DeleteValue($name, $false)
        }
    }
    finally {
        $key.Dispose()
    }
    return (Remove-RegistryKeyIfEmpty -KeyName $KeyName)
}

function New-InstallTransaction {
    param([Parameter(Mandatory = $true)][string]$PackageRoot)

    $stale = Read-ExistingTransaction
    if ($null -ne $stale) {
        if ([string]$stale.kind -eq 'Install') {
            if ([string]$stale.phase -eq 'Committed') {
                Remove-SafeTransactionDirectory
            }
            else {
                Invoke-InstallRollback -State $stale
            }
        }
        elseif ([string]$stale.kind -eq 'Uninstall') {
            [void](Complete-UninstallTransaction -State $stale)
        }
        else {
            throw 'Unknown installer transaction kind was preserved.'
        }
    }
    $registrationState = Get-RegistrationState -Definition $script:Definition
    $candidate = $null
    $previous = $null
    [void][Version]::TryParse([string]$script:Definition.version, [ref]$candidate)
    if (-not [string]::IsNullOrEmpty([string]$registrationState.version)) {
        [void][Version]::TryParse([string]$registrationState.version, [ref]$previous)
        if ($null -eq $previous -or $previous -gt $candidate) {
            throw "Downgrade from $($registrationState.version) to $($script:Definition.version) is not allowed."
        }
    }

    [void](Assert-RegularFile -Path $script:DefinitionPath -Role 'installer definition')
    if (-not (Test-Path -LiteralPath $PackageRoot -PathType Container)) {
        throw 'Installer package stage is missing.'
    }
    $packageNames = @((Get-ChildItem -LiteralPath $PackageRoot -Force) | ForEach-Object { $_.Name } | Sort-Object)
    $expectedNames = @([string[]]$script:Definition.ownedFiles | Sort-Object)
    if ([string]::Join("`n", $packageNames) -cne [string]::Join("`n", $expectedNames)) {
        throw 'Installer package stage is missing, extra, or malformed.'
    }
    foreach ($name in $expectedNames) {
        [void](Assert-RegularFile -Path (Join-Path $PackageRoot $name) -Role 'installer package file')
    }

    [void][IO.Directory]::CreateDirectory($script:TransactionDirectory)
    Assert-NoReparsePath -Path $script:TransactionDirectory -Boundary $script:LocalAppData
    $backupDirectory = Join-Path $script:TransactionDirectory 'backup'
    $newDirectory = Join-Path $script:TransactionDirectory 'new'
    [void][IO.Directory]::CreateDirectory($backupDirectory)
    [void][IO.Directory]::CreateDirectory($newDirectory)

    $oldRecords = if ([string]$registrationState.kind -eq 'Owned') {
        @(Get-FileRecordsFromMarker -Marker $registrationState.marker)
    }
    elseif ([string]$registrationState.kind -eq 'Legacy') {
        @(Get-LegacyFileRecords)
    }
    else {
        @()
    }
    $oldFiles = @()
    $backupIndex = 0
    foreach ($record in $oldRecords) {
        $source = Join-Path $script:InstallDirectory ([string]$record.name)
        [void](Assert-RegularFile -Path $source -Role 'installed backup source')
        if ((Get-FileSHA256 -Path $source) -cne [string]$record.sha256) {
            throw "Installed file changed before backup: $($record.name)"
        }
        $backupName = ('{0:D2}.bin' -f $backupIndex)
        Copy-FileDurable -Source $source -Destination (Join-Path $backupDirectory $backupName)
        $oldFiles += [pscustomobject]@{ name = [string]$record.name; sha256 = [string]$record.sha256; backupName = $backupName }
        $backupIndex += 1
    }

    $newFiles = @()
    foreach ($name in @([string[]]$script:Definition.ownedFiles)) {
        $source = Join-Path $PackageRoot $name
        $destination = Join-Path $newDirectory $name
        Copy-FileDurable -Source $source -Destination $destination
        $newFiles += [pscustomobject]@{ name = $name; sha256 = Get-FileSHA256 -Path $destination; size = (Get-Item -LiteralPath $destination -Force).Length }
    }
    $marker = [ordered]@{
        schemaVersion = [int]$script:Definition.installerSchemaVersion
        productGuid = [string]$script:Definition.productGuid
        installationId = [string]$registrationState.installationId
        installLocation = $script:InstallDirectory
        installedVersion = [string]$script:Definition.version
        ownedFiles = @($newFiles | ForEach-Object { [ordered]@{ name = [string]$_.name; sha256 = [string]$_.sha256; size = [int64]$_.size } })
    }
    $markerPath = Join-Path $newDirectory ([string]$script:Definition.markerFileName)
    Write-JsonAtomic -Path $markerPath -Value $marker
    $newFiles += [pscustomobject]@{ name = [string]$script:Definition.markerFileName; sha256 = Get-FileSHA256 -Path $markerPath; size = (Get-Item -LiteralPath $markerPath -Force).Length }

    $state = [ordered]@{
        schemaVersion = 1
        transactionId = [Guid]::NewGuid().ToString('D')
        kind = 'Install'
        phase = 'Prepared'
        installDirectory = $script:InstallDirectory
        installationId = [string]$registrationState.installationId
        installationState = [string]$registrationState.kind
        currentRegistry = Get-RegistryKeySnapshot -KeyName ([string]$script:Definition.registryKeyName)
        legacyRegistry = Get-RegistryKeySnapshot -KeyName ([string]$script:Definition.legacy.registryKeyName)
        pathBefore = Get-PathSnapshot
        pathAfter = $null
        pathChanged = $false
        oldFiles = $oldFiles
        newFiles = $newFiles
        cleanupComplete = $false
    }
    Save-Transaction -State $state
    return $state
}

function Invoke-Install {
    if ([string]::IsNullOrWhiteSpace($PackageDirectory)) {
        throw 'PackageDirectory is required for installer activation.'
    }
    $state = New-InstallTransaction -PackageRoot (Get-NormalizedPath -Path $PackageDirectory)
    Write-InstallerLog -Message 'install transaction prepared'
    try {
        if (-not (Test-Path -LiteralPath $script:InstallDirectory -PathType Container)) {
            [void][IO.Directory]::CreateDirectory($script:InstallDirectory)
        }
        Assert-NoReparsePath -Path $script:InstallDirectory -Boundary $script:LocalAppData
        $newDirectory = Join-Path $script:TransactionDirectory 'new'
        $supportFiles = @([string[]]$script:Definition.ownedFiles | Where-Object { $_ -cne [string]$script:Definition.executableName })
        $ordered = if ([string]$state.installationState -eq 'Legacy') {
            # v0.0.9 cannot observe the lifecycle gate. Replacing its executable
            # first either fails cleanly while it is running or makes every new
            # launch use the gated executable before support files change.
            @([string]$script:Definition.executableName) + $supportFiles + @([string]$script:Definition.markerFileName)
        }
        else {
            $supportFiles + @([string]$script:Definition.executableName, [string]$script:Definition.markerFileName)
        }
        foreach ($name in $ordered) {
            Move-FileAtomic -Source (Join-Path $newDirectory $name) -Destination (Join-Path $script:InstallDirectory $name)
        }
        $newNames = @($state.newFiles | ForEach-Object { ([string]$_.name).ToLowerInvariant() })
        foreach ($old in @($state.oldFiles)) {
            if ($newNames -notcontains ([string]$old.name).ToLowerInvariant()) {
                $obsolete = Join-Path $script:InstallDirectory ([string]$old.name)
                if (Test-Path -LiteralPath $obsolete) {
                    if ((Get-FileSHA256 -Path $obsolete) -cne [string]$old.sha256) {
                        throw "Obsolete installer-owned file changed and was preserved: $($old.name)"
                    }
                    [IO.File]::Delete($obsolete)
                }
            }
        }
        $state.phase = 'FilesApplied'
        Save-Transaction -State $state

        $pathSnapshot = Get-PathSnapshot
        $currentPath = if ([bool]$pathSnapshot.exists) { [string]$pathSnapshot.data } else { '' }
        $expand = [bool]$pathSnapshot.exists -and [string]$pathSnapshot.kind -eq 'ExpandString'
        $effectivePresent = @([regex]::Split($currentPath, ';') | Where-Object {
                Test-PathEntry -Entry $_ -Expected $script:InstallDirectory -ExpandVariables $expand
            }).Count -gt 0
        $previouslyOwned = [string]$state.installationState -in @('Owned', 'Legacy') -and
            @($state.currentRegistry.values + $state.legacyRegistry.values | Where-Object {
                    [string]$_.name -eq 'PathAdded' -and [bool]$_.exists -and [string]$_.data -eq '1'
                }).Count -gt 0
        $pathOwned = $previouslyOwned
        $pathChanged = $false
        if (-not $effectivePresent) {
            $update = Set-CurrentUserPath -Action Add -Expected $script:InstallDirectory
            $pathChanged = [bool]$update.Changed
            $pathOwned = $true
        }
        $state.pathAfter = Get-PathSnapshot
        $state.pathChanged = $pathChanged
        $state.phase = 'PathApplied'
        Save-Transaction -State $state

        Set-Registration -State $state -PathOwned $pathOwned
        if ([string]$state.installationState -eq 'Legacy') {
            if (Remove-ManagedRegistration -KeyName ([string]$script:Definition.legacy.registryKeyName)) {
                throw 'Legacy registration retained unknown state during migration.'
            }
        }
        $state.phase = 'Applied'
        Save-Transaction -State $state
        Write-InstallerLog -Message 'install payload, registration, and PATH applied'
        return $pathChanged
    }
    catch {
        $original = $_.Exception.Message
        try {
            Invoke-InstallRollback -State (Read-Transaction)
        }
        catch {
            throw "INSTALL_ROLLBACK_INCOMPLETE: $original Rollback was incomplete: $($_.Exception.Message)"
        }
        throw "$original The complete prior installer-owned state was restored."
    }
}

function Invoke-RollbackInstall {
    if (-not (Test-Path -LiteralPath $script:TransactionStatePath -PathType Leaf)) {
        return
    }
    Invoke-InstallRollback -State (Read-Transaction)
}

function Invoke-CommitInstall {
    $state = Read-Transaction
    if ([string]$state.kind -ne 'Install' -or [string]$state.phase -ne 'Applied') {
        throw 'Installer transaction cannot be committed from its current phase.'
    }
    $state.phase = 'Committed'
    Save-Transaction -State $state
    Remove-SafeTransactionDirectory
    Write-InstallerLog -Message 'install transaction committed'
}

function Test-OwnedFileCanBeRemoved {
    param([Parameter(Mandatory = $true)][string]$Path)

    $stream = [IO.File]::Open($Path, [IO.FileMode]::Open, [IO.FileAccess]::ReadWrite, [IO.FileShare]::None)
    $stream.Dispose()
}

function Invoke-InspectUninstall {
    $state = Read-ExistingTransaction
    if ($null -ne $state) {
        if ([string]$state.kind -eq 'Install') {
            if ([string]$state.phase -eq 'Committed') {
                Remove-SafeTransactionDirectory
            }
            else {
                Invoke-InstallRollback -State $state
            }
        }
        elseif ([string]$state.kind -eq 'Uninstall') {
            return [bool]$state.cleanupComplete
        }
    }
    $registration = Get-RegistrationState -Definition $script:Definition
    if ([string]$registration.kind -ne 'Owned' -or [string]$registration.version -cne [string]$script:Definition.version) {
        throw 'This uninstaller does not own the registered installation version and changed nothing.'
    }
    if ([string]$registration.uninstallPhase -eq 'CleanupComplete') {
        return $true
    }
    foreach ($record in @($registration.marker.ownedFiles)) {
        Test-OwnedFileCanBeRemoved -Path (Join-Path $script:InstallDirectory ([string]$record.name))
    }
    $markerPath = Join-Path $script:InstallDirectory ([string]$script:Definition.markerFileName)
    Test-OwnedFileCanBeRemoved -Path $markerPath
    $key = [Microsoft.Win32.Registry]::CurrentUser.OpenSubKey((Get-RegistryPath -KeyName ([string]$script:Definition.registryKeyName)), $true)
    if ($null -eq $key) {
        throw 'Installer registration is not writable.'
    }
    $key.Dispose()
    [void](Get-PathSnapshot)
    Write-InstallerLog -Message 'uninstall ownership and deletion preflight passed'
    return $false
}

function Invoke-MarkCleanupComplete {
    if (Test-Path -LiteralPath $script:TransactionStatePath -PathType Leaf) {
        $state = Read-Transaction
        if ([string]$state.kind -ne 'Uninstall') {
            throw 'Another installer transaction must recover before uninstall can continue.'
        }
        $state.cleanupComplete = $true
        $state.phase = 'CleanupComplete'
        Save-Transaction -State $state
        return
    }
    $registration = Get-RegistrationState -Definition $script:Definition
    if ([string]$registration.kind -ne 'Owned') {
        throw 'Installer ownership changed before cleanup completion could be recorded.'
    }
    [void][IO.Directory]::CreateDirectory($script:TransactionDirectory)
    Assert-NoReparsePath -Path $script:TransactionDirectory -Boundary $script:LocalAppData
    $oldFiles = @(Get-FileRecordsFromMarker -Marker $registration.marker | ForEach-Object {
            [pscustomobject]@{ name = [string]$_.name; sha256 = [string]$_.sha256; backupName = '' }
        })
    $state = [ordered]@{
        schemaVersion = 1
        transactionId = [Guid]::NewGuid().ToString('D')
        kind = 'Uninstall'
        phase = 'CleanupComplete'
        installDirectory = $script:InstallDirectory
        installationId = [string]$registration.installationId
        installationState = 'Owned'
        currentRegistry = Get-RegistryKeySnapshot -KeyName ([string]$script:Definition.registryKeyName)
        legacyRegistry = Get-RegistryKeySnapshot -KeyName ([string]$script:Definition.legacy.registryKeyName)
        pathBefore = Get-PathSnapshot
        pathAfter = $null
        pathChanged = $false
        oldFiles = $oldFiles
        newFiles = @()
        cleanupComplete = $true
    }
    Save-Transaction -State $state
    $key = [Microsoft.Win32.Registry]::CurrentUser.OpenSubKey((Get-RegistryPath -KeyName ([string]$script:Definition.registryKeyName)), $true)
    if ($null -eq $key) {
        throw 'Installer registration disappeared before cleanup completion could be recorded.'
    }
    try {
        $key.SetValue('UninstallPhase', 'CleanupComplete', [Microsoft.Win32.RegistryValueKind]::String)
    }
    finally {
        $key.Dispose()
    }
    Write-InstallerLog -Message 'application cleanup completion recorded'
}

function Remove-OwnedFileIfPresent {
    param([Parameter(Mandatory = $true)]$Record)

    $path = Join-Path $script:InstallDirectory ([string]$Record.name)
    if (-not (Test-Path -LiteralPath $path)) {
        return
    }
    [void](Assert-RegularFile -Path $path -Role 'uninstall owned file')
    if ((Get-FileSHA256 -Path $path) -cne [string]$Record.sha256) {
        throw "Uninstall preserved changed file content: $($Record.name)"
    }
    [IO.File]::Delete($path)
}

function Complete-UninstallTransaction {
    param([Parameter(Mandatory = $true)]$State)

    if ([string]$State.kind -ne 'Uninstall' -or -not [bool]$State.cleanupComplete) {
        throw 'Uninstall cleanup has not reached its durable terminal phase.'
    }
    Write-InstallerLog -Message 'uninstall terminal cleanup started'
    $byName = @{}
    foreach ($record in @($State.oldFiles)) {
        $byName[([string]$record.name).ToLowerInvariant()] = $record
    }
    $exeKey = ([string]$script:Definition.executableName).ToLowerInvariant()
    $uninstallerKey = ([string]$script:Definition.uninstallerName).ToLowerInvariant()
    $markerKey = ([string]$script:Definition.markerFileName).ToLowerInvariant()
    $quietKey = ([string]$script:Definition.quietUninstallHelperName).ToLowerInvariant()
    foreach ($key in @($byName.Keys | Where-Object { $_ -notin @($exeKey, $uninstallerKey, $markerKey, $quietKey) } | Sort-Object)) {
        Remove-OwnedFileIfPresent -Record $byName[$key]
    }

    $pathAddedRecord = @($State.currentRegistry.values | Where-Object { [string]$_.name -eq 'PathAdded' })[0]
    if ($null -ne $pathAddedRecord -and [bool]$pathAddedRecord.exists -and [string]$pathAddedRecord.data -eq '1') {
        [void](Set-CurrentUserPath -Action Remove -Expected $script:InstallDirectory)
    }
    if ($byName.ContainsKey($exeKey)) { Remove-OwnedFileIfPresent -Record $byName[$exeKey] }
    if ($byName.ContainsKey($quietKey)) { Remove-OwnedFileIfPresent -Record $byName[$quietKey] }

    $registrationRetained = Remove-ManagedRegistration -KeyName ([string]$script:Definition.registryKeyName)
    if ($byName.ContainsKey($uninstallerKey)) { Remove-OwnedFileIfPresent -Record $byName[$uninstallerKey] }
    if ($byName.ContainsKey($markerKey)) { Remove-OwnedFileIfPresent -Record $byName[$markerKey] }

    $directoryRetained = $false
    if (Test-Path -LiteralPath $script:InstallDirectory -PathType Container) {
        if (@(Get-ChildItem -LiteralPath $script:InstallDirectory -Force).Count -eq 0) {
            [IO.Directory]::Delete($script:InstallDirectory, $false)
        }
        else {
            $directoryRetained = $true
        }
    }
    Remove-SafeTransactionDirectory
    Write-InstallerLog -Message $(if ($registrationRetained -or $directoryRetained) { 'uninstall completed with preserved unowned state' } else { 'uninstall completed' })
    return ($registrationRetained -or $directoryRetained)
}

function Invoke-FinishUninstall {
    if (-not (Test-Path -LiteralPath $script:TransactionStatePath -PathType Leaf)) {
        throw 'Uninstall transaction state is missing.'
    }
    return (Complete-UninstallTransaction -State (Read-Transaction))
}

$script:Definition = $null
$script:CurrentTransactionId = $null
$script:DefinitionPath = Get-NormalizedPath -Path $DefinitionPath
$script:Definition = Read-Definition -Path $script:DefinitionPath
$script:LocalAppData = Get-NormalizedPath -Path $env:LOCALAPPDATA
$expectedInstallDirectory = Join-Path (Join-Path $script:LocalAppData 'Programs') ([string]$script:Definition.installDirectoryName)
$script:InstallDirectory = Get-NormalizedPath -Path $InstallDirectory
if (-not (Test-PathsEqual -Left $script:InstallDirectory -Right $expectedInstallDirectory)) {
    throw 'InstallDirectory does not equal the product-owned current-user location.'
}
Assert-NoReparsePath -Path $script:InstallDirectory -Boundary $script:LocalAppData
$script:TransactionDirectory = Join-Path (Split-Path -Parent $script:InstallDirectory) ('.' + [string]$script:Definition.applicationName + '-installer-transaction')
$script:TransactionStatePath = Join-Path $script:TransactionDirectory 'state.json'

try {
    switch ($Action) {
        'Install' {
            $changed = Invoke-Install
            if ($changed) { exit 10 }
            exit 0
        }
        'RollbackInstall' {
            Invoke-RollbackInstall
            exit 0
        }
        'CommitInstall' {
            Invoke-CommitInstall
            exit 0
        }
        'InspectUninstall' {
            $cleanupComplete = Invoke-InspectUninstall
            if ($cleanupComplete) { exit 10 }
            exit 0
        }
        'MarkCleanupComplete' {
            Invoke-MarkCleanupComplete
            exit 0
        }
        'FinishUninstall' {
            $retained = Invoke-FinishUninstall
            if ($retained) {
                'Uninstall completed; unowned registry values or installation-directory files were preserved.'
                exit 10
            }
            exit 0
        }
    }
}
catch {
    $message = $_.Exception.Message
    Write-InstallerLog -Message ("failure: " + $message)
    [Console]::Error.WriteLine($message)
    if ($message.StartsWith('INSTALL_ROLLBACK_INCOMPLETE:', [StringComparison]::Ordinal)) {
        exit 20
    }
    exit 1
}
