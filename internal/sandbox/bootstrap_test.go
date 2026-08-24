package sandbox

import (
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
)

func TestBootstrapUsesPowerShellAndVerifiedHostHerdrOnly(t *testing.T) {
	script := string(bootstrapScript)
	for _, required := range []string{
		"Net.SecurityProtocolType]::Tls12",
		"-ErrorAction Stop",
		"[IO.File]::Replace($temporaryPath, $Path, $backupPath, $true)",
		"https://api.github.com/repos/$Repository/releases/latest",
		"Microsoft.DesktopAppInstaller_8wekyb3d8bbwe.msixbundle",
		"DesktopAppInstaller_Dependencies.zip",
		"function Get-ResolvedBootstrapAsset",
		"function Get-BootstrapFileSHA256",
		"function Assert-BootstrapCacheTree",
		"C:\\HerdrSandbox\\cache",
		"bootstrap cache hit",
		"Add-AppxPackage -Path $wingetBundle -DependencyPath $wingetDependencyPaths",
		"$env:HERDR_SANDBOX_STATUS_DIRECTORY = [IO.Path]::GetFullPath($StatusDirectory)",
		"winget-packages.json",
		"tool-versions.json",
		"user.ps1",
		"provisioning-process.cs",
		"workspaces.json",
		"$unknownProjectScriptNames",
		"-Phase 'Registry'",
		"-Phase 'Development'",
		"-WorkspacesDirectory 'C:\\Workspaces' -PackagePlanPath $packagePlanPath",
		"-UserProvisioningPath $userProvisioning",
		"-ProcessOwnerPath $processOwner",
		"function Get-PowerShell7Installation",
		"Get-AppxPackage -Name 'Microsoft.PowerShell'",
		"Join-Path ([string]$package.InstallLocation) 'pwsh.exe'",
		"$file.VersionInfo.ProductVersion",
		"Get-AuthenticodeSignature -LiteralPath $executable",
		"-Value $powerShell7Executable",
		"OpenSSH default shell verification failed",
		"Microsoft.VCRedist.2015+.x64",
		"PowerShell/Win32-OpenSSH",
		"function Get-OpenSSHRelease",
		"releases?per_page=100",
		"p(?<preview>\\d+)-Preview$",
		"strictly named Preview exception",
		"$openSSHAssetName = \"OpenSSH-Win64-v$openSSHAssetVersion.msi\"",
		"OpenSSH MSI signature is invalid",
		"OpenSSH Server version verification",
		"'ADDLOCAL=Server'",
		"administrators_authorized_keys",
		"'connectable.json'",
		"'configuration-handoff.json'",
		"[int]$ConfigurationHandoffTimeoutMinutes",
		"AddMinutes($ConfigurationHandoffTimeoutMinutes)",
		"Verified host configuration did not arrive within $ConfigurationHandoffTimeoutMinutes minutes.",
		"HERDR_SANDBOX_HERDR_EXE",
		"Provisioned guest Herdr directory is not the unique first machine PATH entry.",
		`'--env', "PATH=$workspacePath"`,
		`'--env', "HERDR_SANDBOX_HERDR_EXE=$herdrExecutable"`,
		"Host provisioning did not publish the guest Herdr executable identity.",
		"'status', 'client', '--json'",
		"$workspaceArguments = @('workspace', 'create', '--cwd', $workspaceDirectory, '--label', $workspaceName,",
		"$workspaceArguments += '--focus'",
		"Creating $($workspaceEntries.Count) mounted-project workspaces",
		"$workspaceResponse.result.root_pane.pane_id",
		"PasswordAuthentication no",
	} {
		if !strings.Contains(script, required) {
			t.Fatalf("bootstrap is missing %q", required)
		}
	}
	lower := strings.ToLower(script)
	for _, forbidden := range []string{"cmd.exe", ".cmd", ".bat", "ogulcancelik/herdr", "herdrdev/herdr", "herdr.dev/install", "github.com/hdosys/herdr-win/releases/download/", "herdr-windows-x86_64.zip", "cachekey 'herdr-windows'", "c:\\herdr\\", "active-workspace.txt"} {
		if strings.Contains(lower, forbidden) {
			t.Fatalf("bootstrap contains forbidden path %q", forbidden)
		}
	}
	for _, forbidden := range []string{
		"-FilePath $powerShell7Executable",
		"PowerShell 7 bootstrap version is unexpected",
		"PowerShell 7 bootstrap verification",
	} {
		if strings.Contains(script, forbidden) {
			t.Fatalf("bootstrap executes PowerShell 7 during provisioning with %q", forbidden)
		}
	}
	for _, forbidden := range []string{
		"host-herdr.json",
		"herdr-runtime",
		"Read-HostHerdrRuntimeInput",
		`C:\HerdrSandbox\bin`,
		"server'",
		"reload-config",
	} {
		if strings.Contains(script, forbidden) {
			t.Fatalf("bootstrap retains replaced Herdr lifecycle path %q", forbidden)
		}
	}
	if count := strings.Count(script, "Invoke-WebRequest -Uri"); count != 1 {
		t.Fatalf("bootstrap has %d direct web download owners; want one cached asset owner", count)
	}
	if strings.Contains(script, "-ArgumentList @('--version') -join") {
		t.Fatal("bootstrap must join Invoke-Native output after command invocation")
	}
	mandatoryProfile := "-not (Test-Path -LiteralPath (Join-Path $projectProvisioningDirectory ($workspaceName + '.ps1')) -PathType Leaf)"
	if strings.Contains(script, mandatoryProfile) {
		t.Fatal("bootstrap still requires one provisioning profile per workspace")
	}
}

