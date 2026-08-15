package sandbox

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

type testExitCodeError int

func (err testExitCodeError) Error() string { return "test command exit" }
func (err testExitCodeError) ExitCode() int { return int(err) }

func TestPrepareTailscaleBootstrapRequiresOnlyTheFirstAuthKey(t *testing.T) {
	disabled, err := prepareTailscaleBootstrap(t.TempDir(), false, []byte("tskey-auth-unused"), true)
	if err != nil || disabled.Enabled || disabled.Existing != nil || len(disabled.AuthKey) != 0 {
		t.Fatalf("disabled bootstrap = %#v, error = %v", disabled, err)
	}

	dataDirectory := t.TempDir()
	if _, err := prepareTailscaleBootstrap(dataDirectory, true, nil, false); err == nil || !strings.Contains(err.Error(), tailscaleAuthKeyEnvironment) {
		t.Fatalf("missing first auth-key error = %v", err)
	}
	key := []byte("tskey-auth-test-only")
	bootstrap, err := prepareTailscaleBootstrap(dataDirectory, true, key, true)
	if err != nil {
		t.Fatalf("prepare first enrollment: %v", err)
	}
	defer bootstrap.clear()
	key[0] = 'X'
	if !bootstrap.Enabled || bootstrap.Existing != nil || string(bootstrap.AuthKey) != "tskey-auth-test-only" {
		t.Fatalf("first enrollment bootstrap = %#v", bootstrap)
	}
	if runtime.GOOS == "windows" {
		storedDirectory := t.TempDir()
		if err := storeTailscaleIdentity(storedDirectory, testTailscaleIdentity(t, "100.64.0.10")); err != nil {
			t.Fatal(err)
		}
		restored, err := prepareTailscaleBootstrap(storedDirectory, true, nil, false)
		if err != nil {
			t.Fatalf("prepare existing identity without another auth key: %v", err)
		}
		defer restored.clear()
		if restored.Existing == nil || len(restored.AuthKey) != 0 {
			t.Fatalf("existing identity bootstrap = %#v", restored)
		}
	}
}

func TestChildProcessEnvironmentAlwaysRemovesTailscaleAuthKey(t *testing.T) {
	parent := []string{
		"Path=C:\\Windows",
		tailscaleAuthKeyEnvironment + "=tskey-auth-secret-one",
		strings.ToLower(tailscaleAuthKeyEnvironment) + "=tskey-auth-secret-two",
		"UNCHANGED=value",
	}
	environment := childProcessEnvironment(parent)
	joined := strings.Join(environment, "\x00")
	if strings.Contains(strings.ToUpper(joined), tailscaleAuthKeyEnvironment) || strings.Contains(joined, "secret") {
		t.Fatalf("child environment retained the Tailscale auth key: %q", environment)
	}
	if !strings.Contains(joined, "UNCHANGED=value") || !strings.Contains(joined, "Path=C:\\Windows") {
		t.Fatalf("child environment lost unrelated values: %q", environment)
	}
}

func TestTailscaleApplyRequestsFailBeforeSSHWhenInvalid(t *testing.T) {
	tests := map[string]tailscaleApplyRequest{
		"schema":           {SchemaVersion: tailscaleApplySchemaVersion + 1, Mode: tailscaleApplyModeEnroll, AuthKey: []byte("key")},
		"mode":             {SchemaVersion: tailscaleApplySchemaVersion, Mode: "other", AuthKey: []byte("key")},
		"missing key":      {SchemaVersion: tailscaleApplySchemaVersion, Mode: tailscaleApplyModeEnroll},
		"oversized key":    {SchemaVersion: tailscaleApplySchemaVersion, Mode: tailscaleApplyModeEnroll, AuthKey: bytes.Repeat([]byte{'x'}, maximumTailscaleAuthKeyBytes+1)},
		"restore with key": {SchemaVersion: tailscaleApplySchemaVersion, Mode: tailscaleApplyModeRestore, AuthKey: []byte("key"), State: []byte("state"), WindowsUserSID: "S-1-5-21-1"},
		"oversized state":  {SchemaVersion: tailscaleApplySchemaVersion, Mode: tailscaleApplyModeRestore, State: bytes.Repeat([]byte{'x'}, maximumTailscaleStateBytes+1), WindowsUserSID: "S-1-5-21-1"},
		"invalid SID":      {SchemaVersion: tailscaleApplySchemaVersion, Mode: tailscaleApplyModeRestore, State: []byte("state"), WindowsUserSID: "not-a-sid"},
	}
	for name, request := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := applyAndCaptureTailscale(context.Background(), Connection{}, request); err == nil || strings.Contains(err.Error(), "OpenSSH") {
				t.Fatalf("invalid request crossed the SSH boundary: %v", err)
			}
		})
	}
}

