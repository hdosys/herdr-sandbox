package sandbox

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf16"
)

//go:embed assets/configuration-sync.ps1
var configurationSyncScript []byte

//go:embed assets/agent-worktree-instructions.md
var agentWorktreeInstructions []byte

const (
	agentWorktreeInstructionsStart = "<!-- herdr-sandbox:worktrees:start -->"
	agentWorktreeInstructionsEnd   = "<!-- herdr-sandbox:worktrees:end -->"
)

const (
	maximumConfigurationFileSize                      = 32 * 1024 * 1024
	maximumConfigurationSize                          = 128 * 1024 * 1024
	windowsTerminalStableEdition                      = "stable"
	windowsTerminalPreviewEdition                     = "preview"
	windowsTerminalEditionArchivePath                 = "windows-terminal/edition.txt"
	starshipPresetArchivePath                         = "starship/preset.txt"
	githubCLIAuthenticationArchivePath                = "github-cli/authentication.json"
	configurationApplyScriptArchivePath               = "herdr-sandbox/apply.ps1"
	configurationWorkspaceManifestPath                = "herdr-sandbox/workspaces.json"
	configurationPackagePlanArchivePath               = "herdr-sandbox/winget-packages.json"
	configurationWorktreeDirectoryArchivePath         = "herdr-sandbox/worktree-directory.txt"
	configurationAgentWorktreeInstructionsArchivePath = "herdr-sandbox/agent-worktree-instructions.md"
	powerShellProfileGUID                             = "{574e775e-4f2a-5b96-ac1e-a2962a402336}"
	powerShellCommandLine                             = `pwsh.exe`
	windowsTerminalGuestFont                          = "GeistMono Nerd Font"
	windowsTerminalLightTheme                         = "light"
	windowsTerminalDarkTheme                          = "dark"
	starshipPastelPowerlinePreset                     = "pastel-powerline"
	starshipCatppuccinLattePreset                     = "catppuccin-powerline-latte"
	maximumGitHubCLIAccounts                          = 32
	maximumGitHubCLIStatusSize                        = 256 * 1024
	maximumGitHubCLITokenSize                         = 16 * 1024
	maximumGitHubCLILoginSize                         = 1024
	maximumConfigurationFiles                         = 4096
)

type windowsTerminalConfiguration struct {
	Edition           string
	Theme             string
	WinGetPackageID   string
	PackageFamilyName string
	SettingsPath      string
	FragmentsPath     string
}

func validateAgentWorktreeInstructions(data []byte) error {
	if len(data) == 0 || len(data) > 32*1024 {
		return errors.New("embedded agent worktree instructions must be nonempty and bounded")
	}
	if bytes.IndexByte(data, 0) >= 0 || bytes.Count(data, []byte(agentWorktreeInstructionsStart)) != 1 || bytes.Count(data, []byte(agentWorktreeInstructionsEnd)) != 1 {
		return errors.New("embedded agent worktree instructions have invalid ownership markers")
	}
	text := string(data)
	start := strings.Index(text, agentWorktreeInstructionsStart)
	end := strings.Index(text, agentWorktreeInstructionsEnd)
	if start != 0 || end <= start || strings.TrimSpace(text[end+len(agentWorktreeInstructionsEnd):]) != "" {
		return errors.New("embedded agent worktree instructions have invalid marker ordering")
	}
	for _, required := range []string{
		`herdr worktree create --cwd`,
		`herdr worktree list --cwd`,
		`herdr worktree open --cwd`,
		`herdr worktree remove --workspace`,
		`Omit ` + "`--path`",
		`This does not delete`,
	} {
		if !strings.Contains(text, required) {
			return fmt.Errorf("embedded agent worktree instructions are missing %q", required)
		}
	}
	return nil
}

func detectHostWindowsTerminalConfiguration() (windowsTerminalConfiguration, error) {
	localAppData := strings.TrimSpace(os.Getenv("LOCALAPPDATA"))
	return detectHostWindowsTerminal(localAppData)
}

func detectHostWindowsTerminal(localAppData string) (windowsTerminalConfiguration, error) {
	if !filepath.IsAbs(localAppData) {
		return windowsTerminalConfiguration{}, fmt.Errorf("LOCALAPPDATA is not absolute: %q", localAppData)
	}
	fragments := filepath.Join(localAppData, "Microsoft", "Windows Terminal", "Fragments")
	candidates := []windowsTerminalConfiguration{
		{
			Edition:           windowsTerminalPreviewEdition,
			WinGetPackageID:   "Microsoft.WindowsTerminal.Preview",
			PackageFamilyName: "Microsoft.WindowsTerminalPreview_8wekyb3d8bbwe",
			SettingsPath:      filepath.Join(localAppData, "Packages", "Microsoft.WindowsTerminalPreview_8wekyb3d8bbwe", "LocalState", "settings.json"),
			FragmentsPath:     fragments,
		},
		{
			Edition:           windowsTerminalStableEdition,
			WinGetPackageID:   "Microsoft.WindowsTerminal",
			PackageFamilyName: "Microsoft.WindowsTerminal_8wekyb3d8bbwe",
			SettingsPath:      filepath.Join(localAppData, "Packages", "Microsoft.WindowsTerminal_8wekyb3d8bbwe", "LocalState", "settings.json"),
			FragmentsPath:     fragments,
		},
	}
	for _, candidate := range candidates {
		exists, err := regularFileExists(candidate.SettingsPath)
		if err != nil {
			return windowsTerminalConfiguration{}, fmt.Errorf("inspect host Windows Terminal %s settings: %w", candidate.Edition, err)
		}
		if exists {
			candidate.Theme, err = readHostWindowsTerminalTheme(candidate.SettingsPath)
			if err != nil {
				return windowsTerminalConfiguration{}, fmt.Errorf("inspect host Windows Terminal %s theme: %w", candidate.Edition, err)
			}
			return candidate, nil
		}
		packageRoot := filepath.Join(localAppData, "Packages", candidate.PackageFamilyName)
		info, err := os.Stat(packageRoot)
		if err == nil {
			if !info.IsDir() {
				return windowsTerminalConfiguration{}, fmt.Errorf("host Windows Terminal %s package path is not a directory: %s", candidate.Edition, packageRoot)
			}
			candidate.SettingsPath = ""
			candidate.Theme = windowsTerminalDarkTheme
			return candidate, nil
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return windowsTerminalConfiguration{}, fmt.Errorf("inspect host Windows Terminal %s package: %w", candidate.Edition, err)
		}
	}
	unpackagedSettings := filepath.Join(localAppData, "Microsoft", "Windows Terminal", "settings.json")
	if exists, err := regularFileExists(unpackagedSettings); err != nil {
		return windowsTerminalConfiguration{}, fmt.Errorf("inspect unpackaged host Windows Terminal settings: %w", err)
	} else if exists {
		stable := candidates[1]
		stable.SettingsPath = unpackagedSettings
		stable.Theme, err = readHostWindowsTerminalTheme(stable.SettingsPath)
		if err != nil {
			return windowsTerminalConfiguration{}, fmt.Errorf("inspect unpackaged host Windows Terminal theme: %w", err)
		}
		return stable, nil
	}
	return windowsTerminalConfiguration{}, errors.New("host Windows Terminal Stable or Preview installation was not found")
}

