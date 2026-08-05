//go:build windows

package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
	if err := rejectPendingInstallerTransaction(); err != nil {
		_ = syscall.CloseHandle(mutex)
		return nil, err
	}
	return func() { _ = syscall.CloseHandle(mutex) }, nil
}

func rejectPendingInstallerTransaction() error {
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve executable for installer transaction check: %w", err)
	}
	localAppData, err := os.UserCacheDir()
	if err != nil {
		return fmt.Errorf("resolve local application data for installer transaction check: %w", err)
	}
	return rejectPendingInstallerTransactionAt(executable, localAppData)
}

func rejectPendingInstallerTransactionAt(executable, localAppData string) error {
	installDirectory := filepath.Join(localAppData, "Programs", productidentity.InstallDirectoryName)
	if !strings.EqualFold(filepath.Clean(filepath.Dir(executable)), filepath.Clean(installDirectory)) {
		return nil
	}
	transactionDirectory := filepath.Join(filepath.Dir(installDirectory), "."+productidentity.ApplicationName+"-installer-transaction")
	if _, err := os.Lstat(transactionDirectory); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return fmt.Errorf("inspect pending installer transaction: %w", err)
	}
	return fmt.Errorf("a pending %s installer transaction must be recovered; run setup or uninstall again before using the installed command", productidentity.DisplayName)
}
