//go:build windows

package cli

import (
	"fmt"
	"syscall"
	"unsafe"

	"herdr-sandbox/internal/productidentity"
)

const installerLifecycleAlreadyExists = syscall.Errno(183)

var (
	installerKernel32     = syscall.NewLazyDLL("kernel32.dll")
	createInstallerMutex  = installerKernel32.NewProc("CreateMutexW")
	setInstallerLastError = installerKernel32.NewProc("SetLastError")
)

func acquireInstallerLifecycleGate(args []string) (func(), error) {
	if len(args) > 0 && (args[0] == "__installer-seed-configuration" || args[0] == "__installer-clean-uninstall") {
		return func() {}, nil
	}
	name, err := syscall.UTF16PtrFromString(`Global\` + productidentity.ProductGUID + `.InstallerLifecycle.v2`)
	if err != nil {
		return nil, fmt.Errorf("encode installer lifecycle gate: %w", err)
	}
	setInstallerLastError.Call(0)
	handle, _, callErr := createInstallerMutex.Call(0, 0, uintptr(unsafe.Pointer(name)))
	if handle == 0 {
		return nil, fmt.Errorf("create installer lifecycle gate: %w", callErr)
	}
	mutex := syscall.Handle(handle)
	if callErr == installerLifecycleAlreadyExists {
		_ = syscall.CloseHandle(mutex)
		return nil, fmt.Errorf("setup, uninstall, or another %s command is already using the installed files; wait for it to finish and try again", productidentity.DisplayName)
	}
	return func() { _ = syscall.CloseHandle(mutex) }, nil
}
