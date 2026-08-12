package sandbox

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

const (
	wingetPackagePlanSchema   = 1
	wingetPackagePlanFileName = "winget-packages.json"

	packagePowerShell      = "Microsoft.PowerShell"
	packageStarship        = "Starship.Starship"
	packageFZF             = "junegunn.fzf"
	packageRipgrep         = "BurntSushi.ripgrep.MSVC"
	packageActionlint      = "rhysd.actionlint"
	packageGit             = "Git.Git"
	packageGitHubCLI       = "GitHub.cli"
	packageTailscale       = "Tailscale.Tailscale"
	packageOpenCode        = "SST.opencode"
	packageClaudeCode      = "Anthropic.ClaudeCode"
	packageCodex           = "OpenAI.Codex"
	packageGitHubCopilot   = "GitHub.Copilot"
	packageVulkanRuntime   = "KhronosGroup.VulkanRT"
	packageWinDirStat      = "WinDirStat.WinDirStat"
	packageFilePilot       = "Voidstar.FilePilot"
	packageTerminalXAML    = "Microsoft.UI.Xaml.2.8"
	packageTerminalStable  = "Microsoft.WindowsTerminal"
	packageTerminalPreview = "Microsoft.WindowsTerminal.Preview"
	packageTradingView     = "TradingView.TradingViewDesktop"
	packageUV              = "astral-sh.uv"
	packageOpenJDK25       = "Microsoft.OpenJDK.25"
	packageNSIS            = "NSIS.NSIS"
	packageCMake           = "Kitware.CMake"
	packageVulkanSDK       = "KhronosGroup.VulkanSDK"
	packageWebView2        = "Microsoft.EdgeWebView2Runtime"
)

var basePackageIDs = []string{
	packagePowerShell,
	packageStarship,
	packageFZF,
	packageRipgrep,
	packageActionlint,
	packageGit,
	packageGitHubCLI,
	packageTailscale,
	packageWinDirStat,
	packageFilePilot,
	packageTerminalXAML,
	packageTerminalStable,
	packageTerminalPreview,
}

var projectStackPackageIDs = []string{
	"Microsoft.DotNet.SDK.10",
	packageOpenJDK25,
	"GoLang.Go",
	"OpenJS.NodeJS.LTS",
	packageNSIS,
	"Oven-sh.Bun",
	"zig.zig",
	"Rustlang.Rustup",
	"nextest.cargo-nextest",
	"Casey.Just",
	packageTradingView,
	packageUV,
	packageCMake,
	packageVulkanSDK,
	packageWebView2,
}

type wingetPackageConfiguration struct {
	Remove   []string          `json:"remove"`
	Add      []string          `json:"add"`
	Versions map[string]string `json:"versions"`
}

type wingetPackagePlan struct {
	SchemaVersion          int                      `json:"schemaVersion"`
	WindowsTerminalEdition string                   `json:"windowsTerminalEdition"`
	Defaults               []wingetPackagePlanEntry `json:"defaults"`
	Additions              []wingetPackagePlanEntry `json:"additions"`
}

type wingetPackagePlanEntry struct {
	ID      string `json:"id"`
	Version string `json:"version"`
}

func defaultWingetPackageConfiguration() wingetPackageConfiguration {
	return wingetPackageConfiguration{Remove: []string{}, Add: defaultCodingAgentPackageIDs(), Versions: map[string]string{}}
}

func defaultCodingAgentPackageIDs() []string {
	return []string{packageOpenCode, packageClaudeCode, packageCodex, packageGitHubCopilot}
}

