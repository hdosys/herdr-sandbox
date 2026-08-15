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
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	projectProvisioningPlanSchema        = 3
	projectProvisioningPlanScriptName    = "inspect-project-provisioning.ps1"
	projectProvisioningPlanTimeout       = 30 * time.Second
	maximumProjectProvisioningPlanOutput = 64 * 1024
	maximumProjectPackageLockSize        = 4 * 1024 * 1024
	projectPlaywrightLockSource          = "node-project-lock"
	toolVersionPlanFileName              = "tool-versions.json"
	toolVersionPlanSchema                = 2
	toolVersionSourceExplicit            = "explicit-provisioning"
	toolVersionSourceSelectedProject     = "explicit-project-version"
	toolVersionSourceProject             = "project-version-file"
	toolVersionSourceDefault             = "stack-default"
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
	stackNushell       projectStack = "nushell"
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
	UserTools     []projectToolRequirement       `json:"userTools"`
	Projects      []projectProvisioningPlanEntry `json:"projects"`
}

type projectProvisioningPlanEntry struct {
	Name   string                   `json:"name"`
	Stacks []projectStack           `json:"stacks"`
	Tools  []projectToolRequirement `json:"tools"`
}

type projectToolRequirement struct {
	Tool             string `json:"tool"`
	Version          string `json:"version"`
	Series           string `json:"series"`
	Source           string `json:"source"`
	ProjectDirectory string `json:"projectDirectory"`
}

type resolvedToolVersion struct {
	Tool    string   `json:"tool"`
	Version string   `json:"version"`
	Series  string   `json:"series"`
	Source  string   `json:"source"`
	Owners  []string `json:"owners"`
}

type projectProvisioningInspection struct {
	Workspaces   []workspacePlan
	UserStacks   []projectStack
	ToolVersions []resolvedToolVersion
}

type toolVersionPlan struct {
	SchemaVersion int                   `json:"schemaVersion"`
	Tools         []resolvedToolVersion `json:"tools"`
}

