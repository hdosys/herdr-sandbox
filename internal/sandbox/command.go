package sandbox

import (
	"context"
	"os"
	"strings"

	"herdr-sandbox/internal/hiddenprocess"
)

func hiddenCommandContext(ctx context.Context, name string, args ...string) *hiddenprocess.Command {
	command := hiddenprocess.CommandContext(ctx, name, args...)
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
