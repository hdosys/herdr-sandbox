//go:build !windows

package sandbox

import "os"

func openStatusFile(path string) (*os.File, error) {
	return os.Open(path)
}
