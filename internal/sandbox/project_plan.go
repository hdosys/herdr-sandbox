package sandbox

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"
)

const (
	projectProvisioningPlanSchema        = 2
	projectProvisioningPlanScriptName    = "inspect-project-provisioning.ps1"
	projectProvisioningPlanTimeout       = 30 * time.Second
	maximumProjectProvisioningPlanOutput = 64 * 1024
)

//go:embed assets/project-provisioning-plan.ps1
var projectProvisioningPlanScript []byte

type projectStack string

const (
	stackAndroid       projectStack = "android"
	stackBun           projectStack = "bun"
	stackCargoNextest  projectStack = "cargo-nextest"
	stackCpp           projectStack = "cpp"
	stackDotNet        projectStack = "dotnet"
	stackGitSH         projectStack = "git-sh"
	stackGo            projectStack = "go"
	stackHandy         projectStack = "handy"
	stackJava          projectStack = "java"
	stackJust          projectStack = "just"
	stackNode          projectStack = "node"
	stackNSIS          projectStack = "nsis"
	stackPlaywrightCLI projectStack = "playwright-cli"
	stackPython        projectStack = "python"
	stackRustMSVC      projectStack = "rust-msvc"
	stackTradingView   projectStack = "tradingview"
	stackUV            projectStack = "uv"
	stackZig           projectStack = "zig"
)

type projectProvisioningPlan struct {
	SchemaVersion int                            `json:"schemaVersion"`
	UserStacks    []projectStack                 `json:"userStacks"`
	Projects      []projectProvisioningPlanEntry `json:"projects"`
}

type projectProvisioningPlanEntry struct {
	Name   string         `json:"name"`
	Stacks []projectStack `json:"stacks"`
}

func (stack projectStack) valid() bool {
	switch stack {
	case stackAndroid, stackBun, stackCargoNextest, stackCpp, stackDotNet, stackGitSH, stackGo, stackHandy, stackJava, stackJust, stackNode, stackNSIS, stackPlaywrightCLI, stackPython, stackRustMSVC, stackTradingView, stackUV, stackZig:
		return true
	default:
		return false
	}
}

func inspectProjectProvisioningPlan(ctx context.Context, runDirectory, userScript, projectsDirectory string, workspaces []workspacePlan) ([]workspacePlan, []projectStack, error) {
	if !filepath.IsAbs(runDirectory) || !filepath.IsAbs(userScript) || !filepath.IsAbs(projectsDirectory) {
		return nil, nil, errors.New("provisioning inspection requires absolute paths")
	}
	if len(workspaces) > 16 {
		return nil, nil, fmt.Errorf("project provisioning inspection workspace count is invalid: %d", len(workspaces))
	}
	powerShell, err := windowsPowerShellExecutable()
	if err != nil {
		return nil, nil, err
	}
	scriptPath := filepath.Join(runDirectory, projectProvisioningPlanScriptName)
	if err := os.WriteFile(scriptPath, projectProvisioningPlanScript, 0o600); err != nil {
		return nil, nil, fmt.Errorf("write provisioning inspection script: %w", err)
	}

	inspectionContext, cancel := context.WithTimeout(ctx, projectProvisioningPlanTimeout)
	defer cancel()
	command := hiddenCommandContext(inspectionContext, powerShell,
		"-NoLogo", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass",
		"-File", scriptPath, "-UserProvisioningPath", userScript, "-ProjectsDirectory", projectsDirectory,
	)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		if inspectionContext.Err() != nil {
			return nil, nil, fmt.Errorf("inspect provisioning scripts: %w", inspectionContext.Err())
		}
		return nil, nil, fmt.Errorf("inspect provisioning scripts: %w: %s", err, boundedText(stderr.Bytes()))
	}
	if stdout.Len() == 0 || stdout.Len() > maximumProjectProvisioningPlanOutput {
		return nil, nil, fmt.Errorf("provisioning inspection output size is invalid: %d", stdout.Len())
	}

	return decodeProjectProvisioningPlan(stdout.Bytes(), workspaces)
}

func decodeProjectProvisioningPlan(data []byte, workspaces []workspacePlan) ([]workspacePlan, []projectStack, error) {
	if err := validateExactJSONObjectShape(data, "provisioning inspection", []string{"schemaVersion", "userStacks", "projects"}); err != nil {
		return nil, nil, fmt.Errorf("decode provisioning inspection output: %w", err)
	}
	var decoded projectProvisioningPlan
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&decoded); err != nil {
		return nil, nil, fmt.Errorf("decode provisioning inspection output: %w", err)
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return nil, nil, fmt.Errorf("decode provisioning inspection output: %w", err)
	}
	expectedProjects := 0
	for _, workspace := range workspaces {
		if workspace.ProvisioningPath != "" {
			expectedProjects++
		}
	}
	if decoded.SchemaVersion != projectProvisioningPlanSchema || len(decoded.Projects) != expectedProjects {
		return nil, nil, fmt.Errorf("provisioning inspection schema or project count is invalid")
	}
	userStacks, err := validateInspectedStacks(decoded.UserStacks, "user provisioning")
	if err != nil {
		return nil, nil, err
	}

	result := append([]workspacePlan(nil), workspaces...)
	indexes := make(map[string]int, len(result))
	for index := range result {
		indexes[result[index].Name] = index
		result[index].Stacks = nil
	}
	seenProjects := make(map[string]bool, len(result))
	for _, project := range decoded.Projects {
		index, found := indexes[project.Name]
		if !found || result[index].ProvisioningPath == "" || seenProjects[project.Name] {
			return nil, nil, fmt.Errorf("project provisioning inspection returned unexpected or duplicate project %q", project.Name)
		}
		seenProjects[project.Name] = true
		result[index].Stacks, err = validateInspectedStacks(project.Stacks, fmt.Sprintf("project %q", project.Name))
		if err != nil {
			return nil, nil, err
		}
	}
	for _, workspace := range result {
		if workspace.ProvisioningPath != "" && !seenProjects[workspace.Name] {
			return nil, nil, fmt.Errorf("project provisioning inspection omitted project %q", workspace.Name)
		}
	}
	return result, userStacks, nil
}

func validateInspectedStacks(stacks []projectStack, role string) ([]projectStack, error) {
	result := append([]projectStack(nil), stacks...)
	seen := make(map[projectStack]bool, len(result))
	for _, stack := range result {
		if !stack.valid() || seen[stack] {
			return nil, fmt.Errorf("provisioning inspection returned invalid or duplicate stack %q for %s", stack, role)
		}
		seen[stack] = true
	}
	sort.Slice(result, func(left, right int) bool { return result[left] < result[right] })
	return result, nil
}

func validateGitShellPackageRequirement(workspaces []workspacePlan, userStacks []projectStack, packages wingetPackagePlan) error {
	requiresGitShell := func(stacks []projectStack) bool {
		for _, stack := range stacks {
			if stack == stackGitSH {
				return true
			}
		}
		return false
	}
	required := requiresGitShell(userStacks)
	for _, workspace := range workspaces {
		required = required || requiresGitShell(workspace.Stacks)
	}
	if required && !packages.enabled(packageGit) {
		return errors.New("the selected project requires Git for Windows sh.exe; restore Base package Git.Git by removing it from wingetPackages.remove")
	}
	return nil
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("unexpected trailing JSON value")
		}
		return err
	}
	return nil
}
