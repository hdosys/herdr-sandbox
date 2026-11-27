//go:build windows

package sandbox

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const (
	currentSandboxNativeEnvironment    = "HERDR_SANDBOX_CURRENT_NATIVE"
	currentSandboxStackEnvironment     = "HERDR_SANDBOX_CURRENT_NATIVE_STACK"
	currentSandboxPayloadEnvironment   = "HERDR_SANDBOX_CURRENT_NATIVE_PAYLOAD"
	currentSandboxModelsEnvironment    = "HERDR_SANDBOX_CURRENT_NATIVE_MODELS"
	currentSandboxPreflightEnvironment = "HERDR_SANDBOX_CURRENT_PREFLIGHT"
	currentSandboxFixtureMarker        = "herdr-sandbox current native fixture v1\n"
)

func TestCurrentSandboxProvisioningPreflight(t *testing.T) {
	if os.Getenv(currentSandboxPreflightEnvironment) != "1" {
		t.Skip("explicit current-Sandbox provisioning preflight")
	}
	if !strings.EqualFold(os.Getenv("USERNAME"), "WDAGUtilityAccount") {
		t.Fatalf("current-Sandbox preflight user = %q", os.Getenv("USERNAME"))
	}
	stackPath := defaultProvisioningPath(t, stackProvisioningName)
	functionSetup := provisioningPowerShellFunctionSetup(t, provisioningPowerShellFunctionSource{
		path: stackPath,
		names: []string{
			"Get-StackVisualStudioTargetFromChannel",
			"ConvertFrom-StackVisualStudioLayoutDescriptor",
			"ConvertFrom-StackJavaReleaseVersion",
			"Get-StackVisualStudioRequiredArtifacts",
		},
	})
	script := fmt.Sprintf(`$ErrorActionPreference = 'Stop'
trap { Write-Output ($_ | Out-String); exit 1 }
%s
$javaRoot = [string][Environment]::GetEnvironmentVariable('JAVA_HOME', 'Machine')
$javaVersion = 'not installed'
if (-not [string]::IsNullOrWhiteSpace($javaRoot)) {
    $javaReleasePath = Join-Path $javaRoot 'release'
    if (-not (Test-Path -LiteralPath $javaReleasePath -PathType Leaf)) { throw "Current-Sandbox Java release is missing: $javaReleasePath" }
    $javaVersion = ConvertFrom-StackJavaReleaseVersion -ReleaseText ([IO.File]::ReadAllText($javaReleasePath))
    if ([string]::IsNullOrWhiteSpace($javaVersion)) {
        $javaVersion = 'unverified'
        Write-Warning 'Current-Sandbox Java version output was not recognized; preflight will continue.'
    }
}

$androidSource = 'C:\HerdrSandbox\tools\android-sdk\cmdline-tools\latest\source.properties'
$androidVersion = 'not installed'
if (Test-Path -LiteralPath $androidSource -PathType Leaf) {
    $androidProperties = [IO.File]::ReadAllText($androidSource)
    if ($androidProperties -match '(?m)^Pkg\.Revision=(?<version>\d+\.\d+)\r?$') {
        $androidVersion = [string]$Matches['version']
    } else {
        $androidVersion = 'unverified'
        Write-Warning 'Current-Sandbox Android Command-line Tools version was not recognized; preflight will continue.'
    }
}

$validSlots = @()
$invalidSlots = @()
$invalidDetails = @()
foreach ($slot in @('C:\HerdrSandbox\cache\vsbt\a', 'C:\HerdrSandbox\cache\vsbt\b')) {
    $descriptorPath = Join-Path $slot 'complete.json'
    if (-not (Test-Path -LiteralPath $descriptorPath -PathType Leaf)) { continue }
    try {
        $descriptor = [IO.File]::ReadAllText($descriptorPath) | ConvertFrom-Json
        $target = ConvertFrom-StackVisualStudioLayoutDescriptor -Descriptor $descriptor -Slot $slot
        if (-not [bool]$target.CurrentMetadata) {
            Write-Warning "Current-Sandbox Visual Studio slot uses verified cached metadata; preflight will continue: $slot"
        }
        $layout = Join-Path $slot 'layout'
        foreach ($relativePath in @(Get-StackVisualStudioRequiredArtifacts)) {
            if (-not (Test-Path -LiteralPath (Join-Path $layout $relativePath) -PathType Leaf)) {
                throw "Visual Studio layout artifact is missing: $relativePath"
            }
        }
        $validSlots += $slot
    } catch {
        $invalidSlots += $slot
        $invalidDetails += "${slot}: $($_.Exception.Message)"
    }
}
if ($validSlots.Count -eq 0) {
    throw "Current-Sandbox preflight requires a verified host-prepared Visual Studio slot; valid=$($validSlots.Count), invalid=$($invalidSlots.Count): $($invalidDetails -join '; ')"
}
Write-Output "Current-Sandbox provisioning inputs passed: Java $javaVersion, Android $androidVersion, Visual Studio slot $($validSlots[0])."
`, functionSetup)
	scriptPath := filepath.Join(t.TempDir(), "current-sandbox-preflight.ps1")
	if err := os.WriteFile(scriptPath, []byte(script), 0o600); err != nil {
		t.Fatal(err)
	}
	command := hiddenCommand(mustWindowsPowerShellPath(t), "-NoLogo", "-NoProfile", "-NonInteractive", "-WindowStyle", "Hidden", "-ExecutionPolicy", "Bypass", "-File", scriptPath)
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	if err := command.Run(); err != nil {
		t.Fatalf("current-Sandbox provisioning preflight: %v", err)
	}
}

