package sandbox

import (
	"context"
	"runtime"
	"strings"
	"testing"
)

func TestInspectProjectProvisioningPlanUsesDirectCallsWithoutExecutingScripts(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows PowerShell 5.1 AST regression")
	}
	runDirectory := t.TempDir()
	projectsDirectory := t.TempDir()
	userScript := runDirectory + `\user.ps1`
	writeTestFile(t, userScript, userProvisioningContract+`
$dynamic = 'Install-ZigStack'
& $dynamic
Install-RustMSVCStack -ProjectDirectory 'C:\Workspaces\global'
`)
	writeTestFile(t, projectsDirectory+`\alpha.ps1`, `$dynamic = 'Install-RustMSVCStack'
& $dynamic
& 'Install-NodeStack'
Set-Alias selectedStack Install-RustMSVCStack
selectedStack
Invoke-Expression 'Install-RustMSVCStack'
. '.\hidden.ps1'
Install-GoStack
Install-GoStack -Version '1.26.5'
Install-DotNetStack
throw 'the AST adapter must not execute project code'
`)
	writeTestFile(t, projectsDirectory+`\herdr.ps1`, `Install-HerdrStack -ProjectDirectory 'C:\Workspaces\herdr'
`)
	workspaces := []workspacePlan{
		{Name: "alpha", ProvisioningPath: projectsDirectory + `\alpha.ps1`},
		{Name: "herdr", ProvisioningPath: projectsDirectory + `\herdr.ps1`},
	}
	got, userStacks, err := inspectProjectProvisioningPlan(context.Background(), runDirectory, userScript, projectsDirectory, workspaces)
	if err != nil {
		t.Fatalf("inspectProjectProvisioningPlan: %v", err)
	}
	if strings.Join(projectStackStrings(got[0].Stacks), "|") != "dotnet|go" {
		t.Fatalf("alpha stacks = %v", got[0].Stacks)
	}
	if strings.Join(projectStackStrings(got[1].Stacks), "|") != "bun|cargo-nextest|git-sh|just|python|rust-msvc|zig" {
		t.Fatalf("herdr stacks = %v", got[1].Stacks)
	}
	if strings.Join(projectStackStrings(userStacks), "|") != "rust-msvc" {
		t.Fatalf("user stacks = %v", userStacks)
	}
}

func TestValidateGitShellPackageRequirementRequiresRetainedGit(t *testing.T) {
	terminal := testStableWindowsTerminalConfiguration()
	withoutGit, err := resolveWingetPackagePlan(wingetPackageConfiguration{Remove: []string{packageGit}}, terminal)
	if err != nil {
		t.Fatal(err)
	}
	workspaces := []workspacePlan{{Name: "herdr", Stacks: []projectStack{stackGitSH}}}
	if err := validateGitShellPackageRequirement(workspaces, nil, withoutGit); err == nil || !strings.Contains(err.Error(), packageGit) {
		t.Fatalf("missing Git shell requirement error = %v", err)
	}
	defaults, err := resolveWingetPackagePlan(defaultWingetPackageConfiguration(), terminal)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateGitShellPackageRequirement(workspaces, nil, defaults); err != nil {
		t.Fatalf("retained Git package rejected: %v", err)
	}
	if err := validateGitShellPackageRequirement([]workspacePlan{{Name: "plain"}}, nil, withoutGit); err != nil {
		t.Fatalf("unrelated project rejected without Git: %v", err)
	}
}

func TestInspectProjectProvisioningPlanRejectsParseErrors(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows PowerShell 5.1 AST regression")
	}
	runDirectory := t.TempDir()
	projectsDirectory := t.TempDir()
	userScript := runDirectory + `\user.ps1`
	writeTestFile(t, userScript, userProvisioningContract+"\n")
	writeTestFile(t, projectsDirectory+`\broken-one.ps1`, "function Broken {\n")
	writeTestFile(t, projectsDirectory+`\broken-two.ps1`, "function AlsoBroken {\n")
	workspaces := []workspacePlan{
		{Name: "broken-one", ProvisioningPath: projectsDirectory + `\broken-one.ps1`},
		{Name: "broken-two", ProvisioningPath: projectsDirectory + `\broken-two.ps1`},
	}
	_, _, err := inspectProjectProvisioningPlan(context.Background(), runDirectory, userScript, projectsDirectory, workspaces)
	if err == nil || !strings.Contains(err.Error(), "parse failed") || !strings.Contains(err.Error(), "broken-one.ps1") || !strings.Contains(err.Error(), "broken-two.ps1") {
		t.Fatalf("parse error = %v", err)
	}
}