func TestBootstrapSelectsStableOpenSSHBeforeStrictPreviewInWindowsPowerShell51(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows PowerShell 5.1 OpenSSH release selection regression")
	}
	directory := t.TempDir()
	bootstrapPath := filepath.Join(directory, "bootstrap.ps1")
	if err := os.WriteFile(bootstrapPath, bootstrapScript, 0o600); err != nil {
		t.Fatal(err)
	}
	quote := func(value string) string { return strings.ReplaceAll(value, "'", "''") }
	script := fmt.Sprintf(`$ErrorActionPreference = 'Stop'
$tokens = $null
$errors = $null
$ast = [Management.Automation.Language.Parser]::ParseFile('%s', [ref]$tokens, [ref]$errors)
$definition = $ast.Find({ param($node) $node -is [Management.Automation.Language.FunctionDefinitionAst] -and $node.Name -ceq 'Get-OpenSSHRelease' }, $true)
if ($null -eq $definition) { throw 'Missing OpenSSH release selector.' }
Invoke-Expression $definition.Extent.Text
function Invoke-RestMethod {
    param([string]$Uri, [hashtable]$Headers)
    if ($Uri -cne 'https://api.github.com/repos/PowerShell/Win32-OpenSSH/releases?per_page=100') { throw "Unexpected URI: $Uri" }
    Write-Output -NoEnumerate $script:ReleaseFixture
}
function New-ReleaseFixture {
    param([string]$Tag, [bool]$Prerelease, [string]$AssetVersion = '')
    $assets = @()
    if (-not [string]::IsNullOrWhiteSpace($AssetVersion)) {
        $assets = @([pscustomobject]@{
            name = "OpenSSH-Win64-v$AssetVersion.msi"
            digest = 'sha256:' + (('a' * 64) -join '')
        })
    }
    return [pscustomobject]@{ tag_name = $Tag; draft = $false; prerelease = $Prerelease; assets = $assets }
}
$script:ReleaseFixture = @(
    (New-ReleaseFixture -Tag 'v9.7.0.0' -Prerelease $false -AssetVersion '9.7.0.0'),
    (New-ReleaseFixture -Tag '10.0.0.0p2-Preview' -Prerelease $true -AssetVersion '10.0.0.0'),
    (New-ReleaseFixture -Tag 'v9.9.0.0' -Prerelease $false -AssetVersion '9.9.0.0')
)
$selection = Get-OpenSSHRelease
if ([string]$selection.Version -cne 'v9.9.0.0' -or [string]$selection.AssetVersion -cne '9.9.0.0' -or
    [string]$selection.Channel -cne 'stable' -or [string]$selection.BannerVersion -cne '9.9') {
    throw 'OpenSSH stable release did not win over Preview.'
}
$script:ReleaseFixture = @(
    (New-ReleaseFixture -Tag 'v11.0.0.0' -Prerelease $false),
    (New-ReleaseFixture -Tag '10.0.0.0p1-Preview' -Prerelease $false -AssetVersion '10.0.0.0'),
    (New-ReleaseFixture -Tag '10.0.0.0p2-Preview' -Prerelease $true -AssetVersion '10.0.0.0'),
    (New-ReleaseFixture -Tag '10.1.0.0p1-Beta' -Prerelease $false -AssetVersion '10.1.0.0')
)
$selection = Get-OpenSSHRelease
if ([string]$selection.Version -cne '10.0.0.0p2-Preview' -or [string]$selection.AssetVersion -cne '10.0.0.0' -or
    [string]$selection.Channel -cne 'Preview exception' -or [string]$selection.BannerVersion -cne '10.0p2') {
    throw 'OpenSSH strict Preview fallback is invalid.'
}
$script:ReleaseFixture = @(
    (New-ReleaseFixture -Tag '10.1.0.0p1-Beta' -Prerelease $false -AssetVersion '10.1.0.0')
)
$rejected = $false
try { $null = Get-OpenSSHRelease } catch { $rejected = $_.Exception.Message.Contains('strictly named Preview') }
if (-not $rejected) { throw 'OpenSSH accepted an unsupported release channel.' }
`, quote(bootstrapPath))
	command := hiddenCommand(mustWindowsPowerShellPath(t), "-NoLogo", "-NoProfile", "-NonInteractive", "-WindowStyle", "Hidden", "-EncodedCommand", encodePowerShell(script))
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("OpenSSH release selection regression: %v: %s", err, output)
	}
}

