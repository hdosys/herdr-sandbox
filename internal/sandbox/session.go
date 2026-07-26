package sandbox

import (
	"context"
	"crypto/rand"
	_ "embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	defaultMemoryMB          = 32768
	defaultTimeout           = 20 * time.Minute
	configurationSyncTimeout = 5 * time.Minute
	sshTargetName            = "sandbox"
	guestHerdrPath           = guestRootDirectory + `\runtime\herdr\herdr.exe`
)

//go:embed assets/bootstrap.ps1
var bootstrapScript []byte

type Options struct {
	DataDirectory string
	MemoryMB      int
	Timeout       time.Duration
	Output        io.Writer
}

type Connection struct {
	RunDirectory    string
	StatusDirectory string
	SSHConfigPath   string
	SSHTarget       string
	GuestIP         string
	WinGetVersion   string
	HerdrVersion    string
	HerdrProtocol   int

	privateKeyPath  string
	herdrExecutable string
}

type runPlan struct {
	ID                         string
	DataDirectory              string
	RunDirectory               string
	InputDirectory             string
	StatusDirectory            string
	CacheDirectory             string
	Tailscale                  bool
	Packages                   wingetPackagePlan
	CodingAgentSync            codingAgentSyncConfiguration
	WindowsTerminal            windowsTerminalConfiguration
	ConfigPath                 string
	PrivateKeyPath             string
	PublicKeyPath              string
	Workspaces                 []workspacePlan
	ActiveWorkspace            string
	RequiresVisualStudioLayout bool
	SandboxExecutable          string
}

type provisioningSnapshot struct {
	Directory                  string
	ProjectScriptsDirectory    string
	PackagePlanPath            string
	WorkspaceManifestPath      string
	Workspaces                 []workspacePlan
	ActiveWorkspace            string
	RequiresVisualStudioLayout bool
}

func DefaultOptions() Options {
	return Options{Timeout: defaultTimeout, Output: io.Discard}
}

