package sandbox

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"herdr-sandbox/internal/installerartifact"
	"herdr-sandbox/internal/productidentity"
)

func TestWorkstationPublicationFailurePreservesPreviousPair(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows read-only replacement boundary")
	}
	for _, blocked := range []string{"setup.json", "setup.exe"} {
		t.Run(blocked, func(t *testing.T) {
			root := t.TempDir()
			staging := t.TempDir()
			canonical, evidence := filepath.Join(root, "setup.exe"), filepath.Join(root, "setup.json")
			next, nextEvidence := filepath.Join(staging, "setup.exe"), filepath.Join(staging, "setup.json")
			for path, data := range map[string]string{canonical: "old installer", evidence: "old provenance", next: "new installer", nextEvidence: "new provenance"} {
				if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			blockedPath := filepath.Join(root, blocked)
			if err := os.Chmod(blockedPath, 0o400); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.Chmod(blockedPath, 0o600) })
			retain, err := publishWorkstationPair(next, nextEvidence, canonical, evidence)
			if err == nil || retain {
				t.Fatalf("unexpected publication outcome: retain=%t err=%v", retain, err)
			}
			for path, want := range map[string]string{canonical: "old installer", evidence: "old provenance"} {
				data, err := os.ReadFile(path)
				if err != nil || string(data) != want {
					t.Fatalf("previous pair lost: %s %v", path, err)
				}
			}
		})
	}
}

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
	plan := runPlan{InstallerPath: filepath.Join(directory, productidentity.WorkstationInstallerName), InputDirectory: t.TempDir()}
	selection := provisioningPlan{TTSBundle: filepath.Join(directory, "host-only-tts.zip"), UserScript: filepath.Join(directory, "absent-user.ps1")}
	if err := prepareInstallerRunInputs(t.Context(), &plan, selection); err != nil {
		t.Fatalf("host TTS selection must not override or block the embedded installer snapshot: %v", err)
	}
	copied, err := os.ReadFile(filepath.Join(plan.InputDirectory, "installer", "setup.exe"))
	if err != nil || string(copied) != string(installer) {
		t.Fatalf("selected installer was not preserved: %v", err)
	}
	if err := os.WriteFile(filepath.Join(directory, "profile.json"), []byte("new settings"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := SavedWorkstationInstaller("MyPC"); err == nil {
		t.Fatal("changed saved profile accepted for old installer")
	}
}
