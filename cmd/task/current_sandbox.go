package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strings"
	"time"
)

const (
	currentSandboxNativeEnvironment    = "HERDR_SANDBOX_CURRENT_NATIVE"
	currentSandboxPayloadEnvironment   = "HERDR_SANDBOX_CURRENT_NATIVE_PAYLOAD"
	currentSandboxPreflightEnvironment = "HERDR_SANDBOX_CURRENT_PREFLIGHT"
	currentSandboxPreflightTimeout     = 45 * time.Second
)

var currentSandboxFixtureSafeDirectories = []string{
	"C:/Workspaces/herdr-sandbox-native-audio",
	"C:/Workspaces/herdr-sandbox-native-core",
	"C:/Workspaces/herdr-sandbox-native-handy",
	"C:/Workspaces/herdr-sandbox-native-herdr",
	"C:/Workspaces/herdr-sandbox-native-python-ai",
}

type currentSandboxProcessIdentity struct {
	Role        string `json:"role"`
	PID         int    `json:"pid"`
	SessionID   int    `json:"sessionId"`
	CreationUTC string `json:"creationUtc"`
	Executable  string `json:"executable"`
}

type currentSandboxIdentity struct {
	SchemaVersion int                             `json:"schemaVersion"`
	Herdr         []currentSandboxProcessIdentity `json:"herdr"`
	SSHD          currentSandboxProcessIdentity   `json:"sshd"`
}

func nativeCurrentSandbox(ctx context.Context, stdout, stderr io.Writer, payloadDirectory string) error {
	if err := currentSandboxProvisioningPreflight(ctx, stdout, stderr); err != nil {
		return err
	}
	return nativeCurrentSandboxProvisioning(ctx, stdout, stderr, payloadDirectory)
}

func currentSandboxProvisioningPreflight(ctx context.Context, stdout, stderr io.Writer) error {
	if runtime.GOOS != "windows" || !strings.EqualFold(os.Getenv("USERNAME"), "WDAGUtilityAccount") || os.Getenv("HERDR_ENV") != "1" {
		return errors.New("provisioning-preflight requires the active Herdr-managed Windows Sandbox")
	}
	preflightContext, cancel := context.WithTimeout(ctx, currentSandboxPreflightTimeout)
	defer cancel()
	pattern := "^(TestAudioGridderReleaseManifestBindsSourceAndInstalledFilesInWindowsPowerShell51|TestCurrentProvisioningInputParsersInWindowsPowerShell51|TestCurrentSandboxProvisioningPreflight)$"
	command := hiddenCommandContext(preflightContext, "go", "test", "./internal/sandbox", "-run", pattern, "-count=1", "-timeout", "40s", "-v")
	command.Env = currentSandboxPreflightTestEnvironment()
	command.Stdout = stdout
	command.Stderr = stderr
	command.Stdin = os.Stdin
	if err := command.Run(); err != nil {
		return fmt.Errorf("run current-Sandbox provisioning preflight: %w", err)
	}
	_, err := fmt.Fprintln(stdout, "Current-Sandbox provisioning preflight passed.")
	return err
}

func nativeCurrentSandboxProvisioning(ctx context.Context, stdout, stderr io.Writer, payloadDirectory string) error {
	if runtime.GOOS != "windows" || !strings.EqualFold(os.Getenv("USERNAME"), "WDAGUtilityAccount") {
		return errors.New("native-current-sandbox requires the active confirmed Windows Sandbox")
	}
	if os.Getenv("HERDR_ENV") != "1" {
		return errors.New("native-current-sandbox requires an active Herdr-managed development session")
	}
	before, err := inspectCurrentSandboxIdentity(ctx)
	if err != nil {
		return err
	}
	gitSafeDirectories, err := readGlobalGitSafeDirectories(ctx)
	if err != nil {
		return err
	}

	command := hiddenCommandContext(ctx, "go", "test", "./internal/sandbox", "-run", "^TestCurrentSandboxProvisioning$", "-count=1", "-timeout", "40m")
	command.Env = currentSandboxTestEnvironment(payloadDirectory)
	command.Stdout = stdout
	command.Stderr = stderr
	command.Stdin = os.Stdin
	runErr := command.Run()
	var audioErr error
	if runErr == nil {
		audioErr = runCurrentSandboxAudioSmoke(ctx, stdout, stderr)
	}
	gitRestoreErr := restoreGlobalGitSafeDirectories(ctx, gitSafeDirectories)
	after, identityErr := inspectCurrentSandboxIdentity(ctx)
	if identityErr == nil {
		identityErr = verifyCurrentSandboxIdentityPreserved(before, after)
	}
	if runErr != nil {
		runErr = fmt.Errorf("run current-Sandbox provisioning: %w", runErr)
	}
	if err := errors.Join(runErr, audioErr, gitRestoreErr, identityErr); err != nil {
		return err
	}
	_, err = fmt.Fprintln(stdout, "Current-Sandbox provisioning and REAPER-to-AudioGridder connection passed without restarting SSH or Herdr.")
	return err
}

