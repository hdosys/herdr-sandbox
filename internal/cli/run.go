package cli

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"

	"herdr-sandbox/internal/sandbox"
)

const usage = `Usage:
  herdr-sandbox plan
  herdr-sandbox init [--stack dotnet|go|node|python|rust|zig]...
  herdr-sandbox up [--memory-mb MB] [--timeout DURATION] [--no-attach]
  herdr-sandbox attach
  herdr-sandbox status
  herdr-sandbox down
  herdr-sandbox clean

Commands:
  plan    validate and print the effective plan without changing app or Sandbox state
  init    create one project profile without replacing an existing profile
  up      launch fresh or re-provision the exact ready Sandbox, then attach unless disabled
  attach  verify and attach to the exact ready Sandbox without re-provisioning
  status  report the app-owned Sandbox; proven stale app state may be cleaned
  down    stop only the exact app-owned Sandbox
  clean   remove inactive app-owned run workspaces

Configuration:
  %APPDATA%\herdr-sandbox\config.json
  - workspaces and optional workspaceDiscovery
  - absolute cacheDirectory (default <system-temp>\herdr-sandbox\cache)
  - memoryMB (default 32768), audio, and tailscale
  - codingAgentSync choices
  - wingetPackages additions, removals, and version pins

Behavior:
  - up has no overall timeout unless --timeout is supplied
  - the nearest .herdr-sandbox\provision.ps1 becomes the active workspace
`

const installerCleanUninstallTimeout = 15 * time.Minute

func Run(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	return runWithCommandDependencies(ctx, args, stdin, stdout, stderr, defaultCommandDependencies())
}

type staleCleanup func(context.Context) (sandbox.CleanResult, error)
type sessionInspector func(context.Context) (sandbox.SessionStatus, error)

type commandDependencies struct {
	cleanup        staleCleanup
	inspect        sessionInspector
	resolveHerdr   func(context.Context) (sandbox.HostHerdr, error)
	up             func(context.Context, sandbox.Options, sandbox.HostHerdr) (sandbox.Connection, error)
	openReady      func(context.Context, io.Writer, sandbox.HostHerdr) (sandbox.Connection, error)
	attach         func(context.Context, sandbox.Connection, io.Reader, io.Writer, io.Writer) error
	validateAttach func(io.Reader, io.Writer, io.Writer) error
	resolvePlan    func(context.Context, string) (sandbox.EffectivePlan, error)
	initialize     func(string, []string) (sandbox.ProjectInitResult, error)
	seedInstaller  func() error
	cleanInstaller func(context.Context, bool) error
}

func defaultCommandDependencies() commandDependencies {
	return commandDependencies{
		cleanup:        sandbox.CleanupStaleState,
		inspect:        sandbox.InspectSession,
		resolveHerdr:   sandbox.ResolveHostHerdr,
		up:             sandbox.Up,
		openReady:      sandbox.OpenReadyConnection,
		attach:         sandbox.Attach,
		validateAttach: sandbox.ValidateInteractiveAttachStreams,
		resolvePlan:    sandbox.ResolveEffectivePlan,
		initialize:     sandbox.InitializeProject,
		seedInstaller:  sandbox.SeedInstallerConfiguration,
		cleanInstaller: sandbox.CleanInstallerData,
	}
}

func runWithDependencies(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer, cleanup staleCleanup, inspect sessionInspector) int {
	dependencies := defaultCommandDependencies()
	dependencies.cleanup = cleanup
	dependencies.inspect = inspect
	return runWithCommandDependencies(ctx, args, stdin, stdout, stderr, dependencies)
}

