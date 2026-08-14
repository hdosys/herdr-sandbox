package main

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"herdr-sandbox/internal/productidentity"
)

func TestParseReleaseVersion(t *testing.T) {
	for _, test := range []struct {
		input string
		want  releaseVersion
	}{
		{input: "v0.0.0", want: releaseVersion{Tag: "v0.0.0", Display: "0.0.0", Fixed: "0.0.0.0"}},
		{input: "v0.0.42", want: releaseVersion{Tag: "v0.0.42", Display: "0.0.42", Fixed: "0.0.42.0"}},
		{input: " v0.0.65535 ", want: releaseVersion{Tag: "v0.0.65535", Display: "0.0.65535", Fixed: "0.0.65535.0"}},
	} {
		got, err := parseReleaseVersion(test.input)
		if err != nil || got != test.want {
			t.Fatalf("parseReleaseVersion(%q) = %#v, %v; want %#v", test.input, got, err, test.want)
		}
	}
	for _, input := range []string{"", "0.0.1", "v0.1.0", "v0.0.01", "v0.0.65536", "v0.0.-1"} {
		if _, err := parseReleaseVersion(input); err == nil {
			t.Fatalf("parseReleaseVersion(%q) unexpectedly succeeded", input)
		}
	}
}

func TestReleasePathsKeepZIPAndInstallerTogether(t *testing.T) {
	version, err := parseReleaseVersion("v0.0.7")
	if err != nil {
		t.Fatal(err)
	}
	paths := releasePaths("root", version)
	if filepath.Base(paths.ZIP) != "herdr-sandbox_v0.0.7_windows_amd64.zip" ||
		filepath.Base(paths.Installer) != "herdr-sandbox_v0.0.7_windows_amd64_setup.exe" ||
		len(releaseOutputPaths(paths)) != 2 {
		t.Fatalf("release paths = %#v", paths)
	}
}

func TestInstallerBuildInputsBindSafeCurrentIdentity(t *testing.T) {
	version, err := parseReleaseVersion("v0.0.13")
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	output := filepath.Join(root, productidentity.ApplicationName+"_"+version.Tag+"_windows_amd64_setup.exe")
	if err := validateInstallerBuildInputs(version, output); err != nil {
		t.Fatalf("validateInstallerBuildInputs: %v", err)
	}
	if productidentity.QuietUninstallHelperName != "uninstall.ps1" {
		t.Fatalf("quiet uninstall helper = %q, want uninstall.ps1", productidentity.QuietUninstallHelperName)
	}
	if productidentity.InstallDirectoryName != productidentity.DisplayName {
		t.Fatalf("install directory = %q, want display name %q", productidentity.InstallDirectoryName, productidentity.DisplayName)
	}
	wantOwned := []string{
		productidentity.BaseScriptName,
		productidentity.LicenseName,
		productidentity.StackScriptName,
		productidentity.QuietUninstallHelperName,
		installerUninstallerName,
		productidentity.ExecutableName,
	}
	if !slices.Equal(installerOwnedFiles(), wantOwned) {
		t.Fatalf("installer owned files = %v, want %v", installerOwnedFiles(), wantOwned)
	}
	if err := validateInstallerBuildInputs(version, filepath.Join(root, "wrong.exe")); err == nil {
		t.Fatal("mismatched installer output name unexpectedly passed")
	}
	for _, value := range []string{"", ".", "..", `folder\file.exe`, "file:name", "CON.txt", "NUL.anything", `quote".exe`, " name.exe", "name.exe ", "name.", "a/b", "bad\x01.exe"} {
		if err := validateInstallerLeaf("fixture", value); err == nil {
			t.Fatalf("unsafe installer leaf unexpectedly passed: %q", value)
		}
	}
}

func TestInstallerLeafCanonicalizationRejectsWin32Aliases(t *testing.T) {
	for _, test := range []struct {
		left  string
		right string
	}{
		{left: "app.exe", right: "APP.EXE"},
		{left: "app.exe", right: "app.exe."},
		{left: "app.exe", right: "app.exe "},
	} {
		if canonicalInstallerLeaf(test.left) != canonicalInstallerLeaf(test.right) {
			t.Fatalf("canonicalInstallerLeaf(%q) != canonicalInstallerLeaf(%q)", test.left, test.right)
		}
	}
	for _, reserved := range []string{"CON", "con.txt", "NUL.anything", "COM1.log", "lpt9"} {
		if err := validateInstallerLeaf("fixture", reserved); err == nil {
			t.Fatalf("reserved Windows leaf unexpectedly passed: %q", reserved)
		}
	}
}

