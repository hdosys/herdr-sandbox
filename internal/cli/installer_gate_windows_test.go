//go:build windows

package cli

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"herdr-sandbox/internal/hiddenprocess"
)

const installerGateCrashHelperEnvironment = "HERDR_SANDBOX_TEST_INSTALLER_GATE_CRASH_HELPER"

func TestInstallerLifecycleGateAcquiresExistingUnownedMutex(t *testing.T) {
	name, err := syscall.UTF16PtrFromString(installerLifecycleMutexName)
	if err != nil {
		t.Fatal(err)
	}
	handle, _, callErr := createInstallerMutex.Call(0, 0, uintptr(unsafe.Pointer(name)))
	if handle == 0 {
		t.Fatalf("create unowned mutex: %v", callErr)
	}
	keeper := syscall.Handle(handle)
	defer syscall.CloseHandle(keeper)

	release, err := acquireInstallerLifecycleGate(nil)
	if err != nil {
		t.Fatalf("acquire existing unowned mutex: %v", err)
	}
	release()
}

func TestInstallerLifecycleGateRejectsLiveOwner(t *testing.T) {
	release, err := acquireInstallerLifecycleGate(nil)
	if err != nil {
		t.Fatalf("acquire first lifecycle owner: %v", err)
	}
	defer release()

	result := make(chan error, 1)
	go func() {
		secondRelease, secondErr := acquireInstallerLifecycleGate(nil)
		if secondRelease != nil {
			secondRelease()
		}
		result <- secondErr
	}()
	select {
	case secondErr := <-result:
		if secondErr == nil || !strings.Contains(secondErr.Error(), "already using the installed files") {
			t.Fatalf("second lifecycle acquisition error = %v", secondErr)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("second lifecycle acquisition did not finish")
	}
}

func TestInstallerLifecycleGateAcquiresAbandonedMutex(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	command := hiddenprocess.CommandContext(ctx, os.Args[0], "-test.run=^TestInstallerLifecycleGateCrashHelper$")
	command.Env = append(os.Environ(), installerGateCrashHelperEnvironment+"=1")
	stdout, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	waited := false
	defer func() {
		if waited {
			return
		}
		_ = command.Terminate()
		_ = command.Wait()
	}()
	ready := make(chan error, 1)
	go func() {
		line, readErr := bufio.NewReader(stdout).ReadString('\n')
		if readErr == nil && strings.TrimSpace(line) != "READY" {
			readErr = fmt.Errorf("helper readiness = %q", line)
		}
		ready <- readErr
	}()
	select {
	case err := <-ready:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatalf("wait for crash helper: %v", ctx.Err())
	}

	name, err := syscall.UTF16PtrFromString(installerLifecycleMutexName)
	if err != nil {
		t.Fatal(err)
	}
	handle, _, callErr := createInstallerMutex.Call(0, 0, uintptr(unsafe.Pointer(name)))
	if handle == 0 {
		t.Fatalf("retain crash-helper mutex: %v", callErr)
	}
	keeper := syscall.Handle(handle)
	defer syscall.CloseHandle(keeper)
	if err := command.Terminate(); err != nil {
		t.Fatalf("terminate crash helper: %v", err)
	}
	_ = command.Wait()
	waited = true

	release, err := acquireInstallerLifecycleGate(nil)
	if err != nil {
		t.Fatalf("acquire abandoned lifecycle mutex: %v", err)
	}
	release()
}

func TestInstallerLifecycleGateCrashHelper(t *testing.T) {
	if os.Getenv(installerGateCrashHelperEnvironment) != "1" {
		t.Skip("subprocess helper")
	}
	release, err := acquireInstallerLifecycleGate(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	fmt.Fprintln(os.Stdout, "READY")
	time.Sleep(30 * time.Second)
}
