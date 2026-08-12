package sandbox

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestNSISStackInstallsAndExercisesLatestStableCompiler(t *testing.T) {
	text := readDefaultStackProvisioning(t)
	start := strings.Index(text, "function Install-NSISStack")
	end := strings.Index(text, "function Install-GoStack")
	if start < 0 || end <= start {
		t.Fatal("NSIS stack owner is missing")
	}
	section := text[start:end]
	for _, required := range []string{
		"$packageID = 'NSIS.NSIS'",
		"-Architecture 'x86' -InstallerType 'nullsoft' -Scope 'machine' -PayloadExtension '.exe'",
		"-DownloadSource 'WinGet'",
		"-Adapter 'NSIS'",
		"Join-Path ${env:ProgramFiles(x86)} 'NSIS'",
		"Join-Path $installRoot 'makensis.exe'",
		"Add-ProvisioningMachinePath -Directory $installRoot",
		"NSIS compiler version verification",
		"-ArgumentList @('/VERSION') -TimeoutSeconds 30",
		"NSIS compiler probe",
		"@('/WX', '/V2', '/NOCONFIG', $scriptPath)",
		"RequestExecutionLevel user",
		"SilentInstall silent",
		"NSIS compiler probe produced an invalid Windows executable",
	} {
		if !strings.Contains(section, required) {
			t.Errorf("NSIS stack is missing %q", required)
		}
	}
	for _, forbidden := range []string{"3.12", "NSIS_URL", "curl.exe", "Invoke-WebRequest"} {
		if strings.Contains(section, forbidden) {
			t.Errorf("NSIS stack contains a pinned or alternate package path %q", forbidden)
		}
	}
	if effectiveStackPackageOwner(stackNSIS) != packageNSIS || !projectStackOwnsPackage(packageNSIS) {
		t.Fatalf("NSIS package owner = %q, reserved = %t", effectiveStackPackageOwner(stackNSIS), projectStackOwnsPackage(packageNSIS))
	}
}

func TestCachedWinGetMetadataSupportsExplicitX86InstallerArchitecture(t *testing.T) {
	base := readDefaultBaseProvisioning(t)
	for _, required := range []string{
		"[ValidateSet('x64', 'x86')]",
		"[string]$Architecture = 'x64'",
		"'--architecture', $Architecture",
		"Architecture = $Architecture",
		"[string]$PayloadExtension = ''",
		"-Architecture $Architecture -InstallerType $InstallerType -Scope $Scope",
		"[ValidateSet('Exe', 'Inno', 'NSIS', 'MSI', 'Burn', 'MSIX', 'Portable', 'Rustup'",
		"'NSIS' {",
		"-ArgumentList @('/S') -WaitForProcessTree",
	} {
		if !strings.Contains(base, required) {
			t.Errorf("cached WinGet owner is missing %q", required)
		}
	}
}

func TestWinGetMetadataAcceptsExplicitExtensionForSourceForgeDownloadURL(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows PowerShell 5.1 metadata regression")
	}
	basePath := defaultProvisioningPath(t, baseProvisioningName)
	quote := func(value string) string { return strings.ReplaceAll(value, "'", "''") }
	script := fmt.Sprintf(`$ErrorActionPreference = 'Stop'
$tokens = $null
$errors = $null
$ast = [Management.Automation.Language.Parser]::ParseFile('%s', [ref]$tokens, [ref]$errors)
if ($errors.Count -ne 0) { throw $errors[0].Message }
foreach ($name in @('Get-ProvisioningMetadataValue', 'Get-ProvisioningWinGetMetadata')) {
    $definition = $ast.Find({ param($node) $node -is [Management.Automation.Language.FunctionDefinitionAst] -and $node.Name -ceq $name }, $true)
    if ($null -eq $definition) { throw "Missing metadata function: $name" }
    Invoke-Expression $definition.Extent.Text
}
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
`, quote(basePath))
	scriptPath := filepath.Join(t.TempDir(), "nsis-metadata-regression.ps1")
	if err := os.WriteFile(scriptPath, []byte(script), 0o600); err != nil {
		t.Fatal(err)
	}
	command := hiddenCommand(mustWindowsPowerShellPath(t), "-NoLogo", "-NoProfile", "-NonInteractive", "-WindowStyle", "Hidden", "-ExecutionPolicy", "Bypass", "-File", scriptPath)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("NSIS metadata regression: %v: %s", err, output)
	}
}
