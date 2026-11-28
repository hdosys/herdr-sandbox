package sandbox

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode"
)

// RemoteDevice is a view of an authorized public key and its optional host label.
type RemoteDevice struct {
	Name        string
	Fingerprint string
	PublicKey   string
	Applied     bool
	Managed     bool
}

type RemoteSettings struct {
	Enabled             bool
	Devices             []RemoteDevice
	AuthorizedKeys      []string
	ConfigurationPath   string
	Session             SessionStatus
	LiveError           string
	UnconfirmedRemovals int
}

type RemoteChange struct {
	Action    string
	Device    string // Exact name or fingerprint, never a display-row index.
	Name      string
	PublicKey string
}

// ReadRemoteSettings inspects desired access and the verified run snapshot. It
// does not enable access, start a guest, or treat a stored endpoint as reachable.
func ReadRemoteSettings(ctx context.Context) (RemoteSettings, error) {
	root, err := os.UserConfigDir()
	if err != nil {
		return RemoteSettings{}, err
	}
	edit, err := readConfigurationEdit(filepath.Join(root, applicationName))
	if err != nil {
		return RemoteSettings{}, err
	}
	settings, err := remoteSettingsFromConfiguration(edit.configuration)
	if err != nil {
		return settings, err
	}
	settings.ConfigurationPath = filepath.Join(edit.root, globalConfigurationName)
	managedKey, err := managedRemotePublicKey()
	if err != nil {
		return settings, err
	}
	for i := range settings.Devices {
		settings.Devices[i].Managed = settings.Devices[i].PublicKey == managedKey
	}
	dataRoot, err := defaultDataDirectory()
	if err != nil {
		return settings, err
	}
	found, err := regularFileExists(filepath.Join(dataRoot, activeSessionFileName))
	if err != nil {
		return settings, err
	}
	if !found {
		settings.Session.State = SessionStopped
		return settings, nil
	}
	settings.Session, err = InspectSession(ctx)
	if err != nil {
		settings.LiveError = err.Error()
		return settings, nil
	}
	if settings.Session.State != SessionReady || settings.Session.RemoteAccess == nil {
		return settings, nil
	}
	if !runIDPattern.MatchString(settings.Session.RunID) {
		return settings, errors.New("invalid active run identity")
	}
	inputRoot := filepath.Join(dataRoot, "runs", settings.Session.RunID, "input")
	if err := rejectMappedPathReparsePoints(inputRoot); err != nil {
		return settings, err
	}
	applied, err := readRemoteSSHAuthorizedKeysInput(inputRoot)
	if err != nil {
		return settings, err
	}
	for i := range settings.Devices {
		settings.Devices[i].Applied = slices.Contains(applied, settings.Devices[i].PublicKey)
	}
	expected := provisioningPlan{Tailscale: true, RemoteSSHAuthorizedKeys: settings.AuthorizedKeys}
	if err := includeRemotePublicKey(filepath.Join(dataRoot, "identity", "id_ed25519.pub"), &expected); err != nil {
		return settings, err
	}
	for _, key := range applied {
		if !slices.Contains(expected.RemoteSSHAuthorizedKeys, key) {
			settings.UnconfirmedRemovals++
		}
	}
	return settings, nil
}

func remoteKeyFingerprint(key string) string {
	fields := strings.Fields(key)
	data, _ := base64.StdEncoding.DecodeString(fields[1]) // key is already canonical and validated.
	digest := sha256.Sum256(data)
	return "SHA256:" + base64.RawStdEncoding.EncodeToString(digest[:])
}

func validateRemoteDeviceName(name string) error {
	if name == "" || name != strings.TrimSpace(name) || len(name) > 80 {
		return errors.New("device name must contain 1 to 80 bytes with no surrounding whitespace")
	}
	for _, r := range name {
		if unicode.IsControl(r) || !unicode.IsPrint(r) {
			return errors.New("device name contains control characters")
		}
	}
	if strings.HasPrefix(name, "SHA256:") {
		return errors.New("device name must not look like a key fingerprint")
	}
	return nil
}

func remoteSettingsFromConfiguration(config globalConfiguration) (RemoteSettings, error) {
	keys, err := canonicalizeRemoteSSHAuthorizedKeys(config.RemoteSSHAuthorizedKeys)
	if err != nil {
		return RemoteSettings{}, err
	}
	if len(keys) >= maximumRemoteSSHAuthorizedKeys {
		return RemoteSettings{}, errors.New("at most seven devices are supported; the managing host keeps its own reserved key")
	}
	if len(keys) > 0 && !config.Tailscale {
		return RemoteSettings{}, errors.New("authorized devices require Tailscale to be enabled")
	}
	result := RemoteSettings{Enabled: config.Tailscale, AuthorizedKeys: keys}
	known := map[string]bool{}
	names := map[string]bool{}
	for _, key := range keys {
		fingerprint := remoteKeyFingerprint(key)
		known[fingerprint] = true
		name := config.RemoteSSHDeviceNames[fingerprint]
		if name != "" {
			if err := validateRemoteDeviceName(name); err != nil {
				return result, err
			}
			if names[strings.ToLower(name)] {
				return result, errors.New("device names must be unique")
			}
			names[strings.ToLower(name)] = true
		}
		result.Devices = append(result.Devices, RemoteDevice{Name: name, Fingerprint: fingerprint, PublicKey: key})
	}
	for fingerprint, name := range config.RemoteSSHDeviceNames {
		if !known[fingerprint] || name == "" {
			return result, errors.New("device labels must reference an authorized key and have a nonempty name")
		}
	}
	return result, nil
}

