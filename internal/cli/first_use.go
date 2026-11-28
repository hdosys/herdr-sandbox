package cli

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strings"

	"herdr-sandbox/internal/sandbox"
)

func prepareFirstUse(ctx context.Context, options *sandbox.Options, stacks []string, remote, yes bool, input io.Reader, output, stderr io.Writer, deps commandDependencies) bool {
	reader := bufio.NewReaderSize(input, 8192)
	selection, err := deps.firstUse(options.ProjectDirectory)
	if err != nil {
		fmt.Fprintln(stderr, "sandbox: configuration was not changed:", err)
		return false
	}
	if !selection.NeedsProject {
		if len(stacks) > 0 {
			fmt.Fprintln(stderr, "sandbox: a project is already selected; its profile was not overwritten")
			return false
		}
		if remote && !selection.RemoteEnabled {
			_, saved, err := deps.changeRemote(ctx, sandbox.RemoteChange{Action: "enable"}, func(sandbox.RemoteSettings) bool {
				if yes {
					return true
				}
				approved, readErr := confirmPrompt(reader, output, "Enable Tailscale and the managing host's remote shell access?")
				return readErr == nil && approved
			})
			if err != nil || !saved {
				fmt.Fprintln(stderr, "sandbox: use sandbox remote to approve remote access, or --remote-access --yes for an explicit initial configuration")
				return false
			}
		}
		return true
	}
	if !deps.interactive(input) && (len(stacks) == 0 || !yes) {
		fmt.Fprintln(stderr, "sandbox: first use needs an approved project and stack. Run sandbox up interactively, or sandbox up --project PATH --stack NAME --yes --no-attach.")
		return false
	}
	if deps.interactive(input) {
		folder, err := readPrompt(reader, output, "Project folder ["+selection.Directory+"]: ", 4096)
		if err != nil {
			fmt.Fprintln(stderr, "sandbox:", err)
			return false
		}
		if folder != "" {
			selection.Directory = folder
		}
		if len(stacks) == 0 {
			fmt.Fprintln(output, "Available stacks:", stackSelectionHelp)
			line, err := readPrompt(reader, output, "Required stacks (space or comma separated): ", 1024)
			if err != nil {
				fmt.Fprintln(stderr, "sandbox:", err)
				return false
			}
			stacks = strings.Fields(strings.ReplaceAll(line, ",", " "))
		}
		if !remote && !selection.RemoteEnabled {
			remote, err = confirmPrompt(reader, output, "Enable remote access through Tailscale? Browser sign-in may be required")
			if err != nil {
				fmt.Fprintln(stderr, "sandbox:", err)
				return false
			}
		}
	}
	if len(stacks) == 0 {
		fmt.Fprintln(stderr, "sandbox: choose at least one stack; nothing changed")
		return false
	}
	fmt.Fprintf(output, "Share this exact project read/write: %s\nTools: %s\nHost-file changes persist even after discarding the guest.\n", selection.Directory, strings.Join(stacks, ", "))
	approved := yes
	if !approved {
		approved, err = confirmPrompt(reader, output, "Create this project profile and start setup?")
		if err != nil {
			fmt.Fprintln(stderr, "sandbox:", err)
			return false
		}
	}
	if !approved {
		fmt.Fprintln(output, "Cancelled. No project profile or access setting was changed.")
		return false
	}
	if err := ctx.Err(); err != nil {
		fmt.Fprintln(stderr, "sandbox:", err)
		return false
	}
	result, err := deps.initialize(selection.Directory, stacks)
	if err != nil {
		fmt.Fprintln(stderr, "sandbox:", err)
		return false
	}
	fmt.Fprintln(output, "Created project profile:", result.Path)
	options.ProjectDirectory = selection.Directory
	if remote && !selection.RemoteEnabled {
		_, _, err := deps.changeRemote(ctx, sandbox.RemoteChange{Action: "enable"}, func(sandbox.RemoteSettings) bool { return true })
		if err != nil {
			fmt.Fprintln(stderr, "sandbox: project profile saved, but remote access configuration failed:", err)
			return false
		}
	}
	return true
}
