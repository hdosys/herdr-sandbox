package cli

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"herdr-sandbox/internal/sandbox"
)

const remoteUsage = `Usage: sandbox remote [--qr]
       sandbox remote add --name NAME [--yes]          (paste one public key on stdin)
       sandbox remote rename --device NAME --name NEW [--yes]
       sandbox remote replace --device NAME [--name NEW] [--yes] (public key on stdin)
       sandbox remote remove --device NAME [--yes]
       sandbox remote enable [--yes]
       sandbox remote apply

Without arguments, show saved devices and offer a menu in an interactive terminal.
Every approved change is saved on this host and applied to a running tailnet Sandbox.
Private keys stay on their devices. Removing a key blocks new logins, not existing sessions.
`

func interactiveInput(input io.Reader) bool {
	file, ok := input.(*os.File)
	if !ok {
		return false
	}
	info, err := file.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

func readPrompt(input *bufio.Reader, output io.Writer, prompt string, maximum int) (string, error) {
	fmt.Fprint(output, prompt)
	line, err := input.ReadSlice('\n')
	if len(line) > maximum || errors.Is(err, bufio.ErrBufferFull) {
		return "", errors.New("input is too long")
	}
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	return strings.TrimSpace(string(line)), nil
}

func confirmPrompt(input *bufio.Reader, output io.Writer, prompt string) (bool, error) {
	line, err := readPrompt(input, output, prompt+" [y/N]: ", 32)
	return err == nil && (strings.EqualFold(line, "y") || strings.EqualFold(line, "yes")), err
}

func printRemoteDevices(output io.Writer, settings sandbox.RemoteSettings) {
	if settings.Enabled {
		fmt.Fprintln(output, "Remote access: enabled in host configuration")
	} else {
		fmt.Fprintln(output, "Remote access: not enabled")
	}
	fmt.Fprintln(output, "Device keys are stored on this host for future Sandboxes. Network reachability is not implied.")
	for i, device := range settings.Devices {
		name := device.Name
		if name == "" {
			name = "(unnamed device)"
		}
		if device.Managed {
			name += " (reserved managing-host identity)"
		}
		state := "saved for future starts; not confirmed in current run"
		if device.Applied {
			state = "applied to current run"
		}
		fmt.Fprintf(output, "  %d. %s: %s\n     %s\n", i+1, name, state, device.Fingerprint)
	}
	if len(settings.Devices) == 0 {
		fmt.Fprintln(output, "  No additional devices. The managing host keeps its own reserved key.")
	}
	if settings.LiveError != "" {
		fmt.Fprintln(output, "Current guest state unavailable:", settings.LiveError)
	}
	if settings.UnconfirmedRemovals > 0 {
		fmt.Fprintf(output, "Warning: removal of %d previously applied key(s) has not been confirmed. Run sandbox remote apply.\n", settings.UnconfirmedRemovals)
	}
	if settings.Session.State == sandbox.SessionReady && settings.Session.RemoteAccess != nil {
		if err := printRemoteAccess(output, *settings.Session.RemoteAccess); err != nil {
			fmt.Fprintln(output, "Connection details unavailable:", err)
		}
	}
}

func runRemote(ctx context.Context, args []string, input io.Reader, output, stderr io.Writer, deps commandDependencies) int {
	if commandHelpRequested(args) {
		fmt.Fprint(output, remoteUsage)
		return 0
	}
	settings, err := deps.remoteSettings(ctx)
	if err != nil {
		fmt.Fprintln(stderr, "sandbox:", err)
		return 1
	}
	reader := bufio.NewReaderSize(input, 8192)
	action := ""
	menu := len(args) == 0 && deps.interactive(input)
	if len(args) == 0 || len(args) == 1 && args[0] == "--qr" {
		printRemoteDevices(output, settings)
		if len(args) == 1 {
			if settings.Session.State != sandbox.SessionReady || settings.Session.RemoteAccess == nil {
				fmt.Fprintln(stderr, "sandbox: no current remote endpoint; use sandbox up after enabling remote access")
				return 1
			}
			lines, err := settings.Session.RemoteAccess.QRLines()
			if err != nil {
				fmt.Fprintln(stderr, "sandbox:", err)
				return 1
			}
			for _, line := range lines {
				fmt.Fprintln(output, strings.ReplaceAll(line, "##", "██"))
			}
			return 0
		}
		if !menu {
			if settings.LiveError != "" {
				return 1
			}
			return 0
		}
		fmt.Fprintln(output, "\n1 Add device   2 Rename   3 Replace key   4 Remove   5 Apply saved access   6 Enable remote access   0 Back")
		choice, err := readPrompt(reader, output, "Action: ", 16)
		if err != nil {
			fmt.Fprintln(stderr, "sandbox:", err)
			return 1
		}
		switch choice {
		case "", "0":
			return 0
		case "1":
			action = "add"
		case "2":
			action = "rename"
		case "3":
			action = "replace"
		case "4":
			action = "remove"
		case "5":
			action = "apply"
		case "6":
			action = "enable"
		default:
			fmt.Fprintln(stderr, "sandbox: choose one of the displayed actions")
			return 2
		}
	} else {
		action, args = args[0], args[1:]
	}
	change := sandbox.RemoteChange{Action: action}
	flags := flag.NewFlagSet("sandbox remote "+action, flag.ContinueOnError)
	flags.SetOutput(stderr)
	yes := flags.Bool("yes", false, "approve the displayed access change")
	flags.StringVar(&change.Name, "name", "", "device display name")
	flags.StringVar(&change.Device, "device", "", "existing device name or fingerprint")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(stderr, "sandbox: unexpected remote arguments")
		return 2
	}
	if action != "add" && action != "rename" && action != "replace" && action != "remove" && action != "enable" && action != "apply" {
		fmt.Fprint(stderr, remoteUsage)
		return 2
	}
	if action == "apply" {
		return applyRemoteSettings(ctx, settings, output, stderr, deps)
	}
	if action == "rename" || action == "replace" || action == "remove" {
		if change.Device == "" && menu {
			choice, err := readPrompt(reader, output, "Device number: ", 16)
			if err != nil {
				fmt.Fprintln(stderr, "sandbox:", err)
				return 1
			}
			index, err := strconv.Atoi(choice)
			if err != nil || index < 1 || index > len(settings.Devices) {
				fmt.Fprintln(stderr, "sandbox: choose a listed device")
				return 2
			}
			change.Device = settings.Devices[index-1].Fingerprint
		}
		var selected *sandbox.RemoteDevice
		for i := range settings.Devices {
			device := &settings.Devices[i]
			if change.Device == device.Fingerprint || device.Name != "" && strings.EqualFold(change.Device, device.Name) {
				selected = device
				break
			}
		}
		if selected == nil {
			fmt.Fprintln(stderr, "sandbox: device not found; see sandbox remote")
			return 2
		}
		if selected.Managed {
			fmt.Fprintln(stderr, "sandbox: the managing host identity cannot be edited as a device")
			return 2
		}
		change.Device = selected.Fingerprint
		if action == "replace" && change.Name == "" {
			change.Name = selected.Name
		}
	}
	if action == "add" || action == "rename" || action == "replace" {
		if change.Name == "" && menu {
			change.Name, err = readPrompt(reader, output, "Device name: ", 80)
		}
		if err != nil || change.Name == "" {
			fmt.Fprintln(stderr, "sandbox: a device name is required (--name)")
			return 2
		}
	}
	if action == "add" || action == "replace" {
		if menu {
			fmt.Fprintln(output, "On the OTHER device, copy the public .pub key, not its private key.")
			fmt.Fprintln(output, "Windows PowerShell: Get-Content \"$HOME/.ssh/id_ed25519.pub\" | Set-Clipboard")
			fmt.Fprintln(output, "macOS/Linux: cat ~/.ssh/id_ed25519.pub   (copy the displayed line)")
			fmt.Fprintln(output, "If no key exists, run ssh-keygen -t ed25519 there first. Never overwrite an existing private key.")
		}
		change.PublicKey, err = readPrompt(reader, output, "Paste one public SSH key: ", 4098)
		if err != nil {
			fmt.Fprintln(stderr, "sandbox:", err)
			return 1
		}
	}
	if action == "remove" {
		fmt.Fprintln(output, "Removal blocks new logins. Existing SSH sessions are NOT disconnected.")
	}
	var confirmationErr error
	after, saved, err := deps.changeRemote(ctx, change, func(preview sandbox.RemoteSettings) bool {
		fmt.Fprintf(output, "Save %s permanently on this host and apply to the running Sandbox.\n", action)
		if change.Device != "" {
			fmt.Fprintln(output, "Selected device key:", change.Device)
		}
		for _, device := range preview.Devices {
			fmt.Fprintf(output, "  %s: %s\n", device.Name, device.Fingerprint)
		}
		fmt.Fprintln(output, "Approved devices receive shell and TCP-forwarding access, subject to tailnet policy.")
		if *yes {
			return true
		}
		approved, err := confirmPrompt(reader, output, "Confirm access change?")
		confirmationErr = err
		return approved
	})
	if confirmationErr != nil {
		fmt.Fprintln(stderr, "sandbox:", confirmationErr)
		return 1
	}
	if err != nil {
		fmt.Fprintln(stderr, "sandbox:", err)
		return 1
	}
	if !saved {
		fmt.Fprintln(output, "Cancelled. No settings or guest access were changed.")
		return 0
	}
	return applyRemoteSettings(ctx, after, output, stderr, deps)
}

func applyRemoteSettings(ctx context.Context, settings sandbox.RemoteSettings, output, stderr io.Writer, deps commandDependencies) int {
	fmt.Fprintln(output, "Saved on this host for future Sandboxes. Applying to the current guest...")
	applied, err := deps.applyRemoteKeys(ctx, settings.AuthorizedKeys)
	if err != nil {
		fmt.Fprintln(stderr, "sandbox: saved, but live application was NOT confirmed:", err)
		fmt.Fprintln(stderr, "Previously applied access may still be active. Resolve the condition, then run sandbox remote apply.")
		return 1
	}
	if applied {
		fmt.Fprintln(output, "Applied and verified in the running Sandbox. No restart or reprovisioning.")
	} else {
		fmt.Fprintln(output, "No managed guest. Access will apply automatically at the next sandbox up.")
	}
	return 0
}
