package sandbox

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestTradingViewStackOwnsExactDesktopAndGuestLocalTVControl(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "provisioning", stackProvisioningName))
	if err != nil {
		t.Fatal(err)
	}
	source := string(data)
	start := strings.Index(source, "function Get-TradingViewDesktopPortableMetadata")
	install := strings.Index(source, "function Install-TradingViewStack")
	end := strings.Index(source, "function Resolve-StackPythonPackage")
	if start < 0 || install <= start || end <= install {
		t.Fatal("TradingView stack function boundary is missing")
	}
	block := source[start:end]
	for _, required := range []string{
		"function Get-TradingViewDesktopPortableMetadata",
		"TradingView.TradingViewDesktop",
		"Search-ProvisioningWinGetPackages",
		"raw.githubusercontent.com/microsoft/winget-pkgs/master/",
		"TradingView.TradingViewDesktop.installer.yaml",
		"PackageFamilyName",
		"InstallerSha256",
		"SignatureSha256",
		"-DownloadSource 'Direct' -Adapter 'Portable'",
		"-PortableVersionSource 'File'",
		"-RequireAuthenticodeSignature",
		"AppxManifest.xml",
		"Wait-ProvisioningCommandAvailable -Role 'TradingView Desktop command' -Name 'TradingView.exe'",
		"Ensure-ProvisioningStartShortcut -DisplayName 'TradingView' -Executable $desktopExecutable",
		"-ShortcutArguments '--remote-debugging-port=9222'",
		"function Install-TradingViewActiveSessionLauncher",
		"active-session-launch.ps1",
		"const activeSessionLauncher = 'C:\\\\HerdrSandbox\\\\tools\\\\tvcontrol\\\\active-session-launch.ps1';",
		"timeout: 60000",
		"b52a90e3fcb5b62a1474865c1818bacae3ef942dfe058b59c3b8574adeae4cbb",
		"Install-TradingViewActiveSessionLauncher -TVControlRoot $tvControlRoot",
		"Install-NodeRuntime -Version $NodeVersion",
		"@ferroxlabs/tvcontrol@latest",
		"@ferroxlabs/tvcontrol@$TVControlVersion",
		"$toolRoot = $tvControlRoot",
		"'--ignore-scripts'",
		"'--omit=optional'",
		"$tvBin = [string]$package.bin.tv",
		"$tvControlBin = [string]$package.bin.tvcontrol",
		"foreach ($bin in @($tvBin, $tvControlBin))",
		"$tvCLIEntryPath = [IO.Path]::GetFullPath",
		"$tvControlEntryPath = [IO.Path]::GetFullPath",
		"foreach ($cliEntryPath in @($tvCLIEntryPath, $tvControlEntryPath))",
		"-ArgumentList @($tvCLIEntryPath, '--help')",
		"'tv.cmd'",
		"'tvcontrol.cmd'",
		"'tv.ps1'",
		"'tvcontrol.ps1'",
		"Usage: tv <command> [options]",
		"managed shortcut and TVControl active-session launcher enable local CDP on port 9222",
	} {
		if !strings.Contains(block, required) {
			t.Fatalf("TradingView stack is missing %q", required)
		}
	}
	for _, forbidden := range []string{
		"Start-Process", "taskkill", "launch_tv_debug.bat",
		"Install-NodeStack", "npm ci", "npm link", "ProjectDirectory",
		"Join-Path $tvControlRoot $TVControlVersion",
		"$tvBin -cne $tvControlBin",
		"-ArgumentList @($cliEntryPath, '--help')",
		"Get-AppxPackage", "Add-AppxPackage", "-Adapter 'MSIX'", "$minimumWindowsBuild",
		"3.3.0.7992",
	} {
		if strings.Contains(block, forbidden) {
			t.Fatalf("TradingView stack contains forbidden runtime/project path %q", forbidden)
		}
	}
	commandIndex := strings.Index(block, "Wait-ProvisioningCommandAvailable -Role 'TradingView Desktop command'")
	shortcutIndex := strings.Index(block, "Ensure-ProvisioningStartShortcut -DisplayName 'TradingView'")
	if commandIndex < 0 || shortcutIndex <= commandIndex {
		t.Fatalf("TradingView shortcut is not created after executable verification: command=%d shortcut=%d", commandIndex, shortcutIndex)
	}
	if strings.Count(block, "--remote-debugging-port=9222") != 1 {
		t.Fatalf("TradingView stack does not assign exactly one fixed CDP shortcut argument")
	}
	packageIdentityIndex := strings.Index(block, "TVControl package identity does not match exact version")
	launcherIndex := strings.Index(block, "Install-TradingViewActiveSessionLauncher -TVControlRoot $tvControlRoot")
	cliEntryIndex := strings.Index(block, "$packageRootPath = [IO.Path]::GetFullPath($packageDirectory)")
	if packageIdentityIndex < 0 || launcherIndex <= packageIdentityIndex || cliEntryIndex <= launcherIndex {
		t.Fatalf("active-session launcher order = package identity %d, launcher %d, CLI entries %d", packageIdentityIndex, launcherIndex, cliEntryIndex)
	}
}

