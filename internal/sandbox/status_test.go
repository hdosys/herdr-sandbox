package sandbox

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const testHostKey = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAICdW80xOdnkwYnspcN6BjQBSG3lXQ1j2EJd7M2FQmQnP"

func TestWaitForReadyReportsProgressAndReturnsReady(t *testing.T) {
	directory := t.TempDir()
	writeJSON(t, filepath.Join(directory, progressFileName), progressStatus{
		SchemaVersion: statusSchemaVersion,
		Phase:         "winget",
		Message:       "Installing WinGet",
	})

	ready := readyStatus{
		SchemaVersion: readyStatusSchemaVersion,
		IP:            "172.24.16.3",
		SSHUser:       "WDAGUtilityAccount",
		SSHHostKey:    testHostKey,
		WinGetVersion: "v1.29.280",
		HerdrVersion:  "herdr 0.7.5-nightly.test",
		HerdrProtocol: 17,
	}
	go func() {
		time.Sleep(100 * time.Millisecond)
		data, _ := json.Marshal(ready)
		_ = os.WriteFile(filepath.Join(directory, readyFileName), data, 0o600)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var output bytes.Buffer
	got, err := waitForReady(ctx, directory, &output)
	if err != nil {
		t.Fatalf("waitForReady: %v", err)
	}
	if got.IP != ready.IP || got.HerdrProtocol != ready.HerdrProtocol {
		t.Fatalf("ready = %#v", got)
	}
	if !strings.Contains(output.String(), "[winget] Installing WinGet") {
		t.Fatalf("progress output = %q", output.String())
	}
	if !strings.Contains(output.String(), "[+") {
		t.Fatalf("progress output has no elapsed time = %q", output.String())
	}
}

func TestWaitForReadyReturnsGuestFailure(t *testing.T) {
	directory := t.TempDir()
	writeJSON(t, filepath.Join(directory, failureFileName), failureStatus{
		SchemaVersion: statusSchemaVersion,
		Phase:         "openssh",
		Message:       "capability install failed",
	})

	_, err := waitForReady(context.Background(), directory, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), `Sandbox phase "openssh" failed`) {
		t.Fatalf("waitForReady error = %v", err)
	}
}

func TestWaitForReadyReportsCancellationCause(t *testing.T) {
	ctx, cancel := context.WithCancelCause(context.Background())
	cancel(errors.New("Windows Sandbox launcher exited"))
	_, err := waitForReady(ctx, t.TempDir(), &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "Windows Sandbox launcher exited") {
		t.Fatalf("waitForReady cancellation error = %v", err)
	}
}

func TestWaitForConnectableReturnsBeforeTerminalReady(t *testing.T) {
	directory := t.TempDir()
	connectable := connectableStatus{
		SchemaVersion: statusSchemaVersion,
		IP:            "172.24.16.3",
		SSHUser:       "WDAGUtilityAccount",
		SSHHostKey:    testHostKey,
		WinGetVersion: "v1.29.280",
		HerdrVersion:  "herdr 0.7.5-nightly.test",
		HerdrProtocol: 18,
	}
	writeJSON(t, filepath.Join(directory, connectableFileName), connectable)

	got, err := waitForConnectable(context.Background(), directory, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("waitForConnectable: %v", err)
	}
	if got.IP != connectable.IP || got.HerdrProtocol != connectable.HerdrProtocol {
		t.Fatalf("connectable = %#v", got)
	}
	if _, err := os.Stat(filepath.Join(directory, readyFileName)); !os.IsNotExist(err) {
		t.Fatalf("terminal ready unexpectedly exists: %v", err)
	}
}

func TestWaitForReadyToleratesTransientProgressReadFailure(t *testing.T) {
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, progressFileName), []byte(`{"schemaVersion":`), 0o600); err != nil {
		t.Fatal(err)
	}
	ready := readyStatus{
		SchemaVersion: readyStatusSchemaVersion,
		IP:            "172.24.16.3",
		SSHUser:       "WDAGUtilityAccount",
		SSHHostKey:    testHostKey,
		WinGetVersion: "v1.29.280",
		HerdrVersion:  "herdr 0.7.5-nightly.test",
		HerdrProtocol: 17,
	}
	go func() {
		time.Sleep(750 * time.Millisecond)
		data, _ := json.Marshal(ready)
		_ = os.WriteFile(filepath.Join(directory, readyFileName), data, 0o600)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	got, err := waitForReady(ctx, directory, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("waitForReady: %v", err)
	}
	if got.IP != ready.IP {
		t.Fatalf("ready = %#v", got)
	}
}

func TestReadyStatusRejectsUnknownField(t *testing.T) {
	path := filepath.Join(t.TempDir(), readyFileName)
	data := `{"schemaVersion":2,"ip":"172.24.16.3","sshUser":"WDAGUtilityAccount","sshHostKey":"` + testHostKey + `","wingetVersion":"1","herdrVersion":"1","herdrProtocol":17,"unexpected":true}`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := readOptionalStatus[readyStatus](path); err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("readOptionalStatus error = %v", err)
	}
}

