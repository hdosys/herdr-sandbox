package sandbox

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	tailscaleAuthKeyEnvironment     = "HERDR_SANDBOX_TAILSCALE_AUTH_KEY"
	tailscaleHostName               = "herdr-sandbox"
	tailscaleIdentityFileName       = "tailscale-identity.json"
	tailscaleIdentitySchemaVersion  = 1
	tailscaleProtectedSchemaVersion = 1
	maximumTailscaleAuthKeyBytes    = 4096
	maximumTailscaleStateBytes      = 2 * 1024 * 1024
	maximumTailscaleIdentityBytes   = 4 * 1024 * 1024
)

var windowsSIDPattern = regexp.MustCompile(`^S-[0-9]+(?:-[0-9]+)+$`)

type tailscaleIdentity struct {
	SchemaVersion    int      `json:"schemaVersion"`
	WindowsUserSID   string   `json:"windowsUserSID"`
	NodeID           string   `json:"nodeID"`
	NodeKey          string   `json:"nodeKey"`
	IPv4             string   `json:"ipv4"`
	DNSName          string   `json:"dnsName"`
	HostName         string   `json:"hostName"`
	TailscaleVersion string   `json:"tailscaleVersion"`
	Tags             []string `json:"tags"`
	State            []byte   `json:"state"`
}

type protectedTailscaleIdentity struct {
	SchemaVersion int    `json:"schemaVersion"`
	ProtectedData []byte `json:"protectedData"`
}

func consumeTailscaleAuthKeyEnvironment() ([]byte, bool, error) {
	value, found := os.LookupEnv(tailscaleAuthKeyEnvironment)
	if err := os.Unsetenv(tailscaleAuthKeyEnvironment); err != nil {
		return nil, false, fmt.Errorf("clear Tailscale auth-key environment: %w", err)
	}
	if !found {
		return nil, false, nil
	}
	if value == "" || len(value) > maximumTailscaleAuthKeyBytes || strings.TrimSpace(value) != value ||
		strings.ContainsAny(value, "\x00\r\n\t ") || !strings.HasPrefix(value, "tskey-") {
		return nil, false, errors.New("HERDR_SANDBOX_TAILSCALE_AUTH_KEY must contain one bounded Tailscale auth key without whitespace")
	}
	return []byte(value), true, nil
}

func tailscaleIdentityPath(dataDirectory string) string {
	return filepath.Join(dataDirectory, "identity", tailscaleIdentityFileName)
}

func loadTailscaleIdentity(dataDirectory string) (tailscaleIdentity, bool, error) {
	path := tailscaleIdentityPath(dataDirectory)
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return tailscaleIdentity{}, false, nil
	}
	if err != nil {
		return tailscaleIdentity{}, false, fmt.Errorf("inspect protected Tailscale identity: %w", err)
	}
	reparse, err := fileInfoIsReparsePoint(info)
	if err != nil {
		return tailscaleIdentity{}, false, fmt.Errorf("inspect protected Tailscale identity reparse state: %w", err)
	}
	if reparse || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > maximumTailscaleIdentityBytes {
		return tailscaleIdentity{}, false, errors.New("protected Tailscale identity must be one bounded regular non-reparse file")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return tailscaleIdentity{}, false, fmt.Errorf("read protected Tailscale identity: %w", err)
	}
	if err := validateExactJSONObjectShape(data, "protected Tailscale identity", []string{"schemaVersion", "protectedData"}); err != nil {
		return tailscaleIdentity{}, false, fmt.Errorf("decode protected Tailscale identity: %w", err)
	}
	var protected protectedTailscaleIdentity
	if err := decodeStrictJSON(data, &protected); err != nil {
		return tailscaleIdentity{}, false, fmt.Errorf("decode protected Tailscale identity: %w", err)
	}
	if protected.SchemaVersion != tailscaleProtectedSchemaVersion || len(protected.ProtectedData) == 0 || len(protected.ProtectedData) > maximumTailscaleIdentityBytes {
		return tailscaleIdentity{}, false, errors.New("protected Tailscale identity schema or payload is invalid")
	}
	plaintext, err := unprotectLocalData(protected.ProtectedData)
	if err != nil {
		return tailscaleIdentity{}, false, fmt.Errorf("decrypt protected Tailscale identity: %w", err)
	}
	defer clear(plaintext)
	if len(plaintext) == 0 || len(plaintext) > maximumTailscaleIdentityBytes {
		return tailscaleIdentity{}, false, errors.New("decrypted Tailscale identity size is invalid")
	}
	if err := validateExactJSONObjectShape(plaintext, "Tailscale identity", []string{
		"schemaVersion", "windowsUserSID", "nodeID", "nodeKey", "ipv4", "dnsName", "hostName", "tailscaleVersion", "tags", "state",
	}); err != nil {
		return tailscaleIdentity{}, false, fmt.Errorf("decode Tailscale identity: %w", err)
	}
	var identity tailscaleIdentity
	if err := decodeStrictJSON(plaintext, &identity); err != nil {
		clear(identity.State)
		return tailscaleIdentity{}, false, fmt.Errorf("decode Tailscale identity: %w", err)
	}
	if err := identity.validate(); err != nil {
		clear(identity.State)
		return tailscaleIdentity{}, false, fmt.Errorf("validate Tailscale identity: %w", err)
	}
	return identity, true, nil
}

