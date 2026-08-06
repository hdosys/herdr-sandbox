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

func TestRunRejectsArgumentsForFixedTasks(t *testing.T) {
	for _, task := range []string{"fmt", "build", "check", "native-all-stacks"} {
		err := run(context.Background(), []string{task, "unexpected"}, &bytes.Buffer{}, &bytes.Buffer{})
		if err == nil || !strings.Contains(err.Error(), "accepts no arguments") {
			t.Fatalf("run %s error = %v", task, err)
		}
	}
}

func TestNativeAllStacksUsesExtendedTimeout(t *testing.T) {
	if got := taskTimeoutFor([]string{"native-all-stacks"}); got != nativeAllStacksTaskTimeout {
		t.Fatalf("native timeout = %s", got)
	}
	if got := taskTimeoutFor([]string{"check"}); got != taskTimeout {
		t.Fatalf("ordinary timeout = %s", got)
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
		"-ldflags", "-s -w -X herdr-sandbox/internal/productidentity.Version=0.0.7 -X herdr-sandbox/internal/productidentity.Revision=0123456789abcdef0123456789abcdef01234567",
		"-o", `build\bin\sandbox.exe`,
		"./cmd/sandbox",
	}
	got := goBuildArgs(`build\bin\sandbox.exe`, buildIdentity{Version: "0.0.7", Revision: "0123456789abcdef0123456789abcdef01234567"})
	if !slices.Equal(got, want) {
		t.Fatalf("goBuildArgs = %#v, want %#v", got, want)
	}
	if strings.Contains(strings.Join(got, " "), "-buildid=") {
		t.Fatalf("production build must keep Go's normal build ID: %#v", got)
	}
}

func TestNormalizeSourceRevisionRequiresFullSHA1(t *testing.T) {
	want := "0123456789abcdef0123456789abcdef01234567"
	if got, err := normalizeSourceRevision("  " + strings.ToUpper(want) + "\r\n"); err != nil || got != want {
		t.Fatalf("normalizeSourceRevision = %q, %v", got, err)
	}
	for _, revision := range []string{"", "abc", strings.Repeat("0", 39), strings.Repeat("g", 40), strings.Repeat("0", 41)} {
		if _, err := normalizeSourceRevision(revision); err == nil {
			t.Fatalf("invalid revision unexpectedly succeeded: %q", revision)
		}
	}
}
