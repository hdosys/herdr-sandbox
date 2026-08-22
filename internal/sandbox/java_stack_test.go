package sandbox

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestJavaStackVersionAndUpgradeContractsInWindowsPowerShell51(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows PowerShell 5.1 Java upgrade regression")
	}
	stackPath := defaultProvisioningPath(t, stackProvisioningName)
	quote := func(value string) string { return strings.ReplaceAll(value, "'", "''") }
	script := fmt.Sprintf(`$ErrorActionPreference = 'Stop'
trap { Write-Output ($_ | Out-String); exit 1 }
$tokens = $null
$errors = $null
$ast = [Management.Automation.Language.Parser]::ParseFile('%s', [ref]$tokens, [ref]$errors)
if ($errors.Count -ne 0) { throw $errors[0].Message }
foreach ($name in @('Get-StackJavaInstalledVersions', 'Remove-StackJavaPreviousInstallation', 'Assert-StackJavaVersion')) {
    $definition = $ast.Find({ param($node) $node -is [Management.Automation.Language.FunctionDefinitionAst] -and $node.Name -ceq $name }, $true)
    if ($null -eq $definition) { throw "Missing Java upgrade function: $name" }
    Invoke-Expression $definition.Extent.Text
}
function ConvertFrom-ProvisioningNativeOutput {
    param([string]$Text)
    if ([string]::IsNullOrEmpty($Text)) { return @() }
    return @($Text -split '\r?\n')
}
$script:versions = @()
$script:uninstallCalls = 0
$script:uninstallArguments = @()
function Invoke-ProvisioningNativeResult {
    param($Role, $FilePath, [object[]]$ArgumentList, $TimeoutSeconds)
    $rows = @('Name Id Version Source', '----------------------')
    foreach ($version in $script:versions) {
        $rows += "Microsoft Build of OpenJDK Microsoft.OpenJDK.25 $version winget"
    }
    return [pscustomobject]@{ ExitCode = 0; Output = ($rows -join [Environment]::NewLine) }
}
function Invoke-ProvisioningNative {
    param($Role, $FilePath, [object[]]$ArgumentList, $TimeoutSeconds)
    $script:uninstallCalls += 1
    $script:uninstallArguments = @($ArgumentList)
    $script:versions = @()
    return @()
}
$javaVersion = @'
openjdk version "25.0.4.1" 2026-08-18 LTS
OpenJDK Runtime Environment Microsoft-14951867 (build 25.0.4.1+1-LTS)
'@
Assert-StackJavaVersion -LanguageVersion '25.0.4' -JavaVersion $javaVersion.Trim() -JavacVersion 'javac 25.0.4.1'
Assert-StackJavaVersion -LanguageVersion '25.0.4' -JavaVersion 'openjdk version "25.0.4" LTS Microsoft' -JavacVersion 'javac 25.0.4'
$rejected = $false
try {
    Assert-StackJavaVersion -LanguageVersion '25.0.4' -JavaVersion $javaVersion.Trim() -JavacVersion 'javac 25.0.4'
} catch {
    $rejected = $_.Exception.Message -like 'Microsoft OpenJDK version verification failed:*'
}
if (-not $rejected) { throw 'Mismatched Java runtime and compiler versions were accepted.' }
$metadata = [pscustomobject]@{ Id = 'Microsoft.OpenJDK.25'; Version = '25.0.4.7' }
$script:versions = @('25.0.3.7')
Remove-StackJavaPreviousInstallation -Metadata $metadata
$expected = 'uninstall|--id|Microsoft.OpenJDK.25|--exact|--source|winget|--scope|machine|--all-versions|--silent|--accept-source-agreements|--disable-interactivity'
if ($script:uninstallCalls -ne 1 -or ($script:uninstallArguments -join '|') -cne $expected) {
    throw 'Previous Java version did not use the exact bounded WinGet uninstall contract.'
}
$script:versions = @('25.0.4.7')
Remove-StackJavaPreviousInstallation -Metadata $metadata
if ($script:uninstallCalls -ne 1) { throw 'Matching Java version was uninstalled.' }
$script:versions = @('25.0.3.7', '25.0.4.7')
Remove-StackJavaPreviousInstallation -Metadata $metadata
if ($script:uninstallCalls -ne 2) { throw 'Mixed Java versions were not converged before install.' }
`, quote(stackPath))
	scriptPath := filepath.Join(t.TempDir(), "java-upgrade-regression.ps1")
	if err := os.WriteFile(scriptPath, []byte(script), 0o600); err != nil {
		t.Fatal(err)
	}
	command := hiddenCommand(mustWindowsPowerShellPath(t), "-NoLogo", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File", scriptPath)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("Java upgrade regression: %v: %s", err, output)
	}
}
