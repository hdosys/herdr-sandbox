package sandbox

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

func TestDescribeWSBLaunchDifferencesNamesChangedFields(t *testing.T) {
	root := t.TempDir()
	input := filepath.Join(root, "input")
	status := filepath.Join(root, "status")
	cache := filepath.Join(root, "cache")
	workspace := workspacePlan{
		Name:           "project",
		HostDirectory:  filepath.Join(root, "project"),
		GuestDirectory: guestWorkspaceDirectory("project"),
	}
	baseline, err := renderConfig(input, status, cache, nil, []workspacePlan{workspace}, 4096, false, false)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name        string
		memory      int
		audioOutput bool
		audioInput  bool
		cache       string
		workspace   workspacePlan
		want        string
	}{
		{name: "memory", memory: 8192, cache: cache, workspace: workspace, want: "memory"},
		{name: "audio output", memory: 4096, audioOutput: true, cache: cache, workspace: workspace, want: "audio output"},
		{name: "audio input", memory: 4096, audioInput: true, cache: cache, workspace: workspace, want: "audio input"},
		{name: "cache", memory: 4096, cache: filepath.Join(root, "other-cache"), workspace: workspace, want: "cache"},
		{name: "workspaces", memory: 4096, cache: cache, workspace: workspacePlan{Name: "other", HostDirectory: filepath.Join(root, "other"), GuestDirectory: guestWorkspaceDirectory("other")}, want: "workspaces"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			changed, err := renderConfig(input, status, test.cache, nil, []workspacePlan{test.workspace}, test.memory, test.audioOutput, test.audioInput)
			if err != nil {
				t.Fatal(err)
			}
			differences, err := describeWSBLaunchDifferences(baseline, changed)
			if err != nil || !strings.Contains(strings.Join(differences, ","), test.want) {
				t.Fatalf("differences = %v, error = %v", differences, err)
			}
		})
	}
}

func TestDescribeWSBLaunchDifferencesNamesFolderMountChanges(t *testing.T) {
	root := t.TempDir()
	input := filepath.Join(root, "input")
	status := filepath.Join(root, "status")
	cache := filepath.Join(root, "cache")
	mount := mountPlan{Name: "worktrees", HostDirectory: filepath.Join(root, "worktrees"), GuestDirectory: guestMountDirectory("worktrees")}
	baseline, err := renderConfig(input, status, cache, nil, nil, 4096, false, false)
	if err != nil {
		t.Fatal(err)
	}
	changed, err := renderConfig(input, status, cache, []mountPlan{mount}, nil, 4096, false, false)
	if err != nil {
		t.Fatal(err)
	}
	differences, err := describeWSBLaunchDifferences(baseline, changed)
	if err != nil || strings.Join(differences, ",") != "folder mounts" {
		t.Fatalf("differences = %v, error = %v", differences, err)
	}

	mount.ReadOnly = true
	readOnly, err := renderConfig(input, status, cache, []mountPlan{mount}, nil, 4096, false, false)
	if err != nil {
		t.Fatal(err)
	}
	differences, err = describeWSBLaunchDifferences(changed, readOnly)
	if err != nil || strings.Join(differences, ",") != "folder mounts" {
		t.Fatalf("access differences = %v, error = %v", differences, err)
	}
}

func TestDescribeWSBLaunchDifferencesAllowsEquivalentMappingCaseAndOrder(t *testing.T) {
	root := t.TempDir()
	workspaces := []workspacePlan{
		{Name: "one", HostDirectory: filepath.Join(root, "One"), GuestDirectory: guestWorkspaceDirectory("one")},
		{Name: "two", HostDirectory: filepath.Join(root, "Two"), GuestDirectory: guestWorkspaceDirectory("two")},
	}
	actual, err := renderConfig(filepath.Join(root, "Input"), filepath.Join(root, "Status"), filepath.Join(root, "Cache"), nil,
		workspaces, 4096, false, false)
	if err != nil {
		t.Fatal(err)
	}
	workspaces[0].HostDirectory = strings.ToLower(workspaces[0].HostDirectory)
	workspaces[1].HostDirectory = strings.ToLower(workspaces[1].HostDirectory)
	expected, err := renderConfig(strings.ToLower(filepath.Join(root, "Input")), strings.ToLower(filepath.Join(root, "Status")), strings.ToLower(filepath.Join(root, "Cache")), nil,
		[]workspacePlan{workspaces[1], workspaces[0]}, 4096, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(actual, expected) {
		t.Fatal("mapping path case fixture did not change the WSB bytes")
	}
	differences, err := describeWSBLaunchDifferences(actual, expected)
	if err != nil || len(differences) != 0 {
		t.Fatalf("differences = %v, error = %v", differences, err)
	}
}

func TestDescribeWSBLaunchDifferencesFallsBackForUnknownContractDrift(t *testing.T) {
	root := t.TempDir()
	config, err := renderConfig(filepath.Join(root, "input"), filepath.Join(root, "status"), filepath.Join(root, "cache"), nil,
		[]workspacePlan{{Name: "project", HostDirectory: filepath.Join(root, "project"), GuestDirectory: guestWorkspaceDirectory("project")}}, 4096, false, false)
	if err != nil {
		t.Fatal(err)
	}
	changed := []byte(strings.Replace(string(config), "  <Networking>Enable</Networking>", "  <Networking>Enable</Networking>\n  <Unexpected>true</Unexpected>", 1))
	differences, err := describeWSBLaunchDifferences(changed, config)
	if err != nil || strings.Join(differences, ",") != "launch contract" {
		t.Fatalf("differences = %v, error = %v", differences, err)
	}
}

func TestDescribeWSBLaunchDifferencesReportsAudioAndOtherCommandDrift(t *testing.T) {
	root := t.TempDir()
	workspaces := []workspacePlan{{Name: "project", HostDirectory: filepath.Join(root, "project"), GuestDirectory: guestWorkspaceDirectory("project")}}
	expected, err := renderConfig(filepath.Join(root, "input"), filepath.Join(root, "status"), filepath.Join(root, "cache"), nil, workspaces, 4096, false, false)
	if err != nil {
		t.Fatal(err)
	}
	actual, err := renderConfig(filepath.Join(root, "input"), filepath.Join(root, "status"), filepath.Join(root, "cache"), nil, workspaces, 4096, true, false)
	if err != nil {
		t.Fatal(err)
	}
	actual = []byte(strings.Replace(string(actual), `C:\SandboxBootstrap\bootstrap.ps1`, `C:\SandboxBootstrap\changed.ps1`, 1))
	differences, err := describeWSBLaunchDifferences(actual, expected)
	if err != nil || strings.Join(differences, ",") != "audio output,launch contract" {
		t.Fatalf("differences = %v, error = %v", differences, err)
	}
}
