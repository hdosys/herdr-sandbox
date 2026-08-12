package sandbox

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	stackHandyPreset    projectStack = "handy"
	stackHerdrPreset    projectStack = "herdr"
	stackPythonAIPreset projectStack = "python-ai"
)

var allProjectInitStacks = []projectStack{
	stackBun,
	stackCargoNextest,
	stackCpp,
	stackDotNet,
	stackGo,
	stackJava,
	stackJust,
	stackNode,
	stackNSIS,
	stackPlaywrightCLI,
	stackPython,
	stackRustMSVC,
	stackTradingView,
	stackUV,
	stackZig,
}

// ProjectInitResult describes the one project profile created by InitializeProject.
type ProjectInitResult struct {
	Path   string
	Stacks []string
}

// InitializeProject writes one direct-call project profile and never replaces an
// existing profile. Stack selection is explicit; repository contents are not
// guessed or executed.
func InitializeProject(startDirectory string, requested []string) (ProjectInitResult, error) {
	stacks, labels, err := normalizeProjectInitStacks(requested)
	if err != nil {
		return ProjectInitResult{}, err
	}
	if startDirectory == "" {
		startDirectory, err = os.Getwd()
		if err != nil {
			return ProjectInitResult{}, fmt.Errorf("resolve project directory: %w", err)
		}
	}
	startDirectory, err = filepath.Abs(startDirectory)
	if err != nil {
		return ProjectInitResult{}, fmt.Errorf("resolve project directory: %w", err)
	}
	projectDirectory, err := canonicalMappedDirectory(startDirectory)
	if err != nil {
		return ProjectInitResult{}, fmt.Errorf("validate project directory: %w", err)
	}
	if _, existing, found, findErr := findProjectProvisioning(projectDirectory); findErr != nil {
		return ProjectInitResult{}, findErr
	} else if found {
		return ProjectInitResult{}, fmt.Errorf("project provisioning profile already exists and was not changed: %s", existing)
	}

	contents, err := renderProjectProvisioningProfile(stacks)
	if err != nil {
		return ProjectInitResult{}, err
	}
	configurationDirectory := filepath.Join(projectDirectory, projectConfigurationName)
	createdDirectory := false
	if info, statErr := os.Lstat(configurationDirectory); errors.Is(statErr, os.ErrNotExist) {
		if mkdirErr := os.Mkdir(configurationDirectory, 0o700); mkdirErr != nil {
			return ProjectInitResult{}, fmt.Errorf("create project configuration directory: %w", mkdirErr)
		}
		createdDirectory = true
	} else if statErr != nil {
		return ProjectInitResult{}, fmt.Errorf("inspect project configuration directory: %w", statErr)
	} else {
		reparse, reparseErr := fileInfoIsReparsePoint(info)
		if reparseErr != nil {
			return ProjectInitResult{}, fmt.Errorf("inspect project configuration reparse state: %w", reparseErr)
		}
		if reparse || !info.IsDir() {
			return ProjectInitResult{}, errors.New("project configuration path is not a regular non-reparse directory")
		}
	}
	if err := rejectMappedPathReparsePoints(configurationDirectory); err != nil {
		if createdDirectory {
			_ = os.Remove(configurationDirectory)
		}
		return ProjectInitResult{}, fmt.Errorf("validate project configuration directory: %w", err)
	}

	path := filepath.Join(configurationDirectory, projectProvisioningName)
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if createdDirectory {
			_ = os.Remove(configurationDirectory)
		}
		if errors.Is(err, os.ErrExist) {
			return ProjectInitResult{}, fmt.Errorf("project provisioning profile already exists and was not changed: %s", path)
		}
		return ProjectInitResult{}, fmt.Errorf("create project provisioning profile: %w", err)
	}
	written := false
	defer func() {
		_ = file.Close()
		if !written {
			_ = os.Remove(path)
			if createdDirectory {
				_ = os.Remove(configurationDirectory)
			}
		}
	}()
	if _, err := file.Write(contents); err != nil {
		return ProjectInitResult{}, fmt.Errorf("write project provisioning profile: %w", err)
	}
	if err := file.Sync(); err != nil {
		return ProjectInitResult{}, fmt.Errorf("sync project provisioning profile: %w", err)
	}
	if err := file.Close(); err != nil {
		return ProjectInitResult{}, fmt.Errorf("close project provisioning profile: %w", err)
	}
	written = true
	return ProjectInitResult{Path: path, Stacks: labels}, nil
}

