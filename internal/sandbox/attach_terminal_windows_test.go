//go:build windows

package sandbox

import (
	"bytes"
	"strings"
	"testing"
)

func TestValidateInteractiveAttachStreamsRejectsRedirectedStreams(t *testing.T) {
	var stream bytes.Buffer
	err := validateInteractiveAttachStreams(&stream, &stream, &stream)
	if err == nil || !strings.Contains(err.Error(), "stdin is redirected") || !strings.Contains(err.Error(), "herdr --remote sandbox") {
		t.Fatalf("error = %v", err)
	}
}
