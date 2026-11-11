# Read-only prerequisites and post-install effects. Never installs or repairs.
function Get-InstallerBaseline {
    return [ordered]@{
        schemaVersion = 1
        winget = $null -ne (Get-Command winget.exe -CommandType Application -ErrorAction SilentlyContinue)
        powershell7 = $null -ne (Get-Command pwsh.exe -CommandType Application -ErrorAction SilentlyContinue)
        vcRuntime = [int][Microsoft.Win32.Registry]::GetValue('HKEY_LOCAL_MACHINE\SOFTWARE\Microsoft\VisualStudio\14.0\VC\Runtimes\x64','Installed',0) -eq 1
    }
}

function Invoke-InstallerProbe {
    param([string]$File, [string[]]$Arguments, [string]$Directory = $env:TEMP, [int]$Seconds = 45, [switch]$TerminateDescendantsAfterRootExit)
    $spec = New-Object HerdrSandbox.ProvisioningProcessSpec
    $spec.Role = 'Independent installer capability verification'
    $spec.FilePath = @(Get-Command $File -CommandType Application -ErrorAction Stop)[0].Source
    $spec.Arguments = $Arguments
    $spec.WorkingDirectory = $Directory
    $spec.TimeoutMilliseconds = $Seconds * 1000
    $spec.SuccessExitCodes = [int[]]@(0)
    $spec.TerminateDescendantsAfterRootExit = $TerminateDescendantsAfterRootExit.IsPresent
    $result = [HerdrSandbox.ProvisioningProcess]::Run($spec)
    if (-not $result.Succeeded -or $result.OutputTruncated) { throw 'Installed capability failed its independent probe.' }
    return ([string]$result.Output).Trim()
}

