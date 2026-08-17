package sandbox

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestCanonicalizeMobileSSHAuthorizedKeysRequiresExactUniqueEd25519Keys(t *testing.T) {
	second := testEd25519PublicKey(2)
	first := testEd25519PublicKey(1)
	keys, err := canonicalizeMobileSSHAuthorizedKeys([]string{second + " tablet", first + " phone"})
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
		if _, err := canonicalizeMobileSSHAuthorizedKeys([]string{value}); err == nil {
			t.Fatalf("invalid mobile key accepted: %q", value)
		}
	}
	if _, err := canonicalizeMobileSSHAuthorizedKeys([]string{first, first + " duplicate-comment"}); err == nil {
		t.Fatal("duplicate mobile key accepted")
	}
}

func TestMobileSSHAuthorizedKeyTextIsCanonicalAndRoundTrips(t *testing.T) {
	first := testEd25519PublicKey(1)
	second := testEd25519PublicKey(2)
	encoded, err := encodeMobileSSHAuthorizedKeys([]string{second, first})
	if err != nil {
		t.Fatal(err)
	}
	want := first + "\n" + second + "\n"
	if string(encoded) != want {
		t.Fatalf("encoded keys = %q, want %q", encoded, want)
	}
	decoded, err := decodeMobileSSHAuthorizedKeys(encoded)
	if err != nil || len(decoded) != 2 || decoded[0] != first || decoded[1] != second {
		t.Fatalf("decoded keys = %#v, error = %v", decoded, err)
	}
	for _, invalid := range [][]byte{
		[]byte(first),
		[]byte(first + "\r\n"),
		[]byte(second + "\n" + first + "\n"),
	} {
		if _, err := decodeMobileSSHAuthorizedKeys(invalid); err == nil {
			t.Fatalf("noncanonical key text accepted: %q", invalid)
		}
	}
}

func TestMobileSSHAuthorizedKeyRunInputIsBoundedAndTreatsOldRunAsEmpty(t *testing.T) {
	inputDirectory := t.TempDir()
	keys, err := readMobileSSHAuthorizedKeysInput(inputDirectory)
	if err != nil || len(keys) != 0 {
		t.Fatalf("old run keys = %#v, error = %v", keys, err)
	}
	want := []string{testEd25519PublicKey(1), testEd25519PublicKey(2)}
	if err := writeMobileSSHAuthorizedKeysInput(inputDirectory, want); err != nil {
		t.Fatal(err)
	}
	got, err := readMobileSSHAuthorizedKeysInput(inputDirectory)
	if err != nil || !sameMobileSSHAuthorizedKeys(got, want) {
		t.Fatalf("run input keys = %#v, error = %v", got, err)
	}
}

func TestResolveProvisioningRequiresTailscaleForCanonicalMobileKeys(t *testing.T) {
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
	configuration := `{"tailscale":true,"mobileSSHAuthorizedKeys":[` + strconv.Quote(key+" phone") + `]}`
	if err := os.WriteFile(configurationPath, []byte(configuration), 0o600); err != nil {
		t.Fatal(err)
	}
	plan, err := resolveProvisioningAt(root, global, defaults)
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Tailscale || len(plan.MobileSSHAuthorizedKeys) != 1 || plan.MobileSSHAuthorizedKeys[0] != key {
		t.Fatalf("mobile provisioning plan = %#v", plan)
	}
	if err := os.WriteFile(configurationPath, []byte(`{"mobileSSHAuthorizedKeys":[`+strconv.Quote(key)+`]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := resolveProvisioningAt(root, global, defaults); err == nil || !strings.Contains(err.Error(), "requires tailscale") {
		t.Fatalf("mobile keys without Tailscale error = %v", err)
	}
	for _, invalid := range []string{
		`{"mobileSSHAuthorizedKeys":null}`,
		`{"mobileSSHAuthorizedKeys":"` + key + `"}`,
		`{"mobileSSHAuthorizedKeys":["ssh-rsa AAAA"]}`,
	} {
		if err := os.WriteFile(configurationPath, []byte(invalid), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := resolveProvisioningAt(root, global, defaults); err == nil {
			t.Fatalf("invalid mobile SSH configuration accepted: %s", invalid)
		}
	}
}

func TestBuildMobileAccessUsesStableMagicDNSAndPinnedHostFingerprint(t *testing.T) {
	identity := testTailscaleIdentity(t, "100.64.0.10")
	access, err := buildMobileAccess(identity, testEd25519PublicKey(9)+" host", 2)
	if err != nil {
		t.Fatal(err)
	}
	if access.URI != "ssh://WDAGUtilityAccount@herdr-sandbox.example.ts.net:2222" ||
		access.DNSName != "herdr-sandbox.example.ts.net" || access.IPv4 != identity.IPv4 ||
		access.SSHUser != mobileSSHUser || access.Port != mobileSSHPort || access.AuthorizedKeyCount != 2 ||
		!strings.HasPrefix(access.HostKeyFingerprint, "SHA256:") {
		t.Fatalf("mobile access = %#v", access)
	}
	identity.DNSName = "other.example.ts.net."
	if _, err := buildMobileAccess(identity, testEd25519PublicKey(9), 1); err == nil {
		t.Fatal("unexpected MagicDNS identity accepted")
	}
}

func TestRenderMobileAccessQRHasQuietZoneAndFinderPatterns(t *testing.T) {
	lines, err := renderMobileAccessQR("ssh://WDAGUtilityAccount@herdr-sandbox.example.ts.net:2222")
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
	quietWidth := 2 * mobileSSHQRQuietZone
	for index := range mobileSSHQRQuietZone {
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
	origin := mobileSSHQRQuietZone
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

func TestMobileAccessHandoffIsStrictAndRoundTripsThroughStatusOwner(t *testing.T) {
	identity := testTailscaleIdentity(t, "100.64.0.10")
	access, err := buildMobileAccess(identity, testEd25519PublicKey(9), 1)
	if err != nil {
		t.Fatal(err)
	}
	handoff, err := newMobileAccessHandoff(access)
	if err != nil {
		t.Fatal(err)
	}
	status := configurationHandoffStatus{
		SchemaVersion: statusSchemaVersion,
		Outcome:       configurationHandoffVerified,
		MobileAccess:  handoff,
	}
	directory := t.TempDir()
	if err := writeConfigurationHandoff(directory, status); err != nil {
		t.Fatal(err)
	}
	loaded, found, err := readOptionalStatus[configurationHandoffStatus](filepath.Join(directory, configurationHandoffFileName))
	if err != nil || !found || loaded.MobileAccess == nil || loaded.MobileAccess.URI != access.URI {
		t.Fatalf("loaded mobile handoff = %#v found=%t error=%v", loaded, found, err)
	}
	data, err := os.ReadFile(filepath.Join(directory, configurationHandoffFileName))
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	if len(raw) != 3 || raw["mobileAccess"] == nil {
		t.Fatalf("mobile handoff shape = %s", data)
	}
	invalid := *handoff
	invalid.URI = "ssh://attacker.example:2222"
	status.MobileAccess = &invalid
	if err := status.validate(); err == nil {
		t.Fatal("mismatched mobile URI accepted")
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