func TestTradingViewActiveSessionLauncherIsBoundedPowerShell51(t *testing.T) {
	if err := validateActiveSessionLaunchScript(activeSessionLaunchScript); err != nil {
		t.Fatal(err)
	}
	if err := validateActiveSessionLaunchScript([]byte("Write-Output 'old'")); err == nil {
		t.Fatal("active-session launcher accepted a missing contract")
	}
	source := string(activeSessionLaunchScript)
	for _, required := range []string{
		"Global\\HerdrSandbox.TradingViewActiveSessionLaunch",
		"Get-NetTCPConnection -State Listen -LocalPort $Port",
		"User-Agent' -notmatch '\\bTVDesktop/\\d+'",
		"$definition.Principal.LogonType = 3",
		"$definition.Settings.ExecutionTimeLimit = 'PT1M'",
		"-NoLogo -NoProfile -NonInteractive -WindowStyle Hidden -EncodedCommand",
		"TradingView is already running in interactive session",
		"Stop-HerdrTradingViewProcessTree",
		"AddSeconds(20)",
	} {
		if !strings.Contains(source, required) {
			t.Fatalf("active-session launcher is missing %q", required)
		}
	}
	for _, forbidden := range []string{"pwsh.exe", "taskkill", ".cmd", ".bat", "kill-existing"} {
		if strings.Contains(strings.ToLower(source), strings.ToLower(forbidden)) {
			t.Fatalf("active-session launcher contains forbidden path %q", forbidden)
		}
	}
	assertPowerShell51Parses(t, source)
}

