package sandbox

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestBuildEffectivePlanInspectsDirectStacksWithoutMutatingInputs(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows PowerShell 5.1 AST regression")
	}
	root := t.TempDir()
	project := filepath.Join(root, "project")
	configuration := filepath.Join(project, projectConfigurationName)
	if err := os.MkdirAll(configuration, 0o700); err != nil {
		t.Fatal(err)
	}
	profile := filepath.Join(configuration, projectProvisioningName)
	profileData := []byte("Install-AndroidStack\nInstall-CppStack\nInstall-DotNetStack\nInstall-GoStack -ProjectDirectory $ProjectDirectory\nInstall-HandyStack -ProjectDirectory $ProjectDirectory\nInstall-JavaStack\nInstall-NSISStack\nInstall-PythonAIStack\nInstall-TradingViewStack\n")
	if err := os.WriteFile(profile, profileData, 0o600); err != nil {
		t.Fatal(err)
	}
	plain := filepath.Join(root, "plain")
	if err := os.MkdirAll(plain, 0o700); err != nil {
		t.Fatal(err)
	}
	terminal := testStableWindowsTerminalConfiguration()
	packages, err := resolveWingetPackagePlan(defaultWingetPackageConfiguration(), terminal)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := buildEffectivePlan(context.Background(), provisioningPlan{
		BaseScript:      filepath.Join("..", "..", "provisioning", baseProvisioningName),
		StackScript:     filepath.Join("..", "..", "provisioning", stackProvisioningName),
		UserScript:      filepath.Join(root, "missing-user.ps1"),
		MemoryMB:        4096,
		CodingAgentSync: defaultCodingAgentSyncConfiguration(),
		Packages:        packages,
		WindowsTerminal: terminal,
		Mounts: []mountPlan{{
			Name: "reference", HostDirectory: filepath.Join(root, "reference"), GuestDirectory: guestMountDirectory("reference"), ReadOnly: true,
		}},
		Workspaces: []workspacePlan{{
			Name: "project", HostDirectory: project, GuestDirectory: guestWorkspaceDirectory("project"),
			ProvisioningPath: profile, Active: true,
		}, {
			Name: "plain", HostDirectory: plain, GuestDirectory: guestWorkspaceDirectory("plain"),
		}},
	}, filepath.Join(root, "missing-config.json"), false, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Mounts) != 1 || plan.Mounts[0].Name != "reference" || !plan.Mounts[0].ReadOnly ||
		len(plan.Workspaces) != 2 || strings.Join(plan.Workspaces[0].Stacks, "|") != "android|bun|cpp|dotnet|go|handy|java|nsis|python|rust-msvc|tradingview|uv" || len(plan.Workspaces[1].Stacks) != 0 ||
		len(plan.StackPackages) != 12 || plan.StackPackages[0].PackageOwner != "Android SDK Command-line Tools + Platform Tools + Microsoft OpenJDK 17" ||
		plan.StackPackages[1].PackageOwner != "Oven-sh.Bun" ||
		plan.StackPackages[2].PackageOwner != "Visual Studio 2022 Build Tools (MSVC + Windows 11 SDK 26100)" ||
		plan.StackPackages[5].PackageOwner != "Kitware.CMake + KhronosGroup.VulkanSDK 1.4.309.0 + Microsoft.EdgeWebView2Runtime" ||
		plan.StackPackages[6].PackageOwner != "Microsoft.OpenJDK.25" ||
		plan.StackPackages[7].PackageOwner != packageNSIS ||
		plan.StackPackages[10].PackageOwner != "OpenJS.NodeJS.LTS + TradingView.TradingViewDesktop + @ferroxlabs/tvcontrol@latest" ||
		plan.StackPackages[11].PackageOwner != packageUV || !plan.RequiresVisualStudio ||
		plan.ConfigurationExists || plan.UserScriptExists || !strings.Contains(plan.NextAction, "up") {
		t.Fatalf("effective plan = %#v", plan)
	}
	for index := 1; index < len(plan.Packages); index++ {
		if strings.ToLower(plan.Packages[index-1].ID) > strings.ToLower(plan.Packages[index].ID) {
			t.Fatalf("effective packages are not sorted case-insensitively: %#v", plan.Packages)
		}
	}
	data, err := os.ReadFile(profile)
	if err != nil || string(data) != string(profileData) {
		t.Fatalf("profile changed = %q, %v", data, err)
	}
	if _, err := os.Lstat(filepath.Join(root, "missing-user.ps1")); !os.IsNotExist(err) {
		t.Fatalf("missing user script was seeded: %v", err)
	}
}

