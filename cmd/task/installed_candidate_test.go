package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"herdr-sandbox/internal/productidentity"
)

func TestCurrentSandboxFirstInstallSeedsBecomePreservationBaseline(t *testing.T) {
	appData := t.TempDir()
	t.Setenv("APPDATA", appData)

	preserved, err := captureCurrentSandboxUserConfiguration()
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range preserved {
		if file.Exists {
			t.Fatalf("initially absent file was captured as existing: %s", file.Path)
		}
	}

	root := filepath.Join(appData, productidentity.ApplicationName)
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	for name, contents := range map[string]string{
		productidentity.ConfigurationName: "canonical config\n",
		productidentity.UserScriptName:    "canonical user script\n",
	} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	preserved, err = captureCurrentSandboxSeededConfiguration(preserved)
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyCurrentSandboxUserConfiguration(preserved); err != nil {
		t.Fatalf("verify unchanged first-install seeds: %v", err)
	}

	config := filepath.Join(root, productidentity.ConfigurationName)
	if err := os.WriteFile(config, []byte("changed config\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := verifyCurrentSandboxUserConfiguration(preserved); err == nil || !strings.Contains(err.Error(), "changed preserved") {
		t.Fatalf("changed first-install seed error = %v", err)
	}
}

func TestCurrentSandboxFirstInstallRequiresEverySeed(t *testing.T) {
	appData := t.TempDir()
	t.Setenv("APPDATA", appData)
	preserved, err := captureCurrentSandboxUserConfiguration()
	if err != nil {
		t.Fatal(err)
	}

	root := filepath.Join(appData, productidentity.ApplicationName)
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, productidentity.ConfigurationName), []byte("config\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := captureCurrentSandboxSeededConfiguration(preserved); err == nil || !strings.Contains(err.Error(), productidentity.UserScriptName) {
		t.Fatalf("missing first-install seed error = %v", err)
	}
}
