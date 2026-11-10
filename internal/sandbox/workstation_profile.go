package sandbox

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
)

func allowedWorkstationFile(kind, relative string) bool {
	if relative == "" || len(relative) > 240 || path.Clean(relative) != relative || strings.HasPrefix(relative, "/") || strings.ContainsAny(relative, `\:`+"\x00\r\n") {
		return false
	}
	for component := range strings.SplitSeq(strings.ToLower(relative), "/") {
		if component == ".." || component == "." || strings.HasSuffix(component, ".") || strings.HasSuffix(component, " ") {
			return false
		}
		if slices.Contains([]string{".git", ".agent", ".ssh", ".venv", "node_modules", "__pycache__", "sessions", "logs", "cache", "auth", "credentials"}, component) {
			return false
		}
		if strings.Contains(component, ".env") || slices.Contains([]string{"auth.json", ".credentials.json", "credentials.json", "oauth_creds.json", "hosts.yml", "accounts.json", "id_rsa", "id_ed25519", "sandbox-allow-all.js"}, component) {
			return false
		}
		stem, _, _ := strings.Cut(component, ".")
		if slices.Contains([]string{"con", "prn", "aux", "nul", "com1", "com2", "com3", "com4", "com5", "com6", "com7", "com8", "com9", "lpt1", "lpt2", "lpt3", "lpt4", "lpt5", "lpt6", "lpt7", "lpt8", "lpt9"}, stem) {
			return false
		}
	}
	switch kind {
	case "herdr":
		return relative == "config.toml"
	case "github-cli":
		return relative == "config.yml"
	case "claude-state":
		return relative == ".claude.json"
	case "git":
		return slices.Contains([]string{".gitconfig", ".gitignore_global", ".gitattributes"}, relative)
	case "git-config":
		return true
	case "terminal", "terminal-preview":
		return relative == "settings.json"
	case "starship":
		return relative == "starship.toml"
	case "nushell":
		return relative == "config.nu" || relative == "env.nu" || (strings.HasPrefix(relative, "scripts/") && path.Ext(relative) == ".nu")
	case "terminal-fragments":
		return strings.EqualFold(path.Ext(relative), ".json")
	case "zed":
		return slices.Contains([]string{"settings.json", "keymap.json", "tasks.json"}, relative) || ((strings.HasPrefix(relative, "snippets/") || strings.HasPrefix(relative, "themes/")) && strings.EqualFold(path.Ext(relative), ".json"))
	case "copilot":
		if slices.Contains(copilotConfigurationFiles, relative) {
			return true
		}
		for _, directory := range copilotConfigurationDirectories {
			if strings.HasPrefix(relative, directory+"/") {
				return true
			}
		}
		return false
	case "opencode", "claude", "codex", "pi", "agent-skills":
		// Export selects authored roots through the normal configuration owner.
		// Referenced skill scripts/assets must not disappear by file extension.
		return true
	default:
		return false
	}
}

func workstationSettingsRoots() (map[string]string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	appData, local := os.Getenv("APPDATA"), os.Getenv("LOCALAPPDATA")
	for _, root := range []string{home, appData, local} {
		if !filepath.IsAbs(root) {
			return nil, errors.New("settings require absolute user profile, APPDATA and LOCALAPPDATA")
		}
	}
	openCode, _, err := defaultOpenCodeDirectories(home)
	if err != nil {
		return nil, err
	}
	github, err := defaultGitHubCLIConfigurationDirectory(home, appData)
	if err != nil {
		return nil, err
	}
	return map[string]string{
		"github-cli": github,
		"herdr":      filepath.Join(appData, "herdr"), "opencode": openCode,
		"claude": filepath.Join(home, ".claude"), "claude-state": home, "codex": filepath.Join(home, ".codex"), "pi": filepath.Join(home, ".pi", "agent"),
		"copilot": filepath.Join(home, ".copilot"), "agent-skills": filepath.Join(home, ".agents", "skills"),
		"git": home, "zed": filepath.Join(appData, "Zed"),
		"git-config": filepath.Join(home, ".config", "git"),
		"starship":   filepath.Join(home, ".config"), "nushell": filepath.Join(appData, "nushell"),
		"terminal":           filepath.Join(local, "Packages", "Microsoft.WindowsTerminal_8wekyb3d8bbwe", "LocalState"),
		"terminal-preview":   filepath.Join(local, "Packages", "Microsoft.WindowsTerminalPreview_8wekyb3d8bbwe", "LocalState"),
		"terminal-fragments": filepath.Join(local, "Microsoft", "Windows Terminal", "Fragments"),
	}, nil
}

