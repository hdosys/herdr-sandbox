package sandbox

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFirstUseRejectsBroaderAncestorAndProtectedProfile(t *testing.T) {
	root := t.TempDir()
	for variable, leaf := range map[string]string{"USERPROFILE": "home", "APPDATA": "app", "LOCALAPPDATA": "local"} {
		path := filepath.Join(root, leaf)
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatal(err)
		}
		t.Setenv(variable, path)
	}
	parent := filepath.Join(root, "project")
	child := filepath.Join(parent, "child")
	if err := os.MkdirAll(child, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := InitializeProject(parent, []string{"go"}); err != nil {
		t.Fatal(err)
	}
	if _, err := InspectFirstUse(child); err == nil || !strings.Contains(err.Error(), "broader") {
		t.Fatalf("broader parent selection accepted: %v", err)
	}
	if selected, err := InspectFirstUse(parent); err != nil || selected.NeedsProject {
		t.Fatalf("exact existing profile rejected: %+v %v", selected, err)
	}
	home := os.Getenv("USERPROFILE")
	if _, err := InitializeProject(home, []string{"go"}); err == nil {
		t.Fatal("created a profile in protected home")
	}
	if _, err := os.Stat(filepath.Join(home, projectConfigurationName, projectProvisioningName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("rejected home selection left a profile")
	}
}
