package sandbox

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

func TestFindProjectProvisioningUsesNearestAncestor(t *testing.T) {
	root := t.TempDir()
	outer := filepath.Join(root, projectConfigurationName)
	if err := os.MkdirAll(outer, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outer, projectProvisioningName), []byte("outer"), 0o600); err != nil {
		t.Fatal(err)
	}
	project := filepath.Join(root, "project")
	inner := filepath.Join(project, projectConfigurationName)
	if err := os.MkdirAll(inner, 0o700); err != nil {
		t.Fatal(err)
	}
	innerScript := filepath.Join(inner, projectProvisioningName)
	if err := os.WriteFile(innerScript, []byte("inner"), 0o600); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(project, "src", "nested")
	if err := os.MkdirAll(nested, 0o700); err != nil {
		t.Fatal(err)
	}

	gotRoot, gotScript, found, err := findProjectProvisioning(nested)
	if err != nil {
		t.Fatalf("findProjectProvisioning: %v", err)
	}
	if !found || gotRoot != project || gotScript != innerScript {
		t.Fatalf("project = %q, script = %q", gotRoot, gotScript)
	}
}

func TestEncodeGuestWorkspaceManifestIsStrictAndDeterministic(t *testing.T) {
	data, err := encodeGuestWorkspaceManifest([]workspacePlan{
		{Name: "zeta", GuestDirectory: `C:\Workspaces\zeta`},
		{Name: "alpha", GuestDirectory: `C:\Workspaces\alpha`},
	}, `C:\Workspaces\zeta`)
	if err != nil {
		t.Fatalf("encodeGuestWorkspaceManifest: %v", err)
	}
	var manifest guestWorkspaceManifest
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.SchemaVersion != workspaceManifestSchema || manifest.ActiveWorkspace != `C:\Workspaces\zeta` ||
		len(manifest.Workspaces) != 2 || manifest.Workspaces[0].Name != "alpha" || manifest.Workspaces[1].Name != "zeta" {
		t.Fatalf("workspace manifest = %#v", manifest)
	}
	if _, err := encodeGuestWorkspaceManifest([]workspacePlan{{Name: "alpha", GuestDirectory: `C:\Workspaces\alpha`}}, `C:\Workspaces\missing`); err == nil {
		t.Fatal("missing active workspace unexpectedly accepted")
	}
}

func TestValidateBaseProvisioningContract(t *testing.T) {
	path := filepath.Join(t.TempDir(), baseProvisioningName)
	writeTestFile(t, path, baseProvisioningContract+"\nWrite-Output 'ready'\n")
	if err := validateBaseProvisioningContract(path); err != nil {
		t.Fatalf("validate current contract: %v", err)
	}
	writeTestFile(t, path, "Write-Output 'old'\n")
	if err := validateBaseProvisioningContract(path); err == nil {
		t.Fatal("outdated base provisioning contract unexpectedly succeeded")
	}
}

func TestValidateStackProvisioningContract(t *testing.T) {
	path := filepath.Join(t.TempDir(), stackProvisioningName)
	writeTestFile(t, path, stackProvisioningContract+"\nfunction Install-GoStack {}\n")
	if err := validateStackProvisioningContract(path); err != nil {
		t.Fatalf("validate current stack contract: %v", err)
	}
	writeTestFile(t, path, "function Install-GoStack {}\n")
	if err := validateStackProvisioningContract(path); err == nil {
		t.Fatal("unsupported stack provisioning contract unexpectedly succeeded")
	}
}

func TestValidateUserProvisioningContract(t *testing.T) {
	path := filepath.Join(t.TempDir(), userProvisioningName)
	writeTestFile(t, path, userProvisioningContract+"\nWrite-Output 'ready'\n")
	if err := validateUserProvisioningContract(path); err != nil {
		t.Fatalf("validate current user contract: %v", err)
	}
	writeTestFile(t, path, userProvisioningContract+"\n"+baseProvisioningContract+"\n")
	if err := validateUserProvisioningContract(path); err == nil {
		t.Fatal("app-owned Base masquerading as user provisioning unexpectedly succeeded")
	}
}

func TestValidateProvisioningProcessSource(t *testing.T) {
	if err := validateProvisioningProcessSource(provisioningProcessSource); err != nil {
		t.Fatalf("validate embedded provisioning process source: %v", err)
	}
	if err := validateProvisioningProcessSource([]byte("class Old {}")); err == nil {
		t.Fatal("unsupported provisioning process source unexpectedly succeeded")
	}
}

func TestProjectProvisioningRejectsReparseScriptAndParent(t *testing.T) {
	t.Run("script", func(t *testing.T) {
		root := t.TempDir()
		project := createWorkspaceFixture(t, root, "project")
		script := filepath.Join(project, projectConfigurationName, projectProvisioningName)
		target := filepath.Join(root, "target.ps1")
		writeTestFile(t, target, "target")
		if err := os.Remove(script); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, script); err != nil {
			t.Skipf("file symlink unavailable: %v", err)
		}
		if err := validateProjectProvisioningScript(script); err == nil || !strings.Contains(err.Error(), "non-reparse") {
			t.Fatalf("reparse project script error = %v", err)
		}
	})

	t.Run("parent", func(t *testing.T) {
		root := t.TempDir()
		project := filepath.Join(root, "project")
		target := filepath.Join(root, "configuration")
		if err := os.MkdirAll(project, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(target, 0o700); err != nil {
			t.Fatal(err)
		}
		writeTestFile(t, filepath.Join(target, projectProvisioningName), "target")
		configuration := filepath.Join(project, projectConfigurationName)
		createTestDirectoryLink(t, configuration, target)
		if err := validateProjectProvisioningScript(filepath.Join(configuration, projectProvisioningName)); err == nil || !strings.Contains(err.Error(), "unsafe parent") {
			t.Fatalf("reparse project parent error = %v", err)
		}
	})
}

func TestProtectedRootMappingValidationAllowsOrdinaryDescendantsButRejectsSensitiveRoots(t *testing.T) {
	protected := t.TempDir()
	child := filepath.Join(protected, "selected-project")
	sensitive := filepath.Join(protected, ".ssh")
	sensitiveChild := filepath.Join(sensitive, "keys")
	for _, directory := range []string{child, sensitiveChild} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"USERPROFILE", "APPDATA", "LOCALAPPDATA"} {
		t.Setenv(name, protected)
	}
	protectedIdentity, err := physicalMappedDirectory(protected)
	if err != nil {
		t.Fatal(err)
	}
	if err := validatePhysicalMappingDoesNotContainProtectedRoot("workspace", protectedIdentity); err == nil || !strings.Contains(err.Error(), "must not contain") {
		t.Fatalf("protected-root mapping error = %v", err)
	}
	childIdentity, err := physicalMappedDirectory(child)
	if err != nil {
		t.Fatal(err)
	}
	if err := validatePhysicalMappingDoesNotContainProtectedRoot("workspace", childIdentity); err != nil {
		t.Fatalf("narrow protected-root descendant was rejected: %v", err)
	}
	if err := validatePhysicalMappingDoesNotExposeSensitiveRoot("workspace", childIdentity); err != nil {
		t.Fatalf("ordinary protected-root descendant was rejected: %v", err)
	}
	for _, path := range []string{sensitive, sensitiveChild} {
		identity, err := physicalMappedDirectory(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := validatePhysicalMappingDoesNotExposeSensitiveRoot("folder mount secrets", identity); err == nil || !strings.Contains(err.Error(), "security-sensitive") {
			t.Fatalf("sensitive mapping error for %s = %v", path, err)
		}
	}
}

func TestSensitiveRootMappingValidationRejectsParentOfCredentialJunction(t *testing.T) {
	mappingRoot := t.TempDir()
	credentialTarget := t.TempDir()
	credentialLink := filepath.Join(mappingRoot, "credentials")
	createTestDirectoryLink(t, credentialLink, credentialTarget)
	for _, name := range []string{"USERPROFILE", "APPDATA", "LOCALAPPDATA"} {
		t.Setenv(name, t.TempDir())
	}
	t.Setenv("GH_CONFIG_DIR", credentialLink)
	mappingIdentity, err := physicalMappedDirectory(mappingRoot)
	if err != nil {
		t.Fatal(err)
	}
	if err := validatePhysicalMappingDoesNotExposeSensitiveRoot("folder mount tools", mappingIdentity); err == nil || !strings.Contains(err.Error(), "security-sensitive") {
		t.Fatalf("credential-junction parent mapping error = %v", err)
	}
}

func TestCanonicalWorkspacePlansRebindProvisioningScriptToPhysicalRoot(t *testing.T) {
	root := t.TempDir()
	project := createWorkspaceFixture(t, root, "project")
	plans, err := canonicalWorkspacePlans([]workspacePlan{{
		Name:             "project",
		HostDirectory:    project,
		GuestDirectory:   guestWorkspaceDirectory("project"),
		ProvisioningPath: filepath.Join(root, "stale", projectProvisioningName),
	}})
	if err != nil {
		t.Fatalf("canonicalWorkspacePlans: %v", err)
	}
	wantRoot, err := canonicalMappedDirectory(project)
	if err != nil {
		t.Fatal(err)
	}
	wantScript := filepath.Join(wantRoot, projectConfigurationName, projectProvisioningName)
	if len(plans) != 1 || plans[0].HostDirectory != wantRoot || plans[0].ProvisioningPath != wantScript {
		t.Fatalf("canonical workspace plans = %#v, want root %q and script %q", plans, wantRoot, wantScript)
	}
}

func TestCanonicalWorkspacePlansAllowsMissingProvisioningScript(t *testing.T) {
	project := filepath.Join(t.TempDir(), "plain")
	if err := os.MkdirAll(project, 0o700); err != nil {
		t.Fatal(err)
	}
	plans, err := canonicalWorkspacePlans([]workspacePlan{{
		Name: "plain", HostDirectory: project, GuestDirectory: guestWorkspaceDirectory("plain"),
		ProvisioningPath: filepath.Join(project, projectConfigurationName, projectProvisioningName),
	}})
	if err != nil {
		t.Fatalf("canonicalWorkspacePlans: %v", err)
	}
	if len(plans) != 1 || plans[0].ProvisioningPath != "" {
		t.Fatalf("unprofiled canonical workspace = %#v", plans)
	}
}

func TestDefaultBaseInstallsGitHubCLIThroughCachedMSIAdapter(t *testing.T) {
	text := readDefaultBaseProvisioning(t)
	for _, required := range []string{
		baseProvisioningContract,
		"-Role 'GitHub CLI' -Id 'GitHub.cli'",
		"-InstallerType 'wix' -Scope 'machine' -Adapter 'MSI' -ExecutableName 'gh.exe'",
		"Assert-ProvisioningCommand -Role 'GitHub CLI' -Name 'gh.exe'",
		"GitHub CLI ready:",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("default Base is missing GitHub CLI contract %q", required)
		}
	}
}

func TestDefaultBaseInstallsActionlintThroughCachedPortableAdapter(t *testing.T) {
	text := readDefaultBaseProvisioning(t)
	for _, required := range []string{
		"if (Test-ProvisioningPackageEnabled -Id 'rhysd.actionlint')",
		"-Role 'actionlint' -Id 'rhysd.actionlint'",
		"-InstallerType 'zip' -Adapter 'Portable' -ExecutableName 'actionlint.exe'",
		"Assert-ProvisioningCommand -Role 'actionlint' -Name 'actionlint.exe'",
		"-VersionArguments @('-version')",
		"actionlint ready:",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("default Base is missing actionlint contract %q", required)
		}
	}
}

func TestDefaultBaseInstallsTailscaleWithoutAuthentication(t *testing.T) {
	text := readDefaultBaseProvisioning(t)
	for _, required := range []string{
		"-Role 'Tailscale' -Id 'Tailscale.Tailscale'",
		"-InstallerType 'wix' -Scope 'machine' -Adapter 'MSI' -ExecutableName 'tailscale.exe'",
		"-InstallerArguments @('TS_NOLAUNCH=1')",
		"@('/i', $PayloadPath, '/quiet', '/norestart') + $InstallerArguments",
		"Assert-ProvisioningCommand -Role 'Tailscale' -Name 'tailscale.exe'",
		"Tailscale ready:",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("default Base is missing Tailscale contract %q", required)
		}
	}
	for _, forbidden := range []string{"tailscale up", "tailscale login", "--auth-key"} {
		if strings.Contains(strings.ToLower(text), forbidden) {
			t.Fatalf("default Base authenticates Tailscale with %q", forbidden)
		}
	}
}

func TestDefaultBaseInstallsWinDirStatAndFilePilot(t *testing.T) {
	text := readDefaultBaseProvisioning(t)
	for _, required := range []string{
		"-Role 'WinDirStat' -Id 'WinDirStat.WinDirStat'",
		"-InstallerType 'wix' -Scope 'machine' -Adapter 'MSI'",
		"Join-Path $env:ProgramFiles 'WinDirStat\\WinDirStat.exe'",
		"-Role 'File Pilot' -Id 'Voidstar.FilePilot'",
		"function Ensure-ProvisioningStartShortcut",
		"[ValidateSet('File Pilot', 'TradingView')]",
		"[string]$ShortcutArguments = ''",
		"function Ensure-ProvisioningFilePilotStartShortcut",
		`Microsoft\WinGet\Packages\Voidstar.FilePilot_Microsoft.Winget.Source_8wekyb3d8bbwe\FPilot.exe`,
		"New-Object -ComObject WScript.Shell",
		"Ensure-ProvisioningStartShortcut -DisplayName 'File Pilot' -Executable $filePilotExecutable",
		"[string]$shortcut.Arguments -ceq $ShortcutArguments",
		"$shortcut.Arguments = $ShortcutArguments",
		"[string]$verifiedShortcut.Arguments -cne $ShortcutArguments",
		"Start shortcut read-back did not match the installed executable and arguments",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("default Base is missing GUI utility contract %q", required)
		}
	}
}

func TestProvisioningStartShortcutPreservesExactArguments(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows shortcut regression")
	}
	root := t.TempDir()
	executable := filepath.Join(root, "TradingView.exe")
	if err := os.WriteFile(executable, []byte("shortcut target"), 0o600); err != nil {
		t.Fatal(err)
	}
	quote := func(value string) string { return strings.ReplaceAll(value, "'", "''") }
	script := fmt.Sprintf(`$ErrorActionPreference = 'Stop'
$tokens = $null
$errors = $null
$ast = [Management.Automation.Language.Parser]::ParseFile('%s', [ref]$tokens, [ref]$errors)
if ($errors.Count -gt 0) { throw ($errors | Out-String) }
$shortcutFunction = $ast.Find({
    param($node)
    $node -is [Management.Automation.Language.FunctionDefinitionAst] -and
        $node.Name -ceq 'Ensure-ProvisioningStartShortcut'
}, $true)
if ($null -eq $shortcutFunction) { throw 'Shortcut function is missing.' }
Invoke-Expression $shortcutFunction.Extent.Text
$env:APPDATA = '%s'
$executable = '%s'
Ensure-ProvisioningStartShortcut -DisplayName 'TradingView' -Executable $executable
Ensure-ProvisioningStartShortcut -DisplayName 'TradingView' -Executable $executable -ShortcutArguments '--remote-debugging-port=9222'
Ensure-ProvisioningStartShortcut -DisplayName 'File Pilot' -Executable $executable
$shell = New-Object -ComObject WScript.Shell
$tradingView = $shell.CreateShortcut((Join-Path $env:APPDATA 'Microsoft\Windows\Start Menu\Programs\TradingView.lnk'))
$filePilot = $shell.CreateShortcut((Join-Path $env:APPDATA 'Microsoft\Windows\Start Menu\Programs\File Pilot.lnk'))
if ([string]$tradingView.Arguments -cne '--remote-debugging-port=9222' -or
    -not [string]::IsNullOrEmpty([string]$filePilot.Arguments)) {
    throw "Shortcut argument read-back failed: TradingView=$([string]$tradingView.Arguments), FilePilot=$([string]$filePilot.Arguments)"
}
`, quote(defaultProvisioningPath(t, baseProvisioningName)), quote(filepath.Join(root, "appdata")), quote(executable))
	command := hiddenCommand(mustWindowsPowerShellPath(t),
		"-NoLogo", "-NoProfile", "-NonInteractive", "-EncodedCommand", encodePowerShell(script))
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("shortcut argument regression: %v: %s", err, output)
	}
}

func TestConfigurationSyncForcesManagedOpenCodeConfigurationAfterTransfer(t *testing.T) {
	text := string(configurationSyncScript)
	for _, required := range []string{
		"function Get-OpenCodeAllowAllPermissions",
		"function Get-OpenCodeTVControlMCPConfiguration",
		"function Install-OpenCodeSandboxConfiguration",
		"function Enable-OpenCodeSandboxConfiguration",
		"$openCodeInstalled = $null -ne (Get-Command 'opencode.exe'",
		"if ([bool]$agentSync.opencode -or $openCodeEnabled -or $openCodeInstalled)",
		"[config-sync] enforce-opencode-allow-all",
		"-RequireExecutable ($openCodeEnabled -or $openCodeInstalled)",
		"-TradingViewEnabled $tradingViewStackEnabled",
		"C:\\HerdrSandbox\\tools\\tvcontrol\\node_modules\\@ferroxlabs\\tvcontrol",
		"TV_CDP_HOST = '127.0.0.1'",
		"TV_CDP_PORT = '9222'",
		"TV_MCP_ADVANCED = '0'",
		"TV_MCP_TELEMETRY = '0'",
		"Get-Command 'opencode.exe' -CommandType Application -ErrorAction SilentlyContinue",
		"sandbox-allow-all.js",
		"$allowAllJSON = $permissions | ConvertTo-Json -Compress",
		"const permissions = $allowAllJSON",
		"const tvcontrol = $tvControlMCPJSON",
		"const allowAll = () => ({ ...permissions })",
		"config.permission = allowAll()",
		"for (const agent of Object.values(config.agent ?? {}))",
		"agent.permission = allowAll()",
		"config.mcp = { ...(config.mcp ?? {}), tvcontrol:",
		"external_directory = 'allow'",
		"task = 'allow'",
		"todowrite = 'allow'",
		"doom_loop = 'allow'",
		"[IO.File]::ReadAllText($managedFile.Path) -cne $managedFile.Contents",
		"OpenCode effective permissions were not replaced by the Sandbox allow-all policy",
		"Assert-OpenCodeSandboxConfiguration",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("configuration sync is missing OpenCode allow-all contract %q", required)
		}
	}
	apply := strings.Index(text, "[config-sync] apply-opencode")
	enforce := strings.Index(text, "[config-sync] enforce-opencode-allow-all")
	verify := strings.LastIndex(text, "$openCodePermissionVerified = [bool](Enable-OpenCodeSandboxConfiguration")
	if apply < 0 || enforce <= apply || verify <= enforce {
		t.Fatalf("OpenCode transfer/policy/verification order = %d/%d/%d", apply, enforce, verify)
	}
	if strings.Contains(text, `external_directory: "allow"`) {
		t.Fatal("OpenCode managed plugin repeats the canonical permission table")
	}
	base := readDefaultBaseProvisioning(t)
	for _, removed := range []string{"sandbox-allow-all.js", "OpenCode managed permission", "$openCodeManagedDirectory"} {
		if strings.Contains(base, removed) {
			t.Fatalf("Base retains duplicate OpenCode policy owner %q", removed)
		}
	}
}

func installOpenCodeSandboxConfigurationForTest(t *testing.T, programData string) string {
	t.Helper()
	start := bytes.Index(configurationSyncScript, []byte("$script:CopiedConfigurationFiles = 0"))
	end := bytes.Index(configurationSyncScript, []byte("function Invoke-GuestGitHubCLI {"))
	if start < 0 || end <= start {
		t.Fatal("configuration-sync OpenCode policy helper block was not found")
	}
	script := string(configurationSyncScript[start:end]) + "\nInstall-OpenCodeSandboxConfiguration\n"
	scriptPath := filepath.Join(t.TempDir(), "install-opencode-allow-all.ps1")
	writeTestFile(t, scriptPath, script)
	command := hiddenCommand(mustWindowsPowerShellPath(t), "-NoLogo", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File", scriptPath)
	command.Env = append(os.Environ(), "ProgramData="+programData)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("install OpenCode allow-all policy: %v: %s", err, output)
	}
	return filepath.Join(programData, "opencode")
}

