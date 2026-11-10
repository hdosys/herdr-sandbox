package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"time"

	"herdr-sandbox/internal/sandbox"
)

const workstationUsage = `Usage:
  sandbox workstation create NAME
  sandbox workstation build NAME
  sandbox workstation test NAME
  sandbox workstation test --installer FILE
  sandbox workstation setup [--profile FILE] [--root DIRECTORY] [--refresh]
  sandbox workstation export --output FILE [--include-settings] [--defaults]

create captures the currently selected installation and personal configuration.
build turns that saved snapshot into a private installer using the installed payload
and NSIS compiler. No source checkout, Go, Git build, release tag or version argument
is needed. Saved installations live below %LOCALAPPDATA%\Herdr Installations.
test runs that exact installer in a fresh Sandbox from the Windows host. Existing
guests are preserved; use sandbox down separately only after saving their work.
After a successful test, sandbox attach opens the terminal.
For an installer copied from another computer, use test --installer FILE and keep
its matching JSON sidecar beside it. No separate profile is needed for that test.

setup provisions Windows x64 build 19041 or newer without creating a Sandbox.
Windows 11 is recommended. Setup state stays below ProgramData.
Run elevated as the intended Windows user. Independent steps continue after errors;
the final report and nonzero exit distinguish incomplete setup from success.
Re-run to resume successful unchanged work; --refresh intentionally reruns it.
The total safety deadline includes every selected step's bounded execution budget.

export reads the existing Sandbox tool selection without executing project scripts.
--defaults selects the generic full development profile instead.
--include-settings explicitly includes private authored configuration, never dedicated
auth files, SSH keys, project trees or Git history. Inline private values may remain:
keep the exported file private. create and build manage this snapshot automatically;
normal installation needs no separately transferred profile.
Existing different target configuration is preserved and reported, not overwritten.
`

func runWorkstation(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		fmt.Fprint(stdout, workstationUsage)
		return 0
	}
	if args[0] == "create" || args[0] == "build" {
		if len(args) != 2 {
			fmt.Fprint(stderr, workstationUsage)
			return 2
		}
		operation, cancel := context.WithTimeout(ctx, 10*time.Minute)
		defer cancel()
		var err error
		if args[0] == "create" {
			_, err = sandbox.CreateWorkstationInstallation(operation, args[1], stdout)
		} else {
			_, err = sandbox.BuildSavedWorkstation(operation, args[1], stdout, stderr)
		}
		if err != nil {
			fmt.Fprintln(stderr, "workstation:", err)
			return 1
		}
		return 0
	}
	flags := flag.NewFlagSet("workstation "+args[0], flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.Usage = func() { fmt.Fprint(stderr, workstationUsage) }
	var execute func() error
	failureCode := 1
	switch args[0] {
	case "root":
		execute = func() error {
			root, err := sandbox.WorkstationStateRoot()
			if err == nil {
				fmt.Fprintln(stdout, root)
			}
			return err
		}
	case "setup":
		options := sandbox.WorkstationOptions{Output: stdout}
		flags.StringVar(&options.Profile, "profile", "", "explicit portable profile; otherwise generic development setup")
		flags.StringVar(&options.Root, "root", "", "owned local installation-state root")
		flags.BoolVar(&options.Refresh, "refresh", false, "rerun completed steps intentionally")
		flags.StringVar(&options.Inputs, "inputs", "", "materialized installer data archive")
		flags.StringVar(&options.InputsSHA256, "inputs-sha256", "", "installer-bound data SHA-256")
		execute = func() error {
			report, err := sandbox.SetupWorkstation(ctx, options)
			if err != nil && report == "" {
				failureCode = 2
			}
			return err
		}
	case "export":
		var destination string
		var includeSettings, defaults bool
		flags.StringVar(&destination, "output", "", "new private export file, never overwritten")
		flags.BoolVar(&includeSettings, "include-settings", false, "explicitly include authored private configuration")
		flags.BoolVar(&defaults, "defaults", false, "export generic tools instead of the existing Sandbox selection")
		execute = func() error {
			if destination == "" {
				return errors.New("export requires --output")
			}
			exportContext, cancel := context.WithTimeout(ctx, 2*time.Minute)
			defer cancel()
			return sandbox.ExportWorkstationProfile(exportContext, destination, includeSettings, defaults, stdout)
		}
	case "prepare-tts":
		var root string
		flags.StringVar(&root, "root", "", "existing owned workstation root")
		execute = func() error {
			if root == "" {
				return errors.New("internal TTS preparation requires --root")
			}
			prepareContext, cancel := context.WithTimeout(ctx, 30*time.Minute)
			defer cancel()
			return sandbox.PrepareWorkstationTTS(prepareContext, root, stdout)
		}
	case "inspect-download":
		var selection sandbox.DownloadedPackageSelection
		flags.StringVar(&selection.Directory, "directory", "", "owned WinGet download directory")
		flags.StringVar(&selection.ID, "id", "", "selected package identifier")
		flags.StringVar(&selection.Version, "version", "", "explicit selected version")
		flags.StringVar(&selection.Architecture, "architecture", "x64", "selected architecture")
		flags.StringVar(&selection.InstallerType, "type", "", "selected installer type")
		flags.StringVar(&selection.Scope, "scope", "", "selected installer scope")
		flags.StringVar(&selection.PayloadExtension, "extension", "", "explicit payload extension")
		execute = func() error { return sandbox.InspectDownloadedPackage(selection, stdout) }
	default:
		fmt.Fprint(stderr, workstationUsage)
		return 2
	}
	if err := flags.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(stderr, "unexpected workstation arguments")
		return 2
	}
	if err := execute(); err != nil {
		fmt.Fprintln(stderr, "workstation:", err)
		return failureCode
	}
	return 0
}
