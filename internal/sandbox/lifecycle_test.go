package sandbox

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestActiveSessionRoundTrip(t *testing.T) {
	root := t.TempDir()
	runID := "20260724-123456-abcdef12"
	executable := filepath.Join(root, "WindowsSandbox.exe")
	active := testActiveSession(root, runID, executable)
	active.Tailscale = true
	data, err := json.Marshal(active)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, activeSessionFileName), data, 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, found, err := loadActiveSession(root, executable)
	if err != nil {
		t.Fatalf("loadActiveSession: %v", err)
	}
	if !found || loaded != active {
		t.Fatalf("loaded = %#v, found = %t", loaded, found)
	}
}

func TestActiveSessionValidationRejectsChangedOwnership(t *testing.T) {
	root := t.TempDir()
	executable := filepath.Join(root, "WindowsSandbox.exe")
	valid := testActiveSession(root, "20260724-123456-abcdef12", executable)
	tests := map[string]func(*activeSession){
		"schema":       func(value *activeSession) { value.SchemaVersion++ },
		"run":          func(value *activeSession) { value.RunID = "other" },
		"config":       func(value *activeSession) { value.ConfigPath = filepath.Join(root, "other.wsb") },
		"pid":          func(value *activeSession) { value.PID = 0 },
		"executable":   func(value *activeSession) { value.ExecutablePath = filepath.Join(root, "other.exe") },
		"start":        func(value *activeSession) { value.StartedAtUTC = "not-a-time" },
		"command line": func(value *activeSession) { value.CommandLine = "WindowsSandbox.exe other.wsb" },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			value := valid
			mutate(&value)
			if err := value.validate(root, executable); err == nil {
				t.Fatalf("changed ownership unexpectedly validated: %#v", value)
			}
		})
	}
}

func TestActiveSessionRequiresExactEscapedCommandLine(t *testing.T) {
	root := filepath.Join(t.TempDir(), "directory with spaces")
	executable := filepath.Join(root, "Windows Sandbox.exe")
	active := testActiveSession(root, "20260724-123456-abcdef12", executable)
	if err := active.validate(root, executable); err != nil {
		t.Fatalf("validate exact command line: %v", err)
	}
	active.CommandLine += " --extra"
	if err := active.validate(root, executable); err == nil || !strings.Contains(err.Error(), "exact Windows Sandbox launch command") {
		t.Fatalf("extra command-line argument error = %v", err)
	}
}

func TestActiveSessionMatchesExactProcessIdentity(t *testing.T) {
	root := t.TempDir()
	executable := filepath.Join(root, "WindowsSandbox.exe")
	active := testActiveSession(root, "20260724-123456-abcdef12", executable)
	snapshot := sandboxProcessSnapshot{
		PID:            active.PID,
		Name:           "WindowsSandbox.exe",
		ExecutablePath: active.ExecutablePath,
		StartedAtUTC:   active.StartedAtUTC,
		CommandLine:    active.CommandLine,
	}
	if err := active.matches(snapshot); err != nil {
		t.Fatalf("matches: %v", err)
	}
	tests := map[string]func(*sandboxProcessSnapshot){
		"pid":          func(value *sandboxProcessSnapshot) { value.PID++ },
		"name":         func(value *sandboxProcessSnapshot) { value.Name = "other.exe" },
		"executable":   func(value *sandboxProcessSnapshot) { value.ExecutablePath += ".changed" },
		"start":        func(value *sandboxProcessSnapshot) { value.StartedAtUTC = "2026-07-24T12:34:57Z" },
		"command line": func(value *sandboxProcessSnapshot) { value.CommandLine += " --changed" },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			changed := snapshot
			mutate(&changed)
			if err := active.matches(changed); err == nil || !strings.Contains(err.Error(), "identity changed") {
				t.Fatalf("changed process error = %v", err)
			}
		})
	}
}

func TestTailscaleFailurePhaseIdentifiesPreIdentityFailures(t *testing.T) {
	for _, phase := range []string{"guest-identity", "ssh-material", "ssh-verification", "ssh-alias", "herdr-verification", "tailscale-preflight", "tailscale-not-enrolled"} {
		if !tailscaleFailurePrecedesIdentity(phase) {
			t.Fatalf("phase %q should precede Tailscale identity setup", phase)
		}
	}
	for _, phase := range []string{"tailscale-identity", "configuration-sync", "configuration-timeout"} {
		if tailscaleFailurePrecedesIdentity(phase) {
			t.Fatalf("phase %q can follow or overlap Tailscale identity setup", phase)
		}
	}
}

