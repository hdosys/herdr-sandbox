package sandbox

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
)

func TestLoadGlobalConfigurationDefaultsAndOverridesCodingAgentSync(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	writeTestFile(t, path, `{"codingAgentSync":{"codex":false}}`)
	configuration, err := loadGlobalConfiguration(path)
	if err != nil {
		t.Fatalf("loadGlobalConfiguration: %v", err)
	}
	want := codingAgentSyncConfiguration{OpenCode: true, ClaudeCode: true, Codex: false, GitHubCopilot: true, Pi: true}
	if configuration.CodingAgentSync != want {
		t.Fatalf("codingAgentSync = %#v, want %#v", configuration.CodingAgentSync, want)
	}

	missing, err := loadGlobalConfiguration(filepath.Join(t.TempDir(), "missing.json"))
	if err != nil {
		t.Fatalf("load missing global configuration: %v", err)
	}
	if missing.CodingAgentSync != defaultCodingAgentSyncConfiguration() {
		t.Fatalf("missing config codingAgentSync = %#v", missing.CodingAgentSync)
	}
}

func TestLoadGlobalConfigurationRejectsInvalidCodingAgentSync(t *testing.T) {
	for _, input := range []string{
		`{"codingAgentSync":null}`,
		`{"codingAgentSync":false}`,
		`{"codingAgentSync":{"codex":null}}`,
		`{"codingAgentSync":{"codex":"true"}}`,
		`{"codingAgentSync":{"openCode":true}}`,
		`{"codingAgentSync":{"codex":true,"codex":false}}`,
	} {
		path := filepath.Join(t.TempDir(), "config.json")
		writeTestFile(t, path, input)
		if _, err := loadGlobalConfiguration(path); err == nil {
			t.Fatalf("invalid codingAgentSync unexpectedly succeeded: %s", input)
		}
	}
}

func TestLoadGlobalConfigurationDefaultsAndOverridesCredentialSync(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	writeTestFile(t, path, `{"credentialSync":{"opencode":true,"claudeCode":true,"codex":true,"githubCLI":true,"pi":true,"tradingView":true}}`)
	configuration, err := loadGlobalConfiguration(path)
	if err != nil {
		t.Fatalf("loadGlobalConfiguration: %v", err)
	}
	want := credentialSyncConfiguration{OpenCode: true, ClaudeCode: true, Codex: true, GitHubCLI: true, Pi: true, TradingView: true}
	if configuration.CredentialSync != want {
		t.Fatalf("credentialSync = %#v, want %#v", configuration.CredentialSync, want)
	}
	missing, err := loadGlobalConfiguration(filepath.Join(t.TempDir(), "missing.json"))
	if err != nil {
		t.Fatal(err)
	}
	if missing.CredentialSync != (credentialSyncConfiguration{}) {
		t.Fatalf("missing config credentialSync = %#v", missing.CredentialSync)
	}
}

func TestLoadGlobalConfigurationRejectsInvalidCredentialSync(t *testing.T) {
	for _, input := range []string{
		`{"credentialSync":null}`,
		`{"credentialSync":false}`,
		`{"credentialSync":{"codex":null}}`,
		`{"credentialSync":{"codex":"true"}}`,
		`{"credentialSync":{"githubCopilot":true}}`,
		`{"credentialSync":{"codexMCP":true}}`,
		`{"credentialSync":{"codex":true,"codex":false}}`,
	} {
		path := filepath.Join(t.TempDir(), "config.json")
		writeTestFile(t, path, input)
		if _, err := loadGlobalConfiguration(path); err == nil {
			t.Fatalf("invalid credentialSync unexpectedly succeeded: %s", input)
		}
	}
}

func TestLoadGlobalConfigurationDefaultsAndOverridesConfigurationSync(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	writeTestFile(t, path, `{"configurationSync":{"pullHostGitRepositoriesOnDown":false}}`)
	configuration, err := loadGlobalConfiguration(path)
	if err != nil {
		t.Fatal(err)
	}
	want := configurationSyncConfiguration{PullHostGitRepositoriesOnUp: true, PullHostGitRepositoriesOnDown: false}
	if configuration.ConfigurationSync != want {
		t.Fatalf("configurationSync = %#v, want %#v", configuration.ConfigurationSync, want)
	}
	missing, err := loadGlobalConfiguration(filepath.Join(t.TempDir(), "missing.json"))
	if err != nil {
		t.Fatal(err)
	}
	if missing.ConfigurationSync != defaultConfigurationSyncConfiguration() {
		t.Fatalf("missing configurationSync = %#v", missing.ConfigurationSync)
	}
}

func TestLoadGlobalConfigurationRejectsInvalidConfigurationSync(t *testing.T) {
	for _, input := range []string{
		`{"configurationSync":null}`,
		`{"configurationSync":false}`,
		`{"configurationSync":{"pullHostGitRepositoriesOnUp":null}}`,
		`{"configurationSync":{"pullHostGitRepositoriesOnDown":"true"}}`,
		`{"configurationSync":{"pullOnUp":true}}`,
		`{"configurationSync":{"pullHostGitRepositoriesOnUp":true,"pullHostGitRepositoriesOnUp":false}}`,
	} {
		path := filepath.Join(t.TempDir(), "config.json")
		writeTestFile(t, path, input)
		if _, err := loadGlobalConfiguration(path); err == nil {
			t.Fatalf("invalid configurationSync unexpectedly succeeded: %s", input)
		}
	}
}

