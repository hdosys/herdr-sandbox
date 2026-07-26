package sandbox

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestConsumeTailscaleAuthKeyEnvironmentClearsValue(t *testing.T) {
	const key = "tskey-auth-test-only-value"
	t.Setenv(tailscaleAuthKeyEnvironment, key)
	got, found, err := consumeTailscaleAuthKeyEnvironment()
	if err != nil {
		t.Fatalf("consumeTailscaleAuthKeyEnvironment: %v", err)
	}
	defer clear(got)
	if !found || string(got) != key {
		t.Fatalf("key found = %t, value matches = %t", found, string(got) == key)
	}
	if _, found := os.LookupEnv(tailscaleAuthKeyEnvironment); found {
		t.Fatal("auth key remained in the process environment")
	}
}

func TestConsumeTailscaleAuthKeyEnvironmentRejectsSecretSafely(t *testing.T) {
	const secret = "not-a-key secret-value"
	t.Setenv(tailscaleAuthKeyEnvironment, secret)
	_, _, err := consumeTailscaleAuthKeyEnvironment()
	if err == nil {
		t.Fatal("invalid key unexpectedly succeeded")
	}
	if strings.Contains(err.Error(), secret) || strings.Contains(err.Error(), "secret-value") {
		t.Fatalf("error disclosed auth key: %v", err)
	}
	if _, found := os.LookupEnv(tailscaleAuthKeyEnvironment); found {
		t.Fatal("invalid auth key remained in the process environment")
	}
}

func TestTailscaleIdentityDPAPIRoundTripAndReplacement(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows DPAPI boundary")
	}
	dataDirectory := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dataDirectory, "identity"), 0o700); err != nil {
		t.Fatal(err)
	}
	identity := testTailscaleIdentity(t, "100.64.0.10")
	if err := storeTailscaleIdentity(dataDirectory, identity); err != nil {
		t.Fatalf("storeTailscaleIdentity: %v", err)
	}
	encoded, err := os.ReadFile(tailscaleIdentityPath(dataDirectory))
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range [][]byte{identity.State, []byte(identity.NodeKey), []byte("privkey:test-only-machine-key")} {
		if bytes.Contains(encoded, secret) {
			t.Fatal("protected identity file contains plaintext identity material")
		}
	}
	loaded, found, err := loadTailscaleIdentity(dataDirectory)
	if err != nil || !found {
		t.Fatalf("loadTailscaleIdentity: found=%t err=%v", found, err)
	}
	if !bytes.Equal(loaded.State, identity.State) || loaded.NodeID != identity.NodeID || loaded.IPv4 != identity.IPv4 {
		t.Fatalf("loaded identity does not match: %#v", loaded)
	}
	identity.IPv4 = "100.64.0.11"
	if err := storeTailscaleIdentity(dataDirectory, identity); err != nil {
		t.Fatalf("replace Tailscale identity: %v", err)
	}
	loaded, found, err = loadTailscaleIdentity(dataDirectory)
	if err != nil || !found || loaded.IPv4 != identity.IPv4 {
		t.Fatalf("load replaced identity: %#v found=%t err=%v", loaded, found, err)
	}
}

func TestLoadTailscaleIdentityRejectsTamperedCiphertext(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows DPAPI boundary")
	}
	dataDirectory := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dataDirectory, "identity"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := storeTailscaleIdentity(dataDirectory, testTailscaleIdentity(t, "100.64.0.10")); err != nil {
		t.Fatal(err)
	}
	path := tailscaleIdentityPath(dataDirectory)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var protected protectedTailscaleIdentity
	if err := json.Unmarshal(data, &protected); err != nil {
		t.Fatal(err)
	}
	protected.ProtectedData[len(protected.ProtectedData)/2] ^= 0xff
	data, err = json.Marshal(protected)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := loadTailscaleIdentity(dataDirectory); err == nil || strings.Contains(err.Error(), "privkey:") {
		t.Fatalf("tampered identity error = %v", err)
	}
}

func TestTailscaleIdentityRequiresTaggedPlaintextState(t *testing.T) {
	identity := testTailscaleIdentity(t, "100.64.0.10")
	identity.Tags = nil
	if err := identity.validate(); err == nil {
		t.Fatal("untagged identity unexpectedly validated")
	}
	identity = testTailscaleIdentity(t, "100.64.0.10")
	identity.State = []byte(`{"key":"sealed","nonce":"value","data":"value"}`)
	if err := identity.validate(); err == nil || !strings.Contains(err.Error(), "plaintext machine key") {
		t.Fatalf("TPM-sealed state error = %v", err)
	}
	identity = testTailscaleIdentity(t, "100.64.0.10")
	identity.State = []byte(`{"_machinekey":"bm90LWEtcHJpdmF0ZS1rZXk="}`)
	if err := identity.validate(); err == nil || !strings.Contains(err.Error(), "machine key is invalid") {
		t.Fatalf("malformed plaintext machine key error = %v", err)
	}
}

func testTailscaleIdentity(t *testing.T, ipv4 string) tailscaleIdentity {
	t.Helper()
	state, err := json.Marshal(map[string][]byte{
		"_machinekey":  []byte("privkey:test-only-machine-key"),
		"profile-abcd": []byte(`{"Config":{"PrivateNodeKey":"privkey:test-only-node-key"}}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	return tailscaleIdentity{
		SchemaVersion:    tailscaleIdentitySchemaVersion,
		WindowsUserSID:   "S-1-5-21-1-2-3-504",
		NodeID:           "node-test-stable-id",
		NodeKey:          "nodekey:test-public-node-key",
		IPv4:             ipv4,
		DNSName:          "herdr-sandbox.example.ts.net.",
		HostName:         tailscaleHostName,
		TailscaleVersion: "1.98.9",
		Tags:             []string{"tag:herdr-sandbox"},
		State:            state,
	}
}
