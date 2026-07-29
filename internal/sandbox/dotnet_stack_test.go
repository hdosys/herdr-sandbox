package sandbox

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestModernDotNetStackUsesOnlyCurrentLTSWinGetOwner(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "provisioning", stackProvisioningName))
	if err != nil {
		t.Fatal(err)
	}
	source := string(data)
	for _, required := range []string{
		"function Install-DotNetStack",
		"Microsoft.DotNet.SDK.10",
		"Get-ProvisioningWinGetMetadata",
		"Install-ProvisioningCachedPackage",
		"-InstallerArguments @('/install', '/quiet', '/norestart')",
		"-InstallerSuccessExitCodes @(0, 3010)",
		`C:\Program Files\dotnet\dotnet.exe`,
		"Push-Location -LiteralPath $verificationDirectory",
		"--list-sdks",
		`C:\\Program Files\\dotnet\\sdk`,
	} {
		if !strings.Contains(source, required) {
			t.Fatalf("modern .NET stack is missing %q", required)
		}
	}
	start := strings.Index(source, "function Install-DotNetStack")
	end := strings.Index(source[start:], "function Install-GoStack")
	if start < 0 || end < 0 {
		t.Fatal("modern .NET stack section is missing")
	}
	section := source[start : start+end]
	for _, forbidden := range []string{
		"DotNet.Framework", "NETFramework", "Install-DotNetFramework", "Microsoft.DotNet.SDK.8",
		"Microsoft.DotNet.SDK.9", "Microsoft.DotNet.SDK.Preview", "MSBuild", "Visual Studio",
		"dotnet-install.ps1",
	} {
		if strings.Contains(section, forbidden) {
			t.Fatalf("modern .NET stack contains legacy or alternate path %q", forbidden)
		}
	}
}

func TestModernDotNetInstallerAcceptsOnlyDocumentedRestartSuccess(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "provisioning", baseProvisioningName))
	if err != nil {
		t.Fatal(err)
	}
	source := string(data)
	for _, required := range []string{"[int[]]$SuccessExitCodes = @(0)", "$acceptedExitCodes", "-SuccessExitCodes $InstallerSuccessExitCodes"} {
		if !strings.Contains(source, required) {
			t.Fatalf("native installer exit-code owner is missing %q", required)
		}
	}
}

func TestProjectStackPackageOwnershipReservesModernDotNetSDK(t *testing.T) {
	for _, id := range []string{"Microsoft.DotNet.SDK.10", "microsoft.dotnet.sdk.10"} {
		if !projectStackOwnsPackage(id) {
			t.Fatalf("modern .NET package is not stack-owned: %s", id)
		}
	}
	for _, id := range []string{"Microsoft.DotNet.SDK.8", "Microsoft.DotNet.SDK.9", "Microsoft.DotNet.SDK.Preview"} {
		if projectStackOwnsPackage(id) {
			t.Fatalf("legacy or preview .NET package unexpectedly became a stack path: %s", id)
		}
	}
}
