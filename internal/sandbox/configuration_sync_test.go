package sandbox

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
	"unicode/utf16"
)

func TestBuildDevelopmentConfigurationArchiveUsesAllowlistAndAuthentication(t *testing.T) {
	root := t.TempDir()
	openCode := filepath.Join(root, "opencode")
	if err := os.MkdirAll(filepath.Join(openCode, "agents"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(openCode, "plugin"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(openCode, "node_modules"), 0o700); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(openCode, "opencode.json"), `{}`)
	writeTestFile(t, filepath.Join(openCode, "AGENTS.md"), "global instructions")
	writeTestFile(t, filepath.Join(openCode, "agents", "builder.md"), "agent")
	writeTestFile(t, filepath.Join(openCode, "plugin", "provider.js"), "export default () => ({})")
	writeTestFile(t, filepath.Join(openCode, "node_modules", "excluded.txt"), "excluded")
	authentication := filepath.Join(root, "auth.json")
	writeTestFile(t, authentication, `{"provider":"credential"}`)
	githubCLI := filepath.Join(root, "github-cli")
	if err := os.MkdirAll(filepath.Join(githubCLI, "extensions"), 0o700); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(githubCLI, "config.yml"), "git_protocol: https\n")
	writeTestFile(t, filepath.Join(githubCLI, "hosts.yml"), "github.com:\n  user: fixture\n")
	writeTestFile(t, filepath.Join(githubCLI, "extensions", "excluded.yml"), "excluded")
	githubAuthentication := []byte(`{"schemaVersion":1,"accounts":[{"hostname":"github.com","login":"fixture","active":true,"gitProtocol":"https","token":"fixture-token"}]}`)
	herdrConfigDirectory := filepath.Join(root, "herdr")
	if err := os.MkdirAll(filepath.Join(herdrConfigDirectory, "nested"), 0o700); err != nil {
		t.Fatal(err)
	}
	herdrConfig := filepath.Join(herdrConfigDirectory, "config.toml")
	writeTestFile(t, herdrConfig, "[terminal]\ndefault_shell = \"nu\"\n[theme]\nname = \"one-light\"\n")
	writeTestFile(t, filepath.Join(herdrConfigDirectory, "keys.toml"), "[keys]\nprefix = \"ctrl+a\"\n")
	writeTestFile(t, filepath.Join(herdrConfigDirectory, "herdr-client.log"), "excluded")
	writeTestFile(t, filepath.Join(herdrConfigDirectory, "nested", "excluded.toml"), "excluded")
	gitConfig := filepath.Join(root, ".gitconfig")
	writeTestFile(t, gitConfig, "[user]\nname = Test User\n")
	settings := filepath.Join(root, "settings.json")
	writeTestFile(t, settings, `{}`)
	workspaceManifest := filepath.Join(root, workspaceManifestName)
	writeTestFile(t, workspaceManifest, `{"schemaVersion":1,"activeWorkspace":"C:\\Workspaces\\fixture","workspaces":[{"name":"fixture","directory":"C:\\Workspaces\\fixture"}]}`)
	terminal := windowsTerminalConfiguration{
		Edition:           windowsTerminalPreviewEdition,
		Theme:             windowsTerminalLightTheme,
		WinGetPackageID:   packageTerminalPreview,
		PackageFamilyName: "Microsoft.WindowsTerminalPreview_8wekyb3d8bbwe",
	}
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

	data, err := buildDevelopmentConfigurationArchive(context.Background(), hostConfigurationSources{
		GitConfig:               gitConfig,
		GitHubCLIConfiguration:  githubCLI,
		GitHubCLIAuthentication: githubAuthentication,
		CodingAgents: codingAgentConfigurationSources{
			Selection:              defaultCodingAgentSyncConfiguration(),
			OpenCodeDirectory:      openCode,
			OpenCodeAuthentication: authentication,
		},
		HerdrConfig:             herdrConfig,
		WindowsTerminalSettings: settings,
		WindowsTerminalEdition:  windowsTerminalPreviewEdition,
		StarshipPreset:          starshipCatppuccinLattePreset,
		WorkspaceManifest:       workspaceManifest,
		PackagePlan:             packagePlan,
	}, []byte("Write-Output 'apply fixture'\n"))
	if err != nil {
		t.Fatalf("buildDevelopmentConfigurationArchive: %v", err)
	}
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	entries := map[string]bool{}
	var archivedTerminalSettings []byte
	var archivedHerdrConfig []byte
	var archivedStarshipPreset []byte
	for _, file := range reader.File {
		entries[file.Name] = true
		if file.Name == "windows-terminal/settings.json" {
			stream, openErr := file.Open()
			if openErr != nil {
				t.Fatal(openErr)
			}
			archivedTerminalSettings, err = io.ReadAll(stream)
			stream.Close()
			if err != nil {
				t.Fatal(err)
			}
		}
		if file.Name == "herdr/config.toml" {
			stream, openErr := file.Open()
			if openErr != nil {
				t.Fatal(openErr)
			}
			archivedHerdrConfig, err = io.ReadAll(stream)
			stream.Close()
			if err != nil {
				t.Fatal(err)
			}
		}
		if file.Name == starshipPresetArchivePath {
			stream, openErr := file.Open()
			if openErr != nil {
				t.Fatal(openErr)
			}
			archivedStarshipPreset, err = io.ReadAll(stream)
			stream.Close()
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, required := range []string{configurationApplyScriptArchivePath, configurationPackagePlanArchivePath, configurationWorkspaceManifestPath, "git/.gitconfig", "github-cli/config.yml", "github-cli/hosts.yml", githubCLIAuthenticationArchivePath, "opencode/opencode.json", "opencode/AGENTS.md", "opencode/agents/builder.md", "opencode/plugin/provider.js", "opencode-auth/auth.json", "herdr/config.toml", windowsTerminalEditionArchivePath, starshipPresetArchivePath, "windows-terminal/settings.json"} {
		if !entries[required] {
			t.Fatalf("archive is missing %s: %#v", required, entries)
		}
	}
	if entries["opencode/node_modules/excluded.txt"] {
		t.Fatal("archive contains node_modules")
	}
	if entries["github-cli/extensions/excluded.yml"] {
		t.Fatal("archive contains GitHub CLI extensions")
	}
	for _, excluded := range []string{"herdr/keys.toml", "herdr/herdr-client.log", "herdr/nested/excluded.toml"} {
		if entries[excluded] {
			t.Fatalf("archive contains excluded Herdr state %s", excluded)
		}
	}
	if count, err := configurationArchivePayloadFileCount(data); err != nil || count != 10 {
		t.Fatalf("payload file count = %d, err = %v", count, err)
	}
	var patched map[string]any
	if err := json.Unmarshal(archivedTerminalSettings, &patched); err != nil {
		t.Fatalf("decode archived Terminal settings: %v", err)
	}
	if patched["defaultProfile"] != powerShellProfileGUID {
		t.Fatalf("archived defaultProfile = %#v", patched["defaultProfile"])
	}
	profiles := patched["profiles"].(map[string]any)
	if profiles["defaults"].(map[string]any)["startingDirectory"] != `C:\Workspaces\fixture` {
		t.Fatalf("archived Terminal defaults = %#v", profiles["defaults"])
	}
	for _, value := range profiles["list"].([]any) {
		if profile := value.(map[string]any); profile["startingDirectory"] != `C:\Workspaces\fixture` {
			t.Fatalf("archived Terminal profile = %#v", profile)
		}
	}
	if !strings.Contains(string(archivedHerdrConfig), `default_shell = "pwsh.exe"`) {
		t.Fatalf("archived Herdr config = %q", archivedHerdrConfig)
	}
	if string(archivedStarshipPreset) != starshipCatppuccinLattePreset+"\n" {
		t.Fatalf("archived Starship preset = %q", archivedStarshipPreset)
	}
}

func TestBuildDevelopmentConfigurationArchiveAllowsMissingGitHubCLIHosts(t *testing.T) {
	root := t.TempDir()
	githubCLI := filepath.Join(root, "github-cli")
	if err := os.MkdirAll(githubCLI, 0o700); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(githubCLI, "config.yml"), "git_protocol: https\n")
	packagePlan := filepath.Join(root, wingetPackagePlanFileName)
	writeTestFile(t, packagePlan, `{}`)
	herdrConfig := filepath.Join(root, "herdr-config.toml")
	writeTestFile(t, herdrConfig, "[terminal]\ndefault_shell = \"nu\"\n")

	data, err := buildDevelopmentConfigurationArchive(context.Background(), hostConfigurationSources{
		GitHubCLIConfiguration:  githubCLI,
		GitHubCLIAuthentication: []byte(`{"schemaVersion":1,"accounts":[]}`),
		HerdrConfig:             herdrConfig,
		PackagePlan:             packagePlan,
	}, []byte("Write-Output 'apply fixture'\n"))
	if err != nil {
		t.Fatalf("build archive without GitHub CLI hosts.yml: %v", err)
	}
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	entries := map[string]bool{}
	for _, file := range reader.File {
		entries[file.Name] = true
	}
	if !entries["github-cli/config.yml"] || !entries[githubCLIAuthenticationArchivePath] {
		t.Fatalf("archive is missing available GitHub CLI state: %#v", entries)
	}
	if entries["github-cli/hosts.yml"] {
		t.Fatalf("archive synthesized missing GitHub CLI hosts.yml: %#v", entries)
	}
}

func TestDisabledPackageIntegrationsAreNotDiscoveredOrArchived(t *testing.T) {
	root := t.TempDir()
	t.Setenv("APPDATA", root)
	t.Setenv("XDG_CONFIG_HOME", "relative-must-not-be-read")
	t.Setenv("XDG_DATA_HOME", "relative-must-not-be-read")
	t.Setenv("GH_CONFIG_DIR", "relative-must-not-be-read")
	terminal := testStableWindowsTerminalConfiguration()
	packages, err := resolveWingetPackagePlan(wingetPackageConfiguration{
		Remove: []string{packageGit, packageGitHubCLI, packageStarship, packageTerminalStable},
		Add:    []string{}, Versions: map[string]string{},
	}, terminal)
	if err != nil {
		t.Fatal(err)
	}
	sources, err := defaultHostConfigurationSources(terminal, packages, codingAgentSyncConfiguration{}, false)
	if err != nil {
		t.Fatalf("disabled integrations triggered host discovery: %v", err)
	}
	if sources.GitConfig != "" || sources.GitHubCLIConfiguration != "" || sources.CodingAgents.OpenCodeDirectory != "" ||
		sources.StarshipPreset != "" || sources.WindowsTerminalEdition != "" {
		t.Fatalf("disabled integration sources = %#v", sources)
	}
	if sources.HerdrConfig != filepath.Join(root, "herdr", "config.toml") {
		t.Fatalf("Herdr configuration source = %q, want only config.toml", sources.HerdrConfig)
	}
	herdrConfig := filepath.Join(root, "herdr.toml")
	writeTestFile(t, herdrConfig, "[terminal]\ndefault_shell = \"nu\"\n")
	packagePlan := filepath.Join(root, wingetPackagePlanFileName)
	packagePlanData, err := encodeWingetPackagePlan(packages, terminal)
	if err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, packagePlan, string(packagePlanData))
	sources.HerdrConfig = herdrConfig
	sources.PackagePlan = packagePlan
	data, err := buildDevelopmentConfigurationArchive(context.Background(), sources, []byte("Write-Output 'apply fixture'\n"))
	if err != nil {
		t.Fatalf("build minimal configuration archive: %v", err)
	}
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	entries := map[string]bool{}
	for _, file := range reader.File {
		entries[file.Name] = true
	}
	for _, forbidden := range []string{
		"git/.gitconfig", "github-cli/config.yml", "opencode/opencode.json",
		windowsTerminalEditionArchivePath, starshipPresetArchivePath, configurationWorkspaceManifestPath,
	} {
		if entries[forbidden] {
			t.Fatalf("archive contains disabled integration %s", forbidden)
		}
	}
	for _, required := range []string{configurationApplyScriptArchivePath, configurationPackagePlanArchivePath, "herdr/config.toml"} {
		if !entries[required] {
			t.Fatalf("minimal archive is missing %s", required)
		}
	}
	if count, err := configurationArchivePayloadFileCount(data); err != nil || count != 1 {
		t.Fatalf("minimal payload count = %d, err = %v", count, err)
	}
}

