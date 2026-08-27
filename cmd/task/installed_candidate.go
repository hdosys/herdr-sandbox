package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"herdr-sandbox/internal/productidentity"
)

type installedCandidateState struct {
	SchemaVersion        int    `json:"schemaVersion"`
	Installed            bool   `json:"installed"`
	DisplayName          string `json:"displayName"`
	DisplayVersion       string `json:"displayVersion"`
	InstallLocation      string `json:"installLocation"`
	QuietUninstallString string `json:"quietUninstallString"`
}

type preservedFile struct {
	Path   string
	Exists bool
	SHA256 []byte
}

func packageCurrentSandbox(ctx context.Context, tag string, stdout, stderr io.Writer) (resultErr error) {
	if runtime.GOOS != "windows" || runtime.GOARCH != "amd64" || !strings.EqualFold(os.Getenv("USERNAME"), "WDAGUtilityAccount") || os.Getenv("HERDR_ENV") != "1" {
		return errors.New("package-current-sandbox requires the active Herdr-managed Windows Sandbox")
	}
	version, err := parseReleaseVersion(tag)
	if err != nil {
		return err
	}
	predecessorVersion, err := immediatePredecessorVersion(version)
	if err != nil {
		return err
	}
	if err := currentSandboxProvisioningPreflight(ctx, stdout, stderr); err != nil {
		return err
	}
	identityBefore, err := inspectCurrentSandboxIdentity(ctx)
	if err != nil {
		return err
	}
	defer func() {
		identityContext, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		identityAfter, identityErr := inspectCurrentSandboxIdentity(identityContext)
		if identityErr == nil {
			identityErr = verifyCurrentSandboxIdentityPreserved(identityBefore, identityAfter)
		}
		resultErr = errors.Join(resultErr, identityErr)
	}()
	state, err := inspectInstalledCandidate(ctx)
	if err != nil {
		return err
	}
	installRoot := filepath.Join(os.Getenv("LOCALAPPDATA"), "Programs", productidentity.InstallDirectoryName)
	if state.Installed {
		return fmt.Errorf("package-current-sandbox refuses the pre-existing installation at %s", state.InstallLocation)
	}
	if _, err := os.Lstat(installRoot); err == nil {
		return fmt.Errorf("package-current-sandbox requires an absent install root: %s", installRoot)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect package-current-sandbox install root %s: %w", installRoot, err)
	}
	preserved, err := captureCurrentSandboxUserConfiguration()
	if err != nil {
		return err
	}
	revision, err := sourceRevision(ctx)
	if err != nil {
		return fmt.Errorf("resolve installed-candidate source revision: %w", err)
	}
	if err := validateWingetManifestProductCodes(); err != nil {
		return err
	}
	predecessorRoot, err := os.MkdirTemp("", "herdr-sandbox-predecessor-*")
	if err != nil {
		return fmt.Errorf("create predecessor package workspace: %w", err)
	}
	defer func() {
		if err := os.RemoveAll(predecessorRoot); err != nil {
			resultErr = errors.Join(resultErr, fmt.Errorf("remove predecessor package workspace: %w", err))
		}
	}()
	predecessorPaths := releasePaths(predecessorRoot, predecessorVersion)
	if _, err := fmt.Fprintf(stdout, "Building current-layout %s from source commit %s for immediate-predecessor upgrade acceptance.\n", predecessorVersion.Tag, revision); err != nil {
		return err
	}
	if err := buildWindowsReleasePackage(ctx, predecessorVersion, revision, predecessorPaths, stdout, stderr); err != nil {
		return err
	}
	paths := releasePaths(".", version)
	if err := buildWindowsReleasePackage(ctx, version, revision, paths, stdout, stderr); err != nil {
		return err
	}
	if err := writeReleaseArtifactEvidence(stdout, paths); err != nil {
		return err
	}
	installer, err := filepath.Abs(paths.Installer)
	if err != nil {
		return fmt.Errorf("resolve current-Sandbox candidate installer: %w", err)
	}
	predecessorInstaller, err := filepath.Abs(predecessorPaths.Installer)
	if err != nil {
		return fmt.Errorf("resolve current-Sandbox predecessor installer: %w", err)
	}

	installed := false
	defer func() {
		if installed {
			cleanupContext, cancel := context.WithTimeout(context.Background(), 40*time.Second)
			defer cancel()
			resultErr = errors.Join(resultErr, uninstallCurrentSandboxCandidate(cleanupContext, installRoot, stdout, stderr))
		}
		if resultErr != nil {
			resultErr = errors.Join(resultErr, verifyCurrentSandboxUserConfiguration(preserved))
		}
	}()
	install := func(path string) error {
		if err := runCurrentSandboxInstaller(ctx, path, stdout, stderr); err != nil {
			if !installed {
				inspectContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				state, inspectErr := inspectInstalledCandidate(inspectContext)
				cancel()
				_, helperErr := os.Lstat(filepath.Join(installRoot, productidentity.QuietUninstallHelperName))
				installed = (inspectErr == nil && state.Installed) || helperErr == nil
			}
			return err
		}
		installed = true
		return nil
	}
	if err := install(installer); err != nil {
		return err
	}
	seededConfiguration, err := captureCurrentSandboxSeededConfiguration(preserved)
	if err != nil {
		return err
	}
	preserved = seededConfiguration
	if err := validateInstalledCandidateTransition(ctx, version, revision, installRoot, paths.Stage, "fresh candidate install", preserved, stdout, stderr); err != nil {
		return err
	}
	if err := install(installer); err != nil {
		return err
	}
	if err := validateInstalledCandidateTransition(ctx, version, revision, installRoot, paths.Stage, "same-version repair", preserved, stdout, stderr); err != nil {
		return err
	}
	if err := uninstallCurrentSandboxCandidate(ctx, installRoot, stdout, stderr); err != nil {
		return err
	}
	if err := validateCurrentSandboxCandidateAbsent(ctx, installRoot); err != nil {
		return err
	}
	installed = false
	if err := verifyCurrentSandboxUserConfiguration(preserved); err != nil {
		return err
	}
	if err := install(predecessorInstaller); err != nil {
		return err
	}
	if err := validateInstalledCandidateTransition(ctx, predecessorVersion, revision, installRoot, predecessorPaths.Stage, "immediate-predecessor current-layout install", preserved, stdout, stderr); err != nil {
		return err
	}
	if err := install(installer); err != nil {
		return err
	}
	if err := validateInstalledCandidateTransition(ctx, version, revision, installRoot, paths.Stage, "immediate-predecessor upgrade", preserved, stdout, stderr); err != nil {
		return err
	}
	if err := nativeCurrentSandboxProvisioning(ctx, stdout, stderr, installRoot); err != nil {
		return err
	}
	if err := verifyCurrentSandboxUserConfiguration(preserved); err != nil {
		return err
	}
	if err := uninstallCurrentSandboxCandidate(ctx, installRoot, stdout, stderr); err != nil {
		return err
	}
	if err := validateCurrentSandboxCandidateAbsent(ctx, installRoot); err != nil {
		return err
	}
	installed = false
	if err := verifyCurrentSandboxUserConfiguration(preserved); err != nil {
		return err
	}
	_, err = fmt.Fprintln(stdout, "Installed candidate passed fresh install, same-version repair, immediate-predecessor upgrade, current-Sandbox provisioning, configuration preservation, and quiet uninstall cleanup.")
	return err
}

