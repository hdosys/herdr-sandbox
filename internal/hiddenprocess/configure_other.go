//go:build !windows

package hiddenprocess

import "os/exec"

// Configure is a no-op where Windows console creation flags do not exist.
func Configure(_ *exec.Cmd) {}
