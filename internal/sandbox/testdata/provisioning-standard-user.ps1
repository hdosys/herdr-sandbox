param([Parameter(Mandatory=$true)][string]$ProcessOwnerPath, [switch]$Interactive)
$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'
if ($Interactive) {
    $identity = [Security.Principal.WindowsIdentity]::GetCurrent()
    $sessions = @(Get-Process -Name explorer -IncludeUserName | Where-Object {$_.UserName -ieq $identity.Name -and $_.SessionId -gt 0} | Select-Object -ExpandProperty SessionId -Unique)
    if ($sessions.Count -ne 1) { throw 'Expected one intended-user desktop.' }
    $root = Split-Path -Parent $ProcessOwnerPath
    $name = 'HerdrStandardUserProbe-' + (Split-Path -Leaf $root)
    $resultPath = Join-Path $root 'result.txt'
    $actionScript = '$ErrorActionPreference="Stop"; $ProgressPreference="SilentlyContinue"; try {$text=& "'+$PSCommandPath+'" -ProcessOwnerPath "'+$ProcessOwnerPath+'" | Out-String} catch {$e=$_.Exception.GetBaseException(); $text="Probe failed: "+$e.Message+"; native error "+$e.NativeErrorCode}; [IO.File]::WriteAllText("'+$resultPath+'",$text)'
    $scheduler = New-Object -ComObject Schedule.Service
    $scheduler.Connect()
    $folder = $scheduler.GetFolder('\')
    $definition = $scheduler.NewTask(0)
    $definition.Principal.UserId = $identity.User.Value
    $definition.Principal.LogonType = 3
    $definition.Principal.RunLevel = 1
    $definition.Settings.ExecutionTimeLimit = 'PT30S'
    $action = $definition.Actions.Create(0)
    $action.Path = Join-Path $PSHOME 'powershell.exe'
    $action.Arguments = '-NoLogo -NoProfile -NonInteractive -WindowStyle Hidden -ExecutionPolicy Bypass -EncodedCommand ' + [Convert]::ToBase64String([Text.Encoding]::Unicode.GetBytes($actionScript))
    $task = $folder.RegisterTaskDefinition($name,$definition,2,$null,$null,3,$null)
    try {
        $running = $task.RunEx($null,4,[int]$sessions[0],$null)
        $deadline = [DateTime]::UtcNow.AddSeconds(40)
        while (-not (Test-Path -LiteralPath $resultPath)) {
            if ([DateTime]::UtcNow -ge $deadline) { $running.Stop(); throw 'Probe task deadline exceeded.' }
            Start-Sleep -Milliseconds 100
        }
        [IO.File]::ReadAllText($resultPath)
    } finally { $folder.DeleteTask($name,0) }
    return
}
Add-Type -Path $ProcessOwnerPath
$identity = [Security.Principal.WindowsIdentity]::GetCurrent()
$session = [Diagnostics.Process]::GetCurrentProcess().SessionId
$child = @'
$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'
$identity = [Security.Principal.WindowsIdentity]::GetCurrent()
$principal = New-Object Security.Principal.WindowsPrincipal($identity)
$groups = @(& whoami.exe /groups /fo csv /nh | ConvertFrom-Csv -Header Name,Type,SID,Attributes)
$privileges = @(& whoami.exe /priv /fo csv /nh | ConvertFrom-Csv -Header Name,Description,State)
[ordered]@{
    sid=$identity.User.Value
    session=[Diagnostics.Process]::GetCurrentProcess().SessionId
    admin=$principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
    medium=(@($groups | Where-Object {$_.SID -eq 'S-1-16-8192'}).Count -eq 1)
    privileges=@($privileges | ForEach-Object {$_.Name})
    profile=$env:USERPROFILE
} | ConvertTo-Json -Compress
'@
$spec = New-Object HerdrSandbox.ProvisioningProcessSpec
$spec.Role = 'Standard-user token boundary probe'
$spec.FilePath = Join-Path $PSHOME 'powershell.exe'
$spec.Arguments = @('-NoLogo','-NoProfile','-NonInteractive','-WindowStyle','Hidden','-EncodedCommand',
    [Convert]::ToBase64String([Text.Encoding]::Unicode.GetBytes($child)))
$spec.WorkingDirectory = $env:USERPROFILE
$spec.TimeoutMilliseconds = 15000
$spec.SuccessExitCodes = [int[]]@(0)
$spec.AsStandardUser = $true
$result = [HerdrSandbox.ProvisioningProcess]::Run($spec)
if (-not $result.Succeeded) { throw ('Restricted child failed: exit=' + $result.ExitCode + ', stopped=' + $result.Stopped + ', timeout=' + $result.TimedOut + '; ' + $result.Output) }
$observed = $result.Output | ConvertFrom-Json
if ($observed.sid -ne $identity.User.Value -or $observed.session -ne $session -or $observed.admin -or
    -not $observed.medium -or $observed.profile -ine $env:USERPROFILE -or
    @($observed.privileges | Where-Object {$_ -ne 'SeChangeNotifyPrivilege'}).Count -ne 0) {
    throw ('Restricted child did not satisfy its user/session/integrity/privilege contract: ' + $result.Output)
}
Write-Output 'Verified same user/session/profile, non-administrator, medium integrity, and restricted privileges.'
