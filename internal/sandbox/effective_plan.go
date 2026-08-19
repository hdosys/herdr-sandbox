package sandbox

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type EffectivePackage struct {
	ID      string
	Version string
	Source  string
}

type EffectiveWorkspace struct {
	Name           string
	HostDirectory  string
	GuestDirectory string
	Active         bool
	Stacks         []string
}

type EffectiveMount struct {
	Name           string
	HostDirectory  string
	GuestDirectory string
	ReadOnly       bool
}

type EffectiveStackPackage struct {
	Stack        string
	PackageOwner string
}

type EffectiveToolVersion struct {
	Tool      string
	Selection string
	Source    string
	Owners    []string
}

// EffectivePlan is the read-only user-facing view of the configuration that a
// later up command would consume.
type EffectivePlan struct {
	ConfigurationPath             string
	ConfigurationExists           bool
	UserScriptPath                string
	UserScriptExists              bool
	CacheDirectory                string
	WorktreeDirectory             string
	MemoryMB                      int
	AudioOutput                   bool
	AudioInput                    bool
	Tailscale                     bool
	MobileSSHAuthorizedKeyCount   int
	WindowsTerminal               string
	PullHostGitRepositoriesOnUp   bool
	PullHostGitRepositoriesOnDown bool
	CodingAgents                  []string
	GlobalStacks                  []string
	Packages                      []EffectivePackage
	StackPackages                 []EffectiveStackPackage
	ToolVersions                  []EffectiveToolVersion
	Mounts                        []EffectiveMount
	Workspaces                    []EffectiveWorkspace
	RequiresVisualStudio          bool
	ReadyChanges                  []string
	NextAction                    string
}

