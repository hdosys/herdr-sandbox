package sandbox

import (
	"os"
	"path/filepath"
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
		"[string]$Version = '1.61.1'",
		"[string]$PlaywrightVersion = '1.61.1'",
		"node_modules\\npm\\bin\\npm-cli.js",
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
}