func runWithCommandDependencies(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer, dependencies commandDependencies) int {
	if len(args) == 0 || args[0] == "help" || args[0] == "-h" || args[0] == "--help" {
		fmt.Fprint(stdout, usage)
		return 0
	}
	switch args[0] {
	case "__installer-seed-configuration":
		if len(args) != 1 {
			fmt.Fprintln(stderr, "herdr-sandbox: installer configuration seed does not accept arguments")
			return 2
		}
		if err := dependencies.seedInstaller(); err != nil {
			fmt.Fprintln(stderr, "herdr-sandbox:", err)
			return 1
		}
		return 0
	case "__installer-clean-uninstall":
		deleteConfiguration := false
		if len(args) == 2 && args[1] == "--delete-configuration" {
			deleteConfiguration = true
		} else if len(args) != 1 {
			fmt.Fprintln(stderr, "herdr-sandbox: installer clean uninstall accepts only --delete-configuration")
			return 2
		}
		cleanupContext, cancel := context.WithTimeout(ctx, installerCleanUninstallTimeout)
		defer cancel()
		if err := dependencies.cleanInstaller(cleanupContext, deleteConfiguration); err != nil {
			fmt.Fprintln(stderr, "herdr-sandbox:", err)
			return 1
		}
		return 0
	case "plan":
		if commandHelpRequested(args) {
			fmt.Fprint(stdout, usage)
			return 0
		}
		if len(args) != 1 {
			fmt.Fprintf(stderr, "herdr-sandbox: plan does not accept arguments\n\n%s", usage)
			return 2
		}
		plan, err := dependencies.resolvePlan(ctx, "")
		if err != nil {
			fmt.Fprintln(stderr, "herdr-sandbox:", err)
			return 1
		}
		printEffectivePlan(stdout, plan)
		return 0
	case "init":
		return runInit(args[1:], stdin, stdout, stderr, dependencies.initialize)
	case "attach":
		if commandHelpRequested(args) {
			fmt.Fprint(stdout, usage)
			return 0
		}
		if len(args) != 1 {
			fmt.Fprintf(stderr, "herdr-sandbox: attach does not accept arguments\n\n%s", usage)
			return 2
		}
		if err := dependencies.validateAttach(stdin, stdout, stderr); err != nil {
			fmt.Fprintln(stderr, "herdr-sandbox:", err)
			return 1
		}
		hostHerdr, err := dependencies.resolveHerdr(ctx)
		if err != nil {
			fmt.Fprintln(stderr, "herdr-sandbox:", err)
			return 1
		}
		connection, err := dependencies.openReady(ctx, stdout, hostHerdr)
		if err != nil {
			fmt.Fprintln(stderr, "herdr-sandbox:", err)
			return 1
		}
		return runAttach(ctx, connection, stdin, stdout, stderr, dependencies.attach)
	case "status":
		if commandHelpRequested(args) {
			fmt.Fprint(stdout, usage)
			return 0
		}
		if len(args) != 1 {
			fmt.Fprintf(stderr, "herdr-sandbox: status does not accept arguments\n\n%s", usage)
			return 2
		}
		status, err := dependencies.inspect(ctx)
		if err != nil {
			fmt.Fprintln(stderr, "herdr-sandbox:", err)
			return 1
		}
		printSessionStatus(stdout, status)
		return 0
	case "down":
		if commandHelpRequested(args) {
			fmt.Fprint(stdout, usage)
			return 0
		}
		if len(args) != 1 {
			fmt.Fprintf(stderr, "herdr-sandbox: down does not accept arguments\n\n%s", usage)
			return 2
		}
		if !cleanupBeforeCommand(ctx, stderr, dependencies.cleanup) {
			return 1
		}
		result, err := sandbox.Down(ctx)
		if err != nil {
			fmt.Fprintln(stderr, "herdr-sandbox:", err)
			return 1
		}
		printDownResult(stdout, result)
		return 0
	case "clean":
		if commandHelpRequested(args) {
			fmt.Fprint(stdout, usage)
			return 0
		}
		if len(args) != 1 {
			fmt.Fprintf(stderr, "herdr-sandbox: clean does not accept arguments\n\n%s", usage)
			return 2
		}
		result, err := dependencies.cleanup(ctx)
		if err != nil {
			reportIncompleteCleanup(stderr, result, err)
			return 1
		}
		printCleanResult(stdout, result)
		return 0
	case "up":
	default:
		fmt.Fprintf(stderr, "herdr-sandbox: unknown command %q\n\n%s", args[0], usage)
		return 2
	}

	options := sandbox.DefaultOptions()
	noAttach := false
	flags := flag.NewFlagSet("herdr-sandbox up", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.IntVar(&options.MemoryMB, "memory-mb", options.MemoryMB, "override configured Sandbox memory in MB for this run (minimum 2048)")
	flags.DurationVar(&options.Timeout, "timeout", options.Timeout, "optional launch-to-terminal-ready timeout (no default)")
	flags.BoolVar(&noAttach, "no-attach", false, "leave the verified Sandbox ready without starting the interactive Herdr client")
	flags.Usage = func() {}
	if err := flags.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Fprint(stderr, usage)
			return 0
		}
		fmt.Fprintf(stderr, "herdr-sandbox: %v\n\n%s", err, usage)
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintf(stderr, "herdr-sandbox: unexpected arguments: %s\n\n%s", quotedArguments(flags.Args()), usage)
		return 2
	}
	memoryOverrideSet := false
	timeoutSet := false
	flags.Visit(func(visited *flag.Flag) {
		switch visited.Name {
		case "memory-mb":
			memoryOverrideSet = true
		case "timeout":
			timeoutSet = true
		}
	})
	if memoryOverrideSet && options.MemoryMB < 2048 {
		fmt.Fprintln(stderr, "herdr-sandbox: --memory-mb must be at least 2048")
		return 2
	}
	if timeoutSet && options.Timeout <= 0 {
		fmt.Fprintln(stderr, "herdr-sandbox: --timeout must be positive")
		return 2
	}
	options.Output = stdout
	if !noAttach {
		if err := dependencies.validateAttach(stdin, stdout, stderr); err != nil {
			fmt.Fprintln(stderr, "herdr-sandbox:", err)
			fmt.Fprintln(stderr, "Use `herdr-sandbox up --no-attach` for intentional headless provisioning.")
			return 1
		}
	}
	hostHerdr, err := dependencies.resolveHerdr(ctx)
	if err != nil {
		fmt.Fprintln(stderr, "herdr-sandbox:", err)
		return 1
	}
	if !cleanupBeforeCommand(ctx, stderr, dependencies.cleanup) {
		return 1
	}

	connection, err := dependencies.up(ctx, options, hostHerdr)
	if err != nil {
		fmt.Fprintln(stderr, "herdr-sandbox:", err)
		return 1
	}
	if noAttach {
		fmt.Fprintln(stdout, "Next: run `herdr-sandbox attach` or `herdr --remote sandbox`.")
		return 0
	}
	return runAttach(ctx, connection, stdin, stdout, stderr, dependencies.attach)
}

