//go:build windows

package sandbox

import (
	"errors"
	"fmt"
	"syscall"
	"unsafe"
)

const cryptProtectUIForbidden = 0x1

type dataBlob struct {
	Size uint32
	Data *byte
}

var (
	cryptProtectData   = syscall.NewLazyDLL("crypt32.dll").NewProc("CryptProtectData")
	cryptUnprotectData = syscall.NewLazyDLL("crypt32.dll").NewProc("CryptUnprotectData")
	localFree          = syscall.NewLazyDLL("kernel32.dll").NewProc("LocalFree")
)

func protectLocalData(plaintext []byte) ([]byte, error) {
	if len(plaintext) == 0 || uint64(len(plaintext)) > uint64(^uint32(0)) {
		return nil, errors.New("DPAPI plaintext size is invalid")
	}
	input := dataBlob{Size: uint32(len(plaintext)), Data: &plaintext[0]}
	var output dataBlob
	result, _, callErr := cryptProtectData.Call(
		uintptr(unsafe.Pointer(&input)), 0, 0, 0, 0, cryptProtectUIForbidden, uintptr(unsafe.Pointer(&output)),
	)
	if result == 0 {
		return nil, windowsCallError("CryptProtectData", callErr)
	}
	defer localFree.Call(uintptr(unsafe.Pointer(output.Data)))
	if output.Data == nil || output.Size == 0 {
		return nil, errors.New("CryptProtectData returned an empty payload")
	}
	protected := make([]byte, int(output.Size))
	copy(protected, unsafe.Slice(output.Data, int(output.Size)))
	return protected, nil
}

func unprotectLocalData(protected []byte) ([]byte, error) {
	if len(protected) == 0 || uint64(len(protected)) > uint64(^uint32(0)) {
		return nil, errors.New("DPAPI ciphertext size is invalid")
	}
	input := dataBlob{Size: uint32(len(protected)), Data: &protected[0]}
	var output dataBlob
	result, _, callErr := cryptUnprotectData.Call(
		uintptr(unsafe.Pointer(&input)), 0, 0, 0, 0, cryptProtectUIForbidden, uintptr(unsafe.Pointer(&output)),
	)
	if result == 0 {
		return nil, windowsCallError("CryptUnprotectData", callErr)
	}
	if output.Data == nil || output.Size == 0 {
		if output.Data != nil {
			localFree.Call(uintptr(unsafe.Pointer(output.Data)))
		}
		return nil, errors.New("CryptUnprotectData returned an empty payload")
	}
	nativePlaintext := unsafe.Slice(output.Data, int(output.Size))
	defer func() {
		clear(nativePlaintext)
		localFree.Call(uintptr(unsafe.Pointer(output.Data)))
	}()
	plaintext := make([]byte, int(output.Size))
	copy(plaintext, nativePlaintext)
	return plaintext, nil
}

func windowsCallError(name string, callErr error) error {
	if callErr != nil && callErr != syscall.Errno(0) {
		return fmt.Errorf("%s: %w", name, callErr)
	}
	return fmt.Errorf("%s failed without a Windows error", name)
}
