package sandbox

import (
	"encoding/xml"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestRenderConfigUsesNarrowMappingsAndPowerShell(t *testing.T) {
	workspaces := []workspacePlan{{Name: "one", HostDirectory: `D:\Projects\one`, GuestDirectory: `C:\Workspaces\one`}}
	encoded, err := renderConfig(`C:\Runs\one\input`, `C:\Runs\one\status`, `E:\herdr-sandbox-cache`, nil, workspaces, 8192, false, false)
	if err != nil {
		t.Fatalf("renderConfig: %v", err)
	}

	var config wsbConfiguration
	if err := xml.Unmarshal(encoded, &config); err != nil {
		t.Fatalf("unmarshal config: %v", err)
	}
	if config.Networking != "Enable" || config.MemoryInMB != 8192 {
		t.Fatalf("runtime config = networking %q, memory %d", config.Networking, config.MemoryInMB)
	}
	if config.VGPU != "Enable" || config.ClipboardRedirection != "Enable" || config.AudioInput != "Disable" {
		t.Fatalf("isolation config = vGPU %q, clipboard %q, audio input %q", config.VGPU, config.ClipboardRedirection, config.AudioInput)
	}
	if len(config.MappedFolders.Folders) != 4 {
		t.Fatalf("mapped folders = %#v", config.MappedFolders.Folders)
	}
	if !config.MappedFolders.Folders[0].ReadOnly {
		t.Fatal("bootstrap input mapping is writable")
	}
	if config.MappedFolders.Folders[1].ReadOnly {
		t.Fatal("project mapping is read-only")
	}
	if config.MappedFolders.Folders[2].ReadOnly {
		t.Fatal("status mapping is read-only")
	}
	if cache := config.MappedFolders.Folders[3]; cache.ReadOnly || cache.HostFolder != `E:\herdr-sandbox-cache` || cache.SandboxFolder != guestCacheDirectory {
		t.Fatalf("cache mapping = %#v", cache)
	}
	if !strings.HasPrefix(config.LogonCommand.Command, "powershell.exe ") {
		t.Fatalf("logon command = %q", config.LogonCommand.Command)
	}
	for _, required := range []string{"Start-Process", "-WindowStyle Normal", "-Wait", "'-NoExit'", "'-ConfigurationHandoffTimeoutMinutes'", guestBootstrapScript} {
		if !strings.Contains(config.LogonCommand.Command, required) {
			t.Fatalf("visible bootstrap command is missing %q: %s", required, config.LogonCommand.Command)
		}
	}
	for _, forbidden := range []string{"cmd.exe", ".cmd", ".bat"} {
		if strings.Contains(strings.ToLower(config.LogonCommand.Command), forbidden) {
			t.Fatalf("logon command contains %q: %s", forbidden, config.LogonCommand.Command)
		}
	}
	if strings.Count(config.LogonCommand.Command, "'-AudioPlayback','Disabled'") != 1 || strings.Contains(config.LogonCommand.Command, "'-AudioPlayback','Enabled'") {
		t.Fatalf("default-silent logon command has the wrong explicit audio identity: %s", config.LogonCommand.Command)
	}
	if strings.Count(config.LogonCommand.Command, "'-AudioInput','Disabled'") != 1 || strings.Contains(config.LogonCommand.Command, "'-AudioInput','Enabled'") {
		t.Fatalf("default microphone logon command has the wrong explicit audio identity: %s", config.LogonCommand.Command)
	}
}

func TestRenderConfigMapsNamedFoldersWithExplicitAccessOutsideWorkspaces(t *testing.T) {
	mounts := []mountPlan{
		{Name: "reference", HostDirectory: `D:\Reference`, GuestDirectory: `C:\Mounts\reference`, ReadOnly: true},
		{Name: "worktrees", HostDirectory: `E:\Worktrees`, GuestDirectory: `C:\Mounts\worktrees`, ReadOnly: false},
	}
	workspaces := []workspacePlan{{Name: "project", HostDirectory: `F:\Project`, GuestDirectory: `C:\Workspaces\project`}}
	encoded, err := renderConfig(`C:\Runs\one\input`, `C:\Runs\one\status`, `G:\cache`, mounts, workspaces, 4096, false, false)
	if err != nil {
		t.Fatal(err)
	}
	var config wsbConfiguration
	if err := xml.Unmarshal(encoded, &config); err != nil {
		t.Fatal(err)
	}
	if len(config.MappedFolders.Folders) != 6 {
		t.Fatalf("mapped folders = %#v", config.MappedFolders.Folders)
	}
	for index, expected := range []wsbMappedFolder{
		{HostFolder: `D:\Reference`, SandboxFolder: `C:\Mounts\reference`, ReadOnly: true},
		{HostFolder: `E:\Worktrees`, SandboxFolder: `C:\Mounts\worktrees`, ReadOnly: false},
	} {
		if actual := config.MappedFolders.Folders[index+1]; actual != expected {
			t.Fatalf("folder mount %d = %#v, want %#v", index, actual, expected)
		}
	}
	workspace := config.MappedFolders.Folders[3]
	if workspace.SandboxFolder != `C:\Workspaces\project` || workspace.ReadOnly {
		t.Fatalf("workspace mapping = %#v", workspace)
	}
}

func TestRenderConfigMapsDedicatedWritableWorktreeDirectory(t *testing.T) {
	workspaces := []workspacePlan{{Name: "project", HostDirectory: `D:\Project`, GuestDirectory: `C:\Workspaces\project`}}
	encoded, err := renderConfigWithWorktreeDirectory(
		`C:\Runs\one\input`, `C:\Runs\one\status`, `E:\cache`, `F:\HerdrWorktrees`, nil, workspaces, 4096, false, false,
	)
	if err != nil {
		t.Fatal(err)
	}
	var config wsbConfiguration
	if err := xml.Unmarshal(encoded, &config); err != nil {
		t.Fatal(err)
	}
	if len(config.MappedFolders.Folders) != 5 {
		t.Fatalf("mapped folders = %#v", config.MappedFolders.Folders)
	}
	worktrees := config.MappedFolders.Folders[1]
	if worktrees.HostFolder != `F:\HerdrWorktrees` || worktrees.SandboxFolder != guestWorktreeDirectory || worktrees.ReadOnly {
		t.Fatalf("worktree mapping = %#v", worktrees)
	}
	workspace := config.MappedFolders.Folders[2]
	if workspace.SandboxFolder != `C:\Workspaces\project` || workspace.ReadOnly {
		t.Fatalf("workspace mapping = %#v", workspace)
	}
}

func TestRenderConfigAudioOutputOptInKeepsMicrophoneDisabled(t *testing.T) {
	encoded, err := renderConfig(`C:\Runs\one\input`, `C:\Runs\one\status`, `E:\cache`, nil, nil, 4096, true, false)
	if err != nil {
		t.Fatal(err)
	}
	var config wsbConfiguration
	if err := xml.Unmarshal(encoded, &config); err != nil {
		t.Fatal(err)
	}
	if config.AudioInput != "Disable" || strings.Count(config.LogonCommand.Command, "'-AudioPlayback','Enabled'") != 1 || strings.Contains(config.LogonCommand.Command, "'-AudioPlayback','Disabled'") {
		t.Fatalf("audio output opt-in config = input %q, command %q", config.AudioInput, config.LogonCommand.Command)
	}
}

func TestRenderConfigAudioInputOptInKeepsOutputDisabled(t *testing.T) {
	encoded, err := renderConfig(`C:\Runs\one\input`, `C:\Runs\one\status`, `E:\cache`, nil, nil, 4096, false, true)
	if err != nil {
		t.Fatal(err)
	}
	var config wsbConfiguration
	if err := xml.Unmarshal(encoded, &config); err != nil {
		t.Fatal(err)
	}
	if config.AudioInput != "Enable" ||
		strings.Count(config.LogonCommand.Command, "'-AudioInput','Enabled'") != 1 ||
		strings.Count(config.LogonCommand.Command, "'-AudioPlayback','Disabled'") != 1 {
		t.Fatalf("audio input opt-in config = input %q, command %q", config.AudioInput, config.LogonCommand.Command)
	}
}

func TestAudioSelectionsBindThroughStartProcessInWindowsPowerShell51(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows PowerShell 5.1 audio selection regression")
	}
	root := t.TempDir()
	childPath := filepath.Join(root, "audio-switch-child.ps1")
	if err := os.WriteFile(childPath, []byte(`param(
    [Parameter(Mandatory = $true)][string]$OutputPath,
    [Parameter(Mandatory = $true)]
    [ValidateSet('Disabled', 'Enabled')]
    [string]$AudioPlayback,
    [Parameter(Mandatory = $true)]
    [ValidateSet('Disabled', 'Enabled')]
    [string]$AudioInput
)
[IO.File]::WriteAllText($OutputPath, ($AudioPlayback + '|' + $AudioInput).ToLowerInvariant())
`), 0o600); err != nil {
		t.Fatal(err)
	}
	quote := func(value string) string { return strings.ReplaceAll(value, "'", "''") }
	for _, test := range []struct {
		name            string
		outputSelection string
		inputSelection  string
		expected        string
	}{
		{name: "both-disabled", outputSelection: "'Disabled'", inputSelection: "'Disabled'", expected: "disabled|disabled"},
		{name: "output-only", outputSelection: "'Enabled'", inputSelection: "'Disabled'", expected: "enabled|disabled"},
		{name: "input-only", outputSelection: "'Disabled'", inputSelection: "'Enabled'", expected: "disabled|enabled"},
		{name: "both-enabled", outputSelection: "'Enabled'", inputSelection: "'Enabled'", expected: "enabled|enabled"},
	} {
		t.Run(test.name, func(t *testing.T) {
			outputPath := filepath.Join(root, test.name+".txt")
			arguments := []string{
				"'-NoLogo'", "'-NoProfile'", "'-NonInteractive'", "'-ExecutionPolicy'", "'Bypass'",
				"'-File'", "'" + quote(childPath) + "'", "'-OutputPath'", "'" + quote(outputPath) + "'",
				"'-AudioPlayback'", test.outputSelection, "'-AudioInput'", test.inputSelection,
			}
			powerShell := mustWindowsPowerShellPath(t)
			launcher := "$process = Start-Process -FilePath '" + quote(powerShell) +
				"' -WindowStyle Hidden -Wait -PassThru -ArgumentList @(" + strings.Join(arguments, ",") +
				"); exit $process.ExitCode"
			command := hiddenCommand(powerShell, "-NoLogo", "-NoProfile", "-NonInteractive", "-Command", launcher)
			if output, err := command.CombinedOutput(); err != nil {
				t.Fatalf("explicit audio selection binding: %v: %s", err, output)
			}
			value, err := os.ReadFile(outputPath)
			if err != nil {
				t.Fatal(err)
			}
			if string(value) != test.expected {
				t.Fatalf("explicit audio selections = %q, want %q", value, test.expected)
			}
		})
	}
}

