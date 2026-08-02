//go:build windows

package sandbox

import (
	"errors"
	"fmt"
	"runtime"
	"syscall"
	"unsafe"
)

const (
	coinitApartmentThreaded = 0x2
	coinitDisableOLE1DDE    = 0x4
	rpcEChangedMode         = 0x80010106
	shellNoAssociation      = 31
	swShowNormal            = 1
)

var (
	coInitializeEx = syscall.NewLazyDLL("ole32.dll").NewProc("CoInitializeEx")
	coUninitialize = syscall.NewLazyDLL("ole32.dll").NewProc("CoUninitialize")
	shellExecuteW  = syscall.NewLazyDLL("shell32.dll").NewProc("ShellExecuteW")
)

func openConfigurationFile(path string) error {
	operation, err := syscall.UTF16PtrFromString("open")
	if err != nil {
		return fmt.Errorf("encode Windows Shell open verb: %w", err)
	}
	file, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return fmt.Errorf("encode configuration path for Windows Shell: %w", err)
	}

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	hresult, _, _ := coInitializeEx.Call(0, coinitApartmentThreaded|coinitDisableOLE1DDE)
	hresultCode := uint32(hresult)
	initialized := hresultCode == 0 || hresultCode == 1
	if !initialized && hresultCode != rpcEChangedMode {
		return fmt.Errorf("initialize Windows Shell integration: HRESULT 0x%08X", hresultCode)
	}
	if initialized {
		defer coUninitialize.Call()
	}

	result, _, _ := shellExecuteW.Call(
		0,
		uintptr(unsafe.Pointer(operation)),
		uintptr(unsafe.Pointer(file)),
		0,
		0,
		swShowNormal,
	)
	return shellExecuteResultError(result)
}

func shellExecuteResultError(result uintptr) error {
	if result > 32 {
		return nil
	}
	if result == shellNoAssociation {
		return errors.New("no application is registered to open .json files")
	}
	return fmt.Errorf("Windows Shell open failed with code %d", result)
}
