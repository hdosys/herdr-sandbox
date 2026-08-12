package sandbox

import (
	"bytes"
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

	"herdr-sandbox/internal/productidentity"
)

const (
	applicationName                    = productidentity.ApplicationName
	projectConfigurationName           = productidentity.ProjectDirectoryName
	projectProvisioningName            = productidentity.ProjectScriptName
	baseProvisioningName               = productidentity.BaseScriptName
	stackProvisioningName              = productidentity.StackScriptName
	userProvisioningName               = productidentity.UserScriptName
	provisioningProcessName            = "provisioning-process.cs"
	workspaceManifestName              = "workspaces.json"
	globalConfigurationName            = productidentity.ConfigurationName
	guestMountsDirectory               = `C:\Mounts`
	guestWorkspacesDirectory           = `C:\Workspaces`
	baseProvisioningContract           = "# herdr-sandbox-base-contract: 50"
	stackProvisioningContract          = "# herdr-sandbox-stacks-contract: 15"
	userProvisioningContract           = "# herdr-sandbox-user-contract: 1"
	provisioningProcessContract        = "// herdr-sandbox-provisioning-process-contract: 3"
	workspaceManifestSchema            = 1
	maximumBaseScriptSize              = 1024 * 1024
	maximumStackScriptSize             = 2 * 1024 * 1024
	maximumUserScriptSize              = 1024 * 1024
	maximumProjectScriptSize           = 1024 * 1024
	maximumProvisioningProcessSize     = 512 * 1024
	maximumMounts                      = 16
	maximumWorkspaceDiscoveryEntries   = 4096
	maximumWorkspaceExcludePatterns    = 64
	maximumWorkspaceExcludePatternSize = 1024
)

var defaultUserProvisioningScript = []byte(userProvisioningContract + `
$ErrorActionPreference = 'Stop'
Set-StrictMode -Version 2.0

# Add idempotent global guest customization below. Prefer config.json for packages.
`)

var defaultGlobalConfiguration = []byte("{\n  \"cacheDirectory\": \"\",\n  \"memoryMB\": 32768,\n  \"audio\": false,\n  \"audioInput\": false,\n  \"tailscale\": false,\n  \"mobileSSHAuthorizedKeys\": [],\n  \"codingAgentSync\": {\n    \"opencode\": true,\n    \"claudeCode\": true,\n    \"codex\": true,\n    \"githubCopilot\": true,\n    \"pi\": true\n  },\n  \"workspaces\": {},\n  \"mounts\": {},\n  \"workspaceDiscovery\": {\n    \"root\": \"\",\n    \"exclude\": []\n  },\n  \"wingetPackages\": {\n    \"remove\": [],\n    \"add\": [\n      \"SST.opencode\",\n      \"Anthropic.ClaudeCode\",\n      \"OpenAI.Codex\",\n      \"GitHub.Copilot\"\n    ],\n    \"versions\": {}\n  }\n}\n")

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

type mountPlan struct {
	Name           string
	HostDirectory  string
	GuestDirectory string
	ReadOnly       bool
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
	BaseScript              string
	StackScript             string
	UserScript              string
	CacheDirectory          string
	MemoryMB                int
	AudioOutput             bool
	AudioInput              bool
	Tailscale               bool
	MobileSSHAuthorizedKeys []string
	CodingAgentSync         codingAgentSyncConfiguration
	PackageConfiguration    wingetPackageConfiguration
	Packages                wingetPackagePlan
	WindowsTerminal         windowsTerminalConfiguration
	Mounts                  []mountPlan
	Workspaces              []workspacePlan
}