func TestRenderConfigEscapesHostPaths(t *testing.T) {
	workspaces := []workspacePlan{{Name: "a-b", HostDirectory: `D:\Projects\A&B`, GuestDirectory: `C:\Workspaces\a-b`}}
	encoded, err := renderConfig(`C:\Runs\A&B\input`, `C:\Runs\A&B\status`, `E:\cache&A`, nil, workspaces, 4096, false, false)
	if err != nil {
		t.Fatalf("renderConfig: %v", err)
	}
	if !strings.Contains(string(encoded), `A&amp;B`) {
		t.Fatalf("config did not XML-escape path:\n%s", encoded)
	}
}

func TestCanonicalMappedDirectoryRejectsSymbolicLink(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target")
	link := filepath.Join(root, "link")
	if err := os.MkdirAll(target, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("directory symlink unavailable: %v", err)
	}
	if _, err := canonicalMappedDirectory(link); err == nil {
		t.Fatal("symbolic-link mapping unexpectedly succeeded")
	}
}

func TestCanonicalMappedDirectoryAcceptsPhysicalDirectory(t *testing.T) {
	directory := t.TempDir()
	canonical, err := canonicalMappedDirectory(directory)
	if err != nil {
		t.Fatalf("canonicalMappedDirectory: %v", err)
	}
	expected, err := filepath.EvalSymlinks(directory)
	if err != nil {
		t.Fatalf("resolve expected directory: %v", err)
	}
	if !strings.EqualFold(canonical, filepath.Clean(expected)) {
		t.Fatalf("canonical directory = %q, want %q", canonical, expected)
	}
}

