package sandbox

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const sandboxProcessInspectionTimeout = 30 * time.Second

func ensureIdentity(ctx context.Context, directory string) (string, string, error) {
	var err error
	directory, err = ensurePhysicalDirectory(directory, "SSH identity")
	if err != nil {
		return "", "", err
	}
	privateKey := filepath.Join(directory, "id_ed25519")
	publicKey := privateKey + ".pub"
	privateExists, err := regularFileExists(privateKey)
	if err != nil {
		return "", "", err
	}
	publicExists, err := regularFileExists(publicKey)
	if err != nil {
		return "", "", err
	}
	if privateExists != publicExists {
		return "", "", errors.New("SSH identity is incomplete; both private and public key files are required")
	}
	if !privateExists {
		sshKeygen, err := exec.LookPath("ssh-keygen.exe")
		if err != nil {
			return "", "", errors.New("OpenSSH ssh-keygen.exe is not on PATH")
		}
		command := hiddenCommandContext(ctx, sshKeygen, "-q", "-t", "ed25519", "-a", "64", "-N", "", "-C", "herdr-sandbox", "-f", privateKey)
		if output, err := command.CombinedOutput(); err != nil {
			return "", "", fmt.Errorf("generate host SSH identity: %w: %s", err, boundedText(output))
		}
	}
	data, err := os.ReadFile(publicKey)
	if err != nil {
		return "", "", fmt.Errorf("read generated SSH public key: %w", err)
	}
	if err := validateEd25519PublicKey(string(data)); err != nil {
		return "", "", fmt.Errorf("validate host SSH public key: %w", err)
	}
	return privateKey, publicKey, nil
}

func regularFileExists(path string) (bool, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("inspect %s: %w", path, err)
	}
	reparse, err := fileInfoIsReparsePoint(info)
	if err != nil {
		return false, fmt.Errorf("inspect %s reparse state: %w", path, err)
	}
	if reparse || !info.Mode().IsRegular() {
		return false, fmt.Errorf("expected a regular non-reparse file: %s", path)
	}
	return true, nil
}

func validateEd25519PublicKey(value string) error {
	fields := strings.Fields(value)
	if len(fields) < 2 || len(fields) > 3 || fields[0] != "ssh-ed25519" {
		return errors.New("expected one Ed25519 public key")
	}
	if _, err := base64.StdEncoding.DecodeString(fields[1]); err != nil {
		return fmt.Errorf("decode public key: %w", err)
	}
	return nil
}

func ensureNoRunningSandbox(ctx context.Context) error {
	processes, err := runningSandboxProcesses(ctx)
	if err != nil {
		return err
	}
	if len(processes) == 0 {
		return nil
	}
	descriptions := make([]string, 0, len(processes))
	for _, process := range processes {
		descriptions = append(descriptions, fmt.Sprintf("%s:%d", process.Name, process.PID))
	}
	return fmt.Errorf("an instance of Windows Sandbox is already running (%s); close it before starting a new session", strings.Join(descriptions, ", "))
}

type runningSandboxProcess struct {
	Name      string
	PID       int
	ParentPID int
}

func runningSandboxProcesses(ctx context.Context) ([]runningSandboxProcess, error) {
	powerShell, err := windowsPowerShellExecutable()
	if err != nil {
		return nil, err
	}
	inspectionContext, cancel := context.WithTimeout(ctx, sandboxProcessInspectionTimeout)
	defer cancel()
	script := `$processes = @(Get-CimInstance Win32_Process -Filter "Name = 'WindowsSandbox.exe' OR Name = 'WindowsSandboxClient.exe'" -ErrorAction Stop)
$processes | ForEach-Object {
    $name = [IO.Path]::GetFileNameWithoutExtension([string]$_.Name)
    '{0}:{1}:{2}' -f $name,$_.ProcessId,$_.ParentProcessId
}`
	command := hiddenCommandContext(inspectionContext, powerShell, "-NoLogo", "-NoProfile", "-NonInteractive", "-Command", script)
	output, err := command.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("check for an existing Windows Sandbox: %w: %s", err, boundedText(output))
	}
	return parseRunningSandboxProcesses(output)
}

func parseRunningSandboxProcesses(output []byte) ([]runningSandboxProcess, error) {
	text := strings.ReplaceAll(strings.TrimSpace(string(output)), "\r\n", "\n")
	if text == "" {
		return nil, nil
	}
	lines := strings.Split(text, "\n")
	if len(lines) > 8 {
		return nil, fmt.Errorf("check for an existing Windows Sandbox: unexpected process count %d", len(lines))
	}
	processes := make([]runningSandboxProcess, 0, len(lines))
	for _, line := range lines {
		fields := strings.Split(strings.TrimSpace(line), ":")
		if len(fields) != 3 || (fields[0] != "WindowsSandbox" && fields[0] != "WindowsSandboxClient") {
			return nil, fmt.Errorf("check for an existing Windows Sandbox: unexpected process record %q", line)
		}
		pid, err := strconv.Atoi(fields[1])
		if err != nil || pid < 1 {
			return nil, fmt.Errorf("check for an existing Windows Sandbox: invalid process record %q", line)
		}
		parentPID, err := strconv.Atoi(fields[2])
		if err != nil || parentPID < 1 {
			return nil, fmt.Errorf("check for an existing Windows Sandbox: invalid parent process record %q", line)
		}
		processes = append(processes, runningSandboxProcess{Name: fields[0], PID: pid, ParentPID: parentPID})
	}
	return processes, nil
}

func windowsPowerShellExecutable() (string, error) {
	windowsDirectory := strings.TrimSpace(os.Getenv("WINDIR"))
	if windowsDirectory == "" {
		return "", errors.New("WINDIR is not set")
	}
	path := filepath.Join(windowsDirectory, "System32", "WindowsPowerShell", "v1.0", "powershell.exe")
	if exists, err := regularFileExists(path); err != nil {
		return "", err
	} else if !exists {
		return "", fmt.Errorf("PowerShell 5.1 for Windows is unavailable: %s", path)
	}
	return path, nil
}

func boundedText(data []byte) string {
	text := strings.ToValidUTF8(strings.TrimSpace(string(data)), "�")
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.Map(func(value rune) rune {
		if value == '\n' || value == '\t' || unicode.IsPrint(value) {
			return value
		}
		return '�'
	}, text)
	const maximumBytes = 2000
	if len(text) <= maximumBytes {
		return text
	}
	marker := fmt.Sprintf("\n... diagnostic truncated; original UTF-8 bytes: %d ...\n", len(text))
	contentBudget := maximumBytes - len(marker)
	headEnd := contentBudget / 2
	for headEnd > 0 && !utf8.RuneStart(text[headEnd]) {
		headEnd--
	}
	tailStart := len(text) - (contentBudget - headEnd)
	for tailStart < len(text) && !utf8.RuneStart(text[tailStart]) {
		tailStart++
	}
	return text[:headEnd] + marker + text[tailStart:]
}
