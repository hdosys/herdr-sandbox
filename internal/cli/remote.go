package cli

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"herdr-sandbox/internal/sandbox"
)

func runRemoteSetup(ctx context.Context, args []string, input io.Reader, output, stderr io.Writer, dependencies commandDependencies) int {
	flags := flag.NewFlagSet("sandbox remote setup", flag.ContinueOnError)
	flags.SetOutput(stderr)
	var keyFile string
	var yes bool
	flags.StringVar(&keyFile, "key", "", "public key file for an additional device (otherwise import standard host authorized_keys lists)")
	flags.BoolVar(&yes, "yes", false, "approve enabling remote access and the displayed public-key set")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(stderr, "sandbox: unexpected remote setup arguments")
		return 2
	}
	var inputErr error
	saved, err := dependencies.setupRemote(ctx, keyFile, func(plan sandbox.RemoteSetup) bool {
		fmt.Fprintln(output, "Set up remote access")
		fmt.Fprintln(output, "  This computer: automatic access with its existing Sandbox SSH identity.")
		fmt.Fprintf(output, "  Additional authorized keys: %d\n", plan.AdditionalKeys)
		for _, source := range plan.Sources {
			fmt.Fprintf(output, "  Public-key source: %s\n", source)
		}
		fmt.Fprintln(output, "  Authorized devices can use a full Sandbox shell, Herdr remoting and TCP forwarding.")
		fmt.Fprintln(output, "  Existing tailnet policy still applies. Private keys stay on their devices.")
		if yes {
			return true
		}
		fmt.Fprint(output, "Enable this access? [y/N]: ")
		line, err := bufio.NewReader(io.LimitReader(input, 32)).ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			inputErr = err
			return false
		}
		return strings.EqualFold(strings.TrimSpace(line), "y") || strings.EqualFold(strings.TrimSpace(line), "yes")
	})
	if inputErr != nil {
		fmt.Fprintln(stderr, "sandbox: read confirmation:", inputErr)
		return 1
	}
	if err != nil {
		fmt.Fprintln(stderr, "sandbox:", err)
		return 1
	}
	if !saved {
		fmt.Fprintln(output, "Cancelled. Access settings were not changed.")
		return 0
	}
	fmt.Fprintln(output, "Remote settings saved. Applying access to the running Sandbox without restarting or provisioning it...")
	applied, err := dependencies.applyRemoteKeys(ctx)
	if err != nil {
		fmt.Fprintln(stderr, "sandbox: remote settings saved, but access was not confirmed:", err)
		return 1
	}
	if applied {
		fmt.Fprintln(output, "Remote access verified. Existing Sandbox sessions and services were preserved.")
		return runWithCommandDependencies(ctx, []string{"remote"}, input, output, stderr, dependencies)
	}
	fmt.Fprintln(output, "No managed Sandbox exists yet. Starting it now; first-time Tailscale sign-in opens in your browser.")
	if code := runWithCommandDependencies(ctx, []string{"up", "--no-attach"}, input, output, stderr, dependencies); code != 0 {
		fmt.Fprintln(stderr, "Remote settings were saved. The existing Sandbox was not closed or replaced; resolve the reported startup condition and run sandbox up.")
		return code
	}
	return runWithCommandDependencies(ctx, []string{"remote"}, input, output, stderr, dependencies)
}
