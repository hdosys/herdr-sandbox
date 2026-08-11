package sandbox

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"
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
