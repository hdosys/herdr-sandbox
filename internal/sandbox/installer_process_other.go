//go:build !windows

package sandbox

import (
	"context"
	"errors"
)

func stopExecutablePeers(context.Context, string, int) error {
	return errors.New("installer process shutdown is supported only on Windows")
}
