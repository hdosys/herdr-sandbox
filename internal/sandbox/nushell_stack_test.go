package sandbox

import (
	"strings"
	"testing"
)

func TestNushellStackInstallsLatestStableMachineMSIAndVerifiesCommand(t *testing.T) {
	text := readDefaultStackProvisioning(t)
	start := strings.Index(text, "function Install-NushellStack")
	end := strings.Index(text, "function Install-GoStack")
	if start < 0 || end <= start {
		t.Fatal("Nushell stack owner is missing")
	}
	section := text[start:end]
	for _, required := range []string{
		"$packageID = 'Nushell.Nushell'",
		"Get-ProvisioningToolVersion -Tool $packageID -Requested $Version",
		"-Architecture 'x64' -InstallerType 'wix' -Scope 'machine'",
		"-DownloadSource 'WinGet'",
		"-Adapter 'MSI' -ExecutableName 'nu.exe' -InstallerArguments @('ALLUSERS=1')",
		"Join-Path $env:ProgramFiles 'nu\\bin\\nu.exe'",
		"Get-Command 'nu.exe' -CommandType Application",
		"Nushell version verification",
		"-ArgumentList @('--version') -TimeoutSeconds 30",
		"$nuVersion -cne $Version",
	} {
		if !strings.Contains(section, required) {
			t.Errorf("Nushell stack is missing %q", required)
		}
	}
	for _, forbidden := range []string{"0.114.1", "cargo install", "rustup", "Invoke-WebRequest", ".nu'", ".nu\""} {
		if strings.Contains(section, forbidden) {
			t.Errorf("Nushell stack contains a pin, alternate installer, or script path %q", forbidden)
		}
	}
	if effectiveStackPackageOwner(stackNushell) != packageNushell || !projectStackOwnsPackage(packageNushell) {
		t.Fatalf("Nushell package owner = %q, reserved = %t", effectiveStackPackageOwner(stackNushell), projectStackOwnsPackage(packageNushell))
	}
}