var (
	projectToolTokenPattern      = regexp.MustCompile(`^[A-Za-z0-9@][A-Za-z0-9._+@/-]{0,127}$`)
	projectToolValuePattern      = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]{0,127}$`)
	stableSemanticVersionPattern = regexp.MustCompile(`^(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)$`)
	projectToolNames             = map[string]string{
		"@ferroxlabs/tvcontrol":          "@ferroxlabs/tvcontrol",
		"@playwright/cli":                "@playwright/cli",
		"astral-sh.uv":                   "astral-sh.uv",
		"casey.just":                     "Casey.Just",
		"golang.go":                      "GoLang.Go",
		"khronosgroup.vulkansdk":         "KhronosGroup.VulkanSDK",
		"kitware.cmake":                  "Kitware.CMake",
		"microsoft.dotnet.sdk.10":        "Microsoft.DotNet.SDK.10",
		"microsoft.edgewebview2runtime":  "Microsoft.EdgeWebView2Runtime",
		"microsoft.openjdk.25":           "Microsoft.OpenJDK.25",
		"nextest.cargo-nextest":          "nextest.cargo-nextest",
		"nsis.nsis":                      "NSIS.NSIS",
		"nushell.nushell":                "Nushell.Nushell",
		"openjs.nodejs.lts":              "OpenJS.NodeJS.LTS",
		"oven-sh.bun":                    "Oven-sh.Bun",
		"playwright":                     "playwright",
		"python":                         "Python",
		"rustlang.rustup":                "Rustlang.Rustup",
		"rust-toolchain":                 "rust-toolchain",
		"tradingview.tradingviewdesktop": "TradingView.TradingViewDesktop",
		"zig.zig":                        "zig.zig",
	}
)

func (stack projectStack) valid() bool {
	switch stack {
	case stackAndroid, stackBun, stackCargoNextest, stackCpp, stackDotNet, stackGitSH, stackGo, stackHandy, stackJava, stackJust, stackNode, stackNSIS, stackNushell, stackPlaywrightCLI, stackPython, stackRustMSVC, stackTradingView, stackUV, stackZig:
		return true
	default:
		return false
	}
}

func inspectProjectProvisioningPlan(ctx context.Context, runDirectory, userScript, projectsDirectory string, workspaces []workspacePlan) (projectProvisioningInspection, error) {
	if !filepath.IsAbs(runDirectory) || !filepath.IsAbs(userScript) || !filepath.IsAbs(projectsDirectory) {
		return projectProvisioningInspection{}, errors.New("provisioning inspection requires absolute paths")
	}
	if len(workspaces) > 16 {
		return projectProvisioningInspection{}, fmt.Errorf("project provisioning inspection workspace count is invalid: %d", len(workspaces))
	}
	powerShell, err := windowsPowerShellExecutable()
	if err != nil {
		return projectProvisioningInspection{}, err
	}
	scriptPath := filepath.Join(runDirectory, projectProvisioningPlanScriptName)
	if err := os.WriteFile(scriptPath, projectProvisioningPlanScript, 0o600); err != nil {
		return projectProvisioningInspection{}, fmt.Errorf("write provisioning inspection script: %w", err)
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
			return projectProvisioningInspection{}, fmt.Errorf("inspect provisioning scripts: %w", inspectionContext.Err())
		}
		return projectProvisioningInspection{}, fmt.Errorf("inspect provisioning scripts: %w: %s", err, boundedText(stderr.Bytes()))
	}
	if stdout.Len() == 0 || stdout.Len() > maximumProjectProvisioningPlanOutput {
		return projectProvisioningInspection{}, fmt.Errorf("provisioning inspection output size is invalid: %d", stdout.Len())
	}

	inspection, err := decodeProjectProvisioningPlan(stdout.Bytes(), workspaces)
	if err != nil {
		return projectProvisioningInspection{}, err
	}
	return inspection, nil
}

func decodeProjectProvisioningPlan(data []byte, workspaces []workspacePlan) (projectProvisioningInspection, error) {
	if err := validateExactJSONObjectShape(data, "provisioning inspection", []string{"schemaVersion", "userStacks", "userTools", "projects"}); err != nil {
		return projectProvisioningInspection{}, fmt.Errorf("decode provisioning inspection output: %w", err)
	}
	var raw struct {
		UserTools []json.RawMessage `json:"userTools"`
		Projects  []struct {
			Name   string            `json:"name"`
			Stacks []json.RawMessage `json:"stacks"`
			Tools  []json.RawMessage `json:"tools"`
		} `json:"projects"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return projectProvisioningInspection{}, fmt.Errorf("decode provisioning inspection raw fields: %w", err)
	}
	for index, tool := range raw.UserTools {
		if err := validateExactJSONObjectShape(tool, fmt.Sprintf("userTools[%d]", index), []string{"tool", "version", "series", "source", "projectDirectory"}); err != nil {
			return projectProvisioningInspection{}, fmt.Errorf("decode provisioning inspection output: %w", err)
		}
	}
	var rawTop struct {
		Projects []json.RawMessage `json:"projects"`
	}
	if err := json.Unmarshal(data, &rawTop); err != nil {
		return projectProvisioningInspection{}, err
	}
	if len(rawTop.Projects) != len(raw.Projects) {
		return projectProvisioningInspection{}, fmt.Errorf("decode provisioning inspection output: raw project count is invalid")
	}
	for projectIndex, project := range raw.Projects {
		if err := validateExactJSONObjectShape(rawTop.Projects[projectIndex], fmt.Sprintf("project %q", project.Name), []string{"name", "stacks", "tools"}); err != nil {
			return projectProvisioningInspection{}, fmt.Errorf("decode provisioning inspection output: %w", err)
		}
		for index, tool := range project.Tools {
			if err := validateExactJSONObjectShape(tool, fmt.Sprintf("project %q tools[%d]", project.Name, index), []string{"tool", "version", "series", "source", "projectDirectory"}); err != nil {
				return projectProvisioningInspection{}, fmt.Errorf("decode provisioning inspection output: %w", err)
			}
		}
	}
	var decoded projectProvisioningPlan
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&decoded); err != nil {
		return projectProvisioningInspection{}, fmt.Errorf("decode provisioning inspection output: %w", err)
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return projectProvisioningInspection{}, fmt.Errorf("decode provisioning inspection output: %w", err)
	}
	expectedProjects := 0
	for _, workspace := range workspaces {
		if workspace.ProvisioningPath != "" {
			expectedProjects++
		}
	}
	if decoded.SchemaVersion != projectProvisioningPlanSchema || len(decoded.Projects) != expectedProjects {
		return projectProvisioningInspection{}, fmt.Errorf("provisioning inspection schema or project count is invalid")
	}
	userStacks, err := validateInspectedStacks(decoded.UserStacks, "user provisioning")
	if err != nil {
		return projectProvisioningInspection{}, err
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
			return projectProvisioningInspection{}, fmt.Errorf("project provisioning inspection returned unexpected or duplicate project %q", project.Name)
		}
		seenProjects[project.Name] = true
		result[index].Stacks, err = validateInspectedStacks(project.Stacks, fmt.Sprintf("project %q", project.Name))
		if err != nil {
			return projectProvisioningInspection{}, err
		}
	}
	for _, workspace := range result {
		if workspace.ProvisioningPath != "" && !seenProjects[workspace.Name] {
			return projectProvisioningInspection{}, fmt.Errorf("project provisioning inspection omitted project %q", workspace.Name)
		}
	}
	toolVersions, err := mergeProjectToolVersions(decoded.UserTools, decoded.Projects, result)
	if err != nil {
		return projectProvisioningInspection{}, err
	}
	return projectProvisioningInspection{Workspaces: result, UserStacks: userStacks, ToolVersions: toolVersions}, nil
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

