package main

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
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
		paths.ZIPChecksum != paths.ZIP+".sha256" || paths.InstallerChecksum != paths.Installer+".sha256" {
		t.Fatalf("release paths = %#v", paths)
	}
}

func TestInstallerDefinitionBindsSafeIdentityManifestAndLegacyMigration(t *testing.T) {
	version, err := parseReleaseVersion("v0.0.10")
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	output := filepath.Join(root, productidentity.ApplicationName+"_"+version.Tag+"_windows_amd64_setup.exe")
	definitionPath := filepath.Join(root, "definition.json")
	if err := writeInstallerDefinition(definitionPath, version, output); err != nil {
		t.Fatalf("writeInstallerDefinition: %v", err)
	}
	data, err := os.ReadFile(definitionPath)
	if err != nil {
		t.Fatal(err)
	}
	var definition installerDefinition
	if err := json.Unmarshal(data, &definition); err != nil {
		t.Fatal(err)
	}
	if definition.SchemaVersion != installerDefinitionSchemaVersion ||
		definition.InstallerSchemaVersion != installerSchemaVersion ||
		definition.ProductGUID != productidentity.ProductGUID ||
		definition.RegistryKeyName != productidentity.UninstallKeyName ||
		definition.MarkerFileName != productidentity.InstallerMarkerName ||
		definition.QuietUninstallHelperName != productidentity.QuietUninstallHelperName ||
		definition.OutputFileName != filepath.Base(output) ||
		definition.Legacy.Version != legacyInstallerVersion ||
		definition.Legacy.RegistryKeyName != productidentity.LegacyUninstallKeyName ||
		len(definition.Legacy.Files) != len(releasePackageFiles) ||
		!slices.Equal(definition.OwnedFiles, installerOwnedFiles()) {
		t.Fatalf("installer definition = %#v", definition)
	}
	for _, file := range definition.Legacy.Files {
		if !installerSHA256Pattern.MatchString(file.SHA256) {
			t.Fatalf("legacy file hash is invalid: %#v", file)
		}
	}
	for _, value := range []string{"", ".", "..", `folder\file.exe`, "file:name", "CON.txt", "name. ", "a/b"} {
		if err := validateInstallerLeaf("fixture", value); err == nil {
			t.Fatalf("unsafe installer leaf unexpectedly passed: %q", value)
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
		`CRCCheck force`,
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
		`KERNEL32::GetProcAddress(p r0, m "GetDpiForWindow")`,
		`System::Call '::$1(p $HWNDPARENT)i.r0'`,
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
		`!define MUI_PAGE_CUSTOMFUNCTION_SHOW PositionInstallerFinishLink`,
		`Function PositionInstallerFinishLink`,
		`USER32::DrawTextW`,
		`USER32::SetWindowPos(p $mui.FinishPage.Text`,
		`USER32::SetWindowPos(p $mui.FinishPage.Link`,
		`!insertmacro MUI_PAGE_LICENSE "${PACKAGE_DIR}\${APP_LICENSE}"`,
		`!insertmacro MUI_PAGE_FINISH`,
		`UninstPage custom un.DeleteConfigurationPage un.DeleteConfigurationPageLeave`,
		`StrCmp $0 "/DELETE_CONFIG" delete_config`,
		`StrCmp $0 "/S /DELETE_CONFIG" delete_config`,
		`Unsupported uninstall arguments`,
		`${NSD_CreateCheckbox}`,
		`Also delete ${APP_CONFIG_FILE} and ${APP_USER_SCRIPT}`,
		`A running Sandbox stays open but becomes unmanaged`,
		`any running Sandbox stays open`,
		`File "${PACKAGE_DIR}\${APP_BASE_SCRIPT}"`,
		`File "${PACKAGE_DIR}\${APP_EXECUTABLE}"`,
		`File "${PACKAGE_DIR}\${APP_LICENSE}"`,
		`File "${PACKAGE_DIR}\${APP_STACK_SCRIPT}"`,
		`File "/oname=${APP_QUIET_UNINSTALL_HELPER}" "${QUIET_UNINSTALL_HELPER}"`,
		`WriteUninstaller "$PLUGINSDIR\package\uninstall.exe"`,
		`File "/oname=installer-state.ps1" "${INSTALLER_STATE_HELPER}"`,
		`File "/oname=definition.json" "${INSTALLER_DEFINITION}"`,
		`RunInstallerState "Install"`,
		`RunInstallerState "RollbackInstall"`,
		`RunInstallerState "CommitInstall"`,
		`RunInstallerState "InspectUninstall"`,
		`RunInstallerState "MarkCleanupComplete"`,
		`RunInstallerState "FinishUninstall"`,
		`VIProductVersion "${FIXED_VERSION}"`,
		`VIAddVersionKey "OriginalFilename" "${OUTPUT_FILE_NAME}"`,
		`StrCpy $INSTDIR "$LOCALAPPDATA\Programs\${APP_INSTALL_DIRECTORY}"`,
		`__installer-seed-configuration`,
		`__installer-clean-uninstall`,
		`--installer-schema=1`,
		`--delete-configuration`,
		`!define APP_ENVIRONMENT_BROADCAST_TIMEOUT_MS 100`,
		`USER32::SendMessageTimeoutW`,
		`i ${APP_ENVIRONMENT_BROADCAST_TIMEOUT_MS}`,
		`$2 == 0`,
		`sign out and back in`,
		`!define APP_LIFECYCLE_MUTEX_NAME "Global\${APP_PRODUCT_GUID}.InstallerLifecycle.v2"`,
		`KERNEL32::CreateMutexW`,
		`KERNEL32::CloseHandle`,
		`APP_ERROR_ALREADY_EXISTS 183`,
		`Another ${APP_DISPLAY_NAME} setup or uninstall is already running.`,
		`${AtLeastWin10}`,
		`SetOutPath "$INSTDIR"`,
		`!define APP_EXIT_INVALID_ARGUMENTS 30`,
		`!define APP_EXIT_LIFECYCLE_BUSY 41`,
		`!define APP_EXIT_UNSUPPORTED_PLATFORM 50`,
		`!define APP_EXIT_INSTALL_RECOVERED 70`,
		`!define APP_EXIT_INSTALL_ROLLBACK_INCOMPLETE 71`,
		`!define APP_EXIT_UNINSTALL_PREFLIGHT 80`,
		`!define APP_EXIT_UNINSTALL_FINALIZE 81`,
		`!define APP_EXIT_INTERNAL_STATE 90`,
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("installer source is missing %q", want)
		}
	}
	for _, forbidden := range []string{
		`MUI_PAGE_DIRECTORY`,
		`CRCCheck off`,
		`MUI_FINISHPAGE_RUN`,
		`MUI_FINISHPAGE_SHOWREADME`,
		`MUI_UNPAGE_CONFIRM`,
		`RequestExecutionLevel admin`,
		`RMDir /r`,
		`$APPDATA\herdr-sandbox`,
		`$LOCALAPPDATA\herdr-sandbox`,
		`SendMessage ${HWND_BROADCAST}`,
		`SendNotifyMessage`,
		`Local\${APP_UNINSTALL_KEY}`,
		`ReadRegStr`,
		`ReadRegDWORD`,
		`WriteRegStr`,
		`WriteRegDWORD`,
		`DeleteRegKey`,
		`Delete "$INSTDIR`,
		`RMDir "$INSTDIR`,
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
	} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("installer source contains out-of-scope pattern %q", forbidden)
		}
	}
	cleanupIndex := strings.LastIndex(source, `__installer-clean-uninstall`)
	markIndex := strings.Index(source, `RunInstallerState "MarkCleanupComplete"`)
	finishIndex := strings.Index(source, `RunInstallerState "FinishUninstall"`)
	if cleanupIndex < 0 || markIndex <= cleanupIndex || finishIndex <= markIndex {
		t.Fatalf("clean uninstall must reach its durable phase before terminal file/registration cleanup")
	}
	for _, want := range []string{
		`InspectUninstall`,
		`MarkCleanupComplete`,
		`FinishUninstall`,
		`recorded phase will resume without guessing from missing files`,
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("installer source is missing resumable uninstall contract %q", want)
		}
	}
	activationIndex := strings.Index(source, `RunInstallerState "Install"`)
	seedIndex := strings.Index(source, `__installer-seed-configuration`)
	rollbackIndex := strings.Index(source, `RunInstallerState "RollbackInstall"`)
	commitIndex := strings.Index(source, `RunInstallerState "CommitInstall"`)
	if activationIndex < 0 || seedIndex <= activationIndex || rollbackIndex <= seedIndex || commitIndex <= rollbackIndex {
		t.Fatal("installer activation, transactional seed rollback, and commit are not ordered")
	}
	if strings.Count(source, `!insertmacro AcquireInstallerLifecycleMutex`) != 2 {
		t.Fatal("setup and uninstall must share one process-lifetime lifecycle mutex")
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
		`"/WX"`,
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
		`"/DAPP_PRODUCT_GUID=" + productidentity.ProductGUID`,
		`"/DAPP_UNINSTALL_KEY=" + productidentity.UninstallKeyName`,
		`"/DAPP_LEGACY_UNINSTALL_KEY=" + productidentity.LegacyUninstallKeyName`,
		`"/DAPP_INSTALLER_MARKER=" + productidentity.InstallerMarkerName`,
		`"/DAPP_QUIET_UNINSTALL_HELPER=" + productidentity.QuietUninstallHelperName`,
		`"/DAPP_COPYRIGHT=" + productidentity.Copyright`,
		`"/DINSTALLER_STATE_HELPER=" + installerStateHelper`,
		`"/DQUIET_UNINSTALL_HELPER=" + quietUninstallHelper`,
		`"/DINSTALLER_DEFINITION=" + definitionPath`,
		`"/DOUTPUT_FILE_NAME=" + filepath.Base(outputPath)`,
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("package task is missing canonical installer identity input %q", want)
		}
	}
	if strings.Contains(source, "LEGACY_LICENSE") {
		t.Fatal("package task must not carry a legacy license compatibility path")
	}
}

