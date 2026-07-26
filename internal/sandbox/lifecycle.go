package sandbox

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	activeSessionFileName      = "active.json"
	activeSessionSchemaVersion = 2
	maximumActiveSessionBytes  = 64 * 1024

	SessionStopped   = "stopped"
	SessionUnmanaged = "unmanaged"
	SessionStarting  = "starting"
	SessionReady     = "ready"
	SessionFailed    = "failed"
	SessionStale     = "stale"
)

var runIDPattern = regexp.MustCompile(`^\d{8}-\d{6}-[0-9a-f]{8}$`)

type SessionStatus struct {
	State        string
	RunID        string
	PID          int
	Phase        string
	Message      string
	GuestIP      string
	HerdrVersion string
	Processes    []string
}

type DownResult struct {
	RunID          string
	AlreadyStopped bool
}

type CleanResult struct {
	RemovedRuns int
	ActiveRunID string
}

type inactiveRunCleanupPlan struct {
	RunsDirectory string
	RootIdentity  string
	Candidates    []string
}

type activeSession struct {
	SchemaVersion  int    `json:"schemaVersion"`
	RunID          string `json:"runID"`
	ConfigPath     string `json:"configPath"`
	Tailscale      bool   `json:"tailscale"`
	PID            int    `json:"pid"`
	ExecutablePath string `json:"executablePath"`
	StartedAtUTC   string `json:"startedAtUTC"`
	CommandLine    string `json:"commandLine"`
}

type sandboxProcessSnapshot struct {
	PID            int    `json:"pid"`
	Name           string `json:"name"`
	ExecutablePath string `json:"executablePath"`
	StartedAtUTC   string `json:"startedAtUTC"`
	CommandLine    string `json:"commandLine"`
}

func InspectSession(ctx context.Context) (SessionStatus, error) {
	dataDirectory, err := defaultDataDirectory()
	if err != nil {
		return SessionStatus{}, err
	}
	return inspectSessionAt(ctx, dataDirectory)
}

func Down(ctx context.Context) (DownResult, error) {
	dataDirectory, err := defaultDataDirectory()
	if err != nil {
		return DownResult{}, err
	}
	release, err := acquireLifecycleLock(ctx)
	if err != nil {
		return DownResult{}, err
	}
	result, downErr := downAt(ctx, dataDirectory)
	releaseErr := release()
	if downErr != nil {
		return DownResult{}, downErr
	}
	if releaseErr != nil {
		return DownResult{}, releaseErr
	}
	return result, nil
}

func Clean(ctx context.Context) (CleanResult, error) {
	dataDirectory, err := defaultDataDirectory()
	if err != nil {
		return CleanResult{}, err
	}
	release, err := acquireLifecycleLock(ctx)
	if err != nil {
		return CleanResult{}, err
	}
	result, cleanErr := cleanAt(ctx, dataDirectory)
	releaseErr := release()
	if cleanErr != nil {
		return result, cleanErr
	}
	if releaseErr != nil {
		return result, releaseErr
	}
	return result, nil
}

func inspectSessionAt(ctx context.Context, dataDirectory string) (SessionStatus, error) {
	executable, err := windowsSandboxExecutable()
	if err != nil {
		return SessionStatus{}, err
	}
	active, found, err := loadActiveSession(dataDirectory, executable)
	if err != nil {
		return SessionStatus{}, err
	}
	if !found {
		processes, err := runningSandboxProcesses(ctx)
		if err != nil {
			return SessionStatus{}, err
		}
		if len(processes) == 0 {
			return SessionStatus{State: SessionStopped}, nil
		}
		return SessionStatus{State: SessionUnmanaged, Processes: describeRunningSandboxProcesses(processes)}, nil
	}

	snapshot, running, err := inspectSandboxProcess(ctx, active.PID)
	if err != nil {
		return SessionStatus{}, err
	}
	status := SessionStatus{State: SessionStale, RunID: active.RunID, PID: active.PID}
	if !running {
		status.Message = "recorded Windows Sandbox process is no longer running"
		return status, nil
	}
	if err := active.matches(snapshot); err != nil {
		status.Message = err.Error()
		return status, nil
	}

	return classifyManagedSession(dataDirectory, active)
}

