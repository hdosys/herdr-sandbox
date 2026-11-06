$OptionalFailures = New-Object 'Collections.Generic.List[string]'
$failures = New-Object 'Collections.Generic.List[string]'
function Write-ProvisioningProgress { param($Message) }
Initialize-ProvisioningSteps -Path '' -Resume $false -ImplementationFiles @($PSCommandPath)
$requires = $env:WORKSTATION_TEST_DEPENDENCIES | ConvertFrom-Json
$results = [ordered]@{
    'Vulkan SDK' = @{ status='succeeded' }
    'C and C++' = @{ status='succeeded' }
    'Rust MSVC' = @{ status='failed' }
    'Stack bun' = @{ status='failed' }
    'WebView2' = @{ status='failed' }
}
$script:ran = $false
Invoke-ProvisioningIndependentStep -Name 'Handy native headers' -Requires $requires -Results $results -Failures $failures -ContinueTerminalFailures -Action { $script:ran = $true }
if (-not $script:ran -or $results['Handy native headers'].status -ne 'succeeded') { throw 'Unrelated failures suppressed header preparation.' }
$results['Vulkan SDK'].status = 'failed'
$script:ran = $false
Invoke-ProvisioningIndependentStep -Name 'Handy native headers' -Requires $requires -Results $results -Failures $failures -ContinueTerminalFailures -Action { $script:ran = $true }
if ($script:ran -or $results['Handy native headers'].status -ne 'blocked') { throw 'A failed real prerequisite did not block header preparation.' }
Write-Output 'verified actual Handy dependency effects'
