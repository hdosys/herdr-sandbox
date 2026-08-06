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

function Stop-OwnedProcessTree {
    param([Parameter(Mandatory = $true)][Diagnostics.Process]$Process)

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
        if (-not $termination.WaitForExit(10000)) {
            try { $termination.Kill() } catch {}
            if (-not $termination.WaitForExit(10000)) {
                throw 'Quiet uninstall process-tree termination exceeded its 10-second cleanup limit.'
            }
        }
        if (-not $Process.WaitForExit(10000)) {
            throw 'Quiet uninstall process-tree termination did not stop the uninstaller within 10 seconds.'
        }
    }
    finally {
        $termination.Dispose()
    }
}

try {
    $installRoot = [IO.Path]::GetFullPath($InstallDirectory).TrimEnd([char[]]@('\'))
    $uninstallerPath = [IO.Path]::GetFullPath($Uninstaller)
    if ([IO.Path]::GetDirectoryName($uninstallerPath) -ine $installRoot -or
        [IO.Path]::GetFileName($uninstallerPath) -cne 'uninstall.exe') {
        throw 'Quiet uninstall received an unexpected uninstaller path.'
    }
    $item = Get-Item -LiteralPath $uninstallerPath -Force
    if ($item -isnot [IO.FileInfo] -or
        (($item.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) -or
        $item.Length -le 0) {
        throw 'Quiet uninstall requires a nonempty regular non-reparse uninstaller.'
    }

    $temporaryDirectory = Join-Path ([IO.Path]::GetTempPath()) `
        ('herdr-sandbox-uninstall-' + [Guid]::NewGuid().ToString('N'))
    $temporary = Join-Path $temporaryDirectory 'uninstall.exe'
    $process = $null
    try {
        [void][IO.Directory]::CreateDirectory($temporaryDirectory)
        [IO.File]::Copy($uninstallerPath, $temporary, $false)
        $process = Start-Process -FilePath $temporary -ArgumentList @('/S', ('_?=' + $installRoot)) `
            -WindowStyle Hidden -PassThru
        if (-not $process.WaitForExit(1200000)) {
            Stop-OwnedProcessTree -Process $process
            throw 'Quiet uninstall exceeded its 20-minute wall-clock limit.'
        }
        exit $process.ExitCode
    }
    finally {
        $processStopped = $null -eq $process -or $process.HasExited
        if ($null -ne $process) {
            $process.Dispose()
        }
        if ($processStopped) {
            if (Test-Path -LiteralPath $temporary) {
                [IO.File]::Delete($temporary)
            }
            if (Test-Path -LiteralPath $temporaryDirectory -PathType Container) {
                [IO.Directory]::Delete($temporaryDirectory, $false)
            }
        }
    }
}
catch {
    [Console]::Error.WriteLine($_.Exception.Message)
    exit 1
}
