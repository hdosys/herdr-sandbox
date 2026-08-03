package sandbox

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
)

func TestBootstrapUsesPowerShellAndHerdrWinOnly(t *testing.T) {
	script := string(bootstrapScript)
	for _, required := range []string{
		"Net.SecurityProtocolType]::Tls12",
		"-ErrorAction Stop",
		"[IO.File]::Replace($temporaryPath, $Path, $backupPath, $true)",
		"github.com/microsoft/winget-cli/releases/download/",
		"Microsoft.DesktopAppInstaller_8wekyb3d8bbwe.msixbundle",
		"DesktopAppInstaller_Dependencies.zip",
		"function Get-PinnedBootstrapAsset",
		"function Get-BootstrapFileSHA256",
		"function Assert-BootstrapCacheTree",
		"C:\\HerdrSandbox\\cache",
		"bootstrap cache hit",
		"Add-AppxPackage -Path $wingetBundle -DependencyPath $wingetDependencyPaths",
		"$env:HERDR_SANDBOX_STATUS_DIRECTORY = [IO.Path]::GetFullPath($StatusDirectory)",
		"winget-packages.json",
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
		"default_shell = `\"pwsh.exe`\"",
		"-Value $powerShell7Executable",
		"OpenSSH default shell verification failed",
		"bootstrap-release.json",
		"host-herdr.json",
		"herdr-install",
		"Host Herdr runtime layout is unsupported",
		"download.visualstudio.microsoft.com/download/pr/",
		"VC_redist.x64.exe",
		"@('/install', '/quiet', '/norestart')",
		"github.com/PowerShell/Win32-OpenSSH/releases/download/",
		"$herdrInstallRoot = 'C:\\HerdrSandbox'",
		"$herdrRoot = Join-Path $herdrInstallRoot 'runtime'",
		"$herdrDirectory = Join-Path $herdrRoot $ExpectedHerdrBuildID",
		"$herdrBinDirectory = Join-Path $herdrInstallRoot 'bin'",
		"($managedRuntimePrefix + 'herdr-launcher.exe')",
		"($managedRuntimePrefix + 'runtime.ready')",
		"[IO.File]::Copy($sourcePath, $destinationPath, $false)",
		"[Environment]::SetEnvironmentVariable('Path', $updatedMachinePath, 'Machine')",
		"Get-Command -Name 'herdr.exe' -CommandType Application",
		"Guest PATH resolved an unexpected Herdr executable",
		"OpenSSH-Win64-v10.0.0.0.msi",
		"'ADDLOCAL=Server'",
		"administrators_authorized_keys",
		"Start-Process -FilePath $herdrExecutable -ArgumentList @('server')",
		"'connectable.json'",
		"'configuration-handoff.json'",
		"[int]$ConfigurationHandoffTimeoutMinutes",
		"AddMinutes($ConfigurationHandoffTimeoutMinutes)",
		"Verified host configuration did not arrive within $ConfigurationHandoffTimeoutMinutes minutes.",
		"$workspaceArguments = @('workspace', 'create', '--cwd', $workspaceDirectory, '--label', $workspaceName)",
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

func TestBootstrapOrdersConfigurationBeforeWorkspacesAndReady(t *testing.T) {
	script := string(bootstrapScript)
	needles := []string{
		"$hostHerdrMetadata =",
		"-Phase 'Registry'",
		"Get-PinnedBootstrapAsset -Role 'WinGet bundle'",
		"Add-AppxPackage -Path $wingetBundle",
		"-Phase 'Development'",
		"$powerShell7 = Get-PowerShell7Installation",
		"$vcRuntimeProcess = Start-Process",
		"[IO.File]::Copy($sourcePath, $destinationPath, $false)",
		"$initialHerdrConfig =",
		"$openSSHInstallProcess = Start-Process",
		"Start-Process -FilePath $herdrExecutable",
		"'connectable.json'",
		"'configuration-handoff.json'",
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
	readyIndex := strings.LastIndex(script, "schemaVersion = 2\n        ip = $ipAddress")
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

func TestPinnedBootstrapAssetCachesRepairsAndStagesInWindowsPowerShell51(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows PowerShell 5.1 regression")
	}
	payload := []byte("pinned bootstrap payload\n")
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
foreach ($name in @('Assert-BootstrapCachePath', 'Assert-BootstrapCacheTree', 'Get-BootstrapFileSHA256', 'Get-PinnedBootstrapAsset')) {
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
$first = @(Get-PinnedBootstrapAsset @arguments)
if ($first.Count -ne 1 -or [string]$first[0] -cne $expectedDestination -or
    (Get-BootstrapFileSHA256 -Path ([string]$first[0])) -cne '%s') {
    throw 'Initial cached asset result is invalid.'
}
if (Test-Path -LiteralPath $staleDirectory) {
    throw 'Stale bootstrap cache entry was not pruned.'
}
$cached = Join-Path $cacheRoot 'test-asset\%s\payload.bin'
[IO.File]::WriteAllText($cached, 'corrupt')
$second = @(Get-PinnedBootstrapAsset @arguments)
if ($second.Count -ne 1 -or [string]$second[0] -cne $expectedDestination -or
    (Get-BootstrapFileSHA256 -Path ([string]$second[0])) -cne '%s') {
    throw 'Repaired cached asset result is invalid.'
}
$third = @(Get-PinnedBootstrapAsset @arguments)
if ($third.Count -ne 1 -or [string]$third[0] -cne $expectedDestination -or
    (Get-BootstrapFileSHA256 -Path ([string]$third[0])) -cne '%s') {
    throw 'Cache-hit staged asset result is invalid.'
}
exit 0
`, quote(bootstrapPath), quote(filepath.Join(directory, "cache")), quote(filepath.Join(directory, "stage")), server.URL+"/payload.bin", digest, digest, digest, digest, digest)
	powerShell := mustWindowsPowerShellPath(t)
	command := hiddenCommand(powerShell, "-NoLogo", "-NoProfile", "-NonInteractive", "-EncodedCommand", encodePowerShell(script))
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("pinned bootstrap cache regression: %v: %s", err, output)
	}
	if got := requests.Load(); got != 2 {
		t.Fatalf("bootstrap cache HTTP requests = %d, want 2 (miss + corrupt repair)", got)
	}
}

func TestBootstrapAndReleaseMetadataAreEmbedded(t *testing.T) {
	if len(bytes.TrimSpace(bootstrapScript)) == 0 {
		t.Fatal("bootstrap script is empty")
	}
	if len(bytes.TrimSpace(bootstrapReleaseJSON)) == 0 {
		t.Fatal("bootstrap release metadata is empty")
	}
}

func TestBootstrapReleasePowerShellSchemaMatchesEmbeddedMetadata(t *testing.T) {
	var metadata map[string]json.RawMessage
	if err := json.Unmarshal(bootstrapReleaseJSON, &metadata); err != nil {
		t.Fatal(err)
	}
	properties := make([]string, 0, len(metadata))
	for name := range metadata {
		properties = append(properties, name)
	}
	sort.Slice(properties, func(left, right int) bool {
		return strings.ToLower(properties[left]) < strings.ToLower(properties[right])
	})
	shape := strings.Join(properties, "|")
	expected := "($releaseMetadataProperties -join '|') -cne '" + shape + "'"
	if !strings.Contains(string(bootstrapScript), expected) {
		t.Fatalf("bootstrap PowerShell schema does not match embedded metadata: want %q", shape)
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

func TestBootstrapPreservesManagedHerdrInstallTree(t *testing.T) {
	script := string(bootstrapScript)
	for _, required := range []string{
		"\\.(?<buildID>[0-9a-f]{12}\\.[0-9a-f]{12})$",
		"$ExpectedHerdrBuildID = $herdrBuildIDMatch.Groups['buildID'].Value",
		"$hostHerdrSourceDirectory = Join-Path $InputDirectory 'herdr-install'",
		"$herdrInstallRoot = 'C:\\HerdrSandbox'",
		"$herdrRoot = Join-Path $herdrInstallRoot 'runtime'",
		"$herdrDirectory = Join-Path $herdrRoot $ExpectedHerdrBuildID",
		"$herdrBinDirectory = Join-Path $herdrInstallRoot 'bin'",
		"herdr-managed-bin-v1`n",
		"herdr-runtime-v1`nbuild_id=$ExpectedHerdrBuildID`n",
		"herdr-pointer-v1`nbuild_id=$ExpectedHerdrBuildID`n",
		"Guest-local Herdr managed install copy failed verification",
	} {
		if !strings.Contains(script, required) {
			t.Fatalf("bootstrap managed Herdr layout contract is missing %q", required)
		}
	}
	if count := strings.Count(script, "($managedRuntimePrefix + '"); count != 8 {
		t.Fatalf("bootstrap has %d parenthesized managed-runtime paths, want 8", count)
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