func classifyManagedSession(dataDirectory string, active activeSession) (SessionStatus, error) {
	status := SessionStatus{State: SessionStarting, RunID: active.RunID, PID: active.PID}
	statusDirectory := filepath.Join(dataDirectory, "runs", active.RunID, "status")
	if failure, ok, err := readOptionalStatus[failureStatus](filepath.Join(statusDirectory, failureFileName)); err != nil {
		return SessionStatus{}, fmt.Errorf("read active Sandbox failure status: %w", err)
	} else if ok {
		if err := failure.validate(); err != nil {
			return SessionStatus{}, fmt.Errorf("validate active Sandbox failure status: %w", err)
		}
		status.State = SessionFailed
		status.Phase = failure.Phase
		status.Message = failure.Message
		return status, nil
	}
	if ready, ok, err := readOptionalStatus[readyStatus](filepath.Join(statusDirectory, readyFileName)); err != nil {
		return SessionStatus{}, fmt.Errorf("read active Sandbox ready status: %w", err)
	} else if ok {
		if err := ready.validate(); err != nil {
			return SessionStatus{}, fmt.Errorf("validate active Sandbox ready status: %w", err)
		}
		status.State = SessionReady
		status.GuestIP = ready.IP
		status.HerdrVersion = ready.HerdrVersion
		return status, nil
	}
	if connectable, ok, err := readOptionalStatus[connectableStatus](filepath.Join(statusDirectory, connectableFileName)); err != nil {
		return SessionStatus{}, fmt.Errorf("read active Sandbox connectable status: %w", err)
	} else if ok {
		if err := connectable.validate(); err != nil {
			return SessionStatus{}, fmt.Errorf("validate active Sandbox connectable status: %w", err)
		}
		status.Phase = "connectable"
		status.Message = "SSH and Herdr server are ready; applying verified host configuration"
		return status, nil
	}
	if progress, ok, err := readOptionalStatus[progressStatus](filepath.Join(statusDirectory, progressFileName)); err != nil {
		return SessionStatus{}, fmt.Errorf("read active Sandbox progress status: %w", err)
	} else if ok {
		if err := progress.validate(); err != nil {
			return SessionStatus{}, fmt.Errorf("validate active Sandbox progress status: %w", err)
		}
		status.Phase = progress.Phase
		status.Message = progress.Message
	}
	return status, nil
}