func TestPatchGuestWindowsTerminalSettingsUsesPowerShell7FontAndActiveWorkspace(t *testing.T) {
	input := []byte(`{
        "theme":"light",
        "defaultProfile":"{47302f9c-1ac4-566c-aa3e-8cf29889d6ab}",
        "profiles":{
			"defaults":{"startingDirectory":"D:\\host-default","font":{"face":"Custom Nerd Font","size":12}},
            "list":[
				{"guid":"{61C54BBD-C2C6-5271-96E7-009A87FF44BF}","hidden":false,"name":"Host shell","source":"host","startingDirectory":"D:\\\\","font":{"face":"Other Font"}},
                {"guid":"{47302f9c-1ac4-566c-aa3e-8cf29889d6ab}","name":"Nushell"}
            ]
        }
    }`)
	const activeWorkspace = `C:\Workspaces\fixture`
	patched, err := patchGuestWindowsTerminalSettings(input, activeWorkspace)
	if err != nil {
		t.Fatalf("patchGuestWindowsTerminalSettings: %v", err)
	}
	var settings map[string]any
	if err := json.Unmarshal(patched, &settings); err != nil {
		t.Fatal(err)
	}
	if settings["defaultProfile"] != powerShellProfileGUID || settings["theme"] != "light" {
		t.Fatalf("root settings = %#v", settings)
	}
	profiles := settings["profiles"].(map[string]any)
	defaults := profiles["defaults"].(map[string]any)
	if defaults["font"].(map[string]any)["face"] != windowsTerminalGuestFont {
		t.Fatalf("default font = %#v", defaults["font"])
	}
	if defaults["startingDirectory"] != activeWorkspace {
		t.Fatalf("default startingDirectory = %#v", defaults["startingDirectory"])
	}
	list := profiles["list"].([]any)
	powerShellFound := false
	legacyVisible := false
	for _, value := range list {
		profile := value.(map[string]any)
		if profile["font"].(map[string]any)["face"] != windowsTerminalGuestFont {
			t.Fatalf("profile font = %#v", profile)
		}
		if profile["startingDirectory"] != activeWorkspace {
			t.Fatalf("profile startingDirectory = %#v", profile)
		}
		guid, _ := profile["guid"].(string)
		if strings.EqualFold(guid, powerShellProfileGUID) {
			powerShellFound = true
			if profile["commandline"] != powerShellCommandLine || profile["hidden"] != false || profile["name"] != "PowerShell" || profile["source"] != "Windows.Terminal.PowershellCore" {
				t.Fatalf("PowerShell profile = %#v", profile)
			}
		} else if strings.EqualFold(guid, "{61c54bbd-c2c6-5271-96e7-009a87ff44bf}") && profile["hidden"] == false {
			legacyVisible = true
		}
	}
	if !powerShellFound || !legacyVisible {
		t.Fatalf("PowerShell profile state: found=%v legacyVisible=%v", powerShellFound, legacyVisible)
	}
	if _, err := patchGuestWindowsTerminalSettings([]byte(`{"profiles":{"list":[1]}}`), activeWorkspace); err == nil {
		t.Fatal("invalid Terminal profile unexpectedly succeeded")
	}
}

