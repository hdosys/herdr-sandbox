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

func TestHandyNativeProbeSeparatesCMakeAndCompilerBoundaries(t *testing.T) {
	text := readDefaultStackProvisioning(t)
	start := strings.Index(text, "function Assert-HandyNativeToolchain")
	end := strings.Index(text, "function Install-HandyStack")
	if start < 0 || end <= start {
		t.Fatal("Handy native toolchain probe is missing")
	}
	section := text[start:end]
	for _, required := range []string{
		"$environment = Enable-StackVisualStudioDeveloperEnvironment",
		"project(handy_stack_probe LANGUAGES NONE)",
		"find_package(Vulkan COMPONENTS glslc REQUIRED)",
		"find_package(SPIRV-Headers CONFIG REQUIRED)",
		"TARGET Vulkan::Headers",
		"TARGET SPIRV-Headers::SPIRV-Headers",
		"Handy native CMake configuration",
		"'NMake Makefiles'",
		"Handy native C++ compilation",
		"-FilePath $environment.Compiler",
		"-TerminateDescendantsAfterRootExit",
		"'/Z7'",
		"'/c'",
		`"/I$vulkanInclude"`,
		`"/Fo:$object"`,
		"Handy native C++ linking",
		"-FilePath $environment.Linker",
		"'/NOLOGO', '/DEBUG:NONE'",
		`"/OUT:$probe"`,
		"Handy native C++ execution",
	} {
		if !strings.Contains(section, required) {
			t.Fatalf("Handy native probe is missing separated boundary contract %q", required)
		}
	}
	if count := strings.Count(section, "-TimeoutSeconds 30"); count != 3 {
		t.Fatalf("Handy native probe has %d thirty-second deadlines; want 3", count)
	}
	if count := strings.Count(section, "-TimeoutSeconds 60"); count != 1 {
		t.Fatalf("Handy native probe has %d one-minute deadlines; want 1", count)
	}
	if count := strings.Count(section, "-TerminateDescendantsAfterRootExit"); count != 2 {
		t.Fatalf("Handy native probe has %d root-terminal compiler/linker boundaries; want 2", count)
	}
	for _, forbidden := range []string{
		"Visual Studio 17 2022",
		"MSBUILDDISABLENODEREUSE",
		"CMAKE_TRY_COMPILE_CONFIGURATION",
		"add_executable(handy_stack_probe",
		"target_link_libraries(handy_stack_probe",
		"Handy native CMake build",
		"'-DCMAKE_BUILD_TYPE=Release'",
		`"/Fe:$probe"`,
		"'/link'",
	} {
		if strings.Contains(section, forbidden) {
			t.Fatalf("Handy native probe retains compiler-enabled CMake path %q", forbidden)
		}
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
