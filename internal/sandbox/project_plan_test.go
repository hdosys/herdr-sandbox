package sandbox

import (
	"context"
	"runtime"
	"strings"
	"testing"
)

func TestInspectProjectProvisioningPlanUsesDirectCallsWithoutExecutingScripts(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows PowerShell 5.1 AST regression")
	}
	runDirectory := t.TempDir()
	projectsDirectory := t.TempDir()
	writeTestFile(t, projectsDirectory+`\alpha.ps1`, `$dynamic = 'Install-RustMSVCStack'
& $dynamic
& 'Install-NodeStack'
Set-Alias selectedStack Install-RustMSVCStack
selectedStack
Invoke-Expression 'Install-RustMSVCStack'
. '.\hidden.ps1'
Install-GoStack
Install-GoStack -Version '1.26.5'
throw 'the AST adapter must not execute project code'
`)
	writeTestFile(t, projectsDirectory+`\herdr.ps1`, `function Install-ProjectTools {
    Install-CargoNextest
}
Install-RustMSVCStack -ProjectDirectory 'C:\Workspaces\herdr'
Install-ZigStack -Version '0.15.2'
Install-Just
`)
	workspaces := []workspacePlan{{Name: "alpha"}, {Name: "herdr"}}
	got, err := inspectProjectProvisioningPlan(context.Background(), runDirectory, projectsDirectory, workspaces)
	if err != nil {
		t.Fatalf("inspectProjectProvisioningPlan: %v", err)
	}
	if strings.Join(projectStackStrings(got[0].Stacks), "|") != "go" {
		t.Fatalf("alpha stacks = %v", got[0].Stacks)
	}
	if strings.Join(projectStackStrings(got[1].Stacks), "|") != "cargo-nextest|just|rust-msvc|zig" {
		t.Fatalf("herdr stacks = %v", got[1].Stacks)
	}
}

func TestInspectProjectProvisioningPlanRejectsParseErrors(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows PowerShell 5.1 AST regression")
	}
	runDirectory := t.TempDir()
	projectsDirectory := t.TempDir()
	writeTestFile(t, projectsDirectory+`\broken.ps1`, "function Broken {\n")
	_, err := inspectProjectProvisioningPlan(context.Background(), runDirectory, projectsDirectory, []workspacePlan{{Name: "broken"}})
	if err == nil || !strings.Contains(err.Error(), "parse failed") {
		t.Fatalf("parse error = %v", err)
	}
}

func TestDecodeProjectProvisioningPlanIsStrict(t *testing.T) {
	workspaces := []workspacePlan{{Name: "alpha"}}
	valid := []byte(`{"schemaVersion":1,"projects":[{"name":"alpha","stacks":["go"]}]}`)
	got, err := decodeProjectProvisioningPlan(valid, workspaces)
	if err != nil || len(got) != 1 || len(got[0].Stacks) != 1 || got[0].Stacks[0] != stackGo {
		t.Fatalf("valid plan = %#v, %v", got, err)
	}
	for _, invalid := range [][]byte{
		[]byte(`{"schemaVersion":2,"projects":[{"name":"alpha","stacks":[]}]}`),
		[]byte(`{"schemaVersion":1,"projects":[{"name":"other","stacks":[]}]}`),
		[]byte(`{"schemaVersion":1,"projects":[{"name":"alpha","stacks":["unknown"]}]}`),
		[]byte(`{"schemaVersion":1,"projects":[{"name":"alpha","stacks":["go","go"]}]}`),
		[]byte(`{"schemaVersion":1,"projects":[{"name":"alpha","stacks":[],"extra":true}]}`),
		[]byte(`{"schemaVersion":1,"projects":[{"name":"alpha","stacks":[]}]} {}`),
	} {
		if _, err := decodeProjectProvisioningPlan(invalid, workspaces); err == nil {
			t.Fatalf("invalid plan unexpectedly decoded: %s", invalid)
		}
	}
}

func projectStackStrings(stacks []projectStack) []string {
	values := make([]string, len(stacks))
	for index, stack := range stacks {
		values[index] = string(stack)
	}
	return values
}
