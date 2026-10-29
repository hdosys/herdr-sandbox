$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'

function Read-ProcessSnapshot($Item) {
    if ($null -eq $Item) { return $null }
    $process = $null
    try {
        $process = Get-Process -Id $Item.ProcessId -ErrorAction Stop
        if ($null -eq $process) { throw 'Get-Process returned no process.' }
        $handle = $process.Handle
        $startedAtUTC = $process.StartTime.ToUniversalTime().ToString('O')
        return [ordered]@{
            pid = [int]$Item.ProcessId
            name = [string]$Item.Name
            executablePath = [string]$Item.ExecutablePath
            startedAtUTC = $startedAtUTC
            commandLine = [string]$Item.CommandLine
        }
    } catch {
        $remaining = Get-CimInstance Win32_Process -Filter ('ProcessId = ' + $Item.ProcessId)
        if ($null -eq $remaining) { return $null }
        throw
    } finally {
        if ($null -ne $process) { $process.Dispose() }
    }
}

$filter = 'ProcessId = ' + $inspectPID
if ($includeRunning) {
    $filter += " OR Name = 'WindowsSandbox.exe' OR Name = 'WindowsSandboxClient.exe'"
}
$items = @(Get-CimInstance Win32_Process -Filter $filter)
$processes = @()
if ($includeRunning) {
    $processes = @($items | Where-Object { $_.Name -in @('WindowsSandbox.exe', 'WindowsSandboxClient.exe') } | ForEach-Object {
        [ordered]@{
            name = [IO.Path]::GetFileNameWithoutExtension([string]$_.Name)
            pid = [int]$_.ProcessId
            parentPID = [int]$_.ParentProcessId
        }
    })
}
$snapshot = $null
if ($inspectPID -gt 0) {
    $item = $items | Where-Object { $_.ProcessId -eq $inspectPID }
    $snapshot = Read-ProcessSnapshot $item
}
[ordered]@{ processes = $processes; snapshot = $snapshot } | ConvertTo-Json -Depth 4 -Compress
