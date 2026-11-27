param(
    [Parameter(Mandatory = $true)][string]$NativeProfilePath,
    [Parameter(Mandatory = $true)][string]$NativeRoot,
    [Parameter(Mandatory = $true)][string]$BasePath,
    [Parameter(Mandatory = $true)][string]$StacksPath,
    [Parameter(Mandatory = $true)][string]$RunnerPath,
    [Parameter(Mandatory = $true)][string]$Action,
    [string]$PackageID = '',
    [string]$PackageVersion = '',
    [AllowEmptyString()][string]$ReadySteps = ''
)
$ErrorActionPreference = 'Stop'
Set-StrictMode -Version 2.0
$env:PSModulePath = $PSHOME + '\Modules'
Import-Module Microsoft.PowerShell.Utility -ErrorAction Stop
[Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12
$nativeInputDirectory = Split-Path -Parent $NativeProfilePath
$env:HERDR_PROVISIONING_ROOT = $NativeRoot
$env:HERDR_PROVISIONING_TARGET = 'workstation'
$env:HERDR_PROVISIONING_RUNNER = $RunnerPath
$env:HERDR_SANDBOX_STATUS_DIRECTORY = $null
$nativeProcessOwner = Join-Path $nativeInputDirectory 'provisioning-process.cs'
. $BasePath -Phase Library -ProcessOwnerPath $nativeProcessOwner -PreparedInputsPath (Join-Path $nativeInputDirectory 'prepared-inputs.json')
. $StacksPath
$global:HerdrWorkstationReadySteps = @($ReadySteps -split '\|' | Where-Object { $_ })
$nativeProfile = [IO.File]::ReadAllText($NativeProfilePath) | ConvertFrom-Json
$provisioningPackagePlan = [pscustomobject]@{ Enabled=@{}; Versions=@{} }
foreach ($package in @($nativeProfile.packages)) {
    $provisioningPackagePlan.Enabled[[string]$package.id] = $true
    $provisioningPackagePlan.Versions[[string]$package.id] = [string]$package.version
}
foreach ($tool in @($nativeProfile.tools)) {
    $global:HerdrSandboxToolVersionPlan.Versions[[string]$tool.tool] = [string]$tool.version
    $global:HerdrSandboxToolVersionPlan.Series[[string]$tool.tool] = [string]$tool.series
    $global:HerdrSandboxToolVersionPlan.Sources[[string]$tool.tool] = [string]$tool.source
}
$OptionalFailures = New-Object 'Collections.Generic.List[string]'
Initialize-ProvisioningSteps -Path '' -Resume $false -ImplementationFiles @($BasePath, $StacksPath, $nativeProcessOwner)
$global:HerdrSandboxStepState.StackImplementation = Get-ProvisioningStepFingerprint -Files @($StacksPath)
Update-ProvisioningPath
foreach ($name in @('RUSTUP_HOME','CARGO_HOME','RUSTUP_AUTO_INSTALL','UV_CACHE_DIR','UV_NO_MANAGED_PYTHON',
        'ZIG_LOCAL_CACHE_DIR','ZIG_GLOBAL_CACHE_DIR','ANDROID_HOME','ANDROID_SDK_ROOT','ANDROID_USER_HOME','JAVA_HOME',
        'PLAYWRIGHT_BROWSERS_PATH','CHROME_PATH','NO_UPDATE_NOTIFIER','CMAKE_PREFIX_PATH','OPENSRC_HOME')) {
    $value = [Environment]::GetEnvironmentVariable($name, 'User')
    if ($null -ne $value) { [Environment]::SetEnvironmentVariable($name, $value, 'Process') }
}

switch ($Action) {
    'desktop' {
        & $BasePath -Phase Registry -PreserveWindowsProtections -AudioOutputEnabled -AudioInputEnabled -ProcessOwnerPath $nativeProcessOwner
    }
    'winget' {
        & (Join-Path $nativeInputDirectory 'workstation-winget.ps1')
    }
    'package' {
        Install-ProvisioningPackage -Id $PackageID -Version $PackageVersion
    }
    'shell' { Initialize-ProvisioningPowerShellProfile }
    'nushell' { Initialize-ProvisioningNushell }
    'vs' { Install-StackVisualStudioBuildTools }
    'node-runtime' { Install-NodeRuntime }
    'java' { Install-JavaStack }
    'python' { Install-PythonStack }
    'uv' { Install-Uv }
    'opensrc' { Install-ProvisioningOpenSrc }
    'font' { Install-ProvisioningGeistMonoNerdFont }
    'go' { Install-GoStack }
    'dotnet' { Install-DotNetStack }
    'nsis' { Install-NSISStack }
    'zig' { Install-ZigStack }
    'bun' { Install-BunStack }
    'just' { Install-Just }
    'cpp' { Initialize-StackCppEnvironment }
    'cmake' { Install-StackCMake }
    'rust-msvc' { Install-RustMSVCStack }
    'rust-msvc-ready' { Write-Output 'Rust toolchain and MSVC prerequisites are ready.' }
    'cargo-nextest' { Install-CargoNextest }
    'android' { Install-AndroidStack }
    'reaper' { Install-StackREAPER }
    'audiogridder' { Install-StackAudioGridder }
    'node' { Install-NodeStack }
    'playwright-cli' { Install-PlaywrightCLIStack }
    'oracle' { Install-OracleStack }
    'tradingview-desktop' { Install-StackTradingViewDesktop }
    'tvcontrol' { Install-TVControl }
    'hyperframes' { Install-StackHyperFramesRendering }
    'ffmpeg' { Install-StackFFmpeg }
    'tts' { Install-StackHyperFramesOptionalTTS }
    'git-sh' { Initialize-ProvisioningGitShell }
    'vulkan-sdk' { Install-StackVulkanSDK }
    'webview2' { Install-StackWebView2 }
    'handy-headers' { Initialize-StackHandyNativeHeaders }
    default { throw 'Unsupported workstation action.' }
}
if ($OptionalFailures.Count -gt 0) { exit 3 }
exit 0
