package sandbox

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestEnrichSessionStatusKeepsGuestReadinessSeparateFromLatestOperation(t *testing.T) {
	dataDirectory := t.TempDir()
	runID := "20260729-120000-abcdef12"
	runDirectory := filepath.Join(dataDirectory, "runs", runID)
	provisioningDirectory := filepath.Join(runDirectory, "input", "provisioning")
	statusDirectory := filepath.Join(runDirectory, "status")
	for _, directory := range []string{provisioningDirectory, statusDirectory} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	manifest, err := encodeGuestWorkspaceManifest([]workspacePlan{{
		Name: "project", GuestDirectory: guestWorkspaceDirectory("project"),
	}}, guestWorkspaceDirectory("project"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(provisioningDirectory, workspaceManifestName), manifest, 0o600); err != nil {
		t.Fatal(err)
	}
	operation, err := startSessionOperation(runDirectory, runID, operationKindReprovision,
		"development-provisioning", "Installing the selected stacks.")
	if err != nil {
		t.Fatal(err)
	}
	_, err = finishSessionOperation(runDirectory, operation, operationStateFailed,
		"failed", "The latest retained reprovision failed.")
	if err != nil {
		t.Fatal(err)
	}
	active := activeSession{RunID: runID, StartedAtUTC: "2026-07-29T12:00:00Z"}
	status := SessionStatus{State: SessionReady, RunID: runID}
	enrichSessionStatus(dataDirectory, active, &status)
	status.NextAction = sessionNextAction(status)
	if status.State != SessionReady || status.Operation == nil || status.Operation.State != operationStateFailed ||
		len(status.Workspaces) != 1 || !status.Workspaces[0].Active ||
		!strings.Contains(status.NextAction, "attach") {
		t.Fatalf("enriched status = %#v", status)
	}
}

func TestEnrichReadySessionReportsProtectedMobileAccess(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows DPAPI boundary")
	}
	dataDirectory := t.TempDir()
	runID := "20260729-120000-abcdef12"
	inputDirectory := filepath.Join(dataDirectory, "runs", runID, "input")
	statusDirectory := filepath.Join(dataDirectory, "runs", runID, "status")
	for _, directory := range []string{inputDirectory, statusDirectory} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	key := testEd25519PublicKey(1)
	if err := writeMobileSSHAuthorizedKeysInput(inputDirectory, []string{key}); err != nil {
		t.Fatal(err)
	}
	if err := storeTailscaleIdentity(dataDirectory, testTailscaleIdentity(t, "100.64.0.10")); err != nil {
		t.Fatal(err)
	}
	if err := storeMobileSSHIdentity(dataDirectory, testMobileSSHIdentity()); err != nil {
		t.Fatal(err)
	}
	active := activeSession{RunID: runID, StartedAtUTC: "2026-07-29T12:00:00Z", Tailscale: true}
	status := SessionStatus{State: SessionReady, RunID: runID}
	enrichSessionStatus(dataDirectory, active, &status)
	if status.MobileAccess == nil || status.MobileAccess.URI != "ssh://WDAGUtilityAccount@herdr-sandbox.example.ts.net:2222" ||
		strings.Contains(strings.Join(status.Warnings, "\n"), "Mobile SSH") {
		t.Fatalf("enriched mobile status = %#v", status)
	}
}

func TestSessionNextActionWaitsForRunningRetainedOperation(t *testing.T) {
	status := SessionStatus{
		State: SessionReady,
		Operation: &SessionOperation{
			State: operationStateRunning,
		},
	}
	if next := sessionNextAction(status); !strings.Contains(next, "Wait") || !strings.Contains(next, "attach") {
		t.Fatalf("next action = %q", next)
	}
}

func TestInterruptAbandonedSessionOperationPersistsTerminalRecovery(t *testing.T) {
	dataDirectory := t.TempDir()
	runID := "20260729-120000-abcdef12"
	runDirectory := filepath.Join(dataDirectory, "runs", runID)
	if err := os.MkdirAll(runDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	_, err := startSessionOperation(runDirectory, runID, operationKindReprovision,
		"configuration-sync", "Copying configuration")
	if err != nil {
		t.Fatal(err)
	}
	interruptedOperation, interrupted, err := interruptAbandonedRunOperation(dataDirectory, runID)
	if err != nil || !interrupted || interruptedOperation.State != operationStateInterrupted ||
		interruptedOperation.Phase != "interrupted" {
		t.Fatalf("interrupted = %t, operation = %#v, error = %v", interrupted, interruptedOperation, err)
	}
	loaded, found, err := readSessionOperation(runDirectory)
	if err != nil || !found || loaded != interruptedOperation {
		t.Fatalf("persisted interruption = %#v, found = %t, error = %v", loaded, found, err)
	}
}
