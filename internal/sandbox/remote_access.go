package sandbox

import (
	"crypto/ecdh"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"math/big"
	"net"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"rsc.io/qr"
)

const (
	remoteSSHPort                     = 22
	remoteSSHUser                     = "WDAGUtilityAccount"
	remoteSSHAuthorizedKeysFileName   = "remote_authorized_keys"
	maximumRemoteSSHAuthorizedKeys    = 8
	maximumRemoteSSHAuthorizedKeySize = 4096
	remoteSSHQRQuietZone              = 4
)

// RemoteAccess is the non-secret connection profile for general SSH access
// endpoint exposed only through the opted-in Tailscale node.
type RemoteAccess struct {
	URI                string
	DNSName            string
	IPv4               string
	SSHUser            string
	Port               int
	HostKeyFingerprint string
	AuthorizedKeyCount int
}

func (access RemoteAccess) QRLines() ([]string, error) {
	return renderRemoteAccessQR(access.URI)
}

func canonicalizeRemoteSSHAuthorizedKeys(values []string) ([]string, error) {
	if len(values) > maximumRemoteSSHAuthorizedKeys {
		return nil, fmt.Errorf("remoteSSHAuthorizedKeys contains %d keys, maximum is %d", len(values), maximumRemoteSSHAuthorizedKeys)
	}
	keys := make([]string, 0, len(values))
	seen := make(map[string]bool, len(values))
	for index, value := range values {
		key, err := canonicalRemotePublicKey(value)
		if err != nil {
			return nil, fmt.Errorf("remoteSSHAuthorizedKeys[%d]: %w", index, err)
		}
		if seen[key] {
			return nil, fmt.Errorf("remoteSSHAuthorizedKeys[%d] duplicates another key", index)
		}
		seen[key] = true
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys, nil
}

// canonicalRemotePublicKey never drops authorized_keys options, which could broaden access.
func canonicalRemotePublicKey(value string) (string, error) {
	if len(value) > maximumRemoteSSHAuthorizedKeySize || strings.TrimSpace(value) != value || strings.ContainsAny(value, "\x00\r\n") {
		return "", errors.New("expected a bounded single-line SSH public key")
	}
	fields := strings.Fields(value)
	if len(fields) < 2 {
		return "", errors.New("expected an SSH public key")
	}
	if fields[0] == "ssh-ed25519" {
		key, _, err := canonicalEd25519PublicKey(strings.Join(fields[:2], " "))
		return key, err
	}
	raw, err := base64.StdEncoding.DecodeString(fields[1])
	if err != nil || base64.StdEncoding.EncodeToString(raw) != fields[1] {
		return "", errors.New("SSH public key must contain canonical base64")
	}
	algorithm, rest, ok := readSSHPublicKeyString(raw)
	if !ok || string(algorithm) != fields[0] {
		return "", errors.New("SSH key algorithm mismatch; restricted authorized_keys entries cannot be imported")
	}
	first, rest, ok := readSSHPublicKeyString(rest)
	if !ok {
		return "", errors.New("SSH public key payload is incomplete")
	}
	second, rest, ok := readSSHPublicKeyString(rest)
	if !ok || len(rest) != 0 {
		return "", errors.New("SSH public key payload is invalid")
	}
	switch fields[0] {
	case "ssh-rsa":
		if len(first) == 0 || len(second) == 0 || first[0]&128 != 0 || second[0]&128 != 0 {
			return "", errors.New("RSA public key integers must be positive")
		}
		exponent, modulus := new(big.Int).SetBytes(first), new(big.Int).SetBytes(second)
		if !exponent.IsInt64() || exponent.Int64() < 3 || exponent.Int64() > 2147483647 || exponent.Bit(0) == 0 || modulus.BitLen() < 2048 || modulus.BitLen() > 16384 || modulus.Bit(0) == 0 {
			return "", errors.New("RSA public key must have a valid exponent and a 2048..16384-bit modulus")
		}
	case "ecdsa-sha2-nistp256", "ecdsa-sha2-nistp384", "ecdsa-sha2-nistp521":
		if string(first) != strings.TrimPrefix(fields[0], "ecdsa-sha2-") {
			return "", errors.New("ECDSA curve does not match the key algorithm")
		}
		curves := map[string]ecdh.Curve{"nistp256": ecdh.P256(), "nistp384": ecdh.P384(), "nistp521": ecdh.P521()}
		if _, err := curves[string(first)].NewPublicKey(second); err != nil {
			return "", errors.New("ECDSA public key point is invalid")
		}
	default:
		return "", errors.New("use an Ed25519, RSA or ECDSA public key without authorized_keys restrictions")
	}
	return strings.Join(fields[:2], " "), nil
}

func canonicalEd25519PublicKey(value string) (string, []byte, error) {
	if value == "" || len(value) > maximumRemoteSSHAuthorizedKeySize || strings.TrimSpace(value) != value || strings.ContainsAny(value, "\x00\r\n") {
		return "", nil, errors.New("expected one bounded single-line Ed25519 public key")
	}
	fields := strings.Fields(value)
	if len(fields) < 2 || len(fields) > 3 || fields[0] != "ssh-ed25519" {
		return "", nil, errors.New("expected one Ed25519 public key with an optional single-token comment")
	}
	encoded := fields[1]
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", nil, fmt.Errorf("decode Ed25519 public key: %w", err)
	}
	if base64.StdEncoding.EncodeToString(raw) != encoded {
		return "", nil, errors.New("Ed25519 public key is not canonical base64")
	}
	algorithm, remainder, ok := readSSHPublicKeyString(raw)
	if !ok || string(algorithm) != "ssh-ed25519" {
		return "", nil, errors.New("Ed25519 public key algorithm payload is invalid")
	}
	publicKey, remainder, ok := readSSHPublicKeyString(remainder)
	if !ok || len(publicKey) != 32 || len(remainder) != 0 {
		return "", nil, errors.New("Ed25519 public key payload must contain exactly 32 key bytes")
	}
	canonical := "ssh-ed25519 " + encoded
	return canonical, raw, nil
}

