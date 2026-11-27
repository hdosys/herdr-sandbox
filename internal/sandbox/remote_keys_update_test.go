package sandbox

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// This opt-in exercises the production updater against the current guest's real
// tailnet listener. Only its temporary test key is added and removed; no service
// or Sandbox is started, stopped or reprovisioned.
func TestRemoteKeysCurrentSandbox(t *testing.T) {
	if os.Getenv("HERDR_SANDBOX_REMOTE_KEY_TEST") != "1" {
		t.Skip("requires explicit current-Sandbox remote-key acceptance")
	}
	if !strings.EqualFold(os.Getenv("USERNAME"), "WDAGUtilityAccount") {
		t.Fatal("current-Sandbox acceptance refuses host execution")
	}
	root := `C:\HerdrSandbox\remote-ssh`
	original, err := os.ReadFile(filepath.Join(root, "authorized_keys"))
	if err != nil {
		t.Fatal(err)
	}
	keys, err := decodeRemoteSSHAuthorizedKeys(original)
	if err != nil || len(keys) >= maximumRemoteSSHAuthorizedKeys {
		t.Fatalf("current key set cannot admit a temporary acceptance key: %v", err)
	}
	processBefore, err := os.ReadFile(filepath.Join(root, "process.json"))
	if err != nil {
		t.Fatal(err)
	}
	var state struct {
		TailscaleIPv4 string `json:"tailscaleIPv4"`
	}
	if err := json.Unmarshal(processBefore, &state); err != nil || state.TailscaleIPv4 == "" {
		t.Fatal("remote endpoint address unavailable")
	}
	temporary := t.TempDir()
	identity := filepath.Join(temporary, "device")
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()
	keygen := hiddenCommandContext(ctx, filepath.Join(os.Getenv("ProgramFiles"), "OpenSSH", "ssh-keygen.exe"), "-q", "-t", "ed25519", "-N", "", "-f", identity)
	if output, err := keygen.CombinedOutput(); err != nil {
		t.Fatalf("temporary device identity: %v: %s", err, output)
	}
	public, err := os.ReadFile(identity + ".pub")
	if err != nil {
		t.Fatal(err)
	}
	key, err := canonicalRemotePublicKey(strings.TrimSpace(string(public)))
	if err != nil {
		t.Fatal(err)
	}
	hostPublic, err := os.ReadFile(filepath.Join(root, "ssh_host_ed25519_key.pub"))
	if err != nil {
		t.Fatal(err)
	}
	knownHosts := filepath.Join(temporary, "known_hosts")
	if err := os.WriteFile(knownHosts, []byte(state.TailscaleIPv4+" "+strings.TrimSpace(string(hostPublic))+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	connect := func() ([]byte, error) {
		connectionContext, stop := context.WithTimeout(ctx, 15*time.Second)
		defer stop()
		command := hiddenCommandContext(connectionContext, "ssh.exe", "-F", "NUL", "-o", "BatchMode=yes", "-o", "IdentitiesOnly=yes", "-o", "StrictHostKeyChecking=yes",
			"-o", "ConnectTimeout=5", "-o", "UserKnownHostsFile="+knownHosts, "-o", "GlobalKnownHostsFile=NUL", "-i", identity,
			remoteSSHUser+"@"+state.TailscaleIPv4, "echo", "remote-key-accepted")
		return command.CombinedOutput()
	}
	if output, err := connect(); err == nil || !strings.Contains(string(output), "Permission denied (publickey)") {
		t.Fatalf("unapproved device was not rejected by public-key authentication: %v: %s", err, output)
	}
	apply := func(operationContext context.Context, selected []string) error {
		script, err := remoteKeysUpdateScript(selected)
		if err != nil {
			return err
		}
		command := hiddenCommandContext(operationContext, mustWindowsPowerShellPath(t), "-NoLogo", "-NoProfile", "-NonInteractive", "-WindowStyle", "Hidden", "-ExecutionPolicy", "Bypass", "-EncodedCommand", encodePowerShell(script))
		output, err := command.CombinedOutput()
		if err != nil {
			t.Logf("key update failed: %s", output)
			return err
		}
		_, err = decodeRemoteSSHRuntimeResult(output)
		return err
	}
	// Register bounded cleanup before any mutation, including partial failure.
	t.Cleanup(func() {
		cleanupContext, stop := context.WithTimeout(context.Background(), time.Minute)
		defer stop()
		if err := apply(cleanupContext, keys); err != nil {
			t.Errorf("temporary device authorization cleanup failed: %v", err)
		}
	})
	if err := apply(ctx, append(append([]string{}, keys...), key)); err != nil {
		t.Fatal(err)
	}
	if output, err := connect(); err != nil || !strings.Contains(string(output), "remote-key-accepted") {
		t.Fatalf("newly approved device could not connect: %v: %s", err, output)
	}
	if err := apply(ctx, keys); err != nil {
		t.Fatal(err)
	}
	if output, err := connect(); err == nil || !strings.Contains(string(output), "Permission denied (publickey)") {
		t.Fatalf("removed temporary key remained authorized: %v: %s", err, output)
	}
	after, err := os.ReadFile(filepath.Join(root, "authorized_keys"))
	if err != nil || !bytes.Equal(after, original) {
		t.Fatal("original authorization set was not preserved")
	}
	processAfter, err := os.ReadFile(filepath.Join(root, "process.json"))
	if err != nil || !bytes.Equal(processBefore, processAfter) {
		t.Fatal("remote endpoint identity changed")
	}
}
