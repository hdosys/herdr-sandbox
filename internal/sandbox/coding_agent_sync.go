package sandbox

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const codingAgentSyncManifestArchivePath = "herdr-sandbox/coding-agent-sync.json"

type codingAgentConfigurationSources struct {
	Selection                codingAgentSyncConfiguration
	CredentialSync           credentialSyncConfiguration
	OpenCodeDirectory        string
	OpenCodeAuthentication   string
	ClaudeCodeDirectory      string
	ClaudeCodeState          string
	ClaudeCodeAuthentication string
	CodexDirectory           string
	CodexAuthentication      string
	CodexMCPAuthentication   string
	GitHubCopilotDirectory   string
	PiDirectory              string
	PiAuthentication         string
	SharedSkillsDirectory    string
}

type codingAgentSyncManifest struct {
	SchemaVersion        int                         `json:"schemaVersion"`
	OpenCode             bool                        `json:"opencode"`
	ClaudeCode           bool                        `json:"claudeCode"`
	Codex                bool                        `json:"codex"`
	GitHubCopilot        bool                        `json:"githubCopilot"`
	Pi                   bool                        `json:"pi"`
	CredentialSync       credentialSyncConfiguration `json:"credentialSync"`
	GitTrackedDeletions  map[string][]string         `json:"gitTrackedDeletions"`
	HerdrHookSourcePaths map[string]string           `json:"herdrHookSourcePaths"`
}

func newCodingAgentSyncManifest(configuration codingAgentSyncConfiguration, credentialSync credentialSyncConfiguration, gitTrackedDeletions map[string][]string, herdrHookSourcePaths map[string]string) codingAgentSyncManifest {
	return codingAgentSyncManifest{
		SchemaVersion:        4,
		OpenCode:             configuration.OpenCode,
		ClaudeCode:           configuration.ClaudeCode,
		Codex:                configuration.Codex,
		GitHubCopilot:        configuration.GitHubCopilot,
		Pi:                   configuration.Pi,
		CredentialSync:       credentialSync,
		GitTrackedDeletions:  gitTrackedDeletions,
		HerdrHookSourcePaths: herdrHookSourcePaths,
	}
}

func encodeCodingAgentSyncManifest(configuration codingAgentSyncConfiguration, credentialSync credentialSyncConfiguration, gitTrackedDeletions map[string][]string, herdrHookSourcePaths map[string]string) ([]byte, error) {
	data, err := json.MarshalIndent(newCodingAgentSyncManifest(configuration, credentialSync, gitTrackedDeletions, herdrHookSourcePaths), "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode coding-agent sync manifest: %w", err)
	}
	return append(data, '\n'), nil
}

func defaultCodingAgentConfigurationSources(userHome string, selection codingAgentSyncConfiguration, credentialSync credentialSyncConfiguration) (codingAgentConfigurationSources, error) {
	if !filepath.IsAbs(userHome) {
		return codingAgentConfigurationSources{}, fmt.Errorf("user home is not absolute: %q", userHome)
	}
	sources := codingAgentConfigurationSources{Selection: selection, CredentialSync: credentialSync}
	if selection.OpenCode || credentialSync.OpenCode {
		configuration, data, resolveErr := defaultOpenCodeDirectories(userHome)
		if resolveErr != nil {
			return codingAgentConfigurationSources{}, resolveErr
		}
		if selection.OpenCode {
			sources.OpenCodeDirectory = configuration
		}
		if credentialSync.OpenCode {
			sources.OpenCodeAuthentication = filepath.Join(data, "auth.json")
		}
	}
	if selection.ClaudeCode || credentialSync.ClaudeCode {
		claudeCodeDirectory, err := configuredAgentRoot(userHome, "CLAUDE_CONFIG_DIR", ".claude")
		if err != nil {
			return codingAgentConfigurationSources{}, err
		}
		if selection.ClaudeCode {
			sources.ClaudeCodeDirectory = claudeCodeDirectory
			if strings.TrimSpace(os.Getenv("CLAUDE_CONFIG_DIR")) == "" {
				sources.ClaudeCodeState = filepath.Join(userHome, ".claude.json")
			} else {
				sources.ClaudeCodeState = filepath.Join(claudeCodeDirectory, ".claude.json")
			}
		}
		if credentialSync.ClaudeCode {
			sources.ClaudeCodeAuthentication = filepath.Join(claudeCodeDirectory, ".credentials.json")
		}
	}
	if selection.Codex || credentialSync.Codex {
		codexDirectory, err := configuredAgentRoot(userHome, "CODEX_HOME", ".codex")
		if err != nil {
			return codingAgentConfigurationSources{}, err
		}
		if selection.Codex {
			sources.CodexDirectory = codexDirectory
		}
		if credentialSync.Codex {
			sources.CodexAuthentication = filepath.Join(codexDirectory, "auth.json")
			sources.CodexMCPAuthentication = filepath.Join(codexDirectory, ".credentials.json")
		}
	}
	if selection.GitHubCopilot {
		githubCopilotDirectory, err := configuredAgentRoot(userHome, "COPILOT_HOME", ".copilot")
		if err != nil {
			return codingAgentConfigurationSources{}, err
		}
		sources.GitHubCopilotDirectory = githubCopilotDirectory
	}
	if selection.Pi || credentialSync.Pi {
		piDirectory, err := configuredAgentRoot(userHome, "PI_CODING_AGENT_DIR", ".pi", "agent")
		if err != nil {
			return codingAgentConfigurationSources{}, err
		}
		if selection.Pi {
			sources.PiDirectory = piDirectory
		}
		if credentialSync.Pi {
			sources.PiAuthentication = filepath.Join(piDirectory, "auth.json")
		}
	}
	if selection.Codex || selection.GitHubCopilot || selection.Pi {
		sources.SharedSkillsDirectory = filepath.Join(userHome, ".agents", "skills")
	}
	return sources, nil
}

