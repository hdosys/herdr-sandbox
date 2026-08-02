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
	dependencies.openConfig = func() (string, error) {
		calls++
		return path, nil
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := runWithCommandDependencies(context.Background(), []string{"config"}, &bytes.Buffer{}, &stdout, &stderr, dependencies)
	if code != 0 || calls != 1 || stdout.String() != "Opened configuration: "+path+"\n" || stderr.Len() != 0 {
		t.Fatalf("code = %d, calls = %d, stdout = %q, stderr = %q", code, calls, stdout.String(), stderr.String())
	}
}

func TestRunConfigRejectsArgumentsAndReportsOpenFailure(t *testing.T) {
	for _, test := range []struct {
		name     string
		args     []string
		open     func() (string, error)
		wantCode int
		wantText string
	}{
		{
			name:     "argument",
			args:     []string{"config", "extra"},
			open:     func() (string, error) { t.Fatal("config owner called for invalid syntax"); return "", nil },
			wantCode: 2,
			wantText: "config does not accept arguments",
		},
		{
			name:     "open failure",
			args:     []string{"config"},
			open:     func() (string, error) { return "", errors.New("no application is registered to open .json files") },
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
	dependencies.openConfig = func() (string, error) {
		t.Fatal("config owner called for help")
		return "", nil
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := runWithCommandDependencies(context.Background(), []string{"config", "--help"}, &bytes.Buffer{}, &stdout, &stderr, dependencies)
	if code != 0 || stdout.String() != usage || stderr.Len() != 0 {
		t.Fatalf("code = %d, stdout = %q, stderr = %q", code, stdout.String(), stderr.String())
	}
}
