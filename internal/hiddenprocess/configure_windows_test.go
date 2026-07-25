//go:build windows

package hiddenprocess

import (
	"os/exec"
	"slices"
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
	if !slices.Equal(command.Args[1:3], []string{"-WindowStyle", "Hidden"}) {
		t.Fatalf("PowerShell startup arguments = %#v, want hidden window style first", command.Args)
	}
}

func TestConfigureDoesNotDuplicatePowerShellWindowStyle(t *testing.T) {
	command := exec.Command("powershell.exe", "-WindowStyle", "Hidden", "-NoProfile")
	Configure(command)
	if !slices.Equal(command.Args, []string{"powershell.exe", "-WindowStyle", "Hidden", "-NoProfile"}) {
		t.Fatalf("PowerShell startup arguments = %#v", command.Args)
	}

	other := exec.Command("go.exe", "version")
	Configure(other)
	if !slices.Equal(other.Args, []string{"go.exe", "version"}) {
		t.Fatalf("non-PowerShell arguments changed: %#v", other.Args)
	}
}