func commandHelpRequested(args []string) bool {
	return len(args) == 2 && (args[1] == "-h" || args[1] == "--help")
}

func runAttach(ctx context.Context, connection sandbox.Connection, stdin io.Reader, stdout, stderr io.Writer,
	attach func(context.Context, sandbox.Connection, io.Reader, io.Writer, io.Writer) error,
) int {
	fmt.Fprintln(stdout, "Starting the Herdr remote session. Use Herdr's normal detach key to leave the guest running.")
	if err := attach(ctx, connection, stdin, stdout, stderr); err != nil {
		fmt.Fprintln(stderr, "herdr-sandbox:", err)
		return 1
	}
	return 0
}

type stackSelections []string

func (values *stackSelections) String() string { return strings.Join(*values, ",") }
func (values *stackSelections) Set(value string) error {
	*values = append(*values, value)
	return nil
}

func runInit(args []string, stdin io.Reader, stdout, stderr io.Writer,
	initialize func(string, []string) (sandbox.ProjectInitResult, error),
) int {
	flags := flag.NewFlagSet("herdr-sandbox init", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var selected stackSelections
	flags.Var(&selected, "stack", "stack to add; repeat for multiple stacks: dotnet, go, node, python, rust, zig")
	flags.Usage = func() {}
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Fprint(stderr, usage)
			return 0
		}
		fmt.Fprintf(stderr, "herdr-sandbox: %v\n\n%s", err, usage)
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintf(stderr, "herdr-sandbox: unexpected init arguments: %s\n\n%s", quotedArguments(flags.Args()), usage)
		return 2
	}
	if len(selected) == 0 {
		prompted, err := promptForStacks(stdin, stdout)
		if err != nil {
			fmt.Fprintln(stderr, "herdr-sandbox:", err)
			return 1
		}
		selected = prompted
	}
	result, err := initialize("", selected)
	if err != nil {
		fmt.Fprintln(stderr, "herdr-sandbox:", err)
		return 1
	}
	fmt.Fprintln(stdout, "Project profile created")
	fmt.Fprintf(stdout, "  Path: %s\n", result.Path)
	fmt.Fprintln(stdout, "  Stacks:")
	printBulletList(stdout, result.Stacks, "    ")
	fmt.Fprintln(stdout, "\nNext")
	fmt.Fprintln(stdout, "  1. Run `herdr-sandbox plan`.")
	fmt.Fprintln(stdout, "  2. Run `herdr-sandbox up`.")
	return 0
}

