package sandbox

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

type retainedProgressUpdate func(progressStatus) error

func runWithRetainedProgress(
	ctx context.Context,
	statusDirectory string,
	output io.Writer,
	update retainedProgressUpdate,
	run func(context.Context) error,
) error {
	if output == nil {
		output = io.Discard
	}
	if err := removeRetainedProgress(statusDirectory); err != nil {
		return err
	}
	commandContext, cancelCommand := context.WithCancel(ctx)
	defer cancelCommand()
	observerContext, cancelObserver := context.WithCancel(commandContext)
	observerDone := make(chan error, 1)
	go func() {
		err := observeRetainedProgress(observerContext, statusDirectory, output, update)
		if err != nil {
			cancelCommand()
		}
		observerDone <- err
	}()
	runErr := run(commandContext)
	cancelObserver()
	observerErr := <-observerDone
	if observerErr != nil {
		if runErr != nil && !errors.Is(runErr, context.Canceled) {
			return fmt.Errorf("observe retained provisioning progress: %w; retained command also failed: %v", observerErr, runErr)
		}
		return fmt.Errorf("observe retained provisioning progress: %w", observerErr)
	}
	return runErr
}

func observeRetainedProgress(ctx context.Context, statusDirectory string, output io.Writer, update retainedProgressUpdate) error {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	startedAt := time.Now()
	last := ""
	var readErrorSince time.Time
	for {
		progress, found, err := readOptionalStatus[progressStatus](filepath.Join(statusDirectory, progressFileName))
		if err != nil {
			now := time.Now()
			if readErrorSince.IsZero() {
				readErrorSince = now
			}
			if now.Sub(readErrorSince) >= progressReadGrace {
				return fmt.Errorf("read retained Sandbox progress after %s grace: %w", progressReadGrace, err)
			}
		} else if found {
			readErrorSince = time.Time{}
			if err := progress.validate(); err != nil {
				return fmt.Errorf("validate retained Sandbox progress: %w", err)
			}
			current := progress.Phase + "\x00" + progress.Message
			if current != last {
				elapsed := time.Since(startedAt).Round(100 * time.Millisecond)
				fmt.Fprintf(output, "[+%s] [%s] %s\n", elapsed, progress.Phase, progress.Message)
				if update != nil {
					if err := update(progress); err != nil {
						return err
					}
				}
				last = current
			}
		} else {
			readErrorSince = time.Time{}
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

func removeRetainedProgress(statusDirectory string) error {
	if !filepath.IsAbs(statusDirectory) {
		return fmt.Errorf("retained status directory is not absolute: %q", statusDirectory)
	}
	path := filepath.Join(filepath.Clean(statusDirectory), progressFileName)
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect retained progress: %w", err)
	}
	reparse, err := fileInfoIsReparsePoint(info)
	if err != nil {
		return fmt.Errorf("inspect retained progress reparse state: %w", err)
	}
	if reparse || !info.Mode().IsRegular() {
		return errors.New("retained progress is not a regular non-reparse file")
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("remove stale retained progress: %w", err)
	}
	return nil
}
