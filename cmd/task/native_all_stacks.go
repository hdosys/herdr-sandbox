package main

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"herdr-sandbox/internal/hiddenprocess"
	"herdr-sandbox/internal/productidentity"
)

const (
	nativeAllStacksMarkerContents = "herdr-sandbox native-all-stacks fixture v1\n"
	nativeAllStacksUpTimeout      = "90m"
	nativeAllStacksSmokeGuestPath = `C:\Mounts\reference\native-all-stacks-smoke.ps1`
	nativeAudioSmokeGuestPath     = `C:\Mounts\reference\native-audio-connection-smoke.ps1`
	nativeAudioReaperGuestPath    = `C:\Mounts\reference\native-audio-reaper-smoke.lua`
)

//go:embed assets/native-all-stacks-smoke.ps1
var nativeAllStacksSmokeScript string

//go:embed assets/native-audio-connection-smoke.ps1
var nativeAudioConnectionSmokeScript string

//go:embed assets/native-audio-reaper-smoke.lua
var nativeAudioReaperSmokeScript string

var nativeAcceptanceBoundaries = []string{
	"fresh-windows-sandbox",
	"provisioning-ready",
	"all-built-in-stacks",
	"audio-reaper-connection",
	"managed-ssh",
	"folder-mounts",
	"nsis-compile",
	"owned-shutdown",
}

type nativeAcceptanceEvidence struct {
	Kind       string   `json:"kind"`
	Commit     string   `json:"commit"`
	Platform   string   `json:"platform"`
	Command    string   `json:"command"`
	Result     string   `json:"result"`
	Boundaries []string `json:"boundaries"`
}

func nativeAllStacks(ctx context.Context, stdout, stderr io.Writer) (resultErr error) {
	if runtime.GOOS != "windows" {
		return errors.New("native-all-stacks requires Windows and Windows Sandbox")
	}
	commit, err := nativeAcceptanceCommit(ctx)
	if err != nil {
		return err
	}
	root, err := filepath.Abs(filepath.Join("build", "native-all-stacks"))
	if err != nil {
		return fmt.Errorf("resolve native all-stack fixture root: %w", err)
	}
	fixture, err := prepareNativeAllStacksFixture(root)
	if err != nil {
		return err
	}
	defer func() {
		retained := map[string]bool{canonicalLocalInstallerName: true}
		resultErr = errors.Join(resultErr, cleanBuildOutputs(".", retained))
	}()
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
	if _, err := fmt.Fprintln(stdout, "Native all-stack test passed: folder mounts, opensrc source inspection, Android CLI and wireless ADB tooling, REAPER-to-AudioGridder connection, C/C++, Java, NSIS, dotnet, go, Handy and Herdr virtual stacks, node with Playwright Chromium, Playwright CLI registration, Terminal, and Starship."); err != nil {
		return err
	}
	return writeNativeAcceptanceEvidence(stdout, commit)
}

func nativeAcceptanceCommit(ctx context.Context) (string, error) {
	commit, err := sourceRevision(ctx)
	if err != nil {
		return "", fmt.Errorf("resolve native acceptance commit: %w", err)
	}
	output, err := hiddenCommandContext(ctx, "git", "status", "--porcelain=v1", "--untracked-files=all").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("inspect native acceptance source state: %w", err)
	}
	if strings.TrimSpace(string(output)) != "" {
		return "", errors.New("native-all-stacks requires a clean committed working tree so its evidence identifies one immutable commit")
	}
	return commit, nil
}

func writeNativeAcceptanceEvidence(stdout io.Writer, commit string) error {
	evidence := nativeAcceptanceEvidence{
		Kind:       "native-acceptance",
		Commit:     commit,
		Platform:   runtime.GOOS + "/" + runtime.GOARCH,
		Command:    "go run ./cmd/task native-all-stacks",
		Result:     "passed",
		Boundaries: append([]string(nil), nativeAcceptanceBoundaries...),
	}
	if err := json.NewEncoder(stdout).Encode(evidence); err != nil {
		return fmt.Errorf("write native acceptance evidence: %w", err)
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
  "configurationSync": {
    "pullHostGitRepositoriesOnUp": false,
    "pullHostGitRepositoriesOnDown": false
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
		filepath.Join(fixture.ReadOnlyMount, "host-reference.txt"):                "read-only-mount-ok\n",
		filepath.Join(fixture.ReadOnlyMount, "native-all-stacks-smoke.ps1"):       nativeAllStacksSmokeScript,
		filepath.Join(fixture.ReadOnlyMount, "native-audio-connection-smoke.ps1"): nativeAudioConnectionSmokeScript,
		filepath.Join(fixture.ReadOnlyMount, "native-audio-reaper-smoke.lua"):     nativeAudioReaperSmokeScript,
		filepath.Join(fixture.WritableMount, "host-worktrees.txt"):                "read-write-mount-ok\n",
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
Install-AndroidStack
Install-AudioStack
Install-GoStack
Install-HerdrStack -ProjectDirectory $ProjectDirectory
Install-HyperFramesStack
Install-CppStack
Install-JavaStack
Install-NSISStack
Install-NushellStack
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
	command = nativeAudioConnectionSmokeCommand(ctx, ssh, sshConfig)
	command.Dir = fixture.Project
	command.Env = environment
	command.Stdout = stdout
	command.Stderr = stderr
	if err := command.Run(); err != nil {
		return fmt.Errorf("run native audio connection smoke over managed SSH: %w", err)
	}
	return nil
}

func nativeAllStacksSmokeCommand(ctx context.Context, ssh, sshConfig string) *hiddenprocess.Command {
	return hiddenCommandContext(ctx, ssh,
		"-T", "-F", sshConfig, "sandbox",
		"powershell.exe", "-NoLogo", "-NoProfile", "-NonInteractive", "-WindowStyle", "Hidden",
		"-ExecutionPolicy", "Bypass", "-File", nativeAllStacksSmokeGuestPath)
}

func nativeAudioConnectionSmokeCommand(ctx context.Context, ssh, sshConfig string) *hiddenprocess.Command {
	return hiddenCommandContext(ctx, ssh,
		"-T", "-F", sshConfig, "sandbox",
		"powershell.exe", "-NoLogo", "-NoProfile", "-NonInteractive", "-WindowStyle", "Hidden",
		"-ExecutionPolicy", "Bypass", "-File", nativeAudioSmokeGuestPath,
		"-ReaperScriptPath", nativeAudioReaperGuestPath)
}