func TestStageAndZIPReleasePackageContainExactFiles(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "bin")
	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatal(err)
	}
	wantContents := map[string]string{}
	for _, file := range releasePackageFiles {
		contents := "fixture:" + file.Name
		wantContents[file.Name] = contents
		if err := os.WriteFile(filepath.Join(source, file.Name), []byte(contents), file.Mode); err != nil {
			t.Fatal(err)
		}
	}
	stage := filepath.Join(root, "stage")
	if err := stageReleasePackage(source, stage); err != nil {
		t.Fatalf("stageReleasePackage: %v", err)
	}
	firstZIP := filepath.Join(root, "first.zip")
	secondZIP := filepath.Join(root, "second.zip")
	if err := writeReleaseZIP(stage, firstZIP); err != nil {
		t.Fatalf("write first ZIP: %v", err)
	}
	if err := writeReleaseZIP(stage, secondZIP); err != nil {
		t.Fatalf("write second ZIP: %v", err)
	}
	first, err := os.ReadFile(firstZIP)
	if err != nil {
		t.Fatal(err)
	}
	second, err := os.ReadFile(secondZIP)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("release ZIP is not deterministic for identical staged files")
	}
	archive, err := zip.OpenReader(firstZIP)
	if err != nil {
		t.Fatal(err)
	}
	defer archive.Close()
	var names []string
	for _, file := range archive.File {
		names = append(names, file.Name)
		reader, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, readErr := io.ReadAll(reader)
		closeErr := reader.Close()
		if readErr != nil || closeErr != nil {
			t.Fatal(errors.Join(readErr, closeErr))
		}
		if string(data) != wantContents[file.Name] {
			t.Fatalf("ZIP content %s = %q", file.Name, data)
		}
	}
	wantNames := []string{productidentity.ExecutableName, productidentity.BaseScriptName, productidentity.StackScriptName, "LICENSE.txt"}
	if !slices.Equal(names, wantNames) {
		t.Fatalf("ZIP entries = %v, want %v", names, wantNames)
	}
}