func TestCleanInactiveRunDirectoriesPreservesActiveAndUnknownEntries(t *testing.T) {
	dataDirectory := t.TempDir()
	activeRunID := "20260725-121936-0d9549e4"
	inactiveRunIDs := []string{
		"20260723-101112-12345678",
		"20260724-111213-abcdef12",
	}
	for _, runID := range append([]string{activeRunID}, inactiveRunIDs...) {
		directory := filepath.Join(dataDirectory, "runs", runID, "status")
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(directory, "progress.json"), []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	unknownDirectory := filepath.Join(dataDirectory, "runs", "manual-notes")
	if err := os.MkdirAll(unknownDirectory, 0o700); err != nil {
		t.Fatal(err)
	}

	removed, err := cleanInactiveRunDirectories(dataDirectory, activeRunID)
	if err != nil {
		t.Fatalf("cleanInactiveRunDirectories: %v", err)
	}
	if removed != len(inactiveRunIDs) {
		t.Fatalf("removed runs = %d, want %d", removed, len(inactiveRunIDs))
	}
	for _, preserved := range []string{activeRunID, "manual-notes"} {
		if info, err := os.Stat(filepath.Join(dataDirectory, "runs", preserved)); err != nil || !info.IsDir() {
			t.Fatalf("preserved path %s = %v, %v", preserved, info, err)
		}
	}
	for _, removedRunID := range inactiveRunIDs {
		if _, err := os.Stat(filepath.Join(dataDirectory, "runs", removedRunID)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("inactive run %s still exists: %v", removedRunID, err)
		}
	}
}

func TestCleanInactiveRunDirectoriesPreflightsEveryCandidateBeforeDeletion(t *testing.T) {
	dataDirectory := t.TempDir()
	safeRun := filepath.Join(dataDirectory, "runs", "20260720-101112-12345678")
	unsafeRun := filepath.Join(dataDirectory, "runs", "20260721-101112-abcdef12")
	if err := os.MkdirAll(safeRun, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(unsafeRun, []byte("not a run directory"), 0o600); err != nil {
		t.Fatal(err)
	}

	removed, err := cleanInactiveRunDirectories(dataDirectory, "")
	if err == nil || !strings.Contains(err.Error(), "not a directory") {
		t.Fatalf("removed = %d, error = %v", removed, err)
	}
	if removed != 0 {
		t.Fatalf("removed runs = %d before failed preflight", removed)
	}
	if info, err := os.Stat(safeRun); err != nil || !info.IsDir() {
		t.Fatalf("safe candidate was removed before failed preflight: %v, %v", info, err)
	}
}

func TestRemoveInactiveRunDirectoriesRejectsReplacedRootIdentity(t *testing.T) {
	dataDirectory := t.TempDir()
	runID := "20260720-101112-12345678"
	writeRunDirectoryFixture(t, dataDirectory, runID)
	plan, err := planInactiveRunDirectories(dataDirectory, "")
	if err != nil {
		t.Fatal(err)
	}
	defer plan.close()
	originalRoot := filepath.Join(dataDirectory, "runs-original")
	if err := os.Rename(filepath.Join(dataDirectory, "runs"), originalRoot); err != nil {
		if runtime.GOOS == "windows" {
			return
		}
		t.Fatal(err)
	}
	writeRunDirectoryFixture(t, dataDirectory, runID)

	removed, err := removeInactiveRunDirectories(plan)
	if err == nil || !strings.Contains(err.Error(), "root identity changed") || removed != 0 {
		t.Fatalf("removed = %d, error = %v", removed, err)
	}
	for _, path := range []string{
		filepath.Join(originalRoot, runID),
		filepath.Join(dataDirectory, "runs", runID),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("replaced-root evidence changed %s: %v", path, err)
		}
	}
}

func TestRemoveInactiveRunDirectoriesRejectsReplacedCandidateIdentity(t *testing.T) {
	dataDirectory := t.TempDir()
	runID := "20260720-101112-12345678"
	writeRunDirectoryFixture(t, dataDirectory, runID)
	plan, err := planInactiveRunDirectories(dataDirectory, "")
	if err != nil {
		t.Fatal(err)
	}
	defer plan.close()
	originalRun := filepath.Join(dataDirectory, "runs", "original")
	if err := os.Rename(filepath.Join(dataDirectory, "runs", runID), originalRun); err != nil {
		t.Fatal(err)
	}
	writeRunDirectoryFixture(t, dataDirectory, runID)

	removed, err := removeInactiveRunDirectories(plan)
	if err == nil || !strings.Contains(err.Error(), "identity changed") || removed != 0 {
		t.Fatalf("removed = %d, error = %v", removed, err)
	}
	for _, path := range []string{originalRun, filepath.Join(dataDirectory, "runs", runID)} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("replaced-candidate evidence changed %s: %v", path, err)
		}
	}
}

