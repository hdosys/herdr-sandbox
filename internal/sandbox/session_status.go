package sandbox

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"time"
)

const (
	guestFreeSpaceSchemaVersion = 1
	guestFreeSpaceTimeout       = 5 * time.Second
	maximumGuestFreeSpaceBytes  = 1024
)

type guestFreeSpaceStatus struct {
	SchemaVersion int    `json:"schemaVersion"`
	Volume        string `json:"volume"`
	FreeBytes     uint64 `json:"freeBytes"`
	TotalBytes    uint64 `json:"totalBytes"`
}

func enrichSessionStatus(dataDirectory string, active activeSession, status *SessionStatus) {
	status.StartedAtUTC = active.StartedAtUTC
	runDirectory := filepath.Join(dataDirectory, "runs", active.RunID)
	status.DiagnosticsPath = filepath.Join(runDirectory, "status")
	if operation, found, err := readSessionOperation(runDirectory); err != nil {
		status.Warnings = append(status.Warnings, "Operation diagnostics unavailable: "+err.Error())
	} else if found {
		if operation.RunID != active.RunID {
			status.Warnings = append(status.Warnings, "Operation diagnostics do not match the active run.")
		} else {
			status.Operation = &operation
		}
	}
	if workspaces, err := readSessionWorkspaces(runDirectory); err != nil {
		status.Warnings = append(status.Warnings, "Workspace diagnostics unavailable: "+err.Error())
	} else {
		status.Workspaces = workspaces
	}
	if timings, err := readSessionTimings(status.DiagnosticsPath); err != nil {
		status.Warnings = append(status.Warnings, "Provisioning timings unavailable: "+err.Error())
	} else {
		status.Timings = timings
	}
	if status.State == SessionReady && active.Tailscale {
		keys, err := readMobileSSHAuthorizedKeysInput(filepath.Join(runDirectory, "input"))
		if err != nil {
			status.Warnings = append(status.Warnings, "Mobile SSH access state unavailable: "+err.Error())
		} else if len(keys) > 0 {
			access, err := loadMobileAccess(dataDirectory, len(keys))
			if err != nil {
				status.Warnings = append(status.Warnings, "Mobile SSH access identity unavailable: "+err.Error())
			} else {
				status.MobileAccess = &access
			}
		}
	}
}

func inspectGuestFreeSpace(ctx context.Context, dataDirectory string, active activeSession) (GuestFreeSpace, error) {
	runDirectory := filepath.Join(dataDirectory, "runs", active.RunID)
	ready, found, err := readOptionalStatus[readyStatus](filepath.Join(runDirectory, "status", readyFileName))
	if err != nil {
		return GuestFreeSpace{}, fmt.Errorf("read ready identity: %w", err)
	}
	if !found {
		return GuestFreeSpace{}, errors.New("ready identity is missing")
	}
	if err := ready.validate(); err != nil {
		return GuestFreeSpace{}, fmt.Errorf("validate ready identity: %w", err)
	}

	sshDirectory := filepath.Join(runDirectory, ".ssh")
	knownHostsPath := filepath.Join(sshDirectory, "known_hosts")
	hostKeyAlias := "windows-sandbox-" + active.RunID
	expectedKnownHosts := hostKeyAlias + " " + ready.SSHHostKey + "\n"
	knownHosts, found, err := readBoundedRegularFile(knownHostsPath, maximumUserSSHConfigurationBytes)
	if err != nil {
		return GuestFreeSpace{}, fmt.Errorf("read run SSH host key: %w", err)
	}
	if !found || !bytes.Equal(knownHosts, []byte(expectedKnownHosts)) {
		return GuestFreeSpace{}, errors.New("run SSH host key does not match the ready Sandbox")
	}

	configPath := filepath.Join(sshDirectory, "config")
	privateKeyPath := filepath.Join(dataDirectory, "identity", "id_ed25519")
	expectedConfig := renderSSHConfig(connectionStatus(ready), privateKeyPath, knownHostsPath, hostKeyAlias)
	config, found, err := readBoundedRegularFile(configPath, maximumUserSSHConfigurationBytes)
	if err != nil {
		return GuestFreeSpace{}, fmt.Errorf("read run SSH configuration: %w", err)
	}
	if !found || !bytes.Equal(config, []byte(expectedConfig)) {
		return GuestFreeSpace{}, errors.New("run SSH configuration does not match the ready Sandbox")
	}

	queryContext, cancel := context.WithTimeout(ctx, guestFreeSpaceTimeout)
	defer cancel()
	output, err := runSSHPowerShell(queryContext, Connection{
		SSHConfigPath: configPath,
		SSHTarget:     sshTargetName,
	}, nil, guestFreeSpacePowerShell(), "inspect guest free space", maximumGuestFreeSpaceBytes)
	if err != nil {
		return GuestFreeSpace{}, err
	}
	return decodeGuestFreeSpace(output)
}

func guestFreeSpacePowerShell() string {
	return `$ErrorActionPreference = 'Stop'
$drive = [System.IO.DriveInfo]::new('C:\')
if (-not $drive.IsReady) { throw 'Guest C: logical volume is not ready.' }
$totalBytes = [Int64]$drive.TotalSize
$freeBytes = [Int64]$drive.TotalFreeSpace
if ($totalBytes -le 0 -or $freeBytes -lt 0 -or $freeBytes -gt $totalBytes) { throw 'Guest C: logical volume returned invalid free space.' }
[ordered]@{
    schemaVersion = 1
    volume = 'C:'
    freeBytes = $freeBytes
    totalBytes = $totalBytes
} | ConvertTo-Json -Compress`
}