func downAt(ctx context.Context, dataDirectory string) (DownResult, error) {
	executable, err := windowsSandboxExecutable()
	if err != nil {
		return DownResult{}, err
	}
	active, found, err := loadActiveSession(dataDirectory, executable)
	if err != nil {
		return DownResult{}, err
	}
	if !found {
		processes, err := runningSandboxProcesses(ctx)
		if err != nil {
			return DownResult{}, err
		}
		if len(processes) > 0 {
			return DownResult{}, fmt.Errorf("refusing to stop unmanaged Windows Sandbox process(es): %s", strings.Join(describeRunningSandboxProcesses(processes), ", "))
		}
		return DownResult{AlreadyStopped: true}, nil
	}
	if active.Tailscale {
		managed, err := classifyManagedSession(dataDirectory, active)
		if err != nil {
			return DownResult{}, err
		}
		var captureStatus connectionStatus
		capture := false
		switch managed.State {
		case SessionReady:
			ready, found, err := readOptionalStatus[readyStatus](filepath.Join(dataDirectory, "runs", active.RunID, "status", readyFileName))
			if err != nil {
				return DownResult{}, fmt.Errorf("read ready Sandbox status for Tailscale capture: %w", err)
			}
			if !found {
				return DownResult{}, errors.New("ready Sandbox status disappeared before Tailscale capture")
			}
			if err := ready.validate(); err != nil {
				return DownResult{}, fmt.Errorf("validate ready Sandbox status for Tailscale capture: %w", err)
			}
			captureStatus = connectionStatus(ready)
			capture = true
		case SessionFailed:
			hostPhase := ""
			handoff, handoffFound, err := readOptionalStatus[configurationHandoffStatus](filepath.Join(dataDirectory, "runs", active.RunID, "status", configurationHandoffFileName))
			if err != nil {
				return DownResult{}, fmt.Errorf("read failed Sandbox configuration handoff for Tailscale recovery: %w", err)
			}
			if handoffFound {
				if err := handoff.validate(); err != nil {
					return DownResult{}, fmt.Errorf("validate failed Sandbox configuration handoff for Tailscale recovery: %w", err)
				}
				if handoff.Outcome == configurationHandoffFailed {
					hostPhase = handoff.Phase
				}
			}
			if !tailscaleFailurePrecedesIdentity(hostPhase) {
				connectable, found, err := readOptionalStatus[connectableStatus](filepath.Join(dataDirectory, "runs", active.RunID, "status", connectableFileName))
				if err != nil {
					return DownResult{}, fmt.Errorf("read failed Sandbox connection status for Tailscale recovery: %w", err)
				}
				if found {
					if err := connectable.validate(); err != nil {
						return DownResult{}, fmt.Errorf("validate failed Sandbox connection status for Tailscale recovery: %w", err)
					}
					captureStatus = connectionStatus(connectable)
					capture = true
				}
			}
		case SessionStarting:
			_, connectable, err := readOptionalStatus[connectableStatus](filepath.Join(dataDirectory, "runs", active.RunID, "status", connectableFileName))
			if err != nil {
				return DownResult{}, fmt.Errorf("read starting Sandbox connection status before down: %w", err)
			}
			if connectable {
				return DownResult{}, errors.New("refusing to close an opted-in Sandbox while Tailscale identity setup is still running; wait for `up` to finish or fail")
			}
		}
		if capture {
			if err := captureTailscaleBeforeDown(ctx, dataDirectory, active, captureStatus); err != nil {
				return DownResult{}, fmt.Errorf("preserve stable Tailscale identity before down; no close request was sent: %w", err)
			}
		}
	}

	stopped, err := stopOwnedSandboxProcess(ctx, active)
	if err != nil {
		return DownResult{}, err
	}
	if !stopped {
		processes, err := runningSandboxProcesses(ctx)
		if err != nil {
			return DownResult{}, err
		}
		if len(processes) > 0 {
			return DownResult{}, fmt.Errorf("recorded session ended; refusing to stop unmanaged Windows Sandbox process(es): %s", strings.Join(describeRunningSandboxProcesses(processes), ", "))
		}
		if err := removeActiveSession(dataDirectory); err != nil {
			return DownResult{}, err
		}
		return DownResult{RunID: active.RunID, AlreadyStopped: true}, nil
	}
	if err := removeActiveSession(dataDirectory); err != nil {
		return DownResult{}, err
	}
	return DownResult{RunID: active.RunID}, nil
}

func tailscaleFailurePrecedesIdentity(phase string) bool {
	switch phase {
	case "guest-identity", "ssh-material", "ssh-verification", "herdr-verification", "tailscale-preflight", "tailscale-not-enrolled":
		return true
	default:
		return false
	}
}

func captureTailscaleBeforeDown(ctx context.Context, dataDirectory string, active activeSession, status connectionStatus) error {
	snapshot, running, err := inspectSandboxProcess(ctx, active.PID)
	if err != nil {
		return err
	}
	if !running {
		return errors.New("recorded Windows Sandbox process is no longer running")
	}
	if err := active.matches(snapshot); err != nil {
		return fmt.Errorf("refusing Tailscale capture from an unowned Sandbox process: %w", err)
	}
	runDirectory := filepath.Join(dataDirectory, "runs", active.RunID)
	statusDirectory := filepath.Join(runDirectory, "status")
	connection, err := writeRunConnection(runPlan{
		ID:              active.RunID,
		RunDirectory:    runDirectory,
		StatusDirectory: statusDirectory,
		PrivateKeyPath:  filepath.Join(dataDirectory, "identity", "id_ed25519"),
	}, connectableStatus(status), "")
	if err != nil {
		return err
	}
	captureContext, cancel := context.WithTimeout(ctx, tailscaleIdentityTimeout)
	defer cancel()
	return recoverAndStoreTailscale(captureContext, connection, dataDirectory)
}

func cleanAt(ctx context.Context, dataDirectory string) (CleanResult, error) {
	executable, err := windowsSandboxExecutable()
	if err != nil {
		return CleanResult{}, err
	}
	activeRunID, err := cleanupProtectedRunID(ctx, dataDirectory, executable)
	if err != nil {
		return CleanResult{}, err
	}
	plan, err := planInactiveRunDirectories(dataDirectory, activeRunID)
	if err != nil {
		return CleanResult{ActiveRunID: activeRunID}, err
	}
	revalidatedRunID, err := cleanupProtectedRunID(ctx, dataDirectory, executable)
	if err != nil {
		return CleanResult{ActiveRunID: activeRunID}, err
	}
	if revalidatedRunID != activeRunID {
		return CleanResult{ActiveRunID: activeRunID}, errors.New("refusing to clean because the active Sandbox identity changed during preflight")
	}
	removed, err := removeInactiveRunDirectories(plan)
	return CleanResult{RemovedRuns: removed, ActiveRunID: activeRunID}, err
}