func Up(ctx context.Context, options Options) (Connection, error) {
	if runtime.GOOS != "windows" {
		return Connection{}, errors.New("Windows Sandbox is available only on Windows")
	}
	if options.Output == nil {
		options.Output = io.Discard
	}
	if options.Timeout == 0 {
		options.Timeout = defaultTimeout
	}
	if options.MemoryMB != 0 && options.MemoryMB < 2048 {
		return Connection{}, fmt.Errorf("Sandbox memory must be at least 2048 MB, got %d", options.MemoryMB)
	}
	if options.Timeout <= 0 {
		return Connection{}, errors.New("Sandbox timeout must be positive")
	}
	authKey, authKeyFound, err := consumeTailscaleAuthKeyEnvironment()
	if err != nil {
		return Connection{}, err
	}
	defer clear(authKey)

	release, err := loadHerdrRelease()
	if err != nil {
		return Connection{}, err
	}
	dataDirectory := options.DataDirectory
	if dataDirectory == "" {
		dataDirectory, err = defaultDataDirectory()
		if err != nil {
			return Connection{}, err
		}
	}
	herdrExecutable, hostVersion, err := ensurePinnedHostHerdr(ctx, release, dataDirectory, options.Output)
	if err != nil {
		return Connection{}, err
	}
	if hostVersion != release.Version {
		return Connection{}, fmt.Errorf("host Herdr version = %q, required %q", hostVersion, release.Version)
	}
	provisioning, err := resolveProvisioning("")
	if err != nil {
		return Connection{}, err
	}
	memoryMB := options.MemoryMB
	if memoryMB == 0 {
		memoryMB = provisioning.MemoryMB
	}
	fmt.Fprintf(options.Output, "Windows Terminal host edition: %s\n", provisioning.WindowsTerminal.Edition)
	fmt.Fprintf(options.Output, "Sandbox memory: %d MB\n", memoryMB)
	releaseLifecycle, err := acquireLifecycleLock(ctx)
	if err != nil {
		return Connection{}, err
	}
	defer releaseLifecycle()
	sessionStatus, err := inspectSessionAt(ctx, dataDirectory)
	if err != nil {
		return Connection{}, err
	}
	if sessionStatus.State == SessionReady {
		sandboxExecutable, err := windowsSandboxExecutable()
		if err != nil {
			return Connection{}, err
		}
		active, found, err := loadActiveSession(dataDirectory, sandboxExecutable)
		if err != nil {
			return Connection{}, err
		}
		if !found {
			return Connection{}, errors.New("ready Sandbox lost its active-session identity")
		}
		return reprovisionReadySession(ctx, options, active, provisioning, memoryMB, herdrExecutable, release)
	}
	if sessionStatus.State != SessionStopped {
		return Connection{}, fmt.Errorf("existing Windows Sandbox state is %s; inspect with `herdr-sandbox status` and use `herdr-sandbox down` before a fresh launch", sessionStatus.State)
	}
	if err := ensureNoRunningSandbox(ctx); err != nil {
		return Connection{}, err
	}
	tailscaleBootstrap, err := prepareTailscaleBootstrap(dataDirectory, provisioning.Tailscale, authKey, authKeyFound)
	if err != nil {
		return Connection{}, err
	}
	defer tailscaleBootstrap.clear()
	plan, err := prepareRun(ctx, dataDirectory, memoryMB, provisioning)
	if err != nil {
		return Connection{}, err
	}
	fmt.Fprintf(options.Output, "Run workspace: %s\n", plan.RunDirectory)
	if plan.RequiresVisualStudioLayout {
		fmt.Fprintln(options.Output, "Preparing the required Visual Studio Build Tools layout on the host...")
		if err := prepareVisualStudioLayout(ctx, plan, options.Output); err != nil {
			return Connection{}, err
		}
	}

	if err := ensureNoRunningSandbox(ctx); err != nil {
		return Connection{}, err
	}
	if err := launchSandbox(ctx, plan); err != nil {
		return Connection{}, err
	}
	if err := releaseLifecycle(); err != nil {
		return Connection{}, err
	}
	fmt.Fprintln(options.Output, "Windows Sandbox started; waiting for guest provisioning...")

	waitContext, cancel := context.WithTimeout(ctx, options.Timeout)
	defer cancel()
	connectable, err := waitForConnectable(waitContext, plan.StatusDirectory, options.Output)
	if err != nil {
		return Connection{}, err
	}
	if connectable.HerdrVersion != release.Version || connectable.HerdrProtocol != release.Protocol {
		identityErr := fmt.Errorf("guest Herdr identity = %q protocol %d, required %q protocol %d", connectable.HerdrVersion, connectable.HerdrProtocol, release.Version, release.Protocol)
		return Connection{}, publishConfigurationFailure(plan.StatusDirectory, "guest-identity", identityErr)
	}

	connection, err := writeRunConnection(plan, connectable, herdrExecutable)
	if err != nil {
		return Connection{}, publishConfigurationFailure(plan.StatusDirectory, "ssh-material", err)
	}
	if err := verifySSH(waitContext, connection); err != nil {
		return Connection{}, publishConfigurationFailure(plan.StatusDirectory, "ssh-verification", err)
	}
	if err := verifyGuestHerdr(waitContext, connection); err != nil {
		return Connection{}, publishConfigurationFailure(plan.StatusDirectory, "herdr-verification", err)
	}
	if plan.Tailscale {
		fmt.Fprintln(options.Output, "Restoring or enrolling the stable Tailscale identity...")
		releaseTailscale, lockErr := acquireLifecycleLock(waitContext)
		if lockErr != nil {
			return Connection{}, publishConfigurationFailure(plan.StatusDirectory, "tailscale-preflight", lockErr)
		}
		tailscaleContext, cancelTailscale := context.WithTimeout(waitContext, tailscaleIdentityTimeout)
		err = configureFreshTailscale(tailscaleContext, connection, plan.DataDirectory, tailscaleBootstrap)
		releaseErr := releaseTailscale()
		cancelTailscale()
		if err == nil {
			err = releaseErr
		} else if releaseErr != nil {
			err = fmt.Errorf("%w; additionally release Tailscale lifecycle lock: %v", err, releaseErr)
		}
		if err != nil {
			phase := "tailscale-identity"
			if errors.Is(err, errTailscaleIdentityNotEstablished) {
				phase = "tailscale-not-enrolled"
			}
			return Connection{}, publishConfigurationFailure(plan.StatusDirectory, phase, err)
		}
		fmt.Fprintln(options.Output, "Stable Tailscale identity restored, verified, and protected on the host.")
	}
	fmt.Fprintf(options.Output, "Transferring and verifying selected development configuration: %s...\n", provisioningConfigurationSummary(plan.Packages, plan.CodingAgentSync))
	syncContext, cancelSync := context.WithTimeout(waitContext, configurationSyncTimeout)
	err = syncDevelopmentConfiguration(syncContext, connection, plan.WindowsTerminal, plan.Packages, plan.CodingAgentSync, filepath.Join(plan.InputDirectory, "provisioning"))
	cancelSync()
	if err != nil {
		return Connection{}, publishConfigurationFailure(plan.StatusDirectory, "configuration-sync", err)
	}
	if err := installRunConnectionAlias(plan.DataDirectory, connection); err != nil {
		return Connection{}, publishConfigurationFailure(plan.StatusDirectory, "ssh-alias", err)
	}
	if err := writeConfigurationHandoff(plan.StatusDirectory, configurationHandoffStatus{
		SchemaVersion: statusSchemaVersion,
		Outcome:       configurationHandoffVerified,
	}); err != nil {
		return Connection{}, err
	}
	fmt.Fprintln(options.Output, "Development configuration transferred and verified; waiting for final workspace creation...")
	ready, err := waitForReady(waitContext, plan.StatusDirectory, options.Output)
	if err != nil {
		return Connection{}, err
	}
	if !sameConnectionIdentity(connectable, ready) {
		return Connection{}, errors.New("terminal ready identity differs from the verified connection identity")
	}

	fmt.Fprintf(options.Output, "WinGet: %s\n", connection.WinGetVersion)
	fmt.Fprintf(options.Output, "Herdr: %s (protocol %d)\n", connection.HerdrVersion, connection.HerdrProtocol)
	fmt.Fprintf(options.Output, "SSH config: %s\n", connection.SSHConfigPath)
	fmt.Fprintf(options.Output, "Remote attach: herdr --remote %s\n", connection.SSHTarget)
	return connection, nil
}

