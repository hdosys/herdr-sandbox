package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"unicode/utf16"
)

func encodeNativePowerShell(script string) string {
	codeUnits := utf16.Encode([]rune(script))
	bytes := make([]byte, len(codeUnits)*2)
	for index, value := range codeUnits {
		bytes[index*2] = byte(value)
		bytes[index*2+1] = byte(value >> 8)
	}
	return base64.StdEncoding.EncodeToString(bytes)
}

func TestWriteNativeAcceptanceEvidenceIsCommitKeyedAndComplete(t *testing.T) {
	const commit = "0123456789abcdef0123456789abcdef01234567"
	var output bytes.Buffer
	if err := writeNativeAcceptanceEvidence(&output, commit); err != nil {
		t.Fatal(err)
	}
	var evidence nativeAcceptanceEvidence
	if err := json.NewDecoder(&output).Decode(&evidence); err != nil {
		t.Fatal(err)
	}
	if evidence.Kind != "native-acceptance" || evidence.Commit != commit ||
		evidence.Platform != runtime.GOOS+"/"+runtime.GOARCH ||
		evidence.Command != "go run ./cmd/task native-all-stacks" ||
		evidence.Result != "passed" || !slices.Equal(evidence.Boundaries, nativeAcceptanceBoundaries) {
		t.Fatalf("native evidence = %#v", evidence)
	}
}

