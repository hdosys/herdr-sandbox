package sandbox

import (
	"bytes"
	"context"
	"crypto/sha256"
	_ "embed"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
)

const (
	maximumSSHResultBytes                       = 1024 * 1024
	maximumSSHErrorBytes                        = 64 * 1024
	maximumSSHArchiveTransportCommandCharacters = 30000
)

//go:embed assets/ssh-archive-staging.ps1
var sshArchiveStagingPowerShell string

func guestArchiveStagingPowerShell(directoryName, role string) string {
	quote := func(value string) string { return strings.ReplaceAll(value, "'", "''") }
	return fmt.Sprintf(`$stagingRoot = '%s\staging'
$transferRoot = Join-Path $stagingRoot '%s'
$archive = Join-Path $transferRoot 'input.zip'
$expanded = Join-Path $transferRoot 'expanded'
$stagingRole = '%s'
%s`, quote(guestRootDirectory), quote(directoryName), quote(role), sshArchiveStagingPowerShell)
}

func runSSHArchivePowerShell(ctx context.Context, connection Connection, archive []byte, launcherScript, role string) ([]byte, error) {
	return runSSHArchivePowerShellWithDiagnostics(ctx, connection, archive, launcherScript, role, true)
}

func runSecretSSHArchivePowerShell(ctx context.Context, connection Connection, archive []byte, launcherScript, role string) ([]byte, error) {
	return runSSHArchivePowerShellWithDiagnostics(ctx, connection, archive, launcherScript, role, false)
}

func runSSHArchivePowerShellWithDiagnostics(ctx context.Context, connection Connection, archive []byte, launcherScript, role string, includeRemoteDiagnostics bool) ([]byte, error) {
	if len(archive) == 0 {
		return nil, fmt.Errorf("%s archive is empty", role)
	}
	digest := fmt.Sprintf("%x", sha256.Sum256(archive))
	transportCommand := buildSSHArchiveTransportCommand(digest, len(archive), launcherScript)
	if len(transportCommand) > maximumSSHArchiveTransportCommandCharacters {
		return nil, fmt.Errorf("%s SSH transport command exceeds %d characters", role, maximumSSHArchiveTransportCommandCharacters)
	}
	return runSSHRemoteCommandWithDiagnostics(ctx, connection, bytes.NewReader(archive), []string{transportCommand}, role, maximumSSHResultBytes, includeRemoteDiagnostics)
}

func buildSSHArchiveTransportCommand(expectedDigest string, expectedArchiveLength int, launcherScript string) string {
	staging := guestArchiveStagingPowerShell("transport-"+expectedDigest[:16], "SSH archive transport")
	return fmt.Sprintf(`$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'
%s
$expectedTransportLength = [long]%d
try {
    $inputStream = [Console]::OpenStandardInput()
    $outputStream = [IO.File]::Open($archive, [IO.FileMode]::CreateNew, [IO.FileAccess]::Write, [IO.FileShare]::None)
    try {
        $remaining = $expectedTransportLength
        $buffer = New-Object byte[] 8192
        while ($remaining -gt 0) {
            $requested = [int][Math]::Min([long]$buffer.Length, $remaining)
            $read = $inputStream.Read($buffer, 0, $requested)
            if ($read -le 0) { throw "SSH archive transport ended with $remaining bytes missing." }
            $outputStream.Write($buffer, 0, $read)
            $remaining -= $read
        }
        $outputStream.Flush($true)
    } finally {
        $outputStream.Dispose()
    }
    Remove-Item Env:PSModulePath -ErrorAction SilentlyContinue
    $process = Start-Process -FilePath 'powershell.exe' -ArgumentList @('-NoLogo','-NoProfile','-NonInteractive','-WindowStyle','Hidden','-ExecutionPolicy','Bypass','-EncodedCommand','%s') -RedirectStandardInput $archive -NoNewWindow -Wait -PassThru
    if ($process.ExitCode -ne 0) { exit $process.ExitCode }
} catch {
    [Console]::Error.WriteLine([string]$_.Exception.Message)
    exit 1
} finally {
    Remove-GuestArchiveStaging
}
exit 0`, staging, expectedArchiveLength, encodePowerShell(withPlainPowerShellErrors(launcherScript)))
}

func withPlainPowerShellErrors(script string) string {
	return "try {\n& {\n" + script + "\n}\n} catch {\n" +
		"    [Console]::Error.WriteLine([string]$_.Exception.Message)\n" +
		"    exit 1\n}\n"
}

