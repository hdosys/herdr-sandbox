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

func TestValidateReplacedExecutableNameRejectsPathsAndCurrentPayloadCollisions(t *testing.T) {
	if err := validateReplacedExecutableName(productidentity.ReplacedExecutableName); err != nil {
		t.Fatalf("canonical replaced executable: %v", err)
	}
	for _, value := range []string{
		"",
		"..",
		`..\sandbox.exe`,
		productidentity.ExecutableName,
		strings.ToUpper(installerUninstallerName),
		productidentity.BaseScriptName,
		productidentity.InstallerMarkerName,
	} {
		if err := validateReplacedExecutableName(value); err == nil {
			t.Fatalf("replaced executable %q unexpectedly passed validation", value)
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
	version, err := parseReleaseVersion("v0.0.10")
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	output := filepath.Join(root, productidentity.ApplicationName+"_"+version.Tag+"_windows_amd64_setup.exe")
	if err := validateInstallerBuildInputs(version, output); err != nil {
		t.Fatalf("validateInstallerBuildInputs: %v", err)
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
		`!define APP_INSTALLER_MUTEX_NAME "Global\${APP_PRODUCT_GUID}.InstallerExclusive"`,
		`!define APP_LIFECYCLE_MUTEX_NAME "Local\${APP_APPLICATION_NAME}-lifecycle-v1"`,
		`!insertmacro AcquireInstallerMutex ${APP_EXIT_INSTALL_FAILED}`,
		`!insertmacro AcquireInstallerMutex ${APP_EXIT_UNINSTALL_FAILED}`,
		`!insertmacro AcquireLifecycleMutex ${APP_EXIT_INSTALL_FAILED}`,
		`Another ${APP_DISPLAY_NAME} setup or uninstall is running.`,
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
		`!define MUI_FINISHPAGE_TEXT_LARGE`,
		`!define MUI_FINISHPAGE_TITLE "${APP_DISPLAY_NAME} ${VERSION} is installed"`,
		`Setup is complete. No app window opens. Open a new terminal:`,
		`${APP_NAME} init: Create a project profile`,
		`${APP_NAME} up: Start or reconnect`,
		`${APP_NAME} config: Open the configuration file`,
		`${APP_NAME} status: Inspect Sandbox state`,
		`!define MUI_FINISHPAGE_RUN`,
		`!define MUI_FINISHPAGE_RUN_TEXT "Open ${APP_DISPLAY_NAME} configuration"`,
		`!define MUI_FINISHPAGE_RUN_FUNCTION OpenInstalledConfiguration`,
		`!define MUI_FINISHPAGE_LINK "Open setup and usage guide"`,
		`!define MUI_FINISHPAGE_LINK_LOCATION "${APP_PRODUCT_URL}"`,
		`!define MUI_CUSTOMFUNCTION_ABORT PreventInstallMutationAbort`,
		`!define MUI_CUSTOMFUNCTION_UNABORT un.PreventUninstallMutationAbort`,
		`Function un.DisableUninstallCancellation`,
		`!define MUI_PAGE_CUSTOMFUNCTION_SHOW ConfigureInstallerFinishPage`,
		`Function ConfigureInstallerFinishPage`,
		`${NSD_Uncheck} $mui.FinishPage.Run`,
		`ShowWindow $mui.FinishPage.Run ${SW_HIDE}`,
		`${NSD_SetFocus} $mui.Button.Next`,
		`Function OpenInstalledConfiguration`,
		`IfSilent done`,
		`${If} $ExistingInstallation == "1"`,
		`__installer-open-configuration`,
		`Run ${APP_NAME} config from a new terminal.`,
		`!insertmacro MUI_PAGE_LICENSE "${PACKAGE_DIR}\${APP_LICENSE}"`,
		`!insertmacro MUI_PAGE_FINISH`,
		`UninstPage custom un.DeleteConfigurationPage un.DeleteConfigurationPageLeave`,
		`${GetOptions} $0 " /DELETE_CONFIG " $1`,
		`${NSD_CreateCheckbox}`,
		`Also delete ${APP_CONFIG_FILE} and ${APP_USER_SCRIPT}`,
		`A running Sandbox stays open but becomes unmanaged`,
		`File "${PACKAGE_DIR}\${APP_BASE_SCRIPT}"`,
		`File "${PACKAGE_DIR}\${APP_EXECUTABLE}"`,
		`File "${PACKAGE_DIR}\${APP_LICENSE}"`,
		`File "${PACKAGE_DIR}\${APP_STACK_SCRIPT}"`,
		`File "/oname=${APP_QUIET_UNINSTALL_HELPER}" "${QUIET_UNINSTALL_HELPER}"`,
		`WriteUninstaller "$PLUGINSDIR\package\uninstall.exe"`,
		`File "/oname=path.ps1" "${PATH_HELPER}"`,
		`!macro WriteOwnershipMarker PATH`,
		`FileWrite $0 '{"productGuid":"${APP_PRODUCT_GUID}","installerSchema":${APP_INSTALLER_SCHEMA}}`,
		`!insertmacro VerifyExactOwnershipMarker "${PATH}"`,
		`!insertmacro GetRegularFileState "$INSTDIR\${NAME}"`,
		`ReadRegStr $0 HKCU "${UNINSTALL_KEY}" "ProductGuid"`,
		`ReadRegStr $1 HKCU "${UNINSTALL_KEY}" "InstallLocation"`,
		`EnumRegValue $0 HKCU "${UNINSTALL_KEY}" 0`,
		`The fixed install directory is nonempty but unmarked`,
		`The incomplete registration points to another location`,
		`!insertmacro BackupFile "${APP_INSTALLER_MARKER}"`,
		`!insertmacro InstallFile "${APP_INSTALLER_MARKER}"`,
		`!insertmacro InstallFile "${APP_BASE_SCRIPT}"`,
		`!insertmacro InstallFile "${APP_EXECUTABLE}"`,
		`!insertmacro RestoreFile "${APP_EXECUTABLE}"`,
		`!insertmacro RestoreFile "${APP_INSTALLER_MARKER}"`,
		`StrCpy $PathAction "Contains"`,
		`StrCpy $PathAction "Add"`,
		`StrCpy $PathAction "Remove"`,
		`ReadRegDWORD $PathPending HKCU "${UNINSTALL_KEY}" "PathAddPending"`,
		`WriteRegDWORD HKCU "${UNINSTALL_KEY}" "PathAddPending" 1`,
		`DeleteRegValue HKCU "${UNINSTALL_KEY}" "PathAddPending"`,
		`WriteRegDWORD HKCU "${UNINSTALL_KEY}" "PathAdded" $PathOwned`,
		`WriteRegStr HKCU "${UNINSTALL_KEY}" "ProductGuid" "${APP_PRODUCT_GUID}"`,
		`WriteRegDWORD HKCU "${UNINSTALL_KEY}" "InstallerSchemaVersion" ${APP_INSTALLER_SCHEMA}`,
		`WriteRegDWORD HKCU "${UNINSTALL_KEY}" "InstallComplete" 0`,
		`WriteRegDWORD HKCU "${UNINSTALL_KEY}" "InstallComplete" 1`,
		`WriteRegDWORD HKCU "${UNINSTALL_KEY}" "CleanupComplete" 1`,
		`Function un.CheckDirectoryResidual`,
		`!define APP_ERROR_FILE_NOT_FOUND 2`,
		`!define APP_ERROR_PATH_NOT_FOUND 3`,
		`GetFileAttributesW(w "$INSTDIR") i.r0 ?e`,
		`!insertmacro DeleteFinal "${APP_QUIET_UNINSTALL_HELPER}"`,
		`!insertmacro DeleteRequiredAfterRegistration "uninstall.exe"`,
		`KERNEL32::CopyFileW(w "$EXEPATH", w "$INSTDIR\uninstall.exe", i 0)`,
		`!insertmacro DeleteFinal "${APP_INSTALLER_MARKER}"`,
		`!insertmacro AcquireLifecycleMutex ${APP_EXIT_UNINSTALL_FAILED}`,
		`--installer-lifecycle-lock-held`,
		`Cleanup will run again on the next uninstall attempt`,
		`The previous files were restored`,
		`The application will retry when needed`,
		`MessageBox MB_ICONSTOP|MB_OK`,
		`/SD IDOK`,
		`VIProductVersion "${FIXED_VERSION}"`,
		`VIAddVersionKey "OriginalFilename" "${OUTPUT_FILE_NAME}"`,
		`StrCpy $INSTDIR "$LOCALAPPDATA\Programs\${APP_INSTALL_DIRECTORY}"`,
		`__installer-seed-configuration`,
		`__installer-clean-uninstall`,
		`--installer-schema=${APP_INSTALLER_SCHEMA}`,
		`--delete-configuration`,
		`!define APP_ENVIRONMENT_BROADCAST_TIMEOUT_MS 250`,
		`USER32::SendMessageTimeoutW`,
		`i ${APP_ENVIRONMENT_BROADCAST_TIMEOUT_MS}`,
		`$2 == 0`,
		`sign out and back in`,
		`KERNEL32::CreateMutexW`,
		`KERNEL32::WaitForSingleObject`,
		`KERNEL32::ReleaseMutex`,
		`KERNEL32::CloseHandle`,
		`APP_WAIT_ABANDONED 128`,
		`APP_WAIT_TIMEOUT 258`,
		`${AtLeastWin10}`,
		`SetOutPath "$INSTDIR"`,
		`!define APP_EXIT_LIFECYCLE_BUSY 41`,
		`!define APP_EXIT_UNSUPPORTED_PLATFORM 50`,
		`!define APP_EXIT_INSTALL_FAILED 70`,
		`!define APP_EXIT_UNINSTALL_FAILED 80`,
		`Delete "$INSTDIR\${NAME}"`,
		`DeleteRegKey HKCU "${UNINSTALL_KEY}"`,
		`RMDir "$INSTDIR"`,
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("installer source is missing %q", want)
		}
	}
	for _, forbidden := range []string{
		`MUI_PAGE_DIRECTORY`,
		`CRCCheck off`,
		`MUI_FINISHPAGE_RUN_NOTCHECKED`,
		`MUI_FINISHPAGE_SHOWREADME`,
		`MUI_PAGE_CUSTOMFUNCTION_LEAVE OpenInstalledConfiguration`,
		`MUI_UNPAGE_CONFIRM`,
		`RequestExecutionLevel admin`,
		`RMDir /r`,
		`$APPDATA\herdr-sandbox`,
		`$LOCALAPPDATA\herdr-sandbox`,
		`SendMessage ${HWND_BROADCAST}`,
		`SendNotifyMessage`,
		`Local\${APP_UNINSTALL_KEY}`,
		`installer-state.ps1`,
		`InstallerLifecycle.v2`,
		`Global\${APP_PRODUCT_GUID}.InstallerLifecycle.v3`,
		`acquireInstallerLifecycleGate`,
		`APP_LIFECYCLE_WAIT_INTERVAL_MS`,
		`APP_LIFECYCLE_WAIT_ATTEMPTS`,
		`INSTALLER_STATE_HELPER`,
		`INSTALLER_DEFINITION`,
		`RunInstallerState`,
		`/TIMEOUT=`,
		`installer-transaction`,
		`CleanupIncomplete`,
		`RestoreRetryOwnership`,
		`PreflightOwnedFile`,
		`state.json`,
		`File /r`,
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
		`Stopping the app-owned Sandbox`,
		`Function PositionInstallerFinishLink`,
		`MUI_PAGE_CUSTOMFUNCTION_SHOW PolishInstallerFinishPage`,
		`Function PolishInstallerFinishPage`,
	} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("installer source contains out-of-scope pattern %q", forbidden)
		}
	}
	markerReplaceIndex := strings.Index(source, `!insertmacro InstallFile "${APP_INSTALLER_MARKER}"`)
	baseReplaceIndex := strings.Index(source, `!insertmacro InstallFile "${APP_BASE_SCRIPT}"`)
	executableReplaceIndex := strings.Index(source, `!insertmacro InstallFile "${APP_EXECUTABLE}"`)
	if markerReplaceIndex < 0 || baseReplaceIndex <= markerReplaceIndex || executableReplaceIndex <= baseReplaceIndex {
		t.Fatal("setup must establish its marker, replace support files, and copy the executable last")
	}
	installStart := strings.Index(source, `Section "Install"`)
	uninstallStart := strings.Index(source, `Section "Uninstall"`)
	if installStart < 0 || uninstallStart <= installStart {
		t.Fatal("missing installer sections")
	}
	installSource := source[installStart:uninstallStart]
	installMutationIndex := strings.Index(installSource, `StrCpy $InstallMutationActive "1"`)
	disableInstallCancelIndex := strings.Index(installSource, `Call DisableInstallCancellation`)
	createInstallRootIndex := strings.Index(installSource, `CreateDirectory "$INSTDIR"`)
	incompleteWriteIndex := strings.Index(installSource, `WriteRegDWORD HKCU "${UNINSTALL_KEY}" "InstallComplete" 0`)
	installMarkerReplaceIndex := strings.Index(installSource, `!insertmacro InstallFile "${APP_INSTALLER_MARKER}"`)
	pathIntentIndex := strings.Index(installSource, `WriteRegDWORD HKCU "${UNINSTALL_KEY}" "PathAddPending" 1`)
	pathAddIndex := strings.Index(installSource, `StrCpy $PathAction "Add"`)
	pathOwnershipCommitIndex := strings.Index(installSource, `WriteRegDWORD HKCU "${UNINSTALL_KEY}" "PathAdded" $PathOwned`)
	pathIntentDeleteIndex := strings.LastIndex(installSource, `DeleteRegValue HKCU "${UNINSTALL_KEY}" "PathAddPending"`)
	installCommitIndex := strings.Index(installSource, `WriteRegDWORD HKCU "${UNINSTALL_KEY}" "InstallComplete" 1`)
	if incompleteWriteIndex < 0 || installMarkerReplaceIndex <= incompleteWriteIndex {
		t.Fatal("an owned upgrade must become incomplete before replacing installed payload")
	}
	if installMutationIndex < 0 || disableInstallCancelIndex <= installMutationIndex || createInstallRootIndex <= disableInstallCancelIndex {
		t.Fatal("install cancellation must be disabled before creating or changing installed state")
	}
	if pathIntentIndex < 0 || pathAddIndex <= pathIntentIndex || pathOwnershipCommitIndex <= pathAddIndex || pathIntentDeleteIndex <= pathAddIndex {
		t.Fatal("PATH Add intent must precede mutation and reach an explicit terminal decision before install commit")
	}
	if !strings.Contains(installSource, `pending intent plus a present entry is ambiguous after a crash`) {
		t.Fatal("stale PATH Add intent must recover without claiming ambiguous ownership")
	}
	rollbackStart := strings.Index(installSource, `install_rollback:`)
	if rollbackStart < 0 || !strings.Contains(installSource[rollbackStart:], `$InstallCompleteWasComplete == "1"`) ||
		!strings.Contains(installSource[rollbackStart:], `WriteRegDWORD HKCU "${UNINSTALL_KEY}" "InstallComplete" 1`) {
		t.Fatal("successful payload rollback must restore a previously complete install marker")
	}
	rollbackSource := installSource[rollbackStart:]
	rollbackRootCleanupIndex := strings.Index(rollbackSource, `RMDir "$INSTDIR"`)
	rollbackCancellationEndIndex := strings.Index(rollbackSource, `StrCpy $InstallMutationActive "0"`)
	if rollbackRootCleanupIndex < 0 || rollbackCancellationEndIndex <= rollbackRootCleanupIndex {
		t.Fatal("install cancellation protection must remain active through terminal rollback root cleanup")
	}
	retryRegistrationStart := strings.Index(source, `Function un.RestoreRetryRegistration`)
	retryRegistrationEnd := -1
	if retryRegistrationStart >= 0 {
		retryRegistrationEnd = strings.Index(source[retryRegistrationStart:], `FunctionEnd`)
	}
	if retryRegistrationStart < 0 || retryRegistrationEnd < 0 {
		t.Fatal("missing retry registration restoration owner")
	}
	retryRegistrationSource := source[retryRegistrationStart : retryRegistrationStart+retryRegistrationEnd]
	copyUninstallerIndex := strings.Index(retryRegistrationSource, `KERNEL32::CopyFileW(w "$EXEPATH", w "$INSTDIR\uninstall.exe", i 0)`)
	copyFailureIndex := strings.Index(retryRegistrationSource, `${If} $0 == 0`)
	revalidateUninstallerIndex := strings.LastIndex(retryRegistrationSource, `!insertmacro GetRegularFileState "$INSTDIR\uninstall.exe"`)
	registrationWriteIndex := strings.Index(retryRegistrationSource, `WriteRegStr HKCU "${UNINSTALL_KEY}" "UninstallString"`)
	if copyUninstallerIndex < 0 || copyFailureIndex <= copyUninstallerIndex || revalidateUninstallerIndex <= copyFailureIndex || registrationWriteIndex <= revalidateUninstallerIndex {
		t.Fatal("retry registration must fail on copy failure and revalidate the installed uninstaller before publishing registration")
	}
	installCompleteIndex := installStart + installCommitIndex
	seedIndex := strings.Index(source, `__installer-seed-configuration`)
	if installCompleteIndex < 0 || seedIndex <= installCompleteIndex {
		t.Fatal("payload and registration must commit before best-effort configuration seeding")
	}
	cleanupIndex := strings.LastIndex(source, `__installer-clean-uninstall`)
	cleanupMarkerIndex := strings.LastIndex(source, `WriteRegDWORD HKCU "${UNINSTALL_KEY}" "CleanupComplete" 1`)
	pathRemoveIndex := strings.LastIndex(source, `StrCpy $PathAction "Remove"`)
	baseDeleteIndex := strings.LastIndex(source, `!insertmacro DeleteRetryable "${APP_BASE_SCRIPT}"`)
	executableDeleteIndex := strings.LastIndex(source, `!insertmacro DeleteRetryable "${APP_EXECUTABLE}"`)
	registryDeleteIndex := strings.LastIndex(source, `DeleteRegKey HKCU "${UNINSTALL_KEY}"`)
	quietDeleteIndex := strings.LastIndex(source, `!insertmacro DeleteFinal "${APP_QUIET_UNINSTALL_HELPER}"`)
	uninstallerDeleteIndex := strings.LastIndex(source, `!insertmacro DeleteRequiredAfterRegistration "uninstall.exe"`)
	residualCheckIndex := strings.LastIndex(source, `Call un.CheckDirectoryResidual`)
	markerDeleteIndex := strings.LastIndex(source, `!insertmacro DeleteFinal "${APP_INSTALLER_MARKER}"`)
	directoryDeleteIndex := strings.LastIndex(source, `RMDir "$INSTDIR"`)
	if cleanupIndex < 0 || cleanupMarkerIndex <= cleanupIndex ||
		pathRemoveIndex <= cleanupMarkerIndex || baseDeleteIndex <= pathRemoveIndex ||
		executableDeleteIndex <= cleanupMarkerIndex || baseDeleteIndex <= executableDeleteIndex || registryDeleteIndex <= baseDeleteIndex ||
		uninstallerDeleteIndex <= registryDeleteIndex || quietDeleteIndex <= uninstallerDeleteIndex ||
		residualCheckIndex <= quietDeleteIndex || markerDeleteIndex <= residualCheckIndex ||
		directoryDeleteIndex <= markerDeleteIndex {
		t.Fatal("uninstall must delete registration, uninstaller, quiet helper, then the final ownership marker")
	}
	if strings.Count(source, `StrCpy $ExistingRegistryOwned "1"`) != 2 {
		t.Fatal("complete and marker-backed incomplete registration must share repair ownership")
	}
	if !strings.Contains(source, "Quit") || strings.Count(source, `/SD IDOK`) < 10 || strings.Count(source, `SetErrorLevel`) < 8 {
		t.Fatal("interactive failures need messages while silent failures retain stable nonblocking exits")
	}
	if strings.Count(source, `!insertmacro AcquireInstallerMutex ${APP_EXIT_INSTALL_FAILED}`) != 1 ||
		strings.Count(source, `!insertmacro AcquireInstallerMutex ${APP_EXIT_UNINSTALL_FAILED}`) != 1 {
		t.Fatal("setup and uninstall must acquire the shared installer-only mutex inside their sections")
	}
	mutexStart := strings.Index(source, `!macro AcquireInstallerMutex FAILURE_CODE`)
	if mutexStart < 0 {
		t.Fatal("installer lifecycle mutex macro is missing")
	}
	mutexEnd := strings.Index(source[mutexStart:], `!macroend`)
	if mutexEnd < 0 {
		t.Fatal("installer lifecycle mutex macro is missing")
	}
	mutexSource := source[mutexStart : mutexStart+mutexEnd]
	createIndex := strings.Index(mutexSource, `KERNEL32::CreateMutexW`)
	waitIndex := strings.Index(mutexSource, `KERNEL32::WaitForSingleObject`)
	ownedIndex := strings.Index(mutexSource, `${OrIf} $0 == ${APP_WAIT_ABANDONED}`)
	timeoutIndex := strings.Index(mutexSource, `${ElseIf} $0 == ${APP_WAIT_TIMEOUT}`)
	closeIndex := strings.Index(mutexSource, `KERNEL32::CloseHandle`)
	if createIndex < 0 || waitIndex <= createIndex || ownedIndex <= waitIndex || timeoutIndex <= ownedIndex ||
		closeIndex <= timeoutIndex || !strings.Contains(mutexSource, `APP_EXIT_LIFECYCLE_BUSY`) {
		t.Fatal("installer must acquire new and abandoned mutexes while failing only for a live owner")
	}
	if strings.Contains(source[:installStart], `!insertmacro AcquireInstallerMutex`) || strings.Contains(source[:installStart], `!insertmacro AcquireLifecycleMutex`) {
		t.Fatal("installer mutexes must be acquired by the Install and Uninstall section thread, not initialization callbacks")
	}
	if strings.Count(source, `!insertmacro AcquireLifecycleMutex ${APP_EXIT_INSTALL_FAILED}`) != 1 ||
		strings.Count(source, `!insertmacro AcquireLifecycleMutex ${APP_EXIT_UNINSTALL_FAILED}`) != 1 {
		t.Fatal("setup and uninstall must acquire the application lifecycle mutex inside their sections")
	}
	destructiveUninstallIndex := strings.Index(source[uninstallStart:], `StrCpy $InstallMutationActive "1"`)
	disableCancelIndex := strings.Index(source[uninstallStart:], `Call un.DisableUninstallCancellation`)
	if destructiveUninstallIndex < 0 || disableCancelIndex <= destructiveUninstallIndex {
		t.Fatal("uninstall must disable cancellation before destructive cleanup begins")
	}
	welcomePageIndex := strings.Index(source, `!insertmacro MUI_PAGE_WELCOME`)
	licensePageIndex := strings.Index(source, `!insertmacro MUI_PAGE_LICENSE`)
	installFilesIndex := strings.Index(source, `!insertmacro MUI_PAGE_INSTFILES`)
	finishOptionIndex := strings.Index(source, `!define MUI_FINISHPAGE_RUN_FUNCTION OpenInstalledConfiguration`)
	finishPageIndex := strings.Index(source, `!insertmacro MUI_PAGE_FINISH`)
	uninstallPageIndex := strings.Index(source, `UninstPage custom un.DeleteConfigurationPage`)
	if welcomePageIndex < 0 || licensePageIndex <= welcomePageIndex || installFilesIndex <= licensePageIndex || finishPageIndex <= installFilesIndex || uninstallPageIndex <= finishPageIndex {
		t.Fatal("installer flow must end Welcome/License/Files/Finish before uninstaller pages")
	}
	if finishOptionIndex < 0 || finishOptionIndex >= finishPageIndex {
		t.Fatal("configuration-open option must be defined before the Finish page is declared")
	}
	openFunctionStart := strings.Index(source, `Function OpenInstalledConfiguration`)
	if openFunctionStart < 0 {
		t.Fatal("missing fresh-install configuration-open callback")
	}
	openFunctionEnd := strings.Index(source[openFunctionStart:], `FunctionEnd`)
	if openFunctionEnd < 0 {
		t.Fatal("unterminated fresh-install configuration-open callback")
	}
	openFunctionSource := source[openFunctionStart : openFunctionStart+openFunctionEnd]
	for _, want := range []string{`IfSilent done`, `${If} $ExistingInstallation == "1"`, `__installer-open-configuration`} {
		if !strings.Contains(openFunctionSource, want) {
			t.Fatalf("fresh-install configuration-open callback is missing %q", want)
		}
	}
	finishConfigurationStart := strings.Index(source, `Function ConfigureInstallerFinishPage`)
	if finishConfigurationStart < 0 {
		t.Fatal("missing fresh-install Finish-page configuration")
	}
	finishConfigurationEnd := strings.Index(source[finishConfigurationStart:], `FunctionEnd`)
	if finishConfigurationEnd < 0 {
		t.Fatal("unterminated fresh-install Finish-page configuration")
	}
	finishConfigurationSource := source[finishConfigurationStart : finishConfigurationStart+finishConfigurationEnd]
	for _, want := range []string{`${If} $ExistingInstallation == "1"`, `${NSD_Uncheck} $mui.FinishPage.Run`, `ShowWindow $mui.FinishPage.Run ${SW_HIDE}`} {
		if !strings.Contains(finishConfigurationSource, want) {
			t.Fatalf("fresh-install Finish-page configuration is missing %q", want)
		}
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
		`"/DAPP_REPLACED_EXECUTABLE=" + productidentity.ReplacedExecutableName`,
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
		`"/DAPP_INSTALLER_MARKER=" + productidentity.InstallerMarkerName`,
		`"/DAPP_QUIET_UNINSTALL_HELPER=" + productidentity.QuietUninstallHelperName`,
		`"/DAPP_COPYRIGHT=" + productidentity.Copyright`,
		`"/DPATH_HELPER=" + pathHelper`,
		`"/DQUIET_UNINSTALL_HELPER=" + quietUninstallHelper`,
		`"/DOUTPUT_FILE_NAME=" + filepath.Base(outputPath)`,
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("package task is missing canonical installer identity input %q", want)
		}
	}
	for _, forbidden := range []string{"LegacyUninstallKeyName", "legacyInstaller", "APP_LEGACY_UNINSTALL_KEY", "installerDefinition", "installer-state.ps1"} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("package task still carries installer backward compatibility %q", forbidden)
		}
	}
}

