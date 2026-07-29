package sandbox

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const (
	applicationName                    = "herdr-sandbox"
	projectConfigurationName           = ".herdr-sandbox"
	projectProvisioningName            = "provision.ps1"
	baseProvisioningName               = "base.ps1"
	stackProvisioningName              = "stacks.ps1"
	userProvisioningName               = "user.ps1"
	workspaceManifestName              = "workspaces.json"
	globalConfigurationName            = "config.json"
	guestWorkspacesDirectory           = `C:\Workspaces`
	baseProvisioningContract           = "# herdr-sandbox-base-contract: 33"
	stackProvisioningContract          = "# herdr-sandbox-stacks-contract: 4"
	userProvisioningContract           = "# herdr-sandbox-user-contract: 1"
	workspaceManifestSchema            = 1
	maximumBaseScriptSize              = 1024 * 1024
	maximumStackScriptSize             = 2 * 1024 * 1024
	maximumUserScriptSize              = 1024 * 1024
	maximumProjectScriptSize           = 1024 * 1024
	maximumWorkspaceDiscoveryEntries   = 4096
	maximumWorkspaceExcludePatterns    = 64
	maximumWorkspaceExcludePatternSize = 1024
)

var defaultUserProvisioningScript = []byte(userProvisioningContract + `
$ErrorActionPreference = 'Stop'
Set-StrictMode -Version 2.0

# Add idempotent global guest customization below. Prefer config.json for packages.
`)

var (
	workspaceNamePattern        = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)
	invalidWorkspaceNamePattern = regexp.MustCompile(`[^A-Za-z0-9._-]+`)
)

type workspacePlan struct {
	Name             string
	HostDirectory    string
	GuestDirectory   string
	ProvisioningPath string
	Active           bool
	Stacks           []projectStack
}

type guestWorkspaceManifest struct {
	SchemaVersion   int                           `json:"schemaVersion"`
	ActiveWorkspace string                        `json:"activeWorkspace"`
	Workspaces      []guestWorkspaceManifestEntry `json:"workspaces"`
}

type guestWorkspaceManifestEntry struct {
	Name      string `json:"name"`
	Directory string `json:"directory"`
}

func encodeGuestWorkspaceManifest(workspaces []workspacePlan, activeWorkspace string) ([]byte, error) {
	if len(workspaces) == 0 {
		return nil, errors.New("workspace manifest requires at least one workspace")
	}
	manifest := guestWorkspaceManifest{
		SchemaVersion:   workspaceManifestSchema,
		ActiveWorkspace: filepath.Clean(activeWorkspace),
		Workspaces:      make([]guestWorkspaceManifestEntry, 0, len(workspaces)),
	}
	activeFound := false
	seen := make(map[string]bool, len(workspaces))
	for _, workspace := range workspaces {
		if !workspaceNamePattern.MatchString(workspace.Name) {
			return nil, fmt.Errorf("workspace manifest name is invalid: %q", workspace.Name)
		}
		expectedDirectory := guestWorkspaceDirectory(workspace.Name)
		if !strings.EqualFold(filepath.Clean(workspace.GuestDirectory), expectedDirectory) {
			return nil, fmt.Errorf("workspace %q guest directory = %q, want %q", workspace.Name, workspace.GuestDirectory, expectedDirectory)
		}
		identity := strings.ToLower(workspace.Name)
		if seen[identity] {
			return nil, fmt.Errorf("workspace manifest contains duplicate name %q", workspace.Name)
		}
		seen[identity] = true
		if strings.EqualFold(expectedDirectory, manifest.ActiveWorkspace) {
			activeFound = true
		}
		manifest.Workspaces = append(manifest.Workspaces, guestWorkspaceManifestEntry{Name: workspace.Name, Directory: expectedDirectory})
	}
	if !activeFound {
		return nil, fmt.Errorf("active workspace is not selected: %s", activeWorkspace)
	}
	sort.Slice(manifest.Workspaces, func(left, right int) bool {
		return strings.ToLower(manifest.Workspaces[left].Name) < strings.ToLower(manifest.Workspaces[right].Name)
	})
	encoded, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode workspace manifest: %w", err)
	}
	return append(encoded, '\n'), nil
}

type provisioningPlan struct {
	BaseScript           string
	StackScript          string
	UserScript           string
	CacheDirectory       string
	MemoryMB             int
	Audio                bool
	Tailscale            bool
	CodingAgentSync      codingAgentSyncConfiguration
	PackageConfiguration wingetPackageConfiguration
	Packages             wingetPackagePlan
	WindowsTerminal      windowsTerminalConfiguration
	Workspaces           []workspacePlan
}

type globalConfiguration struct {
	CacheDirectory     string                           `json:"cacheDirectory"`
	MemoryMB           *int                             `json:"memoryMB,omitempty"`
	Audio              bool                             `json:"audio"`
	Tailscale          bool                             `json:"tailscale"`
	CodingAgentSync    codingAgentSyncConfiguration     `json:"codingAgentSync"`
	WingetPackages     wingetPackageConfiguration       `json:"wingetPackages"`
	WorkspaceDiscovery *workspaceDiscoveryConfiguration `json:"workspaceDiscovery,omitempty"`
	Workspaces         map[string]string                `json:"workspaces"`
}

type workspaceDiscoveryConfiguration struct {
	Root    string   `json:"root"`
	Exclude []string `json:"exclude"`
}