func cleanupProtectedRunID(ctx context.Context, dataDirectory, executable string) (string, error) {
	active, found, err := loadActiveSession(dataDirectory, executable)
	if err != nil {
		return "", err
	}
	processes, err := runningSandboxProcesses(ctx)
	if err != nil {
		return "", err
	}
	if !found {
		if len(processes) > 0 {
			return "", fmt.Errorf("refusing to clean while unmanaged Windows Sandbox process(es) are running: %s", strings.Join(describeRunningSandboxProcesses(processes), ", "))
		}
		return "", nil
	}
	snapshot, running, err := inspectSandboxProcess(ctx, active.PID)
	if err != nil {
		return "", err
	}
	if err := validateCleanupProcessOwnership(active, snapshot, running, processes); err != nil {
		return "", err
	}
	return active.RunID, nil
}

func validateCleanupProcessOwnership(active activeSession, snapshot sandboxProcessSnapshot, running bool, processes []runningSandboxProcess) error {
	if !running {
		if len(processes) > 0 {
			return fmt.Errorf("refusing to clean while unmanaged Windows Sandbox process(es) are running: %s", strings.Join(describeRunningSandboxProcesses(processes), ", "))
		}
		return nil
	}
	if err := active.matches(snapshot); err != nil {
		return fmt.Errorf("refusing to clean while the active Sandbox identity is unowned or changed: %w", err)
	}
	launcherCount := 0
	clientCount := 0
	for _, process := range processes {
		switch process.Name {
		case "WindowsSandbox":
			if process.PID != active.PID {
				return fmt.Errorf("refusing to clean while an unmanaged Windows Sandbox is running: %s", strings.Join(describeRunningSandboxProcesses(processes), ", "))
			}
			launcherCount++
		case "WindowsSandboxClient":
			if process.ParentPID != active.PID {
				return fmt.Errorf("refusing to clean while an unmanaged Windows Sandbox client is running: %s", strings.Join(describeRunningSandboxProcesses(processes), ", "))
			}
			clientCount++
		}
	}
	if launcherCount != 1 || clientCount > 1 {
		return fmt.Errorf("refusing to clean because the owned Windows Sandbox process tree could not be revalidated: %s", strings.Join(describeRunningSandboxProcesses(processes), ", "))
	}
	return nil
}

func cleanInactiveRunDirectories(dataDirectory, activeRunID string) (int, error) {
	plan, err := planInactiveRunDirectories(dataDirectory, activeRunID)
	if err != nil {
		return 0, err
	}
	return removeInactiveRunDirectories(plan)
}

func planInactiveRunDirectories(dataDirectory, activeRunID string) (inactiveRunCleanupPlan, error) {
	if !filepath.IsAbs(dataDirectory) {
		return inactiveRunCleanupPlan{}, fmt.Errorf("run data directory is not absolute: %q", dataDirectory)
	}
	if activeRunID != "" && !runIDPattern.MatchString(activeRunID) {
		return inactiveRunCleanupPlan{}, fmt.Errorf("active run ID is invalid: %q", activeRunID)
	}

	runsDirectory := filepath.Join(filepath.Clean(dataDirectory), "runs")
	rootInfo, err := os.Lstat(runsDirectory)
	if errors.Is(err, os.ErrNotExist) {
		return inactiveRunCleanupPlan{RunsDirectory: runsDirectory}, nil
	}
	if err != nil {
		return inactiveRunCleanupPlan{}, fmt.Errorf("inspect run directory root: %w", err)
	}
	if err := rejectMappedPathReparsePoints(runsDirectory); err != nil {
		return inactiveRunCleanupPlan{}, fmt.Errorf("refusing to clean unsafe run directory root: %w", err)
	}
	rootReparse, err := fileInfoIsReparsePoint(rootInfo)
	if err != nil {
		return inactiveRunCleanupPlan{}, fmt.Errorf("inspect run directory root reparse state: %w", err)
	}
	if rootReparse {
		return inactiveRunCleanupPlan{}, errors.New("refusing to clean because the run directory root is a reparse point")
	}
	if !rootInfo.IsDir() {
		return inactiveRunCleanupPlan{}, errors.New("refusing to clean because the run directory root is not a directory")
	}
	rootIdentity, err := physicalMappedDirectory(runsDirectory)
	if err != nil {
		return inactiveRunCleanupPlan{}, fmt.Errorf("resolve run directory root identity: %w", err)
	}

	entries, err := os.ReadDir(runsDirectory)
	if err != nil {
		return inactiveRunCleanupPlan{}, fmt.Errorf("read run directory root: %w", err)
	}
	candidates := make([]string, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if name == activeRunID || !runIDPattern.MatchString(name) {
			continue
		}
		path := filepath.Join(runsDirectory, name)
		if err := validateCleanableRunDirectory(path, name); err != nil {
			return inactiveRunCleanupPlan{}, err
		}
		candidates = append(candidates, path)
	}
	return inactiveRunCleanupPlan{RunsDirectory: runsDirectory, RootIdentity: rootIdentity, Candidates: candidates}, nil
}

