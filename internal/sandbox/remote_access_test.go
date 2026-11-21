package sandbox

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestCanonicalizeRemoteSSHAuthorizedKeysRequiresExactUniqueEd25519Keys(t *testing.T) {
	second := testEd25519PublicKey(2)
	first := testEd25519PublicKey(1)
	keys, err := canonicalizeRemoteSSHAuthorizedKeys([]string{second + " tablet", first + " phone"})
	if err != nil {
		t.Fatalf("canonicalize keys: %v", err)
	}
	if len(keys) != 2 || keys[0] != first || keys[1] != second {
		t.Fatalf("canonical keys = %#v", keys)
	}
	invalid := []string{
		"ssh-rsa AAAA",
		"ssh-ed25519 AAAA",
		first + " comment with spaces",
		first + "\n" + second,
		first + " trailing ",
	}
	for _, value := range invalid {
		if _, err := canonicalizeRemoteSSHAuthorizedKeys([]string{value}); err == nil {
			t.Fatalf("invalid remote key accepted: %q", value)
		}
	}
	if _, err := canonicalizeRemoteSSHAuthorizedKeys([]string{first, first + " duplicate-comment"}); err == nil {
		t.Fatal("duplicate remote key accepted")
	}
}

func TestRemoteSSHAuthorizedKeyTextIsCanonicalAndRoundTrips(t *testing.T) {
	first := testEd25519PublicKey(1)
	second := testEd25519PublicKey(2)
	encoded, err := encodeRemoteSSHAuthorizedKeys([]string{second, first})
	if err != nil {
		t.Fatal(err)
	}
	want := first + "\n" + second + "\n"
	if string(encoded) != want {
		t.Fatalf("encoded keys = %q, want %q", encoded, want)
	}
	decoded, err := decodeRemoteSSHAuthorizedKeys(encoded)
	if err != nil || len(decoded) != 2 || decoded[0] != first || decoded[1] != second {
		t.Fatalf("decoded keys = %#v, error = %v", decoded, err)
	}
	for _, invalid := range [][]byte{
		[]byte(first),
		[]byte(first + "\r\n"),
		[]byte(second + "\n" + first + "\n"),
	} {
		if _, err := decodeRemoteSSHAuthorizedKeys(invalid); err == nil {
			t.Fatalf("noncanonical key text accepted: %q", invalid)
		}
	}
}

func TestRemoteSSHAuthorizedKeyRunInputIsBoundedAndTreatsOldRunAsEmpty(t *testing.T) {
	inputDirectory := t.TempDir()
	keys, err := readRemoteSSHAuthorizedKeysInput(inputDirectory)
	if err != nil || len(keys) != 0 {
		t.Fatalf("old run keys = %#v, error = %v", keys, err)
	}
	want := []string{testEd25519PublicKey(1), testEd25519PublicKey(2)}
	if err := writeRemoteSSHAuthorizedKeysInput(inputDirectory, want); err != nil {
		t.Fatal(err)
	}
	got, err := readRemoteSSHAuthorizedKeysInput(inputDirectory)
	if err != nil || !sameRemoteSSHAuthorizedKeys(got, want) {
		t.Fatalf("run input keys = %#v, error = %v", got, err)
	}
}

