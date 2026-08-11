package sandbox

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestResolveWingetPackagePlanDefaultsAndCustomization(t *testing.T) {
	terminal := testStableWindowsTerminalConfiguration()
	defaults, err := resolveWingetPackagePlan(defaultWingetPackageConfiguration(), terminal)
	if err != nil {
		t.Fatalf("resolve defaults: %v", err)
	}
	for _, id := range []string{
		packagePowerShell, packageStarship, packageFZF, packageRipgrep, packageGit,
		packageActionlint, packageGitHubCLI, packageTailscale, packageWinDirStat, packageFilePilot,
		packageTerminalXAML, packageTerminalStable,
	} {
		if !defaults.enabled(id) {
			t.Fatalf("default plan is missing %s: %#v", id, defaults)
		}
	}
	if defaults.enabled(packageTerminalPreview) {
		t.Fatal("default stable plan contains Terminal Preview")
	}
	if len(defaults.Additions) != len(defaultCodingAgentPackageIDs()) {
		t.Fatalf("default optional additions = %#v", defaults.Additions)
	}
	for _, id := range defaultCodingAgentPackageIDs() {
		if !defaults.enabled(id) {
			t.Fatalf("default plan is missing coding agent %s: %#v", id, defaults.Additions)
		}
	}

	configuration := wingetPackageConfiguration{
		Remove: []string{packageTailscale, packageTerminalStable},
		Add:    []string{"7zip.7zip"},
		Versions: map[string]string{
			packageGit:  "2.55.0",
			"7ZIP.7ZIP": "26.00",
		},
	}
	custom, err := resolveWingetPackagePlan(configuration, terminal)
	if err != nil {
		t.Fatalf("resolve customization: %v", err)
	}
	for _, id := range []string{packageTailscale, packageTerminalXAML, packageTerminalStable} {
		if custom.enabled(id) {
			t.Fatalf("custom plan unexpectedly retained %s", id)
		}
	}
	if !custom.enabled("7zip.7zip") || len(custom.Additions) != 1 || custom.Additions[0].Version != "26.00" {
		t.Fatalf("custom additions = %#v", custom.Additions)
	}
	for _, id := range defaultCodingAgentPackageIDs() {
		if custom.enabled(id) {
			t.Fatalf("explicit additions retained default coding agent %s: %#v", id, custom.Additions)
		}
	}
	gitVersion := ""
	for _, entry := range custom.Defaults {
		if entry.ID == packageGit {
			gitVersion = entry.Version
		}
	}
	if gitVersion != "2.55.0" {
		t.Fatalf("Git version = %q", gitVersion)
	}
	encoded, err := encodeWingetPackagePlan(custom, terminal)
	if err != nil {
		t.Fatalf("encode package plan: %v", err)
	}
	if !bytes.HasSuffix(encoded, []byte("\n")) || !bytes.Contains(encoded, []byte(`"windowsTerminalEdition": "stable"`)) {
		t.Fatalf("encoded plan = %s", encoded)
	}
}

func TestResolveWingetPackagePlanTreatsVulkanRuntimeAsOptionalAddition(t *testing.T) {
	terminal := testStableWindowsTerminalConfiguration()
	defaults, err := resolveWingetPackagePlan(defaultWingetPackageConfiguration(), terminal)
	if err != nil {
		t.Fatalf("resolve defaults: %v", err)
	}
	if defaults.enabled(packageVulkanRuntime) {
		t.Fatal("default package plan unexpectedly enables experimental Vulkan")
	}

	configuration := wingetPackageConfiguration{
		Remove: []string{},
		Add:    []string{packageVulkanRuntime},
		Versions: map[string]string{
			packageVulkanRuntime: "1.4.350.0",
		},
	}
	custom, err := resolveWingetPackagePlan(configuration, terminal)
	if err != nil {
		t.Fatalf("resolve Vulkan opt-in: %v", err)
	}
	if !custom.enabled(packageVulkanRuntime) || len(custom.Additions) != 1 ||
		custom.Additions[0].ID != packageVulkanRuntime || custom.Additions[0].Version != "1.4.350.0" {
		t.Fatalf("Vulkan opt-in package plan = %#v", custom)
	}
}