func removeInactiveRunDirectories(plan inactiveRunCleanupPlan) (int, error) {
	removed := 0
	for _, path := range plan.Candidates {
		if err := validateRunCleanupRoot(plan); err != nil {
			return removed, err
		}
		name := filepath.Base(path)
		if filepath.Dir(path) != plan.RunsDirectory || !runIDPattern.MatchString(name) {
			return removed, fmt.Errorf("refusing to clean invalid planned run path: %s", path)
		}
		if err := validateCleanableRunDirectory(path, name); err != nil {
			return removed, err
		}
		if err := os.RemoveAll(path); err != nil {
			return removed, fmt.Errorf("remove inactive run %s: %w", name, err)
		}
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			if err == nil {
				return removed, fmt.Errorf("remove inactive run %s: path still exists", name)
			}
			return removed, fmt.Errorf("verify inactive run %s removal: %w", name, err)
		}
		removed++
		if err := validateRunCleanupRoot(plan); err != nil {
			return removed, err
		}
	}
	return removed, nil
}

func validateRunCleanupRoot(plan inactiveRunCleanupPlan) error {
	if len(plan.Candidates) == 0 && plan.RootIdentity == "" {
		return nil
	}
	if err := rejectMappedPathReparsePoints(plan.RunsDirectory); err != nil {
		return fmt.Errorf("run directory root changed during cleanup: %w", err)
	}
	identity, err := physicalMappedDirectory(plan.RunsDirectory)
	if err != nil {
		return fmt.Errorf("resolve run directory root during cleanup: %w", err)
	}
	if !strings.EqualFold(identity, plan.RootIdentity) {
		return errors.New("run directory root identity changed during cleanup")
	}
	return nil
}