func TestCleanupStaleStateRemovesGoneRunsAndManagedSSHConfig(t *testing.T) {
	dataDirectory := t.TempDir()
	executable := filepath.Join(dataDirectory, "WindowsSandbox.exe")
	active := testActiveSession(dataDirectory, "20260724-123456-abcdef12", executable)
	inactiveRunID := "20260723-101112-12345678"
	for _, runID := range []string{active.RunID, inactiveRunID} {
		writeRunDirectoryFixture(t, dataDirectory, runID)
	}
	writeJSON(t, filepath.Join(dataDirectory, activeSessionFileName), active)
	managedConfig := filepath.Join(dataDirectory, "ssh", "config")
	writeLifecycleFixtureFile(t, managedConfig, "Host sandbox\n")
	managedSibling := filepath.Join(dataDirectory, "ssh", "keep.txt")
	writeLifecycleFixtureFile(t, managedSibling, "unrelated app-local file")
	identity := filepath.Join(dataDirectory, "identity", "id_ed25519")
	writeLifecycleFixtureFile(t, identity, "persistent identity")
	persistentCache := filepath.Join(dataDirectory, "cache", "payload.bin")
	writeLifecycleFixtureFile(t, persistentCache, "persistent cache")
	unrelatedState := filepath.Join(dataDirectory, "notes", "keep.txt")
	writeLifecycleFixtureFile(t, unrelatedState, "unrelated state")

	inspect := func(context.Context, string, string) (cleanupProtection, error) {
		return cleanupProtection{Active: active, Found: true, SandboxGone: true}, nil
	}
	result, err := cleanupStaleStateWithInspector(context.Background(), dataDirectory, executable, inspect)
	if err != nil {
		t.Fatalf("cleanupStaleStateWithInspector: %v", err)
	}
	if result.RemovedRuns != 2 || result.ActiveRunID != "" {
		t.Fatalf("result = %#v", result)
	}
	for _, path := range []string{
		filepath.Join(dataDirectory, "runs", active.RunID),
		filepath.Join(dataDirectory, "runs", inactiveRunID),
		filepath.Join(dataDirectory, activeSessionFileName),
		managedConfig,
	} {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("stale path still exists %s: %v", path, err)
		}
	}
	for _, path := range []string{identity, persistentCache, unrelatedState, managedSibling} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("persistent or unrelated path changed %s: %v", path, err)
		}
	}
}

func TestCleanupReconcilesRunningOperationBeforeGoneRunDeletion(t *testing.T) {
	dataDirectory := t.TempDir()
	executable := filepath.Join(dataDirectory, "WindowsSandbox.exe")
	active := testActiveSession(dataDirectory, "20260724-123456-abcdef12", executable)
	writeRunDirectoryFixture(t, dataDirectory, active.RunID)
	writeJSON(t, filepath.Join(dataDirectory, activeSessionFileName), active)
	if _, err := startSessionOperation(filepath.Join(dataDirectory, "runs", active.RunID), active.RunID,
		operationKindReprovision, "configuration-sync", "Copying configuration"); err != nil {
		t.Fatal(err)
	}
	inspections := 0
	inspect := func(context.Context, string, string) (cleanupProtection, error) {
		inspections++
		operation, found, err := readSessionOperation(filepath.Join(dataDirectory, "runs", active.RunID))
		if err != nil || !found || operation.State != operationStateInterrupted {
			t.Fatalf("operation before cleanup inspection = %#v, found = %t, error = %v", operation, found, err)
		}
		return cleanupProtection{Active: active, Found: true, SandboxGone: true}, nil
	}
	result, err := cleanupStaleStateWithInspector(context.Background(), dataDirectory, executable, inspect)
	if err != nil || result.RemovedRuns != 1 || inspections != 2 {
		t.Fatalf("result = %#v, inspections = %d, error = %v", result, inspections, err)
	}
	if _, err := os.Lstat(filepath.Join(dataDirectory, "runs", active.RunID)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("gone run was not removed: %v", err)
	}
}