func resolveWingetPackagePlan(configuration wingetPackageConfiguration, terminal windowsTerminalConfiguration) (wingetPackagePlan, error) {
	if err := terminal.validate(); err != nil {
		return wingetPackagePlan{}, err
	}
	terminalID := terminal.WinGetPackageID
	defaults := map[string]string{}
	for _, id := range []string{
		packagePowerShell,
		packageStarship,
		packageFZF,
		packageRipgrep,
		packageActionlint,
		packageGit,
		packageGitHubCLI,
		packageTailscale,
		packageWinDirStat,
		packageFilePilot,
		packageTerminalXAML,
		terminalID,
	} {
		defaults[strings.ToLower(id)] = id
	}

	seenRemove := map[string]bool{}
	for _, id := range configuration.Remove {
		identity := strings.ToLower(id)
		if seenRemove[identity] {
			return wingetPackagePlan{}, fmt.Errorf("wingetPackages.remove contains duplicate package ID %q", id)
		}
		seenRemove[identity] = true
		canonical, found := defaults[identity]
		if !found {
			return wingetPackagePlan{}, fmt.Errorf("wingetPackages.remove package %q is not an effective Base default", id)
		}
		switch canonical {
		case packagePowerShell:
			return wingetPackagePlan{}, errors.New("wingetPackages.remove must not disable Core package Microsoft.PowerShell")
		case packageTerminalXAML:
			return wingetPackagePlan{}, errors.New("wingetPackages.remove must not disable the Windows Terminal framework independently")
		}
		delete(defaults, identity)
		if canonical == terminalID {
			delete(defaults, strings.ToLower(packageTerminalXAML))
		}
	}

	known := map[string]bool{}
	for _, id := range basePackageIDs {
		known[strings.ToLower(id)] = true
	}
	additions := map[string]string{}
	for _, id := range configuration.Add {
		identity := strings.ToLower(id)
		if known[identity] {
			return wingetPackagePlan{}, fmt.Errorf("wingetPackages.add must not re-add known Base package %q", id)
		}
		if projectStackOwnsPackage(id) {
			return wingetPackagePlan{}, fmt.Errorf("wingetPackages.add package %q is owned by a project stack", id)
		}
		if _, found := additions[identity]; found {
			return wingetPackagePlan{}, fmt.Errorf("wingetPackages.add contains duplicate package ID %q", id)
		}
		if seenRemove[identity] {
			return wingetPackagePlan{}, fmt.Errorf("wingetPackages package %q cannot be both added and removed", id)
		}
		additions[identity] = id
	}

	effective := make(map[string]string, len(defaults)+len(additions))
	for identity, id := range defaults {
		effective[identity] = id
	}
	for identity, id := range additions {
		effective[identity] = id
	}
	versions := map[string]string{}
	seenVersions := map[string]bool{}
	for id, version := range configuration.Versions {
		identity := strings.ToLower(id)
		if seenVersions[identity] {
			return wingetPackagePlan{}, fmt.Errorf("wingetPackages.versions contains duplicate package ID %q", id)
		}
		seenVersions[identity] = true
		canonical, found := effective[identity]
		if !found {
			return wingetPackagePlan{}, fmt.Errorf("wingetPackages.versions package %q is not retained or added", id)
		}
		if canonical == packageTerminalXAML {
			minimum := [4]int{8, 2306, 22001, 0}
			parsed, err := parseFourPartPackageVersion(version)
			if err != nil || comparePackageVersion(parsed, minimum) < 0 {
				return wingetPackagePlan{}, fmt.Errorf("wingetPackages.versions package %s must be at least 8.2306.22001.0", packageTerminalXAML)
			}
		}
		versions[identity] = version
	}

	plan := wingetPackagePlan{
		SchemaVersion:          wingetPackagePlanSchema,
		WindowsTerminalEdition: terminal.Edition,
		Defaults:               make([]wingetPackagePlanEntry, 0, len(defaults)),
		Additions:              make([]wingetPackagePlanEntry, 0, len(additions)),
	}
	for identity, id := range defaults {
		plan.Defaults = append(plan.Defaults, wingetPackagePlanEntry{ID: id, Version: versions[identity]})
	}
	for identity, id := range additions {
		plan.Additions = append(plan.Additions, wingetPackagePlanEntry{ID: id, Version: versions[identity]})
	}
	sortWingetPackageEntries(plan.Defaults)
	sortWingetPackageEntries(plan.Additions)
	if err := plan.validate(terminal); err != nil {
		return wingetPackagePlan{}, err
	}
	return plan, nil
}

func sortWingetPackageEntries(entries []wingetPackagePlanEntry) {
	sort.Slice(entries, func(left, right int) bool {
		return strings.ToLower(entries[left].ID) < strings.ToLower(entries[right].ID)
	})
}

