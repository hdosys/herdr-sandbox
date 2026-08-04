package sandbox

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInitializeProjectWritesDeterministicDirectStackCalls(t *testing.T) {
	project := t.TempDir()
	result, err := InitializeProject(project, []string{"zig", "dotnet", "rust", "go", "node", "playwright-cli", "python", "tradingview"})
	if err != nil {
		t.Fatal(err)
	}
	wantLabels := "dotnet|go|node|playwright-cli|python|rust|tradingview|zig"
	if strings.Join(result.Stacks, "|") != wantLabels {
		t.Fatalf("stacks = %v, want %s", result.Stacks, wantLabels)
	}
	data, err := os.ReadFile(result.Path)
	if err != nil {
		t.Fatal(err)
	}
	wantCalls := []string{
		"Install-DotNetStack",
		"Install-GoStack -ProjectDirectory $ProjectDirectory",
		"Install-NodeStack",
		"Install-PlaywrightCLIStack",
		"Install-PythonStack",
		"Install-RustMSVCStack -ProjectDirectory $ProjectDirectory",
		"Install-TradingViewStack",
		"Install-ZigStack",
	}
	text := string(data)
	last := -1
	for _, call := range wantCalls {
		index := strings.Index(text, call)
		if index <= last {
			t.Fatalf("profile does not contain ordered direct call %q: %s", call, text)
		}
		last = index
	}
	for _, forbidden := range []string{"Invoke-Expression", "Set-Alias", "Install-DotNetFramework", "MSBuild"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("profile contains forbidden legacy or dynamic path %q", forbidden)
		}
	}
}

func TestInitializeProjectWritesHerdrVirtualStack(t *testing.T) {
	project := t.TempDir()
	result, err := InitializeProject(project, []string{"herdr"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(result.Stacks, "|") != "herdr" {
		t.Fatalf("stacks = %v", result.Stacks)
	}
	data, err := os.ReadFile(result.Path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.Contains(text, "Install-HerdrStack -ProjectDirectory $ProjectDirectory") {
		t.Fatalf("Herdr virtual profile = %s", text)
	}
	for _, expanded := range []string{"Install-PythonStack", "Install-ZigStack", "Install-RustMSVCStack", "Install-CargoNextest", "Install-Just"} {
		if strings.Contains(text, expanded) {
			t.Fatalf("init duplicated virtual stack implementation %q: %s", expanded, text)
		}
	}
}

func TestInitializeProjectRejectsSelectionsBeforeFilesystemMutation(t *testing.T) {
	for _, requested := range [][]string{nil, {"unknown"}, {"go", "GO"}, {"herdr", "rust"}, {"herdr", "python"}, {"herdr", "zig"}} {
		project := t.TempDir()
		if _, err := InitializeProject(project, requested); err == nil {
			t.Fatalf("selection %v unexpectedly succeeded", requested)
		}
		if _, err := os.Lstat(filepath.Join(project, projectConfigurationName)); !os.IsNotExist(err) {
			t.Fatalf("selection %v mutated project: %v", requested, err)
		}
	}
}

func TestInitializeProjectNeverOverwritesExistingProfileOrSiblingState(t *testing.T) {
	project := t.TempDir()
	configuration := filepath.Join(project, projectConfigurationName)
	if err := os.Mkdir(configuration, 0o700); err != nil {
		t.Fatal(err)
	}
	profile := filepath.Join(configuration, projectProvisioningName)
	sibling := filepath.Join(configuration, "keep.txt")
	if err := os.WriteFile(profile, []byte("existing profile"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sibling, []byte("sibling"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := InitializeProject(project, []string{"dotnet"}); err == nil || !strings.Contains(err.Error(), "was not changed") {
		t.Fatalf("overwrite error = %v", err)
	}
	for path, want := range map[string]string{profile: "existing profile", sibling: "sibling"} {
		data, err := os.ReadFile(path)
		if err != nil || string(data) != want {
			t.Fatalf("preserved file %s = %q, %v", path, data, err)
		}
	}
}

func TestInitializeProjectRefusesNestedProfileUnderExistingOwner(t *testing.T) {
	project := t.TempDir()
	configuration := filepath.Join(project, projectConfigurationName)
	nested := filepath.Join(project, "src", "nested")
	if err := os.MkdirAll(configuration, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(nested, 0o700); err != nil {
		t.Fatal(err)
	}
	profile := filepath.Join(configuration, projectProvisioningName)
	if err := os.WriteFile(profile, []byte("existing profile"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := InitializeProject(nested, []string{"go"}); err == nil ||
		!strings.Contains(err.Error(), "already exists and was not changed") {
		t.Fatalf("nested ownership error = %v", err)
	}
	if _, err := os.Lstat(filepath.Join(nested, projectConfigurationName)); !os.IsNotExist(err) {
		t.Fatalf("nested configuration was created: %v", err)
	}
	if data, err := os.ReadFile(profile); err != nil || string(data) != "existing profile" {
		t.Fatalf("ancestor profile changed: %q, %v", data, err)
	}
}

func TestProjectPlanRecognizesModernDotNetDirectCall(t *testing.T) {
	workspaces := []workspacePlan{{Name: "project", ProvisioningPath: `C:\profiles\project.ps1`}}
	data := []byte(`{"schemaVersion":2,"userStacks":[],"projects":[{"name":"project","stacks":["dotnet"]}]}`)
	result, _, err := decodeProjectProvisioningPlan(data, workspaces)
	if err != nil || len(result) != 1 || len(result[0].Stacks) != 1 || result[0].Stacks[0] != stackDotNet {
		t.Fatalf(".NET plan = %#v, %v", result, err)
	}
}