func validateCleanableRunDirectory(path, runID string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("inspect inactive run %s: %w", runID, err)
	}
	reparse, err := fileInfoIsReparsePoint(info)
	if err != nil {
		return fmt.Errorf("inspect inactive run %s reparse state: %w", runID, err)
	}
	if reparse {
		return fmt.Errorf("refusing to clean inactive run %s because it is a reparse point", runID)
	}
	if !info.IsDir() {
		return fmt.Errorf("refusing to clean inactive run %s because it is not a directory", runID)
	}

	err = filepath.WalkDir(path, func(current string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		entryInfo, infoErr := entry.Info()
		if infoErr != nil {
			return infoErr
		}
		entryReparse, reparseErr := fileInfoIsReparsePoint(entryInfo)
		if reparseErr != nil {
			return reparseErr
		}
		if entryReparse {
			relative, relativeErr := filepath.Rel(path, current)
			if relativeErr != nil {
				return relativeErr
			}
			return fmt.Errorf("contains reparse point %s", relative)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("refusing to clean inactive run %s: %w", runID, err)
	}
	return nil
}

func recordActiveSession(ctx context.Context, plan runPlan, pid int) error {
	deadline := time.Now().Add(5 * time.Second)
	var snapshot sandboxProcessSnapshot
	for {
		current, found, err := inspectSandboxProcess(ctx, pid)
		if err != nil {
			return err
		}
		if found {
			snapshot = current
			break
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("record active Sandbox process %d: process identity was unavailable", pid)
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("record active Sandbox process %d: %w", pid, ctx.Err())
		case <-time.After(100 * time.Millisecond):
		}
	}
	active := activeSession{
		SchemaVersion:  activeSessionSchemaVersion,
		RunID:          plan.ID,
		ConfigPath:     plan.ConfigPath,
		Tailscale:      plan.Tailscale,
		PID:            snapshot.PID,
		ExecutablePath: snapshot.ExecutablePath,
		StartedAtUTC:   snapshot.StartedAtUTC,
		CommandLine:    snapshot.CommandLine,
	}
	if err := active.validate(plan.DataDirectory, plan.SandboxExecutable); err != nil {
		return fmt.Errorf("validate active Sandbox identity: %w", err)
	}
	data, err := json.MarshalIndent(active, "", "  ")
	if err != nil {
		return fmt.Errorf("encode active Sandbox identity: %w", err)
	}
	data = append(data, '\n')
	if err := writeFileAtomically(filepath.Join(plan.DataDirectory, activeSessionFileName), data, 0o600); err != nil {
		return fmt.Errorf("publish active Sandbox identity: %w", err)
	}
	return nil
}

func loadActiveSession(dataDirectory, sandboxExecutable string) (activeSession, bool, error) {
	path := filepath.Join(dataDirectory, activeSessionFileName)
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return activeSession{}, false, nil
	}
	if err != nil {
		return activeSession{}, false, fmt.Errorf("inspect active Sandbox identity: %w", err)
	}
	if !info.Mode().IsRegular() || info.Size() > maximumActiveSessionBytes {
		return activeSession{}, false, fmt.Errorf("active Sandbox identity must be a regular file no larger than %d bytes", maximumActiveSessionBytes)
	}
	reparse, err := fileInfoIsReparsePoint(info)
	if err != nil {
		return activeSession{}, false, fmt.Errorf("inspect active Sandbox identity reparse state: %w", err)
	}
	if reparse {
		return activeSession{}, false, errors.New("active Sandbox identity must not be a reparse point")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return activeSession{}, false, fmt.Errorf("read active Sandbox identity: %w", err)
	}
	if err := validateActiveSessionShape(data); err != nil {
		return activeSession{}, false, fmt.Errorf("decode active Sandbox identity: %w", err)
	}
	var active activeSession
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&active); err != nil {
		return activeSession{}, false, fmt.Errorf("decode active Sandbox identity: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return activeSession{}, false, errors.New("active Sandbox identity contains trailing JSON data")
	}
	if err := active.validate(dataDirectory, sandboxExecutable); err != nil {
		return activeSession{}, false, fmt.Errorf("validate active Sandbox identity: %w", err)
	}
	return active, true, nil
}

func validateActiveSessionShape(data []byte) error {
	if err := validateExactJSONObjectShape(data, "identity", []string{
		"schemaVersion",
		"runID",
		"configPath",
		"tailscale",
		"pid",
		"executablePath",
		"startedAtUTC",
		"commandLine",
	}); err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	tailscale := string(bytes.TrimSpace(fields["tailscale"]))
	if tailscale != "true" && tailscale != "false" {
		return errors.New("identity field \"tailscale\" must be a JSON boolean")
	}
	return nil
}

func removeActiveSession(dataDirectory string) error {
	if err := os.Remove(filepath.Join(dataDirectory, activeSessionFileName)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove active Sandbox identity: %w", err)
	}
	return nil
}

func (active activeSession) validate(dataDirectory, sandboxExecutable string) error {
	if active.SchemaVersion != activeSessionSchemaVersion {
		return fmt.Errorf("schemaVersion = %d, want %d", active.SchemaVersion, activeSessionSchemaVersion)
	}
	if !runIDPattern.MatchString(active.RunID) {
		return fmt.Errorf("runID is invalid: %q", active.RunID)
	}
	expectedConfig := filepath.Join(filepath.Clean(dataDirectory), "runs", active.RunID, applicationName+".wsb")
	if !strings.EqualFold(filepath.Clean(active.ConfigPath), expectedConfig) {
		return fmt.Errorf("configPath = %q, want %q", active.ConfigPath, expectedConfig)
	}
	if active.PID < 1 {
		return fmt.Errorf("pid = %d, want a positive value", active.PID)
	}
	if !strings.EqualFold(filepath.Clean(active.ExecutablePath), filepath.Clean(sandboxExecutable)) {
		return fmt.Errorf("executablePath = %q, want %q", active.ExecutablePath, sandboxExecutable)
	}
	startedAt, err := time.Parse(time.RFC3339Nano, active.StartedAtUTC)
	if err != nil || startedAt.Location() != time.UTC {
		return fmt.Errorf("startedAtUTC is invalid: %q", active.StartedAtUTC)
	}
	if len(active.CommandLine) == 0 || len(active.CommandLine) > 4096 || strings.ContainsAny(active.CommandLine, "\r\n") {
		return errors.New("commandLine is empty, multiline, or too large")
	}
	if active.CommandLine != expectedWindowsSandboxCommandLine(active.ExecutablePath, expectedConfig) {
		return errors.New("commandLine is not the exact Windows Sandbox launch command")
	}
	return nil
}

