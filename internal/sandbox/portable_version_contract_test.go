package sandbox

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestPortablePackageVersionSourcesDefaultAndOverrides(t *testing.T) {
	base := readDefaultBaseProvisioning(t)
	for _, required := range []string{
		"[string[]]$PortableVersionArguments = @('--version')",
		"[ValidateSet('Command', 'File')]",
		"[string]$PortableVersionSource = 'Command'",
		"-FilePath $commands[0].FullName -ArgumentList $VersionArguments",
		"[string]$commands[0].VersionInfo.FileVersion -cne [string]$Metadata.Version",
		"-VersionSource $PortableVersionSource",
		"-PortableVersionArguments $PortableVersionArguments",
	} {
		if !strings.Contains(base, required) {
			t.Fatalf("portable package version contract is missing %q", required)
		}
	}
	payloadStart := strings.Index(base, "function Install-ProvisioningPackagePayload")
	cachedStart := strings.Index(base, "function Install-ProvisioningCachedPackage")
	winGetStart := strings.Index(base, "function Install-ProvisioningWinGetPackage")
	if payloadStart < 0 || cachedStart <= payloadStart || winGetStart <= cachedStart {
		t.Fatal("portable package function ordering is unavailable")
	}
	if strings.Contains(base[payloadStart:cachedStart], "$PortableVersionArguments") {
		t.Fatal("payload helper owns the post-install portable version arguments")
	}
	if !strings.Contains(base[cachedStart:winGetStart], "[string[]]$PortableVersionArguments = @('--version')") {
		t.Fatal("cached-package helper does not own the portable version arguments")
	}
	stacks := readDefaultStackProvisioning(t)
	if !strings.Contains(stacks, "-PortableVersionArguments @('version')") {
		t.Fatal("Zig does not select its supported version subcommand")
	}
	if !strings.Contains(stacks, "-PortableVersionSource 'File'") {
		t.Fatal("TradingView does not select portable file-version readback")
	}
}

func TestPortableFileVersionReadbackDoesNotLaunchGUIBinaryInWindowsPowerShell51(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows PowerShell 5.1 portable file-version regression")
	}
	root := t.TempDir()
	packageRoot := filepath.Join(root, "Fixture.Package")
	if err := os.MkdirAll(packageRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	commandData, err := os.ReadFile(filepath.Join(os.Getenv("WINDIR"), "System32", "cmd.exe"))
	if err != nil {
		t.Fatal(err)
	}
	fixture := filepath.Join(packageRoot, "fixture.exe")
	if err := os.WriteFile(fixture, commandData, 0o700); err != nil {
		t.Fatal(err)
	}
	quote := func(value string) string { return strings.ReplaceAll(value, "'", "''") }
	script := fmt.Sprintf(`$ErrorActionPreference = 'Stop'
$tokens = $null
$errors = $null
$ast = [Management.Automation.Language.Parser]::ParseFile('%s', [ref]$tokens, [ref]$errors)
$definition = $ast.Find({ param($node) $node -is [Management.Automation.Language.FunctionDefinitionAst] -and $node.Name -ceq 'Test-ProvisioningPortablePackageInstalled' }, $true)
if ($null -eq $definition) { throw 'Portable package inspection function is missing.' }
Invoke-Expression $definition.Extent.Text.Replace('C:\HerdrSandbox\tools', '%s')
function Get-ProvisioningSafeCacheName { param([string]$Value) return $Value }
function Add-ProvisioningMachinePath { param([string]$Directory) $env:PATH = $Directory + ';' + $env:PATH }
function Invoke-ProvisioningNativeResult { throw 'File-version readback launched the executable.' }
function ConvertFrom-ProvisioningNativeOutput { throw 'File-version readback parsed command output.' }
$fixture = Get-Item -LiteralPath '%s' -Force
$metadata = [pscustomobject]@{ Id = 'Fixture.Package'; Version = [string]$fixture.VersionInfo.FileVersion }
if (-not (Test-ProvisioningPortablePackageInstalled -Metadata $metadata -ExecutableName 'fixture.exe' -VersionSource 'File')) {
    throw 'Matching portable file version was rejected.'
}
$metadata.Version = '0.0.0.0'
if (Test-ProvisioningPortablePackageInstalled -Metadata $metadata -ExecutableName 'fixture.exe' -VersionSource 'File') {
    throw 'Mismatched portable file version was accepted.'
}
`, quote(defaultProvisioningPath(t, baseProvisioningName)), quote(root), quote(fixture))
	command := hiddenCommand(mustWindowsPowerShellPath(t), "-NoLogo", "-NoProfile", "-NonInteractive", "-EncodedCommand", encodePowerShell(script))
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("portable file-version regression: %v: %s", err, output)
	}
}
