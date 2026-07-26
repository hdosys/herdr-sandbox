package sandbox

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"runtime"
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
	sources, err := defaultCodingAgentConfigurationSources(home, defaultCodingAgentSyncConfiguration())
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
			if _, err := defaultCodingAgentConfigurationSources(t.TempDir(), defaultCodingAgentSyncConfiguration()); err == nil || !strings.Contains(err.Error(), name) {
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
		filepath.Join(openCode, "agents"), filepath.Join(openCode, "node_modules"),
		filepath.Join(claude, "agents"), filepath.Join(claude, "projects"),
		filepath.Join(codex, "agents"), filepath.Join(codex, "skills", ".system"), filepath.Join(codex, "sessions"),
		filepath.Join(copilot, "agents"), filepath.Join(copilot, "session-state"),
		filepath.Join(pi, "extensions", "fixture"), filepath.Join(pi, "extensions", "fixture", "node_modules"), filepath.Join(pi, "sessions"),
		filepath.Join(sharedSkills, "fixture"),
	} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	writeTestFile(t, filepath.Join(openCode, "opencode.json"), `{}`)
	writeTestFile(t, filepath.Join(openCode, "agents", "builder.md"), "agent")
	writeTestFile(t, filepath.Join(openCode, "node_modules", "excluded.js"), "excluded")
	openCodeAuth := filepath.Join(root, "opencode-auth.json")
	writeTestFile(t, openCodeAuth, `{"provider":"fixture"}`)

	writeTestFile(t, filepath.Join(claude, "settings.json"), `{}`)
	writeTestFile(t, filepath.Join(claude, "agents", "reviewer.md"), "agent")
	writeTestFile(t, filepath.Join(claude, "projects", "excluded.jsonl"), "excluded")
	claudeAuth := filepath.Join(claude, ".credentials.json")
	writeTestFile(t, claudeAuth, `{"claudeAiOauth":{"accessToken":"fixture"}}`)
	claudeState := filepath.Join(root, ".claude.json")
	writeTestFile(t, claudeState, `{"mcpServers":{"fixture":{"command":"tool"}},"projects":{"C:\\host":{}}}`)

	writeTestFile(t, filepath.Join(codex, "config.toml"), `model = "fixture"`)
	writeTestFile(t, filepath.Join(codex, "work.config.toml"), `model = "profile"`)
	writeTestFile(t, filepath.Join(codex, "agents", "reviewer.toml"), `name = "reviewer"`)
	writeTestFile(t, filepath.Join(codex, "skills", ".system", "excluded.md"), "excluded")
	writeTestFile(t, filepath.Join(codex, "sessions", "excluded.jsonl"), "excluded")
	writeTestFile(t, filepath.Join(codex, "auth.json"), `{"tokens":{"access_token":"fixture"}}`)
	writeTestFile(t, filepath.Join(codex, ".credentials.json"), `{"oauth":"fixture"}`)

	writeTestFile(t, filepath.Join(copilot, "settings.json"), `{}`)
	writeTestFile(t, filepath.Join(copilot, "agents", "builder.agent.md"), "agent")
	writeTestFile(t, filepath.Join(copilot, "config.json"), `{"token":"excluded"}`)
	writeTestFile(t, filepath.Join(copilot, "session-state", "excluded.json"), "excluded")

	writeTestFile(t, filepath.Join(pi, "settings.json"), `{}`)
	writeTestFile(t, filepath.Join(pi, "models.json"), `{"providers":{}}`)
	writeTestFile(t, filepath.Join(pi, "CLAUDE.md"), "pi instructions")
	writeTestFile(t, filepath.Join(pi, "extensions", "fixture", "index.ts"), "export default {}")
	writeTestFile(t, filepath.Join(pi, "extensions", "fixture", "node_modules", "excluded.js"), "excluded")
	writeTestFile(t, filepath.Join(pi, "sessions", "excluded.jsonl"), "excluded")
	writeTestFile(t, filepath.Join(pi, "auth.json"), `{"provider":{"type":"api_key","key":"fixture"}}`)
	writeTestFile(t, filepath.Join(sharedSkills, "fixture", "SKILL.md"), "skill")

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

	data, err := buildDevelopmentConfigurationArchive(hostConfigurationSources{
		CodingAgents: codingAgentConfigurationSources{
			Selection:                defaultCodingAgentSyncConfiguration(),
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
		"opencode/opencode.json", "opencode/agents/builder.md", "opencode-auth/auth.json",
		"claude-code/settings.json", "claude-code/agents/reviewer.md", "claude-code-auth/.credentials.json", "claude-code-state/.claude.json",
		"codex/config.toml", "codex/work.config.toml", "codex/agents/reviewer.toml", "codex-auth/auth.json", "codex-auth/.credentials.json",
		"github-copilot/settings.json", "github-copilot/agents/builder.agent.md",
		"pi/settings.json", "pi/models.json", "pi/AGENTS.md", "pi/extensions/fixture/index.ts", "pi-auth/auth.json",
		"shared-agent-skills/fixture/SKILL.md",
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
	if err := archiveCodingAgentConfiguration(codingAgentConfigurationSources{Selection: codingAgentSyncConfiguration{}}, func(string, string) error {
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

func TestProvisioningConfigurationSummaryIncludesSelectedCodingAgentsOnce(t *testing.T) {
	terminal := testStableWindowsTerminalConfiguration()
	packages, err := resolveWingetPackagePlan(defaultWingetPackageConfiguration(), terminal)
	if err != nil {
		t.Fatal(err)
	}
	summary := provisioningConfigurationSummary(packages, codingAgentSyncConfiguration{OpenCode: true, ClaudeCode: true, Codex: true})
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
}

func TestCodingAgentPowerShellSyncPreservesAbsentAndExcludedStateAndRejectsJunctions(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows PowerShell 5.1 configuration-sync regression")
	}
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test source path")
	}
	contents, err := os.ReadFile(filepath.Join(filepath.Dir(currentFile), "configuration_sync.go"))
	if err != nil {
		t.Fatal(err)
	}
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
	for _, directory := range []string{sourceRoot, filepath.Join(destinationRoot, "plugins", "cache")} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	writeTestFile(t, filepath.Join(sourceRoot, "settings.json"), `{"enabled":true}`)
	writeTestFile(t, filepath.Join(destinationRoot, "plugins", "cache", "keep.txt"), "keep")
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
if (-not (Test-Path -LiteralPath (Join-Path $env:SYNC_DESTINATION 'plugins\cache\keep.txt') -PathType Leaf)) { throw 'Excluded destination state was removed.' }
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
	command := hiddenCommand(mustWindowsPowerShellPath(t), "-NoLogo", "-NoProfile", "-NonInteractive", "-EncodedCommand", encodePowerShell(script))
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