func promptForStacks(input io.Reader, output io.Writer) ([]string, error) {
	fmt.Fprintln(output, "Available stacks: dotnet, go, node, python, rust, zig")
	fmt.Fprint(output, "Select one or more stacks (comma or space separated): ")
	reader := bufio.NewReader(io.LimitReader(input, 4097))
	line, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("read stack selection: %w", err)
	}
	if len(line) > 4096 {
		return nil, errors.New("stack selection exceeds 4096 bytes")
	}
	selected := strings.FieldsFunc(line, func(value rune) bool {
		return value == ',' || value == ' ' || value == '\t' || value == '\r' || value == '\n'
	})
	if len(selected) == 0 {
		return nil, errors.New("no stack was selected")
	}
	return selected, nil
}

func cleanupBeforeCommand(ctx context.Context, stderr io.Writer, cleanup staleCleanup) bool {
	if result, err := cleanup(ctx); err != nil {
		reportIncompleteCleanup(stderr, result, err)
		return false
	}
	return true
}

func reportIncompleteCleanup(stderr io.Writer, result sandbox.CleanResult, err error) {
	if result.RemovedRuns > 0 {
		printCleanResult(stderr, result)
	}
	fmt.Fprintln(stderr, "herdr-sandbox: stale-state cleanup incomplete:", err)
}

func printDownResult(output io.Writer, result sandbox.DownResult) {
	fmt.Fprintln(output, "Sandbox")
	if result.AlreadyStopped {
		fmt.Fprintln(output, "  Result: already stopped")
		if result.RunID != "" {
			fmt.Fprintf(output, "  Cleared stale run: %s\n", result.RunID)
		}
		return
	}
	fmt.Fprintln(output, "  Result: stopped")
	fmt.Fprintf(output, "  Run: %s\n", result.RunID)
}

func printCleanResult(output io.Writer, result sandbox.CleanResult) {
	fmt.Fprintln(output, "Cleanup")
	if result.RemovedRuns == 0 {
		fmt.Fprintln(output, "  Removed: no inactive run workspaces")
	} else if result.RemovedRuns == 1 {
		fmt.Fprintln(output, "  Removed: 1 inactive run workspace")
	} else {
		fmt.Fprintf(output, "  Removed: %d inactive run workspaces\n", result.RemovedRuns)
	}
	if result.ActiveRunID != "" {
		fmt.Fprintf(output, "  Preserved active run: %s\n", result.ActiveRunID)
	}
}