func normalizeProjectInitStacks(requested []string) ([]projectStack, []string, error) {
	if len(requested) == 0 {
		return nil, nil, errors.New("select at least one stack: all, cpp, dotnet, go, handy, herdr, java, node, nsis, playwright-cli, python, python-ai, rust, tradingview, or zig")
	}
	for _, value := range requested {
		if strings.EqualFold(strings.TrimSpace(value), "all") {
			if len(requested) != 1 {
				return nil, nil, errors.New("stack \"all\" already includes every standalone technology and tool stack")
			}
			return append([]projectStack(nil), allProjectInitStacks...), []string{"all"}, nil
		}
	}
	aliases := map[string]projectStack{
		"cpp":            stackCpp,
		"dotnet":         stackDotNet,
		"go":             stackGo,
		"handy":          stackHandyPreset,
		"herdr":          stackHerdrPreset,
		"java":           stackJava,
		"node":           stackNode,
		"nsis":           stackNSIS,
		"playwright-cli": stackPlaywrightCLI,
		"python":         stackPython,
		"python-ai":      stackPythonAIPreset,
		"rust":           stackRustMSVC,
		"tradingview":    stackTradingView,
		"zig":            stackZig,
	}
	labelsByStack := map[projectStack]string{stackRustMSVC: "rust"}
	result := make([]projectStack, 0, len(requested))
	seen := make(map[projectStack]bool, len(requested))
	for _, value := range requested {
		name := strings.ToLower(strings.TrimSpace(value))
		stack, found := aliases[name]
		if !found {
			return nil, nil, fmt.Errorf("unknown stack %q; choose all, cpp, dotnet, go, handy, herdr, java, node, nsis, playwright-cli, python, python-ai, rust, tradingview, or zig", value)
		}
		if seen[stack] {
			return nil, nil, fmt.Errorf("stack %q was selected more than once", name)
		}
		seen[stack] = true
		result = append(result, stack)
	}
	if seen[stackHerdrPreset] {
		for _, included := range []projectStack{stackPython, stackRustMSVC, stackZig} {
			if seen[included] {
				label := string(included)
				if included == stackRustMSVC {
					label = "rust"
				}
				return nil, nil, fmt.Errorf("stack %q already includes stack %q", stackHerdrPreset, label)
			}
		}
	}
	if seen[stackHandyPreset] && seen[stackRustMSVC] {
		return nil, nil, fmt.Errorf("stack %q already includes stack %q", stackHandyPreset, "rust")
	}
	if seen[stackHandyPreset] && seen[stackHerdrPreset] {
		return nil, nil, fmt.Errorf("stacks %q and %q both include stacks %q and %q", stackHandyPreset, stackHerdrPreset, "bun", "rust")
	}
	if seen[stackPythonAIPreset] && seen[stackPython] {
		return nil, nil, fmt.Errorf("stack %q already includes stack %q", stackPythonAIPreset, stackPython)
	}
	if seen[stackHerdrPreset] && seen[stackPythonAIPreset] {
		return nil, nil, fmt.Errorf("stacks %q and %q both include stack %q", stackHerdrPreset, stackPythonAIPreset, stackPython)
	}
	sort.Slice(result, func(left, right int) bool { return result[left] < result[right] })
	labels := make([]string, len(result))
	for index, stack := range result {
		label := string(stack)
		if alias := labelsByStack[stack]; alias != "" {
			label = alias
		}
		labels[index] = label
	}
	return result, labels, nil
}

func renderProjectProvisioningProfile(stacks []projectStack) ([]byte, error) {
	if len(stacks) == 0 {
		return nil, errors.New("project profile requires at least one stack")
	}
	lines := []string{
		"param(",
		"    [Parameter(Mandatory = $true)]",
		"    [string]$ProjectDirectory",
		")",
		"",
		"$ErrorActionPreference = 'Stop'",
		"Set-StrictMode -Version 2.0",
		"",
	}
	for _, stack := range stacks {
		var call string
		switch stack {
		case stackBun:
			call = "Install-BunStack"
		case stackCargoNextest:
			call = "Install-CargoNextest"
		case stackCpp:
			call = "Install-CppStack"
		case stackDotNet:
			call = "Install-DotNetStack"
		case stackGo:
			call = "Install-GoStack -ProjectDirectory $ProjectDirectory"
		case stackHandyPreset:
			call = "Install-HandyStack -ProjectDirectory $ProjectDirectory"
		case stackHerdrPreset:
			call = "Install-HerdrStack -ProjectDirectory $ProjectDirectory"
		case stackJava:
			call = "Install-JavaStack"
		case stackJust:
			call = "Install-Just"
		case stackNode:
			call = "Install-NodeStack"
		case stackNSIS:
			call = "Install-NSISStack"
		case stackPlaywrightCLI:
			call = "Install-PlaywrightCLIStack"
		case stackPython:
			call = "Install-PythonStack"
		case stackPythonAIPreset:
			call = "Install-PythonAIStack"
		case stackRustMSVC:
			call = "Install-RustMSVCStack -ProjectDirectory $ProjectDirectory"
		case stackTradingView:
			call = "Install-TradingViewStack"
		case stackUV:
			call = "Install-Uv"
		case stackZig:
			call = "Install-ZigStack"
		default:
			return nil, fmt.Errorf("stack %q cannot be rendered by project init", stack)
		}
		lines = append(lines, call)
	}
	return []byte(strings.Join(lines, "\n") + "\n"), nil
}
