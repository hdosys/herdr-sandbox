# herdr-sandbox-current-sandbox-identity-contract: 1
param()

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version 2.0

function Get-ProcessIdentity {
    param(
        [Parameter(Mandatory = $true)][string]$Role,
        [Parameter(Mandatory = $true)][CimInstance]$Process
    )

    $runtime = Get-Process -Id ([int]$Process.ProcessId) -ErrorAction Stop
    return [ordered]@{
        role = $Role
        pid = [int]$Process.ProcessId
        sessionId = [int]$runtime.SessionId
        creationUtc = $runtime.StartTime.ToUniversalTime().ToString('o')
        executable = [IO.Path]::GetFullPath([string]$Process.ExecutablePath)
    }
}

$herdr = @()
foreach ($process in @(Get-CimInstance Win32_Process -Filter "Name='herdr.exe'" -ErrorAction Stop)) {
    $commandLine = [string]$process.CommandLine
    $role = ''
    if ($commandLine -match '(?:^|\s)server(?:\s|$)') {
        $role = 'server'
    } elseif ($commandLine -match '(?:^|\s)remote-client-bridge(?:\s|$)') {
        $role = 'remote-client-bridge'
    }
    if (-not [string]::IsNullOrEmpty($role)) {
        $herdr += Get-ProcessIdentity -Role $role -Process $process
    }
}

$service = Get-CimInstance Win32_Service -Filter "Name='sshd'" -ErrorAction Stop
if ([string]$service.State -cne 'Running' -or [int]$service.ProcessId -le 0) {
    throw 'OpenSSH service is not running.'
}
$sshd = Get-CimInstance Win32_Process -Filter ("ProcessId=" + [string]$service.ProcessId) -ErrorAction Stop

[Console]::Out.WriteLine(([ordered]@{
    schemaVersion = 1
    herdr = @($herdr | Sort-Object pid)
    sshd = Get-ProcessIdentity -Role 'sshd-service' -Process $sshd
} | ConvertTo-Json -Depth 4 -Compress))