func TestDownReconcilesRunningOperationBeforeProcessInspection(t *testing.T) {
	requireExternalBoundaryTest(t, "Windows process inspection")
	if runtime.GOOS != "windows" {
		t.Skip("Windows Sandbox lifecycle regression")
	}
	dataDirectory := t.TempDir()
	executable := filepath.Join(dataDirectory, "WindowsSandbox.exe")
	active := testActiveSession(dataDirectory, "20260724-123456-abcdef12", executable)
	active.PID = 2147483646
	writeRunDirectoryFixture(t, dataDirectory, active.RunID)
	writeJSON(t, filepath.Join(dataDirectory, activeSessionFileName), active)
	if _, err := startSessionOperation(filepath.Join(dataDirectory, "runs", active.RunID), active.RunID,
		operationKindReprovision, "configuration-sync", "Copying configuration"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_, _ = downAtWithExecutable(ctx, dataDirectory, executable)
	operation, found, err := readSessionOperation(filepath.Join(dataDirectory, "runs", active.RunID))
	if err != nil || !found || operation.State != operationStateInterrupted {
		t.Fatalf("operation before down = %#v, found = %t, error = %v", operation, found, err)
	}
}

func TestCleanupStaleStatePreservesActiveRunAndSSHConfig(t *testing.T) {
	dataDirectory := t.TempDir()
	executable := filepath.Join(dataDirectory, "WindowsSandbox.exe")
	active := testActiveSession(dataDirectory, "20260724-123456-abcdef12", executable)
	inactiveRunID := "20260723-101112-12345678"
	for _, runID := range []string{active.RunID, inactiveRunID} {
		writeRunDirectoryFixture(t, dataDirectory, runID)
	}
	writeJSON(t, filepath.Join(dataDirectory, activeSessionFileName), active)
	managedConfig := filepath.Join(dataDirectory, "ssh", "config")
	writeLifecycleFixtureFile(t, managedConfig, "Host sandbox\n")

	inspect := func(context.Context, string, string) (cleanupProtection, error) {
		return cleanupProtection{Active: active, Found: true}, nil
	}
	result, err := cleanupStaleStateWithInspector(context.Background(), dataDirectory, executable, inspect)
	if err != nil {
		t.Fatalf("cleanupStaleStateWithInspector: %v", err)
	}
	if result.RemovedRuns != 1 || result.ActiveRunID != active.RunID {
		t.Fatalf("result = %#v", result)
	}
	for _, path := range []string{
		filepath.Join(dataDirectory, "runs", active.RunID),
		filepath.Join(dataDirectory, activeSessionFileName),
		managedConfig,
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("active path changed %s: %v", path, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dataDirectory, "runs", inactiveRunID)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("inactive run still exists: %v", err)
	}
}

func TestCleanupStaleStatePreservesEvidenceWhenOwnershipIsUncertain(t *testing.T) {
	dataDirectory := t.TempDir()
	executable := filepath.Join(dataDirectory, "WindowsSandbox.exe")
	active := testActiveSession(dataDirectory, "20260724-123456-abcdef12", executable)
	writeRunDirectoryFixture(t, dataDirectory, active.RunID)
	writeJSON(t, filepath.Join(dataDirectory, activeSessionFileName), active)
	managedConfig := filepath.Join(dataDirectory, "ssh", "config")
	writeLifecycleFixtureFile(t, managedConfig, "Host sandbox\n")

	inspect := func(context.Context, string, string) (cleanupProtection, error) {
		return cleanupProtection{}, errors.New("process ownership is uncertain")
	}
	result, err := cleanupStaleStateWithInspector(context.Background(), dataDirectory, executable, inspect)
	if err == nil || !strings.Contains(err.Error(), "ownership is uncertain") || result.RemovedRuns != 0 {
		t.Fatalf("result = %#v, error = %v", result, err)
	}
	for _, path := range []string{
		filepath.Join(dataDirectory, "runs", active.RunID),
		filepath.Join(dataDirectory, activeSessionFileName),
		managedConfig,
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("evidence changed %s: %v", path, err)
		}
	}
}

func TestCleanupStaleStateRefusesChangedPreflight(t *testing.T) {
	dataDirectory := t.TempDir()
	executable := filepath.Join(dataDirectory, "WindowsSandbox.exe")
	active := testActiveSession(dataDirectory, "20260724-123456-abcdef12", executable)
	writeRunDirectoryFixture(t, dataDirectory, active.RunID)
	calls := 0
	inspect := func(context.Context, string, string) (cleanupProtection, error) {
		calls++
		if calls == 1 {
			return cleanupProtection{Active: active, Found: true}, nil
		}
		return cleanupProtection{SandboxGone: true}, nil
	}
	result, err := cleanupStaleStateWithInspector(context.Background(), dataDirectory, executable, inspect)
	if err == nil || !strings.Contains(err.Error(), "identity changed during preflight") || result.RemovedRuns != 0 {
		t.Fatalf("result = %#v, error = %v", result, err)
	}
	if _, err := os.Stat(filepath.Join(dataDirectory, "runs", active.RunID)); err != nil {
		t.Fatalf("active evidence changed: %v", err)
	}
}

