package main

import (
	"context"
	"os/exec"

	"herdr-sandbox/internal/hiddenprocess"
)

func hiddenCommandContext(ctx context.Context, name string, args ...string) *exec.Cmd {
	command := exec.CommandContext(ctx, name, args...)
	hiddenprocess.Configure(command)
	return command
}
