# herdr-sandbox-active-session-launch-contract: 1
[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [string]$Executable,
    [Parameter(Mandatory = $true)]
    [string]$ArgumentsBase64
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version 2.0

function Get-HerdrTradingViewProcesses {
    param(
        [Parameter(Mandatory = $true)][string]$ExpectedExecutable,
        [Parameter(Mandatory = $true)][int]$SessionID
    )

    $matches = @()
    foreach ($process in @(Get-Process -Name 'TradingView' -ErrorAction SilentlyContinue)) {
        try {
            if ([int]$process.SessionId -eq $SessionID -and
                [IO.Path]::GetFullPath([string]$process.Path) -ieq $ExpectedExecutable) {
                $matches += $process
            }
        } catch { }
    }
    return @($matches)
}

function Get-HerdrTradingViewCDPState {
    param(
        [Parameter(Mandatory = $true)][string]$ExpectedExecutable,
        [Parameter(Mandatory = $true)][int]$SessionID,
        [Parameter(Mandatory = $true)][int]$Port
    )

    $listeners = @(Get-NetTCPConnection -State Listen -LocalPort $Port -ErrorAction SilentlyContinue |
        Where-Object { [string]$_.LocalAddress -in @('127.0.0.1', '::1') })
    if ($listeners.Count -eq 0) { return $null }
    $owners = @($listeners | ForEach-Object { [int]$_.OwningProcess } | Sort-Object -Unique)
    if ($owners.Count -ne 1) {
        throw "CDP port $Port has unexpected listener owners: $($owners -join ', ')"
    }
    $owner = Get-Process -Id $owners[0] -ErrorAction SilentlyContinue
    if ($null -eq $owner -or [int]$owner.SessionId -ne $SessionID) {
        throw "CDP port $Port is owned outside interactive session $SessionID."
    }
    try {
        $ownerPath = [IO.Path]::GetFullPath([string]$owner.Path)
    } catch {
        throw "CDP port $Port owner path is unreadable."
    }
    if ($ownerPath -ine $ExpectedExecutable) {
        throw "CDP port $Port is owned by an unexpected executable: $ownerPath"
    }
    try {
        $response = Invoke-WebRequest -Uri "http://127.0.0.1:$Port/json/version" -UseBasicParsing -TimeoutSec 2
        $version = [string]$response.Content | ConvertFrom-Json
    } catch {
        return $null
    }
    if ([string]$version.Browser -notmatch '^Chrome/\d+' -or
        [string]$version.'User-Agent' -notmatch '\bTVDesktop/\d+' -or
        [string]$version.webSocketDebuggerUrl -notmatch ('^ws://127\.0\.0\.1:' + $Port + '/devtools/browser/')) {
        throw "CDP port $Port did not expose the expected TradingView Desktop endpoint."
    }
    return [pscustomobject]@{
        PID = [int]$owners[0]
        Browser = [string]$version.Browser
        UserAgent = [string]$version.'User-Agent'
    }
}

function Stop-HerdrTradingViewProcessTree {
    param(
        [Parameter(Mandatory = $true)][int]$RootPID,
        [Parameter(Mandatory = $true)][string]$ExpectedExecutable,
        [Parameter(Mandatory = $true)][int]$SessionID
    )

    $rows = @(Get-CimInstance -ClassName Win32_Process -ErrorAction SilentlyContinue)
    $owned = New-Object 'System.Collections.Generic.HashSet[int]'
    $owned.Add($RootPID) | Out-Null
    do {
        $added = $false
        foreach ($row in $rows) {
            if ($owned.Contains([int]$row.ParentProcessId) -and $owned.Add([int]$row.ProcessId)) {
                $added = $true
            }
        }
    } while ($added)
    $ownedProcessIDs = @($owned)
    foreach ($processID in @($ownedProcessIDs | Sort-Object -Descending)) {
        $process = Get-Process -Id $processID -ErrorAction SilentlyContinue
        if ($null -eq $process) { continue }
        try {
            if ([int]$process.SessionId -ne $SessionID -or
                [IO.Path]::GetFullPath([string]$process.Path) -ine $ExpectedExecutable) {
                throw "Refusing to stop changed launch process $processID."
            }
            $process | Stop-Process -Force -ErrorAction Stop
        } catch {
            throw "TradingView launch cleanup failed for PID $processID`: $($_.Exception.Message)"
        }
    }
    $deadline = [DateTime]::UtcNow.AddSeconds(5)
    while (@($ownedProcessIDs | Where-Object { $null -ne (Get-Process -Id $_ -ErrorAction SilentlyContinue) }).Count -ne 0) {
        if ([DateTime]::UtcNow -ge $deadline) {
            throw "TradingView launch process tree rooted at PID $RootPID did not stop within five seconds."
        }
        Start-Sleep -Milliseconds 100
    }
}

function Publish-HerdrActiveSessionLaunchStatusScript {
    param(
        [Parameter(Mandatory = $true)][string]$LaunchID,
        [Parameter(Mandatory = $true)][string]$TaskName,
        [Parameter(Mandatory = $true)][string]$StatusPath,
        [Parameter(Mandatory = $true)][string]$ExpectedExecutable,
        [Parameter(Mandatory = $true)][string]$Argument,
        [Parameter(Mandatory = $true)][int]$SessionID
    )

    $script = @'
$ErrorActionPreference = 'Stop'
function Publish-LaunchStatus {
    param([string]$State, [int]$PIDValue = 0, [string]$Message = '')
    $record = [ordered]@{
        schemaVersion = 1
        launchId = '__LAUNCH_ID__'
        taskName = '__TASK_NAME__'
        state = $State
        sessionId = __SESSION_ID__
        pid = $PIDValue
        message = $Message
    } | ConvertTo-Json -Compress
    $temporaryPath = '__STATUS_PATH__.' + [Guid]::NewGuid().ToString('N') + '.tmp'
    try {
        [IO.File]::WriteAllText($temporaryPath, $record, (New-Object Text.UTF8Encoding($false)))
        [IO.File]::Move($temporaryPath, '__STATUS_PATH__')
    } finally {
        if (Test-Path -LiteralPath $temporaryPath) { [IO.File]::Delete($temporaryPath) }
    }
}
function Stop-LaunchProcessTree {
    param([Parameter(Mandatory = $true)][int]$RootPID)
    $rows = @(Get-CimInstance -ClassName Win32_Process -ErrorAction SilentlyContinue)
    $owned = New-Object 'System.Collections.Generic.HashSet[int]'
    $owned.Add($RootPID) | Out-Null
    do {
        $added = $false
        foreach ($row in $rows) {
            if ($owned.Contains([int]$row.ParentProcessId) -and $owned.Add([int]$row.ProcessId)) { $added = $true }
        }
    } while ($added)
    $ownedProcessIDs = @($owned)
    foreach ($processID in @($ownedProcessIDs | Sort-Object -Descending)) {
        $ownedProcess = Get-Process -Id $processID -ErrorAction SilentlyContinue
        if ($null -eq $ownedProcess) { continue }
        if ([int]$ownedProcess.SessionId -ne __SESSION_ID__ -or
            [IO.Path]::GetFullPath([string]$ownedProcess.Path) -ine '__EXECUTABLE__') {
            throw "Refusing to stop changed launch process $processID."
        }
        $ownedProcess | Stop-Process -Force -ErrorAction Stop
    }
    $deadline = [DateTime]::UtcNow.AddSeconds(5)
    while (@($ownedProcessIDs | Where-Object { $null -ne (Get-Process -Id $_ -ErrorAction SilentlyContinue) }).Count -ne 0) {
        if ([DateTime]::UtcNow -ge $deadline) { throw "Launch process tree rooted at PID $RootPID did not stop." }
        Start-Sleep -Milliseconds 100
    }
}
$process = $null
try {
    $sessionID = [int](Get-Process -Id $PID).SessionId
    if ($sessionID -ne __SESSION_ID__) {
        throw "Active-session launch task ran in session $sessionID instead of __SESSION_ID__."
    }
    $process = Start-Process -FilePath '__EXECUTABLE__' -ArgumentList @('__ARGUMENT__') `
        -WorkingDirectory '__WORKING_DIRECTORY__' -PassThru
    if ($null -eq $process -or [int]$process.SessionId -ne $sessionID) {
        throw 'TradingView did not start in the task session.'
    }
    Publish-LaunchStatus -State 'started' -PIDValue ([int]$process.Id)
} catch {
    $message = [string]$_.Exception.Message
    if ($message.Length -gt 4096) { $message = $message.Substring(0, 4096) }
    if ($null -ne $process) {
        try { Stop-LaunchProcessTree -RootPID ([int]$process.Id) } catch { $message += '; process cleanup failed: ' + [string]$_.Exception.Message }
    }
    $failedPID = if ($null -eq $process) { 0 } else { [int]$process.Id }
    try { Publish-LaunchStatus -State 'failed' -PIDValue $failedPID -Message $message } catch { }
    exit 1
}
'@
    return $script.Replace('__LAUNCH_ID__', $LaunchID).
        Replace('__TASK_NAME__', $TaskName.Replace("'", "''")).
        Replace('__STATUS_PATH__', $StatusPath.Replace("'", "''")).
        Replace('__EXECUTABLE__', $ExpectedExecutable.Replace("'", "''")).
        Replace('__ARGUMENT__', $Argument.Replace("'", "''")).
        Replace('__WORKING_DIRECTORY__', (Split-Path -Parent $ExpectedExecutable).Replace("'", "''")).
        Replace('__SESSION_ID__', [string]$SessionID)
}

$mutex = $null
$mutexAcquired = $false
$taskService = $null
$taskRoot = $null
$registeredTask = $null
$runningTask = $null
$definition = $null
$action = $null
$statusPath = ''
$startedPID = 0
$interactiveSessionID = 0
$launchSucceeded = $false
try {
    if (-not [IO.Path]::IsPathRooted($Executable) -or
        -not (Test-Path -LiteralPath $Executable -PathType Leaf)) {
        throw "TradingView executable is missing: $Executable"
    }
    $Executable = [IO.Path]::GetFullPath($Executable)
    $expectedRoot = 'C:\HerdrSandbox\tools\TradingView.TradingViewDesktop\'
    $executableInfo = Get-Item -LiteralPath $Executable -Force
    if (-not $Executable.StartsWith($expectedRoot, [StringComparison]::OrdinalIgnoreCase) -or
        [IO.Path]::GetFileName($Executable) -cne 'TradingView.exe' -or
        ($executableInfo.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {
        throw "TradingView executable is outside the verified portable root: $Executable"
    }
    try {
        $argumentJSON = [Text.Encoding]::UTF8.GetString([Convert]::FromBase64String($ArgumentsBase64))
        $decodedArguments = $argumentJSON | ConvertFrom-Json
        $argumentValues = @($decodedArguments)
    } catch {
        throw "TradingView arguments are invalid: $($_.Exception.Message)"
    }
    $argumentMatch = if ($argumentValues.Count -eq 1 -and $argumentValues[0] -is [string]) {
        [regex]::Match([string]$argumentValues[0], '^--remote-debugging-port=([1-9][0-9]{0,4})$')
    } else {
        $null
    }
    if ($null -eq $argumentMatch -or -not $argumentMatch.Success) {
        throw 'TradingView requires exactly one remote-debugging-port argument.'
    }
    $port = [int]$argumentMatch.Groups[1].Value
    if ($port -gt 65535) { throw "TradingView CDP port is invalid: $port" }
    $argument = [string]$argumentValues[0]

    $explorerProcesses = @(Get-Process -Name 'explorer' -ErrorAction SilentlyContinue)
    $sessionIDs = @($explorerProcesses | ForEach-Object { [int]$_.SessionId } | Sort-Object -Unique)
    if ($sessionIDs.Count -ne 1) {
        throw "Interactive Explorer session is ambiguous: $($sessionIDs -join ', ')"
    }
    $interactiveSessionID = [int]$sessionIDs[0]
    $mutex = New-Object Threading.Mutex($false, 'Global\HerdrSandbox.TradingViewActiveSessionLaunch')
    try {
        $mutexAcquired = $mutex.WaitOne([TimeSpan]::FromSeconds(20))
    } catch [Threading.AbandonedMutexException] {
        $mutexAcquired = $true
    }
    if (-not $mutexAcquired) { throw 'Timed out waiting for another TradingView launch.' }

    $cdp = Get-HerdrTradingViewCDPState -ExpectedExecutable $Executable `
        -SessionID $interactiveSessionID -Port $port
    if ($null -ne $cdp) {
        [Console]::Out.WriteLine(([ordered]@{
            schemaVersion = 1
            state = 'already-running'
            pid = [int]$cdp.PID
            sessionId = $interactiveSessionID
            cdpPort = $port
            browser = [string]$cdp.Browser
            userAgent = [string]$cdp.UserAgent
        } | ConvertTo-Json -Compress))
        exit 0
    }
    $existingProcesses = @(Get-HerdrTradingViewProcesses -ExpectedExecutable $Executable `
        -SessionID $interactiveSessionID)
    if ($existingProcesses.Count -ne 0) {
        throw "TradingView is already running in interactive session $interactiveSessionID without ready CDP on port $port."
    }

    $currentSessionID = [int](Get-Process -Id $PID).SessionId
    if ($currentSessionID -eq $interactiveSessionID) {
        $process = Start-Process -FilePath $Executable -ArgumentList @($argument) `
            -WorkingDirectory (Split-Path -Parent $Executable) -PassThru
        $startedPID = [int]$process.Id
        if ([int]$process.SessionId -ne $interactiveSessionID) {
            throw "TradingView started in session $([int]$process.SessionId) instead of $interactiveSessionID."
        }
    } else {
        $stateDirectory = 'C:\HerdrSandbox\tools\tvcontrol\active-session-state'
        if (-not (Test-Path -LiteralPath $stateDirectory)) {
            New-Item -ItemType Directory -Path $stateDirectory -Force | Out-Null
        }
        $stateDirectoryInfo = Get-Item -LiteralPath $stateDirectory -Force
        if (-not $stateDirectoryInfo.PSIsContainer -or
            ($stateDirectoryInfo.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {
            throw "Active-session launch state directory is unsafe: $stateDirectory"
        }
        $launchID = [Guid]::NewGuid().ToString('N')
        $taskName = 'HerdrSandbox-TradingViewLaunch-' + $launchID
        $statusPath = Join-Path $stateDirectory ($launchID + '.json')
        $taskScript = Publish-HerdrActiveSessionLaunchStatusScript -LaunchID $launchID `
            -TaskName $taskName -StatusPath $statusPath -ExpectedExecutable $Executable `
            -Argument $argument -SessionID $interactiveSessionID
        $encodedTaskScript = [Convert]::ToBase64String([Text.Encoding]::Unicode.GetBytes($taskScript))

        $taskService = New-Object -ComObject 'Schedule.Service'
        $taskService.Connect()
        $taskRoot = $taskService.GetFolder('\')
        $definition = $taskService.NewTask(0)
        $definition.RegistrationInfo.Description = 'One-shot visible TradingView launch in the active interactive session'
        $definition.Settings.Enabled = $true
        $definition.Settings.Hidden = $true
        $definition.Settings.AllowDemandStart = $true
        $definition.Settings.ExecutionTimeLimit = 'PT1M'
        $definition.Principal.UserId = [Security.Principal.WindowsIdentity]::GetCurrent().Name
        $definition.Principal.LogonType = 3
        $definition.Principal.RunLevel = 1
        $action = $definition.Actions.Create(0)
        $action.Path = Join-Path $env:SystemRoot 'System32\WindowsPowerShell\v1.0\powershell.exe'
        $action.Arguments = "-NoLogo -NoProfile -NonInteractive -WindowStyle Hidden -EncodedCommand $encodedTaskScript"
        $registeredTask = $taskRoot.RegisterTaskDefinition($taskName, $definition, 2, $null, $null, 3, $null)
        $runningTask = $registeredTask.Run($null)

        $statusDeadline = [DateTime]::UtcNow.AddSeconds(10)
        while (-not (Test-Path -LiteralPath $statusPath -PathType Leaf)) {
            if ([DateTime]::UtcNow -ge $statusDeadline) {
                throw 'Active-session launch task did not publish status within ten seconds.'
            }
            Start-Sleep -Milliseconds 100
        }
        $statusInfo = Get-Item -LiteralPath $statusPath -Force
        if (($statusInfo.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0 -or
            $statusInfo.Length -le 0 -or $statusInfo.Length -gt 65536) {
            throw "Active-session launch status is unsafe: $statusPath"
        }
        $status = [IO.File]::ReadAllText($statusPath) | ConvertFrom-Json
        $statusProperties = @($status.PSObject.Properties.Name | Sort-Object)
        if (($statusProperties -join '|') -cne 'launchId|message|pid|schemaVersion|sessionId|state|taskName' -or
            [int]$status.schemaVersion -ne 1 -or [string]$status.launchId -cne $launchID -or
            [string]$status.taskName -cne $taskName -or [int]$status.sessionId -ne $interactiveSessionID) {
            throw 'Active-session launch status identity is invalid.'
        }
        if ([int]$status.pid -gt 0) { $startedPID = [int]$status.pid }
        if ([string]$status.state -ceq 'failed') {
            throw "Active-session launch task failed: $([string]$status.message)"
        }
        if ([string]$status.state -cne 'started' -or [int]$status.pid -lt 1) {
            throw "Active-session launch task returned invalid state: $([string]$status.state)"
        }
        $startedPID = [int]$status.pid
    }

    $readyDeadline = [DateTime]::UtcNow.AddSeconds(20)
    do {
        $cdp = Get-HerdrTradingViewCDPState -ExpectedExecutable $Executable `
            -SessionID $interactiveSessionID -Port $port
        if ($null -ne $cdp) { break }
        if ($null -eq (Get-Process -Id $startedPID -ErrorAction SilentlyContinue)) {
            throw "TradingView launch PID $startedPID exited before CDP became ready."
        }
        if ([DateTime]::UtcNow -ge $readyDeadline) {
            throw "TradingView CDP did not become ready on port $port within 20 seconds."
        }
        Start-Sleep -Milliseconds 250
    } while ($true)
    $launchSucceeded = $true
    [Console]::Out.WriteLine(([ordered]@{
        schemaVersion = 1
        state = 'started'
        pid = [int]$cdp.PID
        sessionId = $interactiveSessionID
        cdpPort = $port
        browser = [string]$cdp.Browser
        userAgent = [string]$cdp.UserAgent
    } | ConvertTo-Json -Compress))
} catch {
    $message = [string]$_.Exception.Message
    if ($startedPID -gt 0) {
        try {
            Stop-HerdrTradingViewProcessTree -RootPID $startedPID -ExpectedExecutable $Executable `
                -SessionID $interactiveSessionID
        } catch {
            $message += '; ' + [string]$_.Exception.Message
        }
    }
    [Console]::Error.WriteLine($message)
    exit 1
} finally {
    if (-not [string]::IsNullOrWhiteSpace($statusPath) -and (Test-Path -LiteralPath $statusPath)) {
        try { [IO.File]::Delete($statusPath) } catch { }
    }
    if (-not $launchSucceeded -and $null -ne $runningTask -and [int]$runningTask.State -eq 4) {
        try { $runningTask.Stop() } catch { }
    }
    if ($null -ne $registeredTask -and $null -ne $taskRoot) {
        try { $taskRoot.DeleteTask([string]$registeredTask.Name, 0) } catch { }
    }
    foreach ($comObject in @($runningTask, $registeredTask, $action, $definition, $taskRoot, $taskService)) {
        if ($null -ne $comObject) {
            try { [void][Runtime.InteropServices.Marshal]::FinalReleaseComObject($comObject) } catch { }
        }
    }
    if ($mutexAcquired -and $null -ne $mutex) { $mutex.ReleaseMutex() }
    if ($null -ne $mutex) { $mutex.Dispose() }
}
