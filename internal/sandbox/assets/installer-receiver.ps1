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
$profilePath = Join-Path $privateRoot 'profile.json'
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
try {
    $inputStream=[Console]::OpenStandardInput()
    $target=[IO.File]::Open($profilePath,[IO.FileMode]::CreateNew,[IO.FileAccess]::Write,[IO.FileShare]::None)
    try {
        $remaining=$profileLength;$buffer=New-Object byte[] 65536
        while($remaining -gt 0){$read=$inputStream.Read($buffer,0,[int][Math]::Min($remaining,$buffer.Length));if($read -le 0){throw 'Installer profile transport ended early.'};$target.Write($buffer,0,$read);$remaining-=$read}
        $target.Flush($true)
    } finally {$target.Dispose()}
    if((Get-FileHash -LiteralPath $profilePath -Algorithm SHA256).Hash -ine $profileDigest){throw 'Installer profile transport hash mismatch.'}
    $profile=[IO.File]::ReadAllText($profilePath)|ConvertFrom-Json
    $baseline=[IO.File]::ReadAllText((Join-Path $statusRoot 'installer-baseline.json'))|ConvertFrom-Json
    $current=Get-InstallerBaseline
    if($baseline.schemaVersion -ne 1 -or [bool]$baseline.winget -ne $current.winget -or [bool]$baseline.powershell7 -ne $current.powershell7 -or [bool]$baseline.vcRuntime -ne $current.vcRuntime){throw 'Transport bootstrap changed tested development prerequisites; installer acceptance refused.'}
    $source=Join-Path $bootstrapRoot 'installer\setup.exe'
    $item=Get-Item -LiteralPath $source -Force
    if($item.PSIsContainer -or ($item.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0){throw 'Installer input is unsafe.'}
    [IO.File]::Copy($source,$installerPath,$false)
    if((Get-FileHash -LiteralPath $installerPath -Algorithm SHA256).Hash -ine $installerDigest){throw 'Installer snapshot hash mismatch.'}
    $progress=[ordered]@{schemaVersion=1;phase='installer-running';message='Running the hash-bound workstation installer; no normal provisioning will repair its result.'}
    Publish-InstallerProgress $progress
    $spec=New-Object HerdrSandbox.ProvisioningProcessSpec
    $spec.Role='Workstation installer acceptance'
    $spec.FilePath=$installerPath
    $spec.WorkingDirectory=$privateRoot
    $spec.Arguments=@('/S',('/PROFILE='+$profilePath),('/ROOT='+$stateRoot))
    $spec.TimeoutMilliseconds=172800000
    $spec.SuccessExitCodes=[int[]]@(0,1,2)
    $result=[HerdrSandbox.ProvisioningProcess]::Run($spec)
    if($result.Stopped){throw [OperationCanceledException]::new('Installer acceptance cancelled.')}
    if($result.TimedOut){throw 'Installer acceptance exceeded its terminal deadline.'}
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
    Write-Output ([ordered]@{schemaVersion=1;installerSHA256=$installerDigest;profileSHA256=$profileDigest;exitCode=$result.ExitCode;complete=$complete;passed=$passed;checks=$probes.checks;wingetVersion=$probes.wingetVersion;herdrVersion=$probes.herdrVersion;herdrClient=$probes.herdrClient;reportPath=(Join-Path $stateRoot 'report.html')}|ConvertTo-Json -Depth 7 -Compress)
} finally {
    Remove-GuestArchiveStaging
}
