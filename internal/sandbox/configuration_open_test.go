package sandbox

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenConfigurationSeedsOnlyMissingConfigAndUsesRegisteredApplication(t *testing.T) {
	configurationRoot := t.TempDir()
	expectedPath := filepath.Join(configurationRoot, applicationName, globalConfigurationName)
	opened := ""
	path, err := openConfigurationAt(configurationRoot, func(path string) error {
		opened = path
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("configuration was not created before open: %v", err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if path != expectedPath || opened != expectedPath {
		t.Fatalf("path = %q, opened = %q, want %q", path, opened, expectedPath)
	}
	if _, err := os.Stat(filepath.Join(configurationRoot, applicationName, userProvisioningName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("config command seeded the user provisioning script: %v", err)
	}

	custom := []byte("{\n  \"memoryMB\": 8192\n}\n")
	if err := os.WriteFile(expectedPath, custom, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := openConfigurationAt(configurationRoot, func(string) error { return nil }); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(expectedPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(contents, custom) {
		t.Fatalf("existing configuration was replaced: %q", contents)
	}
}

func TestOpenConfigurationReportsRegisteredApplicationFailure(t *testing.T) {
	configurationRoot := t.TempDir()
	path, err := openConfigurationAt(configurationRoot, func(string) error {
		return errors.New("association fixture")
	})
	expectedPath := filepath.Join(configurationRoot, applicationName, globalConfigurationName)
	if path != expectedPath || err == nil || !strings.Contains(err.Error(), "association fixture") || !strings.Contains(err.Error(), expectedPath) {
		t.Fatalf("path = %q, error = %v", path, err)
	}
}

func TestOpenConfigurationRejectsRelativeConfigurationRoot(t *testing.T) {
	called := false
	if _, err := openConfigurationAt("relative", func(string) error { called = true; return nil }); err == nil || called {
		t.Fatalf("relative root error = %v, open called = %t", err, called)
	}
}
