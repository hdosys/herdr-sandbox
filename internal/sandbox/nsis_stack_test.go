package sandbox

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestWinGetMetadataAcceptsExplicitExtensionForSourceForgeDownloadURL(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows PowerShell 5.1 metadata regression")
	}
	basePath := defaultProvisioningPath(t, baseProvisioningName)
	functionSetup := provisioningPowerShellFunctionSetup(t, provisioningPowerShellFunctionSource{
		path:  basePath,
		names: []string{"Get-ProvisioningMetadataValue", "Get-ProvisioningToolVersion", "Get-ProvisioningWinGetMetadata"},
	})
	script := fmt.Sprintf(`$ErrorActionPreference = 'Stop'
%s
$script:arguments = @()
function Invoke-ProvisioningNative {
    param($Role, $FilePath, [object[]]$ArgumentList)
    $script:arguments = @($ArgumentList)
    return @(
        'Found Nullsoft Install System [NSIS.NSIS]',
        'Version: 3.12',
        'Installer:',
        '  Installer Type: nullsoft',
        '  Installer Url: https://sourceforge.net/projects/nsis/files/NSIS%%203/3.12/nsis-3.12-setup.exe/download',
        '  Installer SHA256: 3BC2B06253A7E4957111BE152AC6A536E0C7478A706E19DA814038DB5D706495'
    )
}
$metadata = Get-ProvisioningWinGetMetadata -Role 'NSIS' -Id 'NSIS.NSIS' -Version '3.12' -Architecture 'x86' -InstallerType 'nullsoft' -Scope 'machine' -PayloadExtension '.exe'
$expected = 'show|--id|NSIS.NSIS|--exact|--source|winget|--architecture|x86|--installer-type|nullsoft|--accept-source-agreements|--disable-interactivity|--version|3.12|--scope|machine'
if (($script:arguments -join '|') -cne $expected -or [string]$metadata.Architecture -cne 'x86' -or [string]$metadata.PayloadName -cne 'payload.exe') {
    throw "Explicit x86 metadata was not preserved: $($metadata | ConvertTo-Json -Compress)"
}
$rejected = $false
try {
    Get-ProvisioningWinGetMetadata -Role 'NSIS' -Id 'NSIS.NSIS' -Version '3.12' -Architecture 'x86' -InstallerType 'nullsoft' -Scope 'machine' | Out-Null
} catch {
    if ([string]$_.Exception.Message -notmatch 'unsupported extension') { throw }
    $rejected = $true
}
if (-not $rejected) { throw 'Extensionless installer URL was accepted without an explicit payload extension.' }
`, functionSetup)
	scriptPath := filepath.Join(t.TempDir(), "nsis-metadata-regression.ps1")
	if err := os.WriteFile(scriptPath, []byte(script), 0o600); err != nil {
		t.Fatal(err)
	}
	command := hiddenCommand(mustWindowsPowerShellPath(t), "-NoLogo", "-NoProfile", "-NonInteractive", "-WindowStyle", "Hidden", "-ExecutionPolicy", "Bypass", "-File", scriptPath)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("NSIS metadata regression: %v: %s", err, output)
	}
}
