//go:build windows

package sandbox

import "syscall"

func expectedWindowsSandboxCommandLine(executable, configPath string) string {
	return syscall.EscapeArg(executable) + " " + syscall.EscapeArg(configPath)
}
