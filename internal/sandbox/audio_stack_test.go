package sandbox

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestAudioGridderConfigurationsPreserveStateAndRejectLiveREAPERInWindowsPowerShell51(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows PowerShell 5.1 AudioGridder configuration regression")
	}
	stackPath := defaultProvisioningPath(t, stackProvisioningName)
	appData := t.TempDir()
	quote := func(value string) string { return strings.ReplaceAll(value, "'", "''") }
	script := fmt.Sprintf(`$ErrorActionPreference = 'Stop'
$tokens = $null
$errors = $null
$ast = [Management.Automation.Language.Parser]::ParseFile('%s', [ref]$tokens, [ref]$errors)
if ($errors.Count -ne 0) { throw $errors[0].Message }
foreach ($name in @('ConvertTo-StackAudioGridderJSONValue', 'Set-StackAudioGridderConfiguration', 'Set-StackAudioGridderServerConfiguration', 'Set-StackAudioGridderClientConfiguration', 'Set-StackREAPERConfiguration')) {
    $definition = $ast.Find({ param($node) $node -is [Management.Automation.Language.FunctionDefinitionAst] -and $node.Name -ceq $name }, $true)
    if ($null -eq $definition) { throw "Missing AudioGridder configuration helper: $name" }
    Invoke-Expression $definition.Extent.Text
}
$env:APPDATA = '%s'
$configurationRoot = Join-Path $env:APPDATA 'AudioGridder'
$configurationPath = Join-Path $configurationRoot 'audiogridderplugin.cfg'
$null = New-Item -ItemType Directory -Path $configurationRoot -Force
$initial = [ordered]@{ ZoomFactor = 1.25; Servers = @('old-host:0'); LastServer = 'old-host:0'; DisableTray = $false }
[IO.File]::WriteAllText($configurationPath, ($initial | ConvertTo-Json -Compress), (New-Object Text.UTF8Encoding($false)))
$script:ReaperRunning = $true
function Get-Process { param([string[]]$Name, [object]$ErrorAction) if ($script:ReaperRunning) { [pscustomobject]@{ Id = 7 } } }
$reaperRoot = Join-Path $env:APPDATA 'REAPER'
$reaperPath = Join-Path $reaperRoot 'REAPER.ini'
$null = New-Item -ItemType Directory -Path $reaperRoot -Force
$reaperInitial = (@('[reaper]', 'lastproject=keep.rpp', '[verchk]', 'lastt=123') -join [Environment]::NewLine) + [Environment]::NewLine
[IO.File]::WriteAllText($reaperPath, $reaperInitial, (New-Object Text.UTF8Encoding($false)))
$reaperBefore = [IO.File]::ReadAllBytes($reaperPath)
$reaperRejected = $false
try { Set-StackREAPERConfiguration } catch { $reaperRejected = $_.Exception.Message.Contains('Close REAPER') }
if (-not $reaperRejected -or
    [Convert]::ToBase64String([IO.File]::ReadAllBytes($reaperPath)) -cne [Convert]::ToBase64String($reaperBefore)) {
    throw 'REAPER configuration changed while REAPER was running.'
}
$before = [IO.File]::ReadAllBytes($configurationPath)
$rejected = $false
try { Set-StackAudioGridderClientConfiguration } catch { $rejected = $_.Exception.Message.Contains('Close REAPER') }
if (-not $rejected -or ([Convert]::ToBase64String([IO.File]::ReadAllBytes($configurationPath)) -cne [Convert]::ToBase64String($before))) {
    throw 'AudioGridder endpoint changed while REAPER was running.'
}
$script:ReaperRunning = $false
Set-StackREAPERConfiguration
$reaperText = [IO.File]::ReadAllText($reaperPath)
if ($reaperText -notmatch '(?m)^verchk=0\r?$' -or $reaperText -notmatch '(?m)^lastproject=keep\.rpp\r?$' -or
    $reaperText -notmatch '(?m)^\[verchk\]\r?$' -or $reaperText -notmatch '(?m)^lastt=123\r?$') {
    throw 'REAPER update-check write did not preserve unrelated settings.'
}
$reaperWritten = [IO.File]::ReadAllBytes($reaperPath)
Set-StackREAPERConfiguration
if ([Convert]::ToBase64String([IO.File]::ReadAllBytes($reaperPath)) -cne [Convert]::ToBase64String($reaperWritten)) {
    throw 'REAPER update-check write is not idempotent.'
}
Set-StackAudioGridderClientConfiguration
$written = [IO.File]::ReadAllBytes($configurationPath)
$configuration = [IO.File]::ReadAllText($configurationPath) | ConvertFrom-Json
$servers = @($configuration.Servers | ForEach-Object { [string]$_ })
if ($servers.Count -ne 1 -or $servers[0] -cne '127.0.0.1:0' -or
    [string]$configuration.LastServer -cne '127.0.0.1:0:::0:0:00000000-0000-0000-0000-000000000000' -or
    [double]$configuration.ZoomFactor -ne 1.25 -or [bool]$configuration.DisableTray) {
    throw 'AudioGridder endpoint write did not preserve unrelated settings.'
}
Set-StackAudioGridderClientConfiguration
if ([Convert]::ToBase64String([IO.File]::ReadAllBytes($configurationPath)) -cne [Convert]::ToBase64String($written)) {
    throw 'AudioGridder endpoint write is not idempotent.'
}
$serverPath = Join-Path $configurationRoot 'audiogridderserver.cfg'
Set-StackAudioGridderServerConfiguration
$server = [IO.File]::ReadAllText($serverPath) | ConvertFrom-Json
$vst2Folders = @($server.VST2Folders | ForEach-Object { [string]$_ })
$vst3Folders = @($server.VST3Folders | ForEach-Object { [string]$_ })
if ([int]$server.ID -ne 0 -or [string]$server.NAME -cne 'Herdr Sandbox' -or
    -not [bool]$server.VST -or -not [bool]$server.VST2 -or -not [bool]$server.VSTNoStandardFolders -or
    -not [bool]$server.ScanForPlugins -or -not [bool]$server.Logger -or [bool]$server.CrashReporting -or
    [int]$server.SandboxMode -ne 1 -or [bool]$server.ScreenLocalMode -or
    $vst2Folders.Count -ne 1 -or $vst2Folders[0] -cne 'C:\Program Files\VstPlugins' -or
    $vst3Folders.Count -ne 1 -or $vst3Folders[0] -cne 'C:\Program Files\Common Files\VST3') {
    throw 'AudioGridder server-0 configuration is invalid.'
}
[IO.File]::WriteAllText($configurationPath, '{', (New-Object Text.UTF8Encoding($false)))
$accepted = $false
try { Set-StackAudioGridderClientConfiguration; $accepted = $true } catch { }
if ($accepted) { throw 'Malformed AudioGridder configuration was replaced.' }
`, quote(stackPath), quote(appData))
	scriptPath := filepath.Join(t.TempDir(), "audio-endpoint-regression.ps1")
	if err := os.WriteFile(scriptPath, []byte(script), 0o600); err != nil {
		t.Fatal(err)
	}
	command := hiddenCommand(mustWindowsPowerShellPath(t), "-NoLogo", "-NoProfile", "-NonInteractive", "-WindowStyle", "Hidden", "-ExecutionPolicy", "Bypass", "-File", scriptPath)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("AudioGridder endpoint regression: %v: %s", err, output)
	}
}
