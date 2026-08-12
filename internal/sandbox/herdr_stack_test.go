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
		"Install-ZigStack -Version '0.15.2'",
		"Install-RustMSVCStack -ProjectDirectory $projectRoot",
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
		"python3.exe",
		"gitShellDirectory",
	} {
		if strings.Contains(section, forbidden) {
			t.Fatalf("Herdr virtual stack duplicates or adds unrelated owner %q", forbidden)
		}
	}
	if effectiveStackPackageOwner(stackBun) != "Oven-sh.Bun" || !projectStackOwnsPackage("oven-sh.bun") {
		t.Fatal("Bun is not owned by the existing project-stack package path")
	}
	if effectiveStackPackageOwner(stackGitSH) != packageGit {
		t.Fatal("Herdr Git shell requirement is not owned by the Base Git package")
	}
}

func TestPythonStackOwnsAppLocalPythonCommands(t *testing.T) {
	text := readDefaultStackProvisioning(t)
	start := strings.Index(text, "function Install-PythonStack")
	if start < 0 {
		t.Fatal("Python stack section is missing")
	}
	end := strings.Index(text[start:], "function Install-ZigStack")
	if end < 0 {
		t.Fatal("Python stack section end is missing")
	}
	section := text[start : start+end]
	last := -1
	for _, required := range []string{
		"C:\\HerdrSandbox\\tools\\python\\bin",
		"$pythonAlias = Join-Path $pythonAliasDirectory 'python.exe'",
		"$python3 = Join-Path $pythonAliasDirectory 'python3.exe'",
		"$pythonSourceExclusions = @('*\\Microsoft\\WindowsApps\\python.exe', $pythonAlias)",
		"$pythonPath = Wait-ProvisioningCommandAvailable",
		"-CommandSourceExclusion $pythonSourceExclusions",
		"Python ready:",
		"$pythonHash = (Get-FileHash -LiteralPath $pythonPath -Algorithm SHA256).Hash",
		"foreach ($pythonCommand in @($pythonAlias, $python3))",
		"Copy-Item -LiteralPath $pythonPath -Destination $pythonCommand -Force",
		"$resolvedPython = Wait-ProvisioningCommandAvailable",
		"[regex]::Escape($pythonVersion)",
		"$resolvedPython3 = Wait-ProvisioningCommandAvailable",
		"App-local Python command ready:",
		"Python 3 command ready:",
	} {
		index := strings.Index(section, required)
		if index <= last {
			t.Fatalf("Python stack is missing ordered Python 3 command requirement %q", required)
		}
		last = index
	}
	if !strings.Contains(section, "Get-FileHash -LiteralPath $pythonCommand -Algorithm SHA256") {
		t.Fatal("Python stack does not verify the copied app-local commands")
	}
	if strings.Contains(section, "tools\\herdr") || strings.Contains(section, "Herdr Python") {
		t.Fatal("generic Python command compatibility remains Herdr-specific")
	}
}

func TestBaseGitOwnsGitForWindowsShell(t *testing.T) {
	text := readDefaultBaseProvisioning(t)
	start := strings.LastIndex(text, "if (Test-ProvisioningPackageEnabled -Id 'Git.Git')")
	if start < 0 {
		t.Fatal("Base Git verification section is missing")
	}
	section := text[start:]
	last := -1
	for _, required := range []string{
		"$gitCommandDirectoryName = Split-Path -Leaf $gitCommandDirectory",
		"$gitCommandDirectoryName -notin @('cmd', 'bin')",
		"$gitShellDirectory = Join-Path $gitRoot 'bin'",
		"$gitShell = Join-Path $gitShellDirectory 'sh.exe'",
		"Add-ProvisioningMachinePath -Directory $gitShellDirectory",
		"$resolvedGitShell = Wait-ProvisioningCommandAvailable",
		"Git for Windows shell ready:",
	} {
		index := strings.Index(section, required)
		if index <= last {
			t.Fatalf("Base Git is missing ordered shell requirement %q", required)
		}
		last = index
	}
	for _, required := range []string{"[IO.FileAttributes]::ReparsePoint", "^GNU bash, version"} {
		if !strings.Contains(section, required) {
			t.Fatalf("Base Git shell does not enforce %q", required)
		}
	}
}