func TestValidateReleasePackageRejectsExtraOrMissingFiles(t *testing.T) {
	stage := t.TempDir()
	for _, file := range releasePackageFiles {
		if err := os.WriteFile(filepath.Join(stage, file.Name), []byte("fixture"), file.Mode); err != nil {
			t.Fatal(err)
		}
	}
	if err := validateReleasePackage(stage); err != nil {
		t.Fatalf("validate exact package: %v", err)
	}
	if err := os.WriteFile(filepath.Join(stage, "unexpected.dll"), []byte("fixture"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := validateReleasePackage(stage); err == nil {
		t.Fatal("package with an extra file unexpectedly validated")
	}
	if err := os.Remove(filepath.Join(stage, "unexpected.dll")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(stage, productidentity.BaseScriptName)); err != nil {
		t.Fatal(err)
	}
	if err := validateReleasePackage(stage); err == nil {
		t.Fatal("package with a missing file unexpectedly validated")
	}
}

func TestPublishReleaseArtifactSetNeverLeavesMixedOutputs(t *testing.T) {
	pathsAt := func(directory string) releasePackagePaths {
		return releasePackagePaths{
			ZIP:       filepath.Join(directory, "release.zip"),
			Installer: filepath.Join(directory, "release_setup.exe"),
		}
	}
	writeSet := func(t *testing.T, paths releasePackagePaths, value string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(paths.ZIP), 0o755); err != nil {
			t.Fatal(err)
		}
		for _, path := range releaseOutputPaths(paths) {
			if err := os.WriteFile(path, []byte(value+filepath.Base(path)), 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}

	root := t.TempDir()
	generated := pathsAt(filepath.Join(root, "generated"))
	destination := pathsAt(filepath.Join(root, "destination"))
	writeSet(t, generated, "new:")
	writeSet(t, destination, "old:")
	if err := verifyReleaseArtifactSet(generated); err != nil {
		t.Fatalf("verify generated set: %v", err)
	}
	if err := publishReleaseArtifactSet(generated, destination); err != nil {
		t.Fatalf("publish generated set: %v", err)
	}
	if err := verifyReleaseArtifactSet(destination); err != nil {
		t.Fatalf("verify published set: %v", err)
	}
	for _, path := range []string{destination.ZIP, destination.Installer} {
		data, err := os.ReadFile(path)
		if err != nil || !strings.HasPrefix(string(data), "new:") {
			t.Fatalf("published artifact %s = %q, %v", path, data, err)
		}
	}

	broken := pathsAt(filepath.Join(root, "broken"))
	writeSet(t, broken, "broken:")
	if err := os.Remove(broken.Installer); err != nil {
		t.Fatal(err)
	}
	writeSet(t, destination, "old-again:")
	if err := publishReleaseArtifactSet(broken, destination); err == nil {
		t.Fatal("incomplete generated set unexpectedly published")
	}
	for _, path := range releaseOutputPaths(destination) {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("mixed release output remains %s: %v", path, err)
		}
	}
}

func TestInstallerWelcomeArtworkAssets(t *testing.T) {
	root := filepath.Join("..", "..")
	source, err := os.ReadFile(filepath.Join(root, "bg.png"))
	if err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprintf("%x", sha256.Sum256(source)); got != "cda0b672eb6ba9d912bc9c422b2ee53fc96aa8b9b1d751b4f653d9b6d0be4b27" {
		t.Fatalf("bg.png SHA-256 = %s", got)
	}
	config, err := png.DecodeConfig(bytes.NewReader(source))
	if err != nil {
		t.Fatalf("decode bg.png: %v", err)
	}
	if config.Width != 906 || config.Height != 1736 || len(source) < 29 || source[24] != 8 || source[25] != 2 || source[28] != 0 {
		t.Fatalf("bg.png contract = %dx%d depth=%d color-type=%d interlace=%d", config.Width, config.Height, source[24], source[25], source[28])
	}

	variants := []struct {
		name   string
		width  int
		height int
		hash   string
	}{
		{name: "installer-welcome-finish-164x314.bmp", width: 164, height: 314, hash: "c9ebaec9dd686eb18e943eada7d51f474e0367771719b8a4918b8fc3812481fd"},
		{name: "installer-welcome-finish-205x393.bmp", width: 205, height: 393, hash: "a2b880e59fa15b1f8f51e5824c7e7c4f90eeb50f3cd649c58beeb5eefe7c64b0"},
		{name: "installer-welcome-finish-246x471.bmp", width: 246, height: 471, hash: "04615093017767a7320c5580368f2aaa92e4f33d4ad1fb42309ee6afa570b927"},
		{name: "installer-welcome-finish-287x550.bmp", width: 287, height: 550, hash: "8912c6dcee700825c4463704841f777e248d807f48cbe5db3ceb1b87c8d96127"},
		{name: "installer-welcome-finish-328x628.bmp", width: 328, height: 628, hash: "b3b5bfaa3b07dd7eb8441f81bf733de86e31b8c37ec60ac3832a824c10e1cd3b"},
	}
	assetDirectory := filepath.Join(root, "packaging", "windows", "assets")
	entries, err := os.ReadDir(assetDirectory)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.EqualFold(filepath.Ext(entry.Name()), ".bmp") {
			names = append(names, entry.Name())
		}
	}
	slices.Sort(names)
	wantNames := make([]string, 0, len(variants))
	for _, variant := range variants {
		wantNames = append(wantNames, variant.name)
	}
	if !slices.Equal(names, wantNames) {
		t.Fatalf("installer BMP assets = %v, want %v", names, wantNames)
	}

	for _, variant := range variants {
		data, err := os.ReadFile(filepath.Join(assetDirectory, variant.name))
		if err != nil {
			t.Fatal(err)
		}
		if got := fmt.Sprintf("%x", sha256.Sum256(data)); got != variant.hash {
			t.Fatalf("%s SHA-256 = %s", variant.name, got)
		}
		rowSize := (variant.width*3 + 3) &^ 3
		pixelSize := rowSize * variant.height
		if len(data) != 54+pixelSize || string(data[:2]) != "BM" ||
			int(binary.LittleEndian.Uint32(data[2:6])) != len(data) ||
			binary.LittleEndian.Uint32(data[10:14]) != 54 ||
			binary.LittleEndian.Uint32(data[14:18]) != 40 ||
			int(int32(binary.LittleEndian.Uint32(data[18:22]))) != variant.width ||
			int(int32(binary.LittleEndian.Uint32(data[22:26]))) != variant.height ||
			binary.LittleEndian.Uint16(data[26:28]) != 1 ||
			binary.LittleEndian.Uint16(data[28:30]) != 24 ||
			binary.LittleEndian.Uint32(data[30:34]) != 0 ||
			int(binary.LittleEndian.Uint32(data[34:38])) != pixelSize {
			t.Fatalf("%s is not an exact uncompressed 24-bit BMP3 at %dx%d", variant.name, variant.width, variant.height)
		}
	}
}

func TestInstallerTemplateExposesSandboxIntegrationContract(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "packaging", "windows", "installer.nsi"))
	if err != nil {
		t.Fatal(err)
	}
	source := string(data)
	for _, want := range []string{
		`RequestExecutionLevel user`,
		`InstallDir "$LOCALAPPDATA\Programs\${APP_INSTALL_DIRECTORY}"`,
		`ShowInstDetails show`,
		`ShowUninstDetails show`,
		`!macro DefineOwnedDirectoryRemoval PREFIX`,
		`!insertmacro DefineOwnedDirectoryRemoval ""`,
		`!insertmacro DefineOwnedDirectoryRemoval "un."`,
		`KERNEL32::DeleteFileW`,
		`KERNEL32::SetFileAttributesW`,
		`APP_FILE_ATTRIBUTE_READONLY`,
		`KERNEL32::RemoveDirectoryW`,
		`APP_FILE_ATTRIBUTE_REPARSE_POINT`,
		`Close every running ${APP_DISPLAY_NAME} command, then run setup again.`,
		`Close every running ${APP_DISPLAY_NAME} command, then run uninstall again.`,
		`Setup is complete. No app window opens. Open a new terminal:`,
		`${APP_NAME} init: Create a project profile`,
		`${APP_NAME} up: Start or reconnect`,
		`${APP_NAME} config: Open the configuration file`,
		`${APP_NAME} status: Inspect Sandbox state`,
		`__installer-open-configuration`,
		`Run ${APP_NAME} config from a new terminal.`,
		`!insertmacro MUI_PAGE_LICENSE "${PACKAGE_DIR}\${APP_LICENSE}"`,
		`!insertmacro MUI_PAGE_FINISH`,
		`UninstPage custom un.DeleteConfigurationPage un.DeleteConfigurationPageLeave`,
		`Also delete ${APP_CONFIG_FILE} and ${APP_USER_SCRIPT}`,
		`A running Sandbox stays open but becomes unmanaged`,
		`__installer-seed-configuration`,
		`installer-stop-processes`,
		`__installer-clean-uninstall`,
		`--installer-lifecycle-lock-held`,
		`--delete-configuration`,
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("installer template is missing Sandbox integration %q", want)
		}
	}
	for _, forbidden := range []string{
		`MUI_PAGE_DIRECTORY`,
		`RequestExecutionLevel admin`,
		`Herdr Sandbox`,
		`herdr-sandbox`,
		`base.ps1`,
		`stacks.ps1`,
		`config.json`,
		`user.ps1`,
		`.herdr-sandbox`,
		`MarkerLegacy`,
		`CheckLegacy`,
		`APP_REPLACED_EXECUTABLE`,
		`APP_INSTALLER_MARKER`,
		`APP_INSTALLER_SCHEMA`,
		`InstallerSchemaVersion`,
		`--installer-schema`,
		`RMDir /r`,
		`"installedVersion":  "0.0.10"`,
	} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("installer template hard-codes project identity %q", forbidden)
		}
	}
}