func TestBuildEffectivePlanInspectsGlobalStacksWithoutAWorkspace(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows PowerShell 5.1 AST regression")
	}
	root := t.TempDir()
	userScript := filepath.Join(root, userProvisioningName)
	if err := os.WriteFile(userScript, []byte(userProvisioningContract+"\nInstall-DotNetStack\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	terminal := testStableWindowsTerminalConfiguration()
	packages, err := resolveWingetPackagePlan(defaultWingetPackageConfiguration(), terminal)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := buildEffectivePlan(context.Background(), provisioningPlan{
		BaseScript: filepath.Join("..", "..", "provisioning", baseProvisioningName), StackScript: filepath.Join("..", "..", "provisioning", stackProvisioningName),
		UserScript: userScript, MemoryMB: defaultMemoryMB, CodingAgentSync: defaultCodingAgentSyncConfiguration(), Packages: packages, WindowsTerminal: terminal,
	}, filepath.Join(root, "missing-config.json"), false, true)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(plan.GlobalStacks, "|") != "dotnet" || len(plan.StackPackages) != 1 ||
		plan.StackPackages[0].PackageOwner != "Microsoft.DotNet.SDK.10" || !strings.Contains(plan.NextAction, "init") {
		t.Fatalf("global effective plan = %#v", plan)
	}
}

func TestBuildEffectivePlanRejectsHerdrWhenBaseGitIsRemoved(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows PowerShell 5.1 AST regression")
	}
	root := t.TempDir()
	project := filepath.Join(root, "herdr")
	configuration := filepath.Join(project, projectConfigurationName)
	if err := os.MkdirAll(configuration, 0o700); err != nil {
		t.Fatal(err)
	}
	profile := filepath.Join(configuration, projectProvisioningName)
	if err := os.WriteFile(profile, []byte("Install-HerdrStack -ProjectDirectory $ProjectDirectory\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	terminal := testStableWindowsTerminalConfiguration()
	packages, err := resolveWingetPackagePlan(wingetPackageConfiguration{Remove: []string{packageGit}}, terminal)
	if err != nil {
		t.Fatal(err)
	}
	_, err = buildEffectivePlan(context.Background(), provisioningPlan{
		BaseScript: filepath.Join("..", "..", "provisioning", baseProvisioningName), StackScript: filepath.Join("..", "..", "provisioning", stackProvisioningName),
		UserScript: filepath.Join(root, "missing-user.ps1"), MemoryMB: defaultMemoryMB, CodingAgentSync: defaultCodingAgentSyncConfiguration(),
		Packages: packages, WindowsTerminal: terminal, Workspaces: []workspacePlan{{Name: "herdr", HostDirectory: project, GuestDirectory: guestWorkspaceDirectory("herdr"), ProvisioningPath: profile, Active: true}},
	}, filepath.Join(root, "missing-config.json"), false, false)
	if err == nil || !strings.Contains(err.Error(), packageGit) || !strings.Contains(err.Error(), "sh.exe") {
		t.Fatalf("Herdr plan without Base Git error = %v", err)
	}
}

func TestBuildEffectivePlanExplainsEmptyWorkspaceNextAction(t *testing.T) {
	terminal := testStableWindowsTerminalConfiguration()
	packages, err := resolveWingetPackagePlan(defaultWingetPackageConfiguration(), terminal)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := buildEffectivePlan(context.Background(), provisioningPlan{
		BaseScript:      filepath.Join("..", "..", "provisioning", baseProvisioningName),
		StackScript:     filepath.Join("..", "..", "provisioning", stackProvisioningName),
		MemoryMB:        defaultMemoryMB,
		CodingAgentSync: defaultCodingAgentSyncConfiguration(),
		Packages:        packages,
		WindowsTerminal: terminal,
	}, "missing-config.json", false, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Workspaces) != 0 || !strings.Contains(plan.NextAction, "init") {
		t.Fatalf("empty effective plan = %#v", plan)
	}
}

func TestResolveProvisioningReadOnlyAtDoesNotSeedGlobalState(t *testing.T) {
	root := t.TempDir()
	globalRoot := filepath.Join(root, "global")
	defaultRoot := filepath.Join(root, "defaults")
	project := filepath.Join(root, "project")
	if err := os.MkdirAll(defaultRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(project, projectConfigurationName), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{baseProvisioningName, stackProvisioningName} {
		data, err := os.ReadFile(filepath.Join("..", "..", "provisioning", name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(defaultRoot, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(project, projectConfigurationName, projectProvisioningName), []byte("Install-DotNetStack\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	plan, err := resolveProvisioningReadOnlyAt(project, globalRoot, defaultRoot)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Workspaces) != 1 || plan.UserScript != filepath.Join(globalRoot, userProvisioningName) {
		t.Fatalf("read-only provisioning = %#v", plan)
	}
	if _, err := os.Lstat(globalRoot); !os.IsNotExist(err) {
		t.Fatalf("read-only provisioning seeded global state: %v", err)
	}
}

func TestInspectEffectiveReadyChangesNeedsNoSandboxWithoutActiveState(t *testing.T) {
	dataDirectory := t.TempDir()
	t.Setenv("WINDIR", filepath.Join(dataDirectory, "missing-windows"))
	changes, err := inspectEffectiveReadyChangesAt(context.Background(), provisioningPlan{}, dataDirectory)
	if err != nil || len(changes) != 0 {
		t.Fatalf("changes = %v, error = %v", changes, err)
	}
}
