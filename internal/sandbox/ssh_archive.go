package sandbox

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
)

func runSSHArchivePowerShell(ctx context.Context, connection Connection, archive []byte, launcherScript, role string) ([]byte, error) {
	if len(archive) == 0 {
		return nil, fmt.Errorf("%s archive is empty", role)
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
	command.Stdin = bytes.NewReader(archive)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		if contextError := ctx.Err(); contextError != nil {
			return nil, fmt.Errorf("%s over SSH: %w (%v): %s", role, err, contextError, boundedText(stderr.Bytes()))
		}
		return nil, fmt.Errorf("%s over SSH: %w: %s", role, err, boundedText(stderr.Bytes()))
	}
	return stdout.Bytes(), nil
}