type codingAgentSyncConfiguration struct {
	OpenCode      bool `json:"opencode"`
	ClaudeCode    bool `json:"claudeCode"`
	Codex         bool `json:"codex"`
	GitHubCopilot bool `json:"githubCopilot"`
	Pi            bool `json:"pi"`
}

func defaultCodingAgentSyncConfiguration() codingAgentSyncConfiguration {
	return codingAgentSyncConfiguration{
		OpenCode:      true,
		ClaudeCode:    true,
		Codex:         true,
		GitHubCopilot: true,
		Pi:            true,
	}
}

func resolveProvisioning(startDirectory string) (provisioningPlan, error) {
	configurationRoot, err := os.UserConfigDir()
	if err != nil {
		return provisioningPlan{}, fmt.Errorf("resolve user configuration directory: %w", err)
	}
	if !filepath.IsAbs(configurationRoot) {
		return provisioningPlan{}, fmt.Errorf("user configuration directory is not absolute: %q", configurationRoot)
	}
	executable, err := os.Executable()
	if err != nil {
		return provisioningPlan{}, fmt.Errorf("resolve herdr-sandbox executable: %w", err)
	}
	plan, err := resolveProvisioningAt(startDirectory, filepath.Join(configurationRoot, applicationName), filepath.Dir(executable))
	if err != nil {
		return provisioningPlan{}, err
	}
	if err := validateBaseProvisioningContract(plan.BaseScript); err != nil {
		return provisioningPlan{}, err
	}
	if err := validateStackProvisioningContract(plan.StackScript); err != nil {
		return provisioningPlan{}, err
	}
	if err := validateUserProvisioningContract(plan.UserScript); err != nil {
		return provisioningPlan{}, err
	}
	plan.WindowsTerminal, err = detectHostWindowsTerminalConfiguration()
	if err != nil {
		return provisioningPlan{}, err
	}
	plan.Packages, err = resolveWingetPackagePlan(plan.PackageConfiguration, plan.WindowsTerminal)
	if err != nil {
		return provisioningPlan{}, err
	}
	if err := validateTailscalePackageSelection(plan.Tailscale, plan.Packages); err != nil {
		return provisioningPlan{}, err
	}
	if len(plan.Workspaces) == 0 {
		return provisioningPlan{}, errors.New("no workspace is selected; run `herdr-sandbox init --stack <name>` from a project containing the source to map, or add a workspace to %APPDATA%\\herdr-sandbox\\config.json")
	}
	return plan, nil
}

// ResolveEffectivePlan validates and reports the current provisioning selection
// without seeding configuration, creating run state, resolving host Herdr, or
// crossing the Sandbox boundary.
func ResolveEffectivePlan(ctx context.Context, startDirectory string) (EffectivePlan, error) {
	configurationRoot, err := os.UserConfigDir()
	if err != nil {
		return EffectivePlan{}, fmt.Errorf("resolve user configuration directory: %w", err)
	}
	if !filepath.IsAbs(configurationRoot) {
		return EffectivePlan{}, fmt.Errorf("user configuration directory is not absolute: %q", configurationRoot)
	}
	executable, err := os.Executable()
	if err != nil {
		return EffectivePlan{}, fmt.Errorf("resolve herdr-sandbox executable: %w", err)
	}
	globalRoot := filepath.Join(configurationRoot, applicationName)
	configurationPath := filepath.Join(globalRoot, globalConfigurationName)
	userScriptPath := filepath.Join(globalRoot, userProvisioningName)
	plan, err := resolveProvisioningReadOnlyAt(startDirectory, globalRoot, filepath.Dir(executable))
	if err != nil {
		return EffectivePlan{}, err
	}
	plan.WindowsTerminal, err = detectHostWindowsTerminalConfiguration()
	if err != nil {
		return EffectivePlan{}, err
	}
	plan.Packages, err = resolveWingetPackagePlan(plan.PackageConfiguration, plan.WindowsTerminal)
	if err != nil {
		return EffectivePlan{}, err
	}
	if err := validateTailscalePackageSelection(plan.Tailscale, plan.Packages); err != nil {
		return EffectivePlan{}, err
	}
	configurationExists, err := regularFileExists(configurationPath)
	if err != nil {
		return EffectivePlan{}, fmt.Errorf("inspect global configuration: %w", err)
	}
	userScriptExists, err := regularFileExists(userScriptPath)
	if err != nil {
		return EffectivePlan{}, fmt.Errorf("inspect user provisioning script: %w", err)
	}
	effective, err := buildEffectivePlan(ctx, plan, configurationPath, configurationExists, userScriptExists)
	if err != nil {
		return EffectivePlan{}, err
	}
	effective.ReadyChanges, err = inspectEffectiveReadyChanges(ctx, plan)
	if err != nil {
		return EffectivePlan{}, err
	}
	if len(effective.ReadyChanges) > 0 {
		effective.NextAction = "Run `herdr-sandbox down`, then `herdr-sandbox up` to apply the changed ready-Sandbox plan."
	}
	return effective, nil
}

func validateTailscalePackageSelection(enabled bool, packages wingetPackagePlan) error {
	if enabled && !packages.enabled(packageTailscale) {
		return errors.New(`tailscale requires Tailscale.Tailscale to remain in the effective WinGet package plan`)
	}
	return nil
}

func validateBaseProvisioningContract(path string) error {
	contents, err := readProvisioningScript(path, "app-owned base provisioning script", maximumBaseScriptSize)
	if err != nil {
		return err
	}
	if !strings.Contains(string(contents), baseProvisioningContract) {
		return fmt.Errorf("app-owned base provisioning script has an unsupported contract: %s", path)
	}
	return nil
}