func TestResolveProvisioningRequiresTailscaleForCanonicalRemoteKeys(t *testing.T) {
	root := t.TempDir()
	defaults := filepath.Join(root, "defaults")
	global := filepath.Join(root, "global")
	for _, directory := range []string{defaults, global} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(defaults, baseProvisioningName), []byte("base"), 0o600); err != nil {
		t.Fatal(err)
	}
	key := testEd25519PublicKey(1)
	configurationPath := filepath.Join(global, globalConfigurationName)
	configuration := `{"tailscale":true,"remoteSSHAuthorizedKeys":[` + strconv.Quote(key+" phone") + `]}`
	if err := os.WriteFile(configurationPath, []byte(configuration), 0o600); err != nil {
		t.Fatal(err)
	}
	plan, err := resolveProvisioningAt(root, global, defaults)
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Tailscale || len(plan.RemoteSSHAuthorizedKeys) != 1 || plan.RemoteSSHAuthorizedKeys[0] != key {
		t.Fatalf("remote provisioning plan = %#v", plan)
	}
	if err := os.WriteFile(configurationPath, []byte(`{"remoteSSHAuthorizedKeys":[`+strconv.Quote(key)+`]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := resolveProvisioningAt(root, global, defaults); err == nil || !strings.Contains(err.Error(), "requires tailscale") {
		t.Fatalf("remote keys without Tailscale error = %v", err)
	}
	for _, invalid := range []string{
		`{"remoteSSHAuthorizedKeys":null}`,
		`{"remoteSSHAuthorizedKeys":"` + key + `"}`,
		`{"remoteSSHAuthorizedKeys":["ssh-rsa AAAA"]}`,
	} {
		if err := os.WriteFile(configurationPath, []byte(invalid), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := resolveProvisioningAt(root, global, defaults); err == nil {
			t.Fatalf("invalid remote SSH configuration accepted: %s", invalid)
		}
	}
}

func TestBuildRemoteAccessUsesStableMagicDNSAndPinnedHostFingerprint(t *testing.T) {
	identity := testTailscaleIdentity(t, "100.64.0.10")
	access, err := buildRemoteAccess(identity, testEd25519PublicKey(9)+" host", 2)
	if err != nil {
		t.Fatal(err)
	}
	if access.URI != "ssh://WDAGUtilityAccount@herdr-sandbox.example.ts.net:2222" ||
		access.DNSName != "herdr-sandbox.example.ts.net" || access.IPv4 != identity.IPv4 ||
		access.SSHUser != remoteSSHUser || access.Port != remoteSSHPort || access.AuthorizedKeyCount != 2 ||
		!strings.HasPrefix(access.HostKeyFingerprint, "SHA256:") {
		t.Fatalf("remote access = %#v", access)
	}
	identity.DNSName = "other.example.ts.net."
	if _, err := buildRemoteAccess(identity, testEd25519PublicKey(9), 1); err == nil {
		t.Fatal("unexpected MagicDNS identity accepted")
	}
}

func TestRenderRemoteAccessQRHasQuietZoneAndFinderPatterns(t *testing.T) {
	lines, err := renderRemoteAccessQR("ssh://WDAGUtilityAccount@herdr-sandbox.example.ts.net:2222")
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) < 29 || len(lines) > 65 {
		t.Fatalf("QR line count = %d", len(lines))
	}
	width := len(lines[0])
	for index, line := range lines {
		if len(line) != width || strings.Trim(line, " #") != "" {
			t.Fatalf("QR line %d is malformed: %q", index, line)
		}
	}
	quietWidth := 2 * remoteSSHQRQuietZone
	for index := range remoteSSHQRQuietZone {
		if strings.TrimSpace(lines[index]) != "" || strings.TrimSpace(lines[len(lines)-1-index]) != "" {
			t.Fatalf("QR quiet row %d is not white", index)
		}
	}
	for _, line := range lines {
		if strings.TrimSpace(line[:quietWidth]) != "" || strings.TrimSpace(line[len(line)-quietWidth:]) != "" {
			t.Fatal("QR quiet columns are not white")
		}
	}
	// Every QR code begins with a 7x7 finder whose outer ring is black.
	origin := remoteSSHQRQuietZone
	for y := range 7 {
		for x := range 7 {
			outer := x == 0 || x == 6 || y == 0 || y == 6
			center := x >= 2 && x <= 4 && y >= 2 && y <= 4
			module := lines[origin+y][2*(origin+x) : 2*(origin+x+1)]
			if (outer || center) != (module == "##") {
				t.Fatalf("finder module (%d,%d) = %q", x, y, module)
			}
		}
	}
}

func testEd25519PublicKey(fill byte) string {
	algorithm := []byte("ssh-ed25519")
	key := bytes.Repeat([]byte{fill}, 32)
	blob := make([]byte, 4+len(algorithm)+4+len(key))
	binary.BigEndian.PutUint32(blob[:4], uint32(len(algorithm)))
	copy(blob[4:], algorithm)
	offset := 4 + len(algorithm)
	binary.BigEndian.PutUint32(blob[offset:offset+4], uint32(len(key)))
	copy(blob[offset+4:], key)
	return "ssh-ed25519 " + base64.StdEncoding.EncodeToString(blob)
}