func TestCurrentSandboxProvisioning(t *testing.T) {
	if os.Getenv(currentSandboxNativeEnvironment) != "1" {
		t.Skip("explicit current-Sandbox native provisioning gate")
	}
	if !strings.EqualFold(os.Getenv("USERNAME"), "WDAGUtilityAccount") {
		t.Fatalf("current-Sandbox provisioning user = %q", os.Getenv("USERNAME"))
	}

	const workspacesRoot = `C:\Workspaces`
	workspaces := map[string][]string{
		"herdr-sandbox-native-core":  {"all"},
		"herdr-sandbox-native-handy": {"handy"},
		"herdr-sandbox-native-herdr": {"herdr"},
	}
	for name := range workspaces {
		path := filepath.Join(workspacesRoot, name)
		if err := resetCurrentSandboxFixture(path); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := removeCurrentSandboxFixture(path); err != nil {
				t.Errorf("clean current-Sandbox fixture %s: %v", path, err)
			}
		})
	}

	core := filepath.Join(workspacesRoot, "herdr-sandbox-native-core")
	for name, stacks := range workspaces {
		if err := prepareCurrentSandboxProject(filepath.Join(workspacesRoot, name), stacks[0]); err != nil {
			t.Fatal(err)
		}
		if _, err := InitializeProject(filepath.Join(workspacesRoot, name), stacks); err != nil {
			t.Fatalf("initialize current-Sandbox profile %s: %v", name, err)
		}
	}

	workingRoot := t.TempDir()
	globalRoot := filepath.Join(workingRoot, "config")
	if err := os.MkdirAll(globalRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	configured := make(map[string]string, len(workspaces))
	for name := range workspaces {
		configured[name] = filepath.Join(workspacesRoot, name)
	}
	configurationValues := map[string]any{
		"workspaces": configured,
		"wingetPackages": map[string]any{
			"remove": []string{}, "add": []string{}, "versions": map[string]string{},
		},
	}
	if modelRoot := strings.TrimSpace(os.Getenv(currentSandboxModelsEnvironment)); modelRoot != "" {
		if !strings.EqualFold(filepath.Clean(modelRoot), filepath.Clean(guestModelsDirectory)) {
			t.Fatalf("current-Sandbox models must be available at %s", guestModelsDirectory)
		}
		configurationValues["modelsDirectory"] = modelRoot
	}
	configuration, err := json.Marshal(configurationValues)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(globalRoot, globalConfigurationName), append(configuration, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}

	payload := os.Getenv(currentSandboxPayloadEnvironment)
	if payload == "" {
		payload, err = filepath.Abs(filepath.Join("..", "..", "provisioning"))
		if err != nil {
			t.Fatal(err)
		}
	}
	plan, err := resolveProvisioningAt(core, globalRoot, payload)
	if err != nil {
		t.Fatalf("resolve current-Sandbox provisioning: %v", err)
	}
	if err := validateBaseProvisioningContract(plan.BaseScript); err != nil {
		t.Fatal(err)
	}
	if err := validateStackProvisioningContract(plan.StackScript); err != nil {
		t.Fatal(err)
	}
	plan.WindowsTerminal, err = detectHostWindowsTerminalConfiguration()
	if err != nil {
		t.Fatal(err)
	}
	plan.Packages, err = resolveWingetPackagePlan(wingetPackageConfiguration{
		Remove: []string{}, Add: []string{}, Versions: map[string]string{},
	}, plan.WindowsTerminal)
	if err != nil {
		t.Fatal(err)
	}

	inspection := filepath.Join(workingRoot, "inspection")
	if err := os.MkdirAll(inspection, 0o700); err != nil {
		t.Fatal(err)
	}
	snapshot, err := prepareProvisioningSnapshot(t.Context(), inspection, filepath.Join(workingRoot, "snapshot"), plan)
	if err != nil {
		t.Fatalf("prepare current-Sandbox provisioning snapshot: %v", err)
	}
	status := filepath.Join(workingRoot, "status")
	if err := os.MkdirAll(status, 0o700); err != nil {
		t.Fatal(err)
	}
	restartID := time.Now().UTC().Format("20060102-150405") + "-00000000"
	ctx, cancel := context.WithTimeout(t.Context(), 35*time.Minute)
	defer cancel()
	command := hiddenCommandContext(ctx, filepath.Join(os.Getenv("SystemRoot"), "System32", "WindowsPowerShell", "v1.0", "powershell.exe"),
		"-NoLogo", "-NoProfile", "-NonInteractive", "-WindowStyle", "Hidden", "-ExecutionPolicy", "Bypass",
		"-File", filepath.Join(snapshot.Directory, baseProvisioningName),
		"-Phase", "Development",
		"-ProjectProvisioningDirectory", snapshot.ProjectScriptsDirectory,
		"-WorkspacesDirectory", workspacesRoot,
		"-PackagePlanPath", snapshot.PackagePlanPath,
		"-UserProvisioningPath", filepath.Join(snapshot.Directory, userProvisioningName),
		"-ProcessOwnerPath", snapshot.ProcessOwnerPath)
	command.Dir = core
	command.Env = currentSandboxProvisioningEnvironment(status, restartID)
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	if err := command.Run(); err != nil {
		t.Fatalf("execute current-Sandbox provisioning: %v", err)
	}
}

