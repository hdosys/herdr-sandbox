$env:PSModulePath = $PSHOME + '\Modules'
Import-Module Microsoft.PowerShell.Utility -ErrorAction Stop
$processOwner = Join-Path $bootstrapRoot 'provisioning\provisioning-process.cs'
if ($null -eq ('HerdrSandbox.ProvisioningProcess' -as [type])) { Add-Type -Path $processOwner }
if ([HerdrSandbox.ProvisioningProcess]::ContractVersion -ne 3) { throw 'Invalid process owner contract.' }
Add-Type -Path (Join-Path $bootstrapRoot 'installer\workstation-root.cs')
. (Join-Path $bootstrapRoot 'installer\probes.ps1')
$privateRoot = Join-Path ([Environment]::GetFolderPath('CommonApplicationData')) ('herdr-installer-input\' + $runID)
[HerdrWorkstation.Directories]::Ensure($privateRoot)
$stateRoot = Join-Path ([Environment]::GetFolderPath('CommonApplicationData')) ('herdr-installer-state\' + $runID)
$profilePath = Join-Path $stateRoot 'selection.json'
$installerPath = Join-Path $privateRoot 'setup.exe'
function Publish-InstallerProgress {
    param($Value)
    $path=Join-Path $statusRoot 'progress.json'
    $temporary=$path+'.'+[Guid]::NewGuid().ToString('N')+'.tmp'
    try {
        [IO.File]::WriteAllText($temporary,($Value|ConvertTo-Json -Compress),(New-Object Text.UTF8Encoding($false)))
        if([IO.File]::Exists($path)){[IO.File]::Replace($temporary,$path,[NullString]::Value)}else{[IO.File]::Move($temporary,$path)}
    } finally {if([IO.File]::Exists($temporary)){[IO.File]::Delete($temporary)}}
}
$ownedDataPath=$null
try {
    $inputStream=[Console]::OpenStandardInput()
    if($profileLength -ne 64){throw 'Invalid embedded-profile identity frame.'}
    $buffer=New-Object byte[] 64
    $offset=0
    while($offset -lt 64){$read=$inputStream.Read($buffer,$offset,64-$offset);if($read -le 0){throw 'Installer identity transport ended early.'};$offset+=$read}
    if([Text.Encoding]::ASCII.GetString($buffer) -cne $profileDigest){throw 'Installer profile identity mismatch.'}
    $baseline=[IO.File]::ReadAllText((Join-Path $statusRoot 'installer-baseline.json'))|ConvertFrom-Json
    $current=Get-InstallerBaseline
    if($baseline.schemaVersion -ne 1 -or [bool]$baseline.winget -ne $current.winget -or [bool]$baseline.powershell7 -ne $current.powershell7 -or [bool]$baseline.vcRuntime -ne $current.vcRuntime){throw 'Transport bootstrap changed tested development prerequisites; installer acceptance refused.'}
    $source=Join-Path $bootstrapRoot 'installer\setup.exe'
    $item=Get-Item -LiteralPath $source -Force
    if($item.PSIsContainer -or ($item.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0){throw 'Installer input is unsafe.'}
    [IO.File]::Copy($source,$installerPath,$false)
    if((Get-FileHash -LiteralPath $installerPath -Algorithm SHA256).Hash -ine $installerDigest){throw 'Installer snapshot hash mismatch.'}
    $dataSource=Join-Path $bootstrapRoot 'installer\herdr-workstation_setup.data.zip'
    $dataInfo=Get-Item -LiteralPath $dataSource -Force -ErrorAction Stop
    if($dataInfo.PSIsContainer -or $dataInfo.Length -le 0 -or $dataInfo.Length -gt 68719476736 -or ($dataInfo.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0){throw 'Materialized installer data is missing or unsafe.'}
    [IO.File]::Copy($dataSource,(Join-Path $privateRoot 'herdr-workstation_setup.data.zip'),$false)
    $ownedDataPath=Join-Path $privateRoot 'herdr-workstation_setup.data.zip'
    $progress=[ordered]@{schemaVersion=1;phase='installer-running';message='Running the hash-bound workstation installer; no normal provisioning will repair its result.'}
    Publish-InstallerProgress $progress
    [HerdrWorkstation.Directories]::Ensure($stateRoot)
    $identity=[Security.Principal.WindowsIdentity]::GetCurrent()
    $sessions=@(Get-Process -Name explorer -IncludeUserName -ErrorAction Stop |
        Where-Object { $_.UserName -ieq $identity.Name -and $_.SessionId -gt 0 } |
        Select-Object -ExpandProperty SessionId -Unique)
    if($sessions.Count -ne 1){throw 'Expected one interactive desktop for the installer user.'}
    $session=[int]$sessions[0]
    $owner=[Diagnostics.Process]::GetCurrentProcess()
    $taskName='HerdrWorkstationInstaller-'+$runID
    $scheduler=New-Object -ComObject Schedule.Service
    $scheduler.Connect()
    $taskRoot=$scheduler.GetFolder('\')
    $definition=$scheduler.NewTask(0)
    $definition.Settings.Enabled=$true
    $definition.Settings.Hidden=$true
    $definition.Settings.AllowDemandStart=$true
    $definition.Settings.ExecutionTimeLimit='PT48H'
    $definition.Settings.DisallowStartIfOnBatteries=$false
    $definition.Settings.StopIfGoingOnBatteries=$false
    $definition.Principal.UserId=$identity.User.Value
    $definition.Principal.LogonType=3
    $definition.Principal.RunLevel=1
    $action=$definition.Actions.Create(0)
    $action.Path=Join-Path $PSHOME 'powershell.exe'
    $action.WorkingDirectory=$privateRoot
    $action.Arguments=('-NoLogo -NoProfile -NonInteractive -WindowStyle Hidden -ExecutionPolicy Bypass -File "{0}" -BootstrapRoot "{1}" -InstallerPath "{2}" -InstallerDigest {3} -StateRoot "{4}" -TaskName {5} -OwnerProcessId {6} -OwnerStartTicks {7} -ExpectedSession {8} -ExpectedUser {9}' -f
        (Join-Path $bootstrapRoot 'installer\interactive.ps1'),$bootstrapRoot,$installerPath,$installerDigest,$stateRoot,$taskName,$PID,$owner.StartTime.ToUniversalTime().Ticks,$session,$identity.User.Value)
    # Create-only registration never adopts another task. The worker deletes its
    # own registration on entry and leases the exact receiver process thereafter.
    $task=$taskRoot.RegisterTaskDefinition($taskName,$definition,2,$null,$null,3,$null)
    try {
        $null=$task.RunEx($null,4,$session,$null)
        $clock=[Diagnostics.Stopwatch]::StartNew()
        $resultPath=Join-Path $privateRoot 'interactive.json'
        $started=$false
        while($true){
            if(Test-Path -LiteralPath $resultPath -PathType Leaf){
                $item=Get-Item -LiteralPath $resultPath -Force
                if($item.Length -gt 16384 -or ($item.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0){throw 'Interactive installer result is unsafe.'}
                $result=[IO.File]::ReadAllText($resultPath)|ConvertFrom-Json
                if($result.state -eq 'failed'){throw ('Interactive installer failed: '+$result.message)}
                if($result.session -ne $session){throw 'Interactive installer result session mismatch.'}
                if($result.state -eq 'finished'){break}
                if($result.state -ne 'running'){throw 'Invalid interactive installer state.'}
                $started=$true
            }
            if(-not $started -and $clock.Elapsed.TotalSeconds -gt 30){throw 'Interactive installer did not start within 30 seconds.'}
            if($clock.Elapsed.TotalHours -ge 48){throw 'Interactive installer exceeded its terminal deadline.'}
            Start-Sleep -Milliseconds 500
        }
    } finally {
        try { $taskRoot.DeleteTask($taskName,0) }
        catch { if(($_.Exception.GetBaseException().HResult -band 0xFFFF) -ne 2){throw} }
    }
    if($result.Stopped){throw [OperationCanceledException]::new('Installer acceptance cancelled.')}
    if($result.TimedOut){throw 'Installer acceptance exceeded its terminal deadline.'}
    $profileItem=Get-Item -LiteralPath $profilePath -Force
    if($profileItem.PSIsContainer -or $profileItem.Length -gt 50331648 -or ($profileItem.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0){throw 'Retained embedded profile is unsafe.'}
    if((Get-FileHash -LiteralPath $profilePath -Algorithm SHA256).Hash -ine $profileDigest){throw 'Installed embedded profile differs from candidate identity.'}
    $profile=[IO.File]::ReadAllText($profilePath)|ConvertFrom-Json
    $reportPath=Join-Path $stateRoot 'report.json'
    if(-not(Test-Path -LiteralPath $reportPath -PathType Leaf)){throw 'Installer did not publish a report; no normal provisioning was run.'}
    $reportItem=Get-Item -LiteralPath $reportPath -Force
    if($reportItem.Length -gt 2097152 -or ($reportItem.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0){throw 'Installer report is unsafe or oversized.'}
    $report=[IO.File]::ReadAllText($reportPath)|ConvertFrom-Json
    $progress.phase='installer-verification';$progress.message='Independently checking installed commands, compiler/runtime behavior and configuration hashes.'
    Publish-InstallerProgress $progress
    $probes=Test-InstallerEffects -Profile $profile -Report $report -ExpectedProfileHash $profileDigest -ProbeRoot $transferRoot -StateRoot $stateRoot
    $complete=[bool]$report.complete -and @($report.steps).Count -gt 0 -and @($report.steps|Where-Object{$_.status -notin @('succeeded','retained')}).Count -eq 0
    $passed=$complete -and $result.ExitCode -eq 0 -and @($probes.checks|Where-Object{-not $_.passed}).Count -eq 0
    if ($passed) {
        # Only Sandbox transport is finalized here. The workstation installer
        # never configures SSH, and a failed installation leaves its shell alone.
        & {
            . (Join-Path $bootstrapRoot 'provisioning\base.ps1') -Phase Library -ProcessOwnerPath $processOwner
            Set-ProvisioningOpenSSHDefaultShell -Executable (Join-Path $env:WINDIR 'System32\cmd.exe')
        }
    }
    $probes.checks += [pscustomobject]@{name='interactive SSH shell';passed=$passed}
    Write-Output ([ordered]@{schemaVersion=1;installerSHA256=$installerDigest;profileSHA256=$profileDigest;exitCode=$result.ExitCode;complete=$complete;passed=$passed;checks=$probes.checks;wingetVersion=$probes.wingetVersion;herdrVersion=$probes.herdrVersion;herdrClient=$probes.herdrClient;reportPath=(Join-Path $stateRoot 'report.html')}|ConvertTo-Json -Depth 7 -Compress)
} catch {
    $safeFailure = 'Installer receiver failed: ' + $_.Exception.GetBaseException().GetType().FullName + ' line=' + $_.InvocationInfo.ScriptLineNumber
    Publish-InstallerProgress ([ordered]@{schemaVersion=1;phase='installer-failed';message=$safeFailure})
    throw [InvalidOperationException]::new($safeFailure)
} finally {
    # Only this receiver's private test copy is disposable. The user's source
    # installer/data, reports, resume state and installed tools remain intact.
    if($null -ne $ownedDataPath -and (Test-Path -LiteralPath $ownedDataPath -PathType Leaf)) {
        Remove-Item -LiteralPath $ownedDataPath -Force
    }
    Remove-GuestArchiveStaging
}