func TestPatchGuestWindowsTerminalSettingsDoesNotSynthesizeLegacyProfile(t *testing.T) {
	patched, err := patchGuestWindowsTerminalSettings([]byte(`{"profiles":{"defaults":{},"list":[]}}`), `C:\Workspaces\fixture`)
	if err != nil {
		t.Fatalf("patchGuestWindowsTerminalSettings: %v", err)
	}
	var settings map[string]any
	if err := json.Unmarshal(patched, &settings); err != nil {
		t.Fatal(err)
	}
	profiles := settings["profiles"].(map[string]any)["list"].([]any)
	legacyFound := false
	for _, value := range profiles {
		profile := value.(map[string]any)
		guid, _ := profile["guid"].(string)
		if strings.EqualFold(guid, "{61c54bbd-c2c6-5271-96e7-009a87ff44bf}") {
			legacyFound = true
		}
	}
	if legacyFound {
		t.Fatal("legacy Windows PowerShell profile was synthesized")
	}
}

func TestPatchGuestHerdrConfigUsesPowerShell7(t *testing.T) {
	input := []byte("onboarding = false\n\n[terminal]\ndefault_shell = \"nu\"\nshell_mode = \"non_login\"\n\n[theme]\nname = \"one-light\"\n")
	patched, err := patchGuestHerdrConfig(input)
	if err != nil {
		t.Fatalf("patchGuestHerdrConfig: %v", err)
	}
	text := string(patched)
	if !strings.Contains(text, "[terminal]\ndefault_shell = \"pwsh.exe\"\nshell_mode = \"non_login\"") ||
		!strings.Contains(text, "[theme]\nname = \"one-light\"") || strings.Contains(text, `default_shell = "nu"`) {
		t.Fatalf("patched config:\n%s", text)
	}
	withoutTerminal, err := patchGuestHerdrConfig([]byte("onboarding = false\n"))
	if err != nil || !strings.Contains(string(withoutTerminal), "[terminal]\ndefault_shell = \"pwsh.exe\"") {
		t.Fatalf("missing-section patch = %q, err = %v", withoutTerminal, err)
	}
	if _, err := patchGuestHerdrConfig([]byte("[terminal]\n[terminal]\n")); err == nil {
		t.Fatal("duplicate terminal sections unexpectedly succeeded")
	}
}

