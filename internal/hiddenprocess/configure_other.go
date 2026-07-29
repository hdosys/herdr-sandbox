//go:build !windows

package hiddenprocess

import (
	"context"
	"os"
	"os/exec"
)

// Configure is a no-op where Windows console creation flags do not exist.
func Configure(_ *exec.Cmd) {}

// TerminateTree terminates the immediate process on non-Windows build hosts.
func TerminateTree(ctx context.Context, process *os.Process) error {
	if ctx == nil || process == nil {
		return os.ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return process.Kill()
}
