$OptionalFailures = New-Object 'Collections.Generic.List[string]'
$failures = New-Object 'Collections.Generic.List[string]'
function Write-ProvisioningProgress { param($Message) }
Initialize-ProvisioningSteps -Path '' -Resume $false -ImplementationFiles @($PSCommandPath)
$requires = $env:WORKSTATION_TEST_DEPENDENCIES | ConvertFrom-Json
$results = [ordered]@{
    'Vulkan SDK' = @{ status='succeeded' }
    'C and C++' = @{ status='succeeded' }
    'CMake' = @{ status='succeeded' }
    'Rust MSVC' = @{ status='failed' }
    'Stack bun' = @{ status='failed' }
    'WebView2' = @{ status='failed' }
}
$script:ran = $false
Invoke-ProvisioningIndependentStep -Name 'Handy native headers' -Requires $requires -Results $results -Failures $failures -Action { $script:ran = $true }
if (-not $script:ran -or $results['Handy native headers'].status -ne 'succeeded') { throw 'Unrelated failures suppressed header preparation.' }
$results['Vulkan SDK'].status = 'failed'
$script:ran = $false
Invoke-ProvisioningIndependentStep -Name 'Handy native headers' -Requires $requires -Results $results -Failures $failures -Action { $script:ran = $true }
if ($script:ran -or $results['Handy native headers'].status -ne 'blocked') { throw 'A failed real prerequisite did not block header preparation.' }
Write-Output 'verified actual Handy dependency effects'

# Execute the shared desktop function, intercepting only the registry boundary.
# Any new policy outside these UI roots must be deliberately reviewed.
$writes = @{}
function Set-ProvisioningRegistryValue {
    param($Path, $Name, $Value, $PropertyType)
    $allowed = @(
        'HKCU:\Software\Microsoft\Windows\CurrentVersion\ContentDeliveryManager',
        'HKCU:\Software\Microsoft\Windows\CurrentVersion\Privacy',
        'HKCU:\Software\Microsoft\Windows\CurrentVersion\UserProfileEngagement',
        'HKCU:\Software\Microsoft\Windows\CurrentVersion\Explorer',
        'HKCU:\Software\Microsoft\Windows\CurrentVersion\Explorer\Advanced',
        'HKCU:\Software\Microsoft\Windows\CurrentVersion\Explorer\Advanced\People',
        'HKCU:\Software\Microsoft\Windows\CurrentVersion\Explorer\CabinetState',
        'HKCU:\Software\Policies\Microsoft\Windows\Explorer',
        'HKCU:\Software\Microsoft\Windows\CurrentVersion\Search',
        'HKCU:\Software\Microsoft\Windows\CurrentVersion\SearchSettings',
        'HKCU:\Software\Microsoft\Windows\CurrentVersion\Windows Search',
        'HKLM:\SOFTWARE\Policies\Microsoft\Dsh',
        'HKLM:\SOFTWARE\Policies\Microsoft\Windows\Windows Feeds'
    )
    if ($Path -notin $allowed -or $PropertyType -ne 'DWord' -or $Name -match 'Audio|SmartScreen|Defender|EnableLUA|ConsentPrompt') {
        throw "Desktop preferences attempted an unapproved policy: ${Path}::$Name"
    }
    $writes[$Path + '/' + $Name] = $Value
    return $true
}
if (-not (Set-ProvisioningDesktopUsability)) { throw 'Desktop changes were not reported.' }
if ($writes['HKCU:\Software\Microsoft\Windows\CurrentVersion\Explorer\Advanced/HideFileExt'] -ne 0 -or
    $writes['HKCU:\Software\Microsoft\Windows\CurrentVersion\Explorer\Advanced/Hidden'] -ne 1 -or
    $writes['HKLM:\SOFTWARE\Policies\Microsoft\Windows\Windows Feeds/EnableFeeds'] -ne 0) {
    throw 'Required desktop preferences were not applied.'
}
$sandboxWrites = $writes.Clone()
$writes = @{}
if (-not (Set-ProvisioningDesktopUsability -PreserveWindowsProtections)) { throw 'Workstation desktop changes were not reported.' }
$widgets = 'HKLM:\SOFTWARE\Policies\Microsoft\Dsh/AllowNewsAndInterests'
if ($writes.ContainsKey($widgets) -or -not $sandboxWrites.ContainsKey($widgets)) { throw 'Widgets policy target boundary was not preserved.' }
$sandboxWrites.Remove($widgets)
if ($writes.Count -ne $sandboxWrites.Count) { throw 'Workstation changed unrelated desktop preferences.' }
foreach ($key in $sandboxWrites.Keys) {
    if (-not $writes.ContainsKey($key) -or $writes[$key] -ne $sandboxWrites[$key]) { throw "Workstation changed desktop preference $key" }
}
