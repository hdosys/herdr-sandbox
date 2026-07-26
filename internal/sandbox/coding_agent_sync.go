package sandbox

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const codingAgentSyncManifestArchivePath = "herdr-sandbox/coding-agent-sync.json"

type codingAgentConfigurationSources struct {
	Selection                codingAgentSyncConfiguration
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
	SchemaVersion int  `json:"schemaVersion"`
	OpenCode      bool `json:"opencode"`
	ClaudeCode    bool `json:"claudeCode"`
	Codex         bool `json:"codex"`
	GitHubCopilot bool `json:"githubCopilot"`
	Pi            bool `json:"pi"`
}

func newCodingAgentSyncManifest(configuration codingAgentSyncConfiguration) codingAgentSyncManifest {
	return codingAgentSyncManifest{
		SchemaVersion: 1,
		OpenCode:      configuration.OpenCode,
		ClaudeCode:    configuration.ClaudeCode,
		Codex:         configuration.Codex,
		GitHubCopilot: configuration.GitHubCopilot,
		Pi:            configuration.Pi,
	}
}

func encodeCodingAgentSyncManifest(configuration codingAgentSyncConfiguration) ([]byte, error) {
	data, err := json.MarshalIndent(newCodingAgentSyncManifest(configuration), "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode coding-agent sync manifest: %w", err)
	}
	return append(data, '\n'), nil
}