func configuredAgentRoot(userHome, environmentName string, defaultParts ...string) (string, error) {
	configured := strings.TrimSpace(os.Getenv(environmentName))
	if configured == "" {
		parts := append([]string{userHome}, defaultParts...)
		return filepath.Join(parts...), nil
	}
	if !filepath.IsAbs(configured) {
		return "", fmt.Errorf("%s is not absolute: %q", environmentName, configured)
	}
	return filepath.Clean(configured), nil
}

func archiveCodingAgentConfiguration(
	ctx context.Context,
	sources codingAgentConfigurationSources,
	add func(string, string) error,
	addData func([]byte, string, string) error,
) error {
	archivedDestinations := map[string]string{}
	gitTrackedDeletions := map[string][]string{}
	herdrHookSourcePaths := map[string]string{}
	addConfiguration := func(source, destination string) error {
		cleaned := filepath.Clean(destination)
		if filepath.IsAbs(cleaned) || filepath.VolumeName(cleaned) != "" || cleaned == "." || cleaned == ".." ||
			strings.HasPrefix(cleaned, ".."+string(os.PathSeparator)) {
			return fmt.Errorf("coding-agent archive destination is unsafe: %q", destination)
		}
		identity := strings.ToLower(filepath.ToSlash(cleaned))
		source = filepath.Clean(source)
		if previous, exists := archivedDestinations[identity]; exists {
			if strings.EqualFold(previous, source) {
				return nil
			}
			return fmt.Errorf("coding-agent archive destination collision %q from %s and %s", destination, previous, source)
		}
		archivedDestinations[identity] = source
		return add(source, cleaned)
	}
	archiveGit := func(directory, archiveRoot string) error {
		deleted, err := archiveAgentGitRepository(ctx, directory, archiveRoot, addConfiguration)
		if err == nil && len(deleted) > 0 {
			gitTrackedDeletions[archiveRoot] = deleted
		}
		return err
	}
	recordHerdrHookSource := func(target, destination string) {
		identity := strings.ToLower(filepath.ToSlash(filepath.Clean(destination)))
		if source, exists := archivedDestinations[identity]; exists {
			herdrHookSourcePaths[target] = source
		}
	}

	if sources.Selection.OpenCode {
		if err := archiveAllowedConfigurationRoot(sources.OpenCodeDirectory, "opencode",
			[]string{"opencode.json", "opencode.jsonc", "tui.json", "tui.jsonc", "AGENTS.md", "herdr-tui-session.js", "package.json", "package-lock.json", "bun.lock", "bun.lockb"},
			[]string{"agent", "agents", "command", "commands", "mode", "modes", "plugin", "plugins", "skill", "skills", "theme", "themes", "tool", "tools"}, nil, addConfiguration); err != nil {
			return fmt.Errorf("archive OpenCode configuration: %w", err)
		}
		if err := archiveGit(sources.OpenCodeDirectory, "opencode"); err != nil {
			return fmt.Errorf("archive OpenCode Git repository: %w", err)
		}
	}
	if sources.CredentialSync.OpenCode {
		if err := addOptionalConfigurationFile(sources.OpenCodeAuthentication, filepath.Join("opencode-auth", "auth.json"), addConfiguration); err != nil {
			return fmt.Errorf("archive OpenCode authentication: %w", err)
		}
	}

	if sources.Selection.ClaudeCode {
		if err := archiveAllowedConfigurationRoot(sources.ClaudeCodeDirectory, "claude-code",
			[]string{"settings.json", "keybindings.json", "CLAUDE.md", "loop.md"},
			[]string{"agents", "commands", "rules", "skills", "output-styles", "themes", "workflows"}, nil, addConfiguration); err != nil {
			return fmt.Errorf("archive Claude Code configuration: %w", err)
		}
		if err := addOptionalConfigurationFile(filepath.Join(sources.ClaudeCodeDirectory, "plugins", "known_marketplaces.json"), filepath.Join("claude-code", "plugins", "known_marketplaces.json"), addConfiguration); err != nil {
			return fmt.Errorf("archive Claude Code marketplace configuration: %w", err)
		}
		if err := addOptionalConfigurationFile(filepath.Join(sources.ClaudeCodeDirectory, "hooks", "herdr-agent-state.ps1"), filepath.Join("claude-code", "hooks", "herdr-agent-state.ps1"), addConfiguration); err != nil {
			return fmt.Errorf("archive Claude Code Herdr integration: %w", err)
		}
		if err := archiveGit(sources.ClaudeCodeDirectory, "claude-code"); err != nil {
			return fmt.Errorf("archive Claude Code Git repository: %w", err)
		}
		recordHerdrHookSource("claude", filepath.Join("claude-code", "hooks", "herdr-agent-state.ps1"))
		state, exists, err := buildClaudeCodeUserState(sources.ClaudeCodeState)
		if err != nil {
			return err
		}
		if exists {
			if err := addData(state, filepath.Join("claude-code-state", ".claude.json"), sources.ClaudeCodeState); err != nil {
				return fmt.Errorf("archive Claude Code user MCP configuration: %w", err)
			}
		}
	}
	if sources.CredentialSync.ClaudeCode {
		if err := addOptionalConfigurationFile(sources.ClaudeCodeAuthentication, filepath.Join("claude-code-auth", ".credentials.json"), addConfiguration); err != nil {
			return fmt.Errorf("archive Claude Code authentication: %w", err)
		}
	}

	if sources.Selection.Codex {
		if err := archiveCodexConfiguration(sources.CodexDirectory, addConfiguration); err != nil {
			return err
		}
		if err := archiveGit(sources.CodexDirectory, "codex"); err != nil {
			return fmt.Errorf("archive Codex Git repository: %w", err)
		}
		recordHerdrHookSource("codex", filepath.Join("codex", "herdr-agent-state.ps1"))
	}
	if sources.CredentialSync.Codex {
		if err := addOptionalConfigurationFile(sources.CodexAuthentication, filepath.Join("codex-auth", "auth.json"), addConfiguration); err != nil {
			return fmt.Errorf("archive Codex authentication: %w", err)
		}
		if err := addOptionalConfigurationFile(sources.CodexMCPAuthentication, filepath.Join("codex-auth", ".credentials.json"), addConfiguration); err != nil {
			return fmt.Errorf("archive Codex MCP authentication: %w", err)
		}
	}

	if sources.Selection.GitHubCopilot {
		if err := archiveAllowedConfigurationRoot(sources.GitHubCopilotDirectory, "github-copilot",
			[]string{"settings.json", "copilot-instructions.md", "mcp-config.json", "lsp-config.json"},
			[]string{"instructions", "agents", "skills", "hooks", "extensions"}, nil, addConfiguration); err != nil {
			return fmt.Errorf("archive GitHub Copilot configuration: %w", err)
		}
		if err := archiveGit(sources.GitHubCopilotDirectory, "github-copilot"); err != nil {
			return fmt.Errorf("archive GitHub Copilot Git repository: %w", err)
		}
		recordHerdrHookSource("copilot", filepath.Join("github-copilot", "hooks", "herdr-agent-state.ps1"))
	}

	if sources.Selection.Pi {
		if err := archiveAllowedConfigurationRoot(sources.PiDirectory, "pi",
			[]string{"settings.json", "models.json", "keybindings.json", "SYSTEM.md", "APPEND_SYSTEM.md", "package.json", "package-lock.json", "pnpm-lock.yaml", "yarn.lock", "bun.lock", "bun.lockb"},
			[]string{"prompts", "skills", "extensions", "themes"}, nil, addConfiguration); err != nil {
			return fmt.Errorf("archive Pi configuration: %w", err)
		}
		if err := archiveFirstOptionalConfigurationFile(sources.PiDirectory, []string{"AGENTS.md", "AGENTS.MD", "CLAUDE.md", "CLAUDE.MD"}, filepath.Join("pi", "AGENTS.md"), addConfiguration); err != nil {
			return fmt.Errorf("archive Pi instructions: %w", err)
		}
		if err := archiveGit(sources.PiDirectory, "pi"); err != nil {
			return fmt.Errorf("archive Pi Git repository: %w", err)
		}
	}
	if sources.CredentialSync.Pi {
		if err := addOptionalConfigurationFile(sources.PiAuthentication, filepath.Join("pi-auth", "auth.json"), addConfiguration); err != nil {
			return fmt.Errorf("archive Pi authentication: %w", err)
		}
	}

	if sources.SharedSkillsDirectory != "" {
		if err := archiveOptionalConfigurationTree(sources.SharedSkillsDirectory, "shared-agent-skills", nil, addConfiguration); err != nil {
			return fmt.Errorf("archive shared agent skills: %w", err)
		}
		if err := archiveGit(sources.SharedSkillsDirectory, "shared-agent-skills"); err != nil {
			return fmt.Errorf("archive shared agent skills Git repository: %w", err)
		}
	}
	manifest, err := encodeCodingAgentSyncManifest(sources.Selection, sources.CredentialSync, gitTrackedDeletions, herdrHookSourcePaths)
	if err != nil {
		return err
	}
	return addData(manifest, codingAgentSyncManifestArchivePath, "coding-agent sync manifest")
}