func validateInstalledCandidateTransition(ctx context.Context, version releaseVersion, revision, installRoot, stage, outcome string, preserved []preservedFile, stdout, stderr io.Writer) error {
	state, err := inspectInstalledCandidate(ctx)
	if err != nil {
		return fmt.Errorf("validate %s: %w", outcome, err)
	}
	if err := validateInstalledCandidate(state, version, installRoot, stage); err != nil {
		return fmt.Errorf("validate %s: %w", outcome, err)
	}
	if err := runInstalledCandidateVersion(ctx, filepath.Join(installRoot, productidentity.ExecutableName), version, revision, stdout, stderr); err != nil {
		return fmt.Errorf("validate %s: %w", outcome, err)
	}
	if err := verifyCurrentSandboxUserConfiguration(preserved); err != nil {
		return fmt.Errorf("validate %s: %w", outcome, err)
	}
	_, err = fmt.Fprintf(stdout, "Installed-candidate %s passed exact identity, payload, version, and configuration checks.\n", outcome)
	return err
}

func validateCurrentSandboxCandidateAbsent(ctx context.Context, installRoot string) error {
	state, err := inspectInstalledCandidate(ctx)
	if err != nil {
		return err
	}
	if state.Installed {
		return errors.New("current-Sandbox candidate remains registered after quiet uninstall")
	}
	if _, err := os.Lstat(installRoot); err == nil {
		return fmt.Errorf("current-Sandbox candidate install root remains after quiet uninstall: %s", installRoot)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect current-Sandbox candidate install root after uninstall: %w", err)
	}
	return nil
}

