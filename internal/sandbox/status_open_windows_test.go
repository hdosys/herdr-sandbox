package sandbox

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestReadStatusDuringAtomicReplacement(t *testing.T) {
	path := filepath.Join(t.TempDir(), "explorer-restart.json")
	data := []byte(`{"schemaVersion":1,"restartId":"test","taskName":"HerdrSandbox-ExplorerRestart-test","state":"succeeded","sessionId":1,"stoppedPids":[10],"startedPids":[20],"message":""}`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	name, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		t.Fatal(err)
	}
	// Hold the destination access needed by the guest's File.Replace publisher.
	const deleteAccess = 0x00010000
	handle, err := syscall.CreateFile(name, syscall.GENERIC_READ|deleteAccess,
		syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE|syscall.FILE_SHARE_DELETE,
		nil, syscall.OPEN_EXISTING, syscall.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer syscall.CloseHandle(handle)
	status, found, err := readOptionalStatus[explorerRestartStatus](path)
	if err != nil || !found {
		t.Fatalf("read published status with replacement handle open: found=%v err=%v", found, err)
	}
	if err := status.validate(); err != nil {
		t.Fatal(err)
	}
	if status.State != explorerRestartStatusSucceeded || status.RestartID != "test" {
		t.Fatalf("unexpected published status: %+v", status)
	}
}