func publishConfigurationFailure(statusDirectory, phase string, cause error) error {
	message := boundedText([]byte(cause.Error()))
	handoffErr := writeConfigurationHandoff(statusDirectory, configurationHandoffStatus{
		SchemaVersion: statusSchemaVersion,
		Outcome:       configurationHandoffFailed,
		Phase:         phase,
		Message:       message,
	})
	if handoffErr != nil {
		return fmt.Errorf("%w; additionally publish configuration failure handoff: %v", cause, handoffErr)
	}
	return cause
}

func Attach(ctx context.Context, connection Connection, stdin io.Reader, stdout, stderr io.Writer) error {
	if connection.herdrExecutable == "" || connection.RunDirectory == "" || connection.SSHTarget == "" {
		return errors.New("Herdr connection is incomplete")
	}
	if err := validateInteractiveAttachStreams(stdin, stdout, stderr); err != nil {
		return err
	}
	command := exec.CommandContext(ctx, connection.herdrExecutable, "--remote", connection.SSHTarget)
	command.Stdin = stdin
	command.Stdout = stdout
	command.Stderr = stderr
	command.Env = attachEnvironment(childProcessEnvironment(os.Environ()))
	if err := command.Run(); err != nil {
		return fmt.Errorf("attach to guest Herdr server: %w", err)
	}
	if err := verifyGuestHerdr(ctx, connection); err != nil {
		return fmt.Errorf("verify guest Herdr persistence after detach: %w", err)
	}
	return nil
}

