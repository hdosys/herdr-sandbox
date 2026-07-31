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
	"sort"
	"strings"
	"time"

	"herdr-sandbox/internal/hiddenprocess"
)

const (
	defaultMemoryMB             = 32768
	configurationSyncTimeout    = 5 * time.Minute
	configurationHandoffTimeout = tailscaleIdentityTimeout + configurationSyncTimeout + 2*time.Minute
	sshTargetName               = "sandbox"
	guestHerdrPath              = guestRootDirectory + `\runtime\herdr\herdr.exe`
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
	return Options{Output: io.Discard}
}

func withOptionalTimeout(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout == 0 {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, timeout)
}

func Up(ctx context.Context, options Options, hostHerdr HostHerdr) (Connection, error) {
	if runtime.GOOS != "windows" {
		return Connection{}, errors.New("Windows Sandbox is available only on Windows")
	}
	if options.Output == nil {
		options.Output = io.Discard
	}
	if options.MemoryMB != 0 && options.MemoryMB < 2048 {
		return Connection{}, fmt.Errorf("Sandbox memory must be at least 2048 MB, got %d", options.MemoryMB)
	}
	if options.Timeout < 0 {
		return Connection{}, errors.New("Sandbox timeout must be positive when set")
	}
	if err := hostHerdr.validate(); err != nil {
		return Connection{}, fmt.Errorf("validate compatible host Herdr: %w", err)
	}
	runContext, cancelRun := withOptionalTimeout(ctx, options.Timeout)
	defer cancelRun()

	authKey, authKeyFound, err := consumeTailscaleAuthKeyEnvironment()
	if err != nil {
		return Connection{}, err
	}
	defer clear(authKey)

	_, err = loadBootstrapRelease()
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
	releaseLifecycle, err := acquireLifecycleLock(runContext)
	if err != nil {
		return Connection{}, err
	}
	defer releaseLifecycle()
	_, interruptedOperation, err := interruptAbandonedActiveOperation(dataDirectory)
	if err != nil {
		return Connection{}, fmt.Errorf("reconcile previous retained operation before up: %w", err)
	}
	if interruptedOperation {
		fmt.Fprintln(options.Output, "Warning: the previous retained reprovision ended without a terminal result and is now recorded as interrupted.")
	}

	provisioning, err := resolveProvisioning("")
	if err != nil {
		return Connection{}, err
	}
	dataDirectory, err = prepareHostStateDirectories(dataDirectory)
	if err != nil {
		return Connection{}, err
	}
	memoryMB := options.MemoryMB
	if memoryMB == 0 {
		memoryMB = provisioning.MemoryMB
	}
	fmt.Fprintln(options.Output, "Launch configuration")
	fmt.Fprintf(options.Output, "  Windows Terminal: %s\n", provisioning.WindowsTerminal.Edition)
	fmt.Fprintf(options.Output, "  Memory: %d MB\n", memoryMB)
	sessionStatus, err := inspectSessionAt(runContext, dataDirectory)
	if err != nil {
		return Connection{}, err
	}
	if !interruptedOperation && sessionStatus.Operation != nil && sessionStatus.Operation.State == operationStateInterrupted {
		fmt.Fprintln(options.Output, "Warning: the previous retained reprovision ended without a terminal result and is recorded as interrupted.")
	}
	var retainedPlan runPlan
	var retainedReady readyStatus
	var tailscaleBootstrap tailscaleBootstrap
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
		retainedPlan, err = retainedRunPlan(active, provisioning, memoryMB)
		if err != nil {
			return Connection{}, err
		}
		retainedReady, found, err = readOptionalStatus[readyStatus](filepath.Join(retainedPlan.StatusDirectory, readyFileName))
		if err != nil {
			return Connection{}, fmt.Errorf("read retained Sandbox ready status: %w", err)
		}
		if !found {
			return Connection{}, errors.New("retained Sandbox ready status is missing")
		}
		if err := retainedReady.validate(); err != nil {
			return Connection{}, fmt.Errorf("validate retained Sandbox ready status: %w", err)
		}
		if retainedReady.HerdrVersion != hostHerdr.version || retainedReady.HerdrProtocol != hostHerdr.protocol {
			return Connection{}, fmt.Errorf("retained guest Herdr identity = %q protocol %d, current host = %q protocol %d; run `herdr-sandbox down` and then `herdr-sandbox up` to provision the current host runtime", retainedReady.HerdrVersion, retainedReady.HerdrProtocol, hostHerdr.version, hostHerdr.protocol)
		}
	} else if sessionStatus.State != SessionStopped {
		return Connection{}, fmt.Errorf("existing Windows Sandbox state is %s; inspect with `herdr-sandbox status` and use `herdr-sandbox down` before a fresh launch", sessionStatus.State)
	} else {
		if err := ensureNoRunningSandbox(runContext); err != nil {
			return Connection{}, err
		}
		tailscaleBootstrap, err = prepareTailscaleBootstrap(dataDirectory, provisioning.Tailscale, authKey, authKeyFound)
		if err != nil {
			return Connection{}, err
		}
		defer tailscaleBootstrap.clear()
	}

	if sessionStatus.State == SessionReady {
		connection, err := reprovisionReadySession(runContext, options, retainedPlan, retainedReady, provisioning, hostHerdr)
		if err != nil {
			return Connection{}, err
		}
		if err := hostHerdr.verifyUnchanged(runContext); err != nil {
			return Connection{}, err
		}
		fmt.Fprintln(options.Output, "Ready Sandbox")
		fmt.Fprintln(options.Output, "  Mode: retained and reprovisioned")
		fmt.Fprintf(options.Output, "  Attach: herdr --remote %s\n", connection.SSHTarget)
		return connection, nil
	}

	plan, err := prepareRun(runContext, dataDirectory, memoryMB, provisioning, hostHerdr)
	if err != nil {
		return Connection{}, err
	}
	fmt.Fprintf(options.Output, "Run workspace: %s\n", plan.RunDirectory)
	if plan.RequiresVisualStudioLayout {
		fmt.Fprintln(options.Output, "Preparing the required Visual Studio Build Tools layout on the host...")
		if err := prepareVisualStudioLayout(runContext, plan, options.Output); err != nil {
			return Connection{}, err
		}
	}

	if err := ensureNoRunningSandbox(runContext); err != nil {
		return Connection{}, err
	}
	if err := launchSandbox(runContext, plan); err != nil {
		return Connection{}, err
	}
	if err := releaseLifecycle(); err != nil {
		return Connection{}, err
	}
	fmt.Fprintln(options.Output, "Windows Sandbox started; waiting for guest provisioning...")

	connectable, err := waitForConnectable(runContext, plan.StatusDirectory, options.Output)
	if err != nil {
		return Connection{}, err
	}
	if connectable.HerdrVersion != hostHerdr.version || connectable.HerdrProtocol != hostHerdr.protocol {
		identityErr := fmt.Errorf("guest Herdr identity = %q protocol %d, copied host runtime = %q protocol %d", connectable.HerdrVersion, connectable.HerdrProtocol, hostHerdr.version, hostHerdr.protocol)
		return Connection{}, publishConfigurationFailure(plan.StatusDirectory, "guest-identity", identityErr)
	}

	connection, err := writeRunConnection(plan, connectable, hostHerdr.commandPath)
	if err != nil {
		return Connection{}, publishConfigurationFailure(plan.StatusDirectory, "ssh-material", err)
	}
	if err := verifySSH(runContext, connection); err != nil {
		return Connection{}, publishConfigurationFailure(plan.StatusDirectory, "ssh-verification", err)
	}
	if err := verifyGuestHerdr(runContext, connection); err != nil {
		return Connection{}, publishConfigurationFailure(plan.StatusDirectory, "herdr-verification", err)
	}
	if plan.Tailscale {
		fmt.Fprintln(options.Output, "Restoring or enrolling the stable Tailscale identity...")
		releaseTailscale, lockErr := acquireLifecycleLock(runContext)
		if lockErr != nil {
			return Connection{}, publishConfigurationFailure(plan.StatusDirectory, "tailscale-preflight", lockErr)
		}
		tailscaleContext, cancelTailscale := context.WithTimeout(runContext, tailscaleIdentityTimeout)
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
	writeProvisioningConfiguration(options.Output, "Transferring and verifying development configuration", plan.Packages, plan.CodingAgentSync)
	syncContext, cancelSync := context.WithTimeout(runContext, configurationSyncTimeout)
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
	ready, err := waitForReady(runContext, plan.StatusDirectory, options.Output)
	if err != nil {
		return Connection{}, err
	}
	if !sameConnectionIdentity(connectable, ready) {
		return Connection{}, errors.New("terminal ready identity differs from the verified connection identity")
	}
	if err := hostHerdr.verifyUnchanged(runContext); err != nil {
		return Connection{}, err
	}

	fmt.Fprintln(options.Output, "Sandbox ready")
	fmt.Fprintf(options.Output, "  WinGet: %s\n", connection.WinGetVersion)
	fmt.Fprintf(options.Output, "  Herdr: %s\n", connection.HerdrVersion)
	fmt.Fprintf(options.Output, "  Herdr protocol: %d\n", connection.HerdrProtocol)
	fmt.Fprintf(options.Output, "  SSH config: %s\n", connection.SSHConfigPath)
	fmt.Fprintf(options.Output, "  Attach: herdr --remote %s\n", connection.SSHTarget)
	return connection, nil
}

