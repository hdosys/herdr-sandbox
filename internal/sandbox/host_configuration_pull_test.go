package sandbox

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHostConfigurationGitRootsUseOnlyExplicitTransferredRoots(t *testing.T) {
	sources := hostConfigurationSources{
		HerdrConfig:              filepath.Join(`C:\config`, "herdr", "config.toml"),
		GitConfig:                filepath.Join(`C:\home`, ".gitconfig"),
		GitConfigDirectory:       filepath.Join(`C:\home`, ".config", "git"),
		GitHubCLIConfiguration:   filepath.Join(`C:\config`, "gh"),
		WindowsTerminalSettings:  filepath.Join(`C:\terminal`, "settings.json"),
		WindowsTerminalFragments: filepath.Join(`C:\terminal`, "Fragments"),
		CodingAgents: codingAgentConfigurationSources{
			OpenCodeDirectory:     filepath.Join(`C:\config`, "opencode"),
			SharedSkillsDirectory: filepath.Join(`C:\home`, ".agents", "skills"),
		},
	}
	roots := hostConfigurationGitRoots(sources)
	got := map[string]string{}
	for _, root := range roots {
		if root.Directory != "" && root.Directory != "." {
			got[root.Name] = filepath.Clean(root.Directory)
		}
	}
	want := map[string]string{
		"Herdr configuration":        filepath.Join(`C:\config`, "herdr"),
		"OpenCode configuration":     filepath.Join(`C:\config`, "opencode"),
		"shared agent skills":        filepath.Join(`C:\home`, ".agents", "skills"),
		"Git configuration":          filepath.Join(`C:\home`, ".config", "git"),
		"GitHub CLI configuration":   filepath.Join(`C:\config`, "gh"),
		"Windows Terminal settings":  `C:\terminal`,
		"Windows Terminal fragments": filepath.Join(`C:\terminal`, "Fragments"),
	}
	for name, directory := range want {
		if got[name] != directory {
			t.Fatalf("%s root = %q, want %q; all roots = %#v", name, got[name], directory, got)
		}
	}
	if _, found := got["global Git config file"]; found {
		t.Fatalf("single-file parent inference entered roots: %#v", got)
	}
}