type globalConfiguration struct {
	CacheDirectory          string                           `json:"cacheDirectory"`
	MemoryMB                *int                             `json:"memoryMB,omitempty"`
	AudioOutput             bool                             `json:"audio"`
	AudioInput              bool                             `json:"audioInput"`
	Tailscale               bool                             `json:"tailscale"`
	MobileSSHAuthorizedKeys []string                         `json:"mobileSSHAuthorizedKeys"`
	Mounts                  map[string]mountConfiguration    `json:"mounts"`
	CodingAgentSync         codingAgentSyncConfiguration     `json:"codingAgentSync"`
	WingetPackages          wingetPackageConfiguration       `json:"wingetPackages"`
	WorkspaceDiscovery      *workspaceDiscoveryConfiguration `json:"workspaceDiscovery,omitempty"`
	Workspaces              map[string]string                `json:"workspaces"`
}

type mountConfiguration struct {
	Path     string `json:"path"`
	ReadOnly bool   `json:"readOnly"`
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
		return provisioningPlan{}, errors.New("no workspace is selected; run `sandbox init --stack <name>` from a project containing the source to map, or add a workspace to %APPDATA%\\herdr-sandbox\\config.json")
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
		effective.NextAction = "Run `sandbox down`, then `sandbox up` to apply the changed ready-Sandbox plan."
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

func validateProvisioningProcessSource(data []byte) error {
	if len(data) == 0 || len(data) > maximumProvisioningProcessSize {
		return fmt.Errorf("embedded provisioning process source must be nonempty and no larger than %d bytes", maximumProvisioningProcessSize)
	}
	if !strings.Contains(string(data), provisioningProcessContract) {
		return errors.New("embedded provisioning process source has an unsupported contract")
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
	mobileSSHAuthorizedKeys, err := canonicalizeMobileSSHAuthorizedKeys(configuration.MobileSSHAuthorizedKeys)
	if err != nil {
		return provisioningPlan{}, err
	}
	if len(mobileSSHAuthorizedKeys) > 0 && !configuration.Tailscale {
		return provisioningPlan{}, errors.New("mobileSSHAuthorizedKeys requires tailscale to be true")
	}
	mountNames := make([]string, 0, len(configuration.Mounts))
	for name := range configuration.Mounts {
		mountNames = append(mountNames, name)
	}
	sort.Strings(mountNames)
	mounts := make([]mountPlan, 0, len(mountNames))
	mountErrors := []error{}
	for _, name := range mountNames {
		mount, err := newMountPlan(name, configuration.Mounts[name])
		if err != nil {
			mountErrors = append(mountErrors, fmt.Errorf("mount %q: %w", name, err))
			continue
		}
		mounts = append(mounts, mount)
	}
	if len(mountErrors) > 0 {
		return provisioningPlan{}, fmt.Errorf("folder mount validation failed: %w", errors.Join(mountErrors...))
	}
	configured := configuration.Workspaces

	names := make([]string, 0, len(configured))
	for name := range configured {
		names = append(names, name)
	}
	sort.Strings(names)
	workspaces := make([]workspacePlan, 0, len(names)+1)
	workspaceErrors := []error{}
	for _, name := range names {
		workspace, err := newWorkspacePlan(name, configured[name])
		if err != nil {
			workspaceErrors = append(workspaceErrors, fmt.Errorf("global workspace %q: %w", name, err))
			continue
		}
		workspaces = append(workspaces, workspace)
	}
	discovered, err := discoverWorkspacePlans(configuration.WorkspaceDiscovery)
	if err != nil {
		workspaceErrors = append(workspaceErrors, err)
	}
	if len(workspaceErrors) > 0 {
		return provisioningPlan{}, fmt.Errorf("workspace validation failed: %w", errors.Join(workspaceErrors...))
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
			overlap, err := mappedDirectoriesOverlap(workspaces[left].HostDirectory, workspaces[right].HostDirectory)
			if err != nil {
				return provisioningPlan{}, fmt.Errorf("compare workspace paths %s and %s: %w", workspaces[left].HostDirectory, workspaces[right].HostDirectory, err)
			}
			if overlap {
				return provisioningPlan{}, fmt.Errorf("workspace paths overlap: %s and %s", workspaces[left].HostDirectory, workspaces[right].HostDirectory)
			}
		}
	}
	for left := range mounts {
		for right := left + 1; right < len(mounts); right++ {
			overlap, err := mappedDirectoriesOverlap(mounts[left].HostDirectory, mounts[right].HostDirectory)
			if err != nil {
				return provisioningPlan{}, fmt.Errorf("compare folder mount paths %s and %s: %w", mounts[left].HostDirectory, mounts[right].HostDirectory, err)
			}
			if overlap {
				return provisioningPlan{}, fmt.Errorf("folder mount paths overlap: %s and %s", mounts[left].HostDirectory, mounts[right].HostDirectory)
			}
		}
		for _, workspace := range workspaces {
			overlap, err := mappedDirectoriesOverlap(mounts[left].HostDirectory, workspace.HostDirectory)
			if err != nil {
				return provisioningPlan{}, fmt.Errorf("compare folder mount %q with workspace %q: %w", mounts[left].Name, workspace.Name, err)
			}
			if overlap {
				return provisioningPlan{}, fmt.Errorf("folder mount %q overlaps workspace %q: %s", mounts[left].Name, workspace.Name, mounts[left].HostDirectory)
			}
		}
	}
	if cacheDirectory != "" {
		for _, mount := range mounts {
			if hostPathsOverlap(cacheDirectory, mount.HostDirectory) {
				return provisioningPlan{}, fmt.Errorf("cache directory overlaps folder mount %q: %s", mount.Name, mount.HostDirectory)
			}
		}
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
		BaseScript:              filepath.Join(defaultRoot, baseProvisioningName),
		StackScript:             filepath.Join(defaultRoot, stackProvisioningName),
		UserScript:              filepath.Join(globalRoot, userProvisioningName),
		CacheDirectory:          cacheDirectory,
		MemoryMB:                memoryMB,
		AudioOutput:             configuration.AudioOutput,
		AudioInput:              configuration.AudioInput,
		Tailscale:               configuration.Tailscale,
		MobileSSHAuthorizedKeys: mobileSSHAuthorizedKeys,
		CodingAgentSync:         configuration.CodingAgentSync,
		PackageConfiguration:    configuration.WingetPackages,
		Mounts:                  mounts,
		Workspaces:              workspaces,
	}, nil
}

func loadGlobalConfiguration(path string) (globalConfiguration, error) {
	defaultMemory := defaultMemoryMB
	config := globalConfiguration{
		MemoryMB:        &defaultMemory,
		CodingAgentSync: defaultCodingAgentSyncConfiguration(),
		WingetPackages:  defaultWingetPackageConfiguration(),
		Mounts:          map[string]mountConfiguration{},
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
			if err := json.Unmarshal(raw, &config.AudioOutput); err != nil {
				return fmt.Errorf("field %q: %w", key, err)
			}
		case "audioInput":
			raw, err := decodeNonNullJSONValue(decoder, key)
			if err != nil {
				return err
			}
			if err := json.Unmarshal(raw, &config.AudioInput); err != nil {
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
		case "mobileSSHAuthorizedKeys":
			raw, err := decodeNonNullJSONValue(decoder, key)
			if err != nil {
				return err
			}
			if err := json.Unmarshal(raw, &config.MobileSSHAuthorizedKeys); err != nil {
				return fmt.Errorf("field %q: %w", key, err)
			}
		case "mounts":
			mounts, err := decodeConfiguredMounts(decoder)
			if err != nil {
				return fmt.Errorf("field %q: %w", key, err)
			}
			config.Mounts = mounts
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

func decodeConfiguredMounts(decoder *json.Decoder) (map[string]mountConfiguration, error) {
	opening, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	if opening != json.Delim('{') {
		return nil, errors.New("must be a JSON object")
	}
	mounts := map[string]mountConfiguration{}
	seen := map[string]bool{}
	for decoder.More() {
		if len(mounts) >= maximumMounts {
			return nil, fmt.Errorf("mount count exceeds limit %d", maximumMounts)
		}
		token, err := decoder.Token()
		if err != nil {
			return nil, err
		}
		name, ok := token.(string)
		if !ok {
			return nil, errors.New("mount name must be a string")
		}
		identity := strings.ToLower(name)
		if seen[identity] {
			return nil, fmt.Errorf("duplicate mount name %q", name)
		}
		seen[identity] = true
		configuration, err := decodeMountConfiguration(decoder)
		if err != nil {
			return nil, fmt.Errorf("mount %q: %w", name, err)
		}
		mounts[name] = configuration
	}
	closing, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	if closing != json.Delim('}') {
		return nil, errors.New("mount object is not closed")
	}
	return mounts, nil
}

func decodeMountConfiguration(decoder *json.Decoder) (mountConfiguration, error) {
	opening, err := decoder.Token()
	if err != nil {
		return mountConfiguration{}, err
	}
	if opening != json.Delim('{') {
		return mountConfiguration{}, errors.New("must be a JSON object")
	}
	var configuration mountConfiguration
	seen := map[string]bool{}
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return mountConfiguration{}, err
		}
		name, ok := token.(string)
		if !ok {
			return mountConfiguration{}, errors.New("mount field name must be a string")
		}
		if seen[name] {
			return mountConfiguration{}, fmt.Errorf("duplicate field %q", name)
		}
		seen[name] = true
		raw, err := decodeNonNullJSONValue(decoder, name)
		if err != nil {
			return mountConfiguration{}, err
		}
		switch name {
		case "path":
			if err := json.Unmarshal(raw, &configuration.Path); err != nil {
				return mountConfiguration{}, fmt.Errorf("field %q: %w", name, err)
			}
		case "readOnly":
			if err := json.Unmarshal(raw, &configuration.ReadOnly); err != nil {
				return mountConfiguration{}, fmt.Errorf("field %q: %w", name, err)
			}
		default:
			return mountConfiguration{}, fmt.Errorf("unknown field %q", name)
		}
	}
	closing, err := decoder.Token()
	if err != nil {
		return mountConfiguration{}, err
	}
	if closing != json.Delim('}') {
		return mountConfiguration{}, errors.New("mount entry is not closed")
	}
	for _, required := range []string{"path", "readOnly"} {
		if !seen[required] {
			return mountConfiguration{}, fmt.Errorf("missing required field %q", required)
		}
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
	if info, err := os.Stat(directory); err == nil {
		if !info.IsDir() {
			return "", fmt.Errorf("cacheDirectory is not a directory: %s", directory)
		}
		physical, err := canonicalMappedDirectory(directory)
		if err != nil {
			return "", fmt.Errorf("validate cacheDirectory: %w", err)
		}
		return physical, nil
	} else if !errors.Is(err, os.ErrNotExist) {
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
	if err := validatePhysicalMappingDoesNotExposeSensitiveRoot("workspaceDiscovery.root", rootIdentity); err != nil {
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
	workspaceErrors := []error{}
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
			workspaceErrors = append(workspaceErrors, fmt.Errorf("inspect workspaceDiscovery child %q: %w", name, err))
			continue
		}
		directoryEntry, err := fileInfoIsDirectory(info)
		if err != nil {
			workspaceErrors = append(workspaceErrors, fmt.Errorf("inspect workspaceDiscovery child %q: %w", name, err))
			continue
		}
		if !directoryEntry {
			continue
		}
		reparse, err := fileInfoIsReparsePoint(info)
		if err != nil {
			workspaceErrors = append(workspaceErrors, fmt.Errorf("inspect workspaceDiscovery child %q: %w", name, err))
			continue
		}
		if reparse {
			workspaceErrors = append(workspaceErrors, fmt.Errorf("workspaceDiscovery child %q must not be a reparse point: %s", name, filepath.Join(root, name)))
			continue
		}
		child, err := canonicalMappedDirectory(filepath.Join(root, name))
		if err != nil {
			workspaceErrors = append(workspaceErrors, fmt.Errorf("workspaceDiscovery child %q: %w", name, err))
			continue
		}
		workspace, err := newWorkspacePlan(deriveWorkspaceName(child), child)
		if err != nil {
			workspaceErrors = append(workspaceErrors, fmt.Errorf("workspaceDiscovery child %q: %w", name, err))
			continue
		}
		workspaces = append(workspaces, workspace)
		if len(workspaces) == 17 {
			workspaceErrors = append(workspaceErrors, errors.New("workspaceDiscovery selects more than 16 direct child directories"))
		}
	}
	return workspaces, errors.Join(workspaceErrors...)
}

func newMountPlan(name string, configuration mountConfiguration) (mountPlan, error) {
	if !workspaceNamePattern.MatchString(name) {
		return mountPlan{}, errors.New("name must match [A-Za-z0-9][A-Za-z0-9._-]{0,63}")
	}
	if !filepath.IsAbs(configuration.Path) {
		return mountPlan{}, fmt.Errorf("path is not absolute: %q", configuration.Path)
	}
	directory, err := canonicalMappedDirectory(configuration.Path)
	if err != nil {
		return mountPlan{}, fmt.Errorf("inspect directory: %w", err)
	}
	volumeRoot := filepath.Clean(filepath.VolumeName(directory) + string(os.PathSeparator))
	if strings.EqualFold(directory, volumeRoot) {
		return mountPlan{}, fmt.Errorf("path must not map an entire volume: %s", directory)
	}
	identity, err := physicalMappedDirectory(directory)
	if err != nil {
		return mountPlan{}, err
	}
	if err := validatePhysicalMappingDoesNotContainProtectedRoot("folder mount "+name, identity); err != nil {
		return mountPlan{}, err
	}
	if err := validatePhysicalMappingDoesNotExposeSensitiveRoot("folder mount "+name, identity); err != nil {
		return mountPlan{}, err
	}
	return mountPlan{
		Name:           name,
		HostDirectory:  directory,
		GuestDirectory: guestMountDirectory(name),
		ReadOnly:       configuration.ReadOnly,
	}, nil
}

func newWorkspacePlan(name, directory string) (workspacePlan, error) {
	if !workspaceNamePattern.MatchString(name) {
		return workspacePlan{}, errors.New("name must match [A-Za-z0-9][A-Za-z0-9._-]{0,63}")
	}
	if !filepath.IsAbs(directory) {
		return workspacePlan{}, fmt.Errorf("path is not absolute: %q", directory)
	}
	directory, err := canonicalMappedDirectory(directory)
	if err != nil {
		return workspacePlan{}, fmt.Errorf("inspect directory: %w", err)
	}
	script, err := optionalProjectProvisioningPath(directory)
	if err != nil {
		return workspacePlan{}, err
	}
	return workspacePlan{
		Name:             name,
		HostDirectory:    directory,
		GuestDirectory:   guestWorkspaceDirectory(name),
		ProvisioningPath: script,
	}, nil
}

func optionalProjectProvisioningPath(directory string) (string, error) {
	script := filepath.Join(directory, projectConfigurationName, projectProvisioningName)
	if _, err := os.Lstat(script); errors.Is(err, os.ErrNotExist) {
		return "", nil
	} else if err != nil {
		return "", fmt.Errorf("inspect project provisioning script: %w", err)
	}
	if err := validateProjectProvisioningScript(script); err != nil {
		return "", err
	}
	return script, nil
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
	workspaceErrors := []error{}
	for index := range result {
		directory, err := canonicalMappedDirectory(result[index].HostDirectory)
		if err != nil {
			workspaceErrors = append(workspaceErrors, fmt.Errorf("workspace %q: %w", result[index].Name, err))
			continue
		}
		script, err := optionalProjectProvisioningPath(directory)
		if err != nil {
			workspaceErrors = append(workspaceErrors, fmt.Errorf("workspace %q: %w", result[index].Name, err))
			continue
		}
		result[index].HostDirectory = directory
		result[index].ProvisioningPath = script
	}
	if len(workspaceErrors) > 0 {
		return nil, fmt.Errorf("workspace validation failed: %w", errors.Join(workspaceErrors...))
	}
	return result, nil
}

func canonicalMountPlans(mounts []mountPlan) ([]mountPlan, error) {
	if len(mounts) > maximumMounts {
		return nil, fmt.Errorf("folder mount count %d exceeds limit %d", len(mounts), maximumMounts)
	}
	result := append([]mountPlan(nil), mounts...)
	mountErrors := []error{}
	for index := range result {
		if !workspaceNamePattern.MatchString(result[index].Name) {
			mountErrors = append(mountErrors, fmt.Errorf("folder mount name is invalid: %q", result[index].Name))
			continue
		}
		expectedGuest := guestMountDirectory(result[index].Name)
		if !strings.EqualFold(filepath.Clean(result[index].GuestDirectory), expectedGuest) {
			mountErrors = append(mountErrors, fmt.Errorf("folder mount %q guest directory = %q, want %q", result[index].Name, result[index].GuestDirectory, expectedGuest))
			continue
		}
		directory, err := canonicalMappedDirectory(result[index].HostDirectory)
		if err != nil {
			mountErrors = append(mountErrors, fmt.Errorf("folder mount %q: %w", result[index].Name, err))
			continue
		}
		result[index].HostDirectory = directory
		result[index].GuestDirectory = expectedGuest
	}
	if len(mountErrors) > 0 {
		return nil, fmt.Errorf("folder mount validation failed: %w", errors.Join(mountErrors...))
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
	if err := seedGlobalProvisioning(globalRoot); err != nil {
		return err
	}
	return rejectLegacyUserBase(globalRoot)
}

// SeedInstallerConfiguration creates the user-owned defaults that setup owns
// seeding once. Existing user files are never replaced.
func SeedInstallerConfiguration() error {
	configurationRoot, err := os.UserConfigDir()
	if err != nil {
		return fmt.Errorf("resolve user configuration directory: %w", err)
	}
	if !filepath.IsAbs(configurationRoot) {
		return fmt.Errorf("user configuration directory is not absolute: %q", configurationRoot)
	}
	return seedInstallerConfigurationRoot(filepath.Join(configurationRoot, applicationName))
}

func seedInstallerConfigurationRoot(globalRoot string) (resultErr error) {
	if !filepath.IsAbs(globalRoot) {
		return errors.New("installer configuration directory must be absolute")
	}
	rootCreated := false
	if info, err := os.Lstat(globalRoot); errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(globalRoot, 0o700); err != nil {
			return fmt.Errorf("create installer configuration directory: %w", err)
		}
		rootCreated = true
	} else if err != nil {
		return fmt.Errorf("inspect installer configuration directory: %w", err)
	} else {
		reparse, reparseErr := fileInfoIsReparsePoint(info)
		if reparseErr != nil {
			return fmt.Errorf("inspect installer configuration directory reparse state: %w", reparseErr)
		}
		if reparse || !info.IsDir() {
			return fmt.Errorf("installer configuration path is not a regular non-reparse directory: %s", globalRoot)
		}
	}

	type seededFile struct {
		path     string
		contents []byte
	}
	created := make([]seededFile, 0, 2)
	defer func() {
		if resultErr == nil {
			return
		}
		var rollbackErr error
		for index := len(created) - 1; index >= 0; index-- {
			if err := removeSeededFileIfUnchanged(created[index].path, created[index].contents); err != nil {
				rollbackErr = errors.Join(rollbackErr, err)
			}
		}
		if rootCreated {
			if err := os.Remove(globalRoot); err != nil && !errors.Is(err, os.ErrNotExist) {
				rollbackErr = errors.Join(rollbackErr, fmt.Errorf("remove empty installer configuration directory: %w", err))
			}
		}
		if rollbackErr != nil {
			resultErr = fmt.Errorf("%w; installer configuration rollback was incomplete: %v", resultErr, rollbackErr)
		}
	}()

	userPath := filepath.Join(globalRoot, userProvisioningName)
	userCreated, err := seedFileOnceResult(userPath, defaultUserProvisioningScript, "user provisioning script", validateUserProvisioningContract)
	if err != nil {
		return err
	}
	if userCreated {
		created = append(created, seededFile{path: userPath, contents: defaultUserProvisioningScript})
	}
	configurationPath := filepath.Join(globalRoot, globalConfigurationName)
	configurationCreated, err := seedFileOnceResult(configurationPath, defaultGlobalConfiguration, "global workspace config", validateExistingGlobalWorkspaceConfig)
	if err != nil {
		return err
	}
	if configurationCreated {
		created = append(created, seededFile{path: configurationPath, contents: defaultGlobalConfiguration})
	}
	return nil
}

func removeSeededFileIfUnchanged(path string, expected []byte) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect seeded installer file %s: %w", path, err)
	}
	reparse, err := fileInfoIsReparsePoint(info)
	if err != nil {
		return fmt.Errorf("inspect seeded installer file reparse state %s: %w", path, err)
	}
	if reparse || !info.Mode().IsRegular() {
		return fmt.Errorf("seeded installer file changed type and was preserved: %s", path)
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read seeded installer file %s: %w", path, err)
	}
	if !bytes.Equal(contents, expected) {
		return fmt.Errorf("seeded installer file changed content and was preserved: %s", path)
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("remove seeded installer file %s: %w", path, err)
	}
	return nil
}

func seedGlobalProvisioning(globalRoot string) error {
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

func validatePhysicalMappingDoesNotExposeSensitiveRoot(role, identity string) error {
	userProfile := strings.TrimSpace(os.Getenv("USERPROFILE"))
	appData := strings.TrimSpace(os.Getenv("APPDATA"))
	localAppData := strings.TrimSpace(os.Getenv("LOCALAPPDATA"))
	paths := []string{
		filepath.Join(userProfile, ".ssh"),
		filepath.Join(userProfile, ".gnupg"),
		filepath.Join(userProfile, ".aws"),
		filepath.Join(userProfile, ".azure"),
		filepath.Join(userProfile, ".docker"),
		filepath.Join(userProfile, ".kube"),
		filepath.Join(userProfile, ".claude"),
		filepath.Join(userProfile, ".codex"),
		filepath.Join(userProfile, ".copilot"),
		filepath.Join(userProfile, ".config", "gh"),
		filepath.Join(userProfile, ".config", "opencode"),
		filepath.Join(userProfile, ".local", "share", "opencode"),
		filepath.Join(userProfile, ".pi", "agent"),
		filepath.Join(appData, "GitHub CLI"),
		filepath.Join(appData, "Microsoft", "Credentials"),
		filepath.Join(appData, "Microsoft", "Protect"),
		filepath.Join(localAppData, "Microsoft", "Credentials"),
		filepath.Join(localAppData, "Microsoft", "Vault"),
	}
	for _, environment := range []string{"GH_CONFIG_DIR", "CLAUDE_CONFIG_DIR", "CODEX_HOME", "COPILOT_HOME", "PI_CODING_AGENT_DIR"} {
		paths = append(paths, strings.TrimSpace(os.Getenv(environment)))
	}
	if root := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME")); root != "" {
		paths = append(paths, filepath.Join(root, "gh"), filepath.Join(root, "opencode"))
	}
	if root := strings.TrimSpace(os.Getenv("XDG_DATA_HOME")); root != "" {
		paths = append(paths, filepath.Join(root, "opencode"))
	}

	seen := map[string]bool{}
	for _, path := range paths {
		if !filepath.IsAbs(path) {
			continue
		}
		path = filepath.Clean(path)
		key := strings.ToLower(path)
		if seen[key] {
			continue
		}
		seen[key] = true
		if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			return fmt.Errorf("%s: inspect security-sensitive directory %s: %w", role, path, err)
		}
		containsLexically, err := physicalMappingContainsLexicalPath(identity, path)
		if err != nil {
			return fmt.Errorf("%s: resolve lexical parents of security-sensitive directory %s: %w", role, path, err)
		}
		if containsLexically {
			return fmt.Errorf("%s must not expose security-sensitive directory: %s", role, path)
		}
		sensitiveIdentity, err := physicalMappedDirectory(path)
		if err != nil {
			return fmt.Errorf("%s: resolve security-sensitive directory %s: %w", role, path, err)
		}
		if hostPathsOverlap(identity, sensitiveIdentity) {
			return fmt.Errorf("%s must not expose security-sensitive directory: %s", role, path)
		}
	}
	return nil
}