func storeTailscaleIdentity(dataDirectory string, identity tailscaleIdentity) error {
	if err := identity.validate(); err != nil {
		return fmt.Errorf("validate captured Tailscale identity: %w", err)
	}
	plaintext, err := json.Marshal(identity)
	if err != nil {
		return fmt.Errorf("encode Tailscale identity: %w", err)
	}
	defer clear(plaintext)
	protectedData, err := protectLocalData(plaintext)
	if err != nil {
		return fmt.Errorf("encrypt Tailscale identity: %w", err)
	}
	defer clear(protectedData)
	protected := protectedTailscaleIdentity{SchemaVersion: tailscaleProtectedSchemaVersion, ProtectedData: protectedData}
	encoded, err := json.MarshalIndent(protected, "", "  ")
	if err != nil {
		return fmt.Errorf("encode protected Tailscale identity: %w", err)
	}
	encoded = append(encoded, '\n')
	identityDirectory := filepath.Join(dataDirectory, "identity")
	if err := os.MkdirAll(identityDirectory, 0o700); err != nil {
		return fmt.Errorf("create protected Tailscale identity directory: %w", err)
	}
	if err := writeProtectedIdentityAtomically(tailscaleIdentityPath(dataDirectory), encoded); err != nil {
		return fmt.Errorf("publish protected Tailscale identity: %w", err)
	}
	return nil
}

func writeProtectedIdentityAtomically(path string, data []byte) error {
	directory := filepath.Dir(path)
	temporary, err := os.CreateTemp(directory, ".tailscale-identity-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
		return os.Rename(temporaryPath, path)
	} else if err != nil {
		return err
	}
	return replaceFileAtomically(path, temporaryPath, "")
}

func decodeStrictJSON(data []byte, destination any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("trailing JSON data")
	}
	return nil
}

func (identity tailscaleIdentity) validate() error {
	if identity.SchemaVersion != tailscaleIdentitySchemaVersion {
		return fmt.Errorf("schemaVersion = %d, want %d", identity.SchemaVersion, tailscaleIdentitySchemaVersion)
	}
	if len(identity.WindowsUserSID) > 184 || !windowsSIDPattern.MatchString(identity.WindowsUserSID) {
		return errors.New("windowsUserSID is invalid")
	}
	for name, value := range map[string]string{
		"nodeID": identity.NodeID, "nodeKey": identity.NodeKey, "dnsName": identity.DNSName, "tailscaleVersion": identity.TailscaleVersion,
	} {
		if strings.TrimSpace(value) == "" || len(value) > 512 || strings.ContainsAny(value, "\x00\r\n") {
			return fmt.Errorf("%s is empty, multiline, or too large", name)
		}
	}
	if !strings.HasPrefix(identity.NodeKey, "nodekey:") {
		return errors.New("nodeKey is invalid")
	}
	if identity.HostName != tailscaleHostName {
		return fmt.Errorf("hostName = %q, want %q", identity.HostName, tailscaleHostName)
	}
	if ip := net.ParseIP(identity.IPv4); ip == nil || ip.To4() == nil || strings.Contains(identity.IPv4, ":") {
		return errors.New("ipv4 is invalid")
	}
	if len(identity.Tags) == 0 || len(identity.Tags) > 16 {
		return errors.New("tags must contain at least one bounded tag")
	}
	seenTags := map[string]bool{}
	for _, tag := range identity.Tags {
		if len(tag) > 256 || !strings.HasPrefix(tag, "tag:") || strings.ContainsAny(tag, "\x00\r\n\t ") || seenTags[tag] {
			return errors.New("tags contain an invalid or duplicate value")
		}
		seenTags[tag] = true
	}
	if len(identity.State) == 0 || len(identity.State) > maximumTailscaleStateBytes {
		return errors.New("state size is invalid")
	}
	var state map[string]json.RawMessage
	clearState := func() {
		for key, value := range state {
			clear(value)
			delete(state, key)
		}
	}
	if err := json.Unmarshal(identity.State, &state); err != nil {
		clearState()
		return errors.New("state is not valid JSON")
	}
	defer clearState()
	machineKeyJSON, ok := state["_machinekey"]
	if !ok {
		return errors.New("state does not contain a plaintext machine key")
	}
	var machineKeyBase64 string
	if err := json.Unmarshal(machineKeyJSON, &machineKeyBase64); err != nil || machineKeyBase64 == "" {
		return errors.New("state machine key is invalid")
	}
	machineKey, err := base64.StdEncoding.DecodeString(machineKeyBase64)
	if err != nil || len(machineKey) == 0 {
		return errors.New("state machine key is invalid")
	}
	validMachineKey := bytes.HasPrefix(machineKey, []byte("privkey:"))
	clear(machineKey)
	if !validMachineKey {
		return errors.New("state machine key is invalid")
	}
	return nil
}