func printSessionStatus(output io.Writer, status sandbox.SessionStatus) {
	fmt.Fprintln(output, "Sandbox")
	fmt.Fprintf(output, "  State: %s\n", status.State)
	if status.RunID != "" {
		fmt.Fprintf(output, "  Run: %s\n", status.RunID)
	}
	if status.PID > 0 {
		fmt.Fprintf(output, "  PID: %d\n", status.PID)
	}
	if status.StartedAtUTC != "" {
		fmt.Fprintf(output, "  Started: %s\n", status.StartedAtUTC)
	}
	if status.Phase != "" {
		fmt.Fprintf(output, "  Phase: %s\n", status.Phase)
	}
	if status.Message != "" {
		fmt.Fprintf(output, "  Message: %s\n", status.Message)
	}
	if status.GuestIP != "" {
		fmt.Fprintf(output, "  Guest IP: %s\n", status.GuestIP)
	}
	if status.WinGetVersion != "" {
		fmt.Fprintf(output, "  WinGet: %s\n", status.WinGetVersion)
	}
	attachable := status.State == sandbox.SessionReady &&
		(status.Operation == nil || status.Operation.State != "running")
	if status.HerdrVersion != "" {
		fmt.Fprintf(output, "  Herdr: %s\n", status.HerdrVersion)
		if status.HerdrProtocol > 0 {
			fmt.Fprintf(output, "  Herdr protocol: %d\n", status.HerdrProtocol)
		}
	}
	if attachable {
		fmt.Fprintln(output, "  Attach: herdr --remote sandbox")
	}
	if len(status.Workspaces) > 0 {
		fmt.Fprintln(output, "\nWorkspaces")
	}
	for _, workspace := range status.Workspaces {
		marker := "-"
		suffix := ""
		if workspace.Active {
			marker = "*"
			suffix = " (active)"
		}
		fmt.Fprintf(output, "  %s %s%s\n", marker, workspace.Name, suffix)
		fmt.Fprintf(output, "    Directory: %s\n", workspace.Directory)
	}
	if status.Operation != nil {
		fmt.Fprintln(output, "\nOperation")
		fmt.Fprintf(output, "  Kind: %s\n", status.Operation.Kind)
		fmt.Fprintf(output, "  State: %s\n", status.Operation.State)
		fmt.Fprintf(output, "  Phase: %s\n", status.Operation.Phase)
		fmt.Fprintf(output, "  Message: %s\n", status.Operation.Message)
		fmt.Fprintf(output, "  Updated: %s\n", status.Operation.UpdatedAtUTC)
	}
	if status.DiagnosticsPath != "" {
		fmt.Fprintln(output, "\nDiagnostics")
		fmt.Fprintf(output, "  Path: %s\n", status.DiagnosticsPath)
	}
	if len(status.Timings) > 0 {
		fmt.Fprintln(output, "\nTimings")
		for _, timing := range status.Timings {
			duration := time.Duration(timing.ElapsedMilliseconds) * time.Millisecond
			fmt.Fprintf(output, "  - %s: %s\n", timing.Role, duration)
		}
	}
	if status.CleanupRemovedRuns > 0 {
		fmt.Fprintln(output, "\nCleanup")
		if status.CleanupRemovedRuns == 1 {
			fmt.Fprintln(output, "  Removed: 1 inactive run workspace")
		} else {
			fmt.Fprintf(output, "  Removed: %d inactive run workspaces\n", status.CleanupRemovedRuns)
		}
	}
	if len(status.Warnings) > 0 {
		fmt.Fprintln(output, "\nWarnings")
		printBulletList(output, status.Warnings, "  ")
	}
	if len(status.Processes) > 0 {
		fmt.Fprintln(output, "\nProcesses")
		printBulletList(output, sortedFold(status.Processes), "  ")
	}
	if status.NextAction != "" {
		fmt.Fprintf(output, "\nNext: %s\n", status.NextAction)
	}
}

