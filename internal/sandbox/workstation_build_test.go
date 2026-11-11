package sandbox

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"herdr-sandbox/internal/installerartifact"
	"herdr-sandbox/internal/productidentity"
)

func TestWorkstationPreparationExecutesImplicitPackageJobs(t *testing.T) {
	t.Setenv(fastTestsEnvironment, "")
	root := t.TempDir()
	setup := provisioningPowerShellFunctionSetup(t, provisioningPowerShellFunctionSource{
		path: defaultProvisioningPath(t, baseProvisioningName), names: []string{"Get-ProvisioningPackagePrerequisites", "Get-ProvisioningPackageJobs"},
	})
	base := "param($Phase,$PackageCacheDirectory,$ProcessOwnerPath)\n" + setup + `
$script:PreparedPackageMetadata = @{}
function Prepare-ProvisioningWinGet {}
function Install-ProvisioningPackage {
    param($Id,$Version)
    $script:PreparedPackageMetadata[$Id] = @{metadata=@{Id=$Id;Version=$Version;InstallerType='fixture';Scope=''}}
}
`
	for name, data := range map[string][]byte{
		"base.ps1": []byte(base), "stacks.ps1": {},
		"selection.json": []byte(`{"schemaVersion":1,"packages":[],"stacks":["go"],"tools":[]}`),
		"prepare.ps1":    workstationPrepareScript,
	} {
		if err := os.WriteFile(filepath.Join(root, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	resultPath := filepath.Join(root, "result.json")
	command := hiddenCommandContext(ctx, mustWindowsPowerShellPath(t), "-NoProfile", "-NonInteractive", "-WindowStyle", "Hidden", "-File", filepath.Join(root, "prepare.ps1"),
		"-SelectionPath", filepath.Join(root, "selection.json"), "-BasePath", filepath.Join(root, "base.ps1"), "-StackPath", filepath.Join(root, "stacks.ps1"),
		"-ProcessOwnerPath", "not-executed.cs", "-CacheDirectory", filepath.Join(root, "cache"), "-WorkDirectory", root, "-ResultPath", resultPath, "-PackagesOnly")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("actual preparation script: %v %s", err, output)
	}
	data, err := os.ReadFile(resultPath)
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		Packages []struct{ Metadata struct{ ID string } }
	}
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Packages) != 1 || result.Packages[0].Metadata.ID != "Microsoft.VCRedist.2015+.x64" {
		t.Fatal("implicit VC runtime was planned but not prepared")
	}
}

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
	materialized := []byte("owned materialized input fixture")
	if err := os.WriteFile(filepath.Join(directory, "profile.json"), profile, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := CreateWorkstationInstallation(t.Context(), "MyPC", io.Discard); err == nil {
		t.Fatal("existing installation was replaced")
	}
	evidence := installerartifact.Evidence{Product: productidentity.WorkstationApplicationName, ConfigurationSource: "saved-installation", ConfigurationFiles: 1, ProfileSHA256: fmt.Sprintf("%x", sha256.Sum256(profile)), InstallerSHA256: fmt.Sprintf("%x", sha256.Sum256(installer)), Identity: installerartifact.Identity{Version: "0.0.28", Revision: strings.Repeat("a", 40), Freshness: "2026.11.10.1200Z"}, PayloadSHA256: map[string]string{}}
	evidence.DataFileName = "herdr-workstation_setup.data.zip"
	evidence.DataSHA256 = fmt.Sprintf("%x", sha256.Sum256(materialized))
	if err := os.WriteFile(filepath.Join(directory, evidence.DataFileName), materialized, 0o600); err != nil {
		t.Fatal(err)
	}
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
	plan := runPlan{InstallerPath: filepath.Join(directory, productidentity.WorkstationInstallerName), InputDirectory: t.TempDir(), ActiveWorkspace: guestWorkspaceDirectory("project")}
	selection := provisioningPlan{BaseScript: defaultProvisioningPath(t, baseProvisioningName), TTSBundle: filepath.Join(directory, "host-only-tts.zip"), UserScript: filepath.Join(directory, "absent-user.ps1"), Workspaces: []workspacePlan{{Name: "project", HostDirectory: directory, GuestDirectory: guestWorkspaceDirectory("project"), Active: true}}}
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