func defaultCodingAgentConfigurationSources(userHome string, selection codingAgentSyncConfiguration) (codingAgentConfigurationSources, error) {
	if !filepath.IsAbs(userHome) {
		return codingAgentConfigurationSources{}, fmt.Errorf("user home is not absolute: %q", userHome)
	}
	sources := codingAgentConfigurationSources{Selection: selection}
	var err error
	if selection.OpenCode {
		configuration, data, resolveErr := defaultOpenCodeDirectories(userHome)
		if resolveErr != nil {
			return codingAgentConfigurationSources{}, resolveErr
		}
		sources.OpenCodeDirectory = configuration
		sources.OpenCodeAuthentication = filepath.Join(data, "auth.json")
	}
	if selection.ClaudeCode {
		sources.ClaudeCodeDirectory, err = configuredAgentRoot(userHome, "CLAUDE_CONFIG_DIR", ".claude")
		if err != nil {
			return codingAgentConfigurationSources{}, err
		}
		if strings.TrimSpace(os.Getenv("CLAUDE_CONFIG_DIR")) == "" {
			sources.ClaudeCodeState = filepath.Join(userHome, ".claude.json")
		} else {
			sources.ClaudeCodeState = filepath.Join(sources.ClaudeCodeDirectory, ".claude.json")
		}
		sources.ClaudeCodeAuthentication = filepath.Join(sources.ClaudeCodeDirectory, ".credentials.json")
	}
	if selection.Codex {
		sources.CodexDirectory, err = configuredAgentRoot(userHome, "CODEX_HOME", ".codex")
		if err != nil {
			return codingAgentConfigurationSources{}, err
		}
		sources.CodexAuthentication = filepath.Join(sources.CodexDirectory, "auth.json")
		sources.CodexMCPAuthentication = filepath.Join(sources.CodexDirectory, ".credentials.json")
	}
	if selection.GitHubCopilot {
		sources.GitHubCopilotDirectory, err = configuredAgentRoot(userHome, "COPILOT_HOME", ".copilot")
		if err != nil {
			return codingAgentConfigurationSources{}, err
		}
	}
	if selection.Pi {
		sources.PiDirectory, err = configuredAgentRoot(userHome, "PI_CODING_AGENT_DIR", ".pi", "agent")
		if err != nil {
			return codingAgentConfigurationSources{}, err
		}
		sources.PiAuthentication = filepath.Join(sources.PiDirectory, "auth.json")
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
	sources codingAgentConfigurationSources,
	add func(string, string) error,
	addData func([]byte, string, string) error,
) error {
	manifest, err := encodeCodingAgentSyncManifest(sources.Selection)
	if err != nil {
		return err
	}
	if err := addData(manifest, codingAgentSyncManifestArchivePath, "coding-agent sync manifest"); err != nil {
		return err
	}

	if sources.Selection.OpenCode {
		if err := archiveAllowedConfigurationRoot(sources.OpenCodeDirectory, "opencode",
			[]string{"opencode.json", "opencode.jsonc", "tui.json", "tui.jsonc", "AGENTS.md", "package.json", "package-lock.json", "bun.lock", "bun.lockb"},
			[]string{"agent", "agents", "command", "commands", "mode", "modes", "plugin", "plugins", "skill", "skills", "theme", "themes", "tool", "tools"}, nil, add); err != nil {
			return fmt.Errorf("archive OpenCode configuration: %w", err)
		}
		if err := addOptionalConfigurationFile(sources.OpenCodeAuthentication, filepath.Join("opencode-auth", "auth.json"), add); err != nil {
			return fmt.Errorf("archive OpenCode authentication: %w", err)
		}
	}

	if sources.Selection.ClaudeCode {
		if err := archiveAllowedConfigurationRoot(sources.ClaudeCodeDirectory, "claude-code",
			[]string{"settings.json", "keybindings.json", "CLAUDE.md", "loop.md"},
			[]string{"agents", "commands", "rules", "skills", "output-styles", "themes", "workflows"}, nil, add); err != nil {
			return fmt.Errorf("archive Claude Code configuration: %w", err)
		}
		if err := addOptionalConfigurationFile(filepath.Join(sources.ClaudeCodeDirectory, "plugins", "known_marketplaces.json"), filepath.Join("claude-code", "plugins", "known_marketplaces.json"), add); err != nil {
			return fmt.Errorf("archive Claude Code marketplace configuration: %w", err)
		}
		state, exists, err := buildClaudeCodeUserState(sources.ClaudeCodeState)
		if err != nil {
			return err
		}
		if exists {
			if err := addData(state, filepath.Join("claude-code-state", ".claude.json"), sources.ClaudeCodeState); err != nil {
				return fmt.Errorf("archive Claude Code user MCP configuration: %w", err)
			}
		}
		if err := addOptionalConfigurationFile(sources.ClaudeCodeAuthentication, filepath.Join("claude-code-auth", ".credentials.json"), add); err != nil {
			return fmt.Errorf("archive Claude Code authentication: %w", err)
		}
	}

	if sources.Selection.Codex {
		if err := archiveCodexConfiguration(sources.CodexDirectory, add); err != nil {
			return err
		}
		if err := addOptionalConfigurationFile(sources.CodexAuthentication, filepath.Join("codex-auth", "auth.json"), add); err != nil {
			return fmt.Errorf("archive Codex authentication: %w", err)
		}
		if err := addOptionalConfigurationFile(sources.CodexMCPAuthentication, filepath.Join("codex-auth", ".credentials.json"), add); err != nil {
			return fmt.Errorf("archive Codex MCP authentication: %w", err)
		}
	}

	if sources.Selection.GitHubCopilot {
		if err := archiveAllowedConfigurationRoot(sources.GitHubCopilotDirectory, "github-copilot",
			[]string{"settings.json", "copilot-instructions.md", "mcp-config.json", "lsp-config.json"},
			[]string{"instructions", "agents", "skills", "hooks", "extensions"}, nil, add); err != nil {
			return fmt.Errorf("archive GitHub Copilot configuration: %w", err)
		}
	}

	if sources.Selection.Pi {
		if err := archiveAllowedConfigurationRoot(sources.PiDirectory, "pi",
			[]string{"settings.json", "models.json", "keybindings.json", "SYSTEM.md", "APPEND_SYSTEM.md", "package.json", "package-lock.json", "pnpm-lock.yaml", "yarn.lock", "bun.lock", "bun.lockb"},
			[]string{"prompts", "skills", "extensions", "themes"}, nil, add); err != nil {
			return fmt.Errorf("archive Pi configuration: %w", err)
		}
		if err := archiveFirstOptionalConfigurationFile(sources.PiDirectory, []string{"AGENTS.md", "AGENTS.MD", "CLAUDE.md", "CLAUDE.MD"}, filepath.Join("pi", "AGENTS.md"), add); err != nil {
			return fmt.Errorf("archive Pi instructions: %w", err)
		}
		if err := addOptionalConfigurationFile(sources.PiAuthentication, filepath.Join("pi-auth", "auth.json"), add); err != nil {
			return fmt.Errorf("archive Pi authentication: %w", err)
		}
	}

	if sources.SharedSkillsDirectory != "" {
		if err := archiveOptionalConfigurationTree(sources.SharedSkillsDirectory, "shared-agent-skills", nil, add); err != nil {
			return fmt.Errorf("archive shared agent skills: %w", err)
		}
	}
	return nil
}

func archiveCodexConfiguration(directory string, add func(string, string) error) error {
	if directory == "" {
		return nil
	}
	entries, exists, err := readOptionalConfigurationDirectory(directory)
	if err != nil || !exists {
		return err
	}
	allowedFiles := map[string]bool{"config.toml": true, "AGENTS.md": true, "AGENTS.override.md": true, "hooks.json": true}
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
	return filepath.WalkDir(directory, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("configuration directory contains a symbolic link: %s", path)
		}
		if entry.IsDir() {
			if path != directory && (excludedDirectoryNames[entry.Name()] || entry.Name() == "node_modules" || entry.Name() == ".git") {
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
		return nil, false, fmt.Errorf("Claude Code user state is not a bounded regular file: %s", path)
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
		return nil, false, errors.New("Claude Code user MCP configuration is not an object")
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
