package sandbox

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"herdr-sandbox/internal/hiddenprocess"
)

func TestProvisioningProcessOwnerQuotesRunsParallelAndKillsSiblingTreeInWindowsPowerShell51(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows Job Object provisioning process regression")
	}
	root := t.TempDir()
	baseScript := defaultProvisioningPath(t, baseProvisioningName)
	processSource := filepath.Join(filepath.Dir(baseScript), "..", "internal", "sandbox", "assets", provisioningProcessName)
	processSource, err := filepath.Abs(processSource)
	if err != nil {
		t.Fatal(err)
	}
	powerShell := mustWindowsPowerShellPath(t)
	quote := func(value string) string { return strings.ReplaceAll(value, "'", "''") }
	write := func(name, contents string) string {
		path := filepath.Join(root, name)
		if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}

	argumentFixture := write("arguments.ps1", `$encoded = @($args | ForEach-Object {
    [Convert]::ToBase64String([Text.Encoding]::UTF8.GetBytes([string]$_))
})
[Console]::Out.Write('ARGS:' + ($encoded -join '|'))
`)
	rawOutputFixture := write("raw-output.ps1", `param([string]$Encoded)
$bytes = [Convert]::FromBase64String($Encoded)
$stdout = [Console]::OpenStandardOutput()
$stdout.Write($bytes, 0, $bytes.Length)
$stdout.Flush()
`)
	barrierFixture := write("barrier.ps1", `param([string]$Role, [string]$Root)
[IO.File]::WriteAllText((Join-Path $Root ($Role + '.ready')), 'ready')
$other = if ($Role -ceq 'left') { 'right.ready' } else { 'left.ready' }
$deadline = [DateTime]::UtcNow.AddSeconds(10)
while (-not (Test-Path -LiteralPath (Join-Path $Root $other) -PathType Leaf)) {
    if ([DateTime]::UtcNow -ge $deadline) { exit 71 }
    Start-Sleep -Milliseconds 20
}
[Console]::Out.Write('BARRIER:' + $Role)
`)
	grandchildFixture := write("grandchild.ps1", `param([string]$Root)
[IO.File]::WriteAllText((Join-Path $Root 'grandchild.started'), 'started')
Start-Sleep -Seconds 3
[IO.File]::WriteAllText((Join-Path $Root 'grandchild.survived'), 'survived')
`)
	siblingFixture := write("sibling.ps1", `param([string]$Root, [string]$PowerShell, [string]$Grandchild)
$child = Start-Process -FilePath $PowerShell -ArgumentList @('-NoLogo', '-NoProfile', '-NonInteractive', '-WindowStyle', 'Hidden', '-File', $Grandchild, $Root) -WindowStyle Hidden -PassThru
try {
    $deadline = [DateTime]::UtcNow.AddSeconds(10)
    while (-not (Test-Path -LiteralPath (Join-Path $Root 'grandchild.started') -PathType Leaf)) {
        if ([DateTime]::UtcNow -ge $deadline) { exit 72 }
        Start-Sleep -Milliseconds 20
    }
    [IO.File]::WriteAllText((Join-Path $Root 'sibling.ready'), 'ready')
    Start-Sleep -Seconds 60
} finally {
    if ($null -ne $child) { $child.Dispose() }
}
`)
	failureFixture := write("failure.ps1", `param([string]$Root)
$deadline = [DateTime]::UtcNow.AddSeconds(10)
while (-not (Test-Path -LiteralPath (Join-Path $Root 'sibling.ready') -PathType Leaf)) {
    if ([DateTime]::UtcNow -ge $deadline) { exit 73 }
    Start-Sleep -Milliseconds 20
}
exit 23
`)

	argumentValues := []string{"", "plain", "with space", "Grüße", `quote"value`, `trailing\`, `slashes\\\"quote`}
	encodedValues := make([]string, 0, len(argumentValues))
	for _, value := range argumentValues {
		encodedValues = append(encodedValues, base64.StdEncoding.EncodeToString([]byte(value)))
	}
	expectedArguments := strings.Join(encodedValues, "|")
	powerShellValues := make([]string, 0, len(encodedValues))
	for _, value := range encodedValues {
		powerShellValues = append(powerShellValues, "'"+value+"'")
	}

	harness := fmt.Sprintf(`$ErrorActionPreference = 'Stop'
Add-Type -Path '%s'
$tokens = $null
$errors = $null
$baseAST = [Management.Automation.Language.Parser]::ParseFile('%s', [ref]$tokens, [ref]$errors)
if ($errors.Count -ne 0) { throw $errors[0].Message }
foreach ($name in @('New-ProvisioningNativeSpec', 'Start-ProvisioningNativeGroup', 'Complete-ProvisioningNativeGroup', 'Stop-ProvisioningNativeGroup')) {
    $definition = $baseAST.Find({ param($node) $node -is [Management.Automation.Language.FunctionDefinitionAst] -and $node.Name -ceq $name }, $true)
    if ($null -eq $definition) { throw "Missing Base process function: $name" }
    Invoke-Expression $definition.Extent.Text
}
function Write-ProvisioningProgress { param([string]$Message) }
function Write-ProvisioningTiming { param([string]$Role, [double]$Seconds) }
function Get-ProvisioningBoundedDiagnosticText { param([string]$Text, [int]$MaximumBytes) return $Text }
$script:activeProvisioningNativeGroup = $null
$script:activeProvisioningNativeRoles = @()
function New-Spec([string]$Role, [string[]]$Arguments, [int[]]$Success = @(0), [int]$Timeout = 10000) {
    $spec = New-Object HerdrSandbox.ProvisioningProcessSpec
    $spec.Role = $Role
    $spec.FilePath = '%s'
    $spec.Arguments = $Arguments
    $spec.WorkingDirectory = '%s'
    $spec.TimeoutMilliseconds = $Timeout
    $spec.SuccessExitCodes = $Success
    return $spec
}
$values = @(%s) | ForEach-Object { [Text.Encoding]::UTF8.GetString([Convert]::FromBase64String($_)) }
$argumentSpec = New-Spec -Role 'arguments' -Arguments ([string[]](@('-NoLogo', '-NoProfile', '-NonInteractive', '-File', '%s') + $values))
$argumentResult = [HerdrSandbox.ProvisioningProcess]::Run($argumentSpec)
if (-not $argumentResult.Succeeded -or [string]$argumentResult.Output -notlike '*ARGS:%s*') {
    throw "Argument result failed: $($argumentResult | Format-List | Out-String)"
}

Add-Type -Namespace ProcessOwnerFixture -Name NativeMethods -MemberDefinition @'
[System.Runtime.InteropServices.DllImport("kernel32.dll")]
public static extern uint GetOEMCP();
'@
$nonASCII = 'Gr' + [char]0x00fc + [char]0x00df + 'e'
$utf8Bytes = [Text.Encoding]::UTF8.GetBytes('UTF8:' + $nonASCII)
$utf8Spec = New-Spec -Role 'UTF-8 output' -Arguments @('-NoLogo', '-NoProfile', '-NonInteractive', '-File', '%s', [Convert]::ToBase64String($utf8Bytes))
$utf8Result = [HerdrSandbox.ProvisioningProcess]::Run($utf8Spec)
if (-not $utf8Result.Succeeded -or [string]$utf8Result.Output -cne ('UTF8:' + $nonASCII)) {
    throw "UTF-8 output result failed: $($utf8Result | Format-List | Out-String)"
}
$oemEncoding = [Text.Encoding]::GetEncoding([ProcessOwnerFixture.NativeMethods]::GetOEMCP())
$oemBytes = $oemEncoding.GetBytes('OEM:' + $nonASCII)
$oemSpec = New-Spec -Role 'OEM output' -Arguments @('-NoLogo', '-NoProfile', '-NonInteractive', '-File', '%s', [Convert]::ToBase64String($oemBytes))
$oemResult = [HerdrSandbox.ProvisioningProcess]::Run($oemSpec)
if (-not $oemResult.Succeeded -or [string]$oemResult.Output -cne ('OEM:' + $nonASCII)) {
    throw "OEM output result failed: $($oemResult | Format-List | Out-String)"
}

$stoppedSpec = New-Spec -Role 'owned background stop' -Arguments @('-NoLogo', '-NoProfile', '-NonInteractive', '-Command', 'Start-Sleep -Seconds 30')
$stoppedTask = [HerdrSandbox.ProvisioningProcess]::Start($stoppedSpec)
try { $stoppedResult = $stoppedTask.Stop() } finally { $stoppedTask.Dispose() }
if (-not $stoppedResult.Stopped -or $stoppedResult.Succeeded) {
    throw "Owned background stop result failed: $($stoppedResult | Format-List | Out-String)"
}

$largeCommand = [Convert]::ToBase64String([Text.Encoding]::Unicode.GetBytes("[Console]::Out.Write(('x' * 1200000))"))
$largeSpec = New-Spec -Role 'bounded output' -Arguments @('-NoLogo', '-NoProfile', '-NonInteractive', '-EncodedCommand', $largeCommand)
$largeResult = [HerdrSandbox.ProvisioningProcess]::Run($largeSpec)
if (-not $largeResult.Succeeded -or -not $largeResult.OutputTruncated -or $largeResult.OutputBytes -lt 1200000 -or
    [Text.Encoding]::UTF8.GetByteCount([string]$largeResult.Output) -gt 1050000 -or
    [string]$largeResult.Output -notlike '*output truncated; original bytes:*') {
    throw "Bounded output result failed: $($largeResult | Format-List | Out-String)"
}

$barrierGroup = Start-ProvisioningNativeGroup -Tasks @(
    @{ Role = 'left'; FilePath = '%s'; ArgumentList = @('-NoLogo', '-NoProfile', '-NonInteractive', '-File', '%s', 'left', '%s'); TimeoutSeconds = 10 },
    @{ Role = 'right'; FilePath = '%s'; ArgumentList = @('-NoLogo', '-NoProfile', '-NonInteractive', '-File', '%s', 'right', '%s'); TimeoutSeconds = 10 }
)
try { $barrierResults = @(Complete-ProvisioningNativeGroup -Group $barrierGroup) }
finally { Stop-ProvisioningNativeGroup -Group $barrierGroup }
if ($barrierResults.Count -ne 2 -or @($barrierResults | Where-Object { -not $_.Succeeded }).Count -ne 0 -or
    [string]$barrierResults[0].Output -notlike '*BARRIER:left*' -or [string]$barrierResults[1].Output -notlike '*BARRIER:right*' -or
    $null -ne $script:activeProvisioningNativeGroup -or $script:activeProvisioningNativeRoles.Count -ne 0) {
    throw "Barrier group failed: $($barrierResults | Format-List | Out-String)"
}

$failureSpecs = [HerdrSandbox.ProvisioningProcessSpec[]]@(
    (New-Spec -Role 'failure' -Arguments @('-NoLogo', '-NoProfile', '-NonInteractive', '-File', '%s', '%s')),
    (New-Spec -Role 'sibling' -Arguments @('-NoLogo', '-NoProfile', '-NonInteractive', '-File', '%s', '%s', '%s', '%s'))
)
$failureGroup = [HerdrSandbox.ProvisioningProcess]::StartGroup($failureSpecs)
try { $failureResults = @($failureGroup.Complete()) } finally { $failureGroup.Dispose() }
if ($failureResults.Count -ne 2 -or $failureResults[0].ExitCode -ne 23 -or $failureResults[0].Succeeded -or
    -not $failureResults[1].Stopped -or $failureResults[1].Succeeded) {
    throw "Failure group result is invalid: $($failureResults | Format-List | Out-String)"
}
Start-Sleep -Milliseconds 3500
if (Test-Path -LiteralPath (Join-Path '%s' 'grandchild.survived') -PathType Leaf) {
    throw 'Cancelled sibling grandchild survived its task Job Object.'
}

$timeoutSpec = New-Spec -Role 'timeout' -Arguments @('-NoLogo', '-NoProfile', '-NonInteractive', '-Command', 'Start-Sleep -Seconds 30') -Timeout 1000
$timeoutResult = [HerdrSandbox.ProvisioningProcess]::Run($timeoutSpec)
if (-not $timeoutResult.TimedOut -or $timeoutResult.Succeeded) {
    throw "Timeout result is invalid: $($timeoutResult | Format-List | Out-String)"
}
`, quote(processSource), quote(baseScript), quote(powerShell), quote(root), strings.Join(powerShellValues, ", "), quote(argumentFixture), expectedArguments,
		quote(rawOutputFixture), quote(rawOutputFixture),
		quote(powerShell), quote(barrierFixture), quote(root), quote(powerShell), quote(barrierFixture), quote(root), quote(failureFixture), quote(root),
		quote(siblingFixture), quote(root), quote(powerShell), quote(grandchildFixture), quote(root))
	harnessPath := write("process-owner-regression.ps1", harness)
	command := hiddenCommand(powerShell, "-NoLogo", "-NoProfile", "-NonInteractive", "-WindowStyle", "Hidden", "-ExecutionPolicy", "Bypass", "-File", harnessPath)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("provisioning process owner regression: %v: %s", err, output)
	}
}

