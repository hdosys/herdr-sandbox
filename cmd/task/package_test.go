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
		paths.ZIPChecksum != paths.ZIP+".sha256" || paths.InstallerChecksum != paths.Installer+".sha256" {
		t.Fatalf("release paths = %#v", paths)
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
			ZIP:               filepath.Join(directory, "release.zip"),
			ZIPChecksum:       filepath.Join(directory, "release.zip.sha256"),
			Installer:         filepath.Join(directory, "release_setup.exe"),
			InstallerChecksum: filepath.Join(directory, "release_setup.exe.sha256"),
		}
	}
	writeSet := func(t *testing.T, paths releasePackagePaths, value string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(paths.ZIP), 0o755); err != nil {
			t.Fatal(err)
		}
		for _, path := range []string{paths.ZIP, paths.Installer} {
			if err := os.WriteFile(path, []byte(value+filepath.Base(path)), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		if err := writeSHA256File(paths.ZIP, paths.ZIPChecksum); err != nil {
			t.Fatal(err)
		}
		if err := writeSHA256File(paths.Installer, paths.InstallerChecksum); err != nil {
			t.Fatal(err)
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

func TestInstallerSourceUsesLeanPerUserPackageContract(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "packaging", "windows", "installer.nsi"))
	if err != nil {
		t.Fatal(err)
	}
	source := string(data)
	for _, want := range []string{
		`RequestExecutionLevel user`,
		`AllowSkipFiles off`,
		`InstallDir "$LOCALAPPDATA\Programs\${APP_INSTALL_DIRECTORY}"`,
		`SetDatablockOptimize on`,
		`SetCompressorDictSize 8`,
		`SetCompressor /SOLID /FINAL lzma`,
		`ManifestDPIAware true`,
		`AutoCloseWindow true`,
		`!define INSTALLER_WELCOME_BITMAP_100 "${__FILEDIR__}\assets\installer-welcome-finish-164x314.bmp"`,
		`!define INSTALLER_WELCOME_BITMAP_125 "${__FILEDIR__}\assets\installer-welcome-finish-205x393.bmp"`,
		`!define INSTALLER_WELCOME_BITMAP_150 "${__FILEDIR__}\assets\installer-welcome-finish-246x471.bmp"`,
		`!define INSTALLER_WELCOME_BITMAP_175 "${__FILEDIR__}\assets\installer-welcome-finish-287x550.bmp"`,
		`!define INSTALLER_WELCOME_BITMAP_200 "${__FILEDIR__}\assets\installer-welcome-finish-328x628.bmp"`,
		`!define MUI_WELCOMEFINISHPAGE_BITMAP "${INSTALLER_WELCOME_BITMAP_100}"`,
		`!define MUI_WELCOMEFINISHPAGE_BITMAP_STRETCH NoStretchNoCropNoAlign`,
		`!define MUI_CUSTOMFUNCTION_GUIINIT SelectInstallerWelcomeBitmap`,
		`System::Call 'USER32::GetDpiForWindow(p $HWNDPARENT)i.r0'`,
		`${If} $0 >= 180`,
		`${ElseIf} $0 >= 156`,
		`${ElseIf} $0 >= 132`,
		`${ElseIf} $0 >= 108`,
		`File "/oname=$PLUGINSDIR\modern-wizard.bmp" "${INSTALLER_WELCOME_BITMAP_200}"`,
		`File "/oname=$PLUGINSDIR\modern-wizard.bmp" "${INSTALLER_WELCOME_BITMAP_175}"`,
		`File "/oname=$PLUGINSDIR\modern-wizard.bmp" "${INSTALLER_WELCOME_BITMAP_150}"`,
		`File "/oname=$PLUGINSDIR\modern-wizard.bmp" "${INSTALLER_WELCOME_BITMAP_125}"`,
		`!define MUI_FINISHPAGE_NOREBOOTSUPPORT`,
		`!define MUI_FINISHPAGE_TITLE "${APP_DISPLAY_NAME} ${VERSION} is installed"`,
		`${APP_DISPLAY_NAME} is a command-line tool, so no application window opens.`,
		`${APP_NAME} init`,
		`${APP_NAME} up`,
		`${APP_NAME} config`,
		`!define MUI_FINISHPAGE_LINK "Open setup and usage guide"`,
		`!define MUI_FINISHPAGE_LINK_LOCATION "${APP_PRODUCT_URL}"`,
		`!insertmacro MUI_PAGE_LICENSE "${PACKAGE_DIR}\${APP_LICENSE}"`,
		`!insertmacro MUI_PAGE_FINISH`,
		`UninstPage custom un.DeleteConfigurationPage un.DeleteConfigurationPageLeave`,
		`${GetOptions} $0 "/DELETE_CONFIG" $1`,
		`${NSD_CreateCheckbox}`,
		`Also delete ${APP_CONFIG_FILE} and ${APP_USER_SCRIPT}`,
		`A running Sandbox stays open but becomes unmanaged`,
		`any running Sandbox stays open`,
		`File "${PACKAGE_DIR}\${APP_BASE_SCRIPT}"`,
		`File "${PACKAGE_DIR}\${APP_EXECUTABLE}"`,
		`File "${PACKAGE_DIR}\${APP_LICENSE}"`,
		`File "${PACKAGE_DIR}\${APP_STACK_SCRIPT}"`,
		`BackupRuntimeFile`,
		`BackupRuntimeFile "${APP_LICENSE}" $R1`,
		`ReplaceRuntimeFile "${APP_LICENSE}"`,
		`RestoreRuntimeFile`,
		`RestoreRuntimeFile "${APP_LICENSE}" $R1`,
		`VIProductVersion "${FIXED_VERSION}"`,
		`WriteRegStr HKCU "${UNINSTALL_KEY}" "DisplayVersion" "${VERSION}"`,
		`WriteRegStr HKCU "${UNINSTALL_KEY}" "UninstallString"`,
		`ReadRegDWORD $2 HKCU "${UNINSTALL_KEY}" "PathAdded"`,
		`WriteRegDWORD HKCU "${UNINSTALL_KEY}" "PathAdded" $5`,
		`StrCpy $INSTDIR "$LOCALAPPDATA\Programs\${APP_INSTALL_DIRECTORY}"`,
		`__installer-seed-configuration`,
		`__installer-clean-uninstall`,
		`--delete-configuration`,
		`!insertmacro UpdateUserPath "Add"`,
		`!insertmacro UpdateUserPath "Remove"`,
		`User32::SendNotifyMessageW`,
		`Delete "$INSTDIR\${APP_EXECUTABLE}"`,
		`Delete "$INSTDIR\${APP_BASE_SCRIPT}"`,
		`Delete "$INSTDIR\${APP_LICENSE}"`,
		`Delete "$INSTDIR\${APP_STACK_SCRIPT}"`,
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("installer source is missing %q", want)
		}
	}
	for _, forbidden := range []string{
		`MUI_PAGE_DIRECTORY`,
		`MUI_FINISHPAGE_RUN`,
		`MUI_FINISHPAGE_SHOWREADME`,
		`MUI_UNPAGE_CONFIRM`,
		`RequestExecutionLevel admin`,
		`RMDir /r`,
		`$APPDATA\herdr-sandbox`,
		`$LOCALAPPDATA\herdr-sandbox`,
		`SendMessage ${HWND_BROADCAST}`,
		`!define PRODUCT_NAME`,
		`Herdr Sandbox`,
		`herdr-sandbox`,
		`HERDR_SANDBOX`,
		`base.ps1`,
		`stacks.ps1`,
		`config.json`,
		`user.ps1`,
		`.herdr-sandbox`,
		`MUI_ICON`,
		`MUI_UNICON`,
		`icon.ico`,
		`assets\installer-welcome-finish.bmp`,
		`herdr.exe`,
		`herdr-win`,
		`Herdr-Win`,
		`WebView`,
		`updater`,
		`runtime bundle`,
		`File /r`,
		`APP_LEGACY_LICENSE`,
		`Stopping the app-owned Sandbox`,
		`MUI_PAGE_CUSTOMFUNCTION_SHOW PolishInstallerFinishPage`,
		`Function PolishInstallerFinishPage`,
		`SetWindowPos(p $mui.FinishPage.Link`,
	} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("installer source contains out-of-scope pattern %q", forbidden)
		}
	}
	cleanupIndex := strings.Index(source, `__installer-clean-uninstall`)
	executableDeleteIndex := strings.Index(source, `Delete "$INSTDIR\${APP_EXECUTABLE}"`)
	if cleanupIndex < 0 || executableDeleteIndex < 0 || cleanupIndex >= executableDeleteIndex {
		t.Fatalf("clean uninstall must finish before executable deletion")
	}
	welcomePageIndex := strings.Index(source, `!insertmacro MUI_PAGE_WELCOME`)
	licensePageIndex := strings.Index(source, `!insertmacro MUI_PAGE_LICENSE`)
	installFilesIndex := strings.Index(source, `!insertmacro MUI_PAGE_INSTFILES`)
	finishPageIndex := strings.Index(source, `!insertmacro MUI_PAGE_FINISH`)
	uninstallPageIndex := strings.Index(source, `UninstPage custom un.DeleteConfigurationPage`)
	if welcomePageIndex < 0 || licensePageIndex <= welcomePageIndex || installFilesIndex <= licensePageIndex || finishPageIndex <= installFilesIndex || uninstallPageIndex <= finishPageIndex {
		t.Fatal("installer flow must end Welcome/License/Files/Finish before uninstaller pages")
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
		`"/DAPP_NAME=" + productidentity.ApplicationName`,
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
		`"/DAPP_UNINSTALL_KEY=" + productidentity.UninstallKeyName`,
		`"/DAPP_COPYRIGHT=" + productidentity.Copyright`,
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("package task is missing canonical installer identity input %q", want)
		}
	}
	if strings.Contains(source, "LEGACY_LICENSE") {
		t.Fatal("package task must not carry a legacy license compatibility path")
	}
}

