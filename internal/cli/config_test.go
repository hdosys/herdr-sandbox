package cli

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
)

func TestRunConfigOpensCanonicalConfiguration(t *testing.T) {
	dependencies := defaultCommandDependencies()
	const path = `C:\Users\user\AppData\Roaming\herdr-sandbox\config.json`
	calls := 0
	dependencies.openConfig = func() (string, bool, error) {
		calls++
		return path, true, nil
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := runWithCommandDependencies(context.Background(), []string{"config"}, &bytes.Buffer{}, &stdout, &stderr, dependencies)
	want := "Created default configuration: " + path + "\nNext: save your changes, then run `sandbox plan`.\n"
	if code != 0 || calls != 1 || stdout.String() != want || stderr.Len() != 0 {
		t.Fatalf("code = %d, calls = %d, stdout = %q, stderr = %q", code, calls, stdout.String(), stderr.String())
	}
	dependencies.openConfig = func() (string, bool, error) { return path, false, nil }
	stdout.Reset()
	code = runWithCommandDependencies(context.Background(), []string{"config"}, &bytes.Buffer{}, &stdout, &stderr, dependencies)
	want = "Opened existing configuration: " + path + "\nNext: save your changes, then run `sandbox plan`.\n"
	if code != 0 || stdout.String() != want || stderr.Len() != 0 {
		t.Fatalf("existing config code = %d, stdout = %q, stderr = %q", code, stdout.String(), stderr.String())
	}
}

func TestRunConfigRejectsArgumentsAndReportsOpenFailure(t *testing.T) {
	for _, test := range []struct {
		name     string
		args     []string
		open     func() (string, bool, error)
		wantCode int
		wantText string
	}{
		{
			name: "argument",
			args: []string{"config", "extra"},
			open: func() (string, bool, error) {
				t.Fatal("config owner called for invalid syntax")
				return "", false, nil
			},
			wantCode: 2,
			wantText: "config does not accept arguments",
		},
		{
			name: "open failure",
			args: []string{"config"},
			open: func() (string, bool, error) {
				return "", false, errors.New("no application is registered to open .json files")
			},
			wantCode: 1,
			wantText: "no application is registered to open .json files",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			dependencies := defaultCommandDependencies()
			dependencies.openConfig = test.open
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			code := runWithCommandDependencies(context.Background(), test.args, &bytes.Buffer{}, &stdout, &stderr, dependencies)
			if code != test.wantCode || stdout.Len() != 0 || !strings.Contains(stderr.String(), test.wantText) {
				t.Fatalf("code = %d, stdout = %q, stderr = %q", code, stdout.String(), stderr.String())
			}
		})
	}
}

func TestRunConfigHelpDoesNotOpenConfiguration(t *testing.T) {
	dependencies := defaultCommandDependencies()
	dependencies.openConfig = func() (string, bool, error) {
		t.Fatal("config owner called for help")
		return "", false, nil
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := runWithCommandDependencies(context.Background(), []string{"config", "--help"}, &bytes.Buffer{}, &stdout, &stderr, dependencies)
	if code != 0 || stdout.String() != usage || stderr.Len() != 0 {
		t.Fatalf("code = %d, stdout = %q, stderr = %q", code, stdout.String(), stderr.String())
	}
}
