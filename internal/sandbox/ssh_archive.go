package sandbox

import (
	"bytes"
	"context"
	"crypto/sha256"
	_ "embed"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
)

const (
	maximumSSHResultBytes                       = 1024 * 1024
	maximumSSHErrorBytes                        = 64 * 1024
	maximumSSHArchiveTransportCommandCharacters = 30000
)

//go:embed assets/ssh-archive-staging.ps1
var sshArchiveStagingPowerShell string

//go:embed assets/ssh-archive-transport.ps1
var sshArchiveTransportPowerShell string

func guestArchiveStagingPowerShell(directoryName, role string, nested bool) string {
	quote := func(value string) string { return strings.ReplaceAll(value, "'", "''") }
	root := "'" + quote(guestRootDirectory) + "\\staging'"
	if nested {
		root = "Join-Path $PSScriptRoot 'staging'"
	}
	return fmt.Sprintf(`$stagingRoot = %s
$transferRoot = Join-Path $stagingRoot '%s'
$archive = Join-Path $transferRoot 'input.zip'
$expanded = Join-Path $transferRoot 'expanded'
$stagingRole = '%s'
%s`, root, quote(directoryName), quote(role), sshArchiveStagingPowerShell)
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
	launcher := sshArchiveLauncherBytes(launcherScript)
	defer clear(launcher)
	attemptID, err := newRunID()
	if err != nil {
		return nil, err
	}
	budget := defaultSandboxUpTimeout
	if deadline, found := ctx.Deadline(); found {
		budget = time.Until(deadline)
	}
	if budget < time.Second {
		return nil, fmt.Errorf("%s has no remaining execution budget: %w", role, context.DeadlineExceeded)
	}
	transportCommand := buildSSHArchiveTransportCommand(digest, len(archive), launcher, budget, attemptID)
	if len(transportCommand) > maximumSSHArchiveTransportCommandCharacters {
		return nil, fmt.Errorf("%s SSH transport command exceeds %d characters", role, maximumSSHArchiveTransportCommandCharacters)
	}
	input := io.MultiReader(bytes.NewReader(provisioningProcessSource), bytes.NewReader(launcher), bytes.NewReader(archive))
	return runSSHArchiveWithInputLease(ctx, connection, input, transportCommand, role, budget, includeRemoteDiagnostics)
}

func runSSHArchiveWithInputLease(ctx context.Context, connection Connection, frames io.Reader, command, role string, budget time.Duration, diagnostics bool) ([]byte, error) {
	reader, writer, err := os.Pipe()
	if err != nil {
		return nil, fmt.Errorf("open SSH input lease: %w", err)
	}
	defer reader.Close()
	defer writer.Close()
	writeResult := make(chan error, 1)
	go func() {
		_, err := io.Copy(writer, frames)
		if err != nil {
			_ = writer.Close()
		}
		writeResult <- err
	}()
	// Closing stdin requests remote Job Object cancellation. Keep SSH alive long
	// enough to receive terminal cleanup instead of abandoning a remote installer.
	const cleanupBudget = 40 * time.Second
	commandContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), budget+cleanupBudget)
	defer cancel()
	finished := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = writer.Close()
			timer := time.NewTimer(cleanupBudget)
			defer timer.Stop()
			select {
			case <-timer.C:
				cancel()
			case <-finished:
			}
		case <-finished:
		}
	}()
	output, runErr := runSSHRemoteCommandWithDiagnostics(commandContext, connection, reader, []string{command}, role, maximumSSHResultBytes, diagnostics)
	close(finished)
	_ = writer.Close()
	writeErr := <-writeResult
	if runErr != nil || ctx.Err() != nil {
		return nil, errors.Join(runErr, ctx.Err())
	}
	if writeErr != nil {
		return nil, fmt.Errorf("send %s input: %w", role, writeErr)
	}
	return output, nil
}

func sshArchiveLauncherBytes(script string) []byte {
	// Windows PowerShell 5.1 requires the BOM to read a UTF-8 script unambiguously.
	return []byte("\xef\xbb\xbf" + withPlainPowerShellErrors(script))
}

func buildSSHArchiveTransportCommand(expectedDigest string, expectedArchiveLength int, launcher []byte, budget time.Duration, attemptID string) string {
	staging := guestArchiveStagingPowerShell("transport-"+attemptID, "SSH archive transport", false)
	return fmt.Sprintf(`$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'
$transportDeadline = [DateTime]::UtcNow.AddMilliseconds(%d)
%s
$expectedProcessOwnerLength = [long]%d
$expectedProcessOwnerDigest = '%x'
$expectedLauncherLength = [long]%d
$expectedArchiveLength = [long]%d
$expectedLauncherDigest = '%x'
%s`, budget.Milliseconds(), staging, len(provisioningProcessSource), sha256.Sum256(provisioningProcessSource), len(launcher), expectedArchiveLength, sha256.Sum256(launcher), sshArchiveTransportPowerShell)
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
