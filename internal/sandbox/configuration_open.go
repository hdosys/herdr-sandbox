package sandbox

import (
	"fmt"
	"os"
	"path/filepath"
)

// OpenConfiguration creates the user-owned global configuration when absent,
// then asks the operating system to open it with the registered application.
func OpenConfiguration() (string, bool, error) {
	configurationRoot, err := os.UserConfigDir()
	if err != nil {
		return "", false, fmt.Errorf("resolve user configuration directory: %w", err)
	}
	return openConfigurationAt(configurationRoot, openConfigurationFile)
}

func openConfigurationAt(configurationRoot string, openFile func(string) error) (string, bool, error) {
	if !filepath.IsAbs(configurationRoot) {
		return "", false, fmt.Errorf("user configuration directory is not absolute: %q", configurationRoot)
	}
	globalRoot := filepath.Join(configurationRoot, applicationName)
	if err := os.MkdirAll(globalRoot, 0o700); err != nil {
		return "", false, fmt.Errorf("create global herdr-sandbox directory: %w", err)
	}
	created, err := ensureGlobalWorkspaceConfigResult(globalRoot)
	if err != nil {
		return "", false, err
	}
	path := filepath.Join(globalRoot, globalConfigurationName)
	if err := openFile(path); err != nil {
		return path, created, fmt.Errorf("open configuration %s with its registered application: %w", path, err)
	}
	return path, created, nil
}
