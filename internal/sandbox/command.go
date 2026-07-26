package sandbox

import (
	"context"
	"os"
	"os/exec"
	"strings"

	"herdr-sandbox/internal/hiddenprocess"
)

func hiddenCommand(name string, args ...string) *exec.Cmd {
	command := exec.Command(name, args...)
	hiddenprocess.Configure(command)
	command.Env = childProcessEnvironment(os.Environ())
	return command
}

func hiddenCommandContext(ctx context.Context, name string, args ...string) *exec.Cmd {
	command := exec.CommandContext(ctx, name, args...)
	hiddenprocess.Configure(command)
	command.Env = childProcessEnvironment(os.Environ())
	return command
}

func childProcessEnvironment(parent []string) []string {
	environment := make([]string, 0, len(parent))
	for _, entry := range parent {
		name, _, found := strings.Cut(entry, "=")
		if found && strings.EqualFold(name, tailscaleAuthKeyEnvironment) {
			continue
		}
		environment = append(environment, entry)
	}
	return environment
}
