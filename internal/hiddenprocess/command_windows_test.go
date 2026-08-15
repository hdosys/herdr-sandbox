//go:build windows

package hiddenprocess

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

const (
	processTreeHelperMode = "HERDR_SANDBOX_PROCESS_TREE_HELPER"
	processTreePIDFile    = "HERDR_SANDBOX_PROCESS_TREE_PID_FILE"
	successHelperMode     = "HERDR_SANDBOX_SUCCESS_HELPER"
	largeOutputHelperMode = "HERDR_SANDBOX_LARGE_OUTPUT_HELPER"
	parentWaitsForChild   = "parent-waits"
	parentExitsAfterSpawn = "parent-exits"
	grandchildSleeps      = "grandchild"
)

const windowsSharingViolation syscall.Errno = 32

func TestCommandCombinedOutputUsesOwnedRunPath(t *testing.T) {
	command := CommandContext(t.Context(), os.Args[0], "-test.run=^TestSuccessfulCommandHelper$")
	command.Env = append(os.Environ(), successHelperMode+"=1")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("CombinedOutput: %v", err)
	}
	if string(output) != "owned" {
		t.Fatalf("CombinedOutput = %q, want owned", output)
	}
}

func TestCommandCombinedOutputStopsAtMemoryLimit(t *testing.T) {
	command := CommandContext(t.Context(), os.Args[0], "-test.run=^TestLargeOutputCommandHelper$")
	command.Env = append(os.Environ(), largeOutputHelperMode+"=1")
	output, err := command.CombinedOutput()
	if !errors.Is(err, errCombinedOutputLimit) {
		t.Fatalf("CombinedOutput error = %v, want output limit", err)
	}
	if len(output) != maximumCombinedOutputBytes {
		t.Fatalf("CombinedOutput length = %d, want %d", len(output), maximumCombinedOutputBytes)
	}
}

func TestLargeOutputCommandHelper(t *testing.T) {
	if os.Getenv(largeOutputHelperMode) == "1" {
		fmt.Fprint(os.Stdout, strings.Repeat("x", maximumCombinedOutputBytes+1))
		os.Exit(0)
	}
}

func TestSuccessfulCommandHelper(t *testing.T) {
	if os.Getenv(successHelperMode) == "1" {
		fmt.Fprint(os.Stdout, "owned")
		os.Exit(0)
	}
}

func TestCommandContextCreatesSuspendedHiddenProcessTreeConsole(t *testing.T) {
	command := CommandContext(t.Context(), "powershell.exe", "-NoProfile")
	if command.SysProcAttr == nil || !command.SysProcAttr.HideWindow {
		t.Fatal("owned command does not set HideWindow")
	}
	wantFlags := uint32(createNewConsole | createSuspended)
	if command.SysProcAttr.CreationFlags&wantFlags != wantFlags {
		t.Fatalf("owned command creation flags = %#x, want %#x", command.SysProcAttr.CreationFlags, wantFlags)
	}
	if command.WaitDelay != commandWaitDelay {
		t.Fatalf("owned command WaitDelay = %s, want %s", command.WaitDelay, commandWaitDelay)
	}
	if !slices.Equal(command.Args[1:3], []string{"-WindowStyle", "Hidden"}) {
		t.Fatalf("owned PowerShell startup arguments = %#v, want hidden window style first", command.Args)
	}
}

func TestJobObjectInformationLayoutOn64BitWindows(t *testing.T) {
	if unsafe.Sizeof(uintptr(0)) != 8 {
		t.Skip("amd64-compatible Job Object layout is only asserted on 64-bit Windows")
	}
	if got := unsafe.Sizeof(jobObjectBasicLimitInformation{}); got != 64 {
		t.Fatalf("JOBOBJECT_BASIC_LIMIT_INFORMATION size = %d, want 64", got)
	}
	if got := unsafe.Sizeof(jobObjectExtendedLimitInformation{}); got != 144 {
		t.Fatalf("JOBOBJECT_EXTENDED_LIMIT_INFORMATION size = %d, want 144", got)
	}
}

func TestCommandContextCancelsDescendantProcessTree(t *testing.T) {
	fixture := startProcessTreeFixture(t, parentWaitsForChild)
	fixture.cancel()
	waitErr := fixture.wait()
	if !errors.Is(waitErr, context.Canceled) {
		t.Fatalf("Wait error = %v, want context.Canceled", waitErr)
	}
	var exitErr *exec.ExitError
	if !errors.As(waitErr, &exitErr) {
		t.Fatalf("Wait error = %v, want preserved process exit error", waitErr)
	}
	assertHandleSignaled(t, "parent", fixture.parentHandle)
	assertHandleSignaled(t, "grandchild", fixture.grandchildHandle)
}

func TestCommandContextCancelsDescendantAfterParentExits(t *testing.T) {
	fixture := startProcessTreeFixture(t, parentExitsAfterSpawn)
	assertHandleSignaled(t, "parent before cancellation", fixture.parentHandle)
	assertHandleActive(t, "grandchild before cancellation", fixture.grandchildHandle)

	fixture.cancel()
	if err := fixture.wait(); !errors.Is(err, context.Canceled) {
		t.Fatalf("Wait error = %v, want context.Canceled", err)
	}
	assertHandleSignaled(t, "grandchild after cancellation", fixture.grandchildHandle)
}

