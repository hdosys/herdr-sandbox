package sandbox

import (
	"context"
	"os/exec"

	"herdr-sandbox/internal/hiddenprocess"
)

func hiddenCommand(name string, args ...string) *exec.Cmd {
	command := exec.Command(name, args...)
	hiddenprocess.Configure(command)
	return command
}

func hiddenCommandContext(ctx context.Context, name string, args ...string) *exec.Cmd {
	command := exec.CommandContext(ctx, name, args...)
	hiddenprocess.Configure(command)
	return command
}