func TestInstallerStateHelperOwnsTransactionsAndPreservesUnownedState(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "packaging", "windows", "installer-state.ps1"))
	if err != nil {
		t.Fatal(err)
	}
	source := string(data)
	for _, want := range []string{
		`[ValidateSet('Install', 'RollbackInstall', 'CommitInstall', 'InspectUninstall', 'MarkCleanupComplete', 'FinishUninstall')]`,
		`RegistryValueOptions]::DoNotExpandEnvironmentNames`,
		`RegistryValueKind]::ExpandString`,
		`Test-PathEntry`,
		`Resolve-UserPathUpdate`,
		`Get-RegistryKeySnapshot`,
		`Restore-RegistryKeySnapshot`,
		`ProductGuid`,
		`InstallationId`,
		`InstallerSchemaVersion`,
		`Get-Marker`,
		`Get-FileSHA256`,
		`Invoke-InstallRollback`,
		`Complete-UninstallTransaction`,
		`Assert-TransactionState`,
		`Installer transaction kind is unknown and was preserved.`,
		`INSTALL_ROLLBACK_INCOMPLETE:`,
		`Copy-FileDurable`,
		`[IO.FileOptions]::WriteThrough`,
		`discarded interrupted pre-journal installer preparation`,
		`Legacy installer directory contains an unowned file at reserved installer path`,
		`sha256 = Get-FileSHA256 -Path $path`,
		`FileShare]::None`,
		`$kept = New-Object 'Collections.Generic.List[string]'`,
		`[string]::Join(';', [string[]]$kept)`,
		`Write-InstallerLog`,
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("installer state helper is missing %q", want)
		}
	}
	for _, forbidden := range []string{"Herdr Sandbox", `"herdr-sandbox"`, "HERDR_SANDBOX_INSTALL_DIRECTORY"} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("installer state helper contains product-specific or unrelated state pattern %q", forbidden)
		}
	}
	if strings.Contains(source, "Legacy installer directory contains unknown or missing entries") {
		t.Fatal("verified legacy repair still rejects unrelated or missing directory entries")
	}
	legacyStart := strings.Index(source, "function Get-LegacyFileRecords")
	if legacyStart < 0 {
		t.Fatal("legacy backup record owner is missing")
	}
	legacyEnd := strings.Index(source[legacyStart:], "function Remove-SafeTransactionDirectory")
	if legacyEnd < 0 {
		t.Fatal("legacy backup record owner has no bounded end")
	}
	if strings.Contains(source[legacyStart:legacyStart+legacyEnd], "sha256 = [string]$record.sha256; size") {
		t.Fatal("legacy backup records still require the published payload hash instead of the actual repair input")
	}
}