func TestInstallerPathHelperPreservesUnownedState(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "packaging", "windows", "path.ps1"))
	if err != nil {
		t.Fatal(err)
	}
	source := string(data)
	for _, want := range []string{
		`[ValidateSet('Add', 'Remove')]`,
		`RegistryValueOptions]::DoNotExpandEnvironmentNames`,
		`RegistryValueKind]::ExpandString`,
		`Test-PathEntry`,
		`Resolve-UserPathUpdate`,
		`[string]$InstallDirectory`,
		`$kept = New-Object 'Collections.Generic.List[string]'`,
		`[string]::Join(';', [string[]]$kept)`,
		`exit 10`,
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("PATH helper is missing %q", want)
		}
	}
	for _, forbidden := range []string{"Remove-Item", "APPDATA", "cache", "identity", "runs", "workspace", "HERDR_SANDBOX_INSTALL_DIRECTORY"} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("PATH helper contains unrelated state pattern %q", forbidden)
		}
	}
}

func TestInstallerPathHelperPreservesPathOwnershipInWindowsPowerShell51(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows PowerShell 5.1 PATH ownership regression")
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
foreach ($name in @('Test-PathEntry', 'Resolve-UserPathUpdate')) {
    $definition = $ast.Find({ param($node) $node -is [Management.Automation.Language.FunctionDefinitionAst] -and $node.Name -ceq $name }, $true)
    if ($null -eq $definition) { throw "Missing function $name" }
    Invoke-Expression $definition.Extent.Text
}
$target = 'C:\Users\Example\AppData\Local\Programs\Herdr Sandbox'
function Assert-Update {
    param([string]$Current, [string]$Action, [bool]$Changed, [string]$Value, [bool]$ExpandVariables = $false)
    $actual = Resolve-UserPathUpdate -Current $Current -Expected $target -RequestedAction $Action -ExpandVariables $ExpandVariables
    if ([bool]$actual.Changed -ne $Changed -or [string]$actual.Value -cne $Value) {
        throw "PATH update $Action [$Current] = changed=$($actual.Changed) value=[$($actual.Value)]"
    }
}
Assert-Update -Current '' -Action Add -Changed $true -Value $target
Assert-Update -Current 'C:\Tools' -Action Add -Changed $true -Value "C:\Tools;$target"
$added = Resolve-UserPathUpdate -Current 'C:\Tools;' -Expected $target -RequestedAction Add -ExpandVariables $false
if (-not $added.Changed -or $added.Value -cne "C:\Tools;;$target") { throw 'Trailing empty PATH entry was not preserved during add.' }
$removed = Resolve-UserPathUpdate -Current $added.Value -Expected $target -RequestedAction Remove -ExpandVariables $false
if (-not $removed.Changed -or $removed.Value -cne 'C:\Tools;') { throw 'Original PATH was not restored after owned removal.' }
Assert-Update -Current $target -Action Add -Changed $false -Value $target
Assert-Update -Current ('"' + $target.ToUpperInvariant() + '\"') -Action Add -Changed $false -Value ('"' + $target.ToUpperInvariant() + '\"')
Assert-Update -Current "$target;$target" -Action Remove -Changed $true -Value $target
$equivalent = '"' + $target.ToUpperInvariant() + '\"'
Assert-Update -Current "$equivalent;$target" -Action Remove -Changed $true -Value $equivalent
Assert-Update -Current 'C:\Tools' -Action Remove -Changed $false -Value 'C:\Tools'
$expandedTarget = Join-Path $env:LOCALAPPDATA 'Programs\Herdr Sandbox'
$target = $expandedTarget
Assert-Update -Current '%%LOCALAPPDATA%%\Programs\Herdr Sandbox' -Action Add -Changed $false -Value '%%LOCALAPPDATA%%\Programs\Herdr Sandbox' -ExpandVariables $true
Assert-Update -Current '%%LOCALAPPDATA%%\Programs\Herdr Sandbox' -Action Add -Changed $true -Value "%%LOCALAPPDATA%%\Programs\Herdr Sandbox;$expandedTarget" -ExpandVariables $false
`, quote(pathHelper))
	scriptPath := filepath.Join(t.TempDir(), "path-ownership.ps1")
	if err := os.WriteFile(scriptPath, []byte(script), 0o600); err != nil {
		t.Fatal(err)
	}
	powerShell := filepath.Join(os.Getenv("SystemRoot"), "System32", "WindowsPowerShell", "v1.0", "powershell.exe")
	command := hiddenCommandContext(context.Background(), powerShell, "-NoLogo", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File", scriptPath)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("PATH ownership regression: %v: %s", err, output)
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
		`Get-ChildItem -LiteralPath 'build\dist' -File`,
		`$assets.Count -ne 4`,
		`$installers.Count -ne 1`,
		`VersionInfo.ProductName`,
		`'--title', "$productName $env:RELEASE_TAG"`,
	} {
		if !strings.Contains(workflow, want) {
			t.Fatalf("release workflow is missing %q", want)
		}
	}
	for _, forbidden := range []string{"Compress-Archive", "Invoke-WebRequest", "choco install", "cargo", "herdr.exe'"} {
		if strings.Contains(workflow, forbidden) {
			t.Fatalf("release workflow contains duplicate or out-of-scope packaging %q", forbidden)
		}
	}
}
