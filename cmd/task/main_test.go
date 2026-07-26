package main

import (
	"bytes"
	"context"
	"slices"
	"strings"
	"testing"
)

func TestRunPrintsHelp(t *testing.T) {
	var stdout bytes.Buffer
	if err := run(context.Background(), nil, &stdout, &bytes.Buffer{}); err != nil {
		t.Fatalf("run help: %v", err)
	}
	if !strings.Contains(stdout.String(), "go run ./cmd/task") {
		t.Fatalf("help output = %q", stdout.String())
	}
}

func TestRunRejectsUnknownTask(t *testing.T) {
	err := run(context.Background(), []string{"unknown"}, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), `unknown task "unknown"`) {
		t.Fatalf("run error = %v", err)
	}
}

func TestCommandTextQuotesWhitespace(t *testing.T) {
	got := commandText("tool", []string{"plain", `path with spaces`})
	if got != `tool plain "path with spaces"` {
		t.Fatalf("commandText = %q", got)
	}
}

func TestGoBuildArgsUseStrippedProductionBuild(t *testing.T) {
	want := []string{
		"build",
		"-trimpath",
		"-buildvcs=false",
		"-ldflags", "-s -w",
		"-o", `build\bin\herdr-sandbox.exe`,
		"./cmd/herdr-sandbox",
	}
	got := goBuildArgs(`build\bin\herdr-sandbox.exe`)
	if !slices.Equal(got, want) {
		t.Fatalf("goBuildArgs = %#v, want %#v", got, want)
	}
	if strings.Contains(strings.Join(got, " "), "-buildid=") {
		t.Fatalf("production build must keep Go's normal build ID: %#v", got)
	}
}
