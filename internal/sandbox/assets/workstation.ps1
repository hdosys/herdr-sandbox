param(
    [Parameter(Mandatory = $true)][string]$NativeProfilePath,
    [Parameter(Mandatory = $true)][string]$NativeRoot,
    [Parameter(Mandatory = $true)][string]$BasePath,
    [Parameter(Mandatory = $true)][string]$StacksPath,
    [Parameter(Mandatory = $true)][string]$RunnerPath,
    [switch]$Refresh,
    [switch]$PlanOnly
)
$ErrorActionPreference = 'Stop'
Set-StrictMode -Version 2.0
$env:PSModulePath = $PSHOME + '\Modules'
Import-Module Microsoft.PowerShell.Utility -ErrorAction Stop
[Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12
$principal = New-Object Security.Principal.WindowsPrincipal([Security.Principal.WindowsIdentity]::GetCurrent())
$nativeUserSID = [Security.Principal.WindowsIdentity]::GetCurrent().User.Value
if (-not $PlanOnly -and -not $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) { throw 'Direct setup requires elevation for the intended Windows user.' }
$nativeWindowsBuild = [string][Microsoft.Win32.Registry]::GetValue('HKEY_LOCAL_MACHINE\SOFTWARE\Microsoft\Windows NT\CurrentVersion', 'CurrentBuildNumber', '')
if (-not [Environment]::Is64BitOperatingSystem -or $nativeWindowsBuild -notmatch '^\d+$' -or [int]$nativeWindowsBuild -lt 19041) { throw 'Direct setup requires Windows x64 build 19041 or newer (Windows 11 recommended).' }
$env:HERDR_PROVISIONING_ROOT = $NativeRoot
$env:HERDR_PROVISIONING_TARGET = 'workstation'
$env:HERDR_SANDBOX_STATUS_DIRECTORY = $null
$nativeInputDirectory = Split-Path -Parent $NativeProfilePath
$nativeEntryScript = Join-Path $nativeInputDirectory 'workstation.ps1'
$nativeProcessOwner = Join-Path $PSScriptRoot 'provisioning-process.cs'
. $BasePath -Phase Library -ProcessOwnerPath $nativeProcessOwner
. $StacksPath
$nativeProfile = [IO.File]::ReadAllText($NativeProfilePath) | ConvertFrom-Json
if ($nativeProfile.schemaVersion -ne 1) { throw 'Unsupported workstation profile.' }
$global:HerdrSandboxToolVersionPlan = [pscustomobject]@{ Versions = @{}; Series = @{}; Sources = @{}; Owners = @{} }
foreach ($tool in @($nativeProfile.tools)) {
    $global:HerdrSandboxToolVersionPlan.Versions[[string]$tool.tool] = [string]$tool.version
    $global:HerdrSandboxToolVersionPlan.Series[[string]$tool.tool] = [string]$tool.series
    $global:HerdrSandboxToolVersionPlan.Sources[[string]$tool.tool] = [string]$tool.source
}
$OptionalFailures = New-Object 'Collections.Generic.List[string]'
$provisioningPackagePlan = [pscustomobject]@{ Enabled = @{}; Versions = @{} }
foreach ($package in @($nativeProfile.packages)) {
    $provisioningPackagePlan.Enabled[[string]$package.id] = $true
    $provisioningPackagePlan.Versions[[string]$package.id] = [string]$package.version
}
$nativeFailures = New-Object 'Collections.Generic.List[string]'
$nativeResults = [ordered]@{}
if (-not $PlanOnly) {
    foreach ($directory in @('cache', 'staging', 'tools', 'state', 'build', 'toolchains')) {
        $destination = Get-ProvisioningPath $directory
        if (Test-Path -LiteralPath $destination) {
            $item = Get-Item -LiteralPath $destination -Force
            if (-not $item.PSIsContainer -or ($item.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) { throw "Unsafe setup directory: $destination" }
        } else { New-Item -ItemType Directory -Path $destination | Out-Null }
    }
}
$nativeState = Get-ProvisioningPath 'state\steps.json'
try {
    Initialize-ProvisioningSteps -Path $nativeState -Resume (-not $Refresh.IsPresent) -ImplementationFiles @($BasePath, $StacksPath, $nativeEntryScript, (Join-Path $nativeInputDirectory 'workstation-action.ps1'), $nativeProcessOwner)
} catch {
    # Invalid historical evidence never means success. Keep it untouched and
    # attempt the explicitly selected work without pretending it can be resumed.
    $nativeResults['Previous completion evidence'] = [ordered]@{ status = 'failed'; message = 'Previous completion evidence is unavailable. It was preserved; this run cannot persist resumable completion.' }
    $nativeFailures.Add('Previous completion evidence is unavailable.')
    Initialize-ProvisioningSteps -Path '' -Resume $false -ImplementationFiles @($BasePath, $StacksPath, $nativeEntryScript, (Join-Path $nativeInputDirectory 'workstation-action.ps1'), $nativeProcessOwner)
}
$global:HerdrSandboxStepState.StackImplementation = Get-ProvisioningStepFingerprint -Files @($StacksPath)

function Save-WorkstationReport {
    $steps = @($nativeResults.Keys | ForEach-Object {
        [ordered]@{ name = $_; status = $nativeResults[$_].status; message = $nativeResults[$_].message }
    })
    $reportPath = Get-ProvisioningPath 'report.json'
    $temporary = $reportPath + '.' + [Guid]::NewGuid().ToString('N') + '.tmp'
    try {
        [IO.File]::WriteAllText($temporary, ([ordered]@{ schemaVersion = 1; steps = $steps } | ConvertTo-Json -Depth 5), (New-Object Text.UTF8Encoding($false)))
        if (Test-Path -LiteralPath $reportPath) { [IO.File]::Replace($temporary, $reportPath, [NullString]::Value) }
        else { [IO.File]::Move($temporary, $reportPath) }
    } catch {
        $message = 'Report publication failed: ' + $_.Exception.Message
        if (-not $nativeFailures.Contains($message)) { $nativeFailures.Add($message) }
        Write-Warning $message
    } finally {
        if (Test-Path -LiteralPath $temporary) {
            try { Remove-Item -LiteralPath $temporary -Force }
            catch {
                $message = 'Temporary report cleanup failed: ' + $_.Exception.Message
                if (-not $nativeFailures.Contains($message)) { $nativeFailures.Add($message) }
                Write-Warning $message
            }
        }
    }
}

$nativeJobs = New-Object 'Collections.Generic.List[object]'
function Add-WorkstationJob {
    param([string]$Name, [string]$Action, [string[]]$Requires = @(), [string[]]$Tools = @(), [string]$PackageID = '', [string]$Version = '', [int]$Seconds = 900)
    $nativeJobs.Add([pscustomobject]@{ Name=$Name; Action=$Action; Requires=$Requires; Tools=$Tools; PackageID=$PackageID; Version=$Version; Seconds=$Seconds; Diagnostic='' })
    $nativeResults[$Name] = [ordered]@{ status='pending'; message='Not attempted yet.' }
}

function Get-WorkstationJobInputs {
    param($Job)
    $inputs = @($NativeRoot, $nativeUserSID, $Job.Action, $Job.PackageID, $Job.Version)
    foreach ($tool in $Job.Tools) {
        $inputs += @($tool, [string]$global:HerdrSandboxToolVersionPlan.Versions[$tool],
            [string]$global:HerdrSandboxToolVersionPlan.Series[$tool], [string]$global:HerdrSandboxToolVersionPlan.Sources[$tool])
    }
    foreach ($dependency in $Job.Requires) {
        $inputs += @($dependency, [string]$global:HerdrSandboxStepState.Steps[$dependency])
    }
    return $inputs
}

$selected = @{}
foreach ($stack in @($nativeProfile.stacks)) { $selected[[string]$stack] = $true }
if ($selected.ContainsKey('python-ai')) { $selected['python'] = $true; $selected['uv'] = $true }
if ($selected.ContainsKey('herdr')) { foreach ($stack in @('python','zig','rust-msvc','bun','cargo-nextest','just')) { $selected[$stack] = $true } }
if ($selected.ContainsKey('handy')) { foreach ($stack in @('rust-msvc','cpp','bun')) { $selected[$stack] = $true } }
if ($selected.ContainsKey('hyperframes')) { foreach ($stack in @('node-runtime','python','uv')) { $selected[$stack] = $true } }
foreach ($stack in @('node','playwright-cli','tradingview')) { if ($selected.ContainsKey($stack)) { $selected['node-runtime'] = $true } }
if ($selected.ContainsKey('android')) { $selected['java'] = $true }
if ($selected.ContainsKey('cargo-nextest')) { $selected['rust-msvc'] = $true }
$needsVisualStudio = $selected.ContainsKey('cpp') -or $selected.ContainsKey('rust-msvc')

    Add-WorkstationJob 'Windows desktop preferences' 'desktop' -Seconds 60
    Add-WorkstationJob 'WinGet' 'winget' -Seconds 600
    $vcRuntimeID = 'Microsoft.VCRedist.2015+.x64'
    $needsVCRuntime = $nativeProfile.stacks.Count -gt 0 -or $provisioningPackagePlan.Enabled.ContainsKey('hdosys.herdr-win') -or $provisioningPackagePlan.Enabled.ContainsKey($vcRuntimeID)
    if ($needsVCRuntime) {
        $vcVersion = [string]$provisioningPackagePlan.Versions[$vcRuntimeID]
        Add-WorkstationJob -Name ('Package ' + $vcRuntimeID) -Action package -Requires @('WinGet') -PackageID $vcRuntimeID -Version $vcVersion -Seconds 600
    }
    foreach ($package in @($nativeProfile.packages)) {
        if ([string]$package.id -ieq $vcRuntimeID) { continue }
        $packageRequirements = @('WinGet')
        if ([string]$package.id -ieq 'hdosys.herdr-win') { $packageRequirements += 'Package ' + $vcRuntimeID }
        Add-WorkstationJob -Name ('Package ' + [string]$package.id) -Action package -Requires $packageRequirements -PackageID ([string]$package.id) -Version ([string]$package.version) -Seconds 600
    }
    if ($provisioningPackagePlan.Enabled.ContainsKey('Microsoft.PowerShell')) {
        Add-WorkstationJob 'PowerShell profile' 'shell' -Requires @('Package Microsoft.PowerShell') -Seconds 60
    }
    if ($provisioningPackagePlan.Enabled.ContainsKey('Nushell.Nushell')) {
        $requirements = @('Package Nushell.Nushell')
        if ($provisioningPackagePlan.Enabled.ContainsKey('Starship.Starship')) { $requirements += 'Package Starship.Starship' }
        Add-WorkstationJob 'Nushell profile' 'nushell' -Requires $requirements -Seconds 90
    }
    if ($needsVisualStudio) {
        Add-WorkstationJob 'Visual Studio layout' 'vs-layout' -Requires @('WinGet') -Seconds 3660
        Add-WorkstationJob 'Visual Studio Build Tools' 'vs' -Requires @('Visual Studio layout') -Seconds 960
    }
    if ($selected.ContainsKey('node-runtime')) { Add-WorkstationJob 'Node.js' 'node-runtime' -Requires @('WinGet') -Tools @('OpenJS.NodeJS') }
    if ($selected.ContainsKey('java')) { Add-WorkstationJob 'Java' 'java' -Requires @('WinGet') -Tools @('Microsoft.OpenJDK') }
    if ($selected.ContainsKey('python')) { Add-WorkstationJob 'Python' 'python' -Requires @('WinGet') -Tools @('Python') }
    if ($selected.ContainsKey('uv')) { Add-WorkstationJob 'uv' 'uv' -Requires @('WinGet') -Tools @('astral-sh.uv') }
    if ($selected.ContainsKey('opensrc')) { Add-WorkstationJob 'OpenSrc' 'opensrc' }
    if ($selected.ContainsKey('font')) { Add-WorkstationJob 'GeistMono font' 'font' }
    $stackTools = [ordered]@{
        'go' = 'GoLang.Go'; 'dotnet' = 'Microsoft.DotNet.SDK'; 'nsis' = 'NSIS.NSIS';
        'zig' = 'zig.zig'; 'bun' = 'Oven-sh.Bun'; 'just' = 'Casey.Just'
    }
    foreach ($stack in $stackTools.Keys) {
        if ($selected.ContainsKey($stack)) {
            Add-WorkstationJob -Name ('Stack ' + $stack) -Action $stack -Requires @('WinGet') -Tools @($stackTools[$stack])
        }
    }
    if ($selected.ContainsKey('cpp')) { Add-WorkstationJob 'C and C++' 'cpp' -Requires @('WinGet','Visual Studio Build Tools') -Tools @('Kitware.CMake') }
    if ($selected.ContainsKey('rust-msvc')) { Add-WorkstationJob 'Rust MSVC' 'rust-msvc' -Requires @('WinGet','Visual Studio Build Tools') -Tools @('Rustlang.Rustup','rust-toolchain') -Seconds 1800 }
    if ($selected.ContainsKey('cargo-nextest')) { Add-WorkstationJob 'Cargo Nextest' 'cargo-nextest' -Requires @('WinGet','Rust MSVC') -Tools @('nextest.cargo-nextest') }
    if ($selected.ContainsKey('android')) { Add-WorkstationJob 'Android' 'android' -Requires @('WinGet','Java') -Tools @('Google.AndroidCommandLineTools','Google.PlatformTools') }
    if ($selected.ContainsKey('audio')) {
        Add-WorkstationJob 'REAPER' 'reaper' -Requires @('WinGet') -Tools @('Cockos.REAPER')
        Add-WorkstationJob 'AudioGridder' 'audiogridder' -Tools @('AudioGridder')
    }
    if ($selected.ContainsKey('node')) { Add-WorkstationJob 'Node browser tools' 'node' -Requires @('Node.js') -Tools @('playwright') }
    if ($selected.ContainsKey('playwright-cli')) { Add-WorkstationJob 'Playwright CLI' 'playwright-cli' -Requires @('Node.js') -Tools @('@playwright/cli') }
    if ($selected.ContainsKey('tradingview')) { Add-WorkstationJob 'TradingView' 'tradingview' -Requires @('Node.js') -Tools @('TradingView.TradingViewDesktop','@ferroxlabs/tvcontrol') }
    if ($selected.ContainsKey('hyperframes')) {
        Add-WorkstationJob 'HyperFrames rendering' 'hyperframes' -Requires @('Node.js','WinGet') -Tools @('hyperframes','Gyan.FFmpeg')
        Add-WorkstationJob 'TTS models' 'tts-models' -Seconds 1860
        Add-WorkstationJob 'HyperFrames TTS' 'tts' -Requires @('HyperFrames rendering','TTS models','Python','uv')
    }
    if ($selected.ContainsKey('git-sh')) {
        Add-WorkstationJob 'Git shell' 'git-sh' -Requires @('Package Git.Git') -Seconds 60
    }
    if ($selected.ContainsKey('handy')) {
        Add-WorkstationJob 'Vulkan SDK' 'vulkan-sdk' -Requires @('WinGet') -Tools @('KhronosGroup.VulkanSDK') -Seconds 1200
        Add-WorkstationJob 'WebView2' 'webview2' -Requires @('WinGet') -Tools @('Microsoft.EdgeWebView2Runtime')
        Add-WorkstationJob 'Handy native headers' 'handy-headers' -Requires @('Vulkan SDK','C and C++')
    }

if ($PlanOnly) {
    $seconds = 120 + $nativeJobs.Count * 30
    foreach ($job in $nativeJobs) { $seconds += $job.Seconds }
    $jobs = @($nativeJobs | ForEach-Object {
        [ordered]@{ name=$_.Name; seconds=$_.Seconds; requires=@($_.Requires); fingerprint=(Get-ProvisioningStepFingerprint -Values (@($global:HerdrSandboxStepState.Implementation) + @(Get-WorkstationJobInputs $_))) }
    })
    Write-Output ([ordered]@{ schemaVersion=1; seconds=$seconds; steps=$nativeJobs.Count; jobs=$jobs } | ConvertTo-Json -Depth 4 -Compress)
    exit 0
}
Save-WorkstationReport
try {
    $stepNumber = 0
    foreach ($job in $nativeJobs) {
        $stepNumber++
        $inputs = @(Get-WorkstationJobInputs $job)
        $nativeResults[$job.Name] = [ordered]@{ status='running'; message=('Started at ' + [DateTime]::UtcNow.ToString('o') + '; deadline ' + $job.Seconds + ' seconds.') }
        Save-WorkstationReport
        Write-Output ('[' + $stepNumber + '/' + $nativeJobs.Count + '] Checking ' + $job.Name)
        Invoke-ProvisioningIndependentStep -Name $job.Name -Requires $job.Requires -Results $nativeResults -Failures $nativeFailures -ContinueTerminalFailures -InputValues $inputs -Action {
            Write-Output ('[' + [DateTime]::Now.ToString('HH:mm:ss') + '] Starting ' + $job.Name + ' (time limit ' + $job.Seconds + ' seconds)')
            $ready = @($nativeResults.Keys | Where-Object { $nativeResults[$_].status -in @('succeeded','retained') }) -join '|'
            $arguments = @('-NoLogo','-NoProfile','-NonInteractive','-WindowStyle','Hidden','-ExecutionPolicy','Bypass','-File',
                (Join-Path $nativeInputDirectory 'workstation-action.ps1'), '-NativeProfilePath',$NativeProfilePath,'-NativeRoot',$NativeRoot,
                '-BasePath',$BasePath,'-StacksPath',$StacksPath,'-RunnerPath',$RunnerPath,'-Action',$job.Action)
            if ($ready) { $arguments += @('-ReadySteps',$ready) }
            if ($job.PackageID) { $arguments += @('-PackageID',$job.PackageID) }
            if ($job.Version) { $arguments += @('-PackageVersion',$job.Version) }
            $result = Invoke-ProvisioningNativeResult -Role $job.Name -FilePath (Join-Path $PSHOME 'powershell.exe') -ArgumentList $arguments -SuccessExitCodes @(0,3) -TimeoutSeconds $job.Seconds -ForwardOutput
            $diagnostic = Get-ProvisioningBoundedDiagnosticText -Text ([string]$result.Output) -MaximumBytes 3000
            $job.Diagnostic = $diagnostic
            if (-not $result.Succeeded) {
                if ($result.Stopped) { throw [OperationCanceledException]::new($job.Name + ' was cancelled.') }
                if ($result.TimedOut) { throw ($job.Name + ' exceeded its ' + $job.Seconds + ' second deadline. ' + $diagnostic) }
                throw ($job.Name + ' failed with exit code ' + $result.ExitCode + '. ' + $diagnostic)
            }
            if ($result.ExitCode -eq 3) { $OptionalFailures.Add($job.Name + ': ' + $diagnostic) }
        }
        if (-not $nativeResults[$job.Name].message -and $job.Diagnostic) { $nativeResults[$job.Name].message = $job.Diagnostic }
        Save-WorkstationReport
        Write-Output ('[' + [DateTime]::Now.ToString('HH:mm:ss') + '] ' + $job.Name + ': ' + $nativeResults[$job.Name].status)
        if ($nativeResults[$job.Name].status -in @('failed','blocked','cancelled')) {
            Write-Output $nativeResults[$job.Name].message
        }
    }
} finally {
    Save-WorkstationReport
}
if ($nativeFailures.Count -gt 0 -or $OptionalFailures.Count -gt 0) { exit 1 }
exit 0