func TestDefaultCodingAgentConfigurationSourcesHonorAbsoluteOverrides(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	xdgConfig := filepath.Join(root, "xdg-config")
	xdgData := filepath.Join(root, "xdg-data")
	claude := filepath.Join(root, "claude")
	codex := filepath.Join(root, "codex")
	copilot := filepath.Join(root, "copilot")
	pi := filepath.Join(root, "pi")
	t.Setenv("XDG_CONFIG_HOME", xdgConfig)
	t.Setenv("XDG_DATA_HOME", xdgData)
	t.Setenv("CLAUDE_CONFIG_DIR", claude)
	t.Setenv("CODEX_HOME", codex)
	t.Setenv("COPILOT_HOME", copilot)
	t.Setenv("PI_CODING_AGENT_DIR", pi)
	sources, err := defaultCodingAgentConfigurationSources(home, defaultCodingAgentSyncConfiguration(), credentialSyncConfiguration{
		OpenCode: true, ClaudeCode: true, Codex: true, Pi: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	for name, got := range map[string]string{
		"OpenCode config": filepath.Dir(sources.OpenCodeDirectory),
		"OpenCode data":   filepath.Dir(sources.OpenCodeAuthentication),
		"Claude":          sources.ClaudeCodeDirectory,
		"Claude state":    sources.ClaudeCodeState,
		"Codex":           sources.CodexDirectory,
		"Copilot":         sources.GitHubCopilotDirectory,
		"Pi":              sources.PiDirectory,
		"Shared skills":   sources.SharedSkillsDirectory,
	} {
		want := map[string]string{
			"OpenCode config": xdgConfig,
			"OpenCode data":   filepath.Join(xdgData, "opencode"),
			"Claude":          claude,
			"Claude state":    filepath.Join(claude, ".claude.json"),
			"Codex":           codex,
			"Copilot":         copilot,
			"Pi":              pi,
			"Shared skills":   filepath.Join(home, ".agents", "skills"),
		}[name]
		if got != want {
			t.Fatalf("%s = %q, want %q", name, got, want)
		}
	}
}

func TestDefaultCodingAgentConfigurationSourcesRejectRelativeOverrides(t *testing.T) {
	for _, name := range []string{"CLAUDE_CONFIG_DIR", "CODEX_HOME", "COPILOT_HOME", "PI_CODING_AGENT_DIR"} {
		t.Run(name, func(t *testing.T) {
			for _, other := range []string{"CLAUDE_CONFIG_DIR", "CODEX_HOME", "COPILOT_HOME", "PI_CODING_AGENT_DIR"} {
				t.Setenv(other, "")
			}
			t.Setenv(name, "relative")
			if _, err := defaultCodingAgentConfigurationSources(t.TempDir(), defaultCodingAgentSyncConfiguration(), credentialSyncConfiguration{}); err == nil || !strings.Contains(err.Error(), name) {
				t.Fatalf("relative %s error = %v", name, err)
			}
		})
	}
}

func TestBuildDevelopmentConfigurationArchiveIncludesApprovedAgentConfigurationAndAuthentication(t *testing.T) {
	root := t.TempDir()
	openCode := filepath.Join(root, "opencode")
	claude := filepath.Join(root, "claude")
	codex := filepath.Join(root, "codex")
	copilot := filepath.Join(root, "copilot")
	pi := filepath.Join(root, "pi")
	sharedSkills := filepath.Join(root, "shared-skills")
	for _, directory := range []string{
		filepath.Join(openCode, "agents"), filepath.Join(openCode, "plugins"), filepath.Join(openCode, "node_modules"),
		filepath.Join(claude, "agents"), filepath.Join(claude, "hooks"), filepath.Join(claude, "projects"),
		filepath.Join(codex, "agents"), filepath.Join(codex, "skills", ".system"), filepath.Join(codex, "sessions"),
		filepath.Join(copilot, "agents"), filepath.Join(copilot, "hooks"), filepath.Join(copilot, "session-state"),
		filepath.Join(pi, "extensions", "fixture"), filepath.Join(pi, "extensions", "fixture", "node_modules"), filepath.Join(pi, "sessions"),
		filepath.Join(sharedSkills, "fixture"),
	} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	writeTestFile(t, filepath.Join(openCode, "opencode.json"), `{}`)
	writeTestFile(t, filepath.Join(openCode, "agents", "builder.md"), "agent")
	writeTestFile(t, filepath.Join(openCode, "plugins", "herdr-agent-state.js"), "// HERDR_INTEGRATION_VERSION=10")
	writeTestFile(t, filepath.Join(openCode, "herdr-tui-session.js"), "// HERDR_INTEGRATION_VERSION=10")
	writeTestFile(t, filepath.Join(openCode, "README.md"), "OpenCode workflow repository")
	writeTestFile(t, filepath.Join(openCode, "removed.md"), "remove in guest")
	writeTestFile(t, filepath.Join(openCode, "node_modules", "excluded.js"), "excluded")
	openCodeAuth := filepath.Join(root, "opencode-auth.json")
	writeTestFile(t, openCodeAuth, `{"provider":"fixture"}`)

	writeTestFile(t, filepath.Join(claude, "settings.json"), `{}`)
	writeTestFile(t, filepath.Join(claude, "agents", "reviewer.md"), "agent")
	writeTestFile(t, filepath.Join(claude, "hooks", "herdr-agent-state.ps1"), "# HERDR_INTEGRATION_VERSION=8")
	writeTestFile(t, filepath.Join(claude, "README.md"), "Claude workflow repository")
	writeTestFile(t, filepath.Join(claude, "projects", "excluded.jsonl"), "excluded")
	claudeAuth := filepath.Join(claude, ".credentials.json")
	writeTestFile(t, claudeAuth, `{"claudeAiOauth":{"accessToken":"fixture"}}`)
	claudeState := filepath.Join(root, ".claude.json")
	writeTestFile(t, claudeState, `{"mcpServers":{"fixture":{"command":"tool"}},"projects":{"C:\\host":{}}}`)

	writeTestFile(t, filepath.Join(codex, "config.toml"), `model = "fixture"`)
	writeTestFile(t, filepath.Join(codex, "hooks.json"), `{}`)
	writeTestFile(t, filepath.Join(codex, "herdr-agent-state.ps1"), "# HERDR_INTEGRATION_VERSION=8")
	writeTestFile(t, filepath.Join(codex, "work.config.toml"), `model = "profile"`)
	writeTestFile(t, filepath.Join(codex, "agents", "reviewer.toml"), `name = "reviewer"`)
	writeTestFile(t, filepath.Join(codex, "README.md"), "Codex workflow repository")
	writeTestFile(t, filepath.Join(codex, "skills", ".system", "excluded.md"), "excluded")
	writeTestFile(t, filepath.Join(codex, "sessions", "excluded.jsonl"), "excluded")
	writeTestFile(t, filepath.Join(codex, "auth.json"), `{"tokens":{"access_token":"fixture"}}`)
	writeTestFile(t, filepath.Join(codex, ".credentials.json"), `{"oauth":"fixture"}`)

	writeTestFile(t, filepath.Join(copilot, "settings.json"), `{}`)
	writeTestFile(t, filepath.Join(copilot, "agents", "builder.agent.md"), "agent")
	writeTestFile(t, filepath.Join(copilot, "hooks", "herdr-agent-state.ps1"), "# HERDR_INTEGRATION_VERSION=3")
	writeTestFile(t, filepath.Join(copilot, "README.md"), "Copilot workflow repository")
	writeTestFile(t, filepath.Join(copilot, "config.json"), `{"token":"excluded"}`)
	writeTestFile(t, filepath.Join(copilot, "session-state", "excluded.json"), "excluded")

	writeTestFile(t, filepath.Join(pi, "settings.json"), `{}`)
	writeTestFile(t, filepath.Join(pi, "models.json"), `{"providers":{}}`)
	writeTestFile(t, filepath.Join(pi, "extensions", "herdr-agent-state.ts"), "// HERDR_INTEGRATION_VERSION=8")
	writeTestFile(t, filepath.Join(pi, "CLAUDE.md"), "pi instructions")
	writeTestFile(t, filepath.Join(pi, "extensions", "fixture", "index.ts"), "export default {}")
	writeTestFile(t, filepath.Join(pi, "README.md"), "Pi workflow repository")
	writeTestFile(t, filepath.Join(pi, "extensions", "fixture", "node_modules", "excluded.js"), "excluded")
	writeTestFile(t, filepath.Join(pi, "sessions", "excluded.jsonl"), "excluded")
	writeTestFile(t, filepath.Join(pi, "auth.json"), `{"provider":{"type":"api_key","key":"fixture"}}`)
	writeTestFile(t, filepath.Join(sharedSkills, "fixture", "SKILL.md"), "skill")
	writeTestFile(t, filepath.Join(sharedSkills, "README.md"), "Shared skills repository")

	initializeAgentGitRepository(t, openCode, "opencode", []string{"opencode.json", "agents/builder.md", "README.md", "removed.md"})
	if err := os.Remove(filepath.Join(openCode, "removed.md")); err != nil {
		t.Fatal(err)
	}
	initializeAgentGitRepository(t, claude, "claude", []string{"settings.json", "agents/reviewer.md", "README.md"})
	initializeAgentGitRepository(t, codex, "codex", []string{"config.toml", "work.config.toml", "agents/reviewer.toml", "README.md"})
	initializeAgentGitRepository(t, copilot, "copilot", []string{"settings.json", "agents/builder.agent.md", "README.md"})
	initializeAgentGitRepository(t, pi, "pi", []string{"settings.json", "models.json", "CLAUDE.md", "extensions/fixture/index.ts", "README.md"})
	initializeAgentGitRepository(t, sharedSkills, "shared-skills", []string{"fixture/SKILL.md", "README.md"})

	terminal := testStableWindowsTerminalConfiguration()
	packages, err := resolveWingetPackagePlan(defaultWingetPackageConfiguration(), terminal)
	if err != nil {
		t.Fatal(err)
	}
	packagePlan := filepath.Join(root, wingetPackagePlanFileName)
	packagePlanData, err := encodeWingetPackagePlan(packages, terminal)
	if err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, packagePlan, string(packagePlanData))
	herdrConfig := filepath.Join(root, "herdr.toml")
	writeTestFile(t, herdrConfig, "[terminal]\ndefault_shell = \"nu\"\n")
	data, err := buildDevelopmentConfigurationArchive(context.Background(), hostConfigurationSources{
		CodingAgents: codingAgentConfigurationSources{
			Selection:                defaultCodingAgentSyncConfiguration(),
			CredentialSync:           credentialSyncConfiguration{OpenCode: true, ClaudeCode: true, Codex: true, Pi: true},
			OpenCodeDirectory:        openCode,
			OpenCodeAuthentication:   openCodeAuth,
			ClaudeCodeDirectory:      claude,
			ClaudeCodeState:          claudeState,
			ClaudeCodeAuthentication: claudeAuth,
			CodexDirectory:           codex,
			CodexAuthentication:      filepath.Join(codex, "auth.json"),
			CodexMCPAuthentication:   filepath.Join(codex, ".credentials.json"),
			GitHubCopilotDirectory:   copilot,
			PiDirectory:              pi,
			PiAuthentication:         filepath.Join(pi, "auth.json"),
			SharedSkillsDirectory:    sharedSkills,
		},
		HerdrConfig: herdrConfig,
		PackagePlan: packagePlan,
	}, []byte("Write-Output 'fixture'\n"))
	if err != nil {
		t.Fatalf("buildDevelopmentConfigurationArchive: %v", err)
	}
	entries, contents := readConfigurationArchiveForTest(t, data)
	for _, required := range []string{
		codingAgentSyncManifestArchivePath,
		"opencode/opencode.json", "opencode/agents/builder.md", "opencode/plugins/herdr-agent-state.js", "opencode/herdr-tui-session.js", "opencode/README.md", "opencode/.git/config", "opencode/.git/HEAD", "opencode/.git/index", "opencode-auth/auth.json",
		"claude-code/settings.json", "claude-code/agents/reviewer.md", "claude-code/hooks/herdr-agent-state.ps1", "claude-code/README.md", "claude-code/.git/config", "claude-code/.git/HEAD", "claude-code/.git/index", "claude-code-auth/.credentials.json", "claude-code-state/.claude.json",
		"codex/config.toml", "codex/hooks.json", "codex/herdr-agent-state.ps1", "codex/work.config.toml", "codex/agents/reviewer.toml", "codex/README.md", "codex/.git/config", "codex/.git/HEAD", "codex/.git/index", "codex-auth/auth.json", "codex-auth/.credentials.json",
		"github-copilot/settings.json", "github-copilot/agents/builder.agent.md", "github-copilot/hooks/herdr-agent-state.ps1", "github-copilot/README.md", "github-copilot/.git/config", "github-copilot/.git/HEAD", "github-copilot/.git/index",
		"pi/settings.json", "pi/models.json", "pi/AGENTS.md", "pi/CLAUDE.md", "pi/extensions/fixture/index.ts", "pi/extensions/herdr-agent-state.ts", "pi/README.md", "pi/.git/config", "pi/.git/HEAD", "pi/.git/index", "pi-auth/auth.json",
		"shared-agent-skills/fixture/SKILL.md", "shared-agent-skills/README.md", "shared-agent-skills/.git/config", "shared-agent-skills/.git/HEAD", "shared-agent-skills/.git/index",
	} {
		if !entries[required] {
			t.Fatalf("archive is missing %s", required)
		}
	}
	for _, forbidden := range []string{
		"opencode/node_modules/excluded.js", "claude-code/projects/excluded.jsonl",
		"codex/skills/.system/excluded.md", "codex/sessions/excluded.jsonl",
		"github-copilot/config.json", "github-copilot/session-state/excluded.json",
		"pi/extensions/fixture/node_modules/excluded.js", "pi/sessions/excluded.jsonl",
	} {
		if entries[forbidden] {
			t.Fatalf("archive contains excluded agent state %s", forbidden)
		}
	}
	for entry := range entries {
		if strings.Contains(entry, "/.git/hooks/") || strings.Contains(entry, "/.git/logs/") {
			t.Fatalf("archive contains excluded Git runtime state %s", entry)
		}
	}
	for _, root := range []string{"opencode", "claude-code", "codex", "github-copilot", "pi", "shared-agent-skills"} {
		foundObject := false
		for entry := range entries {
			if strings.HasPrefix(entry, root+"/.git/objects/") {
				foundObject = true
				break
			}
		}
		if !foundObject {
			t.Fatalf("archive is missing Git objects for %s", root)
		}
	}
	var syncManifest codingAgentSyncManifest
	if err := json.Unmarshal(contents[codingAgentSyncManifestArchivePath], &syncManifest); err != nil {
		t.Fatal(err)
	}
	if syncManifest.SchemaVersion != 4 || syncManifest.CredentialSync != (credentialSyncConfiguration{OpenCode: true, ClaudeCode: true, Codex: true, Pi: true}) ||
		strings.Join(syncManifest.GitTrackedDeletions["opencode"], "|") != "removed.md" || entries["opencode/removed.md"] ||
		syncManifest.HerdrHookSourcePaths["claude"] != filepath.Join(claude, "hooks", "herdr-agent-state.ps1") ||
		syncManifest.HerdrHookSourcePaths["codex"] != filepath.Join(codex, "herdr-agent-state.ps1") ||
		syncManifest.HerdrHookSourcePaths["copilot"] != filepath.Join(copilot, "hooks", "herdr-agent-state.ps1") {
		t.Fatalf("coding-agent Git deletion manifest = %#v", syncManifest)
	}
	var claudeStateArchive map[string]any
	if err := json.Unmarshal(contents["claude-code-state/.claude.json"], &claudeStateArchive); err != nil {
		t.Fatal(err)
	}
	if _, exists := claudeStateArchive["projects"]; exists || claudeStateArchive["mcpServers"] == nil {
		t.Fatalf("archived Claude state = %#v", claudeStateArchive)
	}
	caseInsensitiveEntries := map[string]string{}
	for entry := range entries {
		identity := strings.ToLower(entry)
		if previous := caseInsensitiveEntries[identity]; previous != "" {
			t.Fatalf("case-colliding archive entries: %q and %q", previous, entry)
		}
		caseInsensitiveEntries[identity] = entry
	}
	if count, err := configurationArchivePayloadFileCount(data); err != nil || count != len(entries)-3 {
		t.Fatalf("payload count = %d, entries = %d, err = %v", count, len(entries), err)
	}
}

func TestArchiveCodingAgentConfigurationKeepsCredentialsIndependent(t *testing.T) {
	root := t.TempDir()
	openCodeConfiguration := filepath.Join(root, "opencode")
	if err := os.MkdirAll(openCodeConfiguration, 0o700); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(openCodeConfiguration, "opencode.json"), `{}`)
	credentials := map[string]string{
		"opencode-auth/auth.json":            filepath.Join(root, "opencode-auth.json"),
		"claude-code-auth/.credentials.json": filepath.Join(root, "claude-credentials.json"),
		"codex-auth/auth.json":               filepath.Join(root, "codex-auth.json"),
		"codex-auth/.credentials.json":       filepath.Join(root, "codex-credentials.json"),
		"pi-auth/auth.json":                  filepath.Join(root, "pi-auth.json"),
	}
	for _, source := range credentials {
		writeTestFile(t, source, `{"credential":"fixture"}`)
	}
	archive := func(sources codingAgentConfigurationSources) map[string][]byte {
		t.Helper()
		entries := map[string][]byte{}
		add := func(source, destination string) error {
			contents, err := os.ReadFile(source)
			if err == nil {
				entries[filepath.ToSlash(destination)] = contents
			}
			return err
		}
		addData := func(contents []byte, destination, _ string) error {
			entries[filepath.ToSlash(destination)] = append([]byte(nil), contents...)
			return nil
		}
		if err := archiveCodingAgentConfiguration(context.Background(), sources, add, addData); err != nil {
			t.Fatal(err)
		}
		return entries
	}
	credentialOnly := archive(codingAgentConfigurationSources{
		CredentialSync:           credentialSyncConfiguration{OpenCode: true, ClaudeCode: true, Codex: true, Pi: true},
		OpenCodeAuthentication:   credentials["opencode-auth/auth.json"],
		ClaudeCodeAuthentication: credentials["claude-code-auth/.credentials.json"],
		CodexAuthentication:      credentials["codex-auth/auth.json"],
		CodexMCPAuthentication:   credentials["codex-auth/.credentials.json"],
		PiAuthentication:         credentials["pi-auth/auth.json"],
	})
	for destination := range credentials {
		if credentialOnly[destination] == nil {
			t.Fatalf("credential-only archive is missing %s", destination)
		}
	}
	if credentialOnly["opencode/opencode.json"] != nil {
		t.Fatal("credential-only archive contains OpenCode configuration")
	}
	var manifest codingAgentSyncManifest
	if err := json.Unmarshal(credentialOnly[codingAgentSyncManifestArchivePath], &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.CredentialSync != (credentialSyncConfiguration{OpenCode: true, ClaudeCode: true, Codex: true, Pi: true}) || manifest.OpenCode {
		t.Fatalf("credential-only manifest = %#v", manifest)
	}

	configurationOnly := archive(codingAgentConfigurationSources{
		Selection:              codingAgentSyncConfiguration{OpenCode: true},
		OpenCodeDirectory:      openCodeConfiguration,
		OpenCodeAuthentication: credentials["opencode-auth/auth.json"],
	})
	if configurationOnly["opencode/opencode.json"] == nil {
		t.Fatal("configuration-only archive is missing OpenCode configuration")
	}
	if configurationOnly["opencode-auth/auth.json"] != nil {
		t.Fatal("configuration-only archive contains OpenCode credentials")
	}
}

func TestArchiveAgentGitRepositoryRestoresUpstreamAndTrackedChanges(t *testing.T) {
	root := t.TempDir()
	repository := filepath.Join(root, "opencode")
	if err := os.MkdirAll(filepath.Join(repository, "agents"), 0o700); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(repository, "opencode.json"), `{}`)
	writeTestFile(t, filepath.Join(repository, "agents", "builder.md"), "agent")
	writeTestFile(t, filepath.Join(repository, "README.md"), "before")
	initializeAgentGitRepository(t, repository, "opencode", []string{"opencode.json", "agents/builder.md", "README.md"})
	runAgentGitTest(t, repository, "update-ref", "refs/heads/feature.log", "HEAD")
	runAgentGitTest(t, repository, "update-ref", "refs/heads/worker.pid", "HEAD")
	runAgentGitTest(t, repository, "update-ref", "refs/heads/packed-refs.new", "HEAD")
	writeTestFile(t, filepath.Join(repository, "README.md"), "after")

	archived := map[string][]byte{}
	_, err := archiveAgentGitRepository(context.Background(), repository, "opencode", func(source, destination string) error {
		contents, readErr := os.ReadFile(source)
		if readErr != nil {
			return readErr
		}
		archived[filepath.ToSlash(destination)] = contents
		return nil
	})
	if err != nil {
		t.Fatalf("archive agent Git repository: %v", err)
	}
	for _, required := range []string{"opencode/README.md", "opencode/.git/config", "opencode/.git/HEAD", "opencode/.git/index", "opencode/.git/refs/heads/feature.log", "opencode/.git/refs/heads/worker.pid", "opencode/.git/refs/heads/packed-refs.new"} {
		if _, exists := archived[required]; !exists {
			t.Fatalf("Git repository archive is missing %s", required)
		}
	}
	for name := range archived {
		if strings.Contains(name, "/.git/hooks/") || strings.Contains(name, "/.git/logs/") {
			t.Fatalf("Git repository archive contains excluded state %s", name)
		}
	}

	restored := filepath.Join(root, "restored")
	for name, contents := range archived {
		relative := strings.TrimPrefix(name, "opencode/")
		destination := filepath.Join(restored, filepath.FromSlash(relative))
		if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(destination, contents, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if got := strings.TrimSpace(runAgentGitTest(t, restored, "config", "--local", "--get", "remote.origin.url")); got != "https://example.invalid/opencode.git" {
		t.Fatalf("restored remote URL = %q", got)
	}
	if got := strings.TrimSpace(runAgentGitTest(t, restored, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{upstream}")); got != "origin/main" {
		t.Fatalf("restored upstream = %q", got)
	}
	if got := strings.TrimSpace(runAgentGitTest(t, restored, "status", "--porcelain=v1")); got != "M README.md" {
		t.Fatalf("restored tracked changes = %q", got)
	}
	runAgentGitTest(t, restored, "fsck", "--no-dangling")
}

func TestUpdateAgentGitRepositoryFastForwardsBeforeArchivingAndPreservesLocalChanges(t *testing.T) {
	root := t.TempDir()
	remote := filepath.Join(root, "remote.git")
	runAgentGitTest(t, root, "init", "--bare", "--initial-branch=main", remote)
	seed := filepath.Join(root, "seed")
	initializeAgentGitRepositoryAtRemote(t, seed, remote, []string{"opencode.json", "README.md"})
	repository := filepath.Join(root, "host")
	runAgentGitTest(t, root, "clone", remote, repository)
	hookMarker := filepath.Join(root, "post-merge-ran")
	hook := filepath.Join(repository, ".git", "hooks", "post-merge")
	writeTestFile(t, hook, "#!/bin/sh\nprintf ran > '"+filepath.ToSlash(hookMarker)+"'\n")
	configuredHookMarker := filepath.Join(root, "configured-hook-ran")
	runAgentGitTest(t, repository, "config", "hook.audit.command", "printf ran > '"+filepath.ToSlash(configuredHookMarker)+"'")
	for _, event := range disabledAgentGitUpdateHookEvents {
		runAgentGitTest(t, repository, "config", "--add", "hook.audit.event", event)
	}
	writeTestFile(t, filepath.Join(seed, "opencode.json"), `{"version":2}`)
	runAgentGitTest(t, seed, "add", "--", "opencode.json")
	runAgentGitTest(t, seed, "-c", "core.hooksPath="+os.DevNull, "commit", "-m", "upstream")
	runAgentGitTest(t, seed, "push", "origin", "main")

	writeTestFile(t, filepath.Join(repository, "README.md"), "local edit")
	if _, err := updateHostConfigurationGitRepository(context.Background(), repository); err != nil {
		t.Fatalf("updateHostConfigurationGitRepository: %v", err)
	}
	if _, err := os.Lstat(hookMarker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("automatic update executed a Git hook: %v", err)
	}
	if _, err := os.Lstat(configuredHookMarker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("automatic update executed a configured Git hook: %v", err)
	}
	if got, err := os.ReadFile(filepath.Join(repository, "opencode.json")); err != nil || string(got) != `{"version":2}` {
		t.Fatalf("fast-forwarded config = %q, err = %v", got, err)
	}
	if got, err := os.ReadFile(filepath.Join(repository, "README.md")); err != nil || string(got) != "local edit" {
		t.Fatalf("local change = %q, err = %v", got, err)
	}
	if head, upstream := strings.TrimSpace(runAgentGitTest(t, repository, "rev-parse", "HEAD")), strings.TrimSpace(runAgentGitTest(t, repository, "rev-parse", "@{upstream}")); head != upstream {
		t.Fatalf("fast-forwarded HEAD %s does not match upstream %s", head, upstream)
	}
	if got := strings.TrimSpace(runAgentGitTest(t, repository, "status", "--porcelain=v1")); got != "M README.md" {
		t.Fatalf("updated repository status = %q", got)
	}
}

func TestUpdateAgentGitRepositoryRefusesUnsafeStatesWithoutChangingHead(t *testing.T) {
	t.Run("repository without remotes", func(t *testing.T) {
		repository := filepath.Join(t.TempDir(), "opencode")
		if err := os.MkdirAll(repository, 0o700); err != nil {
			t.Fatal(err)
		}
		writeTestFile(t, filepath.Join(repository, "opencode.json"), `{}`)
		runAgentGitTest(t, repository, "init", "--initial-branch=main")
		configureAgentGitTestIdentity(t, repository)
		runAgentGitTest(t, repository, "add", "--", "opencode.json")
		runAgentGitTest(t, repository, "-c", "core.hooksPath="+os.DevNull, "commit", "-m", "fixture")
		head := strings.TrimSpace(runAgentGitTest(t, repository, "rev-parse", "HEAD"))
		if _, err := updateHostConfigurationGitRepository(context.Background(), repository); err != nil {
			t.Fatalf("repository without remotes: %v", err)
		}
		if got := strings.TrimSpace(runAgentGitTest(t, repository, "rev-parse", "HEAD")); got != head {
			t.Fatalf("repository without remotes changed HEAD from %s to %s", head, got)
		}
	})

	t.Run("remote without upstream", func(t *testing.T) {
		repository := filepath.Join(t.TempDir(), "opencode")
		if err := os.MkdirAll(repository, 0o700); err != nil {
			t.Fatal(err)
		}
		writeTestFile(t, filepath.Join(repository, "opencode.json"), `{}`)
		initializeAgentGitRepository(t, repository, "opencode", []string{"opencode.json"})
		runAgentGitTest(t, repository, "config", "--unset", "branch.main.remote")
		runAgentGitTest(t, repository, "config", "--unset", "branch.main.merge")
		if _, err := updateHostConfigurationGitRepository(context.Background(), repository); err == nil || !strings.Contains(err.Error(), "no upstream") {
			t.Fatalf("missing upstream error = %v", err)
		}
	})

	t.Run("detached head", func(t *testing.T) {
		repository := filepath.Join(t.TempDir(), "opencode")
		if err := os.MkdirAll(repository, 0o700); err != nil {
			t.Fatal(err)
		}
		writeTestFile(t, filepath.Join(repository, "opencode.json"), `{}`)
		initializeAgentGitRepository(t, repository, "opencode", []string{"opencode.json"})
		runAgentGitTest(t, repository, "checkout", "--detach")
		if _, err := updateHostConfigurationGitRepository(context.Background(), repository); err == nil || !strings.Contains(err.Error(), "must be on a branch") {
			t.Fatalf("detached HEAD error = %v", err)
		}
	})

	t.Run("diverged", func(t *testing.T) {
		root := t.TempDir()
		remote := filepath.Join(root, "remote.git")
		runAgentGitTest(t, root, "init", "--bare", "--initial-branch=main", remote)
		seed := filepath.Join(root, "seed")
		initializeAgentGitRepositoryAtRemote(t, seed, remote, []string{"opencode.json"})
		host := filepath.Join(root, "host")
		runAgentGitTest(t, root, "clone", remote, host)
		configureAgentGitTestIdentity(t, host)
		writeTestFile(t, filepath.Join(host, "host.md"), "host")
		runAgentGitTest(t, host, "add", "--", "host.md")
		runAgentGitTest(t, host, "-c", "core.hooksPath="+os.DevNull, "commit", "-m", "host")
		hostHead := strings.TrimSpace(runAgentGitTest(t, host, "rev-parse", "HEAD"))
		writeTestFile(t, filepath.Join(seed, "remote.md"), "remote")
		runAgentGitTest(t, seed, "add", "--", "remote.md")
		runAgentGitTest(t, seed, "-c", "core.hooksPath="+os.DevNull, "commit", "-m", "remote")
		runAgentGitTest(t, seed, "push", "origin", "main")
		if _, err := updateHostConfigurationGitRepository(context.Background(), host); err == nil || !strings.Contains(err.Error(), "resolve local changes") {
			t.Fatalf("diverged update error = %v", err)
		}
		if got := strings.TrimSpace(runAgentGitTest(t, host, "rev-parse", "HEAD")); got != hostHead {
			t.Fatalf("diverged update changed HEAD from %s to %s", hostHead, got)
		}
	})

	t.Run("overlapping working tree change", func(t *testing.T) {
		root := t.TempDir()
		remote := filepath.Join(root, "remote.git")
		runAgentGitTest(t, root, "init", "--bare", "--initial-branch=main", remote)
		seed := filepath.Join(root, "seed")
		initializeAgentGitRepositoryAtRemote(t, seed, remote, []string{"opencode.json"})
		host := filepath.Join(root, "host")
		runAgentGitTest(t, root, "clone", remote, host)
		writeTestFile(t, filepath.Join(host, "opencode.json"), `{"version":"local"}`)
		hostHead := strings.TrimSpace(runAgentGitTest(t, host, "rev-parse", "HEAD"))
		writeTestFile(t, filepath.Join(seed, "opencode.json"), `{"version":"upstream"}`)
		runAgentGitTest(t, seed, "add", "--", "opencode.json")
		runAgentGitTest(t, seed, "-c", "core.hooksPath="+os.DevNull, "commit", "-m", "upstream")
		runAgentGitTest(t, seed, "push", "origin", "main")
		if _, err := updateHostConfigurationGitRepository(context.Background(), host); err == nil || !strings.Contains(err.Error(), "resolve local changes") {
			t.Fatalf("overlapping change update error = %v", err)
		}
		if got := strings.TrimSpace(runAgentGitTest(t, host, "rev-parse", "HEAD")); got != hostHead {
			t.Fatalf("overlapping change update changed HEAD from %s to %s", hostHead, got)
		}
		if got, err := os.ReadFile(filepath.Join(host, "opencode.json")); err != nil || string(got) != `{"version":"local"}` {
			t.Fatalf("overlapping change update changed local file to %q: %v", got, err)
		}
		if _, err := os.Lstat(filepath.Join(host, ".git", "MERGE_HEAD")); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("overlapping change left merge state: %v", err)
		}
	})
}

func TestArchiveCodingAgentConfigurationDoesNotPullGitRepository(t *testing.T) {
	root := t.TempDir()
	remote := filepath.Join(root, "remote.git")
	runAgentGitTest(t, root, "init", "--bare", "--initial-branch=main", remote)
	seed := filepath.Join(root, "seed")
	initializeAgentGitRepositoryAtRemote(t, seed, remote, []string{"opencode.json"})
	host := filepath.Join(root, "host")
	runAgentGitTest(t, root, "clone", remote, host)
	writeTestFile(t, filepath.Join(seed, "opencode.json"), `{"version":2}`)
	runAgentGitTest(t, seed, "add", "--", "opencode.json")
	runAgentGitTest(t, seed, "-c", "core.hooksPath="+os.DevNull, "commit", "-m", "upstream")
	runAgentGitTest(t, seed, "push", "origin", "main")

	archived := map[string][]byte{}
	err := archiveCodingAgentConfiguration(context.Background(), codingAgentConfigurationSources{
		Selection:         codingAgentSyncConfiguration{OpenCode: true},
		OpenCodeDirectory: host,
	}, func(source, destination string) error {
		contents, readErr := os.ReadFile(source)
		if readErr == nil {
			archived[filepath.ToSlash(destination)] = contents
		}
		return readErr
	}, func(contents []byte, destination, _ string) error {
		archived[filepath.ToSlash(destination)] = append([]byte(nil), contents...)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := string(archived["opencode/opencode.json"]); got != "opencode.json" {
		t.Fatalf("disabled Git update archived %q", got)
	}
}

func TestArchiveAgentGitRepositoryRejectsTrackedCredentialsAndGitFiles(t *testing.T) {
	t.Run("tracked credential", func(t *testing.T) {
		repository := filepath.Join(t.TempDir(), "codex")
		if err := os.MkdirAll(repository, 0o700); err != nil {
			t.Fatal(err)
		}
		writeTestFile(t, filepath.Join(repository, "config.toml"), "model = 'fixture'")
		writeTestFile(t, filepath.Join(repository, "auth.json"), `{"token":"fixture"}`)
		initializeAgentGitRepository(t, repository, "codex", []string{"config.toml", "auth.json"})
		_, err := archiveAgentGitRepository(context.Background(), repository, "codex", func(string, string) error { return nil })
		if err == nil || !strings.Contains(err.Error(), "credential") || !strings.Contains(err.Error(), "auth.json") {
			t.Fatalf("tracked credential error = %v", err)
		}
	})

	t.Run("worktree git file", func(t *testing.T) {
		repository := t.TempDir()
		writeTestFile(t, filepath.Join(repository, ".git"), "gitdir: C:/outside")
		_, err := archiveAgentGitRepository(context.Background(), repository, "opencode", func(string, string) error { return nil })
		if err == nil || !strings.Contains(err.Error(), "physical .git directory") {
			t.Fatalf("worktree Git file error = %v", err)
		}
	})

	t.Run("active lock", func(t *testing.T) {
		repository := filepath.Join(t.TempDir(), "opencode")
		if err := os.MkdirAll(repository, 0o700); err != nil {
			t.Fatal(err)
		}
		writeTestFile(t, filepath.Join(repository, "opencode.json"), `{}`)
		initializeAgentGitRepository(t, repository, "opencode", []string{"opencode.json"})
		writeTestFile(t, filepath.Join(repository, ".git", "index.lock"), "active")
		_, err := archiveAgentGitRepository(context.Background(), repository, "opencode", func(string, string) error { return nil })
		if err == nil || !strings.Contains(err.Error(), "locked") {
			t.Fatalf("active Git lock error = %v", err)
		}
	})

}

func TestAgentGitEnvironmentDropsInheritedOverrides(t *testing.T) {
	environment := agentGitEnvironment([]string{"PATH=C:\\tools", "GIT_DIR=C:\\outside", "git_work_tree=C:\\worktree", "GCM_INTERACTIVE=Always", "GCM_TRACE=1", "SSH_ASKPASS_REQUIRE=force", "OTHER=value"})
	joined := strings.ToUpper(strings.Join(environment, "\n"))
	if strings.Contains(joined, "GIT_DIR=C:\\OUTSIDE") || strings.Contains(joined, "GIT_WORK_TREE=C:\\WORKTREE") ||
		strings.Contains(joined, "GCM_INTERACTIVE=ALWAYS") || strings.Contains(joined, "GCM_TRACE=1") || strings.Contains(joined, "SSH_ASKPASS_REQUIRE=FORCE") {
		t.Fatalf("inherited Git override survived: %q", environment)
	}
	for _, required := range []string{"GIT_CONFIG_GLOBAL=", "GIT_CONFIG_NOSYSTEM=1", "GIT_OPTIONAL_LOCKS=0", "GIT_TERMINAL_PROMPT=0", "GCM_INTERACTIVE=NEVER", "SSH_ASKPASS_REQUIRE=NEVER", "OTHER=VALUE"} {
		if !strings.Contains(joined, required) {
			t.Fatalf("Git environment is missing %q: %q", required, environment)
		}
	}
	updateEnvironment := strings.ToUpper(strings.Join(agentGitUpdateEnvironment([]string{"PATH=C:\\tools", "GIT_DIR=C:\\outside", "GCM_INTERACTIVE=Always", "GCM_TRACE=1", "SSH_ASKPASS_REQUIRE=force", "OTHER=value"}), "\n"))
	for _, required := range []string{"GIT_OPTIONAL_LOCKS=0", "GIT_TERMINAL_PROMPT=0", "GCM_INTERACTIVE=NEVER", "SSH_ASKPASS_REQUIRE=NEVER", "GIT_SSH_COMMAND=SSH -OBATCHMODE=YES", "OTHER=VALUE"} {
		if !strings.Contains(updateEnvironment, required) {
			t.Fatalf("Git update environment is missing %q: %q", required, updateEnvironment)
		}
	}
	if strings.Contains(updateEnvironment, "GIT_DIR=C:\\OUTSIDE") || strings.Contains(updateEnvironment, "GIT_CONFIG_GLOBAL=") ||
		strings.Contains(updateEnvironment, "GCM_INTERACTIVE=ALWAYS") || strings.Contains(updateEnvironment, "GCM_TRACE=1") || strings.Contains(updateEnvironment, "SSH_ASKPASS_REQUIRE=FORCE") {
		t.Fatalf("Git update environment retained overrides or discarded user configuration: %q", updateEnvironment)
	}
}

func TestArchiveCodingAgentConfigurationSkipsDisabledAndMissingSources(t *testing.T) {
	var buffer bytes.Buffer
	archive := zip.NewWriter(&buffer)
	total := 0
	addData := func(contents []byte, destination, _ string) error {
		writer, err := archive.Create(strings.ReplaceAll(destination, `\`, "/"))
		if err != nil {
			return err
		}
		_, err = writer.Write(contents)
		total++
		return err
	}
	if err := archiveCodingAgentConfiguration(context.Background(), codingAgentConfigurationSources{Selection: codingAgentSyncConfiguration{}}, func(string, string) error {
		t.Fatal("disabled coding-agent source was inspected")
		return nil
	}, addData); err != nil {
		t.Fatal(err)
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	if total != 1 {
		t.Fatalf("archived file count = %d, want manifest only", total)
	}
}

func TestProvisioningConfigurationNamesAreSortedAndIncludeSelectedCodingAgentsOnce(t *testing.T) {
	terminal := testStableWindowsTerminalConfiguration()
	packages, err := resolveWingetPackagePlan(defaultWingetPackageConfiguration(), terminal)
	if err != nil {
		t.Fatal(err)
	}
	names := provisioningConfigurationNames(packages, codingAgentSyncConfiguration{OpenCode: true, ClaudeCode: true, Codex: true})
	summary := strings.Join(names, "|")
	if !sort.StringsAreSorted(names) {
		t.Fatalf("configuration names are not sorted: %q", names)
	}
	for _, expected := range []string{"OpenCode", "Claude Code", "Codex"} {
		if strings.Count(summary, expected) != 1 {
			t.Fatalf("summary %q does not contain %q exactly once", summary, expected)
		}
	}
	for _, disabled := range []string{"GitHub Copilot", "Pi"} {
		if strings.Contains(summary, disabled) {
			t.Fatalf("summary %q contains disabled %q", summary, disabled)
		}
	}
	var output bytes.Buffer
	writeProvisioningConfiguration(&output, "Development configuration", packages, codingAgentSyncConfiguration{OpenCode: true, ClaudeCode: true, Codex: true})
	if !strings.HasPrefix(output.String(), "Development configuration:\n  - Claude Code\n") || strings.Contains(output.String(), ", ") {
		t.Fatalf("configuration output is not a sorted bullet list: %q", output.String())
	}
}

func TestCodingAgentPowerShellSyncPreservesAbsentAndExcludedStateAndRejectsJunctions(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows PowerShell 5.1 configuration-sync regression")
	}
	contents := configurationSyncScript
	startMarker := []byte("$script:CopiedConfigurationFiles = 0")
	endMarker := []byte("function Invoke-GuestGitHubCLI {")
	start := bytes.Index(contents, startMarker)
	end := bytes.Index(contents, endMarker)
	if start < 0 || end <= start {
		t.Fatal("configuration-sync PowerShell helper block was not found")
	}

	root := t.TempDir()
	sourceRoot := filepath.Join(root, "source")
	destinationRoot := filepath.Join(root, "destination")
	for _, directory := range []string{filepath.Join(sourceRoot, ".git"), filepath.Join(destinationRoot, "plugins", "cache")} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	writeTestFile(t, filepath.Join(sourceRoot, "settings.json"), `{"enabled":true}`)
	writeTestFile(t, filepath.Join(sourceRoot, ".git", "config"), "[remote \"origin\"]\n\turl = https://example.invalid/agent.git\n")
	writeTestFile(t, filepath.Join(destinationRoot, "plugins", "cache", "keep.txt"), "keep")
	writeTestFile(t, filepath.Join(destinationRoot, "removed.md"), "stale tracked file")
	existingAuth := filepath.Join(root, "auth.json")
	writeTestFile(t, existingAuth, "keep-auth")
	junctionTarget := filepath.Join(root, "junction-target")
	if err := os.MkdirAll(junctionTarget, 0o700); err != nil {
		t.Fatal(err)
	}
	junction := filepath.Join(root, "junction")
	createTestDirectoryLink(t, junction, junctionTarget)
	junctionSource := filepath.Join(root, "junction-source.json")
	writeTestFile(t, junctionSource, `{}`)

	script := string(contents[start:end]) + `
Sync-VerifiedConfigurationRoot -Source $env:SYNC_SOURCE -Destination $env:SYNC_DESTINATION
if (-not (Test-Path -LiteralPath (Join-Path $env:SYNC_DESTINATION 'settings.json') -PathType Leaf)) { throw 'Supplied configuration was not copied.' }
if (-not (Test-Path -LiteralPath (Join-Path $env:SYNC_DESTINATION '.git\config') -PathType Leaf)) { throw 'Git repository metadata was not copied.' }
if (-not (Test-Path -LiteralPath (Join-Path $env:SYNC_DESTINATION 'plugins\cache\keep.txt') -PathType Leaf)) { throw 'Excluded destination state was removed.' }
Remove-VerifiedTrackedConfigurationFiles -Destination $env:SYNC_DESTINATION -Paths @('removed.md')
if (Test-Path -LiteralPath (Join-Path $env:SYNC_DESTINATION 'removed.md')) { throw 'Tracked deletion was not applied.' }
$unsafeDeletionRejected = $false
try {
    Remove-VerifiedTrackedConfigurationFiles -Destination $env:SYNC_DESTINATION -Paths @('../outside.txt')
	} catch {
	    if ($_.Exception.Message -notmatch 'unsafe') { throw }
	    $unsafeDeletionRejected = $true
	}
if (-not $unsafeDeletionRejected) { throw 'Unsafe tracked deletion was accepted.' }
Sync-OptionalConfigurationFile -Source $env:SYNC_MISSING_AUTH -Destination $env:SYNC_EXISTING_AUTH
if ([IO.File]::ReadAllText($env:SYNC_EXISTING_AUTH) -cne 'keep-auth') { throw 'Absent authentication source changed the destination.' }
$rejected = $false
try {
    Copy-VerifiedConfigurationFile -Source $env:SYNC_JUNCTION_SOURCE -Destination (Join-Path $env:SYNC_JUNCTION 'config.json')
} catch {
    if ($_.Exception.Message -notmatch 'reparse point') { throw }
    $rejected = $true
}
if (-not $rejected) { throw 'Destination junction was not rejected.' }
`
	scriptPath := filepath.Join(root, "coding-agent-sync-regression.ps1")
	writeTestFile(t, scriptPath, script)
	command := hiddenCommand(mustWindowsPowerShellPath(t), "-NoLogo", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File", scriptPath)
	command.Env = append(os.Environ(),
		"SYNC_SOURCE="+sourceRoot,
		"SYNC_DESTINATION="+destinationRoot,
		"SYNC_MISSING_AUTH="+filepath.Join(root, "missing-auth.json"),
		"SYNC_EXISTING_AUTH="+existingAuth,
		"SYNC_JUNCTION_SOURCE="+junctionSource,
		"SYNC_JUNCTION="+junction,
	)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("coding-agent PowerShell sync regression: %v: %s", err, output)
	}
}

func TestCodingAgentPowerShellRewritesSyncedHerdrHookPaths(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows PowerShell 5.1 configuration-sync regression")
	}
	requireExternalBoundaryTest(t, "Windows PowerShell 5.1 configuration sync")
	contents := configurationSyncScript
	start := bytes.Index(contents, []byte("$script:CopiedConfigurationFiles = 0"))
	end := bytes.Index(contents, []byte("function Invoke-GuestGitHubCLI {"))
	if start < 0 || end <= start {
		t.Fatal("configuration-sync PowerShell helper block was not found")
	}

	root := t.TempDir()
	hostHook := filepath.Join(root, "host-user", ".claude", "hooks", "herdr-agent-state.ps1")
	guestHook := filepath.Join(root, "guest-user", ".claude", "hooks", "herdr-agent-state.ps1")
	configurationPath := filepath.Join(root, "guest-user", ".claude", "settings.json")
	if err := os.MkdirAll(filepath.Dir(configurationPath), 0o700); err != nil {
		t.Fatal(err)
	}
	hostCommand := `powershell -NoProfile -ExecutionPolicy Bypass -File "` + hostHook + `" session`
	settings, err := json.Marshal(map[string]any{
		"hooks": map[string]any{
			"SessionStart": []any{map[string]any{
				"matcher": "*",
				"hooks": []any{map[string]any{
					"type":    "command",
					"command": hostCommand,
					"timeout": 10,
				}},
			}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, configurationPath, string(settings))
	missingConfigurationPath := filepath.Join(root, "guest-user", ".codex", "hooks.json")
	if err := os.MkdirAll(filepath.Dir(missingConfigurationPath), 0o700); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, missingConfigurationPath, `{}`)

	script := string(contents[start:end]) + `
Rewrite-SyncedHerdrHookPath -ConfigurationPath $env:SYNC_CONFIGURATION -SourceHookPath $env:SYNC_HOST_HOOK -DestinationHookPath $env:SYNC_GUEST_HOOK
$rejected = $false
try {
    Rewrite-SyncedHerdrHookPath -ConfigurationPath $env:SYNC_MISSING_CONFIGURATION -SourceHookPath $env:SYNC_HOST_HOOK -DestinationHookPath $env:SYNC_GUEST_HOOK
} catch {
    if ($_.Exception.Message -notmatch 'does not reference') { throw }
    $rejected = $true
}
if (-not $rejected) { throw 'A synced integration without its hook registration was accepted.' }
`
	scriptPath := filepath.Join(root, "synced-herdr-hook-path.ps1")
	writeTestFile(t, scriptPath, script)
	command := hiddenCommand(mustWindowsPowerShellPath(t), "-NoLogo", "-NoProfile", "-NonInteractive", "-WindowStyle", "Hidden", "-ExecutionPolicy", "Bypass", "-File", scriptPath)
	command.Env = append(os.Environ(),
		"SYNC_CONFIGURATION="+configurationPath,
		"SYNC_MISSING_CONFIGURATION="+missingConfigurationPath,
		"SYNC_HOST_HOOK="+hostHook,
		"SYNC_GUEST_HOOK="+guestHook,
	)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("synced Herdr hook path rewrite: %v: %s", err, output)
	}
	updated, err := os.ReadFile(configurationPath)
	if err != nil {
		t.Fatal(err)
	}
	hostHookJSON, err := json.Marshal(hostHook)
	if err != nil {
		t.Fatal(err)
	}
	guestHookJSON, err := json.Marshal(guestHook)
	if err != nil {
		t.Fatal(err)
	}
	hostHookLiteral := string(hostHookJSON[1 : len(hostHookJSON)-1])
	guestHookLiteral := string(guestHookJSON[1 : len(guestHookJSON)-1])
	if strings.Contains(strings.ToLower(string(updated)), strings.ToLower(hostHookLiteral)) ||
		!strings.Contains(strings.ToLower(string(updated)), strings.ToLower(guestHookLiteral)) {
		t.Fatalf("rewritten Herdr hook configuration = %s", updated)
	}
}

func TestCodingAgentPowerShellProjectsManagedWorktreeInstructionsIdempotently(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows PowerShell 5.1 configuration-sync regression")
	}
	contents := configurationSyncScript
	start := bytes.Index(contents, []byte("$script:CopiedConfigurationFiles = 0"))
	end := bytes.Index(contents, []byte("function Get-OpenCodeAllowAllPermissions"))
	if start < 0 || end <= start {
		t.Fatal("configuration-sync managed-instruction helper block was not found")
	}
	root := t.TempDir()
	source := filepath.Join(root, "agent-worktree-instructions.md")
	writeTestFile(t, source, string(agentWorktreeInstructions))
	destinations := []string{
		filepath.Join(root, ".config", "opencode", "AGENTS.md"),
		filepath.Join(root, ".claude", "CLAUDE.md"),
		filepath.Join(root, ".codex", "AGENTS.md"),
		filepath.Join(root, ".copilot", "instructions", "herdr-sandbox-worktrees.instructions.md"),
		filepath.Join(root, ".pi", "agent", "AGENTS.md"),
	}
	for _, destination := range destinations {
		if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
			t.Fatal(err)
		}
		writeTestFile(t, destination, "personal instructions\r\n")
	}
	script := string(contents[start:end]) + `
$destinations = @((ConvertFrom-Json -InputObject $env:SYNC_DESTINATIONS))
Set-ManagedAgentWorktreeInstructions -Source $env:SYNC_SOURCE -Destinations $destinations
Set-ManagedAgentWorktreeInstructions -Source $env:SYNC_SOURCE -Destinations $destinations
foreach ($destination in $destinations) {
	$text = [IO.File]::ReadAllText([string]$destination).Replace([string]([char]13) + [char]10, [string][char]10)
    if (-not $text.StartsWith('<!-- herdr-sandbox:worktrees:start -->')) { throw "Managed block was not prepended: $destination" }
    if (([regex]::Matches($text, [regex]::Escape('<!-- herdr-sandbox:worktrees:start -->'))).Count -ne 1 -or
        ([regex]::Matches($text, [regex]::Escape('<!-- herdr-sandbox:worktrees:end -->'))).Count -ne 1) {
        throw "Managed block is not unique: $destination"
    }
	if (-not $text.EndsWith('personal instructions' + [char]10)) { throw "Personal instructions were not preserved: $destination" }
}

`
	encodedDestinations, err := json.Marshal(destinations)
	if err != nil {
		t.Fatal(err)
	}
	scriptPath := filepath.Join(root, "managed-worktree-instructions.ps1")
	writeTestFile(t, scriptPath, script)
	command := hiddenCommand(mustWindowsPowerShellPath(t), "-NoLogo", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File", scriptPath)
	command.Env = append(os.Environ(), "SYNC_SOURCE="+source, "SYNC_DESTINATIONS="+string(encodedDestinations))
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("managed worktree instruction projection: %v: %s", err, output)
	}

	malformed := filepath.Join(root, "malformed.md")
	writeTestFile(t, malformed, agentWorktreeInstructionsStart+"\n"+agentWorktreeInstructionsStart+"\n"+agentWorktreeInstructionsEnd+"\n")
	rejectionScript := string(contents[start:end]) + `
$rejected = $false
try {
    Set-ManagedAgentWorktreeInstructions -Source $env:SYNC_SOURCE -Destinations @($env:SYNC_DESTINATION)
} catch {
    if ($_.Exception.Message -notmatch 'ownership markers') { throw }
    $rejected = $true
}
if (-not $rejected) { throw 'Malformed ownership markers were accepted.' }
`
	rejectionScriptPath := filepath.Join(root, "managed-worktree-instructions-rejection.ps1")
	writeTestFile(t, rejectionScriptPath, rejectionScript)
	command = hiddenCommand(mustWindowsPowerShellPath(t), "-NoLogo", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File", rejectionScriptPath)
	command.Env = append(os.Environ(), "SYNC_SOURCE="+source, "SYNC_DESTINATION="+malformed)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("malformed managed worktree instruction rejection: %v: %s", err, output)
	}
}

func TestCodingAgentPowerShellKeepsManagedWorktreeInstructionsOutOfGit(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows PowerShell 5.1 configuration-sync Git protection regression")
	}
	contents := configurationSyncScript
	start := bytes.Index(contents, []byte("$script:CopiedConfigurationFiles = 0"))
	end := bytes.Index(contents, []byte("function Get-OpenCodeAllowAllPermissions"))
	if start < 0 || end <= start {
		t.Fatal("configuration-sync managed-instruction helper block was not found")
	}

	root := t.TempDir()
	trackedRepository := filepath.Join(root, "tracked")
	generatedRepository := filepath.Join(root, "generated")
	for _, directory := range []string{trackedRepository, generatedRepository} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	trackedInstructions := filepath.Join(trackedRepository, "AGENTS.md")
	writeTestFile(t, trackedInstructions, "personal instructions\n")
	initializeAgentGitRepository(t, trackedRepository, "tracked", []string{"AGENTS.md"})
	writeTestFile(t, filepath.Join(generatedRepository, "settings.json"), "{}\n")
	initializeAgentGitRepository(t, generatedRepository, "generated", []string{"settings.json"})

	instructionSource := filepath.Join(root, "agent-worktree-instructions.md")
	filterPath := filepath.Join(root, "agent-worktree-clean.ps1")
	writeTestFile(t, instructionSource, string(agentWorktreeInstructions))
	writeTestFile(t, filterPath, string(agentWorktreeCleanFilter))
	generatedRelative := "instructions/herdr-sandbox-worktrees.instructions.md"
	generatedInstructions := filepath.Join(generatedRepository, filepath.FromSlash(generatedRelative))

	script := string(contents[start:end]) + `
Set-ManagedAgentWorktreeInstructions -Source $env:SYNC_INSTRUCTIONS -Destinations @($env:SYNC_TRACKED_DESTINATION, $env:SYNC_GENERATED_DESTINATION)
Set-ManagedAgentWorktreeInstructions -Source $env:SYNC_INSTRUCTIONS -Destinations @($env:SYNC_TRACKED_DESTINATION, $env:SYNC_GENERATED_DESTINATION)
Protect-ManagedAgentWorktreeInstructions -ConfigurationRoot $env:SYNC_TRACKED_ROOT -RelativePath 'AGENTS.md' -ArchivedSource $env:SYNC_TRACKED_SOURCE -FilterScript $env:SYNC_FILTER
Protect-ManagedAgentWorktreeInstructions -ConfigurationRoot $env:SYNC_GENERATED_ROOT -RelativePath 'instructions/herdr-sandbox-worktrees.instructions.md' -ArchivedSource $env:SYNC_MISSING_SOURCE -FilterScript $env:SYNC_FILTER
`
	scriptPath := filepath.Join(root, "managed-worktree-git-protection.ps1")
	writeTestFile(t, scriptPath, script)
	command := hiddenCommand(mustWindowsPowerShellPath(t), "-NoLogo", "-NoProfile", "-NonInteractive", "-WindowStyle", "Hidden", "-ExecutionPolicy", "Bypass", "-File", scriptPath)
	command.Env = append(os.Environ(),
		"SYNC_TRACKED_ROOT="+trackedRepository,
		"SYNC_TRACKED_SOURCE="+trackedInstructions,
		"SYNC_TRACKED_DESTINATION="+trackedInstructions,
		"SYNC_GENERATED_ROOT="+generatedRepository,
		"SYNC_GENERATED_DESTINATION="+generatedInstructions,
		"SYNC_MISSING_SOURCE="+filepath.Join(root, "missing.instructions.md"),
		"SYNC_INSTRUCTIONS="+instructionSource,
		"SYNC_FILTER="+filterPath,
	)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("managed worktree Git protection: %v: %s", err, output)
	}

	if got := strings.TrimSpace(runAgentGitTest(t, trackedRepository, "status", "--porcelain=v1")); got != "" {
		t.Fatalf("managed tracked instructions changed Git status: %q", got)
	}
	if got := strings.TrimSpace(runAgentGitTest(t, generatedRepository, "status", "--porcelain=v1")); got != "" {
		t.Fatalf("managed generated instructions changed Git status: %q", got)
	}
	working, err := os.ReadFile(trackedInstructions)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(working, []byte(agentWorktreeInstructionsStart)) || !bytes.Contains(working, []byte("personal instructions")) {
		t.Fatalf("tracked working instructions lost guest or personal content: %q", working)
	}
	linkedWorktree := filepath.Join(root, "tracked-worktree")
	runAgentGitTest(t, trackedRepository, "worktree", "add", "--detach", linkedWorktree, "HEAD")
	linkedInstructions, err := os.ReadFile(filepath.Join(linkedWorktree, "AGENTS.md"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(linkedInstructions, []byte(agentWorktreeInstructionsStart)) ||
		!bytes.Contains(linkedInstructions, []byte("personal instructions")) {
		t.Fatalf("linked worktree instructions = %q", linkedInstructions)
	}
	runAgentGitTest(t, trackedRepository, "worktree", "remove", linkedWorktree)
	writeTestFile(t, trackedInstructions, string(working)+"legitimate guest edit\n")
	runAgentGitTest(t, trackedRepository, "add", "--", "AGENTS.md")
	staged := runAgentGitTest(t, trackedRepository, "show", ":AGENTS.md")
	if strings.Contains(staged, agentWorktreeInstructionsStart) || strings.Contains(staged, `C:\Worktrees`) ||
		!strings.Contains(staged, "personal instructions") || !strings.Contains(staged, "legitimate guest edit") {
		t.Fatalf("staged instructions = %q", staged)
	}
	runAgentGitTest(t, generatedRepository, "add", "--all")
	if got := strings.TrimSpace(runAgentGitTest(t, generatedRepository, "ls-files", "--cached", "--", generatedRelative)); got != "" {
		t.Fatalf("generated guest-only instructions entered the index: %q", got)
	}
}

func initializeAgentGitRepository(t *testing.T, directory, name string, tracked []string) {
	t.Helper()
	runAgentGitTest(t, directory, "init", "--initial-branch=main")
	runAgentGitTest(t, directory, "config", "user.name", "Herdr Sandbox Test")
	runAgentGitTest(t, directory, "config", "user.email", "herdr-sandbox@example.invalid")
	arguments := append([]string{"add", "--"}, tracked...)
	runAgentGitTest(t, directory, arguments...)
	runAgentGitTest(t, directory, "-c", "core.hooksPath="+os.DevNull, "commit", "-m", "fixture")
	runAgentGitTest(t, directory, "remote", "add", "origin", "https://example.invalid/"+name+".git")
	runAgentGitTest(t, directory, "config", "branch.main.remote", "origin")
	runAgentGitTest(t, directory, "config", "branch.main.merge", "refs/heads/main")
	runAgentGitTest(t, directory, "update-ref", "refs/remotes/origin/main", "HEAD")
}

func initializeAgentGitRepositoryAtRemote(t *testing.T, directory, remote string, tracked []string) {
	t.Helper()
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, relative := range tracked {
		path := filepath.Join(directory, filepath.FromSlash(relative))
		if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
			if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
				t.Fatal(err)
			}
			writeTestFile(t, path, relative)
		} else if err != nil {
			t.Fatal(err)
		}
	}
	runAgentGitTest(t, directory, "init", "--initial-branch=main")
	configureAgentGitTestIdentity(t, directory)
	arguments := append([]string{"add", "--"}, tracked...)
	runAgentGitTest(t, directory, arguments...)
	runAgentGitTest(t, directory, "-c", "core.hooksPath="+os.DevNull, "commit", "-m", "fixture")
	runAgentGitTest(t, directory, "remote", "add", "origin", remote)
	runAgentGitTest(t, directory, "push", "--set-upstream", "origin", "main")
}

func configureAgentGitTestIdentity(t *testing.T, directory string) {
	t.Helper()
	runAgentGitTest(t, directory, "config", "user.name", "Herdr Sandbox Test")
	runAgentGitTest(t, directory, "config", "user.email", "herdr-sandbox@example.invalid")
}

func runAgentGitTest(t *testing.T, directory string, arguments ...string) string {
	t.Helper()
	requireExternalBoundaryTest(t, "Git repository integration")
	commandArguments := append([]string{"-C", directory}, arguments...)
	command := hiddenCommand("git", commandArguments...)
	command.Env = agentGitTestEnvironment(command.Env)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(arguments, " "), err, output)
	}
	return string(output)
}

func agentGitTestEnvironment(parent []string) []string {
	return append(withoutAgentGitOverrides(parent),
		"GIT_TERMINAL_PROMPT=0",
		"GCM_INTERACTIVE=Never",
		"SSH_ASKPASS_REQUIRE=never",
	)
}

func readConfigurationArchiveForTest(t *testing.T, data []byte) (map[string]bool, map[string][]byte) {
	t.Helper()
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	entries := map[string]bool{}
	contents := map[string][]byte{}
	for _, file := range reader.File {
		entries[file.Name] = true
		stream, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		contents[file.Name], err = io.ReadAll(stream)
		stream.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
	return entries, contents
}
