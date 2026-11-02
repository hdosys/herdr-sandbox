package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestLocalInstallerEvidenceBindsSnapshotIdentityAndPayload(t *testing.T) {
	root := t.TempDir()
	version, err := parseReleaseVersion("v0.0.42")
	if err != nil {
		t.Fatal(err)
	}
	paths := releasePaths(root, version)
	paths.Installer = canonicalLocalInstallerPath(root)
	for _, directory := range []string{paths.Stage, filepath.Dir(paths.Installer)} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	for _, file := range releasePackageFiles {
		if err := os.WriteFile(filepath.Join(paths.Stage, file.Name), []byte("payload:"+file.Name), file.Mode); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(paths.Installer, []byte("candidate"), 0o700); err != nil {
		t.Fatal(err)
	}
	identity := buildIdentity{Version: version.Display, Revision: "0123456789abcdef0123456789abcdef01234567", Freshness: "2026.11.02.1200Z"}
	evidence, err := captureLocalInstallerEvidence(identity, paths)
	if err != nil {
		t.Fatal(err)
	}
	metadata := filepath.Join(filepath.Dir(paths.Installer), canonicalLocalEvidenceName)
	writeEvidence := func(value localInstallerEvidence) {
		t.Helper()
		data, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(metadata, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	writeEvidence(evidence)
	actual, err := readLocalInstallerEvidence(root, version)
	if err != nil || actual.Identity != identity {
		t.Fatalf("read original candidate identity: %#v, %v", actual.Identity, err)
	}
	snapshot := filepath.Join(t.TempDir(), "candidate.exe")
	if err := snapshotLocalInstaller(root, snapshot, actual); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.Installer, []byte("replacement"), 0o700); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(snapshot); err != nil || string(data) != "candidate" {
		t.Fatalf("replacement changed the acceptance snapshot: %q, %v", data, err)
	}
	if err := snapshotLocalInstaller(root, filepath.Join(t.TempDir(), "candidate.exe"), actual); err == nil {
		t.Fatal("replaced installer accepted with stale evidence")
	}
	for _, mutate := range []func(*localInstallerEvidence){
		func(value *localInstallerEvidence) { value.Identity.Version = "0.0.41" },
		func(value *localInstallerEvidence) { value.Identity.Revision = "short" },
		func(value *localInstallerEvidence) { value.Identity.Freshness = "invalid" },
		func(value *localInstallerEvidence) { value.InstallerSHA256 = "invalid" },
		func(value *localInstallerEvidence) { value.PayloadSHA256 = map[string]string{} },
	} {
		invalid := evidence
		mutate(&invalid)
		writeEvidence(invalid)
		if _, err := readLocalInstallerEvidence(root, version); err == nil {
			t.Fatal("invalid or incomplete candidate evidence accepted")
		}
	}
}

// Opt-in, read-only artifact acceptance. It never invokes the installer.
func TestCanonicalLocalInstallerEvidence(t *testing.T) {
	versionText := os.Getenv("HERDR_SANDBOX_TEST_LOCAL_INSTALLER_VERSION")
	if versionText == "" {
		t.Skip("explicit canonical installer evidence check")
	}
	version, err := parseReleaseVersion(versionText)
	if err != nil {
		t.Fatal(err)
	}
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)
	evidence, err := readLocalInstallerEvidence(root, version)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateLocalInstallerSource(t.Context(), evidence.Identity.Revision); err != nil {
		t.Fatal(err)
	}
	if err := snapshotLocalInstaller(root, filepath.Join(t.TempDir(), "candidate.exe"), evidence); err != nil {
		t.Fatal(err)
	}
	t.Logf("verified candidate %s, source %s, SHA-256 %s", evidence.Identity.Freshness, evidence.Identity.Revision, evidence.InstallerSHA256)
}
