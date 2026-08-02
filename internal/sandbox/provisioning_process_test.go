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
		quote(powerShell), quote(barrierFixture), quote(root), quote(powerShell), quote(barrierFixture), quote(root), quote(failureFixture), quote(root),
		quote(siblingFixture), quote(root), quote(powerShell), quote(grandchildFixture), quote(root))
	harnessPath := write("process-owner-regression.ps1", harness)
	command := hiddenCommand(powerShell, "-NoLogo", "-NoProfile", "-NonInteractive", "-WindowStyle", "Hidden", "-ExecutionPolicy", "Bypass", "-File", harnessPath)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("provisioning process owner regression: %v: %s", err, output)
	}
}
