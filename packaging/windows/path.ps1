param(
    [Parameter(Mandatory = $true)]
    [ValidateSet('Add', 'Remove')]
    [string]$Action
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version 2.0

$configured = [string]$env:HERDR_SANDBOX_INSTALL_DIRECTORY
if ([string]::IsNullOrWhiteSpace($configured)) {
    throw 'HERDR_SANDBOX_INSTALL_DIRECTORY is required.'
}
$target = [IO.Path]::GetFullPath($configured).TrimEnd([char[]]@('\'))

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
        return [IO.Path]::GetFullPath($candidate).TrimEnd([char[]]@('\')) -ieq $Expected
    }
    catch {
        return $candidate.TrimEnd([char[]]@('\')) -ieq $Expected
    }
}

function Resolve-UserPathUpdate {
    param(
        [AllowEmptyString()]
        [string]$Current,
        [Parameter(Mandatory = $true)]
        [string]$Expected,
        [Parameter(Mandatory = $true)]
        [ValidateSet('Add', 'Remove')]
        [string]$RequestedAction,
        [bool]$ExpandVariables
    )

    $entries = @([regex]::Split($Current, ';'))
    $matches = @($entries | Where-Object {
            Test-PathEntry -Entry $_ -Expected $Expected -ExpandVariables $ExpandVariables
        })
    if ($RequestedAction -eq 'Add') {
        if ($matches.Count -gt 0) {
            return [pscustomobject]@{ Changed = $false; Value = $Current }
        }
        $updated = if ([string]::IsNullOrEmpty($Current)) {
            $Expected
        }
        else {
            $Current + ';' + $Expected
        }
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
        if ($index -eq $removeIndex) {
            continue
        }
        [void]$kept.Add([string]$entries[$index])
    }
    return [pscustomobject]@{
        Changed = $true
        Value = [string]::Join(';', [string[]]$kept)
    }
}

$changed = $false
$key = [Microsoft.Win32.Registry]::CurrentUser.OpenSubKey('Environment', $true)
if ($null -eq $key -and $Action -eq 'Remove') {
    return
}
if ($null -eq $key) {
    $key = [Microsoft.Win32.Registry]::CurrentUser.CreateSubKey('Environment')
}
if ($null -eq $key) {
    throw 'Could not open the current-user Environment registry key.'
}
try {
    $hasPath = @($key.GetValueNames()) -contains 'Path'
    if (-not $hasPath -and $Action -eq 'Remove') {
        return
    }

    $kind = if ($hasPath) {
        $key.GetValueKind('Path')
    }
    else {
        [Microsoft.Win32.RegistryValueKind]::ExpandString
    }
    if ($kind -ne [Microsoft.Win32.RegistryValueKind]::String -and
        $kind -ne [Microsoft.Win32.RegistryValueKind]::ExpandString) {
        throw "The current-user Path has unsupported registry kind '$kind'."
    }

    $current = if ($hasPath) {
        [string]$key.GetValue(
            'Path',
            '',
            [Microsoft.Win32.RegistryValueOptions]::DoNotExpandEnvironmentNames
        )
    }
    else {
        ''
    }
    $expandVariables = $kind -eq [Microsoft.Win32.RegistryValueKind]::ExpandString
    $update = Resolve-UserPathUpdate -Current $current -Expected $target -RequestedAction $Action `
        -ExpandVariables $expandVariables
    if ([bool]$update.Changed) {
        $key.SetValue('Path', [string]$update.Value, $kind)
        $changed = $true
    }
}
finally {
    $key.Dispose()
}

if ($changed) {
    exit 10
}