func TestInstallerStopsApplicationProcessesBeforeLifecycleMutationAndResumesLateUninstall(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "packaging", "windows", "installer.nsi"))
	if err != nil {
		t.Fatal(err)
	}
	source := string(data)
	for _, want := range []string{
		`!macro StopInstalledApplicationProcesses ACTION FAILURE_CODE`,
		`nsExec::ExecToStack /TIMEOUT=7000 '"$INSTDIR\${APP_EXECUTABLE}" installer-stop-processes'`,
		`WriteRegDWORD HKCU "${UNINSTALL_KEY}" "UninstallPending" 0`,
		`WriteRegDWORD HKCU "${UNINSTALL_KEY}" "UninstallPending" 1`,
		`${AndIf} $1 == "1"`,
		`The installation remains registered. Run uninstall again.`,
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("installer process-stop or retry contract is missing %q", want)
		}
	}
	if got := strings.Count(source, `!insertmacro StopInstalledApplicationProcesses "`); got != 2 {
		t.Fatalf("installed process-stop call count = %d, want 2", got)
	}
	if strings.Contains(source, `taskkill /IM`) || strings.Contains(source, `WindowsSandbox.exe`) || strings.Contains(source, `Run setup again before retrying uninstall`) {
		t.Fatal("installer retains a broad process kill or setup-only late-uninstall retry")
	}

	installStart := strings.Index(source, `Section "Install"`)
	uninstallStart := strings.Index(source, `Section "Uninstall"`)
	if installStart < 0 || uninstallStart <= installStart {
		t.Fatal("installer sections are missing or out of order")
	}
	installSection := source[installStart:uninstallStart]
	uninstallSection := source[uninstallStart:]
	assertInstallerOrder := func(name, section string, values ...string) {
		t.Helper()
		previous := -1
		for _, value := range values {
			index := strings.Index(section, value)
			if index <= previous {
				t.Fatalf("%s order for %q = %d after %d", name, value, index, previous)
			}
			previous = index
		}
	}
	assertInstallerOrder("setup", installSection,
		`!insertmacro AcquireInstallerMutex`,
		`!insertmacro StopInstalledApplicationProcesses "setup"`,
		`!insertmacro AcquireLifecycleMutex`,
		`Call EnsureInstallRepairRegistration`,
		`System::Call 'KERNEL32::CreateDirectoryW(w "$INSTDIR", p 0)`,
		`Call RemoveOwnedDirectoryTree`,
	)
	assertInstallerOrder("uninstall", uninstallSection,
		`!insertmacro AcquireInstallerMutex`,
		`!insertmacro StopInstalledApplicationProcesses "uninstall"`,
		`!insertmacro AcquireLifecycleMutex`,
		`Call un.EnsureRetryUninstaller`,
		`StrCpy $InstallMutationActive "1"`,
	)
	ensureStart := strings.Index(source, `Function EnsureInstallRepairRegistration`)
	ensureEnd := strings.Index(source[ensureStart:], `FunctionEnd`)
	if ensureStart < 0 || ensureEnd < 0 || !strings.Contains(source[ensureStart:ensureStart+ensureEnd], `WriteRegDWORD HKCU "${UNINSTALL_KEY}" "UninstallPending" 0`) {
		t.Fatal("setup repair registration does not clear resumable-uninstall state before target mutation")
	}
}

