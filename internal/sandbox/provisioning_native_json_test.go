package sandbox

import (
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// Keep the structured-output regression in the fast gate, without the unrelated
// process-group, descendant cleanup and timeout matrix.
func TestProvisioningNativeJSONSeparatesDiagnosticsInWindowsPowerShell51(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows PowerShell 5.1 native output boundary")
	}
	powerShell, err := windowsPowerShellExecutable()
	if err != nil {
		t.Fatal(err)
	}
	base := defaultProvisioningPath(t, baseProvisioningName)
	processSource := filepath.Join(filepath.Dir(base), "..", "internal", "sandbox", "assets", provisioningProcessName)
	quote := func(value string) string { return strings.ReplaceAll(value, "'", "''") }
	script := fmt.Sprintf(`$ErrorActionPreference = 'Stop'
$env:HERDR_SANDBOX_STATUS_DIRECTORY = ''
. '%s' -Phase Library -ProcessOwnerPath '%s'
$powerShell = '%s'
$jsonCommand = [Convert]::ToBase64String([Text.Encoding]::Unicode.GetBytes('[Console]::Out.WriteLine(''"0.23.1"''); [Console]::Error.WriteLine(''npm notice New major version of npm available!'')'))
$arguments = @('-NoLogo', '-NoProfile', '-NonInteractive', '-WindowStyle', 'Hidden', '-EncodedCommand', $jsonCommand)
$jsonWarnings = @()
$jsonOutput = Invoke-ProvisioningNative -Role 'JSON metadata' -FilePath $powerShell -ArgumentList $arguments -SeparateErrorOutput -WarningVariable jsonWarnings -WarningAction SilentlyContinue -TimeoutSeconds 10
if (($jsonOutput | ConvertFrom-Json) -cne '0.23.1' -or ($jsonWarnings -join '') -notlike '*npm notice*') { throw 'JSON stdout or stderr warning was lost.' }
# Negative control: the same real process must reproduce the original parser
# failure when its two streams are deliberately combined.
$combined = (Invoke-ProvisioningNative -Role 'Combined metadata' -FilePath $powerShell -ArgumentList $arguments -TimeoutSeconds 10) -join [Environment]::NewLine
$rejected = $false
try { $combined | ConvertFrom-Json | Out-Null } catch { $rejected = $true }
if (-not $rejected) { throw 'Combined metadata unexpectedly parsed as JSON.' }
$failureCommand = [Convert]::ToBase64String([Text.Encoding]::Unicode.GetBytes('[Console]::Error.WriteLine(''registry unavailable''); exit 23'))
$failureMessage = ''
try {
    Invoke-ProvisioningNative -Role 'JSON metadata failure' -FilePath $powerShell -ArgumentList @('-NoLogo', '-NoProfile', '-NonInteractive', '-WindowStyle', 'Hidden', '-EncodedCommand', $failureCommand) -SeparateErrorOutput -TimeoutSeconds 10 | Out-Null
} catch { $failureMessage = $_.Exception.Message }
if ($failureMessage -notlike '*exit code 23*registry unavailable*') { throw "Native failure diagnostic was lost: $failureMessage" }
`, quote(base), quote(processSource), quote(powerShell))
	command := hiddenCommand(powerShell, "-NoLogo", "-NoProfile", "-NonInteractive", "-WindowStyle", "Hidden", "-ExecutionPolicy", "Bypass", "-EncodedCommand", encodePowerShell(script))
	command.Dir = t.TempDir()
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("native structured output regression: %v: %s", err, output)
	}
}
