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

    $temporary = Join-Path $env:TEMP ('herdr-sandbox-uninstall-' + [Guid]::NewGuid().ToString('N') + '.exe')
    try {
        [IO.File]::Copy($uninstallerPath, $temporary, $false)
        $process = Start-Process -FilePath $temporary -ArgumentList @('/S', ('_?=' + $installRoot)) `
            -WindowStyle Hidden -PassThru
        if (-not $process.WaitForExit(1200000)) {
            try { $process.Kill() } catch {}
            throw 'Quiet uninstall exceeded its 20-minute wall-clock limit.'
        }
        exit $process.ExitCode
    }
    finally {
        if (Test-Path -LiteralPath $temporary) {
            Remove-Item -LiteralPath $temporary -Force
        }
    }
}
catch {
    [Console]::Error.WriteLine($_.Exception.Message)
    exit 1
}