func TestBuildGuestHerdrConfigAllowsMissingHostConfig(t *testing.T) {
	config, err := buildGuestHerdrConfig(filepath.Join(t.TempDir(), "missing", "config.toml"))
	if err != nil {
		t.Fatalf("build missing host Herdr config: %v", err)
	}
	if string(config) != "[terminal]\ndefault_shell = \"pwsh.exe\"\n" {
		t.Fatalf("generated guest Herdr config = %q", config)
	}
}

func TestDetectHostWindowsTerminalPrefersPreviewSettings(t *testing.T) {
	localAppData := t.TempDir()
	previewSettings := filepath.Join(localAppData, "Packages", "Microsoft.WindowsTerminalPreview_8wekyb3d8bbwe", "LocalState", "settings.json")
	stableSettings := filepath.Join(localAppData, "Packages", "Microsoft.WindowsTerminal_8wekyb3d8bbwe", "LocalState", "settings.json")
	for _, path := range []string{previewSettings, stableSettings} {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	writeTestFile(t, previewSettings, `{"theme":"light"}`)
	writeTestFile(t, stableSettings, `{"theme":"dark"}`)

	configuration, err := detectHostWindowsTerminal(localAppData)
	if err != nil {
		t.Fatalf("detectHostWindowsTerminal: %v", err)
	}
	if configuration.Edition != windowsTerminalPreviewEdition || configuration.Theme != windowsTerminalLightTheme || configuration.WinGetPackageID != "Microsoft.WindowsTerminal.Preview" || configuration.SettingsPath != previewSettings {
		t.Fatalf("configuration = %#v", configuration)
	}
	if err := configuration.validate(); err != nil {
		t.Fatalf("validate: %v", err)
	}
}

func TestDetectHostWindowsTerminalUsesStableWhenPreviewIsAbsent(t *testing.T) {
	localAppData := t.TempDir()
	stableSettings := filepath.Join(localAppData, "Packages", "Microsoft.WindowsTerminal_8wekyb3d8bbwe", "LocalState", "settings.json")
	if err := os.MkdirAll(filepath.Dir(stableSettings), 0o700); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, stableSettings, `{"theme":"dark"}`)

	configuration, err := detectHostWindowsTerminal(localAppData)
	if err != nil {
		t.Fatalf("detectHostWindowsTerminal: %v", err)
	}
	if configuration.Edition != windowsTerminalStableEdition || configuration.Theme != windowsTerminalDarkTheme || configuration.WinGetPackageID != "Microsoft.WindowsTerminal" || configuration.SettingsPath != stableSettings {
		t.Fatalf("configuration = %#v", configuration)
	}
}

func TestDetectHostWindowsTerminalPrefersInstalledPreviewWithoutSettings(t *testing.T) {
	localAppData := t.TempDir()
	previewPackage := filepath.Join(localAppData, "Packages", "Microsoft.WindowsTerminalPreview_8wekyb3d8bbwe")
	if err := os.MkdirAll(previewPackage, 0o700); err != nil {
		t.Fatal(err)
	}
	stableSettings := filepath.Join(localAppData, "Packages", "Microsoft.WindowsTerminal_8wekyb3d8bbwe", "LocalState", "settings.json")
	if err := os.MkdirAll(filepath.Dir(stableSettings), 0o700); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, stableSettings, `{"theme":"light"}`)

	configuration, err := detectHostWindowsTerminal(localAppData)
	if err != nil {
		t.Fatalf("detectHostWindowsTerminal: %v", err)
	}
	if configuration.Edition != windowsTerminalPreviewEdition || configuration.Theme != windowsTerminalDarkTheme || configuration.SettingsPath != "" {
		t.Fatalf("configuration = %#v", configuration)
	}
}