// ChangeRemoteAccess stores one explicitly reviewed change. The caller applies
// the returned exact key snapshot through ApplyRemoteKeys, even for repeated adds.
func ChangeRemoteAccess(ctx context.Context, change RemoteChange, confirm func(RemoteSettings) bool) (RemoteSettings, bool, error) {
	root, err := os.UserConfigDir()
	if err != nil {
		return RemoteSettings{}, false, err
	}
	managedKey, err := managedRemotePublicKey()
	if err != nil {
		return RemoteSettings{}, false, err
	}
	return changeRemoteAccessAt(ctx, filepath.Join(root, applicationName), change, confirm, managedKey)
}

func managedRemotePublicKey() (string, error) {
	root, err := defaultDataDirectory()
	if err != nil {
		return "", err
	}
	data, found, err := readBoundedRegularFile(filepath.Join(root, "identity", "id_ed25519.pub"), maximumRemoteSSHAuthorizedKeySize)
	if err != nil || !found {
		return "", err
	}
	return canonicalRemotePublicKey(strings.TrimSpace(string(data)))
}

func changeRemoteAccessAt(ctx context.Context, root string, change RemoteChange, confirm func(RemoteSettings) bool, managedKey string) (RemoteSettings, bool, error) {
	edit, err := readConfigurationEdit(root)
	if err != nil {
		return RemoteSettings{}, false, err
	}
	before, err := remoteSettingsFromConfiguration(edit.configuration)
	if err != nil {
		return before, false, err
	}
	keys := slices.Clone(before.AuthorizedKeys)
	names := edit.configuration.RemoteSSHDeviceNames
	if names == nil {
		names = map[string]string{}
	}
	index := -1
	if change.Action == "rename" || change.Action == "remove" || change.Action == "replace" {
		for i, device := range before.Devices {
			if device.Fingerprint == change.Device || device.Name != "" && strings.EqualFold(device.Name, change.Device) {
				index = i
				break
			}
		}
		if index < 0 {
			return before, false, errors.New("device not found; use its exact name or fingerprint from sandbox remote")
		}
		if before.Devices[index].PublicKey == managedKey {
			return before, false, errors.New("the managing host identity is reserved and cannot be edited as a device")
		}
	}
	switch change.Action {
	case "enable":
	case "add", "replace":
		key, err := canonicalRemotePublicKey(strings.TrimSpace(change.PublicKey))
		if err != nil {
			return before, false, fmt.Errorf("invalid public SSH key; nothing changed: %w", err)
		}
		if err := validateRemoteDeviceName(change.Name); err != nil {
			return before, false, err
		}
		fingerprint := remoteKeyFingerprint(key)
		if key == managedKey {
			return before, false, errors.New("the managing host already has reserved access; use the other device's own public key")
		}
		if change.Action == "replace" {
			old := before.Devices[index]
			if old.PublicKey != key && slices.Contains(keys, key) {
				return before, false, errors.New("replacement key already belongs to another device")
			}
			delete(names, old.Fingerprint)
			keys[index] = key
		} else if slices.Contains(keys, key) {
			// A repeated add must not silently rename an already named device.
			if existing := names[fingerprint]; existing != "" && !strings.EqualFold(existing, change.Name) {
				return before, false, fmt.Errorf("this key is already registered as %q; use rename instead", existing)
			}
		} else {
			keys = append(keys, key)
		}
		names[fingerprint] = change.Name
	case "rename":
		if err := validateRemoteDeviceName(change.Name); err != nil {
			return before, false, err
		}
		names[before.Devices[index].Fingerprint] = change.Name
	case "remove":
		delete(names, before.Devices[index].Fingerprint)
		keys = append(keys[:index], keys[index+1:]...)
	default:
		return before, false, errors.New("unknown remote access action")
	}
	edit.configuration.Tailscale = true
	edit.configuration.RemoteSSHAuthorizedKeys = keys
	edit.configuration.RemoteSSHDeviceNames = names
	after, err := remoteSettingsFromConfiguration(edit.configuration)
	if err != nil {
		return before, false, err
	}
	after.ConfigurationPath = filepath.Join(root, globalConfigurationName)
	if confirm == nil || !confirm(after) {
		return before, false, nil
	}
	if err := ctx.Err(); err != nil {
		return before, false, err
	}
	release, err := acquireLifecycleMutationLock(ctx)
	if err != nil {
		return before, false, err
	}
	defer release()
	edit.fields["tailscale"] = json.RawMessage("true")
	edit.fields["remoteSSHAuthorizedKeys"], err = json.Marshal(after.AuthorizedKeys)
	if err != nil {
		return before, false, err
	}
	edit.fields["remoteSSHDeviceNames"], err = json.Marshal(names)
	if err != nil {
		return before, false, err
	}
	if err := edit.save(); err != nil {
		return before, false, err
	}
	return after, true, nil
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