func TestCurrentSandboxExplorerRestartPreservesDesktop(t *testing.T) {
	if os.Getenv(currentSandboxNativeEnvironment) != "1" {
		t.Skip("explicit current-Sandbox desktop lifecycle acceptance")
	}
	if !strings.EqualFold(os.Getenv("USERNAME"), "WDAGUtilityAccount") {
		t.Fatal("desktop lifecycle acceptance refuses host execution")
	}
	powerShell := mustWindowsPowerShellPath(t)
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	type shellState struct {
		PID          int    `json:"pid"`
		SessionID    int    `json:"sessionId"`
		StartedAtUTC string `json:"startedAtUTC"`
	}
	query := func() shellState {
		t.Helper()
		command := hiddenCommandContext(ctx, powerShell, "-NoLogo", "-NoProfile", "-NonInteractive", "-WindowStyle", "Hidden", "-ExecutionPolicy", "Bypass", "-File", filepath.Join("testdata", "native-explorer-shell-state.ps1"))
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("registered desktop: %v: %s", err, output)
		}
		var state shellState
		if err := json.Unmarshal(output, &state); err != nil || state.PID <= 0 || state.SessionID <= 0 {
			t.Fatalf("invalid shell evidence: %v: %s", err, output)
		}
		return state
	}
	before := query()
	setup := provisioningPowerShellFunctionSetup(t, provisioningPowerShellFunctionSource{
		path: defaultProvisioningPath(t, baseProvisioningName), names: []string{"Restart-ProvisioningExplorerShell"},
	})
	scriptPath := filepath.Join(t.TempDir(), "native-explorer-restart.ps1")
	if err := os.WriteFile(scriptPath, []byte("$ErrorActionPreference = 'Stop'\n"+setup+"\nRestart-ProvisioningExplorerShell -Role 'native desktop lifecycle acceptance'\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	command := hiddenCommandContext(ctx, powerShell, "-NoLogo", "-NoProfile", "-NonInteractive", "-WindowStyle", "Hidden", "-ExecutionPolicy", "Bypass", "-File", scriptPath)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("production Explorer restart: %v: %s", err, output)
	}
	// Command.Run has now closed the production Job Object and killed descendants.
	after := query()
	if before.PID == after.PID || before.StartedAtUTC == after.StartedAtUTC || before.SessionID != after.SessionID {
		t.Fatalf("desktop was not replaced in the same session: before=%+v after=%+v", before, after)
	}
	t.Logf("registered Explorer %d -> %d in session %d survived provisioning process-tree cleanup", before.PID, after.PID, after.SessionID)
}