func mergeProjectToolVersions(userTools []projectToolRequirement, projects []projectProvisioningPlanEntry, workspaces []workspacePlan) ([]resolvedToolVersion, error) {
	type ownedRequirement struct {
		projectToolRequirement
		owner string
	}
	type deferredProjectVersion struct {
		kind         string
		ownedIndex   int
		projectName  string
		projectRoot  string
		projectOwner string
	}
	owned := make([]ownedRequirement, 0, len(userTools)+len(projects)*2)
	for _, requirement := range userTools {
		if requirement.ProjectDirectory == "$ProjectDirectory" {
			return nil, fmt.Errorf("user.ps1 (%s) cannot use $ProjectDirectory", requirement.Source)
		}
		owned = append(owned, ownedRequirement{projectToolRequirement: requirement, owner: "user.ps1 (" + requirement.Source + ")"})
	}
	deferred := make([]deferredProjectVersion, 0, len(projects)*2)
	workspaceByName := make(map[string]workspacePlan, len(workspaces))
	for _, workspace := range workspaces {
		workspaceByName[workspace.Name] = workspace
	}
	for _, project := range projects {
		for _, requirement := range project.Tools {
			if requirement.Source == projectPlaywrightLockSource {
				_, found := workspaceByName[project.Name]
				if !found || requirement.Tool != "playwright" || requirement.Version != "" || requirement.Series != "" || requirement.ProjectDirectory != "" {
					return nil, fmt.Errorf("project %q returned an invalid Playwright package-lock requirement", project.Name)
				}
			}
			ownedIndex := len(owned)
			owned = append(owned, ownedRequirement{projectToolRequirement: requirement, owner: fmt.Sprintf("project %q (%s)", project.Name, requirement.Source)})
			if requirement.Source == projectPlaywrightLockSource {
				deferred = append(deferred, deferredProjectVersion{kind: projectPlaywrightLockSource, ownedIndex: ownedIndex, projectName: project.Name})
			}
			if strings.EqualFold(requirement.Tool, "rust-toolchain") {
				workspace, found := workspaceByName[project.Name]
				if !found {
					return nil, fmt.Errorf("Rust project workspace is missing for project %q", project.Name)
				}
				root := requirement.ProjectDirectory
				if root == "$ProjectDirectory" {
					root = workspace.HostDirectory
				} else if root != "" && !hostPathContains(workspace.HostDirectory, root) {
					return nil, fmt.Errorf("Rust project directory for project %q must stay within its mapped workspace: %s", project.Name, root)
				}
				if requirement.Source == "handy" && root != "" {
					root = filepath.Join(root, "src-tauri")
				}
				deferred = append(deferred, deferredProjectVersion{kind: "rust-toolchain.toml", ownedIndex: -1, projectName: project.Name, projectRoot: root, projectOwner: fmt.Sprintf("project %q (rust-toolchain.toml)", project.Name)})
			}
		}
	}
	explicitTools := make(map[string]bool)
	for _, requirement := range owned {
		if requirement.Source != projectPlaywrightLockSource && (requirement.Version != "" || requirement.Series != "") {
			explicitTools[strings.ToLower(requirement.Tool)] = true
		}
	}
	for _, projectVersion := range deferred {
		var tool string
		switch projectVersion.kind {
		case projectPlaywrightLockSource:
			tool = "playwright"
		case "rust-toolchain.toml":
			tool = "rust-toolchain"
		default:
			return nil, fmt.Errorf("unsupported deferred project version source: %s", projectVersion.kind)
		}
		if projectVersion.kind == "rust-toolchain.toml" && explicitTools[strings.ToLower(tool)] {
			continue
		}
		switch projectVersion.kind {
		case projectPlaywrightLockSource:
			workspace := workspaceByName[projectVersion.projectName]
			version, err := readProjectPlaywrightVersion(workspace.HostDirectory)
			if err != nil {
				return nil, fmt.Errorf("resolve Playwright package-lock version for project %q: %w", projectVersion.projectName, err)
			}
			owned[projectVersion.ownedIndex].Version = version
		case "rust-toolchain.toml":
			toolchain, found, err := readProjectRustToolchain(projectVersion.projectRoot)
			if err != nil {
				return nil, fmt.Errorf("resolve Rust toolchain for project %q: %w", projectVersion.projectName, err)
			}
			if found {
				owned = append(owned, ownedRequirement{projectToolRequirement: projectToolRequirement{Tool: "rust-toolchain", Version: toolchain, Source: "rust-toolchain.toml"}, owner: projectVersion.projectOwner})
			}
		}
	}

	type mergedValues struct {
		versions map[string][]string
		series   map[string][]string
	}
	type mergedTool struct {
		name                    string
		explicit                mergedValues
		project                 mergedValues
		hasDirectExplicit       bool
		hasSelectedProjectValue bool
		owners                  map[string]bool
	}
	merged := make(map[string]*mergedTool)
	for _, requirement := range owned {
		identity := strings.ToLower(requirement.Tool)
		canonical, found := projectToolNames[identity]
		if !found || requirement.Tool != canonical || !projectToolTokenPattern.MatchString(requirement.Tool) ||
			(requirement.Version != "" && !projectToolValuePattern.MatchString(requirement.Version)) ||
			(requirement.Series != "" && !projectToolValuePattern.MatchString(requirement.Series)) ||
			requirement.Source == "" || len(requirement.Source) > 64 ||
			(requirement.ProjectDirectory != "" && !strings.EqualFold(requirement.Tool, "rust-toolchain")) {
			return nil, fmt.Errorf("provisioning inspection returned invalid tool requirement for %s", requirement.owner)
		}
		entry := merged[identity]
		if entry == nil {
			entry = &mergedTool{
				name:     canonical,
				explicit: mergedValues{versions: map[string][]string{}, series: map[string][]string{}},
				project:  mergedValues{versions: map[string][]string{}, series: map[string][]string{}},
				owners:   map[string]bool{},
			}
			merged[identity] = entry
		}
		entry.owners[requirement.owner] = true
		values := &entry.explicit
		if requirement.Source == "rust-toolchain.toml" {
			values = &entry.project
		}
		if requirement.Version != "" || requirement.Series != "" {
			if requirement.Source == projectPlaywrightLockSource {
				entry.hasSelectedProjectValue = true
			} else if requirement.Source != "rust-toolchain.toml" {
				entry.hasDirectExplicit = true
			}
		}
		if requirement.Version != "" {
			values.versions[requirement.Version] = append(values.versions[requirement.Version], requirement.owner)
		}
		if requirement.Series != "" {
			values.series[requirement.Series] = append(values.series[requirement.Series], requirement.owner)
		}
		if identity == "python" && requirement.Version != "" {
			parts := strings.Split(requirement.Version, ".")
			if len(parts) < 3 {
				return nil, fmt.Errorf("Python version %q from %s is invalid", requirement.Version, requirement.owner)
			}
			derived := parts[0] + "." + parts[1]
			values.series[derived] = append(values.series[derived], requirement.owner)
		}
	}

	identities := make([]string, 0, len(merged))
	for identity := range merged {
		identities = append(identities, identity)
	}
	sort.Strings(identities)
	result := make([]resolvedToolVersion, 0, len(identities))
	for _, identity := range identities {
		entry := merged[identity]
		values := entry.explicit
		source := toolVersionSourceExplicit
		if !entry.hasDirectExplicit && entry.hasSelectedProjectValue {
			source = toolVersionSourceSelectedProject
		}
		if len(values.versions) == 0 && len(values.series) == 0 {
			values = entry.project
			source = toolVersionSourceProject
		}
		if len(values.versions) == 0 && len(values.series) == 0 {
			source = toolVersionSourceDefault
		}
		if len(values.versions) > 1 {
			kind := "versions"
			if source == toolVersionSourceProject {
				kind = "project-file versions"
			}
			return nil, toolVersionConflict(entry.name, kind, values.versions)
		}
		if len(values.series) > 1 {
			kind := "series"
			if source == toolVersionSourceProject {
				kind = "project-file series"
			}
			return nil, toolVersionConflict(entry.name, kind, values.series)
		}
		version := onlyRequirementValue(values.versions)
		series := onlyRequirementValue(values.series)
		owners := make([]string, 0, len(entry.owners))
		for owner := range entry.owners {
			owners = append(owners, owner)
		}
		sort.Strings(owners)
		result = append(result, resolvedToolVersion{Tool: entry.name, Version: version, Series: series, Source: source, Owners: owners})
	}
	return result, nil
}