func validateStackProvisioningContract(path string) error {
	contents, err := readProvisioningScript(path, "app-owned stack provisioning script", maximumStackScriptSize)
	if err != nil {
		return err
	}
	if !strings.Contains(string(contents), stackProvisioningContract) {
		return fmt.Errorf("app-owned stack provisioning script has an unsupported contract: %s", path)
	}
	return nil
}

func validateUserProvisioningContract(path string) error {
	contents, err := readProvisioningScript(path, "user provisioning script", maximumUserScriptSize)
	if err != nil {
		return err
	}
	text := string(contents)
	if !strings.Contains(text, userProvisioningContract) {
		return fmt.Errorf("user provisioning script has an unsupported contract: %s", path)
	}
	if strings.Contains(text, "# herdr-sandbox-base-contract:") {
		return fmt.Errorf("user provisioning script must not contain an app-owned Base contract: %s", path)
	}
	return nil
}

func readProvisioningScript(path, role string, maximumSize int64) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("inspect %s: %w", role, err)
	}
	reparse, err := fileInfoIsReparsePoint(info)
	if err != nil {
		return nil, fmt.Errorf("inspect %s reparse state: %w", role, err)
	}
	if reparse || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > maximumSize {
		return nil, fmt.Errorf("%s must be a nonempty regular non-reparse file no larger than %d bytes: %s", role, maximumSize, path)
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", role, err)
	}
	openedInfo, statErr := file.Stat()
	if statErr != nil {
		_ = file.Close()
		return nil, fmt.Errorf("inspect opened %s: %w", role, statErr)
	}
	if !os.SameFile(info, openedInfo) {
		_ = file.Close()
		return nil, fmt.Errorf("%s changed while it was opened: %s", role, path)
	}
	contents, readErr := io.ReadAll(io.LimitReader(file, maximumSize+1))
	closeErr := file.Close()
	if readErr != nil || closeErr != nil {
		return nil, fmt.Errorf("read %s: %w", role, errors.Join(readErr, closeErr))
	}
	if len(contents) == 0 || int64(len(contents)) > maximumSize {
		return nil, fmt.Errorf("%s changed size while it was read: %s", role, path)
	}
	return contents, nil
}

func resolveProvisioningAt(startDirectory, globalRoot, defaultRoot string) (provisioningPlan, error) {
	if err := ensureGlobalProvisioning(globalRoot); err != nil {
		return provisioningPlan{}, err
	}
	return resolveProvisioningConfigurationAt(startDirectory, globalRoot, defaultRoot)
}

func resolveProvisioningReadOnlyAt(startDirectory, globalRoot, defaultRoot string) (provisioningPlan, error) {
	if !filepath.IsAbs(globalRoot) || !filepath.IsAbs(defaultRoot) {
		return provisioningPlan{}, errors.New("provisioning roots must be absolute")
	}
	if err := rejectLegacyUserBase(globalRoot); err != nil {
		return provisioningPlan{}, err
	}
	return resolveProvisioningConfigurationAt(startDirectory, globalRoot, defaultRoot)
}