func TestInstallerStateHelperRejectsMalformedTransactionBeforeMutation(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows PowerShell 5.1 transaction validation regression")
	}
	root := t.TempDir()
	localAppData := filepath.Join(root, "LocalAppData")
	installDirectory := filepath.Join(localAppData, "Programs", productidentity.InstallDirectoryName)
	transactionDirectory := filepath.Join(filepath.Dir(installDirectory), "."+productidentity.ApplicationName+"-installer-transaction")
	if err := os.MkdirAll(transactionDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	version := releaseVersion{Tag: "v0.0.10", Display: "0.0.10", Fixed: "0.0.10.0"}
	definitionPath := filepath.Join(root, "definition.json")
	outputPath := filepath.Join(root, productidentity.ApplicationName+"_"+version.Tag+"_windows_amd64_setup.exe")
	if err := writeInstallerDefinition(definitionPath, version, outputPath); err != nil {
		t.Fatal(err)
	}
	helper, err := filepath.Abs(filepath.Join("..", "..", "packaging", "windows", "installer-state.ps1"))
	if err != nil {
		t.Fatal(err)
	}
	powerShell := filepath.Join(os.Getenv("SystemRoot"), "System32", "WindowsPowerShell", "v1.0", "powershell.exe")
	newState := func() map[string]any {
		return map[string]any{
			"schemaVersion": 1, "transactionId": "11111111-1111-4111-8111-111111111111",
			"kind": "Unknown", "phase": "Prepared", "installDirectory": installDirectory,
			"installationId": "22222222-2222-4222-8222-222222222222", "installationState": "Fresh",
			"currentRegistry": map[string]any{"exists": false, "values": []any{}},
			"legacyRegistry":  map[string]any{"exists": false, "values": []any{}},
			"pathBefore":      map[string]any{"keyExists": false, "exists": false, "kind": "", "data": ""},
			"pathAfter":       nil, "pathChanged": false, "oldFiles": []any{}, "newFiles": []any{}, "cleanupComplete": false,
		}
	}
	for _, test := range []struct {
		name     string
		mutate   func(map[string]any)
		wantText string
	}{
		{name: "unknown kind", mutate: func(map[string]any) {}, wantText: "kind is unknown and was preserved"},
		{name: "string boolean", mutate: func(state map[string]any) { state["pathChanged"] = "false" }, wantText: "must be a JSON boolean"},
	} {
		t.Run(test.name, func(t *testing.T) {
			state := newState()
			test.mutate(state)
			encoded, err := json.Marshal(state)
			if err != nil {
				t.Fatal(err)
			}
			statePath := filepath.Join(transactionDirectory, "state.json")
			if err := os.WriteFile(statePath, append(encoded, '\n'), 0o600); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			command := hiddenCommandContext(ctx, powerShell,
				"-NoLogo", "-NoProfile", "-NonInteractive", "-WindowStyle", "Hidden", "-ExecutionPolicy", "Bypass",
				"-File", helper, "-Action", "InspectUninstall", "-DefinitionPath", definitionPath, "-InstallDirectory", installDirectory)
			command.Env = append(os.Environ(), "LOCALAPPDATA="+localAppData, "TEMP="+root, "TMP="+root)
			output, runErr := command.CombinedOutput()
			if runErr == nil || !strings.Contains(string(output), test.wantText) {
				t.Fatalf("malformed transaction result = %v: %s", runErr, output)
			}
			if _, err := os.Stat(statePath); err != nil {
				t.Fatalf("malformed transaction was not preserved: %v", err)
			}
		})
	}
}

