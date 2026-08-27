package sandbox

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type HostConfigurationPullResult struct {
	Pulled  []string
	Skipped []string
}

type hostConfigurationGitRoot struct {
	Name      string
	Directory string
}

type hostConfigurationPullState struct {
	Result         HostConfigurationPullResult
	GitDirectories []os.FileInfo
}

func PullHostConfiguration(ctx context.Context) (HostConfigurationPullResult, error) {
	_, path, err := loadDefaultGlobalConfiguration()
	if err != nil {
		return HostConfigurationPullResult{}, err
	}
	return pullSelectedHostConfiguration(ctx, path, nil)
}

func PullHostConfigurationOnUp(ctx context.Context) (HostConfigurationPullResult, error) {
	configuration, path, err := loadDefaultGlobalConfiguration()
	if err != nil {
		return HostConfigurationPullResult{}, err
	}
	if !configuration.ConfigurationSync.PullHostGitRepositoriesOnUp {
		return HostConfigurationPullResult{}, nil
	}
	return pullSelectedHostConfiguration(ctx, path, func(current globalConfiguration) bool {
		return current.ConfigurationSync.PullHostGitRepositoriesOnUp
	})
}

func PullHostConfigurationOnDown(ctx context.Context) (HostConfigurationPullResult, error) {
	configuration, path, err := loadDefaultGlobalConfiguration()
	if err != nil {
		return HostConfigurationPullResult{}, err
	}
	if !configuration.ConfigurationSync.PullHostGitRepositoriesOnDown {
		return HostConfigurationPullResult{}, nil
	}
	return pullSelectedHostConfiguration(ctx, path, func(current globalConfiguration) bool {
		return current.ConfigurationSync.PullHostGitRepositoriesOnDown
	})
}

func pullSelectedHostConfiguration(ctx context.Context, configurationPath string, continueAfterReload func(globalConfiguration) bool) (HostConfigurationPullResult, error) {
	state := &hostConfigurationPullState{Result: HostConfigurationPullResult{Pulled: []string{}, Skipped: []string{}}}
	err := pullHostConfigurationGitRootsInto(ctx, []hostConfigurationGitRoot{{
		Name:      "Herdr Sandbox configuration",
		Directory: filepath.Dir(configurationPath),
	}}, state)
	if err != nil {
		return state.Result, err
	}
	configuration, err := loadGlobalConfiguration(configurationPath)
	if err != nil {
		return state.Result, fmt.Errorf("reload Herdr Sandbox configuration after host Git pull: %w", err)
	}
	if continueAfterReload != nil && !continueAfterReload(configuration) {
		return state.Result, nil
	}
	sources, err := defaultHostConfigurationSourcesForPull(configuration)
	if err != nil {
		return state.Result, err
	}
	err = pullHostConfigurationGitRootsInto(ctx, hostConfigurationGitRoots(sources), state)
	sort.Strings(state.Result.Pulled)
	sort.Strings(state.Result.Skipped)
	return state.Result, err
}

func defaultHostConfigurationSourcesForPull(configuration globalConfiguration) (hostConfigurationSources, error) {
	roamingAppData := strings.TrimSpace(os.Getenv("APPDATA"))
	if !filepath.IsAbs(roamingAppData) {
		return hostConfigurationSources{}, fmt.Errorf("APPDATA is not absolute: %q", roamingAppData)
	}
	localAppData := strings.TrimSpace(os.Getenv("LOCALAPPDATA"))
	if !filepath.IsAbs(localAppData) {
		return hostConfigurationSources{}, fmt.Errorf("LOCALAPPDATA is not absolute: %q", localAppData)
	}
	terminal, err := detectHostWindowsTerminal(localAppData)
	if err != nil {
		return hostConfigurationSources{}, err
	}
	packages, err := resolveWingetPackagePlan(configuration.WingetPackages, terminal)
	if err != nil {
		return hostConfigurationSources{}, err
	}
	return defaultHostConfigurationSources(terminal, packages, configuration.CodingAgentSync, credentialSyncConfiguration{}, false)
}