func resolveProvisioningConfigurationAt(startDirectory, globalRoot, defaultRoot string) (provisioningPlan, error) {
	configuration, err := loadGlobalConfiguration(filepath.Join(globalRoot, globalConfigurationName))
	if err != nil {
		return provisioningPlan{}, err
	}
	cacheDirectory, err := validateConfiguredCacheDirectory(configuration.CacheDirectory)
	if err != nil {
		return provisioningPlan{}, err
	}
	memoryMB, err := validateConfiguredMemoryMB(configuration.MemoryMB)
	if err != nil {
		return provisioningPlan{}, err
	}
	configured := configuration.Workspaces

	names := make([]string, 0, len(configured))
	for name := range configured {
		names = append(names, name)
	}
	sort.Strings(names)
	workspaces := make([]workspacePlan, 0, len(names)+1)
	for _, name := range names {
		workspace, err := newWorkspacePlan(name, configured[name])
		if err != nil {
			return provisioningPlan{}, fmt.Errorf("global workspace %q: %w", name, err)
		}
		workspaces = append(workspaces, workspace)
	}
	discovered, err := discoverWorkspacePlans(configuration.WorkspaceDiscovery)
	if err != nil {
		return provisioningPlan{}, err
	}
	for _, candidate := range discovered {
		duplicatePath := false
		for _, workspace := range workspaces {
			equal, err := workspaceDirectoriesEqual(workspace.HostDirectory, candidate.HostDirectory)
			if err != nil {
				return provisioningPlan{}, fmt.Errorf("compare discovered workspace %s with workspace %s: %w", candidate.HostDirectory, workspace.HostDirectory, err)
			}
			if equal {
				duplicatePath = true
				break
			}
		}
		if duplicatePath {
			continue
		}
		for _, workspace := range workspaces {
			if strings.EqualFold(workspace.Name, candidate.Name) {
				return provisioningPlan{}, fmt.Errorf("discovered workspace name %q for %s conflicts with workspace %s", candidate.Name, candidate.HostDirectory, workspace.HostDirectory)
			}
		}
		workspaces = append(workspaces, candidate)
	}

	activeRoot, activeScript, found, err := findProjectProvisioning(startDirectory)
	if err != nil {
		return provisioningPlan{}, err
	}
	if found {
		activeIndex := -1
		for index := range workspaces {
			equal, compareErr := workspaceDirectoriesEqual(workspaces[index].HostDirectory, activeRoot)
			if compareErr != nil {
				return provisioningPlan{}, fmt.Errorf("compare active project %s with workspace %s: %w", activeRoot, workspaces[index].HostDirectory, compareErr)
			}
			if equal {
				activeIndex = index
				break
			}
		}
		if activeIndex >= 0 {
			workspaces[activeIndex].Active = true
		} else {
			name := deriveWorkspaceName(activeRoot)
			for _, workspace := range workspaces {
				if strings.EqualFold(workspace.Name, name) {
					return provisioningPlan{}, fmt.Errorf("active project name %q conflicts with global workspace %s", name, workspace.HostDirectory)
				}
			}
			workspaces = append(workspaces, workspacePlan{
				Name:             name,
				HostDirectory:    activeRoot,
				GuestDirectory:   guestWorkspaceDirectory(name),
				ProvisioningPath: activeScript,
				Active:           true,
			})
		}
	}
	if len(workspaces) > 16 {
		return provisioningPlan{}, fmt.Errorf("workspace count %d exceeds limit 16", len(workspaces))
	}
	for left := range workspaces {
		for right := left + 1; right < len(workspaces); right++ {
			if hostPathsOverlap(workspaces[left].HostDirectory, workspaces[right].HostDirectory) {
				return provisioningPlan{}, fmt.Errorf("workspace paths overlap: %s and %s", workspaces[left].HostDirectory, workspaces[right].HostDirectory)
			}
		}
	}
	if cacheDirectory != "" {
		for _, workspace := range workspaces {
			if hostPathsOverlap(cacheDirectory, workspace.HostDirectory) {
				return provisioningPlan{}, fmt.Errorf("cache directory overlaps workspace %q: %s", workspace.Name, workspace.HostDirectory)
			}
		}
	}
	sort.SliceStable(workspaces, func(left, right int) bool {
		if workspaces[left].Active != workspaces[right].Active {
			return workspaces[left].Active
		}
		return strings.ToLower(workspaces[left].Name) < strings.ToLower(workspaces[right].Name)
	})
	return provisioningPlan{
		BaseScript:           filepath.Join(defaultRoot, baseProvisioningName),
		StackScript:          filepath.Join(defaultRoot, stackProvisioningName),
		UserScript:           filepath.Join(globalRoot, userProvisioningName),
		CacheDirectory:       cacheDirectory,
		MemoryMB:             memoryMB,
		Audio:                configuration.Audio,
		Tailscale:            configuration.Tailscale,
		CodingAgentSync:      configuration.CodingAgentSync,
		PackageConfiguration: configuration.WingetPackages,
		Workspaces:           workspaces,
	}, nil
}

func loadGlobalConfiguration(path string) (globalConfiguration, error) {
	defaultMemory := defaultMemoryMB
	config := globalConfiguration{
		MemoryMB:        &defaultMemory,
		CodingAgentSync: defaultCodingAgentSyncConfiguration(),
		WingetPackages:  defaultWingetPackageConfiguration(),
		Workspaces:      map[string]string{},
	}
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return config, nil
	}
	if err != nil {
		return globalConfiguration{}, fmt.Errorf("open global herdr-sandbox config: %w", err)
	}
	defer file.Close()
	decoder := json.NewDecoder(file)
	if err := decodeGlobalConfiguration(decoder, &config); err != nil {
		return globalConfiguration{}, fmt.Errorf("decode global herdr-sandbox config: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return globalConfiguration{}, errors.New("global herdr-sandbox config contains trailing JSON data")
	}
	return config, nil
}

func decodeGlobalConfiguration(decoder *json.Decoder, config *globalConfiguration) error {
	opening, err := decoder.Token()
	if err != nil {
		return err
	}
	if opening != json.Delim('{') {
		return errors.New("configuration must be a JSON object")
	}
	seen := map[string]bool{}
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		key, ok := token.(string)
		if !ok {
			return errors.New("configuration field name must be a string")
		}
		if seen[key] {
			return fmt.Errorf("duplicate field %q", key)
		}
		seen[key] = true
		switch key {
		case "cacheDirectory":
			raw, err := decodeNonNullJSONValue(decoder, key)
			if err != nil {
				return err
			}
			if err := json.Unmarshal(raw, &config.CacheDirectory); err != nil {
				return fmt.Errorf("field %q: %w", key, err)
			}
		case "memoryMB":
			raw, err := decodeNonNullJSONValue(decoder, key)
			if err != nil {
				return err
			}
			var memoryMB int
			if err := json.Unmarshal(raw, &memoryMB); err != nil {
				return fmt.Errorf("field %q: %w", key, err)
			}
			config.MemoryMB = &memoryMB
		case "audio":
			raw, err := decodeNonNullJSONValue(decoder, key)
			if err != nil {
				return err
			}
			if err := json.Unmarshal(raw, &config.Audio); err != nil {
				return fmt.Errorf("field %q: %w", key, err)
			}
		case "tailscale":
			raw, err := decodeNonNullJSONValue(decoder, key)
			if err != nil {
				return err
			}
			if err := json.Unmarshal(raw, &config.Tailscale); err != nil {
				return fmt.Errorf("field %q: %w", key, err)
			}
		case "workspaces":
			workspaces, err := decodeConfiguredWorkspaces(decoder)
			if err != nil {
				return fmt.Errorf("field %q: %w", key, err)
			}
			config.Workspaces = workspaces
		case "workspaceDiscovery":
			discovery, err := decodeWorkspaceDiscoveryConfiguration(decoder)
			if err != nil {
				return fmt.Errorf("field %q: %w", key, err)
			}
			config.WorkspaceDiscovery = &discovery
		case "wingetPackages":
			packages, err := decodeWingetPackageConfiguration(decoder)
			if err != nil {
				return fmt.Errorf("field %q: %w", key, err)
			}
			config.WingetPackages = packages
		case "codingAgentSync":
			agents, err := decodeCodingAgentSyncConfiguration(decoder)
			if err != nil {
				return fmt.Errorf("field %q: %w", key, err)
			}
			config.CodingAgentSync = agents
		default:
			return fmt.Errorf("unknown field %q", key)
		}
	}
	closing, err := decoder.Token()
	if err != nil {
		return err
	}
	if closing != json.Delim('}') {
		return errors.New("configuration object is not closed")
	}
	return nil
}

