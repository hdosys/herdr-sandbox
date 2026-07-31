package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestPrepareNativeAllStacksFixtureIsCredentialFreeAndComplete(t *testing.T) {
	fixture, err := prepareNativeAllStacksFixture(filepath.Join(t.TempDir(), "native"))
	if err != nil {
		t.Fatal(err)
	}
	profile, err := os.ReadFile(filepath.Join(fixture.Project, ".herdr-sandbox", "provision.ps1"))
	if err != nil {
		t.Fatal(err)
	}
	for _, call := range []string{"Install-DotNetStack", "Install-GoStack", "Install-NodeStack", "Install-PythonStack", "Install-RustMSVCStack", "Install-ZigStack"} {
		if !strings.Contains(string(profile), call) {
			t.Fatalf("native fixture profile is missing %s", call)
		}
	}
	for _, source := range []string{"go.mod", "main.go", "main_test.go", "smoke.csproj", "Program.cs", "smoke.js", "smoke.py", "smoke.rs", "smoke.zig"} {
		if info, err := os.Stat(filepath.Join(fixture.Project, source)); err != nil || !info.Mode().IsRegular() || info.Size() == 0 {
			t.Fatalf("native fixture source %s is invalid: %v, %v", source, info, err)
		}
	}
	for _, absent := range []string{
		filepath.Join(fixture.AppData, "GitHub CLI", "hosts.yml"),
		filepath.Join(fixture.AppData, "herdr", "config.toml"),
	} {
		if _, err := os.Stat(absent); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("credential-free fixture contains %s: %v", absent, err)
		}
	}
	terminal, err := os.ReadFile(filepath.Join(fixture.LocalAppData, "Packages", "Microsoft.WindowsTerminal_8wekyb3d8bbwe", "LocalState", "settings.json"))
	if err != nil || !strings.Contains(string(terminal), `"theme": "light"`) || !strings.Contains(string(terminal), `"colorScheme": "Herdr Native Light"`) {
		t.Fatalf("native Terminal fixture = %q, %v", terminal, err)
	}
}

func TestNativeAllStacksSmokeScriptParsesWithWindowsPowerShell(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows PowerShell 5.1 parser boundary")
	}
	parser := `$tokens = $null
$errors = $null
[System.Management.Automation.Language.Parser]::ParseInput($env:HERDR_SANDBOX_NATIVE_SCRIPT, [ref]$tokens, [ref]$errors) | Out-Null
if ($errors.Count -gt 0) {
    $errors | ForEach-Object { [Console]::Error.WriteLine($_.Message) }
    exit 1
}`
	command := hiddenCommandContext(context.Background(), "powershell.exe", "-NoLogo", "-NoProfile", "-NonInteractive", "-EncodedCommand", encodeNativePowerShell(parser))
	command.Env = append(os.Environ(), "HERDR_SANDBOX_NATIVE_SCRIPT="+nativeAllStacksSmokeScript)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("parse native all-stack smoke script: %v: %s", err, output)
	}
}
