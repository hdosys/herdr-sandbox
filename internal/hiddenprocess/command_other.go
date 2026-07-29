//go:build !windows

package hiddenprocess

import (
	"context"
	"errors"
	"os"
	"os/exec"
)

// Command wraps exec.Cmd where Windows Job Object ownership is unavailable.
type Command struct {
	*exec.Cmd
}

// CommandContext creates a normally context-bound command on non-Windows hosts.
func CommandContext(ctx context.Context, name string, args ...string) *Command {
	command := exec.CommandContext(ctx, name, args...)
	command.WaitDelay = commandWaitDelay
	return &Command{Cmd: command}
}

// Start starts the wrapped command.
func (c *Command) Start() error {
	return c.Cmd.Start()
}

// Wait waits for the wrapped command.
func (c *Command) Wait() error {
	return c.Cmd.Wait()
}

// Terminate terminates the immediate process where Job Objects are unavailable.
func (c *Command) Terminate() error {
	if c.Process == nil {
		return errors.New("exec: not started")
	}
	err := c.Process.Kill()
	if errors.Is(err, os.ErrProcessDone) {
		return nil
	}
	return err
}