func TestClassifyHostWindowsTerminalThemeSelectsExplicitAndCustomThemes(t *testing.T) {
	for _, test := range []struct {
		name     string
		settings string
		want     string
	}{
		{name: "default system fallback", settings: `{}`, want: windowsTerminalDarkTheme},
		{name: "explicit system fallback", settings: `{"theme":"system"}`, want: windowsTerminalDarkTheme},
		{name: "built in light", settings: `{"theme":"light"}`, want: windowsTerminalLightTheme},
		{name: "built in dark", settings: `{"theme":"dark"}`, want: windowsTerminalDarkTheme},
		{name: "custom light", settings: `{"theme":"fixture","themes":[{"name":"fixture","window":{"applicationTheme":"light"}}]}`, want: windowsTerminalLightTheme},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := classifyHostWindowsTerminalTheme([]byte(test.settings))
			if err != nil || got != test.want {
				t.Fatalf("theme = %q, err = %v", got, err)
			}
		})
	}
	for _, settings := range []string{
		`{"theme":{"light":"light","dark":"dark"}}`,
		`{"theme":"missing","themes":[]}`,
		`{"theme":"fixture","themes":[{"name":"fixture","window":{"applicationTheme":"system"}}]}`,
	} {
		if _, err := classifyHostWindowsTerminalTheme([]byte(settings)); err == nil {
			t.Fatalf("ambiguous theme unexpectedly succeeded: %s", settings)
		}
	}
	if preset, err := starshipPresetForWindowsTerminalTheme(windowsTerminalLightTheme); err != nil || preset != starshipCatppuccinLattePreset {
		t.Fatalf("light preset = %q, err = %v", preset, err)
	}
	if preset, err := starshipPresetForWindowsTerminalTheme(windowsTerminalDarkTheme); err != nil || preset != starshipPastelPowerlinePreset {
		t.Fatalf("dark preset = %q, err = %v", preset, err)
	}
}

func TestBuildDevelopmentConfigurationArchiveAllowsMissingGitConfiguration(t *testing.T) {
	root := t.TempDir()
	packagePlan := filepath.Join(root, wingetPackagePlanFileName)
	writeTestFile(t, packagePlan, `{}`)
	herdrConfig := filepath.Join(root, "herdr-config.toml")
	writeTestFile(t, herdrConfig, "[terminal]\ndefault_shell = \"nu\"\n")

	data, err := buildDevelopmentConfigurationArchive(context.Background(), hostConfigurationSources{
		GitConfig:          filepath.Join(root, "missing", ".gitconfig"),
		GitConfigDirectory: filepath.Join(root, "missing", "config", "git"),
		GitIgnore:          filepath.Join(root, "missing", ".gitignore_global"),
		GitAttributes:      filepath.Join(root, "missing", ".gitattributes"),
		HerdrConfig:        herdrConfig,
		PackagePlan:        packagePlan,
	}, []byte("Write-Output 'apply fixture'\n"))
	if err != nil {
		t.Fatalf("build archive without host Git configuration: %v", err)
	}
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range reader.File {
		if strings.HasPrefix(file.Name, "git/") {
			t.Fatalf("archive synthesized missing host Git configuration: %s", file.Name)
		}
	}
}

func TestExportGitHubCLIAuthenticationAllowsMissingHostCommand(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	payload, count, err := exportGitHubCLIAuthentication(t.Context(), t.TempDir())
	if err != nil || count != 0 {
		t.Fatalf("missing host GitHub CLI export count = %d, err = %v", count, err)
	}
	authentication, err := decodeGitHubCLIAuthentication(payload)
	if err != nil || len(authentication.Accounts) != 0 {
		t.Fatalf("empty host GitHub CLI authentication = %#v, err = %v", authentication, err)
	}
}

func TestDetectHostWindowsTerminalRejectsMissingInstallation(t *testing.T) {
	if _, err := detectHostWindowsTerminal(t.TempDir()); err == nil {
		t.Fatal("missing Windows Terminal installation unexpectedly succeeded")
	}
}

func TestDefaultOpenCodeDirectoriesHonorAbsoluteXDGRoots(t *testing.T) {
	root := t.TempDir()
	configurationRoot := filepath.Join(root, "configuration")
	dataRoot := filepath.Join(root, "data")
	t.Setenv("XDG_CONFIG_HOME", configurationRoot)
	t.Setenv("XDG_DATA_HOME", dataRoot)

	configuration, data, err := defaultOpenCodeDirectories(filepath.Join(root, "home"))
	if err != nil {
		t.Fatalf("defaultOpenCodeDirectories: %v", err)
	}
	if configuration != filepath.Join(configurationRoot, "opencode") || data != filepath.Join(dataRoot, "opencode") {
		t.Fatalf("configuration = %q, data = %q", configuration, data)
	}
}

func TestDefaultGitHubCLIConfigurationDirectoryUsesDocumentedPrecedence(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	appData := filepath.Join(root, "appdata")
	configured := filepath.Join(root, "configured")
	xdg := filepath.Join(root, "xdg")

	t.Setenv("GH_CONFIG_DIR", configured)
	t.Setenv("XDG_CONFIG_HOME", xdg)
	path, err := defaultGitHubCLIConfigurationDirectory(home, appData)
	if err != nil || path != configured {
		t.Fatalf("GH_CONFIG_DIR path = %q, err = %v", path, err)
	}
	t.Setenv("GH_CONFIG_DIR", "")
	path, err = defaultGitHubCLIConfigurationDirectory(home, appData)
	if err != nil || path != filepath.Join(xdg, "gh") {
		t.Fatalf("XDG path = %q, err = %v", path, err)
	}
	t.Setenv("XDG_CONFIG_HOME", "")
	path, err = defaultGitHubCLIConfigurationDirectory(home, appData)
	if err != nil || path != filepath.Join(appData, "GitHub CLI") {
		t.Fatalf("APPDATA path = %q, err = %v", path, err)
	}
	path, err = defaultGitHubCLIConfigurationDirectory(home, "")
	if err != nil || path != filepath.Join(home, ".config", "gh") {
		t.Fatalf("home path = %q, err = %v", path, err)
	}
	t.Setenv("GH_CONFIG_DIR", "relative")
	if _, err := defaultGitHubCLIConfigurationDirectory(home, appData); err == nil {
		t.Fatal("relative GH_CONFIG_DIR unexpectedly succeeded")
	}
}

