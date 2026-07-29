//go:build windows

package hiddenprocess

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

const (
	createNewConsole      = 0x00000010
	maximumTaskkillOutput = 4 * 1024
)

// Configure gives a noninteractive process tree one hidden console that descendants inherit.
// A consoleless parent would allow grandchildren such as Go compiler and test processes to
// allocate their own visible consoles.
func Configure(command *exec.Cmd) {
	if strings.EqualFold(filepath.Base(command.Path), "powershell.exe") && !hasWindowStyleArgument(command.Args) {
		arguments := make([]string, 0, len(command.Args)+2)
		arguments = append(arguments, command.Args[0], "-WindowStyle", "Hidden")
		command.Args = append(arguments, command.Args[1:]...)
	}
	configureHiddenConsole(command)
}

// TerminateTree force-terminates one exact process and every child it started.
func TerminateTree(ctx context.Context, process *os.Process) error {
	if ctx == nil {
		return errors.New("process-tree cleanup context is nil")
	}
	if process == nil || process.Pid <= 0 {
		return errors.New("process identity is unavailable")
	}
	windowsDirectory := strings.TrimSpace(os.Getenv("SystemRoot"))
	if !filepath.IsAbs(windowsDirectory) {
		return fmt.Errorf("SystemRoot is not absolute: %q", windowsDirectory)
	}
	taskkill := exec.CommandContext(ctx, filepath.Join(windowsDirectory, "System32", "taskkill.exe"),
		"/PID", strconv.Itoa(process.Pid), "/T", "/F")
	configureHiddenConsole(taskkill)
	output, err := taskkill.CombinedOutput()
	if err != nil {
		detail := strings.TrimSpace(string(output))
		if len(detail) > maximumTaskkillOutput {
			detail = detail[:maximumTaskkillOutput] + " [truncated]"
		}
		return fmt.Errorf("taskkill PID %d process tree: %w: %s", process.Pid, err, detail)
	}
	return nil
}

func configureHiddenConsole(command *exec.Cmd) {
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
