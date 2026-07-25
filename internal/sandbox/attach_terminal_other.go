//go:build !windows

package sandbox

import "io"

func validateInteractiveAttachStreams(stdin io.Reader, stdout, stderr io.Writer) error {
	return nil
}