func readHostWindowsTerminalTheme(path string) (string, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return "", fmt.Errorf("inspect settings file: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Size() > maximumConfigurationFileSize {
		return "", fmt.Errorf("settings are not a bounded regular file: %s", path)
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read settings: %w", err)
	}
	return classifyHostWindowsTerminalTheme(contents)
}

func classifyHostWindowsTerminalTheme(contents []byte) (string, error) {
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.UseNumber()
	var settings map[string]any
	if err := decoder.Decode(&settings); err != nil {
		return "", fmt.Errorf("decode settings: %w", err)
	}
	if settings == nil {
		return "", errors.New("decode settings: root is not an object")
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return "", errors.New("decode settings: trailing JSON data")
	}
	themeValue, exists := settings["theme"]
	if !exists {
		return windowsTerminalDarkTheme, nil
	}
	themeName, ok := themeValue.(string)
	if !ok || strings.TrimSpace(themeName) != themeName || themeName == "" {
		return "", errors.New("theme must resolve to one explicit light or dark application theme")
	}
	if themeName == windowsTerminalLightTheme || themeName == windowsTerminalDarkTheme {
		return themeName, nil
	}
	if themeName == "system" {
		// Windows Terminal defaults to the dynamic system theme. Starship needs
		// one deterministic preset for a guest that does not observe later host
		// theme changes, so use the established dark baseline instead of making
		// an otherwise valid host setting block startup.
		return windowsTerminalDarkTheme, nil
	}
	themesValue, exists := settings["themes"]
	if !exists {
		return "", fmt.Errorf("custom theme %q is missing from themes", themeName)
	}
	themes, ok := themesValue.([]any)
	if !ok {
		return "", errors.New("themes is not an array")
	}
	matchedTheme := ""
	for index, value := range themes {
		theme, ok := value.(map[string]any)
		if !ok {
			return "", fmt.Errorf("theme %d is not an object", index)
		}
		name, _ := theme["name"].(string)
		if name != themeName {
			continue
		}
		if matchedTheme != "" {
			return "", fmt.Errorf("custom theme %q is duplicated", themeName)
		}
		window, ok := theme["window"].(map[string]any)
		if !ok {
			return "", fmt.Errorf("custom theme %q has no window application theme", themeName)
		}
		applicationTheme, _ := window["applicationTheme"].(string)
		if applicationTheme != windowsTerminalLightTheme && applicationTheme != windowsTerminalDarkTheme {
			return "", fmt.Errorf("custom theme %q does not identify an explicit light or dark application theme", themeName)
		}
		matchedTheme = applicationTheme
	}
	if matchedTheme == "" {
		return "", fmt.Errorf("custom theme %q is missing from themes", themeName)
	}
	return matchedTheme, nil
}

func starshipPresetForWindowsTerminalTheme(theme string) (string, error) {
	switch theme {
	case windowsTerminalDarkTheme:
		return starshipPastelPowerlinePreset, nil
	case windowsTerminalLightTheme:
		return starshipCatppuccinLattePreset, nil
	default:
		return "", fmt.Errorf("unsupported host Windows Terminal theme %q", theme)
	}
}

func (configuration windowsTerminalConfiguration) validate() error {
	expectedPackageID := "Microsoft.WindowsTerminal"
	expectedPackageFamily := "Microsoft.WindowsTerminal_8wekyb3d8bbwe"
	if configuration.Edition == windowsTerminalPreviewEdition {
		expectedPackageID = "Microsoft.WindowsTerminal.Preview"
		expectedPackageFamily = "Microsoft.WindowsTerminalPreview_8wekyb3d8bbwe"
	} else if configuration.Edition != windowsTerminalStableEdition {
		return fmt.Errorf("unsupported host Windows Terminal edition %q", configuration.Edition)
	}
	if configuration.WinGetPackageID != expectedPackageID || configuration.PackageFamilyName != expectedPackageFamily {
		return fmt.Errorf("host Windows Terminal %s package identity is inconsistent", configuration.Edition)
	}
	if configuration.Theme != windowsTerminalLightTheme && configuration.Theme != windowsTerminalDarkTheme {
		return fmt.Errorf("unsupported host Windows Terminal theme %q", configuration.Theme)
	}
	for name, path := range map[string]string{"settings": configuration.SettingsPath, "fragments": configuration.FragmentsPath} {
		if path != "" && !filepath.IsAbs(path) {
			return fmt.Errorf("host Windows Terminal %s path is not absolute: %q", name, path)
		}
	}
	return nil
}

type hostConfigurationSources struct {
	GitConfig                 string
	GitConfigDirectory        string
	GitIgnore                 string
	GitAttributes             string
	GitHubCLIConfiguration    string
	GitHubCLIAuthentication   []byte
	TradingViewProfile        string
	TradingViewAuthentication []byte
	CodingAgents              codingAgentConfigurationSources
	HerdrConfig               string
	WorktreeDirectory         string
	WindowsTerminalSettings   string
	WindowsTerminalFragments  string
	WindowsTerminalEdition    string
	StarshipPreset            string
	WorkspaceManifest         string
	PackagePlan               string
}

type developmentConfigurationSyncResult struct {
	SchemaVersion                     int    `json:"schemaVersion"`
	ArchiveSHA256                     string `json:"archiveSha256"`
	CopiedFiles                       int    `json:"copiedFiles"`
	OpenCodePermissionVerified        bool   `json:"openCodePermissionVerified"`
	WindowsTerminalEdition            string `json:"windowsTerminalEdition"`
	StarshipPreset                    string `json:"starshipPreset"`
	StarshipConfigured                bool   `json:"starshipConfigured"`
	GitHubAuthenticatedAccounts       int    `json:"githubAuthenticatedAccounts"`
	GitHubAuthenticationVerified      bool   `json:"githubAuthenticationVerified"`
	HerdrConfigurationReloaded        bool   `json:"herdrConfigurationReloaded"`
	TradingViewAuthenticatedCookies   int    `json:"tradingViewAuthenticatedCookies"`
	TradingViewAuthenticationVerified bool   `json:"tradingViewAuthenticationVerified"`
}

type githubCLIAuthentication struct {
	SchemaVersion int                `json:"schemaVersion"`
	Accounts      []githubCLIAccount `json:"accounts"`
}

type githubCLIAccount struct {
	Hostname    string `json:"hostname"`
	Login       string `json:"login"`
	Active      bool   `json:"active"`
	GitProtocol string `json:"gitProtocol"`
	Token       string `json:"token"`
}

type githubCLIAuthStatus struct {
	Hosts map[string][]githubCLIAuthStatusEntry `json:"hosts"`
}

type githubCLIAuthStatusEntry struct {
	State       string `json:"state"`
	Error       string `json:"error,omitempty"`
	Active      bool   `json:"active"`
	Host        string `json:"host"`
	Login       string `json:"login"`
	TokenSource string `json:"tokenSource"`
	Scopes      string `json:"scopes,omitempty"`
	GitProtocol string `json:"gitProtocol"`
}

