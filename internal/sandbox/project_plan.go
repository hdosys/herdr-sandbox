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
	projectProvisioningPlanSchema        = 1
	projectProvisioningPlanScriptName    = "inspect-project-provisioning.ps1"
	projectProvisioningPlanTimeout       = 30 * time.Second
	maximumProjectProvisioningPlanOutput = 64 * 1024
)

//go:embed assets/project-provisioning-plan.ps1
var projectProvisioningPlanScript []byte

type projectStack string

const (
	stackCargoNextest projectStack = "cargo-nextest"
	stackGo           projectStack = "go"
	stackJust         projectStack = "just"
	stackNode         projectStack = "node"
	stackPython       projectStack = "python"
	stackRustMSVC     projectStack = "rust-msvc"
	stackZig          projectStack = "zig"
)

type projectProvisioningPlan struct {
	SchemaVersion int                            `json:"schemaVersion"`
	Projects      []projectProvisioningPlanEntry `json:"projects"`
}

type projectProvisioningPlanEntry struct {
	Name   string         `json:"name"`
	Stacks []projectStack `json:"stacks"`
}

func (stack projectStack) valid() bool {
	switch stack {
	case stackCargoNextest, stackGo, stackJust, stackNode, stackPython, stackRustMSVC, stackZig:
		return true
	default:
		return false
	}
}

func inspectProjectProvisioningPlan(ctx context.Context, runDirectory, projectsDirectory string, workspaces []workspacePlan) ([]workspacePlan, error) {
	if !filepath.IsAbs(runDirectory) || !filepath.IsAbs(projectsDirectory) {
		return nil, errors.New("project provisioning inspection requires absolute directories")
	}
	if len(workspaces) == 0 || len(workspaces) > 16 {
		return nil, fmt.Errorf("project provisioning inspection workspace count is invalid: %d", len(workspaces))
	}
	powerShell, err := windowsPowerShellExecutable()
	if err != nil {
		return nil, err
	}
	scriptPath := filepath.Join(runDirectory, projectProvisioningPlanScriptName)
	if err := os.WriteFile(scriptPath, projectProvisioningPlanScript, 0o600); err != nil {
		return nil, fmt.Errorf("write project provisioning inspection script: %w", err)
	}

	inspectionContext, cancel := context.WithTimeout(ctx, projectProvisioningPlanTimeout)
	defer cancel()
	command := hiddenCommandContext(inspectionContext, powerShell,
		"-NoLogo", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass",
		"-File", scriptPath, "-ProjectsDirectory", projectsDirectory,
	)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		if inspectionContext.Err() != nil {
			return nil, fmt.Errorf("inspect project provisioning scripts: %w", inspectionContext.Err())
		}
		return nil, fmt.Errorf("inspect project provisioning scripts: %w: %s", err, boundedText(stderr.Bytes()))
	}
	if stdout.Len() == 0 || stdout.Len() > maximumProjectProvisioningPlanOutput {
		return nil, fmt.Errorf("project provisioning inspection output size is invalid: %d", stdout.Len())
	}

	return decodeProjectProvisioningPlan(stdout.Bytes(), workspaces)
}

func decodeProjectProvisioningPlan(data []byte, workspaces []workspacePlan) ([]workspacePlan, error) {
	var decoded projectProvisioningPlan
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&decoded); err != nil {
		return nil, fmt.Errorf("decode project provisioning inspection output: %w", err)
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return nil, fmt.Errorf("decode project provisioning inspection output: %w", err)
	}
	if decoded.SchemaVersion != projectProvisioningPlanSchema || len(decoded.Projects) != len(workspaces) {
		return nil, fmt.Errorf("project provisioning inspection schema or project count is invalid")
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
		if !found || seenProjects[project.Name] {
			return nil, fmt.Errorf("project provisioning inspection returned unexpected or duplicate project %q", project.Name)
		}
		seenProjects[project.Name] = true
		seenStacks := make(map[projectStack]bool, len(project.Stacks))
		for _, stack := range project.Stacks {
			if !stack.valid() || seenStacks[stack] {
				return nil, fmt.Errorf("project provisioning inspection returned invalid or duplicate stack %q for project %q", stack, project.Name)
			}
			seenStacks[stack] = true
			result[index].Stacks = append(result[index].Stacks, stack)
		}
		sort.Slice(result[index].Stacks, func(left, right int) bool {
			return result[index].Stacks[left] < result[index].Stacks[right]
		})
	}
	return result, nil
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