func printEffectivePlan(output io.Writer, plan sandbox.EffectivePlan) {
	configurationState := "defaults; file is not created by plan"
	if plan.ConfigurationExists {
		configurationState = "existing"
	}
	userState := "default; file is not created by plan"
	if plan.UserScriptExists {
		userState = "existing"
	}
	fmt.Fprintln(output, "Effective plan")
	fmt.Fprintln(output, "\nConfiguration")
	fmt.Fprintf(output, "  File: %s\n", plan.ConfigurationPath)
	fmt.Fprintf(output, "  File state: %s\n", configurationState)
	fmt.Fprintf(output, "  User script: %s\n", plan.UserScriptPath)
	fmt.Fprintf(output, "  User script state: %s\n", userState)
	fmt.Fprintf(output, "  Cache: %s\n", plan.CacheDirectory)
	fmt.Fprintf(output, "  Memory: %d MB\n", plan.MemoryMB)
	fmt.Fprintf(output, "  Audio: %s\n", enabledDisabled(plan.Audio))
	fmt.Fprintf(output, "  Tailscale: %s\n", enabledDisabled(plan.Tailscale))
	fmt.Fprintf(output, "  Windows Terminal: %s\n", plan.WindowsTerminal)

	fmt.Fprintln(output, "\nCoding agents")
	printBulletList(output, sortedFold(plan.CodingAgents), "  ")
	fmt.Fprintln(output, "\nGlobal stacks")
	printBulletList(output, sortedFold(plan.GlobalStacks), "  ")

	fmt.Fprintln(output, "\nPackages")
	if len(plan.Packages) == 0 {
		fmt.Fprintln(output, "  (none)")
	}
	for _, entry := range plan.Packages {
		fmt.Fprintf(output, "  - %s\n", entry.ID)
		fmt.Fprintf(output, "    Version: %s\n", entry.Version)
		fmt.Fprintf(output, "    Source: %s\n", entry.Source)
	}
	if len(plan.StackPackages) > 0 {
		fmt.Fprintln(output, "\nStack package owners")
		for _, entry := range plan.StackPackages {
			fmt.Fprintf(output, "  - %s: %s\n", entry.Stack, entry.PackageOwner)
		}
	}

	fmt.Fprintln(output, "\nWorkspaces")
	if len(plan.Workspaces) == 0 {
		fmt.Fprintln(output, "  (none)")
	}
	for _, workspace := range plan.Workspaces {
		marker := "-"
		suffix := ""
		if workspace.Active {
			marker = "*"
			suffix = " (active)"
		}
		fmt.Fprintf(output, "  %s %s%s\n", marker, workspace.Name, suffix)
		fmt.Fprintf(output, "    Host: %s\n", workspace.HostDirectory)
		fmt.Fprintf(output, "    Guest: %s\n", workspace.GuestDirectory)
		if len(workspace.Stacks) > 0 {
			fmt.Fprintln(output, "    Stacks:")
			printBulletList(output, sortedFold(workspace.Stacks), "      ")
		}
	}
	if plan.RequiresVisualStudio {
		fmt.Fprintln(output, "\nHost preparation")
		fmt.Fprintln(output, "  - Visual Studio Build Tools layout required")
	}
	if len(plan.ReadyChanges) > 0 {
		fmt.Fprintln(output, "\nReady Sandbox changes")
		printBulletList(output, plan.ReadyChanges, "  ")
	}
	fmt.Fprintf(output, "\nNext: %s\n", plan.NextAction)
}

func quotedArguments(arguments []string) string {
	quoted := make([]string, len(arguments))
	for index, argument := range arguments {
		quoted[index] = strconv.Quote(argument)
	}
	return strings.Join(quoted, ", ")
}

func printBulletList(output io.Writer, values []string, indent string) {
	if len(values) == 0 {
		fmt.Fprintf(output, "%s(none)\n", indent)
		return
	}
	for _, value := range values {
		fmt.Fprintf(output, "%s- %s\n", indent, value)
	}
}

func sortedFold(values []string) []string {
	result := append([]string(nil), values...)
	sort.Slice(result, func(left, right int) bool {
		leftFold := strings.ToLower(result[left])
		rightFold := strings.ToLower(result[right])
		if leftFold == rightFold {
			return result[left] < result[right]
		}
		return leftFold < rightFold
	})
	return result
}

func enabledDisabled(value bool) string {
	if value {
		return "enabled"
	}
	return "disabled"
}