func decodeWorkspaceDiscoveryConfiguration(decoder *json.Decoder) (workspaceDiscoveryConfiguration, error) {
	opening, err := decoder.Token()
	if err != nil {
		return workspaceDiscoveryConfiguration{}, err
	}
	if opening != json.Delim('{') {
		return workspaceDiscoveryConfiguration{}, errors.New("must be a JSON object")
	}
	configuration := workspaceDiscoveryConfiguration{Exclude: []string{}}
	seen := map[string]bool{}
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return workspaceDiscoveryConfiguration{}, err
		}
		name, ok := token.(string)
		if !ok {
			return workspaceDiscoveryConfiguration{}, errors.New("workspace-discovery field name must be a string")
		}
		if seen[name] {
			return workspaceDiscoveryConfiguration{}, fmt.Errorf("duplicate field %q", name)
		}
		seen[name] = true
		switch name {
		case "root":
			raw, err := decodeNonNullJSONValue(decoder, name)
			if err != nil {
				return workspaceDiscoveryConfiguration{}, err
			}
			if err := json.Unmarshal(raw, &configuration.Root); err != nil {
				return workspaceDiscoveryConfiguration{}, fmt.Errorf("field %q: %w", name, err)
			}
		case "exclude":
			patterns, err := decodeWorkspaceExcludePatterns(decoder)
			if err != nil {
				return workspaceDiscoveryConfiguration{}, fmt.Errorf("field %q: %w", name, err)
			}
			configuration.Exclude = patterns
		default:
			return workspaceDiscoveryConfiguration{}, fmt.Errorf("unknown field %q", name)
		}
	}
	closing, err := decoder.Token()
	if err != nil {
		return workspaceDiscoveryConfiguration{}, err
	}
	if closing != json.Delim('}') {
		return workspaceDiscoveryConfiguration{}, errors.New("workspace-discovery object is not closed")
	}
	if _, err := compileWorkspaceExcludePatterns(configuration.Exclude); err != nil {
		return workspaceDiscoveryConfiguration{}, err
	}
	return configuration, nil
}

func decodeWorkspaceExcludePatterns(decoder *json.Decoder) ([]string, error) {
	opening, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	if opening != json.Delim('[') {
		return nil, errors.New("must be a JSON array")
	}
	patterns := []string{}
	for decoder.More() {
		if len(patterns) >= maximumWorkspaceExcludePatterns {
			return nil, fmt.Errorf("pattern count exceeds limit %d", maximumWorkspaceExcludePatterns)
		}
		raw, err := decodeNonNullJSONValue(decoder, fmt.Sprintf("exclude[%d]", len(patterns)))
		if err != nil {
			return nil, err
		}
		var pattern string
		if err := json.Unmarshal(raw, &pattern); err != nil {
			return nil, fmt.Errorf("exclude[%d]: %w", len(patterns), err)
		}
		patterns = append(patterns, pattern)
	}
	closing, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	if closing != json.Delim(']') {
		return nil, errors.New("exclude array is not closed")
	}
	return patterns, nil
}

func compileWorkspaceExcludePatterns(patterns []string) ([]*regexp.Regexp, error) {
	if len(patterns) > maximumWorkspaceExcludePatterns {
		return nil, fmt.Errorf("exclude pattern count %d exceeds limit %d", len(patterns), maximumWorkspaceExcludePatterns)
	}
	compiled := make([]*regexp.Regexp, 0, len(patterns))
	seen := map[string]bool{}
	for index, pattern := range patterns {
		if len(pattern) > maximumWorkspaceExcludePatternSize {
			return nil, fmt.Errorf("exclude[%d] exceeds length limit %d", index, maximumWorkspaceExcludePatternSize)
		}
		if seen[pattern] {
			return nil, fmt.Errorf("exclude contains duplicate pattern %q", pattern)
		}
		seen[pattern] = true
		expression, err := regexp.Compile(pattern)
		if err != nil {
			return nil, fmt.Errorf("exclude[%d] is not a valid Go regular expression: %w", index, err)
		}
		compiled = append(compiled, expression)
	}
	return compiled, nil
}

