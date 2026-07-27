//go:build !windows

package sandbox

import (
	"os"
	"path/filepath"
	"strings"
)

func fileInfoIsReparsePoint(info os.FileInfo) (bool, error) {
	return info.Mode()&os.ModeSymlink != 0, nil
}

func fileInfoIsDirectory(info os.FileInfo) (bool, error) {
	return info.IsDir(), nil
}

func mappedDirectoryPhysicalIdentity(path string) (string, error) {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	return strings.ToLower(filepath.Clean(resolved)), nil
}

func resolvedDirectoryPath(path string) (string, error) {
	return filepath.EvalSymlinks(path)
}