func TestResolveWingetPackagePlanRejectsConflicts(t *testing.T) {
	terminal := testStableWindowsTerminalConfiguration()
	tests := map[string]wingetPackageConfiguration{
		"disable Core": {
			Remove: []string{packagePowerShell}, Versions: map[string]string{},
		},
		"remove unknown": {
			Remove: []string{"Example.Unknown"}, Versions: map[string]string{},
		},
		"remove framework": {
			Remove: []string{packageTerminalXAML}, Versions: map[string]string{},
		},
		"re-add known": {
			Add: []string{packageTailscale}, Versions: map[string]string{},
		},
		"bypass project stack": {
			Add: []string{"GoLang.Go"}, Versions: map[string]string{},
		},
		"bypass TradingView stack": {
			Add: []string{packageTradingView}, Versions: map[string]string{},
		},
		"bypass uv stack": {
			Add: []string{packageUV}, Versions: map[string]string{},
		},
		"bypass Java stack case-insensitively": {
			Add: []string{"mIcRoSoFt.OpEnJdK.25"}, Versions: map[string]string{},
		},
		"bypass Handy CMake stack": {
			Add: []string{packageCMake}, Versions: map[string]string{},
		},
		"bypass Handy Vulkan SDK stack": {
			Add: []string{packageVulkanSDK}, Versions: map[string]string{},
		},
		"bypass Handy WebView2 stack": {
			Add: []string{packageWebView2}, Versions: map[string]string{},
		},
		"version removed": {
			Remove: []string{packageGit}, Versions: map[string]string{packageGit: "2.55.0"},
		},
		"version unknown": {
			Versions: map[string]string{"Example.Unknown": "1.0.0"},
		},
		"old terminal framework": {
			Versions: map[string]string{packageTerminalXAML: "8.2306.22000.0"},
		},
		"case duplicate versions": {
			Versions: map[string]string{packageGit: "2.55.0", "git.git": "2.54.0"},
		},
	}
	for name, configuration := range tests {
		t.Run(name, func(t *testing.T) {
			if configuration.Remove == nil {
				configuration.Remove = []string{}
			}
			if configuration.Add == nil {
				configuration.Add = []string{}
			}
			if configuration.Versions == nil {
				configuration.Versions = map[string]string{}
			}
			if _, err := resolveWingetPackagePlan(configuration, terminal); err == nil {
				t.Fatalf("conflicting configuration unexpectedly succeeded: %#v", configuration)
			}
		})
	}
}

func TestDecodeWingetPackageConfigurationIsStrict(t *testing.T) {
	valid := `{"remove":["Tailscale.Tailscale"],"add":["7zip.7zip"],"versions":{"7ZIP.7ZIP":"26.00"}}`
	decoder := json.NewDecoder(strings.NewReader(valid))
	configuration, err := decodeWingetPackageConfiguration(decoder)
	if err != nil {
		t.Fatalf("decode valid package configuration: %v", err)
	}
	if len(configuration.Remove) != 1 || len(configuration.Add) != 1 || configuration.Versions["7ZIP.7ZIP"] != "26.00" {
		t.Fatalf("configuration = %#v", configuration)
	}

	decoder = json.NewDecoder(strings.NewReader(`{"add":[]}`))
	configuration, err = decodeWingetPackageConfiguration(decoder)
	if err != nil {
		t.Fatalf("decode empty replacement additions: %v", err)
	}
	if len(configuration.Add) != 0 {
		t.Fatalf("explicit empty additions retained defaults: %#v", configuration.Add)
	}

	invalid := map[string]string{
		"null":                   `null`,
		"unknown field":          `{"extra":[]}`,
		"duplicate field":        `{"add":[],"add":[]}`,
		"wrong add type":         `{"add":{}}`,
		"non-string ID":          `{"remove":[1]}`,
		"invalid ID":             `{"add":["bad id"]}`,
		"case duplicate add":     `{"add":["Example.Tool","example.tool"]}`,
		"case duplicate version": `{"versions":{"Example.Tool":"1.0.0","example.tool":"2.0.0"}}`,
		"empty version":          `{"versions":{"Example.Tool":""}}`,
		"unsafe version":         `{"versions":{"Example.Tool":"1.0;bad"}}`,
	}
	for name, contents := range invalid {
		t.Run(name, func(t *testing.T) {
			decoder := json.NewDecoder(strings.NewReader(contents))
			if _, err := decodeWingetPackageConfiguration(decoder); err == nil {
				t.Fatalf("invalid package configuration unexpectedly succeeded: %s", contents)
			}
		})
	}
}

func testStableWindowsTerminalConfiguration() windowsTerminalConfiguration {
	return windowsTerminalConfiguration{
		Edition:           windowsTerminalStableEdition,
		Theme:             windowsTerminalDarkTheme,
		WinGetPackageID:   packageTerminalStable,
		PackageFamilyName: "Microsoft.WindowsTerminal_8wekyb3d8bbwe",
	}
}
