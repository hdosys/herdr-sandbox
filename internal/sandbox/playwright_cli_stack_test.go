package sandbox

import (
	"strings"
	"testing"
)

func TestPlaywrightCLIStackIsSeparateFromNodeChromiumAndReusesNodeRuntime(t *testing.T) {
	source := readDefaultStackProvisioning(t)
	runtimeStart := strings.Index(source, "function Install-NodeRuntime")
	chromiumStart := strings.Index(source, "function Install-PlaywrightChromium")
	nodeStart := strings.Index(source, "function Install-NodeStack")
	cliStart := strings.Index(source, "function Install-PlaywrightCLIStack")
	pythonStart := strings.Index(source, "function Resolve-StackPythonPackage")
	if runtimeStart < 0 || chromiumStart <= runtimeStart || nodeStart <= chromiumStart || cliStart <= nodeStart || pythonStart <= cliStart {
		t.Fatalf("Node and Playwright stack ordering is invalid: runtime=%d chromium=%d node=%d cli=%d python=%d", runtimeStart, chromiumStart, nodeStart, cliStart, pythonStart)
	}
	runtimeSection := source[runtimeStart:chromiumStart]
	nodeSection := source[nodeStart:cliStart]
	cliSection := source[cliStart:pythonStart]
	if strings.Count(runtimeSection, "-Id 'OpenJS.NodeJS.LTS'") != 1 {
		t.Fatal("shared Node runtime does not own exactly one Node.js package path")
	}
	for name, section := range map[string]string{"node": nodeSection, "playwright-cli": cliSection} {
		if !strings.Contains(section, "Install-NodeRuntime") {
			t.Fatalf("%s stack bypasses the shared Node runtime", name)
		}
	}
	if !strings.Contains(nodeSection, "Install-PlaywrightChromium") {
		t.Fatal("Node stack lost its existing Playwright Chromium owner")
	}
	if strings.Contains(cliSection, "Install-PlaywrightChromium") {
		t.Fatal("Playwright CLI stack unexpectedly installs managed Chromium")
	}
}

func TestPlaywrightCLIStackPinsApprovedCLIAndRegistersOfficialExtension(t *testing.T) {
	source := readDefaultStackProvisioning(t)
	start := strings.Index(source, "function Install-PlaywrightCLIStack")
	end := strings.Index(source, "function Resolve-StackPythonPackage")
	if start < 0 || end <= start {
		t.Fatal("Playwright CLI stack section is missing")
	}
	section := source[start:end]
	for _, required := range []string{
		"[ValidateSet('0.1.17')]",
		"[string]$Version = '0.1.17'",
		"$playwrightVersion = '1.62.0-alpha-1783623505000'",
		"C:\\HerdrSandbox\\tools\\playwright-cli",
		"PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD",
		"$env:NO_UPDATE_NOTIFIER = '1'",
		"[Environment]::SetEnvironmentVariable('NO_UPDATE_NOTIFIER', '1', 'Machine')",
		"[Environment]::GetEnvironmentVariable('NO_UPDATE_NOTIFIER', 'Machine') -cne '1'",
		"'--global'",
		"'--ignore-scripts'",
		"'--omit=optional'",
		"\"@playwright/cli@$Version\"",
		"node_modules\\@playwright\\cli",
		"playwright-cli.cmd",
		"Remove-Item -LiteralPath $powerShellShim -Force",
		"mmlmfjhmonkocbjadbfplnigmagldckm",
		"https://clients2.google.com/service/update2/crx",
		"HKLM:\\SOFTWARE\\Wow6432Node\\Microsoft\\Edge\\Extensions",
		"PLAYWRIGHT_MCP_EXTENSION_TOKEN",
		"playwright-cli.cmd -s=edge-main attach --extension=msedge",
	} {
		if !strings.Contains(section, required) {
			t.Fatalf("Playwright CLI stack is missing %q", required)
		}
	}
	for _, forbidden := range []string{
		"install-browser",
		"playwright-browsers",
		"'install', 'chromium'",
		"--persistent",
		"--profile",
		"ExtensionInstallForcelist",
		"ExtensionSettings",
		"cgpolhlgjngbfkpcgmjcpnakpmanjndf",
	} {
		if strings.Contains(section, forbidden) {
			t.Fatalf("Playwright CLI stack contains forbidden browser or extension path %q", forbidden)
		}
	}
}

func TestPlaywrightCLIStackPlanningUsesItsExactPackageOwner(t *testing.T) {
	if !stackPlaywrightCLI.valid() {
		t.Fatal("Playwright CLI stack is not a valid inspected stack")
	}
	if got := effectiveStackPackageOwner(stackPlaywrightCLI); got != "OpenJS.NodeJS.LTS + @playwright/cli@0.1.17" {
		t.Fatalf("Playwright CLI package owner = %q", got)
	}
}
