$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'
$env:DOTNET_CLI_TELEMETRY_OPTOUT = '1'
$env:DOTNET_SKIP_FIRST_TIME_EXPERIENCE = '1'
$root = 'C:\HerdrSandbox\all-stacks-smoke'
if (Test-Path -LiteralPath $root) { Remove-Item -LiteralPath $root -Recurse -Force }
$null = New-Item -ItemType Directory -Path $root -Force
$utf8 = New-Object Text.UTF8Encoding($false)

function Write-SmokeFile([string]$Path, [string[]]$Lines) {
    $directory = [IO.Path]::GetDirectoryName($Path)
    if (-not (Test-Path -LiteralPath $directory -PathType Container)) {
        $null = New-Item -ItemType Directory -Path $directory -Force
    }
    [IO.File]::WriteAllText($Path, ([string]::Join([Environment]::NewLine, $Lines) + [Environment]::NewLine), $utf8)
}

function Invoke-SmokeTool([string]$Role, [string]$Executable, [string[]]$Arguments) {
    $previous = $ErrorActionPreference
    try {
        $ErrorActionPreference = 'Continue'
        $output = @(& $Executable @Arguments 2>&1)
        $exitCode = $LASTEXITCODE
    } finally {
        $ErrorActionPreference = $previous
    }
    $text = (($output | ForEach-Object { [string]$_ }) -join [Environment]::NewLine).Trim()
    if ($exitCode -ne 0) { throw "$Role failed with exit code $exitCode. $text" }
    [Console]::Out.WriteLine("[all-stacks] ${Role}: $text")
    return $text
}

function Assert-SmokeOutput([string]$Role, [string]$Output, [string]$Expected) {
    if (-not $Output.Contains($Expected)) { throw "$Role output did not contain $Expected" }
}

function Remove-AndroidJVMWarning([string]$Output) {
    $warning = 'OpenJDK 64-Bit Server VM warning: The UseAllWindowsProcessorGroups flag is not supported on this Windows version and will be ignored.'
    return (@([regex]::Split($Output, '\r?\n') | Where-Object { $_.Trim() -cne $warning }) -join [Environment]::NewLine).Trim()
}

$readOnlyMount = 'C:\Mounts\reference'
$writableMount = 'C:\Mounts\worktrees'
if ([IO.File]::ReadAllText((Join-Path $readOnlyMount 'host-reference.txt')).Trim() -cne 'read-only-mount-ok') {
    throw 'Read-only folder mount content is unavailable.'
}
if ([IO.File]::ReadAllText((Join-Path $writableMount 'host-worktrees.txt')).Trim() -cne 'read-write-mount-ok') {
    throw 'Writable folder mount content is unavailable.'
}
$readOnlyWriteBlocked = $false
try {
    [IO.File]::WriteAllText((Join-Path $readOnlyMount 'guest-write-blocked.txt'), ('unexpected' + [Environment]::NewLine), $utf8)
} catch {
    $readOnlyWriteBlocked = $true
}
if (-not $readOnlyWriteBlocked) { throw 'Read-only folder mount accepted a guest write.' }
[IO.File]::WriteAllText((Join-Path $writableMount 'guest-write.txt'), ('guest-write-ok' + [Environment]::NewLine), $utf8)
[Console]::Out.WriteLine('[all-stacks] folder mounts: read-only and read/write OK')

$dotnet = (Get-Command 'dotnet.exe' -CommandType Application -ErrorAction Stop | Select-Object -First 1).Source
$go = (Get-Command 'go.exe' -CommandType Application -ErrorAction Stop | Select-Object -First 1).Source
$node = (Get-Command 'node.exe' -CommandType Application -ErrorAction Stop | Select-Object -First 1).Source
$makensis = (Get-Command 'makensis.exe' -CommandType Application -ErrorAction Stop | Select-Object -First 1).Source
$nu = (Get-Command 'nu.exe' -CommandType Application -ErrorAction Stop | Select-Object -First 1).Source
$python = (Get-Command 'python.exe' -CommandType Application -ErrorAction Stop | Select-Object -First 1).Source
$python3 = (Get-Command 'python3.exe' -CommandType Application -ErrorAction Stop | Select-Object -First 1).Source
$uv = (Get-Command 'uv.exe' -CommandType Application -ErrorAction Stop | Select-Object -First 1).Source
$bun = (Get-Command 'bun.exe' -CommandType Application -ErrorAction Stop | Select-Object -First 1).Source
$cargo = (Get-Command 'cargo.exe' -CommandType Application -ErrorAction Stop | Select-Object -First 1).Source
$nextest = (Get-Command 'cargo-nextest.exe' -CommandType Application -ErrorAction Stop | Select-Object -First 1).Source
$just = (Get-Command 'just.exe' -CommandType Application -ErrorAction Stop | Select-Object -First 1).Source
$rustc = (Get-Command 'rustc.exe' -CommandType Application -ErrorAction Stop | Select-Object -First 1).Source
$sh = (Get-Command 'sh.exe' -CommandType Application -ErrorAction Stop | Select-Object -First 1).Source
$zig = (Get-Command 'zig.exe' -CommandType Application -ErrorAction Stop | Select-Object -First 1).Source
$playwrightAgentCLI = (Get-Command 'playwright-cli.cmd' -CommandType Application -ErrorAction Stop | Select-Object -First 1).Source
$tradingView = (Get-Command 'TradingView.exe' -CommandType Application -ErrorAction Stop | Select-Object -First 1).Source
$tv = (Get-Command 'tv.cmd' -CommandType Application -ErrorAction Stop | Select-Object -First 1).Source
$tvcontrol = (Get-Command 'tvcontrol.cmd' -CommandType Application -ErrorAction Stop | Select-Object -First 1).Source
$cmake = (Get-Command 'cmake.exe' -CommandType Application -ErrorAction Stop | Select-Object -First 1).Source
$glslc = (Get-Command 'glslc.exe' -CommandType Application -ErrorAction Stop | Select-Object -First 1).Source
$cl = (Get-Command 'cl.exe' -CommandType Application -ErrorAction Stop | Select-Object -First 1).Source
$link = (Get-Command 'link.exe' -CommandType Application -ErrorAction Stop | Select-Object -First 1).Source
$msbuild = (Get-Command 'msbuild.exe' -CommandType Application -ErrorAction Stop | Select-Object -First 1).Source
$java = (Get-Command 'java.exe' -CommandType Application -ErrorAction Stop | Select-Object -First 1).Source
$javac = (Get-Command 'javac.exe' -CommandType Application -ErrorAction Stop | Select-Object -First 1).Source
$androidCLI = (Get-Command 'android.exe' -CommandType Application -ErrorAction Stop | Select-Object -First 1).Source
$adb = (Get-Command 'adb.exe' -CommandType Application -ErrorAction Stop | Select-Object -First 1).Source
$openSrc = (Get-Command 'opensrc.exe' -CommandType Application -ErrorAction Stop | Select-Object -First 1).Source

