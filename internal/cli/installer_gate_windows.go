//go:build windows

package cli

import (
	"fmt"
	"runtime"
	"syscall"
	"unsafe"

	"herdr-sandbox/internal/productidentity"
)

const installerLifecycleMutexName = `Global\` + productidentity.ProductGUID + `.InstallerLifecycle`

var (
	installerKernel32     = syscall.NewLazyDLL("kernel32.dll")
	createInstallerMutex  = installerKernel32.NewProc("CreateMutexW")
	releaseInstallerMutex = installerKernel32.NewProc("ReleaseMutex")
)

func acquireInstallerLifecycleGate(args []string) (func(), error) {
	if len(args) > 0 && (args[0] == "__installer-open-configuration" || args[0] == "__installer-seed-configuration" || args[0] == "__installer-clean-uninstall") {
		return func() {}, nil
	}
	runtime.LockOSThread()
	name, err := syscall.UTF16PtrFromString(installerLifecycleMutexName)
	if err != nil {
		runtime.UnlockOSThread()
		return nil, fmt.Errorf("encode installer lifecycle gate: %w", err)
	}
	handle, _, callErr := createInstallerMutex.Call(0, 0, uintptr(unsafe.Pointer(name)))
	if handle == 0 {
		runtime.UnlockOSThread()
		return nil, fmt.Errorf("create installer lifecycle gate: %w", callErr)
	}
	mutex := syscall.Handle(handle)
	result, waitErr := syscall.WaitForSingleObject(mutex, 0)
	switch result {
	case syscall.WAIT_OBJECT_0, syscall.WAIT_ABANDONED:
		return func() {
			_, _, _ = releaseInstallerMutex.Call(handle)
			_ = syscall.CloseHandle(mutex)
			runtime.UnlockOSThread()
		}, nil
	case syscall.WAIT_TIMEOUT:
		_ = syscall.CloseHandle(mutex)
		runtime.UnlockOSThread()
		return nil, fmt.Errorf("setup, uninstall, or another %s command is already using the installed files; wait for it to finish and try again", productidentity.DisplayName)
	default:
		_ = syscall.CloseHandle(mutex)
		runtime.UnlockOSThread()
		return nil, fmt.Errorf("acquire installer lifecycle gate: wait result %d: %w", result, waitErr)
	}
}
