package main

import (
	"context"
	"encoding/json"
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
	configurationData, err := os.ReadFile(filepath.Join(fixture.AppData, "herdr-sandbox", "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	var configuration struct {
		Mounts map[string]struct {
			Path     string `json:"path"`
			ReadOnly bool   `json:"readOnly"`
		} `json:"mounts"`
	}
	if err := json.Unmarshal(configurationData, &configuration); err != nil {
		t.Fatal(err)
	}
	if len(configuration.Mounts) != 2 || configuration.Mounts["reference"].Path != fixture.ReadOnlyMount ||
		!configuration.Mounts["reference"].ReadOnly || configuration.Mounts["worktrees"].Path != fixture.WritableMount ||
		configuration.Mounts["worktrees"].ReadOnly {
		t.Fatalf("native folder mounts = %#v", configuration.Mounts)
	}
	for _, path := range []string{
		filepath.Join(fixture.ReadOnlyMount, "host-reference.txt"),
		filepath.Join(fixture.WritableMount, "host-worktrees.txt"),
	} {
		if info, err := os.Stat(path); err != nil || !info.Mode().IsRegular() || info.Size() == 0 {
			t.Fatalf("native folder-mount fixture %s is invalid: %v, %v", path, info, err)
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

func TestVerifyNativeAllStacksMountsRequiresBlockedAndPersistedWrites(t *testing.T) {
	fixture := nativeAllStacksFixture{ReadOnlyMount: filepath.Join(t.TempDir(), "reference"), WritableMount: filepath.Join(t.TempDir(), "worktrees")}
	if err := os.MkdirAll(fixture.ReadOnlyMount, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(fixture.WritableMount, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fixture.WritableMount, "guest-write.txt"), []byte("guest-write-ok\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := verifyNativeAllStacksMounts(fixture); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fixture.ReadOnlyMount, "guest-write-blocked.txt"), []byte("unexpected"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := verifyNativeAllStacksMounts(fixture); err == nil {
		t.Fatal("accepted a write through the read-only folder mount")
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
