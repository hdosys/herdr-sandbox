package sandbox

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestVisualStudioLayoutAssetIsEmbedded(t *testing.T) {
	if len(bytes.TrimSpace(visualStudioLayoutScript)) == 0 {
		t.Fatal("Visual Studio layout script is empty")
	}
	for _, required := range []string{
		"https://aka.ms/vs/17/release/channel",
		"https://aka.ms/vs/17/release/vs_buildtools.exe",
		"Microsoft.VisualStudio.Component.VC.Tools.x86.x64",
		"Microsoft.VisualStudio.Component.Windows11SDK.26100",
		`bootstrapper\vs_BuildTools.exe`,
		"function Assert-HerdrHostCacheTree",
		"Assert-HerdrHostCacheTree -Path $selectedSlot",
		"complete.json",
	} {
		if !strings.Contains(string(visualStudioLayoutScript), required) {
			t.Fatalf("Visual Studio layout script is missing %q", required)
		}
	}
	for _, excluded := range []string{
		"Microsoft.VisualStudio.Workload.VCTools",
		"--includeRecommended",
		"--includeOptional",
	} {
		if strings.Contains(string(visualStudioLayoutScript), excluded) {
			t.Fatalf("Visual Studio layout script still contains broad selection %q", excluded)
		}
	}
	if strings.Contains(string(visualStudioLayoutScript), "-FilePath $downloadedBootstrapper") {
		t.Fatal("Visual Studio layout script executes its temporary bootstrapper download")
	}
}

func TestBoundedVisualStudioOutputRetainsTail(t *testing.T) {
	capture := &boundedOutputCapture{}
	value := bytes.Repeat([]byte("x"), maximumVisualStudioOutput+100)
	if _, err := capture.Write(value); err != nil {
		t.Fatal(err)
	}
	if len(capture.data) != maximumVisualStudioOutput {
		t.Fatalf("captured bytes = %d", len(capture.data))
	}
	if !bytes.Equal(capture.data, value[len(value)-maximumVisualStudioOutput:]) {
		t.Fatal("capture did not retain output tail")
	}
}

func TestPublishVisualStudioBootstrapperReplacesExistingFileInWindowsPowerShell51(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows PowerShell 5.1 regression")
	}
	directory := t.TempDir()
	scriptPath := filepath.Join(directory, "visual-studio-layout.ps1")
	source := filepath.Join(directory, "source.exe")
	destination := filepath.Join(directory, "destination.exe")
	if err := os.WriteFile(scriptPath, visualStudioLayoutScript, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(source, []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(destination, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	quote := func(value string) string { return strings.ReplaceAll(value, "'", "''") }
	script := fmt.Sprintf(`$ErrorActionPreference = 'Stop'
$tokens = $null
$errors = $null
$ast = [System.Management.Automation.Language.Parser]::ParseFile('%s', [ref]$tokens, [ref]$errors)
if ($errors.Count -ne 0) { throw 'Visual Studio layout script has parse errors.' }
$definition = $ast.Find({ param($node) $node -is [System.Management.Automation.Language.FunctionDefinitionAst] -and $node.Name -eq 'Publish-HerdrHostVisualStudioBootstrapper' }, $true)
if ($null -eq $definition) { throw 'Visual Studio bootstrapper publication function is missing.' }
Invoke-Expression $definition.Extent.Text
function Assert-HerdrHostCachePath { param([string]$Path) }
function Assert-HerdrHostVisualStudioBootstrapper {
    param([string]$Path, [string]$ExpectedSHA256)
    if ([IO.File]::ReadAllText($Path) -cne $ExpectedSHA256) { throw "Unexpected fixture contents: $Path" }
}
$published = Publish-HerdrHostVisualStudioBootstrapper -Source '%s' -Destination '%s' -ExpectedSHA256 'new'
if ($published -cne '%s' -or [IO.File]::ReadAllText('%s') -cne 'new') {
    throw 'Visual Studio bootstrapper replacement did not publish the staged file.'
}
$files = [IO.Directory]::GetFiles('%s')
if ($files.Count -ne 3) { throw "Visual Studio bootstrapper replacement left transient files: $files" }
exit 0
`, quote(scriptPath), quote(source), quote(destination), quote(destination), quote(destination), quote(directory))
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	powerShell := mustWindowsPowerShellPath(t)
	command := hiddenCommandContext(ctx, powerShell, "-NoLogo", "-NoProfile", "-NonInteractive", "-EncodedCommand", encodePowerShell(script))
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("Visual Studio bootstrapper replacement regression: %v: %s", err, output)
	}
}

func TestPrepareVisualStudioLayoutNoopsWithoutRequirement(t *testing.T) {
	err := prepareVisualStudioLayout(context.Background(), runPlan{}, io.Discard)
	if err != nil {
		t.Fatalf("prepareVisualStudioLayout: %v", err)
	}
}

func TestWorkspaceRequirementsIncludeNonActiveProjects(t *testing.T) {
	plan := runPlan{Workspaces: []workspacePlan{
		{Name: "herdr-sandbox", GuestDirectory: `C:\Workspaces\herdr-sandbox`, Active: true},
		{Name: "native", GuestDirectory: `C:\Workspaces\native`, Stacks: []projectStack{stackCpp}},
	}}
	applyWorkspaceRequirements(&plan)
	if plan.ActiveWorkspace != `C:\Workspaces\herdr-sandbox` {
		t.Fatalf("active workspace = %q", plan.ActiveWorkspace)
	}
	if !plan.RequiresVisualStudioLayout {
		t.Fatal("Visual Studio requirement from non-active workspace was lost")
	}
}

func TestVisualStudioRequirementIncludesCppAndRustStacksOnly(t *testing.T) {
	for _, stacks := range [][]projectStack{{stackCpp}, {stackRustMSVC}, {stackCpp, stackJava}} {
		if !stacksRequireVisualStudioLayout(stacks) {
			t.Fatalf("Visual Studio requirement missing for %v", stacks)
		}
	}
	if stacksRequireVisualStudioLayout([]projectStack{stackJava, stackGo}) {
		t.Fatal("unrelated stacks unexpectedly require Visual Studio")
	}
}

func TestWorkspaceRequirementsFocusFirstGlobalWorkspaceWithoutActiveProject(t *testing.T) {
	plan := runPlan{Workspaces: []workspacePlan{
		{Name: "alpha", GuestDirectory: `C:\Workspaces\alpha`},
		{Name: "zeta", GuestDirectory: `C:\Workspaces\zeta`},
	}}
	applyWorkspaceRequirements(&plan)
	if plan.ActiveWorkspace != `C:\Workspaces\alpha` {
		t.Fatalf("active workspace = %q", plan.ActiveWorkspace)
	}
}