func attachEnvironment(parent []string) []string {
	environment := make([]string, 0, len(parent))
	for _, entry := range parent {
		name, _, found := strings.Cut(entry, "=")
		if found && isAttachEnvironmentOverride(name) {
			continue
		}
		environment = append(environment, entry)
	}
	return environment
}

func isAttachEnvironmentOverride(name string) bool {
	switch strings.ToUpper(name) {
	case "HERDR_ENV", "HERDR_SESSION", "HERDR_SOCKET_PATH", "HERDR_CLIENT_SOCKET_PATH", "HERDR_REATTACH_COMMAND", "HERDR_REMOTE_KEYBINDINGS", "HERDR_RENDER_ENCODING":
		return true
	default:
		return false
	}
}

func defaultDataDirectory() (string, error) {
	localAppData := strings.TrimSpace(os.Getenv("LOCALAPPDATA"))
	if localAppData == "" {
		return "", errors.New("LOCALAPPDATA is not set")
	}
	if !filepath.IsAbs(localAppData) {
		return "", fmt.Errorf("LOCALAPPDATA is not absolute: %q", localAppData)
	}
	return filepath.Join(localAppData, "herdr-sandbox"), nil
}

func effectiveCacheDirectory(configured string) (string, error) {
	if configured != "" {
		return configured, nil
	}
	temporaryDirectory := strings.TrimSpace(os.TempDir())
	if temporaryDirectory == "" {
		return "", errors.New("system temporary directory is empty")
	}
	if !filepath.IsAbs(temporaryDirectory) {
		return "", fmt.Errorf("system temporary directory is not absolute: %q", temporaryDirectory)
	}
	return filepath.Join(temporaryDirectory, applicationName, "cache"), nil
}

