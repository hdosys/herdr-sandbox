//go:build windows

package hiddenprocess

import (
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

const createNewConsole = 0x00000010

// Configure gives a noninteractive process tree one hidden console that descendants inherit.
// A consoleless parent would allow grandchildren such as Go compiler and test processes to
// allocate their own visible consoles.
func Configure(command *exec.Cmd) {
	if strings.EqualFold(filepath.Base(command.Path), "powershell.exe") && !hasWindowStyleArgument(command.Args) {
		arguments := make([]string, 0, len(command.Args)+2)
		arguments = append(arguments, command.Args[0], "-WindowStyle", "Hidden")
		command.Args = append(arguments, command.Args[1:]...)
	}
	if command.SysProcAttr == nil {
		command.SysProcAttr = &syscall.SysProcAttr{}
	}
	command.SysProcAttr.HideWindow = true
	command.SysProcAttr.CreationFlags |= createNewConsole
}

func hasWindowStyleArgument(arguments []string) bool {
	for _, argument := range arguments[1:] {
		if strings.EqualFold(argument, "-WindowStyle") {
			return true
		}
	}
	return false
}