// ExportWorkstationProfile performs explicit capture only. It never follows Git
// repositories/history or dedicated auth stores and never runs exported scripts.
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
		var err error
		if defaults {
			var roots map[string]string
			roots, err = workstationSettingsRoots()
			if err == nil {
				profile.Files, err = exportWorkstationSettings(roots)
			}
		} else {
			profile.Files, err = selectedInstallerSettings(selection)
		}
		if err != nil {
			return err
		}
		defer func() {
			for _, file := range profile.Files {
				clear(file.Data)
			}
		}()
		fmt.Fprintln(output, "Private export: authored settings/scripts may contain inline private values. Known authentication files, SSH keys, Git history and project data are excluded. Do not publish this profile.")
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

func exportWorkstationSettings(roots map[string]string) ([]WorkstationFile, error) {
	result := []WorkstationFile{}
	add := func(kind, relative, source string) error {
		if !allowedWorkstationFile(kind, relative) {
			return nil
		}
		if err := validateWorkstationLocalPath(source); err != nil {
			return err
		}
		data, found, err := readBoundedRegularFile(source, 4*1024*1024)
		if err != nil {
			return fmt.Errorf("read selected configuration %s/%s: %w", kind, relative, err)
		}
		if !found {
			return nil
		}
		result = append(result, WorkstationFile{Kind: kind, Path: relative, SHA256: fmt.Sprintf("%x", sha256.Sum256(data)), Data: data})
		if len(result) > 512 {
			return errors.New("too many configuration files for one workstation profile")
		}
		return nil
	}
	for kind, root := range roots {
		if err := validateWorkstationLocalPath(root); err != nil {
			return nil, err
		}
		if kind == "claude-state" {
			source := filepath.Join(root, ".claude.json")
			if err := validateWorkstationLocalPath(source); err != nil {
				return nil, err
			}
			data, found, err := buildClaudeCodeUserState(source)
			if err != nil {
				return nil, err
			}
			if found {
				result = append(result, WorkstationFile{Kind: kind, Path: ".claude.json", SHA256: fmt.Sprintf("%x", sha256.Sum256(data)), Data: data})
			}
			continue
		}
		// Reuse the authored-configuration selector, not an extension-based walk
		// over an agent's runtime, debug or project-state directories.
		var files, directories []string
		switch kind {
		case "opencode":
			files, directories = openCodeConfigurationFiles, openCodeConfigurationDirectories
		case "claude":
			files, directories = claudeConfigurationFiles, claudeConfigurationDirectories
		case "copilot":
			files, directories = copilotConfigurationFiles, copilotConfigurationDirectories
		case "pi":
			files, directories = piConfigurationFiles, piConfigurationDirectories
		}
		if files != nil || kind == "codex" {
			capture := func(source, destination string) error {
				return add(kind, strings.TrimPrefix(filepath.ToSlash(destination), kind+"/"), source)
			}
			var err error
			if kind == "codex" {
				err = archiveCodexConfiguration(root, capture)
			} else {
				err = archiveAllowedConfigurationRoot(root, kind, files, directories, nil, capture)
			}
			if err != nil {
				return nil, err
			}
			if kind == "pi" {
				if err := archiveFirstOptionalConfigurationFile(root, []string{"AGENTS.md", "AGENTS.MD", "CLAUDE.md", "CLAUDE.MD"}, "pi/AGENTS.md", capture); err != nil {
					return nil, err
				}
			}
			continue
		}
		if kind == "git" {
			for _, name := range []string{".gitconfig", ".gitignore_global", ".gitattributes"} {
				if err := add(kind, name, filepath.Join(root, name)); err != nil {
					return nil, err
				}
			}
			continue
		}
		if kind == "herdr" {
			source := filepath.Join(root, "config.toml")
			if selected, ok := os.LookupEnv("HERDR_CONFIG_PATH"); ok {
				if selected == "" {
					return nil, errors.New("HERDR_CONFIG_PATH is empty")
				}
				source = selected
			}
			if err := add(kind, "config.toml", source); err != nil {
				return nil, err
			}
			continue
		}
		if kind == "starship" || kind == "terminal" || kind == "terminal-preview" {
			name := "settings.json"
			if kind == "starship" {
				name = "starship.toml"
			}
			if err := add(kind, name, filepath.Join(root, name)); err != nil {
				return nil, err
			}
			continue
		}
		err := filepath.WalkDir(root, func(source string, entry fs.DirEntry, walkErr error) error {
			if errors.Is(walkErr, os.ErrNotExist) && source == root {
				return nil
			}
			if walkErr != nil {
				return walkErr
			}
			if source == root {
				return nil
			}
			relative, err := filepath.Rel(root, source)
			if err != nil {
				return err
			}
			relative = filepath.ToSlash(relative)
			if entry.IsDir() {
				// A synthetic extension checks directory exclusions without reading data.
				syntheticLeaf := "/selection.json"
				if kind == "nushell" {
					syntheticLeaf = "/selection.nu"
				}
				if kind == "git-config" {
					syntheticLeaf = "/config"
				}
				if !allowedWorkstationFile(kind, relative+syntheticLeaf) {
					return filepath.SkipDir
				}
				if err := validateWorkstationLocalPath(source); err != nil {
					return err
				}
				return nil
			}
			return add(kind, relative, source)
		})
		if err != nil {
			return nil, fmt.Errorf("export selected %s configuration: %w", kind, err)
		}
	}
	slices.SortFunc(result, func(left, right WorkstationFile) int {
		return strings.Compare(left.Kind+"/"+left.Path, right.Kind+"/"+right.Path)
	})
	return result, nil
}