func prepareRun(ctx context.Context, dataDirectory string, memoryMB int, provisioning provisioningPlan) (runPlan, error) {
	if !filepath.IsAbs(dataDirectory) {
		return runPlan{}, errors.New("data directory must be absolute")
	}
	dataDirectory = filepath.Clean(dataDirectory)
	if err := provisioning.WindowsTerminal.validate(); err != nil {
		return runPlan{}, err
	}
	if err := provisioning.Packages.validate(provisioning.WindowsTerminal); err != nil {
		return runPlan{}, err
	}
	cacheDirectory, err := effectiveCacheDirectory(provisioning.CacheDirectory)
	if err != nil {
		return runPlan{}, err
	}
	for _, protected := range []string{filepath.Join(dataDirectory, "identity"), filepath.Join(dataDirectory, "runs")} {
		if hostPathsOverlap(cacheDirectory, protected) {
			return runPlan{}, fmt.Errorf("cache directory overlaps private run state: %s", protected)
		}
	}
	privateKey, publicKey, err := ensureIdentity(ctx, filepath.Join(dataDirectory, "identity"))
	if err != nil {
		return runPlan{}, err
	}
	sandboxExecutable, err := windowsSandboxExecutable()
	if err != nil {
		return runPlan{}, err
	}
	id, err := newRunID()
	if err != nil {
		return runPlan{}, err
	}

	plan := runPlan{
		ID:                id,
		DataDirectory:     dataDirectory,
		RunDirectory:      filepath.Join(dataDirectory, "runs", id),
		PrivateKeyPath:    privateKey,
		PublicKeyPath:     publicKey,
		Tailscale:         provisioning.Tailscale,
		Packages:          provisioning.Packages,
		CodingAgentSync:   provisioning.CodingAgentSync,
		Workspaces:        provisioning.Workspaces,
		WindowsTerminal:   provisioning.WindowsTerminal,
		SandboxExecutable: sandboxExecutable,
	}
	plan.CacheDirectory = cacheDirectory
	plan.InputDirectory = filepath.Join(plan.RunDirectory, "input")
	plan.StatusDirectory = filepath.Join(plan.RunDirectory, "status")
	plan.ConfigPath = filepath.Join(plan.RunDirectory, "herdr-sandbox.wsb")
	for _, directory := range []string{plan.InputDirectory, plan.StatusDirectory, plan.CacheDirectory} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			return runPlan{}, fmt.Errorf("create mapped directory %s: %w", directory, err)
		}
	}
	plan.InputDirectory, err = canonicalMappedDirectory(plan.InputDirectory)
	if err != nil {
		return runPlan{}, err
	}
	plan.StatusDirectory, err = canonicalMappedDirectory(plan.StatusDirectory)
	if err != nil {
		return runPlan{}, err
	}
	plan.CacheDirectory, err = canonicalMappedDirectory(plan.CacheDirectory)
	if err != nil {
		return runPlan{}, err
	}
	for index := range plan.Workspaces {
		plan.Workspaces[index].HostDirectory, err = canonicalMappedDirectory(plan.Workspaces[index].HostDirectory)
		if err != nil {
			return runPlan{}, fmt.Errorf("workspace %q: %w", plan.Workspaces[index].Name, err)
		}
	}
	type physicalMapping struct {
		role     string
		identity string
	}
	mappings := make([]physicalMapping, 0, len(plan.Workspaces)+3)
	for _, mapped := range []struct {
		role string
		path string
	}{{"bootstrap input", plan.InputDirectory}, {"status", plan.StatusDirectory}, {"cache", plan.CacheDirectory}} {
		identity, identityErr := physicalMappedDirectory(mapped.path)
		if identityErr != nil {
			return runPlan{}, identityErr
		}
		mappings = append(mappings, physicalMapping{role: mapped.role, identity: identity})
	}
	for _, workspace := range plan.Workspaces {
		identity, identityErr := physicalMappedDirectory(workspace.HostDirectory)
		if identityErr != nil {
			return runPlan{}, fmt.Errorf("workspace %q: %w", workspace.Name, identityErr)
		}
		mappings = append(mappings, physicalMapping{role: "workspace " + workspace.Name, identity: identity})
	}
	for left := range mappings {
		for right := left + 1; right < len(mappings); right++ {
			if hostPathsOverlap(mappings[left].identity, mappings[right].identity) {
				return runPlan{}, fmt.Errorf("physical mapped paths overlap: %s and %s", mappings[left].role, mappings[right].role)
			}
		}
	}
	protected := make([]physicalMapping, 0, 2)
	for _, protectedPath := range []struct {
		role string
		path string
	}{{"SSH identity", filepath.Join(dataDirectory, "identity")}, {"run state", filepath.Join(dataDirectory, "runs")}} {
		canonical, canonicalErr := canonicalMappedDirectory(protectedPath.path)
		if canonicalErr != nil {
			return runPlan{}, canonicalErr
		}
		identity, identityErr := physicalMappedDirectory(canonical)
		if identityErr != nil {
			return runPlan{}, identityErr
		}
		protected = append(protected, physicalMapping{role: protectedPath.role, identity: identity})
	}
	for _, mapped := range mappings[2:] {
		for _, private := range protected {
			if hostPathsOverlap(mapped.identity, private.identity) {
				return runPlan{}, fmt.Errorf("%s physically overlaps private %s", mapped.role, private.role)
			}
		}
	}
	config, err := renderConfig(plan.InputDirectory, plan.StatusDirectory, plan.CacheDirectory, plan.Workspaces, memoryMB)
	if err != nil {
		return runPlan{}, err
	}

	publicKeyData, err := os.ReadFile(publicKey)
	if err != nil {
		return runPlan{}, fmt.Errorf("read host SSH public key: %w", err)
	}
	files := []struct {
		path string
		data []byte
		mode os.FileMode
	}{
		{path: filepath.Join(plan.InputDirectory, "bootstrap.ps1"), data: bootstrapScript, mode: 0o644},
		{path: filepath.Join(plan.InputDirectory, "herdr-release.json"), data: herdrReleaseJSON, mode: 0o644},
		{path: filepath.Join(plan.InputDirectory, "authorized_key.pub"), data: publicKeyData, mode: 0o644},
	}
	for _, file := range files {
		if err := os.WriteFile(file.path, file.data, file.mode); err != nil {
			return runPlan{}, fmt.Errorf("write run input %s: %w", filepath.Base(file.path), err)
		}
	}
	snapshot, err := prepareProvisioningSnapshot(ctx, plan.RunDirectory, filepath.Join(plan.InputDirectory, "provisioning"), provisioning)
	if err != nil {
		return runPlan{}, err
	}
	plan.Workspaces = snapshot.Workspaces
	plan.ActiveWorkspace = snapshot.ActiveWorkspace
	plan.RequiresVisualStudioLayout = snapshot.RequiresVisualStudioLayout
	if err := os.WriteFile(plan.ConfigPath, config, 0o600); err != nil {
		return runPlan{}, fmt.Errorf("write Windows Sandbox configuration: %w", err)
	}
	return plan, nil
}