func archiveCodexConfiguration(directory string, add func(string, string) error) error {
	if directory == "" {
		return nil
	}
	entries, exists, err := readOptionalConfigurationDirectory(directory)
	if err != nil || !exists {
		return err
	}
	allowedFiles := map[string]bool{"config.toml": true, "AGENTS.md": true, "AGENTS.override.md": true, "hooks.json": true, "herdr-agent-state.ps1": true}
	for _, entry := range entries {
		if entry.IsDir() || (!allowedFiles[entry.Name()] && !strings.HasSuffix(entry.Name(), ".config.toml")) {
			continue
		}
		if err := add(filepath.Join(directory, entry.Name()), filepath.Join("codex", entry.Name())); err != nil {
			return fmt.Errorf("archive Codex config %s: %w", entry.Name(), err)
		}
	}
	for _, name := range []string{"agents", "rules", "hooks", "themes", "pets", "avatars"} {
		if err := archiveOptionalConfigurationTree(filepath.Join(directory, name), filepath.Join("codex", name), nil, add); err != nil {
			return err
		}
	}
	return archiveOptionalConfigurationTree(filepath.Join(directory, "skills"), filepath.Join("codex", "skills"), map[string]bool{".system": true}, add)
}

func archiveAllowedConfigurationRoot(directory, archiveRoot string, files, directories []string, excludedDirectoryNames map[string]bool, add func(string, string) error) error {
	if directory == "" {
		return nil
	}
	_, exists, err := readOptionalConfigurationDirectory(directory)
	if err != nil || !exists {
		return err
	}
	for _, name := range files {
		if err := addOptionalConfigurationFile(filepath.Join(directory, name), filepath.Join(archiveRoot, name), add); err != nil {
			return err
		}
	}
	for _, name := range directories {
		if err := archiveOptionalConfigurationTree(filepath.Join(directory, name), filepath.Join(archiveRoot, name), excludedDirectoryNames, add); err != nil {
			return err
		}
	}
	return nil
}