func importWorkstationSettings(files []WorkstationFile) []WorkstationStep {
	results := []WorkstationStep{}
	if len(files) == 0 {
		return results
	}
	roots, err := workstationSettingsRoots()
	if err != nil {
		return []WorkstationStep{{"Personal configuration", "failed", err.Error()}}
	}
	for _, file := range files {
		step := WorkstationStep{Name: "Configuration " + file.Kind + "/" + file.Path, Status: "succeeded"}
		err := importWorkstationFile(roots[file.Kind], file)
		if err != nil {
			step.Status, step.Message = "failed", err.Error()
		}
		results = append(results, step)
	}
	return results
}

func importWorkstationFile(root string, file WorkstationFile) error {
	if root == "" || !allowedWorkstationFile(file.Kind, file.Path) || fmt.Sprintf("%x", sha256.Sum256(file.Data)) != file.SHA256 {
		return errors.New("configuration selection or integrity is invalid")
	}
	destination := filepath.Join(root, filepath.FromSlash(file.Path))
	if err := validateWorkstationLocalPath(destination); err != nil {
		return err
	}
	data, found, err := readBoundedRegularFile(destination, 4*1024*1024)
	if err != nil {
		return err
	}
	if found {
		defer clear(data)
		if bytes.Equal(data, file.Data) {
			return nil
		}
		return errors.New("different existing configuration was preserved; reconcile it explicitly before retrying")
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		return err
	}
	if err := validateWorkstationLocalPath(destination); err != nil {
		return err
	}
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, writeErr := output.Write(file.Data)
	return errors.Join(writeErr, output.Sync(), output.Close())
}