func TestConfigurationSyncWritesManagedOpenCodeAllowAllPolicy(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows PowerShell 5.1 OpenCode policy regression")
	}
	managed := installOpenCodeSandboxConfigurationForTest(t, filepath.Join(t.TempDir(), "program-data"))
	configData, err := os.ReadFile(filepath.Join(managed, "opencode.json"))
	if err != nil {
		t.Fatal(err)
	}
	var policy struct {
		Permission map[string]string `json:"permission"`
		Plugin     []string          `json:"plugin"`
	}
	if err := json.Unmarshal(configData, &policy); err != nil {
		t.Fatalf("decode managed OpenCode config: %v", err)
	}
	if len(policy.Permission) != 18 || len(policy.Plugin) != 1 || !strings.HasSuffix(policy.Plugin[0], "/opencode/sandbox-allow-all.js") {
		t.Fatalf("managed OpenCode policy = %#v", policy)
	}
	for name, value := range policy.Permission {
		if value != "allow" {
			t.Fatalf("managed permission %s = %q", name, value)
		}
	}
	plugin, err := os.ReadFile(filepath.Join(managed, "sandbox-allow-all.js"))
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{"config.permission = allowAll()", "agent.permission = allowAll()", "const tvcontrol = null"} {
		if !bytes.Contains(plugin, []byte(required)) {
			t.Fatalf("managed OpenCode plugin is missing %q", required)
		}
	}
}

func TestCurrentOpenCodeManagedPluginReplacesTransferredPermissions(t *testing.T) {
	if runtime.GOOS != "windows" || os.Getenv("HERDR_SANDBOX_TEST_REAL_OPENCODE") != "1" {
		t.Skip("set HERDR_SANDBOX_TEST_REAL_OPENCODE=1 for the installed OpenCode boundary")
	}
	opencode, err := exec.LookPath("opencode.exe")
	if err != nil {
		t.Skip("OpenCode is not installed")
	}

	root := t.TempDir()
	configRoot := filepath.Join(root, "config")
	dataRoot := filepath.Join(root, "data")
	home := filepath.Join(root, "home")
	project := filepath.Join(root, "project")
	for _, directory := range []string{filepath.Join(configRoot, "opencode"), dataRoot, home, project} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	writeTestFile(t, filepath.Join(configRoot, "opencode", "opencode.json"), `{"permission":{"*":"deny","bash":{"*":"deny"}},"agent":{"locked":{"description":"locked","mode":"subagent","permission":{"*":"deny","bash":"deny"}}}}`)
	writeTestFile(t, filepath.Join(project, "opencode.json"), `{"permission":{"edit":"deny"},"agent":{"project":{"description":"project","mode":"subagent","permission":{"*":"deny","edit":"deny"}}}}`)
	programData := filepath.Join(root, "program-data")
	start := bytes.Index(configurationSyncScript, []byte("$script:CopiedConfigurationFiles = 0"))
	end := bytes.Index(configurationSyncScript, []byte("$digest = (Get-FileHash"))
	if start < 0 || end <= start {
		t.Fatal("configuration-sync OpenCode verification helper block was not found")
	}
	applyScript := string(configurationSyncScript[start:end]) + "\nif (-not (Enable-OpenCodeSandboxConfiguration -RequireExecutable $false)) { throw 'Installed OpenCode was not verified.' }\n"
	applyPath := filepath.Join(root, "apply-opencode-policy.ps1")
	writeTestFile(t, applyPath, applyScript)
	apply := hiddenCommand(mustWindowsPowerShellPath(t), "-NoLogo", "-NoProfile", "-NonInteractive", "-File", applyPath)
	apply.Dir = project
	apply.Env = append(os.Environ(),
		"ProgramData="+programData,
		"XDG_CONFIG_HOME="+configRoot,
		"XDG_DATA_HOME="+dataRoot,
		"USERPROFILE="+home,
		"HOME="+home,
		"OPENCODE_TEST_MANAGED_CONFIG_DIR="+filepath.Join(programData, "opencode"),
		"OPENCODE_CONFIG=",
		"OPENCODE_CONFIG_DIR=",
		"OPENCODE_CONFIG_CONTENT=",
	)
	if output, err := apply.CombinedOutput(); err != nil {
		t.Fatalf("apply OpenCode policy for externally installed CLI: %v: %s", err, output)
	}
	managed := filepath.Join(programData, "opencode")

	command := hiddenCommand(opencode, "debug", "config")
	command.Dir = project
	command.Env = append(os.Environ(),
		"XDG_CONFIG_HOME="+configRoot,
		"XDG_DATA_HOME="+dataRoot,
		"USERPROFILE="+home,
		"HOME="+home,
		"OPENCODE_TEST_MANAGED_CONFIG_DIR="+managed,
		"OPENCODE_CONFIG=",
		"OPENCODE_CONFIG_DIR=",
		"OPENCODE_CONFIG_CONTENT=",
	)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("OpenCode managed plugin regression: %v: %s", err, output)
	}
	var resolved struct {
		Permission map[string]any `json:"permission"`
		Agent      map[string]struct {
			Permission map[string]any `json:"permission"`
		} `json:"agent"`
	}
	if err := json.Unmarshal(output, &resolved); err != nil {
		t.Fatalf("decode OpenCode config: %v: %s", err, output)
	}
	if len(resolved.Permission) != 18 {
		t.Fatalf("resolved top-level permissions = %#v", resolved.Permission)
	}
	for name, value := range resolved.Permission {
		if value != "allow" {
			t.Fatalf("top-level permission %s = %#v", name, value)
		}
	}
	for _, agentName := range []string{"locked", "project"} {
		if len(resolved.Agent[agentName].Permission) != 18 {
			t.Fatalf("resolved agent permissions %s = %#v", agentName, resolved.Agent[agentName].Permission)
		}
	}
	for agentName, agent := range resolved.Agent {
		for permissionName, value := range agent.Permission {
			if value != "allow" {
				t.Fatalf("agent permission %s/%s = %#v", agentName, permissionName, value)
			}
		}
	}
}

func TestDefaultBaseInstallsStarshipFZFRipgrepAndPinnedGeistMonoBeforeTerminal(t *testing.T) {
	text := readDefaultBaseProvisioning(t)
	for _, required := range []string{
		"-Role 'Starship' -Id 'Starship.Starship'",
		"-InstallerType 'zip' -Adapter 'Portable' -ExecutableName 'starship.exe'",
		"Assert-ProvisioningCommand -Role 'Starship' -Name 'starship.exe'",
		"Documents\\PowerShell\\profile.ps1",
		"Invoke-Expression (&starship init powershell)",
		"-Role 'fzf' -Id 'junegunn.fzf'",
		"-InstallerType 'zip' -Adapter 'Portable' -ExecutableName 'fzf.exe'",
		"Assert-ProvisioningCommand -Role 'fzf' -Name 'fzf.exe'",
		"-Role 'ripgrep' -Id 'BurntSushi.ripgrep.MSVC'",
		"-InstallerType 'zip' -Adapter 'Portable' -ExecutableName 'rg.exe'",
		"Assert-ProvisioningCommand -Role 'ripgrep' -Name 'rg.exe'",
		"https://github.com/ryanoasis/nerd-fonts/releases/download/v3.4.0/GeistMono.zip",
		"A9F61B7B7F0429DB4FA9A526940F71190127ED95DBE3533163D80D7CAFDB3EC9",
		"-DownloadSource 'Direct' -Adapter 'GeistMonoFont'",
		"function Test-ProvisioningGeistMonoFontPayload",
		"if (-not (Test-ProvisioningGeistMonoFontPayload -Metadata $Metadata))",
		"payload already matches; loading it into the current Windows session",
		"AddFontResourceExW",
		"EnumFontFamiliesExW",
		"HasFamily('GeistMono NF')",
		"SendMessageTimeoutW",
		"0x001d",
		"GeistMono Nerd Font ready:",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("default Base is missing terminal-tool contract %q", required)
		}
	}
	starshipIndex := strings.Index(text, "Write-Output 'Installing Starship...'")
	fzfIndex := strings.Index(text, "Write-Output 'Installing fzf...'")
	ripgrepIndex := strings.Index(text, "Write-Output 'Installing ripgrep...'")
	fontIndex := strings.Index(text, "Write-Output 'Installing GeistMono Nerd Font...'")
	terminalIndex := strings.Index(text, "Write-Output 'Installing Windows Terminal...'")
	if starshipIndex < 0 || fzfIndex <= starshipIndex || ripgrepIndex <= fzfIndex || fontIndex <= ripgrepIndex || terminalIndex <= fontIndex {
		t.Fatalf("Starship/fzf/ripgrep/font/Terminal ordering is invalid: Starship=%d fzf=%d ripgrep=%d font=%d Terminal=%d", starshipIndex, fzfIndex, ripgrepIndex, fontIndex, terminalIndex)
	}
}

func TestDefaultBaseUsesCanonicalGuestLayoutAndExactGitTrust(t *testing.T) {
	text := readDefaultBaseProvisioning(t)
	for _, required := range []string{
		`C:\HerdrSandbox\cache`,
		`C:\HerdrSandbox\tools`,
		`C:\HerdrSandbox\staging\packages`,
		"Workspace manifest has an unsupported Base contract",
		"'config', '--global', '--replace-all', 'safe.directory'",
		"'config', '--global', '--add', 'safe.directory'",
		"Git safe-directory verification failed",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("default Base is missing guest-layout/Git contract %q", required)
		}
	}
	for _, forbidden := range []string{"safe.directory', '*'"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("default Base contains obsolete or wildcard guest contract %q", forbidden)
		}
	}
}

func TestDefaultBaseInstallsPowerShell7WithoutUsingItForProvisioningAndRestartsExplorerForRegistryAndTaskbarChanges(t *testing.T) {
	text := readDefaultBaseProvisioning(t)
	for _, required := range []string{
		"[ValidateSet('Registry', 'Development')]",
		"if ($Phase -eq 'Registry')",
		"-Role 'PowerShell 7' -Id 'Microsoft.PowerShell'",
		"-InstallerType 'msix' -Adapter 'MSIX' -ExecutableName 'pwsh.exe'",
		"function Get-ProvisioningPowerShell7Installation",
		"Get-AppxPackage -Name 'Microsoft.PowerShell'",
		"$file.VersionInfo.ProductVersion",
		"Get-AuthenticodeSignature -LiteralPath $executable",
		"function Restart-ProvisioningExplorerShell",
		"$currentSessionID = [int](Get-Process -Id $PID).SessionId",
		"Winlogon replacement did not become the registered interactive Explorer shell",
		"New-Object -ComObject 'Schedule.Service'",
		"$definition.Principal.LogonType = 3",
		"$taskRoot.RegisterTaskDefinition",
		"$taskRoot.DeleteTask($taskName, 0)",
		"Stopping all Explorer processes after $Role",
		"$explorerProcesses.Count -eq 0",
		"Starting one fresh Explorer shell",
		"Start-Process -FilePath (Join-Path $env:WINDIR 'explorer.exe')",
		"Explorer shell restarted:",
		"Restart-ProvisioningExplorerShell -Role 'registry changes'",
		"Restart-ProvisioningExplorerShell -Role 'taskbar policy change'",
		"Registry state already matches; Explorer restart skipped.",
		"early registry customization",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("default Base is missing PowerShell 7 contract %q", required)
		}
	}
	for _, forbidden := range []string{
		"Get-Command 'pwsh.exe'",
		"-FilePath 'pwsh.exe'",
		"-FilePath $powerShellCommand",
		"$PROFILE.CurrentUserAllHosts",
		"-Name 'pwsh.exe' -VersionArguments",
	} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("default Base executes PowerShell 7 during provisioning with %q", forbidden)
		}
	}
	stopIndex := strings.LastIndex(text, "$explorerProcesses | Stop-Process -Force")
	zeroIndex := strings.LastIndex(text, "if ($explorerProcesses.Count -eq 0) { break }")
	startIndex := strings.LastIndex(text, "Start-Process -FilePath (Join-Path $env:WINDIR 'explorer.exe')")
	if stopIndex < 0 || zeroIndex < 0 || startIndex < 0 || zeroIndex > stopIndex || startIndex < stopIndex {
		t.Fatalf("Explorer stop/zero/start ordering is not explicit: zero=%d stop=%d start=%d", zeroIndex, stopIndex, startIndex)
	}
	if count := strings.Count(text, "Restart-ProvisioningExplorerShell -Role"); count != 2 {
		t.Fatalf("Base invokes Explorer restart %d times, want exactly 2", count)
	}
	powerShellInstallIndex := strings.Index(text, "Write-Output 'Installing PowerShell 7...'")
	registryReturnIndex := strings.Index(text, "Write-ProvisioningTiming -Role 'early registry customization'")
	restartCallIndex := strings.Index(text, "Restart-ProvisioningExplorerShell -Role 'registry changes'")
	searchVisibilityIndex := strings.Index(text, "SearchboxTaskbarMode = 0")
	weatherVisibilityIndex := strings.Index(text, "AllowNewsAndInterests = 0")
	pinLayoutIndex := strings.LastIndex(text, "Ensure-ProvisioningTaskbarPins -Edition $WindowsTerminalEdition")
	if searchVisibilityIndex < 0 || weatherVisibilityIndex < 0 || pinLayoutIndex < 0 ||
		restartCallIndex < startIndex || restartCallIndex < searchVisibilityIndex || restartCallIndex < weatherVisibilityIndex ||
		registryReturnIndex < restartCallIndex || powerShellInstallIndex < registryReturnIndex || pinLayoutIndex < powerShellInstallIndex {
		t.Fatalf("early taskbar visibility/restart and later pin ordering is invalid: explorerDefinition=%d search=%d weather=%d restart=%d registryReturn=%d PowerShell=%d pins=%d", startIndex, searchVisibilityIndex, weatherVisibilityIndex, restartCallIndex, registryReturnIndex, powerShellInstallIndex, pinLayoutIndex)
	}
}

func TestDefaultBaseSeparatesAudioOutputFromMicrophoneInput(t *testing.T) {
	text := readDefaultBaseProvisioning(t)
	for _, required := range []string{
		"[switch]$AudioOutputEnabled",
		"[switch]$AudioInputEnabled",
		"function Initialize-ProvisioningAudioEndpointType",
		"interface IMMDeviceEnumerator",
		"interface IAudioEndpointVolume",
		"SilenceDefaultRenderEndpoint",
		"SetMasterVolumeLevelScalar(0.0f, IntPtr.Zero)",
		"SetMute(true, IntPtr.Zero)",
		"function Disable-ProvisioningAudioPlayback",
		"param([switch]$KeepAudioServices)",
		"AppEvents\\Schemes",
		"-Name '' -Value '.None' -PropertyType 'String'",
		"$serviceNames = @('Audiosrv', 'AudioEndpointBuilder')",
		"Set-Service -Name $serviceName -StartupType Disabled",
		"$controller.WaitForStatus(",
		"[System.ServiceProcess.ServiceControllerStatus]::Stopped",
		"Get-CimInstance -ClassName Win32_Service",
		"Audio disable policy failed",
		"if (-not $AudioOutputEnabled)",
		"Disable-ProvisioningAudioPlayback -KeepAudioServices:$AudioInputEnabled",
		"if (-not $KeepAudioServices)",
		"shared audio services retained for microphone input",
		"$AudioInputEnabled -and $capability -ceq 'microphone'",
		"$consentValue = 'Allow'",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("default Base is missing audio policy contract %q", required)
		}
	}
	if strings.LastIndex(text, "Disable-ProvisioningAudioPlayback") > strings.Index(text, "Restart-ProvisioningExplorerShell -Role 'registry changes'") {
		t.Fatal("default-silent audio policy must complete in the early Registry phase")
	}
}

func TestAudioEndpointInteropCompilesInWindowsPowerShell51(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows PowerShell 5.1 audio interop regression")
	}
	basePath := defaultProvisioningPath(t, baseProvisioningName)
	quote := func(value string) string { return strings.ReplaceAll(value, "'", "''") }
	script := fmt.Sprintf(`$ErrorActionPreference = 'Stop'
trap { Write-Output ($_ | Out-String); exit 1 }
$tokens = $null
$errors = $null
$ast = [Management.Automation.Language.Parser]::ParseFile('%s', [ref]$tokens, [ref]$errors)
if ($errors.Count -ne 0) { throw $errors[0].Message }
$definition = $ast.Find({ param($node) $node -is [Management.Automation.Language.FunctionDefinitionAst] -and $node.Name -ceq 'Initialize-ProvisioningAudioEndpointType' }, $true)
if ($null -eq $definition) { throw 'Missing audio endpoint interop initializer.' }
Invoke-Expression $definition.Extent.Text
Initialize-ProvisioningAudioEndpointType
if ($null -eq ('HerdrSandbox.AudioPolicy' -as [type])) {
    throw 'Audio endpoint interop types were not compiled.'
}
`, quote(basePath))
	scriptPath := filepath.Join(t.TempDir(), "audio-interop-regression.ps1")
	if err := os.WriteFile(scriptPath, []byte(script), 0o600); err != nil {
		t.Fatal(err)
	}
	command := hiddenCommand(mustWindowsPowerShellPath(t), "-NoLogo", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File", scriptPath)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("audio endpoint interop regression: %v: %s", err, output)
	}
}

func TestDefaultBasePrivacySetMatchesSelectedTypedProfile(t *testing.T) {
	text := readDefaultBaseProvisioning(t)
	normalized := strings.Join(strings.Fields(text), " ")
	for _, required := range []string{
		"Applying reviewed Windows UI and privacy settings",
		"SilentInstalledAppsEnabled = 0",
		"DisableSearchBoxSuggestions = 1",
		"DisableWebSearch = 1",
		"EnableDynamicContentInWSB = 0",
		"DoNotShowFeedbackNotifications = 1",
		"DisableFileSyncNGSC = 1",
		"HttpAcceptLanguageOptOut = 1",
		"RestrictImplicitInkCollection = 1",
		"$privacyConsentCapabilities",
		"$consentValue = 'Deny'",
		"Values = [ordered]@{ Value = $consentValue }",
		"Hidden = 1; HideFileExt = 0",
		"NavPaneExpandToCurrentFolder = 1",
		"FullPath = 1",
		"NOC_GLOBAL_SETTING_ALLOW_TOASTS_ABOVE_LOCK = 0",
		"IsAADCloudSearchEnabled = 0",
		"SyncPolicy = 5",
		"PreventDeviceMetadataFromNetwork = 1",
		"PersonalizationReportingEnabled = 0",
		"EnableMmx = 0",
		"AllowNewsAndInterests = 0",
		"EnableFeeds = 0",
		"EnableWebContentEvaluation = 0",
		"DODownloadMode = 0",
		"DeferUpdatePeriod = 0; DeferUpgrade = 1; DeferUpgradePeriod = 1",
		"ExcludeWUDriversInQualityUpdate = 1",
		"NoAutoUpdate = 1",
		"RegisteredWithAU = 0",
		"DisableAntiSpyware = 1",
		"SpyNetReporting = 0; SubmitSamplesConsent = 2",
		"NoGenTicket = 1",
		"PreviousUninstall = 1",
		"EnableActiveProbing = 0",
		"Status = 0",
		"SiteSafetyServicesEnabled = 0",
		"SmartScreenEnabled = 0",
		"TyposquattingCheckerEnabled = 0",
		"DisableOnline = 1",
		"AITEnable = 0; DisableInventory = 1; DisableUAR = 1",
		"Use FormSuggest' = 'no'",
		"EnableEncryptedMediaExtensions = 0",
		"OptimizeWindowsSearchResultsForScreenReaders = 0",
		"EnableCortana = 0",
		"$propertyType = 'String'",
		"function Ensure-ProvisioningRegistryKey",
		"Ensure-ProvisioningRegistryKey -Path $parent",
		"function Set-ProvisioningRegistryValue",
		"Set-ProvisioningRegistryValue -Path $group.Path",
		"Registry key creation did not materialize",
		"Privacy registry write failed",
		"Registry value verification failed",
	} {
		if !strings.Contains(normalized, required) {
			t.Fatalf("default Base is missing curated privacy contract %q", required)
		}
	}
	for _, forbidden := range []string{
		"AutoLogger-Diagtrack-Listener",
		"wuauserv",
		"WOW6432Node",
		"TaskbarDa",
		"ShellFeedsTaskbarViewMode",
		"GlobalUserDisabled",
		"BackgroundAccessApplications",
	} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("default Base contains excluded security-sensitive setting %q", forbidden)
		}
	}
}

