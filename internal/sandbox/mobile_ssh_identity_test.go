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

func TestMobileSSHIdentityDPAPIRoundTripHidesPrivateKey(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows DPAPI boundary")
	}
	dataDirectory := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dataDirectory, "identity"), 0o700); err != nil {
		t.Fatal(err)
	}
	identity := testMobileSSHIdentity()
	if err := storeMobileSSHIdentity(dataDirectory, identity); err != nil {
		t.Fatalf("storeMobileSSHIdentity: %v", err)
	}
	protected, err := os.ReadFile(mobileSSHIdentityPath(dataDirectory))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(protected, identity.PrivateKey) || bytes.Contains(protected, []byte(identity.PublicKey)) {
		t.Fatal("protected mobile SSH identity contains plaintext key material")
	}
	loaded, found, err := loadMobileSSHIdentity(dataDirectory)
	if err != nil || !found {
		t.Fatalf("loadMobileSSHIdentity: found=%t error=%v", found, err)
	}
	defer loaded.clear()
	if !bytes.Equal(loaded.PrivateKey, identity.PrivateKey) || loaded.PublicKey != identity.PublicKey {
		t.Fatal("loaded mobile SSH identity differs")
	}
}

func TestLoadMobileSSHIdentityRejectsTamperedCiphertext(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows DPAPI boundary")
	}
	dataDirectory := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dataDirectory, "identity"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := storeMobileSSHIdentity(dataDirectory, testMobileSSHIdentity()); err != nil {
		t.Fatal(err)
	}
	path := mobileSSHIdentityPath(dataDirectory)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var protected protectedMobileSSHIdentity
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
	if _, _, err := loadMobileSSHIdentity(dataDirectory); err == nil || strings.Contains(err.Error(), "OPENSSH PRIVATE") {
		t.Fatalf("tampered identity error = %v", err)
	}
}

func TestMobileSSHIdentityValidationRejectsMalformedKeys(t *testing.T) {
	identity := testMobileSSHIdentity()
	if err := identity.validate(); err != nil {
		t.Fatalf("valid identity: %v", err)
	}
	identity.PrivateKey = []byte("not a private key")
	if err := identity.validate(); err == nil {
		t.Fatal("malformed private key accepted")
	}
	identity = testMobileSSHIdentity()
	identity.PublicKey = "ssh-rsa AAAA"
	if err := identity.validate(); err == nil {
		t.Fatal("malformed public key accepted")
	}
}

func testMobileSSHIdentity() mobileSSHIdentity {
	return mobileSSHIdentity{
		SchemaVersion: mobileSSHIdentitySchemaVersion,
		PrivateKey: []byte("-----BEGIN OPENSSH PRIVATE KEY-----\n" +
			"dGVzdC1vbmx5LW1vYmlsZS1zc2gtcHJpdmF0ZS1rZXk=\n" +
			"-----END OPENSSH PRIVATE KEY-----\n"),
		PublicKey: testEd25519PublicKey(7),
	}
}
