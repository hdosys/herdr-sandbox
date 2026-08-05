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
		"Install-NodeRuntime -Version $NodeVersion",
		"@ferroxlabs/tvcontrol@latest",
		"@ferroxlabs/tvcontrol@$TVControlVersion",
		"$toolRoot = $tvControlRoot",
		"'--ignore-scripts'",
		"'--omit=optional'",
		"'tv.cmd'",
		"'tvcontrol.cmd'",
		"'tv.ps1'",
		"'tvcontrol.ps1'",
		"Usage: tv <command> [options]",
		"TradingView remains stopped with CDP disabled",
	} {
		if !strings.Contains(block, required) {
			t.Fatalf("TradingView stack is missing %q", required)
		}
	}
	for _, forbidden := range []string{
		"Start-Process", "taskkill", "--remote-debugging-port", "launch_tv_debug.bat",
		"Install-NodeStack", "npm ci", "npm link", "ProjectDirectory",
		"Join-Path $tvControlRoot $TVControlVersion",
		"Get-AppxPackage", "Add-AppxPackage", "-Adapter 'MSIX'", "$minimumWindowsBuild",
		"3.3.0.7992",
	} {
		if strings.Contains(block, forbidden) {
			t.Fatalf("TradingView stack contains forbidden runtime/project path %q", forbidden)
		}
	}
}

func TestTradingViewPortableMetadataUsesStrictOfficialManifestInWindowsPowerShell51(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows PowerShell 5.1 metadata regression")
	}
	quote := func(value string) string { return strings.ReplaceAll(value, "'", "''") }
	script := fmt.Sprintf(`$ErrorActionPreference = 'Stop'
$tokens = $null
$errors = $null
$baseAST = [Management.Automation.Language.Parser]::ParseFile('%s', [ref]$tokens, [ref]$errors)
$metadataValue = $baseAST.Find({ param($node) $node -is [Management.Automation.Language.FunctionDefinitionAst] -and $node.Name -ceq 'Get-ProvisioningMetadataValue' }, $true)
$stackAST = [Management.Automation.Language.Parser]::ParseFile('%s', [ref]$tokens, [ref]$errors)
$resolver = $stackAST.Find({ param($node) $node -is [Management.Automation.Language.FunctionDefinitionAst] -and $node.Name -ceq 'Get-TradingViewDesktopPortableMetadata' }, $true)
if ($null -eq $metadataValue -or $null -eq $resolver) { throw 'TradingView metadata functions are missing.' }
Invoke-Expression $metadataValue.Extent.Text
Invoke-Expression $resolver.Extent.Text
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
`, quote(defaultProvisioningPath(t, baseProvisioningName)), quote(defaultProvisioningPath(t, stackProvisioningName)))
	command := hiddenCommand(mustWindowsPowerShellPath(t), "-NoLogo", "-NoProfile", "-NonInteractive", "-EncodedCommand", encodePowerShell(script))
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("TradingView manifest metadata regression: %v: %s", err, output)
	}
}
