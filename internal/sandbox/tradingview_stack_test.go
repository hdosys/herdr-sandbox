package sandbox

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestTradingViewPortablePackageReusesCurrentCacheMetadataInWindowsPowerShell51(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows PowerShell 5.1 metadata regression")
	}
	functionSetup := provisioningPowerShellFunctionSetup(t,
		provisioningPowerShellFunctionSource{
			path:  defaultProvisioningPath(t, stackProvisioningName),
			names: []string{"Get-TradingViewDesktopPortablePackage"},
		},
	)
	script := fmt.Sprintf(`$ErrorActionPreference = 'Stop'
%s
$script:identityCalls = @()
$script:cacheCalls = @()
function Get-ProvisioningWinGetPackageIdentity {
    param([string]$Role, [string]$Id, [string]$Version)
    $script:identityCalls += ,@($Role, $Id, $Version)
    $resolved = if ([string]::IsNullOrWhiteSpace($Version)) { '4.1.2.9000' } else { $Version }
    return [pscustomobject]@{ Id = $Id; Version = $resolved }
}
function Get-ProvisioningCachedPackageMetadata {
    param([string]$Id, [string]$Version, [string]$Architecture, [string]$InstallerType,
        [string]$PayloadExtension, [string]$AllowedHost)
    $script:cacheCalls += ,@($Id, $Version, $Architecture, $InstallerType, $PayloadExtension, $AllowedHost)
    return [pscustomobject]@{ Id = $Id; Version = $Version; Architecture = $Architecture;
        InstallerType = $InstallerType; Scope = ''; Url = 'https://tvd-packages.tradingview.com/release.msix';
        Sha256 = ('A' * 64); PayloadName = 'payload.msix' }
}
function Get-ProvisioningTargetedWinGetPackage { throw 'Targeted download unexpectedly ran on a cache hit.' }
$package = Get-TradingViewDesktopPortablePackage
if ([string]$package.Metadata.Version -cne '4.1.2.9000' -or
    -not [string]::IsNullOrEmpty([string]$package.PayloadPath) -or
    ($script:identityCalls[0] -join '|') -cne 'TradingView Desktop|TradingView.TradingViewDesktop|' -or
    ($script:cacheCalls[0] -join '|') -cne 'TradingView.TradingViewDesktop|4.1.2.9000|x64|msix|.msix|tvd-packages.tradingview.com') {
    throw "Unexpected cached TradingView package: $($package | ConvertTo-Json -Compress)"
}
`, functionSetup)
	command := hiddenCommand(mustWindowsPowerShellPath(t), "-NoLogo", "-NoProfile", "-NonInteractive", "-EncodedCommand", encodePowerShell(script))
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("TradingView cache metadata regression: %v: %s", err, output)
	}
}

func TestTargetedWinGetDownloadSelectsTradingViewForSupportedPackageOSInWindowsPowerShell51(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows PowerShell 5.1 metadata regression")
	}
	payload := []byte("signed-msix-fixture")
	payloadSHA256 := fmt.Sprintf("%X", sha256.Sum256(payload))
	root := t.TempDir()
	sourcePayload := filepath.Join(root, "source.msix")
	sourceManifest := filepath.Join(root, "source.yaml")
	downloadDirectory := filepath.Join(root, "download")
	if err := os.WriteFile(sourcePayload, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	manifest := fmt.Sprintf(`PackageIdentifier: TradingView.TradingViewDesktop
PackageVersion: 3.3.0.7992
Architecture: x64
InstallerType: msix
InstallerUrl: https://tvd-packages.tradingview.com/stable/latest/win32/TradingView.msix
InstallerSha256: %s
ManifestType: merged
ManifestVersion: 1.12.0
`, payloadSHA256)
	if err := os.WriteFile(sourceManifest, []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(downloadDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	quote := func(value string) string { return "'" + strings.ReplaceAll(value, "'", "''") + "'" }
	functionSetup := provisioningPowerShellFunctionSetup(t,
		provisioningPowerShellFunctionSource{
			path: defaultProvisioningPath(t, baseProvisioningName),
			names: []string{
				"Get-ProvisioningMergedManifestValue",
				"Assert-ProvisioningMergedManifestField",
				"Assert-ProvisioningDownloadedManifest",
				"Get-ProvisioningTargetedWinGetPackage",
			},
		},
	)
	script := fmt.Sprintf(`$ErrorActionPreference = 'Stop'
%s
$script:arguments = @()
function Invoke-ProvisioningNative {
    param($Role, $FilePath, [object[]]$ArgumentList)
    $script:arguments = @($ArgumentList)
    $downloadIndex = [Array]::IndexOf($script:arguments, '--download-directory')
    $destination = [string]$script:arguments[$downloadIndex + 1]
    Copy-Item -LiteralPath %s -Destination (Join-Path $destination 'TradingView.yaml')
    Copy-Item -LiteralPath %s -Destination (Join-Path $destination 'TradingView.msix')
}
$resolved = Get-ProvisioningTargetedWinGetPackage -Role 'TradingView Desktop' -Id 'TradingView.TradingViewDesktop' -Version '3.3.0.7992' -Architecture 'x64' -InstallerType 'msix' -PayloadExtension '.msix' -Platform 'Windows.Desktop' -OSVersion '10.0.19042.0' -DownloadDirectory %s
$expectedArguments = 'download|--id|TradingView.TradingViewDesktop|--exact|--source|winget|--version|3.3.0.7992|--architecture|x64|--installer-type|msix|--platform|Windows.Desktop|--os-version|10.0.19042.0|--skip-dependencies|--skip-license|--download-directory|' + %s + '|--accept-package-agreements|--accept-source-agreements|--disable-interactivity'
if (($script:arguments -join '|') -cne $expectedArguments -or
    [string]$resolved.Metadata.Url -cne 'https://tvd-packages.tradingview.com/stable/latest/win32/TradingView.msix' -or
    [string]$resolved.Metadata.Sha256 -cne '%s' -or
    -not (Test-Path -LiteralPath $resolved.PayloadPath -PathType Leaf)) {
    throw "Targeted TradingView package was not resolved: $($resolved | ConvertTo-Json -Compress)"
}
`, functionSetup, quote(sourceManifest), quote(sourcePayload), quote(downloadDirectory), quote(downloadDirectory), payloadSHA256)
	command := hiddenCommand(mustWindowsPowerShellPath(t), "-NoLogo", "-NoProfile", "-NonInteractive", "-EncodedCommand", encodePowerShell(script))
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("targeted TradingView metadata regression: %v: %s", err, output)
	}
}