func (plan wingetPackagePlan) validate(terminal windowsTerminalConfiguration) error {
	if plan.SchemaVersion != wingetPackagePlanSchema || plan.WindowsTerminalEdition != terminal.Edition {
		return errors.New("WinGet package plan schema or Windows Terminal edition is inconsistent")
	}
	if len(plan.Defaults) == 0 || len(plan.Defaults) > len(basePackageIDs) || len(plan.Additions) > 64 {
		return errors.New("WinGet package plan package count is invalid")
	}
	known := map[string]bool{}
	for _, id := range basePackageIDs {
		known[strings.ToLower(id)] = true
	}
	seen := map[string]bool{}
	for groupIndex, group := range [][]wingetPackagePlanEntry{plan.Defaults, plan.Additions} {
		for _, entry := range group {
			if !validWingetPackageID(entry.ID) || !validWingetPackageVersion(entry.Version) {
				return fmt.Errorf("WinGet package plan entry is invalid: %q", entry.ID)
			}
			identity := strings.ToLower(entry.ID)
			if (groupIndex == 0) != known[identity] {
				return fmt.Errorf("WinGet package plan entry %q is in the wrong package group", entry.ID)
			}
			if groupIndex == 1 && projectStackOwnsPackage(entry.ID) {
				return fmt.Errorf("WinGet package plan addition %q is owned by a project stack", entry.ID)
			}
			if strings.EqualFold(entry.ID, packageTerminalXAML) && entry.Version != "" {
				parsed, err := parseFourPartPackageVersion(entry.Version)
				if err != nil || comparePackageVersion(parsed, [4]int{8, 2306, 22001, 0}) < 0 {
					return fmt.Errorf("WinGet package plan %s version is below 8.2306.22001.0", packageTerminalXAML)
				}
			}
			if seen[identity] {
				return fmt.Errorf("WinGet package plan contains duplicate package %q", entry.ID)
			}
			seen[identity] = true
		}
	}
	if !seen[strings.ToLower(packagePowerShell)] {
		return errors.New("WinGet package plan is missing Core package Microsoft.PowerShell")
	}
	terminalEnabled := seen[strings.ToLower(terminal.WinGetPackageID)]
	xamlEnabled := seen[strings.ToLower(packageTerminalXAML)]
	if terminalEnabled != xamlEnabled {
		return errors.New("WinGet package plan Windows Terminal framework selection is inconsistent")
	}
	otherTerminal := packageTerminalPreview
	if terminal.WinGetPackageID == packageTerminalPreview {
		otherTerminal = packageTerminalStable
	}
	if seen[strings.ToLower(otherTerminal)] {
		return errors.New("WinGet package plan contains the non-selected Windows Terminal edition")
	}
	return nil
}

func (plan wingetPackagePlan) enabled(id string) bool {
	for _, group := range [][]wingetPackagePlanEntry{plan.Defaults, plan.Additions} {
		for _, entry := range group {
			if strings.EqualFold(entry.ID, id) {
				return true
			}
		}
	}
	return false
}

func encodeWingetPackagePlan(plan wingetPackagePlan, terminal windowsTerminalConfiguration) ([]byte, error) {
	if err := plan.validate(terminal); err != nil {
		return nil, err
	}
	encoded, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode WinGet package plan: %w", err)
	}
	return append(encoded, '\n'), nil
}

func decodeWingetPackageConfiguration(decoder *json.Decoder) (wingetPackageConfiguration, error) {
	opening, err := decoder.Token()
	if err != nil {
		return wingetPackageConfiguration{}, err
	}
	if opening != json.Delim('{') {
		return wingetPackageConfiguration{}, errors.New("must be a JSON object")
	}
	configuration := defaultWingetPackageConfiguration()
	seen := map[string]bool{}
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return wingetPackageConfiguration{}, err
		}
		key, ok := token.(string)
		if !ok {
			return wingetPackageConfiguration{}, errors.New("field name must be a string")
		}
		if seen[key] {
			return wingetPackageConfiguration{}, fmt.Errorf("duplicate field %q", key)
		}
		seen[key] = true
		switch key {
		case "remove":
			configuration.Remove, err = decodeWingetPackageIDArray(decoder, key)
		case "add":
			configuration.Add, err = decodeWingetPackageIDArray(decoder, key)
		case "versions":
			configuration.Versions, err = decodeWingetPackageVersions(decoder)
		default:
			return wingetPackageConfiguration{}, fmt.Errorf("unknown field %q", key)
		}
		if err != nil {
			return wingetPackageConfiguration{}, fmt.Errorf("field %q: %w", key, err)
		}
	}
	closing, err := decoder.Token()
	if err != nil {
		return wingetPackageConfiguration{}, err
	}
	if closing != json.Delim('}') {
		return wingetPackageConfiguration{}, errors.New("object is not closed")
	}
	return configuration, nil
}

