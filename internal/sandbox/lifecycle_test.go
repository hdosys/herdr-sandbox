package sandbox

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
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
	for _, phase := range []string{"guest-identity", "ssh-material", "ssh-verification", "herdr-verification", "tailscale-preflight", "tailscale-not-enrolled"} {
		if !tailscaleFailurePrecedesIdentity(phase) {
			t.Fatalf("phase %q should precede Tailscale identity setup", phase)
		}
	}
	for _, phase := range []string{"tailscale-identity", "configuration-sync", "ssh-alias", "configuration-timeout"} {
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
	connectable := connectableStatus{SchemaVersion: statusSchemaVersion, IP: "172.24.1.2", SSHUser: "WDAGUtilityAccount", SSHHostKey: "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIGZha2VwdWJsaWNrZXlieXRlcw==", WinGetVersion: "v1", HerdrVersion: "herdr 1.0.0", HerdrProtocol: 18}
	writeJSON(t, filepath.Join(statusDirectory, connectableFileName), connectable)
	status, err = classifyManagedSession(root, active)
	if err != nil || status.State != SessionStarting || status.Phase != "connectable" || status.GuestIP != "" || status.HerdrVersion != "" {
		t.Fatalf("connectable status = %#v, %v", status, err)
	}
	ready := readyStatus(connectionStatus(connectable))
	ready.SchemaVersion = readyStatusSchemaVersion
	writeJSON(t, filepath.Join(statusDirectory, readyFileName), ready)
	status, err = classifyManagedSession(root, active)
	if err != nil || status.State != SessionReady || status.GuestIP != ready.IP || status.HerdrVersion != ready.HerdrVersion {
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
	if runtime.GOOS != "windows" {
		t.Skip("Windows process boundary")
	}
	if _, found, err := inspectSandboxProcess(context.Background(), 2147483647); err != nil || found {
		t.Fatalf("missing process found = %t, error = %v", found, err)
	}
}

func TestInspectSandboxProcessDecodesCurrentProcess(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows process boundary")
	}
	snapshot, found, err := inspectSandboxProcess(context.Background(), os.Getpid())
	if err != nil || !found || snapshot.PID != os.Getpid() || snapshot.Name == "" || snapshot.CommandLine == "" {
		t.Fatalf("snapshot = %#v, found = %t, error = %v", snapshot, found, err)
	}
}

func TestStopOwnedSandboxProcessRefusesChangedIdentity(t *testing.T) {
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
