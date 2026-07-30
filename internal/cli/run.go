package cli

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"herdr-sandbox/internal/sandbox"
)

const usage = `Usage:
  herdr-sandbox plan
  herdr-sandbox init [--stack go|node|python|rust|zig|dotnet]...
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

Explicit workspaces, optional workspaceDiscovery, absolute cacheDirectory (default <system-temp>\herdr-sandbox\cache), memoryMB (default 32768), audio, tailscale, codingAgentSync choices, and wingetPackages add/remove/version choices come from %APPDATA%\herdr-sandbox\config.json.
The up command has no overall timeout unless --timeout is supplied.
The nearest .herdr-sandbox\provision.ps1, when present, becomes the active workspace.
`

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
	flags.SetOutput(stderr)
	flags.IntVar(&options.MemoryMB, "memory-mb", options.MemoryMB, "override configured Sandbox memory in MB for this run (minimum 2048)")
	flags.DurationVar(&options.Timeout, "timeout", options.Timeout, "optional launch-to-terminal-ready timeout (no default)")
	flags.BoolVar(&noAttach, "no-attach", false, "leave the verified Sandbox ready without starting the interactive Herdr client")
	flags.Usage = func() { fmt.Fprint(stderr, usage) }
	if err := flags.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintf(stderr, "herdr-sandbox: unexpected arguments: %v\n\n%s", flags.Args(), usage)
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
		fmt.Fprintln(stdout, "Sandbox is ready. Attach later with `herdr-sandbox attach` or `herdr --remote sandbox`.")
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
	fmt.Fprintln(stdout, "Attaching the host Herdr client to the persistent guest server. Detach with Herdr's normal detach key.")
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
	flags.SetOutput(stderr)
	var selected stackSelections
	flags.Var(&selected, "stack", "stack to add; repeat for multiple stacks: go, node, python, rust, zig, dotnet")
	flags.Usage = func() { fmt.Fprint(stderr, usage) }
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintf(stderr, "herdr-sandbox: unexpected init arguments: %v\n\n%s", flags.Args(), usage)
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
	fmt.Fprintf(stdout, "Created project profile: %s\n", result.Path)
	fmt.Fprintf(stdout, "Stacks: %s\n", strings.Join(result.Stacks, ", "))
	fmt.Fprintln(stdout, "Next: run `herdr-sandbox plan`, then `herdr-sandbox up`.")
	return 0
}

func promptForStacks(input io.Reader, output io.Writer) ([]string, error) {
	fmt.Fprintln(output, "Available stacks: go, node, python, rust, zig, dotnet")
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
	if result.AlreadyStopped {
		if result.RunID == "" {
			fmt.Fprintln(output, "herdr-sandbox: already stopped")
		} else {
			fmt.Fprintf(output, "herdr-sandbox: already stopped (stale run %s cleared)\n", result.RunID)
		}
		return
	}
	fmt.Fprintf(output, "herdr-sandbox: stopped run %s\n", result.RunID)
}

func printCleanResult(output io.Writer, result sandbox.CleanResult) {
	if result.RemovedRuns == 0 {
		fmt.Fprint(output, "herdr-sandbox: no inactive run workspaces")
	} else if result.RemovedRuns == 1 {
		fmt.Fprint(output, "herdr-sandbox: removed 1 inactive run workspace")
	} else {
		fmt.Fprintf(output, "herdr-sandbox: removed %d inactive run workspaces", result.RemovedRuns)
	}
	if result.ActiveRunID != "" {
		fmt.Fprintf(output, "; preserved active run %s", result.ActiveRunID)
	}
	fmt.Fprintln(output)
}

