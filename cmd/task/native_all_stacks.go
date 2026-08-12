package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
	"unicode/utf16"

	"herdr-sandbox/internal/hiddenprocess"
	"herdr-sandbox/internal/productidentity"
)

const (
	nativeAllStacksMarkerContents = "herdr-sandbox native-all-stacks fixture v1\n"
	nativeAllStacksUpTimeout      = "90m"
	nativeAllStacksSmokeGuestPath = `C:\Mounts\reference\native-all-stacks-smoke.ps1`
)

func nativeAllStacks(ctx context.Context, stdout, stderr io.Writer) (resultErr error) {
	if runtime.GOOS != "windows" {
		return errors.New("native-all-stacks requires Windows and Windows Sandbox")
	}
	root, err := filepath.Abs(filepath.Join("build", "native-all-stacks"))
	if err != nil {
		return fmt.Errorf("resolve native all-stack fixture root: %w", err)
	}
	fixture, err := prepareNativeAllStacksFixture(root)
	if err != nil {
		return err
	}
	if err := build(ctx, stdout, stderr); err != nil {
		return fmt.Errorf("build stable CLI for native all-stack test: %w", err)
	}
	executable, err := filepath.Abs(filepath.Join("build", "bin", productidentity.ExecutableName))
	if err != nil {
		return fmt.Errorf("resolve stable CLI path: %w", err)
	}
	environment := nativeAllStacksEnvironment(fixture)

	if err := runNativeAllStacksCLI(ctx, fixture.Project, environment, stdout, stderr, executable, "down"); err != nil {
		return fmt.Errorf("stop a previous native all-stack fixture: %w", err)
	}
	if err := waitForNativeAllStacksCleanup(ctx, fixture.Project, environment, stdout, stderr, executable); err != nil {
		return err
	}
	if err := runNativeAllStacksCLI(ctx, fixture.Project, environment, stdout, stderr, executable, "plan"); err != nil {
		return fmt.Errorf("validate native all-stack plan: %w", err)
	}

	downNeeded := true
	defer func() {
		if !downNeeded {
			return
		}
		cleanupContext, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		downErr := runNativeAllStacksCLI(cleanupContext, fixture.Project, environment, stdout, stderr, executable, "down")
		if downErr != nil {
			resultErr = errors.Join(resultErr, fmt.Errorf("stop native all-stack Sandbox: %w", downErr))
		}
	}()

	if err := runNativeAllStacksCLI(ctx, fixture.Project, environment, stdout, stderr, executable, "up", "--no-attach", "--timeout", nativeAllStacksUpTimeout); err != nil {
		return fmt.Errorf("provision native all-stack Sandbox: %w", err)
	}
	if err := runNativeAllStacksSmoke(ctx, fixture, environment, stdout, stderr); err != nil {
		return err
	}
	if err := verifyNativeAllStacksMounts(fixture); err != nil {
		return err
	}
	downNeeded = false
	if err := runNativeAllStacksCLI(ctx, fixture.Project, environment, stdout, stderr, executable, "down"); err != nil {
		return fmt.Errorf("stop successful native all-stack Sandbox: %w", err)
	}
	if _, err := fmt.Fprintln(stdout, "Native all-stack test passed: folder mounts, C/C++, Java, NSIS, dotnet, go, Handy and Herdr virtual stacks, node with Playwright Chromium, Playwright CLI registration, Terminal, and Starship."); err != nil {
		return err
	}
	return nil
}

func waitForNativeAllStacksCleanup(ctx context.Context, directory string, environment []string, stdout, stderr io.Writer, executable string) error {
	cleanupContext, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	waitingReported := false
	for {
		var capturedOutput bytes.Buffer
		command := nativeAllStacksCLICommand(cleanupContext, executable, "clean")
		command.Dir = directory
		command.Env = environment
		command.Stdout = &capturedOutput
		command.Stderr = &capturedOutput
		err := command.Run()
		if err == nil {
			if _, writeErr := io.Copy(stdout, &capturedOutput); writeErr != nil {
				return writeErr
			}
			return nil
		}
		diagnostic := strings.ToLower(capturedOutput.String())
		if !strings.Contains(diagnostic, "being used by another process") {
			_, _ = io.Copy(stderr, &capturedOutput)
			return fmt.Errorf("clean previous native all-stack fixture: %w", err)
		}
		if !waitingReported {
			if _, writeErr := fmt.Fprintln(stdout, "Waiting for the previous Sandbox mapping to be released..."); writeErr != nil {
				return writeErr
			}
			waitingReported = true
		}
		timer := time.NewTimer(time.Second)
		select {
		case <-cleanupContext.Done():
			timer.Stop()
			_, _ = io.Copy(stderr, &capturedOutput)
			return fmt.Errorf("wait for previous native all-stack fixture cleanup: %w", cleanupContext.Err())
		case <-timer.C:
		}
	}
}

type nativeAllStacksFixture struct {
	Root          string
	Project       string
	HandyProject  string
	AppData       string
	LocalAppData  string
	UserProfile   string
	ReadOnlyMount string
	WritableMount string
}

