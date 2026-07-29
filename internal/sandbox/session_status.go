package sandbox

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
)

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
		return SessionStatus{}, errors.New("retained operation changed terminal state during status inspection; retry `herdr-sandbox status`")
	}
	status.Operation = &operationAfter
	status.Warnings = append(status.Warnings, "Stale-state cleanup was deferred while retained reprovisioning is active.")
	status.NextAction = sessionNextAction(status)
	return status, nil
}

func sessionNextAction(status SessionStatus) string {
	switch status.State {
	case SessionStopped:
		return "Run `herdr-sandbox up` from a configured project."
	case SessionStarting:
		return "Wait for provisioning, then run `herdr-sandbox status` again."
	case SessionReady:
		if status.Operation != nil && status.Operation.State == operationStateRunning {
			return "Wait for retained reprovisioning to finish, then run `herdr-sandbox attach`."
		}
		return "Run `herdr-sandbox attach` to connect without reprovisioning."
	case SessionFailed:
		return "Inspect the diagnostics above, then run `herdr-sandbox down` before retrying `up`."
	case SessionStale:
		return "Run `herdr-sandbox status` again to retry bounded stale-state cleanup."
	case SessionUnmanaged:
		return "Close or otherwise manage the unrelated Windows Sandbox before running `herdr-sandbox up`."
	default:
		return "Run `herdr-sandbox status` again."
	}
}