func TestTradingViewActiveSessionTaskCleansTreeWhenPIDPublicationFails(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows PowerShell 5.1 active-session task fault injection")
	}
	assetPath := filepath.Join("assets", activeSessionLaunchName)
	functionSetup := provisioningPowerShellFunctionSetup(t, provisioningPowerShellFunctionSource{
		path: assetPath, names: []string{"Publish-HerdrActiveSessionLaunchStatusScript"},
	})
	statusPath := filepath.Join(t.TempDir(), "status.json")
	quote := func(value string) string { return strings.ReplaceAll(value, "'", "''") }
	script := fmt.Sprintf(`$ErrorActionPreference = 'Stop'
%s
$expectedExecutable = 'C:\HerdrSandbox\tools\TradingView.TradingViewDesktop\TradingView.exe'
$expectedSession = [int][Diagnostics.Process]::GetCurrentProcess().SessionId
$taskScript = Publish-HerdrActiveSessionLaunchStatusScript -LaunchID '0123456789abcdef0123456789abcdef' -TaskName 'HerdrSandbox-TradingViewLaunch-0123456789abcdef0123456789abcdef' -StatusPath '%s' -ExpectedExecutable $expectedExecutable -Argument '--remote-debugging-port=9222' -SessionID $expectedSession
if ($taskScript.Contains('DeleteTask')) { throw 'Task child still owns task-definition cleanup.' }
$taskScript = $taskScript.Replace('    exit 1', "    throw 'TASK_EXIT_1'")
$script:harnessPID = $PID
$script:stopped = New-Object 'System.Collections.Generic.HashSet[int]'
$script:convertCalls = 0
$script:startCalls = 0
$script:cleanupRows = 0
$script:capturedMessage = ''
function ConvertTo-Json {
    param([Parameter(ValueFromPipeline = $true)]$InputObject, [switch]$Compress)
    process {
        $script:convertCalls += 1
        if ($script:convertCalls -eq 1) { throw 'injected status publication failure' }
        $script:capturedMessage = [string]$InputObject.message
        return '{}'
    }
}
function Get-Process {
    param([string]$Name, [int]$Id, [object]$ErrorAction)
    if (-not $PSBoundParameters.ContainsKey('Id')) { return @() }
    if ($Id -eq $script:harnessPID) { return [pscustomobject]@{ Id = $Id; SessionId = $expectedSession; Path = 'powershell.exe' } }
    if ($script:stopped.Contains($Id)) { return $null }
    if ($Id -in @(101, 102)) { return [pscustomobject]@{ Id = $Id; SessionId = $expectedSession; Path = $expectedExecutable } }
    return $null
}
function Start-Process {
    param([string]$FilePath, [object[]]$ArgumentList, [string]$WorkingDirectory, [switch]$PassThru)
    $script:startCalls += 1
    return [pscustomobject]@{ Id = 101; SessionId = $expectedSession; Path = $expectedExecutable }
}
function Get-CimInstance {
    param([string]$ClassName, [object]$ErrorAction)
    $script:cleanupRows += 1
    return @([pscustomobject]@{ ProcessId = 101; ParentProcessId = 1 }, [pscustomobject]@{ ProcessId = 102; ParentProcessId = 101 })
}
function Stop-Process {
    param([Parameter(ValueFromPipeline = $true)]$InputObject, [switch]$Force)
    process { $script:stopped.Add([int]$InputObject.Id) | Out-Null }
}
function Start-Sleep { param([int]$Milliseconds) }
$failed = $false
try { Invoke-Expression $taskScript } catch {
    if ([string]$_.Exception.Message -cne 'TASK_EXIT_1') { throw }
    $failed = $true
}
if (-not $failed -or $script:convertCalls -ne 2 -or -not $script:stopped.Contains(101) -or -not $script:stopped.Contains(102)) {
    throw "Status-publication fault did not clean the full process tree: stopped=$($script:stopped -join ',') startCalls=$script:startCalls cleanupRows=$script:cleanupRows message=$script:capturedMessage"
}
`, functionSetup, quote(statusPath))
	command := hiddenCommand(mustWindowsPowerShellPath(t), "-NoLogo", "-NoProfile", "-NonInteractive", "-WindowStyle", "Hidden", "-EncodedCommand", encodePowerShell(script))
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("active-session task cleanup fault injection: %v: %s", err, output)
	}
}