func TestCurrentSandboxStackProvisioning(t *testing.T) {
	if os.Getenv(currentSandboxNativeEnvironment) != "1" || os.Getenv(currentSandboxStackEnvironment) == "" {
		t.Skip("explicit current-Sandbox stack provisioning gate")
	}
	if !strings.EqualFold(os.Getenv("USERNAME"), "WDAGUtilityAccount") {
		t.Fatal("stack provisioning must execute inside Windows Sandbox, not on the host")
	}
	selection := os.Getenv(currentSandboxStackEnvironment)
	_, labels, err := normalizeProjectInitStacks([]string{selection})
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	project := filepath.Join(root, "project")
	configuration := filepath.Join(root, "config")
	inspection := filepath.Join(root, "inspection")
	status := filepath.Join(root, "status")
	for _, path := range []string{project, configuration, inspection, status} {
		if err := os.Mkdir(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := prepareCurrentSandboxProject(project, labels[0]); err != nil {
		t.Fatal(err)
	}
	if _, err := InitializeProject(project, []string{selection}); err != nil {
		t.Fatal(err)
	}
	payload := filepath.Dir(defaultProvisioningPath(t, baseProvisioningName))
	plan, err := resolveProvisioningAt(project, configuration, payload)
	if err != nil {
		t.Fatal(err)
	}
	plan.WindowsTerminal, err = detectHostWindowsTerminalConfiguration()
	if err != nil {
		t.Fatal(err)
	}
	plan.Packages, err = resolveWingetPackagePlan(plan.PackageConfiguration, plan.WindowsTerminal)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := prepareProvisioningSnapshot(t.Context(), inspection, filepath.Join(root, "snapshot"), plan)
	if err != nil {
		t.Fatal(err)
	}
	runner, err := filepath.Abs(filepath.Join("..", "..", "cmd", "task", "assets", "current-sandbox-stack.ps1"))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 35*time.Minute)
	defer cancel()
	command := hiddenCommandContext(ctx, mustWindowsPowerShellPath(t),
		"-NoLogo", "-NoProfile", "-NonInteractive", "-WindowStyle", "Hidden", "-ExecutionPolicy", "Bypass",
		"-File", runner, "-SnapshotDirectory", snapshot.Directory,
		"-ProjectScript", filepath.Join(snapshot.ProjectScriptsDirectory, plan.Workspaces[0].Name+".ps1"),
		"-ProjectDirectory", project)
	command.Dir = project
	command.Env = currentSandboxProvisioningEnvironment(status, "")
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	if err := command.Run(); err != nil {
		t.Fatalf("real %s provisioning: %v", labels[0], err)
	}
}

func prepareCurrentSandboxProject(root, selection string) error {
	switch selection {
	case "herdr":
		return writeCurrentSandboxFixtureFiles(root, map[string]string{
			"Cargo.toml": "[package]\nname = \"herdr-native-current\"\nversion = \"0.0.0\"\nedition = \"2021\"\n",
		})
	case "handy":
		return writeCurrentSandboxFixtureFiles(root, map[string]string{
			"package.json":         "{\"name\":\"handy-app\",\"private\":true,\"version\":\"0.0.0\"}\n",
			"bun.lock":             "# current-Sandbox Handy fixture\n",
			"src-tauri/Cargo.toml": "[package]\nname = \"handy\"\nversion = \"0.0.0\"\nedition = \"2021\"\n",
			"src-tauri/resources/models/silero_vad_v4.onnx": "current-Sandbox model fixture\n",
		})
	}
	return nil
}

func currentSandboxProvisioningEnvironment(status, restartID string) []string {
	filtered := make([]string, 0, len(os.Environ())+3)
	for _, entry := range os.Environ() {
		name, _, found := strings.Cut(entry, "=")
		if found && (strings.EqualFold(name, "PSModulePath") ||
			strings.EqualFold(name, "HERDR_PROVISIONING_ROOT") ||
			strings.EqualFold(name, "HERDR_PROVISIONING_TARGET") ||
			strings.EqualFold(name, "HERDR_PROVISIONING_RUNNER") ||
			strings.EqualFold(name, "HERDR_SANDBOX_STATUS_DIRECTORY") ||
			strings.EqualFold(name, "HERDR_SANDBOX_EXPLORER_RESTART_ID") ||
			strings.EqualFold(name, "HERDR_SANDBOX_EXPLORER_RESTART_TASK_NAME") ||
			strings.EqualFold(name, "HERDR_SANDBOX_EXPLORER_RESTART_SCHEDULED")) {
			continue
		}
		filtered = append(filtered, entry)
	}
	return append(filtered,
		"HERDR_SANDBOX_STATUS_DIRECTORY="+status,
		"HERDR_SANDBOX_EXPLORER_RESTART_ID="+restartID,
		"HERDR_SANDBOX_EXPLORER_RESTART_TASK_NAME=HerdrSandbox-ExplorerRestart-"+restartID)
}

func resetCurrentSandboxFixture(path string) error {
	if err := removeCurrentSandboxFixture(path); err != nil {
		return err
	}
	if err := os.Mkdir(path, 0o700); err != nil {
		return fmt.Errorf("create current-Sandbox fixture %s: %w", path, err)
	}
	return os.WriteFile(filepath.Join(path, ".herdr-sandbox-native-current"), []byte(currentSandboxFixtureMarker), 0o600)
}

func removeCurrentSandboxFixture(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect current-Sandbox fixture %s: %w", path, err)
	}
	reparse, err := fileInfoIsReparsePoint(info)
	if err != nil || reparse || !info.IsDir() {
		return fmt.Errorf("refuse unsafe current-Sandbox fixture %s", path)
	}
	marker, err := os.ReadFile(filepath.Join(path, ".herdr-sandbox-native-current"))
	if err != nil || string(marker) != currentSandboxFixtureMarker {
		return fmt.Errorf("refuse unmarked current-Sandbox fixture %s", path)
	}
	if err := rejectMappedPathReparsePoints(path); err != nil {
		return fmt.Errorf("refuse reparse-backed current-Sandbox fixture %s: %w", path, err)
	}
	if err := os.RemoveAll(path); err != nil {
		return fmt.Errorf("remove current-Sandbox fixture %s: %w", path, err)
	}
	return nil
}

func writeCurrentSandboxFixtureFiles(root string, files map[string]string) error {
	for relative, contents := range files {
		path := filepath.Join(root, filepath.FromSlash(relative))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return err
		}
		if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
			return err
		}
	}
	return nil
}
