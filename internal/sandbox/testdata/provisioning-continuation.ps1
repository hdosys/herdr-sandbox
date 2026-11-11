$OptionalFailures = New-Object 'Collections.Generic.List[string]'
$failures = New-Object 'Collections.Generic.List[string]'
$DevelopmentFailures = $failures
$statePath = Join-Path $PSScriptRoot 'steps.json'
$inputPath = Join-Path $PSScriptRoot 'input.txt'
[IO.File]::WriteAllText($inputPath, 'first')
function Write-ProvisioningProgress { param($Message) }
$script:goodRuns = 0
$script:changedRuns = 0
$global:renderRuns = 0
$global:ttsRuns = 0
$global:activated = $false
$global:runtimeFailure = $true
$global:renderFailure = $false
function Get-ProvisioningToolVersion { param($Tool, $Requested) return $Requested }
function Install-StackHyperFramesRendering { param($NodeVersion, $FFmpegVersion, $Version) $global:renderRuns++; if ($global:renderFailure) { throw 'controlled rendering rejection' } }
function Install-StackFFmpeg { param($Version) }
function Install-NodeRuntime { param($Version); Invoke-ProvisioningStackStep -Name 'Node.js' -Requires @('WinGet') -Action {} }
function Disable-StackHyperFramesTTS { $global:activated = $false }
function Get-StackNodeTools { return @{ Node = 'controlled-node' } }
function Invoke-ProvisioningNative {
    param($Role, $FilePath, $ArgumentList, $TimeoutSeconds)
    if ($global:runtimeFailure) { throw 'controlled missing native Python extension' }
}
function Install-StackHyperFramesVoxCPM2 {
    param($Node)
    $global:ttsRuns++
    Assert-StackSupertonicRuntime -Python 'controlled-python'
    $global:activated = $true
}
$profilePath = Join-Path $PSScriptRoot 'project.ps1'
[IO.File]::WriteAllText($profilePath, 'param($ProjectDirectory); Install-HyperFramesStack')

function Initialize-FixtureSteps([bool]$Resume) {
    Initialize-ProvisioningSteps -Path $statePath -Resume $Resume -ImplementationFiles @($PSCommandPath)
    foreach ($name in @('WinGet','Package Git.Git')) { $global:HerdrSandboxStepState.Results[$name] = [ordered]@{status='succeeded';message='fixture prerequisite'} }
}
Initialize-FixtureSteps $false
$packageJobs = @(Get-ProvisioningPackageJobs -Packages @(
    [pscustomobject]@{id='OpenAI.Codex';version='1.0.0'},
    [pscustomobject]@{id='Anomaly.OpenCode';version='1.0.0'}
))
$implicitRuntime = @($packageJobs | Where-Object ID -eq 'Microsoft.VCRedist.2015+.x64')
if ($implicitRuntime.Count -ne 1 -or $implicitRuntime[0].Version -cne '' -or
    @($packageJobs | Where-Object ID -eq 'Anomaly.OpenCode')[0].Requires.Count -ne 0) {
    throw 'Implicit package dependencies or independent direct packages were planned incorrectly.'
}
Invoke-ProvisioningStep -Name 'Good' -Action { $script:goodRuns++ }
Invoke-ProvisioningStep -Name 'Package Microsoft.VCRedist.2015+.x64' -Action {}
Invoke-ProvisioningStep -Name 'C and C++' -Action {}
Invoke-ProvisioningStep -Name 'Changed' -InputFiles @($inputPath) -Action { $script:changedRuns++ }
Invoke-ProvisioningIndependentStep -Name 'Failed' -Failures $failures -Action { throw 'repair me' }
Invoke-ProvisioningIndependentStep -Name 'Profile' -Failures $failures -Action {
    Invoke-ProvisioningExtensionScript -Kind Project -Path $profilePath -WorkspaceName fixture -ProjectDirectory $PSScriptRoot
}
if ($global:activated -or $OptionalFailures.Count -ne 1 -or $failures.Count -ne 1) {
    throw 'Optional runtime failure did not remain separate from failed development.'
}

# Reload persisted evidence as a new invocation; do not rely on in-memory reuse.
Initialize-FixtureSteps $true
Invoke-ProvisioningStep -Name 'Package Microsoft.VCRedist.2015+.x64' -Action { throw 'Completed VC++ runtime was repeated.' }
Invoke-ProvisioningStep -Name 'C and C++' -Action { throw 'Completed C++ step was repeated.' }
$OptionalFailures.Clear()
$failures.Clear()
[IO.File]::WriteAllText($inputPath, 'corrected')
$global:runtimeFailure = $false
Invoke-ProvisioningStep -Name 'Good' -Action { $script:goodRuns++ }
Invoke-ProvisioningStep -Name 'Changed' -InputFiles @($inputPath) -Action { $script:changedRuns++ }
Invoke-ProvisioningIndependentStep -Name 'Failed' -Failures $failures -Action { $script:repaired = $true }
Invoke-ProvisioningIndependentStep -Name 'Profile' -Failures $failures -Action {
    Invoke-ProvisioningExtensionScript -Kind Project -Path $profilePath -WorkspaceName fixture -ProjectDirectory $PSScriptRoot
}
if ($script:goodRuns -ne 1 -or $script:changedRuns -ne 2 -or $global:renderRuns -ne 1 -or
    $global:ttsRuns -ne 2 -or -not $global:activated -or -not $script:repaired -or
    $OptionalFailures.Count -ne 0 -or $failures.Count -ne 0) { throw 'Continuation reran completed work or failed to repair unfinished work.' }