func TestInstallTradingViewActiveSessionLauncherPatchesExactOwnerIdempotently(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows PowerShell 5.1 active-session patch regression")
	}
	root := t.TempDir()
	tvControlRoot := filepath.Join(root, "tvcontrol")
	packageDirectory := filepath.Join(tvControlRoot, "node_modules", "@ferroxlabs", "tvcontrol")
	healthDirectory := filepath.Join(packageDirectory, "src", "core")
	if err := os.MkdirAll(healthDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	launcherSource := filepath.Join(root, activeSessionLaunchName)
	if err := os.WriteFile(launcherSource, activeSessionLaunchScript, 0o600); err != nil {
		t.Fatal(err)
	}
	healthPath := filepath.Join(healthDirectory, "health.js")
	healthFixture := "export async function launch() {\n  if (killFirst) await killExisting();\n\n  const args = [`--remote-debugging-port=${cdpPort}`];\n}\n"
	if err := os.WriteFile(healthPath, []byte(healthFixture), 0o600); err != nil {
		t.Fatal(err)
	}
	functionSetup := provisioningPowerShellFunctionSetup(t, provisioningPowerShellFunctionSource{
		path: defaultProvisioningPath(t, stackProvisioningName), names: []string{"Install-TradingViewActiveSessionLauncher"},
	})
	quote := func(value string) string { return strings.ReplaceAll(value, "'", "''") }
	script := fmt.Sprintf(`$ErrorActionPreference = 'Stop'
%s
$root = '%s'
$packageDirectory = '%s'
$launcherSource = '%s'
$healthPath = Join-Path $packageDirectory 'src\core\health.js'
$fixtureSource = [IO.File]::ReadAllText($healthPath)
$sha256 = [Security.Cryptography.SHA256]::Create()
try {
    $fixtureSHA256 = ([BitConverter]::ToString($sha256.ComputeHash((New-Object Text.UTF8Encoding($false)).GetBytes($fixtureSource)))).Replace('-', '').ToLowerInvariant()
} finally { $sha256.Dispose() }
$productionDigestRejected = $false
try {
    Install-TradingViewActiveSessionLauncher -TVControlRoot $root -PackageDirectory $packageDirectory -LauncherSource $launcherSource -ExpectedHealthSHA256 'b52a90e3fcb5b62a1474865c1818bacae3ef942dfe058b59c3b8574adeae4cbb' | Out-Null
} catch {
    if ([string]$_.Exception.Message -notmatch 'launch owner SHA-256 is unsupported') { throw }
    $productionDigestRejected = $true
}
if (-not $productionDigestRejected) { throw 'Synthetic TVControl launch owner matched the inspected production digest.' }
Install-TradingViewActiveSessionLauncher -TVControlRoot $root -PackageDirectory $packageDirectory -LauncherSource $launcherSource -ExpectedHealthSHA256 $fixtureSHA256 | Out-Null
$first = [IO.File]::ReadAllText((Join-Path $packageDirectory 'src\core\health.js'))
Install-TradingViewActiveSessionLauncher -TVControlRoot $root -PackageDirectory $packageDirectory -LauncherSource $launcherSource -ExpectedHealthSHA256 $fixtureSHA256 | Out-Null
$second = [IO.File]::ReadAllText((Join-Path $packageDirectory 'src\core\health.js'))
if ($first -cne $second -or
    [regex]::Matches($second, [regex]::Escape("const activeSessionLauncher = 'C:\\HerdrSandbox\\tools\\tvcontrol\\active-session-launch.ps1';")).Count -ne 1 -or
    -not $second.Contains('timeout: 60000') -or
    -not (Test-Path -LiteralPath (Join-Path $root 'active-session-launch.ps1') -PathType Leaf)) {
    throw 'TVControl active-session patch did not converge.'
}
$tampered = $second.Replace('timeout: 60000', 'timeout: 60001')
[IO.File]::WriteAllText((Join-Path $packageDirectory 'src\core\health.js'), $tampered, (New-Object Text.UTF8Encoding($false)))
$rejected = $false
try {
    Install-TradingViewActiveSessionLauncher -TVControlRoot $root -PackageDirectory $packageDirectory -LauncherSource $launcherSource -ExpectedHealthSHA256 $fixtureSHA256 | Out-Null
} catch {
    if ([string]$_.Exception.Message -notmatch 'partially modified') { throw }
    $rejected = $true
}
if (-not $rejected) { throw 'A drifted TVControl launch bridge was accepted.' }
if (@(Get-ChildItem -LiteralPath $root -Recurse -File | Where-Object { $_.Name -match '\.(?:tmp|bak)$' }).Count -ne 0) {
    throw 'Active-session patch left temporary files.'
}
`, functionSetup, quote(tvControlRoot), quote(packageDirectory), quote(launcherSource))
	command := hiddenCommand(mustWindowsPowerShellPath(t), "-NoLogo", "-NoProfile", "-NonInteractive", "-WindowStyle", "Hidden", "-EncodedCommand", encodePowerShell(script))
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("TVControl active-session patch regression: %v: %s", err, output)
	}
}

