//go:build windows

package sandbox

import (
	"fmt"
	"io"
	"os"
	"syscall"
)

func validateInteractiveAttachStreams(stdin io.Reader, stdout, stderr io.Writer) error {
	streams := []struct {
		name  string
		value any
	}{
		{name: "stdin", value: stdin},
		{name: "stdout", value: stdout},
		{name: "stderr", value: stderr},
	}
	for _, stream := range streams {
		file, ok := stream.value.(*os.File)
		if !ok {
			return fmt.Errorf("automatic Herdr attach requires an interactive Windows terminal: %s is redirected; use herdr --remote sandbox", stream.name)
		}
		var mode uint32
		if err := syscall.GetConsoleMode(syscall.Handle(file.Fd()), &mode); err != nil {
			return fmt.Errorf("automatic Herdr attach requires an interactive Windows terminal: %s is not a console; use herdr --remote sandbox", stream.name)
		}
	}
	return nil
}
