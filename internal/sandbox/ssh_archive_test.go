package sandbox

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net"
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
			launcher := sshArchiveTransportProbe(t)
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
	launcher := sshArchiveLauncherBytes(inner)
	command := buildSSHArchiveTransportCommand(strings.Repeat("a", 64), 12345, launcher, time.Minute, "fixture")
	for _, required := range []string{
		`C:\HerdrSandbox\staging`,
		"transport-fixture",
		"$expectedArchiveLength = [long]12345",
		fmt.Sprintf("$expectedLauncherLength = [long]%d", len(launcher)),
		"New-Object byte[] 8192",
		"Remove-Item Env:PSModulePath -ErrorAction SilentlyContinue",
		"HerdrSandbox.ProvisioningProcess]::RunWithInputLease($spec)",
		"'-WindowStyle','Hidden'",
		"$spec.StandardInputFile = $archive",
		"$spec.SeparateErrorOutput = $true",
		"'-File'",
		"Remove-GuestArchiveStaging",
	} {
		if !strings.Contains(command, required) {
			t.Fatalf("SSH archive transport command is missing %q", required)
		}
	}
	if strings.Contains(command, "pwsh.exe") {
		t.Fatal("SSH archive transport starts a second PowerShell 7 process")
	}
	if strings.Contains(command, inner) || strings.Contains(command, "-EncodedCommand") {
		t.Fatal("SSH archive transport embeds the launcher in a process command line")
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

func TestSSHPowerShellFailureWrapperWritesPlainDiagnostics(t *testing.T) {
	requireExternalBoundaryTest(t, "Windows PowerShell 5.1 SSH diagnostics")
	if runtime.GOOS != "windows" {
		t.Skip("Windows PowerShell 5.1 SSH diagnostic regression")
	}
	script := withPlainPowerShellErrors(`$ErrorActionPreference = 'Stop'
throw 'clear remote failure'`)
	command := hiddenCommand(mustWindowsPowerShellPath(t), "-NoLogo", "-NoProfile", "-NonInteractive", "-WindowStyle", "Hidden", "-EncodedCommand", encodePowerShell(script))
	output, err := command.CombinedOutput()
	if err == nil {
		t.Fatal("wrapped PowerShell failure returned success")
	}
	diagnostic := string(output)
	if !strings.Contains(diagnostic, "clear remote failure") {
		t.Fatalf("plain diagnostic is missing: %q", diagnostic)
	}
	for _, forbidden := range []string{"#< CLIXML", "<Objs Version=", "CategoryInfo", "FullyQualifiedErrorId"} {
		if strings.Contains(diagnostic, forbidden) {
			t.Fatalf("PowerShell diagnostic contains %q: %q", forbidden, diagnostic)
		}
	}
}

func TestSSHArchiveTransportCommandsFitWindowsCommandLine(t *testing.T) {
	launchers := map[string]string{
		"configuration": buildDevelopmentConfigurationLauncher(strings.Repeat("a", 64), 12345),
		"reprovision": buildReprovisionLauncher(
			strings.Repeat("a", 64), 12345, 1,
			"20260804-123456-abcdef12", "HerdrSandbox-ExplorerRestart-20260804-123456-abcdef12", false,
		),
		"large launcher": strings.Repeat("# Larger than a Windows command line.\n", 4096),
	}
	for name, launcher := range launchers {
		t.Run(name, func(t *testing.T) {
			command := buildSSHArchiveTransportCommand(strings.Repeat("a", 64), 12345, sshArchiveLauncherBytes(launcher), time.Minute, "fixture")
			t.Logf("launcher: %d bytes; transport command: %d characters", len(launcher), len(command))
			if len(command) > maximumSSHArchiveTransportCommandCharacters {
				t.Fatalf("SSH archive transport command length = %d, maximum = %d", len(command), maximumSSHArchiveTransportCommandCharacters)
			}
		})
	}
}

func sshArchiveTransportProbe(t *testing.T) string {
	t.Helper()
	script, err := os.ReadFile(filepath.Join("testdata", "ssh-archive-probe.ps1"))
	if err != nil {
		t.Fatal(err)
	}
	return string(script)
}

func TestSSHArchiveTransportStreamsLargeLauncherInWindowsPowerShell51(t *testing.T) {
	requireExternalBoundaryTest(t, "Windows PowerShell 5.1 streamed SSH launcher")
	if runtime.GOOS != "windows" {
		t.Skip("Windows process command-line and stdin boundary")
	}
	payload := make([]byte, 1024*1024)
	for index := range payload {
		payload[index] = byte(index)
	}
	digest := fmt.Sprintf("%x", sha256.Sum256(payload))
	largeScript := strings.Repeat("# Launcher growth must not grow the SSH command.\n", 4096) + sshArchiveTransportProbe(t)
	for _, test := range []struct {
		name       string
		script     string
		corrupt    bool
		truncate   bool
		diagnostic string
	}{
		{name: "large UTF-8 launcher and binary archive", script: largeScript},
		{name: "launcher integrity", script: largeScript, corrupt: true, diagnostic: "SSH launcher SHA-256 mismatch"},
		{name: "truncated archive", script: largeScript, truncate: true, diagnostic: "SSH archive transport ended with 1 bytes missing"},
		{name: "launcher failure", script: "throw 'clear remote failure'", diagnostic: "clear remote failure"},
	} {
		t.Run(test.name, func(t *testing.T) {
			guestRoot := filepath.Join(t.TempDir(), "guest staging with spaces")
			launcher := sshArchiveLauncherBytes(test.script)
			command := buildSSHArchiveTransportCommand(digest, len(payload), launcher, 25*time.Second, "fixture")
			command = strings.ReplaceAll(command, guestRootDirectory, strings.ReplaceAll(guestRoot, "'", "''"))
			if test.corrupt {
				launcher[len(launcher)-1] ^= 1
			}
			archive := payload
			if test.truncate {
				archive = archive[:len(archive)-1]
			}
			output, err := runLocalSSHTransport(t, command, launcher, archive, func(closeInput func()) error {
				if test.truncate {
					closeInput()
				}
				return nil
			})
			if test.diagnostic == "" {
				if err != nil {
					t.Fatalf("streamed launcher failed: %v: %s", err, output)
				}
				if got, want := strings.TrimSpace(string(output)), fmt.Sprintf("%d %s", len(payload), digest); got != want {
					t.Fatalf("streamed binary input = %q, want %q", got, want)
				}
			} else if err == nil || !strings.Contains(string(output), test.diagnostic) {
				t.Fatalf("wanted terminal failure %q, got %v: %s", test.diagnostic, err, output)
			}
			if _, err := os.Stat(filepath.Join(guestRoot, "staging", "transport-fixture")); !os.IsNotExist(err) {
				t.Fatalf("transport left staged script or archive: %v", err)
			}
		})
	}
}

func runLocalSSHTransport(t *testing.T, script string, launcher, archive []byte, afterSend func(func()) error) ([]byte, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	defer cancel()
	path := filepath.Join(t.TempDir(), "receiver.ps1")
	if err := os.WriteFile(path, []byte(script), 0o600); err != nil {
		t.Fatal(err)
	}
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	defer writer.Close()
	process := hiddenCommandContext(ctx, mustWindowsPowerShellPath(t), "-NoLogo", "-NoProfile", "-NonInteractive", "-WindowStyle", "Hidden", "-ExecutionPolicy", "Bypass", "-File", path)
	process.Stdin = reader
	var output bytes.Buffer
	process.Stdout = &output
	process.Stderr = &output
	if err := process.Start(); err != nil {
		t.Fatal(err)
	}
	_, sendErr := io.Copy(writer, io.MultiReader(bytes.NewReader(provisioningProcessSource), bytes.NewReader(launcher), bytes.NewReader(archive)))
	if sendErr != nil {
		_ = writer.Close()
	}
	if afterSend != nil {
		if err := afterSend(func() { _ = writer.Close() }); err != nil {
			_ = writer.Close()
			sendErr = err
		}
	}
	waitErr := process.Wait()
	if sendErr != nil && waitErr == nil {
		waitErr = sendErr
	}
	return output.Bytes(), waitErr
}

func TestSSHProvisioningCancellationAndTimeoutPermitRetryInWindowsPowerShell51(t *testing.T) {
	requireExternalBoundaryTest(t, "Windows PowerShell 5.1 cancelled SSH provisioning")
	guestRoot := filepath.Join(t.TempDir(), "guest")
	for _, disconnect := range []bool{true, false} {
		t.Run(fmt.Sprintf("disconnect=%t", disconnect), func(t *testing.T) {
			listener, err := net.ListenTCP("tcp4", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			if err := listener.SetDeadline(time.Now().Add(20 * time.Second)); err != nil {
				t.Fatal(err)
			}
			launcher := sshArchiveLauncherBytes(fmt.Sprintf(`$ErrorActionPreference = 'Stop'
$child = Start-Process -FilePath powershell.exe -WindowStyle Hidden -ArgumentList @('-NoProfile','-NonInteractive','-WindowStyle','Hidden','-Command','Start-Sleep -Seconds 60') -PassThru
$client = New-Object Net.Sockets.TcpClient('127.0.0.1', %d)
$signal = New-Object IO.StreamWriter($client.GetStream())
$signal.WriteLine([string]$child.Id)
$signal.Flush()
$client.Dispose()
Start-Sleep -Seconds 60`, listener.Addr().(*net.TCPAddr).Port))
			budget := 12 * time.Second
			command := buildSSHArchiveTransportCommand(strings.Repeat("a", 64), 1, launcher, budget, "cancel-fixture")
			command = strings.ReplaceAll(command, guestRootDirectory, guestRoot)
			var childID string
			output, err := runLocalSSHTransport(t, command, launcher, []byte{1}, func(closeInput func()) error {
				connection, err := listener.Accept()
				if err != nil {
					return err
				}
				defer connection.Close()
				_ = connection.SetReadDeadline(time.Now().Add(time.Second))
				data, err := io.ReadAll(io.LimitReader(connection, 32))
				childID = strings.TrimSpace(string(data))
				if disconnect {
					closeInput()
				}
				return err
			})
			want := "operation deadline"
			if disconnect {
				want = "cancelled or disconnected"
			}
			if err == nil || !strings.Contains(string(output), want) || childID == "" {
				t.Fatalf("expected %s after child startup, got child=%q err=%v output=%s", want, childID, err, output)
			}
			probe := hiddenCommand(mustWindowsPowerShellPath(t), "-NoProfile", "-NonInteractive", "-WindowStyle", "Hidden", "-Command", "if (Get-Process -Id "+childID+" -ErrorAction SilentlyContinue) { throw 'Owned child survived' }")
			if output, err := probe.CombinedOutput(); err != nil {
				t.Fatalf("owned child was not terminal: %v: %s", err, output)
			}
			retry := sshArchiveLauncherBytes("Write-Output 'retry succeeded'")
			command = buildSSHArchiveTransportCommand(strings.Repeat("a", 64), 1, retry, 20*time.Second, "retry-fixture")
			command = strings.ReplaceAll(command, guestRootDirectory, guestRoot)
			output, err = runLocalSSHTransport(t, command, retry, []byte{1}, nil)
			if err != nil || strings.TrimSpace(string(output)) != "retry succeeded" {
				t.Fatalf("same-guest retry failed: %v: %s", err, output)
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
	return strings.ReplaceAll(guestArchiveStagingPowerShell(directoryName, "Test archive", false), guestRootDirectory, root)
}