func TestReadyStatusValidatesBoundaryFields(t *testing.T) {
	valid := readyStatus{
		SchemaVersion: readyStatusSchemaVersion,
		IP:            "172.24.16.3",
		SSHUser:       "WDAGUtilityAccount",
		SSHHostKey:    testHostKey,
		WinGetVersion: "1",
		HerdrVersion:  "1",
		HerdrProtocol: 17,
	}
	tests := []struct {
		name   string
		mutate func(*readyStatus)
	}{
		{name: "IPv6", mutate: func(value *readyStatus) { value.IP = "::1" }},
		{name: "user", mutate: func(value *readyStatus) { value.SSHUser = "other" }},
		{name: "host key", mutate: func(value *readyStatus) { value.SSHHostKey = "ssh-rsa AAAA" }},
		{name: "winget terminal control", mutate: func(value *readyStatus) { value.WinGetVersion = "1\x1b[2J" }},
		{name: "herdr terminal control", mutate: func(value *readyStatus) { value.HerdrVersion = "1\x1b]8;;https://example.test\a" }},
		{name: "protocol", mutate: func(value *readyStatus) { value.HerdrProtocol = 0 }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			value := valid
			test.mutate(&value)
			if err := value.validate(); err == nil {
				t.Fatal("validate unexpectedly succeeded")
			}
		})
	}
}

func TestGuestDisplayStatusRejectsTerminalControls(t *testing.T) {
	for name, validate := range map[string]func() error{
		"progress phase": func() error {
			return (progressStatus{SchemaVersion: 1, Phase: "phase\x1b[2J", Message: "message"}).validate()
		},
		"progress message": func() error {
			return (progressStatus{SchemaVersion: 1, Phase: "phase", Message: "message\x1b]8;;https://example.test\a"}).validate()
		},
		"failure message": func() error {
			return (failureStatus{SchemaVersion: 1, Phase: "phase", Message: "message\bhidden"}).validate()
		},
	} {
		t.Run(name, func(t *testing.T) {
			if err := validate(); err == nil || !strings.Contains(err.Error(), "terminal control") {
				t.Fatalf("validation error = %v", err)
			}
		})
	}
}

func TestReadyStatusRejectsOldSchemaAndAmbiguousJSON(t *testing.T) {
	valid := `{"schemaVersion":2,"ip":"172.24.16.3","sshUser":"WDAGUtilityAccount","sshHostKey":"` + testHostKey + `","wingetVersion":"1","herdrVersion":"1","herdrProtocol":17}`
	tests := map[string]string{
		"case variant": strings.Replace(valid, `"ip"`, `"IP"`, 1),
		"duplicate":    strings.TrimSuffix(valid, "}") + `,"ip":"172.24.16.4"}`,
		"trailing":     valid + ` {}`,
	}
	for name, data := range tests {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), readyFileName)
			if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, _, err := readOptionalStatus[readyStatus](path); err == nil {
				t.Fatal("ambiguous ready status unexpectedly decoded")
			}
		})
	}
	old := readyStatus{
		SchemaVersion: statusSchemaVersion,
		IP:            "172.24.16.3",
		SSHUser:       "WDAGUtilityAccount",
		SSHHostKey:    testHostKey,
		WinGetVersion: "1",
		HerdrVersion:  "1",
		HerdrProtocol: 17,
	}
	if err := old.validate(); err == nil || !strings.Contains(err.Error(), "want 2") {
		t.Fatalf("old ready schema validation error = %v", err)
	}
}

func TestConfigurationHandoffIsStrictAndSingleAssignment(t *testing.T) {
	directory := t.TempDir()
	verified := configurationHandoffStatus{
		SchemaVersion: statusSchemaVersion,
		Outcome:       configurationHandoffVerified,
	}
	if err := writeConfigurationHandoff(directory, verified); err != nil {
		t.Fatalf("writeConfigurationHandoff: %v", err)
	}
	if err := writeConfigurationHandoff(directory, verified); err == nil || !strings.Contains(err.Error(), "already published") {
		t.Fatalf("second handoff error = %v", err)
	}
	failed := configurationHandoffStatus{
		SchemaVersion: statusSchemaVersion,
		Outcome:       configurationHandoffFailed,
		Phase:         "configuration-sync",
		Message:       "copy failed",
	}
	if err := failed.validate(); err != nil {
		t.Fatalf("failed handoff validation: %v", err)
	}
	failed.Message = ""
	if err := failed.validate(); err == nil {
		t.Fatal("failed handoff accepted an empty message")
	}
}

func TestReadyIdentityMustMatchConnectableIdentity(t *testing.T) {
	connectable := connectableStatus{
		SchemaVersion: statusSchemaVersion,
		IP:            "172.24.16.3",
		SSHUser:       "WDAGUtilityAccount",
		SSHHostKey:    testHostKey,
		WinGetVersion: "v1.29.280",
		HerdrVersion:  "herdr 0.7.5-nightly.test",
		HerdrProtocol: 18,
	}
	ready := readyStatus(connectionStatus(connectable))
	ready.SchemaVersion = readyStatusSchemaVersion
	if !sameConnectionIdentity(connectable, ready) {
		t.Fatal("matching identities did not compare equal")
	}
	ready.HerdrProtocol++
	if sameConnectionIdentity(connectable, ready) {
		t.Fatal("changed ready identity compared equal")
	}
}

func writeJSON(t *testing.T, path string, value any) {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}