func decodeCodingAgentSyncConfiguration(decoder *json.Decoder) (codingAgentSyncConfiguration, error) {
	opening, err := decoder.Token()
	if err != nil {
		return codingAgentSyncConfiguration{}, err
	}
	if opening != json.Delim('{') {
		return codingAgentSyncConfiguration{}, errors.New("must be a JSON object")
	}
	configuration := defaultCodingAgentSyncConfiguration()
	seen := map[string]bool{}
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return codingAgentSyncConfiguration{}, err
		}
		name, ok := token.(string)
		if !ok {
			return codingAgentSyncConfiguration{}, errors.New("coding-agent field name must be a string")
		}
		if seen[name] {
			return codingAgentSyncConfiguration{}, fmt.Errorf("duplicate field %q", name)
		}
		seen[name] = true
		raw, err := decodeNonNullJSONValue(decoder, name)
		if err != nil {
			return codingAgentSyncConfiguration{}, err
		}
		var enabled bool
		if err := json.Unmarshal(raw, &enabled); err != nil {
			return codingAgentSyncConfiguration{}, fmt.Errorf("field %q: %w", name, err)
		}
		switch name {
		case "opencode":
			configuration.OpenCode = enabled
		case "claudeCode":
			configuration.ClaudeCode = enabled
		case "codex":
			configuration.Codex = enabled
		case "githubCopilot":
			configuration.GitHubCopilot = enabled
		case "pi":
			configuration.Pi = enabled
		default:
			return codingAgentSyncConfiguration{}, fmt.Errorf("unknown field %q", name)
		}
	}
	closing, err := decoder.Token()
	if err != nil {
		return codingAgentSyncConfiguration{}, err
	}
	if closing != json.Delim('}') {
		return codingAgentSyncConfiguration{}, errors.New("coding-agent sync object is not closed")
	}
	return configuration, nil
}

func decodeConfiguredWorkspaces(decoder *json.Decoder) (map[string]string, error) {
	opening, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	if opening != json.Delim('{') {
		return nil, errors.New("must be a JSON object")
	}
	workspaces := map[string]string{}
	seen := map[string]bool{}
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return nil, err
		}
		name, ok := token.(string)
		if !ok {
			return nil, errors.New("workspace name must be a string")
		}
		identity := strings.ToLower(name)
		if seen[identity] {
			return nil, fmt.Errorf("duplicate workspace name %q", name)
		}
		seen[identity] = true
		raw, err := decodeNonNullJSONValue(decoder, name)
		if err != nil {
			return nil, err
		}
		var directory string
		if err := json.Unmarshal(raw, &directory); err != nil {
			return nil, fmt.Errorf("workspace %q: %w", name, err)
		}
		workspaces[name] = directory
	}
	closing, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	if closing != json.Delim('}') {
		return nil, errors.New("workspace object is not closed")
	}
	return workspaces, nil
}

func decodeNonNullJSONValue(decoder *json.Decoder, name string) (json.RawMessage, error) {
	var raw json.RawMessage
	if err := decoder.Decode(&raw); err != nil {
		return nil, err
	}
	if strings.TrimSpace(string(raw)) == "null" {
		return nil, fmt.Errorf("field %q must not be null", name)
	}
	return raw, nil
}

func validateConfiguredMemoryMB(memoryMB *int) (int, error) {
	if memoryMB == nil {
		return 0, errors.New("memoryMB must be an integer")
	}
	if *memoryMB < 2048 {
		return 0, fmt.Errorf("memoryMB must be at least 2048, got %d", *memoryMB)
	}
	return *memoryMB, nil
}

func validateConfiguredCacheDirectory(directory string) (string, error) {
	directory = strings.TrimSpace(directory)
	if directory == "" {
		return "", nil
	}
	if !filepath.IsAbs(directory) {
		return "", fmt.Errorf("cacheDirectory is not absolute: %q", directory)
	}
	directory = filepath.Clean(directory)
	volumeRoot := filepath.Clean(filepath.VolumeName(directory) + string(os.PathSeparator))
	if strings.EqualFold(directory, volumeRoot) {
		return "", fmt.Errorf("cacheDirectory must not map an entire volume: %s", directory)
	}
	for _, protected := range []string{os.Getenv("USERPROFILE"), os.Getenv("APPDATA"), os.Getenv("LOCALAPPDATA")} {
		protected = strings.TrimSpace(protected)
		if protected != "" && filepath.IsAbs(protected) && hostPathContains(directory, protected) {
			return "", fmt.Errorf("cacheDirectory must not contain a user profile or AppData root: %s", directory)
		}
	}
	if info, err := os.Stat(directory); err == nil && !info.IsDir() {
		return "", fmt.Errorf("cacheDirectory is not a directory: %s", directory)
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("inspect cacheDirectory: %w", err)
	}
	return directory, nil
}