func physicalMappingContainsLexicalPath(identity, path string) (bool, error) {
	for ancestor := filepath.Dir(path); ; {
		ancestorIdentity, err := physicalMappedDirectory(ancestor)
		if err != nil {
			return false, err
		}
		if hostPathContains(identity, ancestorIdentity) {
			return true, nil
		}
		parent := filepath.Dir(ancestor)
		if parent == ancestor {
			return false, nil
		}
		ancestor = parent
	}
}

func seedUserProvisioning(path string) error {
	return seedFileOnce(path, defaultUserProvisioningScript, "user provisioning script", validateUserProvisioningContract)
}

func ensureGlobalWorkspaceConfig(globalRoot string) error {
	path := filepath.Join(globalRoot, globalConfigurationName)
	return seedFileOnce(path, defaultGlobalConfiguration, "global workspace config", validateExistingGlobalWorkspaceConfig)
}

func validateExistingGlobalWorkspaceConfig(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("inspect global workspace config: %w", err)
	}
	reparse, err := fileInfoIsReparsePoint(info)
	if err != nil {
		return fmt.Errorf("inspect global workspace config reparse state: %w", err)
	}
	if reparse || !info.Mode().IsRegular() {
		return fmt.Errorf("global workspace config is not a regular non-reparse file: %s", path)
	}
	return nil
}

