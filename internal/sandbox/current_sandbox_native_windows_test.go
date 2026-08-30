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
	basePath := defaultProvisioningPath(t, baseProvisioningName)
	functionSetup := provisioningPowerShellFunctionSetup(t,
		provisioningPowerShellFunctionSource{
			path: basePath,
			names: []string{
				"Assert-ProvisioningPathWithinRoot",
				"Assert-ProvisioningVisualStudioCachePath",
			},
		},
		provisioningPowerShellFunctionSource{path: stackPath, names: []string{
			"Get-StackVisualStudioTargetFromChannel",
			"ConvertFrom-StackVisualStudioLayoutDescriptor",
			"ConvertFrom-StackJavaReleaseVersion",
			"ConvertFrom-StackAndroidCLIVersion",
			"Get-StackVisualStudioRequiredArtifacts",
		}},
	)
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

$androidCLI = 'C:\HerdrSandbox\tools\android-sdk\cmdline-tools\latest\bin\android.exe'
$androidVersion = 'not installed'
if (Test-Path -LiteralPath $androidCLI -PathType Leaf) {
    if ([string]::IsNullOrWhiteSpace($javaRoot)) {
        $androidVersion = 'deferred until Java installation'
    } else {
        $previousJavaHome = [string]$env:JAVA_HOME
        $previousErrorActionPreference = $ErrorActionPreference
        try {
            $env:JAVA_HOME = $javaRoot
            $ErrorActionPreference = 'Continue'
            $androidOutput = @(& $androidCLI --no-metrics --version 2>&1 | ForEach-Object { [string]$_ })
            $androidExitCode = $LASTEXITCODE
        } finally {
            $ErrorActionPreference = $previousErrorActionPreference
            $env:JAVA_HOME = $previousJavaHome
        }
        if ($androidExitCode -ne 0) { throw "Current-Sandbox Android CLI exited with code $androidExitCode." }
        $androidVersion = ConvertFrom-StackAndroidCLIVersion -Output $androidOutput
        if ([string]::IsNullOrWhiteSpace($androidVersion)) {
            $androidVersion = 'unverified'
            Write-Warning 'Current-Sandbox Android CLI version output was not recognized; preflight will continue.'
        }
    }
}

$validSlots = @()
$invalidSlots = @()
$invalidDetails = @()
foreach ($slot in @('C:\HerdrSandbox\visual-studio-cache\vsbt\a', 'C:\HerdrSandbox\visual-studio-cache\vsbt\b')) {
    $descriptorPath = Join-Path $slot 'complete.json'
    if (-not (Test-Path -LiteralPath $descriptorPath -PathType Leaf)) { continue }
    try {
        Assert-ProvisioningVisualStudioCachePath -Path $slot
        Assert-ProvisioningVisualStudioCachePath -Path $descriptorPath
        $descriptor = [IO.File]::ReadAllText($descriptorPath) | ConvertFrom-Json
        $target = ConvertFrom-StackVisualStudioLayoutDescriptor -Descriptor $descriptor -Slot $slot
        if (-not [bool]$target.CurrentMetadata) {
            Write-Warning "Current-Sandbox Visual Studio slot uses verified cached metadata; preflight will continue: $slot"
        }
        $layout = Join-Path $slot 'layout'
        foreach ($relativePath in @(Get-StackVisualStudioRequiredArtifacts)) {
            $artifactPath = Join-Path $layout $relativePath
            if (-not (Test-Path -LiteralPath $artifactPath -PathType Leaf)) {
                throw "Visual Studio layout artifact is missing: $relativePath"
            }
            Assert-ProvisioningVisualStudioCachePath -Path $artifactPath
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
		"herdr-sandbox-native-audio": {"audio"},
		"herdr-sandbox-native-core": {
			"dotnet", "android", "go", "hyperframes", "cpp", "java", "nsis", "nushell", "playwright-cli", "tradingview",
		},
		"herdr-sandbox-native-handy":     {"handy"},
		"herdr-sandbox-native-herdr":     {"herdr"},
		"herdr-sandbox-native-python-ai": {"python-ai"},
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
	herdr := filepath.Join(workspacesRoot, "herdr-sandbox-native-herdr")
	handy := filepath.Join(workspacesRoot, "herdr-sandbox-native-handy")
	if err := writeCurrentSandboxFixtureFiles(herdr, map[string]string{
		"Cargo.toml":          "[package]\nname = \"herdr-native-current\"\nversion = \"0.0.0\"\nedition = \"2021\"\n",
		"rust-toolchain.toml": "[toolchain]\nchannel = \"1.96.1\"\ncomponents = [\"clippy\", \"rustfmt\"]\n",
	}); err != nil {
		t.Fatal(err)
	}
	if err := writeCurrentSandboxFixtureFiles(handy, map[string]string{
		"package.json":         "{\"name\":\"handy-app\",\"private\":true,\"version\":\"0.0.0\"}\n",
		"bun.lock":             "# current-Sandbox Handy fixture\n",
		"src-tauri/Cargo.toml": "[package]\nname = \"handy\"\nversion = \"0.0.0\"\nedition = \"2021\"\n",
		"src-tauri/resources/models/silero_vad_v4.onnx": "current-Sandbox model fixture\n",
	}); err != nil {
		t.Fatal(err)
	}
	for name, stacks := range workspaces {
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

func currentSandboxProvisioningEnvironment(status, restartID string) []string {
	filtered := make([]string, 0, len(os.Environ())+3)
	for _, entry := range os.Environ() {
		name, _, found := strings.Cut(entry, "=")
		if found && (strings.EqualFold(name, "PSModulePath") ||
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
