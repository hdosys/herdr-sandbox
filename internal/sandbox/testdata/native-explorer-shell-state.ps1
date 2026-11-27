$ErrorActionPreference = 'Stop'
Add-Type -TypeDefinition @'
using System;
using System.Runtime.InteropServices;
public static class NativeExplorerState {
    [DllImport("user32.dll")] public static extern IntPtr GetShellWindow();
    [DllImport("user32.dll")] public static extern uint GetWindowThreadProcessId(IntPtr window, out uint pid);
}
'@
$window = [NativeExplorerState]::GetShellWindow()
if ($window -eq [IntPtr]::Zero) { throw 'No registered interactive shell survives.' }
$shellPID = [uint32]0
$null = [NativeExplorerState]::GetWindowThreadProcessId($window, [ref]$shellPID)
$shell = Get-Process -Id ([int]$shellPID) -ErrorAction Stop
if ($shell.Path -ine (Join-Path $env:WINDIR 'explorer.exe') -or $shell.SessionId -ne (Get-Process -Id $PID).SessionId) {
    throw 'The registered shell has the wrong image or interactive session.'
}
[pscustomobject]@{ pid = $shell.Id; sessionId = $shell.SessionId; startedAtUTC = $shell.StartTime.ToUniversalTime().ToString('O') } | ConvertTo-Json -Compress
