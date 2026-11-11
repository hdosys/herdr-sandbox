package sandbox

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestProvisioningProcessOwnerQuotesRunsParallelAndIsolatesTaskFailuresInWindowsPowerShell51(t *testing.T) {
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
	grandchildFixture := write("grandchild.ps1", `param([string]$Root, [int]$Seconds = 3)
[IO.File]::WriteAllText((Join-Path $Root 'grandchild.pid'), [string]$PID)
[IO.File]::WriteAllText((Join-Path $Root 'grandchild.started'), 'started')
Start-Sleep -Seconds $Seconds
[IO.File]::WriteAllText((Join-Path $Root 'grandchild.survived'), 'survived')
`)
	siblingFixture := write("sibling.ps1", `param([string]$Root, [string]$PowerShell, [string]$Grandchild, [string]$Mode = 'complete')
$duration = if ($Mode -eq 'hold') { 60 } else { 3 }
$child = Start-Process -FilePath $PowerShell -ArgumentList @('-NoLogo', '-NoProfile', '-NonInteractive', '-WindowStyle', 'Hidden', '-File', $Grandchild, $Root, $duration) -WindowStyle Hidden -PassThru
try {
    $deadline = [DateTime]::UtcNow.AddSeconds(10)
    while (-not (Test-Path -LiteralPath (Join-Path $Root 'grandchild.started') -PathType Leaf)) {
        if ([DateTime]::UtcNow -ge $deadline) { exit 72 }
        Start-Sleep -Milliseconds 20
    }
    [IO.File]::WriteAllText((Join-Path $Root 'sibling.ready'), 'ready')
    if ($Mode -eq 'hold') { Start-Sleep -Seconds 60 }
    if (-not $child.WaitForExit(10000) -or $child.ExitCode -ne 0) { exit 74 }
    [IO.File]::WriteAllText((Join-Path $Root 'sibling.completed'), 'complete')
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
	timeoutWitness := write("timeout-witness.ps1", `param([string]$Root)
$deadline = [DateTime]::UtcNow.AddSeconds(10)
while (-not (Test-Path -LiteralPath (Join-Path $Root 'sibling.ready'))) {
    if ([DateTime]::UtcNow -ge $deadline) { exit 75 }
    Start-Sleep -Milliseconds 20
}
$childID = [int][IO.File]::ReadAllText((Join-Path $Root 'grandchild.pid'))
$child = Get-Process -Id $childID -ErrorAction Stop
try { if (-not $child.WaitForExit(20000)) { exit 76 } } finally { $child.Dispose() }
[IO.File]::WriteAllText((Join-Path $Root 'independent.completed'), 'complete')
`)
	groupCallerFixture := write("group-caller.ps1", `param(
    [string]$PowerShell,
    [string]$Barrier,
    [string]$Root
)
$group = Start-ProvisioningNativeGroup -Tasks @(
    @{ Role = 'left'; FilePath = $PowerShell; ArgumentList = @('-NoLogo', '-NoProfile', '-NonInteractive', '-File', $Barrier, 'left', $Root); TimeoutSeconds = 10 },
    @{ Role = 'right'; FilePath = $PowerShell; ArgumentList = @('-NoLogo', '-NoProfile', '-NonInteractive', '-File', $Barrier, 'right', $Root); TimeoutSeconds = 10 }
)
try { return @(Complete-ProvisioningNativeGroup -Group $group) }
finally { Stop-ProvisioningNativeGroup -Group $group }
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
$global:HerdrSandboxActiveProvisioningNativeGroup = $null
$global:HerdrSandboxActiveProvisioningNativeRoles = @()
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

$largeCommand = [Convert]::ToBase64String([Text.Encoding]::Unicode.GetBytes("[Console]::Out.Write(('x' * 1200000))"))
$largeSpec = New-Spec -Role 'bounded output' -Arguments @('-NoLogo', '-NoProfile', '-NonInteractive', '-EncodedCommand', $largeCommand)
$largeResult = [HerdrSandbox.ProvisioningProcess]::Run($largeSpec)
if (-not $largeResult.Succeeded -or -not $largeResult.OutputTruncated -or $largeResult.OutputBytes -lt 1200000 -or
    [Text.Encoding]::UTF8.GetByteCount([string]$largeResult.Output) -gt 1050000 -or
    [string]$largeResult.Output -notlike '*output truncated; original bytes:*') {
    throw "Bounded output result failed: $($largeResult | Format-List | Out-String)"
}

$barrierResults = @(& '%s' '%s' '%s' '%s')
if ($barrierResults.Count -ne 2 -or @($barrierResults | Where-Object { -not $_.Succeeded }).Count -ne 0 -or
    [string]$barrierResults[0].Output -notlike '*BARRIER:left*' -or [string]$barrierResults[1].Output -notlike '*BARRIER:right*' -or
    $null -ne $global:HerdrSandboxActiveProvisioningNativeGroup -or $global:HerdrSandboxActiveProvisioningNativeRoles.Count -ne 0) {
    throw "Barrier group failed: $($barrierResults | Format-List | Out-String)"
}

$failureRoot = Join-Path $argumentSpec.WorkingDirectory 'failure'
$timeoutRoot = Join-Path $argumentSpec.WorkingDirectory 'timeout'
$cancelRoot = Join-Path $argumentSpec.WorkingDirectory 'cancel'
foreach ($directory in @($failureRoot,$timeoutRoot,$cancelRoot)) { $null = New-Item -ItemType Directory -Path $directory }
$failureScript = '%s'
$siblingScript = '%s'
$grandchildScript = '%s'
$timeoutWitness = '%s'
$failureSpecs = [HerdrSandbox.ProvisioningProcessSpec[]]@(
    (New-Spec -Role 'failure' -Arguments @('-NoLogo', '-NoProfile', '-NonInteractive', '-WindowStyle', 'Hidden', '-File', $failureScript, $failureRoot)),
    (New-Spec -Role 'sibling' -Arguments @('-NoLogo', '-NoProfile', '-NonInteractive', '-WindowStyle', 'Hidden', '-File', $siblingScript, $failureRoot, $argumentSpec.FilePath, $grandchildScript))
)
$failureGroup = [HerdrSandbox.ProvisioningProcess]::StartGroup($failureSpecs)
try { $failureResults = @($failureGroup.Complete()) } finally { $failureGroup.Dispose() }
if ($failureResults.Count -ne 2 -or $failureResults[0].ExitCode -ne 23 -or $failureResults[0].Succeeded -or
    -not $failureResults[1].Succeeded -or $failureResults[1].Stopped -or $failureResults[1].TimedOut) {
    throw "Failure group result is invalid: $($failureResults | Format-List | Out-String)"
}
if (-not (Test-Path -LiteralPath (Join-Path $failureRoot 'grandchild.survived')) -or
    -not (Test-Path -LiteralPath (Join-Path $failureRoot 'sibling.completed'))) {
    throw 'Independent sibling did not finish after another task failed.'
}

$timeoutSpecs = [HerdrSandbox.ProvisioningProcessSpec[]]@(
    (New-Spec -Role 'timeout' -Arguments @('-NoLogo','-NoProfile','-NonInteractive','-WindowStyle','Hidden','-File',$siblingScript,$timeoutRoot,$argumentSpec.FilePath,$grandchildScript,'hold') -Timeout 10000),
    (New-Spec -Role 'independent witness' -Arguments @('-NoLogo','-NoProfile','-NonInteractive','-WindowStyle','Hidden','-File',$timeoutWitness,$timeoutRoot) -Timeout 30000)
)
$timeoutGroup = [HerdrSandbox.ProvisioningProcess]::StartGroup($timeoutSpecs)
try { $timeoutResults = @($timeoutGroup.Complete()) } finally { $timeoutGroup.Dispose() }
if (-not $timeoutResults[0].TimedOut -or $timeoutResults[0].Succeeded -or
    -not $timeoutResults[1].Succeeded -or $timeoutResults[1].Stopped -or
    -not (Test-Path -LiteralPath (Join-Path $timeoutRoot 'independent.completed')) -or
    (Test-Path -LiteralPath (Join-Path $timeoutRoot 'grandchild.survived'))) {
    throw 'Timeout stopped an independent task or failed to stop its own tree.'
}

$cancelGroup = Start-ProvisioningNativeGroup -Tasks @(
    @{Role='cancel tree'; FilePath=$argumentSpec.FilePath; ArgumentList=@('-NoLogo','-NoProfile','-NonInteractive','-WindowStyle','Hidden','-File',$siblingScript,$cancelRoot,$argumentSpec.FilePath,$grandchildScript,'hold'); TimeoutSeconds=30},
    @{Role='cancel peer'; FilePath=$argumentSpec.FilePath; ArgumentList=@('-NoLogo','-NoProfile','-NonInteractive','-WindowStyle','Hidden','-Command','Start-Sleep -Seconds 30'); TimeoutSeconds=30}
)
try {
    $deadline = [DateTime]::UtcNow.AddSeconds(10)
    while (-not (Test-Path -LiteralPath (Join-Path $cancelRoot 'sibling.ready'))) {
        if ([DateTime]::UtcNow -ge $deadline) { throw 'Cancellation fixture did not start.' }
        Start-Sleep -Milliseconds 20
    }
} finally { Stop-ProvisioningNativeGroup -Group $cancelGroup }
$cancelledPID = [int][IO.File]::ReadAllText((Join-Path $cancelRoot 'grandchild.pid'))
if ($null -ne (Get-Process -Id $cancelledPID -ErrorAction SilentlyContinue) -or
    (Test-Path -LiteralPath (Join-Path $cancelRoot 'grandchild.survived'))) {
    throw 'Explicit cancellation left an owned descendant alive.'
}
`, quote(processSource), quote(baseScript), quote(powerShell), quote(root), strings.Join(powerShellValues, ", "), quote(argumentFixture), expectedArguments,
		quote(rawOutputFixture), quote(rawOutputFixture),
		quote(groupCallerFixture), quote(powerShell), quote(barrierFixture), quote(root),
		quote(failureFixture), quote(siblingFixture), quote(grandchildFixture), quote(timeoutWitness))
	harnessPath := write("process-owner-regression.ps1", harness)
	command := hiddenCommand(powerShell, "-NoLogo", "-NoProfile", "-NonInteractive", "-WindowStyle", "Hidden", "-ExecutionPolicy", "Bypass", "-File", harnessPath)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("provisioning process owner regression: %v: %s", err, output)
	}
}

func TestProvisioningProcessCanTerminateOwnedDescendantsAfterRootExit(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows Job Object root-terminal regression")
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
	child := write("root-terminal-child.ps1", `param([string]$Root)
[IO.File]::WriteAllText((Join-Path $Root 'child.pid'), [string]$PID)
Start-Sleep -Seconds 30
[IO.File]::WriteAllText((Join-Path $Root 'child.survived'), 'survived')
`)
	rootProcess := write("root-terminal.ps1", `param([string]$Root, [string]$PowerShell, [string]$Child)
$childProcess = Start-Process -FilePath $PowerShell -ArgumentList @('-NoLogo', '-NoProfile', '-NonInteractive', '-WindowStyle', 'Hidden', '-File', $Child, $Root) -WindowStyle Hidden -PassThru
try {
    $deadline = [DateTime]::UtcNow.AddSeconds(5)
    while (-not (Test-Path -LiteralPath (Join-Path $Root 'child.pid') -PathType Leaf)) {
        if ([DateTime]::UtcNow -ge $deadline) { exit 71 }
        Start-Sleep -Milliseconds 20
    }
    [Console]::Out.Write('ROOT-TERMINAL')
} finally {
    $childProcess.Dispose()
}
`)
	harness := fmt.Sprintf(`$ErrorActionPreference = 'Stop'
Add-Type -Path '%s'
$tokens = $null
$errors = $null
$baseAST = [Management.Automation.Language.Parser]::ParseFile('%s', [ref]$tokens, [ref]$errors)
if ($errors.Count -ne 0) { throw $errors[0].Message }
foreach ($name in @('New-ProvisioningNativeSpec', 'Invoke-ProvisioningNativeResult')) {
    $definition = $baseAST.Find({ param($node) $node -is [Management.Automation.Language.FunctionDefinitionAst] -and $node.Name -ceq $name }, $true)
    if ($null -eq $definition) { throw "Missing Base process function: $name" }
    Invoke-Expression $definition.Extent.Text
}
function Write-ProvisioningProgress { param([string]$Message) }
function Write-ProvisioningTiming { param([string]$Role, [double]$Seconds) }
$result = Invoke-ProvisioningNativeResult -Role 'root-terminal cleanup' -FilePath '%s' -ArgumentList @('-NoLogo', '-NoProfile', '-NonInteractive', '-WindowStyle', 'Hidden', '-File', '%s', '%s', '%s', '%s') -WorkingDirectory '%s' -TimeoutSeconds 10 -TerminateDescendantsAfterRootExit
if (-not $result.Succeeded -or [string]$result.Output -cne 'ROOT-TERMINAL') {
    throw "Root-terminal result failed: $($result | Format-List | Out-String)"
}
$childID = [int][IO.File]::ReadAllText((Join-Path '%s' 'child.pid'))
if ($null -ne (Get-Process -Id $childID -ErrorAction SilentlyContinue)) {
    throw "Residual owned child remains active: $childID"
}
if (Test-Path -LiteralPath (Join-Path '%s' 'child.survived') -PathType Leaf) {
    throw 'Residual owned child reached its post-root action.'
}
`, quote(processSource), quote(baseScript), quote(powerShell), quote(rootProcess), quote(root), quote(powerShell), quote(child), quote(root), quote(root), quote(root))
	harnessPath := write("root-terminal-regression.ps1", harness)
	command := hiddenCommand(powerShell, "-NoLogo", "-NoProfile", "-NonInteractive", "-WindowStyle", "Hidden", "-ExecutionPolicy", "Bypass", "-File", harnessPath)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("root-terminal descendant cleanup regression: %v: %s", err, output)
	}
}
