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
	"regexp"
	"strings"
	"time"
)

const (
	operationFileName          = "operation.json"
	operationSchemaVersion     = 1
	maximumOperationFileBytes  = 8 * 1024
	maximumOperationMessageLen = 512

	operationKindReprovision = "reprovision"

	operationStateRunning     = "running"
	operationStateSucceeded   = "succeeded"
	operationStateFailed      = "failed"
	operationStateCancelled   = "cancelled"
	operationStateTimedOut    = "timed-out"
	operationStateInterrupted = "interrupted"
)

var operationPhasePattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,63}$`)

// SessionOperation reports the current or latest host-owned operation without
// conflating it with the health of the retained guest.
type SessionOperation struct {
	SchemaVersion  int    `json:"schemaVersion"`
	ID             string `json:"operationID"`
	RunID          string `json:"runID"`
	Kind           string `json:"kind"`
	State          string `json:"state"`
	Phase          string `json:"phase"`
	Message        string `json:"message"`
	StartedAtUTC   string `json:"startedAtUTC"`
	UpdatedAtUTC   string `json:"updatedAtUTC"`
	CompletedAtUTC string `json:"completedAtUTC"`
}

func startSessionOperation(runDirectory, runID, kind, phase, message string) (SessionOperation, error) {
	if existing, found, err := readSessionOperation(runDirectory); err != nil {
		return SessionOperation{}, fmt.Errorf("inspect previous %s operation: %w", kind, err)
	} else if found && existing.State == operationStateRunning {
		return SessionOperation{}, fmt.Errorf("previous %s operation %s is still marked running", existing.Kind, existing.ID)
	}
	id, err := newRunID()
	if err != nil {
		return SessionOperation{}, err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	operation := SessionOperation{
		SchemaVersion: operationSchemaVersion,
		ID:            id,
		RunID:         runID,
		Kind:          kind,
		State:         operationStateRunning,
		Phase:         phase,
		Message:       sanitizeOperationMessage(message),
		StartedAtUTC:  now,
		UpdatedAtUTC:  now,
	}
	if err := writeSessionOperation(runDirectory, operation); err != nil {
		return SessionOperation{}, fmt.Errorf("start %s operation: %w", kind, err)
	}
	return operation, nil
}

func updateSessionOperation(runDirectory string, operation SessionOperation, phase, message string) (SessionOperation, error) {
	if operation.State != operationStateRunning {
		return SessionOperation{}, fmt.Errorf("update terminal operation %s in state %s", operation.ID, operation.State)
	}
	operation.Phase = phase
	operation.Message = sanitizeOperationMessage(message)
	operation.UpdatedAtUTC = nextOperationTimestamp(operation.UpdatedAtUTC)
	if err := writeSessionOperation(runDirectory, operation); err != nil {
		return SessionOperation{}, fmt.Errorf("update %s operation: %w", operation.Kind, err)
	}
	return operation, nil
}

func finishSessionOperation(runDirectory string, operation SessionOperation, state, phase, message string) (SessionOperation, error) {
	if operation.State != operationStateRunning {
		return SessionOperation{}, fmt.Errorf("finish terminal operation %s in state %s", operation.ID, operation.State)
	}
	operation.State = state
	operation.Phase = phase
	operation.Message = sanitizeOperationMessage(message)
	operation.UpdatedAtUTC = nextOperationTimestamp(operation.UpdatedAtUTC)
	operation.CompletedAtUTC = operation.UpdatedAtUTC
	if err := writeSessionOperation(runDirectory, operation); err != nil {
		return SessionOperation{}, fmt.Errorf("finish %s operation: %w", operation.Kind, err)
	}
	return operation, nil
}

func interruptRunningSessionOperation(runDirectory string, operation SessionOperation) (SessionOperation, error) {
	return finishSessionOperation(runDirectory, operation, operationStateInterrupted,
		"interrupted", "The previous retained reprovision ended without publishing a terminal result.")
}

func operationFailureState(ctx context.Context, operationErr error) string {
	if errors.Is(operationErr, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return operationStateTimedOut
	}
	if errors.Is(operationErr, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
		return operationStateCancelled
	}
	return operationStateFailed
}

func writeSessionOperation(runDirectory string, operation SessionOperation) error {
	if !filepath.IsAbs(runDirectory) {
		return fmt.Errorf("operation run directory is not absolute: %q", runDirectory)
	}
	runDirectory = filepath.Clean(runDirectory)
	if err := rejectMappedPathReparsePoints(runDirectory); err != nil {
		return fmt.Errorf("operation run directory is unsafe: %w", err)
	}
	if info, err := os.Lstat(runDirectory); err != nil {
		return fmt.Errorf("inspect operation run directory: %w", err)
	} else if !info.IsDir() {
		return fmt.Errorf("operation run path is not a directory: %s", runDirectory)
	}
	if err := operation.validate(); err != nil {
		return fmt.Errorf("validate operation: %w", err)
	}
	data, err := json.Marshal(operation)
	if err != nil {
		return fmt.Errorf("encode operation: %w", err)
	}
	if len(data) > maximumOperationFileBytes {
		return fmt.Errorf("encoded operation exceeds %d bytes", maximumOperationFileBytes)
	}
	path := filepath.Join(runDirectory, operationFileName)
	if err := writeFileAtomically(path, data, 0o600); err != nil {
		return fmt.Errorf("publish operation: %w", err)
	}
	verified, found, err := readSessionOperation(runDirectory)
	if err != nil {
		return fmt.Errorf("read back operation: %w", err)
	}
	if !found || verified != operation {
		return errors.New("operation read-back mismatch")
	}
	return nil
}

func readSessionOperation(runDirectory string) (SessionOperation, bool, error) {
	var operation SessionOperation
	if !filepath.IsAbs(runDirectory) {
		return operation, false, fmt.Errorf("operation run directory is not absolute: %q", runDirectory)
	}
	runDirectory = filepath.Clean(runDirectory)
	if err := rejectMappedPathReparsePoints(runDirectory); err != nil {
		return operation, false, fmt.Errorf("operation run directory is unsafe: %w", err)
	}
	path := filepath.Join(runDirectory, operationFileName)
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return operation, false, nil
	}
	if err != nil {
		return operation, false, fmt.Errorf("inspect operation: %w", err)
	}
	reparse, err := fileInfoIsReparsePoint(info)
	if err != nil {
		return operation, false, fmt.Errorf("inspect operation reparse state: %w", err)
	}
	if reparse || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > maximumOperationFileBytes {
		return operation, false, errors.New("operation is not one bounded regular non-reparse file")
	}
	file, err := os.Open(path)
	if err != nil {
		return operation, false, fmt.Errorf("open operation: %w", err)
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maximumOperationFileBytes+1))
	if err != nil {
		return operation, false, fmt.Errorf("read operation: %w", err)
	}
	if len(data) > maximumOperationFileBytes {
		return operation, false, fmt.Errorf("operation exceeds %d bytes", maximumOperationFileBytes)
	}
	fields := []string{
		"schemaVersion", "operationID", "runID", "kind", "state", "phase", "message",
		"startedAtUTC", "updatedAtUTC", "completedAtUTC",
	}
	if err := validateExactJSONObjectShape(data, "operation", fields); err != nil {
		return operation, false, fmt.Errorf("decode operation: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&operation); err != nil {
		return operation, false, fmt.Errorf("decode operation: %w", err)
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return operation, false, fmt.Errorf("decode operation: %w", err)
	}
	if err := operation.validate(); err != nil {
		return operation, false, fmt.Errorf("validate operation: %w", err)
	}
	return operation, true, nil
}

func (operation SessionOperation) validate() error {
	if operation.SchemaVersion != operationSchemaVersion {
		return fmt.Errorf("schemaVersion = %d, want %d", operation.SchemaVersion, operationSchemaVersion)
	}
	if !runIDPattern.MatchString(operation.ID) || !runIDPattern.MatchString(operation.RunID) {
		return errors.New("operationID or runID is invalid")
	}
	if operation.Kind != operationKindReprovision {
		return fmt.Errorf("operation kind = %q", operation.Kind)
	}
	switch operation.State {
	case operationStateRunning, operationStateSucceeded, operationStateFailed,
		operationStateCancelled, operationStateTimedOut, operationStateInterrupted:
	default:
		return fmt.Errorf("operation state = %q", operation.State)
	}
	if !operationPhasePattern.MatchString(operation.Phase) {
		return fmt.Errorf("operation phase = %q", operation.Phase)
	}
	if operation.Message != sanitizeOperationMessage(operation.Message) || operation.Message == "" {
		return errors.New("operation message is empty, unbounded, or multiline")
	}
	started, err := time.Parse(time.RFC3339Nano, operation.StartedAtUTC)
	if err != nil {
		return fmt.Errorf("parse startedAtUTC: %w", err)
	}
	updated, err := time.Parse(time.RFC3339Nano, operation.UpdatedAtUTC)
	if err != nil {
		return fmt.Errorf("parse updatedAtUTC: %w", err)
	}
	if updated.Before(started) {
		return errors.New("updatedAtUTC precedes startedAtUTC")
	}
	if operation.State == operationStateRunning {
		if operation.CompletedAtUTC != "" {
			return errors.New("running operation has completedAtUTC")
		}
		return nil
	}
	completed, err := time.Parse(time.RFC3339Nano, operation.CompletedAtUTC)
	if err != nil {
		return fmt.Errorf("parse completedAtUTC: %w", err)
	}
	if !completed.Equal(updated) {
		return errors.New("terminal operation completedAtUTC differs from updatedAtUTC")
	}
	return nil
}

func sanitizeOperationMessage(message string) string {
	message = sanitizeTerminalText(message, maximumOperationMessageLen)
	if message == "" {
		return "Operation state changed."
	}
	return message
}

func sanitizeTerminalText(message string, maximumBytes int) string {
	message = strings.Map(func(value rune) rune {
		if isUnsafeTerminalRune(value) {
			return ' '
		}
		return value
	}, message)
	message = strings.Join(strings.Fields(strings.TrimSpace(message)), " ")
	for len([]byte(message)) > maximumBytes {
		_, size := lastRune(message)
		message = message[:len(message)-size]
	}
	return message
}

func lastRune(value string) (rune, int) {
	for index := len(value) - 1; index >= 0; index-- {
		if value[index]&0xc0 != 0x80 {
			return []rune(value[index:])[0], len(value) - index
		}
	}
	return 0, len(value)
}

func nextOperationTimestamp(previous string) string {
	now := time.Now().UTC()
	if parsed, err := time.Parse(time.RFC3339Nano, previous); err == nil && !now.After(parsed) {
		now = parsed.Add(time.Nanosecond)
	}
	return now.Format(time.RFC3339Nano)
}