func TestDefaultBasePreservesSelectedChromiumEdgePrivacyPolicies(t *testing.T) {
	normalized := strings.Join(strings.Fields(readDefaultBaseProvisioning(t)), " ")
	selected := map[string]int{
		"ConfigureDoNotTrack":                            1,
		"PaymentMethodQueryEnabled":                      0,
		"SendSiteInfoToImproveServices":                  0,
		"MetricsReportingEnabled":                        0,
		"PersonalizationReportingEnabled":                0,
		"AddressBarMicrosoftSearchInBingProviderEnabled": 0,
		"UserFeedbackAllowed":                            0,
		"AutofillCreditCardEnabled":                      0,
		"AutofillAddressEnabled":                         0,
		"LocalProvidersEnabled":                          0,
		"SearchSuggestEnabled":                           0,
		"BrowserSignin":                                  0,
		"MicrosoftEditorProofingEnabled":                 0,
		"ResolveNavigationErrorsUseWebService":           0,
		"AlternateErrorPagesEnabled":                     0,
		"NetworkPredictionOptions":                       2,
		"PasswordManagerEnabled":                         0,
		"SiteSafetyServicesEnabled":                      0,
		"SmartScreenEnabled":                             0,
		"TyposquattingCheckerEnabled":                    0,
		"HubsSidebarEnabled":                             0,
		"WebWidgetAllowed":                               0,
	}
	for name, value := range selected {
		needle := fmt.Sprintf("%s = %d", name, value)
		if !strings.Contains(normalized, needle) {
			t.Errorf("default Base is missing selected Chromium Edge policy %s", needle)
		}
	}
}

func TestDefaultBaseAppliesSelectedEdgePoliciesToUserAndMachineScopes(t *testing.T) {
	text := readDefaultBaseProvisioning(t)
	normalized := strings.Join(strings.Fields(text), " ")
	for _, required := range []string{
		"$edgeMachinePolicyPath = 'HKLM:\\SOFTWARE\\Policies\\Microsoft\\Edge'",
		"$edgeUserPolicyPath = 'HKCU:\\SOFTWARE\\Policies\\Microsoft\\Edge'",
		"foreach ($path in @($edgeMachinePolicyPath, $edgeUserPolicyPath))",
		"Set-ProvisioningRegistryValue -Path $path",
	} {
		if !strings.Contains(text, required) {
			t.Errorf("default Base is missing dual-scope Edge ownership contract %q", required)
		}
	}
	if strings.Contains(text, "[ordered]@{ Path = 'HKLM:\\SOFTWARE\\Policies\\Microsoft\\Edge'; Values") {
		t.Fatal("selected Edge privacy policies still use a duplicate machine-only privacy table")
	}
	for _, name := range []string{
		"AddressBarMicrosoftSearchInBingProviderEnabled",
		"ConfigureDoNotTrack",
		"EdgeShoppingAssistantEnabled",
		"HubsSidebarEnabled",
		"MetricsReportingEnabled",
		"SendSiteInfoToImproveServices",
		"WebWidgetAllowed",
	} {
		if count := strings.Count(normalized, name+" ="); count != 1 {
			t.Errorf("selected per-user Edge policy %s has %d assignments, want 1", name, count)
		}
	}
	if strings.Contains(text, "$capability -eq 'location'") {
		t.Fatal("machine-wide location consent is still skipped")
	}
}

func TestDefaultBasePreservesAdditionalSelectedPrivacyPolicies(t *testing.T) {
	normalized := strings.Join(strings.Fields(readDefaultBaseProvisioning(t)), " ")
	selected := map[string]int{
		"LetAppsAccessLocation": 2,
		"AllowTelemetry":        0,
		"DiagnosticData":        0,
		"DisableLocation":       1,
		"Status":                0,
		"NoAutoUpdate":          1,
	}
	for name, value := range selected {
		needle := fmt.Sprintf("%s = %d", name, value)
		if !strings.Contains(normalized, needle) {
			t.Errorf("default Base is missing selected Windows privacy policy %s", needle)
		}
	}
}

func TestDefaultBaseUsesSupportedIdempotentTaskbarPolicy(t *testing.T) {
	text := readDefaultBaseProvisioning(t)
	for _, required := range []string{
		"function Ensure-ProvisioningTaskbarPins",
		"MDM_Policy_User_Config01_Start02",
		`root\cimv2\mdm\dmmap`,
		"./Vendor/MSFT/Policy/Config",
		`<CustomTaskbarLayoutCollection PinListPlacement="Replace">`,
		`<taskbar:DesktopApp DesktopApplicationID="MSEdge" />`,
		`<taskbar:DesktopApp DesktopApplicationID="Microsoft.Windows.Explorer" />`,
		"<taskbar:UWA AppUserModelID=\"",
		"<taskbar:DesktopApp DesktopApplicationID=\"WinDirStat\" />",
		`<taskbar:DesktopApp DesktopApplicationLinkPath="%APPDATA%\Microsoft\Windows\Start Menu\Programs\File Pilot.lnk" />`,
		`<taskbar:DesktopApp DesktopApplicationLinkPath="%APPDATA%\Microsoft\Windows\Start Menu\Programs\TradingView.lnk" />`,
		"HerdrSandboxToolVersionPlan.Versions.ContainsKey('TradingView.TradingViewDesktop')",
		"[Net.WebUtility]::HtmlEncode($layout)",
		"StartLayout -ceq $layout",
		"Taskbar policy read-back did not match the canonical decoded layout",
		"Restart-ProvisioningExplorerShell -Role 'taskbar policy change'",
		"Ensure-ProvisioningTaskbarPins -Edition $WindowsTerminalEdition",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("default Base is missing supported taskbar contract %q", required)
		}
	}
	for _, forbidden := range []string{"Taskband", "Shell.Application", "InvokeVerb", "UIAutomation"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("default Base contains unsupported taskbar fallback %q", forbidden)
		}
	}
	taskbarStart := strings.Index(text, "function Ensure-ProvisioningTaskbarPins")
	if taskbarStart < 0 {
		t.Fatal("taskbar function boundaries are missing")
	}
	taskbarEnd := strings.Index(text[taskbarStart:], "function Initialize-ProvisioningAudioEndpointType")
	if taskbarEnd < 0 {
		t.Fatal("taskbar function boundaries are missing")
	}
	taskbar := text[taskbarStart : taskbarStart+taskbarEnd]
	edgeIndex := strings.Index(taskbar, `DesktopApplicationID="MSEdge"`)
	explorerIndex := strings.Index(taskbar, `DesktopApplicationID="Microsoft.Windows.Explorer"`)
	filePilotIndex := strings.Index(taskbar, `DesktopApplicationLinkPath="%APPDATA%\Microsoft\Windows\Start Menu\Programs\File Pilot.lnk"`)
	tradingViewIndex := strings.Index(taskbar, `DesktopApplicationLinkPath="%APPDATA%\Microsoft\Windows\Start Menu\Programs\TradingView.lnk"`)
	terminalIndex := strings.Index(taskbar, `<taskbar:UWA AppUserModelID="`)
	winDirStatIndex := strings.Index(taskbar, `DesktopApplicationID="WinDirStat"`)
	matchingIndex := strings.Index(taskbar, "Taskbar pins already match:")
	writeIndex := strings.Index(taskbar, "$encodedLayout = [Net.WebUtility]::HtmlEncode($layout)")
	readBackIndex := strings.Index(taskbar, "Taskbar policy read-back did not match the canonical decoded layout")
	restartIndex := strings.Index(taskbar, "Restart-ProvisioningExplorerShell -Role 'taskbar policy change'")
	if edgeIndex < 0 || explorerIndex < edgeIndex || filePilotIndex < explorerIndex || tradingViewIndex < filePilotIndex || terminalIndex < tradingViewIndex || winDirStatIndex < terminalIndex {
		t.Fatalf("taskbar pin order is invalid: Edge=%d Explorer=%d File Pilot=%d TradingView=%d Terminal=%d WinDirStat=%d", edgeIndex, explorerIndex, filePilotIndex, tradingViewIndex, terminalIndex, winDirStatIndex)
	}
	if matchingIndex < 0 || writeIndex < matchingIndex || readBackIndex < writeIndex || restartIndex < readBackIndex {
		t.Fatalf("taskbar match/write/read-back/restart ordering is invalid: match=%d write=%d readBack=%d restart=%d", matchingIndex, writeIndex, readBackIndex, restartIndex)
	}
}

func TestRetainedExplorerRestartTaskParsesInWindowsPowerShell51(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows PowerShell 5.1 regression")
	}
	text := readDefaultBaseProvisioning(t)
	const opening = "$restartScript = @'\n"
	start := strings.Index(text, opening)
	if start < 0 {
		t.Fatal("retained Explorer restart task script is missing")
	}
	start += len(opening)
	end := strings.Index(text[start:], "\n'@")
	if end < 0 {
		t.Fatal("retained Explorer restart task terminator is missing")
	}
	nested := text[start : start+end]
	for placeholder, replacement := range map[string]string{
		"__OWNER_PID__":   "123",
		"__RESTART_ID__":  "20260801-080000-1234abcd",
		"__STATUS_PATH__": `C:\SandboxStatus\explorer-restart.json`,
		"__TASK_NAME__":   "HerdrSandbox-ExplorerRestart-20260801-080000-1234abcd",
		"__SESSION_ID__":  "1",
	} {
		nested = strings.ReplaceAll(nested, placeholder, replacement)
	}
	payload := base64.StdEncoding.EncodeToString([]byte(nested))
	script := fmt.Sprintf(`$source = [Text.Encoding]::UTF8.GetString([Convert]::FromBase64String('%s'))
$tokens = $null
$errors = $null
[void][Management.Automation.Language.Parser]::ParseInput($source, [ref]$tokens, [ref]$errors)
if ($errors.Count -ne 0) { throw ($errors | ForEach-Object Message | Out-String) }
`, payload)
	command := hiddenCommand(mustWindowsPowerShellPath(t), "-NoLogo", "-NoProfile", "-NonInteractive", "-EncodedCommand", encodePowerShell(script))
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("retained Explorer restart task parse: %v: %s", err, output)
	}
}

func TestDefaultBaseSkipsMatchingPackageAndConfigurationState(t *testing.T) {
	text := readDefaultBaseProvisioning(t)
	for _, required := range []string{
		"function Assert-ProvisioningCacheTree",
		"Assert-ProvisioningCacheTree -Path $directory.FullName",
		"function Test-ProvisioningWinGetPackageInstalled",
		"function Confirm-ProvisioningWinGetReadback",
		"function Test-ProvisioningPortablePackageInstalled",
		"function Test-ProvisioningRustupInstalled",
		"function Test-ProvisioningGeistMonoFontInstalled",
		"Test-ProvisioningPackageInstalled -Metadata $metadata -Adapter $Adapter -ExecutableName $ExecutableName",
		"if (Test-ProvisioningWinGetPackageInstalled -Metadata $metadata)",
		"already matches requested version:",
		"online package already matches requested version:",
		"installed package does not match resolved version",
		"installation command succeeded, but WinGet could not confirm package",
		"[IO.File]::ReadAllText($powerShellProfilePath) -cne $expectedPowerShellProfile",
		"if (($existingSafeDirectories -join '|') -cne ($guestSafeDirectories -join '|'))",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("default Base is missing exact-state idempotence contract %q", required)
		}
	}
}

func TestProvisioningCacheTreeRejectsNestedReparseInWindowsPowerShell51(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows PowerShell 5.1 cache-tree regression")
	}
	cacheRoot := filepath.Join(t.TempDir(), "cache")
	candidate := filepath.Join(cacheRoot, "candidate")
	outside := t.TempDir()
	if err := os.MkdirAll(candidate, 0o700); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(outside, "keep.txt")
	if err := os.WriteFile(marker, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	createTestDirectoryLink(t, filepath.Join(candidate, "outside-link"), outside)
	quote := func(value string) string { return strings.ReplaceAll(value, "'", "''") }
	script := fmt.Sprintf(`$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'
trap { Write-Output ($_ | Out-String); exit 1 }
$tokens = $null
$errors = $null
$ast = [Management.Automation.Language.Parser]::ParseFile('%s', [ref]$tokens, [ref]$errors)
foreach ($name in @('Assert-ProvisioningCachePath', 'Assert-ProvisioningCacheTree')) {
    $definition = $ast.Find({ param($node) $node -is [Management.Automation.Language.FunctionDefinitionAst] -and $node.Name -ceq $name }, $true)
    if ($null -eq $definition) { throw "Missing function $name" }
    Invoke-Expression $definition.Extent.Text.Replace('C:\HerdrSandbox\cache', '%s')
}
$accepted = $false
try {
    Assert-ProvisioningCacheTree -Path '%s'
    $accepted = $true
} catch {
    if ([string]$_.Exception.Message -notmatch 'reparse point') { throw }
}
if ($accepted) { throw 'Nested cache alias was accepted.' }
exit 0
`, quote(defaultProvisioningPath(t, baseProvisioningName)), quote(cacheRoot), quote(candidate))
	command := hiddenCommand(mustWindowsPowerShellPath(t), "-NoLogo", "-NoProfile", "-NonInteractive", "-EncodedCommand", encodePowerShell(script))
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("cache-tree regression: %v: %s", err, output)
	}
	if contents, err := os.ReadFile(marker); err != nil || string(contents) != "keep" {
		t.Fatalf("outside cache target changed: %q, %v", contents, err)
	}
}

func TestDefaultStackLibraryExposesFineGrainedFunctionsAndHerdrVirtualStack(t *testing.T) {
	text := readDefaultStackProvisioning(t)
	for _, required := range []string{
		stackProvisioningContract,
		"function Install-AndroidStack",
		"function Resolve-StackPythonPackage",
		"function Resolve-StackRustDistribution",
		"function Install-CppStack",
		"function Install-GoStack",
		"function Install-JavaStack",
		"function Install-NodeRuntime",
		"function Install-PlaywrightChromium",
		"function Install-NodeStack",
		"function Install-NSISStack",
		"function Install-NushellStack",
		"function Install-PlaywrightCLIStack",
		"function Install-PythonStack",
		"function Install-Uv",
		"function Install-PythonAIStack",
		"function Install-ZigStack",
		"function Install-RustMSVCStack",
		"function Install-CargoNextest",
		"function Install-Just",
		"function Install-BunStack",
		"function Install-HandyStack",
		"function Install-HerdrStack",
		"function Install-StackVisualStudioBuildTools",
		"Assert-ProvisioningCacheTree -Path $slot",
		"Get-StackRustManifestSnapshot -Channel 'stable'",
		"-Id 'OpenJS.NodeJS.LTS'",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("stack library is missing %q", required)
		}
	}
	if strings.Contains(text, "1.96.1") || strings.Contains(text, "[string]$Series = '3.13'") {
		t.Fatal("stack library retains a hard-coded omitted-version default")
	}
	for _, forbidden := range []string{
		"function Get-Herdr",
		"function Test-Herdr",
		"function Assert-Herdr",
		"function Invoke-Herdr",
		"function Wait-Herdr",
		"function Copy-Herdr",
		"function Set-Herdr",
	} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("stack library retains repository-specific function prefix %q", forbidden)
		}
	}
	rustStart := strings.Index(text, "function Install-RustMSVCStack")
	rustEnd := strings.Index(text, "function Install-CargoNextest")
	zigStart := strings.Index(text, "function Install-ZigStack")
	if rustStart < 0 || rustEnd <= rustStart || zigStart < 0 || zigStart >= rustStart {
		t.Fatalf("stack function ordering is invalid: zig=%d rust=%d nextest=%d", zigStart, rustStart, rustEnd)
	}
	rust := text[rustStart:rustEnd]
	for _, unrelated := range []string{"-Id 'zig.zig'", "-Id 'nextest.cargo-nextest'", "-Id 'Casey.Just'", "-Id 'Oven-sh.Bun'"} {
		if strings.Contains(rust, unrelated) {
			t.Fatalf("Rust stack contains unrelated package %q", unrelated)
		}
	}
	if !strings.Contains(rust, "-Id 'Rustlang.Rustup'") || !strings.Contains(rust, "Install-StackVisualStudioBuildTools") {
		t.Fatal("Rust stack lost rustup or Visual Studio ownership")
	}
	if !strings.Contains(rust, "Get-ProvisioningToolVersion -Tool 'rust-toolchain'") ||
		strings.Contains(rust, "Get-ProvisioningToolVersion -Tool 'Rustlang.Rustup' -Requested $Toolchain") {
		t.Fatal("Rust stack does not keep the Rust toolchain channel separate from the rustup package version")
	}
	base := readDefaultBaseProvisioning(t)
	dotSource := strings.Index(base, ". $stackProvisioning")
	userCall := strings.Index(base, "& $userProvisioning.FullName")
	projectCall := strings.Index(base, "& $projectScript.FullName -ProjectDirectory $projectDirectory")
	if dotSource < 0 || userCall <= dotSource || projectCall <= userCall {
		t.Fatalf("provisioning order must be stack, user, project: stack=%d user=%d project=%d", dotSource, userCall, projectCall)
	}
}

func TestRustAndVisualStudioUseOneFixedParallelGroup(t *testing.T) {
	text := readDefaultStackProvisioning(t)
	visualStudioStart := strings.Index(text, "function Install-StackVisualStudioBuildTools")
	visualStudioEnd := strings.Index(text, "function Get-StackRustSHA256")
	rustStart := strings.Index(text, "function Install-RustMSVCStack")
	rustEnd := strings.Index(text, "function Install-CargoNextest")
	if visualStudioStart < 0 || visualStudioEnd <= visualStudioStart || rustStart < 0 || rustEnd <= rustStart {
		t.Fatal("Rust or Visual Studio stack owner is missing")
	}
	visualStudio := text[visualStudioStart:visualStudioEnd]
	rust := text[rustStart:rustEnd]
	for _, required := range []string{
		"[Collections.IDictionary]$RustToolchainTask",
		"Start-ProvisioningNativeGroup -Tasks @(",
		"$RustToolchainTask,",
		"Role = 'Visual Studio Build Tools offline installation'",
		"TimeoutSeconds = 900",
		"Complete-ProvisioningNativeGroup -Group $installationGroup",
		"Stop-ProvisioningNativeGroup -Group $installationGroup",
		"Wait-StackVisualStudioInstalled",
	} {
		if !strings.Contains(visualStudio, required) {
			t.Errorf("Visual Studio parallel owner is missing %q", required)
		}
	}
	for _, required := range []string{
		"$rustDistServer = ([Uri][IO.Path]::GetFullPath($rustMirrorRoot)).AbsoluteUri.TrimEnd('/')",
		"$rustDistURI.Scheme -cne 'file'",
		"$env:RUSTUP_DIST_SERVER = $rustDistServer",
		"Role = 'Rust toolchain installation'",
		"WorkingDirectory = $ProjectDirectory",
		"Install-StackVisualStudioBuildTools -RustToolchainTask $rustToolchainTask",
		"Invoke-ProvisioningNative -Role 'Rust default toolchain selection'",
		"Assert-ProvisioningCommand -Role 'Rust'",
	} {
		if !strings.Contains(rust, required) {
			t.Errorf("Rust parallel owner is missing %q", required)
		}
	}
	if strings.Contains(text, "function Invoke-StackVisualStudioInstaller") ||
		strings.Contains(rust, "Invoke-ProvisioningNative -Role 'Rust toolchain installation'") ||
		strings.Contains(rust, "python.exe") ||
		strings.Contains(rust, "http.server") ||
		strings.Contains(rust, "Rust distribution mirror server") ||
		strings.Contains(rust, "Start-Process -FilePath $pythonCommand.Source") ||
		strings.Contains(visualStudio, "Start-Process -FilePath $guestLayoutBootstrapper") {
		t.Fatal("Rust/Visual Studio retains a replaced direct process path")
	}
	fileMirror := strings.Index(rust, "$env:RUSTUP_DIST_SERVER = $rustDistServer")
	groupCall := strings.Index(rust, "Install-StackVisualStudioBuildTools -RustToolchainTask")
	defaultSelection := strings.Index(rust, "Invoke-ProvisioningNative -Role 'Rust default toolchain selection'")
	verification := strings.Index(rust, "Assert-ProvisioningCommand -Role 'Rust'")
	if fileMirror < 0 || groupCall <= fileMirror || defaultSelection <= groupCall || verification <= defaultSelection {
		t.Fatalf("Rust preparation/group/read-back ordering is invalid: mirror=%d group=%d default=%d verify=%d", fileMirror, groupCall, defaultSelection, verification)
	}
}

func TestDevelopmentNativeProcessesUseOwnedBoundary(t *testing.T) {
	base := readDefaultBaseProvisioning(t)
	stacks := readDefaultStackProvisioning(t)
	for _, required := range []string{
		"function Invoke-ProvisioningNativeResult",
		"[HerdrSandbox.ProvisioningProcess]::Run($spec)",
	} {
		if !strings.Contains(base+stacks, required) {
			t.Errorf("development process owner is missing %q", required)
		}
	}
	for _, forbidden := range []string{
		"@(& winget.exe list",
		"@(& $commands[0].FullName @VersionArguments",
		"@(& $executable --version",
		"@(& $openCodeCommand.Source 'debug' 'config'",
		"@(& $gitCommand config --global --get-all safe.directory",
		"@(& $vswhere '-latest'",
		"Start-Process -FilePath $pythonCommand.Source",
	} {
		if strings.Contains(base+stacks, forbidden) {
			t.Errorf("development path retains direct native invocation %q", forbidden)
		}
	}
}

