package sandbox

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHerdrVirtualStackComposesMaintainedProjectRequirements(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "provisioning", stackProvisioningName))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	start := strings.Index(text, "function Install-HerdrStack")
	if start < 0 {
		t.Fatal("Herdr virtual stack is missing")
	}
	section := text[start:]
	requiredInOrder := []string{
		"Cargo.toml",
		"Install-PythonStack -Series '3.13'",
		"C:\\HerdrSandbox\\tools\\herdr\\bin",
		"Copy-Item -LiteralPath $python.Source -Destination $python3 -Force",
		"Herdr Python 3 command ready:",
		"Install-ZigStack -Version '0.15.2'",
		"Install-RustMSVCStack -ProjectDirectory $projectRoot",
		"Add-ProvisioningMachinePath -Directory $gitShellDirectory",
		"Herdr POSIX shell ready:",
		"C:\\HerdrSandbox\\build\\cargo-target",
		"LIBGHOSTTY_VT_ZIG_OUT_DIR",
		"Install-BunStack",
		"Install-CargoNextest",
		"Install-Just",
		"Herdr development toolchain ready.",
	}
	last := -1
	for _, required := range requiredInOrder {
		index := strings.Index(section, required)
		if index <= last {
			t.Fatalf("Herdr virtual stack is missing ordered requirement %q", required)
		}
		last = index
	}
	for _, forbidden := range []string{
		"Install-ProvisioningWinGetPackage",
		"Install-DotNetStack",
		"Install-GoStack",
		"Install-NodeStack",
	} {
		if strings.Contains(section, forbidden) {
			t.Fatalf("Herdr virtual stack duplicates or adds unrelated owner %q", forbidden)
		}
	}
	if effectiveStackPackageOwner(stackBun) != "Oven-sh.Bun" || !projectStackOwnsPackage("oven-sh.bun") {
		t.Fatal("Bun is not owned by the existing project-stack package path")
	}
	for _, required := range []string{"Get-FileHash -LiteralPath $python3 -Algorithm SHA256", "[regex]::Escape($pythonVersion)"} {
		if !strings.Contains(section, required) {
			t.Fatalf("Herdr Python 3 command does not enforce %q", required)
		}
	}
}
