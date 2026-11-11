package sandbox

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// ExportWorkstationProfile captures the selected software and optional shared
// configuration snapshot. Credential capture follows the selected sync settings.
func ExportWorkstationProfile(ctx context.Context, destination string, includeSettings, defaults bool, output io.Writer) error {
	profile := DefaultWorkstationProfile()
	var selection provisioningPlan
	if !defaults {
		configurationRoot, err := os.UserConfigDir()
		if err != nil {
			return err
		}
		executable, err := os.Executable()
		if err != nil {
			return err
		}
		globalRoot := filepath.Join(configurationRoot, applicationName)
		selection, err = resolveProvisioningReadOnlyAt("", globalRoot, filepath.Dir(executable))
		if err != nil {
			return fmt.Errorf("resolve selected old-machine configuration (use --defaults for an explicit generic profile): %w", err)
		}
		terminal, err := detectHostWindowsTerminalConfiguration()
		if err != nil {
			return err
		}
		packages, err := resolveWingetPackagePlan(selection.PackageConfiguration, terminal)
		if err != nil {
			return err
		}
		selection.Packages = packages
		selection.WindowsTerminal = terminal
		profile, _, err = workstationSelectionFromPlan(ctx, selection)
		if err != nil {
			return err
		}
	}
	if includeSettings {
		if defaults {
			local := strings.TrimSpace(os.Getenv("LOCALAPPDATA"))
			if !filepath.IsAbs(local) {
				return errors.New("settings require absolute LOCALAPPDATA")
			}
			terminal := windowsTerminalConfiguration{
				Edition:           windowsTerminalStableEdition,
				Theme:             windowsTerminalDarkTheme,
				WinGetPackageID:   packageTerminalStable,
				PackageFamilyName: "Microsoft.WindowsTerminal_8wekyb3d8bbwe",
				SettingsPath:      filepath.Join(local, "Packages", "Microsoft.WindowsTerminal_8wekyb3d8bbwe", "LocalState", "settings.json"),
				FragmentsPath:     filepath.Join(local, "Microsoft", "Windows Terminal", "Fragments"),
			}
			found, err := regularFileExists(terminal.SettingsPath)
			if err != nil {
				return err
			}
			if found {
				terminal.Theme, err = readHostWindowsTerminalTheme(terminal.SettingsPath)
				if err != nil {
					return err
				}
			} else {
				terminal.SettingsPath = ""
			}
			packages, err := resolveWingetPackagePlan(defaultWingetPackageConfiguration(), terminal)
			if err != nil {
				return err
			}
			selection = provisioningPlan{
				Packages: packages, WindowsTerminal: terminal,
				CodingAgentSync: defaultCodingAgentSyncConfiguration(),
				CredentialSync:  credentialSyncConfiguration{},
			}
		}
		var err error
		profile.Files, err = captureInstallerConfiguration(ctx, selection, slices.Contains(profile.Stacks, "tradingview"))
		if err != nil {
			return err
		}
		defer func() {
			for _, file := range profile.Files {
				clear(file.Data)
			}
		}()
		fmt.Fprintln(output, "PRIVATE export includes selected configuration and supported authentication. Possession may grant account access. Never publish the profile or installer.")
	}
	data, err := json.Marshal(profile)
	if err != nil {
		return err
	}
	defer clear(data)
	if _, err := decodeWorkstationProfile(data); err != nil {
		return err
	}
	absolute, err := filepath.Abs(destination)
	if err != nil {
		return err
	}
	if err := validateWorkstationLocalPath(absolute); err != nil {
		return err
	}
	file, err := os.OpenFile(absolute, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("create new export without overwriting existing data: %w", err)
	}
	_, writeErr := file.Write(data)
	err = errors.Join(writeErr, file.Sync(), file.Close())
	if err != nil {
		return fmt.Errorf("write private workstation export: %w", err)
	}
	fmt.Fprintf(output, "Exported %d package selections, %d stacks and %d configuration files to %s\n", len(profile.Packages), len(profile.Stacks), len(profile.Files), absolute)
	return nil
}

func workstationSelectionFromPlan(ctx context.Context, selection provisioningPlan) (WorkstationProfile, []workspacePlan, error) {
	profile := WorkstationProfile{SchemaVersion: 1, Packages: append(slices.Clone(selection.Packages.Defaults), selection.Packages.Additions...), Stacks: []string{"opensrc", "font"}, Tools: []resolvedToolVersion{}, Files: []WorkstationFile{}}
	if !slices.ContainsFunc(profile.Packages, func(entry wingetPackagePlanEntry) bool { return strings.EqualFold(entry.ID, "hdosys.herdr-win") }) {
		profile.Packages = append(profile.Packages, wingetPackagePlanEntry{ID: "hdosys.herdr-win"})
	}
	userExists, err := regularFileExists(selection.UserScript)
	if err != nil {
		return profile, nil, err
	}
	workspaces := selection.Workspaces
	if userExists || len(workspaces) > 0 {
		inspection, err := inspectEffectivePlanScripts(ctx, selection, userExists)
		if err != nil {
			return profile, nil, err
		}
		workspaces = inspection.Workspaces
		stacks := map[string]bool{"opensrc": true, "font": true}
		for _, stack := range inspection.UserStacks {
			stacks[string(stack)] = true
		}
		for _, workspace := range workspaces {
			for _, stack := range workspace.Stacks {
				stacks[string(stack)] = true
			}
		}
		profile.Stacks = []string{}
		for stack := range stacks {
			profile.Stacks = append(profile.Stacks, stack)
		}
		slices.Sort(profile.Stacks)
		profile.Tools = inspection.ToolVersions
		if profile.Tools == nil {
			profile.Tools = []resolvedToolVersion{}
		}
		for index := range profile.Tools {
			profile.Tools[index].Owners = []string{"workstation"}
		}
	}
	return profile, workspaces, nil
}