func inspectInstalledCandidate(ctx context.Context) (installedCandidateState, error) {
	script, err := filepath.Abs(filepath.Join("cmd", "task", "assets", "installed-candidate-state.ps1"))
	if err != nil {
		return installedCandidateState{}, fmt.Errorf("resolve installed candidate state script: %w", err)
	}
	powerShell := filepath.Join(os.Getenv("SystemRoot"), "System32", "WindowsPowerShell", "v1.0", "powershell.exe")
	command := hiddenCommandContext(ctx, powerShell, "-NoLogo", "-NoProfile", "-NonInteractive", "-WindowStyle", "Hidden", "-ExecutionPolicy", "Bypass", "-File", script, "-UninstallKey", productidentity.UninstallKeyName)
	var output bytes.Buffer
	command.Stdout = &output
	command.Stderr = &output
	if err := command.Run(); err != nil {
		return installedCandidateState{}, fmt.Errorf("inspect installed candidate state: %w: %s", err, strings.TrimSpace(output.String()))
	}
	decoder := json.NewDecoder(&output)
	decoder.DisallowUnknownFields()
	var state installedCandidateState
	if err := decoder.Decode(&state); err != nil {
		return installedCandidateState{}, fmt.Errorf("decode installed candidate state: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return installedCandidateState{}, errors.New("decode installed candidate state: trailing data")
	}
	if state.SchemaVersion != 1 {
		return installedCandidateState{}, errors.New("installed candidate state schema is invalid")
	}
	return state, nil
}

func runCurrentSandboxInstaller(ctx context.Context, installer string, stdout, stderr io.Writer) error {
	command := hiddenCommandContext(ctx, installer, "/S")
	command.Stdout = stdout
	command.Stderr = stderr
	if err := command.Run(); err != nil {
		return fmt.Errorf("silently install current-Sandbox candidate: %w", err)
	}
	return nil
}

func validateInstalledCandidate(state installedCandidateState, version releaseVersion, installRoot, stage string) error {
	if !state.Installed || state.DisplayName != productidentity.DisplayName || state.DisplayVersion != version.Display ||
		!strings.EqualFold(filepath.Clean(state.InstallLocation), filepath.Clean(installRoot)) {
		return fmt.Errorf("installed candidate identity is invalid: %+v", state)
	}
	for _, required := range []string{filepath.Join(installRoot, productidentity.QuietUninstallHelperName), filepath.Join(installRoot, installerUninstallerName)} {
		if !strings.Contains(strings.ToLower(state.QuietUninstallString), strings.ToLower(required)) {
			return fmt.Errorf("installed candidate quiet uninstall does not own %s", required)
		}
	}
	for _, file := range releasePackageFiles {
		installed := filepath.Join(installRoot, file.Name)
		staged := filepath.Join(stage, file.Name)
		info, err := os.Lstat(installed)
		if err != nil {
			return fmt.Errorf("inspect installed candidate payload %s: %w", file.Name, err)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("installed candidate payload is not a regular file: %s", file.Name)
		}
		installedHash, err := fileSHA256(installed)
		if err != nil {
			return fmt.Errorf("hash installed candidate file %s: %w", file.Name, err)
		}
		stagedHash, err := fileSHA256(staged)
		if err != nil {
			return fmt.Errorf("hash staged candidate file %s: %w", file.Name, err)
		}
		if !bytes.Equal(installedHash, stagedHash) {
			return fmt.Errorf("installed candidate file differs from staged payload: %s", file.Name)
		}
	}
	return nil
}

func runInstalledCandidateVersion(ctx context.Context, executable string, version releaseVersion, revision string, stdout, stderr io.Writer) error {
	command := hiddenCommandContext(ctx, executable, "--version")
	var output bytes.Buffer
	command.Stdout = &output
	command.Stderr = stderr
	if err := command.Run(); err != nil {
		return fmt.Errorf("run installed candidate version: %w", err)
	}
	text := strings.TrimSpace(output.String())
	expected, err := expectedInstalledCandidateVersion(version, revision)
	if err != nil {
		return err
	}
	if text != expected {
		return fmt.Errorf("installed candidate version output = %q, want %q", text, expected)
	}
	_, err = fmt.Fprintln(stdout, text)
	return err
}

func expectedInstalledCandidateVersion(version releaseVersion, revision string) (string, error) {
	revision, err := normalizeSourceRevision(revision)
	if err != nil {
		return "", fmt.Errorf("validate installed candidate source revision: %w", err)
	}
	return fmt.Sprintf("%s %s (%s)", productidentity.CommandName, version.Display, revision[:12]), nil
}

func uninstallCurrentSandboxCandidate(ctx context.Context, installRoot string, stdout, stderr io.Writer) error {
	powerShell := filepath.Join(os.Getenv("SystemRoot"), "System32", "WindowsPowerShell", "v1.0", "powershell.exe")
	helper := filepath.Join(installRoot, productidentity.QuietUninstallHelperName)
	uninstaller := filepath.Join(installRoot, installerUninstallerName)
	command := hiddenCommandContext(ctx, powerShell, "-NoLogo", "-NoProfile", "-NonInteractive", "-WindowStyle", "Hidden", "-ExecutionPolicy", "Bypass", "-File", helper, "-Uninstaller", uninstaller, "-InstallDirectory", installRoot)
	command.Stdout = stdout
	command.Stderr = stderr
	if err := command.Run(); err != nil {
		return fmt.Errorf("quietly uninstall current-Sandbox candidate: %w", err)
	}
	return nil
}

func captureCurrentSandboxUserConfiguration() ([]preservedFile, error) {
	root := filepath.Join(os.Getenv("APPDATA"), productidentity.ApplicationName)
	paths := []string{filepath.Join(root, productidentity.ConfigurationName), filepath.Join(root, productidentity.UserScriptName)}
	result := make([]preservedFile, 0, len(paths))
	for _, path := range paths {
		info, err := os.Lstat(path)
		if errors.Is(err, os.ErrNotExist) {
			result = append(result, preservedFile{Path: path})
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("inspect current-Sandbox user configuration %s: %w", path, err)
		}
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("current-Sandbox user configuration is not a regular file: %s", path)
		}
		hash, err := fileSHA256(path)
		if err != nil {
			return nil, fmt.Errorf("capture current-Sandbox user configuration %s: %w", path, err)
		}
		result = append(result, preservedFile{Path: path, Exists: true, SHA256: hash})
	}
	return result, nil
}