func TestVisualStudioFirewallVerifierRejectsNarrowedFiltersInWindowsPowerShell51(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows PowerShell 5.1 firewall verifier regression")
	}
	stackPath := defaultProvisioningPath(t, stackProvisioningName)
	quote := func(value string) string { return strings.ReplaceAll(value, "'", "''") }
	script := fmt.Sprintf(`$ErrorActionPreference = 'Stop'
$tokens = $null
$errors = $null
$ast = [Management.Automation.Language.Parser]::ParseFile('%s', [ref]$tokens, [ref]$errors)
foreach ($name in @('Test-StackFirewallValue', 'Test-StackVisualStudioFirewallRule')) {
    $definition = $ast.Find({ param($node) $node -is [Management.Automation.Language.FunctionDefinitionAst] -and $node.Name -ceq $name }, $true)
    if ($null -eq $definition) { throw "Missing function $name" }
    Invoke-Expression $definition.Extent.Text
}
$script:Application = [pscustomobject]@{ Program = 'C:\fixture.exe'; Package = $null }
$script:Address = [pscustomobject]@{ LocalAddress = 'Any'; RemoteAddress = 'Any' }
$script:Port = [pscustomobject]@{ Protocol = 'Any'; LocalPort = 'Any'; RemotePort = 'Any'; IcmpType = 'Any'; DynamicTarget = $null }
$script:Service = [pscustomobject]@{ Service = 'Any' }
$script:Interface = [pscustomobject]@{ InterfaceAlias = 'Any' }
$script:InterfaceType = [pscustomobject]@{ InterfaceType = 'Any' }
$script:Security = [pscustomobject]@{ Authentication = 'NotRequired'; Encryption = 'NotRequired'; OverrideBlockRules = $false; LocalUser = 'Any'; RemoteUser = 'Any'; RemoteMachine = 'Any' }
function Get-NetFirewallApplicationFilter { process { $script:Application } }
function Get-NetFirewallAddressFilter { process { $script:Address } }
function Get-NetFirewallPortFilter { process { $script:Port } }
function Get-NetFirewallServiceFilter { process { $script:Service } }
function Get-NetFirewallInterfaceFilter { process { $script:Interface } }
function Get-NetFirewallInterfaceTypeFilter { process { $script:InterfaceType } }
function Get-NetFirewallSecurityFilter { process { $script:Security } }
$rule = [pscustomobject]@{
    Name = 'fixture'; DisplayName = 'fixture'; Enabled = 'True'; Profile = 'Any'; Direction = 'Outbound';
    Action = 'Block'; EdgeTraversalPolicy = 'Block'; LooseSourceMapping = $false; LocalOnlyMapping = $false; Owner = $null
}
if (-not (Test-StackVisualStudioFirewallRule -Rules @($rule) -Name 'fixture' -Direction 'Outbound' -Program 'C:\fixture.exe')) {
    throw 'Canonical Visual Studio firewall rule was rejected.'
}
$script:Address.RemoteAddress = 'LocalSubnet'
if (Test-StackVisualStudioFirewallRule -Rules @($rule) -Name 'fixture' -Direction 'Outbound' -Program 'C:\fixture.exe') {
    throw 'Narrowed Visual Studio firewall address filter was accepted.'
}
$script:Address.RemoteAddress = 'Any'
$rule.Profile = 'Private'
if (Test-StackVisualStudioFirewallRule -Rules @($rule) -Name 'fixture' -Direction 'Outbound' -Program 'C:\fixture.exe') {
    throw 'Narrowed Visual Studio firewall profile was accepted.'
}
`, quote(stackPath))
	command := hiddenCommand(mustWindowsPowerShellPath(t), "-NoLogo", "-NoProfile", "-NonInteractive", "-EncodedCommand", encodePowerShell(script))
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("Visual Studio firewall verifier regression: %v: %s", err, output)
	}
}

func TestLatestStackResolversInWindowsPowerShell51(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows PowerShell 5.1 resolver regression")
	}
	stackPath := defaultProvisioningPath(t, stackProvisioningName)
	quote := func(value string) string { return strings.ReplaceAll(value, "'", "''") }
	script := fmt.Sprintf(`$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'
trap { Write-Output ($_ | Out-String); exit 1 }
$tokens = $null
$errors = $null
$ast = [Management.Automation.Language.Parser]::ParseFile('%s', [ref]$tokens, [ref]$errors)
if ($errors.Count -ne 0) { throw $errors[0].Message }
foreach ($name in @('Resolve-StackPythonPackage', 'Get-StackRustSHA256', 'ConvertFrom-StackRustManifest')) {
    $definition = $ast.Find({ param($node) $node -is [Management.Automation.Language.FunctionDefinitionAst] -and $node.Name -ceq $name }, $true)
    if ($null -eq $definition) { throw "Missing function $name" }
    Invoke-Expression $definition.Extent.Text
}
function Search-ProvisioningWinGetPackages {
    param($Role, $IdQuery, [switch]$Exact)
    if ($Exact) { return @([pscustomobject]@{ Name='Python 3.10'; Id='Python.Python.3.10'; Version='3.10.18' }) }
    return @(
        [pscustomobject]@{ Name='Python 2'; Id='Python.Python.2'; Version='2.7.18150' },
        [pscustomobject]@{ Name='Python 3.9'; Id='Python.Python.3.9'; Version='3.9.23' },
        [pscustomobject]@{ Name='Python 3.14'; Id='Python.Python.3.14'; Version='3.14.6' },
        [pscustomobject]@{ Name='Python 3.10'; Id='Python.Python.3.10'; Version='3.10.18' }
    )
}
$latestPython = Resolve-StackPythonPackage -Series '' -Version ''
if ($latestPython.Series -cne '3.14' -or $latestPython.Version -cne '3.14.6') { throw 'Latest Python selection failed.' }
$seriesPython = Resolve-StackPythonPackage -Series '3.10' -Version ''
if ($seriesPython.Version -cne '3.10.18') { throw 'Python series selection failed.' }
$explicitPython = Resolve-StackPythonPackage -Series '' -Version '3.12.9'
if ($explicitPython.Series -cne '3.12') { throw 'Python version-derived series failed.' }
$accepted = $false
try { $null = Resolve-StackPythonPackage -Series '3.11' -Version '3.12.9'; $accepted = $true } catch { }
if ($accepted) { throw 'Conflicting Python series and version were accepted.' }

$hash = 'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa'
$manifest = @"
manifest-version = "2"
date = "2026-07-16"

[pkg.rust]
version = "1.97.1 (123456789 2026-07-16)"
git_commit_hash = "1234567890123456789012345678901234567890"

"@
foreach ($package in @('cargo', 'clippy-preview', 'rust-std', 'rustc', 'rustfmt-preview')) {
    $stem = if ($package -ceq 'clippy-preview') { 'clippy' } elseif ($package -ceq 'rustfmt-preview') { 'rustfmt' } else { $package }
    $newline = [Environment]::NewLine
    $manifest += '[pkg.' + $package + '.target.x86_64-pc-windows-msvc]' + $newline
    $manifest += 'available = true' + $newline
    $manifest += 'zst_url = "https://static.rust-lang.org/dist/2026-07-16/' + $stem + '-1.97.1-x86_64-pc-windows-msvc.tar.zst"' + $newline
    $manifest += 'zst_hash = "' + $hash + '"' + $newline
    $manifest += 'xz_url = "https://static.rust-lang.org/dist/2026-07-16/' + $stem + '-1.97.1-x86_64-pc-windows-msvc.tar.xz"' + $newline
    $manifest += 'xz_hash = "' + $hash + '"' + $newline + $newline
}
$utf8 = New-Object Text.UTF8Encoding($false, $true)
$selection = ConvertFrom-StackRustManifest -ManifestBytes $utf8.GetBytes($manifest) -ExpectedChannel 'stable' -Target 'x86_64-pc-windows-msvc'
if ($selection.Version -cne '1.97.1' -or @($selection.Payloads).Count -ne 5) { throw 'Rust stable manifest selection failed.' }
if (@($selection.Payloads | Where-Object { -not ([string]$_.RelativePath).EndsWith('.tar.zst', [StringComparison]::Ordinal) }).Count -ne 0) { throw 'Rust manifest did not select rustup preferred zstd payloads.' }
$accepted = $false
try { $null = ConvertFrom-StackRustManifest -ManifestBytes $utf8.GetBytes($manifest) -ExpectedChannel '1.96.1' -Target 'x86_64-pc-windows-msvc'; $accepted = $true } catch { }
if ($accepted) { throw 'Mismatched exact Rust manifest was accepted.' }
`, quote(stackPath))
	scriptPath := filepath.Join(t.TempDir(), "latest-stack-resolver.ps1")
	if err := os.WriteFile(scriptPath, []byte(script), 0o600); err != nil {
		t.Fatal(err)
	}
	command := hiddenCommand(mustWindowsPowerShellPath(t), "-NoLogo", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File", scriptPath)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("latest stack resolver regression: %v: %s", err, output)
	}
}

func TestRustMirrorCacheUsesResolvedIdentityInWindowsPowerShell51(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows PowerShell 5.1 Rust cache regression")
	}
	stackPath := defaultProvisioningPath(t, stackProvisioningName)
	root := t.TempDir()
	quote := func(value string) string { return strings.ReplaceAll(value, "'", "''") }
	script := fmt.Sprintf(`$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'
trap { Write-Output ($_ | Out-String); exit 1 }
$tokens = $null
$errors = $null
$ast = [Management.Automation.Language.Parser]::ParseFile('%s', [ref]$tokens, [ref]$errors)
if ($errors.Count -ne 0) { throw $errors[0].Message }
foreach ($name in @('Get-StackRustSHA256', 'Assert-StackRustMirrorPayloads', 'Test-StackRustMirrorCacheEntry')) {
    $definition = $ast.Find({ param($node) $node -is [Management.Automation.Language.FunctionDefinitionAst] -and $node.Name -ceq $name }, $true)
    if ($null -eq $definition) { throw "Missing function $name" }
    Invoke-Expression $definition.Extent.Text
}
function Get-FileHash {
    param([string]$LiteralPath, [string]$Algorithm)
    if ($Algorithm -cne 'SHA256') { throw "Unsupported fixture hash: $Algorithm" }
    return [pscustomobject]@{ Hash = Get-StackRustSHA256 -Bytes ([IO.File]::ReadAllBytes($LiteralPath)) }
}
$entry = '%s'
$mirror = Join-Path $entry 'mirror'
New-Item -ItemType Directory -Path $mirror -Force | Out-Null
$utf8 = New-Object Text.UTF8Encoding($false, $true)
$script:payloads = @()
function Add-TestPayload {
    param([string]$RelativePath, [byte[]]$Bytes)
    $path = Join-Path $mirror $RelativePath
    New-Item -ItemType Directory -Path (Split-Path -Parent $path) -Force | Out-Null
    [IO.File]::WriteAllBytes($path, $Bytes)
    $script:payloads += [pscustomobject]@{
        RelativePath = $RelativePath
        Sha256 = (Get-FileHash -LiteralPath $path -Algorithm SHA256).Hash.ToUpperInvariant()
    }
}
$manifestName = 'channel-rust-1.97.1.toml'
$manifestPath = "dist\$manifestName"
Add-TestPayload -RelativePath $manifestPath -Bytes $utf8.GetBytes('synthetic manifest')
$manifestHash = [string]$script:payloads[0].Sha256
Add-TestPayload -RelativePath ($manifestPath + '.sha256') -Bytes $utf8.GetBytes($manifestHash.ToLowerInvariant() + "  $manifestName" + [Environment]::NewLine)
$componentPath = ''
foreach ($component in @('cargo', 'clippy', 'rust-std', 'rustc', 'rustfmt')) {
    $relativePath = "dist\2026-07-16\$component-1.97.1-x86_64-pc-windows-msvc.tar.zst"
    Add-TestPayload -RelativePath $relativePath -Bytes $utf8.GetBytes("payload:$component")
    if ($component -ceq 'cargo') { $componentPath = $relativePath }
}
$metadata = [pscustomobject][ordered]@{
    schemaVersion = 1
    toolchain = '1.97.1'
    target = 'x86_64-pc-windows-msvc'
    manifestSha256 = $manifestHash
}
$descriptor = [ordered]@{
    schemaVersion = 1
    toolchain = '1.97.1'
    target = 'x86_64-pc-windows-msvc'
    manifestSha256 = $manifestHash
} | ConvertTo-Json -Compress
[IO.File]::WriteAllText((Join-Path $entry 'complete.json'), $descriptor, $utf8)
if (-not (Test-StackRustMirrorCacheEntry -EntryDirectory $entry -Payloads $script:payloads -Metadata $metadata)) {
    throw 'Resolved Rust cache identity was rejected.'
}
[IO.File]::WriteAllText((Join-Path $mirror $componentPath), 'tampered', $utf8)
if (Test-StackRustMirrorCacheEntry -EntryDirectory $entry -Payloads $script:payloads -Metadata $metadata) {
    throw 'Tampered Rust cache payload was accepted.'
}
`, quote(stackPath), quote(filepath.Join(root, "entry")))
	scriptPath := filepath.Join(root, "rust-cache-regression.ps1")
	if err := os.WriteFile(scriptPath, []byte(script), 0o600); err != nil {
		t.Fatal(err)
	}
	command := hiddenCommand(mustWindowsPowerShellPath(t), "-NoLogo", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File", scriptPath)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("Rust cache regression: %v: %s", err, output)
	}
}

func TestWinGetSearchParserInWindowsPowerShell51(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows PowerShell 5.1 WinGet parser regression")
	}
	basePath := defaultProvisioningPath(t, baseProvisioningName)
	quote := func(value string) string { return strings.ReplaceAll(value, "'", "''") }
	script := fmt.Sprintf(`$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'
trap { Write-Output ($_ | Out-String); exit 1 }
$tokens = $null
$errors = $null
$ast = [Management.Automation.Language.Parser]::ParseFile('%s', [ref]$tokens, [ref]$errors)
if ($errors.Count -ne 0) { throw $errors[0].Message }
$definition = $ast.Find({ param($node) $node -is [Management.Automation.Language.FunctionDefinitionAst] -and $node.Name -ceq 'Search-ProvisioningWinGetPackages' }, $true)
Invoke-Expression $definition.Extent.Text
function Invoke-ProvisioningNative {
    return @('Name        Id                 Version', '----------------------------------------', 'Python 3.14 Python.Python.3.14 3.14.6')
}
$rows = @(Search-ProvisioningWinGetPackages -Role 'Python' -IdQuery 'Python.Python.')
if ($rows.Count -ne 1 -or $rows[0].Id -cne 'Python.Python.3.14' -or $rows[0].Version -cne '3.14.6') { throw 'Canonical WinGet search output failed.' }
function Invoke-ProvisioningNative { return @('Nombre Id Version', '-----------------', 'Python Python.Python.3.14 3.14.6') }
$accepted = $false
try { $null = Search-ProvisioningWinGetPackages -Role 'Python' -IdQuery 'Python.Python.'; $accepted = $true } catch { }
if ($accepted) { throw 'Localized WinGet search output was accepted.' }
function Invoke-ProvisioningNative { return @('Failed in attempting to update the source: winget', 'Unexpected source detail') }
$sourceFailure = ''
try { $null = Search-ProvisioningWinGetPackages -Role 'File Pilot' -IdQuery 'Voidstar.FilePilot' -Exact } catch { $sourceFailure = [string]$_.Exception.Message }
if ($sourceFailure -cne 'File Pilot WinGet source update failed before package search: Failed in attempting to update the source: winget') {
    throw "WinGet source failure was not diagnosed directly: $sourceFailure"
}
`, quote(basePath))
	scriptPath := filepath.Join(t.TempDir(), "winget-search-parser.ps1")
	if err := os.WriteFile(scriptPath, []byte(script), 0o600); err != nil {
		t.Fatal(err)
	}
	command := hiddenCommand(mustWindowsPowerShellPath(t), "-NoLogo", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File", scriptPath)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("WinGet search parser regression: %v: %s", err, output)
	}
}

func TestOnlineWinGetPackageConcretizesLatestInWindowsPowerShell51(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows PowerShell 5.1 online WinGet regression")
	}
	basePath := defaultProvisioningPath(t, baseProvisioningName)
	functionSetup := provisioningPowerShellFunctionSetup(t, provisioningPowerShellFunctionSource{
		path:  basePath,
		names: []string{"Get-ProvisioningToolVersion", "Confirm-ProvisioningWinGetReadback", "Install-ProvisioningOnlineWinGetPackage"},
	})
	script := fmt.Sprintf(`$ErrorActionPreference = 'Stop'
trap { Write-Output ($_ | Out-String); exit 1 }
%s
$script:searchCalls = 0
$script:installArguments = @()
$script:installCalls = 0
$script:installedVersion = ''
$script:verifiedVersion = ''
function Search-ProvisioningWinGetPackages {
    param($Role, $IdQuery, [switch]$Exact)
    $script:searchCalls += 1
    if (-not $Exact) { throw 'Latest package search was not exact.' }
    return @([pscustomobject]@{ Name='Example'; Id='Example.Package'; Version='9.8.7' })
}
function Invoke-ProvisioningNative {
    param($Role, $FilePath, [object[]]$ArgumentList)
    $script:installArguments = @($ArgumentList)
    $script:installCalls += 1
    $versionIndex = [Array]::IndexOf($script:installArguments, '--version')
    $script:installedVersion = [string]$script:installArguments[$versionIndex + 1]
    return @()
}
function Update-ProvisioningPath { }
function Test-ProvisioningWinGetPackageInstalled {
    param($Metadata)
    $script:verifiedVersion = [string]$Metadata.Version
    return $script:installedVersion -ceq [string]$Metadata.Version
}
Install-ProvisioningOnlineWinGetPackage -Role 'Example' -Id 'Example.Package'
$versionIndex = [Array]::IndexOf($script:installArguments, '--version')
if ($script:searchCalls -ne 1 -or $versionIndex -lt 0 -or $script:installArguments[$versionIndex + 1] -cne '9.8.7' -or $script:verifiedVersion -cne '9.8.7') {
    throw 'Latest online WinGet package was not concretized through install and readback.'
}
$script:searchCalls = 0
Install-ProvisioningOnlineWinGetPackage -Role 'Example' -Id 'Example.Package' -Version '1.2.3'
$versionIndex = [Array]::IndexOf($script:installArguments, '--version')
if ($script:searchCalls -ne 0 -or $versionIndex -lt 0 -or $script:installArguments[$versionIndex + 1] -cne '1.2.3' -or $script:verifiedVersion -cne '1.2.3') {
    throw 'Exact online WinGet package version was not preserved.'
}
$installCalls = $script:installCalls
Install-ProvisioningOnlineWinGetPackage -Role 'Example' -Id 'Example.Package' -Version '1.2.3'
if ($script:installCalls -ne $installCalls) {
    throw 'Matching online WinGet package was reinstalled.'
}
`, functionSetup)
	scriptPath := filepath.Join(t.TempDir(), "online-winget-regression.ps1")
	if err := os.WriteFile(scriptPath, []byte(script), 0o600); err != nil {
		t.Fatal(err)
	}
	command := hiddenCommand(mustWindowsPowerShellPath(t), "-NoLogo", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File", scriptPath)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("online WinGet regression: %v: %s", err, output)
	}
}

func TestLiveWinGetPythonMetadataResolution(t *testing.T) {
	if runtime.GOOS != "windows" || os.Getenv("HERDR_SANDBOX_LIVE_WINGET_METADATA") != "1" {
		t.Skip("opt-in installed WinGet metadata boundary")
	}
	basePath := defaultProvisioningPath(t, baseProvisioningName)
	stackPath := defaultProvisioningPath(t, stackProvisioningName)
	quote := func(value string) string { return strings.ReplaceAll(value, "'", "''") }
	script := fmt.Sprintf(`$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'
$definitions = @(
    [pscustomobject]@{ Path='%s'; Names=@('Get-ProvisioningBoundedDiagnosticText', 'Invoke-ProvisioningNative', 'Search-ProvisioningWinGetPackages') },
    [pscustomobject]@{ Path='%s'; Names=@('Resolve-StackPythonPackage') }
)
foreach ($source in $definitions) {
    $tokens = $null
    $errors = $null
    $ast = [Management.Automation.Language.Parser]::ParseFile($source.Path, [ref]$tokens, [ref]$errors)
    if ($errors.Count -ne 0) { throw $errors[0].Message }
    foreach ($name in $source.Names) {
        $definition = $ast.Find({ param($node) $node -is [Management.Automation.Language.FunctionDefinitionAst] -and $node.Name -ceq $name }, $true)
        if ($null -eq $definition) { throw "Missing function $name" }
        Invoke-Expression $definition.Extent.Text
    }
}
function Write-ProvisioningProgress { param($Message) }
function Write-ProvisioningTiming { param($Role, $Seconds) }
$resolved = Resolve-StackPythonPackage -Series '' -Version ''
[Console]::Out.Write(([string]$resolved.Series + '|' + [string]$resolved.Version))
`, quote(basePath), quote(stackPath))
	scriptPath := filepath.Join(t.TempDir(), "live-winget-metadata.ps1")
	if err := os.WriteFile(scriptPath, []byte(script), 0o600); err != nil {
		t.Fatal(err)
	}
	command := hiddenCommand(mustWindowsPowerShellPath(t), "-NoLogo", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File", scriptPath)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("live WinGet Python metadata resolution: %v: %s", err, output)
	}
	fields := strings.Split(strings.TrimSpace(string(output)), "|")
	if len(fields) != 2 || fields[0] == "" || !strings.HasPrefix(fields[1], fields[0]+".") {
		t.Fatalf("live WinGet Python metadata result = %q", output)
	}
}

