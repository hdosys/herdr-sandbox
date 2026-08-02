//go:build windows

package sandbox

import (
	"strings"
	"testing"
)

func TestShellExecuteResultRequiresRegisteredApplication(t *testing.T) {
	if err := shellExecuteResultError(33); err != nil {
		t.Fatalf("successful ShellExecute result: %v", err)
	}
	if err := shellExecuteResultError(shellNoAssociation); err == nil || !strings.Contains(err.Error(), ".json") {
		t.Fatalf("missing association error = %v", err)
	}
	if err := shellExecuteResultError(5); err == nil || !strings.Contains(err.Error(), "code 5") {
		t.Fatalf("ShellExecute failure = %v", err)
	}
}