func TestInstallerRestoresExactRegistryValueKinds(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "packaging", "windows", "installer.nsi"))
	if err != nil {
		t.Fatal(err)
	}
	source := string(data)
	for _, want := range []string{
		`!define APP_REGISTRY_TYPE_SZ 1`,
		`!define APP_REGISTRY_TYPE_EXPAND_SZ 2`,
		`!define APP_REGISTRY_TYPE_DWORD 4`,
		`*i .r3, p 0, *i .r2`,
		`!macro SnapshotRegistryValue NAME PRESENT TYPE VALUE`,
		`!macro RestoreRegistryValue NAME PRESENT TYPE VALUE`,
		`WriteRegExpandStr HKCU "${UNINSTALL_KEY}" "${NAME}" "${VALUE}"`,
		`$RegistryValueType != ${TYPE}`,
		`install_snapshot_failure:`,
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("exact registry restoration is missing %q", want)
		}
	}
	if got := strings.Count(source, `!insertmacro SnapshotRegistryValue "`); got != 5 {
		t.Fatalf("registry snapshot count = %d, want 5", got)
	}
	if got := strings.Count(source, `!insertmacro RestoreRegistryValue "`); got != 5 {
		t.Fatalf("registry restore count = %d, want 5", got)
	}
}

func TestInstallerPathHelperConvergesPracticalUserPathInWindowsPowerShell51(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows PowerShell 5.1 PATH regression")
	}
	pathHelper, err := filepath.Abs(filepath.Join("..", "..", "packaging", "windows", "path.ps1"))
	if err != nil {
		t.Fatal(err)
	}
	quote := func(value string) string { return strings.ReplaceAll(value, "'", "''") }
	script := fmt.Sprintf(`$ErrorActionPreference = 'Stop'
$tokens = $null
$errors = $null
$ast = [Management.Automation.Language.Parser]::ParseFile('%s', [ref]$tokens, [ref]$errors)
if ($errors.Count -ne 0) { throw $errors[0].Message }
foreach ($name in @('Test-FullyQualifiedWindowsPath', 'Get-NormalizedPath', 'Get-PathEntryDescriptor', 'Get-ConvergedPathEntries', 'Resolve-UserPathUpdate')) {
    $definition = $ast.Find({ param($node) $node -is [Management.Automation.Language.FunctionDefinitionAst] -and $node.Name -ceq $name }, $true)
    if ($null -eq $definition) { throw "Missing function $name" }
    Invoke-Expression $definition.Extent.Text
}
$target = 'C:\Users\Example\AppData\Local\Programs\Herdr Sandbox'
function Assert-Update {
    param([string]$Current, [string]$Action, [bool]$Changed, [bool]$Present, [string]$Value, [bool]$ExpandVariables = $false)
    $actual = Resolve-UserPathUpdate -Current $Current -Expected $target -RequestedAction $Action -ExpandVariables $ExpandVariables
    if ([bool]$actual.Changed -ne $Changed -or [bool]$actual.Present -ne $Present -or [string]$actual.Value -cne $Value) {
        throw "PATH $Action [$Current] = changed=$($actual.Changed) present=$($actual.Present) value=[$($actual.Value)]"
    }
}
Assert-Update -Current '' -Action Add -Changed $true -Present $true -Value $target
Assert-Update -Current '' -Action Remove -Changed $false -Present $false -Value ''
$added = Resolve-UserPathUpdate -Current "C:\Tools;;$target;$target" -Expected $target -RequestedAction Add -ExpandVariables $false
if (-not $added.Changed -or $added.Value -cne "C:\Tools;$target") { throw 'Add did not remove empty and duplicate product entries.' }
$removed = Resolve-UserPathUpdate -Current $target -Expected $target -RequestedAction Remove -ExpandVariables $false
if (-not $removed.Changed -or $removed.Value -cne '') { throw 'Remove did not support an empty result.' }
$unrelated = Resolve-UserPathUpdate -Current 'C:\Windows;c:\WINDOWS\;relative\bin;RELATIVE\BIN;;C:\Other' -Expected $target -RequestedAction Add -ExpandVariables $false
if ($unrelated.Value -cne "C:\Windows;relative\bin;C:\Other;$target") { throw 'Unrelated PATH convergence changed first-occurrence precedence.' }
$target = Join-Path $env:LOCALAPPDATA 'Programs\Herdr Sandbox'
Assert-Update -Current '%%LOCALAPPDATA%%\Programs\Herdr Sandbox' -Action Remove -Changed $true -Present $false -Value '' -ExpandVariables $true
$composed = 'C:\Temp\Caf' + [char]0x00e9
$decomposed = 'C:\Temp\Cafe' + [char]0x0301
$unicode = Resolve-UserPathUpdate -Current ($composed + ';' + $decomposed) -Expected $composed -RequestedAction Remove -ExpandVariables $false
if (-not [string]::Equals([string]$unicode.Value, $decomposed, [StringComparison]::Ordinal)) { throw 'Distinct Unicode entry was removed.' }
`, quote(pathHelper))
	scriptPath := filepath.Join(t.TempDir(), "path-regression.ps1")
	if err := os.WriteFile(scriptPath, []byte(script), 0o600); err != nil {
		t.Fatal(err)
	}
	powerShell := filepath.Join(os.Getenv("SystemRoot"), "System32", "WindowsPowerShell", "v1.0", "powershell.exe")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	command := hiddenCommandContext(ctx, powerShell, "-NoLogo", "-NoProfile", "-NonInteractive", "-WindowStyle", "Hidden", "-ExecutionPolicy", "Bypass", "-File", scriptPath)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("PATH regression: %v: %s", err, output)
	}
}