func prepareProvisioningSnapshot(ctx context.Context, inspectionDirectory, snapshotDirectory string, provisioning provisioningPlan) (provisioningSnapshot, error) {
	if !filepath.IsAbs(inspectionDirectory) || !filepath.IsAbs(snapshotDirectory) {
		return provisioningSnapshot{}, errors.New("provisioning snapshot directories must be absolute")
	}
	if err := os.MkdirAll(snapshotDirectory, 0o700); err != nil {
		return provisioningSnapshot{}, fmt.Errorf("create provisioning snapshot directory: %w", err)
	}
	packagePlanData, err := encodeWingetPackagePlan(provisioning.Packages, provisioning.WindowsTerminal)
	if err != nil {
		return provisioningSnapshot{}, err
	}
	packagePlanPath := filepath.Join(snapshotDirectory, wingetPackagePlanFileName)
	if err := os.WriteFile(packagePlanPath, packagePlanData, 0o600); err != nil {
		return provisioningSnapshot{}, fmt.Errorf("write WinGet package plan: %w", err)
	}
	for _, source := range []struct {
		path string
		name string
		role string
	}{
		{path: provisioning.BaseScript, name: baseProvisioningName, role: "base"},
		{path: provisioning.StackScript, name: stackProvisioningName, role: "stack"},
	} {
		data, readErr := os.ReadFile(source.path)
		if readErr != nil {
			return provisioningSnapshot{}, fmt.Errorf("read %s provisioning script %s: %w", source.role, source.path, readErr)
		}
		if writeErr := os.WriteFile(filepath.Join(snapshotDirectory, source.name), data, 0o600); writeErr != nil {
			return provisioningSnapshot{}, fmt.Errorf("write %s provisioning snapshot: %w", source.role, writeErr)
		}
	}
	projectScriptsDirectory := filepath.Join(snapshotDirectory, "projects")
	if err := os.MkdirAll(projectScriptsDirectory, 0o700); err != nil {
		return provisioningSnapshot{}, fmt.Errorf("create project provisioning snapshot directory: %w", err)
	}
	for _, workspace := range provisioning.Workspaces {
		data, readErr := os.ReadFile(workspace.ProvisioningPath)
		if readErr != nil {
			return provisioningSnapshot{}, fmt.Errorf("read provisioning script %s: %w", workspace.ProvisioningPath, readErr)
		}
		name := workspace.Name + ".ps1"
		if writeErr := os.WriteFile(filepath.Join(projectScriptsDirectory, name), data, 0o600); writeErr != nil {
			return provisioningSnapshot{}, fmt.Errorf("write project provisioning snapshot %s: %w", name, writeErr)
		}
	}
	workspaces, err := inspectProjectProvisioningPlan(ctx, inspectionDirectory, projectScriptsDirectory, provisioning.Workspaces)
	if err != nil {
		return provisioningSnapshot{}, err
	}
	requirements := runPlan{Workspaces: workspaces}
	applyWorkspaceRequirements(&requirements)
	workspaceManifest, err := encodeGuestWorkspaceManifest(workspaces, requirements.ActiveWorkspace)
	if err != nil {
		return provisioningSnapshot{}, err
	}
	workspaceManifestPath := filepath.Join(snapshotDirectory, workspaceManifestName)
	if err := os.WriteFile(workspaceManifestPath, workspaceManifest, 0o600); err != nil {
		return provisioningSnapshot{}, fmt.Errorf("write workspace manifest: %w", err)
	}
	return provisioningSnapshot{
		Directory:                  snapshotDirectory,
		ProjectScriptsDirectory:    projectScriptsDirectory,
		PackagePlanPath:            packagePlanPath,
		WorkspaceManifestPath:      workspaceManifestPath,
		Workspaces:                 workspaces,
		ActiveWorkspace:            requirements.ActiveWorkspace,
		RequiresVisualStudioLayout: requirements.RequiresVisualStudioLayout,
	}, nil
}

