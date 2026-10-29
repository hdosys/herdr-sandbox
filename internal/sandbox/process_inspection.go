package sandbox

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
)

//go:embed assets/inspect-processes.ps1
var processInspectionScript string

type sandboxProcessInspection struct {
	Processes []runningSandboxProcess `json:"processes"`
	Snapshot  *sandboxProcessSnapshot `json:"snapshot"`
}

// inspectSandboxProcesses obtains the census and optional exact identity in one
// PowerShell process. Cleanup still calls it on both sides of deletion planning.
func inspectSandboxProcesses(ctx context.Context, pid int, includeRunning bool) (sandboxProcessInspection, error) {
	powerShell, err := windowsPowerShellExecutable()
	if err != nil {
		return sandboxProcessInspection{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, sandboxProcessInspectionTimeout)
	defer cancel()
	script := fmt.Sprintf("$inspectPID = %d; $includeRunning = $%t\n", pid, includeRunning) + processInspectionScript
	command := hiddenCommandContext(ctx, powerShell, "-NoLogo", "-NoProfile", "-NonInteractive", "-WindowStyle", "Hidden", "-EncodedCommand", encodePowerShell(script))
	output, err := command.CombinedOutput()
	if err != nil {
		return sandboxProcessInspection{}, fmt.Errorf("inspect Windows Sandbox processes: %w: %s", err, boundedText(output))
	}
	return decodeSandboxProcessInspection(output, pid)
}

func decodeSandboxProcessInspection(output []byte, pid int) (sandboxProcessInspection, error) {
	fields := []string{"processes", "snapshot"}
	seen, err := decodeJSONObjectShape(output, "Windows Sandbox process inspection", fields, false)
	if err != nil {
		return sandboxProcessInspection{}, err
	}
	if !seen["processes"] || !seen["snapshot"] {
		return sandboxProcessInspection{}, errors.New("incomplete Windows Sandbox process inspection")
	}
	var inspection sandboxProcessInspection
	decoder := json.NewDecoder(bytes.NewReader(output))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&inspection); err != nil {
		return sandboxProcessInspection{}, fmt.Errorf("decode Windows Sandbox process inspection: %w", err)
	}
	if inspection.Processes == nil || len(inspection.Processes) > 8 {
		return sandboxProcessInspection{}, errors.New("invalid Windows Sandbox process census")
	}
	for _, process := range inspection.Processes {
		if (process.Name != "WindowsSandbox" && process.Name != "WindowsSandboxClient") || process.PID < 1 || process.ParentPID < 1 {
			return sandboxProcessInspection{}, fmt.Errorf("invalid Windows Sandbox process record: %+v", process)
		}
	}
	if snapshot := inspection.Snapshot; snapshot != nil {
		if pid < 1 || snapshot.PID != pid || snapshot.Name == "" || snapshot.ExecutablePath == "" || snapshot.StartedAtUTC == "" || snapshot.CommandLine == "" {
			return sandboxProcessInspection{}, fmt.Errorf("inspect Windows Sandbox process %d: incomplete or mismatched identity", pid)
		}
	}
	return inspection, nil
}

func inspectSandboxProcess(ctx context.Context, pid int) (sandboxProcessSnapshot, bool, error) {
	inspection, err := inspectSandboxProcesses(ctx, pid, false)
	if err != nil || inspection.Snapshot == nil {
		return sandboxProcessSnapshot{}, false, err
	}
	return *inspection.Snapshot, true, nil
}
