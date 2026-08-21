package sandbox

import (
	"fmt"
	"runtime"
	"testing"
)

func TestTradingViewPortableMetadataUsesReleasePinInWindowsPowerShell51(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows PowerShell 5.1 metadata regression")
	}
	functionSetup := provisioningPowerShellFunctionSetup(t,
		provisioningPowerShellFunctionSource{
			path:  defaultProvisioningPath(t, baseProvisioningName),
			names: []string{"Get-ProvisioningToolVersion"},
		},
		provisioningPowerShellFunctionSource{
			path:  defaultProvisioningPath(t, stackProvisioningName),
			names: []string{"Get-TradingViewDesktopPortableMetadata"},
		},
	)
	script := fmt.Sprintf(`$ErrorActionPreference = 'Stop'
%s
$metadata = Get-TradingViewDesktopPortableMetadata
if ([string]$metadata.Id -cne 'TradingView.TradingViewDesktop' -or
    [string]$metadata.Version -cne '3.3.0.7992' -or
    [string]$metadata.Url -cne 'https://tvd-packages.tradingview.com/stable/latest/win32/TradingView.msix' -or
    [string]$metadata.Sha256 -cne '96B5EBC196A3824EF22667BA9AE1A6AB92E83B70615D0AFE96031AB11C6CE6DF' -or
    [string]$metadata.PayloadName -cne 'payload.msix') {
    throw "Unexpected TradingView metadata: $($metadata | ConvertTo-Json -Compress)"
}
$rejected = $false
try {
    Get-TradingViewDesktopPortableMetadata -Version '3.3.0.7993' | Out-Null
} catch {
    if ([string]$_.Exception.Message -notmatch 'version 3\.3\.0\.7993 is unsupported') { throw }
    $rejected = $true
}
if (-not $rejected) { throw 'TradingView metadata accepted a version outside this release pin.' }
`, functionSetup)
	command := hiddenCommand(mustWindowsPowerShellPath(t), "-NoLogo", "-NoProfile", "-NonInteractive", "-EncodedCommand", encodePowerShell(script))
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("TradingView pinned metadata regression: %v: %s", err, output)
	}
}