func configurationArchivePayloadFileCount(data []byte) (int, error) {
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return 0, fmt.Errorf("inspect development configuration archive: %w", err)
	}
	count := 0
	for _, file := range reader.File {
		if !file.FileInfo().IsDir() && file.Name != windowsTerminalEditionArchivePath && file.Name != starshipPresetArchivePath && file.Name != githubCLIAuthenticationArchivePath && file.Name != tradingViewAuthenticationArchivePath && file.Name != configurationApplyScriptArchivePath && file.Name != configurationWorkspaceManifestPath && file.Name != configurationPackagePlanArchivePath && file.Name != configurationWorktreeDirectoryArchivePath && file.Name != configurationAgentWorktreeInstructionsArchivePath && file.Name != codingAgentSyncManifestArchivePath && file.Name != tradingViewCookieSyncSourceArchivePath {
			count++
		}
	}
	return count, nil
}

func decodeDevelopmentConfigurationSyncResult(output []byte) (developmentConfigurationSyncResult, error) {
	trimmed := bytes.TrimSpace(output)
	if err := validateExactJSONObjectShape(trimmed, "guest development configuration result", []string{
		"schemaVersion", "archiveSha256", "copiedFiles", "openCodePermissionVerified",
		"windowsTerminalEdition", "starshipPreset", "starshipConfigured",
		"githubAuthenticatedAccounts", "githubAuthenticationVerified", "herdrConfigurationReloaded",
		"tradingViewAuthenticatedCookies", "tradingViewAuthenticationVerified",
	}); err != nil {
		return developmentConfigurationSyncResult{}, fmt.Errorf("decode guest development configuration result: %w", err)
	}
	var result developmentConfigurationSyncResult
	decoder := json.NewDecoder(bytes.NewReader(trimmed))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil {
		return developmentConfigurationSyncResult{}, fmt.Errorf("decode guest development configuration result: %w: %s", err, boundedText(output))
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return developmentConfigurationSyncResult{}, errors.New("decode guest development configuration result: trailing JSON data")
	}
	return result, nil
}

func exportGitHubCLIAuthentication(ctx context.Context, configurationDirectory string) ([]byte, int, error) {
	if !filepath.IsAbs(configurationDirectory) {
		return nil, 0, fmt.Errorf("GitHub CLI configuration directory is not absolute: %q", configurationDirectory)
	}
	executable, err := exec.LookPath("gh.exe")
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return emptyGitHubCLIAuthenticationPayload()
		}
		return nil, 0, fmt.Errorf("find GitHub CLI gh.exe: %w", err)
	}
	environment := githubCLICommandEnvironment(configurationDirectory)
	statusOutput, err := runBoundedGitHubCLI(ctx, executable, environment, maximumGitHubCLIStatusSize, "auth", "status", "--json", "hosts")
	var status githubCLIAuthStatus
	decoder := json.NewDecoder(bytes.NewReader(statusOutput))
	decoder.DisallowUnknownFields()
	if decodeErr := decoder.Decode(&status); decodeErr != nil {
		if err != nil && len(statusOutput) == 0 {
			return emptyGitHubCLIAuthenticationPayload()
		}
		return nil, 0, errors.New("decode host GitHub CLI authentication status")
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, 0, errors.New("decode host GitHub CLI authentication status: trailing JSON data")
	}
	hostnames := make([]string, 0, len(status.Hosts))
	for hostname := range status.Hosts {
		hostnames = append(hostnames, hostname)
	}
	sort.Strings(hostnames)
	authentication := githubCLIAuthentication{SchemaVersion: 1, Accounts: []githubCLIAccount{}}
	seenAccounts := make(map[string]bool)
	for _, hostname := range hostnames {
		activeAccounts := 0
		successfulAccounts := 0
		entries := status.Hosts[hostname]
		sort.Slice(entries, func(left, right int) bool { return entries[left].Login < entries[right].Login })
		for _, entry := range entries {
			if entry.State != "success" {
				continue
			}
			successfulAccounts++
			if entry.Active {
				activeAccounts++
			}
			account := githubCLIAccount{
				Hostname:    entry.Host,
				Login:       entry.Login,
				Active:      entry.Active,
				GitProtocol: entry.GitProtocol,
			}
			if hostname != entry.Host {
				return nil, 0, errors.New("host GitHub CLI authentication status has inconsistent host identity")
			}
			if err := validateGitHubCLIAccount(account, false); err != nil {
				return nil, 0, fmt.Errorf("validate host GitHub CLI account metadata: %w", err)
			}
			tokenOutput, err := runBoundedGitHubCLI(ctx, executable, environment, maximumGitHubCLITokenSize, "auth", "token", "--hostname", account.Hostname, "--user", account.Login)
			if err != nil {
				return nil, 0, fmt.Errorf("export one host GitHub CLI credential: %w", err)
			}
			account.Token = strings.TrimSpace(string(tokenOutput))
			if err := validateGitHubCLIAccount(account, true); err != nil {
				return nil, 0, fmt.Errorf("validate one exported host GitHub CLI credential: %w", err)
			}
			tokenEnvironment := append([]string(nil), environment...)
			tokenEnvironment = append(tokenEnvironment, "GH_TOKEN="+account.Token, "GH_ENTERPRISE_TOKEN="+account.Token)
			loginOutput, err := runBoundedGitHubCLI(ctx, executable, tokenEnvironment, maximumGitHubCLILoginSize,
				"api", "--hostname", account.Hostname, "/user", "--jq", ".login")
			for index := range tokenEnvironment {
				tokenEnvironment[index] = ""
			}
			if err != nil {
				return nil, 0, fmt.Errorf("resolve canonical host GitHub CLI account login: %w", err)
			}
			account, err = withCanonicalGitHubCLIAccountLogin(account, loginOutput)
			if err != nil {
				return nil, 0, err
			}
			identity := strings.ToLower(account.Hostname) + "\x00" + strings.ToLower(account.Login)
			if seenAccounts[identity] {
				return nil, 0, errors.New("duplicate canonical host GitHub CLI account metadata")
			}
			seenAccounts[identity] = true
			authentication.Accounts = append(authentication.Accounts, account)
			if len(authentication.Accounts) > maximumGitHubCLIAccounts {
				return nil, 0, fmt.Errorf("host GitHub CLI account count exceeds %d", maximumGitHubCLIAccounts)
			}
		}
		if successfulAccounts > 0 && activeAccounts != 1 {
			return nil, 0, fmt.Errorf("one host GitHub CLI host has %d active successful accounts", activeAccounts)
		}
	}
	sort.Slice(authentication.Accounts, func(left, right int) bool {
		if authentication.Accounts[left].Hostname != authentication.Accounts[right].Hostname {
			return authentication.Accounts[left].Hostname < authentication.Accounts[right].Hostname
		}
		return authentication.Accounts[left].Login < authentication.Accounts[right].Login
	})
	payload, err := json.Marshal(authentication)
	if err != nil {
		return nil, 0, fmt.Errorf("encode host GitHub CLI authentication: %w", err)
	}
	return payload, len(authentication.Accounts), nil
}

func emptyGitHubCLIAuthenticationPayload() ([]byte, int, error) {
	payload, err := json.Marshal(githubCLIAuthentication{SchemaVersion: 1, Accounts: []githubCLIAccount{}})
	if err != nil {
		return nil, 0, fmt.Errorf("encode empty host GitHub CLI authentication: %w", err)
	}
	return payload, 0, nil
}

func withCanonicalGitHubCLIAccountLogin(account githubCLIAccount, output []byte) (githubCLIAccount, error) {
	account.Login = strings.TrimSpace(string(output))
	if err := validateGitHubCLIAccount(account, true); err != nil {
		return githubCLIAccount{}, fmt.Errorf("validate canonical host GitHub CLI account: %w", err)
	}
	return account, nil
}