func TestInstallerBuildValidatorOwnsPowerShell51InputChecks(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "packaging", "windows", installerBuildValidatorName))
	if err != nil {
		t.Fatal(err)
	}
	source := string(data)
	for _, want := range []string{
		`Assert-SafeInstallerText`,
		`Assert-SafeWindowsLeaf`,
		`Assert-PowerShellSyntax`,
		`Assert-BitmapDimensions`,
		`AppProductGuid must be a canonical lowercase GUID.`,
		`AppUninstallKey must be the brace-wrapped uppercase product GUID.`,
		`OutputFileName does not match the basename of OutputFile.`,
		`AppExecutable must use the .exe extension.`,
		`Installer build inputs validated successfully.`,
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("installer build validator is missing %q", want)
		}
	}
	if strings.Contains(source, "pwsh") {
		t.Fatal("installer build validator must remain Windows PowerShell 5.1-only")
	}
}

func TestInstallerPathHelperConvergesEquivalentLiteralPathTokens(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "packaging", "windows", "path.ps1"))
	if err != nil {
		t.Fatal(err)
	}
	source := string(data)
	for _, want := range []string{
		`[ValidateSet('Contains', 'Add', 'Remove')]`,
		`RegistryValueOptions]::DoNotExpandEnvironmentNames`,
		`RegistryValueKind]::ExpandString`,
		`Test-FullyQualifiedWindowsPath`,
		`Test-EffectivePathEntry`,
		`Test-OwnedPathEntry`,
		`[StringComparison]::OrdinalIgnoreCase`,
		`Resolve-UserPathUpdate`,
		`Get-UserPathSnapshot`,
		`Test-SnapshotEqual`,
		`$kept = New-Object 'Collections.Generic.List[string]'`,
		`[string]::Join(';', $remaining)`,
		`[Console]::Out.Write`,
		`exit 10`,
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("installer PATH helper is missing %q", want)
		}
	}
	for _, forbidden := range []string{"Herdr Sandbox", `"herdr-sandbox"`, "HERDR_SANDBOX_INSTALL_DIRECTORY", "Remove-Item", "ProductGuid", "InstallComplete", "CleanupComplete", "Transaction", "IndexOf('%')"} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("installer PATH helper contains product-specific or unrelated state pattern %q", forbidden)
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
foreach ($name in @('Test-FullyQualifiedWindowsPath', 'Get-NormalizedPath', 'Test-EffectivePathEntry', 'Test-OwnedPathEntry', 'Resolve-UserPathUpdate')) {
    $definition = $ast.Find({ param($node) $node -is [Management.Automation.Language.FunctionDefinitionAst] -and $node.Name -ceq $name }, $true)
    if ($null -eq $definition) { throw "Missing function $name" }
    Invoke-Expression $definition.Extent.Text
}
$target = 'C:\Users\Example\AppData\Local\Programs\Herdr Sandbox'
function Assert-Update {
    param([string]$Current, [string]$Action, [bool]$Changed, [bool]$Present, [string]$Value, [bool]$ExpandVariables = $false)
    $actual = Resolve-UserPathUpdate -Current $Current -Expected $target -RequestedAction $Action -ExpandVariables $ExpandVariables
    if ([bool]$actual.Changed -ne $Changed -or [bool]$actual.Present -ne $Present -or [string]$actual.Value -cne $Value) {
        throw "PATH update $Action [$Current] = changed=$($actual.Changed) present=$($actual.Present) value=[$($actual.Value)]"
    }
}
Assert-Update -Current '' -Action Contains -Changed $false -Present $false -Value ''
Assert-Update -Current '' -Action Add -Changed $true -Present $true -Value $target
Assert-Update -Current 'C:\Tools' -Action Add -Changed $true -Present $true -Value "C:\Tools;$target"
$added = Resolve-UserPathUpdate -Current 'C:\Tools;' -Expected $target -RequestedAction Add -ExpandVariables $false
if (-not $added.Changed -or $added.Value -cne "C:\Tools;;$target") { throw 'Trailing empty PATH entry was not preserved during add.' }
$removed = Resolve-UserPathUpdate -Current $added.Value -Expected $target -RequestedAction Remove -ExpandVariables $false
if (-not $removed.Changed -or $removed.Value -cne 'C:\Tools;') { throw 'Literal duplicate cleanup did not converge PATH.' }
Assert-Update -Current $target -Action Add -Changed $false -Present $true -Value $target
Assert-Update -Current ('"' + $target.ToUpperInvariant() + '"') -Action Add -Changed $false -Present $true -Value ('"' + $target.ToUpperInvariant() + '"')
Assert-Update -Current "$target;$target" -Action Remove -Changed $true -Present $false -Value ''
$upperEquivalent = $target.ToUpperInvariant()
Assert-Update -Current "$upperEquivalent;$target" -Action Remove -Changed $true -Present $false -Value ''
Assert-Update -Current "$target;$upperEquivalent" -Action Remove -Changed $true -Present $false -Value ''
$equivalent = '"' + $target.ToUpperInvariant() + '"'
Assert-Update -Current "$equivalent;$target" -Action Remove -Changed $true -Present $false -Value ''
Assert-Update -Current "$target;$equivalent" -Action Remove -Changed $true -Present $false -Value ''
$slashEquivalent = $target.Replace('\', '/') + '/'
Assert-Update -Current "$target;$slashEquivalent" -Action Remove -Changed $true -Present $false -Value ''
Assert-Update -Current 'C:\Tools' -Action Remove -Changed $false -Present $false -Value 'C:\Tools'
$expandedTarget = Join-Path $env:LOCALAPPDATA 'Programs\Herdr Sandbox'
$target = $expandedTarget
Assert-Update -Current '%%LOCALAPPDATA%%\Programs\Herdr Sandbox' -Action Add -Changed $false -Present $true -Value '%%LOCALAPPDATA%%\Programs\Herdr Sandbox' -ExpandVariables $true
Assert-Update -Current '%%LOCALAPPDATA%%\Programs\Herdr Sandbox' -Action Remove -Changed $false -Present $true -Value '%%LOCALAPPDATA%%\Programs\Herdr Sandbox' -ExpandVariables $true
Assert-Update -Current '%%LOCALAPPDATA%%\Programs\Herdr Sandbox' -Action Add -Changed $true -Present $true -Value "%%LOCALAPPDATA%%\Programs\Herdr Sandbox;$expandedTarget" -ExpandVariables $false
$target = 'C:\Users\Example%%Profile\AppData\Local\Programs\Herdr Sandbox'
Assert-Update -Current $target -Action Remove -Changed $true -Present $false -Value ''
$composed = 'C:\Temp\Caf' + [char]0x00e9
$decomposed = 'C:\Temp\Cafe' + [char]0x0301
if (Test-OwnedPathEntry -Entry $decomposed -Expected $composed) { throw 'Unicode-normalized text was incorrectly treated as the same literal path.' }
$unicodeRemoved = Resolve-UserPathUpdate -Current ($composed + ';' + $decomposed) -Expected $composed -RequestedAction Remove -ExpandVariables $false
if (-not $unicodeRemoved.Changed -or -not [string]::Equals([string]$unicodeRemoved.Value, $decomposed, [StringComparison]::Ordinal)) {
    throw 'Literal PATH convergence did not preserve the differently represented Unicode entry.'
}
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

func TestInstallerRecognizesOnlyExactPublishedV0010MarkerGrammar(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "packaging", "windows", "installer.nsi"))
	if err != nil {
		t.Fatal(err)
	}
	source := string(data)
	for _, want := range []string{
		`"schemaVersion":  1,`,
		`"installedVersion":  "0.0.10",`,
		`"name":  "${APP_REPLACED_EXECUTABLE}",`,
		`${If} $MarkerLegacyState == 39`,
		`Function CheckLegacyCanonicalGUID`,
		`Function CheckLegacyLowerHex64`,
		`Function CheckLegacyPositiveDecimal`,
		`Function CheckLegacyRegistration`,
		`${If} $MarkerLineCount != 1`,
		`${AndIf} $MarkerLegacyRegistrationValid != "1"`,
		`Goto marker_invalid`,
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("v0.0.10 marker parser is missing %q", want)
		}
	}
	for _, forbidden := range []string{`"installerSchema":  ${APP_INSTALLER_SCHEMA}`, `MarkerLegacyGuidFound`, `MarkerLegacySchemaFound`} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("v0.0.10 marker parser retains inexact compatibility %q", forbidden)
		}
	}
	checkMarkerIndex := strings.Index(source, `Call CheckOwnershipMarker`)
	checkLegacyRegistrationIndex := strings.Index(source, `Call CheckLegacyRegistration`)
	readProductGUIDIndex := strings.Index(source, `ReadRegStr $0 HKCU "${UNINSTALL_KEY}" "ProductGuid"`)
	if checkMarkerIndex < 0 || checkLegacyRegistrationIndex <= checkMarkerIndex || readProductGUIDIndex <= checkLegacyRegistrationIndex {
		t.Fatal("exact legacy registration must be checked immediately after marker parsing, even when ProductGuid is absent")
	}
}

func TestQuietUninstallWrapperOwnsPrivateTemporaryCopyAndExitCode(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "packaging", "windows", "quiet-uninstall.ps1"))
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
	helper, err := filepath.Abs(filepath.Join("..", "..", "packaging", "windows", "quiet-uninstall.ps1"))
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
