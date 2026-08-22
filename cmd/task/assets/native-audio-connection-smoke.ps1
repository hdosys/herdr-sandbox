param(
    [Parameter(Mandatory = $true)]
    [string]$ReaperScriptPath
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version 2.0

function ConvertTo-AudioSmokePowerShellLiteral {
    param([Parameter(Mandatory = $true)][string]$Value)

    return "'" + $Value.Replace("'", "''") + "'"
}

function Register-AudioSmokeTask {
    param(
        [Parameter(Mandatory = $true)][string]$Name,
        [Parameter(Mandatory = $true)][string]$Description,
        [Parameter(Mandatory = $true)][string]$Executable,
        [Parameter(Mandatory = $true)][AllowEmptyString()][string]$Arguments,
        [Parameter(Mandatory = $true)][string]$WorkingDirectory,
        [Parameter(Mandatory = $true)][string]$ExecutionTimeLimit
    )

    try {
        $null = $script:TaskRoot.GetTask("\$Name")
        throw "Native audio smoke task already exists: $Name"
    } catch {
        if ($_.Exception.Message -notmatch 'cannot find the file specified|0x80070002') {
            throw
        }
    }
    $definition = $script:TaskService.NewTask(0)
    $definition.RegistrationInfo.Description = $Description
    $definition.Settings.Enabled = $true
    $definition.Settings.Hidden = $true
    $definition.Settings.AllowDemandStart = $true
    $definition.Settings.ExecutionTimeLimit = $ExecutionTimeLimit
    $definition.Principal.UserId = [Security.Principal.WindowsIdentity]::GetCurrent().Name
    $definition.Principal.LogonType = 3
    $definition.Principal.RunLevel = 1
    $action = $definition.Actions.Create(0)
    $action.Path = $Executable
    $action.Arguments = $Arguments
    $action.WorkingDirectory = $WorkingDirectory
    return $script:TaskRoot.RegisterTaskDefinition($Name, $definition, 2, $null, $null, 3, $null)
}

function Wait-AudioSmokeFile {
    param(
        [Parameter(Mandatory = $true)][string]$Path,
        [Parameter(Mandatory = $true)][int]$TimeoutSeconds,
        [Parameter(Mandatory = $true)][string]$Description
    )

    $deadline = [DateTime]::UtcNow.AddSeconds($TimeoutSeconds)
    while (-not (Test-Path -LiteralPath $Path -PathType Leaf)) {
        if ([DateTime]::UtcNow -ge $deadline) {
            throw "$Description did not publish within $TimeoutSeconds seconds."
        }
        Start-Sleep -Milliseconds 100
    }
    $item = Get-Item -LiteralPath $Path -Force
    if (($item.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0 -or
        $item.Length -le 0 -or $item.Length -gt 65536) {
        throw "$Description is unsafe: $Path"
    }
    return [IO.File]::ReadAllText($Path)
}

function Get-AudioSmokeProcesses {
    param([Parameter(Mandatory = $true)][string]$Executable)

    return @(Get-CimInstance Win32_Process -ErrorAction Stop | Where-Object {
        -not [string]::IsNullOrWhiteSpace([string]$_.ExecutablePath) -and
        [IO.Path]::GetFullPath([string]$_.ExecutablePath) -ieq $Executable
    })
}

$serverExecutable = 'C:\HerdrSandbox\tools\AudioGridder\bin\AudioGridderServer.exe'
$reaperExecutable = 'C:\Program Files\REAPER (x64)\reaper.exe'
$powerShell = Join-Path $env:SystemRoot 'System32\WindowsPowerShell\v1.0\powershell.exe'
$configurationRoot = Join-Path $env:APPDATA 'AudioGridder'
$serverConfigurationPath = Join-Path $configurationRoot 'audiogridderserver.cfg'
$clientConfigurationPath = Join-Path $configurationRoot 'audiogridderplugin.cfg'
foreach ($file in @($serverExecutable, $reaperExecutable, $serverConfigurationPath,
        $clientConfigurationPath, $ReaperScriptPath, $powerShell)) {
    if (-not (Test-Path -LiteralPath $file -PathType Leaf)) {
        throw "Native audio smoke input is missing: $file"
    }
    $item = Get-Item -LiteralPath $file -Force
    if (($item.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {
        throw "Native audio smoke input is reparse-backed: $file"
    }
}
if ([string](Get-Item -LiteralPath $serverExecutable -Force).VersionInfo.FileVersion -cne '1.2.0' -or
    [string](Get-Item -LiteralPath $reaperExecutable -Force).VersionInfo.FileVersion -cne '7.79') {
    throw 'Native audio smoke executable versions are invalid.'
}

$serverConfiguration = [IO.File]::ReadAllText($serverConfigurationPath) | ConvertFrom-Json
$clientConfiguration = [IO.File]::ReadAllText($clientConfigurationPath) | ConvertFrom-Json
$vst2Folders = @($serverConfiguration.VST2Folders | ForEach-Object { [string]$_ })
$vst3Folders = @($serverConfiguration.VST3Folders | ForEach-Object { [string]$_ })
$servers = @($clientConfiguration.Servers | ForEach-Object { [string]$_ })
if ([int]$serverConfiguration.ID -ne 0 -or [string]$serverConfiguration.NAME -cne 'Herdr Sandbox' -or
    -not [bool]$serverConfiguration.VST -or -not [bool]$serverConfiguration.VST2 -or
    -not [bool]$serverConfiguration.VSTNoStandardFolders -or -not [bool]$serverConfiguration.Logger -or
    [bool]$serverConfiguration.CrashReporting -or [int]$serverConfiguration.SandboxMode -ne 1 -or
    $vst2Folders.Count -ne 1 -or $vst2Folders[0] -cne 'C:\Program Files\VstPlugins' -or
    $vst3Folders.Count -ne 1 -or $vst3Folders[0] -cne 'C:\Program Files\Common Files\VST3' -or
    $servers.Count -ne 1 -or $servers[0] -cne '127.0.0.1:0' -or
    [string]$clientConfiguration.LastServer -cne '127.0.0.1:0:::0:0:00000000-0000-0000-0000-000000000000') {
    throw 'Native audio smoke configuration is invalid.'
}
if (@(Get-AudioSmokeProcesses -Executable $serverExecutable).Count -ne 0 -or
    @(Get-AudioSmokeProcesses -Executable $reaperExecutable).Count -ne 0) {
    throw 'Close REAPER and AudioGridder Server before running the native audio connection smoke.'
}

$explorerSessions = @(Get-Process -Name explorer -ErrorAction SilentlyContinue |
    ForEach-Object { [int]$_.SessionId } | Sort-Object -Unique)
if ($explorerSessions.Count -ne 1) {
    throw "Interactive Explorer session is ambiguous: $($explorerSessions -join ', ')"
}
$interactiveSessionID = [int]$explorerSessions[0]

$buildRoot = 'C:\HerdrSandbox\build'
if (-not (Test-Path -LiteralPath $buildRoot -PathType Container)) {
    throw "Native audio smoke build root is missing: $buildRoot"
}
$buildRootInfo = Get-Item -LiteralPath $buildRoot -Force
if (($buildRootInfo.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {
    throw "Native audio smoke build root is reparse-backed: $buildRoot"
}
$runID = [Guid]::NewGuid().ToString('N')
$temporaryRoot = Join-Path $buildRoot ('native-audio-connection-' + $runID)
$markerPath = Join-Path $temporaryRoot '.herdr-sandbox-native-audio-connection'
$reaperConfigurationPath = Join-Path $temporaryRoot 'reaper.ini'
$reaperLaunchPath = Join-Path $temporaryRoot 'reaper-launch.json'
$reaperResultPath = Join-Path $temporaryRoot 'reaper-result.json'
$reaperStatusPath = Join-Path $temporaryRoot 'reaper-status.txt'
New-Item -ItemType Directory -Path $temporaryRoot | Out-Null
[IO.File]::WriteAllText($markerPath, "herdr-sandbox native audio connection fixture v1`n",
    (New-Object Text.UTF8Encoding($false)))
$reaperConfiguration = @'
[REAPER]
splashscreen=0
splash=0
splash2=0
verchk=0
lastproject=
loadlastproj=0
vstfullstate=33605
vstpath=C:\Program Files\VstPlugins
vstpath64=C:\Program Files\Common Files\VST3;C:\Program Files\VstPlugins
[audioconfig]
mode=5
allow_sr_override=1
dummy_srate=48000
dummy_blocksize=512
'@
[IO.File]::WriteAllText($reaperConfigurationPath, $reaperConfiguration,
    (New-Object Text.UTF8Encoding($false)))

$taskNames = @(
    ('HerdrSandbox-AudioConnection-Server-' + $runID)
    ('HerdrSandbox-AudioConnection-REAPER-' + $runID)
    ('HerdrSandbox-AudioConnection-Dialog-' + $runID)
)
$serverMasterPID = 0
$serverPID = 0
$serverWorkerPIDs = @()
$reaperPID = 0
$serverTaskEnginePID = 0
$runError = $null
$cleanupError = $null
$script:TaskService = New-Object -ComObject Schedule.Service
$script:TaskService.Connect()
$script:TaskRoot = $script:TaskService.GetFolder('\')

try {
    $serverTask = Register-AudioSmokeTask -Name $taskNames[0] `
        -Description 'One-shot AudioGridder native connection smoke server' `
        -Executable $serverExecutable -Arguments '-id 0' `
        -WorkingDirectory (Split-Path -Parent $serverExecutable) -ExecutionTimeLimit 'PT6M'
    $runningServerTask = $serverTask.Run($null)
    $serverTaskEnginePID = [int]$runningServerTask.EnginePID

    $serverDeadline = [DateTime]::UtcNow.AddSeconds(120)
    do {
        Start-Sleep -Milliseconds 250
        $serverProcesses = Get-AudioSmokeProcesses -Executable $serverExecutable
        $master = @($serverProcesses | Where-Object {
            [int]$_.SessionId -eq $interactiveSessionID -and
            [string]$_.CommandLine -match '(?i)(^|\s)-id\s+0(\s|$)' -and
            [string]$_.CommandLine -notmatch '(?i)(^|\s)-server(\s|$)|--sandbox:'
        })
        $server = @($serverProcesses | Where-Object {
            [int]$_.SessionId -eq $interactiveSessionID -and
            [string]$_.CommandLine -match '(?i)(^|\s)-server(\s|$)' -and
            [string]$_.CommandLine -match '(?i)(^|\s)-id\s+0(\s|$)'
        })
        $listener = @(Get-NetTCPConnection -State Listen -LocalPort 55056 -ErrorAction SilentlyContinue)
        if ($master.Count -eq 1 -and $server.Count -eq 1 -and $listener.Count -eq 1 -and
            [int]$server[0].ParentProcessId -eq [int]$master[0].ProcessId -and
            [int]$listener[0].OwningProcess -eq [int]$server[0].ProcessId) {
            $serverMasterPID = [int]$master[0].ProcessId
            $serverPID = [int]$server[0].ProcessId
            break
        }
        if ([DateTime]::UtcNow -ge $serverDeadline) {
            throw 'AudioGridder Server did not establish its server-0 listener within 120 seconds.'
        }
    } while ($true)

    $reaperLiteral = ConvertTo-AudioSmokePowerShellLiteral -Value $reaperExecutable
    $reaperDirectoryLiteral = ConvertTo-AudioSmokePowerShellLiteral -Value (Split-Path -Parent $reaperExecutable)
    $reaperConfigurationLiteral = ConvertTo-AudioSmokePowerShellLiteral -Value $reaperConfigurationPath
    $reaperScriptLiteral = ConvertTo-AudioSmokePowerShellLiteral -Value $ReaperScriptPath
    $reaperStatusLiteral = ConvertTo-AudioSmokePowerShellLiteral -Value $reaperStatusPath
    $reaperLaunchLiteral = ConvertTo-AudioSmokePowerShellLiteral -Value $reaperLaunchPath
    $reaperLaunchPayload = @"
`$ErrorActionPreference = 'Stop'
`$env:HERDR_SANDBOX_AUDIO_SMOKE_STATUS = $reaperStatusLiteral
`$arguments = '-cfgfile "' + $reaperConfigurationLiteral + '" "' + $reaperScriptLiteral + '"'
`$process = Start-Process -FilePath $reaperLiteral -ArgumentList `$arguments -WorkingDirectory $reaperDirectoryLiteral -WindowStyle Minimized -PassThru
`$state = [ordered]@{ schemaVersion = 1; pid = [int]`$process.Id; sessionId = [int]`$process.SessionId }
[IO.File]::WriteAllText($reaperLaunchLiteral, (`$state | ConvertTo-Json -Compress), (New-Object Text.UTF8Encoding(`$false)))
"@
    $reaperLaunchEncoded = [Convert]::ToBase64String([Text.Encoding]::Unicode.GetBytes($reaperLaunchPayload))
    $reaperTask = Register-AudioSmokeTask -Name $taskNames[1] `
        -Description 'One-shot REAPER native AudioGridder connection smoke launch' `
        -Executable $powerShell `
        -Arguments "-NoLogo -NoProfile -NonInteractive -WindowStyle Hidden -EncodedCommand $reaperLaunchEncoded" `
        -WorkingDirectory (Split-Path -Parent $reaperExecutable) -ExecutionTimeLimit 'PT1M'
    $null = $reaperTask.Run($null)

    $reaperLaunch = (Wait-AudioSmokeFile -Path $reaperLaunchPath -TimeoutSeconds 15 `
            -Description 'REAPER active-session launch status') | ConvertFrom-Json
    $reaperPID = [int]$reaperLaunch.pid
    if ([int]$reaperLaunch.schemaVersion -ne 1 -or $reaperPID -lt 1 -or
        [int]$reaperLaunch.sessionId -ne $interactiveSessionID) {
        throw 'REAPER active-session launch status is invalid.'
    }
    $reaperProcess = @(Get-AudioSmokeProcesses -Executable $reaperExecutable | Where-Object {
        [int]$_.ProcessId -eq $reaperPID -and [int]$_.SessionId -eq $interactiveSessionID -and
        [string]$_.CommandLine -like "*$reaperConfigurationPath*"
    })
    if ($reaperProcess.Count -ne 1) {
        throw 'REAPER active-session process identity is invalid.'
    }

    $dialogResultLiteral = ConvertTo-AudioSmokePowerShellLiteral -Value $reaperResultPath
    $dialogStatusLiteral = ConvertTo-AudioSmokePowerShellLiteral -Value $reaperStatusPath
    $dialogPayload = @"
`$ErrorActionPreference = 'Stop'
`$source = @'
using System;
using System.Collections.Generic;
using System.Runtime.InteropServices;
using System.Text;
public static class HerdrReaperDialog {
    private delegate bool EnumWindowsProc(IntPtr window, IntPtr parameter);
    [DllImport("user32.dll")] private static extern bool EnumWindows(EnumWindowsProc callback, IntPtr parameter);
    [DllImport("user32.dll")] private static extern bool EnumChildWindows(IntPtr parent, EnumWindowsProc callback, IntPtr parameter);
    [DllImport("user32.dll")] private static extern uint GetWindowThreadProcessId(IntPtr window, out uint processId);
    [DllImport("user32.dll", CharSet = CharSet.Unicode)] private static extern int GetWindowText(IntPtr window, StringBuilder text, int maximum);
    [DllImport("user32.dll", CharSet = CharSet.Unicode)] private static extern int GetClassName(IntPtr window, StringBuilder text, int maximum);
    [DllImport("user32.dll")] private static extern bool IsWindowVisible(IntPtr window);
    [DllImport("user32.dll")] private static extern bool IsWindowEnabled(IntPtr window);
    [DllImport("user32.dll")] private static extern int GetDlgCtrlID(IntPtr window);
    [DllImport("user32.dll")] private static extern IntPtr SendMessage(IntPtr window, uint message, IntPtr wParam, IntPtr lParam);
    [DllImport("user32.dll")] private static extern bool ShowWindowAsync(IntPtr window, int command);
    private static string Text(IntPtr window) { var value = new StringBuilder(4096); GetWindowText(window, value, value.Capacity); return value.ToString(); }
    private static string Class(IntPtr window) { var value = new StringBuilder(256); GetClassName(window, value, value.Capacity); return value.ToString(); }
    public static bool DismissAudioDevice(int expectedProcessId) {
        var matches = new List<IntPtr>();
        EnumWindows((dialog, ignored) => {
            uint processId;
            GetWindowThreadProcessId(dialog, out processId);
            if (processId != expectedProcessId || !IsWindowVisible(dialog) || Class(dialog) != "#32770" || Text(dialog) != "REAPER") return true;
            bool hasPrompt = false;
            var buttons = new List<IntPtr>();
            EnumChildWindows(dialog, (child, ignoredChild) => {
                var text = Text(child);
                var windowClass = Class(child);
                if (windowClass == "Static" && text == "You have not yet selected an audio device.\r\n\r\nWould you like to select your audio device driver now (recommended)?") hasPrompt = true;
                if (windowClass == "Button" && GetDlgCtrlID(child) == 7 && text == "&No" && IsWindowVisible(child) && IsWindowEnabled(child)) buttons.Add(child);
                return true;
            }, IntPtr.Zero);
            if (hasPrompt && buttons.Count == 1) matches.Add(buttons[0]);
            return true;
        }, IntPtr.Zero);
        if (matches.Count > 1) throw new InvalidOperationException("Audio-device dialog is ambiguous.");
        if (matches.Count == 0) return false;
        SendMessage(matches[0], 0x00F5, IntPtr.Zero, IntPtr.Zero);
        return true;
    }
    public static int DismissEvaluation(int expectedProcessId) {
        int dialogs = 0;
        var buttons = new List<IntPtr>();
        EnumWindows((dialog, ignored) => {
            uint processId;
            GetWindowThreadProcessId(dialog, out processId);
            if (processId != expectedProcessId || !IsWindowVisible(dialog) || Class(dialog) != "#32770" ||
                !Text(dialog).StartsWith("About REAPER v7.79/win64 rev ")) return true;
            bool hasNotice = false;
            var localButtons = new List<IntPtr>();
            EnumChildWindows(dialog, (child, ignoredChild) => {
                var text = Text(child);
                var windowClass = Class(child);
                if (windowClass == "Static" && text.StartsWith("REAPER IS NOT FREE.\r\n\r\nIt is a paid software product.")) hasNotice = true;
                if (windowClass == "Button" && GetDlgCtrlID(child) == 1 && text == "Still Evaluating" &&
                    IsWindowVisible(child) && IsWindowEnabled(child)) localButtons.Add(child);
                return true;
            }, IntPtr.Zero);
            if (hasNotice) {
                dialogs++;
                foreach (var button in localButtons) buttons.Add(button);
            }
            return true;
        }, IntPtr.Zero);
        if (dialogs > 1 || buttons.Count > 1) throw new InvalidOperationException("REAPER evaluation dialog is ambiguous.");
        if (dialogs == 0) return 0;
        if (buttons.Count == 0) return 1;
        SendMessage(buttons[0], 0x00F5, IntPtr.Zero, IntPtr.Zero);
        return 2;
    }
    public static bool MinimizeMain(int expectedProcessId) {
        var matches = new List<IntPtr>();
        EnumWindows((window, ignored) => {
            uint processId;
            GetWindowThreadProcessId(window, out processId);
            if (processId == expectedProcessId && IsWindowVisible(window) && Class(window) == "REAPERwnd") matches.Add(window);
            return true;
        }, IntPtr.Zero);
        if (matches.Count > 1) throw new InvalidOperationException("REAPER main window is ambiguous.");
        return matches.Count == 1 && ShowWindowAsync(matches[0], 6);
    }
}
'@
`$result = [ordered]@{ schemaVersion = 1; state = 'failed'; audioDeviceDismissed = `$false; evaluationDismissed = `$false; status = ''; message = '' }
try {
    Add-Type -TypeDefinition `$source
    `$deadline = [DateTime]::UtcNow.AddSeconds(90)
    do {
        if (Test-Path -LiteralPath $dialogStatusLiteral -PathType Leaf) {
            `$result.status = [IO.File]::ReadAllText($dialogStatusLiteral)
            break
        }
        `$process = Get-Process -Id $reaperPID -ErrorAction SilentlyContinue
        if (`$null -eq `$process -or [int]`$process.SessionId -ne $interactiveSessionID) {
            throw 'REAPER exited before AudioGridder insertion completed.'
        }
        if (-not `$result.audioDeviceDismissed -and [HerdrReaperDialog]::DismissAudioDevice($reaperPID)) {
            `$result.audioDeviceDismissed = `$true
        }
        if ([DateTime]::UtcNow -ge `$deadline) {
            throw 'REAPER did not publish AudioGridder insertion status within 90 seconds.'
        }
        Start-Sleep -Milliseconds 100
    } while (`$true)
    if ([string]`$result.status -notlike 'FX_INSERTED idx=*') {
        throw "REAPER AudioGridder insertion failed: `$([string]`$result.status)"
    }
    `$evaluationDeadline = [DateTime]::UtcNow.AddSeconds(15)
    `$evaluationSeen = `$false
    do {
        `$evaluationState = [HerdrReaperDialog]::DismissEvaluation($reaperPID)
        if (`$evaluationState -eq 2) {
            `$result.evaluationDismissed = `$true
            break
        }
        if (`$evaluationState -eq 1) { `$evaluationSeen = `$true }
        if ([DateTime]::UtcNow -ge `$evaluationDeadline) { break }
        Start-Sleep -Milliseconds 100
    } while (`$true)
    if (`$evaluationSeen -and -not `$result.evaluationDismissed) {
        throw 'REAPER evaluation action did not become available within 15 seconds.'
    }
    `$null = [HerdrReaperDialog]::MinimizeMain($reaperPID)
    `$result.state = 'passed'
} catch {
    `$result.message = `$_.Exception.Message
}
[IO.File]::WriteAllText($dialogResultLiteral, (`$result | ConvertTo-Json -Compress), (New-Object Text.UTF8Encoding(`$false)))
"@
    $dialogEncoded = [Convert]::ToBase64String([Text.Encoding]::Unicode.GetBytes($dialogPayload))
    $dialogTask = Register-AudioSmokeTask -Name $taskNames[2] `
        -Description 'One-shot REAPER audio-device dialog acceptance for native AudioGridder smoke' `
        -Executable $powerShell `
        -Arguments "-NoLogo -NoProfile -NonInteractive -WindowStyle Hidden -EncodedCommand $dialogEncoded" `
        -WorkingDirectory $temporaryRoot -ExecutionTimeLimit 'PT2M'
    $null = $dialogTask.Run($null)

    $reaperResult = (Wait-AudioSmokeFile -Path $reaperResultPath -TimeoutSeconds 105 `
            -Description 'REAPER AudioGridder insertion result') | ConvertFrom-Json
    if ([int]$reaperResult.schemaVersion -ne 1 -or [string]$reaperResult.state -cne 'passed' -or
        [string]$reaperResult.status -notlike 'FX_INSERTED idx=*') {
        throw "REAPER AudioGridder insertion failed: $([string]$reaperResult.message)"
    }

    $connectionDeadline = [DateTime]::UtcNow.AddSeconds(60)
    do {
        Start-Sleep -Milliseconds 250
        $connections = @(Get-NetTCPConnection -State Established -ErrorAction SilentlyContinue | Where-Object {
            [int]$_.OwningProcess -eq $reaperPID -and [string]$_.RemoteAddress -eq '127.0.0.1' -and
            [int]$_.RemotePort -ge 55088 -and [int]$_.RemotePort -le 56088
        })
        $serverWorkers = @(Get-AudioSmokeProcesses -Executable $serverExecutable | Where-Object {
            [int]$_.ParentProcessId -eq $serverPID -and [int]$_.SessionId -eq $interactiveSessionID -and
            [string]$_.CommandLine -match '--sandbox:' -and [string]$_.CommandLine -match '(?i)(^|\s)-id\s+0(\s|$)'
        })
        $workerConnections = @()
        $workerPorts = @()
        if ($serverWorkers.Count -eq 1) {
            $workerConnections = @(Get-NetTCPConnection -State Established -ErrorAction SilentlyContinue | Where-Object {
                [int]$_.OwningProcess -eq [int]$serverWorkers[0].ProcessId -and
                [string]$_.LocalAddress -eq '127.0.0.1' -and
                [int]$_.LocalPort -ge 55088 -and [int]$_.LocalPort -le 56088 -and
                [string]$_.RemoteAddress -eq '127.0.0.1' -and [int]$_.RemotePort -gt 0
            })
            $workerPorts = @($workerConnections | ForEach-Object { [int]$_.LocalPort } | Sort-Object -Unique)
        }
        if ($connections.Count -gt 0 -and $serverWorkers.Count -eq 1 -and
            $workerConnections.Count -eq $connections.Count -and $workerPorts.Count -eq 1 -and
            @($connections | Where-Object { [int]$_.RemotePort -ne [int]$workerPorts[0] }).Count -eq 0) {
            $serverWorkerPIDs = @([int]$serverWorkers[0].ProcessId)
            break
        }
        if ([DateTime]::UtcNow -ge $connectionDeadline) {
            throw 'REAPER did not establish an AudioGridder worker connection within 60 seconds.'
        }
    } while ($true)

    [Console]::Out.WriteLine((
            '[audio-connection] REAPER 7.79 inserted AGridder; server 0 created worker port {0} with {1} established loopback stream(s).' -f
            [int]$workerPorts[0], $connections.Count))
} catch {
    $runError = $_
} finally {
    try {
        if ($reaperPID -gt 0) {
            $ownedREAPER = @(Get-AudioSmokeProcesses -Executable $reaperExecutable | Where-Object {
                [int]$_.ProcessId -eq $reaperPID -and [int]$_.SessionId -eq $interactiveSessionID -and
                [string]$_.CommandLine -like "*$reaperConfigurationPath*"
            })
            if ($ownedREAPER.Count -eq 1) {
                Stop-Process -Id $reaperPID -Force -ErrorAction Stop
                Wait-Process -Id $reaperPID -Timeout 10 -ErrorAction SilentlyContinue
            } elseif ((Get-Process -Id $reaperPID -ErrorAction SilentlyContinue) -ne $null) {
                throw 'Refuse to stop changed REAPER process identity during native audio smoke cleanup.'
            }
        }

        $ownedServerPIDs = @($serverWorkerPIDs + @($serverPID, $serverMasterPID) | Where-Object { $_ -gt 0 } | Select-Object -Unique)
        if ($serverMasterPID -eq 0 -and $serverTaskEnginePID -gt 0) {
            $candidateMasters = @(Get-AudioSmokeProcesses -Executable $serverExecutable | Where-Object {
                [int]$_.ParentProcessId -eq $serverTaskEnginePID -and [int]$_.SessionId -eq $interactiveSessionID -and
                [string]$_.CommandLine -match '(?i)(^|\s)-id\s+0(\s|$)' -and
                [string]$_.CommandLine -notmatch '(?i)(^|\s)-server(\s|$)|--sandbox:'
            })
            if ($candidateMasters.Count -eq 1) {
                $serverMasterPID = [int]$candidateMasters[0].ProcessId
                $ownedServerPIDs += $serverMasterPID
            }
        }
        if ($serverMasterPID -gt 0) {
            $allProcesses = @(Get-CimInstance Win32_Process -ErrorAction Stop)
            $frontier = @($serverMasterPID)
            while ($frontier.Count -gt 0) {
                $children = @($allProcesses | Where-Object { [int]$_.ParentProcessId -in $frontier })
                $newIDs = @($children | ForEach-Object { [int]$_.ProcessId } | Where-Object { $_ -notin $ownedServerPIDs })
                if ($newIDs.Count -eq 0) { break }
                $ownedServerPIDs += $newIDs
                $frontier = $newIDs
            }
        }

        foreach ($taskName in $taskNames) {
            try {
                $task = $script:TaskRoot.GetTask("\$taskName")
                if ([int]$task.State -eq 4) { $task.Stop(0) }
            } catch {
                if ($_.Exception.Message -notmatch 'cannot find the file specified|0x80070002') {
                    throw
                }
            }
        }
        Start-Sleep -Milliseconds 250
        $ownedServerPIDs = @($ownedServerPIDs | Select-Object -Unique)
        $serverCleanupDeadline = [DateTime]::UtcNow.AddSeconds(10)
        do {
            $currentServerProcesses = Get-AudioSmokeProcesses -Executable $serverExecutable
            $newOwned = @($currentServerProcesses | Where-Object {
                [int]$_.ParentProcessId -in $ownedServerPIDs -and [int]$_.ProcessId -notin $ownedServerPIDs
            } | ForEach-Object { [int]$_.ProcessId })
            if ($newOwned.Count -gt 0) {
                $ownedServerPIDs = @($ownedServerPIDs + $newOwned | Select-Object -Unique)
            }
            $ownedProcesses = @($currentServerProcesses | Where-Object { [int]$_.ProcessId -in $ownedServerPIDs })
            if ($ownedProcesses.Count -eq 0) { break }
            $master = @($ownedProcesses | Where-Object { [int]$_.ProcessId -eq $serverMasterPID })
            $remaining = @($ownedProcesses | Where-Object { [int]$_.ProcessId -ne $serverMasterPID })
            foreach ($process in @($master + $remaining)) {
                if ([int]$process.SessionId -ne $interactiveSessionID -or
                    [string]$process.ExecutablePath -ine $serverExecutable) {
                    throw "Refuse to stop changed AudioGridder process identity: $([int]$process.ProcessId)"
                }
                Stop-Process -Id ([int]$process.ProcessId) -Force -ErrorAction Stop
            }
            if ([DateTime]::UtcNow -ge $serverCleanupDeadline) {
                throw 'Timed out cleaning the task-owned AudioGridder process tree.'
            }
            Start-Sleep -Milliseconds 100
        } while ($true)

        foreach ($taskName in $taskNames) {
            try {
                $script:TaskRoot.DeleteTask($taskName, 0)
            } catch {
                if ($_.Exception.Message -notmatch 'cannot find the file specified|0x80070002') {
                    throw
                }
            }
        }
        if (Test-Path -LiteralPath $temporaryRoot -PathType Container) {
            $marker = [IO.File]::ReadAllText($markerPath)
            $temporaryInfo = Get-Item -LiteralPath $temporaryRoot -Force
            $reparse = @(Get-ChildItem -LiteralPath $temporaryRoot -Recurse -Force | Where-Object {
                ($_.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0
            })
            if ($marker -cne "herdr-sandbox native audio connection fixture v1`n" -or
                ($temporaryInfo.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0 -or $reparse.Count -ne 0) {
                throw "Refuse unsafe native audio smoke cleanup root: $temporaryRoot"
            }
            Remove-Item -LiteralPath $temporaryRoot -Recurse -Force
        }
    } catch {
        $cleanupError = $_
    }
}

if ($null -ne $runError -and $null -ne $cleanupError) {
    throw "$($runError.Exception.Message) Cleanup also failed: $($cleanupError.Exception.Message)"
}
if ($null -ne $runError) { throw $runError }
if ($null -ne $cleanupError) { throw $cleanupError }