func TestBootstrapOrdersConfigurationBeforeWorkspacesAndReady(t *testing.T) {
	script := string(bootstrapScript)
	needles := []string{
		"-Phase 'Registry'",
		"Get-ResolvedBootstrapAsset -Role 'WinGet bundle'",
		"Add-AppxPackage -Path $wingetBundle",
		"$vcRuntimeProcess = Start-Process",
		"-Phase 'Development'",
		"$powerShell7 = Get-PowerShell7Installation",
		"$openSSHInstallProcess = Start-Process",
		"'connectable.json'",
		"'configuration-handoff.json'",
		"HERDR_SANDBOX_HERDR_EXE",
		"'status', 'client', '--json'",
		"$workspaceArguments = @('workspace', 'create'",
		"'ready.json'",
	}
	previous := -1
	for _, needle := range needles {
		index := strings.Index(script, needle)
		if index < 0 {
			t.Fatalf("bootstrap is missing %q", needle)
		}
		if index <= previous {
			t.Fatalf("bootstrap ordering is wrong at %q", needle)
		}
		previous = index
	}
	connectableIndex := strings.Index(script, "schemaVersion = 1\n        ip = $ipAddress")
	readyIndex := strings.LastIndex(script, "schemaVersion = 3\n        ip = $ipAddress")
	if connectableIndex < 0 || readyIndex <= connectableIndex {
		t.Fatalf("bootstrap connection/ready schemas are not ordered: connectable=%d ready=%d", connectableIndex, readyIndex)
	}
}

func TestBootstrapPassesAudioSelectionsOnlyToBaseRegistry(t *testing.T) {
	script := string(bootstrapScript)
	registryStart := strings.Index(script, "& $baseProvisioning -Phase 'Registry'")
	developmentStart := strings.Index(script, "& $baseProvisioning -Phase 'Development'")
	if strings.Count(script, "[ValidateSet('Disabled', 'Enabled')]") < 2 ||
		!strings.Contains(script, "[string]$AudioPlayback") || !strings.Contains(script, "[string]$AudioInput") ||
		registryStart < 0 || developmentStart <= registryStart {
		t.Fatalf("bootstrap audio handoff boundaries are missing: registry=%d development=%d", registryStart, developmentStart)
	}
	registryCall := script[registryStart:developmentStart]
	if strings.Count(registryCall, "-AudioOutputEnabled:($AudioPlayback -ceq 'Enabled')") != 1 ||
		strings.Count(registryCall, "-AudioInputEnabled:($AudioInput -ceq 'Enabled')") != 1 {
		t.Fatalf("Base Registry audio handoff = %q", registryCall)
	}
	developmentEnd := strings.Index(script[developmentStart:], "$powerShell7 = Get-PowerShell7Installation")
	if developmentEnd < 0 {
		t.Fatal("Base Development call boundary is missing")
	}
	developmentCall := script[developmentStart : developmentStart+developmentEnd]
	if strings.Contains(developmentCall, "AudioPlayback") || strings.Contains(developmentCall, "AudioInput") ||
		strings.Contains(developmentCall, "AudioOutputEnabled") {
		t.Fatal("Base Development unexpectedly owns the audio selection")
	}
}

