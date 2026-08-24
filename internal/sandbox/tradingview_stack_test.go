package sandbox

import (
	"archive/zip"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestTradingViewPackageMetadataUsesSignedAppxIdentityInWindowsPowerShell51(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows PowerShell 5.1 metadata regression")
	}
	payloadPath := filepath.Join(t.TempDir(), "TradingView.msix")
	payload, err := os.Create(payloadPath)
	if err != nil {
		t.Fatal(err)
	}
	archive := zip.NewWriter(payload)
	manifest, err := archive.Create("AppxManifest.xml")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(manifest, `<Package xmlns="http://schemas.microsoft.com/appx/manifest/foundation/windows10"><Identity Name="TradingView.Desktop" Publisher='CN=&quot;TradingView, Inc.&quot;, O=&quot;TradingView, Inc.&quot;, S=Ohio, C=US' Version="3.3.0.7992" ProcessorArchitecture="x64" /></Package>`); err != nil {
		t.Fatal(err)
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	if err := payload.Close(); err != nil {
		t.Fatal(err)
	}
	payloadBytes, err := os.ReadFile(payloadPath)
	if err != nil {
		t.Fatal(err)
	}
	payloadSHA256 := fmt.Sprintf("%X", sha256.Sum256(payloadBytes))
	quote := func(value string) string { return "'" + strings.ReplaceAll(value, "'", "''") + "'" }
	functionSetup := provisioningPowerShellFunctionSetup(t,
		provisioningPowerShellFunctionSource{
			path:  defaultProvisioningPath(t, stackProvisioningName),
			names: []string{"Get-TradingViewDesktopPackageMetadata"},
		},
	)
	script := fmt.Sprintf(`$ErrorActionPreference = 'Stop'
%s
function Get-AuthenticodeSignature {
    param([string]$LiteralPath)
    return [pscustomobject]@{
        Status = [System.Management.Automation.SignatureStatus]::Valid
        SignerCertificate = [pscustomobject]@{ Subject = 'CN="TradingView, Inc.", O="TradingView, Inc.", S=Ohio, C=US' }
    }
}
$metadata = Get-TradingViewDesktopPackageMetadata -PayloadPath %s
if ([string]$metadata.Id -cne 'TradingView.TradingViewDesktop' -or
    [string]$metadata.Version -cne '3.3.0.7992' -or
    [string]$metadata.Architecture -cne 'x64' -or
    [string]$metadata.InstallerType -cne 'msix' -or
    [string]$metadata.Url -cne 'https://tvd-packages.tradingview.com/stable/latest/win32/TradingView.msix' -or
    [string]$metadata.Sha256 -cne '%s') {
    throw "Unexpected direct TradingView metadata: $($metadata | ConvertTo-Json -Compress)"
}
`, functionSetup, quote(payloadPath), payloadSHA256)
	command := hiddenCommand(mustWindowsPowerShellPath(t), "-NoLogo", "-NoProfile", "-NonInteractive", "-EncodedCommand", encodePowerShell(script))
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("TradingView signed Appx identity regression: %v: %s", err, output)
	}
}
