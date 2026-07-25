package sandbox

import (
	"strings"
	"testing"
	"unicode/utf8"
)

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