func buildEffectivePlan(ctx context.Context, provisioning provisioningPlan, configurationPath string, configurationExists, userScriptExists bool) (EffectivePlan, error) {
	if err := validateBaseProvisioningContract(provisioning.BaseScript); err != nil {
		return EffectivePlan{}, err
	}
	if err := validateStackProvisioningContract(provisioning.StackScript); err != nil {
		return EffectivePlan{}, err
	}
	workspaces := append([]workspacePlan(nil), provisioning.Workspaces...)
	var userStacks []projectStack
	var toolVersions []resolvedToolVersion
	requiresVisualStudio := false
	if userScriptExists || len(workspaces) > 0 {
		inspection, err := inspectEffectivePlanScripts(ctx, provisioning, userScriptExists)
		if err != nil {
			return EffectivePlan{}, err
		}
		workspaces = inspection.Workspaces
		userStacks = inspection.UserStacks
		toolVersions = inspection.ToolVersions
		requirements := runPlan{Workspaces: workspaces}
		applyWorkspaceRequirements(&requirements)
		requiresVisualStudio = requirements.RequiresVisualStudioLayout || stacksRequireVisualStudioLayout(userStacks)
	}
	if err := validateGitPackageRequirement(workspaces, userStacks, provisioning.Packages); err != nil {
		return EffectivePlan{}, err
	}
	cacheDirectory, err := effectiveCacheDirectory(provisioning.CacheDirectory)
	if err != nil {
		return EffectivePlan{}, err
	}
	plan := EffectivePlan{
		ConfigurationPath:             configurationPath,
		ConfigurationExists:           configurationExists,
		UserScriptPath:                provisioning.UserScript,
		UserScriptExists:              userScriptExists,
		CacheDirectory:                cacheDirectory,
		WorktreeDirectory:             provisioning.WorktreeDirectory,
		MemoryMB:                      provisioning.MemoryMB,
		AudioOutput:                   provisioning.AudioOutput,
		AudioInput:                    provisioning.AudioInput,
		Tailscale:                     provisioning.Tailscale,
		MobileSSHAuthorizedKeyCount:   len(provisioning.MobileSSHAuthorizedKeys),
		WindowsTerminal:               provisioning.WindowsTerminal.Edition,
		PullHostGitRepositoriesOnUp:   provisioning.ConfigurationSync.PullHostGitRepositoriesOnUp,
		PullHostGitRepositoriesOnDown: provisioning.ConfigurationSync.PullHostGitRepositoriesOnDown,
		CodingAgents:                  codingAgentSyncNames(provisioning.CodingAgentSync),
		RequiresVisualStudio:          requiresVisualStudio,
	}
	if len(toolVersions) > 0 {
		for _, tool := range toolVersions {
			selection := tool.Version
			if selection == "" && tool.Series != "" {
				selection = "latest stable in series " + tool.Series
			} else if selection == "" {
				selection = "latest stable during provisioning"
			}
			plan.ToolVersions = append(plan.ToolVersions, EffectiveToolVersion{
				Tool: tool.Tool, Selection: selection, Source: effectiveToolVersionSource(tool.Source), Owners: append([]string(nil), tool.Owners...),
			})
		}
	}
	for _, stack := range userStacks {
		plan.GlobalStacks = append(plan.GlobalStacks, string(stack))
	}
	for _, group := range []struct {
		name    string
		entries []wingetPackagePlanEntry
	}{{"base", provisioning.Packages.Defaults}, {"addition", provisioning.Packages.Additions}} {
		for _, entry := range group.entries {
			version := entry.Version
			if version == "" {
				version = "latest during provisioning"
			}
			plan.Packages = append(plan.Packages, EffectivePackage{ID: entry.ID, Version: version, Source: group.name})
		}
	}
	sort.Slice(plan.Packages, func(left, right int) bool {
		leftFold := strings.ToLower(plan.Packages[left].ID)
		rightFold := strings.ToLower(plan.Packages[right].ID)
		if leftFold == rightFold {
			return plan.Packages[left].ID < plan.Packages[right].ID
		}
		return leftFold < rightFold
	})
	for _, workspace := range workspaces {
		stacks := make([]string, len(workspace.Stacks))
		for index, stack := range workspace.Stacks {
			stacks[index] = string(stack)
		}
		plan.Workspaces = append(plan.Workspaces, EffectiveWorkspace{
			Name:           workspace.Name,
			HostDirectory:  workspace.HostDirectory,
			GuestDirectory: workspace.GuestDirectory,
			Active:         workspace.Active,
			Stacks:         stacks,
		})
	}
	for _, mount := range provisioning.Mounts {
		plan.Mounts = append(plan.Mounts, EffectiveMount(mount))
	}
	selectedStacks := make(map[projectStack]bool)
	for _, stack := range userStacks {
		selectedStacks[stack] = true
	}
	for _, workspace := range workspaces {
		for _, stack := range workspace.Stacks {
			selectedStacks[stack] = true
		}
	}
	stackNames := make([]string, 0, len(selectedStacks))
	for stack := range selectedStacks {
		stackNames = append(stackNames, string(stack))
	}
	sort.Strings(stackNames)
	for _, name := range stackNames {
		stack := projectStack(name)
		plan.StackPackages = append(plan.StackPackages, EffectiveStackPackage{
			Stack:        name,
			PackageOwner: effectiveStackPackageOwner(stack),
		})
	}
	if len(plan.Workspaces) == 0 {
		plan.NextAction = "Run `sandbox init --stack <name>` from a project, or add a configured workspace."
	} else {
		plan.NextAction = "Run `sandbox up` to apply this plan."
	}
	return plan, nil
}

func effectiveToolVersionSource(source string) string {
	switch source {
	case toolVersionSourceExplicit:
		return "explicit provisioning value or stack constraint"
	case toolVersionSourceSelectedProject:
		return "project version file explicitly selected by provisioning"
	case toolVersionSourceProject:
		return "optional project version file"
	case toolVersionSourceDefault:
		return "stack default/latest stable"
	default:
		return source
	}
}

func effectiveStackPackageOwner(stack projectStack) string {
	switch stack {
	case stackAndroid:
		return "Android SDK Command-line Tools + Platform Tools + Microsoft OpenJDK 17"
	case stackBun:
		return "Oven-sh.Bun"
	case stackCargoNextest:
		return "nextest.cargo-nextest"
	case stackCpp:
		return "Visual Studio 2022 Build Tools (MSVC + Windows 11 SDK 26100)"
	case stackDotNet:
		return "Microsoft.DotNet.SDK.10"
	case stackGitSH:
		return packageGit
	case stackGo:
		return "GoLang.Go"
	case stackHandy:
		return packageCMake + " + " + packageVulkanSDK + " 1.4.309.0 + " + packageWebView2
	case stackHyperFrames:
		return "OpenJS.NodeJS.LTS + Gyan.FFmpeg full + hyperframes@latest + global HyperFrames skills"
	case stackJava:
		return packageOpenJDK25
	case stackJust:
		return "Casey.Just"
	case stackNode:
		return "OpenJS.NodeJS.LTS"
	case stackNSIS:
		return packageNSIS
	case stackNushell:
		return packageNushell
	case stackPlaywrightCLI:
		return "OpenJS.NodeJS.LTS + @playwright/cli@0.1.17"
	case stackPython:
		return "Python.Python.<family selected by stack>"
	case stackRustMSVC:
		return "Rustlang.Rustup"
	case stackTradingView:
		return "OpenJS.NodeJS.LTS + TradingView.TradingViewDesktop + @ferroxlabs/tvcontrol@latest"
	case stackUV:
		return packageUV
	case stackZig:
		return "zig.zig"
	default:
		return ""
	}
}

