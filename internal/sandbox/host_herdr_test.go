package sandbox

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnsurePinnedHostHerdrArchiveRejectsReparseDownloadRootBeforeNetwork(t *testing.T) {
	dataDirectory := t.TempDir()
	outside := t.TempDir()
	marker := filepath.Join(outside, "keep.txt")
	if err := os.WriteFile(marker, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	createTestDirectoryLink(t, filepath.Join(dataDirectory, "downloads"), outside)
	release := herdrRelease{ArchiveURL: "https://invalid.example/herdr.zip", ArchiveSHA256: strings.Repeat("a", 64)}
	if _, err := ensurePinnedHostHerdrArchive(context.Background(), release, dataDirectory, io.Discard); err == nil || !strings.Contains(strings.ToLower(err.Error()), "reparse point") {
		t.Fatalf("reparse download root error = %v", err)
	}
	if contents, err := os.ReadFile(marker); err != nil || string(contents) != "keep" {
		t.Fatalf("outside download target changed: %q, %v", contents, err)
	}
}

func TestExtractPinnedHostHerdrSelectsOneBoundedExecutable(t *testing.T) {
	root := t.TempDir()
	archivePath := filepath.Join(root, "herdr.zip")
	file, err := os.Create(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	archive := zip.NewWriter(file)
	writer, err := archive.Create("release/herdr.exe")
	if err != nil {
		t.Fatal(err)
	}
	expected := []byte("fixture executable")
	if _, err := writer.Write(expected); err != nil {
		t.Fatal(err)
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	extracted, err := extractPinnedHostHerdr(archivePath, root)
	if err != nil {
		t.Fatalf("extractPinnedHostHerdr: %v", err)
	}
	defer os.Remove(extracted)
	actual, err := os.ReadFile(extracted)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(actual, expected) {
		t.Fatalf("extracted = %q", actual)
	}
}

func TestExtractPinnedHostHerdrRejectsDuplicateExecutables(t *testing.T) {
	root := t.TempDir()
	archivePath := filepath.Join(root, "herdr.zip")
	file, err := os.Create(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	archive := zip.NewWriter(file)
	for _, name := range []string{"a/herdr.exe", "b/HERDR.EXE"} {
		writer, createErr := archive.Create(name)
		if createErr != nil {
			t.Fatal(createErr)
		}
		if _, writeErr := writer.Write([]byte(name)); writeErr != nil {
			t.Fatal(writeErr)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := extractPinnedHostHerdr(archivePath, root); err == nil {
		t.Fatal("duplicate herdr.exe entries unexpectedly succeeded")
	}
}

func TestCleanupReplacedHostHerdrBackupRemovesExactPath(t *testing.T) {
	backup := filepath.Join(t.TempDir(), ".herdr-sandbox-previous-fixture")
	if err := os.WriteFile(backup, []byte("fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := cleanupReplacedHostHerdrBackup(backup); err != nil {
		t.Fatalf("cleanupReplacedHostHerdrBackup: %v", err)
	}
	if _, err := os.Stat(backup); !os.IsNotExist(err) {
		t.Fatalf("backup still exists: %v", err)
	}
}

func TestReplaceFileAtomicallyPreservesTargetAndBackup(t *testing.T) {
	directory := t.TempDir()
	target := filepath.Join(directory, "herdr.exe")
	replacement := filepath.Join(directory, "herdr-new.exe")
	backup := filepath.Join(directory, "herdr-previous.exe")
	failedReplacement := filepath.Join(directory, "herdr-failed.exe")
	if err := os.WriteFile(target, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(replacement, []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := replaceFileAtomically(target, replacement, backup); err != nil {
		t.Fatalf("replaceFileAtomically: %v", err)
	}
	assertTestFileContents(t, target, "new")
	assertTestFileContents(t, backup, "old")
	if err := replaceFileAtomically(target, backup, failedReplacement); err != nil {
		t.Fatalf("replaceFileAtomically rollback: %v", err)
	}
	assertTestFileContents(t, target, "old")
	assertTestFileContents(t, failedReplacement, "new")
}

func TestRecoverFailedAtomicReplacementUsesDiscoverableBackup(t *testing.T) {
	directory := t.TempDir()
	target := filepath.Join(directory, "herdr.exe")
	replacement := filepath.Join(directory, "herdr-new.exe")
	backup := filepath.Join(directory, "herdr.exe.herdr-sandbox-atomic-previous")
	if err := os.WriteFile(replacement, []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(backup, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := recoverFailedAtomicReplacement(target, replacement, backup); err != nil {
		t.Fatalf("recoverFailedAtomicReplacement: %v", err)
	}
	assertTestFileContents(t, target, "old")
}

func TestResolveInstalledHostHerdrRecoversAtomicBackupBeforePATHLookup(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	localAppData := filepath.Join(root, "local")
	releaseDirectory := filepath.Join(home, ".herdr", "packages", "standalone", "releases", "release-fixture")
	standardDirectory := filepath.Join(localAppData, "Programs", "Herdr", "bin")
	if err := os.MkdirAll(releaseDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(standardDirectory), 0o700); err != nil {
		t.Fatal(err)
	}
	createTestDirectoryLink(t, standardDirectory, releaseDirectory)
	target := filepath.Join(releaseDirectory, "herdr.exe")
	backup := target + ".herdr-sandbox-atomic-previous"
	if err := os.WriteFile(backup, []byte("recoverable"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("LOCALAPPDATA", localAppData)
	t.Setenv("PATH", standardDirectory)
	resolved, err := resolveInstalledHostHerdrPath()
	if err != nil {
		t.Fatalf("resolveInstalledHostHerdrPath: %v", err)
	}
	canonicalReleaseDirectory, err := canonicalMappedDirectory(releaseDirectory)
	if err != nil {
		t.Fatalf("canonicalize expected release directory: %v", err)
	}
	expected := filepath.Join(canonicalReleaseDirectory, "herdr.exe")
	if !strings.EqualFold(resolved, expected) {
		t.Fatalf("resolved path = %q, expected %q", resolved, expected)
	}
	assertTestFileContents(t, target, "recoverable")
	if _, err := os.Stat(backup); !os.IsNotExist(err) {
		t.Fatalf("atomic backup still exists: %v", err)
	}
}

func TestResolveHostHerdrTargetDirectoryRejectsReparseReleasesRoot(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	localAppData := filepath.Join(root, "local")
	externalReleases := filepath.Join(root, "external-releases")
	releaseDirectory := filepath.Join(externalReleases, "release-fixture")
	releasesRoot := filepath.Join(home, ".herdr", "packages", "standalone", "releases")
	standardDirectory := filepath.Join(localAppData, "Programs", "Herdr", "bin")
	if err := os.MkdirAll(releaseDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, parent := range []string{filepath.Dir(releasesRoot), filepath.Dir(standardDirectory)} {
		if err := os.MkdirAll(parent, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	createTestDirectoryLink(t, releasesRoot, externalReleases)
	createTestDirectoryLink(t, standardDirectory, releaseDirectory)
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("LOCALAPPDATA", localAppData)
	if _, err := resolveHostHerdrTargetDirectory(standardDirectory); err == nil || !strings.Contains(err.Error(), "reparse point") {
		t.Fatalf("reparse-bearing releases root error = %v", err)
	}
}

func TestResolveHostHerdrTargetDirectoryAcceptsCustomBinWithoutStandardInstall(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	localAppData := filepath.Join(root, "local-without-standard-herdr")
	customDirectory := filepath.Join(root, "custom", "bin")
	if err := os.MkdirAll(customDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("LOCALAPPDATA", localAppData)
	resolved, err := resolveHostHerdrTargetDirectory(customDirectory)
	if err != nil {
		t.Fatalf("resolve custom host Herdr directory: %v", err)
	}
	expected, err := canonicalMappedDirectory(customDirectory)
	if err != nil {
		t.Fatalf("canonicalize custom host Herdr directory: %v", err)
	}
	if !strings.EqualFold(resolved, expected) {
		t.Fatalf("resolved custom directory = %q, expected %q", resolved, expected)
	}
}

func assertTestFileContents(t *testing.T, path, expected string) {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(contents) != expected {
		t.Fatalf("%s contents = %q, expected %q", filepath.Base(path), contents, expected)
	}
}
