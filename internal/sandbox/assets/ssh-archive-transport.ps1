$launcherPath = Join-Path $transferRoot 'launcher.ps1'
try {
    $inputStream = [Console]::OpenStandardInput()
    $buffer = New-Object byte[] 8192
    foreach ($frame in @(
        @{ Path = $launcherPath; Length = $expectedLauncherLength; Role = 'launcher' },
        @{ Path = $archive; Length = $expectedArchiveLength; Role = 'archive' }
    )) {
        $outputStream = [IO.File]::Open($frame.Path, [IO.FileMode]::CreateNew, [IO.FileAccess]::Write, [IO.FileShare]::None)
        try {
            $remaining = [long]$frame.Length
            while ($remaining -gt 0) {
                $requested = [int][Math]::Min([long]$buffer.Length, $remaining)
                $read = $inputStream.Read($buffer, 0, $requested)
                if ($read -le 0) { throw "SSH $($frame.Role) transport ended with $remaining bytes missing." }
                $outputStream.Write($buffer, 0, $read)
                $remaining -= $read
            }
            $outputStream.Flush($true)
        } finally {
            $outputStream.Dispose()
        }
    }
    $sha256 = [Security.Cryptography.SHA256]::Create()
    try {
        $launcherDigest = ([BitConverter]::ToString($sha256.ComputeHash([IO.File]::ReadAllBytes($launcherPath)))).Replace('-', '').ToLowerInvariant()
    } finally {
        $sha256.Dispose()
    }
    if ($launcherDigest -cne $expectedLauncherDigest) { throw 'SSH launcher SHA-256 mismatch.' }
    Remove-Item Env:PSModulePath -ErrorAction SilentlyContinue
    $process = Start-Process -FilePath 'powershell.exe' -ArgumentList @('-NoLogo','-NoProfile','-NonInteractive','-WindowStyle','Hidden','-ExecutionPolicy','Bypass','-File',('"' + $launcherPath + '"')) -RedirectStandardInput $archive -NoNewWindow -Wait -PassThru
    if ($process.ExitCode -ne 0) { exit $process.ExitCode }
} catch {
    [Console]::Error.WriteLine([string]$_.Exception.Message)
    exit 1
} finally {
    Remove-GuestArchiveStaging
}
exit 0
