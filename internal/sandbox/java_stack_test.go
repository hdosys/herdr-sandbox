package sandbox

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestJavaStackInstallsMicrosoftOpenJDK25LTSAndCompilesAProgram(t *testing.T) {
	text := readDefaultStackProvisioning(t)
	start := strings.Index(text, "function Get-StackJavaInstalledVersions")
	end := strings.Index(text, "function Install-GoStack")
	if start < 0 || end <= start {
		t.Fatal("Java stack owner is missing")
	}
	section := text[start:end]
	for _, required := range []string{
		"$packageID = 'Microsoft.OpenJDK.25'",
		"-InstallerType 'wix' -Scope 'machine'",
		"-DownloadSource 'WinGet' -Adapter 'MSI' -ExecutableName 'java.exe'",
		"ADDLOCAL=FeatureMain,FeatureEnvironment,FeatureJavaHome",
		"-RequireAuthenticodeSignature",
		"Remove-StackJavaPreviousInstallation -Metadata $metadata",
		"'uninstall', '--id', [string]$Metadata.Id, '--exact', '--source', 'winget'",
		"'--scope', 'machine', '--all-versions', '--silent'",
		"Microsoft OpenJDK 25 previous-version uninstall",
		"[Environment]::GetEnvironmentVariable('JAVA_HOME', 'Machine')",
		"Join-Path $javaBin 'java.exe'",
		"Join-Path $javaBin 'javac.exe'",
		"Java runtime version verification",
		"Java compiler version verification",
		"Java compiler probe",
		"Java runtime probe",
		"HerdrJavaStackProbe.java",
		"java-stack-ok",
	} {
		if !strings.Contains(section, required) {
			t.Errorf("Java stack is missing %q", required)
		}
	}
	for _, forbidden := range []string{
		"Microsoft.OpenJDK.11", "Microsoft.OpenJDK.17", "Microsoft.OpenJDK.21",
		"EclipseAdoptium", "Oracle.JDK", "JAVA_HOME = 'C:", "25.0.4.7", "'--force'",
	} {
		if strings.Contains(section, forbidden) {
			t.Errorf("Java stack contains a legacy, alternate, or pinned path %q", forbidden)
		}
	}
	if effectiveStackPackageOwner(stackJava) != "Microsoft.OpenJDK.25" {
		t.Fatalf("Java package owner = %q", effectiveStackPackageOwner(stackJava))
	}
}

func TestJavaStackUninstallsPreviousVersionsBeforeInstallInWindowsPowerShell51(t *testing.T) {
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
foreach ($name in @('Get-StackJavaInstalledVersions', 'Remove-StackJavaPreviousInstallation')) {
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
    param($Role, $FilePath, [object[]]$ArgumentList, $TimeoutSeconds, [switch]$WaitForProcessTree)
    $script:uninstallCalls += 1
    $script:uninstallArguments = @($ArgumentList)
    $script:versions = @()
    return @()
}
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