func runSSHPowerShell(ctx context.Context, connection Connection, input io.Reader, launcherScript, role string, maximumOutput int) ([]byte, error) {
	return runSSHPowerShellWithDiagnostics(ctx, connection, input, launcherScript, role, maximumOutput, true)
}

func runSecretSSHPowerShell(ctx context.Context, connection Connection, input io.Reader, launcherScript, role string, maximumOutput int) ([]byte, error) {
	return runSSHPowerShellWithDiagnostics(ctx, connection, input, launcherScript, role, maximumOutput, false)
}

func runSSHPowerShellWithDiagnostics(ctx context.Context, connection Connection, input io.Reader, launcherScript, role string, maximumOutput int, includeRemoteDiagnostics bool) ([]byte, error) {
	return runSSHRemoteCommandWithDiagnostics(ctx, connection, input, []string{
		"powershell.exe", "-NoLogo", "-NoProfile", "-NonInteractive", "-WindowStyle", "Hidden", "-ExecutionPolicy", "Bypass",
		"-EncodedCommand", encodePowerShell(withPlainPowerShellErrors(launcherScript)),
	}, role, maximumOutput, includeRemoteDiagnostics)
}

func runSSHRemoteCommandWithDiagnostics(ctx context.Context, connection Connection, input io.Reader, remoteArguments []string, role string, maximumOutput int, includeRemoteDiagnostics bool) ([]byte, error) {
	if maximumOutput <= 0 {
		return nil, fmt.Errorf("%s output limit is invalid", role)
	}
	sshExecutable, err := exec.LookPath("ssh.exe")
	if err != nil {
		return nil, errors.New("OpenSSH ssh.exe is not on PATH")
	}
	arguments := []string{"-T", "-F", connection.SSHConfigPath, connection.SSHTarget}
	arguments = append(arguments, remoteArguments...)
	command := hiddenCommandContext(ctx, sshExecutable, arguments...)
	command.Stdin = input
	stdout := boundedCommandOutput{maximum: maximumOutput}
	stderr := boundedCommandOutput{maximum: maximumSSHErrorBytes}
	defer stderr.clear()
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		stdout.clear()
		remoteDiagnostics := ""
		if includeRemoteDiagnostics {
			remoteDiagnostics = stderr.text()
		}
		if contextError := ctx.Err(); contextError != nil {
			return nil, sshPowerShellError(role, err, contextError, remoteDiagnostics, includeRemoteDiagnostics)
		}
		return nil, sshPowerShellError(role, err, nil, remoteDiagnostics, includeRemoteDiagnostics)
	}
	if stdout.overflow {
		stdout.clear()
		return nil, fmt.Errorf("%s over SSH exceeded the %d-byte output limit", role, maximumOutput)
	}
	return stdout.buffer.Bytes(), nil
}

func sshPowerShellError(role string, commandError, contextError error, remoteDiagnostics string, includeRemoteDiagnostics bool) error {
	remoteDiagnostics = strings.TrimSpace(remoteDiagnostics)
	if contextError != nil {
		if includeRemoteDiagnostics && remoteDiagnostics != "" {
			return fmt.Errorf("%s over SSH: %w (%v): %s", role, commandError, contextError, remoteDiagnostics)
		}
		if includeRemoteDiagnostics {
			return fmt.Errorf("%s over SSH: %w (%v)", role, commandError, contextError)
		}
		return fmt.Errorf("%s over SSH: %w (%v); remote diagnostics redacted", role, commandError, contextError)
	}
	if includeRemoteDiagnostics && remoteDiagnostics != "" {
		return fmt.Errorf("%s over SSH: %w: %s", role, commandError, remoteDiagnostics)
	}
	if includeRemoteDiagnostics {
		return fmt.Errorf("%s over SSH: %w", role, commandError)
	}
	return fmt.Errorf("%s over SSH: %w; remote diagnostics redacted", role, commandError)
}

type boundedCommandOutput struct {
	buffer   bytes.Buffer
	maximum  int
	overflow bool
}

func (output *boundedCommandOutput) Write(data []byte) (int, error) {
	written := len(data)
	remaining := output.maximum - output.buffer.Len()
	if remaining > 0 {
		if len(data) > remaining {
			_, _ = output.buffer.Write(data[:remaining])
		} else {
			_, _ = output.buffer.Write(data)
		}
	}
	if len(data) > remaining {
		output.overflow = true
	}
	return written, nil
}

func (output *boundedCommandOutput) text() string {
	text := boundedText(output.buffer.Bytes())
	if output.overflow {
		return text + " [truncated]"
	}
	return text
}

func (output *boundedCommandOutput) clear() {
	clear(output.buffer.Bytes())
	output.buffer.Reset()
	output.overflow = false
}
