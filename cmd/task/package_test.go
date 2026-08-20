package main

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
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

func TestWriteReleaseArtifactEvidenceIsStructuredAndComplete(t *testing.T) {
	root := t.TempDir()
	paths := releasePackagePaths{
		ZIP:       filepath.Join(root, "release.zip"),
		Installer: filepath.Join(root, "release_setup.exe"),
	}
	contents := [][]byte{[]byte("zip fixture"), []byte("installer fixture")}
	for index, path := range releaseOutputPaths(paths) {
		if err := os.WriteFile(path, contents[index], 0o600); err != nil {
			t.Fatal(err)
		}
	}

	var output bytes.Buffer
	if err := writeReleaseArtifactEvidence(&output, paths); err != nil {
		t.Fatal(err)
	}
	decoder := json.NewDecoder(&output)
	for index, path := range releaseOutputPaths(paths) {
		var evidence releaseArtifactEvidence
		if err := decoder.Decode(&evidence); err != nil {
			t.Fatalf("decode artifact evidence %d: %v", index, err)
		}
		wantHash := fmt.Sprintf("%x", sha256.Sum256(contents[index]))
		if evidence.Kind != "candidate-artifact" || evidence.Path != filepath.Clean(path) ||
			evidence.Bytes != int64(len(contents[index])) || evidence.SHA256 != wantHash ||
			evidence.SHA256 != strings.ToLower(evidence.SHA256) {
			t.Fatalf("artifact evidence %d = %#v", index, evidence)
		}
	}
	var extra releaseArtifactEvidence
	if err := decoder.Decode(&extra); err != io.EOF {
		t.Fatalf("unexpected trailing artifact evidence: %#v, %v", extra, err)
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
		`--installer-open-configuration`,
		`Run ${APP_NAME} config from a new terminal.`,
		`!insertmacro MUI_PAGE_LICENSE "${PACKAGE_DIR}\${APP_LICENSE}"`,
		`!insertmacro MUI_PAGE_FINISH`,
		`UninstPage custom un.DeleteConfigurationPage un.DeleteConfigurationPageLeave`,
		`Also delete ${APP_CONFIG_FILE} and ${APP_USER_SCRIPT}`,
		`A running Sandbox stays open but becomes unmanaged`,
		`--installer-seed-configuration`,
		`--installer-stop-processes`,
		`--installer-clean-uninstall`,
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
		`nsExec::ExecToStack /TIMEOUT=7000 '"$INSTDIR\${APP_EXECUTABLE}" --installer-stop-processes'`,
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
	requireExternalBoundaryTest(t, "Windows PowerShell installer PATH integration")
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

func TestQuietUninstallWrapperTerminatesOwnedProcessTreeInWindowsPowerShell51(t *testing.T) {
	requireExternalBoundaryTest(t, "Windows PowerShell quiet-uninstall integration")
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
