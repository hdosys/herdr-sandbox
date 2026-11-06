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
. $BasePath -Phase Library -ProcessOwnerPath $nativeProcessOwner
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
        'PLAYWRIGHT_BROWSERS_PATH','NO_UPDATE_NOTIFIER','CMAKE_PREFIX_PATH','OPENSRC_HOME')) {
    $value = [Environment]::GetEnvironmentVariable($name, 'User')
    if ($null -ne $value) { [Environment]::SetEnvironmentVariable($name, $value, 'Process') }
}

function Install-WorkstationHerdr {
    $existing = Get-Command herdr.exe -CommandType Application -ErrorAction SilentlyContinue
    if ($null -ne $existing) {
        $identity = (Invoke-ProvisioningNative -Role 'Existing Herdr identity' -FilePath $existing.Source -ArgumentList @('--version') -TimeoutSeconds 15) -join ' '
        if ($identity -notmatch '^herdr-ext ') { throw 'Existing Herdr is not Herdr Extended; it was preserved, not replaced.' }
        Write-Output 'Existing Herdr Extended retained without update, restart or server mutation.'
        return
    }
    Install-ProvisioningOnlineWinGetPackage -Role 'Herdr Extended' -Id 'hdosys.herdr-win'
    Assert-ProvisioningCommand -Role 'Herdr Extended' -Name 'herdr.exe' -VersionArguments @('--version') -ExpectedPattern '^herdr-ext ' | Out-Null
}

function Assert-WorkstationWritablePath {
    param([string]$Path)
    for ($current = $Path; $current; $current = Split-Path -Parent $current) {
        if (Test-Path -LiteralPath $current) {
            $item = Get-Item -LiteralPath $current -Force
            if (($item.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) { throw [Security.SecurityException]::new("Configuration path is a reparse point: $current") }
        }
    }
}

function Set-WorkstationShell {
    $directory = Get-ProvisioningPath 'shell'
    Assert-WorkstationWritablePath $directory
    New-Item -ItemType Directory -Path $directory -Force | Out-Null
    $initialization = Join-Path $directory 'initialize.ps1'
    Assert-WorkstationWritablePath $initialization
    $contents = @'
$env:Path = [Environment]::GetEnvironmentVariable('Path','Machine') + ';' + [Environment]::GetEnvironmentVariable('Path','User')
if (Get-Command starship.exe -ErrorAction SilentlyContinue) { Invoke-Expression (& starship.exe init powershell) }
'@
    [IO.File]::WriteAllText($initialization, $contents, (New-Object Text.UTF8Encoding($false)))
    $profileRoot = Join-Path ([Environment]::GetFolderPath('MyDocuments')) 'PowerShell'
    Assert-WorkstationWritablePath $profileRoot
    New-Item -ItemType Directory -Path $profileRoot -Force | Out-Null
    $path = Join-Path $profileRoot 'profile.ps1'
    Assert-WorkstationWritablePath $path
    $line = ". '" + $initialization.Replace("'", "''") + "'"
    if (Test-Path -LiteralPath $path) {
        $item = Get-Item -LiteralPath $path -Force
        if ($item.PSIsContainer -or $item.Length -gt 1048576) { throw 'Existing PowerShell profile is unsafe; it was preserved.' }
        $existing = [IO.File]::ReadAllText($path)
        if ($existing.Trim() -ceq $line) { return }
        throw 'Existing PowerShell profile was preserved. Add the managed shell loader explicitly if desired.'
    } else { $contents = $line + "`r`n" }
    [IO.File]::WriteAllText($path, $contents, (New-Object Text.UTF8Encoding($false)))
}

switch ($Action) {
    'winget' {
        & (Join-Path $nativeInputDirectory 'workstation-winget.ps1')
        if ($LASTEXITCODE -ne 0) { throw 'WinGet prerequisite failed.' }
    }
    'package' {
        if ($PackageID -ieq 'hdosys.herdr-win') { Install-WorkstationHerdr }
        else { Install-ProvisioningOnlineWinGetPackage -Role $PackageID -Id $PackageID -Version $PackageVersion }
    }
    'shell' { Set-WorkstationShell }
    'nushell' { Initialize-ProvisioningNushell }
    'vs-layout' {
        Invoke-ProvisioningNative -Role 'Visual Studio layout preparation' -FilePath (Join-Path $PSHOME 'powershell.exe') -ArgumentList @(
            '-NoLogo','-NoProfile','-NonInteractive','-WindowStyle','Hidden','-ExecutionPolicy','Bypass','-File',(Join-Path $nativeInputDirectory 'prepare-visual-studio-layout.ps1'),
            '-CacheDirectory',(Get-ProvisioningPath 'cache'),'-TimeoutSeconds','3600') -TimeoutSeconds 3600 | Out-Null
    }
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
    'cpp' { Install-CppStack }
    'rust-msvc' { Install-RustMSVCStack }
    'cargo-nextest' { Install-CargoNextest }
    'android' { Install-AndroidStack }
    'reaper' { Install-StackREAPER }
    'audiogridder' { Install-StackAudioGridder }
    'node' { Install-NodeStack }
    'playwright-cli' { Install-PlaywrightCLIStack }
    'tradingview' { Install-TradingViewStack }
    'hyperframes' { Install-StackHyperFramesRendering }
    'tts-models' { Invoke-ProvisioningNative -Role 'TTS bundle preparation' -FilePath $RunnerPath -ArgumentList @('workstation','prepare-tts','--root',$NativeRoot) -TimeoutSeconds 1800 | Out-Null }
    'tts' { Install-StackHyperFramesOptionalTTS }
    'git-sh' {
        $git = (Get-Command git.exe -CommandType Application -ErrorAction Stop).Source
        $bash = Join-Path (Split-Path (Split-Path $git -Parent) -Parent) 'bin\bash.exe'
        Invoke-ProvisioningNative -Role 'Git shell verification' -FilePath $bash -ArgumentList @('--version') -TimeoutSeconds 30 | Out-Null
    }
    'vulkan-sdk' { Install-StackVulkanSDK }
    'webview2' { Install-StackWebView2 }
    'handy-headers' {
        Enable-StackVisualStudioDeveloperEnvironment
        $vulkanRoot = [Environment]::GetEnvironmentVariable('VULKAN_SDK', 'Machine')
        $prefix = Write-HandySPIRVHeadersPackage -VulkanRoot $vulkanRoot
        Assert-HandyNativeToolchain -CMakePrefix $prefix -VulkanRoot $vulkanRoot
    }
    default { throw 'Unsupported workstation action.' }
}
if ($OptionalFailures.Count -gt 0) { exit 3 }
exit 0