func decodeGuestFreeSpace(data []byte) (GuestFreeSpace, error) {
	fields := []string{"schemaVersion", "volume", "freeBytes", "totalBytes"}
	if err := validateExactJSONObjectShape(data, "guest free space", fields); err != nil {
		return GuestFreeSpace{}, err
	}
	var status guestFreeSpaceStatus
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&status); err != nil {
		return GuestFreeSpace{}, fmt.Errorf("decode guest free space: %w", err)
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return GuestFreeSpace{}, fmt.Errorf("decode guest free space: %w", err)
	}
	if status.SchemaVersion != guestFreeSpaceSchemaVersion {
		return GuestFreeSpace{}, fmt.Errorf("guest free space schemaVersion = %d, want %d", status.SchemaVersion, guestFreeSpaceSchemaVersion)
	}
	if status.Volume != "C:" {
		return GuestFreeSpace{}, fmt.Errorf("guest free space volume = %q, want C:", status.Volume)
	}
	if status.TotalBytes == 0 {
		return GuestFreeSpace{}, errors.New("guest free space totalBytes must be positive")
	}
	if status.FreeBytes > status.TotalBytes {
		return GuestFreeSpace{}, errors.New("guest free space freeBytes exceeds totalBytes")
	}
	return GuestFreeSpace{
		Volume:     status.Volume,
		FreeBytes:  status.FreeBytes,
		TotalBytes: status.TotalBytes,
	}, nil
}

// interruptAbandonedActiveOperation runs immediately after lifecycle-lock
// acquisition. A live retained reprovision owns that lock, so a freely acquired
// lock proves that any nonterminal operation record for the active run was
// abandoned.
func interruptAbandonedActiveOperation(dataDirectory string) (SessionOperation, bool, error) {
	executable, err := windowsSandboxExecutable()
	if err != nil {
		return SessionOperation{}, false, err
	}
	return interruptAbandonedActiveOperationWithExecutable(dataDirectory, executable)
}

func interruptAbandonedActiveOperationWithExecutable(dataDirectory, executable string) (SessionOperation, bool, error) {
	active, found, err := loadActiveSession(dataDirectory, executable)
	if err != nil || !found {
		return SessionOperation{}, false, err
	}
	return interruptAbandonedRunOperation(dataDirectory, active.RunID)
}

func interruptAbandonedRunOperation(dataDirectory, runID string) (SessionOperation, bool, error) {
	runDirectory := filepath.Join(dataDirectory, "runs", runID)
	operation, found, err := readSessionOperation(runDirectory)
	if err != nil || !found || operation.State != operationStateRunning {
		return operation, false, err
	}
	if operation.RunID != runID {
		return SessionOperation{}, false, errors.New("running retained operation does not match the active run")
	}
	operation, err = interruptRunningSessionOperation(runDirectory, operation)
	if err != nil {
		return SessionOperation{}, false, err
	}
	return operation, true, nil
}

func inspectSessionDuringOperation(ctx context.Context, dataDirectory string, lockErr error) (SessionStatus, error) {
	executable, err := windowsSandboxExecutable()
	if err != nil {
		return SessionStatus{}, err
	}
	before, found, err := loadActiveSession(dataDirectory, executable)
	if err != nil {
		return SessionStatus{}, err
	}
	if !found {
		return SessionStatus{}, fmt.Errorf("lifecycle operation is busy and has no active session identity: %w", lockErr)
	}
	runDirectory := filepath.Join(dataDirectory, "runs", before.RunID)
	operationBefore, found, err := readSessionOperation(runDirectory)
	if err != nil {
		return SessionStatus{}, fmt.Errorf("lifecycle operation is busy and its operation state is invalid: %w", err)
	}
	if !found || operationBefore.RunID != before.RunID || operationBefore.State != operationStateRunning {
		return SessionStatus{}, fmt.Errorf("lifecycle operation is busy without a current retained reprovision: %w", lockErr)
	}
	status, err := inspectSessionAt(ctx, dataDirectory)
	if err != nil {
		return SessionStatus{}, err
	}
	after, found, err := loadActiveSession(dataDirectory, executable)
	if err != nil {
		return SessionStatus{}, err
	}
	if !found || after != before {
		return SessionStatus{}, errors.New("active Sandbox identity changed during status inspection")
	}
	operationAfter, found, err := readSessionOperation(runDirectory)
	if err != nil {
		return SessionStatus{}, err
	}
	if !found || operationAfter.ID != operationBefore.ID || operationAfter.RunID != before.RunID ||
		operationAfter.State != operationStateRunning {
		return SessionStatus{}, errors.New("retained operation changed terminal state during status inspection; retry `sandbox status`")
	}
	status.Operation = &operationAfter
	status.Warnings = append(status.Warnings, "Stale-state cleanup was deferred while retained reprovisioning is active.")
	status.NextAction = sessionNextAction(status)
	return status, nil
}

func sessionNextAction(status SessionStatus) string {
	switch status.State {
	case SessionStopped:
		return "Run `sandbox up` from a configured project."
	case SessionStarting:
		return "Wait for provisioning, then run `sandbox status` again."
	case SessionReady:
		if status.Operation != nil && status.Operation.State == operationStateRunning {
			return "Wait for retained reprovisioning to finish, then run `sandbox attach`."
		}
		return "Run `sandbox attach` to connect without reprovisioning."
	case SessionFailed:
		return "Inspect the diagnostics above, then run `sandbox down` before retrying `up`."
	case SessionStale:
		return "Run `sandbox status` again to retry bounded stale-state cleanup."
	case SessionUnmanaged:
		return "Close or otherwise manage the unrelated Windows Sandbox before running `sandbox up`."
	default:
		return "Run `sandbox status` again."
	}
}
