package sandbox

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestAndroidStackInstallsVerifiedCommandLineBuildAndWirelessADBTools(t *testing.T) {
	text := readDefaultStackProvisioning(t)
	start := strings.Index(text, "function Install-AndroidStack")
	end := strings.Index(text, "function Install-DotNetStack")
	if start < 0 || end <= start {
		t.Fatal("Android stack owner is missing")
	}
	section := text[start:end]
	for _, required := range []string{
		"$androidCLIRevision = '22.0'",
		"commandlinetools-win-15859902_latest.zip",
		"90AE805D20434428BFFCB699C290860F19BB5F66A67E6B330067E3DE801FB04A",
		"Assert-StackAndroidMicrosoftSignature",
		"$jdkVersion = '17.0.20'",
		"microsoft-jdk-17.0.20-windows-x64.zip",
		"E46FD292317C6BB0A8FE9DC63115021329F3A63CAEBA791C185F89F3666A68E5",
		"ANDROID_HOME",
		"ANDROID_JAVA_HOME",
		"ANDROID_USER_HOME",
		"'sdk', 'install', 'platform-tools'",
		"'--no-metrics'",
		"Android CLI version verification",
		"Android SDK location verification",
		"Pkg\\.Revision=(?<version>\\d+\\.\\d+\\.\\d+)",
		"Android ADB version verification",
		"Android JDK runtime version verification",
		"Android JDK compiler version verification",
		"[string]::IsNullOrWhiteSpace($machineJavaHome)",
		"Android JAVA_HOME activation read-back failed",
		"pair HOST\\[:PORT\\]",
		"connect HOST\\[:PORT\\]",
	} {
		if !strings.Contains(section, required) {
			t.Errorf("Android stack is missing %q", required)
		}
	}
	for _, forbidden := range []string{
		"Install-JavaStack", "Microsoft.OpenJDK.25", "Android Studio", "emulator",
		"fastboot", "usbip", "host adb", "adb.exe server nodaemon", "--force",
		"'sdk', 'list', 'platform-tools'",
	} {
		if strings.Contains(section, forbidden) {
			t.Errorf("Android stack contains an unrelated or unsupported path %q", forbidden)
		}
	}
	guard := strings.Index(section, "if ([string]::IsNullOrWhiteSpace($machineJavaHome))")
	activation := strings.Index(section, "[Environment]::SetEnvironmentVariable('JAVA_HOME', $jdkRoot, 'Machine')")
	if guard < 0 || activation <= guard {
		t.Fatal("Android JAVA_HOME activation is not guarded by an absent machine owner")
	}
	if effectiveStackPackageOwner(stackAndroid) != "Android SDK Command-line Tools + Platform Tools + Microsoft OpenJDK 17" {
		t.Fatalf("Android package owner = %q", effectiveStackPackageOwner(stackAndroid))
	}
}

func TestAndroidStackUsesAppOwnedRootsAndLeavesProjectDependenciesAlone(t *testing.T) {
	text := readDefaultStackProvisioning(t)
	start := strings.Index(text, "function Install-AndroidStack")
	end := strings.Index(text, "function Install-DotNetStack")
	if start < 0 || end <= start {
		t.Fatal("Android stack owner is missing")
	}
	section := text[start:end]
	for _, required := range []string{
		`C:\HerdrSandbox\tools\android-sdk`,
		`C:\HerdrSandbox\toolchains\android-jdk-17`,
		`C:\HerdrSandbox\build\android-user`,
		"Add-ProvisioningMachinePath -Directory $androidCLIBin",
		"Add-ProvisioningMachinePath -Directory $platformTools",
	} {
		if !strings.Contains(section, required) {
			t.Errorf("Android stack is missing app-owned state %q", required)
		}
	}
	for _, forbidden := range []string{
		"build.gradle", "settings.gradle", "gradle.properties", "gradlew", "platforms;android-",
		"build-tools;", "system-images;", "cmdline-tools;latest", "sdkmanager --licenses",
	} {
		if strings.Contains(section, forbidden) {
			t.Errorf("Android stack mutates project state or installs speculative SDK packages %q", forbidden)
		}
	}
}

func TestAndroidArchiveHelpersValidateInspectedPublisherPayloadsInWindowsPowerShell51(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows PowerShell 5.1 Android payload regression")
	}
	androidRoot := filepath.Join(os.TempDir(), "opencode", "android-inspection", "extract", "cmdline-tools")
	androidExecutable := filepath.Join(androidRoot, "bin", "android.exe")
	if info, err := os.Stat(androidExecutable); err != nil || !info.Mode().IsRegular() {
		t.Skipf("inspected Android payload is unavailable: %s", androidExecutable)
	}
	stackPath := defaultProvisioningPath(t, stackProvisioningName)
	quote := func(value string) string { return strings.ReplaceAll(value, "'", "''") }
	script := fmt.Sprintf(`$ErrorActionPreference = 'Stop'
$tokens = $null
$errors = $null
$ast = [Management.Automation.Language.Parser]::ParseFile('%s', [ref]$tokens, [ref]$errors)
if ($errors.Count -ne 0) { throw $errors[0].Message }
foreach ($name in @('Test-StackAndroidArchiveEntry', 'Assert-StackAndroidTree')) {
    $definition = $ast.Find({ param($node) $node -is [Management.Automation.Language.FunctionDefinitionAst] -and $node.Name -ceq $name }, $true)
    if ($null -eq $definition) { throw "Missing Android helper: $name" }
    Invoke-Expression $definition.Extent.Text
}
Test-StackAndroidArchiveEntry -Entry 'cmdline-tools/bin/android.exe' -Root 'cmdline-tools'
$rejected = $false
try { Test-StackAndroidArchiveEntry -Entry 'cmdline-tools/../escape' -Root 'cmdline-tools' } catch { $rejected = $true }
if (-not $rejected) { throw 'Unsafe Android archive entry was accepted.' }
Assert-StackAndroidTree -Root '%s' -RequiredRelativePaths @('source.properties', 'bin\android.exe', 'bin\sdkmanager.bat', 'lib\sdk-common\tools.sdk-common.jar')
`, quote(stackPath), quote(androidRoot))
	scriptPath := filepath.Join(t.TempDir(), "android-payload-regression.ps1")
	if err := os.WriteFile(scriptPath, []byte(script), 0o600); err != nil {
		t.Fatal(err)
	}
	command := hiddenCommand(mustWindowsPowerShellPath(t), "-NoLogo", "-NoProfile", "-NonInteractive", "-WindowStyle", "Hidden", "-ExecutionPolicy", "Bypass", "-File", scriptPath)
	command.Env = append(os.Environ(), "PSModulePath="+os.Getenv("PSModulePath"))
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("Android payload regression: %v: %s", err, output)
	}
}