func TestResolvedBootstrapAssetCachesRepairsAndStagesInWindowsPowerShell51(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows PowerShell 5.1 regression")
	}
	payload := []byte("resolved bootstrap payload\n")
	digest := fmt.Sprintf("%x", sha256.Sum256(payload))
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		requests.Add(1)
		response.Header().Set("Content-Type", "application/octet-stream")
		_, _ = response.Write(payload)
	}))
	defer server.Close()

	directory := t.TempDir()
	bootstrapPath := filepath.Join(directory, "bootstrap.ps1")
	if err := os.WriteFile(bootstrapPath, bootstrapScript, 0o600); err != nil {
		t.Fatal(err)
	}
	quote := func(value string) string { return strings.ReplaceAll(value, "'", "''") }
	script := fmt.Sprintf(`$ErrorActionPreference = 'Stop'
Import-Module Microsoft.PowerShell.Utility -ErrorAction Stop
$tokens = $null
$errors = $null
$ast = [System.Management.Automation.Language.Parser]::ParseFile('%s', [ref]$tokens, [ref]$errors)
foreach ($name in @('Assert-BootstrapCachePath', 'Assert-BootstrapCacheTree', 'Get-BootstrapFileSHA256', 'Get-ResolvedBootstrapAsset')) {
    $definition = $ast.Find({ param($node) $node -is [System.Management.Automation.Language.FunctionDefinitionAst] -and $node.Name -eq $name }, $true)
    if ($null -eq $definition) { throw "Missing function: $name" }
    Invoke-Expression $definition.Extent.Text
}
$trustRoot = '%s'
$cacheRoot = Join-Path $trustRoot 'bootstrap'
$stageRoot = '%s'
New-Item -ItemType Directory -Path $trustRoot, $stageRoot -Force | Out-Null
$destination = Join-Path $stageRoot 'payload.bin'
$expectedDestination = [IO.Path]::GetFullPath($destination)
$staleDirectory = Join-Path $cacheRoot 'test-asset\stale'
New-Item -ItemType Directory -Path $staleDirectory -Force | Out-Null
[IO.File]::WriteAllText((Join-Path $staleDirectory 'stale.bin'), 'stale')
$arguments = @{
    Role = 'Test asset'
    CacheKey = 'test-asset'
    Uri = '%s'
    ExpectedSHA256 = '%s'
    FileName = 'payload.bin'
    DestinationPath = $destination
    CacheRoot = $cacheRoot
    CacheTrustRoot = $trustRoot
}
$first = @(Get-ResolvedBootstrapAsset @arguments)
if ($first.Count -ne 1 -or [string]$first[0] -cne $expectedDestination -or
    (Get-BootstrapFileSHA256 -Path ([string]$first[0])) -cne '%s') {
    throw 'Initial cached asset result is invalid.'
}
if (Test-Path -LiteralPath $staleDirectory) {
    throw 'Stale bootstrap cache entry was not pruned.'
}
$cached = Join-Path $cacheRoot 'test-asset\%s\payload.bin'
[IO.File]::WriteAllText($cached, 'corrupt')
$second = @(Get-ResolvedBootstrapAsset @arguments)
if ($second.Count -ne 1 -or [string]$second[0] -cne $expectedDestination -or
    (Get-BootstrapFileSHA256 -Path ([string]$second[0])) -cne '%s') {
    throw 'Repaired cached asset result is invalid.'
}
$third = @(Get-ResolvedBootstrapAsset @arguments)
if ($third.Count -ne 1 -or [string]$third[0] -cne $expectedDestination -or
    (Get-BootstrapFileSHA256 -Path ([string]$third[0])) -cne '%s') {
    throw 'Cache-hit staged asset result is invalid.'
}
exit 0
`, quote(bootstrapPath), quote(filepath.Join(directory, "cache")), quote(filepath.Join(directory, "stage")), server.URL+"/payload.bin", digest, digest, digest, digest, digest)
	powerShell := mustWindowsPowerShellPath(t)
	command := hiddenCommand(powerShell, "-NoLogo", "-NoProfile", "-NonInteractive", "-EncodedCommand", encodePowerShell(script))
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("resolved bootstrap cache regression: %v: %s", err, output)
	}
	if got := requests.Load(); got != 2 {
		t.Fatalf("bootstrap cache HTTP requests = %d, want 2 (miss + corrupt repair)", got)
	}
}

