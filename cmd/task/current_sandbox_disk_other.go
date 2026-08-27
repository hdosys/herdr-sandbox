//go:build !windows

package main

import "errors"

func currentSandboxAvailableDiskBytes(string) (uint64, error) {
	return 0, errors.New("querying current-Sandbox disk space requires Windows")
}