func hostConfigurationGitRoots(sources hostConfigurationSources) []hostConfigurationGitRoot {
	roots := []hostConfigurationGitRoot{
		{Name: "Herdr configuration", Directory: filepath.Dir(sources.HerdrConfig)},
		{Name: "OpenCode configuration", Directory: sources.CodingAgents.OpenCodeDirectory},
		{Name: "Claude Code configuration", Directory: sources.CodingAgents.ClaudeCodeDirectory},
		{Name: "Codex configuration", Directory: sources.CodingAgents.CodexDirectory},
		{Name: "GitHub Copilot configuration", Directory: sources.CodingAgents.GitHubCopilotDirectory},
		{Name: "Pi configuration", Directory: sources.CodingAgents.PiDirectory},
		{Name: "shared agent skills", Directory: sources.CodingAgents.SharedSkillsDirectory},
		{Name: "Git configuration", Directory: sources.GitConfigDirectory},
		{Name: "GitHub CLI configuration", Directory: sources.GitHubCLIConfiguration},
		{Name: "Windows Terminal settings", Directory: configurationFileRoot(sources.WindowsTerminalSettings)},
		{Name: "Windows Terminal fragments", Directory: sources.WindowsTerminalFragments},
	}
	return roots
}

func configurationFileRoot(path string) string {
	if path == "" {
		return ""
	}
	return filepath.Dir(path)
}

func pullHostConfigurationGitRoots(ctx context.Context, roots []hostConfigurationGitRoot) (HostConfigurationPullResult, error) {
	state := &hostConfigurationPullState{Result: HostConfigurationPullResult{Pulled: []string{}, Skipped: []string{}}}
	err := pullHostConfigurationGitRootsInto(ctx, roots, state)
	sort.Strings(state.Result.Pulled)
	sort.Strings(state.Result.Skipped)
	return state.Result, err
}

func pullHostConfigurationGitRootsInto(ctx context.Context, roots []hostConfigurationGitRoot, state *hostConfigurationPullState) error {
	for _, root := range roots {
		if root.Directory == "" {
			continue
		}
		directory := filepath.Clean(root.Directory)
		gitDirectory := filepath.Join(directory, ".git")
		info, err := os.Lstat(gitDirectory)
		if errors.Is(err, os.ErrNotExist) {
			state.Result.Skipped = append(state.Result.Skipped, root.Name+": not a Git repository")
			continue
		}
		if err != nil {
			return fmt.Errorf("inspect %s Git metadata: %w", root.Name, err)
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return fmt.Errorf("%s Git metadata must be one physical .git directory: %s", root.Name, gitDirectory)
		}
		duplicate := false
		for _, previous := range state.GitDirectories {
			if os.SameFile(previous, info) {
				duplicate = true
				break
			}
		}
		if duplicate {
			continue
		}
		state.GitDirectories = append(state.GitDirectories, info)
		pulled, err := updateHostConfigurationGitRepository(ctx, directory)
		if err != nil {
			return fmt.Errorf("pull %s: %w", root.Name, err)
		}
		if pulled {
			state.Result.Pulled = append(state.Result.Pulled, root.Name)
		} else {
			state.Result.Skipped = append(state.Result.Skipped, root.Name+": no remotes")
		}
	}
	return nil
}

func loadDefaultGlobalConfiguration() (globalConfiguration, string, error) {
	configurationRoot, err := os.UserConfigDir()
	if err != nil {
		return globalConfiguration{}, "", fmt.Errorf("resolve user configuration directory: %w", err)
	}
	if !filepath.IsAbs(configurationRoot) {
		return globalConfiguration{}, "", fmt.Errorf("user configuration directory is not absolute: %q", configurationRoot)
	}
	path := filepath.Join(configurationRoot, applicationName, globalConfigurationName)
	configuration, err := loadGlobalConfiguration(path)
	return configuration, path, err
}