func githubCLICommandEnvironment(configurationDirectory string) []string {
	removed := map[string]bool{
		"GH_CONFIG_DIR":             true,
		"GH_TOKEN":                  true,
		"GITHUB_TOKEN":              true,
		"GH_ENTERPRISE_TOKEN":       true,
		"GITHUB_ENTERPRISE_TOKEN":   true,
		"GH_PROMPT_DISABLED":        true,
		"NO_COLOR":                  true,
		tailscaleAuthKeyEnvironment: true,
	}
	environment := make([]string, 0, len(os.Environ())+3)
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		if !removed[strings.ToUpper(name)] {
			environment = append(environment, entry)
		}
	}
	return append(environment,
		"GH_CONFIG_DIR="+configurationDirectory,
		"GH_PROMPT_DISABLED=1",
		"NO_COLOR=1",
	)
}

func runBoundedGitHubCLI(ctx context.Context, executable string, environment []string, maximumBytes int64, arguments ...string) ([]byte, error) {
	command := hiddenCommandContext(ctx, executable, arguments...)
	command.Env = environment
	command.Stderr = io.Discard
	stdout, err := command.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("capture GitHub CLI output: %w", err)
	}
	if err := command.Start(); err != nil {
		return nil, fmt.Errorf("start GitHub CLI: %w", err)
	}
	output, readErr := io.ReadAll(io.LimitReader(stdout, maximumBytes+1))
	if int64(len(output)) > maximumBytes {
		terminateErr := command.Terminate()
		waitErr := command.Wait()
		errs := []error{fmt.Errorf("GitHub CLI output exceeds %d bytes", maximumBytes)}
		if terminateErr != nil {
			errs = append(errs, fmt.Errorf("terminate GitHub CLI Job Object: %w", terminateErr))
		}
		if waitErr != nil {
			errs = append(errs, fmt.Errorf("wait for terminated GitHub CLI: %w", waitErr))
		}
		return nil, errors.Join(errs...)
	}
	waitErr := command.Wait()
	if readErr != nil {
		return nil, fmt.Errorf("read GitHub CLI output: %w", readErr)
	}
	if waitErr != nil {
		return bytes.TrimSpace(output), fmt.Errorf("GitHub CLI command failed: %w", waitErr)
	}
	return bytes.TrimSpace(output), nil
}

func decodeGitHubCLIAuthentication(payload []byte) (githubCLIAuthentication, error) {
	var authentication githubCLIAuthentication
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&authentication); err != nil {
		return githubCLIAuthentication{}, errors.New("decode GitHub CLI authentication payload")
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return githubCLIAuthentication{}, errors.New("decode GitHub CLI authentication payload: trailing JSON data")
	}
	if authentication.SchemaVersion != 1 {
		return githubCLIAuthentication{}, fmt.Errorf("unsupported GitHub CLI authentication schema %d", authentication.SchemaVersion)
	}
	if len(authentication.Accounts) > maximumGitHubCLIAccounts {
		return githubCLIAuthentication{}, fmt.Errorf("GitHub CLI account count exceeds %d", maximumGitHubCLIAccounts)
	}
	seen := make(map[string]bool, len(authentication.Accounts))
	activeByHost := make(map[string]int)
	accountsByHost := make(map[string]int)
	for _, account := range authentication.Accounts {
		if err := validateGitHubCLIAccount(account, true); err != nil {
			return githubCLIAuthentication{}, err
		}
		identity := strings.ToLower(account.Hostname) + "\x00" + strings.ToLower(account.Login)
		if seen[identity] {
			return githubCLIAuthentication{}, errors.New("duplicate GitHub CLI account metadata")
		}
		seen[identity] = true
		accountsByHost[strings.ToLower(account.Hostname)]++
		if account.Active {
			activeByHost[strings.ToLower(account.Hostname)]++
		}
	}
	for hostname := range accountsByHost {
		if activeByHost[hostname] != 1 {
			return githubCLIAuthentication{}, fmt.Errorf("one GitHub CLI host has %d active accounts", activeByHost[hostname])
		}
	}
	return authentication, nil
}

func validateGitHubCLIAccount(account githubCLIAccount, requireToken bool) error {
	for field, value := range map[string]string{"hostname": account.Hostname, "login": account.Login} {
		if strings.TrimSpace(value) != value || value == "" || len(value) > 256 || strings.ContainsAny(value, "\x00\r\n") {
			return fmt.Errorf("GitHub CLI account %s is invalid", field)
		}
	}
	if account.GitProtocol != "https" && account.GitProtocol != "ssh" {
		return errors.New("GitHub CLI account Git protocol is invalid")
	}
	if requireToken && (account.Token == "" || len(account.Token) > maximumGitHubCLITokenSize || strings.ContainsAny(account.Token, "\x00\r\n")) {
		return errors.New("GitHub CLI account token is invalid")
	}
	return nil
}

