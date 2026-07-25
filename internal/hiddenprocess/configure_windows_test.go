//go:build windows

package hiddenprocess

import (
	"os/exec"
	"testing"
)

func TestConfigureCreatesHiddenProcessTreeConsole(t *testing.T) {
	command := exec.Command("powershell.exe", "-NoProfile")
	Configure(command)
	if command.SysProcAttr == nil || !command.SysProcAttr.HideWindow {
		t.Fatal("hidden process does not set HideWindow")
	}
	if command.SysProcAttr.CreationFlags&createNewConsole == 0 {
		t.Fatalf("creation flags = %#x, want CREATE_NEW_CONSOLE", command.SysProcAttr.CreationFlags)
	}
}
