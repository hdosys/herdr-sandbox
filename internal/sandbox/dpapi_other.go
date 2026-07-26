//go:build !windows

package sandbox

import "errors"

func protectLocalData([]byte) ([]byte, error) {
	return nil, errors.New("Windows DPAPI is unavailable")
}

func unprotectLocalData([]byte) ([]byte, error) {
	return nil, errors.New("Windows DPAPI is unavailable")
}