func TestCommandExitedWithFindsWrappedRemoteExit(t *testing.T) {
	err := fmt.Errorf("SSH command failed: %w", testExitCodeError(tailscaleIdentityNotEstablishedExitCode))
	if !commandExitedWith(err, tailscaleIdentityNotEstablishedExitCode) || commandExitedWith(err, 1) {
		t.Fatalf("command exit classification failed: %v", err)
	}
}

func TestDecodeTailscaleIdentityResultIsStrict(t *testing.T) {
	identity := testTailscaleIdentity(t, "100.64.0.10")
	encoded, err := json.Marshal(identity)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeTailscaleIdentityResult(encoded)
	if err != nil || decoded.NodeID != identity.NodeID || !bytes.Equal(decoded.State, identity.State) {
		t.Fatalf("decoded identity = %#v, error = %v", decoded, err)
	}
	invalid := [][]byte{
		bytes.Replace(encoded, []byte(`"state":`), []byte(`"extra":true,"state":`), 1),
		append(append([]byte(nil), encoded...), []byte(` {}`)...),
		bytes.Replace(encoded, []byte(`"nodeID":`), []byte(`"nodeId":`), 1),
	}
	for _, candidate := range invalid {
		if _, err := decodeTailscaleIdentityResult(candidate); err == nil {
			t.Fatalf("invalid identity result decoded: %s", candidate)
		}
	}
}

func TestDecodeTailscaleStateCaptureResultIsStrict(t *testing.T) {
	identity := testTailscaleIdentity(t, "100.64.0.10")
	encoded, err := json.Marshal(tailscaleStateCapture{WindowsUserSID: identity.WindowsUserSID, State: identity.State})
	if err != nil {
		t.Fatal(err)
	}
	captured, err := decodeTailscaleStateCaptureResult(encoded)
	if err != nil || captured.WindowsUserSID != identity.WindowsUserSID || !bytes.Equal(captured.State, identity.State) {
		t.Fatalf("decoded state capture = %#v, error = %v", captured, err)
	}
	clear(captured.State)
	if _, err := decodeTailscaleStateCaptureResult(bytes.Replace(encoded, []byte(`"state":`), []byte(`"extra":true,"state":`), 1)); err == nil {
		t.Fatal("state capture with an extra field unexpectedly decoded")
	}
}

func TestVerifyStableTailscaleIdentityRejectsDrift(t *testing.T) {
	expected := testTailscaleIdentity(t, "100.64.0.10")
	actual := expected
	actual.State = append([]byte(nil), expected.State...)
	actual.TailscaleVersion = "1.99.0"
	if err := verifyStableTailscaleIdentity(expected, actual); err != nil {
		t.Fatalf("stable identity after client version update: %v", err)
	}
	tests := map[string]func(*tailscaleIdentity){
		"Windows SID": func(value *tailscaleIdentity) { value.WindowsUserSID = "S-1-5-21-9-9-9-504" },
		"node ID":     func(value *tailscaleIdentity) { value.NodeID = "node-other" },
		"node key":    func(value *tailscaleIdentity) { value.NodeKey = "nodekey:rotated-public-node-key" },
		"IPv4":        func(value *tailscaleIdentity) { value.IPv4 = "100.64.0.11" },
		"DNS":         func(value *tailscaleIdentity) { value.DNSName = "other.example.ts.net." },
		"hostname":    func(value *tailscaleIdentity) { value.HostName = "other" },
		"tags":        func(value *tailscaleIdentity) { value.Tags = []string{"tag:other"} },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			changed := expected
			mutate(&changed)
			if err := verifyStableTailscaleIdentity(expected, changed); err == nil {
				t.Fatalf("%s drift unexpectedly matched", name)
			}
		})
	}
}

