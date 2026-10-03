package sandbox

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestDecodeGuestFreeSpaceValidatesGuestBoundary(t *testing.T) {
	freeSpace, err := decodeGuestFreeSpace([]byte(`{"schemaVersion":1,"volume":"C:","freeBytes":3221225472,"totalBytes":8589934592}`))
	if err != nil {
		t.Fatalf("decodeGuestFreeSpace: %v", err)
	}
	if freeSpace.Volume != "C:" || freeSpace.FreeBytes != 3<<30 || freeSpace.TotalBytes != 8<<30 {
		t.Fatalf("free space = %#v", freeSpace)
	}

	invalid := map[string]string{
		"unknown field": `{"schemaVersion":1,"volume":"C:","freeBytes":1,"totalBytes":2,"usedBytes":1}`,
		"duplicate":     `{"schemaVersion":1,"volume":"C:","freeBytes":1,"freeBytes":1,"totalBytes":2}`,
		"schema":        `{"schemaVersion":2,"volume":"C:","freeBytes":1,"totalBytes":2}`,
		"volume":        `{"schemaVersion":1,"volume":"D:","freeBytes":1,"totalBytes":2}`,
		"empty total":   `{"schemaVersion":1,"volume":"C:","freeBytes":0,"totalBytes":0}`,
		"free overflow": `{"schemaVersion":1,"volume":"C:","freeBytes":3,"totalBytes":2}`,
	}
	for name, data := range invalid {
		t.Run(name, func(t *testing.T) {
			if _, err := decodeGuestFreeSpace([]byte(data)); err == nil {
				t.Fatal("invalid guest free space unexpectedly decoded")
			}
		})
	}
}

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

func TestFailedInitialProvisioningCanRetryWithoutReplacingGuest(t *testing.T) {
	root := t.TempDir()
	active := testActiveSession(root, "20260729-120000-abcdef12", filepath.Join(root, "WindowsSandbox.exe"))
	runDirectory := filepath.Join(root, "runs", active.RunID)
	statusDirectory := filepath.Join(runDirectory, "status")
	if err := os.MkdirAll(statusDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	connectable := connectableStatus{SchemaVersion: 1, IP: "172.24.1.2", SSHUser: "WDAGUtilityAccount", SSHHostKey: testHostKey, WinGetVersion: "v1"}
	writeJSON(t, filepath.Join(statusDirectory, connectableFileName), connectable)
	operation, err := startSessionOperation(runDirectory, active.RunID, operationKindReprovision, "development-provisioning", "Project profile failed.")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := finishSessionOperation(runDirectory, operation, operationStateFailed, operation.Phase, operation.Message); err != nil {
		t.Fatal(err)
	}
	status, err := classifyManagedSession(root, active)
	if err != nil {
		t.Fatal(err)
	}
	enrichSessionStatus(root, active, &status)
	if status.State != SessionFailed || !canResumeSession(status) || status.RunID != active.RunID || status.GuestIP != connectable.IP ||
		!strings.Contains(sessionNextAction(status), "same Sandbox") {
		t.Fatalf("failed initial provisioning lost retry identity: %#v", status)
	}
	retry, err := startSessionOperation(runDirectory, active.RunID, operationKindReprovision, "development-provisioning", "Corrected profile.")
	if err != nil || retry.ID == operation.ID {
		t.Fatalf("retry operation = %#v, error=%v", retry, err)
	}
	ready := readyStatus(connectable)
	ready.SchemaVersion = readyStatusSchemaVersion
	ready.HerdrVersion, ready.HerdrRuntimeVersion, ready.HerdrProtocol, ready.HerdrBinary = "herdr 1", "1+build", 18, testGuestHerdrExecutable
	if err := writeReadyStatus(statusDirectory, ready); err != nil {
		t.Fatal(err)
	}
	if _, err := finishSessionOperation(runDirectory, retry, operationStateSucceeded, "completed", "Ready."); err != nil {
		t.Fatal(err)
	}
	status, err = classifyManagedSession(root, active)
	if err != nil || status.State != SessionReady || status.RunID != active.RunID || status.PID != active.PID || !sameConnectionIdentity(connectable, ready) {
		t.Fatalf("retry did not retain guest identity: %#v, %v", status, err)
	}
	for _, unsafe := range []SessionStatus{{State: SessionStale}, {State: SessionUnmanaged}, {State: SessionFailed}} {
		if canResumeSession(unsafe) {
			t.Fatalf("unsafe session admitted: %#v", unsafe)
		}
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
