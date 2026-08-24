package sandbox

import (
	"fmt"
	"runtime"
	"testing"
)

func TestTradingViewPortableMetadataDelegatesStableWinGetResolutionInWindowsPowerShell51(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows PowerShell 5.1 metadata regression")
	}
	functionSetup := provisioningPowerShellFunctionSetup(t,
		provisioningPowerShellFunctionSource{
			path:  defaultProvisioningPath(t, stackProvisioningName),
			names: []string{"Get-TradingViewDesktopPortableMetadata"},
		},
	)
	script := fmt.Sprintf(`$ErrorActionPreference = 'Stop'
%s
$script:metadataCalls = @()
function Get-ProvisioningWinGetMetadata {
    param([string]$Role, [string]$Id, [string]$Version, [string]$Architecture,
        [string]$InstallerType, [string]$PayloadExtension)
    $script:metadataCalls += ,@($Role, $Id, $Version, $Architecture, $InstallerType, $PayloadExtension)
    $resolved = if ([string]::IsNullOrWhiteSpace($Version)) { '4.1.2.9000' } else { $Version }
    return [pscustomobject]@{ Id = $Id; Version = $resolved; Architecture = $Architecture;
        InstallerType = $InstallerType; Url = 'https://tvd-packages.tradingview.com/release.msix';
        Sha256 = ('A' * 64); PayloadName = 'payload.msix' }
}
$metadata = Get-TradingViewDesktopPortableMetadata
if ([string]$metadata.Id -cne 'TradingView.TradingViewDesktop' -or
    [string]$metadata.Version -cne '4.1.2.9000' -or
    ($script:metadataCalls[0] -join '|') -cne 'TradingView Desktop|TradingView.TradingViewDesktop||x64|msix|.msix') {
    throw "Unexpected TradingView metadata: $($metadata | ConvertTo-Json -Compress)"
}
$explicit = Get-TradingViewDesktopPortableMetadata -Version '4.0.0.8000'
if ([string]$explicit.Version -cne '4.0.0.8000' -or $script:metadataCalls.Count -ne 2 -or
    [string]$script:metadataCalls[1][2] -cne '4.0.0.8000') {
    throw 'TradingView explicit version was not passed to the shared metadata owner.'
}
`, functionSetup)
	command := hiddenCommand(mustWindowsPowerShellPath(t), "-NoLogo", "-NoProfile", "-NonInteractive", "-EncodedCommand", encodePowerShell(script))
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("TradingView metadata delegation regression: %v: %s", err, output)
	}
}

func TestWinGetMetadataJoinsWrappedTradingViewInstallerFieldsInWindowsPowerShell51(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows PowerShell 5.1 metadata regression")
	}
	functionSetup := provisioningPowerShellFunctionSetup(t,
		provisioningPowerShellFunctionSource{
			path: defaultProvisioningPath(t, baseProvisioningName),
			names: []string{
				"Get-ProvisioningMetadataValue",
				"Get-ProvisioningToolVersion",
				"Get-ProvisioningWinGetMetadata",
			},
		},
	)
	script := fmt.Sprintf(`$ErrorActionPreference = 'Stop'
%s
function Invoke-ProvisioningNative {
    return @(
        'Found TradingView [TradingView.TradingViewDesktop]',
        'Version: 3.3.0.7992',
        'Installer:',
        '  Installer Type: msix',
        '  Installer Url:',
        '    https://tvd-packages.tradingview.com/stable/latest/win32/TradingView.msix',
        '  Installer SHA256: 96B5EBC196A3824EF22667BA9AE1A6AB',
        '    92E83B70615D0AFE96031AB11C6CE6DF'
    )
}
$metadata = Get-ProvisioningWinGetMetadata -Role 'TradingView Desktop' -Id 'TradingView.TradingViewDesktop' -Version '3.3.0.7992' -Architecture 'x64' -InstallerType 'msix' -PayloadExtension '.msix'
if ([string]$metadata.Url -cne 'https://tvd-packages.tradingview.com/stable/latest/win32/TradingView.msix' -or
    [string]$metadata.Sha256 -cne '96B5EBC196A3824EF22667BA9AE1A6AB92E83B70615D0AFE96031AB11C6CE6DF' -or
    [string]$metadata.PayloadName -cne 'payload.msix') {
    throw "Wrapped TradingView metadata was not normalized: $($metadata | ConvertTo-Json -Compress)"
}
`, functionSetup)
	command := hiddenCommand(mustWindowsPowerShellPath(t), "-NoLogo", "-NoProfile", "-NonInteractive", "-EncodedCommand", encodePowerShell(script))
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("wrapped TradingView metadata regression: %v: %s", err, output)
	}
}
