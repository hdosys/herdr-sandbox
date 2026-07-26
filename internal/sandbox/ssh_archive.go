package sandbox

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
)

const (
	maximumSSHResultBytes = 1024 * 1024
	maximumSSHErrorBytes  = 64 * 1024
)

func runSSHArchivePowerShell(ctx context.Context, connection Connection, archive []byte, launcherScript, role string) ([]byte, error) {
	if len(archive) == 0 {
		return nil, fmt.Errorf("%s archive is empty", role)
	}
	return runSSHPowerShell(ctx, connection, bytes.NewReader(archive), launcherScript, role, maximumSSHResultBytes)
}

func runSSHPowerShell(ctx context.Context, connection Connection, input io.Reader, launcherScript, role string, maximumOutput int) ([]byte, error) {
	return runSSHPowerShellWithDiagnostics(ctx, connection, input, launcherScript, role, maximumOutput, true)
}

func runSecretSSHPowerShell(ctx context.Context, connection Connection, input io.Reader, launcherScript, role string, maximumOutput int) ([]byte, error) {
	return runSSHPowerShellWithDiagnostics(ctx, connection, input, launcherScript, role, maximumOutput, false)
}

func runSSHPowerShellWithDiagnostics(ctx context.Context, connection Connection, input io.Reader, launcherScript, role string, maximumOutput int, includeRemoteDiagnostics bool) ([]byte, error) {
	if maximumOutput <= 0 {
		return nil, fmt.Errorf("%s output limit is invalid", role)
	}
	sshExecutable, err := exec.LookPath("ssh.exe")
	if err != nil {
		return nil, errors.New("OpenSSH ssh.exe is not on PATH")
	}
	command := hiddenCommandContext(ctx, sshExecutable,
		"-T", "-F", connection.SSHConfigPath, connection.SSHTarget,
		"powershell.exe", "-NoLogo", "-NoProfile", "-NonInteractive", "-WindowStyle", "Hidden", "-ExecutionPolicy", "Bypass",
		"-EncodedCommand", encodePowerShell(launcherScript),
	)
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
	if contextError != nil {
		if includeRemoteDiagnostics {
			return fmt.Errorf("%s over SSH: %w (%v): %s", role, commandError, contextError, remoteDiagnostics)
		}
		return fmt.Errorf("%s over SSH: %w (%v); remote diagnostics redacted", role, commandError, contextError)
	}
	if includeRemoteDiagnostics {
		return fmt.Errorf("%s over SSH: %w: %s", role, commandError, remoteDiagnostics)
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
