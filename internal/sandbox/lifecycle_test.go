package sandbox

import (
	"context"
	"encoding/json"
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
	writeLifecycleStatus(t, filepath.Join(statusDirectory, progressFileName), progressStatus{SchemaVersion: 1, Phase: "base", Message: "Installing"})
	status, err = classifyManagedSession(root, active)
	if err != nil || status.State != SessionStarting || status.Phase != "base" || status.Message != "Installing" {
		t.Fatalf("progress status = %#v, %v", status, err)
	}
	connectable := connectableStatus{SchemaVersion: statusSchemaVersion, IP: "172.24.1.2", SSHUser: "WDAGUtilityAccount", SSHHostKey: "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIGZha2VwdWJsaWNrZXlieXRlcw==", WinGetVersion: "v1", HerdrVersion: "herdr 1.0.0", HerdrProtocol: 18}
	writeLifecycleStatus(t, filepath.Join(statusDirectory, connectableFileName), connectable)
	status, err = classifyManagedSession(root, active)
	if err != nil || status.State != SessionStarting || status.Phase != "connectable" || status.GuestIP != "" || status.HerdrVersion != "" {
		t.Fatalf("connectable status = %#v, %v", status, err)
	}
	ready := readyStatus(connectionStatus(connectable))
	ready.SchemaVersion = readyStatusSchemaVersion
	writeLifecycleStatus(t, filepath.Join(statusDirectory, readyFileName), ready)
	status, err = classifyManagedSession(root, active)
	if err != nil || status.State != SessionReady || status.GuestIP != ready.IP || status.HerdrVersion != ready.HerdrVersion {
		t.Fatalf("ready status = %#v, %v", status, err)
	}
	writeLifecycleStatus(t, filepath.Join(statusDirectory, failureFileName), failureStatus{SchemaVersion: 1, Phase: "attach", Message: "failed"})
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
		"unknown":      []byte(strings.TrimSuffix(string(data), "}") + `,"extra":true}`),
		"case variant": []byte(strings.Replace(string(data), `"runID"`, `"RunID"`, 1)),
		"duplicate":    []byte(strings.TrimSuffix(string(data), "}") + `,"pid":5678}`),
		"trailing":     append(append([]byte{}, data...), []byte(` {}`)...),
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
	processes, err := parseRunningSandboxProcesses([]byte("WindowsSandbox:12\r\nWindowsSandboxClient:34\r\n"))
	if err != nil {
		t.Fatalf("parseRunningSandboxProcesses: %v", err)
	}
	if len(processes) != 2 || processes[0].PID != 12 || processes[1].Name != "WindowsSandboxClient" {
		t.Fatalf("processes = %#v", processes)
	}
	for _, invalid := range []string{"other:12", "WindowsSandbox:0", "WindowsSandbox:not-a-pid"} {
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

func writeLifecycleStatus(t *testing.T, path string, value any) {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}