func captureCurrentSandboxSeededConfiguration(files []preservedFile) ([]preservedFile, error) {
	result := make([]preservedFile, len(files))
	copy(result, files)
	for index, file := range result {
		if file.Exists {
			continue
		}
		info, err := os.Lstat(file.Path)
		if err != nil {
			return nil, fmt.Errorf("inspect seeded current-Sandbox user configuration %s: %w", file.Path, err)
		}
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("seeded current-Sandbox user configuration is not a regular file: %s", file.Path)
		}
		hash, err := fileSHA256(file.Path)
		if err != nil {
			return nil, fmt.Errorf("capture seeded current-Sandbox user configuration %s: %w", file.Path, err)
		}
		result[index].Exists = true
		result[index].SHA256 = hash
	}
	return result, nil
}

func verifyCurrentSandboxUserConfiguration(files []preservedFile) error {
	for _, file := range files {
		info, inspectErr := os.Lstat(file.Path)
		if !file.Exists && errors.Is(inspectErr, os.ErrNotExist) {
			continue
		}
		if inspectErr != nil {
			return fmt.Errorf("inspect preserved current-Sandbox user configuration %s: %w", file.Path, inspectErr)
		}
		if !file.Exists {
			return fmt.Errorf("current-Sandbox candidate created preserved user configuration: %s", file.Path)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("preserved current-Sandbox user configuration is not a regular file: %s", file.Path)
		}
		hash, err := fileSHA256(file.Path)
		if err != nil {
			return fmt.Errorf("verify preserved current-Sandbox user configuration %s: %w", file.Path, err)
		}
		if !bytes.Equal(hash, file.SHA256) {
			return fmt.Errorf("current-Sandbox candidate changed preserved user configuration: %s", file.Path)
		}
	}
	return nil
}
