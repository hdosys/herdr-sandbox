package sandbox

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestAndroidArchiveHelpersValidateInspectedPublisherPayloadsInWindowsPowerShell51(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows PowerShell 5.1 Android payload regression")
	}
	androidRoot := filepath.Join(os.TempDir(), "opencode", "android-inspection", "extract", "cmdline-tools")
	androidExecutable := filepath.Join(androidRoot, "bin", "android.exe")
	if info, err := os.Stat(androidExecutable); err != nil || !info.Mode().IsRegular() {
		t.Skipf("inspected Android payload is unavailable: %s", androidExecutable)
	}
	stackPath := defaultProvisioningPath(t, stackProvisioningName)
	quote := func(value string) string { return strings.ReplaceAll(value, "'", "''") }
	script := fmt.Sprintf(`$ErrorActionPreference = 'Stop'
$tokens = $null
$errors = $null
$ast = [Management.Automation.Language.Parser]::ParseFile('%s', [ref]$tokens, [ref]$errors)
if ($errors.Count -ne 0) { throw $errors[0].Message }
foreach ($name in @('Test-StackAndroidArchiveEntry', 'Assert-StackAndroidTree')) {
    $definition = $ast.Find({ param($node) $node -is [Management.Automation.Language.FunctionDefinitionAst] -and $node.Name -ceq $name }, $true)
    if ($null -eq $definition) { throw "Missing Android helper: $name" }
    Invoke-Expression $definition.Extent.Text
}
Test-StackAndroidArchiveEntry -Entry 'cmdline-tools/bin/android.exe' -Root 'cmdline-tools'
$rejected = $false
try { Test-StackAndroidArchiveEntry -Entry 'cmdline-tools/../escape' -Root 'cmdline-tools' } catch { $rejected = $true }
if (-not $rejected) { throw 'Unsafe Android archive entry was accepted.' }
Assert-StackAndroidTree -Root '%s' -RequiredRelativePaths @('source.properties', 'bin\android.exe', 'bin\sdkmanager.bat', 'lib\sdk-common\tools.sdk-common.jar')
`, quote(stackPath), quote(androidRoot))
	scriptPath := filepath.Join(t.TempDir(), "android-payload-regression.ps1")
	if err := os.WriteFile(scriptPath, []byte(script), 0o600); err != nil {
		t.Fatal(err)
	}
	command := hiddenCommand(mustWindowsPowerShellPath(t), "-NoLogo", "-NoProfile", "-NonInteractive", "-WindowStyle", "Hidden", "-ExecutionPolicy", "Bypass", "-File", scriptPath)
	command.Env = append(os.Environ(), "PSModulePath="+os.Getenv("PSModulePath"))
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("Android payload regression: %v: %s", err, output)
	}
}