func (active activeSession) matches(snapshot sandboxProcessSnapshot) error {
	if snapshot.PID != active.PID || !strings.EqualFold(snapshot.Name, "WindowsSandbox.exe") ||
		!strings.EqualFold(snapshot.ExecutablePath, active.ExecutablePath) || snapshot.StartedAtUTC != active.StartedAtUTC ||
		snapshot.CommandLine != active.CommandLine {
		return errors.New("recorded Windows Sandbox process identity changed")
	}
	return nil
}

func inspectSandboxProcess(ctx context.Context, pid int) (sandboxProcessSnapshot, bool, error) {
	powerShell, err := windowsPowerShellExecutable()
	if err != nil {
		return sandboxProcessSnapshot{}, false, err
	}
	script := fmt.Sprintf(`$ProgressPreference = 'SilentlyContinue'
$item = Get-CimInstance Win32_Process -Filter 'ProcessId = %d' -ErrorAction Stop
if ($null -eq $item) { exit 3 }
$process = Get-Process -Id %d -ErrorAction Stop
[ordered]@{
    pid = [int]$item.ProcessId
    name = [string]$item.Name
    executablePath = [string]$item.ExecutablePath
    startedAtUTC = $process.StartTime.ToUniversalTime().ToString('O')
    commandLine = [string]$item.CommandLine
} | ConvertTo-Json -Compress`, pid, pid)
	command := hiddenCommandContext(ctx, powerShell, "-NoLogo", "-NoProfile", "-NonInteractive", "-EncodedCommand", encodePowerShell(script))
	output, err := command.CombinedOutput()
	if err != nil {
		var exitError *exec.ExitError
		if errors.As(err, &exitError) && exitError.ExitCode() == 3 {
			return sandboxProcessSnapshot{}, false, nil
		}
		return sandboxProcessSnapshot{}, false, fmt.Errorf("inspect Windows Sandbox process %d: %w: %s", pid, err, boundedText(output))
	}
	var snapshot sandboxProcessSnapshot
	decoder := json.NewDecoder(bytes.NewReader(output))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&snapshot); err != nil {
		return sandboxProcessSnapshot{}, false, fmt.Errorf("decode Windows Sandbox process %d: %w", pid, err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return sandboxProcessSnapshot{}, false, fmt.Errorf("decode Windows Sandbox process %d: trailing JSON data", pid)
	}
	if snapshot.PID < 1 || snapshot.Name == "" || snapshot.ExecutablePath == "" || snapshot.StartedAtUTC == "" || snapshot.CommandLine == "" {
		return sandboxProcessSnapshot{}, false, fmt.Errorf("inspect Windows Sandbox process %d: incomplete identity", pid)
	}
	return snapshot, true, nil
}