func currentSandboxPreflightTestEnvironment() []string {
	filtered := make([]string, 0, len(os.Environ())+1)
	for _, entry := range os.Environ() {
		name, _, found := strings.Cut(entry, "=")
		if found && (strings.EqualFold(name, currentSandboxPreflightEnvironment) || strings.EqualFold(name, fastTestsEnvironment)) {
			continue
		}
		filtered = append(filtered, entry)
	}
	return append(filtered, currentSandboxPreflightEnvironment+"=1")
}

func runCurrentSandboxAudioSmoke(ctx context.Context, stdout, stderr io.Writer) error {
	smokeScript, err := filepath.Abs(filepath.Join("cmd", "task", "assets", "native-audio-connection-smoke.ps1"))
	if err != nil {
		return fmt.Errorf("resolve current-Sandbox audio smoke script: %w", err)
	}
	reaperScript, err := filepath.Abs(filepath.Join("cmd", "task", "assets", "native-audio-reaper-smoke.lua"))
	if err != nil {
		return fmt.Errorf("resolve current-Sandbox REAPER smoke script: %w", err)
	}
	for _, path := range []string{smokeScript, reaperScript} {
		if _, err := os.Stat(path); err != nil {
			return fmt.Errorf("find current-Sandbox audio smoke asset %s: %w", path, err)
		}
	}
	powerShell := filepath.Join(os.Getenv("SystemRoot"), "System32", "WindowsPowerShell", "v1.0", "powershell.exe")
	command := hiddenCommandContext(ctx, powerShell,
		"-NoLogo", "-NoProfile", "-NonInteractive", "-WindowStyle", "Hidden", "-ExecutionPolicy", "Bypass",
		"-File", smokeScript, "-ReaperScriptPath", reaperScript)
	command.Stdout = stdout
	command.Stderr = stderr
	if err := command.Run(); err != nil {
		return fmt.Errorf("run current-Sandbox REAPER-to-AudioGridder connection smoke: %w", err)
	}
	return nil
}

func readGlobalGitSafeDirectories(ctx context.Context) ([]string, error) {
	command := hiddenCommandContext(ctx, "git", "config", "--global", "--get-all", "safe.directory")
	var output bytes.Buffer
	command.Stdout = &output
	command.Stderr = &output
	err := command.Run()
	if err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) || exitErr.ExitCode() != 1 {
			return nil, fmt.Errorf("read global Git safe directories: %w: %s", err, strings.TrimSpace(output.String()))
		}
	}
	text := strings.TrimSpace(output.String())
	if text == "" {
		return nil, nil
	}
	values := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	for _, value := range values {
		if strings.TrimSpace(value) == "" || strings.ContainsAny(value, "\r\n\x00") {
			return nil, errors.New("global Git safe-directory output is invalid")
		}
	}
	return values, nil
}

func restoreGlobalGitSafeDirectories(ctx context.Context, expected []string) error {
	current, err := readGlobalGitSafeDirectories(ctx)
	if err != nil || slices.Equal(current, expected) {
		return err
	}
	if !slices.Equal(current, currentSandboxFixtureSafeDirectories) {
		return fmt.Errorf("refuse to overwrite concurrently changed global Git safe directories: %q", current)
	}
	if len(expected) == 0 {
		command := hiddenCommandContext(ctx, "git", "config", "--global", "--unset-all", "safe.directory")
		if err := command.Run(); err != nil {
			var exitErr *exec.ExitError
			if !errors.As(err, &exitErr) || exitErr.ExitCode() != 5 {
				return fmt.Errorf("remove task-owned global Git safe directories: %w", err)
			}
		}
	} else {
		if err := hiddenCommandContext(ctx, "git", "config", "--global", "--replace-all", "safe.directory", expected[0]).Run(); err != nil {
			return fmt.Errorf("restore first global Git safe directory: %w", err)
		}
		for _, value := range expected[1:] {
			if err := hiddenCommandContext(ctx, "git", "config", "--global", "--add", "safe.directory", value).Run(); err != nil {
				return fmt.Errorf("restore global Git safe directory %s: %w", value, err)
			}
		}
	}
	restored, err := readGlobalGitSafeDirectories(ctx)
	if err != nil {
		return err
	}
	if !slices.Equal(restored, expected) {
		return fmt.Errorf("global Git safe directories were not restored: got %q want %q", restored, expected)
	}
	return nil
}