func onlyRequirementValue(values map[string][]string) string {
	for value := range values {
		return value
	}
	return ""
}

func toolVersionConflict(tool, kind string, values map[string][]string) error {
	keys := make([]string, 0, len(values))
	for value := range values {
		keys = append(keys, value)
	}
	sort.Strings(keys)
	details := make([]string, 0, len(keys))
	for _, value := range keys {
		owners := append([]string(nil), values[value]...)
		sort.Strings(owners)
		details = append(details, fmt.Sprintf("%s by %s", value, strings.Join(owners, ", ")))
	}
	return fmt.Errorf("conflicting exact %s for tool %s: %s", kind, tool, strings.Join(details, "; "))
}

func readProjectRustToolchain(projectDirectory string) (string, bool, error) {
	if projectDirectory == "" {
		return "", false, nil
	}
	if !filepath.IsAbs(projectDirectory) {
		return "", false, fmt.Errorf("Rust project directory must be absolute: %s", projectDirectory)
	}
	if _, err := os.Stat(projectDirectory); errors.Is(err, os.ErrNotExist) {
		return "", false, nil
	} else if err != nil {
		return "", false, err
	}
	if err := rejectMappedPathReparsePoints(projectDirectory); err != nil {
		return "", false, fmt.Errorf("Rust project directory is unsafe: %w", err)
	}
	path := filepath.Join(projectDirectory, "rust-toolchain.toml")
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	reparse, err := fileInfoIsReparsePoint(info)
	if err != nil {
		return "", false, err
	}
	if reparse || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > 64*1024 {
		return "", false, fmt.Errorf("Rust toolchain file must be one bounded regular non-reparse file: %s", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false, err
	}
	matches := regexp.MustCompile(`(?m)^\s*channel\s*=\s*"([^"]+)"\s*$`).FindAllSubmatch(data, -1)
	if len(matches) != 1 || !regexp.MustCompile(`^(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)$`).Match(matches[0][1]) {
		return "", false, fmt.Errorf("Rust toolchain file must declare exactly one literal x.y.z channel: %s", path)
	}
	return string(matches[0][1]), true, nil
}

func readProjectPlaywrightVersion(projectDirectory string) (string, error) {
	if !filepath.IsAbs(projectDirectory) {
		return "", fmt.Errorf("project directory must be absolute: %s", projectDirectory)
	}
	frontendDirectory := filepath.Join(projectDirectory, "frontend")
	if err := rejectMappedPathReparsePoints(frontendDirectory); err != nil {
		return "", fmt.Errorf("frontend directory is unsafe: %w", err)
	}
	path := filepath.Join(frontendDirectory, "package-lock.json")
	data, err := readProvisioningScript(path, "project Playwright package lock", maximumProjectPackageLockSize)
	if err != nil {
		return "", err
	}
	var lock struct {
		LockfileVersion int                        `json:"lockfileVersion"`
		Packages        map[string]json.RawMessage `json:"packages"`
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(&lock); err != nil {
		return "", fmt.Errorf("decode package lock: %w", err)
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return "", fmt.Errorf("decode package lock: %w", err)
	}
	if lock.LockfileVersion != 3 || lock.Packages == nil {
		return "", errors.New("package lock must use npm lockfile version 3")
	}
	type packageIdentity struct {
		Version      string            `json:"version"`
		Dependencies map[string]string `json:"dependencies"`
	}
	readIdentity := func(name string) (packageIdentity, error) {
		raw, found := lock.Packages[name]
		if !found {
			return packageIdentity{}, fmt.Errorf("package lock is missing %s", name)
		}
		var identity packageIdentity
		if err := json.Unmarshal(raw, &identity); err != nil {
			return packageIdentity{}, fmt.Errorf("decode %s identity: %w", name, err)
		}
		return identity, nil
	}
	testPackage, err := readIdentity("node_modules/@playwright/test")
	if err != nil {
		return "", err
	}
	playwrightPackage, err := readIdentity("node_modules/playwright")
	if err != nil {
		return "", err
	}
	corePackage, err := readIdentity("node_modules/playwright-core")
	if err != nil {
		return "", err
	}
	version := testPackage.Version
	if !projectToolValuePattern.MatchString(version) || !stableSemanticVersionPattern.MatchString(version) ||
		testPackage.Dependencies["playwright"] != version || playwrightPackage.Version != version ||
		playwrightPackage.Dependencies["playwright-core"] != version || corePackage.Version != version {
		return "", errors.New("Playwright package-lock versions are missing or inconsistent")
	}
	return version, nil
}

func encodeToolVersionPlan(tools []resolvedToolVersion) ([]byte, error) {
	if err := validateResolvedToolVersionPlan(tools); err != nil {
		return nil, err
	}
	data, err := json.Marshal(toolVersionPlan{SchemaVersion: toolVersionPlanSchema, Tools: tools})
	if err != nil {
		return nil, fmt.Errorf("encode tool version plan: %w", err)
	}
	return append(data, '\n'), nil
}

func validateResolvedToolVersionPlan(tools []resolvedToolVersion) error {
	if len(tools) > 64 {
		return fmt.Errorf("resolved tool version count is invalid: %d", len(tools))
	}
	seen := make(map[string]bool, len(tools))
	previous := ""
	for _, tool := range tools {
		identity := strings.ToLower(tool.Tool)
		canonical, found := projectToolNames[identity]
		if !found || tool.Tool != canonical || seen[identity] || (previous != "" && previous >= identity) ||
			(tool.Version != "" && !projectToolValuePattern.MatchString(tool.Version)) ||
			(tool.Series != "" && !projectToolValuePattern.MatchString(tool.Series)) ||
			(tool.Source != toolVersionSourceExplicit && tool.Source != toolVersionSourceSelectedProject &&
				tool.Source != toolVersionSourceProject && tool.Source != toolVersionSourceDefault) ||
			len(tool.Owners) == 0 || len(tool.Owners) > 32 {
			return fmt.Errorf("resolved tool version is invalid: %s", tool.Tool)
		}
		seen[identity] = true
		previous = identity
		lastOwner := ""
		for _, owner := range tool.Owners {
			if owner == "" || len(owner) > 256 || strings.ContainsAny(owner, "\r\n\x00") || (lastOwner != "" && lastOwner >= owner) {
				return fmt.Errorf("resolved tool version owner is invalid for %s", tool.Tool)
			}
			lastOwner = owner
		}
	}
	return nil
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