function Test-InstallerEffects {
    param($Profile, $Report, [string]$ExpectedProfileHash, [string]$ProbeRoot, [string]$StateRoot)
    $checks = New-Object 'Collections.Generic.List[object]'
    $checks.Add([ordered]@{ name='native report identity'; passed=($Report.schemaVersion -eq 1 -and [string]$Report.profileSha256 -ceq $ExpectedProfileHash) })
    $env:Path = [Environment]::GetEnvironmentVariable('Path','Machine') + ';' + [Environment]::GetEnvironmentVariable('Path','User')
    foreach ($name in @('RUSTUP_HOME','CARGO_HOME','JAVA_HOME','ANDROID_HOME','ANDROID_SDK_ROOT','ANDROID_USER_HOME','PLAYWRIGHT_BROWSERS_PATH','UV_CACHE_DIR','ZIG_LOCAL_CACHE_DIR','ZIG_GLOBAL_CACHE_DIR','CMAKE_PREFIX_PATH','INCLUDE','LIB','LIBPATH')) {
        $value = [Environment]::GetEnvironmentVariable($name,'User')
        if ($null -ne $value) { [Environment]::SetEnvironmentVariable($name,$value,'Process') }
    }
    $wingetVersion = ''
    try { $wingetVersion = Invoke-InstallerProbe 'winget.exe' @('--version'); $checks.Add([ordered]@{name='WinGet command';passed=$true}) }
    catch { $checks.Add([ordered]@{name='WinGet command';passed=$false}) }
    $portableCommands = @{'Starship.Starship'='starship.exe';'junegunn.fzf'='fzf.exe';'BurntSushi.ripgrep.MSVC'='rg.exe';'rhysd.actionlint'='actionlint.exe';'Anthropic.ClaudeCode'='claude.exe';'GitHub.Copilot'='copilot.exe';'OpenAI.Codex'='codex.exe'}
    foreach ($package in @($Profile.packages)) {
        $passed = $false
        try {
            if ($package.id -eq 'Anomaly.OpenCode') {
                $passed = (Invoke-InstallerProbe 'opencode.exe' @('--version')).Trim() -match '^1\.\d+\.\d+$'
            } elseif ($portableCommands.ContainsKey([string]$package.id)) {
                $arguments = if ($package.id -eq 'rhysd.actionlint') { @('-version') } else { @('--version') }
                $passed = -not [string]::IsNullOrWhiteSpace((Invoke-InstallerProbe $portableCommands[[string]$package.id] $arguments))
            } elseif ($package.id -eq 'Voidstar.FilePilot') {
                $command = Get-Command 'FPilot.exe' -CommandType Application -ErrorAction Stop | Select-Object -First 1
                $item = Get-Item -LiteralPath $command.Source -Force
                $passed = -not $item.PSIsContainer -and ($item.Attributes -band [IO.FileAttributes]::ReparsePoint) -eq 0
            } else {
                $null = Invoke-InstallerProbe 'winget.exe' @('list','--id',[string]$package.id,'--exact','--source','winget','--accept-source-agreements','--disable-interactivity')
                $passed = $true
            }
        } catch {}
        $checks.Add([ordered]@{name=('package '+[string]$package.id);passed=$passed})
    }
    $commands = @{}
    $packageCommands = @{'Microsoft.PowerShell'='pwsh.exe';'Nushell.Nushell'='nu.exe';'Starship.Starship'='starship.exe';'Git.Git'='git.exe';'GitHub.cli'='gh.exe';'junegunn.fzf'='fzf.exe';'BurntSushi.ripgrep.MSVC'='rg.exe';'rhysd.actionlint'='actionlint.exe';'Anomaly.OpenCode'='opencode.exe'}
    foreach ($package in @($Profile.packages)) { if ($packageCommands.ContainsKey([string]$package.id)) { $commands[$packageCommands[[string]$package.id]]=$true } }
    $stackCommands = @{'python'=@('python.exe');'python-ai'=@('python.exe','uv.exe');'uv'=@('uv.exe');'node'=@('node.exe');'hyperframes'=@('node.exe','ffmpeg.exe');'playwright-cli'=@('node.exe');'tradingview'=@('node.exe');'go'=@('go.exe');'rust-msvc'=@('rustc.exe','cargo.exe');'herdr'=@('rustc.exe','cargo.exe','zig.exe','bun.exe','python.exe','just.exe');'handy'=@('rustc.exe','cargo.exe','bun.exe','cmake.exe');'cpp'=@('cmake.exe');'dotnet'=@('dotnet.exe');'java'=@('java.exe');'nsis'=@('makensis.exe');'zig'=@('zig.exe');'bun'=@('bun.exe');'just'=@('just.exe');'cargo-nextest'=@('cargo-nextest.exe');'opensrc'=@('opensrc.exe');'android'=@('java.exe','adb.exe')}
    foreach ($stack in @($Profile.stacks)) { foreach ($command in @($stackCommands[[string]$stack])) { if ($command) { $commands[$command]=$true } } }
    foreach ($name in @($commands.Keys | Sort-Object)) {
        $arguments = if ($name -eq 'go.exe') { @('version') } elseif ($name -eq 'zig.exe') { @('version') } elseif ($name -eq 'makensis.exe') { @('/VERSION') } elseif ($name -eq 'ffmpeg.exe') { @('-version') } elseif ($name -eq 'adb.exe') { @('version') } else { @('--version') }
        $passed = $false
        try { $text = Invoke-InstallerProbe $name $arguments; $passed = -not [string]::IsNullOrWhiteSpace($text) } catch {}
        $checks.Add([ordered]@{name=('command '+$name);passed=$passed})
    }
    if ($commands.ContainsKey('go.exe')) {
        $passed=$false
        try {
            $env:GOMAXPROCS=[string][Environment]::ProcessorCount
            $env:GOFLAGS='-p='+[Environment]::ProcessorCount
            $env:GOCACHE=Join-Path $ProbeRoot 'go-cache'
            $file=Join-Path $ProbeRoot 'main.go'
            [IO.File]::WriteAllText($file, 'package main; import "fmt"; func main(){fmt.Print("installer-go-ok")}',(New-Object Text.UTF8Encoding($false)))
            $text=Invoke-InstallerProbe 'go.exe' @('run',$file) $ProbeRoot 180
            $passed=$text -ceq 'installer-go-ok'
        } catch {}
        $checks.Add([ordered]@{name='Go compiler and executable';passed=$passed})
    }
    if ($commands.ContainsKey('python.exe')) {
        $passed=$false
        try { $text=Invoke-InstallerProbe 'python.exe' @('-I','-c','import ssl,sqlite3,ctypes,venv; print("installer-python-ok")');$passed=$text -ceq 'installer-python-ok' } catch {}
        $checks.Add([ordered]@{name='Python standard-library/native modules';passed=$passed})
    }
    if ($commands.ContainsKey('makensis.exe')) {
        $passed=$false
        try {
            $file=Join-Path $ProbeRoot 'probe.nsi'
            $source='Unicode true' + "`n" + 'Name "Installer acceptance"' + "`n" + 'OutFile "probe.exe"' + "`n" + 'RequestExecutionLevel user' + "`n" + 'SilentInstall silent' + "`n" + 'Section' + "`n" + 'FileOpen $0 "$EXEDIR\effect.txt" w' + "`n" + 'FileWrite $0 "installer-nsis-ok"' + "`n" + 'FileClose $0' + "`n" + 'SectionEnd' + "`n"
            [IO.File]::WriteAllText($file,$source,(New-Object Text.UTF8Encoding($false)))
            $null=Invoke-InstallerProbe 'makensis.exe' @('/WX','/V2','/NOCONFIG',$file) $ProbeRoot 120
            $null=Invoke-InstallerProbe (Join-Path $ProbeRoot 'probe.exe') @('/S') $ProbeRoot 30
            $passed=[IO.File]::ReadAllText((Join-Path $ProbeRoot 'effect.txt')) -ceq 'installer-nsis-ok'
        } catch {}
        $checks.Add([ordered]@{name='NSIS compile and installed effect';passed=$passed})
    }
    if ($commands.ContainsKey('rustc.exe')) {
        $passed=$false
        try {
            $file=Join-Path $ProbeRoot 'main.rs'
            [IO.File]::WriteAllText($file,'fn main(){println!("installer-rust-ok");}',(New-Object Text.UTF8Encoding($false)))
            $executable=Join-Path $ProbeRoot 'rust-probe.exe'
            # rustc finishes compilation/linking before returning; its owned MSVC
            # telemetry uploader is not part of the compiler capability contract.
            $null=Invoke-InstallerProbe 'rustc.exe' @($file,'-o',$executable) $ProbeRoot 180 -TerminateDescendantsAfterRootExit
            $passed=(Invoke-InstallerProbe $executable @() $ProbeRoot 30) -ceq 'installer-rust-ok'
        } catch {}
        $checks.Add([ordered]@{name='Rust compile, link and executable';passed=$passed})
    }
    if (@($Profile.stacks | Where-Object { $_ -in @('cpp','handy') }).Count -gt 0) {
        $passed=$false
        try {
            $file=Join-Path $ProbeRoot 'main.cpp'
            [IO.File]::WriteAllText($file,'#include <iostream>' + "`n" + '#include <string>' + "`n" + 'int main(){std::cout << std::string("installer-cpp-ok");}',(New-Object Text.UTF8Encoding($false)))
            [IO.File]::WriteAllText((Join-Path $ProbeRoot 'CMakeLists.txt'),'cmake_minimum_required(VERSION 3.20)' + "`n" + 'project(installer_probe CXX)' + "`n" + 'add_executable(cpp-probe main.cpp)' + "`n",(New-Object Text.UTF8Encoding($false)))
            $build=Join-Path $ProbeRoot 'cpp-build'
            $null=Invoke-InstallerProbe 'cmake.exe' @('-S',$ProbeRoot,'-B',$build,'-A','x64') $ProbeRoot 180
            $null=Invoke-InstallerProbe 'cmake.exe' @('--build',$build,'--config','Release','--parallel',[string][Environment]::ProcessorCount) $ProbeRoot 180
            $executable=Join-Path $build 'Release\cpp-probe.exe'
            $passed=(Invoke-InstallerProbe $executable @() $ProbeRoot 30) -ceq 'installer-cpp-ok'
        } catch {}
        $checks.Add([ordered]@{name='C++ compiler, SDK, linker and executable';passed=$passed})
    }
    if (@($Profile.stacks | Where-Object { $_ -eq 'audio' }).Count -gt 0) {
        $passed=$false
        try {
            $null=Invoke-InstallerProbe 'winget.exe' @('list','--id','Cockos.REAPER','--exact','--source','winget','--accept-source-agreements','--disable-interactivity')
            $audioRoot=Join-Path $StateRoot 'tools\AudioGridder'
            $items=@(Get-ChildItem -LiteralPath $audioRoot -Recurse -Force -ErrorAction Stop)
            $servers=@($items|Where-Object{-not $_.PSIsContainer -and $_.Name -ceq 'AudioGridderServer.exe'})
            $passed=$servers.Count -eq 1 -and @($items|Where-Object{($_.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0}).Count -eq 0
        } catch {}
        $checks.Add([ordered]@{name='REAPER registration and AudioGridder payload';passed=$passed})
    }
    $roots = @{
        herdr=(Join-Path $env:APPDATA 'herdr'); opencode=(Join-Path $env:USERPROFILE '.config\opencode');
        claude=(Join-Path $env:USERPROFILE '.claude');'claude-state'=$env:USERPROFILE;codex=(Join-Path $env:USERPROFILE '.codex');pi=(Join-Path $env:USERPROFILE '.pi\agent');
        copilot=(Join-Path $env:USERPROFILE '.copilot');'agent-skills'=(Join-Path $env:USERPROFILE '.agents\skills');
        'github-cli'=$(if($env:GH_CONFIG_DIR){$env:GH_CONFIG_DIR}elseif($env:XDG_CONFIG_HOME){Join-Path $env:XDG_CONFIG_HOME 'gh'}else{Join-Path $env:APPDATA 'GitHub CLI'});
        git=$env:USERPROFILE;'git-config'=(Join-Path $env:USERPROFILE '.config\git');zed=(Join-Path $env:APPDATA 'Zed');
        starship=(Join-Path $env:USERPROFILE '.config');nushell=(Join-Path $env:APPDATA 'nushell');
        terminal=(Join-Path $env:LOCALAPPDATA 'Packages\Microsoft.WindowsTerminal_8wekyb3d8bbwe\LocalState');
        'terminal-preview'=(Join-Path $env:LOCALAPPDATA 'Packages\Microsoft.WindowsTerminalPreview_8wekyb3d8bbwe\LocalState');
        'terminal-fragments'=(Join-Path $env:LOCALAPPDATA 'Microsoft\Windows Terminal\Fragments')
    }
    if ($env:XDG_CONFIG_HOME) { $roots.opencode=Join-Path $env:XDG_CONFIG_HOME 'opencode' }
    $configurationPassed=$true
    foreach ($file in @($Profile.files)) {
        try {
            if ($file.kind -eq 'configuration-archive') {
                $result = [IO.File]::ReadAllText((Join-Path $StateRoot 'configuration-result.json')) | ConvertFrom-Json
                if ($result.schemaVersion -ne 9 -or $result.archiveSha256 -cne $file.sha256 -or -not $result.herdrConfigurationPublished) { throw 'Shared configuration publication did not match the private snapshot.' }
                continue
            }
            if (-not $roots.ContainsKey([string]$file.kind)) {throw 'Unsupported configuration destination.'}
            $path=Join-Path $roots[[string]$file.kind] ([string]$file.path).Replace('/','\')
            $item=Get-Item -LiteralPath $path -Force -ErrorAction Stop
            if ($item.PSIsContainer -or ($item.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0 -or (Get-FileHash -LiteralPath $path -Algorithm SHA256).Hash -ine [string]$file.sha256) {throw 'Configuration did not match.'}
        } catch {$configurationPassed=$false}
    }
    $checks.Add([ordered]@{name='selected authored configuration hashes';passed=$configurationPassed})
    $herdrVersion='';$herdrClient=$null
    if (@($Profile.packages | Where-Object { [string]$_.id -ieq 'hdosys.herdr-win' }).Count -gt 0) {
        $passed=$false
        try {
            $command=@(Get-Command herdr.exe -CommandType Application -ErrorAction Stop)[0]
            $herdrVersion=Invoke-InstallerProbe $command.Source @('--version')
            $herdrClient=(Invoke-InstallerProbe $command.Source @('status','client','--json'))|ConvertFrom-Json
            $binary=Get-Item -LiteralPath ([string]$herdrClient.binary) -Force
            $passed=($herdrVersion -match '^herdr-ext ' -and -not $binary.PSIsContainer -and ($binary.Attributes -band [IO.FileAttributes]::ReparsePoint) -eq 0)
            $null=Invoke-InstallerProbe $command.Source @('config','check')
        } catch {$passed=$false}
        $checks.Add([ordered]@{name='installer-owned Herdr and configuration';passed=$passed})
    }
    return [pscustomobject]@{ checks=@($checks.ToArray()); wingetVersion=$wingetVersion; herdrVersion=$herdrVersion; herdrClient=$herdrClient }
}
