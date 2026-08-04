package sandbox

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestNativeSSHArchiveTransportHandlesLargeInput(t *testing.T) {
	runDirectory := strings.TrimSpace(os.Getenv("HERDR_SANDBOX_NATIVE_RUN_DIRECTORY"))
	if runDirectory == "" {
		t.Skip("set HERDR_SANDBOX_NATIVE_RUN_DIRECTORY for live guest SSH transport verification")
	}
	if !filepath.IsAbs(runDirectory) {
		t.Fatalf("native run directory is not absolute: %q", runDirectory)
	}
	connection := Connection{
		RunDirectory:  runDirectory,
		SSHConfigPath: filepath.Join(runDirectory, ".ssh", "config"),
		SSHTarget:     sshTargetName,
	}
	for _, size := range []int{1024 * 1024, 8 * 1024 * 1024, 32 * 1024 * 1024} {
		t.Run(fmt.Sprintf("%d-bytes", size), func(t *testing.T) {
			payload := make([]byte, size)
			for index := range payload {
				payload[index] = byte(index)
			}
			expectedDigest := fmt.Sprintf("%x", sha256.Sum256(payload))
			launcher := fmt.Sprintf(`$ErrorActionPreference = 'Stop'
[Console]::Error.WriteLine('[ssh-transport] receive-input')
$expectedLength = [long]%d
$inputStream = [Console]::OpenStandardInput()
$outputStream = New-Object IO.MemoryStream
try {
    $remaining = $expectedLength
    $buffer = New-Object byte[] 8192
    while ($remaining -gt 0) {
		$requested = [int][Math]::Min([long]$buffer.Length, $remaining)
		$read = $inputStream.Read($buffer, 0, $requested)
		if ($read -le 0) { throw "Input ended with $remaining bytes missing." }
		$outputStream.Write($buffer, 0, $read)
		$remaining -= $read
    }
    $data = $outputStream.ToArray()
    $sha256 = [Security.Cryptography.SHA256]::Create()
    try {
        $digest = ([BitConverter]::ToString($sha256.ComputeHash($data))).Replace('-', '').ToLowerInvariant()
    } finally {
        $sha256.Dispose()
    }
    [Console]::Out.WriteLine(('{0} {1}' -f $data.Length, $digest))
} finally {
    $outputStream.Dispose()
}
exit 0`, size)
			ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
			defer cancel()
			output, err := runSSHArchivePowerShell(ctx, connection, payload, launcher, "verify SSH archive transport")
			if err != nil {
				t.Fatal(err)
			}
			if got, want := strings.TrimSpace(string(output)), fmt.Sprintf("%d %s", size, expectedDigest); got != want {
				t.Fatalf("SSH binary input result = %q, want %q", got, want)
			}
		})
	}
}

func TestSSHArchiveTransportUsesDefaultShellReceiverAndHiddenWindowsPowerShell(t *testing.T) {
	inner := "Write-Output 'verified'"
	command := buildSSHArchiveTransportCommand(strings.Repeat("a", 64), 12345, inner)
	for _, required := range []string{
		`C:\HerdrSandbox\staging`,
		"transport-aaaaaaaaaaaaaaaa",
		"[ssh-transport] receive-archive",
		"$expectedTransportLength = [long]12345",
		"New-Object byte[] 8192",
		"Remove-Item Env:PSModulePath -ErrorAction SilentlyContinue",
		"Start-Process -FilePath 'powershell.exe'",
		"'-WindowStyle','Hidden'",
		"-RedirectStandardInput $archive",
		"-NoNewWindow -Wait -PassThru",
		encodePowerShell(inner),
		"Remove-GuestArchiveStaging",
	} {
		if !strings.Contains(command, required) {
			t.Fatalf("SSH archive transport command is missing %q", required)
		}
	}
	if strings.Contains(command, "pwsh.exe") {
		t.Fatal("SSH archive transport starts a second PowerShell 7 process")
	}
	if len(command) > maximumSSHArchiveTransportCommandCharacters {
		t.Fatalf("SSH archive transport command length = %d, maximum = %d", len(command), maximumSSHArchiveTransportCommandCharacters)
	}
	if runtime.GOOS == "windows" {
		parserScript := fmt.Sprintf(`$tokens = $null
$errors = $null
[void][Management.Automation.Language.Parser]::ParseInput('%s', [ref]$tokens, [ref]$errors)
if ($errors.Count -ne 0) { throw ($errors | ForEach-Object { $_.ToString() } | Out-String) }
`, strings.ReplaceAll(command, "'", "''"))
		parser := hiddenCommand(mustWindowsPowerShellPath(t), "-NoLogo", "-NoProfile", "-NonInteractive", "-EncodedCommand", encodePowerShell(parserScript))
		if output, err := parser.CombinedOutput(); err != nil {
			t.Fatalf("SSH archive transport PowerShell parse: %v: %s", err, output)
		}
	}
}

func TestSSHArchiveTransportCommandsFitWindowsCommandLine(t *testing.T) {
	launchers := map[string]string{
		"configuration": buildDevelopmentConfigurationLauncher(strings.Repeat("a", 64), 12345),
		"reprovision": buildReprovisionLauncher(
			strings.Repeat("a", 64), 12345, 1,
			"20260804-123456-abcdef12", "HerdrSandbox-ExplorerRestart-20260804-123456-abcdef12",
		),
	}
	for name, launcher := range launchers {
		t.Run(name, func(t *testing.T) {
			command := buildSSHArchiveTransportCommand(strings.Repeat("a", 64), 12345, launcher)
			if len(command) > maximumSSHArchiveTransportCommandCharacters {
				t.Fatalf("SSH archive transport command length = %d, maximum = %d", len(command), maximumSSHArchiveTransportCommandCharacters)
			}
		})
	}
}

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