func inspectEffectivePlanScripts(ctx context.Context, provisioning provisioningPlan, userScriptExists bool) (projectProvisioningInspection, error) {
	temporary, err := os.MkdirTemp("", "herdr-sandbox-plan-")
	if err != nil {
		return projectProvisioningInspection{}, fmt.Errorf("create temporary plan inspection directory: %w", err)
	}
	defer os.RemoveAll(temporary)
	projectsDirectory := filepath.Join(temporary, "projects")
	if err := os.Mkdir(projectsDirectory, 0o700); err != nil {
		return projectProvisioningInspection{}, fmt.Errorf("create temporary project inspection directory: %w", err)
	}
	userData := append([]byte(nil), defaultUserProvisioningScript...)
	if userScriptExists {
		userData, err = readProvisioningScript(provisioning.UserScript, "user provisioning script", maximumUserScriptSize)
		if err != nil {
			return projectProvisioningInspection{}, err
		}
	}
	userPath := filepath.Join(temporary, userProvisioningName)
	if err := os.WriteFile(userPath, userData, 0o600); err != nil {
		return projectProvisioningInspection{}, fmt.Errorf("write temporary user inspection input: %w", err)
	}
	for _, workspace := range provisioning.Workspaces {
		if workspace.ProvisioningPath == "" {
			continue
		}
		data, err := readProvisioningScript(workspace.ProvisioningPath, "project provisioning script", maximumProjectScriptSize)
		if err != nil {
			return projectProvisioningInspection{}, err
		}
		if err := os.WriteFile(filepath.Join(projectsDirectory, workspace.Name+".ps1"), data, 0o600); err != nil {
			return projectProvisioningInspection{}, fmt.Errorf("write temporary project inspection input: %w", err)
		}
	}
	inspection, err := inspectProjectProvisioningPlan(ctx, temporary, userPath, projectsDirectory, provisioning.Workspaces)
	if err != nil {
		return projectProvisioningInspection{}, err
	}
	if err := os.RemoveAll(temporary); err != nil && !errors.Is(err, os.ErrNotExist) {
		return projectProvisioningInspection{}, fmt.Errorf("remove temporary plan inspection directory: %w", err)
	}
	return inspection, nil
}

func inspectEffectiveReadyChanges(ctx context.Context, provisioning provisioningPlan) ([]string, error) {
	dataDirectory, err := defaultDataDirectory()
	if err != nil {
		return nil, err
	}
	return inspectEffectiveReadyChangesAt(ctx, provisioning, dataDirectory)
}

func inspectEffectiveReadyChangesAt(ctx context.Context, provisioning provisioningPlan, dataDirectory string) ([]string, error) {
	activeExists, err := regularFileExists(filepath.Join(dataDirectory, activeSessionFileName))
	if err != nil {
		return nil, fmt.Errorf("inspect active Sandbox identity for effective plan: %w", err)
	}
	if !activeExists {
		return nil, nil
	}
	executable, err := windowsSandboxExecutable()
	if err != nil {
		return nil, err
	}
	active, found, err := loadActiveSession(dataDirectory, executable)
	if err != nil || !found {
		return nil, err
	}
	snapshot, running, err := inspectSandboxProcess(ctx, active.PID)
	if err != nil {
		return nil, err
	}
	if !running || active.matches(snapshot) != nil {
		return nil, nil
	}
	status, err := classifyManagedSession(dataDirectory, active)
	if err != nil {
		return nil, err
	}
	if status.State != SessionReady {
		return nil, nil
	}
	_, differences, err := retainedRunPlanDetails(active, provisioning, provisioning.MemoryMB)
	if err != nil {
		return nil, err
	}
	after, found, err := loadActiveSession(dataDirectory, executable)
	if err != nil {
		return nil, err
	}
	if !found || after != active {
		return nil, errors.New("active Sandbox identity changed while comparing the effective plan")
	}
	return differences, nil
}