func TestGitHubCLIAuthenticationContractIsStrictAndSecretSafe(t *testing.T) {
	payload := []byte(`{"schemaVersion":1,"accounts":[{"hostname":"github.com","login":"fixture","active":true,"gitProtocol":"https","token":"do-not-log-this-token"}]}`)
	authentication, err := decodeGitHubCLIAuthentication(payload)
	if err != nil || len(authentication.Accounts) != 1 {
		t.Fatalf("authentication count = %d, err = %v", len(authentication.Accounts), err)
	}
	for _, invalid := range [][]byte{
		[]byte(`{"schemaVersion":1,"accounts":[],"extra":true}`),
		[]byte(`{"schemaVersion":1,"accounts":[{"hostname":"github.com","login":"fixture","active":true,"gitProtocol":"https","token":"secret","extra":true}]}`),
		[]byte(`{"schemaVersion":1,"accounts":[{"hostname":"github.com","login":"fixture","active":false,"gitProtocol":"https","token":"secret"}]}`),
	} {
		_, decodeErr := decodeGitHubCLIAuthentication(invalid)
		if decodeErr == nil {
			t.Fatalf("invalid authentication unexpectedly succeeded")
		}
		if strings.Contains(decodeErr.Error(), "secret") || strings.Contains(decodeErr.Error(), "do-not-log") {
			t.Fatalf("authentication error exposed credential content: %v", decodeErr)
		}
	}
}

func TestGitHubCLICommandEnvironmentRemovesTokenOverrides(t *testing.T) {
	t.Setenv("GH_TOKEN", "secret-gh")
	t.Setenv("GITHUB_TOKEN", "secret-github")
	t.Setenv("GH_ENTERPRISE_TOKEN", "secret-enterprise")
	t.Setenv("GITHUB_ENTERPRISE_TOKEN", "secret-github-enterprise")
	t.Setenv("GH_CONFIG_DIR", "old")
	environment := githubCLICommandEnvironment(filepath.Join(t.TempDir(), "gh"))
	joined := strings.Join(environment, "\n")
	for _, secret := range []string{"secret-gh", "secret-github", "secret-enterprise", "secret-github-enterprise", "GH_CONFIG_DIR=old"} {
		if strings.Contains(joined, secret) {
			t.Fatalf("GitHub CLI environment retained %q", secret)
		}
	}
	if !strings.Contains(joined, "GH_PROMPT_DISABLED=1") || !strings.Contains(joined, "NO_COLOR=1") {
		t.Fatal("GitHub CLI environment is missing noninteractive controls")
	}
}

func TestGuestGitHubCLIAuthenticationAvoidsCredentialUIAndOwnsGitHTTPS(t *testing.T) {
	script := string(configurationSyncScript)
	for _, required := range []string{
		"'--with-token', '--insecure-storage', '--skip-ssh-key'",
		"'auth', 'setup-git', '--hostname'",
		"GitHub CLI Git credential-helper setup",
		`$credentialHelperKey = "credential.https://$hostname.helper"`,
		"$credentialHelpers.Count -ne 2",
		"[string]$credentialHelpers[0] -cne ''",
		"GitHub CLI authenticated-account import requires the Git.Git package",
		`$expectedCredentialHelper = "!'" + $script:GitHubCLICommand + "' auth git-credential"`,
		"[string]::Equals($credentialHelper, $expectedCredentialHelper, [StringComparison]::Ordinal)",
		"$matches[0].tokenSource",
		"Join-Path $githubCLIDestination 'hosts.yml'",
	} {
		if !strings.Contains(script, required) {
			t.Fatalf("guest GitHub CLI authentication is missing %q", required)
		}
	}
	for _, forbidden := range []string{
		"$credentialHelper.StartsWith('!')",
		"$credentialHelper.IndexOf('gh.exe'",
		"$credentialHelper.EndsWith(' auth git-credential'",
	} {
		if strings.Contains(script, forbidden) {
			t.Fatalf("guest GitHub CLI authentication retains broad helper check %q", forbidden)
		}
	}
	login := strings.Index(script, "'--with-token', '--insecure-storage', '--skip-ssh-key'")
	setupGit := strings.Index(script, "GitHub CLI Git credential-helper setup")
	status := strings.Index(script, "GitHub CLI authentication verification")
	if login < 0 || setupGit <= login || status <= setupGit {
		t.Fatalf("GitHub CLI login/setup/status order = %d/%d/%d", login, setupGit, status)
	}
}

func TestCanonicalGitHubCLIAccountLoginHandlesRenamedAccount(t *testing.T) {
	account := githubCLIAccount{
		Hostname: "github.com", Login: "legacy-user", Active: true,
		GitProtocol: "https", Token: "fixture-token",
	}
	canonical, err := withCanonicalGitHubCLIAccountLogin(account, []byte("current-user\r\n"))
	if err != nil {
		t.Fatalf("withCanonicalGitHubCLIAccountLogin: %v", err)
	}
	if canonical.Login != "current-user" || canonical.Hostname != account.Hostname ||
		canonical.Active != account.Active || canonical.GitProtocol != account.GitProtocol || canonical.Token != account.Token {
		t.Fatalf("canonical account = %#v", canonical)
	}
	if _, err := withCanonicalGitHubCLIAccountLogin(account, []byte("invalid\nlogin")); err == nil {
		t.Fatal("invalid canonical login unexpectedly succeeded")
	}
}