func seedFileOnce(path string, contents []byte, role string, validateExisting func(string) error) error {
	_, err := seedFileOnceResult(path, contents, role, validateExisting)
	return err
}

func seedFileOnceResult(path string, contents []byte, role string, validateExisting func(string) error) (bool, error) {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		return false, validateExisting(path)
	}
	if err != nil {
		return false, fmt.Errorf("create %s %s: %w", role, path, err)
	}
	written := false
	defer func() {
		_ = file.Close()
		if !written {
			_ = os.Remove(path)
		}
	}()
	if _, err := file.Write(contents); err != nil {
		return false, fmt.Errorf("write %s %s: %w", role, path, err)
	}
	if err := file.Sync(); err != nil {
		return false, fmt.Errorf("sync %s %s: %w", role, path, err)
	}
	if err := file.Close(); err != nil {
		return false, fmt.Errorf("close %s %s: %w", role, path, err)
	}
	written = true
	return true, nil
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

func guestMountDirectory(name string) string {
	return guestMountsDirectory + `\` + name
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

func mappedDirectoriesOverlap(left, right string) (bool, error) {
	leftIdentity, err := physicalMappedDirectory(left)
	if err != nil {
		return false, err
	}
	rightIdentity, err := physicalMappedDirectory(right)
	if err != nil {
		return false, err
	}
	return hostPathsOverlap(leftIdentity, rightIdentity), nil
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
