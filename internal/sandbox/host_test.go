package sandbox

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestRegularFileExistsRejectsSymbolicLink(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target")
	link := filepath.Join(root, "link")
	if err := os.WriteFile(target, []byte("fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("file symlink unavailable: %v", err)
	}
	if _, err := regularFileExists(link); err == nil || !strings.Contains(err.Error(), "non-reparse") {
		t.Fatalf("symbolic-link regular file error = %v", err)
	}
}

func TestBoundedTextPreservesDiagnosticHeadAndTail(t *testing.T) {
	input := "HEAD:" + strings.Repeat("ä", 1800) + ":TAIL"
	result := boundedText([]byte(input))
	if len(result) > 2000 {
		t.Fatalf("bounded text length = %d", len(result))
	}
	if !utf8.ValidString(result) {
		t.Fatal("bounded text is not valid UTF-8")
	}
	if !strings.HasPrefix(result, "HEAD:") || !strings.HasSuffix(result, ":TAIL") || !strings.Contains(result, "diagnostic truncated") {
		t.Fatalf("bounded text did not preserve head and tail: %q", result)
	}
}

func TestBoundedTextReplacesTerminalControlsAndPreservesLayout(t *testing.T) {
	result := boundedText([]byte("before\x1b]0;forged\x07after\nnext\tcolumn"))
	if strings.ContainsAny(result, "\x1b\x07") {
		t.Fatalf("bounded text retained terminal controls: %q", result)
	}
	if !strings.Contains(result, "before�]0;forged�after\nnext\tcolumn") {
		t.Fatalf("bounded text lost safe diagnostic layout: %q", result)
	}
}
