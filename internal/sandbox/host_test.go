package sandbox

import (
	"context"
	"errors"
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

func TestInspectHostHerdrAtClassifiesReparseAsUnsafe(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target.exe")
	link := filepath.Join(root, "herdr.exe")
	if err := os.WriteFile(target, []byte("fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("file symlink unavailable: %v", err)
	}
	if _, _, err := inspectHostHerdrAt(context.Background(), link); !errors.Is(err, errUnsafeHostHerdrInspection) {
		t.Fatalf("unsafe host Herdr inspection error = %v", err)
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