func discoverWorkspacePlans(configuration *workspaceDiscoveryConfiguration) ([]workspacePlan, error) {
	if configuration == nil {
		return nil, nil
	}
	patterns, err := compileWorkspaceExcludePatterns(configuration.Exclude)
	if err != nil {
		return nil, fmt.Errorf("workspaceDiscovery: %w", err)
	}
	root := strings.TrimSpace(configuration.Root)
	if root == "" {
		return nil, nil
	}
	if !filepath.IsAbs(root) {
		return nil, fmt.Errorf("workspaceDiscovery.root is not absolute: %q", configuration.Root)
	}
	root, err = canonicalMappedDirectory(root)
	if err != nil {
		return nil, fmt.Errorf("workspaceDiscovery.root: %w", err)
	}
	rootIdentity, err := physicalMappedDirectory(root)
	if err != nil {
		return nil, fmt.Errorf("workspaceDiscovery.root: %w", err)
	}
	if err := validatePhysicalMappingDoesNotContainProtectedRoot("workspaceDiscovery.root", rootIdentity); err != nil {
		return nil, err
	}

	directory, err := os.Open(root)
	if err != nil {
		return nil, fmt.Errorf("open workspaceDiscovery.root: %w", err)
	}
	entries, readErr := directory.ReadDir(maximumWorkspaceDiscoveryEntries + 1)
	closeErr := directory.Close()
	if readErr != nil && !errors.Is(readErr, io.EOF) {
		return nil, fmt.Errorf("read workspaceDiscovery.root: %w", readErr)
	}
	if closeErr != nil {
		return nil, fmt.Errorf("close workspaceDiscovery.root: %w", closeErr)
	}
	if len(entries) > maximumWorkspaceDiscoveryEntries {
		return nil, fmt.Errorf("workspaceDiscovery.root contains more than %d direct entries", maximumWorkspaceDiscoveryEntries)
	}
	sort.Slice(entries, func(left, right int) bool {
		leftName := strings.ToLower(entries[left].Name())
		rightName := strings.ToLower(entries[right].Name())
		if leftName == rightName {
			return entries[left].Name() < entries[right].Name()
		}
		return leftName < rightName
	})

	workspaces := []workspacePlan{}
	for _, entry := range entries {
		name := entry.Name()
		excluded := false
		for _, pattern := range patterns {
			if pattern.MatchString(name) {
				excluded = true
				break
			}
		}
		if excluded {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return nil, fmt.Errorf("inspect workspaceDiscovery child %q: %w", name, err)
		}
		directoryEntry, err := fileInfoIsDirectory(info)
		if err != nil {
			return nil, fmt.Errorf("inspect workspaceDiscovery child %q: %w", name, err)
		}
		if !directoryEntry {
			continue
		}
		reparse, err := fileInfoIsReparsePoint(info)
		if err != nil {
			return nil, fmt.Errorf("inspect workspaceDiscovery child %q: %w", name, err)
		}
		if reparse {
			return nil, fmt.Errorf("workspaceDiscovery child must not be a reparse point: %s", filepath.Join(root, name))
		}
		child, err := canonicalMappedDirectory(filepath.Join(root, name))
		if err != nil {
			return nil, fmt.Errorf("workspaceDiscovery child %q: %w", name, err)
		}
		workspace, err := newWorkspacePlan(deriveWorkspaceName(child), child)
		if err != nil {
			return nil, fmt.Errorf("workspaceDiscovery child %q: %w", name, err)
		}
		workspaces = append(workspaces, workspace)
		if len(workspaces) > 16 {
			return nil, fmt.Errorf("workspaceDiscovery selects more than 16 direct child directories")
		}
	}
	return workspaces, nil
}

func newWorkspacePlan(name, directory string) (workspacePlan, error) {
	if !workspaceNamePattern.MatchString(name) {
		return workspacePlan{}, errors.New("name must match [A-Za-z0-9][A-Za-z0-9._-]{0,63}")
	}
	if !filepath.IsAbs(directory) {
		return workspacePlan{}, fmt.Errorf("path is not absolute: %q", directory)
	}
	directory = filepath.Clean(directory)
	info, err := os.Stat(directory)
	if err != nil {
		return workspacePlan{}, fmt.Errorf("inspect directory: %w", err)
	}
	if !info.IsDir() {
		return workspacePlan{}, fmt.Errorf("path is not a directory: %s", directory)
	}
	script := filepath.Join(directory, projectConfigurationName, projectProvisioningName)
	if err := validateProjectProvisioningScript(script); err != nil {
		return workspacePlan{}, err
	}
	return workspacePlan{
		Name:             name,
		HostDirectory:    directory,
		GuestDirectory:   guestWorkspaceDirectory(name),
		ProvisioningPath: script,
	}, nil
}

func validateProjectProvisioningScript(path string) error {
	if err := rejectMappedPathReparsePoints(filepath.Dir(path)); err != nil {
		return fmt.Errorf("project provisioning script has an unsafe parent: %w", err)
	}
	_, err := readProvisioningScript(path, "project provisioning script", maximumProjectScriptSize)
	return err
}

func canonicalWorkspacePlans(workspaces []workspacePlan) ([]workspacePlan, error) {
	result := append([]workspacePlan(nil), workspaces...)
	for index := range result {
		directory, err := canonicalMappedDirectory(result[index].HostDirectory)
		if err != nil {
			return nil, fmt.Errorf("workspace %q: %w", result[index].Name, err)
		}
		script := filepath.Join(directory, projectConfigurationName, projectProvisioningName)
		if err := validateProjectProvisioningScript(script); err != nil {
			return nil, fmt.Errorf("workspace %q: %w", result[index].Name, err)
		}
		result[index].HostDirectory = directory
		result[index].ProvisioningPath = script
	}
	return result, nil
}

func findProjectProvisioning(startDirectory string) (string, string, bool, error) {
	if startDirectory == "" {
		var err error
		startDirectory, err = os.Getwd()
		if err != nil {
			return "", "", false, fmt.Errorf("resolve current directory: %w", err)
		}
	}
	startDirectory, err := filepath.Abs(startDirectory)
	if err != nil {
		return "", "", false, fmt.Errorf("resolve project search directory: %w", err)
	}
	info, err := os.Stat(startDirectory)
	if err != nil {
		return "", "", false, fmt.Errorf("inspect project search directory: %w", err)
	}
	if !info.IsDir() {
		return "", "", false, fmt.Errorf("project search path is not a directory: %s", startDirectory)
	}

	for directory := filepath.Clean(startDirectory); ; directory = filepath.Dir(directory) {
		script := filepath.Join(directory, projectConfigurationName, projectProvisioningName)
		if _, statErr := os.Lstat(script); statErr == nil {
			if err := validateProjectProvisioningScript(script); err != nil {
				return "", "", false, err
			}
			return directory, script, true, nil
		} else if !errors.Is(statErr, os.ErrNotExist) {
			return "", "", false, fmt.Errorf("inspect project provisioning script: %w", statErr)
		}
		parent := filepath.Dir(directory)
		if parent == directory {
			break
		}
	}
	return "", "", false, nil
}

