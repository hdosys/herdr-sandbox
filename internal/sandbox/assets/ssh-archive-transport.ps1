$launcherPath = Join-Path $transferRoot 'launcher.ps1'
$processOwnerPath = Join-Path $transferRoot 'provisioning-process.cs'
$applyMutex = $null
$applyMutexOwned = $false
try {
    $inputStream = [Console]::OpenStandardInput()
    $buffer = New-Object byte[] 8192
    foreach ($frame in @(
        @{ Path = $processOwnerPath; Length = $expectedProcessOwnerLength; Role = 'process owner' },
        @{ Path = $launcherPath; Length = $expectedLauncherLength; Role = 'launcher' },
        @{ Path = $archive; Length = $expectedArchiveLength; Role = 'archive' }
    )) {
        $outputStream = [IO.File]::Open($frame.Path, [IO.FileMode]::CreateNew, [IO.FileAccess]::Write, [IO.FileShare]::None)
        try {
            $remaining = [long]$frame.Length
            while ($remaining -gt 0) {
                $requested = [int][Math]::Min([long]$buffer.Length, $remaining)
                $readTask = $inputStream.ReadAsync($buffer, 0, $requested)
                $remainingMilliseconds = [long]($transportDeadline - [DateTime]::UtcNow).TotalMilliseconds
                if ($remainingMilliseconds -le 0 -or
                    -not $readTask.Wait([int][Math]::Min([int]::MaxValue, $remainingMilliseconds))) {
                    throw "SSH $($frame.Role) input transport exceeded its operation deadline with $remaining of $($frame.Length) bytes missing."
                }
                $read = $readTask.GetAwaiter().GetResult()
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
        $processOwnerDigest = ([BitConverter]::ToString($sha256.ComputeHash([IO.File]::ReadAllBytes($processOwnerPath)))).Replace('-', '').ToLowerInvariant()
    } finally {
        $sha256.Dispose()
    }
    if ($launcherDigest -cne $expectedLauncherDigest) { throw 'SSH launcher SHA-256 mismatch.' }
    if ($processOwnerDigest -cne $expectedProcessOwnerDigest) { throw 'SSH process owner SHA-256 mismatch.' }
    # sshd's descriptor table describes its own child handles. Native programs
    # launched below receive different handles from the process owner.
    Remove-Item Env:c28fc6f98a2c44abbbd89d6a3037d0d9_POSIX_FD_STATE -ErrorAction SilentlyContinue
    Remove-Item Env:PSModulePath -ErrorAction SilentlyContinue
    Add-Type -Path $processOwnerPath
    $applyMutex = New-Object Threading.Mutex($false, 'Local\HerdrSandbox-SSH-Provisioning')
    try {
        $applyMutexOwned = $applyMutex.WaitOne(30000)
    } catch [Threading.AbandonedMutexException] {
        $applyMutexOwned = $true
        throw 'The previous guest provisioning receiver ended unexpectedly. Its owned process tree is being terminated; inspect sandbox status before retrying.'
    }
    if (-not $applyMutexOwned) { throw 'The previous guest provisioning operation is still active. Its input and installed state were preserved; retry after it finishes.' }
    foreach ($otherAttempt in @(Get-ChildItem -LiteralPath $stagingRoot -Filter 'transport-*' -Force)) {
        if (-not [string]::Equals($otherAttempt.FullName, $transferRoot, [StringComparison]::OrdinalIgnoreCase)) {
            throw "Another SSH transfer has not completed cleanup: $($otherAttempt.FullName). The Sandbox and its input were preserved; resolve that interrupted transfer before retrying."
        }
    }
    $remainingMilliseconds = [long]($transportDeadline - [DateTime]::UtcNow).TotalMilliseconds
    if ($remainingMilliseconds -lt 1000) { throw 'SSH provisioning exceeded its operation deadline before execution.' }
    $spec = New-Object HerdrSandbox.ProvisioningProcessSpec
    $spec.Role = 'SSH provisioning launcher'
    $spec.FilePath = Join-Path $env:SystemRoot 'System32\WindowsPowerShell\v1.0\powershell.exe'
    $spec.Arguments = [string[]]@('-NoLogo','-NoProfile','-NonInteractive','-WindowStyle','Hidden','-ExecutionPolicy','Bypass','-File',$launcherPath)
    $spec.WorkingDirectory = $transferRoot
    $spec.StandardInputFile = $archive
    $spec.SeparateErrorOutput = $true
    $spec.TimeoutMilliseconds = [int][Math]::Min([int]::MaxValue, $remainingMilliseconds)
    $spec.SuccessExitCodes = [int[]]@(0)
    $spec.TerminateDescendantsAfterRootExit = $true
    $result = [HerdrSandbox.ProvisioningProcess]::RunWithInputLease($spec)
    [Console]::Out.Write([string]$result.Output)
    [Console]::Error.Write([string]$result.ErrorOutput)
    if ($result.OutputTruncated) { throw 'SSH provisioning output exceeded its bounded capture limit.' }
    if ($result.TimedOut) { throw 'SSH provisioning exceeded its operation deadline; its owned process tree was stopped.' }
    if ($result.Stopped) { throw 'SSH provisioning was cancelled or disconnected; its owned process tree was stopped.' }
    if (-not $result.Succeeded) { exit $result.ExitCode }
} catch {
    [Console]::Error.WriteLine([string]$_.Exception.Message)
    exit 1
} finally {
    try {
        Remove-GuestArchiveStaging
    } finally {
        if ($applyMutexOwned) { $applyMutex.ReleaseMutex() }
        if ($null -ne $applyMutex) { $applyMutex.Dispose() }
    }
}
exit 0
