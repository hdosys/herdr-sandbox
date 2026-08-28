package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"herdr-sandbox/internal/productidentity"
)

func TestInstalledCandidateValidationBindsIdentityPayloadAndSourceVersion(t *testing.T) {
	version, err := parseReleaseVersion("v0.0.42")
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	installRoot := filepath.Join(root, "installed")
	stage := filepath.Join(root, "stage")
	for _, directory := range []string{installRoot, stage} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	for _, file := range releasePackageFiles {
		contents := []byte("payload:" + file.Name)
		for _, directory := range []string{installRoot, stage} {
			if err := os.WriteFile(filepath.Join(directory, file.Name), contents, file.Mode); err != nil {
				t.Fatal(err)
			}
		}
	}
	quietHelper := filepath.Join(installRoot, productidentity.QuietUninstallHelperName)
	uninstaller := filepath.Join(installRoot, installerUninstallerName)
	state := installedCandidateState{
		SchemaVersion:        1,
		Installed:            true,
		DisplayName:          productidentity.DisplayName,
		DisplayVersion:       version.Display,
		InstallLocation:      installRoot,
		QuietUninstallString: fmt.Sprintf(`powershell -File "%s" -Uninstaller "%s"`, quietHelper, uninstaller),
	}
	if err := validateInstalledCandidate(state, version, installRoot, stage); err != nil {
		t.Fatalf("validate exact installed candidate: %v", err)
	}
	if err := os.WriteFile(filepath.Join(installRoot, releasePackageFiles[0].Name), []byte("different"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := validateInstalledCandidate(state, version, installRoot, stage); err == nil || !strings.Contains(err.Error(), "differs from staged payload") {
		t.Fatalf("mismatched installed payload error = %v", err)
	}

	revision := "0123456789abcdef0123456789abcdef01234567"
	identity := buildIdentity{Version: version.Display, Revision: revision, Freshness: "2026.08.28.0927Z"}
	got, err := expectedInstalledCandidateVersion(identity)
	if err != nil {
		t.Fatal(err)
	}
	want := fmt.Sprintf("%s %s 2026.08.28.0927Z (0123456789ab)", productidentity.CommandName, version.Display)
	if got != want {
		t.Fatalf("installed version output = %q, want %q", got, want)
	}
	identity.Revision = "short"
	if _, err := expectedInstalledCandidateVersion(identity); err == nil {
		t.Fatal("invalid installed source revision unexpectedly accepted")
	}
}

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