func TestInspectProjectProvisioningPlanRejectsUserParamBlock(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows PowerShell 5.1 AST regression")
	}
	runDirectory := t.TempDir()
	projectsDirectory := t.TempDir()
	userScript := runDirectory + `\user.ps1`
	writeTestFile(t, userScript, userProvisioningContract+"\nparam([string]$Unexpected)\n")
	writeTestFile(t, projectsDirectory+`\alpha.ps1`, "Write-Output 'project'\n")
	_, _, err := inspectProjectProvisioningPlan(context.Background(), runDirectory, userScript, projectsDirectory, []workspacePlan{{Name: "alpha", ProvisioningPath: projectsDirectory + `\alpha.ps1`}})
	if err == nil || !strings.Contains(err.Error(), "must not declare a script-level param block") {
		t.Fatalf("user param block error = %v", err)
	}
}

func TestDecodeProjectProvisioningPlanIsStrict(t *testing.T) {
	workspaces := []workspacePlan{{Name: "alpha", ProvisioningPath: `C:\profiles\alpha.ps1`}}
	valid := []byte(`{"schemaVersion":2,"userStacks":["rust-msvc"],"projects":[{"name":"alpha","stacks":["go"]}]}`)
	got, userStacks, err := decodeProjectProvisioningPlan(valid, workspaces)
	if err != nil || len(got) != 1 || len(got[0].Stacks) != 1 || got[0].Stacks[0] != stackGo {
		t.Fatalf("valid plan = %#v, %v", got, err)
	}
	if len(userStacks) != 1 || userStacks[0] != stackRustMSVC {
		t.Fatalf("valid user stacks = %#v", userStacks)
	}
	for _, invalid := range [][]byte{
		[]byte(`{"schemaVersion":1,"userStacks":[],"projects":[{"name":"alpha","stacks":[]}]}`),
		[]byte(`{"schemaVersion":2,"projects":[{"name":"alpha","stacks":[]}]}`),
		[]byte(`{"schemaVersion":2,"userStacks":[],"projects":[{"name":"other","stacks":[]}]}`),
		[]byte(`{"schemaVersion":2,"userStacks":["unknown"],"projects":[{"name":"alpha","stacks":[]}]}`),
		[]byte(`{"schemaVersion":2,"userStacks":[],"projects":[{"name":"alpha","stacks":["unknown"]}]}`),
		[]byte(`{"schemaVersion":2,"userStacks":[],"projects":[{"name":"alpha","stacks":["go","go"]}]}`),
		[]byte(`{"schemaVersion":2,"userStacks":[],"projects":[{"name":"alpha","stacks":[],"extra":true}]}`),
		[]byte(`{"schemaVersion":2,"userStacks":[],"projects":[{"name":"alpha","stacks":[]}]} {}`),
	} {
		if _, _, err := decodeProjectProvisioningPlan(invalid, workspaces); err == nil {
			t.Fatalf("invalid plan unexpectedly decoded: %s", invalid)
		}
	}
}

func TestDecodeProjectProvisioningPlanAllowsWorkspaceWithoutProfile(t *testing.T) {
	workspaces := []workspacePlan{
		{Name: "profiled", ProvisioningPath: `C:\profiles\profiled.ps1`},
		{Name: "plain"},
	}
	data := []byte(`{"schemaVersion":2,"userStacks":[],"projects":[{"name":"profiled","stacks":["go"]}]}`)
	got, _, err := decodeProjectProvisioningPlan(data, workspaces)
	if err != nil {
		t.Fatalf("decode optional project provisioning plan: %v", err)
	}
	if len(got) != 2 || len(got[0].Stacks) != 1 || got[0].Stacks[0] != stackGo || len(got[1].Stacks) != 0 {
		t.Fatalf("optional project provisioning plan = %#v", got)
	}
}

func projectStackStrings(stacks []projectStack) []string {
	values := make([]string, len(stacks))
	for index, stack := range stacks {
		values[index] = string(stack)
	}
	return values
}
