package sandbox

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	remoteSSHIdentityFileName       = "remote-ssh-identity.json"
	remoteSSHIdentitySchemaVersion  = 1
	remoteSSHProtectedSchemaVersion = 1
	maximumRemoteSSHPrivateKeyBytes = 16 * 1024
	maximumRemoteSSHIdentityBytes   = 64 * 1024
)

var (
	openSSHPrivateKeyStart = []byte("-----BEGIN OPENSSH PRIVATE KEY-----\n")
	openSSHPrivateKeyEnd   = []byte("-----END OPENSSH PRIVATE KEY-----\n")
)

type remoteSSHIdentity struct {
	SchemaVersion int    `json:"schemaVersion"`
	PrivateKey    []byte `json:"privateKey"`
	PublicKey     string `json:"publicKey"`
}

type protectedRemoteSSHIdentity struct {
	SchemaVersion int    `json:"schemaVersion"`
	ProtectedData []byte `json:"protectedData"`
}

func remoteSSHIdentityPath(dataDirectory string) string {
	return filepath.Join(dataDirectory, "identity", remoteSSHIdentityFileName)
}

func loadRemoteSSHIdentity(dataDirectory string) (remoteSSHIdentity, bool, error) {
	path := remoteSSHIdentityPath(dataDirectory)
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return remoteSSHIdentity{}, false, nil
	}
	if err != nil {
		return remoteSSHIdentity{}, false, fmt.Errorf("inspect protected remote SSH identity: %w", err)
	}
	reparse, err := fileInfoIsReparsePoint(info)
	if err != nil {
		return remoteSSHIdentity{}, false, fmt.Errorf("inspect protected remote SSH identity reparse state: %w", err)
	}
	if reparse || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > maximumRemoteSSHIdentityBytes {
		return remoteSSHIdentity{}, false, errors.New("protected remote SSH identity must be one bounded regular non-reparse file")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return remoteSSHIdentity{}, false, fmt.Errorf("read protected remote SSH identity: %w", err)
	}
	if err := validateExactJSONObjectShape(data, "protected remote SSH identity", []string{"schemaVersion", "protectedData"}); err != nil {
		return remoteSSHIdentity{}, false, fmt.Errorf("decode protected remote SSH identity: %w", err)
	}
	var protected protectedRemoteSSHIdentity
	if err := decodeStrictJSON(data, &protected); err != nil {
		return remoteSSHIdentity{}, false, fmt.Errorf("decode protected remote SSH identity: %w", err)
	}
	if protected.SchemaVersion != remoteSSHProtectedSchemaVersion || len(protected.ProtectedData) == 0 || len(protected.ProtectedData) > maximumRemoteSSHIdentityBytes {
		return remoteSSHIdentity{}, false, errors.New("protected remote SSH identity schema or payload is invalid")
	}
	plaintext, err := unprotectLocalData(protected.ProtectedData)
	if err != nil {
		return remoteSSHIdentity{}, false, fmt.Errorf("decrypt protected remote SSH identity: %w", err)
	}
	defer clear(plaintext)
	if len(plaintext) == 0 || len(plaintext) > maximumRemoteSSHIdentityBytes {
		return remoteSSHIdentity{}, false, errors.New("decrypted remote SSH identity size is invalid")
	}
	if err := validateExactJSONObjectShape(plaintext, "remote SSH identity", []string{"schemaVersion", "privateKey", "publicKey"}); err != nil {
		return remoteSSHIdentity{}, false, fmt.Errorf("decode remote SSH identity: %w", err)
	}
	var identity remoteSSHIdentity
	if err := decodeStrictJSON(plaintext, &identity); err != nil {
		clear(identity.PrivateKey)
		return remoteSSHIdentity{}, false, fmt.Errorf("decode remote SSH identity: %w", err)
	}
	if err := identity.validate(); err != nil {
		clear(identity.PrivateKey)
		return remoteSSHIdentity{}, false, fmt.Errorf("validate remote SSH identity: %w", err)
	}
	return identity, true, nil
}

func storeRemoteSSHIdentity(dataDirectory string, identity remoteSSHIdentity) error {
	if err := identity.validate(); err != nil {
		return fmt.Errorf("validate captured remote SSH identity: %w", err)
	}
	plaintext, err := json.Marshal(identity)
	if err != nil {
		return fmt.Errorf("encode remote SSH identity: %w", err)
	}
	defer clear(plaintext)
	protectedData, err := protectLocalData(plaintext)
	if err != nil {
		return fmt.Errorf("encrypt remote SSH identity: %w", err)
	}
	defer clear(protectedData)
	protected := protectedRemoteSSHIdentity{SchemaVersion: remoteSSHProtectedSchemaVersion, ProtectedData: protectedData}
	encoded, err := json.MarshalIndent(protected, "", "  ")
	if err != nil {
		return fmt.Errorf("encode protected remote SSH identity: %w", err)
	}
	encoded = append(encoded, '\n')
	identityDirectory := filepath.Join(dataDirectory, "identity")
	if err := os.MkdirAll(identityDirectory, 0o700); err != nil {
		return fmt.Errorf("create protected remote SSH identity directory: %w", err)
	}
	if err := writeProtectedIdentityAtomically(remoteSSHIdentityPath(dataDirectory), encoded); err != nil {
		return fmt.Errorf("publish protected remote SSH identity: %w", err)
	}
	return nil
}

func (identity remoteSSHIdentity) validate() error {
	if identity.SchemaVersion != remoteSSHIdentitySchemaVersion {
		return fmt.Errorf("schemaVersion = %d, want %d", identity.SchemaVersion, remoteSSHIdentitySchemaVersion)
	}
	if len(identity.PrivateKey) == 0 || len(identity.PrivateKey) > maximumRemoteSSHPrivateKeyBytes ||
		!bytes.HasPrefix(identity.PrivateKey, openSSHPrivateKeyStart) || !bytes.HasSuffix(identity.PrivateKey, openSSHPrivateKeyEnd) ||
		bytes.ContainsAny(identity.PrivateKey, "\x00\r") {
		return errors.New("privateKey is not one bounded canonical OpenSSH private key")
	}
	body := bytes.TrimSuffix(bytes.TrimPrefix(identity.PrivateKey, openSSHPrivateKeyStart), openSSHPrivateKeyEnd)
	if len(body) == 0 || body[len(body)-1] != '\n' {
		return errors.New("privateKey body is empty")
	}
	body = bytes.TrimSuffix(body, []byte{'\n'})
	for line := range bytes.SplitSeq(body, []byte{'\n'}) {
		if len(line) == 0 || len(line) > 128 || strings.Trim(string(line), "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/=") != "" {
			return errors.New("privateKey body is not canonical base64 text")
		}
	}
	canonical, _, err := canonicalEd25519PublicKey(strings.TrimSpace(identity.PublicKey))
	if err != nil {
		return fmt.Errorf("publicKey: %w", err)
	}
	if identity.PublicKey != canonical {
		return errors.New("publicKey is not canonical")
	}
	return nil
}

func (identity *remoteSSHIdentity) clear() {
	if identity == nil {
		return
	}
	clear(identity.PrivateKey)
	identity.PrivateKey = nil
}