func TestPrepareNativeAllStacksFixtureIsCredentialFreeAndComplete(t *testing.T) {
	fixture, err := prepareNativeAllStacksFixture(filepath.Join(t.TempDir(), "native"))
	if err != nil {
		t.Fatal(err)
	}
	profile, err := os.ReadFile(filepath.Join(fixture.Project, ".herdr-sandbox", "provision.ps1"))
	if err != nil {
		t.Fatal(err)
	}
	for _, call := range []string{"Install-AndroidStack", "Install-CppStack", "Install-DotNetStack", "Install-GoStack", "Install-HerdrStack", "Install-HyperFramesStack", "Install-JavaStack", "Install-NSISStack", "Install-NushellStack", "Install-Uv", "Install-NodeStack", "Install-PlaywrightCLIStack", "Install-TradingViewStack"} {
		if !strings.Contains(string(profile), call) {
			t.Fatalf("native fixture profile is missing %s", call)
		}
	}
	for _, duplicate := range []string{"Install-PythonStack", "Install-RustMSVCStack", "Install-ZigStack"} {
		if strings.Contains(string(profile), duplicate) {
			t.Fatalf("native fixture bypasses Herdr virtual composition with %s", duplicate)
		}
	}
	handyProfile, err := os.ReadFile(filepath.Join(fixture.HandyProject, ".herdr-sandbox", "provision.ps1"))
	if err != nil || !strings.Contains(string(handyProfile), "Install-HandyStack -ProjectDirectory $ProjectDirectory") {
		t.Fatalf("native Handy profile = %q, %v", handyProfile, err)
	}
	for _, duplicate := range []string{"Install-BunStack", "Install-RustMSVCStack", "Kitware.CMake", "KhronosGroup.VulkanSDK"} {
		if strings.Contains(string(handyProfile), duplicate) {
			t.Fatalf("native fixture bypasses Handy virtual composition with %s", duplicate)
		}
	}
	for _, source := range []string{"Cargo.toml", "rust-toolchain.toml", "go.mod", "main.go", "main_test.go", "smoke.csproj", "Program.cs", "smoke.c", "smoke.cpp", "Smoke.java", "smoke.js", "smoke.py", "smoke.rs", "smoke.zig", "justfile"} {
		if info, err := os.Stat(filepath.Join(fixture.Project, source)); err != nil || !info.Mode().IsRegular() || info.Size() == 0 {
			t.Fatalf("native fixture source %s is invalid: %v, %v", source, info, err)
		}
	}
	for _, source := range []string{"package.json", "bun.lock", filepath.Join("src-tauri", "Cargo.toml"), filepath.Join("src-tauri", "resources", "models", "silero_vad_v4.onnx")} {
		if info, err := os.Stat(filepath.Join(fixture.HandyProject, source)); err != nil || !info.Mode().IsRegular() || info.Size() == 0 {
			t.Fatalf("native Handy fixture source %s is invalid: %v, %v", source, info, err)
		}
	}
	configurationData, err := os.ReadFile(filepath.Join(fixture.AppData, "herdr-sandbox", "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	var configuration struct {
		Mounts map[string]struct {
			Path     string `json:"path"`
			ReadOnly bool   `json:"readOnly"`
		} `json:"mounts"`
	}
	if err := json.Unmarshal(configurationData, &configuration); err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{"opensrc-version", "opensrc-path", "vercel-labs/opensrc@v0.7.3", `C:\HerdrSandbox\cache\opensrc`, "pinned native CLI and isolated source fetch OK", "android-cli-version", "android-jdk-version", "android-sdk-location", "android-platform-tools", "Remove-AndroidJVMWarning", "UseAllWindowsProcessorGroups flag is not supported on this Windows version and will be ignored.", `platform-tools\source.properties`, "adb-version", "pair HOST[:PORT]", "connect HOST[:PORT]", "ANDROID_JAVA_HOME", `C:\HerdrSandbox\tools\android-sdk`, `C:\HerdrSandbox\toolchains\android-jdk-17`, "c-compile", "c-link", "c-run", "native-c-ok", "cpp-compile", "cpp-link", "cpp-run", "native-cpp-ok", "'/Z7'", "'/c'", "'/NOLOGO','/DEBUG:NONE'", "msbuild-version", `C:\HerdrSandbox\toolchains\visual-studio`, "java-version", "javac-version", "java-compile", "java-run", "native-java-ok", "JAVA_HOME", "nsis-version", "nsis-compile", "Native NSIS Smoke", "installer compile OK", "nushell-version", `Join-Path $env:ProgramFiles 'nu\bin\nu.exe'`, "machine MSI command and version OK", "Python commands resolved from unexpected paths", "playwright-chromium", "playwright-cli-version", "mmlmfjhmonkocbjadbfplnigmagldckm", `C:\HerdrSandbox\tools\playwright`, "PLAYWRIGHT_BROWSERS_PATH", "tvcontrol-help", "$tvBin -ceq $tvControlBin", "TVControl commands resolved from unexpected paths", "TradingView.Desktop", `C:\HerdrSandbox\tools\TradingView.TradingViewDesktop`, `C:\HerdrSandbox\tools\tvcontrol`, "active-session-launch.ps1", "TVControl active-session launch wiring is invalid", "portable signed-MSIX payload", "launch intentionally skipped", "bun-run", "python3-version", "uv-version", "uv-cache-dir", "uv-sync", "uv-run", "python-ai-smoke-ok", `C:\HerdrSandbox\cache\uv`, "UV_NO_MANAGED_PYTHON", "herdr-just-toolchain", "python3-just-ok", "bun-just-ok", "cargo-nextest-version", "just-version", "sh-run", "LIBGHOSTTY_VT_ZIG_OUT_DIR", "handy-cmake-version", "handy-glslc-version", `C:\VulkanSDK\1.4.309.0`, "SPIRV-HeadersConfig.cmake", "Microsoft Edge WebView2 Runtime", "handy-native-toolchain"} {
		if !strings.Contains(nativeAllStacksSmokeScript, required) {
			t.Fatalf("native smoke does not verify %s", required)
		}
	}
	if strings.Contains(nativeAllStacksSmokeScript, "Get-AppxPackage -Name 'TradingView.Desktop'") {
		t.Fatal("native TradingView smoke retains the rejected AppX registration path")
	}
	if strings.Contains(nativeAllStacksSmokeScript, "tvcontrol-alias-help") {
		t.Fatal("native TradingView smoke invokes the long-lived TVControl MCP server as help")
	}
	if strings.Contains(nativeAllStacksSmokeScript, "'sdk','list','platform-tools'") {
		t.Fatal("native Android smoke retains the crash-prone package listing path")
	}
	if !strings.Contains(nativeAllStacksSmokeScript, `@('-e',"console.log('bun-smoke-ok')")`) ||
		strings.Contains(nativeAllStacksSmokeScript, `@('-e','console.log("bun-smoke-ok")')`) {
		t.Fatal("native Bun smoke does not preserve its JavaScript string through Windows PowerShell")
	}
	if len(configuration.Mounts) != 2 || configuration.Mounts["reference"].Path != fixture.ReadOnlyMount ||
		!configuration.Mounts["reference"].ReadOnly || configuration.Mounts["worktrees"].Path != fixture.WritableMount ||
		configuration.Mounts["worktrees"].ReadOnly {
		t.Fatalf("native folder mounts = %#v", configuration.Mounts)
	}
	var fullConfiguration struct {
		Workspaces map[string]string `json:"workspaces"`
	}
	if err := json.Unmarshal(configurationData, &fullConfiguration); err != nil || fullConfiguration.Workspaces["handy"] != fixture.HandyProject {
		t.Fatalf("native Handy workspace = %#v, %v", fullConfiguration.Workspaces, err)
	}
	for _, path := range []string{
		filepath.Join(fixture.ReadOnlyMount, "host-reference.txt"),
		filepath.Join(fixture.ReadOnlyMount, "native-all-stacks-smoke.ps1"),
		filepath.Join(fixture.WritableMount, "host-worktrees.txt"),
	} {
		if info, err := os.Stat(path); err != nil || !info.Mode().IsRegular() || info.Size() == 0 {
			t.Fatalf("native folder-mount fixture %s is invalid: %v, %v", path, info, err)
		}
	}
	smokeScript, err := os.ReadFile(filepath.Join(fixture.ReadOnlyMount, "native-all-stacks-smoke.ps1"))
	if err != nil || string(smokeScript) != nativeAllStacksSmokeScript {
		t.Fatalf("native smoke fixture script does not match its repository owner: %v", err)
	}
	for _, absent := range []string{
		filepath.Join(fixture.AppData, "GitHub CLI", "hosts.yml"),
		filepath.Join(fixture.AppData, "herdr", "config.toml"),
	} {
		if _, err := os.Stat(absent); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("credential-free fixture contains %s: %v", absent, err)
		}
	}
	terminal, err := os.ReadFile(filepath.Join(fixture.LocalAppData, "Packages", "Microsoft.WindowsTerminal_8wekyb3d8bbwe", "LocalState", "settings.json"))
	if err != nil || !strings.Contains(string(terminal), `"theme": "light"`) || !strings.Contains(string(terminal), `"colorScheme": "Herdr Native Light"`) {
		t.Fatalf("native Terminal fixture = %q, %v", terminal, err)
	}
}