func decodeWingetPackageIDArray(decoder *json.Decoder, field string) ([]string, error) {
	opening, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	if opening != json.Delim('[') {
		return nil, errors.New("must be an array")
	}
	values := []string{}
	seen := map[string]bool{}
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return nil, err
		}
		value, ok := token.(string)
		if !ok || !validWingetPackageID(value) {
			return nil, errors.New("contains an invalid WinGet package ID")
		}
		identity := strings.ToLower(value)
		if seen[identity] {
			return nil, fmt.Errorf("contains duplicate package ID %q", value)
		}
		seen[identity] = true
		values = append(values, value)
		if len(values) > 64 {
			return nil, fmt.Errorf("contains more than 64 package IDs in %s", field)
		}
	}
	closing, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	if closing != json.Delim(']') {
		return nil, errors.New("array is not closed")
	}
	return values, nil
}

func decodeWingetPackageVersions(decoder *json.Decoder) (map[string]string, error) {
	opening, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	if opening != json.Delim('{') {
		return nil, errors.New("must be an object")
	}
	versions := map[string]string{}
	seen := map[string]bool{}
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return nil, err
		}
		id, ok := token.(string)
		if !ok || !validWingetPackageID(id) {
			return nil, errors.New("contains an invalid WinGet package ID")
		}
		identity := strings.ToLower(id)
		if seen[identity] {
			return nil, fmt.Errorf("contains duplicate package ID %q", id)
		}
		seen[identity] = true
		value, err := decoder.Token()
		if err != nil {
			return nil, err
		}
		version, ok := value.(string)
		if !ok || !validWingetPackageVersion(version) || version == "" {
			return nil, fmt.Errorf("package %q has an invalid exact version", id)
		}
		versions[id] = version
		if len(versions) > 64 {
			return nil, errors.New("contains more than 64 package versions")
		}
	}
	closing, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	if closing != json.Delim('}') {
		return nil, errors.New("object is not closed")
	}
	return versions, nil
}

func validWingetPackageID(value string) bool {
	if len(value) == 0 || len(value) > 128 || strings.TrimSpace(value) != value {
		return false
	}
	for index, character := range value {
		if (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9') || (index > 0 && (character == '.' || character == '_' || character == '-')) {
			continue
		}
		return false
	}
	return true
}

func projectStackOwnsPackage(id string) bool {
	for _, reserved := range projectStackPackageIDs {
		if strings.EqualFold(id, reserved) {
			return true
		}
	}
	return strings.HasPrefix(strings.ToLower(id), "python.python.")
}

func validWingetPackageVersion(value string) bool {
	if value == "" {
		return true
	}
	if len(value) > 128 || strings.TrimSpace(value) != value {
		return false
	}
	for index, character := range value {
		if (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9') || (index > 0 && strings.ContainsRune("._+-", character)) {
			continue
		}
		return false
	}
	return true
}

func parseFourPartPackageVersion(value string) ([4]int, error) {
	var parsed [4]int
	parts := strings.Split(value, ".")
	if len(parts) != len(parsed) {
		return parsed, errors.New("version must contain four numeric parts")
	}
	for index, part := range parts {
		number, err := strconv.Atoi(part)
		if err != nil || number < 0 {
			return parsed, errors.New("version must contain four numeric parts")
		}
		parsed[index] = number
	}
	return parsed, nil
}

func comparePackageVersion(left, right [4]int) int {
	for index := range left {
		if left[index] < right[index] {
			return -1
		}
		if left[index] > right[index] {
			return 1
		}
	}
	return 0
}