func addOptionalConfigurationFile(source, destination string, add func(string, string) error) error {
	if source == "" {
		return nil
	}
	info, err := os.Lstat(source)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return fmt.Errorf("configuration source is not a regular file: %s", source)
	}
	return add(source, destination)
}

func archiveFirstOptionalConfigurationFile(directory string, names []string, destination string, add func(string, string) error) error {
	if directory == "" {
		return nil
	}
	for _, name := range names {
		source := filepath.Join(directory, name)
		info, err := os.Lstat(source)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return fmt.Errorf("configuration source is not a regular file: %s", source)
		}
		return add(source, destination)
	}
	return nil
}

func readOptionalConfigurationDirectory(directory string) ([]os.DirEntry, bool, error) {
	info, err := os.Lstat(directory)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return nil, false, fmt.Errorf("configuration source is not a physical directory: %s", directory)
	}
	entries, err := os.ReadDir(directory)
	return entries, true, err
}

func archiveOptionalConfigurationTree(directory, archiveRoot string, excludedDirectoryNames map[string]bool, add func(string, string) error) error {
	_, exists, err := readOptionalConfigurationDirectory(directory)
	if err != nil || !exists {
		return err
	}
	excluded := make(map[string]bool, len(excludedDirectoryNames)+2)
	maps.Copy(excluded, excludedDirectoryNames)
	excluded["node_modules"] = true
	excluded[".git"] = true
	return addConfigurationTree(directory, archiveRoot, excluded, add)
}

