$OptionalFailures = New-Object 'Collections.Generic.List[string]'
$failures = New-Object 'Collections.Generic.List[string]'
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
function Get-ProvisioningToolVersion { param($Tool, $Requested) return $Requested }
function Install-StackHyperFramesRendering { param($NodeVersion, $FFmpegVersion, $Version) $global:renderRuns++ }
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

Initialize-ProvisioningSteps -Path $statePath -Resume $false -ImplementationFiles @($PSCommandPath)
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
Initialize-ProvisioningSteps -Path $statePath -Resume $true -ImplementationFiles @($PSCommandPath)
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
Initialize-ProvisioningSteps -Path $statePath -Resume $true -ImplementationFiles @($PSCommandPath)
Invoke-ProvisioningStep -Name 'Good' -Action { $script:goodRuns++ }
if ($script:goodRuns -ne 2) { throw 'Interrupted step retained stale success.' }

# Integrity and cancellation are never independent warnings.
foreach ($exception in @([Security.SecurityException]::new('checksum mismatch'), [OperationCanceledException]::new('cancelled'))) {
    $terminal = $false
    try { Invoke-ProvisioningIndependentStep -Name 'Terminal' -Failures $failures -Action { throw $exception } }
    catch { $terminal = Test-ProvisioningTerminalError -Exception $_.Exception }
    if (-not $terminal -or $failures.Count -ne 0) { throw 'Terminal failure was hidden.' }
}

# TTS validation failure deactivates it, and cannot become optional availability.
function Install-StackHyperFramesVoxCPM2 { param($Node) throw 'controlled archive checksum mismatch' }
$terminal = $false
try { Invoke-ProvisioningIndependentStep -Name 'Bad TTS' -Failures $failures -Action { Install-HyperFramesStack } }
catch { $terminal = Test-ProvisioningTerminalError -Exception $_.Exception }
if (-not $terminal -or $global:activated -or $OptionalFailures.Count -ne 0 -or $failures.Count -ne 0) {
    throw 'TTS integrity failure was activated or downgraded.'
}

Initialize-ProvisioningSteps -Path $statePath -Resume $false -ImplementationFiles @($PSCommandPath)
Invoke-ProvisioningStep -Name 'Good' -Action { $script:goodRuns++ }
if ($script:goodRuns -ne 3) { throw 'Explicit refresh incorrectly reused completion.' }
Write-Output 'verified selective continuation'