func currentSandboxTestEnvironment(payloadDirectory string) []string {
	filtered := make([]string, 0, len(os.Environ())+2)
	for _, entry := range os.Environ() {
		name, _, found := strings.Cut(entry, "=")
		if !found || strings.EqualFold(name, currentSandboxNativeEnvironment) ||
			strings.EqualFold(name, currentSandboxPayloadEnvironment) {
			continue
		}
		filtered = append(filtered, entry)
	}
	filtered = append(filtered, currentSandboxNativeEnvironment+"=1")
	if payloadDirectory != "" {
		filtered = append(filtered, currentSandboxPayloadEnvironment+"="+payloadDirectory)
	}
	return filtered
}

func inspectCurrentSandboxIdentity(ctx context.Context) (currentSandboxIdentity, error) {
	script, err := filepath.Abs(filepath.Join("cmd", "task", "assets", "current-sandbox-identity.ps1"))
	if err != nil {
		return currentSandboxIdentity{}, fmt.Errorf("resolve current-Sandbox identity script: %w", err)
	}
	if _, err := os.Stat(script); err != nil {
		return currentSandboxIdentity{}, fmt.Errorf("find current-Sandbox identity script: %w", err)
	}
	powerShell := filepath.Join(os.Getenv("SystemRoot"), "System32", "WindowsPowerShell", "v1.0", "powershell.exe")
	command := hiddenCommandContext(ctx, powerShell, "-NoLogo", "-NoProfile", "-NonInteractive", "-WindowStyle", "Hidden", "-ExecutionPolicy", "Bypass", "-File", script)
	var output bytes.Buffer
	command.Stdout = &output
	command.Stderr = &output
	if err := command.Run(); err != nil {
		return currentSandboxIdentity{}, fmt.Errorf("inspect current Sandbox SSH and Herdr identity: %w: %s", err, strings.TrimSpace(output.String()))
	}
	decoder := json.NewDecoder(&output)
	decoder.DisallowUnknownFields()
	var identity currentSandboxIdentity
	if err := decoder.Decode(&identity); err != nil {
		return currentSandboxIdentity{}, fmt.Errorf("decode current Sandbox SSH and Herdr identity: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return currentSandboxIdentity{}, errors.New("decode current Sandbox SSH and Herdr identity: trailing data")
	}
	if identity.SchemaVersion != 1 || identity.SSHD.Role != "sshd-service" || identity.SSHD.PID <= 0 || len(identity.Herdr) < 2 {
		return currentSandboxIdentity{}, errors.New("current Sandbox SSH and Herdr identity is incomplete")
	}
	serverCount := 0
	for _, process := range identity.Herdr {
		if process.Role == "server" {
			serverCount++
		}
		if process.PID <= 0 || process.SessionID < 0 || process.CreationUTC == "" || !filepath.IsAbs(process.Executable) {
			return currentSandboxIdentity{}, fmt.Errorf("current Sandbox Herdr %s identity is invalid", process.Role)
		}
	}
	if serverCount != 1 || !filepath.IsAbs(identity.SSHD.Executable) || identity.SSHD.CreationUTC == "" {
		return currentSandboxIdentity{}, errors.New("current Sandbox server or SSH service identity is invalid")
	}
	sort.Slice(identity.Herdr, func(left, right int) bool { return identity.Herdr[left].PID < identity.Herdr[right].PID })
	return identity, nil
}

func verifyCurrentSandboxIdentityPreserved(before, after currentSandboxIdentity) error {
	afterByPID := make(map[int]currentSandboxProcessIdentity, len(after.Herdr))
	for _, process := range after.Herdr {
		afterByPID[process.PID] = process
	}
	for _, expected := range before.Herdr {
		actual, found := afterByPID[expected.PID]
		if !found || actual != expected {
			return fmt.Errorf("current-Sandbox provisioning changed Herdr %s process identity: before=%+v after=%+v", expected.Role, expected, actual)
		}
	}
	if after.SSHD != before.SSHD {
		return fmt.Errorf("current-Sandbox provisioning changed OpenSSH service identity: before=%+v after=%+v", before.SSHD, after.SSHD)
	}
	return nil
}