func TestPackageTaskSuppliesCanonicalInstallerIdentity(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("package.go"))
	if err != nil {
		t.Fatal(err)
	}
	source := string(data)
	for _, want := range []string{
		`"herdr-sandbox/internal/productidentity"`,
		`"/WX"`,
		`installerBuildValidatorName`,
		`"-WindowStyle", "Hidden"`,
		`"-AppApplicationName", productidentity.ApplicationName`,
		`"-AssetsDirectory", assetsDirectory`,
		`validate NSIS installer inputs`,
		`"/DAPP_NAME=" + productidentity.CommandName`,
		`"/DAPP_APPLICATION_NAME=" + productidentity.ApplicationName`,
		`"/DAPP_DISPLAY_NAME=" + productidentity.DisplayName`,
		`"/DAPP_EXECUTABLE=" + productidentity.ExecutableName`,
		`"/DAPP_BASE_SCRIPT=" + productidentity.BaseScriptName`,
		`"/DAPP_STACK_SCRIPT=" + productidentity.StackScriptName`,
		`"/DAPP_LICENSE=" + productidentity.LicenseName`,
		`"/DAPP_CONFIG_FILE=" + productidentity.ConfigurationName`,
		`"/DAPP_USER_SCRIPT=" + productidentity.UserScriptName`,
		`"/DAPP_PROJECT_DIRECTORY=" + productidentity.ProjectDirectoryName`,
		`"/DAPP_INSTALL_DIRECTORY=" + productidentity.InstallDirectoryName`,
		`"/DAPP_PUBLISHER=" + productidentity.Publisher`,
		`"/DAPP_PRODUCT_URL=" + productidentity.ProductURL`,
		`"/DAPP_PRODUCT_GUID=" + productidentity.ProductGUID`,
		`"/DAPP_UNINSTALL_KEY=" + productidentity.UninstallKeyName`,
		`"/DAPP_QUIET_UNINSTALL_HELPER=" + productidentity.QuietUninstallHelperName`,
		`"/DAPP_COPYRIGHT=" + productidentity.Copyright`,
		`"/DPATH_HELPER=" + pathHelper`,
		`"/DQUIET_UNINSTALL_HELPER=" + quietUninstallHelper`,
		`"/DASSETS_DIR=" + assetsDirectory`,
		`"-InstallerScript", script`,
		`"/DOUTPUT_FILE_NAME=" + filepath.Base(outputPath)`,
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("package task is missing canonical installer identity input %q", want)
		}
	}
	for _, forbidden := range []string{"LegacyUninstallKeyName", "legacyInstaller", "APP_LEGACY_UNINSTALL_KEY", "ReplacedExecutableName", "APP_REPLACED_EXECUTABLE", "installerDefinition", "installer-state.ps1"} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("package task still carries installer backward compatibility %q", forbidden)
		}
	}
}