# An interrupted changed step must not leave its older completion usable.
try {
    Invoke-ProvisioningStep -Name 'Good' -InputValues @('changed') -Action { throw [OperationCanceledException]::new('cancelled') }
    throw 'Cancellation was swallowed.'
} catch [OperationCanceledException] {}
Initialize-FixtureSteps $true
Invoke-ProvisioningStep -Name 'Good' -Action { $script:goodRuns++ }
if ($script:goodRuns -ne 2) { throw 'Interrupted step retained stale success.' }

# Failed packages block dependants, not independent work or the containing profile.
$results = [ordered]@{}
$script:compositeRuns = 0
$script:dependentRuns = 0
$script:independentRuns = 0
$script:packageBroken = $true
$compositeAction = {
    $script:compositeRuns++
    Invoke-ProvisioningIndependentStep -Name 'Fixture package' -Results $results -Failures $failures -Action {
        if ($script:packageBroken) { throw [Security.SecurityException]::new('controlled package checksum mismatch') }
    }
    Invoke-ProvisioningIndependentStep -Name 'Fixture dependant' -Requires @('Fixture package') -Results $results -Failures $failures -Action { $script:dependentRuns++ }
    Invoke-ProvisioningIndependentStep -Name 'Fixture independent effect' -Results $results -Failures $failures -Action { $script:independentRuns++ }
}
Invoke-ProvisioningIndependentStep -Name 'Fixture composite' -Results $results -Failures $failures -Action $compositeAction
if ($failures.Count -eq 0 -or $results['Fixture package'].status -ne 'failed' -or
    $results['Fixture dependant'].status -ne 'blocked' -or $script:dependentRuns -ne 0 -or
    $results['Fixture independent effect'].status -ne 'succeeded' -or $script:independentRuns -ne 1) {
    throw 'Package failure escaped its boundary, ran a blocked dependant, or suppressed independent work.'
}
Initialize-FixtureSteps $true
$failures.Clear()
$results = [ordered]@{}
$script:packageBroken = $false
Invoke-ProvisioningIndependentStep -Name 'Fixture composite' -Results $results -Failures $failures -Action $compositeAction
if ($script:compositeRuns -ne 2 -or $script:dependentRuns -ne 1 -or
    $script:independentRuns -ne 1 -or $failures.Count -ne 0 -or
    $results['Fixture package'].status -ne 'succeeded' -or $results['Fixture dependant'].status -ne 'succeeded') {
    throw 'Failed composite was retained or successful independent work repeated.'
}

# Both direct and wrapped cancellation must still stop nested execution.
foreach ($exception in @([OperationCanceledException]::new('cancelled'), [Exception]::new('wrapped cancellation', [OperationCanceledException]::new('cancelled')))) {
    $terminal = $false
    try {
        Invoke-ProvisioningIndependentStep -Name 'Terminal parent' -Failures $failures -Action {
            Invoke-ProvisioningIndependentStep -Name 'Terminal child' -Failures $failures -Action { throw $exception }
        }
    }
    catch { $terminal = Test-ProvisioningTerminalError -Exception $_.Exception }
    if (-not $terminal -or $failures.Count -ne 0) { throw 'Terminal failure was hidden.' }
}

# TTS validation failure deactivates it, and cannot become optional availability.
Initialize-FixtureSteps $true
function Install-StackHyperFramesVoxCPM2 { param($Node) $global:ttsRuns++; throw 'controlled archive checksum mismatch' }
Invoke-ProvisioningIndependentStep -Name 'Bad TTS' -Failures $failures -Action { Install-HyperFramesStack }
if ($global:activated -or $OptionalFailures.Count -ne 0 -or $failures.Count -ne 1) {
    throw 'TTS integrity failure was activated or downgraded.'
}
$failures.Clear()

# Multiple profiles must not repeatedly retry the same failed package in one run.
$script:failedStackAttempts = 0
$failedStack = { $script:failedStackAttempts++; throw 'controlled WinGet lookup failure' }
Invoke-ProvisioningStackStep -Name 'Shared failed stack' -Action $failedStack
Invoke-ProvisioningStackStep -Name 'Shared failed stack' -Action $failedStack
if ($script:failedStackAttempts -ne 1 -or $failures.Count -ne 2) { throw 'Shared failed stack was retried or hidden from its callers.' }
$failures.Clear()

Initialize-FixtureSteps $false
$global:renderFailure = $true
$renderBefore = $global:renderRuns
$ttsBefore = $global:ttsRuns
Install-HyperFramesStack
Install-HyperFramesStack
if ($global:renderRuns -ne $renderBefore + 1 -or $global:ttsRuns -ne $ttsBefore) { throw 'Failed rendering repeated or its dependent TTS ran.' }
$failures.Clear()

