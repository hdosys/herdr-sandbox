package sandbox

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"rsc.io/qr"
)

const (
	mobileSSHPort                     = 2222
	mobileSSHUser                     = "WDAGUtilityAccount"
	mobileSSHAuthorizedKeysFileName   = "mobile_authorized_keys"
	maximumMobileSSHAuthorizedKeys    = 8
	maximumMobileSSHAuthorizedKeySize = 1024
	mobileSSHQRQuietZone              = 4
)

// MobileAccess is the non-secret connection profile for the dedicated Herdr
// endpoint exposed only through the opted-in Tailscale node.
type MobileAccess struct {
	URI                string
	DNSName            string
	IPv4               string
	SSHUser            string
	Port               int
	HostKeyFingerprint string
	AuthorizedKeyCount int
}

func (access MobileAccess) QRLines() ([]string, error) {
	return renderMobileAccessQR(access.URI)
}

type mobileAccessHandoff struct {
	URI                string   `json:"uri"`
	DNSName            string   `json:"dnsName"`
	IPv4               string   `json:"ipv4"`
	SSHUser            string   `json:"sshUser"`
	Port               int      `json:"port"`
	HostKeyFingerprint string   `json:"hostKeyFingerprint"`
	QR                 []string `json:"qr"`
}

func newMobileAccessHandoff(access MobileAccess) (*mobileAccessHandoff, error) {
	qrLines, err := renderMobileAccessQR(access.URI)
	if err != nil {
		return nil, err
	}
	handoff := &mobileAccessHandoff{
		URI:                access.URI,
		DNSName:            access.DNSName,
		IPv4:               access.IPv4,
		SSHUser:            access.SSHUser,
		Port:               access.Port,
		HostKeyFingerprint: access.HostKeyFingerprint,
		QR:                 qrLines,
	}
	if err := handoff.validate(); err != nil {
		return nil, err
	}
	return handoff, nil
}

func (handoff mobileAccessHandoff) validate() error {
	if handoff.SSHUser != mobileSSHUser || handoff.Port != mobileSSHPort {
		return errors.New("mobile access handoff SSH user or port is invalid")
	}
	dnsName, err := mobileTailscaleDNSName(tailscaleIdentity{DNSName: handoff.DNSName, HostName: tailscaleHostName})
	if err != nil || dnsName != handoff.DNSName {
		return errors.New("mobile access handoff MagicDNS name is invalid")
	}
	ip := net.ParseIP(handoff.IPv4)
	if ip == nil || ip.To4() == nil || strings.Contains(handoff.IPv4, ":") {
		return errors.New("mobile access handoff IPv4 address is invalid")
	}
	expectedURI := (&url.URL{
		Scheme: "ssh",
		User:   url.User(mobileSSHUser),
		Host:   net.JoinHostPort(handoff.DNSName, strconv.Itoa(mobileSSHPort)),
	}).String()
	if handoff.URI != expectedURI {
		return errors.New("mobile access handoff URI does not match its endpoint")
	}
	if !strings.HasPrefix(handoff.HostKeyFingerprint, "SHA256:") {
		return errors.New("mobile access handoff host-key fingerprint is invalid")
	}
	fingerprint, err := base64.RawStdEncoding.DecodeString(strings.TrimPrefix(handoff.HostKeyFingerprint, "SHA256:"))
	if err != nil || len(fingerprint) != sha256.Size {
		return errors.New("mobile access handoff host-key fingerprint is invalid")
	}
	if len(handoff.QR) < 29 || len(handoff.QR) > 65 {
		return errors.New("mobile access handoff QR height is invalid")
	}
	wantWidth := 2 * len(handoff.QR)
	for _, line := range handoff.QR {
		if len(line) != wantWidth {
			return errors.New("mobile access handoff QR is not square")
		}
		for index := 0; index < len(line); index += 2 {
			if module := line[index : index+2]; module != "  " && module != "##" {
				return errors.New("mobile access handoff QR contains an invalid module")
			}
		}
	}
	return nil
}