func prepareNativeAllStacksFixture(root string) (nativeAllStacksFixture, error) {
	fixture := nativeAllStacksFixture{
		Root:          root,
		Project:       filepath.Join(root, "project"),
		HandyProject:  filepath.Join(root, "handy"),
		AppData:       filepath.Join(root, "appdata"),
		LocalAppData:  filepath.Join(root, "localappdata"),
		UserProfile:   filepath.Join(root, "userprofile"),
		ReadOnlyMount: filepath.Join(root, "mounts", "reference"),
		WritableMount: filepath.Join(root, "mounts", "worktrees"),
	}
	marker := filepath.Join(root, ".herdr-sandbox-native-all-stacks")
	if info, err := os.Lstat(root); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return nativeAllStacksFixture{}, fmt.Errorf("native all-stack fixture root is not a regular directory: %s", root)
		}
		contents, readErr := os.ReadFile(marker)
		if readErr != nil || string(contents) != nativeAllStacksMarkerContents {
			return nativeAllStacksFixture{}, fmt.Errorf("refusing unmarked native all-stack fixture root: %s", root)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nativeAllStacksFixture{}, fmt.Errorf("inspect native all-stack fixture root: %w", err)
	} else {
		if err := os.MkdirAll(root, 0o700); err != nil {
			return nativeAllStacksFixture{}, fmt.Errorf("create native all-stack fixture root: %w", err)
		}
		if err := os.WriteFile(marker, []byte(nativeAllStacksMarkerContents), 0o600); err != nil {
			return nativeAllStacksFixture{}, fmt.Errorf("mark native all-stack fixture root: %w", err)
		}
	}

	files := map[string]string{
		filepath.Join(fixture.AppData, "herdr-sandbox", "config.json"): fmt.Sprintf(`{
  "cacheDirectory": "",
  "memoryMB": 32768,
  "audio": false,
  "audioInput": false,
  "tailscale": false,
  "mounts": {
    "reference": {
      "path": %q,
      "readOnly": true
    },
    "worktrees": {
      "path": %q,
      "readOnly": false
    }
  },
  "codingAgentSync": {
    "opencode": false,
    "claudeCode": false,
    "codex": false,
    "githubCopilot": false,
    "pi": false
  },
  "wingetPackages": {
    "remove": [],
    "add": [],
    "versions": {}
  },
  "workspaceDiscovery": {
    "root": "",
    "exclude": []
  },
  "workspaces": {
    "handy": %q
  }
}
`, fixture.ReadOnlyMount, fixture.WritableMount, fixture.HandyProject),
		filepath.Join(fixture.ReadOnlyMount, "host-reference.txt"):          "read-only-mount-ok\n",
		filepath.Join(fixture.ReadOnlyMount, "native-all-stacks-smoke.ps1"): nativeAllStacksSmokeScript,
		filepath.Join(fixture.WritableMount, "host-worktrees.txt"):          "read-write-mount-ok\n",
		filepath.Join(fixture.AppData, "herdr-sandbox", "user.ps1"): `# herdr-sandbox-user-contract: 1
$ErrorActionPreference = 'Stop'
Set-StrictMode -Version 2.0

# Native all-stack verification uses no extra global guest customization.
`,
		filepath.Join(fixture.AppData, "GitHub CLI", "config.yml"): "git_protocol: https\nprompt: disabled\n",
		filepath.Join(fixture.UserProfile, ".gitconfig"):           "[init]\n\tdefaultBranch = main\n",
		filepath.Join(fixture.Project, ".herdr-sandbox", "provision.ps1"): `param(
    [Parameter(Mandatory = $true)]
    [string]$ProjectDirectory
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version 2.0

Install-DotNetStack
Install-GoStack -ProjectDirectory $ProjectDirectory
Install-HerdrStack -ProjectDirectory $ProjectDirectory
Install-CppStack
Install-JavaStack
Install-NSISStack
Install-Uv
Install-NodeStack
Install-PlaywrightCLIStack
Install-TradingViewStack
`,
		filepath.Join(fixture.Project, "Cargo.toml"): `[package]
name = "herdr-native-all-stacks"
version = "0.0.0"
edition = "2021"
`,
		filepath.Join(fixture.Project, "rust-toolchain.toml"): `[toolchain]
channel = "1.96.1"
components = ["clippy", "rustfmt"]
`,
		filepath.Join(fixture.Project, "go.mod"): `module example.com/herdr-native-all-stacks

go 1.22
`,
		filepath.Join(fixture.Project, "main.go"): `package main

func answer() int { return 42 }

func main() {}
`,
		filepath.Join(fixture.Project, "main_test.go"): `package main

import "testing"

func TestAnswer(t *testing.T) {
	if answer() != 42 {
		t.Fatal("unexpected answer")
	}
}
`,
		filepath.Join(fixture.Project, "smoke.csproj"): `<Project Sdk="Microsoft.NET.Sdk">
  <PropertyGroup>
    <OutputType>Exe</OutputType>
    <TargetFramework>net10.0</TargetFramework>
    <ImplicitUsings>enable</ImplicitUsings>
    <Nullable>enable</Nullable>
  </PropertyGroup>
</Project>
`,
		filepath.Join(fixture.Project, "Program.cs"): `if (21 * 2 != 42)
{
    throw new InvalidOperationException("unexpected answer");
}

Console.WriteLine("dotnet-ok");
`,
		filepath.Join(fixture.Project, "smoke.c"): `#include <stdio.h>
int main(void) {
    puts("native-c-ok");
    return 0;
}
`,
		filepath.Join(fixture.Project, "smoke.cpp"): `#include <iostream>
int main() {
    std::cout << "native-cpp-ok\n";
    return 0;
}
`,
		filepath.Join(fixture.Project, "Smoke.java"): `public final class Smoke {
    public static void main(String[] args) {
        System.out.println("native-java-ok");
    }
}
`,
		filepath.Join(fixture.Project, "smoke.js"): `if (21 * 2 !== 42) {
  throw new Error("unexpected answer")
}
console.log("node-ok")
`,
		filepath.Join(fixture.Project, "smoke.py"): `assert 21 * 2 == 42
print("python-ok")
`,
		filepath.Join(fixture.Project, "justfile"): `herdr-toolchain-smoke:
    python3 -c "print('python3-just-ok')"
    bun -e "console.log('bun-just-ok')"
`,
		filepath.Join(fixture.Project, "smoke.rs"): `fn main() {
    assert_eq!(21 * 2, 42);
    println!("rust-ok");
}
`,
		filepath.Join(fixture.Project, "smoke.zig"): `const std = @import("std");

test "answer" {
    try std.testing.expect(21 * 2 == 42);
}
`,
		filepath.Join(fixture.HandyProject, ".herdr-sandbox", "provision.ps1"): `param(
    [Parameter(Mandatory = $true)]
    [string]$ProjectDirectory
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version 2.0

Install-HandyStack -ProjectDirectory $ProjectDirectory
`,
		filepath.Join(fixture.HandyProject, "package.json"): `{
  "name": "handy-app",
  "private": true,
  "version": "0.0.0"
}
`,
		filepath.Join(fixture.HandyProject, "bun.lock"): "# native Handy stack fixture\n",
		filepath.Join(fixture.HandyProject, "src-tauri", "Cargo.toml"): `[package]
name = "handy"
version = "0.0.0"
edition = "2021"
`,
		filepath.Join(fixture.HandyProject, "src-tauri", "resources", "models", "silero_vad_v4.onnx"): "native Handy model fixture\n",
		filepath.Join(fixture.LocalAppData, "Packages", "Microsoft.WindowsTerminal_8wekyb3d8bbwe", "LocalState", "settings.json"): `{
    "theme": "light",
    "profiles": {
        "defaults": {
            "colorScheme": "Herdr Native Light",
            "font": {
                "face": "Native Test Host Font",
                "size": 12
            }
        },
        "list": []
    },
    "schemes": [
        {
            "name": "Herdr Native Light",
            "background": "#FAFAFA",
            "foreground": "#1F2328",
            "black": "#1F2328",
            "red": "#CF222E",
            "green": "#116329",
            "yellow": "#4D2D00",
            "blue": "#0969DA",
            "purple": "#8250DF",
            "cyan": "#1B7C83",
            "white": "#6E7781",
            "brightBlack": "#57606A",
            "brightRed": "#A40E26",
            "brightGreen": "#1A7F37",
            "brightYellow": "#633C01",
            "brightBlue": "#218BFF",
            "brightPurple": "#A475F9",
            "brightCyan": "#3192AA",
            "brightWhite": "#8C959F",
            "cursorColor": "#0969DA",
            "selectionBackground": "#D0D7DE"
        }
    ]
}
`,
	}
	for path, contents := range files {
		if err := writeNativeAllStacksFixtureFile(path, contents); err != nil {
			return nativeAllStacksFixture{}, err
		}
	}
	for _, absent := range []string{
		filepath.Join(fixture.AppData, "GitHub CLI", "hosts.yml"),
		filepath.Join(fixture.AppData, "herdr", "config.toml"),
		filepath.Join(fixture.ReadOnlyMount, "guest-write-blocked.txt"),
		filepath.Join(fixture.WritableMount, "guest-write.txt"),
	} {
		if err := os.Remove(absent); err != nil && !errors.Is(err, os.ErrNotExist) {
			return nativeAllStacksFixture{}, fmt.Errorf("remove credential-bearing native fixture input %s: %w", absent, err)
		}
	}
	return fixture, nil
}

