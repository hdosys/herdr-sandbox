package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"unicode/utf16"
)

func encodeNativePowerShell(script string) string {
	codeUnits := utf16.Encode([]rune(script))
	bytes := make([]byte, len(codeUnits)*2)
	for index, value := range codeUnits {
		bytes[index*2] = byte(value)
		bytes[index*2+1] = byte(value >> 8)
	}
	return base64.StdEncoding.EncodeToString(bytes)
}

func TestWriteNativeAcceptanceEvidenceIsCommitKeyedAndComplete(t *testing.T) {
	const commit = "0123456789abcdef0123456789abcdef01234567"
	var output bytes.Buffer
	if err := writeNativeAcceptanceEvidence(&output, commit); err != nil {
		t.Fatal(err)
	}
	var evidence nativeAcceptanceEvidence
	if err := json.NewDecoder(&output).Decode(&evidence); err != nil {
		t.Fatal(err)
	}
	if evidence.Kind != "native-acceptance" || evidence.Commit != commit ||
		evidence.Platform != runtime.GOOS+"/"+runtime.GOARCH ||
		evidence.Command != "go run ./cmd/task native-all-stacks" ||
		evidence.Result != "passed" || !slices.Equal(evidence.Boundaries, nativeAcceptanceBoundaries) {
		t.Fatalf("native evidence = %#v", evidence)
	}
}

func TestPrepareNativeAllStacksFixtureIsCredentialFreeAndUsesNarrowMounts(t *testing.T) {
	fixture, err := prepareNativeAllStacksFixture(filepath.Join(t.TempDir(), "native"))
	if err != nil {
		t.Fatal(err)
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
		Workspaces map[string]string `json:"workspaces"`
	}
	if err := json.Unmarshal(configurationData, &configuration); err != nil {
		t.Fatal(err)
	}
	if len(configuration.Mounts) != 2 || configuration.Mounts["reference"].Path != fixture.ReadOnlyMount ||
		!configuration.Mounts["reference"].ReadOnly || configuration.Mounts["worktrees"].Path != fixture.WritableMount ||
		configuration.Mounts["worktrees"].ReadOnly || configuration.Workspaces["handy"] != fixture.HandyProject {
		t.Fatalf("native fixture configuration = %#v", configuration)
	}
	smokeScript, err := os.ReadFile(filepath.Join(fixture.ReadOnlyMount, "native-all-stacks-smoke.ps1"))
	if err != nil || string(smokeScript) != nativeAllStacksSmokeScript {
		t.Fatalf("native smoke fixture script does not match its repository owner: %v", err)
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

func TestNativeAllStacksSmokeCommandUsesReadOnlyFixtureScript(t *testing.T) {
	sshConfig := `C:\Runs\native\.ssh\config`
	command := nativeAllStacksSmokeCommand(context.Background(), "ssh.exe", sshConfig)
	want := strings.Join([]string{
		"-T", "-F", sshConfig, "sandbox",
		"powershell.exe", "-NoLogo", "-NoProfile", "-NonInteractive", "-WindowStyle", "Hidden",
		"-ExecutionPolicy", "Bypass", "-File", nativeAllStacksSmokeGuestPath,
	}, "\x00")
	if got := strings.Join(command.Args[1:], "\x00"); got != want {
		t.Fatalf("native smoke SSH arguments = %q, want %q", got, want)
	}
	if command.Stdin != nil || strings.Contains(want, "-EncodedCommand") || strings.Contains(want, nativeAllStacksSmokeScript) {
		t.Fatal("native smoke payload remains on the SSH command line or standard input")
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
	requireExternalBoundaryTest(t, "Windows PowerShell native smoke parsing")
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