func readSSHPublicKeyString(data []byte) (value, remainder []byte, ok bool) {
	if len(data) < 4 {
		return nil, nil, false
	}
	length := uint64(binary.BigEndian.Uint32(data[:4]))
	if length > uint64(len(data)-4) {
		return nil, nil, false
	}
	end := 4 + int(length)
	return data[4:end], data[end:], true
}

func encodeRemoteSSHAuthorizedKeys(keys []string) ([]byte, error) {
	canonical, err := canonicalizeRemoteSSHAuthorizedKeys(keys)
	if err != nil {
		return nil, err
	}
	if len(canonical) == 0 {
		return []byte{}, nil
	}
	return []byte(strings.Join(canonical, "\n") + "\n"), nil
}

func decodeRemoteSSHAuthorizedKeys(data []byte) ([]string, error) {
	if len(data) == 0 {
		return []string{}, nil
	}
	if len(data) > maximumRemoteSSHAuthorizedKeys*maximumRemoteSSHAuthorizedKeySize {
		return nil, errors.New("remote SSH authorized-key input is too large")
	}
	if data[len(data)-1] != '\n' || strings.Contains(string(data), "\r") {
		return nil, errors.New("remote SSH authorized-key input is not canonical LF-delimited text")
	}
	lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	keys, err := canonicalizeRemoteSSHAuthorizedKeys(lines)
	if err != nil {
		return nil, err
	}
	encoded, err := encodeRemoteSSHAuthorizedKeys(keys)
	if err != nil {
		return nil, err
	}
	if string(encoded) != string(data) {
		return nil, errors.New("remote SSH authorized-key input is not sorted canonical text")
	}
	return keys, nil
}

func buildRemoteAccess(identity tailscaleIdentity, hostPublicKey string, authorizedKeyCount int) (RemoteAccess, error) {
	if authorizedKeyCount < 1 || authorizedKeyCount > maximumRemoteSSHAuthorizedKeys {
		return RemoteAccess{}, errors.New("remote SSH access requires a bounded nonempty authorized-key set")
	}
	dnsName, err := remoteTailscaleDNSName(identity)
	if err != nil {
		return RemoteAccess{}, err
	}
	ip := net.ParseIP(identity.IPv4)
	if ip == nil || ip.To4() == nil || strings.Contains(identity.IPv4, ":") {
		return RemoteAccess{}, errors.New("remote SSH access requires one valid Tailscale IPv4 address")
	}
	_, hostKeyBlob, err := canonicalEd25519PublicKey(strings.TrimSpace(hostPublicKey))
	if err != nil {
		return RemoteAccess{}, fmt.Errorf("validate remote SSH host key: %w", err)
	}
	digest := sha256.Sum256(hostKeyBlob)
	host := net.JoinHostPort(dnsName, strconv.Itoa(remoteSSHPort))
	uri := (&url.URL{Scheme: "ssh", User: url.User(remoteSSHUser), Host: host}).String()
	return RemoteAccess{
		URI:                uri,
		DNSName:            dnsName,
		IPv4:               identity.IPv4,
		SSHUser:            remoteSSHUser,
		Port:               remoteSSHPort,
		HostKeyFingerprint: "SHA256:" + base64.RawStdEncoding.EncodeToString(digest[:]),
		AuthorizedKeyCount: authorizedKeyCount,
	}, nil
}

func remoteTailscaleDNSName(identity tailscaleIdentity) (string, error) {
	dnsName := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(identity.DNSName), "."))
	if len(dnsName) == 0 || len(dnsName) > 253 || !strings.HasPrefix(dnsName, tailscaleHostName+".") || !strings.HasSuffix(dnsName, ".ts.net") {
		return "", errors.New("remote SSH access requires the stable herdr-sandbox MagicDNS name")
	}
	for label := range strings.SplitSeq(dnsName, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return "", errors.New("remote SSH access MagicDNS name is invalid")
		}
		for _, character := range label {
			if (character < 'a' || character > 'z') && (character < '0' || character > '9') && character != '-' {
				return "", errors.New("remote SSH access MagicDNS name is invalid")
			}
		}
	}
	return dnsName, nil
}

func renderRemoteAccessQR(uri string) ([]string, error) {
	if uri == "" || len(uri) > 512 || strings.TrimSpace(uri) != uri || strings.ContainsAny(uri, "\x00\r\n") {
		return nil, errors.New("remote SSH QR payload is invalid")
	}
	code, err := qr.Encode(uri, qr.Q)
	if err != nil {
		return nil, fmt.Errorf("encode remote SSH QR: %w", err)
	}
	size := code.Size + 2*remoteSSHQRQuietZone
	lines := make([]string, 0, size)
	for y := -remoteSSHQRQuietZone; y < code.Size+remoteSSHQRQuietZone; y++ {
		var line strings.Builder
		line.Grow(2 * size)
		for x := -remoteSSHQRQuietZone; x < code.Size+remoteSSHQRQuietZone; x++ {
			if code.Black(x, y) {
				line.WriteString("##")
			} else {
				line.WriteString("  ")
			}
		}
		lines = append(lines, line.String())
	}
	return lines, nil
}