func TestBootstrapBoundsWinGetRegistrationRaceRetries(t *testing.T) {
	script := string(bootstrapScript)
	for _, required := range []string{
		"for ($attempt = 1; $attempt -le 4; $attempt += 1)",
		"$diagnostic.IndexOf('0x80073CF3'",
		"$diagnostic.IndexOf('0x80070003'",
		"$diagnostic.IndexOf('AppxManifest.xml'",
		"if (-not $registrationNotReady -or $attempt -eq 4)",
		"Start-Sleep -Seconds (5 * $attempt)",
	} {
		if !strings.Contains(script, required) {
			t.Fatalf("bootstrap WinGet retry contract is missing %q", required)
		}
	}
}

func TestBootstrapDefersHerdrDeploymentAndLifecycleToHostProvisioning(t *testing.T) {
	script := string(bootstrapScript)
	for _, required := range []string{
		"HERDR_SANDBOX_HERDR_EXE",
		"Host provisioning did not publish the guest Herdr executable identity.",
		"Provisioned Herdr client status",
		"function Invoke-HerdrBoundary",
		"[HerdrSandbox.ProvisioningProcess]::Run($spec)",
		"$result.OutputTruncated",
		"$result.OutputBytes -gt 65536",
		"'version|herdr_version|build_id|protocol|binary|session'",
		"Provisioned guest Herdr client identity is invalid.",
		"herdrRuntimeVersion = $herdrRuntimeVersion",
		"herdrBinary = $herdrExecutable",
	} {
		if !strings.Contains(script, required) {
			t.Fatalf("bootstrap provisioned Herdr handoff is missing %q", required)
		}
	}
	for _, forbidden := range []string{"host-herdr.json", "herdr-runtime", "Read-HostHerdrRuntimeInput", `C:\HerdrSandbox\bin`, "Start-Process -FilePath $herdrExecutable", "reload-config"} {
		if strings.Contains(script, forbidden) {
			t.Fatalf("bootstrap retains replaced Herdr deployment or lifecycle contract %q", forbidden)
		}
	}
}

func TestBootstrapHerdrBoundaryRejectsTimeoutAndOverflowInWindowsPowerShell51(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows Job Object guest boundary regression")
	}
	directory := t.TempDir()
	bootstrapPath := filepath.Join(directory, "bootstrap.ps1")
	processPath := filepath.Join(directory, provisioningProcessName)
	if err := os.WriteFile(bootstrapPath, bootstrapScript, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(processPath, provisioningProcessSource, 0o600); err != nil {
		t.Fatal(err)
	}
	quote := func(value string) string { return strings.ReplaceAll(value, "'", "''") }
	script := fmt.Sprintf(`$ErrorActionPreference = 'Stop'
Add-Type -Path '%s'
$tokens = $null
$errors = $null
$ast = [Management.Automation.Language.Parser]::ParseFile('%s', [ref]$tokens, [ref]$errors)
foreach ($name in @('Get-BoundedDiagnosticText', 'Invoke-HerdrBoundary')) {
    $definition = $ast.Find({ param($node) $node -is [Management.Automation.Language.FunctionDefinitionAst] -and $node.Name -eq $name }, $true)
    if ($null -eq $definition) { throw "Missing function: $name" }
    Invoke-Expression $definition.Extent.Text
}
$powerShell = '%s'
$timedOut = $false
try {
    $null = Invoke-HerdrBoundary -Role 'timeout fixture' -FilePath $powerShell -ArgumentList @('-NoLogo','-NoProfile','-NonInteractive','-Command','Start-Sleep -Seconds 30') -TimeoutSeconds 1
} catch {
    $timedOut = $_.Exception.Message -like '*exceeded 1 seconds*'
}
if (-not $timedOut) { throw 'Hung Herdr boundary was not terminated.' }
$overflow = $false
try {
    $null = Invoke-HerdrBoundary -Role 'overflow fixture' -FilePath $powerShell -ArgumentList @('-NoLogo','-NoProfile','-NonInteractive','-Command','[Console]::Out.Write((''x'' * 70000))')
} catch {
    $overflow = $_.Exception.Message -like '*exceeded the 65536-byte output limit*'
}
if (-not $overflow) { throw 'Noisy Herdr boundary was not rejected.' }
exit 0
`, quote(processPath), quote(bootstrapPath), quote(mustWindowsPowerShellPath(t)))
	harnessPath := filepath.Join(directory, "herdr-boundary-regression.ps1")
	if err := os.WriteFile(harnessPath, []byte(script), 0o600); err != nil {
		t.Fatal(err)
	}
	command := hiddenCommand(mustWindowsPowerShellPath(t), "-NoLogo", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File", harnessPath)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("bounded guest Herdr boundary regression: %v: %s", err, output)
	}
}

