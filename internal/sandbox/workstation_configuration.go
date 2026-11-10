package sandbox

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// The local installer and SSH provisioning differ only in archive transport.
// Exported provider state and target transformations retain their existing owners.
func captureInstallerConfiguration(ctx context.Context, selection provisioningPlan, tradingView bool) ([]WorkstationFile, error) {
	credentials := selection.CredentialSync
	sources, err := defaultHostConfigurationSources(selection.WindowsTerminal, selection.Packages, selection.CodingAgentSync, credentials, tradingView)
	if err != nil {
		return nil, err
	}
	// Windows Terminal expands this on the target rather than retaining a source
	// drive or source-user directory. Managed Sandbox uses its workspace manifest.
	sources.StartingDirectory = `%USERPROFILE%`
	if selection.Packages.enabled(packageGitHubCLI) && credentials.GitHubCLI {
		sources.GitHubCLIAuthentication, _, err = exportGitHubCLIAuthentication(ctx, sources.GitHubCLIConfiguration)
		if err != nil {
			return nil, err
		}
		defer clear(sources.GitHubCLIAuthentication)
	}
	if tradingView {
		sources.TradingViewAuthentication, _, err = emptyTradingViewAuthenticationPayload()
		if credentials.TradingView {
			sources.TradingViewAuthentication, _, err = exportTradingViewAuthentication(ctx, sources.TradingViewProfile)
		}
		if err != nil {
			return nil, err
		}
		defer clear(sources.TradingViewAuthentication)
	}
	directory, err := os.MkdirTemp("", "herdr-installer-configuration-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(directory)
	plan, err := encodeWingetPackagePlan(selection.Packages, selection.WindowsTerminal)
	if err != nil {
		return nil, err
	}
	sources.PackagePlan = filepath.Join(directory, wingetPackagePlanFileName)
	if err := os.WriteFile(sources.PackagePlan, plan, 0o600); err != nil {
		return nil, err
	}
	archive, err := buildDevelopmentConfigurationArchive(ctx, sources, configurationSyncScript)
	if err != nil {
		return nil, err
	}
	return []WorkstationFile{{Kind: "configuration-archive", Path: "configuration.zip", SHA256: fmt.Sprintf("%x", sha256.Sum256(archive)), Data: archive}}, nil
}

func applyInstallerConfiguration(ctx context.Context, root string, file WorkstationFile, output io.Writer) error {
	if fmt.Sprintf("%x", sha256.Sum256(file.Data)) != file.SHA256 {
		return errors.New("configuration archive digest mismatch")
	}
	archive, err := zip.NewReader(bytes.NewReader(file.Data), int64(len(file.Data)))
	if err != nil {
		return errors.New("configuration archive is invalid")
	}
	directory, err := os.MkdirTemp(filepath.Join(root, "input"), "configuration-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(directory)
	expanded := filepath.Join(directory, "expanded")
	if err := os.Mkdir(expanded, 0o700); err != nil {
		return err
	}
	if len(archive.File) > maximumConfigurationFiles {
		return errors.New("configuration archive has too many entries")
	}
	var total uint64
	seen := map[string]bool{}
	for _, entry := range archive.File {
		name := strings.ReplaceAll(entry.Name, `\`, "/")
		path := filepath.Join(expanded, filepath.FromSlash(name))
		if !strings.HasPrefix(path, expanded+string(os.PathSeparator)) || filepath.IsAbs(name) || strings.Contains(name, ":") || seen[strings.ToLower(name)] || entry.Mode()&os.ModeSymlink != 0 {
			return errors.New("configuration archive contains an unsafe entry")
		}
		seen[strings.ToLower(name)] = true
		total += entry.UncompressedSize64
		if entry.UncompressedSize64 > maximumConfigurationFileSize || total > maximumConfigurationSize {
			return errors.New("configuration archive exceeds its size boundary")
		}
		if entry.FileInfo().IsDir() {
			if err := os.MkdirAll(path, 0o700); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return err
		}
		reader, err := entry.Open()
		if err != nil {
			return errors.New("configuration archive entry could not be opened")
		}
		data, readErr := io.ReadAll(io.LimitReader(reader, maximumConfigurationFileSize+1))
		closeErr := reader.Close()
		if readErr != nil || closeErr != nil || len(data) > maximumConfigurationFileSize {
			clear(data)
			return errors.New("configuration archive entry failed validation")
		}
		err = os.WriteFile(path, data, 0o600)
		clear(data)
		if err != nil {
			return err
		}
	}
	// The captured document carries the source shell preference. Merge it into
	// target-owned Herdr settings now, exactly as the SSH path does before transfer.
	herdrTarget := filepath.Join(os.Getenv("APPDATA"), "herdr", "config.toml")
	if err := validateWorkstationLocalPath(herdrTarget); err != nil {
		return err
	}
	targetConfiguration, targetFound, err := readBoundedRegularFile(herdrTarget, maximumConfigurationFileSize)
	if err != nil {
		return err
	}
	defer clear(targetConfiguration)
	herdrSource := filepath.Join(expanded, "herdr", "config.toml")
	merged, err := buildGuestHerdrConfig(herdrSource, targetConfiguration, "")
	if err != nil {
		return err
	}
	defer clear(merged)
	if err := os.WriteFile(herdrSource, merged, 0o600); err != nil {
		return err
	}
	expectedHerdr := "missing"
	if targetFound {
		expectedHerdr = fmt.Sprintf("%x", sha256.Sum256(targetConfiguration))
	}
	if err := os.WriteFile(filepath.Join(expanded, "herdr", "expected-sha256.txt"), []byte(expectedHerdr), 0o600); err != nil {
		return err
	}
	archivePath := filepath.Join(directory, "configuration.zip")
	if err := os.WriteFile(archivePath, file.Data, 0o600); err != nil {
		return err
	}
	scriptPath := filepath.Join(directory, "apply.ps1")
	if err := os.WriteFile(scriptPath, configurationSyncScript, 0o600); err != nil {
		return err
	}
	powerShell, err := windowsPowerShellExecutable()
	if err != nil {
		return err
	}
	applyContext, stop := context.WithTimeout(ctx, 10*time.Minute)
	defer stop()
	script := `$ErrorActionPreference='Stop'; $ProgressPreference='SilentlyContinue'; $env:PSModulePath=$PSHOME+'\Modules'; $env:Path=[Environment]::GetEnvironmentVariable('Path','Machine')+';'+[Environment]::GetEnvironmentVariable('Path','User'); & $env:HERDR_WORKSTATION_CONFIGURATION_SCRIPT -LocalTarget`
	command := hiddenCommandContext(applyContext, powerShell, "-NoLogo", "-NoProfile", "-NonInteractive", "-WindowStyle", "Hidden", "-ExecutionPolicy", "Bypass", "-EncodedCommand", encodePowerShell(script))
	command.Env = append(childProcessEnvironment(os.Environ()), "HERDR_SANDBOX_CONFIGURATION_ARCHIVE="+archivePath, "HERDR_SANDBOX_CONFIGURATION_EXPANDED="+expanded, "HERDR_WORKSTATION_CONFIGURATION_SCRIPT="+scriptPath, "HERDR_PROVISIONING_ROOT="+root)
	// Provider tools can include account material in errors. Keep their diagnostics
	// in memory, just as the authenticated transfer boundary does, never setup.log.
	var diagnostics bytes.Buffer
	command.Stderr = &diagnostics
	defer func() { clear(diagnostics.Bytes()) }()
	fmt.Fprintln(output, "Applying shared configuration and selected authentication locally...")
	result, err := command.Output()
	defer clear(result)
	if err != nil {
		return fmt.Errorf("apply shared configuration and authentication: %w (private diagnostics withheld)", err)
	}
	verified, err := decodeDevelopmentConfigurationSyncResult(result)
	if err != nil {
		return errors.New("shared configuration application did not publish a valid result")
	}
	if err := verifyDevelopmentConfigurationResult(file.Data, verified, true); err != nil {
		return err
	}
	if err := writeVoxCPM2JSONAtomically(filepath.Join(root, "configuration-result.json"), verified); err != nil {
		return err
	}
	fmt.Fprintln(output, "Shared configuration and authentication applied; configured provider checks completed.")
	return nil
}
