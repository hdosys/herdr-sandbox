package sandbox

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestConnectionValidationRequiresCompleteAbsoluteIdentity(t *testing.T) {
	root := t.TempDir()
	valid := Connection{
		RunDirectory:        root,
		StatusDirectory:     filepath.Join(root, "status"),
		SSHConfigPath:       filepath.Join(root, "ssh", "config"),
		SSHTarget:           sshTargetName,
		GuestIP:             "172.24.1.2",
		WinGetVersion:       "v1.29.0",
		HerdrVersion:        "herdr 1.0.0",
		HerdrProtocol:       18,
		privateKeyPath:      filepath.Join(root, "identity", "id_ed25519"),
		herdrExecutable:     filepath.Join(root, "herdr.exe"),
		guestHerdrPath:      guestHerdrExecutable,
		herdrRuntimeVersion: "1.0.0",
	}
	if err := valid.validate(); err != nil {
		t.Fatalf("valid connection: %v", err)
	}
	tests := map[string]func(*Connection){
		"run":      func(value *Connection) { value.RunDirectory = "relative" },
		"status":   func(value *Connection) { value.StatusDirectory = "" },
		"config":   func(value *Connection) { value.SSHConfigPath = "config" },
		"key":      func(value *Connection) { value.privateKeyPath = "key" },
		"client":   func(value *Connection) { value.herdrExecutable = "herdr.exe" },
		"guest":    func(value *Connection) { value.guestHerdrPath = "herdr.exe" },
		"runtime":  func(value *Connection) { value.herdrRuntimeVersion = "" },
		"target":   func(value *Connection) { value.SSHTarget = "other" },
		"protocol": func(value *Connection) { value.HerdrProtocol = 0 },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			changed := valid
			mutate(&changed)
			if err := changed.validate(); err == nil || !strings.Contains(err.Error(), "connection") {
				t.Fatalf("validation error = %v", err)
			}
		})
	}
}
