//go:build windows

package sandbox

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

const (
	installerProcessSnapshotFlag           = 0x00000002
	installerProcessTerminateAccess        = 0x00000001
	installerProcessSynchronizeAccess      = 0x00100000
	installerProcessQueryLimitedAccess     = 0x00001000
	installerProcessTerminationExitCode    = 1
	installerProcessWaitObject             = 0x00000000
	installerProcessWaitTimeout            = 0x00000102
	installerProcessWaitFailed             = 0xffffffff
	installerProcessMaximumImagePathLength = 32768
	installerProcessNoMoreFiles            = syscall.Errno(18)
	installerProcessInvalidParameter       = syscall.Errno(87)
)

var (
	installerProcessKernel32                  = syscall.NewLazyDLL("kernel32.dll")
	installerProcessQueryFullProcessImageName = installerProcessKernel32.NewProc("QueryFullProcessImageNameW")
	installerProcessTerminateProcess          = installerProcessKernel32.NewProc("TerminateProcess")
	installerProcessWaitForSingleObject       = installerProcessKernel32.NewProc("WaitForSingleObject")
)

func stopExecutablePeers(ctx context.Context, executable string, currentPID int) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		stopped, err := stopExecutablePeerSnapshot(ctx, executable, currentPID)
		if err != nil {
			return err
		}
		if stopped == 0 {
			return nil
		}
	}
}

func stopExecutablePeerSnapshot(ctx context.Context, executable string, currentPID int) (stopped int, resultErr error) {
	snapshot, err := syscall.CreateToolhelp32Snapshot(installerProcessSnapshotFlag, 0)
	if err != nil {
		return 0, fmt.Errorf("snapshot running processes: %w", err)
	}
	defer func() {
		resultErr = errors.Join(resultErr, closeInstallerProcessHandle("process snapshot", snapshot))
	}()

	entry := syscall.ProcessEntry32{Size: uint32(unsafe.Sizeof(syscall.ProcessEntry32{}))}
	if err := syscall.Process32First(snapshot, &entry); err != nil {
		if errors.Is(err, installerProcessNoMoreFiles) {
			return 0, nil
		}
		return 0, fmt.Errorf("read first process snapshot entry: %w", err)
	}

	targetName := filepath.Base(executable)
	for {
		if err := ctx.Err(); err != nil {
			return stopped, err
		}
		name := syscall.UTF16ToString(entry.ExeFile[:])
		if entry.ProcessID != uint32(currentPID) && strings.EqualFold(name, targetName) {
			matched, err := stopExecutablePeer(ctx, executable, entry.ProcessID)
			if err != nil {
				return stopped, err
			}
			if matched {
				stopped++
			}
		}

		entry.Size = uint32(unsafe.Sizeof(entry))
		if err := syscall.Process32Next(snapshot, &entry); err != nil {
			if errors.Is(err, installerProcessNoMoreFiles) {
				return stopped, nil
			}
			return stopped, fmt.Errorf("read next process snapshot entry: %w", err)
		}
	}
}

func stopExecutablePeer(ctx context.Context, executable string, pid uint32) (matched bool, resultErr error) {
	handle, err := syscall.OpenProcess(
		installerProcessTerminateAccess|installerProcessSynchronizeAccess|installerProcessQueryLimitedAccess,
		false,
		pid,
	)
	if err != nil {
		if errors.Is(err, installerProcessInvalidParameter) {
			return false, nil
		}
		return false, fmt.Errorf("open candidate process %d: %w", pid, err)
	}
	defer func() {
		resultErr = errors.Join(resultErr, closeInstallerProcessHandle(fmt.Sprintf("process %d", pid), handle))
	}()

	image, running, err := installerProcessImage(handle)
	if err != nil {
		return false, fmt.Errorf("inspect candidate process %d: %w", pid, err)
	}
	if !running || !strings.EqualFold(filepath.Clean(image), executable) {
		return false, nil
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}

	terminated, _, callErr := installerProcessTerminateProcess.Call(uintptr(handle), installerProcessTerminationExitCode)
	if terminated == 0 {
		signaled, waitErr := installerProcessSignaled(handle)
		if waitErr == nil && signaled {
			return false, nil
		}
		return false, fmt.Errorf("terminate installed process %d: %w", pid, installerProcessCallError(callErr))
	}
	if err := waitForInstallerProcess(ctx, handle, pid); err != nil {
		return false, err
	}
	return true, nil
}

func installerProcessImage(handle syscall.Handle) (path string, running bool, err error) {
	buffer := make([]uint16, installerProcessMaximumImagePathLength)
	size := uint32(len(buffer))
	result, _, callErr := installerProcessQueryFullProcessImageName.Call(
		uintptr(handle),
		0,
		uintptr(unsafe.Pointer(&buffer[0])),
		uintptr(unsafe.Pointer(&size)),
	)
	if result == 0 {
		signaled, waitErr := installerProcessSignaled(handle)
		if waitErr == nil && signaled {
			return "", false, nil
		}
		return "", false, installerProcessCallError(callErr)
	}
	return syscall.UTF16ToString(buffer[:size]), true, nil
}

func waitForInstallerProcess(ctx context.Context, handle syscall.Handle, pid uint32) error {
	deadline, ok := ctx.Deadline()
	if !ok {
		return errors.New("installer process shutdown requires a deadline")
	}
	remaining := time.Until(deadline)
	if remaining <= 0 {
		return context.DeadlineExceeded
	}
	milliseconds := uint32((remaining + time.Millisecond - 1) / time.Millisecond)
	result, _, callErr := installerProcessWaitForSingleObject.Call(uintptr(handle), uintptr(milliseconds))
	switch result {
	case installerProcessWaitObject:
		return nil
	case installerProcessWaitTimeout:
		return fmt.Errorf("installed process %d did not stop before the installer deadline", pid)
	case installerProcessWaitFailed:
		return fmt.Errorf("wait for installed process %d: %w", pid, installerProcessCallError(callErr))
	default:
		return fmt.Errorf("wait for installed process %d returned unexpected status %d", pid, result)
	}
}

func installerProcessSignaled(handle syscall.Handle) (bool, error) {
	result, _, callErr := installerProcessWaitForSingleObject.Call(uintptr(handle), 0)
	switch result {
	case installerProcessWaitObject:
		return true, nil
	case installerProcessWaitTimeout:
		return false, nil
	case installerProcessWaitFailed:
		return false, installerProcessCallError(callErr)
	default:
		return false, fmt.Errorf("zero-time process wait returned unexpected status %d", result)
	}
}

func closeInstallerProcessHandle(role string, handle syscall.Handle) error {
	if handle == 0 {
		return nil
	}
	if err := syscall.CloseHandle(handle); err != nil {
		return fmt.Errorf("close %s handle: %w", role, err)
	}
	return nil
}

func installerProcessCallError(callErr error) error {
	if errno, ok := callErr.(syscall.Errno); ok && errno == 0 {
		return errors.New("Windows process operation failed without an error code")
	}
	if callErr == nil {
		return errors.New("Windows process operation failed without an error code")
	}
	return callErr
}