func TestTradingViewPortableMetadataUsesStrictOfficialManifestInWindowsPowerShell51(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows PowerShell 5.1 metadata regression")
	}
	functionSetup := provisioningPowerShellFunctionSetup(t,
		provisioningPowerShellFunctionSource{
			path:  defaultProvisioningPath(t, baseProvisioningName),
			names: []string{"Get-ProvisioningMetadataValue", "Get-ProvisioningToolVersion"},
		},
		provisioningPowerShellFunctionSource{
			path:  defaultProvisioningPath(t, stackProvisioningName),
			names: []string{"Get-TradingViewDesktopPortableMetadata"},
		},
	)
	script := fmt.Sprintf(`$ErrorActionPreference = 'Stop'
%s
$script:searchCount = 0
$script:manifestContent = @'
PackageIdentifier: TradingView.TradingViewDesktop
PackageVersion: 3.3.0.7992
MinimumOSVersion: 10.0.19042.0
InstallerType: msix
PackageFamilyName: TradingView.Desktop_n534cwy3pjxzj
Installers:
- Architecture: x64
  InstallerUrl: https://tvd-packages.tradingview.com/stable/latest/win32/TradingView.msix
  InstallerSha256: AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA
  SignatureSha256: BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB
ManifestType: installer
ManifestVersion: 1.12.0
'@
function Search-ProvisioningWinGetPackages {
    param([string]$Role, [string]$IdQuery, [switch]$Exact)
    $script:searchCount += 1
    return [pscustomobject]@{ Id = 'TradingView.TradingViewDesktop'; Version = '3.3.0.7992' }
}
function Invoke-WebRequest {
    param([string]$Uri, [switch]$UseBasicParsing)
    $script:lastManifestURI = $Uri
    return [pscustomobject]@{ StatusCode = 200; Content = $script:manifestContent }
}
$metadata = Get-TradingViewDesktopPortableMetadata
if ($script:searchCount -ne 1 -or
    $script:lastManifestURI -cne 'https://raw.githubusercontent.com/microsoft/winget-pkgs/master/manifests/t/TradingView/TradingViewDesktop/3.3.0.7992/TradingView.TradingViewDesktop.installer.yaml' -or
    [string]$metadata.Id -cne 'TradingView.TradingViewDesktop' -or
    [string]$metadata.Version -cne '3.3.0.7992' -or
    [string]$metadata.Url -cne 'https://tvd-packages.tradingview.com/stable/latest/win32/TradingView.msix' -or
    [string]$metadata.Sha256 -cne ('A' * 64) -or
    [string]$metadata.PayloadName -cne 'payload.msix' -or
    [string]$metadata.DeclaredMinimumOSVersion -cne '10.0.19042.0') {
    throw "Unexpected TradingView metadata: $($metadata | ConvertTo-Json -Compress)"
}
$script:manifestContent += [Environment]::NewLine + '  InstallerUrl: https://example.invalid/duplicate.msix'
$rejected = $false
try {
    Get-TradingViewDesktopPortableMetadata -Version '3.3.0.7992' | Out-Null
} catch {
    if ([string]$_.Exception.Message -notmatch 'InstallerUrl resolved to 2 values') { throw }
    $rejected = $true
}
if (-not $rejected) { throw 'Duplicate TradingView installer URL was accepted.' }
`, functionSetup)
	command := hiddenCommand(mustWindowsPowerShellPath(t), "-NoLogo", "-NoProfile", "-NonInteractive", "-EncodedCommand", encodePowerShell(script))
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("TradingView manifest metadata regression: %v: %s", err, output)
	}
}
