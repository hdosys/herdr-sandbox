package sandbox

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestHyperFramesOpenCodeLauncherUsesProcessScopedSkillsInWindowsPowerShell51(t *testing.T) {
	requireExternalBoundaryTest(t, "Windows PowerShell HyperFrames OpenCode activation")
	if runtime.GOOS != "windows" {
		t.Skip("Windows PowerShell 5.1 HyperFrames OpenCode activation contract")
	}

	root := t.TempDir()
	skillsRoot := filepath.Join(root, "activation", "skills")
	for _, name := range []string{"hyperframes", "hyperframes-core"} {
		directory := filepath.Join(skillsRoot, name)
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(directory, "SKILL.md"), []byte("---\nname: "+name+"\ndescription: test\n---\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	launcher := filepath.Join(root, "bin", "hyperframes-opencode.ps1")
	if err := os.MkdirAll(filepath.Dir(launcher), 0o700); err != nil {
		t.Fatal(err)
	}
	fakeOpenCode := filepath.Join(root, "commands", "opencode.exe")
	if err := os.MkdirAll(filepath.Dir(fakeOpenCode), 0o700); err != nil {
		t.Fatal(err)
	}
	testExecutable, err := os.Executable()
	if err != nil {
		t.Fatalf("resolve test executable: %v", err)
	}
	commandBytes, err := os.ReadFile(testExecutable)
	if err != nil {
		t.Fatalf("read test executable: %v", err)
	}
	if err := os.WriteFile(fakeOpenCode, commandBytes, 0o700); err != nil {
		t.Fatalf("write fake OpenCode command: %v", err)
	}

	quote := func(value string) string { return strings.ReplaceAll(value, "'", "''") }
	stackPath := defaultProvisioningPath(t, stackProvisioningName)
	script := fmt.Sprintf(`$ErrorActionPreference = 'Stop'
$tokens = $null
$errors = $null
$ast = [Management.Automation.Language.Parser]::ParseFile('%s', [ref]$tokens, [ref]$errors)
if ($errors.Count -ne 0) { throw $errors[0].Message }
foreach ($name in @('Assert-StackHyperFramesSkillTree', 'Assert-StackHyperFramesActivationSkills', 'Write-StackHyperFramesOpenCodeLauncher')) {
    $definition = $ast.Find({ param($node) $node -is [Management.Automation.Language.FunctionDefinitionAst] -and $node.Name -ceq $name }, $true)
    if ($null -eq $definition) { throw "Missing HyperFrames activation helper: $name" }
    Invoke-Expression $definition.Extent.Text
}
$skillRoot = '%s'
$report = [pscustomobject]@{
    updateAvailable = $false
    lockMissing = $false
    scope = 'global'
    agent = 'claude-code'
    location = $skillRoot
    summary = [pscustomobject]@{ current = 2; outdated = 0; missing = 0; removed = 0 }
    skills = @(
        [pscustomobject]@{ name = 'hyperframes'; status = 'current' },
        [pscustomobject]@{ name = 'hyperframes-core'; status = 'current' }
    )
}
$names = @(Assert-StackHyperFramesActivationSkills -Report $report -SkillRoot $skillRoot)
if (($names -join '|') -cne 'hyperframes|hyperframes-core') { throw "Unexpected skill names: $($names -join '|')" }
$launcher = '%s'
Write-StackHyperFramesOpenCodeLauncher -Path $launcher -SkillRoot $skillRoot
$env:Path = '%s;' + $env:Path
Remove-Item Env:\OPENCODE_CONFIG_CONTENT -ErrorAction SilentlyContinue
$env:HERDR_HYPERFRAMES_TEST_HELPER = '1'
$env:HERDR_HYPERFRAMES_EXPECTED_SKILL_ROOT = [IO.Path]::GetFullPath($skillRoot)
try {
    & $launcher '-test.run=^TestHyperFramesOpenCodeChild$'
    if ($LASTEXITCODE -ne 0) { throw "Launcher child failed with exit code $LASTEXITCODE." }
} finally {
    Remove-Item Env:\HERDR_HYPERFRAMES_EXPECTED_SKILL_ROOT -ErrorAction SilentlyContinue
    Remove-Item Env:\HERDR_HYPERFRAMES_TEST_HELPER -ErrorAction SilentlyContinue
}
if (Test-Path Env:\OPENCODE_CONFIG_CONTENT) { throw 'Launcher leaked inline OpenCode configuration.' }
$existing = '{"model":"preserve"}'
$env:OPENCODE_CONFIG_CONTENT = $existing
$rejected = $false
try { & $launcher '-test.run=^TestHyperFramesOpenCodeChild$' } catch {
    $rejected = $_.Exception.Message -like '*requires OPENCODE_CONFIG_CONTENT to be unset*'
}
if (-not $rejected -or $env:OPENCODE_CONFIG_CONTENT -cne $existing) {
    throw 'Launcher replaced or accepted existing inline OpenCode configuration.'
}
`, quote(stackPath), quote(skillsRoot), quote(launcher), quote(filepath.Dir(fakeOpenCode)))
	scriptPath := filepath.Join(root, "hyperframes-opencode-test.ps1")
	if err := os.WriteFile(scriptPath, []byte(script), 0o600); err != nil {
		t.Fatal(err)
	}
	command := hiddenCommand(mustWindowsPowerShellPath(t), "-NoLogo", "-NoProfile", "-NonInteractive", "-WindowStyle", "Hidden", "-ExecutionPolicy", "Bypass", "-File", scriptPath)
	command.Env = append(os.Environ(), "PSModulePath="+os.Getenv("PSModulePath"))
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("HyperFrames OpenCode activation contract: %v: %s", err, output)
	}
}

func TestHyperFramesOpenCodeChild(t *testing.T) {
	if os.Getenv("HERDR_HYPERFRAMES_TEST_HELPER") != "1" {
		return
	}
	var config struct {
		Skills struct {
			Paths []string `json:"paths"`
		} `json:"skills"`
	}
	if err := json.Unmarshal([]byte(os.Getenv("OPENCODE_CONFIG_CONTENT")), &config); err != nil {
		t.Fatalf("decode inline OpenCode configuration: %v", err)
	}
	expected := os.Getenv("HERDR_HYPERFRAMES_EXPECTED_SKILL_ROOT")
	if len(config.Skills.Paths) != 1 ||
		!strings.EqualFold(filepath.Clean(config.Skills.Paths[0]), filepath.Clean(expected)) {
		t.Fatalf("inline OpenCode skill paths = %q, want %q", config.Skills.Paths, expected)
	}
}