func TestBootstrapConfigurationHandoffParserIsStrictInWindowsPowerShell51(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows PowerShell 5.1 regression")
	}
	directory := t.TempDir()
	bootstrapPath := filepath.Join(directory, "bootstrap.ps1")
	if err := os.WriteFile(bootstrapPath, bootstrapScript, 0o600); err != nil {
		t.Fatal(err)
	}
	handoffPath := filepath.Join(directory, "configuration-handoff.json")
	quote := func(value string) string { return strings.ReplaceAll(value, "'", "''") }
	script := fmt.Sprintf(`$ErrorActionPreference = 'Stop'
$tokens = $null
$errors = $null
$ast = [System.Management.Automation.Language.Parser]::ParseFile('%s', [ref]$tokens, [ref]$errors)
$definition = $ast.Find({ param($node) $node -is [System.Management.Automation.Language.FunctionDefinitionAst] -and $node.Name -eq 'Read-ConfigurationHandoff' }, $true)
Invoke-Expression $definition.Extent.Text
$path = '%s'
$utf8 = New-Object Text.UTF8Encoding($false)
[IO.File]::WriteAllText($path, '{"schemaVersion":1,"outcome":"verified"}', $utf8)
$verified = Read-ConfigurationHandoff -Path $path
if ([string]$verified.outcome -cne 'verified') { throw 'Canonical verified handoff was rejected.' }
[IO.File]::WriteAllText($path, '{"schemaVersion":1,"outcome":"failed","phase":"configuration-sync","message":"copy failed"}', $utf8)
$failed = Read-ConfigurationHandoff -Path $path
if ([string]$failed.outcome -cne 'failed' -or [string]$failed.phase -cne 'configuration-sync' -or [string]$failed.message -cne 'copy failed') {
    throw 'Canonical failed handoff was rejected.'
}
$invalid = @(
    '{"schemaVersion":"1","outcome":"verified"}',
    '{"schemaVersion":true,"outcome":"verified"}',
    '{"schemaVersion":1,"outcome":["verified"]}',
    '{"schemaVersion":1,"outcome":"verified","outcome":"verified"}',
    '{"schemaVersion":1,"outcome":"verified","extra":true}',
    ' {"schemaVersion":1,"outcome":"verified"}'
)
foreach ($value in $invalid) {
    [IO.File]::WriteAllText($path, $value, $utf8)
    $accepted = $false
    try { $null = Read-ConfigurationHandoff -Path $path; $accepted = $true } catch { }
    if ($accepted) { throw "Invalid handoff was accepted: $value" }
}
[IO.File]::WriteAllText($path, ('x' * 8193), $utf8)
$accepted = $false
try { $null = Read-ConfigurationHandoff -Path $path; $accepted = $true } catch { }
if ($accepted) { throw 'Oversized handoff was accepted.' }
exit 0
`, quote(bootstrapPath), quote(handoffPath))
	powerShell := mustWindowsPowerShellPath(t)
	command := hiddenCommand(powerShell, "-NoLogo", "-NoProfile", "-NonInteractive", "-EncodedCommand", encodePowerShell(script))
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("configuration handoff parser regression: %v: %s", err, output)
	}
}