func TestProvisioningProcessOwnerKillsTreeWhenOwningPowerShellExits(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows Job Object parent-exit regression")
	}
	root := t.TempDir()
	baseScript := defaultProvisioningPath(t, baseProvisioningName)
	processSource := filepath.Join(filepath.Dir(baseScript), "..", "internal", "sandbox", "assets", provisioningProcessName)
	processSource, err := filepath.Abs(processSource)
	if err != nil {
		t.Fatal(err)
	}
	powerShell := mustWindowsPowerShellPath(t)
	quote := func(value string) string { return strings.ReplaceAll(value, "'", "''") }
	childPath := filepath.Join(root, "parent-exit-child.ps1")
	child := `param([string]$Root)
[IO.File]::WriteAllText((Join-Path $Root 'parent-exit-child.started'), 'started')
Start-Sleep -Seconds 3
[IO.File]::WriteAllText((Join-Path $Root 'parent-exit-child.survived'), 'survived')
`
	if err := os.WriteFile(childPath, []byte(child), 0o600); err != nil {
		t.Fatal(err)
	}
	ownerPath := filepath.Join(root, "parent-exit-owner.ps1")
	owner := fmt.Sprintf(`$ErrorActionPreference = 'Stop'
Add-Type -Path '%s'
$spec = New-Object HerdrSandbox.ProvisioningProcessSpec
$spec.Role = 'parent exit child'
$spec.FilePath = '%s'
$spec.Arguments = [string[]]@('-NoLogo', '-NoProfile', '-NonInteractive', '-File', '%s', '%s')
$spec.WorkingDirectory = '%s'
$spec.TimeoutMilliseconds = 30000
$spec.SuccessExitCodes = [int[]]@(0)
$task = [HerdrSandbox.ProvisioningProcess]::Start($spec)
$deadline = [DateTime]::UtcNow.AddSeconds(10)
while (-not (Test-Path -LiteralPath (Join-Path '%s' 'parent-exit-child.started') -PathType Leaf)) {
    if ([DateTime]::UtcNow -ge $deadline) { throw 'Parent-exit child did not start.' }
    Start-Sleep -Milliseconds 20
}
[IO.File]::WriteAllText((Join-Path '%s' 'parent-exit-owner.ready'), 'ready')
Start-Sleep -Seconds 60
`, quote(processSource), quote(powerShell), quote(childPath), quote(root), quote(root), quote(root), quote(root))
	if err := os.WriteFile(ownerPath, []byte(owner), 0o600); err != nil {
		t.Fatal(err)
	}

	command := exec.Command(powerShell, "-NoLogo", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File", ownerPath)
	hiddenprocess.Configure(command)
	var output bytes.Buffer
	command.Stdout = &output
	command.Stderr = &output
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	waited := false
	defer func() {
		if !waited {
			_ = command.Process.Kill()
			_ = command.Wait()
		}
	}()
	readyPath := filepath.Join(root, "parent-exit-owner.ready")
	deadline := time.Now().Add(15 * time.Second)
	for {
		if _, err := os.Stat(readyPath); err == nil {
			break
		} else if !os.IsNotExist(err) {
			t.Fatal(err)
		}
		if time.Now().After(deadline) {
			t.Fatalf("owning PowerShell did not become ready: %s", output.String())
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err := command.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = command.Wait()
	waited = true
	time.Sleep(4 * time.Second)
	if _, err := os.Stat(filepath.Join(root, "parent-exit-child.survived")); err == nil {
		t.Fatal("child survived abrupt termination of the owning PowerShell process")
	} else if !os.IsNotExist(err) {
		t.Fatal(err)
	}
}