func TestLiveRustStableMetadataResolution(t *testing.T) {
	if runtime.GOOS != "windows" || os.Getenv("HERDR_SANDBOX_LIVE_RUST_METADATA") != "1" {
		t.Skip("opt-in official Rust metadata boundary")
	}
	stackPath := defaultProvisioningPath(t, stackProvisioningName)
	quote := func(value string) string { return strings.ReplaceAll(value, "'", "''") }
	script := fmt.Sprintf(`$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'
$tokens = $null
$errors = $null
$ast = [Management.Automation.Language.Parser]::ParseFile('%s', [ref]$tokens, [ref]$errors)
if ($errors.Count -ne 0) { throw $errors[0].Message }
foreach ($name in @('Get-StackRustSHA256', 'Invoke-StackRustMetadataDownload', 'ConvertFrom-StackRustManifest', 'Get-StackRustManifestSnapshot', 'Resolve-StackRustDistribution')) {
    $definition = $ast.Find({ param($node) $node -is [Management.Automation.Language.FunctionDefinitionAst] -and $node.Name -ceq $name }, $true)
    if ($null -eq $definition) { throw "Missing function $name" }
    Invoke-Expression $definition.Extent.Text
}
$resolved = Resolve-StackRustDistribution -RequestedChannel 'stable'
[Console]::Out.Write(([string]$resolved.Toolchain + '|' + @($resolved.Payloads).Count + '|' + [string]$resolved.CacheEntryName))
`, quote(stackPath))
	scriptPath := filepath.Join(t.TempDir(), "live-rust-metadata.ps1")
	if err := os.WriteFile(scriptPath, []byte(script), 0o600); err != nil {
		t.Fatal(err)
	}
	command := hiddenCommand(mustWindowsPowerShellPath(t), "-NoLogo", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File", scriptPath)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("live Rust metadata resolution: %v: %s", err, output)
	}
	fields := strings.Split(strings.TrimSpace(string(output)), "|")
	if len(fields) != 3 || fields[0] == "" || fields[1] != "7" || !strings.HasPrefix(fields[2], fields[0]+"-x86_64-pc-windows-msvc-") || len(fields[2]) < 64 {
		t.Fatalf("live Rust metadata result = %q", output)
	}
}

func TestDefaultBaseConsumesOneResolvedWinGetPackagePlan(t *testing.T) {
	text := readDefaultBaseProvisioning(t)
	for _, required := range []string{
		"[string]$PackagePlanPath",
		"[string]$UserProvisioningPath",
		"[string]$ProcessOwnerPath",
		"[HerdrSandbox.ProvisioningProcess]::MaximumGroupTasks -ne 2",
		"[HerdrSandbox.ProvisioningProcess]::MaximumConcurrentDownloads -ne 3",
		"function Start-ProvisioningNativeGroup",
		"function Complete-ProvisioningNativeGroup",
		"function Stop-ProvisioningNativeGroup",
		"function Read-ProvisioningPackagePlan",
		"function Read-ProvisioningToolVersionPlan",
		"function Get-ProvisioningToolVersion",
		"function Get-ProvisioningToolSeries",
		"function Test-ProvisioningPackageEnabled",
		"function Get-ProvisioningPackageVersion",
		"function Assert-ProvisioningVulkanDevice",
		"WinGet package plan is missing Core package Microsoft.PowerShell",
		"if (Test-ProvisioningPackageEnabled -Id 'Git.Git')",
		"if (Test-ProvisioningPackageEnabled -Id 'GitHub.cli')",
		"if (Test-ProvisioningPackageEnabled -Id 'SST.opencode')",
		"foreach ($package in @($provisioningPackagePlan.Data.additions))",
		"Install-ProvisioningOnlineWinGetPackage -Role \"additional WinGet package $packageID\"",
		"-Id $packageID -Version ([string]$package.version)",
		"if (Test-ProvisioningPackageEnabled -Id 'KhronosGroup.VulkanRT')",
		"Invoke-ProvisioningNativeResult -Role 'Vulkan device inspection'",
		"Vulkan Runtime did not expose a physical device",
		"Search-ProvisioningWinGetPackages -Role $Role -IdQuery $Id -Exact",
		"'--version', $resolvedVersion",
		"Test-ProvisioningWinGetPackageInstalled -Metadata $metadata",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("default Base is missing resolved package-plan contract %q", required)
		}
	}
	additionStart := strings.Index(text, "foreach ($package in @($provisioningPackagePlan.Data.additions))")
	if additionStart < 0 {
		t.Fatal("generic package installation block was not found")
	}
	workspaceStart := strings.Index(text[additionStart:], "$workspaceManifestPath =")
	if workspaceStart < 0 {
		t.Fatal("generic package installation block was not found")
	}
	additionBlock := text[additionStart : additionStart+workspaceStart]
	if strings.Contains(additionBlock, "-Override") || strings.Contains(additionBlock, "Install-ProvisioningWinGetPackage") {
		t.Fatal("generic package additions bypass the exact-ID online WinGet boundary")
	}
	openCodeSkip := strings.Index(additionBlock, "if ($packageID -ieq 'SST.opencode')")
	onlineInstall := strings.Index(additionBlock, "Install-ProvisioningOnlineWinGetPackage")
	if openCodeSkip < 0 || onlineInstall < 0 || openCodeSkip > onlineInstall {
		t.Fatal("generic package additions do not defer OpenCode to its specialized adapter")
	}
	vulkanVerification := strings.Index(additionBlock, "if (Test-ProvisioningPackageEnabled -Id 'KhronosGroup.VulkanRT')")
	if vulkanVerification < 0 || vulkanVerification < onlineInstall {
		t.Fatal("Vulkan device verification does not follow generic package installation")
	}
}

func TestDefaultBaseTreatsProjectProvisioningProfilesAsOptional(t *testing.T) {
	text := readDefaultBaseProvisioning(t)
	for _, required := range []string{
		"$projectScriptPath = Join-Path $ProjectProvisioningDirectory ($workspaceName + '.ps1')",
		"if (-not (Test-Path -LiteralPath $projectScriptPath -PathType Leaf)) {",
		"$projectScript = Get-Item -LiteralPath $projectScriptPath",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("default Base is missing optional project provisioning contract %q", required)
		}
	}
	mandatory := "-not (Test-Path -LiteralPath (Join-Path $ProjectProvisioningDirectory ($workspaceName + '.ps1')) -PathType Leaf) -or"
	if strings.Contains(text, mandatory) {
		t.Fatal("default Base still requires one provisioning profile per workspace")
	}
}

func TestBasePackagePlanReaderIsStrictInWindowsPowerShell51(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows PowerShell 5.1 regression")
	}
	root := t.TempDir()
	basePath := filepath.Join(root, baseProvisioningName)
	if err := os.WriteFile(basePath, []byte(readDefaultBaseProvisioning(t)), 0o600); err != nil {
		t.Fatal(err)
	}
	planPath := filepath.Join(root, wingetPackagePlanFileName)
	quote := func(value string) string { return strings.ReplaceAll(value, "'", "''") }
	script := fmt.Sprintf(`$ErrorActionPreference = 'Stop'
$tokens = $null
$errors = $null
$ast = [System.Management.Automation.Language.Parser]::ParseFile('%s', [ref]$tokens, [ref]$errors)
try {
$definition = $ast.Find({ param($node) $node -is [System.Management.Automation.Language.FunctionDefinitionAst] -and $node.Name -eq 'Read-ProvisioningPackagePlan' }, $true)
Invoke-Expression $definition.Extent.Text
$path = '%s'
$utf8 = New-Object Text.UTF8Encoding($false)
[IO.File]::WriteAllText($path, '{"schemaVersion":1,"windowsTerminalEdition":"stable","defaults":[{"id":"Microsoft.PowerShell","version":""}],"additions":[{"id":"7zip.7zip","version":"26.00"}]}', $utf8)
$resolved = Read-ProvisioningPackagePlan -Path $path
if (-not $resolved.Enabled.ContainsKey('Microsoft.PowerShell') -or -not $resolved.Enabled.ContainsKey('7ZIP.7ZIP') -or [string]$resolved.Versions['7zip.7zip'] -cne '26.00') {
    throw 'Canonical package plan was not preserved.'
}
[IO.File]::WriteAllText($path, '{"schemaVersion":1,"windowsTerminalEdition":"stable","defaults":[{"id":"Git.Git","version":""}],"additions":[]}', $utf8)
$accepted = $false
try { $null = Read-ProvisioningPackagePlan -Path $path; $accepted = $true } catch { }
if ($accepted) { throw 'Package plan without Core PowerShell was accepted.' }
} catch {
    Write-Output ($_ | Out-String)
    exit 1
}
`, quote(basePath), quote(planPath))
	scriptPath := filepath.Join(root, "package-plan-regression.ps1")
	if err := os.WriteFile(scriptPath, []byte(script), 0o600); err != nil {
		t.Fatal(err)
	}
	command := hiddenCommand(mustWindowsPowerShellPath(t), "-NoLogo", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File", scriptPath)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("package-plan reader regression: %v: %s", err, output)
	}
}

func TestBaseToolVersionPlanConvergesRequestsInWindowsPowerShell51(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows PowerShell 5.1 regression")
	}
	root := t.TempDir()
	basePath := filepath.Join(root, baseProvisioningName)
	if err := os.WriteFile(basePath, []byte(readDefaultBaseProvisioning(t)), 0o600); err != nil {
		t.Fatal(err)
	}
	planPath := filepath.Join(root, toolVersionPlanFileName)
	quote := func(value string) string { return strings.ReplaceAll(value, "'", "''") }
	script := fmt.Sprintf(`$ErrorActionPreference = 'Stop'
$tokens = $null
$errors = $null
$ast = [System.Management.Automation.Language.Parser]::ParseFile('%s', [ref]$tokens, [ref]$errors)
if ($errors.Count -ne 0) { throw $errors[0].Message }
foreach ($name in @('Read-ProvisioningToolVersionPlan','Get-ProvisioningToolVersion','Get-ProvisioningToolSeries')) {
    $definition = $ast.Find({ param($node) $node -is [System.Management.Automation.Language.FunctionDefinitionAst] -and $node.Name -ceq $name }, $true)
    if ($null -eq $definition) { throw "Missing tool-plan function: $name" }
    Invoke-Expression $definition.Extent.Text
}
$path = '%s'
$utf8 = New-Object Text.UTF8Encoding($false)
[IO.File]::WriteAllText($path, '{"schemaVersion":1,"tools":[{"tool":"GoLang.Go","version":"1.26.5","series":"","owners":["project alpha (go)","project beta (go)"]},{"tool":"Python","version":"","series":"3.13","owners":["project alpha (python)"]},{"tool":"zig.zig","version":"","series":"","owners":["project alpha (zig)","project beta (zig)"]}]}', $utf8)
$global:HerdrSandboxToolVersionPlan = Read-ProvisioningToolVersionPlan -Path $path
if ((Get-ProvisioningToolVersion -Tool 'GoLang.Go') -cne '1.26.5' -or
    (Get-ProvisioningToolVersion -Tool 'GoLang.Go' -Requested '1.26.5') -cne '1.26.5' -or
    (Get-ProvisioningToolSeries -Tool 'Python') -cne '3.13') { throw 'Resolved exact tool version was not reused.' }
if ((Get-ProvisioningToolVersion -Tool 'zig.zig') -cne '') { throw 'Latest tool was prematurely concretized.' }
if ((Get-ProvisioningToolVersion -Tool 'zig.zig' -Requested '0.15.2') -cne '0.15.2' -or
    (Get-ProvisioningToolVersion -Tool 'zig.zig') -cne '0.15.2') { throw 'Latest tool version was not concretized once.' }
$accepted = $false
try { $null = Get-ProvisioningToolVersion -Tool 'GoLang.Go' -Requested '1.26.4'; $accepted = $true } catch { }
if ($accepted) { throw 'Conflicting post-preflight tool version was accepted.' }
[IO.File]::WriteAllText($path, '{"schemaVersion":1,"tools":[{"tool":"GoLang.Go","version":"1.26.5","series":"","owners":["project alpha (go)"],"extra":true}]}', $utf8)
$accepted = $false
try { $null = Read-ProvisioningToolVersionPlan -Path $path; $accepted = $true } catch { }
if ($accepted) { throw 'Tool version plan with an unknown nested field was accepted.' }
`, quote(basePath), quote(planPath))
	scriptPath := filepath.Join(root, "tool-version-plan-regression.ps1")
	if err := os.WriteFile(scriptPath, []byte(script), 0o600); err != nil {
		t.Fatal(err)
	}
	command := hiddenCommand(mustWindowsPowerShellPath(t), "-NoLogo", "-NoProfile", "-NonInteractive", "-WindowStyle", "Hidden", "-ExecutionPolicy", "Bypass", "-File", scriptPath)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("tool-version plan regression: %v: %s", err, output)
	}
}

func readDefaultBaseProvisioning(t *testing.T) string {
	t.Helper()
	contents, err := os.ReadFile(defaultProvisioningPath(t, baseProvisioningName))
	if err != nil {
		t.Fatal(err)
	}
	return string(contents)
}

func readDefaultStackProvisioning(t *testing.T) string {
	t.Helper()
	contents, err := os.ReadFile(defaultProvisioningPath(t, stackProvisioningName))
	if err != nil {
		t.Fatal(err)
	}
	return string(contents)
}

func defaultProvisioningPath(t *testing.T, name string) string {
	t.Helper()
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test source path")
	}
	return filepath.Join(filepath.Dir(currentFile), "..", "..", "provisioning", name)
}

func TestDefaultBaseExtractsPortablePackagesWithInboxTar(t *testing.T) {
	text := readDefaultBaseProvisioning(t)
	for _, required := range []string{
		"Join-Path $env:SystemRoot 'System32\\tar.exe'",
		"-ArgumentList @('-xf', $PayloadPath, '-C', $toolRoot)",
		"archive produced a reparse point",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("default Base is missing portable extraction contract %q", required)
		}
	}
	if strings.Contains(text, "Expand-Archive -LiteralPath $PayloadPath") {
		t.Fatal("default Base retains the slow portable Expand-Archive path")
	}
}

func TestProvisioningProgressAndTimingDiagnosticsInWindowsPowerShell51(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows PowerShell 5.1 regression")
	}
	baseScript := defaultProvisioningPath(t, baseProvisioningName)
	statusDirectory := t.TempDir()
	quote := func(value string) string { return strings.ReplaceAll(value, "'", "''") }
	script := fmt.Sprintf(`$ErrorActionPreference = 'Stop'
$tokens = $null
$errors = $null
$ast = [System.Management.Automation.Language.Parser]::ParseFile('%s', [ref]$tokens, [ref]$errors)
foreach ($name in @('Write-ProvisioningProgress', 'Write-ProvisioningTiming')) {
    $definition = $ast.Find({ param($node) $node -is [System.Management.Automation.Language.FunctionDefinitionAst] -and $node.Name -eq $name }, $true)
    Invoke-Expression $definition.Extent.Text
}
$env:HERDR_SANDBOX_STATUS_DIRECTORY = '%s'
Write-ProvisioningProgress -Message 'first fixture progress'
Write-ProvisioningProgress -Message 'fixture progress'
Write-ProvisioningTiming -Role 'fixture timing' -Seconds 1.25
$progress = [IO.File]::ReadAllText((Join-Path '%s' 'progress.json')) | ConvertFrom-Json
if ([int]$progress.schemaVersion -ne 1 -or [string]$progress.phase -cne 'development-provisioning' -or
    [string]$progress.message -cne 'fixture progress') { throw "Unexpected progress: $($progress | ConvertTo-Json -Compress)" }
$lines = [IO.File]::ReadAllLines((Join-Path '%s' 'timings.jsonl'))
if ($lines.Count -ne 1) { throw "Unexpected timing line count: $($lines.Count)" }
$record = $lines[0] | ConvertFrom-Json
$properties = @($record.PSObject.Properties.Name | Sort-Object)
if (($properties -join '|') -cne 'elapsedMilliseconds|recordedAtUTC|role|schemaVersion' -or
    [int]$record.schemaVersion -ne 1 -or [string]$record.role -cne 'fixture timing' -or
    [long]$record.elapsedMilliseconds -ne 1250 -or [string]::IsNullOrWhiteSpace([string]$record.recordedAtUTC)) {
    throw "Unexpected timing record: $($lines[0])"
}
`, quote(baseScript), quote(statusDirectory), quote(statusDirectory), quote(statusDirectory))
	powerShell := mustWindowsPowerShellPath(t)
	command := hiddenCommand(powerShell, "-NoLogo", "-NoProfile", "-NonInteractive", "-EncodedCommand", encodePowerShell(script))
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("provisioning diagnostics regression: %v: %s", err, output)
	}
	temporaryProgressFiles, err := filepath.Glob(filepath.Join(statusDirectory, "progress.json.*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(temporaryProgressFiles) != 0 {
		t.Fatalf("progress publication left temporary files: %v", temporaryProgressFiles)
	}
}

func TestRegistryValueWriterIsTypedAndIdempotentInWindowsPowerShell51(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows PowerShell 5.1 regression")
	}
	baseScript := defaultProvisioningPath(t, baseProvisioningName)
	registryPath := `HKCU:\Software\HerdrSandboxTests\` + strings.ReplaceAll(t.Name(), "/", "-")
	quote := func(value string) string { return strings.ReplaceAll(value, "'", "''") }
	script := fmt.Sprintf(`$ErrorActionPreference = 'Stop'
$tokens = $null
$errors = $null
$ast = [System.Management.Automation.Language.Parser]::ParseFile('%s', [ref]$tokens, [ref]$errors)
foreach ($name in @('Ensure-ProvisioningRegistryKey', 'Set-ProvisioningRegistryValue')) {
    $definition = $ast.Find({ param($node) $node -is [System.Management.Automation.Language.FunctionDefinitionAst] -and $node.Name -eq $name }, $true)
    Invoke-Expression $definition.Extent.Text
}
$path = '%s'
try {
    $first = Set-ProvisioningRegistryValue -Path $path -Name 'Fixture' -Value 7 -PropertyType DWord
    $second = Set-ProvisioningRegistryValue -Path $path -Name 'Fixture' -Value 7 -PropertyType DWord
    $typeChange = Set-ProvisioningRegistryValue -Path $path -Name 'Fixture' -Value '7' -PropertyType String
    $third = Set-ProvisioningRegistryValue -Path $path -Name 'Fixture' -Value '7' -PropertyType String
    $defaultFirst = Set-ProvisioningRegistryValue -Path $path -Name '' -Value 0 -PropertyType DWord
    $defaultSecond = Set-ProvisioningRegistryValue -Path $path -Name '' -Value 0 -PropertyType DWord
    $key = Get-Item -LiteralPath $path
    if ($first -ne $true -or $second -ne $false -or $typeChange -ne $true -or $third -ne $false -or
        $defaultFirst -ne $true -or $defaultSecond -ne $false -or
        [string]$key.GetValueKind('Fixture') -cne 'String' -or [string]$key.GetValue('Fixture') -cne '7' -or
        [string]$key.GetValueKind('') -cne 'DWord' -or [int]$key.GetValue('') -ne 0) {
        throw 'Typed/idempotent registry fixture did not match.'
    }
} finally {
    if (Test-Path -LiteralPath $path) { Remove-Item -LiteralPath $path -Recurse -Force }
}
`, quote(baseScript), quote(registryPath))
	powerShell := mustWindowsPowerShellPath(t)
	command := hiddenCommand(powerShell, "-NoLogo", "-NoProfile", "-NonInteractive", "-EncodedCommand", encodePowerShell(script))
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("typed/idempotent registry regression: %v: %s", err, output)
	}
}

func TestWinGetListParserRequiresExactIDAndVersionInWindowsPowerShell51(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows PowerShell 5.1 regression")
	}
	baseScript := defaultProvisioningPath(t, baseProvisioningName)
	quote := func(value string) string { return strings.ReplaceAll(value, "'", "''") }
	script := fmt.Sprintf(`$ErrorActionPreference = 'Stop'