func canonicalizeMobileSSHAuthorizedKeys(values []string) ([]string, error) {
	if len(values) > maximumMobileSSHAuthorizedKeys {
		return nil, fmt.Errorf("mobileSSHAuthorizedKeys contains %d keys, maximum is %d", len(values), maximumMobileSSHAuthorizedKeys)
	}
	keys := make([]string, 0, len(values))
	seen := make(map[string]bool, len(values))
	for index, value := range values {
		key, _, err := canonicalEd25519PublicKey(value)
		if err != nil {
			return nil, fmt.Errorf("mobileSSHAuthorizedKeys[%d]: %w", index, err)
		}
		if seen[key] {
			return nil, fmt.Errorf("mobileSSHAuthorizedKeys[%d] duplicates another key", index)
		}
		seen[key] = true
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys, nil
}

func canonicalEd25519PublicKey(value string) (string, []byte, error) {
	if value == "" || len(value) > maximumMobileSSHAuthorizedKeySize || strings.TrimSpace(value) != value || strings.ContainsAny(value, "\x00\r\n") {
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

func encodeMobileSSHAuthorizedKeys(keys []string) ([]byte, error) {
	canonical, err := canonicalizeMobileSSHAuthorizedKeys(keys)
	if err != nil {
		return nil, err
	}
	if len(canonical) == 0 {
		return []byte{}, nil
	}
	return []byte(strings.Join(canonical, "\n") + "\n"), nil
}

func decodeMobileSSHAuthorizedKeys(data []byte) ([]string, error) {
	if len(data) == 0 {
		return []string{}, nil
	}
	if len(data) > maximumMobileSSHAuthorizedKeys*maximumMobileSSHAuthorizedKeySize {
		return nil, errors.New("mobile SSH authorized-key input is too large")
	}
	if data[len(data)-1] != '\n' || strings.Contains(string(data), "\r") {
		return nil, errors.New("mobile SSH authorized-key input is not canonical LF-delimited text")
	}
	lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	keys, err := canonicalizeMobileSSHAuthorizedKeys(lines)
	if err != nil {
		return nil, err
	}
	encoded, err := encodeMobileSSHAuthorizedKeys(keys)
	if err != nil {
		return nil, err
	}
	if string(encoded) != string(data) {
		return nil, errors.New("mobile SSH authorized-key input is not sorted canonical text")
	}
	return keys, nil
}

func buildMobileAccess(identity tailscaleIdentity, hostPublicKey string, authorizedKeyCount int) (MobileAccess, error) {
	if authorizedKeyCount < 1 || authorizedKeyCount > maximumMobileSSHAuthorizedKeys {
		return MobileAccess{}, errors.New("mobile SSH access requires a bounded nonempty authorized-key set")
	}
	dnsName, err := mobileTailscaleDNSName(identity)
	if err != nil {
		return MobileAccess{}, err
	}
	ip := net.ParseIP(identity.IPv4)
	if ip == nil || ip.To4() == nil || strings.Contains(identity.IPv4, ":") {
		return MobileAccess{}, errors.New("mobile SSH access requires one valid Tailscale IPv4 address")
	}
	_, hostKeyBlob, err := canonicalEd25519PublicKey(strings.TrimSpace(hostPublicKey))
	if err != nil {
		return MobileAccess{}, fmt.Errorf("validate mobile SSH host key: %w", err)
	}
	digest := sha256.Sum256(hostKeyBlob)
	host := net.JoinHostPort(dnsName, strconv.Itoa(mobileSSHPort))
	uri := (&url.URL{Scheme: "ssh", User: url.User(mobileSSHUser), Host: host}).String()
	return MobileAccess{
		URI:                uri,
		DNSName:            dnsName,
		IPv4:               identity.IPv4,
		SSHUser:            mobileSSHUser,
		Port:               mobileSSHPort,
		HostKeyFingerprint: "SHA256:" + base64.RawStdEncoding.EncodeToString(digest[:]),
		AuthorizedKeyCount: authorizedKeyCount,
	}, nil
}

func mobileTailscaleDNSName(identity tailscaleIdentity) (string, error) {
	dnsName := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(identity.DNSName), "."))
	if len(dnsName) == 0 || len(dnsName) > 253 || !strings.HasPrefix(dnsName, tailscaleHostName+".") || !strings.HasSuffix(dnsName, ".ts.net") {
		return "", errors.New("mobile SSH access requires the stable herdr-sandbox MagicDNS name")
	}
	for _, label := range strings.Split(dnsName, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return "", errors.New("mobile SSH access MagicDNS name is invalid")
		}
		for _, character := range label {
			if (character < 'a' || character > 'z') && (character < '0' || character > '9') && character != '-' {
				return "", errors.New("mobile SSH access MagicDNS name is invalid")
			}
		}
	}
	return dnsName, nil
}

func renderMobileAccessQR(uri string) ([]string, error) {
	if uri == "" || len(uri) > 512 || strings.TrimSpace(uri) != uri || strings.ContainsAny(uri, "\x00\r\n") {
		return nil, errors.New("mobile SSH QR payload is invalid")
	}
	code, err := qr.Encode(uri, qr.Q)
	if err != nil {
		return nil, fmt.Errorf("encode mobile SSH QR: %w", err)
	}
	size := code.Size + 2*mobileSSHQRQuietZone
	lines := make([]string, 0, size)
	for y := -mobileSSHQRQuietZone; y < code.Size+mobileSSHQRQuietZone; y++ {
		var line strings.Builder
		line.Grow(2 * size)
		for x := -mobileSSHQRQuietZone; x < code.Size+mobileSSHQRQuietZone; x++ {
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