func TestQuietUninstallWrapperOwnsPrivateTemporaryCopyAndExitCode(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "packaging", "windows", "uninstall.ps1"))
	if err != nil {
		t.Fatal(err)
	}
	source := string(data)
	for _, want := range []string{
		`function Get-FileSHA256`,
		`[Security.Cryptography.SHA256]::Create()`,
		`$hasher.ComputeHash($stream)`,
		`$hasher.Dispose()`,
		`$stream.Dispose()`,
		`function Stop-OwnedProcessTree`,
		`System32\taskkill.exe`,
		`@('/PID', [string]$Process.Id, '/T', '/F')`,
		`[Diagnostics.Stopwatch]::StartNew()`,
		`$termination.WaitForExit(2500)`,
		`5000 - [int]$cleanup.ElapsedMilliseconds`,
		`$tempDirectory = Join-Path $tempRoot`,
		`$tempUninstaller = Join-Path $tempDirectory 'uninstall.exe'`,
		`$startInfo.Arguments = '/S _?=' + $installPath`,
		`25000 - [int]$total.ElapsedMilliseconds`,
		`$process.WaitForExit($operationBudget)`,
		`Quiet uninstall exceeded its 30-second total limit.`,
		`$exitCode = $process.ExitCode`,
		`Remove-Item -LiteralPath $tempDirectory -Recurse -Force`,
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("quiet uninstall helper is missing %q", want)
		}
	}
	if strings.Contains(source, `$process.Kill()`) {
		t.Fatal("quiet uninstall must terminate the owned process tree, not only the immediate uninstaller")
	}
	if strings.Contains(source, `Get-FileHash`) {
		t.Fatal("quiet uninstall must not depend on PowerShell module auto-loading for SHA-256 verification")
	}
}

