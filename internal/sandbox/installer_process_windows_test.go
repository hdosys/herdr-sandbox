//go:build windows

package sandbox

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"herdr-sandbox/internal/hiddenprocess"
)

const installerProcessFixtureEnvironment = "HERDR_INSTALLER_PROCESS_FIXTURE"

func TestStopInstallerProcessesTerminatesOnlyExactExecutablePeers(t *testing.T) {
	if os.Getenv(installerProcessFixtureEnvironment) == "1" {
		fmt.Println("ready")
		time.Sleep(30 * time.Second)
		return
	}

	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	exactPeer := startInstallerProcessFixture(t, executable)

	differentExecutable := filepath.Join(t.TempDir(), filepath.Base(executable))
	data, err := os.ReadFile(executable)
	if err != nil {
		t.Fatalf("read fixture executable: %v", err)
	}
	if err := os.WriteFile(differentExecutable, data, 0o700); err != nil {
		t.Fatalf("copy fixture executable: %v", err)
	}
	differentPathPeer := startInstallerProcessFixture(t, differentExecutable)

	stopContext, cancelStop := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelStop()
	if err := StopInstallerProcesses(stopContext); err != nil {
		t.Fatalf("stop installed executable peer: %v", err)
	}
	if err := exactPeer.Wait(); err == nil {
		t.Fatal("installed executable peer exited normally instead of being terminated")
	}
	if err := differentPathPeer.Process.Kill(); err != nil {
		t.Fatalf("same-name different-path process did not survive exact-path shutdown: %v", err)
	}
	_ = differentPathPeer.Wait()
}

func startInstallerProcessFixture(t *testing.T, executable string) *hiddenprocess.Command {
	t.Helper()
	childContext, cancelChild := context.WithTimeout(context.Background(), 15*time.Second)
	command := hiddenCommandContext(childContext, executable, "-test.run=^TestStopInstallerProcessesTerminatesOnlyExactExecutablePeers$")
	command.Env = append(os.Environ(), installerProcessFixtureEnvironment+"=1")
	stdout, err := command.StdoutPipe()
	if err != nil {
		cancelChild()
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	command.Stderr = &stderr
	if err := command.Start(); err != nil {
		cancelChild()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cancelChild()
		_ = command.Process.Kill()
		_ = command.Wait()
	})
	line, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil || strings.TrimSpace(line) != "ready" {
		t.Fatalf("fixture readiness = %q, %v: %s", line, err, stderr.String())
	}
	return command
}
