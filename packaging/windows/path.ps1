param(
    [Parameter(Mandatory = $true)]
    [ValidateSet('Contains', 'Add', 'Remove')]
    [string]$Action,

    [Parameter(Mandatory = $true)]
    [ValidateNotNullOrEmpty()]
    [string]$InstallDirectory
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version 2.0

function Test-FullyQualifiedWindowsPath {
    param([Parameter(Mandatory = $true)][string]$Path)

    if ([string]::IsNullOrWhiteSpace($Path)) {
        return $false
    }
    return ($Path -match '^[A-Za-z]:[\\/]' -or $Path -match '^[\\/]{2}[^\\/]+[\\/][^\\/]+(?:[\\/]|$)')
}

function Get-NormalizedPath {
    param([Parameter(Mandatory = $true)][string]$Path)

    if ($Path.IndexOf(';') -ge 0) {
        throw "A PATH entry cannot contain a semicolon: '$Path'."
    }
    if (-not (Test-FullyQualifiedWindowsPath -Path $Path)) {
        throw "PATH entry is not fully qualified: '$Path'."
    }

    $full = [IO.Path]::GetFullPath($Path)
    $root = [IO.Path]::GetPathRoot($full)
    if ($full.Length -gt $root.Length) {
        $full = $full.TrimEnd([char[]]@('\', '/'))
    }
    return $full
}

function Test-EffectivePathEntry {
    param(
        [AllowEmptyString()][string]$Entry,
        [Parameter(Mandatory = $true)][string]$Expected,
        [Parameter(Mandatory = $true)][bool]$ExpandVariables
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
        return $false
    }
}

function Test-OwnedPathEntry {
    param(
        [AllowEmptyString()][string]$Entry,
        [Parameter(Mandatory = $true)][string]$Expected
    )

    $candidate = $Entry.Trim().Trim([char[]]@('"'))
    if ([string]::IsNullOrWhiteSpace($candidate)) {
        return $false
    }
    try {
        # Compare normalized literal paths without expanding environment variables.
        # This converges duplicate spelling variants of the fixed install directory
        # while expressions such as %LOCALAPPDATA% remain foreign.
        return [string]::Equals(
            (Get-NormalizedPath -Path $candidate),
            $Expected,
            [StringComparison]::OrdinalIgnoreCase
        )
    }
    catch {
        return $false
    }
}

function Resolve-UserPathUpdate {
    param(
        [AllowEmptyString()][string]$Current,
        [Parameter(Mandatory = $true)][string]$Expected,
        [Parameter(Mandatory = $true)][ValidateSet('Contains', 'Add', 'Remove')][string]$RequestedAction,
        [Parameter(Mandatory = $true)][bool]$ExpandVariables
    )

    $entries = @([regex]::Split($Current, ';'))
    $present = @($entries | Where-Object {
            Test-EffectivePathEntry -Entry $_ -Expected $Expected -ExpandVariables $ExpandVariables
        }).Count -gt 0

    if ($RequestedAction -eq 'Contains') {
        return [pscustomobject]@{ Changed = $false; Present = $present; Value = $Current }
    }
    if ($RequestedAction -eq 'Add') {
        if ($present) {
            return [pscustomobject]@{ Changed = $false; Present = $true; Value = $Current }
        }
        $updated = if ([string]::IsNullOrEmpty($Current)) { $Expected } else { $Current + ';' + $Expected }
        # Windows environment-variable values cannot exceed 32,767 characters,
        # including the terminating null character.
        if ($updated.Length -ge 32767) {
            throw 'Adding the install directory would exceed the Windows PATH length limit.'
        }
        return [pscustomobject]@{ Changed = $true; Present = $true; Value = $updated }
    }

    # The product owns complete literal convergence for its fixed install path.
    # Remove every normalized literal spelling so uninstall leaves no dead alias.
    # Environment expressions and unrelated entries remain untouched.
    $kept = New-Object 'Collections.Generic.List[string]'
    $removed = $false
    foreach ($entry in $entries) {
        if (Test-OwnedPathEntry -Entry ([string]$entry) -Expected $Expected) {
            $removed = $true
        }
        else {
            [void]$kept.Add([string]$entry)
        }
    }
    if (-not $removed) {
        return [pscustomobject]@{ Changed = $false; Present = $present; Value = $Current }
    }
    $remaining = [string[]]$kept
    $remainingPresent = @($remaining | Where-Object {
            Test-EffectivePathEntry -Entry $_ -Expected $Expected -ExpandVariables $ExpandVariables
        }).Count -gt 0
    return [pscustomobject]@{
        Changed = $true
        Present = $remainingPresent
        Value = [string]::Join(';', $remaining)
    }
}

function Get-UserPathSnapshot {
    $key = [Microsoft.Win32.Registry]::CurrentUser.OpenSubKey('Environment', $false)
    if ($null -eq $key) {
        return [pscustomobject]@{ KeyExists = $false; Exists = $false; Kind = ''; Value = '' }
    }
    try {
        if (@($key.GetValueNames()) -inotcontains 'Path') {
            return [pscustomobject]@{ KeyExists = $true; Exists = $false; Kind = ''; Value = '' }
        }
        $kind = $key.GetValueKind('Path')
        if ($kind -ne [Microsoft.Win32.RegistryValueKind]::String -and
            $kind -ne [Microsoft.Win32.RegistryValueKind]::ExpandString) {
            throw "The current-user Path has unsupported registry kind '$kind'."
        }
        $value = [string]$key.GetValue(
            'Path',
            '',
            [Microsoft.Win32.RegistryValueOptions]::DoNotExpandEnvironmentNames
        )
        return [pscustomobject]@{ KeyExists = $true; Exists = $true; Kind = [string]$kind; Value = $value }
    }
    finally {
        $key.Dispose()
    }
}

function Test-SnapshotEqual {
    param($Left, $Right)

    return [bool]$Left.KeyExists -eq [bool]$Right.KeyExists -and
        [bool]$Left.Exists -eq [bool]$Right.Exists -and
        [string]$Left.Kind -ceq [string]$Right.Kind -and
        [string]$Left.Value -ceq [string]$Right.Value
}

$target = Get-NormalizedPath -Path $InstallDirectory
if ($Action -eq 'Contains') {
    $snapshot = Get-UserPathSnapshot
    $kind = if ([bool]$snapshot.Exists) { [string]$snapshot.Kind } else { 'ExpandString' }
    $result = Resolve-UserPathUpdate -Current ([string]$snapshot.Value) -Expected $target `
        -RequestedAction Contains -ExpandVariables ($kind -eq 'ExpandString')
    [Console]::Out.Write($(if ([bool]$result.Present) { '1' } else { '0' }))
    exit 0
}

for ($attempt = 1; $attempt -le 3; $attempt += 1) {
    $snapshot = Get-UserPathSnapshot
    $kind = if ([bool]$snapshot.Exists) {
        [Microsoft.Win32.RegistryValueKind][Enum]::Parse(
            [Microsoft.Win32.RegistryValueKind],
            [string]$snapshot.Kind
        )
    }
    else {
        [Microsoft.Win32.RegistryValueKind]::ExpandString
    }
    $update = Resolve-UserPathUpdate -Current ([string]$snapshot.Value) -Expected $target `
        -RequestedAction $Action -ExpandVariables ($kind -eq [Microsoft.Win32.RegistryValueKind]::ExpandString)
    if (-not [bool]$update.Changed) {
        exit 0
    }
    if (-not (Test-SnapshotEqual -Left $snapshot -Right (Get-UserPathSnapshot))) {
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

    $readback = Get-UserPathSnapshot
    if (-not [bool]$readback.Exists -or [string]$readback.Kind -cne [string]$kind -or
        [string]$readback.Value -cne [string]$update.Value) {
        throw 'Current-user PATH write did not pass exact type/data read-back.'
    }
    exit 10
}

throw 'Current-user PATH changed concurrently three times; no installer PATH write was applied.'
