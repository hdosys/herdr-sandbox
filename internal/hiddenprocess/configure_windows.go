//go:build windows

package hiddenprocess

import (
	"os/exec"
	"syscall"
)

const createNewConsole = 0x00000010

// Configure gives a noninteractive process tree one hidden console that descendants inherit.
// A consoleless parent would allow grandchildren such as Go compiler and test processes to
// allocate their own visible consoles.
func Configure(command *exec.Cmd) {
	if command.SysProcAttr == nil {
		command.SysProcAttr = &syscall.SysProcAttr{}
	}
	command.SysProcAttr.HideWindow = true
	command.SysProcAttr.CreationFlags |= createNewConsole
}