func provisioningConfigurationSummary(packages wingetPackagePlan, codingAgents codingAgentSyncConfiguration) string {
	selected := []string{"Herdr"}
	for _, integration := range []struct {
		id   string
		name string
	}{
		{packageGit, "Git"},
		{packageGitHubCLI, "GitHub CLI"},
		{packageStarship, "Starship"},
		{packageTerminalStable, "Windows Terminal"},
		{packageTerminalPreview, "Windows Terminal"},
	} {
		if packages.enabled(integration.id) {
			selected = append(selected, integration.name)
		}
	}
	selected = append(selected, codingAgentSyncNames(codingAgents)...)
	return strings.Join(selected, ", ")
}

func applyWorkspaceRequirements(plan *runPlan) {
	plan.ActiveWorkspace = ""
	plan.RequiresVisualStudioLayout = false
	for _, workspace := range plan.Workspaces {
		if workspaceHasStack(workspace, stackRustMSVC) {
			plan.RequiresVisualStudioLayout = true
		}
		if workspace.Active {
			plan.ActiveWorkspace = workspace.GuestDirectory
		}
	}
	if plan.ActiveWorkspace == "" && len(plan.Workspaces) > 0 {
		plan.ActiveWorkspace = plan.Workspaces[0].GuestDirectory
	}
}

func workspaceHasStack(workspace workspacePlan, expected projectStack) bool {
	for _, stack := range workspace.Stacks {
		if stack == expected {
			return true
		}
	}
	return false
}

func newRunID() (string, error) {
	random := make([]byte, 4)
	if _, err := rand.Read(random); err != nil {
		return "", fmt.Errorf("generate run ID: %w", err)
	}
	return time.Now().UTC().Format("20060102-150405") + "-" + hex.EncodeToString(random), nil
}

func windowsSandboxExecutable() (string, error) {
	windowsDirectory := strings.TrimSpace(os.Getenv("WINDIR"))
	if windowsDirectory == "" {
		return "", errors.New("WINDIR is not set")
	}
	path := filepath.Join(windowsDirectory, "System32", "WindowsSandbox.exe")
	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", errors.New("Windows Sandbox is unavailable; enable Containers-DisposableClientVM and reboot")
		}
		return "", fmt.Errorf("inspect Windows Sandbox executable: %w", err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("Windows Sandbox executable is not a regular file: %s", path)
	}
	return path, nil
}

func launchSandbox(ctx context.Context, plan runPlan) error {
	command := exec.Command(plan.SandboxExecutable, plan.ConfigPath)
	if err := command.Start(); err != nil {
		return fmt.Errorf("launch Windows Sandbox: %w", err)
	}
	if command.Process == nil {
		return errors.New("launch Windows Sandbox: process identity is unavailable")
	}
	if err := recordActiveSession(ctx, plan, command.Process.Pid); err != nil {
		_ = command.Process.Kill()
		_ = command.Wait()
		return err
	}
	if err := command.Process.Release(); err != nil {
		_ = removeActiveSession(plan.DataDirectory)
		_ = command.Process.Kill()
		_ = command.Wait()
		return fmt.Errorf("release Windows Sandbox launcher process: %w", err)
	}
	return nil
}
