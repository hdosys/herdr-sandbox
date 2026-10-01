package sandbox

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestApifyTokenStorageInWindowsPowerShell51(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows PowerShell 5.1 token storage")
	}
	directory := t.TempDir()
	asset := filepath.Join(directory, apifyAccessName)
	if err := os.WriteFile(asset, apifyAccessScript, 0o600); err != nil {
		t.Fatal(err)
	}
	quote := func(value string) string { return strings.ReplaceAll(value, "'", "''") }
	script := fmt.Sprintf(`$ErrorActionPreference = 'Stop'
. '%s'
$tokenPath = Join-Path '%s' 'config\.usage-status.env'
New-Item -ItemType Directory -Path (Join-Path (Split-Path -Parent $tokenPath) '.git\info') -Force | Out-Null
Set-ApifyToken -Value '  apify_api_fixture  ' -Path $tokenPath
if ([IO.File]::ReadAllText($tokenPath) -cne ('APIFY_TOKEN="apify_api_fixture"' + [char]10)) { throw 'Token publication failed.' }
Set-ApifyToken -Value '' -Path $tokenPath
foreach ($value in @('APIFY_TOKEN=x', ('a' * 513), ('bad' + [char]10 + 'token'))) {
    $rejected = $false
    try { Set-ApifyToken -Value $value -Path $tokenPath } catch { $rejected = $true }
    if (-not $rejected) { throw 'Invalid token accepted.' }
}
if ([IO.File]::ReadAllText($tokenPath) -cne ('APIFY_TOKEN="apify_api_fixture"' + [char]10)) { throw 'Empty or invalid input changed token.' }
Set-ApifyToken -Value 'apify_api_replaced' -Path $tokenPath
if ([IO.File]::ReadAllText($tokenPath) -cne ('APIFY_TOKEN="apify_api_replaced"' + [char]10)) { throw 'Replacement failed.' }
$exclude = [IO.File]::ReadAllText((Join-Path (Split-Path -Parent $tokenPath) '.git\info\exclude'))
if (@($exclude -split '\r?\n' | Where-Object { $_ -ceq '/.usage-status.env' }).Count -ne 1) { throw 'Private Git exclusion missing or duplicated.' }
if (@(Get-ChildItem -LiteralPath (Split-Path -Parent $tokenPath) -Filter '*.tmp' -Force).Count -ne 0) { throw 'Temporary secret retained.' }
[Console]::WriteLine('Apify token save, replace, exclusion and invalid-input preservation passed.')
`, quote(asset), quote(directory))
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	command := hiddenCommandContext(ctx, mustWindowsPowerShellPath(t), "-NoLogo", "-NoProfile", "-NonInteractive", "-WindowStyle", "Hidden", "-ExecutionPolicy", "Bypass", "-EncodedCommand", encodePowerShell(script))
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("Apify token storage: %v: %s", err, output)
	}
}
