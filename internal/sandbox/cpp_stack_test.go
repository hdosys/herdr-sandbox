package sandbox

import (
	"strings"
	"testing"
)

func TestCppStackReusesCurrentVisualStudioBuildToolsAndVerifiesCAndCpp(t *testing.T) {
	text := readDefaultStackProvisioning(t)
	start := strings.Index(text, "function Enable-StackVisualStudioDeveloperEnvironment")
	end := strings.Index(text, "function Get-StackRustSHA256")
	if start < 0 || end <= start {
		t.Fatal("C/C++ stack owner is missing")
	}
	section := text[start:end]
	for _, required := range []string{
		"Install-StackVisualStudioBuildTools",
		"Enable-StackVisualStudioDeveloperEnvironment",
		"Assert-StackCppToolchain",
		"Launch-VsDevShell.ps1",
		"-Arch 'amd64' -HostArch 'amd64' -SkipAutomaticLocation",
		"'INCLUDE', 'LIB', 'LIBPATH'",
		"'cl.exe' = $installationRoot",
		"'link.exe' = $installationRoot",
		"'msbuild.exe' = $installationRoot",
		"'rc.exe' = $windowsKitsRoot",
		"MSVC C compiler probe",
		"MSVC C++ compiler probe",
		"'/TC'",
		"'/TP'",
		"c-stack-ok",
		"cpp-stack-ok",
	} {
		if !strings.Contains(section, required) {
			t.Errorf("C/C++ stack is missing %q", required)
		}
	}
	for _, forbidden := range []string{"LLVM.LLVM", "MSYS2.MSYS2", "mingw", "Install-ProvisioningWinGetPackage"} {
		if strings.Contains(section, forbidden) {
			t.Errorf("C/C++ stack adds a second compiler path with %q", forbidden)
		}
	}
	if effectiveStackPackageOwner(stackCpp) != "Visual Studio 2022 Build Tools (MSVC + Windows 11 SDK 26100)" {
		t.Fatalf("C/C++ package owner = %q", effectiveStackPackageOwner(stackCpp))
	}
}

func TestVisualStudioInstallerSupportsStandaloneCppAndParallelRustReuse(t *testing.T) {
	text := readDefaultStackProvisioning(t)
	start := strings.Index(text, "function Install-StackVisualStudioBuildTools")
	end := strings.Index(text, "function Enable-StackVisualStudioDeveloperEnvironment")
	if start < 0 || end <= start {
		t.Fatal("Visual Studio stack installer owner is missing")
	}
	section := text[start:end]
	for _, required := range []string{
		"[Collections.IDictionary]$RustToolchainTask = $null",
		"Get-StackVisualStudioInstallation -Target $target",
		"Visual Studio Build Tools already matches Current",
		"if ($null -eq $RustToolchainTask)",
		"Invoke-ProvisioningNative -Role 'Visual Studio Build Tools offline installation'",
		"Start-ProvisioningNativeGroup -Tasks @(",
		"Wait-StackVisualStudioInstalled -Target $target",
	} {
		if !strings.Contains(section, required) {
			t.Errorf("Visual Studio reuse owner is missing %q", required)
		}
	}
}
