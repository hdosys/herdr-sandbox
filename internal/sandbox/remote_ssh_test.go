package sandbox

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRemoteSSHInitialRetryReusesVerifiedEndpointInWindowsPowerShell51(t *testing.T) {
	requireExternalBoundaryTest(t, "Windows PowerShell 5.1 remote retry")
	fixture, err := os.ReadFile(filepath.Join("testdata", "retry-remote.ps1"))
	if err != nil {
		t.Fatal(err)
	}
	setup := provisioningPowerShellFunctionSetup(t, provisioningPowerShellFunctionSource{
		path: filepath.Join("assets", "remote-ssh.ps1"), names: []string{"Assert-RepeatedPreparation", "Invoke-Activate"},
	})
	path := filepath.Join(t.TempDir(), "retry-remote.ps1")
	if err := os.WriteFile(path, []byte("$ErrorActionPreference = 'Stop'\n"+setup+string(fixture)), 0o600); err != nil {
		t.Fatal(err)
	}
	command := hiddenCommand(mustWindowsPowerShellPath(t), "-NoLogo", "-NoProfile", "-NonInteractive", "-WindowStyle", "Hidden", "-ExecutionPolicy", "Bypass", "-File", path)
	if output, err := command.CombinedOutput(); err != nil || strings.TrimSpace(string(output)) != "verified" {
		t.Fatalf("remote retry failed: %v: %s", err, output)
	}
}

func TestRemoteSSHControlScriptIsPowerShell51AndOwnsNarrowEndpoint(t *testing.T) {
	script := string(remoteSSHScript)
	for _, required := range []string{
		"[ValidateSet('Prepare', 'Activate', 'Verify')]",
		"$script:RemotePort = 22",
		"ListenAddress $([string]$request.tailscaleIPv4)",
		"AuthenticationMethods publickey",
		"PasswordAuthentication no",
		"DisableForwarding no",
		"AllowAgentForwarding no",
		"AllowTcpForwarding yes",
		"-RemoteAddress '100.64.0.0/10' -LocalPort $script:RemotePort",
		"Start-Process -FilePath $sshd",
		"-WindowStyle Hidden",
		"[DateTime]::UtcNow.AddSeconds(30)",
		"WaitForExit(15000)",
		"Remote SSH control script differs from the current application",
		"Remote SSH host private key no longer matches its stable public identity",
	} {
		if !strings.Contains(script, required) {
			t.Fatalf("remote SSH control script is missing %q", required)
		}
	}
	for _, forbidden := range []string{
		"ForceCommand",
		"PasswordAuthentication yes",
		"ListenAddress 0.0.0.0",
		"KbdInteractiveAuthentication",
		"PermitUserEnvironment",
		"PidFile",
	} {
		if strings.Contains(script, forbidden) {
			t.Fatalf("remote SSH control script contains unsupported or unsafe directive %q", forbidden)
		}
	}
	assertPowerShell51Parses(t, script)
}

func TestRemoteSSHPrepareArchiveContainsOnlyNativeAssetAndSecretRequest(t *testing.T) {
	identity := testRemoteSSHIdentity()
	request := remoteSSHPrepareRequest{
		SchemaVersion:  remoteSSHContractSchemaVersion,
		TailscaleIPv4:  "100.64.0.10",
		AuthorizedKeys: []string{testEd25519PublicKey(1)},
		PrivateKey:     append([]byte(nil), identity.PrivateKey...),
		PublicKey:      identity.PublicKey,
		ScriptSHA256:   fmt.Sprintf("%x", sha256.Sum256(remoteSSHScript)),
	}
	archive, err := buildRemoteSSHPrepareArchive(request)
	if err != nil {
		t.Fatal(err)
	}
	reader, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		t.Fatal(err)
	}
	if len(reader.File) != 2 || reader.File[0].Name != "remote-ssh.ps1" || reader.File[1].Name != "request.json" {
		t.Fatalf("remote SSH archive entries = %#v", reader.File)
	}
	requestFile, err := reader.File[1].Open()
	if err != nil {
		t.Fatal(err)
	}
	requestJSON, err := io.ReadAll(requestFile)
	_ = requestFile.Close()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(requestJSON, []byte(`"privateKey":"`)) || bytes.Contains(requestJSON, identity.PrivateKey) {
		t.Fatal("remote SSH request did not base64-wrap its private key")
	}
	digest := fmt.Sprintf("%x", sha256.Sum256(archive))
	launcher := buildRemoteSSHPrepareLauncher(digest, len(archive))
	if strings.Contains(launcher, string(identity.PrivateKey)) || !strings.Contains(launcher, digest) || !strings.Contains(launcher, "Remote SSH archive must contain exactly two regular files") {
		t.Fatal("remote SSH launcher leaks secret input or omits archive validation")
	}
	assertPowerShell51Parses(t, launcher)
}

func TestDecodeRemoteSSHResultsIsStrictAndClearsIdentityShape(t *testing.T) {
	identity := testRemoteSSHIdentity()
	encoded, err := json.Marshal(remoteSSHPrepareResult{
		SchemaVersion: remoteSSHContractSchemaVersion,
		PrivateKey:    identity.PrivateKey,
		PublicKey:     identity.PublicKey,
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := decodeRemoteSSHPrepareResult(encoded)
	if err != nil {
		t.Fatal(err)
	}
	result.clear()
	if result.PrivateKey != nil {
		t.Fatal("remote SSH prepare result did not clear private key")
	}
	invalid := bytes.Replace(encoded, []byte(`"publicKey":`), []byte(`"extra":true,"publicKey":`), 1)
	if _, err := decodeRemoteSSHPrepareResult(invalid); err == nil {
		t.Fatal("remote SSH prepare result accepted an unknown field")
	}
	if _, err := decodeRemoteSSHRuntimeResult([]byte(`{"schemaVersion":1,"state":"running","pid":42}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := decodeRemoteSSHRuntimeResult([]byte(`{"schemaVersion":1,"state":"stopped","pid":42}`)); err == nil {
		t.Fatal("remote SSH runtime result accepted a non-running state")
	}
}
