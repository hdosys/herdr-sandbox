package sandbox

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func normalizedProjectPlanError(err error) string {
	if err == nil {
		return ""
	}
	return strings.Join(strings.Fields(err.Error()), " ")
}

func TestInspectProjectProvisioningPlanUsesDirectCallsWithoutExecutingScripts(t *testing.T) {
	requireExternalBoundaryTest(t, "Windows PowerShell project-plan inspection")
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
Install-Uv
Install-AndroidStack
Install-AudioStack
Install-CppStack
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
Install-AndroidStack
Install-CppStack
Install-DotNetStack
Install-HandyStack -ProjectDirectory $ProjectDirectory
Install-HyperFramesStack
Install-JavaStack
Install-NSISStack
Install-NushellStack
Install-PlaywrightCLIStack
Install-PythonAIStack
Install-TradingViewStack
throw 'the AST adapter must not execute project code'
`)
	writeTestFile(t, projectsDirectory+`\herdr.ps1`, `Install-HerdrStack -ProjectDirectory $ProjectDirectory
`)
	alphaDirectory := t.TempDir()
	workspaces := []workspacePlan{
		{Name: "alpha", HostDirectory: alphaDirectory, ProvisioningPath: projectsDirectory + `\alpha.ps1`},
		{Name: "herdr", ProvisioningPath: projectsDirectory + `\herdr.ps1`},
	}
	inspection, err := inspectProjectProvisioningPlan(context.Background(), runDirectory, userScript, projectsDirectory, workspaces)
	if err != nil {
		t.Fatalf("inspectProjectProvisioningPlan: %v", err)
	}
	got := inspection.Workspaces
	userStacks := inspection.UserStacks
	if strings.Join(projectStackStrings(got[0].Stacks), "|") != "android|audio|bun|cpp|dotnet|go|handy|hyperframes|java|nsis|nushell|playwright-cli|python|rust-msvc|tradingview|uv" {
		t.Fatalf("alpha stacks = %v", got[0].Stacks)
	}
	if strings.Join(projectStackStrings(got[1].Stacks), "|") != "bun|cargo-nextest|git-sh|just|python|rust-msvc|zig" {
		t.Fatalf("herdr stacks = %v", got[1].Stacks)
	}
	if strings.Join(projectStackStrings(userStacks), "|") != "android|cpp|rust-msvc|uv" {
		t.Fatalf("user stacks = %v", userStacks)
	}
}

func TestInspectProjectProvisioningPlanAcceptsEmptyGoProject(t *testing.T) {
	requireExternalBoundaryTest(t, "Windows PowerShell project-plan inspection")
	if runtime.GOOS != "windows" {
		t.Skip("Windows PowerShell 5.1 AST regression")
	}
	runDirectory := t.TempDir()
	projectsDirectory := t.TempDir()
	projectDirectory := t.TempDir()
	userScript := filepath.Join(runDirectory, userProvisioningName)
	writeTestFile(t, userScript, userProvisioningContract+"\n")
	profile := filepath.Join(projectsDirectory, "project.ps1")
	writeTestFile(t, profile, "Install-GoStack\n")
	inspection, err := inspectProjectProvisioningPlan(context.Background(), runDirectory, userScript, projectsDirectory,
		[]workspacePlan{{Name: "project", HostDirectory: projectDirectory, ProvisioningPath: profile}})
	if err != nil {
		t.Fatalf("empty Go project inspection: %v", err)
	}
	if len(inspection.ToolVersions) != 1 || inspection.ToolVersions[0].Tool != "GoLang.Go" ||
		inspection.ToolVersions[0].Version != "" || inspection.ToolVersions[0].Source != toolVersionSourceDefault {
		t.Fatalf("empty Go project tool plan = %#v", inspection.ToolVersions)
	}
}

func TestInspectProjectProvisioningPlanDefaultsPythonToCurrentSeries(t *testing.T) {
	requireExternalBoundaryTest(t, "Windows PowerShell project-plan inspection")
	if runtime.GOOS != "windows" {
		t.Skip("Windows PowerShell 5.1 AST regression")
	}
	runDirectory := t.TempDir()
	projectsDirectory := t.TempDir()
	userScript := filepath.Join(runDirectory, userProvisioningName)
	writeTestFile(t, userScript, userProvisioningContract+"\n")
	profile := filepath.Join(projectsDirectory, "project.ps1")
	writeTestFile(t, profile, "Install-PythonStack\n")
	inspection, err := inspectProjectProvisioningPlan(context.Background(), runDirectory, userScript, projectsDirectory,
		[]workspacePlan{{Name: "project", ProvisioningPath: profile}})
	if err != nil {
		t.Fatalf("default Python inspection: %v", err)
	}
	if len(inspection.ToolVersions) != 1 || inspection.ToolVersions[0].Tool != "Python" ||
		inspection.ToolVersions[0].Version != "" || inspection.ToolVersions[0].Series != "3.14" ||
		inspection.ToolVersions[0].Source != toolVersionSourceDefault {
		t.Fatalf("default Python tool plan = %#v", inspection.ToolVersions)
	}
}

func TestValidateGitPackageRequirementRequiresRetainedGit(t *testing.T) {
	terminal := testStableWindowsTerminalConfiguration()
	withoutGit, err := resolveWingetPackagePlan(wingetPackageConfiguration{Remove: []string{packageGit}}, terminal)
	if err != nil {
		t.Fatal(err)
	}
	workspaces := []workspacePlan{{Name: "herdr", Stacks: []projectStack{stackGitSH}}}
	if err := validateGitPackageRequirement(workspaces, nil, withoutGit); err == nil || !strings.Contains(err.Error(), packageGit) {
		t.Fatalf("missing Git shell requirement error = %v", err)
	}
	defaults, err := resolveWingetPackagePlan(defaultWingetPackageConfiguration(), terminal)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateGitPackageRequirement(workspaces, nil, defaults); err != nil {
		t.Fatalf("retained Git package rejected: %v", err)
	}
	if err := validateGitPackageRequirement([]workspacePlan{{Name: "hyperframes", Stacks: []projectStack{stackHyperFrames}}}, nil, withoutGit); err == nil ||
		!strings.Contains(err.Error(), packageGit) || !strings.Contains(err.Error(), "global agent skills") {
		t.Fatalf("missing HyperFrames Git requirement error = %v", err)
	}
	if err := validateGitPackageRequirement([]workspacePlan{{Name: "plain"}}, nil, withoutGit); err != nil {
		t.Fatalf("unrelated project rejected without Git: %v", err)
	}
}

func TestInspectProjectProvisioningPlanDefaultsHyperFramesToolsToLatestStable(t *testing.T) {
	requireExternalBoundaryTest(t, "Windows PowerShell project-plan inspection")
	if runtime.GOOS != "windows" {
		t.Skip("Windows PowerShell 5.1 AST regression")
	}
	runDirectory := t.TempDir()
	projectsDirectory := t.TempDir()
	userScript := filepath.Join(runDirectory, userProvisioningName)
	writeTestFile(t, userScript, userProvisioningContract+"\n")
	profile := filepath.Join(projectsDirectory, "hyperframes.ps1")
	writeTestFile(t, profile, "Install-HyperFramesStack\n")
	inspection, err := inspectProjectProvisioningPlan(context.Background(), runDirectory, userScript, projectsDirectory,
		[]workspacePlan{{Name: "hyperframes", ProvisioningPath: profile}})
	if err != nil {
		t.Fatalf("HyperFrames inspection: %v", err)
	}
	if strings.Join(projectStackStrings(inspection.Workspaces[0].Stacks), "|") != "hyperframes" {
		t.Fatalf("HyperFrames stacks = %v", inspection.Workspaces[0].Stacks)
	}
	wantTools := []string{"Gyan.FFmpeg", "hyperframes", "OpenJS.NodeJS.LTS"}
	if len(inspection.ToolVersions) != len(wantTools) {
		t.Fatalf("HyperFrames tool plan = %#v", inspection.ToolVersions)
	}
	for index, want := range wantTools {
		tool := inspection.ToolVersions[index]
		if tool.Tool != want || tool.Version != "" || tool.Series != "" || tool.Source != toolVersionSourceDefault {
			t.Fatalf("HyperFrames tool %d = %#v, want latest stable %s", index, tool, want)
		}
	}
}

func TestInspectProjectProvisioningPlanOwnsExactAudioTools(t *testing.T) {
	requireExternalBoundaryTest(t, "Windows PowerShell project-plan inspection")
	if runtime.GOOS != "windows" {
		t.Skip("Windows PowerShell 5.1 AST regression")
	}
	runDirectory := t.TempDir()
	projectsDirectory := t.TempDir()
	userScript := filepath.Join(runDirectory, userProvisioningName)
	writeTestFile(t, userScript, userProvisioningContract+"\n")
	profile := filepath.Join(projectsDirectory, "audio.ps1")
	writeTestFile(t, profile, "Install-AudioStack\n")
	inspection, err := inspectProjectProvisioningPlan(context.Background(), runDirectory, userScript, projectsDirectory,
		[]workspacePlan{{Name: "audio", ProvisioningPath: profile}})
	if err != nil {
		t.Fatalf("audio inspection: %v", err)
	}
	if strings.Join(projectStackStrings(inspection.Workspaces[0].Stacks), "|") != "audio" {
		t.Fatalf("audio stacks = %v", inspection.Workspaces[0].Stacks)
	}
	if len(inspection.ToolVersions) != 2 || inspection.ToolVersions[0].Tool != "AudioGridder" ||
		inspection.ToolVersions[0].Version != "1.2.0" || inspection.ToolVersions[1].Tool != packageREAPER ||
		inspection.ToolVersions[1].Version != "7.78" {
		t.Fatalf("audio tool plan = %#v", inspection.ToolVersions)
	}
	if !projectStackOwnsPackage(packageREAPER) {
		t.Fatalf("REAPER package is not audio-stack-owned: %s", packageREAPER)
	}
}

func TestInspectProjectProvisioningPlanRejectsParseErrors(t *testing.T) {
	requireExternalBoundaryTest(t, "Windows PowerShell project-plan inspection")
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
	_, err := inspectProjectProvisioningPlan(context.Background(), runDirectory, userScript, projectsDirectory, workspaces)
	if err == nil || !strings.Contains(err.Error(), "parse failed") || !strings.Contains(err.Error(), "broken-one.ps1") || !strings.Contains(err.Error(), "broken-two.ps1") {
		t.Fatalf("parse error = %v", err)
	}
}

func TestInspectProjectProvisioningPlanRejectsUserParamBlock(t *testing.T) {
	requireExternalBoundaryTest(t, "Windows PowerShell project-plan inspection")
	if runtime.GOOS != "windows" {
		t.Skip("Windows PowerShell 5.1 AST regression")
	}
	runDirectory := t.TempDir()
	projectsDirectory := t.TempDir()
	userScript := runDirectory + `\user.ps1`
	writeTestFile(t, userScript, userProvisioningContract+"\nparam([string]$Unexpected)\n")
	writeTestFile(t, projectsDirectory+`\alpha.ps1`, "Write-Output 'project'\n")
	_, err := inspectProjectProvisioningPlan(context.Background(), runDirectory, userScript, projectsDirectory, []workspacePlan{{Name: "alpha", ProvisioningPath: projectsDirectory + `\alpha.ps1`}})
	if err == nil || !strings.Contains(err.Error(), "must not declare a script-level param block") {
		t.Fatalf("user param block error = %v", err)
	}
}

func TestDecodeProjectProvisioningPlanIsStrict(t *testing.T) {
	workspaces := []workspacePlan{{Name: "alpha", ProvisioningPath: `C:\profiles\alpha.ps1`}}
	valid := []byte(`{"schemaVersion":3,"userStacks":["rust-msvc"],"userTools":[{"tool":"rust-toolchain","version":"1.92.0","series":"","source":"rust-msvc","projectDirectory":""}],"projects":[{"name":"alpha","stacks":["go"],"tools":[{"tool":"GoLang.Go","version":"1.26.5","series":"","source":"go","projectDirectory":""}]}]}`)
	inspection, err := decodeProjectProvisioningPlan(valid, workspaces)
	if err != nil || len(inspection.Workspaces) != 1 || len(inspection.Workspaces[0].Stacks) != 1 || inspection.Workspaces[0].Stacks[0] != stackGo {
		t.Fatalf("valid plan = %#v, %v", inspection, err)
	}
	if len(inspection.UserStacks) != 1 || inspection.UserStacks[0] != stackRustMSVC || len(inspection.ToolVersions) != 2 {
		t.Fatalf("valid inspection = %#v", inspection)
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
		[]byte(`{"schemaVersion":3,"userStacks":[],"userTools":[],"projects":[{"name":"alpha","stacks":[],"tools":[],"extra":true}]}`),
		[]byte(`{"schemaVersion":3,"userStacks":[],"userTools":[],"projects":[{"name":"alpha","stacks":[],"tools":[{"tool":"GoLang.Go","version":"","series":"","source":"go","projectDirectory":"","extra":true}]}]}`),
	} {
		if _, err := decodeProjectProvisioningPlan(invalid, workspaces); err == nil {
			t.Fatalf("invalid plan unexpectedly decoded: %s", invalid)
		}
	}
}

func TestDecodeProjectProvisioningPlanAllowsWorkspaceWithoutProfile(t *testing.T) {
	workspaces := []workspacePlan{
		{Name: "profiled", ProvisioningPath: `C:\profiles\profiled.ps1`},
		{Name: "plain"},
	}
	data := []byte(`{"schemaVersion":3,"userStacks":[],"userTools":[],"projects":[{"name":"profiled","stacks":["go"],"tools":[{"tool":"GoLang.Go","version":"","series":"","source":"go","projectDirectory":""}]}]}`)
	inspection, err := decodeProjectProvisioningPlan(data, workspaces)
	if err != nil {
		t.Fatalf("decode optional project provisioning plan: %v", err)
	}
	got := inspection.Workspaces
	if len(got) != 2 || len(got[0].Stacks) != 1 || got[0].Stacks[0] != stackGo || len(got[1].Stacks) != 0 {
		t.Fatalf("optional project provisioning plan = %#v", got)
	}
}

func TestInspectProjectProvisioningPlanMergesExactAndOmittedToolVersions(t *testing.T) {
	requireExternalBoundaryTest(t, "Windows PowerShell project-plan inspection")
	if runtime.GOOS != "windows" {
		t.Skip("Windows PowerShell 5.1 AST regression")
	}
	runDirectory := t.TempDir()
	projectsDirectory := t.TempDir()
	userScript := runDirectory + `\user.ps1`
	writeTestFile(t, userScript, userProvisioningContract+"\nInstall-GoStack -Version '1.26.5'\n")
	writeTestFile(t, projectsDirectory+`\alpha.ps1`, "Install-GoStack\n")
	projectDirectory := t.TempDir()
	inspection, err := inspectProjectProvisioningPlan(context.Background(), runDirectory, userScript, projectsDirectory,
		[]workspacePlan{{Name: "alpha", HostDirectory: projectDirectory, ProvisioningPath: projectsDirectory + `\alpha.ps1`}})
	if err != nil {
		t.Fatalf("merge exact and omitted versions: %v", err)
	}
	var goTool resolvedToolVersion
	for _, tool := range inspection.ToolVersions {
		if tool.Tool == "GoLang.Go" {
			goTool = tool
		}
	}
	if goTool.Version != "1.26.5" || goTool.Source != toolVersionSourceExplicit || len(goTool.Owners) != 2 {
		t.Fatalf("merged Go tool = %#v", goTool)
	}
}

func TestInspectProjectProvisioningPlanRejectsConflictingExactToolVersions(t *testing.T) {
	requireExternalBoundaryTest(t, "Windows PowerShell project-plan inspection")
	if runtime.GOOS != "windows" {
		t.Skip("Windows PowerShell 5.1 AST regression")
	}
	runDirectory := t.TempDir()
	projectsDirectory := t.TempDir()
	userScript := runDirectory + `\user.ps1`
	writeTestFile(t, userScript, userProvisioningContract+"\n")
	writeTestFile(t, projectsDirectory+`\alpha.ps1`, "Install-GoStack -Version '1.26.4'\n")
	writeTestFile(t, projectsDirectory+`\beta.ps1`, "Install-GoStack -Version '1.26.5'\n")
	_, err := inspectProjectProvisioningPlan(context.Background(), runDirectory, userScript, projectsDirectory, []workspacePlan{
		{Name: "alpha", HostDirectory: t.TempDir(), ProvisioningPath: projectsDirectory + `\alpha.ps1`},
		{Name: "beta", HostDirectory: t.TempDir(), ProvisioningPath: projectsDirectory + `\beta.ps1`},
	})
	if err == nil || !strings.Contains(err.Error(), "GoLang.Go") || !strings.Contains(err.Error(), "1.26.4") ||
		!strings.Contains(err.Error(), "1.26.5") || !strings.Contains(err.Error(), `project "alpha"`) || !strings.Contains(err.Error(), `project "beta"`) {
		t.Fatalf("tool version conflict = %v", err)
	}
}

func TestInspectProjectProvisioningPlanRejectsDynamicToolVersion(t *testing.T) {
	requireExternalBoundaryTest(t, "Windows PowerShell project-plan inspection")
	if runtime.GOOS != "windows" {
		t.Skip("Windows PowerShell 5.1 AST regression")
	}
	runDirectory := t.TempDir()
	projectsDirectory := t.TempDir()
	projectDirectory := t.TempDir()
	userScript := runDirectory + `\user.ps1`
	writeTestFile(t, userScript, userProvisioningContract+"\n")
	profileDirectory := filepath.Join(projectDirectory, projectConfigurationName)
	if err := os.Mkdir(profileDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	profile := filepath.Join(profileDirectory, projectProvisioningName)
	profileData := "$version = '1.26.5'\nInstall-GoStack -Version $version\n"
	writeTestFile(t, profile, profileData)
	writeTestFile(t, projectsDirectory+`\alpha.ps1`, profileData)
	_, err := inspectProjectProvisioningPlan(context.Background(), runDirectory, userScript, projectsDirectory,
		[]workspacePlan{{Name: "alpha", HostDirectory: projectDirectory, ProvisioningPath: profile}})
	if err == nil || !strings.Contains(normalizedProjectPlanError(err), "must be one literal string") ||
		!strings.Contains(err.Error(), "Workspace: alpha") ||
		!strings.Contains(err.Error(), "Host directory: "+projectDirectory) ||
		!strings.Contains(err.Error(), "Profile: "+profile) {
		t.Fatalf("dynamic version error = %v", err)
	}
}

func TestInspectProjectProvisioningPlanResolvesProjectPlaywrightLockVersion(t *testing.T) {
	requireExternalBoundaryTest(t, "Windows PowerShell project-plan inspection")
	if runtime.GOOS != "windows" {
		t.Skip("Windows PowerShell 5.1 AST regression")
	}
	runDirectory := t.TempDir()
	projectsDirectory := t.TempDir()
	projectDirectory := t.TempDir()
	frontendDirectory := filepath.Join(projectDirectory, "frontend")
	if err := os.Mkdir(frontendDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(frontendDirectory, "package-lock.json"), playwrightPackageLock("1.61.1", "1.61.1", "1.61.1", "1.61.1", "1.61.1"))
	userScript := filepath.Join(runDirectory, userProvisioningName)
	writeTestFile(t, userScript, userProvisioningContract+"\n")
	profile := filepath.Join(projectsDirectory, "jobs.ps1")
	writeTestFile(t, profile, "$projectPlaywrightVersion = 'runtime-validated-from-lock'\nInstall-NodeStack -PlaywrightVersion $projectPlaywrightVersion\n")
	inspection, err := inspectProjectProvisioningPlan(context.Background(), runDirectory, userScript, projectsDirectory,
		[]workspacePlan{{Name: "jobs", HostDirectory: projectDirectory, ProvisioningPath: profile}})
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range inspection.ToolVersions {
		if tool.Tool == "playwright" {
			if tool.Version != "1.61.1" || tool.Source != toolVersionSourceSelectedProject ||
				strings.Join(tool.Owners, "|") != `project "jobs" (node-project-lock)` {
				t.Fatalf("Playwright lock tool = %#v", tool)
			}
			return
		}
	}
	t.Fatal("Playwright lock tool is missing")
}

func TestInspectProjectProvisioningPlanRejectsInvalidProjectPlaywrightLock(t *testing.T) {
	requireExternalBoundaryTest(t, "Windows PowerShell project-plan inspection")
	if runtime.GOOS != "windows" {
		t.Skip("Windows PowerShell 5.1 AST regression")
	}
	runDirectory := t.TempDir()
	projectsDirectory := t.TempDir()
	projectDirectory := t.TempDir()
	frontendDirectory := filepath.Join(projectDirectory, "frontend")
	if err := os.Mkdir(frontendDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(frontendDirectory, "package-lock.json"), playwrightPackageLock("1.61.1", "1.61.0", "1.61.1", "1.61.1", "1.61.1"))
	userScript := filepath.Join(runDirectory, userProvisioningName)
	writeTestFile(t, userScript, userProvisioningContract+"\n")
	profile := filepath.Join(projectsDirectory, "jobs.ps1")
	writeTestFile(t, profile, "Install-NodeStack -PlaywrightVersion $projectPlaywrightVersion\n")
	_, err := inspectProjectProvisioningPlan(context.Background(), runDirectory, userScript, projectsDirectory,
		[]workspacePlan{{Name: "jobs", HostDirectory: projectDirectory, ProvisioningPath: profile}})
	if err == nil || !strings.Contains(err.Error(), "package-lock versions for Playwright are missing or inconsistent") {
		t.Fatalf("invalid Playwright package lock error = %v", err)
	}
}

func TestReadProjectPlaywrightVersionRejectsPrerelease(t *testing.T) {
	projectDirectory := t.TempDir()
	frontendDirectory := filepath.Join(projectDirectory, "frontend")
	if err := os.Mkdir(frontendDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(frontendDirectory, "package-lock.json"), playwrightPackageLock("1.62.0-beta.1", "1.62.0-beta.1", "1.62.0-beta.1", "1.62.0-beta.1", "1.62.0-beta.1"))
	_, err := readProjectPlaywrightVersion(projectDirectory)
	if err == nil || !strings.Contains(err.Error(), "package-lock versions for Playwright are missing or inconsistent") {
		t.Fatalf("prerelease Playwright package lock error = %v", err)
	}
}

func TestInspectProjectProvisioningPlanRejectsOtherDynamicPlaywrightVersion(t *testing.T) {
	requireExternalBoundaryTest(t, "Windows PowerShell project-plan inspection")
	if runtime.GOOS != "windows" {
		t.Skip("Windows PowerShell 5.1 AST regression")
	}
	runDirectory := t.TempDir()
	projectsDirectory := t.TempDir()
	userScript := filepath.Join(runDirectory, userProvisioningName)
	writeTestFile(t, userScript, userProvisioningContract+"\n")
	profile := filepath.Join(projectsDirectory, "project.ps1")
	writeTestFile(t, profile, "$version = '1.61.1'\nInstall-NodeStack -PlaywrightVersion $version\n")
	_, err := inspectProjectProvisioningPlan(context.Background(), runDirectory, userScript, projectsDirectory,
		[]workspacePlan{{Name: "project", HostDirectory: t.TempDir(), ProvisioningPath: profile}})
	if err == nil || !strings.Contains(normalizedProjectPlanError(err), "parameter -PlaywrightVersion must be one literal string") {
		t.Fatalf("dynamic Playwright version error = %v", err)
	}
}

func TestInspectProjectProvisioningPlanRejectsDynamicRustProjectDirectory(t *testing.T) {
	requireExternalBoundaryTest(t, "Windows PowerShell project-plan inspection")
	if runtime.GOOS != "windows" {
		t.Skip("Windows PowerShell 5.1 AST regression")
	}
	runDirectory := t.TempDir()
	projectsDirectory := t.TempDir()
	userScript := runDirectory + `\user.ps1`
	writeTestFile(t, userScript, userProvisioningContract+"\n")
	writeTestFile(t, projectsDirectory+`\alpha.ps1`, "$rustRoot = Join-Path $ProjectDirectory 'src'\nInstall-RustMSVCStack -ProjectDirectory $rustRoot\n")
	_, err := inspectProjectProvisioningPlan(context.Background(), runDirectory, userScript, projectsDirectory,
		[]workspacePlan{{Name: "alpha", HostDirectory: t.TempDir(), ProvisioningPath: projectsDirectory + `\alpha.ps1`}})
	if err == nil || !strings.Contains(normalizedProjectPlanError(err), "parameter -ProjectDirectory must be one literal string") {
		t.Fatalf("dynamic Rust project directory error = %v", err)
	}
}

func TestMergeProjectToolVersionsRejectsRustDirectoryOutsideWorkspace(t *testing.T) {
	workspace := t.TempDir()
	outside := t.TempDir()
	_, err := mergeProjectToolVersions(nil, []projectProvisioningPlanEntry{{
		Name:  "project",
		Tools: []projectToolRequirement{{Tool: "rust-toolchain", Source: "rust-msvc", ProjectDirectory: outside}},
	}}, []workspacePlan{{Name: "project", HostDirectory: workspace}})
	if err == nil || !strings.Contains(err.Error(), "must stay within its mapped workspace") {
		t.Fatalf("outside Rust project directory error = %v", err)
	}
}

func TestMergeProjectToolVersionsIncludesRustToolchainFiles(t *testing.T) {
	alpha := t.TempDir()
	beta := t.TempDir()
	writeTestFile(t, filepath.Join(alpha, "rust-toolchain.toml"), "[toolchain]\nchannel = \"1.91.0\"\n")
	writeTestFile(t, filepath.Join(beta, "rust-toolchain.toml"), "[toolchain]\nchannel = \"1.92.0\"\n")
	projects := []projectProvisioningPlanEntry{
		{Name: "alpha", Tools: []projectToolRequirement{{Tool: "Rustlang.Rustup", Source: "rust-msvc"}, {Tool: "rust-toolchain", Source: "rust-msvc", ProjectDirectory: "$ProjectDirectory"}}},
		{Name: "beta", Tools: []projectToolRequirement{{Tool: "Rustlang.Rustup", Source: "rust-msvc"}, {Tool: "rust-toolchain", Source: "rust-msvc", ProjectDirectory: "$ProjectDirectory"}}},
	}
	_, err := mergeProjectToolVersions(nil, projects, []workspacePlan{{Name: "alpha", HostDirectory: alpha}, {Name: "beta", HostDirectory: beta}})
	if err == nil || !strings.Contains(err.Error(), "rust-toolchain") || !strings.Contains(err.Error(), "1.91.0") || !strings.Contains(err.Error(), "1.92.0") {
		t.Fatalf("Rust toolchain conflict = %v", err)
	}
}

func TestMergeProjectToolVersionsKeepsRustupPackageSeparateFromToolchain(t *testing.T) {
	project := t.TempDir()
	writeTestFile(t, filepath.Join(project, "rust-toolchain.toml"), "[toolchain]\nchannel = \"1.92.0\"\n")
	tools, err := mergeProjectToolVersions(nil, []projectProvisioningPlanEntry{{
		Name: "project",
		Tools: []projectToolRequirement{
			{Tool: "Rustlang.Rustup", Source: "rust-msvc"},
			{Tool: "rust-toolchain", Source: "rust-msvc", ProjectDirectory: "$ProjectDirectory"},
		},
	}}, []workspacePlan{{Name: "project", HostDirectory: project}})
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != 2 || tools[0].Tool != "rust-toolchain" || tools[0].Version != "1.92.0" ||
		tools[0].Source != toolVersionSourceProject || tools[1].Tool != "Rustlang.Rustup" ||
		tools[1].Version != "" || tools[1].Source != toolVersionSourceDefault {
		t.Fatalf("Rust package/toolchain plan = %#v", tools)
	}
}

func TestMergeProjectToolVersionsExplicitRustOverridesProjectFile(t *testing.T) {
	project := t.TempDir()
	writeTestFile(t, filepath.Join(project, "rust-toolchain.toml"), "not valid TOML for this contract\n")
	tools, err := mergeProjectToolVersions(nil, []projectProvisioningPlanEntry{{
		Name:  "project",
		Tools: []projectToolRequirement{{Tool: "rust-toolchain", Version: "1.92.0", Source: "rust-msvc", ProjectDirectory: "$ProjectDirectory"}},
	}}, []workspacePlan{{Name: "project", HostDirectory: project}})
	if err != nil {
		t.Fatalf("explicit Rust toolchain did not override optional project file: %v", err)
	}
	if len(tools) != 1 || tools[0].Version != "1.92.0" || tools[0].Source != toolVersionSourceExplicit {
		t.Fatalf("explicit Rust precedence = %#v", tools)
	}
}

func TestMergeProjectToolVersionsRejectsConflictingPythonSeries(t *testing.T) {
	_, err := mergeProjectToolVersions(nil, []projectProvisioningPlanEntry{
		{Name: "alpha", Tools: []projectToolRequirement{{Tool: "Python", Series: "3.12", Source: "python"}}},
		{Name: "beta", Tools: []projectToolRequirement{{Tool: "Python", Version: "3.13.7", Source: "python"}}},
	}, []workspacePlan{{Name: "alpha"}, {Name: "beta"}})
	if err == nil || !strings.Contains(err.Error(), "conflicting exact series for tool Python") ||
		!strings.Contains(err.Error(), "3.12") || !strings.Contains(err.Error(), "3.13") {
		t.Fatalf("Python series conflict = %v", err)
	}
}

func TestPrepareProvisioningSnapshotWritesMergedToolPlan(t *testing.T) {
	requireExternalBoundaryTest(t, "Windows PowerShell project-plan inspection")
	if runtime.GOOS != "windows" {
		t.Skip("Windows PowerShell 5.1 AST regression")
	}
	root := t.TempDir()
	project := filepath.Join(root, "project")
	profileDirectory := filepath.Join(project, projectConfigurationName)
	if err := os.MkdirAll(profileDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	profile := filepath.Join(profileDirectory, projectProvisioningName)
	writeTestFile(t, profile, "param([Parameter(Mandatory = $true)][string]$ProjectDirectory)\nInstall-GoStack -Version '1.26.5'\n")
	user := filepath.Join(root, userProvisioningName)
	writeTestFile(t, user, userProvisioningContract+"\n")
	terminal := testStableWindowsTerminalConfiguration()
	packages, err := resolveWingetPackagePlan(defaultWingetPackageConfiguration(), terminal)
	if err != nil {
		t.Fatal(err)
	}
	inspectionDirectory := filepath.Join(root, "inspection")
	if err := os.Mkdir(inspectionDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	snapshot, err := prepareProvisioningSnapshot(context.Background(), inspectionDirectory, filepath.Join(root, "snapshot"), provisioningPlan{
		BaseScript: filepath.Join("..", "..", "provisioning", baseProvisioningName), StackScript: filepath.Join("..", "..", "provisioning", stackProvisioningName),
		UserScript: user, Packages: packages, WindowsTerminal: terminal,
		Workspaces: []workspacePlan{{Name: "project", HostDirectory: project, GuestDirectory: guestWorkspaceDirectory("project"), ProvisioningPath: profile, Active: true}},
	})
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(snapshot.ToolVersionPlanPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "{\"schemaVersion\":2,\"tools\":[{\"tool\":\"GoLang.Go\",\"version\":\"1.26.5\",\"series\":\"\",\"source\":\"explicit-provisioning\",\"owners\":[\"project \\\"project\\\" (go)\"]}]}\n" {
		t.Fatalf("tool version snapshot = %s", data)
	}
	launcherData, err := os.ReadFile(filepath.Join(snapshot.Directory, activeSessionLaunchName))
	if err != nil {
		t.Fatal(err)
	}
	if string(launcherData) != string(activeSessionLaunchScript) {
		t.Fatal("active-session launcher snapshot differs from its embedded owner")
	}
}

func playwrightPackageLock(testVersion, testDependency, playwrightVersion, coreDependency, coreVersion string) string {
	return `{"lockfileVersion":3,"packages":{"node_modules/@playwright/test":{"version":"` + testVersion + `","dependencies":{"playwright":"` + testDependency + `"}},"node_modules/playwright":{"version":"` + playwrightVersion + `","dependencies":{"playwright-core":"` + coreDependency + `"}},"node_modules/playwright-core":{"version":"` + coreVersion + `"}}}`
}

func projectStackStrings(stacks []projectStack) []string {
	values := make([]string, len(stacks))
	for index, stack := range stacks {
		values[index] = string(stack)
	}
	return values
}
