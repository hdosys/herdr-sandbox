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
	mobileSSHIdentityFileName       = "mobile-ssh-identity.json"
	mobileSSHIdentitySchemaVersion  = 1
	mobileSSHProtectedSchemaVersion = 1
	maximumMobileSSHPrivateKeyBytes = 16 * 1024
	maximumMobileSSHIdentityBytes   = 64 * 1024
)

var (
	openSSHPrivateKeyStart = []byte("-----BEGIN OPENSSH PRIVATE KEY-----\n")
	openSSHPrivateKeyEnd   = []byte("-----END OPENSSH PRIVATE KEY-----\n")
)

type mobileSSHIdentity struct {
	SchemaVersion int    `json:"schemaVersion"`
	PrivateKey    []byte `json:"privateKey"`
	PublicKey     string `json:"publicKey"`
}

type protectedMobileSSHIdentity struct {
	SchemaVersion int    `json:"schemaVersion"`
	ProtectedData []byte `json:"protectedData"`
}

func mobileSSHIdentityPath(dataDirectory string) string {
	return filepath.Join(dataDirectory, "identity", mobileSSHIdentityFileName)
}

func loadMobileSSHIdentity(dataDirectory string) (mobileSSHIdentity, bool, error) {
	path := mobileSSHIdentityPath(dataDirectory)
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return mobileSSHIdentity{}, false, nil
	}
	if err != nil {
		return mobileSSHIdentity{}, false, fmt.Errorf("inspect protected mobile SSH identity: %w", err)
	}
	reparse, err := fileInfoIsReparsePoint(info)
	if err != nil {
		return mobileSSHIdentity{}, false, fmt.Errorf("inspect protected mobile SSH identity reparse state: %w", err)
	}
	if reparse || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > maximumMobileSSHIdentityBytes {
		return mobileSSHIdentity{}, false, errors.New("protected mobile SSH identity must be one bounded regular non-reparse file")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return mobileSSHIdentity{}, false, fmt.Errorf("read protected mobile SSH identity: %w", err)
	}
	if err := validateExactJSONObjectShape(data, "protected mobile SSH identity", []string{"schemaVersion", "protectedData"}); err != nil {
		return mobileSSHIdentity{}, false, fmt.Errorf("decode protected mobile SSH identity: %w", err)
	}
	var protected protectedMobileSSHIdentity
	if err := decodeStrictJSON(data, &protected); err != nil {
		return mobileSSHIdentity{}, false, fmt.Errorf("decode protected mobile SSH identity: %w", err)
	}
	if protected.SchemaVersion != mobileSSHProtectedSchemaVersion || len(protected.ProtectedData) == 0 || len(protected.ProtectedData) > maximumMobileSSHIdentityBytes {
		return mobileSSHIdentity{}, false, errors.New("protected mobile SSH identity schema or payload is invalid")
	}
	plaintext, err := unprotectLocalData(protected.ProtectedData)
	if err != nil {
		return mobileSSHIdentity{}, false, fmt.Errorf("decrypt protected mobile SSH identity: %w", err)
	}
	defer clear(plaintext)
	if len(plaintext) == 0 || len(plaintext) > maximumMobileSSHIdentityBytes {
		return mobileSSHIdentity{}, false, errors.New("decrypted mobile SSH identity size is invalid")
	}
	if err := validateExactJSONObjectShape(plaintext, "mobile SSH identity", []string{"schemaVersion", "privateKey", "publicKey"}); err != nil {
		return mobileSSHIdentity{}, false, fmt.Errorf("decode mobile SSH identity: %w", err)
	}
	var identity mobileSSHIdentity
	if err := decodeStrictJSON(plaintext, &identity); err != nil {
		clear(identity.PrivateKey)
		return mobileSSHIdentity{}, false, fmt.Errorf("decode mobile SSH identity: %w", err)
	}
	if err := identity.validate(); err != nil {
		clear(identity.PrivateKey)
		return mobileSSHIdentity{}, false, fmt.Errorf("validate mobile SSH identity: %w", err)
	}
	return identity, true, nil
}

func storeMobileSSHIdentity(dataDirectory string, identity mobileSSHIdentity) error {
	if err := identity.validate(); err != nil {
		return fmt.Errorf("validate captured mobile SSH identity: %w", err)
	}
	plaintext, err := json.Marshal(identity)
	if err != nil {
		return fmt.Errorf("encode mobile SSH identity: %w", err)
	}
	defer clear(plaintext)
	protectedData, err := protectLocalData(plaintext)
	if err != nil {
		return fmt.Errorf("encrypt mobile SSH identity: %w", err)
	}
	defer clear(protectedData)
	protected := protectedMobileSSHIdentity{SchemaVersion: mobileSSHProtectedSchemaVersion, ProtectedData: protectedData}
	encoded, err := json.MarshalIndent(protected, "", "  ")
	if err != nil {
		return fmt.Errorf("encode protected mobile SSH identity: %w", err)
	}
	encoded = append(encoded, '\n')
	identityDirectory := filepath.Join(dataDirectory, "identity")
	if err := os.MkdirAll(identityDirectory, 0o700); err != nil {
		return fmt.Errorf("create protected mobile SSH identity directory: %w", err)
	}
	if err := writeProtectedIdentityAtomically(mobileSSHIdentityPath(dataDirectory), encoded); err != nil {
		return fmt.Errorf("publish protected mobile SSH identity: %w", err)
	}
	return nil
}

func (identity mobileSSHIdentity) validate() error {
	if identity.SchemaVersion != mobileSSHIdentitySchemaVersion {
		return fmt.Errorf("schemaVersion = %d, want %d", identity.SchemaVersion, mobileSSHIdentitySchemaVersion)
	}
	if len(identity.PrivateKey) == 0 || len(identity.PrivateKey) > maximumMobileSSHPrivateKeyBytes ||
		!bytes.HasPrefix(identity.PrivateKey, openSSHPrivateKeyStart) || !bytes.HasSuffix(identity.PrivateKey, openSSHPrivateKeyEnd) ||
		bytes.ContainsAny(identity.PrivateKey, "\x00\r") {
		return errors.New("privateKey is not one bounded canonical OpenSSH private key")
	}
	body := bytes.TrimSuffix(bytes.TrimPrefix(identity.PrivateKey, openSSHPrivateKeyStart), openSSHPrivateKeyEnd)
	if len(body) == 0 || body[len(body)-1] != '\n' {
		return errors.New("privateKey body is empty")
	}
	body = bytes.TrimSuffix(body, []byte{'\n'})
	for _, line := range bytes.Split(body, []byte{'\n'}) {
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

func (identity *mobileSSHIdentity) clear() {
	if identity == nil {
		return
	}
	clear(identity.PrivateKey)
	identity.PrivateKey = nil
}