func verifyNativeAllStacksMounts(fixture nativeAllStacksFixture) error {
	if _, err := os.Lstat(filepath.Join(fixture.ReadOnlyMount, "guest-write-blocked.txt")); !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("read-only folder mount accepted a guest write: %v", err)
	}
	contents, err := os.ReadFile(filepath.Join(fixture.WritableMount, "guest-write.txt"))
	if err != nil {
		return fmt.Errorf("read writable folder-mount result: %w", err)
	}
	if string(contents) != "guest-write-ok\r\n" && string(contents) != "guest-write-ok\n" {
		return fmt.Errorf("writable folder-mount result = %q", contents)
	}
	return nil
}

func writeNativeAllStacksFixtureFile(path, contents string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create native fixture directory for %s: %w", path, err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		return fmt.Errorf("write native fixture file %s: %w", path, err)
	}
	return nil
}

func nativeAllStacksEnvironment(fixture nativeAllStacksFixture) []string {
	removed := map[string]bool{
		"APPDATA":                          true,
		"LOCALAPPDATA":                     true,
		"USERPROFILE":                      true,
		"HOME":                             true,
		"GH_CONFIG_DIR":                    true,
		"XDG_CONFIG_HOME":                  true,
		"XDG_DATA_HOME":                    true,
		"GH_TOKEN":                         true,
		"GITHUB_TOKEN":                     true,
		"GH_ENTERPRISE_TOKEN":              true,
		"GITHUB_ENTERPRISE_TOKEN":          true,
		"HERDR_SANDBOX_TAILSCALE_AUTH_KEY": true,
	}
	environment := make([]string, 0, len(os.Environ())+7)
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		if !removed[strings.ToUpper(name)] {
			environment = append(environment, entry)
		}
	}
	return append(environment,
		"APPDATA="+fixture.AppData,
		"LOCALAPPDATA="+fixture.LocalAppData,
		"USERPROFILE="+fixture.UserProfile,
		"HOME="+fixture.UserProfile,
		"GH_CONFIG_DIR="+filepath.Join(fixture.AppData, "GitHub CLI"),
		"XDG_CONFIG_HOME="+filepath.Join(fixture.UserProfile, ".config"),
		"XDG_DATA_HOME="+filepath.Join(fixture.UserProfile, ".local", "share"),
	)
}

func runNativeAllStacksCLI(ctx context.Context, directory string, environment []string, stdout, stderr io.Writer, executable string, arguments ...string) error {
	command := nativeAllStacksCLICommand(ctx, executable, arguments...)
	command.Dir = directory
	command.Env = environment
	command.Stdout = stdout
	command.Stderr = stderr
	if err := command.Run(); err != nil {
		return fmt.Errorf("run %s: %w", commandText(executable, arguments), err)
	}
	return nil
}

func nativeAllStacksCLICommand(ctx context.Context, executable string, arguments ...string) *exec.Cmd {
	command := exec.CommandContext(ctx, executable, arguments...)
	hiddenprocess.Configure(command)
	return command
}

func runNativeAllStacksSmoke(ctx context.Context, fixture nativeAllStacksFixture, environment []string, stdout, stderr io.Writer) error {
	ssh, err := exec.LookPath("ssh.exe")
	if err != nil {
		return errors.New("native all-stack test requires ssh.exe on PATH")
	}
	sshConfig := filepath.Join(fixture.LocalAppData, "herdr-sandbox", "ssh", "config")
	command := nativeAllStacksSmokeCommand(ctx, ssh, sshConfig)
	command.Dir = fixture.Project
	command.Env = environment
	command.Stdout = stdout
	command.Stderr = stderr
	if err := command.Run(); err != nil {
		return fmt.Errorf("run all-stack smoke over managed SSH: %w", err)
	}
	return nil
}

func nativeAllStacksSmokeCommand(ctx context.Context, ssh, sshConfig string) *hiddenprocess.Command {
	return hiddenCommandContext(ctx, ssh,
		"-T", "-F", sshConfig, "sandbox",
		"powershell.exe", "-NoLogo", "-NoProfile", "-NonInteractive", "-WindowStyle", "Hidden",
		"-ExecutionPolicy", "Bypass", "-File", nativeAllStacksSmokeGuestPath)
}