func TestNativeGitHubCLIAuthenticationExport(t *testing.T) {
	if os.Getenv("HERDR_SANDBOX_NATIVE_GITHUB_CLI") != "1" {
		t.Skip("set HERDR_SANDBOX_NATIVE_GITHUB_CLI=1 for host keyring verification")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	configuration, err := defaultGitHubCLIConfigurationDirectory(home, strings.TrimSpace(os.Getenv("APPDATA")))
	if err != nil {
		t.Fatal(err)
	}
	payload, count, err := exportGitHubCLIAuthentication(context.Background(), configuration)
	if err != nil {
		t.Fatalf("exportGitHubCLIAuthentication: %v", err)
	}
	authentication, err := decodeGitHubCLIAuthentication(payload)
	if err != nil {
		t.Fatalf("decodeGitHubCLIAuthentication: %v", err)
	}
	if count < 1 || len(authentication.Accounts) != count {
		t.Fatalf("exported GitHub CLI account count = %d", count)
	}
	for _, account := range authentication.Accounts {
		if account.Token == "" {
			t.Fatal("exported GitHub CLI account has no token")
		}
	}
}

func TestNativeDevelopmentConfigurationSync(t *testing.T) {
	runDirectory := strings.TrimSpace(os.Getenv("HERDR_SANDBOX_NATIVE_RUN_DIRECTORY"))
	if runDirectory == "" {
		t.Skip("set HERDR_SANDBOX_NATIVE_RUN_DIRECTORY for live guest configuration verification")
	}
	if !filepath.IsAbs(runDirectory) {
		t.Fatalf("native run directory is not absolute: %q", runDirectory)
	}
	terminal, err := detectHostWindowsTerminalConfiguration()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	connection := Connection{
		RunDirectory:  runDirectory,
		SSHConfigPath: filepath.Join(runDirectory, ".ssh", "config"),
		SSHTarget:     sshTargetName,
	}
	packagePlanPath := filepath.Join(runDirectory, "input", "provisioning", wingetPackagePlanFileName)
	packagePlanData, err := os.ReadFile(packagePlanPath)
	if err != nil {
		t.Fatalf("read native run package plan: %v", err)
	}
	var packages wingetPackagePlan
	decoder := json.NewDecoder(bytes.NewReader(packagePlanData))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&packages); err != nil {
		t.Fatalf("decode native run package plan: %v", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		t.Fatal("decode native run package plan: trailing JSON data")
	}
	if err := packages.validate(terminal); err != nil {
		t.Fatalf("validate native run package plan: %v", err)
	}
	if err := syncDevelopmentConfiguration(ctx, connection, terminal, packages, defaultCodingAgentSyncConfiguration(), false, filepath.Join(runDirectory, "input", "provisioning")); err != nil {
		t.Fatalf("syncDevelopmentConfiguration: %v", err)
	}
}

func TestDecodeDevelopmentConfigurationSyncResultIsStrict(t *testing.T) {
	valid := []byte(`{"schemaVersion":7,"archiveSha256":"abc","copiedFiles":4,"openCodePermissionVerified":true,"windowsTerminalEdition":"preview","starshipPreset":"catppuccin-powerline-latte","starshipConfigured":true,"githubAuthenticatedAccounts":1,"githubAuthenticationVerified":true,"herdrConfigurationReloaded":true,"tradingViewAuthenticatedCookies":1,"tradingViewAuthenticationVerified":true}`)
	result, err := decodeDevelopmentConfigurationSyncResult(valid)
	if err != nil || result.CopiedFiles != 4 || result.TradingViewAuthenticatedCookies != 1 || !result.TradingViewAuthenticationVerified {
		t.Fatalf("result = %#v, err = %v", result, err)
	}
	for _, invalid := range [][]byte{
		[]byte(`{"schemaVersion":7,"archiveSha256":"abc","copiedFiles":4,"openCodePermissionVerified":true,"windowsTerminalEdition":"preview","starshipPreset":"catppuccin-powerline-latte","starshipConfigured":true,"githubAuthenticatedAccounts":1,"githubAuthenticationVerified":true,"herdrConfigurationReloaded":true,"tradingViewAuthenticatedCookies":1,"tradingViewAuthenticationVerified":true,"extra":true}`),
		[]byte(`{"schemaVersion":7,"schemaVersion":7,"archiveSha256":"abc","copiedFiles":4,"openCodePermissionVerified":true,"windowsTerminalEdition":"preview","starshipPreset":"catppuccin-powerline-latte","starshipConfigured":true,"githubAuthenticatedAccounts":1,"githubAuthenticationVerified":true,"herdrConfigurationReloaded":true,"tradingViewAuthenticatedCookies":1,"tradingViewAuthenticationVerified":true}`),
		[]byte(`{"schemaVersion":7,"archiveSha256":"abc","copiedFiles":4,"openCodePermissionVerified":true,"windowsTerminalEdition":"preview","starshipPreset":"catppuccin-powerline-latte","starshipConfigured":true,"githubAuthenticatedAccounts":1,"githubAuthenticationVerified":true,"herdrConfigurationReloaded":true,"tradingViewAuthenticatedCookies":1}`),
		[]byte(`{"schemaVersion":7,"archiveSha256":"abc","copiedFiles":4,"openCodePermissionVerified":true,"windowsTerminalEdition":"preview","starshipPreset":"catppuccin-powerline-latte","starshipConfigured":true,"githubAuthenticatedAccounts":1,"githubAuthenticationVerified":true,"herdrConfigurationReloaded":true,"tradingViewAuthenticatedCookies":1,"tradingViewAuthenticationVerified":true} {}`),
	} {
		if _, err := decodeDevelopmentConfigurationSyncResult(invalid); err == nil {
			t.Fatalf("invalid result unexpectedly succeeded: %s", invalid)
		}
	}
}

func TestEncodePowerShellUsesUTF16LE(t *testing.T) {
	script := "Write-Output 'Herdr Sandbox ✓'"
	encoded, err := base64.StdEncoding.DecodeString(encodePowerShell(script))
	if err != nil {
		t.Fatal(err)
	}
	if len(encoded)%2 != 0 {
		t.Fatalf("encoded byte length = %d", len(encoded))
	}
	codeUnits := make([]uint16, len(encoded)/2)
	for index := range codeUnits {
		codeUnits[index] = uint16(encoded[index*2]) | uint16(encoded[index*2+1])<<8
	}
	if decoded := string(utf16.Decode(codeUnits)); decoded != script {
		t.Fatalf("decoded script = %q", decoded)
	}
}

func TestDevelopmentConfigurationLauncherReadsExactLengthWithoutWaitingForEOF(t *testing.T) {
	script := buildDevelopmentConfigurationLauncher(strings.Repeat("a", 64), 12345)
	for _, required := range []string{
		"$expectedArchiveLength = [long]12345",
		"while ($remaining -gt 0)",
		"$inputStream.Read($buffer, 0, $requested)",
		"archive ended with $remaining bytes missing",
		"[config-sync] invoke-apply-script",
		"function Remove-GuestArchiveStaging",
		`C:\HerdrSandbox\staging`,
		"configuration-aaaaaaaaaaaaaaaa",
		"Assert-GuestArchiveTree",
		"staging tree contains a reparse point",
	} {
		if !strings.Contains(script, required) {
			t.Fatalf("launcher is missing %q", required)
		}
	}
	if strings.Contains(script, ".CopyTo(") {
		t.Fatal("launcher waits for SSH standard-input EOF")
	}
	if strings.Contains(script, "$env:TEMP") {
		t.Fatal("launcher stages personal configuration outside the canonical guest root")
	}
	if strings.Contains(script, "Remove-Item -LiteralPath $archive -Force -ErrorAction SilentlyContinue") ||
		strings.Contains(script, "Remove-Item -LiteralPath $expanded -Recurse -Force -ErrorAction SilentlyContinue") {
		t.Fatal("launcher silently ignores credential-bearing staging cleanup")
	}
	if runtime.GOOS == "windows" {
		parserScript := fmt.Sprintf(`$tokens = $null
$errors = $null
[void][Management.Automation.Language.Parser]::ParseInput('%s', [ref]$tokens, [ref]$errors)
if ($errors.Count -ne 0) { throw ($errors | ForEach-Object { $_.ToString() } | Out-String) }
`, strings.ReplaceAll(script, "'", "''"))
		command := hiddenCommand(mustWindowsPowerShellPath(t), "-NoLogo", "-NoProfile", "-NonInteractive", "-EncodedCommand", encodePowerShell(parserScript))
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("launcher PowerShell parse: %v: %s", err, output)
		}
	}
}

