package sandbox

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestHerdrConfigurationUsesSelectedSourceOnlyForGuestOverrides(t *testing.T) {
	root := t.TempDir()
	t.Setenv("APPDATA", root)
	selected := filepath.Join(root, "selected.toml")
	t.Setenv("HERDR_CONFIG_PATH", selected)
	writeTestFile(t, selected, "[terminal]\ndefault_shell = 'nu'\n[theme]\nname = 'host-theme'\n[agent]\nargs = ['host-only']\n")
	terminal := testStableWindowsTerminalConfiguration()
	packages, err := resolveWingetPackagePlan(wingetPackageConfiguration{Add: []string{}}, terminal)
	if err != nil {
		t.Fatal(err)
	}
	sources, err := defaultHostConfigurationSources(terminal, packages, codingAgentSyncConfiguration{}, credentialSyncConfiguration{}, false)
	if err != nil || sources.HerdrConfig != selected {
		t.Fatalf("selected source = %q, error = %v", sources.HerdrConfig, err)
	}
	guest := []byte("[terminal]\nnew_cwd = 'C:/Guest'\n[theme]\nname = 'guest-theme'\n[agent]\nargs = ['guest-only']\n")
	patched, err := buildGuestHerdrConfig(sources.HerdrConfig, guest, guestWorktreeDirectory)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`default_shell = "nu.exe"`, `directory = "C:/Worktrees"`, "new_cwd = 'C:/Guest'", "name = 'guest-theme'", "args = ['guest-only']"} {
		if !bytes.Contains(patched, []byte(want)) {
			t.Fatalf("guest override lost %q: %s", want, patched)
		}
	}
	if bytes.Contains(patched, []byte("host-theme")) || bytes.Contains(patched, []byte("host-only")) {
		t.Fatal("Sandbox copied host settings owned by Herdr or the target machine")
	}
	for _, snapshot := range []struct {
		input  string
		absent bool
	}{{"missing", true}, {"present:", false}, {"present:" + base64.StdEncoding.EncodeToString(guest), false}} {
		got, err := decodeGuestHerdrConfiguration([]byte(snapshot.input))
		if err != nil || (got == nil) != snapshot.absent {
			t.Fatalf("snapshot presence: %v, %v", got == nil, err)
		}
	}
}

func TestHerdrGuestConfigurationSnapshotAndPublicationInWindowsPowerShell51(t *testing.T) {
	requireExternalBoundaryTest(t, "guest Herdr configuration publication")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	root := t.TempDir()
	t.Setenv("APPDATA", root)
	config := filepath.Join(root, "herdr", "config.toml")
	readScript := filepath.Join(root, "read.ps1")
	writeTestFile(t, readScript, guestHerdrConfigurationReadScript)
	readSnapshot := func() []byte {
		t.Helper()
		command := hiddenCommandContext(ctx, mustWindowsPowerShellPath(t), "-NoLogo", "-NoProfile", "-NonInteractive", "-WindowStyle", "Hidden", "-ExecutionPolicy", "Bypass", "-File", readScript)
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("read snapshot: %v: %s", err, output)
		}
		data, err := decodeGuestHerdrConfiguration(output)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	if readSnapshot() != nil {
		t.Fatal("missing configuration was not represented as absent")
	}
	original := "[agent]\nargs = ['guest-local']\n"
	writeTestFile(t, config, original)
	if string(readSnapshot()) != original {
		t.Fatal("guest configuration did not round trip")
	}
	patched, err := patchGuestHerdrConfig([]byte(original), guestWorktreeDirectory, "nu.exe")
	if err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(root, "patched.toml")
	writeTestFile(t, source, string(patched))
	start := bytes.Index(configurationSyncScript, []byte("$script:CopiedConfigurationFiles = 0"))
	end := bytes.Index(configurationSyncScript, []byte("function Copy-VerifiedConfigurationTree"))
	if start < 0 || end <= start {
		t.Fatal("configuration writer helper not found")
	}
	t.Setenv("TEST_HERDR_SOURCE", source)
	t.Setenv("TEST_HERDR_DESTINATION", config)
	t.Setenv("TEST_HERDR_EXPECTED", fmt.Sprintf("%x", sha256.Sum256([]byte(original))))
	applyScript := filepath.Join(root, "apply.ps1")
	writeTestFile(t, applyScript, "$ErrorActionPreference = 'Stop'\n"+string(configurationSyncScript[start:end])+`
Set-AtomicConfigurationFile -Source $env:TEST_HERDR_SOURCE -Destination $env:TEST_HERDR_DESTINATION -ExpectedSHA256 $env:TEST_HERDR_EXPECTED
$rejected = $false
try {
    Set-AtomicConfigurationFile -Source $env:TEST_HERDR_SOURCE -Destination $env:TEST_HERDR_DESTINATION -ExpectedSHA256 $env:TEST_HERDR_EXPECTED
} catch {
    if ($_.Exception.Message -notlike '*changed during sync*') { throw }
    $rejected = $true
}
if (-not $rejected) { throw 'A stale guest snapshot overwrote a newer configuration.' }
`)
	command := hiddenCommandContext(ctx, mustWindowsPowerShellPath(t), "-NoLogo", "-NoProfile", "-NonInteractive", "-WindowStyle", "Hidden", "-ExecutionPolicy", "Bypass", "-File", applyScript)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("publish guest overrides: %v: %s", err, output)
	}
	if !bytes.Equal(readSnapshot(), patched) {
		t.Fatal("published configuration changed after stale-snapshot rejection")
	}
}

func TestGuestHerdrOverridesSurviveNativeProvisionImport(t *testing.T) {
	executable := os.Getenv("HERDR_SANDBOX_TEST_CONFIG_IMPORT_HERDR")
	if executable == "" {
		t.Skip("set HERDR_SANDBOX_TEST_CONFIG_IMPORT_HERDR to the matching packaged runtime")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	root := t.TempDir()
	path := filepath.Join(root, "config.toml")
	guest, err := patchGuestHerdrConfig([]byte("[agent]\nargs = ['guest-local']\n"), guestWorktreeDirectory, "nu.exe")
	if err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, path, string(guest))
	t.Setenv("HERDR_CONFIG_PATH", path)
	// Native import receives the same encoding as Herdr's export owner. It must
	// preserve our overlay even when incoming machine-local values are present.
	incoming := "[terminal]\ndefault_shell = 'host-shell'\n[worktrees]\ndirectory = 'D:/Host'\n[agent]\nargs = ['host-local']\n[theme]\nname = 'dracula'\n"
	command := hiddenCommandContext(ctx, executable, "config", "provision-import")
	command.Env = attachEnvironment(childProcessEnvironment(os.Environ()))
	for index := 0; index < len(command.Env); index++ {
		name, _, _ := strings.Cut(command.Env[index], "=")
		if strings.EqualFold(name, "HERDR_REMOTE_SIDECAR_V1") {
			command.Env = append(command.Env[:index], command.Env[index+1:]...)
			break
		}
	}
	command.Stdin = strings.NewReader(base64.StdEncoding.EncodeToString([]byte(incoming)))
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("native Herdr import: %v: %s", err, output)
	}
	saved, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"nu.exe", "C:/Worktrees", "guest-local", "dracula"} {
		if !bytes.Contains(saved, []byte(want)) {
			t.Fatalf("native imported config lost %q: %s", want, saved)
		}
	}
	if bytes.Contains(saved, []byte("host-local")) || bytes.Contains(saved, []byte("D:/Host")) {
		t.Fatal("native import overwrote Sandbox-owned or guest-local settings")
	}
}