$tokens = $null
$errors = $null
$ast = [System.Management.Automation.Language.Parser]::ParseFile('%s', [ref]$tokens, [ref]$errors)
$definition = $ast.Find({ param($node) $node -is [System.Management.Automation.Language.FunctionDefinitionAst] -and $node.Name -eq 'Test-ProvisioningWinGetListOutput' }, $true)
Invoke-Expression $definition.Extent.Text
$metadata = [pscustomobject]@{ Id = 'Microsoft.PowerShell'; Version = '7.6.4.0' }
$matching = @('Name       Id                   Version', '---------------------------------------', 'PowerShell Microsoft.PowerShell 7.6.4.0')
$wrongVersion = @('PowerShell Microsoft.PowerShell 7.6.3.0')
$duplicate = @('PowerShell Microsoft.PowerShell 7.6.4.0', 'PowerShell Microsoft.PowerShell 7.6.4.0')
if (-not (Test-ProvisioningWinGetListOutput -Lines $matching -Metadata $metadata) -or
    (Test-ProvisioningWinGetListOutput -Lines $wrongVersion -Metadata $metadata) -or
    (Test-ProvisioningWinGetListOutput -Lines $duplicate -Metadata $metadata)) {
    throw 'WinGet list parser did not enforce one exact ID/version row.'
}
`, quote(baseScript))
	powerShell := mustWindowsPowerShellPath(t)
	command := hiddenCommand(powerShell, "-NoLogo", "-NoProfile", "-NonInteractive", "-EncodedCommand", encodePowerShell(script))
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("WinGet list parser regression: %v: %s", err, output)
	}
}

func TestWinGetReadbackMismatchWarnsAndContinuesInWindowsPowerShell51(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows PowerShell 5.1 regression")
	}
	baseScript := defaultProvisioningPath(t, baseProvisioningName)
	quote := func(value string) string { return strings.ReplaceAll(value, "'", "''") }
	script := fmt.Sprintf(`$ErrorActionPreference = 'Stop'
$tokens = $null
$errors = $null
$ast = [System.Management.Automation.Language.Parser]::ParseFile('%s', [ref]$tokens, [ref]$errors)
$definition = $ast.Find({ param($node) $node -is [System.Management.Automation.Language.FunctionDefinitionAst] -and $node.Name -eq 'Confirm-ProvisioningWinGetReadback' }, $true)
Invoke-Expression $definition.Extent.Text
$metadata = [pscustomobject]@{ Id = '7zip.7zip'; Version = '26.02' }
$warnings = @()
Confirm-ProvisioningWinGetReadback -Role '7-Zip' -Metadata $metadata -Verified $false -WarningVariable warnings
$warningText = (@($warnings | ForEach-Object { [string]$_ }) -join [Environment]::NewLine)
if ($warnings.Count -ne 1 -or $warningText -notmatch '7zip\.7zip' -or
    $warningText -notmatch '26\.02' -or $warningText -notmatch 'Provisioning will continue') {
    throw "Unexpected WinGet read-back warning: $warningText"
}
$warnings = @()
Confirm-ProvisioningWinGetReadback -Role '7-Zip' -Metadata $metadata -Verified $true -WarningVariable warnings
if ($warnings.Count -ne 0) { throw 'Verified WinGet package emitted a warning.' }
`, quote(baseScript))
	command := hiddenCommand(mustWindowsPowerShellPath(t), "-NoLogo", "-NoProfile", "-NonInteractive", "-EncodedCommand", encodePowerShell(script))
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("WinGet read-back warning regression: %v: %s", err, output)
	}
}

func TestNativeProcessTreeWaitUsesReturnedExitCodeInWindowsPowerShell51(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows PowerShell 5.1 regression")
	}
	baseScript := defaultProvisioningPath(t, baseProvisioningName)
	processSource := filepath.Join(filepath.Dir(baseScript), "..", "internal", "sandbox", "assets", provisioningProcessName)
	processSource, err := filepath.Abs(processSource)
	if err != nil {
		t.Fatal(err)
	}
	powerShell := mustWindowsPowerShellPath(t)
	quote := func(value string) string { return strings.ReplaceAll(value, "'", "''") }
	script := fmt.Sprintf(`$ErrorActionPreference = 'Stop'
$tokens = $null
$errors = $null
$ast = [System.Management.Automation.Language.Parser]::ParseFile('%s', [ref]$tokens, [ref]$errors)
Add-Type -Path '%s'
foreach ($name in @('New-ProvisioningNativeSpec', 'ConvertFrom-ProvisioningNativeOutput', 'Invoke-ProvisioningNativeResult', 'Invoke-ProvisioningNative')) {
    $definition = $ast.Find({ param($node) $node -is [System.Management.Automation.Language.FunctionDefinitionAst] -and $node.Name -eq $name }, $true)
    if ($null -eq $definition) { throw "Missing function: $name" }
    Invoke-Expression $definition.Extent.Text
}
function Write-ProvisioningProgress { param([string]$Message) }
function Write-ProvisioningTiming { param([string]$Role, [double]$Seconds) }
function Get-ProvisioningBoundedDiagnosticText { param([string]$Text, [int]$MaximumBytes) return $Text }
$global:LASTEXITCODE = 91
Invoke-ProvisioningNative -Role 'empty arguments' -FilePath (Join-Path $env:SystemRoot 'System32\whoami.exe') -ArgumentList @() | Out-Null
Invoke-ProvisioningNative -Role 'fixture success' -FilePath '%s' -ArgumentList @('-NoLogo', '-NoProfile', '-NonInteractive', '-EncodedCommand', '%s') -WaitForProcessTree | Out-Null
$failed = $false
try {
    Invoke-ProvisioningNative -Role 'fixture failure' -FilePath '%s' -ArgumentList @('-NoLogo', '-NoProfile', '-NonInteractive', '-EncodedCommand', '%s') -WaitForProcessTree | Out-Null
} catch {
    if ($_.Exception.Message -notmatch 'exit code 23') { throw }
    $failed = $true
}
if (-not $failed) { throw 'Process-tree wait ignored the returned nonzero exit code.' }
`, quote(baseScript), quote(processSource), quote(powerShell), encodePowerShell("exit 0"), quote(powerShell), encodePowerShell("exit 23"))
	command := hiddenCommand(powerShell, "-NoLogo", "-NoProfile", "-NonInteractive", "-EncodedCommand", encodePowerShell(script))
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("native process-tree wait regression: %v: %s", err, output)
	}
}

func TestRustupAdapterPreservesInstallerBasenameInWindowsPowerShell51(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows PowerShell 5.1 regression")
	}
	baseScript := defaultProvisioningPath(t, baseProvisioningName)
	stage := t.TempDir()
	payload := filepath.Join(stage, "payload")
	if err := os.WriteFile(payload, []byte("fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	quote := func(value string) string { return strings.ReplaceAll(value, "'", "''") }
	script := fmt.Sprintf(`$ErrorActionPreference = 'Stop'
$tokens = $null
$errors = $null
$ast = [System.Management.Automation.Language.Parser]::ParseFile('%s', [ref]$tokens, [ref]$errors)
$definition = $ast.Find({ param($node) $node -is [System.Management.Automation.Language.FunctionDefinitionAst] -and $node.Name -eq 'Install-ProvisioningPackagePayload' }, $true)
Invoke-Expression $definition.Extent.Text
function Invoke-ProvisioningNative {
    param([string]$Role, [object]$FilePath, [string[]]$ArgumentList)
    $script:capturedRole = $Role
    $script:capturedPath = [string]$FilePath
    $script:capturedArguments = @($ArgumentList)
}
function Update-ProvisioningPath {}
function Get-FileHash {
    param([string]$LiteralPath, [string]$Algorithm)
    $stream = [IO.File]::OpenRead($LiteralPath)
    $sha = [Security.Cryptography.SHA256]::Create()
    try { $hash = [BitConverter]::ToString($sha.ComputeHash($stream)).Replace('-', '') }
    finally { $sha.Dispose(); $stream.Dispose() }
    return [pscustomobject]@{ Hash = $hash }
}
$payloadHash = (Get-FileHash -LiteralPath '%s' -Algorithm SHA256).Hash.ToUpperInvariant()
$metadata = [pscustomobject]@{ Id = 'Rustlang.Rustup'; Sha256 = $payloadHash }
Install-ProvisioningPackagePayload -Role 'Rustup' -Metadata $metadata -PayloadPath '%s' -Adapter 'Rustup' -InstallerArguments @('-y', '--default-toolchain', 'none')
$expected = Join-Path '%s' 'rustup-init.exe'
if ($script:capturedRole -cne 'Rustup cached installation' -or $script:capturedPath -cne $expected -or
    ($script:capturedArguments -join '|') -cne '-y|--default-toolchain|none' -or
    -not (Test-Path -LiteralPath $expected -PathType Leaf) -or -not (Test-Path -LiteralPath '%s' -PathType Leaf)) {
    throw "Unexpected Rustup invocation: role=$script:capturedRole path=$script:capturedPath args=$($script:capturedArguments -join '|')"
}
`, quote(baseScript), quote(payload), quote(payload), quote(stage), quote(payload))
	powerShell := mustWindowsPowerShellPath(t)
	command := hiddenCommand(powerShell, "-NoLogo", "-NoProfile", "-NonInteractive", "-EncodedCommand", encodePowerShell(script))
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("Rustup basename regression: %v: %s", err, output)
	}
}

func TestPackageAdapterCanDeferCommandReadinessInWindowsPowerShell51(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows PowerShell 5.1 regression")
	}
	baseScript := defaultProvisioningPath(t, baseProvisioningName)
	payload := filepath.Join(t.TempDir(), "payload.exe")
	if err := os.WriteFile(payload, []byte("fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	quote := func(value string) string { return strings.ReplaceAll(value, "'", "''") }
	script := fmt.Sprintf(`$ErrorActionPreference = 'Stop'
$tokens = $null
$errors = $null
$ast = [System.Management.Automation.Language.Parser]::ParseFile('%s', [ref]$tokens, [ref]$errors)
$definition = $ast.Find({ param($node) $node -is [System.Management.Automation.Language.FunctionDefinitionAst] -and $node.Name -eq 'Install-ProvisioningPackagePayload' }, $true)
Invoke-Expression $definition.Extent.Text
function Invoke-ProvisioningNative {
    param([string]$Role, [object]$FilePath, [string[]]$ArgumentList, [switch]$WaitForProcessTree)
    if (-not $WaitForProcessTree) { throw 'Burn adapter did not request a process-tree wait.' }
}
function Update-ProvisioningPath {}
function Wait-ProvisioningCommandAvailable { param([string]$Role, [string]$Name, [string]$CommandSourceExclusion) $script:waitCount += 1 }
$metadata = [pscustomobject]@{ Id = 'Fixture.Burn' }
$script:waitCount = 0
Install-ProvisioningPackagePayload -Role 'Fixture' -Metadata $metadata -PayloadPath '%s' -Adapter 'Burn' -ExecutableName 'fixture.exe' -DeferCommandReadiness
if ($script:waitCount -ne 0) { throw 'Deferred adapter unexpectedly waited for command readiness.' }
Install-ProvisioningPackagePayload -Role 'Fixture' -Metadata $metadata -PayloadPath '%s' -Adapter 'Burn' -ExecutableName 'fixture.exe'
if ($script:waitCount -ne 1) { throw "Default adapter readiness count: $script:waitCount" }
`, quote(baseScript), quote(payload), quote(payload))
	powerShell := mustWindowsPowerShellPath(t)
	command := hiddenCommand(powerShell, "-NoLogo", "-NoProfile", "-NonInteractive", "-EncodedCommand", encodePowerShell(script))
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("deferred command readiness regression: %v: %s", err, output)
	}
}

func TestMergedManifestParserAcceptsBlankLinesInWindowsPowerShell51(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows PowerShell 5.1 regression")
	}
	baseScript := defaultProvisioningPath(t, baseProvisioningName)
	manifest := filepath.Join(t.TempDir(), "package.yaml")
	hash := strings.Repeat("A", 64)
	contents := "PackageIdentifier: Fixture.Package\nPackageVersion: 1.2.3\n\nInstallers:\n- Architecture: x64\n  InstallerType: inno\n  Scope: machine\n  InstallerUrl: https://example.invalid/fixture.exe\n  InstallerSha256: " + hash + "\n  Dependencies:\n    PackageDependencies:\n    - PackageIdentifier: Nested.Dependency\nManifestType: merged\nManifestVersion: 1.10.0\n"
	if err := os.WriteFile(manifest, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	quote := func(value string) string { return strings.ReplaceAll(value, "'", "''") }
	script := fmt.Sprintf(`$ErrorActionPreference = 'Stop'
$tokens = $null
$errors = $null
$ast = [System.Management.Automation.Language.Parser]::ParseFile('%s', [ref]$tokens, [ref]$errors)
foreach ($name in @('Assert-ProvisioningMergedManifestField', 'Assert-ProvisioningDownloadedManifest')) {
    $definition = $ast.Find({ param($node) $node -is [System.Management.Automation.Language.FunctionDefinitionAst] -and $node.Name -eq $name }, $true)
    Invoke-Expression $definition.Extent.Text
}
$metadata = [pscustomobject]@{ Id='Fixture.Package'; Version='1.2.3'; Architecture='x64'; InstallerType='inno'; Scope='machine'; Url='https://example.invalid/fixture.exe'; Sha256='%s' }
Assert-ProvisioningDownloadedManifest -Path '%s' -Metadata $metadata
`, quote(baseScript), hash, quote(manifest))
	powerShell, err := windowsPowerShellExecutable()
	if err != nil {
		t.Fatal(err)
	}
	command := hiddenCommand(powerShell, "-NoLogo", "-NoProfile", "-NonInteractive", "-EncodedCommand", encodePowerShell(script))
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("blank-line manifest regression: %v: %s", err, output)
	}
}

func TestGuestPackageStageCleanupRetriesSharingViolation(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows PowerShell 5.1 regression")
	}
	baseScript := defaultProvisioningPath(t, baseProvisioningName)
	stageRoot := t.TempDir()
	stage := filepath.Join(stageRoot, "locked-stage")
	if err := os.MkdirAll(stage, 0o700); err != nil {
		t.Fatal(err)
	}
	payload := filepath.Join(stage, "payload.exe")
	marker := filepath.Join(stageRoot, "locked.ready")
	if err := os.WriteFile(payload, []byte("fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	quote := func(value string) string { return strings.ReplaceAll(value, "'", "''") }
	childScript := fmt.Sprintf(`$stream = [IO.File]::Open('%s', [IO.FileMode]::Open, [IO.FileAccess]::ReadWrite, [IO.FileShare]::None)
try {
    [IO.File]::WriteAllText('%s', 'ready')
    Start-Sleep -Milliseconds 1200
} finally {
    $stream.Dispose()
}
`, quote(payload), quote(marker))
	script := fmt.Sprintf(`$ErrorActionPreference = 'Stop'
$tokens = $null
$errors = $null
$ast = [System.Management.Automation.Language.Parser]::ParseFile('%s', [ref]$tokens, [ref]$errors)
foreach ($name in @('Get-ProvisioningBoundedDiagnosticText', 'Remove-ProvisioningGuestPackageStage')) {
    $definition = $ast.Find({ param($node) $node -is [System.Management.Automation.Language.FunctionDefinitionAst] -and $node.Name -eq $name }, $true)
    Invoke-Expression $definition.Extent.Text
}
$child = Start-Process -FilePath '%s' -ArgumentList @('-NoLogo', '-NoProfile', '-NonInteractive', '-WindowStyle', 'Hidden', '-EncodedCommand', '%s') -WindowStyle Hidden -PassThru
try {
    $deadline = [DateTime]::UtcNow.AddSeconds(5)
    while (-not (Test-Path -LiteralPath '%s') -and [DateTime]::UtcNow -lt $deadline) {
        Start-Sleep -Milliseconds 25
    }
    if (-not (Test-Path -LiteralPath '%s')) { throw 'Lock fixture did not become ready.' }
    $deferred = Remove-ProvisioningGuestPackageStage -Path '%s' -StageRoot '%s' -Attempts 1 -DelayMilliseconds 0 -BestEffort
    if ($deferred -ne $false -or -not (Test-Path -LiteralPath '%s')) { throw 'Best-effort cleanup did not preserve the locked stage.' }
    Remove-ProvisioningGuestPackageStage -Path '%s' -StageRoot '%s' -Attempts 30 -DelayMilliseconds 100
    if (Test-Path -LiteralPath '%s') { throw 'Locked stage still exists.' }
    if (-not $child.WaitForExit(5000)) { throw 'Lock fixture did not exit.' }
} finally {
    if (-not $child.HasExited) { Stop-Process -InputObject $child -Force }
    $child.Dispose()
}
`, quote(baseScript), quote(mustWindowsPowerShellPath(t)), encodePowerShell(childScript), quote(marker), quote(marker), quote(stage), quote(stageRoot), quote(stage), quote(stage), quote(stageRoot), quote(stage))
	powerShell, err := windowsPowerShellExecutable()
	if err != nil {
		t.Fatal(err)
	}
	command := hiddenCommand(powerShell, "-NoLogo", "-NoProfile", "-NonInteractive", "-EncodedCommand", encodePowerShell(script))
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("guest-stage sharing retry regression: %v: %s", err, output)
	}
}

func TestWaitProvisioningCommandAvailableHandlesDelayedInstallerChild(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows PowerShell 5.1 regression")
	}
	baseScript := defaultProvisioningPath(t, baseProvisioningName)
	commandPath := filepath.Join(t.TempDir(), "delayed-command.exe")
	quote := func(value string) string { return strings.ReplaceAll(value, "'", "''") }
	childScript := fmt.Sprintf(`Start-Sleep -Milliseconds 900
[IO.File]::WriteAllBytes('%s', [byte[]](1, 2, 3))
`, quote(commandPath))
	script := fmt.Sprintf(`$ErrorActionPreference = 'Stop'
$tokens = $null
$errors = $null
$ast = [System.Management.Automation.Language.Parser]::ParseFile('%s', [ref]$tokens, [ref]$errors)
foreach ($name in @('Update-ProvisioningPath', 'Wait-ProvisioningCommandAvailable')) {
    $definition = $ast.Find({ param($node) $node -is [System.Management.Automation.Language.FunctionDefinitionAst] -and $node.Name -eq $name }, $true)
    Invoke-Expression $definition.Extent.Text
}
$child = Start-Process -FilePath '%s' -ArgumentList @('-NoLogo', '-NoProfile', '-NonInteractive', '-WindowStyle', 'Hidden', '-EncodedCommand', '%s') -WindowStyle Hidden -PassThru
try {
    $resolved = Wait-ProvisioningCommandAvailable -Role 'Delayed fixture' -Name '%s' -TimeoutSeconds 5 -DelayMilliseconds 100
    if ($resolved -ine '%s') { throw "Resolved unexpected command: $resolved" }
    if (-not $child.WaitForExit(5000)) { throw 'Delayed fixture did not exit.' }
} finally {
    if (-not $child.HasExited) { Stop-Process -InputObject $child -Force }
    $child.Dispose()
}
`, quote(baseScript), quote(mustWindowsPowerShellPath(t)), encodePowerShell(childScript), quote(commandPath), quote(commandPath))
	powerShell := mustWindowsPowerShellPath(t)
	command := hiddenCommand(powerShell, "-NoLogo", "-NoProfile", "-NonInteractive", "-EncodedCommand", encodePowerShell(script))
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("delayed command readiness regression: %v: %s", err, output)
	}
}

func TestWaitProvisioningCommandAvailableRejectsExcludedAliases(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows PowerShell 5.1 regression")
	}
	baseScript := defaultProvisioningPath(t, baseProvisioningName)
	quote := func(value string) string { return strings.ReplaceAll(value, "'", "''") }
	script := fmt.Sprintf(`$ErrorActionPreference = 'Stop'