func TestPhysicalMappedDirectoryUsesNativeIdentity(t *testing.T) {
	directory := t.TempDir()
	identity, err := physicalMappedDirectory(directory)
	if err != nil {
		t.Fatalf("physicalMappedDirectory: %v", err)
	}
	if identity == "" {
		t.Fatal("physical identity is empty")
	}
	if runtime.GOOS == "windows" && !strings.HasPrefix(identity, `\device\`) {
		t.Fatalf("Windows physical identity = %q", identity)
	}
}

func TestPhysicalMappedDirectoryDetectsSubstAlias(t *testing.T) {
	if runtime.GOOS != "windows" || os.Getenv("HERDR_SANDBOX_NATIVE_SUBST_TEST") != "1" {
		t.Skip("set HERDR_SANDBOX_NATIVE_SUBST_TEST=1 for the native SUBST alias gate")
	}
	target := t.TempDir()
	drive := ""
	for letter := 'Z'; letter >= 'T'; letter-- {
		candidate := string(letter) + `:`
		if _, err := os.Stat(candidate + `\`); os.IsNotExist(err) {
			drive = candidate
			break
		}
	}
	if drive == "" {
		t.Skip("no free drive letter is available for the native SUBST alias gate")
	}
	if output, err := hiddenCommand("subst.exe", drive, target).CombinedOutput(); err != nil {
		t.Fatalf("create SUBST alias: %v: %s", err, output)
	}
	t.Cleanup(func() {
		if output, err := hiddenCommand("subst.exe", drive, `/D`).CombinedOutput(); err != nil {
			t.Errorf("remove SUBST alias: %v: %s", err, output)
		}
	})

	alias, err := canonicalMappedDirectory(drive + `\`)
	if err != nil {
		t.Fatalf("canonicalize SUBST alias: %v", err)
	}
	targetIdentity, err := physicalMappedDirectory(target)
	if err != nil {
		t.Fatal(err)
	}
	aliasIdentity, err := physicalMappedDirectory(alias)
	if err != nil {
		t.Fatal(err)
	}
	if !hostPathsOverlap(targetIdentity, aliasIdentity) {
		t.Fatalf("SUBST identities do not overlap: target %q, alias %q", targetIdentity, aliasIdentity)
	}
	project := createWorkspaceFixture(t, target, "project")
	stateRoot := t.TempDir()
	defaults := filepath.Join(stateRoot, "defaults")
	global := filepath.Join(stateRoot, "global")
	if err := os.MkdirAll(defaults, 0o700); err != nil {
		t.Fatal(err)
	}
	writeWorkspaceDiscoveryConfig(t, global, &workspaceDiscoveryConfiguration{Root: target, Exclude: []string{}}, map[string]string{"custom": filepath.Join(drive+`\`, "project")})
	plan, err := resolveProvisioningAt(filepath.Join(project, "src"), global, defaults)
	if err != nil {
		t.Fatalf("resolve SUBST workspace plan: %v", err)
	}
	if len(plan.Workspaces) != 1 || plan.Workspaces[0].Name != "custom" || !plan.Workspaces[0].Active {
		t.Fatalf("SUBST workspace plan = %#v", plan.Workspaces)
	}
	t.Setenv("USERPROFILE", target)
	if _, err := discoverWorkspacePlans(&workspaceDiscoveryConfiguration{Root: drive + `\`, Exclude: []string{}}); err == nil || !strings.Contains(err.Error(), "must not contain a user profile") {
		t.Fatalf("SUBST-protected discovery root error = %v", err)
	}
	worktrees := filepath.Join(target, "worktrees")
	if err := os.MkdirAll(worktrees, 0o700); err != nil {
		t.Fatal(err)
	}
	configurationRoot := filepath.Join(drive+`\`, "worktrees", "configuration")
	if err := os.MkdirAll(configurationRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(configurationRoot, globalConfigurationName), `{"worktreeDirectory":"`+filepath.ToSlash(worktrees)+`"}`)
	if _, err := resolveProvisioningAt(target, configurationRoot, t.TempDir()); err == nil || !strings.Contains(err.Error(), "overlaps user configuration") {
		t.Fatalf("SUBST configuration-root overlap error = %v", err)
	}
}

func TestRenderConfigRejectsUnsafeLayout(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		status    string
		cache     string
		workspace workspacePlan
		memory    int
	}{
		{name: "relative input", input: `input`, status: `C:\Runs\status`, cache: `E:\Cache`, workspace: workspacePlan{Name: "project", HostDirectory: `D:\Project`, GuestDirectory: `C:\Workspaces\project`}, memory: 4096},
		{name: "relative status", input: `C:\Runs\input`, status: `status`, cache: `E:\Cache`, workspace: workspacePlan{Name: "project", HostDirectory: `D:\Project`, GuestDirectory: `C:\Workspaces\project`}, memory: 4096},
		{name: "relative cache", input: `C:\Runs\input`, status: `C:\Runs\status`, cache: `cache`, workspace: workspacePlan{Name: "project", HostDirectory: `D:\Project`, GuestDirectory: `C:\Workspaces\project`}, memory: 4096},
		{name: "relative project", input: `C:\Runs\input`, status: `C:\Runs\status`, cache: `E:\Cache`, workspace: workspacePlan{Name: "project", HostDirectory: `project`, GuestDirectory: `C:\Workspaces\project`}, memory: 4096},
		{name: "shared directory", input: `C:\Runs\same`, status: `c:\runs\same`, cache: `E:\Cache`, workspace: workspacePlan{Name: "project", HostDirectory: `D:\Project`, GuestDirectory: `C:\Workspaces\project`}, memory: 4096},
		{name: "cache contains run", input: `C:\Runs\one\input`, status: `C:\Runs\one\status`, cache: `C:\Runs`, workspace: workspacePlan{Name: "project", HostDirectory: `D:\Project`, GuestDirectory: `C:\Workspaces\project`}, memory: 4096},
		{name: "project overlaps input", input: `C:\Runs\input`, status: `C:\Runs\status`, cache: `E:\Cache`, workspace: workspacePlan{Name: "project", HostDirectory: `c:\runs\input`, GuestDirectory: `C:\Workspaces\project`}, memory: 4096},
		{name: "project overlaps cache", input: `C:\Runs\input`, status: `C:\Runs\status`, cache: `E:\Cache`, workspace: workspacePlan{Name: "project", HostDirectory: `e:\cache\project`, GuestDirectory: `C:\Workspaces\project`}, memory: 4096},
		{name: "low memory", input: `C:\Runs\input`, status: `C:\Runs\status`, cache: `E:\Cache`, workspace: workspacePlan{Name: "project", HostDirectory: `D:\Project`, GuestDirectory: `C:\Workspaces\project`}, memory: 1024},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := renderConfig(test.input, test.status, test.cache, nil, []workspacePlan{test.workspace}, test.memory, false, false); err == nil {
				t.Fatal("renderConfig unexpectedly succeeded")
			}
		})
	}
}

func TestRenderConfigRejectsUnsafeFolderMountLayouts(t *testing.T) {
	workspace := workspacePlan{Name: "project", HostDirectory: `D:\Project`, GuestDirectory: `C:\Workspaces\project`}
	tests := map[string][]mountPlan{
		"workspace overlap": {{Name: "shared", HostDirectory: `D:\Project\shared`, GuestDirectory: `C:\Mounts\shared`, ReadOnly: true}},
		"duplicate hosts": {
			{Name: "first", HostDirectory: `E:\Shared`, GuestDirectory: `C:\Mounts\first`, ReadOnly: true},
			{Name: "second", HostDirectory: `E:\Shared\nested`, GuestDirectory: `C:\Mounts\second`, ReadOnly: false},
		},
		"arbitrary guest path": {{Name: "shared", HostDirectory: `E:\Shared`, GuestDirectory: `C:\Windows`, ReadOnly: true}},
		"invalid name":         {{Name: `..\shared`, HostDirectory: `E:\Shared`, GuestDirectory: `C:\Mounts\..\shared`, ReadOnly: true}},
	}
	for name, mounts := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := renderConfig(`C:\Runs\input`, `C:\Runs\status`, `F:\Cache`, mounts, []workspacePlan{workspace}, 4096, false, false); err == nil {
				t.Fatal("unsafe folder mount layout unexpectedly succeeded")
			}
		})
	}
}

func TestRenderConfigRejectsUnsafeWorktreeDirectoryLayouts(t *testing.T) {
	workspace := workspacePlan{Name: "project", HostDirectory: `D:\Project`, GuestDirectory: `C:\Workspaces\project`}
	for name, directory := range map[string]string{
		"relative":          `worktrees`,
		"workspace overlap": `D:\Project\worktrees`,
		"cache overlap":     `F:\Cache\worktrees`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := renderConfigWithWorktreeDirectory(`C:\Runs\input`, `C:\Runs\status`, `F:\Cache`, directory, nil, []workspacePlan{workspace}, 4096, false, false); err == nil {
				t.Fatal("unsafe worktree directory layout unexpectedly succeeded")
			}
		})
	}
}
