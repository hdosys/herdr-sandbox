package sandbox

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestGuestArchiveStagingPowerShellPreservesReparseTargetAndCleansPhysicalInput(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows PowerShell 5.1 staging regression")
	}
	const directoryName = "configuration-aaaaaaaaaaaaaaaa"

	t.Run("root reparse target", func(t *testing.T) {
		guestRoot := filepath.Join(t.TempDir(), "HerdrSandbox")
		stagingRoot := filepath.Join(guestRoot, "staging")
		transferRoot := filepath.Join(stagingRoot, directoryName)
		outside := filepath.Join(t.TempDir(), "outside")
		if err := os.MkdirAll(stagingRoot, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(outside, 0o700); err != nil {
			t.Fatal(err)
		}
		marker := filepath.Join(outside, "keep.txt")
		if err := os.WriteFile(marker, []byte("keep"), 0o600); err != nil {
			t.Fatal(err)
		}
		createTestDirectoryLink(t, transferRoot, outside)

		script := stagingScriptAtTestRoot(guestRoot, directoryName)
		command := hiddenCommand(mustWindowsPowerShellPath(t), "-NoLogo", "-NoProfile", "-NonInteractive", "-EncodedCommand", encodePowerShell(script))
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("root reparse staging cleanup: %v: %s", err, output)
		}
		if contents, err := os.ReadFile(marker); err != nil || string(contents) != "keep" {
			t.Fatalf("outside target changed: %q, %v", contents, err)
		}
		if info, err := os.Lstat(transferRoot); err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			t.Fatalf("root reparse was not replaced by a physical transfer directory: %v", err)
		}
	})

	t.Run("nested reparse target", func(t *testing.T) {
		guestRoot := filepath.Join(t.TempDir(), "HerdrSandbox")
		transferRoot := filepath.Join(guestRoot, "staging", directoryName)
		outside := filepath.Join(t.TempDir(), "outside")
		if err := os.MkdirAll(outside, 0o700); err != nil {
			t.Fatal(err)
		}
		marker := filepath.Join(outside, "keep.txt")
		if err := os.WriteFile(marker, []byte("keep"), 0o600); err != nil {
			t.Fatal(err)
		}
		outsideLiteral := strings.ReplaceAll(outside, "'", "''")
		script := stagingScriptAtTestRoot(guestRoot, directoryName) + `
New-Item -ItemType Junction -Path (Join-Path $transferRoot 'outside-link') -Target '` + outsideLiteral + `' -ErrorAction Stop | Out-Null
$rejected = $false
try {
    Assert-GuestArchiveTree
} catch {
    if ([string]$_.Exception.Message -notmatch 'reparse point') { throw }
    $rejected = $true
} finally {
    Remove-GuestArchiveStaging
}
if (-not $rejected) { throw 'Nested staging alias was accepted.' }
exit 0`
		command := hiddenCommand(mustWindowsPowerShellPath(t), "-NoLogo", "-NoProfile", "-NonInteractive", "-EncodedCommand", encodePowerShell(script))
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("nested reparse staging cleanup: %v: %s", err, output)
		}
		if contents, err := os.ReadFile(marker); err != nil || string(contents) != "keep" {
			t.Fatalf("outside target changed: %q, %v", contents, err)
		}
		if _, err := os.Lstat(transferRoot); !os.IsNotExist(err) {
			t.Fatalf("credential-bearing transfer root remains after rejection: %v", err)
		}
	})

	t.Run("physical stale input", func(t *testing.T) {
		guestRoot := filepath.Join(t.TempDir(), "HerdrSandbox")
		transferRoot := filepath.Join(guestRoot, "staging", directoryName)
		marker := filepath.Join(transferRoot, "stale.txt")
		if err := os.MkdirAll(transferRoot, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(marker, []byte("stale"), 0o600); err != nil {
			t.Fatal(err)
		}

		script := stagingScriptAtTestRoot(guestRoot, directoryName)
		command := hiddenCommand(mustWindowsPowerShellPath(t), "-NoLogo", "-NoProfile", "-NonInteractive", "-EncodedCommand", encodePowerShell(script))
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("clean physical staging: %v: %s", err, output)
		}
		if _, err := os.Stat(marker); !os.IsNotExist(err) {
			t.Fatalf("stale staging marker remains: %v", err)
		}
		if info, err := os.Stat(transferRoot); err != nil || !info.IsDir() {
			t.Fatalf("fresh transfer root is unavailable: %v", err)
		}
	})
}

func stagingScriptAtTestRoot(root, directoryName string) string {
	root = strings.ReplaceAll(root, "'", "''")
	return strings.ReplaceAll(guestArchiveStagingPowerShell(directoryName, "Test archive"), guestRootDirectory, root)
}
