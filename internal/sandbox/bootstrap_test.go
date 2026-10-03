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

func TestPlaywrightBrowserAccessTokenInWindowsPowerShell51(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows PowerShell 5.1 Playwright token input regression")
	}
	directory := t.TempDir()
	accessPath := filepath.Join(directory, playwrightAccessName)
	if err := os.WriteFile(accessPath, playwrightAccessScript, 0o600); err != nil {
		t.Fatal(err)
	}
	quote := func(value string) string { return strings.ReplaceAll(value, "'", "''") }
	script := fmt.Sprintf(`$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'
trap { [Console]::Error.WriteLine($_.Exception.ToString()); exit 1 }
class Environment {
    static [hashtable]$Values = @{}
    static [string] GetEnvironmentVariable([string]$name, [string]$target) { return [Environment]::Values[$target] }
    static [void] SetEnvironmentVariable([string]$name, [string]$value, [string]$target) { [Environment]::Values[$target] = $value }
}
. '%s'
if ([string](Initialize-PlaywrightExtensionToken -Enabled $true) -cne '' -or [Environment]::Values.Count -ne 0) {
    throw 'Missing token did not continue without UI or environment writes.'
}
if ((ConvertFrom-PlaywrightExtensionTokenInput -Value ' token-value_123 ') -cne 'token-value_123') { throw 'Bare token normalization failed.' }
if ((ConvertFrom-PlaywrightExtensionTokenInput -Value 'PLAYWRIGHT_MCP_EXTENSION_TOKEN=token-value_456') -cne 'token-value_456') { throw 'Copied assignment normalization failed.' }
$empty = [string](ConvertFrom-PlaywrightExtensionTokenInput -Value '  ')
if ($empty -cne '') { throw 'Empty token normalization failed.' }
$invalidAccepted = $false
try { $null = ConvertFrom-PlaywrightExtensionTokenInput -Value (('a' * 513) -join ''); $invalidAccepted = $true } catch { }
if ($invalidAccepted) { throw 'Overlong token was accepted.' }
$invalidAccepted = $false
try { $null = ConvertFrom-PlaywrightExtensionTokenInput -Value ('first' + [char]10 + 'second'); $invalidAccepted = $true } catch { }
if ($invalidAccepted) { throw 'Multiline token was accepted.' }
Set-PlaywrightExtensionToken -Value 'PLAYWRIGHT_MCP_EXTENSION_TOKEN=token-value_saved'
if ([Environment]::Values['Machine'] -cne 'token-value_saved' -or [Environment]::Values['Process'] -cne 'token-value_saved') {
    throw 'Dialog token was not published to the guest environment.'
}
[Environment]::Values['Process'] = 'stale-token'
if ((Initialize-PlaywrightExtensionToken -Enabled $true) -cne 'token-value_saved') {
    throw 'Bootstrap did not prefer the latest dialog-saved machine token.'
}
if ([string](Initialize-PlaywrightExtensionToken -Enabled $false) -cne '') { throw 'Unselected Playwright used a token.' }
[Console]::WriteLine('ok')
`, quote(accessPath))
	command := hiddenCommand(mustWindowsPowerShellPath(t), "-NoLogo", "-NoProfile", "-NonInteractive", "-WindowStyle", "Hidden", "-ExecutionPolicy", "Bypass", "-EncodedCommand", encodePowerShell(script))
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("Playwright token input regression: %v: %s", err, output)
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
		"function ConvertFrom-HerdrClientStatus",
		"[HerdrSandbox.ProvisioningProcess]::Run($spec)",
		"$result.OutputTruncated",
		"$result.OutputBytes -gt 65536",
		"@('version', 'herdr_version', 'build_id', 'protocol', 'binary', 'session')",
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
