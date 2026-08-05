package sandbox

import (
	"strings"
	"testing"
)

func TestHandyVirtualStackOwnsExactWindowsDevelopmentRequirements(t *testing.T) {
	text := readDefaultStackProvisioning(t)
	start := strings.Index(text, "function Install-HandyStack")
	end := strings.Index(text, "function Install-HerdrStack")
	if start < 0 || end <= start {
		t.Fatal("Handy virtual stack section is missing")
	}
	section := text[start:end]
	requiredInOrder := []string{
		"package.json",
		"bun.lock",
		"src-tauri",
		"resources\\models\\silero_vad_v4.onnx",
		"'handy-app'",
		"$package.private -isnot [bool]",
		"-Id 'Kitware.CMake'",
		"ADD_CMAKE_TO_PATH=System",
		"$vulkanVersion = '1.4.309.0'",
		"-Id 'KhronosGroup.VulkanSDK'",
		"-Id 'Microsoft.EdgeWebView2Runtime'",
		"Install-BunStack",
		"Install-RustMSVCStack -ProjectDirectory $rustRoot",
		"Write-HandySPIRVHeadersPackage",
		"Assert-HandyNativeToolchain",
		"Handy development toolchain ready.",
	}
	last := -1
	for _, required := range requiredInOrder {
		index := strings.Index(section, required)
		if index <= last {
			t.Fatalf("Handy virtual stack is missing ordered requirement %q", required)
		}
		last = index
	}
	for _, forbidden := range []string{
		"Install-NodeStack", "Install-PythonStack", "vcpkg", "ORT_LIB_LOCATION",
		"trusted-signing-cli", "bun install", "Invoke-WebRequest",
	} {
		if strings.Contains(section, forbidden) {
			t.Fatalf("Handy virtual stack added project or release responsibility %q", forbidden)
		}
	}
	if effectiveStackPackageOwner(stackHandy) != "Kitware.CMake + KhronosGroup.VulkanSDK 1.4.309.0 + Microsoft.EdgeWebView2Runtime" {
		t.Fatal("Handy native package ownership is not explicit")
	}
}

func TestHandySPIRVPackageCorrectsPinnedSDKConfigWithoutVcpkg(t *testing.T) {
	text := readDefaultStackProvisioning(t)
	start := strings.Index(text, "function Write-HandySPIRVHeadersPackage")
	end := strings.Index(text, "function Assert-HandyNativeToolchain")
	if start < 0 || end <= start {
		t.Fatal("Handy SPIRV CMake owner is missing")
	}
	section := text[start:end]
	for _, required := range []string{
		"$vulkanBase = 'C:\\VulkanSDK'",
		"@($vulkanBase, $vulkanRootFull, $include, $spirvInclude)",
		"$toolsOwner = 'C:\\HerdrSandbox'",
		"@($toolsOwner, $toolsRoot, $prefix",
		"$header = Join-Path $spirvInclude 'spirv.hpp'",
		"SPIRV-HeadersConfig.cmake",
		"SPIRV-Headers::SPIRV-Headers",
		"INTERFACE_INCLUDE_DIRECTORIES",
		"CMAKE_PREFIX_PATH",
		"C:\\HerdrSandbox\\tools\\handy-cmake-prefix",
	} {
		if !strings.Contains(section, required) {
			t.Fatalf("Handy SPIRV package is missing %q", required)
		}
	}
	for _, forbidden := range []string{"vcpkg", "C:\\VulkanSDK\\include", "Invoke-WebRequest"} {
		if strings.Contains(section, forbidden) {
			t.Fatalf("Handy SPIRV package retains alternate path %q", forbidden)
		}
	}
	destinationCheck := strings.Index(section, "if (Test-Path -LiteralPath $config)")
	write := strings.Index(section, "[IO.File]::WriteAllText($config")
	if destinationCheck < 0 || write <= destinationCheck {
		t.Fatal("Handy SPIRV package does not reject an existing unsafe destination before writing")
	}
}

func TestProvisioningEXEAdapterRequiresExactArguments(t *testing.T) {
	base := readDefaultBaseProvisioning(t)
	for _, required := range []string{
		"ValidateSet('Exe', 'Inno'",
		"$Role EXE adapter requires explicit installer arguments.",
		"-ArgumentList $InstallerArguments",
		"-SuccessExitCodes $InstallerSuccessExitCodes",
		"-WaitForProcessTree",
	} {
		if !strings.Contains(base, required) {
			t.Fatalf("EXE adapter is missing %q", required)
		}
	}
}