func TestCleanupStaleStateRetriesWhenOwnedSandboxExitsDuringPreflight(t *testing.T) {
	dataDirectory := t.TempDir()
	executable := filepath.Join(dataDirectory, "WindowsSandbox.exe")
	active := testActiveSession(dataDirectory, "20260724-123456-abcdef12", executable)
	writeRunDirectoryFixture(t, dataDirectory, active.RunID)
	writeJSON(t, filepath.Join(dataDirectory, activeSessionFileName), active)
	managedConfig := filepath.Join(dataDirectory, "ssh", "config")
	writeLifecycleFixtureFile(t, managedConfig, "Host sandbox\n")

	calls := 0
	inspect := func(context.Context, string, string) (cleanupProtection, error) {
		calls++
		return cleanupProtection{Active: active, Found: true, SandboxGone: calls > 1}, nil
	}
	result, err := cleanupStaleStateWithInspector(context.Background(), dataDirectory, executable, inspect)
	if err != nil || calls != 3 || result.RemovedRuns != 1 || result.ActiveRunID != "" {
		t.Fatalf("result = %#v, calls = %d, error = %v", result, calls, err)
	}
	for _, path := range []string{filepath.Join(dataDirectory, activeSessionFileName), filepath.Join(dataDirectory, "runs", active.RunID), managedConfig} {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("stale path still exists %s: %v", path, err)
		}
	}
}

func TestCleanupStaleStateRejectsUnsafeSSHBeforeRemovingRuns(t *testing.T) {
	dataDirectory := t.TempDir()
	runID := "20260724-123456-abcdef12"
	writeRunDirectoryFixture(t, dataDirectory, runID)
	sshTarget := filepath.Join(t.TempDir(), "ssh-target")
	if err := os.MkdirAll(sshTarget, 0o700); err != nil {
		t.Fatal(err)
	}
	writeLifecycleFixtureFile(t, filepath.Join(sshTarget, "config"), "Host sandbox\n")
	createTestDirectoryLink(t, filepath.Join(dataDirectory, "ssh"), sshTarget)

	inspect := func(context.Context, string, string) (cleanupProtection, error) {
		return cleanupProtection{SandboxGone: true}, nil
	}
	result, err := cleanupStaleStateWithInspector(context.Background(), dataDirectory, filepath.Join(dataDirectory, "WindowsSandbox.exe"), inspect)
	if err == nil || !strings.Contains(err.Error(), "unsafe managed SSH configuration") || result.RemovedRuns != 0 {
		t.Fatalf("result = %#v, error = %v", result, err)
	}
	if _, err := os.Stat(filepath.Join(dataDirectory, "runs", runID)); err != nil {
		t.Fatalf("run evidence changed before SSH preflight: %v", err)
	}
	if _, err := os.Stat(filepath.Join(sshTarget, "config")); err != nil {
		t.Fatalf("SSH reparse target changed: %v", err)
	}
}

func TestCleanupStaleStateRejectsReparseDataRootBeforeActiveRemoval(t *testing.T) {
	parent := t.TempDir()
	dataDirectory := filepath.Join(parent, "data")
	target := filepath.Join(t.TempDir(), "target")
	if err := os.MkdirAll(target, 0o700); err != nil {
		t.Fatal(err)
	}
	activePath := filepath.Join(target, activeSessionFileName)
	writeLifecycleFixtureFile(t, activePath, "external identity")
	createTestDirectoryLink(t, dataDirectory, target)
	active := testActiveSession(dataDirectory, "20260724-123456-abcdef12", filepath.Join(parent, "WindowsSandbox.exe"))
	inspect := func(context.Context, string, string) (cleanupProtection, error) {
		return cleanupProtection{Active: active, Found: true, SandboxGone: true}, nil
	}

	result, err := cleanupStaleStateWithInspector(context.Background(), dataDirectory, active.ExecutablePath, inspect)
	if err == nil || !strings.Contains(err.Error(), "unsafe cleanup data directory") || result.RemovedRuns != 0 {
		t.Fatalf("result = %#v, error = %v", result, err)
	}
	if data, err := os.ReadFile(activePath); err != nil || string(data) != "external identity" {
		t.Fatalf("external active identity changed: %q, %v", data, err)
	}
}

func TestCleanInactiveRunDirectoriesRejectsNestedReparsePoint(t *testing.T) {
	dataDirectory := t.TempDir()
	runID := "20260720-101112-12345678"
	runDirectory := filepath.Join(dataDirectory, "runs", runID)
	target := filepath.Join(dataDirectory, "outside-target")
	link := filepath.Join(runDirectory, "linked")
	for _, directory := range []string{runDirectory, target} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	createTestDirectoryLink(t, link, target)

	removed, err := cleanInactiveRunDirectories(dataDirectory, "")
	if err == nil || !strings.Contains(err.Error(), "reparse point") {
		t.Fatalf("removed = %d, error = %v", removed, err)
	}
	if removed != 0 {
		t.Fatalf("removed reparse-bearing run count = %d", removed)
	}
	if info, err := os.Stat(target); err != nil || !info.IsDir() {
		t.Fatalf("reparse target changed: %v, %v", info, err)
	}
}

