package sandbox

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRetainedRunPlanRequiresExactExistingLaunchPlan(t *testing.T) {
	root := t.TempDir()
	dataDirectory := filepath.Join(root, "data")
	runID := "20260725-121936-0d9549e4"
	runDirectory := filepath.Join(dataDirectory, "runs", runID)
	inputDirectory := filepath.Join(runDirectory, "input")
	statusDirectory := filepath.Join(runDirectory, "status")
	cacheDirectory := filepath.Join(root, "cache")
	workspaceDirectory := filepath.Join(root, "workspace")
	for _, directory := range []string{inputDirectory, statusDirectory, filepath.Join(dataDirectory, "identity"), cacheDirectory, workspaceDirectory} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	configurationDirectory := filepath.Join(workspaceDirectory, projectConfigurationName)
	if err := os.MkdirAll(configurationDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configurationDirectory, projectProvisioningName), []byte("Write-Output 'fixture'\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	inputDirectory, err := canonicalMappedDirectory(inputDirectory)
	if err != nil {
		t.Fatal(err)
	}
	statusDirectory, err = canonicalMappedDirectory(statusDirectory)
	if err != nil {
		t.Fatal(err)
	}
	cacheDirectory, err = canonicalMappedDirectory(cacheDirectory)
	if err != nil {
		t.Fatal(err)
	}
	workspaceDirectory, err = canonicalMappedDirectory(workspaceDirectory)
	if err != nil {
		t.Fatal(err)
	}
	terminal := testStableWindowsTerminalConfiguration()
	packages, err := resolveWingetPackagePlan(defaultWingetPackageConfiguration(), terminal)
	if err != nil {
		t.Fatal(err)
	}
	provisioning := provisioningPlan{
		CacheDirectory:  cacheDirectory,
		MemoryMB:        4096,
		Packages:        packages,
		WindowsTerminal: terminal,
		Workspaces: []workspacePlan{{
			Name:             "project",
			HostDirectory:    workspaceDirectory,
			GuestDirectory:   guestWorkspaceDirectory("project"),
			ProvisioningPath: filepath.Join(workspaceDirectory, projectConfigurationName, projectProvisioningName),
			Active:           true,
		}},
	}
	config, err := renderConfig(inputDirectory, statusDirectory, cacheDirectory, provisioning.Workspaces, 4096, provisioning.Audio)
	if err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(runDirectory, applicationName+".wsb")
	if err := os.WriteFile(configPath, config, 0o600); err != nil {
		t.Fatal(err)
	}
	active := activeSession{RunID: runID, ConfigPath: configPath, ExecutablePath: filepath.Join(root, "WindowsSandbox.exe")}
	plan, err := retainedRunPlan(active, provisioning, 4096)
	if err != nil {
		t.Fatalf("retainedRunPlan: %v", err)
	}
	if plan.ID != runID || plan.DataDirectory != dataDirectory || len(plan.Workspaces) != 1 {
		t.Fatalf("retained plan = %#v", plan)
	}
	legacyConfig := bytes.Replace(config, []byte(`,&#39;-AudioPlayback&#39;,&#39;Disabled&#39;`), nil, 1)
	if bytes.Equal(legacyConfig, config) {
		t.Fatal("current default-silent WSB has no explicit audio launch identity")
	}
	if err := os.WriteFile(configPath, legacyConfig, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := retainedRunPlan(active, provisioning, 4096); err == nil || !strings.Contains(err.Error(), "audio") {
		t.Fatalf("legacy retained audio plan error = %v", err)
	}
	if err := os.WriteFile(configPath, config, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := retainedRunPlan(active, provisioning, 8192); err == nil || !strings.Contains(err.Error(), "memory") || !strings.Contains(err.Error(), "differ from the ready Sandbox") {
		t.Fatalf("changed retained plan error = %v", err)
	}
	provisioning.Audio = true
	if _, err := retainedRunPlan(active, provisioning, 4096); err == nil || !strings.Contains(err.Error(), "audio") {
		t.Fatalf("changed retained audio selection error = %v", err)
	}
	provisioning.Audio = false
	provisioning.Tailscale = true
	if _, err := retainedRunPlan(active, provisioning, 4096); err == nil || !strings.Contains(err.Error(), "Tailscale identity selection differs") {
		t.Fatalf("changed retained Tailscale selection error = %v", err)
	}
	active.Tailscale = true
	plan, err = retainedRunPlan(active, provisioning, 4096)
	if err != nil || !plan.Tailscale {
		t.Fatalf("matching retained Tailscale plan = %#v, error = %v", plan, err)
	}
}

func TestBuildReprovisionArchiveContainsOnlyCurrentProvisioningSnapshot(t *testing.T) {
	directory := t.TempDir()
	projects := filepath.Join(directory, "projects")
	if err := os.MkdirAll(projects, 0o700); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		baseProvisioningName:                 "base",
		stackProvisioningName:                "stacks",
		userProvisioningName:                 "user",
		wingetPackagePlanFileName:            "packages",
		workspaceManifestName:                "workspaces",
		filepath.Join("projects", "one.ps1"): "project",
	}
	for name, contents := range files {
		if err := os.WriteFile(filepath.Join(directory, name), []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	snapshot := provisioningSnapshot{
		Directory:               directory,
		ProjectScriptsDirectory: projects,
		PackagePlanPath:         filepath.Join(directory, wingetPackagePlanFileName),
		WorkspaceManifestPath:   filepath.Join(directory, workspaceManifestName),
		Workspaces:              []workspacePlan{{Name: "one"}},
	}
	data, err := buildReprovisionArchive(snapshot)
	if err != nil {
		t.Fatalf("buildReprovisionArchive: %v", err)
	}
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	entries := map[string]bool{}
	for _, file := range reader.File {
		entries[file.Name] = true
	}
	for expected := range files {
		expected = filepath.ToSlash(expected)
		if !entries[expected] {
			t.Fatalf("archive is missing %s: %#v", expected, entries)
		}
	}
	if len(entries) != len(files) {
		t.Fatalf("archive entries = %#v", entries)
	}
}

func TestDecodeReprovisionResultIsStrict(t *testing.T) {
	valid := []byte(`{"schemaVersion":1,"archiveSha256":"abc","projectCount":3}`)
	result, err := decodeReprovisionResult(valid)
	if err != nil || result.ProjectCount != 3 {
		t.Fatalf("result = %#v, error = %v", result, err)
	}
	for _, invalid := range [][]byte{
		[]byte(`{"schemaVersion":1,"archiveSha256":"abc","projectCount":3,"extra":true}`),
		append(append([]byte{}, valid...), []byte(` {}`)...),
	} {
		if _, err := decodeReprovisionResult(invalid); err == nil {
			t.Fatalf("invalid result decoded: %s", invalid)
		}
	}
}

func TestBuildReprovisionLauncherUsesBoundedArchiveInputAndHiddenGuestState(t *testing.T) {
	digest := strings.Repeat("a", 64)
	launcher := buildReprovisionLauncher(digest, 1234, 2)
	for _, required := range []string{
		"[Console]::OpenStandardInput()",
		"$expectedArchiveLength = [long]1234",
		"Retained provisioning archive SHA-256 mismatch",
		"function Remove-GuestArchiveStaging",
		"staging cleanup did not remove all input",
		`C:\HerdrSandbox\staging`,
		"reprovision-aaaaaaaaaaaaaaaa",
		"Assert-GuestArchiveTree",
		`$env:HERDR_SANDBOX_STATUS_DIRECTORY = 'C:\SandboxStatus'`,
		`-UserProvisioningPath (Join-Path $expanded 'user.ps1')`,
		"*>&1",
	} {
		if !strings.Contains(launcher, required) {
			t.Fatalf("retained provisioning launcher is missing %q", required)
		}
	}
	if strings.Contains(launcher, "Remove-Item -LiteralPath $archive -Force -ErrorAction SilentlyContinue") ||
		strings.Contains(launcher, "Remove-Item -LiteralPath $expanded -Recurse -Force -ErrorAction SilentlyContinue") {
		t.Fatal("retained provisioning launcher silently ignores staging cleanup")
	}
	if strings.Contains(launcher, "$env:TEMP") {
		t.Fatal("retained provisioning launcher stages input outside the canonical guest root")
	}
}
