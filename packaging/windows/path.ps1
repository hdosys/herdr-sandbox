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
    if ($ExpandVariables) {
        $candidate = [Environment]::ExpandEnvironmentVariables($candidate)
    }
    if ([string]::IsNullOrWhiteSpace($candidate)) {
        return $false
    }
    try {
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
        return (Get-NormalizedPath -Path $candidate) -ieq $Expected
    }
    catch {
        return $false
    }
}

function Resolve-UserPathUpdate {
    param(
        [AllowNull()][AllowEmptyString()][string]$Current,
        [Parameter(Mandatory = $true)][string]$Expected,
        [Parameter(Mandatory = $true)][ValidateSet('Contains', 'Add', 'Remove')][string]$RequestedAction,
        [Parameter(Mandatory = $true)][bool]$ExpandVariables
    )

    if ($null -eq $Current) { $Current = '' }
    $entries = [string[]]($Current -split ';', -1)
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
        if ($updated.Length -ge 32767) {
            throw 'Adding the install directory would exceed the Windows PATH length limit.'
        }
        return [pscustomobject]@{ Changed = $true; Present = $true; Value = $updated }
    }

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
    param([Parameter(Mandatory = $true)][Microsoft.Win32.RegistryKey]$EnvironmentKey)

    $kind = $null
    try {
        $kind = $EnvironmentKey.GetValueKind('Path')
    }
    catch [System.ArgumentException] {
        return [pscustomobject]@{ Exists = $false; Kind = [Microsoft.Win32.RegistryValueKind]::String; Value = '' }
    }
    if ($kind -ne [Microsoft.Win32.RegistryValueKind]::String -and
        $kind -ne [Microsoft.Win32.RegistryValueKind]::ExpandString) {
        throw "The current-user PATH has unsupported registry kind $kind."
    }
    $value = [string]$EnvironmentKey.GetValue('Path', '', [Microsoft.Win32.RegistryValueOptions]::DoNotExpandEnvironmentNames)
    return [pscustomobject]@{ Exists = $true; Kind = $kind; Value = $value }
}

function Test-SnapshotEqual {
    param($Left, $Right)
    return ($Left.Exists -eq $Right.Exists -and $Left.Kind -eq $Right.Kind -and $Left.Value -ceq $Right.Value)
}

try {
    $expected = Get-NormalizedPath -Path $InstallDirectory
    $environment = [Microsoft.Win32.Registry]::CurrentUser.OpenSubKey('Environment', $true)
    if ($null -eq $environment) {
        throw 'Could not open the current-user Environment registry key.'
    }
    try {
        for ($attempt = 0; $attempt -lt 8; $attempt += 1) {
            $before = Get-UserPathSnapshot -EnvironmentKey $environment
            $expand = $before.Kind -eq [Microsoft.Win32.RegistryValueKind]::ExpandString
            $update = Resolve-UserPathUpdate -Current $before.Value -Expected $expected -RequestedAction $Action -ExpandVariables $expand
            if (-not $update.Changed) {
                [Console]::Out.Write($(if ($update.Present) { '1' } else { '0' }))
                exit 0
            }

            $environment.SetValue('Path', $update.Value, $before.Kind)
            $after = Get-UserPathSnapshot -EnvironmentKey $environment
            if ($after.Kind -eq $before.Kind -and $after.Value -ceq $update.Value) {
                [Console]::Out.Write($(if ($update.Present) { '1' } else { '0' }))
                exit 10
            }
            if (Test-SnapshotEqual -Left $before -Right $after) {
                continue
            }
        }
        throw 'The current-user PATH changed concurrently too many times.'
    }
    finally {
        $environment.Dispose()
    }
}
catch {
    [Console]::Error.WriteLine($_.Exception.Message)
    exit 1
}
