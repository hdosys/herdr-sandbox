package sandbox

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

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