$reaper = 'C:\Program Files\REAPER (x64)\reaper.exe'
$reaperInfo = Get-Item -LiteralPath $reaper -Force -ErrorAction Stop
$reaperSignature = Get-AuthenticodeSignature -LiteralPath $reaper
if ($reaperInfo.PSIsContainer -or ($reaperInfo.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0 -or
    [string]$reaperInfo.VersionInfo.FileVersion -cne '7.78' -or
    $reaperSignature.Status -ne [System.Management.Automation.SignatureStatus]::Valid -or
    $reaperSignature.SignerCertificate.Subject -notmatch '(^|,\s*)O=Cockos Incorporated(,|$)') {
    throw 'REAPER 7.78 installation identity is invalid.'
}
$audioGridderRoot = 'C:\HerdrSandbox\tools\AudioGridder'
$audioGridderCopies = [ordered]@{
    'bin\AudioGridderPluginTray.exe' = 'C:\Program Files\AudioGridderPluginTray\AudioGridderPluginTray.exe'
    'bin\crashpad_handler.exe' = 'C:\Program Files\AudioGridderPluginTray\crashpad_handler.exe'
    'lib\VST\AudioGridder.dll' = 'C:\Program Files\VstPlugins\AudioGridder.dll'
    'lib\VST\AudioGridderInst.dll' = 'C:\Program Files\VstPlugins\AudioGridderInst.dll'
    'lib\VST\AudioGridderMidi.dll' = 'C:\Program Files\VstPlugins\AudioGridderMidi.dll'
    'lib\VST3\AudioGridder.vst3' = 'C:\Program Files\Common Files\VST3\AudioGridder.vst3'
    'lib\VST3\AudioGridderInst.vst3' = 'C:\Program Files\Common Files\VST3\AudioGridderInst.vst3'
    'lib\VST3\AudioGridderMidi.vst3' = 'C:\Program Files\Common Files\VST3\AudioGridderMidi.vst3'
}
$audioGridderRootPath = [IO.Path]::GetFullPath($audioGridderRoot).TrimEnd('\')
$audioGridderActual = @(Get-ChildItem -LiteralPath $audioGridderRoot -File -Recurse -Force |
    ForEach-Object { $_.FullName.Substring($audioGridderRootPath.Length + 1) } | Sort-Object)
if (($audioGridderActual -join '|') -cne (@(@($audioGridderCopies.Keys) + 'bin\AudioGridderServer.exe' | Sort-Object) -join '|') -or
    (Test-Path -LiteralPath (Join-Path $audioGridderRoot 'lib\AAX'))) {
    throw 'AudioGridder retained server/client payload contains missing or unsupported files.'
}
foreach ($entry in $audioGridderCopies.GetEnumerator()) {
    $source = Join-Path $audioGridderRoot ([string]$entry.Key)
    $destination = [string]$entry.Value
    if (-not (Test-Path -LiteralPath $destination -PathType Leaf) -or
        (Get-FileHash -LiteralPath $source -Algorithm SHA256).Hash -cne
        (Get-FileHash -LiteralPath $destination -Algorithm SHA256).Hash) {
        throw "AudioGridder installed client file is invalid: $destination"
    }
}
$audioGridderServer = Get-Item -LiteralPath (Join-Path $audioGridderRoot 'bin\AudioGridderServer.exe') -Force
$audioGridderTray = Get-Item -LiteralPath 'C:\Program Files\AudioGridderPluginTray\AudioGridderPluginTray.exe' -Force
if ([string]$audioGridderServer.VersionInfo.FileVersion -cne '1.2.0' -or
    [string]$audioGridderServer.VersionInfo.ProductName -cne 'AudioGridderServer' -or
    [string]$audioGridderTray.VersionInfo.FileVersion -cne '1.2.0' -or
    [string]$audioGridderTray.VersionInfo.ProductName -cne 'AudioGridderPluginTray') {
    throw 'AudioGridder server or Plugin Tray identity is invalid.'
}
$audioGridderPluginConfiguration = [IO.File]::ReadAllText((Join-Path $env:APPDATA 'AudioGridder\audiogridderplugin.cfg')) | ConvertFrom-Json
$audioGridderServerConfiguration = [IO.File]::ReadAllText((Join-Path $env:APPDATA 'AudioGridder\audiogridderserver.cfg')) | ConvertFrom-Json
$audioGridderGateways = @(Get-NetIPConfiguration -ErrorAction Stop | Where-Object {
        $null -ne $_.IPv4DefaultGateway -and [string]$_.NetAdapter.Status -ceq 'Up'
    } | ForEach-Object { @($_.IPv4DefaultGateway) | ForEach-Object { [string]$_.NextHop } } |
    Where-Object { -not [string]::IsNullOrWhiteSpace($_) -and $_ -cne '0.0.0.0' } | Sort-Object -Unique)
$audioGridderServers = @($audioGridderPluginConfiguration.Servers | ForEach-Object { [string]$_ })
if ($audioGridderGateways.Count -ne 1 -or $audioGridderServers.Count -ne 1 -or
    $audioGridderServers[0] -cne '127.0.0.1:0' -or
    [string]$audioGridderPluginConfiguration.LastServer -cne '127.0.0.1:0:::0:0:00000000-0000-0000-0000-000000000000') {
    throw 'AudioGridder local REAPER endpoint is invalid.'
}
$audioGridderVST2Folders = @($audioGridderServerConfiguration.VST2Folders | ForEach-Object { [string]$_ })
$audioGridderVST3Folders = @($audioGridderServerConfiguration.VST3Folders | ForEach-Object { [string]$_ })
if ([int]$audioGridderServerConfiguration.ID -ne 0 -or [string]$audioGridderServerConfiguration.NAME -cne 'Herdr Sandbox' -or
    -not [bool]$audioGridderServerConfiguration.VST -or -not [bool]$audioGridderServerConfiguration.VST2 -or
    -not [bool]$audioGridderServerConfiguration.VSTNoStandardFolders -or -not [bool]$audioGridderServerConfiguration.ScanForPlugins -or
    -not [bool]$audioGridderServerConfiguration.Logger -or [bool]$audioGridderServerConfiguration.CrashReporting -or
    [int]$audioGridderServerConfiguration.SandboxMode -ne 1 -or [bool]$audioGridderServerConfiguration.ScreenLocalMode -or
    $audioGridderVST2Folders.Count -ne 1 -or $audioGridderVST2Folders[0] -cne 'C:\Program Files\VstPlugins' -or
    $audioGridderVST3Folders.Count -ne 1 -or $audioGridderVST3Folders[0] -cne 'C:\Program Files\Common Files\VST3') {
    throw 'AudioGridder server-0 VST execution configuration is invalid.'
}
foreach ($ruleName in @('HerdrSandbox-AudioGridder-Server0', 'HerdrSandbox-AudioGridder-Workers')) {
    $rules = @(Get-NetFirewallRule -Name $ruleName -ErrorAction Stop)
    $application = @($rules | Get-NetFirewallApplicationFilter -ErrorAction Stop)
    $address = @($rules | Get-NetFirewallAddressFilter -ErrorAction Stop)
    if ($rules.Count -ne 1 -or $application.Count -ne 1 -or $address.Count -ne 1 -or
        [string]$rules[0].Enabled -cne 'True' -or [string]$rules[0].Direction -cne 'Inbound' -or
        [string]$rules[0].Action -cne 'Allow' -or
        [IO.Path]::GetFullPath([string]$application[0].Program) -ine $audioGridderServer.FullName -or
        [string]@($address[0].RemoteAddress)[0] -cne $audioGridderGateways[0]) {
        throw "AudioGridder guest firewall rule is invalid: $ruleName"
    }
}
[Console]::Out.WriteLine("[all-stacks] audio: REAPER 7.78, AudioGridder server 0, local clients, and host-gateway firewall ready")

$expectedOpenSrc = 'C:\HerdrSandbox\tools\vercel-labs.opensrc\opensrc.exe'
$expectedOpenSrcHome = 'C:\HerdrSandbox\cache\opensrc'
if ([IO.Path]::GetFullPath($openSrc) -ine $expectedOpenSrc -or
    $env:OPENSRC_HOME -cne $expectedOpenSrcHome -or
    -not (Test-Path -LiteralPath $expectedOpenSrcHome -PathType Container) -or
    ((Get-Item -LiteralPath $expectedOpenSrcHome -Force).Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {
    throw 'opensrc command or persistent source cache is unavailable in the SSH session.'
}
$openSrcVersion = Invoke-SmokeTool 'opensrc-version' $openSrc @('--version')
if ($openSrcVersion -cne 'opensrc 0.7.3') { throw "opensrc version is unexpected: $openSrcVersion" }
$openSrcSmokeHome = Join-Path $root 'opensrc-cache'
try {
    $env:OPENSRC_HOME = $openSrcSmokeHome
    $openSrcPath = Invoke-SmokeTool 'opensrc-path' $openSrc @('path','vercel-labs/opensrc@v0.7.3')
} finally {
    $env:OPENSRC_HOME = $expectedOpenSrcHome
}
$openSrcSource = [IO.Path]::GetFullPath($openSrcPath.Trim())
$openSrcSmokeRoot = [IO.Path]::GetFullPath($openSrcSmokeHome).TrimEnd('\') + '\'
if (-not $openSrcSource.StartsWith($openSrcSmokeRoot, [StringComparison]::OrdinalIgnoreCase) -or
    -not (Test-Path -LiteralPath (Join-Path $openSrcSource 'packages\opensrc\cli\Cargo.toml') -PathType Leaf)) {
    throw "opensrc source fetch returned an unexpected path: $openSrcSource"
}
[Console]::Out.WriteLine('[all-stacks] opensrc: pinned native CLI and isolated source fetch OK')

$androidSDK = 'C:\HerdrSandbox\tools\android-sdk'
$androidJDK = 'C:\HerdrSandbox\toolchains\android-jdk-17'
if ($env:ANDROID_HOME -cne $androidSDK -or $env:ANDROID_JAVA_HOME -cne $androidJDK -or
    $env:ANDROID_USER_HOME -cne 'C:\HerdrSandbox\build\android-user' -or
    [IO.Path]::GetFullPath($androidCLI) -ine (Join-Path $androidSDK 'cmdline-tools\latest\bin\android.exe') -or
    [IO.Path]::GetFullPath($adb) -ine (Join-Path $androidSDK 'platform-tools\adb.exe')) {
    throw 'Android SDK, JDK, or command environment is unavailable in the SSH session.'
}
$androidVersion = Remove-AndroidJVMWarning (Invoke-SmokeTool 'android-cli-version' $androidCLI @('--no-metrics','--version'))
if ($androidVersion -notmatch '^1\.0\.\d+$') { throw "Android CLI version is unexpected: $androidVersion" }
$androidJDKVersion = Invoke-SmokeTool 'android-jdk-version' (Join-Path $androidJDK 'bin\java.exe') @('-version')
Assert-SmokeOutput 'android-jdk-version' $androidJDKVersion 'openjdk version "17.0.20"'
$reportedAndroidSDK = Remove-AndroidJVMWarning (Invoke-SmokeTool 'android-sdk-location' $androidCLI @('--no-metrics',"--sdk=$androidSDK",'info','sdk'))
if ([IO.Path]::GetFullPath($reportedAndroidSDK).TrimEnd('\') -ine $androidSDK) {
    throw "Android SDK location is unexpected: $reportedAndroidSDK"
}
$platformToolsProperties = [IO.File]::ReadAllText((Join-Path $androidSDK 'platform-tools\source.properties'))
if ($platformToolsProperties -notmatch '(?m)^Pkg\.Revision=(?<version>\d+\.\d+\.\d+)\r?$') {
    throw 'Android Platform Tools source identity is unexpected.'
}
$platformToolsVersion = [string]$Matches['version']
[Console]::Out.WriteLine("[all-stacks] android-platform-tools: $platformToolsVersion")
$adbVersion = Invoke-SmokeTool 'adb-version' $adb @('version')
Assert-SmokeOutput 'adb-version' $adbVersion 'Android Debug Bridge version 1.0.41'
Assert-SmokeOutput 'adb-version' $adbVersion "Version ${platformToolsVersion}-"
$adbHelp = Invoke-SmokeTool 'adb-wireless-help' $adb @('help')
Assert-SmokeOutput 'adb-wireless-help' $adbHelp 'pair HOST[:PORT]'
Assert-SmokeOutput 'adb-wireless-help' $adbHelp 'connect HOST[:PORT]'
[Console]::Out.WriteLine('[all-stacks] android: command-line SDK, isolated JDK 17, and wireless ADB commands OK')

$nsisRoot = Join-Path ${env:ProgramFiles(x86)} 'NSIS'
if ([IO.Path]::GetFullPath($makensis) -ine [IO.Path]::GetFullPath((Join-Path $nsisRoot 'makensis.exe'))) {
    throw 'NSIS compiler resolved from an unexpected path.'
}
$nsisVersion = Invoke-SmokeTool 'nsis-version' $makensis @('/VERSION')
if ($nsisVersion -notmatch '^v\d+\.\d+(?:\.\d+)?$') { throw "NSIS version is unexpected: $nsisVersion" }
$nsisRootProbe = Join-Path $root 'nsis'
$null = New-Item -ItemType Directory -Path $nsisRootProbe -Force
$nsisScript = Join-Path $nsisRootProbe 'smoke.nsi'
$nsisInstaller = Join-Path $nsisRootProbe 'smoke.exe'
Write-SmokeFile $nsisScript @('Unicode true','Name "Native NSIS Smoke"','OutFile "smoke.exe"','RequestExecutionLevel user','SilentInstall silent','Section "Smoke"','  SetOutPath "$TEMP"','SectionEnd')
$null = Invoke-SmokeTool 'nsis-compile' $makensis @('/WX','/V2','/NOCONFIG',$nsisScript)
$nsisBytes = [IO.File]::ReadAllBytes($nsisInstaller)
if ($nsisBytes.Length -lt 1024 -or $nsisBytes[0] -ne 0x4d -or $nsisBytes[1] -ne 0x5a) {
    throw 'NSIS smoke compiler output is not a Windows executable.'
}
[Console]::Out.WriteLine('[all-stacks] nsis: installer compile OK')

$expectedNu = Join-Path $env:ProgramFiles 'nu\bin\nu.exe'
if ([IO.Path]::GetFullPath($nu) -ine [IO.Path]::GetFullPath($expectedNu)) {
    throw "Nushell command resolved from an unexpected path: $nu"
}
$nuVersion = Invoke-SmokeTool 'nushell-version' $nu @('--version')
if ($nuVersion -notmatch '^\d+\.\d+\.\d+$') { throw "Nushell version is unexpected: $nuVersion" }
[Console]::Out.WriteLine('[all-stacks] nushell: machine MSI command and version OK')

$pythonAliasRoot = 'C:\HerdrSandbox\tools\python\bin'
if ([IO.Path]::GetFullPath($python) -ine (Join-Path $pythonAliasRoot 'python.exe') -or
    [IO.Path]::GetFullPath($python3) -ine (Join-Path $pythonAliasRoot 'python3.exe')) {
    throw 'Python commands resolved from unexpected paths.'
}
$visualStudioRoot = 'C:\HerdrSandbox\toolchains\visual-studio'
if (-not [IO.Path]::GetFullPath($cl).StartsWith($visualStudioRoot + '\', [StringComparison]::OrdinalIgnoreCase) -or
    -not [IO.Path]::GetFullPath($link).StartsWith($visualStudioRoot + '\', [StringComparison]::OrdinalIgnoreCase) -or
    -not [IO.Path]::GetFullPath($msbuild).StartsWith($visualStudioRoot + '\', [StringComparison]::OrdinalIgnoreCase) -or
    [string]::IsNullOrWhiteSpace($env:INCLUDE) -or [string]::IsNullOrWhiteSpace($env:LIB)) {
    throw 'C/C++ toolchain commands or environment are unavailable in the SSH session.'
}
$null = Invoke-SmokeTool 'msbuild-version' $msbuild @('-version','-nologo')
$nativeRoot = Join-Path $root 'native'
$cExecutable = Join-Path $nativeRoot 'smoke-c.exe'
$cppExecutable = Join-Path $nativeRoot 'smoke-cpp.exe'
$null = New-Item -ItemType Directory -Path $nativeRoot -Force
$null = Invoke-SmokeTool 'c-compile' $cl @('/nologo','/W4','/WX','/Z7','/TC','/c','C:\Workspaces\project\smoke.c',"/Fo:$nativeRoot\smoke-c.obj")
$null = Invoke-SmokeTool 'c-link' $link @('/NOLOGO','/DEBUG:NONE',"/OUT:$cExecutable","$nativeRoot\smoke-c.obj")
$cOutput = Invoke-SmokeTool 'c-run' $cExecutable @()
Assert-SmokeOutput 'c-run' $cOutput 'native-c-ok'
$null = Invoke-SmokeTool 'cpp-compile' $cl @('/nologo','/W4','/WX','/Z7','/EHsc','/std:c++20','/TP','/c','C:\Workspaces\project\smoke.cpp',"/Fo:$nativeRoot\smoke-cpp.obj")
$null = Invoke-SmokeTool 'cpp-link' $link @('/NOLOGO','/DEBUG:NONE',"/OUT:$cppExecutable","$nativeRoot\smoke-cpp.obj")
$cppOutput = Invoke-SmokeTool 'cpp-run' $cppExecutable @()
Assert-SmokeOutput 'cpp-run' $cppOutput 'native-cpp-ok'

$javaHome = [IO.Path]::GetFullPath([string]$env:JAVA_HOME).TrimEnd('\')
if ([string]::IsNullOrWhiteSpace($javaHome) -or
    [IO.Path]::GetFullPath($java) -ine (Join-Path $javaHome 'bin\java.exe') -or
    [IO.Path]::GetFullPath($javac) -ine (Join-Path $javaHome 'bin\javac.exe')) {
    throw 'Microsoft OpenJDK JAVA_HOME or commands are unavailable in the SSH session.'
}
$javaVersion = Invoke-SmokeTool 'java-version' $java @('-version')
Assert-SmokeOutput 'java-version' $javaVersion 'Microsoft'
$null = Invoke-SmokeTool 'javac-version' $javac @('-version')
$javaRoot = Join-Path $root 'java'
$null = New-Item -ItemType Directory -Path $javaRoot -Force
$null = Invoke-SmokeTool 'java-compile' $javac @('-d',$javaRoot,'C:\Workspaces\project\Smoke.java')
$javaOutput = Invoke-SmokeTool 'java-run' $java @('-cp',$javaRoot,'Smoke')
Assert-SmokeOutput 'java-run' $javaOutput 'native-java-ok'

$null = Invoke-SmokeTool 'dotnet-version' $dotnet @('--version')
$dotnetRoot = Join-Path $root 'dotnet'
Write-SmokeFile (Join-Path $dotnetRoot 'Smoke.csproj') @('<Project Sdk="Microsoft.NET.Sdk">','  <PropertyGroup>','    <OutputType>Exe</OutputType>','    <TargetFramework>net10.0</TargetFramework>','  </PropertyGroup>','</Project>')
Write-SmokeFile (Join-Path $dotnetRoot 'Program.cs') @('using System;','Console.WriteLine("dotnet-smoke-ok");')
$null = Invoke-SmokeTool 'dotnet-build' $dotnet @('build',(Join-Path $dotnetRoot 'Smoke.csproj'),'--nologo','--verbosity','quiet')
$dotnetOutput = Invoke-SmokeTool 'dotnet-run' $dotnet @((Join-Path $dotnetRoot 'bin\Debug\net10.0\Smoke.dll'))
Assert-SmokeOutput 'dotnet-run' $dotnetOutput 'dotnet-smoke-ok'

$goRoot = Join-Path $root 'go'
Write-SmokeFile (Join-Path $goRoot 'go.mod') @('module smoke','go 1.22')
Write-SmokeFile (Join-Path $goRoot 'main.go') @('package main','import "fmt"','func main() { fmt.Println("go-smoke-ok") }','func add(a, b int) int { return a + b }')
Write-SmokeFile (Join-Path $goRoot 'main_test.go') @('package main','import "testing"','func TestAdd(t *testing.T) { if add(2, 3) != 5 { t.Fatal("bad sum") } }')
$null = Invoke-SmokeTool 'go-version' $go @('version')
Push-Location $goRoot
try {
    $null = Invoke-SmokeTool 'go-test' $go @('test','./...')
    $goOutput = Invoke-SmokeTool 'go-run' $go @('run','.')
} finally { Pop-Location }
Assert-SmokeOutput 'go-run' $goOutput 'go-smoke-ok'

$null = Invoke-SmokeTool 'node-version' $node @('--version')
$nodeFile = Join-Path $root 'node\smoke.js'
Write-SmokeFile $nodeFile @('console.log("node-smoke-ok");')
$nodeOutput = Invoke-SmokeTool 'node-run' $node @($nodeFile)
Assert-SmokeOutput 'node-run' $nodeOutput 'node-smoke-ok'

$playwrightVersions = @(Get-ChildItem -LiteralPath 'C:\HerdrSandbox\tools\playwright' -Directory -Force)
if ($playwrightVersions.Count -ne 1 -or $playwrightVersions[0].Name -notmatch '^\d+\.\d+\.\d+$') {
    throw "Playwright tool version directories are invalid: $($playwrightVersions.Name -join ', ')"
}
$playwrightCLI = Join-Path $playwrightVersions[0].FullName 'node_modules\playwright\cli.js'
if (-not (Test-Path -LiteralPath $playwrightCLI -PathType Leaf)) { throw "Playwright CLI is missing: $playwrightCLI" }
$expectedPlaywrightBrowsers = 'C:\HerdrSandbox\tools\playwright-browsers'
if ($env:PLAYWRIGHT_BROWSERS_PATH -cne $expectedPlaywrightBrowsers) {
    throw "SSH session Playwright browser path is unexpected: $env:PLAYWRIGHT_BROWSERS_PATH"
}
$playwrightScreenshot = Join-Path $root 'node\playwright-chromium.png'
$null = Invoke-SmokeTool 'playwright-chromium' $node @($playwrightCLI, 'screenshot', '-b', 'chromium', 'about:blank', $playwrightScreenshot)
$playwrightScreenshotBytes = [IO.File]::ReadAllBytes($playwrightScreenshot)
if ($playwrightScreenshotBytes.Length -lt 8 -or
    (($playwrightScreenshotBytes[0..7] -join ',') -cne '137,80,78,71,13,10,26,10')) {
    throw 'Playwright Chromium SSH smoke returned an invalid PNG screenshot.'
}
[Console]::Out.WriteLine('[all-stacks] playwright-chromium: headless launch OK')

$playwrightAgentVersion = Invoke-SmokeTool 'playwright-cli-version' $playwrightAgentCLI @('--version')
if ($playwrightAgentVersion -cne '0.1.17') { throw "Playwright CLI version is unexpected: $playwrightAgentVersion" }
$playwrightPowerShellShim = Join-Path (Split-Path -Parent $playwrightAgentCLI) 'playwright-cli.ps1'
if (Test-Path -LiteralPath $playwrightPowerShellShim) {
    throw "Playwright CLI PowerShell shim remains installed: $playwrightPowerShellShim"
}
$playwrightExtensionKey = 'HKLM:\SOFTWARE\Wow6432Node\Microsoft\Edge\Extensions\mmlmfjhmonkocbjadbfplnigmagldckm'
$playwrightExtensionUpdateURL = [string](Get-ItemPropertyValue -LiteralPath $playwrightExtensionKey -Name 'update_url' -ErrorAction Stop)
if ($playwrightExtensionUpdateURL -cne 'https://clients2.google.com/service/update2/crx') {
    throw "Playwright Extension registration is unexpected: $playwrightExtensionUpdateURL"
}
[Console]::Out.WriteLine('[all-stacks] playwright-cli: exact CLI and official extension registration OK')

$tradingViewRoot = 'C:\HerdrSandbox\tools\TradingView.TradingViewDesktop'
$tradingViewExecutables = @(Get-ChildItem -LiteralPath $tradingViewRoot -File -Recurse -Filter 'TradingView.exe')
$tradingViewManifestPath = Join-Path $tradingViewRoot 'AppxManifest.xml'
if ($tradingViewExecutables.Count -ne 1 -or
    [IO.Path]::GetFullPath($tradingView) -ine [IO.Path]::GetFullPath($tradingViewExecutables[0].FullName) -or
    [string]$tradingViewExecutables[0].VersionInfo.FileVersion -notmatch '^\d+\.\d+\.\d+\.\d+$' -or
    -not (Test-Path -LiteralPath $tradingViewManifestPath -PathType Leaf)) {
    throw 'TradingView Desktop portable payload is invalid.'
}
[xml]$tradingViewManifest = [IO.File]::ReadAllText($tradingViewManifestPath)
if ([string]$tradingViewManifest.Package.Identity.Name -cne 'TradingView.Desktop' -or
    [string]$tradingViewManifest.Package.Identity.Version -cne [string]$tradingViewExecutables[0].VersionInfo.FileVersion -or
    [string]$tradingViewManifest.Package.Identity.Publisher -cne 'CN="TradingView, Inc.", O="TradingView, Inc.", S=Ohio, C=US') {
    throw 'TradingView Desktop package identity is invalid.'
}
$tvControlRoot = 'C:\HerdrSandbox\tools\tvcontrol'
$expectedTVCommand = Join-Path $tvControlRoot 'tv.cmd'
$expectedTVControlCommand = Join-Path $tvControlRoot 'tvcontrol.cmd'
if ([IO.Path]::GetFullPath($tv) -ine [IO.Path]::GetFullPath($expectedTVCommand) -or
    [IO.Path]::GetFullPath($tvcontrol) -ine [IO.Path]::GetFullPath($expectedTVControlCommand)) {
    throw 'TVControl commands resolved from unexpected paths.'
}
$tvControlPackagePath = Join-Path $tvControlRoot 'node_modules\@ferroxlabs\tvcontrol\package.json'
$tvControlPackage = [IO.File]::ReadAllText($tvControlPackagePath) | ConvertFrom-Json
$tvBin = [string]$tvControlPackage.bin.tv
$tvControlBin = [string]$tvControlPackage.bin.tvcontrol
if ([string]$tvControlPackage.name -cne '@ferroxlabs/tvcontrol' -or
    [string]$tvControlPackage.version -notmatch '^\d+\.\d+\.\d+$' -or
    [string]::IsNullOrWhiteSpace($tvBin) -or [string]::IsNullOrWhiteSpace($tvControlBin) -or
    $tvBin -ceq $tvControlBin) {
    throw 'TVControl installed package identity is invalid.'
}
$tvHelp = Invoke-SmokeTool 'tvcontrol-help' $tv @('--help')
Assert-SmokeOutput 'tvcontrol-help' $tvHelp 'Usage: tv <command> [options]'
foreach ($shim in @((Join-Path $tvControlRoot 'tv.ps1'), (Join-Path $tvControlRoot 'tvcontrol.ps1'))) {
    if (Test-Path -LiteralPath $shim) { throw "TVControl PowerShell shim remains installed: $shim" }
}
[Console]::Out.WriteLine('[all-stacks] tradingview: portable signed-MSIX payload and direct TVControl commands OK; launch intentionally skipped')

$null = Invoke-SmokeTool 'python-version' $python @('--version')
$pythonFile = Join-Path $root 'python\smoke.py'
Write-SmokeFile $pythonFile @('print("python-smoke-ok")')
$pythonOutput = Invoke-SmokeTool 'python-run' $python @($pythonFile)
Assert-SmokeOutput 'python-run' $pythonOutput 'python-smoke-ok'
$null = Invoke-SmokeTool 'python3-version' $python3 @('--version')
$null = Invoke-SmokeTool 'uv-version' $uv @('--version')
$expectedUvCache = 'C:\HerdrSandbox\cache\uv'
if ($env:UV_CACHE_DIR -cne $expectedUvCache -or $env:UV_NO_MANAGED_PYTHON -cne '1') {
    throw "uv environment is unexpected: cache=$env:UV_CACHE_DIR managed=$env:UV_NO_MANAGED_PYTHON"
}
$uvCache = Invoke-SmokeTool 'uv-cache-dir' $uv @('cache','dir')
if ([IO.Path]::GetFullPath($uvCache).TrimEnd('\') -ine $expectedUvCache) {
    throw "uv cache path is unexpected: $uvCache"
}
$uvRoot = Join-Path $root 'python-ai'
Write-SmokeFile (Join-Path $uvRoot 'pyproject.toml') @('[project]','name = "herdr-python-ai-smoke"','version = "0.0.0"','requires-python = ">=3.13,<3.14"','dependencies = []','','[tool.uv]','package = false')
Write-SmokeFile (Join-Path $uvRoot 'smoke.py') @('print("python-ai-smoke-ok")')
Push-Location $uvRoot
try {
    $null = Invoke-SmokeTool 'uv-sync' $uv @('sync','--offline')
    $uvOutput = Invoke-SmokeTool 'uv-run' $uv @('run','--offline','--frozen','python','smoke.py')
} finally { Pop-Location }
Assert-SmokeOutput 'uv-run' $uvOutput 'python-ai-smoke-ok'
if (-not (Test-Path -LiteralPath (Join-Path $uvRoot 'uv.lock') -PathType Leaf) -or
    -not (Test-Path -LiteralPath (Join-Path $uvRoot '.venv\Scripts\python.exe') -PathType Leaf)) {
    throw 'uv did not create the locked Python 3.13 project environment.'
}

$null = Invoke-SmokeTool 'cargo-version' $cargo @('--version')
$null = Invoke-SmokeTool 'bun-version' $bun @('--version')
$bunOutput = Invoke-SmokeTool 'bun-run' $bun @('-e',"console.log('bun-smoke-ok')")
Assert-SmokeOutput 'bun-run' $bunOutput 'bun-smoke-ok'
$null = Invoke-SmokeTool 'cargo-nextest-version' $nextest @('--version')
$null = Invoke-SmokeTool 'just-version' $just @('--version')
$null = Invoke-SmokeTool 'rustc-version' $rustc @('--version')
$null = Invoke-SmokeTool 'sh-version' $sh @('--version')
$shellOutput = Invoke-SmokeTool 'sh-run' $sh @('-lc','printf sh-smoke-ok')
Assert-SmokeOutput 'sh-run' $shellOutput 'sh-smoke-ok'
$justRoot = 'C:\Workspaces\project'
Push-Location $justRoot
try {
    $justOutput = Invoke-SmokeTool 'herdr-just-toolchain' $just @('herdr-toolchain-smoke')
} finally { Pop-Location }
Assert-SmokeOutput 'herdr-just-toolchain' $justOutput 'python3-just-ok'
Assert-SmokeOutput 'herdr-just-toolchain' $justOutput 'bun-just-ok'
$expectedLibghosttyOutput = 'C:\HerdrSandbox\build\cargo-target\zig-out'
if ($env:LIBGHOSTTY_VT_ZIG_OUT_DIR -cne $expectedLibghosttyOutput -or
    -not (Test-Path -LiteralPath $expectedLibghosttyOutput -PathType Container)) {
    throw "Herdr libghostty output environment is unavailable: $env:LIBGHOSTTY_VT_ZIG_OUT_DIR"
}
$rustRoot = Join-Path $root 'rust'
$rustSource = Join-Path $rustRoot 'main.rs'
$rustBinary = Join-Path $rustRoot 'smoke-rust.exe'
Write-SmokeFile $rustSource @('fn main() { println!("rust-smoke-ok"); }')
$null = Invoke-SmokeTool 'rust-compile' $rustc @($rustSource,'-o',$rustBinary)
$rustOutput = Invoke-SmokeTool 'rust-run' $rustBinary @()
Assert-SmokeOutput 'rust-run' $rustOutput 'rust-smoke-ok'

$cmakeVersion = Invoke-SmokeTool 'handy-cmake-version' $cmake @('--version')
Assert-SmokeOutput 'handy-cmake-version' $cmakeVersion 'cmake version '
$null = Invoke-SmokeTool 'handy-glslc-version' $glslc @('--version')
$expectedVulkanRoot = 'C:\VulkanSDK\1.4.309.0'
$expectedHandyPrefix = 'C:\HerdrSandbox\tools\handy-cmake-prefix'
$handyConfig = Join-Path $expectedHandyPrefix 'share\cmake\SPIRV-Headers\SPIRV-HeadersConfig.cmake'
if ($env:VULKAN_SDK -cne $expectedVulkanRoot -or
    @($env:CMAKE_PREFIX_PATH -split ';')[0] -cne $expectedHandyPrefix -or
    -not (Test-Path -LiteralPath $handyConfig -PathType Leaf) -or
    -not ([IO.File]::ReadAllText($handyConfig).Contains(($expectedVulkanRoot + '/Include').Replace('\','/')))) {
    throw 'Handy Vulkan SDK or corrected SPIRV-Headers package is unavailable.'
}
$webViewKey = 'HKLM:\SOFTWARE\WOW6432Node\Microsoft\EdgeUpdate\Clients\{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}'
$webView = Get-ItemProperty -LiteralPath $webViewKey -ErrorAction Stop
$webViewExecutable = Join-Path (Join-Path ([string]$webView.location) ([string]$webView.pv)) 'msedgewebview2.exe'
$webViewSignature = Get-AuthenticodeSignature -LiteralPath $webViewExecutable
if ([string]$webView.name -cne 'Microsoft Edge WebView2 Runtime' -or
    -not (Test-Path -LiteralPath $webViewExecutable -PathType Leaf) -or
    $webViewSignature.Status -ne [System.Management.Automation.SignatureStatus]::Valid -or
    $webViewSignature.SignerCertificate.Subject -notmatch '(^|,\s*)O=Microsoft Corporation(,|$)') {
    throw 'Handy WebView2 Runtime is unavailable or untrusted.'
}
[Console]::Out.WriteLine('[all-stacks] handy-native-toolchain: CMake, Vulkan 1.4.309.0, SPIRV-Headers, and WebView2 OK')

$null = Invoke-SmokeTool 'zig-version' $zig @('version')
$zigSource = Join-Path $root 'zig\smoke.zig'
Write-SmokeFile $zigSource @('const std = @import("std");','test "addition" {','    try std.testing.expect(2 + 2 == 4);','}')
$null = Invoke-SmokeTool 'zig-test' $zig @('test',$zigSource)

$terminalSettingsPath = Join-Path $env:LOCALAPPDATA 'Packages\Microsoft.WindowsTerminal_8wekyb3d8bbwe\LocalState\settings.json'
if (-not (Test-Path -LiteralPath $terminalSettingsPath -PathType Leaf)) { throw 'Windows Terminal settings were not copied.' }
$terminalSettings = [IO.File]::ReadAllText($terminalSettingsPath) | ConvertFrom-Json
$powerShellGUID = '{574e775e-4f2a-5b96-ac1e-a2962a402336}'
$powerShellProfiles = @($terminalSettings.profiles.list | Where-Object { [string]$_.guid -ieq $powerShellGUID })
if ([string]$terminalSettings.theme -cne 'light' -or [string]$terminalSettings.defaultProfile -ine $powerShellGUID -or
    [string]$terminalSettings.profiles.defaults.colorScheme -cne 'Herdr Native Light' -or
    [string]$terminalSettings.profiles.defaults.font.face -cne 'GeistMono Nerd Font' -or
    $powerShellProfiles.Count -ne 1 -or [string]$powerShellProfiles[0].commandline -cne 'pwsh.exe' -or
    [string]$powerShellProfiles[0].font.face -cne 'GeistMono Nerd Font' -or
    @($terminalSettings.schemes | Where-Object { [string]$_.name -ceq 'Herdr Native Light' }).Count -ne 1) {
    throw 'Windows Terminal theme, default profile, or font does not match the transferred configuration.'
}
$starshipConfigPath = Join-Path $env:USERPROFILE '.config\starship.toml'
if (-not (Test-Path -LiteralPath $starshipConfigPath -PathType Leaf)) { throw 'Starship configuration is missing.' }
$starshipConfig = [IO.File]::ReadAllText($starshipConfigPath)
if ($starshipConfig -notmatch "(?m)^palette = 'catppuccin_latte'\r?$" -or $starshipConfig -match "(?m)^palette = 'catppuccin_mocha'\r?$") {
    throw 'Starship did not retain the Catppuccin Latte preset selected by the light Terminal theme.'
}
$starship = (Get-Command 'starship.exe' -CommandType Application -ErrorAction Stop | Select-Object -First 1).Source
$previousStarshipConfig = $env:STARSHIP_CONFIG
try {
    $env:STARSHIP_CONFIG = $starshipConfigPath
    $null = Invoke-SmokeTool 'starship-prompt' $starship @('prompt')
} finally { $env:STARSHIP_CONFIG = $previousStarshipConfig }

Remove-Item -LiteralPath $root -Recurse -Force
[Console]::Out.WriteLine('[all-stacks] PASS: opensrc, Android, Audio, C/C++, Java, Nushell, dotnet, go, node, Handy and Herdr virtual stacks')
[Console]::Out.WriteLine('[all-stacks] PASS: Windows Terminal light chrome and color scheme, PowerShell 7, GeistMono Nerd Font, Catppuccin Latte Starship')
