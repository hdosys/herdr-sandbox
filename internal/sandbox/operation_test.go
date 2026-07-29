package sandbox

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSessionOperationRoundTripAndTerminalOutcome(t *testing.T) {
	runDirectory := t.TempDir()
	operation, err := startSessionOperation(runDirectory, "20260729-120000-abcdef12",
		operationKindReprovision, "preparing", "Preparing retained provisioning")
	if err != nil {
		t.Fatal(err)
	}
	operation, err = updateSessionOperation(runDirectory, operation,
		"development-provisioning", "Installing modern tooling")
	if err != nil {
		t.Fatal(err)
	}
	operation, err = finishSessionOperation(runDirectory, operation,
		operationStateSucceeded, "completed", "Retained provisioning verified.")
	if err != nil {
		t.Fatal(err)
	}
	loaded, found, err := readSessionOperation(runDirectory)
	if err != nil || !found || loaded != operation {
		t.Fatalf("loaded = %#v, found = %t, error = %v", loaded, found, err)
	}
	if loaded.State != operationStateSucceeded || loaded.CompletedAtUTC == "" {
		t.Fatalf("terminal operation = %#v", loaded)
	}
}

func TestSessionOperationIsStrictAndBounded(t *testing.T) {
	runDirectory := t.TempDir()
	valid := SessionOperation{
		SchemaVersion: operationSchemaVersion,
		ID:            "20260729-120001-abcdef12",
		RunID:         "20260729-120000-abcdef12",
		Kind:          operationKindReprovision,
		State:         operationStateRunning,
		Phase:         "preparing",
		Message:       "Preparing retained provisioning.",
		StartedAtUTC:  "2026-07-29T12:00:01Z",
		UpdatedAtUTC:  "2026-07-29T12:00:01Z",
	}
	data, err := json.Marshal(valid)
	if err != nil {
		t.Fatal(err)
	}
	for name, contents := range map[string][]byte{
		"unknown":   []byte(strings.TrimSuffix(string(data), "}") + `,"extra":true}`),
		"duplicate": []byte(strings.TrimSuffix(string(data), "}") + `,"state":"failed"}`),
		"missing":   []byte(strings.Replace(string(data), `,"message":"Preparing retained provisioning."`, "", 1)),
		"trailing":  append(append([]byte{}, data...), []byte(` {}`)...),
		"oversized": []byte(strings.Repeat("x", maximumOperationFileBytes+1)),
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(runDirectory, operationFileName)
			if err := os.WriteFile(path, contents, 0o600); err != nil {
				t.Fatal(err)
			}
			if _, _, err := readSessionOperation(runDirectory); err == nil {
				t.Fatal("invalid operation unexpectedly loaded")
			}
		})
	}
}

func TestSessionOperationKeepsGuestAndOperationRunIdentityExact(t *testing.T) {
	runDirectory := t.TempDir()
	operation, err := startSessionOperation(runDirectory, "20260729-120000-abcdef12",
		operationKindReprovision, "preparing", "Preparing")
	if err != nil {
		t.Fatal(err)
	}
	operation.RunID = "other"
	if err := writeSessionOperation(runDirectory, operation); err == nil {
		t.Fatal("invalid run identity unexpectedly written")
	}
}

func TestSanitizeOperationMessageIsSingleLineAndByteBounded(t *testing.T) {
	message := sanitizeOperationMessage("  first\r\nsecond\x1b]8;;https://example.test\a link  " + strings.Repeat("é", 400))
	if strings.ContainsAny(message, "\r\n\x1b\a") || len([]byte(message)) > maximumOperationMessageLen ||
		!strings.HasPrefix(message, "first second") {
		t.Fatalf("sanitized message = %q (%d bytes)", message, len([]byte(message)))
	}
}

func TestStartSessionOperationRefusesToOverwriteRunningOutcome(t *testing.T) {
	runDirectory := t.TempDir()
	first, err := startSessionOperation(runDirectory, "20260729-120000-abcdef12",
		operationKindReprovision, "preparing", "Preparing")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := startSessionOperation(runDirectory, first.RunID,
		operationKindReprovision, "preparing", "Preparing again"); err == nil || !strings.Contains(err.Error(), "still marked running") {
		t.Fatalf("second operation error = %v", err)
	}
	loaded, found, err := readSessionOperation(runDirectory)
	if err != nil || !found || loaded != first {
		t.Fatalf("preserved operation = %#v, found = %t, error = %v", loaded, found, err)
	}
}

func TestOperationFailureStatePreservesCancellationAndTimeout(t *testing.T) {
	if state := operationFailureState(context.Background(), errors.New("failed")); state != operationStateFailed {
		t.Fatalf("failure state = %s", state)
	}
	if state := operationFailureState(context.Background(), context.Canceled); state != operationStateCancelled {
		t.Fatalf("cancelled state = %s", state)
	}
	if state := operationFailureState(context.Background(), context.DeadlineExceeded); state != operationStateTimedOut {
		t.Fatalf("timeout state = %s", state)
	}
}