func TestQuietUninstallWrapperTerminatesOwnedProcessTreeInWindowsPowerShell51(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows PowerShell 5.1 process-tree termination regression")
	}
	helper, err := filepath.Abs(filepath.Join("..", "..", "packaging", "windows", "uninstall.ps1"))
	if err != nil {
		t.Fatal(err)
	}
	powerShell := filepath.Join(os.Getenv("SystemRoot"), "System32", "WindowsPowerShell", "v1.0", "powershell.exe")
	quote := func(value string) string { return strings.ReplaceAll(value, "'", "''") }
	root := t.TempDir()
	controller := fmt.Sprintf(`$ErrorActionPreference = 'Stop'
Set-StrictMode -Version 2.0
$tokens = $null
$errors = $null
$ast = [Management.Automation.Language.Parser]::ParseFile('%s', [ref]$tokens, [ref]$errors)
if ($errors.Count -ne 0) { throw $errors[0].Message }
$definition = $ast.Find({ param($node) $node -is [Management.Automation.Language.FunctionDefinitionAst] -and $node.Name -ceq 'Stop-OwnedProcessTree' }, $true)
if ($null -eq $definition) { throw 'Missing Stop-OwnedProcessTree.' }
Invoke-Expression $definition.Extent.Text
$powerShell = '%s'
$pidFile = '%s'
$childScript = '%s'
[IO.File]::WriteAllText($childScript, 'Start-Sleep -Seconds 120', (New-Object Text.UTF8Encoding($false)))
$env:HERDR_TEST_POWERSHELL = $powerShell
$env:HERDR_TEST_PID_FILE = $pidFile
$env:HERDR_TEST_CHILD_SCRIPT = $childScript
$parentSource = @'
$child = Start-Process -FilePath $env:HERDR_TEST_POWERSHELL -ArgumentList @('-NoLogo', '-NoProfile', '-NonInteractive', '-WindowStyle', 'Hidden', '-ExecutionPolicy', 'Bypass', '-File', $env:HERDR_TEST_CHILD_SCRIPT) -WindowStyle Hidden -PassThru
[IO.File]::WriteAllText($env:HERDR_TEST_PID_FILE, [string]$child.Id, (New-Object Text.UTF8Encoding($false)))
try { Start-Sleep -Seconds 120 } finally { if (-not $child.HasExited) { $child.Kill() } }
'@
$encoded = [Convert]::ToBase64String([Text.Encoding]::Unicode.GetBytes($parentSource))
$parent = Start-Process -FilePath $powerShell -ArgumentList @('-NoLogo', '-NoProfile', '-NonInteractive', '-WindowStyle', 'Hidden', '-EncodedCommand', $encoded) -WindowStyle Hidden -PassThru
try {
    $deadline = [DateTime]::UtcNow.AddSeconds(10)
    while (-not [IO.File]::Exists($pidFile) -and [DateTime]::UtcNow -lt $deadline) { Start-Sleep -Milliseconds 25 }
    if (-not [IO.File]::Exists($pidFile)) { throw 'Child PID was not published.' }
    $childPid = [int][IO.File]::ReadAllText($pidFile)
    Stop-OwnedProcessTree -Process $parent
    if (-not $parent.HasExited) { throw 'Parent process survived tree termination.' }
    try {
        $child = [Diagnostics.Process]::GetProcessById($childPid)
        if (-not $child.WaitForExit(5000)) { throw 'Child process survived tree termination.' }
        $child.Dispose()
    }
    catch [ArgumentException] {}
}
finally {
    if (-not $parent.HasExited) {
        $cleanup = Start-Process -FilePath (Join-Path $env:SystemRoot 'System32\taskkill.exe') -ArgumentList @('/PID', [string]$parent.Id, '/T', '/F') -WindowStyle Hidden -PassThru
        [void]$cleanup.WaitForExit(10000)
        $cleanup.Dispose()
    }
    $parent.Dispose()
}
`, quote(helper), quote(powerShell), quote(filepath.Join(root, "child.pid")), quote(filepath.Join(root, "child.ps1")))
	controllerPath := filepath.Join(root, "controller.ps1")
	if err := os.WriteFile(controllerPath, []byte(controller), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	command := hiddenCommandContext(ctx, powerShell, "-NoLogo", "-NoProfile", "-NonInteractive", "-WindowStyle", "Hidden", "-ExecutionPolicy", "Bypass", "-File", controllerPath)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("quiet uninstall process-tree regression: %v: %s", err, output)
	}
}

func TestReleaseWorkflowUsesCanonicalPackageTaskAndPinnedNSIS(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", ".github", "workflows", "release.yml"))
	if err != nil {
		t.Fatal(err)
	}
	workflow := string(data)
	for _, want := range []string{
		`NSIS_VERSION: "` + installerEngineVersion + `"`,
		`NSIS_URL: https://downloads.sourceforge.net/project/nsis/NSIS%203/3.12/nsis-3.12.zip`,
		`NSIS_SHA256: 56581f90db321581c5381193d796fffcf2d24b2f8fed2160a6c6a3baa67f2c4f`,
		`$curl = Join-Path $env:SystemRoot 'System32\curl.exe'`,
		`& $curl --fail --location --silent --show-error --output $archive $env:NSIS_URL`,
		`go run ./cmd/task package $env:RELEASE_TAG`,
		`go run ./cmd/task release-notes $env:RELEASE_TAG`,
		`Get-ChildItem -LiteralPath 'build\dist' -File`,
		`$assets.Count -ne 2`,
		`$installers.Count -ne 1`,
		`VersionInfo.ProductName`,
		`release view $env:RELEASE_TAG --json assets`,
		`Get-FileHash -LiteralPath $asset.FullName -Algorithm SHA256`,
		`'--notes', $releaseNotes`,
		`'--title', "$productName $env:RELEASE_TAG"`,
	} {
		if !strings.Contains(workflow, want) {
			t.Fatalf("release workflow is missing %q", want)
		}
	}
	for _, forbidden := range []string{"--generate-notes", "Compress-Archive", "Invoke-WebRequest", "choco install", "cargo", "herdr.exe'", ".sha256", "release verify-asset"} {
		if strings.Contains(workflow, forbidden) {
			t.Fatalf("release workflow contains duplicate or out-of-scope packaging %q", forbidden)
		}
	}
}
