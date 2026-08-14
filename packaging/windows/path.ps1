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

function Get-PathEntryDescriptor {
    param(
        [AllowEmptyString()][string]$Entry,
        [Parameter(Mandatory = $true)][bool]$ExpandVariables,
        [Parameter(Mandatory = $true)][string]$Expected
    )

    $raw = [string]$Entry
    $candidate = $raw.Trim().Trim([char[]]@('"'))
    if ([string]::IsNullOrWhiteSpace($candidate)) {
        return [pscustomobject]@{
            Raw = $raw
            Empty = $true
            Product = $false
            EffectiveKey = $null
        }
    }

    $literal = $null
    try {
        $literal = Get-NormalizedPath -Path $candidate
    }
    catch {
        $literal = $null
    }

    $effective = $literal
    if ($ExpandVariables) {
        try {
            $expanded = [Environment]::ExpandEnvironmentVariables($candidate)
            $effective = Get-NormalizedPath -Path $expanded
        }
        catch {
            # A literal path containing percent characters must still compare as
            # its literal spelling even when the registry value is REG_EXPAND_SZ.
            $effective = $literal
        }
    }

    $product = ($null -ne $literal -and
            [string]::Equals($literal, $Expected, [StringComparison]::OrdinalIgnoreCase)) -or
        ($null -ne $effective -and
            [string]::Equals($effective, $Expected, [StringComparison]::OrdinalIgnoreCase))

    if ($null -ne $effective) {
        $effectiveKey = 'P|' + $effective
    }
    elseif ($null -ne $literal) {
        $effectiveKey = 'P|' + $literal
    }
    else {
        # Preserve unusual relative or malformed entries, but collapse exact
        # duplicates because repeating the same text cannot change precedence.
        $effectiveKey = 'R|' + $candidate
    }

    return [pscustomobject]@{
        Raw = $raw
        Empty = $false
        Product = [bool]$product
        EffectiveKey = $effectiveKey
    }
}

function Get-ConvergedPathEntries {
    param(
        [Parameter(Mandatory = $true)]
        [AllowEmptyCollection()]
        [AllowEmptyString()]
        [string[]]$Entries,
        [Parameter(Mandatory = $true)][bool]$ExpandVariables,
        [Parameter(Mandatory = $true)][string]$Expected,
        [Parameter(Mandatory = $true)][bool]$RemoveProduct
    )

    # We deliberately deduplicate the full effective PATH while preserving the first occurrence.
    # Add will canonicalize this product's directory to one literal entry on Add.
    # Uninstall will remove every effective product entry on Remove.
    $kept = New-Object 'Collections.Generic.List[string]'
    $seenEffective = New-Object 'Collections.Generic.HashSet[string]' ([StringComparer]::OrdinalIgnoreCase)
    foreach ($entry in $Entries) {
        $descriptor = Get-PathEntryDescriptor -Entry ([string]$entry) `
            -ExpandVariables $ExpandVariables -Expected $Expected
        if ([bool]$descriptor.Empty) {
            continue
        }

        if ([bool]$descriptor.Product) {
            if (-not $RemoveProduct) {
                $effectiveKey = 'P|' + $Expected
                if ($seenEffective.Add($effectiveKey)) {
                    [void]$kept.Add($Expected)
                }
            }
            continue
        }

        $effectiveKey = [string]$descriptor.EffectiveKey
        if ($seenEffective.Add($effectiveKey)) {
            [void]$kept.Add([string]$descriptor.Raw)
        }
    }

    if (-not $RemoveProduct) {
        $effectiveKey = 'P|' + $Expected
        if ($seenEffective.Add($effectiveKey)) {
            [void]$kept.Add($Expected)
        }
    }

    return [string[]]$kept
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
            $descriptor = Get-PathEntryDescriptor -Entry ([string]$_) `
                -ExpandVariables $ExpandVariables -Expected $Expected
            [bool]$descriptor.Product
        }).Count -gt 0

    if ($RequestedAction -eq 'Contains') {
        return [pscustomobject]@{ Changed = $false; Present = $present; Value = $Current }
    }

    $removeProduct = $RequestedAction -eq 'Remove'
    $converged = @(Get-ConvergedPathEntries -Entries $entries `
            -ExpandVariables $ExpandVariables -Expected $Expected -RemoveProduct $removeProduct)
    $updated = [string]::Join(';', [string[]]$converged)

    if (-not $removeProduct -and $updated.Length -ge 32767) {
        throw 'Adding the install directory would exceed the Windows PATH length limit.'
    }

    return [pscustomobject]@{
        Changed = -not [string]::Equals($updated, $Current, [StringComparison]::Ordinal)
        Present = -not $removeProduct
        Value = $updated
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

function Enter-UserPathLock {
    $localAppData = [Environment]::GetFolderPath([Environment+SpecialFolder]::LocalApplicationData)
    if ([string]::IsNullOrWhiteSpace($localAppData)) {
        throw 'Windows did not provide the current-user LocalAppData directory.'
    }
    $lockDirectory = Join-Path $localAppData 'InstallerPathLocks'
    [void][IO.Directory]::CreateDirectory($lockDirectory)
    $lockPath = Join-Path $lockDirectory 'user-path.lock'
    $deadline = [DateTime]::UtcNow.AddSeconds(15)
    do {
        try {
            return [IO.File]::Open(
                $lockPath,
                [IO.FileMode]::OpenOrCreate,
                [IO.FileAccess]::ReadWrite,
                [IO.FileShare]::None
            )
        }
        catch [IO.IOException] {
            if ([DateTime]::UtcNow -ge $deadline) {
                throw 'Another current-user PATH update did not release the serialization lock.'
            }
            Start-Sleep -Milliseconds 100
        }
    } while ($true)
}

function Invoke-UserPathAction {
    param(
        [Parameter(Mandatory = $true)][string]$Target,
        [Parameter(Mandatory = $true)][ValidateSet('Contains', 'Add', 'Remove')][string]$RequestedAction
    )

    if ($RequestedAction -eq 'Contains') {
        $snapshot = Get-UserPathSnapshot
        $kind = if ([bool]$snapshot.Exists) { [string]$snapshot.Kind } else { 'ExpandString' }
        $result = Resolve-UserPathUpdate -Current ([string]$snapshot.Value) -Expected $Target `
            -RequestedAction Contains -ExpandVariables ($kind -eq 'ExpandString')
        [Console]::Out.Write($(if ([bool]$result.Present) { '1' } else { '0' }))
        return 0
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
        $update = Resolve-UserPathUpdate -Current ([string]$snapshot.Value) -Expected $Target `
            -RequestedAction $RequestedAction -ExpandVariables ($kind -eq [Microsoft.Win32.RegistryValueKind]::ExpandString)
        if (-not [bool]$update.Changed) {
            return 0
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
        return 10
    }

    throw 'Current-user PATH changed concurrently three times; no installer PATH write was applied.'
}

$target = Get-NormalizedPath -Path $InstallDirectory
$pathLock = Enter-UserPathLock
try {
    $exitCode = Invoke-UserPathAction -Target $target -RequestedAction $Action
}
finally {
    $pathLock.Dispose()
}
exit $exitCode
