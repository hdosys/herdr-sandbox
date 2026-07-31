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

type EffectiveStackPackage struct {
	Stack        string
	PackageOwner string
}

// EffectivePlan is the read-only user-facing view of the configuration that a
// later up command would consume.
type EffectivePlan struct {
	ConfigurationPath    string
	ConfigurationExists  bool
	UserScriptPath       string
	UserScriptExists     bool
	CacheDirectory       string
	MemoryMB             int
	Audio                bool
	Tailscale            bool
	WindowsTerminal      string
	CodingAgents         []string
	GlobalStacks         []string
	Packages             []EffectivePackage
	StackPackages        []EffectiveStackPackage
	Workspaces           []EffectiveWorkspace
	RequiresVisualStudio bool
	ReadyChanges         []string
	NextAction           string
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
	requiresVisualStudio := false
	if userScriptExists || len(workspaces) > 0 {
		inspected, inspectedUserStacks, err := inspectEffectivePlanScripts(ctx, provisioning, userScriptExists)
		if err != nil {
			return EffectivePlan{}, err
		}
		workspaces = inspected
		userStacks = inspectedUserStacks
		requirements := runPlan{Workspaces: workspaces}
		applyWorkspaceRequirements(&requirements)
		requiresVisualStudio = requirements.RequiresVisualStudioLayout || stacksContain(userStacks, stackRustMSVC)
	}
	cacheDirectory, err := effectiveCacheDirectory(provisioning.CacheDirectory)
	if err != nil {
		return EffectivePlan{}, err
	}
	plan := EffectivePlan{
		ConfigurationPath:    configurationPath,
		ConfigurationExists:  configurationExists,
		UserScriptPath:       provisioning.UserScript,
		UserScriptExists:     userScriptExists,
		CacheDirectory:       cacheDirectory,
		MemoryMB:             provisioning.MemoryMB,
		Audio:                provisioning.Audio,
		Tailscale:            provisioning.Tailscale,
		WindowsTerminal:      provisioning.WindowsTerminal.Edition,
		CodingAgents:         codingAgentSyncNames(provisioning.CodingAgentSync),
		RequiresVisualStudio: requiresVisualStudio,
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
		plan.NextAction = "Run `herdr-sandbox init --stack <name>` from a project, or add a configured workspace."
	} else {
		plan.NextAction = "Run `herdr-sandbox up` to apply this plan."
	}
	return plan, nil
}

func effectiveStackPackageOwner(stack projectStack) string {
	switch stack {
	case stackCargoNextest:
		return "nextest.cargo-nextest"
	case stackDotNet:
		return "Microsoft.DotNet.SDK.10"
	case stackGo:
		return "GoLang.Go"
	case stackJust:
		return "Casey.Just"
	case stackNode:
		return "OpenJS.NodeJS.LTS"
	case stackPython:
		return "Python.Python.<family selected by stack>"
	case stackRustMSVC:
		return "Rustlang.Rustup"
	case stackZig:
		return "zig.zig"
	default:
		return ""
	}
}

func inspectEffectivePlanScripts(ctx context.Context, provisioning provisioningPlan, userScriptExists bool) ([]workspacePlan, []projectStack, error) {
	temporary, err := os.MkdirTemp("", "herdr-sandbox-plan-")
	if err != nil {
		return nil, nil, fmt.Errorf("create temporary plan inspection directory: %w", err)
	}
	defer os.RemoveAll(temporary)
	projectsDirectory := filepath.Join(temporary, "projects")
	if err := os.Mkdir(projectsDirectory, 0o700); err != nil {
		return nil, nil, fmt.Errorf("create temporary project inspection directory: %w", err)
	}
	userData := append([]byte(nil), defaultUserProvisioningScript...)
	if userScriptExists {
		userData, err = readProvisioningScript(provisioning.UserScript, "user provisioning script", maximumUserScriptSize)
		if err != nil {
			return nil, nil, err
		}
	}
	userPath := filepath.Join(temporary, userProvisioningName)
	if err := os.WriteFile(userPath, userData, 0o600); err != nil {
		return nil, nil, fmt.Errorf("write temporary user inspection input: %w", err)
	}
	for _, workspace := range provisioning.Workspaces {
		data, err := readProvisioningScript(workspace.ProvisioningPath, "project provisioning script", maximumProjectScriptSize)
		if err != nil {
			return nil, nil, err
		}
		if err := os.WriteFile(filepath.Join(projectsDirectory, workspace.Name+".ps1"), data, 0o600); err != nil {
			return nil, nil, fmt.Errorf("write temporary project inspection input: %w", err)
		}
	}
	inspected, userStacks, err := inspectProjectProvisioningPlan(ctx, temporary, userPath, projectsDirectory, provisioning.Workspaces)
	if err != nil {
		return nil, nil, err
	}
	if err := os.RemoveAll(temporary); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, nil, fmt.Errorf("remove temporary plan inspection directory: %w", err)
	}
	return inspected, userStacks, nil
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
