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
		if found {
			if strings.EqualFold(name, tailscaleAuthKeyEnvironment) {
				continue
			}
			// Win32-OpenSSH serializes its own child descriptor table here.
			// Our processes receive new standard handles, not that ancestor's
			// descriptors. Reusing the table can hang even a nested ssh client.
			if strings.EqualFold(name, "c28fc6f98a2c44abbbd89d6a3037d0d9_POSIX_FD_STATE") {
				continue
			}
		}
		environment = append(environment, entry)
	}
	return environment
}
