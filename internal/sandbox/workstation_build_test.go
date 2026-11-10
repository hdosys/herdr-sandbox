package sandbox

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"herdr-sandbox/internal/installerartifact"
	"herdr-sandbox/internal/productidentity"
)

func TestSavedWorkstationPreservesExistingInstallationAndRejectsDrift(t *testing.T) {
	t.Setenv("LOCALAPPDATA", t.TempDir())
	for _, name := range []string{"", "..", "../escape", `C:\escape`, "a/b", "a b"} {
		if _, err := WorkstationInstallationDirectory(name); err == nil {
			t.Fatalf("unsafe installation name accepted: %q", name)
		}
	}
	directory, err := WorkstationInstallationDirectory("MyPC")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	profile := []byte("saved private fixture")
	installer := []byte("owned installer fixture")
	if err := os.WriteFile(filepath.Join(directory, "profile.json"), profile, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := CreateWorkstationInstallation(t.Context(), "MyPC", io.Discard); err == nil {
		t.Fatal("existing installation was replaced")
	}
	evidence := installerartifact.Evidence{Product: productidentity.WorkstationApplicationName, ConfigurationSource: "saved-installation", ConfigurationFiles: 1, ProfileSHA256: fmt.Sprintf("%x", sha256.Sum256(profile)), InstallerSHA256: fmt.Sprintf("%x", sha256.Sum256(installer)), Identity: installerartifact.Identity{Version: "0.0.28", Revision: strings.Repeat("a", 40), Freshness: "2026.11.10.1200Z"}, PayloadSHA256: map[string]string{}}
	for _, name := range []string{productidentity.ExecutableName, productidentity.BaseScriptName, productidentity.StackScriptName, productidentity.LicenseName} {
		evidence.PayloadSHA256[name] = strings.Repeat("b", 64)
	}
	data, err := json.Marshal(evidence)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, productidentity.WorkstationEvidenceName), data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, productidentity.WorkstationInstallerName), installer, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := SavedWorkstationInstaller("MyPC"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "profile.json"), []byte("new settings"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := SavedWorkstationInstaller("MyPC"); err == nil {
		t.Fatal("changed saved profile accepted for old installer")
	}
}