func TestInstallerStateHelperPreservesPathOwnershipInWindowsPowerShell51(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows PowerShell 5.1 PATH ownership regression")
	}
	pathHelper, err := filepath.Abs(filepath.Join("..", "..", "packaging", "windows", "installer-state.ps1"))
	if err != nil {
		t.Fatal(err)
	}
	quote := func(value string) string { return strings.ReplaceAll(value, "'", "''") }
	script := fmt.Sprintf(`$ErrorActionPreference = 'Stop'
$tokens = $null
$errors = $null
$ast = [Management.Automation.Language.Parser]::ParseFile('%s', [ref]$tokens, [ref]$errors)
if ($errors.Count -ne 0) { throw $errors[0].Message }
foreach ($name in @('Get-NormalizedPath', 'Test-PathEntry', 'Resolve-UserPathUpdate')) {
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
		`go run ./cmd/task release-notes $env:RELEASE_TAG`,
		`Get-ChildItem -LiteralPath 'build\dist' -File`,
		`$assets.Count -ne 4`,
		`$installers.Count -ne 1`,
		`VersionInfo.ProductName`,
		`'--notes', $releaseNotes`,
		`'--title', "$productName $env:RELEASE_TAG"`,
	} {
		if !strings.Contains(workflow, want) {
			t.Fatalf("release workflow is missing %q", want)
		}
	}
	for _, forbidden := range []string{"--generate-notes", "Compress-Archive", "Invoke-WebRequest", "choco install", "cargo", "herdr.exe'"} {
		if strings.Contains(workflow, forbidden) {
			t.Fatalf("release workflow contains duplicate or out-of-scope packaging %q", forbidden)
		}
	}
}