func TestCommandWaitTerminatesDescendantAfterParentExits(t *testing.T) {
	fixture := startProcessTreeFixture(t, parentExitsAfterSpawn)
	assertHandleSignaled(t, "parent before Wait", fixture.parentHandle)
	assertHandleActive(t, "grandchild before Wait", fixture.grandchildHandle)

	if err := fixture.wait(); err != nil {
		t.Fatalf("Wait: %v", err)
	}
	assertHandleSignaled(t, "grandchild after Wait", fixture.grandchildHandle)
}

func TestProcessTreeHelper(t *testing.T) {
	switch os.Getenv(processTreeHelperMode) {
	case "":
		return
	case parentWaitsForChild, parentExitsAfterSpawn:
		pidFile := os.Getenv(processTreePIDFile)
		command := exec.Command(os.Args[0], "-test.run=^TestProcessTreeHelper$")
		command.Env = append(os.Environ(), processTreeHelperMode+"="+grandchildSleeps)
		if err := command.Start(); err != nil {
			t.Fatalf("start process-tree grandchild: %v", err)
		}
		temporaryPIDFile := pidFile + ".tmp"
		if err := os.WriteFile(temporaryPIDFile, []byte(strconv.Itoa(command.Process.Pid)), 0o600); err != nil {
			_ = command.Process.Kill()
			t.Fatalf("publish process-tree grandchild PID: %v", err)
		}
		if err := os.Rename(temporaryPIDFile, pidFile); err != nil {
			_ = os.Remove(temporaryPIDFile)
			_ = command.Process.Kill()
			t.Fatalf("publish process-tree grandchild PID atomically: %v", err)
		}
		if os.Getenv(processTreeHelperMode) == parentExitsAfterSpawn {
			if err := command.Process.Release(); err != nil {
				t.Fatalf("release process-tree grandchild: %v", err)
			}
			return
		}
		if err := command.Wait(); err != nil {
			t.Fatalf("wait for process-tree grandchild: %v", err)
		}
	case grandchildSleeps:
		time.Sleep(2 * time.Minute)
	default:
		t.Fatalf("unknown process-tree helper mode %q", os.Getenv(processTreeHelperMode))
	}
}

type processTreeFixture struct {
	command          *Command
	cancel           context.CancelFunc
	parentHandle     syscall.Handle
	grandchildHandle syscall.Handle
	waited           bool
}

func startProcessTreeFixture(t *testing.T, parentMode string) *processTreeFixture {
	t.Helper()
	pidFile := filepath.Join(t.TempDir(), "grandchild.pid")
	ctx, cancel := context.WithCancel(t.Context())
	command := CommandContext(ctx, os.Args[0], "-test.run=^TestProcessTreeHelper$")
	command.Env = append(os.Environ(), processTreeHelperMode+"="+parentMode, processTreePIDFile+"="+pidFile)
	if err := command.Start(); err != nil {
		cancel()
		t.Fatalf("start process-tree helper: %v", err)
	}
	fixture := &processTreeFixture{command: command, cancel: cancel}
	t.Cleanup(func() {
		cancel()
		if !fixture.waited {
			_ = command.Terminate()
			_ = command.Wait()
		}
		for _, handle := range []syscall.Handle{fixture.parentHandle, fixture.grandchildHandle} {
			if handle != 0 {
				_ = syscall.CloseHandle(handle)
			}
		}
	})

	var err error
	fixture.parentHandle, err = syscall.OpenProcess(syscall.SYNCHRONIZE, false, uint32(command.Process.Pid))
	if err != nil {
		t.Fatalf("open parent process %d: %v", command.Process.Pid, err)
	}
	grandchildPID := waitForHelperPID(t, pidFile)
	fixture.grandchildHandle, err = syscall.OpenProcess(syscall.SYNCHRONIZE, false, uint32(grandchildPID))
	if err != nil {
		t.Fatalf("open grandchild process %d: %v", grandchildPID, err)
	}
	return fixture
}

func (f *processTreeFixture) wait() error {
	err := f.command.Wait()
	f.waited = true
	return err
}

func assertHandleSignaled(t *testing.T, role string, handle syscall.Handle) {
	t.Helper()
	result, err := syscall.WaitForSingleObject(handle, uint32(commandWaitDelay/time.Millisecond))
	if err != nil {
		t.Fatalf("wait for %s termination: %v", role, err)
	}
	if result != syscall.WAIT_OBJECT_0 {
		t.Fatalf("%s wait result = %#x, want WAIT_OBJECT_0", role, result)
	}
}

func assertHandleActive(t *testing.T, role string, handle syscall.Handle) {
	t.Helper()
	result, err := syscall.WaitForSingleObject(handle, 0)
	if err != nil {
		t.Fatalf("inspect %s state: %v", role, err)
	}
	if result != syscall.WAIT_TIMEOUT {
		t.Fatalf("%s wait result = %#x, want WAIT_TIMEOUT", role, result)
	}
}

func waitForHelperPID(t *testing.T, path string) int {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(path)
		if err == nil {
			pid, parseErr := strconv.Atoi(strings.TrimSpace(string(data)))
			if parseErr != nil || pid <= 0 {
				t.Fatalf("parse process-tree helper PID %q: %v", data, parseErr)
			}
			return pid
		}
		if !os.IsNotExist(err) && !errors.Is(err, windowsSharingViolation) {
			t.Fatalf("read process-tree helper PID: %v", err)
		}
		lastErr = err
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("process-tree helper did not publish a readable PID within 10 seconds: %v", lastErr)
	return 0
}