func TestDevelopmentConfigurationRemoteScriptParsesInWindowsPowerShell51(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows PowerShell 5.1 regression")
	}
	remoteScript := configurationSyncScript
	for _, required := range [][]byte{
		[]byte("herdr-sandbox\\workspaces.json"),
		[]byte("--replace-all' 'safe.directory"),
		[]byte("Git safe-directory verification failed"),
		[]byte("Get-Command -Name 'herdr.exe' -CommandType Application"),
		[]byte("starship\\preset.txt"),
		[]byte("catppuccin_latte"),
		[]byte("starshipConfigured = $starshipConfigured"),
		[]byte("herdr-sandbox\\coding-agent-sync.json"),
		[]byte("Assert-ConfigurationDestinationPath"),
		[]byte("[config-sync] apply-claude-code"),
		[]byte("[config-sync] apply-codex"),
		[]byte("[config-sync] apply-github-copilot"),
		[]byte("[config-sync] apply-pi"),
		[]byte("[AllowEmptyCollection()][object[]]$Paths"),
		[]byte("[config-sync] apply-tradingview-authentication"),
		[]byte("TradingView Desktop is running in the guest"),
		[]byte("HerdrSandbox.TradingViewCookieSync"),
		[]byte("tradingViewAuthenticationVerified = $tradingViewAuthenticationVerified"),
	} {
		if !bytes.Contains(remoteScript, required) {
			t.Fatalf("remote configuration script is missing %q", required)
		}
	}
	if bytes.Contains(remoteScript, []byte("Remove-Item -LiteralPath $archive")) || bytes.Contains(remoteScript, []byte("Remove-Item -LiteralPath $expanded")) {
		t.Fatal("remote apply script duplicates the launcher's staging cleanup owner")
	}
	remotePath := filepath.Join(t.TempDir(), "configuration-sync.ps1")
	if err := os.WriteFile(remotePath, remoteScript, 0o600); err != nil {
		t.Fatal(err)
	}
	parserScript := fmt.Sprintf(`$path = '%s'
$tokens = $null
$errors = $null
[void][Management.Automation.Language.Parser]::ParseFile($path, [ref]$tokens, [ref]$errors)
if ($errors.Count -ne 0) { throw ($errors | ForEach-Object { $_.ToString() } | Out-String) }
`, strings.ReplaceAll(remotePath, "'", "''"))
	command := hiddenCommand(mustWindowsPowerShellPath(t), "-NoLogo", "-NoProfile", "-NonInteractive", "-EncodedCommand", encodePowerShell(parserScript))
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("remote configuration PowerShell parse: %v: %s", err, output)
	}
}

func writeTestFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}