func TestCleanInactiveRunDirectoriesRejectsReparseAncestor(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target-data")
	linkedData := filepath.Join(root, "linked-data")
	runID := "20260720-101112-12345678"
	runDirectory := filepath.Join(target, "runs", runID)
	if err := os.MkdirAll(runDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	createTestDirectoryLink(t, linkedData, target)

	removed, err := cleanInactiveRunDirectories(linkedData, "")
	if err == nil || !strings.Contains(err.Error(), "reparse point") {
		t.Fatalf("removed = %d, error = %v", removed, err)
	}
	if removed != 0 {
		t.Fatalf("removed runs through reparse ancestor = %d", removed)
	}
	if info, err := os.Stat(runDirectory); err != nil || !info.IsDir() {
		t.Fatalf("target run changed through reparse ancestor: %v, %v", info, err)
	}
}

func TestValidateCleanupProcessOwnershipRequiresExactClientParent(t *testing.T) {
	root := t.TempDir()
	active := testActiveSession(root, "20260724-123456-abcdef12", filepath.Join(root, "WindowsSandbox.exe"))
	snapshot := sandboxProcessSnapshot{
		PID:            active.PID,
		Name:           "WindowsSandbox.exe",
		ExecutablePath: active.ExecutablePath,
		StartedAtUTC:   active.StartedAtUTC,
		CommandLine:    active.CommandLine,
	}
	owned := []runningSandboxProcess{
		{Name: "WindowsSandbox", PID: active.PID, ParentPID: 100},
		{Name: "WindowsSandboxClient", PID: active.PID + 1, ParentPID: active.PID},
	}
	if err := validateCleanupProcessOwnership(active, snapshot, true, owned); err != nil {
		t.Fatalf("owned cleanup process tree: %v", err)
	}
	unmanaged := append([]runningSandboxProcess(nil), owned...)
	unmanaged[1].ParentPID = active.PID + 99
	if err := validateCleanupProcessOwnership(active, snapshot, true, unmanaged); err == nil || !strings.Contains(err.Error(), "unmanaged Windows Sandbox client") {
		t.Fatalf("unmanaged client error = %v", err)
	}
}

func TestClassifyManagedSessionUsesTerminalStatusPrecedence(t *testing.T) {
	root := t.TempDir()
	active := testActiveSession(root, "20260724-123456-abcdef12", filepath.Join(root, "WindowsSandbox.exe"))
	statusDirectory := filepath.Join(root, "runs", active.RunID, "status")
	if err := os.MkdirAll(statusDirectory, 0o700); err != nil {
		t.Fatal(err)
	}

	status, err := classifyManagedSession(root, active)
	if err != nil || status.State != SessionStarting || status.Phase != "" {
		t.Fatalf("empty status = %#v, %v", status, err)
	}
	writeJSON(t, filepath.Join(statusDirectory, progressFileName), progressStatus{SchemaVersion: 1, Phase: "base", Message: "Installing"})
	status, err = classifyManagedSession(root, active)
	if err != nil || status.State != SessionStarting || status.Phase != "base" || status.Message != "Installing" {
		t.Fatalf("progress status = %#v, %v", status, err)
	}
	connectable := connectableStatus{SchemaVersion: statusSchemaVersion, IP: "172.24.1.2", SSHUser: "WDAGUtilityAccount", SSHHostKey: "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIGZha2VwdWJsaWNrZXlieXRlcw==", WinGetVersion: "v1"}
	writeJSON(t, filepath.Join(statusDirectory, connectableFileName), connectable)
	status, err = classifyManagedSession(root, active)
	if err != nil || status.State != SessionStarting || status.Phase != "connectable" || status.GuestIP != connectable.IP ||
		status.WinGetVersion != connectable.WinGetVersion || status.HerdrVersion != "" || status.HerdrProtocol != 0 {
		t.Fatalf("connectable status = %#v, %v", status, err)
	}
	ready := readyStatus{SchemaVersion: readyStatusSchemaVersion, IP: connectable.IP, SSHUser: connectable.SSHUser,
		SSHHostKey: connectable.SSHHostKey, WinGetVersion: connectable.WinGetVersion, HerdrVersion: "herdr 1.0.0",
		HerdrRuntimeVersion: "1.0.0+build", HerdrProtocol: 18,
		HerdrBinary: `C:\Users\WDAGUtilityAccount\.herdr\remote\build\herdr.exe`}
	writeJSON(t, filepath.Join(statusDirectory, readyFileName), ready)
	status, err = classifyManagedSession(root, active)
	if err != nil || status.State != SessionReady || status.GuestIP != ready.IP ||
		status.WinGetVersion != ready.WinGetVersion || status.HerdrVersion != ready.HerdrVersion ||
		status.HerdrProtocol != ready.HerdrProtocol {
		t.Fatalf("ready status = %#v, %v", status, err)
	}
	writeJSON(t, filepath.Join(statusDirectory, failureFileName), failureStatus{SchemaVersion: 1, Phase: "attach", Message: "failed"})
	status, err = classifyManagedSession(root, active)
	if err != nil || status.State != SessionFailed || status.Phase != "attach" || status.Message != "failed" {
		t.Fatalf("failure status = %#v, %v", status, err)
	}
}

func TestLoadActiveSessionRejectsUnknownAndTrailingJSON(t *testing.T) {
	root := t.TempDir()
	executable := filepath.Join(root, "WindowsSandbox.exe")
	active := testActiveSession(root, "20260724-123456-abcdef12", executable)
	data, err := json.Marshal(active)
	if err != nil {
		t.Fatal(err)
	}
	tests := map[string][]byte{
		"unknown":       []byte(strings.TrimSuffix(string(data), "}") + `,"extra":true}`),
		"case variant":  []byte(strings.Replace(string(data), `"runID"`, `"RunID"`, 1)),
		"duplicate":     []byte(strings.TrimSuffix(string(data), "}") + `,"pid":5678}`),
		"missing field": []byte(strings.Replace(string(data), `,"tailscale":false`, "", 1)),
		"null boolean":  []byte(strings.Replace(string(data), `"tailscale":false`, `"tailscale":null`, 1)),
		"trailing":      append(append([]byte{}, data...), []byte(` {}`)...),
	}
	for name, contents := range tests {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), activeSessionFileName)
			if err := os.WriteFile(path, contents, 0o600); err != nil {
				t.Fatal(err)
			}
			if _, _, err := loadActiveSession(filepath.Dir(path), executable); err == nil {
				t.Fatal("invalid active identity unexpectedly loaded")
			}
		})
	}
}