func syncDevelopmentConfiguration(ctx context.Context, connection Connection, terminal windowsTerminalConfiguration, packages wingetPackagePlan, codingAgents codingAgentSyncConfiguration, tradingViewEnabled, worktreesEnabled bool, provisioningInput string) error {
	if err := terminal.validate(); err != nil {
		return err
	}
	if err := packages.validate(terminal); err != nil {
		return err
	}
	sources, err := defaultHostConfigurationSources(terminal, packages, codingAgents, tradingViewEnabled)
	if err != nil {
		return err
	}
	if !filepath.IsAbs(provisioningInput) {
		return fmt.Errorf("configuration sync provisioning input is not absolute: %q", provisioningInput)
	}
	provisioningInput = filepath.Clean(provisioningInput)
	sources.PackagePlan = filepath.Join(provisioningInput, wingetPackagePlanFileName)
	if packages.enabled(packageGit) || sources.WindowsTerminalSettings != "" {
		sources.WorkspaceManifest = filepath.Join(provisioningInput, workspaceManifestName)
	}
	if worktreesEnabled {
		sources.WorktreeDirectory = guestWorktreeDirectory
	}
	expectedGitHubAccounts := 0
	expectedTradingViewCookies := 0
	if packages.enabled(packageGitHubCLI) {
		authenticationPayload, accountCount, err := exportGitHubCLIAuthentication(ctx, sources.GitHubCLIConfiguration)
		if err != nil {
			return err
		}
		sources.GitHubCLIAuthentication = authenticationPayload
		defer clear(sources.GitHubCLIAuthentication)
		expectedGitHubAccounts = accountCount
	}
	if tradingViewEnabled {
		authenticationPayload, cookieCount, err := exportTradingViewAuthentication(ctx, sources.TradingViewProfile)
		if err != nil {
			return err
		}
		sources.TradingViewAuthentication = authenticationPayload
		defer clear(sources.TradingViewAuthentication)
		expectedTradingViewCookies = cookieCount
	}
	archive, err := buildDevelopmentConfigurationArchive(ctx, sources, configurationSyncScript)
	if err != nil {
		return err
	}
	defer clear(archive)
	expectedDigest := fmt.Sprintf("%x", sha256.Sum256(archive))
	expectedCopiedFiles, err := configurationArchivePayloadFileCount(archive)
	if err != nil {
		return err
	}
	launcherScript := buildDevelopmentConfigurationLauncher(expectedDigest, len(archive))
	output, err := runSSHArchivePowerShell(ctx, connection, archive, launcherScript, "transfer development configuration")
	if err != nil {
		return err
	}
	result, err := decodeDevelopmentConfigurationSyncResult(output)
	if err != nil {
		return err
	}
	if result.SchemaVersion != 7 {
		return fmt.Errorf("verify guest development configuration: unsupported result schema %d", result.SchemaVersion)
	}
	if result.ArchiveSHA256 != expectedDigest {
		return fmt.Errorf("verify guest development configuration: expected SHA-256 %s but got %q", expectedDigest, result.ArchiveSHA256)
	}
	if result.CopiedFiles != expectedCopiedFiles {
		return fmt.Errorf("verify guest development configuration: copied %d files, expected %d", result.CopiedFiles, expectedCopiedFiles)
	}
	expectedTerminalEdition := ""
	if packages.enabled(terminal.WinGetPackageID) {
		expectedTerminalEdition = terminal.Edition
	}
	if result.WindowsTerminalEdition != expectedTerminalEdition {
		return fmt.Errorf("verify guest Windows Terminal edition: expected %q but got %q", expectedTerminalEdition, result.WindowsTerminalEdition)
	}
	if result.StarshipPreset != sources.StarshipPreset || result.StarshipConfigured != packages.enabled(packageStarship) {
		return fmt.Errorf("verify guest Starship configuration: preset %q, expected %q", result.StarshipPreset, sources.StarshipPreset)
	}
	if packages.enabled(packageOpenCode) && !result.OpenCodePermissionVerified {
		return errors.New("verify guest OpenCode permissions: selected OpenCode was not verified")
	}
	if result.GitHubAuthenticatedAccounts != expectedGitHubAccounts || result.GitHubAuthenticationVerified != packages.enabled(packageGitHubCLI) {
		return fmt.Errorf("verify guest GitHub CLI authentication: authenticated %d accounts, expected %d", result.GitHubAuthenticatedAccounts, expectedGitHubAccounts)
	}
	if !result.HerdrConfigurationReloaded {
		return errors.New("verify guest Herdr configuration: server did not report a successful reload")
	}
	if result.TradingViewAuthenticatedCookies != expectedTradingViewCookies || result.TradingViewAuthenticationVerified != tradingViewEnabled {
		return fmt.Errorf("verify guest TradingView authentication: imported %d cookies, expected %d", result.TradingViewAuthenticatedCookies, expectedTradingViewCookies)
	}
	return nil
}

func buildDevelopmentConfigurationLauncher(expectedDigest string, expectedArchiveLength int) string {
	staging := guestArchiveStagingPowerShell("configuration-"+expectedDigest[:16], "Development configuration")
	return fmt.Sprintf(`$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'
%s
$expectedArchiveLength = [long]%d
try {
    [Console]::Error.WriteLine('[config-sync] receive-archive')
    $inputStream = [Console]::OpenStandardInput()
    $outputStream = [IO.File]::Open($archive, [IO.FileMode]::CreateNew, [IO.FileAccess]::Write, [IO.FileShare]::None)
    try {
        $remaining = $expectedArchiveLength
        $buffer = New-Object byte[] 65536
        while ($remaining -gt 0) {
            $requested = [int][Math]::Min([long]$buffer.Length, $remaining)
            $read = $inputStream.Read($buffer, 0, $requested)
            if ($read -le 0) { throw "Development configuration archive ended with $remaining bytes missing." }
            $outputStream.Write($buffer, 0, $read)
            $remaining -= $read
        }
        $outputStream.Flush($true)
    } finally {
        $outputStream.Dispose()
    }
    [Console]::Error.WriteLine('[config-sync] verify-archive')
    $digest = (Get-FileHash -LiteralPath $archive -Algorithm SHA256).Hash.ToLowerInvariant()
    if ($digest -cne '%s') { throw 'Development configuration archive SHA-256 mismatch.' }
    Expand-Archive -LiteralPath $archive -DestinationPath $expanded
    Assert-GuestArchiveTree
    $applyScript = Join-Path $expanded 'herdr-sandbox\apply.ps1'
    if (-not (Test-Path -LiteralPath $applyScript -PathType Leaf)) {
        throw 'Development configuration apply script is missing.'
    }
    [Console]::Error.WriteLine('[config-sync] invoke-apply-script')
    $env:HERDR_SANDBOX_CONFIGURATION_ARCHIVE = $archive
    $env:HERDR_SANDBOX_CONFIGURATION_EXPANDED = $expanded
    & $applyScript
} finally {
    Remove-Item Env:HERDR_SANDBOX_CONFIGURATION_ARCHIVE -ErrorAction SilentlyContinue
    Remove-Item Env:HERDR_SANDBOX_CONFIGURATION_EXPANDED -ErrorAction SilentlyContinue
    Remove-GuestArchiveStaging
}
exit 0`, staging, expectedArchiveLength, expectedDigest)
}

func defaultHostConfigurationSources(terminal windowsTerminalConfiguration, packages wingetPackagePlan, codingAgents codingAgentSyncConfiguration, tradingViewEnabled bool) (hostConfigurationSources, error) {
	if err := packages.validate(terminal); err != nil {
		return hostConfigurationSources{}, err
	}
	userHome, err := os.UserHomeDir()
	if err != nil {
		return hostConfigurationSources{}, fmt.Errorf("resolve user home for development configuration: %w", err)
	}
	roamingAppData := strings.TrimSpace(os.Getenv("APPDATA"))
	if !filepath.IsAbs(roamingAppData) {
		return hostConfigurationSources{}, fmt.Errorf("APPDATA is not absolute: %q", roamingAppData)
	}
	sources := hostConfigurationSources{HerdrConfig: filepath.Join(roamingAppData, "herdr", "config.toml")}
	if tradingViewEnabled {
		sources.TradingViewProfile, err = defaultTradingViewProfilePath()
		if err != nil {
			return hostConfigurationSources{}, err
		}
	}
	sources.CodingAgents, err = defaultCodingAgentConfigurationSources(userHome, codingAgents)
	if err != nil {
		return hostConfigurationSources{}, err
	}
	if packages.enabled(packageGit) {
		sources.GitConfig = filepath.Join(userHome, ".gitconfig")
		sources.GitConfigDirectory = filepath.Join(userHome, ".config", "git")
		sources.GitIgnore = filepath.Join(userHome, ".gitignore_global")
		sources.GitAttributes = filepath.Join(userHome, ".gitattributes")
	}
	if packages.enabled(packageGitHubCLI) {
		sources.GitHubCLIConfiguration, err = defaultGitHubCLIConfigurationDirectory(userHome, roamingAppData)
		if err != nil {
			return hostConfigurationSources{}, err
		}
	}
	if packages.enabled(packageStarship) {
		sources.StarshipPreset, err = starshipPresetForWindowsTerminalTheme(terminal.Theme)
		if err != nil {
			return hostConfigurationSources{}, err
		}
	}
	if packages.enabled(terminal.WinGetPackageID) {
		sources.WindowsTerminalSettings = terminal.SettingsPath
		sources.WindowsTerminalFragments = terminal.FragmentsPath
		sources.WindowsTerminalEdition = terminal.Edition
	}
	return sources, nil
}