func addConfigurationTree(directory, archiveRoot string, excludedDirectoryNames map[string]bool, add func(string, string) error) error {
	return filepath.WalkDir(directory, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("configuration directory contains a symbolic link: %s", path)
		}
		if entry.IsDir() {
			if path != directory && excludedDirectoryNames[entry.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		relative, err := filepath.Rel(directory, path)
		if err != nil {
			return err
		}
		return add(path, filepath.Join(archiveRoot, relative))
	})
}

func buildClaudeCodeUserState(path string) ([]byte, bool, error) {
	if path == "" {
		return nil, false, nil
	}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("inspect Claude Code user state: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Size() > maximumConfigurationFileSize {
		return nil, false, fmt.Errorf("user state for Claude Code is not a bounded regular file: %s", path)
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		return nil, false, fmt.Errorf("read Claude Code user state: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.UseNumber()
	var state map[string]any
	if err := decoder.Decode(&state); err != nil {
		return nil, false, fmt.Errorf("decode Claude Code user state: %w", err)
	}
	if state == nil {
		return nil, false, errors.New("decode Claude Code user state: root is not an object")
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, false, errors.New("decode Claude Code user state: trailing JSON data")
	}
	mcpServers, exists := state["mcpServers"]
	if !exists {
		return nil, false, nil
	}
	if _, ok := mcpServers.(map[string]any); !ok {
		return nil, false, errors.New("user MCP configuration for Claude Code is not an object")
	}
	portable, err := json.MarshalIndent(map[string]any{"mcpServers": mcpServers}, "", "  ")
	if err != nil {
		return nil, false, fmt.Errorf("encode Claude Code user MCP configuration: %w", err)
	}
	return append(portable, '\n'), true, nil
}

func codingAgentSyncNames(configuration codingAgentSyncConfiguration) []string {
	selected := []string{}
	for _, entry := range []struct {
		enabled bool
		name    string
	}{
		{configuration.OpenCode, "OpenCode"},
		{configuration.ClaudeCode, "Claude Code"},
		{configuration.Codex, "Codex"},
		{configuration.GitHubCopilot, "GitHub Copilot"},
		{configuration.Pi, "Pi"},
	} {
		if entry.enabled {
			selected = append(selected, entry.name)
		}
	}
	sort.Strings(selected)
	return selected
}

func credentialSyncNames(configuration credentialSyncConfiguration) []string {
	selected := []string{}
	for _, entry := range []struct {
		enabled bool
		name    string
	}{
		{configuration.OpenCode, "OpenCode"},
		{configuration.ClaudeCode, "Claude Code"},
		{configuration.Codex, "Codex"},
		{configuration.GitHubCLI, "GitHub CLI"},
		{configuration.Pi, "Pi"},
		{configuration.TradingView, "TradingView"},
	} {
		if entry.enabled {
			selected = append(selected, entry.name)
		}
	}
	sort.Strings(selected)
	return selected
}