$tokens = $null
$errors = $null
$ast = [System.Management.Automation.Language.Parser]::ParseFile('%s', [ref]$tokens, [ref]$errors)
$definition = $ast.Find({ param($node) $node -is [System.Management.Automation.Language.FunctionDefinitionAst] -and $node.Name -eq 'Wait-ProvisioningCommandAvailable' }, $true)
Invoke-Expression $definition.Extent.Text
function Update-ProvisioningPath {}
function Get-Command {
    param($Name, $CommandType, $ErrorAction)
    return @(
        [pscustomobject]@{ Source = 'C:\Users\WDAGUtilityAccount\AppData\Local\Microsoft\WindowsApps\python.exe' },
        [pscustomobject]@{ Source = 'C:\HerdrSandbox\tools\python\bin\python.exe' },
        [pscustomobject]@{ Source = 'C:\Program Files\Python313\python.exe' }
    )
}
$resolved = Wait-ProvisioningCommandAvailable -Role 'Python fixture' -Name 'python.exe' -TimeoutSeconds 1 -DelayMilliseconds 25 -CommandSourceExclusion @('*\Microsoft\WindowsApps\python.exe', 'C:\HerdrSandbox\tools\python\bin\python.exe')
if ($resolved -cne 'C:\Program Files\Python313\python.exe') { throw "Resolved unexpected Python command: $resolved" }
`, quote(baseScript))
	powerShell := mustWindowsPowerShellPath(t)
	command := hiddenCommand(powerShell, "-NoLogo", "-NoProfile", "-NonInteractive", "-EncodedCommand", encodePowerShell(script))
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("excluded aliases regression: %v: %s", err, output)
	}
}

func mustWindowsPowerShellPath(t *testing.T) string {
	t.Helper()
	path, err := windowsPowerShellExecutable()
	if err != nil {
		t.Fatal(err)
	}
	return path
}

type provisioningPowerShellFunctionSource struct {
	path  string
	names []string
}

func provisioningPowerShellFunctionSetup(t *testing.T, sources ...provisioningPowerShellFunctionSource) string {
	t.Helper()
	if len(sources) == 0 {
		t.Fatal("PowerShell function setup requires at least one source")
	}
	quote := func(value string) string { return "'" + strings.ReplaceAll(value, "'", "''") + "'" }
	var script strings.Builder
	script.WriteString("$herdrFunctionSources = @(\n")
	for _, source := range sources {
		if source.path == "" || len(source.names) == 0 {
			t.Fatal("PowerShell function source requires a path and explicit names")
		}
		names := make([]string, len(source.names))
		for index, name := range source.names {
			names[index] = quote(name)
		}
		fmt.Fprintf(&script, "    [pscustomobject]@{ Path = %s; Names = @(%s) }\n", quote(source.path), strings.Join(names, ", "))
	}
	script.WriteString(`)
foreach ($source in $herdrFunctionSources) {
    $tokens = $null
    $errors = $null
    $ast = [Management.Automation.Language.Parser]::ParseFile($source.Path, [ref]$tokens, [ref]$errors)
    if ($errors.Count -ne 0) { throw $errors[0].Message }
    foreach ($name in $source.Names) {
        $definition = $ast.Find({ param($node) $node -is [Management.Automation.Language.FunctionDefinitionAst] -and $node.Name -ceq $name }, $true)
        if ($null -eq $definition) { throw "Missing provisioning function $name from $($source.Path)" }
        Invoke-Expression $definition.Extent.Text
    }
}
$global:HerdrSandboxToolVersionPlan = [pscustomobject]@{ Versions = @{}; Series = @{}; Owners = @{} }
`)
	return script.String()
}

func TestResolveProvisioningCombinesGlobalAndActiveWorkspaces(t *testing.T) {
	root := t.TempDir()
	defaults := filepath.Join(root, "defaults")
	global := filepath.Join(root, "global")
	if err := os.MkdirAll(defaults, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(defaults, baseProvisioningName), []byte("base"), 0o600); err != nil {
		t.Fatal(err)
	}
	first := createWorkspaceFixture(t, root, "first")
	active := createWorkspaceFixture(t, root, "active")
	config := `{"workspaces":{"first":"` + filepath.ToSlash(first) + `"}}`
	if err := os.MkdirAll(global, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(global, globalConfigurationName), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}

	plan, err := resolveProvisioningAt(filepath.Join(active, "src"), global, defaults)
	if err != nil {
		t.Fatalf("resolveProvisioningAt: %v", err)
	}
	if plan.MemoryMB != defaultMemoryMB || len(plan.Workspaces) != 2 || !plan.Workspaces[0].Active || plan.Workspaces[0].HostDirectory != active {
		t.Fatalf("workspaces = %#v", plan.Workspaces)
	}
	if plan.BaseScript != filepath.Join(defaults, baseProvisioningName) || plan.StackScript != filepath.Join(defaults, stackProvisioningName) || plan.UserScript != filepath.Join(global, userProvisioningName) {
		t.Fatalf("provisioning owners = base %q, stacks %q, user %q", plan.BaseScript, plan.StackScript, plan.UserScript)
	}
}

func TestResolveProvisioningIncludesNamedFolderMountsOutsideWorkspaces(t *testing.T) {
	root := t.TempDir()
	defaults := filepath.Join(root, "defaults")
	global := filepath.Join(root, "global")
	active := createWorkspaceFixture(t, root, "active")
	reference := filepath.Join(root, "reference")
	worktrees := filepath.Join(root, "worktrees")
	for _, directory := range []string{defaults, global, reference, worktrees} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	contents, err := json.Marshal(struct {
		Mounts     map[string]mountConfiguration `json:"mounts"`
		Workspaces map[string]string             `json:"workspaces"`
	}{
		Mounts: map[string]mountConfiguration{
			"worktrees": {Path: worktrees, ReadOnly: false},
			"reference": {Path: reference, ReadOnly: true},
		},
		Workspaces: map[string]string{},
	})
	if err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(global, globalConfigurationName), string(contents))

	plan, err := resolveProvisioningAt(filepath.Join(active, "src"), global, defaults)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Mounts) != 2 || plan.Mounts[0].Name != "reference" || !plan.Mounts[0].ReadOnly ||
		plan.Mounts[0].GuestDirectory != `C:\Mounts\reference` || plan.Mounts[1].Name != "worktrees" || plan.Mounts[1].ReadOnly ||
		plan.Mounts[1].GuestDirectory != `C:\Mounts\worktrees` {
		t.Fatalf("folder mounts = %#v", plan.Mounts)
	}
	if len(plan.Workspaces) != 1 || plan.Workspaces[0].Name != "active" || !plan.Workspaces[0].Active {
		t.Fatalf("workspaces = %#v", plan.Workspaces)
	}
}

func TestResolveProvisioningIncludesDedicatedWorktreeDirectory(t *testing.T) {
	root := t.TempDir()
	defaults := filepath.Join(root, "defaults")
	global := filepath.Join(root, "global")
	active := createWorkspaceFixture(t, root, "active")
	worktrees := filepath.Join(root, "worktrees")
	for _, directory := range []string{defaults, global, worktrees} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	contents, err := json.Marshal(map[string]any{
		"worktreeDirectory": worktrees,
		"workspaces":        map[string]string{},
	})
	if err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(global, globalConfigurationName), string(contents))

	plan, err := resolveProvisioningAt(filepath.Join(active, "src"), global, defaults)
	if err != nil {
		t.Fatal(err)
	}
	expectedWorktreeDirectory, err := canonicalMappedDirectory(worktrees)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.EqualFold(filepath.Clean(plan.WorktreeDirectory), expectedWorktreeDirectory) ||
		len(plan.Workspaces) != 1 || !plan.Workspaces[0].Active {
		t.Fatalf("provisioning plan = %#v", plan)
	}
}

func TestResolveProvisioningRejectsUnsafeWorktreeDirectory(t *testing.T) {
	root := t.TempDir()
	worktrees := filepath.Join(root, "worktrees")
	cache := filepath.Join(worktrees, "cache")
	workspace := filepath.Join(root, "workspace")
	linked := filepath.Join(workspace, "linked")
	for _, directory := range []string{worktrees, cache, workspace, linked} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	for name, configuration := range map[string]map[string]any{
		"relative": {"worktreeDirectory": "worktrees"},
		"cache": {
			"worktreeDirectory": worktrees,
			"cacheDirectory":    cache,
		},
		"workspace": {
			"worktreeDirectory": linked,
			"workspaces":        map[string]string{"project": workspace},
		},
	} {
		t.Run(name, func(t *testing.T) {
			global := filepath.Join(root, "global-"+name)
			if err := os.MkdirAll(global, 0o700); err != nil {
				t.Fatal(err)
			}
			data, err := json.Marshal(configuration)
			if err != nil {
				t.Fatal(err)
			}
			writeTestFile(t, filepath.Join(global, globalConfigurationName), string(data))
			if _, err := resolveProvisioningAt(root, global, root); err == nil {
				t.Fatal("unsafe worktreeDirectory unexpectedly succeeded")
			}
		})
	}
}

func TestResolveProvisioningRejectsFolderMountOverlaps(t *testing.T) {
	root := t.TempDir()
	shared := filepath.Join(root, "shared")
	child := filepath.Join(shared, "child")
	for _, directory := range []string{shared, child} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	tests := map[string]map[string]any{
		"another mount": {
			"mounts": map[string]any{
				"parent": map[string]any{"path": shared, "readOnly": true},
				"child":  map[string]any{"path": child, "readOnly": false},
			},
		},
		"workspace": {
			"mounts":     map[string]any{"shared": map[string]any{"path": shared, "readOnly": true}},
			"workspaces": map[string]string{"project": shared},
		},
		"cache": {
			"cacheDirectory": shared,
			"mounts":         map[string]any{"shared": map[string]any{"path": shared, "readOnly": false}},
		},
	}
	for name, configuration := range tests {
		t.Run(name, func(t *testing.T) {
			global := filepath.Join(root, strings.ReplaceAll(name, " ", "-"))
			if err := os.MkdirAll(global, 0o700); err != nil {
				t.Fatal(err)
			}
			data, err := json.Marshal(configuration)
			if err != nil {
				t.Fatal(err)
			}
			writeTestFile(t, filepath.Join(global, globalConfigurationName), string(data))
			if _, err := resolveProvisioningAt(root, global, root); err == nil || !strings.Contains(strings.ToLower(err.Error()), "overlap") {
				t.Fatalf("overlap error = %v", err)
			}
		})
	}
}

func TestFolderMountValidationRejectsVolumeRootsAndExcessEntries(t *testing.T) {
	volumeRoot := filepath.VolumeName(t.TempDir()) + string(os.PathSeparator)
	if _, err := newMountPlan("volume", mountConfiguration{Path: volumeRoot, ReadOnly: true}); err == nil {
		t.Fatal("volume-root folder mount unexpectedly succeeded")
	}

	mounts := make(map[string]mountConfiguration, maximumMounts+1)
	for index := 0; index <= maximumMounts; index++ {
		mounts[fmt.Sprintf("mount-%02d", index)] = mountConfiguration{Path: t.TempDir(), ReadOnly: true}
	}
	data, err := json.Marshal(map[string]any{"mounts": mounts})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), globalConfigurationName)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadGlobalConfiguration(path); err == nil || !strings.Contains(err.Error(), "count exceeds") {
		t.Fatalf("excess mount error = %v", err)
	}
}

func TestResolveProvisioningDiscoversDirectWorkspaceChildren(t *testing.T) {
	root := t.TempDir()
	defaults := filepath.Join(root, "defaults")
	global := filepath.Join(root, "global")
	projects := filepath.Join(root, "projects")
	for _, directory := range []string{defaults, projects} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	explicit := createWorkspaceFixture(t, projects, "Alpha Project")
	external := createWorkspaceFixture(t, root, "external")
	active := createWorkspaceFixture(t, projects, "zeta")
	plain := filepath.Join(projects, "plain")
	plainExternal := filepath.Join(root, "plain-external")
	for _, directory := range []string{plain, plainExternal} {
		if err := os.MkdirAll(filepath.Join(directory, "src"), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	_ = createWorkspaceFixture(t, active, "nested")
	for _, excluded := range []string{"archive", "Scratch-Temp"} {
		if err := os.MkdirAll(filepath.Join(projects, excluded), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	writeTestFile(t, filepath.Join(projects, "notes.txt"), "not a workspace")
	writeWorkspaceDiscoveryConfig(t, global, &workspaceDiscoveryConfiguration{
		Root:    projects,
		Exclude: []string{`^archive$`, `(?i)^scratch`},
	}, map[string]string{"Alpha-Project": external, "custom-alpha": explicit, "plain-external": plainExternal})

	plan, err := resolveProvisioningAt(filepath.Join(active, "src"), global, defaults)
	if err != nil {
		t.Fatalf("resolveProvisioningAt: %v", err)
	}
	if len(plan.Workspaces) != 5 || !plan.Workspaces[0].Active || plan.Workspaces[0].Name != "zeta" ||
		plan.Workspaces[1].Name != "Alpha-Project" || plan.Workspaces[2].Name != "custom-alpha" ||
		plan.Workspaces[3].Name != "plain" || plan.Workspaces[4].Name != "plain-external" {
		t.Fatalf("discovered workspaces = %#v", plan.Workspaces)
	}
	for index, expected := range []string{active, external, explicit, plain, plainExternal} {
		equal, err := workspaceDirectoriesEqual(plan.Workspaces[index].HostDirectory, expected)
		if err != nil || !equal {
			t.Fatalf("workspace %q path = %q, want physical path %q: %v", plan.Workspaces[index].Name, plan.Workspaces[index].HostDirectory, expected, err)
		}
	}
	if plan.Workspaces[3].ProvisioningPath != "" || plan.Workspaces[4].ProvisioningPath != "" {
		t.Fatalf("unprofiled workspaces received provisioning paths: %#v", plan.Workspaces)
	}
	encoded, err := renderConfig(filepath.Join(root, "run-input"), filepath.Join(root, "run-status"), filepath.Join(root, "cache"), plan.Mounts, plan.Workspaces, plan.MemoryMB, plan.AudioOutput, plan.AudioInput)
	if err != nil {
		t.Fatalf("render discovered workspace mappings: %v", err)
	}
	var sandboxConfig wsbConfiguration
	if err := xml.Unmarshal(encoded, &sandboxConfig); err != nil {
		t.Fatalf("decode discovered workspace mappings: %v", err)
	}
	if len(sandboxConfig.MappedFolders.Folders) != len(plan.Workspaces)+3 {
		t.Fatalf("mapped folders = %#v", sandboxConfig.MappedFolders.Folders)
	}
	for index, workspace := range plan.Workspaces {
		mapping := sandboxConfig.MappedFolders.Folders[index+1]
		if mapping.HostFolder != workspace.HostDirectory || mapping.SandboxFolder != workspace.GuestDirectory || mapping.ReadOnly {
			t.Fatalf("workspace %q mapping = %#v", workspace.Name, mapping)
		}
	}
}

func TestDiscoverWorkspacePlansRejectsInvalidRootsChildrenAndCollisions(t *testing.T) {
	t.Run("relative root", func(t *testing.T) {
		_, err := discoverWorkspacePlans(&workspaceDiscoveryConfiguration{Root: "projects", Exclude: []string{}})
		if err == nil || !strings.Contains(err.Error(), "not absolute") {
			t.Fatalf("relative root error = %v", err)
		}
	})

	t.Run("user profile root", func(t *testing.T) {
		root := t.TempDir()
		t.Setenv("USERPROFILE", root)
		_, err := discoverWorkspacePlans(&workspaceDiscoveryConfiguration{Root: root, Exclude: []string{}})
		if err == nil || !strings.Contains(err.Error(), "must not contain a user profile") {
			t.Fatalf("user profile root error = %v", err)
		}
	})

	t.Run("unprofiled child", func(t *testing.T) {
		root := t.TempDir()
		child := filepath.Join(root, "not-a-project")
		if err := os.MkdirAll(child, 0o700); err != nil {
			t.Fatal(err)
		}
		workspaces, err := discoverWorkspacePlans(&workspaceDiscoveryConfiguration{Root: root, Exclude: []string{}})
		if err != nil {
			t.Fatalf("discover unprofiled child: %v", err)
		}
		equal := false
		if len(workspaces) == 1 {
			equal, err = workspaceDirectoriesEqual(workspaces[0].HostDirectory, child)
		}
		if err != nil || len(workspaces) != 1 || !equal || workspaces[0].ProvisioningPath != "" {
			t.Fatalf("unprofiled child workspace = %#v", workspaces)
		}
	})

	t.Run("invalid profiles reported together", func(t *testing.T) {
		root := t.TempDir()
		valid := createWorkspaceFixture(t, root, "valid")
		for _, name := range []string{"broken-one", "broken-two"} {
			project := createWorkspaceFixture(t, root, name)
			if err := os.WriteFile(filepath.Join(project, projectConfigurationName, projectProvisioningName), nil, 0o600); err != nil {
				t.Fatal(err)
			}
		}
		workspaces, err := discoverWorkspacePlans(&workspaceDiscoveryConfiguration{Root: root, Exclude: []string{}})
		if err == nil || !strings.Contains(err.Error(), "broken-one") || !strings.Contains(err.Error(), "broken-two") {
			t.Fatalf("combined invalid profile error = %v", err)
		}
		equal := false
		if len(workspaces) == 1 {
			equal, err = workspaceDirectoriesEqual(workspaces[0].HostDirectory, valid)
		}
		if err != nil || len(workspaces) != 1 || !equal {
			t.Fatalf("valid workspaces beside invalid profiles = %#v", workspaces)
		}
	})

	t.Run("derived name collision", func(t *testing.T) {
		root := t.TempDir()
		projects := filepath.Join(root, "projects")
		defaults := filepath.Join(root, "defaults")
		global := filepath.Join(root, "global")
		if err := os.MkdirAll(defaults, 0o700); err != nil {
			t.Fatal(err)
		}
		_ = createWorkspaceFixture(t, projects, "alpha space")
		_ = createWorkspaceFixture(t, projects, "alpha@space")
		writeWorkspaceDiscoveryConfig(t, global, &workspaceDiscoveryConfiguration{Root: projects, Exclude: []string{}}, nil)
		_, err := resolveProvisioningAt(root, global, defaults)
		if err == nil || !strings.Contains(err.Error(), "discovered workspace name") {
			t.Fatalf("derived name collision error = %v", err)
		}
	})

	t.Run("workspace limit", func(t *testing.T) {
		root := t.TempDir()
		for index := 0; index < 17; index++ {
			_ = createWorkspaceFixture(t, root, fmt.Sprintf("project-%02d", index))
		}
		_, err := discoverWorkspacePlans(&workspaceDiscoveryConfiguration{Root: root, Exclude: []string{}})
		if err == nil || !strings.Contains(err.Error(), "more than 16") {
			t.Fatalf("workspace limit error = %v", err)
		}
	})

	t.Run("reparse child", func(t *testing.T) {
		root := t.TempDir()
		targetRoot := t.TempDir()
		target := createWorkspaceFixture(t, targetRoot, "target")
		createTestDirectoryLink(t, filepath.Join(root, "linked"), target)
		_, err := discoverWorkspacePlans(&workspaceDiscoveryConfiguration{Root: root, Exclude: []string{}})
		if runtime.GOOS != "windows" {
			if err != nil {
				t.Fatalf("non-Windows directory symlink should be ignored without following it: %v", err)
			}
			return
		}
		if err == nil || !strings.Contains(err.Error(), "reparse point") {
			t.Fatalf("reparse child error = %v", err)
		}
	})

	t.Run("reparse file ignored", func(t *testing.T) {
		root := t.TempDir()
		project := createWorkspaceFixture(t, root, "project")
		target := filepath.Join(t.TempDir(), "target.txt")
		writeTestFile(t, target, "target")
		if err := os.Symlink(target, filepath.Join(root, "linked.txt")); err != nil {
			t.Skipf("file symlink unavailable: %v", err)
		}
		workspaces, err := discoverWorkspacePlans(&workspaceDiscoveryConfiguration{Root: root, Exclude: []string{}})
		if err != nil {
			t.Fatalf("discoverWorkspacePlans: %v", err)
		}
		if len(workspaces) != 1 {
			t.Fatalf("workspaces with reparse file = %#v", workspaces)
		}
		equal, err := workspaceDirectoriesEqual(workspaces[0].HostDirectory, project)
		if err != nil || !equal {
			t.Fatalf("workspace with reparse file path = %q, want physical path %q: %v", workspaces[0].HostDirectory, project, err)
		}
	})
}

func TestLoadGlobalConfigurationRejectsInvalidWorkspaceDiscovery(t *testing.T) {
	tooMany := make([]string, maximumWorkspaceExcludePatterns+1)
	for index := range tooMany {
		tooMany[index] = fmt.Sprintf("^project-%d$", index)
	}
	tooManyJSON, err := json.Marshal(map[string]any{"workspaceDiscovery": map[string]any{"root": "", "exclude": tooMany}})
	if err != nil {
		t.Fatal(err)
	}
	tests := map[string]string{
		"null object":       `{"workspaceDiscovery":null}`,
		"nonobject":         `{"workspaceDiscovery":[]}`,
		"unknown field":     `{"workspaceDiscovery":{"path":"C:\\Projects"}}`,
		"duplicate root":    `{"workspaceDiscovery":{"root":"","root":"C:\\Projects"}}`,
		"null root":         `{"workspaceDiscovery":{"root":null}}`,
		"nonstring root":    `{"workspaceDiscovery":{"root":42}}`,
		"null exclude":      `{"workspaceDiscovery":{"exclude":null}}`,
		"nonarray exclude":  `{"workspaceDiscovery":{"exclude":{}}}`,
		"null pattern":      `{"workspaceDiscovery":{"exclude":[null]}}`,
		"nonstring pattern": `{"workspaceDiscovery":{"exclude":[1]}}`,
		"invalid pattern":   `{"workspaceDiscovery":{"exclude":["["]}}`,
		"duplicate pattern": `{"workspaceDiscovery":{"exclude":["^a$","^a$"]}}`,
		"long pattern":      `{"workspaceDiscovery":{"exclude":["` + strings.Repeat("a", maximumWorkspaceExcludePatternSize+1) + `"]}}`,
		"too many patterns": string(tooManyJSON),
	}
	for name, contents := range tests {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), globalConfigurationName)
			writeTestFile(t, path, contents)
			if _, err := loadGlobalConfiguration(path); err == nil {
				t.Fatalf("invalid workspaceDiscovery unexpectedly succeeded: %s", contents)
			}
		})
	}
}

func TestResolveProvisioningUsesConfiguredMemoryAndAudioSelections(t *testing.T) {
	root := t.TempDir()
	defaults := filepath.Join(root, "defaults")
	global := filepath.Join(root, "global")
	if err := os.MkdirAll(defaults, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(defaults, baseProvisioningName), []byte("base"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(global, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(global, globalConfigurationName), []byte(`{"memoryMB":16384,"audio":true,"audioInput":true,"workspaces":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}

	plan, err := resolveProvisioningAt(root, global, defaults)
	if err != nil {
		t.Fatalf("resolveProvisioningAt: %v", err)
	}
	if plan.MemoryMB != 16384 || !plan.AudioOutput || !plan.AudioInput {
		t.Fatalf("resolved runtime config = memory %d, output %t, input %t", plan.MemoryMB, plan.AudioOutput, plan.AudioInput)
	}
}