Initialize-FixtureSteps $false
Invoke-ProvisioningStep -Name 'Good' -Action { $script:goodRuns++ }
if ($script:goodRuns -ne 3) { throw 'Explicit refresh incorrectly reused completion.' }

# VS preflight and already-installed branches must preserve independent Rust work.
Initialize-FixtureSteps $false
$script:rustAttempts = 0
$script:rustFails = $false
$script:vsPathReads = 0
function Get-ProvisioningPath { param($Relative); $script:vsPathReads++; if ($Relative -eq 'visual-studio\layout') { return 'C:\vs-fixture' }; return (Join-Path $PSScriptRoot 'vs-cache') }
function Invoke-ProvisioningNative { param($Role,$FilePath,$ArgumentList,$TimeoutSeconds,$WorkingDirectory); $script:rustAttempts++; if ($script:rustFails) { throw 'controlled Rust failure' } }
$rustTask = @{Role='controlled Rust';FilePath='not-executed.exe';ArgumentList=@();TimeoutSeconds=1}
Install-StackVisualStudioBuildTools -RustToolchainTask $rustTask
$firstReads = $script:vsPathReads
Install-StackVisualStudioBuildTools -RustToolchainTask $rustTask
if ($script:rustAttempts -ne 2 -or $script:vsPathReads -ne $firstReads -or
    $global:HerdrSandboxStepState.Results['Visual Studio Build Tools'].status -ne 'failed') {
    throw 'VS preflight failure skipped Rust or repeated the same VS attempt.'
}
$failures.Clear()
Initialize-FixtureSteps $false
$cache = Join-Path $PSScriptRoot 'vs-cache'
$null = New-Item -ItemType Directory -Path (Join-Path $cache 'a')
[IO.File]::WriteAllText((Join-Path $cache 'a\complete.json'), '{}')
function Assert-ProvisioningCachePath { param($Path); if (-not $Path.StartsWith($PSScriptRoot)) { throw 'Fixture escaped its owned directory.' } }
function ConvertFrom-StackVisualStudioLayoutDescriptor { param($Descriptor,$Slot); return [pscustomobject]@{BuildVersion='1.0.0';CurrentMetadata=$true} }
function Test-StackVisualStudioLayoutSlot { param($Slot,$Target); return $true }
function Get-StackVisualStudioInstallation { param($Target); return 'C:\verified-vs-fixture' }
function Write-ProvisioningTiming { param($Role,$Seconds) }
$script:rustFails = $true
$rustRejected = $false
try { Install-StackVisualStudioBuildTools -RustToolchainTask $rustTask } catch { $rustRejected=$true }
if (-not $rustRejected -or $global:HerdrSandboxStepState.Results['Visual Studio Build Tools'].status -ne 'succeeded') {
    throw 'Rust failure invalidated independently verified installed VS.'
}
$failures.Clear()

# Online WinGet preparation must not leave later installation steps in prepare mode.
function Get-ProvisioningPath { param($Relative) return $PSScriptRoot }
function Get-ProvisioningGitHubAssetMetadata { param($Role,$Repository,$AssetName,$Id,$Version,$Architecture,$InstallerType,$PayloadName); return [pscustomobject]@{Id=$Id;Version='1.0.0'} }
function Get-ProvisioningSignedMSIXMetadata { param($PayloadPath,$Id,$Url,$PackageName,$Publisher,$Architecture); return [pscustomobject]@{Id=$Id;Version='1.0.0'} }
function Invoke-WebRequest { param($Uri,$OutFile,[switch]$UseBasicParsing,$TimeoutSec); [IO.File]::WriteAllText($OutFile,'fixture') }
function Install-ProvisioningCachedPackage {
    param($Role,$Metadata,$DownloadSource,$Adapter,$ResolvedDirectPayloadPath)
    if (-not (Test-ProvisioningPreparation)) { throw 'Preparation attempted installation.' }
    if ($script:rejectPreparation) { throw 'controlled download rejection' }
    $script:PreparedPackageMetadata[$Metadata.Id] = @{metadata=$Metadata}
}
$script:ProvisioningPreparation = $false
$script:PreparedPackageMetadata = @{prior='preserve'}
$priorMetadata = $script:PreparedPackageMetadata
foreach ($reject in @($false,$true)) {
    $script:rejectPreparation = $reject
    $distribution = $null
    $rejected = $false
    try { Prepare-ProvisioningWinGet -Distribution ([ref]$distribution) } catch { if (-not $reject) { throw }; $rejected=$true }
    if ((Test-ProvisioningPreparation) -or -not [object]::ReferenceEquals($priorMetadata,$script:PreparedPackageMetadata) -or
        $rejected -ne $reject -or (-not $reject -and @($distribution.packages).Count -ne 3)) {
        throw 'WinGet preparation leaked state or concealed its result.'
    }
}
Write-Output 'verified selective continuation'
