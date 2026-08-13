param(
    [Parameter(Mandatory = $true)]
    [ValidateNotNullOrEmpty()]
    [string]$Uninstaller,

    [Parameter(Mandatory = $true)]
    [ValidateNotNullOrEmpty()]
    [string]$InstallDirectory
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version 2.0
$total = [Diagnostics.Stopwatch]::StartNew()

function Test-FullyQualifiedWindowsPath {
    param([Parameter(Mandatory = $true)][string]$Path)

    return (-not [string]::IsNullOrWhiteSpace($Path) -and
        ($Path -match '^[A-Za-z]:[\\/]' -or $Path -match '^[\\/]{2}[^\\/]+[\\/][^\\/]+(?:[\\/]|$)'))
}

function Get-FileSHA256 {
    param([Parameter(Mandatory = $true)][string]$Path)

    $stream = $null
    $hasher = $null
    try {
        $stream = [IO.File]::Open($Path, [IO.FileMode]::Open, [IO.FileAccess]::Read, [IO.FileShare]::Read)
        $hasher = [Security.Cryptography.SHA256]::Create()
        return [BitConverter]::ToString($hasher.ComputeHash($stream)).Replace('-', '')
    }
    finally {
        if ($null -ne $hasher) { $hasher.Dispose() }
        if ($null -ne $stream) { $stream.Dispose() }
    }
}

function Stop-OwnedProcessTree {
    param([Parameter(Mandatory = $true)][Diagnostics.Process]$Process)

    $cleanup = [Diagnostics.Stopwatch]::StartNew()
    $taskkillPath = Join-Path $env:SystemRoot 'System32\taskkill.exe'
    $taskkillItem = Get-Item -LiteralPath $taskkillPath -Force
    if ($taskkillItem -isnot [IO.FileInfo] -or
        (($taskkillItem.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) -or
        $taskkillItem.Length -le 0) {
        throw 'Quiet uninstall could not validate the Windows process-tree terminator.'
    }
    $termination = Start-Process -FilePath $taskkillPath `
        -ArgumentList @('/PID', [string]$Process.Id, '/T', '/F') -WindowStyle Hidden -PassThru
    try {
        if (-not $termination.WaitForExit(2500)) {
            try { $termination.Kill() } catch {}
            $remaining = [Math]::Max(0, 5000 - [int]$cleanup.ElapsedMilliseconds)
            if (-not $termination.WaitForExit($remaining)) {
                throw 'Quiet uninstall process-tree termination exceeded its 5-second cleanup limit.'
            }
        }
        $remaining = [Math]::Max(0, 5000 - [int]$cleanup.ElapsedMilliseconds)
        if (-not $Process.WaitForExit($remaining)) {
            throw 'Quiet uninstall process-tree termination did not stop the uninstaller within 5 seconds.'
        }
    }
    finally {
        $termination.Dispose()
        $cleanup.Stop()
    }
}

# Never keep the install directory as this wrapper's current directory.
$tempRoot = [IO.Path]::GetTempPath()
[Environment]::CurrentDirectory = $tempRoot
Set-Location -LiteralPath $tempRoot

$exitCode = 80
$tempDirectory = $null
$process = $null
try {
    if (-not (Test-FullyQualifiedWindowsPath -Path $InstallDirectory) -or
        -not (Test-FullyQualifiedWindowsPath -Path $Uninstaller)) {
        throw 'Uninstall paths must be fully qualified.'
    }

    $installPath = [IO.Path]::GetFullPath($InstallDirectory)
    $installRoot = [IO.Path]::GetPathRoot($installPath)
    if ($installPath.Length -gt $installRoot.Length) {
        $installPath = $installPath.TrimEnd([char[]]@('\', '/'))
    }
    $uninstallerPath = [IO.Path]::GetFullPath($Uninstaller)
    if (-not ([IO.Path]::GetDirectoryName($uninstallerPath) -ieq $installPath) -or
        [IO.Path]::GetFileName($uninstallerPath) -ine 'uninstall.exe') {
        throw 'The registered uninstaller is not directly inside the registered install directory.'
    }

    $installItem = Get-Item -LiteralPath $installPath -Force
    if (-not $installItem.PSIsContainer -or
        (($installItem.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0)) {
        throw 'The registered install directory is not a regular directory.'
    }
    $uninstallerItem = Get-Item -LiteralPath $uninstallerPath -Force
    if ($uninstallerItem.PSIsContainer -or
        (($uninstallerItem.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) -or
        $uninstallerItem.Length -le 0) {
        throw 'The registered uninstaller is not a nonempty regular file.'
    }

    $tempDirectory = Join-Path $tempRoot ('nsis-uninstall-' + [Guid]::NewGuid().ToString('N'))
    [void](New-Item -ItemType Directory -Path $tempDirectory)
    $tempUninstaller = Join-Path $tempDirectory 'uninstall.exe'
    Copy-Item -LiteralPath $uninstallerPath -Destination $tempUninstaller -Force
    $copiedItem = Get-Item -LiteralPath $tempUninstaller -Force
    if ($copiedItem.PSIsContainer -or
        (($copiedItem.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) -or
        $copiedItem.Length -ne $uninstallerItem.Length) {
        throw 'The temporary uninstaller copy did not validate.'
    }
    $sourceHash = Get-FileSHA256 -Path $uninstallerPath
    $copiedHash = Get-FileSHA256 -Path $tempUninstaller
    if ([string]$sourceHash -cne [string]$copiedHash) {
        throw 'The temporary uninstaller copy failed SHA-256 verification.'
    }

    $startInfo = New-Object Diagnostics.ProcessStartInfo
    $startInfo.FileName = $tempUninstaller
    # NSIS requires _?= to be the final, unquoted argument. It consumes the
    # remainder of the command line as the path, including embedded spaces.
    $startInfo.Arguments = '/S _?=' + $installPath
    $startInfo.WorkingDirectory = $tempRoot
    $startInfo.UseShellExecute = $false
    $startInfo.CreateNoWindow = $true
    $process = [Diagnostics.Process]::Start($startInfo)
    if ($null -eq $process) {
        throw 'Windows did not start the copied uninstaller.'
    }
    $operationBudget = [Math]::Max(0, 25000 - [int]$total.ElapsedMilliseconds)
    if (-not $process.WaitForExit($operationBudget)) {
        Stop-OwnedProcessTree -Process $process
        throw 'Quiet uninstall exceeded its 30-second total limit.'
    }
    $exitCode = $process.ExitCode
}
catch {
    [Console]::Error.WriteLine($_.Exception.Message)
}
finally {
    if ($null -ne $process) {
        $process.Dispose()
    }
    if ($null -ne $tempDirectory) {
        Remove-Item -LiteralPath $tempDirectory -Recurse -Force -ErrorAction SilentlyContinue
    }
    $total.Stop()
}

exit $exitCode
