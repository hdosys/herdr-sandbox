package sandbox

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"testing"
)

func TestMobileSSHControlScriptIsPowerShell51AndOwnsNarrowEndpoint(t *testing.T) {
	script := string(mobileSSHScript)
	for _, required := range []string{
		"[ValidateSet('Prepare', 'Activate', 'Verify')]",
		"$script:MobilePort = 2222",
		"ListenAddress $([string]$request.tailscaleIPv4)",
		"AuthenticationMethods publickey",
		"PasswordAuthentication no",
		"DisableForwarding yes",
		"AllowAgentForwarding no",
		"AllowTcpForwarding no",
		"-RemoteAddress '100.64.0.0/10' -LocalPort $script:MobilePort",
		"-Action Block -RemoteAddress '100.64.0.0/10' -LocalPort 22",
		"Start-Process -FilePath $sshd",
		"-WindowStyle Hidden",
		"[DateTime]::UtcNow.AddSeconds(30)",
		"WaitForExit(15000)",
		"Mobile SSH control script differs from the current application",
		"Mobile SSH host private key no longer matches its stable public identity",
	} {
		if !strings.Contains(script, required) {
			t.Fatalf("mobile SSH control script is missing %q", required)
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
			t.Fatalf("mobile SSH control script contains unsupported or unsafe directive %q", forbidden)
		}
	}
	assertPowerShell51Parses(t, script)
}

func TestBaseProfileRoutesOnlyMobileSSHPortDirectlyIntoHerdr(t *testing.T) {
	base := readDefaultBaseProvisioning(t)
	for _, required := range []string{
		`$env:SSH_CONNECTION -split '\s+'`,
		"$herdrSSHConnection.Count -eq 4",
		"[string]$herdrSSHConnection[3] -ceq '2222'",
		"HERDR_SANDBOX_HERDR_EXE",
		"GetEnvironmentVariable",
		"& $herdrExecutable",
		"exit $LASTEXITCODE",
		"$expectedPowerShellProfile = $mobileSSHInitialization + $starshipInitialization",
		"mobile SSH and Starship profile verification failed",
	} {
		if !strings.Contains(base, required) {
			t.Fatalf("Base mobile SSH profile is missing %q", required)
		}
	}
	if strings.Contains(base, `C:\HerdrSandbox\bin\herdr.exe`) {
		t.Fatal("Base mobile SSH profile retains the replaced Sandbox-owned Herdr path")
	}
	if strings.Index(base, "$mobileSSHInitialization = @'") > strings.Index(base, "$expectedPowerShellProfile =") {
		t.Fatal("Base assembles the PowerShell profile before defining mobile SSH initialization")
	}
}

func TestBootstrapActivatesAndDisplaysOnlyValidatedSecretFreeMobileHandoff(t *testing.T) {
	bootstrap := string(bootstrapScript)
	for _, required := range []string{
		"schemaVersion|outcome|mobileAccess",
		"uri|dnsName|ipv4|sshUser|port|hostKeyFingerprint|qr",
		"'^(?:##|  )+$'",
		"Write-ProgressStatus -Phase 'mobile-access'",
		"$mobileScript -Mode Activate",
		"Mobile Herdr access is ready over Tailscale.",
		"Scan this secret-free QR code",
		"Verify host key:",
		"The device private key never leaves that device.",
	} {
		if !strings.Contains(bootstrap, required) {
			t.Fatalf("bootstrap mobile handoff is missing %q", required)
		}
	}
	activationIndex := strings.Index(bootstrap, "$mobileScript -Mode Activate")
	workspaceIndex := strings.Index(bootstrap, "$createdRootPaneIds[$rootPaneId] = $true")
	readyIndex := strings.Index(bootstrap, "Write-AtomicJson -Path (Join-Path $StatusDirectory 'ready.json')")
	if activationIndex <= workspaceIndex || readyIndex <= activationIndex {
		t.Fatalf("mobile activation ordering is invalid: workspace=%d activation=%d ready=%d", workspaceIndex, activationIndex, readyIndex)
	}
	assertPowerShell51Parses(t, bootstrap)
}

func TestMobileSSHPrepareArchiveContainsOnlyNativeAssetAndSecretRequest(t *testing.T) {
	identity := testMobileSSHIdentity()
	request := mobileSSHPrepareRequest{
		SchemaVersion:  mobileSSHContractSchemaVersion,
		TailscaleIPv4:  "100.64.0.10",
		AuthorizedKeys: []string{testEd25519PublicKey(1)},
		PrivateKey:     append([]byte(nil), identity.PrivateKey...),
		PublicKey:      identity.PublicKey,
		ScriptSHA256:   fmt.Sprintf("%x", sha256.Sum256(mobileSSHScript)),
	}
	archive, err := buildMobileSSHPrepareArchive(request)
	if err != nil {
		t.Fatal(err)
	}
	reader, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		t.Fatal(err)
	}
	if len(reader.File) != 2 || reader.File[0].Name != "mobile-ssh.ps1" || reader.File[1].Name != "request.json" {
		t.Fatalf("mobile SSH archive entries = %#v", reader.File)
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
		t.Fatal("mobile SSH request did not base64-wrap its private key")
	}
	digest := fmt.Sprintf("%x", sha256.Sum256(archive))
	launcher := buildMobileSSHPrepareLauncher(digest, len(archive))
	if strings.Contains(launcher, string(identity.PrivateKey)) || !strings.Contains(launcher, digest) || !strings.Contains(launcher, "Mobile SSH archive must contain exactly two regular files") {
		t.Fatal("mobile SSH launcher leaks secret input or omits archive validation")
	}
	assertPowerShell51Parses(t, launcher)
}

func TestDecodeMobileSSHResultsIsStrictAndClearsIdentityShape(t *testing.T) {
	identity := testMobileSSHIdentity()
	encoded, err := json.Marshal(mobileSSHPrepareResult{
		SchemaVersion: mobileSSHContractSchemaVersion,
		PrivateKey:    identity.PrivateKey,
		PublicKey:     identity.PublicKey,
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := decodeMobileSSHPrepareResult(encoded)
	if err != nil {
		t.Fatal(err)
	}
	result.clear()
	if result.PrivateKey != nil {
		t.Fatal("mobile SSH prepare result did not clear private key")
	}
	invalid := bytes.Replace(encoded, []byte(`"publicKey":`), []byte(`"extra":true,"publicKey":`), 1)
	if _, err := decodeMobileSSHPrepareResult(invalid); err == nil {
		t.Fatal("mobile SSH prepare result accepted an unknown field")
	}
	if _, err := decodeMobileSSHRuntimeResult([]byte(`{"schemaVersion":1,"state":"running","pid":42}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := decodeMobileSSHRuntimeResult([]byte(`{"schemaVersion":1,"state":"stopped","pid":42}`)); err == nil {
		t.Fatal("mobile SSH runtime result accepted a non-running state")
	}
}