func TestNativeAllStacksSmokeCommandUsesReadOnlyFixtureScript(t *testing.T) {
	sshConfig := `C:\Runs\native\.ssh\config`
	command := nativeAllStacksSmokeCommand(context.Background(), "ssh.exe", sshConfig)
	want := strings.Join([]string{
		"-T", "-F", sshConfig, "sandbox",
		"powershell.exe", "-NoLogo", "-NoProfile", "-NonInteractive", "-WindowStyle", "Hidden",
		"-ExecutionPolicy", "Bypass", "-File", nativeAllStacksSmokeGuestPath,
	}, "\x00")
	if got := strings.Join(command.Args[1:], "\x00"); got != want {
		t.Fatalf("native smoke SSH arguments = %q, want %q", got, want)
	}
	if command.Stdin != nil || strings.Contains(want, "-EncodedCommand") || strings.Contains(want, nativeAllStacksSmokeScript) {
		t.Fatal("native smoke payload remains on the SSH command line or standard input")
	}
}

func TestVerifyNativeAllStacksMountsRequiresBlockedAndPersistedWrites(t *testing.T) {
	fixture := nativeAllStacksFixture{ReadOnlyMount: filepath.Join(t.TempDir(), "reference"), WritableMount: filepath.Join(t.TempDir(), "worktrees")}
	if err := os.MkdirAll(fixture.ReadOnlyMount, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(fixture.WritableMount, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fixture.WritableMount, "guest-write.txt"), []byte("guest-write-ok\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := verifyNativeAllStacksMounts(fixture); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fixture.ReadOnlyMount, "guest-write-blocked.txt"), []byte("unexpected"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := verifyNativeAllStacksMounts(fixture); err == nil {
		t.Fatal("accepted a write through the read-only folder mount")
	}
}

func TestNativeAllStacksSmokeScriptParsesWithWindowsPowerShell(t *testing.T) {
	requireExternalBoundaryTest(t, "Windows PowerShell native smoke parsing")
	if runtime.GOOS != "windows" {
		t.Skip("Windows PowerShell 5.1 parser boundary")
	}
	parser := `$tokens = $null
$errors = $null
[System.Management.Automation.Language.Parser]::ParseInput($env:HERDR_SANDBOX_NATIVE_SCRIPT, [ref]$tokens, [ref]$errors) | Out-Null
if ($errors.Count -gt 0) {
    $errors | ForEach-Object { [Console]::Error.WriteLine($_.Message) }
    exit 1
}`
	command := hiddenCommandContext(context.Background(), "powershell.exe", "-NoLogo", "-NoProfile", "-NonInteractive", "-EncodedCommand", encodeNativePowerShell(parser))
	command.Env = append(os.Environ(), "HERDR_SANDBOX_NATIVE_SCRIPT="+nativeAllStacksSmokeScript)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("parse native all-stack smoke script: %v: %s", err, output)
	}
}