func TestParseRunningSandboxProcesses(t *testing.T) {
	processes, err := parseRunningSandboxProcesses([]byte("WindowsSandbox:12:5\r\nWindowsSandboxClient:34:12\r\n"))
	if err != nil {
		t.Fatalf("parseRunningSandboxProcesses: %v", err)
	}
	if len(processes) != 2 || processes[0].PID != 12 || processes[1].Name != "WindowsSandboxClient" || processes[1].ParentPID != 12 {
		t.Fatalf("processes = %#v", processes)
	}
	for _, invalid := range []string{"other:12:5", "WindowsSandbox:0:5", "WindowsSandbox:not-a-pid:5", "WindowsSandbox:12:0", "WindowsSandbox:12"} {
		if _, err := parseRunningSandboxProcesses([]byte(invalid)); err == nil {
			t.Fatalf("invalid record unexpectedly parsed: %q", invalid)
		}
	}
	if processes, err := parseRunningSandboxProcesses(nil); err != nil || len(processes) != 0 {
		t.Fatalf("empty processes = %#v, %v", processes, err)
	}
}

func TestInspectSandboxProcessReportsMissingPID(t *testing.T) {
	requireExternalBoundaryTest(t, "Windows process inspection")
	if runtime.GOOS != "windows" {
		t.Skip("Windows process boundary")
	}
	if _, found, err := inspectSandboxProcess(context.Background(), 2147483647); err != nil || found {
		t.Fatalf("missing process found = %t, error = %v", found, err)
	}
}

func TestSandboxProcessInspectionHandlesProcessDisappearance(t *testing.T) {
	script := sandboxProcessInspectionScript(1234)
	for _, want := range []string{
		"Get-CimInstance Win32_Process",
		"$process = Get-Process -Id 1234 -ErrorAction Stop",
		"if ($null -eq $process) {",
		"$remaining = Get-CimInstance Win32_Process",
		"if ($null -eq $remaining) { exit 3 }",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("process inspection script is missing %q: %s", want, script)
		}
	}
	handleIndex := strings.Index(script, "$handle = $process.Handle")
	startIndex := strings.Index(script, "$startedAtUTC = $process.StartTime")
	if handleIndex < 0 || startIndex <= handleIndex {
		t.Fatalf("process inspection must pin the process handle before reading StartTime: %s", script)
	}
}

func TestInspectSandboxProcessDecodesCurrentProcess(t *testing.T) {
	requireExternalBoundaryTest(t, "Windows process inspection")
	if runtime.GOOS != "windows" {
		t.Skip("Windows process boundary")
	}
	snapshot, found, err := inspectSandboxProcess(context.Background(), os.Getpid())
	if err != nil || !found || snapshot.PID != os.Getpid() || snapshot.Name == "" || snapshot.CommandLine == "" {
		t.Fatalf("snapshot = %#v, found = %t, error = %v", snapshot, found, err)
	}
}

