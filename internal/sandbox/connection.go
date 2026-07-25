package sandbox

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	managedSSHIncludeStart = "# BEGIN herdr-sandbox managed SSH include"
	managedSSHIncludeEnd   = "# END herdr-sandbox managed SSH include"
)

func writeRunConnection(plan runPlan, connectable connectableStatus, herdrExecutable string) (Connection, error) {
	status := connectionStatus(connectable)
	sshDirectory := filepath.Join(plan.RunDirectory, ".ssh")
	if err := os.MkdirAll(sshDirectory, 0o700); err != nil {
		return Connection{}, fmt.Errorf("create run SSH directory: %w", err)
	}
	hostKeyAlias := "windows-sandbox-" + plan.ID
	knownHostsPath := filepath.Join(sshDirectory, "known_hosts")
	knownHosts := hostKeyAlias + " " + status.SSHHostKey + "\n"
	if err := os.WriteFile(knownHostsPath, []byte(knownHosts), 0o600); err != nil {
		return Connection{}, fmt.Errorf("write run known_hosts: %w", err)
	}

	configPath := filepath.Join(sshDirectory, "config")
	config := renderSSHConfig(status, plan.PrivateKeyPath, knownHostsPath, hostKeyAlias)
	if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
		return Connection{}, fmt.Errorf("write run SSH config: %w", err)
	}
	return Connection{
		RunDirectory:    plan.RunDirectory,
		StatusDirectory: plan.StatusDirectory,
		SSHConfigPath:   configPath,
		SSHTarget:       sshTargetName,
		GuestIP:         status.IP,
		WinGetVersion:   status.WinGetVersion,
		HerdrVersion:    status.HerdrVersion,
		HerdrProtocol:   status.HerdrProtocol,
		privateKeyPath:  plan.PrivateKeyPath,
		herdrExecutable: herdrExecutable,
	}, nil
}

func installRunConnectionAlias(dataDirectory string, connection Connection) error {
	config, err := os.ReadFile(connection.SSHConfigPath)
	if err != nil {
		return fmt.Errorf("read verified run SSH config: %w", err)
	}
	if err := installSSHHostAlias(dataDirectory, string(config)); err != nil {
		return err
	}
	return nil
}

func installSSHHostAlias(dataDirectory, config string) error {
	userHome, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("resolve user home for SSH config: %w", err)
	}
	return installSSHHostAliasAt(dataDirectory, userHome, config)
}

func installSSHHostAliasAt(dataDirectory, userHome, config string) error {
	if !filepath.IsAbs(dataDirectory) {
		return fmt.Errorf("SSH data directory is not absolute: %q", dataDirectory)
	}
	if !filepath.IsAbs(userHome) {
		return fmt.Errorf("user home for SSH config is not absolute: %q", userHome)
	}

	managedDirectory := filepath.Join(dataDirectory, "ssh")
	if err := os.MkdirAll(managedDirectory, 0o700); err != nil {
		return fmt.Errorf("create managed SSH directory: %w", err)
	}
	managedPath := filepath.Join(managedDirectory, "config")
	managedConfig := "# Managed by herdr-sandbox; changes are replaced on the next run.\n" + config
	if err := writeFileAtomically(managedPath, []byte(managedConfig), 0o600); err != nil {
		return fmt.Errorf("write managed Sandbox SSH config: %w", err)
	}

	userSSHDirectory := filepath.Join(userHome, ".ssh")
	if err := os.MkdirAll(userSSHDirectory, 0o700); err != nil {
		return fmt.Errorf("create user SSH directory: %w", err)
	}
	userConfigPath := filepath.Join(userSSHDirectory, "config")
	existing, err := os.ReadFile(userConfigPath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("read user SSH config: %w", err)
	}
	updated, err := updateManagedSSHInclude(string(existing), managedPath)
	if err != nil {
		return fmt.Errorf("update user SSH config: %w", err)
	}
	if updated == string(existing) {
		return nil
	}
	mode := os.FileMode(0o600)
	if info, statErr := os.Stat(userConfigPath); statErr == nil {
		mode = info.Mode().Perm()
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return fmt.Errorf("inspect user SSH config: %w", statErr)
	}
	if err := writeFileAtomically(userConfigPath, []byte(updated), mode); err != nil {
		return fmt.Errorf("write user SSH config: %w", err)
	}
	return nil
}