func encodeNativePowerShell(script string) string {
	codeUnits := utf16.Encode([]rune(script))
	bytes := make([]byte, len(codeUnits)*2)
	for index, value := range codeUnits {
		bytes[index*2] = byte(value)
		bytes[index*2+1] = byte(value >> 8)
	}
	return base64.StdEncoding.EncodeToString(bytes)
}

const nativeAllStacksSmokeScript = `$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'
$env:DOTNET_CLI_TELEMETRY_OPTOUT = '1'
$env:DOTNET_SKIP_FIRST_TIME_EXPERIENCE = '1'
$root = 'C:\HerdrSandbox\all-stacks-smoke'
if (Test-Path -LiteralPath $root) { Remove-Item -LiteralPath $root -Recurse -Force }
$null = New-Item -ItemType Directory -Path $root -Force
$utf8 = New-Object Text.UTF8Encoding($false)

function Write-SmokeFile([string]$Path, [string[]]$Lines) {
    $directory = [IO.Path]::GetDirectoryName($Path)
    if (-not (Test-Path -LiteralPath $directory -PathType Container)) {
        $null = New-Item -ItemType Directory -Path $directory -Force
    }
    [IO.File]::WriteAllText($Path, ([string]::Join([Environment]::NewLine, $Lines) + [Environment]::NewLine), $utf8)
}

function Invoke-SmokeTool([string]$Role, [string]$Executable, [string[]]$Arguments) {
    $previous = $ErrorActionPreference
    try {
        $ErrorActionPreference = 'Continue'
        $output = @(& $Executable @Arguments 2>&1)
        $exitCode = $LASTEXITCODE
    } finally {
        $ErrorActionPreference = $previous
    }
    $text = (($output | ForEach-Object { [string]$_ }) -join [Environment]::NewLine).Trim()
    if ($exitCode -ne 0) { throw "$Role failed with exit code $exitCode. $text" }
    [Console]::Out.WriteLine("[all-stacks] ${Role}: $text")
    return $text
}

function Assert-SmokeOutput([string]$Role, [string]$Output, [string]$Expected) {
    if (-not $Output.Contains($Expected)) { throw "$Role output did not contain $Expected" }
}

$readOnlyMount = 'C:\Mounts\reference'
$writableMount = 'C:\Mounts\worktrees'
if ([IO.File]::ReadAllText((Join-Path $readOnlyMount 'host-reference.txt')).Trim() -cne 'read-only-mount-ok') {
    throw 'Read-only folder mount content is unavailable.'
}
if ([IO.File]::ReadAllText((Join-Path $writableMount 'host-worktrees.txt')).Trim() -cne 'read-write-mount-ok') {
    throw 'Writable folder mount content is unavailable.'
}
$readOnlyWriteBlocked = $false
try {
    [IO.File]::WriteAllText((Join-Path $readOnlyMount 'guest-write-blocked.txt'), ('unexpected' + [Environment]::NewLine), $utf8)
} catch {
    $readOnlyWriteBlocked = $true
}
if (-not $readOnlyWriteBlocked) { throw 'Read-only folder mount accepted a guest write.' }
[IO.File]::WriteAllText((Join-Path $writableMount 'guest-write.txt'), ('guest-write-ok' + [Environment]::NewLine), $utf8)
[Console]::Out.WriteLine('[all-stacks] folder mounts: read-only and read/write OK')

$dotnet = (Get-Command 'dotnet.exe' -CommandType Application -ErrorAction Stop | Select-Object -First 1).Source
$go = (Get-Command 'go.exe' -CommandType Application -ErrorAction Stop | Select-Object -First 1).Source
$node = (Get-Command 'node.exe' -CommandType Application -ErrorAction Stop | Select-Object -First 1).Source
$makensis = (Get-Command 'makensis.exe' -CommandType Application -ErrorAction Stop | Select-Object -First 1).Source
$python = (Get-Command 'python.exe' -CommandType Application -ErrorAction Stop | Select-Object -First 1).Source
$python3 = (Get-Command 'python3.exe' -CommandType Application -ErrorAction Stop | Select-Object -First 1).Source
$uv = (Get-Command 'uv.exe' -CommandType Application -ErrorAction Stop | Select-Object -First 1).Source
$bun = (Get-Command 'bun.exe' -CommandType Application -ErrorAction Stop | Select-Object -First 1).Source
$cargo = (Get-Command 'cargo.exe' -CommandType Application -ErrorAction Stop | Select-Object -First 1).Source
$nextest = (Get-Command 'cargo-nextest.exe' -CommandType Application -ErrorAction Stop | Select-Object -First 1).Source
$just = (Get-Command 'just.exe' -CommandType Application -ErrorAction Stop | Select-Object -First 1).Source
$rustc = (Get-Command 'rustc.exe' -CommandType Application -ErrorAction Stop | Select-Object -First 1).Source
$sh = (Get-Command 'sh.exe' -CommandType Application -ErrorAction Stop | Select-Object -First 1).Source
$zig = (Get-Command 'zig.exe' -CommandType Application -ErrorAction Stop | Select-Object -First 1).Source
$playwrightAgentCLI = (Get-Command 'playwright-cli.cmd' -CommandType Application -ErrorAction Stop | Select-Object -First 1).Source
$tradingView = (Get-Command 'TradingView.exe' -CommandType Application -ErrorAction Stop | Select-Object -First 1).Source
$tv = (Get-Command 'tv.cmd' -CommandType Application -ErrorAction Stop | Select-Object -First 1).Source
$tvcontrol = (Get-Command 'tvcontrol.cmd' -CommandType Application -ErrorAction Stop | Select-Object -First 1).Source
$cmake = (Get-Command 'cmake.exe' -CommandType Application -ErrorAction Stop | Select-Object -First 1).Source
$glslc = (Get-Command 'glslc.exe' -CommandType Application -ErrorAction Stop | Select-Object -First 1).Source
$cl = (Get-Command 'cl.exe' -CommandType Application -ErrorAction Stop | Select-Object -First 1).Source
$link = (Get-Command 'link.exe' -CommandType Application -ErrorAction Stop | Select-Object -First 1).Source
$msbuild = (Get-Command 'msbuild.exe' -CommandType Application -ErrorAction Stop | Select-Object -First 1).Source
$java = (Get-Command 'java.exe' -CommandType Application -ErrorAction Stop | Select-Object -First 1).Source
$javac = (Get-Command 'javac.exe' -CommandType Application -ErrorAction Stop | Select-Object -First 1).Source

$nsisRoot = Join-Path ${env:ProgramFiles(x86)} 'NSIS'
if ([IO.Path]::GetFullPath($makensis) -ine [IO.Path]::GetFullPath((Join-Path $nsisRoot 'makensis.exe'))) {
    throw 'NSIS compiler resolved from an unexpected path.'
}
$nsisVersion = Invoke-SmokeTool 'nsis-version' $makensis @('/VERSION')
if ($nsisVersion -notmatch '^v\d+\.\d+(?:\.\d+)?$') { throw "NSIS version is unexpected: $nsisVersion" }
$nsisRootProbe = Join-Path $root 'nsis'
$null = New-Item -ItemType Directory -Path $nsisRootProbe -Force
$nsisScript = Join-Path $nsisRootProbe 'smoke.nsi'
$nsisInstaller = Join-Path $nsisRootProbe 'smoke.exe'
Write-SmokeFile $nsisScript @('Unicode true','Name "Native NSIS Smoke"','OutFile "smoke.exe"','RequestExecutionLevel user','SilentInstall silent','Section "Smoke"','  SetOutPath "$TEMP"','SectionEnd')
$null = Invoke-SmokeTool 'nsis-compile' $makensis @('/WX','/V2','/NOCONFIG',$nsisScript)
$nsisBytes = [IO.File]::ReadAllBytes($nsisInstaller)
if ($nsisBytes.Length -lt 1024 -or $nsisBytes[0] -ne 0x4d -or $nsisBytes[1] -ne 0x5a) {
    throw 'NSIS smoke compiler output is not a Windows executable.'
}
[Console]::Out.WriteLine('[all-stacks] nsis: installer compile OK')

$pythonAliasRoot = 'C:\HerdrSandbox\tools\python\bin'
if ([IO.Path]::GetFullPath($python) -ine (Join-Path $pythonAliasRoot 'python.exe') -or
    [IO.Path]::GetFullPath($python3) -ine (Join-Path $pythonAliasRoot 'python3.exe')) {
    throw 'Python commands resolved from unexpected paths.'
}
$visualStudioRoot = 'C:\HerdrSandbox\toolchains\visual-studio'
if (-not [IO.Path]::GetFullPath($cl).StartsWith($visualStudioRoot + '\', [StringComparison]::OrdinalIgnoreCase) -or
    -not [IO.Path]::GetFullPath($link).StartsWith($visualStudioRoot + '\', [StringComparison]::OrdinalIgnoreCase) -or
    -not [IO.Path]::GetFullPath($msbuild).StartsWith($visualStudioRoot + '\', [StringComparison]::OrdinalIgnoreCase) -or
    [string]::IsNullOrWhiteSpace($env:INCLUDE) -or [string]::IsNullOrWhiteSpace($env:LIB)) {
    throw 'C/C++ toolchain commands or environment are unavailable in the SSH session.'
}
$null = Invoke-SmokeTool 'msbuild-version' $msbuild @('-version','-nologo')
$nativeRoot = Join-Path $root 'native'
$cExecutable = Join-Path $nativeRoot 'smoke-c.exe'
$cppExecutable = Join-Path $nativeRoot 'smoke-cpp.exe'
$null = New-Item -ItemType Directory -Path $nativeRoot -Force
$null = Invoke-SmokeTool 'c-compile' $cl @('/nologo','/W4','/WX','/Z7','/TC','/c','C:\Workspaces\project\smoke.c',"/Fo:$nativeRoot\smoke-c.obj")
$null = Invoke-SmokeTool 'c-link' $link @('/NOLOGO','/DEBUG:NONE',"/OUT:$cExecutable","$nativeRoot\smoke-c.obj")
$cOutput = Invoke-SmokeTool 'c-run' $cExecutable @()
Assert-SmokeOutput 'c-run' $cOutput 'native-c-ok'
$null = Invoke-SmokeTool 'cpp-compile' $cl @('/nologo','/W4','/WX','/Z7','/EHsc','/std:c++20','/TP','/c','C:\Workspaces\project\smoke.cpp',"/Fo:$nativeRoot\smoke-cpp.obj")
$null = Invoke-SmokeTool 'cpp-link' $link @('/NOLOGO','/DEBUG:NONE',"/OUT:$cppExecutable","$nativeRoot\smoke-cpp.obj")
$cppOutput = Invoke-SmokeTool 'cpp-run' $cppExecutable @()
Assert-SmokeOutput 'cpp-run' $cppOutput 'native-cpp-ok'

$javaHome = [IO.Path]::GetFullPath([string]$env:JAVA_HOME).TrimEnd('\')
if ([string]::IsNullOrWhiteSpace($javaHome) -or
    [IO.Path]::GetFullPath($java) -ine (Join-Path $javaHome 'bin\java.exe') -or
    [IO.Path]::GetFullPath($javac) -ine (Join-Path $javaHome 'bin\javac.exe')) {
    throw 'Microsoft OpenJDK JAVA_HOME or commands are unavailable in the SSH session.'
}
$javaVersion = Invoke-SmokeTool 'java-version' $java @('-version')
Assert-SmokeOutput 'java-version' $javaVersion 'Microsoft'
$null = Invoke-SmokeTool 'javac-version' $javac @('-version')
$javaRoot = Join-Path $root 'java'
$null = New-Item -ItemType Directory -Path $javaRoot -Force
$null = Invoke-SmokeTool 'java-compile' $javac @('-d',$javaRoot,'C:\Workspaces\project\Smoke.java')
$javaOutput = Invoke-SmokeTool 'java-run' $java @('-cp',$javaRoot,'Smoke')
Assert-SmokeOutput 'java-run' $javaOutput 'native-java-ok'

$null = Invoke-SmokeTool 'dotnet-version' $dotnet @('--version')
$dotnetRoot = Join-Path $root 'dotnet'
Write-SmokeFile (Join-Path $dotnetRoot 'Smoke.csproj') @('<Project Sdk="Microsoft.NET.Sdk">','  <PropertyGroup>','    <OutputType>Exe</OutputType>','    <TargetFramework>net10.0</TargetFramework>','  </PropertyGroup>','</Project>')
Write-SmokeFile (Join-Path $dotnetRoot 'Program.cs') @('using System;','Console.WriteLine("dotnet-smoke-ok");')
$null = Invoke-SmokeTool 'dotnet-build' $dotnet @('build',(Join-Path $dotnetRoot 'Smoke.csproj'),'--nologo','--verbosity','quiet')
$dotnetOutput = Invoke-SmokeTool 'dotnet-run' $dotnet @((Join-Path $dotnetRoot 'bin\Debug\net10.0\Smoke.dll'))
Assert-SmokeOutput 'dotnet-run' $dotnetOutput 'dotnet-smoke-ok'

$goRoot = Join-Path $root 'go'
Write-SmokeFile (Join-Path $goRoot 'go.mod') @('module smoke','go 1.22')
Write-SmokeFile (Join-Path $goRoot 'main.go') @('package main','import "fmt"','func main() { fmt.Println("go-smoke-ok") }','func add(a, b int) int { return a + b }')
Write-SmokeFile (Join-Path $goRoot 'main_test.go') @('package main','import "testing"','func TestAdd(t *testing.T) { if add(2, 3) != 5 { t.Fatal("bad sum") } }')
$null = Invoke-SmokeTool 'go-version' $go @('version')
Push-Location $goRoot
try {
    $null = Invoke-SmokeTool 'go-test' $go @('test','./...')
    $goOutput = Invoke-SmokeTool 'go-run' $go @('run','.')
} finally { Pop-Location }
Assert-SmokeOutput 'go-run' $goOutput 'go-smoke-ok'

$null = Invoke-SmokeTool 'node-version' $node @('--version')
$nodeFile = Join-Path $root 'node\smoke.js'
Write-SmokeFile $nodeFile @('console.log("node-smoke-ok");')
$nodeOutput = Invoke-SmokeTool 'node-run' $node @($nodeFile)
Assert-SmokeOutput 'node-run' $nodeOutput 'node-smoke-ok'

$playwrightVersions = @(Get-ChildItem -LiteralPath 'C:\HerdrSandbox\tools\playwright' -Directory -Force)
if ($playwrightVersions.Count -ne 1 -or $playwrightVersions[0].Name -notmatch '^\d+\.\d+\.\d+$') {
    throw "Playwright tool version directories are invalid: $($playwrightVersions.Name -join ', ')"
}
$playwrightCLI = Join-Path $playwrightVersions[0].FullName 'node_modules\playwright\cli.js'
if (-not (Test-Path -LiteralPath $playwrightCLI -PathType Leaf)) { throw "Playwright CLI is missing: $playwrightCLI" }
$expectedPlaywrightBrowsers = 'C:\HerdrSandbox\tools\playwright-browsers'
if ($env:PLAYWRIGHT_BROWSERS_PATH -cne $expectedPlaywrightBrowsers) {
    throw "SSH session Playwright browser path is unexpected: $env:PLAYWRIGHT_BROWSERS_PATH"
}
$playwrightScreenshot = Join-Path $root 'node\playwright-chromium.png'
$null = Invoke-SmokeTool 'playwright-chromium' $node @($playwrightCLI, 'screenshot', '-b', 'chromium', 'about:blank', $playwrightScreenshot)
$playwrightScreenshotBytes = [IO.File]::ReadAllBytes($playwrightScreenshot)
if ($playwrightScreenshotBytes.Length -lt 8 -or
    (($playwrightScreenshotBytes[0..7] -join ',') -cne '137,80,78,71,13,10,26,10')) {
    throw 'Playwright Chromium SSH smoke returned an invalid PNG screenshot.'
}
[Console]::Out.WriteLine('[all-stacks] playwright-chromium: headless launch OK')

$playwrightAgentVersion = Invoke-SmokeTool 'playwright-cli-version' $playwrightAgentCLI @('--version')
if ($playwrightAgentVersion -cne '0.1.17') { throw "Playwright CLI version is unexpected: $playwrightAgentVersion" }
$playwrightPowerShellShim = Join-Path (Split-Path -Parent $playwrightAgentCLI) 'playwright-cli.ps1'
if (Test-Path -LiteralPath $playwrightPowerShellShim) {
    throw "Playwright CLI PowerShell shim remains installed: $playwrightPowerShellShim"
}
$playwrightExtensionKey = 'HKLM:\SOFTWARE\Wow6432Node\Microsoft\Edge\Extensions\mmlmfjhmonkocbjadbfplnigmagldckm'
$playwrightExtensionUpdateURL = [string](Get-ItemPropertyValue -LiteralPath $playwrightExtensionKey -Name 'update_url' -ErrorAction Stop)
if ($playwrightExtensionUpdateURL -cne 'https://clients2.google.com/service/update2/crx') {
    throw "Playwright Extension registration is unexpected: $playwrightExtensionUpdateURL"
}
[Console]::Out.WriteLine('[all-stacks] playwright-cli: exact CLI and official extension registration OK')

$tradingViewRoot = 'C:\HerdrSandbox\tools\TradingView.TradingViewDesktop'
$tradingViewExecutables = @(Get-ChildItem -LiteralPath $tradingViewRoot -File -Recurse -Filter 'TradingView.exe')
$tradingViewManifestPath = Join-Path $tradingViewRoot 'AppxManifest.xml'
if ($tradingViewExecutables.Count -ne 1 -or
    [IO.Path]::GetFullPath($tradingView) -ine [IO.Path]::GetFullPath($tradingViewExecutables[0].FullName) -or
    [string]$tradingViewExecutables[0].VersionInfo.FileVersion -notmatch '^\d+\.\d+\.\d+\.\d+$' -or
    -not (Test-Path -LiteralPath $tradingViewManifestPath -PathType Leaf)) {
    throw 'TradingView Desktop portable payload is invalid.'
}
[xml]$tradingViewManifest = [IO.File]::ReadAllText($tradingViewManifestPath)
if ([string]$tradingViewManifest.Package.Identity.Name -cne 'TradingView.Desktop' -or
    [string]$tradingViewManifest.Package.Identity.Version -cne [string]$tradingViewExecutables[0].VersionInfo.FileVersion -or
    [string]$tradingViewManifest.Package.Identity.Publisher -cne 'CN="TradingView, Inc.", O="TradingView, Inc.", S=Ohio, C=US') {
    throw 'TradingView Desktop package identity is invalid.'
}
$tvControlRoot = 'C:\HerdrSandbox\tools\tvcontrol'
$expectedTVCommand = Join-Path $tvControlRoot 'tv.cmd'
$expectedTVControlCommand = Join-Path $tvControlRoot 'tvcontrol.cmd'
if ([IO.Path]::GetFullPath($tv) -ine [IO.Path]::GetFullPath($expectedTVCommand) -or
    [IO.Path]::GetFullPath($tvcontrol) -ine [IO.Path]::GetFullPath($expectedTVControlCommand)) {
    throw 'TVControl commands resolved from unexpected paths.'
}
$tvControlPackagePath = Join-Path $tvControlRoot 'node_modules\@ferroxlabs\tvcontrol\package.json'
$tvControlPackage = [IO.File]::ReadAllText($tvControlPackagePath) | ConvertFrom-Json
$tvBin = [string]$tvControlPackage.bin.tv
$tvControlBin = [string]$tvControlPackage.bin.tvcontrol
if ([string]$tvControlPackage.name -cne '@ferroxlabs/tvcontrol' -or
    [string]$tvControlPackage.version -notmatch '^\d+\.\d+\.\d+$' -or
    [string]::IsNullOrWhiteSpace($tvBin) -or [string]::IsNullOrWhiteSpace($tvControlBin) -or
    $tvBin -ceq $tvControlBin) {
    throw 'TVControl installed package identity is invalid.'
}
$tvHelp = Invoke-SmokeTool 'tvcontrol-help' $tv @('--help')
Assert-SmokeOutput 'tvcontrol-help' $tvHelp 'Usage: tv <command> [options]'
foreach ($shim in @((Join-Path $tvControlRoot 'tv.ps1'), (Join-Path $tvControlRoot 'tvcontrol.ps1'))) {
    if (Test-Path -LiteralPath $shim) { throw "TVControl PowerShell shim remains installed: $shim" }
}
[Console]::Out.WriteLine('[all-stacks] tradingview: portable signed-MSIX payload, TVControl command mappings, and CLI help OK; launch intentionally skipped')

$null = Invoke-SmokeTool 'python-version' $python @('--version')
$pythonFile = Join-Path $root 'python\smoke.py'
Write-SmokeFile $pythonFile @('print("python-smoke-ok")')
$pythonOutput = Invoke-SmokeTool 'python-run' $python @($pythonFile)
Assert-SmokeOutput 'python-run' $pythonOutput 'python-smoke-ok'
$null = Invoke-SmokeTool 'python3-version' $python3 @('--version')
$null = Invoke-SmokeTool 'uv-version' $uv @('--version')
$expectedUvCache = 'C:\HerdrSandbox\cache\uv'
if ($env:UV_CACHE_DIR -cne $expectedUvCache -or $env:UV_NO_MANAGED_PYTHON -cne '1') {
    throw "uv environment is unexpected: cache=$env:UV_CACHE_DIR managed=$env:UV_NO_MANAGED_PYTHON"
}
$uvCache = Invoke-SmokeTool 'uv-cache-dir' $uv @('cache','dir')
if ([IO.Path]::GetFullPath($uvCache).TrimEnd('\') -ine $expectedUvCache) {
    throw "uv cache path is unexpected: $uvCache"
}
$uvRoot = Join-Path $root 'python-ai'
Write-SmokeFile (Join-Path $uvRoot 'pyproject.toml') @('[project]','name = "herdr-python-ai-smoke"','version = "0.0.0"','requires-python = ">=3.13,<3.14"','dependencies = []','','[tool.uv]','package = false')
Write-SmokeFile (Join-Path $uvRoot 'smoke.py') @('print("python-ai-smoke-ok")')
Push-Location $uvRoot
try {
    $null = Invoke-SmokeTool 'uv-sync' $uv @('sync','--offline')
    $uvOutput = Invoke-SmokeTool 'uv-run' $uv @('run','--offline','--frozen','python','smoke.py')
} finally { Pop-Location }
Assert-SmokeOutput 'uv-run' $uvOutput 'python-ai-smoke-ok'
if (-not (Test-Path -LiteralPath (Join-Path $uvRoot 'uv.lock') -PathType Leaf) -or
    -not (Test-Path -LiteralPath (Join-Path $uvRoot '.venv\Scripts\python.exe') -PathType Leaf)) {
    throw 'uv did not create the locked Python 3.13 project environment.'
}

$null = Invoke-SmokeTool 'cargo-version' $cargo @('--version')
$null = Invoke-SmokeTool 'bun-version' $bun @('--version')
$bunOutput = Invoke-SmokeTool 'bun-run' $bun @('-e',"console.log('bun-smoke-ok')")
Assert-SmokeOutput 'bun-run' $bunOutput 'bun-smoke-ok'
$null = Invoke-SmokeTool 'cargo-nextest-version' $nextest @('--version')
$null = Invoke-SmokeTool 'just-version' $just @('--version')
$null = Invoke-SmokeTool 'rustc-version' $rustc @('--version')
$null = Invoke-SmokeTool 'sh-version' $sh @('--version')
$shellOutput = Invoke-SmokeTool 'sh-run' $sh @('-lc','printf sh-smoke-ok')
Assert-SmokeOutput 'sh-run' $shellOutput 'sh-smoke-ok'
$justRoot = 'C:\Workspaces\project'
Push-Location $justRoot
try {
    $justOutput = Invoke-SmokeTool 'herdr-just-toolchain' $just @('herdr-toolchain-smoke')
} finally { Pop-Location }
Assert-SmokeOutput 'herdr-just-toolchain' $justOutput 'python3-just-ok'
Assert-SmokeOutput 'herdr-just-toolchain' $justOutput 'bun-just-ok'
$expectedLibghosttyOutput = 'C:\HerdrSandbox\build\cargo-target\zig-out'
if ($env:LIBGHOSTTY_VT_ZIG_OUT_DIR -cne $expectedLibghosttyOutput -or
    -not (Test-Path -LiteralPath $expectedLibghosttyOutput -PathType Container)) {
    throw "Herdr libghostty output environment is unavailable: $env:LIBGHOSTTY_VT_ZIG_OUT_DIR"
}
$rustRoot = Join-Path $root 'rust'
$rustSource = Join-Path $rustRoot 'main.rs'
$rustBinary = Join-Path $rustRoot 'smoke-rust.exe'
Write-SmokeFile $rustSource @('fn main() { println!("rust-smoke-ok"); }')
$null = Invoke-SmokeTool 'rust-compile' $rustc @($rustSource,'-o',$rustBinary)
$rustOutput = Invoke-SmokeTool 'rust-run' $rustBinary @()
Assert-SmokeOutput 'rust-run' $rustOutput 'rust-smoke-ok'

$cmakeVersion = Invoke-SmokeTool 'handy-cmake-version' $cmake @('--version')
Assert-SmokeOutput 'handy-cmake-version' $cmakeVersion 'cmake version '
$null = Invoke-SmokeTool 'handy-glslc-version' $glslc @('--version')
$expectedVulkanRoot = 'C:\VulkanSDK\1.4.309.0'
$expectedHandyPrefix = 'C:\HerdrSandbox\tools\handy-cmake-prefix'
$handyConfig = Join-Path $expectedHandyPrefix 'share\cmake\SPIRV-Headers\SPIRV-HeadersConfig.cmake'
if ($env:VULKAN_SDK -cne $expectedVulkanRoot -or
    @($env:CMAKE_PREFIX_PATH -split ';')[0] -cne $expectedHandyPrefix -or
    -not (Test-Path -LiteralPath $handyConfig -PathType Leaf) -or
    -not ([IO.File]::ReadAllText($handyConfig).Contains(($expectedVulkanRoot + '/Include').Replace('\','/')))) {
    throw 'Handy Vulkan SDK or corrected SPIRV-Headers package is unavailable.'
}
$webViewKey = 'HKLM:\SOFTWARE\WOW6432Node\Microsoft\EdgeUpdate\Clients\{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}'
$webView = Get-ItemProperty -LiteralPath $webViewKey -ErrorAction Stop
$webViewExecutable = Join-Path (Join-Path ([string]$webView.location) ([string]$webView.pv)) 'msedgewebview2.exe'
$webViewSignature = Get-AuthenticodeSignature -LiteralPath $webViewExecutable
if ([string]$webView.name -cne 'Microsoft Edge WebView2 Runtime' -or
    -not (Test-Path -LiteralPath $webViewExecutable -PathType Leaf) -or
    $webViewSignature.Status -ne [System.Management.Automation.SignatureStatus]::Valid -or
    $webViewSignature.SignerCertificate.Subject -notmatch '(^|,\s*)O=Microsoft Corporation(,|$)') {
    throw 'Handy WebView2 Runtime is unavailable or untrusted.'
}
[Console]::Out.WriteLine('[all-stacks] handy-native-toolchain: CMake, Vulkan 1.4.309.0, SPIRV-Headers, and WebView2 OK')

$null = Invoke-SmokeTool 'zig-version' $zig @('version')
$zigSource = Join-Path $root 'zig\smoke.zig'
Write-SmokeFile $zigSource @('const std = @import("std");','test "addition" {','    try std.testing.expect(2 + 2 == 4);','}')
$null = Invoke-SmokeTool 'zig-test' $zig @('test',$zigSource)

$terminalSettingsPath = Join-Path $env:LOCALAPPDATA 'Packages\Microsoft.WindowsTerminal_8wekyb3d8bbwe\LocalState\settings.json'
if (-not (Test-Path -LiteralPath $terminalSettingsPath -PathType Leaf)) { throw 'Windows Terminal settings were not copied.' }
$terminalSettings = [IO.File]::ReadAllText($terminalSettingsPath) | ConvertFrom-Json
$powerShellGUID = '{574e775e-4f2a-5b96-ac1e-a2962a402336}'
$powerShellProfiles = @($terminalSettings.profiles.list | Where-Object { [string]$_.guid -ieq $powerShellGUID })
if ([string]$terminalSettings.theme -cne 'light' -or [string]$terminalSettings.defaultProfile -ine $powerShellGUID -or
    [string]$terminalSettings.profiles.defaults.colorScheme -cne 'Herdr Native Light' -or
    [string]$terminalSettings.profiles.defaults.font.face -cne 'GeistMono Nerd Font' -or
    $powerShellProfiles.Count -ne 1 -or [string]$powerShellProfiles[0].commandline -cne 'pwsh.exe' -or
    [string]$powerShellProfiles[0].font.face -cne 'GeistMono Nerd Font' -or
    @($terminalSettings.schemes | Where-Object { [string]$_.name -ceq 'Herdr Native Light' }).Count -ne 1) {
    throw 'Windows Terminal theme, default profile, or font does not match the transferred configuration.'
}
$starshipConfigPath = Join-Path $env:USERPROFILE '.config\starship.toml'
if (-not (Test-Path -LiteralPath $starshipConfigPath -PathType Leaf)) { throw 'Starship configuration is missing.' }
$starshipConfig = [IO.File]::ReadAllText($starshipConfigPath)
if ($starshipConfig -notmatch "(?m)^palette = 'catppuccin_latte'\r?$" -or $starshipConfig -match "(?m)^palette = 'catppuccin_mocha'\r?$") {
    throw 'Starship did not retain the Catppuccin Latte preset selected by the light Terminal theme.'
}
$starship = (Get-Command 'starship.exe' -CommandType Application -ErrorAction Stop | Select-Object -First 1).Source
$previousStarshipConfig = $env:STARSHIP_CONFIG
try {
    $env:STARSHIP_CONFIG = $starshipConfigPath
    $null = Invoke-SmokeTool 'starship-prompt' $starship @('prompt')
} finally { $env:STARSHIP_CONFIG = $previousStarshipConfig }

Remove-Item -LiteralPath $root -Recurse -Force
[Console]::Out.WriteLine('[all-stacks] PASS: C/C++, Java, dotnet, go, node, Handy and Herdr virtual stacks')
[Console]::Out.WriteLine('[all-stacks] PASS: Windows Terminal light chrome and color scheme, PowerShell 7, GeistMono Nerd Font, Catppuccin Latte Starship')
`
