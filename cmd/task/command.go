package main

import (
	"context"

	"herdr-sandbox/internal/hiddenprocess"
)

func hiddenCommandContext(ctx context.Context, name string, args ...string) *hiddenprocess.Command {
	return hiddenprocess.CommandContext(ctx, name, args...)
}