func updateManagedSSHInclude(existing, managedPath string) (string, error) {
	startCount := strings.Count(existing, managedSSHIncludeStart)
	endCount := strings.Count(existing, managedSSHIncludeEnd)
	if startCount != endCount || startCount > 1 {
		return "", errors.New("managed Sandbox SSH include markers are malformed")
	}

	remaining := existing
	if startCount == 1 {
		start := strings.Index(existing, managedSSHIncludeStart)
		endRelative := strings.Index(existing[start:], managedSSHIncludeEnd)
		if endRelative < 0 {
			return "", errors.New("managed Sandbox SSH include end marker is missing")
		}
		end := start + endRelative + len(managedSSHIncludeEnd)
		if strings.HasPrefix(existing[end:], "\r\n") {
			end += 2
		} else if strings.HasPrefix(existing[end:], "\n") {
			end++
		}
		remaining = existing[:start] + existing[end:]
		remaining = strings.TrimPrefix(remaining, "\r\n")
		remaining = strings.TrimPrefix(remaining, "\n")
	}

	block := strings.Join([]string{
		managedSSHIncludeStart,
		"Include " + quoteSSHPath(managedPath),
		managedSSHIncludeEnd,
		"",
	}, "\n")
	if remaining == "" {
		return block, nil
	}
	return block + "\n" + remaining, nil
}

func writeFileAtomically(path string, data []byte, mode os.FileMode) error {
	directory := filepath.Dir(path)
	temporary, err := os.CreateTemp(directory, ".herdr-sandbox-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	defer temporary.Close()
	if err := temporary.Chmod(mode); err != nil {
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		return err
	}
	if err := temporary.Sync(); err != nil {
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryPath, path)
}

func renderSSHConfig(status connectionStatus, privateKeyPath, knownHostsPath, hostKeyAlias string) string {
	return strings.Join([]string{
		"Host " + sshTargetName,
		"    HostName " + status.IP,
		"    Port 22",
		"    User " + status.SSHUser,
		"    IdentityFile " + quoteSSHPath(privateKeyPath),
		"    IdentitiesOnly yes",
		"    ForwardAgent no",
		"    BatchMode yes",
		"    PreferredAuthentications publickey",
		"    PasswordAuthentication no",
		"    KbdInteractiveAuthentication no",
		"    StrictHostKeyChecking yes",
		"    UserKnownHostsFile " + quoteSSHPath(knownHostsPath),
		"    HostKeyAlias " + hostKeyAlias,
		"    ConnectTimeout 10",
		"    LogLevel ERROR",
		"",
	}, "\n")
}

func quoteSSHPath(path string) string {
	return strconv.Quote(strings.ReplaceAll(path, `\`, `/`))
}

func verifySSH(ctx context.Context, connection Connection) error {
	ssh, err := exec.LookPath("ssh.exe")
	if err != nil {
		return errors.New("OpenSSH ssh.exe is not on PATH")
	}
	verifyContext, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	output, err := hiddenCommandContext(verifyContext, ssh, "-F", connection.SSHConfigPath, connection.SSHTarget, "Write-Output SANDBOX_SSH_READY").CombinedOutput()
	if err != nil {
		return fmt.Errorf("verify host-to-Sandbox SSH: %w: %s", err, boundedText(output))
	}
	found := false
	for _, line := range strings.Split(strings.ReplaceAll(string(output), "\r\n", "\n"), "\n") {
		if strings.TrimSpace(line) == "SANDBOX_SSH_READY" {
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("verify host-to-Sandbox SSH: readiness marker missing from %q", boundedText(output))
	}
	return nil
}

func verifyGuestHerdr(ctx context.Context, connection Connection) error {
	ssh, err := exec.LookPath("ssh.exe")
	if err != nil {
		return errors.New("OpenSSH ssh.exe is not on PATH")
	}
	verifyContext, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	remoteCommand := `powershell.exe -NoLogo -NoProfile -NonInteractive -WindowStyle Hidden -ExecutionPolicy Bypass -Command "& '` + guestHerdrPath + `' status server"`
	output, err := hiddenCommandContext(verifyContext, ssh, "-F", connection.SSHConfigPath, connection.SSHTarget, remoteCommand).CombinedOutput()
	if err != nil {
		return fmt.Errorf("verify guest Herdr server over SSH: %w: %s", err, boundedText(output))
	}
	text := strings.ReplaceAll(string(output), "\r\n", "\n")
	if !strings.Contains(text, "status: running\n") || !strings.Contains(text, fmt.Sprintf("protocol: %d", connection.HerdrProtocol)) {
		return fmt.Errorf("verify guest Herdr server: unexpected status %q", boundedText(output))
	}
	return nil
}
