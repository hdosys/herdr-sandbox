package sandbox

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

func remoteSSHAuthorizedKeysInputPath(inputDirectory string) string {
	return filepath.Join(inputDirectory, remoteSSHAuthorizedKeysFileName)
}

func writeRemoteSSHAuthorizedKeysInput(inputDirectory string, keys []string) error {
	if !filepath.IsAbs(inputDirectory) {
		return fmt.Errorf("remote SSH input directory is not absolute: %q", inputDirectory)
	}
	encoded, err := encodeRemoteSSHAuthorizedKeys(keys)
	if err != nil {
		return err
	}
	path := remoteSSHAuthorizedKeysInputPath(inputDirectory)
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		return fmt.Errorf("write remote SSH authorized-key input: %w", err)
	}
	return nil
}

func readRemoteSSHAuthorizedKeysInput(inputDirectory string) ([]string, error) {
	if !filepath.IsAbs(inputDirectory) {
		return nil, fmt.Errorf("remote SSH input directory is not absolute: %q", inputDirectory)
	}
	path := remoteSSHAuthorizedKeysInputPath(inputDirectory)
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		// Runs created before remote access existed have no remote keys.
		return []string{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("inspect remote SSH authorized-key input: %w", err)
	}
	reparse, err := fileInfoIsReparsePoint(info)
	if err != nil {
		return nil, fmt.Errorf("inspect remote SSH authorized-key input reparse state: %w", err)
	}
	if reparse || !info.Mode().IsRegular() || info.Size() < 0 || info.Size() > maximumRemoteSSHAuthorizedKeys*maximumRemoteSSHAuthorizedKeySize {
		return nil, errors.New("remote SSH authorized-key input must be one bounded regular non-reparse file")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read remote SSH authorized-key input: %w", err)
	}
	keys, err := decodeRemoteSSHAuthorizedKeys(data)
	if err != nil {
		return nil, fmt.Errorf("decode remote SSH authorized-key input: %w", err)
	}
	return keys, nil
}

func sameRemoteSSHAuthorizedKeys(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