func stopOwnedSandboxProcess(ctx context.Context, active activeSession) (bool, error) {
	powerShell, err := windowsPowerShellExecutable()
	if err != nil {
		return false, err
	}
	script := `$ProgressPreference = 'SilentlyContinue'
$item = Get-CimInstance Win32_Process -Filter ("ProcessId = " + $env:HERDR_SANDBOX_EXPECTED_PID) -ErrorAction Stop
if ($null -eq $item) { exit 3 }
	$process = Get-Process -Id ([int]$env:HERDR_SANDBOX_EXPECTED_PID) -ErrorAction Stop
$handle = $process.Handle
$startedAtUTC = $process.StartTime.ToUniversalTime().ToString('O')
if ([string]$item.Name -cne 'WindowsSandbox.exe' -or
    [string]$item.ExecutablePath -cne $env:HERDR_SANDBOX_EXPECTED_EXECUTABLE -or
    $startedAtUTC -cne $env:HERDR_SANDBOX_EXPECTED_STARTED -or
    [string]$item.CommandLine -cne $env:HERDR_SANDBOX_EXPECTED_COMMAND_LINE) {
    exit 12
}
$clientItem = $null
$deadline = [DateTime]::UtcNow.AddSeconds(30)
do {
    if ($process.HasExited) { exit 3 }
    $children = @(Get-CimInstance Win32_Process -Filter "Name = 'WindowsSandboxClient.exe'" -ErrorAction Stop |
        Where-Object { [int]$_.ParentProcessId -eq [int]$env:HERDR_SANDBOX_EXPECTED_PID })
    if ($children.Count -gt 1) { exit 15 }
    if ($children.Count -eq 1) {
        $clientItem = $children[0]
        break
    }
    Start-Sleep -Milliseconds 200
} while ([DateTime]::UtcNow -lt $deadline)
if ($null -eq $clientItem) { exit 15 }
$client = Get-Process -Id ([int]$clientItem.ProcessId) -ErrorAction Stop
$clientHandle = $client.Handle
$verifiedClient = Get-CimInstance Win32_Process -Filter ("ProcessId = " + [string]$clientItem.ProcessId) -ErrorAction Stop
if ($null -eq $verifiedClient -or [string]$verifiedClient.Name -cne 'WindowsSandboxClient.exe' -or
    [int]$verifiedClient.ParentProcessId -ne [int]$env:HERDR_SANDBOX_EXPECTED_PID) {
    exit 15
}
$windowDeadline = [DateTime]::UtcNow.AddSeconds(10)
do {
    $client.Refresh()
    if ($client.MainWindowHandle -ne [IntPtr]::Zero) { break }
    if ($client.HasExited) { exit 3 }
    Start-Sleep -Milliseconds 100
} while ([DateTime]::UtcNow -lt $windowDeadline)
if ($client.MainWindowHandle -eq [IntPtr]::Zero -or -not $client.CloseMainWindow()) { exit 15 }
if (-not $client.WaitForExit(60000)) { exit 16 }
if (-not $process.WaitForExit(30000)) { exit 17 }
$client.Dispose()
$process.Dispose()
Write-Output 'HERDR_SANDBOX_STOPPED'`
	command := hiddenCommandContext(ctx, powerShell, "-NoLogo", "-NoProfile", "-NonInteractive", "-EncodedCommand", encodePowerShell(script))
	command.Env = append(childProcessEnvironment(os.Environ()),
		"HERDR_SANDBOX_EXPECTED_PID="+strconv.Itoa(active.PID),
		"HERDR_SANDBOX_EXPECTED_EXECUTABLE="+active.ExecutablePath,
		"HERDR_SANDBOX_EXPECTED_STARTED="+active.StartedAtUTC,
		"HERDR_SANDBOX_EXPECTED_COMMAND_LINE="+active.CommandLine,
	)
	output, err := command.CombinedOutput()
	if err != nil {
		var exitError *exec.ExitError
		if errors.As(err, &exitError) {
			switch exitError.ExitCode() {
			case 3:
				return false, nil
			case 12:
				return false, fmt.Errorf("recorded Windows Sandbox process identity changed; refusing to stop PID %d", active.PID)
			case 15:
				return false, errors.New("owned Windows Sandbox client did not expose one closable main window; it was not force-terminated")
			case 16:
				return false, errors.New("owned Windows Sandbox client refused to exit within 60 seconds; it was not force-terminated")
			case 17:
				return false, errors.New("owned Windows Sandbox launcher did not exit after its client closed; it was not force-terminated")
			}
		}
		return false, fmt.Errorf("stop owned Windows Sandbox: %w: %s", err, boundedText(output))
	}
	if strings.TrimSpace(string(output)) != "HERDR_SANDBOX_STOPPED" {
		return false, fmt.Errorf("stop owned Windows Sandbox: completion marker missing from %q", boundedText(output))
	}
	return true, nil
}

func describeRunningSandboxProcesses(processes []runningSandboxProcess) []string {
	descriptions := make([]string, 0, len(processes))
	for _, process := range processes {
		descriptions = append(descriptions, fmt.Sprintf("%s:%d", process.Name, process.PID))
	}
	return descriptions
}