func defaultGitHubCLIConfigurationDirectory(userHome, roamingAppData string) (string, error) {
	if !filepath.IsAbs(userHome) {
		return "", fmt.Errorf("user home is not absolute: %q", userHome)
	}
	if roamingAppData != "" && !filepath.IsAbs(roamingAppData) {
		return "", fmt.Errorf("APPDATA is not absolute: %q", roamingAppData)
	}
	if configured := strings.TrimSpace(os.Getenv("GH_CONFIG_DIR")); configured != "" {
		if !filepath.IsAbs(configured) {
			return "", fmt.Errorf("GH_CONFIG_DIR is not absolute: %q", configured)
		}
		return filepath.Clean(configured), nil
	}
	if configurationRoot := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME")); configurationRoot != "" {
		if !filepath.IsAbs(configurationRoot) {
			return "", fmt.Errorf("XDG_CONFIG_HOME is not absolute: %q", configurationRoot)
		}
		return filepath.Join(configurationRoot, "gh"), nil
	}
	if roamingAppData != "" {
		return filepath.Join(roamingAppData, "GitHub CLI"), nil
	}
	return filepath.Join(userHome, ".config", "gh"), nil
}

func defaultOpenCodeDirectories(userHome string) (string, string, error) {
	if !filepath.IsAbs(userHome) {
		return "", "", fmt.Errorf("user home is not absolute: %q", userHome)
	}
	configurationRoot := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME"))
	if configurationRoot == "" {
		configurationRoot = filepath.Join(userHome, ".config")
	} else if !filepath.IsAbs(configurationRoot) {
		return "", "", fmt.Errorf("XDG_CONFIG_HOME is not absolute: %q", configurationRoot)
	}
	dataRoot := strings.TrimSpace(os.Getenv("XDG_DATA_HOME"))
	if dataRoot == "" {
		dataRoot = filepath.Join(userHome, ".local", "share")
	} else if !filepath.IsAbs(dataRoot) {
		return "", "", fmt.Errorf("XDG_DATA_HOME is not absolute: %q", dataRoot)
	}
	return filepath.Join(configurationRoot, "opencode"), filepath.Join(dataRoot, "opencode"), nil
}