func TestStopOwnedSandboxProcessRefusesChangedIdentity(t *testing.T) {
	requireExternalBoundaryTest(t, "Windows process termination")
	if runtime.GOOS != "windows" {
		t.Skip("Windows process boundary")
	}
	active := activeSession{
		PID:            os.Getpid(),
		ExecutablePath: `C:\Windows\System32\WindowsSandbox.exe`,
		StartedAtUTC:   "2026-07-24T12:34:56Z",
		CommandLine:    `WindowsSandbox.exe C:\not-this-process.wsb`,
	}
	stopped, err := stopOwnedSandboxProcess(context.Background(), active)
	if err == nil || stopped || !strings.Contains(err.Error(), "identity changed") {
		t.Fatalf("stopped = %t, error = %v", stopped, err)
	}
}

func TestLifecycleLockSerializesMutations(t *testing.T) {
	release, err := acquireLifecycleLock(context.Background())
	if err != nil {
		t.Fatalf("acquire first lifecycle lock: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	result := make(chan error, 1)
	go func() {
		secondRelease, err := acquireLifecycleLock(ctx)
		if err == nil {
			_ = secondRelease()
		}
		result <- err
	}()
	if err := <-result; err == nil {
		t.Fatal("second lifecycle mutation acquired the held lock")
	}
	if err := release(); err != nil {
		t.Fatalf("release first lifecycle lock: %v", err)
	}
	secondRelease, err := acquireLifecycleLock(context.Background())
	if err != nil {
		t.Fatalf("reacquire lifecycle lock: %v", err)
	}
	if err := secondRelease(); err != nil {
		t.Fatalf("release second lifecycle lock: %v", err)
	}
}

func TestLifecycleTestsUseProcessIsolatedMutex(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows named-mutex boundary")
	}
	production := `Local\` + applicationName + `-lifecycle-v1`
	if lifecycleMutexName == production || !strings.Contains(lifecycleMutexName, strconv.Itoa(os.Getpid())) {
		t.Fatalf("test lifecycle mutex = %q, production = %q", lifecycleMutexName, production)
	}
}

func TestLifecycleMutationLockTimesOutWithActionableStatusCommand(t *testing.T) {
	release, err := acquireLifecycleLock(context.Background())
	if err != nil {
		t.Fatalf("acquire held lifecycle lock: %v", err)
	}
	defer release()

	result := make(chan error, 1)
	go func() {
		secondRelease, err := acquireLifecycleMutationLockWithin(context.Background(), 50*time.Millisecond)
		if err == nil {
			_ = secondRelease()
		}
		result <- err
	}()
	select {
	case err := <-result:
		if err == nil || !errors.Is(err, context.DeadlineExceeded) || !strings.Contains(err.Error(), "sandbox status") ||
			!strings.Contains(err.Error(), "active command") {
			t.Fatalf("bounded lifecycle lock error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("bounded lifecycle lock wait did not terminate")
	}
}

func testActiveSession(root, runID, executable string) activeSession {
	config := filepath.Join(root, "runs", runID, applicationName+".wsb")
	return activeSession{
		SchemaVersion:  activeSessionSchemaVersion,
		RunID:          runID,
		ConfigPath:     config,
		PID:            1234,
		ExecutablePath: executable,
		StartedAtUTC:   "2026-07-24T12:34:56.1234567Z",
		CommandLine:    expectedWindowsSandboxCommandLine(executable, config),
	}
}

func writeRunDirectoryFixture(t *testing.T, dataDirectory, runID string) {
	t.Helper()
	statusDirectory := filepath.Join(dataDirectory, "runs", runID, "status")
	if err := os.MkdirAll(statusDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(statusDirectory, "progress.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func writeLifecycleFixtureFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}

func createTestDirectoryLink(t *testing.T, link, target string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		script := `New-Item -ItemType Junction -Path $env:HERDR_SANDBOX_TEST_LINK -Target $env:HERDR_SANDBOX_TEST_TARGET -ErrorAction Stop | Out-Null`
		command := hiddenCommand(mustWindowsPowerShellPath(t), "-NoLogo", "-NoProfile", "-NonInteractive", "-EncodedCommand", encodePowerShell(script))
		command.Env = append(os.Environ(), "HERDR_SANDBOX_TEST_LINK="+link, "HERDR_SANDBOX_TEST_TARGET="+target)
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("create Windows junction: %v: %s", err, output)
		}
	} else if err := os.Symlink(target, link); err != nil {
		t.Skipf("directory link unavailable: %v", err)
	}
}
