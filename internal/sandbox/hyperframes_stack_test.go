package sandbox

import (
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
	configOutput := filepath.Join(root, "opencode-config.txt")
	if err := os.MkdirAll(filepath.Dir(fakeOpenCode), 0o700); err != nil {
		t.Fatal(err)
	}
	commandInterpreter := os.Getenv("ComSpec")
	commandBytes, err := os.ReadFile(commandInterpreter)
	if err != nil {
		t.Fatalf("read command interpreter: %v", err)
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
$env:HERDR_HYPERFRAMES_CONFIG_OUTPUT = '%s'
try {
    & $launcher /d /c 'set OPENCODE_CONFIG_CONTENT>"%%HERDR_HYPERFRAMES_CONFIG_OUTPUT%%"'
    if ($LASTEXITCODE -ne 0 -or -not (Test-Path -LiteralPath $env:HERDR_HYPERFRAMES_CONFIG_OUTPUT -PathType Leaf)) {
        throw 'Launcher child did not capture its inline configuration.'
    }
    $captured = [IO.File]::ReadAllText($env:HERDR_HYPERFRAMES_CONFIG_OUTPUT).Trim()
} finally {
    Remove-Item Env:\HERDR_HYPERFRAMES_CONFIG_OUTPUT -ErrorAction SilentlyContinue
}
$prefix = 'OPENCODE_CONFIG_CONTENT='
if (-not $captured.StartsWith($prefix, [StringComparison]::Ordinal)) { throw "Launcher child returned unexpected configuration: $captured" }
$output = $captured.Substring($prefix.Length)
$config = $output | ConvertFrom-Json
if (@($config.skills.paths).Count -ne 1 -or [string]$config.skills.paths[0] -ine $skillRoot) {
    throw "Launcher supplied an unexpected skill path: $output"
}
if (Test-Path Env:\OPENCODE_CONFIG_CONTENT) { throw 'Launcher leaked inline OpenCode configuration.' }
$existing = '{"model":"preserve"}'
$env:OPENCODE_CONFIG_CONTENT = $existing
$rejected = $false
try { & $launcher /d /c 'exit 0' } catch {
    $rejected = $_.Exception.Message -like '*requires OPENCODE_CONFIG_CONTENT to be unset*'
}
if (-not $rejected -or $env:OPENCODE_CONFIG_CONTENT -cne $existing) {
    throw 'Launcher replaced or accepted existing inline OpenCode configuration.'
}
`, quote(stackPath), quote(skillsRoot), quote(launcher), quote(filepath.Dir(fakeOpenCode)), quote(configOutput))
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