func printSessionStatus(output io.Writer, status sandbox.SessionStatus) {
	fmt.Fprintf(output, "state: %s\n", status.State)
	if status.RunID != "" {
		fmt.Fprintf(output, "run: %s\n", status.RunID)
	}
	if status.PID > 0 {
		fmt.Fprintf(output, "pid: %d\n", status.PID)
	}
	if status.StartedAtUTC != "" {
		fmt.Fprintf(output, "started: %s\n", status.StartedAtUTC)
	}
	if status.Phase != "" {
		fmt.Fprintf(output, "phase: %s\n", status.Phase)
	}
	if status.Message != "" {
		fmt.Fprintf(output, "message: %s\n", status.Message)
	}
	if status.GuestIP != "" {
		fmt.Fprintf(output, "ip: %s\n", status.GuestIP)
	}
	if status.WinGetVersion != "" {
		fmt.Fprintf(output, "winget: %s\n", status.WinGetVersion)
	}
	attachable := status.State == sandbox.SessionReady &&
		(status.Operation == nil || status.Operation.State != "running")
	if status.HerdrVersion != "" {
		fmt.Fprintf(output, "herdr: %s\n", status.HerdrVersion)
		if status.HerdrProtocol > 0 {
			fmt.Fprintf(output, "herdr-protocol: %d\n", status.HerdrProtocol)
		}
	}
	if attachable {
		fmt.Fprintln(output, "attach: herdr --remote sandbox")
	}
	for _, workspace := range status.Workspaces {
		marker := " "
		if workspace.Active {
			marker = "*"
		}
		fmt.Fprintf(output, "workspace: %s %s -> %s\n", marker, workspace.Name, workspace.Directory)
	}
	if status.Operation != nil {
		fmt.Fprintf(output, "operation: %s %s\n", status.Operation.Kind, status.Operation.State)
		fmt.Fprintf(output, "operation-phase: %s\n", status.Operation.Phase)
		fmt.Fprintf(output, "operation-message: %s\n", status.Operation.Message)
		fmt.Fprintf(output, "operation-updated: %s\n", status.Operation.UpdatedAtUTC)
	}
	if status.DiagnosticsPath != "" {
		fmt.Fprintf(output, "diagnostics: %s\n", status.DiagnosticsPath)
	}
	for _, timing := range status.Timings {
		duration := time.Duration(timing.ElapsedMilliseconds) * time.Millisecond
		fmt.Fprintf(output, "timing: %s = %s\n", timing.Role, duration)
	}
	if status.CleanupRemovedRuns > 0 {
		fmt.Fprintf(output, "cleanup: removed %d inactive run workspace(s)\n", status.CleanupRemovedRuns)
	}
	for _, warning := range status.Warnings {
		fmt.Fprintf(output, "warning: %s\n", warning)
	}
	if len(status.Processes) > 0 {
		fmt.Fprintf(output, "processes: %s\n", strings.Join(status.Processes, ", "))
	}
	if status.NextAction != "" {
		fmt.Fprintf(output, "next: %s\n", status.NextAction)
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
	fmt.Fprintf(output, "configuration: %s (%s)\n", plan.ConfigurationPath, configurationState)
	fmt.Fprintf(output, "user-script: %s (%s)\n", plan.UserScriptPath, userState)
	fmt.Fprintf(output, "cache: %s\n", plan.CacheDirectory)
	fmt.Fprintf(output, "memory-mb: %d\n", plan.MemoryMB)
	fmt.Fprintf(output, "audio: %t\n", plan.Audio)
	fmt.Fprintf(output, "tailscale: %t\n", plan.Tailscale)
	fmt.Fprintf(output, "windows-terminal: %s\n", plan.WindowsTerminal)
	if len(plan.CodingAgents) == 0 {
		fmt.Fprintln(output, "coding-agents: none")
	} else {
		fmt.Fprintf(output, "coding-agents: %s\n", strings.Join(plan.CodingAgents, ", "))
	}
	if len(plan.GlobalStacks) == 0 {
		fmt.Fprintln(output, "global-stacks: none")
	} else {
		fmt.Fprintf(output, "global-stacks: %s\n", strings.Join(plan.GlobalStacks, ", "))
	}
	for _, entry := range plan.Packages {
		fmt.Fprintf(output, "package: %s (%s; %s)\n", entry.ID, entry.Version, entry.Source)
	}
	for _, entry := range plan.StackPackages {
		fmt.Fprintf(output, "stack-package: %s -> %s\n", entry.Stack, entry.PackageOwner)
	}
	if len(plan.Workspaces) == 0 {
		fmt.Fprintln(output, "workspaces: none")
	}
	for _, workspace := range plan.Workspaces {
		marker := " "
		if workspace.Active {
			marker = "*"
		}
		fmt.Fprintf(output, "workspace: %s %s: %s -> %s", marker, workspace.Name, workspace.HostDirectory, workspace.GuestDirectory)
		if len(workspace.Stacks) > 0 {
			fmt.Fprintf(output, " [%s]", strings.Join(workspace.Stacks, ", "))
		}
		fmt.Fprintln(output)
	}
	if plan.RequiresVisualStudio {
		fmt.Fprintln(output, "host-preparation: Visual Studio Build Tools layout required")
	}
	if len(plan.ReadyChanges) > 0 {
		fmt.Fprintf(output, "ready-sandbox-changes: %s\n", strings.Join(plan.ReadyChanges, ", "))
	}
	fmt.Fprintf(output, "next: %s\n", plan.NextAction)
}