func TestResolveProvisioningRejectsInvalidConfiguredMemory(t *testing.T) {
	root := t.TempDir()
	defaults := filepath.Join(root, "defaults")
	global := filepath.Join(root, "global")
	if err := os.MkdirAll(defaults, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(defaults, baseProvisioningName), []byte("base"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(global, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"1024", "null"} {
		t.Run(value, func(t *testing.T) {
			config := []byte(`{"memoryMB":` + value + `,"workspaces":{}}`)
			if err := os.WriteFile(filepath.Join(global, globalConfigurationName), config, 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := resolveProvisioningAt(root, global, defaults); err == nil {
				t.Fatalf("memoryMB %s unexpectedly succeeded", value)
			}
		})
	}
}

func TestLoadGlobalConfigurationRejectsNonCanonicalJSON(t *testing.T) {
	tests := map[string]string{
		"null cache":                   `{"cacheDirectory":null,"workspaces":{}}`,
		"null worktree directory":      `{"worktreeDirectory":null,"workspaces":{}}`,
		"nonstring worktree directory": `{"worktreeDirectory":42,"workspaces":{}}`,
		"null audio":                   `{"audio":null,"workspaces":{}}`,
		"nonboolean audio":             `{"audio":"true","workspaces":{}}`,
		"duplicate audio":              `{"audio":true,"audio":false,"workspaces":{}}`,
		"null audio input":             `{"audioInput":null,"workspaces":{}}`,
		"nonboolean audio input":       `{"audioInput":"true","workspaces":{}}`,
		"duplicate audio input":        `{"audioInput":true,"audioInput":false,"workspaces":{}}`,
		"null tailscale":               `{"tailscale":null,"workspaces":{}}`,
		"nonboolean tailscale":         `{"tailscale":"true","workspaces":{}}`,
		"duplicate tailscale":          `{"tailscale":true,"tailscale":false,"workspaces":{}}`,
		"null mounts":                  `{"mounts":null}`,
		"nonobject mounts":             `{"mounts":[]}`,
		"case duplicate mount":         `{"mounts":{"Shared":{"path":"C:\\one","readOnly":true},"shared":{"path":"C:\\two","readOnly":false}}}`,
		"nonobject mount":              `{"mounts":{"shared":"C:\\shared"}}`,
		"missing mount path":           `{"mounts":{"shared":{"readOnly":true}}}`,
		"missing mount access":         `{"mounts":{"shared":{"path":"C:\\shared"}}}`,
		"nonboolean mount access":      `{"mounts":{"shared":{"path":"C:\\shared","readOnly":"true"}}}`,
		"unknown mount field":          `{"mounts":{"shared":{"path":"C:\\shared","readOnly":true,"guest":"C:\\Shared"}}}`,
		"null workspaces":              `{"workspaces":null}`,
		"case variant field":           `{"MemoryMB":32768,"workspaces":{}}`,
		"duplicate field":              `{"memoryMB":32768,"memoryMB":16384,"workspaces":{}}`,
		"duplicate workspace":          `{"workspaces":{"alpha":"C:\\one","alpha":"C:\\two"}}`,
		"case duplicate workspace":     `{"workspaces":{"Alpha":"C:\\one","alpha":"C:\\two"}}`,
		"null workspace path":          `{"workspaces":{"alpha":null}}`,
		"null package delta":           `{"wingetPackages":null,"workspaces":{}}`,
		"unknown package field":        `{"wingetPackages":{"remove":[],"add":[],"versions":{},"extra":true},"workspaces":{}}`,
	}
	for name, contents := range tests {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), globalConfigurationName)
			if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := loadGlobalConfiguration(path); err == nil {
				t.Fatalf("noncanonical configuration unexpectedly succeeded: %s", contents)
			}
		})
	}
}

func TestLoadGlobalConfigurationDefaultsMissingOptionalFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), globalConfigurationName)
	if err := os.WriteFile(path, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	config, err := loadGlobalConfiguration(path)
	if err != nil {
		t.Fatalf("loadGlobalConfiguration: %v", err)
	}
	if config.CacheDirectory != "" || config.WorktreeDirectory != "" || config.MemoryMB == nil || *config.MemoryMB != defaultMemoryMB || config.AudioOutput || config.AudioInput || config.Tailscale || config.WorkspaceDiscovery != nil ||
		len(config.WingetPackages.Remove) != 0 || !slices.Equal(config.WingetPackages.Add, defaultCodingAgentPackageIDs()) ||
		len(config.WingetPackages.Versions) != 0 || len(config.Mounts) != 0 || len(config.Workspaces) != 0 {
		t.Fatalf("configuration = %#v", config)
	}
}

func TestLoadGlobalConfigurationEnablesAudioOutputOnlyForExactBoolean(t *testing.T) {
	path := filepath.Join(t.TempDir(), globalConfigurationName)
	if err := os.WriteFile(path, []byte(`{"audio":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	config, err := loadGlobalConfiguration(path)
	if err != nil {
		t.Fatalf("loadGlobalConfiguration: %v", err)
	}
	if !config.AudioOutput || config.AudioInput {
		t.Fatal("audio playback was not enabled")
	}
}

func TestLoadGlobalConfigurationEnablesAudioInputOnlyForExactBoolean(t *testing.T) {
	path := filepath.Join(t.TempDir(), globalConfigurationName)
	if err := os.WriteFile(path, []byte(`{"audioInput":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	config, err := loadGlobalConfiguration(path)
	if err != nil {
		t.Fatalf("loadGlobalConfiguration: %v", err)
	}
	if config.AudioOutput || !config.AudioInput {
		t.Fatal("microphone input was not enabled independently")
	}
}

func TestLoadGlobalConfigurationEnablesTailscaleOnlyForExactBoolean(t *testing.T) {
	path := filepath.Join(t.TempDir(), globalConfigurationName)
	if err := os.WriteFile(path, []byte(`{"tailscale":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	config, err := loadGlobalConfiguration(path)
	if err != nil {
		t.Fatalf("loadGlobalConfiguration: %v", err)
	}
	if !config.Tailscale {
		t.Fatal("tailscale was not enabled")
	}
}

func TestValidateTailscalePackageSelectionRequiresInstalledClient(t *testing.T) {
	terminal := testStableWindowsTerminalConfiguration()
	packages, err := resolveWingetPackagePlan(defaultWingetPackageConfiguration(), terminal)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateTailscalePackageSelection(true, packages); err != nil {
		t.Fatalf("enabled default package: %v", err)
	}
	packages.Defaults = slices.DeleteFunc(packages.Defaults, func(entry wingetPackagePlanEntry) bool {
		return strings.EqualFold(entry.ID, packageTailscale)
	})
	if err := validateTailscalePackageSelection(true, packages); err == nil || !strings.Contains(err.Error(), packageTailscale) {
		t.Fatalf("missing package error = %v", err)
	}
	if err := validateTailscalePackageSelection(false, packages); err != nil {
		t.Fatalf("disabled integration: %v", err)
	}
}

func TestValidateWorktreePackageSelectionRequiresGit(t *testing.T) {
	terminal := testStableWindowsTerminalConfiguration()
	packages, err := resolveWingetPackagePlan(defaultWingetPackageConfiguration(), terminal)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateWorktreePackageSelection(t.TempDir(), packages); err != nil {
		t.Fatalf("enabled default package: %v", err)
	}
	packages.Defaults = slices.DeleteFunc(packages.Defaults, func(entry wingetPackagePlanEntry) bool {
		return strings.EqualFold(entry.ID, packageGit)
	})
	if err := validateWorktreePackageSelection(t.TempDir(), packages); err == nil || !strings.Contains(err.Error(), packageGit) {
		t.Fatalf("missing package error = %v", err)
	}
	if err := validateWorktreePackageSelection("", packages); err != nil {
		t.Fatalf("disabled worktrees = %v", err)
	}
}

func TestResolveProvisioningUsesConfiguredCacheDirectory(t *testing.T) {
	root := t.TempDir()
	defaults := filepath.Join(root, "defaults")
	global := filepath.Join(root, "global")
	cache := filepath.Join(root, "cache-on-another-drive")
	if err := os.MkdirAll(defaults, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(defaults, baseProvisioningName), []byte("base"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(global, 0o700); err != nil {
		t.Fatal(err)
	}
	config, err := json.Marshal(globalConfiguration{
		CacheDirectory:          cache,
		MobileSSHAuthorizedKeys: []string{},
		WingetPackages:          defaultWingetPackageConfiguration(),
		Mounts:                  map[string]mountConfiguration{},
		Workspaces:              map[string]string{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(global, globalConfigurationName), config, 0o600); err != nil {
		t.Fatal(err)
	}

	plan, err := resolveProvisioningAt(root, global, defaults)
	if err != nil {
		t.Fatalf("resolveProvisioningAt: %v", err)
	}
	if plan.CacheDirectory != cache {
		t.Fatalf("cache directory = %q, want %q", plan.CacheDirectory, cache)
	}
}

func TestResolveProvisioningRejectsRelativeCacheDirectory(t *testing.T) {
	root := t.TempDir()
	defaults := filepath.Join(root, "defaults")
	global := filepath.Join(root, "global")
	if err := os.MkdirAll(defaults, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(defaults, baseProvisioningName), []byte("base"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(global, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(global, globalConfigurationName), []byte(`{"cacheDirectory":"cache","workspaces":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := resolveProvisioningAt(root, global, defaults); err == nil {
		t.Fatal("relative cacheDirectory unexpectedly succeeded")
	}
}

func TestValidateConfiguredCacheDirectoryRejectsVolumeRoot(t *testing.T) {
	volumeRoot := filepath.VolumeName(t.TempDir()) + string(os.PathSeparator)
	if _, err := validateConfiguredCacheDirectory(volumeRoot); err == nil {
		t.Fatal("volume-root cacheDirectory unexpectedly succeeded")
	}
}

func createWorkspaceFixture(t *testing.T, root, name string) string {
	t.Helper()
	directory := filepath.Join(root, name)
	configuration := filepath.Join(directory, projectConfigurationName)
	if err := os.MkdirAll(filepath.Join(directory, "src"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(configuration, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configuration, projectProvisioningName), []byte(name), 0o600); err != nil {
		t.Fatal(err)
	}
	return directory
}

func writeWorkspaceDiscoveryConfig(t *testing.T, global string, discovery *workspaceDiscoveryConfiguration, workspaces map[string]string) {
	t.Helper()
	if err := os.MkdirAll(global, 0o700); err != nil {
		t.Fatal(err)
	}
	if workspaces == nil {
		workspaces = map[string]string{}
	}
	contents, err := json.Marshal(struct {
		WorkspaceDiscovery *workspaceDiscoveryConfiguration `json:"workspaceDiscovery,omitempty"`
		Workspaces         map[string]string                `json:"workspaces"`
	}{WorkspaceDiscovery: discovery, Workspaces: workspaces})
	if err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(global, globalConfigurationName), string(contents))
}

func TestEnsureGlobalProvisioningSeedsUserWithoutOverwriting(t *testing.T) {
	root := t.TempDir()
	global := filepath.Join(root, "global")
	if err := ensureGlobalProvisioning(global); err != nil {
		t.Fatalf("ensureGlobalProvisioning: %v", err)
	}
	if _, err := os.Stat(filepath.Join(global, globalConfigurationName)); err != nil {
		t.Fatalf("global workspace config was not seeded: %v", err)
	}
	config, err := loadGlobalConfiguration(filepath.Join(global, globalConfigurationName))
	if err != nil {
		t.Fatalf("load seeded config: %v", err)
	}
	if config.CacheDirectory != "" || config.WorktreeDirectory != "" || config.MemoryMB == nil || *config.MemoryMB != defaultMemoryMB || config.AudioOutput || config.AudioInput || config.Tailscale ||
		config.MobileSSHAuthorizedKeys == nil || len(config.MobileSSHAuthorizedKeys) != 0 ||
		config.WingetPackages.Remove == nil || config.WingetPackages.Add == nil || config.WingetPackages.Versions == nil ||
		!slices.Equal(config.WingetPackages.Add, defaultCodingAgentPackageIDs()) ||
		config.WorkspaceDiscovery == nil || config.WorkspaceDiscovery.Root != "" || config.WorkspaceDiscovery.Exclude == nil || config.Mounts == nil || config.Workspaces == nil {
		t.Fatalf("seeded config = %#v", config)
	}
	seededContents, err := os.ReadFile(filepath.Join(global, globalConfigurationName))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(seededContents, []byte(`"audio": false`)) {
		t.Fatalf("seeded config does not expose the default-silent audio setting: %s", seededContents)
	}
	if !bytes.Contains(seededContents, []byte(`"audioInput": false`)) {
		t.Fatalf("seeded config does not expose the default-disabled microphone setting: %s", seededContents)
	}
	if !bytes.Contains(seededContents, []byte(`"worktreeDirectory": ""`)) {
		t.Fatalf("seeded config does not expose the optional worktree directory: %s", seededContents)
	}
	if !bytes.Contains(seededContents, []byte(`"mobileSSHAuthorizedKeys": []`)) {
		t.Fatalf("seeded config does not expose disabled mobile SSH access: %s", seededContents)
	}
	if !bytes.Contains(seededContents, []byte(`"pullHostGitRepositoriesOnUp": true`)) ||
		!bytes.Contains(seededContents, []byte(`"pullHostGitRepositoriesOnDown": true`)) ||
		!config.ConfigurationSync.PullHostGitRepositoriesOnUp || !config.ConfigurationSync.PullHostGitRepositoriesOnDown {
		t.Fatalf("seeded config does not expose default-on host configuration pulls: %s", seededContents)
	}
	if !bytes.Contains(seededContents, []byte(`"mounts": {}`)) {
		t.Fatalf("seeded config does not expose named folder mounts: %s", seededContents)
	}
	for _, id := range defaultCodingAgentPackageIDs() {
		if !bytes.Contains(seededContents, []byte(`"`+id+`"`)) {
			t.Fatalf("seeded config does not expose default coding agent %s: %s", id, seededContents)
		}
	}
	remaining := seededContents
	for _, field := range []string{`"cacheDirectory"`, `"worktreeDirectory"`, `"memoryMB"`, `"audio"`, `"audioInput"`, `"tailscale"`, `"mobileSSHAuthorizedKeys"`, `"codingAgentSync"`, `"workspaces"`, `"mounts"`, `"workspaceDiscovery"`, `"wingetPackages"`} {
		index := bytes.Index(remaining, []byte(field))
		if index < 0 {
			t.Fatalf("seeded config field order is missing %s: %s", field, seededContents)
		}
		remaining = remaining[index+len(field):]
	}
	user := filepath.Join(global, userProvisioningName)
	custom := []byte(userProvisioningContract + "\nWrite-Output 'custom'\n")
	if err := os.WriteFile(user, custom, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ensureGlobalProvisioning(global); err != nil {
		t.Fatalf("second ensureGlobalProvisioning: %v", err)
	}
	data, err := os.ReadFile(user)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, custom) {
		t.Fatalf("user provisioning was overwritten: %q", data)
	}
}

func TestEnsureGlobalProvisioningPreservesAndRefusesLegacyBase(t *testing.T) {
	global := t.TempDir()
	legacy := filepath.Join(global, baseProvisioningName)
	legacyData := []byte(baseProvisioningContract + "\nWrite-Output 'legacy customization'\n")
	if err := os.WriteFile(legacy, legacyData, 0o600); err != nil {
		t.Fatal(err)
	}
	err := ensureGlobalProvisioning(global)
	if err == nil || !strings.Contains(err.Error(), "was not modified and will not be executed") || !strings.Contains(err.Error(), userProvisioningName) {
		t.Fatalf("legacy Base migration error = %v", err)
	}
	got, readErr := os.ReadFile(legacy)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if !bytes.Equal(got, legacyData) {
		t.Fatalf("legacy Base changed: %q", got)
	}
	if err := validateUserProvisioningContract(filepath.Join(global, userProvisioningName)); err != nil {
		t.Fatalf("user provisioning was not seeded before migration refusal: %v", err)
	}
}

func TestInstallerSeedCreatesDefaultsWithoutOwningLegacyBase(t *testing.T) {
	global := t.TempDir()
	legacy := filepath.Join(global, baseProvisioningName)
	legacyData := []byte("user-owned legacy Base")
	if err := os.WriteFile(legacy, legacyData, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := seedGlobalProvisioning(global); err != nil {
		t.Fatalf("seedGlobalProvisioning: %v", err)
	}
	for _, path := range []string{
		filepath.Join(global, globalConfigurationName),
		filepath.Join(global, userProvisioningName),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("installer default was not seeded %s: %v", path, err)
		}
	}
	got, err := os.ReadFile(legacy)
	if err != nil || !bytes.Equal(got, legacyData) {
		t.Fatalf("legacy Base changed: %q, %v", got, err)
	}
}

func TestInstallerSeedRollsBackOnlyDefaultsCreatedByFailedAttempt(t *testing.T) {
	global := filepath.Join(t.TempDir(), "global")
	if err := os.MkdirAll(filepath.Join(global, globalConfigurationName), 0o700); err != nil {
		t.Fatal(err)
	}
	err := seedInstallerConfigurationRoot(global)
	if err == nil || !strings.Contains(err.Error(), globalConfigurationName) {
		t.Fatalf("seedInstallerConfigurationRoot error = %v", err)
	}
	if _, statErr := os.Lstat(filepath.Join(global, userProvisioningName)); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("user default created by failed installer seed was not rolled back: %v", statErr)
	}
	if info, statErr := os.Stat(filepath.Join(global, globalConfigurationName)); statErr != nil || !info.IsDir() {
		t.Fatalf("preexisting unknown configuration entry changed: %v, %#v", statErr, info)
	}
}

func TestSeedFileOnceNeverReplacesExistingUserFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), globalConfigurationName)
	existing := []byte("user-owned configuration")
	if err := os.WriteFile(path, existing, 0o600); err != nil {
		t.Fatal(err)
	}
	validated := false
	if err := seedFileOnce(path, defaultGlobalConfiguration, "test configuration", func(candidate string) error {
		validated = true
		if candidate != path {
			t.Fatalf("validated path = %q, want %q", candidate, path)
		}
		return nil
	}); err != nil {
		t.Fatalf("seedFileOnce existing file: %v", err)
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !validated || !bytes.Equal(contents, existing) {
		t.Fatalf("existing user file changed: validated = %t, contents = %q", validated, contents)
	}
}