func TestTailscaleLaunchersUseBoundedSecretSafePowerShell51(t *testing.T) {
	digest := fmt.Sprintf("%x", sha256.Sum256([]byte("test request")))
	apply := buildTailscaleApplyLauncher(digest, 12345)
	capture := buildTailscaleCaptureLauncher()
	downCapture := buildTailscaleDownCaptureLauncher()
	for _, required := range []string{
		"[Console]::OpenStandardInput()",
		"$expectedLength = [long]12345",
		digest,
		"EncryptState",
		"HardwareAttestation",
		"--auth-key=file:",
		"status --json --peers=false",
		"exit 42",
		"Join-Path $env:ProgramData 'Tailscale\\server-state.conf'",
		"Stop-TailscaleService",
		"Start-TailscaleService",
		"Remove-Item -LiteralPath $authPath -Force -ErrorAction Stop",
		"auth-key staging cleanup did not remove the credential file",
		"plaintext state staging cleanup did not remove the credential file",
	} {
		if !strings.Contains(apply, required) {
			t.Fatalf("Tailscale apply launcher is missing %q", required)
		}
	}
	if strings.Contains(apply, tailscaleAuthKeyEnvironment) || strings.Contains(apply, "tskey-") ||
		strings.Contains(capture, tailscaleAuthKeyEnvironment) || strings.Contains(downCapture, tailscaleAuthKeyEnvironment) {
		t.Fatal("Tailscale launcher embeds an auth-key value or environment contract")
	}
	downBodyIndex := strings.LastIndex(downCapture, "$ErrorActionPreference = 'Stop'")
	if downBodyIndex < 0 {
		t.Fatal("down Tailscale capture body is missing")
	}
	downBody := downCapture[downBodyIndex:]
	if !strings.Contains(downBody, "Capture-TailscaleStateBytes -ExpectedSID $currentSID") || strings.Contains(downBody, "Wait-TailscaleIdentity") {
		t.Fatalf("down Tailscale capture still waits for control-plane readiness: %s", downBody)
	}
	if strings.Contains(apply, "Remove-Item -LiteralPath $authPath -Force -ErrorAction SilentlyContinue") {
		t.Fatal("Tailscale launcher silently ignores auth-key staging cleanup")
	}
	declaration := strings.Index(apply, "function Assert-ExactProperties")
	use := strings.Index(apply, "Assert-ExactProperties $request")
	if declaration < 0 || use < 0 || declaration > use {
		t.Fatal("Tailscale helper functions are declared after use")
	}
	for name, script := range map[string]string{"apply": apply, "capture": capture, "down capture": downCapture} {
		t.Run(name, func(t *testing.T) {
			assertPowerShell51Parses(t, script)
		})
	}
}

func TestBoundedCommandOutputCapturesOnlyItsLimit(t *testing.T) {
	output := boundedCommandOutput{maximum: 4}
	written, err := output.Write([]byte("abcdef"))
	if err != nil || written != 6 || output.buffer.String() != "abcd" || !output.overflow {
		t.Fatalf("bounded output = %q overflow=%t written=%d error=%v", output.buffer.String(), output.overflow, written, err)
	}
	if text := output.text(); !strings.Contains(text, "abcd") || !strings.Contains(text, "truncated") {
		t.Fatalf("bounded output text = %q", text)
	}
	contents := output.buffer.Bytes()
	output.clear()
	if output.buffer.Len() != 0 || output.overflow || !bytes.Equal(contents, make([]byte, len(contents))) {
		t.Fatalf("bounded output was not cleared: bytes=%v overflow=%t length=%d", contents, output.overflow, output.buffer.Len())
	}
}

func TestSecretSSHErrorsRedactRemoteDiagnostics(t *testing.T) {
	const marker = "privkey:test-only-secret-marker"
	err := sshPowerShellError("capture stable Tailscale identity", errors.New("exit status 1"), nil, marker, false)
	if strings.Contains(err.Error(), marker) || !strings.Contains(err.Error(), "redacted") {
		t.Fatalf("secret SSH error = %v", err)
	}
	diagnostic := sshPowerShellError("ordinary role", errors.New("exit status 1"), nil, marker, true)
	if !strings.Contains(diagnostic.Error(), marker) {
		t.Fatalf("ordinary SSH error lost bounded diagnostics: %v", diagnostic)
	}
}

func assertPowerShell51Parses(t *testing.T, script string) {
	t.Helper()
	if runtime.GOOS != "windows" {
		t.Skip("Windows PowerShell 5.1 parser boundary")
	}
	path := filepath.Join(t.TempDir(), "tailscale.ps1")
	if err := os.WriteFile(path, []byte(script), 0o600); err != nil {
		t.Fatal(err)
	}
	parser := `$tokens = $null
$errors = $null
[void][Management.Automation.Language.Parser]::ParseFile($env:HERDR_SANDBOX_TEST_SCRIPT, [ref]$tokens, [ref]$errors)
if ($errors.Count -ne 0) { throw ($errors | ForEach-Object { $_.ToString() } | Out-String) }`
	command := hiddenCommand(mustWindowsPowerShellPath(t), "-NoLogo", "-NoProfile", "-NonInteractive", "-EncodedCommand", encodePowerShell(parser))
	command.Env = append(childProcessEnvironment(os.Environ()), "HERDR_SANDBOX_TEST_SCRIPT="+path)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("Windows PowerShell 5.1 parse: %v: %s", err, output)
	}
}
