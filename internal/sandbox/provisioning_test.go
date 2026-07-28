package sandbox

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"os"
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

func TestDefaultBaseForcesManagedOpenCodeAllowAllAfterAgentMerge(t *testing.T) {
	text := readDefaultBaseProvisioning(t)
	for _, required := range []string{
		"$openCodeManagedDirectory = Join-Path $env:ProgramData 'opencode'",
		"sandbox-allow-all.js",
		"config.permission = allowAll()",
		"for (const agent of Object.values(config.agent ?? {}))",
		"agent.permission = allowAll()",
		"external_directory = 'allow'",
		"task = 'allow'",
		"todowrite = 'allow'",
		"doom_loop = 'allow'",
		"$openCodeCommand.Source 'debug' 'config'",
		"OpenCode effective managed permission is not allow",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("default Base is missing OpenCode allow-all contract %q", required)
		}
	}
	if strings.Contains(text, `"permission": "allow"`) {
		t.Fatal("default Base still relies on the merge-unsafe scalar OpenCode permission")
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

func TestDefaultBaseInstallsPowerShell7WithoutUsingItForProvisioningAndRestartsExplorerOnlyForRegistryChanges(t *testing.T) {
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
		"Stopping all Explorer processes after $Role",
		"$explorerProcesses.Count -eq 0",
		"Starting one fresh Explorer shell",
		"Start-Process -FilePath (Join-Path $env:WINDIR 'explorer.exe')",
		"Explorer shell restarted:",
		"Restart-ProvisioningExplorerShell -Role 'registry changes'",
		"Registry state already matches; Explorer restart skipped.",
		"early registry and Explorer customization",
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
	stopIndex := strings.Index(text, "$explorerProcesses | Stop-Process -Force")
	zeroIndex := strings.Index(text, "if ($explorerProcesses.Count -eq 0) { break }")
	startIndex := strings.Index(text, "Start-Process -FilePath (Join-Path $env:WINDIR 'explorer.exe')")
	if stopIndex < 0 || zeroIndex < 0 || startIndex < 0 || zeroIndex > stopIndex || startIndex < stopIndex {
		t.Fatalf("Explorer stop/zero/start ordering is not explicit: zero=%d stop=%d start=%d", zeroIndex, stopIndex, startIndex)
	}
	powerShellInstallIndex := strings.Index(text, "Write-Output 'Installing PowerShell 7...'")
	registryReturnIndex := strings.Index(text, "Write-ProvisioningTiming -Role 'early registry and Explorer customization'")
	if registryReturnIndex < startIndex || powerShellInstallIndex < registryReturnIndex {
		t.Fatalf("PowerShell installation must follow the completed registry phase: explorer=%d registryReturn=%d PowerShell=%d", startIndex, registryReturnIndex, powerShellInstallIndex)
	}
}

func TestDefaultBaseDisablesPlaybackUnlessAudioIsEnabled(t *testing.T) {
	text := readDefaultBaseProvisioning(t)
	for _, required := range []string{
		"[switch]$AudioEnabled",
		"function Initialize-ProvisioningAudioEndpointType",
		"interface IMMDeviceEnumerator",
		"interface IAudioEndpointVolume",
		"SilenceDefaultRenderEndpoint",
		"SetMasterVolumeLevelScalar(0.0f, IntPtr.Zero)",
		"SetMute(true, IntPtr.Zero)",
		"function Disable-ProvisioningAudioPlayback",
		"AppEvents\\Schemes",
		"-Name '' -Value '.None' -PropertyType 'String'",
		"$serviceNames = @('Audiosrv', 'AudioEndpointBuilder')",
		"Set-Service -Name $serviceName -StartupType Disabled",
		"$controller.WaitForStatus(",
		"[System.ServiceProcess.ServiceControllerStatus]::Stopped",
		"Get-CimInstance -ClassName Win32_Service",
		"Audio disable policy failed",
		"if (-not $AudioEnabled)",
		"Disable-ProvisioningAudioPlayback",
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
		"Values = [ordered]@{ Value = 'Deny' }",
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

func TestDefaultBaseUsesSupportedIdempotentTerminalTaskbarPolicy(t *testing.T) {
	text := readDefaultBaseProvisioning(t)
	for _, required := range []string{
		"function Ensure-ProvisioningWindowsTerminalTaskbarPin",
		"MDM_Policy_User_Config01_Start02",
		`root\cimv2\mdm\dmmap`,
		"./Vendor/MSFT/Policy/Config",
		"<CustomTaskbarLayoutCollection>",
		"<taskbar:UWA AppUserModelID=\"",
		"[Net.WebUtility]::HtmlEncode($layout)",
		"StartLayout -ceq $layout",
		"taskbar policy read-back did not match the canonical decoded layout",
		"Restart-ProvisioningExplorerShell -Role 'Windows Terminal taskbar policy change'",
		"Ensure-ProvisioningWindowsTerminalTaskbarPin -Edition $WindowsTerminalEdition",
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
}

func TestDefaultBaseSkipsMatchingPackageAndConfigurationState(t *testing.T) {
	text := readDefaultBaseProvisioning(t)
	for _, required := range []string{
		"function Test-ProvisioningWinGetPackageInstalled",
		"function Test-ProvisioningPortablePackageInstalled",
		"function Test-ProvisioningRustupInstalled",
		"function Test-ProvisioningGeistMonoFontInstalled",
		"if (Test-ProvisioningPackageInstalled -Metadata $metadata -Adapter $Adapter -ExecutableName $ExecutableName)",
		"already matches requested version:",
		"installed package does not match resolved version",
		"[IO.File]::ReadAllText($powerShellProfilePath) -cne $starshipInitialization",
		"[IO.File]::ReadAllText($managedFile.Path) -cne $managedFile.Contents",
		"if (($existingSafeDirectories -join '|') -cne ($guestSafeDirectories -join '|'))",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("default Base is missing exact-state idempotence contract %q", required)
		}
	}
}

func TestDefaultStackLibraryExposesFineGrainedFunctionsWithoutHerdrPrefixes(t *testing.T) {
	text := readDefaultStackProvisioning(t)
	for _, required := range []string{
		stackProvisioningContract,
		"function Resolve-StackPythonPackage",
		"function Resolve-StackRustDistribution",
		"function Install-GoStack",
		"function Install-NodeStack",
		"function Install-PythonStack",
		"function Install-ZigStack",
		"function Install-RustMSVCStack",
		"function Install-CargoNextest",
		"function Install-Just",
		"function Install-StackVisualStudioBuildTools",
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
		"function Install-Herdr",
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
	for _, unrelated := range []string{"-Id 'zig.zig'", "-Id 'nextest.cargo-nextest'", "-Id 'Casey.Just'"} {
		if strings.Contains(rust, unrelated) {
			t.Fatalf("Rust stack contains unrelated package %q", unrelated)
		}
	}
	if !strings.Contains(rust, "-Id 'Rustlang.Rustup'") || !strings.Contains(rust, "Install-StackVisualStudioBuildTools") {
		t.Fatal("Rust stack lost rustup or Visual Studio ownership")
	}
	base := readDefaultBaseProvisioning(t)
	dotSource := strings.Index(base, ". $stackProvisioning")
	userCall := strings.Index(base, "& $userProvisioning.FullName")
	projectCall := strings.Index(base, "& $projectScript.FullName -ProjectDirectory $projectDirectory")
	if dotSource < 0 || userCall <= dotSource || projectCall <= userCall {
		t.Fatalf("provisioning order must be stack, user, project: stack=%d user=%d project=%d", dotSource, userCall, projectCall)
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
Import-Module Microsoft.PowerShell.Utility -ErrorAction Stop
$tokens = $null
$errors = $null
$ast = [Management.Automation.Language.Parser]::ParseFile('%s', [ref]$tokens, [ref]$errors)
if ($errors.Count -ne 0) { throw $errors[0].Message }
foreach ($name in @('Assert-StackRustMirrorPayloads', 'Test-StackRustMirrorCacheEntry')) {
    $definition = $ast.Find({ param($node) $node -is [Management.Automation.Language.FunctionDefinitionAst] -and $node.Name -ceq $name }, $true)
    if ($null -eq $definition) { throw "Missing function $name" }
    Invoke-Expression $definition.Extent.Text
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
	quote := func(value string) string { return strings.ReplaceAll(value, "'", "''") }
	script := fmt.Sprintf(`$ErrorActionPreference = 'Stop'
trap { Write-Output ($_ | Out-String); exit 1 }
$tokens = $null
$errors = $null
$ast = [Management.Automation.Language.Parser]::ParseFile('%s', [ref]$tokens, [ref]$errors)
if ($errors.Count -ne 0) { throw $errors[0].Message }
$definition = $ast.Find({ param($node) $node -is [Management.Automation.Language.FunctionDefinitionAst] -and $node.Name -ceq 'Install-ProvisioningOnlineWinGetPackage' }, $true)
if ($null -eq $definition) { throw 'Missing online WinGet installer function.' }
Invoke-Expression $definition.Extent.Text
$script:searchCalls = 0
$script:installArguments = @()
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
    return @()
}
function Update-ProvisioningPath { }
function Test-ProvisioningWinGetPackageInstalled {
    param($Metadata)
    $script:verifiedVersion = [string]$Metadata.Version
    return $true
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
`, quote(basePath))
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
		"function Read-ProvisioningPackagePlan",
		"function Test-ProvisioningPackageEnabled",
		"function Get-ProvisioningPackageVersion",
		"WinGet package plan is missing Core package Microsoft.PowerShell",
		"if (Test-ProvisioningPackageEnabled -Id 'Git.Git')",
		"if (Test-ProvisioningPackageEnabled -Id 'GitHub.cli')",
		"if (Test-ProvisioningPackageEnabled -Id 'SST.opencode')",
		"foreach ($package in @($provisioningPackagePlan.Data.additions))",
		"Install-ProvisioningOnlineWinGetPackage -Role \"additional WinGet package $packageID\"",
		"-Id $packageID -Version ([string]$package.version)",
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

func TestNativeProcessTreeWaitUsesReturnedExitCodeInWindowsPowerShell51(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows PowerShell 5.1 regression")
	}
	baseScript := defaultProvisioningPath(t, baseProvisioningName)
	powerShell := mustWindowsPowerShellPath(t)
	quote := func(value string) string { return strings.ReplaceAll(value, "'", "''") }
	script := fmt.Sprintf(`$ErrorActionPreference = 'Stop'
$tokens = $null
$errors = $null
$ast = [System.Management.Automation.Language.Parser]::ParseFile('%s', [ref]$tokens, [ref]$errors)
$definition = $ast.Find({ param($node) $node -is [System.Management.Automation.Language.FunctionDefinitionAst] -and $node.Name -eq 'Invoke-ProvisioningNative' }, $true)
Invoke-Expression $definition.Extent.Text
function Write-ProvisioningProgress { param([string]$Message) }
function Write-ProvisioningTiming { param([string]$Role, [double]$Seconds) }
function Get-ProvisioningBoundedDiagnosticText { param([string]$Text, [int]$MaximumBytes) return $Text }
$global:LASTEXITCODE = 91
Invoke-ProvisioningNative -Role 'fixture success' -FilePath '%s' -ArgumentList @('-NoLogo', '-NoProfile', '-NonInteractive', '-EncodedCommand', '%s') -WaitForProcessTree | Out-Null
$failed = $false
try {
    Invoke-ProvisioningNative -Role 'fixture failure' -FilePath '%s' -ArgumentList @('-NoLogo', '-NoProfile', '-NonInteractive', '-EncodedCommand', '%s') -WaitForProcessTree | Out-Null
} catch {
    if ($_.Exception.Message -notmatch 'exit code 23') { throw }
    $failed = $true
}
if (-not $failed) { throw 'Process-tree wait ignored the returned nonzero exit code.' }
`, quote(baseScript), quote(powerShell), encodePowerShell("exit 0"), quote(powerShell), encodePowerShell("exit 23"))
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

func TestWaitProvisioningCommandAvailableRejectsExcludedAlias(t *testing.T) {
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
        [pscustomobject]@{ Source = 'C:\Program Files\Python313\python.exe' }
    )
}
$resolved = Wait-ProvisioningCommandAvailable -Role 'Python fixture' -Name 'python.exe' -TimeoutSeconds 1 -DelayMilliseconds 25 -CommandSourceExclusion '*\Microsoft\WindowsApps\python.exe'
if ($resolved -cne 'C:\Program Files\Python313\python.exe') { throw "Resolved unexpected Python command: $resolved" }
`, quote(baseScript))
	powerShell := mustWindowsPowerShellPath(t)
	command := hiddenCommand(powerShell, "-NoLogo", "-NoProfile", "-NonInteractive", "-EncodedCommand", encodePowerShell(script))
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("excluded alias regression: %v: %s", err, output)
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
	}, map[string]string{"Alpha-Project": external, "custom-alpha": explicit})

	plan, err := resolveProvisioningAt(filepath.Join(active, "src"), global, defaults)
	if err != nil {
		t.Fatalf("resolveProvisioningAt: %v", err)
	}
	if len(plan.Workspaces) != 3 || !plan.Workspaces[0].Active || plan.Workspaces[0].Name != "zeta" ||
		plan.Workspaces[1].Name != "Alpha-Project" || plan.Workspaces[2].Name != "custom-alpha" {
		t.Fatalf("discovered workspaces = %#v", plan.Workspaces)
	}
	for index, expected := range []string{active, external, explicit} {
		equal, err := workspaceDirectoriesEqual(plan.Workspaces[index].HostDirectory, expected)
		if err != nil || !equal {
			t.Fatalf("workspace %q path = %q, want physical path %q: %v", plan.Workspaces[index].Name, plan.Workspaces[index].HostDirectory, expected, err)
		}
	}
	encoded, err := renderConfig(filepath.Join(root, "run-input"), filepath.Join(root, "run-status"), filepath.Join(root, "cache"), plan.Workspaces, plan.MemoryMB, plan.Audio)
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
		if err := os.MkdirAll(filepath.Join(root, "not-a-project"), 0o700); err != nil {
			t.Fatal(err)
		}
		_, err := discoverWorkspacePlans(&workspaceDiscoveryConfiguration{Root: root, Exclude: []string{}})
		if err == nil || !strings.Contains(err.Error(), "not-a-project") || !strings.Contains(err.Error(), "provisioning script") {
			t.Fatalf("unprofiled child error = %v", err)
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

func TestResolveProvisioningUsesConfiguredMemoryAndAudio(t *testing.T) {
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
	if err := os.WriteFile(filepath.Join(global, globalConfigurationName), []byte(`{"memoryMB":16384,"audio":true,"workspaces":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}

	plan, err := resolveProvisioningAt(root, global, defaults)
	if err != nil {
		t.Fatalf("resolveProvisioningAt: %v", err)
	}
	if plan.MemoryMB != 16384 || !plan.Audio {
		t.Fatalf("resolved runtime config = memory %d, audio %t", plan.MemoryMB, plan.Audio)
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
		"null cache":               `{"cacheDirectory":null,"workspaces":{}}`,
		"null audio":               `{"audio":null,"workspaces":{}}`,
		"nonboolean audio":         `{"audio":"true","workspaces":{}}`,
		"duplicate audio":          `{"audio":true,"audio":false,"workspaces":{}}`,
		"null tailscale":           `{"tailscale":null,"workspaces":{}}`,
		"nonboolean tailscale":     `{"tailscale":"true","workspaces":{}}`,
		"duplicate tailscale":      `{"tailscale":true,"tailscale":false,"workspaces":{}}`,
		"null workspaces":          `{"workspaces":null}`,
		"case variant field":       `{"MemoryMB":32768,"workspaces":{}}`,
		"duplicate field":          `{"memoryMB":32768,"memoryMB":16384,"workspaces":{}}`,
		"duplicate workspace":      `{"workspaces":{"alpha":"C:\\one","alpha":"C:\\two"}}`,
		"case duplicate workspace": `{"workspaces":{"Alpha":"C:\\one","alpha":"C:\\two"}}`,
		"null workspace path":      `{"workspaces":{"alpha":null}}`,
		"null package delta":       `{"wingetPackages":null,"workspaces":{}}`,
		"unknown package field":    `{"wingetPackages":{"remove":[],"add":[],"versions":{},"extra":true},"workspaces":{}}`,
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
	if config.CacheDirectory != "" || config.MemoryMB == nil || *config.MemoryMB != defaultMemoryMB || config.Audio || config.Tailscale || config.WorkspaceDiscovery != nil ||
		len(config.WingetPackages.Remove) != 0 || len(config.WingetPackages.Add) != 0 ||
		len(config.WingetPackages.Versions) != 0 || len(config.Workspaces) != 0 {
		t.Fatalf("configuration = %#v", config)
	}
}

func TestLoadGlobalConfigurationEnablesAudioOnlyForExactBoolean(t *testing.T) {
	path := filepath.Join(t.TempDir(), globalConfigurationName)
	if err := os.WriteFile(path, []byte(`{"audio":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	config, err := loadGlobalConfiguration(path)
	if err != nil {
		t.Fatalf("loadGlobalConfiguration: %v", err)
	}
	if !config.Audio {
		t.Fatal("audio playback was not enabled")
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
		CacheDirectory: cache,
		WingetPackages: defaultWingetPackageConfiguration(),
		Workspaces:     map[string]string{},
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
	if config.CacheDirectory != "" || config.MemoryMB == nil || *config.MemoryMB != defaultMemoryMB || config.Audio || config.Tailscale ||
		config.WingetPackages.Remove == nil || config.WingetPackages.Add == nil || config.WingetPackages.Versions == nil ||
		config.WorkspaceDiscovery == nil || config.WorkspaceDiscovery.Root != "" || config.WorkspaceDiscovery.Exclude == nil || config.Workspaces == nil {
		t.Fatalf("seeded config = %#v", config)
	}
	seededContents, err := os.ReadFile(filepath.Join(global, globalConfigurationName))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(seededContents, []byte(`"audio": false`)) {
		t.Fatalf("seeded config does not expose the default-silent audio setting: %s", seededContents)
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