func publishConfigurationFailure(statusDirectory, phase string, cause error) error {
	message := sanitizeTerminalText(boundedText([]byte(cause.Error())), 4096)
	if message == "" {
		message = "Configuration failed without printable diagnostics."
	}
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
	if err := connection.validate(); err != nil {
		return err
	}
	if err := validateInteractiveAttachStreams(stdin, stdout, stderr); err != nil {
		return err
	}
	currentHost, err := inspectCompatibleHostHerdr(ctx, connection.herdrExecutable)
	if err != nil {
		return fmt.Errorf("verify host Herdr before attach: %w", err)
	}
	if currentHost.version != connection.HerdrVersion || currentHost.protocol != connection.HerdrProtocol {
		return fmt.Errorf("host Herdr identity = %q protocol %d, ready guest = %q protocol %d; run `herdr-sandbox down` and then `herdr-sandbox up` to provision the current host runtime", currentHost.version, currentHost.protocol, connection.HerdrVersion, connection.HerdrProtocol)
	}
	command := exec.CommandContext(ctx, currentHost.commandPath, "--remote", connection.SSHTarget)
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

// ValidateInteractiveAttachStreams lets the CLI reject an implicit interactive
// attach before provisioning performs expensive or mutating work.
func ValidateInteractiveAttachStreams(stdin io.Reader, stdout, stderr io.Writer) error {
	return validateInteractiveAttachStreams(stdin, stdout, stderr)
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

func prepareHostStateDirectories(dataDirectory string) (string, error) {
	physicalDataDirectory, err := ensurePhysicalDirectory(dataDirectory, "app data")
	if err != nil {
		return "", err
	}
	for _, child := range []struct {
		name string
		role string
	}{{"identity", "private identity"}, {"runs", "run state"}} {
		if _, err := ensurePhysicalDirectory(filepath.Join(physicalDataDirectory, child.name), child.role); err != nil {
			return "", err
		}
	}
	return physicalDataDirectory, nil
}

func prepareRun(ctx context.Context, dataDirectory string, memoryMB int, provisioning provisioningPlan, hostHerdr HostHerdr) (runPlan, error) {
	var err error
	dataDirectory, err = prepareHostStateDirectories(dataDirectory)
	if err != nil {
		return runPlan{}, err
	}
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
	for _, directory := range []string{plan.InputDirectory, plan.StatusDirectory} {
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
	plan.CacheDirectory, err = ensurePhysicalDirectory(plan.CacheDirectory, "cache")
	if err != nil {
		return runPlan{}, err
	}
	plan.Workspaces, err = canonicalWorkspacePlans(plan.Workspaces)
	if err != nil {
		return runPlan{}, err
	}
	if err := validatePhysicalMappings(dataDirectory, plan.InputDirectory, plan.StatusDirectory, plan.CacheDirectory, plan.Workspaces); err != nil {
		return runPlan{}, err
	}
	provisioning.Workspaces = plan.Workspaces
	config, err := renderConfig(plan.InputDirectory, plan.StatusDirectory, plan.CacheDirectory, plan.Workspaces, memoryMB, provisioning.Audio)
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
		{path: filepath.Join(plan.InputDirectory, "bootstrap-release.json"), data: bootstrapReleaseJSON, mode: 0o644},
		{path: filepath.Join(plan.InputDirectory, "authorized_key.pub"), data: publicKeyData, mode: 0o644},
	}
	for _, file := range files {
		if err := os.WriteFile(file.path, file.data, file.mode); err != nil {
			return runPlan{}, fmt.Errorf("write run input %s: %w", filepath.Base(file.path), err)
		}
	}
	if err := writeHostHerdrRunInput(ctx, hostHerdr, plan.InputDirectory); err != nil {
		return runPlan{}, fmt.Errorf("snapshot compatible host Herdr runtime: %w", err)
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

type physicalMapping struct {
	role     string
	identity string
}

func validatePhysicalMappings(dataDirectory, inputDirectory, statusDirectory, cacheDirectory string, workspaces []workspacePlan) error {
	mappings := make([]physicalMapping, 0, len(workspaces)+3)
	for _, mapped := range []struct {
		role string
		path string
	}{{"bootstrap input", inputDirectory}, {"status", statusDirectory}, {"cache", cacheDirectory}} {
		identity, err := physicalMappedDirectory(mapped.path)
		if err != nil {
			return err
		}
		mappings = append(mappings, physicalMapping{role: mapped.role, identity: identity})
	}
	for _, workspace := range workspaces {
		identity, err := physicalMappedDirectory(workspace.HostDirectory)
		if err != nil {
			return fmt.Errorf("workspace %q: %w", workspace.Name, err)
		}
		mappings = append(mappings, physicalMapping{role: "workspace " + workspace.Name, identity: identity})
	}
	for left := range mappings {
		for right := left + 1; right < len(mappings); right++ {
			if hostPathsOverlap(mappings[left].identity, mappings[right].identity) {
				return fmt.Errorf("physical mapped paths overlap: %s and %s", mappings[left].role, mappings[right].role)
			}
		}
	}
	protected := make([]physicalMapping, 0, 2)
	for _, protectedPath := range []struct {
		role string
		path string
	}{{"SSH identity", filepath.Join(dataDirectory, "identity")}, {"run state", filepath.Join(dataDirectory, "runs")}} {
		canonical, err := canonicalMappedDirectory(protectedPath.path)
		if err != nil {
			return err
		}
		identity, err := physicalMappedDirectory(canonical)
		if err != nil {
			return err
		}
		protected = append(protected, physicalMapping{role: protectedPath.role, identity: identity})
	}
	for _, mapped := range mappings[2:] {
		for _, private := range protected {
			if hostPathsOverlap(mapped.identity, private.identity) {
				return fmt.Errorf("%s physically overlaps private %s", mapped.role, private.role)
			}
		}
		if err := validatePhysicalMappingDoesNotContainProtectedRoot(mapped.role, mapped.identity); err != nil {
			return err
		}
	}
	return nil
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
		path        string
		name        string
		role        string
		maximumSize int64
	}{
		{path: provisioning.BaseScript, name: baseProvisioningName, role: "base", maximumSize: maximumBaseScriptSize},
		{path: provisioning.StackScript, name: stackProvisioningName, role: "stack", maximumSize: maximumStackScriptSize},
		{path: provisioning.UserScript, name: userProvisioningName, role: "user", maximumSize: maximumUserScriptSize},
	} {
		data, readErr := readProvisioningScript(source.path, source.role+" provisioning script", source.maximumSize)
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
		data, readErr := readProvisioningScript(workspace.ProvisioningPath, "project provisioning script", maximumProjectScriptSize)
		if readErr != nil {
			return provisioningSnapshot{}, fmt.Errorf("read provisioning script %s: %w", workspace.ProvisioningPath, readErr)
		}
		name := workspace.Name + ".ps1"
		if writeErr := os.WriteFile(filepath.Join(projectScriptsDirectory, name), data, 0o600); writeErr != nil {
			return provisioningSnapshot{}, fmt.Errorf("write project provisioning snapshot %s: %w", name, writeErr)
		}
	}
	workspaces, userStacks, err := inspectProjectProvisioningPlan(ctx, inspectionDirectory, filepath.Join(snapshotDirectory, userProvisioningName), projectScriptsDirectory, provisioning.Workspaces)
	if err != nil {
		return provisioningSnapshot{}, err
	}
	requirements := runPlan{Workspaces: workspaces}
	applyWorkspaceRequirements(&requirements)
	if stacksContain(userStacks, stackRustMSVC) {
		requirements.RequiresVisualStudioLayout = true
	}
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

func provisioningConfigurationNames(packages wingetPackagePlan, codingAgents codingAgentSyncConfiguration) []string {
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
	sort.Slice(selected, func(left, right int) bool {
		leftFold := strings.ToLower(selected[left])
		rightFold := strings.ToLower(selected[right])
		if leftFold == rightFold {
			return selected[left] < selected[right]
		}
		return leftFold < rightFold
	})
	return selected
}

func writeProvisioningConfiguration(output io.Writer, title string, packages wingetPackagePlan, codingAgents codingAgentSyncConfiguration) {
	fmt.Fprintf(output, "%s:\n", title)
	for _, name := range provisioningConfigurationNames(packages, codingAgents) {
		fmt.Fprintf(output, "  - %s\n", name)
	}
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
	return stacksContain(workspace.Stacks, expected)
}

func stacksContain(stacks []projectStack, expected projectStack) bool {
	for _, stack := range stacks {
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
	path, err := expectedWindowsSandboxExecutable()
	if err != nil {
		return "", err
	}
	exists, err := regularFileExists(path)
	if err != nil {
		return "", fmt.Errorf("inspect Windows Sandbox executable: %w", err)
	}
	if !exists {
		return "", errors.New("Windows Sandbox is unavailable; enable Containers-DisposableClientVM and reboot")
	}
	return path, nil
}

func expectedWindowsSandboxExecutable() (string, error) {
	windowsDirectory := strings.TrimSpace(os.Getenv("WINDIR"))
	if windowsDirectory == "" {
		return "", errors.New("WINDIR is not set")
	}
	if !filepath.IsAbs(windowsDirectory) {
		return "", fmt.Errorf("WINDIR is not absolute: %q", windowsDirectory)
	}
	return filepath.Join(filepath.Clean(windowsDirectory), "System32", "WindowsSandbox.exe"), nil
}

func launchSandbox(ctx context.Context, plan runPlan) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("launch Windows Sandbox: %w", err)
	}
	command := exec.Command(plan.SandboxExecutable, plan.ConfigPath)
	if err := command.Start(); err != nil {
		return fmt.Errorf("launch Windows Sandbox: %w", err)
	}
	if command.Process == nil {
		return errors.New("launch Windows Sandbox: process identity is unavailable")
	}
	if err := recordActiveSession(ctx, plan, command.Process.Pid); err != nil {
		if cleanupErr := terminateUnpublishedSandbox(command); cleanupErr != nil {
			return errors.Join(err, cleanupErr)
		}
		return err
	}
	if err := command.Process.Release(); err != nil {
		_ = removeActiveSession(plan.DataDirectory)
		if cleanupErr := terminateUnpublishedSandbox(command); cleanupErr != nil {
			return errors.Join(fmt.Errorf("release Windows Sandbox launcher process: %w", err), cleanupErr)
		}
		return fmt.Errorf("release Windows Sandbox launcher process: %w", err)
	}
	return nil
}

func terminateUnpublishedSandbox(command *exec.Cmd) error {
	cleanupContext, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := hiddenprocess.TerminateTree(cleanupContext, command.Process); err != nil {
		_ = command.Process.Kill()
		_ = command.Wait()
		return fmt.Errorf("terminate unpublished Windows Sandbox process tree: %w", err)
	}
	_ = command.Wait()
	return nil
}
