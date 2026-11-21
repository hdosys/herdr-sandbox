package sandbox

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// RemoteSetup describes the public authorization changes awaiting confirmation.
type RemoteSetup struct {
	Sources           []string
	AdditionalKeys    int
	ConfigurationPath string
}

// SetupRemote enables tailnet access using the app-owned host identity and an
// explicitly confirmed snapshot of incoming host keys or a selected public file.
func SetupRemote(ctx context.Context, keyFile string, confirm func(RemoteSetup) bool) (bool, error) {
	root, err := os.UserConfigDir()
	if err != nil {
		return false, err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return false, err
	}
	sources := []string{filepath.Join(home, ".ssh", "authorized_keys")}
	if programData := os.Getenv("ProgramData"); filepath.IsAbs(programData) {
		sources = append(sources, filepath.Join(programData, "ssh", "administrators_authorized_keys"))
	}
	if keyFile != "" {
		keyFile, err = filepath.Abs(keyFile)
		if err != nil {
			return false, err
		}
		if exists, err := regularFileExists(keyFile); err != nil || !exists {
			return false, fmt.Errorf("public key file is unavailable: %s: %v", keyFile, err)
		}
		sources = []string{keyFile}
	}
	return setupRemoteAt(ctx, filepath.Join(root, applicationName), sources, confirm)
}

func setupRemoteAt(ctx context.Context, root string, sources []string, confirm func(RemoteSetup) bool) (bool, error) {
	path := filepath.Join(root, globalConfigurationName)
	original, found, err := readBoundedRegularFile(path, 1024*1024)
	if err != nil {
		return false, err
	}
	data := original
	if !found {
		data = defaultGlobalConfiguration
	}
	var configuration globalConfiguration
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := decodeGlobalConfiguration(decoder, &configuration); err != nil {
		return false, err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return false, errors.New("configuration contains trailing data")
	}
	keys, err := canonicalizeRemoteSSHAuthorizedKeys(configuration.RemoteSSHAuthorizedKeys)
	if err != nil {
		return false, err
	}
	preview := RemoteSetup{ConfigurationPath: path}
	for _, source := range sources {
		contents, exists, err := readBoundedRegularFile(source, maximumRemoteSSHAuthorizedKeys*maximumRemoteSSHAuthorizedKeySize)
		if err != nil {
			return false, fmt.Errorf("read host public-key list %s: %w", source, err)
		}
		if !exists {
			continue
		}
		for index, line := range strings.Split(string(contents), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			key, err := canonicalRemotePublicKey(line)
			if err != nil {
				return false, fmt.Errorf("public-key list %s line %d: %w; no access settings were changed", source, index+1, err)
			}
			if !slices.Contains(keys, key) {
				keys = append(keys, key)
			}
		}
		preview.Sources = append(preview.Sources, source)
	}
	keys, err = canonicalizeRemoteSSHAuthorizedKeys(keys)
	if err != nil {
		return false, err
	}
	if len(keys) >= maximumRemoteSSHAuthorizedKeys {
		return false, errors.New("remote access requires one free key slot for this computer")
	}
	preview.AdditionalKeys = len(keys)
	if confirm == nil || !confirm(preview) {
		return false, nil
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	release, err := acquireLifecycleLock(ctx)
	if err != nil {
		return false, err
	}
	defer release()
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return false, err
	}
	fields["tailscale"] = json.RawMessage("true")
	fields["remoteSSHAuthorizedKeys"], err = json.Marshal(keys)
	if err != nil {
		return false, err
	}
	updated, err := json.MarshalIndent(fields, "", "  ")
	if err != nil {
		return false, err
	}
	if _, err := ensurePhysicalDirectory(root, "remote access configuration"); err != nil {
		return false, err
	}
	if err := writeConfigurationReferences(root); err != nil {
		return false, err
	}
	if err := writeFileAtomicallyIfUnchanged(path, original, append(updated, '\n'), 0o600); err != nil {
		return false, err
	}
	return true, nil
}

func includeHostRemoteKey(ctx context.Context, dataDirectory string, provisioning *provisioningPlan) error {
	if !provisioning.Tailscale || provisioning.InstallerProvisioning {
		return nil
	}
	_, publicPath, err := ensureIdentity(ctx, filepath.Join(dataDirectory, "identity"))
	if err != nil {
		return err
	}
	return includeRemotePublicKey(publicPath, provisioning)
}

func includeRemotePublicKey(publicPath string, provisioning *provisioningPlan) error {
	if !provisioning.Tailscale || provisioning.InstallerProvisioning {
		return nil
	}
	data, _, err := readBoundedRegularFile(publicPath, maximumRemoteSSHAuthorizedKeySize)
	if err != nil {
		return err
	}
	key, err := canonicalRemotePublicKey(strings.TrimSpace(string(data)))
	if err != nil {
		return err
	}
	keys := slices.Clone(provisioning.RemoteSSHAuthorizedKeys)
	if !slices.Contains(keys, key) {
		keys = append(keys, key)
	}
	provisioning.RemoteSSHAuthorizedKeys, err = canonicalizeRemoteSSHAuthorizedKeys(keys)
	return err
}
