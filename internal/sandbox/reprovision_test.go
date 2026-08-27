package sandbox

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRetainedRunPlanRequiresCompatibleExistingLaunchPlan(t *testing.T) {
	root := t.TempDir()
	dataDirectory := filepath.Join(root, "data")
	runID := "20260725-121936-0d9549e4"
	runDirectory := filepath.Join(dataDirectory, "runs", runID)
	inputDirectory := filepath.Join(runDirectory, "input")
	statusDirectory := filepath.Join(runDirectory, "status")
	cacheDirectory := filepath.Join(root, "cache")
	worktreeDirectory := filepath.Join(root, "worktrees")
	workspaceDirectory := filepath.Join(root, "workspace")
	for _, directory := range []string{inputDirectory, statusDirectory, filepath.Join(dataDirectory, "identity"), cacheDirectory, worktreeDirectory, workspaceDirectory} {
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
	worktreeDirectory, err = canonicalMappedDirectory(worktreeDirectory)
	if err != nil {
		t.Fatal(err)
	}
	workspaceDirectory, err = canonicalMappedDirectory(workspaceDirectory)
	if err != nil {
		t.Fatal(err)
	}
	terminal := testStableWindowsTerminalConfiguration()
	packages, err := resolveWingetPackagePlan(wingetPackageConfiguration{Remove: []string{}, Add: []string{}, Versions: map[string]string{}}, terminal)
	if err != nil {
		t.Fatal(err)
	}
	provisioning := provisioningPlan{
		CacheDirectory:    cacheDirectory,
		WorktreeDirectory: worktreeDirectory,
		MemoryMB:          4096,
		Packages:          packages,
		WindowsTerminal:   terminal,
		Workspaces: []workspacePlan{{
			Name:             "project",
			HostDirectory:    workspaceDirectory,
			GuestDirectory:   guestWorkspaceDirectory("project"),
			ProvisioningPath: filepath.Join(workspaceDirectory, projectConfigurationName, projectProvisioningName),
			Active:           true,
		}},
	}
	config, err := renderConfigWithWorktreeDirectory(inputDirectory, statusDirectory, cacheDirectory, worktreeDirectory, provisioning.Mounts, provisioning.Workspaces, 4096, provisioning.AudioOutput, provisioning.AudioInput)
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
	if plan.ID != runID || plan.DataDirectory != dataDirectory || plan.WorktreeDirectory != worktreeDirectory || len(plan.Workspaces) != 1 {
		t.Fatalf("retained plan = %#v", plan)
	}
	withOpenCode, err := resolveWingetPackagePlan(defaultWingetPackageConfiguration(), terminal)
	if err != nil {
		t.Fatal(err)
	}
	provisioning.Packages = withOpenCode
	provisioning.CredentialSync = credentialSyncConfiguration{OpenCode: true, GitHubCLI: true}
	plan, err = retainedRunPlan(active, provisioning, 4096)
	if err != nil || !plan.Packages.enabled(packageOpenCode) || plan.CredentialSync != provisioning.CredentialSync {
		t.Fatalf("retained dynamic plan = %#v, error = %v", plan, err)
	}
	legacyConfig := bytes.Replace(config, []byte(`,&#39;-AudioPlayback&#39;,&#39;Disabled&#39;`), nil, 1)
	if bytes.Equal(legacyConfig, config) {
		t.Fatal("current default-silent WSB has no explicit audio launch identity")
	}
	if err := os.WriteFile(configPath, legacyConfig, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := retainedRunPlan(active, provisioning, 4096); err == nil || !strings.Contains(err.Error(), "audio output") {
		t.Fatalf("legacy retained audio output plan error = %v", err)
	}
	if err := os.WriteFile(configPath, config, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := retainedRunPlan(active, provisioning, 8192); err == nil || !strings.Contains(err.Error(), "memory") || !strings.Contains(err.Error(), "differ from the ready Sandbox") {
		t.Fatalf("changed retained plan error = %v", err)
	}
	provisioning.WorktreeDirectory = ""
	if _, err := retainedRunPlan(active, provisioning, 4096); err == nil || !strings.Contains(err.Error(), "worktree directory") {
		t.Fatalf("changed retained worktree directory error = %v", err)
	}
	provisioning.WorktreeDirectory = worktreeDirectory
	provisioning.AudioOutput = true
	if _, err := retainedRunPlan(active, provisioning, 4096); err == nil || !strings.Contains(err.Error(), "audio output") {
		t.Fatalf("changed retained audio output selection error = %v", err)
	}
	provisioning.AudioOutput = false
	provisioning.AudioInput = true
	if _, err := retainedRunPlan(active, provisioning, 4096); err == nil || !strings.Contains(err.Error(), "audio input") {
		t.Fatalf("changed retained audio input selection error = %v", err)
	}
	provisioning.AudioInput = false
	provisioning.Tailscale = true
	if _, err := retainedRunPlan(active, provisioning, 4096); err == nil || !strings.Contains(err.Error(), "Tailscale identity selection differs") {
		t.Fatalf("changed retained Tailscale selection error = %v", err)
	}
	active.Tailscale = true
	plan, err = retainedRunPlan(active, provisioning, 4096)
	if err != nil || !plan.Tailscale {
		t.Fatalf("matching retained Tailscale plan = %#v, error = %v", plan, err)
	}
	provisioning.MobileSSHAuthorizedKeys = []string{testEd25519PublicKey(1)}
	if _, err := retainedRunPlan(active, provisioning, 4096); err == nil || !strings.Contains(err.Error(), "mobile SSH authorized keys") {
		t.Fatalf("changed retained mobile key selection error = %v", err)
	}
	if err := writeMobileSSHAuthorizedKeysInput(filepath.Join(runDirectory, "input"), provisioning.MobileSSHAuthorizedKeys); err != nil {
		t.Fatal(err)
	}
	plan, err = retainedRunPlan(active, provisioning, 4096)
	if err != nil || !sameMobileSSHAuthorizedKeys(plan.MobileSSHAuthorizedKeys, provisioning.MobileSSHAuthorizedKeys) {
		t.Fatalf("matching retained mobile keys plan = %#v, error = %v", plan.MobileSSHAuthorizedKeys, err)
	}
}

func TestCredentialSyncRunsBeforeUnchangedRetainedProvisioning(t *testing.T) {
	root := t.TempDir()
	input := filepath.Join(root, "input")
	previous := filepath.Join(input, "provisioning", wingetPackagePlanFileName)
	current := filepath.Join(root, "snapshot", wingetPackagePlanFileName)
	for _, directory := range []string{filepath.Dir(previous), filepath.Dir(current)} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	writeTestFile(t, previous, `{"schemaVersion":1,"defaults":[],"additions":[]}`)
	writeTestFile(t, current, `{"schemaVersion":1,"defaults":[],"additions":[]}`)
	plan := runPlan{InputDirectory: input}
	snapshot := provisioningSnapshot{PackagePlanPath: current}
	credentials := credentialSyncConfiguration{OpenCode: true, ClaudeCode: true, Codex: true, GitHubCLI: true, Pi: true, TradingView: true}
	if !shouldSyncCredentialsBeforeRetainedProvisioning(plan, snapshot, credentials) {
		t.Fatal("unchanged retained package plan did not schedule early credential sync")
	}
	if shouldSyncCredentialsBeforeRetainedProvisioning(plan, snapshot, credentialSyncConfiguration{}) {
		t.Fatal("disabled credentials scheduled early credential sync")
	}
	writeTestFile(t, current, `{"schemaVersion":1,"defaults":[{"id":"SST.opencode","version":""}],"additions":[]}`)
	if shouldSyncCredentialsBeforeRetainedProvisioning(plan, snapshot, credentials) {
		t.Fatal("changed retained package plan scheduled credential sync before package provisioning")
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
		provisioningProcessName:              "process",
		wingetPackagePlanFileName:            "packages",
		toolVersionPlanFileName:              "tools",
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
		ProcessOwnerPath:        filepath.Join(directory, provisioningProcessName),
		ProjectScriptsDirectory: projects,
		PackagePlanPath:         filepath.Join(directory, wingetPackagePlanFileName),
		ToolVersionPlanPath:     filepath.Join(directory, toolVersionPlanFileName),
		WorkspaceManifestPath:   filepath.Join(directory, workspaceManifestName),
		Workspaces: []workspacePlan{
			{Name: "one", ProvisioningPath: filepath.Join(projects, "one.ps1")},
			{Name: "plain"},
		},
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
	valid := []byte(`{"schemaVersion":2,"archiveSha256":"abc","projectCount":3,"explorerRestartScheduled":true,"explorerRestartId":"20260801-080000-1234abcd","explorerRestartTaskName":"HerdrSandbox-ExplorerRestart-20260801-080000-1234abcd"}`)
	result, err := decodeReprovisionResult(valid)
	if err != nil || result.ProjectCount != 3 || !result.ExplorerRestartScheduled {
		t.Fatalf("result = %#v, error = %v", result, err)
	}
	for _, invalid := range [][]byte{
		[]byte(`{"schemaVersion":2,"archiveSha256":"abc","projectCount":3,"explorerRestartScheduled":true,"explorerRestartId":"id","explorerRestartTaskName":"task","extra":true}`),
		[]byte(`{"schemaVersion":2,"archiveSha256":"abc","projectCount":3,"explorerRestartId":"id","explorerRestartTaskName":"task"}`),
		append(append([]byte{}, valid...), []byte(` {}`)...),
	} {
		if _, err := decodeReprovisionResult(invalid); err == nil {
			t.Fatalf("invalid result decoded: %s", invalid)
		}
	}
}

func TestExplorerRestartStatusContract(t *testing.T) {
	restartID := "20260801-080000-1234abcd"
	taskName := "HerdrSandbox-ExplorerRestart-" + restartID
	valid := []explorerRestartStatus{
		{SchemaVersion: 1, RestartID: restartID, TaskName: taskName, State: explorerRestartStatusPending, SessionID: 1, StoppedPIDs: []int{10}},
		{SchemaVersion: 1, RestartID: restartID, TaskName: taskName, State: explorerRestartStatusSucceeded, SessionID: 1, StoppedPIDs: []int{10}, StartedPIDs: []int{11}},
		{SchemaVersion: 1, RestartID: restartID, TaskName: taskName, State: explorerRestartStatusFailed, SessionID: 1, Message: "restart failed"},
	}
	for _, status := range valid {
		if err := status.validate(); err != nil {
			t.Fatalf("valid Explorer restart status %#v: %v", status, err)
		}
	}
	invalid := []explorerRestartStatus{
		{SchemaVersion: 2, RestartID: restartID, TaskName: taskName, State: explorerRestartStatusPending, SessionID: 1, StoppedPIDs: []int{10}},
		{SchemaVersion: 1, RestartID: restartID, TaskName: taskName, State: explorerRestartStatusPending, SessionID: 0, StoppedPIDs: []int{10}},
		{SchemaVersion: 1, RestartID: restartID, TaskName: taskName, State: explorerRestartStatusPending, SessionID: 1},
		{SchemaVersion: 1, RestartID: restartID, TaskName: taskName, State: explorerRestartStatusSucceeded, SessionID: 1, StoppedPIDs: []int{10}, StartedPIDs: []int{10}},
		{SchemaVersion: 1, RestartID: restartID, TaskName: taskName, State: explorerRestartStatusFailed, SessionID: 1},
		{SchemaVersion: 1, RestartID: restartID, TaskName: taskName, State: "unknown", SessionID: 1, StoppedPIDs: []int{10}},
	}
	for _, status := range invalid {
		if err := status.validate(); err == nil {
			t.Fatalf("invalid Explorer restart status succeeded: %#v", status)
		}
	}
	path := filepath.Join(t.TempDir(), "explorer-restart.json")
	data := []byte(`{"schemaVersion":1,"restartId":"20260801-080000-1234abcd","taskName":"HerdrSandbox-ExplorerRestart-20260801-080000-1234abcd","state":"succeeded","sessionId":1,"stoppedPids":[10],"startedPids":[11],"message":""}`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	decoded, found, err := readOptionalStatus[explorerRestartStatus](path)
	if err != nil || !found || decoded.RestartID != restartID {
		t.Fatalf("read Explorer restart status = %#v, found=%t, error=%v", decoded, found, err)
	}
}

func TestBuildReprovisionLauncherUsesBoundedArchiveInputAndHiddenGuestState(t *testing.T) {
	digest := strings.Repeat("a", 64)
	restartID := "20260801-080000-1234abcd"
	taskName := "HerdrSandbox-ExplorerRestart-" + restartID
	launcher := buildReprovisionLauncher(digest, 1234, 2, restartID, taskName)
	for _, required := range []string{
		"[Console]::OpenStandardInput()",
		"$expectedArchiveLength = [long]1234",
		"Retained provisioning archive SHA-256 mismatch",
		"function Remove-GuestArchiveStaging",
		"staging cleanup did not remove all input",
		`C:\HerdrSandbox\staging`,
		"reprovision-aaaaaaaaaaaaaaaa",
		"Assert-GuestArchiveTree",
		"New-Item -ItemType Directory -Path $projectsDirectory -Force",
		`$env:HERDR_SANDBOX_STATUS_DIRECTORY = 'C:\SandboxStatus'`,
		"HERDR_SANDBOX_EXPLORER_RESTART_SCHEDULED",
		"explorerRestartScheduled = $explorerRestartScheduled",
		restartID,
		taskName,
		`-UserProvisioningPath (Join-Path $expanded 'user.ps1')`,
		`-ProcessOwnerPath (Join-Path $expanded 'provisioning-process.cs')`,
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
