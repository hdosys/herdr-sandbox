package sandbox

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestNodeStackOwnsVersionedGuestLocalPlaywrightChromium(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "provisioning", stackProvisioningName))
	if err != nil {
		t.Fatal(err)
	}
	source := string(data)
	playwrightStart := strings.Index(source, "function Install-PlaywrightChromium")
	nodeStart := strings.Index(source, "function Install-NodeStack")
	pythonStart := strings.Index(source, "function Resolve-StackPythonPackage")
	if playwrightStart < 0 || nodeStart <= playwrightStart || pythonStart <= nodeStart {
		t.Fatal("Node and Playwright stack sections are missing or out of order")
	}
	section := source[playwrightStart:pythonStart]
	for _, required := range []string{
		"[string]$Version = ''",
		"[string]$PlaywrightVersion = ''",
		"node_modules\\npm\\bin\\npm-cli.js",
		"$env:npm_config_cache = $npmCache",
		"Playwright latest version resolution",
		"@($npmCLI, 'view', 'playwright@latest', 'version', '--json')",
		"C:\\HerdrSandbox\\tools\\playwright",
		"C:\\HerdrSandbox\\tools\\playwright-browsers",
		"--ignore-scripts",
		"--omit=optional",
		"--no-bin-links",
		"playwright@$Version",
		"@($playwrightCLI, 'install', 'chromium')",
		"@($playwrightCLI, 'screenshot', '-b', 'chromium', 'about:blank', $screenshotPath)",
		"SetEnvironmentVariable('PLAYWRIGHT_BROWSERS_PATH'",
		"Install-PlaywrightChromium -Version $PlaywrightVersion",
	} {
		if !strings.Contains(section, required) {
			t.Fatalf("Node stack is missing %q", required)
		}
	}
	for _, forbidden := range []string{"npm ci", "package-lock.json", "--with-deps", "'firefox'", "'webkit'", "npm.cmd", "npx.cmd"} {
		if strings.Contains(strings.ToLower(section), strings.ToLower(forbidden)) {
			t.Fatalf("Node stack contains a second Playwright package owner %q", forbidden)
		}
	}
	cacheSetup := strings.Index(section, "$env:npm_config_cache = $npmCache")
	latestResolution := strings.Index(section, "Playwright latest version resolution")
	if cacheSetup < 0 || latestResolution < 0 || cacheSetup > latestResolution {
		t.Fatal("Playwright latest resolution can write outside the guest-local npm cache")
	}
	if pin := regexp.MustCompile(`playwright@\d+\.\d+\.\d+`).FindString(section); pin != "" {
		t.Fatalf("Node stack contains an agent-selected default Playwright pin %q", pin)
	}
}
