package sandbox

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

//go:embed assets/remote-ssh-keys.ps1
var remoteSSHKeysScript string

// ApplyRemoteKeys updates the existing tailnet endpoint without provisioning or
// restarting anything. False means no active managed Sandbox exists yet.
func ApplyRemoteKeys(ctx context.Context, approvedKeys []string) (applied bool, resultErr error) {
	release, err := acquireLifecycleMutationLock(ctx)
	if err != nil {
		return false, err
	}
	defer func() {
		if err := release(); resultErr == nil && err != nil {
			resultErr = err
		}
	}()
	dataDirectory, err := defaultDataDirectory()
	if err != nil {
		return false, err
	}
	executable, err := windowsSandboxExecutable()
	if err != nil {
		return false, err
	}
	active, found, err := loadActiveSession(dataDirectory, executable)
	if err != nil || !found {
		return false, err
	}
	snapshot, running, err := inspectSandboxProcess(ctx, active.PID)
	if err != nil {
		return false, err
	}
	if !running {
		return false, errors.New("recorded Sandbox is no longer running; inspect `sandbox status` before starting it")
	}
	if err := active.matches(snapshot); err != nil {
		return false, err
	}
	status, err := classifyManagedSession(dataDirectory, active)
	if err != nil {
		return false, err
	}
	if status.State != SessionReady || !active.Tailscale || active.InstallerProvisioning {
		return false, errors.New("live remote access requires a ready Sandbox with Tailscale already enabled; settings are saved, but the current Sandbox was not changed")
	}
	root, err := os.UserConfigDir()
	if err != nil {
		return false, err
	}
	configuration, err := loadGlobalConfiguration(filepath.Join(root, applicationName, globalConfigurationName))
	if err != nil {
		return false, err
	}
	if !configuration.Tailscale {
		return false, errors.New("remote access is no longer enabled in configuration")
	}
	if err := verifyApprovedRemoteKeys(configuration.RemoteSSHAuthorizedKeys, approvedKeys); err != nil {
		return false, err
	}
	provisioning := provisioningPlan{Tailscale: true, RemoteSSHAuthorizedKeys: configuration.RemoteSSHAuthorizedKeys}
	if err := includeRemotePublicKey(filepath.Join(dataDirectory, "identity", "id_ed25519.pub"), &provisioning); err != nil {
		return false, err
	}
	runDirectory := filepath.Join(dataDirectory, "runs", active.RunID)
	ready, found, err := readOptionalStatus[readyStatus](filepath.Join(runDirectory, "status", readyFileName))
	if err != nil {
		return false, err
	}
	if !found {
		return false, errors.New("ready Sandbox identity disappeared before remote access update")
	}
	if err := ready.validate(); err != nil {
		return false, err
	}
	plan := runPlan{ID: active.RunID, DataDirectory: dataDirectory, RunDirectory: runDirectory,
		StatusDirectory: filepath.Join(runDirectory, "status"), PrivateKeyPath: filepath.Join(dataDirectory, "identity", "id_ed25519")}
	connection, err := writeRunConnection(plan, connectableStatus(connectionStatus(ready)), "")
	if err != nil {
		return false, err
	}
	inputPath := remoteSSHAuthorizedKeysInputPath(filepath.Join(runDirectory, "input"))
	original, exists, err := readBoundedRegularFile(inputPath, maximumRemoteSSHAuthorizedKeys*maximumRemoteSSHAuthorizedKeySize)
	if err != nil {
		return false, err
	}
	if !exists {
		return false, errors.New("active remote authorization snapshot is missing")
	}
	if _, err := decodeRemoteSSHAuthorizedKeys(original); err != nil {
		return false, err
	}
	keys := provisioning.RemoteSSHAuthorizedKeys
	launcher, err := remoteKeysUpdateScript(keys)
	if err != nil {
		return false, err
	}
	operationContext, cancel := context.WithTimeout(ctx, remoteSSHVerificationTimeout)
	defer cancel()
	output, err := runSSHPowerShell(operationContext, connection, nil, launcher, "update remote SSH access", maximumRemoteSSHResultBytes)
	if err != nil {
		return false, err
	}
	if _, err := decodeRemoteSSHRuntimeResult(output); err != nil {
		return false, err
	}
	encoded, err := encodeRemoteSSHAuthorizedKeys(keys)
	if err != nil {
		return false, err
	}
	if err := writeFileAtomicallyIfUnchanged(inputPath, original, encoded, 0o600); err != nil {
		return false, fmt.Errorf("remote keys applied, but active plan could not be saved; repeat remote setup to reconcile: %w", err)
	}
	return true, nil
}

func verifyApprovedRemoteKeys(configured, approved []string) error {
	keys, err := canonicalizeRemoteSSHAuthorizedKeys(configured)
	if err != nil {
		return err
	}
	if !sameRemoteSSHAuthorizedKeys(keys, approved) {
		return errors.New("configured remote keys changed after approval; no guest keys were changed, repeat remote setup to review the current set")
	}
	return nil
}

func remoteKeysUpdateScript(keys []string) (string, error) {
	encoded, err := encodeRemoteSSHAuthorizedKeys(keys)
	if err != nil {
		return "", err
	}
	if len(keys) == 0 {
		return "", errors.New("remote access requires at least one authorized key")
	}
	return fmt.Sprintf("$expectedScriptDigest = '%x'\n$authorizedKeysBase64 = '%s'\n%s", sha256.Sum256(remoteSSHScript), base64.StdEncoding.EncodeToString(encoded), remoteSSHKeysScript), nil
}
