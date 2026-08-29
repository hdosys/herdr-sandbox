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
	path, created, err := openConfigurationAt(configurationRoot, func(path string) error {
		opened = path
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("configuration was not created before open: %v", err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if path != expectedPath || !created || opened != expectedPath {
		t.Fatalf("path = %q, created = %t, opened = %q, want created %q", path, created, opened, expectedPath)
	}
	if _, err := os.Stat(filepath.Join(configurationRoot, applicationName, userProvisioningName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("config command seeded the user provisioning script: %v", err)
	}
	for name, expected := range map[string][]byte{
		sampleConfigurationName: sampleGlobalConfiguration,
		configurationSchemaName: globalConfigurationSchema,
	} {
		contents, err := os.ReadFile(filepath.Join(configurationRoot, applicationName, name))
		if err != nil || !bytes.Equal(contents, expected) {
			t.Fatalf("configuration reference %s = %q, %v", name, contents, err)
		}
	}

	custom := []byte("{\n  \"memoryMB\": 8192\n}\n")
	if err := os.WriteFile(expectedPath, custom, 0o600); err != nil {
		t.Fatal(err)
	}
	_, created, err = openConfigurationAt(configurationRoot, func(string) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	if created {
		t.Fatal("existing configuration was reported as created")
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
	path, created, err := openConfigurationAt(configurationRoot, func(string) error {
		return errors.New("association fixture")
	})
	expectedPath := filepath.Join(configurationRoot, applicationName, globalConfigurationName)
	if path != expectedPath || !created || err == nil || !strings.Contains(err.Error(), "association fixture") || !strings.Contains(err.Error(), expectedPath) {
		t.Fatalf("path = %q, created = %t, error = %v", path, created, err)
	}
}

func TestOpenConfigurationRejectsRelativeConfigurationRoot(t *testing.T) {
	called := false
	if _, _, err := openConfigurationAt("relative", func(string) error { called = true; return nil }); err == nil || called {
		t.Fatalf("relative root error = %v, open called = %t", err, called)
	}
}
