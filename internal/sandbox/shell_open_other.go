//go:build !windows

package sandbox

import "errors"

func openShellTarget(string) error {
	return errors.New("opening configuration is supported only on Windows")
}
