package sandbox

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

// StopInstallerProcesses is the installer-only owner for terminating other
// processes running from this exact executable path. It never targets the
// Windows Sandbox process or descendants of a matching command.
func StopInstallerProcesses(ctx context.Context) error {
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve installed executable for process shutdown: %w", err)
	}
	if !filepath.IsAbs(executable) {
		return fmt.Errorf("installed executable is not absolute: %q", executable)
	}
	return stopExecutablePeers(ctx, filepath.Clean(executable), os.Getpid())
}