func TestPullHostConfigurationGitRootsPullsAndReportsExplicitRoots(t *testing.T) {
	root := t.TempDir()
	remote := filepath.Join(root, "remote.git")
	runAgentGitTest(t, root, "init", "--bare", "--initial-branch=main", remote)
	seed := filepath.Join(root, "seed")
	initializeAgentGitRepositoryAtRemote(t, seed, remote, []string{"config.json"})
	host := filepath.Join(root, "host")
	runAgentGitTest(t, root, "clone", remote, host)
	writeTestFile(t, filepath.Join(seed, "config.json"), `{"version":2}`)
	runAgentGitTest(t, seed, "add", "--", "config.json")
	runAgentGitTest(t, seed, "-c", "core.hooksPath="+os.DevNull, "commit", "-m", "upstream")
	runAgentGitTest(t, seed, "push", "origin", "main")
	notRepository := filepath.Join(root, "plain")
	if err := os.MkdirAll(notRepository, 0o700); err != nil {
		t.Fatal(err)
	}

	result, err := pullHostConfigurationGitRoots(context.Background(), []hostConfigurationGitRoot{
		{Name: "OpenCode configuration", Directory: host},
		{Name: "OpenCode duplicate", Directory: host},
		{Name: "Herdr configuration", Directory: notRepository},
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(result.Pulled, "|") != "OpenCode configuration" {
		t.Fatalf("pulled = %#v", result.Pulled)
	}
	if strings.Join(result.Skipped, "|") != "Herdr configuration: not a Git repository" {
		t.Fatalf("skipped = %#v", result.Skipped)
	}
	if got, err := os.ReadFile(filepath.Join(host, "config.json")); err != nil || string(got) != `{"version":2}` {
		t.Fatalf("pulled config = %q, err = %v", got, err)
	}
}

func TestPullHostConfigurationGitRootsDeduplicatesPhysicalAliases(t *testing.T) {
	root := t.TempDir()
	repository := filepath.Join(root, "repository")
	if err := os.MkdirAll(repository, 0o700); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(repository, "config.json"), `{}`)
	runAgentGitTest(t, repository, "init", "--initial-branch=main")
	configureAgentGitTestIdentity(t, repository)
	runAgentGitTest(t, repository, "add", "--", "config.json")
	runAgentGitTest(t, repository, "-c", "core.hooksPath="+os.DevNull, "commit", "-m", "fixture")
	alias := filepath.Join(root, "alias")
	createTestDirectoryLink(t, alias, repository)

	result, err := pullHostConfigurationGitRoots(context.Background(), []hostConfigurationGitRoot{
		{Name: "first", Directory: repository},
		{Name: "alias", Directory: alias},
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(result.Skipped, "|") != "first: no remotes" || len(result.Pulled) != 0 {
		t.Fatalf("aliased repository was not deduplicated with first label: %#v", result)
	}
}

func TestPullHostConfigurationOnFlagsCanDisableAutomaticOwners(t *testing.T) {
	configurationRoot := t.TempDir()
	appData := filepath.Join(configurationRoot, "appdata")
	t.Setenv("APPDATA", appData)
	t.Setenv("LOCALAPPDATA", filepath.Join(configurationRoot, "local"))
	configDirectory := filepath.Join(appData, applicationName)
	if err := os.MkdirAll(configDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(configDirectory, globalConfigurationName), `{
  "configurationSync": {
    "pullHostGitRepositoriesOnUp": false,
    "pullHostGitRepositoriesOnDown": false
  }
}`)
	for name, pull := range map[string]func(context.Context) (HostConfigurationPullResult, error){
		"up":   PullHostConfigurationOnUp,
		"down": PullHostConfigurationOnDown,
	} {
		t.Run(name, func(t *testing.T) {
			result, err := pull(context.Background())
			if err != nil || len(result.Pulled) != 0 || len(result.Skipped) != 0 {
				t.Fatalf("result = %#v, err = %v", result, err)
			}
		})
	}
}

func TestPullHostConfigurationOnUpReloadsPulledFlagBeforeRemainingRoots(t *testing.T) {
	root := t.TempDir()
	appData := filepath.Join(root, "appdata")
	localAppData := filepath.Join(root, "local")
	t.Setenv("APPDATA", appData)
	t.Setenv("LOCALAPPDATA", localAppData)
	remote := filepath.Join(root, "remote.git")
	runAgentGitTest(t, root, "init", "--bare", "--initial-branch=main", remote)
	seed := filepath.Join(root, "seed")
	if err := os.MkdirAll(seed, 0o700); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(seed, globalConfigurationName), `{"configurationSync":{"pullHostGitRepositoriesOnUp":true}}`)
	initializeAgentGitRepositoryAtRemote(t, seed, remote, []string{globalConfigurationName})
	configurationRoot := filepath.Join(appData, applicationName)
	if err := os.MkdirAll(appData, 0o700); err != nil {
		t.Fatal(err)
	}
	runAgentGitTest(t, root, "clone", remote, configurationRoot)
	writeTestFile(t, filepath.Join(seed, globalConfigurationName), `{"configurationSync":{"pullHostGitRepositoriesOnUp":false}}`)
	runAgentGitTest(t, seed, "add", "--", globalConfigurationName)
	runAgentGitTest(t, seed, "-c", "core.hooksPath="+os.DevNull, "commit", "-m", "disable automatic up pull")
	runAgentGitTest(t, seed, "push", "origin", "main")

	result, err := PullHostConfigurationOnUp(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(result.Pulled, "|") != "Herdr Sandbox configuration" || len(result.Skipped) != 0 {
		t.Fatalf("result after pulled flag disable = %#v", result)
	}
	configuration, err := loadGlobalConfiguration(filepath.Join(configurationRoot, globalConfigurationName))
	if err != nil {
		t.Fatal(err)
	}
	if configuration.ConfigurationSync.PullHostGitRepositoriesOnUp {
		t.Fatalf("pulled configuration did not disable remaining up pulls: %#v", configuration.ConfigurationSync)
	}
}
