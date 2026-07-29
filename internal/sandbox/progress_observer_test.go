package sandbox

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRunWithRetainedProgressReportsAndPersistsChanges(t *testing.T) {
	directory := t.TempDir()
	writeJSON(t, filepath.Join(directory, progressFileName), progressStatus{
		SchemaVersion: statusSchemaVersion,
		Phase:         "old",
		Message:       "stale progress",
	})
	var output bytes.Buffer
	updates := make(chan progressStatus, 1)
	err := runWithRetainedProgress(context.Background(), directory, &output, func(progress progressStatus) error {
		updates <- progress
		return nil
	}, func(ctx context.Context) error {
		writeJSON(t, filepath.Join(directory, progressFileName), progressStatus{
			SchemaVersion: statusSchemaVersion,
			Phase:         "development-provisioning",
			Message:       "Installing modern .NET",
		})
		select {
		case progress := <-updates:
			if progress.Message != "Installing modern .NET" {
				t.Fatalf("progress = %#v", progress)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("retained progress was not observed")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output.String(), "stale progress") || !strings.Contains(output.String(), "Installing modern .NET") {
		t.Fatalf("output = %q", output.String())
	}
}

func TestRunWithRetainedProgressCancelsCommandWhenPersistenceFails(t *testing.T) {
	directory := t.TempDir()
	want := errors.New("operation persistence failed")
	err := runWithRetainedProgress(context.Background(), directory, &bytes.Buffer{}, func(progressStatus) error {
		return want
	}, func(ctx context.Context) error {
		writeJSON(t, filepath.Join(directory, progressFileName), progressStatus{
			SchemaVersion: statusSchemaVersion,
			Phase:         "development-provisioning",
			Message:       "Installing",
		})
		<-ctx.Done()
		return ctx.Err()
	})
	if !errors.Is(err, want) {
		t.Fatalf("error = %v", err)
	}
}
