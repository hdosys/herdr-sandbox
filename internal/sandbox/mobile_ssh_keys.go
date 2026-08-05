package sandbox

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

func mobileSSHAuthorizedKeysInputPath(inputDirectory string) string {
	return filepath.Join(inputDirectory, mobileSSHAuthorizedKeysFileName)
}

func writeMobileSSHAuthorizedKeysInput(inputDirectory string, keys []string) error {
	if !filepath.IsAbs(inputDirectory) {
		return fmt.Errorf("mobile SSH input directory is not absolute: %q", inputDirectory)
	}
	encoded, err := encodeMobileSSHAuthorizedKeys(keys)
	if err != nil {
		return err
	}
	path := mobileSSHAuthorizedKeysInputPath(inputDirectory)
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		return fmt.Errorf("write mobile SSH authorized-key input: %w", err)
	}
	return nil
}

func readMobileSSHAuthorizedKeysInput(inputDirectory string) ([]string, error) {
	if !filepath.IsAbs(inputDirectory) {
		return nil, fmt.Errorf("mobile SSH input directory is not absolute: %q", inputDirectory)
	}
	path := mobileSSHAuthorizedKeysInputPath(inputDirectory)
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		// Runs created before mobile access existed have no mobile keys.
		return []string{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("inspect mobile SSH authorized-key input: %w", err)
	}
	reparse, err := fileInfoIsReparsePoint(info)
	if err != nil {
		return nil, fmt.Errorf("inspect mobile SSH authorized-key input reparse state: %w", err)
	}
	if reparse || !info.Mode().IsRegular() || info.Size() < 0 || info.Size() > maximumMobileSSHAuthorizedKeys*maximumMobileSSHAuthorizedKeySize {
		return nil, errors.New("mobile SSH authorized-key input must be one bounded regular non-reparse file")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read mobile SSH authorized-key input: %w", err)
	}
	keys, err := decodeMobileSSHAuthorizedKeys(data)
	if err != nil {
		return nil, fmt.Errorf("decode mobile SSH authorized-key input: %w", err)
	}
	return keys, nil
}

func sameMobileSSHAuthorizedKeys(left, right []string) bool {
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