func buildDevelopmentConfigurationArchive(ctx context.Context, sources hostConfigurationSources, applyScript []byte) ([]byte, error) {
	var buffer bytes.Buffer
	archive := zip.NewWriter(&buffer)
	total := int64(0)
	files := 0
	addData := func(contents []byte, destination, description string) error {
		if files >= maximumConfigurationFiles {
			return fmt.Errorf("configuration archive exceeds file limit %d", maximumConfigurationFiles)
		}
		if len(contents) > maximumConfigurationFileSize || total+int64(len(contents)) > maximumConfigurationSize {
			return fmt.Errorf("configuration source exceeds size limit: %s", description)
		}
		writer, err := archive.Create(strings.ReplaceAll(destination, `\`, "/"))
		if err != nil {
			return err
		}
		if _, err := writer.Write(contents); err != nil {
			return err
		}
		total += int64(len(contents))
		files++
		return nil
	}
	add := func(source, destination string) error {
		info, err := os.Lstat(source)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return fmt.Errorf("configuration source is not a regular file: %s", source)
		}
		if info.Size() > maximumConfigurationFileSize || total+info.Size() > maximumConfigurationSize {
			return fmt.Errorf("configuration source exceeds size limit: %s", source)
		}
		contents, err := os.ReadFile(source)
		if err != nil {
			return err
		}
		return addData(contents, destination, source)
	}
	if len(applyScript) == 0 {
		return nil, errors.New("development configuration apply script is empty")
	}
	if err := addData(applyScript, configurationApplyScriptArchivePath, "development configuration apply script"); err != nil {
		return nil, fmt.Errorf("archive development configuration apply script: %w", err)
	}
	if err := add(sources.PackagePlan, configurationPackagePlanArchivePath); err != nil {
		return nil, fmt.Errorf("archive resolved WinGet package plan: %w", err)
	}
	activeWorkspace := ""
	if sources.WorkspaceManifest != "" {
		workspaceManifest, found, err := readBoundedRegularFile(sources.WorkspaceManifest, maximumWorkspaceManifestBytes)
		if err != nil {
			return nil, fmt.Errorf("read guest workspace manifest for configuration archive: %w", err)
		}
		if !found {
			return nil, errors.New("guest workspace manifest for configuration archive is missing")
		}
		manifest, err := decodeGuestWorkspaceManifest(workspaceManifest)
		if err != nil {
			return nil, err
		}
		activeWorkspace = manifest.ActiveWorkspace
		if err := addData(workspaceManifest, configurationWorkspaceManifestPath, sources.WorkspaceManifest); err != nil {
			return nil, fmt.Errorf("archive guest workspace manifest: %w", err)
		}
	}
	if sources.WindowsTerminalEdition != "" {
		if sources.WindowsTerminalEdition != windowsTerminalStableEdition && sources.WindowsTerminalEdition != windowsTerminalPreviewEdition {
			return nil, fmt.Errorf("archive Windows Terminal edition: unsupported value %q", sources.WindowsTerminalEdition)
		}
		if err := addData([]byte(sources.WindowsTerminalEdition+"\n"), windowsTerminalEditionArchivePath, "Windows Terminal edition"); err != nil {
			return nil, fmt.Errorf("archive Windows Terminal edition: %w", err)
		}
	}
	if sources.StarshipPreset != "" {
		if sources.StarshipPreset != starshipPastelPowerlinePreset && sources.StarshipPreset != starshipCatppuccinLattePreset {
			return nil, fmt.Errorf("archive Starship preset: unsupported value %q", sources.StarshipPreset)
		}
		if err := addData([]byte(sources.StarshipPreset+"\n"), starshipPresetArchivePath, "Starship preset marker"); err != nil {
			return nil, fmt.Errorf("archive Starship preset: %w", err)
		}
	}
	if sources.WorktreeDirectory != "" {
		if !strings.EqualFold(filepath.Clean(sources.WorktreeDirectory), guestWorktreeDirectory) {
			return nil, fmt.Errorf("archive worktree directory: unsupported value %q", sources.WorktreeDirectory)
		}
		if err := addData([]byte(guestWorktreeDirectory+"\n"), configurationWorktreeDirectoryArchivePath, "guest worktree directory"); err != nil {
			return nil, fmt.Errorf("archive worktree directory: %w", err)
		}
		if err := validateAgentWorktreeInstructions(agentWorktreeInstructions); err != nil {
			return nil, err
		}
		if err := addData(agentWorktreeInstructions, configurationAgentWorktreeInstructionsArchivePath, "guest agent worktree instructions"); err != nil {
			return nil, fmt.Errorf("archive guest agent worktree instructions: %w", err)
		}
	}

	if sources.GitConfig != "" {
		if err := add(sources.GitConfig, filepath.Join("git", ".gitconfig")); err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				return nil, fmt.Errorf("archive global Git config: %w", err)
			}
		}
	}
	if sources.GitConfigDirectory != "" {
		if info, err := os.Stat(sources.GitConfigDirectory); err == nil && info.IsDir() {
			if err := addConfigurationTree(sources.GitConfigDirectory, filepath.Join("git", "config"), nil, add); err != nil {
				return nil, err
			}
		} else if err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("inspect Git config directory: %w", err)
		}
	}
	for _, optional := range []struct {
		source      string
		destination string
	}{
		{source: sources.GitIgnore, destination: filepath.Join("git", ".gitignore_global")},
		{source: sources.GitAttributes, destination: filepath.Join("git", ".gitattributes")},
	} {
		if optional.source == "" {
			continue
		}
		if info, err := os.Stat(optional.source); err == nil && info.Mode().IsRegular() {
			if err := add(optional.source, optional.destination); err != nil {
				return nil, err
			}
		} else if err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("inspect optional Git config file: %w", err)
		}
	}

	if sources.GitHubCLIConfiguration != "" {
		for _, name := range []string{"config.yml", "hosts.yml"} {
			source := filepath.Join(sources.GitHubCLIConfiguration, name)
			if err := add(source, filepath.Join("github-cli", name)); err != nil {
				if errors.Is(err, os.ErrNotExist) {
					continue
				}
				return nil, fmt.Errorf("archive GitHub CLI %s: %w", name, err)
			}
		}
		if _, err := decodeGitHubCLIAuthentication(sources.GitHubCLIAuthentication); err != nil {
			return nil, fmt.Errorf("validate GitHub CLI authentication payload: %w", err)
		}
		if err := addData(sources.GitHubCLIAuthentication, githubCLIAuthenticationArchivePath, "GitHub CLI authentication"); err != nil {
			return nil, fmt.Errorf("archive GitHub CLI authentication: %w", err)
		}
	}
	if len(sources.TradingViewAuthentication) != 0 {
		if _, err := decodeTradingViewAuthentication(sources.TradingViewAuthentication); err != nil {
			return nil, fmt.Errorf("validate TradingView authentication payload: %w", err)
		}
		if len(tradingViewCookieSyncSource) == 0 {
			return nil, errors.New("TradingView cookie sync source is empty")
		}
		if err := addData(sources.TradingViewAuthentication, tradingViewAuthenticationArchivePath, "TradingView authentication"); err != nil {
			return nil, fmt.Errorf("archive TradingView authentication: %w", err)
		}
		if err := addData(tradingViewCookieSyncSource, tradingViewCookieSyncSourceArchivePath, "TradingView cookie sync source"); err != nil {
			return nil, fmt.Errorf("archive TradingView cookie sync source: %w", err)
		}
	}

	if err := archiveCodingAgentConfiguration(ctx, sources.CodingAgents, add, addData); err != nil {
		return nil, err
	}
	herdrConfig, err := buildGuestHerdrConfigWithWorktreeDirectory(sources.HerdrConfig, sources.WorktreeDirectory)
	if err != nil {
		return nil, err
	}
	if err := addData(herdrConfig, filepath.Join("herdr", "config.toml"), sources.HerdrConfig); err != nil {
		return nil, fmt.Errorf("archive Herdr config: %w", err)
	}
	if sources.WindowsTerminalEdition != "" && sources.WindowsTerminalSettings != "" {
		if activeWorkspace == "" {
			return nil, errors.New("archive Windows Terminal settings: active guest workspace is missing")
		}
		settings, err := buildGuestWindowsTerminalSettings(sources.WindowsTerminalSettings, activeWorkspace)
		if err != nil {
			return nil, err
		}
		if err := addData(settings, filepath.Join("windows-terminal", "settings.json"), sources.WindowsTerminalSettings); err != nil {
			return nil, fmt.Errorf("archive Windows Terminal settings: %w", err)
		}
	}
	if sources.WindowsTerminalEdition != "" && sources.WindowsTerminalFragments != "" {
		if info, err := os.Stat(sources.WindowsTerminalFragments); err == nil && info.IsDir() {
			if err := addConfigurationTree(sources.WindowsTerminalFragments, filepath.Join("windows-terminal", "Fragments"), nil, add); err != nil {
				return nil, err
			}
		} else if err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("inspect Windows Terminal fragments: %w", err)
		}
	}
	if err := archive.Close(); err != nil {
		return nil, fmt.Errorf("finish development configuration archive: %w", err)
	}
	return buffer.Bytes(), nil
}

func buildGuestHerdrConfig(path string) ([]byte, error) {
	return buildGuestHerdrConfigWithWorktreeDirectory(path, "")
}

func buildGuestHerdrConfigWithWorktreeDirectory(path, worktreeDirectory string) ([]byte, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return patchGuestHerdrConfigWithWorktreeDirectory(nil, worktreeDirectory)
	}
	if err != nil {
		return nil, fmt.Errorf("inspect Herdr config: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Size() > maximumConfigurationFileSize {
		return nil, fmt.Errorf("Herdr config is not a bounded regular file: %s", path)
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read Herdr config: %w", err)
	}
	return patchGuestHerdrConfigWithWorktreeDirectory(contents, worktreeDirectory)
}

func patchGuestHerdrConfig(contents []byte) ([]byte, error) {
	return patchGuestHerdrConfigWithWorktreeDirectory(contents, "")
}

func patchGuestHerdrConfigWithWorktreeDirectory(contents []byte, worktreeDirectory string) ([]byte, error) {
	if bytes.IndexByte(contents, 0) >= 0 {
		return nil, errors.New("Herdr config contains a NUL byte")
	}
	text := strings.ReplaceAll(string(contents), "\r\n", "\n")
	lines := strings.Split(text, "\n")
	if worktreeDirectory != "" {
		if err := rejectAmbiguousHerdrWorktreeDefinitions(lines); err != nil {
			return nil, err
		}
	}
	var err error
	lines, err = upsertHerdrConfigValue(lines, "terminal", "default_shell", `default_shell = "pwsh.exe"`)
	if err != nil {
		return nil, err
	}
	if worktreeDirectory != "" {
		if !strings.EqualFold(filepath.Clean(worktreeDirectory), guestWorktreeDirectory) {
			return nil, fmt.Errorf("guest Herdr worktree directory = %q, want %q", worktreeDirectory, guestWorktreeDirectory)
		}
		lines, err = upsertHerdrConfigValue(lines, "worktrees", "directory", `directory = "C:/Worktrees"`)
		if err != nil {
			return nil, err
		}
	}
	return []byte(strings.TrimRight(strings.Join(lines, "\n"), "\n") + "\n"), nil
}

func rejectAmbiguousHerdrWorktreeDefinitions(lines []string) error {
	inWorktreeSection := false
	atRoot := true
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if strings.HasPrefix(trimmed, "[") {
			header, exact := herdrConfigHeader(trimmed)
			if !exact && herdrConfigHeaderTargetsWorktrees(header) {
				return errors.New("Herdr config has an ambiguous worktrees definition")
			}
			if !exact {
				inWorktreeSection = false
				atRoot = false
				continue
			}
			atRoot = false
			inWorktreeSection = herdrConfigSectionName(header) == "worktrees"
			if !inWorktreeSection && herdrConfigHeaderTargetsWorktrees(header) {
				return errors.New("Herdr config has an ambiguous worktrees definition")
			}
			continue
		}
		if inWorktreeSection {
			continue
		}
		if atRoot && herdrConfigAssignmentTargetsWorktrees(trimmed) {
			return errors.New("Herdr config has an ambiguous worktrees definition")
		}
	}
	return nil
}

func herdrConfigHeader(line string) (string, bool) {
	closing := strings.Index(line, "]")
	if closing < 0 {
		return line, false
	}
	if strings.HasPrefix(line, "[[") {
		second := strings.Index(line[closing+1:], "]")
		if second < 0 {
			return line, false
		}
		closing += second + 1
	}
	header := line[:closing+1]
	remainder := strings.TrimSpace(line[closing+1:])
	return header, remainder == "" || strings.HasPrefix(remainder, "#")
}

func herdrConfigSectionName(header string) string {
	if strings.HasPrefix(header, "[[") || !strings.HasPrefix(header, "[") || !strings.HasSuffix(header, "]") {
		return ""
	}
	return strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(header, "["), "]"))
}

func herdrConfigHeaderTargetsWorktrees(header string) bool {
	header = strings.TrimSpace(header)
	header = strings.TrimLeft(header, "[")
	header = strings.TrimSpace(strings.TrimRight(header, "]"))
	for _, key := range []string{"worktrees", `"worktrees"`, "'worktrees'"} {
		if !strings.HasPrefix(header, key) {
			continue
		}
		remainder := strings.TrimSpace(header[len(key):])
		return remainder == "" || strings.HasPrefix(remainder, ".")
	}
	return false
}

func herdrConfigAssignmentTargetsWorktrees(line string) bool {
	for _, key := range []string{"worktrees", `"worktrees"`, "'worktrees'"} {
		if !strings.HasPrefix(line, key) {
			continue
		}
		remainder := strings.TrimSpace(line[len(key):])
		return strings.HasPrefix(remainder, "=") || strings.HasPrefix(remainder, ".")
	}
	return false
}

func upsertHerdrConfigValue(lines []string, sectionName, key, replacement string) ([]string, error) {
	sectionStart := -1
	sectionEnd := len(lines)
	keyLine := -1
	inSection := false
	for index, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") {
			header, exact := herdrConfigHeader(trimmed)
			if !exact {
				continue
			}
			section := herdrConfigSectionName(header)
			if inSection {
				sectionEnd = index
				inSection = false
			}
			if section == sectionName {
				if sectionStart >= 0 {
					return nil, fmt.Errorf("Herdr config contains duplicate [%s] sections", sectionName)
				}
				sectionStart = index
				sectionEnd = len(lines)
				inSection = true
			}
			continue
		}
		if inSection {
			withoutLeadingSpace := strings.TrimLeft(line, " \t")
			if strings.HasPrefix(withoutLeadingSpace, key) {
				separator := strings.Index(withoutLeadingSpace, "=")
				if separator < 0 || strings.TrimSpace(withoutLeadingSpace[:separator]) != key || keyLine >= 0 {
					return nil, fmt.Errorf("Herdr config has an ambiguous %s.%s", sectionName, key)
				}
				keyLine = index
			}
		}
	}
	if sectionStart < 0 {
		if len(lines) == 1 && lines[0] == "" {
			lines = nil
		} else if len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) != "" {
			lines = append(lines, "")
		}
		lines = append(lines, "["+sectionName+"]", replacement)
	} else if keyLine >= 0 {
		indent := lines[keyLine][:len(lines[keyLine])-len(strings.TrimLeft(lines[keyLine], " \t"))]
		lines[keyLine] = indent + replacement
	} else {
		lines = append(lines[:sectionEnd], append([]string{replacement}, lines[sectionEnd:]...)...)
	}
	return lines, nil
}

func buildGuestWindowsTerminalSettings(path, startingDirectory string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("inspect Windows Terminal settings: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Size() > maximumConfigurationFileSize {
		return nil, fmt.Errorf("Windows Terminal settings are not a bounded regular file: %s", path)
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read Windows Terminal settings: %w", err)
	}
	return patchGuestWindowsTerminalSettings(contents, startingDirectory)
}

func patchGuestWindowsTerminalSettings(contents []byte, startingDirectory string) ([]byte, error) {
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.UseNumber()
	var settings map[string]any
	if err := decoder.Decode(&settings); err != nil {
		return nil, fmt.Errorf("decode Windows Terminal settings: %w", err)
	}
	if settings == nil {
		return nil, errors.New("decode Windows Terminal settings: root is not an object")
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, errors.New("decode Windows Terminal settings: trailing JSON data")
	}
	settings["defaultProfile"] = powerShellProfileGUID

	profiles, err := terminalSettingsObject(settings, "profiles")
	if err != nil {
		return nil, err
	}
	defaults, err := terminalSettingsObject(profiles, "defaults")
	if err != nil {
		return nil, err
	}
	if err := patchGuestTerminalFont(defaults); err != nil {
		return nil, err
	}
	defaults["startingDirectory"] = startingDirectory

	listValue, exists := profiles["list"]
	if !exists {
		listValue = []any{}
	}
	list, ok := listValue.([]any)
	if !ok {
		return nil, errors.New("Windows Terminal profiles.list is not an array")
	}
	powerShellFound := false
	for index, value := range list {
		profile, ok := value.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("Windows Terminal profile %d is not an object", index)
		}
		if err := patchGuestTerminalFont(profile); err != nil {
			return nil, fmt.Errorf("Windows Terminal profile %d: %w", index, err)
		}
		profile["startingDirectory"] = startingDirectory
		guid, _ := profile["guid"].(string)
		if strings.EqualFold(guid, powerShellProfileGUID) {
			configureGuestPowerShellProfile(profile)
			powerShellFound = true
		}
	}
	if !powerShellFound {
		profile := map[string]any{}
		if err := patchGuestTerminalFont(profile); err != nil {
			return nil, fmt.Errorf("configure PowerShell Terminal profile: %w", err)
		}
		configureGuestPowerShellProfile(profile)
		profile["startingDirectory"] = startingDirectory
		list = append(list, profile)
	}
	profiles["list"] = list

	patched, err := json.MarshalIndent(settings, "", "    ")
	if err != nil {
		return nil, fmt.Errorf("encode guest Windows Terminal settings: %w", err)
	}
	return append(patched, '\n'), nil
}

func terminalSettingsObject(parent map[string]any, name string) (map[string]any, error) {
	value, exists := parent[name]
	if !exists {
		object := map[string]any{}
		parent[name] = object
		return object, nil
	}
	object, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("Windows Terminal %s is not an object", name)
	}
	return object, nil
}

func patchGuestTerminalFont(profile map[string]any) error {
	if _, exists := profile["fontFace"]; exists {
		profile["fontFace"] = windowsTerminalGuestFont
	}
	font, exists := profile["font"]
	if !exists {
		font = map[string]any{}
		profile["font"] = font
	}
	fontObject, ok := font.(map[string]any)
	if !ok {
		return errors.New("font is not an object")
	}
	fontObject["face"] = windowsTerminalGuestFont
	return nil
}

func configureGuestPowerShellProfile(profile map[string]any) {
	profile["guid"] = powerShellProfileGUID
	profile["name"] = "PowerShell"
	profile["hidden"] = false
	profile["commandline"] = powerShellCommandLine
	profile["source"] = "Windows.Terminal.PowershellCore"
}

func encodePowerShell(script string) string {
	codeUnits := utf16.Encode([]rune(script))
	encoded := make([]byte, len(codeUnits)*2)
	for index, codeUnit := range codeUnits {
		encoded[index*2] = byte(codeUnit)
		encoded[index*2+1] = byte(codeUnit >> 8)
	}
	return base64.StdEncoding.EncodeToString(encoded)
}
