package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"

	"herdr-sandbox/internal/cli"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	go func() {
		<-ctx.Done()
		fmt.Fprintln(os.Stderr, "\nCancellation requested. Cleaning up; press Ctrl+C again to force exit.")
		stop()
	}()
	os.Exit(cli.Run(ctx, os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}