func ensureGlobalProvisioning(globalRoot string) error {
	if !filepath.IsAbs(globalRoot) {
		return errors.New("global provisioning directory must be absolute")
	}
	if err := os.MkdirAll(globalRoot, 0o700); err != nil {
		return fmt.Errorf("create global herdr-sandbox directory: %w", err)
	}
	userPath := filepath.Join(globalRoot, userProvisioningName)
	if err := seedUserProvisioning(userPath); err != nil {
		return err
	}
	if err := rejectLegacyUserBase(globalRoot); err != nil {
		return err
	}
	return ensureGlobalWorkspaceConfig(globalRoot)
}

func rejectLegacyUserBase(globalRoot string) error {
	userPath := filepath.Join(globalRoot, userProvisioningName)
	legacyBase := filepath.Join(globalRoot, baseProvisioningName)
	if _, err := os.Lstat(legacyBase); err == nil {
		return fmt.Errorf("legacy user-owned Base found at %s; it was not modified and will not be executed: move only deliberate global extension commands into %s, route package choices to %s, archive the legacy file under a non-reserved name, then retry", legacyBase, userPath, filepath.Join(globalRoot, globalConfigurationName))
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect legacy user-owned Base: %w", err)
	}
	return nil
}

func seedUserProvisioning(path string) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		return validateUserProvisioningContract(path)
	}
	if err != nil {
		return fmt.Errorf("create user provisioning script %s: %w", path, err)
	}
	written := false
	defer func() {
		_ = file.Close()
		if !written {
			_ = os.Remove(path)
		}
	}()
	if _, err := file.Write(defaultUserProvisioningScript); err != nil {
		return fmt.Errorf("write user provisioning script %s: %w", path, err)
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("sync user provisioning script %s: %w", path, err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close user provisioning script %s: %w", path, err)
	}
	written = true
	return nil
}

func ensureGlobalWorkspaceConfig(globalRoot string) error {
	path := filepath.Join(globalRoot, globalConfigurationName)
	if info, err := os.Stat(path); err == nil {
		if !info.Mode().IsRegular() {
			return fmt.Errorf("global workspace config is not a regular file: %s", path)
		}
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect global workspace config: %w", err)
	}
	contents := []byte("{\n  \"cacheDirectory\": \"\",\n  \"memoryMB\": 32768,\n  \"audio\": false,\n  \"tailscale\": false,\n  \"codingAgentSync\": {\n    \"opencode\": true,\n    \"claudeCode\": true,\n    \"codex\": true,\n    \"githubCopilot\": true,\n    \"pi\": true\n  },\n  \"wingetPackages\": {\n    \"remove\": [],\n    \"add\": [],\n    \"versions\": {}\n  },\n  \"workspaceDiscovery\": {\n    \"root\": \"\",\n    \"exclude\": []\n  },\n  \"workspaces\": {}\n}\n")
	if err := writeFileAtomically(path, contents, 0o600); err != nil {
		return fmt.Errorf("seed global workspace config %s: %w", path, err)
	}
	return nil
}

func deriveWorkspaceName(directory string) string {
	name := invalidWorkspaceNamePattern.ReplaceAllString(filepath.Base(directory), "-")
	name = strings.Trim(name, ".-_")
	if name == "" {
		return "workspace"
	}
	if len(name) > 64 {
		name = name[:64]
	}
	return name
}

func guestWorkspaceDirectory(name string) string {
	return guestWorkspacesDirectory + `\` + name
}

func hostPathsOverlap(left, right string) bool {
	return hostPathContains(left, right) || hostPathContains(right, left)
}

func workspaceDirectoriesEqual(left, right string) (bool, error) {
	leftIdentity, err := physicalMappedDirectory(left)
	if err != nil {
		return false, err
	}
	rightIdentity, err := physicalMappedDirectory(right)
	if err != nil {
		return false, err
	}
	return strings.EqualFold(leftIdentity, rightIdentity), nil
}

func hostPathContains(directory, path string) bool {
	directory = strings.ToLower(filepath.Clean(directory))
	path = strings.ToLower(filepath.Clean(path))
	relative, err := filepath.Rel(directory, path)
	return err == nil && (relative == "." || (relative != ".." && !strings.HasPrefix(relative, ".."+string(os.PathSeparator))))
}

func validatePhysicalMappingDoesNotContainProtectedRoot(role, identity string) error {
	for _, protected := range []string{os.Getenv("USERPROFILE"), os.Getenv("APPDATA"), os.Getenv("LOCALAPPDATA")} {
		protected = strings.TrimSpace(protected)
		if protected == "" || !filepath.IsAbs(protected) {
			continue
		}
		protectedIdentity, err := physicalMappedDirectory(filepath.Clean(protected))
		if err != nil {
			return fmt.Errorf("%s: resolve protected directory %s: %w", role, protected, err)
		}
		if hostPathContains(identity, protectedIdentity) {
			return fmt.Errorf("%s must not contain a user profile or AppData root: %s", role, protected)
		}
	}
	return nil
}
