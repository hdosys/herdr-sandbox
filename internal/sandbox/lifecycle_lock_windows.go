//go:build windows

package sandbox

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"syscall"
	"unsafe"
)

var (
	kernel32Lifecycle     = syscall.NewLazyDLL("kernel32.dll")
	createMutexLifecycle  = kernel32Lifecycle.NewProc("CreateMutexW")
	releaseMutexLifecycle = kernel32Lifecycle.NewProc("ReleaseMutex")
)

func acquireLifecycleLock(ctx context.Context) (func() error, error) {
	runtime.LockOSThread()
	name, err := syscall.UTF16PtrFromString(`Local\herdr-sandbox-lifecycle-v1`)
	if err != nil {
		runtime.UnlockOSThread()
		return nil, fmt.Errorf("encode lifecycle mutex name: %w", err)
	}
	handle, _, callErr := createMutexLifecycle.Call(0, 0, uintptr(unsafe.Pointer(name)))
	if handle == 0 {
		runtime.UnlockOSThread()
		return nil, fmt.Errorf("create lifecycle mutex: %w", callErr)
	}
	mutex := syscall.Handle(handle)
	for {
		result, err := syscall.WaitForSingleObject(mutex, 100)
		if err != nil {
			syscall.CloseHandle(mutex)
			runtime.UnlockOSThread()
			return nil, fmt.Errorf("wait for lifecycle mutex: %w", err)
		}
		switch result {
		case syscall.WAIT_OBJECT_0, syscall.WAIT_ABANDONED:
			var once sync.Once
			var releaseErr error
			return func() error {
				once.Do(func() {
					if released, _, err := releaseMutexLifecycle.Call(handle); released == 0 {
						releaseErr = fmt.Errorf("release lifecycle mutex: %w", err)
					}
					if err := syscall.CloseHandle(mutex); err != nil && releaseErr == nil {
						releaseErr = fmt.Errorf("close lifecycle mutex: %w", err)
					}
					runtime.UnlockOSThread()
				})
				return releaseErr
			}, nil
		case syscall.WAIT_TIMEOUT:
			select {
			case <-ctx.Done():
				syscall.CloseHandle(mutex)
				runtime.UnlockOSThread()
				return nil, fmt.Errorf("wait for lifecycle mutex: %w", ctx.Err())
			default:
			}
		default:
			syscall.CloseHandle(mutex)
			runtime.UnlockOSThread()
			return nil, fmt.Errorf("wait for lifecycle mutex: unexpected result %d", result)
		}
	}
}
