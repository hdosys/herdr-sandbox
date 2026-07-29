package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"herdr-sandbox/internal/sandbox"
)

const usage = `Usage:
  herdr-sandbox up [--memory-mb MB] [--timeout DURATION]
  herdr-sandbox status
  herdr-sandbox down
  herdr-sandbox clean

Commands:
  up      launch fresh or re-provision the exact ready Sandbox, then attach
  status  report the app-owned Sandbox without changing it
  down    stop only the exact app-owned Sandbox
  clean   remove inactive app-owned run workspaces

Explicit workspaces, optional workspaceDiscovery, absolute cacheDirectory (default <system-temp>\herdr-sandbox\cache), memoryMB (default 32768), codingAgentSync choices, and wingetPackages add/remove/version choices come from %APPDATA%\herdr-sandbox\config.json.
The up command has no overall timeout unless --timeout is supplied.
The nearest .herdr-sandbox\provision.ps1, when present, becomes the active workspace.
`

func Run(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	return runWithDependencies(ctx, args, stdin, stdout, stderr, sandbox.CleanupStaleState, sandbox.InspectSession)
}

type staleCleanup func(context.Context) (sandbox.CleanResult, error)
type sessionInspector func(context.Context) (sandbox.SessionStatus, error)

func runWithDependencies(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer, cleanup staleCleanup, inspect sessionInspector) int {
	if len(args) == 0 || args[0] == "help" || args[0] == "-h" || args[0] == "--help" {
		fmt.Fprint(stdout, usage)
		return 0
	}
	switch args[0] {
	case "status":
		if len(args) != 1 {
			fmt.Fprintf(stderr, "herdr-sandbox: status does not accept arguments\n\n%s", usage)
			return 2
		}
		if result, err := cleanup(ctx); err != nil {
			reportIncompleteCleanup(stderr, result, err)
		}
		status, err := inspect(ctx)
		if err != nil {
			fmt.Fprintln(stderr, "herdr-sandbox:", err)
			return 1
		}
		printSessionStatus(stdout, status)
		return 0
	case "down":
		if len(args) != 1 {
			fmt.Fprintf(stderr, "herdr-sandbox: down does not accept arguments\n\n%s", usage)
			return 2
		}
		if !cleanupBeforeCommand(ctx, stderr, cleanup) {
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
		if len(args) != 1 {
			fmt.Fprintf(stderr, "herdr-sandbox: clean does not accept arguments\n\n%s", usage)
			return 2
		}
		result, err := cleanup(ctx)
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
	flags := flag.NewFlagSet("herdr-sandbox up", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.IntVar(&options.MemoryMB, "memory-mb", options.MemoryMB, "override configured Sandbox memory in MB for this run (minimum 2048)")
	flags.DurationVar(&options.Timeout, "timeout", options.Timeout, "optional launch-to-terminal-ready timeout (no default)")
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
	if !cleanupBeforeCommand(ctx, stderr, cleanup) {
		return 1
	}

	connection, err := sandbox.Up(ctx, options)
	if err != nil {
		fmt.Fprintln(stderr, "herdr-sandbox:", err)
		return 1
	}
	fmt.Fprintln(stdout, "Attaching the host Herdr client to the persistent guest server. Detach with Herdr's normal detach key.")
	if err := sandbox.Attach(ctx, connection, stdin, stdout, stderr); err != nil {
		fmt.Fprintln(stderr, "herdr-sandbox:", err)
		return 1
	}
	return 0
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
	if status.Phase != "" {
		fmt.Fprintf(output, "phase: %s\n", status.Phase)
	}
	if status.Message != "" {
		fmt.Fprintf(output, "message: %s\n", status.Message)
	}
	if status.GuestIP != "" {
		fmt.Fprintf(output, "ip: %s\n", status.GuestIP)
	}
	if status.HerdrVersion != "" {
		fmt.Fprintf(output, "herdr: %s\n", status.HerdrVersion)
		fmt.Fprintln(output, "attach: herdr --remote sandbox")
	}
	if len(status.Processes) > 0 {
		fmt.Fprintf(output, "processes: %s\n", strings.Join(status.Processes, ", "))
	}
}
